# 图像生成

`POST /v1/images/generations`

本文是图像生成的通用约定，适用于所有图像模型。模型之间的差异见文末的模型索引。

## 1 认证与限流

请求需要携带 API Key：

```bash
curl -X POST https://futureaiapi.com/v1/images/generations \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"...","prompt":"a cat"}'
```

认证方式、限流参数与常见状态码见 [客户端 API](../README.md)。

## 2 请求字段

请求体只接受下表字段。其他字段会被忽略，不会报错，也不会传给上游。

| 字段 | 类型 | 说明 |
|---|---|---|
| `model` | string | 必填，平台模型名 |
| `prompt` | string | 提示词 |
| `size` | string | 画幅。支持比例或像素尺寸，见第 3 章 |
| `resolution` | string | 分辨率档位，如 `1k` / `2k`，默认 `1k`。部分模型忽略 |
| `image_urls` | string[] | 参考图 URL 数组 |
| `image_ids` | string[] | 参考图 file_id 数组，由 `/v1/files/upload/complete` 返回 |

`size` 与 `resolution` 省略时，平台按 `size=16:9`、`resolution=1k` 补齐。使用 MOSS 模型时必须显式传 `size`，原因见第 3 章。

## 3 size 的两种格式

`size` 支持两种写法，使用哪一种由模型决定。

| 写法 | 示例 | 配合字段 | 当前模型 |
|---|---|---|---|
| 比例 | `16:9`、`1:1` | `resolution` | APIMart 模型 |
| 像素尺寸 | `1024x1024` | 无 | MOSS 模型 |

MOSS 模型要求像素尺寸，且宽高需满足模型文档中的限制。省略 `size` 时平台补齐的 `16:9` 不是合法像素尺寸，请求会在扣费前返回 400。`resolution` 只对使用比例尺寸的模型生效，MOSS 模型忽略该字段。

## 4 参考图

参考图可以使用 URL 或平台 file_id，规则如下。

- `image_ids` 非空时只使用 `image_ids`，`image_urls` 被完全忽略。
- `image_ids` 缺省或为空数组时，回退到 `image_urls`。
- 两个字段都只接受字符串数组。单个字符串会被判为类型错误并返回 400。
- 不支持参考图的模型会忽略这两个字段，不校验 file_id，也不计参考图费用。

`image_ids` 中的每一项必须是平台上传接口返回的 `file_id`，并满足以下条件：

- 属于当前 API Key 对应的用户；
- 用途为 `reference` 或 `result`；
- 处理状态为 `ready`；
- 媒体类型为图片；
- 未过期，且剩余有效期不少于 30 分钟。

校验失败不会扣费、不会创建任务。失败状态码如下：

| 状态码 | 场景 |
|---|---|
| 400 | file_id 形态错误、去重后超过 15 张、不是图片、HEIC/HEIF |
| 404 | 文件不存在、不属于当前用户、用途不符 |
| 409 | 文件尚未完成处理 |
| 410 | 文件已过期或剩余有效期不足 30 分钟 |
| 503 | 文件存储暂不可用 |

服务端把 `file_id` 解析成短期签名 URL 后交给上游，调用方不需要也不会看到签名地址。HEIC/HEIF 参考图暂不支持，请先转换为 JPG/PNG/WEBP。

`image_urls` 应传上传接口返回的 URL，不要内联 base64 data URI。请求体上限为 1MB，一张 10MB 的图 base64 之后约 13MB，会在网关层被拒绝。

## 5 请求示例

使用 file_id 参考图：

```json
{
  "model": "gpt-image-2.5-flare-ext",
  "prompt": "a cat sitting on a windowsill",
  "resolution": "2k",
  "image_ids": ["file_1a2b3c4d5e6f"]
}
```

使用 URL 参考图：

```json
{
  "model": "gpt-image-2.5-flare-ext",
  "prompt": "a cat sitting on a windowsill",
  "resolution": "2k",
  "image_urls": ["https://example.com/ref.png"]
}
```

