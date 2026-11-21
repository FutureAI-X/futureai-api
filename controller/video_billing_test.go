package controller

import (
	"math"
	"testing"

	"github.com/FutureAI-X/futureai-api/model"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestCeilVideoSeconds(t *testing.T) {
	cases := []struct {
		name string
		ms   int64
		want int
	}{
		{"零", 0, 0},
		{"负数", -500, 0},
		{"不足一秒", 1, 1},
		{"接近一秒", 999, 1},
		{"整秒", 1000, 1},
		{"超过一秒", 1001, 2},
		{"3.2 秒向上取整", 3200, 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ceilVideoSeconds(tc.ms); got != tc.want {
				t.Errorf("ceilVideoSeconds(%d) = %d, want %d", tc.ms, got, tc.want)
			}
		})
	}
}

func sampleVideoBillingRule() *model.VideoCreditRule {
	return &model.VideoCreditRule{
		ID:                  12,
		ModelID:             1,
		Status:              1,
		ImageCreditsPerItem: 0.05,
		ImageFreeItems:      5,
		AudioCreditsPerItem: 0.02,
		Rates: []model.VideoCreditRate{
			{
				ID:                            45,
				RuleID:                        12,
				Resolution:                    "1080P",
				InputVideoCreditsPerSecond:    0.14,
				OutputCreditsPerSecondNoRef:   0.14,
				OutputCreditsPerSecondWithRef: 0.10,
			},
		},
	}
}

func TestComputeVideoBillingWithReferenceVideo(t *testing.T) {
	rule := sampleVideoBillingRule()
	rate := rule.Rates[0]

	result, err := computeVideoBilling(rule, rate, videoBillingInput{
		Resolution:        "1080P",
		ImageCount:        2, // 免费 5 张，全部免费
		AudioCount:        1,
		InputVideoSeconds: 4,
		OutputSeconds:     5,
		HasReferenceVideo: true,
	})
	if err != nil {
		t.Fatalf("计算不应报错: %v", err)
	}

	want := map[string]float64{
		"image":       0,
		"audio":       0.02,
		"input_video": 0.56,
		"output":      0.50,
		"total":       1.08,
	}
	got := result.Snapshot.Charges
	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"image", got.Image, want["image"]},
		{"audio", got.Audio, want["audio"]},
		{"input_video", got.InputVideo, want["input_video"]},
		{"output", got.Output, want["output"]},
		{"total", result.Total, want["total"]},
	}
	for _, c := range checks {
		if !almostEqual(c.got, c.want) {
			t.Errorf("%s = %.6f, want %.6f", c.name, c.got, c.want)
		}
	}

	if result.Snapshot.Units.BillableImageCount != 0 {
		t.Errorf("billable image count = %d, want 0", result.Snapshot.Units.BillableImageCount)
	}
	if !almostEqual(result.Snapshot.Rates.OutputCreditsPerSecond, rate.OutputCreditsPerSecondWithRef) {
		t.Errorf("快照应记录含参考视频的输出费率")
	}
	if !result.Snapshot.HasReferenceVideo {
		t.Errorf("快照应标记含参考视频")
	}
}

func TestComputeVideoBillingWithoutReferenceVideo(t *testing.T) {
	rule := sampleVideoBillingRule()
	rule.ImageFreeItems = 0
	rate := rule.Rates[0]

	result, err := computeVideoBilling(rule, rate, videoBillingInput{
		Resolution:        "1080P",
		ImageCount:        3,
		AudioCount:        0,
		InputVideoSeconds: 0,
		OutputSeconds:     5,
		HasReferenceVideo: false,
	})
	if err != nil {
		t.Fatalf("计算不应报错: %v", err)
	}

	// 图片 3 × 0.05 = 0.15；输出 5 × 0.14 = 0.70。
	if !almostEqual(result.Snapshot.Charges.Image, 0.15) {
		t.Errorf("image = %.6f, want 0.15", result.Snapshot.Charges.Image)
	}
	if !almostEqual(result.Snapshot.Charges.Output, 0.70) {
		t.Errorf("output = %.6f, want 0.70", result.Snapshot.Charges.Output)
	}
	if !almostEqual(result.Total, 0.85) {
		t.Errorf("total = %.6f, want 0.85", result.Total)
	}
	if result.Snapshot.HasReferenceVideo {
		t.Errorf("不应标记含参考视频")
	}
}

