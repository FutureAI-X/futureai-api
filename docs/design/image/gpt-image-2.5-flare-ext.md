# APIMart 图像设计（gpt-image-2.5-flare-ext）

## 1 范围

本文说明平台模型 `gpt-image-2.5-flare-ext` 的内部实现。通用图像链路见 [图像生成链路设计](README.md)，调用方文档见 [客户端文档](../../api/image/gpt-image-2.5-flare-ext.md)。

## 2 供应商与端点

APIMart 使用异步任务接口。平台提交后拿到上游 `task_id`，再由对账循环轮询任务状态。

| 项目 | 值 |
|---|---|
| 供应商名称 | `apimart` |
| Base URL | APIMart API 地址 |
| 提交路径 | `/v1/images/generations` |
| 查询路径 | `/v1/tasks/{task_id}` |
| 认证 | `Authorization: Bearer <API Key>` |
| 供应商模型 ID | `gpt-image-2.5-ext` |

## 3 能力声明

APIMart 未实现 `ImageCapabilityProvider`，使用平台默认能力。

| 字段 | 默认值 | 影响 |
|---|---|---|
| `BackgroundSubmit` | `false` | handler 内同步提交，最长 30 秒 |
| `SupportsReferenceImages` | `true` | 参考图参与上游请求与计费 |
| `RequiresPixelSize` | `false` | `size` 按比例透传 |
| `SubmitTimeout` | 30 秒 | 使用包级 `SubmitTimeout` |

## 4 入参校验

APIMart 没有供应商特有的请求预校验。平台标准字段校验与参考图校验由控制器统一完成：

- `size` 使用比例，如 `16:9`、`1:1`；
- `resolution` 使用 `1k` / `2k`；
- `image_ids` 去重后最多 15 张；
- `image_urls` 去重后单次最多 100 张计入计费。

参考图字段的优先级与校验规则见 [客户端图像生成文档](../../api/image/README.md#4-参考图)。

## 5 请求体转换

`buildRequest` 写入以下字段：

| 上游字段 | 值 |
|---|---|
| `model` | `gpt-image-2.5-ext` |
| `prompt` | 调用方提示词 |
| `size` | 调用方画幅比例，已由控制器补齐默认值 |
| `resolution` | 调用方分辨率档位，已补齐默认值 |
| `image_urls` | 非空时写入 |
| `version` | 按平台模型名补全 |

两个平台模型共用同一个供应商模型 ID，调用方请求体里的 `model` 已不足以区分风格版本。`applyImageVersion` 回看调用方原始模型名，把 `gpt-image-2.5-flare-ext` 映射为 `version=flare`。映射表在 `supplier/apimart.go` 的 `apimartImageVersions` 中维护。新增映射到该供应商模型的平台模型时，必须同步登记映射，否则会落到上游默认值并记录错误日志。

## 6 提交方式与超时

`ImageGenerate` 使用 `common.OutboundHTTPClient`，提交超时为包级 `SubmitTimeout`（30 秒）。上游正常在秒级返回 `task_id`。

失败性质如下。

| 场景 | 失败性质 | 处理 |
|---|---|---|
| 本地序列化失败、构建请求失败、4xx | `FailureRejected` | 退款 |
| 5xx、超时、连接中断、响应读取失败 | `FailureUnknown` | 不退款，转 `unknown` |
| 业务 `code != 200` | `FailureRejected` | 退款 |
| 响应中缺少 `task_id` | `FailureUnknown` | 不退款，转 `unknown` |

`FailureUnknown` 的语义是「上游可能已经建单」。控制器不会立即退款，而是在超龄兜底后转 `call_fail` 并退款。

## 7 任务状态与对账

对账循环调用 `TaskQuery`，向 `/v1/tasks/{task_id}?language=zh` 发起查询。上游状态按以下规则映射。

| 上游状态 | 平台状态 |
|---|---|
| `completed` | `completed` |
| `failed` | `failed` |
| `cancelled` | `cancelled` |
| `pending` / `processing` | 保持非终态 |
| 查询失败 | `call_fail` |
| 未识别的状态 | 保持非终态，下一轮继续查询 |

`task_id` 来自上游响应，拼接查询 URL 前必须通过字符集校验。校验失败按查询失败处理，不拼接 URL。

## 8 结果转存

`TaskQuery` 在 `completed` 时返回上游图片地址或 `b64_json`。对账循环调用 `persistCompletedImage` 下载并转存。

APIMart 不声明 `RequireResultFetch`，因此转存失败只记录日志，任务仍按 `completed` 处理，`data.url_vendor` 保留上游地址。

## 9 失败性质与退款

可退款的终态为 `failed`、`cancelled` 和 `call_fail`。`unknown` 不是终态，不退款；任务创建满 6 小时后仍未确认时，对账循环转 `call_fail` 并退款，同时记录 `[需人工对账]` 日志。

## 10 配置

管理端配置如下。

| 配置项 | 值 |
|---|---|
| 供应商名称 | `apimart` |
| Base URL | APIMart API 地址 |
| 供应商模型 ID | `gpt-image-2.5-ext` |
| 平台模型名 | `gpt-image-2.5-flare-ext` |
| 模型类型 | `image` |
| 端点 | `/v1/images/generations` |
| 计费规则 | 基础积分 + 可选的参考图单价 |

## 11 代码与测试

实现与测试位置如下。

| 内容 | 位置 |
|---|---|
| 供应商实现 | `supplier/apimart.go` |
| 供应商单元测试 | `supplier/apimart_test.go` |
| 版本映射 | `supplier/apimart.go` 的 `apimartImageVersions` |
| 控制器计费与参考图 | `controller/image.go` |
| 任务状态与对账 | `model/task.go`、`controller/image.go` |
