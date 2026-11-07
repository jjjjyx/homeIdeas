import 'package:shared_preferences/shared_preferences.dart';

/// 令牌与当前家庭的本地存储。
///
/// 说明：MVP 阶段用 SharedPreferences；后续可换成 flutter_secure_storage。
class TokenStore {
  TokenStore(this._prefs);

  final SharedPreferences _prefs;

  static const _kAccess = 'auth.access_token';
  static const _kRefresh = 'auth.refresh_token';
  static const _kUserId = 'auth.user_id';
  static const _kNickname = 'auth.nickname';
  static const _kAccount = 'auth.account';
  static const _kServer = 'app.server_url';

  static Future<TokenStore> load() async =>
      TokenStore(await SharedPreferences.getInstance());

  String? get accessToken => _prefs.getString(_kAccess);
  String? get refreshToken => _prefs.getString(_kRefresh);
  String? get userId => _prefs.getString(_kUserId);
  String? get nickname => _prefs.getString(_kNickname);
  String? get account => _prefs.getString(_kAccount);

  /// 用户自定义的服务器地址（空表示用默认值）
  String? get serverUrl => _prefs.getString(_kServer);

  Future<void> saveServerUrl(String url) async {
    if (url.trim().isEmpty) {
      await _prefs.remove(_kServer);
    } else {
      await _prefs.setString(_kServer, url.trim());
    }
  }

  /// 设备唯一标识：配对时用，只在本机生成一次
  static const _kDeviceUid = 'device_uid';

  String get deviceUid {
    final existing = _prefs.getString(_kDeviceUid);
    if (existing != null && existing.isNotEmpty) return existing;
    final generated =
        '${DateTime.now().microsecondsSinceEpoch.toRadixString(16)}-${DateTime.now().hashCode.toRadixString(16)}';
    _prefs.setString(_kDeviceUid, generated);
    return generated;
  }

  bool get isLoggedIn => (accessToken ?? '').isNotEmpty;

  Future<void> saveSession({
    required String accessToken,
    required String refreshToken,
    required String userId,
    required String nickname,
    required String account,
  }) async {
    await _prefs.setString(_kAccess, accessToken);
    await _prefs.setString(_kRefresh, refreshToken);
    await _prefs.setString(_kUserId, userId);
    await _prefs.setString(_kNickname, nickname);
    await _prefs.setString(_kAccount, account);
  }

  Future<void> saveProfile({required String nickname, required String account}) async {
    await _prefs.setString(_kNickname, nickname);
    await _prefs.setString(_kAccount, account);
  }

  Future<void> clear() async {
    await _prefs.remove(_kAccess);
    await _prefs.remove(_kRefresh);
    await _prefs.remove(_kUserId);
    await _prefs.remove(_kNickname);
    await _prefs.remove(_kAccount);
  }
}
