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

// ── 任务状态机 ──
//
// 对外只有三个状态：
//
//	processing  提交中、生成中、结果确认中
//	completed   成功
//	failed      失败且已退款；包含上游拒绝、超时、取消、内部错误
//
// 提交中/已提交/查询中不再拆成独立状态，用 vendor_task_id、
// submit_deadline、deadline_at 这些字段判断；失败原因用 error_code 表达。
// 只要任务进入 failed，就表示已经退款（credits_refunded 保证幂等）。
const (
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)

// terminalStatuses 终态列表：进入这些状态后任务不再被轮询，且不会再被改动。
var terminalStatuses = []string{StatusCompleted, StatusFailed}

// TerminalStatuses 返回终态列表的副本（供查询使用，调用方不可修改内部切片）
func TerminalStatuses() []string {
	return append([]string(nil), terminalStatuses...)
}

// IsTerminalStatus 判断是否为终态
func IsTerminalStatus(status string) bool {
	for _, s := range terminalStatuses {
		if s == status {
			return true
		}
	}
	return false
}

// Task 任务记录
type Task struct {
	ID     int    `json:"id" gorm:"primaryKey"`
	TaskID string `json:"task_id" gorm:"uniqueIndex;size:64;not null"`
	// UserID 与 IdempotencyKey 组成联合唯一索引：幂等键只需在单个用户内唯一
	UserID     int `json:"user_id" gorm:"index;uniqueIndex:idx_tasks_user_idem;not null;default:0"`
	VendorID   int `json:"vendor_id" gorm:"index;not null;default:0"`
	ModelID    int `json:"model_id" gorm:"index;not null;default:0"`
	EndpointID int `json:"endpoint_id" gorm:"index;not null;default:0"`

	// Status 上建索引：对账循环每隔几十秒就要按状态筛一次，
	// 没有索引的话每次都是对最大表的顺序扫描。
	Status string `json:"status" gorm:"index;size:32;not null;default:'processing'"`

	// SubmitDeadline 超过这个时间仍没有 vendor_response，判定提交未成功。
	SubmitDeadline *time.Time `json:"submit_deadline,omitempty" gorm:"index"`
	// DeadlineAt 任务整体截止时间；超过后仍未完成，判定失败并退款。
	DeadlineAt *time.Time `json:"deadline_at,omitempty" gorm:"index"`

	// ErrorCode / ErrorMessage 失败原因；仅 failed 状态有值。
	ErrorCode    string `json:"error_code,omitempty" gorm:"size:64;not null;default:''"`
	ErrorMessage string `json:"error_message,omitempty" gorm:"size:512;not null;default:''"`

	Credits         float64 `json:"credits" gorm:"type:numeric(20,10);default:0"` // 消耗的积分数量
	CreditsRefunded bool    `json:"credits_refunded" gorm:"default:false"`        // 积分是否已退还
	VendorResponse  string  `json:"vendor_response" gorm:"type:text"`             // 供应商任务提交响应 JSON
	QueryResponse   string  `json:"query_response" gorm:"type:text"`              // 供应商任务查询响应 JSON
	RequestBody     string  `json:"request_body" gorm:"type:text"`                // 调用方原始请求体 JSON（超长会截断），用于问题回溯

	// FileID 成图在 files 表中的文件 ID。
	// 转存失败时为空（尽力而为），原始上游 URL 仍保存在 QueryResponse 的 data.url 中。
	FileID string `json:"file_id,omitempty" gorm:"size:64;not null;default:''"`

	// IdempotencyKey 调用方通过 Idempotency-Key 请求头提供的去重键。
	// 用指针而非字符串：Postgres 的唯一索引把 NULL 视为互不相等，
	// 因此绝大多数没有带这个头的请求不会互相冲突。
	// 与 user_id 组成联合唯一索引，避免不同用户的同名键互相阻塞。
	//
	// size 必须写在 uniqueIndex 之前并用分号隔开：写成
	// "uniqueIndex:名字,size:128" 时 GORM 会把 size 当作**索引的**选项解析，
	// 列宽反而退化成 text。
	IdempotencyKey *string `json:"idempotency_key,omitempty" gorm:"size:128;uniqueIndex:idx_tasks_user_idem"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`

	// 非数据库字段
	Username  string `json:"username,omitempty" gorm:"-"`
	ModelName string `json:"model_name,omitempty" gorm:"-"`
}

