# MiniMax-H3-Image 设计

## 1 范围

本文说明平台模型 `MiniMax-H3-Image` / `MiniMax-H3-Image-Highspeed` 的内部实现。通用图像链路见 [图像生成链路设计](README.md)，客户端文档见 [MiniMax-H3-Image](../../api/image/minimax-h3-image.md)。

## 2 供应商与端点

| 项目 | 值 |
|---|---|
| 供应商名称 | `minimax` |
| Base URL | `https://api.minimax.cn` |
| 文生图路径 | `/v1/images/generations` |
| 图片编辑路径 | `/v1/images/edits` |
| 认证 | `Authorization: Bearer <API Key>` |
| 上游模型 ID | `MiniMax-H3-Image` 或 `MiniMax-H3-Image-Highspeed` |

H3 是同步接口：一次 POST 在图片生成完成后返回，没有 `task_id`，也没有查询接口。平台通过 `BackgroundSubmit` 把调用放到后台 goroutine，handler 立即返回 `taskId`。

## 3 能力声明

| 字段 | 值 | 影响 |
|---|---|---|
| `BackgroundSubmit` | `true` | handler 不等待最长 600 秒的生成 |
| `SupportsReferenceImages` | `true` | 有参考图时走编辑接口 |
| `RequiresPixelSize` | `true` | `size` 必须是像素尺寸 |
| `SubmitTimeout` | 620 秒 | 上游要求客户端不低于 600 秒 |

## 4 入参校验

`ValidateImageRequest` 在扣费与建任务之前执行：

- prompt 非空、不超过 32000 字符（平台 param_spec 建议限制 4000）；
- `size` 为 `宽x高`，总像素 655,360–20,000,000，宽高比 1:3–3:1；
- 参考图不超过 16 张；`image_urls` 必须是 http(s)；
- `output_format` 只能是 png / jpeg / webp（jpg 归一化为 jpeg）。

比例值、`auto`、`resolution` 都会被拒绝或忽略。

## 5 请求体转换

`buildMinimaxRequest` 只写以下字段：

| 上游字段 | 文生图 | 图像编辑 |
|---|---|---|
| `model` | VendorModelID | VendorModelID |
| `prompt` | 原样 | 原样 |
| `n` | `1` | `1` |
| `size` | 规范化后的像素尺寸 | 同左 |
| `aspect_ratio` / `area` | 不传 | 不传 |
| `disable_prompt_enhanced` | `false` | 不传（编辑接口没有该字段） |
| `images` | 不传 | `[{"image_url": ...}]` |
| `output_format` | 可选 | 可选 |
| `response_format` | `url` | `url` |
| `stream` | `false` | `false` |
| `aigc_watermark` | `false` | `false` |

文生图当前开启 Prompt 增强（`disable_prompt_enhanced=false`），以达到更好的生成效果；
输出尺寸仍由像素 `size` 决定。编辑接口始终开启 Prompt 增强，无法关闭。

## 6 参数规格（param_spec）

`imageparam/seeds/minimax-h3-image.json` 的关键字段：

- `size`：`pixel_size`，`min_pixels=655360`、`max_pixels=20000000`、宽高比 1:3–3:1；
- `size.presets`：比例按钮及其 1K 默认像素；
- `size.resolutions`：`1K/2K/4K` 的目标面积，客户端用于「比例 + 分辨率」自动填充；
- `ref_images`：最多 16 张；
- `output_format`：可选 `png/jpeg/webp`；
- 没有 `resolution` 参数，因此不会产生该请求字段。

`resolutions` 是纯 UI 元数据：客户端按 `sqrt(area × ratioW / ratioH)` 计算宽高，最终仍只提交 `size`。后端不解析这个字段，也不把它写进请求快照。

## 7 预扣估算

H3 实现 `supplier.TokenUsageEstimator`：

- 输出 token：等效公式为 `max(宽 × 高 / 1024, 4096)`；
  - `1024×1024` → `4096`；
  - `2048×2048` → `4096`；
  - `4096×4096` → `16384`；
  - `2720×1536` → `4080`（文档）；
  - `5440×3072` → `16320`（文档）；
  - 1K 和 2K 都是 4096 token，4K 是 16384；统一按面积/1024 会让 1K 低估 4 倍，统一按 16×16 会让 2K 高估 4 倍；
- 文生图文本 token：4000 + prompt 字符数（开启 Prompt 增强，文档示例约 3,174）；
- 图片编辑文本 token：14000 + prompt 字符数（编辑强制增强，文档示例约 12k）；
- 图片输入 token：参考图张数 × 6000。

计费层再乘以 `token_config.preauth_margin`（默认 1.2）得到预扣金额。估算偏保守，避免结算时把余额扣穿。

由于 H3 适配器已经实现了 `TokenUsageEstimator`，`token_config` 里的文本/参考图/输出换算参数对 H3 **不生效**；H3 规则通常留空即可，只有 `preauth_margin` 会被使用。

## 8 任务状态与结算

`TaskQuery` 不发起网络请求，只把后台写入 `vendor_response` 的结果还原成 `completed`，并带回 `usage`。

完成后 `pollOnce` 按 `billing_snapshot` 与上游 usage 结算：

- 实扣 < 预扣：追加一条 `refund`，退还差额；
- 实扣 > 预扣：追加一条 `deduct`，补扣差额，余额可以变为负数；
- usage 缺失：保留预扣，标记已结算并打 `[TOKEN_USAGE_MISSING]`；
- 快照损坏：保留预扣并打 `[MANUAL_RECONCILE]`。

结果**不转存对象存储**：`TaskQuery` 返回 `SkipResultPersist=true`，控制器跳过下载与 COS 上传，任务的 `file_id` 为空，`data.url` 直接使用上游原始地址（有效期 24 小时）。同时 `RequireResultFetch=false`，不会因为平台侧 CDN 获取失败而把上游已完成的图判为失败。

## 9 失败语义

- 非 2xx：完整响应说明上游明确失败；H3 只对成功图片计费，按 `rejected` 退款；
- 超时、连接中断、响应读取/解析失败：`unknown`，后台提交路径退款并打 `[MANUAL_RECONCILE]`；
- 上游明确不支持幂等，平台不自动重试；客户端重试由 `Idempotency-Key` 去重。

## 10 配置

| 配置项 | 值 |
|---|---|
| 供应商名称 | `minimax` |
| Base URL | `https://api.minimax.cn` |
| 模型 | `MiniMax-H3-Image`、`MiniMax-H3-Image-Highspeed` |
| 模型类型 | `image` |
| 端点 | `/v1/images/generations` |
| 计费规则 | `rule_type=per_token`，配置三段单价与可选的 `token_config` |

## 11 代码与测试

| 内容 | 位置 |
|---|---|
| 供应商实现 | `supplier/minimax.go` |
| 供应商单元测试 | `supplier/minimax_test.go` |
| 参数规格 | `imageparam/seeds/minimax-h3-image.json` |
| Token 计费与结算 | `model/token_billing.go` |
| 控制器预扣与对账 | `controller/image.go` |
| 像素尺寸控件 | `futureai-image/src/components/params/PixelSizePicker.tsx` |
