# select

## 1 用途

下拉选择，适合选项较多、需要多选，或选项由其他参数动态决定的场景。

## 2 JSON 设计

```json
{
  "key": "quality",
  "widget": "select",
  "label": "质量",
  "group": "advanced",
  "default": "auto",
  "options": ["auto", "low", "medium", "high", "xhigh", "max"],
  "allow_empty": false
}
```

多选：

```json
{
  "key": "modalities",
  "widget": "select",
  "multiple": true,
  "options": ["image", "text"],
  "default": ["image"]
}
```

## 3 字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `options` | array | 是 | 合法取值，元素为字符串或 `{label,value}` |
| `multiple` | boolean | 否 | 是否多选，默认 `false` |
| `default` | scalar 或 array | 否 | 默认值；多选时为数组 |
| `allow_empty` | boolean | 否 | 空字符串是否合法并提交 |
| 其余通用字段 | — | — | 见 [../schema.md](../schema.md) |

## 4 值校验

- 单选：值必须在 `options` 内；
- 多选：值必须是数组，每个元素都在 `options` 内，去重后校验；
- `default` 的类型必须与 `multiple` 一致；
- 空字符串仅在 `allow_empty` 为 `true` 时合法。

## 5 前端组件

下拉选择。多选时展示为标签或复选列表。选项为对象时显示 `label`，提交 `value`。选项可以动态变化；当前值不在新选项内时回退到默认值并提示。

## 6 服务端行为

按 `path` 读取标量或数组，做选项成员校验。单选收到数组、多选收到标量时返回 `PARAM_TYPE`。

## 7 组合与注意事项

- 选项较少且互斥时优先使用 `segmented`；
- 多选顺序按 `options` 顺序归一化，避免 `["a","b"]` 与 `["b","a"]` 产生不同快照；
- 选项动态变化时，元校验需要逐分支检查 `default` 是否合法。
