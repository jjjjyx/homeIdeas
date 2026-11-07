import 'package:flutter/material.dart';

import '../core/money.dart';
import '../data/ledger_store.dart';
import '../data/models.dart';
import 'entry_edit_page.dart';
import 'split_page.dart';
import 'classify_review_page.dart';
import 'widgets/common.dart';

class HomePage extends StatelessWidget {
  const HomePage({super.key, required this.store});

  final LedgerStore store;

  Future<void> _confirmDelete(BuildContext context, Transaction entry) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('删除这笔记录？'),
        content: Text(
          '${entry.note.isEmpty ? '这笔' : entry.note} ${formatMoney(entry.amountCents)} 元',
        ),
        actions: <Widget>[
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      await store.deleteEntry(entry);
    }
  }

  /// 长按账目：编辑 / 拆分 / 删除
  Future<void> _showActions(BuildContext context, Transaction entry) async {
    await showModalBottomSheet<void>(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            ListTile(
              leading: const Icon(Icons.edit_outlined),
              title: const Text('编辑'),
              onTap: () {
                Navigator.pop(ctx);
                openEntryEditor(context, store, entry: entry);
              },
            ),
            if (entry.type == EntryType.expense)
              ListTile(
                leading: const Icon(Icons.call_split_rounded),
                title: const Text('拆分这笔'),
                subtitle: const Text('家庭用品和个人用品混在一起时用',
                    style: TextStyle(fontSize: 12)),
                onTap: () {
                  Navigator.pop(ctx);
                  Navigator.of(context).push<void>(
                    MaterialPageRoute<void>(
                      builder: (_) => SplitPage(store: store, entry: entry),
                    ),
                  );
                },
              ),
            ListTile(
              leading: const Icon(Icons.delete_outline, color: Color(0xFFD64545)),
              title: const Text('删除', style: TextStyle(color: Color(0xFFD64545))),
              onTap: () {
                Navigator.pop(ctx);
                _confirmDelete(context, entry);
              },
            ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      bottom: false,
      child: ListenableBuilder(
        listenable: store,
        builder: (context, _) {
          final summary = store.selectedSummary;
          final entries = store.entriesInSelectedMonth;
          final groups = store.groupedByDay(entries);

          return RefreshIndicator(
            onRefresh: () => store.syncNow(),
            child: CustomScrollView(
              physics: const AlwaysScrollableScrollPhysics(),
              slivers: <Widget>[
                SliverToBoxAdapter(child: _Header(pendingCount: store.pendingCount)),
                SliverToBoxAdapter(child: MonthSelector(store: store)),
                SliverToBoxAdapter(child: SummaryCard(summary: summary)),
                SliverToBoxAdapter(child: _ScopeStrip(store: store)),
                if (store.pendingClassify > 0)
                  SliverToBoxAdapter(child: _PendingClassifyBanner(store: store)),
                SliverToBoxAdapter(
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(20, 12, 20, 2),
                    child: Row(
                      children: <Widget>[
                        const Text(
                          '账单明细',
                          style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600),
                        ),
                        const Spacer(),
                        Text(
                          '共 ${entries.length} 笔',
                          style: TextStyle(fontSize: 12, color: Colors.grey.shade500),
                        ),
                      ],
                    ),
                  ),
                ),
                if (groups.isEmpty)
                  const SliverFillRemaining(hasScrollBody: false, child: _EmptyState())
                else
                  SliverList.list(
                    children: <Widget>[
                      for (final group in groups) ...<Widget>[
                        DayHeader(
                          day: group.key,
                          incomeCents: group.value
                              .where((e) => e.type.isIncome)
                              .fold(0, (sum, e) => sum + e.amountCents),
                          expenseCents: group.value
                              .where((e) => !e.type.isIncome)
                              .fold(0, (sum, e) => sum + e.amountCents),
                        ),
                        for (final entry in group.value)
                          EntryTile(
                            entry: entry,
                            category: store.categoryOf(entry.categoryId),
                            memberName: store.memberName(entry.memberId),
                            onTap: () => openEntryEditor(context, store, entry: entry),
                            onLongPress: () => _showActions(context, entry),
                          ),
                      ],
                    ],
                  ),
                // 给悬浮按钮留出空间，避免遮住最后一条账单
                const SliverToBoxAdapter(child: SizedBox(height: 150)),
              ],
            ),
          );
        },
      ),
    );
  }
}

