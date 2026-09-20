#!/usr/bin/env bash
#
# 生成一张覆盖三个域名的自签证书，让网关在域名解析生效前就能启动起来。
#
# 用法:
#   ./scripts/self-signed.sh              # 已有证书则不动
#   ./scripts/self-signed.sh --force      # 强制重新生成
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

: "${FUTUREAI_API_DOMAIN:?未设置 FUTUREAI_API_DOMAIN}"

mkdir -p certs

if [ -f certs/fullchain.pem ] && [ "${1:-}" != "--force" ]; then
  echo "certs/fullchain.pem 已存在，未改动。要覆盖请加 --force"
  exit 0
fi


# ---------------------------------------------------------------------------
# 用配置文件生成，而不是 -subj / -addext
# ---------------------------------------------------------------------------
#
# 在 Windows 的 Git Bash（MSYS）里，以斜杠开头的参数会被当成 POSIX 路径
# **自动转换成 Windows 路径**：
#
#     -subj "/CN=a.com"   ->   -subj "C:/Program Files/Git/CN=a.com"
#
# openssl 收到这个东西只会报一句 `subject name is expected to be in the
# format /type0=value0/...`。更麻烦的是失败时机：openssl 先写私钥再写证书，
# 于是报错时目录里已经留下一个孤零零的 privkey.pem，网关因为找不到
# fullchain.pem 而陷入重启循环，现场看起来完全不像"参数被转义了"。
#
# 写成配置文件就没有这个问题，而且 Linux / macOS / Windows 行为完全一致。
CONF="$(mktemp)"
trap 'rm -f "$CONF"' EXIT

cat >"$CONF" <<EOF
[req]
distinguished_name = dn
x509_extensions    = v3_req
prompt             = no

[dn]
CN = ${FUTUREAI_API_DOMAIN}

[v3_req]
basicConstraints = CA:FALSE
keyUsage         = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName   = @alt_names

[alt_names]
DNS.1 = ${FUTUREAI_API_DOMAIN}
EOF


# 先清掉可能残留的半成品。上一次失败留下的 privkey.pem 会让"证书已存在"
# 的判断失效，也会让下一次失败看起来像是新问题。
rm -f certs/fullchain.pem certs/privkey.pem

echo "==> 生成自签证书（SAN: $FUTUREAI_API_DOMAIN）"

openssl req -x509 -nodes -newkey rsa:2048 -days 365 \
  -keyout certs/privkey.pem \
  -out certs/fullchain.pem \
  -config "$CONF" -extensions v3_req

chmod 600 certs/privkey.pem
chmod 644 certs/fullchain.pem


# ---------------------------------------------------------------------------
# 自检
# ---------------------------------------------------------------------------
#
# 不能只看"openssl 没报错"就完事：这一步失败的表现是**网关起不来**，
# 而网关的日志离这里很远。宁可在生成时就大声报出来。
for f in certs/fullchain.pem certs/privkey.pem; do
  [ -s "$f" ] || {
    echo "生成失败：$f 不存在或为空" >&2
    exit 1
  }
done

echo "==> 自检"
openssl x509 -in certs/fullchain.pem -noout -subject -dates -ext subjectAltName

cat <<EOF

完成。启动网关:
  docker compose up -d
  docker compose exec gateway nginx -t

用 curl 验证（-k 跳过自签证书校验；--resolve 让 SNI 和 Host 都是目标域名）:
  curl -k --resolve ${FUTUREAI_API_DOMAIN}:443:127.0.0.1 https://${FUTUREAI_API_DOMAIN}/health

域名解析生效后，换成正式证书:
  ./scripts/certbot.sh issue

EOF
