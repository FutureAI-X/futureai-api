package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/FutureAI-X/futureai-api/storage"
	"github.com/gin-gonic/gin"
)

// 参考媒体上传参数。
const (
	mediaPolicyTTL  = 30 * time.Minute
	mediaSessionTTL = 40 * time.Minute
	mediaTTL        = 6 * time.Hour
	mediaQuotaBytes = int64(10) << 30 // 10 GiB / 用户

	maxImageBytes = int64(30) << 20
	maxAudioBytes = int64(15) << 20
	maxVideoBytes = int64(200) << 20

	mediaMinDimension = 300
	mediaMaxDimension = 5500
	mediaMinAspect    = 0.4
	mediaMaxAspect    = 2.5

	mediaMinDurationMS = int64(2) * 1000
	mediaMaxDurationMS = int64(30) * 1000

	// 23.976 是常见电影帧率，按 23.9 作为下界兼容它。
	mediaMinFPS = 23.9
	mediaMaxFPS = 60.0

	mediaStagingPrefix = "staging/"
	referenceKeyPrefix = "reference/"
)

// mediaSpec 一种媒体类型下的格式规格。
type mediaSpec struct {
	MediaType   string
	ContentType string
	Ext         string
	MaxBytes    int64
}

// mediaSpecs 允许的 content_type 白名单。
// 只认服务端映射，不接受客户端自定义扩展名。
var mediaSpecs = map[string]map[string]mediaSpec{
	model.FileMediaTypeImage: {
		"image/jpeg": {model.FileMediaTypeImage, "image/jpeg", "jpg", maxImageBytes},
		"image/png":  {model.FileMediaTypeImage, "image/png", "png", maxImageBytes},
		"image/webp": {model.FileMediaTypeImage, "image/webp", "webp", maxImageBytes},
		"image/heic": {model.FileMediaTypeImage, "image/heic", "heic", maxImageBytes},
		"image/heif": {model.FileMediaTypeImage, "image/heif", "heif", maxImageBytes},
	},
	model.FileMediaTypeAudio: {
		"audio/wav":  {model.FileMediaTypeAudio, "audio/wav", "wav", maxAudioBytes},
		"audio/mpeg": {model.FileMediaTypeAudio, "audio/mpeg", "mp3", maxAudioBytes},
	},
	model.FileMediaTypeVideo: {
		"video/mp4":       {model.FileMediaTypeVideo, "video/mp4", "mp4", maxVideoBytes},
		"video/quicktime": {model.FileMediaTypeVideo, "video/quicktime", "mov", maxVideoBytes},
	},
}

// fileUploadPrepareRequest POST /v1/files/upload/prepare
type fileUploadPrepareRequest struct {
	MediaType   string `json:"media_type" binding:"required"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type" binding:"required"`
	Size        int64  `json:"size" binding:"required"`
}

// fileUploadCompleteRequest POST /v1/files/upload/complete
type fileUploadCompleteRequest struct {
	UploadID string `json:"upload_id" binding:"required"`
}

