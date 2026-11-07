# APIMart 图像设计（gpt-image-2.5-sunburst-ext）

## 1 范围

本文说明平台模型 `gpt-image-2.5-sunburst-ext` 的内部实现。除 `version` 取值外，其余设计与 [gpt-image-2.5-flare-ext](gpt-image-2.5-flare-ext.md) 一致。通用图像链路见 [图像生成链路设计](README.md)，调用方文档见 [客户端文档](../../client/image/gpt-image-2.5-sunburst-ext.md)。

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

与 flare 版本相同。平台标准字段校验与参考图校验由控制器统一完成，APIMart 没有供应商特有的请求预校验。

## 5 请求体转换

`buildRequest` 写入 `model`、`prompt`、`size`、`resolution`，参考图非空时写入 `image_urls`。平台模型名映射为 `version=sunburst`。

两个 APIMart 模型共用供应商模型 `gpt-image-2.5-ext`。`applyImageVersion` 根据调用方原始模型名补全 `version`，映射表在 `supplier/apimart.go` 的 `apimartImageVersions` 中维护。

## 6 提交方式与超时

与 flare 版本相同：使用 `common.OutboundHTTPClient`，提交超时 30 秒。

| 场景 | 失败性质 | 处理 |
|---|---|---|
| 4xx、业务 `code != 200`、请求未发出 | `FailureRejected` | 退款 |
| 5xx、超时、连接中断、响应读取失败 | `FailureUnknown` | 不退款，转 `unknown` |
| 缺少 `task_id` | `FailureUnknown` | 不退款，转 `unknown` |

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
| 版本映射 | `supplier/apimart.go` 的 `apimartImageVersions` |
| 控制器计费与参考图 | `controller/image.go` |
| 任务状态与对账 | `model/task.go`、`controller/image.go` |
