package supplier

import (
	"time"
)

// Config 供应商配置（调用方从 Vendor 模型读取并解密后传入）
type Config struct {
	BaseURL string // 供应商 API 基础地址
	APIKey  string // 已解密的 API Key
}

// ImageGenerateRequest 图像生成规范化请求
type ImageGenerateRequest struct {
	// Model 为调用方请求的模型名（用户侧模型名，未被替换成供应商模型 ID）。
	// 多个调用方模型映射到同一个 vendor_model_id 时，Body["model"] 已经无法区分
	// 调用方要的是哪个变体，供应商实现需要靠这个字段补齐变体参数。
	Model string

	// Body 为外部传入的原始 JSON，直接透传给供应商 API
	Body map[string]interface{}
}

// ImageGenerateResponse 图像生成规范化响应（所有供应商统一返回）
// 成功时 Data 为供应商返回的业务数据（不同供应商内容可能不同）
// 失败时 Data 为 nil
type ImageGenerateResponse struct {
	Code string                 `json:"code"`           // "success" 或 "fail"
	Data map[string]interface{} `json:"data,omitempty"` // 成功时的业务数据
}

// TaskQueryResponse 任务查询规范化响应（所有供应商统一返回）
type TaskQueryResponse struct {
	TaskID string                 `json:"task_id"`
	Status string                 `json:"status"`           // completed, failed, cancelled, pending, processing, call_fail
	Data   map[string]interface{} `json:"data,omitempty"`   // 仅 completed 时有值
}

// Supplier 供应商接口，每个供应商实现此接口
type Supplier interface {
	ImageGenerate(req ImageGenerateRequest) ImageGenerateResponse
	// TaskQuery 查询任务状态，vendorResponse 为提交任务时供应商返回的原始 JSON
	TaskQuery(vendorResponse string) TaskQueryResponse
}

// NewSupplier 工厂函数，根据供应商名称返回对应实现
func NewSupplier(vendorName string, cfg Config) Supplier {
	switch vendorName {
	case "apimart", "APIMart":
		return newAPIMart(cfg)
	default:
		return nil
	}
}

// defaultHTTPTimeout 默认 HTTP 超时
const defaultHTTPTimeout = 30 * time.Second
