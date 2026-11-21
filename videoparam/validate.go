package videoparam

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// Error 是参数校验失败的错误，结构与 imageparam.Error 保持一致，
// 便于客户端统一处理。
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

// RefFile 是已解析的参考媒体元数据。
//
// 所有字段都来自上传完成时服务端探测并落库的数据，不信任调用方声明。
type RefFile struct {
	ID          string
	Bytes       int64
	ContentType string
	Width       int
	Height      int
	DurationMS  int64
	FPS         float64
	VideoCodecs []string
	AudioCodecs []string
}

// Validate 校验标量参数（prompt / resolution / duration / ratio）。
//
// 参考媒体的数量与逐文件约束由 ValidateRefMedia 在解析 file_id 之后完成：
// 那时才拿得到真实的尺寸、时长、帧率与编码。
func (s *Spec) Validate(v Values) *Error {
	if s == nil {
		return nil
	}
	if v == nil {
		return newError("PARAM_SPEC_INVALID", "", "参数集合不能为空")
	}

	for i := range s.Params {
		p := &s.Params[i]
		cur, ok := v[p.Path]
		if !ok || isEmptyValue(cur) {
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
		case "number":
			if err := validateNumberValue(p, cur); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateRefCount 只校验参考媒体数量。
//
// 控制器应在解析 file_id、生成签名 URL 之前调用：超量请求不必先把每个文件
// 都查出来再拒绝，既省资源也避免被大数组拖垮。
func ValidateRefCount(p *Param, count int) *Error {
	if p == nil || p.MaxCount == nil || *p.MaxCount <= 0 {
		return nil
	}
	if count > *p.MaxCount {
		return newError("PARAM_REF_TOO_MANY", p.Key, "%s最多 %d 个，当前 %d 个",
			displayName(p), *p.MaxCount, count)
	}
	return nil
}

// ValidateRefMedia 校验参考媒体的数量、格式、大小与逐文件约束。
//
// p 为 nil、max_count 为 0 时表示模型不支持该媒体，直接跳过。
// 控制器应在调用前清空不支持的字段。
func ValidateRefMedia(p *Param, files []RefFile) *Error {
	if p == nil || p.MaxCount == nil || *p.MaxCount <= 0 {
		return nil
	}
	if len(files) > *p.MaxCount {
		return newError("PARAM_REF_TOO_MANY", p.Key, "%s最多 %d 个，当前 %d 个",
			displayName(p), *p.MaxCount, len(files))
	}

	allowedMime := make(map[string]bool, len(p.AllowedMimeTypes))
	for _, mime := range p.AllowedMimeTypes {
		allowedMime[strings.ToLower(strings.TrimSpace(mime))] = true
	}

	totalSeconds := 0
	for _, f := range files {
		if p.MaxFileBytes > 0 && f.Bytes > p.MaxFileBytes {
			return newError("PARAM_REF_FILE_TOO_LARGE", p.Key,
				"%s %s 超过单个上限 %d 字节", displayName(p), f.ID, p.MaxFileBytes)
		}
		if len(allowedMime) > 0 && !allowedMime[strings.ToLower(strings.TrimSpace(f.ContentType))] {
			return newError("PARAM_REF_TYPE_UNSUPPORTED", p.Key,
				"%s %s 的类型 %s 不受支持", displayName(p), f.ID, f.ContentType)
		}

		needDuration := p.DurationSeconds != nil || p.MaxTotalDurationSeconds != nil
		if needDuration && f.DurationMS <= 0 {
			return newError("PARAM_REF_DURATION_MISSING", p.Key,
				"%s %s 缺少时长信息", displayName(p), f.ID)
		}
		seconds := ceilSeconds(f.DurationMS)
		if p.DurationSeconds != nil &&
			(seconds < p.DurationSeconds.Min || seconds > p.DurationSeconds.Max) {
			return newError("PARAM_REF_DURATION_OUT_OF_RANGE", p.Key,
				"%s %s 的时长 %d 秒，需在 %d-%d 秒", displayName(p), f.ID,
				seconds, p.DurationSeconds.Min, p.DurationSeconds.Max)
		}
		totalSeconds += seconds

		if p.AspectRatio != nil {
			if f.Width <= 0 || f.Height <= 0 {
				return newError("PARAM_REF_DIMENSION_MISSING", p.Key,
					"%s %s 缺少宽高信息", displayName(p), f.ID)
			}
			ratio := float64(f.Width) / float64(f.Height)
			if ratio < p.AspectRatio.Min || ratio > p.AspectRatio.Max {
				return newError("PARAM_REF_ASPECT_OUT_OF_RANGE", p.Key,
					"%s %s 的宽高比 %.3f，需在 %.2f-%.2f",
					displayName(p), f.ID, ratio, p.AspectRatio.Min, p.AspectRatio.Max)
			}
		}
		if p.Dimensions != nil {
			if f.Width <= 0 || f.Height <= 0 {
				return newError("PARAM_REF_DIMENSION_MISSING", p.Key,
					"%s %s 缺少宽高信息", displayName(p), f.ID)
			}
			if f.Width < p.Dimensions.Min || f.Width > p.Dimensions.Max ||
				f.Height < p.Dimensions.Min || f.Height > p.Dimensions.Max {
				return newError("PARAM_REF_DIMENSION_OUT_OF_RANGE", p.Key,
					"%s %s 的宽高 %dx%d，需在 %d-%d px",
					displayName(p), f.ID, f.Width, f.Height, p.Dimensions.Min, p.Dimensions.Max)
			}
		}
		if p.FPS != nil {
			if f.FPS <= 0 {
				return newError("PARAM_REF_FPS_MISSING", p.Key,
					"%s %s 缺少帧率信息", displayName(p), f.ID)
			}
			if f.FPS < p.FPS.Min || f.FPS > p.FPS.Max {
				return newError("PARAM_REF_FPS_OUT_OF_RANGE", p.Key,
					"%s %s 的帧率 %.3f，需在 %.3f-%.3f",
					displayName(p), f.ID, f.FPS, p.FPS.Min, p.FPS.Max)
			}
		}
		if len(p.VideoCodecs) > 0 {
			if len(f.VideoCodecs) == 0 {
				return newError("PARAM_REF_CODEC_MISSING", p.Key,
					"%s %s 无法解析视频编码", displayName(p), f.ID)
			}
			for _, codec := range f.VideoCodecs {
				if !codecInList(p.VideoCodecs, codec) {
					return newError("PARAM_REF_CODEC_UNSUPPORTED", p.Key,
						"%s %s 的视频编码 %s 不受支持，仅支持 %s",
						displayName(p), f.ID, codec, strings.Join(p.VideoCodecs, "、"))
				}
			}
		}
		if len(p.AudioCodecs) > 0 {
			for _, codec := range f.AudioCodecs {
				if !codecInList(p.AudioCodecs, codec) {
					return newError("PARAM_REF_CODEC_UNSUPPORTED", p.Key,
						"%s %s 的音频编码 %s 不受支持，仅支持 %s",
						displayName(p), f.ID, codec, strings.Join(p.AudioCodecs, "、"))
				}
			}
		}
	}

	if p.MaxTotalDurationSeconds != nil && totalSeconds > *p.MaxTotalDurationSeconds {
		return newError("PARAM_REF_TOTAL_DURATION_EXCEEDED", p.Key,
			"%s总时长 %d 秒，超过上限 %d 秒", displayName(p), totalSeconds, *p.MaxTotalDurationSeconds)
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
		return nil
	}
	n := CountChars(s, p.CountMode)
	if p.MinLength != nil && n < *p.MinLength {
		return newError("PARAM_TOO_SHORT", p.Key, "%s 不能少于 %d 个字符", displayName(p), *p.MinLength)
	}
	if p.MaxLength != nil && n > *p.MaxLength {
		return newError("PARAM_TOO_LONG", p.Key, "%s 不能超过 %d 个字符", displayName(p), *p.MaxLength)
	}
	return nil
}

func validateSegmentedValue(p *Param, cur any) *Error {
	raw, ok := cur.(string)
	if !ok {
		return newError("PARAM_TYPE", p.Key, "%s 必须是字符串", displayName(p))
	}
	value := normalizeOption(p.Path, raw)
	for _, opt := range p.Options {
		if opt == value {
			return nil
		}
	}
	return newError("PARAM_UNSUPPORTED_VALUE", p.Key, "%s 只支持 %s",
		displayName(p), strings.Join(p.Options, "、"))
}

func validateNumberValue(p *Param, cur any) *Error {
	value, ok := numberValue(cur)
	if !ok {
		return newError("PARAM_TYPE", p.Key, "%s 必须是数字", displayName(p))
	}
	if p.Integer && math.Trunc(value) != value {
		return newError("PARAM_TYPE", p.Key, "%s 必须是整数", displayName(p))
	}
	if p.Min != nil && value < *p.Min {
		return newError("PARAM_TOO_SMALL", p.Key, "%s 不能小于 %v", displayName(p), formatNumber(*p.Min))
	}
	if p.Max != nil && value > *p.Max {
		return newError("PARAM_TOO_LARGE", p.Key, "%s 不能大于 %v", displayName(p), formatNumber(*p.Max))
	}
	return nil
}

func numberValue(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func formatNumber(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%g", v)
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

func displayName(p *Param) string {
	if strings.TrimSpace(p.Label) != "" {
		return p.Label
	}
	return p.Key
}

// CountChars 按指定口径统计字符数。
//
//   - codepoint：Unicode code point，汉字/字母/数字/空格均为 1。
//   - utf16：UTF-16 code unit，与 JavaScript String.length 一致（增补平面字符算 2）。
//   - gbk_bytes：GB18030 编码后的字节数，汉字通常为 2、ASCII 为 1，
//     这是中文 TTS/短信上游常见的「汉字算 2」口径。
//   - cjk_weighted：显式按 Han=2、其它=1 加权，不依赖具体编码。
func CountChars(s, mode string) int {
	switch mode {
	case "utf16":
		n := 0
		for _, r := range s {
			if r > 0xFFFF {
				n += 2
			} else {
				n++
			}
		}
		return n
	case "gbk_bytes":
		out, _, err := transform.String(simplifiedchinese.GB18030.NewEncoder(), s)
		if err != nil {
			return countCJKWeighted(s)
		}
		return len(out)
	case "cjk_weighted":
		return countCJKWeighted(s)
	default:
		return utf8.RuneCountInString(s)
	}
}

func countCJKWeighted(s string) int {
	n := 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func ceilSeconds(durationMS int64) int {
	if durationMS <= 0 {
		return 0
	}
	return int((durationMS + 999) / 1000)
}

func codecInList(list []string, raw string) bool {
	codec := NormalizeCodec(raw)
	for _, allowed := range list {
		if NormalizeCodec(allowed) == codec {
			return true
		}
	}
	return false
}
