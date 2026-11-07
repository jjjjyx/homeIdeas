# 后端接口冒烟测试（单家庭：认证 / 邀请码 / 账本 / 同步 / 管理后台）
#
# 默认自带一个临时服务实例（临时数据库 + 随机端口），可重复执行；
# 也可以指定 -BaseUrl 打到已经在跑的服务（注意：该库必须是空的）。
#
#   .\scripts\smoke.ps1                      # 自托管，跑完自动清理
#   .\scripts\smoke.ps1 -BaseUrl http://127.0.0.1:8088
param(
    [string]$BaseUrl = ''
)

$ErrorActionPreference = 'Stop'
$script:pass = 0
$script:fail = 0
$script:serverProc = $null
$script:tempDir = $null
$script:scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$script:repoRoot = Split-Path -Parent $script:scriptRoot

function Start-TempServer {
    $root = $script:repoRoot
    $bin = Join-Path $root 'bin\ledger-server.exe'
    if (-not (Test-Path $bin)) {
        Write-Host '未找到 bin/ledger-server.exe，先执行 go build ...' -ForegroundColor Yellow
        Push-Location $root
        & go build -trimpath -o bin/ledger-server.exe ./cmd/api
        Pop-Location
        if (-not (Test-Path $bin)) { throw '编译服务端失败' }
    }

    $script:tempDir = Join-Path $root ('.smoke-tmp\' + [Guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Force -Path $script:tempDir | Out-Null

    $port = Get-Random -Minimum 18000 -Maximum 19999
    $env:APP_ENV = 'dev'
    $env:HTTP_ADDR = ":$port"
    $env:DB_PATH = (Join-Path $script:tempDir 'app.db')
    $env:JWT_SECRET = 'smoke-test-secret-0123456789'
    $env:MAIL_POLL_MINUTES = '0'

    $out = Join-Path $script:tempDir 'server.log'
    $err = Join-Path $script:tempDir 'server.err.log'
    $script:serverProc = Start-Process -FilePath $bin -PassThru -WindowStyle Hidden `
        -RedirectStandardOutput $out -RedirectStandardError $err

    $script:BaseUrl = "http://127.0.0.1:$port"
    for ($i = 0; $i -lt 40; $i++) {
        Start-Sleep -Milliseconds 250
        try {
            $res = Invoke-RestMethod -Uri "$($script:BaseUrl)/readyz" -TimeoutSec 2
            if ($res.data.status -eq 'ready') { return }
        }
        catch { }
    }
    throw "临时服务启动超时，日志：$out"
}

function Stop-TempServer {
    if ($script:serverProc -and -not $script:serverProc.HasExited) {
        Stop-Process -Id $script:serverProc.Id -Force -ErrorAction SilentlyContinue
    }
    if ($script:tempDir -and (Test-Path $script:tempDir)) {
        Start-Sleep -Milliseconds 300
        Remove-Item $script:tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

if (-not $BaseUrl) {
    Start-TempServer
}
else {
    $script:BaseUrl = $BaseUrl
}

trap {
    Stop-TempServer
    Write-Host "`n脚本异常终止: $_" -ForegroundColor Red
    exit 1
}

function Check([string]$name, [bool]$cond, [string]$detail = '') {
    if ($cond) {
        $script:pass++
        Write-Host "  [PASS] $name"
    }
    else {
        $script:fail++
        Write-Host "  [FAIL] $name  $detail" -ForegroundColor Red
    }
}

function Api {
    param(
        [string]$Method,
        [string]$Path,
        $Body = $null,
        [string]$Token = ''
    )
    $headers = @{}
    if ($Token) { $headers['Authorization'] = "Bearer $Token" }
    $params = @{
        Uri         = "$script:BaseUrl$Path"
        Method      = $Method
        Headers     = $headers
        TimeoutSec  = 15
        ErrorAction = 'Stop'
    }
    if ($null -ne $Body) {
        # PowerShell 5.1 默认不按 UTF-8 发送，中文会变问号；显式转成 UTF-8 字节
        $json = $Body | ConvertTo-Json -Compress -Depth 6
        $params['Body'] = [System.Text.Encoding]::UTF8.GetBytes($json)
        $params['ContentType'] = 'application/json; charset=utf-8'
    }
    try {
        $resp = Invoke-RestMethod @params
        return @{ ok = $true; status = 200; body = $resp }
    }
    catch {
        $status = 0
        $payload = $null
        $resp = $_.Exception.Response
        if ($resp) {
            try { $status = [int]$resp.StatusCode } catch { $status = 0 }
        }
        try {
            $raw = $null
            if ($_.ErrorDetails -and $_.ErrorDetails.Message) {
                $raw = $_.ErrorDetails.Message
            }
            elseif ($resp -and $resp.PSObject.Methods['GetResponseStream']) {
                $stream = $resp.GetResponseStream()
                if ($stream) {
                    $reader = New-Object System.IO.StreamReader($stream)
                    $raw = $reader.ReadToEnd()
                }
            }
            if ($raw) { $payload = $raw | ConvertFrom-Json }
        }
        catch { }
        return @{ ok = $false; status = $status; body = $payload }
    }
}

Write-Host "`n=== 家庭记账本后端冒烟测试（单家庭） @ $script:BaseUrl ===" -ForegroundColor Cyan
$suffix = (Get-Random -Minimum 10000 -Maximum 99999)
$u1 = "papa$suffix"
$u2 = "mama$suffix"
$u3 = "uncle$suffix"
$pwd = 'Smoke-Test-2026'

Write-Host "`n[1] 健康检查"
$r = Api GET '/healthz'
Check 'healthz 返回 200' ($r.ok -and $r.body.data.status -eq 'ok')
$r = Api GET '/readyz'
Check 'readyz 数据库就绪' ($r.ok)

Write-Host "`n[2] 首次初始化：创建唯一家庭 + 户主"
$r = Api POST '/api/v1/auth/register' @{ account = $u1; password = $pwd; nickname = '爸爸'; family_name = '我们的家' }
Check "初始化 $u1 成功" ($r.ok -and $r.body.data.access_token)
$tok1 = $r.body.data.access_token
$uid1 = $r.body.data.user.id
$refresh1 = $r.body.data.refresh_token
Check '首个用户是户主' ($r.body.data.user.role -eq 'owner')
Check '首个用户可进管理后台' ($r.body.data.user.is_platform_admin -eq $true)

Write-Host "`n[3] /me：唯一家庭"
$r = Api GET '/api/v1/me' -Token $tok1
Check 'me 返回用户信息' ($r.ok -and $r.body.data.user.account -eq $u1)
Check '家庭已自动创建' ($r.body.data.family.name -eq '我们的家')
Check '家庭里只有 1 位成员' ($r.body.data.members.Count -eq 1)
$famName = $r.body.data.family.name

Write-Host "`n[4] 修改家庭名称"
$r = Api PATCH '/api/v1/family' @{ name = '我们的家·测试' } -Token $tok1
Check 'owner 改名成功' ($r.ok)
$r = Api GET '/api/v1/family' -Token $tok1
Check '家庭名已更新' ($r.body.data.family.name -eq '我们的家·测试')

Write-Host "`n[5] 生成邀请码（成员加入的主要方式）"
$r = Api POST '/api/v1/family/invites' @{ role = 'member'; expires_in_hours = 24 } -Token $tok1
Check '生成邀请码成功' ($r.ok -and $r.body.data.code.Length -eq 6)
$code = $r.body.data.code

Write-Host "`n[6] 无邀请码注册第二个用户（应被拒绝）"
$r = Api POST '/api/v1/auth/register' @{ account = "ghost$suffix"; password = $pwd; nickname = '路人' }
Check '拒绝无邀请码注册 (4003)' ((-not $r.ok) -and $r.body.code -eq 4003)

Write-Host "`n[7] 用邀请码注册并加入唯一家庭"
$r = Api POST '/api/v1/auth/register' @{ account = $u2; password = $pwd; nickname = '妈妈'; invite_code = $code }
Check "注册 $u2 并加入家庭" ($r.ok -and $r.body.data.user.account -eq $u2)
$tok2 = $r.body.data.access_token
$uid2 = $r.body.data.user.id
Check '新用户角色是 member' ($r.body.data.user.role -eq 'member')
$r = Api GET '/api/v1/me' -Token $tok2
Check '新用户看到同一个家庭' ($r.body.data.family.name -eq '我们的家·测试')

Write-Host "`n[8] 成员列表"
$r = Api GET '/api/v1/family/members' -Token $tok1
Check '家庭成员数为 2' ($r.body.data.items.Count -eq 2)
Check '按角色排序（owner 在前）' ($r.body.data.items[0].role -eq 'owner')

Write-Host "`n[9] 重复使用同一邀请码（应失败）"
$r = Api POST '/api/v1/auth/register' @{ account = $u3; password = $pwd; nickname = '叔叔'; invite_code = $code }
Check '邀请码不可复用 (4004)' ((-not $r.ok) -and $r.body.code -eq 4004)

Write-Host "`n[10] 普通成员不能改名"
$r = Api PATCH '/api/v1/family' @{ name = '乱改' } -Token $tok2
Check 'member 改名被拒绝 (403)' ((-not $r.ok) -and $r.status -eq 403)

Write-Host "`n[11] 户主把成员提升为管理员"
$r = Api PATCH "/api/v1/family/members/$uid2" @{ role = 'admin' } -Token $tok1
Check '提升为 admin 成功' ($r.ok)
$r = Api GET '/api/v1/family/members' -Token $tok1
$roleOf2 = ($r.body.data.items | Where-Object { $_.user_id -eq $uid2 }).role
Check '角色已变为 admin' ($roleOf2 -eq 'admin')

Write-Host "`n[12] 管理员不能移除户主"
$r = Api DELETE "/api/v1/family/members/$uid1" -Token $tok2
Check '移除户主被拒绝' ((-not $r.ok) -and ($r.body.code -eq 4005 -or $r.status -eq 403))

Write-Host "`n[13] 只有户主能发管理员邀请；管理员只能发成员邀请"
$r = Api POST '/api/v1/family/invites' @{ role = 'admin'; expires_in_hours = 24 } -Token $tok2
Check '管理员发 admin 邀请被拒绝 (403)' ((-not $r.ok) -and $r.status -eq 403)
$r = Api POST '/api/v1/family/invites' @{ role = 'member'; expires_in_hours = 24 } -Token $tok2
Check '管理员可以发 member 邀请' ($r.ok -and $r.body.data.code.Length -eq 6)

Write-Host "`n[14] 令牌刷新"
$r = Api POST '/api/v1/auth/refresh' @{ refresh_token = $refresh1 }
Check 'refresh 换取新 access token' ($r.ok -and $r.body.data.access_token)
$newTok = $r.body.data.access_token
$r = Api GET '/api/v1/me' -Token $newTok
Check '新令牌可用' ($r.ok)

Write-Host "`n[15] 退出登录后 refresh 失效"
$r = Api POST '/api/v1/auth/logout' @{ refresh_token = $refresh1 }
Check 'logout 成功' ($r.ok)
$r = Api POST '/api/v1/auth/refresh' @{ refresh_token = $refresh1 }
Check 'refresh 已失效 (401)' ((-not $r.ok) -and $r.status -eq 401)

Write-Host "`n[16] 未登录 / 错误密码"
$r = Api GET '/api/v1/me'
Check '未登录返回 401' ((-not $r.ok) -and $r.status -eq 401)
$r = Api POST '/api/v1/auth/login' @{ account = $u1; password = 'wrong-password' }
Check '错误密码返回 401' ((-not $r.ok) -and $r.status -eq 401)

# ---------------- 账本与同步 ----------------

Write-Host "`n[17] 同步拉取（首次全量：分类/账户/成员）"
$r = Api GET '/api/v1/sync?since=0' -Token $tok1
Check 'sync 返回 200' ($r.ok)
$changes = $r.body.data.changes
Check '默认分类 19 个' ($changes.categories.Count -eq 19)
Check '默认账户 4 个' ($changes.accounts.Count -eq 4)
Check '成员 2 人' ($changes.members.Count -eq 2)
$catFood = ($changes.categories | Where-Object { $_.name -eq '餐饮' }).id
Check '能取到餐饮分类 id' ([bool]$catFood)
$cursor0 = $r.body.data.cursor
Check '初始 cursor 大于 0' ($cursor0 -gt 0)

Write-Host "`n[18] 记一笔支出"
$now = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$r = Api POST '/api/v1/transactions' @{ type = 0; amount_cents = 12850; category_id = $catFood; occurred_at = $now; note = '午饭' } -Token $tok1
Check '记账成功' ($r.ok -and $r.body.data.amount_cents -eq 12850)
Check '返回 rev' ($r.body.data.rev -gt 0)
$tx1 = $r.body.data.id

Write-Host "`n[19] 账目列表 / 统计"
$r = Api GET '/api/v1/transactions' -Token $tok1
Check '列表返回 1 条' ($r.body.data.items.Count -eq 1)
Check '金额正确' ($r.body.data.items[0].amount_cents -eq 12850)
$r = Api GET '/api/v1/stats/summary' -Token $tok1
Check '汇总支出 12850' ($r.body.data.expense_cents -eq 12850)
Check '汇总收入 0' ($r.body.data.income_cents -eq 0)
$r = Api GET '/api/v1/stats/categories?type=0' -Token $tok1
Check '分类统计 1 条' ($r.body.data.items.Count -eq 1)
Check '占比为 100%' ([math]::Abs($r.body.data.items[0].ratio - 1) -lt 0.001)
$r = Api GET '/api/v1/stats/trend?months=6&type=0' -Token $tok1
Check '趋势返回 6 个月' ($r.body.data.items.Count -eq 6)
Check '最后一个月金额正确' ($r.body.data.items[5].amount_cents -eq 12850)

Write-Host "`n[20] 私密账对家人不可见"
$r = Api POST '/api/v1/transactions' @{ type = 0; amount_cents = 9900; category_id = $catFood; occurred_at = $now; note = '私房钱'; visibility = 'private' } -Token $tok1
Check '创建私密账成功' ($r.ok)
$r = Api GET '/api/v1/transactions' -Token $tok2
Check '成员只看到 1 条（看不到私密账）' ($r.body.data.items.Count -eq 1)
$r = Api GET '/api/v1/transactions' -Token $tok1
Check '本人看到 2 条' ($r.body.data.items.Count -eq 2)

Write-Host "`n[21] 增量同步"
$r = Api GET "/api/v1/sync?since=$cursor0" -Token $tok1
$cursor1 = $r.body.data.cursor
Check '增量返回 2 条账单' ($r.body.data.changes.transactions.Count -eq 2)
$r = Api GET "/api/v1/sync?since=$cursor1" -Token $tok1
Check '再拉一次为空' ($r.body.data.changes.transactions.Count -eq 0)
Check 'cursor 不再前进' ($r.body.data.cursor -eq $cursor1)

Write-Host "`n[22] 离线推送（客户端生成 id）"
$offlineId = '01a1' + (Get-Random -Minimum 100000 -Maximum 999999) + '000000000000'
$r = Api POST '/api/v1/sync/push' @{ ops = @(@{ client_op_id = 'local-1'; entity = 'transaction'; op = 'upsert'; base_rev = 0; payload = @{ id = $offlineId; type = 0; amount_cents = 3000; category_id = $catFood; occurred_at = $now; note = '离线记的' } }) } -Token $tok2
Check '推送状态 applied' ($r.ok -and $r.body.data.results[0].status -eq 'applied')
Check '返回 rev' ($r.body.data.results[0].rev -gt 0)
$r = Api GET "/api/v1/sync?since=$cursor1" -Token $tok1
Check '户主能拉到成员离线记的账' (@($r.body.data.changes.transactions | Where-Object { $_.id -eq $offlineId }).Count -eq 1)

Write-Host "`n[23] 版本冲突"
$r = Api POST '/api/v1/sync/push' @{ ops = @(@{ client_op_id = 'local-2'; entity = 'transaction'; op = 'upsert'; base_rev = 1; payload = @{ id = $offlineId; type = 0; amount_cents = 7777; category_id = $catFood; occurred_at = $now } }) } -Token $tok2
Check '旧版本推送判定为 conflict' ($r.body.data.results[0].status -eq 'conflict')
Check '冲突时回传服务端版本' ($r.body.data.results[0].server.amount_cents -eq 3000)

Write-Host "`n[24] 删除账单（软删 + 墓碑）"
$r = Api DELETE "/api/v1/transactions/$tx1" -Token $tok1
Check '删除成功' ($r.ok)
$r = Api GET "/api/v1/sync?since=$cursor1" -Token $tok1
$tomb = $r.body.data.changes.transactions | Where-Object { $_.id -eq $tx1 }
Check '同步下发墓碑 deleted_at' ([bool]$tomb -and [bool]$tomb.deleted_at)

# ---------------- 管理后台 ----------------

Write-Host "`n[25] 管理后台登录"
$r = Api POST '/admin/api/auth/login' @{ account = $u1; password = $pwd }
Check '户主登录成功' ($r.ok -and $r.body.data.access_token)
$adminTok = $r.body.data.access_token
$r = Api POST '/admin/api/auth/login' @{ account = $u2; password = $pwd }
Check '管理员也能登录' ($r.ok)
$adminTok2 = $r.body.data.access_token

Write-Host "`n[26] 单家庭概览"
$r = Api GET '/admin/api/overview' -Token $adminTok
Check '概览返回 200' ($r.ok)
Check '家庭成员数为 2' ($r.body.data.members -eq 2)
Check '概览包含当前家庭' ($r.body.data.family.name -eq '我们的家·测试')
Check '账目总数 >= 2' ($r.body.data.transactions -ge 2)

Write-Host "`n[27] 后台成员列表 / 账目"
$r = Api GET '/admin/api/members' -Token $adminTok
Check '成员列表有 2 人' ($r.body.data.total -eq 2)
$r = Api GET '/admin/api/transactions' -Token $adminTok
Check '后台能查到家庭账目' ($r.body.data.items.Count -ge 1)

Write-Host "`n[28] 审计日志"
$r = Api GET '/admin/api/audit-logs' -Token $adminTok
Check '审计日志有记录' ($r.body.data.items.Count -gt 0)
Check '包含注册审计' (@($r.body.data.items | Where-Object { $_.action -eq 'auth.register' }).Count -gt 0)

Write-Host "`n[29] 令牌隔离：App 令牌打不进管理接口"
$r = Api GET '/admin/api/overview' -Token $tok1
Check 'App 令牌访问管理接口被拒 (401)' ((-not $r.ok) -and $r.status -eq 401)

Write-Host "`n[30] 封禁 / 解禁成员"
$r = Api PATCH "/admin/api/members/$uid2" @{ status = 0 } -Token $adminTok
Check '封禁成功' ($r.ok)
$r = Api POST '/api/v1/auth/login' @{ account = $u2; password = $pwd }
Check '被封禁成员无法登录 (403)' ((-not $r.ok) -and $r.status -eq 403)
$r = Api PATCH "/admin/api/members/$uid2" @{ status = 1 } -Token $adminTok
Check '解禁成功' ($r.ok)
$r = Api POST '/api/v1/auth/login' @{ account = $u2; password = $pwd }
Check '解禁后可正常登录' ($r.ok)

Write-Host "`n[31] 邀请码（后台）"
$r = Api POST '/admin/api/invites' @{ role = 'member'; expires_in_hours = 24 } -Token $adminTok
Check '后台生成邀请码成功' ($r.ok -and $r.body.data.code.Length -eq 6)

Write-Host "`n[32] 管理后台静态页"
$r = Api GET '/admin/'
Check '返回 HTML 页面' ($r.ok -and "$($r.body)" -like '*id="app"*')

Write-Host "`n[33] 账目 CSV 导出"
try {
    $csv = Invoke-WebRequest -Uri "$script:BaseUrl/api/v1/export.csv" `
        -Headers @{ Authorization = "Bearer $tok1" } -UseBasicParsing -TimeoutSec 15
    Check '导出返回 200' ($csv.StatusCode -eq 200)
    Check 'Content-Type 是 CSV' ("$($csv.Headers['Content-Type'])" -like 'text/csv*')
    $text = $csv.Content
    Check '带 UTF-8 BOM' ($text.Length -gt 0 -and [int][char]$text[0] -eq 0xFEFF)
    Check '表头正确' ($text.Contains('日期,类型,分类,账户,金额'))
    $lines = @($text -split "`r?`n" | Where-Object { $_.Trim() -ne '' })
    Check '至少有一行账目' ($lines.Count -ge 2)
    Check '包含成员离线记的账(30.00)' ($text.Contains('30.00'))
} catch {
    Check '导出返回 200' $false $_.Exception.Message
    Check 'Content-Type 是 CSV' $false
    Check '带 UTF-8 BOM' $false
    Check '表头正确' $false
    Check '至少有一行账目' $false
    Check '包含成员离线记的账(30.00)' $false
}

Write-Host "`n[34] 设备配对（批准后自动登录）"
$r = Api POST '/api/v1/auth/device/start' @{ device_uid = "smoke-device-$suffix"; device_key = "key-$suffix"; model = 'Smoke Device'; os_version = 'Test'; app_version = '1.0.0' }
Check '发起配对成功' ($r.ok -and $r.body.data.pair_code)
$pairCode = $r.body.data.pair_code
$pairSecret = $r.body.data.pair_secret
$r = Api GET '/admin/api/devices/pending' -Token $adminTok
$pending = $r.body.data.items | Where-Object { $_.pair_code -eq $pairCode }
Check '后台能看到待授权设备' ([bool]$pending)
if ($pending) {
    $r = Api POST "/admin/api/devices/$($pending.id)/approve" @{ user_id = $uid1; role = 'owner' } -Token $adminTok
    Check '批准设备成功' ($r.ok)
    $r = Api POST '/api/v1/auth/device/poll' @{ pair_code = $pairCode; pair_secret = $pairSecret }
    Check '设备拿到令牌' ($r.ok -and $r.body.data.access_token)
    $deviceTok = $r.body.data.access_token
    $r = Api GET '/api/v1/me' -Token $deviceTok
    Check '设备会话进入唯一家庭' ($r.ok -and $r.body.data.family.name -eq '我们的家·测试')

    $r = Api POST '/api/v1/auth/device/login' @{ device_key = "key-$suffix" }
    Check '已授权设备可免配对直接登录' ($r.ok -and $r.body.data.access_token)

    $r = Api POST '/api/v1/auth/device/start' @{ device_uid = "smoke-device-$suffix"; device_key = "key-$suffix"; model = 'Smoke Device'; os_version = 'Test'; app_version = '1.0.0' }
    Check '已授权设备再次发起配对被拒绝 (409)' ((-not $r.ok) -and $r.status -eq 409)
}

Write-Host "`n=== 结果：$script:pass 通过 / $script:fail 失败 ===" -ForegroundColor Cyan
Stop-TempServer
if ($script:fail -gt 0) { exit 1 }
