# 部署笔记

[README.md](README.md) 只列了照着敲的命令。这里放**为什么这么设计**、以及出问题时的排查资料。
平时不用看。

---

## 为什么是这套结构

同一台机器上三个服务并存，有三条硬性约束，整套方案就是围绕它们设计的：

1. **80/443 全机器只有一个进程能占** —— 所以只有网关容器发布这两个端口，
   三个服务各自只在内网监听。
2. **数据库端口一个都不发布** —— 容器间用服务名互访。这同时消除了
   「PostgreSQL 暴露到公网、密码哈希与供应商密钥密文被直读」的风险。
3. **每个服务必须知道网关是可信的** —— 否则从 `X-Forwarded-For` 还原不出真实
   客户端 IP，登录限流会把全体用户当成同一个人。

共享网络 `gateway-proxy`（172.20.0.0/16）是手工建的，没有交给任何 compose 项目管：
由 compose 管理的网络，任何一个项目 `down` 都可能把它一起删掉，导致其余服务瞬间失去网络。

---

## 为什么上游用 resolver + 变量，而不是 upstream 块

网关配置里写的是：

```nginx
resolver 127.0.0.11 valid=10s ipv6=off;
set $token_hub_up "token-hub:3001";
proxy_pass http://$token_hub_up;
```

而不是常见的：

```nginx
upstream backend { server token-hub:3001; }
```

**原因**：nginx 对 `upstream` 块里的主机名**只在启动时解析一次**。容器重建换了 IP 之后，
网关会一直把请求打到旧地址，表现为持续 502，直到手动重启 nginx。
而这三个服务都会各自独立升级重建，踩中的概率很高。

`resolver` 指向 Docker 内置 DNS，`valid=10s` 让 IP 变化在 10 秒内生效。
代价是失去了 upstream keepalive，每个请求要新建一条到同机容器的连接 ——
在同一个 docker 网络里这个开销可以忽略，换来的是**升级后不需要碰网关**。

---

## 为什么必须先有自签证书

nginx 的 `ssl_certificate` 指向的文件不存在时，**进程会直接启动失败** ——
不是某个域名 502，而是整个网关起不来。

所以在正式证书签发之前，必须先跑一次 `scripts/self-signed.sh` 放一对证书占位。
域名解析生效后再换成正式证书。

浏览器在自签阶段会报警告，这是预期的，用 `curl -k` 绕过。

---

## TRUSTED_PROXIES 怎么确认真的生效了

这是最容易漏、后果最严重的一项。**部署完必须验证一次。**

漏配的症状：应用把所有请求的来源都看成网关 IP `172.20.0.2`，
登录限流会把全体用户当成同一来源 —— 任意几次失败就能锁死所有人。

```bash
# 从外部 IP 连续登录失败几次
docker compose -f /opt/stacks/token-hub/docker-compose.yml logs token-hub | grep -i login
```

- 看到的是**你自己的公网 IP** → 正确
- 看到的是 `172.20.0.2` → 没生效。检查 `deploy/token-hub/.env` 的 `GATEWAY_IP`
  与 `deploy/gateway/docker-compose.yml` 里的 `ipv4_address` 是否一致。

反向的坑同样要避免：**绝不能填 `0.0.0.0/0`**，那等于信任一切，
`X-Forwarded-For` 重新变得可伪造，限流再次失效。

---

## 上线自检清单

```bash
# 1. HSTS 已下发
curl -sI https://token.example.com/api/pricing | grep -i strict-transport

# 2. 前端资源长缓存、index.html 不缓存
curl -sI https://token.example.com/ | grep -i cache-control              # no-store
curl -sI https://token.example.com/assets/<某个文件名> | grep -i cache-control
#    -> public, max-age=31536000, immutable

# 3. 凭证不接受走 URL（应返回 401）
curl -s -o /dev/null -w '%{http_code}\n' \
     "https://token.example.com/v1/models?token=sk-anything"

# 4. 上传 10MB 文件不应出现 413
curl -s -o /dev/null -w '%{http_code}\n' \
     -H "Authorization: Bearer sk-xxx" \
     -F "file=@10mb.png" https://token.example.com/v1/uploads/images

# 5. 从公网直连应用端口应当连不上（端口根本没发布）
curl -m 3 http://<服务器IP>:3001/health

# 6. 数据库端口同样不可达
curl -m 3 telnet://<服务器IP>:5432
```

