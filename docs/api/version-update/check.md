# 查询版本更新

`GET /api/apps/:app_key/version?platform=windows&current_version=1.2.3`

无需认证。请求参数如下。

| 参数 | 必填 | 说明 |
|---|---|---|
| `app_key` | 是 | 应用标识，由管理后台「应用管理」登记，如 `futureai-image` |
| `platform` | 是 | `windows` 或 `macos` |
| `current_version` | 是 | 客户端当前版本，严格三段 semver，如 `1.2.3` |

服务端取该应用、该平台下已上架的最高版本，与 `current_version` 比较，响应如下。

```json
{
  "success": true,
  "data": {
    "app_key": "futureai-image",
    "platform": "windows",
    "latest_version": "1.3.0",
    "has_update": true,
    "update_url": "https://pan.example.com/...",
    "release_notes": "修复了...",
    "published_at": "2026-11-02 10:00:00"
  }
}
```

- `has_update` 由服务端计算，客户端不需要自己比较版本号。
- `update_url` 与 `release_notes` 只在 `has_update` 为 `true` 时下发。
- 该应用在该平台下没有任何已上架版本时，返回 `latest_version: ""`、`has_update: false`。
- 应用不存在或被禁用返回 `404`；`platform` 非法或 `current_version` 格式非法返回 `400`。
- 限流按来源 IP 统计，速率由 `APP_VERSION_RATE_LIMIT_PER_MINUTE` 配置，默认 120 次/分钟。
