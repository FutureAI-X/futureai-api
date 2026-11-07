# MOSS 文生图设计（moss-image-sft-1.0）

## 1 范围

本文说明平台模型 `moss-image-sft-1.0` 的内部实现。除上游模型 ID 与默认 snapshot 外，其余设计与 [moss-image-base-1.0](moss-image-base-1.0.md) 完全一致。通用图像链路见 [图像生成链路设计](README.md)，调用方文档见 [客户端文档](../../api/image/moss-image-sft-1.0.md)。

## 2 供应商与端点

MOSS 是同步接口，一次 `POST` 返回图片 URL，没有任务查询接口。

| 项目 | 值 |
|---|---|
| 供应商名称 | `moss` |
| Base URL | `https://api.mosi.cn` |
| 上游路径 | `/v1/images/generations` |
| 认证 | `Authorization: Bearer <API Key>` |
| 上游模型 ID | `moss-image-sft-1.0`，或管理端配置的 snapshot |

## 3 能力声明

能力与 base 版本相同。

| 字段 | 值 | 影响 |
|---|---|---|
| `BackgroundSubmit` | `true` | 任务以 `submitting` 落库，handler 立即返回 |
| `SupportsReferenceImages` | `false` | 忽略参考图，不计费 |
| `RequiresPixelSize` | `true` | `size` 必须是像素尺寸 |
| `SubmitTimeout` | 15 分钟 | 同步生成的最长等待时间 |

## 4 入参校验

校验逻辑与 base 版本共用 `moss.ValidateImageRequest`：

- `prompt` 非空且不超过 4000 个字符；
- `size` 为 `宽x高`，宽高 256–2048、16 的倍数、总像素不超过 2097152。

参考图不校验，控制器在能力分流阶段已经清空相关字段。

### 4.1 参数规格（param_spec）

本模型的参数约束由 `models.param_spec` 提供。字段定义与校验规则见 [参数规格文档](params/README.md)。本规格与 [moss-image-base-1.0](moss-image-base-1.0.md) 完全一致，只有上游模型 ID 不同。

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

- 与 base 版本使用同一份规格，仅上游模型 ID 不同。
- 规格中没有 `resolution` 与 `ref_images`：MOSS 不支持 resolution，也不接受参考图。
- `required=true` 且没有 `default` 时，前端使用第一个预设作为初始值。
- 预设值均满足 `step=16`、单边 256–2048、总像素 ≤ 2,097,152；上线前需与 MOSS 文档复核。
- 当前实现已接入 `imageparam` 引擎；`supplier/moss.go` 的旧校验暂时保留作为双保险。

## 5 请求体转换

请求体与 base 版本相同，仅 `model` 不同：

| 上游字段 | 值 |
|---|---|
| `model` | `moss-image-sft-1.0` |
| `prompt` | 调用方提示词 |
| `size` | 规范化后的 `宽x高` |
| `negative_prompt` | `nsfw, worst quality, bad anatomy, watermark, jpeg artifact` |
| `prompt_enhance` | `true` |
| `n` | `1` |
| `response_format` | `url` |
| `output_watermark` | `{"enabled": false}` |

## 6 提交方式与超时

与 base 版本相同：控制器使用 `BackgroundSubmit` 分流，goroutine 使用 `common.LongOutboundHTTPClient` 和 15 分钟 context 调用上游。成功写入 `vendor_response` 并转入 `submitted`，失败进入 `call_fail` 并退款。

## 7 任务状态与对账

与 base 版本相同：

- `TaskQuery` 不访问上游，直接读取已落库的结果；
- `submitting` 超过 20 分钟无结果时由 `FailStaleSubmittingTask` 退款；
- 进程重启中断同样按超龄规则退款。

## 8 结果转存

与 base 版本相同：`RequireResultFetch=true`，成图下载失败、404 或内容不是图片时重试两次后退款。对象存储上传失败不影响任务完成。

## 9 失败性质与退款

与 base 版本相同：所有失败统一为 `FailureRejected`，后台提交路径直接退款，并记录 `[MANUAL_RECONCILE]` 日志。

## 10 配置

管理端配置如下。

| 配置项 | 值 |
|---|---|
| 供应商名称 | `moss` |
| Base URL | `https://api.mosi.cn` |
| 供应商模型 ID | `moss-image-sft-1.0` 或 `moss-image-sft-1.0-2026-10-09` |
| 模型类型 | `image` |
| 端点 | `/v1/images/generations` |
| 计费规则 | 只配置基础积分，可为 0 |

## 11 代码与测试

实现与测试复用 base 版本的代码：

| 内容 | 位置 |
|---|---|
| 供应商实现 | `supplier/moss.go` |
| 供应商单元测试 | `supplier/moss_test.go` |
| 控制器分流与后台提交 | `controller/image.go` |
| 任务状态与超龄退款 | `model/task.go` |

测试覆盖与 base 版本相同，模型 ID 通过管理端配置区分，不需要在代码中硬编码。
