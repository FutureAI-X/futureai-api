# 管理员接口

全部端点需要 `Authorization: Bearer <JWT>`，且 `role = 100`。

## 1 分组索引

管理端按以下分组组织，各组文档如下。

| 分组 | 路径前缀 | 文档 |
|---|---|---|
| 用户 | `/api/admin/users` | [用户管理](users.md) |
| 供应商 | `/api/admin/vendors` | [供应商管理](vendors.md) |
| 供应商模型 | `/api/admin/vendor-models` | [供应商模型管理](vendor-models.md) |
| 上游端点 | `/api/admin/endpoints` | [上游端点管理](endpoints.md) |
| 模型 | `/api/admin/models` | [模型管理](models.md) |
| 模型展示顺序 | `/api/admin/model-order` | [模型管理](models.md#2-保存模型顺序) |
| 模型与端点绑定 | `/api/admin/models/:id/endpoints` | [模型与端点绑定](model-endpoints.md) |
| 计费规则 | `/api/admin/models/:id/credit-rule` | [计费规则管理](credit-rules.md) |
| 积分流水 | `/api/admin/credit-logs` | [积分流水](credit-logs.md) |
| 全员任务记录 | `/api/admin/task-logs` | [全员任务记录](task-logs.md) |
| 客户端应用 | `/api/admin/apps` | [客户端应用管理](apps.md) |
| 客户端版本 | `/api/admin/app-releases` | [客户端版本管理](app-releases.md) |

## 2 完整端点列表

完整的方法、参数与响应以路由注册为准：

`router/router.go`

该文件是唯一的事实来源。各分组文档只说明路径前缀，不重复维护每个端点的方法与参数，避免重构后文档失效。
