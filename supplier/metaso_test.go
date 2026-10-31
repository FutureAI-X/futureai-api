package supplier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
)

// ── 视频请求校验 ──

func validVideoRequest() VideoGenerateRequest {
	return VideoGenerateRequest{
		Model: "MiniMax-H3",
		Content: []VideoContentItem{
			{Type: VideoContentTypeText, Text: "a cat walking"},
			{
				Type:     VideoContentTypeImage,
				ImageURL: &VideoContentURL{URL: "https://cdn.example.com/ref.png"},
				Role:     "reference_image",
			},
		},
		Resolution:    "2K",
		Duration:      5,
		Ratio:         "adaptive",
		VendorModelID: "MiniMax-H3",
	}
}

func TestVideoGenerateRequestValidate(t *testing.T) {
	valid := validVideoRequest()
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法请求不应报错: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*VideoGenerateRequest)
		want   string
	}{
		{"缺少 model", func(r *VideoGenerateRequest) { r.Model = "  " }, "model"},
		{"缺少 content", func(r *VideoGenerateRequest) { r.Content = nil }, "content"},
		{"缺少 resolution", func(r *VideoGenerateRequest) { r.Resolution = "" }, "resolution"},
		{"duration 为 0", func(r *VideoGenerateRequest) { r.Duration = 0 }, "duration"},
		{"duration 为负", func(r *VideoGenerateRequest) { r.Duration = -1 }, "duration"},
		{"缺少 ratio", func(r *VideoGenerateRequest) { r.Ratio = "" }, "ratio"},
		{"没有非空 text", func(r *VideoGenerateRequest) {
			r.Content = []VideoContentItem{
				{Type: VideoContentTypeText, Text: "   "},
				{Type: VideoContentTypeImage, ImageURL: &VideoContentURL{URL: "https://x/a.png"}},
			}
		}, "text"},
		{"未知 type", func(r *VideoGenerateRequest) {
			r.Content = append(r.Content, VideoContentItem{Type: "file", Text: "x"})
		}, "不支持"},
		{"image_url 缺少 url", func(r *VideoGenerateRequest) {
			r.Content[1].ImageURL = nil
		}, "image_url.url"},
		{"image_url 为空", func(r *VideoGenerateRequest) {
			r.Content[1].ImageURL = &VideoContentURL{URL: "  "}
		}, "image_url.url"},
		{"非 http 地址", func(r *VideoGenerateRequest) {
			r.Content[1].ImageURL = &VideoContentURL{URL: "file:///etc/passwd"}
		}, "http(s)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validVideoRequest()
			tc.mutate(&req)
			err := req.Validate()
			if err == nil {
				t.Fatalf("应当返回错误，实际通过")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息 = %q，应包含 %q", err.Error(), tc.want)
			}
		})
	}
}

// 四类内容项都应通过校验，避免只支持了示例里的 text + image。
func TestVideoGenerateRequestValidateAllContentTypes(t *testing.T) {
	req := VideoGenerateRequest{
		Model: "MiniMax-H3",
		Content: []VideoContentItem{
			{Type: VideoContentTypeText, Text: "prompt"},
			{Type: VideoContentTypeImage, ImageURL: &VideoContentURL{URL: "https://x/a.png"}, Role: "reference_image"},
			{Type: VideoContentTypeVideo, VideoURL: &VideoContentURL{URL: "https://x/a.mp4"}, Role: "reference_video"},
			{Type: VideoContentTypeAudio, AudioURL: &VideoContentURL{URL: "https://x/a.mp3"}, Role: "reference_audio"},
		},
		Resolution: "2K",
		Duration:   5,
		Ratio:      "adaptive",
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("四类内容项应全部合法: %v", err)
	}
}

