# file

## 1 用途

单文件上传，适合蒙版、标注等参数。与 `ref_images` 的区别是只接受一个文件，且通常参与命名校验器。

## 2 JSON 设计

```json
{
  "key": "mask",
  "path": "mask_url",
  "widget": "file",
  "label": "蒙版",
  "group": "reference",
  "max_file_bytes": 10485760,
  "allowed_mime_types": ["image/png"],
  "visible_when": { "field": "images", "op": "not_empty" },
  "conflicts_with": ["markup"],
  "validators": ["mask_matches_first_image"]
}
```

## 3 字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `path` | string | 是 | 请求字段，例如 `mask_url`、`markup_url` |
| `max_file_bytes` | integer | 是 | 单文件上限，单位字节 |
| `allowed_mime_types` | string[] | 是 | 允许的 MIME |
| `visible_when` | condition | 否 | 通常依赖参考图非空 |
| `conflicts_with` | string[] | 否 | 互斥字段，例如蒙版与标注 |
| `validators` | string[] | 否 | 命名校验器 |
| 其余通用字段 | — | — | 见 [../schema.md](../schema.md) |

## 4 值校验

- 文件必须已上传并拿到可用的 URL 或 file_id；
- 单文件大小不超过 `max_file_bytes`；
- MIME 在 `allowed_mime_types` 内；
- 命名校验器可以进一步检查尺寸、Alpha 通道等；
- `conflicts_with` 中的字段同时有值时返回 `PARAM_CONFLICT`。

## 5 前端组件

单文件上传：

- 选择后展示缩略图、文件名与大小；
- 上传完成后填入 `path` 对应的请求字段；
- `visible_when` 不满足时隐藏；
- `conflicts_with` 的另一个字段有值时禁用当前控件，并提示互斥；
- 校验器失败时展示服务端返回的错误。

## 6 服务端行为

按 `path` 读取文件标识，解析为文件元数据后校验大小与 MIME。命名校验器可以使用文件元数据与完整参数集合，例如 `mask_matches_first_image` 需要首张参考图的尺寸。

## 7 组合与注意事项

- 蒙版通常要求 PNG 与首图同尺寸，标注层同理；
- 蒙版与标注互斥，使用 `conflicts_with`；
- 运行时数据依赖首期由服务端阻断，客户端只提示；
- 单文件上限同样不得超过上传接口的 30MB。
