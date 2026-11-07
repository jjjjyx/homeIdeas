import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../auth/auth_store.dart';
import '../auth/biometric.dart';
import '../data/ledger_store.dart';
import '../theme/app_theme.dart';
import 'classify_review_page.dart';
import 'mail_config_page.dart';

class SettingsPage extends StatefulWidget {
  const SettingsPage({super.key, required this.store, required this.auth});

  final LedgerStore store;
  final AuthStore auth;

  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  bool _biometricAvailable = false;
  bool _biometricEnabled = false;

  LedgerStore get store => widget.store;
  AuthStore get auth => widget.auth;

  @override
  void initState() {
    super.initState();
    _loadBiometric();
  }

  Future<void> _loadBiometric() async {
    final available = await BiometricGuard.isAvailable();
    final enabled = await BiometricGuard.isEnabled();
    if (!mounted) return;
    setState(() {
      _biometricAvailable = available;
      _biometricEnabled = enabled;
    });
  }

  Future<void> _toggleBiometric(bool value) async {
    if (value) {
      final ok = await BiometricGuard.authenticate(reason: '开启指纹解锁前先验证一次');
      if (!ok) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('验证失败，未开启')),
          );
        }
        return;
      }
    }
    await BiometricGuard.setEnabled(value);
    if (!mounted) return;
    setState(() => _biometricEnabled = value);
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(value ? '已开启，之后每次进入 App 都要验证指纹' : '已关闭启动验证')),
    );
  }

  Future<void> _editServer(BuildContext context) async {
    final controller = TextEditingController(text: auth.api.baseUrl);
    final url = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('服务器地址'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            TextField(
              controller: controller,
              autofocus: true,
              keyboardType: TextInputType.url,
              decoration: const InputDecoration(hintText: 'http://192.168.3.93:8088'),
            ),
            const SizedBox(height: 10),
            Text(
              'USB 调试填 http://127.0.0.1:8088（需要 adb reverse）；'
              '同一 WiFi 下填电脑的局域网 IP。',
              style: TextStyle(fontSize: 12, color: Colors.grey.shade600),
            ),
          ],
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
    if (url == null || url.isEmpty) return;
    final normalized = url.replaceAll(RegExp(r'/+$'), '');
    auth.api.updateBaseUrl(normalized);
    await auth.tokens.saveServerUrl(normalized);
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('服务器已切换到 $normalized')),
      );
    }
  }
  Future<void> _sync(BuildContext context) async {
    final outcome = await store.syncNow();
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          outcome.ok
              ? '同步完成：上传 ${outcome.applied} 条，拉取 ${outcome.pulled} 条'
              : '同步失败：${outcome.error}',
        ),
      ),
    );
  }

  Future<void> _invite(BuildContext context) async {
    try {
      final code = await auth.api.createInvite();
      if (!context.mounted) return;
      await showDialog<void>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('家庭邀请码'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Text(
                code,
                style: const TextStyle(
                  fontSize: 30,
                  fontWeight: FontWeight.w700,
                  letterSpacing: 6,
                ),
              ),
              const SizedBox(height: 8),
              Text(
                '72 小时内有效，仅可使用一次。\n家人在 App 启动页点「已有账号 / 用邀请码登录」→「用邀请码注册」。',
                style: TextStyle(fontSize: 12, color: Colors.grey.shade600),
              ),
            ],
          ),
          actions: <Widget>[
            TextButton(
              onPressed: () async {
                await Clipboard.setData(ClipboardData(text: code));
                if (ctx.mounted) Navigator.pop(ctx);
                if (context.mounted) {
                  ScaffoldMessenger.of(context).showSnackBar(
                    const SnackBar(content: Text('已复制邀请码')),
                  );
                }
              },
              child: const Text('复制'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(ctx),
              child: const Text('好'),
            ),
          ],
        ),
      );
    } catch (e) {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('生成邀请码失败：$e')),
      );
    }
  }

  Future<void> _logout(BuildContext context) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('退出登录？'),
        content: const Text('本机缓存会被清空，未同步的记录会丢失。'),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('取消')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('退出')),
        ],
      ),
    );
    if (ok == true) await auth.logout();
  }

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: Listenable.merge(<Listenable>[store, auth]),
      builder: (context, _) {
        final family = auth.currentFamily;
        final user = auth.user;
        final lastSync = store.lastSyncAt;
        return SafeArea(
          bottom: false,
          child: ListView(
            padding: const EdgeInsets.fromLTRB(16, 18, 16, 150),
            children: <Widget>[
              const Text(
                '设置',
                style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 16),

              // 账号
              Container(
                decoration: cardDecoration(context),
                child: ListTile(
                  leading: CircleAvatar(
                    backgroundColor: const Color(0xFF2F6F62).withValues(alpha: 0.12),
                    child: Text(
                      (user?.nickname.isNotEmpty ?? false)
                          ? user!.nickname.characters.first
                          : '?',
                      style: const TextStyle(color: Color(0xFF2F6F62)),
                    ),
                  ),
                  title: Text(user?.nickname ?? '未登录'),
                  subtitle: Text('账号 ${user?.account ?? '-'}'),
                ),
              ),
              const SizedBox(height: 12),

              // 当前家庭
              Container(
                decoration: cardDecoration(context),
                padding: const EdgeInsets.fromLTRB(16, 14, 16, 8),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Row(
                      children: <Widget>[
                        const Text('当前家庭',
                            style: TextStyle(fontSize: 13, color: Colors.grey)),
                        const Spacer(),
                        Text(
                          family == null ? '-' : _roleLabel(family.myRole),
                          style: const TextStyle(fontSize: 12, color: Colors.grey),
                        ),
                      ],
                    ),
                    const SizedBox(height: 4),
                    Text(
                      family?.name ?? '未选择',
                      style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
                    ),
                    const SizedBox(height: 10),
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      children: <Widget>[
                        for (final m in store.members)
                          Chip(
                            label: Text(
                              m.nickname + (m.userId == user?.id ? '（我）' : ''),
                              style: const TextStyle(fontSize: 12),
                            ),
                            visualDensity: VisualDensity.compact,
                            side: BorderSide(color: Colors.grey.shade200),
                            backgroundColor: Colors.grey.shade50,
                          ),
                      ],
                    ),
                    if (family?.canManage ?? false)
                      Align(
                        alignment: Alignment.centerRight,
                        child: TextButton.icon(
                          onPressed: () => _invite(context),
                          icon: const Icon(Icons.person_add_alt, size: 16),
                          label: const Text('邀请成员'),
                        ),
                      ),
                    if (family?.canManage ?? false)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 6),
                        child: Text(
                          '也可以直接在管理后台「家庭成员」里创建账号',
                          style: TextStyle(fontSize: 12, color: Colors.grey.shade500),
                        ),
                      ),
                  ],
                ),
              ),
              const SizedBox(height: 12),

              // 同步
              Container(
                decoration: cardDecoration(context),
                padding: const EdgeInsets.fromLTRB(16, 14, 16, 6),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Row(
                      children: <Widget>[
                        const Text('同步',
                            style: TextStyle(fontSize: 13, color: Colors.grey)),
                        const Spacer(),
                        if (store.syncing)
                          const SizedBox(
                            width: 14,
                            height: 14,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          ),
                      ],
                    ),
                    const SizedBox(height: 6),
                    Text(
                      store.pendingCount == 0
                          ? '所有记录已同步'
                          : '${store.pendingCount} 条待同步',
                      style: TextStyle(
                        fontSize: 15,
                        color: store.pendingCount == 0
                            ? const Color(0xFF2E9E6B)
                            : const Color(0xFFD64545),
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      lastSync == null ? '还没有同步过' : '上次同步 ${_timeText(lastSync)}',
                      style: TextStyle(fontSize: 12, color: Colors.grey.shade500),
                    ),
                    if (store.lastSyncError != null)
                      Padding(
                        padding: const EdgeInsets.only(top: 4),
                        child: Text(
                          store.lastSyncError!,
                          style: const TextStyle(fontSize: 12, color: Color(0xFFD64545)),
                        ),
                      ),
                    Align(
                      alignment: Alignment.centerRight,
                      child: TextButton.icon(
                        onPressed: store.syncing ? null : () => _sync(context),
                        icon: const Icon(Icons.sync, size: 16),
                        label: const Text('立即同步'),
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 12),

              Container(
                decoration: cardDecoration(context),
                child: Column(
                  children: <Widget>[
                    if (_biometricAvailable)
                      SwitchListTile(
                        secondary: const Icon(Icons.fingerprint, size: 22),
                        title: const Text('启动验证'),
                        subtitle: Text(
                          _biometricEnabled
                              ? '已开启：每次进入 App 都要验证指纹'
                              : '开启后，每次进入 App 需验证指纹',
                          style: const TextStyle(fontSize: 12),
                        ),
                        value: _biometricEnabled,
                        onChanged: _toggleBiometric,
                      ),
                    if (_biometricAvailable)
                      const Divider(height: 1, indent: 16, endIndent: 16),
                    if (family?.canManage ?? false)
                      ListTile(
                        leading: const Icon(Icons.mark_email_read_outlined, size: 20),
                        title: const Text('账单邮箱'),
                        subtitle: const Text(
                          '自动导入微信/支付宝账单',
                          style: TextStyle(fontSize: 12),
                        ),
                        trailing: const Icon(Icons.chevron_right_rounded, size: 18),
                        onTap: () => Navigator.of(context).push(
                          MaterialPageRoute<void>(
                            builder: (_) => MailConfigPage(auth: auth),
                          ),
                        ),
                      ),
                    if (family?.canManage ?? false)
                      const Divider(height: 1, indent: 16, endIndent: 16),
                    ListTile(
                      leading: const Icon(Icons.auto_awesome_outlined, size: 20),
                      title: const Text('账单分类'),
                      subtitle: const Text(
                        '确认家庭/个人归属和分类',
                        style: TextStyle(fontSize: 12),
                      ),
                      trailing: const Icon(Icons.chevron_right_rounded, size: 18),
                      onTap: () => Navigator.of(context).push(
                        MaterialPageRoute<void>(
                          builder: (_) => ClassifyReviewPage(
                            api: auth.api,
                            categories: store.categories,
                          ),
                        ),
                      ),
                    ),
                    const Divider(height: 1, indent: 16, endIndent: 16),
                    ListTile(
                      leading: const Icon(Icons.dns_outlined, size: 20),
                      title: const Text('服务器地址'),
                      subtitle: Text(
                        auth.api.baseUrl,
                        style: const TextStyle(fontSize: 12),
                      ),
                      trailing: const Icon(Icons.chevron_right_rounded, size: 18),
                      onTap: () => _editServer(context),
                    ),
                    const Divider(height: 1, indent: 16, endIndent: 16),
                    ListTile(
                      leading: const Icon(Icons.logout_rounded, size: 20),
                      title: const Text('退出登录'),
                      trailing: const Icon(Icons.chevron_right_rounded, size: 18),
                      onTap: () => _logout(context),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 16),
              Center(
                child: Text(
                  '数据存在自己的服务器上 · 支持离线记账',
                  style: TextStyle(fontSize: 12, color: Colors.grey.shade400),
                ),
              ),
            ],
          ),
        );
      },
    );
  }

  String _roleLabel(String role) => switch (role) {
        'owner' => '户主',
        'admin' => '管理员',
        _ => '成员',
      };

  String _timeText(DateTime time) {
    final now = DateTime.now();
    final diff = now.difference(time);
    if (diff.inMinutes < 1) return '刚刚';
    if (diff.inMinutes < 60) return '${diff.inMinutes} 分钟前';
    if (diff.inHours < 24) return '${diff.inHours} 小时前';
    return '${time.month}月${time.day}日 ${time.hour}:${time.minute.toString().padLeft(2, '0')}';
  }
}
