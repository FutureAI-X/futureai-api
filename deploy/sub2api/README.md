# 把 sub2api 接进网关

sub2api 用官方镜像和官方 compose 部署即可，这里只说明**两处必须改动**，
以及网关侧已经替你处理掉的坑。

> 本目录不提供 compose 文件：sub2api 的官方 compose 带 PostgreSQL 与 Redis，
> 环境变量（`DATABASE_URL`、`REDIS_URL`、`JWT_SECRET`、`TOTP_ENCRYPTION_KEY` 等）
> 请以官方 `deploy/` 目录下的 `.env.example` 为准。

## 1. 部署位置与网络

```
/opt/stacks/sub2api/
├── docker-compose.yml     # 官方那份，改两处
└── .env
```

在官方 compose 里加上共享网络，并让 **sub2api 服务本身**接进去：

```yaml
services:
  sub2api:
    # ... 官方原有内容保持不变 ...
    networks:
      - default    # 保留默认网络，用它连自己的 PostgreSQL 与 Redis
      - proxy      # 新增：接进网关所在的共享网络

  # 它自带的 db / redis 服务保持不动，不要接进 proxy 网络

networks:
  proxy:
    external: true
    name: gateway-proxy
```

**服务名必须是 `sub2api`**（官方 compose 里的默认名）。网关配置写的是
`sub2api:8080`，靠 Docker 内置 DNS 解析；改了服务名就要同步改
`deploy/gateway/templates/default.conf.template` 里的 `set $sub2api_up`。

**不要加 `ports:`**，也不要把它的 `db` / `redis` 的端口发布出来。
官方 compose 里如果原本有 `ports:`，直接删掉。

## 2. 网关侧

`deploy/gateway/templates/default.conf.template` 里已经有 sub2api 的 server 块，
默认按端口 `8080` 写。域名在 `deploy/gateway/.env` 的 `SUB2API_DOMAIN`。

## 已经替你处理掉的坑：下划线请求头

搭配 Codex CLI 使用时，请求头里会带下划线（如 `session_id`）。
nginx 默认**静默丢弃**这类请求头 —— 不报错，只是下游认证莫名其妙地失败。

网关配置的 http 段里已经加了一行：

```nginx
underscores_in_headers on;
```

如果你后面在别处再加一层 nginx，记得也带上这一行。

## 3. 流式响应

sub2api 转发的是订阅制模型的长回答，同样需要长超时与流式透传。
网关侧已按 new-api 的同样参数配置：

```nginx
proxy_read_timeout 600s;
proxy_send_timeout 600s;
proxy_buffering off;
```

## 4. 首次访问

浏览器打开 `https://<SUB2API_DOMAIN>/` 会进入 Setup Wizard 创建管理员账号。
如果密码是自动生成的，可以在日志里找：

```bash
docker compose logs sub2api | grep -i password
```

## 5. 验证

```bash
curl -I https://<SUB2API_DOMAIN>/          # 应返回 200/302，不是 502
```

排查顺序同 new-api：`docker compose ps` → `docker network inspect gateway-proxy`
→ `docker compose exec gateway getent hosts sub2api`。
