package supplier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/FutureAI-X/futureai-api/common"
)

// minimax MiniMax-H3-Image 供应商实现。
//
// H3 是同步接口：一次 POST 在图片生成完成后才返回，没有 task_id，也没有
// 任务查询接口。因此控制器通过 ImageCapabilities.BackgroundSubmit 把它分流
// 到后台 goroutine，避免 HTTP handler 等待最长 600 秒。
//
// 文生图与图片编辑走两个上游路径：
//   - 无参考图：POST /v1/images/generations
//   - 有参考图：POST /v1/images/edits
//
// 平台对外仍然只有 /v1/images/generations；参考图由控制器解析成签名 URL 后
// 由本适配层改走编辑接口。调用方不需要也不应该感知这两个路径的差异。
type minimax struct {
	cfg Config
}

// newMinimax 创建 MiniMax H3 图像供应商实例。
func newMinimax(cfg Config) *minimax {
	return &minimax{cfg: cfg}
}

const (
	// minimaxMaxPromptRunes 上游 prompt 上限（32000 个 Unicode 字符）。
	// 平台 param_spec 通常配置得更小（4000），这里只做上游兜底。
	minimaxMaxPromptRunes = 32000

	// H3 像素尺寸约束：面积 655,360 ~ 20,000,000，宽高比 1:3 ~ 3:1。
	minimaxMinPixels = 655360
	minimaxMaxPixels = 20000000

	// minimaxMaxReferenceImages 单次编辑最多 16 张参考图。
	minimaxMaxReferenceImages = 16

	// minimaxSubmitTimeout 同步生图最长等待时间。上游文档要求客户端超时
	// 不低于 600 秒，这里留 20 秒余量。
	minimaxSubmitTimeout = 620 * time.Second

	// minimaxOutputPixelsPerToken 输出 token 与像素面积的实测关系。
	minimaxOutputPixelsPerToken = 1024

	// minimaxMinOutputTokens 预扣估算的输出 token 下限。
	//
	// 实测与文档的 output_tokens：
	//   1024×1024 → 4096
	//   2048×2048 → 4096
	//   4096×4096 → 16384
	//   2720×1536 → 4080（文档）
	//   5440×3072 → 16320（文档）
	// 等效公式 max(宽×高/1024, 4096) 同时拟合以上数据：
	//   1K: max(1024, 4096)=4096；2K: max(4096, 4096)=4096；4K: max(16384, 4096)=16384。
	// 统一按面积/1024 会让 1K 低估 4 倍；统一按 16×16 会让 2K 高估 4 倍。
	// 结算仍按实际 usage 多退少补。
	minimaxMinOutputTokens = 4096

	// minimaxEditTextTokens 图片编辑的 Prompt 增强固定开销（保守上限）。
	// 文档示例中一次短 prompt 的编辑请求文本输入约 12,093 token。
	minimaxEditTextTokens = 14000

	// minimaxEnhancedTextTokens 文生图开启 Prompt 增强时的固定开销（保守上限）。
	// 文档示例中一次短 prompt 的生成请求文本输入约 3,174 token。
	minimaxEnhancedTextTokens = 4000

	// minimaxRefImageTokens 每张参考图的保守 token 估算。
	minimaxRefImageTokens = 6000
)

// minimaxSizePattern 匹配「宽x高」。允许大小写 x 与两侧空白。
var minimaxSizePattern = regexp.MustCompile(`^\s*(\d{1,5})\s*[xX]\s*(\d{1,5})\s*$`)

// ImageCapabilities 声明 H3 的图像能力。
func (m *minimax) ImageCapabilities() ImageCapabilities {
	return ImageCapabilities{
		BackgroundSubmit:        true,
		SupportsReferenceImages: true,
		RequiresPixelSize:       true,
		SubmitTimeout:           minimaxSubmitTimeout,
	}
}

