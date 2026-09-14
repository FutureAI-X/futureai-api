# 部署指南

同一台服务器上并行部署三个服务：**token-hub**、**new-api**、**sub2api**。
三个都是「一个容器里同时装着前端和后端」的形态，各自独立升级、互不影响。

## 全貌

```
                          ┌── token.example.com ──┐
   公网 ──80/443──> 网关 ─┼── newapi.example.com ─┼─> 按域名分流
                  (nginx) └── sub2api.example.com ┘
                     │
                     │  共享网络 gateway-proxy (172.20.0.0/16)，网关固定 172.20.0.2
                     │
     ┌───────────────┴───────────────┐
     │               │               │
 token-hub:3001  new-api:3000  sub2api:8080
     │               │               │
     ▼               ▼               ▼
  postgres       自带 db/redis   自带 db/redis
（各自独立，且都不在共享网络上）
```

三条硬性约束，整套方案就是围绕它们设计的：

1. **80/443 全机器只有一个进程能占** —— 只有网关容器发布这两个端口。
2. **数据库端口一个都不发布** —— 容器间用服务名互访。这同时也消除了
   「PostgreSQL 直接暴露到公网、密码哈希与供应商密钥密文可被直读」的风险。
3. **每个服务必须知道网关是可信的** —— 否则从 `X-Forwarded-For` 还原不出真实
   客户端 IP，登录限流会把全体用户当成同一个人。

## 服务器准备（一次性）

```bash
# 1. 确认 CPU 架构 —— 决定本机构建镜像时传哪个参数
uname -m          # x86_64 -> linux/amd64   aarch64 -> linux/arm64

# 2. 装 Docker 与 compose 插件（略，见官方文档）

# 3. 建共享网络。刻意手工建，不让任何 compose 项目管它：
#    由 compose 管理的网络，任何一个项目 down 都可能把它一起删掉，
#    导致其余服务瞬间失去网络。
docker network create --subnet 172.20.0.0/16 gateway-proxy

# 4. 目录
mkdir -p /opt/stacks

# 5. 防火墙：只放行 22 / 80 / 443
#    云服务器还要在安全组里放行 80 和 443，两处都要做。
```

## 第 1 步：网关

在**本机**（Windows 开发机）把网关配置传上去：

```bash
scp -r deploy/gateway <user>@<server>:/opt/stacks/
```

然后在**服务器**上：

```bash
cd /opt/stacks/gateway
cp .env.example .env
vi .env                       # 填三个域名和 ACME_EMAIL

chmod +x scripts/*.sh
./scripts/self-signed.sh      # 先生成自签证书占位

docker compose up -d
docker compose exec gateway nginx -t      # 配置语法自检，必须通过
```

> **为什么先要自签证书**：nginx 的 `ssl_certificate` 指向的文件不存在时，
> 进程会**直接启动失败**（不是某个域名 502，是整个网关起不来）。
> 正式证书签发之前必须先有东西占位。

验证：

```bash
curl -k -I -H "Host: token.example.com" https://127.0.0.1/health
# 现在应该返回 502 —— 因为 token-hub 还没部署，这是正常的
```

自签证书阶段浏览器会报警告，是预期的。用 `curl -k` 跳过即可。

## 第 2 步：构建 token-hub 镜像并上传

在**本机**：

```bash
# x86 服务器
./deploy/build-image.sh linux/amd64
# arm 服务器
./deploy/build-image.sh linux/arm64
```

> **架构参数在哪改？** 它是命令行参数，**没有任何配置文件需要动**。
> 脚本里默认 `linux/amd64`，把 `linux/arm64` 当第一个参数传进去就覆盖了。
> 等价写法：`PLATFORM=linux/arm64 make docker`，或 `make docker PLATFORM=linux/arm64`。
>
> 这个参数只影响 token-hub —— new-api 与 sub2api 用的是官方多架构镜像，
> 在服务器上 `docker compose pull` 时会自动拉取匹配的版本。
>
> 改错了的症状：容器起不来，日志里是 `exec format error`，
> 或者 `docker compose up` 直接报 `no matching manifest for linux/arm64`。
>
> 确认手上镜像的架构：
> ```bash
> docker image inspect token-hub:latest --format '{{.Architecture}}'
> ```

