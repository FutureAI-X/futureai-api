// Package imageparam 解析并校验图像生成的参数规格（models.param_spec）。
//
// 当前阶段只实现现有 5 个模型用到的控件：
// textarea、segmented、ratio_size、pixel_size、ref_images。
// 条件、动态值、emit、命名校验器在规格中会被明确拒绝，防止配置看起来生效、
// 实际运行时却被忽略。
package imageparam

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	// MaxSpecBytes 单条规格的大小上限，与设计文档一致。
	MaxSpecBytes = 64 << 10
	// MaxParams 单条规格的参数数量上限。
	MaxParams = 64
	// MaxOptions 单个参数的选项数量上限。
	MaxOptions = 100
	// MaxRatios 比例集合的数量上限。
	MaxRatios = 50
	// MaxRefImages 计费侧的参考图张数封顶。
	MaxRefImages = 100
	// MaxUploadBytes 上传接口的全局单文件上限（30MB）。
	MaxUploadBytes = int64(30) << 20
)

// ErrSpecMissing 表示模型尚未配置规格。
var ErrSpecMissing = errors.New("param spec missing")

// Spec 是 models.param_spec 的结构。
type Spec struct {
	Params []Param `json:"params"`
}

// Param 是单个参数的定义。
type Param struct {
	Key     string `json:"key"`
	Path    string `json:"path,omitempty"`
	Widget  string `json:"widget"`
	Label   string `json:"label,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Group   string `json:"group,omitempty"`
	Required bool  `json:"required,omitempty"`

	// Default 只支持字面量。动态值对象在 ValidateSpec 中被拒绝。
	Default json.RawMessage `json:"default,omitempty"`

	// 条件与序列化表达式当前不支持。
	VisibleWhen json.RawMessage `json:"visible_when,omitempty"`
	EnabledWhen json.RawMessage `json:"enabled_when,omitempty"`
	Emit        json.RawMessage `json:"emit,omitempty"`

	ConflictsWith []string `json:"conflicts_with,omitempty"`

	Options    []Option `json:"options,omitempty"`
	Multiple   bool     `json:"multiple,omitempty"`
	AllowEmpty bool     `json:"allow_empty,omitempty"`

	MinLength *int   `json:"min_length,omitempty"`
	MaxLength *int   `json:"max_length,omitempty"`
	CountMode string `json:"count_mode,omitempty"`

	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
	Step *float64 `json:"step,omitempty"`

	Modes  []string    `json:"modes,omitempty"`
	Ratios []string    `json:"ratios,omitempty"`
	Pixel  *PixelRule  `json:"pixel,omitempty"`

	Presets     []Preset `json:"presets,omitempty"`
	AllowCustom *bool    `json:"allow_custom,omitempty"`

	MaxCount         *int     `json:"max_count,omitempty"`
	MaxFileBytes     int64    `json:"max_file_bytes,omitempty"`
	MaxTotalBytes    int64    `json:"max_total_bytes,omitempty"`
	AllowedMimeTypes []string `json:"allowed_mime_types,omitempty"`

	// 命名校验器当前不支持。
	Validators []string `json:"validators,omitempty"`
}

// Option 是 segmented 的选项，支持 "value" 与 {"label","value"} 两种写法。
type Option struct {
	Label string
	Value string
}

// UnmarshalJSON 兼容字符串与对象两种选项写法。
func (o *Option) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		o.Label = s
		o.Value = s
		return nil
	}

	var obj struct {
		Label string `json:"label"`
		Value string `json:"value"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&obj); err != nil {
		return fmt.Errorf("option 必须是字符串或 {label,value}: %w", err)
	}
	if obj.Value == "" {
		return errors.New("option.value 不能为空")
	}
	o.Label = obj.Label
	o.Value = obj.Value
	return nil
}

// PixelRule 是像素尺寸约束。字段为 0 表示不限制。
type PixelRule struct {
	Step      int        `json:"step,omitempty"`
	MinSide   int        `json:"min_side,omitempty"`
	MaxSide   int        `json:"max_side,omitempty"`
	MinPixels int        `json:"min_pixels,omitempty"`
	MaxPixels int        `json:"max_pixels,omitempty"`
	Aspect    *AspectRule `json:"aspect,omitempty"`
}