// FileUploadPrepare 申请参考媒体的直传凭证。
//
// 流程：校验媒体类型/格式/大小 → 检查配额 → 生成 staging key 与 POST Object
// 凭证 → 在 files 表插入一条 private/pending 记录。客户端拿到 url + fields 后
// 直接用 multipart/form-data 上传到 COS，再调用 complete。
func FileUploadPrepare(c *gin.Context) {
	var req fileUploadPrepareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if common.IsBodyTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "fail", "message": "请求体过大"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "请求体格式错误"})
		return
	}

	userID := c.GetInt("user_id")
	if userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"message": "无效的 API Key", "type": "authentication_error"},
		})
		return
	}

	mediaType := strings.ToLower(strings.TrimSpace(req.MediaType))
	contentType, spec, err := resolveMediaSpec(mediaType, req.ContentType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": err.Error()})
		return
	}
	if req.Size <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "文件大小必须大于 0"})
		return
	}
	if req.Size > spec.MaxBytes {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": fmt.Sprintf("文件大小超过上限（%d MB）", spec.MaxBytes>>20)})
		return
	}

	now := time.Now()
	used, err := model.SumUserMediaBytes(userID, now)
	if err != nil {
		common.SysErrorf("[FileUpload] 统计媒体配额失败: userID=%d, err=%v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "服务暂时不可用，请稍后再试"})
		return
	}
	if used+req.Size > mediaQuotaBytes {
		c.JSON(http.StatusInsufficientStorage, gin.H{"code": "fail", "message": "媒体存储配额已满"})
		return
	}

	store, err := storage.MediaFromEnv()
	if err != nil {
		common.SysErrorf("[FileUpload] 对象存储未配置: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "文件上传服务暂不可用"})
		return
	}

	fileID := model.GenerateFileIDWithExt(spec.Ext)
	stagingKey := mediaStagingPrefix + fileID

	uploadURL, fields, policyExpires, err := store.PresignPost(
		context.Background(), stagingKey, contentType, spec.MaxBytes, mediaPolicyTTL,
	)
	if err != nil {
		common.SysErrorf("[FileUpload] 生成上传凭证失败: userID=%d, fileID=%s, err=%v", userID, fileID, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "文件上传服务暂不可用"})
		return
	}

	filename := sanitizeFilename(req.Filename)
	if strings.TrimSpace(req.Filename) == "" {
		filename = "upload." + spec.Ext
	}
	sessionExpires := now.Add(mediaSessionTTL)

	file := &model.File{
		FileID:         fileID,
		ServiceType:    store.ServiceType(),
		ServiceAddress: store.ServiceAddress(),
		FileKey:        stagingKey,
		URL:            store.ObjectURL(stagingKey),
		TTLSeconds:     int(mediaSessionTTL / time.Second),
		CreatedAt:      now,
		ExpiresAt:      &sessionExpires,
		UserID:         userID,
		Visibility:     model.FileVisibilityPrivate,
		Status:         model.FileStatusPending,
		MediaType:      mediaType,
		Purpose:        model.FilePurposeReference,
		ContentType:    contentType,
		OriginalName:   filename,
	}
	if err := model.CreateFile(file); err != nil {
		common.SysErrorf("[FileUpload] 创建上传记录失败: userID=%d, fileID=%s, err=%v", userID, fileID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "上传任务创建失败"})
		return
	}

	common.SysLogf("[FileUpload] 已创建上传会话: userID=%d, fileID=%s, mediaType=%s, contentType=%s",
		userID, fileID, mediaType, contentType)

	c.JSON(http.StatusOK, gin.H{
		"upload_id":          fileID,
		"file_id":            fileID,
		"url":                uploadURL,
		"fields":             fields,
		"expires_at":         policyExpires,
		"session_expires_at": sessionExpires,
	})
}

