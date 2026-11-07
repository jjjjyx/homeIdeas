import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../data/ledger_store.dart';
import '../data/models.dart';
import '../data/remote_api.dart';
import '../core/money.dart';
import '../theme/app_theme.dart';

/// 拆单页：一笔账目（比如超市采购）里既有家庭用品也有个人物品时，
/// 把它拆成多行，每行单独指定归属和分类。
class SplitPage extends StatefulWidget {
  const SplitPage({super.key, required this.store, required this.entry});

  final LedgerStore store;
  final Transaction entry;

  @override
  State<SplitPage> createState() => _SplitPageState();
}

class _SplitLine {
  _SplitLine({
    required this.controller,
    required this.noteController,
    this.scope = 'family',
    this.categoryId = '',
  });

  final TextEditingController controller;
  final TextEditingController noteController;
  String scope;
  String categoryId;

  String get note => noteController.text;

  int get cents {
    final text = controller.text.trim();
    if (text.isEmpty) return 0;
    final value = double.tryParse(text);
    if (value == null) return 0;
    return (value * 100).round();
  }
}

class _SplitPageState extends State<SplitPage> {
  final List<_SplitLine> _lines = <_SplitLine>[];
  bool _saving = false;

  List<Category> get _categories =>
      widget.store.categories.where((c) => c.type == EntryType.expense).toList();

  int get _total => _lines.fold(0, (sum, l) => sum + l.cents);
  int get _remain => widget.entry.amountCents - _total;

  @override
  void initState() {
    super.initState();
    // 默认给两行，金额按 6:4 预分，方便直接改
    final total = widget.entry.amountCents;
    final first = (total * 0.6).round();
    _lines.add(_SplitLine(
      controller: TextEditingController(text: (first / 100).toStringAsFixed(2)),
      noteController: TextEditingController(text: '家庭部分'),
      categoryId: widget.entry.categoryId,
    ));
    _lines.add(_SplitLine(
      controller: TextEditingController(text: ((total - first) / 100).toStringAsFixed(2)),
      noteController: TextEditingController(text: '个人部分'),
      scope: 'personal',
      categoryId: widget.entry.categoryId,
    ));
  }

  @override
  void dispose() {
    for (final l in _lines) {
      l.controller.dispose();
      l.noteController.dispose();
    }
    super.dispose();
  }

  void _addLine() {
    setState(() {
      _lines.add(_SplitLine(
        controller: TextEditingController(text: '0.00'),
        noteController: TextEditingController(),
        categoryId: widget.entry.categoryId,
      ));
    });
  }

  void _removeLine(int index) {
    setState(() {
      _lines[index].controller.dispose();
      _lines[index].noteController.dispose();
      _lines.removeAt(index);
    });
  }

