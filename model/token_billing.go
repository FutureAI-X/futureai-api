package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ── Token 三段计费 ──
//
// per_token 规则的两段式扣费：
//   1. 提交时按预扣金额扣费（与 per_request 一样先扣后调上游）；
//   2. 完成后按上游实际 usage 结算，多退少补，差额追加一条 credit_log。
//
// 预扣金额同时写入 tasks.preauth_credits，结算后的实扣写入 tasks.settled_credits。
// 原预扣日志永远不改写：账本是追加式的，结算是一条独立流水。

// BillingUsage 归一化后的 token 用量。
type BillingUsage struct {
	TextInputTokens   int `json:"text_input_tokens"`
	ImageInputTokens  int `json:"image_input_tokens"`
	ImageOutputTokens int `json:"image_output_tokens"`
}

// Total 返回三段 token 之和；仅用于日志与展示。
func (u BillingUsage) Total() int {
	return u.TextInputTokens + u.ImageInputTokens + u.ImageOutputTokens
}

// TokenBillingConfig 控制预扣估算。字段为 0/空时使用默认值。
type TokenBillingConfig struct {
	// OutputPixelsPerToken 输出图片每多少像素折算 1 个 token。
	OutputPixelsPerToken int64 `json:"output_pixels_per_token"`

	// ResolutionPixels 分辨率档位对应的目标像素面积，用于比例尺寸的估算。
	ResolutionPixels map[string]int64 `json:"resolution_pixels"`

	// TextCharsPerToken 文本字符到 token 的粗略折算系数。
	TextCharsPerToken float64 `json:"text_chars_per_token"`

	// TextTokenOverhead 每次请求的固定文本 token 开销。
	TextTokenOverhead int `json:"text_token_overhead"`

	// TextEnhancementMultiplier Prompt 增强的保守放大系数。
	TextEnhancementMultiplier float64 `json:"text_enhancement_multiplier"`

	// RefImageTokens 每张参考图的保守 token 估算。
	RefImageTokens int `json:"ref_image_tokens"`

	// PreauthMargin 预扣安全系数；必须 >= 1。
	PreauthMargin float64 `json:"preauth_margin"`
}

// DefaultTokenBillingConfig 返回保守的默认估算参数。
//
// H3 的实测关系：输出 token 约等于像素面积 / 1024；图片编辑的 Prompt 增强
// 固定消耗上万文本 token，因此 text overhead 与 enhancement multiplier 都
// 按偏保守的方向取值，宁可在预扣阶段多扣一点，也不要让结算把余额扣穿。
func DefaultTokenBillingConfig() TokenBillingConfig {
	return TokenBillingConfig{
		OutputPixelsPerToken:       1024,
		ResolutionPixels:           map[string]int64{"1K": 1048576, "2K": 4194304, "4K": 16777216},
		TextCharsPerToken:          1,
		TextTokenOverhead:          200,
		TextEnhancementMultiplier:  3,
		RefImageTokens:             6000,
		PreauthMargin:              1.2,
	}
}

// tokenBillingConfigJSON 用于区分「字段省略」与「显式填 0」。
type tokenBillingConfigJSON struct {
	OutputPixelsPerToken      *int64            `json:"output_pixels_per_token"`
	ResolutionPixels          map[string]int64  `json:"resolution_pixels"`
	TextCharsPerToken         *float64          `json:"text_chars_per_token"`
	TextTokenOverhead         *int              `json:"text_token_overhead"`
	TextEnhancementMultiplier *float64          `json:"text_enhancement_multiplier"`
	RefImageTokens            *int              `json:"ref_image_tokens"`
	PreauthMargin             *float64          `json:"preauth_margin"`
}

