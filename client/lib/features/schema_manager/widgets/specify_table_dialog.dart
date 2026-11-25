import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/external_access.dart';
import 'package:file4base_client/features/schema_manager/manage_file_references_dialog.dart';
import 'package:flutter/material.dart';

/// Specify Table: the table a new occurrence shows, from this solution or,
/// through a file reference, from another solution of the server (#107).
///
/// It answers `{base_table_id, name}` for a table of this solution and
/// `{external: {file_reference, table}, name}` for one of another solution.
class SpecifyTableDialog extends StatefulWidget {

  const SpecifyTableDialog({
    required this.tables, required this.existingOccurrences, super.key,
    this.activeDatabaseName = 'Current file',
    this.initialOccurrence,
    this.apiClient,
  });
  final List<TableModel> tables;
  final List<TableOccurrenceModel> existingOccurrences;
  final String activeDatabaseName;
  final TableOccurrenceModel? initialOccurrence;

  /// Lists the file references and, through the session this one holds in
  /// the solution one is mapped to, that solution's tables. Without it only
  /// this solution's tables are offered.
  final ApiClient? apiClient;

  static Future<Map<String, dynamic>?> show(
    BuildContext context, {
    required List<TableModel> tables,
    required List<TableOccurrenceModel> existingOccurrences,
    String activeDatabaseName = 'Current file',
    TableOccurrenceModel? initialOccurrence,
    ApiClient? apiClient,
  }) {
    return showDialog<Map<String, dynamic>>(
      context: context,
      builder: (ctx) => SpecifyTableDialog(
        tables: tables,
        existingOccurrences: existingOccurrences,
        activeDatabaseName: activeDatabaseName,
        initialOccurrence: initialOccurrence,
        apiClient: apiClient,
      ),
    );
  }

  @override
  State<SpecifyTableDialog> createState() => _SpecifyTableDialogState();
}

class _SpecifyTableDialogState extends State<SpecifyTableDialog> {
  TableModel? _selectedTable;
  late final TextEditingController _nameController;

  /// The data source: [_local], a file reference's id, or [_addReference]
  /// while the dialog to add one is open.
  String _selectedDataSource = _local;
  static const _local = '';
  static const _addReference = '+';

  List<FileReferenceModel> _references = const [];

  /// The tables of the selected file reference's solution, or why they are
  /// not listed.
  List<TableModel>? _externalTables;
  bool _loadingExternal = false;
  String? _externalProblem;
  bool _needsSignIn = false;

  FileReferenceModel? get _reference => _references.where((r) => r.id == _selectedDataSource).firstOrNull;

  List<TableModel> get _listedTables => _selectedDataSource == _local ? widget.tables : (_externalTables ?? const []);

  @override
  void initState() {
    super.initState();
    if (widget.apiClient != null && widget.initialOccurrence == null) unawaited(_loadReferences());
    if (widget.initialOccurrence != null) {
      _selectedTable = widget.tables.where((t) => t.id == widget.initialOccurrence!.baseTableId).firstOrNull ??
          (widget.tables.isNotEmpty ? widget.tables.first : null);
      _nameController = TextEditingController(text: widget.initialOccurrence!.name);
    } else {
      _selectedTable = widget.tables.isNotEmpty ? widget.tables.first : null;
      final defaultName = _selectedTable != null ? _suggestOccurrenceName(_selectedTable!) : '';
      _nameController = TextEditingController(text: defaultName);
    }
  }

  @override
  void dispose() {
    _nameController.dispose();
    super.dispose();
  }

  String _suggestOccurrenceName(TableModel table) {
    final baseName = table.displayName.trim().isNotEmpty ? table.displayName.trim() : table.name;
    final existsExact = widget.existingOccurrences.any(
      (o) => o.name.toLowerCase() == baseName.toLowerCase(),
    );
    if (!existsExact) {
      return baseName;
    }
    var count = 2;
    while (widget.existingOccurrences.any((o) => o.name.toLowerCase() == '$baseName $count'.toLowerCase())) {
      count++;
    }
    return '$baseName $count';
  }

