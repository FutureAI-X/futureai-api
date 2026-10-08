# 构建镜像并在本地运行

将项目构建成镜像、以 Docker Desktop 在本地运行，也是发版前验证镜像的方式。

日常改代码见 [DEV.md](DEV.md)，部署到服务器见 [../deploy/README.md](../deploy/README.md)。

仅在本机验证时执行第 1 至 3 章即可；发版则在此基础上继续执行第 5 章。第 6 章为常用操作，第 7 章为故障排查。

## 概览

前端由 Vite 构建为静态文件后编入 Go 二进制，因此对外只需一个端口，无须额外的 Nginx 托管静态文件。镜像在 `docker images` 中显示约 54MB，导出为压缩包约 14MB。

容器与本机 `go run` 共用同一个数据库，可以同时运行。容器刻意映射到 8080 而非 3001，正是为此。

| 运行方式 | 地址 | 适用场景 |
|---|---|---|
| 容器 | http://localhost:8080 | 验证镜像、发版前的成品 |
| 本机 `go run` | http://localhost:3001 | 修改代码、调试 |

两者连接同一数据库，因此数据、账号与模型配置完全一致。

## 1 准备

### 1.1 启动 Docker Desktop

```bash
docker info
```

能打印出信息即为已启动。

### 1.2 准备 .env

`.env` 被 gitignore 排除，刚克隆的仓库中没有该文件，先确认：

```bash
ls .env
```

若不存在，由模板复制：

```bash
cp .env.example .env
```

缺失 `.env` 时 `docker compose` 会在解析配置阶段失败，报 `required variable POSTGRES_PASSWORD is missing a value`。

随后按 [DEV.md](DEV.md) 的 1.2 填好 `POSTGRES_PASSWORD`、`JWT_SECRET`、`SECRET_KEY`。后两项留空时服务会拒绝启动。

## 2 在本地运行

```bash
docker compose up -d
```

无须预先构建镜像：compose 文件含 `build: .`，镜像不存在时会自动构建后启动。

会启动两个容器。

| 容器 | 端口 | 说明 |
|---|---|---|
| `futureai-api` | `127.0.0.1:8080` → 3001 | 应用 |
| `futureai-api-postgres` | `127.0.0.1:5432` | 数据库，与本机开发共用 |

等待其就绪：

```bash
docker compose ps
```

`STATUS` 列出现 `healthy` 即为就绪，随后访问 http://localhost:8080。

> **注意**：这两个端口仅绑定 `127.0.0.1`。该编排供本机开发使用，不得运行在公网主机上，否则等同于将管理后台对外开放。

> **注意**：镜像已存在时，`up -d` 会直接复用，不会重新构建。修改代码后见第 4 章。

## 3 验证运行结果

以下四项检查建议在发版前执行。

健康检查：

```bash
curl -s http://localhost:8080/health
```

预期返回 `{"status":"ok"}`。

首页：

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/
```

预期返回 `200`。有页面即说明内嵌前端正常。

SPA 回退：

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/dashboard
```

预期返回 `200`。`/dashboard` 在服务端没有对应文件，须回退到 `index.html`。

API 的 404：

```bash
curl -s http://localhost:8080/api/nope
```

预期返回 `{"error":{"message":"Not found",...}}`，而不是 HTML。

## 4 修改代码之后

```bash
docker compose up -d --build
```

`--build` 强制重新构建镜像后再启动，无须执行 `build-image.sh`。

> **注意**：`--build` 不可省略。不加时 `up -d` 会复用已有镜像，运行的仍是旧代码。

## 5 构建镜像（发版用）

```bash
bash deploy/build-image.sh linux/amd64
```

参数是**目标平台**，不是本机架构：

- 在 Windows / macOS 上本地测试用 `linux/amd64`，Docker Desktop 运行的是 Linux 虚拟机；
- 服务器为 x86 用 `linux/amd64`，为 arm 用 `linux/arm64`。

执行后会打印：

```
完成，镜像包 14M，标签 20260915225600。
```

产出两项：镜像 `futureai-api:<标签>`，以及仓库根目录下的 `futureai-api-<标签>-amd64.tar.gz`，后者用于传输到服务器。

### 5.1 镜像标签

标签默认取**构建时刻**（形如 `20260915225600`），不是 `latest`。每次构建生成新标签，服务器上的历史镜像因此不被覆盖，回滚即切换标签。

脚本会附带构建一个指向同一镜像的 `latest`，便于 `.env` 仍为默认值的机器直接运行，但回滚时不应使用它，它只代表最后一次 `docker load` 导入的版本。

## 6 常用操作

常用命令如下。

| 我要… | 命令 |
|---|---|
| 查看应用日志 | `docker compose logs -f futureai-api`（`Ctrl+C` 退出） |
| 进入容器 | `docker exec -it futureai-api sh`（alpine 基础镜像，自带 shell） |
| 停止容器 | `docker compose down`（数据保留在卷中） |
| 确认镜像架构 | `docker image inspect futureai-api:latest --format '{{.Architecture}}'` |
| 查看镜像大小 | `docker images futureai-api` |

## 7 故障排查

### 7.1 构建时报 `failed to fetch anonymous token`

网络受限所致。先在本机手动拉取三个基础镜像：

```bash
docker pull node:22-alpine
```

```bash
docker pull golang:1.27-alpine
```

```bash
docker pull alpine:3.22
```

随后重新构建。此后若修改 Dockerfile 中的基础镜像 tag，也需先手动拉取一次。

> 原因是 `docker pull` 与构建器不走同一条网络栈：挂了系统代理时 `docker pull` 可能正常，但构建器解析基础镜像元数据时仍直连 `auth.docker.io`，因而超时。

### 7.2 容器已启动，页面返回 503，日志提示「前端资源未构建」

镜像中的 `web/dist` 为空。正常构建流程不会出现，多数情况是 Dockerfile 顺序被改动，`COPY --from=web /build/web/dist ./web/dist` 必须排在 `go build` 之前。

先查看启动日志确认：

```bash
docker compose logs futureai-api | head -20
```

### 7.3 日志提示数据库认证失败

容器之间使用服务名 `postgres:5432`，不是宿主机的 `127.0.0.1:5432` 映射。

修改过 `POSTGRES_PASSWORD` 而数据卷仍以旧密码初始化时，会出现认证失败。清空重建，**将丢失数据**：

```bash
docker compose down -v
```

```bash
docker compose up -d
```

### 7.4 更换端口

修改 [../docker-compose.yml](../docker-compose.yml) 中 `futureai-api` 服务 `ports` 左侧的数字即可。右侧的 `3001` 是容器内端口，不要改动。

## 下一步

- 部署到服务器 → [../deploy/README.md](../deploy/README.md)
- 上线前自检、升级回滚备份、排查 → [../deploy/NOTES.md](../deploy/NOTES.md)
