# 家庭记账本 · 架构设计 v1

> 目标：把当前「单机离线记账」改造成「家庭成员共用一本账」的多端系统。
> 本文只做设计，不含实现。文末列出需要你拍板的决策项。

---

## 1. 需求与约束

### 1.1 业务目标

| 能力 | 说明 |
|---|---|
| 多人共记 | 一个家庭一本（或多本）账，成员用各自账号登录，记的账互相可见 |
| 归属清晰 | 每笔账记录「谁记的」「哪个账户」「什么分类」 |
| 权限分层 | 户主 / 管理员 / 成员，删除家庭、管理成员、改分类要有权限 |
| 多端一致 | 手机端看到的数据与后台一致，A 记账 B 刷新能看到 |
| 管理后台 | 运营/你自己能管用户、家庭、账本数据、看审计日志 |
| 可部署 | 一台 1C1G 的小服务器 / NAS 就能跑，单文件数据库，好备份 |

### 1.2 规模假设（决定选型的核心）

- 单家庭 2～10 人；单家庭 10 年约 1～3 万笔记录。
- 初期 10～100 个家庭（自用 + 亲友），并发写峰值 < 5 QPS。
- 这个量级 **SQLite 完全够用**，瓶颈永远在网络和 UI，不在数据库。

### 1.3 非目标（v1 不做）

多币种、多账本合并、发票 OCR、银行直连、实时协同编辑（同一笔账两人同时改）、水平分库。

---

## 2. 总体架构

```
┌────────────────┐        ┌────────────────┐
│  Flutter App   │        │  Vue3 管理后台  │
│  (Android)     │        │  (Vite + TS)   │
│                │        │                │
│ 本地缓存 SQLite │        │ 纯在线，无缓存   │
│ + Outbox 队列   │        │                │
└───────┬────────┘        └───────┬────────┘
        │ HTTPS /api/v1           │ HTTPS /admin/api
        │ JSON + JWT              │ JSON + JWT(aud=admin)
        └───────────┬─────────────┘
                    ▼
        ┌───────────────────────────┐
        │      Nginx / Caddy        │  TLS、gzip、静态资源、限流
        └───────────┬───────────────┘
                    ▼
        ┌───────────────────────────────────────┐
        │        Go 单体服务（单二进制）           │
        │  ┌─────────┐ ┌─────────┐ ┌─────────┐  │
        │  │  API    │ │  同步    │ │  管理   │  │
        │  │  认证    │ │  引擎    │ │  后台   │  │
        │  └────┬────┘ └────┬────┘ └────┬────┘  │
        │       └───────────┼───────────┘       │
        │              service 层               │
        │                   ▼                   │
        │              store 层（Repository）    │
        └───────────────────┬───────────────────┘
                            ▼
                  ┌──────────────────┐
                  │ SQLite (WAL)     │  data/app.db
                  │ + 自动备份副本    │  data/backup/
                  └──────────────────┘
```

### 2.1 为什么是「单体 + 单库」

| 方案 | 评价 |
|---|---|
| **Go 单体 + SQLite**（选定） | 一个二进制、一个 db 文件，`scp` 上去就能跑；备份=复制文件；无运维负担 |
| Go 微服务 | 家庭规模下纯属自残，多进程多库，部署和排错成本翻倍 |
| Go + PostgreSQL | 技术上更"正统"，但要多养一个数据库服务；等真到多实例再迁 |
| BaaS（Supabase 等） | 省事但把数据放到别人服务器，家庭账本隐私敏感；且离线同步逻辑还是要自己写 |

**迁移预案**：store 层全部走 Repository 接口 + 手写 SQL（不用 ORM 特性），将来换 PostgreSQL 只改 `store/` 一个目录。触发迁移的信号：需要多实例部署、写入 QPS 持续 > 50、单库 > 10 GB。

---

## 3. 领域模型

### 3.1 实体关系

```
users ──< family_members >── families
  │                            │
  │                            ├──< categories
  │                            ├──< accounts
  │                            ├──< transactions >── categories
  │                            │                 └── accounts
  │                            ├──< budgets      └── 记录人 member
  │                            └──< audit_logs
  └──< refresh_tokens
```

### 3.2 核心表设计

> 所有表统一带：`id TEXT PRIMARY KEY`（UUIDv7，**客户端生成**）、`created_at`、`updated_at`、`deleted_at`（软删）、`rev INTEGER`（同步版本号）。