其他要逐项确认的：

- [ ] `JWT_SECRET` 与 `SECRET_KEY` 均 ≥32 字符随机值。服务在缺失或过短时会**拒绝启动**。
      ⚠️ `SECRET_KEY` 一旦用于加密数据后不可更改，否则已存的供应商密钥将无法解密。
- [ ] `GIN_MODE=release`、`DEBUG=false`。
- [ ] 防火墙与云安全组都只放行 22 / 80 / 443。
- [ ] 按业务规模调整 `API_RATE_LIMIT_PER_MINUTE` / `API_RATE_LIMIT_BURST`。
- [ ] 确认每个启用中的模型都配置了计费规则：未配置规则的模型会返回
      「该模型未配置计费规则，暂不可用」，而不是静默免费。
- [ ] 登录后立即修改 root 密码，并清空 `.env` 里的 `INITIAL_ROOT_PASSWORD`。
- [ ] 证书续期的 cron 已挂上，并且**手动跑一次确认能成功**。

---

## 升级

```bash
# 本机
./deploy/build-image.sh linux/amd64
scp token-hub-latest-amd64.tar.gz <user>@<server>:/tmp/

# 服务器
gunzip -c /tmp/token-hub-latest-amd64.tar.gz | docker load
cd /opt/stacks/token-hub && docker compose up -d
```

应用启动时会自动跑 `AutoMigrate`，不需要单独的迁移步骤。
数据库结构变更前建议先备份。

**不需要重启网关** —— 上游用 `resolver` 解析，容器重建换了 IP 会在 10 秒内自动生效
（理由见上文）。

---

## 回滚

镜像按标签保留，回滚就是换标签：

```bash
cd /opt/stacks/token-hub
sed -i 's/^TOKEN_HUB_TAG=.*/TOKEN_HUB_TAG=<上一个版本>/' .env
docker compose up -d
```

所以打镜像时建议带版本号而不是一律 `latest`：

```bash
TAG=v1.2.3 ./deploy/build-image.sh linux/amd64
```

---

## 备份

```bash
docker compose -f /opt/stacks/token-hub/docker-compose.yml exec -T postgres \
  pg_dump -U token_hub token_hub | gzip > /root/backup/token-hub-$(date +%F).sql.gz
```

挂 cron，并定期**实际恢复一次**验证备份可用 —— 没验证过的备份等于没有备份。

`.env` 也要备份：`SECRET_KEY` 丢了，数据库里的供应商 API Key 就永远解不开了。

---

## 日志

```bash
cd /opt/stacks/gateway
tail -f logs/token-hub.access.log      # 网关侧访问日志（已剔除查询串）

docker compose -f /opt/stacks/token-hub/docker-compose.yml logs -f token-hub
```

---

## 排查表

| 现象 | 多半是 |
|---|---|
| 某个域名 502，另两个正常 | 那个服务没起来 / 服务名对不上 / 没接进 `gateway-proxy` 网络 |
| 全部域名 502 | 网关没起来，`docker compose exec gateway nginx -t` 看配置 |
| 网关容器起不来 | 证书文件缺失，跑一次 `./scripts/self-signed.sh` |
| 登录失败几次后所有人都登不进 | `TRUSTED_PROXIES` 没配，所有人被当成同一来源 |
| 上传 10MB 图片返回 413 | 网关 `client_max_body_size` 没生效（确认 `nginx -t` 加载了本目录的模板） |
| 长回答到一半卡住 | 该服务的 `proxy_read_timeout` 太短 / `proxy_buffering` 没关 |
| 页面能开但字体不对 | CSP 拦掉了字体 CDN，见 `webui/webui.go` 的 `contentSecurityPolicy` |
| 容器报 `exec format error` | 镜像架构和服务器不匹配，重新用正确的 `PLATFORM` 构建 |
