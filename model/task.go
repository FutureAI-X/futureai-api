package model

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Task 任务记录
type Task struct {
	ID              int            `json:"id" gorm:"primaryKey"`
	TaskID          string         `json:"task_id" gorm:"uniqueIndex;size:64;not null"`
	UserID          int            `json:"user_id" gorm:"index;not null;default:0"` // 用户ID
	VendorID        int            `json:"vendor_id" gorm:"index;not null;default:0"`
	ModelID         int            `json:"model_id" gorm:"index;not null;default:0"`
	EndpointID      int            `json:"endpoint_id" gorm:"index;not null;default:0"`
	Status          string         `json:"status" gorm:"size:32;not null;default:'submitted'"` // submitted, completed, failed
	Credits         float64        `json:"credits" gorm:"type:numeric(20,6);default:0"`        // 消耗的积分数量
	CreditsRefunded bool           `json:"credits_refunded" gorm:"default:false"`              // 积分是否已退还
	VendorResponse  string         `json:"vendor_response" gorm:"type:text"`                   // 供应商任务提交响应 JSON
	QueryResponse   string         `json:"query_response" gorm:"type:text"`                    // 供应商任务查询响应 JSON
	RequestBody     string         `json:"request_body" gorm:"type:text"`                      // 调用方原始请求体 JSON（超长会截断），用于问题回溯
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `json:"-" gorm:"index"`

	// 非数据库字段
	Username string `json:"username,omitempty" gorm:"-"`
}

// GenerateTaskID 生成唯一任务ID
func GenerateTaskID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "task_" + hex.EncodeToString(b)
}

// CreateTask 创建任务
func CreateTask(task *Task) error {
	return DB.Create(task).Error
}

// CreateTaskAndDeduct 原子创建任务并扣除积分
// 事务内先创建任务（此时任务ID已知），再按规则扣除积分；
// 任一失败则整体回滚，避免出现「已创建但未支付」的孤儿任务。
func CreateTaskAndDeduct(task *Task, amount float64, remark string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// 先创建任务
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		// 再扣除积分（积分日志直接引用真实任务ID）
		return deductCreditsTx(tx, task.UserID, task.TaskID, amount, remark)
	})
}

// SetTaskVendorResponse 记录供应商提交响应并将任务置为已提交。
// 与 UpdateTaskStatus 的区别：这里写的是 vendor_response 字段——
// 恢复轮询（RecoverPendingTasks）依赖该字段取回供应商任务 ID，
// 若误写到 query_response，重启后任务会被判定为「缺少供应商响应」而错误退款。
func SetTaskVendorResponse(taskID string, vendorResponse string) error {
	return DB.Model(&Task{}).Where("task_id = ?", taskID).Updates(map[string]interface{}{
		"status":          "submitted",
		"vendor_response": vendorResponse,
	}).Error
}

// GetTaskByTaskID 根据任务ID获取任务
func GetTaskByTaskID(taskID string) (*Task, error) {
	var task Task
	err := DB.Where("task_id = ?", taskID).First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// UpdateTaskStatus 更新任务状态和查询响应
func UpdateTaskStatus(taskID string, status string, queryResponse string) error {
	updates := map[string]interface{}{
		"status": status,
	}
	if queryResponse != "" {
		updates["query_response"] = queryResponse
	}
	return DB.Model(&Task{}).Where("task_id = ?", taskID).Updates(updates).Error
}

// UpdateTaskStatusWithRefund 更新任务状态，如果失败则退还积分
// 使用事务 + 行级锁（SELECT ... FOR UPDATE）串行化退款决策，避免并发重复退款
func UpdateTaskStatusWithRefund(taskID string, status string, queryResponse string) error {
	// 判断是否为失败终态
	failedStatuses := []string{"failed", "cancelled", "call_fail"}
	isFailed := false
	for _, s := range failedStatuses {
		if status == s {
			isFailed = true
			break
		}
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		// 加行级锁读取任务，阻塞并发的同任务退款处理
		var task Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("task_id = ?", taskID).First(&task).Error; err != nil {
			return err
		}

		updates := map[string]interface{}{
			"status": status,
		}
		if queryResponse != "" {
			updates["query_response"] = queryResponse
		}

		// 如果任务失败且积分未退还，则退还积分并把消耗更新为0
		if isFailed && !task.CreditsRefunded && task.Credits > 0 {
			if err := refundCreditsTx(tx, task.UserID, taskID, task.Credits, "任务失败退还"); err != nil {
				return err
			}
			updates["credits_refunded"] = true
			updates["credits"] = 0 // 消耗更新为0
		}

		return tx.Model(&Task{}).Where("task_id = ?", taskID).Updates(updates).Error
	})
}

// GetPendingTasks 获取所有未完成的任务（用于启动时恢复轮询）
func GetPendingTasks() ([]Task, error) {
	var tasks []Task
	err := DB.Where("status NOT IN ?", []string{"completed", "failed", "cancelled", "call_fail"}).
		Order("id ASC").Find(&tasks).Error
	return tasks, err
}

// GetTaskLogs 获取任务日志（分页，管理员用，支持按用户/状态筛选）
func GetTaskLogs(page, pageSize int, status string, userID int) ([]Task, int64, error) {
	var tasks []Task
	var total int64

	query := DB.Model(&Task{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	if err := query.Order("id DESC").Offset(offset).Limit(pageSize).Find(&tasks).Error; err != nil {
		return nil, 0, err
	}

	if err := fillTaskUsernames(tasks); err != nil {
		return nil, 0, err
	}

	return tasks, total, nil
}

// GetTaskLogsByUserID 获取指定用户的任务日志（分页）
func GetTaskLogsByUserID(userID int, page, pageSize int, status string) ([]Task, int64, error) {
	var tasks []Task
	var total int64

	query := DB.Model(&Task{}).Where("user_id = ?", userID)
	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Order("id DESC").Offset(offset).Limit(pageSize).Find(&tasks).Error; err != nil {
		return nil, 0, err
	}

	if err := fillTaskUsernames(tasks); err != nil {
		return nil, 0, err
	}

	return tasks, total, nil
}

// fillTaskUsernames 为任务列表填充用户名
func fillTaskUsernames(tasks []Task) error {
	userMap, err := GetUsernameMap()
	if err != nil {
		return err
	}
	for i := range tasks {
		if name, ok := userMap[tasks[i].UserID]; ok {
			tasks[i].Username = name
		}
	}
	return nil
}

// GetTaskByID 根据 ID 获取任务
func GetTaskByID(id int) (*Task, error) {
	var task Task
	err := DB.Where("id = ?", id).First(&task).Error
	if err != nil {
		return nil, err
	}
	// 填充用户名
	if userMap, err := GetUsernameMap(); err == nil {
		if name, ok := userMap[task.UserID]; ok {
			task.Username = name
		}
	}
	return &task, nil
}