| 表 | 关键字段 | 说明 |
|---|---|---|
| `users` | account, password_hash, nickname, avatar, status, is_platform_admin | account 支持手机号或用户名 |
| `families` | name, owner_id, currency, invite_code, status | 一个家庭 = 一个独立数据空间 |
| `family_members` | family_id, user_id, role(owner/admin/member), joined_at | 联合唯一 (family_id,user_id) |
| `refresh_tokens` | user_id, token_hash, expires_at, revoked_at, device | 可吊销，支持多设备 |
| `categories` | family_id, name, type(0支出/1收入), icon_key, color, sort_order, is_system | 新家庭自动种一套默认分类 |
| `accounts` | family_id, name, kind(现金/储蓄卡/信用卡/支付宝/微信/其他), sort_order | 记账户，余额由流水推导，不单独存 |
| `transactions` | family_id, type, amount_cents, category_id, account_id, member_id, occurred_at, note, visibility | 见下方说明 |
| `budgets`（v2） | family_id, month, category_id, amount_cents | 预算提醒 |
| `audit_logs` | family_id, user_id, action, target_type, target_id, detail_json, ip | 谁在什么时候干了什么 |

**`transactions` 的三个关键设计**

1. `amount_cents INTEGER`：金额永远存「分」，不用浮点（沿用现有做法）。
2. `occurred_at`（账目发生日期）与 `created_at`（记录创建时间）分离：补记昨天的账时，排序按 occurred_at，审计按 created_at。
3. `visibility`（family / private）：家庭共享 + 个人私密两种。默认 family；private 的账只有 member_id 本人可见，接口层过滤。**这是多人记账最容易被忽略的需求**——家里总有人不想让另一半看到某笔支出。

### 3.3 索引

```
transactions (family_id, occurred_at DESC)     -- 首页列表
transactions (family_id, rev)                  -- 同步增量拉取
transactions (family_id, category_id, occurred_at) -- 统计
family_members (user_id)                       -- 我的家庭
categories (family_id, type, sort_order)
```

---

## 4. 后台服务设计

### 4.1 分层

```
httpapi  路由 / 中间件 / 参数校验 / DTO 转换     ← 只做协议层
   ↓
service  业务用例 / 权限校验 / 事务边界          ← 全部业务逻辑
   ↓
store    Repository 接口 + SQLite 实现 + 迁移    ← 只做持久化
```

铁律：**handler 里不写 SQL，store 里不做权限判断，service 里不碰 HTTP**。

### 4.2 目录结构

```
server/
├── cmd/api/main.go              启动、优雅退出
├── internal/
│   ├── config/                  环境变量 + 配置文件
│   ├── httpapi/
│   │   ├── router.go
│   │   ├── middleware/          认证、CORS、限流、恢复、日志、请求ID
│   │   └── handler/             auth / family / ledger / sync / stats / admin
│   ├── service/                 auth, family, ledger, sync, stats, admin
│   ├── store/
│   │   ├── sqlite/              db.go, tx.go, 各 repo 实现
│   │   └── migrations/*.sql     embed 进二进制，启动自动迁移
│   ├── domain/                  实体、枚举、错误码
│   └── pkg/                     id(uuidv7), clock, hash(argon2), validate, log
├── web/                         admin 构建产物（embed.FS 打包）
├── migrations/
└── go.mod
```

### 4.3 SQLite 使用规范（关键）

SQLite 的坑集中在并发写。规范如下：

| 项 | 做法 |
|---|---|
| 库连接 | `modernc.org/sqlite`（纯 Go，无 CGO，交叉编译友好） |
| 连接池 | 读写分离：`write DB` 限 1 条连接，`read DB` 池化；简单起见可先全局 `MaxOpenConns=1`（家庭规模下完全够） |
| PRAGMA | `journal_mode=WAL`、`busy_timeout=5000`、`synchronous=NORMAL`、`foreign_keys=ON` |
| 事务 | 一次业务操作 = 一个事务；账目+审计+rev 递增必须同事务 |
| 迁移 | `migrations/*.sql` 嵌入二进制，启动时按版本号顺序执行，只前进不回滚 |
| 备份 | 每天 `VACUUM INTO 'backup/app-YYYYMMDD.db'`（比 cp 安全，不锁库），保留 14 天；重要数据再异地同步一份 |

### 4.4 认证与鉴权

**双令牌 + 可吊销**

- `access_token`：JWT，15 分钟，无状态校验；载荷 `{uid, aud: "app"|"admin", exp}`。
- `refresh_token`：随机 256bit，**哈希后存库**，30 天，可吊销（改密码、退出登录、封号即失效）。
- App 与管理后台用同一个 `/auth/login`，但 `aud` 不同；管理接口中间件强制校验 `aud=="admin"`，App 令牌打不进后台。

