import 'package:family_ledger/data/local_db.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

/// 本地缓存层测试：用 sqflite 的内存实现跑，不依赖真机。
void main() {
  late String dbPath;

  setUpAll(() async {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfi;
    dbPath = '${await databaseFactory.getDatabasesPath()}/family_ledger.db';
  });

  Future<void> resetDb() async {
    await LocalDatabase.instance.close();
    await databaseFactory.deleteDatabase(dbPath);
  }

  setUp(resetDb);
  tearDown(resetDb);

  test('v1 老库升级到 v2：旧表改名保留，新表建好', () async {
    // 手工造一个 v1 结构的库
    final old = await databaseFactory.openDatabase(
      dbPath,
      options: OpenDatabaseOptions(
        version: 1,
        onCreate: (db, version) async {
          await db.execute('''
            CREATE TABLE categories (
              id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, type INTEGER,
              icon_key TEXT, color INTEGER, sort_order INTEGER
            )
          ''');
          await db.execute('''
            CREATE TABLE entries (
              id INTEGER PRIMARY KEY AUTOINCREMENT, type INTEGER, amount_cents INTEGER,
              category_id INTEGER, date INTEGER, note TEXT, created_at INTEGER
            )
          ''');
          await db.insert('entries', {
            'type': 0,
            'amount_cents': 2850,
            'category_id': 1,
            'date': DateTime.now().millisecondsSinceEpoch,
            'note': '旧数据',
            'created_at': DateTime.now().millisecondsSinceEpoch,
          });
        },
      ),
    );
    await old.close();

    final db = await LocalDatabase.instance.database;
    final tables = (await db.rawQuery("SELECT name FROM sqlite_master WHERE type='table'"))
        .map((e) => e['name'] as String)
        .toSet();

    expect(tables, contains('legacy_entries'));
    expect(tables, contains('legacy_categories'));
    expect(tables, containsAll(<String>['transactions', 'accounts', 'outbox', 'sync_state']));

    // 旧数据没丢
    final legacy = await db.rawQuery('SELECT COUNT(*) AS n FROM legacy_entries');
    expect(legacy.first['n'], 1);
  });

  test('同步数据落库、墓碑删除、游标与 outbox', () async {
    final db = LocalDatabase.instance;
    const familyId = 'fam-1';

    await db.upsertCategories(<Map<String, Object?>>[
      {'id': 'c1', 'name': '餐饮', 'type': 0, 'icon_key': 'restaurant', 'color': 0xFFEF5350, 'sort_order': 0, 'rev': 2, 'updated_at': 1},
    ]);
    await db.upsertTransactions(familyId, <Map<String, Object?>>[
      {'id': 't1', 'type': 0, 'amount_cents': 12850, 'category_id': 'c1', 'member_id': 'u1', 'occurred_at': 1000, 'rev': 3},
      {'id': 't2', 'type': 1, 'amount_cents': 500, 'category_id': 'c1', 'member_id': 'u1', 'occurred_at': 2000, 'rev': 4},
    ]);

    expect((await db.allCategories()).length, 1);
    expect((await db.allTransactions(familyId)).length, 2);

    // 墓碑：deleted_at 非空时应从本地删掉
    await db.upsertTransactions(familyId, <Map<String, Object?>>[
      {'id': 't1', 'deleted_at': 9999, 'rev': 5},
    ]);
    final left = await db.allTransactions(familyId);
    expect(left.length, 1);
    expect(left.first['id'], 't2');

    // 游标
    await db.setCursor(familyId, 42);
    expect(await db.getCursor(familyId), 42);
    expect(await db.getCursor('other-fam'), 0);

    // outbox
    await db.enqueue(opId: 'op1', entity: 'transaction', op: 'upsert', payload: {'id': 't3'}, baseRev: 0);
    await db.enqueue(opId: 'op2', entity: 'transaction', op: 'delete', payload: {'id': 't2'}, baseRev: 4);
    expect(await db.pendingCount(), 2);
    final pending = await db.pendingOps();
    expect(pending.first['op_id'], 'op1'); // 按创建顺序
    await db.removeOp('op1');
    expect(await db.pendingCount(), 1);

    // 清空
    await db.clearSyncedData();
    expect(await db.allTransactions(familyId), isEmpty);
    expect(await db.allCategories(), isEmpty);
    expect(await db.pendingCount(), 0);
    expect(await db.getCursor(familyId), 0);
  });

  test('切换家庭不会串数据（清理后重新拉）', () async {
    final db = LocalDatabase.instance;
    await db.upsertCategories(<Map<String, Object?>>[
      {'id': 'a-cat', 'name': 'A家分类', 'type': 0, 'icon_key': 'other', 'color': 0, 'sort_order': 0, 'rev': 1, 'updated_at': 1},
    ]);
    await db.upsertTransactions('family-a', <Map<String, Object?>>[
      {'id': 'a1', 'type': 0, 'amount_cents': 100, 'category_id': 'a-cat', 'member_id': 'u1', 'occurred_at': 1, 'rev': 2},
    ]);
    await db.setCursor('family-a', 2);

    await db.clearSyncedData();

    expect(await db.allCategories(), isEmpty);
    expect(await db.allTransactions('family-a'), isEmpty);
    expect(await db.getCursor('family-a'), 0);
  });
}
