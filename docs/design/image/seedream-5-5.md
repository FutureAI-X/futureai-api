# APIMart 图像设计（seedream-5-5）

## 1 范围

本文说明平台模型 `seedream-5-5` 的内部实现。通用图像链路见 [图像生成链路设计](README.md)，调用方文档见 [客户端文档](../../api/image/seedream-5-5.md)。

本模型只开放图片生成：不支持图层拆分、文字 OCR、透明背景、`output_format` 等可选能力。

## 2 供应商与端点

APIMart 使用异步任务接口。平台提交后拿到上游 `task_id`，再由对账循环轮询任务状态。

| 项目 | 值 |
|---|---|
| 供应商名称 | `apimart` |
| Base URL | APIMart API 地址 |
| 提交路径 | `/v1/images/generations` |
| 查询路径 | `/v1/tasks/{task_id}` |
| 认证 | `Authorization: Bearer <API Key>` |
| 供应商模型 ID | `seedream-5-5` |

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
- `size` 只接受 10 种比例：`1:1`、`4:3`、`3:4`、`16:9`、`9:16`、`3:2`、`2:3`、`21:9`、`2:1`、`1:2`；不接受像素尺寸和档位写法；`auto` 是否开放由该模型的 `param_spec` 决定（当前配置未包含）；
- `resolution` 只接受 `1K`、`1.5K`、`2K`，不支持 `4K`；平台值统一大写，发往上游时保持大写；
- 参考图去重后最多 10 张（`image_ids` 优先，为空时看 `image_urls`）；
- 不开放 `quality` / `output_format` / `complexity`：控制器会清空调用方传入的值，供应商适配层对非空值同样拒绝（fail-closed）。

校验失败在扣费前返回 400，不建任务、不扣费。

### 4.1 为什么不开放像素尺寸

上游的精确像素写法本身可用，但计费按实际总像素分档。平台在扣费前只能看到请求里的 `size` 和 `resolution`，无法用一条参数组合规则准确覆盖任意像素尺寸，容易出现少扣或多扣。本期只开放比例 + 分辨率，让计费规则与上游档位一一对应。

上游的档位写法（`size: "2K"`）会让模型自行决定比例，输出尺寸不可预期，同样不开放。

