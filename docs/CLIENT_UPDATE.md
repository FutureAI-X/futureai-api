# Electron 客户端更新检查接入指南

本文说明 Electron 客户端如何接入服务端的版本检查接口。适用范围：`futureai-image`、`futureai-video` 及后续新增的桌面应用。

服务端接口定义见 [API.md](API.md#25-客户端更新检查)。

## 1 发布侧准备

1. 登录管理后台，进入 **版本管理 → 应用管理**，登记应用标识。
   - 标识如 `futureai-image`，只能包含小写字母、数字和连字符。
   - **创建后不可修改**：客户端代码里写死的是它，改了等于让所有已安装客户端查不到更新。
2. 进入 **版本管理 → 版本发布**，选择应用与平台，填写版本号、更新地址、更新说明。
   - 版本号必须为严格三段 `x.y.z`，同一应用、同一平台下不能重复。
   - 更新地址可以是网盘链接，也可以是文档页面；客户端只负责用系统浏览器打开。
3. 确认该版本处于「已上架」。公开接口取的是已上架的最高版本，下架最新版会自动回退到上一版。

发布后可用下面的命令验证（把应用标识和版本换成实际值）：

```bash
curl "https://futureaiapi.com/api/apps/futureai-image/version?platform=windows&current_version=1.0.0"
```

## 2 客户端版本号来源

客户端上报的版本号必须与安装包版本一致。Electron 中直接使用：

```ts
import { app } from 'electron'

app.getVersion() // 读取 package.json 的 version
```

因此 `package.json` 的 `version` 必须写成严格三段，例如 `1.3.0`。以下写法都会被服务端拒绝（返回 400）：

- `v1.3.0`
- `1.3`
- `1.3.0-beta.1`
- `1.03.0`

打包时 `electron-builder` 也使用这个字段，保持它与发布记录一致即可。

## 3 启动时检查

建议在主进程发起检查：主进程没有渲染进程的跨域限制，也不受页面加载失败影响。

```ts
import { app, dialog, shell } from 'electron'

const API_BASE = 'https://futureaiapi.com'
const APP_KEY = 'futureai-image' // 与后台登记的应用标识完全一致

interface UpdateInfo {
  latest_version: string
  has_update: boolean
  update_url?: string
  release_notes?: string
}

async function checkForUpdates() {
  // 平台映射：服务端只认 windows / macos
  const platform = process.platform === 'darwin'
    ? 'macos'
    : process.platform === 'win32'
      ? 'windows'
      : null
  if (!platform) return

  const url = new URL(`${API_BASE}/api/apps/${APP_KEY}/version`)
  url.searchParams.set('platform', platform)
  url.searchParams.set('current_version', app.getVersion())

  let info: UpdateInfo
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(5000) })
    if (!res.ok) return
    const body = await res.json()
    if (!body?.success) return
    info = body.data
  } catch {
    // 网络失败、超时、服务端异常一律静默：
    // 更新检查是附加能力，不能影响应用启动
    return
  }

  if (!info.has_update || !info.update_url) return

  const { response } = await dialog.showMessageBox({
    type: 'info',
    title: `发现新版本 ${info.latest_version}`,
    message: `当前版本 ${app.getVersion()}，最新版本 ${info.latest_version}`,
    detail: info.release_notes || '',
    buttons: ['去更新', '稍后'],
    defaultId: 0,
    cancelId: 1,
  })

  if (response === 0) {
    await shell.openExternal(info.update_url)
  }
}

app.whenReady().then(() => {
  // 不 await：检查慢或失败都不能阻塞主窗口
  void checkForUpdates()
  createWindow()
})
```

## 4 注意事项

- **不要阻塞启动**：检查放在 `app.whenReady()` 之后异步执行；超时设 5 秒左右，任何异常都静默。
- **不要自行比较版本号**：`has_update` 由服务端计算，客户端只根据该字段弹窗。
- **`update_url` 仅在 `has_update` 为 `true` 时下发**：没有更新时不要展示下载入口。
- **更新地址用系统浏览器打开**：`shell.openExternal()`，不要在 Electron 窗口内加载。
- **禁用应用等于停止更新提示**：管理后台禁用应用后，接口返回 404，客户端按「无更新」处理即可。
- **同一应用多平台独立发布**：Windows 与 macOS 的版本号互不影响，客户端只查自己所在的平台。
- **接口按 IP 限流**（默认 120 次/分钟）：每个客户端启动查一次，正常量级远低于阈值；
  不要在页面内轮询。

## 5 常见问题

| 现象 | 原因 |
|---|---|
| 接口 404 | 应用标识不存在、写错，或应用被禁用 |
| 接口 400 | `platform` 不是 `windows`/`macos`，或 `current_version` 不是严格三段 |
| `has_update` 一直为 `false` | 该平台没有已上架版本，或发布版本不高于客户端当前版本 |
| 有更新但 `update_url` 为空 | 检查发布记录的更新地址是否填写完整（必须 http/https） |
