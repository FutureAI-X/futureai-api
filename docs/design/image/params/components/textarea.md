# textarea

## 1 用途

多行文本输入，prompt 使用。

## 2 JSON 设计

```json
{
  "key": "prompt",
  "widget": "textarea",
  "label": "提示词",
  "group": "basic",
  "required": true,
  "min_length": 1,
  "max_length": 2000,
  "count_mode": "codepoint",
  "allow_empty": false
}
```

## 3 字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `key` | string | 是 | 参数标识 |
| `path` | string | 否 | 请求字段，默认等于 `key` |
| `label` | string | 否 | 展示标签，省略时客户端按 key 取默认文案 |
| `group` | string | 否 | `basic`、`reference`、`advanced` |
| `required` | boolean | 否 | 是否必填；与 `default` 互斥 |
| `default` | string | 否 | 默认文本 |
| `min_length` | integer | 否 | 最小字符数，默认 0 |
| `max_length` | integer | 否 | 最大字符数 |
| `count_mode` | string | 否 | `codepoint`（默认）或 `utf16` |
| `allow_empty` | boolean | 否 | 空字符串是否合法并提交，默认 `false` |
| `visible_when` / `enabled_when` | condition | 否 | 显示与可用条件 |
| `validators` | string[] | 否 | 命名校验器 |

## 4 值校验

- 去除首尾空白后不能为空（当 `required` 或 `min_length` 大于 0 时）；
- 字符数按 `count_mode` 计算，边界为闭区间；
- 超过 `max_length` 返回 `PARAM_TOO_LONG`，低于 `min_length` 返回 `PARAM_TOO_SHORT`；
- 空字符串是否合法由 `allow_empty` 决定。

`codepoint` 与 `utf16` 的差异见 [../schema.md](../schema.md) 的字符计数一节。客户端计数器与服务端必须使用同一口径。

## 5 前端组件

多行输入框，右下角显示字符计数，上限来自 `max_length`。计数使用 `count_mode`：

- `codepoint`：`Array.from(value).length`；
- `utf16`：`value.length`。

超过上限时禁止提交，并高亮计数。

## 6 服务端行为

读取 `path` 对应的字符串字段，按 `count_mode` 计数并校验长度。空字符串按 `allow_empty` 处理。

## 7 组合与注意事项

- prompt 通常 `required: true`，不配置 `default`；
- 若上游对 prompt 没有明确上限，`max_length` 是平台策略，需要在模型文档中注明；
- 不使用字素簇计数。