// URL 的 scheme 大小写不应影响判断。
func TestVideoGenerateRequestValidateAcceptsUppercaseScheme(t *testing.T) {
	req := validVideoRequest()
	req.Content[1].ImageURL = &VideoContentURL{URL: "HTTPS://cdn.example.com/ref.png"}

	if err := req.Validate(); err != nil {
		t.Errorf("大写 scheme 应被接受: %v", err)
	}
}

// 上游只接受约定字段：model 用供应商模型 ID，fast_mode 固定 false。
func TestBuildVideoRequestUsesVendorModelIDAndFixedFastMode(t *testing.T) {
	req := validVideoRequest()
	req.Model = "caller-facing-name"
	req.VendorModelID = "MiniMax-H3"

	data, err := json.Marshal(buildVideoRequest(req))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if body["model"] != "MiniMax-H3" {
		t.Errorf("model = %v, want MiniMax-H3（应使用 vendor_model_id）", body["model"])
	}
	if body["fast_mode"] != false {
		t.Errorf("fast_mode = %v, want false", body["fast_mode"])
	}
	if body["resolution"] != "2K" || body["duration"] != float64(5) || body["ratio"] != "adaptive" {
		t.Errorf("标准字段未原样写入: %v", body)
	}

	// 只允许约定的顶层字段，多写任何字段都可能被上游当作参数。
	allowed := map[string]bool{
		"model": true, "content": true, "resolution": true,
		"duration": true, "ratio": true, "fast_mode": true,
	}
	for key := range body {
		if !allowed[key] {
			t.Errorf("请求体出现非约定字段 %q", key)
		}
	}

	content, ok := body["content"].([]interface{})
	if !ok || len(content) != 2 {
		t.Fatalf("content = %v, want 2 项", body["content"])
	}
	imageItem, ok := content[1].(map[string]interface{})
	if !ok {
		t.Fatalf("第二项不是对象: %v", content[1])
	}
	if imageItem["role"] != "reference_image" {
		t.Errorf("role = %v, want reference_image", imageItem["role"])
	}
	imageURL, ok := imageItem["image_url"].(map[string]interface{})
	if !ok || imageURL["url"] != "https://cdn.example.com/ref.png" {
		t.Errorf("image_url 未原样写入: %v", imageItem["image_url"])
	}
}

// ── 提交接口 ──

func newTestMetaso(t *testing.T, handler http.HandlerFunc) (*metaso, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	// 注入 httptest 的 client 绕过 SSRF 防护：
	// SafeDialContext 会拒绝测试服务器所在的回环地址。
	common.SetOutboundClientForTest(srv.Client())
	return &metaso{cfg: Config{BaseURL: srv.URL, APIKey: "test-key"}}, func() {
		common.SetOutboundClientForTest(nil)
		srv.Close()
	}
}

func TestMetasoVideoGenerateSendsRequestAndReturnsTaskID(t *testing.T) {
	var received map[string]interface{}
	var auth string
	var path string

	m, cleanup := newTestMetaso(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("解析上游收到的请求体失败: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"task_id":"424010985738629"}`)
	})
	defer cleanup()

	resp := m.VideoGenerate(context.Background(), validVideoRequest())

	if resp.Code != "success" {
		t.Fatalf("resp.Code = %q, want success (kind=%q)", resp.Code, resp.FailureKind)
	}
	if resp.Data["task_id"] != "424010985738629" {
		t.Errorf("task_id = %v, want 424010985738629", resp.Data["task_id"])
	}
	if path != "/video_generation" {
		t.Errorf("请求路径 = %q, want /video_generation", path)
	}
	if auth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", auth)
	}
	if received["model"] != "MiniMax-H3" {
		t.Errorf("上游收到的 model = %v, want MiniMax-H3", received["model"])
	}
	if received["fast_mode"] != false {
		t.Errorf("上游收到的 fast_mode = %v, want false", received["fast_mode"])
	}
}

