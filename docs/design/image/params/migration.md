# 迁移与测试

## 1 种子数据

迁移时按下表配置现有模型。数值必须在接入前与上游文档复核。

| 模型 | size | resolution | 参考图 | prompt |
|---|---|---|---|---|
| `gpt-image-2` / `gpt-image-2-ext` | 比例 15 种或像素；`step=16`，单边 ≤ 3840，总像素 ≤ 8,294,400，宽高比 1:3–3:1 | 1K/2K/4K | 15 | 2000 |
| `gpt-image-2.5-flare-ext` / `sunburst-ext` | 比例 10 种 | 1K/2K/4K | 16 | 2000 |
| `moss-image-base-1.0` / `sft-1.0` | 像素；`step=16`，单边 256–2048，总像素 ≤ 2,097,152 | 无 | 0 | 4000 |
| `gpt-image-2.6-flare-ext` / `sunburst-ext` | 比例 10 种（auto 暂不开放） | 1K/2K/4K | 16 | 2000 |
| `seedream-5-5` | 比例 10 种（不支持像素、档位写法与 auto） | 1K/1.5K/2K | 10（单张 30MB，合计 300MB） | 2000 |
| `gpt-image-2.6` 正式版（按 token 计费） | 比例 10 种（auto 暂不开放） | 1K | 16 | 32000 |

2.6 的 GIF/SVG 参数与 `auto` 尺寸在后续阶段补入；本期种子只包含静态图片。

各模型的完整 `param_spec` 见对应设计文档：

- [gpt-image-2-ext.md](../gpt-image-2-ext.md)
- [gpt-image-2.5-flare-ext.md](../gpt-image-2.5-flare-ext.md)
- [gpt-image-2.5-sunburst-ext.md](../gpt-image-2.5-sunburst-ext.md)
- [gpt-image-2.6-flare-ext.md](../gpt-image-2.6-flare-ext.md)
- [gpt-image-2.6-sunburst-ext.md](../gpt-image-2.6-sunburst-ext.md)
- [seedream-5-5.md](../seedream-5-5.md)
- [moss-image-base-1.0.md](../moss-image-base-1.0.md)
- [moss-image-sft-1.0.md](../moss-image-sft-1.0.md)

## 2 阶段

P0：定义规格语法、Go 与 TypeScript 校验器、一致性测试向量、当前模型与 2.6 的黄金规格。此阶段不改变线上行为。

P1：服务端生效。

1. 增加 `param_spec` 列。
2. 把现有 APIMart 与 MOSS 的硬编码约束转换为 JSON，写入所有在售图像模型。
3. 增加管理端接口与页面。
4. `GET /v1/models` 输出 `param_spec_version` 与 `param_spec`。
5. 控制器接入引擎，扣费前校验。
6. 执行 [validation.md](validation.md) 的发布门禁查询，结果必须为空。
7. 供应商旧校验暂时保留一个版本作为双保险。

P2：客户端生效。按规格渲染与预校验，删除硬编码常量。必须在 P1 完成后发布。

P3：2.6 接入。在 P0/P1 的引擎上完成：

1. `ImageGenerateRequest` 新增字段与请求字段注册表；
2. 供应商适配层翻译；
3. 输出格式参数与结果渲染；多结果另行设计；
4. 计费条件与倍数；
5. 2.6 的命名校验器；
6. 2.6 的种子规格与客户端控件测试。

P4：清理。删除供应商适配层中的参数约束、`SizeRule` 与相关能力声明。

## 3 发布顺序与回滚

由于客户端在缺少规格时直接禁用生成，服务端必须先升级，客户端后升级。

回滚顺序相反：先回滚客户端，再回滚服务端。若只回滚服务端，新客户端会因为拿不到 `param_spec_version` 而整体禁用生成。

`param_spec` 列对旧版本服务端无影响，回滚时无需删除。

## 4 一致性测试

规范文件为 `imageparam/testdata/conformance-v1.json`，Go 与 TypeScript 的测试读取同一份内容。`futureai-image` 中的副本必须逐字节一致，CI 校验 SHA-256。

测试向量至少覆盖：

1. 控件类型：每种控件的合法值、非法类型、必填与默认值。
2. 条件：`eq`、`neq`、`in`、`not_in`、`empty`、`not_empty`、`truthy`、`falsy`、`not`、`all`、`any` 的组合。
3. 动态值：`map` 命中与未命中、引用缺失、循环检测。
4. `emit`：`value`、`const`、`none`、`map`、`when`，以及空字符串与 `allow_empty`。
5. `ratio_size` 与 `pixel_size`：比例/像素格式、`modes`、预设、自定义值、边界值、宽高比交叉相乘。
6. prompt：中文、emoji、ZWJ 组合在 `codepoint` 与 `utf16` 下的边界。
7. 参考图与文件：张数、单张大小、合计大小、MIME。
8. 命名校验器：成功、失败、客户端未实现时跳过。
9. 元校验：未知 widget、未知字段、未知路径、循环、超限、默认值非法。
10. 黄金规格：当前在售模型与 2.6 Ext 的完整规格必须能通过元校验，并由引擎产出预期结果。

另外增加一层种子一致性测试：遍历每个模型的 `param_spec`，断言其允许集合与旧 `ValidateImageRequest` 的允许集合一致。该测试只在迁移期存在，P4 后删除。

## 5 风险

| 风险 | 说明 | 处理 |
|---|---|---|
| 通用结构的复杂度 | 条件 DSL、动态值、序列化表达式都会增加引擎面积 | 严格限制语法，不做算术；每种能力都有测试向量 |
| 服务端仍需逐参数实现 | 通用规格只降低客户端成本，不替代请求字段与上游翻译 | [server.md](server.md) 的注册表强制三件事同时完成 |
| 计费联动 | 当前计费条件只有四个字段；2.6 Ext 的 `quality` / `output_format` / `complexity` 不参与定价，`n` / `frame_count` 后续需要倍数计费 | Ext 不需要扩展；开放 `n` 或 GIF 时再扩展计费模块 |
| 多结果 | 任务存储与 API 响应目前按单资产设计 | 2.6 正式版接入时单独设计 |
| `auto` 尺寸缺失 | 2.6 的 GIF 依赖 `size=auto`，本期未开放 | 开放 GIF 前补齐 auto 尺寸与结果渲染 |
| L5 运行时数据依赖 | 蒙版尺寸依赖用户上传的首图 | 首期只在服务端阻断，客户端做提示 |
| 未知请求字段被静默忽略 | 现有契约忽略不认识的字段 | 客户端只提交规格内的字段；服务端文档明确该行为 |
| 外部 URL 参考图无法校验大小 | 不下载无法确认 | 只对 `image_ids` 校验大小与 MIME |
| 文案与 i18n | 规格中的 `label`、`hint` 是中文时难以国际化 | 标准 key 由客户端维护文案 |
| 客户端测试框架 | `futureai-image` 当前没有测试框架 | P0 阶段决定是否引入 vitest，至少覆盖解释器与状态 rebase |

## 6 后续扩展

- 输出格式过滤、结果类型与多结果渲染：在 2.6 正式版接入时设计。
- 2.6 的 `auto` 尺寸：开放 GIF 前必须支持；届时扩展 `ratio_size` 的 `modes` 或新增控件。
- 同一平台模型对应多个能力不同的供应商时，公开规格取交集；确需差异时增加可选的 `vendor_overrides`。
- 视频生成可复用同一列与同一套引擎，通过模型的 `type` 区分规格内容。
- 若需要在上传阶段就按模型限制文件大小，可给 `prepare` 接口增加可选的 `model` 参数。
