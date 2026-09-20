// Package webui 托管内嵌的前端构建产物。
//
// 前端由 Vite 构建成纯静态文件（web/dist），再通过 go:embed 编进二进制，
// 于是部署时一个容器里同时装着前端和后端，对外只需要一个端口 ——
// 与 new-api、sub2api 的形态一致。
//
// 本包负责三件事：
//
//  1. 静态文件服务。带内容哈希的 assets/ 走长缓存，其余走短缓存。
//  2. SPA 回退。前端路由（/dashboard、/admin/* 等）在服务端没有对应文件，
//     未命中的路径必须回退到 index.html，否则用户刷新页面就是 404。
//  3. HTML 响应的安全头，尤其是 CSP。这段职责原先由 Nginx 承担，
//     静态文件改由 Go 提供后必须跟着搬过来，否则 CSP 会静默消失。
package webui

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/gin-gonic/gin"
)

const (
	indexFile = "index.html"
	assetsDir = "assets/"

	// assets/ 下的文件名带内容哈希，内容一变文件名就变，可以放心长缓存。
	cacheImmutable = "public, max-age=31536000, immutable"
	// 其余静态文件（favicon.svg、icons.svg 等）名字固定，只给短缓存。
	cachePlainFiles = "public, max-age=3600"
	// index.html 不缓存，确保前端发版后用户立即拿到新的资源引用。
	cacheIndex = "no-store, must-revalidate"

	contentTypeHTML = "text/html; charset=utf-8"
)

// contentSecurityPolicy 只对 HTML 文档有意义，因此只在 index.html 的响应上下发。
//
// style-src / font-src 里的两个域名来自 web/index.html 引用的字体 CDN
// （fonts.font.im 是 Google Fonts 的国内镜像，其字体文件又可能落在
// fonts.gstatic.com）。去掉它们页面不会报错，字体会静默回落到系统字体，
// 很难察觉。更彻底的做法是把字体下载到 web/public/ 下自托管，
// 那样这两个域名可以一并删掉，也少一个外部依赖。
const contentSecurityPolicy = "default-src 'self'; " +
	"img-src 'self' data: https:; " +
	"style-src 'self' 'unsafe-inline' https://fonts.font.im; " +
	"font-src 'self' data: https://fonts.font.im https://fonts.gstatic.com; " +
	"script-src 'self'; " +
	"connect-src 'self'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

// handler 持有内嵌的文件系统与预先读出的 index.html。
type handler struct {
	fs    fs.FS
	files http.Handler
	index []byte
}

// Register 把前端静态文件挂到 engine 上。
//
// fsys 是内嵌文件系统，root 是前端产物在其中的目录（如 "web/dist"）。
// 若 root/index.html 读不到，服务照常启动、API 照常工作，
// 只是访问页面会得到一条明确的提示——比编译期失败或一个空白的 404 好排查。
func Register(engine *gin.Engine, fsys fs.FS, root string) error {
	sub, err := fs.Sub(fsys, root)
	if err != nil {
		return err
	}

	h := &handler{fs: sub, files: http.FileServer(http.FS(sub))}

	if data, err := fs.ReadFile(sub, indexFile); err == nil {
		h.index = data
	} else {
		common.SysErrorf("[webui] 读取 %s/%s 失败，前端页面将不可用（API 不受影响）: %v", root, indexFile, err)
	}

	// 用 NoRoute 而不是注册 /*filepath 通配路由：通配路由会与 router 包
	// 已注册的静态路径争抢路由树，容易触发 panic；NoRoute 只在所有已注册
	// 路由都未命中时触发，语义也更直白。
	engine.NoRoute(h.serve)
	return nil
}

func (h *handler) serve(c *gin.Context) {
	// API 命名空间下的未注册路径必须是 404，不能回退到 index.html：
	// 否则前端调用一个拼错的接口会拿到一坨 HTML 加 200，错误被掩盖成
	// 「解析失败」，排查成本极高。
	if common.IsAPIPath(c.Request.URL.Path) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"message": "Not found",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 页面只响应读请求。POST 到一个不存在的路径应当 404，而不是拿到首页。
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.Status(http.StatusNotFound)
		return
	}

	name := strings.TrimPrefix(c.Request.URL.Path, "/")
	if name == "" || name == indexFile {
		h.serveIndex(c)
		return
	}

	if !safeStaticPath(name) {
		// 隐藏文件与路径穿越一律拒绝，且绝不能回退到 index.html 返回 200
		// ——那会让扫描器以为该路径存在。
		c.Status(http.StatusNotFound)
		return
	}

	if info, err := fs.Stat(h.fs, name); err != nil || info.IsDir() {
		// 前端路由回退
		h.serveIndex(c)
		return
	}

	c.Writer.Header().Set("Cache-Control", cachePolicyFor(name))
	h.files.ServeHTTP(c.Writer, c.Request)
}

func (h *handler) serveIndex(c *gin.Context) {
	if h.index == nil {
		c.String(http.StatusServiceUnavailable,
			"前端资源未构建：请先执行 `make web`（或 `cd web && npm run build`）再编译后端。\n")
		return
	}

	header := c.Writer.Header()
	header.Set("Content-Type", contentTypeHTML)
	header.Set("Cache-Control", cacheIndex)
	header.Set("Content-Security-Policy", contentSecurityPolicy)

	c.Writer.WriteHeader(http.StatusOK)
	if c.Request.Method == http.MethodHead {
		return
	}
	if _, err := c.Writer.Write(h.index); err != nil {
		common.SysErrorf("[webui] 写出 index.html 失败: %v", err)
	}
}

// safeStaticPath 判断请求路径是否可以尝试作为静态文件处理。
//
// 拒绝两类输入：
//   - 路径穿越（..）——fs.FS 本身也会拦，这里提前挡掉避免走到 400
//   - 任一路径段以点开头——防止 .env、.git 之类被误放进前端产物目录后
//     能被直接下载
func safeStaticPath(name string) bool {
	if strings.Contains(name, "..") {
		return false
	}
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") {
			return false
		}
	}
	return true
}

// cachePolicyFor 返回静态资源的缓存策略。
func cachePolicyFor(name string) string {
	if strings.HasPrefix(name, assetsDir) {
		return cacheImmutable
	}
	return cachePlainFiles
}
