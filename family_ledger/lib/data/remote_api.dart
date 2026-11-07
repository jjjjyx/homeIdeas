import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart' show debugPrint;

import 'models.dart';
import 'token_store.dart';

class ApiException implements Exception {
  ApiException(this.message, {this.code = 0, this.status = 0});

  final String message;
  final int code;
  final int status;

  @override
  String toString() => message;
}

class Session {
  const Session({
    required this.accessToken,
    required this.refreshToken,
    required this.user,
  });

  final String accessToken;
  final String refreshToken;
  final AppUser user;
}

/// 后端 REST 客户端。统一处理 {code,message,data} 信封与 401 自动刷新。
class RemoteApi {
  RemoteApi({required String baseUrl, required this.tokens}) : _baseUrl = baseUrl {
    _dio = Dio(
      BaseOptions(
        baseUrl: baseUrl,
        connectTimeout: const Duration(seconds: 8),
        receiveTimeout: const Duration(seconds: 20),
        contentType: 'application/json; charset=utf-8',
        responseType: ResponseType.json,
      ),
    );
    _dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) {
          final token = tokens.accessToken;
          if (token != null && token.isNotEmpty) {
            options.headers['Authorization'] = 'Bearer $token';
          }
          handler.next(options);
        },
        onError: (error, handler) async {
          final status = error.response?.statusCode;
          final path = error.requestOptions.path;
          if (status == 401 && !path.contains('/auth/')) {
            final refreshed = await _refreshToken();
            if (refreshed) {
              try {
                final options = error.requestOptions;
                options.headers['Authorization'] = 'Bearer ${tokens.accessToken}';
                final retried = await _dio.fetch(options);
                handler.resolve(retried);
                return;
              } catch (_) {
                // 落到下面统一抛错
              }
            }
          }
          handler.next(error);
        },
      ),
    );
  }

  String _baseUrl;
  String get baseUrl => _baseUrl;

  /// 允许在登录页修改服务器地址（手机用 WiFi 连电脑时很有用）
  void updateBaseUrl(String url) {
    _baseUrl = url;
    _dio.options.baseUrl = url;
  }

  final TokenStore tokens;
  late final Dio _dio;

  Future<bool> _refreshToken() async {
    final refresh = tokens.refreshToken;
    if (refresh == null || refresh.isEmpty) return false;
    try {
      final resp = await Dio(BaseOptions(baseUrl: baseUrl)).post(
        '/api/v1/auth/refresh',
        data: {'refresh_token': refresh},
      );
      final data = _unwrap(resp);
      await tokens.saveSession(
        accessToken: data['access_token'] as String,
        refreshToken: refresh,
        userId: data['user']['id'] as String,
        nickname: (data['user']['nickname'] as String?) ?? '',
        account: (data['user']['account'] as String?) ?? '',
      );
      return true;
    } catch (_) {
      return false;
    }
  }

  dynamic _unwrap(Response<dynamic> resp) {
    final body = resp.data;
    if (body is Map && body['code'] == 0) return body['data'];
    throw ApiException(
      (body is Map ? body['message'] as String? : null) ?? '服务返回异常',
      code: (body is Map ? body['code'] as int? : null) ?? -1,
      status: resp.statusCode ?? 0,
    );
  }

  Never _throwDio(DioException e) {
    final body = e.response?.data;
    if (body is Map) {
      final msg = (body['message'] as String?) ?? '网络异常';
      debugPrint('[api] ${e.requestOptions.method} ${e.requestOptions.path} -> '
          '${e.response?.statusCode} $msg');
      throw ApiException(
        msg,
        code: (body['code'] as int?) ?? -1,
        status: e.response?.statusCode ?? 0,
      );
    }
    final hint = e.type == DioExceptionType.connectionTimeout ||
            e.type == DioExceptionType.connectionError
        ? '连不上服务器，请检查网络或服务器地址'
        : '网络异常：${e.message}';
    debugPrint('[api] ${e.requestOptions.method} ${e.requestOptions.uri} -> '
        '${e.type} ${e.message}');
    throw ApiException(hint);
  }

  Future<Session> register({
    required String account,
    required String password,
    required String nickname,
    String inviteCode = '',
  }) async {
    try {
      final resp = await _dio.post('/api/v1/auth/register', data: {
        'account': account,
        'password': password,
        'nickname': nickname,
        'invite_code': inviteCode,
        'device': 'flutter-android',
      });
      return _toSession(_unwrap(resp));
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  Future<Session> login({required String account, required String password}) async {
    try {
      final resp = await _dio.post('/api/v1/auth/login', data: {
        'account': account,
        'password': password,
        'device': 'flutter-android',
      });
      return _toSession(_unwrap(resp));
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  Session _toSession(dynamic data) => Session(
        accessToken: data['access_token'] as String,
        refreshToken: (data['refresh_token'] as String?) ?? '',
        user: AppUser.fromJson(data['user'] as Map<String, dynamic>),
      );

  Future<void> logout(String refreshToken) async {
    try {
      await _dio.post('/api/v1/auth/logout', data: {'refresh_token': refreshToken});
    } catch (_) {
      // 退出登录失败不阻塞本地清理
    }
  }

  /// 单家庭系统：返回当前用户和唯一的家庭（尚未初始化时 family 为 null）。
  Future<(AppUser, Family?)> me() async {
    try {
      final resp = await _dio.get('/api/v1/me');
      final data = _unwrap(resp);
      final user = AppUser.fromJson(data['user'] as Map<String, dynamic>);
      final rawFamily = data['family'];
      final family = rawFamily is Map<String, dynamic>
          ? Family.fromJson(rawFamily)
          : null;
      return (user, family);
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  Future<List<MemberInfo>> members() async {
    try {
      final resp = await _dio.get('/api/v1/family/members');
      final data = _unwrap(resp);
      return (data['items'] as List<dynamic>)
          .map((e) => MemberInfo.fromJson(e as Map<String, dynamic>))
          .toList();
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  /// 生成加入唯一家庭的邀请码（户主/管理员）。
  Future<String> createInvite({String role = 'member'}) async {
    try {
      final resp = await _dio.post(
        '/api/v1/family/invites',
        data: {'role': role, 'expires_in_hours': 72},
      );
      return _unwrap(resp)['code'] as String;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  Future<Map<String, dynamic>> syncPull(int since, {int limit = 500}) async {
    try {
      final resp = await _dio.get(
        '/api/v1/sync',
        queryParameters: {'since': since, 'limit': limit},
      );
      return _unwrap(resp) as Map<String, dynamic>;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  Future<Map<String, dynamic>> syncPush(List<Map<String, dynamic>> ops) async {
    try {
      final resp = await _dio.post('/api/v1/sync/push', data: {'ops': ops});
      return _unwrap(resp) as Map<String, dynamic>;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }
// ---------- 账单邮箱 ----------

  Future<Map<String, dynamic>> mailStatus() async {
    try {
      final resp = await _dio.get('/api/v1/mail');
      return _unwrap(resp) as Map<String, dynamic>;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  Future<void> saveMailConfig({
    required String host,
    required int port,
    required String username,
    String password = '',
    bool useTls = true,
    bool enabled = true,
    String extractPassword = '',
    bool resetCursor = false,
  }) async {
    try {
      await _dio.put('/api/v1/mail', data: {
        'host': host,
        'port': port,
        'username': username,
        'password': password,
        'use_tls': useTls,
        'enabled': enabled,
        'extract_password': extractPassword,
        'reset_cursor': resetCursor,
      });
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  Future<void> testMailConfig({
    required String host,
    required int port,
    required String username,
    String password = '',
    bool useTls = true,
  }) async {
    try {
      await _dio.post('/api/v1/mail/test', data: {
        'host': host,
        'port': port,
        'username': username,
        'password': password,
        'use_tls': useTls,
      });
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  Future<Map<String, dynamic>> syncMail() async {
    try {
      final resp = await _dio.post('/api/v1/mail/sync');
      return _unwrap(resp) as Map<String, dynamic>;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  /// 提交解压码。带 messageId 只重试这一封（每份账单密码都不同）。
  Future<Map<String, dynamic>> submitMailPassword(
    String password, {
    String messageId = '',
  }) async {
    try {
      final resp = await _dio.post('/api/v1/mail/password', data: {
        'message_id': messageId,
        'password': password,
      });
      return _unwrap(resp) as Map<String, dynamic>;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }
// ---------- AI 分类 / 人工确认 ----------

  Future<Map<String, dynamic>> classifyPending({double minConfidence = 0.7}) async {
    try {
      final resp = await _dio.get(
        '/api/v1/classify/pending',
        queryParameters: <String, dynamic>{'min_confidence': minConfidence},
      );
      return _unwrap(resp) as Map<String, dynamic>;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  Future<Map<String, dynamic>> runClassify() async {
    try {
      final resp = await _dio.post('/api/v1/classify');
      return _unwrap(resp) as Map<String, dynamic>;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  /// 人工确认一条账目：家庭/个人 + 分类，可选「记住这个商户」
  Future<void> reviewTransaction(
    String txId, {
    required String scope,
    String ownerId = '',
    String categoryId = '',
    bool remember = true,
  }) async {
    try {
      await _dio.post('/api/v1/transactions/$txId/review', data: <String, dynamic>{
        'scope': scope,
        'owner_id': ownerId,
        'category_id': categoryId,
        'remember': remember,
      });
    } on DioException catch (e) {
      _throwDio(e);
    }
  }
// ---------- 拆单 ----------

  /// 把一笔账目拆成多行。lines 里每行：mid 无意义，用 map。
  Future<int> splitTransaction(
    String txId,
    List<Map<String, dynamic>> lines,
  ) async {
    try {
      final resp = await _dio.post(
        '/api/v1/transactions/$txId/split',
        data: <String, dynamic>{'lines': lines},
      );
      final data = _unwrap(resp) as Map<String, dynamic>;
      return data['lines'] as int? ?? lines.length;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  Future<void> undoSplit(String txId) async {
    try {
      await _dio.post('/api/v1/transactions/$txId/unsplit');
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  Future<List<Map<String, dynamic>>> splitChildren(String txId) async {
    try {
      final resp = await _dio.get('/api/v1/transactions/$txId/children');
      final data = _unwrap(resp) as Map<String, dynamic>;
      return (data['items'] as List<dynamic>? ?? <dynamic>[]).cast<Map<String, dynamic>>();
    } on DioException catch (e) {
      _throwDio(e);
    }
  }
Future<Map<String, dynamic>> scopeBreakdown({required String month}) async {
    try {
      final resp = await _dio.get(
        '/api/v1/stats/scopes',
        queryParameters: <String, dynamic>{'month': month},
      );
      return _unwrap(resp) as Map<String, dynamic>;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }
// ---------- 分享导入（AI 生成的 CSV） ----------

  /// 上传 CSV 做预览，不入库
  Future<Map<String, dynamic>> previewImportBytes(
    String fileName,
    List<int> bytes,
  ) async {
    try {
      final form = FormData.fromMap(<String, dynamic>{
        'file': MultipartFile.fromBytes(bytes, filename: fileName),
      });
      final resp = await _dio.post(
        '/api/v1/imports/preview',
        data: form,
        options: Options(contentType: 'multipart/form-data'),
      );
      return _unwrap(resp) as Map<String, dynamic>;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  /// 确认导入预览过的记录
  Future<Map<String, dynamic>> confirmImport(
    List<Map<String, dynamic>> rows,
  ) async {
    try {
      final resp = await _dio.post(
        '/api/v1/imports/confirm',
        data: <String, dynamic>{'rows': rows},
      );
      return _unwrap(resp) as Map<String, dynamic>;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }
// ---------- 设备配对 ----------

  Future<Map<String, dynamic>> deviceStart({
    required String deviceUid,
    required String deviceKey,
    required String model,
    required String osVersion,
    required String appVersion,
  }) async {
    try {
      final resp = await _dio.post('/api/v1/auth/device/start', data: <String, dynamic>{
        'device_uid': deviceUid,
        'device_key': deviceKey,
        'model': model,
        'os_version': osVersion,
        'app_version': appVersion,
      });
      return _unwrap(resp) as Map<String, dynamic>;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }

  Future<Map<String, dynamic>> devicePoll({
    required String pairCode,
    required String pairSecret,
  }) async {
    try {
      final resp = await _dio.post('/api/v1/auth/device/poll', data: <String, dynamic>{
        'pair_code': pairCode,
        'pair_secret': pairSecret,
      });
      return _unwrap(resp) as Map<String, dynamic>;
    } on DioException catch (e) {
      _throwDio(e);
    }
  }
/// 用永久记住的设备标识直接换令牌。
  /// 设备还没被授权过时后端返回 4101，调用方据此转去走配对流程。
  Future<Session> deviceLogin(String deviceKey) async {
    try {
      final resp = await _dio.post('/api/v1/auth/device/login', data: {'device_key': deviceKey});
      final data = _unwrap(resp) as Map<String, dynamic>;
      return Session(
        accessToken: data['access_token'] as String,
        refreshToken: data['refresh_token'] as String,
        user: AppUser.fromJson(data['user'] as Map<String, dynamic>),
      );
    } on DioException catch (e) {
      _throwDio(e);
    }
  }
}