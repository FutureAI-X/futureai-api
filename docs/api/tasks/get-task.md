# 查询任务

`GET /v1/tasks/:task_id`

查询本人提交的图像或视频任务。请求需要 `Authorization: Bearer sk-...`，只能查询当前 API Key 所属用户的任务。查询他人任务返回 `403`。

## 1 响应

图像任务完成时返回 `file_id` 与结果地址：

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

视频任务完成时只返回 `taskId`、`status` 与 `data.url`：

```json
{
  "taskId": "task_e629c5e8a37a614cf2289f650585aae4",
  "status": "completed",
  "data": {
    "url": "https://files.metaso.cn/api/minimax/v2/video_generation/..."
  }
}
```

失败或取消的视频任务，`data` 为 `{ "error": { "code": ..., "message": ... } }`。非终态时 `data` 为 `null`。

## 2 任务状态

`status` 的取值如下。

| 状态 | 终态 | 含义 | 积分 |
|---|---|---|---|
| `pending` | 否 | 已扣费，正在提交或生成 | 已扣 |
| `submitted` | 否 | 上游已受理，正在生成 | 已扣 |
| `unknown` | 否 | 结果无法确认 | 已扣 |
| `completed` | 是 | 生成完成，结果在 `data` 里 | 已扣 |
| `failed` | 是 | 上游明确返回失败 | 已退 |
| `cancelled` | 是 | 上游明确返回取消 | 已退 |
| `call_fail` | 是 | 上游拒绝受理、提交未成功或超期无法确认 | 已退 |

只有 `completed` 时 `data` 才有值。非终态表示任务仍在推进，应继续轮询，建议间隔 5 秒左右。视频生成任务当前按 0 积分创建，积分列对它不适用。

MOSS 是同步接口，由平台后台提交，任务在生成期间显示为 `pending`。任何失败都会直接转为 `call_fail` 并退款，不会进入 `unknown`。

## 3 结果字段

图像任务的 `data` 字段如下。

| 字段 | 说明 |
|---|---|
| `data.url` | 可直接下载的地址。有 `file_id` 时为对象存储签名地址 |
| `data.url_expires_at` | 签名地址的失效时间。过期后重新查询任务即可 |
| `data.url_vendor` | 上游供应商返回的原始地址，始终保留 |
| `data.usage` | 部分模型返回的实际用量 |

签名地址不可用时会回落到应用代理地址 `{PUBLIC_BASE_URL}/v1/images/get/{file_id}`。若 `PUBLIC_BASE_URL` 未配置或非法，则回落到上游原始地址。

视频结果不转存到对象存储，`data.url` 是上游的限时下载地址，没有 `file_id` 与 `url_vendor`。URL 过期后需要重新生成任务。

## 4 unknown 状态

`unknown` 是非终态。平台不知道上游是否已经出图，因此既不能算成功也不能退款。分两种情况：

- 已经拿到上游 taskId、只是查询失败：平台会继续重试，通常很快推进到 `completed` 或 `failed`。
- 提交阶段结果不确定，连 taskId 都没拿到：平台没有继续查证的凭据，不会自动重试，等待人工核对。

两种都会在任务创建满 6 小时后转为 `call_fail` 并退款。因此看到 `unknown` 时积分不会被吞掉，只是退款时间被推迟到兜底时限。
