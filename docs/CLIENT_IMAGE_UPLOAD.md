# Electron 客户端图片上传接入指南

本文说明 Electron 客户端如何把本地图片上传为参考图，并取得 `file_id` 用于图像生成。适用范围：`futureai-image` 等需要给 `/v1/images/generations` 传参考图的桌面应用。

服务端接口定义见 [API.md](API.md#46-参考媒体上传)；`image_ids` 的生成侧规则见 [API.md](API.md#41-图像生成的请求标准)。

## 1 与旧上传方式的区别

旧方式 `POST /v1/uploads/images` 以 `multipart/form-data` 把文件传到平台服务端，由平台转存后返回公开 URL，客户端再把 URL 填进 `image_urls`。新方式分三步：申请凭证、客户端直传对象存储、调用完成接口。完成后返回 `file_id`，填进图像生成请求的 `image_ids`。

| 项目 | 旧方式 | 新方式 |
|---|---|---|
| 上传内容传输 | 先传到平台，再由平台转存 | 图片直接传到对象存储 |
| 返回标识 | URL | `file_id` |
| 生成侧字段 | `image_urls` | `image_ids` |
| 文件可见性 | 公开可读 | 私有，仅上传者可用 |
| 大小上限 | 10MB | 30MB |
| 有效期 | 按结果文件策略（默认 24 小时） | 完成上传后 6 小时 |

新方式把参考图上限从 10MB 提高到 30MB，图片不再经过平台服务端转发，文件也保持私有。新项目应直接使用 `image_ids`。

## 2 上传流程

完整流程有五个步骤，任何一步失败都不要跳到下一步。

1. 读取本地文件，取得字节数和 MIME 类型。
2. 调用 `POST /v1/files/upload/prepare`，取得上传地址 `url`、表单字段 `fields`、`upload_id`。
3. 以 `multipart/form-data` 把 `fields` 和文件 POST 到 `url`。
4. 调用 `POST /v1/files/upload/complete`，提交 `upload_id`。
5. 把响应中的 `file_id` 填进图像生成请求的 `image_ids`。

上传凭证有效期 30 分钟，服务端上传会话有效期 40 分钟。超过 40 分钟仍未调用 `complete`，必须重新 `prepare`。

## 3 申请上传凭证

请求：

```http
POST /v1/files/upload/prepare
Authorization: Bearer sk-...
Content-Type: application/json

{
  "media_type": "image",
  "filename": "reference.png",
  "content_type": "image/png",
  "size": 1048576
}
```

请求字段：

| 字段 | 必填 | 说明 |
|---|---|---|
| `media_type` | 是 | 固定为 `image` |
| `filename` | 否 | 原始文件名，仅用于记录；服务端会清理非法字符 |
| `content_type` | 是 | 图片 MIME 类型，见 7.1 节 |
| `size` | 是 | 文件实际字节数，必须大于 0 且不超过 30MB |

响应示例：

```json
{
  "upload_id": "file_9f2c1d7e8a4b",
  "file_id": "file_9f2c1d7e8a4b",
  "url": "https://<bucket>.cos.<region>.myqcloud.com/",
  "fields": {
    "key": "staging/file_9f2c1d7e8a4b.png",
    "policy": "...",
    "q-sign-algorithm": "sha1",
    "q-ak": "...",
    "q-key-time": "...",
    "q-signature": "...",
    "Content-Type": "image/png",
    "success_action_status": "200"
  },
  "expires_at": "2026-11-02T10:30:00Z",
  "session_expires_at": "2026-11-02T10:40:00Z"
}
```

`upload_id` 与 `file_id` 在 prepare 阶段相同。完成上传前 `file_id` 不可用于生成；`upload_id` 只用于调用 `complete`。

`expires_at` 是上传凭证（POST policy）的过期时间，`session_expires_at` 是上传会话的过期时间。两者都过期后只能重新 `prepare`。

## 4 直传对象存储

这一步不经过平台服务端，客户端把表单直接 POST 到 `prepare` 返回的 `url`。

### 4.1 构造表单

按以下顺序构造 `multipart/form-data`：

1. 依次追加 `fields` 中的全部键值，名称与值都不能改动。
2. 最后追加一个名为 `file` 的字段，内容为图片字节，文件名用原始文件名。

> **注意**：不要手动设置请求的 `Content-Type`。浏览器或 `fetch` 需要自动生成带 boundary 的 `multipart/form-data` 头；手动设置会导致服务端无法解析表单。

`fields` 中的 `key` 决定对象路径，`policy`、`q-*` 是签名信息，任一项缺失或被修改都会得到 403。

### 4.2 在 Electron 主进程上传

建议在主进程完成上传：主进程不受渲染进程的跨域限制，也不需要给对象存储桶配置 CORS。使用 Electron 的 `net.fetch` 而不是 Node 原生的 `fetch`，前者会遵循系统代理设置。

以下示例封装完整的三步流程，可在主进程直接调用：

```ts
import { net } from 'electron'
import { promises as fs } from 'node:fs'
import path from 'node:path'

const API_BASE = 'https://futureaiapi.com'

interface PreparedUpload {
  upload_id: string
  file_id: string
  url: string
  fields: Record<string, string>
}

export async function uploadReferenceImage(
  apiKey: string,
  filePath: string,
): Promise<string> {
  const bytes = await fs.readFile(filePath)
  const filename = path.basename(filePath)
  const contentType = guessImageContentType(filename)

  const prepareResp = await net.fetch(`${API_BASE}/v1/files/upload/prepare`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${apiKey}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      media_type: 'image',
      filename,
      content_type: contentType,
      size: bytes.byteLength,
    }),
  })
  const prepared = (await prepareResp.json()) as PreparedUpload & {
    message?: string
  }
  if (!prepareResp.ok) {
    throw new Error(prepared.message ?? `prepare 失败: ${prepareResp.status}`)
  }

  const form = new FormData()
  for (const [name, value] of Object.entries(prepared.fields)) {
    form.append(name, value)
  }
  form.append(
    'file',
    new Blob([new Uint8Array(bytes)], { type: contentType }),
    filename,
  )

  // 不要设置 Content-Type，交给 fetch 生成 multipart boundary。
  const uploadResp = await net.fetch(prepared.url, {
    method: 'POST',
    body: form,
  })
  if (!uploadResp.ok) {
    throw new Error(`直传失败: ${uploadResp.status} ${await uploadResp.text()}`)
  }

  const completeResp = await net.fetch(
    `${API_BASE}/v1/files/upload/complete`,
    {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${apiKey}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ upload_id: prepared.upload_id }),
      // complete 可能触发媒体探测，服务端最长等待 90 秒。
      signal: AbortSignal.timeout(120_000),
    },
  )
  const completed = (await completeResp.json()) as {
    file_id?: string
    message?: string
  }
  if (!completeResp.ok || !completed.file_id) {
    throw new Error(completed.message ?? `complete 失败: ${completeResp.status}`)
  }

  return completed.file_id
}

function guessImageContentType(filename: string): string {
  switch (path.extname(filename).toLowerCase()) {
    case '.jpg':
    case '.jpeg':
      return 'image/jpeg'
    case '.png':
      return 'image/png'
    case '.webp':
      return 'image/webp'
    default:
      throw new Error('仅支持 JPG、PNG、WEBP 图片')
  }
}
```

`FormData` 与 `Blob` 需要 Electron 28 或更高版本（Node.js 18+）。更低版本可改用 `form-data` 包，但需要自行处理流式请求的 boundary。

渲染进程通过 IPC 调用主进程：

```ts
// 主进程：apiKey 由客户端的配置模块读取，不暴露给渲染进程
import { ipcMain } from 'electron'
ipcMain.handle('upload-reference-image', (_event, filePath: string) =>
  uploadReferenceImage(apiKey, filePath),
)

// 渲染进程
const fileId = await window.electronAPI.uploadReferenceImage(filePath)
```

### 4.3 文件选择

主进程使用 `dialog.showOpenDialog` 获取路径。文件过滤器应只放行业务支持的格式：

```ts
const result = await dialog.showOpenDialog({
  properties: ['openFile'],
  filters: [{ name: '图片', extensions: ['jpg', 'jpeg', 'png', 'webp'] }],
})
```

如果客户端必须支持 HEIC/HEIF，先转换为 JPG/PNG/WEBP 再上传：上传接口接受 HEIC/HEIF，但图像生成接口当前会拒绝这两种格式（见 7.3 节）。

### 4.4 直传失败的处理

直传可能遇到以下失败：

| 现象 | 原因 | 处理 |
|---|---|---|
| 403 | `fields` 被修改、签名过期、`key` 不匹配 | 重新 `prepare` |
| 400 | 表单缺少字段，或 `file` 字段不在最后 | 检查表单构造 |
| 网络错误 | 代理、断网、对象存储不可达 | 重试同一次直传，凭证在 30 分钟内仍然有效 |

直传成功后不要再次上传同一凭证。对象存储返回 200 即表示对象已写入，响应体可能为空。

## 5 完成上传

请求：

```http
POST /v1/files/upload/complete
Authorization: Bearer sk-...
Content-Type: application/json

{ "upload_id": "file_9f2c1d7e8a4b" }
```

响应示例：

```json
{
  "file_id": "file_9f2c1d7e8a4b",
  "media_type": "image",
  "content_type": "image/png",
  "bytes": 1048576,
  "duration_ms": 0,
  "width": 1024,
  "height": 1024,
  "expires_at": "2026-11-02T16:25:00Z"
}
```

`complete` 会读取对象、校验文件头与图片宽高，校验通过后才把对象从 `staging/` 复制到正式路径并标记为可用。因此它比普通接口慢，客户端超时应设为 120 秒以上。

`complete` 是幂等的：同一个 `upload_id` 重复调用会返回同一份元数据，不会重复复制对象。网络超时后可以直接重试。

响应中的 `expires_at` 是参考图的实际过期时间，从完成时刻起 6 小时。

## 6 在生成接口中使用 file_id

图像生成请求把 `file_id` 放进 `image_ids`：

```json
{
  "model": "gpt-image-2.5-flare-ext",
  "prompt": "a cat sitting on a windowsill",
  "resolution": "2k",
  "image_ids": ["file_9f2c1d7e8a4b"]
}
```

生成侧的规则：

- `image_ids` 非空时只认它，`image_urls` 被忽略；空数组或缺省时回退 `image_urls`。
- 去重后最多 15 张，超过返回 400。
- file_id 必须属于当前 API Key 的用户，用途为 `reference` 或 `result`，状态为 `ready`。
- 文件剩余有效期不足 30 分钟时返回 410，需要重新上传。

用户自己生成且仍在有效期内的图片结果也可以作为参考图。结果文件的 `file_id` 来自任务完成后的结果，不需要再次上传。

视频生成接口的参考媒体同样使用文件上传，但字段是 `images` / `videos` / `audios`，见 [API.md](API.md#45-视频生成)。

## 7 限制与有效期

### 7.1 图片规格

上传前应先在客户端校验以下规格，避免上传完成后才失败：

| 项目 | 限制 |
|---|---|
| 格式 | JPG/JPEG、PNG、WEBP、HEIC、HEIF |
| 不支持 | GIF、AVIF |
| 大小 | ≤ 30MB |
| 宽高 | 每边 300–5500 像素 |
| 宽高比 | 0.4–2.5 |

服务端在 `complete` 阶段按文件头识别真实格式，并以识别结果覆盖记录中的 `content_type`；无法识别的格式（如 GIF、AVIF）会被拒绝。文件名后缀不参与判断。

### 7.2 配额与限流

每个用户的参考媒体总配额为 10GB，按未过期文件之和计算。配额不足时 `prepare` 返回 507。

`prepare` 与 `complete` 都属于 `/v1` 端点，按用户限流。默认每分钟 60 次、突发 10 次，超限返回 429，响应头带 `Retry-After`。

### 7.3 有效期

各阶段的有效期如下：

| 项目 | 时长 | 说明 |
|---|---|---|
| 上传凭证 | 30 分钟 | `prepare` 响应的 `expires_at` |
| 上传会话 | 40 分钟 | 必须在此之前调用 `complete` |
| 参考图 | 6 小时 | 从 `complete` 成功起算 |
| 可用于生成 | 剩余 ≥ 30 分钟 | 低于该值返回 410 |

参考图是私有文件，不能通过 `GET /v1/images/get/:file_id` 读取。服务端在生成时把它转换成短期签名地址交给上游，客户端不需要也不应该自行拼接访问地址。

HEIC/HEIF 可以在上传阶段完成，但当前不能在图像生成中使用，`image_ids` 会返回 400。需要支持这两种格式时，客户端应先转换为 JPG/PNG/WEBP。

## 8 错误处理

`prepare` 与 `complete` 的业务错误响应形如：

```json
{ "code": "fail", "message": "文件大小超过上限（30 MB）" }
```

认证失败与限流使用 OpenAI 风格的错误结构，客户端应同时兼容两种：

```json
{ "error": { "message": "无效的 API Key", "type": "authentication_error" } }
```

`prepare` 的常见状态码：

| 状态码 | 场景 | 处理 |
|---|---|---|
| 400 | `media_type`、`content_type`、`size` 不合法 | 修正请求，不要重试 |
| 401 | API Key 无效 | 提示用户重新登录或配置密钥 |
| 429 | 触发限流 | 按 `Retry-After` 等待后重试 |
| 507 | 用户媒体配额已满 | 提示用户清理已上传文件 |
| 503 | 对象存储暂不可用 | 稍后重试 |

`complete` 的常见状态码：

| 状态码 | 场景 | 处理 |
|---|---|---|
| 400 | 文件为空、格式不符、宽高或宽高比超限 | 重新选择文件并 `prepare` |
| 404 | `upload_id` 不存在或不属于当前用户 | 检查是否混用了其他用户的凭证 |
| 409 | 对象尚未上传完成 | 等待 1–2 秒后重试 `complete` |
| 410 | 上传会话已过期 | 重新 `prepare` 并重新上传 |
| 502 | 媒体探测或复制暂时失败 | 用同一 `upload_id` 重试，不要重新上传 |
| 503 | 对象存储不可用 | 稍后重试 |

因文件校验失败返回的 400 会把文件标记为失败，同一个 `upload_id` 无法继续使用，必须重新 `prepare`。502 不影响文件状态，可以放心重试。

## 9 常见问题

**上传成功但 `complete` 返回 409**

对象存储写入有短暂延迟，或直传请求实际失败但客户端没有检查响应。先确认直传返回 200，再间隔 1–2 秒重试 `complete`。

**`complete` 返回 400，提示宽高不符**

服务端从图片内容解析宽高，与客户端预览尺寸无关。检查图片是否被旋转、裁剪，或实际上是 SVG 改名为 PNG。

**生成时返回 410**

参考图剩余有效期不足 30 分钟。重新上传即可。文件总有效期只有 6 小时，不适合长期保存素材。

**生成时返回 404**

file_id 不存在、不属于当前用户，或用途不是 `reference` / `result`。旧版本生成的结果 `user_id` 为 0，不能作为参考图复用。

**渲染进程直接上传时报 CORS 错误**

对象存储桶默认没有为客户端页面配置跨域规则。改为在主进程上传（见 4.2 节），不要通过关闭 Electron 的 `webSecurity` 绕过。

**同一张图重复放进 `image_ids`**

服务端按去重后的 file_id 计费，重复项只算一张。尽管如此，客户端仍应在提交前自行去重，避免超过 15 张上限。
