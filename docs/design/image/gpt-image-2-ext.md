# APIMart 图像设计（gpt-image-2-ext）

## 1 范围

本文说明平台模型 `gpt-image-2-ext`（上游模型 `gpt-image-2` 的等价别名）的内部实现。通用图像链路见 [图像生成链路设计](README.md)，调用方文档见 [客户端文档](../../api/image/gpt-image-2-ext.md)。

## 2 供应商与端点

APIMart 使用异步任务接口。平台提交后拿到上游 `task_id`，再由对账循环轮询任务状态。

| 项目 | 值 |
|---|---|
| 供应商名称 | `apimart` |
| Base URL | APIMart API 地址 |
| 提交路径 | `/v1/images/generations` |
| 查询路径 | `/v1/tasks/{task_id}` |
| 认证 | `Authorization: Bearer <API Key>` |
| 供应商模型 ID | `gpt-image-2-ext`（上游也接受 `gpt-image-2`，以管理端配置为准） |

本模型不使用 `version` 字段；`version` 由 gpt-image-2.5-ext 的模型规格使用。

## 3 能力声明

与 gpt-image-2.5 系列相同，使用平台默认能力。

| 字段 | 默认值 | 影响 |
|---|---|---|
| `BackgroundSubmit` | `false` | handler 内同步提交，最长 30 秒 |
| `SupportsReferenceImages` | `true` | 参考图参与上游请求与计费 |
| `RequiresPixelSize` | `false` | 供应商级声明；实际 `size` 校验按模型规格 `SizeRule` 走 |
| `SubmitTimeout` | 30 秒 | 使用包级 `SubmitTimeout` |

## 4 入参校验

APIMart 通过 `ImageRequestValidator` 在扣费前校验以下约束。这些约束是**模型规格**，登记在 `supplier/apimart.go` 的 `apimartImageModels`，由 `req.Model` 查表：

- 模型必须已登记，未登记直接拒绝（fail-closed）；
- `prompt` 去除首尾空格后不能为空；
- `size` 接受比例或像素尺寸，由 `SizeRule` 按模型校验，平台只校验、不做换算：
  - 比例：`1:1`、`3:2`、`2:3`、`4:3`、`3:4`、`5:4`、`4:5`、`16:9`、`9:16`、`2:1`、`1:2`、`3:1`、`1:3`、`21:9`、`9:21`；
  - 像素：宽高为 `16` 的倍数、宽高比 `1:3`~`3:1`、单边 ≤ `3840`、总像素 ≤ `3840×2160`；超过 `2560×1440` 属于上游实验性支持，平台不拒绝；
  - 上游支持的 `auto` 不开放；
- `resolution` 只接受该模型规格中的档位，本模型为 `1K`、`2K`、`4K`；匹配大小写不敏感；
- `image_ids` 优先，非空时去重后不超过该模型的参考图上限，本模型为 15 张；
- `image_urls` 在 `image_ids` 为空时去重后不超过该模型上限，本模型为 15 张。

参考图数量由模型规格控制，控制器只校验 `file_id` 的形态。

校验失败直接返回 400，不扣费、不建任务。

## 5 请求体转换

`buildRequest` 写入以下字段：

| 上游字段 | 值 |
|---|---|
| `model` | 管理端配置的供应商模型 ID（`gpt-image-2-ext` 或 `gpt-image-2`） |
| `prompt` | 调用方提示词 |
| `size` | 调用方画幅比例，已由控制器补齐默认值 |
| `resolution` | 平台档位转成小写 `1k` / `2k` / `4k` |
| `n` | 固定 `1` |
| `image_urls` | 非空时写入 |

本模型不写 `version`。规格表 `apimartImageModels` 中的 `lowercaseResolution` 为 `true`，`buildRequest` 据此把平台统一的大写档位转成上游要求的小写；gpt-image-2.5-ext 使用大写，两类模型互不影响。

## 6 提交方式与超时

`ImageGenerate` 使用 `common.OutboundHTTPClient`，提交超时为包级 `SubmitTimeout`（30 秒）。

请求刻意只携带 `Content-Type` 与 `Authorization`，**不传** `Idempotency-Key` 和 `X-APIMart-Response-Version`。平台自身已按 `Idempotency-Key` 做任务级去重，不需要上游再提供一层；不带响应版本头时，上游固定返回 HTTP 200 + `data[].task_id`。

失败性质如下。

| 场景 | 失败性质 | 处理 |
|---|---|---|
| 本地序列化失败、构建请求失败、4xx | `FailureRejected` | 退款 |
| 5xx、超时、连接中断、响应读取失败 | `FailureUnknown` | 不退款，转 `unknown` |
| 业务 `code` 不是 200 | `FailureRejected` | 退款 |
| 响应中缺少任务 ID | `FailureUnknown` | 不退款，转 `unknown` |
| 非预期的 HTTP 202 | `FailureUnknown` | 不退款，转 `unknown` |

`FailureUnknown` 的语义是「上游可能已经建单」。控制器不会立即退款，而是在超龄兜底后转 `call_fail` 并退款。

## 7 任务状态与对账

对账循环调用 `TaskQuery`，向 `/v1/tasks/{task_id}?language=zh` 发起查询。上游状态按以下规则映射。

| 上游状态 | 平台状态 |
|---|---|
| `completed` | `completed` |
| `failed` | `failed` |
| `cancelled` | `cancelled` |
| `submitted` / `pending` / `processing` | 保持非终态 |
| 查询失败 | `call_fail` |
| 未识别的状态 | 保持非终态，下一轮继续查询 |

`task_id` 来自上游响应，拼接查询 URL 前必须通过字符集校验。校验失败按查询失败处理，不拼接 URL。

## 8 结果转存

`TaskQuery` 在 `completed` 时返回上游图片地址或 `b64_json`，对账循环调用 `persistCompletedImage` 下载并转存。

APIMart 不声明 `RequireResultFetch`，因此转存失败只记录日志，任务仍按 `completed` 处理，`data.url_vendor` 保留上游地址。

## 9 失败性质与退款

可退款的终态为 `failed`、`cancelled` 和 `call_fail`。`unknown` 不是终态，不退款；任务创建满 6 小时后仍未确认时，对账循环转 `call_fail` 并退款，同时记录 `[需人工对账]` 日志。

## 10 配置

管理端配置如下。

| 配置项 | 值 |
|---|---|
| 供应商名称 | `apimart` |
| Base URL | APIMart API 地址 |
| 供应商模型 ID | `gpt-image-2-ext` 或 `gpt-image-2` |
| 平台模型名 | `gpt-image-2-ext` 或 `gpt-image-2`（须与 `apimartImageModels` 的键一致） |
| 模型类型 | `image` |
| 端点 | `/v1/images/generations` |
| 计费规则 | 基础积分 + 可选的参考图单价 |

平台模型名必须是 `apimartImageModels` 中已登记的键，否则请求会在扣费前返回 400。

## 11 代码与测试

实现与测试位置如下。

| 内容 | 位置 |
|---|---|
| 供应商实现 | `supplier/apimart.go` |
| 供应商单元测试 | `supplier/apimart_test.go` |
| 模型规格（size / resolution / 参考图上限） | `supplier/apimart.go` 的 `apimartImageModels` |
| 控制器计费与参考图 | `controller/image.go` |
| 任务状态与对账 | `model/task.go`、`controller/image.go` |
