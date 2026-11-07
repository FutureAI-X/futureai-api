# 查询模型列表

`GET /v1/models`

返回当前可用的模型列表。请求需要 `Authorization: Bearer sk-...`。

## 1 响应

```json
{
  "object": "list",
  "data": [
    {
      "id": "deepseek-v4-flash",
      "object": "model",
      "owned_by": "futureai-api",
      "type": "text"
    }
  ]
}
```

`type` 是 OpenAI 规范外的附加字段，供调用方区分模型能力。取值如下。

| 取值 | 含义 |
|---|---|
| `image` | 图像生成 |
| `video` | 视频生成 |
| `text` | 文本生成 |
| `music` | 音乐生成 |
| `other` | 其他 |

标准 OpenAI 客户端会忽略 `type` 字段。选择图像或视频模型后，按对应模型文档传参：

- 图像模型见 [图像生成](../image/README.md)；
- 视频模型见 [视频生成](../video/README.md)。
