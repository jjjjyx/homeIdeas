import 'package:flutter/material.dart';

import '../auth/auth_store.dart';
import '../theme/app_theme.dart';

/// 账号入口：账号密码登录 / 用邀请码注册加入唯一家庭。
///
/// 单家庭系统不再有「创建/加入/切换家庭」，但邀请码仍是成员加入的主要方式：
/// 家人在后台生成邀请码 → 新成员在这里填邀请码注册。
class AccountAuthPage extends StatefulWidget {
  const AccountAuthPage({super.key, required this.auth, this.startWithRegister = false});

  final AuthStore auth;
  final bool startWithRegister;

  @override
  State<AccountAuthPage> createState() => _AccountAuthPageState();
}

class _AccountAuthPageState extends State<AccountAuthPage> {
  late bool _registerMode = widget.startWithRegister;
  final _account = TextEditingController();
  final _password = TextEditingController();
  final _nickname = TextEditingController();
  final _invite = TextEditingController();

  @override
  void dispose() {
    _account.dispose();
    _password.dispose();
    _nickname.dispose();
    _invite.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    final account = _account.text.trim();
    final password = _password.text;
    if (account.isEmpty || password.isEmpty) {
      _toast('请填写账号和密码');
      return;
    }
    if (_registerMode) {
      await widget.auth.register(
        account: account,
        password: password,
        nickname: _nickname.text.trim(),
        inviteCode: _invite.text,
      );
    } else {
      await widget.auth.login(account, password);
    }
    if (!mounted) return;
    if (widget.auth.stage == AuthStage.ready) {
      Navigator.of(context).pop();
    } else {
      _toast(widget.auth.error ?? '登录失败');
    }
  }

  void _toast(String message) {
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('账号登录')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 20, 20, 40),
          children: <Widget>[
            SegmentedButton<bool>(
              segments: const <ButtonSegment<bool>>[
                ButtonSegment<bool>(value: false, label: Text('登录')),
                ButtonSegment<bool>(value: true, label: Text('用邀请码注册')),
              ],
              selected: <bool>{_registerMode},
              onSelectionChanged: (s) => setState(() => _registerMode = s.first),
            ),
            const SizedBox(height: 20),
            if (_registerMode) ...<Widget>[
              TextField(
                controller: _invite,
                textCapitalization: TextCapitalization.characters,
                decoration: const InputDecoration(
                  labelText: '邀请码',
                  hintText: '首次初始化可留空；加入家庭时必填',
                ),
              ),
              const SizedBox(height: 12),
            ],
            TextField(
              controller: _account,
              autocorrect: false,
              decoration: const InputDecoration(labelText: '账号'),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _password,
              obscureText: true,
              decoration: const InputDecoration(labelText: '密码'),
              onSubmitted: (_) => _submit(),
            ),
            if (_registerMode) ...<Widget>[
              const SizedBox(height: 12),
              TextField(
                controller: _nickname,
                decoration: const InputDecoration(
                  labelText: '昵称',
                  hintText: '家人看到的名字，留空则用账号',
                ),
              ),
            ],
            const SizedBox(height: 20),
            FilledButton(
              onPressed: widget.auth.busy ? null : _submit,
              child: widget.auth.busy
                  ? const SizedBox(
                      width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
                  : Text(_registerMode ? '注册并加入家庭' : '登录'),
            ),
            const SizedBox(height: 16),
            Text(
              '新设备也可以直接走「设备授权」：在管理后台批准后自动登录，无需输入密码。',
              style: TextStyle(fontSize: 12, color: Colors.grey.shade500),
            ),
            if (!_registerMode) ...<Widget>[
              const SizedBox(height: 12),
              Container(
                padding: const EdgeInsets.all(12),
                decoration: cardDecoration(context),
                child: const Text(
                  '第一次加入？请让户主/管理员在管理后台「家庭成员 → 邀请码」里生成一个，'
                  '再切到「用邀请码注册」。',
                  style: TextStyle(fontSize: 12.5),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
