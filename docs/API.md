# API 参考

## 1 认证方式

三套凭证，各自对应一组路由。

| 路由前缀 | 凭证 | 传递方式 |
|---|---|---|
| `/api/auth`、`/api/pricing` | 无 | 公开 |
| `/api/user` | 登录态 | `Authorization: Bearer <JWT>` |
| `/api/admin` | 管理员 | `Authorization: Bearer <JWT>`（role = 100） |
| `/v1` | API Key | `Authorization: Bearer sk-...` |

所有凭证仅通过请求头传递，查询参数形式（`?token=` / `?data_key=`）已不再支持：查询串会进入访问日志、反向代理日志、浏览器历史与 `Referer` 头。获取 API Key 列表时请使用 `X-Data-Key` 请求头。

## 2 公开接口

### 2.1 健康检查

请求 `GET /health`，响应：

```json
{ "status": "ok" }
```

### 2.2 登录

请求 `POST /api/auth/login`，请求体：

```json
{ "username": "root", "password": "..." }
```

首次启动且 `users` 表为空时会自动创建 `root` 用户，其初始密码：

- 若设置了 `INITIAL_ROOT_PASSWORD` 环境变量，则使用该值；
- 否则随机生成并写入 `./root_initial_password.txt`（权限 `0600`）。

该密码不会写入日志。登录后应立即通过 `PUT /api/user/password` 修改，并删除密码文件。容器部署建议直接使用环境变量，理由见 [deploy/futureai-api/.env.example](../deploy/futureai-api/.env.example)。

响应：

```json
{
  "success": true,
  "message": "登录成功",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "data_key": "5Y2lzM8k...",
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

`data_key` 是当次会话的 AES-GCM 密钥（base64 的 32 字节），后续读取密钥类字段的接口（如 `GET /api/user/tokens`）需通过 `X-Data-Key` 请求头携带。

**令牌有效期**：`token` 为 JWT，24 小时过期。修改密码（无论用户自助还是管理员重置）会立即作废该用户已签发的所有令牌，客户端需重新登录。

> **注意**：登录接口有防爆破限流，同一来源 5 分钟内失败 5 次即返回 429，成功登录会清零计数。计数按来源 IP 统计，因此 `TRUSTED_PROXIES` 必须配置正确，否则所有用户会被算作同一来源，一人触发即锁定全站登录。

### 2.3 计费规则

请求 `GET /api/pricing`。该接口对未登录用户开放，因此响应中不应包含任何 `api_key` 字段。

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
        "type": "image",
        "credit_rule": {
          "rule_type": "per_request",
          "base_credits": 1.0,
          "ref_image_credits": 2.0,
          "items": [
            { "credits": 0.1, "conditions": [{ "param_path": "resolution", "param_value": "2k" }] }
          ]
        }
      }
    ]
  }
}
```

`credit_rule` 在模型未配置或规则被禁用时不出现，调用方需要判空。

`type` 取值为 `image` / `video` / `text` / `music` / `other`（图像生成 / 视频生成 / 文本生成 / 音乐生成 / 其他）。它不只是展示字段：参考图附加计费仅在类型为 `image` 且端点在图片白名单内时生效。

单个模型的积分规则由管理端配置，计算公式为：

```
本次总积分 = 基础积分（或命中的参数组合积分）+ 参考图张数 × 每张参考图积分
```

参考图附加计费只对图片生成类端点（`/v1/images/generations`、`/v1/images/edits`）生效，按请求体中标准字段 `image_urls` 数组的图片张数叠加扣除；`每张参考图积分 = 0` 等同于不开启。数组中重复的 URL 只计一次，单次计费张数上限为 100。

参数组合条件只能引用标准请求字段（`model`、`prompt`、`size`、`resolution`），且值必须为字符串。数组字段（`image_urls`）永远命中不了条件，因此不能用参数组合实现参考图计费。

管理端保存规则时会校验 `param_path` 必须是上述字段之一，否则返回 400 并列出可选值。

### 2.4 积分的小数位

