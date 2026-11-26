package supplier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
	"strings"

	"github.com/FutureAI-X/futureai-api/common"
)

// apimart APIMart 供应商实现
type apimart struct {
	cfg Config
}

// newAPIMart 创建 APIMart 供应商实例。
// 刻意不持有自己的 http.Client：出站客户端是进程级共享的，
// 每实例一个客户端会让连接池退化成「每次请求一条新连接」（见 common.OutboundHTTPClient）。
func newAPIMart(cfg Config) *apimart {
	return &apimart{cfg: cfg}
}

// apimartResponse APIMart API 原始响应结构
type apimartResponse struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

// apimartTaskItem APIMart 任务数据项
type apimartTaskItem struct {
	Status string `json:"status"`
	TaskID string `json:"task_id"`
}

// apimartImageModelSpec 单个平台模型在 APIMart 侧的规格。
//
// size 规则、分辨率档位、参考图上限和 version 都是「模型」的属性，而不是
// 「供应商」的属性：同一个 APIMart 账号下不同模型的上游限制并不相同。
// 因此这些约束按平台模型名登记在 apimartImageModels，供应商适配层只负责
// 把查到的规格翻译成上游请求。
type apimartImageModelSpec struct {
	// version 上游外观风格版本；空串表示请求体不写该字段。
	version string
	// sizeRule 平台开放给该模型的 size 格式与取值约束。
	sizeRule SizeRule
	// resolutions 平台开放给该模型的分辨率档位（平台统一大写）。
	resolutions []string
	// lowercaseResolution 为 true 时，发给上游的 resolution 转成小写。
	// gpt-image-2 / gpt-image-2.6-ext 要求 1k / 2k / 4k，gpt-image-2.5-ext 要求 1K / 2K / 4K。
	lowercaseResolution bool
	// maxReferenceImages 上游单次请求支持的最大参考图张数。
	maxReferenceImages int
	// qualityLevels / outputFormats / complexityLevels 为该模型开放的可选参数取值。
	// nil 表示模型不支持该参数：收到非空值直接拒绝，避免把上游会忽略的字段
	// 当成已生效的配置放行。
	qualityLevels    []string
	outputFormats    []string
	complexityLevels []string

	// watermark 非 nil 时固定写入请求体的 watermark 字段。
	// 用于把模型强制为无水印/有水印；平台没有对应的公开字段，调用方无法覆盖。
	watermark *bool
}

// apimartImageSizes15 gpt-image-2 / gpt-image-2-ext 支持的画幅比例。
//
// 是否开放上游的 auto 由该模型的 models.param_spec 决定：规格的 ratios
// 里写了 auto 就放行，没写就由控制器在扣费前拒绝。这里的列表只约束比例取值。
var apimartImageSizes15 = []string{
	"1:1", "3:2", "2:3", "4:3", "3:4", "5:4", "4:5",
	"16:9", "9:16", "2:1", "1:2", "3:1", "1:3", "21:9", "9:21",
}

// apimartImageSizes10 gpt-image-2.5-ext 支持的画幅比例。
var apimartImageSizes10 = []string{
	"1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3", "5:4", "4:5", "21:9",
}

// apimartImageResolutions 平台对外统一的分辨率档位。
// 发往上游时由模型的 lowercaseResolution 决定是否转小写。
var apimartImageResolutions = []string{"1K", "2K", "4K"}

// apimartSeedreamSizes Seedream 5.5 支持的画幅比例。
// 上游还接受「1K / 1.5K / 2K」档位、auto 和精确像素写法：
// 档位写法和像素写法不开放（输出尺寸不可预期、计费无法在扣费前准确计算）；
// auto 是否开放由该模型的 models.param_spec 决定。
var apimartSeedreamSizes = []string{
	"1:1", "4:3", "3:4", "16:9", "9:16", "3:2", "2:3", "21:9", "2:1", "1:2",
}

// apimartSeedreamResolutions Seedream 5.5 支持的分辨率档位（不支持 4K）。
// 上游要求大写，因此 lowercaseResolution 保持为 false。
var apimartSeedreamResolutions = []string{"1K", "1.5K", "2K"}

// apimartWatermarkOff 固定无水印，供模型规格引用。
var apimartWatermarkOff = false

// gpt-image-2.6-ext 开放的可选参数取值。
// quality 不影响单张价格；output_format 本期只开放静态图片格式（SVG/GIF 暂不接入）；
// complexity 省略时由上游决定。
var (
	apimartImage26QualityLevels    = []string{"auto", "low", "medium", "high", "xhigh", "max"}
	apimartImage26OutputFormats    = []string{"png", "jpeg", "webp"}
	apimartImage26ComplexityLevels = []string{"low", "medium", "high"}
)

