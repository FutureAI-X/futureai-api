package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/FutureAI-X/futureai-api/storage"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// fileCacheMaxAge 成功响应的浏览器缓存上限。
// 文件内容不可变（同一 ID 永远指向同一份内容），可以放心缓存；
// 仍按 expires_at 截断，避免文件过期后浏览器还继续提供副本。
const fileCacheMaxAge = 24 * time.Hour

// presignTTL 私有对象临时地址的有效期。
// 只在「代理这一跳」使用：拿到后立即发起上游请求，5 分钟足够覆盖
// 连接排队与重定向，也不需要把长期凭证写进 files.url。
const presignTTL = 5 * time.Minute

// inlineFileTypes 允许在浏览器中直接内联打开的内容类型。
//
// 刻意不包含 image/svg+xml：SVG 可以内嵌脚本，而本接口与前端管理后台
// 同源，内联渲染等于在本域下执行上传方提供的内容（存储型 XSS）。
// 其余类型一律降级为 application/octet-stream + attachment，
// 浏览器会下载而不是渲染，代码客户端不受影响。
var inlineFileTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
	"image/gif":  {},
	"image/avif": {},
	"image/bmp":  {},
}

// GetImageFile 公开文件读取端点
// GET /v1/images/get/:file_id
//
// 无需认证：文件 ID 本身即访问凭证，因此 ID 必须随机不可猜（见
// model.GenerateFileID）。限流由路由上的 IPRateLimit 中间件负责。
func GetImageFile(c *gin.Context) {
	fileID := c.Param("file_id")
	if fileID == "" || len(fileID) > model.MaxFileIDLen {
		fileNotFound(c)
		return
	}

	record, err := model.GetFileByFileID(fileID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fileNotFound(c)
			return
		}
		common.SysErrorf("[GetImageFile] 查询文件记录失败: file_id=%s, err=%v", fileID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "服务暂时不可用，请稍后再试"})
		return
	}

	now := time.Now()
	if fileExpired(record.ExpiresAt, now) {
		c.JSON(http.StatusGone, gin.H{"code": "fail", "message": "文件已过期"})
		return
	}

	proxyFileContent(c, record, now)
}

// fileExpired 判断记录是否已过期。expires_at 为空（NULL 或零值）表示长期有效。
func fileExpired(expiresAt *time.Time, now time.Time) bool {
	return expiresAt != nil && !expiresAt.IsZero() && !expiresAt.After(now)
}

