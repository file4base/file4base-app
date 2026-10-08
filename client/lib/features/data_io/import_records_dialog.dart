// #38 — File > Import Records. File4Base could only read its own files; this
// brings records in from a CSV, a tab-separated file, an Excel workbook or
// XML, matching the file's columns to the table's fields.

import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../core/api/api_client.dart';
import '../../core/services/solution_storage.dart';

/// How a date like 03/04/2011 is to be read. There is no telling from the
/// value, so the reader says rather than the import guessing.
class ImportDateOrder {
  static const String iso = 'iso';
  static const String dmy = 'dmy';
  static const String mdy = 'mdy';

  static const Map<String, String> labels = {
    iso: 'Year first — 2011-04-03',
    dmy: 'Day first — 03/04/2011 is 3 April',
    mdy: 'Month first — 03/04/2011 is 4 March',
  };
}

/// What an import does with the rows it read.
class ImportAction {
  static const String add = 'add';
  static const String updateMatching = 'update_matching';
}

class ImportRecordsDialog extends StatefulWidget {
  final TableModel table;
  final ApiClient apiClient;

  /// A file the caller already has. Null makes the dialog ask for one.
  final String? fileName;
  final Uint8List? bytes;

  const ImportRecordsDialog({
    super.key,
    required this.table,
    required this.apiClient,
    this.fileName,
    this.bytes,
  });

  /// Shows the dialog. Returns the report when records were imported, so the
  /// caller can refresh, or null when nothing was.
  static Future<ImportReportModel?> show(
    BuildContext context, {
    required TableModel table,
    required ApiClient apiClient,
  }) {
    return showDialog<ImportReportModel>(
      context: context,
      barrierDismissible: false,
      builder: (_) => ImportRecordsDialog(table: table, apiClient: apiClient),
    );
  }

  @override
  State<ImportRecordsDialog> createState() => _ImportRecordsDialogState();
}

class _ImportRecordsDialogState extends State<ImportRecordsDialog> {
  String? _fileName;
  Uint8List? _bytes;

  ImportPreviewModel? _preview;
  bool _busy = false;
  String? _error;

  bool _hasHeader = true;
  String? _sheet;
  String _dateOrder = ImportDateOrder.iso;
  String _action = ImportAction.add;
  bool _addUnmatched = false;
  final List<String> _matchFields = [];

  /// Which field each column of the file goes to. A column that is not here
  /// is left out.
  final Map<int, String> _mapping = {};

  ImportReportModel? _report;

  @override
  void initState() {
    super.initState();
    _fileName = widget.fileName;
    _bytes = widget.bytes;
    if (_bytes != null) _loadPreview();
  }

  /// The fields a column can be sent to: not the primary key, which File4Base
  /// gives every record, and not the fields the database works out itself.
  List<ColumnModel> get _targets => widget.table.columns
      .where((c) => !c.isPrimaryKey && c.fieldType != 'SUMMARY' && c.fieldType != 'CALCULATION')
      .toList();

  Future<void> _pickFile() async {
    // The same picker the solution files use, so the web and desktop builds
    // behave the same way.
    final file = await SolutionStorageService.pickFile(
      allowedExtensions: const ['csv', 'tsv', 'tab', 'txt', 'xlsx', 'xlsm', 'xml'],
    );
    if (file == null) return;
    setState(() {
      _fileName = file.name;
      _bytes = file.bytes;
      _preview = null;
      _sheet = null;
      _mapping.clear();
      _matchFields.clear();
      _report = null;
    });
    await _loadPreview();
  }

