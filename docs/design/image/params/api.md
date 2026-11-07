# 对外接口

## 1 模型列表

`GET /v1/models` 增加顶层字段：

```json
{
  "object": "list",
  "param_spec_version": 1,
  "data": [
    {
      "id": "gpt-image-2",
      "object": "model",
      "owned_by": "futureai-api",
      "type": "image",
      "param_spec": {
        "params": [
          {
            "key": "prompt",
            "widget": "textarea",
            "required": true,
            "min_length": 1,
            "max_length": 2000
          }
        ]
      }
    }
  ]
}
```

规则：

- `param_spec_version` 是规格格式的全局版本，当前为 `1`。模型配置内部不再带版本字段。
- 图像模型始终包含 `param_spec` 键。未配置时为 `null`，客户端据此禁用该模型。
- 非图像模型不输出 `param_spec`。
- 新增字段均为增量字段，OpenAI 兼容客户端会忽略。
- 示例中的参数只保留必要字段，完整结构见 [schema.md](schema.md) 与 [components/](components/README.md)。

## 2 错误响应

现有 `code` / `message` 保持不变，增加 `error_code`、`param`，命名校验器失败时增加 `validator`。完整定义见 [errors.md](errors.md)。

## 3 管理端接口

- `GET /api/admin/models` 每项增加 `param_spec` 与 `param_spec_valid`；
- 新增 `PUT /api/admin/models/:id/param-spec`，请求体为 `{"param_spec": {...}}`；传 `null` 表示清除配置；
- 保存前执行 [validation.md](validation.md) 的元校验，失败返回 400 与字段级原因；
- 仅管理员可调用。

## 4 管理端页面

模型列表提供「参数配置」入口，包含：

- JSON 编辑与格式化；
- 模板导入；
- 控件预览；
- 保存前校验结果。

模板至少覆盖以下场景。具体配置示例见各模型设计文档。

| 模板 | 参考模型 |
|---|---|
| 比例 + 像素混合接口、前端只比例 | [gpt-image-2-ext.md](../gpt-image-2-ext.md) |
| 仅比例 | [gpt-image-2.5-flare-ext.md](../gpt-image-2.5-flare-ext.md) |
| 仅像素、带预设 | [moss-image-base-1.0.md](../moss-image-base-1.0.md) |
| 不支持参考图 | [moss-image-sft-1.0.md](../moss-image-sft-1.0.md) |
| 无 resolution | [moss-image-base-1.0.md](../moss-image-base-1.0.md) |
