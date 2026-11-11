package supplier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
)

// ── ApplyDefaults：标准字段默认值 ──

func TestApplyDefaultsFillsOmittedFields(t *testing.T) {
	var req ImageGenerateRequest
	req.ApplyDefaults()

	if req.Resolution != "1K" {
		t.Errorf("resolution 默认值 = %q, want 1K", req.Resolution)
	}
	if req.Size != "16:9" {
		t.Errorf("size 默认值 = %q, want 16:9", req.Size)
	}
}

// 调用方显式传的值必须原样保留（大小写归一化除外），默认值只补空缺。
func TestApplyDefaultsKeepsExplicitValues(t *testing.T) {
	req := ImageGenerateRequest{Resolution: "4K", Size: "1:1"}
	req.ApplyDefaults()

	if req.Resolution != "4K" || req.Size != "1:1" {
		t.Errorf("显式取值被覆盖: resolution=%q, size=%q", req.Resolution, req.Size)
	}
}

// 对外分辨率统一为大写；历史客户端继续传小写时在入口归一化，而不是让
// 每个供应商各自处理大小写。
func TestApplyDefaultsNormalizesResolutionToUpper(t *testing.T) {
	req := ImageGenerateRequest{Resolution: "2k"}
	req.ApplyDefaults()

	if req.Resolution != "2K" {
		t.Errorf("resolution = %q, want 2K", req.Resolution)
	}
}

// 幂等：重复调用不应改变结果（controller 与供应商两侧都会经过默认值逻辑）。
func TestApplyDefaultsIsIdempotent(t *testing.T) {
	var req ImageGenerateRequest
	req.ApplyDefaults()
	size, resolution := req.Size, req.Resolution

	req.ApplyDefaults()

	if req.Size != size || req.Resolution != resolution {
		t.Errorf("重复调用改变了结果: size=%q, resolution=%q，want %q / %q",
			req.Size, req.Resolution, size, resolution)
	}
}

// 默认值必须在计费之前补齐，否则省略 resolution 的请求计的是基础价、
// 实际却按 1K 出图。这里锁住「补齐后计费条件能命中默认值」这一行为。
func TestApplyDefaultsFeedsCreditParams(t *testing.T) {
	var req ImageGenerateRequest
	req.ApplyDefaults()

	params := req.CreditParams()

	// 未补齐时 params["resolution"] 是空串，永远匹配不上 resolution=1K 的条件
	if params["resolution"] != "1K" {
		t.Errorf("计费字段 resolution = %q, want 1K（默认值须在计费前补齐）", params["resolution"])
	}
	if params["size"] != "16:9" {
		t.Errorf("计费字段 size = %q, want 16:9", params["size"])
	}
}

// ── 计费字段名清单 ──

// CreditParams 的键与 CreditParamNames 必须一致：管理端按后者校验 param_path，
// 两边漏改就会出现「配了却永远匹配不上」的静默失效。
func TestCreditParamNamesMatchCreditParamsKeys(t *testing.T) {
	names := CreditParamNames()
	sort.Strings(names)

	keys := make([]string, 0, len(names))
	for k := range (ImageGenerateRequest{}).CreditParams() {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if len(names) != len(keys) {
		t.Fatalf("字段数不一致: CreditParamNames=%v, CreditParams=%v", names, keys)
	}
	for i := range names {
		if names[i] != keys[i] {
			t.Errorf("第 %d 项不一致: CreditParamNames=%v, CreditParams=%v", i, names, keys)
			break
		}
	}
}

// 返回副本：调用方改了不该影响全局清单。
func TestCreditParamNamesReturnsCopy(t *testing.T) {
	names := CreditParamNames()
	names[0] = "hacked"

	if IsCreditParamName("hacked") {
		t.Error("修改返回值污染了全局字段清单")
	}
	if !IsCreditParamName("model") {
		t.Error("标准字段 model 应被识别")
	}
}

// 数组字段、可选参数与任意名字都不参与计费：配上去永远匹配不上。
func TestIsCreditParamNameRejectsNonBillingFields(t *testing.T) {
	for _, name := range []string{"image_urls", "quality", "n", "seed", "version", "", "Model"} {
		if IsCreditParamName(name) {
			t.Errorf("%q 不该被当作可计费的标准字段", name)
		}
	}
}

// ── applyImageVersion：按平台模型规格补全 APIMart 的 version 字段 ──

// 测试用的平台模型名与供应商模型 ID。
const (
	testAPIMartModel         = "gpt-image-2.5-flare-ext"
	testAPIMartVendorModelID = "gpt-image-2.5-ext"
)

func TestApplyImageVersionSetsVersionByRequestedModel(t *testing.T) {
	cases := []struct {
		name           string
		requestedModel string
		wantVersion    interface{}
		wantSet        bool
	}{
		{"flare 变体", "gpt-image-2.5-flare-ext", "flare", true},
		{"sunburst 变体", "gpt-image-2.5-sunburst-ext", "sunburst", true},
		{"2.6 flare 变体", "gpt-image-2.6-flare-ext", "flare", true},
		{"2.6 sunburst 变体", "gpt-image-2.6-sunburst-ext", "sunburst", true},
		{"未登记的调用方模型不写 version", "gpt-image-2.5-unknown-ext", nil, false},
		{"调用方模型名为空不写 version", "", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]interface{}{
				"model":  testAPIMartVendorModelID,
				"prompt": "a cat",
			}

			applyImageVersion(body, tc.requestedModel)

			got, set := body["version"]
			if set != tc.wantSet {
				t.Fatalf("version 是否写入 = %v, want %v (body=%v)", set, tc.wantSet, body)
			}
			if got != tc.wantVersion {
				t.Errorf("version = %v, want %v", got, tc.wantVersion)
			}
		})
	}
}

