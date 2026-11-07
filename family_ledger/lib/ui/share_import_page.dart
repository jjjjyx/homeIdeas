import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../auth/auth_store.dart';
import '../data/models.dart';
import '../data/remote_api.dart';
import '../theme/app_theme.dart';

/// 接收「分享到本 App」的 CSV：预览 → 确认 → 入库。
///
/// 场景：用 DeepSeek 等 AI 把消费截图转成 CSV，在分享面板里点「家庭记账本」，
/// 文件就到这里；也可能是纯文本（直接粘一段表格）。
class ShareImportPage extends StatefulWidget {
  const ShareImportPage({
    super.key,
    required this.auth,
    required this.fileName,
    required this.categories,
    this.text,
    this.fileBytes,
  });

  final AuthStore auth;
  final String fileName;
  final List<Category> categories;
  final String? text;
  final List<int>? fileBytes;

  @override
  State<ShareImportPage> createState() => _ShareImportPageState();
}

class _ShareImportPageState extends State<ShareImportPage> {
  bool _loading = true;
  bool _busy = false;
  String? _error;
  String _source = '';

  List<Map<String, dynamic>> _rows = <Map<String, dynamic>>[];
  List<Category> _categories = const <Category>[];

  @override
  void initState() {
    super.initState();
    _preview();
  }

  Future<void> _preview() async {
    try {
      if (widget.text != null && widget.text!.startsWith('__SHARE_ERROR__:')) {
        throw ApiException('读取分享文件失败：');
      }

      final bytes = widget.fileBytes ??
          (widget.text != null
              ? utf8.encode(widget.text!.replaceAll('\r\n', '\n').replaceAll('\\n', '\n'))
              : <int>[]);
      if (bytes.isEmpty) {
        throw ApiException('分享内容为空');
      }
      final data = await widget.auth.api.previewImportBytes(
        widget.fileName,
        bytes,
      );
      final rows = (data['rows'] as List<dynamic>? ?? <dynamic>[])
          .map((e) => Map<String, dynamic>.from(e as Map))
          .toList();
      if (!mounted) return;
      setState(() {
        _rows = rows;
        _categories = widget.categories.where((c) => c.type == EntryType.expense).toList();
        _source = (data['source'] as String?) ?? '';
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = e is ApiException ? e.message : '$e';
      });
    }
  }

