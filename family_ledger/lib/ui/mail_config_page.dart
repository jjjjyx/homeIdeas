import 'package:flutter/material.dart';

import '../auth/auth_store.dart';
import '../data/remote_api.dart';
import '../theme/app_theme.dart';

/// 账单邮箱：配置 IMAP、立即同步、补填解压码、查看导入记录。
///
/// 关键点：**每一份账单的解压密码都是独立的**（微信通过「微信支付」公众号下发，
/// 支付宝通过「服务消息 / 申请记录」下发），所以密码是按邮件填的，不是填一次通用。
class MailConfigPage extends StatefulWidget {
  const MailConfigPage({super.key, required this.auth});

  final AuthStore auth;

  @override
  State<MailConfigPage> createState() => _MailConfigPageState();
}

class _MailConfigPageState extends State<MailConfigPage> {
  final TextEditingController _host = TextEditingController(text: 'imap.qq.com');
  final TextEditingController _port = TextEditingController(text: '993');
  final TextEditingController _user = TextEditingController();
  final TextEditingController _pass = TextEditingController();

  Map<String, dynamic>? _status;
  bool _loading = true;
  bool _busy = false;
  String? _error;

  RemoteApi get _api => widget.auth.api;
  String? get _familyId => widget.auth.currentFamily?.id;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _host.dispose();
    _port.dispose();
    _user.dispose();
    _pass.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final fid = _familyId;
    if (fid == null) {
      setState(() => _loading = false);
      return;
    }
    try {
      final data = await _api.mailStatus(fid);
      if (!mounted) return;
      setState(() {
        _status = data;
        _loading = false;
        _error = null;
        if (data['configured'] == true) {
          _host.text = (data['host'] as String?) ?? '';
          _port.text = '${data['port'] ?? 993}';
          _user.text = (data['username'] as String?) ?? '';
        }
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = e is ApiException ? e.message : '$e';
      });
    }
  }

  Future<void> _run(Future<void> Function() action) async {
    setState(() => _busy = true);
    try {
      await action();
    } catch (e) {
      if (mounted) {
        _toast(e is ApiException ? e.message : '$e', error: true);
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _toast(String message, {bool error = false}) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(message),
        backgroundColor: error ? const Color(0xFFD64545) : null,
      ),
    );
  }

  Future<void> _test() => _run(() async {
        final fid = _familyId;
        if (fid == null) return;
        await _api.testMailConfig(
          fid,
          host: _host.text.trim(),
          port: int.tryParse(_port.text.trim()) ?? 993,
          username: _user.text.trim(),
          password: _pass.text,
        );
        _toast('连接成功');
      });

  Future<void> _save({bool resetCursor = false}) => _run(() async {
        final fid = _familyId;
        if (fid == null) return;
        await _api.saveMailConfig(
          fid,
          host: _host.text.trim(),
          port: int.tryParse(_port.text.trim()) ?? 993,
          username: _user.text.trim(),
          password: _pass.text,
          enabled: true,
          resetCursor: resetCursor,
        );
        _pass.clear();
        await _load();
        if (mounted) _toast(resetCursor ? '已保存，游标已重置' : '已保存');
      });

  Future<void> _sync() => _run(() async {
        final fid = _familyId;
        if (fid == null) return;
        final r = await _api.syncMail(fid);
        await _load();
        if (!mounted) return;
        final imported = r['imported'] ?? 0;
        final dup = r['duplicated'] ?? 0;
        final pending = r['pending_password'] ?? 0;
        final failed = r['failed'] ?? 0;
        var msg = '导入 $imported 条';
        if (dup != 0) msg += '，重复 $dup 条';
        if (pending != 0) msg += '，$pending 封待填密码';
        if (failed != 0) msg += '，$failed 封失败';
        _toast(msg);
      });

  Future<void> _enterPassword(Map<String, dynamic> message) async {
    final controller = TextEditingController();
    final password = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('输入解压密码'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(
              message['subject'] as String? ?? '',
              style: const TextStyle(fontSize: 13, color: Colors.grey),
            ),
            const SizedBox(height: 12),
            const Text(
              '每份账单的密码都不一样：\n'
              '· 微信：在「微信支付」公众号的消息里\n'
              '· 支付宝：在服务消息或「申请记录」里',
              style: TextStyle(fontSize: 12, color: Colors.grey),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: controller,
              autofocus: true,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(labelText: '解压密码'),
            ),
          ],
        ),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('取消')),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, controller.text.trim()),
            child: const Text('提交'),
          ),
        ],
      ),
    );
    if (password == null || password.isEmpty) return;

    await _run(() async {
      final fid = _familyId;
      if (fid == null) return;
      final r = await _api.submitMailPassword(
        fid,
        password,
        messageId: message['id'] as String? ?? '',
      );
      await _load();
      if (!mounted) return;
      final imported = r['imported'] ?? 0;
      _toast(imported > 0 ? '密码正确，导入 $imported 条' : '密码已保存，但没有导入新账目');
    });
  }

  @override
  Widget build(BuildContext context) {
    final configured = _status?['configured'] == true;
    return Scaffold(
      appBar: AppBar(title: const Text('账单邮箱')),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : SafeArea(
              child: ListView(
                padding: const EdgeInsets.fromLTRB(16, 16, 16, 40),
                children: <Widget>[
                  if (_error != null)
                    Container(
                      margin: const EdgeInsets.only(bottom: 12),
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: const Color(0xFFD64545).withValues(alpha: 0.08),
                        borderRadius: BorderRadius.circular(10),
                      ),
                      child: Text(_error!,
                          style: const TextStyle(color: Color(0xFFD64545), fontSize: 13)),
                    ),
                  if (configured) _statusCard() else _configCard(),
                  if (configured) ...<Widget>[
                    const SizedBox(height: 12),
                    _messagesCard(),
                  ],
                ],
              ),
            ),
    );
  }

  Widget _configCard() {
    return Container(
      decoration: cardDecoration(context),
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          const Text('配置账单邮箱', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
          const SizedBox(height: 6),
          Text(
            '在微信/支付宝里申请账单时填这个邮箱，之后账单会自动进账本。',
            style: TextStyle(fontSize: 12, color: Colors.grey.shade600),
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _host,
            decoration: const InputDecoration(labelText: 'IMAP 服务器', hintText: 'imap.qq.com'),
          ),
          const SizedBox(height: 10),
          TextField(
            controller: _port,
            keyboardType: TextInputType.number,
            decoration: const InputDecoration(labelText: '端口', hintText: '993'),
          ),
          const SizedBox(height: 10),
          TextField(
            controller: _user,
            keyboardType: TextInputType.emailAddress,
            decoration: const InputDecoration(
              labelText: '邮箱地址',
              hintText: '完整地址，如 123456@qq.com',
            ),
          ),
          const SizedBox(height: 10),
          TextField(
            controller: _pass,
            obscureText: true,
            decoration: const InputDecoration(
              labelText: 'IMAP 授权码',
              hintText: '不是登录密码，在邮箱设置里开通 IMAP 后获取',
            ),
          ),
          const SizedBox(height: 16),
          Row(
            children: <Widget>[
              Expanded(
                child: OutlinedButton(
                  onPressed: _busy ? null : _test,
                  child: const Text('测试连接'),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: FilledButton(
                  onPressed: _busy ? null : () => _save(),
                  child: _busy
                      ? const SizedBox(
                          width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
                      : const Text('保存'),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _statusCard() {
    final enabled = _status?['enabled'] == true;
    final lastSync = _status?['last_sync_at'] as int?;
    return Container(
      decoration: cardDecoration(context),
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              const Text('账单邮箱', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
              const Spacer(),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                decoration: BoxDecoration(
                  color: (enabled ? const Color(0xFF2E9E6B) : Colors.grey)
                      .withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(20),
                ),
                child: Text(
                  enabled ? '已开启' : '已暂停',
                  style: TextStyle(
                    fontSize: 11,
                    color: enabled ? const Color(0xFF2E9E6B) : Colors.grey.shade600,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Text(_status?['username'] as String? ?? '-',
              style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w500)),
          const SizedBox(height: 2),
          Text(
            '${_status?['host'] ?? ''}:${_status?['port'] ?? ''}',
            style: TextStyle(fontSize: 12, color: Colors.grey.shade500),
          ),
          const SizedBox(height: 6),
          Text(
            lastSync == null
                ? '还没有同步过'
                : '上次同步 ${_timeText(DateTime.fromMillisecondsSinceEpoch(lastSync))}',
            style: TextStyle(fontSize: 12, color: Colors.grey.shade500),
          ),
          const SizedBox(height: 4),
          Wrap(
            spacing: 4,
            children: <Widget>[
              TextButton.icon(
                onPressed: _busy ? null : _sync,
                icon: const Icon(Icons.cloud_download_outlined, size: 16),
                label: const Text('立即同步'),
              ),
              TextButton.icon(
                onPressed: _busy ? null : () => _save(resetCursor: true),
                icon: const Icon(Icons.restart_alt, size: 16),
                label: const Text('重新扫描'),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _messagesCard() {
    final messages = (_status?['messages'] as List<dynamic>? ?? <dynamic>[])
        .cast<Map<String, dynamic>>();
    return Container(
      decoration: cardDecoration(context),
      padding: const EdgeInsets.fromLTRB(16, 14, 16, 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          const Text('最近的账单邮件', style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600)),
          const SizedBox(height: 8),
          if (messages.isEmpty)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 12),
              child: Text('还没有收到账单邮件',
                  style: TextStyle(fontSize: 13, color: Colors.grey.shade500)),
            ),
          for (final m in messages) _messageTile(m),
        ],
      ),
    );
  }

  Widget _messageTile(Map<String, dynamic> m) {
    final status = m['status'] as String? ?? '';
    final needPassword = status == 'need_password';
    final (label, color) = switch (status) {
      'done' => ('已导入', const Color(0xFF2E9E6B)),
      'need_password' => ('待填密码', const Color(0xFFE08A1E)),
      'failed' => ('失败', const Color(0xFFD64545)),
      _ => ('已跳过', Colors.grey.shade600),
    };
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: InkWell(
        onTap: needPassword && !_busy ? () => _enterPassword(m) : null,
        borderRadius: BorderRadius.circular(8),
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 4),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Padding(
                padding: const EdgeInsets.only(top: 3),
                child: Icon(
                  needPassword ? Icons.lock_outline : Icons.mail_outline,
                  size: 16,
                  color: needPassword ? color : Colors.grey.shade400,
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(
                      m['subject'] as String? ?? '(无主题)',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w500),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      '${m['detail'] ?? ''}',
                      style: TextStyle(fontSize: 11.5, color: Colors.grey.shade500),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 8),
              Text(label, style: TextStyle(fontSize: 11.5, color: color)),
              if (needPassword) ...<Widget>[
                const SizedBox(width: 4),
                const Icon(Icons.chevron_right_rounded, size: 16, color: Colors.grey),
              ],
            ],
          ),
        ),
      ),
    );
  }

  String _timeText(DateTime time) {
    final diff = DateTime.now().difference(time);
    if (diff.inMinutes < 1) return '刚刚';
    if (diff.inMinutes < 60) return '${diff.inMinutes} 分钟前';
    if (diff.inHours < 24) return '${diff.inHours} 小时前';
    return '${time.month}月${time.day}日 ${time.hour}:${time.minute.toString().padLeft(2, '0')}';
  }
}
