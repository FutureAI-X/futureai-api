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

	"github.com/FutureAI/token-hub/common"
)

// apimart APIMart 供应商实现
type apimart struct {
	cfg    Config
	client *http.Client
}

// newAPIMart 创建 APIMart 供应商实例
func newAPIMart(cfg Config) *apimart {
	return &apimart{
		cfg:    cfg,
		client: common.NewSafeHTTPClient(defaultHTTPTimeout),
	}
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

// apimartVersionedImageModel 在 APIMart 侧靠 version 字段区分外观风格的图像模型。
// 调用方侧的多个模型都映射到这一个 vendor_model_id，因此请求体里的 model
// 不足以判断调用方要哪个变体，必须回看调用方原始模型名（req.Model）。
const apimartVersionedImageModel = "gpt-image-2.5-ext"

// apimartImageVersions 调用方模型名 → APIMart version 取值。
// version 不是 OpenAI 兼容接口的参数，调用方不会传，只能由服务端按模型名补全。
// 新增映射到 apimartVersionedImageModel 的模型时，必须在这里登记，
// 漏配会落到 APIMart 的默认值（flare）并打错误日志。
var apimartImageVersions = map[string]string{
	"gpt-image-2.5-flare-ext":    "flare",
	"gpt-image-2.5-sunburst-ext": "sunburst",
}

// applyImageVersion 按调用方请求的模型名补齐请求体里的 version 字段。
// 只有映射到 apimartVersionedImageModel 的模型才需要补；其余模型原样透传。
func applyImageVersion(body map[string]interface{}, requestedModel string) {
	if body["model"] != apimartVersionedImageModel {
		return
	}

	version, ok := apimartImageVersions[requestedModel]
	if !ok {
		common.SysErrorf("[APIMart] 模型 %s 未配置 version 映射，将使用 APIMart 默认值: model=%s",
			requestedModel, apimartVersionedImageModel)
		return
	}

	body["version"] = version

	common.SysLogf("[APIMart] 根据模型 %s 补全 version=%s", requestedModel, version)
}

// buildRequest 把平台标准字段翻译成 APIMart 的线上请求体。
//
// size / resolution 直接写入：默认值已在计费前由 ApplyDefaults 补齐，
// 传空串上游会当作未指定，反而绕开了默认值。image_urls 为空时不写入。
func buildRequest(req ImageGenerateRequest) map[string]interface{} {
	body := map[string]interface{}{
		"model":      req.VendorModelID,
		"prompt":     req.Prompt,
		"size":       req.Size,
		"resolution": req.Resolution,
	}
	if len(req.ImageURLs) > 0 {
		body["image_urls"] = req.ImageURLs
	}

	applyImageVersion(body, req.Model)

	return body
}

// ImageGenerate 调用 APIMart 图像生成 API
func (a *apimart) ImageGenerate(req ImageGenerateRequest) ImageGenerateResponse {
	fail := ImageGenerateResponse{Code: "fail"}

	bodyBytes, err := json.Marshal(buildRequest(req))
	if err != nil {
		common.SysErrorf("[APIMart] 请求体序列化失败: %v", err)
		return fail
	}

	// 构建 HTTP 请求
	url := fmt.Sprintf("%s/v1/images/generations", a.cfg.BaseURL)
	common.SysLogf("[APIMart] 发起图像生成请求: POST %s", url)

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		common.SysErrorf("[APIMart] 构建HTTP请求失败: %v", err)
		return fail
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.cfg.APIKey))

	// 发送请求
	resp, err := a.client.Do(httpReq)
	if err != nil {
		common.SysErrorf("[APIMart] 请求发送失败: %v", err)
		return fail
	}
	defer resp.Body.Close()

	common.SysLogf("[APIMart] 收到响应: HTTP %d", resp.StatusCode)

	// 读取响应
	// 限制读取体积：Timeout 只限时长，恶意上游可流式输出打爆内存
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, common.MaxOutboundBodySize))
	if err != nil {
		common.SysErrorf("[APIMart] 读取响应体失败: %v", err)
		return fail
	}

	// HTTP 状态码非 200 直接失败
	if resp.StatusCode != http.StatusOK {
		common.SysErrorf("[APIMart] HTTP状态码异常: %d, 响应: %s", resp.StatusCode, truncate(string(respBody), 500))
		return fail
	}

	// 解析响应
	var apiResp apimartResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		common.SysErrorf("[APIMart] 响应JSON解析失败: %v, 原始响应: %s", err, truncate(string(respBody), 500))
		return fail
	}

	// code 非 200 视为失败
	if apiResp.Code != 200 {
		common.SysErrorf("[APIMart] 业务code异常: %d, 响应: %s", apiResp.Code, truncate(string(respBody), 500))
		return fail
	}

	// 解析 data 数组，提取第一个元素的 task_id
	var tasks []apimartTaskItem
	if err := json.Unmarshal(apiResp.Data, &tasks); err != nil {
		common.SysErrorf("[APIMart] data字段解析失败: %v, data: %s", err, string(apiResp.Data))
		return fail
	}
	if len(tasks) == 0 {
		common.SysErrorf("[APIMart] data数组为空")
		return fail
	}

	common.SysLogf("[APIMart] 图像生成成功, taskId: %s", tasks[0].TaskID)

	return ImageGenerateResponse{
		Code: "success",
		Data: map[string]interface{}{
			"taskId": tasks[0].TaskID,
		},
	}
}

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
}

type apimartTaskResult struct {
	Images []apimartTaskImage `json:"images"`
}

type apimartTaskImage struct {
	URL     []string `json:"url"`
	B64JSON string   `json:"b64_json"`
}

// TaskQuery 查询 APIMart 任务状态
// vendorResponse 为提交任务时供应商返回的原始 JSON，APIMart 从中提取 taskId
func (a *apimart) TaskQuery(vendorResponse string) TaskQueryResponse {
	// 从 vendorResponse 中提取 taskId
	var respData map[string]interface{}
	if err := json.Unmarshal([]byte(vendorResponse), &respData); err != nil {
		common.SysErrorf("[APIMart] vendorResponse 解析失败: %v", err)
		return TaskQueryResponse{Status: "call_fail"}
	}
	taskID, _ := respData["taskId"].(string)
	if taskID == "" {
		common.SysErrorf("[APIMart] vendorResponse 中缺少 taskId")
		return TaskQueryResponse{Status: "call_fail"}
	}

	failResp := TaskQueryResponse{TaskID: taskID, Status: "call_fail"}

	// 构建请求
	url := fmt.Sprintf("%s/v1/tasks/%s?language=zh", a.cfg.BaseURL, taskID)
	common.SysLogf("[APIMart] 查询任务状态: GET %s", url)

	httpReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		common.SysErrorf("[APIMart] 构建查询请求失败: %v", err)
		return failResp
	}
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.cfg.APIKey))

	// 发送请求
	resp, err := a.client.Do(httpReq)
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
			Data:   map[string]interface{}{"url": "", "b64_json": ""},
		}
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
		return TaskQueryResponse{TaskID: taskID, Status: taskData.Status}
	}

	// 中间态：pending / processing
	return TaskQueryResponse{TaskID: taskID, Status: taskData.Status}
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
	cfg    Config
	client *http.Client
}

// newAPIMartUploader 创建 APIMart 上传实例
func newAPIMartUploader(cfg Config) *apimartUploader {
	return &apimartUploader{
		cfg:    cfg,
		client: common.NewSafeHTTPClient(defaultHTTPTimeout),
	}
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

	resp, err := u.client.Do(httpReq)
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
