import 'dart:async';
import 'dart:io';

import 'package:device_info_plus/device_info_plus.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../auth/auth_store.dart';
import '../auth/biometric.dart';
import '../data/remote_api.dart';
import '../theme/app_theme.dart';

/// 设备配对页：新设备首次使用时不输密码，等家人在后台批准。
///
/// 流程：发起配对（拿 6 位配对码）→ 每 2 秒轮询 → 批准后自动登录。
class PairingPage extends StatefulWidget {
  const PairingPage({super.key, required this.auth});

  final AuthStore auth;

  @override
  State<PairingPage> createState() => _PairingPageState();
}

class _PairingPageState extends State<PairingPage> {
  static const MethodChannel _channel = MethodChannel('family_ledger/share');

  bool _loading = true;
  String? _error;
  String? _pairCode;
  String? _pairSecret;
  String _deviceName = '本机';
  Timer? _timer;
  bool _expired = false;

  @override
  void initState() {
    super.initState();
    _start();
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _start() async {
    setState(() {
      _loading = true;
      _error = null;
      _expired = false;
    });
    try {
      final info = await _deviceInfo();
      final deviceKey = await _deviceKey();
      final tokens = widget.auth.tokens;
      final data = await widget.auth.api.deviceStart(
        deviceUid: tokens.deviceUid,
        deviceKey: deviceKey,
        model: info.$1,
        osVersion: info.$2,
        appVersion: '1.0.0',
      );
      if (!mounted) return;
      setState(() {
        _pairCode = data['pair_code'] as String?;
        _pairSecret = data['pair_secret'] as String?;
        _deviceName = info.$1;
        _loading = false;
      });
      _startPolling();
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = e is ApiException ? e.message : '$e';
      });
    }
  }

  /// 配对成功后问一次要不要开指纹解锁
  Future<void> _offerBiometric() async {
    final available = await BiometricGuard.isAvailable();
    final enabled = await BiometricGuard.isEnabled();
    if (!available || enabled || !mounted) return;

    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('开启指纹解锁？'),
        content: const Text('以后打开 App 按一下指纹就能进，不用再等家人批准。'),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('暂不')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('开启')),
        ],
      ),
    );
    if (ok == true) {
      final passed = await BiometricGuard.authenticate(reason: '验证一次以开启指纹解锁');
      if (passed) await BiometricGuard.setEnabled(true);
    }
  }

  /// 首次部署时要能改服务器地址（App 端不再有登录页承载这个入口）
  Future<void> _editServer() async {
    final current = widget.auth.api.baseUrl;
    final controller = TextEditingController(text: current);
    final result = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('服务器地址'),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: const InputDecoration(
            hintText: 'http://192.168.1.10:8088',
            helperText: '填服务器地址，不要带 /admin',
          ),
        ),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('取消')),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, controller.text.trim()),
            child: const Text('保存'),
          ),
        ],
      ),
    );
    if (result == null) return;
    widget.auth.api.updateBaseUrl(result);
    await widget.auth.tokens.saveServerUrl(result);
    if (!mounted) return;
    setState(() {});
    await _start();
  }

  /// 设备唯一标识（ANDROID_ID），同一台手机卸载重装后不变
  Future<String> _deviceKey() async {
    try {
      final v = await _channel.invokeMethod<String>('getDeviceId');
      if (v != null && v.isNotEmpty) return v;
    } catch (_) {
      // 通道不可用就用空值，服务端不做去重
    }
    return '';
  }

  Future<(String, String)> _deviceInfo() async {
    try {
      final plugin = DeviceInfoPlugin();
      if (Platform.isAndroid) {
        final a = await plugin.androidInfo;
        return ('${a.manufacturer} ${a.model}', 'Android ${a.version.release}');
      }
      if (Platform.isIOS) {
        final i = await plugin.iosInfo;
        return ('${i.name} ${i.model}', 'iOS ${i.systemVersion}');
      }
    } catch (_) {
      // 拿不到就用兜底值
    }
    return ('Android 设备', 'Android ${Platform.operatingSystemVersion}');
  }

  void _startPolling() {
    _timer?.cancel();
    _timer = Timer.periodic(const Duration(seconds: 2), (_) async {
      if (!mounted) return;
      await _poll();
    });
  }

  Future<void> _poll() async {
    final code = _pairCode;
    final secret = _pairSecret;
    if (code == null || secret == null) return;

    try {
      final data = await widget.auth.api.devicePoll(pairCode: code, pairSecret: secret);
      final status = data['status'] as String? ?? 'pending';
      if (!mounted) return;

      if (status == 'approved') {
        _timer?.cancel();
        final access = data['access_token'] as String?;
        final refresh = data['refresh_token'] as String?;
        if (access == null || refresh == null) {
          setState(() => _error = '批准了但没拿到令牌，请重试');
          return;
        }
        // 先问指纹（此时页面还在），再写入会话切到主界面
        await _offerBiometric();
        if (!mounted) return;
        await widget.auth.applyDeviceSession(access, refresh);
        // 登录成功后由 AppRoot 根据会话状态切到主界面，这里不用手动跳转
        return;
      }

      if (status == 'rejected') {
        _timer?.cancel();
        setState(() => _error = '家人拒绝了这台设备，请确认后再试');
        return;
      }

      if (status == 'expired') {
        _timer?.cancel();
        setState(() => _expired = true);
        return;
      }


    } catch (_) {
      // 网络抖动忽略，下一轮继续
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('设备授权')),
      body: SafeArea(
        child: _loading
            ? const Center(child: CircularProgressIndicator())
            : ListView(
                padding: const EdgeInsets.fromLTRB(20, 24, 20, 32),
                children: <Widget>[
                  const Icon(Icons.devices_other_rounded, size: 56, color: Color(0xFF2F6F62)),
                  const SizedBox(height: 12),
                  const Center(
                    child: Text('等待家人批准',
                        style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700)),
                  ),
                  const SizedBox(height: 6),
                  Center(
                    child: Text(_deviceName,
                        style: TextStyle(fontSize: 12.5, color: Colors.grey.shade600)),
                  ),
                  const SizedBox(height: 24),

                  if (_error != null) ...<Widget>[
                    Container(
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: const Color(0xFFD64545).withValues(alpha: 0.08),
                        borderRadius: BorderRadius.circular(10),
                      ),
                      child: Text(_error!,
                          style: const TextStyle(color: Color(0xFFD64545), fontSize: 13)),
                    ),
                    const SizedBox(height: 16),
                    // 网络或服务端异常时给个重试，否则页面会卡死没法往下走
                    if (_pairCode == null)
                      FilledButton.icon(
                        onPressed: _loading ? null : _start,
                        icon: const Icon(Icons.refresh_rounded, size: 18),
                        label: const Text('重新连接'),
                      ),
                    const SizedBox(height: 8),
                  ],

                  if (_expired) ...<Widget>[
                    Container(
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: const Color(0xFFE08A1E).withValues(alpha: 0.1),
                        borderRadius: BorderRadius.circular(10),
                      ),
                      child: const Text('配对码已过期，请重新发起',
                          style: TextStyle(color: Color(0xFFE08A1E), fontSize: 13)),
                    ),
                    const SizedBox(height: 16),
                    FilledButton(onPressed: _start, child: const Text('重新发起配对')),
                  ] else if (_pairCode != null) ...<Widget>[
                    Container(
                      decoration: cardDecoration(context),
                      padding: const EdgeInsets.symmetric(vertical: 20),
                      child: Column(
                        children: <Widget>[
                          Text('配对码',
                              style: TextStyle(fontSize: 12, color: Colors.grey.shade500)),
                          const SizedBox(height: 8),
                          Text(
                            _pairCode!,
                            style: const TextStyle(
                              fontSize: 38,
                              fontWeight: FontWeight.w700,
                              letterSpacing: 10,
                              color: Color(0xFF2F6F62),
                            ),
                          ),
                          const SizedBox(height: 8),
                          Text('请让家人在「管理后台 → 设备授权」里批准',
                              style: TextStyle(fontSize: 12, color: Colors.grey.shade500)),
                        ],
                      ),
                    ),
                    const SizedBox(height: 16),
                    Row(
                      mainAxisAlignment: MainAxisAlignment.center,
                      children: <Widget>[
                        const SizedBox(
                          width: 14,
                          height: 14,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        ),
                        const SizedBox(width: 10),
                        Text('正在等待批准…',
                            style: TextStyle(fontSize: 12.5, color: Colors.grey.shade600)),
                      ],
                    ),
                    const SizedBox(height: 24),
                    TextButton(
                      onPressed: () async {
                        await Clipboard.setData(ClipboardData(text: _pairCode!));
                        if (!context.mounted) return;
                        ScaffoldMessenger.of(context).showSnackBar(
                          const SnackBar(content: Text('配对码已复制')),
                        );
                      },
                      child: const Text('复制配对码'),
                    ),
                  ],

                  const SizedBox(height: 32),
                  const Divider(),
                  const SizedBox(height: 8),
                  Center(
                    child: TextButton.icon(
                      onPressed: _editServer,
                      icon: const Icon(Icons.dns_outlined, size: 16),
                      label: Text(
                        '服务器：${widget.auth.api.baseUrl}',
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                  ),
                ],
              ),
      ),
    );
  }
}
