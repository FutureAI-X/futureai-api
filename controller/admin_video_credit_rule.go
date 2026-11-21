package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AdminGetVideoCreditRule 获取模型的视频计费规则（不限状态，供编辑回显）。
func AdminGetVideoCreditRule(c *gin.Context) {
	modelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的模型ID"})
		return
	}

	rule, err := model.GetVideoCreditRuleByModelIDForAdmin(modelID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusOK, gin.H{"success": true, "data": nil})
			return
		}
		common.SysErrorf("[AdminGetVideoCreditRule] 查询失败: modelID=%d, err=%v", modelID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取视频计费规则失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": rule})
}

// AdminVideoCreditRateRequest 单个分辨率档位的价目。
type AdminVideoCreditRateRequest struct {
	Resolution                    string  `json:"resolution"`
	InputVideoCreditsPerSecond    float64 `json:"input_video_credits_per_second"`
	OutputCreditsPerSecondNoRef   float64 `json:"output_credits_per_second_no_ref"`
	OutputCreditsPerSecondWithRef float64 `json:"output_credits_per_second_with_ref"`
}

// AdminSaveVideoCreditRuleRequest 保存视频计费规则的请求。
//
// 采用全量替换语义：rates 会替换该规则下的全部档位。管理端保存时必须带齐
// 所有字段，避免「省略即清零」这种难以察觉的配置事故。
type AdminSaveVideoCreditRuleRequest struct {
	Description          string                        `json:"description"`
	Status               int                           `json:"status"`
	ImageCreditsPerItem  float64                       `json:"image_credits_per_item"`
	ImageFreeItems       int                           `json:"image_free_items"`
	AudioCreditsPerItem  float64                       `json:"audio_credits_per_item"`
	Rates                []AdminVideoCreditRateRequest `json:"rates"`
}

// AdminSaveVideoCreditRule 保存视频计费规则（创建或全量更新）。
func AdminSaveVideoCreditRule(c *gin.Context) {
	modelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的模型ID"})
		return
	}

	var req AdminSaveVideoCreditRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请填写完整信息"})
		return
	}

	m, err := model.GetModelByID(modelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "模型不存在"})
		return
	}
	if m.Type != model.ModelTypeVideo {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "只有视频模型可以配置视频计费规则"})
		return
	}

	status := req.Status
	if status == 0 {
		status = 1
	}
	if status != 1 && status != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "规则状态无效"})
		return
	}

	rule := &model.VideoCreditRule{
		ModelID:             modelID,
		Description:         req.Description,
		Status:              status,
		ImageCreditsPerItem: req.ImageCreditsPerItem,
		ImageFreeItems:      req.ImageFreeItems,
		AudioCreditsPerItem: req.AudioCreditsPerItem,
		Rates:               make([]model.VideoCreditRate, len(req.Rates)),
	}
	for i, rate := range req.Rates {
		rule.Rates[i] = model.VideoCreditRate{
			Resolution:                    rate.Resolution,
			InputVideoCreditsPerSecond:    rate.InputVideoCreditsPerSecond,
			OutputCreditsPerSecondNoRef:   rate.OutputCreditsPerSecondNoRef,
			OutputCreditsPerSecondWithRef: rate.OutputCreditsPerSecondWithRef,
		}
	}

	if err := model.ValidateVideoCreditRule(rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	if err := model.SaveVideoCreditRule(rule); err != nil {
		common.SysErrorf("[AdminSaveVideoCreditRule] 保存失败: modelID=%d, err=%v", modelID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存视频计费规则失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "视频计费规则保存成功", "data": rule})
}