// FileUploadComplete 校验直传对象并登记为可用的参考媒体。
func FileUploadComplete(c *gin.Context) {
	var req fileUploadCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if common.IsBodyTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "fail", "message": "请求体过大"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "请求体格式错误"})
		return
	}

	userID := c.GetInt("user_id")
	if userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"message": "无效的 API Key", "type": "authentication_error"},
		})
		return
	}

	uploadID := strings.TrimSpace(req.UploadID)
	if uploadID == "" || len(uploadID) > model.MaxFileIDLen {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "无效的 upload_id"})
		return
	}

	file, err := model.GetFileByFileID(uploadID)
	if err != nil || file.UserID != userID || file.Visibility != model.FileVisibilityPrivate {
		c.JSON(http.StatusNotFound, gin.H{"code": "fail", "message": "上传任务不存在"})
		return
	}

	// 幂等：已经完成的上传直接返回元数据。
	if file.Status == model.FileStatusReady {
		if file.ExpiresAt != nil && !file.ExpiresAt.After(time.Now()) {
			c.JSON(http.StatusGone, gin.H{"code": "fail", "message": "文件已过期"})
			return
		}
		c.JSON(http.StatusOK, fileUploadResponse(file))
		return
	}
	if file.Status == model.FileStatusFailed {
		msg := file.ErrorMessage
		if msg == "" {
			msg = "上传校验失败"
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": msg})
		return
	}
	if file.Status != model.FileStatusPending {
		c.JSON(http.StatusConflict, gin.H{"code": "fail", "message": "上传任务状态异常"})
		return
	}

	now := time.Now()
	if file.ExpiresAt != nil && !file.ExpiresAt.After(now) {
		c.JSON(http.StatusGone, gin.H{"code": "fail", "message": "上传会话已过期，请重新 prepare"})
		return
	}

	spec, ok := mediaSpecs[file.MediaType][file.ContentType]
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "上传记录格式无效"})
		return
	}

	store, err := storage.MediaFrom(file.ServiceType, file.ServiceAddress)
	if err != nil {
		common.SysErrorf("[FileUpload] 对象存储不可用: fileID=%s, err=%v", file.FileID, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "文件上传服务暂不可用"})
		return
	}

	// 校验/复制/探测使用独立超时，避免客户端断开导致任务半途而废。
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stagingKey := file.FileKey
	objMeta, err := store.Head(ctx, stagingKey)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			c.JSON(http.StatusConflict, gin.H{"code": "fail", "message": "文件尚未上传完成，请稍后再试"})
			return
		}
		mediaTransientError(c, "HEAD", err)
		return
	}
	if objMeta.Size <= 0 {
		failMediaUpload(c, store, file, userID, http.StatusBadRequest, "文件为空")
		return
	}
	if objMeta.Size > spec.MaxBytes {
		failMediaUpload(c, store, file, userID, http.StatusBadRequest,
			fmt.Sprintf("文件大小超过上限（%d MB）", spec.MaxBytes>>20))
		return
	}

	headBytes, err := store.GetRange(ctx, stagingKey, 0, 64*1024-1)
	if err != nil {
		mediaTransientError(c, "RangeGet", err)
		return
	}

	detected, err := detectMediaFromBytes(headBytes, file.MediaType)
	if err != nil {
		failMediaUpload(c, store, file, userID, http.StatusBadRequest, err.Error())
		return
	}
	if detected.MediaType != file.MediaType {
		failMediaUpload(c, store, file, userID, http.StatusBadRequest, "文件实际类型与声明的 media_type 不一致")
		return
	}

	// 配额按实际大小严格复核。
	used, err := model.SumUserMediaBytes(userID, time.Now())
	if err != nil {
		common.SysErrorf("[FileUpload] 统计媒体配额失败: userID=%d, err=%v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "服务暂时不可用，请稍后再试"})
		return
	}
	if used+objMeta.Size > mediaQuotaBytes {
		failMediaUpload(c, store, file, userID, http.StatusInsufficientStorage, "媒体存储配额已满")
		return
	}

	updates := map[string]interface{}{
		"file_key":     referenceKeyPrefix + file.MediaType + "/" + file.FileID,
		"url":          "",
		"bytes":        objMeta.Size,
		"content_type": detected.ContentType,
		"media_type":   detected.MediaType,
	}

	switch file.MediaType {
	case model.FileMediaTypeImage:
		probe, err := store.ProbeImageInfo(ctx, stagingKey)
		if err != nil {
			mediaTransientError(c, "imageInfo", err)
			return
		}
		if err := validateImageProbe(probe); err != nil {
			failMediaUpload(c, store, file, userID, http.StatusBadRequest, err.Error())
			return
		}
		updates["duration_ms"] = 0
		updates["width"] = probe.Width
		updates["height"] = probe.Height
		updates["media_meta"] = marshalMediaMeta(&uploadedMediaMeta{Format: probe.Format})

	case model.FileMediaTypeAudio:
		probe, err := store.ProbeAVInfo(ctx, stagingKey)
		if err != nil {
			mediaTransientError(c, "avinfo", err)
			return
		}
		if err := validateAudioProbe(probe); err != nil {
			failMediaUpload(c, store, file, userID, http.StatusBadRequest, err.Error())
			return
		}
		updates["duration_ms"] = probe.DurationMS
		updates["width"] = 0
		updates["height"] = 0
		updates["media_meta"] = marshalMediaMeta(&uploadedMediaMeta{
			Format:      probe.Format,
			HasAudio:    probe.HasAudio,
			AudioCodecs: probe.AudioCodecs,
		})

	case model.FileMediaTypeVideo:
		probe, err := store.ProbeAVInfo(ctx, stagingKey)
		if err != nil {
			mediaTransientError(c, "avinfo", err)
			return
		}
		if err := validateVideoProbe(probe); err != nil {
			failMediaUpload(c, store, file, userID, http.StatusBadRequest, err.Error())
			return
		}
		updates["duration_ms"] = probe.DurationMS
		updates["width"] = probe.Width
		updates["height"] = probe.Height
		updates["media_meta"] = marshalMediaMeta(&uploadedMediaMeta{
			FPS:         probe.FPS,
			VideoCodecs: probe.VideoCodecs,
			AudioCodecs: probe.AudioCodecs,
			Format:      probe.Format,
			HasVideo:    probe.HasVideo,
			HasAudio:    probe.HasAudio,
		})

	default:
		failMediaUpload(c, store, file, userID, http.StatusBadRequest, "不支持的媒体类型")
		return
	}

	finalKey := updates["file_key"].(string)
	if err := store.Copy(ctx, stagingKey, finalKey); err != nil {
		mediaTransientError(c, "Copy", err)
		return
	}

	expiresAt := time.Now().Add(mediaTTL)
	updates["url"] = store.ObjectURL(finalKey)
	updates["ttl_seconds"] = int(mediaTTL / time.Second)
	updates["expires_at"] = expiresAt

	if err := model.UpdateFileReady(file.FileID, userID, updates); err != nil {
		if errors.Is(err, model.ErrFileNotPending) {
			// 并发请求已经完成了状态转换，重新读取后按幂等路径返回。
			if latest, getErr := model.GetFileByFileID(file.FileID); getErr == nil {
				switch latest.Status {
				case model.FileStatusReady:
					_ = store.Delete(context.Background(), stagingKey)
					c.JSON(http.StatusOK, fileUploadResponse(latest))
					return
				case model.FileStatusFailed:
					msg := latest.ErrorMessage
					if msg == "" {
						msg = "上传校验失败"
					}
					c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": msg})
					return
				}
			}
		}
		common.SysErrorf("[FileUpload] 更新文件状态失败: fileID=%s, err=%v", file.FileID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "上传登记失败，请稍后重试"})
		return
	}

	// 落库成功后再删 staging；删除失败只记日志，由清理任务/生命周期规则兜底。
	if err := store.Delete(context.Background(), stagingKey); err != nil {
		common.SysErrorf("[FileUpload] 删除 staging 对象失败: fileID=%s, key=%s, err=%v", file.FileID, stagingKey, err)
	}

	latest, err := model.GetFileByFileID(file.FileID)
	if err != nil {
		common.SysErrorf("[FileUpload] 读取完成记录失败: fileID=%s, err=%v", file.FileID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "上传登记失败，请稍后重试"})
		return
	}

	common.SysLogf("[FileUpload] 参考媒体上传完成: userID=%d, fileID=%s, mediaType=%s, bytes=%d, durationMS=%d",
		userID, latest.FileID, latest.MediaType, latest.Bytes, latest.DurationMS)

	c.JSON(http.StatusOK, fileUploadResponse(latest))
}

