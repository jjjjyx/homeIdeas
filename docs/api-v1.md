# 家庭记账本 · API v1 契约

> 面向三端（Flutter App / Vue 管理后台 / 后续集成）的接口约定。实现见 `server/`。
> 状态：**M1（认证/家庭/成员）+ M2（账本/同步/统计）+ M3（管理后台）已完成**；M4 App 接入待做。

---

## 0. 通用约定

| 项 | 约定 |
|---|---|
| Base URL | `https://<host>/api/v1`（管理后台为 `/admin/api`） |
| 编码 | UTF-8 JSON，请求 `Content-Type: application/json; charset=utf-8` |
| 时间 | 全部为 **Unix 毫秒**（int64），客户端自行格式化 |
| ID | UUIDv7 字符串，**客户端可预生成**（离线建账需要） |
| 鉴权 | `Authorization: Bearer <access_token>` |
| 幂等 | 写接口建议带 `X-Request-Id`，服务端回填到响应与日志 |

### 统一响应包

```json
{
  "code": 0,
  "message": "ok",
  "data": { },
  "request_id": "01a19726-8deb-71db-a441-c12bc9865ff7"
}
```

- 成功：HTTP 200/201，`code = 0`
- 失败：HTTP 4xx/5xx，`code != 0`，`data` 为空

### 错误码

| code | HTTP | 含义 |
|---|---|---|
| 1001 | 400 | 参数不合法 |
| 2001 | 401 | 账号或密码错误 |
| 2002 | 401 | 未登录或登录已过期 |
| 2003 | 403 | 账号已被禁用 |
| 3001 | 403 | 没有权限（含跨家庭访问） |
| 4001 | 404 | 资源不存在 |
| 4002 | 409 | 资源已存在（账号重复等） |
| 4003 | 403 | 需要邀请码才能注册 |
| 4004 | 400 | 邀请码无效/过期/已使用 |
| 4005 | 400 | 家庭至少保留一位户主 |
| 5000 | 500 | 服务器内部错误 |

---

## 1. 认证

### 1.1 注册

`POST /api/v1/auth/register`

```json
{
  "account": "papa",
  "password": "Passw0rd!demo",
  "nickname": "爸爸",
  "invite_code": "2XQDM7",
  "device": "Xiaomi M2102J2SC"
}
```

规则：

- `account`：3–32 位，小写字母/数字/`.`/`_`/`-`，首个字符必须是字母或数字
- `password`：8–72 位
- `invite_code`：**数据库里第一个用户可省略**（引导账号，自动成为平台管理员）；之后必须提供，除非服务端开启 `ALLOW_PUBLIC_REGISTER=true`

