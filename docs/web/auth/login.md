# 登录

`POST /api/auth/login`

请求体：

```json
{ "username": "root", "password": "..." }
```

首次启动且 `users` 表为空时，平台会自动创建 `root` 用户。初始密码的来源如下：

- 设置了 `INITIAL_ROOT_PASSWORD` 环境变量时，使用该值；
- 否则随机生成并写入 `./root_initial_password.txt`，权限为 `0600`。

该密码不会写入日志。登录后应立即通过 [修改密码](../user/password.md) 修改，并删除密码文件。容器部署建议直接使用环境变量，理由见 [ENV.md](../../dev/ENV.md#initial_root_password)。

响应：

```json
{
  "success": true,
  "message": "登录成功",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "data_key": "5Y2lzM8k...",
    "user": {
      "id": 1,
      "username": "root",
      "display_name": "Root User",
      "role": 100,
      "status": 1,
      "email": "",
      "credits": 100000000,
      "used_credits": 0
    }
  }
}
```

`token` 与 `data_key` 的用途见 [登录与认证](README.md)。
