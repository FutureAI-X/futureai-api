# ratio_size

## 1 用途

比例尺寸控件。前端只渲染比例按钮，提交比例字符串。适用于两种模型：

- 上游只接受比例；
- 上游同时接受比例与像素，但平台前端只暴露比例（mixed 模型）。

## 2 JSON 设计

只接受比例：

```json
{
  "key": "size",
  "widget": "ratio_size",
  "label": "尺寸",
  "group": "basic",
  "default": "16:9",
  "ratios": ["1:1", "16:9", "9:16", "4:3", "3:4"]
}
```

接口层同时接受像素：

```json
{
  "key": "size",
  "widget": "ratio_size",
  "label": "尺寸",
  "group": "basic",
  "default": "16:9",
  "modes": ["ratio", "pixel"],
  "ratios": ["1:1", "16:9", "9:16"],
  "pixel": {
    "step": 16,
    "min_side": 0,
    "max_side": 3840,
    "min_pixels": 0,
    "max_pixels": 8294400,
    "aspect": { "min": "1:3", "max": "3:1" }
  }
}
```

## 3 字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `ratios` | string[] | 是 | 前端展示与服务端接受的比例集合 |
| `modes` | string[] | 否 | 服务端接受的格式，默认 `["ratio"]`；mixed 写 `["ratio","pixel"]` |
| `pixel` | object | 条件 | `modes` 含 `pixel` 时必填，仅用于服务端校验像素请求 |
| `default` | string | 否 | 默认比例，必须出现在 `ratios` 中 |
| 其余通用字段 | — | — | 见 [../schema.md](../schema.md) |

`pixel` 的字段说明见 [pixel-size.md](pixel-size.md)。

## 4 值校验

- 比例格式 `^[1-9]\d{0,3}:[1-9]\d{0,3}$`；
- 比例按字符串精确匹配，`2:4` 与 `1:2` 是两个不同的值，不做约分；
- 比例必须在 `ratios` 内，否则返回 `PARAM_SIZE_RATIO_UNSUPPORTED`；
- 当前 `modes` 不接受该写法时返回 `PARAM_SIZE_MODE_UNSUPPORTED`；
- `modes` 含 `pixel` 时，像素输入按 `pixel` 规则校验；不含 `pixel` 时不得配置 `pixel`。

## 5 前端组件

比例按钮组，按 `ratios` 顺序展示。当前值高亮。即使 `modes` 包含 `pixel`，前端也只渲染比例按钮，不提供像素输入。

- 比例值原样提交，不做换算；
- 切换模型时，当前比例不在新规格内则回退到 `default`；
- `auto` 尺寸暂不开放。

## 6 服务端行为

按 `path` 读取 `size`：

- 传入比例：按 `ratios` 与 `modes` 校验；
- 传入像素：仅当 `modes` 含 `pixel` 时按 `pixel` 规则校验；否则拒绝；
- 平台不做比例与像素换算。

mixed 模型的接口层继续接受像素，是为了兼容已有调用方；平台前端只提交比例。

## 7 组合与注意事项

- 需要像素快捷选项与宽高输入时使用 [pixel-size.md](pixel-size.md)；
- `modes` 只影响服务端校验，不改变前端组件；
- 同一 spec 中只能有一个 `size` 参数。
