# 一条命令产出可部署的二进制（管理后台静态资源会打进 Go 二进制）
#
#   .\build-release.ps1                    # 默认 linux/amd64
#   .\build-release.ps1 -GOOS windows      # 本机 Windows 版
param(
    [string]$GOOS = 'linux',
    [string]$GOARCH = 'amd64',
    [switch]$SkipAdmin
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$admin = Join-Path $root 'admin'
$server = Join-Path $root 'server'
$dist = Join-Path $root 'dist'

function Step([string]$text) {
    Write-Host "`n=== $text ===" -ForegroundColor Cyan
}

Step "1/3 构建管理后台"
if ($SkipAdmin) {
    Write-Host '已跳过'
}
else {
    Push-Location $admin
    # Windows 上 esbuild 在某些环境下删不掉系统临时目录里的文件，指到工作区更稳
    $env:TEMP = Join-Path $root '.tmp'
    $env:TMP = $env:TEMP
    New-Item -ItemType Directory -Force -Path $env:TEMP | Out-Null
    npm run build
    if ($LASTEXITCODE -ne 0) { Pop-Location; throw '管理后台构建失败' }
    Pop-Location
}

Step "2/3 编译 Go 服务端（$GOOS/$GOARCH）"
New-Item -ItemType Directory -Force -Path $dist | Out-Null
Push-Location $server
$env:CGO_ENABLED = '0'
$env:GOOS = $GOOS
$env:GOARCH = $GOARCH
$suffix = if ($GOOS -eq 'windows') { '.exe' } else { '' }
go build -trimpath -ldflags '-s -w' -o (Join-Path $dist "ledger-server$suffix") ./cmd/api
if ($LASTEXITCODE -ne 0) { Pop-Location; throw '服务端编译失败' }

Step "3/3 编译备份工具"
go build -trimpath -ldflags '-s -w' -o (Join-Path $dist "ledger-backup$suffix") ./cmd/backup
if ($LASTEXITCODE -ne 0) { Pop-Location; throw '备份工具编译失败' }
Pop-Location

Step "产物"
Get-ChildItem $dist | Select-Object Name, @{ n = 'MB'; e = { [math]::Round($_.Length / 1MB, 1) } } | Format-Table -AutoSize

Write-Host "部署说明见 server/deploy/README.md" -ForegroundColor Green
