package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/FutureAI-X/futureai-api/storage"
	"github.com/FutureAI-X/futureai-api/supplier"
	"github.com/FutureAI-X/futureai-api/videoparam"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// videoGenerationsPath 视频生成端点路径。
// 必须与 endpoints 表里手动创建的记录一致，否则 GetEndpointByPath 会返回 404。
const videoGenerationsPath = "/v1/videos/generations"

const (
	// videoReferenceSignTTL 给上游的参考媒体预签名有效期。
	// 文件本身只有 6 小时 TTL，实际 TTL 还会被剩余有效期截断。
	videoReferenceSignTTL = 2 * time.Hour
	// videoReferenceMinRemaining 参考文件至少还要保留这么久才允许提交生成。
	videoReferenceMinRemaining = 30 * time.Minute
)

// videoGenerateRequest 视频生成端点的公开请求。
//
// 参考素材只接受平台 file_id：客户端先调 /v1/files/upload/complete 拿到
// file_id，再填进 image_ids/video_ids/audio_ids。字段名与图像生成端点的
// image_ids 保持一致，_ids 后缀明确表示这里是文件标识而不是 URL；
// 外部 URL 一律拒绝。顶层其他字段由 encoding/json 忽略。
type videoGenerateRequest struct {
	Model      string   `json:"model"`
	Prompt     string   `json:"prompt"`
	ImageIDs   []string `json:"image_ids"`
	VideoIDs   []string `json:"video_ids"`
	AudioIDs   []string `json:"audio_ids"`
	Resolution string   `json:"resolution"`
	Duration   int      `json:"duration"`
	Ratio      string   `json:"ratio"`
}

// validate 校验公开请求的标量字段。
//
// 参考媒体的 file_id 形态放到规格解析之后校验：模型不支持某类媒体时，
// 该字段会被静默忽略，而不是因为调用方多发了一个非法值就直接 400。
func (r *videoGenerateRequest) validate() error {
	if strings.TrimSpace(r.Model) == "" {
		return errors.New("缺少 model 参数")
	}
	if strings.TrimSpace(r.Prompt) == "" {
		return errors.New("缺少 prompt 参数")
	}
	if strings.TrimSpace(r.Resolution) == "" {
		return errors.New("缺少 resolution 参数")
	}
	if r.Duration <= 0 {
		return errors.New("duration 必须是正整数")
	}
	if strings.TrimSpace(r.Ratio) == "" {
		return errors.New("缺少 ratio 参数")
	}
	return nil
}

// validateMediaIDs 校验三个参考媒体数组的 file_id 形态。
func (r *videoGenerateRequest) validateMediaIDs() error {
	groups := []struct {
		name string
		ids  []string
	}{
		{"image_ids", r.ImageIDs},
		{"video_ids", r.VideoIDs},
		{"audio_ids", r.AudioIDs},
	}
	for _, group := range groups {
		for _, id := range group.ids {
			id = strings.TrimSpace(id)
			if id == "" {
				return fmt.Errorf("%s 中存在空的 file_id", group.name)
			}
			if strings.Contains(id, "://") {
				return fmt.Errorf("%s 只接受平台 file_id，不接受 URL", group.name)
			}
			if !strings.HasPrefix(id, "file_") {
				return fmt.Errorf("%s 中存在无效的 file_id: %s", group.name, id)
			}
			if len(id) > model.MaxFileIDLen {
				return fmt.Errorf("%s 中的 file_id 过长", group.name)
			}
		}
	}
	return nil
}

// clearUnsupportedVideoMedia 清空规格未开放的参考媒体字段。
//
// max_count=0 或规格中没有对应控件，都表示模型不支持该媒体：跳过 file_id
// 校验、不向上游发送，也不计入计费。任务快照仍保留调用方原始请求。
func clearUnsupportedVideoMedia(req *videoGenerateRequest, spec *videoparam.Spec) {
	clearField := func(p *videoparam.Param, ids *[]string, name string) {
		if p != nil && p.MaxCount != nil && *p.MaxCount > 0 {
			return
		}
		if len(*ids) > 0 {
			common.SysLogf("[VideoGenerate] 模型不支持 %s，已忽略调用方传入的 %d 个文件", name, len(*ids))
		}
		*ids = nil
	}
	clearField(spec.ParamByPath("image_ids"), &req.ImageIDs, "image_ids")
	clearField(spec.ParamByPath("video_ids"), &req.VideoIDs, "video_ids")
	clearField(spec.ParamByPath("audio_ids"), &req.AudioIDs, "audio_ids")
}

