import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:local_auth/local_auth.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 指纹 / 人脸解锁。
///
/// 只解决「每次打开都要输密码」这一件事：登录一次后，冷启动用生物识别解锁。
/// 没有引入任何第三方登录资质，纯系统能力。
class BiometricGuard {
  BiometricGuard._();

  static const _prefsKey = 'biometric_unlock_enabled';
  static final LocalAuthentication _auth = LocalAuthentication();

  /// 设备是否支持（有指纹/人脸，且已录入）
  static Future<bool> isAvailable() async {
    try {
      final supported = await _auth.isDeviceSupported();
      final canCheck = await _auth.canCheckBiometrics;
      final enrolled = await _auth.getAvailableBiometrics();
      return supported && canCheck && enrolled.isNotEmpty;
    } on PlatformException {
      return false;
    }
  }

  static Future<bool> isEnabled() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getBool(_prefsKey) ?? false;
  }

  static Future<void> setEnabled(bool value) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_prefsKey, value);
  }

  /// 弹系统生物识别，成功返回 true
  static Future<bool> authenticate({String reason = '解锁家庭记账本'}) async {
    try {
      return await _auth.authenticate(
        localizedReason: reason,
        biometricOnly: false, // 允许退回设备密码
        sensitiveTransaction: true,
        persistAcrossBackgrounding: true,
      );
    } on PlatformException {
      return false;
    }
  }
}

/// 冷启动时的解锁页
class LockPage extends StatefulWidget {
  const LockPage({super.key, required this.onUnlocked, required this.onLogout});

  final VoidCallback onUnlocked;
  final VoidCallback onLogout;

  @override
  State<LockPage> createState() => _LockPageState();
}

class _LockPageState extends State<LockPage> {
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    // 进来就自动弹一次，失败后可以点按钮重试
    WidgetsBinding.instance.addPostFrameCallback((_) => _unlock());
  }

  Future<void> _unlock() async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    final ok = await BiometricGuard.authenticate();
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) {
      widget.onUnlocked();
    } else {
      setState(() => _error = '解锁失败，可重试或改用密码登录');
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: <Widget>[
              const Icon(Icons.fingerprint, size: 72, color: Color(0xFF2F6F62)),
              const SizedBox(height: 16),
              const Text('家庭记账本',
                  style: TextStyle(fontSize: 20, fontWeight: FontWeight.w700)),
              const SizedBox(height: 6),
              Text('用指纹或面容解锁',
                  style: TextStyle(fontSize: 13, color: Colors.grey.shade600)),
              const SizedBox(height: 28),
              SizedBox(
                width: 200,
                child: FilledButton(
                  onPressed: _busy ? null : _unlock,
                  child: _busy
                      ? const SizedBox(
                          width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
                      : const Text('解锁'),
                ),
              ),
              if (_error != null) ...<Widget>[
                const SizedBox(height: 14),
                Text(_error!, style: const TextStyle(fontSize: 12, color: Color(0xFFD64545))),
              ],
              const SizedBox(height: 20),
              TextButton(
                onPressed: widget.onLogout,
                child: const Text('改用密码登录', style: TextStyle(fontSize: 13)),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
