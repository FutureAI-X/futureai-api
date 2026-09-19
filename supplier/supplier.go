package supplier

import (
	"time"
)

// Config 供应商配置（调用方从 Vendor 模型读取并解密后传入）
type Config struct {
	BaseURL string // 供应商 API 基础地址
	APIKey  string // 已解密的 API Key
}

// ImageGenerateRequest 图像生成的标准化请求。
//
// 这是平台对外的唯一字段集：调用方只能通过这些字段表达意图，请求体里的其他字段
// 一律忽略（encoding/json 的默认行为），各供应商的参数差异（改名、变体参数等）
// 全部在供应商实现内部消化。json tag 既是对外契约，也是 tasks.request_body 快照的来源。
type ImageGenerateRequest struct {
	Model      string   `json:"model"`
	Prompt     string   `json:"prompt"`
	Size       string   `json:"size"`
	Resolution string   `json:"resolution"`
	ImageURLs  []string `json:"image_urls"`

	// VendorModelID 为供应商侧的模型 ID，由 controller 按映射关系填充。
	// 它不是对外标准字段：json:"-" 既让它进不了快照，
	// 也保证调用方无法通过请求体把它顶掉。
	VendorModelID string `json:"-"`
}

// 标准字段的默认值：调用方省略时补齐。
const (
	DefaultResolution = "1k"
	DefaultSize       = "16:9"
)

// ApplyDefaults 补齐调用方省略的标准字段。
//
// 必须在计费之前调用。计费条件匹配的是补齐后的值，若拖到供应商侧再补，
// 省略 resolution 的请求会按基础积分收费、实际却按 1k 出图——价格与生成结果不符。
// 供应商实现因此可以直接认为 size / resolution 已经就绪。
func (r *ImageGenerateRequest) ApplyDefaults() {
	if r.Resolution == "" {
		r.Resolution = DefaultResolution
	}
	if r.Size == "" {
		r.Size = DefaultSize
	}
}

// creditParamNames 参与参数组合计费的标准字段名。
// 与 CreditParams 的键保持一致（有测试交叉校验，避免两边漏改）。
var creditParamNames = []string{"model", "prompt", "size", "resolution"}

// CreditParamNames 返回可参与参数组合计费的标准字段名（副本，调用方可自由修改）。
func CreditParamNames() []string {
	return append([]string(nil), creditParamNames...)
}

// IsCreditParamName 判断参数名是否为可参与计费的标准字段。
// 之外的名字永远匹配不上——计费只认标准字段，调用方的非标准字段进不了计费流程。
func IsCreditParamName(name string) bool {
	for _, n := range creditParamNames {
		if n == name {
			return true
		}
	}
	return false
}

// CreditParams 返回参与参数组合计费的字段（参数名 → 值）。
//
// 只列字符串字段：image_urls 是数组，按约定永远命中不了参数组合条件
// （与文档中「条件仅支持顶层字符串参数」一致），因此不参与匹配。
// 键必须与 creditParamNames 一致。
func (r ImageGenerateRequest) CreditParams() map[string]string {
	return map[string]string{
		"model":      r.Model,
		"prompt":     r.Prompt,
		"size":       r.Size,
		"resolution": r.Resolution,
	}
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
	Status string                 `json:"status"`         // completed, failed, cancelled, pending, processing, call_fail
	Data   map[string]interface{} `json:"data,omitempty"` // 仅 completed 时有值
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
