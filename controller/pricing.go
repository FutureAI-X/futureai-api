package controller

import (
	"net/http"

	"github.com/FutureAI-X/futureai-api/model"
	"github.com/gin-gonic/gin"
)

// GetPricing 获取模型定价信息
func GetPricing(c *gin.Context) {
	// 获取所有可用模型（带供应商信息）
	models, err := model.GetPricingModels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "获取模型列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"models": models,
		},
	})
}