// apimartPixelRule 接受像素尺寸的 APIMart 模型的约束。
//
// 上游要求：宽高都是 16 的倍数；宽高比在 1:3 到 3:1 之间；
// 任意一边不超过 3840；总像素不超过 3840×2160。
// 超过 2560×1440 属于上游的实验性支持，平台不拒绝，只在文档中标注。
var apimartPixelRule = PixelRule{
	Step:            16,
	MaxSide:         3840,
	MaxPixels:       3840 * 2160,
	MinAspectWidth:  1,
	MinAspectHeight: 3,
	MaxAspectWidth:  3,
	MaxAspectHeight: 1,
}

// apimartImageModels 平台模型名 → APIMart 侧规格。
//
// 新增映射到 APIMart 的平台模型时必须在这里登记：未登记的模型会在
// ValidateImageRequest 阶段被拒绝（fail-closed），避免漏配后用错误的限制放行，
// 或把请求发到上游才失败。
//
// gpt-image-2 与 gpt-image-2-ext 是上游同一个模型的两个名字，平台侧两种
// 配置都允许，因此都登记。两者参数完全一致：15 种比例或像素尺寸、
// 15 张参考图、resolution 小写。gpt-image-2.5 / 2.6 系列只接受比例。
var apimartImageModels = map[string]apimartImageModelSpec{
	"gpt-image-2": {
		sizeRule: SizeRule{
			AllowRatio:  true,
			AllowPixels: true,
			Ratios:      apimartImageSizes15,
			Pixels:      &apimartPixelRule,
		},
		resolutions:         apimartImageResolutions,
		lowercaseResolution: true,
		maxReferenceImages:  15,
	},
	"gpt-image-2-ext": {
		sizeRule: SizeRule{
			AllowRatio:  true,
			AllowPixels: true,
			Ratios:      apimartImageSizes15,
			Pixels:      &apimartPixelRule,
		},
		resolutions:         apimartImageResolutions,
		lowercaseResolution: true,
		maxReferenceImages:  15,
	},
	"gpt-image-2.5-flare-ext": {
		version: "flare",
		sizeRule: SizeRule{
			AllowRatio: true,
			Ratios:     apimartImageSizes10,
		},
		resolutions:        apimartImageResolutions,
		maxReferenceImages: 16,
	},
	"gpt-image-2.5-sunburst-ext": {
		version: "sunburst",
		sizeRule: SizeRule{
			AllowRatio: true,
			Ratios:     apimartImageSizes10,
		},
		resolutions:        apimartImageResolutions,
		maxReferenceImages: 16,
	},
	"gpt-image-2.6-flare-ext": {
		version: "flare",
		sizeRule: SizeRule{
			AllowRatio: true,
			Ratios:     apimartImageSizes10,
		},
		resolutions:         apimartImageResolutions,
		lowercaseResolution: true,
		maxReferenceImages:  16,
		qualityLevels:       apimartImage26QualityLevels,
		outputFormats:       apimartImage26OutputFormats,
		complexityLevels:    apimartImage26ComplexityLevels,
	},
	"gpt-image-2.6-sunburst-ext": {
		version: "sunburst",
		sizeRule: SizeRule{
			AllowRatio: true,
			Ratios:     apimartImageSizes10,
		},
		resolutions:         apimartImageResolutions,
		lowercaseResolution: true,
		maxReferenceImages:  16,
		qualityLevels:       apimartImage26QualityLevels,
		outputFormats:       apimartImage26OutputFormats,
		complexityLevels:    apimartImage26ComplexityLevels,
	},

	// Seedream 5.5：只开放比例尺寸；最多 10 张参考图；固定无水印。
	// background / output_format / optimize_prompt_options 等字段一律不传，
	// 由上游使用默认值；layer_decomposition（图层拆分）不在平台开放。
	"seedream-5-5": {
		sizeRule: SizeRule{
			AllowRatio: true,
			Ratios:     apimartSeedreamSizes,
		},
		resolutions:        apimartSeedreamResolutions,
		maxReferenceImages: 10,
		watermark:          &apimartWatermarkOff,
	},
}

// apimartImageSpecFor 查找平台模型的 APIMart 规格。
func apimartImageSpecFor(modelName string) (apimartImageModelSpec, bool) {
	spec, ok := apimartImageModels[strings.TrimSpace(modelName)]
	return spec, ok
}

// supportsResolution 判断模型规格是否开放该分辨率档位。
// 平台标准值为大写，但上游不同模型要求的大小写不同，这里按大小写不敏感匹配。
func (s apimartImageModelSpec) supportsResolution(raw string) bool {
	raw = strings.TrimSpace(raw)
	for _, allowed := range s.resolutions {
		if strings.EqualFold(raw, allowed) {
			return true
		}
	}
	return false
}

// formatResolution 把平台统一的档位转换成上游要求的大小写。
// gpt-image-2 要 1k / 2k / 4k，gpt-image-2.5-ext 要 1K / 2K / 4K。
func (s apimartImageModelSpec) formatResolution(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if s.lowercaseResolution {
		return strings.ToLower(raw)
	}
	return strings.ToUpper(raw)
}

