package controller

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/FutureAI/token-hub/common"
	"github.com/FutureAI/token-hub/model"
	"github.com/FutureAI/token-hub/supplier"
	"github.com/gin-gonic/gin"
)

// uploadTimeout 上传调用超时
const uploadTimeout = 30 * time.Second

// uploadCreditsEnv 图片上传的单次积分成本（环境变量名）
const uploadCreditsEnv = "UPLOAD_CREDITS"

// uploadCredits 读取上传计费配置，默认 0（免费）。
//
// 默认留 0 是保持既有行为；但必须存在这个开关：上传端点是拿平台自己的
// 供应商密钥把用户文件转存到上游的，平台承担全部存储与带宽成本。
// 不开计费又不设配额时，任何持有效 API Key 的用户（哪怕余额为 0）
// 都能以每分钟 60 次的速度持续消耗，账单全记在平台头上。
//
// 配置非法时按 0 处理而不是拒绝启动：这个值只影响定价，
// 不该因为一次笔误让整个服务起不来。但会打错误日志，不会静默。
func uploadCredits() float64 {
	raw := strings.TrimSpace(os.Getenv(uploadCreditsEnv))
	if raw == "" {
		return 0
	}

	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		common.SysErrorf("[UploadImage] %s 配置无效(%q)，本次按 0（免费）处理", uploadCreditsEnv, raw)
		return 0
	}
	return model.RoundCredits(v)
}

// maxFilenameLen 文件名最大长度
const maxFilenameLen = 100

// UploadImage 图片上传端点
// POST /v1/uploads/images
// 仅一个参数：file；返回 url(来自第三方)/filename/content_type/bytes(程序解析)
func UploadImage(c *gin.Context) {
	// 1. 请求体大小已由 middleware.BodyLimit 在进入本函数前限制住
	//（multipart 端点按 文件上限 + 边框余量 放宽，其余端点 1MB）。
	// 顺序仍然关键：c.FormFile 会触发 multipart 解析，超过内存阈值的部分
	// 会被写入临时文件，因此限制必须发生在**解析之前**——中间件在 handler
	// 之前执行，正好满足这一点。
	fileHeader, err := c.FormFile("file")
	if err != nil {
		if common.IsBodyTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "fail", "message": "文件大小超过 10MB 限制"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "缺少 file 参数"})
		return
	}

	// 2. 大小限制（体积上限之外的兜底：Content-Length 与实际文件大小可能不一致）
	if fileHeader.Size > common.MaxUploadFileSize {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "文件大小超过 10MB 限制"})
		return
	}

	// 3. 读取文件内容
	f, err := fileHeader.Open()
	if err != nil {
		common.SysErrorf("[UploadImage] 打开文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "文件读取失败"})
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		common.SysErrorf("[UploadImage] 读取文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "文件读取失败"})
		return
	}

	// 4. 识别图片类型（仅支持 JPEG/PNG/WebP/GIF）
	contentType := detectImageType(data)
	if contentType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "仅支持 JPEG/PNG/WebP/GIF 图片"})
		return
	}

	// 4.5 清洗文件名并强制扩展名与真实类型一致。
	// 文件名会被转发给上游供应商，且可能被其用于存储路径与内容分发，
	// 必须防住 CRLF 头注入、目录穿越，以及用 .html 等扩展名做内容伪装。
	safeFilename := enforceImageExtension(sanitizeFilename(fileHeader.Filename), contentType)

	// 5. 根据供应商名称（忽略大小写）获取 APIMart
	vendor, err := model.GetEnabledVendorByNameInsensitive("APIMart")
	if err != nil {
		common.SysErrorf("[UploadImage] 未找到启用的 APIMart 供应商: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "图片上传服务暂不可用"})
		return
	}

	// 6. 解密供应商 API Key
	apiKey, err := common.DecryptSecret(vendor.APIKey)
	if err != nil {
		common.SysErrorf("[UploadImage] 供应商密钥解密失败: vendor=%s, err=%v", vendor.Name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "服务暂时不可用，请稍后再试"})
		return
	}

	// 7. 调用上传服务
	cfg := supplier.Config{
		BaseURL: vendor.BaseURL,
		APIKey:  apiKey,
	}
	uploader := supplier.NewUploader(vendor.Name, cfg)
	if uploader == nil {
		common.SysErrorf("[UploadImage] 不支持的图片上传服务: %s", vendor.Name)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "图片上传服务暂不可用"})
		return
	}

	// 7.5 计费。顺序与图像生成一致：先扣费、后调用上游。
	// 反过来的话，零余额用户可以先把文件传上去、再由扣费失败收场，
	// 而平台已经为这次上传付过上游成本。
	userID := c.GetInt("user_id")
	if userID <= 0 {
		common.SysErrorf("[UploadImage] 缺少有效用户身份")
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"message": "无效的 API Key", "type": "authentication_error"},
		})
		return
	}

	credits := uploadCredits()
	ref := ""
	if credits > 0 {
		ref = model.GenerateUploadRef()
		if err := model.DeductCreditsFor(userID, ref, credits, "图片上传"); err != nil {
			if errors.Is(err, model.ErrInsufficientCredits) {
				common.SysErrorf("[UploadImage] 积分不足: userID=%d, amount=%.6f", userID, credits)
				c.JSON(http.StatusPaymentRequired, gin.H{"code": "fail", "message": "积分不足"})
			} else {
				common.SysErrorf("[UploadImage] 扣费失败: userID=%d, err=%v", userID, err)
				c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "服务暂时不可用，请稍后再试"})
			}
			return
		}
	}

	// 上传用独立带超时的 context，理由同提交任务：客户端断开就取消，
	// 会让我们既不知道上游是否收下文件、又已经把积分扣掉。
	ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
	defer cancel()
	result, err := uploader.UploadImage(ctx, safeFilename, contentType, data)
	if err != nil {
		common.SysErrorf("[UploadImage] 上传失败: vendor=%s, err=%v", vendor.Name, err)
		// 明确失败（拿到响应且非 200、或请求根本没发出去）才退款；
		// 这里无法区分「超时但上游已收下」，与图像生成的取舍一致：
		// 用户没拿到 URL，收钱不发货比平台承担损失更糟。
		if credits > 0 {
			if refundErr := model.RefundCreditsFor(userID, ref, credits, "图片上传失败退还"); refundErr != nil {
				common.SysErrorf("[UploadImage] 退还积分失败: userID=%d, ref=%s, err=%v", userID, ref, refundErr)
			}
		}
		c.JSON(http.StatusBadGateway, gin.H{"code": "fail", "message": "上传失败，请稍后再试"})
		return
	}

	// 8. url 来自第三方，filename/content_type/bytes 由程序解析
	c.JSON(http.StatusOK, gin.H{
		"url":          result.URL,
		"filename":     safeFilename,
		"content_type": contentType,
		"bytes":        int64(len(data)),
	})
}

