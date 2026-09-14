#!/usr/bin/env bash
#
# 生成一张覆盖三个域名的自签证书，让网关在域名解析生效前就能启动起来。
#
# 用法:
#   ./scripts/self-signed.sh              # 已有证书则不动
#   ./scripts/self-signed.sh --force      # 强制覆盖
#
# 为什么需要这一步:
#   nginx 的 ssl_certificate 指向的文件不存在时，**进程会直接启动失败**
#   （不是某个域名 502，是整个网关起不来）。所以在正式证书签发之前，
#   必须先放一对证书文件在这里占位。
#
#   浏览器会提示证书不受信任 —— 这是预期的。这一阶段用
#   curl -k 或临时改 hosts 来验证链路即可，正式证书签发后自然消失。
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

if [ ! -f .env ]; then
  echo "缺少 .env。先执行: cp .env.example .env 并填写域名" >&2
  exit 1
fi

# shellcheck disable=SC1091
set -a && source .env && set +a

: "${TOKEN_HUB_DOMAIN:?未设置 TOKEN_HUB_DOMAIN}"
: "${NEW_API_DOMAIN:?未设置 NEW_API_DOMAIN}"
: "${SUB2API_DOMAIN:?未设置 SUB2API_DOMAIN}"

mkdir -p certs

if [ -f certs/fullchain.pem ] && [ "${1:-}" != "--force" ]; then
  echo "certs/fullchain.pem 已存在，未改动。要覆盖请加 --force"
  exit 0
fi

echo "==> 生成自签证书（SAN: $TOKEN_HUB_DOMAIN, $NEW_API_DOMAIN, $SUB2API_DOMAIN）"

openssl req -x509 -nodes -newkey rsa:2048 -days 365 \
  -keyout certs/privkey.pem \
  -out certs/fullchain.pem \
  -subj "/CN=${TOKEN_HUB_DOMAIN}" \
  -addext "subjectAltName=DNS:${TOKEN_HUB_DOMAIN},DNS:${NEW_API_DOMAIN},DNS:${SUB2API_DOMAIN}"

chmod 600 certs/privkey.pem
chmod 644 certs/fullchain.pem

cat <<EOF

完成。启动网关:
  docker compose up -d
  docker compose exec gateway nginx -t

用 curl 验证（-k 跳过自签证书校验）:
  curl -k -H "Host: ${TOKEN_HUB_DOMAIN}" https://127.0.0.1/health

域名解析生效后，换成正式证书:
  ./scripts/certbot.sh issue

EOF
