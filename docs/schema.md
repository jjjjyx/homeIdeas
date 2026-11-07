# 数据库 Schema（SQLite）

权威定义在 `server/internal/store/migrations/`，启动时自动执行，内嵌在二进制里。
这里做一份可读的说明。

## 通用约定

| 约定 | 说明 |
|---|---|
| 主键 | `id TEXT`，**UUIDv7**，客户端可预生成（离线建账） |
| 时间 | 全部 `INTEGER`，Unix 毫秒 |
| 金额 | `amount_cents INTEGER`，单位「分」，永远为正，方向由 `type` 决定 |
| 软删除 | `deleted_at INTEGER NULL`；同步时作为**墓碑**下发 |
| 同步版本 | `rev INTEGER`，家庭级单调递增；客户端用它当增量游标 |
| 布尔 | `INTEGER` 0/1 |

## ER 关系

```
users ──< family_members >── families
  │                            │
  │                            ├──< categories
  │                            ├──< accounts
  │                            ├──< transactions >── categories
  │                            │                 └── accounts
  │                            ├──< invites
  │                            └──< audit_logs
  └──< refresh_tokens
```

## 表

### users

| 列 | 类型 | 说明 |
|---|---|---|
| id | TEXT PK | UUIDv7 |
| account | TEXT UNIQUE | 小写字母/数字/`._-`，3–32 位 |
| password_hash | TEXT | bcrypt(cost=12) |
| nickname | TEXT | 家人看到的名字 |
| avatar_url / phone | TEXT | 预留 |
| status | INTEGER | 1 正常 / 0 禁用（禁用即吊销全部 refresh token） |
| is_platform_admin | INTEGER | 1 可登录管理后台 |
| created_at / updated_at | INTEGER | 毫秒 |

### refresh_tokens

| 列 | 说明 |
|---|---|
| id / user_id | UUIDv7 / 用户 |
| token_hash | **只存 sha256**，不存明文令牌 |
| device | 设备标识 |
| expires_at / revoked_at | 过期 / 吊销（退出登录、改密、封号） |

### families

| 列 | 说明 |
|---|---|
| id / name / owner_id | 家庭 |
| currency | 默认 CNY |
| **rev** | 同步版本号，任何成员/分类/账户/账目变更都 +1 |
| deleted_at | 家庭软删（v2 支持删除家庭） |

### family_members

| 列 | 说明 |
|---|---|
| family_id + user_id | 复合主键 |
| role | `owner` / `admin` / `member` |
| joined_at | 加入时间 |

> 成员表**没有 rev**：成员数量很小，同步接口每次返回全量，避免为「移除成员」再造一套墓碑。

### invites

| 列 | 说明 |
|---|---|
| code | 6 位，去掉 `0/O/1/I` 易混字符，UNIQUE |
| role | 邀请加入后的角色 |
| expires_at | 默认 72 小时 |
| used_by / used_at | 一次性核销；**未使用必须是 NULL**（曾经写成空串导致可无限复用） |

### categories / accounts

| 列 | 说明 |
|---|---|
| family_id | 归属家庭 |
| name / icon_key / color / sort_order | 展示用 |
| is_system | 预置分类（新家庭自动种 16 个分类 + 4 个账户） |
| rev / updated_at / deleted_at | 同步字段 |

### transactions

| 列 | 说明 |
|---|---|
| family_id | 归属家庭 |
| type | 0 支出 / 1 收入 |
| amount_cents | 分 |
| category_id / account_id | 分类、账户（账户可空） |
| member_id | **记账人** |
| occurred_at | 账目发生时间（与创建时间分离，补记昨天不影响排序） |
| note | 备注 |
| **visibility** | `family` 家庭可见 / `private` 仅自己可见 |
| **source** | `manual` 手工记 / `wechat` / `alipay` / `unionpay` / `bank`（导入） |
| **external_id** | 导入账单的交易单号，唯一索引 `(family_id, external_id)` 用于去重 |
| tags | 预留（v2 标签） |
| rev / created_at / updated_at / deleted_at | 同步字段 |

### import_batches

一次账单导入的记录：来源、文件名、总条数、导入数、跳过数、失败数、操作人。

### category_rules

商户名/关键词 → 分类的映射（`family_id + keyword` 唯一）。导入时先套规则，命中不了才落默认分类；用户在 App 里改分类可以沉淀成规则。

### mail_accounts

每个家庭一个账单邮箱：IMAP 服务器、账号、**密码密文**（AES-256-GCM）、`last_uid` 增量游标、启用开关。
`extract_password_enc` 只保存「上一次用过的解压码」当兜底——**每一份账单的密码都是独立的**，真正生效的密码在 `mail_messages` 上。

### mail_messages

处理过的邮件，按 `(family_id, message_uid)` 去重，状态 `done / need_password / failed / skipped`，避免同一封邮件重复导入。
`password_enc` 存**这一份账单自己的解压密码**（微信、支付宝都是每份一个，且没有固定规则）。

### audit_logs

`action`（如 `auth.login`、`family.create`、`member.remove`、`admin.user.disable`）、操作人、对象、IP、时间。

### system_config / schema_migrations

键值配置表 & 迁移版本表。

## 索引

```sql
family_members (user_id)                      -- 我的家庭
transactions  (family_id, occurred_at DESC)   -- 首页列表
transactions  (family_id, rev)                -- 增量同步
transactions  (family_id, external_id) UNIQUE -- 账单导入去重（external_id 非空时生效）
categories    (family_id, type, sort_order)
accounts      (family_id)
invites       (family_id)
import_batches(family_id, created_at DESC)
category_rules(family_id, keyword) UNIQUE
mail_messages (family_id, message_uid) UNIQUE
audit_logs    (created_at DESC)
refresh_tokens(user_id)
```

## 同步语义（重要）

1. **rev 每行唯一**：一次写入 = 一次 `UPDATE families SET rev = rev + 1`。
   如果同一批多行共用一个 rev，客户端分页拉取时 cursor 会越过未返回的同 rev 行 → 漏数据。
2. **删除是墓碑**：`deleted_at` 置位 + `rev` 抬升，客户端拉到后本地删除。
3. **可见性过滤**：`visibility='family' OR member_id = 当前用户`，在**列表 / 同步 / 统计 / 导出**四个出口都要加，漏一个就是隐私泄露。
4. **游标**：`cursor = max(since, 本次返回行的最大 rev)`。

## 容量估算

| 项 | 估算 |
|---|---|
| 单条 transaction（含索引） | ≈ 300 B |
| 100 家庭 × 2 万笔 | 200 万行 ≈ 700 MB |
| 峰值写入 | 压测实测 ~680 条/秒 |
| 迁移到 PostgreSQL 的触发点 | 多实例部署、写入 > 50 QPS、单库 > 10 GB |