// 未登记的模型不会命中任何规格，不能往请求体里塞多余的 version 字段。
func TestApplyImageVersionLeavesUnregisteredModelsUntouched(t *testing.T) {
	body := map[string]interface{}{
		"model":   testAPIMartVendorModelID,
		"prompt":  "a cat",
		"version": "caller-supplied",
	}

	applyImageVersion(body, "gpt-image-2.5-unknown-ext")

	if got := body["version"]; got != "caller-supplied" {
		t.Errorf("未登记模型的 version 被改写为 %v, want 原样透传", got)
	}
	if len(body) != 3 {
		t.Errorf("请求体被改动了: %v", body)
	}
}

// ── buildRequest：标准字段 → APIMart 线上请求体 ──

// 标准字段逐个落到线上格式，模型发的是供应商侧 ID，不是调用方模型名。
func TestBuildRequestMapsStandardFields(t *testing.T) {
	body := buildRequest(ImageGenerateRequest{
		Model:         "gpt-image-2.5-sunburst-ext",
		Prompt:        "a cat",
		Size:          "16:9",
		Resolution:    "2k",
		ImageURLs:     []string{"https://a.png", "https://b.png"},
		VendorModelID: testAPIMartVendorModelID,
	})

	wants := map[string]interface{}{
		"model":      testAPIMartVendorModelID,
		"prompt":     "a cat",
		"size":       "16:9",
		"resolution": "2K",
		"n":          1,
		"version":    "sunburst",
	}
	for k, want := range wants {
		if got := body[k]; got != want {
			t.Errorf("%s = %v, want %v（请求体=%v）", k, got, want, body)
		}
	}

	urls, ok := body["image_urls"].([]string)
	if !ok || len(urls) != 2 {
		t.Errorf("image_urls = %#v, want 两个 URL", body["image_urls"])
	}
	if len(body) != len(wants)+1 {
		t.Errorf("请求体多出未预期的字段: %v", body)
	}
}

// 2.6 的可选参数按小写写入请求体；空值不写，避免覆盖上游默认行为。
func TestBuildRequestMapsGPTImage26OptionalFields(t *testing.T) {
	body := buildRequest(ImageGenerateRequest{
		Model:         "gpt-image-2.6-flare-ext",
		Prompt:        "a cat",
		Size:          "16:9",
		Resolution:    "2K",
		Quality:       "HIGH",
		OutputFormat:  "WEBP",
		Complexity:    "Medium",
		VendorModelID: "gpt-image-2.6-ext",
	})

	wants := map[string]interface{}{
		"model":         "gpt-image-2.6-ext",
		"version":       "flare",
		"resolution":    "2k",
		"quality":       "high",
		"output_format": "webp",
		"complexity":    "medium",
		"n":             1,
	}
	for k, want := range wants {
		if got := body[k]; got != want {
			t.Errorf("%s = %v, want %v（请求体=%v）", k, got, want, body)
		}
	}

	// 可选参数为空时不写入；complexity 不设默认值，用户不选就由上游决定。
	empty := buildRequest(ImageGenerateRequest{
		Model:         "gpt-image-2.6-flare-ext",
		Prompt:        "a cat",
		Size:          "1:1",
		Resolution:    "1K",
		VendorModelID: "gpt-image-2.6-ext",
	})
	for _, key := range []string{"quality", "output_format", "complexity"} {
		if _, ok := empty[key]; ok {
			t.Errorf("%s 为空时不该写入请求体: %v", key, empty)
		}
	}
}

