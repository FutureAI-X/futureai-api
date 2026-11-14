package supplier

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
)

func TestMinimaxCapabilities(t *testing.T) {
	m := newMinimax(Config{})
	caps := m.ImageCapabilities()

	if !caps.BackgroundSubmit {
		t.Error("MiniMax H3 必须是后台提交")
	}
	if !caps.SupportsReferenceImages {
		t.Error("MiniMax H3 应支持参考图（编辑接口）")
	}
	if !caps.RequiresPixelSize {
		t.Error("MiniMax H3 必须要求像素尺寸")
	}
	if caps.SubmitTimeout != minimaxSubmitTimeout {
		t.Errorf("SubmitTimeout = %s, want %s", caps.SubmitTimeout, minimaxSubmitTimeout)
	}
}

func TestMinimaxValidateImageRequest(t *testing.T) {
	m := newMinimax(Config{})

	refs := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("https://example.com/ref-%d.png", i)
		}
		return out
	}

	tests := []struct {
		name    string
		req     ImageGenerateRequest
		wantErr bool
	}{
		{"合法像素尺寸", ImageGenerateRequest{Prompt: "cat", Size: "2720x1536"}, false},
		{"大写 x 与空白", ImageGenerateRequest{Prompt: "cat", Size: " 2720 X 1536 "}, false},
		{"比例值", ImageGenerateRequest{Prompt: "cat", Size: "16:9"}, true},
		{"auto", ImageGenerateRequest{Prompt: "cat", Size: "auto"}, true},
		{"空 size", ImageGenerateRequest{Prompt: "cat", Size: ""}, true},
		{"总像素过小", ImageGenerateRequest{Prompt: "cat", Size: "800x800"}, true},
		{"总像素过大", ImageGenerateRequest{Prompt: "cat", Size: "5000x5000"}, true},
		{"宽高比超过 3:1", ImageGenerateRequest{Prompt: "cat", Size: "4000x1000"}, true},
		{"空 prompt", ImageGenerateRequest{Prompt: "   ", Size: "1024x1024"}, true},
		{"prompt 超长", ImageGenerateRequest{Prompt: strings.Repeat("字", minimaxMaxPromptRunes+1), Size: "1024x1024"}, true},
		{"参考图 17 张", ImageGenerateRequest{Prompt: "cat", Size: "1024x1024", ImageURLs: refs(17)}, true},
		{"参考图非 http", ImageGenerateRequest{Prompt: "cat", Size: "1024x1024", ImageURLs: []string{"file:///a.png"}}, true},
		{"非法 output_format", ImageGenerateRequest{Prompt: "cat", Size: "1024x1024", OutputFormat: "svg"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.ValidateImageRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateImageRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildMinimaxRequestGeneration(t *testing.T) {
	body, err := buildMinimaxRequest(ImageGenerateRequest{
		VendorModelID: "MiniMax-H3-Image",
		Prompt:        "a cat",
		Size:          "2720X1536",
		Resolution:    "2K",
		OutputFormat:  "JPG",
	})
	if err != nil {
		t.Fatalf("buildMinimaxRequest() error = %v", err)
	}

	want := map[string]interface{}{
		"model":                    "MiniMax-H3-Image",
		"prompt":                   "a cat",
		"n":                        1,
		"size":                     "2720x1536",
		"response_format":          "url",
		"stream":                   false,
		"aigc_watermark":           false,
		"disable_prompt_enhanced":  false,
		"output_format":            "jpeg",
	}
	for key, value := range want {
		if body[key] != value {
			t.Errorf("body[%q] = %v, want %v", key, body[key], value)
		}
	}
	for _, key := range []string{"aspect_ratio", "area", "resolution", "images"} {
		if _, exists := body[key]; exists {
			t.Errorf("文生图请求体不应包含 %q", key)
		}
	}
}

func TestBuildMinimaxRequestEdit(t *testing.T) {
	body, err := buildMinimaxRequest(ImageGenerateRequest{
		VendorModelID: "MiniMax-H3-Image",
		Prompt:        "把猫换成狗",
		Size:          "1024x1024",
		ImageURLs:     []string{"https://example.com/a.png", "https://example.com/b.png"},
	})
	if err != nil {
		t.Fatalf("buildMinimaxRequest() error = %v", err)
	}
	if _, exists := body["disable_prompt_enhanced"]; exists {
		t.Error("编辑请求不应携带 disable_prompt_enhanced")
	}
	images, ok := body["images"].([]map[string]string)
	if !ok || len(images) != 2 {
		t.Fatalf("images = %#v, want 2 项", body["images"])
	}
	if images[0]["image_url"] != "https://example.com/a.png" {
		t.Errorf("images[0] = %#v", images[0])
	}
}

func TestMinimaxEstimateImageUsage(t *testing.T) {
	m := newMinimax(Config{})

	gen := m.EstimateImageUsage(ImageGenerateRequest{
		Prompt: "a cat",
		Size:   "2720x1536",
	})
	if gen.ImageOutputTokens != 4080 {
		t.Errorf("ImageOutputTokens = %d, want 4080", gen.ImageOutputTokens)
	}
	if gen.ImageInputTokens != 0 {
		t.Errorf("ImageInputTokens = %d, want 0", gen.ImageInputTokens)
	}
	if gen.TextInputTokens < minimaxEnhancedTextTokens {
		t.Errorf("TextInputTokens = %d, want >= %d", gen.TextInputTokens, minimaxEnhancedTextTokens)
	}

	edit := m.EstimateImageUsage(ImageGenerateRequest{
		Prompt:    "把猫换成狗",
		Size:      "2720x1536",
		ImageIDs:  []string{"file_a", "file_b"},
	})
	if edit.ImageInputTokens != 2*minimaxRefImageTokens {
		t.Errorf("ImageInputTokens = %d, want %d", edit.ImageInputTokens, 2*minimaxRefImageTokens)
	}
	if edit.TextInputTokens < minimaxEditTextTokens {
		t.Errorf("TextInputTokens = %d, want >= %d", edit.TextInputTokens, minimaxEditTextTokens)
	}
}

func TestMinimaxImageGenerateSuccess(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("请求体不是 JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"created": 1792308678,
			"data": [{
				"url": "https://example.com/output-0.jpg",
				"size": "2720x1536",
				"revised_prompt": "一只橘猫"
			}],
			"output_format": "jpeg",
			"usage": {
				"generated_images": 1,
				"total_tokens": 7254,
				"input_tokens": 3174,
				"output_tokens": 4080,
				"input_tokens_details": {"text_tokens": 3174, "image_tokens": 0},
				"output_tokens_details": {"image_tokens": 4080}
			},
			"request_id": "req-1"
		}`)
	}))
	defer srv.Close()

	common.SetLongOutboundClientForTest(srv.Client())
	defer common.SetLongOutboundClientForTest(nil)

	m := newMinimax(Config{BaseURL: srv.URL, APIKey: "test-key"})
	resp := m.ImageGenerate(context.Background(), ImageGenerateRequest{
		VendorModelID: "MiniMax-H3-Image",
		Prompt:        "a cat",
		Size:          "2720x1536",
	})

	if resp.Code != "success" {
		t.Fatalf("Code = %q, want success (failure kind %q)", resp.Code, resp.FailureKind)
	}
	if gotPath != "/v1/images/generations" {
		t.Errorf("path = %q, want /v1/images/generations", gotPath)
	}
	if got := resp.Data["url"]; got != "https://example.com/output-0.jpg" {
		t.Errorf("url = %v", got)
	}
	if resp.Usage == nil {
		t.Fatal("成功响应必须带 usage")
	}
	if resp.Usage.TextInputTokens != 3174 || resp.Usage.ImageOutputTokens != 4080 {
		t.Errorf("usage = %#v", resp.Usage)
	}
	if gotBody["disable_prompt_enhanced"] != false {
		t.Errorf("disable_prompt_enhanced = %v, want false", gotBody["disable_prompt_enhanced"])
	}
}

func TestMinimaxImageGenerateEditPath(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		io.WriteString(w, `{
			"created": 1792308695,
			"data": [{"url":"https://example.com/edit.jpg","size":"2720x1536"}],
			"usage": {
				"generated_images": 1,
				"total_tokens": 40225,
				"input_tokens": 23905,
				"output_tokens": 16320,
				"input_tokens_details": {"text_tokens": 12093, "image_tokens": 11812},
				"output_tokens_details": {"image_tokens": 16320}
			}
		}`)
	}))
	defer srv.Close()

	common.SetLongOutboundClientForTest(srv.Client())
	defer common.SetLongOutboundClientForTest(nil)

	m := newMinimax(Config{BaseURL: srv.URL, APIKey: "k"})
	resp := m.ImageGenerate(context.Background(), ImageGenerateRequest{
		VendorModelID: "MiniMax-H3-Image",
		Prompt:        "把猫换成狗",
		Size:          "2720x1536",
		ImageURLs:     []string{"https://example.com/a.png", "https://example.com/b.png"},
	})

	if resp.Code != "success" {
		t.Fatalf("Code = %q, want success", resp.Code)
	}
	if gotPath != "/v1/images/edits" {
		t.Errorf("path = %q, want /v1/images/edits", gotPath)
	}
	if _, exists := gotBody["disable_prompt_enhanced"]; exists {
		t.Error("编辑请求不应携带 disable_prompt_enhanced")
	}
	if resp.Usage.ImageInputTokens != 11812 {
		t.Errorf("ImageInputTokens = %d, want 11812", resp.Usage.ImageInputTokens)
	}
}

func TestMinimaxFailures(t *testing.T) {
	tests := []struct {
		name string
		status int
		body string
		wantKind string
	}{
		{"400", http.StatusBadRequest, `{"type":"error","error":{"type":"bad_request_error","message":"invalid params (2013)","http_code":"400"}}`, FailureRejected},
		{"422", http.StatusUnprocessableEntity, `{"type":"error","error":{"type":"unprocessable_entity_error","message":"failed safety check (1027)","http_code":"422"}}`, FailureRejected},
		{"500", http.StatusInternalServerError, `{"type":"error","error":{"type":"server_error","message":"internal error (1000)","http_code":"500"}}`, FailureRejected},
		{"200 无 data", http.StatusOK, `{"created":1,"data":[]}`, FailureRejected},
		{"200 单张失败", http.StatusOK, `{"data":[{"error":{"code":"1027","message":"safety"}}]}`, FailureRejected},
		{"200 url 非 http", http.StatusOK, `{"data":[{"url":"file:///etc/passwd"}]}`, FailureRejected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			common.SetLongOutboundClientForTest(srv.Client())
			defer common.SetLongOutboundClientForTest(nil)

			m := newMinimax(Config{BaseURL: srv.URL, APIKey: "k"})
			resp := m.ImageGenerate(context.Background(), ImageGenerateRequest{
				VendorModelID: "MiniMax-H3-Image",
				Prompt:        "a cat",
				Size:          "1024x1024",
			})
			if resp.Code != "fail" {
				t.Fatalf("Code = %q, want fail", resp.Code)
			}
			if resp.FailureKind != tt.wantKind {
				t.Errorf("FailureKind = %q, want %q", resp.FailureKind, tt.wantKind)
			}
		})
	}
}

func TestMinimaxTimeoutIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		io.WriteString(w, `{"data":[{"url":"https://example.com/a.png"}]}`)
	}))
	defer srv.Close()

	common.SetLongOutboundClientForTest(srv.Client())
	defer common.SetLongOutboundClientForTest(nil)

	m := newMinimax(Config{BaseURL: srv.URL, APIKey: "k"})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	resp := m.ImageGenerate(ctx, ImageGenerateRequest{
		VendorModelID: "MiniMax-H3-Image",
		Prompt:        "a cat",
		Size:          "1024x1024",
	})
	if resp.Code != "fail" || resp.FailureKind != FailureUnknown {
		t.Errorf("超时后应标记为结果不确定: code=%q kind=%q", resp.Code, resp.FailureKind)
	}
}

func TestMinimaxTaskQueryCompletedFromStoredResult(t *testing.T) {
	m := newMinimax(Config{})

	resp := m.TaskQuery(context.Background(), `{
		"url": "https://example.com/a.png",
		"size": "2720x1536",
		"usage": {
			"total_tokens": 7254,
			"input_tokens_details": {"text_tokens": 3174, "image_tokens": 0},
			"output_tokens_details": {"image_tokens": 4080}
		}
	}`)

	if resp.Status != "completed" {
		t.Fatalf("Status = %q, want completed", resp.Status)
	}
	if resp.Kind != ResultKindImage {
		t.Errorf("Kind = %q, want image", resp.Kind)
	}
	if resp.RequireResultFetch {
		t.Error("H3 不应要求结果资产必须可下载")
	}
	if !resp.SkipResultPersist {
		t.Error("H3 应跳过对象存储转存，直接返回上游 URL")
	}
	if resp.Usage == nil || resp.Usage.ImageOutputTokens != 4080 {
		t.Errorf("Usage = %#v", resp.Usage)
	}
	if got := resp.Data["url"]; got != "https://example.com/a.png" {
		t.Errorf("url = %v", got)
	}
}

func TestMinimaxTaskQueryInvalidStoredResultFails(t *testing.T) {
	m := newMinimax(Config{})

	for _, raw := range []string{"not-json", `{}`, `{"url":""}`, `{"url":"file:///x"}`} {
		resp := m.TaskQuery(context.Background(), raw)
		if resp.Status != "failed" {
			t.Errorf("vendorResponse=%q: Status = %q, want failed", raw, resp.Status)
		}
	}
}

func TestMinimaxRegisteredInFactories(t *testing.T) {
	if s := NewSupplier("MiniMax", Config{}); s == nil {
		t.Error("NewSupplier(\"minimax\") 不应返回 nil")
	}
	if q := NewTaskQuerier("minimax", Config{}); q == nil {
		t.Error("NewTaskQuerier(\"minimax\") 不应返回 nil")
	}
	if v := NewVideoSupplier("minimax", Config{}); v != nil {
		t.Error("MiniMax H3 图像供应商不是视频供应商")
	}
}