func TestComputeVideoBillingZeroRateIsFree(t *testing.T) {
	rule := &model.VideoCreditRule{
		ID:      1,
		ModelID: 1,
		Rates: []model.VideoCreditRate{{
			ID:         1,
			RuleID:     1,
			Resolution: "480P",
		}},
	}
	rate := rule.Rates[0]

	result, err := computeVideoBilling(rule, rate, videoBillingInput{
		Resolution:        "480P",
		ImageCount:        10,
		AudioCount:        5,
		InputVideoSeconds: 30,
		OutputSeconds:     20,
		HasReferenceVideo: true,
	})
	if err != nil {
		t.Fatalf("0 费率是合法配置: %v", err)
	}
	if result.Total != 0 {
		t.Errorf("total = %.6f, want 0", result.Total)
	}
}

func TestComputeVideoBillingRejectsNegativeConfig(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*model.VideoCreditRule, *model.VideoCreditRate)
	}{
		{"免费张数为负", func(r *model.VideoCreditRule, _ *model.VideoCreditRate) { r.ImageFreeItems = -1 }},
		{"图片单价为负", func(r *model.VideoCreditRule, _ *model.VideoCreditRate) { r.ImageCreditsPerItem = -0.01 }},
		{"音频单价为负", func(r *model.VideoCreditRule, _ *model.VideoCreditRate) { r.AudioCreditsPerItem = -0.01 }},
		{"输入视频单价为负", func(_ *model.VideoCreditRule, rate *model.VideoCreditRate) {
			rate.InputVideoCreditsPerSecond = -0.01
		}},
		{"输出单价为负", func(_ *model.VideoCreditRule, rate *model.VideoCreditRate) {
			rate.OutputCreditsPerSecondNoRef = -0.01
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := sampleVideoBillingRule()
			rate := rule.Rates[0]
			tc.mutate(rule, &rate)
			if _, err := computeVideoBilling(rule, rate, videoBillingInput{}); err == nil {
				t.Fatalf("负值配置应当报错")
			}
		})
	}
}

func TestComputeVideoBillingSnapshotChargesSumToTotal(t *testing.T) {
	rule := sampleVideoBillingRule()
	rule.ImageFreeItems = 1
	rule.ImageCreditsPerItem = 0.333
	rule.AudioCreditsPerItem = 0.111
	rate := rule.Rates[0]
	rate.InputVideoCreditsPerSecond = 0.077
	rate.OutputCreditsPerSecondWithRef = 0.099

	result, err := computeVideoBilling(rule, rate, videoBillingInput{
		Resolution:        "1080P",
		ImageCount:        4,
		AudioCount:        3,
		InputVideoSeconds: 7,
		OutputSeconds:     9,
		HasReferenceVideo: true,
	})
	if err != nil {
		t.Fatalf("计算不应报错: %v", err)
	}

	sum := result.Snapshot.Charges.Image + result.Snapshot.Charges.Audio +
		result.Snapshot.Charges.InputVideo + result.Snapshot.Charges.Output
	if !almostEqual(model.RoundCredits(sum), result.Total) {
		t.Errorf("分项之和 %.6f 与总额 %.6f 不一致", sum, result.Total)
	}
}

func TestVideoBillingRemarkIsShort(t *testing.T) {
	snapshot := videoBillingSnapshot{
		Resolution: "1080P",
		Units: videoBillingUnits{
			ImageCount:        12,
			AudioCount:        3,
			InputVideoSeconds: 120,
			OutputSeconds:     300,
		},
	}
	remark := videoBillingRemark(snapshot)
	if remark == "" {
		t.Fatalf("备注不应为空")
	}
	if len([]rune(remark)) > 255 {
		t.Errorf("备注过长: %d", len([]rune(remark)))
	}
}
