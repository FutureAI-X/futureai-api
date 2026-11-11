package supplier

import "testing"

func TestSizeRuleValidate(t *testing.T) {
	ratioOnly := SizeRule{
		AllowRatio: true,
		Ratios:     []string{"1:1", "16:9"},
	}
	pixelsOnly := SizeRule{
		AllowPixels: true,
		Pixels: &PixelRule{
			Step:            16,
			MaxSide:         3840,
			MaxPixels:       3840 * 2160,
			MinAspectWidth:  1,
			MinAspectHeight: 3,
			MaxAspectWidth:  3,
			MaxAspectHeight: 1,
		},
	}
	both := SizeRule{
		AllowRatio:  true,
		AllowPixels: true,
		Ratios:      []string{"1:1", "16:9"},
		Pixels:      pixelsOnly.Pixels,
	}
	anyPixels := SizeRule{AllowPixels: true}

	tests := []struct {
		name    string
		rule    SizeRule
		size    string
		wantErr bool
	}{
		{"比例命中", ratioOnly, "16:9", false},
		{"比例不在集合", ratioOnly, "4:3", true},
		{"比例模型拒绝像素", ratioOnly, "1024x1024", true},
		{"像素模型命中", pixelsOnly, "1024x1024", false},
		{"像素模型拒绝比例", pixelsOnly, "16:9", true},
		{"x 大小写均可", pixelsOnly, "1024X768", false},
		{"像素规则未配置时接受任意合法像素", anyPixels, "1232x1232", false},
		{"两种都接受：比例", both, "1:1", false},
		{"两种都接受：像素", both, "1024x1024", false},
		// auto 是 param_spec 决定是否开放的特殊取值，供应商层不做硬编码限制。
		{"auto 不受比例集合限制", ratioOnly, "auto", false},
		{"auto 不受像素规则限制", pixelsOnly, "auto", false},
		{"auto 大小写敏感", ratioOnly, "AUTO", true},
		{"空 size", both, "  ", true},
		{"非法格式", both, "16by9", true},
		{"比例中带空格", ratioOnly, "16 : 9", true},
		{"像素非 16 倍数", pixelsOnly, "1000x1024", true},
		{"像素超过最大边长", pixelsOnly, "4096x1024", true},
		{"像素超过总像素", pixelsOnly, "2896x2896", true},
		{"像素比例低于下限", pixelsOnly, "512x1552", true},
		{"像素比例高于上限", pixelsOnly, "1552x512", true},
		{"像素比例恰为下限", pixelsOnly, "512x1536", false},
		{"像素比例恰为上限", pixelsOnly, "1536x512", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate(tt.size)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate(%q) error = %v, wantErr %v", tt.size, err, tt.wantErr)
			}
		})
	}
}