使用像素尺寸：

```json
{
  "model": "moss-image-base-1.0",
  "prompt": "一只橘猫坐在窗边，窗外是雨后的花园，柔和的自然光",
  "size": "1024x1024"
}
```

## 6 响应

提交成功时返回任务 ID：

```json
{
  "code": "success",
  "taskId": "task_e629c5e8a37a614cf2289f650585aae4"
}
```

同步提交的供应商明确拒绝受理时返回 HTTP 200 与失败信息：

```json
{
  "code": "fail",
  "message": "供应商调用失败"
}
```

MOSS 采用后台提交，提交请求总是先返回 `taskId`。上游失败体现在任务状态中，不会出现在这个响应里。

随后使用 `GET /v1/tasks/:task_id` 轮询任务。任务完成后，图像任务返回以下字段：

```json
{
  "taskId": "task_e629c5e8a37a614cf2289f650585aae4",
  "status": "completed",
  "file_id": "file_6d41e4bf9a4bfac7b1ff97d7.png",
  "data": {
    "url": "https://futureaiapi.com/v1/images/get/file_6d41e4bf9a4bfac7b1ff97d7.png",
    "url_expires_at": "2026-11-08T12:00:00Z",
    "url_vendor": "https://upstream.example.com/generated.png"
  }
}
```

各字段含义如下。

| 字段 | 说明 |
|---|---|
| `status` | 任务状态，见第 7 章 |
| `file_id` | 成图在产品对象存储中的文件 ID；转存失败时为空 |
| `data.url` | 可直接下载的地址；有 `file_id` 时为签名地址 |
| `data.url_expires_at` | 签名地址的失效时间；过期后重新查询任务即可 |
| `data.url_vendor` | 上游供应商返回的原始地址 |

任务状态与退款规则见 [任务查询](../tasks/get-task.md#2-任务状态)。

## 7 幂等与请求体限制

提交图像生成时建议携带 `Idempotency-Key` 请求头。同一用户使用相同键重复提交只会处理一次。

```bash
curl -X POST https://futureaiapi.com/v1/images/generations \
  -H "Authorization: Bearer sk-xxx" \
  -H "Idempotency-Key: $(uuidgen)" \
  -H "Content-Type: application/json" \
  -d '{"model":"moss-image-base-1.0","prompt":"a cat","size":"1024x1024"}'
```

不携带该请求头时不去重。键最长 128 字符。完整说明见 [客户端 API](../README.md#4-幂等键)。

除 `/v1/uploads/images` 外，其余端点请求体上限为 1MB，超限返回 413，见 [客户端 API](../README.md#3-请求体大小限制)。

## 8 计费

计费公式如下：

```text
本次总积分 = 基础积分（或命中的参数组合积分）+ 参考图张数 × 每张参考图积分
```

基础积分可以为 0，表示免费模型。参数组合条件只能引用 `model`、`prompt`、`size`、`resolution`，且值必须为字符串。参考图附加计费只在供应商支持参考图时生效。

## 9 模型索引

各模型的能力与参数差异如下。

| 模型 | 参考图 | size 格式 | 文档 |
|---|---|---|---|
| `moss-image-base-1.0` | 不支持 | 像素尺寸，必填 | [moss-image-base-1.0.md](moss-image-base-1.0.md) |
| `moss-image-sft-1.0` | 不支持 | 像素尺寸，必填 | [moss-image-sft-1.0.md](moss-image-sft-1.0.md) |
| `gpt-image-2.5-flare-ext` | 支持 | 比例 + resolution | [gpt-image-2.5-flare-ext.md](gpt-image-2.5-flare-ext.md) |
| `gpt-image-2.5-sunburst-ext` | 支持 | 比例 + resolution | [gpt-image-2.5-sunburst-ext.md](gpt-image-2.5-sunburst-ext.md) |

模型以管理端实际配置为准。
