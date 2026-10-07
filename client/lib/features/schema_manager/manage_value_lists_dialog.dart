import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../main.dart' show apiClientProvider;

/// File > Manage > Value Lists (#31).
///
/// A value list is a named set of values a field can be filled from, either
/// typed in here or taken from what a field already holds. It decides what a
/// field *offers*; restricting what may be stored is the "existing value"
/// validation rule in Field Options.
class ManageValueListsDialog extends ConsumerStatefulWidget {
  const ManageValueListsDialog({super.key});

  static Future<void> show(BuildContext context) {
    return showDialog(
      context: context,
      builder: (_) => const ManageValueListsDialog(),
    );
  }

  @override
  ConsumerState<ManageValueListsDialog> createState() => _ManageValueListsDialogState();
}

class _ManageValueListsDialogState extends ConsumerState<ManageValueListsDialog> {
  List<ValueListModel> _lists = [];
  List<TableModel> _tables = [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final client = ref.read(apiClientProvider);
      final results = await Future.wait([
        client.listValueLists(),
        client.listTables().catchError((_) => <TableModel>[]),
      ]);
      if (!mounted) return;
      setState(() {
        _lists = results[0] as List<ValueListModel>;
        _tables = results[1] as List<TableModel>;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = '$e';
        _loading = false;
      });
    }
  }

  Future<void> _edit({ValueListModel? existing}) async {
    final saved = await showDialog<ValueListModel>(
      context: context,
      builder: (_) => _ValueListEditor(existing: existing, tables: _tables),
    );
    if (saved == null) return;

    try {
      final client = ref.read(apiClientProvider);
      if (existing == null) {
        await client.createValueList(saved);
      } else {
        await client.updateValueList(saved);
      }
      await _load();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Could not save the value list: $e')),
      );
    }
  }

  Future<void> _delete(ValueListModel list) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete value list'),
        content: Text(
          'Delete "${list.name}"?\n\n'
          'Fields that offered this list go back to a plain edit box. '
          'No record data is changed.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Colors.red.shade700),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    try {
      await ref.read(apiClientProvider).deleteValueList(list.id);
      await _load();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Could not delete the value list: $e')),
      );
    }
  }

  String _sourceLabel(ValueListModel list) {
    for (final table in _tables) {
      for (final col in table.columns) {
        if (col.id == list.sourceColumnId) {
          return '${table.displayName} · ${col.displayName}';
        }
      }
    }
    return 'a field that is no longer there';
  }

  @override
  Widget build(BuildContext context) {
    return Dialog(
      child: SizedBox(
        width: 720,
        height: 540,
        child: Column(
          children: [
            ListTile(
              leading: const Icon(Icons.list_alt_outlined),
              title: const Text('Manage Value Lists', style: TextStyle(fontWeight: FontWeight.bold)),
              subtitle: const Text(
                'Named sets of values a field can be filled from',
                style: TextStyle(fontSize: 12),
              ),
              trailing: IconButton(
                icon: const Icon(Icons.close),
                onPressed: () => Navigator.of(context).pop(),
              ),
            ),
            const Divider(height: 1),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
              child: Row(
                children: [
                  Text('${_lists.length} ${_lists.length == 1 ? 'value list' : 'value lists'}',
                      style: const TextStyle(fontSize: 12, color: Colors.grey)),
                  const Spacer(),
                  FilledButton.icon(
                    icon: const Icon(Icons.add, size: 16),
                    label: const Text('New Value List...'),
                    onPressed: () => _edit(),
                  ),
                ],
              ),
            ),
            const Divider(height: 1),
            Expanded(child: _buildBody()),
            const Divider(height: 1),
            Padding(
              padding: const EdgeInsets.all(12),
              child: Row(
                children: [
                  const Expanded(
                    child: Text(
                      'A value list decides what a field offers. To refuse anything else, '
                      'add an "existing value" rule in Field Options.',
                      style: TextStyle(fontSize: 11, color: Colors.grey),
                    ),
                  ),
                  TextButton(
                    onPressed: () => Navigator.of(context).pop(),
                    child: const Text('Close'),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildBody() {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error != null) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text('Could not read the value lists: $_error', style: const TextStyle(fontSize: 12)),
            const SizedBox(height: 8),
            OutlinedButton(onPressed: _load, child: const Text('Retry')),
          ],
        ),
      );
    }
    if (_lists.isEmpty) {
      return const Center(
        child: Padding(
          padding: EdgeInsets.all(32),
          child: Text(
            'No value lists yet.\n\n'
            'A value list lets a field be filled from a pop-up, a drop-down, '
            'checkboxes or radio buttons instead of being typed into.',
            textAlign: TextAlign.center,
            style: TextStyle(fontSize: 12, color: Colors.grey),
          ),
        ),
      );
    }

    return ListView.separated(
      itemCount: _lists.length,
      separatorBuilder: (_, _) => const Divider(height: 1),
      itemBuilder: (_, i) {
        final list = _lists[i];
        final summary = list.isFromField
            ? 'From the values of ${_sourceLabel(list)}'
            : list.customValueList.join(' · ');
        return ListTile(
          key: ValueKey('value-list-${list.id}'),
          leading: Icon(list.isFromField ? Icons.dynamic_feed_outlined : Icons.format_list_bulleted),
          title: Text(list.name),
          subtitle: Text(
            summary,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: const TextStyle(fontSize: 12),
          ),
          trailing: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              IconButton(
                tooltip: 'Edit',
                icon: const Icon(Icons.edit_outlined, size: 18),
                onPressed: () => _edit(existing: list),
              ),
              IconButton(
                tooltip: 'Delete',
                icon: Icon(Icons.delete_outline, size: 18, color: Colors.red.shade700),
                onPressed: () => _delete(list),
              ),
            ],
          ),
          onTap: () => _edit(existing: list),
        );
      },
    );
  }
}

