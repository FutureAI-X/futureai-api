package model

import (
	"time"
)

// Model 模型信息
// ModelType 模型类型。
// 存英文键、中文标签放前端——与 rule_type / status 的既有约定一致，改文案不动数据。
type ModelType string

const (
	ModelTypeImage ModelType = "image" // 图像生成
	ModelTypeVideo ModelType = "video" // 视频生成
	ModelTypeText  ModelType = "text"  // 文本生成
	ModelTypeMusic ModelType = "music" // 音乐生成
	ModelTypeOther ModelType = "other" // 其他
)

// AllModelTypes 全部合法类型，供管理端校验使用（顺序与前端选项一致）
var AllModelTypes = []ModelType{
	ModelTypeImage,
	ModelTypeVideo,
	ModelTypeText,
	ModelTypeMusic,
	ModelTypeOther,
}

// IsValidModelType 判断类型是否在枚举内
func IsValidModelType(t ModelType) bool {
	for _, valid := range AllModelTypes {
		if t == valid {
			return true
		}
	}
	return false
}

type Model struct {
	// 唯一标识，自增主键
	ID int `json:"id" gorm:"primaryKey"`

	// 开发者/提供商名称（兼容旧数据）
	Owner string `json:"owner" gorm:"size:64;default:futureai-api"`

	// 名称，全局唯一，用于 API 调用
	Name string `json:"name" gorm:"uniqueIndex;size:64;not null"`

	// 类型：image=图像生成, video=视频生成, text=文本生成, music=音乐生成, other=其他
	//
	// default 是给存量表回填用的：ADD COLUMN ... NOT NULL DEFAULT 'image'
	// 会让 PostgreSQL 把已有行一并填成 image。注意它不是「只用于回填」——
	// GORM 在每次 INSERT 时也会带上它，零值会被替换成 image，
	// 所以「新建模型忘了选类型」在数据库层会静默变成图像模型，
	// 拦截点是管理端 API 的 binding:"required"。
	Type ModelType `json:"type" gorm:"size:32;not null;default:image"`

	// 标签，逗号分隔
	Tags string `json:"tags,omitempty" gorm:"size:255"`

	// 描述
	Description string `json:"description,omitempty" gorm:"type:text"`

	// 状态：1=启用, 2=禁用
	Status int `json:"status" gorm:"default:1"`

	// 记录创建时间
	CreatedAt time.Time `json:"created_at"`

	// 记录最后更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// GetModels 获取所有可用模型
func GetModels() ([]Model, error) {
	var models []Model
	err := DB.Where("status = ?", 1).Find(&models).Error
	return models, err
}

// GetModelByName 根据名称获取模型
func GetModelByName(name string) (*Model, error) {
	var model Model
	err := DB.Where("name = ? AND status = ?", name, 1).First(&model).Error
	if err != nil {
		return nil, err
	}
	return &model, nil
}

// GetPricingModels 获取所有可用模型（带积分规则）
func GetPricingModels() ([]map[string]interface{}, error) {
	var models []Model
	err := DB.Where("status = ?", 1).
		Order("name ASC").
		Find(&models).Error
	if err != nil {
		return nil, err
	}

	// 获取所有启用的积分规则（顺序必须确定，见 PreloadRuleItems）
	var rules []CreditRule
	PreloadRuleItems(DB).Where("status = ?", 1).Find(&rules)

	// 构建模型ID到规则的映射
	ruleMap := make(map[int]*CreditRule)
	for i := range rules {
		ruleMap[rules[i].ModelID] = &rules[i]
	}

	// 构建返回数据
	result := make([]map[string]interface{}, len(models))
	for i, m := range models {
		item := map[string]interface{}{
			"id":          m.ID,
			"name":        m.Name,
			"description": m.Description,
			"tags":        m.Tags,
			"owner":       m.Owner,
			"status":      m.Status,
			"type":        m.Type,
		}
		if rule, ok := ruleMap[m.ID]; ok {
			item["credit_rule"] = rule
		}
		result[i] = item
	}

	return result, nil
}

// AdminGetModels 管理员获取全部模型（含禁用）
func AdminGetModels() ([]Model, error) {
	var models []Model
	err := DB.Order("id ASC").Find(&models).Error
	return models, err
}

// GetModelByID 根据 ID 获取模型
func GetModelByID(id int) (*Model, error) {
	var m Model
	err := DB.Where("id = ?", id).First(&m).Error
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// CreateModel 创建模型
func CreateModel(m *Model) error {
	return DB.Create(m).Error
}

// UpdateModel 更新模型
func UpdateModel(id int, updates map[string]interface{}) error {
	return DB.Model(&Model{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateModelStatus 更新模型状态
func UpdateModelStatus(id int, status int) error {
	return DB.Model(&Model{}).Where("id = ?", id).Update("status", status).Error
}

// DeleteModel 删除模型（硬删除，直接从表中删除）
func DeleteModel(id int) error {
	return DB.Delete(&Model{}, id).Error
}
