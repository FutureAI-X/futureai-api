# APIMart 视频设计（MAGI-2）

## 1 范围

本文说明平台模型 `MAGI-2` 的内部实现。通用视频链路见 [视频生成链路设计](README.md)，调用方文档见 [客户端文档](../../api/video/magi-2.md)。

本模型只开放**参考生视频**：`image_ids` 中的图片一律是参考图，不做首帧/尾帧；视频与音频参考也按同一模式提交。没有参考素材时为文生视频。

## 2 供应商与端点

APIMart 使用异步任务接口。平台提交后拿到上游 `task_id`，再由对账循环轮询任务状态。图像与视频共用同一个 APIMart 供应商配置与查询端点。

| 项目 | 值 |
|---|---|
| 供应商名称 | `apimart` |
| Base URL | APIMart API 地址 |
| 提交路径 | `/v1/videos/generations` |
| 查询路径 | `/v1/tasks/{task_id}` |
| 认证 | `Authorization: Bearer <API Key>` |
| 供应商模型 ID | `magi-2` |
| 平台模型名 | 自行配置（示例 `MAGI-2`） |

供应商规格按**供应商模型 ID** 登记（`supplier/apimart.go` 的 `apimartVideoModels`，key 为 `magi-2`）。平台模型名是管理端自由配置的对外别名，只要把平台模型映射到供应商模型 `magi-2` 即可；未登记的供应商模型会 fail-closed 拒绝。

## 3 能力声明

本模型固定为参考模式，参数约束全部由 `models.param_spec` 表达。

| 公开字段 | 内部字段 | 说明 |
|---|---|---|
| `model` | `Model` | 平台模型名（管理端配置的对外别名） |
| `prompt` | `Prompt` | 提示词，最多 15,000 字符 |
| `image_ids` | `ImageURLs` | 解析后的参考图签名 URL，1–10 张 |
| `video_ids` | `VideoURLs` | 解析后的参考视频签名 URL，0–5 段 |
| `audio_ids` | `AudioURLs` | 解析后的参考音频签名 URL，0–5 段 |
| `resolution` | `Resolution` | 固定 `1080P` |
| `duration` | `Duration` | 输出时长，4–30 整数秒 |
| `ratio` | `Ratio` | 六种固定比例，不支持 `adaptive` |

## 4 入参校验

控制器按 `param_spec` 校验标量与参考媒体（见 [视频参数规格](params/README.md)）。供应商侧再做三项兜底，防止管理端规格配错后按错误档位计费：

- 供应商模型 ID 未在 `apimartVideoModels` 登记 → 拒绝；
- `resolution` 不等于 `1080P` → 拒绝；
- `ratio` 为空或为 `adaptive` → 拒绝（参考模式没有可跟随的首帧）。

这三项在任务创建后才会触发，控制器按 `FailureRejected` 失败并全额退款。正常配置下不会出现。

## 5 请求体转换

`buildAPIMartVideoRequest` 显式写入所有带默认值的字段，不依赖上游默认行为：

| 上游字段 | 值 |
|---|---|
| `model` | 供应商模型 ID `magi-2` |
| `prompt` | 调用方提示词 |
| `duration` | 调用方时长（4–30） |
| `resolution` | 固定 `1080p`（上游文档使用小写） |
| `aspect_ratio` | 调用方比例 |
| `generate_audio` | 固定 `true` |
| `nsfw_check` | 固定 `false` |
| `seed` | 固定 `-1`（随机） |
| `omni_reference_task_type` | 有任意参考素材时为 `reference`，否则为 `auto` |
| `image_urls` / `video_urls` / `audio_urls` | 非空时写入 |

参考素材为空时不写对应数组：这表示「没有该输入」，不是一个有默认值的参数。

以下字段不发送：

- `seconds` / `audio`：与 `duration` / `generate_audio` 互为别名，只传主字段；
- `negative_prompt`、`multi_shot`、`keyframe` 等文档明确不支持的字段；
- 请求头不传 `Idempotency-Key` 与 `X-APIMart-Response-Version`，保持 `HTTP 200 + data[].task_id` 的返回格式。

## 6 提交方式与超时

`VideoGenerate` 使用 `common.OutboundHTTPClient`，提交超时为包级 `SubmitTimeout`（30 秒）。失败性质与图像提交一致：

| 场景 | 失败性质 | 处理 |
|---|---|---|
| 4xx、业务 `code` 不是 200、参数兜底拒绝、请求未发出 | `FailureRejected` | 转 `failed` 并退款 |
| 5xx、意外 202、超时、连接中断、响应读取/解析失败 | `FailureUnknown` | 保持 `processing`，超过 60 秒提交确认窗口后转 `failed` 并退款 |
| 缺少任务 ID | `FailureUnknown` | 同上 |

成功时写入的供应商响应为：

```json
{ "taskId": "task_xxx", "kind": "video" }
```

`kind` 是对账循环区分图像/视频结果的唯一依据。

## 7 任务状态与对账