// AspectRule 是宽高比范围，闭区间。
type AspectRule struct {
	Min string `json:"min,omitempty"`
	Max string `json:"max,omitempty"`
}

// Preset 是 pixel_size 的比例快捷尺寸。
// ratio 用于比例按钮，pixels 是该比例对应的默认像素值。
type Preset struct {
	Ratio  string `json:"ratio"`
	Pixels string `json:"pixels"`
}

var (
	allowedWidgets = map[string]bool{
		"textarea":   true,
		"segmented":  true,
		"ratio_size": true,
		"pixel_size": true,
		"ref_images": true,
	}
	allowedPaths = map[string]bool{
		"prompt":        true,
		"size":          true,
		"resolution":    true,
		"quality":       true,
		"output_format": true,
		"complexity":    true,
		"image_ids":     true,
		"image_urls":    true,
	}
	// allowedSegmentedPaths 是 segmented 控件可以映射的请求字段。
	// 新增枚举型参数时在这里登记，避免把控件误配到 size 这类非枚举字段。
	allowedSegmentedPaths = map[string]bool{
		"resolution":    true,
		"quality":       true,
		"output_format": true,
		"complexity":    true,
	}
	allowedGroups = map[string]bool{
		"basic":     true,
		"reference": true,
		"advanced":  true,
	}
	allowedMimeTypes = map[string]bool{
		"image/png":  true,
		"image/jpeg": true,
		"image/webp": true,
	}
	keyPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	ratioPattern = regexp.MustCompile(`^([1-9]\d{0,3}):([1-9]\d{0,3})$`)
	pixelPattern = regexp.MustCompile(`^([1-9]\d{0,3})[xX]([1-9]\d{0,3})$`)
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

// ValidateSpec 校验规格本身的结构与语义。
func (s *Spec) ValidateSpec() error {
	if s == nil || len(s.Params) == 0 {
		return errors.New("params 不能为空")
	}
	if len(s.Params) > MaxParams {
		return fmt.Errorf("参数数量不能超过 %d", MaxParams)
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
		// 当前阶段控件与请求字段一一对应，避免把 textarea 映射到 size 这类误配。
		switch p.Widget {
		case "textarea":
			if p.Path != "prompt" {
				return fmt.Errorf("参数 %s: textarea 的 path 只能是 prompt", p.Key)
			}
		case "segmented":
			if !allowedSegmentedPaths[p.Path] {
				return fmt.Errorf("参数 %s: segmented 的 path 只能是 resolution / quality / output_format / complexity", p.Key)
			}
		case "ratio_size", "pixel_size":
			if p.Path != "size" {
				return fmt.Errorf("参数 %s: %s 的 path 只能是 size", p.Key, p.Widget)
			}
		case "ref_images":
			if p.Path != "image_ids" && p.Path != "image_urls" {
				return fmt.Errorf("参数 %s: ref_images 的 path 只能是 image_ids 或 image_urls", p.Key)
			}
		}
		if p.Required && hasJSONValue(p.Default) {
			return fmt.Errorf("参数 %s 不能同时声明 required 与 default", p.Key)
		}
		if hasJSONValue(p.VisibleWhen) {
			return fmt.Errorf("参数 %s 的 visible_when 暂不支持", p.Key)
		}
		if hasJSONValue(p.EnabledWhen) {
			return fmt.Errorf("参数 %s 的 enabled_when 暂不支持", p.Key)
		}
		if hasJSONValue(p.Emit) {
			return fmt.Errorf("参数 %s 的 emit 暂不支持", p.Key)
		}
		if len(p.Validators) > 0 {
			return fmt.Errorf("参数 %s 的 validators 暂不支持", p.Key)
		}

		def, hasDefault, err := defaultString(p)
		if err != nil {
			return fmt.Errorf("参数 %s: %w", p.Key, err)
		}

		switch p.Widget {
		case "textarea":
			if err := validateText(p); err != nil {
				return fmt.Errorf("参数 %s: %w", p.Key, err)
			}
		case "segmented":
			if err := validateSegmented(p, def, hasDefault); err != nil {
				return fmt.Errorf("参数 %s: %w", p.Key, err)
			}
		case "ratio_size":
			if err := validateRatioSize(p, def, hasDefault); err != nil {
				return fmt.Errorf("参数 %s: %w", p.Key, err)
			}
		case "pixel_size":
			if err := validatePixelSize(p, def, hasDefault); err != nil {
				return fmt.Errorf("参数 %s: %w", p.Key, err)
			}
		case "ref_images":
			if err := validateRefImages(p); err != nil {
				return fmt.Errorf("参数 %s: %w", p.Key, err)
			}
		}
	}

	for i := range s.Params {
		for _, name := range s.Params[i].ConflictsWith {
			if name == s.Params[i].Key {
				return fmt.Errorf("参数 %s 不能与自己互斥", s.Params[i].Key)
			}
			if !keys[name] {
				return fmt.Errorf("参数 %s 的 conflicts_with 引用了不存在的 key %q", s.Params[i].Key, name)
			}
		}
	}
	return nil
}

// hasJSONValue 判断 RawMessage 是否包含一个非 null 的值。
func hasJSONValue(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null"
}

func defaultString(p *Param) (string, bool, error) {
	raw := bytes.TrimSpace(p.Default)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false, fmt.Errorf("default 不是合法 JSON: %w", err)
	}
	switch val := v.(type) {
	case string:
		return val, true, nil
	case map[string]any:
		return "", false, errors.New("动态值暂不支持")
	default:
		return "", false, errors.New("default 必须是字符串")
	}
}