// ParseTokenBillingConfig 解析 credit_rules.token_config；空值返回默认配置。
// 返回错误表示配置非法，调用方应按 fail-closed 拒绝该请求。
func ParseTokenBillingConfig(raw JSONText) (TokenBillingConfig, error) {
	cfg := DefaultTokenBillingConfig()

	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return cfg, nil
	}

	var parsed tokenBillingConfigJSON
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&parsed); err != nil {
		return cfg, fmt.Errorf("token_config 解析失败: %w", err)
	}

	if parsed.OutputPixelsPerToken != nil {
		cfg.OutputPixelsPerToken = *parsed.OutputPixelsPerToken
	}
	if len(parsed.ResolutionPixels) > 0 {
		cfg.ResolutionPixels = parsed.ResolutionPixels
	}
	if parsed.TextCharsPerToken != nil {
		cfg.TextCharsPerToken = *parsed.TextCharsPerToken
	}
	if parsed.TextTokenOverhead != nil {
		cfg.TextTokenOverhead = *parsed.TextTokenOverhead
	}
	if parsed.TextEnhancementMultiplier != nil {
		cfg.TextEnhancementMultiplier = *parsed.TextEnhancementMultiplier
	}
	if parsed.RefImageTokens != nil {
		cfg.RefImageTokens = *parsed.RefImageTokens
	}
	if parsed.PreauthMargin != nil {
		cfg.PreauthMargin = *parsed.PreauthMargin
	}

	if cfg.OutputPixelsPerToken <= 0 {
		return cfg, errors.New("token_config.output_pixels_per_token 必须大于 0")
	}
	if cfg.TextCharsPerToken <= 0 {
		return cfg, errors.New("token_config.text_chars_per_token 必须大于 0")
	}
	if cfg.TextTokenOverhead < 0 {
		return cfg, errors.New("token_config.text_token_overhead 不能为负数")
	}
	if cfg.TextEnhancementMultiplier <= 0 {
		return cfg, errors.New("token_config.text_enhancement_multiplier 必须大于 0")
	}
	if cfg.RefImageTokens < 0 {
		return cfg, errors.New("token_config.ref_image_tokens 不能为负数")
	}
	if cfg.PreauthMargin < 1 {
		return cfg, errors.New("token_config.preauth_margin 不能小于 1")
	}
	for name, area := range cfg.ResolutionPixels {
		if strings.TrimSpace(name) == "" || area <= 0 {
			return cfg, errors.New("token_config.resolution_pixels 的档位名与面积必须为正")
		}
	}
	return cfg, nil
}

// OutputTokensFor 估算一次输出所需的 token。
//
// size 为像素尺寸时按面积精确计算；为比例时回落到分辨率档位的目标面积。
// 无法识别时返回 0（调用方仍可用固定预扣兜底）。
func (c TokenBillingConfig) OutputTokensFor(size, resolution string) int {
	if w, h, ok := parseBillingPixel(size); ok {
		return int(int64(w) * int64(h) / c.OutputPixelsPerToken)
	}
	if area, ok := c.ResolutionPixels[strings.ToUpper(strings.TrimSpace(resolution))]; ok {
		return int(area / c.OutputPixelsPerToken)
	}
	return 0
}

// TextTokensFor 估算文本输入 token。enhanced 为 true 时叠加 Prompt 增强系数。
func (c TokenBillingConfig) TextTokensFor(prompt string, enhanced bool) int {
	chars := len([]rune(strings.TrimSpace(prompt)))
	tokens := int(float64(chars)*c.TextCharsPerToken) + c.TextTokenOverhead
	if enhanced {
		tokens = int(float64(tokens) * c.TextEnhancementMultiplier)
	}
	if tokens < 0 {
		return 0
	}
	return tokens
}

// parseBillingPixel 解析「宽x高」像素尺寸。
func parseBillingPixel(raw string) (int, int, bool) {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(raw)), func(r rune) bool {
		return r == 'x' || r == '×' || r == '*'
	})
	if len(parts) != 2 {
		return 0, 0, false
	}
	var w, h int
	if _, err := fmt.Sscanf(parts[0], "%d", &w); err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &h); err != nil {
		return 0, 0, false
	}
	if w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// TokenBillingSnapshot 提交时快照下来的计费参数。
//
// 结算必须按快照而不是当前规则计算：管理员在任务生成期间改价，
// 不应影响已经受理的请求。
//
// per_token 没有固定附加积分：只按三段 token 计费。base_credits 是
// per_request 规则的字段，不进入本快照。
type TokenBillingSnapshot struct {
	InputTextCreditsPerMTok   float64      `json:"input_text_credits_per_mtok"`
	ImageInputCreditsPerMTok  float64      `json:"image_input_credits_per_mtok"`
	ImageOutputCreditsPerMTok float64      `json:"image_output_credits_per_mtok"`
	PreauthCredits            float64      `json:"preauth_credits"`
	EstimatedUsage            BillingUsage `json:"estimated_usage,omitempty"`
}

// CreditsFor 按给定 usage 计算应扣积分。
func (s TokenBillingSnapshot) CreditsFor(u BillingUsage) float64 {
	amount := float64(u.TextInputTokens)*s.InputTextCreditsPerMTok/1e6 +
		float64(u.ImageInputTokens)*s.ImageInputCreditsPerMTok/1e6 +
		float64(u.ImageOutputTokens)*s.ImageOutputCreditsPerMTok/1e6
	return RoundCredits(amount)
}

