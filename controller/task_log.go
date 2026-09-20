package controller

import (
	"net/http"
	"strconv"

	"github.com/FutureAI-X/futureai-api/model"
	"github.com/gin-gonic/gin"
)

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
		Status          string  `json:"status"`
		Credits         float64 `json:"credits"`
		CreditsRefunded bool    `json:"credits_refunded"`
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
			Status:          t.Status,
			Credits:         t.Credits,
			CreditsRefunded: t.CreditsRefunded,
			CreatedAt:       t.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:       t.UpdatedAt.Format("2006-01-02 15:04:05"),
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

	c.JSON(http.StatusOK, gin.H{"success": true, "data": task})
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
		ID            int     `json:"id"`
		TaskID        string  `json:"task_id"`
		Status        string  `json:"status"`
		Credits       float64 `json:"credits"`
		CreditsRefunded bool  `json:"credits_refunded"`
		CreatedAt     string  `json:"created_at"`
		UpdatedAt     string  `json:"updated_at"`
	}

	items := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		items[i] = taskResponse{
			ID:            t.ID,
			TaskID:        t.TaskID,
			Status:        t.Status,
			Credits:       t.Credits,
			CreditsRefunded: t.CreditsRefunded,
			CreatedAt:     t.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:     t.UpdatedAt.Format("2006-01-02 15:04:05"),
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

	c.JSON(http.StatusOK, gin.H{"success": true, "data": task})
}
