# 任务详情

`GET /api/user/task-logs/:id`

返回当前用户的一条任务详情。请求需要 `Authorization: Bearer <JWT>`。

只能查看属于当前用户的任务。

## 可见字段

返回 `id`、`task_id`、`status`、`error_code`、`error_message`、`credits`、`credits_refunded`、`request_body`、`created_at`、`updated_at`、`duration_seconds`。

## 不返回上游信息

该接口刻意不返回 `vendor_response` 与 `query_response`，也不展示结果地址：这些内容包含上游供应商的任务 ID、结果地址等信息，只供管理端任务日志排查使用。个人端如需下载成图，应使用平台自己的文件地址，而不是上游地址。
