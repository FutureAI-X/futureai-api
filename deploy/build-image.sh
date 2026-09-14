#!/usr/bin/env bash
#
# 在本机构建 Token Hub 镜像，并导出成可直接传到服务器的压缩包。
#
# 用法:
#   ./deploy/build-image.sh                  # 默认 linux/amd64
#   ./deploy/build-image.sh linux/arm64      # 交叉编译到 arm64，无需 QEMU
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
TAG="${TAG:-latest}"

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

echo "==> 构建 $IMAGE:$TAG ($PLATFORM)"
docker buildx build --platform "$PLATFORM" -t "$IMAGE:$TAG" --load .

echo "==> 导出 $ARCHIVE"
docker save "$IMAGE:$TAG" | gzip >"$ARCHIVE"

SIZE="$(du -h "$ARCHIVE" | cut -f1)"

cat <<EOF

完成，镜像包 $SIZE。

传到服务器并加载:
  scp "$IMAGE-$TAG-$ARCH.tar.gz" <user>@<server>:/tmp/
  ssh <user>@<server> 'gunzip -c /tmp/$IMAGE-$TAG-$ARCH.tar.gz | docker load'

EOF
