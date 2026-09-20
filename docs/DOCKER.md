# 打包镜像 & 用 Docker 在本地跑

日常改代码看 [DEVELOPMENT.md](DEVELOPMENT.md)。
部署到服务器看 [../deploy/README.md](../deploy/README.md)。

**这份讲**：把项目打包成镜像、用 Docker Desktop 跑起来 —— 也是发版前验证镜像的方式。

**先知道两件事**

**① 一个容器里同时装着前端和后端。**

前端由 Vite 构建成静态文件，再通过 `go:embed` 编进 Go 二进制
（见 [webui/webui.go](../webui/webui.go)），所以对外只需要一个端口，
不需要额外的 Nginx 托管静态文件 —— 与 new-api、sub2api 的形态一致。

最终镜像 `docker images` 显示约 **54MB**，导出成压缩包约 **14MB**。

**② 两种跑法共用同一个数据库，可以同时开着，端口不冲突。**

| 跑法 | 地址 | 什么时候用 |
|---|---|---|
| 容器 | http://localhost:8080 | 验证镜像、发版前的成品 |
| 本机 `go run` | http://localhost:3001 | 改代码、热调试 |

容器刻意映射到 8080 而不是 3001，就是为了这一点。连的是同一个库，
所以数据、账号、模型配置完全一样。

---

## 一 准备

### 1.1 启动 Docker Desktop

```bash
docker info
```

能打印出信息就是启动好了。

### 1.2 确认 .env 存在

```bash
ls .env
```

### 1.3 没有的话从模板复制

```bash
cp .env.example .env
```

`.env` 被 gitignore，干净 clone 上没有这个文件；缺了它 `docker compose`
会直接报 `env file ... not found`。

### 1.4 填好三处密钥

按 [DEVELOPMENT.md](DEVELOPMENT.md) 的 3.2 ~ 3.5 填
`POSTGRES_PASSWORD` / `JWT_SECRET` / `SECRET_KEY`。

后两项留空的话服务会拒绝启动 —— 这是刻意的 fail-closed。

---

## 二 构建镜像

**两种场合，两种做法 —— 先看你要哪个：**

| 你要做的事 | 怎么做 |
|---|---|
| 只在本机 Docker Desktop 跑起来验证 | **跳过本节**。第 3.1 步的 `docker compose up -d` 会在镜像不存在时自动构建 |
| 要发版、要把镜像传服务器 | 跑 2.1，它会顺带导出 `.tar.gz` |

### 2.1 构建并导出（发版用）

```bash
bash deploy/build-image.sh linux/amd64
```

### 2.2 理解平台参数

参数是**目标平台**，不是你这台机器的架构：

- 在 Windows / macOS 上本地测试 → `linux/amd64`
  （Docker Desktop 跑的是 Linux 虚拟机）
- 服务器是 x86 → `linux/amd64`；服务器是 arm → `linux/arm64`

### 2.3 看构建结果

```
完成，镜像包 14M，标签 20260915225600。
```

得到两样东西：镜像 `futureai-api:<标签>`，以及仓库根目录下的
`futureai-api-<标签>-amd64.tar.gz`（用于传到服务器）。

### 2.4 关于标签

标签默认是**构建时刻**（形如 `20260915225600`），不是 `latest`。

每次打包都是一个新标签，服务器上的历史镜像因此不会被覆盖，**回滚就是换标签**。
脚本会**附带**打一个 `latest` 指向同一个镜像（方便 `.env` 还是默认值的机器直接跑），
但回滚时不要用它 —— 它只代表最后一次 `docker load` 进来的版本。

### 2.5 为什么必须走 buildx

脚本已经这么做了。要构建 **arm64** 镜像只能走它：Dockerfile 里
`TARGETARCH` 的默认值是 `amd64`，直接 `docker build .` 只会得到 amd64，
而错误要到服务器上才暴露成 `exec format error`。

> 交叉编译不需要 QEMU。Dockerfile 的两个构建阶段都固定在 `$BUILDPLATFORM` 上执行 ——
> 前端产物与架构无关，Go 用 `GOARCH` 原生交叉编译。所以在 x86 机器上构建 arm64
> 镜像同样很快。

### 2.6 网络受限时报 `failed to fetch anonymous token` 怎么办

**在本机先手动拉三个基础镜像**：

```bash
docker pull node:22-alpine
```

```bash
docker pull golang:1.27-alpine
```

```bash
docker pull alpine:3.22
```