积分的业务精度为 3 位小数（`model.CreditPrecision`），规则录入、计费、管理员调整用户积分与界面展示均按此口径。

`/v1/*` 接口不接受金额参数，积分由平台按模型规则算出，因此该约束只影响管理端：配置积分规则、调整用户积分时，超过 3 位小数返回 400。界面向下取整展示，因此显示出的积分一定可以花费。

数据库列的标度宽于业务口径（`numeric(20,10)`），放宽到 4 位只需修改 `CreditPrecision` 与前端的 `CREDIT_DECIMALS`，无需改动 schema，也无需订正数据。

## 3 用户接口

全部端点需要 `Authorization: Bearer <JWT>`。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/user/info` | 当前用户信息 |
| PUT | `/api/user/profile` | 修改昵称 / 邮箱 |
| PUT | `/api/user/password` | 修改自己的密码 |
| GET | `/api/user/tokens` | API Key 列表 |
| POST | `/api/user/tokens` | 新建 API Key |
| PUT | `/api/user/tokens/:id` | 修改 API Key |
| DELETE | `/api/user/tokens/:id` | 删除 API Key |
| GET | `/api/user/credit-logs` | 积分流水 |
| GET | `/api/user/task-logs` | 自己的任务记录 |
| GET | `/api/user/task-logs/:id` | 任务详情 |

## 4 OpenAI 兼容接口

全部端点需要 `Authorization: Bearer sk-...`，并按用户做令牌桶限流（`API_RATE_LIMIT_PER_MINUTE` / `API_RATE_LIMIT_BURST`）。这些端点每次调用都会真实消耗供应商配额。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/v1/models` | 可用模型列表 |
| POST | `/v1/images/generations` | 图像生成 |
| POST | `/v1/uploads/images` | 图片上传（单文件上限 10MB） |
| GET | `/v1/tasks/:task_id` | 查询任务 |

请求 `GET /v1/models` 的响应：

```json
{
  "object": "list",
  "data": [
    { "id": "deepseek-v4-flash", "object": "model", "owned_by": "futureai-api", "type": "text" }
  ]
}
```

`type` 是 OpenAI 规范外的附加字段（`image` / `video` / `text` / `music` / `other`），供调用方区分模型能力，标准客户端会忽略它。

上传使用 `multipart/form-data`，字段名 `file`，超过 10MB 返回 413。网关侧的 `client_max_body_size` 为 12m，若小于请求体大小，请求会在到达应用之前被拒绝。

### 4.1 图像生成的请求标准

`POST /v1/images/generations` 只接受下表字段，请求体中的其他字段一律忽略（不报错，也不透传给上游）。平台对各供应商的适配（参数改名、变体参数补全等）全部在这一层之下完成，调用方只需面向这一套字段。

| 字段 | 类型 | 说明 |
|---|---|---|
| `model` | string | 必填，平台模型名（不是供应商侧模型 ID） |
| `prompt` | string | 提示词 |
| `size` | string | 画幅比例，如 `16:9` / `1:1`，默认 `16:9` |
| `resolution` | string | 分辨率档位，如 `1k` / `2k`，默认 `1k` |
| `image_urls` | string[] | 参考图 URL 数组，同时决定参考图附加计费张数 |

`size` 与 `resolution` 省略时按上表默认值补齐，且补齐发生在计费之前，因此省略 `resolution` 的请求按 `resolution=1k` 匹配计费条件，与实际生成的分辨率一致。调用方显式传入的值原样保留。

`image_urls` 只接受字符串数组：单个字符串（如 `"image_urls": "https://..."`）会被判为类型错误返回 400，而不是被忽略。需要参考图时在报文中写上该字段即可，无需传任何供应商相关参数。

请求示例：

```json
{
  "model": "gpt-image-2.5-flare-ext",
  "prompt": "a cat sitting on a windowsill",
  "resolution": "2k",
  "image_urls": ["https://example.com/ref.png"]
}
```