class _Header extends StatelessWidget {
  const _Header({required this.pendingCount});

  final int pendingCount;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 18, 20, 4),
      child: Row(
        children: <Widget>[
          Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              const Text(
                '家庭记账本',
                style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 2),
              Text(
                pendingCount > 0 ? '$pendingCount 条待同步' : '已与家人同步',
                style: TextStyle(
                  fontSize: 12,
                  color: pendingCount > 0 ? Colors.orange.shade700 : Colors.grey.shade500,
                ),
              ),
            ],
          ),
          const Spacer(),
          if (pendingCount > 0)
            Icon(Icons.cloud_upload_outlined, size: 20, color: Colors.orange.shade400),
        ],
      ),
    );
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState();

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 48),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: <Widget>[
          Icon(Icons.edit_note_rounded, size: 64, color: Colors.grey.shade300),
          const SizedBox(height: 12),
          Text(
            '这个月还没有记账',
            style: TextStyle(fontSize: 15, color: Colors.grey.shade600),
          ),
          const SizedBox(height: 4),
          Text(
            '点右下角的 + 记一笔，家人也能看到',
            style: TextStyle(fontSize: 12, color: Colors.grey.shade400),
          ),
        ],
      ),
    );
  }
}

/// 家庭 / 个人 / 资金往来的口径条
class _ScopeStrip extends StatelessWidget {
  const _ScopeStrip({required this.store});

  final LedgerStore store;

  @override
  Widget build(BuildContext context) {
    final data = store.scopeBreakdown;
    final items = (data['items'] as List<dynamic>? ?? <dynamic>[]).cast<Map<String, dynamic>>();
    if (items.isEmpty) return const SizedBox.shrink();

    Map<String, dynamic>? pick(String scope) {
      for (final it in items) {
        if (it['scope'] == scope) return it;
      }
      return null;
    }

    final family = pick('family');
    final personal = pick('personal');
    final transfer = pick('transfer');

    String money(Map<String, dynamic>? m) =>
        m == null ? '0' : ((m['amount_cents'] as int? ?? 0) / 100).toStringAsFixed(0);

    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 10, 20, 0),
      child: Row(
        children: <Widget>[
          _scopeCell('家庭', '¥${money(family)}', const Color(0xFF2F6F62)),
          _scopeCell('个人', '¥${money(personal)}', const Color(0xFFD98A2B)),
          _scopeCell('资金往来', '¥${money(transfer)}', const Color(0xFF7A8B99)),
        ],
      ),
    );
  }

  Widget _scopeCell(String label, String value, Color color) {
    return Expanded(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(label, style: TextStyle(fontSize: 11, color: Colors.grey.shade500)),
          const SizedBox(height: 2),
          Text(value,
              style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: color)),
        ],
      ),
    );
  }
}

/// 待确认提醒：点进去批量归类
class _PendingClassifyBanner extends StatelessWidget {
  const _PendingClassifyBanner({required this.store});

  final LedgerStore store;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 12, 20, 0),
      child: Material(
        color: const Color(0xFF2F6F62).withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(12),
        child: InkWell(
          borderRadius: BorderRadius.circular(12),
          onTap: () => Navigator.of(context).push(
            MaterialPageRoute<void>(
              builder: (_) => ClassifyReviewPage(
                api: store.api,
                familyId: store.familyId,
                categories: store.categories,
              ),
            ),
          ),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(14, 12, 12, 12),
            child: Row(
              children: <Widget>[
                const Icon(Icons.auto_awesome, size: 18, color: Color(0xFF2F6F62)),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    '${store.pendingClassify} 笔账单待确认归属',
                    style: const TextStyle(fontSize: 13.5, color: Color(0xFF2F6F62)),
                  ),
                ),
                const Icon(Icons.chevron_right_rounded, size: 18, color: Color(0xFF2F6F62)),
              ],
            ),
          ),
        ),
      ),
    );
  }
}