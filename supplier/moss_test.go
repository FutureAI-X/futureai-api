package supplier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
)

func TestMossCapabilities(t *testing.T) {
	m := newMoss(Config{})
	caps := m.ImageCapabilities()

	if !caps.BackgroundSubmit {
		t.Error("MOSS 必须是后台提交")
	}
	if caps.SupportsReferenceImages {
		t.Error("MOSS 不应支持参考图")
	}
	if !caps.RequiresPixelSize {
		t.Error("MOSS 必须要求像素尺寸")
	}
	if caps.SubmitTimeout != mossSubmitTimeout {
		t.Errorf("SubmitTimeout = %s, want %s", caps.SubmitTimeout, mossSubmitTimeout)
	}
}

func TestMossValidateImageRequest(t *testing.T) {
	m := newMoss(Config{})

	tests := []struct {
		name    string
		req     ImageGenerateRequest
		wantErr bool
	}{
		{"合法尺寸", ImageGenerateRequest{Prompt: "cat", Size: "1024x1024"}, false},
		{"非正方形", ImageGenerateRequest{Prompt: "cat", Size: "1280x720"}, false},
		{"大小写 x", ImageGenerateRequest{Prompt: "cat", Size: "1024X768"}, false},
		{"两侧空白", ImageGenerateRequest{Prompt: "cat", Size: " 1024 x 768 "}, false},
		{"比例值", ImageGenerateRequest{Prompt: "cat", Size: "16:9"}, true},
		{"空 size", ImageGenerateRequest{Prompt: "cat", Size: ""}, true},
		{"小于下限", ImageGenerateRequest{Prompt: "cat", Size: "240x1024"}, true},
		{"大于上限", ImageGenerateRequest{Prompt: "cat", Size: "2064x1024"}, true},
		{"非 16 倍数", ImageGenerateRequest{Prompt: "cat", Size: "1025x1024"}, true},
		{"总像素超限", ImageGenerateRequest{Prompt: "cat", Size: "2048x2048"}, true},
		{"空 prompt", ImageGenerateRequest{Prompt: "   ", Size: "1024x1024"}, true},
		{"prompt 超长", ImageGenerateRequest{Prompt: strings.Repeat("字", mossMaxPromptRunes+1), Size: "1024x1024"}, true},
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

func TestBuildMossRequestUsesFixedParameters(t *testing.T) {
	req := ImageGenerateRequest{
		Model:         "moss-image-base-1.0",
		VendorModelID: "moss-image-base-1.0",
		Prompt:        "a cat",
		Size:          "1280X720",
		Resolution:    "2k",
		ImageURLs:     []string{"https://example.com/ref.png"},
	}

	body, err := buildMossRequest(req)
	if err != nil {
		t.Fatalf("buildMossRequest() error = %v", err)
	}

	want := map[string]interface{}{
		"model":           "moss-image-base-1.0",
		"prompt":          "a cat",
		"size":            "1280x720",
		"negative_prompt": mossNegativePrompt,
		"prompt_enhance":  true,
		"n":               1,
		"response_format": "url",
	}
	for key, value := range want {
		if body[key] != value {
			t.Errorf("body[%q] = %v, want %v", key, body[key], value)
		}
	}

	watermark, ok := body["output_watermark"].(map[string]interface{})
	if !ok || watermark["enabled"] != false {
		t.Errorf("output_watermark = %v, want enabled=false", body["output_watermark"])
	}

	for _, key := range []string{"image_urls", "image_ids", "resolution", "seed", "num_inference_steps", "guidance_scale"} {
		if _, exists := body[key]; exists {
			t.Errorf("MOSS 请求体不应包含 %q", key)
		}
	}
}

func TestMossImageGenerateSuccess(t *testing.T) {
	var gotBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("path = %q, want /v1/images/generations", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("请求体不是 JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"created": 1792380763,
			"data": [{"url": "https://example.com/generated-image.png"}],
			"usage": {"generated_images": 1, "output_width": 1280, "output_height": 720}
		}`)
	}))
	defer srv.Close()

	common.SetLongOutboundClientForTest(srv.Client())
	defer common.SetLongOutboundClientForTest(nil)

	m := newMoss(Config{BaseURL: srv.URL, APIKey: "test-key"})
	resp := m.ImageGenerate(context.Background(), ImageGenerateRequest{
		Model:         "moss-image-base-1.0",
		VendorModelID: "moss-image-base-1.0",
		Prompt:        "a cat",
		Size:          "1280x720",
	})

	if resp.Code != "success" {
		t.Fatalf("Code = %q, want success (failure kind %q)", resp.Code, resp.FailureKind)
	}
	if got := resp.Data["url"]; got != "https://example.com/generated-image.png" {
		t.Errorf("url = %v", got)
	}
	if _, ok := resp.Data["usage"]; !ok {
		t.Error("data 中应包含 usage")
	}

	if got := gotBody["size"]; got != "1280x720" {
		t.Errorf("上游 size = %v, want 1280x720", got)
	}
	if got := gotBody["prompt_enhance"]; got != true {
		t.Errorf("prompt_enhance = %v, want true", got)
	}
}

func TestMossImageGenerateFailuresAreRejected(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"400", http.StatusBadRequest, `{"error":{"code":"invalid_field_value","message":"bad size"}}`},
		{"403", http.StatusForbidden, `{"error":{"code":"safety_guardrail_blocked"}}`},
		{"429", http.StatusTooManyRequests, `{"error":{"code":"concurrency_limit_exceeded"}}`},
		{"502", http.StatusBadGateway, `{"error":{"code":"upstream_invalid_result"}}`},
		{"200 但缺少 url", http.StatusOK, `{"created":1,"data":[]}`},
		{"200 但 url 非 http", http.StatusOK, `{"data":[{"url":"file:///etc/passwd"}]}`},
		{"200 但带错误体", http.StatusOK, `{"error":{"code":"prompt_enhancement_refused"}}`},
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

			m := newMoss(Config{BaseURL: srv.URL, APIKey: "k"})
			resp := m.ImageGenerate(context.Background(), ImageGenerateRequest{
				VendorModelID: "moss-image-base-1.0",
				Prompt:        "a cat",
				Size:          "1024x1024",
			})

			if resp.Code != "fail" {
				t.Fatalf("Code = %q, want fail", resp.Code)
			}
			if resp.FailureKind != FailureRejected {
				t.Errorf("FailureKind = %q, want %q（MOSS 失败一律退款）", resp.FailureKind, FailureRejected)
			}
		})
	}
}

func TestMossImageGenerateTimeoutIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		io.WriteString(w, `{"data":[{"url":"https://example.com/a.png"}]}`)
	}))
	defer srv.Close()

	common.SetLongOutboundClientForTest(srv.Client())
	defer common.SetLongOutboundClientForTest(nil)

	m := newMoss(Config{BaseURL: srv.URL, APIKey: "k"})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	resp := m.ImageGenerate(ctx, ImageGenerateRequest{
		VendorModelID: "moss-image-base-1.0",
		Prompt:        "a cat",
		Size:          "1024x1024",
	})

	if resp.Code != "fail" || resp.FailureKind != FailureRejected {
		t.Errorf("超时后应可退款: code=%q kind=%q", resp.Code, resp.FailureKind)
	}
}

func TestMossTaskQueryCompletedFromStoredResult(t *testing.T) {
	m := newMoss(Config{})

	resp := m.TaskQuery(context.Background(), `{
		"url": "https://example.com/a.png",
		"usage": {"generated_images": 1}
	}`)

	if resp.Status != "completed" {
		t.Fatalf("Status = %q, want completed", resp.Status)
	}
	if resp.Kind != ResultKindImage {
		t.Errorf("Kind = %q, want image", resp.Kind)
	}
	if !resp.RequireResultFetch {
		t.Error("MOSS 任务必须要求成功下载结果资产")
	}
	if got := resp.Data["url"]; got != "https://example.com/a.png" {
		t.Errorf("url = %v", got)
	}
}

func TestMossTaskQueryInvalidStoredResultFails(t *testing.T) {
	m := newMoss(Config{})

	for _, raw := range []string{"not-json", `{}`, `{"url":""}`} {
		resp := m.TaskQuery(context.Background(), raw)
		if resp.Status != "failed" {
			t.Errorf("vendorResponse=%q: Status = %q, want failed", raw, resp.Status)
		}
	}
}

func TestMossRegisteredInFactories(t *testing.T) {
	if s := NewSupplier("MOSS", Config{}); s == nil {
		t.Error("NewSupplier(\"moss\") 不应返回 nil")
	}
	if q := NewTaskQuerier("moss", Config{}); q == nil {
		t.Error("NewTaskQuerier(\"moss\") 不应返回 nil")
	}
	if v := NewVideoSupplier("moss", Config{}); v != nil {
		t.Error("MOSS 不是视频供应商")
	}
}
