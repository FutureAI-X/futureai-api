package model

import (
	"time"
)

// Model 模型信息
type Model struct {
	// 唯一标识，自增主键
	ID int `json:"id" gorm:"primaryKey"`

	// 开发者/提供商名称（兼容旧数据）
	Owner string `json:"owner" gorm:"size:64;default:token-hub"`

	// 名称，全局唯一，用于 API 调用
	Name string `json:"name" gorm:"uniqueIndex;size:64;not null"`

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

// createDefaultModels 创建默认模型数据
func createDefaultModels() error {
	// 检查是否已有模型数据
	var count int64
	DB.Model(&Model{}).Count(&count)
	if count > 0 {
		return nil
	}

	// 创建默认模型
	defaultModels := []Model{
		// DeepSeek
		{Name: "deepseek-chat", Description: "DeepSeek V3 对话模型，擅长中文理解和代码生成", Tags: "对话,代码"},
		{Name: "deepseek-reasoner", Description: "DeepSeek R1 推理模型，支持深度思考", Tags: "推理,思考"},

		// OpenAI
		{Name: "gpt-4o", Description: "OpenAI 多模态旗舰模型，支持文本和图像输入", Tags: "多模态,对话"},
		{Name: "gpt-4o-mini", Description: "OpenAI 轻量模型，性价比极高", Tags: "轻量,快速"},
		{Name: "o3-mini", Description: "OpenAI 推理模型，支持复杂推理任务", Tags: "推理"},

		// Claude
		{Name: "claude-sonnet-4-20250514", Description: "Anthropic Claude 4 Sonnet，平衡性能与速度", Tags: "对话,代码"},
		{Name: "claude-haiku-3-5", Description: "Anthropic 轻量模型，响应极快", Tags: "快速,轻量"},

		// Gemini
		{Name: "gemini-2.5-flash", Description: "Google Gemini 2.5 Flash，高速多模态模型", Tags: "快速,多模态"},
		{Name: "gemini-2.5-pro", Description: "Google Gemini 2.5 Pro，最强推理能力", Tags: "推理,多模态"},
	}

	for _, m := range defaultModels {
		if m.Owner == "" {
			m.Owner = "token-hub"
		}
		if err := DB.Create(&m).Error; err != nil {
			return err
		}
	}

	return nil
}
