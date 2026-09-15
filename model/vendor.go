package model

import (
	"time"
)

// Vendor 供应商信息
type Vendor struct {
	// 唯一标识，自增主键
	ID int `json:"id" gorm:"primaryKey"`

	// 名称。唯一：上传路径通过名称（忽略大小写）定位供应商，
	// 重名会让凭据选择变得不确定。
	Name string `json:"name" gorm:"size:128;not null;uniqueIndex"`

	// 描述
	Description string `json:"description,omitempty" gorm:"type:text"`

	// API 基础地址
	BaseURL string `json:"base_url" gorm:"size:512"`

	// API 密钥（密文）。json:"-" 确保该字段永不会被误序列化到任何 API 响应中，
	// 需要对外展示时一律使用 PublicVendor 或显式脱敏。
	APIKey string `json:"-" gorm:"size:512"`

	// 状态：1=启用, 2=禁用, 3=已删除
	Status int `json:"status" gorm:"default:1"`

	// 记录创建时间
	CreatedAt time.Time `json:"created_at"`

	// 记录最后更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// Vendor 状态常量
const (
	VendorStatusEnabled  = 1 // 启用
	VendorStatusDisabled = 2 // 禁用
	VendorStatusDeleted  = 3 // 已删除
)

// GetVendors 获取所有启用的供应商
func GetVendors() ([]Vendor, error) {
	var vendors []Vendor
	err := DB.Where("status = ?", 1).Order("id ASC").Find(&vendors).Error
	return vendors, err
}

// GetVendorMap 获取供应商名称到对象的映射
func GetVendorMap() (map[string]Vendor, error) {
	var vendors []Vendor
	err := DB.Where("status = ?", 1).Find(&vendors).Error
	if err != nil {
		return nil, err
	}
	m := make(map[string]Vendor, len(vendors))
	for _, v := range vendors {
		m[v.Name] = v
	}
	return m, nil
}

// GetVendorMapByID 获取供应商 ID 到对象的映射
func GetVendorMapByID() (map[int]Vendor, error) {
	var vendors []Vendor
	err := DB.Where("status = ?", 1).Find(&vendors).Error
	if err != nil {
		return nil, err
	}
	m := make(map[int]Vendor, len(vendors))
	for _, v := range vendors {
		m[v.ID] = v
	}
	return m, nil
}

// AdminGetVendors 管理员获取全部供应商（不含已删除）
func AdminGetVendors() ([]Vendor, error) {
	var vendors []Vendor
	err := DB.Where("status != ?", VendorStatusDeleted).Order("id ASC").Find(&vendors).Error
	return vendors, err
}

// GetEnabledVendorByID 根据 ID 获取「启用」的供应商。
// 用于生成/上传等真实调用路径：禁用（status=2）的供应商必须立刻停止承接流量，
// 否则运维停用失效/欠费供应商后流量仍会继续消耗。
func GetEnabledVendorByID(id int) (*Vendor, error) {
	var vendor Vendor
	err := DB.Where("id = ? AND status = ?", id, VendorStatusEnabled).First(&vendor).Error
	if err != nil {
		return nil, err
	}
	return &vendor, nil
}

// GetVendorByID 根据 ID 获取供应商（不含已删除）
func GetVendorByID(id int) (*Vendor, error) {
	var vendor Vendor
	err := DB.Where("id = ? AND status != ?", id, VendorStatusDeleted).First(&vendor).Error
	if err != nil {
		return nil, err
	}
	return &vendor, nil
}

// GetEnabledVendorByNameInsensitive 根据名称获取启用的供应商（名称忽略大小写）
func GetEnabledVendorByNameInsensitive(name string) (*Vendor, error) {
	var vendor Vendor
	err := DB.Where("LOWER(name) = LOWER(?) AND status = ?", name, VendorStatusEnabled).First(&vendor).Error
	if err != nil {
		return nil, err
	}
	return &vendor, nil
}

// CreateVendor 创建供应商
func CreateVendor(vendor *Vendor) error {
	return DB.Create(vendor).Error
}

// UpdateVendor 更新供应商
func UpdateVendor(id int, updates map[string]interface{}) error {
	return DB.Model(&Vendor{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateVendorStatus 更新供应商状态
func UpdateVendorStatus(id int, status int) error {
	return DB.Model(&Vendor{}).Where("id = ?", id).Update("status", status).Error
}

// DeleteVendor 删除供应商（置为已删除状态，非物理删除）
func DeleteVendor(id int) error {
	return DB.Model(&Vendor{}).Where("id = ?", id).Update("status", VendorStatusDeleted).Error
}
