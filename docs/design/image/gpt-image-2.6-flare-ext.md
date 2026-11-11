# APIMart 图像设计（gpt-image-2.6-flare-ext）

## 1 范围

本文说明平台模型 `gpt-image-2.6-flare-ext` 的内部实现。除 `version` 取值外，其余设计与 [gpt-image-2.6-sunburst-ext](gpt-image-2.6-sunburst-ext.md) 一致。通用图像链路见 [图像生成链路设计](README.md)，调用方文档见 [客户端文档](../../api/image/gpt-image-2.6-flare-ext.md)。

2.6 Ext 与 2.5 Ext 的主要差异：

- 上游模型 ID 为 `gpt-image-2.6-ext`，通过 `version` 选择 `flare` / `sunburst`；
- 新增 `quality`、`output_format`、`complexity` 三个客户端可传参数；
- `output_format` 本期只开放 `png` / `jpeg` / `webp`，SVG 与 GIF 暂不接入；
- `resolution` 平台的 `1K` / `2K` / `4K` 发往上游时转小写。

## 2 供应商与端点

APIMart 使用异步任务接口。平台提交后拿到上游 `task_id`，再由对账循环轮询任务状态。

| 项目 | 值 |
|---|---|
| 供应商名称 | `apimart` |
| Base URL | APIMart API 地址 |
| 提交路径 | `/v1/images/generations` |
| 查询路径 | `/v1/tasks/{task_id}` |
| 认证 | `Authorization: Bearer <API Key>` |
| 供应商模型 ID | `gpt-image-2.6-ext` |

## 3 能力声明

APIMart 未实现 `ImageCapabilityProvider`，使用平台默认能力。

| 字段 | 默认值 | 影响 |
|---|---|---|
| `BackgroundSubmit` | `false` | handler 内同步提交，最长 30 秒 |
| `SupportsReferenceImages` | `true` | 参考图参与上游请求与计费 |
| `RequiresPixelSize` | `false` | `size` 按比例透传 |
| `SubmitTimeout` | 30 秒 | 使用包级 `SubmitTimeout` |

## 4 入参校验

APIMart 通过 `ImageRequestValidator` 在扣费前校验以下约束。这些约束按平台模型名登记在 `supplier/apimart.go` 的 `apimartImageModels`，由 `req.Model` 查表。

- 模型必须已登记，未登记直接拒绝（fail-closed）；
- `prompt` 去除首尾空格后不能为空；
- `size` 只接受 10 种比例：`1:1`、`3:2`、`2:3`、`4:3`、`3:4`、`5:4`、`4:5`、`16:9`、`9:16`、`21:9`；不接受像素尺寸；`auto` 是否开放由该模型的 `param_spec` 决定（当前配置未包含）；
- `resolution` 只接受 `1K`、`2K`、`4K`，平台值统一大写，发往上游时转小写；
- `quality` 为空或 `auto` / `low` / `medium` / `high` / `xhigh` / `max`；
- `output_format` 为空或 `png` / `jpeg` / `webp`；`svg` / `gif` 在本期直接拒绝；
- `complexity` 为空或 `low` / `medium` / `high`；
- 参考图去重后最多 16 张（`image_ids` 优先，为空时看 `image_urls`）。

`quality`、`output_format`、`complexity` 的枚举值大小写不敏感，控制器在校验前统一转小写。参数为空表示调用方未指定：`quality` 由 param_spec 补默认值 `auto`；`complexity` 没有默认值，直接不发送给上游。

校验失败在扣费前返回 400，不建任务、不扣费。

### 4.1 参数规格（param_spec）

本模型的参数约束由 `models.param_spec` 提供。字段定义与校验规则见 [参数规格文档](params/README.md)。

```json
{
  "params": [
    {
      "key": "prompt",
      "widget": "textarea",
      "label": "提示词",
      "group": "basic",
      "required": true,
      "min_length": 1,
      "max_length": 2000,
      "count_mode": "codepoint"
    },
    {
      "key": "size",
      "widget": "ratio_size",
      "label": "尺寸",
      "group": "basic",
      "default": "16:9",
      "ratios": [
        "1:1", "3:2", "2:3", "4:3", "3:4",
        "5:4", "4:5", "16:9", "9:16", "21:9"
      ]
    },
    {
      "key": "resolution",
      "widget": "segmented",
      "label": "分辨率",
      "group": "basic",
      "default": "1K",
      "options": ["1K", "2K", "4K"]
    },
    {
      "key": "quality",
      "widget": "segmented",
      "label": "质量",
      "group": "advanced",
      "default": "auto",
      "options": [
        { "label": "自动", "value": "auto" },
        { "label": "低", "value": "low" },
        { "label": "中", "value": "medium" },
        { "label": "高", "value": "high" },
        { "label": "极高", "value": "xhigh" },
        { "label": "最高", "value": "max" }
      ]
    },
    {
      "key": "output_format",
      "widget": "segmented",
      "label": "输出格式",
      "group": "advanced",
      "default": "png",
      "options": [
        { "label": "PNG", "value": "png" },
        { "label": "JPEG", "value": "jpeg" },
        { "label": "WebP", "value": "webp" }
      ]
    },
    {
      "key": "complexity",
      "widget": "segmented",
      "label": "画面复杂度",
      "group": "advanced",
      "options": [
        { "label": "低", "value": "low" },
        { "label": "中", "value": "medium" },
        { "label": "高", "value": "high" }
      ]
    },
    {
      "key": "images",
      "path": "image_ids",
      "widget": "ref_images",
      "label": "参考图",
      "group": "reference",
      "max_count": 16,
      "max_file_bytes": 10485760,
      "max_total_bytes": 268435456,
      "allowed_mime_types": ["image/png", "image/jpeg", "image/webp"]
    }
  ]
}
```

