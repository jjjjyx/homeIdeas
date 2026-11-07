# 家庭记账本 · 架构设计（单家庭版）

> 一个部署只服务一个家庭：没有家庭列表、创建/加入/切换家庭。
> 成员通过**邀请码注册**（主路径）或管理员在后台直接创建加入。
> 家庭在首次初始化时创建，之后所有数据（账本、分类、账户、邮箱、规则、审计）都围绕它运行。
> 本文替代早期的「多家庭」设计。

---

## 1. 目标与边界

### 1.1 业务目标

| 能力 | 说明 |
|---|---|
| 一个家庭一本账 | 所有成员共享同一份账本、同一套分类/账户 |
| 多人共记 | 成员用自己账号（设备配对）登录，记的账互相可见 |
| 归属清晰 | 每笔账记录「谁记的」「哪个账户」「什么分类」，支持家庭/个人归属 |
| 权限分层 | 户主 / 管理员 / 成员；户主/管理员可在后台管理成员与家庭 |
| 多端一致 | App 离线优先，增量同步；后台只读治理，不代记账 |
| 管理后台 | 单家庭概览、账目、成员、设备、审计 |
| 可部署 | 单二进制 + SQLite，1C1G 可跑，备份=复制文件 |

### 1.2 规模假设

- 单家庭 2～10 人；10 年约 1～3 万笔记录。
- 一个部署一个家庭，不存在跨家庭数据隔离需求。
- SQLite 单写者足够；瓶颈在网络和 UI。

### 1.3 非目标

多家庭/多租户、多币种、多账本合并、发票 OCR、银行直连、实时协同编辑、水平分库。

---

## 2. 总体架构

```
┌────────────────┐        ┌────────────────┐
│  Flutter App   │        │  Vue3 管理后台  │
│  (Android)     │        │  (Vite + TS)   │
│ 本地缓存+Outbox │        │  纯在线，无缓存  │
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
        │  httpapi → service → store → SQLite   │
        └───────────────────┬───────────────────┘
                            ▼
                  ┌──────────────────┐
                  │ SQLite (WAL)     │  data/app.db
                  │ + 每日备份副本    │  data/backup/
                  └──────────────────┘
```

### 2.1 为什么是「单体 + 单库 + 单家庭」

- **单家庭**是产品前提，不是权限补丁：去掉 family_id 路径参数、家庭列表和跨家庭校验后，
  整条链路（API、同步、后台、App）都更短，越权面也更小。
- Go 单体 + SQLite：一个二进制、一个 db 文件，`scp` 上去就能跑；备份=复制文件。
- store 层仍用 Repository 风格手写 SQL，将来要换 PostgreSQL 只改 `store/`。

---

## 3. 领域模型

### 3.1 实体关系

```
users ──< family_members >── family (唯一一行)
                               │
                               ├──< categories
                               ├──< accounts
                               ├──< transactions >── categories
                               │                 └── accounts
                               ├──< import_batches / category_rules
                               ├──< mail_accounts / mail_messages
                               └──< audit_logs

users ──< refresh_tokens
users ──< devices（配对授权）
```

> `family_id` 仍保留在数据表里，用于同步版本 `rev` 和祖先数据的归属，
> 但系统保证只有一行有效家庭；API/UI 不再暴露家庭 ID。

### 3.2 核心表

| 表 | 关键字段 | 说明 |
|---|---|---|
| `users` | account, password_hash, nickname, status | 家庭成员账号 |
| `families` | name, owner_id, currency, rev | **唯一家庭**；`rev` 是同步游标根 |
| `family_members` | family_id, user_id, role | 唯一家庭下的成员；角色 owner/admin/member |
| `refresh_tokens` | user_id, token_hash, device_id, expires_at | 可吊销 |
| `categories` | name, type, icon_key, color, sort_order | 预置 19 个，可按需补 |
| `accounts` | name, kind, sort_order | 现金/支付宝/微信/银行卡 |
| `transactions` | type, amount_cents, category_id, account_id, member_id, occurred_at, note, visibility, source, external_id, scope, owner_id | 见下 |
| `import_batches` | source, file_name, total/imported/skipped/failed | 导入批次 |
| `category_rules` | keyword, category_id | 商户 → 分类 |
| `mail_accounts` | host/port/username/password_enc, last_uid | **唯一**账单邮箱 |
| `mail_messages` | message_uid, status, password_enc | 邮件去重与解压码 |
| `devices` | device_uid, user_id, role, status, pair_code | 配对授权 |
| `audit_logs` | user_id, action, target, detail, ip | 审计 |
| `system_config` | key/value | 运行配置 |

**transactions 的三个关键设计**

1. `amount_cents INTEGER`：金额永远存「分」。
2. `occurred_at`（账目发生）与 `created_at`（记录创建）分离。
3. `visibility`（family/private）：私密账只有本人可见；列表、同步、统计、导出四个出口统一过滤。

