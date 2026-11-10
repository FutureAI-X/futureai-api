package imageparam

import (
	"embed"
	"fmt"
)

//go:embed seeds/*.json
var seedFS embed.FS

// SeedSpecs 返回存量模型的默认参数规格。
//
// 只映射当前在售的模型名；gpt-image-2 与 gpt-image-2-ext 共用一份规格，
// gpt-image-2.5 与 gpt-image-2.6 的 flare/sunburst 分别共用一份，
// MOSS 的 base/sft 共用一份，seedream-5-5 单独一份。调用方应只在
// param_spec 为空时写入，避免覆盖管理端的修改。
func SeedSpecs() map[string]string {
	files := map[string]string{
		"gpt-image-2":                 "seeds/gpt-image-2.json",
		"gpt-image-2-ext":             "seeds/gpt-image-2.json",
		"gpt-image-2.5-flare-ext":     "seeds/gpt-image-2.5-ext.json",
		"gpt-image-2.5-sunburst-ext":  "seeds/gpt-image-2.5-ext.json",
		"gpt-image-2.6-flare-ext":     "seeds/gpt-image-2.6-ext.json",
		"gpt-image-2.6-sunburst-ext":  "seeds/gpt-image-2.6-ext.json",
		"moss-image-base-1.0":         "seeds/moss-image.json",
		"moss-image-sft-1.0":          "seeds/moss-image.json",
		"seedream-5-5":                "seeds/seedream-5-5.json",
	}

	out := make(map[string]string, len(files))
	for modelName, file := range files {
		data, err := seedFS.ReadFile(file)
		if err != nil {
			// embed 文件缺失属于构建期错误，这里返回空串让调用方跳过并记录。
			out[modelName] = ""
			continue
		}
		out[modelName] = string(data)
	}
	return out
}

// MustSeedSpec 返回指定模型的种子规格，供测试与诊断使用。
func MustSeedSpec(modelName string) (string, error) {
	spec, ok := SeedSpecs()[modelName]
	if !ok {
		return "", fmt.Errorf("模型 %s 没有种子规格", modelName)
	}
	if spec == "" {
		return "", fmt.Errorf("模型 %s 的种子规格为空", modelName)
	}
	return spec, nil
}
