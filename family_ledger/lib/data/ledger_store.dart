import 'dart:async';

import 'package:flutter/foundation.dart' show ChangeNotifier, ValueNotifier;

import '../core/ids.dart';
import '../core/money.dart';
import 'local_db.dart';
import 'models.dart';
import 'remote_api.dart';
import 'sync_service.dart';

class MonthSummary {
  const MonthSummary({required this.incomeCents, required this.expenseCents});

  final int incomeCents;
  final int expenseCents;

  int get balanceCents => incomeCents - expenseCents;
  bool get isEmpty => incomeCents == 0 && expenseCents == 0;
}

class MonthBucket {
  const MonthBucket({
    required this.month,
    required this.incomeCents,
    required this.expenseCents,
  });

  final DateTime month;
  final int incomeCents;
  final int expenseCents;
}

class CategorySlice {
  const CategorySlice({
    required this.category,
    required this.amountCents,
    required this.ratio,
  });

  final Category category;
  final int amountCents;
  final double ratio;
}

/// 本地缓存的读写入口：读永远走本地（秒开），写先落本地再进同步队列。
class LedgerStore extends ChangeNotifier {
  LedgerStore({required this.db, required this.sync, required this.api}) {
    final now = DateTime.now();
    _month = ValueNotifier<DateTime>(DateTime(now.year, now.month));
    _month.addListener(notifyListeners);
  }

  final LocalDatabase db;
  final SyncService sync;
  final RemoteApi api;

  String familyId = '';

  Map<String, dynamic> _scope = const <String, dynamic>{};
  Map<String, dynamic> get scopeBreakdown => _scope;
  int _pendingClassify = 0;
  int get pendingClassify => _pendingClassify;
  String userId = '';

  late final ValueNotifier<DateTime> _month;
  ValueNotifier<DateTime> get month => _month;
  DateTime get selectedMonth => _month.value;

  List<Category> _categories = const [];
  List<Account> _accounts = const [];
  List<Transaction> _entries = const [];
  List<MemberInfo> _members = const [];

  bool _loading = true;
  bool _syncing = false;
  int _pending = 0;
  DateTime? _lastSyncAt;
  String? _lastError;
  Timer? _debounce;

  List<Category> get categories => _categories;
  List<Account> get accounts => _accounts;
  List<Transaction> get entries => _entries;
  List<MemberInfo> get members => _members;
  bool get loading => _loading;
  bool get syncing => _syncing;
  int get pendingCount => _pending;
  DateTime? get lastSyncAt => _lastSyncAt;
  String? get lastSyncError => _lastError;

  Category? categoryOf(String id) {
    for (final c in _categories) {
      if (c.id == id) return c;
    }
    return null;
  }

  String memberName(String id) {
    for (final m in _members) {
      if (m.userId == id) return m.nickname;
    }
    return '';
  }

  List<Category> categoriesOf(EntryType type) =>
      _categories.where((c) => c.type == type).toList(growable: false);

  /// 绑定家庭后：先读本地缓存，再触发一次同步。
  Future<void> configure(String familyId, String userId) async {
    this.familyId = familyId;
    this.userId = userId;
    await load();
    unawaited(syncNow(silent: true));
  }

  Future<void> load() async {
    final cats = await db.allCategories();
    final accts = await db.allAccounts();
    await refreshScope();
    final txs = await db.allTransactions(familyId);
    final members = await db.allMembers();
    final pending = await db.pendingCount();

    _categories = cats.map(Category.fromMap).toList(growable: false);
    _accounts = accts.map(Account.fromMap).toList(growable: false);
    _members = members
        .map((m) => MemberInfo(
              userId: m['user_id']! as String,
              nickname: (m['nickname'] as String?) ?? '',
              account: (m['account'] as String?) ?? '',
              role: (m['role'] as String?) ?? 'member',
              joinedAt: (m['joined_at'] as int?) ?? 0,
            ))
        .toList(growable: false);

    final parsed = txs.map(Transaction.fromMap).toList();
    parsed.sort((a, b) {
      final byTime = b.occurredAt.compareTo(a.occurredAt);
      if (byTime != 0) return byTime;
      return b.id.compareTo(a.id);
    });
    _entries = parsed;
    _pending = pending;
    _loading = false;
    notifyListeners();
  }

  Future<SyncOutcome> syncNow({bool silent = false}) async {
    if (_syncing || familyId.isEmpty) return const SyncOutcome();
    _syncing = true;
    if (!silent) notifyListeners();

    final outcome = await sync.syncNow(familyId);
    _lastError = outcome.error;
    if (outcome.ok) _lastSyncAt = DateTime.now();

    await load();
    _syncing = false;
    notifyListeners();
    return outcome;
  }

