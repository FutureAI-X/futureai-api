package controller

import (
	"net/http"
	"strconv"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/gin-gonic/gin"
)

// GetModelEndpoints 获取模型的端点关联列表
func GetModelEndpoints(c *gin.Context) {
	modelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的模型ID"})
		return
	}

	items, err := model.GetModelEndpoints(modelID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取端点关联失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

// SyncModelEndpoints 同步模型端点关联
func SyncModelEndpoints(c *gin.Context) {
	modelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的模型ID"})
		return
	}

	var req struct {
		EndpointIDs []int `json:"endpoint_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供端点ID列表"})
		return
	}

	if err := model.SyncModelEndpoints(modelID, req.EndpointIDs); err != nil {
		common.SysErrorf("[SyncModelEndpoints] 同步端点关联失败: modelID=%d, err=%v", modelID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "同步失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "端点关联同步成功"})
}
