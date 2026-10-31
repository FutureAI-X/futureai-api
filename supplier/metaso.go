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

	"github.com/FutureAI-X/futureai-api/common"
)

// metaso METASO 供应商实现（当前只承载视频生成）。
type metaso struct {
	cfg Config
}

// newMetaso 创建 METASO 供应商实例。
// 与 APIMart 一样不持有自己的 http.Client，复用进程级共享的出站客户端。
func newMetaso(cfg Config) *metaso {
	return &metaso{cfg: cfg}
}

// metasoVideoRequest Metaso 视频生成请求体。
//
// 只包含约定的标准字段。fast_mode 固定 false，且不是对外字段：
// 调用方无法通过请求体把它改成 true。
type metasoVideoRequest struct {
	Model      string             `json:"model"`
	Content    []VideoContentItem `json:"content"`
	Resolution string             `json:"resolution"`
	Duration   int                `json:"duration"`
	Ratio      string             `json:"ratio"`
	FastMode   bool               `json:"fast_mode"`
}

// buildVideoRequest 把平台标准字段翻译成 Metaso 的线上请求体。
// model 使用供应商模型 ID（controller 按管理端的映射填充），而不是外部模型名。
func buildVideoRequest(req VideoGenerateRequest) metasoVideoRequest {
	return metasoVideoRequest{
		Model:      req.VendorModelID,
		Content:    req.Content,
		Resolution: req.Resolution,
		Duration:   req.Duration,
		Ratio:      req.Ratio,
		FastMode:   false,
	}
}

// metasoSubmitResponse 生成接口的成功响应
type metasoSubmitResponse struct {
	TaskID string `json:"task_id"`
}

// metasoErrorResponse 生成 / 查询接口的错误响应
type metasoErrorResponse struct {
	Type  string `json:"type"`
	Error struct {
		Type     string `json:"type"`
		Message  string `json:"message"`
		HTTPCode string `json:"http_code"`
	} `json:"error"`
	RequestID string `json:"request_id"`
}

// metasoBaseURL 返回去掉末尾斜杠的 BaseURL。
// 管理端配置的地址可能带或不带末尾斜杠，直接拼接会出现 "//video_generation"。
func (m *metaso) baseURL() string {
	return strings.TrimRight(m.cfg.BaseURL, "/")
}

// VideoGenerate 调用 Metaso 视频生成 API。
func (m *metaso) VideoGenerate(ctx context.Context, req VideoGenerateRequest) VideoGenerateResponse {
	// 本函数内所有失败都必须明确标注性质：
	// 请求根本没发出去 / 上游明确拒绝 → rejected；发出去了但结果读不到 → unknown。
	// 两者的差别决定了控制器是否退款，判错就是资损。
	failRejected := VideoGenerateResponse{Code: "fail", FailureKind: FailureRejected}
	failUnknown := VideoGenerateResponse{Code: "fail", FailureKind: FailureUnknown}

	bodyBytes, err := json.Marshal(buildVideoRequest(req))
	if err != nil {
		// 本地序列化失败，请求尚未发出
		common.SysErrorf("[Metaso] 请求体序列化失败: %v", err)
		return failRejected
	}

	url := m.baseURL() + "/video_generation"
	common.SysLogf("[Metaso] 发起视频生成请求: POST %s", url)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		common.SysErrorf("[Metaso] 构建HTTP请求失败: %v", err)
		return failRejected
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", m.cfg.APIKey))

	resp, err := common.OutboundHTTPClient().Do(httpReq)
	if err != nil {
		// 超时或连接中断：请求可能已经送达并被受理，绝不能当成「没接单」
		common.SysErrorf("[Metaso] 请求发送失败(结果不确定，不得退款): %v", err)
		return failUnknown
	}
	defer resp.Body.Close()

	common.SysLogf("[Metaso] 收到响应: HTTP %d", resp.StatusCode)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, common.MaxOutboundBodySize))
	if err != nil {
		common.SysErrorf("[Metaso] 读取响应体失败(结果不确定，不得退款): %v", err)
		return failUnknown
	}

	// HTTP 状态码非 2xx。
	// 4xx 是上游明确拒绝，可以退款；5xx 可能来自上游前面的网关，
	// 请求或许已经落到上游并建了任务，只能算不确定。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		common.SysErrorf("[Metaso] HTTP状态码异常: %d, 响应: %s", resp.StatusCode, truncate(string(respBody), 500))
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return failRejected
		}
		return failUnknown
	}

	// 2xx 优先按成功响应解析。
	var submitResp metasoSubmitResponse
	if err := json.Unmarshal(respBody, &submitResp); err == nil && submitResp.TaskID != "" {
		common.SysLogf("[Metaso] 视频生成成功, taskId: %s", submitResp.TaskID)
		return VideoGenerateResponse{
			Code: "success",
			Data: map[string]interface{}{"task_id": submitResp.TaskID},
		}
	}

	// 2xx 但拿不到 task_id：可能响应里包了一层错误体，也可能响应被截断。
	// 错误体带 4xx 说明上游明确拒绝，可以退款；其余按不确定处理。
	if code, ok := metasoErrorHTTPCode(respBody); ok {
		common.SysErrorf("[Metaso] 2xx 响应中包含错误体: %s", truncate(string(respBody), 500))
		if code >= 400 && code < 500 {
			return failRejected
		}
		return failUnknown
	}

	common.SysErrorf("[Metaso] 响应中缺少 task_id(结果不确定，不得退款): %s", truncate(string(respBody), 500))
	return failUnknown
}

