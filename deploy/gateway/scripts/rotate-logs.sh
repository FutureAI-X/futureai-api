#!/usr/bin/env bash
#
# 轮转网关日志（挂 cron，见 deploy/README.md 第 9.1 步）。
#
# 用法:
#   ./scripts/rotate-logs.sh                # 归档并保留 14 天
#   KEEP_DAYS=30 ./scripts/rotate-logs.sh   # 保留 30 天
#
# 为什么不是 `find ... -name '*.log' -mtime +14 -delete`:
#   nginx 一直持有 /var/log/nginx/*.log 的打开句柄，而这些文件边写边更新
#   mtime —— 活动日志的 mtime 永远是「刚刚」，`-mtime +14` 一条也匹配不到。
#   于是那条 cron 实际什么也没删，日志照样无限增长直到写满磁盘（与数据库同盘）。
#   删掉正在被写入的文件也只是解除目录项，inode 仍被 nginx 占着：
#   空间要等 nginx 重新打开日志才释放，在此之前日志还会继续写进那个已不可见的文件。
#
# 因此这里按正确顺序做三件事：改名归档 → 让 nginx 重开日志 → 删除过期归档。
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

COMPOSE_FILE="$PWD/docker-compose.yml"
LOG_DIR="${LOG_DIR:-$PWD/logs}"
KEEP_DAYS="${KEEP_DAYS:-14}"

log() { echo "[$(date '+%F %T')] $*"; }

[ -d "$LOG_DIR" ] || { echo "找不到日志目录: $LOG_DIR" >&2; exit 1; }
[ -f "$COMPOSE_FILE" ] || { echo "找不到 $COMPOSE_FILE" >&2; exit 1; }

STAMP="$(date +%F_%H%M%S)"
rotated=0

# 逐个改名。归档名统一是 <原名>.<时间戳>，与活动日志的 *.log 区分开，
# 下面的清理只匹配归档，不会误删正在写入的那几个。
for f in "$LOG_DIR"/*.log; do
  [ -f "$f" ] || continue
  mv "$f" "$f.$STAMP"
  rotated=$((rotated + 1))
done

status=0
if [ "$rotated" -gt 0 ]; then
  # 不重开的话，nginx 会继续写进刚刚改名的那个 inode：
  # 新的 access.log 永远不出现，而归档文件在持续变大 —— 看起来像是没轮转成功。
  # 这一步失败必须报出来，但不要因此跳过下面的清理：否则修好 nginx 之前，
  # 归档只会越堆越多。
  if docker compose -f "$COMPOSE_FILE" exec -T gateway nginx -s reopen; then
    log "已轮转 $rotated 个日志文件，并通知 nginx 重新打开"
  else
    log "错误: 通知 nginx 重新打开日志失败 —— 它可能仍在往已归档的文件里写。请检查网关容器是否在运行"
    status=1
  fi
else
  log "没有需要轮转的日志"
fi

find "$LOG_DIR" -maxdepth 1 -type f -name '*.log.*' -mtime +"$KEEP_DAYS" -delete
log "已清理 $KEEP_DAYS 天前的归档"

exit "$status"
