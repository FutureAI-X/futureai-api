package model

import (
	"time"

	"gorm.io/gorm"
)

// 积分操作类型
const (
	CreditLogTypeDeduct = "deduct" // 扣除
	CreditLogTypeRefund = "refund" // 退还
	CreditLogTypeAdjust = "adjust" // 管理员手动调整
)

// CreditLog 积分日志
type CreditLog struct {
	ID        int       `json:"id" gorm:"primaryKey"`
	UserID    int       `json:"user_id" gorm:"index;not null"`
	TaskID    string    `json:"task_id" gorm:"index;size:64"`
	Credits   float64   `json:"credits" gorm:"type:numeric(20,10);not null"` // 积分数量（正数）
	Type      string    `json:"type" gorm:"size:32;not null"`               // deduct=扣除, refund=退还
	Remark    string    `json:"remark" gorm:"size:255"`                     // 备注
	CreatedAt time.Time `json:"created_at"`

	// 非数据库字段
	Username string `json:"username,omitempty" gorm:"-"`
}

// TableName 指定表名（原为 quota_logs）
func (CreditLog) TableName() string {
	return "credit_logs"
}

// deductCreditsTx 在给定事务中扣除用户积分（同事务维护 used_credits）
func deductCreditsTx(tx *gorm.DB, userID int, taskID string, amount float64, remark string) error {
	// 账本边界：金额在这里收敛并校验，调用方忘了收敛也脏不了库
	amount, err := NormalizeDeductAmount(amount)
	if err != nil {
		return err
	}

	// 扣除用户积分并累计已用积分
	result := tx.Model(&User{}).Where("id = ? AND credits >= ?", userID, amount).
		Updates(map[string]interface{}{
			"credits":      gorm.Expr("credits - ?", amount),
			"used_credits": gorm.Expr("used_credits + ?", amount),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrInsufficientCredits
	}

	// 记录积分日志
	log := CreditLog{
		UserID:  userID,
		TaskID:  taskID,
		Credits: amount,
		Type:    CreditLogTypeDeduct,
		Remark:  remark,
	}
	return tx.Create(&log).Error
}

// refundCreditsTx 在给定事务中退还用户积分（同事务维护 used_credits）
func refundCreditsTx(tx *gorm.DB, userID int, taskID string, amount float64, remark string) error {
	// 账本边界：与扣费侧同样收敛，保证「扣多少、退多少」记的是同一个数
	amount, err := NormalizeRefundAmount(amount)
	if err != nil {
		return err
	}

	// 增加用户积分并减少已用积分（已用不足时按0计算，避免负值）
	if err := tx.Model(&User{}).Where("id = ?", userID).
		Updates(map[string]interface{}{
			"credits":      gorm.Expr("credits + ?", amount),
			"used_credits": gorm.Expr("GREATEST(used_credits - ?, 0)", amount),
		}).Error; err != nil {
		return err
	}

	// 记录积分日志
	log := CreditLog{
		UserID:  userID,
		TaskID:  taskID,
		Credits: amount,
		Type:    CreditLogTypeRefund,
		Remark:  remark,
	}
	return tx.Create(&log).Error
}

// DeductCreditsFor 为一次非任务型调用扣除积分（目前用于图片上传）。
// ref 记入 credit_logs.task_id，用于把账目回溯到具体调用。
// amount <= 0 视为免费调用，不产生任何记录。
//
// 与任务路径共用 deductCreditsTx，因此精度收敛、余额守卫、
// 「扣不成负余额」这些不变量与图像生成完全一致。
func DeductCreditsFor(userID int, ref string, amount float64, remark string) error {
	if amount <= 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		return deductCreditsTx(tx, userID, ref, amount, remark)
	})
}

// RefundCreditsFor 退还一次非任务型调用的积分。
// 调用方保证只在明确失败时调用一次（上传路径没有重试，因此无需幂等标记）。
func RefundCreditsFor(userID int, ref string, amount float64, remark string) error {
	if amount <= 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		return refundCreditsTx(tx, userID, ref, amount, remark)
	})
}

// GetCreditLogsByUserID 获取用户积分日志
func GetCreditLogsByUserID(userID int, page, pageSize int) ([]CreditLog, int64, error) {
	var logs []CreditLog
	var total int64

	query := DB.Model(&CreditLog{}).Where("user_id = ?", userID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Order("id DESC").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	if err := fillCreditLogUsernames(logs); err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// GetAllCreditLogs 获取全部用户积分日志（管理员用，支持按用户筛选）
func GetAllCreditLogs(page, pageSize int, userID int) ([]CreditLog, int64, error) {
	var logs []CreditLog
	var total int64

	query := DB.Model(&CreditLog{})
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Order("id DESC").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	if err := fillCreditLogUsernames(logs); err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// fillCreditLogUsernames 为积分日志列表填充用户名
func fillCreditLogUsernames(logs []CreditLog) error {
	userMap, err := GetUsernameMap()
	if err != nil {
		return err
	}
	for i := range logs {
		if name, ok := userMap[logs[i].UserID]; ok {
			logs[i].Username = name
		}
	}
	return nil
}

// GetCreditLogByTaskID 根据任务ID获取积分日志
func GetCreditLogByTaskID(taskID string) (*CreditLog, error) {
	var log CreditLog
	err := DB.Where("task_id = ? AND type = ?", taskID, CreditLogTypeDeduct).First(&log).Error
	if err != nil {
		return nil, err
	}
	return &log, nil
}
