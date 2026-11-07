import 'package:flutter/material.dart';

import '../auth/auth_store.dart';
import '../data/models.dart';

/// 家庭选择：一个账号可以属于多个家庭，也在这里创建或凭邀请码加入。
class FamilyPage extends StatelessWidget {
  const FamilyPage({super.key, required this.auth});

  final AuthStore auth;

  Future<void> _createDialog(BuildContext context) async {
    final controller = TextEditingController();
    final name = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('创建家庭'),
        content: TextField(
          controller: controller,
          autofocus: true,
          maxLength: 20,
          decoration: const InputDecoration(
            labelText: '家庭名称',
            hintText: '例如：我们的家',
          ),
        ),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('取消')),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, controller.text.trim()),
            child: const Text('创建'),
          ),
        ],
      ),
    );
    if (name == null || name.isEmpty) return;
    final ok = await auth.createFamily(name);
    if (!ok && context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(auth.error ?? '创建失败')),
      );
    }
  }

  Future<void> _joinDialog(BuildContext context) async {
    final controller = TextEditingController();
    final code = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('加入家庭'),
        content: TextField(
          controller: controller,
          autofocus: true,
          textCapitalization: TextCapitalization.characters,
          maxLength: 6,
          decoration: const InputDecoration(
            labelText: '邀请码',
            hintText: '6 位字母数字',
          ),
        ),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('取消')),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, controller.text.trim()),
            child: const Text('加入'),
          ),
        ],
      ),
    );
    if (code == null || code.isEmpty) return;
    final ok = await auth.joinFamily(code);
    if (!ok && context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(auth.error ?? '加入失败')),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final families = auth.families;
    return Scaffold(
      appBar: AppBar(
        title: const Text('选择家庭'),
        actions: <Widget>[
          TextButton(
            onPressed: () => auth.logout(),
            child: const Text('退出登录'),
          ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
        children: <Widget>[
          Text(
            '你好，${auth.user?.nickname ?? ''}',
            style: const TextStyle(fontSize: 20, fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: 4),
          Text(
            '选择一个家庭开始记账，或创建/加入新家庭',
            style: TextStyle(fontSize: 13, color: Colors.grey.shade600),
          ),
          const SizedBox(height: 18),
          if (families.isEmpty)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 24),
              child: Column(
                children: <Widget>[
                  Icon(Icons.groups_outlined, size: 56, color: Colors.grey.shade300),
                  const SizedBox(height: 10),
                  Text(
                    '你还没有加入任何家庭',
                    style: TextStyle(fontSize: 14, color: Colors.grey.shade600),
                  ),
                ],
              ),
            ),
          for (final Family family in families)
            Padding(
              padding: const EdgeInsets.only(bottom: 10),
              child: Material(
                color: Colors.white,
                borderRadius: BorderRadius.circular(16),
                child: InkWell(
                  borderRadius: BorderRadius.circular(16),
                  onTap: auth.busy ? null : () => auth.selectFamily(family),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 16),
                    child: Row(
                      children: <Widget>[
                        Container(
                          width: 42,
                          height: 42,
                          decoration: BoxDecoration(
                            color: const Color(0xFF2F6F62).withValues(alpha: 0.1),
                            borderRadius: BorderRadius.circular(12),
                          ),
                          child: const Icon(Icons.home_rounded,
                              color: Color(0xFF2F6F62), size: 20),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: <Widget>[
                              Text(
                                family.name,
                                style: const TextStyle(
                                  fontSize: 16,
                                  fontWeight: FontWeight.w600,
                                ),
                              ),
                              const SizedBox(height: 2),
                              Text(
                                '${family.memberCount} 位成员 · ${_roleLabel(family.myRole)}',
                                style: TextStyle(
                                  fontSize: 12,
                                  color: Colors.grey.shade500,
                                ),
                              ),
                            ],
                          ),
                        ),
                        const Icon(Icons.chevron_right_rounded, size: 18),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          const SizedBox(height: 12),
          Row(
            children: <Widget>[
              Expanded(
                child: OutlinedButton.icon(
                  onPressed: auth.busy ? null : () => _createDialog(context),
                  icon: const Icon(Icons.add_home_work_outlined, size: 18),
                  label: const Text('创建家庭'),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: OutlinedButton.icon(
                  onPressed: auth.busy ? null : () => _joinDialog(context),
                  icon: const Icon(Icons.vpn_key_outlined, size: 18),
                  label: const Text('凭邀请码加入'),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  String _roleLabel(String role) => switch (role) {
        'owner' => '户主',
        'admin' => '管理员',
        _ => '成员',
      };
}