func fileUploadResponse(f *model.File) gin.H {
	return gin.H{
		"file_id":      f.FileID,
		"media_type":   f.MediaType,
		"content_type": f.ContentType,
		"bytes":        f.Bytes,
		"duration_ms":  f.DurationMS,
		"width":        f.Width,
		"height":       f.Height,
		"expires_at":   f.ExpiresAt,
	}
}

// failMediaUpload 处理确定性校验失败：删除 staging，标记 failed，返回 400/507。
func failMediaUpload(c *gin.Context, store storage.MediaStore, file *model.File, userID, status int, message string) {
	if store != nil {
		if err := store.Delete(context.Background(), file.FileKey); err != nil {
			common.SysErrorf("[FileUpload] 清理 staging 失败: fileID=%s, key=%s, err=%v", file.FileID, file.FileKey, err)
		}
	}
	if err := model.UpdateFileFailed(file.FileID, userID, message); err != nil {
		common.SysErrorf("[FileUpload] 标记上传失败状态失败: fileID=%s, err=%v", file.FileID, err)
	}
	c.JSON(status, gin.H{"code": "fail", "message": message})
}

// mediaTransientError 处理暂时性失败：不删除 staging、不标记 failed，客户端可重试 complete。
func mediaTransientError(c *gin.Context, operation string, err error) {
	common.SysErrorf("[FileUpload] %s 暂时失败: %v", operation, err)
	c.JSON(http.StatusBadGateway, gin.H{"code": "fail", "message": "媒体校验暂时不可用，请稍后重试"})
}

