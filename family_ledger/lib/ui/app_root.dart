import 'package:flutter/material.dart';

import '../auth/auth_store.dart';
import '../auth/biometric.dart';
import '../data/ledger_store.dart';
import 'family_page.dart';
import 'ledger_shell.dart';
import 'pairing_page.dart';

/// 根节点：按会话状态在「设备授权 → 选家庭 → 主界面」之间切换。
///
/// App 端**不提供账号密码登录**：新设备一律走配对授权，
/// 由管理员在后台确认设备和身份。账号密码只用于管理后台。
class AppRoot extends StatefulWidget {
  const AppRoot({super.key, required this.auth, required this.ledger});

  final AuthStore auth;
  final LedgerStore ledger;

  @override
  State<AppRoot> createState() => _AppRootState();
}

class _AppRootState extends State<AppRoot> {
  bool _unlocked = false;
  bool _needLock = false;
  bool _checked = false;

  @override
  void initState() {
    super.initState();
    _checkLock();
  }

  /// 冷启动时判断是否需要先解锁
  Future<void> _checkLock() async {
    final enabled = await BiometricGuard.isEnabled();
    if (!mounted) return;
    setState(() {
      _needLock = enabled;
      _unlocked = !enabled;
      _checked = true;
    });
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
          case AuthStage.pickingFamily:
            return FamilyPage(auth: widget.auth);
          case AuthStage.ready:
            if (_needLock && !_unlocked) {
              return LockPage(
                onUnlocked: () => setState(() => _unlocked = true),
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