// 失败性质分类：直接决定控制器是否退款。
// 4xx / 明确错误体 = 上游没受理 → rejected；
// 5xx / 超时 / 响应不可读 = 可能已受理 → unknown。
func TestMetasoVideoGenerateFailureClassification(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		body       string
		want       string
	}{
		{
			name:       "400 参数错误",
			statusCode: http.StatusBadRequest,
			body:       `{"type":"error","error":{"type":"bad_request_error","message":"invalid params (2013)","http_code":"400"}}`,
			want:       FailureRejected,
		},
		{
			name:       "401 鉴权失败",
			statusCode: http.StatusUnauthorized,
			body:       `{"type":"error","error":{"type":"authorized_error","message":"login fail (1004)","http_code":"401"}}`,
			want:       FailureRejected,
		},
		{
			name:       "402 余额不足",
			statusCode: http.StatusPaymentRequired,
			body:       `{"type":"error","error":{"type":"insufficient_balance_error","message":"insufficient balance (1008)","http_code":"402"}}`,
			want:       FailureRejected,
		},
		{
			name:       "422 敏感内容",
			statusCode: http.StatusUnprocessableEntity,
			body:       `{"type":"error","error":{"type":"unprocessable_entity_error","message":"sensitive content (1026)","http_code":"422"}}`,
			want:       FailureRejected,
		},
		{
			name:       "429 限流",
			statusCode: http.StatusTooManyRequests,
			body:       `{"type":"error","error":{"type":"rate_limit_error","message":"rate limit (1002)","http_code":"429"}}`,
			want:       FailureRejected,
		},
		{
			name:       "500 上游错误",
			statusCode: http.StatusInternalServerError,
			body:       `{"type":"error","error":{"type":"server_error","message":"internal error (1000)","http_code":"500"}}`,
			want:       FailureUnknown,
		},
		{
			name:       "2xx 但包装了 401 错误体",
			statusCode: http.StatusOK,
			body:       `{"type":"error","error":{"type":"authorized_error","message":"login fail","http_code":"401"}}`,
			want:       FailureRejected,
		},
		{
			name:       "2xx 但是 500 错误体",
			statusCode: http.StatusOK,
			body:       `{"type":"error","error":{"type":"server_error","message":"internal","http_code":"500"}}`,
			want:       FailureUnknown,
		},
		{
			name:       "2xx 缺少 task_id",
			statusCode: http.StatusOK,
			body:       `{"task_id":""}`,
			want:       FailureUnknown,
		},
		{
			name:       "2xx 响应不可解析",
			statusCode: http.StatusOK,
			body:       `not-json`,
			want:       FailureUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, cleanup := newTestMetaso(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				fmt.Fprint(w, tc.body)
			})
			defer cleanup()

			resp := m.VideoGenerate(context.Background(), validVideoRequest())

			if resp.Code != "fail" {
				t.Fatalf("resp.Code = %q, want fail", resp.Code)
			}
			if resp.FailureKind != tc.want {
				t.Errorf("FailureKind = %q, want %q", resp.FailureKind, tc.want)
			}
		})
	}
}

