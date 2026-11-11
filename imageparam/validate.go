package imageparam

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Error 是对调用方可见的参数错误。
type Error struct {
	Code    string
	Param   string
	Message string
}

func (e *Error) Error() string { return e.Message }

func newError(code, param, format string, args ...any) *Error {
	return &Error{Code: code, Param: param, Message: fmt.Sprintf(format, args...)}
}

// Values 是请求参数值，键为参数 path。
type Values map[string]any

// RefFile 是已解析的参考图文件元数据。
type RefFile struct {
	ID          string
	Bytes       int64
	ContentType string
}

// ApplyDefaults 为省略的参数补齐默认值，并检查必填项。
// 必须在参数校验与计费之前调用。
func (s *Spec) ApplyDefaults(v Values) *Error {
	if s == nil {
		return nil
	}
	if v == nil {
		return newError("PARAM_SPEC_INVALID", "", "参数集合不能为空")
	}
	for i := range s.Params {
		p := &s.Params[i]
		cur, _ := v[p.Path]
		if !isEmptyValue(cur) {
			continue
		}
		if def, ok, err := defaultString(p); err == nil && ok {
			v[p.Path] = def
			continue
		}
		if p.Required {
			return newError("PARAM_REQUIRED", p.Key, "%s 不能为空", displayName(p))
		}
	}
	return nil
}

// Validate 校验参数值。调用方必须先把有效的参考图数组放到 image_ids/image_urls。
func (s *Spec) Validate(v Values) *Error {
	if s == nil {
		return nil
	}
	if v == nil {
		return newError("PARAM_SPEC_INVALID", "", "参数集合不能为空")
	}
	for i := range s.Params {
		p := &s.Params[i]
		cur, _ := v[p.Path]
		if isEmptyValue(cur) {
			if p.Required {
				return newError("PARAM_REQUIRED", p.Key, "%s 不能为空", displayName(p))
			}
			continue
		}
		switch p.Widget {
		case "textarea":
			if err := validateTextValue(p, cur); err != nil {
				return err
			}
		case "segmented":
			if err := validateSegmentedValue(p, cur); err != nil {
				return err
			}
		case "ratio_size":
			if err := validateRatioSizeValue(p, cur); err != nil {
				return err
			}
		case "pixel_size":
			if err := validatePixelSizeValue(p, cur); err != nil {
				return err
			}
		case "ref_images":
			if err := validateRefCount(p, v); err != nil {
				return err
			}
		}
	}

	for i := range s.Params {
		p := &s.Params[i]
		if isEmptyValue(v[p.Path]) {
			continue
		}
		for _, otherKey := range p.ConflictsWith {
			other := s.paramByKey(otherKey)
			if other != nil && !isEmptyValue(v[other.Path]) {
				return newError("PARAM_CONFLICT", p.Key, "%s 与 %s 不能同时使用", displayName(p), displayName(other))
			}
		}
	}
	return nil
}

// ValidateRefFiles 校验已解析参考图的大小与 MIME。
// 只对平台 file_id 生效；外部 URL 无法在不下载的前提下确认大小与类型。
func ValidateRefFiles(rule *RefImagesRule, files []RefFile) *Error {
	if rule == nil || rule.MaxCount <= 0 {
		return nil
	}
	allowed := make(map[string]bool, len(rule.AllowedMimeTypes))
	for _, mime := range rule.AllowedMimeTypes {
		allowed[strings.ToLower(strings.TrimSpace(mime))] = true
	}

	var total int64
	for _, f := range files {
		if rule.MaxFileBytes > 0 && f.Bytes > rule.MaxFileBytes {
			return newError("PARAM_REF_FILE_TOO_LARGE", rule.Key,
				"参考图 %s 超过单张上限 %d 字节", f.ID, rule.MaxFileBytes)
		}
		if len(allowed) > 0 && !allowed[strings.ToLower(strings.TrimSpace(f.ContentType))] {
			return newError("PARAM_REF_TYPE_UNSUPPORTED", rule.Key,
				"参考图 %s 的类型 %s 不受支持", f.ID, f.ContentType)
		}
		total += f.Bytes
		if rule.MaxTotalBytes > 0 && total > rule.MaxTotalBytes {
			return newError("PARAM_REF_TOTAL_TOO_LARGE", rule.Key,
				"参考图合计大小超过上限 %d 字节", rule.MaxTotalBytes)
		}
	}
	return nil
}

func (s *Spec) paramByKey(key string) *Param {
	for i := range s.Params {
		if s.Params[i].Key == key {
			return &s.Params[i]
		}
	}
	return nil
}

