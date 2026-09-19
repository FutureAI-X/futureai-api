#!/usr/bin/env bash
#
# 从 backup.sh 产出的备份恢复数据库。
#
# 用法（在服务器上）:
#   ./restore.sh /opt/backups/token-hub/db-2026-09-20_030000.sql.gz
#
# ⚠️ 这是破坏性操作：会用备份覆盖当前数据库。
#    脚本会二次确认，并要求先停掉应用容器（否则恢复过程中应用仍在读写，
#    可能留下不一致的数据）。
#
# 演练建议：每季度在**临时目录**里真的跑一次恢复，确认备份可用。
# 没验证过的备份等于没有备份。

set -euo pipefail

BACKUP_FILE="${1:-}"
STACK_DIR="${STACK_DIR:-/opt/stacks/token-hub}"
COMPOSE_FILE="$STACK_DIR/docker-compose.yml"
ENV_FILE="$STACK_DIR/.env"

log() { echo "[$(date '+%F %T')] $*"; }
fail() { echo "[$(date '+%F %T')] 错误: $*" >&2; exit 1; }

[ -n "$BACKUP_FILE" ] || fail "用法: $0 <备份文件.sql.gz>"
[ -f "$BACKUP_FILE" ] || fail "找不到备份文件: $BACKUP_FILE"
[ -f "$COMPOSE_FILE" ] || fail "找不到 $COMPOSE_FILE"
[ -f "$ENV_FILE" ] || fail "找不到 $ENV_FILE"

gzip -t "$BACKUP_FILE" || fail "备份文件损坏: $BACKUP_FILE"

read_env() {
  sed -n "s/^$1=//p" "$ENV_FILE" | tail -1 | tr -d "\"'"
}

PG_USER="$(read_env POSTGRES_USER)"; PG_USER="${PG_USER:-token_hub}"
PG_DB="$(read_env POSTGRES_DB)"; PG_DB="${PG_DB:-token_hub}"

log "即将用以下备份覆盖数据库 $PG_DB:"
log "  $BACKUP_FILE ($(du -h "$BACKUP_FILE" | cut -f1))"
log "  部署目录: $STACK_DIR"
echo
read -r -p "确认继续？应用容器会先被停止。输入 yes 继续: " answer
[ "$answer" = "yes" ] || { echo "已取消"; exit 0; }

log "停止应用容器（保留数据库）"
docker compose -f "$COMPOSE_FILE" stop token-hub

log "恢复数据库"
# --clean --if-exists 让 pg_dump 的产物自带 DROP，能覆盖已有的表；
# ON_ERROR_STOP 保证中途出错立即中断，而不是留下半恢复的状态。
docker compose -f "$COMPOSE_FILE" exec -T postgres \
  psql -U "$PG_USER" -d "$PG_DB" -v ON_ERROR_STOP=1 < "$BACKUP_FILE"

log "恢复完成，重新启动应用"

# ⚠️ 恢复旧数据后，库里的 schema 可能是旧版本。
# 应用启动时会跑 AutoMigrate 把缺的列/索引补上，这一步会在启动阶段完成，
# 期间服务不可用 —— 属预期行为。
docker compose -f "$COMPOSE_FILE" up -d

log "等待应用健康"
for i in $(seq 1 30); do
  if docker compose -f "$COMPOSE_FILE" exec -T token-hub \
      wget -qO- http://127.0.0.1:3001/health >/dev/null 2>&1; then
    log "应用已就绪"
    exit 0
  fi
  sleep 2
done

fail "应用在 60 秒内未就绪，请查看: docker compose -f $COMPOSE_FILE logs token-hub"
