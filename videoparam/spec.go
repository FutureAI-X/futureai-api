// Package videoparam 解析并校验视频生成的参数规格（models.param_spec）。
//
// 与 imageparam 分开：视频的请求字段（duration / ratio / video_ids / audio_ids）、
// 控件（number、ref_audios、ref_videos）和媒体约束（时长、帧率、编码）都不同。
// 两个规格共用 models.param_spec 列，由模型 type 决定用哪个解析器。
package videoparam

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	// MaxSpecBytes 单条规格的大小上限。
	MaxSpecBytes = 64 << 10
	// MaxParams 单条规格的参数数量上限。
	MaxParams = 32
	// MaxOptions 单个参数的选项数量上限。
	MaxOptions = 100

	// 平台全局硬上限。上传探测与模型规格共用同一组常量：
	// 模型规格只能是全局范围的子集，管理端保存时会校验。
	MinDimension       = 64
	MaxDimension       = 8192
	MinAspect          = 0.1
	MaxAspect          = 10.0
	MinDurationSeconds = 1
	MaxDurationSeconds = 300
	MinFPS             = 1.0
	MaxFPS             = 240.0

	// 单文件全局上限。模型规格可以配得更小，但不能更大。
	MaxImageBytes = int64(30) << 20
	MaxAudioBytes = int64(15) << 20
	MaxVideoBytes = int64(200) << 20

	// MaxTotalDurationLimit 总时长上限的合理性边界（24 小时）。
	MaxTotalDurationLimit = 86400

	// DefaultCountMode 字符计数的默认口径：按 Unicode code point 计数。
	DefaultCountMode = "codepoint"
)

// ErrSpecMissing 表示模型尚未配置视频参数规格。
var ErrSpecMissing = errors.New("video param spec missing")

var (
	keyPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	ratioPattern = regexp.MustCompile(`^[1-9]\d{0,3}:[1-9]\d{0,3}$`)
)

// Spec 是 models.param_spec 在视频模型上的结构。
type Spec struct {
	// Version 规格版本，当前为 1。
	Version int `json:"version,omitempty"`
	// Params 参数定义，顺序即客户端展示顺序。
	Params []Param `json:"params"`
}

// Param 是单个参数的定义。
//
// 字段按控件分组；不属于当前控件的字段会在元校验中被拒绝，
// 避免配置看起来生效、实际被忽略。
type Param struct {
	Key      string `json:"key"`
	Path     string `json:"path,omitempty"`
	Widget   string `json:"widget"`
	Label    string `json:"label,omitempty"`
	Hint     string `json:"hint,omitempty"`
	Group    string `json:"group,omitempty"`
	Required bool   `json:"required,omitempty"`

	// ── textarea ──
	MinLength *int   `json:"min_length,omitempty"`
	MaxLength *int   `json:"max_length,omitempty"`
	CountMode string `json:"count_mode,omitempty"`

	// ── segmented ──
	Options []string `json:"options,omitempty"`

	// ── number ──
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Step    *float64 `json:"step,omitempty"`
	Integer bool     `json:"integer,omitempty"`

	// ── 参考媒体公共字段 ──
	MaxCount         *int     `json:"max_count,omitempty"`
	AllowedMimeTypes []string `json:"allowed_mime_types,omitempty"`
	MaxFileBytes     int64    `json:"max_file_bytes,omitempty"`

	// ── 参考图片 / 参考视频 ──
	AspectRatio *RangeFloat `json:"aspect_ratio,omitempty"`
	Dimensions  *RangeInt   `json:"dimensions,omitempty"`

	// ── 参考音频 / 参考视频 ──
	DurationSeconds         *RangeInt `json:"duration_seconds,omitempty"`
	MaxTotalDurationSeconds *int      `json:"max_total_duration_seconds,omitempty"`

	// ── 参考视频 ──
	FPS         *RangeFloat `json:"fps,omitempty"`
	VideoCodecs []string    `json:"video_codecs,omitempty"`
	AudioCodecs []string    `json:"audio_codecs,omitempty"`
}

// RangeInt 是闭区间整数范围。
type RangeInt struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// RangeFloat 是闭区间浮点范围。
type RangeFloat struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