脚本会构建镜像并导出成 `token-hub-latest-<arch>.tar.gz`。
构建在 Docker 里完成，前端（Node）与后端（Go）都在多阶段构建中编译，
**交叉编译不需要 QEMU**，所以在 x86 开发机上打 arm64 镜像也很快。

```bash
scp token-hub-latest-amd64.tar.gz <user>@<server>:/tmp/
```

在**服务器**上：

```bash
gunzip -c /tmp/token-hub-latest-amd64.tar.gz | docker load
docker images | grep token-hub     # 确认镜像已就位
```

这条路线的好处：服务器上**不需要源码、不需要 git 凭据、不需要装任何工具链**，
镜像本身就是交付物。

## 第 3 步：token-hub

```bash
scp -r deploy/token-hub <user>@<server>:/opt/stacks/
```

在**服务器**上：

```bash
cd /opt/stacks/token-hub
cp .env.example .env
chmod 600 .env

# 生成两个密钥，填进 .env
openssl rand -hex 32      # -> JWT_SECRET
openssl rand -hex 32      # -> SECRET_KEY
openssl rand -hex 24      # -> POSTGRES_PASSWORD

vi .env
docker compose up -d
docker compose logs -f token-hub
```

首次启动且 `users` 表为空时会自动创建 `root` 用户，
密码取 `.env` 里的 `INITIAL_ROOT_PASSWORD`。
**登录后立即改密**，并把 `.env` 里那一行清掉。

验证：

```bash
curl -k -H "Host: token.example.com" https://127.0.0.1/health
# {"status":"ok"}

# 首页应当返回 HTML
curl -k -s -H "Host: token.example.com" https://127.0.0.1/ | head -3

# API 路径下不存在的地址应当是 JSON 404，不是 HTML
curl -k -s -H "Host: token.example.com" https://127.0.0.1/api/nope
```

### 确认 TRUSTED_PROXIES 真的生效了

这是最容易漏、后果最严重的一项。从外部 IP 连续登录失败几次，看应用日志里的来源 IP：

```bash
docker compose logs token-hub | grep -i login
```

- 看到的是**你自己的公网 IP** → 配置正确
- 看到的是 `172.20.0.2` → `TRUSTED_PROXIES` 没生效，全部用户会被限流当成同一个人，
  任意几次失败就能锁死所有人。检查 `.env` 的 `GATEWAY_IP` 与
  `deploy/gateway/docker-compose.yml` 里的 `ipv4_address` 是否一致。

## 第 4 步：new-api 与 sub2api

这两个用官方 compose，各自只需接进共享网络。详见：

- [new-api/README.md](new-api/README.md)
- [sub2api/README.md](sub2api/README.md)

两个服务都是流式转发，网关侧已经为它们配好了长超时和 `proxy_buffering off` ——
这两项漏了的话，长回答会在中途被静默截断。

## 第 5 步：域名与证书

三个域名的 A 记录都指向服务器 IP，解析生效后：

```bash
cd /opt/stacks/gateway
./scripts/certbot.sh issue
```

它会给三个域名签**一张 SAN 证书**（三个 server 块共用同一对文件，
续期只需跑一次）。

续期挂 cron：

```bash
crontab -e
# 每天凌晨 3:17 检查一次。certbot 只在临近到期时才真正续期。
17 3 * * * cd /opt/stacks/gateway && ./scripts/certbot.sh renew >> /var/log/certbot-renew.log 2>&1
```

## 上线自检

