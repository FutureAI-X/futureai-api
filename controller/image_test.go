package controller

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/FutureAI/token-hub/model"
	"github.com/FutureAI/token-hub/supplier"
)

// ── 请求标准：非标准字段一律忽略 ──

// 标准之外的字段不能被解析进来：它们既进不了计费条件，也进不了快照和上游请求。
// VendorModelID 尤其重要——它由服务端填充，调用方注入就等于绕过模型映射。
func TestImageGenerateRequestIgnoresNonStandardFields(t *testing.T) {
	body := `{
		"model": "gpt-image-2.5-flare-ext",
		"prompt": "a cat",
		"size": "1024x1024",
		"resolution": "2k",
		"image_urls": ["https://a.png"],
		"quality": "high",
		"n": 4,
		"seed": 7,
		"version": "sunburst",
		"vendor_model_id": "hacked",
		"body": {"model": "hacked"}
	}`

	var req supplier.ImageGenerateRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("解析标准字段失败: %v", err)
	}

	if req.VendorModelID != "" {
		t.Errorf("调用方不该能注入 VendorModelID，实际 %q", req.VendorModelID)
	}
	if req.Model != "gpt-image-2.5-flare-ext" || req.Prompt != "a cat" ||
		req.Size != "1024x1024" || req.Resolution != "2k" || len(req.ImageURLs) != 1 {
		t.Fatalf("标准字段解析结果不符: %+v", req)
	}

	params := req.CreditParams()
	if len(params) != 4 {
		t.Errorf("参与计费的字段集不符: %v", params)
	}
	for _, name := range []string{"quality", "n", "seed", "version", "vendor_model_id"} {
		if _, ok := params[name]; ok {
			t.Errorf("非标准字段 %s 不该参与计费", name)
		}
	}
}

// ── countRefImages：参考图张数统计 ──

func TestCountRefImagesCountsURLs(t *testing.T) {
	cases := []struct {
		name      string
		imageURLs []string
		want      int
	}{
		{"无参考图", nil, 0},
		{"空数组", []string{}, 0},
		{"单个", []string{"https://a.png"}, 1},
		{"多个", []string{"https://a.png", "https://b.png", "https://c.png"}, 3},
		{"空串是占位没填，不算一张", []string{"https://a.png", "", "https://b.png"}, 2},
		{"全是空串", []string{"", ""}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countRefImages(tc.imageURLs); got != tc.want {
				t.Errorf("countRefImages(%v) = %d, want %d", tc.imageURLs, got, tc.want)
			}
		})
	}
}

// 同一张图重复出现只计一次：否则调用方复制一遍 URL 就会被多扣一次。
func TestCountRefImagesDeduplicates(t *testing.T) {
	urls := []string{"https://a.png", "https://b.png", "https://a.png"}

	if got := countRefImages(urls); got != 2 {
		t.Errorf("countRefImages(%v) = %d, want 2（去重后）", urls, got)
	}
}

// 请求体目前没有大小限制，一个上万元素的数组会产生荒谬的扣费，必须封顶。
func TestCountRefImagesClampsToMax(t *testing.T) {
	huge := make([]string, maxRefImagesPerRequest+50)
	for i := range huge {
		huge[i] = "https://example.com/" + strconv.Itoa(i) + ".png"
	}

	if got := countRefImages(huge); got != maxRefImagesPerRequest {
		t.Errorf("超限时应封顶到 %d，实际 %d", maxRefImagesPerRequest, got)
	}
}

// 恰好等于上限时不应触发封顶，也不应少计
func TestCountRefImagesAtMaxBoundary(t *testing.T) {
	exact := make([]string, maxRefImagesPerRequest)
	for i := range exact {
		exact[i] = "https://example.com/" + strconv.Itoa(i) + ".png"
	}

	if got := countRefImages(exact); got != maxRefImagesPerRequest {
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

// 快照是调用方视角：只含标准字段，供应商侧模型 ID 不能落库。
func TestMarshalRequestBodyExcludesVendorModelID(t *testing.T) {
	req := supplier.ImageGenerateRequest{
		Model:         "gpt-image-2.5-flare-ext",
		Prompt:        "a cat",
		Size:          "1024x1024",
		Resolution:    "2k",
		ImageURLs:     []string{"https://a.png"},
		VendorModelID: "gpt-image-2.5-ext",
	}

	got := marshalRequestBody(req)

	if strings.Contains(got, `"gpt-image-2.5-ext"`) {
		t.Errorf("快照不应包含供应商侧模型 ID: %s", got)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("快照应为合法 JSON: %v", err)
	}
	if len(decoded) != 5 {
		t.Fatalf("快照字段集不符: %v", decoded)
	}
	for _, k := range []string{"model", "prompt", "size", "resolution", "image_urls"} {
		if _, ok := decoded[k]; !ok {
			t.Errorf("快照缺少标准字段 %s", k)
		}
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
	if got != "图像生成任务(基础1+参考图2张)" {
		t.Errorf("备注格式不符: %q", got)
	}
}

// 备注是审计记录，位数写死会和实际扣费对不上：
// 3 位精度下 0.001 写成 "0.00" 就是在说这次没扣钱。
func TestCreditResolutionRemarkKeepsSmallAmounts(t *testing.T) {
	got := (creditResolution{Total: 0.003, Base: 0.001, RefImageCount: 2}).Remark()
	if got != "图像生成任务(基础0.001+参考图2张)" {
		t.Errorf("小数额备注被截断: %q", got)
	}
}
