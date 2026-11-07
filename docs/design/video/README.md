# 视频生成链路设计

本文面向维护者，说明视频生成从 HTTP 请求进入系统到结果返回的完整设计。调用方文档见 [客户端视频生成文档](../../api/video/README.md)。

## 1 范围

本文覆盖 `/v1/videos/generations` 的控制器、参考文件解析、供应商适配层、任务状态与对账。单个模型的差异见第 8 章的模型设计文档。

## 2 模块与职责

各模块的职责与代码位置如下。

| 模块 | 职责 | 代码 |
|---|---|---|
| 控制器 | 解析请求、解析参考文件、建任务、提交上游 | `controller/video.go` |
| 供应商接口 | 定义 `VideoGenerate`、`TaskQuery` | `supplier/supplier.go` |
| 供应商实现 | 把平台字段翻译成上游 `content` 结构 | `supplier/metaso.go` |
| 任务模型 | 任务状态、对账查询 | `model/task.go` |
| 出站客户端 | 共享连接池、SSRF 防护、超时策略 | `common/outbound.go` |

## 3 请求流程

控制器按以下顺序处理请求。参数错误在创建任务之前返回，不产生任务行。

1. 解析请求体并校验必填字段。`model`、`prompt`、`resolution`、`duration`、`ratio` 必填。
2. 查找端点、模型、模型与端点的绑定关系，选择第一个可用供应商。
3. 解密供应商 API Key，确认调用方身份。
4. 处理幂等键。命中已有任务时直接返回原任务 ID。
5. 解析 `images` / `videos` / `audios` 中的 `file_id`，生成短期签名 URL。
6. 组装 `VideoGenerateRequest` 并再次校验媒体 URL。
7. 创建任务。视频计费尚未启用，任务按 0 积分创建。
8. 调用供应商提交接口，拿到上游 `task_id`。
9. 写入 `vendor_response`，交给对账循环。

## 4 参考文件解析

`file_id` 必须是平台上传接口返回的标识，不接受外部 URL。解析时校验以下条件：

- 属于当前用户；
- `purpose` 为 `reference` 或 `result`；
- `status` 为 `ready`；
- 未过期，且剩余有效期不少于 30 分钟；
- 媒体类型与所在数组匹配；
- 视频参考文件有可用的时长信息。

签名 URL 最长 2 小时有效，且不超过文件剩余有效期。签名地址不会返回给调用方，也不会写入任务快照。

## 5 供应商适配

Metaso 的请求体使用 `content` 数组。`buildVideoRequest` 按以下顺序组装：

1. `text`：调用方提示词；
2. `image_url`：`images` 中的每个 URL，`role=reference_image`；
3. `video_url`：`videos` 中的每个 URL，`role=reference_video`；
4. `audio_url`：`audios` 中的每个 URL，`role=reference_audio`。

`fast_mode` 固定为 `false`，不是公开字段。`model` 使用管理端配置的供应商模型 ID。

## 6 任务状态与对账

视频任务与图像任务共用同一套状态机。上游状态按以下规则映射。

| 上游状态 | 平台状态 |
|---|---|
| `queued` / `running` | `submitted` |
| `succeeded` | `completed` |
| `failed` | `failed` |
| `cancelled` | `cancelled` |
| 查询失败或未知状态 | `call_fail` |

`call_fail` 表示查询本身失败，不是任务失败。对账循环不会据此退款，任务保持非终态等待下一轮。

## 7 失败性质与结果

Metaso 提交接口的失败性质如下。

| 场景 | 失败性质 | 处理 |
|---|---|---|
| 4xx、业务错误码、请求未发出 | `FailureRejected` | 转终态 |
| 5xx、超时、连接中断、响应缺少 `task_id` | `FailureUnknown` | 转 `unknown`，不立即退款 |

视频任务按 0 积分创建，因此退款流程实际不会产生积分流水，但状态机与图像任务保持一致。

任务完成后只返回 `taskId`、`status` 与 `data.url`。视频结果不转存到对象存储，`data.url` 是上游的限时下载地址，没有 `file_id` 与 `url_vendor`。

## 8 模型设计文档

各模型的设计文档如下。

| 模型 | 文档 |
|---|---|
| `MiniMax-H3` | [minimax-h3.md](minimax-h3.md) |

## 9 新增模型检查清单

新增视频模型时，按以下清单补齐：

1. 在 `supplier` 包实现 `VideoSupplier` 接口。
2. 在 `NewVideoSupplier` 与 `NewTaskQuerier` 注册供应商名称。
3. 确认参考媒体类型与上游 `role` 的映射。
4. 补充单元测试，覆盖成功、4xx、5xx、超时、任务查询状态映射。
5. 在 `docs/api/video/` 增加客户端文档，在 `docs/design/video/` 增加设计文档。
6. 在管理端配置供应商、供应商模型、模型类型、端点绑定。
