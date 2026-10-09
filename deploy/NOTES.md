# 部署笔记

[README.md](README.md) 只列照着敲的命令。本文收录**为什么这么设计**，以及出问题时的排查资料。日常部署不需要阅读。

## 1 为什么是这套结构

有三条硬性约束，整套方案围绕它们设计：

1. **80/443 全机器只能有一个进程占用**，因此只有网关容器发布这两个端口，应用只在网关背后的内网中监听。
2. **数据库端口一个都不发布**，容器之间用服务名互访。这同时消除了「PostgreSQL 暴露到公网、密码哈希与供应商密钥密文被直读」的风险。
3. **应用必须知道网关是可信的**，否则无法从 `X-Forwarded-For` 还原真实客户端 IP，登录限流会把全体用户当成同一个人。

这套结构也为**以后往同一台机器上加服务**做了准备：新服务接入同一个共享网络、在网关上增加一个 `server` 块即可，不必再改动 80/443 的占用关系。

共享网络 `gateway-proxy`（172.20.0.0/16）是手工创建的，没有交给任何 compose 项目管理。由 compose 管理的网络，在项目 `down` 时可能被一并删除，导致服务瞬间失去网络。

## 2 为什么上游用 resolver + 变量，而不是 upstream 块

网关配置里写的是：

```nginx
resolver 127.0.0.11 valid=10s ipv6=off;
set $futureai_api_up "futureai-api:3001";
proxy_pass http://$futureai_api_up;
```

而不是常见的：

```nginx
upstream backend { server futureai-api:3001; }
```

原因是 nginx 对 `upstream` 块中的主机名**只在启动时解析一次**。容器重建换了 IP 之后，网关会一直把请求打到旧地址，表现为持续 502，直到手动重启 nginx。而服务每次升级都会重建容器，踩中的概率很高。

`resolver` 指向 Docker 内置 DNS，`valid=10s` 让 IP 变化在 10 秒内生效。代价是失去 upstream keepalive，每个请求要新建一条到同机容器的连接；在同一个 Docker 网络内这个开销可以忽略，换来的是**升级后不需要碰网关**。

## 3 为什么必须先有自签证书