func validateTextValue(p *Param, cur any) *Error {
	s, ok := cur.(string)
	if !ok {
		return newError("PARAM_TYPE", p.Key, "%s 必须是字符串", displayName(p))
	}
	if strings.TrimSpace(s) == "" {
		if p.Required {
			return newError("PARAM_REQUIRED", p.Key, "%s 不能为空", displayName(p))
		}
		if !p.AllowEmpty {
			return nil
		}
	}
	n := countChars(s, p.CountMode)
	if p.MinLength != nil && n < *p.MinLength {
		return newError("PARAM_TOO_SHORT", p.Key, "%s 不能少于 %d 个字符", displayName(p), *p.MinLength)
	}
	if p.MaxLength != nil && n > *p.MaxLength {
		return newError("PARAM_TOO_LONG", p.Key, "%s 不能超过 %d 个字符", displayName(p), *p.MaxLength)
	}
	return nil
}

func validateSegmentedValue(p *Param, cur any) *Error {
	s, ok := cur.(string)
	if !ok {
		return newError("PARAM_TYPE", p.Key, "%s 必须是字符串", displayName(p))
	}
	if s == "" && p.AllowEmpty {
		return nil
	}
	for _, opt := range p.Options {
		if opt.Value == s {
			return nil
		}
	}
	return newError("PARAM_UNSUPPORTED_VALUE", p.Key, "%s 只支持 %s", displayName(p), optionValues(p.Options))
}

func validateRatioSizeValue(p *Param, cur any) *Error {
	raw, ok := cur.(string)
	if !ok {
		return newError("PARAM_TYPE", p.Key, "%s 必须是字符串", displayName(p))
	}
	raw = strings.TrimSpace(raw)
	// auto 不是比例也不是像素，是否可用只看规格里的 ratios 是否配置了它。
	if raw == sizeAuto {
		if !contains(p.Ratios, sizeAuto) {
			return newError("PARAM_SIZE_RATIO_UNSUPPORTED", p.Key, "size 只支持 %s", strings.Join(p.Ratios, "、"))
		}
		return nil
	}
	if w, h, ok := parseRatio(raw); ok {
		if !contains(p.Modes, "ratio") {
			return newError("PARAM_SIZE_MODE_UNSUPPORTED", p.Key, "该模型的 size 只支持像素尺寸")
		}
		if !contains(p.Ratios, raw) {
			return newError("PARAM_SIZE_RATIO_UNSUPPORTED", p.Key, "size 只支持 %s", strings.Join(p.Ratios, "、"))
		}
		_ = w
		_ = h
		return nil
	}
	if w, h, err := parsePixel(raw); err == nil {
		if !contains(p.Modes, "pixel") {
			return newError("PARAM_SIZE_MODE_UNSUPPORTED", p.Key, "该模型的 size 只支持比例")
		}
		if err := validatePixelValue(p.Pixel, raw); err != nil {
			return pixelError(p.Key, err)
		}
		_ = w
		_ = h
		return nil
	}
	return newError("PARAM_SIZE_FORMAT", p.Key, "size 必须是「宽:高」比例或「宽x高」像素尺寸")
}

func validatePixelSizeValue(p *Param, cur any) *Error {
	raw, ok := cur.(string)
	if !ok {
		return newError("PARAM_TYPE", p.Key, "%s 必须是字符串", displayName(p))
	}
	raw = strings.TrimSpace(raw)
	if _, _, err := parsePixel(raw); err != nil {
		return newError("PARAM_SIZE_FORMAT", p.Key, "size 必须是「宽x高」像素尺寸")
	}
	if err := validatePixelValue(p.Pixel, raw); err != nil {
		return pixelError(p.Key, err)
	}
	if p.AllowCustom != nil && !*p.AllowCustom {
		for _, preset := range p.Presets {
			if preset.Pixels == normalizePixel(raw) {
				return nil
			}
		}
		return newError("PARAM_UNSUPPORTED_VALUE", p.Key, "size 只能从预设中选择")
	}
	return nil
}

func validateRefCount(p *Param, v Values) *Error {
	if p.MaxCount == nil || *p.MaxCount <= 0 {
		return nil
	}
	refs := effectiveRefs(v)
	if len(refs) > *p.MaxCount {
		return newError("PARAM_REF_TOO_MANY", p.Key, "参考图最多 %d 张", *p.MaxCount)
	}
	return nil
}

func effectiveRefs(v Values) []string {
	ids := dedupe(valueStrings(v["image_ids"]))
	if len(ids) > 0 {
		return ids
	}
	return dedupe(valueStrings(v["image_urls"]))
}

