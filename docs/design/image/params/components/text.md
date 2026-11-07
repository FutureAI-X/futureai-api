# text

## 1 用途

单行文本输入，适合名称、短描述等短字符串。

## 2 JSON 设计

```json
{
  "key": "short_label",
  "widget": "text",
  "label": "短文本",
  "group": "advanced",
  "max_length": 64,
  "count_mode": "codepoint",
  "allow_empty": false
}
```

## 3 字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `min_length` | integer | 否 | 最小字符数 |
| `max_length` | integer | 否 | 最大字符数 |
| `count_mode` | string | 否 | `codepoint` 或 `utf16` |
| `allow_empty` | boolean | 否 | 空字符串是否合法并提交 |
| `default` | string | 否 | 默认文本 |
| 其余通用字段 | — | — | 见 [../schema.md](../schema.md) |

## 4 值校验

与 [textarea.md](textarea.md) 一致，区别只在输入框为单行。

## 5 前端组件

单行输入框，可显示字符计数。按回车不提交表单。

## 6 服务端行为

按 `path` 读取字符串，校验长度与空值。类型不是字符串时返回 `PARAM_TYPE`。

## 7 组合与注意事项

- 需要多行输入时使用 `textarea`；
- 选项固定且数量较少时优先使用 `segmented` 或 `select`，不要用自由文本。
