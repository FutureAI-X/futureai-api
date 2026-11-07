package supplier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/FutureAI-X/futureai-api/common"
)

// moss MOSS 供应商实现（文生图）。
//
// MOSS 与 APIMart 的结构性差异：它是**同步**接口，一次 POST 直接返回图片
// URL，没有 task_id，也没有任务查询接口。因此控制器通过 ImageCapabilities
// 把它分流到后台提交，避免 HTTP handler 等待最长 15 分钟。
type moss struct {
	cfg Config
}

// newMoss 创建 MOSS 供应商实例。
// 与其它供应商一样不持有自己的 http.Client：同步调用最长可达 15 分钟，
// 仍复用进程级共享的长响应客户端（见 common.LongOutboundHTTPClient）。
func newMoss(cfg Config) *moss {
	return &moss{cfg: cfg}
}

const (
	// mossNegativePrompt 固定的负向词，取官方 Model Card 推荐值。
	mossNegativePrompt = "nsfw, worst quality, bad anatomy, watermark, jpeg artifact"

	// mossMaxPromptRunes prompt 长度上限（官方文档为 4000 字符）。
	mossMaxPromptRunes = 4000

	// MOSS 对 size 的约束：每边 256–2048、16 的倍数、总像素不超过 2097152。
	mossMinSide      = 256
	mossMaxSide      = 2048
	mossSideMultiple = 16
	mossMaxPixels    = 2097152

	// mossSubmitTimeout 同步生图的最长等待时间，与官方示例的 --max-time 900 对齐。
	mossSubmitTimeout = 15 * time.Minute
)

// mossSizePattern 匹配「宽x高」。允许大小写 x 与两侧空白，最终统一规范化。
var mossSizePattern = regexp.MustCompile(`^\s*(\d{1,4})\s*[xX]\s*(\d{1,4})\s*$`)

// ImageCapabilities 声明 MOSS 的图像能力。
func (m *moss) ImageCapabilities() ImageCapabilities {
	return ImageCapabilities{
		BackgroundSubmit:        true,
		SupportsReferenceImages: false,
		RequiresPixelSize:       true,
		SubmitTimeout:           mossSubmitTimeout,
	}
}

// ValidateImageRequest 在扣费前校验 MOSS 特有的参数约束。
//
// 这里只校验 MOSS 真正需要的字段：prompt 与 size。参考图由控制器直接忽略，
// 不参与校验，也不参与计费。
func (m *moss) ValidateImageRequest(req ImageGenerateRequest) error {
	if strings.TrimSpace(req.Prompt) == "" {
		return fmt.Errorf("prompt 不能为空")
	}
	if utf8.RuneCountInString(req.Prompt) > mossMaxPromptRunes {
		return fmt.Errorf("prompt 不能超过 %d 个字符", mossMaxPromptRunes)
	}
	if _, _, err := parseMossSize(req.Size); err != nil {
		return err
	}
	return nil
}

// parseMossSize 解析并校验 MOSS 的 size。
// 返回的宽高已通过全部官方约束，可直接拼成上游的 size 字符串。
func parseMossSize(raw string) (int, int, error) {
	matches := mossSizePattern.FindStringSubmatch(raw)
	if matches == nil {
		return 0, 0, fmt.Errorf("MOSS 模型的 size 必须为「宽x高」像素尺寸，如 1024x1024")
	}

	width, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, 0, fmt.Errorf("size 中的宽度无效")
	}
	height, err := strconv.Atoi(matches[2])
	if err != nil {
		return 0, 0, fmt.Errorf("size 中的高度无效")
	}

	if width < mossMinSide || height < mossMinSide {
		return 0, 0, fmt.Errorf("size 的宽高不能小于 %d", mossMinSide)
	}
	if width > mossMaxSide || height > mossMaxSide {
		return 0, 0, fmt.Errorf("size 的宽高不能大于 %d", mossMaxSide)
	}
	if width%mossSideMultiple != 0 || height%mossSideMultiple != 0 {
		return 0, 0, fmt.Errorf("size 的宽高必须是 %d 的倍数", mossSideMultiple)
	}
	if width*height > mossMaxPixels {
		return 0, 0, fmt.Errorf("size 的总像素不能超过 %d", mossMaxPixels)
	}
	return width, height, nil
}

// buildMossRequest 把平台标准字段翻译成 MOSS 的请求体。
//
// 参考图、resolution、seed 等字段刻意不写入：MOSS 文生图不支持参考图，
// 其余参数按产品约定使用固定值或上游默认值。
func buildMossRequest(req ImageGenerateRequest) (map[string]interface{}, error) {
	width, height, err := parseMossSize(req.Size)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"model":           req.VendorModelID,
		"prompt":          req.Prompt,
		"size":            fmt.Sprintf("%dx%d", width, height),
		"negative_prompt": mossNegativePrompt,
		"prompt_enhance":  true,
		"n":               1,
		"response_format": "url",
		"output_watermark": map[string]interface{}{
			"enabled": false,
		},
	}, nil
}

// mossResponse MOSS 生成接口的响应体。
type mossResponse struct {
	Created int64                  `json:"created"`
	Data    []mossImage            `json:"data"`
	Usage   map[string]interface{} `json:"usage"`
	Error   *mossError             `json:"error"`
}

