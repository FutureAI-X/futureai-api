// Package filestore 把外部 URL 的文件转存到对象存储，并登记到 files 表。
//
// 这是「先下载、再上传、最后落库」的编排层：
// 对象存储的差异由 storage.Store 抽象，本包只负责下载校验、
// 生成不可猜的文件 ID、计算有效期与写库。
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

	// defaultMaxBytes 单个文件下载上限。
	// 成图通常远小于这个值，上限只用于防住被控上游用无限流打爆内存。
	defaultMaxBytes int64 = 32 << 20

	// defaultTimeout 单次转存（下载 + 上传）的总超时
	defaultTimeout = 90 * time.Second

	// fileIDRandomBytes 随机部分的字节数（12 字节 = 24 位十六进制 = 96 位熵）
	fileIDRandomBytes = 12

	// maxInsertAttempts 落库遇到 file_id 冲突时的最大尝试次数
	maxInsertAttempts = 3
)

// Config 转存配置
type Config struct {
	ServiceType string
	TTL         time.Duration
	MaxBytes    int64
	Timeout     time.Duration
}

// LoadConfig 从环境变量读取配置。
// 配置非法时按默认值处理并打错误日志，不阻止服务启动：
// 成图转存是尽力而为的附加能力，不该因为一次笔误让整个服务起不来。
func LoadConfig() Config {
	cfg := Config{
		ServiceType: storage.ServiceTencentCOS,
		TTL:         DefaultTTL,
		MaxBytes:    defaultMaxBytes,
		Timeout:     defaultTimeout,
	}

	raw := strings.TrimSpace(os.Getenv(ttlEnv))
	if raw == "" {
		return cfg
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		common.SysErrorf("[FileStore] %s 配置无效(%q)，按默认 %d 秒处理",
			ttlEnv, raw, int64(DefaultTTL/time.Second))
		return cfg
	}
	cfg.TTL = time.Duration(seconds) * time.Second
	return cfg
}

// Persister 转存执行器。
// 字段不导出，测试可在同包内替换依赖。
type Persister struct {
	cfg        Config
	newStore   func(serviceType, serviceAddress string) (storage.Store, error)
	createFile func(*model.File) error
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
func (p *Persister) PersistURL(ctx context.Context, rawURL string) (*model.File, error) {
	target, err := parseSourceURL(rawURL)
	if err != nil {
		return nil, err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()

	data, err := p.download(ctx, target)
	if err != nil {
		return nil, err
	}

	ext, contentType, ok := detectImageExt(data)
	if !ok {
		return nil, errors.New("仅支持 JPEG/PNG/WebP/GIF 图片")
	}

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
		}

		if err := p.createFile(file); err != nil {
			lastErr = err
			if model.IsDuplicateKeyError(err) {
				// 96 位随机值撞库概率极低；真撞上就换一个 ID 重传。
				// 上一次上传的对象会残留，由存储侧生命周期规则清理。
				common.SysErrorf("[FileStore] file_id 冲突，换一个重试: %s", fileID)
				continue
			}
			return nil, err
		}
		return file, nil
	}

	return nil, fmt.Errorf("file_id 冲突重试次数用尽: %w", lastErr)
}

// download 下载源文件并施加体积上限。
func (p *Persister) download(ctx context.Context, target *url.URL) ([]byte, error) {
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
	if resp.ContentLength > p.cfg.MaxBytes {
		return nil, fmt.Errorf("文件超过 %d 字节上限", p.cfg.MaxBytes)
	}

	// 多读 1 字节用于判断是否超限：只信任 Content-Length 会放过分块传输
	data, err := io.ReadAll(io.LimitReader(resp.Body, p.cfg.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	if int64(len(data)) > p.cfg.MaxBytes {
		return nil, fmt.Errorf("文件超过 %d 字节上限", p.cfg.MaxBytes)
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

// generateFileID 生成随机不可猜的文件 ID，形如 file_<24位hex>.png。
// 随机部分使用 crypto/rand：该 ID 是公开读取接口唯一的访问凭证。
func generateFileID(ext string) (string, error) {
	b := make([]byte, fileIDRandomBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成 file_id 失败: %w", err)
	}
	return "file_" + hex.EncodeToString(b) + "." + ext, nil
}