  /// 写操作后延迟触发同步，避免连续记账时频繁请求。
  void _scheduleSync() {
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 600), () {
      unawaited(syncNow(silent: true));
    });
  }

  /// 拉本月「家庭/个人/资金往来」口径 + 待确认数量
  Future<void> refreshScope() async {
    if (familyId.isEmpty) return;
    try {
      final month = '${selectedMonth.year}-${selectedMonth.month.toString().padLeft(2, '0')}';
      final data = await api.scopeBreakdown(familyId, month: month);
      _scope = data;
      final pending = await api.classifyPending(familyId, minConfidence: 0.7);
      _pendingClassify = (pending['pending_count'] as int?) ?? 0;
      notifyListeners();
    } catch (_) {
      // 口径数据失败不影响主流程
    }
  }
  void selectMonth(DateTime value) {
    final normalized = monthStart(value);
    if (isSameMonth(normalized, _month.value)) return;
    _month.value = normalized;
    refreshScope();
  }

  void shiftMonth(int delta) => selectMonth(addMonths(_month.value, delta));

  List<Transaction> entriesOfMonth(DateTime month) {
    final start = monthStart(month);
    final end = addMonths(start, 1);
    return _entries
        .where((e) => !e.occurredAt.isBefore(start) && e.occurredAt.isBefore(end))
        .toList(growable: false);
  }

  List<Transaction> get entriesInSelectedMonth => entriesOfMonth(_month.value);

  MonthSummary summaryOfMonth(DateTime month) {
    var income = 0;
    var expense = 0;
    for (final entry in entriesOfMonth(month)) {
      if (entry.type.isIncome) {
        income += entry.amountCents;
      } else {
        expense += entry.amountCents;
      }
    }
    return MonthSummary(incomeCents: income, expenseCents: expense);
  }

  MonthSummary get selectedSummary => summaryOfMonth(_month.value);

  List<CategorySlice> categoryBreakdown(DateTime month, EntryType type) {
    final totals = <String, int>{};
    var sum = 0;
    for (final entry in entriesOfMonth(month)) {
      if (entry.type != type) continue;
      totals.update(entry.categoryId, (v) => v + entry.amountCents,
          ifAbsent: () => entry.amountCents);
      sum += entry.amountCents;
    }
    final slices = totals.entries
        .map((e) => CategorySlice(
              category: categoryOf(e.key) ??
                  Category(
                    id: e.key,
                    name: '已删除',
                    type: type,
                    iconKey: 'other',
                    colorValue: 0xFF78909C,
                    sortOrder: 999,
                    rev: 0,
                  ),
              amountCents: e.value,
              ratio: sum == 0 ? 0 : e.value / sum,
            ))
        .toList();
    slices.sort((a, b) => b.amountCents.compareTo(a.amountCents));
    return slices;
  }

  List<MonthBucket> trend(int count) {
    final current = _month.value;
    final buckets = <MonthBucket>[];
    for (var i = count - 1; i >= 0; i--) {
      final m = addMonths(current, -i);
      final summary = summaryOfMonth(m);
      buckets.add(MonthBucket(
        month: m,
        incomeCents: summary.incomeCents,
        expenseCents: summary.expenseCents,
      ));
    }
    return buckets;
  }

  List<MapEntry<DateTime, List<Transaction>>> groupedByDay(
      List<Transaction> items) {
    final groups = <MapEntry<DateTime, List<Transaction>>>[];
    for (final entry in items) {
      final day = DateTime(
          entry.occurredAt.year, entry.occurredAt.month, entry.occurredAt.day);
      if (groups.isNotEmpty && groups.last.key == day) {
        groups.last.value.add(entry);
      } else {
        groups.add(MapEntry(day, <Transaction>[entry]));
      }
    }
    return groups;
  }

  // ---------- 写 ----------

  Future<Transaction> addEntry({
    required EntryType type,
    required int amountCents,
    required String categoryId,
    required DateTime date,
    String note = '',
    String accountId = '',
    EntryVisibility visibility = EntryVisibility.family,
  }) async {
    final now = DateTime.now();
    final tx = Transaction(
      id: newId(),
      type: type,
      amountCents: amountCents,
      categoryId: categoryId,
      accountId: accountId,
      memberId: userId,
      occurredAt: date,
      note: note,
      visibility: visibility,
      rev: 0,
      createdAt: now.millisecondsSinceEpoch,
      updatedAt: now.millisecondsSinceEpoch,
    );
    await db.saveTransaction(familyId, tx.toMap());
    await db.enqueue(
      opId: newId(),
      entity: 'transaction',
      op: 'upsert',
      payload: tx.toPayload(),
      baseRev: 0,
    );
    await load();
    _scheduleSync();
    return tx;
  }

  Future<void> updateEntry(Transaction entry) async {
    final updated = entry.copyWith(updatedAt: DateTime.now().millisecondsSinceEpoch);
    await db.saveTransaction(familyId, updated.toMap());
    await db.enqueue(
      opId: newId(),
      entity: 'transaction',
      op: 'upsert',
      payload: updated.toPayload(),
      baseRev: entry.rev,
    );
    await load();
    _scheduleSync();
  }

  Future<void> deleteEntry(Transaction entry) async {
    await db.deleteTransaction(entry.id);
    await db.enqueue(
      opId: newId(),
      entity: 'transaction',
      op: 'delete',
      payload: {'id': entry.id},
      baseRev: entry.rev,
    );
    await load();
    _scheduleSync();
  }

  /// 切换家庭：清空缓存后重新同步
  Future<void> switchFamily(String newFamilyId, String userId) async {
    await db.clearSyncedData();
    familyId = newFamilyId;
    this.userId = userId;
    _entries = const [];
    _categories = const [];
    _accounts = const [];
    _members = const [];
    notifyListeners();
    await configure(newFamilyId, userId);
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _month.removeListener(notifyListeners);
    _month.dispose();
    super.dispose();
  }
}
