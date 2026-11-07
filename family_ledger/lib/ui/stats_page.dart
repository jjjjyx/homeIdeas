import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../core/money.dart';
import '../data/ledger_store.dart';
import '../data/models.dart';
import '../theme/app_theme.dart';
import 'widgets/common.dart';

class StatsPage extends StatefulWidget {
  const StatsPage({super.key, required this.store});

  final LedgerStore store;

  @override
  State<StatsPage> createState() => _StatsPageState();
}

class _StatsPageState extends State<StatsPage> {
  EntryType _type = EntryType.expense;

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      bottom: false,
      child: ListenableBuilder(
        listenable: widget.store,
        builder: (context, _) {
          final store = widget.store;
          final slices = store.categoryBreakdown(store.selectedMonth, _type);
          final total = slices.fold<int>(0, (sum, s) => sum + s.amountCents);
          final buckets = store.trend(6);

          return ListView(
            padding: const EdgeInsets.fromLTRB(0, 18, 0, 150),
            children: <Widget>[
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 20),
                child: Text(
                  '统计',
                  style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700),
                ),
              ),
              const SizedBox(height: 4),
              MonthSelector(store: store),
              Center(
                child: SegmentedButton<EntryType>(
                  segments: const <ButtonSegment<EntryType>>[
                    ButtonSegment<EntryType>(
                      value: EntryType.expense,
                      label: Text('支出'),
                    ),
                    ButtonSegment<EntryType>(
                      value: EntryType.income,
                      label: Text('收入'),
                    ),
                  ],
                  selected: <EntryType>{_type},
                  onSelectionChanged: (selection) =>
                      setState(() => _type = selection.first),
                ),
              ),
              const SizedBox(height: 16),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: Container(
                  padding: const EdgeInsets.all(18),
                  decoration: cardDecoration(context),
                  child: Column(
                    children: <Widget>[
                      Align(
                        alignment: Alignment.centerLeft,
                        child: Text(
                          '分类占比',
                          style: const TextStyle(
                            fontSize: 14,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                      const SizedBox(height: 14),
                      _BreakdownBody(
                        slices: slices,
                        totalCents: total,
                        type: _type,
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 14),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: Container(
                  padding: const EdgeInsets.all(18),
                  decoration: cardDecoration(context),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      const Text(
                        '近 6 个月趋势',
                        style: TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(height: 6),
                      Row(
                        children: <Widget>[
                          // 趋势图跟随上面的口径切换，避免收入/支出共用刻度导致柱子看不出来
                          _LegendDot(
                            color: _type.isIncome
                                ? kIncomeColor
                                : kExpenseColor,
                            label: _type.label,
                          ),
                        ],
                      ),
                      const SizedBox(height: 12),
                      TrendChart(buckets: buckets, type: _type),
                    ],
                  ),
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}

class _BreakdownBody extends StatelessWidget {
  const _BreakdownBody({
    required this.slices,
    required this.totalCents,
    required this.type,
  });

  final List<CategorySlice> slices;
  final int totalCents;
  final EntryType type;

  @override
  Widget build(BuildContext context) {
    if (slices.isEmpty) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 28),
        child: Column(
          children: <Widget>[
            Icon(
              Icons.donut_large_rounded,
              size: 44,
              color: Colors.grey.shade300,
            ),
            const SizedBox(height: 8),
            Text(
              '本月还没有${type.label}记录',
              style: TextStyle(fontSize: 13, color: Colors.grey.shade500),
            ),
          ],
        ),
      );
    }
    return Column(
      children: <Widget>[
        DonutChart(slices: slices, totalCents: totalCents),
        const SizedBox(height: 18),
        for (final slice in slices) _SliceRow(slice: slice, type: type),
      ],
    );
  }
}

class _SliceRow extends StatelessWidget {
  const _SliceRow({required this.slice, required this.type});

  final CategorySlice slice;
  final EntryType type;

  @override
  Widget build(BuildContext context) {
    final color = Color(slice.category.colorValue);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5),
      child: Row(
        children: <Widget>[
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(color: color, shape: BoxShape.circle),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              slice.category.name,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontSize: 13),
            ),
          ),
          Text(
            '${(slice.ratio * 100).toStringAsFixed(1)}%',
            style: TextStyle(fontSize: 12, color: Colors.grey.shade500),
          ),
          const SizedBox(width: 12),
          Text(
            '¥${formatMoney(slice.amountCents)}',
            style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
          ),
        ],
      ),
    );
  }
}

/// 环形图（自绘，不依赖第三方图表库）
class DonutChart extends StatelessWidget {
  const DonutChart({super.key, required this.slices, required this.totalCents});

