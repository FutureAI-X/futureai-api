# 当前用户信息

`GET /api/user/info`

返回当前登录用户的资料、积分余额与角色。请求需要 `Authorization: Bearer <JWT>`。

`role` 为 100 时表示管理员。
