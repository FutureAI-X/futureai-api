package supplier

import (
	"context"
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

// 失败性质。区分二者是计费正确性的前提：
//
//   - FailureRejected：上游明确拒绝（业务错误码、4xx），本次请求未被受理，
//     用户没有拿到任何东西，可以安全退款；
//   - FailureUnknown：结果不确定（超时、连接被中断、响应读不完整或解析失败）。
//     请求很可能已经送达并被受理，**退款就是平台净损失**，因此不能当作失败处理，
//     必须交给对账逻辑继续查证。
//
// 把两者混为一谈是最容易发生的资损来源：一次普通的网络抖动就会让平台
// 「上游照常出图、用户全额退款」。
const (
	// FailureRejected 上游明确拒绝，未受理
	FailureRejected = "rejected"
	// FailureUnknown 结果不确定，上游可能已受理
	FailureUnknown = "unknown"
)

// ImageGenerateResponse 图像生成规范化响应（所有供应商统一返回）
// 成功时 Data 为供应商返回的业务数据（不同供应商内容可能不同）
// 失败时 Data 为 nil，且 FailureKind 说明失败性质
type ImageGenerateResponse struct {
	Code string                 `json:"code"`           // "success" 或 "fail"
	Data map[string]interface{} `json:"data,omitempty"` // 成功时的业务数据

	// FailureKind 仅 Code == "fail" 时有意义，取值见 FailureRejected / FailureUnknown
	FailureKind string `json:"failure_kind,omitempty"`
}

// TaskQueryResponse 任务查询规范化响应（所有供应商统一返回）
//
// 注意 Status == "call_fail" 的语义是「**查询本身**失败」，而不是「任务失败」。
// 查询失败时任务状态仍然未知，调用方不得据此退款或判定任务终结，
// 只能稍后重试；只有上游明确返回 failed / cancelled 才是可退款的终态。
type TaskQueryResponse struct {
	TaskID string                 `json:"task_id"`
	Status string                 `json:"status"`         // completed, failed, cancelled, pending, processing, call_fail
	Data   map[string]interface{} `json:"data,omitempty"` // 仅 completed 时有值
}

// Supplier 供应商接口，每个供应商实现此接口。
//
// 两个方法都接收 context：出站调用必须能被超时与取消约束，
// 否则上游卡住时调用方只能一直等（客户端早已断开，却仍占着连接与 goroutine）。
type Supplier interface {
	ImageGenerate(ctx context.Context, req ImageGenerateRequest) ImageGenerateResponse
	// TaskQuery 查询任务状态，vendorResponse 为提交任务时供应商返回的原始 JSON
	TaskQuery(ctx context.Context, vendorResponse string) TaskQueryResponse
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

// SubmitTimeout 提交任务到上游的超时。
// 上游是「提交后返回 task_id」的异步语义，正常在秒级返回。
const SubmitTimeout = 30 * time.Second

// QueryTimeout 单次查询上游任务状态的超时。
// 只查状态、不传数据，应显著短于提交超时——查询卡住时轮询循环要能快速翻页。
const QueryTimeout = 15 * time.Second
