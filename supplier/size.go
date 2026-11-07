package supplier

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// PixelRule 像素尺寸的入参约束。字段为 0 表示不限制。
type PixelRule struct {
	// Step 宽高必须同时能被 Step 整除。0 表示不限。
	Step int

	// MaxSide 任意一边的最大长度。0 表示不限。
	MaxSide int

	// MaxPixels 总像素（宽×高）上限。0 表示不限。
	MaxPixels int

	// 宽高比范围：[MinAspectWidth/MinAspectHeight, MaxAspectWidth/MaxAspectHeight]。
	// 任一分量为 0 表示不限制。
	MinAspectWidth  int
	MinAspectHeight int
	MaxAspectWidth  int
	MaxAspectHeight int
}

// SizeRule 单个模型对 size 的入参约束。
//
// 只负责校验，不做比例与像素之间的换算：调用方必须按模型能力选择一种写法，
// 平台不替它把「16:9 + 2K」算成像素，也不把像素约分成比例。
type SizeRule struct {
	// AllowRatio 是否接受「宽:高」比例。
	AllowRatio bool

	// AllowPixels 是否接受「宽x高」像素尺寸。
	AllowPixels bool

	// Ratios 允许的比例集合，仅在 AllowRatio 为 true 时生效。
	Ratios []string

	// Pixels 像素约束，仅在 AllowPixels 为 true 时生效。
	// 为 nil 时表示接受任意合法的「宽x高」。
	Pixels *PixelRule
}

// sizeRatioPattern 匹配「宽:高」。
//
// 只允许首尾空白，不允许数字与冒号之间出现空白：平台把 size 原样透传给上游，
// 宽松匹配会让上游不认的写法通过本地校验。
var sizeRatioPattern = regexp.MustCompile(`^(\d{1,4}):(\d{1,4})$`)

// sizePixelPattern 匹配「宽x高」，x 大小写均可。
var sizePixelPattern = regexp.MustCompile(`^(\d{1,4})[xX](\d{1,4})$`)

// Validate 校验 size 是否满足该模型的入参约束。
func (r SizeRule) Validate(raw string) error {
	size := strings.TrimSpace(raw)
	if size == "" {
		return fmt.Errorf("size 不能为空")
	}

	if sizeRatioPattern.MatchString(size) {
		if !r.AllowRatio {
			return fmt.Errorf("该模型的 size 只支持「宽x高」像素尺寸")
		}
		for _, allowed := range r.Ratios {
			if size == allowed {
				return nil
			}
		}
		return fmt.Errorf("size 比例只支持 %s", strings.Join(r.Ratios, "、"))
	}

	if m := sizePixelPattern.FindStringSubmatch(size); m != nil {
		if !r.AllowPixels {
			return fmt.Errorf("该模型的 size 只支持「宽:高」比例")
		}
		width, _ := strconv.Atoi(m[1])
		height, _ := strconv.Atoi(m[2])
		return r.Pixels.validate(width, height)
	}

	switch {
	case r.AllowRatio && r.AllowPixels:
		return fmt.Errorf("size 只支持「宽:高」比例或「宽x高」像素尺寸")
	case r.AllowRatio:
		return fmt.Errorf("size 只支持「宽:高」比例")
	case r.AllowPixels:
		return fmt.Errorf("size 只支持「宽x高」像素尺寸")
	default:
		return fmt.Errorf("size 不合法")
	}
}

// validate 校验像素尺寸。nil 表示该模型对像素没有额外约束。
func (p *PixelRule) validate(width, height int) error {
	if p == nil {
		return nil
	}
	if width <= 0 || height <= 0 {
		return fmt.Errorf("size 的宽高必须是正整数")
	}
	if p.Step > 0 && (width%p.Step != 0 || height%p.Step != 0) {
		return fmt.Errorf("size 的宽高必须是 %d 的倍数", p.Step)
	}
	if p.MaxSide > 0 && (width > p.MaxSide || height > p.MaxSide) {
		return fmt.Errorf("size 任意一边不能超过 %d", p.MaxSide)
	}
	if p.MaxPixels > 0 && width*height > p.MaxPixels {
		return fmt.Errorf("size 总像素不能超过 %d", p.MaxPixels)
	}

	// 用交叉相乘比较比例，避免浮点误差。
	if p.MinAspectWidth > 0 && p.MinAspectHeight > 0 &&
		width*p.MinAspectHeight < height*p.MinAspectWidth {
		return p.aspectError()
	}
	if p.MaxAspectWidth > 0 && p.MaxAspectHeight > 0 &&
		width*p.MaxAspectHeight > height*p.MaxAspectWidth {
		return p.aspectError()
	}
	return nil
}

func (p *PixelRule) aspectError() error {
	if p.MinAspectWidth > 0 && p.MinAspectHeight > 0 &&
		p.MaxAspectWidth > 0 && p.MaxAspectHeight > 0 {
		return fmt.Errorf("size 宽高比必须在 %d:%d 到 %d:%d 之间",
			p.MinAspectWidth, p.MinAspectHeight, p.MaxAspectWidth, p.MaxAspectHeight)
	}
	return fmt.Errorf("size 宽高比超出允许范围")
}