### 4.2 参数规格（param_spec）

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
        "1:1", "4:3", "3:4", "16:9", "9:16",
        "3:2", "2:3", "21:9", "2:1", "1:2"
      ]
    },
    {
      "key": "resolution",
      "widget": "segmented",
      "label": "分辨率",
      "group": "basic",
      "default": "1K",
      "options": ["1K", "1.5K", "2K"]
    },
    {
      "key": "images",
      "path": "image_ids",
      "widget": "ref_images",
      "label": "参考图",
      "group": "reference",
      "max_count": 10,
      "max_file_bytes": 31457280,
      "max_total_bytes": 314572800,
      "allowed_mime_types": ["image/png", "image/jpeg", "image/webp"]
    }
  ]
}
```

说明：

- `size` 只暴露比例，不配置 `modes` 与 `pixel`；
- `resolution` 默认 `1K`，与平台其他图像模型一致；1.5K 是本模型独有档位；
- 参考图单张 30MB、合计 300MB，接口上限 10 张；
- prompt 上限 2000 是平台策略。

## 5 请求体转换

`buildRequest` 写入以下字段：

| 上游字段 | 值 |
|---|---|
| `model` | `seedream-5-5` |
| `prompt` | 调用方提示词 |
| `size` | 调用方画幅比例，已由控制器补齐默认值 |
| `resolution` | 调用方分辨率档位，保持大写 |
| `n` | 固定 `1` |
| `watermark` | 固定 `false` |
| `image_urls` | 非空时写入 |

以下字段一律不发送，由上游使用默认值：

- `background`：默认 `auto`，平台不开放透明背景；
- `output_format`：默认 `png`；
- `optimize_prompt_options`：上游默认启用 `standard`，且无法关闭；
- `quality` / `complexity`：模型不支持；
- `layer_decomposition` / `layer_decomposition_options`：平台不开放图层拆分；
- `stream` / `tools` / `sequential_image_generation`：模型不支持。

## 6 提交方式与超时

`ImageGenerate` 使用 `common.OutboundHTTPClient`，提交超时为包级 `SubmitTimeout`（30 秒）。请求只携带 `Content-Type` 与 `Authorization`，不传 `Idempotency-Key` 和 `X-APIMart-Response-Version`。

失败性质与其他 APIMart 模型一致：

| 场景 | 失败性质 | 处理 |
|---|---|---|
| 4xx、业务 `code` 不是 200、请求未发出 | `FailureRejected` | 退款 |
| 5xx、超时、连接中断、响应读取失败、非预期的 202 | `FailureUnknown` | 保持 `processing`，超过 60 秒提交确认窗口后转 `failed` 并退款 |
| 缺少任务 ID | `FailureUnknown` | 同上 |

## 7 任务状态与对账

对账循环查询 `/v1/tasks/{task_id}?language=zh`。上游 `completed` 映射为 `completed`，`failed` / `cancelled` 映射为 `failed`；查询失败或未识别状态时保持 `processing`。图片任务创建后 15 分钟仍未完成时转 `failed` 并退款。

失败任务会读取 `data.error.reason` 并归一化为平台失败原因，控制器据此生成用户可见的错误码与文案：

| 上游 `reason` | 平台错误码 | 用户文案 |
|---|---|---|
| `content_policy_violation` | `content_policy_violation` | 内容审核未通过，请修改提示词或参考图后重试，积分已退回 |
| `invalid_parameter` / `unsupported_option` | `invalid_parameter` | 生成参数不受支持，请调整后重试，积分已退回 |
| `invalid_media` | `invalid_media` | 参考图格式或内容无效，积分已退回 |
| `media_unreachable` | `media_unreachable` | 参考图无法访问，请重新上传后重试，积分已退回 |
| `generation_timeout` | `generation_timeout` | 生成超时，积分已退回，请稍后重试 |
| `service_unavailable` | `service_unavailable` | 上游服务繁忙，积分已退回，请稍后重试 |
| `delivery_failed` / `internal_error` / `unknown_error` | `upstream_failed` | 上游生成失败，积分已退回 |
| 未知或缺失 | `upstream_failed` | 任务失败，积分已退回 |
| 上游状态为 `cancelled` | `cancelled` | 任务已取消，积分已退回 |

上游原文 `data.error.message` 只写入 `query_response` 供排查，不透传给调用方。

## 8 结果转存与输出格式

APIMart 不声明 `RequireResultFetch`，转存失败只记录日志，任务仍按 `completed` 处理，`data.url_vendor` 保留上游地址。成图下载上限为 32MB。

上游默认输出 PNG，`file_id` 扩展名跟随实际图片格式。结果图在上游只保留 3 天，平台会在任务完成后立即下载转存。

## 9 计费

上游按输出图片的实际总像素分档：不超过 2,610,000 像素为低档，超过为高档；1K / 1.5K 对应低档，2K 对应高档。每张参考图单独收费，没有首张免费。

平台侧建议配置：

- 基础积分 = 低档单价；
- 参数组合 `resolution=2K` = 高档单价（如 1K 与 1.5K 价格不同，可分别配置 `resolution=1K` / `1.5K` 的组合价）；
- `RefImageCredits` = 每张参考图单价。

计费公式与其他图像模型一致：

```text
本次总积分 = 基础积分（或命中的参数组合积分）+ 参考图张数 × 每张参考图积分
```

计费条件仍只支持 `model` / `prompt` / `size` / `resolution`。

## 10 配置

管理端配置如下。

| 配置项 | 值 |
|---|---|
| 供应商名称 | `apimart` |
| Base URL | APIMart API 地址 |
| 供应商模型 ID | `seedream-5-5` |
| 平台模型名 | `seedream-5-5` |
| `owner` | `ByteDance` |
| 模型类型 | `image` |
| 端点 | `/v1/images/generations` |
| 计费规则 | 基础积分 + 可选的参考图单价 |

`param_spec` 的种子只在服务启动时写入「规格为空」的模型。上线顺序是：先在管理端创建模型并绑定供应商，再重启服务让种子生效；也可以直接通过管理端的参数规格接口粘贴第 4.2 节的 JSON。

## 11 代码与测试

| 内容 | 位置 |
|---|---|
| 供应商实现 | `supplier/apimart.go` |
| 供应商单元测试 | `supplier/apimart_test.go` |
| 参数规格校验 | `imageparam/spec.go`、`imageparam/seeds/seedream-5-5.json` |
| 参数规格测试 | `imageparam/spec_test.go` |
| 失败原因文案 | `controller/image.go`、`controller/image_test.go` |
| 任务状态与对账 | `model/task.go`、`controller/image.go` |
