#!/usr/bin/env bash
#
# 备份 Token Hub 的数据库与配置。
#
# 用法（在服务器上，从仓库的 deploy/ 目录或在服务器上的任意位置调用）:
#   ./backup.sh                 # 备份到 /opt/backups/token-hub
#   BACKUP_DIR=/data/bak ./backup.sh
#   KEEP=30 ./backup.sh         # 保留最近 30 份（默认 14）
#
# 挂 cron（root 用户，因为要读 .env 并写 /opt）:
#   sudo crontab -e
#   30 3 * * * /opt/stacks/token-hub/backup.sh >> /var/log/token-hub-backup.log 2>&1
#
# ⚠️ 没验证过的备份等于没有备份。改完 schema 或升级之后，
#    请按 NOTES.md 的步骤实际恢复一次（见 restore.sh）。

set -euo pipefail

STACK_DIR="${STACK_DIR:-/opt/stacks/token-hub}"
BACKUP_DIR="${BACKUP_DIR:-/opt/backups/token-hub}"
KEEP="${KEEP:-14}"
COMPOSE_FILE="$STACK_DIR/docker-compose.yml"
ENV_FILE="$STACK_DIR/.env"

log() { echo "[$(date '+%F %T')] $*"; }
fail() { echo "[$(date '+%F %T')] 错误: $*" >&2; exit 1; }

[ -f "$COMPOSE_FILE" ] || fail "找不到 $COMPOSE_FILE（用 STACK_DIR 指定部署目录）"
[ -f "$ENV_FILE" ] || fail "找不到 $ENV_FILE"

# 从 .env 读取，不硬编码用户名/库名——它们本来就是可配置的，
# 写死会在改过配置的部署上静默备份错误的库（或直接报错）。
# 只取需要的三个键，不 source 整个文件：.env 里的值可能包含 shell 特殊字符。
read_env() {
  sed -n "s/^$1=//p" "$ENV_FILE" | tail -1 | tr -d "\"'"
}

PG_USER="$(read_env POSTGRES_USER)"; PG_USER="${PG_USER:-token_hub}"
PG_DB="$(read_env POSTGRES_DB)"; PG_DB="${PG_DB:-token_hub}"

# mkdir -p 是必须的：没有它时重定向会失败，而 cron 里的失败
# 只会进 cron 邮件，表现是「以为有备份，其实一个都没有」。
mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"

STAMP="$(date +%F_%H%M%S)"
DB_FILE="$BACKUP_DIR/db-$STAMP.sql.gz"
ENV_FILE_BAK="$BACKUP_DIR/env-$STAMP.backup"

log "备份数据库 $PG_DB（用户 $PG_USER）→ $DB_FILE"
docker compose -f "$COMPOSE_FILE" exec -T postgres \
  pg_dump -U "$PG_USER" -d "$PG_DB" --clean --if-exists \
  | gzip > "$DB_FILE"

# 校验产物非空：pg_dump 失败时管道仍可能产出一个空的 .gz，
# 而「看起来成功了」的空备份比没有备份更危险。
[ -s "$DB_FILE" ] || fail "备份文件为空，pg_dump 可能失败了"
gzip -t "$DB_FILE" || fail "备份文件损坏: $DB_FILE"

chmod 600 "$DB_FILE"

# .env 必须一起备份：SECRET_KEY 丢了，库里所有供应商 API Key 就永远解不开，
# 数据库恢复得再好也没用。
cp "$ENV_FILE" "$ENV_FILE_BAK"
chmod 600 "$ENV_FILE_BAK"

log "已备份: $(du -h "$DB_FILE" | cut -f1) 数据库 + 配置"

# 轮转：按时间排序保留最近 KEEP 份
for pattern in 'db-*.sql.gz' 'env-*.backup'; do
  # shellcheck disable=SC2012
  ls -1t "$BACKUP_DIR"/$pattern 2>/dev/null \
    | tail -n +$((KEEP + 1)) \
    | while read -r old; do
        rm -f "$old"
        log "已清理旧备份: $(basename "$old")"
      done
done

log "完成。当前保留 $(ls -1 "$BACKUP_DIR"/db-*.sql.gz 2>/dev/null | wc -l) 份数据库备份"
