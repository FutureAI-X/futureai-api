# 打包镜像 & 用 Docker 在本地跑

日常改代码不用这个 —— 那是 [DEVELOPMENT.md](DEVELOPMENT.md)。
这里是把项目打包成镜像、用 Docker Desktop 跑起来的流程，也是发版前验证镜像的方式。

生产服务器上的部署是另一套（带 Nginx 网关、三个服务共存），见 [deploy/README.md](../deploy/README.md)。

---

## 镜像是什么形态

**一个容器里同时装着前端和后端。**

前端由 Vite 构建成静态文件，再通过 `go:embed` 编进 Go 二进制（见 [webui/](../webui/)）。
所以对外只需要一个端口，不需要额外的 Nginx 托管静态文件 —— 与 new-api、sub2api 的形态一致。

最终镜像约 **14MB**（alpine + 静态编译的 Go 二进制）。

---

## 1. 构建镜像

确认 Docker Desktop 已启动，然后：

```bash
./deploy/build-image.sh linux/amd64        # 或 linux/arm64
```

这个参数是**目标平台**，不是你这台机器的架构：

- 在 Windows 上本地测试 → `linux/amd64`（Docker Desktop 跑的是 Linux 虚拟机）
- 服务器是 x86 → `linux/amd64`；服务器是 arm → `linux/arm64`

脚本做两件事：构建出镜像 `token-hub:latest`，并导出
`token-hub-latest-<arch>.tar.gz`（用于传到服务器）。

> 交叉编译不需要 QEMU。Dockerfile 的两个构建阶段都固定在 `$BUILDPLATFORM` 上执行 ——
> 前端产物与架构无关，Go 用 `GOARCH` 原生交叉编译。所以在 x86 机器上构建 arm64
> 镜像同样很快。

### ⚠️ 网络受限时的坑

镜像需要 `node:22-alpine`、`golang:1.27-alpine`、`alpine:3.22` 三个基础镜像。

**`docker pull` 和构建器走的不是同一条网络栈。** 挂了系统代理时，
`docker pull` 可能正常，但构建器解析基础镜像元数据时仍会直连 `auth.docker.io`，
超时报 `failed to fetch anonymous token`。

绕过办法 —— **先手动把基础镜像拉到本地**，构建时就不用再去认证了：

```bash
docker pull node:22-alpine
docker pull golang:1.27-alpine
docker pull alpine:3.22
```

以后改了 Dockerfile 里的基础镜像 tag，也要先 pull 一次。

---

## 2. 跑起来

```bash
docker compose up -d
```

根目录的 [docker-compose.yml](../docker-compose.yml) 会起两个容器：

| 容器 | 端口 | 说明 |
|---|---|---|
| `token-hub` | **8080** → 3001 | 应用 |
| `token-hub-postgres` | `127.0.0.1:5432` | 开发数据库 |

浏览器打开 **http://localhost:8080**。

容器里的应用连的是**你本地开发用的那个数据库**，所以数据、账号、模型配置
都和你 `go run main.go` 时看到的完全一样。

### 为什么映射到 8080 而不是 3001

把 3001 留给本机 `go run main.go`。这样两种跑法可以同时开着，共用同一个库：

| 跑法 | 地址 | 什么时候用 |
|---|---|---|
| 容器 | http://localhost:8080 | 验证镜像、发版前的成品 |
| 本机 `go run` | http://localhost:3001 | 改代码、热调试 |

---

## 3. 验证

```bash
curl -s http://localhost:8080/health                                        # {"status":"ok"}
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/             # 200
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/dashboard    # 200（SPA 回退）
curl -s http://localhost:8080/api/nope                                      # JSON 404，不是 HTML
```

浏览器打开 http://localhost:8080 能看到页面，就说明内嵌前端正常。

---

## 4. 改了代码之后

```bash
./deploy/build-image.sh linux/amd64 && docker compose up -d
```

---

## 常用操作

```bash
docker compose logs -f token-hub        # 看日志
docker exec -it token-hub sh            # 进容器（alpine 基础镜像，有 shell）
docker compose down                     # 停掉，数据卷保留
docker image inspect token-hub:latest --format '{{.Architecture}}'   # 确认镜像架构
docker images token-hub                 # 看镜像大小
```

---

## 常见问题

### 容器起来了，但页面返回 503，日志说「前端资源未构建」

镜像里的 `web/dist` 是空的。正常构建流程不会出现，多半是 Dockerfile 的顺序被改坏了 ——
`COPY --from=web /build/web/dist ./web/dist` 必须排在 `go build` **之前**。

### 应用连不上数据库

```bash
docker compose logs token-hub | head -20
```

容器之间走的是服务名 `postgres:5432`，**不是**宿主机那条 `127.0.0.1:5432` 映射。
如果改了 `POSTGRES_PASSWORD` 而数据卷是用旧密码初始化的，就会认证失败。
清空重建（**会丢数据**）：

```bash
docker compose down -v && docker compose up -d
```

### 想换成别的端口

改 [docker-compose.yml](../docker-compose.yml) 里 `token-hub` 服务的 `ports`
左侧那个数字即可，右侧的 `3001` 是容器内端口，不要动。

---

## 下一步

- 部署到服务器 → [deploy/README.md](../deploy/README.md)
- 上线前自检、升级回滚备份、排查 → [deploy/NOTES.md](../deploy/NOTES.md)
