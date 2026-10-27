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

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/filestore"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/FutureAI-X/futureai-api/storage"
	"github.com/FutureAI-X/futureai-api/supplier"
	"github.com/gin-gonic/gin"
)

// uploadTimeout 上传调用超时
const uploadTimeout = 30 * time.Second

// uploadCreditsEnv 图片上传的单次积分成本（环境变量名）
const uploadCreditsEnv = "UPLOAD_CREDITS"

// uploadBackendEnv 图片上传后端（cos / apimart）
const uploadBackendEnv = "IMAGE_UPLOAD_BACKEND"

const (
	// uploadBackendCOS 转存到本服务配置的对象存储，读取走公开读取接口
	uploadBackendCOS = "cos"
	// uploadBackendAPIMart 转存到 APIMart，保留原有实现便于切换
	uploadBackendAPIMart = "apimart"
)

// uploadBackend 读取上传后端配置，默认 cos。
//
// 非法值按 cos 处理并打错误日志：上传是核心能力，不因一次配置笔误拒绝服务；
// 但必须留下痕迹，否则调用方会以为切到了 APIMart。
func uploadBackend() string {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(uploadBackendEnv)))
	switch raw {
	case "", uploadBackendCOS:
		return uploadBackendCOS
	case uploadBackendAPIMart:
		return uploadBackendAPIMart
	default:
		common.SysErrorf("[UploadImage] %s 配置无效(%q)，按 %s 处理", uploadBackendEnv, raw, uploadBackendCOS)
		return uploadBackendCOS
	}
}

// persistUploadedImage 把上传内容写入对象存储并登记 files 表。
// 抽成变量是为了测试能替换掉依赖数据库与对象存储的实现。
var persistUploadedImage = func(ctx context.Context, data []byte) (*model.File, error) {
	return filestore.Default().PersistBytes(ctx, data)
}

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
// 仅一个参数：file；返回 url/filename/content_type/bytes。
// url 默认是本服务的公开读取地址（IMAGE_UPLOAD_BACKEND=cos），
// 切到 apimart 时是第三方托管地址。
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

	// 5. 选择上传后端。默认存到自己的对象存储；出问题时把
	// IMAGE_UPLOAD_BACKEND 改成 apimart 即可切回第三方。
	backend := uploadBackend()

	// APIMart 的依赖必须在扣费之前准备好：配置或密钥有问题时不应产生一次
	// 扣费与退款，也不应该先扣钱再发现无服务可用。
	var uploader supplier.Uploader
	var vendorName string
	if backend == uploadBackendAPIMart {
		vendor, err := model.GetEnabledVendorByNameInsensitive("APIMart")
		if err != nil {
			common.SysErrorf("[UploadImage] 未找到启用的 APIMart 供应商: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "图片上传服务暂不可用"})
			return
		}

		apiKey, err := common.DecryptSecret(vendor.APIKey)
		if err != nil {
			common.SysErrorf("[UploadImage] 供应商密钥解密失败: vendor=%s, err=%v", vendor.Name, err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "服务暂时不可用，请稍后再试"})
			return
		}

		uploader = supplier.NewUploader(vendor.Name, supplier.Config{
			BaseURL: vendor.BaseURL,
			APIKey:  apiKey,
		})
		if uploader == nil {
			common.SysErrorf("[UploadImage] 不支持的图片上传服务: %s", vendor.Name)
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "图片上传服务暂不可用"})
			return
		}
		vendorName = vendor.Name
	}

	// COS 后端必须能拼出对外地址，否则传上去也没法给调用方用。
	// 检查放在扣费之前：配置错误不应该产生一次计费与退款。
	if backend == uploadBackendCOS && configuredPublicBaseURL() == "" {
		common.SysErrorf("[UploadImage] %s 未配置，无法生成公开文件地址", publicBaseURLEnv)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "图片上传服务暂不可用"})
		return
	}

	// 6. 计费。顺序与图像生成一致：先扣费、后上传。
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

	// 7. 上传。用独立带超时的 context，理由同提交任务：客户端断开就取消，
	// 会让我们既不知道上游是否收下文件、又已经把积分扣掉。
	ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
	defer cancel()

	var resultURL string
	if backend == uploadBackendCOS {
		file, err := persistUploadedImage(ctx, data)
		if err != nil {
			common.SysErrorf("[UploadImage] 转存到对象存储失败: %v", err)
			refundUploadCredits(userID, ref, credits)
			if errors.Is(err, storage.ErrNotConfigured) {
				c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "图片上传服务暂不可用"})
			} else {
				c.JSON(http.StatusBadGateway, gin.H{"code": "fail", "message": "上传失败，请稍后再试"})
			}
			return
		}

		resultURL = publicFileURL(file.FileID)
		if resultURL == "" {
			// 请求开始时已检查过；走到这里说明配置在请求期间被改掉，按失败处理。
			common.SysErrorf("[UploadImage] 生成公开文件地址失败: file_id=%s", file.FileID)
			refundUploadCredits(userID, ref, credits)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "服务暂时不可用，请稍后再试"})
			return
		}
	} else {
		result, err := uploader.UploadImage(ctx, safeFilename, contentType, data)
		if err != nil {
			common.SysErrorf("[UploadImage] 上传失败: vendor=%s, err=%v", vendorName, err)
			// 明确失败（拿到响应且非 200、或请求根本没发出去）才退款；
			// 这里无法区分「超时但上游已收下」，与图像生成的取舍一致：
			// 用户没拿到 URL，收钱不发货比平台承担损失更糟。
			refundUploadCredits(userID, ref, credits)
			c.JSON(http.StatusBadGateway, gin.H{"code": "fail", "message": "上传失败，请稍后再试"})
			return
		}
		resultURL = result.URL
	}

	// 8. url 取决于后端：cos 是本服务的公开读取地址，apimart 是第三方托管地址。
	//    filename/content_type/bytes 由程序解析。
	c.JSON(http.StatusOK, gin.H{
		"url":          resultURL,
		"filename":     safeFilename,
		"content_type": contentType,
		"bytes":        int64(len(data)),
	})
}

// refundUploadCredits 退还本次上传的积分，失败只记日志。
func refundUploadCredits(userID int, ref string, credits float64) {
	if credits <= 0 {
		return
	}
	if err := model.RefundCreditsFor(userID, ref, credits, "图片上传失败退还"); err != nil {
		common.SysErrorf("[UploadImage] 退还积分失败: userID=%d, ref=%s, err=%v", userID, ref, err)
	}
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
