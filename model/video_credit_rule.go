package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// VideoCreditRule 视频生成模型的计费规则（每个模型一条）。
//
// 与图片的 CreditRule 分开存储：视频按秒/按个计费，输出费率随分辨率分档，
// 无法用「基础积分 + 参数组合积分 + 参考图单价」准确表达。两者互斥：
// 一个模型只会按自身的 type 命中其中一张表。
type VideoCreditRule struct {
	ID int `json:"id" gorm:"primaryKey"`

	// ModelID 关联的模型 ID，唯一。
	ModelID int `json:"model_id" gorm:"uniqueIndex;not null"`

	Description string `json:"description,omitempty" gorm:"type:text"`
	Status      int    `json:"status" gorm:"not null;default:1"`

	// BaseCreditsPerRequest 每次请求固定收取的积分，与时长、分辨率、参考素材无关。
	// 0 表示不收取。用于上游按次收取的固定费用（如 H3 Context IR）。
	BaseCreditsPerRequest float64 `json:"base_credits_per_request" gorm:"type:numeric(20,10);not null;default:0"`

	// BaseCreditsDescription 基础扣费说明，展示给用户；为空则不展示。
	BaseCreditsDescription string `json:"base_credits_description,omitempty" gorm:"size:255;not null;default:''"`

	// ImageCreditsPerItem 每张参考图消耗的积分。0 表示免费。
	ImageCreditsPerItem float64 `json:"image_credits_per_item" gorm:"type:numeric(20,10);not null;default:0"`

	// ImageFreeItems 每次请求免费的前 N 张参考图。0 表示不免费。
	ImageFreeItems int `json:"image_free_items" gorm:"not null;default:0"`

	// AudioCreditsPerItem 每个参考音频消耗的积分。0 表示免费。
	AudioCreditsPerItem float64 `json:"audio_credits_per_item" gorm:"type:numeric(20,10);not null;default:0"`

	// Rates 按输出分辨率划分的价目档位。
	Rates []VideoCreditRate `json:"rates,omitempty" gorm:"foreignKey:RuleID"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (VideoCreditRule) TableName() string {
	return "video_credit_rules"
}

// VideoCreditRate 某一输出分辨率下的视频价目。
//
// 输入视频费率与输出费率都随输出分辨率变化；输出费率还区分是否包含参考视频。
type VideoCreditRate struct {
	ID int `json:"id" gorm:"primaryKey"`

	// RuleID 关联的视频计费规则 ID。
	RuleID int `json:"rule_id" gorm:"not null;uniqueIndex:idx_video_credit_rates_rule_resolution"`

	// Resolution 输出分辨率档位，统一大写（如 480P / 768P / 1080P / 2K）。
	Resolution string `json:"resolution" gorm:"size:32;not null;uniqueIndex:idx_video_credit_rates_rule_resolution"`

	// InputVideoCreditsPerSecond 每参考视频秒消耗的积分。
	InputVideoCreditsPerSecond float64 `json:"input_video_credits_per_second" gorm:"type:numeric(20,10);not null;default:0"`

	// OutputCreditsPerSecondNoRef 不含参考视频时，每秒输出消耗的积分。
	OutputCreditsPerSecondNoRef float64 `json:"output_credits_per_second_no_ref" gorm:"type:numeric(20,10);not null;default:0"`

	// OutputCreditsPerSecondWithRef 含参考视频时，每秒输出消耗的积分。
	OutputCreditsPerSecondWithRef float64 `json:"output_credits_per_second_with_ref" gorm:"type:numeric(20,10);not null;default:0"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (VideoCreditRate) TableName() string {
	return "video_credit_rates"
}

// NormalizeVideoResolution 统一输出分辨率的比较口径：去空白 + 大写。
//
// 管理端录入、请求解析与价目匹配都必须走这里，否则 480p / 480P 会被当成两个档位。
// 供应商侧的特殊写法（如某些上游要求小写或别名）留在供应商适配层转换。
func NormalizeVideoResolution(resolution string) string {
	return strings.ToUpper(strings.TrimSpace(resolution))
}

// preloadVideoCreditRates 预加载价目档位并固定顺序，保证展示与匹配稳定。
func preloadVideoCreditRates(db *gorm.DB) *gorm.DB {
	return db.Preload("Rates", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("video_credit_rates.id ASC")
	})
}