// MarshalJSONText 把快照编码为可落库的 JSONText。
func (s TokenBillingSnapshot) MarshalJSONText() (JSONText, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return JSONText(data), nil
}

// ParseTokenBillingSnapshot 从任务的 billing_snapshot 解析快照。
func ParseTokenBillingSnapshot(raw JSONText) (TokenBillingSnapshot, bool, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return TokenBillingSnapshot{}, false, nil
	}
	var snapshot TokenBillingSnapshot
	if err := json.Unmarshal([]byte(text), &snapshot); err != nil {
		return TokenBillingSnapshot{}, false, fmt.Errorf("billing_snapshot 解析失败: %w", err)
	}
	return snapshot, true, nil
}

// CompleteTaskWithTokenSettlement 在同一个事务里完成 token 结算并置为 completed。
//
// 失败原因（usage 缺失等）由调用方决定：调用方应改用 CompleteTaskWithPreauth，
// 绝不能把缺失 usage 当成 0 来结算——那会把整笔预扣退给用户。
func CompleteTaskWithTokenSettlement(taskID, queryResponse, fileID string, usage BillingUsage, snapshot TokenBillingSnapshot) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("task_id = ?", taskID).First(&task).Error; err != nil {
			return err
		}
		if IsTerminalStatus(task.Status) || task.CreditsSettled {
			return nil
		}

		preauth := task.PreauthCredits
		if preauth == 0 && task.Credits > 0 {
			// 兼容没有 preauth_credits 的旧行；正常 per_token 任务必有快照与预扣。
			preauth = task.Credits
		}

		actual := snapshot.CreditsFor(usage)
		if actual < 0 {
			return errors.New("结算积分为负数")
		}
		delta := RoundCredits(actual - preauth)

		switch {
		case delta < 0:
			remark := fmt.Sprintf("Token 结算退还：文本%d/图入%d/图出%d", usage.TextInputTokens, usage.ImageInputTokens, usage.ImageOutputTokens)
			if err := refundCreditsTx(tx, task.UserID, task.TaskID, -delta, remark); err != nil {
				return err
			}
		case delta > 0:
			remark := fmt.Sprintf("Token 结算补扣：文本%d/图入%d/图出%d", usage.TextInputTokens, usage.ImageInputTokens, usage.ImageOutputTokens)
			// 允许余额变负：上游成本已经发生，债务必须如实记账。
			// 下一次提交的预扣校验（credits >= amount）会自然拦住负余额用户。
			if err := deductCreditsAllowNegativeTx(tx, task.UserID, task.TaskID, delta, remark); err != nil {
				return err
			}
		}

		updates := map[string]interface{}{
			"status":                  StatusCompleted,
			"credits":                 actual,
			"settled_credits":         actual,
			"credits_settled":         true,
			"usage_text_tokens":       usage.TextInputTokens,
			"usage_image_input_tokens": usage.ImageInputTokens,
			"usage_image_output_tokens": usage.ImageOutputTokens,
			"error_code":              "",
			"error_message":           "",
		}
		if queryResponse != "" {
			updates["query_response"] = queryResponse
		}
		if fileID != "" {
			updates["file_id"] = fileID
		}
		return tx.Model(&Task{}).Where("task_id = ?", taskID).Updates(updates).Error
	})
}

// CompleteTaskWithPreauth 在上游成功但没有 usage 时完成任务：保留预扣金额，
// 只标记已结算，避免对账循环重复尝试。usage 缺失属于上游异常，需打日志人工核对。
func CompleteTaskWithPreauth(taskID, queryResponse, fileID string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("task_id = ?", taskID).First(&task).Error; err != nil {
			return err
		}
		if IsTerminalStatus(task.Status) || task.CreditsSettled {
			return nil
		}

		preauth := task.PreauthCredits
		if preauth == 0 {
			preauth = task.Credits
		}
		updates := map[string]interface{}{
			"status":          StatusCompleted,
			"credits":         preauth,
			"settled_credits": preauth,
			"credits_settled": true,
			"error_code":      "",
			"error_message":   "",
		}
		if queryResponse != "" {
			updates["query_response"] = queryResponse
		}
		if fileID != "" {
			updates["file_id"] = fileID
		}
		return tx.Model(&Task{}).Where("task_id = ?", taskID).Updates(updates).Error
	})
}
