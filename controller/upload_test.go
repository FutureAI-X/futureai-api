package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FutureAI-X/futureai-api/model"
	"github.com/FutureAI-X/futureai-api/storage"
	"github.com/gin-gonic/gin"
)

// ── 上传后端选择 ──

func TestUploadBackendConfig(t *testing.T) {
	cases := map[string]string{
		"":            uploadBackendCOS,
		"cos":         uploadBackendCOS,
		"APIMart":     uploadBackendAPIMart,
		"  apimart ":  uploadBackendAPIMart,
		"unsupported": uploadBackendCOS,
	}
	for raw, want := range cases {
		t.Setenv(uploadBackendEnv, raw)
		if got := uploadBackend(); got != want {
			t.Errorf("IMAGE_UPLOAD_BACKEND=%q -> %q, want %q", raw, got, want)
		}
	}
}

// newUploadTestContext 构造一个已解析 multipart 的 gin 上下文。
// 预先解析是必须的：gin.Context.FormFile 会读 c.engine，而测试上下文没有 engine。
func newUploadTestContext(t *testing.T, filename string, data []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("构造 multipart 失败: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("写入 multipart 失败: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭 multipart 失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/uploads/images", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		t.Fatalf("解析 multipart 失败: %v", err)
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("user_id", 1)
	return c, rec
}

func overridePersistUploadedImage(t *testing.T, fn func(context.Context, []byte) (*model.File, error)) {
	t.Helper()
	original := persistUploadedImage
	persistUploadedImage = fn
	t.Cleanup(func() { persistUploadedImage = original })
}

func TestUploadImageStoresToCOS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(uploadBackendEnv, uploadBackendCOS)
	t.Setenv(uploadCreditsEnv, "0")
	t.Setenv(publicBaseURLEnv, "https://example.com")

	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4}
	var gotData []byte
	overridePersistUploadedImage(t, func(_ context.Context, data []byte) (*model.File, error) {
		gotData = append([]byte(nil), data...)
		return &model.File{FileID: "file_abcdef.png"}, nil
	})

	c, rec := newUploadTestContext(t, "photo.PNG", png)
	UploadImage(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if string(gotData) != string(png) {
		t.Errorf("传给存储的内容与请求体不一致")
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if want := "https://example.com/v1/images/get/file_abcdef.png"; resp["url"] != want {
		t.Errorf("url = %v, want %s", resp["url"], want)
	}
	if resp["filename"] != "photo.png" {
		t.Errorf("filename = %v, want photo.png", resp["filename"])
	}
	if resp["content_type"] != "image/png" {
		t.Errorf("content_type = %v, want image/png", resp["content_type"])
	}
	if resp["bytes"] != float64(len(png)) {
		t.Errorf("bytes = %v, want %d", resp["bytes"], len(png))
	}
}

func TestUploadImageRejectsWhenPublicBaseURLMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(uploadBackendEnv, uploadBackendCOS)
	t.Setenv(uploadCreditsEnv, "0")
	t.Setenv(publicBaseURLEnv, "")

	called := false
	overridePersistUploadedImage(t, func(context.Context, []byte) (*model.File, error) {
		called = true
		return &model.File{FileID: "file_abcdef.png"}, nil
	})

	c, rec := newUploadTestContext(t, "photo.png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4})
	UploadImage(c)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("状态码 = %d, want 503", rec.Code)
	}
	if called {
		t.Error("缺少对外地址时不应上传")
	}
}

func TestUploadImageCOSNotConfiguredReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(uploadBackendEnv, uploadBackendCOS)
	t.Setenv(uploadCreditsEnv, "0")
	t.Setenv(publicBaseURLEnv, "https://example.com")

	overridePersistUploadedImage(t, func(context.Context, []byte) (*model.File, error) {
		return nil, storage.ErrNotConfigured
	})

	c, rec := newUploadTestContext(t, "photo.png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4})
	UploadImage(c)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("状态码 = %d, want 503", rec.Code)
	}
}

// ── sanitizeFilename：阻断路径穿越与 multipart 头注入 ──

func TestSanitizeFilenameStripsCRLF(t *testing.T) {
	// 这是 M2 的实际攻击载荷：RFC 2231 解码后 CR/LF 会进入文件名
	evil := "evil\r\nX-Injected: pwned\r\n"
	got := sanitizeFilename(evil)

	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("结果仍含 CR/LF，可造成 multipart 头注入: %q", got)
	}
	if strings.Contains(got, ":") {
		t.Errorf("结果仍含冒号，可构造伪头部: %q", got)
	}
}

