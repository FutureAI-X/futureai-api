# 视频生成

`POST /v1/videos/generations`

本文是视频生成的通用约定，适用于所有视频模型。模型之间的差异见文末的模型索引。

## 1 认证与限流

请求需要携带 API Key。认证方式、限流参数与常见状态码见 [客户端 API](../README.md)。

## 2 请求字段

请求体只接受下表字段。其他字段会被忽略。`model`、`prompt`、`resolution`、`duration`、`ratio` 必填，三个媒体数组可以为空。

| 字段 | 类型 | 说明 |
|---|---|---|
| `model` | string | 平台模型名 |
| `prompt` | string | 提示词，必填 |
| `image_ids` | string[] | 参考图片的 `file_id` 数组，可空 |
| `video_ids` | string[] | 参考视频的 `file_id` 数组，可空 |
| `audio_ids` | string[] | 参考音频的 `file_id` 数组，可空 |
| `resolution` | string | 分辨率档位，如 `480P` / `1K` / `2K` |
| `duration` | integer | 输出时长（秒），必须为正整数 |
| `ratio` | string | 画幅，如 `16:9` / `adaptive` |

## 3 参考媒体

`image_ids` / `video_ids` / `audio_ids` 只接受 `/v1/files/upload/complete` 返回的 `file_id`，不接受外部 URL。数组里出现 URL 返回 400。同一个 `file_id` 在同一个数组里重复出现只处理一次。

每个参考文件必须属于调用方，并满足以下条件：

- `purpose` 为 `reference` 或 `result`；
- `status` 为 `ready`；
- 未过期，且剩余有效期不少于 30 分钟；
- 媒体类型与所在数组匹配；
- 视频参考文件有可用的时长信息。

服务端把 `file_id` 解析成最长 2 小时有效的短期签名 URL 后交给上游。签名 URL 不会返回给客户端，也不会写入任务快照。

## 4 请求示例

```json
{
  "model": "MiniMax-H3",
  "prompt": "从首帧平滑过渡到尾帧，保持参考图中角色的外观与服装一致。",
  "image_ids": ["file_..."],
  "video_ids": ["file_..."],
  "audio_ids": ["file_..."],
  "resolution": "2K",
  "duration": 5,
  "ratio": "adaptive"
}
```

## 5 响应

提交成功时返回任务 ID：

```json
{
  "code": "success",
  "taskId": "task_e629c5e8a37a614cf2289f650585aae4"
}
```

上游明确拒绝受理时返回 HTTP 200 与失败信息：

```json
{
  "code": "fail",
  "message": "供应商调用失败"
}
```

任务完成后，视频任务只返回 `taskId`、`status` 与 `data.url`：

```json
{
  "taskId": "task_e629c5e8a37a614cf2289f650585aae4",
  "status": "completed",
  "data": {
    "url": "https://files.metaso.cn/api/minimax/v2/video_generation/..."
  }
}
```

`data.url` 是上游的限时下载地址，平台不转存到对象存储。URL 过期后需要重新生成任务，也没有 `url_vendor`。失败或取消时 `data` 为 `{ "error": { "code": ..., "message": ... } }`，非终态时 `data` 为 `null`。

任务状态与退款规则见 [任务查询](../tasks/get-task.md#2-任务状态)。

## 6 幂等与请求体限制

提交视频生成时建议携带 `Idempotency-Key` 请求头。同一用户使用相同键重复提交只会处理一次，说明见 [客户端 API](../README.md#4-幂等键)。

除 `/v1/uploads/images` 外，其余端点请求体上限为 1MB，超限返回 413，见 [客户端 API](../README.md#3-请求体大小限制)。

## 7 计费

视频任务在提交时扣除积分，公式如下：

```text
总积分 = 图片计费 + 音频计费 + 参考视频计费 + 输出计费
```

- 图片：`max(0, 去重后图片数 - 免费张数) × 每张积分`；
- 音频：`去重后音频数 × 每个积分`；
- 参考视频：`Σ ceil(每个视频时长秒) × 每秒积分`，秒数逐文件向上取整；
- 输出：`duration × 每秒积分`，每秒积分随输出分辨率、是否含参考视频分档。

分辨率统一大写后匹配价目档位；不在价目表中的分辨率返回 400。任一费率填 0 表示该维度免费。

任务失败或取消时全额退还提交时扣除的积分。所有数量在提交时已确定，完成后不做二次结算。具体档位与费率以管理端配置为准，也可通过 `/api/pricing` 获取。

## 8 模型索引

各模型的文档如下。

| 模型 | 文档 |
|---|---|
| `MiniMax-H3` | [minimax-h3.md](minimax-h3.md) |

模型以管理端实际配置为准。
