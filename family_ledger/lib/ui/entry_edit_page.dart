import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core/money.dart';
import '../data/category_icons.dart';
import '../data/ledger_store.dart';
import '../data/models.dart';
import '../theme/app_theme.dart';

Future<void> openEntryEditor(
  BuildContext context,
  LedgerStore store, {
  Transaction? entry,
}) {
  return Navigator.of(context).push<void>(
    MaterialPageRoute<void>(
      builder: (_) => EntryEditPage(store: store, entry: entry),
    ),
  );
}

class EntryEditPage extends StatefulWidget {
  const EntryEditPage({super.key, required this.store, this.entry});

  final LedgerStore store;
  final Transaction? entry;

  @override
  State<EntryEditPage> createState() => _EntryEditPageState();
}

class _EntryEditPageState extends State<EntryEditPage> {
  final TextEditingController _amountController = TextEditingController();
  final TextEditingController _noteController = TextEditingController();
  final FocusNode _amountFocus = FocusNode();

  late EntryType _type;
  late String _categoryId;
  late DateTime _date;
  late EntryVisibility _visibility;
  bool _saving = false;

  bool get _isEditing => widget.entry != null;

  @override
  void initState() {
    super.initState();
    final entry = widget.entry;
    _type = entry?.type ?? EntryType.expense;
    _date = entry?.occurredAt ?? DateTime.now();
    _visibility = entry?.visibility ?? EntryVisibility.family;
    final categories = widget.store.categoriesOf(_type);
    _categoryId = entry?.categoryId ?? (categories.isEmpty ? '' : categories.first.id);
    if (entry != null) {
      _amountController.text = formatMoneyTrimmed(entry.amountCents);
      _noteController.text = entry.note;
    }
  }

  @override
  void dispose() {
    _amountController.dispose();
    _noteController.dispose();
    _amountFocus.dispose();
    super.dispose();
  }

  void _changeType(EntryType type) {
    if (type == _type) return;
    final categories = widget.store.categoriesOf(type);
    setState(() {
      _type = type;
      _categoryId = categories.isEmpty ? '' : categories.first.id;
    });
    // 切换收支类型后 SegmentedButton 会抢走焦点，把光标还给金额输入框
    _amountFocus.requestFocus();
  }

