import 'dart:io';

import 'package:family_ledger/core/ids.dart';
import 'package:family_ledger/data/models.dart';
import 'package:family_ledger/data/remote_api.dart';
import 'package:family_ledger/data/token_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 真连后端的集成测试（默认跳过）。
///
///   $env:LEDGER_TEST_ACCOUNT='your-account'
///   $env:LEDGER_TEST_PASSWORD='your-password'
///   $env:LEDGER_TEST_BASE_URL='http://127.0.0.1:8088'
///   flutter test test/api_integration_test.dart
void main() {
  final account = Platform.environment['LEDGER_TEST_ACCOUNT'];
  final password = Platform.environment['LEDGER_TEST_PASSWORD'];
  final baseUrl =
      Platform.environment['LEDGER_TEST_BASE_URL'] ?? 'http://127.0.0.1:8088';

  if (account == null || password == null) {
    test('集成测试已跳过（未设置 LEDGER_TEST_ACCOUNT / LEDGER_TEST_PASSWORD）', () {},
        skip: '需要真实后端账号');
    return;
  }

  late RemoteApi api;
  late TokenStore tokens;
  late Family family;
  final createdIds = <String>[];

  setUpAll(() async {
    TestWidgetsFlutterBinding.ensureInitialized();
    // flutter_test 默认把所有 HTTP 拦成 400，这里放开真实网络
    HttpOverrides.global = null;
    SharedPreferences.setMockInitialValues(<String, Object>{});
    tokens = await TokenStore.load();
    api = RemoteApi(baseUrl: baseUrl, tokens: tokens);

    final session = await api.login(account: account, password: password);
    await tokens.saveSession(
      accessToken: session.accessToken,
      refreshToken: session.refreshToken,
      userId: session.user.id,
      nickname: session.user.nickname,
      account: session.user.account,
    );
  });

  tearDownAll(() async {
    // 清理测试数据，避免污染账本
    for (final id in createdIds) {
      try {
        await api.syncPush([
          {
            'client_op_id': 'cleanup-$id',
            'entity': 'transaction',
            'op': 'delete',
            'payload': {'id': id},
          }
        ]);
      } catch (_) {}
    }
  });

  test('登录后能拿到用户与唯一家庭', () async {
    final (user, familyOrNull) = await api.me();
    expect(user.account, account);
    expect(familyOrNull, isNotNull);
    family = familyOrNull!;
    expect(family.name, isNotEmpty);
  });

  test('首次同步能拿到分类与账户', () async {
    final data = await api.syncPull(0);
    final changes = data['changes'] as Map<String, dynamic>;
    final categories = (changes['categories'] as List)
        .map((e) => Category.fromMap(Map<String, Object?>.from(e as Map)))
        .toList();
    final accounts = (changes['accounts'] as List)
        .map((e) => Account.fromMap(Map<String, Object?>.from(e as Map)))
        .toList();
    expect(categories.length, greaterThanOrEqualTo(16));
    expect(accounts.length, greaterThanOrEqualTo(4));
    expect((data['cursor'] as num).toInt(), greaterThan(0));
  });

  test('客户端生成的账目能推上去，并被其他端拉到', () async {
    final before = await api.syncPull(0);
    final cursor = (before['cursor'] as num).toInt();
    final categoryId =
        (before['changes']['categories'] as List).first['id'] as String;

    final id = newId();
    createdIds.add(id);
    final result = await api.syncPush([
      {
        'client_op_id': 'test-$id',
        'entity': 'transaction',
        'op': 'upsert',
        'base_rev': 0,
        'payload': {
          'id': id,
          'type': 0,
          'amount_cents': 1234,
          'category_id': categoryId,
          'occurred_at': DateTime.now().millisecondsSinceEpoch,
          'note': '集成测试',
          'visibility': 'family',
        },
      }
    ]);
    final results = result['results'] as List;
    expect(results.first['status'], 'applied');

    final after = await api.syncPull(cursor);
    final txs = (after['changes']['transactions'] as List)
        .map((e) => Transaction.fromMap(Map<String, Object?>.from(e as Map)))
        .toList();
    expect(txs.any((t) => t.id == id), isTrue, reason: '增量拉取应包含刚推送的账目');
  });

  test('未登录访问会抛 ApiException', () async {
    await tokens.clear();
    final fresh = RemoteApi(baseUrl: baseUrl, tokens: await TokenStore.load());
    expect(
      () => fresh.me(),
      throwsA(isA<ApiException>()),
    );
  });
}