// resolveMediaSpec 归一化并校验 content_type 是否属于给定 media_type。
func resolveMediaSpec(mediaType, rawContentType string) (string, mediaSpec, error) {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	specs, ok := mediaSpecs[mediaType]
	if !ok {
		return "", mediaSpec{}, fmt.Errorf("media_type 必须是 image/video/audio")
	}

	mt, _, err := mime.ParseMediaType(strings.TrimSpace(rawContentType))
	if err != nil {
		return "", mediaSpec{}, fmt.Errorf("content_type 无效")
	}
	mt = strings.ToLower(strings.TrimSpace(mt))
	switch mt {
	case "image/jpg":
		mt = "image/jpeg"
	case "audio/x-wav", "audio/wave":
		mt = "audio/wav"
	case "audio/mp3":
		mt = "audio/mpeg"
	case "video/mov":
		mt = "video/quicktime"
	}

	spec, ok := specs[mt]
	if !ok {
		return "", mediaSpec{}, fmt.Errorf("content_type %q 与 media_type %q 不匹配，或格式不支持", rawContentType, mediaType)
	}
	return mt, spec, nil
}

// detectedMedia 根据文件头识别出的实际媒体信息。
type detectedMedia struct {
	MediaType   string
	ContentType string
	Ext         string
}

var pngSignature = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

// detectMediaFromBytes 根据文件头识别媒体类型，并与声明类型做交叉校验的基础。
func detectMediaFromBytes(data []byte, declared string) (*detectedMedia, error) {
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return &detectedMedia{model.FileMediaTypeImage, "image/jpeg", "jpg"}, nil
	}
	if bytes.HasPrefix(data, pngSignature) {
		return &detectedMedia{model.FileMediaTypeImage, "image/png", "png"}, nil
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return &detectedMedia{model.FileMediaTypeImage, "image/webp", "webp"}, nil
	}
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		brand := string(data[8:12])
		switch brand {
		case "heic", "heix":
			return &detectedMedia{model.FileMediaTypeImage, "image/heic", "heic"}, nil
		case "hevc", "hevx", "heim", "heis", "hevm", "hevs", "mif1", "msf1", "heif":
			return &detectedMedia{model.FileMediaTypeImage, "image/heif", "heif"}, nil
		case "avif", "avis":
			return nil, errors.New("不支持 AVIF 图片")
		case "M4A ", "M4B ", "M4P ":
			return nil, errors.New("不支持独立 M4A/AAC 音频，请使用 WAV 或 MP3")
		case "qt  ":
			return &detectedMedia{model.FileMediaTypeVideo, "video/quicktime", "mov"}, nil
		default:
			return &detectedMedia{model.FileMediaTypeVideo, "video/mp4", "mp4"}, nil
		}
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		return &detectedMedia{model.FileMediaTypeAudio, "audio/wav", "wav"}, nil
	}
	if len(data) >= 3 && string(data[:3]) == "ID3" {
		return &detectedMedia{model.FileMediaTypeAudio, "audio/mpeg", "mp3"}, nil
	}
	if len(data) >= 2 && data[0] == 0xFF && (data[1]&0xE0) == 0xE0 {
		version := (data[1] >> 3) & 0x03
		layer := (data[1] >> 1) & 0x03
		// MPEG 音频帧：version 不能是保留值 01，layer 不能是保留值 00。
		// layer=00 主要用于排除 AAC ADTS，避免把 AAC 误判成 MP3。
		if version != 1 && layer != 0 {
			return &detectedMedia{model.FileMediaTypeAudio, "audio/mpeg", "mp3"}, nil
		}
	}
	if len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a") {
		return nil, errors.New("不支持 GIF 图片")
	}

	if declared == model.FileMediaTypeImage {
		return nil, errors.New("无法识别的图片格式")
	}
	return nil, errors.New("无法识别的媒体格式")
}

