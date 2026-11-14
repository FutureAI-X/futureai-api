# MiniMax-H3-Image

`POST /v1/images/generations`

`MiniMax-H3-Image` 与 `MiniMax-H3-Image-Highspeed` 参数和限制相同，区别只在供应商侧模型 ID 与计费单价。

## 1 能力概览

| 项目 | 说明 |
|---|---|
| 尺寸 | 只支持像素尺寸 `宽x高` |
| `resolution` | 不支持，传了也会被忽略 |
| 比例 / `aspect_ratio` | 不受支持；客户端可用「比例 + 分辨率」辅助填充像素值，但请求只发送 `size` |
| 参考图 | 支持 1–16 张；有参考图时平台自动使用上游的图片编辑接口 |
| 生成数量 | 固定 1 张 |
| 输出格式 | `png` / `jpeg` / `webp`，默认 `jpeg` |
| 计费 | 按 token 三段计费：文本输入、图片输入、图片输出 |

平台对外仍然只有 `/v1/images/generations` 一个端点。调用方不需要关心文生图与图片编辑的上游差异。

## 2 请求字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `model` | 是 | `MiniMax-H3-Image` 或 `MiniMax-H3-Image-Highspeed` |
| `prompt` | 是 | 提示词，最多 4000 字符 |
| `size` | 是 | 像素尺寸，如 `2720x1536`；总像素 655,360–20,000,000，宽高比 1:3–3:1 |
| `output_format` | 否 | `png` / `jpeg` / `webp`，默认 `jpeg` |
| `image_ids` | 否 | 参考图 file_id 数组，1–16 张 |
| `image_urls` | 否 | 参考图 URL 数组，1–16 张；`image_ids` 非空时忽略 |

`quality`、`complexity`、`resolution` 不被该模型支持；请求里带这些字段会被忽略。

## 3 尺寸

只接受像素尺寸：

```json
{
  "model": "MiniMax-H3-Image",
  "prompt": "一只橘猫趴在窗台上晒太阳",
  "size": "2720x1536"
}
```

`size` 必须同时满足：

- 宽、高为正整数；
- 总像素在 655,360–20,000,000 之间；
- 宽高比在 1:3–3:1 之间。

不满足时在扣费前返回 400。

客户端界面提供「比例 + 分辨率」快捷填充（例如 2K + 16:9 → 约 `2731x1536`），用户仍可手动调整；最终提交的只有像素 `size`。

## 4 参考图与图片编辑

携带 1–16 张参考图时，平台自动调用上游的图片编辑能力；调用方仍然请求本端点。

```json
{
  "model": "MiniMax-H3-Image",
  "prompt": "把图1里的猫换成图2里的柴犬，保持背景不变",
  "size": "2720x1536",
  "image_ids": ["file_cat", "file_dog"]
}
```

参考图规则：

- 使用 `image_ids` 时只认它，`image_urls` 被忽略；
- 支持 png / jpeg / webp；HEIC/HEIF 暂不支持；
- 单张文件、总大小与有效期规则见[图像生成通用文档](README.md#4-参考图)；
- 上游不支持 `mask`，平台不会发送。

## 5 响应与任务

提交成功返回 `taskId`，随后使用 `GET /v1/tasks/:task_id` 轮询：

H3 的成图不转存平台对象存储，因此 `file_id` 为空，`data.url` 直接是上游原始地址：

```json
{
  "taskId": "task_xxx",
  "status": "completed",
  "file_id": "",
  "data": {
    "url": "https://hailuo-image...png?Expires=...&Signature=...",
    "url_vendor": "https://hailuo-image...png?Expires=...&Signature=...",
    "usage": {
      "generated_images": 1,
      "total_tokens": 7254,
      "input_tokens": 3174,
      "output_tokens": 4080,
      "input_tokens_details": { "text_tokens": 3174, "image_tokens": 0 },
      "output_tokens_details": { "image_tokens": 4080 }
    }
  }
}
```

`data.url` 是上游签名地址，**有效期 24 小时**；请在有效期内下载并自行保存。

上游最长等待 600 秒。平台采用后台提交，提交请求会先返回 `taskId`；上游失败、内容审核未通过或超时都会体现在任务状态中。

## 6 计费

计费公式：

```text
本次积分 = 文本输入 token × 文本输入单价 / 1,000,000
         + 图片输入 token × 图片输入单价 / 1,000,000
         + 图片输出 token × 图片输出单价 / 1,000,000
```

提交时按保守估算预扣，任务完成后按上游返回的实际 `usage` 多退少补。参考图成本已经包含在图片输入 token 中，不再按张额外收费。

若实际金额低于预扣，差额退还给用户；若高于预扣，差额补扣，余额可能变为负数。负余额用户在下一次提交时会被「积分不足」拦住，充值后自动恢复。
