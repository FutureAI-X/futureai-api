package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/gin-gonic/gin"
)

// ── GetImageFile 参数校验 ──

func TestGetImageFileRejectsInvalidFileID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := map[string]string{
		"空 ID":  "",
		"超长 ID": strings.Repeat("a", model.MaxFileIDLen+1),
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/images/get/x", nil)
			c.Params = gin.Params{{Key: "file_id", Value: id}}

			// 非法 ID 不应触达数据库；若实现了校验，此处不会因 DB 为 nil 而 panic
			GetImageFile(c)

			if rec.Code != http.StatusNotFound {
				t.Errorf("状态码 = %d, want 404", rec.Code)
			}
		})
	}
}

// ── fileExpired ──

func TestFileExpired(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Second)
	past := now.Add(-time.Second)
	zero := time.Time{}

	cases := []struct {
		name      string
		expiresAt *time.Time
		want      bool
	}{
		{"NULL 表示长期有效", nil, false},
		{"零值表示长期有效", &zero, false},
		{"未来时间未过期", &future, false},
		{"过去时间已过期", &past, true},
		{"恰好等于当前时间视为过期", &now, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fileExpired(tc.expiresAt, now); got != tc.want {
				t.Errorf("fileExpired() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── parseFileURL ──

func TestParseFileURL(t *testing.T) {
	valid := []string{
		"https://cdn.example.com/a/photo.png",
		"http://8.8.8.8/a/photo.png?sign=abc",
	}
	for _, raw := range valid {
		if _, err := parseFileURL(raw); err != nil {
			t.Errorf("合法地址 %q 被拒绝: %v", raw, err)
		}
	}

	invalid := map[string]string{
		"非 http 协议": "ftp://example.com/a.png",
		"file 协议":   "file:///etc/passwd",
		"缺少主机名":     "https:///a.png",
		"携带用户凭据":    "https://user:pass@8.8.8.8/a.png",
		"空地址":       "",
		"相对路径":      "/a/photo.png",
	}
	for name, raw := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := parseFileURL(raw); err == nil {
				t.Errorf("%q 应被拒绝", raw)
			}
		})
	}
}

// ── fileResponseHeaders ──

func TestFileResponseHeaders(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		wantType    string
		wantDisp    string
	}{
		{"PNG 内联", "image/png", "image/png", "inline"},
		{"JPEG 内联（带 charset 参数）", "image/jpeg; charset=binary", "image/jpeg", "inline"},
		{"WebP 内联", "image/webp", "image/webp", "inline"},
		{"GIF 内联", "image/gif", "image/gif", "inline"},
		{"SVG 可执行脚本，强制下载", "image/svg+xml", "application/octet-stream", "attachment"},
		{"HTML 强制下载", "text/html", "application/octet-stream", "attachment"},
		{"缺少类型按二进制处理", "", "application/octet-stream", "attachment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotType, gotDisp := fileResponseHeaders(tc.contentType, "photo.png")
			if gotType != tc.wantType {
				t.Errorf("Content-Type = %q, want %q", gotType, tc.wantType)
			}
			if !strings.HasPrefix(gotDisp, tc.wantDisp) {
				t.Errorf("Content-Disposition = %q, 期望以 %q 开头", gotDisp, tc.wantDisp)
			}
		})
	}
}

// ── filenameFromURL ──

func TestFilenameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://cdn.example.com/a/photo.png":       "photo.png",
		"https://cdn.example.com/a/photo.png?sig=1": "photo.png",
		"https://cdn.example.com/":                  "file",
		"https://cdn.example.com":                   "file",
	}
	for raw, want := range cases {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("测试地址解析失败: %v", err)
		}
		if got := filenameFromURL(u); got != want {
			t.Errorf("filenameFromURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestFilenameFromURLDecodesAndSanitizes(t *testing.T) {
	u, err := url.Parse("https://cdn.example.com/%E6%88%AA%E5%9B%BE.png")
	if err != nil {
		t.Fatalf("测试地址解析失败: %v", err)
	}
	if got := filenameFromURL(u); got != "截图.png" {
		t.Errorf("中文文件名未正确解码: %q", got)
	}

	// 百分号编码的 CRLF 解码后必须被清洗掉，否则可注入响应头
	u2, err := url.Parse("https://cdn.example.com/a%0d%0aX-Injected:%20pwned.png")
	if err != nil {
		t.Fatalf("测试地址解析失败: %v", err)
	}
	got := filenameFromURL(u2)
	if strings.ContainsAny(got, "\r\n") || strings.Contains(got, ":") {
		t.Errorf("文件名仍含 CR/LF 或冒号: %q", got)
	}
}

// ── fileCacheControl ──

func TestFileCacheControl(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	hourLater := now.Add(time.Hour)
	twoHoursLater := now.Add(2 * time.Hour)
	twoDaysLater := now.Add(48 * time.Hour)
	past := now.Add(-time.Hour)
	zero := time.Time{}

	cases := []struct {
		name      string
		expiresAt *time.Time
		want      string
	}{
		{"长期有效按上限缓存", nil, "public, max-age=86400"},
		{"零值按上限缓存", &zero, "public, max-age=86400"},
		{"按剩余有效期截断", &hourLater, "public, max-age=3600"},
		{"剩余有效期不足上限时用剩余时间", &twoHoursLater, "public, max-age=7200"},
		{"剩余有效期超过上限时用上限", &twoDaysLater, "public, max-age=86400"},
		{"已过期时不允许缓存", &past, "public, max-age=0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fileCacheControl(tc.expiresAt, now); got != tc.want {
				t.Errorf("fileCacheControl() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ── proxyFileContent：流式转发 ──

// overrideFileDownloadClient 让被测代码走 httptest 的回环地址。
// 生产配置的拨号器会拦截回环地址（SSRF 防护），测试必须绕过。
func overrideFileDownloadClient(t *testing.T, c *http.Client) {
	t.Helper()
	common.SetFileDownloadClientForTest(c)
	t.Cleanup(func() { common.SetFileDownloadClientForTest(nil) })
}

func newProxyTestContext(t *testing.T, headers map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/images/get/file_test", nil)
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	return c, rec
}

func TestProxyFileContentStreamsImageInline(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 1, 2, 3}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept-Encoding"); got != "identity" {
			t.Errorf("Accept-Encoding = %q, want identity", got)
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", "12")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Write(png)
	}))
	defer upstream.Close()
	overrideFileDownloadClient(t, upstream.Client())

	c, rec := newProxyTestContext(t, nil)
	record := &model.File{FileID: "file_test", URL: upstream.URL + "/photo.png"}

	proxyFileContent(c, record, time.Now())

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", rec.Code)
	}
	if !strings.EqualFold(rec.Body.String(), string(png)) {
		t.Errorf("响应体与上游内容不一致: %q", rec.Body.Bytes())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Errorf("Content-Disposition = %q, 期望 inline", got)
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q, want bytes", got)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.HasPrefix(got, "public, max-age=") {
		t.Errorf("Cache-Control = %q, 期望可缓存", got)
	}
}

func TestProxyFileContentForwardsRange(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=0-3" {
			t.Errorf("Range = %q, want bytes=0-3", got)
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Range", "bytes 0-3/12")
		w.Header().Set("Content-Length", "4")
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("0123"))
	}))
	defer upstream.Close()
	overrideFileDownloadClient(t, upstream.Client())

	c, rec := newProxyTestContext(t, map[string]string{"Range": "bytes=0-3"})
	record := &model.File{FileID: "file_test", URL: upstream.URL + "/photo.png"}

	proxyFileContent(c, record, time.Now())

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("状态码 = %d, want 206", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 0-3/12" {
		t.Errorf("Content-Range = %q 未透传", got)
	}
	if rec.Body.String() != "0123" {
		t.Errorf("响应体 = %q, want 0123", rec.Body.String())
	}
}

