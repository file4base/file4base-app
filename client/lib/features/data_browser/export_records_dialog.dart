import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import '../../core/services/solution_storage.dart';

enum ExportFormat {
  csv,
  tsv,
  xlsx,
  json,
  xml,
  html,
}

class ExportRecordsDialog extends StatefulWidget {
  final ApiClient apiClient;
  final List<TableModel> tables;
  final TableModel? initialTable;

  /// The find requests that define the found set of [initialTable], so the
  /// export can be of what was found rather than the whole table (#38).
  final List<Map<String, dynamic>> findRequests;

  /// How many records that found set holds, for saying so.
  final int foundCount;

  const ExportRecordsDialog({
    super.key,
    required this.apiClient,
    required this.tables,
    this.initialTable,
    this.findRequests = const [],
    this.foundCount = 0,
  });

  static Future<void> show(
    BuildContext context, {
    required ApiClient apiClient,
    required List<TableModel> tables,
    TableModel? initialTable,
    List<Map<String, dynamic>> findRequests = const [],
    int foundCount = 0,
  }) {
    return showDialog(
      context: context,
      builder: (ctx) => ExportRecordsDialog(
        apiClient: apiClient,
        tables: tables,
        initialTable: initialTable,
        findRequests: findRequests,
        foundCount: foundCount,
      ),
    );
  }

  @override
  State<ExportRecordsDialog> createState() => _ExportRecordsDialogState();
}

class _ExportRecordsDialogState extends State<ExportRecordsDialog> {
  late TableModel? _selectedTable;
  ExportFormat _selectedFormat = ExportFormat.csv;
  bool _includeHeaders = true;

  /// Write the found set rather than the whole table. Only offered when
  /// there is one, and only for the table it was found in.
  bool _foundSetOnly = true;

  /// True when a found set is in hand for the table being exported.
  bool get _hasFoundSet =>
      widget.findRequests.isNotEmpty && _selectedTable?.id == widget.initialTable?.id;

