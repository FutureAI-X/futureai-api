# gpt-image-2.5-sunburst-ext

## 1 概览

APIMart 图像模型，风格版本为 sunburst。通用请求与响应格式见 [图像生成](README.md)。

模型能力如下。

| 项目 | 值 |
|---|---|
| 平台模型名 | `gpt-image-2.5-sunburst-ext` |
| 参考图 | 支持 |
| `size` | 画幅比例，配合 `resolution` |
| `resolution` | 支持 |
| 输出数量 | 1 张 |
| 生成方式 | 异步提交，平台轮询上游任务 |

## 2 请求字段

通用字段的含义见 [图像生成](README.md#2-请求字段)。本模型的字段约定如下。

| 字段 | 必填 | 说明 |
|---|---|---|
| `model` | 是 | `gpt-image-2.5-sunburst-ext` |
| `prompt` | 是 | 提示词 |
| `size` | 否 | 画幅比例，如 `16:9`、`1:1`，默认 `16:9` |
| `resolution` | 否 | `1k` 或 `2k`，默认 `1k` |
| `image_urls` | 否 | 参考图 URL 数组 |
| `image_ids` | 否 | 参考图 file_id 数组，优先级高于 `image_urls` |

## 3 参数限制

各参数的约定如下。

| 参数 | 限制 |
|---|---|
| `size` | 画幅比例，不使用像素尺寸 |
| `resolution` | `1k` 或 `2k` |
| `image_ids` | 去重后最多 15 张 |
| `image_urls` | 旧版路径，去重后单次最多 100 张计入计费 |

参考图的校验规则见 [图像生成](README.md#4-参考图)。

## 4 请求示例

```json
{
  "model": "gpt-image-2.5-sunburst-ext",
  "prompt": "暖阳下的海边小屋，胶片质感",
  "size": "16:9",
  "resolution": "2k"
}
```

## 5 响应

提交成功时返回：

```json
{
  "code": "success",
  "taskId": "task_e629c5e8a37a614cf2289f650585aae4"
}
```

任务完成后返回 `file_id` 与 `data.url`，格式见 [图像生成](README.md#6-响应)。

## 6 错误与重试

上游明确拒绝受理时返回 HTTP 200 与 `code=fail`。提交结果不确定时，任务状态为 `unknown`，平台会继续确认结果，不会立即退款。

建议携带 `Idempotency-Key`，避免 SDK 超时重试导致重复扣费。

## 7 计费

计费公式见 [图像生成](README.md#8-计费)。本模型按基础积分计费；如果配置了参考图单价，按实际生效字段去重后的张数叠加。