func validateImageProbe(p *storage.ImageProbe) error {
	if p == nil || p.Width <= 0 || p.Height <= 0 {
		return errors.New("无法解析图片宽高")
	}
	// 图片不限制宽高与宽高比：不同上游模型支持的尺寸范围差异很大，
	// 平台侧统一卡范围会把上游本来能处理的图片挡在上传阶段。
	// 宽高仍会被记录到 files 表，供排查与展示使用；视频的限制见 validateVideoProbe。
	switch p.Format {
	case "", "jpeg", "png", "webp", "heic", "heif":
		return nil
	default:
		return fmt.Errorf("不支持的图片格式: %s", p.Format)
	}
}

func validateAudioProbe(p *storage.MediaProbe) error {
	if p == nil || !p.HasAudio {
		return errors.New("未检测到音频流")
	}
	if p.DurationMS < mediaMinDurationMS || p.DurationMS > mediaMaxDurationMS {
		return fmt.Errorf("音频时长需在 2-30 秒，当前 %.2f 秒", float64(p.DurationMS)/1000)
	}
	format := strings.ToLower(strings.TrimSpace(p.Format))
	if format != "" && !strings.Contains(format, "wav") && !strings.Contains(format, "mp3") {
		return fmt.Errorf("不支持的音频格式: %s", p.Format)
	}
	return nil
}

func validateVideoProbe(p *storage.MediaProbe) error {
	if p == nil || !p.HasVideo {
		return errors.New("未检测到视频流")
	}
	if p.Width < mediaMinDimension || p.Width > mediaMaxDimension ||
		p.Height < mediaMinDimension || p.Height > mediaMaxDimension {
		return fmt.Errorf("视频宽高需在 %d-%d 像素", mediaMinDimension, mediaMaxDimension)
	}
	aspect := float64(p.Width) / float64(p.Height)
	if aspect < mediaMinAspect || aspect > mediaMaxAspect {
		return fmt.Errorf("视频宽高比需在 %.1f-%.1f", mediaMinAspect, mediaMaxAspect)
	}
	if p.DurationMS < mediaMinDurationMS || p.DurationMS > mediaMaxDurationMS {
		return fmt.Errorf("视频时长需在 2-30 秒，当前 %.2f 秒", float64(p.DurationMS)/1000)
	}
	if p.FPS < mediaMinFPS || p.FPS > mediaMaxFPS {
		return fmt.Errorf("视频帧率需在 %.1f-%.0f 帧/秒，当前 %.2f", mediaMinFPS, mediaMaxFPS, p.FPS)
	}

	if len(p.VideoCodecs) == 0 {
		return errors.New("无法解析视频编码")
	}
	for _, codec := range p.VideoCodecs {
		switch strings.ToLower(strings.TrimSpace(codec)) {
		case "h264", "avc", "avc1", "hevc", "h265", "hev1", "hvc1":
		default:
			return fmt.Errorf("视频编码不支持: %s，仅支持 H.264/H.265", codec)
		}
	}
	for _, codec := range p.AudioCodecs {
		switch strings.ToLower(strings.TrimSpace(codec)) {
		case "aac", "mp3":
		default:
			return fmt.Errorf("音频编码不支持: %s，仅支持 AAC/MP3", codec)
		}
	}

	format := strings.ToLower(strings.TrimSpace(p.Format))
	if format != "" && !strings.Contains(format, "mp4") && !strings.Contains(format, "mov") && !strings.Contains(format, "quicktime") {
		return fmt.Errorf("不支持的视频容器格式: %s，仅支持 MP4/MOV", p.Format)
	}
	return nil
}

type uploadedMediaMeta struct {
	FPS         float64  `json:"fps,omitempty"`
	VideoCodecs []string `json:"video_codecs,omitempty"`
	AudioCodecs []string `json:"audio_codecs,omitempty"`
	Format      string   `json:"format,omitempty"`
	HasVideo    bool     `json:"has_video,omitempty"`
	HasAudio    bool     `json:"has_audio,omitempty"`
}

func marshalMediaMeta(meta *uploadedMediaMeta) string {
	data, err := json.Marshal(meta)
	if err != nil {
		return ""
	}
	return string(data)
}
