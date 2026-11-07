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
  },
  "error": null
}
```

视频任务完成时只返回 `taskId`、`status` 与 `data.url`：

```json
{
  "taskId": "task_e629c5e8a37a614cf2289f650585aae4",
  "status": "completed",
  "data": {
    "url": "https://files.metaso.cn/api/minimax/v2/video_generation/..."
  },
  "error": null
}
```

任务失败时，`data` 固定为 `null`，错误信息放在顶层 `error`：

```json
{
  "taskId": "task_e629c5e8a37a614cf2289f650585aae4",
  "status": "failed",
  "data": null,
  "error": {
    "code": "vendor_rejected",
    "message": "上游拒绝受理，积分已退回"
  }
}
```

处理中时 `data` 与 `error` 都是 `null`。

## 2 任务状态

`status` 只有三种取值。

| 状态 | 终态 | 含义 | 积分 |
|---|---|---|---|
| `processing` | 否 | 提交中、生成中或结果确认中 | 已扣 |
| `completed` | 是 | 生成完成，结果在 `data` 里 | 已扣 |
| `failed` | 是 | 失败，已退款；原因在 `error` 里 | 已退 |

只有 `completed` 时 `data` 才有值。`processing` 表示任务仍在推进，应继续轮询，建议间隔 5 秒左右。视频生成任务当前按 0 积分创建，积分列对它不适用。

MOSS 是同步接口，由平台后台提交，任务在生成期间显示为 `processing`。任何失败都会直接转为 `failed` 并退款。

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

## 4 超时与失败原因

平台不会让任务无限停留在 `processing`：

| 场景 | 时限 |
|---|---|
| 异步提交确认 | 创建后 60 秒 |
| 图片结果确认 | 创建后 15 分钟 |
| MOSS 同步生成 | 创建后 15 分钟 |
| 视频结果确认 | 创建后 30 分钟 |

到达截止时间仍未完成的任务会转为 `failed` 并退款。`error.code` 取值如下。

| code | 含义 |
|---|---|
| `vendor_rejected` | 上游明确拒绝受理 |
| `submit_timeout` | 提交结果超时未确认 |
| `query_timeout` | 结果确认超时 |
| `cancelled` | 上游取消 |
| `upstream_failed` | 上游返回失败 |
| `result_unavailable` | 上游报告完成但没有可用结果 |
| `internal_error` | 平台内部错误 |
| `task_failed` | 历史任务缺少具体原因时的兜底值 |

`failed` 表示积分已退还，调用方不需要再轮询。
