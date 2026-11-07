# 部署

目标：一台 1C1G 的小服务器跑全部东西——Go 单二进制（含管理后台静态页）+ SQLite 文件。

## 1. 构建

```powershell
cd server

# 先把管理后台打包进 webassets/dist
cd ..\admin; npm run build; cd ..\server

# 交叉编译到 Linux（amd64）
$env:GOOS='linux'; $env:GOARCH='amd64'
go build -trimpath -ldflags "-s -w" -o dist/ledger-server ./cmd/api
go build -trimpath -ldflags "-s -w" -o dist/ledger-backup ./cmd/backup
```

产物：`dist/ledger-server`（≈18 MB）、`dist/ledger-backup`（≈15 MB）。

## 2. 服务器准备

```bash
sudo useradd -r -s /usr/sbin/nologin ledger
sudo mkdir -p /opt/family-ledger/data /etc/family-ledger
sudo chown -R ledger:ledger /opt/family-ledger

# 上传
scp dist/ledger-server dist/ledger-backup user@host:/tmp/
sudo mv /tmp/ledger-server /tmp/ledger-backup /opt/family-ledger/
sudo chmod +x /opt/family-ledger/ledger-*
sudo cp deploy/ledger-server.service /etc/systemd/system/
sudo cp deploy/backup.sh /opt/family-ledger/deploy-backup.sh && sudo chmod +x /opt/family-ledger/deploy-backup.sh
```

## 3. 配置

```bash
sudo tee /etc/family-ledger/env >/dev/null <<'EOF'
JWT_SECRET=换成 32 位以上随机串
ALLOW_PUBLIC_REGISTER=false
ADMIN_TTL_HOURS=8
CORS_ORIGINS=https://ledger.example.com
EOF
sudo chmod 600 /etc/family-ledger/env
sudo chown root:root /etc/family-ledger/env
```

生成随机密钥：

```bash
openssl rand -base64 32
```

## 4. 启动

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now family-ledger
sudo systemctl status family-ledger
curl -s localhost:8080/readyz
```

## 5. 反向代理 + HTTPS

```bash
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile   # 先改域名
sudo systemctl reload caddy
```

## 6. 每日备份

```bash
sudo crontab -u ledger -e
# 0 3 * * * /opt/family-ledger/deploy-backup.sh >> /var/log/ledger-backup.log 2>&1
```

备份用的是 `VACUUM INTO`，不锁库、快照一致；保留最近 14 份。

**恢复方法**：

```bash
sudo systemctl stop family-ledger
sudo cp data/backup/app-20261101-030000.000.db data/app.db
sudo rm -f data/app.db-wal data/app.db-shm
sudo systemctl start family-ledger
```

## 7. 升级

```bash
scp dist/ledger-server user@host:/tmp/
sudo systemctl stop family-ledger
sudo mv /tmp/ledger-server /opt/family-ledger/ledger-server
sudo chmod +x /opt/family-ledger/ledger-server
sudo systemctl start family-ledger
```

数据库迁移是**启动时自动执行**的（内嵌在二进制里），只需要保证提前备份过。

## 8. 首次使用

1. 系统零用户时，第一个注册的账号会**自动创建唯一家庭并成为户主**（App 启动页 →「已有账号 / 用邀请码登录」→「用邀请码注册」，邀请码留空；或直接调 `POST /api/v1/auth/register`）。
2. 户主用这个账号登录管理后台 `https://ledger.example.com/admin/`，可改家庭名、管理成员/设备。
3. 其他家人：管理员在后台「家庭成员 → 邀请码」（或 App 设置 → 邀请成员）生成 6 位邀请码，家人在 App 用邀请码注册加入；
   也可以继续用设备配对，批准后自动登录。

> `ALLOW_PUBLIC_REGISTER=false` 时，除首次初始化外，其他人必须凭邀请码注册或由管理员直接创建账号。

## 本地 Windows 运行

```powershell
$env:APP_ENV='dev'; $env:HTTP_ADDR=':8088'; $env:DB_PATH='./data/app.db'
$env:JWT_SECRET='dev-secret-change-me-123456'
.\bin\ledger-server.exe
```

备份：见 `deploy/backup.ps1`（里面有注册计划任务的命令）。

手机联调（USB）：

```powershell
adb reverse tcp:8088 tcp:8088
```

## 排错

| 现象 | 排查 |
|---|---|
| 502 | `systemctl status family-ledger`，看 journald 日志 |
| 数据库被锁 | 是否同时在跑两个实例；SQLite 单文件只能一个进程写 |
| 管理后台白屏 | 是否忘了 `npm run build`；`ls /opt/family-ledger` 确认二进制是带静态资源的新版本 |
| 迁移失败 | 先从备份恢复，再检查版本兼容性 |
