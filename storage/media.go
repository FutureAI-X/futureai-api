package storage

import (
	"context"
	"errors"
	"time"
)

// ErrObjectNotFound 对象不存在。
var ErrObjectNotFound = errors.New("object not found")

// ObjectMeta 对象存储 HEAD 结果。
type ObjectMeta struct {
	Size        int64
	ContentType string
	ETag        string
}

// MediaProbe 音视频媒体信息。
type MediaProbe struct {
	// DurationMS 容器/流时长（毫秒）。
	DurationMS int64
	Width      int
	Height     int
	FPS        float64

	// VideoCodecs / AudioCodecs 保留所有流的编码名（小写）。
	VideoCodecs []string
	AudioCodecs []string

	// Format 容器格式描述，例如 mov,mp4,m4a,3gp,3g2,mj2。
	Format string

	HasVideo bool
	HasAudio bool
}

// ImageProbe 图片基本信息。
type ImageProbe struct {
	Width  int
	Height int
	Format string
}

// MediaStore 参考媒体上传所需的扩展能力。
//
// 刻意不把这些方法加到 Store 上：现有图片上传/读取只需要 Store，
// 新增接口可以避免已有实现与测试被迫补方法。
type MediaStore interface {
	Store

	// PresignPost 生成 COS POST Object 表单上传所需的 URL 与字段。
	// policy 限制 key、Content-Type、大小范围与过期时间。
	PresignPost(ctx context.Context, key, contentType string, maxBytes int64, ttl time.Duration) (uploadURL string, fields map[string]string, expiresAt time.Time, err error)

	// Head 查询对象元数据。
	Head(ctx context.Context, key string) (*ObjectMeta, error)

	// GetRange 读取对象的一段字节，用于魔数校验。
	GetRange(ctx context.Context, key string, start, end int64) ([]byte, error)

	// Copy 服务端复制对象（staging -> final）。
	Copy(ctx context.Context, srcKey, dstKey string) error

	// ProbeAVInfo 使用 CI 媒体信息解析音频/视频。
	ProbeAVInfo(ctx context.Context, key string) (*MediaProbe, error)

	// ProbeImageInfo 使用 CI 图片信息解析宽高。
	ProbeImageInfo(ctx context.Context, key string) (*ImageProbe, error)
}

// MediaFromEnv 按当前环境变量构造媒体存储。
func MediaFromEnv() (MediaStore, error) {
	return MediaFrom(ServiceTencentCOS, "")
}

// MediaFrom 按服务类型与地址构造媒体存储。
// serviceAddress 为空时回落到环境变量，与 FromEnv 的语义一致。
func MediaFrom(serviceType, serviceAddress string) (MediaStore, error) {
	s, err := FromEnv(serviceType, serviceAddress)
	if err != nil {
		return nil, err
	}
	ms, ok := s.(MediaStore)
	if !ok {
		return nil, ErrNotSupported
	}
	return ms, nil
}
