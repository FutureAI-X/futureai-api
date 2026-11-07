package model

import (
	"encoding/json"
	"fmt"

	"github.com/FutureAI-X/futureai-api/common"
)

// migrateParamSpecPresetLabels 删除存量 pixel_size presets 里的 label 字段。
//
// 新规格只保留 {ratio, pixels}；旧规格里的 label 会被严格解析拒绝。
// 这里只做结构清理，不改变 ratio / pixels。
func migrateParamSpecPresetLabels() error {
	type row struct {
		ID        int
		ParamSpec JSONText
	}

	var rows []row
	if err := DB.Model(&Model{}).
		Select("id", "param_spec").
		Where("param_spec IS NOT NULL AND param_spec::text <> 'null'").
		Scan(&rows).Error; err != nil {
		return fmt.Errorf("读取模型参数规格失败: %w", err)
	}

	updated := 0
	for _, item := range rows {
		var spec map[string]any
		if err := json.Unmarshal([]byte(item.ParamSpec), &spec); err != nil {
			// 已损坏的规格交给后续的 ParseSpec 报错，不在迁移里静默修复。
			continue
		}

		params, _ := spec["params"].([]any)
		changed := false
		for _, rawParam := range params {
			param, _ := rawParam.(map[string]any)
			if param == nil || param["widget"] != "pixel_size" {
				continue
			}
			presets, _ := param["presets"].([]any)
			for _, rawPreset := range presets {
				preset, _ := rawPreset.(map[string]any)
				if preset == nil {
					continue
				}
				if _, ok := preset["label"]; ok {
					delete(preset, "label")
					changed = true
				}
			}
		}

		if !changed {
			continue
		}
		b, err := json.Marshal(spec)
		if err != nil {
			return fmt.Errorf("序列化模型 %d 的参数规格失败: %w", item.ID, err)
		}
		if err := DB.Exec("UPDATE models SET param_spec = ?::jsonb WHERE id = ?", string(b), item.ID).Error; err != nil {
			return fmt.Errorf("更新模型 %d 的参数规格失败: %w", item.ID, err)
		}
		updated++
	}

	if updated > 0 {
		common.SysLogf("[迁移] 已清理 %d 个模型参数规格里的 preset label", updated)
	}
	return nil
}
