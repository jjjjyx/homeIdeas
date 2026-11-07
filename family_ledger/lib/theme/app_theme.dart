import 'package:flutter/material.dart';

/// 支出用暖红，收入用墨绿
const Color kExpenseColor = Color(0xFFD64545);
const Color kIncomeColor = Color(0xFF2E9E6B);
const Color kCanvasColor = Color(0xFFF3F5F6);
const Color kCardColor = Colors.white;

ThemeData buildAppTheme() {
  final scheme = ColorScheme.fromSeed(seedColor: const Color(0xFF2F6F62));
  return ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    scaffoldBackgroundColor: kCanvasColor,
  );
}

/// 统一卡片外观，避免依赖各主题类的构造函数名
BoxDecoration cardDecoration(BuildContext context, {double radius = 20}) {
  return BoxDecoration(
    color: kCardColor,
    borderRadius: BorderRadius.circular(radius),
    boxShadow: <BoxShadow>[
      BoxShadow(
        color: Colors.black.withValues(alpha: 0.04),
        blurRadius: 12,
        offset: const Offset(0, 4),
      ),
    ],
  );
}
