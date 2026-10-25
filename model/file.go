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