说明：

- `version=flare` 是上游风格字段，属于适配层行为，不写入 `param_spec`；
- `size` 只暴露比例，`widget` 为 `ratio_size`，不配置 `modes` 与 `pixel`；
- `quality` 默认 `auto`，总是显式下发；`complexity` 不设默认，用户不选就不发送；
- `output_format` 默认 `png`；SVG / GIF 不进入 options，服务端会在扣费前拒绝；
- 参考图单张 10MB、合计 256MB，接口上限 16 张；
- prompt 上限 2000 是平台策略，与客户端现有上限一致。

## 5 请求体转换

`buildRequest` 写入以下字段：

| 上游字段 | 值 |
|---|---|
| `model` | `gpt-image-2.6-ext` |
| `prompt` | 调用方提示词 |
| `size` | 调用方画幅比例，已由控制器补齐默认值 |
| `resolution` | 调用方分辨率档位，按模型规格转成 `1k` / `2k` / `4k` |
| `quality` | 非空时写入，统一小写 |
| `output_format` | 非空时写入，统一小写 |
| `complexity` | 非空时写入，统一小写 |
| `n` | 固定 `1` |
| `image_urls` | 非空时写入 |
| `version` | 取自平台模型规格（`gpt-image-2.6-flare-ext` → `flare`） |

`quality` / `output_format` / `complexity` 只在该模型规格中登记时才允许非空；2.5、gpt-image-2、MOSS 的规格没有这三个字段，控制器会清空调用方传入的值，确保它们不会进入上游请求。`aspect_ratio` 不作为平台字段，收到时按未知字段忽略。

## 6 提交方式与超时

`ImageGenerate` 使用 `common.OutboundHTTPClient`，提交超时为包级 `SubmitTimeout`（30 秒）。请求只携带 `Content-Type` 与 `Authorization`，不传 `Idempotency-Key` 和 `X-APIMart-Response-Version`。

失败性质与 2.5 一致：

| 场景 | 失败性质 | 处理 |
|---|---|---|
| 4xx、业务 `code` 不是 200、请求未发出 | `FailureRejected` | 退款 |
| 5xx、超时、连接中断、响应读取失败、非预期的 202 | `FailureUnknown` | 保持 `processing`，超过 60 秒提交确认窗口后转 `failed` 并退款 |
| 缺少任务 ID | `FailureUnknown` | 同上 |

## 7 任务状态与对账

对账循环查询 `/v1/tasks/{task_id}?language=zh`。上游 `completed` 映射为 `completed`，`failed` / `cancelled` 映射为 `failed`；查询失败或未识别状态时保持 `processing`。图片任务创建后 15 分钟仍未完成时转 `failed` 并退款。4K 与 `quality=xhigh` / `max` 的耗时可能更长，当前沿用 15 分钟窗口，后续可根据线上耗时再评估。

## 8 结果转存与输出格式

APIMart 不声明 `RequireResultFetch`，转存失败只记录日志，任务仍按 `completed` 处理，`data.url_vendor` 保留上游地址。成图下载上限为 32MB；4K PNG 在极复杂画面下可能接近该上限，超限时任务仍完成但没有 `file_id`，可回退使用 `url_vendor`。

`output_format` 为 `jpeg` / `webp` 时，`filestore` 按下载内容识别扩展名，任务结果的 `file_id` 分别是 `.jpg` / `.webp`；客户端下载文件名需要跟随实际扩展名，不能写死 `.png`。

## 9 计费

单张价格只由 `version` × `resolution` 决定：`quality`、`output_format`、`complexity`、比例、参考图张数都不影响单价。

计费公式与其他图像模型一致：

```text
本次总积分 = 基础积分（或命中的参数组合积分）+ 参考图张数 × 每张参考图积分
```

flare 与 sunburst 是两个平台模型，可以分别配置基础积分；计费条件仍只支持 `model` / `prompt` / `size` / `resolution`。`quality` 不进入计费条件，避免和上游「质量不影响价格」的定价规则不一致。

## 10 配置

管理端配置如下。

| 配置项 | 值 |
|---|---|
| 供应商名称 | `apimart` |
| Base URL | APIMart API 地址 |
| 供应商模型 ID | `gpt-image-2.6-ext` |
| 平台模型名 | `gpt-image-2.6-flare-ext` |
| 模型类型 | `image` |
| 端点 | `/v1/images/generations` |
| 计费规则 | 基础积分 + 可选的参考图单价 |

## 11 代码与测试

| 内容 | 位置 |
|---|---|
| 供应商实现 | `supplier/apimart.go` |
| 供应商单元测试 | `supplier/apimart_test.go` |
| 参数规格校验 | `imageparam/spec.go`、`imageparam/seeds/gpt-image-2.6-ext.json` |
| 参数规格测试 | `imageparam/spec_test.go` |
| 控制器字段映射与清空 | `controller/image.go` |
| 控制器请求契约测试 | `controller/image_test.go` |
| 任务状态与对账 | `model/task.go`、`controller/image.go` |
