package supplier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"time"
	"testing"

	"github.com/FutureAI-X/futureai-api/common"
)

// ── ApplyDefaults：标准字段默认值 ──

func TestApplyDefaultsFillsOmittedFields(t *testing.T) {
	var req ImageGenerateRequest
	req.ApplyDefaults()

	if req.Resolution != "1k" {
		t.Errorf("resolution 默认值 = %q, want 1k", req.Resolution)
	}
	if req.Size != "16:9" {
		t.Errorf("size 默认值 = %q, want 16:9", req.Size)
	}
}

// 调用方显式传的值必须原样保留，默认值只补空缺。
func TestApplyDefaultsKeepsExplicitValues(t *testing.T) {
	req := ImageGenerateRequest{Resolution: "4k", Size: "1:1"}
	req.ApplyDefaults()

	if req.Resolution != "4k" || req.Size != "1:1" {
		t.Errorf("显式取值被覆盖: resolution=%q, size=%q", req.Resolution, req.Size)
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
// 实际却按 1k 出图。这里锁住「补齐后计费条件能命中默认值」这一行为。
func TestApplyDefaultsFeedsCreditParams(t *testing.T) {
	var req ImageGenerateRequest
	req.ApplyDefaults()

	params := req.CreditParams()

	// 未补齐时 params["resolution"] 是空串，永远匹配不上 resolution=1k 的条件
	if params["resolution"] != "1k" {
		t.Errorf("计费字段 resolution = %q, want 1k（默认值须在计费前补齐）", params["resolution"])
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

// 数组字段与任意名字都不参与计费：配上去永远匹配不上。
func TestIsCreditParamNameRejectsNonStandardFields(t *testing.T) {
	for _, name := range []string{"image_urls", "quality", "n", "seed", "version", "", "Model"} {
		if IsCreditParamName(name) {
			t.Errorf("%q 不该被当作可计费的标准字段", name)
		}
	}
}

// ── applyImageVersion：按调用方模型名补全 APIMart 的 version 字段 ──

func TestApplyImageVersionSetsVersionByRequestedModel(t *testing.T) {
	cases := []struct {
		name           string
		requestedModel string
		wantVersion    interface{}
		wantSet        bool
	}{
		{"flare 变体", "gpt-image-2.5-flare-ext", "flare", true},
		{"sunburst 变体", "gpt-image-2.5-sunburst-ext", "sunburst", true},
		{"未登记的调用方模型不写 version", "gpt-image-2.5-unknown-ext", nil, false},
		{"调用方模型名为空不写 version", "", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]interface{}{
				"model":  apimartVersionedImageModel,
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

// 其他图像模型不带 version 参数，必须原样透传，不能被塞进一个多余的字段。
func TestApplyImageVersionLeavesOtherModelsUntouched(t *testing.T) {
	body := map[string]interface{}{
		"model":   "gpt-image-1",
		"prompt":  "a cat",
		"version": "caller-supplied",
	}

	applyImageVersion(body, "gpt-image-2.5-sunburst-ext")

	if got := body["version"]; got != "caller-supplied" {
		t.Errorf("非目标模型的 version 被改写为 %v, want 原样透传", got)
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
		VendorModelID: apimartVersionedImageModel,
	})

	wants := map[string]interface{}{
		"model":      apimartVersionedImageModel,
		"prompt":     "a cat",
		"size":       "16:9",
		"resolution": "2k",
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

// image_urls 为空时不写入；size / resolution 一律写入——默认值已在计费前补齐，
// 发空串反而会让上游当作未指定，绕开标准默认值。
func TestBuildRequestAlwaysWritesSizeAndResolution(t *testing.T) {
	var req ImageGenerateRequest
	req.ApplyDefaults()
	req.Model = "gpt-image-1"
	req.Prompt = "a cat"
	req.VendorModelID = "gpt-image-1"

	body := buildRequest(req)

	if body["size"] != DefaultSize {
		t.Errorf("size = %v, want %s", body["size"], DefaultSize)
	}
	if body["resolution"] != DefaultResolution {
		t.Errorf("resolution = %v, want %s", body["resolution"], DefaultResolution)
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
	req.VendorModelID = apimartVersionedImageModel
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
	if got := received["model"]; got != apimartVersionedImageModel {
		t.Errorf("上游收到的 model = %v, want %s", got, apimartVersionedImageModel)
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
	req := ImageGenerateRequest{Model: "m", Prompt: "a cat", VendorModelID: "vendor-m"}
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
