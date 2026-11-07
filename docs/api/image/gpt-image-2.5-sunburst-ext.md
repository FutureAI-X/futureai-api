# gpt-image-2.5-sunburst-ext

## 1 概览

APIMart 图像模型，风格版本为 sunburst。通用请求与响应格式见 [图像生成](README.md)。

模型能力如下。

| 项目 | 值 |
|---|---|
| 平台模型名 | `gpt-image-2.5-sunburst-ext` |
| 参考图 | 支持，最多 16 张 |
| `size` | 10 种画幅比例，默认 `16:9` |
| `resolution` | `1K`、`2K` 或 `4K`，默认 `1K` |
| 输出数量 | 1 张 |
| 生成方式 | 异步提交，平台轮询上游任务 |

上游单次最多生成 4 张，但平台当前固定按 1 张提交，不开放 `n` 参数。

## 2 请求字段

通用字段的含义见 [图像生成](README.md#2-请求字段)。本模型的字段约定如下。

| 字段 | 必填 | 说明 |
|---|---|---|
| `model` | 是 | `gpt-image-2.5-sunburst-ext` |
| `prompt` | 是 | 提示词，去除首尾空格后不能为空 |
| `size` | 否 | 画幅比例，如 `16:9`、`1:1`，默认 `16:9` |
| `resolution` | 否 | `1K`、`2K` 或 `4K`，默认 `1K` |
| `image_urls` | 否 | 参考图 URL 数组，最多 16 张 |
| `image_ids` | 否 | 参考图 file_id 数组，优先级高于 `image_urls`，最多 16 张 |

## 3 参数限制

各参数的约定如下。

| 参数 | 限制 |
|---|---|
| `size` | `1:1`、`16:9`、`9:16`、`4:3`、`3:4`、`3:2`、`2:3`、`5:4`、`4:5`、`21:9`，不支持 `auto` |
| `resolution` | `1K`、`2K`、`4K`，平台标准值为大写；小写输入会被归一化 |
| `image_ids` | 去重后最多 16 张 |
| `image_urls` | 去重后最多 16 张 |
| `n` | 平台固定为 1，不接受调用方传值 |

`size` 使用比例，不接受像素尺寸。参考图的校验规则见 [图像生成](README.md#4-参考图)。

## 4 请求示例

```json
{
  "model": "gpt-image-2.5-sunburst-ext",
  "prompt": "暖阳下的海边小屋，胶片质感",
  "size": "16:9",
  "resolution": "2K"
}
```

4K 分辨率：

```json
{
  "model": "gpt-image-2.5-sunburst-ext",
  "prompt": "极简风格的产品发布会主视觉",
  "size": "16:9",
  "resolution": "4K"
}
```

带参考图：

```json
{
  "model": "gpt-image-2.5-sunburst-ext",
  "prompt": "保留商品主体和包装文字，将背景替换为柔和的米白色摄影棚",
  "size": "1:1",
  "resolution": "2K",
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

任务完成后返回 `file_id` 与 `data.url`，格式见 [图像生成](README.md#6-响应)。

## 6 错误与重试

参数错误在扣费前返回 400，包括空提示词、不支持的 `size`、不支持的 `resolution`，以及参考图超过 16 张。

上游明确拒绝受理时返回 HTTP 200 与 `code=fail`。提交结果不确定时，任务保持 `processing`，超过 60 秒提交确认窗口仍无法确认后转为 `failed` 并退款。

建议携带 `Idempotency-Key`，避免 SDK 超时重试导致重复扣费。

## 7 计费

计费公式见 [图像生成](README.md#8-计费)。本模型按基础积分计费；如果配置了参考图单价，按实际生效字段去重后的张数叠加。

如果按分辨率差异化计价，可在计费规则中配置 `resolution` 条件。平台标准值为大写 `1K`、`2K`、`4K`；匹配时大小写不敏感，历史小写规则仍可命中。