// sanitizeFilename 清洗上传文件名，仅保留安全字符。
// 防住三类问题：路径分隔符（目录穿越）、控制字符与 CRLF（multipart 头注入）、
// 超长名字（上游存储截断/异常）。
func sanitizeFilename(name string) string {
	// 统一分隔符后只取最后一段
	name = strings.ReplaceAll(name, "\\", "/")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}

	cleaned := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7F: // 控制字符，含 CR / LF / TAB
			continue
		case strings.ContainsRune(`"'<>\:|?*`, r):
			continue
		default:
			cleaned = append(cleaned, r)
		}
	}

	// 去掉前导点，避免产生 "." / ".." / 隐藏文件
	result := strings.TrimLeft(strings.TrimSpace(string(cleaned)), ".")
	if result == "" {
		result = "image"
	}
	if runes := []rune(result); len(runes) > maxFilenameLen {
		result = string(runes[:maxFilenameLen])
	}
	return result
}

// enforceImageExtension 用探测到的真实 MIME 类型强制扩展名。
// 防止用户把图片命名为 .html/.svg 等内容类型，借助上游 CDN 的按扩展名
// 分发策略制造存储型 XSS。
func enforceImageExtension(name, contentType string) string {
	ext, ok := map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
		"image/gif":  ".gif",
	}[contentType]
	if !ok {
		return name
	}
	if idx := strings.LastIndex(name, "."); idx > 0 {
		name = name[:idx]
	}
	return name + ext
}

// detectImageType 通过文件头（magic bytes）识别图片 MIME 类型，不支持则返回空
func detectImageType(data []byte) string {
	if len(data) < 12 {
		return ""
	}
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		// JPEG
		return "image/jpeg"
	case len(data) >= 8 && bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		// PNG
		return "image/png"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		// WebP
		return "image/webp"
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		// GIF
		return "image/gif"
	default:
		return ""
	}
}
