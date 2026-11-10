# seedream-5-5

## 1 概览

APIMart 图像模型，支持文生图与图生图。通用请求与响应格式见 [图像生成](README.md)。

模型能力如下。

| 项目 | 值 |
|---|---|
| 平台模型名 | `seedream-5-5` |
| 参考图 | 支持，最多 10 张 |
| `size` | 10 种画幅比例，默认 `16:9` |
| `resolution` | `1K`、`1.5K` 或 `2K`，默认 `1K` |
| 输出数量 | 1 张 |
| 输出格式 | PNG |
| 水印 | 固定关闭 |
| 生成方式 | 异步提交，平台轮询上游任务 |

本模型只支持图片生成，不支持图层拆分、文字 OCR、透明背景或自定义输出格式。

## 2 请求字段

通用字段的含义见 [图像生成](README.md#2-请求字段)。本模型的字段约定如下。

| 字段 | 必填 | 说明 |
|---|---|---|
| `model` | 是 | `seedream-5-5` |
| `prompt` | 是 | 提示词，去除首尾空格后不能为空，最多 2000 字符 |
| `size` | 否 | 画幅比例，如 `16:9`、`1:1`，默认 `16:9` |
| `resolution` | 否 | `1K`、`1.5K` 或 `2K`，默认 `1K` |
| `image_urls` | 否 | 参考图 URL 数组，最多 10 张 |
| `image_ids` | 否 | 参考图 file_id 数组，优先级高于 `image_urls`，最多 10 张 |

`quality`、`output_format`、`complexity`、`background`、`n`、`watermark` 不是本模型的公开参数，传入会被忽略。平台固定 `n=1`、`watermark=false`，输出格式为 PNG。

## 3 参数限制

各参数的约定如下。

| 参数 | 限制 |
|---|---|
| `size` | `1:1`、`4:3`、`3:4`、`16:9`、`9:16`、`3:2`、`2:3`、`21:9`、`2:1`、`1:2`。不支持像素尺寸、`2K` 这类档位写法和 `auto` |
| `resolution` | `1K`、`1.5K`、`2K`；平台标准值为大写，小写输入会被归一化；不支持 `4K` |
| `image_ids` | 去重后最多 10 张；单张不超过 30MB，合计不超过 300MB |
| `image_urls` | 去重后最多 10 张；上游单张上限 30MB，总像素不超过 3600 万 |
| `n` | 平台固定为 1，不接受调用方传值 |

`aspect_ratio` 不是平台字段，传入会被忽略；请使用 `size`。参考图的校验规则见 [图像生成](README.md#4-参考图)。

> 旧的 `/v1/uploads/images` 接口单文件上限是 10MB。需要上传 30MB 以内的参考图时，请使用 `/v1/files/upload/prepare` → 对象存储直传 → `/v1/files/upload/complete` 流程，再把返回的 `file_id` 放进 `image_ids`。

## 4 请求示例

文生图：

```json
{
  "model": "seedream-5-5",
  "prompt": "An orange cat sticker with clean edges",
  "size": "1:1",
  "resolution": "2K"
}
```

图生图：

```json
{
  "model": "seedream-5-5",
  "prompt": "把第一张图中的产品放到第二张图的场景里，保持产品外观",
  "size": "16:9",
  "resolution": "1.5K",
  "image_ids": ["file_1a2b3c4d5e6f", "file_2b3c4d5e6f7a"]
}
```

使用 URL 参考图：

```json
{
  "model": "seedream-5-5",
  "prompt": "保持角色外观，改为雨夜街头场景",
  "image_urls": ["https://example.com/character.png"]
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

任务完成后返回 `file_id` 与 `data.url`，格式见 [图像生成](README.md#6-响应)。上游结果默认为 PNG。

## 6 错误与重试

参数错误在扣费前返回 400，包括空提示词、不支持的 `size` / `resolution`、参考图超过 10 张或超过大小限制。

上游明确拒绝受理时返回 HTTP 200 与 `code=fail`。提交结果不确定时，任务保持 `processing`，超过 60 秒提交确认窗口仍无法确认后转为 `failed` 并退款。

任务失败时 `error.code` 可能为：

| code | 含义 |
|---|---|
| `content_policy_violation` | 内容审核未通过 |
| `invalid_parameter` | 上游参数不受支持 |
| `invalid_media` | 参考图格式或内容无效 |
| `media_unreachable` | 参考图无法访问 |
| `generation_timeout` | 生成超时 |
| `service_unavailable` | 上游服务繁忙 |
| `upstream_failed` | 上游其他失败 |
| `cancelled` | 任务被取消 |

失败任务的积分会自动退回。建议携带 `Idempotency-Key`，避免 SDK 超时重试导致重复扣费。

## 7 计费

计费公式见 [图像生成](README.md#8-计费)。上游按输出图片的实际像素分档：

- `1K` / `1.5K`：低档；
- `2K`：高档；
- 参考图按张单独计费，没有首张免费。

平台标准值仍为大写 `1K`、`1.5K`、`2K`，计费条件按 `resolution` 匹配。
