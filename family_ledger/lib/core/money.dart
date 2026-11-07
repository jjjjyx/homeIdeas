import 'package:intl/intl.dart';

/// 金额统一用「分」存整数，避免浮点误差。
final NumberFormat _moneyFormat = NumberFormat('#,##0.00', 'en_US');
final NumberFormat _compactFormat = NumberFormat('#,##0', 'en_US');

/// 1234567 -> "12,345.67"
String formatMoney(int cents) => _moneyFormat.format(cents / 100);

/// 去掉无意义的小数尾巴：1200 -> "12"，1250 -> "12.5"，1234 -> "12.34"
String formatMoneyTrimmed(int cents) {
  final text = formatMoney(cents);
  if (text.endsWith('.00')) return text.substring(0, text.length - 3);
  if (text.endsWith('0')) return text.substring(0, text.length - 1);
  return text;
}

/// -123400 + isIncome=false -> "-1,234.00"
String formatSignedMoney(int cents, {required bool isIncome}) {
  final sign = isIncome ? '+' : '-';
  return '$sign${formatMoney(cents.abs())}';
}

String formatCompactMoney(int cents) => _compactFormat.format(cents / 100);

/// "12.5" / "12.50" / "1,234.56" -> 1250 / 1250 / 123456；非法输入返回 null
int? parseAmountToCents(String raw) {
  final text = raw.replaceAll(',', '').replaceAll('¥', '').trim();
  if (text.isEmpty) return null;
  final value = double.tryParse(text);
  if (value == null || value.isNaN || value.isInfinite || value <= 0) {
    return null;
  }
  return (value * 100).round();
}

DateTime monthStart(DateTime date) => DateTime(date.year, date.month);

DateTime addMonths(DateTime month, int delta) =>
    DateTime(month.year, month.month + delta);

bool isSameMonth(DateTime a, DateTime b) =>
    a.year == b.year && a.month == b.month;

bool isSameDay(DateTime a, DateTime b) =>
    a.year == b.year && a.month == b.month && a.day == b.day;

String monthLabel(DateTime month) => '${month.year}年${month.month}月';

const List<String> _weekdays = ['一', '二', '三', '四', '五', '六', '日'];

/// 今天 / 昨天 / 10月28日 周二
String dayLabel(DateTime day, {DateTime? now}) {
  final today = now ?? DateTime.now();
  if (isSameDay(day, today)) return '今天';
  if (isSameDay(day, today.subtract(const Duration(days: 1)))) return '昨天';
  return '${day.month}月${day.day}日 周${_weekdays[day.weekday - 1]}';
}

/// 用于图表横轴：9月 / 10月
String shortMonthLabel(DateTime month) => '${month.month}月';
