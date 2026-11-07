# 查询计费规则

`GET /api/pricing`

该接口对未登录用户开放，因此响应中不包含任何 `api_key` 字段。

响应结构：

```json
{
  "success": true,
  "data": {
    "models": [
      {
        "id": 3,
        "name": "gpt-image-2",
        "owner": "FutureAI",
        "description": "...",
        "tags": "...",
        "status": 1,
        "type": "image",
        "credit_rule": {
          "rule_type": "per_request",
          "base_credits": 1.0,
          "ref_image_credits": 2.0,
          "items": [
            {
              "credits": 0.1,
              "conditions": [
                { "param_path": "resolution", "param_value": "2K" }
              ]
            }
          ]
        }
      }
    ]
  }
}
```

`credit_rule` 在模型未配置或规则被禁用时不出现，调用方需要判空。

`type` 取值为 `image`、`video`、`text`、`music`、`other`，分别对应图像生成、视频生成、文本生成、音乐生成与其他。该字段不只是展示字段：参考图附加计费仅在类型为 `image` 且端点在图片白名单内时生效。

计费公式与积分精度见 [计费规则](README.md)。
