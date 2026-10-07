// #33 — the New Layout assistant. The button used to say "New Layout / Report"
// and the dialog "New Layout / Presentation", and it made neither: every
// layout came out as a copy of the standard form.
//
// It now asks what kind of layout, which fields and in which order, which
// theme, and — for the kinds that need them — the break field or the label
// stock, and builds that.

import 'package:flutter/material.dart';

import '../../core/api/api_client.dart';
import 'models/layout_blueprint.dart';

/// Shows the assistant and returns what to build, or null if it was cancelled.
class NewLayoutAssistant extends StatefulWidget {
  final List<TableModel> tables;
  final TableModel? initialTable;

  const NewLayoutAssistant({super.key, required this.tables, this.initialTable});

  static Future<({LayoutBlueprint blueprint, TableModel table})?> show(
    BuildContext context, {
    required List<TableModel> tables,
    TableModel? initialTable,
  }) {
    if (tables.isEmpty) return Future.value(null);
    return showDialog<({LayoutBlueprint blueprint, TableModel table})>(
      context: context,
      builder: (_) => NewLayoutAssistant(tables: tables, initialTable: initialTable),
    );
  }

  @override
  State<NewLayoutAssistant> createState() => _NewLayoutAssistantState();
}

class _NewLayoutAssistantState extends State<NewLayoutAssistant> {
  late TableModel _table;
  final _nameCtrl = TextEditingController();
  LayoutKind _kind = LayoutKind.form;
  LayoutTheme _theme = LayoutTheme.enlightened;

  /// The fields chosen, in the order they will appear.
  final List<String> _chosen = [];

  String? _breakField;
  final List<String> _chosenSummaries = [];

  String _stockId = LabelStock.averyL7160.id;
  final _customWidthCtrl = TextEditingController(text: '90');
  final _customHeightCtrl = TextEditingController(text: '50');

  /// True once the name has been typed in, so it stops following the kind.
  bool _nameEdited = false;

  @override
  void initState() {
    super.initState();
    _table = widget.initialTable ?? widget.tables.first;
    _chooseDefaultFields();
    _nameCtrl.text = _suggestedName();
  }

  @override
  void dispose() {
    _nameCtrl.dispose();
    _customWidthCtrl.dispose();
    _customHeightCtrl.dispose();
    super.dispose();
  }

  String _suggestedName() => '${_table.displayName} ${_kind.label}';

  /// The fields a user would put on a layout: not the primary key, and not the
  /// summary fields, which go in a report's own bands.
  List<ColumnModel> get _placeable =>
      _table.columns.where((c) => !c.isPrimaryKey && c.fieldType != 'SUMMARY').toList();

  List<ColumnModel> get _summaryColumns =>
      _table.columns.where((c) => c.fieldType == 'SUMMARY').toList();

  void _chooseDefaultFields() {
    _chosen
      ..clear()
      ..addAll(_placeable.take(_kind == LayoutKind.labels ? 4 : 8).map((c) => c.name));
    _breakField = null;
    _chosenSummaries.clear();
  }

  ColumnModel? _column(String name) =>
      _table.columns.where((c) => c.name == name).firstOrNull;

  BlueprintField _blueprintField(String name) {
    final col = _column(name);
    return (
      name: name,
      label: col?.displayName ?? name,
      isSummary: col?.fieldType == 'SUMMARY',
    );
  }

  LabelStock get _stock => _stockId == 'custom'
      ? LabelStock.custom(
          widthMm: double.tryParse(_customWidthCtrl.text.trim()) ?? 90,
          heightMm: double.tryParse(_customHeightCtrl.text.trim()) ?? 50,
        )
      : LabelStock.byId(_stockId);

  /// What stops the assistant from building: the reasons are shown rather than
  /// the button silently doing nothing.
  String? get _blocker {
    if (_nameCtrl.text.trim().isEmpty) return 'Give the layout a name.';
    if (_kind.needsFields && _chosen.isEmpty) return 'Choose at least one field.';
    if (_kind.needsBreakField && _breakField == null) {
      return 'A report groups by a field. Choose the one it breaks on.';
    }
    return null;
  }

