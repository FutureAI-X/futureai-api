package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

const (
	indexBody = "<!doctype html><html><body>token-hub</body></html>"
	jsBody    = "console.log('token hub')"
	svgBody   = "<svg></svg>"
)

// newTestEngine 用内存文件系统搭一个最小引擎，避免测试依赖真实的 web/dist。
func newTestEngine(t *testing.T, withIndex bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	files := fstest.MapFS{
		"web/dist/assets/index-Br4SuDVP.js": &fstest.MapFile{Data: []byte(jsBody)},
		"web/dist/favicon.svg":              &fstest.MapFile{Data: []byte(svgBody)},
	}
	if withIndex {
		files["web/dist/index.html"] = &fstest.MapFile{Data: []byte(indexBody)}
	}

	engine := gin.New()
	if err := Register(engine, files, "web/dist"); err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	return engine
}

func doRequest(engine *gin.Engine, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// TestRootServesIndex 根路径必须返回 index.html
func TestRootServesIndex(t *testing.T) {
	engine := newTestEngine(t, true)
	rec := doRequest(engine, http.MethodGet, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if rec.Body.String() != indexBody {
		t.Errorf("响应体不是 index.html：%q", rec.Body.String())
	}
}

// TestSpaFallbackServesIndex 是最容易漏的一条：
// /dashboard 这类前端路由在服务端没有文件，必须回退到 index.html，
// 否则用户刷新页面就是 404。
func TestSpaFallbackServesIndex(t *testing.T) {
	engine := newTestEngine(t, true)
	for _, path := range []string{"/dashboard", "/admin/users", "/models/deepseek"} {
		rec := doRequest(engine, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200（SPA 回退）", path, rec.Code)
			continue
		}
		if rec.Body.String() != indexBody {
			t.Errorf("GET %s 未回退到 index.html", path)
		}
	}
}

// TestIndexIsNotCachedAndCarriesCSP index.html 不缓存，且必须带 CSP。
// CSP 原先由 Nginx 下发，静态文件改由 Go 提供后如果没搬过来，
// 这个安全头会静默消失——所以专门盯住它。
func TestIndexIsNotCachedAndCarriesCSP(t *testing.T) {
	engine := newTestEngine(t, true)
	rec := doRequest(engine, http.MethodGet, "/")

	if got := rec.Header().Get("Cache-Control"); got != cacheIndex {
		t.Errorf("index.html Cache-Control = %q, want %q", got, cacheIndex)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("index.html 缺少 Content-Security-Policy")
	}
	if !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP 内容异常: %q", csp)
	}
}

// TestAssetsAreLongCached Vite 产物带内容哈希，必须长缓存
func TestAssetsAreLongCached(t *testing.T) {
	engine := newTestEngine(t, true)
	rec := doRequest(engine, http.MethodGet, "/assets/index-Br4SuDVP.js")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET assets = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != cacheImmutable {
		t.Errorf("assets Cache-Control = %q, want %q", got, cacheImmutable)
	}
	if rec.Body.String() != jsBody {
		t.Errorf("响应体不正确: %q", rec.Body.String())
	}
}

// TestRootLevelStaticFilesAreShortCached favicon 这类固定文件名只给短缓存
func TestRootLevelStaticFilesAreShortCached(t *testing.T) {
	engine := newTestEngine(t, true)
	rec := doRequest(engine, http.MethodGet, "/favicon.svg")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET favicon.svg = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != cachePlainFiles {
		t.Errorf("favicon Cache-Control = %q, want %q", got, cachePlainFiles)
	}
	// 静态资源不应带上只对 HTML 有意义的 CSP
	if csp := rec.Header().Get("Content-Security-Policy"); csp != "" {
		t.Errorf("静态资源不应下发 CSP: %q", csp)
	}
}

// TestUnknownAPIPathReturns404JSON 未注册的 API 路径必须返回 404 JSON。
// 如果这里回退成 index.html，前端调用拼错的接口会拿到 200 + 一坨 HTML，
// 错误被掩盖成解析失败，非常难查。
//
// 注意 /health 是精确端点而非命名空间，/health/extra 不属于 API，
// 会走前端路由回退——这与浏览器的预期一致。
func TestUnknownAPIPathReturns404JSON(t *testing.T) {
	engine := newTestEngine(t, true)
	for _, path := range []string{"/api/nope", "/v1/nope", "/api/user/nope"} {
		rec := doRequest(engine, http.MethodGet, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
			continue
		}
		if strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("GET %s 返回了 HTML，应为 JSON 错误", path)
		}
		if !strings.Contains(rec.Body.String(), "invalid_request_error") {
			t.Errorf("GET %s 响应体不是预期的错误结构: %q", path, rec.Body.String())
		}
	}
}