  Future<void> _confirm() async {
    if (_rows.isEmpty) return;
    setState(() => _busy = true);
    try {
      final summary = await widget.auth.api.confirmImport(_rows);
      if (!mounted) return;
      final imported = summary['imported'] ?? 0;
      final dup = summary['duplicated'] ?? 0;
      await showDialog<void>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('导入完成'),
          content: Text(dup > 0 ? '导入 $imported 条，$dup 条重复已跳过' : '成功导入 $imported 条'),
          actions: <Widget>[
            FilledButton(
              onPressed: () => Navigator.pop(ctx),
              child: const Text('好'),
            ),
          ],
        ),
      );
      if (mounted) Navigator.of(context).pop(true);
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('导入失败：${e is ApiException ? e.message : e}')),
      );
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _removeRow(int index) => setState(() => _rows.removeAt(index));

  Future<void> _editRow(int index) async {
    final row = _rows[index];
    final noteCtrl = TextEditingController(text: row['note'] as String? ?? '');
    final amountCtrl = TextEditingController(
        text: ((row['amount_cents'] as int? ?? 0) / 100).toStringAsFixed(2));
    var scope = row['scope'] as String? ?? 'unknown';
    var categoryId = row['category_id'] as String? ?? '';

    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setLocal) => AlertDialog(
          title: const Text('修改这一条'),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                TextField(
                  controller: amountCtrl,
                  keyboardType: const TextInputType.numberWithOptions(decimal: true),
                  decoration: const InputDecoration(labelText: '金额'),
                ),
                const SizedBox(height: 10),
                TextField(
                  controller: noteCtrl,
                  decoration: const InputDecoration(labelText: '备注'),
                ),
                const SizedBox(height: 12),
                SegmentedButton<String>(
                  segments: const <ButtonSegment<String>>[
                    ButtonSegment<String>(value: 'family', label: Text('家庭')),
                    ButtonSegment<String>(value: 'personal', label: Text('个人')),
                    ButtonSegment<String>(value: 'transfer', label: Text('往来')),
                  ],
                  selected: <String>{scope},
                  onSelectionChanged: (s) => setLocal(() => scope = s.first),
                ),
                const SizedBox(height: 12),
                DropdownButtonFormField<String>(
                  initialValue: categoryId.isEmpty ? null : categoryId,
                  decoration: const InputDecoration(labelText: '分类'),
                  items: <DropdownMenuItem<String>>[
                    for (final c in _categories)
                      DropdownMenuItem<String>(value: c.id, child: Text(c.name)),
                  ],
                  onChanged: (v) => setLocal(() => categoryId = v ?? categoryId),
                ),
              ],
            ),
          ),
          actions: <Widget>[
            TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('取消')),
            FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('保存')),
          ],
        ),
      ),
    );

    if (ok != true) return;
    final cents = ((double.tryParse(amountCtrl.text.trim()) ?? 0) * 100).round();
    setState(() {
      row['note'] = noteCtrl.text.trim();
      row['amount_cents'] = cents;
      row['scope'] = scope;
      if (categoryId.isNotEmpty) {
        row['category_id'] = categoryId;
        for (final c in _categories) {
          if (c.id == categoryId) row['category_name'] = c.name;
        }
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('导入账单'),
        actions: <Widget>[
          if (!_loading && _rows.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(right: 12),
              child: Center(
                child: Text('${_rows.length} 条',
                    style: const TextStyle(fontSize: 13, color: Colors.grey)),
              ),
            ),
        ],
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : SafeArea(
              child: _error != null
                  ? _errorView()
                  : Column(
                      children: <Widget>[
                        _banner(),
                        Expanded(child: ListView.builder(
                          padding: const EdgeInsets.fromLTRB(16, 4, 16, 12),
                          itemCount: _rows.length,
                          itemBuilder: (context, i) => _rowCard(i),
                        )),
                        _bottomBar(),
                      ],
                    ),
            ),
    );
  }

  Widget _banner() {
    final ai = _source == 'ai';
    return Container(
      margin: const EdgeInsets.fromLTRB(16, 12, 16, 8),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: const Color(0xFF2F6F62).withValues(alpha: 0.07),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Text(
        ai
            ? '这个表格是 AI 识别后整理出来的，请核对金额和分类再导入。'
            : '已按记账模板解析，核对无误后点「确认导入」。',
        style: const TextStyle(fontSize: 12.5, color: Color(0xFF2F6F62)),
      ),
    );
  }

  Widget _rowCard(int index) {
    final row = _rows[index];
    final date = DateTime.fromMillisecondsSinceEpoch(row['occurred_at'] as int? ?? 0);
    final income = (row['type'] as int? ?? 0) == 1;
    final scope = row['scope'] as String? ?? 'unknown';

    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      decoration: cardDecoration(context),
      padding: const EdgeInsets.fromLTRB(14, 10, 8, 10),
      child: Row(
        children: <Widget>[
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Row(
                  children: <Widget>[
                    Text(
                      '${income ? '+' : '-'}¥${((row['amount_cents'] as int? ?? 0) / 100).toStringAsFixed(2)}',
                      style: TextStyle(
                        fontSize: 15,
                        fontWeight: FontWeight.w600,
                        color: income ? const Color(0xFF2E9E6B) : const Color(0xFFD64545),
                      ),
                    ),
                    const SizedBox(width: 8),
                    _chip(row['category_name'] as String? ?? '其他'),
                    const SizedBox(width: 6),
                    _chip(_scopeLabel(scope), muted: true),
                  ],
                ),
                const SizedBox(height: 4),
                Text(
                  '${row['note'] ?? ''}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 12.5, color: Colors.grey.shade600),
                ),
                Text(
                  '${date.year}-${date.month.toString().padLeft(2, '0')}-${date.day.toString().padLeft(2, '0')}',
                  style: TextStyle(fontSize: 11, color: Colors.grey.shade400),
                ),
              ],
            ),
          ),
          IconButton(
            tooltip: '修改',
            icon: const Icon(Icons.edit_outlined, size: 18),
            onPressed: () => _editRow(index),
          ),
          IconButton(
            tooltip: '不要这条',
            icon: const Icon(Icons.close, size: 18),
            onPressed: () => _removeRow(index),
          ),
        ],
      ),
    );
  }

  Widget _chip(String text, {bool muted = false}) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
      decoration: BoxDecoration(
        color: muted ? Colors.grey.shade100 : const Color(0xFF2F6F62).withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Text(
        text,
        style: TextStyle(
          fontSize: 11,
          color: muted ? Colors.grey.shade600 : const Color(0xFF2F6F62),
        ),
      ),
    );
  }

  Widget _bottomBar() {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 4, 16, 16),
      child: Row(
        children: <Widget>[
          Expanded(
            child: OutlinedButton(
              onPressed: _busy ? null : () => Navigator.of(context).pop(false),
              child: const Text('取消'),
            ),
          ),
          const SizedBox(width: 10),
          Expanded(
            flex: 2,
            child: FilledButton(
              onPressed: _busy || _rows.isEmpty ? null : _confirm,
              child: _busy
                  ? const SizedBox(
                      width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
                  : Text('确认导入 ${_rows.length} 条'),
            ),
          ),
        ],
      ),
    );
  }

  Widget _errorView() {
    return Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: <Widget>[
          const Icon(Icons.error_outline, size: 44, color: Color(0xFFD64545)),
          const SizedBox(height: 12),
          Text(_error!, textAlign: TextAlign.center, style: const TextStyle(fontSize: 13)),
          const SizedBox(height: 20),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('返回'),
          ),
        ],
      ),
    );
  }

  String _scopeLabel(String scope) => switch (scope) {
        'family' => '家庭',
        'personal' => '个人',
        'transfer' => '往来',
        _ => '待定',
      };
}

