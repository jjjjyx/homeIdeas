/// 记账类型：支出 / 收入
enum EntryType {
  expense(0, '支出'),
  income(1, '收入');

  const EntryType(this.dbValue, this.label);

  final int dbValue;
  final String label;

  static EntryType fromDb(int value) =>
      value == 1 ? EntryType.income : EntryType.expense;

  bool get isIncome => this == EntryType.income;
}

/// 账目可见性：家庭共享 / 仅自己可见
enum EntryVisibility {
  family('family', '家庭可见'),
  private('private', '仅自己');

  const EntryVisibility(this.dbValue, this.label);

  final String dbValue;
  final String label;

  static EntryVisibility fromDb(String? value) =>
      value == EntryVisibility.private.dbValue ? EntryVisibility.private : EntryVisibility.family;
}

class AppUser {
  const AppUser({
    required this.id,
    required this.account,
    required this.nickname,
    this.isPlatformAdmin = false,
  });

  final String id;
  final String account;
  final String nickname;
  final bool isPlatformAdmin;

  factory AppUser.fromJson(Map<String, dynamic> json) => AppUser(
        id: json['id'] as String,
        account: json['account'] as String,
        nickname: (json['nickname'] as String?) ?? json['account'] as String,
        isPlatformAdmin: json['is_platform_admin'] == true,
      );
}

class Family {
  const Family({
    required this.id,
    required this.name,
    required this.myRole,
    required this.memberCount,
    required this.rev,
  });

  final String id;
  final String name;
  final String myRole;
  final int memberCount;
  final int rev;

  bool get canManage => myRole == 'owner' || myRole == 'admin';

  factory Family.fromJson(Map<String, dynamic> json) => Family(
        id: json['id'] as String,
        name: json['name'] as String,
        myRole: (json['my_role'] as String?) ?? 'member',
        memberCount: (json['member_count'] as int?) ?? 1,
        rev: (json['rev'] as num?)?.toInt() ?? 0,
      );
}

class MemberInfo {
  const MemberInfo({
    required this.userId,
    required this.nickname,
    required this.account,
    required this.role,
    required this.joinedAt,
  });

  final String userId;
  final String nickname;
  final String account;
  final String role;
  final int joinedAt;

  factory MemberInfo.fromJson(Map<String, dynamic> json) => MemberInfo(
        userId: json['user_id'] as String,
        nickname: (json['nickname'] as String?) ?? '',
        account: (json['account'] as String?) ?? '',
        role: (json['role'] as String?) ?? 'member',
        joinedAt: (json['joined_at'] as num?)?.toInt() ?? 0,
      );
}

class Category {
  const Category({
    required this.id,
    required this.name,
    required this.type,
    required this.iconKey,
    required this.colorValue,
    required this.sortOrder,
    required this.rev,
  });

  final String id;
  final String name;
  final EntryType type;
  final String iconKey;
  final int colorValue;
  final int sortOrder;
  final int rev;

  factory Category.fromMap(Map<String, Object?> map) => Category(
        id: map['id']! as String,
        name: map['name']! as String,
        type: EntryType.fromDb(map['type']! as int),
        iconKey: (map['icon_key'] as String?) ?? 'other',
        colorValue: (map['color'] as int?) ?? 0xFF78909C,
        sortOrder: (map['sort_order'] as int?) ?? 0,
        rev: (map['rev'] as int?) ?? 0,
      );
}

class Account {
  const Account({
    required this.id,
    required this.name,
    required this.kind,
    required this.sortOrder,
  });

  final String id;
  final String name;
  final String kind;
  final int sortOrder;

  factory Account.fromMap(Map<String, Object?> map) => Account(
        id: map['id']! as String,
        name: map['name']! as String,
        kind: (map['kind'] as String?) ?? 'other',
        sortOrder: (map['sort_order'] as int?) ?? 0,
      );
}

/// 一笔账（与服务端结构一致）
class Transaction {
  const Transaction({
    required this.id,
    required this.type,
    required this.amountCents,
    required this.categoryId,
    required this.memberId,
    required this.occurredAt,
    this.accountId = '',
    this.note = '',
    this.visibility = EntryVisibility.family,
    this.tags = '',
    this.rev = 0,
    this.createdAt = 0,
    this.updatedAt = 0,
  });

  final String id;
  final EntryType type;
  final int amountCents;
  final String categoryId;
  final String accountId;
  final String memberId;
  final DateTime occurredAt;
  final String note;
  final EntryVisibility visibility;
  final String tags;

  /// 服务端版本号，0 表示本地新建、尚未同步
  final int rev;
  final int createdAt;
  final int updatedAt;

  int get signedCents => type.isIncome ? amountCents : -amountCents;

  Transaction copyWith({
    EntryType? type,
    int? amountCents,
    String? categoryId,
    String? accountId,
    DateTime? occurredAt,
    String? note,
    EntryVisibility? visibility,
    String? tags,
    int? rev,
    int? updatedAt,
  }) =>
      Transaction(
        id: id,
        type: type ?? this.type,
        amountCents: amountCents ?? this.amountCents,
        categoryId: categoryId ?? this.categoryId,
        accountId: accountId ?? this.accountId,
        memberId: memberId,
        occurredAt: occurredAt ?? this.occurredAt,
        note: note ?? this.note,
        visibility: visibility ?? this.visibility,
        tags: tags ?? this.tags,
        rev: rev ?? this.rev,
        createdAt: createdAt,
        updatedAt: updatedAt ?? this.updatedAt,
      );

  factory Transaction.fromMap(Map<String, Object?> map) => Transaction(
        id: map['id']! as String,
        type: EntryType.fromDb(map['type']! as int),
        amountCents: map['amount_cents']! as int,
        categoryId: (map['category_id'] as String?) ?? '',
        accountId: (map['account_id'] as String?) ?? '',
        memberId: (map['member_id'] as String?) ?? '',
        occurredAt: DateTime.fromMillisecondsSinceEpoch(map['occurred_at']! as int),
        note: (map['note'] as String?) ?? '',
        visibility: EntryVisibility.fromDb(map['visibility'] as String?),
        tags: (map['tags'] as String?) ?? '',
        rev: (map['rev'] as int?) ?? 0,
        createdAt: (map['created_at'] as int?) ?? 0,
        updatedAt: (map['updated_at'] as int?) ?? 0,
      );

  Map<String, Object?> toMap() => {
        'id': id,
        'type': type.dbValue,
        'amount_cents': amountCents,
        'category_id': categoryId,
        'account_id': accountId,
        'member_id': memberId,
        'occurred_at': occurredAt.millisecondsSinceEpoch,
        'note': note,
        'visibility': visibility.dbValue,
        'tags': tags,
        'rev': rev,
        'created_at': createdAt,
        'updated_at': updatedAt,
      };

  /// 同步推送时的载荷（不含服务端字段）
  Map<String, Object?> toPayload() => {
        'id': id,
        'type': type.dbValue,
        'amount_cents': amountCents,
        'category_id': categoryId,
        if (accountId.isNotEmpty) 'account_id': accountId,
        'occurred_at': occurredAt.millisecondsSinceEpoch,
        'note': note,
        'visibility': visibility.dbValue,
        if (tags.isNotEmpty) 'tags': tags,
      };
}
