# 模型管理

路径前缀：`/api/admin/models`；排序接口：`/api/admin/model-order`

完整的方法、参数与响应以 `router/router.go` 中的路由注册为准。

## 1 模型列表

`GET /api/admin/models`

每项在原有字段之外增加：

| 字段 | 类型 | 说明 |
|---|---|---|
| `param_spec` | object 或 null | 图像或视频模型的参数规格；未配置为 null |
| `param_spec_valid` | boolean | 已配置且通过对应类型的元校验时为 true |
| `sort_order` | integer 或 null | 手动排序值；null 表示未手动排序 |

`param_spec` 的结构：图像模型见 [图像参数规格](../../design/image/params/README.md)，视频模型见 [视频参数规格](../../design/video/params/README.md)。

列表按 `sort_order`、开发者、模型名、ID 排序；未手动排序的模型排在已排序模型之后。新建模型会追加到当前顺序的末尾（创建时先固化现有模型的当前顺序）。

## 2 保存模型顺序

`PUT /api/admin/model-order`

请求体：

```json
{ "ids": [3, 1, 2, 5] }
```

- `ids` 为当前列表从上到下的完整顺序；
- 服务端先清空所有 `sort_order`，再按 `ids` 顺序写入 `1..N`；
- 不在 `ids` 里的模型会变成未排序状态，排在末尾；
- ID 为空、重复、非正数时返回 400；
- 请求里的模型已被删除时返回 409，前端应刷新列表后重试。

## 3 恢复默认排序

`DELETE /api/admin/model-order`

清空所有模型的 `sort_order`，回落到「开发者 → 模型名 → ID」的默认顺序。

## 4 更新参数规格

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
- 仅图像或视频模型可调用；
- 保存前执行元校验，结构或取值不合法时返回 400，`message` 为具体原因；
- 图像模型支持 `textarea`、`segmented`、`ratio_size`、`pixel_size`、`ref_images`；
- 视频模型支持 `textarea`、`segmented`、`number`、`ref_images`、`ref_audios`、`ref_videos`。

视频模型的分辨率同时出现在参数规格与计费价目表中。保存任一边时，服务端会要求两边的分辨率集合一致；不一致返回 400 并列出缺少的档位。
