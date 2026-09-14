package common

import "strings"

// apiExactPaths 是 API 命名空间下不带尾斜杠的精确路径。
var apiExactPaths = []string{"/health", "/api", "/v1"}

// apiPrefixes 是 API 命名空间的路径前缀。
var apiPrefixes = []string{"/api/", "/v1/"}

// IsAPIPath 判断请求路径是否属于 API 命名空间。
//
// 需要这个判断，是因为 API 响应与静态资源的缓存策略正好相反：
// API 一律 no-store（响应可能含账户数据），静态资源要长缓存
// （Vite 产物带内容哈希）。中间件和静态处理器都要据此分流。
//
// 另一个用途是把「未注册的 API 路径」和「前端路由」区分开：
// 前者必须返回 404 JSON，后者要回退到 index.html。
func IsAPIPath(p string) bool {
	for _, exact := range apiExactPaths {
		if p == exact {
			return true
		}
	}
	for _, prefix := range apiPrefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}
