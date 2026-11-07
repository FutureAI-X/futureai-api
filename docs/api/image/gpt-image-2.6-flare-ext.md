# gpt-image-2.6-flare-ext

## 1 概览

APIMart 图像模型，风格版本为 flare。通用请求与响应格式见 [图像生成](README.md)。

模型能力如下。

| 项目 | 值 |
|---|---|
| 平台模型名 | `gpt-image-2.6-flare-ext` |
| 参考图 | 支持，最多 16 张 |
| `size` | 10 种画幅比例，默认 `16:9` |
| `resolution` | `1K`、`2K` 或 `4K`，默认 `1K` |
| `quality` | `auto` / `low` / `medium` / `high` / `xhigh` / `max`，默认 `auto` |
| `output_format` | `png` / `jpeg` / `webp`，默认 `png` |
| `complexity` | `low` / `medium` / `high`，默认不指定 |
| 输出数量 | 1 张 |
| 生成方式 | 异步提交，平台轮询上游任务 |

## 2 请求字段

通用字段的含义见 [图像生成](README.md#2-请求字段)。本模型的字段约定如下。

| 字段 | 必填 | 说明 |
|---|---|---|
| `model` | 是 | `gpt-image-2.6-flare-ext` |
| `prompt` | 是 | 提示词，去除首尾空格后不能为空 |
| `size` | 否 | 画幅比例，如 `16:9`、`1:1`，默认 `16:9` |
| `resolution` | 否 | `1K`、`2K` 或 `4K`，默认 `1K` |
| `quality` | 否 | 生成质量，默认 `auto` |
| `output_format` | 否 | 输出格式，默认 `png` |
| `complexity` | 否 | 画面复杂度；省略时不发送，由上游决定 |
| `image_urls` | 否 | 参考图 URL 数组，最多 16 张 |
| `image_ids` | 否 | 参考图 file_id 数组，优先级高于 `image_urls`，最多 16 张 |

## 3 参数限制

各参数的约定如下。

| 参数 | 限制 |
|---|---|
| `size` | `1:1`、`3:2`、`2:3`、`4:3`、`3:4`、`5:4`、`4:5`、`16:9`、`9:16`、`21:9`，不支持像素尺寸与 `auto` |
| `resolution` | `1K`、`2K`、`4K`，平台标准值为大写；小写输入会被归一化 |
| `quality` | `auto`、`low`、`medium`、`high`、`xhigh`、`max`；大小写不敏感 |
| `output_format` | `png`、`jpeg`、`webp`；大小写不敏感。SVG / GIF 本期不支持 |
| `complexity` | `low`、`medium`、`high`；大小写不敏感，省略表示由上游决定 |
| `image_ids` | 去重后最多 16 张 |
| `image_urls` | 去重后最多 16 张 |
| `n` | 平台固定为 1，不接受调用方传值 |

`aspect_ratio` 不是平台字段，传入会被忽略；请使用 `size`。参考图的校验规则见 [图像生成](README.md#4-参考图)。

## 4 请求示例

基础用法：

```json
{
  "model": "gpt-image-2.6-flare-ext",
  "prompt": "a cat sitting on a windowsill",
  "size": "16:9",
  "resolution": "2K"
}
```

指定质量与输出格式：

```json
{
  "model": "gpt-image-2.6-flare-ext",
  "prompt": "极简风格的产品发布会主视觉",
  "size": "16:9",
  "resolution": "4K",
  "quality": "high",
  "output_format": "webp"
}
```

指定画面复杂度并带参考图：

```json
{
  "model": "gpt-image-2.6-flare-ext",
  "prompt": "保持角色外观，改为雨夜街头场景",
  "size": "1:1",
  "resolution": "2K",
  "quality": "xhigh",
  "output_format": "jpeg",
  "complexity": "high",
  "image_ids": ["file_1a2b3c4d5e6f"]
}
```

## 5 响应

提交成功时返回：

```json
{
  "code": "success",
  "taskId": "task_e629c5e8a37a614cf2289f650585aae4"
}
```

任务完成后返回 `file_id` 与 `data.url`，格式见 [图像生成](README.md#6-响应)。`output_format` 为 `jpeg` / `webp` 时，`file_id` 分别带 `.jpg` / `.webp` 扩展名，下载时请按实际扩展名保存。

## 6 错误与重试

参数错误在扣费前返回 400，包括空提示词、不支持的 `size` / `resolution` / `quality` / `output_format` / `complexity`，以及参考图超过 16 张。`output_format=svg` 或 `gif` 本期直接返回 400。

上游明确拒绝受理时返回 HTTP 200 与 `code=fail`。提交结果不确定时，任务保持 `processing`，超过 60 秒提交确认窗口仍无法确认后转为 `failed` 并退款。

建议携带 `Idempotency-Key`，避免 SDK 超时重试导致重复扣费。

## 7 计费

计费公式见 [图像生成](README.md#8-计费)。单张价格只由 `version` × `resolution` 决定：`quality`、`output_format`、`complexity`、画幅比例和参考图张数都不影响单价。flare 与 sunburst 是两个平台模型，分别配置基础积分。

如果按分辨率差异化计价，可在计费规则中配置 `resolution` 条件。平台标准值为大写 `1K`、`2K`、`4K`；匹配时大小写不敏感，历史小写规则仍可命中。