var (
	allowedWidgets = map[string]bool{
		"textarea":   true,
		"segmented":  true,
		"number":     true,
		"ref_images": true,
		"ref_audios": true,
		"ref_videos": true,
	}
	allowedPaths = map[string]bool{
		"prompt":     true,
		"resolution": true,
		"duration":   true,
		"ratio":      true,
		"image_ids":  true,
		"video_ids":  true,
		"audio_ids":  true,
	}
	allowedGroups = map[string]bool{
		"basic":     true,
		"reference": true,
		"advanced":  true,
	}
	allowedCountModes = map[string]bool{
		"codepoint":    true,
		"utf16":        true,
		"gbk_bytes":    true,
		"cjk_weighted": true,
	}
	// 参考媒体允许的 MIME。JPG 与 JPEG 都映射到 image/jpeg。
	mediaMimeAllowlist = map[string]map[string]bool{
		"image": {
			"image/jpeg": true,
			"image/png":  true,
			"image/webp": true,
			"image/heic": true,
			"image/heif": true,
		},
		"audio": {
			"audio/wav":  true,
			"audio/mpeg": true,
		},
		"video": {
			"video/mp4":       true,
			"video/quicktime": true,
		},
	}
	mediaBytesLimit = map[string]int64{
		"image": MaxImageBytes,
		"audio": MaxAudioBytes,
		"video": MaxVideoBytes,
	}
	videoCodecAllowlist = map[string]bool{"h264": true, "hevc": true}
	audioCodecAllowlist = map[string]bool{"aac": true, "mp3": true}
)

// ParseSpec 解析并执行元校验。raw 为空或 null 时返回 ErrSpecMissing。
func ParseSpec(raw string) (*Spec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, ErrSpecMissing
	}
	if len(raw) > MaxSpecBytes {
		return nil, fmt.Errorf("param_spec 不能超过 %d 字节", MaxSpecBytes)
	}

	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var s Spec
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("param_spec 解析失败: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("param_spec 只能包含一个 JSON 对象")
	}
	if err := s.ValidateSpec(); err != nil {
		return nil, err
	}
	return &s, nil
}

// ParamByPath 按请求字段路径查找参数。返回 nil 表示规格未声明该字段。
func (s *Spec) ParamByPath(path string) *Param {
	if s == nil {
		return nil
	}
	for i := range s.Params {
		if s.Params[i].Path == path {
			return &s.Params[i]
		}
	}
	return nil
}

// ResolutionOptions 返回规格声明的分辨率档位（已归一化为大写）。
func (s *Spec) ResolutionOptions() []string {
	p := s.ParamByPath("resolution")
	if p == nil {
		return nil
	}
	return append([]string(nil), p.Options...)
}

