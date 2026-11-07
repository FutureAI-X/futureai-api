# 图像生成链路设计

本文面向维护者，说明图像生成从 HTTP 请求进入系统到结果落库的完整设计。调用方文档见 [客户端图像生成文档](../../api/image/README.md)。

## 1 范围

本文覆盖 `/v1/images/generations` 的控制器、供应商适配层、计费、任务状态机、对账与结果转存。单个供应商或模型的差异见第 9 章的模型设计文档。

## 2 模块与职责

各模块的职责与代码位置如下。

| 模块 | 职责 | 代码 |
|---|---|---|
| 控制器 | 解析请求、选择供应商、扣费、建任务、提交上游 | `controller/image.go` |
| 供应商接口 | 定义 `ImageGenerate`、`TaskQuery` 与能力声明 | `supplier/supplier.go` |
| 供应商实现 | 把平台标准字段翻译成上游请求，解析上游响应 | `supplier/*.go` |
| 任务模型 | 任务状态、扣费、退款、对账查询 | `model/task.go` |
| 结果转存 | 下载上游成图并写入对象存储 | `filestore/filestore.go` |
| 出站客户端 | 共享连接池、SSRF 防护、超时策略 | `common/outbound.go` |

## 3 请求流程

控制器按以下顺序处理请求。任何参数错误都在扣费之前返回，不产生任务行。

1. 解析请求体并补齐标准默认值。`size` 缺省为 `16:9`，`resolution` 缺省为 `1K`。
2. 保存调用方原始请求快照。快照必须在 file_id 解析与字段清理之前生成。
3. 查找端点、模型、模型与端点的绑定关系，选择第一个可用供应商。
4. 解密供应商 API Key，构造供应商实现，读取能力声明。
5. 处理参考图。不支持参考图的供应商会在这里清空 `image_urls` 与 `image_ids`。
6. 调用供应商的请求预校验（如果实现了 `ImageRequestValidator`）。
7. 计算积分。参考图附加计费只在供应商支持参考图时参与。
8. 处理幂等键。命中已有任务时直接返回原任务 ID。
9. 把 `image_ids` 解析成短期签名 URL。仅支持参考图的供应商会执行。
10. 创建任务并扣费。同步提交的供应商状态为 `pending`，后台提交的供应商状态为 `submitting`。
11. 提交上游。后台提交的供应商立即返回 `taskId`，由 goroutine 完成调用；其余供应商在 handler 内等待。
12. 上游受理后写入 `vendor_response`，再交给对账循环查询结果。

## 4 供应商能力

供应商通过可选接口 `ImageCapabilityProvider` 声明能力。未实现该接口时使用默认值。

| 字段 | 默认值 | 含义 |
|---|---|---|
| `BackgroundSubmit` | `false` | 为 `true` 时控制器不在 handler 内等待上游 |
| `SupportsReferenceImages` | `true` | 为 `false` 时忽略参考图，且不计参考图费用 |
| `RequiresPixelSize` | `false` | 为 `true` 时 `size` 必须是像素尺寸 |
| `SubmitTimeout` | `0` | 为 `0` 时使用包级 `SubmitTimeout`，否则使用声明值 |

请求预校验通过可选接口 `ImageRequestValidator` 实现。控制器在扣费、建任务和调用上游之前调用它，返回错误时直接 400。

## 5 计费

计费公式如下：

```text
本次总积分 = 基础积分（或命中的参数组合积分）+ 参考图张数 × 每张参考图积分
```

基础积分可以为 0，表示免费模型。参数组合条件只匹配 `model`、`prompt`、`size`、`resolution`，值必须为字符串。参考图部分同时受三个条件约束：模型类型为图像、端点在图片白名单内、供应商支持参考图。供应商不支持参考图时跳过参考图计费，并记录配置不一致日志。

`resolveCredits` 的签名包含 `refImagesSupported` 参数，调用方必须把供应商能力传进来，否则会出现「忽略参考图但仍收费」。

## 6 任务状态机

任务状态分为终态与非终态。只有终态允许退款，非终态由对账循环继续接管。

