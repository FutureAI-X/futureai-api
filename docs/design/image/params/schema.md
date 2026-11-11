# 规格结构

本文说明 `param_spec` 的顶层结构、参数对象、条件、动态值与序列化规则。具体控件的设计见 [components/](components/README.md)。

## 1 顶层结构

```json
{
  "params": [
    {
      "key": "prompt",
      "widget": "textarea",
      "required": true,
      "min_length": 1,
      "max_length": 2000
    }
  ]
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `params` | object[] | 是 | 参数定义，至少一项；`key` 全局唯一 |

`params` 的顺序即客户端的展示顺序。`group` 取 `basic`、`reference`、`advanced`，分组标题由客户端维护，服务端不下发页面布局。

## 2 参数对象

```json
{
  "key": "output_format",
  "path": "output_format",
  "widget": "segmented",
  "label": "输出格式",
  "hint": "",
  "group": "basic",
  "required": false,
  "default": "png",
  "visible_when": null,
  "enabled_when": null,
  "conflicts_with": [],
  "options": ["png", "jpeg", "webp"],
  "emit": null,
  "validators": []
}
```

| 字段 | 类型 | 适用范围 | 说明 |
|---|---|---|---|
| `key` | string | 全部 | 表单字段标识，`^[a-z][a-z0-9_]{0,63}$`，同一规格内唯一 |
| `path` | string | 全部 | 请求字段路径，默认等于 `key`；必须是服务端已注册的请求字段 |
| `widget` | string | 全部 | 控件类型，见第 3 节 |
| `label` | string | 全部 | 可选。标准 key 由客户端提供中文标签，服务端值仅用于覆盖 |
| `hint` | string | 全部 | 可选，展示在控件下方 |
| `group` | string | 全部 | `basic`、`reference`、`advanced`，默认 `basic` |
| `required` | boolean | 全部 | 是否必填，默认 `false`；与 `default` 互斥 |
| `default` | any | 全部 | 默认值；可以是字面量或动态值，见第 5 节 |
| `visible_when` | condition | 全部 | 不可见时保留值但不提交，见第 4 节 |
| `enabled_when` | condition | 全部 | 不可用时展示为禁用，且不提交 |
| `conflicts_with` | string[] | 全部 | 互斥的 key；双方同时有值时客户端提示，服务端拒绝 |
| `options` | array | `segmented`、`select` | 合法取值；元素可以是字符串或 `{label,value}` |
| `multiple` | boolean | `select` | 是否多选，默认 `false` |
| `allow_empty` | boolean | `text`、`textarea`、`segmented`、`select` | 空字符串是否作为合法值并提交，默认 `false` |
| `min_length` | integer | `text`、`textarea` | 最小字符数 |
| `max_length` | integer | `text`、`textarea` | 最大字符数 |
| `count_mode` | string | `text`、`textarea` | `codepoint`（默认）或 `utf16`，见第 7 节 |
| `min` | number | `number` | 最小值 |
| `max` | number | `number` | 最大值 |
| `step` | number | `number` | 步长 |
| `modes` | string[] | `ratio_size` | 服务端接受的尺寸格式，默认 `["ratio"]`；mixed 模型写 `["ratio","pixel"]` |
| `ratios` | string[] | `ratio_size` | 允许的尺寸取值：比例，或特殊值 `auto`（由上游决定输出尺寸） |
| `pixel` | object | `ratio_size`、`pixel_size` | 像素约束，见对应组件 |
| `presets` | object[] | `pixel_size` | 比例快捷尺寸，见 [pixel-size.md](components/pixel-size.md) |
| `allow_custom` | boolean | `pixel_size` | 是否允许手动修改像素值，默认 `true` |
| `max_count` | integer | `ref_images` | 参考图张数上限；`0` 表示不支持 |
| `max_file_bytes` | integer | `ref_images`、`file` | 单张上限，单位字节 |
| `max_total_bytes` | integer | `ref_images` | 本次请求合计上限，单位字节 |
| `allowed_mime_types` | string[] | `ref_images`、`file` | 允许的文件类型 |
| `emit` | object | 全部 | 序列化表达式，见第 6 节 |
| `validators` | string[] | 全部 | 命名校验器，见 [validation.md](validation.md) |

`required` 与 `default` 互斥。配置了默认值的参数不应标记为必填，否则默认值永远不会生效。

类型列描述的是解析后的类型。`default`、`options`、`modes`、`max`、`max_count`、`min`、`max_length` 可以是动态值。

## 3 控件索引

| widget | 值类型 | 用途 | 文档 |
|---|---|---|---|
| `textarea` | string | 多行文本，prompt | [textarea.md](components/textarea.md) |
| `text` | string | 单行文本 | [text.md](components/text.md) |
| `segmented` | scalar | 少量选项 | [segmented.md](components/segmented.md) |
| `select` | scalar 或 array | 下拉选择 | [select.md](components/select.md) |
| `number` | number | 数字输入 | [number.md](components/number.md) |
| `switch` | boolean | 开关 | [switch.md](components/switch.md) |
| `ratio_size` | string | 比例尺寸 | [ratio-size.md](components/ratio-size.md) |
| `pixel_size` | string | 像素尺寸 | [pixel-size.md](components/pixel-size.md) |
| `ref_images` | string[] | 参考图上传 | [ref-images.md](components/ref-images.md) |
| `file` | string | 单文件上传 | [file.md](components/file.md) |
| `hidden` | scalar | 固定默认值 | [hidden.md](components/hidden.md) |

客户端只实现上述白名单。未知控件的处理见 [client.md](client.md)。服务端在保存规格时只接受白名单内的控件。

## 4 条件

条件只做比较和逻辑组合，不做算术：

```json
{ "field": "output_format", "op": "eq", "value": "gif" }
{ "field": "output_format", "op": "in", "value": ["png", "jpeg", "webp"] }
{ "field": "images", "op": "not_empty" }
{ "not": { "field": "output_format", "op": "eq", "value": "gif" } }
{ "all": [ { "field": "a", "op": "eq", "value": 1 }, { "field": "b", "op": "truthy" } ] }
{ "any": [ { "field": "a", "op": "eq", "value": 1 }, { "field": "b", "op": "eq", "value": 2 } ] }
```

| op | 含义 |
|---|---|
| `eq` / `neq` | 等于 / 不等于 |
| `in` / `not_in` | 在 / 不在集合内 |
| `empty` / `not_empty` | 空 / 非空；空包括空串、空数组、`null` |
| `truthy` / `falsy` | 布尔真值 / 布尔假值 |

`field` 必须是同一规格中的 `key`。条件不得引用请求字段路径，也不得引用外部的 `model`、`prompt` 等未在 `params` 中声明的值。条件图不得有环，深度不超过 8，节点数不超过 64。

## 5 动态值

某些字段的取值依赖另一个参数：

```json
{
  "by": "output_format",
  "map": { "svg": 0 },
  "default": 16
}
```

规则：

- `by` 必须是同一规格中的 `key`；
- `map` 的键是被引用字段的字符串值；
- 当前值不在 `map` 中时使用 `default`；
- 动态值不得嵌套；
- 允许使用动态值的字段：`default`、`options`、`modes`、`max`、`max_count`、`min`、`max_length`；
- 动态值在运行时按依赖顺序解析一次，`by` 的引用图不得有环。

## 6 序列化

默认按当前值发送。以下场景使用 `emit`：

```json
{ "op": "value" }
{ "op": "const", "value": "1K" }
{ "op": "none" }
{ "op": "map", "map": { "a": "b" } }
{ "op": "when", "when": { "field": "output_format", "op": "eq", "value": "svg" }, "value": "1K", "else": { "op": "value" } }
```

约定：

- 默认表达式为 `{"op":"value"}`；
- 空字符串默认不发送；`allow_empty` 为 `true` 时才发送；
- `null` 与 `undefined` 不发送；
- `hidden` 控件默认发送其默认值；
- `map` 只允许映射到另一个值，不允许做算术；
- `ratio_size` 与 `pixel_size` 禁止 `emit map`，避免客户端做比例与像素换算；
- 未知 `emit` 操作符在客户端视为 `{"op":"none"}`，由服务端用默认值兜底，并产生提示。

## 7 字符计数

`count_mode` 的取值与实现方式如下：

| 取值 | 含义 | Go | TypeScript |
|---|---|---|---|
| `codepoint` | Unicode 码点，默认 | `utf8.RuneCountInString` | `Array.from(str).length` |
| `utf16` | UTF-16 code unit | `len(utf16.Encode([]rune(str)))` | `str.length` |

客户端计数器与服务端校验必须使用同一个 `count_mode`。不使用字素簇计数。

## 8 尺寸控件选择

| 场景 | 控件 |
|---|---|
| 上游只接受比例，或平台只暴露比例 | `ratio_size` |
| 上游只接受像素 | `pixel_size` |
| 上游同时接受比例与像素，接口层保持兼容 | `ratio_size` + `modes: ["ratio","pixel"]` |

`ratio_size` 的前端只渲染比例按钮；`pixel_size` 的前端复用同款比例按钮，并附宽、高两个数字输入。两者都不在服务端做比例与像素换算。

`ratio_size` 的 `ratios` 可以包含字面量 `auto`，表示输出尺寸由上游按画面内容决定。是否开放完全由模型规格配置：配置了就能用，没配置由服务端在扣费前拒绝。前端把 `auto` 和 `1:1` 一起显示在「基础」组，文案为「自动」，提交值仍是 `auto`。供应商层不做「哪些模型支持 auto」的硬编码名单。