// ValidateImageRequest 在扣费与建任务之前校验 H3 的入参。
func (m *minimax) ValidateImageRequest(req ImageGenerateRequest) error {
	if strings.TrimSpace(req.Prompt) == "" {
		return fmt.Errorf("prompt 不能为空")
	}
	if utf8.RuneCountInString(req.Prompt) > minimaxMaxPromptRunes {
		return fmt.Errorf("prompt 不能超过 %d 个字符", minimaxMaxPromptRunes)
	}
	if _, _, err := parseMinimaxSize(req.Size); err != nil {
		return err
	}
	if req.ReferenceCount() > minimaxMaxReferenceImages {
		return fmt.Errorf("参考图最多 %d 张", minimaxMaxReferenceImages)
	}
	for _, u := range req.ImageURLs {
		if !isHTTPURL(u) {
			return fmt.Errorf("参考图必须是 http(s) 地址")
		}
	}
	if v := strings.TrimSpace(req.OutputFormat); v != "" {
		if _, ok := normalizeMinimaxOutputFormat(v); !ok {
			return fmt.Errorf("output_format 只支持 png、jpeg、webp")
		}
	}
	return nil
}

// parseMinimaxSize 解析并校验 H3 的像素尺寸。
func parseMinimaxSize(raw string) (int, int, error) {
	matches := minimaxSizePattern.FindStringSubmatch(raw)
	if matches == nil {
		return 0, 0, fmt.Errorf("H3 的 size 必须为「宽x高」像素尺寸，如 2720x1536")
	}

	var width, height int
	if _, err := fmt.Sscanf(matches[1], "%d", &width); err != nil || width <= 0 {
		return 0, 0, fmt.Errorf("size 中的宽度无效")
	}
	if _, err := fmt.Sscanf(matches[2], "%d", &height); err != nil || height <= 0 {
		return 0, 0, fmt.Errorf("size 中的高度无效")
	}

	pixels := int64(width) * int64(height)
	if pixels < minimaxMinPixels {
		return 0, 0, fmt.Errorf("size 的总像素不能小于 %d", minimaxMinPixels)
	}
	if pixels > minimaxMaxPixels {
		return 0, 0, fmt.Errorf("size 的总像素不能超过 %d", minimaxMaxPixels)
	}
	// 宽高比必须在 1:3 ~ 3:1（含边界），用交叉相乘避免浮点误差。
	if width*3 < height || height*3 < width {
		return 0, 0, fmt.Errorf("size 的宽高比必须在 1:3 到 3:1 之间")
	}
	return width, height, nil
}

// normalizeMinimaxOutputFormat 归一化输出格式；jpg 与 jpeg 等价。
func normalizeMinimaxOutputFormat(raw string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "jpg" {
		v = "jpeg"
	}
	switch v {
	case "png", "jpeg", "webp":
		return v, true
	default:
		return "", false
	}
}

// buildMinimaxRequest 把平台标准字段翻译成 H3 的请求体。
//
// size 只传像素尺寸：不传 aspect_ratio / area，也不传 resolution。
// 文生图开启 Prompt 增强（disable_prompt_enhanced=false）；编辑接口没有该字段，
// 且上游始终开启增强。
func buildMinimaxRequest(req ImageGenerateRequest) (map[string]interface{}, error) {
	width, height, err := parseMinimaxSize(req.Size)
	if err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"model":           req.VendorModelID,
		"prompt":          req.Prompt,
		"n":               1,
		"size":            fmt.Sprintf("%dx%d", width, height),
		"response_format": "url",
		"stream":          false,
		"aigc_watermark":  false,
	}

	if len(req.ImageURLs) > 0 {
		images := make([]map[string]string, 0, len(req.ImageURLs))
		for _, u := range req.ImageURLs {
			images = append(images, map[string]string{"image_url": u})
		}
		body["images"] = images
	} else {
		body["disable_prompt_enhanced"] = false
	}

	if v := strings.TrimSpace(req.OutputFormat); v != "" {
		if normalized, ok := normalizeMinimaxOutputFormat(v); ok {
			body["output_format"] = normalized
		}
	}
	return body, nil
}