// image_urls 为空时不写入；size / resolution 一律写入——默认值已在计费前补齐，
// 发空串反而会让上游当作未指定，绕开标准默认值。
func TestBuildRequestAlwaysWritesSizeAndResolution(t *testing.T) {
	var req ImageGenerateRequest
	req.ApplyDefaults()
	req.Model = "gpt-image-1" // 未登记模型：没有 version，但 size/resolution/n 仍要写入
	req.Prompt = "a cat"
	req.VendorModelID = "gpt-image-1"

	body := buildRequest(req)

	if body["size"] != DefaultSize {
		t.Errorf("size = %v, want %s", body["size"], DefaultSize)
	}
	if body["resolution"] != "1K" {
		t.Errorf("resolution = %v, want 1K", body["resolution"])
	}
	if body["n"] != 1 {
		t.Errorf("n = %v, want 1", body["n"])
	}
	if _, ok := body["image_urls"]; ok {
		t.Error("image_urls 为空时不该写入请求体")
	}
	if _, ok := body["version"]; ok {
		t.Error("非目标模型不该写入 version")
	}
}

// 端到端校验：发往 APIMart 的 JSON 就是标准字段翻译出来的结果。
func TestImageGenerateSendsStandardFields(t *testing.T) {
	var received map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("请求路径 = %s, want /v1/images/generations", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("解析上游收到的请求体失败: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":200,"data":[{"status":"submitted","task_id":"vendor-task-1"}]}`)
	}))
	defer srv.Close()

	// 注入 httptest 的 client 绕过 SSRF 防护：
	// SafeDialContext 会拒绝测试服务器所在的回环地址。
	common.SetOutboundClientForTest(srv.Client())
	defer common.SetOutboundClientForTest(nil)

	a := &apimart{cfg: Config{BaseURL: srv.URL, APIKey: "test-key"}}

	// 默认值由 controller 在计费前补齐，这里按同一条路径构造请求。
	var req ImageGenerateRequest
	req.Model = "gpt-image-2.5-sunburst-ext"
	req.Prompt = "a cat"
	req.ImageURLs = []string{"https://a.png"}
	req.VendorModelID = testAPIMartVendorModelID
	req.ApplyDefaults()

	resp := a.ImageGenerate(context.Background(), req)

	if resp.Code != "success" {
		t.Fatalf("resp.Code = %s, want success", resp.Code)
	}
	if resp.Data["taskId"] != "vendor-task-1" {
		t.Errorf("taskId = %v, want vendor-task-1", resp.Data["taskId"])
	}
	if got := received["version"]; got != "sunburst" {
		t.Errorf("上游收到的 version = %v, want sunburst（请求体=%v）", got, received)
	}
	if got := received["model"]; got != testAPIMartVendorModelID {
		t.Errorf("上游收到的 model = %v, want %s", got, testAPIMartVendorModelID)
	}
}

// ── 失败性质分类：直接决定控制器是否退款 ──
//
// 这组测试守的是最贵的一条边界：把「不知道结果」误判成「上游没受理」
// 会让用户在平台已经付过上游成本的情况下被退款——平台净损失。

// newTestAPIMart 起一个行为由 handler 决定的假上游，并注入其 client。
func newTestAPIMart(t *testing.T, handler http.HandlerFunc) (*apimart, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	common.SetOutboundClientForTest(srv.Client())
	return &apimart{cfg: Config{BaseURL: srv.URL, APIKey: "test-key"}}, func() {
		common.SetOutboundClientForTest(nil)
		srv.Close()
	}
}

func testImageReq() ImageGenerateRequest {
	req := ImageGenerateRequest{Model: testAPIMartModel, Prompt: "a cat", VendorModelID: testAPIMartVendorModelID}
	req.ApplyDefaults()
	return req
}

// 超时：请求很可能已经送达并被受理，绝不能当成「上游没接单」去退款。
func TestImageGenerateTimeoutIsUnknownNotRejected(t *testing.T) {
	a, cleanup := newTestAPIMart(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	resp := a.ImageGenerate(ctx, testImageReq())

	if resp.Code != "fail" {
		t.Fatalf("resp.Code = %q, want fail", resp.Code)
	}
	if resp.FailureKind != FailureUnknown {
		t.Errorf("超时必须归类为 %q（上游可能已受理，不能退款），实际 %q",
			FailureUnknown, resp.FailureKind)
	}
}

// 4xx：上游明确拒绝，本次请求未被受理，可以安全退款。
func TestImageGenerate4xxIsRejected(t *testing.T) {
	a, cleanup := newTestAPIMart(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"bad model"}`)
	})
	defer cleanup()

	resp := a.ImageGenerate(context.Background(), testImageReq())

	if resp.FailureKind != FailureRejected {
		t.Errorf("HTTP 4xx 必须归类为 %q（可退款），实际 %q", FailureRejected, resp.FailureKind)
	}
}