// metasoErrorHTTPCode 从错误响应体中取 http_code。
// Metaso 把 http_code 写成字符串；解析失败时 ok=false。
func metasoErrorHTTPCode(body []byte) (int, bool) {
	var errResp metasoErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		return 0, false
	}
	// 既不是 type=error，也没有任何错误字段，视为不是错误体
	if errResp.Type != "error" && errResp.Error.Type == "" &&
		errResp.Error.Message == "" && errResp.Error.HTTPCode == "" {
		return 0, false
	}
	code, err := strconv.Atoi(strings.TrimSpace(errResp.Error.HTTPCode))
	if err != nil {
		return 0, false
	}
	return code, true
}

// metasoTaskQueryResponse 查询接口响应
type metasoTaskQueryResponse struct {
	Task metasoTask `json:"task"`
}

type metasoTask struct {
	ID         string                 `json:"id"`
	Model      string                 `json:"model"`
	Title      string                 `json:"title"`
	Status     string                 `json:"status"`
	CreatedAt  int64                  `json:"created_at"`
	UpdatedAt  int64                  `json:"updated_at"`
	Content    metasoTaskContent      `json:"content"`
	Resolution string                 `json:"resolution"`
	Duration   int                    `json:"duration"`
	Usage      map[string]interface{} `json:"usage"`
	Ratio      string                 `json:"ratio"`
	Error      metasoTaskError        `json:"error"`
}

type metasoTaskContent struct {
	URL string `json:"url"`
}

type metasoTaskError struct {
	// Code 用 interface{} 接收：上游可能给字符串，也可能给数字。
	// 用 string 会让整段查询响应解析失败，进而把「任务已失败」误判成「查询失败」。
	Code    interface{} `json:"code"`
	Message string      `json:"message"`
}