// applyImageVersion 按平台模型的规格补齐请求体里的 version 字段。
// 未登记的模型不写 version（校验阶段会拒绝该请求）。
func applyImageVersion(body map[string]interface{}, requestedModel string) {
	spec, ok := apimartImageSpecFor(requestedModel)
	if !ok {
		common.SysErrorf("[APIMart] 模型 %s 未配置规格，无法补全 version", requestedModel)
		return
	}
	if spec.version == "" {
		return
	}

	body["version"] = spec.version

	common.SysLogf("[APIMart] 根据模型 %s 补全 version=%s", requestedModel, spec.version)
}

// ValidateImageRequest 在扣费前校验平台模型在 APIMart 侧的请求约束。
func (a *apimart) ValidateImageRequest(req ImageGenerateRequest) error {
	if strings.TrimSpace(req.Prompt) == "" {
		return fmt.Errorf("prompt 不能为空")
	}

	spec, ok := apimartImageSpecFor(req.Model)
	if !ok {
		return fmt.Errorf("模型 %s 未配置 APIMart 规格，暂不可用", strings.TrimSpace(req.Model))
	}

	if err := spec.sizeRule.Validate(req.Size); err != nil {
		return err
	}
	if !spec.supportsResolution(req.Resolution) {
		return fmt.Errorf("resolution 只支持 %s", strings.Join(spec.resolutions, "、"))
	}
	if err := validateEnumOption("quality", req.Quality, spec.qualityLevels); err != nil {
		return err
	}
	if err := validateEnumOption("output_format", req.OutputFormat, spec.outputFormats); err != nil {
		return err
	}
	if err := validateEnumOption("complexity", req.Complexity, spec.complexityLevels); err != nil {
		return err
	}

	return validateAPIMartReferenceCount(req, spec.maxReferenceImages)
}

// validateEnumOption 校验可选枚举字段。
// 空值表示调用方未指定，始终合法；模型规格未登记该参数时拒绝非空值，
// 与 size/resolution 一样保持 fail-closed。
func validateEnumOption(name, value string, allowed []string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if len(allowed) == 0 {
		return fmt.Errorf("模型不支持 %s 参数", name)
	}
	for _, item := range allowed {
		if strings.EqualFold(item, value) {
			return nil
		}
	}
	return fmt.Errorf("%s 只支持 %s", name, strings.Join(allowed, "、"))
}

// validateAPIMartReferenceCount 校验实际生效的参考图数组不超过该模型的上限。
// 与控制器一致：image_ids 非空时只认它，否则看 image_urls。
func validateAPIMartReferenceCount(req ImageGenerateRequest, max int) error {
	if len(req.ImageIDs) > 0 {
		seen := make(map[string]struct{}, len(req.ImageIDs))
		for _, id := range req.ImageIDs {
			if id = strings.TrimSpace(id); id != "" {
				seen[id] = struct{}{}
			}
		}
		if len(seen) > max {
			return fmt.Errorf("image_ids 去重后最多 %d 张", max)
		}
		return nil
	}

	seen := make(map[string]struct{}, len(req.ImageURLs))
	for _, u := range req.ImageURLs {
		if u = strings.TrimSpace(u); u != "" {
			seen[u] = struct{}{}
		}
	}
	if len(seen) > max {
		return fmt.Errorf("image_urls 去重后最多 %d 张", max)
	}
	return nil
}

// buildRequest 把平台标准字段翻译成 APIMart 的线上请求体。
//
// size / resolution 直接写入：默认值已在计费前由 ApplyDefaults 补齐，
// 传空串上游会当作未指定，反而绕开了默认值。resolution 的大小写由模型规格
// 决定（gpt-image-2 / 2.6-ext 小写、gpt-image-2.5-ext 大写），未登记模型按平台默认大写
// 兜底（这类请求在 ValidateImageRequest 阶段已被拒绝）。
// image_urls / quality / output_format / complexity 为空时不写入。
// 平台一次任务只生成一张图，因此 n 固定为 1。
func buildRequest(req ImageGenerateRequest) map[string]interface{} {
	spec, _ := apimartImageSpecFor(req.Model)

	body := map[string]interface{}{
		"model":      req.VendorModelID,
		"prompt":     req.Prompt,
		"size":       strings.TrimSpace(req.Size),
		"resolution": spec.formatResolution(req.Resolution),
		"n":          1,
	}
	if len(req.ImageURLs) > 0 {
		body["image_urls"] = req.ImageURLs
	}

	// 上游枚举大小写不敏感，这里统一转小写，保证同一取值只产生一种线上写法。
	for key, value := range map[string]string{
		"quality":       req.Quality,
		"output_format": req.OutputFormat,
		"complexity":    req.Complexity,
	} {
		if v := strings.ToLower(strings.TrimSpace(value)); v != "" {
			body[key] = v
		}
	}

	// 模型级固定字段。watermark 目前只有 Seedream 声明，显式下发 false，
	// 不依赖上游默认值。
	if spec.watermark != nil {
		body["watermark"] = *spec.watermark
	}

	applyImageVersion(body, req.Model)

	return body
}

