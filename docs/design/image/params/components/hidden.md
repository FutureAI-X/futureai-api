# hidden

## 1 用途

不渲染，但参与序列化的固定值。适合强制某个参数为常量，例如某个模型只接受 `resolution=1K`。

## 2 JSON 设计

```json
{
  "key": "resolution",
  "widget": "hidden",
  "default": "1K"
}
```

需要条件常量时使用 `emit`：

```json
{
  "key": "resolution",
  "widget": "hidden",
  "default": "1K",
  "emit": {
    "op": "when",
    "when": { "field": "output_format", "op": "eq", "value": "svg" },
    "value": "1K",
    "else": { "op": "none" }
  }
}
```

## 3 字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `default` | scalar | 是 | 固定值 |
| `emit` | object | 否 | 序列化表达式；默认发送 `default` |
| 其余通用字段 | — | — | 见 [../schema.md](../schema.md) |

## 4 值校验

- 隐藏参数使用默认值参与序列化；
- 调用方传入不同的值时报 `PARAM_DISABLED`；
- `emit` 为 `none` 时不发送该字段。

## 5 前端组件

不渲染。值由规格提供，用户不可见、不可改。

## 6 服务端行为

按 `path` 读取；缺失时使用 `default`；传入不同值时按 `PARAM_DISABLED` 拒绝。供应商适配层按正常字段处理。

## 7 组合与注意事项

- **仅用于计算的值不要声明成 hidden 并提交**。例如 MOSS 不支持 resolution，组件需要根据其他信息计算像素时，在客户端本地计算，不要为了计算虚构一个 resolution 参数；
- 需要完全省略的字段使用 `emit: {"op":"none"}`；
- 隐藏参数仍然进入任务快照，避免用 hidden 传递敏感信息。
