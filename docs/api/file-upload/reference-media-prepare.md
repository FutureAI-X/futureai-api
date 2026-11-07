# 申请上传凭证

`POST /v1/files/upload/prepare`

为参考媒体上传申请对象存储直传凭证。请求需要 `Authorization: Bearer sk-...`。

## 1 请求

```http
POST /v1/files/upload/prepare
Authorization: Bearer sk-...
Content-Type: application/json

{
  "media_type": "image",
  "filename": "reference.png",
  "content_type": "image/png",
  "size": 1048576
}
```

请求字段：

| 字段 | 必填 | 说明 |
|---|---|---|
| `media_type` | 是 | `image`、`video` 或 `audio` |
| `filename` | 否 | 原始文件名，仅用于记录；服务端会清理非法字符 |
| `content_type` | 是 | 文件的 MIME 类型 |
| `size` | 是 | 文件实际字节数，必须大于 0 且不超过对应类型上限 |

格式、大小、配额与限流规则见 [文件上传与读取](README.md#4-限制与有效期)。

## 2 响应

```json
{
  "upload_id": "file_9f2c1d7e8a4b",
  "file_id": "file_9f2c1d7e8a4b",
  "url": "https://<bucket>.cos.<region>.myqcloud.com/",
  "fields": {
    "key": "staging/file_9f2c1d7e8a4b.png",
    "policy": "...",
    "q-sign-algorithm": "sha1",
    "q-ak": "...",
    "q-key-time": "...",
    "q-signature": "...",
    "Content-Type": "image/png",
    "success_action_status": "200"
  },
  "expires_at": "2026-11-02T10:30:00Z",
  "session_expires_at": "2026-11-02T10:40:00Z"
}
```

`upload_id` 与 `file_id` 在 prepare 阶段相同。完成上传前 `file_id` 不可用于生成；`upload_id` 只用于调用 [完成上传](reference-media-complete.md)。

`expires_at` 是上传凭证的过期时间，`session_expires_at` 是上传会话的过期时间。两者都过期后只能重新 `prepare`。

## 3 后续步骤

拿到响应后，按 [文件上传与读取](README.md#3-直传对象存储) 构造表单并直传对象存储，再调用 [完成上传](reference-media-complete.md)。
