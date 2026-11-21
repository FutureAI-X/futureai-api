# 视频参数规格

本文说明视频生成模型的 `param_spec` 结构、校验规则与客户端接入方式。图像侧见 [图像参数规格](../../image/params/README.md)。

## 1 范围与分层

视频规格与图像规格共用 `models.param_spec` 列，由模型 `type` 决定解析器：

- `type=image` → `imageparam`
- `type=video` → `videoparam`

校验分三层：

1. **管理端保存规格**：元校验，拒绝自相矛盾或超出全局上限的配置；
2. **上传完成**：平台全局的格式、大小、时长、宽高、帧率、编码检查，并落库探测元数据；
3. **生成请求**：按模型规格校验标量参数与参考媒体。规格缺失或非法时 fail-closed。

## 2 顶层结构

```json
{
  "version": 1,
  "params": [
    { "key": "prompt", "widget": "textarea", "required": true, "max_length": 2000 },
    { "key": "resolution", "widget": "segmented", "required": true,
      "options": ["480P", "768P", "2K"] },
    { "key": "duration", "widget": "number", "required": true,
      "min": 4, "max": 15, "step": 1, "integer": true },
    { "key": "ratio", "widget": "segmented", "required": true,
      "options": ["21:9", "16:9", "4:3", "1:1", "3:4", "9:16", "adaptive"] },

    { "key": "images", "path": "image_ids", "widget": "ref_images", "group": "reference",
      "max_count": 9,
      "allowed_mime_types": ["image/jpeg", "image/png", "image/webp", "image/heic", "image/heif"],
      "max_file_bytes": 31457280,
      "aspect_ratio": { "min": 0.4, "max": 2.5 },
      "dimensions": { "min": 256, "max": 5760 } },

    { "key": "audios", "path": "audio_ids", "widget": "ref_audios", "group": "reference",
      "max_count": 3,
      "allowed_mime_types": ["audio/wav", "audio/mpeg"],
      "max_file_bytes": 15728640,
      "duration_seconds": { "min": 2, "max": 15 },
      "max_total_duration_seconds": 15 },

    { "key": "videos", "path": "video_ids", "widget": "ref_videos", "group": "reference",
      "max_count": 3,
      "allowed_mime_types": ["video/mp4", "video/quicktime"],
      "max_file_bytes": 52428800,
      "duration_seconds": { "min": 2, "max": 15 },
      "max_total_duration_seconds": 15,
      "aspect_ratio": { "min": 0.4, "max": 2.5 },
      "dimensions": { "min": 256, "max": 5760 },
      "fps": { "min": 23.976, "max": 60 },
      "video_codecs": ["h264", "hevc"],
      "audio_codecs": ["aac", "mp3"] }
  ]
}
```

### 2.1 必填的标量参数

`prompt`、`resolution`、`duration`、`ratio` 必须存在、控件类型正确且 `required: true`。其他参数（参考媒体）都是可选的。

### 2.2 字段说明

| 字段 | 适用控件 | 说明 |
|---|---|---|
| `key` | 全部 | 参数标识，`^[a-z][a-z0-9_]{0,63}$`，同一规格内唯一 |
| `path` | 全部 | 请求字段路径；默认等于 `key` |
| `widget` | 全部 | 见第 3 节 |
| `label` / `hint` / `group` | 全部 | 展示元数据；`group` 取 `basic` / `reference` / `advanced` |
| `required` | 标量参数 | 必须为 `true`；参考媒体不能设为必填 |
| `min_length` / `max_length` | `textarea` | 最小/最大字符数 |
| `count_mode` | `textarea` | `codepoint`（默认）/ `utf16` / `gbk_bytes` / `cjk_weighted` |
| `options` | `segmented` | 枚举值 |
| `min` / `max` / `step` / `integer` | `number` | duration 必须设置 `min`、`max` 且 `integer: true` |
| `max_count` | 参考媒体 | 必填；`0` 表示不支持 |
| `allowed_mime_types` | 参考媒体 | `max_count > 0` 时必填 |
| `max_file_bytes` | 参考媒体 | 可选；不得超过全局单文件上限 |
| `aspect_ratio` | `ref_images` / `ref_videos` | 宽高比闭区间，`min > 0` |
| `dimensions` | `ref_images` / `ref_videos` | 宽和高的闭区间，单位 px |
| `duration_seconds` | `ref_audios` / `ref_videos` | 单文件时长闭区间，单位秒 |
| `max_total_duration_seconds` | `ref_audios` / `ref_videos` | 本次请求总时长上限，单位秒 |
| `fps` | `ref_videos` | 帧率闭区间 |
| `video_codecs` | `ref_videos` | `h264` / `hevc`，可接受别名并归一化 |
| `audio_codecs` | `ref_videos` | `aac` / `mp3` |

