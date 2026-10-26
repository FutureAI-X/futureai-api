package model

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// MaxFileIDLen 文件 ID 的最大长度，与 files.file_id 的列宽一致。
const MaxFileIDLen = 64

// File 对外可读文件的登记记录。
//
// 本表由负责上传的一方写入，本服务只读：GET /v1/images/get/:file_id
// 按 file_id 查出 url，再把内容流式转发给调用方。
//
// 读取侧以 expires_at 为准判断是否过期（NULL / 零值表示长期有效）。
// ttl_seconds 仅作为写入方计算过期时间时的原始依据保留，读取时不参与判断，
// 避免写入方算错时间时读侧还替它兜底，把问题掩盖成「文件一直能访问」。
type File struct {
	ID int `json:"id" gorm:"primaryKey"`
	// FileID 是对外的访问凭证：接口无需认证，知道 ID 即可读取文件，
	// 因此写入方必须使用不可猜测的随机值（见 GenerateFileID）。
	FileID string `json:"file_id" gorm:"uniqueIndex;size:64;not null"`

	// ServiceType 文件所在的对象存储服务，如 TENCENT_COS。
	// ServiceAddress / FileKey 用于实际访问该存储，完整 URL 无法访问时
	// 可据此重新签发地址。
	ServiceType    string `json:"service_type" gorm:"size:32;not null;default:''"`
	ServiceAddress string `json:"service_address" gorm:"size:512;not null;default:''"`
	FileKey        string `json:"file_key" gorm:"size:512;not null;default:''"`

	// URL 当前可直接读取文件的完整地址，读取接口实际代理的就是它。
	URL        string `json:"url" gorm:"type:text;not null"`
	TTLSeconds int    `json:"ttl_seconds" gorm:"not null;default:0"`

	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at" gorm:"index"`

	// ObjectDeletedAt 对象已从存储删除的时间。
	// 删除对象后记录仍保留一段保留期：期间读取接口继续按过期返回 410，
	// 保留期满后记录本身才会被清理（见 filestore 的清理任务）。
	// 刻意为空表示对象尚未删除；长期有效的记录永远不会被清理。
	ObjectDeletedAt *time.Time `json:"object_deleted_at"`
}

// TableName 指定表名
func (File) TableName() string {
	return "files"
}

// GenerateFileID 生成对外使用的文件 ID（file_ + 24 位十六进制）。
//
// 该 ID 是公开读取接口唯一的访问凭证，必须足够随机以防遍历，
// 因此使用 crypto/rand。生成规则放在本包内，供写入方复用同一格式。
func GenerateFileID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "file_" + hex.EncodeToString(b)
}

// GetFileByFileID 按文件 ID 查询记录
func GetFileByFileID(fileID string) (*File, error) {
	var file File
	if err := DB.Where("file_id = ?", fileID).First(&file).Error; err != nil {
		return nil, err
	}
	return &file, nil
}

// CreateFile 插入文件记录。
// 调用方负责先完成对象上传；本函数只落库，失败时由调用方决定是否重试。
func CreateFile(file *File) error {
	return DB.Create(file).Error
}

// FileIDExists 判断某个 file_id 是否已经落库。
//
// 用于「落库返回错误」之后的确认：PostgreSQL 在 COMMIT 成功后、客户端收到
// 响应前断开连接时，调用方看到的是错误，而记录其实已经写入。
func FileIDExists(fileID string) (bool, error) {
	var count int64
	if err := DB.Model(&File{}).Where("file_id = ?", fileID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListFilesPendingObjectDeletion 查询「已过期但对象尚未删除」的记录。
//
// 时间窗 [now-grace-retryWindow, now-grace] 有两个作用：
//   - 上界留出宽限期，避免删除仍在被读取接口代理流式转发的对象；
//   - 下界避免每轮扫描全部历史过期记录，也让超过重试窗口仍失败的记录
//     自然退出（对象会残留在桶里，通过日志暴露）。
//
// 走 expires_at 索引，范围固定，不随库里历史数据增长而变慢。
func ListFilesPendingObjectDeletion(now time.Time, grace, retryWindow time.Duration, limit int) ([]File, error) {
	if limit <= 0 {
		return nil, nil
	}
	var files []File
	err := DB.
		Where("expires_at IS NOT NULL AND expires_at <= ? AND expires_at > ? AND object_deleted_at IS NULL",
			now.Add(-grace), now.Add(-grace-retryWindow)).
		Order("expires_at ASC, id ASC").
		Limit(limit).
		Find(&files).Error
	return files, err
}

// MarkFileObjectDeleted 标记对象已从存储删除。重复标记无副作用。
func MarkFileObjectDeleted(id int, at time.Time) error {
	return DB.Model(&File{}).
		Where("id = ? AND object_deleted_at IS NULL", id).
		Update("object_deleted_at", at).Error
}

// PurgeFiles 删除保留期已满的记录，返回实际删除条数。
// 保留期从对象删除时间算起，而不是从过期时间算起。
//
// expires_at 的下界只是为了让查询继续走索引；它不会误伤真正可回收的记录，
// 因为可回收记录的 object_deleted_at >= expires_at。
func PurgeFiles(now time.Time, grace, retention time.Duration, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}

	var ids []int
	err := DB.Model(&File{}).
		Where("expires_at IS NOT NULL AND expires_at <= ? AND object_deleted_at IS NOT NULL AND object_deleted_at <= ?",
			now.Add(-grace-retention), now.Add(-retention)).
		Order("expires_at ASC, id ASC").
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return 0, err
	}

	res := DB.Where("id IN ?", ids).Delete(&File{})
	return res.RowsAffected, res.Error
}
