# 用户接口

全部端点需要 `Authorization: Bearer <JWT>`。各接口的文档如下。

## 1 端点索引

| 方法 | 路径 | 文档 |
|---|---|---|
| GET | `/api/user/info` | [当前用户信息](info.md) |
| PUT | `/api/user/profile` | [修改资料](profile.md) |
| PUT | `/api/user/password` | [重置密码](password.md) |
| GET | `/api/user/tokens` | [API Key 列表](tokens-list.md) |
| POST | `/api/user/tokens` | [新建 API Key](tokens-create.md) |
| PUT | `/api/user/tokens/:id` | [修改 API Key](tokens-update.md) |
| DELETE | `/api/user/tokens/:id` | [删除 API Key](tokens-delete.md) |
| GET | `/api/user/credit-logs` | [积分流水](credit-logs.md) |
| GET | `/api/user/task-logs` | [任务记录](task-logs.md) |
| GET | `/api/user/task-logs/:id` | [任务详情](task-log-detail.md) |

## 2 API Key 的可见性

完整 key 只在 `POST /api/user/tokens` 的响应里出现一次，并用 `data_key` 加密。请当场保存。列表接口只给前后各几位，因为库里存的是哈希。忘记保存只能删除重建。

## 3 积分精度

用户积分按 3 位小数展示与计算。管理端调整用户积分时，超过 3 位小数返回 400。详细口径见 [计费规则](../pricing/README.md#3-积分的小数位)。
