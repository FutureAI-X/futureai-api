# 校验

校验分为两层：管理端保存规格时的元校验，以及服务端处理生成请求时的运行时校验。客户端的声明式校验只服务体验，服务端始终是权威。

## 1 元校验

管理端保存规格时执行以下校验。任一条不满足即返回 400，并给出字段级原因。

1. 顶层 `params` 非空，`key` 唯一，`path` 唯一。
2. `key` 匹配 `^[a-z][a-z0-9_]{0,63}$`；`path` 必须是服务端注册过的请求字段。
3. `widget` 在白名单内；`group` 在 `basic`、`reference`、`advanced` 内。
4. `required` 为 `true` 时不得同时配置 `default`。
5. `default` 的类型与控件一致，且满足该控件的全部约束。
6. `options` 非空，值唯一；非 `allow_empty` 时 `default` 必须在其中。
7. `min <= max`，`step > 0`；`min_length <= max_length`；`count_mode` 为合法枚举。
8. `visible_when`、`enabled_when` 的 `field` 必须引用已存在的 `key`；条件深度不超过 8，节点数不超过 64。
9. `conflicts_with` 必须引用已存在的 `key`。
10. 动态值的 `by` 必须引用已存在的 `key`；动态值不得嵌套；引用图不得有环。
11. `ratio_size` 的 `ratios` 非空，取值必须是合法比例或字面量 `auto`（不得重复），`default`（如有）必须出现在 `ratios` 中；`auto` 是否开放只看它是否出现在 `ratios` 里，不参与比例格式校验；`modes` 默认 `["ratio"]`，只能是 `ratio`/`pixel` 子集且必须包含 `ratio`；`modes` 含 `pixel` 时 `pixel` 必填，不含时不得配置。`pixel_size` 的 `pixel` 必填，`modes` 只能是 `["pixel"]` 或省略，不得配置 `ratios`；`presets` 可省略，每项 `ratio` 必填且唯一、`pixels` 满足像素规则且唯一，`ratio` 与像素宽高比精确匹配；`allow_custom` 为 `false` 时至少一个预设，且默认值必须在预设内。
12. `ref_images` 的 `max_count >= 0`；`max_count > 0` 时 `max_file_bytes > 0`、`max_total_bytes >= max_file_bytes`、`allowed_mime_types` 非空；`max_count` 的每个分支都不得大于计费侧的参考图封顶值 100。
13. `ref_images` 与 `file` 的 `max_file_bytes` 不得大于上传接口的全局单文件上限 30MB，否则该限制永远无法触达。
14. `file` 控件的 `max_file_bytes > 0`，`allowed_mime_types` 非空。
15. `emit` 的操作符在白名单内；`map` 的键必须是当前控件的合法值；`ratio_size` 与 `pixel_size` 禁止 `emit map`。
16. `validators` 中的名称必须在服务端校验器注册表中存在。
17. 规格总大小不超过 64KB；参数数量不超过 64；单个参数的 `options` 不超过 100；`ratios` 不超过 50。
18. 保存时严格拒绝未知字段。字段拼写错误必须报错，不能静默忽略。

元校验与运行时校验使用同一份解析实现，避免两套规则。

## 2 运行时校验

服务端按以下顺序处理，所有参数错误都在扣费与建任务之前返回：

1. 解析请求体。
2. 查找端点与平台模型。
3. 读取 `param_spec`。
4. 用请求值与默认值构造参数值集合，按依赖顺序解析动态值。
5. 计算每个参数的可见性与可用性；隐藏参数使用默认值，禁用参数不得有值。
6. 逐个校验可见且可用的参数。
7. 处理幂等键。命中已有任务时直接返回原任务 ID。
8. 解析参考图、蒙版、标注等文件，校验张数、单张大小、合计大小与 MIME。
9. 执行命名校验器。校验器可以使用第 8 步得到的文件元数据。
10. 计算积分。
11. 扣费并创建任务。
12. 交给供应商适配层，只做传输转换。

