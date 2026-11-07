# 手机无线调试一键恢复
#
# 用法：
#   1) 手机先插一次 USB（或者知道手机 IP）
#   2) .\wifi-adb.ps1                    # 自动从 USB 设备取 IP
#      .\wifi-adb.ps1 -Ip 192.168.3.105  # 直接指定 IP
#
# 作用：把手机的 adbd 切到 TCP 5555，然后通过 WiFi 连接，并重建 8088 端口转发。
param(
    [string]$Ip = '',
    [int]$Port = 5555,
    [int]$ReversePort = 8088
)

$ErrorActionPreference = 'Stop'
$adb = 'D:\dev\android-sdk\platform-tools\adb.exe'

if (-not (Test-Path $adb)) {
    Write-Error "找不到 adb: $adb"
}

# 1) 没给 IP 就从 USB 设备里取
if ([string]::IsNullOrWhiteSpace($Ip)) {
    $serial = (& $adb devices) | Select-String -Pattern '^\S+\s+device$' |
        ForEach-Object { ($_.Line -split '\s+')[0] } |
        Where-Object { $_ -notmatch ':' } |
        Select-Object -First 1

    if (-not $serial) {
        Write-Error "没有 USB 设备。请插上数据线，或用 -Ip 指定手机 IP。"
    }

    $line = (& $adb -s $serial shell ip -f inet addr show wlan0) | Select-String -Pattern 'inet (\d+\.\d+\.\d+\.\d+)'
    if (-not $line) {
        Write-Error "手机没有连 WiFi（wlan0 没拿到 IP）。"
    }
    $Ip = ([regex]::Match($line.Line, 'inet (\d+\.\d+\.\d+\.\d+)')).Groups[1].Value
    Write-Host "手机 IP: $Ip（来自 USB 设备 $serial）"

    Write-Host "切换 adbd 到 TCP 模式…"
    & $adb -s $serial tcpip $Port | Out-Null
    Start-Sleep -Seconds 4
}

# 2) 通过 WiFi 连接
Write-Host "连接 $Ip`:$Port …"
$out = & $adb connect "$Ip`:$Port" 2>&1
Write-Host "  $out"

Start-Sleep -Seconds 2
$target = "$Ip`:$Port"
$ok = (& $adb devices) -match [regex]::Escape($target)

if (-not $ok) {
    Write-Warning "连接失败。常见原因："
    Write-Host "  · 手机锁屏/休眠，WiFi 被省电断开 → 点亮屏幕重试"
    Write-Host "  · 手机重启过 → 需要重新插一次 USB 执行本脚本"
    Write-Host "  · 手机换了 WiFi / IP 变了 → 重新确认 IP"
    exit 1
}

# 3) 重建 8088 端口转发（App 用 127.0.0.1:8088 时需要）
& $adb -s $target reverse "tcp:$ReversePort" "tcp:$ReversePort" | Out-Null
$rev = (& $adb -s $target reverse --list) -join ', '

Write-Host ""
Write-Host "✅ 无线调试已就绪"
Write-Host "   设备：$target"
Write-Host "   转发：$rev"
Write-Host ""
Write-Host "现在可以拔掉数据线了。之后用 adb 时加 -s $target，"
Write-Host "或者直接 adb（只有一台设备时）。"
