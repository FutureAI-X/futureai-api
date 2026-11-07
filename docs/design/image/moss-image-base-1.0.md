# MOSS 文生图设计（moss-image-base-1.0）

## 1 范围

本文说明平台模型 `moss-image-base-1.0` 的内部实现。通用图像链路见 [图像生成链路设计](README.md)，调用方文档见 [客户端文档](../../api/image/moss-image-base-1.0.md)。

## 2 供应商与端点

MOSS 是同步接口：一次 `POST` 直接返回图片 URL，没有 `task_id`，也没有任务查询接口。平台的公共接口仍按异步任务返回 `taskId`，由后台 goroutine 完成同步调用。

| 项目 | 值 |
|---|---|
| 供应商名称 | `moss` |
| Base URL | `https://api.mosi.cn` |
| 上游路径 | `/v1/images/generations` |
| 认证 | `Authorization: Bearer <API Key>` |
| 上游模型 ID | `moss-image-base-1.0`，或管理端配置的 snapshot |

## 3 能力声明

`moss.ImageCapabilities` 的取值如下。

| 字段 | 值 | 影响 |
|---|---|---|
| `BackgroundSubmit` | `true` | 控制器不阻塞 handler，任务以 `submitting` 落库 |
| `SupportsReferenceImages` | `false` | 忽略 `image_urls` / `image_ids`，不计参考图费用 |
| `RequiresPixelSize` | `true` | `size` 必须是像素尺寸 |
| `SubmitTimeout` | 15 分钟 | 与官方示例的 `--max-time 900` 对齐 |

## 4 入参校验

`moss.ValidateImageRequest` 在扣费与建任务之前执行，校验以下内容：

- `prompt` 非空，且不超过 4000 个字符；
- `size` 符合 `宽x高` 格式；
- 宽、高均在 256–2048 之间；
- 宽、高均为 16 的倍数；
- 总像素不超过 2097152。

`size` 的正则允许大小写 `x` 和两侧空白，写请求体时统一规范化为小写 `x`。校验失败返回 400，不扣费、不建任务。

