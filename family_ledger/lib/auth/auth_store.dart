import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import '../data/ledger_store.dart';
import '../data/models.dart';
import '../data/remote_api.dart';
import '../data/token_store.dart';

enum AuthStage {
  /// 启动中，正在读取本地令牌并校验
  loading,

  /// 未登录（或系统尚未初始化）
  loggedOut,

  /// 正常使用
  ready,
}

/// 账号与会话状态：App 的根状态机就是它。
///
/// 单家庭系统：登录后必然进入唯一家庭，不再有选择/创建/加入家庭的流程。
class AuthStore extends ChangeNotifier {
  AuthStore({required this.api, required this.tokens, required this.ledger});

  final RemoteApi api;
  final TokenStore tokens;
  final LedgerStore ledger;

  AuthStage stage = AuthStage.loading;
  AppUser? user;
  Family? currentFamily;
  String? error;
  bool busy = false;

  static const MethodChannel _channel = MethodChannel('family_ledger/share');

  bool get isLoggedIn => user != null;

  /// 启动时恢复会话：有令牌就拉一次 /me 校验，并进入唯一家庭。
  Future<void> bootstrap() async {
    debugPrint("[pair] bootstrap 开始, isLoggedIn=${tokens.isLoggedIn}");
    if (!tokens.isLoggedIn) {
      // 设备授权是永久的：本机标识被家人批准过，就直接换令牌，不用再配对
      if (await _tryDeviceLogin()) return;
      stage = AuthStage.loggedOut;
      notifyListeners();
      return;
    }
    try {
      final (u, family) = await api.me();
      user = u;
      await tokens.saveProfile(nickname: u.nickname, account: u.account);

      if (family != null) {
        await _enter(family);
      } else {
        error = '系统尚未初始化，请先在管理后台完成初始化';
        stage = AuthStage.loggedOut;
      }
    } catch (e) {
      // 令牌失效或服务不可用：回到配对页
      debugPrint("[pair] bootstrap 失败: $e");
      stage = AuthStage.loggedOut;
    }
    notifyListeners();
  }

  /// 用本机设备标识直接换令牌（授权是永久的），成功后等价于正常登录
  Future<bool> _tryDeviceLogin() async {
    final key = await _deviceKey();
    if (key.isEmpty) return false;
    try {
      final session = await api.deviceLogin(key);
      user = session.user;
      await tokens.saveSession(
        accessToken: session.accessToken,
        refreshToken: session.refreshToken,
        userId: session.user.id,
        nickname: session.user.nickname,
        account: session.user.account,
      );
      final (u, family) = await api.me();
      user = u;
      await tokens.saveProfile(nickname: u.nickname, account: u.account);
      if (family == null) {
        stage = AuthStage.loggedOut;
        notifyListeners();
        return false;
      }
      await _enter(family);
      notifyListeners();
      return true;
    } catch (e) {
      debugPrint('[pair] 设备自动登录未通过: $e');
      return false;
    }
  }

  /// 读取本机设备标识（ANDROID_ID，重装不变）
  Future<String> _deviceKey() async {
    try {
      final v = await _channel.invokeMethod<String>('getDeviceId');
      return v ?? '';
    } catch (_) {
      return '';
    }
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

  /// 用邀请码注册并加入唯一家庭（邀请码是成员加入的主要方式）。
  Future<void> register({
    required String account,
    required String password,
    required String nickname,
    required String inviteCode,
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
    final (_, family) = await api.me();
    if (family == null) {
      error = '系统尚未初始化，请先在管理后台完成初始化';
      stage = AuthStage.loggedOut;
      return;
    }
    await _enter(family);
  }

  Future<void> _enter(Family family) async {
    currentFamily = family;
    await ledger.configure(family.id, user?.id ?? '');
    error = null;
    stage = AuthStage.ready;
  }

  Future<void> logout() async {
    final refresh = tokens.refreshToken ?? '';
    if (refresh.isNotEmpty) {
      await api.logout(refresh);
    }
    await tokens.clear();
    await ledger.db.clearSyncedData();
    user = null;
    currentFamily = null;
    stage = AuthStage.loggedOut;
    notifyListeners();
  }
}