| 状态 | 终态 | 用途 |
|---|---|---|
| `pending` | 否 | 已扣费，尚未提交上游 |
| `submitting` | 否 | 已交给后台同步提交的 goroutine，尚未拿到结果 |
| `submitted` | 否 | 上游已受理，正在轮询 |
| `unknown` | 否 | 结果无法确认，等待人工核对或超龄兜底 |
| `completed` | 是 | 已出图 |
| `failed` | 是 | 上游明确失败，已退款 |
| `cancelled` | 是 | 上游取消，已退款 |
| `call_fail` | 是 | 上游拒绝、提交失败或超期无法确认，已退款 |

`submitting` 仅用于后台同步提交，对外查询时映射为 `pending`。状态映射由 `publicTaskStatus` 完成。

## 7 后台提交与对账

同步接口（当前为 MOSS）不能占用 HTTP handler 等待完整生成，否则会被网关、服务端 `WriteTimeout` 和调用方 SDK 任意一层截断。因此控制器使用 `BackgroundSubmit` 能力把任务交给 goroutine：

1. 任务以 `submitting` 落库，handler 立即返回 `taskId`。
2. goroutine 使用 `SubmitTimeout` 声明的超时调用上游。
3. 成功时写入 `vendor_response`；`SetTaskVendorResponse` 同时把状态推进到 `submitted`，随后交给对账循环。
4. 失败时进入 `call_fail` 并退款。
5. 进程重启或 goroutine 丢失时，任务停在 `submitting`。对账循环超过 `backgroundSubmitGrace` 后调用 `FailStaleSubmittingTask` 退款。

`FailStaleSubmittingTask` 在事务内加行锁并重新检查状态与 `vendor_response`，避免与后台成功写入竞争。

## 8 结果转存与失败语义

对账循环拿到 `completed` 后调用 `persistCompletedImage` 下载成图并写入对象存储。

失败分两类。`filestore.ErrSourceUnavailable` 表示上游结果资产不可用，包括 URL 非法、下载失败、404 和内容不是图片。其余错误表示平台存储侧失败。

供应商可以通过 `TaskQueryResponse.RequireResultFetch` 声明「必须成功下载结果才能判为完成」。当前 MOSS 使用该标记：成图下载失败会重试两次，仍失败则 `call_fail` 并退款。普通供应商保持尽力而为，存储失败不影响任务完成，`data.url_vendor` 仍可用于取图。

## 9 模型设计文档

各模型的设计文档如下。

| 模型 | 文档 |
|---|---|
| `moss-image-base-1.0` | [moss-image-base-1.0.md](moss-image-base-1.0.md) |
| `moss-image-sft-1.0` | [moss-image-sft-1.0.md](moss-image-sft-1.0.md) |
| `gpt-image-2-ext` / `gpt-image-2` | [gpt-image-2-ext.md](gpt-image-2-ext.md) |
| `gpt-image-2.5-flare-ext` | [gpt-image-2.5-flare-ext.md](gpt-image-2.5-flare-ext.md) |
| `gpt-image-2.5-sunburst-ext` | [gpt-image-2.5-sunburst-ext.md](gpt-image-2.5-sunburst-ext.md) |

## 10 新增模型检查清单

新增一个图像模型时，按以下清单补齐：

1. 在 `supplier` 包实现 `Supplier` 接口。同步接口还需要实现 `ImageCapabilityProvider` 和 `ImageRequestValidator`。
2. APIMart 模型要在 `supplier/apimart.go` 的 `apimartImageModels` 登记模型规格（size 集合、分辨率档位及大小写、参考图上限、version），未登记会在扣费前被拒绝。
3. 在 `NewSupplier` 与 `NewTaskQuerier` 注册供应商名称。
4. 同步接口使用 `common.LongOutboundHTTPClient`，避免通用客户端的响应头超时。
5. 在控制器侧确认参考图、计费与状态映射符合预期。
6. 补充供应商单元测试，覆盖成功、4xx、5xx、超时、结果缺失和 TaskQuery。
7. 在 `docs/api/image/` 增加客户端文档，在 `docs/design/image/` 增加设计文档。
8. 在管理端配置供应商、供应商模型、模型类型、端点绑定与基础积分。