type mossImage struct {
	URL string `json:"url"`
}

// mossError 同时兼容官方文档的 error.code 与 OpenAI 风格的 error.type。
type mossError struct {
	Code    string `json:"code"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (e *mossError) summary() string {
	if e == nil {
		return ""
	}
	code := e.Code
	if code == "" {
		code = e.Type
	}
	if code == "" {
		return truncate(e.Message, 200)
	}
	if e.Message == "" {
		return code
	}
	return code + ": " + truncate(e.Message, 200)
}

// mossBaseURL 返回去掉末尾斜杠的 BaseURL。
func (m *moss) baseURL() string {
	return strings.TrimRight(m.cfg.BaseURL, "/")
}

// ImageGenerate 调用 MOSS 文生图 API。
//
// 失败性质按产品约定统一为 rejected：MOSS 没有 task_id，超时/断连后也
// 无法再查询结果，因此「没拿到图片」就退款。需要人工对账的模糊失败会打
// [MANUAL_RECONCILE] 标记。
func (m *moss) ImageGenerate(ctx context.Context, req ImageGenerateRequest) ImageGenerateResponse {
	fail := ImageGenerateResponse{Code: "fail", FailureKind: FailureRejected}

	body, err := buildMossRequest(req)
	if err != nil {
		common.SysErrorf("[MOSS] 请求参数校验失败: %v", err)
		return fail
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		common.SysErrorf("[MOSS] 请求体序列化失败: %v", err)
		return fail
	}

	url := m.baseURL() + "/v1/images/generations"
	common.SysLogf("[MOSS] 发起图像生成请求: POST %s, model=%s, size=%s", url, req.VendorModelID, body["size"])

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		common.SysErrorf("[MOSS] 构建HTTP请求失败: %v", err)
		return fail
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", m.cfg.APIKey))

	resp, err := common.LongOutboundHTTPClient().Do(httpReq)
	if err != nil {
		// 超时或连接中断：图片可能已经生成并计费，但我们无从取回。
		// 按产品约定仍然退款，同时留下人工对账标记。
		common.SysErrorf("[MOSS] [MANUAL_RECONCILE] 请求发送失败，按失败退款: %v", err)
		return fail
	}
	defer resp.Body.Close()

	common.SysLogf("[MOSS] 收到响应: HTTP %d", resp.StatusCode)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, common.MaxOutboundBodySize))
	if err != nil {
		common.SysErrorf("[MOSS] [MANUAL_RECONCILE] 读取响应体失败，按失败退款: %v", err)
		return fail
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		common.SysErrorf("[MOSS] HTTP状态码异常: %d, 响应: %s", resp.StatusCode, truncate(string(respBody), 500))
		return fail
	}

	var apiResp mossResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		common.SysErrorf("[MOSS] [MANUAL_RECONCILE] 响应JSON解析失败，按失败退款: %v, 原始响应: %s",
			err, truncate(string(respBody), 500))
		return fail
	}

	if apiResp.Error != nil {
		common.SysErrorf("[MOSS] 业务错误: %s", apiResp.Error.summary())
		return fail
	}

	if len(apiResp.Data) == 0 || strings.TrimSpace(apiResp.Data[0].URL) == "" {
		common.SysErrorf("[MOSS] [MANUAL_RECONCILE] 响应缺少图片地址，按失败退款: %s", truncate(string(respBody), 500))
		return fail
	}

	imageURL := strings.TrimSpace(apiResp.Data[0].URL)
	if !isHTTPURL(imageURL) {
		common.SysErrorf("[MOSS] 响应中的图片地址不是 http(s): %s", truncate(imageURL, 300))
		return fail
	}

	data := map[string]interface{}{
		"url": imageURL,
	}
	if len(apiResp.Usage) > 0 {
		data["usage"] = apiResp.Usage
	}
	if apiResp.Created > 0 {
		data["created"] = apiResp.Created
	}

	common.SysLogf("[MOSS] 图像生成成功: model=%s, size=%s", req.VendorModelID, body["size"])

	return ImageGenerateResponse{Code: "success", Data: data}
}

// TaskQuery 对 MOSS 任务不发起任何上游请求。
//
// MOSS 没有任务查询接口；后台提交成功时已经把最终结果写进 vendor_response，
// 这里只需把它还原成 completed。结果缺失说明落库数据异常，按失败处理并退款。
func (m *moss) TaskQuery(ctx context.Context, vendorResponse string) TaskQueryResponse {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(vendorResponse), &data); err != nil {
		common.SysErrorf("[MOSS] vendorResponse 解析失败，按失败退款: %v", err)
		return TaskQueryResponse{
			Status: "failed",
			Kind:   ResultKindImage,
			Data:   map[string]interface{}{"error": "invalid moss result"},
		}
	}

	rawURL, _ := data["url"].(string)
	if strings.TrimSpace(rawURL) == "" {
		common.SysErrorf("[MOSS] vendorResponse 缺少 url，按失败退款")
		return TaskQueryResponse{
			Status: "failed",
			Kind:   ResultKindImage,
			Data:   map[string]interface{}{"error": "moss result missing url"},
		}
	}

	return TaskQueryResponse{
		Status:             "completed",
		Kind:               ResultKindImage,
		Data:               data,
		RequireResultFetch: true,
	}
}
