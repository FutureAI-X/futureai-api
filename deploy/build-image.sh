#!/usr/bin/env bash
#
# 在本机构建 FutureAI API 镜像，并导出成可直接传到服务器的压缩包。
#
# 用法:
#   ./deploy/build-image.sh                  # 默认 linux/amd64
#   ./deploy/build-image.sh linux/arm64      # 交叉编译到 arm64，无需 QEMU
#   TAG=v1.2.3 ./deploy/build-image.sh       # 指定标签，不用构建时刻
#
# 标签默认取构建时刻（20260915225600），每打一次包就是一个新标签。
# 服务器上的历史镜像因此不会被覆盖，回滚只是改 .env 里的 TOKEN_HUB_TAG。
#
# 服务器买好后先确认架构:
#   ssh <server> uname -m
#     x86_64  -> linux/amd64
#     aarch64 -> linux/arm64
#
# 镜像只在本机导出，不推送到任何 registry —— 服务器上不需要源码、
# 不需要 git 凭据、也不需要装构建工具链。
set -euo pipefail

PLATFORM="${1:-linux/amd64}"
IMAGE="${IMAGE:-token-hub}"
TAG="${TAG:-$(date +%Y%m%d%H%M%S)}"

case "$PLATFORM" in
  linux/amd64 | linux/arm64) ;;
  *)
    echo "不支持的平台: $PLATFORM（仅支持 linux/amd64 或 linux/arm64）" >&2
    exit 1
    ;;
esac

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
ARCH="${PLATFORM#linux/}"
ARCHIVE="$REPO_ROOT/$IMAGE-$TAG-$ARCH.tar.gz"

cd "$REPO_ROOT"

if ! docker info >/dev/null 2>&1; then
  echo "连不上 Docker。Docker Desktop 启动了吗？" >&2
  exit 1
fi

# 除了时间戳标签，再打一个 latest：服务器上的 .env 若还停在默认值也能直接跑起来。
# 两个标签指向同一个镜像，docker save / load 都不会多出体积。
REFS=("$IMAGE:$TAG")
[ "$TAG" = latest ] || REFS+=("$IMAGE:latest")

TAGS=()
for ref in "${REFS[@]}"; do TAGS+=(-t "$ref"); done

# 把 commit 写进镜像元数据（org.opencontainers.image.revision）。
# 时间戳标签与代码版本之间没有对应关系，出问题时无法确定「上一个版本」
# 到底是哪一版，回滚只能靠猜。
GIT_SHA="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"

# 把本机的 GOPROXY 带进构建容器。
#
# ⚠️ 构建容器不继承宿主机的 go env。不带过去的话容器会去访问
#    proxy.golang.org，在国内网络下通常不可达，构建直接失败
#    （`connect: connection refused`）。而这个坑只在缓存失效时才暴露：
#    本地缓存热着的时候构建能成功，换台机器或改了 Dockerfile 就突然不行。
#
# 设置为空串（GOPROXY= ）可退回镜像内的默认值。
GOPROXY="${GOPROXY:-$(go env GOPROXY 2>/dev/null || true)}"
BUILD_ARGS=()
[ -n "$GOPROXY" ] && BUILD_ARGS+=(--build-arg "GOPROXY=$GOPROXY")

echo "==> 构建 $IMAGE:$TAG ($PLATFORM, commit $GIT_SHA)"
[ -n "$GOPROXY" ] && echo "    模块代理: $GOPROXY"

docker buildx build --platform "$PLATFORM" \
  --build-arg "GIT_SHA=$GIT_SHA" \
  "${BUILD_ARGS[@]}" \
  "${TAGS[@]}" --load .

echo "==> 导出 $ARCHIVE"
docker save "${REFS[@]}" | gzip >"$ARCHIVE"

SIZE="$(du -h "$ARCHIVE" | cut -f1)"

cat <<EOF

完成，镜像包 $SIZE，标签 $TAG。

传到服务器并加载:
  scp "$IMAGE-$TAG-$ARCH.tar.gz" <user>@<server>:/tmp/
  ssh <user>@<server> 'gunzip -c /tmp/$IMAGE-$TAG-$ARCH.tar.gz | docker load'

然后在服务器上把 .env 指向这个标签并重启:
  cd /opt/stacks/token-hub
  sed -i 's/^TOKEN_HUB_TAG=.*/TOKEN_HUB_TAG=$TAG/' .env
  docker compose up -d

EOF