// ImageGenerate 调用 APIMart 图像生成 API
func (a *apimart) ImageGenerate(ctx context.Context, req ImageGenerateRequest) ImageGenerateResponse {
	// 本函数内所有失败都必须明确标注性质：
	// 请求根本没发出去 → rejected；发出去了但结果读不到 → unknown。
	// 两者的差别决定了控制器是否退款，判错就是资损。
	failRejected := ImageGenerateResponse{Code: "fail", FailureKind: FailureRejected}
	failUnknown := ImageGenerateResponse{Code: "fail", FailureKind: FailureUnknown}

	bodyBytes, err := json.Marshal(buildRequest(req))
	if err != nil {
		// 本地序列化失败，请求尚未发出
		common.SysErrorf("[APIMart] 请求体序列化失败: %v", err)
		return failRejected
	}

	// 构建 HTTP 请求
	url := fmt.Sprintf("%s/v1/images/generations", a.cfg.BaseURL)
	common.SysLogf("[APIMart] 发起图像生成请求: POST %s", url)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		common.SysErrorf("[APIMart] 构建HTTP请求失败: %v", err)
		return failRejected
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.cfg.APIKey))
	// 刻意不传 Idempotency-Key 与 X-APIMart-Response-Version：
	// 不带响应版本头时上游固定返回 200 + data[].task_id；
	// 平台自身已按 Idempotency-Key 做任务级去重，不需要上游再提供一层。

	// 发送请求
	resp, err := common.OutboundHTTPClient().Do(httpReq)
	if err != nil {
		// 超时或连接中断：请求可能已经送达并被受理，绝不能当成「没接单」
		common.SysErrorf("[APIMart] 请求发送失败(结果不确定，不得退款): %v", err)
		return failUnknown
	}
	defer resp.Body.Close()

	common.SysLogf("[APIMart] 收到响应: HTTP %d", resp.StatusCode)

	// 读取响应
	// 限制读取体积：超时只限时长，恶意或被控的上游可流式输出打爆内存
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, common.MaxOutboundBodySize))
	if err != nil {
		common.SysErrorf("[APIMart] 读取响应体失败(结果不确定，不得退款): %v", err)
		return failUnknown
	}

	// 不传响应版本头时，成功固定返回 HTTP 200。
	// 4xx 是上游明确拒绝，可以退款；其余（5xx、意外的 202 等）可能来自上游
	// 前面的网关或协议变动，请求或许已经落到上游并建了任务，只能算不确定。
	if resp.StatusCode != http.StatusOK {
		common.SysErrorf("[APIMart] HTTP状态码异常: %d, 响应: %s", resp.StatusCode, truncate(string(respBody), 500))
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return failRejected
		}
		return failUnknown
	}

	// 解析响应。
	// 解析失败的一种常见成因是响应体超过 MaxOutboundBodySize 被 LimitReader 截断，
	// 此时上游其实已经成功建单，只是我们读不完整 → 只能算不确定。
	var apiResp apimartResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		common.SysErrorf("[APIMart] 响应JSON解析失败(结果不确定，不得退款): %v, 原始响应: %s", err, truncate(string(respBody), 500))
		return failUnknown
	}

	// code 非 200：拿到了结构完整的业务响应且上游自述失败，可以退款。
	if apiResp.Code != http.StatusOK {
		common.SysErrorf("[APIMart] 业务code异常: %d, 响应: %s", apiResp.Code, truncate(string(respBody), 500))
		return failRejected
	}

	// 走到这里上游已经返回成功，只是任务 ID 取不到——任务很可能已经建了，
	// 不能退款，否则就是「平台付钱、用户免费」。
	taskID, err := apimartTaskID(apiResp.Data)
	if err != nil {
		common.SysErrorf("[APIMart] 响应中缺少任务ID(结果不确定，不得退款): %v, data: %s", err, truncate(string(apiResp.Data), 500))
		return failUnknown
	}

	common.SysLogf("[APIMart] 图像生成成功, taskId: %s", taskID)

	return ImageGenerateResponse{
		Code: "success",
		Data: map[string]interface{}{
			"taskId": taskID,
		},
	}
}