未设置的媒体约束不校验；只有 `max_count` 与 `allowed_mime_types` 是媒体支持的硬前提。

当前不支持 `default`、`visible_when`、`enabled_when`、`emit`、`conflicts_with`、`validators` 等扩展字段；规格里出现这些字段会被 `DisallowUnknownFields` 拒绝，避免配置看起来生效、实际被忽略。

`allowed_mime_types` 使用 MIME 而不是扩展名，常见对应关系：

| 扩展名 | MIME |
|---|---|
| JPG / JPEG | `image/jpeg` |
| PNG | `image/png` |
| WEBP | `image/webp` |
| HEIC | `image/heic` |
| HEIF | `image/heif` |
| WAV | `audio/wav` |
| MP3 | `audio/mpeg` |
| MP4 | `video/mp4` |
| MOV | `video/quicktime` |

## 3 控件与请求字段

| widget | path | 说明 |
|---|---|---|
| `textarea` | `prompt` | 提示词 |
| `segmented` | `resolution` | 输出分辨率枚举，统一大写 |
| `segmented` | `ratio` | 画面比例枚举，`adaptive` 统一小写 |
| `number` | `duration` | 输出时长，整数秒 |
| `ref_images` | `image_ids` | 参考图片 |
| `ref_audios` | `audio_ids` | 参考音频 |
| `ref_videos` | `video_ids` | 参考视频 |

## 4 字符计数

`count_mode` 的四种口径：

| 模式 | 汉字 | 英文字母 | 说明 |
|---|---|---|---|
| `codepoint` | 1 | 1 | Unicode code point，默认 |
| `utf16` | 1 | 1 | UTF-16 code unit，与 JS `String.length` 一致，增补平面字符算 2 |
| `gbk_bytes` | 2 | 1 | GB18030 编码字节数，中文 TTS/短信上游常用的“汉字算 2”口径 |
| `cjk_weighted` | 2 | 1 | 显式 Han=2、其它=1；与 GBK 在标点/假名/emoji 上可能不同 |

服务端与客户端必须使用同一个 `count_mode`。对接 TTS 上游时，以对方文档的精确规则为准。

## 5 时长口径

参考音频/视频的时长校验与计费使用同一口径：

```text
每个文件的秒数 = ceil(duration_ms / 1000)
单文件范围判断：min ≤ 秒数 ≤ max
总时长 = Σ 每个文件的秒数
```

因此 1.5 秒按 2 秒计。视频输出时长直接使用请求的 `duration`。

## 6 编码校验

- 视频流：**所有**视频流的编码都要在 `video_codecs` 中；
- 音频流：**所有**音频流的编码都要在 `audio_codecs` 中；
- 没有音频流是合法的，不会因为配置了 `audio_codecs` 就被拒绝；
- 编码名会归一化：`avc` / `avc1` / `H.264/AVC` → `h264`，`hevc` / `h265` / `hev1` / `hvc1` / `H.265/HEVC` → `hevc`；`AAC`、`MP3` 直接填写即可。

## 7 全局硬上限

视频媒体的规格必须是以下全局范围的子集；图片的宽高与宽高比不在上传阶段限制，规格可按模型能力配置。

| 项 | 全局范围 |
|---|---|
| 视频宽高 | 64–8192 px（图片上传不限制宽高，由模型规格约束） |
| 视频宽高比 | 0.1–10 |
| 音视频单文件时长 | 1–300 秒 |
| 视频帧率 | 1–240 |
| 图片/音频/视频单文件 | 30MB / 15MB / 200MB |
| 图片格式 | JPEG / PNG / WEBP / HEIC / HEIF |
| 音频格式 | WAV / MP3 |
| 视频格式 | MP4 / MOV |
| 视频编码 | H.264 / H.265 |
| 视频内音频编码 | AAC / MP3 |

