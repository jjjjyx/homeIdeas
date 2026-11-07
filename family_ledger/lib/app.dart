import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';

import 'auth/auth_store.dart';
import 'data/ledger_store.dart';
import 'theme/app_theme.dart';
import 'ui/app_root.dart';

class FamilyLedgerApp extends StatelessWidget {
  const FamilyLedgerApp({super.key, required this.auth, required this.ledger});

  final AuthStore auth;
  final LedgerStore ledger;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: '家庭记账本',
      debugShowCheckedModeBanner: false,
      theme: buildAppTheme(),
      locale: const Locale('zh', 'CN'),
      supportedLocales: const <Locale>[
        Locale('zh', 'CN'),
        Locale('en', 'US'),
      ],
      localizationsDelegates: const <LocalizationsDelegate<dynamic>>[
        GlobalMaterialLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
      ],
      home: AppRoot(auth: auth, ledger: ledger),
    );
  }
}