// metasoTaskIDPattern 供应商任务 ID 的合法形态。
//
// task_id 来自上游响应且会被直接拼进查询 URL，必须限制字符集：
// 否则一个被控的上游（或异常响应）返回 "../other" 之类的内容，
// 就会带着供应商 API Key 去请求该主机上的任意路径。
var metasoTaskIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// TaskQuery 查询 Metaso 视频任务状态
// vendorResponse 为提交任务时供应商返回的原始 JSON，从中提取 task_id
func (m *metaso) TaskQuery(ctx context.Context, vendorResponse string) TaskQueryResponse {
	var respData map[string]interface{}
	if err := json.Unmarshal([]byte(vendorResponse), &respData); err != nil {
		common.SysErrorf("[Metaso] vendorResponse 解析失败: %v", err)
		return TaskQueryResponse{Status: "call_fail"}
	}
	taskID, _ := respData["task_id"].(string)
	if taskID == "" {
		common.SysErrorf("[Metaso] vendorResponse 中缺少 task_id")
		return TaskQueryResponse{Status: "call_fail"}
	}
	if !metasoTaskIDPattern.MatchString(taskID) {
		common.SysErrorf("[Metaso] task_id 形态非法，拒绝拼接查询 URL: %q", truncate(taskID, 64))
		return TaskQueryResponse{Status: "call_fail"}
	}

	// 查询失败一律是 call_fail（= 状态未知），控制器不会据此退款，
	// 因此这里所有错误路径共用同一个返回值。
	failResp := TaskQueryResponse{TaskID: taskID, Status: "call_fail"}

	url := m.baseURL() + "/query/video_generation/" + taskID
	common.SysLogf("[Metaso] 查询视频任务状态: GET %s", url)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		common.SysErrorf("[Metaso] 构建查询请求失败: %v", err)
		return failResp
	}
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", m.cfg.APIKey))

	resp, err := common.OutboundHTTPClient().Do(httpReq)
	if err != nil {
		common.SysErrorf("[Metaso] 查询请求发送失败: %v", err)
		return failResp
	}
	defer resp.Body.Close()

	common.SysLogf("[Metaso] 查询响应: HTTP %d", resp.StatusCode)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, common.MaxOutboundBodySize))
	if err != nil {
		common.SysErrorf("[Metaso] 读取查询响应失败: %v", err)
		return failResp
	}

	if resp.StatusCode != http.StatusOK {
		common.SysErrorf("[Metaso] 查询HTTP状态异常: %d, 响应: %s", resp.StatusCode, truncate(string(respBody), 500))
		return failResp
	}

	var queryResp metasoTaskQueryResponse
	if err := json.Unmarshal(respBody, &queryResp); err != nil {
		common.SysErrorf("[Metaso] 查询响应JSON解析失败: %v, 响应: %s", err, truncate(string(respBody), 500))
		return failResp
	}

	task := queryResp.Task
	if task.ID == "" && task.Status == "" {
		common.SysErrorf("[Metaso] 查询响应缺少 task: %s", truncate(string(respBody), 500))
		return failResp
	}

	common.SysLogf("[Metaso] 任务状态: %s", task.Status)

	switch task.Status {
	case "succeeded":
		return TaskQueryResponse{
			TaskID: taskID,
			Status: "completed",
			Kind:   ResultKindVideo,
			Data:   metasoCompletedData(task),
		}
	case "failed":
		return TaskQueryResponse{
			TaskID: taskID,
			Status: "failed",
			Kind:   ResultKindVideo,
			Data:   metasoFailureData(task),
		}
	case "cancelled":
		return TaskQueryResponse{
			TaskID: taskID,
			Status: "cancelled",
			Kind:   ResultKindVideo,
			Data:   metasoFailureData(task),
		}
	case "queued", "running":
		// 中间态：控制器保持任务非终态，下一轮继续查询
		return TaskQueryResponse{TaskID: taskID, Status: "submitted", Kind: ResultKindVideo}
	default:
		// 未识别的状态按查询失败处理：任务保持非终态，下一轮重试。
		// 绝不能把未知状态当成终态去退款。
		common.SysErrorf("[Metaso] 未识别的任务状态 %q，按查询失败处理: taskID=%s", task.Status, taskID)
		return TaskQueryResponse{TaskID: taskID, Status: "call_fail", Kind: ResultKindVideo}
	}
}

// metasoCompletedData 把上游 task 规范化为对外结果。
// 视频产物不转存，url 直接使用上游的限时下载地址。
func metasoCompletedData(task metasoTask) map[string]interface{} {
	data := map[string]interface{}{
		"url":        task.Content.URL,
		"resolution": task.Resolution,
		"duration":   task.Duration,
		"ratio":      task.Ratio,
	}
	if task.ID != "" {
		data["vendor_task_id"] = task.ID
	}
	if task.Model != "" {
		data["model"] = task.Model
	}
	if task.Title != "" {
		data["title"] = task.Title
	}
	if len(task.Usage) > 0 {
		data["usage"] = task.Usage
	}
	if task.CreatedAt > 0 {
		data["created_at"] = task.CreatedAt
	}
	if task.UpdatedAt > 0 {
		data["updated_at"] = task.UpdatedAt
	}
	return data
}

// metasoFailureData 把上游错误信息写入 query_response，便于回溯。
func metasoFailureData(task metasoTask) map[string]interface{} {
	data := map[string]interface{}{}
	if task.ID != "" {
		data["vendor_task_id"] = task.ID
	}

	code := ""
	if task.Error.Code != nil {
		code = fmt.Sprint(task.Error.Code)
	}
	if code != "" || task.Error.Message != "" {
		data["error"] = map[string]string{
			"code":    code,
			"message": task.Error.Message,
		}
	}
	return data
}