上传阶段先按全局上限拦截，生成请求阶段再按模型规格拦截。模型规格比全局更宽时，管理端保存会返回 400。

## 8 分辨率与计费一致性

分辨率同时存在于：

- 参数规格的 `resolution.options`；
- 计费表 `video_credit_rates.resolution`。

保存任一边时，服务端要求两边的集合一致，不一致返回 400 并列出缺少的档位。这保证客户端展示的选项一定能通过计费查找。

## 9 缺失与非法规格

- 模型没有 `param_spec`：生成请求返回 503，`error_code=PARAM_SPEC_MISSING`；
- 规格解析失败：返回 503，`error_code=PARAM_SPEC_INVALID`；
- 请求参数不满足规格：返回 400，`error_code` 为具体错误码，`param` 为参数 key。

常见错误码：

| 错误码 | 含义 |
|---|---|
| `PARAM_REQUIRED` | 必填参数为空 |
| `PARAM_TYPE` | 参数类型不正确（如 duration 不是整数） |
| `PARAM_TOO_SHORT` / `PARAM_TOO_LONG` | 文本长度不符 |
| `PARAM_UNSUPPORTED_VALUE` | 枚举值不在 options 内 |
| `PARAM_TOO_SMALL` / `PARAM_TOO_LARGE` | 数值超出范围 |
| `PARAM_REF_TOO_MANY` | 参考媒体数量超限 |
| `PARAM_REF_TYPE_UNSUPPORTED` | MIME 不在允许列表 |
| `PARAM_REF_FILE_TOO_LARGE` | 单文件大小超限 |
| `PARAM_REF_DURATION_MISSING` / `PARAM_REF_DURATION_OUT_OF_RANGE` | 时长缺失/超出范围 |
| `PARAM_REF_TOTAL_DURATION_EXCEEDED` | 总时长超限 |
| `PARAM_REF_ASPECT_OUT_OF_RANGE` | 宽高比超出范围 |
| `PARAM_REF_DIMENSION_MISSING` / `PARAM_REF_DIMENSION_OUT_OF_RANGE` | 宽高缺失/超出范围 |
| `PARAM_REF_FPS_MISSING` / `PARAM_REF_FPS_OUT_OF_RANGE` | 帧率缺失/超出范围 |
| `PARAM_REF_CODEC_MISSING` / `PARAM_REF_CODEC_UNSUPPORTED` | 编码无法解析/不受支持 |

## 10 客户端接入

1. 调用 `GET /v1/models`，按 `type=video` 读取 `param_spec`；
2. 按 `params` 顺序渲染表单：`textarea` / `segmented` / `number` / 三个参考媒体上传控件；
3. 上传前按 `max_count`、`allowed_mime_types`、`max_file_bytes` 做本地预检；
4. 上传完成后结合顶层 `width` / `height` / `duration_ms` 与 `probe`（fps、编码）继续做宽高比、时长、总时长、帧率、编码校验；
5. 提交生成请求。服务端会重新校验，客户端校验只用于提前反馈。

`max_count=0` 或规格中不存在对应控件时，客户端应隐藏该媒体入口；提交时应清空对应字段。

## 11 已知限制

- 历史生成结果文件（`purpose=result`）目前不落库宽高。如果图像规格启用了 `dimensions` 或 `aspect_ratio`，把这类文件当作参考图会被判为「缺少宽高信息」。请重新上传为参考图，或暂时不启用这两项约束。
- 帧率取 `avg_frame_rate`（缺失时回落到 `r_frame_rate`）；VFR 视频按平均值判断。
- `gbk_bytes` 使用 GB18030 编码；极少数不在 GBK 范围内的字符会按 4 字节计。
- 参考视频/音频的时长校验与计费都使用逐文件向上取整后的秒数，可能与上游按真实时长结算存在边界差异。

## 12 代码位置

| 内容 | 位置 |
|---|---|
| 规格解析与元校验 | `videoparam/spec.go` |
| 请求参数与参考媒体校验 | `videoparam/validate.go` |
| 视频生成接入 | `controller/video.go` |
| 管理端保存与类型分发 | `controller/admin_model.go` |
| 分辨率一致性校验 | `controller/admin_video_credit_rule.go` |
| 模型列表下发规格 | `controller/model.go` |
