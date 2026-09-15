package controller

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

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

// ── 参考图加价的两道闸门 ──

// 多模态对话端点的请求体同样会带 images / image_urls，白名单能防止它们被误加价。
func TestRefImageEndpointWhitelist(t *testing.T) {
	if isRefImageEndpoint("/v1/chat/completions") {
		t.Error("/v1/chat/completions 不应被当作图片端点")
	}
	if !isRefImageEndpoint("/v1/images/generations") {
		t.Error("/v1/images/generations 应被当作图片端点")
	}
}

// 加价要求模型类型是图像生成；空类型按图像处理（fail-closed，宁可多扣不漏扣）。
func TestIsRefImageModel(t *testing.T) {
	cases := []struct {
		name string
		typ  model.ModelType
		want bool
	}{
		{"图像生成", model.ModelTypeImage, true},
		{"空类型按图像处理", "", true},
		{"视频生成", model.ModelTypeVideo, false},
		{"文本生成", model.ModelTypeText, false},
		{"音乐生成", model.ModelTypeMusic, false},
		{"其他", model.ModelTypeOther, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRefImageModel(tc.typ); got != tc.want {
				t.Errorf("isRefImageModel(%q) = %v, want %v", tc.typ, got, tc.want)
			}
		})
	}
}

// ── marshalRequestBody：回溯用请求体快照 ──

func TestMarshalRequestBodyKeepsNormalBody(t *testing.T) {
	body := map[string]interface{}{"model": "gpt-image-1", "prompt": "a cat"}
	got := marshalRequestBody(body)

	// 用 json.Marshal 的结果做期望值，避免依赖 map 键序
	want, _ := json.Marshal(body)
	if got != string(want) {
		t.Errorf("未超长时不应改动内容: got %q, want %q", got, want)
	}
}

// 参考图 base64 内联会让请求体到数 MB，必须截断，否则 tasks 表会被撑爆。
func TestMarshalRequestBodyTruncatesOversizedBody(t *testing.T) {
	body := map[string]interface{}{"prompt": strings.Repeat("a", maxStoredRequestBody+1024)}
	got := marshalRequestBody(body)

	if len(got) > maxStoredRequestBody+len(requestBodyTruncatedMarker) {
		t.Errorf("截断后长度 %d 超出预期上限", len(got))
	}
	if !strings.HasSuffix(got, requestBodyTruncatedMarker) {
		t.Errorf("截断后应带标记，实际长度 %d，结尾: %q", len(got), got[len(got)-32:])
	}
	if !utf8.ValidString(got) {
		t.Error("截断后必须是合法 UTF-8，否则 Postgres 拒绝写入")
	}
}

// 截断点落在多字节字符中间时，按 rune 边界回退而不是硬切字节
func TestMarshalRequestBodyTruncationOnRuneBoundary(t *testing.T) {
	// 每个汉字 3 字节，截断点必然落在字符内部
	body := map[string]interface{}{"prompt": strings.Repeat("汉", maxStoredRequestBody)}
	got := marshalRequestBody(body)

	if !utf8.ValidString(got) {
		t.Error("截断点应回退到 rune 边界，避免写入无效 UTF-8")
	}
	if !strings.HasSuffix(got, requestBodyTruncatedMarker) {
		t.Error("截断后应带标记")
	}
}

// 恰好等于上限时不应截断（边界值，off-by-one 的常见位置）
func TestMarshalRequestBodyAtMaxBoundary(t *testing.T) {
	// 先构造固定内容，再用 padding 顶到恰好 maxStoredRequestBody 字节
	payload := map[string]interface{}{"prompt": ""}
	raw, _ := json.Marshal(payload)
	payload["prompt"] = strings.Repeat("a", maxStoredRequestBody-len(raw))
	raw, _ = json.Marshal(payload)

	if len(raw) != maxStoredRequestBody {
		t.Fatalf("用例构造有误: 期望恰好 %d 字节，实际 %d", maxStoredRequestBody, len(raw))
	}
	if got := marshalRequestBody(payload); got != string(raw) {
		t.Error("恰好等于上限时不应截断")
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
