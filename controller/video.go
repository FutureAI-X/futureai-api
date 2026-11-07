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
// file_id，再填进 images/videos/audios。外部 URL 一律拒绝。
// 顶层其他字段由 encoding/json 忽略。
type videoGenerateRequest struct {
	Model      string   `json:"model"`
	Prompt     string   `json:"prompt"`
	Images     []string `json:"images"`
	Videos     []string `json:"videos"`
	Audios     []string `json:"audios"`
	Resolution string   `json:"resolution"`
	Duration   int      `json:"duration"`
	Ratio      string   `json:"ratio"`
}

// validate 校验公开请求的必填字段与 file_id 形态。
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

	groups := []struct {
		name string
		ids  []string
	}{
		{"images", r.Images},
		{"videos", r.Videos},
		{"audios", r.Audios},
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

// videoReferenceError 参考文件校验失败，携带对调用方可见的 HTTP 状态码。
type videoReferenceError struct {
	status  int
	message string
}

func (e *videoReferenceError) Error() string { return e.message }

type resolvedVideoReferences struct {
	Images []string
	Videos []string
	Audios []string
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

	resolve := func(ids []string, mediaType, group string) ([]string, error) {
		ids = dedupeVideoFileIDs(ids)
		urls := make([]string, 0, len(ids))

		for _, id := range ids {
			file, err := model.GetFileByFileID(id)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, &videoReferenceError{http.StatusNotFound, "参考文件不存在或无权使用"}
				}
				return nil, fmt.Errorf("查询参考文件 %s 失败: %w", id, err)
			}

			if file.UserID != userID ||
				(file.Purpose != model.FilePurposeReference && file.Purpose != model.FilePurposeResult) {
				return nil, &videoReferenceError{http.StatusNotFound, "参考文件不存在或无权使用"}
			}
			if file.Status != model.FileStatusReady {
				return nil, &videoReferenceError{http.StatusConflict, "参考文件尚未完成处理"}
			}
			if file.MediaType != mediaType {
				return nil, &videoReferenceError{http.StatusBadRequest,
					fmt.Sprintf("%s 中存在类型不匹配的参考文件", group)}
			}
			if mediaType == model.FileMediaTypeVideo && file.DurationMS <= 0 {
				return nil, &videoReferenceError{http.StatusBadRequest, "参考视频缺少时长信息"}
			}
			if strings.TrimSpace(file.FileKey) == "" {
				return nil, &videoReferenceError{http.StatusServiceUnavailable, "参考文件存储信息不完整"}
			}

			ttl := videoReferenceSignTTL
			if file.ExpiresAt != nil && !file.ExpiresAt.IsZero() {
				remaining := file.ExpiresAt.Sub(now)
				if remaining < videoReferenceMinRemaining {
					return nil, &videoReferenceError{http.StatusGone, "参考文件已过期或即将过期，请重新上传"}
				}
				if remaining < ttl {
					// 留 1 分钟余量，避免签名地址比对象存活更久。
					ttl = remaining - time.Minute
				}
			}
			if ttl <= 0 {
				return nil, &videoReferenceError{http.StatusGone, "参考文件即将过期，请重新上传"}
			}

			store, err := storage.MediaFrom(file.ServiceType, file.ServiceAddress)
			if err != nil {
				common.SysErrorf("[VideoGenerate] 参考文件存储不可用: fileID=%s, err=%v", id, err)
				return nil, &videoReferenceError{http.StatusServiceUnavailable, "参考文件存储暂不可用"}
			}
			signed, err := store.PresignGet(ctx, file.FileKey, ttl)
			if err != nil {
				common.SysErrorf("[VideoGenerate] 参考文件签名失败: fileID=%s, err=%v", id, err)
				return nil, &videoReferenceError{http.StatusServiceUnavailable, "参考文件存储暂不可用"}
			}
			urls = append(urls, signed)
		}

		return urls, nil
	}

	images, err := resolve(req.Images, model.FileMediaTypeImage, "images")
	if err != nil {
		return nil, err
	}
	videos, err := resolve(req.Videos, model.FileMediaTypeVideo, "videos")
	if err != nil {
		return nil, err
	}
	audios, err := resolve(req.Audios, model.FileMediaTypeAudio, "audios")
	if err != nil {
		return nil, err
	}

	return &resolvedVideoReferences{Images: images, Videos: videos, Audios: audios}, nil
}