// apimartTaskID 从响应 data 中提取任务 ID。
// 不传响应版本头时 data 固定是数组，元素含 task_id。
func apimartTaskID(data json.RawMessage) (string, error) {
	var tasks []apimartTaskItem
	if err := json.Unmarshal(data, &tasks); err != nil {
		return "", fmt.Errorf("data 不是任务数组: %w", err)
	}
	for _, task := range tasks {
		if id := strings.TrimSpace(task.TaskID); id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("data 中缺少任务 ID")
}

// ── APIMart 视频生成（MAGI-2）──

// apimartVideoModelSpec 单个平台视频模型在 APIMart 侧的固定参数。
//
// 分辨率、声音、审核、种子与参考模式全部由代码显式写入请求体，不依赖上游默认值：
// 上游调整默认行为时，平台的输出规格与计费口径都不受影响。
// 规格按供应商模型 ID 登记：平台模型名是管理端自由配置的对外别名，
// 真正决定上游行为的是供应商模型 ID。未登记的供应商模型在提交时拒绝（fail-closed）。
type apimartVideoModelSpec struct {
	// upstreamResolution 上游要求的固定输出分辨率写法。MAGI-2 文档使用小写 1080p。
	upstreamResolution string
	// platformResolution 平台侧唯一允许的分辨率档位。
	// 校验它是为了防止管理端规格配错后「按 720P 计费、实际出 1080p」的错配。
	platformResolution string
	// generateAudio / nsfwCheck 是写死值，不开放给调用方。
	generateAudio bool
	nsfwCheck     bool
	// referenceMode 为 true 时，带参考素材的请求固定按参考模式提交。
	referenceMode bool
}

// apimartVideoModels 供应商模型 ID → APIMart 视频规格。
//
// 平台模型名可以自由配置（例如「MAGI-2」「MAGI-2 参考生视频」），
// 只要管理端把供应商模型 ID 映射到 `magi-2`，就会命中这份规格。
//
// MAGI-2 只开放参考生视频：图片一律是参考图，不做首帧/尾帧；
// 视频与音频参考也按同一模式提交。
var apimartVideoModels = map[string]apimartVideoModelSpec{
	"magi-2": {
		upstreamResolution: "1080p",
		platformResolution: "1080P",
		generateAudio:      true,
		nsfwCheck:          false,
		referenceMode:      true,
	},
}

// apimartVideoSpecFor 按供应商模型 ID 查找 APIMart 视频规格。
func apimartVideoSpecFor(vendorModelID string) (apimartVideoModelSpec, bool) {
	spec, ok := apimartVideoModels[strings.TrimSpace(vendorModelID)]
	return spec, ok
}

// buildAPIMartVideoRequest 把平台字段翻译成 MAGI-2 的线上请求体。
//
// 所有带默认值的上游字段都显式写入（resolution / generate_audio / nsfw_check /
// seed / omni_reference_task_type），避免上游改默认值后平台行为漂移。
// 参考素材数组为空时不写入：它表示「没有该输入」，不是一个有默认值的参数。
func buildAPIMartVideoRequest(req VideoGenerateRequest) (map[string]interface{}, error) {
	spec, ok := apimartVideoSpecFor(req.VendorModelID)
	if !ok {
		return nil, fmt.Errorf("供应商模型 %s 未配置 APIMart 视频规格", strings.TrimSpace(req.VendorModelID))
	}
	if !strings.EqualFold(strings.TrimSpace(req.Resolution), spec.platformResolution) {
		return nil, fmt.Errorf("resolution 只支持 %s", spec.platformResolution)
	}

	ratio := strings.TrimSpace(req.Ratio)
	// 参考生视频没有可跟随的首帧，adaptive 在上游没有确定语义，平台不开放。
	if ratio == "" || strings.EqualFold(ratio, "adaptive") {
		return nil, fmt.Errorf("ratio 必须是固定比例，不支持 adaptive")
	}

	body := map[string]interface{}{
		"model":                    req.VendorModelID,
		"prompt":                   req.Prompt,
		"duration":                 req.Duration,
		"resolution":               spec.upstreamResolution,
		"aspect_ratio":             ratio,
		"generate_audio":           spec.generateAudio,
		"nsfw_check":               spec.nsfwCheck,
		"seed":                     -1,
		"omni_reference_task_type": "auto",
	}

	// 有任意参考素材时固定参考模式：只有图片时，这是让上游把图片当参考图
	// （而不是首帧）的必要条件；带视频/音频时上游本来也会进入参考模式。
	if spec.referenceMode && (len(req.ImageURLs) > 0 || len(req.VideoURLs) > 0 || len(req.AudioURLs) > 0) {
		body["omni_reference_task_type"] = "reference"
	}

	if len(req.ImageURLs) > 0 {
		body["image_urls"] = req.ImageURLs
	}
	if len(req.VideoURLs) > 0 {
		body["video_urls"] = req.VideoURLs
	}
	if len(req.AudioURLs) > 0 {
		body["audio_urls"] = req.AudioURLs
	}

	return body, nil
}

// VideoGenerate 调用 APIMart 视频生成 API（MAGI-2）。
//
// 失败性质与图像提交一致：4xx、业务 code 非 200、请求未发出 → rejected（可退款）；
// 超时、连接中断、响应读不完整或解析失败、缺少任务 ID → unknown（不得退款）。
func (a *apimart) VideoGenerate(ctx context.Context, req VideoGenerateRequest) VideoGenerateResponse {
	failRejected := VideoGenerateResponse{Code: "fail", FailureKind: FailureRejected}
	failUnknown := VideoGenerateResponse{Code: "fail", FailureKind: FailureUnknown}

	body, err := buildAPIMartVideoRequest(req)
	if err != nil {
		// 参数不合法，请求尚未发出
		common.SysErrorf("[APIMart] 视频请求参数无效: %v", err)
		return failRejected
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		// 本地序列化失败，请求尚未发出
		common.SysErrorf("[APIMart] 视频请求体序列化失败: %v", err)
		return failRejected
	}

	url := fmt.Sprintf("%s/v1/videos/generations", a.cfg.BaseURL)
	common.SysLogf("[APIMart] 发起视频生成请求: POST %s", url)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		common.SysErrorf("[APIMart] 构建视频请求失败: %v", err)
		return failRejected
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.cfg.APIKey))
	// 与图像提交一致：不传 Idempotency-Key 与 X-APIMart-Response-Version，
	// 保持 HTTP 200 + data[].task_id 的返回格式。

	resp, err := common.OutboundHTTPClient().Do(httpReq)
	if err != nil {
		// 超时或连接中断：请求可能已经送达并被受理，绝不能当成「没接单」
		common.SysErrorf("[APIMart] 视频请求发送失败(结果不确定，不得退款): %v", err)
		return failUnknown
	}
	defer resp.Body.Close()

	common.SysLogf("[APIMart] 视频提交响应: HTTP %d", resp.StatusCode)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, common.MaxOutboundBodySize))
	if err != nil {
		common.SysErrorf("[APIMart] 读取视频响应体失败(结果不确定，不得退款): %v", err)
		return failUnknown
	}

	// 不传响应版本头时，成功固定返回 HTTP 200。4xx 是上游明确拒绝；
	// 5xx、意外的 202 等可能来自上游前面的网关，请求或许已经建了任务 → 不确定。
	if resp.StatusCode != http.StatusOK {
		common.SysErrorf("[APIMart] 视频提交HTTP状态异常: %d, 响应: %s", resp.StatusCode, truncate(string(respBody), 500))
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return failRejected
		}
		return failUnknown
	}

	var apiResp apimartResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		common.SysErrorf("[APIMart] 视频响应JSON解析失败(结果不确定，不得退款): %v, 原始响应: %s", err, truncate(string(respBody), 500))
		return failUnknown
	}

	if apiResp.Code != http.StatusOK {
		common.SysErrorf("[APIMart] 视频业务code异常: %d, 响应: %s", apiResp.Code, truncate(string(respBody), 500))
		return failRejected
	}

	taskID, err := apimartTaskID(apiResp.Data)
	if err != nil {
		common.SysErrorf("[APIMart] 视频响应中缺少任务ID(结果不确定，不得退款): %v, data: %s", err, truncate(string(apiResp.Data), 500))
		return failUnknown
	}

	common.SysLogf("[APIMart] 视频生成提交成功, taskId: %s", taskID)

	return VideoGenerateResponse{
		Code: "success",
		Data: map[string]interface{}{
			"taskId": taskID,
			// 对账循环按它判断结果类型：视频不转存，也不登记结果文件。
			"kind": ResultKindVideo,
		},
	}
}

