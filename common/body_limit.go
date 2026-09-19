package common

import (
	"errors"
	"net/http"
)

// 请求体大小上限。
//
// 为什么必须有：gin 的 ShouldBindJSON 会把整个请求体读进内存并分配等量空间，
// 且**结构体里没有的字段同样会被完整解析后才丢弃**。没有上限时，
// 一个几百 MB 的 JSON 就能让进程分配等量内存，并发几十个即被 OOM 杀掉。
//
// 认证接口尤其危险：/api/auth/login 无需任何凭证，而它的防爆破计数只在
// **凭证错误**时递增，JSON 解析失败走的是另一条分支——于是攻击者可以
// 无限次携带超大请求体打这个接口，既不触发限流，也无需账号。
const (
	// DefaultBodyLimit JSON 端点（认证、用户、管理、业务）的请求体上限。
	// 1MB 远大于任何正常请求（实际只有几 KB），只用来兜住异常与恶意体积。
	DefaultBodyLimit int64 = 1 << 20

	// MaxUploadFileSize 单个上传文件的大小上限
	MaxUploadFileSize int64 = 10 << 20

	// MultipartOverhead multipart 边框与头字段占用的额外字节余量。
	// 请求体上限 = 文件上限 + 余量，否则一个刚好 10MB 的文件会因为
	// 边框开销而被整体拒掉。
	MultipartOverhead int64 = 64 << 10
)

// UploadPath 是唯一需要放宽体积上限的端点。
// 其余端点统一按 DefaultBodyLimit 限制——白名单而非黑名单：
// 将来新增的端点默认受保护，而不是默认敞开。
const UploadPath = "/v1/uploads/images"

// BodyLimitFor 返回该请求路径适用的请求体上限。
func BodyLimitFor(path string) int64 {
	if path == UploadPath {
		return MaxUploadFileSize + MultipartOverhead
	}
	return DefaultBodyLimit
}

// IsBodyTooLarge 判断错误是否由请求体超出上限产生（MaxBytesReader 的返回值）。
//
// 各处理函数用它把「体积超限」从「格式错误」里区分出来：
// 前者调用方要改请求大小，后者要改请求内容，混成同一个 400 会让人往错方向排查。
func IsBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}