// writeVideoParamError 输出与图像生成一致的参数错误结构。
func writeVideoParamError(c *gin.Context, err *videoparam.Error) {
	c.JSON(http.StatusBadRequest, gin.H{
		"code":       "fail",
		"error_code": err.Code,
		"param":      err.Param,
		"message":    err.Message,
	})
}

// videoReferenceError 参考文件校验失败，携带对调用方可见的 HTTP 状态码。
type videoReferenceError struct {
	status  int
	message string
}

func (e *videoReferenceError) Error() string { return e.message }

type resolvedVideoReferences struct {
	ImageURLs []string
	VideoURLs []string
	AudioURLs []string

	// InputVideoSeconds 去重后每个参考视频按秒向上取整再求和，用于输入计费。
	InputVideoSeconds int

	// 以下为已解析文件的服务端探测元数据，用于参数规格校验。
	ImageFiles []videoparam.RefFile
	VideoFiles []videoparam.RefFile
	AudioFiles []videoparam.RefFile
}

// parseUploadedMediaMeta 解析上传完成时落库的探测结果。
func parseUploadedMediaMeta(raw string) uploadedMediaMeta {
	var meta uploadedMediaMeta
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &meta)
	}
	return meta
}

// dedupeVideoFileIDs 去重并保持首次出现顺序；空串在 validate 阶段已被拒绝。
func dedupeVideoFileIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// resolveVideoReferences 把 file_id 解析成上游可直接抓取的短期签名 URL。
//
// 允许 purpose=reference（上传的参考媒体）和 purpose=result（用户自己的
// 生成结果），但必须属于当前用户、已完成处理、未过期，且媒体类型匹配。
func resolveVideoReferences(ctx context.Context, userID int, req *videoGenerateRequest) (*resolvedVideoReferences, error) {
	now := time.Now()

	resolve := func(ids []string, mediaType, group string) ([]string, int, []videoparam.RefFile, error) {
		ids = dedupeVideoFileIDs(ids)
		urls := make([]string, 0, len(ids))
		files := make([]videoparam.RefFile, 0, len(ids))
		videoSeconds := 0

		for _, id := range ids {
			file, err := model.GetFileByFileID(id)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, 0, nil, &videoReferenceError{http.StatusNotFound, "参考文件不存在或无权使用"}
				}
				return nil, 0, nil, fmt.Errorf("查询参考文件 %s 失败: %w", id, err)
			}

			if file.UserID != userID ||
				(file.Purpose != model.FilePurposeReference && file.Purpose != model.FilePurposeResult) {
				return nil, 0, nil, &videoReferenceError{http.StatusNotFound, "参考文件不存在或无权使用"}
			}
			if file.Status != model.FileStatusReady {
				return nil, 0, nil, &videoReferenceError{http.StatusConflict, "参考文件尚未完成处理"}
			}
			if file.MediaType != mediaType {
				return nil, 0, nil, &videoReferenceError{http.StatusBadRequest,
					fmt.Sprintf("%s 中存在类型不匹配的参考文件", group)}
			}
			if mediaType == model.FileMediaTypeVideo && file.DurationMS <= 0 {
				return nil, 0, nil, &videoReferenceError{http.StatusBadRequest, "参考视频缺少时长信息"}
			}
			if strings.TrimSpace(file.FileKey) == "" {
				return nil, 0, nil, &videoReferenceError{http.StatusServiceUnavailable, "参考文件存储信息不完整"}
			}

			ttl := videoReferenceSignTTL
			if file.ExpiresAt != nil && !file.ExpiresAt.IsZero() {
				remaining := file.ExpiresAt.Sub(now)
				if remaining < videoReferenceMinRemaining {
					return nil, 0, nil, &videoReferenceError{http.StatusGone, "参考文件已过期或即将过期，请重新上传"}
				}
				if remaining < ttl {
					// 留 1 分钟余量，避免签名地址比对象存活更久。
					ttl = remaining - time.Minute
				}
			}
			if ttl <= 0 {
				return nil, 0, nil, &videoReferenceError{http.StatusGone, "参考文件即将过期，请重新上传"}
			}

			store, err := storage.MediaFrom(file.ServiceType, file.ServiceAddress)
			if err != nil {
				common.SysErrorf("[VideoGenerate] 参考文件存储不可用: fileID=%s, err=%v", id, err)
				return nil, 0, nil, &videoReferenceError{http.StatusServiceUnavailable, "参考文件存储暂不可用"}
			}
			signed, err := store.PresignGet(ctx, file.FileKey, ttl)
			if err != nil {
				common.SysErrorf("[VideoGenerate] 参考文件签名失败: fileID=%s, err=%v", id, err)
				return nil, 0, nil, &videoReferenceError{http.StatusServiceUnavailable, "参考文件存储暂不可用"}
			}
			urls = append(urls, signed)

			ref := videoparam.RefFile{
				ID:          id,
				Bytes:       file.Bytes,
				ContentType: file.ContentType,
				Width:       file.Width,
				Height:      file.Height,
				DurationMS:  file.DurationMS,
			}
			if mediaType == model.FileMediaTypeVideo {
				meta := parseUploadedMediaMeta(file.MediaMeta)
				ref.FPS = meta.FPS
				ref.VideoCodecs = meta.VideoCodecs
				ref.AudioCodecs = meta.AudioCodecs
				// 计费与参数校验使用同一口径：逐文件向上取整到整秒。
				videoSeconds += ceilVideoSeconds(file.DurationMS)
			}
			files = append(files, ref)
		}

		return urls, videoSeconds, files, nil
	}

	images, _, imageFiles, err := resolve(req.ImageIDs, model.FileMediaTypeImage, "image_ids")
	if err != nil {
		return nil, err
	}
	videos, videoSeconds, videoFiles, err := resolve(req.VideoIDs, model.FileMediaTypeVideo, "video_ids")
	if err != nil {
		return nil, err
	}
	audios, _, audioFiles, err := resolve(req.AudioIDs, model.FileMediaTypeAudio, "audio_ids")
	if err != nil {
		return nil, err
	}

	return &resolvedVideoReferences{
		ImageURLs:         images,
		VideoURLs:         videos,
		AudioURLs:         audios,
		InputVideoSeconds: videoSeconds,
		ImageFiles:        imageFiles,
		VideoFiles:        videoFiles,
		AudioFiles:        audioFiles,
	}, nil
}