响应 `201`：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "access_token": "eyJhbGciOi...",
    "expires_in": 900,
    "refresh_token": "8Zk3...（256bit 随机串）",
    "user": {
      "id": "01a197...",
      "account": "papa",
      "nickname": "爸爸",
      "avatar_url": "",
      "is_platform_admin": true
    }
  }
}
```

失败示例：`4003`（无邀请码）、`4004`（邀请码已用）、`4002`（账号被占）。

### 1.2 登录

`POST /api/v1/auth/login`

```json
{ "account": "papa", "password": "Passw0rd!demo", "device": "iPhone 15" }
```

响应同 1.1。错误密码统一返回 `2001`，不区分"账号不存在/密码错误"。

### 1.3 刷新令牌

`POST /api/v1/auth/refresh`

```json
{ "refresh_token": "8Zk3..." }
```

响应同 1.1（`refresh_token` 原样返回，不轮换）。失效/过期返回 `2002`。

### 1.4 退出登录

`POST /api/v1/auth/logout`

```json
{ "refresh_token": "8Zk3..." }
```

响应 `{ "code": 0, "message": "已退出登录" }`。幂等：重复调用也返回成功。

### 1.5 当前用户

`GET /api/v1/me`（需鉴权）

```json
{
  "code": 0,
  "data": {
    "user": { "id": "...", "account": "papa", "nickname": "爸爸", "avatar_url": "", "is_platform_admin": false },
    "families": [
      { "id": "...", "name": "我们的家", "owner_id": "...", "my_role": "owner", "member_count": 2, "rev": 3, "created_at": 1762000000000 }
    ]
  }
}
```

---

## 2. 家庭

### 2.1 创建家庭

`POST /api/v1/families`

```json
{ "name": "我们的家" }
```

响应 `201`：家庭对象（创建者自动成为 `owner`，自动种入 16 个默认分类，`rev = 1`）。

### 2.2 我的家庭列表

`GET /api/v1/families`

```json
{ "code": 0, "data": { "items": [ { "id": "...", "name": "我们的家", "my_role": "owner", "member_count": 2, "rev": 3, "owner_id": "...", "created_at": 1762000000000 } ] } }
```

### 2.3 家庭详情

`GET /api/v1/families/{id}`（需是该家庭成员）

```json
{
  "code": 0,
  "data": {
    "family": { "id": "...", "name": "我们的家", "owner_id": "...", "my_role": "owner", "member_count": 2, "rev": 3, "created_at": 1762000000000 },
    "members": [
      { "user_id": "...", "nickname": "爸爸", "account": "papa", "role": "owner", "joined_at": 1762000000000 },
      { "user_id": "...", "nickname": "妈妈", "account": "mama", "role": "member", "joined_at": 1762000100000 }
    ]
  }
}
```

> 非成员访问返回 **403（3001）**，不是 404 —— 避免通过状态码枚举家庭是否存在。

### 2.4 修改家庭信息

`PATCH /api/v1/families/{id}` —— 需要 `owner` 或 `admin`

```json
{ "name": "小明的家" }
```

### 2.5 生成邀请码

`POST /api/v1/families/{id}/invites` —— 需要 `owner` 或 `admin`

```json
{ "role": "member", "expires_in_hours": 24 }
```

| 字段 | 说明 |
|---|---|
| `role` | `member`（默认）/ `admin`；**只有 owner 能发 admin 邀请** |
| `expires_in_hours` | 1–720，默认 168 |

响应 `201`：

```json
{ "code": 0, "data": { "code": "2XQDM7", "role": "member", "expires_at": 1762086400000 } }
```

- 码为 6 位，去掉 `0/O/1/I` 等易混字符
- **一次性**：核销是带条件的原子 UPDATE，并发下只有一个请求能成功

### 2.6 凭邀请码加入

`POST /api/v1/families/join`

```json
{ "code": "2XQDM7" }
```

响应：家庭对象。错误：`4004`（无效/过期/已用）、`4002`（已在该家庭）。

### 2.7 成员列表

`GET /api/v1/families/{id}/members` —— owner 排最前，其次 admin，再按加入时间

### 2.8 调整成员角色

`PATCH /api/v1/families/{id}/members/{uid}` —— **仅 owner**

```json
{ "role": "admin" }
```

约束：不能改自己；`role` 只能是 `admin` / `member`（转让户主在 v2）。

### 2.9 移除成员

`DELETE /api/v1/families/{id}/members/{uid}`

| 操作者 | 可移除 |
|---|---|
| owner | admin、member |
| admin | 仅 member |
| member | 不允许 |

户主不可被移除（`4005`）。

### 2.10 退出家庭

`POST /api/v1/families/{id}/leave` —— 户主不能退出（`403`），需先转让或删除家庭。

---

## 3. 健康检查

| 接口 | 说明 |
|---|---|
| `GET /healthz` | 进程存活 |
| `GET /readyz` | 数据库可读（失败返回 503） |

---

## 4. 账本与同步（M2 已实现）

### 4.0 账目 CRUD

| 接口 | 说明 |
|---|---|
| `POST /api/v1/families/{id}/transactions` | 新增一笔（可带客户端生成的 `id`） |
| `GET /api/v1/families/{id}/transactions?from=&to=&type=&category_id=&member_id=&limit=&offset=` | 列表，`from/to` 支持毫秒或 `2026-11-01` |
| `PATCH /api/v1/families/{id}/transactions/{tid}` | 修改 |
| `DELETE /api/v1/families/{id}/transactions/{tid}` | 软删除（写墓碑） |

请求体：

```json
{
  "id": "01a1...（可选，离线建账时客户端生成）",
  "type": 0,
  "amount_cents": 12850,
  "category_id": "01a1...",
  "account_id": "01a1...（可选）",
  "occurred_at": 1762000000000,
  "note": "午饭",
  "visibility": "family",
  "tags": ""
}
```

校验：`amount_cents` 1 ~ 10 亿元分；`occurred_at` 在 2000-01-01 ~ 今天+1 年；`category_id` 必须属于该家庭。
权限：普通成员只能改/删自己记的账；私密账只有本人可改。

### 4.1 增量拉取

`GET /api/v1/families/{id}/sync?since={rev}&limit=500`

```json
{
  "code": 0,
  "data": {
    "cursor": 42,
    "has_more": false,
    "changes": {
      "categories": [ { "id": "...", "name": "餐饮", "type": 0, "icon_key": "restaurant", "color": 4293722384, "sort_order": 0, "rev": 41, "updated_at": 1762000000000 } ],
      "accounts": [ { "id": "...", "name": "现金", "kind": "cash", "sort_order": 0, "rev": 41, "updated_at": 1762000000000 } ],
      "transactions": [ { "id": "...", "type": 0, "amount_cents": 12850, "category_id": "...", "member_id": "...", "occurred_at": 1762000000000, "note": "午饭", "visibility": "family", "rev": 42, "created_at": 1762000000000, "updated_at": 1762000000000 } ],
      "members": [ { "user_id": "...", "nickname": "爸爸", "account": "papa", "role": "owner", "joined_at": 1762000000000 } ]
    }
  }
}
```

实现约定：

- `cursor = max(since, 本次返回行的最大 rev)`，客户端原样回传
- 删除是墓碑：`deleted_at != null` 的行同样返回，客户端据此本地删除
- `visibility=private` 的账只发给 `member_id == 当前用户` 的请求（列表、同步、统计三处都过滤）
- **members 每次返回全量**（成员不会超过几十个，避免为移除操作再造一套墓碑机制）
- 任一类数据触到 `limit` 即 `has_more=true`，客户端继续用新 cursor 拉

### 4.2 批量推送

`POST /api/v1/families/{id}/sync/push`

```json
{
  "ops": [
    { "client_op_id": "local-1", "entity": "transaction", "op": "upsert", "base_rev": 0, "payload": { "id": "01a1...", "type": 0, "amount_cents": 2850, "category_id": "...", "occurred_at": 1762000000000, "note": "打车" } },
    { "client_op_id": "local-2", "entity": "transaction", "op": "delete", "payload": { "id": "01a1..." } }
  ]
}
```

```json
{
  "code": 0,
  "data": {
    "cursor": 43,
    "results": [
      { "client_op_id": "local-1", "status": "applied", "rev": 43 },
      { "client_op_id": "local-2", "status": "conflict", "server": { "id": "01a1...", "rev": 43, "amount_cents": 2850 } }
    ]
  }
}
```

| status | 含义 | 客户端处理 |
|---|---|---|
| `applied` | 已应用，带新 rev | 删除 outbox 里这条 |
| `conflict` | `base_rev` 与服务端不一致，`server` 回传服务端版本 | 用 server 覆盖本地并提示"这笔账已被家人修改" |
| `rejected` | 校验失败或无权限，`reason` 说明原因 | 标记为失败，不要无限重试 |

- 单条失败不影响同批其他条；单批最多 200 条
- 应答里附带 `cursor`，客户端可省一次 pull
- 幂等：同一个 `id` 重复 upsert 不会产生重复数据；删除不存在的账返回 `applied`

### 4.3 统计

| 接口 | 返回 |
|---|---|
| `GET /api/v1/families/{id}/stats/summary?month=2026-11` | `{ month, income_cents, expense_cents, balance_cents, count }` |
| `GET /api/v1/families/{id}/stats/categories?month=&type=0` | `{ total_cents, items:[{category_id,name,color,icon_key,amount_cents,count,ratio}] }` |
| `GET /api/v1/families/{id}/stats/trend?months=6&type=0` | `{ items:[{month:"2026-06",amount_cents:...}] }` |

`month` 省略时为当月；`type` 省略时为支出（0）。

### 4.4 导出 CSV

`GET /api/v1/families/{id}/export.csv?from=2026-01-01&to=2026-12-31`

- 返回 `text/csv; charset=utf-8`，**带 UTF-8 BOM**（Excel 直接打开不乱码）
- `Content-Disposition: attachment; filename="family-ledger-20261101.csv"`
- 表头：`日期,类型,分类,账户,金额,备注,记账人,可见性`
- 私密账同样按 `member_id` 过滤，导出不会把别人的私密账带出去
- `from`/`to` 支持 `2026-01-01` 或毫秒时间戳；省略时默认 2000-01-01 ~ 下个月

### 4.5 账单导入

因为微信/支付宝**都不提供个人账单 API**，只能由用户导出账单文件后导入。支持 `.csv` 和加密的 `.zip`（微信/支付宝导出的是加密 zip）。

`POST /api/v1/families/{id}/imports`（multipart/form-data）

| 字段 | 说明 |
|---|---|
| `file` | 账单文件（.csv / .zip，≤32MB） |
| `password` | zip 解压密码。**每一份账单都有各自独立的密码**，既不是账号密码，也没有固定规则：微信的通过「微信支付」公众号下发，支付宝的通过「服务消息 / 开具交易流水证明 → 申请记录」下发 |

```json
{
  "code": 0,
  "data": {
    "batch_id": "01a1976a-42ee-...",
    "source": "wechat",
    "source_name": "微信支付",
    "total": 3,
    "imported": 3,
    "duplicated": 0,
    "skipped": 1,
    "failed": 0
  }
}
```

- `skipped` 主要是「不计收支」（转账、还信用卡、买理财）和退款/关闭的订单，这些记进来会让统计翻倍
- 去重靠 `(family_id, external_id)` 唯一索引：**同一份账单重复导入不会产生重复账目**
- 支持来源：微信支付、支付宝、云闪付、银行流水（按文件名和表头自动识别，CSV 支持 GBK/UTF-8）

`GET /api/v1/families/{id}/imports` —— 导入批次历史

`POST /api/v1/families/{id}/category-rules` —— 记住「商户名 → 分类」，下次导入自动套用

```json
{ "keyword": "美团", "category_id": "01a1..." }
```

### 4.6 账单邮箱（自动导入）

用 IMAP 拉取一个专用邮箱，用户在自己邮箱里设一条「发件人含 微信支付/支付宝 → 转发到账单邮箱」即可全自动。

| 接口 | 说明 |
|---|---|
| `GET /api/v1/families/{id}/mail` | 配置状态（**密码不返回**，只返回 `has_password`）+ 最近处理记录 |
| `PUT /api/v1/families/{id}/mail` | 保存配置 `{host, port, username, password, use_tls, enabled, extract_password, reset_cursor}` |
| `POST /api/v1/families/{id}/mail/test` | 试登录（不保存） |
| `POST /api/v1/families/{id}/mail/sync` | 立即拉取一次，返回 `{fetched, imported, duplicated, pending_password, failed}` |
| `POST /api/v1/families/{id}/mail/password` | 提交账单解压码 `{message_id, password}`；带 `message_id` 只重试这一封，不带则作为默认密码重试所有待填邮件 |

`reset_cursor: true` 会清空邮件处理记录并把 UID 游标归零，用于「重新扫描整个邮箱」。

邮件处理状态：`done`（已导入）/ `need_password`（加密包待填密码）/ `skipped`（不是账单邮件）/ `failed`。

两种账单形态都支持：

| 来源 | 形态 | 密码来源 |
|---|---|---|
| 支付宝 | 邮件**附件**里的加密 zip（内含 CSV） | 支付宝服务消息 / 申请记录 |
| 微信支付 | 正文里的**下载链接**（7 天有效），下载得到加密 zip（内含 xlsx） | 微信支付公众号 |

- **每份账单的密码不同**，所以密码按邮件存（`mail_messages.password_enc`），不是全局一个
- 下载链接只允许白名单域名（`*.wechatpay.cn`、`*.tenpay.com`、`*.alipay.com`、`*.mybank.cn`）且必须 HTTPS，防止邮件里的链接把服务端当 SSRF 跳板

说明：

- 邮箱密码和解压码用 **AES-256-GCM 加密存库**（密钥 `DATA_KEY`，未配置时退化为 `JWT_SECRET`）
- 服务端默认每 `MAIL_POLL_MINUTES`（15）分钟轮询一次邮件的 UID 增量
- **首次同步只扫最近 10 封**，不会把几年前的邮件翻出来（账单邮箱里历史推广邮件很多）
- 遇到加密账单且没有解压码时，邮件标记为 `need_password`，App 提示用户补填后自动重试
- 账单邮箱里混着的推广/通知邮件按发件人和标题粗筛，标 `skipped` 而不是报错

---

### 4.7 AI 分类与人工修正

账单导入后，用大模型把每笔支出分成「家庭共同 / 个人 / 无法判断」，并给出细分类。

| 接口 | 说明 |
|---|---|
| `POST /api/v1/families/{id}/classify` | 对未分类账目跑一遍 AI，返回 `{total, classified, unknown, batches, prompt_tokens, completion_tokens}` |
| `GET /api/v1/families/{id}/classify/pending?min_confidence=0.7` | 待确认列表：`items`（逐条）+ `merchants`（按商户聚合，供批量归类） |
| `POST /api/v1/families/{id}/transactions/{tid}/review` | 人工修正 `{scope, owner_id, category_id, remember}` |

`review` 的 `remember=true` 时，会把「商户名 → 分类」写成规则，下次导入自动套用。

账目新增字段：

| 字段 | 说明 |
|---|---|
| `scope` | `family` 家庭共同 / `personal` 个人 / `unknown` 待判断 |
| `owner_id` | 个人开支归属人 |
| `category_source` | `ai` / `rule` / `manual` |
| `ai_confidence` | 模型置信度 0-1 |
| `ai_reason` | 模型给的一句话理由 |
| `reviewed` | 是否已人工确认 |

模型配置（服务端环境变量，OpenAI 兼容协议）：

```
LLM_ENABLED=true
LLM_PROVIDER=deepseek        # deepseek | doubao | qwen
LLM_API_KEY=sk-xxx
LLM_BASE_URL=https://api.deepseek.com   # 留空按 provider 取默认
LLM_MODEL=deepseek-chat
LLM_BATCH_SIZE=30            # 一次请求多少笔
```

也可以用命令行跑：`go run ./cmd/classify -db ./data/app.db -family 我们的家 -dry-run`
## 5. 管理后台 API（M3 已实现）

前缀 `/admin/api`，**独立令牌**：`scope=admin`，有效期 8 小时，不提供 refresh（过期重新登录）。
App 令牌（scope=app）访问管理接口一律 401。

### 5.1 登录

`POST /admin/api/auth/login`

```json
{ "account": "papa", "password": "Passw0rd!demo" }
```

```json
{ "code": 0, "data": { "access_token": "eyJ...", "expires_in": 28800, "user": { "id": "...", "account": "papa", "nickname": "爸爸", "is_platform_admin": true } } }
```

非平台管理员返回 `403（3001）`，错误密码 `401（2001）`。

### 5.2 其余接口（均需 `Authorization: Bearer <admin token>`）

| 接口 | 说明 |
|---|---|
| `GET /admin/api/me` | 当前管理员 |
| `GET /admin/api/overview` | `{ users, families, transactions, expense_total_cents, new_users_today, new_tx_today }` |
| `GET /admin/api/users?q=&page=&page_size=` | `{ items:[{id,account,nickname,status,is_platform_admin,family_count,created_at}], total }` |
| `PATCH /admin/api/users/{uid}` | `{ "status": 0 }` 封禁 / `1` 解封；封禁会**立即吊销该用户所有 refresh token**；不能封禁自己 |
| `GET /admin/api/families?q=&page=&page_size=` | `{ items:[{id,name,owner_nickname,member_count,transaction_count,rev,created_at}], total }` |
| `GET /admin/api/families/{id}/transactions?page=&page_size=` | 该家庭全部账目（**含已删除**，带 `category_name` / `member_name`），便于审计 |
| `GET /admin/api/audit-logs?action=&page=&page_size=` | `{ items:[{action,nickname,target_type,target_id,detail,ip,created_at}], total }` |

未知的 `/admin/api/*` 返回 JSON 404，不会落到前端页面。

### 5.3 静态资源

管理后台是单页应用，构建产物 embed 进 Go 二进制：

- `GET /admin/` → `index.html`
- `GET /admin/assets/*` → 静态文件
- 其他 `/admin/xxx` → 回落到 `index.html`（前端路由）

---

## 6. 变更记录

| 日期 | 内容 |
|---|---|
| 2026-11-01 | v1 首版；M1 接口定稿；补 M2 同步契约草案 |
| 2026-11-01 | M2 全部实现并通过 60 项端到端测试；补账目 CRUD、同步推送/拉取、统计三节 |
| 2026-11-01 | M3 管理后台实现完成；补管理接口与静态资源约定，端到端检查扩到 77 项 |
| 2026-11-01 | M5 部分：CSV 导出接口、`VACUUM INTO` 备份工具、部署脚本；端到端检查扩到 83 项 |
