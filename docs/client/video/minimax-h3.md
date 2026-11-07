# MiniMax-H3

## 1 概览

METASO 视频生成模型。通用请求与响应格式见 [视频生成](README.md)。

模型能力如下。

| 项目 | 值 |
|---|---|
| 平台模型名 | `MiniMax-H3` |
| 参考输入 | 图片、视频、音频三种 file_id 数组，均可为空 |
| 输出 | 上游限时下载地址，平台不转存 |
| 生成方式 | 异步提交，平台轮询上游任务 |

## 2 请求字段

通用字段的含义见 [视频生成](README.md#2-请求字段)。本模型的字段约定如下。

| 字段 | 必填 | 说明 |
|---|---|---|
| `model` | 是 | `MiniMax-H3` |
| `prompt` | 是 | 提示词 |
| `images` | 否 | 参考图片 file_id 数组 |
| `videos` | 否 | 参考视频 file_id 数组 |
| `audios` | 否 | 参考音频 file_id 数组 |
| `resolution` | 是 | `480P`、`1K` 或 `2K` |
| `duration` | 是 | 输出时长（秒），正整数 |
| `ratio` | 是 | 画幅，如 `16:9`、`adaptive` |

## 3 参数限制

各参数的约定如下。

| 参数 | 限制 |
|---|---|
| `prompt` | 必填 |
| `duration` | 正整数 |
| `images` / `videos` / `audios` | 只接受平台 file_id，不接受外部 URL |
| 参考文件有效期 | 剩余不少于 30 分钟 |

参考文件的校验规则见 [视频生成](README.md#3-参考媒体)。

## 4 请求示例

```json
{
  "model": "MiniMax-H3",
  "prompt": "从首帧平滑过渡到尾帧，保持参考图中角色的外观与服装一致。",
  "images": ["file_..."],
  "videos": ["file_..."],
  "audios": ["file_..."],
  "resolution": "2K",
  "duration": 5,
  "ratio": "adaptive"
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

任务完成后只返回 `taskId`、`status` 与 `data.url`，格式见 [视频生成](README.md#5-响应)。

## 6 错误与重试

参数、身份、模型配置或参考文件校验错误会返回 4xx/503。上游明确拒绝受理时返回 HTTP 200 与 `code=fail`。提交结果不确定时，任务状态为 `unknown`，平台会继续确认结果，不会立即退款。

建议携带 `Idempotency-Key`，避免 SDK 超时重试导致重复提交。

## 7 计费

视频生成当前按 0 积分创建，不产生扣费与退款流水。