func validateText(p *Param) error {
	if p.MinLength != nil && *p.MinLength < 0 {
		return errors.New("min_length 不能为负数")
	}
	if p.MaxLength != nil && *p.MaxLength < 0 {
		return errors.New("max_length 不能为负数")
	}
	if p.MinLength != nil && p.MaxLength != nil && *p.MinLength > *p.MaxLength {
		return errors.New("min_length 不能大于 max_length")
	}
	if p.CountMode == "" {
		p.CountMode = "codepoint"
	}
	if p.CountMode != "codepoint" && p.CountMode != "utf16" {
		return errors.New("count_mode 只能是 codepoint 或 utf16")
	}
	if len(p.Options) > 0 {
		return errors.New("textarea 不支持 options")
	}
	return nil
}

func validateSegmented(p *Param, def string, hasDefault bool) error {
	if len(p.Options) == 0 {
		return errors.New("segmented 需要 options")
	}
	if len(p.Options) > MaxOptions {
		return fmt.Errorf("options 不能超过 %d 个", MaxOptions)
	}
	seen := make(map[string]bool, len(p.Options))
	for _, opt := range p.Options {
		if opt.Value == "" && !p.AllowEmpty {
			return errors.New("option value 不能为空")
		}
		if seen[opt.Value] {
			return fmt.Errorf("option value %q 重复", opt.Value)
		}
		seen[opt.Value] = true
	}
	if hasDefault && !seen[def] {
		return fmt.Errorf("default %q 不在 options 中", def)
	}
	return nil
}

func validateRatioSize(p *Param, def string, hasDefault bool) error {
	if len(p.Ratios) == 0 {
		return errors.New("ratio_size 需要 ratios")
	}
	if len(p.Ratios) > MaxRatios {
		return fmt.Errorf("ratios 不能超过 %d 个", MaxRatios)
	}
	if p.Modes == nil {
		p.Modes = []string{"ratio"}
	}
	if len(p.Modes) == 0 {
		return errors.New("modes 不能为空")
	}
	modeSet := make(map[string]bool, len(p.Modes))
	for _, m := range p.Modes {
		if m != "ratio" && m != "pixel" {
			return fmt.Errorf("modes 只能包含 ratio 或 pixel，收到 %q", m)
		}
		if modeSet[m] {
			return fmt.Errorf("modes 中 %q 重复", m)
		}
		modeSet[m] = true
	}
	if !modeSet["ratio"] {
		return errors.New("ratio_size 的 modes 必须包含 ratio")
	}
	if modeSet["pixel"] {
		if p.Pixel == nil {
			return errors.New("modes 包含 pixel 时必须配置 pixel")
		}
	} else if p.Pixel != nil {
		return errors.New("modes 不包含 pixel 时不能配置 pixel")
	}
	if p.Pixel != nil {
		if err := validatePixelRule(p.Pixel); err != nil {
			return err
		}
	}

	seen := make(map[string]bool, len(p.Ratios))
	for _, r := range p.Ratios {
		if !ratioPattern.MatchString(r) {
			return fmt.Errorf("比例 %q 格式不合法", r)
		}
		if seen[r] {
			return fmt.Errorf("比例 %q 重复", r)
		}
		seen[r] = true
	}
	if hasDefault && !seen[def] {
		return fmt.Errorf("default %q 不在 ratios 中", def)
	}
	return nil
}

