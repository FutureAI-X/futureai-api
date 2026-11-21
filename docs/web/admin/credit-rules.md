# 计费规则管理

图像模型使用 `/api/admin/models/:id/credit-rule、/api/admin/credit-rules/:id`。

视频模型使用独立的规则表，接口为：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/admin/models/:id/video-credit-rule` | 读取视频计费规则与分辨率价目（不限状态） |
| PUT | `/api/admin/models/:id/video-credit-rule` | 全量保存规则与价目；仅允许 `type=video` 的模型 |

视频规则包含每张参考图积分、每请求图片免费张数、每个参考音频积分，以及按分辨率分档的参考视频/输出每秒费率。保存时全量替换 `rates`，校验规则见 [视频计费设计](../../design/video/billing.md)。

完整的方法、参数与响应以 `router/router.go` 中的路由注册为准。
