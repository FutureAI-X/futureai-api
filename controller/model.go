package controller

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/FutureAI-X/futureai-api/model"
	"github.com/gin-gonic/gin"
)

// ListModels 返回可用的模型列表
func ListModels(c *gin.Context) {
	models, err := model.GetModels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "Failed to get models",
				"type":    "server_error",
			},
		})
		return
	}

	// 转换为 OpenAI 兼容格式。
	// type 是 OpenAI 规范外的附加字段：调用方需要据此区分图像/视频/文本模型，
	// 纯增量字段，标准客户端会直接忽略。
	data := make([]gin.H, len(models))
	for i, m := range models {
		item := gin.H{
			"id":       m.Name,
			"object":   "model",
			"owned_by": m.Owner,
			"type":     m.Type,
		}
		if m.Type == model.ModelTypeImage {
			if strings.TrimSpace(string(m.ParamSpec)) == "" {
				item["param_spec"] = nil
			} else {
				item["param_spec"] = json.RawMessage([]byte(m.ParamSpec))
			}
		}
		data[i] = item
	}

	c.JSON(http.StatusOK, gin.H{
		"object":             "list",
		"param_spec_version": 1,
		"data":               data,
	})
}
