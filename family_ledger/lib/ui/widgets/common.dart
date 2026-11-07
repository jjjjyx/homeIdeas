import 'package:flutter/material.dart';

import '../../core/money.dart';
import '../../data/category_icons.dart';
import '../../data/ledger_store.dart';
import '../../data/models.dart';
import '../../theme/app_theme.dart';

class MonthSelector extends StatelessWidget {
  const MonthSelector({super.key, required this.store});

  final LedgerStore store;

  @override
  Widget build(BuildContext context) {
    final now = DateTime.now();
    final isCurrent = isSameMonth(store.selectedMonth, now);
    return Row(
      mainAxisAlignment: MainAxisAlignment.center,
      children: <Widget>[
        IconButton(
          tooltip: '上个月',
          onPressed: () => store.shiftMonth(-1),
          icon: const Icon(Icons.chevron_left_rounded),
        ),
        Text(
          monthLabel(store.selectedMonth),
          style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
        ),
        IconButton(
          tooltip: '下个月',
          onPressed: () => store.shiftMonth(1),
          icon: const Icon(Icons.chevron_right_rounded),
        ),
        if (!isCurrent)
          TextButton(
            onPressed: () => store.selectMonth(now),
            child: const Text('回本月'),
          ),
      ],
    );
  }
}

/// 本月支出 / 收入 / 结余 概览
class SummaryCard extends StatelessWidget {
  const SummaryCard({super.key, required this.summary});

  final MonthSummary summary;

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.fromLTRB(16, 4, 16, 8),
      padding: const EdgeInsets.fromLTRB(20, 18, 20, 18),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(24),
        gradient: const LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: <Color>[Color(0xFF33796B), Color(0xFF1E4B44)],
        ),
        boxShadow: <BoxShadow>[
          BoxShadow(
            color: const Color(0xFF1E4B44).withValues(alpha: 0.28),
            blurRadius: 18,
            offset: const Offset(0, 8),
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              Text(
                '本月支出',
                style: TextStyle(
                  fontSize: 13,
                  color: Colors.white.withValues(alpha: 0.72),
                ),
              ),
              const Spacer(),
              Icon(
                Icons.account_balance_wallet_outlined,
                size: 18,
                color: Colors.white.withValues(alpha: 0.72),
              ),
            ],
          ),
          const SizedBox(height: 6),
          Row(
            crossAxisAlignment: CrossAxisAlignment.baseline,
            textBaseline: TextBaseline.alphabetic,
            children: <Widget>[
              const Text(
                '¥ ',
                style: TextStyle(
                  fontSize: 18,
                  color: Colors.white,
                  fontWeight: FontWeight.w600,
                ),
              ),
              Flexible(
                child: Text(
                  formatMoney(summary.expenseCents),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 34,
                    height: 1.1,
                    color: Colors.white,
                    fontWeight: FontWeight.w700,
                    letterSpacing: 0.5,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 16),
          Divider(color: Colors.white.withValues(alpha: 0.16), height: 1),
          const SizedBox(height: 12),
          Row(
            children: <Widget>[
              _SummaryItem(label: '收入', cents: summary.incomeCents),
              Container(
                width: 1,
                height: 28,
                color: Colors.white.withValues(alpha: 0.16),
                margin: const EdgeInsets.symmetric(horizontal: 16),
              ),
              _SummaryItem(label: '结余', cents: summary.balanceCents),
            ],
          ),
        ],
      ),
    );
  }
}

class _SummaryItem extends StatelessWidget {
  const _SummaryItem({required this.label, required this.cents});

  final String label;
  final int cents;

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(
            label,
            style: TextStyle(
              fontSize: 12,
              color: Colors.white.withValues(alpha: 0.68),
            ),
          ),
          const SizedBox(height: 2),
          Text(
            // 负数写成 -¥284.30，而不是 ¥ -284.30
            '${cents < 0 ? '-' : ''}¥ ${formatMoney(cents.abs())}',
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: const TextStyle(
              fontSize: 15,
              color: Colors.white,
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
    );
  }
}

/// 一天的账单分组头
class DayHeader extends StatelessWidget {
  const DayHeader({
    super.key,
    required this.day,
    required this.incomeCents,
    required this.expenseCents,
  });

  final DateTime day;
  final int incomeCents;
  final int expenseCents;

  @override
  Widget build(BuildContext context) {
    final parts = <String>[];
    if (incomeCents > 0) parts.add('收入 ${formatMoneyTrimmed(incomeCents)}');
    if (expenseCents > 0) parts.add('支出 ${formatMoneyTrimmed(expenseCents)}');
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 16, 20, 6),
      child: Row(
        children: <Widget>[
          Text(
            dayLabel(day),
            style: TextStyle(
              fontSize: 13,
              fontWeight: FontWeight.w600,
              color: Colors.grey.shade700,
            ),
          ),
          const Spacer(),
          Text(
            parts.join(' · '),
            style: TextStyle(fontSize: 12, color: Colors.grey.shade500),
          ),
        ],
      ),
    );
  }
}

/// 单条账单
class EntryTile extends StatelessWidget {
  const EntryTile({
    super.key,
    required this.entry,
    required this.category,
    required this.memberName,
    required this.onTap,
    required this.onLongPress,
  });

  final Transaction entry;
  final Category? category;
  final String memberName;
  final VoidCallback onTap;
  final VoidCallback onLongPress;

  @override
  Widget build(BuildContext context) {
    final color = Color(category?.colorValue ?? 0xFF78909C);
    final name = category?.name ?? '已删除分类';
    final subtitle = <String>[
      if (entry.note.isNotEmpty) entry.note,
      if (memberName.isNotEmpty) memberName,
      if (entry.visibility == EntryVisibility.private) '仅自己',
    ].join(' · ');

    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
      child: Material(
        color: kCardColor,
        borderRadius: BorderRadius.circular(16),
        child: InkWell(
          borderRadius: BorderRadius.circular(16),
          onTap: onTap,
          onLongPress: onLongPress,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
            child: Row(
              children: <Widget>[
                Container(
                  width: 40,
                  height: 40,
                  decoration: BoxDecoration(
                    color: color.withValues(alpha: 0.14),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Icon(iconFor(category?.iconKey ?? 'other'), size: 20, color: color),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(
                        name,
                        style: const TextStyle(
                          fontSize: 15,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      if (subtitle.isNotEmpty) ...<Widget>[
                        const SizedBox(height: 2),
                        Text(
                          subtitle,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontSize: 12,
                            color: Colors.grey.shade500,
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
                const SizedBox(width: 8),
                Column(
                  crossAxisAlignment: CrossAxisAlignment.end,
                  children: <Widget>[
                    Text(
                      formatSignedMoney(entry.amountCents, isIncome: entry.type.isIncome),
                      style: TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w600,
                        color: entry.type.isIncome ? kIncomeColor : kExpenseColor,
                      ),
                    ),
                    if (entry.rev == 0)
                      Text(
                        '待同步',
                        style: TextStyle(fontSize: 10, color: Colors.orange.shade400),
                      ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
