# homeIdeas 工作区

本目录用于家庭记账本 App 的开发。**一个部署只服务一个家庭**：没有家庭列表/创建/加入/切换，
成员通过邀请码注册（或管理员后台创建）加入唯一家庭。

## 目录

```
homeIdeas/
├── server/              Go 后端（认证 + 家庭 + 邀请码 + 账本 + 同步 + 管理接口）
├── admin/               Vue3 管理后台（构建产物 embed 进 Go 二进制）
├── family_ledger/       Flutter App（设备配对 / 邀请码注册 / 离线记账 / 增量同步）
└── docs/
    ├── architecture.md  架构设计（单家庭）
    ├── api-v1.md        接口契约
    └── schema.md        数据库表结构说明
```

## 已安装的开发环境

| 组件 | 版本 | 安装位置 |
|---|---|---|
| **Go** | **1.27.3** | `D:\dev\go`（`GOROOT`） |
| Flutter SDK | 3.47.9 stable | `D:\dev\flutter` |
| Dart | 3.13.7 | 随 Flutter |
| JDK | Eclipse Temurin 17.0.20.1 | `D:\dev\jdk17` |
| Node.js / npm | 24.14.0 / 11.9.0 | `C:\Program Files\nodejs` || Android SDK Build-Tools | 36.1.0（AGP 另装了 36.0.0） | `D:\dev\android-sdk` |
| Android SDK Platform | android-36 | 同上 |
| Android Platform-Tools（adb） | 37.0.1 | 同上 |
| Android NDK | 28.2.13676358 | 同上 |
| Gradle | 9.3.1（项目 wrapper 自动下载） | `C:\Users\jyx\.gradle` |

已写入的用户环境变量：

- `GOROOT = D:\dev\go`、`GOPATH = C:\Users\jyx\go`
- `GOPROXY = https://goproxy.cn,direct`（国内代理，直接 `go get` 即可）
- `JAVA_HOME = D:\dev\jdk17`
- `ANDROID_HOME` / `ANDROID_SDK_ROOT = D:\dev\android-sdk`
- `PUB_HOSTED_URL = https://pub.flutter-io.cn`
- `FLUTTER_STORAGE_BASE_URL = https://storage.flutter-io.cn`
- `PATH` 追加了 `D:\dev\go\bin`、`C:\Users\jyx\go\bin`、`D:\dev\jdk17\bin`、`android-sdk\platform-tools`、`android-sdk\emulator`、`android-sdk\cmdline-tools\latest\bin`、`D:\dev\flutter\bin`

> 新开的终端才会读到这些变量；当前已开的命令行窗口需重启。

## 日常命令

```powershell
cd G:\26-workspace\homeIdeas\family_ledger
flutter doctor          # 环境体检
flutter devices         # 看有没有连上手机
flutter run             # 手机开 USB 调试后直接跑
flutter test            # 单元 / 冒烟测试
flutter build apk --release
```

## 两个已踩过的坑

1. **Gradle 官方源连不上**：`android/gradle/wrapper/gradle-wrapper.properties` 已改成腾讯镜像
   `https://mirrors.cloud.tencent.com/gradle/gradle-9.3.1-all.zip`；
   Maven 依赖在 `android/settings.gradle.kts`、`android/build.gradle.kts` 里优先走阿里云镜像，找不到会自动回落官方源。
2. **Flutter 自动装 NDK 失效**：新版 `sdkmanager` 已被 `android` CLI 取代，Flutter 的自动安装会报
   `Package ndk not found`。已手动安装 `ndk/28.2.13676358`；将来换 Flutter 版本若提示缺 NDK，执行：
   ```powershell
   android --sdk="D:\dev\android-sdk" sdk install "ndk/<版本号>"
   ```

## 想用安卓模拟器（可选）

模拟器包和系统镜像尚未安装（约 2.5 GB），且需要先在「启用或关闭 Windows 功能」里勾选
**Windows 虚拟机监控程序平台（Windows Hypervisor Platform）** 并重启（需要管理员权限）。
更省事的办法：直接插 USB 线用真机调试。

```powershell
# 需要时再执行
android --sdk="D:\dev\android-sdk" sdk install "emulator" "system-images/android-36/google_apis/x86_64"
avdmanager create avd -n pixel -k "system-images;android-36;google_apis;x86_64"
```
