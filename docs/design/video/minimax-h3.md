# METASO 视频设计（MiniMax-H3）

## 1 范围

本文说明平台模型 `MiniMax-H3` 的内部实现。通用视频链路见 [视频生成链路设计](README.md)，调用方文档见 [客户端文档](../../client/video/minimax-h3.md)。

## 2 供应商与端点

METASO 使用异步任务接口。平台提交后拿到上游 `task_id`，再由对账循环轮询任务状态。

| 项目 | 值 |
|---|---|
| 供应商名称 | `metaso` |
| Base URL | `https://metaso.cn/api/minimax/v2` |
| 提交路径 | `/video_generation` |
| 查询路径 | `/query/video_generation/{task_id}` |
| 认证 | `Authorization: Bearer <API Key>` |
| 供应商模型 ID | `MiniMax-H3` |

## 3 能力声明

模型支持三种参考媒体，均可以为空。公开字段与内部字段的对应关系如下。

| 公开字段 | 内部字段 | 说明 |
|---|---|---|
| `model` | `Model` | 平台模型名，用于配置路由 |
| `prompt` | `Prompt` | 提示词 |
| `images` | `Images` | 解析后的图片签名 URL |
| `videos` | `Videos` | 解析后的视频签名 URL |
| `audios` | `Audios` | 解析后的音频签名 URL |
| `resolution` | `Resolution` | 上游分辨率档位 |
| `duration` | `Duration` | 输出时长 |
| `ratio` | `Ratio` | 画幅 |

内部请求还会填入 `VendorModelID`。该字段不进入公开请求，也不会写入快照。

## 4 入参校验

控制器先校验必填字段与 `file_id` 形态，再解析三个媒体数组。解析时校验归属、用途、状态、有效期、媒体类型和视频时长。详细规则见 [视频生成链路设计](README.md#4-参考文件解析)。

## 5 请求体转换

`buildVideoRequest` 把内部请求转换为 METASO 的 `content` 数组：

1. `text`：`prompt`；
2. `image_url`：`Images` 中的每个 URL，`role=reference_image`；
3. `video_url`：`Videos` 中的每个 URL，`role=reference_video`；
4. `audio_url`：`Audios` 中的每个 URL，`role=reference_audio`。

`resolution`、`duration`、`ratio` 直接写入请求体。`fast_mode` 固定为 `false`，不开放给调用方。

## 6 提交方式与超时

`VideoGenerate` 使用 `common.OutboundHTTPClient`，提交超时为包级 `SubmitTimeout`（30 秒）。

提交响应优先按 `{"task_id":"..."}` 解析。2xx 但缺少 `task_id` 时，再尝试从错误体读取 `http_code`：

- 错误体中的 `http_code` 为 4xx：`FailureRejected`，可退款；
- 其余情况：`FailureUnknown`，不退款，转 `unknown`。

## 7 任务状态与对账

`TaskQuery` 向 `/query/video_generation/{task_id}` 发起查询。`task_id` 来自上游响应，拼接 URL 前必须通过字符集校验。

上游状态映射如下。

| 上游状态 | 平台状态 |
|---|---|
| `succeeded` | `completed` |
| `failed` | `failed` |
| `cancelled` | `cancelled` |
| `queued` / `running` | `submitted` |
| 未知状态 | `call_fail` |

查询失败、HTTP 非 200、响应解析失败一律返回 `call_fail`。`call_fail` 不是任务失败，控制器不会据此退款。

## 8 结果转存

任务完成后，`QueryResponse` 中保存上游 `content.url`、`resolution`、`duration`、`ratio`、`usage` 等字段。对外只返回 `taskId`、`status` 与 `data.url`。

视频结果不转存到对象存储，也没有 `file_id` 与 `url_vendor`。`data.url` 是上游的限时下载地址，过期后需要重新生成任务。

## 9 失败性质与退款

视频任务按 0 积分创建，退款流程不会产生积分流水。状态机仍与图像任务一致：`failed`、`cancelled`、`call_fail` 是终态，`unknown` 不是终态。

## 10 配置

管理端配置如下。

| 配置项 | 值 |
|---|---|
| 供应商名称 | `metaso` |
| Base URL | `https://metaso.cn/api/minimax/v2` |
| 供应商模型 ID | `MiniMax-H3` |
| 模型类型 | `video` |
| 端点 | `/v1/videos/generations` |

## 11 代码与测试

实现与测试位置如下。

| 内容 | 位置 |
|---|---|
| 供应商实现 | `supplier/metaso.go` |
| 供应商单元测试 | `supplier/metaso_test.go` |
| 控制器与参考文件解析 | `controller/video.go` |
| 任务状态与对账 | `model/task.go`、`controller/image.go` |