// VideoGenerate 视频生成端点
// POST /v1/videos/generations
//
// 流转逻辑与图像生成一致：校验 → 端点/模型/供应商路由 → 解析参考文件 →
// 计算积分 → 建任务并原子扣费 → 提交上游 → 记录供应商响应 → 对账循环轮询。差异：
//   - 参考素材只接受平台 file_id，服务端解析成短期签名 URL 后交给上游；
//   - 计费走独立的视频规则：图片按张（含免费张数）、音频按个、参考视频按秒，
//     输出按请求时长；费率随输出分辨率分档；
//   - 所有数量在提交时已知，预扣即终值，完成后不做二次结算；
//   - 结果不转存到对象存储，任务完成时直接把上游 URL 返回给调用方。
func VideoGenerate(c *gin.Context) {
	var req videoGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if common.IsBodyTooLarge(err) {
			common.SysErrorf("[VideoGenerate] 请求体超过上限: userID=%d", c.GetInt("user_id"))
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "fail", "message": "请求体过大"})
			return
		}
		common.SysErrorf("[VideoGenerate] 请求体解析失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "请求体格式错误"})
		return
	}

	// 本地先校验必填字段与 file_id 形态：参数错误不应留下任务行。
	if err := req.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": err.Error()})
		return
	}

	modelName := req.Model
	common.SysLogf("[VideoGenerate] 收到请求: model=%s", modelName)

	// 1. 根据端点路径查找端点
	endpoint, err := model.GetEndpointByPath(videoGenerationsPath)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 端点不存在: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"code": "fail", "message": "端点不存在"})
		return
	}

	// 2. 根据 model 名称查找模型
	m, err := model.GetModelByName(modelName)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 模型不存在或已禁用: model=%s, err=%v", modelName, err)
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "模型不存在或已禁用"})
		return
	}

	// 类型不参与路由判断（端点绑定才是闸门），但配错会让 /v1/models 的 type
	// 与实际能力不符，留一条日志便于发现。
	if m.Type != model.ModelTypeVideo {
		common.SysErrorf("[VideoGenerate] 模型类型不是 video，请检查管理端配置: model=%s, type=%s", modelName, m.Type)
	}

	// 3. 验证模型是否支持此端点
	modelEndpoints, err := model.GetModelEndpoints(m.ID)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 查询端点关联失败: modelID=%d, err=%v", m.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "查询端点关联失败"})
		return
	}
	supported := false
	for _, me := range modelEndpoints {
		if me.EndpointID == endpoint.ID {
			supported = true
			break
		}
	}
	if !supported {
		common.SysErrorf("[VideoGenerate] 模型不支持此端点: model=%s, endpoint=%s", modelName, endpoint.Path)
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "该模型不支持视频生成端点"})
		return
	}

	// 4. 查找可用供应商（关联启用且供应商本身也启用）
	vendorModels, err := model.GetEnabledVendorModelsByModelID(m.ID)
	if err != nil || len(vendorModels) == 0 {
		common.SysErrorf("[VideoGenerate] 无可用供应商: model=%s, err=%v", modelName, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "无可用供应商"})
		return
	}

	vendorModel := vendorModels[0]
	vendor, err := model.GetEnabledVendorByID(vendorModel.VendorID)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 供应商不可用: vendorID=%d, err=%v", vendorModel.VendorID, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "无可用供应商"})
		return
	}

	common.SysLogf("[VideoGenerate] 选择供应商: %s (vendorID=%d), 供应商模型ID: %s",
		vendor.Name, vendor.ID, vendorModel.VendorModelID)

	// 5. 解密 API Key
	apiKey, err := common.DecryptSecret(vendor.APIKey)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 供应商密钥解密失败: vendor=%s, err=%v", vendor.Name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "服务暂时不可用，请稍后再试"})
		return
	}

	// 6. 确认调用方身份（由 APIAuth 中间件从 API Key 解析）
	userID := c.GetInt("user_id")
	if userID <= 0 {
		common.SysErrorf("[VideoGenerate] 缺少有效用户身份: userID=%d", userID)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"message": "无效的 API Key", "type": "authentication_error"},
		})
		return
	}

	// 7. 幂等键。语义与图像生成一致：窗口内重复提交复用已有任务。
	// 必须在解析 file_id 之前：参考文件过期后重试仍应返回原任务，而不是 410。
	idemKey, ok := parseIdempotencyKey(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "Idempotency-Key 过长"})
		return
	}
	if idemKey != nil {
		if existing, err := model.GetTaskByUserAndIdempotencyKey(userID, *idemKey); err == nil {
			common.SysLogf("[VideoGenerate] 命中幂等键，复用已有任务: userID=%d, taskID=%s", userID, existing.TaskID)
			c.JSON(http.StatusOK, gin.H{"code": "success", "taskId": existing.TaskID})
			return
		}
	}

	// 8. 读取并校验视频参数规格。规格缺失或非法直接失败（fail-closed），
	// 与图像生成一致：没有规格就不知道模型接受什么，不能放行。
	spec, specErr := videoparam.ParseSpec(string(m.ParamSpec))
	if specErr != nil {
		if errors.Is(specErr, videoparam.ErrSpecMissing) {
			common.SysErrorf("[VideoGenerate] 模型未配置视频参数规格: model=%s", modelName)
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"code":       "fail",
				"error_code": "PARAM_SPEC_MISSING",
				"message":    "该模型参数配置缺失，暂不可用",
			})
			return
		}
		common.SysErrorf("[VideoGenerate] 模型视频参数规格无效: model=%s, err=%v", modelName, specErr)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":       "fail",
			"error_code": "PARAM_SPEC_INVALID",
			"message":    "该模型参数配置无效，暂不可用",
		})
		return
	}

	// 快照保留调用方原始请求：下面会清空模型不支持的参考媒体字段。
	requestSnapshot := marshalRequestBodyFor("VideoGenerate", req)

	resolution := videoparam.NormalizeResolution(req.Resolution)
	ratio := videoparam.NormalizeRatio(req.Ratio)

	// 模型不支持的参考媒体静默忽略；之后再校验保留字段的 file_id 形态。
	clearUnsupportedVideoMedia(&req, spec)
	if err := req.validateMediaIDs(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": err.Error()})
		return
	}

	values := videoparam.Values{
		"prompt":     req.Prompt,
		"resolution": resolution,
		"duration":   req.Duration,
		"ratio":      ratio,
		"image_ids":  req.ImageIDs,
		"video_ids":  req.VideoIDs,
		"audio_ids":  req.AudioIDs,
	}
	if paramErr := spec.Validate(values); paramErr != nil {
		writeVideoParamError(c, paramErr)
		return
	}

	// 9. 解析计费配置。分辨率统一大写后查价目档位；查不到即拒绝——
	// 0 费率是显式免费，缺档位是配置/请求错误，两者不能混为一谈。
	// 放在幂等键之后：同一幂等键的重试即使档位后来被下架，也应返回原任务。
	billingRule, err := model.GetVideoCreditRuleByModelID(m.ID)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 视频计费规则缺少或不可用: model=%s, err=%v", modelName, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "该模型未配置计费规则，暂不可用"})
		return
	}
	if len(billingRule.Rates) == 0 {
		common.SysErrorf("[VideoGenerate] 视频计费规则未配置分辨率档位: model=%s, ruleID=%d", modelName, billingRule.ID)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "该模型未配置计费档位，暂不可用"})
		return
	}
	billingRate := model.FindVideoCreditRate(billingRule, resolution)
	if billingRate == nil {
		supported := model.SupportedVideoResolutions(billingRule)
		common.SysErrorf("[VideoGenerate] 不支持的输出分辨率: model=%s, resolution=%s, supported=%v",
			modelName, resolution, supported)
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "fail",
			"message": fmt.Sprintf("不支持的输出分辨率：%s，支持：%s", resolution, strings.Join(supported, "、")),
		})
		return
	}

	// 10. 解析参考文件：校验归属/用途/状态/有效期/媒体类型，并生成短期签名 URL。
	// 先在解析前做一次数量检查，超量请求不必逐个查文件。
	if paramErr := videoparam.ValidateRefCount(spec.ParamByPath("image_ids"), len(dedupeVideoFileIDs(req.ImageIDs))); paramErr != nil {
		writeVideoParamError(c, paramErr)
		return
	}
	if paramErr := videoparam.ValidateRefCount(spec.ParamByPath("video_ids"), len(dedupeVideoFileIDs(req.VideoIDs))); paramErr != nil {
		writeVideoParamError(c, paramErr)
		return
	}
	if paramErr := videoparam.ValidateRefCount(spec.ParamByPath("audio_ids"), len(dedupeVideoFileIDs(req.AudioIDs))); paramErr != nil {
		writeVideoParamError(c, paramErr)
		return
	}
	refs, err := resolveVideoReferences(c.Request.Context(), userID, &req)
	if err != nil {
		var refErr *videoReferenceError
		if errors.As(err, &refErr) {
			common.SysErrorf("[VideoGenerate] 参考文件校验失败: userID=%d, status=%d, message=%s",
				userID, refErr.status, refErr.message)
			c.JSON(refErr.status, gin.H{"code": "fail", "message": refErr.message})
			return
		}
		common.SysErrorf("[VideoGenerate] 解析参考文件失败: userID=%d, err=%v", userID, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "参考文件暂时不可用，请稍后再试"})
		return
	}

	// 11. 按规格校验参考媒体的数量、格式、大小、宽高、时长、帧率与编码。
	// 时长与计费同口径：每个文件先向上取整到整秒再判断。
	if paramErr := videoparam.ValidateRefMedia(spec.ParamByPath("image_ids"), refs.ImageFiles); paramErr != nil {
		writeVideoParamError(c, paramErr)
		return
	}
	if paramErr := videoparam.ValidateRefMedia(spec.ParamByPath("video_ids"), refs.VideoFiles); paramErr != nil {
		writeVideoParamError(c, paramErr)
		return
	}
	if paramErr := videoparam.ValidateRefMedia(spec.ParamByPath("audio_ids"), refs.AudioFiles); paramErr != nil {
		writeVideoParamError(c, paramErr)
		return
	}

	// 12. 计算积分：图片按张（每请求前 N 张免费）、音频按个、参考视频按秒、
	// 输出按请求时长；参考视频秒数已按文件逐个向上取整。
	billing, err := computeVideoBilling(billingRule, *billingRate, videoBillingInput{
		Resolution:        resolution,
		ImageCount:        len(refs.ImageURLs),
		AudioCount:        len(refs.AudioURLs),
		InputVideoSeconds: refs.InputVideoSeconds,
		OutputSeconds:     req.Duration,
		HasReferenceVideo: len(refs.VideoURLs) > 0,
	})
	if err != nil {
		common.SysErrorf("[VideoGenerate] 视频计费计算失败: model=%s, err=%v", modelName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "计费配置无效，暂不可用"})
		return
	}
	billingJSON, err := json.Marshal(billing.Snapshot)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 计费快照序列化失败: model=%s, err=%v", modelName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "计费配置无效，暂不可用"})
		return
	}
	common.SysLogf("[VideoGenerate] 计费: model=%s, userID=%d, resolution=%s, hasRefVideo=%v, "+
		"基础=%.3f, 图=%d(计费%d), 音频=%d, 视频=%d秒, 输出=%d秒, 合计=%.3f",
		modelName, userID, resolution, billing.Snapshot.HasReferenceVideo,
		billing.Snapshot.Charges.Base,
		billing.Snapshot.Units.ImageCount, billing.Snapshot.Units.BillableImageCount,
		billing.Snapshot.Units.AudioCount, billing.Snapshot.Units.InputVideoSeconds,
		billing.Snapshot.Units.OutputSeconds, billing.Total)

	// 13. 组装供应商适配层所需的平台内部请求（这里装的是签名 URL）。
	// 分辨率与比例用归一化后的形式；供应商侧的特殊写法由适配层转换。
	internalReq := supplier.VideoGenerateRequest{
		Model:         modelName,
		Prompt:        req.Prompt,
		ImageURLs:     refs.ImageURLs,
		VideoURLs:     refs.VideoURLs,
		AudioURLs:     refs.AudioURLs,
		Resolution:    resolution,
		Duration:      req.Duration,
		Ratio:         ratio,
		VendorModelID: vendorModel.VendorModelID,
	}
	if err := internalReq.Validate(); err != nil {
		common.SysErrorf("[VideoGenerate] 内部请求校验失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "服务暂时不可用，请稍后再试"})
		return
	}

	// 14. 创建任务并原子扣费；任一步失败整体回滚，不会留下未付款的任务。
	// 快照落库的是公开请求（file_id），不是带签名 URL 的内部请求。
	now := time.Now()
	submitDeadline := now.Add(videoSubmitConfirmWindow)
	deadlineAt := now.Add(videoResultDeadline)
	task := model.Task{
		TaskID:         model.GenerateTaskID(),
		UserID:         userID,
		VendorID:       vendor.ID,
		ModelID:        m.ID,
		EndpointID:     endpoint.ID,
		Status:         model.StatusProcessing,
		Kind:           model.TaskKindVideo,
		SubmitDeadline: &submitDeadline,
		DeadlineAt:     &deadlineAt,
		NextPollAt:     &now,
		// 视频的数量在提交时全部已知，预扣即终值，因此直接标记为已结算。
		CreditsSettled: true,
		SettledCredits: billing.Total,
		VideoBilling:   model.JSONText(billingJSON),
		RequestBody:    requestSnapshot,
		IdempotencyKey: idemKey,
	}

	if err := model.CreateTaskAndDeduct(&task, billing.Total, videoBillingRemark(billing.Snapshot)); err != nil {
		if idemKey != nil && model.IsDuplicateKeyError(err) {
			if existing, lookupErr := model.GetTaskByUserAndIdempotencyKey(userID, *idemKey); lookupErr == nil {
				common.SysLogf("[VideoGenerate] 幂等键并发冲突，复用已有任务: userID=%d, taskID=%s", userID, existing.TaskID)
				c.JSON(http.StatusOK, gin.H{"code": "success", "taskId": existing.TaskID})
				return
			}
		}

		if errors.Is(err, model.ErrInsufficientCredits) {
			common.SysErrorf("[VideoGenerate] 积分不足: userID=%d, amount=%.6f", userID, billing.Total)
			c.JSON(http.StatusPaymentRequired, gin.H{"code": "fail", "message": "积分不足"})
			return
		}

		common.SysErrorf("[VideoGenerate] 任务创建失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "任务创建失败"})
		return
	}

	// 15. 构造供应商客户端
	s := supplier.NewVideoSupplier(vendor.Name, supplier.Config{
		BaseURL: vendor.BaseURL,
		APIKey:  apiKey,
	})
	if s == nil {
		common.SysErrorf("[VideoGenerate] 不支持的视频供应商类型: %s", vendor.Name)
		model.FailTask(task.TaskID, "internal_error", "服务暂时不可用", `{"error":"unsupported vendor"}`)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "不支持的供应商类型"})
		return
	}

	// 提交用独立带超时的 context：客户端断开时若直接取消上游调用，
	// 我们就再也不知道它有没有建单（task_id 只存在于响应里）。
	submitCtx, cancelSubmit := context.WithTimeout(context.Background(), supplier.SubmitTimeout)
	defer cancelSubmit()

	result := s.VideoGenerate(submitCtx, internalReq)

	// 16. 上游明确拒绝受理（4xx、业务失败码、请求没发出去）→ 转终态并退款
	if result.Code != "success" && result.FailureKind != supplier.FailureUnknown {
		common.SysErrorf("[VideoGenerate] 供应商明确拒绝: vendor=%s, model=%s, kind=%s",
			vendor.Name, modelName, result.FailureKind)
		if err := model.FailTask(task.TaskID, "vendor_rejected", "上游拒绝受理", `{"error":"vendor rejected"}`); err != nil {
			common.SysErrorf("[VideoGenerate] 更新任务状态失败: taskID=%s, err=%v", task.TaskID, err)
		}
		c.JSON(http.StatusOK, gin.H{"code": "fail", "message": "供应商调用失败"})
		return
	}

	// 17. 结果不确定（超时、连接中断、响应读不完整）。
	// 任务保持 processing，由 submit_deadline 兜底；调用方拿到 taskId 后继续轮询。
	if result.FailureKind == supplier.FailureUnknown {
		common.SysErrorf("[VideoGenerate] [SUBMIT_UNKNOWN] 提交结果不确定，进入提交确认窗口: taskID=%s, userID=%d, vendor=%s, model=%s",
			task.TaskID, userID, vendor.Name, modelName)
		c.JSON(http.StatusOK, gin.H{
			"code":    "success",
			"taskId":  task.TaskID,
			"message": "提交结果待确认",
		})
		return
	}

	// 18. 提交成功：记录供应商响应并交给对账循环轮询。
	// vendor_response 是重启后找回上游任务的唯一线索，写失败要重试并在日志里标出来。
	vendorRespJSON, err := json.Marshal(result.Data)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 供应商响应序列化失败: taskID=%s, err=%v", task.TaskID, err)
	}
	if err := setVendorResponseWithRetry("VideoGenerate", task.TaskID, string(vendorRespJSON)); err != nil {
		common.SysErrorf("[VideoGenerate] [SUBMIT_UNKNOWN] 记录供应商响应失败，重启后该任务将无法对账: taskID=%s, err=%v",
			task.TaskID, err)
	}

	common.SysLogf("[VideoGenerate] 任务创建成功: taskId=%s, vendor=%s, model=%s", task.TaskID, vendor.Name, modelName)

	// 19. 立即安排一次轮询；抢不到并发位也无妨，下一轮对账循环会接手。
	ScheduleTaskPoll(task.TaskID)

	// 20. 返回系统 taskId
	c.JSON(http.StatusOK, gin.H{
		"code":   "success",
		"taskId": task.TaskID,
	})
}