## 3 空与非法规格

规格缺失与规格非法是两种不同状态，但都直接拒绝生成，不回退到旧校验。

| 状态 | 判定 | HTTP | `error_code` | 对外提示 |
|---|---|---|---|---|
| 未配置 | `NULL`、空串、空白串、JSON `null` | 503 | `PARAM_SPEC_MISSING` | 该模型参数配置缺失，暂不可用 |
| 非法 | 非空但解析失败或元校验失败 | 503 | `PARAM_SPEC_INVALID` | 该模型参数配置无效，暂不可用 |

具体解析错误只写服务端日志，不向调用方暴露。使用 503 而非 400：这是平台侧配置问题。

由于不存在回退，上线前必须完成所有在售图像模型的规格配置。发布门禁查询：

```sql
SELECT id, name
FROM models
WHERE type = 'image'
  AND (param_spec IS NULL OR btrim(param_spec::text) IN ('', 'null'));
```

查询结果必须为空。管理端模型列表同时返回 `param_spec_valid`。

保存与运行时的严格程度不同：保存时严格拒绝未知字段；运行时忽略未知字段，以便新旧版本滚动升级。

## 4 默认值、隐藏与禁用

- 参数省略时使用 `default`；可见的 `required` 参数省略时报错。
- `default` 为动态值时，按当前值集合解析后再校验。
- 动态值的解析顺序由 `by` 引用图决定；不存在依赖的值视为空。
- 隐藏参数使用默认值参与序列化；调用方传入不同的值时报 `PARAM_DISABLED`。
- 禁用参数有非空值时同样报 `PARAM_DISABLED`。
- 不再保留全局默认值 `16:9` 与 `1K`。默认值只来自规格。

`conflicts_with` 在全部参数校验完成后统一检查：可见且可用的参数双方同时有非空值时报 `PARAM_CONFLICT`。

## 5 按控件校验

| 控件 | 校验 |
|---|---|
| `textarea`、`text` | `required`、`min_length`、`max_length`、`count_mode`、`allow_empty` |
| `segmented`、`select` | `required`、选项合法性、`multiple`、`allow_empty` |
| `number` | `required`、类型、`min`、`max`、`step` |
| `switch` | 类型 |
| `ratio_size` | 格式、`modes`、比例集合；`modes` 含 `pixel` 时校验像素规则 |
| `pixel_size` | 格式、像素规则、预设与自定义值 |
| `ref_images` | 张数、单张大小、合计大小、MIME；`max_count` 为 0 时静默忽略 |
| `file` | 单张大小、MIME |
| `hidden` | 只允许默认值或 `emit` 结果 |

控件细节见 [components/](components/README.md)。

## 6 命名校验器

跨参数数学约束与运行时数据依赖不扩展条件 DSL，使用命名校验器：

```json
{
  "validators": ["gif_sheet"]
}
```

- 服务端按名称查注册表；名称不存在时在保存阶段就拒绝。
- 校验器可以访问完整参数值集合、文件元数据与模型类型。
- 客户端只实现白名单内的校验器；不认识的校验器跳过，由服务端返回 400。
- 失败时返回 `PARAM_VALIDATOR_FAILED`，并带 `validator` 字段。

2.6 至少需要以下校验器：

| 名称 | 约束层级 | 说明 |
|---|---|---|
| `gif_sheet` | L4 | 帧数 × 单帧尺寸决定拼图是否合法 |
| `mask_matches_first_image` | L5 | 蒙版为 Alpha PNG，尺寸与首张参考图一致 |
| `markup_matches_first_image` | L5 | 标注层尺寸与首张参考图一致 |
| `svg_requires_text_model` | L3 | SVG 输出要求服务端存在可用文本模型 |

`mask_matches_first_image` 与 `markup_matches_first_image` 首期只在服务端实现；客户端在拿到参考图尺寸后可以做同样的提示，但不作为提交阻断条件。错误码见 [errors.md](errors.md)。
