# switch

## 1 用途

布尔开关，适合启用/禁用某个可选能力。

## 2 JSON 设计

```json
{
  "key": "enhance",
  "widget": "switch",
  "label": "提示词增强",
  "group": "advanced",
  "default": true
}
```

## 3 字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `default` | boolean | 否 | 默认值，默认 `false` |
| 其余通用字段 | — | — | 见 [../schema.md](../schema.md) |

## 4 值校验

值必须是布尔值，否则返回 `PARAM_TYPE`。`required` 对布尔值的意义有限，通常只配置 `default`。

## 5 前端组件

开关。值为 `true` 时提交布尔 `true`，`false` 时提交布尔 `false`；不要转成字符串或数字。

## 6 服务端行为

按 `path` 读取布尔值。缺失时使用 `default`。

## 7 组合与注意事项

- 需要三个以上状态时使用 `select`；
- 空字符串与布尔 `false` 是不同值，序列化时不要做隐式转换。
