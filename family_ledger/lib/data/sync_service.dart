import 'dart:convert';

import 'local_db.dart';
import 'remote_api.dart';

class SyncOutcome {
  const SyncOutcome({
    this.applied = 0,
    this.conflicts = 0,
    this.rejected = 0,
    this.pulled = 0,
    this.error,
  });

  final int applied;
  final int conflicts;
  final int rejected;
  final int pulled;
  final String? error;

  bool get ok => error == null;
}

/// 同步引擎：先推本地队列，再按游标增量拉取。
///
/// 约定：
/// - 服务端 rev 是唯一游标，客户端只记 max(rev)
/// - 删除在服务端是墓碑，本地收到 deleted_at 就删掉
/// - 冲突时用服务端版本覆盖本地，并把 op 丢掉（避免无限重试）
class SyncService {
  SyncService({required this.api, required this.db});

  final RemoteApi api;
  final LocalDatabase db;

  static const int _maxPullRounds = 10;

  Future<SyncOutcome> syncNow(String familyId) async {
    var applied = 0;
    var conflicts = 0;
    var rejected = 0;
    var pulled = 0;

    try {
      // 1) 推送本地待同步队列
      final ops = await db.pendingOps();
      if (ops.isNotEmpty) {
        final payload = ops
            .map((row) => <String, dynamic>{
                  'client_op_id': row['op_id'] as String,
                  'entity': row['entity'] as String,
                  'op': row['op'] as String,
                  'base_rev': row['base_rev'] as int? ?? 0,
                  'payload': jsonDecode(row['payload'] as String) as Map<String, dynamic>,
                })
            .toList();

        final result = await api.syncPush(payload);
        final results = (result['results'] as List<dynamic>? ?? []);
        final byOp = <String, Map<String, dynamic>>{};
        for (final item in results) {
          final map = item as Map<String, dynamic>;
          byOp[map['client_op_id'] as String? ?? ''] = map;
        }

        for (final row in ops) {
          final opId = row['op_id'] as String;
          final res = byOp[opId];
          final status = res?['status'] as String?;
          switch (status) {
            case 'applied':
              await db.removeOp(opId);
              applied++;
            case 'conflict':
              // 用服务端版本覆盖本地，然后丢弃这条操作
              final server = res?['server'];
              if (server is Map<String, dynamic>) {
                await db.upsertTransactions(familyId, [server]);
              }
              await db.removeOp(opId);
              conflicts++;
            default:
              // 校验失败或无权限：不能无限重试，丢弃并提示
              await db.removeOp(opId);
              rejected++;
          }
        }
      }

      // 2) 增量拉取
      var cursor = await db.getCursor(familyId);
      for (var round = 0; round < _maxPullRounds; round++) {
        final data = await api.syncPull(cursor);
        final changes = data['changes'] as Map<String, dynamic>? ?? {};

        final categories = (changes['categories'] as List<dynamic>? ?? [])
            .cast<Map<String, dynamic>>()
            .map((e) => Map<String, Object?>.from(e))
            .toList();
        final accounts = (changes['accounts'] as List<dynamic>? ?? [])
            .cast<Map<String, dynamic>>()
            .map((e) => Map<String, Object?>.from(e))
            .toList();
        final transactions = (changes['transactions'] as List<dynamic>? ?? [])
            .cast<Map<String, dynamic>>()
            .map((e) => Map<String, Object?>.from(e))
            .toList();
        final members = (changes['members'] as List<dynamic>? ?? [])
            .cast<Map<String, dynamic>>()
            .map((e) => Map<String, Object?>.from(e))
            .toList();

        if (categories.isNotEmpty) await db.upsertCategories(categories);
        if (accounts.isNotEmpty) await db.upsertAccounts(accounts);
        if (transactions.isNotEmpty) await db.upsertTransactions(familyId, transactions);
        if (members.isNotEmpty) await db.replaceMembers(members);
        pulled += transactions.length;

        cursor = (data['cursor'] as num?)?.toInt() ?? cursor;
        await db.setCursor(familyId, cursor);

        if (data['has_more'] != true) break;
      }

      return SyncOutcome(
        applied: applied,
        conflicts: conflicts,
        rejected: rejected,
        pulled: pulled,
        error: rejected > 0 ? '有 $rejected 条记录被服务器拒绝' : null,
      );
    } on ApiException catch (e) {
      return SyncOutcome(
        applied: applied,
        conflicts: conflicts,
        rejected: rejected,
        pulled: pulled,
        error: e.message,
      );
    } catch (e) {
      return SyncOutcome(
        applied: applied,
        conflicts: conflicts,
        rejected: rejected,
        pulled: pulled,
        error: '同步失败：$e',
      );
    }
  }
}
