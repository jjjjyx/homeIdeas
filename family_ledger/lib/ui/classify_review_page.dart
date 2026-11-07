import 'package:flutter/material.dart';


import '../data/models.dart';
import '../data/remote_api.dart';
import '../theme/app_theme.dart';

/// 账单分类确认页：把「AI 拿不准」的账目按商户聚起来，一次归类一批。
///
/// 设计要点：
/// - 按商户分组，不是按账目逐条列（139 笔只有 78 个商户，逐条点会疯）
/// - 一次操作 = 这个商户的所有账目 + 记住这条规则
/// - 看不到「家庭/个人」的判断依据时不敢点，所以展示 AI 的理由和置信度
class ClassifyReviewPage extends StatefulWidget {
  const ClassifyReviewPage({super.key, required this.api, required this.familyId, required this.categories});

  final RemoteApi api;
  final String familyId;
  final List<Category> categories;

  @override
  State<ClassifyReviewPage> createState() => _ClassifyReviewPageState();
}

class _ClassifyReviewPageState extends State<ClassifyReviewPage> {
  bool _loading = true;
  bool _busy = false;
  String? _error;

  List<Map<String, dynamic>> _items = const <Map<String, dynamic>>[];
  List<Map<String, dynamic>> _merchants = const <Map<String, dynamic>>[];

  RemoteApi get _api => widget.api;
  String? get _familyId => widget.familyId.isEmpty ? null : widget.familyId;

