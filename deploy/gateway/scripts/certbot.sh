#!/usr/bin/env bash
#
# 用 certbot 为本项目的域名申请证书，并部署给网关使用。
#
# 用法:
#   ./scripts/certbot.sh issue    # 首次签发
#   ./scripts/certbot.sh renew    # 续期（挂 cron，见 deploy/README.md）
#
# 前置条件（缺一不可，否则 HTTP-01 校验必然失败）:
#   1. FUTUREAI_API_DOMAIN 的 A 记录已指向本机公网 IP，且已生效
#   2. 80 端口可从公网访问（安全组 / 防火墙都要放行）
#   3. 网关已经在运行（它负责响应 /.well-known/acme-challenge/）
#
# 域名取自 .env 的 FUTUREAI_API_DOMAIN，写死在一处：
#   以后往这台机器上加服务时，各自用自己的域名单独签一张证书即可，
#   不要合成一张 SAN 证书 —— 一个域名要改动就得把全部服务一起重签。
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

if [ ! -f .env ]; then
  echo "缺少 .env。先执行: cp .env.example .env 并填写域名" >&2
  exit 1
fi

# shellcheck disable=SC1091
set -a && source .env && set +a

: "${FUTUREAI_API_DOMAIN:?未设置 FUTUREAI_API_DOMAIN}"
: "${ACME_EMAIL:?未设置 ACME_EMAIL}"

# 证书以第一个域名为名
PRIMARY="$FUTUREAI_API_DOMAIN"
LIVE_DIR="certbot/conf/live/$PRIMARY"

mkdir -p certbot/conf certbot/www certs

run_certbot() {
  docker run --rm \
    -v "$PWD/certbot/conf:/etc/letsencrypt" \
    -v "$PWD/certbot/www:/var/www/certbot" \
    certbot/certbot "$@"
}

# 把续期后的证书复制到网关挂载的目录，并让 nginx 重新加载。
# 用 cp -L 解开 Let's Encrypt 的符号链接（live/ 下是指向 archive/ 的链接，
# 直接复制链接会得到一对断掉的路径）。
deploy_certs() {
  if [ ! -f "$LIVE_DIR/fullchain.pem" ]; then
    echo "找不到 $LIVE_DIR/fullchain.pem，证书尚未签发" >&2
    return 1
  fi

  cp -L "$LIVE_DIR/fullchain.pem" certs/fullchain.pem
  cp -L "$LIVE_DIR/privkey.pem" certs/privkey.pem
  chmod 644 certs/fullchain.pem
  chmod 600 certs/privkey.pem

  echo "==> 证书已更新，重载网关"
  docker compose exec gateway nginx -s reload
}

case "${1:-}" in
  issue)
    echo "==> 为 $FUTUREAI_API_DOMAIN 申请证书"
    run_certbot certonly --webroot -w /var/www/certbot \
      -d "$FUTUREAI_API_DOMAIN" \
      --email "$ACME_EMAIL" \
      --agree-tos --no-eff-email

    deploy_certs

    echo
    echo "==> 完成。可以去掉自签证书的 -k 验证："
    echo "    curl -I https://$FUTUREAI_API_DOMAIN/health"
    ;;

  renew)
    echo "==> 检查续期"
    # certbot 只在确实临近到期时才真正续期；未续期时返回 0 且不改动文件，
    # 下面的复制与 reload 因此是无害的空操作。
    run_certbot renew --webroot -w /var/www/certbot
    # 尚未签发过正式证书（还在自签阶段）不是错误：deploy_certs 会因为找不到
    # live 目录而返回 1。直接让它冒出去，这条 cron 就会从挂上那天起每天记一次
    # 失败，把真正该看的报错淹掉。
    deploy_certs || echo "==> 尚未签发正式证书（自签阶段），跳过部署"
    ;;

  *)
    echo "用法: $0 {issue|renew}" >&2
    exit 1
    ;;
esac