// GetVideoCreditRuleByModelID 获取模型启用的视频计费规则（含价目档位）。
// 计费是 fail-closed 的：没有启用规则时返回 gorm.ErrRecordNotFound。
func GetVideoCreditRuleByModelID(modelID int) (*VideoCreditRule, error) {
	var rule VideoCreditRule
	err := preloadVideoCreditRates(DB).
		Where("model_id = ? AND status = ?", modelID, 1).
		First(&rule).Error
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// GetVideoCreditRuleByModelIDForAdmin 获取模型的视频计费规则（不限状态），供管理端编辑。
func GetVideoCreditRuleByModelIDForAdmin(modelID int) (*VideoCreditRule, error) {
	var rule VideoCreditRule
	err := preloadVideoCreditRates(DB).
		Where("model_id = ?", modelID).
		First(&rule).Error
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// GetEnabledVideoCreditRules 获取所有启用的视频计费规则，供公开定价接口使用。
func GetEnabledVideoCreditRules() ([]VideoCreditRule, error) {
	var rules []VideoCreditRule
	err := preloadVideoCreditRates(DB).
		Where("status = ?", 1).
		Find(&rules).Error
	return rules, err
}

// FindVideoCreditRate 在规则中查找指定分辨率的档位。resolution 必须已归一化。
func FindVideoCreditRate(rule *VideoCreditRule, resolution string) *VideoCreditRate {
	if rule == nil {
		return nil
	}
	resolution = NormalizeVideoResolution(resolution)
	for i := range rule.Rates {
		if rule.Rates[i].Resolution == resolution {
			return &rule.Rates[i]
		}
	}
	return nil
}

// SupportedVideoResolutions 返回规则里已配置的分辨率列表（用于错误提示）。
func SupportedVideoResolutions(rule *VideoCreditRule) []string {
	if rule == nil {
		return nil
	}
	out := make([]string, 0, len(rule.Rates))
	for i := range rule.Rates {
		out = append(out, rule.Rates[i].Resolution)
	}
	return out
}

// VideoCreditError 是视频计费规则校验失败的错误，调用方可直接展示给管理员。
type VideoCreditError struct {
	Message string
}

func (e *VideoCreditError) Error() string { return e.Message }

func videoCreditErrorf(format string, args ...interface{}) error {
	return &VideoCreditError{Message: fmt.Sprintf(format, args...)}
}

// ValidateVideoCreditRule 校验规则本身的不变量。
//
// 只做与数据库无关的检查；模型是否存在、类型是否为 video 由控制器负责。
// 校验通过时会就地归一化分辨率。
func ValidateVideoCreditRule(rule *VideoCreditRule) error {
	if rule == nil {
		return videoCreditErrorf("视频计费规则不能为空")
	}
	if rule.ImageFreeItems < 0 {
		return videoCreditErrorf("图片免费张数不能为负数")
	}
	if len([]rune(rule.BaseCreditsDescription)) > 255 {
		return videoCreditErrorf("基础扣费说明不能超过 255 个字符")
	}
	for _, item := range []struct {
		name  string
		value float64
	}{
		{"每次请求基础积分", rule.BaseCreditsPerRequest},
		{"每张参考图积分", rule.ImageCreditsPerItem},
		{"每个参考音频积分", rule.AudioCreditsPerItem},
	} {
		if item.value < 0 {
			return videoCreditErrorf("%s不能为负数", item.name)
		}
		if !ValidCreditPrecision(item.value) {
			return videoCreditErrorf("%s最多支持%d位小数", item.name, CreditPrecision)
		}
	}

	if len(rule.Rates) == 0 {
		return videoCreditErrorf("至少配置一个输出分辨率档位")
	}

	seen := make(map[string]struct{}, len(rule.Rates))
	for i := range rule.Rates {
		rate := &rule.Rates[i]
		rate.Resolution = NormalizeVideoResolution(rate.Resolution)
		if rate.Resolution == "" {
			return videoCreditErrorf("第 %d 个分辨率档位不能为空", i+1)
		}
		if _, ok := seen[rate.Resolution]; ok {
			return videoCreditErrorf("分辨率档位 %s 重复", rate.Resolution)
		}
		seen[rate.Resolution] = struct{}{}

		for _, item := range []struct {
			name  string
			value float64
		}{
			{"参考视频每秒积分", rate.InputVideoCreditsPerSecond},
			{"无参考视频的输出每秒积分", rate.OutputCreditsPerSecondNoRef},
			{"含参考视频的输出每秒积分", rate.OutputCreditsPerSecondWithRef},
		} {
			if item.value < 0 {
				return videoCreditErrorf("%s（%s）不能为负数", item.name, rate.Resolution)
			}
			if !ValidCreditPrecision(item.value) {
				return videoCreditErrorf("%s（%s）最多支持%d位小数", item.name, rate.Resolution, CreditPrecision)
			}
		}
	}
	return nil
}

// SaveVideoCreditRule 保存视频计费规则（按 model_id 创建或全量更新），并替换全部价目档位。
func SaveVideoCreditRule(rule *VideoCreditRule) error {
	if rule == nil {
		return errors.New("视频计费规则不能为空")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var existing VideoCreditRule
		err := tx.Where("model_id = ?", rule.ModelID).First(&existing).Error
		switch {
		case err == nil:
			rule.ID = existing.ID
			if err := tx.Model(&VideoCreditRule{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{
				"description":              rule.Description,
				"status":                   rule.Status,
				"base_credits_per_request": rule.BaseCreditsPerRequest,
				"base_credits_description": rule.BaseCreditsDescription,
				"image_credits_per_item":   rule.ImageCreditsPerItem,
				"image_free_items":         rule.ImageFreeItems,
				"audio_credits_per_item":   rule.AudioCreditsPerItem,
			}).Error; err != nil {
				return err
			}
			if err := tx.Where("rule_id = ?", existing.ID).Delete(&VideoCreditRate{}).Error; err != nil {
				return err
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
			rule.ID = 0
			if err := tx.Omit("Rates").Create(rule).Error; err != nil {
				return err
			}
		default:
			return err
		}

		for i := range rule.Rates {
			rule.Rates[i].ID = 0
			rule.Rates[i].RuleID = rule.ID
			rule.Rates[i].CreatedAt = time.Time{}
			rule.Rates[i].UpdatedAt = time.Time{}
		}
		if len(rule.Rates) > 0 {
			if err := tx.Create(&rule.Rates).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
