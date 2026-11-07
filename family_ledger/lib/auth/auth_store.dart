import 'package:flutter/foundation.dart';

import '../data/ledger_store.dart';
import '../data/models.dart';
import '../data/remote_api.dart';
import '../data/token_store.dart';

enum AuthStage {
  /// 启动中，正在读取本地令牌并校验
  loading,

  /// 未登录
  loggedOut,

  /// 已登录但还没有选择/加入家庭
  pickingFamily,

  /// 正常使用
  ready,
}

/// 账号与会话状态：App 的根状态机就是它。
class AuthStore extends ChangeNotifier {
  AuthStore({required this.api, required this.tokens, required this.ledger});

  final RemoteApi api;
  final TokenStore tokens;
  final LedgerStore ledger;

  AuthStage stage = AuthStage.loading;
  AppUser? user;
  List<Family> families = const [];
  Family? currentFamily;
  String? error;
  bool busy = false;

  bool get isLoggedIn => user != null;

  /// 启动时恢复会话：有令牌就拉一次 /me 校验，并进入上次使用的家庭。
  Future<void> bootstrap() async {
    debugPrint("[pair] bootstrap 开始, isLoggedIn=${tokens.isLoggedIn}");
    if (!tokens.isLoggedIn) {
      stage = AuthStage.loggedOut;
      notifyListeners();
      return;
    }
    try {
      final (u, fams) = await api.me();
      user = u;
      families = fams;
      await tokens.saveProfile(nickname: u.nickname, account: u.account);

      final savedId = tokens.familyId;
      Family? target;
      for (final f in fams) {
        if (f.id == savedId) target = f;
      }
      if (target == null && fams.length == 1) target = fams.first;

      if (target != null) {
        await _enter(target);
      } else {
        stage = AuthStage.pickingFamily;
      }
    } catch (e) {
      // 令牌失效或服务不可用：回到配对页
      debugPrint("[pair] bootstrap 失败: $e");
      stage = AuthStage.loggedOut;
    }
    notifyListeners();
  }

  Future<void> login(String account, String password) async {
    busy = true;
    error = null;
    notifyListeners();
    try {
      final session = await api.login(account: account.trim(), password: password);
      await _afterAuth(session);
    } catch (e) {
      error = e is ApiException ? e.message : '$e';
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  Future<void> register({
    required String account,
    required String password,
    required String nickname,
    String inviteCode = '',
  }) async {
    busy = true;
    error = null;
    notifyListeners();
    try {
      final session = await api.register(
        account: account.trim(),
        password: password,
        nickname: nickname.trim(),
        inviteCode: inviteCode.trim().toUpperCase(),
      );
      await _afterAuth(session);
    } catch (e) {
      error = e is ApiException ? e.message : '$e';
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  /// 设备配对批准后写入令牌，并继续走 bootstrap 恢复会话
  Future<void> applyDeviceSession(String access, String refresh) async {
    debugPrint("[pair] 写入令牌 access=${access.length} refresh=${refresh.length}");
    await tokens.saveSession(
      accessToken: access,
      refreshToken: refresh,
      userId: tokens.userId ?? '',
      nickname: '',
      account: '',
    );
    await bootstrap();
  }
  Future<void> _afterAuth(Session session) async {
    user = session.user;
    await tokens.saveSession(
      accessToken: session.accessToken,
      refreshToken: session.refreshToken,
      userId: session.user.id,
      nickname: session.user.nickname,
      account: session.user.account,
    );
    families = await api.families();
    if (families.length == 1) {
      await _enter(families.first);
    } else {
      error = null;
      stage = AuthStage.pickingFamily;
    }
  }

  Future<void> _enter(Family family) async {
    // 切换家庭时必须清空本地缓存：categories/accounts/members 表不带 family_id，
    // 不清会和新家庭的数据混在一起。
    final changing = currentFamily != null && currentFamily!.id != family.id;
    currentFamily = family;
    await tokens.saveFamily(family.id);
    if (changing) {
      await ledger.switchFamily(family.id, user?.id ?? '');
    } else {
      await ledger.configure(family.id, user?.id ?? '');
    }
    stage = AuthStage.ready;
  }

  Future<void> selectFamily(Family family) async {
    busy = true;
    notifyListeners();
    try {
      await _enter(family);
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  Future<bool> createFamily(String name) async {
    busy = true;
    error = null;
    notifyListeners();
    try {
      final family = await api.createFamily(name.trim());
      families = [...families, family];
      await _enter(family);
      return true;
    } catch (e) {
      error = e is ApiException ? e.message : '$e';
      return false;
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  Future<bool> joinFamily(String code) async {
    busy = true;
    error = null;
    notifyListeners();
    try {
      final family = await api.joinFamily(code.trim().toUpperCase());
      final exists = families.any((f) => f.id == family.id);
      if (!exists) families = [...families, family];
      await _enter(family);
      return true;
    } catch (e) {
      error = e is ApiException ? e.message : '$e';
      return false;
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  Future<void> refreshFamilies() async {
    try {
      families = await api.families();
      notifyListeners();
    } catch (_) {
      // 忽略，下次再试
    }
  }

  /// 从设置页回到家庭选择
  void openFamilyPicker() {
    stage = AuthStage.pickingFamily;
    notifyListeners();
  }

  Future<void> logout() async {
    final refresh = tokens.refreshToken ?? '';
    if (refresh.isNotEmpty) {
      await api.logout(refresh);
    }
    await tokens.clear();
    await ledger.db.clearSyncedData();
    user = null;
    families = const [];
    currentFamily = null;
    stage = AuthStage.loggedOut;
    notifyListeners();
  }
}