nginx 的 `ssl_certificate` 指向的文件不存在时，**进程会直接启动失败**——不是某个域名返回 502，而是整个网关起不来。因此正式证书签发之前，必须先执行一次 `scripts/self-signed.sh` 放一对证书占位。这正是 [README 第 4.4 步](README.md#44-生成自签证书)那条注意事项。

浏览器在自签阶段会报 `ERR_CERT_AUTHORITY_INVALID`。**这不是配置错误**，恰恰说明前面的环节都正确。

| 错误码 | 含义 |
|---|---|
| `ERR_CERT_AUTHORITY_INVALID` | 域名匹配成功，只是签发者（它自己）不在系统信任列表中，自签阶段本应如此 |
| `ERR_CERT_COMMON_NAME_INVALID` | 域名对不上，说明证书的 SAN 或 SNI 分流有问题 |

因此看到第一种可以直接继续访问，或使用 `curl -k`；看到第二种才是真有问题。

## 4 TRUSTED_PROXIES 为什么这么配

**验证方法在 [README 第 6.4 步](README.md#64-确认-trusted_proxies-生效)**，本节只讲背后的取舍。

`X-Forwarded-For` 的可信度完全取决于「谁写的这条头」。网关用 `$proxy_add_x_forwarded_for` 把真实客户端 IP **追加**到链尾，而 Gin 从右往左找到第一个不在信任列表里的 IP——前提是应用知道网关是可信的。

因此这个值要卡在两头之间。

| 填什么 | 后果 |
|---|---|
| `172.20.0.2/32`（网关容器固定 IP） | 正确。只有网关追加的 IP 会被采信 |
| 留空 | 所有请求的来源都变成网关 IP，全体用户被登录限流当成同一人 |
| `0.0.0.0/0` | **等于信任一切**，客户端自带的伪造 XFF 被采信，限流彻底失效 |

第三种最危险，因为它**看起来像是配置已完成**：应用日志里能看到真实 IP，一切正常，只是这个 IP 是攻击者自己填的。任何情况下都不要为了「先跑通」而填它。

同理，网关上 `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for` 也不要改成 `$http_x_forwarded_for`，那会直接透传客户端伪造的值。

## 5 上线自检清单

以下命令逐条执行，结果与预期一致方可上线。

**1. HSTS 已下发**

```bash
curl -sI https://futureaiapi.com/api/pricing | grep -i strict-transport
```

**2. 前端资源长缓存，index.html 不缓存**

```bash
curl -sI https://futureaiapi.com/
```

预期 `cache-control: no-store`。

```bash
curl -sI https://futureaiapi.com/assets/<某个文件名>
```

预期 `cache-control: public, max-age=31536000, immutable`。

**3. 凭证不接受走 URL，应返回 401**

```bash
curl -s -o /dev/null -w '%{http_code}\n' "https://futureaiapi.com/v1/models?token=sk-anything"
```

**4. 上传 10MB 文件不应出现 413**

```bash
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer sk-xxx" -F "file=@10mb.png" https://futureaiapi.com/v1/uploads/images
```

**5. 请求体限制生效，超大 JSON 应返回 413**

先造一个 3MB 的请求体：

```bash
head -c 3000000 /dev/zero | tr '\0' 'a' > /tmp/big.txt
```

再打这个无需认证的接口：

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X POST https://futureaiapi.com/api/auth/login -H 'Content-Type: application/json' --data-binary @/tmp/big.txt
```

预期 `413`；若返回 `400` 或 `200`，说明中间件没挂上。

**6. 从公网直连应用端口应当连不上**

```bash
curl -m 3 http://<服务器IP>:3001/health
```

**7. 数据库端口同样不可达**

```bash
curl -m 3 telnet://<服务器IP>:5432
```

其余需逐项确认的：

- [ ] `JWT_SECRET` 与 `SECRET_KEY` 均为 ≥32 字符的随机值。服务在缺失或过短时会拒绝启动。
- [ ] `SECRET_KEY` 已另存一份到密码管理器。它一旦丢失（磁盘损坏或误删 `.env`），库中所有供应商 API Key 将永久不可解。
- [ ] `GIN_MODE=release`、`DEBUG=false`。`DEBUG` 开启后会打印 SQL 日志。
- [ ] 防火墙与云安全组都只放行 22 / 80 / 443。
- [ ] 已按业务规模调整 `API_RATE_LIMIT_PER_MINUTE` / `API_RATE_LIMIT_BURST`。
- [ ] 确认每个启用中的模型都配置了计费规则。未配置规则的模型会返回「该模型未配置计费规则，暂不可用」，而不是静默免费。启动日志中 `[计费] ... 缺少计费规则` 会列出漏配的模型，逐条清掉再上线。
- [ ] 已决定 `UPLOAD_CREDITS`。默认 0 表示上传免费；上传是拿平台自己的供应商密钥把用户文件转存到上游，成本全由平台承担，不收费时至少要确认这个口子可以接受。
- [ ] 若服务器必须经代理才能访问供应商 API，已设置 `OUTBOUND_PROXY`（本服务不读 `HTTP_PROXY` / `HTTPS_PROXY`）。设错的表现是所有图像生成超时且日志没有任何提示。可先在容器里验证连通性：

      ```bash
      docker compose exec futureai-api wget -qO- --timeout=5 <供应商域名>
      ```

- [ ] 登录后立即修改 root 密码，并清空 `.env` 里的 `INITIAL_ROOT_PASSWORD`。
- [ ] 证书续期的 cron 已挂上，并且**手动跑过一次确认能成功**。注意用 `sudo crontab -e`：日志写在 `/var/log/`，普通用户无权限，cron 会静默失败，直到证书过期、网站打不开才发现。
- [ ] 备份 cron 已挂上，并**实际跑一次 `restore.sh` 验证备份可用**。
- [ ] 网关日志的清理 cron 已挂上，即 [README 第 9.1 步](README.md#91-挂定时任务)的第三条。这条最容易漏，而它是唯一会无限增长的日志，写满磁盘会把同盘的数据库一起拖死。
- [ ] 确认**只有网关一个入口**。仓库根目录的 `docker-compose.yml` 是开发用的，它把应用端口发布到宿主机，不要把它跑在公网机器上。
- [ ] 确认本次是**单实例部署**。限流器与任务对账状态都在进程内存中，横向扩到 2 个副本会让限流翻倍宽松，且同一个任务可能被两个实例同时轮询。
- [ ] 首日观察 HSTS。出厂值已按「首次上线」调成 `max-age=300`，稳定一两天后再改回一年并考虑开启 `includeSubDomains`，见 `snippets/tls.conf`。

## 6 升级

先在本机构建并上传新镜像：

```bash
./deploy/build-image.sh linux/amd64
```

```bash
scp futureai-api-20260915225600-amd64.tar.gz <user>@<server>:/tmp/
```

再在服务器上备份。**必须先备份，再升级**：

```bash
/opt/stacks/futureai-api/backup.sh
```

先把标签记进变量，本章后续命令都在同一个终端会话中执行：

```bash
TAG=20260915225600
```

加载新镜像：

```bash
gunzip -c /tmp/futureai-api-$TAG-amd64.tar.gz | docker load
```

记录本次标签与 commit 的对应关系，回滚时要靠它确定「上一个版本」：

```bash
docker image inspect futureai-api:$TAG --format '{{index .Config.Labels "org.opencontainers.image.revision"}}'
```

切换标签并重启：

```bash
cd /opt/stacks/futureai-api
```

```bash
sed -i "s/^FUTUREAI_API_TAG=.*/FUTUREAI_API_TAG=$TAG/" .env
```

```bash
docker compose up -d
```

**升级前必须备份**：应用启动时会自动跑 `AutoMigrate`，不需要单独的迁移步骤，但**部分变更不可逆**，回滚旧镜像不一定能回到旧 schema。有些版本还会在启动时执行 `ALTER TABLE`（例如放宽积分列精度），那会在启动阶段重写大表并加表锁，期间服务不可用、请求超时排队。看到启动日志里出现「放宽 xxx 的标度」时，这次升级就不是秒级的。

**不需要重启网关**：上游用 `resolver` 解析，容器重建换了 IP 会在 10 秒内自动生效，理由见第 2 章。

**升级会有数秒中断**：应用收到 SIGTERM 后会先把在途请求跑完再退出（宽限期 40 秒），但新容器起来之前网关会返回 502。应选低峰期进行。

## 7 回滚

镜像按标签保留，回滚就是换标签。

先看还有哪些版本：

```bash
cd /opt/stacks/futureai-api
```

```bash
docker image ls futureai-api
```

查某个标签对应哪个 commit：

```bash
docker image inspect futureai-api:<标签> --format '{{index .Config.Labels "org.opencontainers.image.revision"}}'
```

改回上一个版本的标签：

```bash
sed -i 's/^FUTUREAI_API_TAG=.*/FUTUREAI_API_TAG=<上一个版本>/' .env
```

```bash
docker compose up -d
```

> **注意**：回滚前先评估 schema 变更。`AutoMigrate` 只加不删，新版本引入的列与索引在回滚后依然留在库里。旧代码通常不认识它们（无害），但如果新版本对已有列做过类型变更，旧代码可能读不回来。不确定就先在临时库上试一次。

数据库本身的回滚要用备份：

```bash
./restore.sh /opt/backups/futureai-api/db-<时间戳>.sql.gz
```

打镜像默认用构建时刻当标签（`20260915225600`），每次都是一个新版本，旧镜像留在服务器上不会被覆盖。想用更直观的版本号可以自己指定：

```bash
TAG=v1.2.3 ./deploy/build-image.sh linux/amd64
```

> 脚本同时会打一个 `latest` 标签指向同一个镜像，方便 `.env` 还是默认值的机器直接跑起来。**但回滚时不要把 `.env` 改回 `latest`**，它只代表最后一次 `docker load` 进来的那个版本，说不清是哪一个。

## 8 备份

使用 `deploy/backup.sh`，它会自动建目录、从 `.env` 读库名、轮转旧备份。

手动跑一次（必须用 `sudo`，脚本要读 `.env` 并写 `/opt`）：

```bash
sudo /opt/stacks/futureai-api/backup.sh
```

挂到 cron：

```bash
sudo crontab -e
```

```crontab
30 3 * * * /opt/stacks/futureai-api/backup.sh >> /var/log/futureai-api-backup.log 2>&1
```

脚本会同时备份数据库和 `.env`。**`.env` 必须一起备份**：`SECRET_KEY` 丢了，库里所有供应商 API Key 就永远解不开，数据库恢复得再好也没用。

保留份数用 `KEEP` 控制：

```bash
KEEP=30 /opt/stacks/futureai-api/backup.sh
```

> **注意**：没验证过的备份等于没有备份。每季度实际恢复一次：

```bash
sudo /opt/stacks/futureai-api/restore.sh /opt/backups/futureai-api/db-<时间戳>.sql.gz
```

`restore.sh` 会停应用、恢复、再拉起，并要求二次确认。

## 9 日志

查看网关侧访问日志（已剔除查询串）：

```bash
cd /opt/stacks/gateway
```

```bash
tail -f logs/futureai-api.access.log
```

查看应用日志：

```bash
docker compose -f /opt/stacks/futureai-api/docker-compose.yml logs -f futureai-api
```

**日志必须轮转，否则迟早写满磁盘**，而数据库与应用同盘，会一起挂。

- 容器日志：两份 compose 都已设 `logging.options.max-size=10m / max-file=3`，不需要额外操作。它管的是容器的 stdout / stderr。
- 网关日志：nginx 把访问日志写进挂载到宿主机的 `deploy/gateway/logs/`。**这是文件，不是容器的 stdout，上述上限管不到它**，必须靠主机侧的定时删除，命令见 [README 第 9.1 步](README.md#91-挂定时任务)。这是整份编排里唯一会无限增长的东西。

上线后一周内留意磁盘：

```bash
df -h
```

```bash
du -sh /var/lib/docker/containers
```

## 10 排查表

下表按现象列出可能的原因。

| 现象 | 多半是 |
|---|---|
| 域名返回 502 | 应用没起来 / 服务名对不上 / 没接进 `gateway-proxy` 网络 |
| 全部域名 502 | 网关没起来，用 `docker compose exec gateway nginx -t` 看配置 |
| 网关容器起不来 | 证书文件缺失，跑一次 `./scripts/self-signed.sh` |
| 网关报 `Address already in use` | 建网络时漏了 `--ip-range`，固定 IP `172.20.0.2` 被别的容器占了。让占用者 `docker network disconnect` 后重启网关 |
| 分不清 429 是网关拦的还是应用拦的 | 看访问日志的 `urt` 字段：`urt=-` 是网关直接返回没转发，有数字是应用返回的 |
| 登录失败几次后所有人都登不进 | `TRUSTED_PROXIES` 没配，所有人被当成同一来源 |
| 上传 10MB 图片返回 413 | 网关 `client_max_body_size` 没生效，确认 `nginx -t` 加载了本目录的模板 |
| 长回答到一半卡住 | 该服务的 `proxy_read_timeout` 太短，或 `proxy_buffering` 没关 |
| 页面能开但字体不对 | CSP 拦掉了字体 CDN，见 `webui/webui.go` 的 `contentSecurityPolicy` |
| 容器报 `exec format error` | 镜像架构和服务器不匹配。构建必须用 `build-image.sh`（走 buildx）；直接 `docker build .` 时 `TARGETOS` / `TARGETARCH` 为空，会静默产出宿主架构镜像 |
| 正常请求返回 413 | 请求体超过上限（JSON 端点 1MB）。参考图请先用 `/v1/uploads/images` 上传再传 URL，不要 base64 内联 |
| 所有图像生成都超时，日志无报错 | 服务器需要代理出网但没设 `OUTBOUND_PROXY`（本服务不读 `HTTP_PROXY`） |
| 任务长期停在 `submitted` | 上游一直没返回终态。超过 6 小时会被对账循环退款并打印 `[需人工对账]` 日志，拿这个 taskID 去上游核对账单 |
| 用户投诉「扣了积分没出图」 | 先看任务状态：`call_fail` 加日志里的 `[SUBMIT_UNKNOWN]` 表示提交结果不确定（已退款）。频繁出现说明上游不稳，或提交超时太短 |
| compose 报 `required variable POSTGRES_PASSWORD is missing a value` | 没执行 `cp .env.example .env`，`.env` 被 gitignore 了。插值发生在选服务之前，所以只起单个服务也一样报 |
| 生产容器报 `container name "/futureai-api" is already in use` | 同机跑过开发栈。生产 compose 已不再写死 `container_name`，若仍报错说明服务器上是旧版编排文件 |
| 网关起来后又因 `Address already in use` 挂掉 | 建网络时漏了 `--ip-range 172.20.128.0/17` |
