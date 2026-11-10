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

1. 解析请求体并按模型参数规格补齐默认值。`size` 缺省为 `16:9`，`resolution` 缺省为 `1K`；规格声明 `quality` / `output_format` 时分别补 `auto` / `png`，`complexity` 不补默认值。
2. 保存调用方原始请求快照。快照必须在 file_id 解析与字段清理之前生成。
3. 查找端点、模型、模型与端点的绑定关系，选择第一个可用供应商。
4. 解密供应商 API Key，构造供应商实现，读取能力声明。
5. 处理参考图。不支持参考图的供应商会在这里清空 `image_urls` 与 `image_ids`。
6. 调用供应商的请求预校验（如果实现了 `ImageRequestValidator`）。
7. 计算积分。参考图附加计费只在供应商支持参考图时参与。
8. 处理幂等键。命中已有任务时直接返回原任务 ID。
9. 把 `image_ids` 解析成短期签名 URL。仅支持参考图的供应商会执行。
10. 创建任务并扣费。任务统一以 `processing` 落库，同时写入 `submit_deadline` 与 `deadline_at`。
11. 提交上游。后台提交的供应商立即返回 `taskId`，由 goroutine 完成调用；其余供应商在 handler 内等待。
12. 上游受理后写入 `vendor_response`，再交给对账循环查询结果。

## 4 供应商能力

供应商通过可选接口 `ImageCapabilityProvider` 声明能力。未实现该接口时使用默认值。

| 字段 | 默认值 | 含义 |
|---|---|---|
| `BackgroundSubmit` | `false` | 为 `true` 时控制器不在 handler 内等待上游 |
| `SupportsReferenceImages` | `true` | 为 `false` 时忽略参考图，且不计参考图费用 |
| `RequiresPixelSize` | `false` | 供应商级声明，控制器不据此校验；模型级 `size` 约束见 `SizeRule` |
| `SubmitTimeout` | `0` | 为 `0` 时使用包级 `SubmitTimeout`，否则使用声明值 |

请求预校验通过可选接口 `ImageRequestValidator` 实现。控制器在扣费、建任务和调用上游之前调用它，返回错误时直接 400。

图像模型的 `size` 校验按模型规格 `SizeRule`（`supplier/size.go`）执行：调用方根据模型能力自行选择比例或像素写法，平台只校验格式与取值，不做比例与像素之间的换算。

## 5 计费

计费公式如下：

```text
本次总积分 = 基础积分（或命中的参数组合积分）+ 参考图张数 × 每张参考图积分
```

基础积分可以为 0，表示免费模型。参数组合条件只匹配 `model`、`prompt`、`size`、`resolution`，值必须为字符串。参考图部分同时受三个条件约束：模型类型为图像、端点在图片白名单内、供应商支持参考图。供应商不支持参考图时跳过参考图计费，并记录配置不一致日志。

`resolveCredits` 的签名包含 `refImagesSupported` 参数，调用方必须把供应商能力传进来，否则会出现「忽略参考图但仍收费」。

## 6 任务状态机

任务只有三个状态。`processing` 覆盖提交、生成和结果确认；失败原因由 `error_code` 表达。

| 状态 | 终态 | 用途 |
|---|---|---|
| `processing` | 否 | 提交中、生成中或结果确认中 |
| `completed` | 是 | 已出图，不退款 |
| `failed` | 是 | 失败，已退款；包含上游拒绝、超时、取消、内部错误 |

阶段不靠状态区分，靠字段判断：

| 字段 | 用途 |
|---|---|
| `vendor_response` | 空表示尚未拿到上游响应 |
| `submit_deadline` | 提交确认截止；超时仍无响应则 `failed` |
| `deadline_at` | 整体截止；超时仍未完成则 `failed` |
| `error_code` / `error_message` | 失败原因 |

- 异步提交确认窗口：60 秒；
- 图片结果确认：创建后 15 分钟；
- MOSS 同步生成：创建后 15 分钟；
- 视频结果确认：创建后 30 分钟。

## 7 后台提交与对账

同步接口（当前为 MOSS）不能占用 HTTP handler 等待完整生成，否则会被网关、服务端 `WriteTimeout` 和调用方 SDK 任意一层截断。因此控制器使用 `BackgroundSubmit` 能力把任务交给 goroutine：