func validatePixelSize(p *Param, def string, hasDefault bool) error {
	if p.Pixel == nil {
		return errors.New("pixel_size 需要 pixel")
	}
	if len(p.Modes) > 0 {
		if len(p.Modes) != 1 || p.Modes[0] != "pixel" {
			return errors.New("pixel_size 的 modes 只能是 [\"pixel\"]")
		}
	}
	if len(p.Ratios) > 0 {
		return errors.New("pixel_size 不能配置 ratios")
	}
	if err := validatePixelRule(p.Pixel); err != nil {
		return err
	}

	seenPixels := make(map[string]bool, len(p.Presets))
	seenRatios := make(map[string]bool, len(p.Presets))
	for _, preset := range p.Presets {
		if preset.Ratio == "" {
			return fmt.Errorf("预设 %q 缺少 ratio", preset.Pixels)
		}
		if !ratioPattern.MatchString(preset.Ratio) {
			return fmt.Errorf("预设比例 %q 格式不合法", preset.Ratio)
		}
		if _, _, err := parsePixel(preset.Pixels); err != nil {
			return fmt.Errorf("预设 %q: %w", preset.Pixels, err)
		}
		if err := validatePixelValue(p.Pixel, preset.Pixels); err != nil {
			return fmt.Errorf("预设 %q: %w", preset.Pixels, err)
		}
		if !ratioMatchesPixels(preset.Ratio, preset.Pixels) {
			return fmt.Errorf("预设 %q 与比例 %s 不匹配", preset.Pixels, preset.Ratio)
		}
		if seenPixels[preset.Pixels] {
			return fmt.Errorf("预设 %q 重复", preset.Pixels)
		}
		if seenRatios[preset.Ratio] {
			return fmt.Errorf("预设比例 %q 重复", preset.Ratio)
		}
		seenPixels[preset.Pixels] = true
		seenRatios[preset.Ratio] = true
	}
	if p.AllowCustom == nil {
		v := true
		p.AllowCustom = &v
	}
	if !*p.AllowCustom {
		if len(p.Presets) == 0 {
			return errors.New("allow_custom=false 时至少需要一个预设")
		}
		if hasDefault && !seenPixels[def] {
			return fmt.Errorf("allow_custom=false 时 default %q 必须在预设中", def)
		}
	}
	if p.Required && !hasDefault && len(p.Presets) == 0 {
		return errors.New("必填的 pixel_size 需要至少一个预设或 default")
	}
	if hasDefault {
		if err := validatePixelValue(p.Pixel, def); err != nil {
			return fmt.Errorf("default %q: %w", def, err)
		}
	}
	return nil
}