**权限模型（RBAC + 数据域）**

| 角色 | 权限 |
|---|---|
| owner | 改家庭信息、邀请/移除成员、改角色、删除家庭、转账 |
| admin | 管理分类/账户、移除普通成员、删除任意账目 |
| member | 增删改**自己记的**账、看全家账（受 visibility 限制） |

所有请求先过 **数据域校验**：从 URL 的 `family_id` → 查 `family_members` → 当前用户是否属于该家庭。**这条校验必须写在 service 层且每个家庭相关接口都走**，否则就是越权漏洞（A 家庭用户改 URL 就能看 B 家庭数据）。

### 4.5 同步协议（离线优先的核心）

App 不能因为没网就不能记账，所以采用 **server-authoritative + 本地缓存 + 增量同步**：

```
客户端                                   服务端
  │ 1. POST /sync/push  [outbox 中的操作]  │
  │ ────────────────────────────────────▶ │ 逐条校验 → 写库 → rev+1
  │ ◀──────────────── {results[]}         │ 返回 applied / conflict
  │ 2. GET /sync?since=<rev>              │
  │ ────────────────────────────────────▶ │ 查 rev>cursor 的行（含墓碑）
  │ ◀────────── changes + new cursor      │
```

**四条设计约定**

1. **ID 由客户端生成（UUIDv7）**：离线也能建账，联网直接 upsert，天然幂等（重试不会重复插入）。
2. **`rev` 是单调递增的同步游标**：每个家庭维护一个计数器，任何一次写入让 rev+1。客户端只记 `max(rev)`，拉取 `rev > cursor ORDER BY rev`。比时间戳可靠（不依赖时钟同步）。
3. **删除即墓碑**：`deleted_at` 置位并 `rev+1`，客户端拉到后本地删除。物理删除只在后台管理端触发。
4. **冲突策略：字段级 LWW**。同一笔账被两个人改，按服务端接收顺序保留最后一次（`updated_at` 早的丢弃），并把被覆盖的版本回给客户端提示。家庭场景下冲突概率极低（各自记各自的账），优先保证可用性。

### 4.6 API 草案（v1）

**认证**

```
POST /api/v1/auth/register      {account, password, nickname, invite_code?}
POST /api/v1/auth/login         {account, password} → tokens + user
POST /api/v1/auth/refresh       {refresh_token} → 新 access_token
POST /api/v1/auth/logout        {refresh_token}
GET  /api/v1/me                 当前用户 + 所属家庭列表
```

**家庭**

```
POST   /api/v1/families                  创建家庭（自动成为 owner）
GET    /api/v1/families                  我的家庭
GET    /api/v1/families/{id}             详情 + 成员
PATCH  /api/v1/families/{id}             改名
POST   /api/v1/families/{id}/invites     生成邀请码（可设过期/角色）
POST   /api/v1/families/join             凭邀请码加入
GET    /api/v1/families/{id}/members
PATCH  /api/v1/families/{id}/members/{uid}   改角色
DELETE /api/v1/families/{id}/members/{uid}   移除成员
POST   /api/v1/families/{id}/leave       退出家庭
```

**同步**

```
GET  /api/v1/families/{id}/sync?since={rev}&limit=500
POST /api/v1/families/{id}/sync/push    {ops:[{entity,op,payload}]}
GET  /api/v1/families/{id}/events       SSE，家庭成员有新流水时推送
```

**统计与导出**

```
GET /api/v1/families/{id}/stats/summary?month=2026-11
GET /api/v1/families/{id}/stats/categories?month=&type=
GET /api/v1/families/{id}/stats/trend?months=6
GET /api/v1/families/{id}/export.csv?from=&to=
```

**管理后台（独立前缀 + 独立 aud）**

```
POST  /admin/api/auth/login
GET   /admin/api/overview                 平台指标：用户数/家庭数/记录数
GET   /admin/api/users?q=&page=
PATCH /admin/api/users/{id}               封禁/重置密码
GET   /admin/api/families?q=
GET   /admin/api/families/{id}/transactions
GET   /admin/api/audit-logs?actor=&action=&from=
POST  /admin/api/category-templates       维护默认分类模板
```

统一响应体 `{code, message, data, request_id}`；错误码分段（1xxx 参数、2xxx 认证、3xxx 权限、4xxx 业务、5xxx 系统）。

---

## 5. 管理后台设计（Vite + Vue3）

### 5.1 技术栈

