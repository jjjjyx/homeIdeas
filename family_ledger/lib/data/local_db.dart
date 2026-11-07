import 'dart:convert';

import 'package:path/path.dart' as p;
import 'package:sqflite/sqflite.dart';

/// 本地库：只做「缓存 + 待同步队列」，真正的数据以服务端为准。
///
/// v1 是纯本地记账（categories / entries 两张表）；
/// v2 改成多人版后重新组织表结构，旧表改名为 legacy_* 保留，避免升级丢数据。
class LocalDatabase {
  LocalDatabase._();

  static final LocalDatabase instance = LocalDatabase._();

  static const String _fileName = 'family_ledger.db';
  static const int _version = 2;

  Database? _db;

  Future<Database> get database async => _db ??= await _open();

  Future<Database> _open() async {
    final dir = await getDatabasesPath();
    final path = p.join(dir, _fileName);
    return openDatabase(
      path,
      version: _version,
      onConfigure: (db) => db.execute('PRAGMA foreign_keys = ON'),
      onCreate: (db, version) => _createV2(db),
      onUpgrade: _upgrade,
    );
  }

  Future<void> _upgrade(Database db, int from, int to) async {
    if (from < 2) {
      // 旧版本数据留档，不自动迁移（旧分类是本地自增 id，与云端 UUID 对不上）
      final tables = await db.rawQuery(
        "SELECT name FROM sqlite_master WHERE type='table'",
      );
      final names = tables.map((e) => e['name'] as String).toSet();
      if (names.contains('entries')) {
        await db.execute('ALTER TABLE entries RENAME TO legacy_entries');
      }
      if (names.contains('categories')) {
        await db.execute('ALTER TABLE categories RENAME TO legacy_categories');
      }
      await _createV2(db);
    }
  }

  Future<void> _createV2(Database db) async {
    await db.execute('''
      CREATE TABLE IF NOT EXISTS categories (
        id TEXT PRIMARY KEY,
        name TEXT NOT NULL,
        type INTEGER NOT NULL,
        icon_key TEXT NOT NULL,
        color INTEGER NOT NULL,
        sort_order INTEGER NOT NULL DEFAULT 0,
        rev INTEGER NOT NULL DEFAULT 0,
        updated_at INTEGER NOT NULL DEFAULT 0
      )
    ''');
    await db.execute('''
      CREATE TABLE IF NOT EXISTS accounts (
        id TEXT PRIMARY KEY,
        name TEXT NOT NULL,
        kind TEXT NOT NULL DEFAULT 'other',
        sort_order INTEGER NOT NULL DEFAULT 0,
        rev INTEGER NOT NULL DEFAULT 0,
        updated_at INTEGER NOT NULL DEFAULT 0
      )
    ''');
    await db.execute('''
      CREATE TABLE IF NOT EXISTS transactions (
        id TEXT PRIMARY KEY,
        family_id TEXT NOT NULL,
        type INTEGER NOT NULL,
        amount_cents INTEGER NOT NULL,
        category_id TEXT NOT NULL,
        account_id TEXT NOT NULL DEFAULT '',
        member_id TEXT NOT NULL,
        occurred_at INTEGER NOT NULL,
        note TEXT NOT NULL DEFAULT '',
        visibility TEXT NOT NULL DEFAULT 'family',
        tags TEXT NOT NULL DEFAULT '',
        rev INTEGER NOT NULL DEFAULT 0,
        created_at INTEGER NOT NULL,
        updated_at INTEGER NOT NULL
      )
    ''');
    await db.execute(
      'CREATE INDEX IF NOT EXISTS idx_tx_time ON transactions (occurred_at DESC)',
    );
    await db.execute('''
      CREATE TABLE IF NOT EXISTS members (
        user_id TEXT PRIMARY KEY,
        nickname TEXT NOT NULL,
        account TEXT NOT NULL,
        role TEXT NOT NULL,
        joined_at INTEGER NOT NULL
      )
    ''');
    await db.execute('''
      CREATE TABLE IF NOT EXISTS outbox (
        op_id TEXT PRIMARY KEY,
        entity TEXT NOT NULL,
        op TEXT NOT NULL,
        payload TEXT NOT NULL,
        base_rev INTEGER NOT NULL DEFAULT 0,
        created_at INTEGER NOT NULL
      )
    ''');
    await db.execute('''
      CREATE TABLE IF NOT EXISTS sync_state (
        k TEXT PRIMARY KEY,
        v TEXT NOT NULL
      )
    ''');
  }

  Future<void> close() async {
    await _db?.close();
    _db = null;
  }

  // ---------- 读 ----------

  Future<List<Map<String, Object?>>> allCategories() async {
    final db = await database;
    return db.query('categories', orderBy: 'type ASC, sort_order ASC');
  }

  Future<List<Map<String, Object?>>> allAccounts() async {
    final db = await database;
    return db.query('accounts', orderBy: 'sort_order ASC');
  }

  Future<List<Map<String, Object?>>> allTransactions(String familyId) async {
    final db = await database;
    return db.query(
      'transactions',
      where: 'family_id = ?',
      whereArgs: [familyId],
      orderBy: 'occurred_at DESC, id DESC',
    );
  }

  Future<List<Map<String, Object?>>> allMembers() async {
    final db = await database;
    return db.query('members', orderBy: 'joined_at ASC');
  }

  Future<Map<String, Object?>?> transactionById(String id) async {
    final db = await database;
    final rows = await db.query('transactions', where: 'id = ?', whereArgs: [id], limit: 1);
    return rows.isEmpty ? null : rows.first;
  }

  // ---------- 写（服务端数据落地） ----------

