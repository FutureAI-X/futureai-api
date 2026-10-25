package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/tencentyun/cos-go-sdk-v5"
)

const (
	// 腾讯云 COS 的配置项。桶地址形如
	// https://examplebucket-1250000000.cos.ap-guangzhou.myqcloud.com
	tencentCOSBucketURLEnv = "TENCENT_COS_BUCKET_URL"
	tencentCOSSecretIDEnv  = "TENCENT_COS_SECRET_ID"
	tencentCOSSecretKeyEnv = "TENCENT_COS_SECRET_KEY"

	// defaultPresignTTL 调用方未指定时的预签名有效期
	defaultPresignTTL = 5 * time.Minute
)

// TencentCOSConfig 腾讯云 COS 客户端配置。
type TencentCOSConfig struct {
	BucketURL string
	SecretID  string
	SecretKey string
	// Transport 可选，测试或特殊网络环境可注入；
	// 默认复用进程级出站 Transport，保证连接池共享与 SSRF 防护。
	Transport http.RoundTripper
}

// 编译期确认实现完整
var _ Store = (*TencentCOS)(nil)

// TencentCOS 腾讯云 COS 实现。
type TencentCOS struct {
	bucketURL string
	secretID  string
	secretKey string
	client    *cos.Client
}

// NewTencentCOS 创建 COS 客户端。
func NewTencentCOS(cfg TencentCOSConfig) (*TencentCOS, error) {
	bucketURL := strings.TrimRight(strings.TrimSpace(cfg.BucketURL), "/")
	if bucketURL == "" {
		return nil, fmt.Errorf("%w: %s 未配置", ErrNotConfigured, tencentCOSBucketURLEnv)
	}

	u, err := url.Parse(bucketURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%w: %s 不是合法的 http(s) 地址", ErrNotConfigured, tencentCOSBucketURLEnv)
	}

	secretID := strings.TrimSpace(cfg.SecretID)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	if secretID == "" || secretKey == "" {
		return nil, fmt.Errorf("%w: %s / %s 未配置", ErrNotConfigured, tencentCOSSecretIDEnv, tencentCOSSecretKeyEnv)
	}

	transport := cfg.Transport
	if transport == nil {
		// 复用共享 Transport：SDK 的签名层只包裹 RoundTripper，
		// 底层连接池仍由它管理，避免每次上传都重新握手。
		transport = common.FileDownloadHTTPClient().Transport
	}

	client := cos.NewClient(&cos.BaseURL{BucketURL: u}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  secretID,
			SecretKey: secretKey,
			Transport: transport,
		},
	})

	return &TencentCOS{
		bucketURL: bucketURL,
		secretID:  secretID,
		secretKey: secretKey,
		client:    client,
	}, nil
}

// newTencentCOSFromEnv 从环境变量构造 COS 客户端。
// serviceAddress 优先（读取历史记录时按记录里的桶地址签 URL）。
func newTencentCOSFromEnv(serviceAddress string) (Store, error) {
	bucketURL := strings.TrimSpace(serviceAddress)
	if bucketURL == "" {
		bucketURL = strings.TrimSpace(os.Getenv(tencentCOSBucketURLEnv))
	}

	return NewTencentCOS(TencentCOSConfig{
		BucketURL: bucketURL,
		SecretID:  os.Getenv(tencentCOSSecretIDEnv),
		SecretKey: os.Getenv(tencentCOSSecretKeyEnv),
	})
}

// ServiceType 实现 Store
func (s *TencentCOS) ServiceType() string {
	return ServiceTencentCOS
}

// ServiceAddress 实现 Store
func (s *TencentCOS) ServiceAddress() string {
	return s.bucketURL
}

// Upload 上传对象。
// 不设置单对象 ACL：让对象继承桶策略，避免逐对象 ACL 命中条数限制，
// 也避免把「私有桶」的假设写死在代码里。
func (s *TencentCOS) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := s.client.Object.Put(ctx, key, bytes.NewReader(data), &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType:   contentType,
			ContentLength: int64(len(data)),
		},
	})
	if err != nil {
		return fmt.Errorf("COS 上传失败: %w", err)
	}
	return nil
}

// PresignGet 生成临时下载地址。
// 签名在本地完成，不产生额外网络请求；读取接口拿到后立即代理。
func (s *TencentCOS) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("缺少 file_key")
	}
	if ttl <= 0 {
		ttl = defaultPresignTTL
	}

	u, err := s.client.Object.GetPresignedURL(
		ctx, http.MethodGet, key, s.secretID, s.secretKey, ttl, nil,
	)
	if err != nil {
		return "", fmt.Errorf("COS 预签名失败: %w", err)
	}
	return u.String(), nil
}

// ObjectURL 返回对象的规范地址（私有桶下不可匿名访问）。
func (s *TencentCOS) ObjectURL(key string) string {
	return s.bucketURL + "/" + strings.TrimLeft(key, "/")
}
