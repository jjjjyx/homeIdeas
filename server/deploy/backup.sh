#!/usr/bin/env bash
# 每日备份：VACUUM INTO 生成一致快照，保留最近 14 份。
# crontab:  0 3 * * * /opt/family-ledger/deploy/backup.sh >> /var/log/ledger-backup.log 2>&1
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/family-ledger}"
DB="${DB:-$APP_DIR/data/app.db}"
BACKUP_DIR="${BACKUP_DIR:-$APP_DIR/data/backup}"
KEEP="${KEEP:-14}"

echo "[$(date '+%F %T')] 开始备份 $DB"
"$APP_DIR/ledger-backup" -db "$DB" -dir "$BACKUP_DIR" -keep "$KEEP"
echo "[$(date '+%F %T')] 完成，目录占用：$(du -sh "$BACKUP_DIR" | cut -f1)"

# 如需异地备份，取消下面注释（示例：rsync 到另一台机器）
# rsync -a --delete "$BACKUP_DIR/" backup@backup-host:/data/ledger-backup/