// TableName 指定表名，并把幂等键的唯一性与 user_id 绑定
func (Task) TableName() string {
	return "tasks"
}

// GenerateTaskID 生成唯一任务ID
func GenerateTaskID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "task_" + hex.EncodeToString(b)
}

// GenerateUploadRef 生成图片上传调用的记账引用。
// 上传不是任务（没有上游任务状态要轮询），但同样要能在积分日志里回溯，
// 因此单独给一个前缀便于区分。
func GenerateUploadRef() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "upload_" + hex.EncodeToString(b)
}

// IsDuplicateKeyError 判断错误是否为唯一约束冲突（Postgres SQLSTATE 23505）。
//
// 用于幂等键的并发兜底：两个带同一 Idempotency-Key 的请求同时到达时，
// 两边都可能先查不到记录、再去插入，其中一个必然撞唯一索引——
// 那不是故障，而是幂等机制正在生效，调用方应回查已有任务而非报错。
//
// 用字符串匹配而非 *pgconn.PgError：GORM 未开启 TranslateError，
// 引入 pgconn 只为判一个错误码不值得，且该文案在 Postgres 各版本间稳定。
func IsDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key value violates unique constraint") ||
		strings.Contains(msg, "SQLSTATE 23505")
}

// CreateTask 创建任务
func CreateTask(task *Task) error {
	return DB.Create(task).Error
}

// CreateTaskAndDeduct 原子创建任务并扣除积分
// 事务内先创建任务（此时任务ID已知），再按规则扣除积分；
// 任一失败则整体回滚，避免出现「已创建但未支付」的孤儿任务。
func CreateTaskAndDeduct(task *Task, amount float64, remark string) error {
	// 收敛 + 校验后再写进任务行：任务行与账本必须记同一个数。
	// 退款读的是 task.Credits，两处分叉会退出一笔与实扣不同的金额。
	// 放在建任务之前，非法金额就不必先落一行再回滚。
	normalized, err := NormalizeDeductAmount(amount)
	if err != nil {
		return err
	}
	amount = normalized
	task.Credits = amount

	return DB.Transaction(func(tx *gorm.DB) error {
		// 先创建任务
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		// 再扣除积分（积分日志直接引用真实任务ID）
		return deductCreditsTx(tx, task.UserID, task.TaskID, amount, remark)
	})
}

// SetTaskVendorResponse 记录供应商提交响应。任务状态保持 processing。
// 对账循环依赖 vendor_response 取回供应商任务 ID；若误写到 query_response，
// 任务会被判定为「缺少供应商响应」而错误退款。
//
// 带终态守卫：上游 task_id 只存在于进程内存中（提交响应里），
// 一旦被并发地把任务置成终态，这个 ID 就永久丢失了——上游照常出图，
// 而我们再也无法查询它。因此绝不允许覆盖已终结的任务。
func SetTaskVendorResponse(taskID string, vendorResponse string) error {
	return DB.Model(&Task{}).
		Where("task_id = ? AND status NOT IN ?", taskID, terminalStatuses).
		Update("vendor_response", vendorResponse).Error
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

// UpdateTaskCompleted 将任务置为 completed，并记录成图的 file_id。
//
// fileID 为空表示转存失败：任务仍按完成处理（尽力而为），
// 原始上游 URL 保留在 query_response 的 data.url 中。
// 与 UpdateTaskStatus 一样带终态守卫，避免并发对账重复改写。
func UpdateTaskCompleted(taskID string, queryResponse string, fileID string) error {
	updates := map[string]interface{}{
		"status":        StatusCompleted,
		"error_code":    "",
		"error_message": "",
	}
	if queryResponse != "" {
		updates["query_response"] = queryResponse
	}
	if fileID != "" {
		updates["file_id"] = fileID
	}
	return DB.Model(&Task{}).
		Where("task_id = ? AND status NOT IN ?", taskID, terminalStatuses).
		Updates(updates).Error
}

// failTaskTx 在事务内把任务置为 failed、记录原因并退款。
// 调用方必须已经加锁读取 task 并完成终态/阶段检查。
func failTaskTx(tx *gorm.DB, task *Task, errorCode, errorMessage, queryResponse string) error {
	updates := map[string]interface{}{
		"status":        StatusFailed,
		"error_code":    errorCode,
		"error_message": errorMessage,
	}
	if queryResponse != "" {
		updates["query_response"] = queryResponse
	}
	if !task.CreditsRefunded && task.Credits > 0 {
		if err := refundCreditsTx(tx, task.UserID, task.TaskID, task.Credits, "任务失败退还"); err != nil {
			return err
		}
		updates["credits_refunded"] = true
		updates["credits"] = 0
	}
	return tx.Model(&Task{}).Where("task_id = ?", task.TaskID).Updates(updates).Error
}

// FailTask 把任务置为 failed、记录失败原因并退款。
func FailTask(taskID, errorCode, errorMessage, queryResponse string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("task_id = ?", taskID).First(&task).Error; err != nil {
			return err
		}
		if IsTerminalStatus(task.Status) {
			return nil
		}
		return failTaskTx(tx, &task, errorCode, errorMessage, queryResponse)
	})
}