// VideoGenerate 视频生成端点
// POST /v1/videos/generations
//
// 流转逻辑与图像生成一致：校验 → 端点/模型/供应商路由 → 解析参考文件 →
// 建任务 → 提交上游 → 记录供应商响应 → 对账循环轮询。差异：
//   - 参考素材只接受平台 file_id，服务端解析成短期签名 URL 后交给上游；
//   - 视频计费尚未启用，任务按 0 积分创建；
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

	// 7. 视频计费尚未启用：任务按 0 积分创建，不查计费规则、不扣费。
	common.SysLogf("[VideoGenerate] 视频计费尚未启用，本次任务按 0 积分创建: userID=%d, model=%s",
		userID, modelName)

	// 8. 幂等键。语义与图像生成一致：窗口内重复提交复用已有任务。
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

	// 9. 解析参考文件：校验归属/用途/状态/有效期/媒体类型，并生成短期签名 URL。
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

	// 10. 组装供应商适配层所需的平台内部请求（这里装的是签名 URL）。
	internalReq := supplier.VideoGenerateRequest{
		Model:         modelName,
		Prompt:        req.Prompt,
		Images:        refs.Images,
		Videos:        refs.Videos,
		Audios:        refs.Audios,
		Resolution:    req.Resolution,
		Duration:      req.Duration,
		Ratio:         req.Ratio,
		VendorModelID: vendorModel.VendorModelID,
	}
	if err := internalReq.Validate(); err != nil {
		common.SysErrorf("[VideoGenerate] 内部请求校验失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "服务暂时不可用，请稍后再试"})
		return
	}

	// 11. 创建任务。没有扣费，因此不需要事务；幂等键冲突的处理与图像生成一致。
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
		SubmitDeadline: &submitDeadline,
		DeadlineAt:     &deadlineAt,
		Credits:        0, // 计费尚未启用
		RequestBody:    marshalRequestBodyFor("VideoGenerate", req),
		IdempotencyKey: idemKey,
	}

	if err := model.CreateTask(&task); err != nil {
		if idemKey != nil && model.IsDuplicateKeyError(err) {
			if existing, lookupErr := model.GetTaskByUserAndIdempotencyKey(userID, *idemKey); lookupErr == nil {
				common.SysLogf("[VideoGenerate] 幂等键并发冲突，复用已有任务: userID=%d, taskID=%s", userID, existing.TaskID)
				c.JSON(http.StatusOK, gin.H{"code": "success", "taskId": existing.TaskID})
				return
			}
		}

		common.SysErrorf("[VideoGenerate] 任务创建失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "任务创建失败"})
		return
	}

	// 12. 构造供应商客户端
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

	// 13. 上游明确拒绝受理（4xx、业务失败码、请求没发出去）→ 转终态
	if result.Code != "success" && result.FailureKind != supplier.FailureUnknown {
		common.SysErrorf("[VideoGenerate] 供应商明确拒绝: vendor=%s, model=%s, kind=%s",
			vendor.Name, modelName, result.FailureKind)
		if err := model.FailTask(task.TaskID, "vendor_rejected", "上游拒绝受理", `{"error":"vendor rejected"}`); err != nil {
			common.SysErrorf("[VideoGenerate] 更新任务状态失败: taskID=%s, err=%v", task.TaskID, err)
		}
		c.JSON(http.StatusOK, gin.H{"code": "fail", "message": "供应商调用失败"})
		return
	}

	// 14. 结果不确定（超时、连接中断、响应读不完整）。
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

	// 15. 提交成功：记录供应商响应并交给对账循环轮询。
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

	// 16. 立即安排一次轮询；抢不到并发位也无妨，下一轮对账循环会接手。
	ScheduleTaskPoll(task.TaskID)

	// 17. 返回系统 taskId
	c.JSON(http.StatusOK, gin.H{
		"code":   "success",
		"taskId": task.TaskID,
	})
}
