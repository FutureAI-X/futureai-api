# 视频生成链路设计

本文面向维护者，说明视频生成从 HTTP 请求进入系统到结果返回的完整设计。调用方文档见 [客户端视频生成文档](../../api/video/README.md)。

## 1 范围

本文覆盖 `/v1/videos/generations` 的控制器、参考文件解析、计费、供应商适配层、任务状态与对账。计费细节见 [视频计费设计](billing.md)，单个模型的差异见第 9 章的模型设计文档。

## 2 模块与职责

各模块的职责与代码位置如下。

| 模块 | 职责 | 代码 |
|---|---|---|
| 控制器 | 解析请求、解析参考文件、计算积分、建任务、提交上游 | `controller/video.go` |
| 参数规格 | 解析并校验模型的 `param_spec` 与请求参数 | `videoparam/` |
| 计费计算 | 按数量/秒数与分辨率档位计算积分并生成快照 | `controller/video_billing.go` |
| 视频计费规则 | 规则与分辨率价目表、校验、读写 | `model/video_credit_rule.go` |
| 供应商接口 | 定义 `VideoGenerate`、`TaskQuery` | `supplier/supplier.go` |
| 供应商实现 | 把平台字段翻译成上游请求体 | `supplier/metaso.go`、`supplier/apimart.go` |
| 任务模型 | 任务状态、对账查询 | `model/task.go` |
| 出站客户端 | 共享连接池、SSRF 防护、超时策略 | `common/outbound.go` |

## 3 请求流程

控制器按以下顺序处理请求。参数错误在创建任务之前返回，不产生任务行。

1. 解析请求体并校验必填字段。`model`、`prompt`、`resolution`、`duration`、`ratio` 必填。
2. 查找端点、模型、模型与端点的绑定关系，选择第一个可用供应商。
3. 解密供应商 API Key，确认调用方身份。
4. 处理幂等键。命中已有任务时直接返回原任务 ID。
5. 解析模型参数规格，校验 prompt / resolution / duration / ratio；清空规格未开放的参考媒体。规格缺失或非法返回 503。
6. 分辨率统一大写后查视频计费规则与价目档位。规则缺失返回 503，档位缺失返回 400。
7. 解析 `image_ids` / `video_ids` / `audio_ids` 中的 `file_id`，生成短期签名 URL，并统计去重后的数量与参考视频秒数。
8. 按规格校验参考媒体的数量、格式、大小、宽高、时长、帧率与编码。
9. 计算积分并生成计费快照。
10. 组装 `VideoGenerateRequest` 并再次校验媒体 URL。
11. 原子创建任务并扣费；任一步失败整体回滚。
12. 调用供应商提交接口，拿到上游 `task_id`。
13. 写入 `vendor_response`，交给对账循环。

## 4 参考文件解析

`file_id` 必须是平台上传接口返回的标识，不接受外部 URL。解析时校验以下条件：

- 属于当前用户；
- `purpose` 为 `reference` 或 `result`；
- `status` 为 `ready`；
- 未过期，且剩余有效期不少于 30 分钟；
- 媒体类型与所在数组匹配；
- 视频参考文件有可用的时长信息。

签名 URL 最长 2 小时有效，且不超过文件剩余有效期。签名地址不会返回给调用方，也不会写入任务快照。

## 4.1 参数规格

视频模型使用独立的 `param_spec`，由 `videoparam` 解析与校验。它覆盖 prompt 长度、分辨率枚举、时长范围、比例枚举，以及参考图片/音频/视频的数量、格式、大小、宽高、时长、帧率与编码。

- 规格缺失或非法：请求返回 503，`PARAM_SPEC_MISSING` / `PARAM_SPEC_INVALID`；
- 请求不满足规格：返回 400，带结构化 `error_code` 与 `param`；
- `max_count=0` 或规格中没有对应控件：该媒体视为不支持，字段被静默清空；
- 分辨率枚举必须与计费价目表的档位一致，管理端保存时交叉校验。

完整结构与错误码见 [视频参数规格](params/README.md)。

## 5 供应商适配

### 5.1 Metaso