// 5xx：可能来自上游前面的网关，请求或许已经落地并建单，只能算不确定。
func TestImageGenerate5xxIsUnknown(t *testing.T) {
	a, cleanup := newTestAPIMart(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	defer cleanup()

	resp := a.ImageGenerate(context.Background(), testImageReq())

	if resp.FailureKind != FailureUnknown {
		t.Errorf("HTTP 5xx 必须归类为 %q（不得退款），实际 %q", FailureUnknown, resp.FailureKind)
	}
}

// 响应体无法解析：常见成因是超过读取上限被截断，
// 此时上游其实已经建单，只是我们读不完整。
func TestImageGenerateUnparseableBodyIsUnknown(t *testing.T) {
	a, cleanup := newTestAPIMart(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":200,"data":[{"task_id":"t1"`) // 截断的 JSON
	})
	defer cleanup()

	resp := a.ImageGenerate(context.Background(), testImageReq())

	if resp.FailureKind != FailureUnknown {
		t.Errorf("响应无法解析必须归类为 %q（不得退款），实际 %q", FailureUnknown, resp.FailureKind)
	}
}

// 上游返回了结构完整的失败业务码：它明确告诉我们没有受理，可以退款。
func TestImageGenerateBusinessFailureIsRejected(t *testing.T) {
	a, cleanup := newTestAPIMart(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":400,"data":null}`)
	})
	defer cleanup()

	resp := a.ImageGenerate(context.Background(), testImageReq())

	if resp.FailureKind != FailureRejected {
		t.Errorf("业务失败码应归类为 %q（可退款），实际 %q", FailureRejected, resp.FailureKind)
	}
}

// 上游成功但没有 task_id：任务很可能已经建了，不能退款。
func TestImageGenerateMissingTaskIDIsUnknown(t *testing.T) {
	a, cleanup := newTestAPIMart(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":200,"data":[]}`)
	})
	defer cleanup()

	resp := a.ImageGenerate(context.Background(), testImageReq())

	if resp.FailureKind != FailureUnknown {
		t.Errorf("缺少 task_id 必须归类为 %q（不得退款），实际 %q", FailureUnknown, resp.FailureKind)
	}
}

// ── 查询 URL 注入防护 ──

// taskId 来自上游响应且会被拼进查询 URL，必须限制字符集，
// 否则一个被控的上游就能带着 API Key 去请求该主机的任意路径。
func TestTaskQueryRejectsMalformedTaskID(t *testing.T) {
	hits := 0
	a, cleanup := newTestAPIMart(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `{"code":200,"data":{"id":"t","status":"completed"}}`)
	})
	defer cleanup()

	bad := []string{
		"../../etc/passwd",
		"a/../b",
		"a b",
		"a?language=en",
		"a#frag",
		strings.Repeat("a", 65),
		"",
	}
	for _, id := range bad {
		resp := a.TaskQuery(context.Background(), `{"taskId":"`+id+`"}`)
		if resp.Status != "call_fail" {
			t.Errorf("taskId %q 应被拒绝（call_fail），实际 status=%q", id, resp.Status)
		}
	}

	if hits != 0 {
		t.Errorf("非法 taskId 不应发出任何上游请求，实际发出 %d 次", hits)
	}
}

// 合法的 taskId 仍要正常工作，别把校验做成过严
func TestTaskQueryAcceptsWellFormedTaskID(t *testing.T) {
	a, cleanup := newTestAPIMart(t, func(w http.ResponseWriter, r *http.Request) {
		if want := "/v1/tasks/abc-123_XYZ"; r.URL.Path != want {
			t.Errorf("查询路径 = %q, want %q", r.URL.Path, want)
		}
		fmt.Fprint(w, `{"code":200,"data":{"id":"abc-123_XYZ","status":"completed","result":{"images":[{"url":["https://img/1.png"]}]}}}`)
	})
	defer cleanup()

	resp := a.TaskQuery(context.Background(), `{"taskId":"abc-123_XYZ"}`)

	if resp.Status != "completed" {
		t.Fatalf("status = %q, want completed", resp.Status)
	}
	if resp.Data["url"] != "https://img/1.png" {
		t.Errorf("url = %v, want https://img/1.png", resp.Data["url"])
	}
}

