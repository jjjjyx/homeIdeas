# Windows 本地/小主机上的每日备份
# 注册计划任务（管理员 PowerShell 执行一次）：
#   schtasks /Create /TN "FamilyLedgerBackup" /SC DAILY /ST 03:00 /RU SYSTEM /TR "powershell -NoProfile -ExecutionPolicy Bypass -File G:\26-workspace\homeIdeas\server\deploy\backup.ps1"

$ErrorActionPreference = 'Stop'

$appDir = 'G:\26-workspace\homeIdeas\server'
$db = Join-Path $appDir 'data\app.db'
$backupDir = Join-Path $appDir 'data\backup'
$keep = 14

if (-not (Test-Path $db)) {
    Write-Host "[$(Get-Date -Format s)] 数据库不存在，跳过：$db"
    exit 0
}

Write-Host "[$(Get-Date -Format s)] 开始备份 $db"
& (Join-Path $appDir 'bin\ledger-backup.exe') -db $db -dir $backupDir -keep $keep
if ($LASTEXITCODE -ne 0) {
    Write-Host "[$(Get-Date -Format s)] 备份失败，退出码 $LASTEXITCODE"
    exit 1
}

$size = (Get-ChildItem $backupDir -File | Measure-Object Length -Sum).Sum / 1MB
Write-Host ("[$(Get-Date -Format s)] 完成，备份目录 {0:N1} MB" -f $size)