`image_urls` 应传上传接口返回的 URL，不要内联 base64 data URI：请求体上限为 1MB，一张 10MB 的图 base64 之后约 13MB，会在网关层即被拒绝（413）。

### 4.2 请求体大小限制

各端点的上限如下。

| 端点 | 上限 | 超限响应 |
|---|---|---|
| `/v1/uploads/images` | 10MB + multipart 余量 | `413` |
| 其余所有端点 | 1MB | `413` |

### 4.3 幂等键（可选）

提交图像生成时携带 `Idempotency-Key` 请求头，可在窗口内安全重试：

```bash
curl -X POST https://futureaiapi.com/v1/images/generations \
  -H "Authorization: Bearer sk-xxx" \
  -H "Idempotency-Key: $(uuidgen)" \
  -H "Content-Type: application/json" \
  -d '{"model":"...","prompt":"a cat"}'
```

- 同一用户携带同一键重复提交只扣一次积分，后续请求直接返回首次的 `taskId`。
- 不同用户可以使用相同的键，互不影响。
- 不携带该请求头时不去重，保持原有行为。
- 键最长 128 字符。

**强烈建议调用方使用它**：提交是同步语义（上游最长 30 秒才响应），而多数 OpenAI 兼容 SDK 在超时后会自动重试；没有幂等键时，一次超时重试即会扣两次积分、在上游生成两张图。

### 4.4 任务状态

`GET /v1/tasks/:task_id` 返回的 `status` 取值如下。

| 状态 | 终态 | 含义 | 积分 |
|---|---|---|---|
| `pending` | 否 | 已扣费，正在提交上游 | 已扣 |
| `submitted` | 否 | 上游已受理，正在生成 | 已扣 |
| `unknown` | 否 | 结果无法确认（提交响应丢失，或查询持续失败） | 已扣 |
| `completed` | 是 | 生成完成，结果在 `data` 里 | 已扣 |
| `failed` | 是 | 上游明确返回失败 | **已退** |
| `cancelled` | 是 | 上游明确返回取消 | **已退** |
| `call_fail` | 是 | 上游明确拒绝受理 / 提交未成功 / 超期无法确认 | **已退** |

只有 `completed` 时 `data` 才有值。非终态表示仍在推进中，应继续轮询（建议间隔 5 秒左右），不要当作失败。

`unknown` 是刻意保留的非终态：平台并不知道上游是否出图，此时既不能算成功也不能退款。分两种情况——

- 已经拿到上游 taskId、只是查询失败：会继续重试，通常很快推进到 `completed` / `failed`。
- 提交阶段结果不确定（超时、响应读不完整），连 taskId 都没拿到：没有再查证的凭据，**不会自动重试**，等人工核对。

两种都会在原任务创建满 6 小时后被对账循环转为 `call_fail` 并退款，同时打 `[需人工对账]` 日志。因此看到 `unknown` 时积分不会被吞掉，只是退还时间被推迟到兜底时限。

## 5 管理员接口

全部端点需要 `Authorization: Bearer <JWT>` 且 `role = 100`。

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

完整列表见 [router/router.go](../router/router.go)。该文件是唯一的事实来源，本文不重复维护每个端点的方法与参数。

## 6 错误格式

```json
{
  "error": {
    "message": "Not found",
    "type": "invalid_request_error"
  }
}
```

`/api` 与 `/v1` 前缀下未注册的路径一律返回上述结构的 404 JSON，不会回退到前端的 `index.html`，否则接口拼写错误会得到 200 与一段 HTML，错误被掩盖成解析失败。

`/v1` 的常见状态码如下。

| 状态码 | 含义 |
|---|---|
| 401 | API Key 缺失、无效、已过期，或所属账号被禁用 |
| 402 | 积分不足 |
| 403 | 无权访问该资源（如查询他人的任务） |
| 413 | 请求体超过上限，见 4.2 节 |
| 429 | 触发限流，`Retry-After` 头给出建议等待秒数 |
| 503 | 无可用供应商，或该模型未配置计费规则 |
