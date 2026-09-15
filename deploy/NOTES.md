# 部署笔记

[README.md](README.md) 只列了照着敲的命令。这里放**为什么这么设计**、以及出问题时的排查资料。
平时不用看。

---

## 为什么是这套结构

有三条硬性约束，整套方案就是围绕它们设计的：

1. **80/443 全机器只有一个进程能占** —— 所以只有网关容器发布这两个端口，
   应用只在网关背后的内网里监听。
2. **数据库端口一个都不发布** —— 容器间用服务名互访。这同时消除了
   「PostgreSQL 暴露到公网、密码哈希与供应商密钥密文被直读」的风险。
3. **应用必须知道网关是可信的** —— 否则从 `X-Forwarded-For` 还原不出真实
   客户端 IP，登录限流会把全体用户当成同一个人。

这套结构也是为**以后往同一台机器上加服务**准备的：新服务接进同一个共享网络、
在网关上加一个 server 块就能跑，不必再动 80/443 的占用关系。

共享网络 `gateway-proxy`（172.20.0.0/16）是手工建的，没有交给任何 compose 项目管：
由 compose 管理的网络，项目 `down` 时可能把它一起删掉，导致服务瞬间失去网络。

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
而服务每次升级都会重建容器，踩中的概率很高。

`resolver` 指向 Docker 内置 DNS，`valid=10s` 让 IP 变化在 10 秒内生效。
代价是失去了 upstream keepalive，每个请求要新建一条到同机容器的连接 ——
在同一个 docker 网络里这个开销可以忽略，换来的是**升级后不需要碰网关**。

---

## 为什么必须先有自签证书

nginx 的 `ssl_certificate` 指向的文件不存在时，**进程会直接启动失败** ——
不是某个域名 502，而是整个网关起不来。所以正式证书签发之前，必须先跑一次
`scripts/self-signed.sh` 放一对证书占位。（这正是 [README 第 2 步](README.md#2-服务器--起网关)那条注意事项。）

浏览器在自签阶段会报 `ERR_CERT_AUTHORITY_INVALID`。**这不是配错了**，而恰恰说明前面的环节都对：

| 错误码 | 含义 |
|---|---|
| `ERR_CERT_AUTHORITY_INVALID` | 域名匹配成功，只是签发者（它自己）不在系统信任列表里 ← 自签阶段就该是这个 |
| `ERR_CERT_COMMON_NAME_INVALID` | 域名对不上，说明证书的 SAN 或 SNI 分流有问题 |

所以看到第一种可以直接点「继续前往」，或用 `curl -k`。看到第二种才是真有问题。

---

## TRUSTED_PROXIES 为什么这么配

**验证方法在 [README 第 4 步](README.md#4-服务器--验证)**，这里只讲背后的取舍。

`X-Forwarded-For` 的可信度完全取决于「谁写的这条头」。网关用
`$proxy_add_x_forwarded_for` 把真实客户端 IP **追加**到链尾，而 Gin 从右往左
找到第一个不在信任列表里的 IP —— 前提是应用知道网关是可信的。

因此这个值要卡在两头之间：

| 填什么 | 后果 |
|---|---|
| `172.20.0.2/32`（网关容器固定 IP） | 正确。只有网关追加的 IP 会被采信 |
| 留空 | 所有请求的来源都变成网关 IP，全体用户被登录限流当成同一人 |
| `0.0.0.0/0` | **等于信任一切**，客户端自带的伪造 XFF 被采信，限流彻底失效 |

第三个是最危险的，因为它**看起来像是"配好了"** —— 应用日志里能看到真实 IP，
一切正常，只是这个 IP 是攻击者自己填的。任何情况下都不要为了"先跑通"而填它。

同理，网关上 `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for`
也不要改成 `$http_x_forwarded_for`（那会直接透传客户端伪造的值）。

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
# 本机 —— 脚本会打印出本次的标签，形如 20260915225600
./deploy/build-image.sh linux/amd64
scp token-hub-20260915225600-amd64.tar.gz <user>@<server>:/tmp/

# 服务器
TAG=20260915225600
gunzip -c /tmp/token-hub-$TAG-amd64.tar.gz | docker load
cd /opt/stacks/token-hub
sed -i "s/^TOKEN_HUB_TAG=.*/TOKEN_HUB_TAG=$TAG/" .env
docker compose up -d
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
docker image ls token-hub          # 先看还有哪些版本
sed -i 's/^TOKEN_HUB_TAG=.*/TOKEN_HUB_TAG=<上一个版本>/' .env
docker compose up -d
```

打镜像默认就用构建时刻当标签（`20260915225600`），每次都是一个新版本，
旧镜像留在服务器上不会被覆盖。想用更直观的版本号就自己指定：

```bash
TAG=v1.2.3 ./deploy/build-image.sh linux/amd64
```

> 脚本同时会打一个 `latest` 标签指向同一个镜像，方便 `.env` 还是默认值的机器
> 直接跑起来。**但回滚时不要把 `.env` 改回 `latest`** —— 它只代表最后一次
> `docker load` 进来的那个版本，说不清是哪一个。

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
| 域名返回 502 | 应用没起来 / 服务名对不上 / 没接进 `gateway-proxy` 网络 |
| 全部域名 502 | 网关没起来，`docker compose exec gateway nginx -t` 看配置 |
| 网关容器起不来 | 证书文件缺失，跑一次 `./scripts/self-signed.sh` |
| 网关报 `Address already in use` | 建网络时漏了 `--ip-range`，固定 IP `172.20.0.2` 被别的容器占了。让占用者 `docker network disconnect` 后重启网关 |
| 分不清 429 是网关拦的还是应用拦的 | 看访问日志的 `urt` 字段：`urt=-` 是网关直接返回没转发，有数字是应用返回的 |
| 登录失败几次后所有人都登不进 | `TRUSTED_PROXIES` 没配，所有人被当成同一来源 |
| 上传 10MB 图片返回 413 | 网关 `client_max_body_size` 没生效（确认 `nginx -t` 加载了本目录的模板） |
| 长回答到一半卡住 | 该服务的 `proxy_read_timeout` 太短 / `proxy_buffering` 没关 |
| 页面能开但字体不对 | CSP 拦掉了字体 CDN，见 `webui/webui.go` 的 `contentSecurityPolicy` |
| 容器报 `exec format error` | 镜像架构和服务器不匹配，重新用正确的 `PLATFORM` 构建 |
