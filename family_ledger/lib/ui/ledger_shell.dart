import 'package:flutter/material.dart';

import '../auth/auth_store.dart';
import '../data/ledger_store.dart';
import 'entry_edit_page.dart';
import 'home_page.dart';
import 'share_import_page.dart';
import 'settings_page.dart';
import 'stats_page.dart';

class LedgerShell extends StatefulWidget {
  const LedgerShell({super.key, required this.auth, required this.store});

  final AuthStore auth;
  final LedgerStore store;

  @override
  State<LedgerShell> createState() => _LedgerShellState();
}

class _LedgerShellState extends State<LedgerShell> {
  int _index = 0;

  // 接收系统分享过来的 CSV（AI 智能体生成的账单）
  late final ShareIntentHandler _shareHandler;

  @override
  void initState() {
    super.initState();
    _shareHandler = ShareIntentHandler(widget.auth, widget.store.categories, (page) {
      if (!mounted) return;
      Navigator.of(context)
          .push(MaterialPageRoute<bool>(builder: (_) => page))
          .then((imported) {
        if (imported == true) {
          widget.store.syncNow();
          widget.store.refreshScope();
        }
      });
    });
    _shareHandler.start();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: IndexedStack(
        index: _index,
        children: <Widget>[
          HomePage(store: widget.store),
          StatsPage(store: widget.store),
          SettingsPage(store: widget.store, auth: widget.auth),
        ],
      ),
      // 只在「账单」页显示悬浮按钮：统计页图表在右下角，按钮会压住柱子
      floatingActionButton: _index == 0
          ? FloatingActionButton(
              onPressed: () => openEntryEditor(context, widget.store),
              tooltip: '记一笔',
              child: const Icon(Icons.add_rounded, size: 28),
            )
          : null,
      bottomNavigationBar: NavigationBar(
        selectedIndex: _index,
        onDestinationSelected: (value) => setState(() => _index = value),
        destinations: const <NavigationDestination>[
          NavigationDestination(
            icon: Icon(Icons.receipt_long_outlined),
            selectedIcon: Icon(Icons.receipt_long),
            label: '账单',
          ),
          NavigationDestination(
            icon: Icon(Icons.pie_chart_outline_rounded),
            selectedIcon: Icon(Icons.pie_chart_rounded),
            label: '统计',
          ),
          NavigationDestination(
            icon: Icon(Icons.settings_outlined),
            selectedIcon: Icon(Icons.settings),
            label: '设置',
          ),
        ],
      ),
    );
  }
}