### 3.3 索引

```sql
transactions (family_id, occurred_at DESC)
transactions (family_id, rev)
transactions (family_id, category_id, occurred_at)
transactions (family_id, external_id) UNIQUE  -- external_id 非空时
family_members (user_id)
categories (family_id, type, sort_order)
accounts (family_id)
category_rules (family_id, keyword) UNIQUE
mail_messages (family_id, message_uid) UNIQUE
audit_logs (created_at DESC)
refresh_tokens (user_id)
```

---

## 4. 后端分层

```
httpapi  路由 / 中间件 / 参数校验 / DTO        ← 只做协议层
   ↓
service  业务用例 / 权限校验 / 事务边界         ← 全部业务逻辑
   ↓
store    Repository + SQLite + 迁移            ← 只做持久化
```

铁律：handler 不写 SQL，store 不做权限判断，service 不碰 HTTP。

### 4.1 单家庭的实现方式

- 鉴权中间件解析令牌后，把**唯一家庭 ID**放进请求上下文（`familyIDFrom`）。
- 所有业务接口从上下文取家庭 ID，而不是从 URL 取；对外没有 `/families/{id}` 这类路径。
- 家庭服务只提供 `Detail / Rename / CreateInvite / AddMember / SetRole / RemoveMember`，没有 Create/Join/Leave 家庭。
- 成员加入有两条路：**邀请码注册**（主路径）或管理员在后台直接创建账号；两者都只能加入唯一家庭。

### 4.2 SQLite 使用规范

| 项 | 做法 |
|---|---|
| 驱动 | `modernc.org/sqlite`（纯 Go，无 CGO） |
| 连接 | `MaxOpenConns=1` 串行化读写，避开 SQLITE_BUSY |
| PRAGMA | `journal_mode=WAL`、`busy_timeout=5000`、`synchronous=NORMAL`、`foreign_keys=ON` |
| 事务 | 一次业务操作 = 一个事务；账目+审计+rev 递增同事务 |
| 迁移 | `migrations/*.sql` 嵌入二进制，启动时按版本号顺序执行，只前进 |
| 备份 | 每天 `VACUUM INTO`，保留 14 天 |

---

## 5. 认证与权限

### 5.1 双入口

- **App**：设备配对。新设备 `device/start` 拿配对码 → 管理员在后台批准 → App 轮询拿到令牌。
  家人不用记账号密码；重装后可以重新配对到同一个身份。
- **管理后台**：账号密码登录，只允许唯一家庭的 `owner` / `admin`，令牌 `aud=admin`、8 小时。
- 首次初始化：`POST /api/v1/auth/register` 在系统零用户时创建家庭+户主。
- 之后成员用**邀请码注册**加入同一个家庭（邀请码一次性、可设角色）；也可以由户主/管理员在后台直接创建账号。
- 没有「创建/加入/切换家庭」，也没有自助注册（除非显式打开 `ALLOW_PUBLIC_REGISTER`）。

### 5.2 权限模型

| 角色 | 能力 |
|---|---|
| owner | 改家庭名、生成邀请码（含 admin 邀请）、添加/移除成员、调整角色、后台全部查看 |
| admin | 改家庭名、生成 member 邀请码、添加成员、添加/编辑分类账户、导入、AI 分类；只能移除普通成员 |
| member | 记自己的账、看家庭账（受 visibility 限制）、导出自己的可见数据 |

### 5.3 数据可见性

- 所有账目查询都带 `(visibility <> 'private' OR member_id = 当前用户)`。
- 普通成员只能改/删自己记的账；私密账只有本人可操作。
- 后台账目列表也遵循同一过滤，管理员看不到成员的私密账。

---

## 6. 同步协议（离线优先）

```
客户端                                   服务端
  │ 1. POST /sync/push  [outbox 中的操作]  │
  │ ────────────────────────────────────▶ │ 逐条校验 → 写库 → rev+1
  │ ◀──────────────── {results[]}         │ applied / conflict / rejected
  │ 2. GET /sync?since=<rev>              │
  │ ────────────────────────────────────▶ │ 查 rev>cursor 的行（含墓碑）
  │ ◀────────── changes + new cursor      │
```

约定：

1. **ID 由客户端生成（UUIDv7）**：离线建账，联网 upsert，天然幂等。
2. **`rev` 是单调递增游标**：任何写入让唯一家庭的 `rev+1`；客户端只记 `max(rev)`。
3. **删除即墓碑**：`deleted_at` 置位 + `rev+1`，客户端拉到后本地删除。
4. **冲突策略：字段级 LWW**：按服务端接收顺序保留最后一次；冲突时回传服务端版本。

---

## 7. API 摘要

