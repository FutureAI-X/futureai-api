# 客户端 API

本文面向桌面客户端与第三方调用方，覆盖图像生成、视频生成、任务查询、文件上传、模型查询与版本更新检查。

## 1 认证方式

`/v1/*` 端点使用 API Key，版本更新检查与公开文件读取无需认证。

| 路由 | 凭证 | 传递方式 |
|---|---|---|
| `/v1/*` | API Key | `Authorization: Bearer sk-...` |
| `/v1/images/get/:file_id` | 无 | 文件 ID 即凭证 |
| `/api/apps/:app_key/version` | 无 | 公开 |

所有凭证仅通过请求头传递，不支持查询参数形式。`/v1/*` 按用户做令牌桶限流，速率由 `API_RATE_LIMIT_PER_MINUTE` 与 `API_RATE_LIMIT_BURST` 配置。

## 2 端点索引

各端点对应的文档如下。

| 方法 | 路径 | 文档 |
|---|---|---|
| GET | `/v1/models` | [模型查询](models/list.md) |
| POST | `/v1/images/generations` | [图像生成](image/README.md) |
| POST | `/v1/videos/generations` | [视频生成](video/README.md) |
| POST | `/v1/uploads/images` | [公开图片上传](file-upload/public-image-upload.md) |
| POST | `/v1/files/upload/prepare` | [申请上传凭证](file-upload/reference-media-prepare.md) |
| POST | `/v1/files/upload/complete` | [完成上传](file-upload/reference-media-complete.md) |
| GET | `/v1/images/get/:file_id` | [公开文件读取](file-upload/public-file-read.md) |
| GET | `/v1/tasks/:task_id` | [任务查询](tasks/get-task.md) |
| GET | `/api/apps/:app_key/version` | [版本更新检查](version-update/check.md) |
| GET | `/health` | 见第 6 章 |

## 3 请求体大小限制

各端点的上限如下。

| 端点 | 上限 | 超限响应 |
|---|---|---|
| `/v1/uploads/images` | 10MB + multipart 余量 | `413` |
| 其余所有端点 | 1MB | `413` |

网关侧的 `client_max_body_size` 为 12m。若请求体超过该值，请求会在到达应用之前被拒绝。

## 4 幂等键

提交图像或视频生成时携带 `Idempotency-Key` 请求头，可在窗口内安全重试：

```bash
curl -X POST https://futureaiapi.com/v1/images/generations \
  -H "Authorization: Bearer sk-xxx" \
  -H "Idempotency-Key: $(uuidgen)" \
  -H "Content-Type: application/json" \
  -d '{"model":"...","prompt":"a cat"}'
```

- 同一用户携带同一键重复提交只会处理一次，后续请求直接返回首次的 `taskId`。
- 不同用户可以使用相同的键，互不影响。
- 不携带该请求头时不去重。
- 键最长 128 字符。

上游提交可能耗时较长（普通供应商最长 30 秒；MOSS 同步生成最长 15 分钟），而多数 OpenAI 兼容 SDK 在超时后会自动重试。没有幂等键时，一次超时重试就会重复扣费，并在上游重复生成。

## 5 错误格式

`/v1` 与 `/api` 前缀下未注册的路径一律返回以下结构，不会回退到前端的 `index.html`。

```json
{
  "error": {
    "message": "Not found",
    "type": "invalid_request_error"
  }
}
```

`/v1` 的常见状态码如下。

| 状态码 | 含义 |
|---|---|
| 401 | API Key 缺失、无效、已过期，或所属账号被禁用 |
| 402 | 积分不足 |
| 403 | 无权访问该资源，例如查询他人的任务 |
| 413 | 请求体超过上限，见第 3 章 |
| 429 | 触发限流，`Retry-After` 头给出建议等待秒数 |
| 503 | 无可用供应商，或该模型未配置计费规则 |

## 6 健康检查

请求 `GET /health`，响应：

```json
{ "status": "ok" }
```
