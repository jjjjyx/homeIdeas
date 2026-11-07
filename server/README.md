# server · 家庭记账本后端

Go 1.27 + SQLite（modernc，纯 Go 无 CGO）实现，单二进制部署。

## 目录

```
cmd/api/            启动入口（配置 → 迁移 → 路由 → 优雅退出）
cmd/backup/         SQLite 在线备份工具（VACUUM INTO + 保留份数）
cmd/devdump/        开发期小工具：直接看库里的用户/家庭/邀请码
internal/config/    环境变量配置
internal/domain/    实体、枚举、错误码
internal/store/     SQLite 访问层 + 迁移（embed，启动自动执行）
internal/service/   业务逻辑（认证、家庭、账本、同步、统计、导出、管理）
internal/httpapi/   路由、中间件、DTO、静态资源
internal/backup/    备份逻辑
internal/importer/  账单文件解析（微信/支付宝/云闪付/银行，GBK + 加密 zip）
internal/mailer/    IMAP 拉取与附件提取
internal/pkg/       id(UUIDv7)、密码哈希、JWT、secretbox(AES-GCM)
deploy/             systemd / Caddy / 备份脚本 / 部署说明
scripts/smoke.ps1   端到端接口冒烟测试（自带临时实例）
webassets/          管理后台构建产物（embed 进二进制）
```

### 账单导入踩过的坑（都是真机上验证出来的）

| 坑 | 现象 | 处理 |
|---|---|---|
| 邮件头是 GBK | `unhandled charset "gbk"`，整封邮件解析出 0 个部件 | 引入 `go-message/charset` 注册解码器 |
| 附件名 RFC2047 | 文件名是 GB18030 编码且跨行折叠，`looksLikeBill` 判不出来 | 按类型断言 + `mime.ParseMediaType` 兜底 |
| 加密 zip 空密码 | `yeka/zip` 直接 nil panic，后台轮询会把进程带崩 | 显式判断 `IsEncrypted() && password==""`，另加 `recover` |
| 首次同步方向 | `lastUID=0` 时从 UID 1 开始，先处理的是最早的历史邮件 | 首次只扫最近 10 封 |
| 支付宝账单口径 | 155 行里 90 行是「不计收支」（余额宝转入转出、还款、转账） | 按 `收/支` 列排除，否则支出翻倍 |
| 微信账单形态 | 没有附件，正文里是**下载链接**（7 天有效），下载后是加密 zip 里套 **xlsx** | 支持链接下载（域名白名单）+ 自研 xlsx 解析 |
| Excel 序列号 | 微信 xlsx 的「交易时间」是 `46326.73` 这种天数 | 按 1899-12-30 起算转时间，固定 UTC+8 |
| 退款行 | 微信/支付宝把原交易和退款写成两行，状态都是「已退款」 | **都保留**，净额才对；只跳过「关闭/失败/撤销」 |
| 密码各不相同 | 支付宝一份密码、微信另一份 | 密码按邮件存，不是全局一个 |

## 运行

```powershell
$env:APP_ENV='dev'
$env:HTTP_ADDR=':8088'
$env:DB_PATH='./data/app.db'
$env:JWT_SECRET='dev-secret-change-me-123456'
go run ./cmd/api
```

编译单文件：

```powershell
go build -trimpath -ldflags "-s -w" -o bin/ledger-server.exe ./cmd/api
```

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `APP_ENV` | `dev` | `prod` 时强制要求设置 `JWT_SECRET` |
| `HTTP_ADDR` | `:8080` | 监听地址 |
| `DB_PATH` | `./data/app.db` | SQLite 文件路径，目录自动创建 |
| `JWT_SECRET` | 开发默认值 | **生产必须改**，至少 16 位 |
| `ACCESS_TTL_MINUTES` | `15` | access token 有效期 |
| `REFRESH_TTL_HOURS` | `720` | refresh token 有效期 |
| `ALLOW_PUBLIC_REGISTER` | `false` | 是否允许无邀请码注册 |
| `CORS_ORIGINS` | `*` | 逗号分隔；生产建议填管理后台域名 |
| `DATA_KEY` | 同 `JWT_SECRET` | 加密邮箱密码/解压码的主密钥，**换掉会导致已存密文失效** |
| `MAIL_POLL_MINUTES` | `15` | 账单邮箱轮询间隔，0 表示关闭定时拉取 |

## 测试

```powershell
# 单元测试（权限、邀请码、跨家庭越权、私密账、同步游标、冲突、墓碑）
go test ./...

# 端到端：自带临时服务实例（临时库 + 随机端口），跑完自动清理，可重复执行
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke.ps1

# 打到已经在跑的服务（注意：该库必须是空的，否则首个用户不是引导账号）
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke.ps1 -BaseUrl http://127.0.0.1:8088
```