func validatePixelRule(rule *PixelRule) error {
	if rule == nil {
		return errors.New("pixel 不能为空")
	}
	if rule.Step < 0 || rule.MinSide < 0 || rule.MaxSide < 0 ||
		rule.MinPixels < 0 || rule.MaxPixels < 0 {
		return errors.New("pixel 的数值不能为负数")
	}
	if rule.MaxSide > 0 && rule.MinSide > rule.MaxSide {
		return errors.New("pixel.min_side 不能大于 max_side")
	}
	if rule.MaxPixels > 0 && rule.MinPixels > rule.MaxPixels {
		return errors.New("pixel.min_pixels 不能大于 max_pixels")
	}
	if rule.Aspect != nil {
		if rule.Aspect.Min != "" && !ratioPattern.MatchString(rule.Aspect.Min) {
			return fmt.Errorf("aspect.min %q 格式不合法", rule.Aspect.Min)
		}
		if rule.Aspect.Max != "" && !ratioPattern.MatchString(rule.Aspect.Max) {
			return fmt.Errorf("aspect.max %q 格式不合法", rule.Aspect.Max)
		}
		if rule.Aspect.Min != "" && rule.Aspect.Max != "" && !ratioLE(rule.Aspect.Min, rule.Aspect.Max) {
			return errors.New("aspect.min 不能大于 aspect.max")
		}
	}
	return nil
}

func validateRefImages(p *Param) error {
	if p.MaxCount == nil {
		return errors.New("ref_images 需要 max_count")
	}
	if *p.MaxCount < 0 {
		return errors.New("max_count 不能为负数")
	}
	if p.Path != "image_ids" && p.Path != "image_urls" {
		return errors.New("ref_images 的 path 只能是 image_ids 或 image_urls")
	}
	if hasJSONValue(p.Default) {
		return errors.New("ref_images 不支持 default")
	}
	if *p.MaxCount == 0 {
		if p.MaxFileBytes != 0 || p.MaxTotalBytes != 0 || len(p.AllowedMimeTypes) > 0 {
			return errors.New("max_count=0 时不能配置大小或 MIME 限制")
		}
		return nil
	}
	if *p.MaxCount > MaxRefImages {
		return fmt.Errorf("max_count 不能超过 %d", MaxRefImages)
	}
	if p.MaxFileBytes <= 0 {
		return errors.New("max_file_bytes 必须大于 0")
	}
	if p.MaxFileBytes > MaxUploadBytes {
		return fmt.Errorf("max_file_bytes 不能超过 %d", MaxUploadBytes)
	}
	if p.MaxTotalBytes < p.MaxFileBytes {
		return errors.New("max_total_bytes 不能小于 max_file_bytes")
	}
	if len(p.AllowedMimeTypes) == 0 {
		return errors.New("allowed_mime_types 不能为空")
	}
	for _, mime := range p.AllowedMimeTypes {
		if !allowedMimeTypes[strings.ToLower(mime)] {
			return fmt.Errorf("allowed_mime_types 不支持 %q", mime)
		}
	}
	return nil
}

// HasKey 判断规格中是否存在指定 key 的参数。
func (s *Spec) HasKey(key string) bool {
	if s == nil {
		return false
	}
	for i := range s.Params {
		if s.Params[i].Key == key {
			return true
		}
	}
	return false
}

// HasPath 判断规格中是否存在映射到指定请求字段路径的参数。
// 规格允许 key 与 path 不同（例如 key=image_quality、path=quality），
// 请求字段是否存在必须按 path 判断，不能按 key 判断。
func (s *Spec) HasPath(path string) bool {
	if s == nil {
		return false
	}
	for i := range s.Params {
		if s.Params[i].Path == path {
			return true
		}
	}
	return false
}

// RefImagesRule 是 ref_images 的运行时规则。
type RefImagesRule struct {
	Key              string
	Path             string
	MaxCount         int
	MaxFileBytes     int64
	MaxTotalBytes    int64
	AllowedMimeTypes []string
}

// RefImagesRule 返回规格中的参考图规则；没有该控件时返回 nil。
func (s *Spec) RefImagesRule() *RefImagesRule {
	if s == nil {
		return nil
	}
	for i := range s.Params {
		p := &s.Params[i]
		if p.Widget != "ref_images" || p.MaxCount == nil {
			continue
		}
		return &RefImagesRule{
			Key:              p.Key,
			Path:             p.Path,
			MaxCount:         *p.MaxCount,
			MaxFileBytes:     p.MaxFileBytes,
			MaxTotalBytes:    p.MaxTotalBytes,
			AllowedMimeTypes: p.AllowedMimeTypes,
		}
	}
	return nil
}
