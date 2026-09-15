package model

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// DefaultRefImageParams 参考图参数名的内置默认值。
// 各供应商对同一张参考图的字段名不同（image / image_urls / ref_images ...），
// 这里只作为「管理员未显式配置」时的兜底。
const DefaultRefImageParams = "image,images,image_url,image_urls,ref_images"

// CreditRuleType 计费规则类型
type CreditRuleType string

const (
	// CreditRuleTypePerRequest 按次计费
	CreditRuleTypePerRequest CreditRuleType = "per_request"
)

// CreditRule 积分扣除规则（每个模型一条）
type CreditRule struct {
	// 规则唯一标识，自增主键
	ID int `json:"id" gorm:"primaryKey"`

	// 关联的模型 ID（唯一）
	ModelID int `json:"model_id" gorm:"uniqueIndex;not null"`

	// 规则类型：per_request=按次计费
	RuleType CreditRuleType `json:"rule_type" gorm:"size:32;not null;default:'per_request'"`

	// 基础积分（每次请求扣除的积分数量）
	BaseCredits float64 `json:"base_credits" gorm:"not null;default:0"`

	// 规则描述
	Description string `json:"description,omitempty" gorm:"type:text"`

	// 规则状态：1=启用, 2=禁用
	Status int `json:"status" gorm:"default:1"`

	// 记录创建时间
	CreatedAt time.Time `json:"created_at"`

	// 记录最后更新时间
	UpdatedAt time.Time `json:"updated_at"`

	// 参考图附加计费：每张参考图消耗的积分，0=不计费。
	// 叠加在基础积分（或命中的参数组合积分）之上，不取代它们。
	RefImageCredits float64 `json:"ref_image_credits" gorm:"not null;default:0"`

	// 参考图参数名（逗号分隔），空串表示使用 DefaultRefImageParams。
	// 刻意不加 not null：GORM 会生成无默认值的 ADD COLUMN ... NOT NULL，
	// 在已有数据的表上直接失败，导致服务起不来。
	RefImageParams string `json:"ref_image_params" gorm:"size:255"`

	// 关联的参数组合积分映射
	Items []CreditRuleItem `json:"items,omitempty" gorm:"foreignKey:RuleID"`
}

// TableName 指定表名（原为 quota_rules）
func (CreditRule) TableName() string {
	return "credit_rules"
}

// EffectiveRefImageParams 返回实际生效的参考图参数名列表。
// 未配置时回落到内置默认值，保证存量规则升级后也能识别参考图。
func (r *CreditRule) EffectiveRefImageParams() []string {
	if r == nil {
		return ParseRefImageParams(DefaultRefImageParams)
	}
	return ParseRefImageParams(r.RefImageParams)
}

// ParseRefImageParams 清洗参考图参数名配置：逗号切分、去空白、丢弃空项、去重。
// 清洗后为空（空串、全空白，或只剩逗号如 ","）时回落到内置默认值——否则
// 参考图永远识别不到，附加计费会静默失效。保留首次出现的顺序，
// 便于把清洗结果原样回写到数据库时输出稳定。
func ParseRefImageParams(raw string) []string {
	result := parseRefImageParams(raw)

	// 只剩分隔符的配置（如 ","）会被清洗成空列表，必须回落
	if len(result) == 0 && raw != DefaultRefImageParams {
		return parseRefImageParams(DefaultRefImageParams)
	}
	return result
}

// parseRefImageParams 纯粹的切分逻辑，不做默认值回落
func parseRefImageParams(raw string) []string {
	result := make([]string, 0, 4)
	seen := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result
}

// JoinRefImageParams 把参数名列表拼回逗号分隔的存储格式
func JoinRefImageParams(names []string) string {
	return strings.Join(names, ",")
}

// CreditRuleItem 参数组合积分映射项（一组条件的 AND 命中部请求时，使用该积分）
type CreditRuleItem struct {
	// 项唯一标识，自增主键
	ID int `json:"id" gorm:"primaryKey"`

	// 关联的规则 ID
	RuleID int `json:"rule_id" gorm:"index;not null"`

	// 该组合命中部请求时对应的积分
	Credits float64 `json:"credits" gorm:"not null;default:0"`

	// 记录创建时间
	CreatedAt time.Time `json:"created_at"`

	// 记录最后更新时间
	UpdatedAt time.Time `json:"updated_at"`

	// 该组合的 AND 条件（至少 1 条，无上限）
	Conditions []CreditRuleCondition `json:"conditions,omitempty" gorm:"foreignKey:ItemID"`
}

// TableName 指定表名（原为 quota_rule_items）
func (CreditRuleItem) TableName() string {
	return "credit_rule_items"
}