// proxyFileContent 从记录中的完整 URL 拉取内容并流式转发。
//
// 全程不落盘、不整份读入内存：文件可能是几十 MB 的视频，缓冲下来既慢
// 又会让并发数直接受内存限制。上游读取沿用请求自身的 context，
// 客户端断开时上游连接会一并取消。
func proxyFileContent(c *gin.Context, record *model.File, now time.Time) {
	target, err := resolveFileURL(c.Request.Context(), record)
	if err != nil {
		common.SysErrorf("[GetImageFile] 解析文件地址失败: file_id=%s, err=%v", record.FileID, err)
		c.JSON(http.StatusBadGateway, gin.H{"code": "fail", "message": "文件暂时无法获取"})
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		common.SysErrorf("[GetImageFile] 构造上游请求失败: file_id=%s, err=%v", record.FileID, err)
		c.JSON(http.StatusBadGateway, gin.H{"code": "fail", "message": "文件暂时无法获取"})
		return
	}
	// 显式要求不压缩：Go 的 Transport 只在它自己添加 Accept-Encoding 时才
	// 透明解压，若上游主动压缩而我们又不转发 Content-Encoding，
	// 客户端拿到的会是解压不一致的字节。
	req.Header.Set("Accept-Encoding", "identity")
	// 透传 Range，支持大文件的断点续传与视频拖动
	if r := c.GetHeader("Range"); r != "" {
		req.Header.Set("Range", r)
	}
	if ir := c.GetHeader("If-Range"); ir != "" {
		req.Header.Set("If-Range", ir)
	}

	resp, err := common.FileDownloadHTTPClient().Do(req)
	if err != nil {
		// 客户端主动断开时这里也会返回错误（请求 context 已取消），
		// 那不是故障，不写错误日志。
		if c.Request.Context().Err() == nil {
			common.SysErrorf("[GetImageFile] 拉取文件失败: file_id=%s, err=%v", record.FileID, err)
		}
		c.JSON(http.StatusBadGateway, gin.H{"code": "fail", "message": "文件暂时无法获取"})
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
		// 正常内容，继续转发
	case http.StatusRequestedRangeNotSatisfiable:
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			c.Header("Content-Range", cr)
		}
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		return
	default:
		common.SysErrorf("[GetImageFile] 上游返回异常状态: file_id=%s, status=%d", record.FileID, resp.StatusCode)
		c.JSON(http.StatusBadGateway, gin.H{"code": "fail", "message": "文件暂时无法获取"})
		return
	}

	contentType, disposition := fileResponseHeaders(resp.Header.Get("Content-Type"), filenameFromURL(target))
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", disposition)
	// 仅在真正支持分片时声明。无条件写 bytes 会让客户端以为可以续传，
	// 而上游其实会忽略 Range 并返回完整内容。
	if ar := resp.Header.Get("Accept-Ranges"); ar != "" {
		c.Header("Accept-Ranges", ar)
	} else if resp.StatusCode == http.StatusPartialContent {
		c.Header("Accept-Ranges", "bytes")
	}
	// 覆盖 SecurityHeaders 给 API 路径设置的 no-store：文件内容不可变，可缓存
	c.Header("Cache-Control", fileCacheControl(record.ExpiresAt, now))
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		c.Header("Content-Length", cl)
	}
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		c.Header("Content-Range", cr)
	}
	// 上游若无视 identity 仍返回压缩内容，必须把编码告诉客户端，
	// 否则客户端会把压缩字节当成原始文件。
	if ce := resp.Header.Get("Content-Encoding"); ce != "" {
		c.Header("Content-Encoding", ce)
	}

	c.Status(resp.StatusCode)
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		// 头已经发出，无法再改状态码；客户端断开属正常情况
		if c.Request.Context().Err() == nil {
			common.SysErrorf("[GetImageFile] 流式转发中断: file_id=%s, err=%v", record.FileID, err)
		}
	}
}

// fileNotFound 统一的 404 响应
func fileNotFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"code": "fail", "message": "文件不存在"})
}

// publicBaseURLEnv 对外访问地址的环境变量名。
// 用于把文件 ID 拼成调用方可以直接访问的绝对 URL。
const publicBaseURLEnv = "PUBLIC_BASE_URL"

var warnPublicBaseURLOnce sync.Once

// configuredPublicBaseURL 读取对外访问地址。
//
// 支持完整 origin（https://futureaiapi.com）或裸域名（futureaiapi.com，按 https 处理），
// 末尾斜杠会被去掉。未配置或非法时返回空串，调用方回落到上游原始地址。
func configuredPublicBaseURL() string {
	raw := strings.TrimSpace(os.Getenv(publicBaseURLEnv))
	if raw == "" {
		warnPublicBaseURLOnce.Do(func() {
			common.SysErrorf("[PublicURL] %s 未配置，任务响应中的 url 将回落到上游原始地址", publicBaseURLEnv)
		})
		return ""
	}

	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		warnPublicBaseURLOnce.Do(func() {
			common.SysErrorf("[PublicURL] %s 配置无效(%q)，任务响应中的 url 将回落到上游原始地址", publicBaseURLEnv, raw)
		})
		return ""
	}

	if u.Path != "" && u.Path != "/" {
		warnPublicBaseURLOnce.Do(func() {
			common.SysErrorf("[PublicURL] %s 包含路径(%q)，已忽略；请只配置协议与域名", publicBaseURLEnv, u.Path)
		})
	}
	return u.Scheme + "://" + u.Host
}