  Future<void> _loadPreview() async {
    final bytes = _bytes;
    final name = _fileName;
    if (bytes == null || name == null) return;

    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final preview = await widget.apiClient.previewImport(
        widget.table.name,
        fileName: name,
        bytes: bytes,
        hasHeader: _hasHeader,
        sheet: _sheet,
      );
      if (!mounted) return;
      setState(() {
        _preview = preview;
        _busy = false;
        if (_sheet == null && preview.sheets.isNotEmpty) _sheet = preview.sheets.first;
      });
      _guessMapping();
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _preview = null;
        _error = e.toString();
      });
    }
  }

  /// Matches each column to the field whose name or label it looks like, so
  /// an export of this table reimports without being mapped by hand.
  void _guessMapping() {
    final preview = _preview;
    if (preview == null) return;
    String key(String s) => s.toLowerCase().replaceAll(RegExp(r'[^a-z0-9]'), '');

    final taken = <String>{};
    setState(() {
      _mapping.clear();
      for (final (i, column) in preview.columns.indexed) {
        final wanted = key(column);
        for (final target in _targets) {
          if (taken.contains(target.name)) continue;
          if (key(target.name) == wanted || key(target.displayName) == wanted) {
            _mapping[i] = target.name;
            taken.add(target.name);
            break;
          }
        }
      }
    });
  }

  /// What stops the import, said rather than leaving the button dead.
  String? get _blocker {
    if (_bytes == null) return 'Choose a file to import.';
    if (_preview == null) return null;
    if (_preview!.rowCount == 0) return 'The file holds no rows.';
    if (_mapping.isEmpty) return 'Send at least one column to a field.';
    if (_action == ImportAction.updateMatching) {
      if (_matchFields.isEmpty) return 'Choose a field to match records on.';
      for (final field in _matchFields) {
        if (!_mapping.values.contains(field)) {
          return 'A column has to be sent to every field records are matched on.';
        }
      }
    }
    return null;
  }

  Future<void> _import() async {
    final bytes = _bytes;
    final name = _fileName;
    if (bytes == null || name == null) return;

    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final report = await widget.apiClient.importRecords(
        widget.table.name,
        fileName: name,
        bytes: bytes,
        hasHeader: _hasHeader,
        sheet: _sheet,
        options: {
          'action': _action,
          'date_order': _dateOrder,
          'add_unmatched': _addUnmatched,
          'match_fields': _matchFields,
          'mappings': [
            for (final entry in _mapping.entries) {'column': entry.key, 'field': entry.value},
          ],
        },
      );
      if (!mounted) return;
      setState(() {
        _report = report;
        _busy = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = e.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final report = _report;

    return AlertDialog(
      title: Row(
        children: [
          const Icon(Icons.file_download_outlined, color: Color(0xFF1E88E5)),
          const SizedBox(width: 8),
          Expanded(child: Text('Import Records into ${widget.table.displayName}')),
        ],
      ),
      content: SizedBox(
        width: 760,
        height: 520,
        child: report != null ? _reportView(report) : _setupView(),
      ),
      actions: report != null
          ? [
              FilledButton(
                key: const ValueKey('import-done'),
                onPressed: () => Navigator.of(context).pop(report),
                child: const Text('Done'),
              ),
            ]
          : [
              TextButton(
                onPressed: _busy ? null : () => Navigator.of(context).pop(),
                child: const Text('Cancel'),
              ),
              FilledButton(
                key: const ValueKey('import-start'),
                onPressed: _busy || _blocker != null ? null : _import,
                child: Text(_busy ? 'Working…' : 'Import'),
              ),
            ],
    );
  }

  Widget _setupView() {
    final blocker = _blocker;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Expanded(
          child: ListView(
            children: [
              _filePicker(),
              if (_error != null) ...[
                const SizedBox(height: 10),
                Container(
                  key: const ValueKey('import-error'),
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: Colors.red.withValues(alpha: 0.08),
                    borderRadius: BorderRadius.circular(4),
                    border: Border.all(color: Colors.red.shade300),
                  ),
                  child: Text(_error!, style: const TextStyle(fontSize: 11.5, color: Colors.red)),
                ),
              ],
              if (_preview != null) ...[
                const SizedBox(height: 14),
                _readingOptions(),
                const SizedBox(height: 14),
                _mappingTable(),
                const SizedBox(height: 14),
                _actionOptions(),
              ],
            ],
          ),
        ),
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
                    key: const ValueKey('import-blocker'),
                    style: TextStyle(fontSize: 11.5, color: Colors.orange.shade800),
                  ),
                ),
              ],
            ),
          ),
      ],
    );
  }

  Widget _filePicker() {
    final preview = _preview;
    return Row(
      children: [
        OutlinedButton.icon(
          key: const ValueKey('import-choose-file'),
          icon: const Icon(Icons.folder_open, size: 16),
          label: Text(_fileName == null ? 'Choose a file...' : 'Choose another file...'),
          onPressed: _busy ? null : _pickFile,
        ),
        const SizedBox(width: 12),
        Expanded(
          child: Text(
            _fileName == null
                ? 'CSV, tab-separated text, an Excel workbook (.xlsx) or XML.'
                : preview == null
                    ? (_busy ? '$_fileName — reading…' : _fileName!)
                    : '$_fileName — ${preview.rowCount} row(s), '
                        '${preview.columns.length} column(s)'
                        '${preview.truncated ? ' (the file was longer and was cut)' : ''}',
            style: const TextStyle(fontSize: 11.5),
          ),
        ),
      ],
    );
  }

  Widget _readingOptions() {
    final preview = _preview!;
    return Wrap(
      spacing: 16,
      runSpacing: 8,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        SizedBox(
          width: 240,
          child: CheckboxListTile(
            key: const ValueKey('import-has-header'),
            dense: true,
            contentPadding: EdgeInsets.zero,
            title: const Text('First row holds the column names',
                style: TextStyle(fontSize: 11.5)),
            value: _hasHeader,
            onChanged: _busy
                ? null
                : (v) {
                    setState(() => _hasHeader = v ?? true);
                    _loadPreview();
                  },
          ),
        ),
        if (preview.sheets.length > 1)
          SizedBox(
            width: 200,
            child: DropdownButtonFormField<String>(
              key: const ValueKey('import-sheet'),
              isExpanded: true,
              value: _sheet,
              decoration: const InputDecoration(
                  labelText: 'Sheet', border: OutlineInputBorder(), isDense: true),
              items: [
                for (final sheet in preview.sheets)
                  DropdownMenuItem(
                      value: sheet, child: Text(sheet, style: const TextStyle(fontSize: 11.5))),
              ],
              onChanged: _busy
                  ? null
                  : (v) {
                      setState(() => _sheet = v);
                      _loadPreview();
                    },
            ),
          ),
        SizedBox(
          width: 260,
          child: DropdownButtonFormField<String>(
            key: const ValueKey('import-date-order'),
            isExpanded: true,
            value: _dateOrder,
            decoration: const InputDecoration(
                labelText: 'Dates are written', border: OutlineInputBorder(), isDense: true),
            items: [
              for (final entry in ImportDateOrder.labels.entries)
                DropdownMenuItem(
                    value: entry.key,
                    child: Text(entry.value, style: const TextStyle(fontSize: 11))),
            ],
            onChanged: (v) => setState(() => _dateOrder = v ?? ImportDateOrder.iso),
          ),
        ),
      ],
    );
  }

  Widget _mappingTable() {
    final preview = _preview!;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            const Text('Each column goes to:',
                style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
            const Spacer(),
            TextButton(
              onPressed: _guessMapping,
              child: const Text('Match by name', style: TextStyle(fontSize: 11)),
            ),
            TextButton(
              onPressed: () => setState(_mapping.clear),
              child: const Text('Clear', style: TextStyle(fontSize: 11)),
            ),
          ],
        ),
        const Text('A column with no field is left out.',
            style: TextStyle(fontSize: 10.5, color: Colors.grey)),
        const SizedBox(height: 6),
        Container(
          constraints: const BoxConstraints(maxHeight: 220),
          decoration: BoxDecoration(
            border: Border.all(color: Colors.grey.shade400),
            borderRadius: BorderRadius.circular(4),
          ),
          child: ListView.separated(
            shrinkWrap: true,
            itemCount: preview.columns.length,
            separatorBuilder: (_, _) => const Divider(height: 1),
            itemBuilder: (ctx, i) {
              final sample = preview.rows.isEmpty ? '' : preview.cell(0, i);
              final chosen = _mapping[i];
              // A field another column already has is not offered twice.
              final available = _targets
                  .where((c) => c.name == chosen || !_mapping.values.contains(c.name))
                  .toList();

              return Padding(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                child: Row(
                  children: [
                    Expanded(
                      flex: 4,
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(preview.columns[i],
                              style:
                                  const TextStyle(fontSize: 11.5, fontWeight: FontWeight.w600),
                              overflow: TextOverflow.ellipsis),
                          if (sample.isNotEmpty)
                            Text(sample,
                                style: const TextStyle(fontSize: 10, color: Colors.grey),
                                overflow: TextOverflow.ellipsis),
                        ],
                      ),
                    ),
                    const Icon(Icons.arrow_forward, size: 14, color: Colors.grey),
                    const SizedBox(width: 8),
                    Expanded(
                      flex: 5,
                      child: DropdownButtonFormField<String>(
                        key: ValueKey('import-map-$i'),
                        isExpanded: true,
                        value: chosen,
                        decoration:
                            const InputDecoration(border: OutlineInputBorder(), isDense: true),
                        hint: const Text('(leave out)', style: TextStyle(fontSize: 11)),
                        items: [
                          const DropdownMenuItem(
                              value: '',
                              child: Text('(leave out)', style: TextStyle(fontSize: 11))),
                          for (final col in available)
                            DropdownMenuItem(
                              value: col.name,
                              child: Text('${col.displayName} (${col.fieldType})',
                                  style: const TextStyle(fontSize: 11)),
                            ),
                        ],
                        onChanged: (v) => setState(() {
                          if (v == null || v.isEmpty) {
                            _mapping.remove(i);
                          } else {
                            _mapping[i] = v;
                          }
                          _matchFields.removeWhere((f) => !_mapping.values.contains(f));
                        }),
                      ),
                    ),
                  ],
                ),
              );
            },
          ),
        ),
      ],
    );
  }

  Widget _actionOptions() {
    final mapped = _mapping.values.toSet().toList();

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text('What to do with the rows:',
            style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
        // A RadioGroup ancestor is what drives these now; the per-tile
        // groupValue and onChanged are ignored without one, so the taps did
        // nothing at all.
        RadioGroup<String>(
          groupValue: _action,
          onChanged: (v) => setState(() => _action = v ?? ImportAction.add),
          child: const Column(
            children: [
              RadioListTile<String>(
                key: ValueKey('import-action-add'),
                dense: true,
                contentPadding: EdgeInsets.zero,
                value: ImportAction.add,
                title: Text('Add every row as a new record', style: TextStyle(fontSize: 11.5)),
              ),
              RadioListTile<String>(
                key: ValueKey('import-action-update'),
                dense: true,
                contentPadding: EdgeInsets.zero,
                value: ImportAction.updateMatching,
                title: Text('Update the records that match, on:',
                    style: TextStyle(fontSize: 11.5)),
              ),
            ],
          ),
        ),
        if (_action == ImportAction.updateMatching) ...[
          Padding(
            padding: const EdgeInsets.only(left: 32),
            child: Wrap(
              spacing: 5,
              children: [
                for (final field in mapped)
                  FilterChip(
                    key: ValueKey('import-match-$field'),
                    label: Text(
                      widget.table.columns
                              .where((c) => c.name == field)
                              .firstOrNull
                              ?.displayName ??
                          field,
                      style: const TextStyle(fontSize: 11),
                    ),
                    selected: _matchFields.contains(field),
                    onSelected: (on) => setState(() {
                      on ? _matchFields.add(field) : _matchFields.remove(field);
                    }),
                  ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.only(left: 24),
            child: CheckboxListTile(
              key: const ValueKey('import-add-unmatched'),
              dense: true,
              contentPadding: EdgeInsets.zero,
              title: const Text('Add the rows that match no record',
                  style: TextStyle(fontSize: 11.5)),
              subtitle: const Text('Otherwise they are passed over.',
                  style: TextStyle(fontSize: 10.5)),
              value: _addUnmatched,
              onChanged: (v) => setState(() => _addUnmatched = v ?? false),
            ),
          ),
        ],
        const Padding(
          padding: EdgeInsets.only(top: 6),
          child: Text(
            'An import is one go: if a row cannot be read, nothing is imported and the '
            'row is named.',
            style: TextStyle(fontSize: 10.5, color: Colors.grey),
          ),
        ),
      ],
    );
  }

  Widget _reportView(ImportReportModel report) {
    return Center(
      key: const ValueKey('import-report'),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(Icons.check_circle_outline, size: 44, color: Color(0xFF2E7D32)),
          const SizedBox(height: 12),
          Text('${report.rows} row(s) read from $_fileName',
              style: const TextStyle(fontSize: 13, fontWeight: FontWeight.bold)),
          const SizedBox(height: 10),
          for (final line in [
            if (report.added > 0) '${report.added} record(s) added',
            if (report.updated > 0) '${report.updated} record(s) updated',
            if (report.skipped > 0) '${report.skipped} row(s) passed over',
          ])
            Text(line, style: const TextStyle(fontSize: 12)),
          if (report.added == 0 && report.updated == 0)
            const Text('Nothing was written.', style: TextStyle(fontSize: 12)),
          if (report.truncated)
            Padding(
              padding: const EdgeInsets.only(top: 10),
              child: Text(
                'The file held more rows than File4Base reads in one go, '
                'so the rest were not imported.',
                textAlign: TextAlign.center,
                style: TextStyle(fontSize: 11, color: Colors.orange.shade800),
              ),
            ),
          if (report.fields.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 12),
              child: Text('Fields written: ${report.fields.join(', ')}',
                  textAlign: TextAlign.center,
                  style: const TextStyle(fontSize: 10.5, color: Colors.grey)),
            ),
        ],
      ),
    );
  }
}
