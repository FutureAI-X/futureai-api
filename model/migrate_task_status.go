package model

import (
	"fmt"
	"strings"

	"github.com/FutureAI-X/futureai-api/common"
)

// migrateTaskStatusModel 把旧任务状态收敛为 processing / completed / failed。
//
// 规则：
//   - pending / submitting / submitted / unknown -> processing
//   - failed / call_fail / cancelled            -> failed
//   - completed                                 -> completed
//
// 退款标记复用 credits_refunded：只根据 credit_logs 里已有的 refund 记录回填，
// 迁移本身绝不调用退款逻辑，避免对历史终态重复退款。
func migrateTaskStatusModel() error {
	// 1) 已有退款日志的旧失败任务，回填 credits_refunded。
	res := DB.Exec(`
		UPDATE tasks t
		SET credits_refunded = TRUE, credits = 0
		WHERE t.status IN ('failed', 'cancelled', 'call_fail')
		  AND t.credits_refunded = FALSE
		  AND t.credits > 0
		  AND EXISTS (
		      SELECT 1 FROM credit_logs c
		      WHERE c.task_id = t.task_id AND c.type = 'refund'
		  )`)
	if res.Error != nil {
		return fmt.Errorf("回填退款标记失败: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		common.SysLogf("[迁移] 已为 %d 个历史失败任务回填退款标记", res.RowsAffected)
	}

	// 2) 旧 failed 任务补默认失败原因（可重复执行）。
	res = DB.Exec(`
		UPDATE tasks
		SET error_code = CASE WHEN error_code = '' THEN 'upstream_failed' ELSE error_code END,
		    error_message = CASE WHEN error_message = '' THEN '任务失败，积分已退回' ELSE error_message END
		WHERE status = 'failed' AND (error_code = '' OR error_message = '')`)
	if res.Error != nil {
		return fmt.Errorf("回填失败原因失败: %w", res.Error)
	}

	// 3) 旧状态映射与截止时间。
	var oldCount int64
	if err := DB.Model(&Task{}).
		Where("status IN ?", []string{"pending", "submitting", "submitted", "unknown", "cancelled", "call_fail"}).
		Count(&oldCount).Error; err != nil {
		return fmt.Errorf("统计旧状态任务失败: %w", err)
	}

	if oldCount > 0 {
		common.SysLogf("[迁移] 检测到 %d 个旧状态任务，开始收敛为三态", oldCount)

		// 旧进行中任务补截止时间。旧 submitting/unknown 按 15 分钟提交窗口处理，
		// pending/submitted 按 60 秒提交窗口处理；视频整体截止 30 分钟。
		res = DB.Exec(`
			UPDATE tasks t
			SET deadline_at = t.created_at + CASE
			        WHEN m.type = 'video' THEN INTERVAL '30 minutes'
			        ELSE INTERVAL '15 minutes'
			    END,
			    submit_deadline = t.created_at + CASE
			        WHEN t.status IN ('submitting', 'unknown') THEN INTERVAL '15 minutes'
			        ELSE INTERVAL '60 seconds'
			    END
			FROM models m
			WHERE m.id = t.model_id
			  AND t.status IN ('pending', 'submitting', 'submitted', 'unknown')
			  AND t.deadline_at IS NULL
			  AND t.submit_deadline IS NULL`)
		if res.Error != nil {
			return fmt.Errorf("回填任务截止时间失败: %w", res.Error)
		}

		// 找不到模型行的旧任务用图片默认值兜底。
		res = DB.Exec(`
			UPDATE tasks
			SET deadline_at = created_at + INTERVAL '15 minutes',
			    submit_deadline = created_at + CASE
			        WHEN status IN ('submitting', 'unknown') THEN INTERVAL '15 minutes'
			        ELSE INTERVAL '60 seconds'
			    END
			WHERE status IN ('pending', 'submitting', 'submitted', 'unknown')
			  AND deadline_at IS NULL
			  AND submit_deadline IS NULL`)
		if res.Error != nil {
			return fmt.Errorf("回填任务截止时间（兜底）失败: %w", res.Error)
		}

		// 旧终态写失败原因。
		res = DB.Exec(`
			UPDATE tasks
			SET status = 'failed',
			    error_code = CASE WHEN error_code = '' THEN 'vendor_rejected' ELSE error_code END,
			    error_message = CASE WHEN error_message = '' THEN '上游拒绝受理，积分已退回' ELSE error_message END
			WHERE status = 'call_fail'`)
		if res.Error != nil {
			return fmt.Errorf("映射 call_fail 失败: %w", res.Error)
		}

		res = DB.Exec(`
			UPDATE tasks
			SET status = 'failed',
			    error_code = CASE WHEN error_code = '' THEN 'cancelled' ELSE error_code END,
			    error_message = CASE WHEN error_message = '' THEN '任务已取消，积分已退回' ELSE error_message END
			WHERE status = 'cancelled'`)
		if res.Error != nil {
			return fmt.Errorf("映射 cancelled 失败: %w", res.Error)
		}

		// 旧进行中统一为 processing。
		res = DB.Exec(`
			UPDATE tasks
			SET status = 'processing'
			WHERE status IN ('pending', 'submitting', 'submitted', 'unknown')`)
		if res.Error != nil {
			return fmt.Errorf("映射 processing 失败: %w", res.Error)
		}

		common.SysLogf("[迁移] 旧任务状态收敛完成")
	}

	// 4) 修正 status 列默认值。只在需要时执行 DDL。
	var column struct {
		ColumnDefault *string `gorm:"column:column_default"`
	}
	if err := DB.Raw(`
		SELECT column_default
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'tasks' AND column_name = 'status'`).Scan(&column).Error; err != nil {
		return fmt.Errorf("读取 tasks.status 默认值失败: %w", err)
	}
	if column.ColumnDefault == nil || !strings.Contains(*column.ColumnDefault, "processing") {
		if err := DB.Exec("ALTER TABLE tasks ALTER COLUMN status SET DEFAULT 'processing'").Error; err != nil {
			return fmt.Errorf("修改 tasks.status 默认值失败: %w", err)
		}
	}

	// 5) 列出历史终态但找不到退款日志的任务，交人工核对，不做自动退款。
	var ambiguous int64
	if err := DB.Model(&Task{}).
		Where("status = ? AND credits_refunded = ? AND credits > 0", StatusFailed, false).
		Count(&ambiguous).Error; err != nil {
		common.SysErrorf("[迁移] 统计未退款失败任务失败: %v", err)
	} else if ambiguous > 0 {
		common.SysErrorf("[迁移] 有 %d 个 failed 任务未找到退款日志且 credits>0，请人工核对", ambiguous)
	}

	return nil
}
