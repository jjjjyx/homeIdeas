# 启动记账服务端（供计划任务调用）
#
# 电脑开机后自动运行，手机就能直接记账，不用手动开窗口。
# 日志写到 server/logs/server.log。
$ErrorActionPreference = 'Stop'

$root = 'G:\26-workspace\homeIdeas\server'
$exe = Join-Path $root 'bin\ledger-server.exe'
$logDir = Join-Path $root 'logs'

if (-not (Test-Path $exe)) {
    throw "找不到服务端程序: $exe。请先执行 build-release.ps1 或 go build。"
}
if (-not (Test-Path $logDir)) {
    New-Item -ItemType Directory -Force -Path $logDir | Out-Null
}

# ---- 运行参数（按需修改）----
$env:APP_ENV = 'dev'
$env:HTTP_ADDR = ':8088'
$env:DB_PATH = './data/app.db'
$env:MAIL_POLL_MINUTES = '0'
$env:LLM_PROVIDER = 'deepseek'
$env:LLM_BASE_URL = 'https://api.deepseek.com'
$env:LLM_MODEL = 'deepseek-chat'

# 密钥不写进脚本：优先读取 server\.env.dev（已 gitignore），也支持系统环境变量
$envFile = Join-Path $root '.env.dev'
if (Test-Path $envFile) {
    foreach ($line in Get-Content $envFile) {
        $t = $line.Trim()
        if ($t -eq '' -or $t.StartsWith('#')) { continue }
        $i = $t.IndexOf('=')
        if ($i -le 0) { continue }
        Set-Item -Path ('env:' + $t.Substring(0, $i).Trim()) -Value $t.Substring($i + 1).Trim()
    }
}
if (-not $env:JWT_SECRET) { throw '缺少 JWT_SECRET：请在 server\.env.dev 或系统环境变量中配置' }

Set-Location $root
$stamp = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'

# 已经在跑就不重复启动（端口被占会直接退出）
$running = Get-Process ledger-server -ErrorAction SilentlyContinue
if ($running) {
    "[$stamp] 服务已在运行（PID $($running.Id)），跳过" | Out-File -Append -Encoding utf8 (Join-Path $logDir 'server.log')
    exit 0
}

"[$stamp] 启动 ledger-server" | Out-File -Append -Encoding utf8 (Join-Path $logDir 'server.log')
& $exe *>> (Join-Path $logDir 'server.log')