// ValidateSpec 校验规格本身的结构与语义。
func (s *Spec) ValidateSpec() error {
	if s == nil || len(s.Params) == 0 {
		return errors.New("params 不能为空")
	}
	if len(s.Params) > MaxParams {
		return fmt.Errorf("参数数量不能超过 %d", MaxParams)
	}
	if s.Version != 0 && s.Version != 1 {
		return fmt.Errorf("不支持的规格版本 %d", s.Version)
	}

	keys := make(map[string]bool, len(s.Params))
	paths := make(map[string]bool, len(s.Params))

	for i := range s.Params {
		p := &s.Params[i]
		if !keyPattern.MatchString(p.Key) {
			return fmt.Errorf("参数 key %q 不合法", p.Key)
		}
		if keys[p.Key] {
			return fmt.Errorf("参数 key %q 重复", p.Key)
		}
		keys[p.Key] = true

		if p.Path == "" {
			p.Path = p.Key
		}
		if !allowedPaths[p.Path] {
			return fmt.Errorf("参数 %s 的 path %q 暂不支持", p.Key, p.Path)
		}
		if paths[p.Path] {
			return fmt.Errorf("参数 path %q 重复", p.Path)
		}
		paths[p.Path] = true

		if !allowedWidgets[p.Widget] {
			return fmt.Errorf("参数 %s 的 widget %q 暂不支持", p.Key, p.Widget)
		}
		if p.Group == "" {
			p.Group = "basic"
		}
		if !allowedGroups[p.Group] {
			return fmt.Errorf("参数 %s 的 group %q 不合法", p.Key, p.Group)
		}

		switch p.Widget {
		case "textarea":
			if p.Path != "prompt" {
				return fmt.Errorf("参数 %s: textarea 的 path 只能是 prompt", p.Key)
			}
			if err := validateTextSpec(p); err != nil {
				return fmt.Errorf("参数 %s: %w", p.Key, err)
			}
		case "segmented":
			if p.Path != "resolution" && p.Path != "ratio" {
				return fmt.Errorf("参数 %s: segmented 的 path 只能是 resolution 或 ratio", p.Key)
			}
			if err := validateSegmentedSpec(p); err != nil {
				return fmt.Errorf("参数 %s: %w", p.Key, err)
			}
		case "number":
			if p.Path != "duration" {
				return fmt.Errorf("参数 %s: number 的 path 只能是 duration", p.Key)
			}
			if err := validateNumberSpec(p); err != nil {
				return fmt.Errorf("参数 %s: %w", p.Key, err)
			}
		case "ref_images":
			if p.Path != "image_ids" {
				return fmt.Errorf("参数 %s: ref_images 的 path 只能是 image_ids", p.Key)
			}
			if err := validateRefMediaSpec(p, "image"); err != nil {
				return fmt.Errorf("参数 %s: %w", p.Key, err)
			}
		case "ref_audios":
			if p.Path != "audio_ids" {
				return fmt.Errorf("参数 %s: ref_audios 的 path 只能是 audio_ids", p.Key)
			}
			if err := validateRefMediaSpec(p, "audio"); err != nil {
				return fmt.Errorf("参数 %s: %w", p.Key, err)
			}
		case "ref_videos":
			if p.Path != "video_ids" {
				return fmt.Errorf("参数 %s: ref_videos 的 path 只能是 video_ids", p.Key)
			}
			if err := validateRefMediaSpec(p, "video"); err != nil {
				return fmt.Errorf("参数 %s: %w", p.Key, err)
			}
		}
	}

	// 四个标量参数是视频请求的硬性组成：必须有且标记为必填。
	// 否则规格少写一个字段，运行时就会跳过对应校验，形成静默的配置漏洞。
	for _, item := range []struct {
		path   string
		widget string
	}{
		{"prompt", "textarea"},
		{"resolution", "segmented"},
		{"duration", "number"},
		{"ratio", "segmented"},
	} {
		p := s.ParamByPath(item.path)
		if p == nil {
			return fmt.Errorf("缺少必填参数 %s", item.path)
		}
		if p.Widget != item.widget {
			return fmt.Errorf("参数 %s 必须使用 %s 控件", item.path, item.widget)
		}
		if !p.Required {
			return fmt.Errorf("参数 %s 必须设置为必填", item.path)
		}
	}
	return nil
}

func validateTextSpec(p *Param) error {
	if p.MinLength != nil && *p.MinLength < 0 {
		return errors.New("min_length 不能为负数")
	}
	if p.MaxLength != nil && *p.MaxLength <= 0 {
		return errors.New("max_length 必须大于 0")
	}
	if p.MinLength != nil && p.MaxLength != nil && *p.MinLength > *p.MaxLength {
		return errors.New("min_length 不能大于 max_length")
	}
	if p.CountMode == "" {
		p.CountMode = DefaultCountMode
	}
	if !allowedCountModes[p.CountMode] {
		return errors.New("count_mode 只能是 codepoint / utf16 / gbk_bytes / cjk_weighted")
	}
	if len(p.Options) > 0 {
		return errors.New("textarea 不支持 options")
	}
	return nil
}

func validateSegmentedSpec(p *Param) error {
	if len(p.Options) == 0 {
		return errors.New("需要 options")
	}
	if len(p.Options) > MaxOptions {
		return fmt.Errorf("options 不能超过 %d 个", MaxOptions)
	}

	seen := make(map[string]bool, len(p.Options))
	for i := range p.Options {
		value := normalizeOption(p.Path, p.Options[i])
		if value == "" {
			return errors.New("option 不能为空")
		}
		if p.Path == "ratio" && value != "adaptive" && !ratioPattern.MatchString(value) {
			return fmt.Errorf("ratio 选项 %q 必须是「宽:高」或 adaptive", p.Options[i])
		}
		if seen[value] {
			return fmt.Errorf("option %q 重复", value)
		}
		seen[value] = true
		p.Options[i] = value
	}
	return nil
}

