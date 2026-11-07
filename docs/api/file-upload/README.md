# 文件上传与读取

本文说明文件上传与读取的通用流程、限制和客户端接入方式。各接口的参数与响应见第 6 章列出的接口文档。

## 1 新旧上传方式

旧方式把文件传到平台服务端，由平台转存后返回公开 URL，客户端再把 URL 填进 `image_urls`。新方式分三步：申请凭证、客户端直传对象存储、调用完成接口。完成后返回 `file_id`，填进图像生成请求的 `image_ids`。

| 项目 | 旧方式 | 新方式 |
|---|---|---|
| 上传内容传输 | 先传到平台，再由平台转存 | 文件直接传到对象存储 |
| 返回标识 | URL | `file_id` |
| 生成侧字段 | `image_urls` | `image_ids` |
| 文件可见性 | 公开可读 | 私有，仅上传者可用 |
| 大小上限 | 10MB | 图片 30MB，音频 15MB，视频 200MB |
| 有效期 | 按结果文件策略，默认 24 小时 | 完成上传后 6 小时 |

新方式把参考图上限从 10MB 提高到 30MB，文件不再经过平台服务端转发，也保持私有。新项目应直接使用 `image_ids`。

## 2 参考媒体上传流程

完整流程有五个步骤，任何一步失败都不要跳到下一步。

1. 读取本地文件，取得字节数和 MIME 类型。
2. 调用 `POST /v1/files/upload/prepare`，取得上传地址 `url`、表单字段 `fields`、`upload_id`。
3. 以 `multipart/form-data` 把 `fields` 和文件 POST 到 `url`。
4. 调用 `POST /v1/files/upload/complete`，提交 `upload_id`。
5. 把响应中的 `file_id` 填进图像生成请求的 `image_ids`，或视频生成请求的 `images` / `videos` / `audios`。

上传凭证有效期 30 分钟，服务端上传会话有效期 40 分钟。超过 40 分钟仍未调用 `complete`，必须重新 `prepare`。

## 3 直传对象存储

这一步不经过平台服务端，客户端把表单直接 POST 到 `prepare` 返回的 `url`。

### 3.1 构造表单

按以下顺序构造 `multipart/form-data`：

1. 依次追加 `fields` 中的全部键值，名称与值都不能改动。
2. 最后追加一个名为 `file` 的字段，内容为文件字节，文件名用原始文件名。

> **注意**：不要手动设置请求的 `Content-Type`。浏览器或 `fetch` 需要自动生成带 boundary 的 `multipart/form-data` 头；手动设置会导致服务端无法解析表单。

`fields` 中的 `key` 决定对象路径，`policy`、`q-*` 是签名信息，任一项缺失或被修改都会得到 403。

### 3.2 Electron 主进程上传

建议在主进程完成上传。主进程不受渲染进程的跨域限制，也不需要给对象存储桶配置 CORS。使用 Electron 的 `net.fetch` 而不是 Node 原生的 `fetch`，前者会遵循系统代理设置。

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

### 3.3 文件选择

主进程使用 `dialog.showOpenDialog` 获取路径。文件过滤器应只放行业务支持的格式：

```ts
const result = await dialog.showOpenDialog({
  properties: ['openFile'],
  filters: [{ name: '图片', extensions: ['jpg', 'jpeg', 'png', 'webp'] }],
})
```

如果客户端必须支持 HEIC/HEIF，先转换为 JPG/PNG/WEBP 再上传：上传接口接受 HEIC/HEIF，但图像生成接口当前会拒绝这两种格式，见第 4.3 节。

### 3.4 直传失败的处理

直传可能遇到以下失败，处理方式如下。

| 现象 | 原因 | 处理 |
|---|---|---|
| 403 | `fields` 被修改、签名过期、`key` 不匹配 | 重新 `prepare` |
| 400 | 表单缺少字段，或 `file` 字段不在最后 | 检查表单构造 |
| 网络错误 | 代理、断网、对象存储不可达 | 重试同一次直传，凭证在 30 分钟内仍然有效 |

直传成功后不要再次上传同一凭证。对象存储返回 200 即表示对象已写入，响应体可能为空。

## 4 限制与有效期

### 4.1 格式与大小

参考媒体的格式与大小限制如下。