// EstimateImageUsage 为 per_token 预扣提供保守估算。
//
// 输出 token 与像素面积的实测关系基本是 面积/1024；文本与参考图部分按上限估算：
//   - 文生图关闭增强，文本约等于 prompt 长度；
//   - 图片编辑强制增强，固定给 14000 token 的开销；
//   - 每张参考图按 6000 token 估算。
//
// 预扣安全系数由计费层统一乘上，本函数只返回估算的原始 usage。
func (m *minimax) EstimateImageUsage(req ImageGenerateRequest) TokenUsage {
	width, height, err := parseMinimaxSize(req.Size)
	if err != nil {
		return TokenUsage{}
	}

	refCount := req.ReferenceCount()
	outputTokens := int(int64(width) * int64(height) / minimaxOutputPixelsPerToken)
	if outputTokens < minimaxMinOutputTokens {
		// 上游对较小尺寸也可能按 2K 出图，预扣按 2K 兜底，避免结算大额补扣。
		outputTokens = minimaxMinOutputTokens
	}
	usage := TokenUsage{
		ImageOutputTokens: outputTokens,
		ImageInputTokens:  refCount * minimaxRefImageTokens,
	}

	text := utf8.RuneCountInString(strings.TrimSpace(req.Prompt))
	if refCount > 0 {
		// 编辑接口始终增强；文档示例的文本输入约 12k，这里取 14k 作为保守值。
		text = minimaxEditTextTokens + utf8.RuneCountInString(strings.TrimSpace(req.Prompt))
	} else {
		// 文生图已开启 Prompt 增强；文档示例的文本输入约 3,174，这里取 4,000 保守值。
		text = minimaxEnhancedTextTokens + utf8.RuneCountInString(strings.TrimSpace(req.Prompt))
	}
	usage.TextInputTokens = text
	usage.TotalTokens = usage.TextInputTokens + usage.ImageInputTokens + usage.ImageOutputTokens
	return usage
}

