package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/FutureAI-X/futureai-api/imageparam"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/FutureAI-X/futureai-api/supplier"
	"github.com/gin-gonic/gin"
)

// ── 请求标准：非标准字段一律忽略 ──

// 标准之外的字段不能被解析进来：它们既进不了计费条件，也进不了快照和上游请求。
// VendorModelID 尤其重要——它由服务端填充，调用方注入就等于绕过模型映射。
func TestImageGenerateRequestParsesStandardFieldsAndIgnoresOthers(t *testing.T) {
	body := `{
		"model": "gpt-image-2.6-flare-ext",
		"prompt": "a cat",
		"size": "1024x1024",
		"resolution": "2k",
		"quality": "high",
		"output_format": "webp",
		"complexity": "medium",
		"image_urls": ["https://a.png"],
		"aspect_ratio": "16:9",
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
	if req.Model != "gpt-image-2.6-flare-ext" || req.Prompt != "a cat" ||
		req.Size != "1024x1024" || req.Resolution != "2k" || len(req.ImageURLs) != 1 {
		t.Fatalf("标准字段解析结果不符: %+v", req)
	}
	if req.Quality != "high" || req.OutputFormat != "webp" || req.Complexity != "medium" {
		t.Fatalf("可选标准字段解析结果不符: %+v", req)
	}

	params := req.CreditParams()
	if len(params) != 4 {
		t.Errorf("参与计费的字段集不符: %v", params)
	}
	for _, name := range []string{"n", "seed", "version", "vendor_model_id", "aspect_ratio"} {
		if _, ok := params[name]; ok {
			t.Errorf("非标准字段 %s 不该参与计费", name)
		}
	}
	// quality / output_format / complexity 是请求字段，但不是计费维度：
	// 单张价格只由 version × resolution 决定。
	for _, name := range []string{"quality", "output_format", "complexity"} {
		if _, ok := params[name]; ok {
			t.Errorf("可选参数 %s 不该参与计费", name)
		}
	}
}

// 规格没有声明的可选字段必须被清空：它们不能进入快照，也不能传给上游。
// 2.5 / gpt-image-2 / MOSS 的规格都没有 quality / output_format / complexity。
func TestClearUnsupportedImageParams(t *testing.T) {
	spec, err := imageparam.ParseSpec(`{
		"params": [
			{"key": "prompt", "widget": "textarea"},
			{"key": "quality", "widget": "segmented", "options": ["auto", "high"]}
		]
	}`)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}

	req := supplier.ImageGenerateRequest{
		Resolution:   "2K",
		Quality:      "high",
		OutputFormat: "webp",
		Complexity:   "low",
	}

	clearUnsupportedImageParams(&req, spec)

	if req.Quality != "high" {
		t.Errorf("规格声明的 quality 被误清空: %q", req.Quality)
	}
	if req.Resolution != "" || req.OutputFormat != "" || req.Complexity != "" {
		t.Errorf("规格未声明的字段未清空: resolution=%q output_format=%q complexity=%q",
			req.Resolution, req.OutputFormat, req.Complexity)
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

// ── image_ids：file_id 参考图路径 ──

// 空数组必须回退 image_urls：客户端先发 image_ids: [] 占位、
// 后续迁移到 file_id 时不应把现有的 URL 路径弄坏。
func TestUseImageFileIDsOnlyWhenNonEmpty(t *testing.T) {
	cases := []struct {
		name string
		ids  []string
		want bool
	}{
		{"未提供", nil, false},
		{"空数组回退 image_urls", []string{}, false},
		{"非空生效", []string{"file_abc"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := useImageFileIDs(tc.ids); got != tc.want {
				t.Errorf("useImageFileIDs(%v) = %v, want %v", tc.ids, got, tc.want)
			}
		})
	}
}

func TestValidateImageFileIDs(t *testing.T) {
	ids := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "file_" + strconv.Itoa(i)
		}
		return out
	}

	cases := []struct {
		name    string
		ids     []string
		wantErr bool
	}{
		{"nil 合法", nil, false},
		// 数量上限属于模型属性，由供应商在扣费前按 req.Model 校验，
		// 这里只做形态校验，不能把某个模型的上限写死成全员上限。
		{"多张通过形态校验", ids(50), false},
		{"空元素", []string{"file_a", "   "}, true},
		{"URL 形式", []string{"https://example.com/a.png"}, true},
		{"前缀不符", []string{"abc"}, true},
		{"超长", []string{"file_" + strings.Repeat("a", model.MaxFileIDLen)}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateImageFileIDs(tc.ids)
			if tc.wantErr && err == nil {
				t.Errorf("validateImageFileIDs(%v) = nil, want error", tc.ids)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateImageFileIDs(%v) = %v, want nil", tc.ids, err)
			}
		})
	}
}

func TestCountRefImageIDsDeduplicates(t *testing.T) {
	got := countRefImageIDs([]string{"file_a", "file_b", "file_a", " file_b "})
	if got != 2 {
		t.Errorf("countRefImageIDs 去重后 = %d, want 2", got)
	}
}

// 计费口径跟随生效字段：image_ids 非空时 URL 必须被忽略，
// 否则调用方可以塞一堆 URL 来虚增（或未来被误算）参考图张数。
func TestEffectiveRefImageCountFollowsEffectiveField(t *testing.T) {
	urls := []string{"https://a.png", "https://b.png"}

	cases := []struct {
		name string
		req  supplier.ImageGenerateRequest
		want int
	}{
		{"无 file_id 按 URL 计数", supplier.ImageGenerateRequest{ImageURLs: urls}, 2},
		{"file_id 非空时忽略 URL", supplier.ImageGenerateRequest{
			ImageURLs: urls, ImageIDs: []string{"file_a"},
		}, 1},
		{"file_id 空数组回退 URL", supplier.ImageGenerateRequest{
			ImageURLs: urls, ImageIDs: []string{},
		}, 2},
		{"file_id 去重计数", supplier.ImageGenerateRequest{
			ImageIDs: []string{"file_a", "file_a", "file_b"},
		}, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveRefImageCount(tc.req); got != tc.want {
				t.Errorf("effectiveRefImageCount(%+v) = %d, want %d", tc.req, got, tc.want)
			}
		})
	}
}

// 快照要保留调用方原始的两个参考图字段，签名 URL 解析发生在快照之后。
func TestMarshalRequestBodyKeepsOriginalReferenceFields(t *testing.T) {
	req := supplier.ImageGenerateRequest{
		Model:     "m",
		ImageURLs: []string{"https://a.png"},
		ImageIDs:  []string{"file_a"},
	}

	got := marshalRequestBody(req)

	for _, want := range []string{`"image_ids":["file_a"]`, `"image_urls":["https://a.png"]`} {
		if !strings.Contains(got, want) {
			t.Errorf("快照缺少 %s: %s", want, got)
		}
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
	if len(decoded) != 6 {
		t.Fatalf("快照字段集不符: %v", decoded)
	}
	for _, k := range []string{"model", "prompt", "size", "resolution", "image_urls", "image_ids"} {
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

// ── 幂等键解析 ──

func TestParseIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name      string
		header    string
		wantKey   *string
		wantValid bool
	}{
		{"未提供", "", nil, true},
		{"仅空白视为未提供", "   ", nil, true},
		{"正常值", "req-abc-123", strPtr("req-abc-123"), true},
		{"首尾空白被裁剪", "  req-1  ", strPtr("req-1"), true},
		{"超长非法", strings.Repeat("a", maxIdempotencyKeyLen+1), nil, false},
		{"恰好上限合法", strings.Repeat("a", maxIdempotencyKeyLen), strPtr(strings.Repeat("a", maxIdempotencyKeyLen)), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			if tc.header != "" {
				c.Request.Header.Set("Idempotency-Key", tc.header)
			}

			key, valid := parseIdempotencyKey(c)

			if valid != tc.wantValid {
				t.Fatalf("valid = %v, want %v", valid, tc.wantValid)
			}
			if tc.wantKey == nil {
				if key != nil {
					t.Errorf("key = %q, want nil", *key)
				}
				return
			}
			if key == nil {
				t.Fatal("key = nil, want 非空")
			}
			if *key != *tc.wantKey {
				t.Errorf("key = %q, want %q", *key, *tc.wantKey)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

// 新 SLA：图片 15 分钟、视频 30 分钟；提交确认窗口必须小于结果截止时间。
func TestTaskDeadlines(t *testing.T) {
	if imageResultDeadline != 15*time.Minute {
		t.Errorf("imageResultDeadline = %s, want 15m", imageResultDeadline)
	}
	if videoResultDeadline != 30*time.Minute {
		t.Errorf("videoResultDeadline = %s, want 30m", videoResultDeadline)
	}
	if asyncSubmitConfirmWindow <= 0 || asyncSubmitConfirmWindow >= imageResultDeadline {
		t.Errorf("asyncSubmitConfirmWindow = %s，必须大于 0 且小于图片截止时间", asyncSubmitConfirmWindow)
	}
	if backgroundSubmitWindow < 15*time.Minute {
		t.Errorf("backgroundSubmitWindow = %s，不能小于 MOSS 的 15 分钟上游超时", backgroundSubmitWindow)
	}
}

// 并发查询上限不能超过批量大小，否则一轮里永远用不满并发
func TestReconcileConcurrencyFitsInBatch(t *testing.T) {
	if maxConcurrentQueries > reconcileBatchSize {
		t.Errorf("maxConcurrentQueries(%d) 大于 reconcileBatchSize(%d)，一轮用不满并发",
			maxConcurrentQueries, reconcileBatchSize)
	}
}

// ── completed 但没有产出：必须能识别出来并退款 ──

// 上游返回 status=completed 而结果为空时（内容审核拦截、上游故障），
// 不能当成成功：completed 是终态、不会被任何对账查询捡起，
// 误判的代价是用户拿不到结果而积分永远扣着。
func TestHasDeliverableResult(t *testing.T) {
	cases := []struct {
		name string
		data map[string]interface{}
		want bool
	}{
		{"apimart 有 url", map[string]interface{}{"url": "https://x/1.png", "b64_json": ""}, true},
		{"apimart 只有 b64", map[string]interface{}{"url": "", "b64_json": "aGk="}, true},
		{"apimart 两个都空", map[string]interface{}{"url": "", "b64_json": ""}, false},
		{"空白字符不算有内容", map[string]interface{}{"url": "   "}, false},
		{"键存在但为 null", map[string]interface{}{"url": nil}, false},
		{"完全空 map", map[string]interface{}{}, false},
		{"nil map", nil, false},
		{"数组形态", map[string]interface{}{"images": []interface{}{"https://x/1.png"}}, true},
		{"只有元数据字段不算交付", map[string]interface{}{"url": "", "revised_prompt": "更详细的提示词"}, false},
		{"视频有 url", map[string]interface{}{"url": "https://x/1.mp4"}, true},
		{"视频 url 为空", map[string]interface{}{"url": ""}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasDeliverableResult(tc.data); got != tc.want {
				t.Errorf("hasDeliverableResult(%v) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

// 任务状态只剩 processing/completed/failed，终态判断只认后两者。
func TestTaskTerminalStatuses(t *testing.T) {
	if !model.IsTerminalStatus(model.StatusCompleted) {
		t.Errorf("completed 应为终态")
	}
	if !model.IsTerminalStatus(model.StatusFailed) {
		t.Errorf("failed 应为终态")
	}
	if model.IsTerminalStatus(model.StatusProcessing) {
		t.Errorf("processing 不应为终态")
	}
}

// size / resolution 的大小写不敏感，避免历史小写规则在统一为大写后静默失效。
func TestCreditParamValuesEqual(t *testing.T) {
	cases := []struct {
		name     string
		actual   string
		expected string
		want     bool
	}{
		{"resolution", "2K", "2k", true},
		{"resolution", "2K", "2K", true},
		{"resolution", "2K", "4K", false},
		{"size", "1024x1024", "1024X1024", true},
		{"model", "gpt-image-2.5-ext", "GPT-IMAGE-2.5-EXT", false},
		{"prompt", "Cat", "cat", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := creditParamValuesEqual(tc.name, tc.actual, tc.expected); got != tc.want {
				t.Errorf("creditParamValuesEqual(%q, %q, %q) = %v, want %v",
					tc.name, tc.actual, tc.expected, got, tc.want)
			}
		})
	}
}
