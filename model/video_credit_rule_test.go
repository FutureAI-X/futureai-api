package model

import (
	"strings"
	"testing"
)

func validVideoCreditRule() *VideoCreditRule {
	return &VideoCreditRule{
		ModelID:             1,
		Status:              1,
		ImageCreditsPerItem: 0.05,
		ImageFreeItems:      5,
		AudioCreditsPerItem: 0.02,
		Rates: []VideoCreditRate{
			{
				Resolution:                    "480p",
				InputVideoCreditsPerSecond:    0.06,
				OutputCreditsPerSecondNoRef:   0.06,
				OutputCreditsPerSecondWithRef: 0.03,
			},
			{
				Resolution:                    " 1080P ",
				InputVideoCreditsPerSecond:    0.14,
				OutputCreditsPerSecondNoRef:   0.14,
				OutputCreditsPerSecondWithRef: 0.10,
			},
		},
	}
}

func TestValidateVideoCreditRuleNormalizesResolution(t *testing.T) {
	rule := validVideoCreditRule()
	if err := ValidateVideoCreditRule(rule); err != nil {
		t.Fatalf("合法规则不应报错: %v", err)
	}
	if rule.Rates[0].Resolution != "480P" {
		t.Errorf("分辨率未归一化: %q", rule.Rates[0].Resolution)
	}
	if rule.Rates[1].Resolution != "1080P" {
		t.Errorf("分辨率未去空白/大写: %q", rule.Rates[1].Resolution)
	}
}

func TestValidateVideoCreditRuleRejectsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*VideoCreditRule)
		want   string
	}{
		{"没有档位", func(r *VideoCreditRule) { r.Rates = nil }, "至少配置"},
		{"空分辨率", func(r *VideoCreditRule) { r.Rates[0].Resolution = "  " }, "不能为空"},
		{"重复分辨率（大小写不同）", func(r *VideoCreditRule) {
			r.Rates[0].Resolution = "1080p"
			r.Rates[1].Resolution = "1080P"
		}, "重复"},
		{"免费张数为负", func(r *VideoCreditRule) { r.ImageFreeItems = -1 }, "负数"},
		{"图片单价为负", func(r *VideoCreditRule) { r.ImageCreditsPerItem = -0.01 }, "负数"},
		{"音频单价为负", func(r *VideoCreditRule) { r.AudioCreditsPerItem = -0.01 }, "负数"},
		{"图片单价精度过高", func(r *VideoCreditRule) { r.ImageCreditsPerItem = 0.0001 }, "小数"},
		{"档位单价为负", func(r *VideoCreditRule) { r.Rates[0].OutputCreditsPerSecondNoRef = -0.01 }, "负数"},
		{"档位单价精度过高", func(r *VideoCreditRule) { r.Rates[0].InputVideoCreditsPerSecond = 0.0001 }, "小数"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := validVideoCreditRule()
			tc.mutate(rule)
			err := ValidateVideoCreditRule(rule)
			if err == nil {
				t.Fatalf("应当返回错误，实际通过")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息 = %q，应包含 %q", err.Error(), tc.want)
			}
		})
	}
}

func TestValidateVideoCreditRuleAllowsZeroRates(t *testing.T) {
	rule := &VideoCreditRule{
		ModelID: 1,
		Status:  1,
		Rates: []VideoCreditRate{{
			Resolution: "480P",
		}},
	}
	if err := ValidateVideoCreditRule(rule); err != nil {
		t.Fatalf("全 0 费率是合法的免费配置: %v", err)
	}
}

func TestFindVideoCreditRate(t *testing.T) {
	rule := &VideoCreditRule{
		Rates: []VideoCreditRate{
			{ID: 1, Resolution: "480P"},
			{ID: 2, Resolution: "2K"},
		},
	}

	if rate := FindVideoCreditRate(rule, " 2k "); rate == nil || rate.ID != 2 {
		t.Errorf("应命中 2K 档位，实际 %+v", rate)
	}
	if rate := FindVideoCreditRate(rule, "1080P"); rate != nil {
		t.Errorf("未配置的档位应返回 nil，实际 %+v", rate)
	}
	if rate := FindVideoCreditRate(nil, "2K"); rate != nil {
		t.Errorf("空规则应返回 nil")
	}
}

func TestNormalizeVideoResolution(t *testing.T) {
	cases := map[string]string{
		" 1080p ": "1080P",
		"2k":      "2K",
		"480P":    "480P",
		"":        "",
	}
	for in, want := range cases {
		if got := NormalizeVideoResolution(in); got != want {
			t.Errorf("NormalizeVideoResolution(%q) = %q, want %q", in, got, want)
		}
	}
}
