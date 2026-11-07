# gpt-image-2.6-sunburst-ext

## 1 概览

APIMart 图像模型，风格版本为 sunburst，偏构图与编辑精度，适合成品素材、图中文字和精细编辑。

除模型名与风格版本外，本模型的请求字段、参数限制与计费规则与 [gpt-image-2.6-flare-ext](gpt-image-2.6-flare-ext.md) 完全一致。通用请求与响应格式见 [图像生成](README.md)。

| 项目 | 值 |
|---|---|
| 平台模型名 | `gpt-image-2.6-sunburst-ext` |
| 参考图 | 支持，最多 16 张 |
| `size` | 10 种画幅比例，默认 `16:9` |
| `resolution` | `1K`、`2K` 或 `4K`，默认 `1K` |
| `quality` | `auto` / `low` / `medium` / `high` / `xhigh` / `max`，默认 `auto` |
| `output_format` | `png` / `jpeg` / `webp`，默认 `png` |
| `complexity` | `low` / `medium` / `high`，默认不指定 |
| 输出数量 | 1 张 |
| 生成方式 | 异步提交，平台轮询上游任务 |

## 2 请求示例

```json
{
  "model": "gpt-image-2.6-sunburst-ext",
  "prompt": "Put the product from the first image into the scene in the second image, keep the logo and packaging text",
  "size": "16:9",
  "resolution": "2K",
  "quality": "high",
  "output_format": "png",
  "image_ids": ["file_1a2b3c4d5e6f", "file_2b3c4d5e6f7a"]
}
```

## 3 参数限制与错误

`size`、`resolution`、`quality`、`output_format`、`complexity`、参考图数量与错误码均与 [gpt-image-2.6-flare-ext](gpt-image-2.6-flare-ext.md#3-参数限制) 相同：

- `size` 为 10 种比例之一，不支持像素尺寸与 `auto`；
- `resolution` 为 `1K` / `2K` / `4K`；
- `output_format` 只支持 `png` / `jpeg` / `webp`，SVG / GIF 本期返回 400；
- `aspect_ratio` 会被忽略，请使用 `size`；
- 参考图去重后最多 16 张。

## 4 计费

与 flare 版本相同：单张价格只由 `version` × `resolution` 决定，`quality` / `output_format` / `complexity` 不影响单价。