Metaso 的请求体使用 `content` 数组。`buildVideoRequest` 按以下顺序组装：

1. `text`：调用方提示词；
2. `image_url`：`image_ids` 解析后的每个签名 URL，`role=reference_image`；
3. `video_url`：`video_ids` 解析后的每个签名 URL，`role=reference_video`；
4. `audio_url`：`audio_ids` 解析后的每个签名 URL，`role=reference_audio`。

`fast_mode` 固定为 `false`，不是公开字段。`model` 使用管理端配置的供应商模型 ID。

### 5.2 APIMart

APIMart 使用平铺字段。`buildAPIMartVideoRequest` 把平台字段翻译为 `prompt`、`duration`、`aspect_ratio`、`image_urls`、`video_urls`、`audio_urls`，并显式写死 `resolution=1080p`、`generate_audio=true`、`nsfw_check=false`、`seed=-1`；带参考素材时固定 `omni_reference_task_type=reference`。所有带默认值的上游字段都由代码显式下发，不依赖上游默认行为。详见 [magi-2.md](magi-2.md)。

## 6 任务状态与对账

视频任务与图像任务共用同一套状态机。上游状态按以下规则映射。

| 上游状态 | 平台状态 |
|---|---|
| `queued` / `running`（Metaso） | `processing` |
| `pending` / `processing`（APIMart） | `processing` |
| `succeeded` / `completed` | `completed` |
| `failed` / `cancelled` | `failed` |
| 查询失败或未知状态 | 保持 `processing` |

查询失败表示查询本身失败，不是任务失败。对账循环不会据此退款，任务保持 `processing` 等待下一轮；超过 30 分钟截止时间仍无法确认时，统一转 `failed` 并写入 `error_code=query_timeout`。

## 7 计费

视频计费使用独立于图片的规则表，详见 [视频计费设计](billing.md)。要点：

- 规则按模型配置，价目按输出分辨率分档；
- 图片按张（每请求前 N 张免费）、音频按个、参考视频按秒、输出按请求时长；
- 参考视频秒数按文件逐个向上取整；
- 所有数量在提交时已知，预扣即终值，完成后不做二次结算；
- 0 费率是显式免费，缺少档位则拒绝请求。

## 8 失败性质与结果

Metaso 与 APIMart 提交接口的失败性质如下。

| 场景 | 失败性质 | 处理 |
|---|---|---|
| 4xx、业务错误码、请求未发出 | `FailureRejected` | 转 `failed` 并退款 |
| 5xx、超时、连接中断、响应缺少 `task_id` | `FailureUnknown` | 保持 `processing`，超过 60 秒提交确认窗口后转 `failed` 并退款 |

失败或取消时按任务预扣的 `credits` 全额退款；退款金额与提交时扣费一致。

任务完成后只返回 `taskId`、`status` 与 `data.url`。视频结果不转存到对象存储，`data.url` 是上游的限时下载地址，没有 `file_id` 与 `url_vendor`。

## 9 模型设计文档

各模型的设计文档如下。

| 模型 | 文档 |
|---|---|
| `MiniMax-H3` | [minimax-h3.md](minimax-h3.md) |
| `MAGI-2` | [magi-2.md](magi-2.md) |

## 10 新增模型检查清单

新增视频模型时，按以下清单补齐：

1. 在 `supplier` 包实现 `VideoSupplier` 接口。
2. 在 `NewVideoSupplier` 与 `NewTaskQuerier` 注册供应商名称。
3. 确认参考媒体类型与上游 `role` 的映射。
4. 补充单元测试，覆盖成功、4xx、5xx、超时、任务查询状态映射。
5. 在 `docs/api/video/` 增加客户端文档，在 `docs/design/video/` 增加设计文档。
6. 在管理端配置供应商、供应商模型、模型类型、端点绑定。
7. 为该模型配置视频参数规格，见 [视频参数规格](params/README.md)；分辨率枚举必须与计费价目表一致。
8. 为该模型配置视频计费规则，至少包含一个与上游一致的分辨率档位；否则请求会被 fail-closed 拒绝。
