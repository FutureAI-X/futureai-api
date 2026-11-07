# 视频生成

`POST /v1/videos/generations`

本文是视频生成的通用约定，适用于所有视频模型。模型之间的差异见文末的模型索引。

## 1 认证与限流

请求需要携带 API Key。认证方式、限流参数与常见状态码见 [API.md](../../API.md)。

## 2 请求字段

请求体只接受下表字段。其他字段会被忽略。`model`、`prompt`、`resolution`、`duration`、`ratio` 必填，三个媒体数组可以为空。

| 字段 | 类型 | 说明 |
|---|---|---|
| `model` | string | 平台模型名 |
| `prompt` | string | 提示词，必填 |
| `images` | string[] | 参考图片的 `file_id` 数组，可空 |
| `videos` | string[] | 参考视频的 `file_id` 数组，可空 |
| `audios` | string[] | 参考音频的 `file_id` 数组，可空 |
| `resolution` | string | 分辨率档位，如 `480P` / `1K` / `2K` |
| `duration` | integer | 输出时长（秒），必须为正整数 |
| `ratio` | string | 画幅，如 `16:9` / `adaptive` |

## 3 参考媒体

`images` / `videos` / `audios` 只接受 `/v1/files/upload/complete` 返回的 `file_id`，不接受外部 URL。数组里出现 URL 返回 400。同一个 `file_id` 在同一个数组里重复出现只处理一次。

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
  "images": ["file_..."],
  "videos": ["file_..."],
  "audios": ["file_..."],
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

任务状态与退款规则见 [API.md](../../API.md#45-任务状态)。

## 6 幂等与请求体限制

提交视频生成时建议携带 `Idempotency-Key` 请求头。同一用户使用相同键重复提交只会处理一次，说明见 [API.md](../../API.md#44-幂等键可选)。

除 `/v1/uploads/images` 外，其余端点请求体上限为 1MB，超限返回 413，见 [API.md](../../API.md#43-请求体大小限制)。

## 7 计费

视频生成当前按 0 积分创建，不产生扣费与退款流水。

## 8 模型索引

各模型的文档如下。

| 模型 | 文档 |
|---|---|
| `MiniMax-H3` | [minimax-h3.md](minimax-h3.md) |

模型以管理端实际配置为准。
