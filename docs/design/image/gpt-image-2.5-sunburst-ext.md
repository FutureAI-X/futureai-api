# APIMart 图像设计（gpt-image-2.5-sunburst-ext）

## 1 范围

本文说明平台模型 `gpt-image-2.5-sunburst-ext` 的内部实现。除 `version` 取值外，其余设计与 [gpt-image-2.5-flare-ext](gpt-image-2.5-flare-ext.md) 一致。通用图像链路见 [图像生成链路设计](README.md)，调用方文档见 [客户端文档](../../api/image/gpt-image-2.5-sunburst-ext.md)。

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

与 flare 版本相同，使用平台默认能力：同步提交、支持参考图、`size` 按比例透传、提交超时 30 秒。

## 4 入参校验

与 flare 版本相同。APIMart 通过 `ImageRequestValidator`，按 `apimartImageModels[req.Model]` 的模型规格校验提示词、画幅、分辨率和参考图数量：

- 模型必须已在 `apimartImageModels` 中登记，未登记直接拒绝；
- `prompt` 非空；
- `size` 为 10 种比例之一；上游支持的 `auto` 刻意不开放，避免输出尺寸不确定；
- `resolution` 为 `1K`、`2K`、`4K`（按模型规格转成大写后发给上游）；
- 参考图去重后最多 16 张（上限随模型规格走，不是供应商统一常量）。

校验失败在扣费前返回 400。

### 4.1 参数规格（param_spec）

迁移后，本模型的参数约束由 `models.param_spec` 提供。字段定义与校验规则见 [参数规格文档](params/README.md)。本规格与 [gpt-image-2.5-flare-ext](gpt-image-2.5-flare-ext.md) 完全一致，只有适配层写入的 `version` 不同。

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

- `version=sunburst` 是上游风格字段，属于适配层行为，不写入 `param_spec`。
- `size` 只暴露比例，`widget` 为 `ratio_size`，不配置 `modes` 与 `pixel`。
- `resolution` 在规格中统一为大写，发往上游时保持大写。
- prompt 上限 2000 是平台策略，与客户端现有上限一致。
- 本节是迁移目标。P1 之前，校验仍由 `supplier/apimart.go` 的 `apimartImageModels` 执行。

## 5 请求体转换

`buildRequest` 写入 `model`、`prompt`、`size`、`resolution`、`n=1`，参考图非空时写入 `image_urls`。`resolution` 按模型规格转成大写（`1K` / `2K` / `4K`），平台模型名映射为 `version=sunburst`。

两个 APIMart 模型共用供应商模型 `gpt-image-2.5-ext`。`applyImageVersion` 根据调用方原始模型名查 `apimartImageModels`，取规格里的 `version` 补全请求体（本模型 → `version=sunburst`）。该表同时维护每个模型的 size 集合、分辨率档位与大小写、参考图上限。

## 6 提交方式与超时

与 flare 版本相同：使用 `common.OutboundHTTPClient`，提交超时 30 秒。请求刻意不传 `Idempotency-Key` 和 `X-APIMart-Response-Version`，上游成功时固定返回 HTTP 200 + `data[].task_id`。

| 场景 | 失败性质 | 处理 |
|---|---|---|
| 4xx、业务 `code` 不是 200、请求未发出 | `FailureRejected` | 退款 |
| 5xx、超时、连接中断、响应读取失败、非预期的 202 | `FailureUnknown` | 不退款，转 `unknown` |
| 缺少任务 ID | `FailureUnknown` | 不退款，转 `unknown` |

## 7 任务状态与对账

与 flare 版本相同。对账循环查询 `/v1/tasks/{task_id}?language=zh`，把上游 `completed` / `failed` / `cancelled` 映射为平台终态。查询失败返回 `call_fail`，任务保持非终态；未识别的状态同样保持非终态，下一轮继续查询。

## 8 结果转存

与 flare 版本相同。APIMart 不声明 `RequireResultFetch`，转存失败只记录日志，任务仍按 `completed` 处理，`data.url_vendor` 保留上游地址。

## 9 失败性质与退款

与 flare 版本相同。`failed`、`cancelled`、`call_fail` 可退款；`unknown` 不退款，超龄后转 `call_fail` 并退款。

## 10 配置

管理端配置如下。

| 配置项 | 值 |
|---|---|
| 供应商名称 | `apimart` |
| Base URL | APIMart API 地址 |
| 供应商模型 ID | `gpt-image-2.5-ext` |
| 平台模型名 | `gpt-image-2.5-sunburst-ext` |
| 模型类型 | `image` |
| 端点 | `/v1/images/generations` |
| 计费规则 | 基础积分 + 可选的参考图单价 |

## 11 代码与测试

与 flare 版本复用同一套实现与测试。

| 内容 | 位置 |
|---|---|
| 供应商实现 | `supplier/apimart.go` |
| 供应商单元测试 | `supplier/apimart_test.go` |
| 模型规格（size / resolution / 参考图上限 / version） | `supplier/apimart.go` 的 `apimartImageModels` |
| 控制器计费与参考图 | `controller/image.go` |
| 任务状态与对账 | `model/task.go`、`controller/image.go` |
