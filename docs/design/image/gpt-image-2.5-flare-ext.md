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

APIMart 通过 `ImageRequestValidator` 在扣费前校验以下约束。这些约束是**模型规格**，不是供应商规格：同一个 APIMart 账号下不同模型的画幅、分辨率与参考图上限可能不同，因此按平台模型名登记在 `supplier/apimart.go` 的 `apimartImageModels`，由 `req.Model` 查表。

- 模型必须在 `apimartImageModels` 中登记，未登记直接拒绝（fail-closed），避免漏配后以错误的限制放行；
- `prompt` 去除首尾空格后不能为空；
- `size` 只接受该模型规格中的比例。本模型为 `1:1`、`16:9`、`9:16`、`4:3`、`3:4`、`3:2`、`2:3`、`5:4`、`4:5`、`21:9`；上游支持的 `auto` 刻意不开放，避免输出尺寸由上游内容决定、调用方无法预期；
- `resolution` 只接受该模型规格中的档位，本模型为 `1K`、`2K`、`4K`；平台标准值为大写，小写输入会被归一化；
- `image_ids` 优先，非空时去重后不超过该模型的参考图上限，本模型为 16 张；
- `image_urls` 在 `image_ids` 为空时去重后不超过该模型上限，本模型为 16 张。

参考图数量不再由控制器统一封顶——控制器只校验 `file_id` 的形态，数量上限随模型规格走，避免某个模型的上限误伤其他模型。

校验失败直接返回 400，不扣费、不建任务。参考图字段的优先级与平台通用规则见 [客户端图像生成文档](../../api/image/README.md#4-参考图)。

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
        "1:1", "16:9", "9:16", "4:3", "3:4",
        "3:2", "2:3", "5:4", "4:5", "21:9"
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

- `size` 只暴露比例，`widget` 为 `ratio_size`，不配置 `modes` 与 `pixel`。
- `resolution` 在规格中统一为大写；本模型发往上游时保持大写，属于适配层行为，不写入 `param_spec`。
- `version=flare` 是上游风格字段，属于适配层行为，不写入 `param_spec`。
- prompt 上限 2000 是平台策略，与客户端现有上限一致。
- 参考图单张 10MB、合计 256MB，接口上限 16 张。
- 当前实现已接入 `imageparam` 引擎；`supplier/apimart.go` 的旧校验暂时保留作为双保险。

## 5 请求体转换

`buildRequest` 写入以下字段：

| 上游字段 | 值 |
|---|---|
| `model` | `gpt-image-2.5-ext` |
| `prompt` | 调用方提示词 |
| `size` | 调用方画幅比例，已由控制器补齐默认值 |
| `resolution` | 调用方分辨率档位，按模型规格转成 `1K` / `2K` / `4K`（gpt-image-2.5-ext 用大写） |
| `n` | 固定 `1` |
| `image_urls` | 非空时写入 |
| `version` | 取自平台模型规格（`gpt-image-2.5-flare-ext` → `flare`） |

两个平台模型共用同一个供应商模型 ID，调用方请求体里的 `model` 已不足以区分风格版本。`applyImageVersion` 按调用方原始模型名查 `apimartImageModels`，取到规格里的 `version` 写入请求体（`gpt-image-2.5-flare-ext` → `version=flare`）。规格表同时承载 size 集合、分辨率档位、参考图上限与 resolution 大小写；新增映射到 APIMart 的平台模型时，必须在该表中登记完整规格，未登记的模型会在扣费前被拒绝。

`buildRequest` 通过 `spec.formatResolution` 转换大小写：gpt-image-2.5-ext 使用大写 `1K` / `2K` / `4K`，gpt-image-2 / gpt-image-2-ext 使用小写 `1k` / `2k` / `4k`。校验阶段按大小写不敏感匹配，调用方传入任意大小写都能命中。

## 6 提交方式与超时

`ImageGenerate` 使用 `common.OutboundHTTPClient`，提交超时为包级 `SubmitTimeout`（30 秒）。

请求刻意只携带 `Content-Type` 与 `Authorization`，**不传** `Idempotency-Key` 和 `X-APIMart-Response-Version`。平台自身已按 `Idempotency-Key` 做任务级去重，不需要上游再提供一层；不带响应版本头时，上游固定返回 HTTP 200 + `data[].task_id`。

| 成功结构 | 任务 ID 位置 |
|---|---|
| HTTP 200，`code=200` | `data[].task_id` |

失败性质如下。

| 场景 | 失败性质 | 处理 |
|---|---|---|
| 本地序列化失败、构建请求失败、4xx | `FailureRejected` | 退款 |
| 5xx、超时、连接中断、响应读取失败 | `FailureUnknown` | 保持 `processing`，超过 60 秒提交确认窗口后转 `failed` 并退款 |
| 业务 `code` 不是 200 | `FailureRejected` | 退款 |
| 响应中缺少任务 ID | `FailureUnknown` | 保持 `processing`，超过 60 秒提交确认窗口后转 `failed` 并退款 |
| 非预期的 HTTP 202 | `FailureUnknown` | 保持 `processing`，超过 60 秒提交确认窗口后转 `failed` 并退款 |

`FailureUnknown` 的语义是「上游可能已经建单」。任务保持 `processing`，由 `submit_deadline` 兜底；超过 60 秒仍没有 `vendor_response` 时转 `failed` 并退款，同时记录 `[SUBMIT_UNKNOWN]` 供事后对账。

## 7 任务状态与对账

对账循环调用 `TaskQuery`，向 `/v1/tasks/{task_id}?language=zh` 发起查询。上游状态按以下规则映射。

| 上游状态 | 平台状态 |
|---|---|
| `completed` | `completed` |
| `failed` / `cancelled` | `failed` |
| `pending` / `processing` | 保持 `processing` |
| 查询失败 | 保持 `processing` |
| 未识别的状态 | 保持 `processing`，下一轮继续查询 |

`task_id` 来自上游响应，拼接查询 URL 前必须通过字符集校验。校验失败按查询失败处理，不拼接 URL。

图片任务的结果确认截止时间为创建后 15 分钟；超过后仍未完成，转 `failed` 并写入 `error_code=query_timeout`。

## 8 结果转存

`TaskQuery` 在 `completed` 时返回上游图片地址或 `b64_json`。对账循环调用 `persistCompletedImage` 下载并转存。

APIMart 不声明 `RequireResultFetch`，因此转存失败只记录日志，任务仍按 `completed` 处理，`data.url_vendor` 保留上游地址。

## 9 失败性质与退款

任务只有 `processing` / `completed` / `failed` 三种状态。`failed` 表示已退款，失败原因写入 `error_code` / `error_message`。图片任务创建后 15 分钟仍未完成时，对账循环转 `failed` 并退款，同时记录 `[需人工对账]` 日志。

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
| 模型规格（size / resolution / 参考图上限 / version） | `supplier/apimart.go` 的 `apimartImageModels` |
| 控制器计费与参考图 | `controller/image.go` |
| 任务状态与对账 | `model/task.go`、`controller/image.go` |
