# 错误码

## 1 响应结构

现有响应结构保持不变，增加两个字段：

```json
{
  "code": "fail",
  "error_code": "PARAM_UNSUPPORTED_VALUE",
  "param": "output_format",
  "message": "output_format 只支持 png、jpeg、webp"
}
```

- `error_code` 是稳定枚举，供客户端定位控件与统计使用；
- `param` 是参数 `key`；
- 命名校验器失败时额外带 `validator`；
- `message` 供用户阅读，措辞可变，不得作为程序判断依据。

## 2 规格与通用错误

| 参数 | `error_code` | HTTP | 场景 |
|---|---|---|---|
| 规格 | `PARAM_SPEC_MISSING` | 503 | 模型未配置规格 |
| 规格 | `PARAM_SPEC_INVALID` | 503 | 规格解析或元校验失败，或供应商能力冲突 |
| 通用 | `PARAM_REQUIRED` | 400 | 必填参数缺失 |
| 通用 | `PARAM_TYPE` | 400 | 值的类型不符合控件 |
| 通用 | `PARAM_UNSUPPORTED_VALUE` | 400 | 不在 `options` 内 |
| 通用 | `PARAM_OUT_OF_RANGE` | 400 | 小于 `min` 或大于 `max` |
| 通用 | `PARAM_STEP` | 400 | 不符合 `step` |
| 通用 | `PARAM_TOO_SHORT` | 400 | 小于 `min_length` |
| 通用 | `PARAM_TOO_LONG` | 400 | 大于 `max_length` |
| 通用 | `PARAM_CONFLICT` | 400 | 互斥参数同时有值 |
| 通用 | `PARAM_DISABLED` | 400 | 隐藏或禁用参数被传入非默认值 |
| 通用 | `PARAM_VALIDATOR_FAILED` | 400 | 命名校验器失败 |

## 3 尺寸错误

| 参数 | `error_code` | HTTP | 场景 |
|---|---|---|---|
| size | `PARAM_SIZE_FORMAT` | 400 | 既不是比例也不是像素格式 |
| size | `PARAM_SIZE_MODE_UNSUPPORTED` | 400 | 当前 `modes` 不接受该写法 |
| size | `PARAM_SIZE_RATIO_UNSUPPORTED` | 400 | 比例不在 `ratios` 内 |
| size | `PARAM_SIZE_PIXEL_STEP` | 400 | 宽高不是 `step` 的倍数 |
| size | `PARAM_SIZE_PIXEL_MIN_SIDE` | 400 | 任意一边小于 `min_side` |
| size | `PARAM_SIZE_PIXEL_MAX_SIDE` | 400 | 任意一边大于 `max_side` |
| size | `PARAM_SIZE_PIXEL_MIN_PIXELS` | 400 | 总像素小于 `min_pixels` |
| size | `PARAM_SIZE_PIXEL_MAX_PIXELS` | 400 | 总像素大于 `max_pixels` |
| size | `PARAM_SIZE_PIXEL_ASPECT` | 400 | 宽高比超出 `aspect` |

## 4 参考图与文件错误

| 参数 | `error_code` | HTTP | 场景 |
|---|---|---|---|
| 参考图 | `PARAM_REF_TOO_MANY` | 400 | 张数超过 `max_count` |
| 参考图 | `PARAM_REF_FILE_TOO_LARGE` | 400 | 单张超过 `max_file_bytes` |
| 参考图 | `PARAM_REF_TOTAL_TOO_LARGE` | 400 | 合计超过 `max_total_bytes` |
| 参考图 | `PARAM_REF_TYPE_UNSUPPORTED` | 400 | MIME 不在 `allowed_mime_types` 内 |
| 文件 | `PARAM_FILE_TOO_LARGE` | 400 | 蒙版、标注等单文件超限 |
| 文件 | `PARAM_FILE_TYPE_UNSUPPORTED` | 400 | 单文件类型不允许 |

`max_count` 为 0 或 `ref_images` 不存在时不返回参考图相关错误码，按 [ref-images.md](components/ref-images.md) 静默忽略。