  List<Category> get _expenseCategories =>
      widget.categories.where((c) => c.type == EntryType.expense).toList();

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final fid = _familyId;
    if (fid == null) {
      setState(() => _loading = false);
      return;
    }
    try {
      final data = await _api.classifyPending(fid, minConfidence: 0.7);
      if (!mounted) return;
      setState(() {
        _items = (data['items'] as List<dynamic>? ?? <dynamic>[])
            .cast<Map<String, dynamic>>();
        _merchants = (data['merchants'] as List<dynamic>? ?? <dynamic>[])
            .cast<Map<String, dynamic>>();
        _loading = false;
        _error = null;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = e is ApiException ? e.message : '$e';
      });
    }
  }

  /// 某个商户下的所有待确认账目
  List<Map<String, dynamic>> _itemsOf(String merchant) {
    return _items.where((item) {
      final note = (item['note'] as String? ?? '');
      final idx = note.indexOf(' · ');
      final m = idx > 0 ? note.substring(0, idx) : note;
      return m.trim() == merchant;
    }).toList();
  }

  Future<void> _assign(String merchant, String scope, Category category) async {
    final fid = _familyId;
    if (fid == null) return;
    final targets = _itemsOf(merchant);
    if (targets.isEmpty) return;

    setState(() => _busy = true);
    var ok = 0;
    try {
      for (final item in targets) {
        await _api.reviewTransaction(
          fid,
          item['id'] as String,
          scope: scope,
          categoryId: category.id,
          remember: true,
        );
        ok++;
      }
      await _load();
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('「$merchant」$ok 笔 → ${scope == 'family' ? '家庭' : '个人'}·${category.name}，规则已记住'),
          backgroundColor: const Color(0xFF2E9E6B),
        ),
      );
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('处理失败：${e is ApiException ? e.message : e}')),
      );
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _openCategoryPicker(String merchant) async {
    var scope = 'family';
    String? picked;

    final category = await showModalBottomSheet<Category>(
      context: context,
      isScrollControlled: true,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setSheetState) => SafeArea(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(merchant,
                    style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
                const SizedBox(height: 10),
                SegmentedButton<String>(
                  segments: const <ButtonSegment<String>>[
                    ButtonSegment<String>(value: 'family', label: Text('家庭开支')),
                    ButtonSegment<String>(value: 'personal', label: Text('个人开支')),
                  ],
                  selected: <String>{scope},
                  onSelectionChanged: (s) => setSheetState(() => scope = s.first),
                ),
                const SizedBox(height: 12),
                const Text('分类', style: TextStyle(fontSize: 13, color: Colors.grey)),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: <Widget>[
                    for (final c in _expenseCategories)
                      ActionChip(
                        label: Text(c.name),
                        onPressed: () => Navigator.pop(ctx, c),
                      ),
                  ],
                ),
                const SizedBox(height: 12),
                TextField(
                  decoration: const InputDecoration(
                    labelText: '或直接输入分类名（可选）',
                    isDense: true,
                  ),
                  onSubmitted: (v) {
                    final name = v.trim();
                    if (name.isEmpty) return;
                    final match = _expenseCategories.where((c) => c.name == name);
                    if (match.isNotEmpty) Navigator.pop(ctx, match.first);
                  },
                ),
              ],
            ),
          ),
        ),
      ),
    );

    if (category == null) return;
    picked = category.name;
    await _assign(merchant, scope, category);
    debugPrint('分类 $picked');
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('账单分类'),
        actions: <Widget>[
          if (!_loading)
            Padding(
              padding: const EdgeInsets.only(right: 12),
              child: Center(
                child: Text('${_items.length} 笔待确认',
                    style: const TextStyle(fontSize: 13, color: Colors.grey)),
              ),
            ),
        ],
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : SafeArea(
              child: ListView(
                padding: const EdgeInsets.fromLTRB(16, 12, 16, 40),
                children: <Widget>[
                  if (_error != null)
                    _errorBox(_error!),
                  if (_items.isEmpty && _error == null) _emptyBox(),
                  if (_items.isNotEmpty) ...<Widget>[
                    _hint(),
                    const SizedBox(height: 8),
                    for (final m in _merchants)
                      if (_itemsOf(m['merchant'] as String).isNotEmpty)
                        _merchantCard(m),
                  ],
                ],
              ),
            ),
    );
  }

  Widget _hint() {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: const Color(0xFF2F6F62).withValues(alpha: 0.06),
        borderRadius: BorderRadius.circular(10),
      ),
      child: const Text(
        '把同类消费一次归好，系统会记住「商户 → 分类」，下次导入自动套用。',
        style: TextStyle(fontSize: 12.5, color: Color(0xFF2F6F62)),
      ),
    );
  }

  Widget _merchantCard(Map<String, dynamic> m) {
    final merchant = m['merchant'] as String;
    final count = m['count'] as int? ?? 0;
    final amount = (m['amount_cents'] as int? ?? 0) / 100;
    final items = _itemsOf(merchant);
    final sample = items.isNotEmpty ? items.first : null;

    return Container(
      margin: const EdgeInsets.only(bottom: 10),
      decoration: cardDecoration(context),
      padding: const EdgeInsets.fromLTRB(14, 12, 12, 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(
                      merchant.isEmpty ? '(无商户)' : merchant,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w600),
                    ),
                    const SizedBox(height: 3),
                    Text(
                      '$count 笔 · ¥${amount.toStringAsFixed(2)}',
                      style: TextStyle(fontSize: 12, color: Colors.grey.shade500),
                    ),
                  ],
                ),
              ),
              FilledButton.tonal(
                onPressed: _busy ? null : () => _openCategoryPicker(merchant),
                child: const Text('归类'),
              ),
            ],
          ),
          if (sample != null && (sample['reason'] as String? ?? '').isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Text(
                'AI：${sample['reason']}（${_scopeLabel(sample['scope'] as String? ?? '')}'
                ' ${((sample['confidence'] as num? ?? 0) * 100).round()}%）',
                style: TextStyle(fontSize: 11.5, color: Colors.grey.shade500),
              ),
            ),
        ],
      ),
    );
  }

  Widget _emptyBox() {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 60),
      child: Column(
        children: <Widget>[
          const Icon(Icons.task_alt_rounded, size: 44, color: Color(0xFF2E9E6B)),
          const SizedBox(height: 12),
          const Text('全部账目都已确认', style: TextStyle(fontSize: 15)),
          const SizedBox(height: 6),
          Text('新导入的账单如果 AI 拿不准，会出现在这里',
              style: TextStyle(fontSize: 12.5, color: Colors.grey.shade500)),
        ],
      ),
    );
  }

  Widget _errorBox(String message) {
    return Container(
      margin: const EdgeInsets.only(bottom: 12),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: const Color(0xFFD64545).withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Text(message, style: const TextStyle(color: Color(0xFFD64545), fontSize: 13)),
    );
  }

  String _scopeLabel(String scope) => switch (scope) {
        'family' => '家庭',
        'personal' => '个人',
        _ => '待定',
      };
}