func validateNumberSpec(p *Param) error {
	if p.Min == nil || p.Max == nil {
		return errors.New("duration 必须同时设置 min 与 max")
	}
	if !p.Integer {
		return errors.New("duration 必须设置 integer: true")
	}
	if *p.Min < MinDurationSeconds || *p.Max > MaxDurationSeconds {
		return fmt.Errorf("duration 范围必须在 %d-%d 秒", MinDurationSeconds, MaxDurationSeconds)
	}
	if *p.Min > *p.Max {
		return errors.New("min 不能大于 max")
	}
	if *p.Min != float64(int(*p.Min)) || *p.Max != float64(int(*p.Max)) {
		return errors.New("duration 的 min/max 必须是整数")
	}
	if p.Step != nil && (*p.Step <= 0 || *p.Step != float64(int(*p.Step))) {
		return errors.New("step 必须是正整数")
	}
	if len(p.Options) > 0 {
		return errors.New("number 不支持 options")
	}
	return nil
}

func validateRefMediaSpec(p *Param, mediaType string) error {
	if p.Required {
		return errors.New("参考媒体是可选输入，不能设置为必填")
	}
	if p.MaxCount == nil {
		return errors.New("max_count 必填（0 表示不支持）")
	}
	if *p.MaxCount < 0 {
		return errors.New("max_count 不能为负数")
	}
	if *p.MaxCount == 0 {
		// 不支持该媒体：其他约束没有意义，忽略。
		return nil
	}

	if len(p.AllowedMimeTypes) == 0 {
		return errors.New("max_count > 0 时必须配置 allowed_mime_types")
	}
	allowedMimes := mediaMimeAllowlist[mediaType]
	seenMime := make(map[string]bool, len(p.AllowedMimeTypes))
	for i := range p.AllowedMimeTypes {
		mime := strings.ToLower(strings.TrimSpace(p.AllowedMimeTypes[i]))
		if !allowedMimes[mime] {
			return fmt.Errorf("不支持的 MIME 类型 %q", p.AllowedMimeTypes[i])
		}
		if seenMime[mime] {
			return fmt.Errorf("MIME 类型 %q 重复", mime)
		}
		seenMime[mime] = true
		p.AllowedMimeTypes[i] = mime
	}

	if p.MaxFileBytes < 0 {
		return errors.New("max_file_bytes 不能为负数")
	}
	if p.MaxFileBytes > mediaBytesLimit[mediaType] {
		return fmt.Errorf("max_file_bytes 不能超过全局上限 %d 字节", mediaBytesLimit[mediaType])
	}

	// 按控件拒绝不适用的约束，避免配置看起来生效、实际被忽略。
	switch mediaType {
	case "image":
		if p.DurationSeconds != nil || p.MaxTotalDurationSeconds != nil || p.FPS != nil ||
			len(p.VideoCodecs) > 0 || len(p.AudioCodecs) > 0 {
			return errors.New("ref_images 只支持 aspect_ratio / dimensions")
		}
	case "audio":
		if p.AspectRatio != nil || p.Dimensions != nil || p.FPS != nil ||
			len(p.VideoCodecs) > 0 || len(p.AudioCodecs) > 0 {
			return errors.New("ref_audios 只支持 duration_seconds / max_total_duration_seconds")
		}
	}

	if p.AspectRatio != nil {
		if p.AspectRatio.Min <= 0 || p.AspectRatio.Min > p.AspectRatio.Max {
			return errors.New("aspect_ratio 的 min 必须大于 0 且不大于 max")
		}
		// 全局宽高比上限只约束上传阶段会做检查的视频；图片上传不限制宽高比，
		// 因此图片规格可以按模型能力自行配置。
		if mediaType == "video" && (p.AspectRatio.Min < MinAspect || p.AspectRatio.Max > MaxAspect) {
			return fmt.Errorf("aspect_ratio 必须在 %.1f-%.0f 之间", MinAspect, MaxAspect)
		}
	}
	if p.Dimensions != nil {
		if p.Dimensions.Min <= 0 || p.Dimensions.Min > p.Dimensions.Max {
			return errors.New("dimensions 的 min 必须大于 0 且不大于 max")
		}
		if mediaType == "video" && (p.Dimensions.Min < MinDimension || p.Dimensions.Max > MaxDimension) {
			return fmt.Errorf("dimensions 必须在 %d-%d px 之间", MinDimension, MaxDimension)
		}
	}
	if p.DurationSeconds != nil {
		if p.DurationSeconds.Min < MinDurationSeconds || p.DurationSeconds.Max > MaxDurationSeconds {
			return fmt.Errorf("duration_seconds 必须在 %d-%d 秒之间", MinDurationSeconds, MaxDurationSeconds)
		}
		if p.DurationSeconds.Min <= 0 || p.DurationSeconds.Min > p.DurationSeconds.Max {
			return errors.New("duration_seconds 的 min 必须大于 0 且不大于 max")
		}
	}
	if p.MaxTotalDurationSeconds != nil {
		if *p.MaxTotalDurationSeconds <= 0 || *p.MaxTotalDurationSeconds > MaxTotalDurationLimit {
			return fmt.Errorf("max_total_duration_seconds 必须在 1-%d 秒之间", MaxTotalDurationLimit)
		}
	}
	if p.FPS != nil {
		if p.FPS.Min < MinFPS || p.FPS.Max > MaxFPS {
			return fmt.Errorf("fps 必须在 %.0f-%.0f 之间", MinFPS, MaxFPS)
		}
		if p.FPS.Min <= 0 || p.FPS.Min > p.FPS.Max {
			return errors.New("fps 的 min 必须大于 0 且不大于 max")
		}
	}
	if len(p.VideoCodecs) > 0 {
		if mediaType != "video" {
			return errors.New("video_codecs 只适用于 ref_videos")
		}
		normalized, err := normalizeCodecList(p.VideoCodecs, videoCodecAllowlist, "video_codecs")
		if err != nil {
			return err
		}
		p.VideoCodecs = normalized
	}
	if len(p.AudioCodecs) > 0 {
		if mediaType != "video" {
			return errors.New("audio_codecs 只适用于 ref_videos")
		}
		normalized, err := normalizeCodecList(p.AudioCodecs, audioCodecAllowlist, "audio_codecs")
		if err != nil {
			return err
		}
		p.AudioCodecs = normalized
	}
	return nil
}

