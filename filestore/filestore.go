// Package filestore 把文件转存到对象存储，并登记到 files 表。
//
// 支持两种来源：从外部 URL 下载（PersistURL），以及直接在内存中的内容
// （PersistBytes，供图片上传接口使用）。对象存储的差异由 storage.Store 抽象，
// 本包只负责校验、生成不可猜的文件 ID、计算有效期与写库。
package filestore

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/FutureAI-X/futureai-api/storage"
)

const (
	// DefaultTTL 文件默认有效期
	DefaultTTL = 24 * time.Hour

	// ttlEnv 文件有效期（秒）
	ttlEnv = "FILE_TTL_SECONDS"

	// defaultMaxBytes 单个图片文件下载上限。
	// 成图通常远小于这个值，上限只用于防住被控上游用无限流打爆内存。
	defaultMaxBytes int64 = 32 << 20

	// defaultVideoMaxBytes 单个视频文件下载上限。
	// 该值只防住异常大文件；正常 1080p、30 秒的成片远小于它。
	defaultVideoMaxBytes int64 = 500 << 20

	// defaultTimeout 单次图片转存（下载 + 上传）的总超时
	defaultTimeout = 90 * time.Second

	// defaultVideoTimeout 单次视频转存的总超时。
	// 视频体积大、跨境链路可能很慢，90 秒经常不够。
	defaultVideoTimeout = 10 * time.Minute

	// maxConcurrentVideoPersist 同时进行的视频转存数默认值。
	// 视频会整段读进内存，并发过高时大文件可能同时占用数 GB；
	// 可用 FILE_VIDEO_PERSIST_CONCURRENCY 调整。
	maxConcurrentVideoPersist = 2

	// fileIDRandomBytes 随机部分的字节数（12 字节 = 24 位十六进制 = 96 位熵）
	fileIDRandomBytes = 12

	// maxInsertAttempts 落库遇到 file_id 冲突时的最大尝试次数
	maxInsertAttempts = 3

	// orphanCleanupTimeout 落库失败后删除已上传对象的超时
	orphanCleanupTimeout = 10 * time.Second
)

// ErrSourceUnavailable 表示上游结果资产本身不可用：
// URL 非法、下载失败、HTTP 404、或下载到的内容不是受支持的图片格式。
//
// 与「上传到对象存储失败」刻意区分：前者说明上游没有交付可用的成图，
// 调用方可以据此判定任务失败并退款；后者是我们自己的存储侧故障，
// 不应把一次已经成功的生成判成失败。
var ErrSourceUnavailable = errors.New("结果资产不可用")

// ErrPersistBusy 表示视频转存并发已满。
// 这不是结果资产不可用：调用方应稍后重试，而不是把任务判为失败。
var ErrPersistBusy = errors.New("视频转存并发已满，稍后重试")

// videoPersistSem 限制同时进行的视频转存；容量在首次使用时按环境变量初始化。
var (
	videoPersistSemOnce sync.Once
	videoPersistSem     chan struct{}
)

// acquireVideoPersistSlot 尝试占用一个视频转存位。
// 并发满时返回 false，调用方应稍后重试；不阻塞轮询 goroutine。
func acquireVideoPersistSlot() (func(), bool) {
	videoPersistSemOnce.Do(func() {
		n := common.GetEnvOrDefault("FILE_VIDEO_PERSIST_CONCURRENCY", maxConcurrentVideoPersist)
		if n <= 0 {
			n = maxConcurrentVideoPersist
		}
		videoPersistSem = make(chan struct{}, n)
	})

	select {
	case videoPersistSem <- struct{}{}:
		return func() { <-videoPersistSem }, true
	default:
		return nil, false
	}
}

// Config 转存配置
type Config struct {
	ServiceType   string
	TTL           time.Duration
	MaxBytes      int64
	Timeout       time.Duration
	VideoMaxBytes int64
	VideoTimeout  time.Duration
}

