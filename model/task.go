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
// 关键区分是「终态」与「非终态」，而不是「成功」与「失败」：
// 只有终态才允许退款与释放，非终态一律由对账循环继续接管。
//
// 这个区分是为了堵住一类资损：查询上游失败、轮询轮次耗尽、并发槽位占满，
// 这些都是**我方不知道结果**，不是**上游没有受理**。把它们当成失败去退款，
// 就会出现「上游照常出图并计费、用户全额退款」——平台净损失。
const (
	// StatusPending 已扣费、已建任务行，但尚未向上游提交成功
	StatusPending = "pending"
	// StatusSubmitted 上游已受理，正在轮询
	StatusSubmitted = "submitted"
	// StatusCompleted 上游已出图（终态，不退款）
	StatusCompleted = "completed"
	// StatusFailed 上游明确返回失败（终态，退款）
	StatusFailed = "failed"
	// StatusCancelled 上游明确返回取消（终态，退款）
	StatusCancelled = "cancelled"
	// StatusCallFail 上游明确拒绝受理（终态，退款）
	StatusCallFail = "call_fail"
	// StatusUnknown 结果未知——轮询轮次耗尽或查询持续失败（**非终态**，不退款）。
	// 任务会留在库里等后续对账；超过兜底时限仍无法确认时才转 call_fail 退款并告警。
	StatusUnknown = "unknown"
)

// terminalStatuses 终态列表：进入这些状态后任务不再被轮询，且不会再被改动。
var terminalStatuses = []string{StatusCompleted, StatusFailed, StatusCancelled, StatusCallFail}

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

// refundableStatuses 需要对用户退款的终态。
// 上游**明确**失败/取消/拒绝受理才退款——这三个状态的共同点是
// 「上游告诉我们它没有产出」，而不是「我们没问出结果」。
var refundableStatuses = []string{StatusFailed, StatusCancelled, StatusCallFail}

