// Package storage 定义对象存储的统一入口。
//
// 新增存储服务时实现 Store 接口并在 FromEnv 中注册即可；
// 上层的转存逻辑（filestore）与读取接口都只依赖这个接口。
package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ServiceTencentCOS 腾讯云 COS 的服务类型标识，写入 files.service_type。
const ServiceTencentCOS = "TENCENT_COS"

var (
	// ErrNotConfigured 对象存储未配置（缺少地址或密钥）
	ErrNotConfigured = errors.New("对象存储未配置")
	// ErrNotSupported 不支持的服务类型
	ErrNotSupported = errors.New("不支持的对象存储服务")
)

// Store 对象存储的统一接口。
type Store interface {
	// ServiceType 返回写入 files.service_type 的值。
	ServiceType() string
	// ServiceAddress 返回写入 files.service_address 的值（桶或服务地址）。
	ServiceAddress() string
	// Upload 上传对象。key 形如 png/file_xxx.png。
	Upload(ctx context.Context, key string, data []byte, contentType string) error
	// PresignGet 生成可直接 GET 的临时地址，供读取接口代理。
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	// ObjectURL 返回对象的规范地址。私有桶下它不可匿名访问，仅作记录。
	ObjectURL(key string) string
}

// FromEnv 按服务类型构造存储实例。
//
// serviceAddress 为空时回落到该服务自己的环境变量；读取 files 表的历史记录时
// 应传入记录里的 service_address，写入时留空即可。
func FromEnv(serviceType, serviceAddress string) (Store, error) {
	switch strings.ToUpper(strings.TrimSpace(serviceType)) {
	case ServiceTencentCOS:
		return newTencentCOSFromEnv(serviceAddress)
	default:
		return nil, fmt.Errorf("%w: %q", ErrNotSupported, serviceType)
	}
}
