# 图像生成参数规格与校验引擎

> 状态：第一阶段已实现。服务端 `imageparam` 包、`models.param_spec`、管理端接口与客户端规格驱动渲染已接入；当前支持 `textarea`、`segmented`、`ratio_size`、`pixel_size`、`ref_images`。条件、动态值、`emit`、命名校验器与其余控件尚未实现。

## 1 背景与目标

图像生成的参数约束目前分散在 `supplier/apimart.go`、`supplier/size.go`、`supplier/moss.go`、`controller/file_upload.go` 与 `futureai-image` 的 `ImageGeneration.tsx`。这些约束已经不一致，且每接一个模型都要同时改服务端与客户端。

GPT-Image-2.6 的接入改变了前提：

- `output_format` 会同时改变参考图是否可用、`size` 的取值方式和结果渲染形态；
- `quality`、`n`、`background`、`output_compression`、`moderation` 是新增参数；
- GIF 有帧数与帧时长，且存在「帧数 × 单帧尺寸」的跨参数约束；
- SVG 与参考图、蒙版互斥，且依赖文本模型；
- 蒙版尺寸依赖用户刚上传的首张参考图。

固定四段结构无法表达这些联动，因此采用通用参数数组：每个参数由 `key`、`widget`、默认值、条件、选项与校验规则描述。客户端只保留解释器和控件注册表，不再持有模型知识。

目标：

1. 参数规格以平台模型为唯一事实源，存储于 `models.param_spec`。
2. 新模型复用已有控件与声明式条件时，客户端不需要发版。
3. 服务端在扣费前做权威校验，客户端做提交前提示。
4. 规格缺失或非法时直接拒绝生成，不回退到旧的硬编码校验。
5. 两端校验逻辑共享同一份一致性测试向量。

## 2 关键决策

| 编号 | 决策 |
|---|---|
| D1 | 参数规格挂在 `models.param_spec`，不挂在 `vendor_models` |
| D2 | 采用通用 `params[]` 结构，而不是固定四段 |
| D3 | 规格缺失或非法直接失败，不回退；返回 503 与明确提示 |
| D4 | 规格格式版本由 `/v1/models` 顶层的 `param_spec_version` 统一表示，模型配置内不带版本字段 |
| D5 | 服务端权威，客户端只做声明式校验 |
| D6 | 客户端拿不到规格直接禁用生成 |
| D7 | 参考图不支持时静默忽略，不报错 |
| D8 | 跨参数数学约束用命名校验器，不扩展条件 DSL |
| D9 | 2.6 的输出格式、多个结果与计费联动必须与参数引擎同期设计 |
| D10 | `size` 拆成 `ratio_size` 与 `pixel_size` 两个独立前端组件，不复用同一套 UI |
| D11 | mixed 模型的接口层保持接受比例与像素，前端只暴露 `ratio_size` |
| D12 | `auto` 尺寸暂不开放；开放 GIF 前必须补齐 |

## 3 文档目录

| 文档 | 内容 |
|---|---|
| [schema.md](schema.md) | 顶层结构、参数对象、控件索引、条件、动态值、序列化 |
| [validation.md](validation.md) | 元校验、运行时校验、空/非法规格、默认值与命名校验器 |
| [errors.md](errors.md) | 错误响应结构与错误码 |
| [api.md](api.md) | `/v1/models`、管理端接口与页面 |
| [client.md](client.md) | 客户端解释器、未知内容处理、状态与缓存 |
| [server.md](server.md) | 服务端引擎、请求字段注册表、供应商适配 |
| [dependencies.md](dependencies.md) | 计费联动、输出格式、多结果与 2.6 接入清单 |
| [migration.md](migration.md) | 种子数据、迁移阶段、发布回滚、一致性测试、风险 |
| [components/README.md](components/README.md) | 控件目录与选择指南 |

每个控件的 JSON 设计、字段、校验、前端与服务端行为都在 `components/` 下独立成文。模型的具体配置示例放在各模型的设计文档中，例如 [gpt-image-2-ext.md](../gpt-image-2-ext.md)、[moss-image-base-1.0.md](../moss-image-base-1.0.md)。

## 4 术语

| 术语 | 含义 |
|---|---|
| 平台模型 | `models` 表中的一行，是客户端看到并调用的模型 |
| 参数规格 | `models.param_spec` 中的 JSON，描述该模型接受的参数与限制 |
| 元校验 | 对参数规格本身的结构与取值进行校验，在管理端保存时执行 |
| 运行时校验 | 对一次生成请求的参数进行校验，在服务端扣费前执行 |
| 控件 | `widget` 字段，决定前端渲染方式与服务端解析方式 |

## 5 存储与版本

`models` 表新增一列：

```sql
ALTER TABLE models ADD COLUMN param_spec jsonb;
```

约束：

- 可空。`NULL` 表示该模型尚未配置参数规格。
- 单条大小上限 64KB，由管理端接口限制。
- 仅图像模型使用。非图像模型不读取该列。
- 不挂在 `vendor_models` 上。客户端、计费、端点绑定都以平台模型为维度。

规格格式版本由 `GET /v1/models` 顶层的 `param_spec_version` 表示，当前为 `1`。模型配置内部不带版本字段。新增可选字段保持向后兼容；删除字段、改变语义或改变必填性属于不兼容变更，必须提升 `param_spec_version`。

## 6 非目标

- 不把任意算法写进 JSON。跨参数数学约束与运行时数据依赖使用命名校验器。
- 不引入 `react-jsonschema-form` 等通用表单库。
- 不在服务端做比例与像素换算。
- 不提供「拉不到规格就用内置兜底」的路径。
- 页面、草稿、轮询、历史的完整设计不在本目录范围，只在 [dependencies.md](dependencies.md) 说明参数引擎需要的元数据。