// publicFileURL 生成文件的公开读取地址；未配置对外地址时返回空串。
func publicFileURL(fileID string) string {
	if fileID == "" {
		return ""
	}
	base := configuredPublicBaseURL()
	if base == "" {
		return ""
	}
	return base + "/v1/images/get/" + url.PathEscape(fileID)
}

// resolveFileURL 决定实际拉取地址。
//
// 写入侧把成图转存到私有桶，files.url 只是对象的规范地址，不能匿名读取；
// 因此 TENCENT_COS 记录要用 file_key 现签一个短期地址。service_type 为空
// 的历史记录（以及将来由外部写入、但本服务不认识的服务）仍按 url 直连。
func resolveFileURL(ctx context.Context, record *model.File) (*url.URL, error) {
	raw := record.URL

	if strings.TrimSpace(record.ServiceType) != "" {
		store, err := storage.FromEnv(record.ServiceType, record.ServiceAddress)
		switch {
		case err == nil:
			if strings.TrimSpace(record.FileKey) == "" {
				return nil, errors.New("记录缺少 file_key")
			}
			signed, signErr := store.PresignGet(ctx, record.FileKey, presignTTL)
			if signErr != nil {
				return nil, signErr
			}
			raw = signed
		case errors.Is(err, storage.ErrNotSupported):
			// 本服务不认识的服务类型：可能是公开地址，回落到 url
		default:
			return nil, err
		}
	}

	return parseFileURL(raw)
}

// parseFileURL 校验并解析记录中的完整 URL。
// 只接受 http(s) 且必须带主机名；带用户凭据的地址一并拒绝，
// 避免把存储侧的账号密码长期留在库里又被悄悄使用。
func parseFileURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("仅支持 http/https，当前为 %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("缺少主机名")
	}
	if u.User != nil {
		return nil, errors.New("URL 不允许携带用户名/密码")
	}
	return u, nil
}

// fileResponseHeaders 决定响应的 Content-Type 与 Content-Disposition。
// 只有惰性图片类型内联返回，其余一律强制下载（理由见 inlineFileTypes）。
func fileResponseHeaders(rawContentType, filename string) (string, string) {
	mediaType, _, err := mime.ParseMediaType(rawContentType)
	if err != nil {
		mediaType = ""
	}

	if _, ok := inlineFileTypes[mediaType]; ok {
		return mediaType, formatContentDisposition("inline", filename)
	}
	return "application/octet-stream", formatContentDisposition("attachment", filename)
}

// formatContentDisposition 生成 Content-Disposition 头。
// 使用 mime.FormatMediaType 做 RFC 2231 编码，文件名中的引号、换行等
// 不会注入额外的响应头。
func formatContentDisposition(kind, filename string) string {
	if filename == "" {
		filename = "file"
	}
	return mime.FormatMediaType(kind, map[string]string{"filename": filename})
}

// filenameFromURL 从 URL 路径推导下载文件名。
// 仅用于 Content-Disposition；解析不出来时回落到 "file"。
func filenameFromURL(u *url.URL) string {
	name := path.Base(u.Path)
	if name == "." || name == "/" || name == "" {
		return "file"
	}
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	return sanitizeFilename(name)
}

// fileCacheControl 生成成功响应的 Cache-Control。
// 缓存时长取「剩余有效期」与上限中的较小者；长期有效的记录按上限缓存。
func fileCacheControl(expiresAt *time.Time, now time.Time) string {
	maxAge := fileCacheMaxAge
	if expiresAt != nil && !expiresAt.IsZero() {
		remaining := expiresAt.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
		if remaining < maxAge {
			maxAge = remaining
		}
	}
	return fmt.Sprintf("public, max-age=%d", int64(maxAge/time.Second))
}
