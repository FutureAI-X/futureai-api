# 模型管理

路径前缀：`/api/admin/models`

完整的方法、参数与响应以 `router/router.go` 中的路由注册为准。

## 1 模型列表

`GET /api/admin/models`

每项在原有字段之外增加：

| 字段 | 类型 | 说明 |
|---|---|---|
| `param_spec` | object 或 null | 图像模型的参数规格；未配置为 null |
| `param_spec_valid` | boolean | 已配置且通过元校验时为 true |

`param_spec` 的结构见 [参数规格文档](../../design/image/params/README.md)。

## 2 更新参数规格

`PUT /api/admin/models/:id/param-spec`

请求体：

```json
{
  "param_spec": {
    "params": [
      {
        "key": "prompt",
        "widget": "textarea",
        "required": true,
        "max_length": 2000
      }
    ]
  }
}
```

- `param_spec` 为 `null` 时清除配置；
- 仅图像模型可调用；
- 保存前执行元校验，结构或取值不合法时返回 400，`message` 为具体原因；
- 当前支持 `textarea`、`segmented`、`ratio_size`、`pixel_size`、`ref_images`。
