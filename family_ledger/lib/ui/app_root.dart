import 'package:flutter/material.dart';

import '../auth/auth_store.dart';
import '../auth/biometric.dart';
import '../data/ledger_store.dart';
import 'ledger_shell.dart';
import 'pairing_page.dart';

/// 根节点：按会话状态在「设备授权 → 主界面」之间切换。
///
/// App 端**不提供账号密码登录**：新设备一律走配对授权，
/// 由管理员在后台确认设备和身份。单家庭系统没有选择家庭的步骤。
class AppRoot extends StatefulWidget {
  const AppRoot({super.key, required this.auth, required this.ledger});

  final AuthStore auth;
  final LedgerStore ledger;

  @override
  State<AppRoot> createState() => _AppRootState();
}

class _AppRootState extends State<AppRoot> with WidgetsBindingObserver {
  bool _unlocked = false;
  bool _needLock = false;
  bool _checked = false;
  bool _leftApp = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _syncLockSetting();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  /// 读取「是否开启了本地锁」——设置里改了之后回到前台也会重新读
  Future<void> _syncLockSetting() async {
    final enabled = await BiometricGuard.isEnabled();
    if (!mounted) return;
    setState(() {
      _needLock = enabled;
      if (!enabled) _unlocked = true;
      _checked = true;
    });
  }

  /// 每次切回前台都要重新验证：切后台就标记，回来时上锁
  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.paused) {
      _leftApp = true;
      return;
    }
    if (state == AppLifecycleState.resumed && _leftApp) {
      _leftApp = false;
      _syncLockSetting().then((_) {
        if (!mounted || !_needLock) return;
        setState(() => _unlocked = false);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: widget.auth,
      builder: (context, _) {
        if (!_checked) return const _Splash();

        switch (widget.auth.stage) {
          case AuthStage.loading:
            return const _Splash();
          case AuthStage.loggedOut:
            return PairingPage(auth: widget.auth);
          case AuthStage.ready:
            if (_needLock && !_unlocked) {
              return LockPage(
                onUnlocked: () => setState(() {
                  _unlocked = true;
                  _leftApp = false;
                }),
                onLogout: () async {
                  await BiometricGuard.setEnabled(false);
                  await widget.auth.logout();
                },
              );
            }
            return LedgerShell(auth: widget.auth, store: widget.ledger);
        }
      },
    );
  }
}

class _Splash extends StatelessWidget {
  const _Splash();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: <Widget>[
            const Icon(Icons.menu_book_rounded, size: 56, color: Color(0xFF2F6F62)),
            const SizedBox(height: 16),
            Text(
              '家庭记账本',
              style: TextStyle(fontSize: 18, color: Colors.grey.shade700),
            ),
            const SizedBox(height: 24),
            const SizedBox(
              width: 22,
              height: 22,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
          ],
        ),
      ),
    );
  }
}
