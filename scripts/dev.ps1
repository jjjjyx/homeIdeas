<#
.SYNOPSIS
    家庭记账本开发环境一键启动/停止/重启/编译。

.DESCRIPTION
    管理三样东西：后端服务（:8088）、Android 模拟器、adb 端口转发。

    所有外部命令都有硬超时（默认 15 秒），超时直接杀掉，
    所以 start/stop/restart 都不会无限挂住。

.EXAMPLE
    .\scripts\dev.ps1 start            启动后端 + 模拟器
    .\scripts\dev.ps1 start -App       启动并打开 App
    .\scripts\dev.ps1 stop             全部关闭
    .\scripts\dev.ps1 restart -App     重启并打开 App
    .\scripts\dev.ps1 status           查看状态
    .\scripts\dev.ps1 logs             看后端日志
    .\scripts\dev.ps1 build            重新编译后端 + App（自动走代理）

.NOTES
    环境变量读 server\.env.dev（含 API Key，不要提交到版本库）。
#>
param(
    [Parameter(Position = 0)]
    [ValidateSet('start', 'stop', 'restart', 'status', 'logs', 'build')]
    [string]$Action = 'start',

    # 启动后自动打开 App
    [switch]$App,

    # 只起后端，不碰模拟器
    [switch]$NoEmulator
)

$ErrorActionPreference = 'Continue'

# ---------- 路径 ----------
$Root      = Split-Path -Parent $PSScriptRoot
$ServerDir = Join-Path $Root 'server'
$AppDir    = Join-Path $Root 'family_ledger'
$DevDir    = Join-Path $Root '.dev'
$PidFile   = Join-Path $DevDir 'server.pid'
$ServerLog = Join-Path $DevDir 'server.log'
$EnvFile   = Join-Path $ServerDir '.env.dev'

$AndroidSdk = 'D:\dev\android-sdk'
$Emulator   = Join-Path $AndroidSdk 'emulator\emulator.exe'
$Adb        = Join-Path $AndroidSdk 'platform-tools\adb.exe'
$AvdName    = 'ledger35'
$Serial     = 'emulator-5554'
$BaseUrl    = 'http://127.0.0.1:8088'
$AppPackage = 'com.family.family_ledger'

# 全局超时（秒）：任何一步都不许超过这些值
$TimeoutAdb      = 15
$TimeoutServerUp = 25
$TimeoutBoot     = 90
$TimeoutStopEmu  = 10

if (-not (Test-Path $DevDir)) { New-Item -ItemType Directory -Path $DevDir -Force | Out-Null }

# ---------- 输出 ----------
function Step($msg)  { Write-Host "  $msg" -ForegroundColor DarkGray }
function Ok($msg)    { Write-Host "  [OK] $msg" -ForegroundColor Green }
function Warn($msg)  { Write-Host "  [!] $msg" -ForegroundColor Yellow }
function Fail($msg)  { Write-Host "  [x] $msg" -ForegroundColor Red }
function Title($msg) { Write-Host ""; Write-Host $msg -ForegroundColor Cyan }

# ---------- 带超时的外部命令 ----------
# 关键：PowerShell 直接调原生命令没有超时，adb 一旦卡住整个脚本就永久挂起。
# 统一走 Start-Process + WaitForExit(ms) + Kill，保证一定会返回。
function Invoke-Cmd {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [string[]]$Arguments = @(),
        [int]$TimeoutSec = 15
    )

    $id = [guid]::NewGuid().ToString('N').Substring(0, 8)
    $outFile = Join-Path $DevDir "cmd-$id.out"
    $errFile = Join-Path $DevDir "cmd-$id.err"

    try {
        $p = Start-Process -FilePath $FilePath -ArgumentList $Arguments -PassThru `
            -WindowStyle Hidden `
            -RedirectStandardOutput $outFile -RedirectStandardError $errFile

        if (-not $p.WaitForExit($TimeoutSec * 1000)) {
            try { $p.Kill() } catch { }
            try { $null = $p.WaitForExit(2000) } catch { }
            return $null   # 超时
        }

        $text = @()
        if (Test-Path $outFile) { $text += Get-Content $outFile }
        if (Test-Path $errFile) { $text += Get-Content $errFile }
        return ($text -join "`n")
    } finally {
        Remove-Item $outFile, $errFile -Force -ErrorAction SilentlyContinue
    }
}

