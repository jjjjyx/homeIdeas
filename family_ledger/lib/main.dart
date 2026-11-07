import 'dart:async';

import 'package:flutter/material.dart';

import 'app.dart';
import 'auth/auth_store.dart';
import 'data/ledger_store.dart';
import 'data/local_db.dart';
import 'data/remote_api.dart';
import 'data/sync_service.dart';
import 'data/token_store.dart';

/// 后端地址：
/// - 真机 USB 调试时用 `adb reverse tcp:8088 tcp:8088`，然后走 127.0.0.1
/// - 局域网/公网部署时用 --dart-define=API_BASE_URL=http://192.168.x.x:8088
const String apiBaseUrl = String.fromEnvironment(
  'API_BASE_URL',
  defaultValue: 'http://127.0.0.1:8088',
);

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();

  final tokens = await TokenStore.load();
  final savedUrl = tokens.serverUrl;
  final api = RemoteApi(
    baseUrl: (savedUrl == null || savedUrl.isEmpty) ? apiBaseUrl : savedUrl,
    tokens: tokens,
  );
  final db = LocalDatabase.instance;
  final sync = SyncService(api: api, db: db);
  final ledger = LedgerStore(db: db, sync: sync, api: api);
  final auth = AuthStore(api: api, tokens: tokens, ledger: ledger);

  runApp(FamilyLedgerApp(auth: auth, ledger: ledger));
  unawaited(auth.bootstrap());
}
