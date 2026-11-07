import 'package:flutter/material.dart';

/// 分类图标：用固定常量表 + key 引用，保证 release 构建裁剪图标字体后依然能显示。
const Map<String, IconData> categoryIcons = <String, IconData>{
  'restaurant': Icons.restaurant,
  'bus': Icons.directions_bus,
  'shopping': Icons.shopping_bag,
  'home': Icons.home_rounded,
  'hospital': Icons.local_hospital_rounded,
  'school': Icons.school_rounded,
  'game': Icons.sports_esports,
  'gift': Icons.redeem,
  'pet': Icons.pets,
  'travel': Icons.flight_takeoff,
  'other': Icons.category_rounded,
  'salary': Icons.payments_rounded,
  'card': Icons.card_giftcard,
  'invest': Icons.trending_up_rounded,
  'savings': Icons.savings_rounded,
  'wallet': Icons.account_balance_wallet_rounded,
};

/// 新建分类时可选图标顺序
const List<String> categoryIconKeys = <String>[
  'restaurant',
  'bus',
  'shopping',
  'home',
  'hospital',
  'school',
  'game',
  'gift',
  'pet',
  'travel',
  'salary',
  'card',
  'invest',
  'savings',
  'wallet',
  'other',
];

/// 新建分类时可选颜色
const List<int> categoryColorValues = <int>[
  0xFFEF5350,
  0xFFEC407A,
  0xFFAB47BC,
  0xFF7E57C2,
  0xFF5C6BC0,
  0xFF42A5F5,
  0xFF29B6F6,
  0xFF26C6DA,
  0xFF26A69A,
  0xFF66BB6A,
  0xFF9CCC65,
  0xFFFFA726,
  0xFFFF7043,
  0xFF8D6E63,
  0xFF78909C,
];

IconData iconFor(String key) => categoryIcons[key] ?? Icons.category_rounded;
