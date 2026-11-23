# 视频计费设计

本文面向维护者，说明视频生成任务如何计费、扣费、退款与审计。客户端价格约定见 [视频生成](../../api/video/README.md#7-计费)。

## 1 范围

视频计费与图片计费完全分开：

- 图片使用 `credit_rules`（`per_request` / `per_token`）；
- 视频使用 `video_credit_rules` + `video_credit_rates`。

两者互斥，一个模型只会按自身 `type` 命中其中一张表。分开的原因是视频计费是「多档费率 × 动态数量/秒数」的乘积求和，无法用图片的「固定积分 + 参数组合 + 参考图单价」表达。

## 2 数据模型

### 2.1 `video_credit_rules`

每个视频模型一条。

| 字段 | 说明 |
|---|---|
| `model_id` | 关联模型，唯一 |
| `status` | 1 启用 / 2 禁用 |
| `base_credits_per_request` | 每次请求固定收取的积分，0=不收取（如 H3 Context IR） |
| `base_credits_description` | 基础扣费说明，展示在模型广场，最长 255 字符 |
| `image_credits_per_item` | 每张参考图片积分，0=免费 |
| `image_free_items` | 每请求免费的前 N 张，0=不免费 |
| `audio_credits_per_item` | 每个参考音频积分，0=免费 |
| `description` | 规则描述 |

### 2.2 `video_credit_rates`

每规则 × 每输出分辨率一行，`(rule_id, resolution)` 唯一。

| 字段 | 说明 |
|---|---|
| `resolution` | 输出分辨率档位，统一大写 |
| `input_video_credits_per_second` | 每参考视频秒的积分 |
| `output_credits_per_second_no_ref` | 不含参考视频时每秒输出的积分 |
| `output_credits_per_second_with_ref` | 含参考视频时每秒输出的积分 |

分辨率采用行存储而不是固定列：新增档位只加一行，不改 schema。

## 3 计费公式

```text
resolution = upper(trim(req.resolution))

若模型没有启用规则            → 503，拒绝请求
若规则里没有该分辨率档位      → 400，拒绝请求（消息中列出支持档位）

baseCharge        = base_credits_per_request（每次请求固定收取，0=不收取）
billableImages    = max(0, 去重后的图片数 - image_free_items)
imageCharge       = billableImages × image_credits_per_item
audioCharge       = 去重后的音频数 × audio_credits_per_item
inputVideoSeconds = Σ ceil(每个去重视频文件的 DurationMS / 1000)
inputVideoCharge  = inputVideoSeconds × input_video_credits_per_second
hasRefVideo       = 去重后的视频数 > 0
outputRate        = hasRefVideo ? output_with_ref : output_no_ref
outputCharge      = 请求的 duration × outputRate
total             = RoundCredits(baseCharge + imageCharge + audioCharge + inputVideoCharge + outputCharge)
```

基础扣费与分辨率档位无关，任何一次视频请求都会收取；用于承接上游按次收取的固定费用（例如 H3 Context IR）。`base_credits_description` 只用于展示与审计，不影响计算。

### 3.1 取整口径

- 参考视频**逐文件**向上取整到整秒后再求和。不同上游的结算口径不一致，逐文件向上取整最保守，保证平台不因取整差额亏损。
- 输出时长直接使用请求的 `duration`（正整数），不按上游实际返回时长二次结算。
- 费率最多 3 位小数、数量均为整数，因此每项金额在积分业务精度内是精确的；仍逐项 `RoundCredits` 以消除浮点噪声，并保证快照分项之和等于总额。

### 3.2 0 费率与未配置

- 费率填 `0` 是显式免费，合法。
- 规则缺失、规则禁用、分辨率档位缺失都是配置/请求错误，不能静默按免费处理。
- 管理端保存时要求至少一个档位；重复分辨率（大小写归一后）会被拒绝。

## 4 任务与账本

1. 计算完成后用 `CreateTaskAndDeduct` 原子创建任务并扣除 `total`；失败整体回滚。
2. `tasks.credits` 与 `tasks.preauth_credits` 均为本次预扣，`credits_settled=true`、`settled_credits=total`：数量在提交时已知，预扣即终值，没有二次结算。
3. 上游明确拒绝、提交确认超时、任务失败/取消、查询超时，统一走现有 `FailTask` 全额退款，退款金额读取 `tasks.credits`。
4. 幂等键命中时直接返回原任务，不重复扣费；幂等检查在计费与参考文件解析之前。

## 5 计费快照

快照写入 `tasks.video_billing`（独立于 per_token 的 `billing_snapshot`，后者会被对账循环按 token 形状解析）。快照包含：

- 命中的 `rule_id` / `rate_row_id` 与分辨率；
- 是否含参考视频；
- 基础扣费的金额与说明；
- **提交时的费率拷贝**；
- 各项数量与秒数；
- 各项金额与总额。

费率拷贝保证管理员之后改价或删除档位，不影响已建任务的审计口径。

## 6 管理端与公开接口

| 接口 | 说明 |
|---|---|
| `GET /api/admin/models/:id/video-credit-rule` | 读取规则（不限状态，供编辑回显） |
| `PUT /api/admin/models/:id/video-credit-rule` | 全量保存规则与档位；仅允许 `type=video` 的模型 |
| `/api/pricing` | 视频模型附带 `video_credit_rule`，客户端可据此预估价格 |

保存时校验：模型存在且为视频类型、基础扣费与各费率非负且不超过业务精度、基础扣费说明不超过 255 字符、免费张数非负、至少一个非空且不重复的分辨率档位。

## 7 已知限制

视频目前还没有参数规格（`param_spec`）。计费按**请求中实际出现的**参考素材计价；如果某个模型实际不支持某种媒体、上游静默忽略，平台仍会按请求收费。参数校验落地后，这一口径会自然收紧。

## 8 代码与测试

| 内容 | 位置 |
|---|---|
| 规则与档位模型、校验、读写 | `model/video_credit_rule.go` |
| 计费计算与快照 | `controller/video_billing.go` |
| 请求入口与扣费 | `controller/video.go` |
| 管理端接口 | `controller/admin_video_credit_rule.go` |
| 公开定价 | `model/model.go` `GetPricingModels` |
| 单元测试 | `controller/video_billing_test.go`、`model/video_credit_rule_test.go` |
