# 设备授权登录（配对制）

目标：家庭新成员/新设备**不用输账号密码**，由「已授权的人」在后台批准一次即可；
重装、清缓存后可以**重新授权到同一个已有身份**，而不是每次都新建账号。

## 一、整体流程

```
① App 首次启动
   └─ 生成 device_uid（本地 UUID）+ 采集 型号/系统/版本
   └─ POST /auth/device/start
         返回：pair_code（6 位，显示给用户看）
               pair_secret（32 字节，App 自己留着轮询用）
               expires_in（600 秒）

② App 显示「配对码 482163 · 等待批准」+ 每 2 秒轮询
   POST /auth/device/poll {pair_code, pair_secret}

③ 批准方（管理后台）看到待授权设备：
       小米 M2102J2SC · Android 11 · 19:42 · 192.168.1.5 · 配对码 482163
   选择：已有账号「妈妈」+ 角色「成员」→ 批准（单家庭，无需选择家庭）

④ App 轮询到 approved → 直接拿到 access/refresh token → 进入主界面

⑤ 提示「开启指纹解锁」（可跳过）→ 开启后冷启动只需指纹
```

## 二、为什么这样设计

| 需求 | 对应做法 |
|---|---|
| 不用输密码 | 批准即登录，App 侧零输入 |
| 重装后还是同一个人 | 批准时选的是**已有用户**，不是新建 |
| 防止陌生人混进来 | 只有已登录的管理员/户主能批准 |
| 可追溯 | 每条设备记录 `approved_by` + 时间 + IP |
| 可撤销 | 设备列表支持吊销，吊销即失效所有令牌 |

## 三、数据模型

```sql
CREATE TABLE devices (
  id            TEXT PRIMARY KEY,      -- UUIDv7
  device_uid    TEXT NOT NULL,         -- App 本地生成的稳定标识
  user_id       TEXT,                  -- 批准后绑定；NULL 表示待授权
  role          TEXT,                  -- 批准时指定的角色
  model         TEXT,                  -- 小米 M2102J2SC
  os_version    TEXT,                  -- Android 11
  app_version   TEXT,                  -- 1.0.0
  ip            TEXT,
  status        TEXT NOT NULL,         -- pending | approved | rejected | revoked
  pair_code     TEXT NOT NULL,         -- 6 位，给用户看
  pair_secret   TEXT NOT NULL,         -- 只存 sha256
  created_at    INTEGER NOT NULL,
  expires_at    INTEGER NOT NULL,
  approved_at   INTEGER,
  approved_by   TEXT,                  -- 谁批准的
  last_seen_at  INTEGER
);

CREATE INDEX idx_devices_status ON devices(status, created_at DESC);
CREATE INDEX idx_devices_uid ON devices(device_uid);
```

`refresh_tokens` 增加 `device_id`，吊销设备时按 device_id 一次性作废。

## 四、接口

### 未登录可用

| 接口 | 说明 |
|---|---|
| `POST /api/v1/auth/device/start` | 入参 `{device_uid, device_key, model, os_version, app_version}`；返回 `{pair_code, pair_secret, expires_in}` |
| `POST /api/v1/auth/device/poll` | 入参 `{pair_code, pair_secret}`；返回 `{status}`，approved 时附带 `{access_token, refresh_token, expires_in, user}` |

### 需要管理员/户主（管理后台）

| 接口 | 说明 |
|---|---|
| `GET /admin/api/devices/pending` | 待授权列表 |
| `POST /admin/api/devices/{id}/approve` | 入参 `{user_id, role}`；`user_id` 为空且传了 `new_account/new_nickname` 时创建新用户并加入唯一家庭 |
| `POST /admin/api/devices/{id}/reject` | 拒绝 |
| `GET /admin/api/devices` | 全部设备（含已授权/已吊销） |
| `POST /admin/api/devices/{id}/revoke` | 吊销 |

## 五、状态机

```
pending ──approve──> approved ──revoke──> revoked
   │
   ├─reject──> rejected
   └─超时(10min)──> expired
```

App 侧：

```
unpaired ──start──> waiting ──poll:approved──> paired ──绑定指纹──> ready
                       │
                       ├─rejected ──> 提示「被拒绝，请联系家人」
                       └─expired  ──> 自动重新发起（最多 3 次）
```

## 六、安全要点（必须做）

| 点 | 做法 |
|---|---|
| 配对码被猜到 | 6 位数字 + 10 分钟过期 + 同 IP 每分钟最多 3 次 start |
| 轮询被劫持 | `pair_secret` 必须匹配（只存哈希），否则 404 |
| 一次性 | approved 后 `pair_secret` 立即失效，重复 poll 返回相同结果但不给新令牌 |
| 批准权限 | 只有唯一家庭的 `owner` / `admin` 能批准 |
| 设备伪装 | 记录 IP + UA；同一 IP 短时间内多次 pending 报警 |
| 令牌泄露 | refresh token 绑定 device_id，吊销设备即失效 |
| 旧设备 | 允许一台设备同时在线，但列表里能看出重复，可手动吊销 |

## 七、边界情况

| 场景 | 处理 |
|---|---|
| 家人第一次来（没有账号） | 批准页支持「新建用户」：填账号 + 昵称，自动建号并绑定 |
| 重装 App | 重新配对，批准时选**同一个已有用户**；旧设备记录保留，可吊销 |
| 清了 App 数据 | 同上 |
| 批准方不在 | 保留账号密码登录做兜底（管理员始终能登） |
| 用户改了手机 | 新设备配对，旧设备吊销 |
| 一个用户多台设备 | 允许；每台一条记录 |
| 待授权列表堆积 | 24 小时后自动清理 expired |
| 成员还没有账号 | 先让户主/管理员发邀请码，用邀请码注册后再配对；或在批准页直接新建用户 |

## 八、和现有功能的关系

- **账号密码登录**：保留，作为管理员兜底和 App 启动页的备用入口
- **邀请码**：保留，是成员加入/注册的主要方式；设备授权是登录的主要方式
- **指纹解锁**：配对成功后引导开启，冷启动只需指纹

## 九、实施顺序

| 阶段 | 内容 | 工作量 |
|---|---|---|
| P1 | devices 表 + start/poll 接口 + 后台待授权页 | 约 4 小时 |
| P2 | App 配对页（显示配对码 + 轮询 + 状态） | 约 3 小时 |
| P3 | 批准后引导绑定指纹 | 约 1 小时 |
| P4 | 设备列表 + 吊销 + 审计日志 | 约 2 小时 |
| P5 | 户主在 App 内批准（不用开电脑） | 约 2 小时 |

## 十、批准入口选型

| 方案 | 优点 | 缺点 |
|---|---|---|
| 只在管理后台批准 | 实现最简单 | 必须开电脑 |
| 只在户主 App 批准 | 家里随手就能批 | 户主不在时没人能批 |
| **两者都要**（推荐） | 互补 | 多做一个页面 |

两者共用同一套服务端逻辑，只是入口不同。