  Future<void> _loadReferences() async {
    try {
      final refs = await widget.apiClient!.listFileReferences();
      if (mounted) setState(() => _references = refs..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase())));
    } catch (_) {
      // Only this solution's tables are offered; the list says so.
    }
  }

  Future<void> _chooseDataSource(String? value) async {
    if (value == null) return;
    if (value == _addReference) {
      final created = await FileReferenceEditor.show(context, apiClient: widget.apiClient!);
      if (!mounted) return;
      if (created == null) return;
      setState(() => _references = [..._references, created]);
      value = created.id;
    }
    setState(() {
      _selectedDataSource = value!;
      _selectedTable = null;
      _externalTables = null;
      _externalProblem = null;
      _needsSignIn = false;
      if (value == _local) {
        _selectedTable = widget.tables.firstOrNull;
        if (_selectedTable != null) _nameController.text = _suggestOccurrenceName(_selectedTable!);
      }
    });
    if (value != _local) await _loadExternalTables();
  }

  /// Lists the tables of the selected reference's solution, as the account
  /// this session holds there.
  Future<void> _loadExternalTables() async {
    final ref = _reference;
    final client = widget.apiClient;
    if (ref == null || client == null) return;
    if (!ref.isMapped) {
      setState(() => _externalProblem = 'The file reference "${ref.name}" is not mapped to a solution yet. '
          'Choose its solution in File > Manage > File References to list its tables.');
      return;
    }
    final token = client.linkedToken(ref.database);
    if (token == null) {
      setState(() {
        _needsSignIn = true;
        _externalProblem = 'Not signed in to ${ref.database} (through "${ref.name}"). Sign in to it to list its tables.';
      });
      return;
    }
    setState(() {
      _loadingExternal = true;
      _externalProblem = null;
      _needsSignIn = false;
    });
    try {
      final tables = await client.forPeerSession(token).listTables();
      if (!mounted || _reference?.id != ref.id) return;
      setState(() {
        _externalTables = tables;
        _loadingExternal = false;
        _externalProblem = tables.isEmpty ? '${ref.database} has no tables yet.' : null;
        _selectedTable = tables.firstOrNull;
        if (_selectedTable != null) _nameController.text = _suggestOccurrenceName(_selectedTable!);
      });
    } catch (e) {
      if (!mounted) return;
      final ended = e is ApiException && e.statusCode == 401;
      if (ended) ExternalAccess.of(client).peerRejected(ref.database);
      setState(() {
        _loadingExternal = false;
        _needsSignIn = ended;
        _externalProblem = ended
            ? 'Your session in ${ref.database} has ended. Sign in to it again to list its tables.'
            : 'The tables of ${ref.database} (through "${ref.name}") could not be listed: '
                '${e is ApiException ? e.detail : e}';
      });
    }
  }

  Future<void> _signIn() async {
    final ref = _reference;
    final client = widget.apiClient;
    if (ref == null || client == null) return;
    if (await ExternalAccess.of(client).signInTo(context, ref.database) && mounted) {
      await _loadExternalTables();
    }
  }

  void _onTableSelected(TableModel table) {
    setState(() {
      _selectedTable = table;
      if (widget.initialOccurrence == null) {
        _nameController.text = _suggestOccurrenceName(table);
      }
    });
  }

  /// What an occurrence of another solution's table shows instead of the
  /// table list: where its records live, read-only. Turning it into an
  /// occurrence of a local table is not offered, so it cannot happen by
  /// accident; only its name can change.
  List<Widget> _externalSource(ThemeData theme, ExternalOccurrenceModel external) {
    final muted = TextStyle(fontSize: 13, color: theme.textTheme.bodySmall?.color ?? Colors.grey[700]);
    Widget line(String label, String value) => Padding(
          padding: const EdgeInsets.only(bottom: 8),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SizedBox(width: 100, child: Text(label, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w500))),
              Expanded(child: Text(value, style: muted)),
            ],
          ),
        );
    return [
      Text(
        'This occurrence shows a table of another file. Its data source is set when the file reference is bound.',
        style: TextStyle(fontSize: 12.5, color: theme.textTheme.bodySmall?.color ?? Colors.grey[700], height: 1.3),
      ),
      const SizedBox(height: 16),
      line('Data source:', external.fileReference),
      line('Solution:', external.database.isNotEmpty ? external.database : '(not mapped yet)'),
      line('Table:', external.table.isNotEmpty ? external.table : '(not bound yet)'),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final external = widget.initialOccurrence?.external;

    return AlertDialog(
      title: Row(
        children: [
          Icon(Icons.table_chart_outlined, color: theme.colorScheme.primary, size: 22),
          const SizedBox(width: 8),
          const Text('Specify Table', style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
        ],
      ),
      content: SizedBox(
        width: 440,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (external != null) ..._externalSource(theme, external) else ...[
            Text(
              'Select a table from this file or another data source to include in the graph. The same table can appear more than once.',
              style: TextStyle(fontSize: 12.5, color: theme.textTheme.bodySmall?.color ?? Colors.grey[700], height: 1.3),
            ),
            const SizedBox(height: 16),
            Row(
              children: [
                const Text('Data source:', style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500)),
                const SizedBox(width: 10),
                Expanded(
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 2),
                    decoration: BoxDecoration(
                      border: Border.all(color: theme.dividerColor),
                      borderRadius: BorderRadius.circular(6),
                    ),
                    child: DropdownButtonHideUnderline(
                      child: DropdownButton<String>(
                        key: const ValueKey('specify-table-data-source'),
                        value: _selectedDataSource,
                        isDense: true,
                        isExpanded: true,
                        items: [
                          DropdownMenuItem(
                            value: _local,
                            child: Text('Current file ("${widget.activeDatabaseName}")', style: const TextStyle(fontSize: 13)),
                          ),
                          for (final ref in _references)
                            DropdownMenuItem(
                              value: ref.id,
                              child: Text(
                                ref.isMapped ? '${ref.name} (${ref.database})' : '${ref.name} (not mapped)',
                                style: const TextStyle(fontSize: 13, fontStyle: FontStyle.italic),
                              ),
                            ),
                          if (widget.apiClient != null)
                            const DropdownMenuItem(
                              value: _addReference,
                              child: Text('Add File Reference…', style: TextStyle(fontSize: 13)),
                            ),
                        ],
                        onChanged: (val) => unawaited(_chooseDataSource(val)),
                      ),
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 14),
            Container(
              height: 180,
              decoration: BoxDecoration(
                border: Border.all(color: theme.dividerColor),
                borderRadius: BorderRadius.circular(6),
                color: theme.cardColor,
              ),
              child: _loadingExternal
                  ? const Center(child: CircularProgressIndicator())
                  : _externalProblem != null
                  ? Center(
                      child: Padding(
                        padding: const EdgeInsets.all(12),
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Text(_externalProblem!,
                                key: const ValueKey('specify-table-external-problem'),
                                textAlign: TextAlign.center,
                                style: const TextStyle(fontSize: 12.5)),
                            if (_needsSignIn)
                              TextButton(
                                key: const ValueKey('specify-table-sign-in'),
                                onPressed: () => unawaited(_signIn()),
                                child: Text('Sign in to ${_reference?.database}…'),
                              ),
                          ],
                        ),
                      ),
                    )
                  : _listedTables.isEmpty
                  ? const Center(
                      child: Text('No tables available', style: TextStyle(color: Colors.grey, fontSize: 13)),
                    )
                  : ListView.builder(
                      itemCount: _listedTables.length,
                      itemBuilder: (ctx, idx) {
                        final tbl = _listedTables[idx];
                        final isSelected = _selectedTable?.id == tbl.id;
                        return InkWell(
                          onTap: () => _onTableSelected(tbl),
                          child: Container(
                            color: isSelected ? theme.colorScheme.primary.withValues(alpha: 0.15) : Colors.transparent,
                            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                            child: Row(
                              children: [
                                Icon(
                                  Icons.table_view,
                                  size: 16,
                                  color: isSelected ? theme.colorScheme.primary : Colors.grey[600],
                                ),
                                const SizedBox(width: 8),
                                Expanded(
                                  child: Text(
                                    tbl.displayName.isNotEmpty ? tbl.displayName : tbl.name,
                                    style: TextStyle(
                                      fontSize: 13,
                                      fontWeight: isSelected ? FontWeight.bold : FontWeight.normal,
                                      color: isSelected ? theme.colorScheme.primary : null,
                                    ),
                                  ),
                                ),
                                Text(
                                  '${tbl.columns.length} fields',
                                  style: TextStyle(fontSize: 11, color: Colors.grey[600]),
                                ),
                              ],
                            ),
                          ),
                        );
                      },
                    ),
            ),
            ],
            const SizedBox(height: 16),
            Row(
              children: [
                const SizedBox(
                  width: 70,
                  child: Text('Name:', style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500)),
                ),
                Expanded(
                  child: TextField(
                    controller: _nameController,
                    decoration: const InputDecoration(
                      isDense: true,
                      contentPadding: EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                      border: OutlineInputBorder(),
                    ),
                    style: const TextStyle(fontSize: 13),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
      actions: [
        OutlinedButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        ElevatedButton(
          onPressed: (external == null && _selectedTable == null) || _nameController.text.trim().isEmpty
              ? null
              : () {
                  final reference = _reference;
                  Navigator.of(context).pop({
                    if (external == null && reference == null) 'base_table_id': _selectedTable!.id,
                    if (external == null && reference != null)
                      'external': {'file_reference': reference.name, 'table': _selectedTable!.name},
                    'name': _nameController.text.trim(),
                  });
                },
          child: const Text('OK'),
        ),
      ],
    );
  }
}