// LoadConfig 从环境变量读取配置。
// 配置非法时按默认值处理并打错误日志，不阻止服务启动：
// 成图转存是尽力而为的附加能力，不该因为一次笔误让整个服务起不来。
func LoadConfig() Config {
	cfg := Config{
		ServiceType:   storage.ServiceTencentCOS,
		TTL:           DefaultTTL,
		MaxBytes:      defaultMaxBytes,
		Timeout:       defaultTimeout,
		VideoMaxBytes: defaultVideoMaxBytes,
		VideoTimeout:  defaultVideoTimeout,
	}

	raw := strings.TrimSpace(os.Getenv(ttlEnv))
	if raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds <= 0 {
			common.SysErrorf("[FileStore] %s 配置无效(%q)，按默认 %d 秒处理",
				ttlEnv, raw, int64(DefaultTTL/time.Second))
		} else {
			cfg.TTL = time.Duration(seconds) * time.Second
		}
	}

	// 视频转存的上限与超时允许运维调整：不同上游的码率与跨境链路差异很大。
	if mb := common.GetEnvOrDefault("FILE_VIDEO_MAX_MB", 0); mb > 0 {
		cfg.VideoMaxBytes = int64(mb) << 20
	}
	if seconds := common.GetEnvOrDefault("FILE_VIDEO_TIMEOUT_SECONDS", 0); seconds > 0 {
		cfg.VideoTimeout = time.Duration(seconds) * time.Second
	}
	return cfg
}

// Persister 转存执行器。
// 字段不导出，测试可在同包内替换依赖。
type Persister struct {
	cfg        Config
	newStore   func(serviceType, serviceAddress string) (storage.Store, error)
	createFile func(*model.File) error
	fileExists func(fileID string) (bool, error)
	httpClient *http.Client
	now        func() time.Time
	newFileID  func(ext string) (string, error)
}

// NewPersister 创建转存执行器
func NewPersister(cfg Config) *Persister {
	return &Persister{
		cfg:        cfg,
		newStore:   storage.FromEnv,
		createFile: model.CreateFile,
		fileExists: model.FileIDExists,
		httpClient: common.FileDownloadHTTPClient(),
		now:        time.Now,
		newFileID:  generateFileID,
	}
}

var (
	defaultOnce      sync.Once
	defaultPersister *Persister
)

// Default 返回进程级默认执行器（配置在首次使用时读取）
func Default() *Persister {
	defaultOnce.Do(func() {
		cfg := LoadConfig()
		defaultPersister = NewPersister(cfg)

		// 提前探测一次配置，让「忘了配 COS」在第一次转存时就有明确日志，
		// 而不是等到下载完、要上传时才发现（那样会白白消耗一次下载）。
		if _, err := storage.FromEnv(cfg.ServiceType, ""); err != nil {
			common.SysErrorf("[FileStore] 对象存储未配置，成图转存将跳过: %v", err)
		}
	})
	return defaultPersister
}

// PersistURL 下载 rawURL 并转存到对象存储，返回写入 files 表的记录。
//
// 任一步失败都返回错误，由调用方决定是否降级（当前调用方选择尽力而为）。
// 该入口不带用户归属，生成结果应使用 PersistURLForUser。
func (p *Persister) PersistURL(ctx context.Context, rawURL string) (*model.File, error) {
	return p.PersistURLForUser(ctx, rawURL, 0)
}

// PersistURLForUser 与 PersistURL 相同，但把结果登记到指定用户名下，
// 使生成结果可以作为后续视频生成的参考输入复用。
func (p *Persister) PersistURLForUser(ctx context.Context, rawURL string, userID int) (*model.File, error) {
	target, err := parseSourceURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceUnavailable, err)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()

	data, err := p.download(ctx, target, p.cfg.MaxBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceUnavailable, err)
	}

	ext, contentType, ok := detectImageExt(data)
	if !ok {
		return nil, fmt.Errorf("%w: 仅支持 JPEG/PNG/WebP/GIF 图片", ErrSourceUnavailable)
	}

	return p.persistBytes(ctx, data, ext, contentType, resultKeyPrefix,
		model.FilePurposeResult, userID, model.FileMediaTypeImage)
}

