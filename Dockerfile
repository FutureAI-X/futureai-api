# ============================================================================
# Token Hub — 生产镜像
#
# 三段式：前端构建 → 后端编译 → 运行时。
# 前端产物通过 go:embed 编进二进制，因此最终镜像里同时装着前端页面与 API，
# 对外只需要暴露一个端口 —— 与 new-api、sub2api 的形态一致。
#
# 交叉编译不需要 QEMU：
#   两个构建阶段都固定在 $BUILDPLATFORM 上执行。前端产物是纯静态文件，
#   与目标架构无关；Go 用 GOARCH 原生交叉编译。因此在 x86 开发机上构建
#   arm64 镜像时，不会有任何指令模拟开销。
#
# 刻意不写 `# syntax=docker/dockerfile:1`：
# 那行会让构建额外去 Docker Hub 拉一个 frontend 镜像，而本文件没有用到
# 任何它独有的特性（$BUILDPLATFORM、TARGETARCH、多阶段 COPY 都是内置语法）。
# 少一个网络依赖，在网络受限的环境里少一个失败点。
#
# 构建（推荐用脚本，它会顺带导出镜像文件）：
#   ./deploy/build-image.sh linux/amd64
#   ./deploy/build-image.sh linux/arm64
# ============================================================================


# ---------------------------------------------------------------------------
# 阶段 1：前端
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM node:22-alpine AS web

WORKDIR /build/web

# 先只拷依赖清单，让 npm ci 这一层能被缓存——源码改动不会导致重装依赖。
COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build


# ---------------------------------------------------------------------------
# 阶段 2：后端
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build

# buildx 自动注入的目标平台信息
ARG TARGETOS
ARG TARGETARCH

WORKDIR /build

# 同样先只拷依赖清单，让 go mod download 这一层能被缓存
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# ⚠️ 顺序关键：必须在 COPY . . 之后执行。
# 因为 .dockerignore 排除了宿主机上的 web/dist（它可能已经过期），
# Go 的 //go:embed all:web/dist 需要在这里拿到真正刚构建出来的产物，
# 否则编译直接失败。
COPY --from=web /build/web/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/token-hub .


# ---------------------------------------------------------------------------
# 阶段 3：运行时
#
# 选 alpine 而不是 distroless：镜像大十几 MB，但保留了 shell，
# 出问题时能 docker exec 进去用 curl/dig 排查。对首次部署更划算。
# ---------------------------------------------------------------------------
FROM alpine:3.22

# ca-certificates：调用上游供应商 API 需要信任根证书
# tzdata：        日志时间戳按本地时区输出
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 -h /app appuser \
    && mkdir -p /app \
    && chown -R 10001:10001 /app

COPY --from=build /out/token-hub /usr/local/bin/token-hub

# 以非 root 运行。注意这带来一个副作用：程序把 root 初始密码写到工作目录
# 时会因权限不足而只记一条日志。容器部署请改用 INITIAL_ROOT_PASSWORD
# 环境变量传入初始密码，见 deploy/token-hub/.env.example。
USER 10001:10001
WORKDIR /app

EXPOSE 3001

ENTRYPOINT ["/usr/local/bin/token-hub"]
