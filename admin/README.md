# admin · 管理后台（Vue3 + Vite）

单家庭管理端：家庭概览、账目、成员、邀请码、设备授权、审计日志。构建产物会被 Go 服务 embed 进二进制，最终访问 `http://<host>/admin/`。

## 技术栈

Vue 3 + TypeScript + Vite 5 + Pinia + Vue Router + Element Plus + Axios。

## 开发

```powershell
cd G:\26-workspace\homeIdeas\admin
npm install --registry=https://registry.npmmirror.com
npm run dev          # http://127.0.0.1:5174/admin/ ，/admin/api 代理到 127.0.0.1:8088
```

后端要先在 8088 端口跑起来（见 `../server/README.md`）。

## 构建

```powershell
npm run build        # 输出到 ../server/webassets/dist
cd ../server
go build -trimpath -o bin/ledger-server.exe ./cmd/api   # 静态资源随二进制打包
```

> Windows 上如果 esbuild 报 `Access is denied`（临时目录删文件失败），把 `TEMP`/`TMP` 指到工作区内即可：
> `$env:TEMP='G:\26-workspace\homeIdeas\.tmp'; $env:TMP=$env:TEMP`

## 截图（可选）

`scripts/screenshot.mjs` 用 CDP 驱动无头 Chrome 登录并截图，用于验证页面渲染：

```powershell
# 1) 起一个可调试的无头 Chrome
chrome --headless=new --remote-debugging-port=9222 --remote-allow-origins=* --user-data-dir=<临时目录> about:blank

# 2) 截图
node scripts/screenshot.mjs http://127.0.0.1:8088 <管理员账号> <密码> out.png /admin/dashboard
```

## 页面

| 路由 | 说明 |
|---|---|
| `/admin/login` | 管理员登录（唯一家庭的 owner/admin 可进） |
| `/admin/dashboard` | 家庭概览：家庭名、成员数、账单数、累计支出、今日新增；可改家庭名 |
| `/admin/ledger` | 家庭全部账目（含已删除，便于审计） |
| `/admin/members` | 成员列表、添加成员、改角色、封禁/解禁、重置密码、移除、生成邀请码 |
| `/admin/devices` | 设备授权：批准/拒绝/吊销（批准后 App 自动登录） |
| `/admin/audit` | 审计日志，按 action 过滤 |

## 约定

- 令牌存 `localStorage.admin_token`，401 自动跳登录页
- 所有请求经 `src/api/client.ts`，统一拆 `{code,message,data}` 信封，只把 `data` 交给页面
- 后台**不做业务写入**（不代用户记账），只读 + 成员/设备/邀请码治理
- 单家庭：没有家庭列表与切换；所有页面都围绕唯一家庭