/// Creating or changing one value list.
class _ValueListEditor extends StatefulWidget {
  final ValueListModel? existing;
  final List<TableModel> tables;

  const _ValueListEditor({required this.existing, required this.tables});

  @override
  State<_ValueListEditor> createState() => _ValueListEditorState();
}

class _ValueListEditorState extends State<_ValueListEditor> {
  late final TextEditingController _nameController;
  late final TextEditingController _valuesController;
  late String _kind;
  String? _sourceTableId;
  String? _sourceColumnId;
  String? _error;

  @override
  void initState() {
    super.initState();
    final existing = widget.existing;
    _nameController = TextEditingController(text: existing?.name ?? '');
    _valuesController = TextEditingController(text: existing?.customValues ?? '');
    _kind = existing?.kind ?? ValueListModel.kindCustom;
    _sourceTableId = existing?.sourceTableId;
    _sourceColumnId = existing?.sourceColumnId;
  }

  @override
  void dispose() {
    _nameController.dispose();
    _valuesController.dispose();
    super.dispose();
  }

  List<ColumnModel> get _sourceColumns {
    final table = widget.tables.where((t) => t.id == _sourceTableId).firstOrNull;
    return table?.columns.where((c) => !c.isPrimaryKey).toList() ?? const [];
  }

  void _save() {
    final name = _nameController.text.trim();
    if (name.isEmpty) {
      setState(() => _error = 'Give the value list a name.');
      return;
    }
    if (_kind == ValueListModel.kindCustom && _valuesController.text.trim().isEmpty) {
      setState(() => _error = 'Type at least one value, one per line.');
      return;
    }
    if (_kind == ValueListModel.kindFromField && (_sourceTableId == null || _sourceColumnId == null)) {
      setState(() => _error = 'Choose the table and the field to take the values from.');
      return;
    }

    Navigator.of(context).pop(ValueListModel(
      id: widget.existing?.id ?? '',
      name: name,
      kind: _kind,
      customValues: _kind == ValueListModel.kindCustom ? _valuesController.text : '',
      sourceTableId: _kind == ValueListModel.kindFromField ? _sourceTableId : null,
      sourceColumnId: _kind == ValueListModel.kindFromField ? _sourceColumnId : null,
    ));
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(widget.existing == null ? 'New Value List' : 'Edit Value List'),
      content: SizedBox(
        width: 460,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              TextField(
                controller: _nameController,
                autofocus: true,
                decoration: const InputDecoration(
                  labelText: 'Value List Name',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
              ),
              const SizedBox(height: 16),
              SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: ValueListModel.kindCustom, label: Text('Custom values')),
                  ButtonSegment(value: ValueListModel.kindFromField, label: Text('From a field')),
                ],
                selected: {_kind},
                onSelectionChanged: (v) => setState(() => _kind = v.first),
              ),
              const SizedBox(height: 16),
              if (_kind == ValueListModel.kindCustom) ...[
                const Text('One value per line.', style: TextStyle(fontSize: 12, color: Colors.grey)),
                const SizedBox(height: 8),
                TextField(
                  key: const ValueKey('value-list-custom-values'),
                  controller: _valuesController,
                  minLines: 6,
                  maxLines: 12,
                  decoration: const InputDecoration(
                    border: OutlineInputBorder(),
                    hintText: 'New\nContinuing',
                  ),
                ),
              ] else ...[
                const Text(
                  'The list offers the values this field already holds, so it grows with the data.',
                  style: TextStyle(fontSize: 12, color: Colors.grey),
                ),
                const SizedBox(height: 12),
                DropdownButtonFormField<String>(
                  key: const ValueKey('value-list-source-table'),
                  initialValue: _sourceTableId,
                  decoration: const InputDecoration(
                      labelText: 'Table', border: OutlineInputBorder(), isDense: true),
                  items: widget.tables
                      .map((t) => DropdownMenuItem(value: t.id, child: Text(t.displayName)))
                      .toList(),
                  onChanged: (v) => setState(() {
                    _sourceTableId = v;
                    _sourceColumnId = null;
                  }),
                ),
                const SizedBox(height: 12),
                DropdownButtonFormField<String>(
                  key: ValueKey('value-list-source-column-$_sourceTableId'),
                  initialValue: _sourceColumnId,
                  decoration: const InputDecoration(
                      labelText: 'Field', border: OutlineInputBorder(), isDense: true),
                  items: _sourceColumns
                      .map((c) => DropdownMenuItem(value: c.id, child: Text(c.displayName)))
                      .toList(),
                  onChanged: _sourceTableId == null ? null : (v) => setState(() => _sourceColumnId = v),
                ),
              ],
              if (_error != null) ...[
                const SizedBox(height: 12),
                Row(
                  children: [
                    const Icon(Icons.error_outline, size: 16, color: Colors.red),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(_error!, style: const TextStyle(fontSize: 12, color: Colors.red)),
                    ),
                  ],
                ),
              ],
            ],
          ),
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(onPressed: _save, child: const Text('Save')),
      ],
    );
  }
}