再重新跑 2.1（只想本机跑的话，重跑 3.1）。
以后改了 Dockerfile 里的基础镜像 tag，也要先 pull 一次。

> **为什么**：`docker pull` 和构建器走的不是同一条网络栈。挂了系统代理时
> `docker pull` 可能正常，但构建器解析基础镜像元数据时仍会直连 `auth.docker.io`，
> 于是超时。

---

## 三 把容器跑起来

### 3.1 起容器

```bash
docker compose up -d
```

**第一次跑不用先构建镜像** —— compose 文件里写了 `build: .`（见
[../docker-compose.yml](../docker-compose.yml)），镜像不存在时它会自己构建再启动。

⚠️ 但镜像**已经存在**时，`up -d` 会直接拿来用，**不会**重新构建。
改了代码要重启见 5.1。

### 3.2 知道起了哪两个容器

根目录的 [../docker-compose.yml](../docker-compose.yml) 会起两个：

| 容器 | 端口 | 说明 |
|---|---|---|
| `futureai-api` | `127.0.0.1:8080` → 3001 | 应用 |
| `futureai-api-postgres` | `127.0.0.1:5432` | 数据库（和你本机开发用的是同一个） |

### 3.3 等它变成 healthy

```bash
docker compose ps
```

### 3.4 打开页面

浏览器访问 **http://localhost:8080**。

### 3.5 ⚠️ 这两个端口只绑在 127.0.0.1

这套编排是给本机开发用的，**别跑在公网机器上** —— 那等于把管理后台敞开。

---

## 四 验证镜像（发版前照着跑一遍）

### 4.1 健康检查

```bash
curl -s http://localhost:8080/health
```

预期 `{"status":"ok"}`。

### 4.2 首页

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/
```

预期 `200`。能看到页面就说明内嵌前端正常。

### 4.3 SPA 回退

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/dashboard
```

预期 `200` —— `/dashboard` 在服务端没有对应文件，必须回退到 `index.html`。

### 4.4 API 路径的 404 必须是 JSON

```bash
curl -s http://localhost:8080/api/nope
```

预期 `{"error":{"message":"Not found",...}}`，**不是** HTML。

---

## 五 改了代码之后

### 5.1 重新构建再重启

```bash
docker compose up -d --build
```

`--build` 强制重新构建镜像，然后再启动 —— 不用跑 `build-image.sh`。

⚠️ **`--build` 不能省**：不加的话 `up -d` 会直接复用已有镜像，跑的仍是旧代码，
页面怎么刷新都没变化。

> 依赖没变时很快：`npm ci` 和 `go mod download` 那两层走的是构建缓存，
> 只有源码改动之后的层会重跑。

---

## 六 常用操作

### 6.1 看应用日志

```bash
docker compose logs -f futureai-api
```

`Ctrl+C` 退出。

### 6.2 进容器

```bash
docker exec -it futureai-api sh
```

alpine 基础镜像，自带 shell。

### 6.3 停掉容器

```bash
docker compose down
```

数据保留在卷里。

### 6.4 确认镜像架构

```bash
docker image inspect futureai-api:latest --format '{{.Architecture}}'
```

### 6.5 看镜像大小

```bash
docker images futureai-api
```

---

## 七 出问题时的排查

### 7.1 现象：容器起来了，页面 503，日志说「前端资源未构建」

镜像里的 `web/dist` 是空的。正常构建流程不会出现，多半是 Dockerfile 的顺序被改坏了 ——
`COPY --from=web /build/web/dist ./web/dist` 必须排在 `go build` **之前**。

### 7.2 看应用启动日志

```bash
docker compose logs futureai-api | head -20
```

### 7.3 现象：日志说数据库认证失败

容器之间走的是服务名 `postgres:5432`，**不是**宿主机那条 `127.0.0.1:5432` 映射。

改过 `POSTGRES_PASSWORD`、而数据卷是用旧密码初始化的，就会认证失败。

### 7.4 清空重建（**会丢数据**）

```bash
docker compose down -v && docker compose up -d
```

### 7.5 想换端口

改 [../docker-compose.yml](../docker-compose.yml) 里 `futureai-api` 服务
`ports` 左侧那个数字即可。右侧的 `3001` 是容器内端口，不要动。

---

## 下一步

- 部署到服务器 → [../deploy/README.md](../deploy/README.md)
- 上线前自检、升级回滚备份、排查 → [../deploy/NOTES.md](../deploy/NOTES.md)
