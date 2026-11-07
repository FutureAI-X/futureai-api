# number

## 1 用途

数字输入，适合 `n`、`frame_count`、`frame_duration_ms`、`output_compression` 等。

## 2 JSON 设计

```json
{
  "key": "n",
  "widget": "number",
  "label": "数量",
  "group": "advanced",
  "default": 1,
  "min": 1,
  "max": 4,
  "step": 1
}
```

## 3 字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `min` | number | 否 | 最小值，闭区间 |
| `max` | number | 否 | 最大值，闭区间 |
| `step` | number | 否 | 步长，必须大于 0 |
| `default` | number | 否 | 默认值 |
| 其余通用字段 | — | — | 见 [../schema.md](../schema.md) |

`min`、`max` 可以是动态值，按其他参数解析。

## 4 值校验

- 值必须是 JSON 数字，字符串形式的数字按 `PARAM_TYPE` 拒绝；
- `min <= value <= max`，越界返回 `PARAM_OUT_OF_RANGE`；
- `(value - min) % step === 0` 时必须整除，否则返回 `PARAM_STEP`；没有 `min` 时按 0 计算；
- `default` 必须满足同样的范围与步长。

## 5 前端组件

数字输入框，显示 `min`、`max`、`step` 提示。支持键盘上下调整。输入非法时即时提示，但不禁用其他字段。

## 6 服务端行为

按 `path` 读取数字，校验类型、范围与步长。浮点参数需要明确精度，避免 `0.1 + 0.2` 类误差；当前规格以整数为主。

## 7 组合与注意事项

- 数值参数若影响计费（如 `n`、`frame_count`），需要在计费模块配置倍数，见 [../dependencies.md](../dependencies.md)；
- `n > 1` 需要多结果存储与渲染支持，未完成前不要开放大于 1 的上限。