// PersistVideoURLForUser 下载视频并转存到对象存储。
//
// 与图片的区别：体积上限更大、超时更长，并按内容识别 MP4 / MOV / WebM。
// 视频会整段读进内存，因此用 videoPersistSem 限制并发；并发满时返回
// ErrPersistBusy，调用方应稍后重试，而不是把任务判为失败。
func (p *Persister) PersistVideoURLForUser(ctx context.Context, rawURL string, userID int) (*model.File, error) {
	release, ok := acquireVideoPersistSlot()
	if !ok {
		return nil, ErrPersistBusy
	}
	defer release()

	target, err := parseSourceURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceUnavailable, err)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	timeout := p.cfg.VideoTimeout
	if timeout <= 0 {
		timeout = defaultVideoTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	maxBytes := p.cfg.VideoMaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultVideoMaxBytes
	}
	data, err := p.download(ctx, target, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceUnavailable, err)
	}

	ext, contentType, ok := detectVideoExt(data)
	if !ok {
		return nil, fmt.Errorf("%w: 仅支持 MP4/MOV/WebM 视频", ErrSourceUnavailable)
	}

	return p.persistBytes(ctx, data, ext, contentType, videoResultKeyPrefix,
		model.FilePurposeResult, userID, model.FileMediaTypeVideo)
}

// PersistBytes 把已经在内存中的图片转存到对象存储并登记 files 表。
//
// 供图片上传接口使用：内容已经在请求体里，不需要再下载一次。
// 过期时间与清理机制与 PersistURL 完全一致。
func (p *Persister) PersistBytes(ctx context.Context, data []byte) (*model.File, error) {
	ext, contentType, ok := detectImageExt(data)
	if !ok {
		return nil, errors.New("仅支持 JPEG/PNG/WebP/GIF 图片")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return p.persistBytes(ctx, data, ext, contentType, "",
		model.FilePurposeUpload, 0, model.FileMediaTypeImage)
}

// 生成结果在对象存储中的前缀。
// 旧结果文件仍保留在 png/、jpg/ 等扩展名前缀下，新结果统一走 result/<media>/。
const (
	resultKeyPrefix      = "result/image/"
	videoResultKeyPrefix = "result/video/"
)

// persistBytes 执行「上传对象 + 登记记录」。
// 调用方负责确定扩展名与 Content-Type，并控制 context 的时限。
// keyPrefix 为空时沿用 <ext>/<file_id> 的旧布局（图片上传）；非空时使用
// <keyPrefix><file_id>（生成结果）。userID 为 0 表示无归属的历史/公开上传。
func (p *Persister) persistBytes(ctx context.Context, data []byte, ext, contentType, keyPrefix, purpose string, userID int, mediaType string) (*model.File, error) {
	store, err := p.newStore(p.cfg.ServiceType, "")
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt < maxInsertAttempts; attempt++ {
		fileID, err := p.newFileID(ext)
		if err != nil {
			return nil, err
		}
		key := ext + "/" + fileID
		if keyPrefix != "" {
			key = keyPrefix + fileID
		}

		if err := store.Upload(ctx, key, data, contentType); err != nil {
			return nil, err
		}

		now := p.now()
		expiresAt := now.Add(p.cfg.TTL)
		file := &model.File{
			FileID:         fileID,
			ServiceType:    store.ServiceType(),
			ServiceAddress: store.ServiceAddress(),
			FileKey:        key,
			URL:            store.ObjectURL(key),
			TTLSeconds:     int(p.cfg.TTL / time.Second),
			CreatedAt:      now,
			ExpiresAt:      &expiresAt,
			UserID:         userID,
			MediaType:      mediaType,
			Purpose:        purpose,
			ContentType:    contentType,
			Bytes:          int64(len(data)),
		}

		if err := p.createFile(file); err != nil {
			lastErr = err
			if model.IsDuplicateKeyError(err) {
				// 96 位随机值撞库概率极低；真撞上就换一个 ID 重传。
				// 这里刻意不删除刚上传的对象：扩展名相同时它的 key 与已有记录
				// 完全相同，删除会毁掉那条有效记录。极小概率的残留可以接受。
				common.SysErrorf("[FileStore] file_id 冲突，换一个重试: %s", fileID)
				continue
			}
			p.cleanupAfterCreateFailure(store, fileID, key)
			return nil, err
		}
		return file, nil
	}

	return nil, fmt.Errorf("file_id 冲突重试次数用尽: %w", lastErr)
}

// cleanupAfterCreateFailure 在落库失败后删除刚上传的对象，避免留下孤儿对象。
//
// 只有确认记录确实没写进去时才删：PostgreSQL 在 COMMIT 成功后、客户端收到
// 响应前断开连接时，调用方拿到的是错误，但记录其实已经存在；此时删对象会让
// 那条记录永远读不到内容。清理失败只记日志，不改变调用方拿到的主错误。
func (p *Persister) cleanupAfterCreateFailure(store storage.Store, fileID, key string) {
	exists, err := p.fileExists(fileID)
	if err != nil {
		common.SysErrorf("[FileStore] 落库失败且无法确认记录状态，保留对象: file_id=%s, err=%v", fileID, err)
		return
	}
	if exists {
		common.SysErrorf("[FileStore] 落库返回错误但记录已存在，保留对象: file_id=%s", fileID)
		return
	}

	// 原 context 可能已经超时（落库失败有时就发生在超时之后），
	// 单独开一个短超时让清理有机会完成。
	ctx, cancel := context.WithTimeout(context.Background(), orphanCleanupTimeout)
	defer cancel()

	if err := store.Delete(ctx, key); err != nil {
		common.SysErrorf("[FileStore] 落库失败后清理对象失败，可能残留: key=%s, err=%v", key, err)
	}
}

// download 下载源文件并施加体积上限。
func (p *Persister) download(ctx context.Context, target *url.URL, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("构造下载请求失败: %w", err)
	}
	// 显式要求不压缩，避免按压缩后的长度做上限判断时失真
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载文件失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("下载文件返回 HTTP %d", resp.StatusCode)
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("文件超过 %d 字节上限", maxBytes)
	}

	// 多读 1 字节用于判断是否超限：只信任 Content-Length 会放过分块传输
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("文件超过 %d 字节上限", maxBytes)
	}
	return data, nil
}