冒烟脚本覆盖 30 项：注册/登录/刷新/登出、建房、邀请、加入、角色、越权、邀请码复用、错误密码等。

> 注意：脚本用 Windows PowerShell 5.1 编写，已显式按 UTF-8 发送请求体；
> 文件需保留 UTF-8 BOM，否则 5.1 会把中文按 ANSI 解析导致语法错误。

## 已完成

- **M0**：配置、结构化日志、SQLite 连接（WAL + 单连接）、嵌入式迁移、健康检查、优雅退出、CORS、请求 ID、panic 恢复
- **M1**：注册（首用户引导 + 邀请码）、登录、刷新、登出、家庭 CRUD、邀请码（一次性、可设角色与有效期）、成员列表/改角色/移除/退出、RBAC + 数据域校验、审计日志、默认分类与账户种子
- **M2**：账目增删改查（软删墓碑）、`/sync` 增量拉取（rev 游标）、`/sync/push` 批量推送（applied/conflict/rejected）、月度汇总 / 分类占比 / 趋势统计
- **M3**：管理后台 API（`/admin/api`，独立 `scope=admin` 令牌）、平台概览、用户封禁/解禁（同步吊销 refresh token）、家庭与账目查询、审计日志；Vue 前端构建产物通过 `webassets` embed 进二进制
- **M5（部分）**：CSV 导出（带 BOM、私密账过滤）、`VACUUM INTO` 在线备份工具（`bin/ledger-backup`）、systemd + Caddy + 备份脚本（`deploy/`）
- **账单导入**：微信/支付宝/云闪付/银行 CSV 解析（GBK、表头定位、不计收支排除、加密 zip）、`(family, external_id)` 去重、商户名→分类规则；IMAP 邮箱自动拉取 + 解压码补填重试

测试规模：`go test ./...` **24 个用例**，`scripts/smoke.ps1` **83 项**端到端检查（脚本自带临时服务实例，可重复执行）。

## 发布

仓库根目录 `build-release.ps1` 一条命令产出可部署产物：

```powershell
.\build-release.ps1                  # linux/amd64
.\build-release.ps1 -GOOS windows    # 本机 Windows
```

输出到 `dist/`：`ledger-server`（含管理后台静态资源）+ `ledger-backup`。
部署步骤见 `deploy/README.md`。

## 设计要点

1. **单连接 + WAL**：家庭规模下以简单正确优先，彻底避开 `SQLITE_BUSY`；将来要提并发改成读写双池即可。
2. **数据域校验收口在 service 层**：`mustMember()` 是唯一入口，跨家庭一律 403，不暴露资源是否存在。
3. **邀请码先核销再加入**：`UPDATE ... WHERE used_by IS NULL` 是原子条件更新，并发下只有一个请求成功。
   初版曾把未使用写成空串导致条件永远不命中、邀请码可无限复用，已修复并有回归测试。
4. **时间戳统一 Unix 毫秒**，金额统一「分」，与 App 端一致。
5. **rev 递增**：家庭级同步版本号，M2 的增量同步游标。

## 压测

```powershell
go run ./cmd/loadtest -writers 8 -batches 40 -batch 5 -readers 4
```

自带临时服务实例（临时库 + 随机端口），跑完清理。2026-11-01 本机（16 核 / 31 GB，Windows）实测：

| 场景 | 请求 | 失败 | 写 p95 | 读 p95 | 吞吐 | 数据完整 |
|---|---|---|---|---|---|---|
| 8 写 × 40 批 × 5 条 + 4 读 | 320 写 / 1280 读 | **0** | 185 ms | 5.2 ms | **698 条/秒** | 1600/1600 ✓ |
| 24 写 × 30 批 × 5 条 + 8 读 | 720 写 / 5760 读 | **0** | 189 ms | 14.3 ms | **672 条/秒** | 3600/3600 ✓ |

结论：

- 写入吞吐稳定在 **~680 条/秒**，并发加到 24 也没出现 `SQLITE_BUSY`，`busy_timeout=5000` + 单写连接策略有效。
- 读几乎不受写影响（p95 个位数毫秒）。
- 家庭场景的真实峰值是「几个人同时点保存」，远低于压测强度；SQLite 完全够用。
- 吞吐上限来自「一条 op 一个事务 + 每行一次 `BumpRev`」。这里刻意保持 **rev 每行唯一**：如果同批共享一个 rev，分页拉取时客户端可能把 cursor 推过同名 rev 的行，导致漏数据。

## 下一步

- **M4**：Flutter App 接入（登录、家庭、本地缓存、Outbox、同步状态）
- **M5**：部署（systemd + Caddy）、`VACUUM INTO` 定时备份、CSV 导出、压测
