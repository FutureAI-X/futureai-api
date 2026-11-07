# 控件目录

每个控件独立成文，包含用途、JSON 设计、字段、校验、前端组件与服务端行为。通用字段、条件、动态值与序列化规则见 [../schema.md](../schema.md)。

## 1 控件列表

| widget | 值类型 | 用途 | 文档 |
|---|---|---|---|
| `textarea` | string | 多行文本，prompt | [textarea.md](textarea.md) |
| `text` | string | 单行文本 | [text.md](text.md) |
| `segmented` | scalar | 少量选项 | [segmented.md](segmented.md) |
| `select` | scalar 或 array | 下拉选择 | [select.md](select.md) |
| `number` | number | 数字输入 | [number.md](number.md) |
| `switch` | boolean | 开关 | [switch.md](switch.md) |
| `ratio_size` | string | 比例尺寸 | [ratio-size.md](ratio-size.md) |
| `pixel_size` | string | 像素尺寸 | [pixel-size.md](pixel-size.md) |
| `ref_images` | string[] | 参考图上传 | [ref-images.md](ref-images.md) |
| `file` | string | 单文件上传 | [file.md](file.md) |
| `hidden` | scalar | 固定默认值 | [hidden.md](hidden.md) |

## 2 选择指南

| 场景 | 控件 |
|---|---|
| 提示词、长文本 | `textarea` |
| 短文本 | `text` |
| 2–5 个互斥选项 | `segmented` |
| 选项较多或多选 | `select` |
| 数字、步长、范围 | `number` |
| 布尔开关 | `switch` |
| 模型只接受比例 | `ratio_size` |
| 模型接受比例与像素，接口层保持兼容 | `ratio_size` + `modes: ["ratio","pixel"]` |
| 模型只接受像素 | `pixel_size` |
| 参考图 | `ref_images` |
| 蒙版、标注等单文件 | `file` |
| 固定值、不展示 | `hidden` |

`auto` 尺寸暂不开放。

## 3 组件文档结构

每个控件文档按以下顺序组织：

1. 用途；
2. JSON 设计；
3. 字段；
4. 值校验；
5. 前端组件；
6. 服务端行为；
7. 组合与注意事项。

## 4 通用规则

- `required` 与 `default` 互斥；
- `visible_when` 控制显示，`enabled_when` 控制可用；
- `conflicts_with` 由客户端提示、服务端拒绝；
- 空字符串默认不发送，`allow_empty` 为 `true` 时例外；
- 跨参数数学约束使用命名校验器，不扩展条件 DSL；
- 所有参数错误在扣费前返回，错误码见 [../errors.md](../errors.md)。