`TaskQuery` 从 `vendor_response` 读取 `kind`，视频任务解析 `data.result.videos[0].url`。`url` 兼容字符串与字符串数组两种上游形态。

| 上游状态 | 平台状态 |
|---|---|
| `pending` / `processing` | 保持 `processing` |
| `completed` | `completed`，`Kind=video` |
| `failed` / `cancelled` | `failed` / `cancelled` |
| 查询失败、HTTP 非 200、未识别状态 | `call_fail`，保持 `processing` |

失败任务读取 `data.error.reason`，沿用 `supplier/apimart.go` 中的 `apimartFailureReasons`（与图像侧同一张表）：`content_policy_violation`、`invalid_parameter`、`unsupported_option`、`invalid_media`、`media_unreachable`、`generation_timeout`、`service_unavailable` 等。上游原文只写入 `query_response`，不透传给调用方。

任务超过 180 分钟仍未确认完成时，统一转 `failed` 并写入 `error_code=query_timeout`。轮询节奏为 0–3 分钟每 10 秒、3–10 分钟每 20 秒、10 分钟以后每 30 秒（±10% 抖动），由服务端按 `next_poll_at` 调度，并通过任务查询接口下发 `poll_after_ms`。

## 8 结果转存

视频结果不转存到对象存储。`Kind=video` 让控制器跳过图片转存逻辑，任务完成时直接把上游成片地址作为 `data.url` 返回。没有 `file_id` 与 `url_vendor`。

## 9 计费

上游按输出秒数收费。平台侧建议配置：

- `base_credits_per_request = 0`；
- `image_credits_per_item`、`audio_credits_per_item`、`input_video_credits_per_second` 均为 `0`；
- 唯一档位 `1080P`：`output_credits_per_second_no_ref` 与 `output_credits_per_second_with_ref` 填同一个每秒积分单价。

因此总积分 = `duration × 每秒积分`，与上游口径一致。失败或取消时全额退回。

## 10 配置

管理端配置如下。

| 配置项 | 值 |
|---|---|
| 供应商名称 | `apimart`（与图像共用） |
| 供应商模型 ID | `magi-2` |
| 平台模型名 | 自行配置（示例 `MAGI-2`） |
| 模型类型 | `video` |
| 端点 | `/v1/videos/generations` |
| 计费规则 | 仅 `1080P` 档位，按输出秒数计价 |

`param_spec` 通过管理端参数规格接口粘贴（视频侧没有种子机制）：

```json
{
  "version": 1,
  "params": [
    {
      "key": "prompt",
      "widget": "textarea",
      "label": "提示词",
      "group": "basic",
      "required": true,
      "max_length": 15000,
      "count_mode": "codepoint"
    },
    {
      "key": "resolution",
      "widget": "segmented",
      "label": "分辨率",
      "group": "basic",
      "required": true,
      "options": ["1080P"]
    },
    {
      "key": "duration",
      "widget": "number",
      "label": "时长（秒）",
      "group": "basic",
      "required": true,
      "min": 4,
      "max": 30,
      "step": 1,
      "integer": true
    },
    {
      "key": "ratio",
      "widget": "segmented",
      "label": "画面比例",
      "group": "basic",
      "required": true,
      "options": ["16:9", "9:16", "1:1", "4:3", "3:4", "21:9"]
    },
    {
      "key": "images",
      "path": "image_ids",
      "widget": "ref_images",
      "label": "参考图片",
      "hint": "所有图片均作为参考图，不会被当作首帧或尾帧",
      "group": "reference",
      "max_count": 10,
      "allowed_mime_types": ["image/jpeg", "image/png", "image/webp"]
    },
    {
      "key": "videos",
      "path": "video_ids",
      "widget": "ref_videos",
      "label": "参考视频",
      "group": "reference",
      "max_count": 5,
      "allowed_mime_types": ["video/mp4", "video/quicktime"],
      "max_file_bytes": 52428800,
      "duration_seconds": { "min": 2, "max": 30 },
      "max_total_duration_seconds": 30
    },
    {
      "key": "audios",
      "path": "audio_ids",
      "widget": "ref_audios",
      "label": "参考音频",
      "group": "reference",
      "max_count": 5,
      "allowed_mime_types": ["audio/wav", "audio/mpeg"],
      "max_file_bytes": 15728640,
      "duration_seconds": { "min": 2, "max": 30 },
      "max_total_duration_seconds": 30
    }
  ]
}
```

分辨率集合必须与计费价目表一致（管理端保存时交叉校验），否则请求会被 fail-closed 拒绝。

## 11 代码与测试

| 内容 | 位置 |
|---|---|
| 供应商实现 | `supplier/apimart.go`（`apimartVideoModels`、`buildAPIMartVideoRequest`、`VideoGenerate`、`TaskQuery`） |
| 供应商单元测试 | `supplier/apimart_test.go` |
| 控制器与参考文件解析 | `controller/video.go` |
| 计费计算 | `controller/video_billing.go` |
| 参数规格解析与校验 | `videoparam/` |
| 任务状态与对账 | `model/task.go`、`controller/image.go` |