// TestHiddenFilesAreRejected 隐藏文件必须 404，且不能回退成 200 的首页。
// 静态根目录万一被放进 .env 之类文件，这里是最后一道拦截。
func TestHiddenFilesAreRejected(t *testing.T) {
	engine := newTestEngine(t, true)
	for _, path := range []string{"/.env", "/.git/config", "/assets/.hidden"} {
		rec := doRequest(engine, http.MethodGet, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}

// TestNonReadMethodsReturn404 POST 到一个不存在的页面路径应当 404，而不是首页。
func TestNonReadMethodsReturn404(t *testing.T) {
	engine := newTestEngine(t, true)
	rec := doRequest(engine, http.MethodPost, "/dashboard")
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /dashboard = %d, want 404", rec.Code)
	}
}

// TestExplicitIndexPathServesHTML 直接请求 /index.html 也应当返回页面本身，
// 而不是被 http.FileServer 301 重定向走。
func TestExplicitIndexPathServesHTML(t *testing.T) {
	engine := newTestEngine(t, true)
	rec := doRequest(engine, http.MethodGet, "/index.html")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /index.html = %d, want 200", rec.Code)
	}
	if rec.Body.String() != indexBody {
		t.Errorf("响应体不是 index.html：%q", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != cacheIndex {
		t.Errorf("Cache-Control = %q, want %q", got, cacheIndex)
	}
}

// TestMissingIndexYields503 忘了构建前端时，必须给出可读的提示而不是空白 404。
func TestMissingIndexYields503(t *testing.T) {
	engine := newTestEngine(t, false)
	rec := doRequest(engine, http.MethodGet, "/")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("未构建前端时 GET / = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "npm run build") {
		t.Errorf("提示信息应说明如何构建前端，实际: %q", rec.Body.String())
	}
}

// TestHeadRequestHasNoBody HEAD 应当拿到与 GET 一致的头，但没有响应体。
func TestHeadRequestHasNoBody(t *testing.T) {
	engine := newTestEngine(t, true)
	rec := doRequest(engine, http.MethodHead, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD / = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD 不应有响应体，实际 %d 字节", rec.Body.Len())
	}
}

func TestSafeStaticPath(t *testing.T) {
	cases := map[string]bool{
		"assets/index-abc.js": true,
		"favicon.svg":         true,
		"icons/logo.svg":      true,
		"..":                  false,
		"../secret":           false,
		"assets/../../secret": false,
		".env":                false,
		".git/config":         false,
		"assets/.hidden":      false,
	}
	for in, want := range cases {
		if got := safeStaticPath(in); got != want {
			t.Errorf("safeStaticPath(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCachePolicyFor(t *testing.T) {
	cases := map[string]string{
		"assets/index-abc.js":  cacheImmutable,
		"assets/index-abc.css": cacheImmutable,
		"favicon.svg":          cachePlainFiles,
		"icons.svg":            cachePlainFiles,
	}
	for in, want := range cases {
		if got := cachePolicyFor(in); got != want {
			t.Errorf("cachePolicyFor(%q) = %q, want %q", in, got, want)
		}
	}
}
