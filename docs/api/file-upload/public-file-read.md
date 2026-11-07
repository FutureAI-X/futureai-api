# 公开文件读取

`GET /v1/images/get/:file_id`

无需认证。文件 ID 使用随机不可猜的格式：`file_` 加 24 位十六进制，再加扩展名。ID 本身就是唯一凭证，可枚举等于所有文件公开。

## 1 文件来源

图像生成任务完成时，平台会把上游成图转存到对象存储并写入 `files` 表。任务查询响应中的 `file_id` 即该记录的 ID。转存使用私有桶，读取接口在服务端用 `file_key` 现签一个 5 分钟有效的地址后再拉取，不依赖对象的匿名读权限。

参考媒体是私有对象，不能通过该接口读取。

## 2 响应

响应直接返回文件字节，不包 JSON：

- 图片类型（JPEG、PNG、WebP、GIF、AVIF、BMP）以原 `Content-Type` 和 `Content-Disposition: inline` 返回。
- 其余类型一律以 `application/octet-stream` 和 `Content-Disposition: attachment` 返回。SVG 也在其中：它可以内嵌脚本，内联渲染会在本域下执行。
- 支持 `Range` 与 `If-Range` 请求头，大文件可断点续传，响应为 `206 Partial Content`。
- 记录的有效期以 `files.expires_at` 为准，`NULL` 表示长期有效。成功响应允许浏览器缓存，缓存时长取剩余有效期与 24 小时中的较小者。

## 3 错误处理

错误响应为 JSON，状态码如下。

| 状态码 | 含义 |
|---|---|
| 404 | 文件 ID 为空、超长或记录不存在 |
| 410 | 记录已过期 |
| 502 | 对象存储未配置、预签名失败，或拉取文件时上游返回非 2xx |
| 429 | 超出该来源 IP 的限流额度 |

限流按来源 IP 统计，速率由 `IMAGE_GET_RATE_LIMIT_PER_MINUTE` 配置，默认 1000 次/分钟。该端点与前端同源，`TRUSTED_PROXIES` 配置错误时所有请求会被算作同一个 IP。