  Future<void> upsertCategories(List<Map<String, Object?>> rows) async {
    final db = await database;
    final batch = db.batch();
    for (final row in rows) {
      if (row['deleted_at'] != null) {
        batch.delete('categories', where: 'id = ?', whereArgs: [row['id']]);
      } else {
        batch.insert('categories', {
          'id': row['id'],
          'name': row['name'],
          'type': row['type'],
          'icon_key': row['icon_key'],
          'color': row['color'],
          'sort_order': row['sort_order'] ?? 0,
          'rev': row['rev'] ?? 0,
          'updated_at': row['updated_at'] ?? 0,
        }, conflictAlgorithm: ConflictAlgorithm.replace);
      }
    }
    await batch.commit(noResult: true);
  }

  Future<void> upsertAccounts(List<Map<String, Object?>> rows) async {
    final db = await database;
    final batch = db.batch();
    for (final row in rows) {
      if (row['deleted_at'] != null) {
        batch.delete('accounts', where: 'id = ?', whereArgs: [row['id']]);
      } else {
        batch.insert('accounts', {
          'id': row['id'],
          'name': row['name'],
          'kind': row['kind'] ?? 'other',
          'sort_order': row['sort_order'] ?? 0,
          'rev': row['rev'] ?? 0,
          'updated_at': row['updated_at'] ?? 0,
        }, conflictAlgorithm: ConflictAlgorithm.replace);
      }
    }
    await batch.commit(noResult: true);
  }

  Future<void> upsertTransactions(String familyId, List<Map<String, Object?>> rows) async {
    final db = await database;
    final batch = db.batch();
    for (final row in rows) {
      if (row['deleted_at'] != null) {
        batch.delete('transactions', where: 'id = ?', whereArgs: [row['id']]);
        continue;
      }
      batch.insert('transactions', {
        'id': row['id'],
        'family_id': familyId,
        'type': row['type'],
        'amount_cents': row['amount_cents'],
        'category_id': row['category_id'],
        'account_id': row['account_id'] ?? '',
        'member_id': row['member_id'] ?? '',
        'occurred_at': row['occurred_at'],
        'note': row['note'] ?? '',
        'visibility': row['visibility'] ?? 'family',
        'tags': row['tags'] ?? '',
        'rev': row['rev'] ?? 0,
        'created_at': row['created_at'] ?? 0,
        'updated_at': row['updated_at'] ?? 0,
      }, conflictAlgorithm: ConflictAlgorithm.replace);
    }
    await batch.commit(noResult: true);
  }

  Future<void> replaceMembers(List<Map<String, Object?>> rows) async {
    final db = await database;
    final batch = db.batch();
    batch.delete('members');
    for (final row in rows) {
      batch.insert('members', {
        'user_id': row['user_id'],
        'nickname': row['nickname'] ?? '',
        'account': row['account'] ?? '',
        'role': row['role'] ?? 'member',
        'joined_at': row['joined_at'] ?? 0,
      }, conflictAlgorithm: ConflictAlgorithm.replace);
    }
    await batch.commit(noResult: true);
  }

  /// 本地写（乐观更新）
  Future<void> saveTransaction(String familyId, Map<String, Object?> row) async {
    final db = await database;
    await db.insert(
      'transactions',
      {...row, 'family_id': familyId},
      conflictAlgorithm: ConflictAlgorithm.replace,
    );
  }

  Future<void> deleteTransaction(String id) async {
    final db = await database;
    await db.delete('transactions', where: 'id = ?', whereArgs: [id]);
  }

  // ---------- 待同步队列 ----------

  Future<void> enqueue({
    required String opId,
    required String entity,
    required String op,
    required Map<String, Object?> payload,
    required int baseRev,
  }) async {
    final db = await database;
    await db.insert('outbox', {
      'op_id': opId,
      'entity': entity,
      'op': op,
      'payload': jsonEncode(payload),
      'base_rev': baseRev,
      'created_at': DateTime.now().millisecondsSinceEpoch,
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<List<Map<String, Object?>>> pendingOps({int limit = 100}) async {
    final db = await database;
    return db.query('outbox', orderBy: 'created_at ASC', limit: limit);
  }

  Future<void> removeOp(String opId) async {
    final db = await database;
    await db.delete('outbox', where: 'op_id = ?', whereArgs: [opId]);
  }

  Future<int> pendingCount() async {
    final db = await database;
    final rows = await db.rawQuery('SELECT COUNT(*) AS n FROM outbox');
    return (rows.first['n'] as int?) ?? 0;
  }

  // ---------- 同步状态 ----------

  Future<int> getCursor(String familyId) async {
    final db = await database;
    final rows = await db.query('sync_state', where: 'k = ?', whereArgs: ['cursor:$familyId']);
    if (rows.isEmpty) return 0;
    return int.tryParse(rows.first['v'] as String? ?? '0') ?? 0;
  }

  Future<void> setCursor(String familyId, int cursor) async {
    final db = await database;
    await db.insert('sync_state', {'k': 'cursor:$familyId', 'v': '$cursor'},
        conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<String?> getMeta(String key) async {
    final db = await database;
    final rows = await db.query('sync_state', where: 'k = ?', whereArgs: ['meta:$key']);
    return rows.isEmpty ? null : rows.first['v'] as String?;
  }

  Future<void> setMeta(String key, String value) async {
    final db = await database;
    await db.insert('sync_state', {'k': 'meta:$key', 'v': value},
        conflictAlgorithm: ConflictAlgorithm.replace);
  }

  /// 切换家庭 / 退出登录时清空缓存
  Future<void> clearSyncedData() async {
    final db = await database;
    final batch = db.batch();
    batch.delete('categories');
    batch.delete('accounts');
    batch.delete('transactions');
    batch.delete('members');
    batch.delete('outbox');
    batch.delete('sync_state');
    await batch.commit(noResult: true);
  }
}