// 上游成功固定是 HTTP 200 + data[].task_id。
// 同时锁住「不向上游传 Idempotency-Key / X-APIMart-Response-Version」：
// 带上响应版本头会让上游改用 202 + data.id 的新格式。
func TestImageGenerateSendsNoVersionOrIdempotencyHeaders(t *testing.T) {
	var gotVersion, gotIdempotency string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.Header.Get("X-APIMart-Response-Version")
		gotIdempotency = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":200,"data":[{"status":"submitted","task_id":"task_200"}]}`)
	}))
	defer srv.Close()

	common.SetOutboundClientForTest(srv.Client())
	defer common.SetOutboundClientForTest(nil)

	a := &apimart{cfg: Config{BaseURL: srv.URL, APIKey: "test-key"}}
	resp := a.ImageGenerate(context.Background(), ImageGenerateRequest{
		Model:         "gpt-image-2.5-flare-ext",
		Prompt:        "a cat",
		Size:          "1:1",
		Resolution:    "1k",
		VendorModelID: testAPIMartVendorModelID,
	})

	if resp.Code != "success" {
		t.Fatalf("Code = %q, want success (kind %q)", resp.Code, resp.FailureKind)
	}
	if resp.Data["taskId"] != "task_200" {
		t.Errorf("taskId = %v, want task_200", resp.Data["taskId"])
	}
	if gotVersion != "" {
		t.Errorf("X-APIMart-Response-Version = %q, want 不传", gotVersion)
	}
	if gotIdempotency != "" {
		t.Errorf("Idempotency-Key = %q, want 不传", gotIdempotency)
	}
}