function Invoke-Adb {
    param([string[]]$AdbArgs, [int]$TimeoutSec = 15)
    if (-not (Test-Path $Adb)) { return $null }
    $all = @('-s', $Serial) + $AdbArgs
    if ($AdbArgs[0] -eq 'devices' -or $AdbArgs[0] -eq 'start-server') { $all = $AdbArgs }
    return (Invoke-Cmd -FilePath $Adb -Arguments $all -TimeoutSec $TimeoutSec)
}

# ---------- 探测 ----------
function Test-ServerUp {
    try {
        $r = Invoke-RestMethod "$BaseUrl/readyz" -TimeoutSec 2
        return ($r.data.status -eq 'ready')
    } catch { return $false }
}

function Test-EmulatorRunning {
    if (-not (Test-Path $Adb)) { return $false }
    $out = Invoke-Adb @('devices') 6
    if ($null -eq $out) { return $false }
    return [bool]($out -match "$Serial\s+device")
}

function Test-EmulatorBooted {
    $b = Invoke-Adb @('shell', 'getprop', 'sys.boot_completed') 8
    if ($null -eq $b) { return $false }
    return $b.Trim() -eq '1'
}

function Wait-Server($timeoutSec = 25) {
    $sw = [Diagnostics.Stopwatch]::StartNew()
    Write-Host "  等待后端就绪" -NoNewline -ForegroundColor DarkGray
    while ($sw.Elapsed.TotalSeconds -lt $timeoutSec) {
        if (Test-ServerUp) { Write-Host " ok" -ForegroundColor DarkGray; return $true }
        Write-Host "." -NoNewline -ForegroundColor DarkGray
        Start-Sleep -Milliseconds 700
    }
    Write-Host ""
    return $false
}

function Wait-Boot($timeoutSec = 90) {
    $sw = [Diagnostics.Stopwatch]::StartNew()
    Write-Host "  等待系统引导" -NoNewline -ForegroundColor DarkGray
    while ($sw.Elapsed.TotalSeconds -lt $timeoutSec) {
        if (Test-EmulatorBooted) {
            Write-Host (" ok（{0:N0}s）" -f $sw.Elapsed.TotalSeconds) -ForegroundColor DarkGray
            return $true
        }
        Write-Host "." -NoNewline -ForegroundColor DarkGray
        Start-Sleep -Seconds 2
    }
    Write-Host ""
    return $false
}

# ---------- 环境变量 ----------
function Import-DevEnv {
    if (-not (Test-Path $EnvFile)) {
        Warn "缺少 $EnvFile，使用当前环境变量（可能没有 LLM_API_KEY）"
        return
    }
    $count = 0
    foreach ($line in (Get-Content $EnvFile)) {
        $t = $line.Trim()
        if ($t -eq '' -or $t.StartsWith('#')) { continue }
        $i = $t.IndexOf('=')
        if ($i -le 0) { continue }
        Set-Item -Path ('env:' + $t.Substring(0, $i).Trim()) -Value $t.Substring($i + 1).Trim()
        $count++
    }
    Step "已加载 $count 项环境变量"
}

