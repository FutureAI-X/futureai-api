package supplier

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
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

	// Quality / OutputFormat / Complexity 是部分模型开放的可选生成参数
	// （当前为 gpt-image-2.6-ext）。它们由 models.param_spec 决定是否可用：
	// 规格里没有对应字段时控制器会清空，不传给上游。
	// omitempty 让未使用这些参数的请求快照保持原有字段集。
	Quality      string `json:"quality,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
	Complexity   string `json:"complexity,omitempty"`

	// ImageIDs 是平台通用上传接口返回的 file_id 数组。
	// 非空时 controller 只认它，忽略 ImageURLs，并在调用上游前解析成短期签名 URL。
	// 供应商适配层不直接消费本字段：发往上游的始终是 ImageURLs。
	ImageIDs []string `json:"image_ids"`

	// VendorModelID 为供应商侧的模型 ID，由 controller 按映射关系填充。
	// 它不是对外标准字段：json:"-" 既让它进不了快照，
	// 也保证调用方无法通过请求体把它顶掉。
	VendorModelID string `json:"-"`
}

// 标准字段的默认值：调用方省略时补齐。
const (
	DefaultResolution = "1K"
	DefaultSize       = "16:9"
)

// ApplyDefaults 补齐调用方省略的标准字段，并把分辨率统一成大写。
//
// 必须在计费之前调用。计费条件匹配的是补齐后的值，若拖到供应商侧再补，
// 省略 resolution 的请求会按基础积分收费、实际却按 1K 出图——价格与生成结果不符。
// 供应商实现因此可以直接认为 size / resolution 已经就绪。
//
// 对外统一使用 1K / 2K / 4K；小写输入会被归一化为大写，供应商适配层再按
// 各自上游的要求转换。
func (r *ImageGenerateRequest) ApplyDefaults() {
	r.Size = strings.TrimSpace(r.Size)
	if r.Size == "" {
		r.Size = DefaultSize
	}

	r.Resolution = strings.ToUpper(strings.TrimSpace(r.Resolution))
	if r.Resolution == "" {
		r.Resolution = DefaultResolution
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
// 只列计费相关的字符串字段：image_urls 是数组，按约定永远命中不了参数组合条件
// （与文档中「条件仅支持顶层字符串参数」一致）；quality / output_format /
// complexity 不影响单价，也不是计费维度，因此同样不参与匹配。
// 键必须与 creditParamNames 一致。
//
// size / resolution 按平台规范做去空白归一化。分辨率统一为大写（1K / 2K / 4K），
// 与对外接口保持一致；size 统一为小写，避免 1024X1024 与 1024x1024 被当成两个值。
func (r ImageGenerateRequest) CreditParams() map[string]string {
	return map[string]string{
		"model":      r.Model,
		"prompt":     r.Prompt,
		"size":       strings.ToLower(strings.TrimSpace(r.Size)),
		"resolution": strings.ToUpper(strings.TrimSpace(r.Resolution)),
	}
}

// ── 视频生成 ──

// VideoGenerateRequest 视频生成的平台内部请求。
//
// 公开契约在 controller 层：prompt + images/videos/audios 三个 file_id 数组。
// controller 把 file_id 解析成短期签名 URL 后填充本结构，再交给供应商适配层。
// 因此这里的 Images/Videos/Audios 是**已解析的 URL**，不是 file_id。
//
// VendorModelID 由 controller 按管理端映射填充；json:"-" 保证它进不了快照，
// 也保证调用方无法通过请求体顶掉映射。
type VideoGenerateRequest struct {
	Model      string   `json:"model"`
	Prompt     string   `json:"prompt"`
	Images     []string `json:"images"`
	Videos     []string `json:"videos"`
	Audios     []string `json:"audios"`
	Resolution string   `json:"resolution"`
	Duration   int      `json:"duration"`
	Ratio      string   `json:"ratio"`

	VendorModelID string `json:"-"`
}

// Validate 校验平台内部视频请求。
//
// prompt 必填；媒体数组可以为空（纯文生视频）。数组元素是 controller 生成的
// 签名 URL，只接受 http(s)。
func (r *VideoGenerateRequest) Validate() error {
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
		urls []string
	}{
		{"images", r.Images},
		{"videos", r.Videos},
		{"audios", r.Audios},
	}
	for _, group := range groups {
		for _, u := range group.urls {
			if !isHTTPURL(u) {
				return fmt.Errorf("%s 包含非 http(s) 地址", group.name)
			}
		}
	}
	return nil
}

// isHTTPURL 判断字符串是否为 http/https 绝对地址。
// 媒体数组里的 URL 会被上游直接抓取，拒绝 file://、data: 等无法获取或不安全的形态。
func isHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return (scheme == "http" || scheme == "https") && u.Host != ""
}

// VideoGenerateResponse 视频生成规范化响应（与图像生成的失败性质语义一致）。
type VideoGenerateResponse struct {
	Code        string                 `json:"code"`
	Data        map[string]interface{} `json:"data,omitempty"`
	FailureKind string                 `json:"failure_kind,omitempty"`
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
	Data   map[string]interface{} `json:"data,omitempty"` // completed 时为结果；failed / cancelled 时可带上游错误
	// Kind 结果类型：image / video。空串按 image 处理（兼容旧供应商实现）。
	// 控制器据此决定是否把完成结果转存到对象存储——视频只返回上游 URL，不转存。
	Kind string `json:"kind,omitempty"`

	// RequireResultFetch 为 true 时，控制器必须成功下载结果资产才能判为完成。
	// MOSS 的结果 URL 可能指向不存在的资产（文档中的 404）；此时任务应失败
	// 并退款，而不是带着一个取不到图的地址进入 completed。
	RequireResultFetch bool `json:"require_result_fetch,omitempty"`
}

// 结果类型
const (
	ResultKindImage = "image"
	ResultKindVideo = "video"
)

// ImageCapabilities 供应商对图像请求的能力声明。
//
// 平台公开契约保持统一，但不同供应商的语义并不相同：MOSS 是同步生图且
// 不接受参考图，而 APIMart 是异步提交且支持参考图。控制器只通过能力声明
// 决定分流，不在控制器里写供应商名字。
type ImageCapabilities struct {
	// BackgroundSubmit 为 true 时，控制器不在 HTTP handler 里等待上游，
	// 而是先返回 taskId，由后台 goroutine 完成调用。
	BackgroundSubmit bool

	// SupportsReferenceImages 为 false 时，控制器忽略 image_urls / image_ids，
	// 不解析 file_id，也不计参考图附加费用。
	SupportsReferenceImages bool

	// RequiresPixelSize 为 true 时，size 必须是「宽x高」像素尺寸（如 1024x1024），
	// 比例值（如 16:9）会被拒绝。
	RequiresPixelSize bool

	// SubmitTimeout 本次提交允许的最长耗时；0 表示使用包级 SubmitTimeout。
	SubmitTimeout time.Duration
}

// DefaultImageCapabilities 返回未声明能力时的默认值：
// 同步提交、支持参考图、size 仍按平台原有语义透传。
func DefaultImageCapabilities() ImageCapabilities {
	return ImageCapabilities{SupportsReferenceImages: true}
}

// ImageCapabilityProvider 由需要覆盖默认能力的图像供应商实现。
type ImageCapabilityProvider interface {
	ImageCapabilities() ImageCapabilities
}

// ImageRequestValidator 供应商可选的请求预校验。
//
// 控制器在扣费、建任务、调用上游之前调用。返回错误说明请求本身不合法，
// 应当直接 400，绝不能先扣费再让上游拒绝。
type ImageRequestValidator interface {
	ValidateImageRequest(req ImageGenerateRequest) error
}

// Supplier 图像供应商接口，每个图像供应商实现此接口。
//
// 两个方法都接收 context：出站调用必须能被超时与取消约束，
// 否则上游卡住时调用方只能一直等（客户端早已断开，却仍占着连接与 goroutine）。
type Supplier interface {
	ImageGenerate(ctx context.Context, req ImageGenerateRequest) ImageGenerateResponse
	// TaskQuery 查询任务状态，vendorResponse 为提交任务时供应商返回的原始 JSON
	TaskQuery(ctx context.Context, vendorResponse string) TaskQueryResponse
}

// VideoSupplier 视频供应商接口。
type VideoSupplier interface {
	VideoGenerate(ctx context.Context, req VideoGenerateRequest) VideoGenerateResponse
	// TaskQuery 查询任务状态，vendorResponse 为提交任务时供应商返回的原始 JSON
	TaskQuery(ctx context.Context, vendorResponse string) TaskQueryResponse
}

// TaskQuerier 只依赖任务查询能力的窄接口，供对账循环使用。
// 图像与视频供应商都实现它：对账循环不关心任务类型，只需要查状态。
type TaskQuerier interface {
	TaskQuery(ctx context.Context, vendorResponse string) TaskQueryResponse
}

// NewSupplier 工厂函数，根据供应商名称返回图像供应商实现（名称不区分大小写）
func NewSupplier(vendorName string, cfg Config) Supplier {
	switch strings.ToLower(strings.TrimSpace(vendorName)) {
	case "apimart":
		return newAPIMart(cfg)
	case "moss":
		return newMoss(cfg)
	default:
		return nil
	}
}

// NewVideoSupplier 工厂函数，根据供应商名称返回视频供应商实现（名称不区分大小写）
func NewVideoSupplier(vendorName string, cfg Config) VideoSupplier {
	switch strings.ToLower(strings.TrimSpace(vendorName)) {
	case "metaso":
		return newMetaso(cfg)
	default:
		return nil
	}
}

// NewTaskQuerier 工厂函数，返回可查询任务状态的实现（名称不区分大小写）。
// 新增供应商时若它承载异步任务，记得在这里登记，否则重启后它的任务无法被对账。
func NewTaskQuerier(vendorName string, cfg Config) TaskQuerier {
	switch strings.ToLower(strings.TrimSpace(vendorName)) {
	case "apimart":
		return newAPIMart(cfg)
	case "metaso":
		return newMetaso(cfg)
	case "moss":
		return newMoss(cfg)
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