// 意外出现的 202 不是协议内的成功：上游可能已经建单，只能算结果不确定，
// 绝不能当成明确拒绝去退款。
func TestImageGenerateUnexpected202IsUnknown(t *testing.T) {
	a, cleanup := newTestAPIMart(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"code":202,"data":{"id":"task_202"}}`)
	})
	defer cleanup()

	resp := a.ImageGenerate(context.Background(), testImageReq())

	if resp.FailureKind != FailureUnknown {
		t.Errorf("HTTP 202 必须归类为 %q（不得退款），实际 %q", FailureUnknown, resp.FailureKind)
	}
}

func TestAPIMartValidateImageRequest(t *testing.T) {
	fileIDs := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("file_%d", i)
		}
		return out
	}
	urls := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("https://example.com/%d.png", i)
		}
		return out
	}

	ids16, urls16 := fileIDs(16), urls(16)
	ids17, urls17 := fileIDs(17), urls(17)
	ids15, urls15 := fileIDs(15), urls(15)
	ids10, urls10 := fileIDs(10), urls(10)
	ids11, urls11 := fileIDs(11), urls(11)

	tests := []struct {
		name    string
		req     ImageGenerateRequest
		wantErr bool
	}{
		// gpt-image-2.5-ext：10 种比例、16 张参考图、resolution 大写
		{"2.5 合法比例", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "16:9", Resolution: "2k"}, false},
		{"2.5 sunburst 变体同样可用", ImageGenerateRequest{Model: "gpt-image-2.5-sunburst-ext", Prompt: "cat", Size: "16:9", Resolution: "2k"}, false},
		{"2.5 上限 16 张 image_ids", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "1:1", Resolution: "1k", ImageIDs: ids16}, false},
		{"2.5 上限 16 张 image_urls", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "1:1", Resolution: "1k", ImageURLs: urls16}, false},
		{"2.5 image_ids 优先于 image_urls", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "1:1", Resolution: "1k", ImageIDs: ids16, ImageURLs: urls17}, false},
		// auto 的开放由 param_spec 决定，供应商层不再拒绝。
		{"2.5 支持 auto", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "auto", Resolution: "4k"}, false},
		{"2.5 不支持 3:1", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "3:1", Resolution: "4k"}, true},
		{"2.5 不支持 9:21", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "9:21", Resolution: "1k"}, true},
		{"2.5 超限 17 张 image_ids", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "1:1", Resolution: "1k", ImageIDs: ids17}, true},
		{"2.5 超限 17 张 image_urls", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "1:1", Resolution: "1k", ImageURLs: urls17}, true},

		// gpt-image-2.6-ext：10 种比例、16 张参考图、resolution 小写，
		// 并开放 quality / output_format / complexity 三个可选参数。
		{"2.6 flare 合法比例", ImageGenerateRequest{Model: "gpt-image-2.6-flare-ext", Prompt: "cat", Size: "16:9", Resolution: "2k"}, false},
		{"2.6 sunburst 变体同样可用", ImageGenerateRequest{Model: "gpt-image-2.6-sunburst-ext", Prompt: "cat", Size: "21:9", Resolution: "4K"}, false},
		{"2.6 合法可选参数", ImageGenerateRequest{Model: "gpt-image-2.6-flare-ext", Prompt: "cat", Size: "1:1", Resolution: "1k", Quality: "auto", OutputFormat: "webp", Complexity: "high"}, false},
		{"2.6 可选参数大小写不敏感", ImageGenerateRequest{Model: "gpt-image-2.6-flare-ext", Prompt: "cat", Size: "1:1", Resolution: "1k", Quality: "HIGH", OutputFormat: "JPEG", Complexity: "Medium"}, false},
		{"2.6 不支持像素", ImageGenerateRequest{Model: "gpt-image-2.6-flare-ext", Prompt: "cat", Size: "1024x1024", Resolution: "1k"}, true},
		{"2.6 支持 auto", ImageGenerateRequest{Model: "gpt-image-2.6-flare-ext", Prompt: "cat", Size: "auto", Resolution: "1k"}, false},
		{"2.6 不支持 3:1", ImageGenerateRequest{Model: "gpt-image-2.6-flare-ext", Prompt: "cat", Size: "3:1", Resolution: "1k"}, true},
		{"2.6 上限 16 张 image_urls", ImageGenerateRequest{Model: "gpt-image-2.6-flare-ext", Prompt: "cat", Size: "1:1", Resolution: "1k", ImageURLs: urls16}, false},
		{"2.6 超限 17 张 image_urls", ImageGenerateRequest{Model: "gpt-image-2.6-flare-ext", Prompt: "cat", Size: "1:1", Resolution: "1k", ImageURLs: urls17}, true},
		{"2.6 非法 quality", ImageGenerateRequest{Model: "gpt-image-2.6-flare-ext", Prompt: "cat", Size: "1:1", Resolution: "1k", Quality: "ultra"}, true},
		{"2.6 非法 output_format", ImageGenerateRequest{Model: "gpt-image-2.6-flare-ext", Prompt: "cat", Size: "1:1", Resolution: "1k", OutputFormat: "gif"}, true},
		{"2.6 svg 本期不开放", ImageGenerateRequest{Model: "gpt-image-2.6-flare-ext", Prompt: "cat", Size: "1:1", Resolution: "1k", OutputFormat: "svg"}, true},
		{"2.6 非法 complexity", ImageGenerateRequest{Model: "gpt-image-2.6-flare-ext", Prompt: "cat", Size: "1:1", Resolution: "1k", Complexity: "xhigh"}, true},
		{"2.5 不支持 quality", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "1:1", Resolution: "1k", Quality: "high"}, true},
		{"2.5 不支持 output_format", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "1:1", Resolution: "1k", OutputFormat: "webp"}, true},
		{"gpt-image-2 不支持 complexity", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "1:1", Resolution: "1k", Complexity: "low"}, true},

		// gpt-image-2 / gpt-image-2-ext：15 种比例、15 张参考图、resolution 小写
		{"gpt-image-2 合法比例", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "16:9", Resolution: "2K"}, false},
		{"gpt-image-2-ext 别名同样可用", ImageGenerateRequest{Model: "gpt-image-2-ext", Prompt: "cat", Size: "3:1", Resolution: "4k"}, false},
		{"gpt-image-2 支持 9:21", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "9:21", Resolution: "1k"}, false},
		{"gpt-image-2 上限 15 张 image_ids", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "1:1", Resolution: "1k", ImageIDs: ids15}, false},
		{"gpt-image-2 上限 15 张 image_urls", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "1:1", Resolution: "1k", ImageURLs: urls15}, false},
		{"gpt-image-2 支持 auto", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "auto", Resolution: "4k"}, false},
		{"gpt-image-2 超限 16 张 image_ids", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "1:1", Resolution: "1k", ImageIDs: ids16}, true},
		{"gpt-image-2 超限 16 张 image_urls", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "1:1", Resolution: "1k", ImageURLs: urls16}, true},

		// gpt-image-2 / gpt-image-2-ext：同时接受像素尺寸
		{"gpt-image-2 合法像素 1:1", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "1024x1024", Resolution: "2K"}, false},
		{"gpt-image-2-ext 合法像素 16:9", ImageGenerateRequest{Model: "gpt-image-2-ext", Prompt: "cat", Size: "2560x1440", Resolution: "2K"}, false},
		{"gpt-image-2 像素面积上限", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "3840x2160", Resolution: "4K"}, false},
		{"gpt-image-2 正方形面积上限", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "2880x2880", Resolution: "4K"}, false},
		{"gpt-image-2 像素比例下限", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "512x1536", Resolution: "1K"}, false},
		{"gpt-image-2 像素非 16 倍数", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "1000x1000", Resolution: "1K"}, true},
		{"gpt-image-2 像素超过最大边长", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "4096x1024", Resolution: "1K"}, true},
		{"gpt-image-2 像素超过总面积", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "2896x2896", Resolution: "1K"}, true},
		{"gpt-image-2 像素比例过窄", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "512x1552", Resolution: "1K"}, true},
		{"gpt-image-2 像素比例过宽", ImageGenerateRequest{Model: "gpt-image-2", Prompt: "cat", Size: "1552x512", Resolution: "1K"}, true},
		{"gpt-image-2.5 拒绝像素", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "1024x1024", Resolution: "1K"}, true},

		// seedream-5-5：10 种比例、10 张参考图、1K/1.5K/2K、只接受比例
		{"seedream 合法比例", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "16:9", Resolution: "1.5K"}, false},
		{"seedream 21:9 可用", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "21:9", Resolution: "2K"}, false},
		{"seedream 2:1 可用", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "2:1", Resolution: "1K"}, false},
		{"seedream 1:2 可用", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "1:2", Resolution: "1K"}, false},
		{"seedream 上限 10 张 image_ids", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "1:1", Resolution: "1K", ImageIDs: ids10}, false},
		{"seedream 上限 10 张 image_urls", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "1:1", Resolution: "1K", ImageURLs: urls10}, false},
		{"seedream 不支持 4K", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "1:1", Resolution: "4K"}, true},
		{"seedream 不支持 5:4", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "5:4", Resolution: "1K"}, true},
		{"seedream 不支持像素", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "1024x1024", Resolution: "1K"}, true},
		{"seedream 支持 auto", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "auto", Resolution: "1K"}, false},
		{"seedream 超限 11 张 image_ids", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "1:1", Resolution: "1K", ImageIDs: ids11}, true},
		{"seedream 超限 11 张 image_urls", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "1:1", Resolution: "1K", ImageURLs: urls11}, true},
		{"seedream 不支持 quality", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "1:1", Resolution: "1K", Quality: "high"}, true},
		{"seedream 不支持 output_format", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "1:1", Resolution: "1K", OutputFormat: "png"}, true},
		{"seedream 不支持 complexity", ImageGenerateRequest{Model: "seedream-5-5", Prompt: "cat", Size: "1:1", Resolution: "1K", Complexity: "low"}, true},

		// 通用
		{"空 prompt", ImageGenerateRequest{Model: testAPIMartModel, Size: "1:1", Resolution: "1k"}, true},
		{"未登记模型被拒绝", ImageGenerateRequest{Model: "gpt-image-2.5-unknown-ext", Prompt: "cat", Size: "1:1", Resolution: "1k"}, true},
		{"非法 size", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "7:3", Resolution: "1k"}, true},
		{"非法 resolution", ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "1:1", Resolution: "8k"}, true},
	}

	a := &apimart{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := a.ValidateImageRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateImageRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// gpt-image-2 / gpt-image-2-ext 没有风格版本，请求体不能被塞入 version。
func TestBuildRequestOmitsVersionForGPTImage2(t *testing.T) {
	for _, model := range []string{"gpt-image-2", "gpt-image-2-ext"} {
		body := buildRequest(ImageGenerateRequest{
			Model:         model,
			Prompt:        "a cat",
			Size:          "1:1",
			Resolution:    "1K",
			VendorModelID: model,
		})
		if _, ok := body["version"]; ok {
			t.Errorf("%s 的请求体不应包含 version: %v", model, body)
		}
	}
}

// resolution 的大小写按模型转换：gpt-image-2 小写，gpt-image-2.5-ext 大写。
func TestBuildRequestFormatsResolutionPerModel(t *testing.T) {
	cases := []struct {
		model      string
		resolution string
		want       string
	}{
		{"gpt-image-2", "2K", "2k"},
		{"gpt-image-2", "4k", "4k"},
		{"gpt-image-2-ext", "1K", "1k"},
		{testAPIMartModel, "2k", "2K"},
		{"gpt-image-2.5-sunburst-ext", "4K", "4K"},
		{"gpt-image-2.6-flare-ext", "2K", "2k"},
		{"gpt-image-2.6-sunburst-ext", "4k", "4k"},
		{"seedream-5-5", "1.5k", "1.5K"},
	}

	for _, tc := range cases {
		t.Run(tc.model+"/"+tc.resolution, func(t *testing.T) {
			body := buildRequest(ImageGenerateRequest{
				Model:         tc.model,
				Prompt:        "a cat",
				Size:          "1:1",
				Resolution:    tc.resolution,
				VendorModelID: "vendor-model",
			})
			if body["resolution"] != tc.want {
				t.Errorf("resolution = %v, want %s", body["resolution"], tc.want)
			}
		})
	}
}

// 模型规格是独立登记的：临时给测试模型换一份更小的 size/参考图集合，
// 不应影响其他模型的校验结果。
func TestAPIMartSpecsArePerModel(t *testing.T) {
	const custom = "test-apimart-custom-model"
	apimartImageModels[custom] = apimartImageModelSpec{
		sizeRule:           SizeRule{AllowRatio: true, Ratios: []string{"1:1"}},
		resolutions:        []string{"1K"},
		maxReferenceImages: 1,
	}
	defer delete(apimartImageModels, custom)

	a := &apimart{}

	err := a.ValidateImageRequest(ImageGenerateRequest{Model: custom, Prompt: "cat", Size: "1:1", Resolution: "1K"})
	if err != nil {
		t.Fatalf("自定义模型合法请求被拒绝: %v", err)
	}

	// 自定义模型不开放 16:9；同一请求对 gpt-image-2.5 模型仍然合法。
	if err := a.ValidateImageRequest(ImageGenerateRequest{Model: custom, Prompt: "cat", Size: "16:9", Resolution: "1K"}); err == nil {
		t.Error("自定义模型的 size 限制未生效")
	}
	if err := a.ValidateImageRequest(ImageGenerateRequest{Model: testAPIMartModel, Prompt: "cat", Size: "16:9", Resolution: "1K"}); err != nil {
		t.Errorf("另一个模型不应受自定义规格影响: %v", err)
	}

	// 参考图上限同样按模型生效。
	two := []string{"file_a", "file_b"}
	if err := a.ValidateImageRequest(ImageGenerateRequest{Model: custom, Prompt: "cat", Size: "1:1", Resolution: "1K", ImageIDs: two}); err == nil {
		t.Error("自定义模型的参考图上限未生效")
	}
}

// Seedream 5.5 的请求体只包含平台开放的字段：固定 watermark=false，
// 不发送 background / output_format / quality / complexity 等未开放字段。
func TestBuildRequestMapsSeedream55Fields(t *testing.T) {
	body := buildRequest(ImageGenerateRequest{
		Model:         "seedream-5-5",
		Prompt:        "a cat",
		Size:          "16:9",
		Resolution:    "2K",
		ImageURLs:     []string{"https://a.png"},
		VendorModelID: "seedream-5-5",
	})

	wants := map[string]interface{}{
		"model":      "seedream-5-5",
		"prompt":     "a cat",
		"size":       "16:9",
		"resolution": "2K",
		"n":          1,
		"watermark":  false,
	}
	for k, want := range wants {
		if got := body[k]; got != want {
			t.Errorf("%s = %v, want %v（请求体=%v）", k, got, want, body)
		}
	}

	urls, ok := body["image_urls"].([]string)
	if !ok || len(urls) != 1 {
		t.Errorf("image_urls = %#v, want 一个 URL", body["image_urls"])
	}
	if len(body) != len(wants)+1 {
		t.Errorf("请求体字段数 = %d, want %d（存在未预期字段）: %v", len(body), len(wants)+1, body)
	}
	for _, key := range []string{
		"background", "output_format", "quality", "complexity",
		"optimize_prompt_options", "layer_decomposition", "version",
	} {
		if _, ok := body[key]; ok {
			t.Errorf("模型未开放字段 %s 不应写入请求体: %v", key, body)
		}
	}
}

func TestAPIMartFailureReasonMapping(t *testing.T) {
	cases := []struct {
		reason string
		want   string
	}{
		{"content_policy_violation", FailureReasonContentPolicy},
		{"invalid_parameter", FailureReasonInvalidParameter},
		{"unsupported_option", FailureReasonInvalidParameter},
		{"invalid_media", FailureReasonInvalidMedia},
		{"media_unreachable", FailureReasonMediaUnreachable},
		{"generation_timeout", FailureReasonGenerationTimeout},
		{"service_unavailable", FailureReasonServiceUnavailable},
		{"delivery_failed", FailureReasonUpstreamFailed},
		{"internal_error", FailureReasonUpstreamFailed},
		{"unknown_error", FailureReasonUpstreamFailed},
		{" CONTENT_POLICY_VIOLATION ", FailureReasonContentPolicy},
		{"unexpected_reason", ""},
		{"", ""},
	}

	for _, tc := range cases {
		if got := apimartFailureReason(tc.reason); got != tc.want {
			t.Errorf("apimartFailureReason(%q) = %q, want %q", tc.reason, got, tc.want)
		}
	}
}

// 失败任务的原因要同时提供「归一化原因」和「上游原文」：
// 前者用于生成用户文案，后者落库供排查。
func TestTaskQueryCarriesFailureReasonAndRawError(t *testing.T) {
	a, cleanup := newTestAPIMart(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"data":{"id":"task_s","status":"failed","error":{"message":"blocked by policy","reason":"content_policy_violation"}}}`)
	})
	defer cleanup()

	resp := a.TaskQuery(context.Background(), `{"taskId":"task_s"}`)

	if resp.Status != "failed" {
		t.Fatalf("status = %q, want failed", resp.Status)
	}
	if resp.FailureReason != FailureReasonContentPolicy {
		t.Errorf("FailureReason = %q, want %q", resp.FailureReason, FailureReasonContentPolicy)
	}
	if resp.Data["error"] != "blocked by policy" {
		t.Errorf("Data.error = %v, want blocked by policy", resp.Data["error"])
	}
	if resp.Data["reason"] != "content_policy_violation" {
		t.Errorf("Data.reason = %v, want content_policy_violation", resp.Data["reason"])
	}
}