// 超时：请求很可能已经送达并被受理，绝不能当成「上游没接单」去退款。
func TestMetasoVideoGenerateTimeoutIsUnknown(t *testing.T) {
	m, cleanup := newTestMetaso(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	resp := m.VideoGenerate(ctx, validVideoRequest())

	if resp.Code != "fail" {
		t.Fatalf("resp.Code = %q, want fail", resp.Code)
	}
	if resp.FailureKind != FailureUnknown {
		t.Errorf("超时必须归类为 %q，实际 %q", FailureUnknown, resp.FailureKind)
	}
}

// 请求根本没发出去（地址非法）：上游不可能受理，可以安全退款。
func TestMetasoVideoGenerateInvalidURLIsRejected(t *testing.T) {
	m := &metaso{cfg: Config{BaseURL: "://bad", APIKey: "k"}}

	resp := m.VideoGenerate(context.Background(), validVideoRequest())

	if resp.Code != "fail" || resp.FailureKind != FailureRejected {
		t.Errorf("非法地址应归类为 %q，实际 code=%q kind=%q",
			FailureRejected, resp.Code, resp.FailureKind)
	}
}

// ── 查询接口 ──

func newTestMetasoWithBody(t *testing.T, statusCode int, body string, onRequest func(*http.Request)) (*metaso, func()) {
	t.Helper()
	return newTestMetaso(t, func(w http.ResponseWriter, r *http.Request) {
		if onRequest != nil {
			onRequest(r)
		}
		w.WriteHeader(statusCode)
		fmt.Fprint(w, body)
	})
}

func TestMetasoTaskQuerySucceeded(t *testing.T) {
	body := `{
		"task": {
			"id": "2116517652677840896",
			"model": "MiniMax-H3",
			"title": "测试视频",
			"status": "succeeded",
			"created_at": 1793452106,
			"updated_at": 1793452123,
			"content": {"url": "https://files.metaso.cn/video.mp4?signature=x"},
			"resolution": "480P",
			"duration": 5,
			"usage": {"total_seconds": 5, "input_seconds": 0, "output_seconds": 5, "input_image_count": 0},
			"ratio": "16:9"
		}
	}`

	var path string
	m, cleanup := newTestMetasoWithBody(t, http.StatusOK, body, func(r *http.Request) {
		path = r.URL.Path
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
	})
	defer cleanup()

	resp := m.TaskQuery(context.Background(), `{"task_id":"2116517652677840896"}`)

	if path != "/query/video_generation/2116517652677840896" {
		t.Errorf("查询路径 = %q", path)
	}
	if resp.Status != "completed" {
		t.Fatalf("Status = %q, want completed", resp.Status)
	}
	if resp.Kind != ResultKindVideo {
		t.Errorf("Kind = %q, want %q", resp.Kind, ResultKindVideo)
	}
	if resp.Data["url"] != "https://files.metaso.cn/video.mp4?signature=x" {
		t.Errorf("url = %v", resp.Data["url"])
	}
	if resp.Data["resolution"] != "480P" || resp.Data["duration"] != 5 || resp.Data["ratio"] != "16:9" {
		t.Errorf("结果字段未规范化: %v", resp.Data)
	}
	if resp.Data["vendor_task_id"] != "2116517652677840896" {
		t.Errorf("vendor_task_id = %v", resp.Data["vendor_task_id"])
	}
	if _, ok := resp.Data["usage"]; !ok {
		t.Error("usage 应被保留")
	}
}

// succeeded 但没有产物地址：仍按 completed 返回空 url，
// 由控制器的 hasDeliverableResult 判定为「没有产出」并退款。
func TestMetasoTaskQuerySucceededWithoutURL(t *testing.T) {
	body := `{"task":{"id":"1","status":"succeeded","content":{"url":""}}}`
	m, cleanup := newTestMetasoWithBody(t, http.StatusOK, body, nil)
	defer cleanup()

	resp := m.TaskQuery(context.Background(), `{"task_id":"1"}`)

	if resp.Status != "completed" {
		t.Fatalf("Status = %q, want completed", resp.Status)
	}
	if resp.Data["url"] != "" {
		t.Errorf("url = %v, want 空串", resp.Data["url"])
	}
}

func TestMetasoTaskQueryStatusMapping(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   string
	}{
		{"排队中", "queued", "submitted"},
		{"运行中", "running", "submitted"},
		{"成功", "succeeded", "completed"},
		{"失败", "failed", "failed"},
		{"已取消", "cancelled", "cancelled"},
		{"未识别状态", "paused", "call_fail"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"task":{"id":"1","status":%q}}`, tc.status)
			m, cleanup := newTestMetasoWithBody(t, http.StatusOK, body, nil)
			defer cleanup()

			resp := m.TaskQuery(context.Background(), `{"task_id":"1"}`)

			if resp.Status != tc.want {
				t.Errorf("Status = %q, want %q", resp.Status, tc.want)
			}
		})
	}
}

// 失败任务的错误信息要能写进 query_response；code 可能是数字。
func TestMetasoTaskQueryFailedCarriesError(t *testing.T) {
	body := `{"task":{"id":"1","status":"failed","error":{"code":1026,"message":"sensitive content"}}}`
	m, cleanup := newTestMetasoWithBody(t, http.StatusOK, body, nil)
	defer cleanup()

	resp := m.TaskQuery(context.Background(), `{"task_id":"1"}`)

	if resp.Status != "failed" {
		t.Fatalf("Status = %q, want failed", resp.Status)
	}
	errData, ok := resp.Data["error"].(map[string]string)
	if !ok {
		t.Fatalf("error = %v, want map[string]string", resp.Data["error"])
	}
	if errData["code"] != "1026" {
		t.Errorf("error.code = %q, want 1026", errData["code"])
	}
	if errData["message"] != "sensitive content" {
		t.Errorf("error.message = %q", errData["message"])
	}
}

func TestMetasoTaskQueryHTTPErrorIsCallFail(t *testing.T) {
	m, cleanup := newTestMetasoWithBody(t, http.StatusInternalServerError, `{"error":"boom"}`, nil)
	defer cleanup()

	resp := m.TaskQuery(context.Background(), `{"task_id":"1"}`)

	if resp.Status != "call_fail" {
		t.Errorf("Status = %q, want call_fail", resp.Status)
	}
}

// task_id 会被拼进查询 URL，非法形态必须在发请求之前就被拒绝。
func TestMetasoTaskQueryRejectsMalformedTaskID(t *testing.T) {
	cases := []string{
		`{"task_id":"../etc/passwd"}`,
		`{"task_id":"a/b"}`,
		`{"taskId":"missing-key"}`,
		`not-json`,
		`{"task_id":""}`,
	}

	for _, vendorResponse := range cases {
		t.Run(vendorResponse, func(t *testing.T) {
			called := false
			m, cleanup := newTestMetaso(t, func(w http.ResponseWriter, r *http.Request) {
				called = true
			})
			defer cleanup()

			resp := m.TaskQuery(context.Background(), vendorResponse)

			if resp.Status != "call_fail" {
				t.Errorf("Status = %q, want call_fail", resp.Status)
			}
			if called {
				t.Error("非法 task_id 不应发出查询请求")
			}
		})
	}
}

// ── 工厂函数 ──

func TestVideoSupplierFactory(t *testing.T) {
	if NewVideoSupplier("METASO", Config{}) == nil {
		t.Error("METASO 应被识别为视频供应商")
	}
	if NewVideoSupplier(" metaso ", Config{}) == nil {
		t.Error("供应商名称应忽略大小写与首尾空白")
	}
	if NewVideoSupplier("apimart", Config{}) != nil {
		t.Error("APIMart 当前不是视频供应商")
	}
	if NewVideoSupplier("unknown", Config{}) != nil {
		t.Error("未知供应商应返回 nil")
	}
}

func TestTaskQuerierFactory(t *testing.T) {
	if NewTaskQuerier("APIMart", Config{}) == nil {
		t.Error("APIMart 应可查询任务")
	}
	if NewTaskQuerier("METASO", Config{}) == nil {
		t.Error("METASO 应可查询任务")
	}
	if NewTaskQuerier("unknown", Config{}) != nil {
		t.Error("未知供应商应返回 nil")
	}
}

// METASO 目前只承载视频：图像工厂不应把它当成图像供应商。
func TestImageSupplierFactoryRejectsMetaso(t *testing.T) {
	if NewSupplier("METASO", Config{}) != nil {
		t.Error("METASO 不应被识别为图像供应商")
	}
}