// CreditRuleCondition 参数映射条件（属于某个映射项，全部 AND）
type CreditRuleCondition struct {
	// 条件唯一标识，自增主键
	ID int `json:"id" gorm:"primaryKey"`

	// 所属映射项 ID
	ItemID int `json:"item_id" gorm:"index;not null"`

	// 请求参数路径（如 "resolution", "quality", "model"）
	ParamPath string `json:"param_path" gorm:"size:255;not null"`

	// 参数值（如 "1k", "low", "gpt-4"）
	ParamValue string `json:"param_value" gorm:"size:255;not null"`

	// 记录创建时间
	CreatedAt time.Time `json:"created_at"`

	// 记录最后更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (CreditRuleCondition) TableName() string {
	return "credit_rule_conditions"
}

// GetCreditRuleByModelID 获取指定模型的积分规则（含参数组合映射）
func GetCreditRuleByModelID(modelID int) (*CreditRule, error) {
	var rule CreditRule
	err := DB.Preload("Items.Conditions").Where("model_id = ? AND status = ?", modelID, 1).First(&rule).Error
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// GetCreditRuleByID 根据 ID 获取积分规则
func GetCreditRuleByID(id int) (*CreditRule, error) {
	var rule CreditRule
	err := DB.Preload("Items.Conditions").Where("id = ?", id).First(&rule).Error
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// CreateCreditRule 创建积分规则（含参数组合映射）
func CreateCreditRule(rule *CreditRule) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Items").Create(rule).Error; err != nil {
			return err
		}
		// 创建映射项及条件
		for i := range rule.Items {
			rule.Items[i].RuleID = rule.ID
			if err := tx.Omit("Conditions").Create(&rule.Items[i]).Error; err != nil {
				return err
			}
			for j := range rule.Items[i].Conditions {
				rule.Items[i].Conditions[j].ItemID = rule.Items[i].ID
			}
			if len(rule.Items[i].Conditions) > 0 {
				if err := tx.Create(&rule.Items[i].Conditions).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// UpdateCreditRule 更新积分规则
func UpdateCreditRule(id int, updates map[string]interface{}) error {
	return DB.Model(&CreditRule{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteCreditRule 删除积分规则（硬删除，会级联删除参数组合映射）
func DeleteCreditRule(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// 先取此项规则的所有映射项 id
		var itemIDs []int
		if err := tx.Model(&CreditRuleItem{}).Where("rule_id = ?", id).Pluck("id", &itemIDs).Error; err != nil {
			return err
		}
		// 删除映射项的条件
		if len(itemIDs) > 0 {
			if err := tx.Where("item_id IN ?", itemIDs).Delete(&CreditRuleCondition{}).Error; err != nil {
				return err
			}
		}
		// 删除映射项
		if err := tx.Where("rule_id = ?", id).Delete(&CreditRuleItem{}).Error; err != nil {
			return err
		}
		// 删除规则
		return tx.Delete(&CreditRule{}, id).Error
	})
}

// DeleteCreditRuleByModelID 删除指定模型的积分规则（硬删除，会级联删除参数组合映射）
func DeleteCreditRuleByModelID(modelID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// 先获取规则 ID
		var rule CreditRule
		if err := tx.Where("model_id = ?", modelID).First(&rule).Error; err != nil {
			return err
		}
		return DeleteCreditRule(rule.ID)
	})
}

// ReplaceCreditRuleItems 替换规则的所有参数组合映射
func ReplaceCreditRuleItems(ruleID int, items []CreditRuleItem) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// 先取旧项的 id，用于级联删除条件
		var oldIDs []int
		if err := tx.Model(&CreditRuleItem{}).Where("rule_id = ?", ruleID).Pluck("id", &oldIDs).Error; err != nil {
			return err
		}

		// 删除旧的映射项及其条件
		if len(oldIDs) > 0 {
			if err := tx.Where("item_id IN ?", oldIDs).Delete(&CreditRuleCondition{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("rule_id = ?", ruleID).Delete(&CreditRuleItem{}).Error; err != nil {
			return err
		}

		// 创建新的映射项及条件
		for i := range items {
			items[i].RuleID = ruleID
			items[i].ID = 0
			items[i].CreatedAt = time.Time{}
			items[i].UpdatedAt = time.Time{}
			if err := tx.Omit("Conditions").Create(&items[i]).Error; err != nil {
				return err
			}
			for j := range items[i].Conditions {
				items[i].Conditions[j].ItemID = items[i].ID
				items[i].Conditions[j].ID = 0
				items[i].Conditions[j].CreatedAt = time.Time{}
				items[i].Conditions[j].UpdatedAt = time.Time{}
			}
			if len(items[i].Conditions) > 0 {
				if err := tx.Create(&items[i].Conditions).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
