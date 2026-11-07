# 家庭记账本 · API v1 契约（单家庭）

> 面向 Flutter App / Vue 管理后台 / 后续集成。实现见 `server/`。
>
> **单家庭模型**：一个部署只有一个家庭，所有数据接口不再带 `family_id`。
> 家庭在首次初始化时创建；成员通过**邀请码注册**或管理员后台创建加入，没有「创建/加入/切换家庭」。

---

## 0. 通用约定

| 项 | 约定 |
|---|---|
| Base URL | `https://<host>/api/v1`（管理后台为 `/admin/api`） |
| 编码 | UTF-8 JSON，请求 `Content-Type: application/json; charset=utf-8` |
| 时间 | 全部为 **Unix 毫秒**（int64），客户端自行格式化 |
| ID | UUIDv7 字符串，**客户端可预生成**（离线建账需要） |
| 鉴权 | `Authorization: Bearer <access_token>` |
| 家庭 | 由服务端根据当前部署唯一确定，**不需要也不允许客户端传 family_id** |

### 统一响应包

```json
{ "code": 0, "message": "ok", "data": { }, "request_id": "01a19726-..." }
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
| 3001 | 403 | 没有权限（不是本家庭成员 / 系统已初始化） |
| 4001 | 404 | 资源不存在 |
| 4002 | 409 | 资源已存在（账号重复等） |
| 4003 | 403 | 需要邀请码才能加入家庭 |
| 4004 | 400 | 邀请码无效 / 过期 / 已使用 |
| 4005 | 400 | 家庭至少保留一位户主 |
| 5000 | 500 | 服务器内部错误 |

---

## 1. 认证

App 端正常流程是**设备配对**（见 `device-pairing.md`）；账号密码只用于：
① 首次初始化系统；② 管理后台登录；③ 排障兜底。

### 1.1 注册（邀请码是成员加入的主要方式）

`POST /api/v1/auth/register`

```json
{
  "account": "mama",
  "password": "Passw0rd!demo",
  "nickname": "妈妈",
  "invite_code": "2XQDM7",
  "device": "Xiaomi M2102J2SC"
}
```

- **系统没有任何用户时**：免邀请码，创建唯一家庭 + 户主账号，`family_name` 可指定（默认「我的家」）
- **之后**：必须带有效邀请码，以邀请码上的角色加入同一个家庭；未带码返回 `4003`
- 邀请码一次性使用；无效/过期/已用返回 `4004`
- 邀请码用于**注册加入**；注册成功后可用账号密码或设备配对登录
- 若服务端设置 `ALLOW_PUBLIC_REGISTER=true`，无邀请码也可注册为普通成员
- 响应 `201`：同 1.2（登录成功结构）

### 1.2 登录

`POST /api/v1/auth/login`

```json
{ "account": "papa", "password": "Passw0rd!demo", "device": "iPhone 15" }
```

```json
{
  "code": 0,
  "data": {
    "access_token": "eyJhbGciOi...",
    "expires_in": 900,
    "refresh_token": "8Zk3...",
    "user": {
      "id": "01a197...",
      "account": "papa",
      "nickname": "爸爸",
      "avatar_url": "",
      "role": "owner",
      "is_platform_admin": true
    }
  }
}
```

错误密码统一 `2001`，不区分「账号不存在 / 密码错误」。

### 1.3 刷新令牌

`POST /api/v1/auth/refresh` `{ "refresh_token": "8Zk3..." }`

响应同 1.2（refresh_token 原样返回，不轮换）。失效/过期返回 `2002`。

### 1.4 退出登录

`POST /api/v1/auth/logout` `{ "refresh_token": "8Zk3..." }` —— 幂等。

### 1.5 当前用户与家庭

`GET /api/v1/me`

```json
{
  "code": 0,
  "data": {
    "user": { "id": "...", "account": "papa", "nickname": "爸爸", "role": "owner", "is_platform_admin": true },
    "family": {
      "id": "...", "name": "我们的家", "owner_id": "...",
      "my_role": "owner", "member_count": 2, "rev": 3416, "created_at": 1762000000000
    },
    "members": [
      { "user_id": "...", "nickname": "爸爸", "account": "papa", "role": "owner", "joined_at": 1762000000000 },
      { "user_id": "...", "nickname": "妈妈", "account": "mama", "role": "admin", "joined_at": 1762000100000 }
    ]
  }
}
```

系统尚未初始化时 `family` 为 `null`。

### 1.6 设备配对（App 主路径）

| 接口 | 鉴权 | 说明 |
|---|---|---|
| `POST /api/v1/auth/device/start` | 否 | `{device_uid, device_key, model, os_version, app_version}` → `{pair_code, pair_secret, expires_in}` |
| `POST /api/v1/auth/device/poll` | 否 | `{pair_code, pair_secret}` → `{status, access_token?, refresh_token?, expires_in?}`，`approved` 时直接登录 |

---

## 2. 家庭信息与成员

### 2.1 家庭详情

`GET /api/v1/family`（需登录）—— 返回 `{ family, members }`，结构同 1.5 的 `family`/`members`。

### 2.2 修改家庭名称

`PATCH /api/v1/family` —— 需要 `owner` 或 `admin`

```json
{ "name": "小明的家" }
```

### 2.3 成员列表

`GET /api/v1/family/members` —— owner 最前，其次 admin，再按加入时间。

### 2.4 添加成员

`POST /api/v1/family/members` —— 需要 `owner` 或 `admin`

```json
{ "account": "mama", "password": "Passw0rd!demo", "nickname": "妈妈", "role": "member" }
```

- 账号规则：3–32 位小写字母/数字/`.`/`_`/`-`，首字符为字母或数字
- 密码 8–72 位；`role` 为 `member`（默认）或 `admin`，**只有 owner 能添加 admin**
- 响应 `201`：成员对象

### 2.5 调整角色

`PATCH /api/v1/family/members/{uid}` —— **仅 owner**

```json
{ "role": "admin" }
```

约束：不能改自己；`role` 只能是 `admin` / `member`；户主转让暂不做。

### 2.6 移除成员

`DELETE /api/v1/family/members/{uid}`

| 操作者 | 可移除 |
|---|---|
| owner | admin、member |
| admin | 仅 member |
| member | 不允许 |

户主不可被移除（`4005`）。移除会立即吊销该成员全部设备令牌。
单家庭系统没有「退出家庭」：成员关系只能由户主/管理员在后台解除。

### 2.7 生成邀请码

`POST /api/v1/family/invites` —— 需要 `owner` 或 `admin`

```json
{ "role": "member", "expires_in_hours": 72 }
```

| 字段 | 说明 |
|---|---|
| `role` | `member`（默认）/ `admin`；**只有 owner 能发 admin 邀请** |
| `expires_in_hours` | 1–720，默认 168 |

响应 `201`：`{ "code": "2XQDM7", "role": "member", "expires_at": 1762086400000 }`

- 6 位，去掉 `0/O/1/I` 等易混字符
- **一次性**：核销是带条件的原子 UPDATE，并发下只有一个请求能成功
- 新成员在 App 启动页「已有账号 / 用邀请码登录」→「用邀请码注册」

---

## 3. 健康检查

| 接口 | 说明 |
|---|---|
| `GET /healthz` | 进程存活 |
| `GET /readyz` | 数据库可读（失败 503） |

---

## 4. 账本与同步

### 4.0 账目 CRUD

| 接口 | 说明 |
|---|---|
| `POST /api/v1/transactions` | 新增一笔（可带客户端生成的 `id`） |
| `GET /api/v1/transactions?from=&to=&type=&category_id=&member_id=&limit=&offset=` | 列表，`from/to` 支持毫秒或 `2026-11-01` |
| `PATCH /api/v1/transactions/{tid}` | 修改 |
| `DELETE /api/v1/transactions/{tid}` | 软删除（写墓碑） |

```json
{
  "id": "01a1...（可选）",
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

校验：`amount_cents` 1 ~ 10 亿元分；`occurred_at` 在 2000-01-01 ~ 今天+1 年；分类必须存在。
权限：普通成员只能改/删自己记的账；私密账只有本人可改。

### 4.1 增量拉取

`GET /api/v1/sync?since={rev}&limit=500`

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

- `cursor = max(since, 本次返回行的最大 rev)`
- 删除是墓碑：`deleted_at != null` 的行同样返回
- `visibility=private` 的账只发给 `member_id == 当前用户`（列表 / 同步 / 统计 / 导出统一过滤）
- **members 每次返回全量**
- 任一类数据触到 `limit` 即 `has_more=true`

### 4.2 批量推送

`POST /api/v1/sync/push`

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

| status | 客户端处理 |
|---|---|
| `applied` | 删除 outbox 里这条 |
| `conflict` | 用 `server` 覆盖本地并提示「这笔账已被家人修改」 |
| `rejected` | 标记失败，不要无限重试 |

单批最多 200 条；单条失败不影响其他条；应答附带最新 `cursor`。

### 4.3 统计

| 接口 | 返回 |
|---|---|
| `GET /api/v1/stats/summary?month=2026-11` | `{ month, income_cents, expense_cents, balance_cents, count }` |
| `GET /api/v1/stats/categories?month=&type=0` | `{ total_cents, items:[{category_id,name,color,icon_key,amount_cents,count,ratio}] }` |
| `GET /api/v1/stats/trend?months=6&type=0` | `{ items:[{month:"2026-06",amount_cents:...}] }` |
| `GET /api/v1/stats/scopes?month=2026-11` | 家庭/个人/往来/待确认口径拆分 |

### 4.4 导出 CSV

`GET /api/v1/export.csv?from=2026-01-01&to=2026-12-31`

- `text/csv; charset=utf-8`，**带 UTF-8 BOM**
- 表头：`日期,类型,分类,账户,金额,备注,记账人,可见性`
- 私密账按 `member_id` 过滤

### 4.5 账单导入

`POST /api/v1/imports`（multipart/form-data：`file` + `password`）

- 支持 `.csv` / 加密 `.zip`（≤32MB），自动识别微信/支付宝/云闪付/银行
- 去重靠 `external_id` 唯一索引：同一份账单重复导入不会产生重复账目
- `GET /api/v1/imports` —— 导入批次历史
- `POST /api/v1/imports/preview` —— 分享导入预览（不入库）
- `POST /api/v1/imports/confirm` —— 确认导入
- `POST /api/v1/category-rules` —— 记住「商户名 → 分类」

### 4.6 账单邮箱

| 接口 | 说明 |
|---|---|
| `GET /api/v1/mail` | 配置状态 + 最近处理记录（密码只返回 `has_password`） |
| `PUT /api/v1/mail` | 保存配置 `{host, port, username, password, use_tls, enabled, extract_password, reset_cursor}` |
| `POST /api/v1/mail/test` | 试登录（不保存） |
| `POST /api/v1/mail/sync` | 立即拉取一次 |
| `POST /api/v1/mail/password` | 提交解压码 `{message_id, password}` |

系统只有一个账单邮箱；每份账单的解压密码按邮件单独保存。

### 4.7 AI 分类与人工修正

| 接口 | 说明 |
|---|---|
| `POST /api/v1/classify` | 对未分类账目跑一遍 AI |
| `GET /api/v1/classify/pending?min_confidence=0.7` | 待确认列表（逐条 + 按商户聚合） |
| `POST /api/v1/transactions/{tid}/review` | 人工修正 `{scope, owner_id, category_id, remember}` |

### 4.8 拆单

| 接口 | 说明 |
|---|---|
| `POST /api/v1/transactions/{tid}/split` | 把一笔拆成多行 `{lines:[{amount_cents,scope,owner_id,category_id,note}]}` |
| `POST /api/v1/transactions/{tid}/unsplit` | 撤销拆分 |
| `GET /api/v1/transactions/{tid}/children` | 拆出的子账目 |

---

## 5. 管理后台 API

前缀 `/admin/api`，**独立令牌**：`scope=admin`，8 小时有效，不提供 refresh。
只有唯一家庭的 **户主 / 管理员**能登录；App 令牌访问管理接口一律 401。

### 5.1 登录

`POST /admin/api/auth/login` `{ "account": "papa", "password": "Passw0rd!demo" }`

```json
{ "code": 0, "data": { "access_token": "eyJ...", "expires_in": 28800, "user": { "id": "...", "account": "papa", "nickname": "爸爸", "role": "owner", "is_platform_admin": true } } }
```

### 5.2 其余接口（均需 admin token）

| 接口 | 说明 |
|---|---|
| `GET /admin/api/me` | 当前管理员 |
| `GET /admin/api/overview` | `{ family, members, transactions, expense_total_cents, new_tx_today, new_users_today }` |
| `GET /admin/api/family` | 唯一家庭 + 成员 |
| `PATCH /admin/api/family` | 修改家庭名称 |
| `POST /admin/api/invites` | 生成邀请码（同 2.7） |
| `GET /admin/api/members?q=&page=&page_size=` | `{ items:[{id,account,nickname,role,status,joined_at,created_at}], total }` |
| `POST /admin/api/members` | 添加成员（同 2.4） |
| `PATCH /admin/api/members/{uid}` | `{role}` / `{status:0|1}` / `{password}` 改角色、封禁/解禁、重置密码 |
| `DELETE /admin/api/members/{uid}` | 移除成员（同 2.6） |
| `GET /admin/api/transactions?page=&page_size=` | 家庭全部账目（**含已删除**，带 `category_name`/`member_name`） |
| `GET /admin/api/audit-logs?action=&page=&page_size=` | 审计日志 |
| `GET /admin/api/devices/pending` | 待授权设备 |
| `GET /admin/api/devices` | 全部设备 |
| `POST /admin/api/devices/{did}/approve` | `{user_id, role}` 或 `{new_account, new_nickname, new_password, role}` |
| `POST /admin/api/devices/{did}/reject` | 拒绝 |
| `POST /admin/api/devices/{did}/revoke` | 吊销 |

封禁/移除成员会立即吊销其全部 refresh token；不能封禁自己。
未知的 `/admin/api/*` 返回 JSON 404，不落到前端页面。

### 5.3 静态资源

- `GET /admin/` → `index.html`
- `GET /admin/assets/*` → 静态文件
- 其他 `/admin/xxx` → 回落到 `index.html`

---

## 6. 变更记录

| 日期 | 内容 |
|---|---|
| 2026-11-01 | v1 首版；M1–M5 完成 |
| 2026-11-07 | **单家庭改造**：去掉家庭列表/创建/加入/切换；数据接口去掉 `family_id`；**保留并强化邀请码注册**（成员主要加入方式）；成员也可由后台直接创建；管理后台改为单家庭概览/账目/成员 |