func pixelError(param string, err error) *Error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "倍数"):
		return newError("PARAM_SIZE_PIXEL_STEP", param, "%s", msg)
	case strings.Contains(msg, "最小边"):
		return newError("PARAM_SIZE_PIXEL_MIN_SIDE", param, "%s", msg)
	case strings.Contains(msg, "最大边"):
		return newError("PARAM_SIZE_PIXEL_MAX_SIDE", param, "%s", msg)
	case strings.Contains(msg, "总像素下限"):
		return newError("PARAM_SIZE_PIXEL_MIN_PIXELS", param, "%s", msg)
	case strings.Contains(msg, "总像素上限"):
		return newError("PARAM_SIZE_PIXEL_MAX_PIXELS", param, "%s", msg)
	case strings.Contains(msg, "宽高比"):
		return newError("PARAM_SIZE_PIXEL_ASPECT", param, "%s", msg)
	default:
		return newError("PARAM_SIZE_PIXEL_STEP", param, "%s", msg)
	}
}

func validatePixelValue(rule *PixelRule, raw string) error {
	if rule == nil {
		return nil
	}
	w, h, err := parsePixel(raw)
	if err != nil {
		return err
	}
	if rule.Step > 0 && (w%rule.Step != 0 || h%rule.Step != 0) {
		return fmt.Errorf("size 的宽高必须是 %d 的倍数", rule.Step)
	}
	if rule.MinSide > 0 && (w < rule.MinSide || h < rule.MinSide) {
		return fmt.Errorf("size 的最小边不能小于 %d", rule.MinSide)
	}
	if rule.MaxSide > 0 && (w > rule.MaxSide || h > rule.MaxSide) {
		return fmt.Errorf("size 的最大边不能超过 %d", rule.MaxSide)
	}
	pixels := w * h
	if rule.MinPixels > 0 && pixels < rule.MinPixels {
		return fmt.Errorf("size 总像素下限为 %d", rule.MinPixels)
	}
	if rule.MaxPixels > 0 && pixels > rule.MaxPixels {
		return fmt.Errorf("size 总像素上限为 %d", rule.MaxPixels)
	}
	if rule.Aspect != nil {
		if rule.Aspect.Min != "" && !ratioPixelsGE(raw, rule.Aspect.Min) {
			return fmt.Errorf("size 宽高比不能小于 %s", rule.Aspect.Min)
		}
		if rule.Aspect.Max != "" && !ratioPixelsLE(raw, rule.Aspect.Max) {
			return fmt.Errorf("size 宽高比不能大于 %s", rule.Aspect.Max)
		}
	}
	return nil
}

func parseRatio(raw string) (int, int, bool) {
	m := ratioPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return 0, 0, false
	}
	w, _ := strconv.Atoi(m[1])
	h, _ := strconv.Atoi(m[2])
	return w, h, true
}

func parsePixel(raw string) (int, int, error) {
	m := pixelPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return 0, 0, fmt.Errorf("不是合法的像素尺寸")
	}
	w, _ := strconv.Atoi(m[1])
	h, _ := strconv.Atoi(m[2])
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("宽高必须是正整数")
	}
	return w, h, nil
}

func normalizePixel(raw string) string {
	w, h, err := parsePixel(raw)
	if err != nil {
		return raw
	}
	return fmt.Sprintf("%dx%d", w, h)
}

func ratioMatchesPixels(ratio, pixels string) bool {
	rw, rh, ok := parseRatio(ratio)
	pw, ph, err := parsePixel(pixels)
	if !ok || err != nil {
		return false
	}
	return rw*ph == pw*rh
}

// ratioPixelsGE 判断像素尺寸的宽高比是否 >= ratio。
func ratioPixelsGE(pixels, ratio string) bool {
	pw, ph, err := parsePixel(pixels)
	rw, rh, ok := parseRatio(ratio)
	if err != nil || !ok {
		return false
	}
	return pw*rh >= rw*ph
}

// ratioPixelsLE 判断像素尺寸的宽高比是否 <= ratio。
func ratioPixelsLE(pixels, ratio string) bool {
	pw, ph, err := parsePixel(pixels)
	rw, rh, ok := parseRatio(ratio)
	if err != nil || !ok {
		return false
	}
	return pw*rh <= rw*ph
}

// ratioLE 判断比例 a 是否不大于比例 b。
func ratioLE(a, b string) bool {
	aw, ah, okA := parseRatio(a)
	bw, bh, okB := parseRatio(b)
	if !okA || !okB {
		return false
	}
	return aw*bh <= bw*ah
}

func countChars(s, mode string) int {
	if mode == "utf16" {
		return len(utf16.Encode([]rune(s)))
	}
	return utf8.RuneCountInString(s)
}

func isEmptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []string:
		return len(x) == 0
	case []any:
		return len(x) == 0
	default:
		return false
	}
}

func valueStrings(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(x) == "" {
			return nil
		}
		return []string{x}
	default:
		return nil
	}
}

func dedupe(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func optionValues(options []Option) string {
	values := make([]string, len(options))
	for i, opt := range options {
		values[i] = opt.Value
	}
	return strings.Join(values, "、")
}

func displayName(p *Param) string {
	if strings.TrimSpace(p.Label) != "" {
		return p.Label
	}
	return p.Key
}