  final List<CategorySlice> slices;
  final int totalCents;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 168,
      height: 168,
      child: Stack(
        alignment: Alignment.center,
        children: <Widget>[
          CustomPaint(
            size: const Size(168, 168),
            painter: _DonutPainter(slices: slices),
          ),
          Column(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Text(
                '合计',
                style: TextStyle(fontSize: 11, color: Colors.grey.shade500),
              ),
              const SizedBox(height: 2),
              Text(
                '¥${formatCompactMoney(totalCents)}',
                style: const TextStyle(
                  fontSize: 20,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _DonutPainter extends CustomPainter {
  _DonutPainter({required this.slices});

  final List<CategorySlice> slices;

  @override
  void paint(Canvas canvas, Size size) {
    final center = Offset(size.width / 2, size.height / 2);
    final radius = math.min(size.width, size.height) / 2 - 8;
    final rect = Rect.fromCircle(center: center, radius: radius);
    const stroke = 22.0;

    final paint = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = stroke
      ..strokeCap = StrokeCap.butt;

    // 底环
    canvas.drawArc(
      rect,
      0,
      math.pi * 2,
      false,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = stroke
        ..color = const Color(0xFFEDF1F2),
    );

    var start = -math.pi / 2;
    const gap = 0.03;
    for (final slice in slices) {
      final sweep = slice.ratio * math.pi * 2;
      if (sweep <= 0) continue;
      paint.color = Color(slice.category.colorValue);
      final drawSweep = math.max(sweep - gap, 0.01);
      canvas.drawArc(rect, start, drawSweep, false, paint);
      start += sweep;
    }
  }

  @override
  bool shouldRepaint(covariant _DonutPainter oldDelegate) =>
      oldDelegate.slices.length != slices.length ||
      oldDelegate.slices.fold<int>(0, (s, e) => s + e.amountCents) !=
          slices.fold<int>(0, (s, e) => s + e.amountCents);
}

class _LegendDot extends StatelessWidget {
  const _LegendDot({required this.color, required this.label});

  final Color color;
  final String label;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Container(
          width: 8,
          height: 8,
          decoration: BoxDecoration(color: color, shape: BoxShape.circle),
        ),
        const SizedBox(width: 4),
        Text(
          label,
          style: TextStyle(fontSize: 11, color: Colors.grey.shade600),
        ),
      ],
    );
  }
}

/// 单口径柱状趋势图（自绘，不依赖第三方图表库）
class TrendChart extends StatelessWidget {
  const TrendChart({super.key, required this.buckets, required this.type});

  final List<MonthBucket> buckets;

  /// 只画当前口径（支出或收入），避免两个量级共用刻度时柱子被压平
  final EntryType type;

  static const double _barAreaHeight = 116;
  static const double _barWidth = 14;

  int _valueOf(MonthBucket bucket) =>
      type.isIncome ? bucket.incomeCents : bucket.expenseCents;

  @override
  Widget build(BuildContext context) {
    var maxValue = 0;
    for (final bucket in buckets) {
      maxValue = math.max(maxValue, _valueOf(bucket));
    }
    final hasData = maxValue > 0;
    final scale = hasData ? maxValue : 1;

    double heightOf(int cents) {
      if (cents <= 0) return 2;
      return math.max(cents / scale * _barAreaHeight, 4);
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Align(
          alignment: Alignment.centerRight,
          child: Text(
            hasData ? '峰值 ¥${formatCompactMoney(maxValue)}' : '暂无数据',
            style: TextStyle(fontSize: 11, color: Colors.grey.shade400),
          ),
        ),
        const SizedBox(height: 6),
        Container(
          height: _barAreaHeight,
          decoration: const BoxDecoration(
            border: Border(bottom: BorderSide(color: Color(0xFFE8EDEE))),
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: <Widget>[
              for (final bucket in buckets)
                Expanded(
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.end,
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: <Widget>[
                      _Bar(
                        width: _barWidth,
                        height: heightOf(_valueOf(bucket)),
                        color: type.isIncome ? kIncomeColor : kExpenseColor,
                        empty: _valueOf(bucket) <= 0,
                      ),
                    ],
                  ),
                ),
            ],
          ),
        ),
        const SizedBox(height: 8),
        Row(
          children: <Widget>[
            for (final bucket in buckets)
              Expanded(
                child: Text(
                  shortMonthLabel(bucket.month),
                  textAlign: TextAlign.center,
                  style: TextStyle(fontSize: 10, color: Colors.grey.shade500),
                ),
              ),
          ],
        ),
      ],
    );
  }
}

class _Bar extends StatelessWidget {
  const _Bar({
    required this.width,
    required this.height,
    required this.color,
    required this.empty,
  });

  final double width;
  final double height;
  final Color color;
  final bool empty;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: width,
      height: height,
      decoration: BoxDecoration(
        color: color.withValues(alpha: empty ? 0.18 : 0.9),
        borderRadius: const BorderRadius.vertical(top: Radius.circular(4)),
      ),
    );
  }
}