// parseSourceURL 校验源地址。
// 只接受 http(s)、必须带主机名；带用户凭据的地址一并拒绝。
// 内网/回环地址由出站客户端的拨号期校验拦截，这里不重复解析 DNS。
func parseSourceURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("URL 无效: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("仅支持 http/https URL，当前为 %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("URL 缺少主机名")
	}
	if u.User != nil {
		return nil, errors.New("URL 不允许携带用户名/密码")
	}
	return u, nil
}

// detectImageExt 按文件头识别图片类型，返回扩展名与标准 Content-Type。
//
// 只认这四种：与上传接口的白名单一致，也覆盖 APIMart 成图的常见格式。
// 扩展名以内容为准而不是 URL 后缀：上游 URL 常常没有后缀，或后缀与实际内容不符。
func detectImageExt(data []byte) (string, string, bool) {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "jpg", "image/jpeg", true
	case len(data) >= 8 && bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return "png", "image/png", true
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "webp", "image/webp", true
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "gif", "image/gif", true
	default:
		return "", "", false
	}
}

// detectVideoExt 按文件头识别视频类型，返回扩展名与标准 Content-Type。
//
// 只认 MP4/MOV/WebM：APIMart 固定输出 1080p，实际常见为 MP4；
// 以内容为准而不是 URL 后缀，避免上游不带后缀或后缀与实际不符。
func detectVideoExt(data []byte) (string, string, bool) {
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		// MOV 的 major brand 是 "qt  "；其余 ftyp 容器按 MP4 处理。
		if string(data[8:12]) == "qt  " {
			return "mov", "video/quicktime", true
		}
		return "mp4", "video/mp4", true
	}
	if len(data) >= 4 && data[0] == 0x1A && data[1] == 0x45 && data[2] == 0xDF && data[3] == 0xA3 {
		return "webm", "video/webm", true
	}
	return "", "", false
}

// generateFileID 生成随机不可猜的文件 ID，形如 file_<24位hex>.png。
// 随机部分使用 crypto/rand：该 ID 是公开读取接口唯一的访问凭证。
func generateFileID(ext string) (string, error) {
	b := make([]byte, fileIDRandomBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成 file_id 失败: %w", err)
	}
	return "file_" + hex.EncodeToString(b) + "." + ext, nil
}