参考图不做任何校验。控制器在能力分流阶段已经清空 `image_urls` 与 `image_ids`，供应商适配层不会再看到这两个字段。

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
      "max_length": 4000,
      "count_mode": "codepoint"
    },
    {
      "key": "size",
      "widget": "pixel_size",
      "label": "尺寸",
      "group": "basic",
      "required": true,
      "pixel": {
        "step": 16,
        "min_side": 256,
        "max_side": 2048,
        "min_pixels": 0,
        "max_pixels": 2097152
      },
      "presets": [
        { "label": "正方形", "ratio": "1:1", "pixels": "1440x1440" },
        { "label": "横图", "ratio": "16:9", "pixels": "1792x1008" },
        { "label": "竖图", "ratio": "9:16", "pixels": "1008x1792" },
        { "label": "4:3", "ratio": "4:3", "pixels": "1664x1248" },
        { "label": "3:4", "ratio": "3:4", "pixels": "1248x1664" },
        { "label": "3:2", "ratio": "3:2", "pixels": "1728x1152" },
        { "label": "2:3", "ratio": "2:3", "pixels": "1152x1728" },
        { "label": "5:4", "ratio": "5:4", "pixels": "1600x1280" },
        { "label": "4:5", "ratio": "4:5", "pixels": "1280x1600" },
        { "label": "21:9", "ratio": "21:9", "pixels": "2016x864" }
      ],
      "allow_custom": true
    }
  ]
}
```

说明：

- `size` 使用 `pixel_size` 组件：预设卡片 + 宽高输入，提交像素；`presets` 由管理端配置。
- 规格中没有 `resolution`。MOSS 不支持该参数，组件也不展示。
- 规格中没有 `ref_images`。服务端按“不支持参考图”静默忽略参考图字段。
- `required=true` 且没有 `default` 时，前端使用第一个预设作为初始值。
- 预设值均满足 `step=16`、单边 256–2048、总像素 ≤ 2,097,152；上线前需与 MOSS 文档复核。
- 当前实现已接入 `imageparam` 引擎；`supplier/moss.go` 的旧校验暂时保留作为双保险。

## 5 请求体转换

`buildMossRequest` 只写入以下固定字段：

| 上游字段 | 值 |
|---|---|
| `model` | 管理端配置的供应商模型 ID |
| `prompt` | 调用方提示词 |
| `size` | 规范化后的 `宽x高` |
| `negative_prompt` | `nsfw, worst quality, bad anatomy, watermark, jpeg artifact` |
| `prompt_enhance` | `true` |
| `n` | `1` |
| `response_format` | `url` |
| `output_watermark` | `{"enabled": false}` |

`resolution`、`image_urls`、`image_ids`、`seed`、`num_inference_steps`、`guidance_scale` 不写入请求体。参考图字段被忽略是产品决策，不是适配遗漏。

## 6 提交方式与超时

实现位于 `supplier/moss.go` 的 `ImageGenerate`。请求使用 `common.LongOutboundHTTPClient`，该客户端不设置 `ResponseHeaderTimeout`，整次调用的时限由 15 分钟的 context 控制。通用出站客户端的 30 秒响应头超时会在生成完成前切断连接，不能用于 MOSS。

控制器在处理 `BackgroundSubmit` 供应商时：

1. 以 `submitting` 状态创建任务并扣费；
2. 启动 `runBackgroundImageSubmit` goroutine；
3. 立即向调用方返回 `taskId`。

goroutine 成功后调用 `SetTaskVendorResponse` 写入结果，该函数同时把状态推进到 `submitted`，随后调用 `ScheduleTaskPoll`。失败时调用 `UpdateTaskStatusWithRefund` 进入 `call_fail` 并退款。goroutine 内的 panic 由 `recoverBackgroundSubmit` 捕获并退款。

## 7 任务状态与对账

MOSS 的 `TaskQuery` 不发起任何网络请求。后台 goroutine 已经把最终结果写入 `vendor_response`，`TaskQuery` 只负责把它还原成 `completed`。

`submitting` 状态不参与 stale-pending 退款，否则一次超过 90 秒的正常生成会被误退款。对账循环使用独立的 `backgroundSubmitGrace`（20 分钟）判断后台提交是否卡死，超过后调用 `FailStaleSubmittingTask` 转为 `call_fail` 并退款。

`FailStaleSubmittingTask` 在事务内加行锁，并重新检查 `status` 与 `vendor_response`。如果后台 goroutine 已经写入结果，任务状态已变为 `submitted`，这里不会退款。

进程在生成中途重启时，任务停在 `submitting`，结果无法恢复。下次启动后由上述宽限期规则退款，并记录 `[MANUAL_RECONCILE]` 日志。

## 8 结果转存

`TaskQuery` 返回 `RequireResultFetch=true`。对账循环在 `completed` 分支下载成图：

- 下载失败、HTTP 404、或内容不是图片时返回 `filestore.ErrSourceUnavailable`；
- 该错误会重试两次，仍失败则 `call_fail` 并退款；
- 对象存储上传或写库失败不属于结果资产问题，任务仍按 `completed` 处理，`file_id` 为空，`data.url_vendor` 保留上游地址。

成功转存后，`data.usage` 与 `data.created` 随 `query_response` 一起返回给调用方。

## 9 失败性质与退款

MOSS 适配层把所有失败统一标记为 `FailureRejected`。失败范围包括：请求构建失败、4xx、5xx、超时、连接中断、响应解析失败、响应缺少图片地址、图片地址不是 http(s)。

后台提交路径对任何非成功结果都直接退款，不进入 `unknown`。超时或断连时 MOSS 可能已经生成并计费，平台仍按产品策略退款，并记录 `[MANUAL_RECONCILE]` 日志供事后对账。

## 10 配置

管理端配置如下。

| 配置项 | 值 |
|---|---|
| 供应商名称 | `moss` |
| Base URL | `https://api.mosi.cn` |
| 供应商模型 ID | `moss-image-base-1.0` 或 `moss-image-base-1.0-2026-10-09` |
| 模型类型 | `image` |
| 端点 | `/v1/images/generations` |
| 计费规则 | 只配置基础积分，可为 0；参考图单价留 0 |

参考图单价即使配置也不会生效，`resolveCredits` 会跳过并记录配置不一致日志。

## 11 代码与测试

实现与测试位置如下。

| 内容 | 位置 |
|---|---|
| 供应商实现 | `supplier/moss.go` |
| 供应商单元测试 | `supplier/moss_test.go` |
| 控制器分流与后台提交 | `controller/image.go` |
| 任务状态与超龄退款 | `model/task.go` |
| 长响应客户端 | `common/outbound.go` |
| 结果资产错误分类 | `filestore/filestore.go` |

测试覆盖尺寸校验、固定请求体、成功响应、4xx/5xx/超时、结果缺失、`TaskQuery` 以及工厂注册。