| 层 | 选型 | 理由 |
|---|---|---|
| 框架 | Vue 3.5 + TypeScript | 组合式 API，类型安全 |
| 构建 | Vite 7 | 冷启动快，HMR 秒级 |
| 路由 | Vue Router 4 | 动态路由 + 权限守卫 |
| 状态 | Pinia | 轻量，TS 友好 |
| UI | Element Plus | 后台组件最全（表格/表单/树），中文文档好 |
| 图表 | ECharts | 看板 |
| 请求 | Axios + 拦截器 | 统一 token/错误码处理 |
| 工具 | dayjs / unocss（可选） | |

### 5.2 页面结构

```
/login
/                    工作台（平台概览：用户数、家庭数、今日新增记录、金额分布）
/users               用户列表（搜索/封禁/重置密码）
/families            家庭列表 → 详情（成员、账目、统计）
/ledger              账目查询（按家庭/时间/分类/成员筛选，可导出 CSV）
/categories          默认分类模板维护
/audit               操作审计日志
/settings            系统设置（是否开放注册、邀请码策略、备份状态）
```

### 5.3 关键约束

- 后台**不做**复杂业务写入（不代用户记账），只做查询、治理、模板维护，避免与 App 的同步逻辑打架。
- 与 API 同域部署：Go 用 `embed.FS` 打包 `web/` 产物，`/admin` 直接返回 `index.html`，省一个 Nginx location，也避免跨域。
- 本地开发：`vite.config.ts` 里 `proxy: {'/admin/api': 'http://localhost:8080'}`。

---

## 6. App 端改造（Flutter）

### 6.1 从「本地库」到「本地缓存 + 同步」

现有代码的 `LedgerStore` 直接读写 `AppDatabase`。改造后：

```
UI 层（不变）
   ↓ 读：永远读本地缓存（秒开）
   ↓ 写：写本地 + 塞进 outbox + 立即刷新 UI（乐观更新）
LedgerRepository
   ├── LocalCache  (sqflite/drift，与现在的表结构几乎一致，多几列同步字段)
   ├── Outbox      (待同步操作队列，含重试次数/下次重试时间)
   └── SyncEngine  (push → pull → 更新游标，网络恢复/前台切换时触发)
          ↓
      ApiClient (dio + 拦截器：自动刷新 token、统一错误)
```

### 6.2 新增页面

| 页面 | 说明 |
|---|---|
| 登录/注册 | 账号密码，可选邀请码 |
| 家庭选择/创建 | 一个用户可加入多个家庭，首页顶部可切换 |
| 邀请成员 | 生成 6 位邀请码 + 二维码，可分享到微信 |
| 成员管理 | 只有 owner/admin 可见 |
| 流水详情 | 显示「谁记的」+ 头像，可编辑规则按角色 |
| 设置 | 账号信息、退出登录、离线队列状态（"3 条待同步"） |

### 6.3 本地数据迁移

老用户（现在的 4 笔测试数据）升级时：首次登录后提示「把本机账单导入家庭账本」，一次性把本地 `entries` 转成 `transactions` 走 push 上传，成功后清空本地。

---

## 7. 安全设计

| 面 | 措施 |
|---|---|
| 传输 | 强制 HTTPS（Caddy 自动证书），HSTS |
| 密码 | argon2id 哈希（内存 64MB / 迭代 3），登录失败按 IP+账号限流（5 次/分钟，指数退避） |
| 令牌 | access 15min + refresh 可吊销；refresh 只存哈希；设备变更需重新登录 |
| 越权 | 每个 family 接口强制 membership 校验；单元测试覆盖「跨家庭访问必须 403」 |
| 注入 | 全部参数化 SQL，禁止字符串拼接 |
| 上传 | v1 不做图片附件；若做则限制大小 + 类型 + 重编码 |
| 审计 | 登录、改密码、移除成员、删除账目、后台操作全记 `audit_logs` |
| 备份 | 数据库文件权限 600；备份目录不放 Web 根目录 |
| 敏感配置 | 密钥走环境变量/`config.yaml`（600），不入库不入 git |

---

## 8. 部署与运维

### 8.1 单机部署（推荐起步）

```
/opt/family-ledger/
├── ledger-server          # go build 产物（含 admin 前端）
├── config.yaml
└── data/
    ├── app.db             # SQLite
    └── backup/
```

- systemd 托管，`Restart=always`。
- Caddy 反代：`/` → :8080，自动 HTTPS。
- 备份：cron 每天 03:00 执行 `VACUUM INTO`，保留 14 天；每周 rsync 一份到对象存储/另一台机器。
- 监控：`/healthz`（存活）+ `/readyz`（db 可读写）+ 结构化 JSON 日志（slog）；可选 Prometheus `/metrics`。

### 8.2 容量估算

