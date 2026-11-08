package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/gin-gonic/gin"
)

// taskDetailResponse 详情响应：在任务字段之外补上耗时。
// 用嵌入而不是往 model.Task 上加字段：耗时是展示层派生值，只属于详情接口。
type taskDetailResponse struct {
	model.Task
	DurationSeconds int64 `json:"duration_seconds"`
}

// userTaskDetailResponse 个人端任务详情：只返回用户可见字段。
//
// 刻意不嵌入 model.Task：vendor_response / query_response 包含上游任务 ID、
// 结果地址等上游信息，个人端不需要也不应该拿到。结果地址同理不在个人端展示，
// 如后续需要给用户下载成图，应返回平台自己的文件地址而不是上游地址。
type userTaskDetailResponse struct {
	ID              int       `json:"id"`
	TaskID          string    `json:"task_id"`
	Status          string    `json:"status"`
	ErrorCode       string    `json:"error_code,omitempty"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	Credits         float64   `json:"credits"`
	CreditsRefunded bool      `json:"credits_refunded"`
	RequestBody     string    `json:"request_body"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	DurationSeconds int64     `json:"duration_seconds"`
}

// taskDurationSeconds 返回任务耗时（秒）。
//
// 终态取 updated_at - created_at；进行中的任务取当前时间，否则耗时会一直停在上一次
// 状态写入的时刻（通常就是 0）。cancelled / call_fail 是历史状态值，也视为终态。
func taskDurationSeconds(t model.Task) int64 {
	end := t.UpdatedAt
	switch t.Status {
	case model.StatusCompleted, model.StatusFailed, "cancelled", "call_fail":
	default:
		end = time.Now()
	}
	seconds := int64(end.Sub(t.CreatedAt).Seconds())
	if seconds < 0 {
		return 0
	}
	return seconds
}

// AdminGetTaskLogs 管理员获取任务日志列表（支持按用户/状态筛选）
func AdminGetTaskLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")
	userID, _ := strconv.Atoi(c.Query("user_id"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	tasks, total, err := model.GetTaskLogs(page, pageSize, status, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取任务日志失败"})
		return
	}

	type taskResponse struct {
		ID              int     `json:"id"`
		TaskID          string  `json:"task_id"`
		UserID          int     `json:"user_id"`
		Username        string  `json:"username,omitempty"`
		ModelName       string  `json:"model_name,omitempty"`
		Status          string  `json:"status"`
		Credits         float64 `json:"credits"`
		CreditsRefunded bool    `json:"credits_refunded"`
		DurationSeconds int64   `json:"duration_seconds"`
		CreatedAt       string  `json:"created_at"`
		UpdatedAt       string  `json:"updated_at"`
	}

	items := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		items[i] = taskResponse{
			ID:              t.ID,
			TaskID:          t.TaskID,
			UserID:          t.UserID,
			Username:        t.Username,
			ModelName:       t.ModelName,
			Status:          t.Status,
			Credits:         t.Credits,
			CreditsRefunded: t.CreditsRefunded,
			DurationSeconds: taskDurationSeconds(t),
			CreatedAt:       common.FormatDisplayTime(t.CreatedAt),
			UpdatedAt:       common.FormatDisplayTime(t.UpdatedAt),
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"items":     items,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// AdminGetTaskLogDetail 管理员获取任务日志详情
func AdminGetTaskLogDetail(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的任务ID"})
		return
	}

	task, err := model.GetTaskByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任务不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    taskDetailResponse{Task: *task, DurationSeconds: taskDurationSeconds(*task)},
	})
}

// GetUserTaskLogs 获取当前用户的任务日志
func GetUserTaskLogs(c *gin.Context) {
	uid, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "未登录"})
		return
	}
	userID := uid.(int)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	tasks, total, err := model.GetTaskLogsByUserID(userID, page, pageSize, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取任务日志失败"})
		return
	}

	type taskResponse struct {
		ID              int     `json:"id"`
		TaskID          string  `json:"task_id"`
		ModelName       string  `json:"model_name,omitempty"`
		Status          string  `json:"status"`
		Credits         float64 `json:"credits"`
		CreditsRefunded bool    `json:"credits_refunded"`
		DurationSeconds int64   `json:"duration_seconds"`
		CreatedAt       string  `json:"created_at"`
		UpdatedAt       string  `json:"updated_at"`
	}

	items := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		items[i] = taskResponse{
			ID:              t.ID,
			TaskID:          t.TaskID,
			ModelName:       t.ModelName,
			Status:          t.Status,
			Credits:         t.Credits,
			CreditsRefunded: t.CreditsRefunded,
			DurationSeconds: taskDurationSeconds(t),
			CreatedAt:       common.FormatDisplayTime(t.CreatedAt),
			UpdatedAt:       common.FormatDisplayTime(t.UpdatedAt),
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"items":     items,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// GetUserTaskLogDetail 获取当前用户的任务详情
func GetUserTaskLogDetail(c *gin.Context) {
	uid, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "未登录"})
		return
	}
	userID := uid.(int)

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的任务ID"})
		return
	}

	task, err := model.GetTaskByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任务不存在"})
		return
	}

	// 确保只能查看自己的任务
	if task.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "无权查看此任务"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": userTaskDetailResponse{
			ID:              task.ID,
			TaskID:          task.TaskID,
			Status:          task.Status,
			ErrorCode:       task.ErrorCode,
			ErrorMessage:    task.ErrorMessage,
			Credits:         task.Credits,
			CreditsRefunded: task.CreditsRefunded,
			RequestBody:     task.RequestBody,
			CreatedAt:       task.CreatedAt,
			UpdatedAt:       task.UpdatedAt,
			DurationSeconds: taskDurationSeconds(*task),
		},
	})
}
