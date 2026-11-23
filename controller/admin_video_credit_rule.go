package controller

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/FutureAI-X/futureai-api/videoparam"
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
	Description            string                        `json:"description"`
	Status                 int                           `json:"status"`
	BaseCreditsPerRequest  float64                       `json:"base_credits_per_request"`
	BaseCreditsDescription string                        `json:"base_credits_description"`
	ImageCreditsPerItem    float64                       `json:"image_credits_per_item"`
	ImageFreeItems         int                           `json:"image_free_items"`
	AudioCreditsPerItem    float64                       `json:"audio_credits_per_item"`
	Rates                  []AdminVideoCreditRateRequest `json:"rates"`
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
		ModelID:                modelID,
		Description:            req.Description,
		Status:                 status,
		BaseCreditsPerRequest:  req.BaseCreditsPerRequest,
		BaseCreditsDescription: strings.TrimSpace(req.BaseCreditsDescription),
		ImageCreditsPerItem:    req.ImageCreditsPerItem,
		ImageFreeItems:         req.ImageFreeItems,
		AudioCreditsPerItem:    req.AudioCreditsPerItem,
		Rates:                  make([]model.VideoCreditRate, len(req.Rates)),
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

	// 参数规格已经配置时，分辨率集合必须与价目表一致。规格尚未配置则先放行：
	// 管理员通常先保存规格，再配置价目；规格保存时会反向校验。
	if raw := strings.TrimSpace(string(m.ParamSpec)); raw != "" && raw != "null" {
		spec, err := videoparam.ParseSpec(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "模型参数规格无效，请先修复参数配置"})
			return
		}
		rateResolutions := make([]string, len(rule.Rates))
		for i := range rule.Rates {
			rateResolutions[i] = rule.Rates[i].Resolution
		}
		if err := compareVideoResolutionSets(spec.ResolutionOptions(), rateResolutions); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
	}

	if err := model.SaveVideoCreditRule(rule); err != nil {
		common.SysErrorf("[AdminSaveVideoCreditRule] 保存失败: modelID=%d, err=%v", modelID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存视频计费规则失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "视频计费规则保存成功", "data": rule})
}

// ensureVideoResolutionConsistency 校验参数规格与计费价目表是否覆盖同一组分辨率。
// 规则尚未配置时直接放行。
func ensureVideoResolutionConsistency(modelID int, specResolutions []string) error {
	rule, err := model.GetVideoCreditRuleByModelIDForAdmin(modelID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	rateResolutions := make([]string, len(rule.Rates))
	for i := range rule.Rates {
		rateResolutions[i] = rule.Rates[i].Resolution
	}
	return compareVideoResolutionSets(specResolutions, rateResolutions)
}

// compareVideoResolutionSets 对比两组分辨率档位（大小写不敏感）。
func compareVideoResolutionSets(specResolutions, rateResolutions []string) error {
	specSet := make(map[string]bool, len(specResolutions))
	for _, r := range specResolutions {
		specSet[videoparam.NormalizeResolution(r)] = true
	}
	rateSet := make(map[string]bool, len(rateResolutions))
	for _, r := range rateResolutions {
		rateSet[videoparam.NormalizeResolution(r)] = true
	}

	var missingRates, missingSpec []string
	for r := range specSet {
		if !rateSet[r] {
			missingRates = append(missingRates, r)
		}
	}
	for r := range rateSet {
		if !specSet[r] {
			missingSpec = append(missingSpec, r)
		}
	}
	if len(missingRates) == 0 && len(missingSpec) == 0 {
		return nil
	}

	sort.Strings(missingRates)
	sort.Strings(missingSpec)
	parts := make([]string, 0, 2)
	if len(missingRates) > 0 {
		parts = append(parts, "计费价目缺少档位："+strings.Join(missingRates, "、"))
	}
	if len(missingSpec) > 0 {
		parts = append(parts, "参数规格缺少档位："+strings.Join(missingSpec, "、"))
	}
	return fmt.Errorf("分辨率档位不一致（%s）", strings.Join(parts, "；"))
}