| 类型 | 格式 | 编码 | 大小 | 时长 | 宽高 | 宽高比 | 帧率 |
|---|---|---|---|---|---|---|---|
| 图片 | JPG/JPEG、PNG、WEBP、HEIC、HEIF | — | ≤30MB | — | 不限 | 不限 | — |
| 音频 | WAV、MP3 | — | ≤15MB | 2–30 秒 | — | — | — |
| 视频 | MP4、MOV | 视频 H.264/AVC、H.265/HEVC；音频 AAC、MP3 | ≤200MB | 2–30 秒 | 每边 300–5500 | 0.4–2.5 | 23.9–60 |

- 静音视频允许；存在音频流时必须是 AAC/MP3。
- 新上传不接受 GIF、AVIF、独立 M4A/AAC 音频。
- 服务端在 `complete` 阶段按文件头识别真实格式，并以识别结果覆盖记录中的 `content_type`。文件名后缀不参与判断。
- 图片宽高不受平台限制。服务端仍会解析宽高并记录在案，解析失败时上传会失败。

### 4.2 配额与限流

每个用户的参考媒体总配额为 10GB，按未过期文件之和计算。配额不足时 `prepare` 返回 507。

`prepare` 与 `complete` 都属于 `/v1` 端点，按用户限流。默认每分钟 60 次、突发 10 次，超限返回 429，响应头带 `Retry-After`。

### 4.3 有效期

各阶段的有效期如下。

| 项目 | 时长 | 说明 |
|---|---|---|
| 上传凭证 | 30 分钟 | `prepare` 响应的 `expires_at` |
| 上传会话 | 40 分钟 | 必须在此之前调用 `complete` |
| 参考文件 | 6 小时 | 从 `complete` 成功起算 |
| 可用于生成 | 剩余 ≥ 30 分钟 | 低于该值返回 410 |

参考文件是私有文件，不能通过 `GET /v1/images/get/:file_id` 读取。服务端在生成时把它转换成短期签名地址交给上游，客户端不需要也不应该自行拼接访问地址。

HEIC/HEIF 可以在上传阶段完成，但当前不能在图像生成中使用，`image_ids` 会返回 400。需要支持这两种格式时，客户端应先转换为 JPG/PNG/WEBP。

## 5 错误处理

`prepare` 与 `complete` 的业务错误响应形如：

```json
{ "code": "fail", "message": "文件大小超过上限（30 MB）" }
```

认证失败与限流使用 OpenAI 风格的错误结构，客户端应同时兼容两种：

```json
{ "error": { "message": "无效的 API Key", "type": "authentication_error" } }
```

各接口的常见状态码见对应接口文档。

## 6 接口索引

| 接口 | 文档 |
|---|---|
| `POST /v1/uploads/images` | [公开图片上传](public-image-upload.md) |
| `POST /v1/files/upload/prepare` | [申请上传凭证](reference-media-prepare.md) |
| `POST /v1/files/upload/complete` | [完成上传](reference-media-complete.md) |
| `GET /v1/images/get/:file_id` | [公开文件读取](public-file-read.md) |

## 7 常见问题

**上传成功但 `complete` 返回 409**

对象存储写入有短暂延迟，或直传请求实际失败但客户端没有检查响应。先确认直传返回 200，再间隔 1–2 秒重试 `complete`。

**`complete` 返回 400，提示无法解析图片宽高**

服务端需要从图片内容解析出宽高并记录。提示该错误通常说明文件损坏、内容不是完整图片，或实际格式与后缀不符，例如 SVG 改名为 PNG。重新导出为 JPG/PNG/WEBP 后再上传。

**生成时返回 410**

参考图剩余有效期不足 30 分钟。重新上传即可。文件总有效期只有 6 小时，不适合长期保存素材。

**生成时返回 404**

file_id 不存在、不属于当前用户，或用途不是 `reference` / `result`。旧版本生成的结果 `user_id` 为 0，不能作为参考图复用。

**渲染进程直接上传时报 CORS 错误**

对象存储桶默认没有为客户端页面配置跨域规则。改为在主进程上传，见第 3.2 节。不要通过关闭 Electron 的 `webSecurity` 绕过。

**同一张图重复放进 `image_ids`**

服务端按去重后的 file_id 计费，重复项只算一张。尽管如此，客户端仍应在提交前自行去重，避免超过 15 张上限。