// FailStaleSubmitTask 把提交阶段超时、仍没有 vendor_response 的任务置为 failed。
// 事务内重新检查 vendor_response，避免与后台提交成功写入竞争。
func FailStaleSubmitTask(taskID, errorCode, errorMessage string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("task_id = ?", taskID).First(&task).Error; err != nil {
			return err
		}
		if task.Status != StatusProcessing || strings.TrimSpace(task.VendorResponse) != "" {
			return nil
		}
		return failTaskTx(tx, &task, errorCode, errorMessage, "")
	})
}

// GetTasksAwaitingPoll 取出需要轮询的任务：非终态、且已经记下了供应商响应。
//
// 刻意带 LIMIT：tasks 是留存量最大的表且带三个 TEXT 列，
// 无上限的 Find 会在异常堆积时把整表读进内存。
func GetTasksAwaitingPoll(limit int) ([]Task, error) {
	var tasks []Task
	// vendor_response 必须显式排除 NULL：该列可空，历史行或人工插入的行可能是
	// NULL 而不是空串，而 `NULL <> ''` 在 SQL 里求值为 NULL（非真）——
	// 只写 <> '' 的话这类行会被静默漏掉。
	err := DB.Where("status = ? AND vendor_response IS NOT NULL AND vendor_response <> ''", StatusProcessing).
		Order("id ASC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// GetStaleSubmitTasks 取出提交阶段超时、仍没有供应商响应的任务。
// now 由调用方传入，便于测试和批量对账使用同一时间基准。
func GetStaleSubmitTasks(now time.Time, limit int) ([]Task, error) {
	var tasks []Task
	err := DB.Where("status = ? AND (vendor_response IS NULL OR vendor_response = '') AND submit_deadline IS NOT NULL AND submit_deadline <= ?",
		StatusProcessing, now).
		Order("id ASC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// GetExpiredTasks 取出超过整体截止时间仍未终结的任务。
func GetExpiredTasks(now time.Time, limit int) ([]Task, error) {
	var tasks []Task
	err := DB.Where("status = ? AND deadline_at IS NOT NULL AND deadline_at <= ?", StatusProcessing, now).
		Order("id ASC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// GetTaskByUserAndIdempotencyKey 按幂等键查找用户已有的任务
func GetTaskByUserAndIdempotencyKey(userID int, key string) (*Task, error) {
	var task Task
	err := DB.Where("user_id = ? AND idempotency_key = ?", userID, key).First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
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
	if err := fillTaskModelNames(tasks); err != nil {
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
	if err := fillTaskModelNames(tasks); err != nil {
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

// fillTaskModelNames 为任务列表填充模型名。
// 模型被删除时名称为空，由前端显示占位符。
func fillTaskModelNames(tasks []Task) error {
	modelMap, err := GetModelNameMap()
	if err != nil {
		return err
	}
	for i := range tasks {
		tasks[i].ModelName = modelMap[tasks[i].ModelID]
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
