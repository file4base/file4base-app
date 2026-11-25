import 'dart:async';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/external_access.dart';
import 'package:file4base_client/main.dart' show apiClientProvider;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

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
  List<TableOccurrenceModel> _occurrences = [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
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
        client.listOccurrences().catchError((_) => <TableOccurrenceModel>[]),
      ]);
      if (!mounted) return;
      setState(() {
        _lists = results[0] as List<ValueListModel>;
        _tables = results[1] as List<TableModel>;
        _occurrences = results[2] as List<TableOccurrenceModel>;
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
      builder: (_) => _ValueListEditor(
        existing: existing,
        tables: _tables,
        occurrences: _occurrences,
        apiClient: ref.read(apiClientProvider),
      ),
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
    final through = _occurrences.where((o) => o.id == list.sourceOccurrenceId).firstOrNull;
    if (list.sourceColumnName != null && through != null && through.isExternal) {
      var label = '${through.name} · ${list.sourceColumnName} (${through.external!.database.isEmpty ? through.external!.fileReference : through.external!.database})';
      final second = list.secondColumnName;
      if (second != null) label += list.showSecondOnly ? ', showing $second' : ' and $second';
      final related = _occurrences.where((o) => o.id == list.relatedOccurrenceId).firstOrNull;
      if (related != null) label += ', related from ${related.name}';
      return label;
    }
    for (final table in _tables) {
      for (final col in table.columns) {
        if (col.id == list.sourceColumnId) {
          var label = '${table.displayName} · ${col.displayName}';
          final second = table.columns.where((c) => c.id == list.secondColumnId).firstOrNull;
          if (second != null) label += list.showSecondOnly ? ', showing ${second.displayName}' : ' and ${second.displayName}';
          final related = _occurrences.where((o) => o.id == list.relatedOccurrenceId).firstOrNull;
          if (related != null) label += ', related from ${related.name}';
          return label;
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
                    onPressed: _edit,
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

  const _ValueListEditor({required this.existing, required this.tables, this.occurrences = const [], this.apiClient});
  final ValueListModel? existing;
  final List<TableModel> tables;
  final List<TableOccurrenceModel> occurrences;

  /// Reads the fields of another solution's table, for a list taken from
  /// one through an external occurrence (#107).
  final ApiClient? apiClient;

  @override
  State<_ValueListEditor> createState() => _ValueListEditorState();
}

class _ValueListEditorState extends State<_ValueListEditor> {
  late final TextEditingController _nameController;
  late final TextEditingController _valuesController;
  late String _kind;
  String? _sourceTableId;
  String? _sourceColumnId;
  bool _useSecond = false;
  String? _secondColumnId;
  bool _showSecondOnly = false;
  String _sortBy = ValueListModel.sortFirst;
  bool _related = false;
  String? _relatedOccurrenceId;
  String? _sourceOccurrenceId;
  String? _error;

  /// The source of a list taken from a field of another solution's table:
  /// `occ:` and the external occurrence it is read through. Its fields are
  /// then named, and [_sourceColumnId] and [_secondColumnId] hold names.
  static const _externalPrefix = 'occ:';
  TableModel? _externalTable;
  ExternalUnavailable? _externalProblem;
  String? _externalError;

  TableOccurrenceModel? get _externalOccurrence {
    final key = _sourceTableId;
    if (key == null || !key.startsWith(_externalPrefix)) return null;
    final id = key.substring(_externalPrefix.length);
    return widget.occurrences.where((o) => o.id == id).firstOrNull;
  }

  bool get _isExternal => _sourceTableId?.startsWith(_externalPrefix) ?? false;

  String _columnKey(ColumnModel c) => _isExternal ? c.name : c.id;

  /// The entries of a field list whose stored value is [stored]. A field of
  /// another solution's table the migration knew only by its FileMaker id
  /// (`#<id>`) is the field it resolves to, and keeps its stored value, so
  /// saving the list unchanged changes nothing; one no field answers is
  /// listed as it is, unresolved, never replaced by another field.
  List<DropdownMenuItem<String>> _fieldItems(String? stored) {
    final columns = _sourceColumns;
    final resolved = _isExternal ? ColumnModel.externalField(columns, stored) : null;
    final items = [
      for (final c in columns)
        DropdownMenuItem(
          value: resolved != null && c.name == resolved.name ? stored! : _columnKey(c),
          child: Text(c.displayName),
        ),
    ];
    if (_isExternal && stored != null && !items.any((i) => i.value == stored)) {
      items.add(DropdownMenuItem(
        value: stored,
        child: Text(
          _externalTable == null ? stored : ColumnModel.unresolvedLabel(stored),
          style: const TextStyle(fontStyle: FontStyle.italic),
        ),
      ));
    }
    return items;
  }

  void _readExternalTable() {
    final occ = _externalOccurrence;
    final client = widget.apiClient;
    _externalTable = null;
    _externalProblem = null;
    _externalError = null;
    if (occ == null) return;
    final external = occ.external!;
    if (client == null || external.database.isEmpty || external.table.isEmpty) {
      _externalError = 'The fields of "${occ.name}" are not known: its file reference "${external.fileReference}" '
          'is not bound yet. Bind it in File > Manage > File References.';
      return;
    }
    unawaited(ExternalAccess.of(client).table(occ).then((table) {
      if (mounted && _externalOccurrence?.id == occ.id) setState(() => _externalTable = table);
    }, onError: (Object e) {
      if (!mounted || _externalOccurrence?.id != occ.id) return;
      setState(() {
        _externalProblem = ExternalUnavailable.of(e);
        if (_externalProblem == null) {
          _externalError = 'The fields of "${occ.name}" could not be read in ${external.database}: '
              '${e is ApiException ? e.detail : e}';
        }
      });
    }));
  }

  @override
  void initState() {
    super.initState();
    final existing = widget.existing;
    _nameController = TextEditingController(text: existing?.name ?? '');
    _valuesController = TextEditingController(text: existing?.customValues ?? '');
    _kind = existing?.kind ?? ValueListModel.kindCustom;
    _sourceTableId = existing?.sourceTableId;
    _sourceColumnId = existing?.sourceColumnId;
    _secondColumnId = existing?.secondColumnId;
    _useSecond = _secondColumnId != null;
    _showSecondOnly = existing?.showSecondOnly ?? false;
    _sortBy = existing?.sortBy ?? ValueListModel.sortFirst;
    _relatedOccurrenceId = existing?.relatedOccurrenceId;
    _sourceOccurrenceId = existing?.sourceOccurrenceId;
    _related = _relatedOccurrenceId != null;
    if (existing != null && existing.sourceTableId == null && existing.sourceColumnName != null) {
      _sourceTableId = '$_externalPrefix${existing.sourceOccurrenceId}';
      _sourceColumnId = existing.sourceColumnName;
      _secondColumnId = existing.secondColumnName;
      _useSecond = _secondColumnId != null;
      _readExternalTable();
    }
  }

  @override
  void dispose() {
    _nameController.dispose();
    _valuesController.dispose();
    super.dispose();
  }

  List<ColumnModel> get _sourceColumns {
    final table = _isExternal ? _externalTable : widget.tables.where((t) => t.id == _sourceTableId).firstOrNull;
    return table?.columns.where((c) => !c.isPrimaryKey).toList() ?? const [];
  }

  /// The occurrences of the list's table, one of which the related values are
  /// read through: for another solution's table, the external occurrence
  /// the list is taken through.
  List<TableOccurrenceModel> get _sourceOccurrences {
    final external = _externalOccurrence;
    if (external != null) return [external];
    return widget.occurrences.where((o) => o.baseTableId == _sourceTableId).toList();
  }

  /// The occurrences related values can start from: any of this solution's,
  /// the ones a layout shows records of. One of another solution's table
  /// shows that solution's records, which nothing here relates from.
  List<TableOccurrenceModel> get _startOccurrences =>
      _isExternal ? widget.occurrences.where((o) => !o.isExternal).toList() : widget.occurrences;

  void _chooseTable(String? id) {
    _sourceTableId = id;
    _sourceColumnId = null;
    _secondColumnId = null;
    final through = _sourceOccurrences;
    _sourceOccurrenceId = through.length == 1 ? through.single.id : null;
    _readExternalTable();
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
    final fromField = _kind == ValueListModel.kindFromField;
    final second = fromField && _useSecond ? _secondColumnId : null;
    if (fromField && _useSecond && second == null) {
      setState(() => _error = 'Choose the second field, or stop displaying one.');
      return;
    }
    if (fromField) {
      for (final id in [_sourceColumnId, second]) {
        final column = _sourceColumns.where((c) => c.id == id).firstOrNull;
        final why = column?.unindexableReason;
        if (why != null) {
          setState(() => _error =
              '"${column!.displayName}" cannot be indexed, so a value list cannot take its values from it: $why.');
          return;
        }
      }
    }
    final related = fromField && _related;
    if (related && (_relatedOccurrenceId == null || _sourceOccurrenceId == null)) {
      setState(() => _error = 'Choose the occurrence the related values start from, and the one they are read through.');
      return;
    }

    final external = fromField ? _externalOccurrence : null;
    if (external != null) {
      Navigator.of(context).pop(ValueListModel(
        id: widget.existing?.id ?? '',
        name: name,
        kind: _kind,
        sourceOccurrenceId: external.id,
        sourceColumnName: _sourceColumnId,
        secondColumnName: second,
        showSecondOnly: second != null && _showSecondOnly,
        sortBy: second != null ? _sortBy : ValueListModel.sortFirst,
        relatedOccurrenceId: related ? _relatedOccurrenceId : null,
      ));
      return;
    }

    Navigator.of(context).pop(ValueListModel(
      id: widget.existing?.id ?? '',
      name: name,
      kind: _kind,
      customValues: _kind == ValueListModel.kindCustom ? _valuesController.text : '',
      sourceTableId: fromField ? _sourceTableId : null,
      sourceColumnId: fromField ? _sourceColumnId : null,
      secondColumnId: second,
      showSecondOnly: second != null && _showSecondOnly,
      sortBy: second != null ? _sortBy : ValueListModel.sortFirst,
      relatedOccurrenceId: related ? _relatedOccurrenceId : null,
      sourceOccurrenceId: related ? _sourceOccurrenceId : null,
    ));
  }

  /// Why the fields of another solution's table are not listed, and, when
  /// the session is only not signed in there, a way to sign in.
  List<Widget> _externalNotice() {
    final problem = _externalProblem;
    final message = problem?.message ?? _externalError;
    if (!_isExternal || message == null) return const [];
    return [
      const SizedBox(height: 8),
      Text(message, key: const ValueKey('value-list-external-problem'), style: const TextStyle(fontSize: 12)),
      if (problem != null && problem.isNotSignedIn && widget.apiClient != null)
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton(
            key: const ValueKey('value-list-external-sign-in'),
            onPressed: () async {
              if (await ExternalAccess.of(widget.apiClient!).signIn(context, problem) && mounted) {
                setState(_readExternalTable);
              }
            },
            child: Text('Sign in to ${problem.database}…'),
          ),
        ),
    ];
  }

  /// FileMaker's Specify Fields for Value List options: a second field, the
  /// sort, and including only related values.
  List<Widget> _fieldOptions() {
    const small = TextStyle(fontSize: 13);
    final hasSecond = _useSecond && _secondColumnId != null;
    return [
      const SizedBox(height: 8),
      CheckboxListTile(
        key: const ValueKey('value-list-use-second'),
        contentPadding: EdgeInsets.zero,
        dense: true,
        controlAffinity: ListTileControlAffinity.leading,
        value: _useSecond,
        onChanged: _sourceTableId == null ? null : (v) => setState(() => _useSecond = v ?? false),
        title: const Text('Also display values from a second field', style: small),
      ),
      if (_useSecond) ...[
        DropdownButtonFormField<String>(
          key: ValueKey('value-list-second-column-$_sourceTableId'),
          initialValue: _secondColumnId,
          decoration: const InputDecoration(labelText: 'Second field', border: OutlineInputBorder(), isDense: true),
          items: _fieldItems(_secondColumnId),
          onChanged: (v) => setState(() => _secondColumnId = v),
        ),
        CheckboxListTile(
          key: const ValueKey('value-list-show-second-only'),
          contentPadding: EdgeInsets.zero,
          dense: true,
          controlAffinity: ListTileControlAffinity.leading,
          value: _showSecondOnly,
          onChanged: (v) => setState(() => _showSecondOnly = v ?? false),
          title: const Text('Show values only from the second field', style: small),
          subtitle: const Text("Choosing one still stores the first field's value.",
              style: TextStyle(fontSize: 11, color: Colors.grey)),
        ),
      ],
      const SizedBox(height: 8),
      const Text('Sort values using', style: small),
      const SizedBox(height: 8),
      SegmentedButton<String>(
        key: const ValueKey('value-list-sort-by'),
        segments: [
          const ButtonSegment(value: ValueListModel.sortFirst, label: Text('First field')),
          ButtonSegment(value: ValueListModel.sortSecond, label: const Text('Second field'), enabled: hasSecond),
        ],
        selected: {if (hasSecond) _sortBy else ValueListModel.sortFirst},
        onSelectionChanged: (v) => setState(() => _sortBy = v.first),
      ),
      const SizedBox(height: 8),
      CheckboxListTile(
        key: const ValueKey('value-list-related'),
        contentPadding: EdgeInsets.zero,
        dense: true,
        controlAffinity: ListTileControlAffinity.leading,
        value: _related,
        onChanged: _sourceTableId == null
            ? null
            : (v) => setState(() {
                  _related = v ?? false;
                  final through = _sourceOccurrences;
                  if (_sourceOccurrenceId == null && through.length == 1) _sourceOccurrenceId = through.single.id;
                }),
        title: const Text('Include only related values starting from', style: small),
      ),
      if (_related) ...[
        DropdownButtonFormField<String>(
          key: const ValueKey('value-list-related-occurrence'),
          initialValue: _relatedOccurrenceId,
          decoration: const InputDecoration(
              labelText: 'Starting from occurrence', border: OutlineInputBorder(), isDense: true),
          items: _startOccurrences
              .map((o) => DropdownMenuItem(value: o.id, child: Text(o.name)))
              .toList(),
          onChanged: (v) => setState(() => _relatedOccurrenceId = v),
        ),
        const SizedBox(height: 12),
        if (!_isExternal)
        DropdownButtonFormField<String>(
          key: ValueKey('value-list-source-occurrence-$_sourceTableId'),
          initialValue: _sourceOccurrenceId,
          decoration: const InputDecoration(
              labelText: 'Values read through occurrence', border: OutlineInputBorder(), isDense: true),
          items: _sourceOccurrences
              .map((o) => DropdownMenuItem(value: o.id, child: Text(o.name)))
              .toList(),
          onChanged: (v) => setState(() => _sourceOccurrenceId = v),
        ),
      ],
    ];
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
                  items: [
                    for (final t in widget.tables) DropdownMenuItem(value: t.id, child: Text(t.displayName)),
                    // A table of another solution, through the external
                    // occurrence the list reads it with (#107).
                    for (final o in widget.occurrences.where((o) => o.isExternal))
                      DropdownMenuItem(
                        value: '$_externalPrefix${o.id}',
                        child: Text(
                          '${o.name} (${o.external!.database.isEmpty ? o.external!.fileReference : o.external!.database})',
                          style: const TextStyle(fontStyle: FontStyle.italic),
                        ),
                      ),
                  ],
                  onChanged: (v) => setState(() => _chooseTable(v)),
                ),
                const SizedBox(height: 12),
                DropdownButtonFormField<String>(
                  key: ValueKey('value-list-source-column-$_sourceTableId'),
                  initialValue: _sourceColumnId,
                  decoration: const InputDecoration(
                      labelText: 'Field', border: OutlineInputBorder(), isDense: true),
                  items: _fieldItems(_sourceColumnId),
                  onChanged: _sourceTableId == null ? null : (v) => setState(() => _sourceColumnId = v),
                ),
                ..._externalNotice(),
                ..._fieldOptions(),
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
