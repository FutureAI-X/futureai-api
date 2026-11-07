# 完成上传

`POST /v1/files/upload/complete`

直传对象存储完成后，调用该接口校验文件并登记为可用的参考媒体。请求需要 `Authorization: Bearer sk-...`。

## 1 请求

```http
POST /v1/files/upload/complete
Authorization: Bearer sk-...
Content-Type: application/json

{ "upload_id": "file_9f2c1d7e8a4b" }
```

## 2 响应

```json
{
  "file_id": "file_9f2c1d7e8a4b",
  "media_type": "image",
  "content_type": "image/png",
  "bytes": 1048576,
  "duration_ms": 0,
  "width": 1024,
  "height": 1024,
  "expires_at": "2026-11-02T16:25:00Z"
}
```

## 3 处理逻辑

`complete` 会读取对象、校验文件头与媒体信息，并按媒体类型校验格式、时长、编码、帧率等。视频另校验宽高与宽高比。全部通过后，服务端把对象从 `staging/` 复制到正式路径并标记为可用。

因此该接口比普通接口慢，客户端超时应设为 120 秒以上。

`complete` 是幂等的。同一个 `upload_id` 重复调用会返回同一份元数据，不会重复复制对象。网络超时后可以直接重试。

响应中的 `expires_at` 是参考文件的实际过期时间，从完成时刻起 6 小时。

## 4 错误处理

常见状态码如下。

| 状态码 | 场景 | 处理 |
|---|---|---|
| 400 | 文件为空、格式不符、无法解析宽高 | 重新选择文件并 `prepare` |
| 404 | `upload_id` 不存在或不属于当前用户 | 检查是否混用了其他用户的凭证 |
| 409 | 对象尚未上传完成 | 等待 1–2 秒后重试 `complete` |
| 410 | 上传会话已过期 | 重新 `prepare` 并重新上传 |
| 502 | 媒体探测或复制暂时失败 | 用同一 `upload_id` 重试，不要重新上传 |
| 503 | 对象存储不可用 | 稍后重试 |

因文件校验失败返回的 400 会把文件标记为失败，同一个 `upload_id` 无法继续使用，必须重新 `prepare`。502 不影响文件状态，可以放心重试。