| 分组 | 路径 |
|---|---|
| 认证 | `/api/v1/auth/{register,login,refresh,logout,device/start,device/poll}` |
| 当前用户 | `GET /api/v1/me` |
| 家庭/成员 | `GET/PATCH /api/v1/family`；`POST /api/v1/family/invites`；`GET/POST /api/v1/family/members`；`PATCH/DELETE /api/v1/family/members/{uid}` |
| 账目 | `GET/POST /api/v1/transactions`；`PATCH/DELETE /api/v1/transactions/{tid}` |
| 同步 | `GET /api/v1/sync`；`POST /api/v1/sync/push` |
| 统计/导出 | `/api/v1/stats/{summary,categories,trend,scopes}`；`GET /api/v1/export.csv` |
| 导入/邮箱/分类 | `/api/v1/{imports,category-rules,mail,classify}` |
| 拆单 | `/api/v1/transactions/{tid}/{split,unsplit,children}` |
| 管理后台 | `/admin/api/{auth/login,me,overview,family,members,transactions,audit-logs,devices}` |

统一响应 `{code, message, data, request_id}`；错误码分段（1xxx 参数、2xxx 认证、3xxx 权限、4xxx 业务、5xxx 系统）。

---

## 8. 管理后台

技术栈：Vue 3 + Vite + TypeScript + Element Plus + Pinia + Vue Router，构建产物 embed 进 Go 二进制。

页面：

```
/login
/            家庭概览（家庭名、成员数、账目数、累计支出、今日新增）
/ledger      账目（全部，含已删除，可翻页）
/members     家庭成员（添加、改角色、封禁、重置密码、移除）
/devices     设备授权（批准/拒绝/吊销）
/audit       审计日志
```

后台**不做**业务写入（不代记账、不改账目），只做查看、成员与设备治理，避免与 App 的同步逻辑打架。

---

## 9. App 端

- 登录方式：设备配对，无账号密码入口。
- 无「选家庭」页：配对批准后直接进入唯一家庭。
- 本地缓存 + Outbox + 增量同步；读写永远先落本地。
- 设置页展示当前家庭、成员、同步状态；成员管理引导到后台。
- 账单邮箱、AI 分类、拆分、分享导入等能力保持不变，只是接口不再需要 family_id。

---

## 10. 安全与部署

| 面 | 措施 |
|---|---|
| 传输 | 强制 HTTPS（Caddy 自动证书），HSTS |
| 密码 | bcrypt（cost=12）；登录失败按 IP+账号限流 |
| 令牌 | access 15min + refresh 可吊销；refresh 只存哈希；设备令牌绑定 device_id |
| 越权 | 单家庭仍做成员校验；私密账在四个出口统一过滤 |
| 注入 | 全部参数化 SQL |
| 敏感配置 | 邮箱密码/解压码 AES-256-GCM；`DATA_KEY`/`JWT_SECRET` 走环境变量 |
| 审计 | 登录、成员变更、删账、后台操作全记录 |
| 备份 | 每日 `VACUUM INTO`，保留 14 天；异地副本 |

单机部署：

```
/opt/family-ledger/
├── ledger-server          # go build 产物（含 admin 前端）
├── config.yaml
└── data/
    ├── app.db
    └── backup/
```

systemd 托管；Caddy 反代；`/healthz` + `/readyz`；结构化 JSON 日志。

---

## 11. 关键决策

| # | 决策 | 结论 |
|---|---|---|
| 1 | 部署形态 | 自用为主；单家庭、单二进制、SQLite |
| 2 | 家庭数量 | **一个部署一个家庭**；没有家庭列表/创建/加入/切换 |
| 3 | 成员加入 | 首次初始化建户主；之后**邀请码注册为主**，也可由户主/管理员后台创建；App 走设备配对 |
| 4 | 注册/邀请码 | 保留邀请码（一次性、可设角色）；首次初始化免邀请码；邀请码绑定唯一家庭 |
| 5 | 离线能力 | 必须支持；本地缓存 + Outbox + rev 增量同步 |
| 6 | 私密账 | 支持；四出口过滤 |
| 7 | 账本数量 | 一本账 + 标签（v2） |
| 8 | 后台写入 | 只查看/治理，不代记账 |

---

## 12. 单家庭改造的迁移说明

- 迁移 `0008_single_family.sql`：老库若有多个家庭，保留「账单最多、创建最早」的那个为唯一家庭，
  其余软删除（数据仍在库中，可手工找回）。`invites` 表保留，邀请码继续使用。
- 现有账目、分类、账户、导入记录、邮箱配置、审计日志全部保留。
- App 升级后不再显示家庭选择页；本地缓存的 `family_id` 继续使用，不影响数据。
- API 路径变化是**不兼容变更**：需同时升级 App、后台和服务端。旧版客户端会收到 404，
  服务端 v1 不提供 `/families/{id}` 兼容路由。
