package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
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

	// ParamSpec 图像模型的参数规格 JSON；非图像模型为空。
	// 空值在数据库中是 NULL，在 JSON 中是 null。
	ParamSpec JSONText `json:"param_spec,omitempty" gorm:"type:jsonb"`

	// SortOrder 管理端手动排序值，越小越靠前；NULL 表示未手动排序。
	// 未排序的模型统一排在已排序模型之后，再按开发者、模型名回退。
	SortOrder *int `json:"sort_order,omitempty" gorm:"type:integer"`

	// 状态：1=启用, 2=禁用
	Status int `json:"status" gorm:"default:1"`

	// 记录创建时间
	CreatedAt time.Time `json:"created_at"`

	// 记录最后更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// modelDisplayOrder 三处模型列表共用的排序表达式。
//
// sort_order IS NULL 为 false 的行（已手动排序）排在前，true 的行（未排序）
// 排在后；未排序时回落到「开发者 → 模型名 → id」。三处查询必须共用它，
// 否则管理端、客户端下拉和模型广场会出现不同的模型顺序。
const modelDisplayOrder = "sort_order IS NULL, sort_order, LOWER(TRIM(owner)), LOWER(name), id"

// GetModels 获取所有可用模型
func GetModels() ([]Model, error) {
	var models []Model
	err := DB.Where("status = ?", 1).Order(modelDisplayOrder).Find(&models).Error
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
		Order(modelDisplayOrder).
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
	err := DB.Order(modelDisplayOrder).Find(&models).Error
	return models, err
}

// ErrModelNotFound 排序请求引用了不存在的模型，通常是被并发删除。
var ErrModelNotFound = errors.New("model not found")

// ReorderModels 按给定顺序写入 sort_order（1..N）。
//
// 调用方必须先校验 ids 非空且无重复。事务内先把所有模型置为 NULL，再给
// 请求里的 ID 顺序编号：这样请求里没有的模型（例如拖拽期间新建的）会落到
// 末尾，也不会和 1..N 产生重复序号。RowsAffected 为 0 时回滚，避免管理端
// 页面停留期间模型被删除后只写进去一半的顺序。
func ReorderModels(ids []int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// GORM 默认拒绝没有 WHERE 的全局更新，显式写 1=1 表示清空全部。
		// 用 UpdateColumn 跳过 updated_at 自动更新：排序是展示元数据，
		// 不该把每个模型的「最后修改时间」都刷一遍。
		if err := tx.Model(&Model{}).Where("1 = 1").UpdateColumn("sort_order", gorm.Expr("NULL")).Error; err != nil {
			return err
		}
		for i, id := range ids {
			res := tx.Model(&Model{}).Where("id = ?", id).UpdateColumn("sort_order", i+1)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return ErrModelNotFound
			}
		}
		return nil
	})
}

// ResetModelOrder 清空所有模型的手动排序，回落到默认顺序。
func ResetModelOrder() error {
	return DB.Model(&Model{}).Where("1 = 1").UpdateColumn("sort_order", gorm.Expr("NULL")).Error
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

// CreateModel 创建模型，并把新模型放到当前展示顺序的末尾。
//
// 如果直接让新模型的 sort_order 保持 NULL，它会落到「未排序」分组里，
// 按开发者/模型名排序，不一定在末尾。这里先把现有模型的当前顺序固化为
// 1..N（不触碰 updated_at），再给新模型 N+1，确保它稳定排在最后。
func CreateModel(m *Model) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var existing []Model
		if err := tx.Order(modelDisplayOrder).Find(&existing).Error; err != nil {
			return err
		}
		for i := range existing {
			if err := tx.Model(&Model{}).Where("id = ?", existing[i].ID).
				UpdateColumn("sort_order", i+1).Error; err != nil {
				return err
			}
		}
		next := len(existing) + 1
		m.SortOrder = &next
		return tx.Create(m).Error
	})
}

// UpdateModel 更新模型
func UpdateModel(id int, updates map[string]interface{}) error {
	return DB.Model(&Model{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateModelStatus 更新模型状态
func UpdateModelStatus(id int, status int) error {
	return DB.Model(&Model{}).Where("id = ?", id).Update("status", status).Error
}

// UpdateModelParamSpec 更新模型的参数规格。
// spec 为 nil 表示清除配置；非 nil 时必须是已通过元校验的 JSON。
func UpdateModelParamSpec(id int, spec *string) error {
	if spec == nil {
		return DB.Model(&Model{}).Where("id = ?", id).Update("param_spec", gorm.Expr("NULL")).Error
	}
	return DB.Model(&Model{}).Where("id = ?", id).
		Update("param_spec", gorm.Expr("?::jsonb", *spec)).Error
}

// DeleteModel 删除模型（硬删除，直接从表中删除）
func DeleteModel(id int) error {
	return DB.Delete(&Model{}, id).Error
}