```bash
# 1. HSTS 已下发
curl -sI https://token.example.com/api/pricing | grep -i strict-transport

# 2. 前端资源长缓存、index.html 不缓存
curl -sI https://token.example.com/ | grep -i cache-control          # no-store
curl -sI https://token.example.com/assets/<某个文件名> | grep -i cache-control
#    -> public, max-age=31536000, immutable

# 3. 响应里不应出现两个 Cache-Control 头（构建产物由 Go 提供，正常只有一个）

# 4. 凭证不接受走 URL（应返回 401）
curl -s -o /dev/null -w '%{http_code}\n' \
     "https://token.example.com/v1/models?token=sk-anything"

# 5. 上传 10MB 文件不应出现 413
curl -s -o /dev/null -w '%{http_code}\n' \
     -H "Authorization: Bearer sk-xxx" \
     -F "file=@10mb.png" https://token.example.com/v1/uploads/images

# 6. 从公网直连应用端口应当连不上（端口根本没发布）
curl -m 3 http://<服务器IP>:3001/health     # 应超时

# 7. 数据库端口同样不可达
curl -m 3 telnet://<服务器IP>:5432          # 应超时
```

## 日常运维

### 升级 token-hub

```bash
# 本机
./deploy/build-image.sh linux/amd64
scp token-hub-latest-amd64.tar.gz <user>@<server>:/tmp/

# 服务器
gunzip -c /tmp/token-hub-latest-amd64.tar.gz | docker load
cd /opt/stacks/token-hub && docker compose up -d
```

应用启动时会自动跑 `AutoMigrate`，不需要单独的迁移步骤。
数据库结构变更前建议先备份（见下）。

**不需要重启网关**：网关用 `resolver` + 变量解析上游，容器重建换了 IP 会在
10 秒内自动生效。这是刻意这么设计的 —— 否则每次升级后都会 502 到手动重启网关为止。

### 回滚

镜像按标签保留，回滚就是换标签：

```bash
cd /opt/stacks/token-hub
sed -i 's/^TOKEN_HUB_TAG=.*/TOKEN_HUB_TAG=<上一个版本>/' .env
docker compose up -d
```

所以打镜像时建议带上版本号而不是一律 `latest`：

```bash
TAG=v1.2.3 ./deploy/build-image.sh linux/amd64
```

### 备份

```bash
# 数据库
docker compose -f /opt/stacks/token-hub/docker-compose.yml exec -T postgres \
  pg_dump -U token_hub token_hub | gzip > /root/backup/token-hub-$(date +%F).sql.gz
```

挂 cron，并定期**实际恢复一次**验证备份可用 —— 没验证过的备份等于没有备份。

`.env` 也要备份：`SECRET_KEY` 丢了，数据库里的供应商 API Key 就永远解不开了。

### 日志

```bash
cd /opt/stacks/gateway
tail -f logs/token-hub.access.log      # 网关侧访问日志（已剔除查询串）
docker compose -f /opt/stacks/token-hub/docker-compose.yml logs -f token-hub
```

## 一句话排查表

| 现象 | 多半是 |
|---|---|
| 某个域名 502，另两个正常 | 那个服务没起来 / 服务名对不上 / 没接进 `gateway-proxy` 网络 |
| 全部域名 502 | 网关没起来，`docker compose exec gateway nginx -t` 看配置 |
| 网关容器起不来 | 证书文件缺失，跑一次 `./scripts/self-signed.sh` |
| 登录失败几次后所有人都登不进 | `TRUSTED_PROXIES` 没配，所有人被当成同一来源 |
| 上传 10MB 图片返回 413 | 网关 `client_max_body_size` 没生效（检查 `nginx -t` 是否加载了本目录的模板） |
| 长回答到一半卡住 | 该服务的 `proxy_read_timeout` 太短 / `proxy_buffering` 没关 |
| 页面能开但样式字体不对 | CSP 拦掉了字体 CDN，见 `webui/webui.go` 的 `contentSecurityPolicy` |