// ── APIMart 任务查询 ──

// apimartTaskQueryResponse APIMart 任务查询响应结构
type apimartTaskQueryResponse struct {
	Code int                  `json:"code"`
	Data apimartTaskQueryData `json:"data"`
}

type apimartTaskQueryData struct {
	ID       string            `json:"id"`
	Status   string            `json:"status"`
	Progress int               `json:"progress"`
	Result   apimartTaskResult `json:"result"`
	Error    *apimartTaskError `json:"error"`
}

type apimartTaskResult struct {
	Images []apimartTaskImage `json:"images"`
	Videos []apimartTaskVideo `json:"videos"`
}

type apimartTaskImage struct {
	URL     apimartURLList `json:"url"`
	B64JSON string         `json:"b64_json"`
}

type apimartTaskVideo struct {
	URL apimartURLList `json:"url"`
}

// apimartURLList 兼容上游 URL 字段的两种形态：字符串与字符串数组。
// 图像接口返回数组；视频接口文档示例没有写死类型，两种都要能解析，
// 否则一次格式差异就会把「任务已完成」误判成「查询失败」。
type apimartURLList []string

func (l *apimartURLList) UnmarshalJSON(data []byte) error {
	if string(bytes.TrimSpace(data)) == "null" {
		*l = nil
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*l = apimartURLList{single}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	*l = apimartURLList(list)
	return nil
}

// apimartTaskError 失败任务的上游错误。reason 是机器可读的分类，
// message 是上游原文；平台只把 reason 映射成用户文案，原文仅用于日志。
type apimartTaskError struct {
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

// apimartFailureReasons APIMart 失败原因 → 平台统一失败原因。
// 未列出的原因（包括空串）返回空串，由控制器回退到默认文案。
var apimartFailureReasons = map[string]string{
	"content_policy_violation": FailureReasonContentPolicy,
	"invalid_parameter":        FailureReasonInvalidParameter,
	"unsupported_option":       FailureReasonInvalidParameter,
	"invalid_media":            FailureReasonInvalidMedia,
	"media_unreachable":        FailureReasonMediaUnreachable,
	"generation_timeout":       FailureReasonGenerationTimeout,
	"service_unavailable":      FailureReasonServiceUnavailable,
	"delivery_failed":          FailureReasonUpstreamFailed,
	"internal_error":           FailureReasonUpstreamFailed,
	"unknown_error":            FailureReasonUpstreamFailed,
}

// apimartFailureReason 归一化上游失败原因。
func apimartFailureReason(raw string) string {
	return apimartFailureReasons[strings.ToLower(strings.TrimSpace(raw))]
}

// apimartTaskIDPattern 供应商任务 ID 的合法形态。
//
// taskID 来自上游响应且会被直接拼进查询 URL，必须限制字符集：
// 否则一个被控的上游（或将来某次响应异常）返回 "../other" 之类的内容，
// 就会带着供应商 API Key 去请求该主机上的任意路径。
var apimartTaskIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// TaskQuery 查询 APIMart 任务状态
// vendorResponse 为提交任务时供应商返回的原始 JSON，APIMart 从中提取 taskId
//
// 图像与视频共用本方法：视频提交会在 vendor_response 中写 kind=video，
// 这里据此决定解析 videos 还是 images，并把 Kind 一路带到控制器。
func (a *apimart) TaskQuery(ctx context.Context, vendorResponse string) TaskQueryResponse {
	// 从 vendorResponse 中提取 taskId
	var respData map[string]interface{}
	if err := json.Unmarshal([]byte(vendorResponse), &respData); err != nil {
		common.SysErrorf("[APIMart] vendorResponse 解析失败: %v", err)
		return TaskQueryResponse{Status: "call_fail"}
	}

	// 旧图像任务的 vendor_response 没有 kind，保持空串（控制器按 image 处理）。
	kind, _ := respData["kind"].(string)
	responseKind := ""
	if kind == ResultKindVideo {
		responseKind = ResultKindVideo
	}

	taskID, _ := respData["taskId"].(string)
	if taskID == "" {
		common.SysErrorf("[APIMart] vendorResponse 中缺少 taskId")
		return TaskQueryResponse{Status: "call_fail", Kind: responseKind}
	}
	if !apimartTaskIDPattern.MatchString(taskID) {
		common.SysErrorf("[APIMart] taskId 形态非法，拒绝拼接查询 URL: %q", truncate(taskID, 64))
		return TaskQueryResponse{Status: "call_fail", Kind: responseKind}
	}

	// 查询失败一律是 call_fail（= 状态未知），控制器不会据此退款，
	// 因此这里所有错误路径共用同一个返回值。
	failResp := TaskQueryResponse{TaskID: taskID, Status: "call_fail", Kind: responseKind}

	// 构建请求
	url := fmt.Sprintf("%s/v1/tasks/%s?language=zh", a.cfg.BaseURL, taskID)
	common.SysLogf("[APIMart] 查询任务状态: GET %s", url)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		common.SysErrorf("[APIMart] 构建查询请求失败: %v", err)
		return failResp
	}
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.cfg.APIKey))

	// 发送请求
	resp, err := common.OutboundHTTPClient().Do(httpReq)
	if err != nil {
		common.SysErrorf("[APIMart] 查询请求发送失败: %v", err)
		return failResp
	}
	defer resp.Body.Close()

	common.SysLogf("[APIMart] 查询响应: HTTP %d", resp.StatusCode)

	// 读取响应
	// 限制读取体积：Timeout 只限时长，恶意上游可流式输出打爆内存
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, common.MaxOutboundBodySize))
	if err != nil {
		common.SysErrorf("[APIMart] 读取查询响应失败: %v", err)
		return failResp
	}

	// HTTP 状态码非 200
	if resp.StatusCode != http.StatusOK {
		common.SysErrorf("[APIMart] 查询HTTP状态异常: %d, 响应: %s", resp.StatusCode, truncate(string(respBody), 500))
		return failResp
	}

	// 解析响应
	var apiResp apimartTaskQueryResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		common.SysErrorf("[APIMart] 查询响应JSON解析失败: %v, 响应: %s", err, truncate(string(respBody), 500))
		return failResp
	}

	if apiResp.Code != 200 {
		common.SysErrorf("[APIMart] 查询业务code异常: %d, 响应: %s", apiResp.Code, truncate(string(respBody), 500))
		return failResp
	}

	taskData := apiResp.Data
	common.SysLogf("[APIMart] 任务状态: %s (progress=%d)", taskData.Status, taskData.Progress)

	// 终态：completed
	if taskData.Status == "completed" {
		result := TaskQueryResponse{
			TaskID: taskID,
			Status: "completed",
			Kind:   responseKind,
		}

		// 视频结果只取 data.result.videos[0].url；Kind=video 让控制器跳过转存。
		if responseKind == ResultKindVideo {
			result.Data = map[string]interface{}{"url": ""}
			if len(taskData.Result.Videos) > 0 {
				for _, u := range taskData.Result.Videos[0].URL {
					if u = strings.TrimSpace(u); u != "" {
						result.Data["url"] = u
						break
					}
				}
			}
			return result
		}

		result.Data = map[string]interface{}{"url": "", "b64_json": ""}
		if len(taskData.Result.Images) > 0 {
			img := taskData.Result.Images[0]
			if len(img.URL) > 0 {
				result.Data["url"] = img.URL[0]
			}
			result.Data["b64_json"] = img.B64JSON
		}
		return result
	}

	// 终态：failed / cancelled
	if taskData.Status == "failed" || taskData.Status == "cancelled" {
		result := TaskQueryResponse{TaskID: taskID, Status: taskData.Status, Kind: responseKind}
		if taskData.Error != nil {
			result.FailureReason = apimartFailureReason(taskData.Error.Reason)
			// 原始错误只放进 Data，由控制器落到 query_response 供排查；
			// 不直接透传给调用方，避免把上游英文原文当成用户文案。
			result.Data = map[string]interface{}{
				"error":  taskData.Error.Message,
				"reason": taskData.Error.Reason,
			}
		}
		return result
	}

	// 中间态：pending / processing
	return TaskQueryResponse{
		TaskID:   taskID,
		Status:   taskData.Status,
		Kind:     responseKind,
		Progress: taskData.Progress,
	}
}