// minimaxUsage 上游 usage 结构。
type minimaxUsage struct {
	GeneratedImages     int `json:"generated_images"`
	TotalTokens         int `json:"total_tokens"`
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	InputTokensDetails  struct {
		TextTokens  int `json:"text_tokens"`
		ImageTokens int `json:"image_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails struct {
		ImageTokens int `json:"image_tokens"`
	} `json:"output_tokens_details"`
}

// tokenUsage 把上游 usage 归一化为平台标准结构。
func (u minimaxUsage) tokenUsage() TokenUsage {
	text := u.InputTokensDetails.TextTokens
	imageIn := u.InputTokensDetails.ImageTokens
	if text == 0 && imageIn == 0 && u.InputTokens > 0 {
		// 老响应可能只给 input_tokens，按文本输入兜底。
		text = u.InputTokens
	}
	imageOut := u.OutputTokensDetails.ImageTokens
	if imageOut == 0 {
		imageOut = u.OutputTokens
	}
	return TokenUsage{
		TextInputTokens:   text,
		ImageInputTokens:  imageIn,
		ImageOutputTokens: imageOut,
		TotalTokens:       u.TotalTokens,
	}
}

// minimaxImageResponse 上游图片项。
type minimaxImageResponse struct {
	URL           string              `json:"url"`
	B64JSON       string              `json:"b64_json"`
	Size          string              `json:"size"`
	RevisedPrompt string              `json:"revised_prompt"`
	OutputFormat  string              `json:"output_format"`
	Error         *minimaxImageError  `json:"error"`
}

type minimaxImageError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// minimaxResponse 生成 / 编辑接口的响应体，同时兼容成功与错误两种形态。
type minimaxResponse struct {
	Created      int64                   `json:"created"`
	Data         []minimaxImageResponse  `json:"data"`
	OutputFormat string                  `json:"output_format"`
	Usage        minimaxUsage            `json:"usage"`
	RequestID    string                  `json:"request_id"`
	Type         string                  `json:"type"`
	Error        *minimaxErrorDetail     `json:"error"`
}

type minimaxErrorDetail struct {
	Type     string `json:"type"`
	Message  string `json:"message"`
	HTTPCode string `json:"http_code"`
}

func (e *minimaxErrorDetail) summary() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return e.Type
	}
	if e.HTTPCode == "" {
		return truncate(e.Message, 200)
	}
	return truncate(e.Message, 200) + " (http_code=" + e.HTTPCode + ")"
}

// minimaxBaseURL 返回去掉末尾斜杠的 BaseURL。
func (m *minimax) baseURL() string {
	return strings.TrimRight(m.cfg.BaseURL, "/")
}

// ImageGenerate 调用 H3 文生图或图片编辑 API。
func (m *minimax) ImageGenerate(ctx context.Context, req ImageGenerateRequest) ImageGenerateResponse {
	failRejected := ImageGenerateResponse{Code: "fail", FailureKind: FailureRejected}
	failUnknown := ImageGenerateResponse{Code: "fail", FailureKind: FailureUnknown}

	body, err := buildMinimaxRequest(req)
	if err != nil {
		common.SysErrorf("[MiniMax] 请求参数校验失败: %v", err)
		return failRejected
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		common.SysErrorf("[MiniMax] 请求体序列化失败: %v", err)
		return failRejected
	}

	path := "/v1/images/generations"
	if len(req.ImageURLs) > 0 {
		path = "/v1/images/edits"
	}
	url := m.baseURL() + path
	common.SysLogf("[MiniMax] 发起图像请求: POST %s, model=%s, size=%s, refs=%d",
		url, req.VendorModelID, body["size"], len(req.ImageURLs))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		common.SysErrorf("[MiniMax] 构建HTTP请求失败: %v", err)
		return failRejected
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.cfg.APIKey)

	resp, err := common.LongOutboundHTTPClient().Do(httpReq)
	if err != nil {
		// 超时或连接中断：上游可能已经出图并计费，但我们无从取回。
		common.SysErrorf("[MiniMax] [MANUAL_RECONCILE] 请求发送失败，结果不确定: %v", err)
		return failUnknown
	}
	defer resp.Body.Close()

	common.SysLogf("[MiniMax] 收到响应: HTTP %d", resp.StatusCode)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, common.MaxOutboundBodySize))
	if err != nil {
		common.SysErrorf("[MiniMax] [MANUAL_RECONCILE] 读取响应体失败，结果不确定: %v", err)
		return failUnknown
	}

	// 完整收到非 2xx 响应：上游明确返回了错误，全部图片失败时 usage 为 0、不计费，
	// 可以安全退款。这与 MOSS 的处理一致。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		common.SysErrorf("[MiniMax] HTTP状态码异常: %d, 响应: %s", resp.StatusCode, truncate(string(respBody), 500))
		return failRejected
	}

	var apiResp minimaxResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		// 响应不完整或格式变动：上游可能已经成功出图，不能当成未受理。
		common.SysErrorf("[MiniMax] [MANUAL_RECONCILE] 响应JSON解析失败，结果不确定: %v, 原始响应: %s",
			err, truncate(string(respBody), 500))
		return failUnknown
	}

	if apiResp.Error != nil {
		common.SysErrorf("[MiniMax] 业务错误: %s", apiResp.Error.summary())
		return failRejected
	}
	if len(apiResp.Data) == 0 {
		common.SysErrorf("[MiniMax] 响应缺少 data: %s", truncate(string(respBody), 500))
		return failRejected
	}

	// n 固定为 1；若上游返回多项也只取第一项。
	image := apiResp.Data[0]
	if len(apiResp.Data) > 1 {
		common.SysErrorf("[MiniMax] 上游返回了 %d 张图，平台只取第一张", len(apiResp.Data))
	}
	if image.Error != nil {
		common.SysErrorf("[MiniMax] 单张图片失败: code=%s, message=%s", image.Error.Code, truncate(image.Error.Message, 200))
		return failRejected
	}

	imageURL := strings.TrimSpace(image.URL)
	if imageURL == "" || !isHTTPURL(imageURL) {
		common.SysErrorf("[MiniMax] 响应中的图片地址无效: %s", truncate(imageURL, 300))
		return failRejected
	}

	usage := apiResp.Usage.tokenUsage()
	data := map[string]interface{}{
		"url":   imageURL,
		"size":  image.Size,
		"usage": apiResp.Usage,
	}
	if image.RevisedPrompt != "" {
		data["revised_prompt"] = image.RevisedPrompt
	}
	if apiResp.Created > 0 {
		data["created"] = apiResp.Created
	}
	if apiResp.RequestID != "" {
		data["request_id"] = apiResp.RequestID
	}

	common.SysLogf("[MiniMax] 图像生成成功: model=%s, size=%s, tokens(text=%d,img_in=%d,img_out=%d)",
		req.VendorModelID, body["size"], usage.TextInputTokens, usage.ImageInputTokens, usage.ImageOutputTokens)

	return ImageGenerateResponse{Code: "success", Data: data, Usage: &usage}
}

// minimaxStoredResult 后台提交成功时写入 vendor_response 的结构。
type minimaxStoredResult struct {
	URL           string       `json:"url"`
	Size          string       `json:"size"`
	RevisedPrompt string       `json:"revised_prompt"`
	Usage         minimaxUsage `json:"usage"`
	Created       int64        `json:"created"`
	RequestID     string       `json:"request_id"`
}

// TaskQuery 对 H3 不发起任何上游请求。
//
// H3 没有任务查询接口；后台提交成功时已经把结果（含 usage）写进 vendor_response，
// 这里只需把它还原成 completed 并带回 usage，供 token 结算使用。
func (m *minimax) TaskQuery(ctx context.Context, vendorResponse string) TaskQueryResponse {
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(vendorResponse), &stored); err != nil {
		common.SysErrorf("[MiniMax] vendorResponse 解析失败，按失败退款: %v", err)
		return TaskQueryResponse{
			Status:        "failed",
			Kind:          ResultKindImage,
			FailureReason: FailureReasonUpstreamFailed,
			Data:          map[string]interface{}{"error": "invalid minimax result"},
		}
	}

	rawURL, _ := stored["url"].(string)
	if strings.TrimSpace(rawURL) == "" || !isHTTPURL(rawURL) {
		common.SysErrorf("[MiniMax] vendorResponse 缺少有效 url，按失败退款: %s", truncate(vendorResponse, 300))
		return TaskQueryResponse{
			Status:        "failed",
			Kind:          ResultKindImage,
			FailureReason: FailureReasonUpstreamFailed,
			Data:          map[string]interface{}{"error": "minimax result missing url"},
		}
	}

	var parsed minimaxStoredResult
	if err := json.Unmarshal([]byte(vendorResponse), &parsed); err != nil {
		common.SysErrorf("[MiniMax] vendorResponse 解析 usage 失败，按失败退款: %v", err)
		return TaskQueryResponse{
			Status:        "failed",
			Kind:          ResultKindImage,
			FailureReason: FailureReasonUpstreamFailed,
			Data:          map[string]interface{}{"error": "invalid minimax usage"},
		}
	}

	usage := parsed.Usage.tokenUsage()
	return TaskQueryResponse{
		Status:             "completed",
		Kind:               ResultKindImage,
		Data:               stored,
		RequireResultFetch: false,
		// H3 的结果地址有效 24 小时，且不需要占用平台对象存储；
		// 直接返回上游 URL，不下载、不转存 COS。
		SkipResultPersist: true,
		Usage:             &usage,
	}
}