| 项 | 估算 |
|---|---|
| 单条 transaction | ≈ 300 B（含索引） |
| 100 家庭 × 10 年 × 2 万笔 | 200 万行 ≈ 700 MB |
| 峰值写入 | < 5 QPS（SQLite 单写者可轻松到 500+ TPS） |
| 内存 | 50～100 MB |

**结论：一台 1C1G 的机器足够跑到几百个家庭。**

---

## 9. 仓库组织

建议 **monorepo**（一个人维护三个端，跨端改接口时一次提交）：

```
homeIdeas/
├── server/            Go 服务（含 admin 静态产物）
├── admin/             Vue3 管理后台
├── app/               Flutter App（由现有 family_ledger 迁移而来）
├── docs/              架构、API、数据库设计
└── README.md
```

若坚持三个独立仓库：`family-ledger-server` / `family-ledger-admin` / `family-ledger-app`，用 `docs/api-v1.md` 作为契约。

---

## 10. 里程碑

| 阶段 | 内容 | 产出 | 状态 |
|---|---|---|---|
| M0 | 骨架：配置、日志、迁移、健康检查、CI | 能跑的 hello world | ✅ 完成 |
| M1 | 认证 + 用户 + 家庭 + 邀请码 + 成员权限 | curl 可完成注册登录建房邀请 | ✅ 完成 |
| M2 | 账本 CRUD + 同步引擎（push/pull/rev/墓碑）+ 统计接口 | 同步协议跑通，有单元测试 | ✅ 完成 |
| M3 | 管理后台：登录、用户、家庭、账目、审计 | 后台可查可用 | ✅ 完成 |
| M4 | App 接入：登录、家庭、本地缓存、outbox、同步状态 | 两部手机同记一本账 | ✅ 真机验证通过 |
| M5 | 部署、备份、CSV 导出、压测、文档 | 上线可用 | ✅ 完成 |

合计约 **2 周**（单人全职）；按 M0→M5 顺序推进，每个里程碑结束给一次可验证的产物。

进度（2026-11-01）：**M0–M5 全部完成**。后端 35 个 API + 8 个管理接口，端到端检查 83 项全绿，App 已在小米真机上完成登录/同步/记账闭环验证，压测 ~680 条/秒无失败。

---

## 11. 风险清单

| 风险 | 影响 | 应对 |
|---|---|---|
| SQLite 单写者 | 高并发写变慢 | 家庭规模下不构成问题；预留 PG 迁移（Repository 隔离） |
| 离线冲突 | 数据被覆盖 | LWW + 返回冲突提示；同一笔账的并发编辑概率极低 |
| 时区/日期 | 记到错误的月份 | 服务端存 UTC，`occurred_at` 用「日期+12:00 本地」；客户端展示按设备时区 |
| 家庭数据越权 | 严重安全事故 | service 层强制 membership 校验 + 专项测试用例 |
| 单文件损坏 | 全量丢数据 | WAL + 每日 VACUUM INTO + 异地副本；启动时 `PRAGMA integrity_check` |
| 手机号登录需短信 | 成本/合规 | v1 用「用户名+密码」，手机号仅作可选字段 |

---

## 12. 已确认的决策（2026-11-01）

| # | 决策 | 结论 | 对实现的影响 |
|---|---|---|---|
| 1 | 部署形态 | **自用为主**（多家庭代码结构，但默认关闭自由注册） | 注册必须邀请码；加一个 `system_config.allow_public_register` 开关；不做计费/配额 |
| 2 | 离线能力 | **必须离线可记** | 本地缓存 + Outbox + rev 增量同步全部要做；App 需处理「待同步 N 条」状态与失败重试 |
| 3 | 登录方式 | **用户名 + 密码**（手机号留作可选字段） | 无需短信服务商；密码重置走管理员后台操作 |
| 4 | 私密账 | **需要** | `transactions.visibility`；查询层按 `visibility='family' OR member_id=me` 过滤；统计接口同样过滤 |
| 5 | 账本数量 | **v1 一本账 + 标签**，多账本 v2 | `transactions.tags`（JSON/关联表）预留，不做多账本切换 |

### 12.1 因此锁定/排除的项

- **锁定**：邀请码注册、outbox 同步、UUIDv7 主键、rev 游标、墓碑删除、visibility 过滤、三角色 RBAC、SSE 通知（可选但保留）。
- **排除**：短信验证码、微信登录、支付/订阅、多币种、多账本、图片凭证、跨家庭共享账本。
- **预留**：Repository 接口（为将来换 PG）、`tags` 字段（为多账本/标签）、`accounts` 表（为资产负债统计）、`budgets` 表（v2 预算提醒）。

