import 'dart:convert';
import 'dart:io';

import 'package:family_ledger/data/models.dart';
import 'package:flutter_test/flutter_test.dart';

/// 用服务端真实返回的 /sync 响应做解析测试，防止字段名对不上。
/// 夹具由 `scripts/smoke.ps1` 同源的真实接口抓取，见 test/fixtures/sync_sample.json。
void main() {
  final raw =
      File('test/fixtures/sync_sample.json').readAsStringSync().replaceFirst('\uFEFF', '');
  final json = jsonDecode(raw) as Map<String, dynamic>;

  test('分类能按服务端字段解析', () {
    final category = Category.fromMap(
      Map<String, Object?>.from(json['category'] as Map),
    );
    expect(category.id, isNotEmpty);
    expect(category.name, isNotEmpty);
    expect(category.colorValue, greaterThan(0));
    expect(category.iconKey, isNotEmpty);
  });

  test('账户能按服务端字段解析', () {
    final account = Account.fromMap(
      Map<String, Object?>.from(json['account'] as Map),
    );
    expect(account.id, isNotEmpty);
    expect(account.name, isNotEmpty);
    expect(account.kind, isNotEmpty);
  });

  test('成员能按服务端字段解析', () {
    final member = MemberInfo.fromJson(
      Map<String, dynamic>.from(json['member'] as Map),
    );
    expect(member.userId, isNotEmpty);
    expect(member.nickname, isNotEmpty);
    expect(['owner', 'admin', 'member'], contains(member.role));
  });

  test('账目能按服务端字段解析', () {
    final tx = Transaction.fromMap(
      Map<String, Object?>.from(json['transaction'] as Map),
    );
    expect(tx.id, isNotEmpty);
    expect(tx.amountCents, greaterThan(0));
    expect(tx.categoryId, isNotEmpty);
    expect(tx.memberId, isNotEmpty);
    expect(tx.rev, greaterThan(0));
    expect(tx.occurredAt.year, greaterThan(2000));
  });

  test('推送载荷包含服务端必需字段', () {
    final tx = Transaction.fromMap(
      Map<String, Object?>.from(json['transaction'] as Map),
    );
    final payload = tx.toPayload();
    for (final key in <String>[
      'id',
      'type',
      'amount_cents',
      'category_id',
      'occurred_at',
      'visibility',
    ]) {
      expect(payload.containsKey(key), isTrue, reason: '缺少字段 $key');
    }
  });

  test('家庭列表能解析', () {
    final family = Family.fromJson(<String, dynamic>{
      'id': 'f1',
      'name': '我们的家',
      'owner_id': 'u1',
      'my_role': 'owner',
      'member_count': 2,
      'rev': 8,
    });
    expect(family.name, '我们的家');
    expect(family.canManage, isTrue);
    expect(family.memberCount, 2);
  });
}
