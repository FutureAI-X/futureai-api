package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FutureAI-X/futureai-api/model"
	"github.com/gin-gonic/gin"
)

// TestPublicFileRouteNeedsNoAuth 验证 /v1/images/get/:file_id 没有被
// APIAuth 拦下：带一个超长（必然非法）的 ID 请求，期望拿到处理函数
// 返回的 404，而不是认证中间件的 401。
func TestPublicFileRouteNeedsNoAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := gin.New()
	SetRouter(server)

	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/images/get/"+strings.Repeat("a", model.MaxFileIDLen+1),
		nil,
	)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Fatal("公开文件读取不应要求认证")
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("状态码 = %d, want 404（非法 ID 由处理函数返回）", rec.Code)
	}
}

// TestAuthenticatedV1RouteStillRequiresAuth 作为对照：同一前缀下的
// 认证端点仍必须要求 API Key。
func TestAuthenticatedV1RouteStillRequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := gin.New()
	SetRouter(server)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("状态码 = %d, want 401", rec.Code)
	}
}