1. 任务以 `processing` 落库，同时写入 `submit_deadline` 与 `deadline_at`，handler 立即返回 `taskId`。
2. goroutine 使用 `SubmitTimeout` 声明的超时调用上游。
3. 成功时写入 `vendor_response`；任务保持 `processing`，随后交给对账循环。
4. 失败时进入 `failed` 并退款，写入 `error_code`。
5. 进程重启或 goroutine 丢失时，任务停在 `processing` 且 `vendor_response` 为空；超过 `submit_deadline` 后由对账循环调用 `FailStaleSubmitTask` 退款。

`FailStaleSubmitTask` 在事务内加行锁并重新检查状态与 `vendor_response`，避免与后台成功写入竞争。

## 8 结果转存与失败语义

对账循环拿到 `completed` 后调用 `persistCompletedImage` 下载成图并写入对象存储。

失败分两类。`filestore.ErrSourceUnavailable` 表示上游结果资产不可用，包括 URL 非法、下载失败、404 和内容不是图片。其余错误表示平台存储侧失败。

供应商可以通过 `TaskQueryResponse.RequireResultFetch` 声明「必须成功下载结果才能判为完成」。当前 MOSS 使用该标记：成图下载失败会重试两次，仍失败则进入 `failed` 并退款。普通供应商保持尽力而为，存储失败不影响任务完成，`data.url_vendor` 仍可用于取图。

供应商还可以通过 `TaskQueryResponse.FailureReason` 声明归一化后的失败原因（取值见 `supplier.FailureReason*` 常量）。控制器把它翻译成用户可见的 `error.code` 与 `error.message`；未知原因回退到默认文案。上游原始错误不直接透传，只写入 `query_response` 供排查。

## 9 模型设计文档

各模型的设计文档如下。

| 模型 | 文档 |
|---|---|
| `moss-image-base-1.0` | [moss-image-base-1.0.md](moss-image-base-1.0.md) |
| `moss-image-sft-1.0` | [moss-image-sft-1.0.md](moss-image-sft-1.0.md) |
| `gpt-image-2-ext` / `gpt-image-2` | [gpt-image-2-ext.md](gpt-image-2-ext.md) |
| `gpt-image-2.5-flare-ext` | [gpt-image-2.5-flare-ext.md](gpt-image-2.5-flare-ext.md) |
| `gpt-image-2.5-sunburst-ext` | [gpt-image-2.5-sunburst-ext.md](gpt-image-2.5-sunburst-ext.md) |
| `gpt-image-2.6-flare-ext` | [gpt-image-2.6-flare-ext.md](gpt-image-2.6-flare-ext.md) |
| `gpt-image-2.6-sunburst-ext` | [gpt-image-2.6-sunburst-ext.md](gpt-image-2.6-sunburst-ext.md) |
| `seedream-5-5` | [seedream-5-5.md](seedream-5-5.md) |

参数规格的字段定义、校验顺序与两端复用方式见 [params/README.md](params/README.md)。该目录描述的是待实现的设计，当前代码仍使用供应商适配层与客户端常量中的硬编码约束。

## 10 新增模型检查清单

新增一个图像模型时，按以下清单补齐：

1. 在 `supplier` 包实现 `Supplier` 接口。同步接口还需要实现 `ImageCapabilityProvider` 和 `ImageRequestValidator`。
2. APIMart 模型要在 `supplier/apimart.go` 的 `apimartImageModels` 登记模型规格（size 集合、分辨率档位及大小写、参考图上限、version、模型级固定字段如 watermark，以及 quality / output_format / complexity 等可选参数的允许值），未登记会在扣费前被拒绝。
3. 在 `NewSupplier` 与 `NewTaskQuerier` 注册供应商名称。
4. 同步接口使用 `common.LongOutboundHTTPClient`，避免通用客户端的响应头超时。
5. 在控制器侧确认参考图、计费与状态映射符合预期。
6. 补充供应商单元测试，覆盖成功、4xx、5xx、超时、结果缺失和 TaskQuery。
7. 在 `docs/api/image/` 增加客户端文档，在 `docs/design/image/` 增加设计文档。
8. 在管理端配置供应商、供应商模型、模型类型、端点绑定与基础积分。
