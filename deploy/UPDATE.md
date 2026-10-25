# 更新已部署的 futureai-api

本文只讲更新已经在服务器上运行的实例：打包、上传、备份、切换标签、验证。首次部署见 [README.md](README.md)，为什么这么设计见 [NOTES.md](NOTES.md)，回滚见 [NOTES.md 第 7 章](NOTES.md#7-回滚)。

一次更新分六步，前两步在本机，后四步在服务器。

| 步骤 | 在哪台机器 | 做什么 |
|---|---|---|
| 1 | 本机 | 构建镜像并记下标签 |
| 2 | 本机 | 上传镜像包 |
| 3 | 服务器 | 备份数据库与 `.env` |
| 4 | 服务器 | 加载新镜像 |
| 5 | 服务器 | 切换标签并重启应用 |
| 6 | 服务器 | 验证新版本已生效 |

文中的占位写法如下。

| 写法 | 换成 |
|---|---|
| `<user>@<server>` | 你的登录信息，如 `ubuntu@1.2.3.4` |
| `20260915225600` | 第 1.2 步打印出来的实际标签 |
| 镜像包名里的 `amd64` | 服务器为 arm64 时换成 `arm64` |
| 章节标题里的「本机」 / 「服务器」 | 这一章在哪台机器上执行 |

路径按「登录用户是 `ubuntu`、主目录是 `/home/ubuntu`」来写，换了用户名要把 `/home/ubuntu` 一并替换。

## 1 本机：打包

### 1.1 确认代码已提交

在仓库根目录执行：

```bash
git status --short
```

没有输出才继续。有输出说明存在未提交的改动，此时镜像里记录的 commit 与实际内容不一致，出问题时无法确定服务器上跑的到底是哪一版。

若本次发布改动了 `deploy/futureai-api/` 下的部署配置，第 2.2 步要一并上传。是否需要上传，以本次提交的内容为准。

### 1.2 构建镜像

```bash
bash deploy/build-image.sh linux/amd64
```

参数是目标服务器的架构，不是本机的架构。不确定时在服务器上执行 `uname -m`，按下表对应：

| `uname -m` 的输出 | 参数 |
|---|---|
| `x86_64` | `linux/amd64` |
| `aarch64` | `linux/arm64` |

**预期看到**：最后一行形如

```
完成，镜像包 14M，标签 20260915225600。
```

标签取构建时刻，每次构建都不同，记下来供后续章节使用。产出两项：本机镜像 `futureai-api:<标签>` 与仓库根目录下的 `futureai-api-<标签>-<架构>.tar.gz`。

报 `failed to fetch anonymous token` 时见 [../docs/DOCKER.md 第 7.1 节](../docs/DOCKER.md#71-构建时报-failed-to-fetch-anonymous-token)。

## 2 本机：上传

### 2.1 上传镜像包

```bash
scp futureai-api-20260915225600-amd64.tar.gz <user>@<server>:/tmp/
```

`20260915225600` 换成第 1.2 步记下的标签。`scp` 没有报错即为成功。

### 2.2 上传变动的部署配置

本次发布若改动了部署配置，同时上传这两个文件：

```bash
scp deploy/futureai-api/docker-compose.yml deploy/futureai-api/.env.example <user>@<server>:/home/ubuntu/stacks/futureai-api/
```

> **注意**：不要上传本机的 `.env`。服务器上的 `.env` 含本机没有的密钥（`POSTGRES_PASSWORD`、`JWT_SECRET`、`SECRET_KEY`），覆盖后服务无法解密已存的供应商 API Key。若新的 `.env.example` 增加了配置项，登录服务器按模板手工补进 `.env`。

## 3 服务器：备份

登录服务器后执行：

```bash
/home/ubuntu/stacks/futureai-api/backup.sh
```

**预期看到**：最后一行形如 `完成。当前保留 14 份数据库备份`。脚本同时备份数据库与 `.env`。

> **注意**：必须先完成备份再更新。应用启动时会自动执行 `AutoMigrate`，部分变更不可逆，回滚旧镜像不一定能回到旧 schema。脚本报错或没有打印「已备份」时不要继续。不可逆变更的判据与实例见 [NOTES.md 第 6 章](NOTES.md#6-升级)。

## 4 服务器：加载镜像

本章及第 5 章的命令在同一个终端会话中执行。先把标签记进变量：

```bash
TAG=20260915225600
```

`20260915225600` 换成第 1.2 步记下的标签。

加载：

```bash
gunzip -c /tmp/futureai-api-$TAG-amd64.tar.gz | docker load
```

**预期看到**：`Loaded image: futureai-api:20260915225600` 与 `Loaded image: futureai-api:latest` 两行。

确认架构与服务器一致：

```bash
docker image inspect futureai-api:$TAG --format '{{.Architecture}}'
```

输出应为 `amd64` 或 `arm64`，与服务器架构相符。不符时容器会以 `exec format error` 反复退出，须按第 1.2 步用正确参数重新打包。

确认镜像对应的 commit：

```bash
docker image inspect futureai-api:$TAG --format '{{index .Config.Labels "org.opencontainers.image.revision"}}'
```

输出应与本机 `git rev-parse --short HEAD` 相同。回滚时靠它判断某个标签对应哪一版代码。

## 5 服务器：切换标签并重启

```bash
cd /home/ubuntu/stacks/futureai-api
```

以下命令均在此目录执行。

先记下当前标签，回滚时要用：

```bash
grep '^FUTUREAI_API_TAG=' .env
```

改标签：

```bash
sed -i "s/^FUTUREAI_API_TAG=.*/FUTUREAI_API_TAG=$TAG/" .env
```

确认改成功：

```bash
grep '^FUTUREAI_API_TAG=' .env
```

输出应为 `FUTUREAI_API_TAG=20260915225600`。

启动新版本：

```bash
docker compose up -d
```

compose 发现镜像标签变化后会重建应用容器，数据库容器与数据卷不受影响。

> **注意**：执行 `docker compose up -d` 后有数十秒中断。旧容器收到 SIGTERM 后会先把在途请求跑完（宽限期 40 秒），这期间网关返回 502。应在低峰期执行。

> **注意**：网关不需要重启。网关的上游地址由 `resolver` 每 10 秒重新解析，容器 IP 变化会自动生效。

## 6 服务器：验证

以下命令仍在 `/home/ubuntu/stacks/futureai-api` 目录执行。确认两个容器均健康：

```bash
docker compose ps
```

`STATUS` 列出现 `healthy` 即为就绪。刚启动的 30 秒内应用可能显示 `health: starting`，等待后重查。

确认正在运行的镜像标签：

```bash
docker compose images futureai-api
```

`TAG` 列应与第 4 章设置的标签一致。

查看启动日志：

```bash
docker compose logs --tail=50 futureai-api
```

**预期看到** `using PostgreSQL as database`、`database migration started`、`FutureAI API started on port 3001`。出现 `[迁移]` 字样时，先按 [NOTES.md 第 6 章](NOTES.md#6-升级)确认该迁移是否可逆；不可逆时，回滚必须连数据库一起恢复。

经网关检查健康状态：

```bash
curl -k -s --resolve futureaiapi.com:443:127.0.0.1 https://futureaiapi.com/health
```

**预期**：`{"status":"ok"}`。

检查首页：

```bash
curl -k -s -o /dev/null -w '%{http_code}\n' --resolve futureaiapi.com:443:127.0.0.1 https://futureaiapi.com/
```

**预期**：`200`。

最后用浏览器登录，实际走一遍本次改动的功能。

## 7 回滚

回滚就是把 `.env` 里的 `FUTUREAI_API_TAG` 改回上一个标签并重启。完整步骤、标签查询命令与 schema 限制见 [NOTES.md 第 7 章](NOTES.md#7-回滚)。

若本次启动日志出现过不可逆迁移（如 `[迁移] api_keys 明文列已删除`），不能只换镜像标签，必须连数据库一起用第 3 章的备份恢复。

> **注意**：不要清理 `futureai-api` 的旧标签镜像，也不要执行 `docker image prune -a` 或 `docker system prune -a`。回滚依赖服务器上保留的历史镜像，删掉后只能重新打包。

## 8 故障排查

更新后的常见现象如下。

| 现象 | 多半是 | 处理 |
|---|---|---|
| `up -d` 报 `image "futureai-api:xxx" not found` | `.env` 里的标签与已加载的镜像不一致 | 核对第 5 章的 `grep` 输出与第 4 章的 `docker load` 输出 |
| 应用容器反复重启，日志含 `exec format error` | 镜像架构与服务器不符 | 按服务器架构重新打包，再执行第 4 至 6 章 |
| 更新后访问返回 502 | 新容器仍在启动，或启动失败 | 等容器变为 `healthy` 后重试；仍失败则查看第 6 章的启动日志 |
| 日志停在 `database migration` 或进程退出 | 迁移失败，或数据库连接失败 | 保留完整日志，用第 3 章的备份恢复后再排查 |
| `docker compose images` 仍显示旧标签 | `.env` 未改成功，或命令不在部署目录执行 | 重做第 5 章前两步后重启 |