  LayoutBlueprint _build() => LayoutBlueprint(
        name: _nameCtrl.text.trim(),
        kind: _kind,
        tableOccurrence: _table.name,
        fields: [for (final name in _chosen) _blueprintField(name)],
        theme: _theme,
        breakField: _breakField,
        summaryFields: [for (final name in _chosenSummaries) _blueprintField(name)],
        stock: _kind.needsLabelStock ? _stock : null,
      );

  @override
  Widget build(BuildContext context) {
    final blocker = _blocker;

    return AlertDialog(
      title: const Row(
        children: [
          Icon(Icons.auto_awesome_mosaic, color: Color(0xFF1E88E5)),
          SizedBox(width: 8),
          Text('New Layout'),
        ],
      ),
      content: SizedBox(
        width: 720,
        height: 540,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Expanded(
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  SizedBox(width: 250, child: _kindPicker()),
                  const VerticalDivider(width: 24),
                  Expanded(child: _settings()),
                ],
              ),
            ),
            // What is missing is said here, beside the settings it is about,
            // rather than the Create button just not working.
            if (blocker != null)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Row(
                  children: [
                    Icon(Icons.info_outline, size: 15, color: Colors.orange.shade800),
                    const SizedBox(width: 6),
                    Expanded(
                      child: Text(
                        blocker,
                        key: const ValueKey('assistant-blocker'),
                        style: TextStyle(fontSize: 11.5, color: Colors.orange.shade800),
                      ),
                    ),
                  ],
                ),
              ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const ValueKey('assistant-create'),
          onPressed: blocker != null
              ? null
              : () => Navigator.of(context).pop((blueprint: _build(), table: _table)),
          child: const Text('Create Layout'),
        ),
      ],
    );
  }

  Widget _kindPicker() {
    return ListView(
      children: [
        const Text('Kind of layout:', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
        const SizedBox(height: 6),
        for (final kind in LayoutKind.values)
          Card(
            key: ValueKey('assistant-kind-${kind.name}'),
            margin: const EdgeInsets.only(bottom: 6),
            elevation: _kind == kind ? 2 : 0,
            color: _kind == kind ? const Color(0xFFE3F0FD) : null,
            child: ListTile(
              dense: true,
              selected: _kind == kind,
              title: Text(kind.label,
                  style: const TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600)),
              subtitle: Text(kind.description, style: const TextStyle(fontSize: 10.5)),
              onTap: () => setState(() {
                _kind = kind;
                if (!_nameEdited) _nameCtrl.text = _suggestedName();
                _chooseDefaultFields();
              }),
            ),
          ),
      ],
    );
  }

  Widget _settings() {
    return ListView(
      children: [
        const Text('Layout name:', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
        const SizedBox(height: 4),
        TextField(
          key: const ValueKey('assistant-name'),
          controller: _nameCtrl,
          decoration: const InputDecoration(border: OutlineInputBorder(), isDense: true),
          onChanged: (_) => setState(() => _nameEdited = true),
        ),
        const SizedBox(height: 14),

        const Text('Show records from:',
            style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
        const SizedBox(height: 4),
        DropdownButtonFormField<String>(
          key: const ValueKey('assistant-table'),
        isExpanded: true,
          value: _table.id,
          decoration: const InputDecoration(border: OutlineInputBorder(), isDense: true),
          items: [
            for (final t in widget.tables)
              DropdownMenuItem(
                value: t.id,
                child: Text('${t.displayName} (${t.name})', style: const TextStyle(fontSize: 12)),
              ),
          ],
          onChanged: (id) {
            if (id == null) return;
            setState(() {
              _table = widget.tables.firstWhere((t) => t.id == id);
              if (!_nameEdited) _nameCtrl.text = _suggestedName();
              _chooseDefaultFields();
            });
          },
        ),
        const SizedBox(height: 14),

        if (_kind.needsFields) ..._fieldPicker(),
        if (_kind.needsBreakField) ..._reportSettings(),
        if (_kind.needsLabelStock) ..._labelSettings(),

        const SizedBox(height: 14),
        const Text('Theme:', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
        const SizedBox(height: 4),
        DropdownButtonFormField<String>(
          key: const ValueKey('assistant-theme'),
        isExpanded: true,
          value: _theme.id,
          decoration: const InputDecoration(border: OutlineInputBorder(), isDense: true),
          items: [
            for (final theme in LayoutTheme.all)
              DropdownMenuItem(
                value: theme.id,
                child: Text(theme.name, style: const TextStyle(fontSize: 12)),
              ),
          ],
          onChanged: (id) => setState(() => _theme = LayoutTheme.byId(id)),
        ),
        Padding(
          padding: const EdgeInsets.only(top: 4),
          child: Text(_theme.description,
              style: const TextStyle(fontSize: 10.5, color: Colors.grey)),
        ),
      ],
    );
  }

  /// Which fields go on the layout, and in which order — the order is the
  /// column order of a list, and the line order of a label.
  List<Widget> _fieldPicker() {
    final available = _placeable.where((c) => !_chosen.contains(c.name)).toList();

    return [
      Row(
        children: [
          const Text('Fields on the layout:',
              style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
          const Spacer(),
          TextButton(
            onPressed: _chosen.length == _placeable.length
                ? null
                : () => setState(() {
                      _chosen
                        ..clear()
                        ..addAll(_placeable.map((c) => c.name));
                    }),
            child: const Text('All', style: TextStyle(fontSize: 11)),
          ),
          TextButton(
            onPressed: _chosen.isEmpty ? null : () => setState(_chosen.clear),
            child: const Text('None', style: TextStyle(fontSize: 11)),
          ),
        ],
      ),
      const Text('In the order they will appear.',
          style: TextStyle(fontSize: 10.5, color: Colors.grey)),
      const SizedBox(height: 4),
      Container(
        height: 150,
        decoration: BoxDecoration(
          border: Border.all(color: Colors.grey.shade400),
          borderRadius: BorderRadius.circular(4),
        ),
        child: _chosen.isEmpty
            ? const Center(
                child: Text('No fields chosen', style: TextStyle(fontSize: 11, color: Colors.grey)))
            : ReorderableListView.builder(
                key: const ValueKey('assistant-chosen'),
                buildDefaultDragHandles: false,
                itemCount: _chosen.length,
                onReorder: (from, to) => setState(() {
                  if (to > from) to -= 1;
                  _chosen.insert(to, _chosen.removeAt(from));
                }),
                itemBuilder: (ctx, i) {
                  final name = _chosen[i];
                  final col = _column(name);
                  return ListTile(
                    key: ValueKey('assistant-field-$name'),
                    dense: true,
                    visualDensity: VisualDensity.compact,
                    leading: ReorderableDragStartListener(
                      index: i,
                      child: const Icon(Icons.drag_handle, size: 16),
                    ),
                    title: Text('${col?.displayName ?? name}  (${col?.fieldType ?? '?'})',
                        style: const TextStyle(fontSize: 11.5)),
                    trailing: IconButton(
                      icon: const Icon(Icons.remove_circle_outline, size: 16),
                      tooltip: 'Take this field off the layout',
                      onPressed: () => setState(() => _chosen.remove(name)),
                    ),
                  );
                },
              ),
      ),
      if (available.isNotEmpty) ...[
        const SizedBox(height: 6),
        Wrap(
          spacing: 5,
          runSpacing: 4,
          children: [
            for (final col in available)
              ActionChip(
                key: ValueKey('assistant-add-${col.name}'),
                avatar: const Icon(Icons.add, size: 13),
                label: Text(col.displayName, style: const TextStyle(fontSize: 11)),
                onPressed: () => setState(() => _chosen.add(col.name)),
              ),
          ],
        ),
      ],
    ];
  }

  List<Widget> _reportSettings() {
    final summaries = _summaryColumns;
    return [
      const SizedBox(height: 14),
      const Text('Group the records by:',
          style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
      const SizedBox(height: 4),
      DropdownButtonFormField<String>(
        key: const ValueKey('assistant-break-field'),
        isExpanded: true,
        value: _breakField,
        decoration: const InputDecoration(border: OutlineInputBorder(), isDense: true),
        hint: const Text('Choose a field', style: TextStyle(fontSize: 12)),
        items: [
          for (final col in _placeable)
            DropdownMenuItem(
              value: col.name,
              child: Text('${col.displayName} (${col.fieldType})',
                  style: const TextStyle(fontSize: 12)),
            ),
        ],
        onChanged: (v) => setState(() => _breakField = v),
      ),
      const Padding(
        padding: EdgeInsets.only(top: 4),
        child: Text(
          'The report breaks into a group whenever this field changes, and the found set '
          'has to be sorted by it. The report offers to sort itself.',
          style: TextStyle(fontSize: 10.5, color: Colors.grey),
        ),
      ),
      const SizedBox(height: 14),
      const Text('Subtotal with:', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
      const SizedBox(height: 4),
      if (summaries.isEmpty)
        Text(
          'This table has no summary fields yet. Add one in Manage Database — a Summary '
          'field that totals a number — and the report will subtotal each group and the lot.',
          style: TextStyle(fontSize: 10.5, color: Colors.orange.shade800),
        )
      else
        Wrap(
          spacing: 5,
          children: [
            for (final col in summaries)
              FilterChip(
                key: ValueKey('assistant-summary-${col.name}'),
                label: Text(col.displayName, style: const TextStyle(fontSize: 11)),
                selected: _chosenSummaries.contains(col.name),
                onSelected: (on) => setState(() {
                  on ? _chosenSummaries.add(col.name) : _chosenSummaries.remove(col.name);
                }),
              ),
          ],
        ),
    ];
  }

  List<Widget> _labelSettings() {
    final stock = _stock;
    return [
      const SizedBox(height: 14),
      const Text('Label stock:', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
      const SizedBox(height: 4),
      DropdownButtonFormField<String>(
        key: const ValueKey('assistant-stock'),
        value: _stockId,
        decoration: const InputDecoration(border: OutlineInputBorder(), isDense: true),
        isExpanded: true,
        items: [
          for (final s in LabelStock.all)
            DropdownMenuItem(
              value: s.id,
              child: Text(s.name,
                  style: const TextStyle(fontSize: 11.5), overflow: TextOverflow.ellipsis),
            ),
          const DropdownMenuItem(
            value: 'custom',
            child: Text('Custom size…', style: TextStyle(fontSize: 11.5)),
          ),
        ],
        onChanged: (v) => setState(() => _stockId = v ?? LabelStock.averyL7160.id),
      ),
      if (_stockId == 'custom') ...[
        const SizedBox(height: 8),
        Row(
          children: [
            Expanded(
              child: TextField(
                key: const ValueKey('assistant-stock-width'),
                controller: _customWidthCtrl,
                keyboardType: TextInputType.number,
                decoration: const InputDecoration(
                    labelText: 'Width (mm)', border: OutlineInputBorder(), isDense: true),
                style: const TextStyle(fontSize: 12),
                onChanged: (_) => setState(() {}),
              ),
            ),
            const SizedBox(width: 8),
            Expanded(
              child: TextField(
                key: const ValueKey('assistant-stock-height'),
                controller: _customHeightCtrl,
                keyboardType: TextInputType.number,
                decoration: const InputDecoration(
                    labelText: 'Height (mm)', border: OutlineInputBorder(), isDense: true),
                style: const TextStyle(fontSize: 12),
                onChanged: (_) => setState(() {}),
              ),
            ),
          ],
        ),
      ],
      Padding(
        padding: const EdgeInsets.only(top: 6),
        child: Text(
          '${stock.sizeLabel} — ${stock.perSheet} to a sheet. '
          'The layout is one label; Preview lays them out on the page. A field that is '
          'empty takes its line with it, so a short address does not print a blank line.',
          style: const TextStyle(fontSize: 10.5, color: Colors.grey),
        ),
      ),
    ];
  }
}
