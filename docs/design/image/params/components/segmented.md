# segmented

## 1 用途

少量互斥选项的分段按钮，适合 output_format、resolution、moderation 等。

## 2 JSON 设计

```json
{
  "key": "resolution",
  "widget": "segmented",
  "label": "分辨率",
  "group": "basic",
  "default": "1K",
  "options": ["1K", "2K", "4K"]
}
```

`options` 也可以使用带标签的对象：

```json
{
  "options": [
    { "label": "PNG", "value": "png" },
    { "label": "JPEG", "value": "jpeg" }
  ]
}
```

## 3 字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `options` | array | 是 | 合法取值，元素为字符串或 `{label,value}` |
| `default` | scalar | 否 | 默认值，必须在 `options` 内 |
| `allow_empty` | boolean | 否 | 空字符串是否合法并提交 |
| `multiple` | boolean | 否 | 不支持；多选使用 `select` |
| 其余通用字段 | — | — | 见 [../schema.md](../schema.md) |

## 4 值校验

- 值必须在 `options` 内，否则返回 `PARAM_UNSUPPORTED_VALUE`；
- `default` 必须在 `options` 内；
- 空字符串仅在 `allow_empty` 为 `true` 时合法；
- 选项值必须唯一。

## 5 前端组件

分段按钮组，选项较少时横向排列，过多时换行。当前值高亮。`options` 为对象时显示 `label`，提交 `value`。

## 6 服务端行为

按 `path` 读取标量，做选项成员校验。类型不匹配时返回 `PARAM_TYPE`。

## 7 组合与注意事项

- 选项超过 5–6 个时改用 `select`；
- 需要多选时使用 `select` 的 `multiple`；
- 选项可以动态变化，见 [../schema.md](../schema.md) 的动态值一节。