/// 处理 Android 分享进来的内容（平台通道，见 MainActivity.kt）
class ShareIntentHandler {
  ShareIntentHandler(this.auth, this.categories, this.onReady);

  final AuthStore auth;
  final List<Category> categories;
  final void Function(ShareImportPage page) onReady;

  static const MethodChannel _channel = MethodChannel('family_ledger/share');
  bool _started = false;

  void start() {
    if (_started) return;
    _started = true;

    _channel.setMethodCallHandler((call) async {
      if (call.method == 'onShared') {
        _handle(call.arguments as Map?);
      }
      return null;
    });

    // App 冷启动时可能带着分享内容
    _channel.invokeMethod<Map>('getInitialShare').then((data) {
      debugPrint('[share] 初始内容: $data');
      if (data != null) _handle(data);
    }).catchError((_) {});
  }

  Future<void> _handle(Map? raw) async {
    debugPrint('[share] 收到: $raw');
    if (raw == null) return;
    if (auth.currentFamily == null) return;

    final path = raw['path'] as String?;
    final text = raw['text'] as String?;

    if (path != null && path.isNotEmpty) {
      final file = File(path);
      if (await file.exists()) {
        final bytes = await file.readAsBytes();
        if (bytes.isNotEmpty) {
          onReady(ShareImportPage(
            auth: auth,
            fileName: path.split(Platform.pathSeparator).last,
            categories: categories,
            fileBytes: bytes,
          ));
          return;
        }
      }
    }

    if (text != null && text.startsWith('__SHARE_ERROR__:')) {
      onReady(ShareImportPage(
        auth: auth,
        fileName: 'share-error',
        categories: categories,
        text: text,
      ));
      return;
    }

    if (text != null && text.trim().isNotEmpty) {
      onReady(ShareImportPage(
        auth: auth,
        fileName: 'shared-text.csv',
        categories: categories,
        text: text,
      ));
    }
  }
}