import 'package:family_ledger/core/money.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('金额格式化', () {
    test('保留两位小数并加千分位', () {
      expect(formatMoney(123456), '1,234.56');
      expect(formatMoney(5), '0.05');
      expect(formatMoney(0), '0.00');
    });

    test('去掉无意义的小数尾巴', () {
      expect(formatMoneyTrimmed(120000), '1,200');
      expect(formatMoneyTrimmed(125000), '1,250');
      expect(formatMoneyTrimmed(125050), '1,250.5');
      expect(formatMoneyTrimmed(123456), '1,234.56');
    });

    test('收支符号', () {
      expect(formatSignedMoney(125000, isIncome: true), '+1,250.00');
      expect(formatSignedMoney(125000, isIncome: false), '-1,250.00');
    });
  });

  group('金额解析', () {
    test('常见输入', () {
      expect(parseAmountToCents('12.5'), 1250);
      expect(parseAmountToCents('12.50'), 1250);
      expect(parseAmountToCents('1,234.56'), 123456);
      expect(parseAmountToCents('¥ 8'), 800);
    });

    test('非法输入返回 null', () {
      expect(parseAmountToCents(''), isNull);
      expect(parseAmountToCents('abc'), isNull);
      expect(parseAmountToCents('0'), isNull);
      expect(parseAmountToCents('-5'), isNull);
    });
  });

  group('日期工具', () {
    test('月份加减跨年', () {
      expect(addMonths(DateTime(2026, 12), 1), DateTime(2027, 1));
      expect(addMonths(DateTime(2026, 1), -1), DateTime(2025, 12));
    });

    test('同月判断', () {
      expect(
        isSameMonth(DateTime(2026, 11, 1), DateTime(2026, 11, 30)),
        isTrue,
      );
      expect(
        isSameMonth(DateTime(2026, 11, 1), DateTime(2026, 12, 1)),
        isFalse,
      );
    });

    test('今天 / 昨天', () {
      final now = DateTime(2026, 11, 1, 20);
      expect(dayLabel(DateTime(2026, 11, 1), now: now), '今天');
      expect(dayLabel(DateTime(2026, 10, 31), now: now), '昨天');
      expect(dayLabel(DateTime(2026, 10, 20), now: now), '10月20日 周二');
    });
  });
}
