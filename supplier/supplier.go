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

// ── 视频生成 ──

// 标准内容项类型。视频请求的 content 数组只接受这四类项。
const (
	VideoContentTypeText  = "text"
	VideoContentTypeImage = "image_url"
	VideoContentTypeVideo = "video_url"
	VideoContentTypeAudio = "audio_url"
)

// VideoContentURL 内容项里的 URL 容器。
type VideoContentURL struct {
	URL string `json:"url"`
}

// VideoContentItem 视频生成请求中的内容项。
//
// 只声明标准内容项：text / image_url / video_url / audio_url。
// 请求体里的其他字段由 encoding/json 忽略；未知 type 由 Validate 拒绝，
// 而不是静默丢弃——丢弃会让调用方以为参考素材已经生效。
type VideoContentItem struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *VideoContentURL `json:"image_url,omitempty"`
	VideoURL *VideoContentURL `json:"video_url,omitempty"`
	AudioURL *VideoContentURL `json:"audio_url,omitempty"`
	Role     string           `json:"role,omitempty"`
}

// VideoGenerateRequest 视频生成的标准化请求。
//
// 字段集就是对外契约：请求体里的其他字段由 encoding/json 忽略。
// 供应商侧参数差异（例如 fast_mode 固定为 false）在供应商实现内部消化。
type VideoGenerateRequest struct {
	Model      string             `json:"model"`
	Content    []VideoContentItem `json:"content"`
	Resolution string             `json:"resolution"`
	Duration   int                `json:"duration"`
	Ratio      string             `json:"ratio"`

	// VendorModelID 供应商侧模型 ID，由 controller 按映射关系填充。
	// json:"-" 既让它进不了请求体快照，也保证调用方无法通过请求体顶掉映射。
	VendorModelID string `json:"-"`
}

// Validate 校验视频请求的必填字段与内容项。
//
// 本地先挡住参数错误：上游对这类问题的报错需要一次网络往返，
// 而且提交失败还会走退款/未知态流程，没有必要为一次笔误付出这些代价。
func (r *VideoGenerateRequest) Validate() error {
	if strings.TrimSpace(r.Model) == "" {
		return errors.New("缺少 model 参数")
	}
	if len(r.Content) == 0 {
		return errors.New("缺少 content 参数")
	}

	textCount := 0
	for i, item := range r.Content {
		switch item.Type {
		case VideoContentTypeText:
			if strings.TrimSpace(item.Text) != "" {
				textCount++
			}
		case VideoContentTypeImage, VideoContentTypeVideo, VideoContentTypeAudio:
			var u *VideoContentURL
			switch item.Type {
			case VideoContentTypeImage:
				u = item.ImageURL
			case VideoContentTypeVideo:
				u = item.VideoURL
			case VideoContentTypeAudio:
				u = item.AudioURL
			}
			if u == nil || strings.TrimSpace(u.URL) == "" {
				return fmt.Errorf("content[%d] 缺少 %s.url", i, item.Type)
			}
			if !isHTTPURL(u.URL) {
				return fmt.Errorf("content[%d].%s.url 必须是 http(s) 地址", i, item.Type)
			}
		default:
			return fmt.Errorf("content[%d] 的 type %q 不支持", i, item.Type)
		}
	}

	// 上游要求 prompt 必填：content 中至少一个非空 text 项。
	if textCount == 0 {
		return errors.New("content 至少需要一个非空 text 项")
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
	return nil
}

// isHTTPURL 判断字符串是否为 http/https 绝对地址。
// 内容项 URL 会被上游直接抓取，拒绝 file://、data: 等无法获取或不安全的形态。
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
}

// 结果类型
const (
	ResultKindImage = "image"
	ResultKindVideo = "video"
)

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
