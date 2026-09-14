# 把 new-api 接进网关

new-api 用官方镜像和官方 compose 部署即可，这里只说明**两处必须改动**，
以及一个网关侧已经替你处理掉的坑。

> 本目录不提供 compose 文件：new-api 的官方 compose 里有一批它自己的环境变量
> （数据库类型、Redis、会话密钥等），照抄一份很容易过期或抄错。
> 请以官方仓库里的 compose 为准，在其基础上做下面的改动。

## 1. 部署位置与网络

```
/opt/stacks/new-api/
├── docker-compose.yml     # 官方那份，改两处
└── .env
```

在官方 compose 里加上共享网络，并让服务接进去：

```yaml
services:
  new-api:
    # ... 官方原有内容保持不变 ...
    networks:
      - default    # 保留默认网络，用它连自己的数据库/Redis
      - proxy      # 新增：接进网关所在的共享网络

networks:
  proxy:
    external: true
    name: gateway-proxy
```

**服务名必须是 `new-api`**。网关配置里写的是 `new-api:3000`，
靠 Docker 内置 DNS 解析服务名 —— 改了服务名就要同步改
`deploy/gateway/templates/default.conf.template` 里的 `set $new_api_up`。

**不要加 `ports:`**。网关通过共享网络直连容器，不需要把端口发布到宿主机，
发布了反而会让这个服务可以被绕过网关直接访问。

## 2. 网关侧

`deploy/gateway/templates/default.conf.template` 里已经有 new-api 的 server 块，
默认按端口 `3000` 写。如果你的部署改了端口，改那一行 `set $new_api_up` 即可。

域名在 `deploy/gateway/.env` 的 `NEW_API_DOMAIN`。

## 已经替你处理掉的坑：流式响应被截断

new-api 的中转接口是流式的，一次补全可能持续好几分钟。nginx 默认的
`proxy_read_timeout` 是 60 秒，会把长回答从中间切断 ——
而且日志里看不出是超时，因为连接是正常断开的，现象只是"回答到一半卡住"。

网关配置里已经为 new-api 设了：

```nginx
proxy_read_timeout 600s;
proxy_send_timeout 600s;
proxy_buffering off;      # 让 token 逐个流到浏览器，而不是攒满缓冲区才下发
```

如果你后面自己在别处加了 nginx 层，记得把这三行一起带过去。

## 3. 客户端 IP（需要你确认一下）

Token Hub 用 `TRUSTED_PROXIES` 来从 `X-Forwarded-For` 还原真实客户端 IP。
**new-api 是否有等价的配置项，请查它的官方文档确认** —— 如果有，
应填网关容器的固定地址 `172.20.0.2`（不是 `0.0.0.0/0`，那等于信任一切）。

如果它默认采信 `X-Forwarded-For`，那么在网关后面部署是安全的（网关会追加真实 IP）；
但如果这个服务哪天被绕过网关直接访问，XFF 就可以被伪造。

## 4. 验证

```bash
curl -I https://<NEW_API_DOMAIN>/          # 应返回 200/302，不是 502
```

502 通常是三种原因：服务名不对、没接进 `gateway-proxy` 网络、或者服务本身没起来。
排查顺序：`docker compose ps` → `docker network inspect gateway-proxy`
→ `docker compose exec gateway getent hosts new-api`。