func TestProxyFileContentForcesDownloadForHTML(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<script>alert(1)</script>"))
	}))
	defer upstream.Close()
	overrideFileDownloadClient(t, upstream.Client())

	c, rec := newProxyTestContext(t, nil)
	record := &model.File{FileID: "file_test", URL: upstream.URL + "/evil.html"}

	proxyFileContent(c, record, time.Now())

	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q, 非图片类型必须降级为 octet-stream", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("Content-Disposition = %q, 期望 attachment", got)
	}
}

func TestProxyFileContentUpstreamErrorReturns502(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()
	overrideFileDownloadClient(t, upstream.Client())

	c, rec := newProxyTestContext(t, nil)
	record := &model.File{FileID: "file_test", URL: upstream.URL + "/missing.png"}

	proxyFileContent(c, record, time.Now())

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("状态码 = %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "文件暂时无法获取") {
		t.Errorf("响应体 = %q, 期望通用错误信息", rec.Body.String())
	}
}

func TestProxyFileContentRejectsInvalidURL(t *testing.T) {
	c, rec := newProxyTestContext(t, nil)
	record := &model.File{FileID: "file_test", URL: "file:///etc/passwd"}

	proxyFileContent(c, record, time.Now())

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("状态码 = %d, want 502", rec.Code)
	}
}

// TestProxyFileContentFallsBackForUnknownServiceType 验证本服务不认识的
// service_type 仍按 url 直连，兼容历史记录与外部写入方。
func TestProxyFileContentFallsBackForUnknownServiceType(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3})
	}))
	defer upstream.Close()
	overrideFileDownloadClient(t, upstream.Client())

	c, rec := newProxyTestContext(t, nil)
	record := &model.File{
		FileID:      "file_test",
		ServiceType: "SOME_OTHER_STORAGE",
		URL:         upstream.URL + "/photo.png",
	}

	proxyFileContent(c, record, time.Now())

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", rec.Code)
	}
}

// TestProxyFileContentUsesTrustedClientForStorage 验证对象存储记录走可信端点客户端。
// 腾讯云 COS 在云主机内解析到 169.254.0.0/16，通用客户端会拦截；这里用回环地址
// 模拟同类目标：只有走 TrustedEndpointHTTPClient 才能读取成功。
func TestProxyFileContentUsesTrustedClientForStorage(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 9, 9, 9}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	}))
	defer upstream.Close()

	t.Setenv("TENCENT_COS_BUCKET_URL", upstream.URL)
	t.Setenv("TENCENT_COS_SECRET_ID", "test-id")
	t.Setenv("TENCENT_COS_SECRET_KEY", "test-key")
	t.Setenv(common.OutboundProxyEnv, "")

	c, rec := newProxyTestContext(t, nil)
	record := &model.File{
		FileID:         "file_test",
		ServiceType:    "TENCENT_COS",
		ServiceAddress: upstream.URL,
		FileKey:        "png/file_test.png",
		URL:            upstream.URL + "/png/file_test.png",
	}

	proxyFileContent(c, record, time.Now())

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.EqualFold(rec.Body.String(), string(png)) {
		t.Errorf("响应体与上游内容不一致")
	}
}

// TestProxyFileContentTencentCOSWithoutCredentials 验证私有桶记录在
// 缺少密钥时明确失败，而不是拿私有 URL 去请求后得到 403。
func TestProxyFileContentTencentCOSWithoutCredentials(t *testing.T) {
	t.Setenv("TENCENT_COS_BUCKET_URL", "")
	t.Setenv("TENCENT_COS_SECRET_ID", "")
	t.Setenv("TENCENT_COS_SECRET_KEY", "")

	c, rec := newProxyTestContext(t, nil)
	record := &model.File{
		FileID:         "file_test",
		ServiceType:    "TENCENT_COS",
		ServiceAddress: "https://bucket.example.com",
		FileKey:        "png/file_test.png",
		URL:            "https://bucket.example.com/png/file_test.png",
	}

	proxyFileContent(c, record, time.Now())

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("状态码 = %d, want 502", rec.Code)
	}
}
