package middleware

import (
	"github.com/FutureAI/token-hub/common"
	"github.com/gin-gonic/gin"
)

// SecurityHeaders 为所有响应添加基础安全响应头。
//
// 缓存策略只对 API 生效：静态资源由 webui 包自行下发 Cache-Control
// （Vite 产物带内容哈希，必须长缓存），两者正好相反。
// 原先这里对所有响应无条件写 no-store，服务只发 API 时没问题，
// 一旦开始托管静态文件就会把 JS/CSS 的缓存全部打掉。
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		// 禁止浏览器对响应体做 MIME 类型嗅探
		h.Set("X-Content-Type-Options", "nosniff")
		// 禁止被嵌入 iframe，防点击劫持
		h.Set("X-Frame-Options", "DENY")
		// 不向外部站点泄露完整 URL（含路径中的资源 ID）
		h.Set("Referrer-Policy", "no-referrer")
		// API 服务不需要任何浏览器特性
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		// API 响应不应被缓存（可能包含账户数据）
		if common.IsAPIPath(c.Request.URL.Path) {
			h.Set("Cache-Control", "no-store")
		}

		c.Next()
	}
}

// HSTS 仅在 HTTPS 部署时才有意义，因此由反向代理（Nginx/CDN）负责下发，
// 不在此处硬编码，避免本地 HTTP 开发时把浏览器锁死到 HTTPS。
