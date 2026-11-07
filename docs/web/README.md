# Web 前端 API

本文面向 web 前端，覆盖登录、用户、管理端与计费规则查询。客户端软件调用的 `/v1/*` 接口见 [客户端 API](../api/README.md)。

## 1 认证方式

`/api/auth` 与 `/api/pricing` 公开，`/api/user` 与 `/api/admin` 需要登录态。

| 路由前缀 | 凭证 | 传递方式 |
|---|---|---|
| `/api/auth`、`/api/pricing` | 无 | 公开 |
| `/api/user` | 登录态 | `Authorization: Bearer <JWT>` |
| `/api/admin` | 管理员 | `Authorization: Bearer <JWT>`，`role = 100` |

所有凭证仅通过请求头传递，不支持查询参数形式。查询串会进入访问日志、反向代理日志、浏览器历史与 `Referer` 头。

`data_key` 用于按需读取密钥类字段，通过 `X-Data-Key` 请求头携带，不要放进查询串。

## 2 端点索引

各分组对应的文档如下。

| 分组 | 文档 |
|---|---|
| 登录、会话与令牌 | [登录与认证](auth/README.md) |
| 用户资料、API Key、积分与任务记录 | [用户接口](user/README.md) |
| 用户、供应商、模型、计费规则、任务记录、版本管理 | [管理员接口](admin/README.md) |
| 公开计费规则与积分精度 | [计费规则](pricing/README.md) |

图像展示使用 `/v1/images/get/:file_id`，该端点无需认证，说明见 [公开文件读取](../api/file-upload/public-file-read.md)。

## 3 错误格式

`/api` 与 `/v1` 前缀下未注册的路径一律返回以下结构，不会回退到前端的 `index.html`。否则接口拼写错误会得到 200 与一段 HTML，错误被掩盖成解析失败。

```json
{
  "error": {
    "message": "Not found",
    "type": "invalid_request_error"
  }
}
```

业务接口的通用响应结构为 `{ "success": true, "data": ... }` 或 `{ "success": false, "message": "..." }`，具体以各接口文档为准。