func TestSanitizeFilenameStripsPathTraversal(t *testing.T) {
	cases := []string{
		"../../etc/passwd",
		"..\\..\\windows\\system32\\config",
		"/absolute/path/image.png",
		"C:\\Users\\evil\\image.png",
	}
	for _, in := range cases {
		got := sanitizeFilename(in)
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("sanitizeFilename(%q) = %q，仍含路径分隔符", in, got)
		}
		if strings.HasPrefix(got, ".") {
			t.Errorf("sanitizeFilename(%q) = %q，仍为隐藏/相对路径", in, got)
		}
	}
}

func TestSanitizeFilenameHandlesDegenerateInput(t *testing.T) {
	cases := map[string]string{
		"":          "image",
		"..":        "image",
		".":         "image",
		"...":       "image",
		"\r\n":      "image",
		"   ":       "image",
		"../../../": "image",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeFilenameTruncatesLongNames(t *testing.T) {
	long := strings.Repeat("a", 500) + ".png"
	got := sanitizeFilename(long)

	if len([]rune(got)) > maxFilenameLen {
		t.Errorf("文件名长度 %d 超过上限 %d", len([]rune(got)), maxFilenameLen)
	}
}

func TestSanitizeFilenamePreservesNormalNames(t *testing.T) {
	for _, name := range []string{"photo.png", "my-image_01.jpg", "截图2026.png"} {
		if got := sanitizeFilename(name); got != name {
			t.Errorf("正常文件名不应被改动: sanitizeFilename(%q) = %q", name, got)
		}
	}
}

// ── enforceImageExtension：强制扩展名与真实类型一致 ──

// TestEnforceImageExtensionBlocksHTMLDisguise 覆盖一个真实风险：
// 用户可以把 HTML/GIF 多态文件命名为 .html，借助上游 CDN 的按扩展名分发
// 策略制造存储型 XSS。
func TestEnforceImageExtensionBlocksHTMLDisguise(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		wantExt     string
	}{
		{"evil.html", "image/png", ".png"},
		{"evil.svg", "image/png", ".png"},
		{"evil.htm", "image/jpeg", ".jpg"},
		{"evil.js", "image/gif", ".gif"},
		{"noext", "image/webp", ".webp"},
	}
	for _, tc := range cases {
		got := enforceImageExtension(tc.name, tc.contentType)
		if !strings.HasSuffix(got, tc.wantExt) {
			t.Errorf("enforceImageExtension(%q, %q) = %q, 期望以 %s 结尾",
				tc.name, tc.contentType, got, tc.wantExt)
		}
		if strings.Contains(got, ".html") || strings.Contains(got, ".svg") {
			t.Errorf("危险扩展名未被替换: %q", got)
		}
	}
}

func TestEnforceImageExtensionReplacesExistingExtension(t *testing.T) {
	got := enforceImageExtension("photo.bmp", "image/png")
	if got != "photo.png" {
		t.Errorf("got %q, want %q", got, "photo.png")
	}
}

func TestEnforceImageExtensionKeepsBaseName(t *testing.T) {
	got := enforceImageExtension("holiday-photo", "image/jpeg")
	if got != "holiday-photo.jpg" {
		t.Errorf("got %q, want %q", got, "holiday-photo.jpg")
	}
}

// ── detectImageType：magic bytes 识别 ──

func TestDetectImageType(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"JPEG", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0}, "image/jpeg"},
		{"PNG", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}, "image/png"},
		{"GIF87a", []byte("GIF87a\x00\x00\x00\x00\x00\x00"), "image/gif"},
		{"GIF89a", []byte("GIF89a\x00\x00\x00\x00\x00\x00"), "image/gif"},
		{"HTML", []byte("<!DOCTYPE html><html>"), ""},
		{"SVG", []byte("<svg xmlns=\"http://www.w3.org/2000/svg\">"), ""},
		{"too short", []byte{0xFF, 0xD8}, ""},
		{"empty", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectImageType(tc.data); got != tc.want {
				t.Errorf("detectImageType() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDetectImageTypeWebP(t *testing.T) {
	// RIFF....WEBP
	data := []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")
	if got := detectImageType(data); got != "image/webp" {
		t.Errorf("got %q, want %q", got, "image/webp")
	}
}
