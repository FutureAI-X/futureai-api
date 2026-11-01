package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MaxFileIDLen 文件 ID 的最大长度，与 files.file_id 的列宽一致。
const MaxFileIDLen = 64

// 文件可见性。
//
// 存量记录与公开结果文件为 public；参考媒体（视频/音频/图片）为 private，
// 不会通过无认证的 GET /v1/images/get/:file_id 暴露。
const (
	FileVisibilityPublic  = "public"
	FileVisibilityPrivate = "private"
)

// 文件处理状态。
//
// 存量记录与结果文件为 ready；准备直传时为 pending；校验失败为 failed。
const (
	FileStatusPending = "pending"
	FileStatusReady   = "ready"
	FileStatusFailed  = "failed"
)

// 文件媒体类型。
const (
	FileMediaTypeImage = "image"
	FileMediaTypeVideo = "video"
	FileMediaTypeAudio = "audio"
)

// 文件用途。
const (
	FilePurposeReference = "reference" // 参考媒体（输入）
	FilePurposeResult    = "result"    // 生成结果（输出）
	FilePurposeUpload    = "upload"    // 公开图片上传（旧 /v1/uploads/images）
)

// ErrFileNotPending 表示文件不在 pending 状态，无法完成上传。
var ErrFileNotPending = errors.New("文件不在待处理状态")

// File 文件登记记录。
//
// 本表同时承载两类记录：
//   - public + ready：对外可读的结果文件，GET /v1/images/get/:file_id 可读取；
//   - private + pending/ready：上传的参考媒体，不会通过公开接口暴露。
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

	// ── 参考媒体（私有文件）──
	//
	// 以下字段对存量公开结果文件保持零值/默认值；上传参考媒体时填写。

	// UserID 上传者。参考媒体只允许上传者使用；存量公开文件为 0。
	UserID int `json:"user_id" gorm:"index;not null;default:0"`

	// Visibility public=公开结果文件；private=私有参考媒体。
	Visibility string `json:"visibility" gorm:"size:16;not null;default:'public'"`

	// Status pending=已 prepare 待完成；ready=可用；failed=校验失败。
	Status string `json:"status" gorm:"index;size:16;not null;default:'ready'"`

	// MediaType image/video/audio；存量公开文件为空。
	MediaType string `json:"media_type" gorm:"size:16;not null;default:''"`

	// Purpose reference=参考媒体, result=生成结果, upload=公开图片上传；存量为空。
	Purpose string `json:"purpose" gorm:"size:16;not null;default:''"`

	// ContentType 实际 MIME 类型（以服务端探测结果为准）。
	ContentType string `json:"content_type" gorm:"size:128;not null;default:''"`

	// Bytes 实际对象大小。
	Bytes int64 `json:"bytes" gorm:"not null;default:0"`

	// DurationMS 视频/音频时长（毫秒）；图片为 0。
	DurationMS int64 `json:"duration_ms" gorm:"not null;default:0"`

	// Width / Height 视频/图片宽高。
	Width  int `json:"width" gorm:"not null;default:0"`
	Height int `json:"height" gorm:"not null;default:0"`

	// MediaMeta 额外探测结果 JSON：fps、编码、容器格式等。
	MediaMeta string `json:"media_meta,omitempty" gorm:"type:text"`

	// OriginalName 客户端上传时的文件名（已清洗）。
	OriginalName string `json:"original_name" gorm:"size:255;not null;default:''"`

	// ErrorMessage 完成校验失败时的原因。
	ErrorMessage string `json:"error_message,omitempty" gorm:"size:512;not null;default:''"`
}

// BeforeCreate 为存量写入方补默认值。
//
// 现有图片上传/成图转存只关心公开文件，不会设置 visibility/status；
// 这里默认成 public/ready，保证它们的行为与新增字段之前完全一致。
// 新的参考媒体上传会显式设置 private/pending，不受影响。
func (f *File) BeforeCreate(tx *gorm.DB) error {
	if f.Visibility == "" {
		f.Visibility = FileVisibilityPublic
	}
	if f.Status == "" {
		f.Status = FileStatusReady
	}
	return nil
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

// GenerateFileIDWithExt 生成带扩展名的文件 ID，形如 file_<24位hex>.mp4。
// ext 由调用方的固定映射给出，不接受外部输入。
func GenerateFileIDWithExt(ext string) string {
	ext = strings.TrimPrefix(strings.TrimSpace(ext), ".")
	id := GenerateFileID()
	if ext == "" {
		return id
	}
	return id + "." + ext
}

// SumUserMediaBytes 统计用户当前占用的私有媒体字节数。
//
// 只统计 private 且 pending/ready 的记录；failed 记录的对象会被清理，
// 不再计入配额。pending 记录在 complete 前 bytes 为 0。
func SumUserMediaBytes(userID int, now time.Time) (int64, error) {
	var total int64
	err := DB.Model(&File{}).
		Where("user_id = ? AND visibility = ? AND status IN ? AND (expires_at IS NULL OR expires_at > ?)",
			userID, FileVisibilityPrivate,
			[]string{FileStatusPending, FileStatusReady}, now).
		Select("COALESCE(SUM(bytes), 0)").
		Scan(&total).Error
	return total, err
}

// UpdateFileReady 将 pending 的参考媒体置为 ready。
//
// 使用事务 + 行锁：complete 可能被并发调用（客户端重试），
// 只有第一个请求能完成状态转换；后续请求看到 ready 后直接返回。
// updates 中的 status/error_message 由本函数统一覆盖，调用方不需要传。
func UpdateFileReady(fileID string, userID int, updates map[string]interface{}) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var f File
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("file_id = ? AND user_id = ?", fileID, userID).
			First(&f).Error; err != nil {
			return err
		}

		// 已 ready：幂等返回，不覆盖已写入的元数据。
		if f.Status == FileStatusReady {
			return nil
		}
		if f.Status != FileStatusPending {
			return ErrFileNotPending
		}

		if updates == nil {
			updates = map[string]interface{}{}
		}
		updates["status"] = FileStatusReady
		updates["error_message"] = ""
		return tx.Model(&File{}).Where("id = ?", f.ID).Updates(updates).Error
	})
}

// UpdateFileFailed 将 pending 的参考媒体标记为失败。
// 终态守卫：只更新 pending 记录，避免覆盖已 ready 的文件。
func UpdateFileFailed(fileID string, userID int, message string) error {
	return DB.Model(&File{}).
		Where("file_id = ? AND user_id = ? AND status = ?", fileID, userID, FileStatusPending).
		Updates(map[string]interface{}{
			"status":        FileStatusFailed,
			"error_message": message,
		}).Error
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
