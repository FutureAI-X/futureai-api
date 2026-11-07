# 依赖与 2.6 接入

参数规格只描述参数本身。以下能力必须与它同期或后续设计，不能假设「改完 schema 就接完模型」。

## 1 计费联动

参数规格不携带价格。当前计费规则只支持 `model`、`prompt`、`size`、`resolution` 四个条件字段，2.6 的 `quality`、`n`、`frame_count` 无法表达。必须与 2.6 同期完成以下改造：

1. 计费条件支持任意已注册的参数路径，不再限制为四个字段；
2. 数值参数支持 `multiplier`，`n` 与 `frame_count` 按倍数计费；
3. `quality=auto` 的预扣金额按最高档估算，任务结束后按实际档位结算或退款。

这部分属于计费模块，不在参数引擎内实现，但参数规格必须为其提供稳定的 `key` 与 `path`。

## 2 输出格式、结果与页面（后续）

本期规格不包含 `output_formats` 与 `result`：

- `output_format` 作为普通参数存在，合法取值由该参数的 `options` 表达；
- 客户端页面过滤、结果类型标注与多结果渲染在 2.6 正式版接入时另行设计；
- 本期不因为缺少顶层元数据而阻塞 2.6 Ext：Ext 的 `n` 固定为 1，结果仍是单资产。

后续设计页面时，仍需保证：

- 页面可以固定 `output_format`，并隐藏对应控件；
- 同一个模型可以出现在多个页面；
- 结果渲染由 `output_format` 决定，不在页面里写 `if (format === 'gif')`。

服务端届时还需完成：

1. 任务结果从单资产扩展为资产数组；
2. API 响应在 `n > 1` 时返回多个结果；
3. 对 GIF、SVG 的结果类型做正确标注；
4. 供应商适配层按 `output_format` 选择上游参数。

## 3 GPT-Image-2.6 接入清单

| 2.6 能力 | 规格表达 | 服务端仍需完成 |
|---|---|---|
| `output_format` | `segmented` 或 `select`，合法值由 `options` 表达 | 请求字段、上游映射 |
| `size=auto` | 暂不开放（后续） | 开放 GIF 前必须补齐 auto 尺寸与结果渲染 |
| `size` 比例 | `ratio_size` + `ratios` | 无新增，沿用现有适配 |
| `resolution` 仅 1K | `hidden` + `default=1K` | 无新增 |
| `quality` | `select` | 请求字段、上游映射、计费档位 |
| `n` | `number` | 请求字段、多结果存储与响应（后续）、计费倍数 |
| `background` | `select` | 请求字段、上游映射 |
| `output_compression` | `number` + `visible_when` | 请求字段、上游映射 |
| `moderation` | `select` | 请求字段、上游映射 |
| 参考图上限 | `max_count`，需要时可用动态值 | 上游映射不变 |
| `mask_url` | `file` + `validators` | 请求字段、文件校验、首图尺寸校验 |
| `markup_url` | `file` + `conflicts_with` | 请求字段、文件校验 |
| `frame_count`、`frame_duration_ms` | 后续（依赖 auto） | 请求字段、上游映射、拼图约束 |
| `svg.model`、`svg.reasoning.effort` | `path` 为嵌套路径 | 请求字段、文本模型依赖 |
| 多结果（后续） | 本期不声明，正式版接入时另行设计 | 任务结果数组、API 响应、客户端渲染器 |
| 计费联动 | 规格不承载价格 | 扩展计费条件与倍数 |

本期 2.6 Ext 只接静态图片格式（png/jpeg/webp）；GIF/SVG 与 `auto` 尺寸依赖未完成的结果渲染与 auto 支持，暂不进入规格。

两个平台模型名的问题：2.6 Ext 的 `flare` 与 `sunburst` 建议继续拆成两个平台模型名，与 2.5 的命名保持一致。理由是计费条件只支持 `model` 等字段，`version` 不是计费维度，客户端也不需要额外的版本选择器。若选择单模型 + `version` 参数，则计费模块必须支持按 `version` 定价。
