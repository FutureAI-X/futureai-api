package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/gin-gonic/gin"
)

// AdminGetEndpoints 获取端点列表
func AdminGetEndpoints(c *gin.Context) {
	endpoints, err := model.AdminGetEndpoints()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取端点列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": endpoints})
}

// AdminCreateEndpointRequest 创建端点请求
type AdminCreateEndpointRequest struct {
	Path        string `json:"path" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// AdminCreateEndpoint 创建端点
func AdminCreateEndpoint(c *gin.Context) {
	var req AdminCreateEndpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请填写完整信息"})
		return
	}

	ep := model.Endpoint{
		Path:        req.Path,
		Name:        req.Name,
		Description: req.Description,
		Status:      1,
	}

	if err := model.CreateEndpoint(&ep); err != nil {
		common.SysErrorf("[AdminCreateEndpoint] 创建端点失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "创建失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "端点创建成功"})
}

// AdminUpdateEndpointRequest 更新端点请求
type AdminUpdateEndpointRequest struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AdminUpdateEndpoint 更新端点
func AdminUpdateEndpoint(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的ID"})
		return
	}

	var req AdminUpdateEndpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数无效"})
		return
	}

	updates := map[string]interface{}{}
	if req.Path != "" {
		updates["path"] = req.Path
	}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "没有需要更新的字段"})
		return
	}

	if err := model.UpdateEndpoint(id, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "更新失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "更新成功"})
}

// AdminUpdateEndpointStatus 更新端点状态
func AdminUpdateEndpointStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的ID"})
		return
	}

	var req struct {
		Status int `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "状态值无效"})
		return
	}

	if req.Status != 1 && req.Status != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "状态值必须为 1 或 2"})
		return
	}

	if err := model.UpdateEndpointStatus(id, req.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "更新状态失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "状态已更新"})
}

// AdminDeleteEndpoint 删除端点
func AdminDeleteEndpoint(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的ID"})
		return
	}

	if err := model.DeleteEndpoint(id); err != nil {
		// 仍被模型引用时返回 409 及具体提示
		if errors.Is(err, model.ErrEndpointInUse) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "删除失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已删除"})
}