  List<Map<String, dynamic>> get _requests =>
      _hasFoundSet && _foundSetOnly ? widget.findRequests : const [];
  bool _isExporting = false;
  String? _statusMessage;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _selectedTable = widget.initialTable ?? (widget.tables.isNotEmpty ? widget.tables.first : null);
  }

  String _formatName(ExportFormat format) {
    switch (format) {
      case ExportFormat.csv:
        return 'CSV (Comma-Separated Values)';
      case ExportFormat.tsv:
        return 'Tab-Separated Text (.tsv)';
      case ExportFormat.xlsx:
        return 'Excel Workbook (.xlsx)';
      case ExportFormat.json:
        return 'JSON (JavaScript Object Notation)';
      case ExportFormat.xml:
        return 'XML (Extensible Markup Language)';
      case ExportFormat.html:
        return 'HTML Table (.html)';
    }
  }

  String _extensionFor(ExportFormat format) {
    switch (format) {
      case ExportFormat.csv:
        return 'csv';
      case ExportFormat.tsv:
        return 'tsv';
      case ExportFormat.xlsx:
        return 'xlsx';
      case ExportFormat.json:
        return 'json';
      case ExportFormat.xml:
        return 'xml';
      case ExportFormat.html:
        return 'html';
    }
  }

  String _escapeCsv(dynamic val, {String delimiter = ','}) {
    if (val == null) return '';
    final str = val.toString();
    if (str.contains(delimiter) || str.contains('"') || str.contains('\n') || str.contains('\r')) {
      return '"${str.replaceAll('"', '""')}"';
    }
    return str;
  }

  String _escapeXml(dynamic val) {
    if (val == null) return '';
    return val
        .toString()
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&apos;');
  }

  String _escapeHtml(dynamic val) {
    if (val == null) return '';
    return val
        .toString()
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;');
  }

  Uint8List _generateExportBytes(List<Map<String, dynamic>> rows, List<String> columns) {
    switch (_selectedFormat) {
      // A workbook never reaches here: the server writes it (see
      // _exportWorkbook), because it is the one format needing a library.
      case ExportFormat.xlsx:
        throw StateError('a workbook is written by the server');
      case ExportFormat.csv:
      case ExportFormat.tsv:
        final delimiter = _selectedFormat == ExportFormat.csv ? ',' : '\t';
        final buffer = StringBuffer();
        if (_includeHeaders) {
          buffer.writeln(columns.map((c) => _escapeCsv(c, delimiter: delimiter)).join(delimiter));
        }
        for (final row in rows) {
          buffer.writeln(columns.map((c) => _escapeCsv(row[c], delimiter: delimiter)).join(delimiter));
        }
        return Uint8List.fromList(utf8.encode(buffer.toString()));

      case ExportFormat.json:
        final formattedJson = const JsonEncoder.withIndent('  ').convert(rows);
        return Uint8List.fromList(utf8.encode(formattedJson));

      case ExportFormat.xml:
        final buffer = StringBuffer();
        buffer.writeln('<?xml version="1.0" encoding="UTF-8"?>');
        final tableName = _selectedTable?.name.replaceAll(RegExp(r'[^a-zA-Z0-9_]'), '_') ?? 'records';
        buffer.writeln('<$tableName>');
        for (final row in rows) {
          buffer.writeln('  <record>');
          for (final col in columns) {
            final tag = col.replaceAll(RegExp(r'[^a-zA-Z0-9_]'), '_');
            buffer.writeln('    <$tag>${_escapeXml(row[col])}</$tag>');
          }
          buffer.writeln('  </record>');
        }
        buffer.writeln('</$tableName>');
        return Uint8List.fromList(utf8.encode(buffer.toString()));

      case ExportFormat.html:
        final buffer = StringBuffer();
        buffer.writeln('<!DOCTYPE html><html><head><meta charset="utf-8">');
        buffer.writeln('<title>${_escapeHtml(_selectedTable?.displayName ?? "Export")}</title>');
        buffer.writeln('<style>table{border-collapse:collapse;width:100%;font-family:sans-serif;}th,td{border:1px solid #ddd;padding:8px;text-align:left;}th{background:#f2f2f2;}</style>');
        buffer.writeln('</head><body>');
        buffer.writeln('<h2>${_escapeHtml(_selectedTable?.displayName ?? "Exported Records")}</h2>');
        buffer.writeln('<table>');
        if (_includeHeaders) {
          buffer.writeln('<thead><tr>');
          for (final col in columns) {
            buffer.writeln('<th>${_escapeHtml(col)}</th>');
          }
          buffer.writeln('</tr></thead>');
        }
        buffer.writeln('<tbody>');
        for (final row in rows) {
          buffer.writeln('<tr>');
          for (final col in columns) {
            buffer.writeln('<td>${_escapeHtml(row[col])}</td>');
          }
          buffer.writeln('</tr>');
        }
        buffer.writeln('</tbody></table></body></html>');
        return Uint8List.fromList(utf8.encode(buffer.toString()));
    }
  }

  /// An Excel workbook is written by the server: it is the only format that
  /// needs a library, and the server already has one for reading them.
  Future<void> _exportWorkbook() async {
    final table = _selectedTable!;
    final columns = table.columns.where((c) => c.fieldType != 'SUMMARY').toList();

    setState(() => _statusMessage = 'Writing the workbook...');
    final bytes = await widget.apiClient.exportRecords(
      table.name,
      format: 'xlsx',
      name: table.displayName,
      requests: _requests,
      fields: [for (final c in columns) c.name],
      headings: _includeHeaders ? [for (final c in columns) c.displayName] : const [],
    );

    final filename = '${table.name}_export.xlsx';
    await SolutionStorageService.saveFile(filename: filename, bytes: bytes);
    if (!mounted) return;
    Navigator.of(context).pop();
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text('Exported "$filename".'),
        backgroundColor: Colors.green.shade700,
      ),
    );
  }

  Future<void> _performExport() async {
    if (_selectedTable == null) {
      setState(() => _errorMessage = 'Please select a table to export.');
      return;
    }

    setState(() {
      _isExporting = true;
      _errorMessage = null;
      _statusMessage = 'Fetching records from server...';
    });

    try {
      // A workbook is written by the server, which has the library for it;
      // everything else is formatted here (#38).
      if (_selectedFormat == ExportFormat.xlsx) {
        await _exportWorkbook();
        return;
      }

      // Every page must arrive before anything is saved (issue #6).
      final rows = _requests.isEmpty
          ? await widget.apiClient.listAllRows(_selectedTable!.name, onProgress: (n) {
              if (mounted) setState(() => _statusMessage = 'Fetching records from server... $n');
            })
          : await widget.apiClient.executeFindAll(_selectedTable!.name, _requests);

      if (!mounted) return;

      setState(() {
        _statusMessage = 'Formatting ${rows.length} records...';
      });

      List<String> columns = _selectedTable!.columns.map((c) => c.name).toList();
      if (columns.isEmpty && rows.isNotEmpty) {
        columns = rows.first.keys.toList();
      }

      final bytes = _generateExportBytes(rows, columns);
      final ext = _extensionFor(_selectedFormat);
      final filename = '${_selectedTable!.name}_export.$ext';

      await SolutionStorageService.saveFile(filename: filename, bytes: bytes);

      if (mounted) {
        Navigator.of(context).pop();
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Successfully exported ${rows.length} records as "$filename".'),
            backgroundColor: Colors.green.shade700,
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isExporting = false;
          _errorMessage = 'Export failed: $e';
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Row(
        children: [
          Icon(Icons.file_download, color: Color(0xFF1E88E5)),
          SizedBox(width: 10),
          Text('Export Records'),
        ],
      ),
      content: SizedBox(
        width: 440,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Select table and target format to export records:',
              style: TextStyle(fontSize: 13),
            ),
            const SizedBox(height: 16),
            if (_errorMessage != null) ...[
              Container(
                padding: const EdgeInsets.all(8),
                decoration: BoxDecoration(
                  color: Colors.red.shade50,
                  borderRadius: BorderRadius.circular(4),
                  border: Border.all(color: Colors.red.shade300),
                ),
                child: Row(
                  children: [
                    const Icon(Icons.error_outline, color: Colors.red, size: 16),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        _errorMessage!,
                        style: const TextStyle(fontSize: 12, color: Colors.red),
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 12),
            ],
            DropdownButtonFormField<TableModel>(
              initialValue: _selectedTable,
              isExpanded: true,
              decoration: const InputDecoration(
                labelText: 'Source Table',
                border: OutlineInputBorder(),
                isDense: true,
                prefixIcon: Icon(Icons.table_chart, size: 18),
              ),
              items: widget.tables.map((t) {
                return DropdownMenuItem<TableModel>(
                  value: t,
                  child: Text('${t.displayName} (${t.name})', overflow: TextOverflow.ellipsis),
                );
              }).toList(),
              onChanged: _isExporting
                  ? null
                  : (val) {
                      setState(() => _selectedTable = val);
                    },
            ),
            const SizedBox(height: 14),
            DropdownButtonFormField<ExportFormat>(
              initialValue: _selectedFormat,
              isExpanded: true,
              decoration: const InputDecoration(
                labelText: 'Export Format',
                border: OutlineInputBorder(),
                isDense: true,
                prefixIcon: Icon(Icons.format_list_bulleted, size: 18),
              ),
              items: ExportFormat.values.map((f) {
                return DropdownMenuItem<ExportFormat>(
                  value: f,
                  child: Text(_formatName(f), overflow: TextOverflow.ellipsis),
                );
              }).toList(),
              onChanged: _isExporting
                  ? null
                  : (val) {
                      if (val != null) {
                        setState(() => _selectedFormat = val);
                      }
                    },
            ),
            const SizedBox(height: 12),
            if (_selectedFormat == ExportFormat.csv ||
                _selectedFormat == ExportFormat.tsv ||
                _selectedFormat == ExportFormat.html)
              CheckboxListTile(
                contentPadding: EdgeInsets.zero,
                dense: true,
                title: const Text('Include field names as first row (header)', style: TextStyle(fontSize: 13)),
                value: _includeHeaders,
                onChanged: _isExporting ? null : (v) => setState(() => _includeHeaders = v ?? true),
              ),
            if (_isExporting && _statusMessage != null) ...[
              const SizedBox(height: 12),
              Row(
                children: [
                  const SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      _statusMessage!,
                      style: const TextStyle(fontSize: 12, color: Colors.blueGrey),
                    ),
                  ),
                ],
              ),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _isExporting ? null : () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton.icon(
          onPressed: _isExporting || _selectedTable == null ? null : _performExport,
          icon: const Icon(Icons.download, size: 16),
          label: const Text('Export Records'),
        ),
      ],
    );
  }
}
