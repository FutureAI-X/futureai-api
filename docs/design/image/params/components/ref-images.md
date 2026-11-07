# ref_images

## 1 用途

参考图上传。前端展示参考图列表与上传入口，提交 `image_ids`；服务端负责张数、大小、类型校验与静默忽略。

## 2 JSON 设计

```json
{
  "key": "images",
  "path": "image_ids",
  "widget": "ref_images",
  "label": "参考图",
  "group": "reference",
  "max_count": 16,
  "max_file_bytes": 10485760,
  "max_total_bytes": 268435456,
  "allowed_mime_types": ["image/png", "image/jpeg", "image/webp"]
}
```

`max_count` 可以动态变化：

```json
{
  "max_count": {
    "by": "output_format",
    "map": { "svg": 0 },
    "default": 16
  }
}
```

## 3 字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `path` | string | 是 | 通常为 `image_ids`；服务端也接受 `image_urls` |
| `max_count` | integer | 是 | 张数上限；`0` 表示不支持 |
| `max_file_bytes` | integer | 条件 | 单张上限，单位字节；`max_count > 0` 时必填 |
| `max_total_bytes` | integer | 条件 | 本次请求合计上限，单位字节 |
| `allowed_mime_types` | string[] | 条件 | 允许的 MIME |
| 其余通用字段 | — | — | 见 [../schema.md](../schema.md) |

## 4 值校验

- 参考图的有效数组：`image_ids` 非空时只认它，否则使用 `image_urls`；`image_ids` 按 file_id 去重后计数；
- 张数上限对两种数组都生效；
- 单张大小、合计大小与 MIME 只对 `image_ids` 生效。外部 URL 无法在不下载的前提下确认大小与真实类型，只校验张数；
- 单张与合计大小取文件记录中的实际字节数；
- `max_count` 为 0 或规格中没有 `ref_images` 时，服务端静默忽略参考图字段。

## 5 前端组件

参考图上传列表：

- 支持选择与拖拽；
- 按 `max_count`、`max_file_bytes`、`max_total_bytes`、`allowed_mime_types` 提前拦截；
- 展示单张大小与合计大小；
- `max_count` 解析为 0 时隐藏整个入口；
- 模型切换后已选图片保留在本地；如果新模型不支持参考图，提交时不携带，并提示「当前模型不支持参考图，参考图不会生效」。

## 6 服务端行为

`ref_images` 不存在或 `max_count` 解析为 0 时：

- 在解析 file_id 与参数校验之前清空 `image_urls` 与 `image_ids`；
- 不校验张数、大小与 MIME；
- 不计参考图附加费用；
- 不向上游发送参考图字段；
- 只记一条服务端日志；
- 任务快照仍然保留调用方原始请求，便于回溯。

`max_count > 0` 时按第 4 节校验。供应商能力声明与规格冲突时，以规格为准；若规格声明支持参考图而适配层声明不支持，按配置不一致处理，返回 503 `PARAM_SPEC_INVALID`，不得静默丢弃。

## 7 组合与注意事项

- 上传接口的全局单文件上限仍是 30MB，模型规格的单张上限必须不大于它；
- 计费侧的参考图张数封顶值是 100，元校验要求 `max_count` 不超过它；
- 蒙版、标注等单文件参数使用 [file.md](file.md)，不要塞进参考图数组；
- 错误码见 [../errors.md](../errors.md)。
