# 生产日志查看

本文只讲**已按 [README.md](README.md) 部署好**的服务器上，日志在哪里、怎么看。

文中路径按「登录用户是 `ubuntu`、主目录是 `/home/ubuntu`」来写，与 [README.md 第 1.2 节](README.md#12-全文约定)一致。所有命令都在服务器上执行。

## 1 日志总览

生产环境有三类日志，位置和查看方式完全不同，先分清再看。

| 日志 | 产生者 | 落在哪 | 查看方式 |
|---|---|---|---|
| 应用请求与系统日志 | `futureai-api` 容器 | Docker 的 json-file 日志 | `docker compose logs` |
| 数据库日志 | `postgres` 容器 | Docker 的 json-file 日志 | `docker compose logs` |
| 网关访问 / 错误日志 | 网关里的 nginx | 宿主机 `/home/ubuntu/stacks/gateway/logs/` | `tail` 文件 |
| 网关启动输出 | 容器入口脚本 | Docker 的 json-file 日志 | `docker compose logs` |
| 定时任务输出 | backup / rotate / certbot | `/home/ubuntu/logs/*.log` | `tail` 文件 |

两条最常用的命令：

```bash
# 应用日志，实时跟踪
cd /home/ubuntu/stacks/futureai-api
docker compose logs -f --tail=100 futureai-api
```

```bash
# 网关访问日志，实时跟踪
cd /home/ubuntu/stacks/gateway
tail -F logs/futureai-api.access.log
```

> **注意**：网关的请求日志**不在** `docker compose logs gateway` 里。nginx 把访问日志和错误日志写进了挂载到宿主机的文件，容器 stdout 里只有启动信息。见第 3 章。

## 2 应用日志

### 2.1 基本命令

生产 compose 的项目名是 `futureai-api-prod`，服务名是 `futureai-api`。在配置目录里直接用服务名即可：

```bash
cd /home/ubuntu/stacks/futureai-api

docker compose logs -f --tail=100 futureai-api   # 实时跟踪，Ctrl+C 退出
docker compose logs --tail=200 futureai-api      # 最近 200 行
docker compose logs -t --since 30m futureai-api  # 带时间戳，最近 30 分钟
docker compose logs --since 2h futureai-api      # 最近 2 小时
```

不在那个目录时，也可以按 compose 文件和容器名查：

```bash
docker compose -f /home/ubuntu/stacks/futureai-api/docker-compose.yml logs -f futureai-api
docker logs -f --tail=100 futureai-api-prod-futureai-api-1
```

容器名里的 `-1` 是副本序号，由 compose 自动生成；不要依赖它写脚本，目录里的 `docker compose` 写法更稳。

### 2.2 一次正常启动的日志

```text
using PostgreSQL as database
database migration started
FutureAI API started on port 3001
```

日志停在 `database migration` 或进程直接退出，多半是 `.env` 里的 `POSTGRES_PASSWORD` 填错了，见 [README 第 5.11 节](README.md#511-看启动日志)。

### 2.3 日志里的关键前缀

应用没有分级日志系统，靠前缀区分：

| 前缀 | 含义 |
|---|---|
| `[FUTUREAI-API]` | 系统日志（普通信息） |
| `[FUTUREAI-API] [ERROR]` | 系统错误 |
| `[FUTUREAI-API] [FATAL]` | 致命错误，进程随后退出 |
| `[PANIC]` | 请求触发了 panic，下一行是堆栈 |
| `[GIN]` | 每条 HTTP 请求一行访问日志 |
| `[安全]` | 启动时的安全配置输出，如 `信任的代理网段: 172.20.0.2/32` |
| `[迁移]` | 数据库迁移，升级时重点看 |
| `[FileCleaner]` | 过期对象清理 |
| `[SUBMIT_UNKNOWN]` | 上游提交结果不确定，需要人工对账 |
| `[需人工对账]` | 超过 6 小时仍未终态、已被自动退款的任务 |

`[GIN]` 行形如：

```text
[GIN] 2026/11/09 - 12:34:56 | 200 |    12.3ms | 203.0.113.10 | POST     "/api/auth/login"
```

查询串被刻意剔除（`SkipQueryString: true`），因此不会在日志里看到 token 等参数。来源 IP 是**真实客户端 IP**，前提是 `TRUSTED_PROXIES` 配对了；启动日志里应能看到 `[安全] 信任的代理网段: 172.20.0.2/32`。

### 2.4 常用过滤

```bash
# 只看错误
docker compose logs futureai-api | grep -E '\[ERROR\]|\[FATAL\]|\[PANIC\]'

# 登录相关（验证 TRUSTED_PROXIES）
docker compose logs futureai-api | grep -i login

# 按任务 ID 追一条图像任务的全过程
docker compose logs futureai-api | grep '<taskID>'

# 排除每 30 秒一次的健康检查噪声
docker compose logs futureai-api | grep -v '/health'
```

> **注意**：容器健康检查每 30 秒请求一次 `/health`，应用会照常记一条 `[GIN]` 日志。这不是异常；过滤时用上面的 `grep -v '/health'` 去掉。

### 2.5 保留上限

应用与数据库容器都配了日志滚动（[docker-compose.yml](futureai-api/docker-compose.yml)）：

```yaml
logging:
  driver: json-file
  options:
    max-size: "10m"
    max-file: "3"
```

即每个容器最多约 30MB，超出的旧日志会被删除。所以**事故发生后要尽快取日志**，隔太久可能已经被覆盖。确认真实日志文件位置与大小：

```bash
docker inspect --format '{{.LogPath}}' futureai-api-prod-futureai-api-1
ls -lh "$(docker inspect --format '{{.LogPath}}' futureai-api-prod-futureai-api-1)"
```

不要用 `DEBUG=true` 来「多打点日志」：它会额外输出 SQL 日志，既可能包含敏感数据，也会迅速吃掉这 30MB 的额度。生产保持 `.env` 里的 `DEBUG=false`。

## 3 网关日志

### 3.1 文件在哪

`deploy/gateway/docker-compose.yml` 把宿主机目录挂进了容器：

```yaml
volumes:
  - ./logs:/var/log/nginx
```

因此在服务器上直接看 `/home/ubuntu/stacks/gateway/logs/` 下的文件：

| 文件 | 内容 |
|---|---|
| `futureai-api.access.log` | 443 端口上 futureai-api 的访问日志 |
| `http.access.log` | 80 端口：ACME 校验请求与跳转 HTTPS |
| `error.log` | nginx 自身的错误，502、证书问题等看这里 |
| `*.log.<时间戳>` | 已轮转的归档 |

```bash
cd /home/ubuntu/stacks/gateway

tail -F logs/futureai-api.access.log   # 用 -F 而不是 -f：日志轮转后仍能跟到新文件
tail -F logs/error.log
ls -lh logs/                           # 看当前文件与归档
```

### 3.2 一条访问日志怎么读

日志格式定义在 [templates/default.conf.template](gateway/templates/default.conf.template)：

```text
203.0.113.10 - - [09/Nov/2026:12:34:56 +0000] "POST /api/auth/login HTTP/1.1" 200 123 "-" "Mozilla/5.0" rt=0.013 urt=0.012
```

| 字段 | 含义 |
|---|---|
| 第一个字段 | 客户端真实 IP（网关是边缘节点，看到的即真实来源） |
| `[09/Nov/2026:12:34:56 +0000]` | 请求时间，容器时区（默认 UTC） |
| `"POST /api/auth/login HTTP/1.1"` | 方法、**路径**、协议；查询串已被刻意剔除 |
| `200` | 状态码 |
| `rt=0.013` | 网关处理总耗时（秒） |
| `urt=0.012` | 上游应用耗时（秒）；`urt=-` 表示请求没有转发到应用 |

两条关键判读：

- **429 是谁返回的**：`urt=-` 是网关层 `limit_req` 直接拦下的（登录接口限流）；`urt` 有数字是应用返回的。
- **5xx 断在哪**：`urt=-` 说明网关根本没连上应用，先查应用是否在运行；`urt` 有数字说明应用返回了 5xx，去应用日志里找同一时间的 `[ERROR]` / `[PANIC]`。

> **注意**：`/health` 在网关侧配置了 `access_log off`，不会出现在访问日志里。健康检查失败要看应用日志或 `docker compose ps`。

### 3.3 常用过滤

```bash
cd /home/ubuntu/stacks/gateway

# 最近的 5xx
grep -E 'HTTP/[0-9.]+" 5[0-9][0-9] ' logs/futureai-api.access.log | tail -n 50

# 429：先看是网关拦的还是应用拦的
grep -E 'HTTP/[0-9.]+" 429 ' logs/futureai-api.access.log | grep 'urt=-'     # 网关限流
grep -E 'HTTP/[0-9.]+" 429 ' logs/futureai-api.access.log | grep -v 'urt=-'  # 应用返回

# 慢请求：rt 超过 5 秒
awk '{for (i = 1; i <= NF; i++) if ($i ~ /^rt=/) {sub(/^rt=/, "", $i); if ($i + 0 > 5) print}}' \
  logs/futureai-api.access.log | tail -n 50

# 网关连接应用的错误
grep -iE 'connect\(\) failed|upstream|no live upstreams' logs/error.log | tail -n 50
```

按时间翻历史（nginx 时间格式是 `日/月/年:时`，UTC）：

```bash
grep '09/Nov/2026:1[0-2]' logs/futureai-api.access.log
```

### 3.4 轮转与保留

网关日志是**普通文件**，Docker 的 `max-size` 管不到它。不轮转迟早写满磁盘，而数据库与应用同盘，会一起挂。

定时任务每天 4:00 执行 [scripts/rotate-logs.sh](gateway/scripts/rotate-logs.sh)：

- 把 `*.log` 改名为 `*.log.<YYYY-MM-DD_HHMMSS>`；
- 通知 nginx 重新打开日志；
- 删除 14 天前的归档（`KEEP_DAYS` 可调）。

脚本输出写在 `/home/ubuntu/logs/rotate-logs.log`。手工执行与调整保留天数：

```bash
cd /home/ubuntu/stacks/gateway
./scripts/rotate-logs.sh
KEEP_DAYS=30 ./scripts/rotate-logs.sh
```

> **注意**：不要用 `rm logs/*.log` 或 `find ... -mtime +14 -delete` 清理正在写入的日志。nginx 持有打开句柄，删掉只是解除目录项，磁盘空间不会立刻释放，日志还会继续写进那个不可见的文件。必须走 `rotate-logs.sh` 的「改名 → reopen → 清理归档」顺序。

### 3.5 网关容器的 stdout 日志

只看 nginx 启动失败、权限问题这类容器级输出时：

```bash
cd /home/ubuntu/stacks/gateway
docker compose logs --tail=100 gateway
```

再次强调：**请求日志不在这里**。`docker compose logs gateway` 通常只有入口脚本的几行输出。

## 4 数据库与定时任务日志

数据库（容器名固定为 `futureai-api-pg`）：

```bash
cd /home/ubuntu/stacks/futureai-api
docker compose logs --tail=100 postgres
docker logs -f futureai-api-pg
```

定时任务的输出都通过 cron 重定向到宿主机文件，不走 Docker：

```bash
tail -n 50 /home/ubuntu/logs/futureai-api-backup.log
tail -n 50 /home/ubuntu/logs/rotate-logs.log
tail -n 50 /home/ubuntu/logs/certbot-renew.log
```

这三个文件没有自动轮转，但每天只追加几行，增长很慢。需要时手工归档即可。任务是否挂上、为什么没执行，见 [README 第 9 章](README.md#9-服务器挂定时任务)。

## 5 常见排查路径

| 现象 | 先看 | 关键点 |
|---|---|---|
| 应用起不来 | `docker compose logs --tail=100 futureai-api` | 找 `[FATAL]`；密钥缺失、端口占用、数据库密码错都会在这里直接说 |
| 域名返回 502 | 网关 `error.log` + 应用 `docker compose ps` | `connect() failed` 说明应用没起来或不在 `gateway-proxy` 网络 |
| 登录被 429 | 网关访问日志里 ` 429 ` 的 `urt` | `urt=-` 是网关限流，有数字是应用限流；再确认来源 IP 不是所有人同一个 |
| 请求很慢 | 网关访问日志的 `rt` / `urt` | `rt` 大而 `urt` 小是网关或网络；两者都大是应用或上游供应商慢 |
| 用户说扣了积分没出图 | 应用日志按 taskID grep | 找 `[SUBMIT_UNKNOWN]` / `[需人工对账]`，按 [NOTES 排查表](NOTES.md#10-排查表)处理 |
| 升级后行为不对 | `docker compose logs --tail=50 futureai-api` | 确认启动日志、`[迁移]` 行与实际镜像标签（`docker compose images futureai-api`） |
| 磁盘告警 | `df -h`、`du -sh`（见第 6 章） | 重点看网关日志目录和 `/var/lib/docker/containers` |

排查表还有更完整的现象与原因对照，见 [NOTES.md 第 10 章](NOTES.md#10-排查表)。

## 6 时间、容量与清理

### 6.1 时区

服务器与容器默认都用 UTC：

- `docker compose logs -t` 的时间戳是 UTC；
- nginx 的 `$time_local` 也是容器时区（默认 UTC）。

人在东八区时，日志时间比本地时间**早 8 小时**。对比应用与网关日志时两边都是 UTC，可以直接对齐；不确定时优先用相对时间过滤，避免换算：

```bash
docker compose logs --since 30m futureai-api
docker compose logs --since 2h futureai-api
```

### 6.2 检查磁盘

```bash
df -h
du -sh /var/lib/docker/containers
du -sh /home/ubuntu/stacks/gateway/logs
```

容器日志受 30MB 上限约束，通常不是大头；网关日志和 Docker 镜像才是。不要执行 `docker system prune -a`，它会删掉回滚要用的历史镜像（见 [UPDATE.md 第 7 章](UPDATE.md#7-回滚)）。

### 6.3 保留策略一览

| 日志 | 保留 |
|---|---|
| 应用容器 stdout/stderr | 约 30MB（10MB × 3 个文件） |
| 数据库容器 stdout/stderr | 约 30MB |
| 网关请求 / 错误日志 | 每天轮转，保留 14 天（可调 `KEEP_DAYS`） |
| 网关容器 stdout | 未设上限，但量极小 |
| 定时任务输出 | 不自动清理，增长很慢 |

需要长期留存的日志，应在覆盖之前导出到别处；容器日志一旦滚动就找不回来了。