# ---------- 启动 ----------
function Start-Server {
    Title '后端服务'

    if (Test-ServerUp) { Ok "已在运行（$BaseUrl）"; return }

    $exe = Join-Path $ServerDir 'bin\ledger-server.exe'
    if (-not (Test-Path $exe)) {
        Fail "找不到 $exe，先跑 .\scripts\dev.ps1 build"
        return
    }

    Import-DevEnv
    Get-Process ledger-server -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

    $proc = Start-Process -FilePath $exe -WorkingDirectory $ServerDir -PassThru -WindowStyle Hidden `
        -RedirectStandardOutput $ServerLog -RedirectStandardError "$ServerLog.err"
    $proc.Id | Set-Content -Path $PidFile -Encoding ASCII

    if (Wait-Server $TimeoutServerUp) {
        Ok "已启动，PID $($proc.Id)"
    } else {
        Fail "启动超时（${TimeoutServerUp}s），看日志：.dev\server.log"
        if (Test-Path $ServerLog) { Get-Content $ServerLog -Tail 10 | ForEach-Object { "      $_" } }
    }
}

function Start-Emulator {
    Title 'Android 模拟器'

    if (Test-EmulatorBooted) {
        Ok "已在运行（$Serial）"
        Enable-PortForward
        return
    }

    if (-not (Test-Path $Emulator)) { Fail "找不到模拟器：$Emulator"; return }

    Invoke-Adb @('start-server') 10 | Out-Null

    # 有残留 qemu 但 adb 看不到 → 先清掉，避免端口/锁冲突
    $zombie = Get-Process qemu-system-x86_64 -ErrorAction SilentlyContinue
    if ($zombie -and -not (Test-EmulatorRunning)) {
        Step "清理残留模拟器进程"
        $zombie | Stop-Process -Force -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 1
    }

    if (-not (Test-EmulatorRunning)) {
        Step "启动 AVD $AvdName（窗口会弹出）"
        Start-Process -FilePath $Emulator -ArgumentList @(
            '-avd', $AvdName,
            '-no-snapshot-save', '-no-snapshot-load',
            '-no-audio',
            '-no-boot-anim',
            '-gpu', 'swiftshader_indirect'
        ) -RedirectStandardOutput (Join-Path $DevDir 'emulator.log') `
          -RedirectStandardError (Join-Path $DevDir 'emulator.log.err') | Out-Null
    }

    if (Wait-Boot $TimeoutBoot) {
        Ok "已启动并完成引导"
        Enable-PortForward
    } else {
        Fail "模拟器 ${TimeoutBoot}s 内没有完成引导"
        $emuLog = Join-Path $DevDir 'emulator.log'
        if (Test-Path $emuLog) { Get-Content $emuLog -Tail 8 | ForEach-Object { "      $_" } }
    }
}

function Enable-PortForward {
    $null = Invoke-Adb @('reverse', 'tcp:8088', 'tcp:8088') 8
    $list = Invoke-Adb @('reverse', '--list') 8
    if ($list -and $list -match 'tcp:8088') {
        Ok "端口转发 tcp:8088 -> 本机 :8088"
    } else {
        Warn "端口转发未生效，App 可能连不上后端"
    }
}

function Open-App {
    if (-not (Test-EmulatorBooted)) { Warn "模拟器没起来，跳过打开 App"; return }
    $null = Invoke-Adb @('shell', 'am', 'start', '-n', "$AppPackage/.MainActivity") 15
    Ok "App 已拉起"
}

# ---------- 停止 ----------
function Stop-Server {
    Title '关闭后端'
    $stopped = $false

    if (Test-Path $PidFile) {
        $oldPid = (Get-Content $PidFile -ErrorAction SilentlyContinue | Select-Object -First 1)
        if ($oldPid) {
            if (Get-Process -Id $oldPid -ErrorAction SilentlyContinue) {
                Stop-Process -Id $oldPid -Force -ErrorAction SilentlyContinue
                $stopped = $true
            }
        }
        Remove-Item $PidFile -Force -ErrorAction SilentlyContinue
    }

    Get-Process ledger-server -ErrorAction SilentlyContinue | ForEach-Object {
        Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
        $stopped = $true
    }

    if ($stopped) { Ok "已停止" } else { Step "本来就没在跑" }
}

function Stop-Emulator {
    Title '关闭模拟器'

    if (-not (Test-EmulatorRunning)) {
        Get-Process qemu-system-x86_64 -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
        Step "本来就没在跑"
        return
    }

    # 先礼后兵：adb emu kill 限时 10 秒，不走就直接杀 qemu
    $null = Invoke-Adb @('emu', 'kill') 8
    $sw = [Diagnostics.Stopwatch]::StartNew()
    Write-Host "  正在关闭" -NoNewline -ForegroundColor DarkGray
    while ($sw.Elapsed.TotalSeconds -lt $TimeoutStopEmu) {
        if (-not (Test-EmulatorRunning)) {
            Write-Host " ok" -ForegroundColor DarkGray
            Ok "已关闭"
            return
        }
        Write-Host "." -NoNewline -ForegroundColor DarkGray
        Start-Sleep -Milliseconds 600
    }
    Write-Host ""

    Get-Process qemu-system-x86_64 -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 500
    Ok "已强制关闭"
}

