# 家庭记账本 · App（Flutter）

多人共享记账的 Android 客户端。数据以服务端为准，本地只做缓存和待同步队列，**没网也能记账**。

## 架构

```
UI (页面)
   │ 读：永远读本地缓存（秒开）
   │ 写：先写本地 + 进 outbox，立刻刷新界面（乐观更新）
LedgerStore
   ├── LocalDatabase   本地 SQLite（categories / accounts / transactions / members / outbox / sync_state）
   ├── Outbox          待同步操作队列
   └── SyncService     push（推队列）→ pull（按 rev 游标增量拉）
          │
       RemoteApi (dio + 自动刷新 token)
          │
       Go 服务端
```

会话状态机：`loading → 登录 → 选家庭 → 主界面`（`AuthStore`）。

## 目录

```
lib/
├── main.dart                 启动：读令牌 → 组装依赖 → 跑 bootstrap
├── app.dart                  MaterialApp（中文 locale）
├── app_root 在 ui/           按登录状态切页
├── core/
│   ├── money.dart            金额/日期格式化（纯函数，有单测）
│   └── ids.dart              UUIDv7 生成（离线建账主键）
├── data/
│   ├── models.dart           User / Family / Member / Category / Account / Transaction
│   ├── local_db.dart         本地缓存 + outbox + 同步游标
│   ├── remote_api.dart       REST 客户端（信封解析、401 刷新）
│   ├── sync_service.dart     推送 + 增量拉取 + 冲突处理
│   ├── token_store.dart      令牌 / 当前家庭 / 服务器地址
│   └── ledger_store.dart     页面用的数据仓库（ChangeNotifier）
├── auth/auth_store.dart      登录态、家庭选择
└── ui/                       登录、选家庭、账单、记一笔、统计、设置、账单邮箱
```

## 数据同步规则

| 场景 | 行为 |
|---|---|
| 新建/修改账目 | 写本地（`rev=0`，列表显示「待同步」）→ 进 outbox → 600ms 后自动推送 |
| 删除 | 本地删 + outbox 里放一条 delete |
| 推送成功 | 移除 outbox，用服务端返回的版本覆盖本地 |
| 版本冲突 | 用服务端版本覆盖本地并丢弃本地操作，避免无限重试 |
| 被拒绝（无权限/校验失败） | 丢弃并计数，设置页提示 |
| 增量拉取 | `cursor = max(rev)`，只拉 `rev > cursor` 的数据；服务端墓碑会让本地删除 |

## 运行

### 1. 先起后端

```powershell
cd ..\server
$env:HTTP_ADDR=':8088'; $env:DB_PATH='./data/app.db'; $env:JWT_SECRET='dev-secret-change-me-123456'
.\bin\ledger-server.exe
```

### 2. 连接服务器

**方式 A：USB 调试（推荐，走 adb 反向端口）**

```powershell
adb reverse tcp:8088 tcp:8088      # 手机的 127.0.0.1:8088 指向电脑
flutter run                        # 默认 baseUrl 就是 http://127.0.0.1:8088
```

**方式 B：WiFi 局域网**

打包时指定电脑的局域网 IP：

```powershell
flutter build apk --release --dart-define=API_BASE_URL=http://192.168.1.10:8088
```

**方式 C：登录页直接改**

登录页底部「服务器：…」可随时修改地址并保存，不用重新打包。

### 3. 首次使用

1. 数据库里**第一个注册的账号**免邀请码，并自动成为平台管理员（用于进管理后台）。
2. 登录后没有家庭 → 创建家庭。
3. 设置 → 邀请成员 → 拿到 6 位邀请码，家人用它注册/加入。

## 测试

```powershell
flutter analyze
flutter test                                      # 14 个：金额工具 + 服务端字段解析

# 真连后端的集成测试（默认跳过）
$env:LEDGER_TEST_ACCOUNT='your-account'
$env:LEDGER_TEST_PASSWORD='your-password'
$env:LEDGER_TEST_BASE_URL='http://127.0.0.1:8088'
flutter test test/api_integration_test.dart
```

## 从单机版升级

v1 的数据存在 `entries` / `categories` 两张表里，升级时会被改名为 `legacy_entries` / `legacy_categories` 保留，不自动上传（旧分类是本地自增 id，和云端 UUID 对不上）。新表结构从 v2 开始。

## Android 上踩过的坑（release 包才有）

1. **缺 INTERNET 权限**：Flutter 只在 `debug`/`profile` 的 manifest 里自动加 `android.permission.INTERNET`，release 主 manifest 没有 → 打包后所有请求 `Connection failed`。已在 `src/main/AndroidManifest.xml` 显式声明。
2. **明文 HTTP 被拦**：targetSdk ≥ 28 默认禁止 cleartext，连 `http://127.0.0.1:8088` 直接失败。已加 `res/xml/network_security_config.xml` 允许明文（正式公网部署请走 HTTPS 并关掉）。
3. **Kotlin 增量编译偶发失败**：`Could not close incremental caches`，`flutter clean` 也不一定好；在 `android/gradle.properties` 关掉 `kotlin.incremental`。

## 账单邮箱（自动导入）

设置 → 账单邮箱（仅户主/管理员可见）：

1. 填 IMAP 服务器、邮箱地址、**IMAP 授权码**（不是登录密码），点「测试连接」
2. 保存后点「立即同步」，或在微信/支付宝里申请账单时直接填这个邮箱
3. 账单压缩包有密码时，列表里会出现「待填密码」的条目，点进去填那一份账单的密码

**每一份账单的密码都是独立的**（微信在「微信支付」公众号下发，支付宝在服务消息 / 申请记录里），所以密码是按邮件填的，不是填一次通用。填完只重试那一封。

## 已知取舍

- 令牌存在 `SharedPreferences`，后续应换 `flutter_secure_storage`
- 没有做后台定时同步，只在启动、下拉刷新、写操作后同步
- 暂不支持附件/图片凭证
- 账单导入只做了邮件自动拉取，还没有「手动选文件上传」入口
