package controller

import (
	"strconv"
	"testing"

	"github.com/FutureAI/token-hub/model"
)

// ── countRefImages：参考图张数统计 ──

func TestCountRefImagesCountsArraysAndStrings(t *testing.T) {
	params := []string{"image", "images", "image_urls"}

	cases := []struct {
		name string
		body map[string]interface{}
		want int
	}{
		{"无参考图字段", map[string]interface{}{"prompt": "a cat"}, 0},
		{"单字符串算1张", map[string]interface{}{"image": "https://a.png"}, 1},
		{"字符串数组按元素个数", map[string]interface{}{"image": []interface{}{"https://a.png", "https://b.png"}}, 2},
		{"Go字符串切片", map[string]interface{}{"images": []string{"a.png", "b.png", "c.png"}}, 3},
		{"空数组算0张", map[string]interface{}{"image": []interface{}{}}, 0},
		{"空字符串算0张", map[string]interface{}{"image": ""}, 0},
		{"数组里的空串不计", map[string]interface{}{"image": []interface{}{"a.png", "", "b.png"}}, 2},
		{"数组里的null不计", map[string]interface{}{"image": []interface{}{"a.png", nil}}, 1},
		{"显式null算0张", map[string]interface{}{"image": nil}, 0},
		{"未配置的参数名不参与", map[string]interface{}{"reference_images": []interface{}{"a.png", "b.png"}}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countRefImages(tc.body, params); got != tc.want {
				t.Errorf("countRefImages(%v) = %d, want %d", tc.body, got, tc.want)
			}
		})
	}
}

// 默认参数名列表里 image / image_url / image_urls 是同一张图的不同 SDK 写法，
// 按参数名累加会把一张图扣上 2~3 次，必须按图片值去重。
func TestCountRefImagesDeduplicatesAcrossParamNames(t *testing.T) {
	params := model.ParseRefImageParams(model.DefaultRefImageParams)

	cases := []struct {
		name string
		body map[string]interface{}
		want int
	}{
		{
			"同一张图出现在两个参数名下只计一次",
			map[string]interface{}{"image": "https://a.png", "image_urls": []interface{}{"https://a.png"}},
			1,
		},
		{
			"不同图片分别计数",
			map[string]interface{}{"image": "https://a.png", "image_urls": []interface{}{"https://b.png"}},
			2,
		},
		{
			"同一数组内的重复值只计一次",
			map[string]interface{}{"image": []interface{}{"https://a.png", "https://a.png"}},
			1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countRefImages(tc.body, params); got != tc.want {
				t.Errorf("countRefImages(%v) = %d, want %d", tc.body, got, tc.want)
			}
		})
	}
}

// 类型不认识时保守按 1 张计：漏计费是资损，多计费只是一次可解释的争议。
func TestCountRefImagesConservativeOnUnknownTypes(t *testing.T) {
	params := []string{"image"}

	cases := []struct {
		name string
		body map[string]interface{}
		want int
	}{
		{"对象形态按1张", map[string]interface{}{"image": map[string]interface{}{"url": "https://a.png"}}, 1},
		{"数字按1张", map[string]interface{}{"image": 0}, 1},
		{"布尔按1张", map[string]interface{}{"image": false}, 1},
		{"对象数组每个算1张", map[string]interface{}{"image": []interface{}{map[string]interface{}{"url": "a"}, map[string]interface{}{"url": "b"}}}, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countRefImages(tc.body, params); got != tc.want {
				t.Errorf("countRefImages(%v) = %d, want %d", tc.body, got, tc.want)
			}
		})
	}
}

// 请求体目前没有大小限制，一个上万元素的数组会产生荒谬的扣费，必须封顶。
func TestCountRefImagesClampsToMax(t *testing.T) {
	huge := make([]interface{}, maxRefImagesPerRequest+50)
	for i := range huge {
		huge[i] = "https://example.com/" + strconv.Itoa(i) + ".png"
	}

	got := countRefImages(map[string]interface{}{"image": huge}, []string{"image"})
	if got != maxRefImagesPerRequest {
		t.Errorf("超限时应封顶到 %d，实际 %d", maxRefImagesPerRequest, got)
	}
}

// 恰好等于上限时不应触发封顶，也不应少计
func TestCountRefImagesAtMaxBoundary(t *testing.T) {
	exact := make([]interface{}, maxRefImagesPerRequest)
	for i := range exact {
		exact[i] = "https://example.com/" + strconv.Itoa(i) + ".png"
	}

	if got := countRefImages(map[string]interface{}{"image": exact}, []string{"image"}); got != maxRefImagesPerRequest {
		t.Errorf("恰好上限时应为 %d，实际 %d", maxRefImagesPerRequest, got)
	}
}

// ── resolveCredits：端点白名单 ──

// 多模态对话端点的请求体同样会带 images / image_urls，白名单能防止它们被误加价。
func TestResolveCreditsOnlyChargesRefImagesOnImageEndpoints(t *testing.T) {
	if isRefImageEndpoint("/v1/chat/completions") {
		t.Error("/v1/chat/completions 不应被当作图片端点")
	}
	if !isRefImageEndpoint("/v1/images/generations") {
		t.Error("/v1/images/generations 应被当作图片端点")
	}
}

// ── creditResolution.Remark：扣费备注明细 ──

func TestCreditResolutionRemark(t *testing.T) {
	if got := (creditResolution{Total: 1, Base: 1}).Remark(); got != "图像生成任务" {
		t.Errorf("无参考图时备注不应带明细，实际 %q", got)
	}

	got := (creditResolution{Total: 5, Base: 1, RefImageCount: 2}).Remark()
	if got != "图像生成任务(基础1.00+参考图2张)" {
		t.Errorf("备注格式不符: %q", got)
	}
}
