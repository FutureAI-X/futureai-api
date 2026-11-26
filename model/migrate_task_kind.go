package model

import (
	"fmt"

	"github.com/FutureAI-X/futureai-api/common"
)

// videoGenerationsEndpointPath 视频生成端点的历史路径。
// 控制器里有同名常量，但 model 不能反向依赖 controller，这里保留字面量：
// 它只用于迁移历史任务，路径本身就是稳定的对外契约。
const videoGenerationsEndpointPath = "/v1/videos/generations"

// migrateTaskKindAndPolling 回填任务类型与轮询时间。
//
// 新列由 AutoMigrate 建出，历史行会拿到 kind=image 的默认值；
// 这里把挂在视频端点上的历史任务改成 video，并给所有仍在 processing 的任务
// 补一个 next_poll_at，保证对账循环能立刻接管它们。
//
// 全部语句可重复执行：已正确的行不会被改动。
func migrateTaskKindAndPolling() error {
	res := DB.Exec(`
		UPDATE tasks t
		SET kind = 'video'
		FROM endpoints e
		WHERE e.id = t.endpoint_id
		  AND e.path = ?
		  AND t.kind <> 'video'`, videoGenerationsEndpointPath)
	if res.Error != nil {
		return fmt.Errorf("回填任务类型失败: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		common.SysLogf("[迁移] 已将 %d 个历史任务标记为 video", res.RowsAffected)
	}

	res = DB.Exec(`
		UPDATE tasks
		SET next_poll_at = NOW()
		WHERE status = 'processing' AND next_poll_at IS NULL`)
	if res.Error != nil {
		return fmt.Errorf("回填任务轮询时间失败: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		common.SysLogf("[迁移] 已为 %d 个进行中任务补齐 next_poll_at", res.RowsAffected)
	}

	return nil
}