// Task 任务记录
type Task struct {
	ID         int    `json:"id" gorm:"primaryKey"`
	TaskID     string `json:"task_id" gorm:"uniqueIndex;size:64;not null"`
	// UserID 与 IdempotencyKey 组成联合唯一索引：幂等键只需在单个用户内唯一
	UserID     int    `json:"user_id" gorm:"index;uniqueIndex:idx_tasks_user_idem;not null;default:0"`
	VendorID   int    `json:"vendor_id" gorm:"index;not null;default:0"`
	ModelID    int    `json:"model_id" gorm:"index;not null;default:0"`
	EndpointID int    `json:"endpoint_id" gorm:"index;not null;default:0"`

	// Status 上建索引：对账循环每隔几十秒就要按状态筛一次，
	// 没有索引的话每次都是对最大表的顺序扫描。
	Status string `json:"status" gorm:"index;size:32;not null;default:'submitted'"`

	Credits         float64 `json:"credits" gorm:"type:numeric(20,10);default:0"` // 消耗的积分数量
	CreditsRefunded bool    `json:"credits_refunded" gorm:"default:false"`        // 积分是否已退还
	VendorResponse  string  `json:"vendor_response" gorm:"type:text"`             // 供应商任务提交响应 JSON
	QueryResponse   string  `json:"query_response" gorm:"type:text"`              // 供应商任务查询响应 JSON
	RequestBody     string  `json:"request_body" gorm:"type:text"`                // 调用方原始请求体 JSON（超长会截断），用于问题回溯

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
	Username string `json:"username,omitempty" gorm:"-"`
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

// SetTaskVendorResponse 记录供应商提交响应并将任务置为已提交。
// 与 UpdateTaskStatus 的区别：这里写的是 vendor_response 字段——
// 对账循环依赖该字段取回供应商任务 ID，
// 若误写到 query_response，任务会被判定为「缺少供应商响应」而错误退款。
//
// 带终态守卫：上游 task_id 只存在于进程内存中（提交响应里），
// 一旦被并发地把任务置成终态，这个 ID 就永久丢失了——上游照常出图，
// 而我们再也无法查询它。因此绝不允许覆盖已终结的任务。
func SetTaskVendorResponse(taskID string, vendorResponse string) error {
	return DB.Model(&Task{}).
		Where("task_id = ? AND status NOT IN ?", taskID, terminalStatuses).
		Updates(map[string]interface{}{
			"status":          StatusSubmitted,
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

// UpdateTaskStatus 更新任务状态和查询响应。
// 带终态守卫：已终结的任务不再改动。否则会出现「先退款、后被另一个轮询者
// 覆盖成 completed」的组合——用户既拿图又退款，而审计上完全看不出来。
func UpdateTaskStatus(taskID string, status string, queryResponse string) error {
	updates := map[string]interface{}{
		"status": status,
	}
	if queryResponse != "" {
		updates["query_response"] = queryResponse
	}
	return DB.Model(&Task{}).
		Where("task_id = ? AND status NOT IN ?", taskID, terminalStatuses).
		Updates(updates).Error
}

// UpdateTaskStatusWithRefund 更新任务状态，如果进入可退款终态则退还积分。
// 使用事务 + 行级锁（SELECT ... FOR UPDATE）串行化退款决策，避免并发重复退款。
//
// 注意 status 必须是 refundableStatuses 之一才会退款——这保证了
// 「不知道结果」不会被当成「失败」处理（见状态机注释）。
func UpdateTaskStatusWithRefund(taskID string, status string, queryResponse string) error {
	shouldRefund := false
	for _, s := range refundableStatuses {
		if status == s {
			shouldRefund = true
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

		// 已是终态则不再改动，避免覆盖已退款任务的状态
		if IsTerminalStatus(task.Status) {
			return nil
		}

		updates := map[string]interface{}{
			"status": status,
		}
		if queryResponse != "" {
			updates["query_response"] = queryResponse
		}

		// 如果任务失败且积分未退还，则退还积分并把消耗更新为0
		if shouldRefund && !task.CreditsRefunded && task.Credits > 0 {
			if err := refundCreditsTx(tx, task.UserID, taskID, task.Credits, "任务失败退还"); err != nil {
				return err
			}
			updates["credits_refunded"] = true
			updates["credits"] = 0 // 消耗更新为0
		}

		return tx.Model(&Task{}).Where("task_id = ?", taskID).Updates(updates).Error
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
	err := DB.Where("status NOT IN ? AND vendor_response IS NOT NULL AND vendor_response <> ''", terminalStatuses).
		Order("id ASC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// GetStalePendingTasks 取出「卡在 pending 且从未记下供应商响应」的任务。
//
// cutoff 必须显著晚于提交超时（调用方负责留出余量）：一条刚创建、
// 正在同步提交上游的任务同样是 pending 且 vendor_response 为空，
// 若把它也捞出来退款，就变成了「用户既拿图又退款」。
// 只有确定超过提交窗口仍未落库的，才是真的没提交成功。
func GetStalePendingTasks(cutoff time.Time, limit int) ([]Task, error) {
	var tasks []Task
	// 同样要覆盖 NULL：`NULL = ''` 求值为 NULL（非真），只写 = '' 会漏掉
	// vendor_response 为 NULL 的行，让它们一路漏到兜底时限才被处理。
	// 正常情况下 Go 侧写入的是空串，但历史行与人工插入的行可能是 NULL。
	err := DB.Where("status = ? AND (vendor_response IS NULL OR vendor_response = '') AND updated_at < ?",
		StatusPending, cutoff).
		Order("id ASC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// GetExpiredTasks 取出超过兜底时限仍未终结的任务。
// 这些任务已无法通过轮询确认结果，需要退款并人工对账。
func GetExpiredTasks(cutoff time.Time, limit int) ([]Task, error) {
	var tasks []Task
	err := DB.Where("status NOT IN ? AND created_at < ?", terminalStatuses, cutoff).
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