  void _toast(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  Future<void> _pickDate() async {
    final now = DateTime.now();
    final picked = await showDatePicker(
      context: context,
      initialDate: _date,
      firstDate: DateTime(2000),
      lastDate: DateTime(now.year + 1, 12, 31),
      helpText: '选择日期',
    );
    if (picked != null) {
      setState(() => _date = picked);
    }
  }

  Future<void> _save() async {
    final cents = parseAmountToCents(_amountController.text);
    if (cents == null) {
      _toast('请输入大于 0 的金额');
      return;
    }
    if (_categoryId.isEmpty) {
      _toast('请选择一个分类');
      return;
    }
    final date = DateTime(_date.year, _date.month, _date.day, 12);
    setState(() => _saving = true);

    if (_isEditing) {
      await widget.store.updateEntry(
        widget.entry!.copyWith(
          type: _type,
          amountCents: cents,
          categoryId: _categoryId,
          occurredAt: date,
          note: _noteController.text.trim(),
          visibility: _visibility,
        ),
      );
    } else {
      await widget.store.addEntry(
        type: _type,
        amountCents: cents,
        categoryId: _categoryId,
        date: date,
        note: _noteController.text.trim(),
        visibility: _visibility,
      );
    }
    if (!mounted) return;
    Navigator.of(context).pop();
  }

  Future<void> _delete() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('删除这笔记录？'),
        content: const Text('删除后家人也会看不到'),
        actions: <Widget>[
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed != true || widget.entry == null) return;
    await widget.store.deleteEntry(widget.entry!);
    if (!mounted) return;
    Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    final categories = widget.store.categoriesOf(_type);
    final color = _type.isIncome ? kIncomeColor : kExpenseColor;

    return Scaffold(
      appBar: AppBar(
        title: Text(_isEditing ? '编辑记录' : '记一笔'),
        actions: <Widget>[
          if (_isEditing)
            IconButton(
              tooltip: '删除',
              onPressed: _delete,
              icon: const Icon(Icons.delete_outline_rounded),
            ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
        children: <Widget>[
          Center(
            child: SegmentedButton<EntryType>(
              segments: const <ButtonSegment<EntryType>>[
                ButtonSegment<EntryType>(
                  value: EntryType.expense,
                  label: Text('支出'),
                  icon: Icon(Icons.arrow_upward_rounded),
                ),
                ButtonSegment<EntryType>(
                  value: EntryType.income,
                  label: Text('收入'),
                  icon: Icon(Icons.arrow_downward_rounded),
                ),
              ],
              selected: <EntryType>{_type},
              onSelectionChanged: (selection) => _changeType(selection.first),
            ),
          ),
          const SizedBox(height: 20),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 14),
            decoration: cardDecoration(context),
            child: Row(
              children: <Widget>[
                Text(
                  '¥',
                  style: TextStyle(
                    fontSize: 24,
                    fontWeight: FontWeight.w600,
                    color: color,
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: TextField(
                    controller: _amountController,
                    focusNode: _amountFocus,
                    autofocus: !_isEditing,
                    keyboardType: const TextInputType.numberWithOptions(decimal: true),
                    inputFormatters: <TextInputFormatter>[
                      FilteringTextInputFormatter.allow(RegExp(r'[0-9.]')),
                      LengthLimitingTextInputFormatter(12),
                    ],
                    style: TextStyle(
                      fontSize: 34,
                      fontWeight: FontWeight.w700,
                      color: color,
                    ),
                    decoration: const InputDecoration(
                      isDense: true,
                      border: InputBorder.none,
                      hintText: '0.00',
                    ),
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 20),
          const _SectionLabel('分类'),
          const SizedBox(height: 10),
          Wrap(
            spacing: 10,
            runSpacing: 10,
            children: <Widget>[
              for (final category in categories)
                _CategoryChip(
                  category: category,
                  selected: category.id == _categoryId,
                  onTap: () => setState(() => _categoryId = category.id),
                ),
            ],
          ),
          const SizedBox(height: 20),
          const _SectionLabel('谁能看到'),
          const SizedBox(height: 10),
          Row(
            children: <Widget>[
              Expanded(
                child: _VisibilityChip(
                  icon: Icons.groups_rounded,
                  label: '家人可见',
                  selected: _visibility == EntryVisibility.family,
                  onTap: () => setState(() => _visibility = EntryVisibility.family),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: _VisibilityChip(
                  icon: Icons.lock_outline_rounded,
                  label: '仅自己可见',
                  selected: _visibility == EntryVisibility.private,
                  onTap: () => setState(() => _visibility = EntryVisibility.private),
                ),
              ),
            ],
          ),
          const SizedBox(height: 22),
          Container(
            decoration: cardDecoration(context),
            child: Column(
              children: <Widget>[
                ListTile(
                  leading: const Icon(Icons.event_rounded, size: 20),
                  title: const Text('日期'),
                  trailing: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: <Widget>[
                      Text(
                        '${_date.year}年${_date.month}月${_date.day}日',
                        style: TextStyle(fontSize: 14, color: Colors.grey.shade600),
                      ),
                      const SizedBox(width: 4),
                      const Icon(Icons.chevron_right_rounded, size: 18),
                    ],
                  ),
                  onTap: _pickDate,
                ),
                const Divider(height: 1, indent: 16, endIndent: 16),
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 6, 16, 6),
                  child: TextField(
                    controller: _noteController,
                    maxLength: 40,
                    decoration: const InputDecoration(
                      labelText: '备注',
                      hintText: '例如：晚饭、超市采购',
                      border: InputBorder.none,
                      counterText: '',
                    ),
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 26),
          SizedBox(
            height: 50,
            child: FilledButton(
              onPressed: _saving ? null : _save,
              style: FilledButton.styleFrom(
                backgroundColor: color,
                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
              ),
              child: _saving
                  ? const SizedBox(
                      width: 20,
                      height: 20,
                      child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white),
                    )
                  : Text(
                      _isEditing ? '保存修改' : '保存',
                      style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
                    ),
            ),
          ),
        ],
      ),
    );
  }
}

class _SectionLabel extends StatelessWidget {
  const _SectionLabel(this.text);

  final String text;

  @override
  Widget build(BuildContext context) {
    return Text(
      text,
      style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
    );
  }
}

class _VisibilityChip extends StatelessWidget {
  const _VisibilityChip({
    required this.icon,
    required this.label,
    required this.selected,
    required this.onTap,
  });

  final IconData icon;
  final String label;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    const active = Color(0xFF2F6F62);
    return InkWell(
      borderRadius: BorderRadius.circular(14),
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(vertical: 12),
        decoration: BoxDecoration(
          color: selected ? active.withValues(alpha: 0.1) : kCardColor,
          borderRadius: BorderRadius.circular(14),
          border: Border.all(
            color: selected ? active : const Color(0xFFE8EBED),
            width: selected ? 1.6 : 1,
          ),
        ),
        child: Row(
          mainAxisAlignment: MainAxisAlignment.center,
          children: <Widget>[
            Icon(icon, size: 16, color: selected ? active : Colors.grey.shade500),
            const SizedBox(width: 6),
            Text(
              label,
              style: TextStyle(
                fontSize: 13,
                fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
                color: selected ? active : Colors.grey.shade700,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _CategoryChip extends StatelessWidget {
  const _CategoryChip({
    required this.category,
    required this.selected,
    required this.onTap,
  });

  final Category category;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final color = Color(category.colorValue);
    return InkWell(
      borderRadius: BorderRadius.circular(14),
      onTap: onTap,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 150),
        width: 82,
        padding: const EdgeInsets.symmetric(vertical: 10),
        decoration: BoxDecoration(
          color: selected ? color.withValues(alpha: 0.14) : kCardColor,
          borderRadius: BorderRadius.circular(14),
          border: Border.all(
            color: selected ? color : const Color(0xFFE8EBED),
            width: selected ? 1.6 : 1,
          ),
        ),
        child: Column(
          children: <Widget>[
            Icon(iconFor(category.iconKey), size: 20, color: color),
            const SizedBox(height: 6),
            Text(
              category.name,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: 12,
                fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
                color: selected ? color : Colors.grey.shade700,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