  Future<void> _save() async {
    if (_lines.length < 2) {
      _toast('至少拆成两行');
      return;
    }
    if (_remain != 0) {
      _toast('各行合计必须等于 ${formatMoney(widget.entry.amountCents)}');
      return;
    }
    for (final l in _lines) {
      if (l.cents <= 0) {
        _toast('每行金额都要大于 0');
        return;
      }
      if (l.categoryId.isEmpty) {
        _toast('每行都要选分类');
        return;
      }
    }

    setState(() => _saving = true);
    try {
      final familyId = widget.store.familyId;
      await widget.store.api.splitTransaction(
        familyId,
        widget.entry.id,
        _lines
            .map((l) => <String, dynamic>{
                  'amount_cents': l.cents,
                  'scope': l.scope,
                  'category_id': l.categoryId,
                  'note': l.note.trim(),
                })
            .toList(),
      );
      await widget.store.syncNow();
      if (!mounted) return;
      Navigator.of(context).pop(true);
      _toast('已拆成 ${_lines.length} 笔');
    } catch (e) {
      if (!mounted) return;
      _toast(e is ApiException ? e.message : '$e', error: true);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  void _toast(String message, {bool error = false}) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(message),
        backgroundColor: error ? const Color(0xFFD64545) : null,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final entry = widget.entry;
    final canSave = _remain == 0 && _lines.length >= 2 && !_saving;

    return Scaffold(
      appBar: AppBar(
        title: const Text('拆分这笔'),
        actions: <Widget>[
          TextButton(
            onPressed: canSave ? _save : null,
            child: _saving
                ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
                : const Text('保存'),
          ),
        ],
      ),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 120),
          children: <Widget>[
            Container(
              decoration: cardDecoration(context),
              padding: const EdgeInsets.all(16),
              child: Row(
                children: <Widget>[
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: <Widget>[
                        Text(entry.note.isEmpty ? '无备注' : entry.note,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w600)),
                        const SizedBox(height: 4),
                        Text(
                          '原金额 ${formatMoney(entry.amountCents)}',
                          style: TextStyle(fontSize: 12, color: Colors.grey.shade500),
                        ),
                      ],
                    ),
                  ),
                  Text(
                    formatMoney(entry.amountCents),
                    style: const TextStyle(fontSize: 20, fontWeight: FontWeight.w700),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 12),
            for (var i = 0; i < _lines.length; i++) _lineCard(i),
            const SizedBox(height: 8),
            OutlinedButton.icon(
              onPressed: _addLine,
              icon: const Icon(Icons.add, size: 18),
              label: const Text('添加一行'),
            ),
            const SizedBox(height: 16),
            _summaryBar(),
          ],
        ),
      ),
    );
  }

  Widget _lineCard(int index) {
    final line = _lines[index];
    final categories = _categories;
    final selected = categories.where((c) => c.id == line.categoryId).firstOrNull;

    return Container(
      margin: const EdgeInsets.only(bottom: 10),
      decoration: cardDecoration(context),
      padding: const EdgeInsets.fromLTRB(14, 10, 8, 10),
      child: Column(
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(
                flex: 3,
                child: TextField(
                  controller: line.controller,
                  keyboardType: const TextInputType.numberWithOptions(decimal: true),
                  inputFormatters: <TextInputFormatter>[
                    FilteringTextInputFormatter.allow(RegExp(r'[0-9.]')),
                  ],
                  decoration: const InputDecoration(labelText: '金额', isDense: true),
                  onChanged: (_) => setState(() {}),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                flex: 4,
                child: TextField(
                  controller: line.noteController,
                  decoration: const InputDecoration(labelText: '商品（可选）', isDense: true),
                ),
              ),
              IconButton(
                onPressed: _lines.length > 2 ? () => _removeLine(index) : null,
                icon: const Icon(Icons.remove_circle_outline, size: 20),
                tooltip: '删除这行',
              ),
            ],
          ),
          const SizedBox(height: 8),
          Row(
            children: <Widget>[
              Expanded(
                child: SegmentedButton<String>(
                  segments: const <ButtonSegment<String>>[
                    ButtonSegment<String>(value: 'family', label: Text('家庭', style: TextStyle(fontSize: 12))),
                    ButtonSegment<String>(value: 'personal', label: Text('个人', style: TextStyle(fontSize: 12))),
                  ],
                  selected: <String>{line.scope},
                  onSelectionChanged: (s) => setState(() => line.scope = s.first),
                  style: const ButtonStyle(visualDensity: VisualDensity.compact),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: DropdownButtonFormField<String>(
                  initialValue: selected?.id,
                  isDense: true,
                  decoration: const InputDecoration(labelText: '分类', isDense: true),
                  items: <DropdownMenuItem<String>>[
                    for (final c in categories)
                      DropdownMenuItem<String>(value: c.id, child: Text(c.name, style: const TextStyle(fontSize: 13))),
                  ],
                  onChanged: (v) => setState(() => line.categoryId = v ?? ''),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _summaryBar() {
    final ok = _remain == 0;
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: (ok ? const Color(0xFF2E9E6B) : const Color(0xFFE08A1E)).withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Row(
        children: <Widget>[
          Icon(ok ? Icons.check_circle_outline : Icons.error_outline,
              size: 18, color: ok ? const Color(0xFF2E9E6B) : const Color(0xFFE08A1E)),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              ok
                  ? '合计 ${formatMoney(_total)}，与原金额一致'
                  : _remain > 0
                      ? '还剩 ${formatMoney(_remain)} 未分配'
                      : '超出 ${formatMoney(-_remain)}',
              style: TextStyle(
                fontSize: 13,
                color: ok ? const Color(0xFF2E9E6B) : const Color(0xFFE08A1E),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