# ---------- 状态 ----------
function Show-Status {
    Title '运行状态'

    if (Test-ServerUp) { Write-Host "  后端      运行中  $BaseUrl" -ForegroundColor Green }
    else               { Write-Host "  后端      未运行" -ForegroundColor Red }

    $emuUp = Test-EmulatorRunning
    if ($emuUp) { Write-Host "  模拟器    运行中  $Serial" -ForegroundColor Green }
    else        { Write-Host "  模拟器    未运行" -ForegroundColor Red }

    if ($emuUp) {
        if (Test-EmulatorBooted) { Write-Host "  引导状态  已完成" -ForegroundColor Green }
        else                     { Write-Host "  引导状态  正在启动…" -ForegroundColor Yellow }

        $list = Invoke-Adb @('reverse', '--list') 8
        if ($list -and $list -match 'tcp:8088') { Write-Host "  端口转发  已建立" -ForegroundColor Green }
        else { Write-Host "  端口转发  未建立" -ForegroundColor Yellow }

        $pkg = Invoke-Adb @('shell', 'pm', 'list', 'packages') 10
        if ($pkg -and $pkg -match [regex]::Escape($AppPackage)) {
            Write-Host "  App       已安装" -ForegroundColor Green
        } else {
            Write-Host "  App       未安装" -ForegroundColor Yellow
        }
    }

    $aehd = Get-Service aehd -ErrorAction SilentlyContinue
    if ($aehd -and $aehd.Status -eq 'Running') { Write-Host "  AEHD 驱动 运行中" -ForegroundColor Green }
    else { Write-Host "  AEHD 驱动 未运行（模拟器会很慢）" -ForegroundColor Yellow }
    Write-Host ""
}

function Show-Logs {
    if (Test-Path $ServerLog) {
        Title '后端日志（最后 40 行）'
        Get-Content $ServerLog -Tail 40
    } else {
        Warn "还没有日志：$ServerLog"
    }
}

# ---------- 编译 ----------
function Invoke-Build {
    Title '重新编译'

    # 编译要访问外网（Flutter 引擎产物 / 依赖），走本机代理；
    # 没开代理只是慢，不会失败。
    $proxy = 'http://127.0.0.1:10809'
    $proxyUp = Test-NetConnection -ComputerName 127.0.0.1 -Port 10809 -InformationLevel Quiet `
        -WarningAction SilentlyContinue -ErrorAction SilentlyContinue
    if ($proxyUp) {
        $env:HTTPS_PROXY = $proxy
        $env:HTTP_PROXY = $proxy
        Step "使用代理 $proxy"
    } else {
        Warn "代理未监听，直接连接（首次可能很慢）"
    }

    $env:JAVA_HOME = 'D:\dev\jdk17'
    $env:ANDROID_HOME = 'D:\dev\android-sdk'
    $env:PATH = "D:\dev\jdk17\bin;D:\dev\android-sdk\platform-tools;D:\dev\flutter\bin;" + $env:PATH

    Step '编译后端'
    Push-Location $ServerDir
    & go build -trimpath -o bin/ledger-server.exe ./cmd/api
    $goOk = ($LASTEXITCODE -eq 0)
    Pop-Location
    if ($goOk) { Ok '后端编译完成' } else { Fail '后端编译失败' }

    Step '编译 App（debug）'
    Push-Location $AppDir
    & flutter build apk --debug --dart-define=API_BASE_URL=http://127.0.0.1:8088
    $flutterOk = ($LASTEXITCODE -eq 0)
    Pop-Location
    if ($flutterOk) { Ok 'App 编译完成' } else { Fail 'App 编译失败' }
}

# ---------- 主流程 ----------
$total = [Diagnostics.Stopwatch]::StartNew()

switch ($Action) {
    'start' {
        Start-Server
        if (-not $NoEmulator) { Start-Emulator }
        if ($App) { Open-App }
        Show-Status
    }
    'stop' {
        Stop-Server
        Stop-Emulator
        Show-Status
    }
    'restart' {
        Stop-Server
        Stop-Emulator
        Start-Sleep -Milliseconds 500
        Start-Server
        if (-not $NoEmulator) { Start-Emulator }
        if ($App) { Open-App }
        Show-Status
    }
    'status' { Show-Status }
    'logs'   { Show-Logs }
    'build'  { Invoke-Build }
}

$total.Stop()
$secs = [math]::Round($total.Elapsed.TotalSeconds, 0)
if ($secs -gt 60) {
    Warn "本次耗时 ${secs} 秒（超过 1 分钟，检查 .dev\emulator.log）"
} else {
    Write-Host "  耗时 ${secs} 秒" -ForegroundColor DarkGray
}
