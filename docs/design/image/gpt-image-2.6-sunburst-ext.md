# APIMart 图像设计（gpt-image-2.6-sunburst-ext）

## 1 范围

本文说明平台模型 `gpt-image-2.6-sunburst-ext` 的内部实现。除 `version` 取值外，其余设计与 [gpt-image-2.6-flare-ext](gpt-image-2.6-flare-ext.md) 一致。通用图像链路见 [图像生成链路设计](README.md)，调用方文档见 [客户端文档](../../api/image/gpt-image-2.6-sunburst-ext.md)。

## 2 供应商与端点

| 项目 | 值 |
|---|---|
| 供应商名称 | `apimart` |
| Base URL | APIMart API 地址 |
| 提交路径 | `/v1/images/generations` |
| 查询路径 | `/v1/tasks/{task_id}` |
| 认证 | `Authorization: Bearer <API Key>` |
| 供应商模型 ID | `gpt-image-2.6-ext` |

## 3 能力声明

与 flare 版本相同：同步提交、支持参考图、`size` 按比例透传、提交超时 30 秒。

## 4 入参校验

与 flare 版本相同。`size` 为 10 种比例之一，`resolution` 为 `1K` / `2K` / `4K`，参考图最多 16 张；`quality` / `output_format` / `complexity` 的允许值也完全一致。所有校验在扣费前完成，失败返回 400。

### 4.1 参数规格（param_spec）

本模型与 [gpt-image-2.6-flare-ext](gpt-image-2.6-flare-ext.md) 共用同一份参数规格，只有适配层写入的 `version` 不同。完整 JSON 见 flare 文档第 4.1 节。

## 5 请求体转换

`buildRequest` 写入 `model`、`prompt`、`size`、`resolution`（转小写）、`n=1`，非空时写入 `image_urls` 与 `quality` / `output_format` / `complexity`。`applyImageVersion` 根据平台模型名写入 `version=sunburst`。

## 6 提交方式与超时

与 flare 版本相同：使用 `common.OutboundHTTPClient`，提交超时 30 秒，不传 `Idempotency-Key` 和 `X-APIMart-Response-Version`。失败性质与退款规则见 flare 文档第 6 节。

## 7 任务状态与对账

与 flare 版本相同。图片任务创建后 15 分钟仍未完成时转 `failed` 并退款。

## 8 结果转存与输出格式

与 flare 版本相同。`output_format=jpeg/webp` 时任务结果的 `file_id` 分别带 `.jpg` / `.webp` 扩展名。

## 9 计费

与 flare 版本相同。单张价格只由 `version` × `resolution` 决定，`quality` / `output_format` / `complexity` 不参与计费。

## 10 配置

| 配置项 | 值 |
|---|---|
| 供应商名称 | `apimart` |
| Base URL | APIMart API 地址 |
| 供应商模型 ID | `gpt-image-2.6-ext` |
| 平台模型名 | `gpt-image-2.6-sunburst-ext` |
| 模型类型 | `image` |
| 端点 | `/v1/images/generations` |
| 计费规则 | 基础积分 + 可选的参考图单价 |

## 11 代码与测试

与 flare 版本复用同一套实现与测试，见 [gpt-image-2.6-flare-ext.md](gpt-image-2.6-flare-ext.md) 第 11 节。
