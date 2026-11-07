# 客户端

客户端只保留解释器和控件注册表，不再维护模型 Profile，也不保留 `SIZE_GROUPS`、`RESOLUTION_OPTIONS`、prompt 上限、参考图张数与大小等硬编码常量。

## 1 模块划分

```text
src/params/schema.ts     规格类型与形状校验
src/params/engine.ts     evaluate(schema, values) → 可见、可用、错误、请求体
src/params/widgets/      控件注册表
src/services/parameterSchema.ts  随模型列表缓存规格
```

## 2 求值结果

```ts
interface FormEval {
  visible: Record<string, boolean>;
  enabled: Record<string, boolean>;
  errors: Record<string, string>;
  notices: string[];
  isValid: boolean;
  request: Record<string, unknown>;
  images?: UploadedImage[];
}
```

`request` 只包含可见、可用、非空且经过 `emit` 的字段。

## 3 未知内容处理

| 情况 | 行为 |
|---|---|
| 未知可选 widget | 隐藏控件，使用默认值；没有默认值时省略；提示「当前客户端不支持该参数，将使用服务端默认值」 |
| 未知必填 widget | 禁用生成，提示升级客户端 |
| 未知条件操作符 | 视为真，提示；由服务端校验 |
| 未知 `emit` 操作符 | 不发送该字段，提示；由服务端默认值兜底 |
| 未知校验器 | 跳过，由服务端返回 400 |
| 未知 `group` | 归入 `advanced` |
| 未知渲染器 | 回退为下载原文件 |

## 4 状态与切换

- 表单只有一个 `values: Record<string, unknown>`；
- 切模型时保留两边都存在的 `key`，非法枚举回退到默认值并产生提示；
- 不自动清洗隐藏字段，切回来能恢复；
- 序列化时剔除隐藏、禁用与空值字段。

## 5 规格获取

- 规格随 `/v1/models` 一起缓存，key 为 `baseUrl + apiKey`，增加短 TTL 或手动刷新；
- 模型列表请求失败时沿用现有错误处理，不标记模型不可用；
- 单个图像模型没有规格或解析失败时，该模型置灰并标注「参数配置缺失，暂不可用」；
- 服务端没有 `param_spec_version` 时，禁用生成并提示服务端版本过低；
- `param_spec_version` 高于客户端支持的最高版本时，禁用生成并提示客户端版本过低；
- 不提供内置兜底规格。

## 6 尺寸组件

`ratio_size` 与 `pixel_size` 是两个独立组件：

- `ratio_size` 渲染比例按钮，提交比例；mixed 模型也只渲染比例按钮；
- `pixel_size` 渲染预设卡片与宽、高两个数字输入，提交像素；预设由管理端配置，修改后标记「自定义」。

组件细节见 [ratio-size.md](components/ratio-size.md) 与 [pixel-size.md](components/pixel-size.md)。
