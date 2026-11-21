package controller

import (
	"errors"
	"fmt"

	"github.com/FutureAI-X/futureai-api/model"
)

// videoBillingInput 计算视频费用所需的、已从请求与参考文件中解析出的数量。
type videoBillingInput struct {
	// Resolution 已归一化为大写的输出分辨率。
	Resolution string
	// ImageCount 去重后的参考图片数量。
	ImageCount int
	// AudioCount 去重后的参考音频数量。
	AudioCount int
	// InputVideoSeconds 去重后每个参考视频按秒向上取整再求和。
	InputVideoSeconds int
	// OutputSeconds 调用方请求的输出时长（秒）。
	OutputSeconds int
	// HasReferenceVideo 去重后的参考视频是否非空。
	HasReferenceVideo bool
}

// videoBillingSnapshot 视频任务的计费快照。
//
// 费率是提交时的拷贝：管理员之后改价或删除档位，都不影响已建任务的审计口径。
type videoBillingSnapshot struct {
	Version           int                 `json:"version"`
	RuleID            int                 `json:"rule_id"`
	RateRowID         int                 `json:"rate_row_id"`
	Resolution        string              `json:"resolution"`
	HasReferenceVideo bool                `json:"has_ref_video"`
	Rates             videoBillingRates   `json:"rates"`
	Units             videoBillingUnits   `json:"units"`
	Charges           videoBillingCharges `json:"charges"`
	Total             float64             `json:"total"`
}

type videoBillingRates struct {
	ImageCreditsPerItem          float64 `json:"image_credits_per_item"`
	ImageFreeItems               int     `json:"image_free_items"`
	AudioCreditsPerItem          float64 `json:"audio_credits_per_item"`
	InputVideoCreditsPerSecond   float64 `json:"input_video_credits_per_second"`
	OutputCreditsPerSecond       float64 `json:"output_credits_per_second"`
}

type videoBillingUnits struct {
	ImageCount         int `json:"image_count"`
	BillableImageCount int `json:"billable_image_count"`
	AudioCount         int `json:"audio_count"`
	InputVideoSeconds  int `json:"input_video_seconds"`
	OutputSeconds      int `json:"output_seconds"`
}

type videoBillingCharges struct {
	Image      float64 `json:"image"`
	Audio      float64 `json:"audio"`
	InputVideo float64 `json:"input_video"`
	Output     float64 `json:"output"`
}

// videoBillingResult 一次视频请求的计费结果，供建任务与快照使用。
type videoBillingResult struct {
	Total    float64
	Snapshot videoBillingSnapshot
}

// ceilVideoSeconds 把毫秒时长折算成秒，向上取整。
//
// 逐文件取整而不是先求和再取整：不同上游可能按各自口径结算，逐文件向上取整
// 是最保守的做法，保证平台不因取整差额亏损。
func ceilVideoSeconds(durationMS int64) int {
	if durationMS <= 0 {
		return 0
	}
	return int((durationMS + 999) / 1000)
}

// computeVideoBilling 按规则、命中的分辨率档位与各项数量计算总费用。
//
// 所有费率最多 3 位小数、所有数量都是整数，因此每一项金额在 3 位精度内是精确的。
// 仍然逐项 RoundCredits，既消除浮点噪声，也保证快照里的分项之和等于 Total。
func computeVideoBilling(rule *model.VideoCreditRule, rate model.VideoCreditRate, in videoBillingInput) (videoBillingResult, error) {
	if rule == nil {
		return videoBillingResult{}, errors.New("视频计费规则为空")
	}
	if rule.ImageFreeItems < 0 {
		return videoBillingResult{}, errors.New("视频计费规则中的图片免费张数为负数")
	}
	for _, item := range []struct {
		name  string
		value float64
	}{
		{"每张参考图积分", rule.ImageCreditsPerItem},
		{"每个参考音频积分", rule.AudioCreditsPerItem},
		{"参考视频每秒积分", rate.InputVideoCreditsPerSecond},
		{"无参考视频的输出每秒积分", rate.OutputCreditsPerSecondNoRef},
		{"含参考视频的输出每秒积分", rate.OutputCreditsPerSecondWithRef},
	} {
		if item.value < 0 {
			return videoBillingResult{}, fmt.Errorf("视频计费规则中的%s为负数", item.name)
		}
	}

	billableImages := in.ImageCount - rule.ImageFreeItems
	if billableImages < 0 {
		billableImages = 0
	}

	outputRate := rate.OutputCreditsPerSecondNoRef
	if in.HasReferenceVideo {
		outputRate = rate.OutputCreditsPerSecondWithRef
	}

	imageCharge := model.RoundCredits(float64(billableImages) * rule.ImageCreditsPerItem)
	audioCharge := model.RoundCredits(float64(in.AudioCount) * rule.AudioCreditsPerItem)
	inputVideoCharge := model.RoundCredits(float64(in.InputVideoSeconds) * rate.InputVideoCreditsPerSecond)
	outputCharge := model.RoundCredits(float64(in.OutputSeconds) * outputRate)
	total := model.RoundCredits(imageCharge + audioCharge + inputVideoCharge + outputCharge)

	return videoBillingResult{
		Total: total,
		Snapshot: videoBillingSnapshot{
			Version:           1,
			RuleID:            rule.ID,
			RateRowID:         rate.ID,
			Resolution:        in.Resolution,
			HasReferenceVideo: in.HasReferenceVideo,
			Rates: videoBillingRates{
				ImageCreditsPerItem:        rule.ImageCreditsPerItem,
				ImageFreeItems:             rule.ImageFreeItems,
				AudioCreditsPerItem:        rule.AudioCreditsPerItem,
				InputVideoCreditsPerSecond: rate.InputVideoCreditsPerSecond,
				OutputCreditsPerSecond:     outputRate,
			},
			Units: videoBillingUnits{
				ImageCount:         in.ImageCount,
				BillableImageCount: billableImages,
				AudioCount:         in.AudioCount,
				InputVideoSeconds:  in.InputVideoSeconds,
				OutputSeconds:      in.OutputSeconds,
			},
			Charges: videoBillingCharges{
				Image:      imageCharge,
				Audio:      audioCharge,
				InputVideo: inputVideoCharge,
				Output:     outputCharge,
			},
			Total: total,
		},
	}, nil
}

// videoBillingRemark 生成扣费备注。CreditLog.Remark 是 varchar(255)，
// 格式固定且短，避免超长在 Postgres 上硬报错。
func videoBillingRemark(snapshot videoBillingSnapshot) string {
	return fmt.Sprintf("视频生成任务(%s,图%d/音%d/视频%d秒,输出%d秒)",
		snapshot.Resolution,
		snapshot.Units.ImageCount,
		snapshot.Units.AudioCount,
		snapshot.Units.InputVideoSeconds,
		snapshot.Units.OutputSeconds,
	)
}