// truncate 截断字符串到指定长度
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ── APIMart 图片上传 ──

// apimartUploader APIMart 图片上传实现
type apimartUploader struct {
	cfg Config
}

// newAPIMartUploader 创建 APIMart 上传实例（同样复用进程级共享的出站客户端）
func newAPIMartUploader(cfg Config) *apimartUploader {
	return &apimartUploader{cfg: cfg}
}

// apimartUploadResponse APIMart 上传响应体
type apimartUploadResponse struct {
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Bytes       int64  `json:"bytes"`
	CreatedAt   int64  `json:"created_at"`
}

// UploadImage 上传图片到 APIMart
// APIMart 返回 HTTP 200 视为成功，其余视为失败
func (u *apimartUploader) UploadImage(ctx context.Context, filename string, contentType string, data []byte) (*UploadResult, error) {
	url := fmt.Sprintf("%s/v1/uploads/images", u.cfg.BaseURL)

	// 构造 multipart/form-data。
	// 使用 mime.FormatMediaType 而非手工拼接：它会按 RFC 2231 对
	// 特殊字符（含 CR/LF）做百分号编码，避免文件名注入额外的请求头。
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name":     "file",
		"filename": filename,
	}))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	writer.Close()

	common.SysLogf("[APIMart] 上传图片: POST %s, type=%s, size=%d", url, contentType, len(data))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", u.cfg.APIKey))

	resp, err := common.OutboundHTTPClient().Do(httpReq)
	if err != nil {
		common.SysErrorf("[APIMart] 上传请求发送失败: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	// 限制读取体积：Timeout 只限时长，恶意上游可流式输出打爆内存
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, common.MaxOutboundBodySize))
	if err != nil {
		common.SysErrorf("[APIMart] 读取上传响应失败: %v", err)
		return nil, err
	}

	// HTTP 非 200 视为失败
	if resp.StatusCode != http.StatusOK {
		common.SysErrorf("[APIMart] 上传 HTTP 状态异常: %d, 响应: %s", resp.StatusCode, truncate(string(respBody), 300))
		return nil, fmt.Errorf("upload failed: HTTP %d", resp.StatusCode)
	}

	var apiResp apimartUploadResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		common.SysErrorf("[APIMart] 上传响应解析失败: %v, 响应: %s", err, truncate(string(respBody), 300))
		return nil, err
	}

	if apiResp.URL == "" {
		common.SysErrorf("[APIMart] 上传响应缺少 url: %s", truncate(string(respBody), 300))
		return nil, fmt.Errorf("upload response missing url")
	}

	common.SysLogf("[APIMart] 上传成功: %s", apiResp.URL)

	return &UploadResult{
		URL:         apiResp.URL,
		Filename:    apiResp.Filename,
		ContentType: apiResp.ContentType,
		Bytes:       apiResp.Bytes,
	}, nil
}
