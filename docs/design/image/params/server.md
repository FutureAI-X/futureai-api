# 服务端

## 1 模块划分

新增独立包 `imageparam`，只包含纯函数，不访问数据库与网络：

```text
ParseSpec(raw []byte) (*Spec, error)                 解析与元校验
(s *Spec) Evaluate(values map[string]any) (*Eval, error)  解析动态值、可见性与错误
(s *Spec) Validate(values map[string]any, meta FileMeta) error  参数校验
```

控制器在查到模型后调用。供应商适配层不再重复校验 prompt、size、resolution、参考图张数等公共约束。

## 2 请求字段注册表

`path` 只能取服务端已注册的字段。注册表至少包含：

```text
model, prompt, size, resolution, image_ids, image_urls,
output_format, quality, n, background, output_compression,
moderation, mask_url, markup_url, markup,
frame_count, frame_duration_ms,
svg.model, svg.reasoning.effort
```

当前代码已注册 `prompt`、`size`、`resolution`、`quality`、`output_format`、`complexity`、`image_ids`、`image_urls`；其余路径随对应控件接入时再登记。新增参数时必须同时完成三件事，缺一不可：

1. 在 `ImageGenerateRequest` 增加字段；
2. 在请求字段注册表登记路径；
3. 在供应商适配层实现翻译。

参数规格只降低客户端成本，不替代服务端的字段实现。

## 3 供应商适配层

迁移完成后，供应商适配层只保留：

- 供应商模型 ID、`version` 等传输字段；
- resolution 大小写等格式转换；
- 同步与后台提交、超时等实现能力；
- 新参数到上游字段的翻译。

以下内容从适配层移除：

- `SizeRule` 与像素约束；
- 每个模型的分辨率档位；
- 每个模型的参考图张数；
- prompt 长度上限；
- `ImageCapabilities.RequiresPixelSize` 与 `ImageCapabilities.SupportsReferenceImages`。参数能力以规格为准。

## 4 请求快照与幂等

请求快照仍保存调用方原始请求，包含新增参数。幂等键的语义不变：同一幂等键的重复提交返回原任务。

## 5 计费

计费联动不在参数引擎内实现，但参数规格必须为计费模块提供稳定的 `key` 与 `path`。详见 [dependencies.md](dependencies.md)。