func normalizeCodecList(values []string, allowed map[string]bool, field string) ([]string, error) {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		codec := NormalizeCodec(raw)
		if !allowed[codec] {
			return nil, fmt.Errorf("%s 不支持编码 %q", field, raw)
		}
		if seen[codec] {
			continue
		}
		seen[codec] = true
		out = append(out, codec)
	}
	return out, nil
}

// NormalizeCodec 把显示名/别名归一化为平台内部的编码名。
func NormalizeCodec(raw string) string {
	codec := strings.ToLower(strings.TrimSpace(raw))
	codec = strings.Join(strings.Fields(codec), " ")
	switch codec {
	case "h264", "avc", "avc1", "h.264", "h.264/avc", "h264/avc":
		return "h264"
	case "hevc", "h265", "hev1", "hvc1", "h.265", "h.265/hevc", "h265/hevc":
		return "hevc"
	case "aac", "mpeg-4 aac", "mpeg4 aac":
		return "aac"
	case "mp3", "mpeg audio", "mpeg audio layer 3":
		return "mp3"
	default:
		return codec
	}
}

// normalizeOption 统一枚举值的比较口径：分辨率大写，比例保留原样但把 adaptive 转小写。
func normalizeOption(path, value string) string {
	value = strings.TrimSpace(value)
	if path == "resolution" {
		return strings.ToUpper(value)
	}
	if strings.EqualFold(value, "adaptive") {
		return "adaptive"
	}
	return value
}

// NormalizeResolution 统一分辨率入参的大写口径。
func NormalizeResolution(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

// NormalizeRatio 统一画面比例的入参口径。
func NormalizeRatio(value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "adaptive") {
		return "adaptive"
	}
	return value
}
