# 部署

同一台服务器上跑三个服务：**token-hub**、**new-api**、**sub2api**。
一个 Nginx 容器网关按域名分流，三个服务各自独立 compose、独立升级。

```
公网 ──80/443──> 网关(nginx) ──┬──> token-hub:3001  ──> postgres
                               ├──> new-api:3000    ──> 自带 db/redis
                               └──> sub2api:8080    ──> 自带 db/redis
```

三个服务都不发布宿主机端口；数据库同理。

> **为什么这么设计**、**出问题怎么办**、**上线自检清单**、**升级/回滚/备份** —— 都在
> [NOTES.md](NOTES.md)。平时不用看。

---

## 照这个顺序敲

```bash
# ===== 服务器：一次性准备 =====
uname -m                      # x86_64 -> linux/amd64   aarch64 -> linux/arm64
                              # 记下来，第 3 步要用
mkdir -p /opt/stacks
docker network create --subnet 172.20.0.0/16 gateway-proxy
# 防火墙 + 云安全组都只放行 22 / 80 / 443

# ===== 本机：传网关配置 =====
scp -r deploy/gateway <user>@<server>:/opt/stacks/

# ===== 服务器：起网关 =====
cd /opt/stacks/gateway
cp .env.example .env
vi .env                       # 填三个域名 + ACME_EMAIL
chmod +x scripts/*.sh
./scripts/self-signed.sh      # 域名还没解析，先放自签证书占位
docker compose up -d
docker compose exec gateway nginx -t          # 必须通过
curl -k -I -H "Host: token.example.com" https://127.0.0.1/health
#   现在返回 502 是正常的，token-hub 还没部署

# ===== 本机：构建镜像并上传 =====
./deploy/build-image.sh linux/amd64           # 或 linux/arm64
scp token-hub-latest-amd64.tar.gz <user>@<server>:/tmp/

# ===== 服务器：部署 token-hub =====
gunzip -c /tmp/token-hub-latest-amd64.tar.gz | docker load

# ===== 本机：传编排文件 =====
scp -r deploy/token-hub <user>@<server>:/opt/stacks/

# ===== 服务器：起 token-hub =====
cd /opt/stacks/token-hub
cp .env.example .env && chmod 600 .env
openssl rand -hex 32          # -> JWT_SECRET
openssl rand -hex 32          # -> SECRET_KEY
openssl rand -hex 24          # -> POSTGRES_PASSWORD
vi .env                       # 填上面三个，再设一个 INITIAL_ROOT_PASSWORD
docker compose up -d
docker compose logs -f token-hub

curl -k -H "Host: token.example.com" https://127.0.0.1/health          # {"status":"ok"}
curl -k -s -H "Host: token.example.com" https://127.0.0.1/ | head -3   # HTML 页面
curl -k -s -H "Host: token.example.com" https://127.0.0.1/api/nope     # JSON 404

# ===== 服务器：域名解析生效后换正式证书 =====
cd /opt/stacks/gateway
./scripts/certbot.sh issue

# 挂续期
crontab -e
17 3 * * * cd /opt/stacks/gateway && ./scripts/certbot.sh renew >> /var/log/certbot-renew.log 2>&1
```

登录后立即改 root 密码，并清空 `.env` 里的 `INITIAL_ROOT_PASSWORD`。

---

## 三件容易漏的事

**1. `TRUSTED_PROXIES` 必须验证一次。** 漏配会让登录限流把全体用户当成同一个人
——任意几次失败就锁死所有人，而且症状看起来像"密码错了"。
验证方法见 [NOTES.md](NOTES.md#trusted_proxies-怎么确认真的生效了)。

**2. `SECRET_KEY` 备份好，且永远不要改。** 它用来加密供应商 API Key，
改了之后数据库里已存的密钥就全部解不开，且无法恢复。
`.env` 要和数据库一起备份。

**3. 网关的 `nginx -t` 别跳过。** 那份模板有 230 行，语法错误会让**整个网关**起不来
（不是某个域名 502）。先自签证书再启动的原因也在这里：证书文件不存在时 nginx 直接拒绝启动。

---

## new-api 与 sub2api

用官方 compose 部署，只需**两处改动**：

1. **服务接进共享网络**（保留默认网络用来连它自己的库）：

   ```yaml
   services:
     <服务名>:
       networks:
         - default
         - proxy

   networks:
     proxy:
       external: true
       name: gateway-proxy
   ```

2. **删掉所有 `ports:`**。网关通过共享网络直连容器，发布到宿主机反而让服务能被绕过网关访问。

服务名必须是网关配置里写的那两个：**`new-api`**（端口 3000）、**`sub2api`**（端口 8080）。
对不上的话，改 `deploy/gateway/templates/default.conf.template` 里对应的 `set $xxx_up` 一行。

> ⚠️ 这两个服务都是**流式转发**。网关侧已经为它们配好了 `proxy_read_timeout 600s`
> 和 `proxy_buffering off` —— 漏了这两项，长回答会在中途被**静默截断**，
> 日志里看不出任何异常。

---

## 接下来

- **上线前逐项自检**：[NOTES.md](NOTES.md#上线自检清单)
- **升级 / 回滚 / 备份**：[NOTES.md](NOTES.md#升级)
- **出问题了**：[NOTES.md](NOTES.md#排查表)
