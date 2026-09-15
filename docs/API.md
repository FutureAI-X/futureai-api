# API 参考

## 认证方式

三套凭证，各自对应一组路由：

| 路由前缀 | 凭证 | 传递方式 |
|---|---|---|
| `/api/auth`、`/api/pricing` | 无 | 公开 |
| `/api/user` | 登录态 | `Authorization: Bearer <JWT>` |
| `/api/admin` | 管理员 | `Authorization: Bearer <JWT>`（role = 100） |
| `/v1` | API Key | `Authorization: Bearer sk-...` |

**所有凭证仅通过请求头传递。** 查询参数形式（`?token=` / `?data_key=`）已不再支持：
查询串会进入访问日志、反向代理日志、浏览器历史与 `Referer` 头。
获取 API Key 列表时请使用 `X-Data-Key` 请求头。

---

## 公开接口

### 健康检查

```bash
GET /health
```
```json
{ "status": "ok" }
```

### 登录

```bash
POST /api/auth/login
```
```json
{ "username": "root", "password": "..." }
```

首次启动且 `users` 表为空时会自动创建 `root` 用户，其初始密码：

- 若设置了 `INITIAL_ROOT_PASSWORD` 环境变量，则使用该值；
- 否则随机生成并写入 `./root_initial_password.txt`（权限 `0600`）。

该密码**不会**写入日志。登录后请立即通过 `PUT /api/user/password` 修改，
并删除密码文件（容器部署建议直接用环境变量，理由见
[deploy/token-hub/.env.example](../deploy/token-hub/.env.example)）。

响应：

```json
{
  "success": true,
  "message": "登录成功",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "user": {
      "id": 1,
      "username": "root",
      "display_name": "Root User",
      "role": 100,
      "status": 1,
      "email": "",
      "credits": 100000000,
      "used_credits": 0
    }
  }
}
```

### 计费规则

```bash
GET /api/pricing
```

该接口对未登录用户开放，因此响应里**不应包含任何 `api_key` 字段**。

响应结构：

```json
{
  "success": true,
  "data": {
    "models": [
      {
        "id": 3,
        "name": "gpt-image-2",
        "owner": "FutureAI",
        "description": "...",
        "tags": "...",
        "status": 1,
        "credit_rule": {
          "rule_type": "per_request",
          "base_credits": 1.0,
          "ref_image_credits": 2.0,
          "ref_image_params": "image,images,image_url,image_urls,ref_images",
          "items": [
            { "credits": 0.1, "conditions": [{ "param_path": "resolution", "param_value": "2k" }] }
          ]
        }
      }
    ]
  }
}
```

`credit_rule` 在模型未配置（或规则被禁用）时**不出现**，调用方需要判空。
模型广场的详情弹框消费的就是这个字段。

单个模型的积分规则由管理端配置，计算公式为：

```
本次总积分 = 基础积分（或命中的参数组合积分）+ 参考图张数 × 每张参考图积分
```

参考图附加计费只对图片生成类端点（`/v1/images/generations`、`/v1/images/edits`）生效，
按请求体中携带的参考图张数叠加扣除；`每张参考图积分 = 0` 时等同于不开启。
参考图的参数名由管理员配置（见 `ref_image_params`），默认为
`image,images,image_url,image_urls,ref_images`；同一张图重复出现在多个参数名下只计一次，
单次计费张数上限为 100。

参数组合条件仅支持**顶层参数且值必须为字符串**，数组类型的参数不会命中，
因此不能用参数组合来做参考图计费。

---

## 用户接口

全部需要 `Authorization: Bearer <JWT>`。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/user/info` | 当前用户信息 |
| PUT | `/api/user/profile` | 改昵称 / 邮箱 |
| PUT | `/api/user/password` | 改自己的密码 |
| GET | `/api/user/tokens` | API Key 列表 |
| POST | `/api/user/tokens` | 新建 API Key |
| PUT | `/api/user/tokens/:id` | 改 API Key |
| DELETE | `/api/user/tokens/:id` | 删除 API Key |
| GET | `/api/user/credit-logs` | 积分流水 |
| GET | `/api/user/task-logs` | 自己的任务记录 |
| GET | `/api/user/task-logs/:id` | 任务详情 |

```bash
GET /api/user/info
Authorization: Bearer <token>
```

---

## OpenAI 兼容接口

需要 `Authorization: Bearer sk-...`，并按用户做令牌桶限流
（`API_RATE_LIMIT_PER_MINUTE` / `API_RATE_LIMIT_BURST`）——
这些端点每次调用都会真实消耗供应商配额。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/v1/models` | 可用模型列表 |
| POST | `/v1/images/generations` | 图像生成 |
| POST | `/v1/uploads/images` | 图片上传（单文件上限 10MB） |
| GET | `/v1/tasks/:task_id` | 查询任务 |

```bash
GET /v1/models
```
```json
{
  "object": "list",
  "data": [
    { "id": "deepseek-v4-flash", "object": "model", "owned_by": "token-hub" }
  ]
}
```

上传为 `multipart/form-data`，字段名 `file`。超过 10MB 返回 413。
注意网关侧的 `client_max_body_size` 是 12m，小于此值的话请求会在到达应用之前就被拒绝。

---

## 管理员接口

需要 `Authorization: Bearer <JWT>` 且 `role = 100`。

| 分组 | 路径前缀 |
|---|---|
| 用户 | `/api/admin/users` |
| 供应商 | `/api/admin/vendors` |
| 供应商模型 | `/api/admin/vendor-models` |
| 上游端点 | `/api/admin/endpoints` |
| 模型 | `/api/admin/models` |
| 模型↔端点绑定 | `/api/admin/models/:id/endpoints` |
| 计费规则 | `/api/admin/models/:id/credit-rule`、`/api/admin/credit-rules/:id` |
| 积分流水 | `/api/admin/credit-logs` |
| 全员任务记录 | `/api/admin/task-logs` |

完整列表见 [router/router.go](../router/router.go) —— 那份是唯一的事实来源，
本文件不重复维护每个端点的方法与参数。

---

## 错误格式

```json
{
  "error": {
    "message": "Not found",
    "type": "invalid_request_error"
  }
}
```

`/api` 与 `/v1` 前缀下未注册的路径一律返回这个结构的 404 JSON，
不会回退到前端的 `index.html`（否则接口拼错会拿到 200 + 一坨 HTML，
错误被掩盖成解析失败）。
