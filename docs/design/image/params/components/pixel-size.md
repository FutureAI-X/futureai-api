# pixel_size

## 1 用途

像素尺寸控件。前端复用 `ratio_size` 的比例按钮组件与样式，并附宽、高两个数字输入。适用于只接受像素尺寸的模型。

## 2 JSON 设计

```json
{
  "key": "size",
  "widget": "pixel_size",
  "label": "尺寸",
  "group": "basic",
  "required": true,
  "pixel": {
    "step": 16,
    "min_side": 256,
    "max_side": 2048,
    "min_pixels": 0,
    "max_pixels": 2097152
  },
  "presets": [
    { "ratio": "1:1", "pixels": "1440x1440" },
    { "ratio": "16:9", "pixels": "1792x1008" },
    { "ratio": "9:16", "pixels": "1008x1792" }
  ],
  "allow_custom": true
}
```

## 3 字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `pixel` | object | 是 | 像素约束 |
| `pixel.step` | integer | 是 | 宽高必须同时整除该值，`0` 表示不限 |
| `pixel.min_side` | integer | 是 | 任意一边最小值，`0` 表示不限 |
| `pixel.max_side` | integer | 是 | 任意一边最大值，`0` 表示不限 |
| `pixel.min_pixels` | integer | 是 | 宽乘高的下限，`0` 表示不限 |
| `pixel.max_pixels` | integer | 是 | 宽乘高的上限，`0` 表示不限 |
| `pixel.aspect` | object | 否 | 宽高比范围，省略表示不限 |
| `presets` | object[] | 否 | 比例快捷尺寸；每项为 `{ratio, pixels}`，`ratio` 用于比例按钮，`pixels` 是该比例的默认像素值 |
| `allow_custom` | boolean | 否 | 是否允许手动修改像素值，默认 `true` |
| `default` | string | 否 | 默认像素值，必须满足 `pixel` 规则 |
| 其余通用字段 | — | — | 见 [../schema.md](../schema.md) |

`modes` 固定为 `["pixel"]` 或省略；不得配置 `ratios`。

## 4 值校验

- 像素格式 `^[1-9]\d{0,3}[xX][1-9]\d{0,3}$`；
- 宽高必须是正整数；
- `step` 同时约束宽和高；
- `min_side <= 宽/高 <= max_side`；
- `min_pixels <= 宽×高 <= max_pixels`；
- 宽高比用交叉相乘比较，边界为闭区间；
- 每个预设的 `ratio` 必填且格式合法，`pixels` 必须满足以上规则，并与 `ratio` 精确匹配；
- 预设的 `pixels` 与 `ratio` 都必须唯一；
- `allow_custom` 为 `false` 时，值必须命中某个预设；
- `required` 为 `true` 且没有 `default` 时，前端用第一个预设作为初始值。

## 5 前端组件

比例选择复用 `ratio_size` 组件与样式；像素输入仍是独立部分：

- 比例按钮来自 `presets[].ratio`，使用 `.size-groups` / `.size-btn` 等现有样式；
- 选中比例后填入该预设的 `pixels`；
- 宽、高两个数字输入保留，用户可以继续修改；修改后比例按钮不再高亮，视为自定义值；
- 实时按 `pixel` 规则校验，越界或不是 `step` 倍数时提示；
- `allow_custom` 为 `false` 时隐藏或禁用自定义输入；
- 可选提供「锁定宽高比」，修改一边时按当前比例计算另一边，并按 `step` 取整后再次校验。

## 6 服务端行为

按 `path` 读取 `size`，只接受像素值，按 `pixel` 规则校验。预设是前端元数据，服务端只做元校验，不在运行时使用。

平台不在服务端做比例与像素换算；最终提交的是可编辑的像素值。

## 7 组合与注意事项

- 规格中没有 `resolution` 时，组件不展示分辨率；不要为了计算而提交一个假的分辨率参数；
- 预设由管理端配置，客户端不内置模型尺寸；
- 只接受比例的模型使用 [ratio-size.md](ratio-size.md)；
- 同一 spec 中只能有一个 `size` 参数。
