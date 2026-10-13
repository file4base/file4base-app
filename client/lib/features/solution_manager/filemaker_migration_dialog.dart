import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import '../../core/services/solution_storage.dart';

/// Assisted FileMaker DDR / Save-as-XML & FMPXMLRESULT Migration Dialog (#26).
class FileMakerMigrationDialog extends StatefulWidget {
  final ApiClient apiClient;
  final String? initialFmp12FileName;

  const FileMakerMigrationDialog({
    super.key,
    required this.apiClient,
    this.initialFmp12FileName,
  });

  static Future<MigrationApplyResultModel?> show(
    BuildContext context,
    ApiClient apiClient, {
    String? initialFmp12FileName,
  }) {
    return showDialog<MigrationApplyResultModel>(
      context: context,
      barrierDismissible: true,
      builder: (ctx) => FileMakerMigrationDialog(
        apiClient: apiClient,
        initialFmp12FileName: initialFmp12FileName,
      ),
    );
  }

  /// Returns true when [fileName] or [bytes] represents a binary `.fmp12`
  /// container rather than a File4Base MessagePack bundle.
  static bool isFmp12Binary(String fileName, Uint8List bytes) {
    final lower = fileName.trim().toLowerCase();
    if (lower.endsWith('.fmp12') ||
        lower.endsWith('.fp7') ||
        lower.endsWith('.fp5')) {
      return true;
    }
    if (bytes.length >= 20) {
      if (bytes[0] == 0x00 &&
          bytes[1] == 0x01 &&
          bytes[2] == 0x00 &&
          bytes[3] == 0x00 &&
          bytes[4] == 0x00) {
        return true;
      }
      final headerStr = String.fromCharCodes(
        bytes.sublist(0, bytes.length < 128 ? bytes.length : 128),
      );
      if (headerStr.contains('HBAM2') || headerStr.contains('FileMaker Pro')) {
        return true;
      }
    }
    return false;
  }

  @override
  State<FileMakerMigrationDialog> createState() =>
      _FileMakerMigrationDialogState();
}

class _FileMakerMigrationDialogState extends State<FileMakerMigrationDialog> {
  final _designXmlCtrl = TextEditingController();
  final _solutionNameCtrl = TextEditingController();
  final _dataTableCtrl = TextEditingController();
  String? _designFileName;
  final Map<String, String> _dataFiles = {};

  bool _isRunningPreflight = false;
  bool _isApplying = false;
  String? _error;
  MigrationReportModel? _report;
  MigrationApplyResultModel? _appliedResult;
  int _selectedCategoryTab = 0;

  @override
  void dispose() {
    _designXmlCtrl.dispose();
    _solutionNameCtrl.dispose();
    _dataTableCtrl.dispose();
    super.dispose();
  }

  Future<void> _pickDesignXmlFile() async {
    setState(() => _error = null);
    try {
      final file = await SolutionStorageService.pickFile(
        allowedExtensions: ['xml', 'ddr'],
      );
      if (file == null) return;
      if (FileMakerMigrationDialog.isFmp12Binary(file.name, file.bytes)) {
        setState(() {
          _error =
              '"${file.name}" is a binary .fmp12 container. Export a Database Design Report (DDR) or Save a Copy as XML from FileMaker first.';
        });
        return;
      }
      final text = utf8.decode(file.bytes, allowMalformed: true);
      setState(() {
        _designFileName = file.name;
        _designXmlCtrl.text = text;
        _report = null;
      });
    } catch (e) {
      setState(() => _error = 'Failed reading XML file: $e');
    }
  }

  Future<void> _pickDataXmlFile() async {
    final tableName = _dataTableCtrl.text.trim();
    if (tableName.isEmpty) {
      setState(() {
        _error =
            'Enter the target Base Table name before attaching an FMPXMLRESULT record export.';
      });
      return;
    }
    setState(() => _error = null);
    try {
      final file = await SolutionStorageService.pickFile(
        allowedExtensions: ['xml'],
      );
      if (file == null) return;
      final text = utf8.decode(file.bytes, allowMalformed: true);
      setState(() {
        _dataFiles[tableName] = text;
        _dataTableCtrl.clear();
        _report = null;
      });
    } catch (e) {
      setState(() => _error = 'Failed reading FMPXMLRESULT file: $e');
    }
  }

  Future<void> _runPreflight() async {
    final xml = _designXmlCtrl.text.trim();
    if (xml.isEmpty) {
      setState(() {
        _error =
            'Select or paste a FileMaker DDR / Save-as-XML design file first.';
      });
      return;
    }
    setState(() {
      _isRunningPreflight = true;
      _error = null;
    });
    try {
      final report = await widget.apiClient.migratePreflight(
        designXml: xml,
        solutionName: _solutionNameCtrl.text.trim(),
        dataFiles: _dataFiles,
      );
      if (!mounted) return;
      setState(() {
        _report = report;
        if (_solutionNameCtrl.text.trim().isEmpty &&
            report.solutionName.isNotEmpty) {
          _solutionNameCtrl.text = report.solutionName;
        }
        _isRunningPreflight = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString().replaceFirst('ApiException ', '');
        _isRunningPreflight = false;
      });
    }
  }

  Future<void> _applyMigration() async {
    final xml = _designXmlCtrl.text.trim();
    if (xml.isEmpty) return;
    setState(() {
      _isApplying = true;
      _error = null;
    });
    try {
      final result = await widget.apiClient.migrateApply(
        designXml: xml,
        solutionName: _solutionNameCtrl.text.trim(),
        dataFiles: _dataFiles,
      );
      if (!mounted) return;
      setState(() {
        _appliedResult = result;
        _report = result.report;
        _isApplying = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString().replaceFirst('ApiException ', '');
        _isApplying = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final report = _report;
    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 16),
      backgroundColor: const Color(0xFF1E293B),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 820, maxHeight: 720),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  const Icon(
                    Icons.transform_rounded,
                    color: Color(0xFF0EA5E9),
                    size: 22,
                  ),
                  const SizedBox(width: 10),
                  const Expanded(
                    child: Text(
                      'Assisted FileMaker XML / DDR Migration',
                      style: TextStyle(
                        color: Colors.white,
                        fontSize: 16,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  IconButton(
                    icon: const Icon(Icons.close, color: Colors.white70, size: 20),
                    padding: EdgeInsets.zero,
                    constraints: const BoxConstraints(minWidth: 32, minHeight: 32),
                    onPressed: () => Navigator.of(context).pop(_appliedResult),
                  ),
                ],
              ),
              const SizedBox(height: 6),
              Expanded(
                child: SingleChildScrollView(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      if (widget.initialFmp12FileName != null)
                        Container(
                          margin: const EdgeInsets.only(bottom: 8),
                          padding: const EdgeInsets.all(8),
                          decoration: BoxDecoration(
                            color: const Color(0xFF0F172A),
                            borderRadius: BorderRadius.circular(8),
                            border: Border.all(color: const Color(0xFF38BDF8)),
                          ),
                          child: Text(
                            '"${widget.initialFmp12FileName}" is a proprietary binary .fmp12 container. '
                            'Export a Database Design Report (DDR) or Save a Copy as XML in FileMaker '
                            '(plus optional FMPXMLRESULT record exports) and run Preflight below.',
                            style: const TextStyle(
                              color: Color(0xFFE2E8F0),
                              fontSize: 12,
                            ),
                          ),
                        ),
                      Row(
                        children: [
                          Expanded(
                            child: TextField(
                              controller: _solutionNameCtrl,
                              style: const TextStyle(
                                color: Colors.white,
                                fontSize: 12.5,
                              ),
                              decoration: const InputDecoration(
                                labelText: 'Target Solution Name (optional)',
                                labelStyle: TextStyle(color: Colors.white60),
                                isDense: true,
                              ),
                            ),
                          ),
                          const SizedBox(width: 10),
                          ElevatedButton.icon(
                            onPressed: _pickDesignXmlFile,
                            icon: const Icon(Icons.upload_file, size: 15),
                            label: Text(
                              _designFileName == null
                                  ? 'Choose DDR XML...'
                                  : 'Design: $_designFileName',
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 8),
                      SizedBox(
                        height: 68,
                        child: TextField(
                          key: const Key('migration_design_xml_input'),
                          controller: _designXmlCtrl,
                          maxLines: 3,
                          style: const TextStyle(
                            color: Colors.white,
                            fontSize: 11.5,
                            fontFamily: 'monospace',
                          ),
                          decoration: const InputDecoration(
                            hintText:
                                'Paste FileMaker <FMPReport> / <FMPSaveAsXML> XML here or choose a file above...',
                            hintStyle: TextStyle(color: Colors.white38),
                            border: OutlineInputBorder(),
                            contentPadding: EdgeInsets.all(8),
                          ),
                        ),
                      ),
                      const SizedBox(height: 8),
                      Row(
                        children: [
                          Expanded(
                            child: TextField(
                              controller: _dataTableCtrl,
                              style: const TextStyle(
                                color: Colors.white,
                                fontSize: 12.5,
                              ),
                              decoration: const InputDecoration(
                                labelText:
                                    'Optional: Base Table Name for FMPXMLRESULT records',
                                labelStyle: TextStyle(color: Colors.white60),
                                isDense: true,
                              ),
                            ),
                          ),
                          const SizedBox(width: 8),
                          OutlinedButton.icon(
                            onPressed: _pickDataXmlFile,
                            icon: const Icon(Icons.dataset_outlined, size: 15),
                            label: const Text('Attach FMPXMLRESULT...'),
                          ),
                        ],
                      ),
                      if (_dataFiles.isNotEmpty) ...[
                        const SizedBox(height: 6),
                        Wrap(
                          spacing: 6,
                          children: _dataFiles.keys
                              .map(
                                (tbl) => Chip(
                                  label: Text(
                                    'Records: $tbl',
                                    style: const TextStyle(fontSize: 11),
                                  ),
                                  onDeleted: () =>
                                      setState(() => _dataFiles.remove(tbl)),
                                ),
                              )
                              .toList(),
                        ),
                      ],
                      if (_error != null) ...[
                        const SizedBox(height: 8),
                        Container(
                          padding: const EdgeInsets.all(8),
                          decoration: BoxDecoration(
                            color: Colors.red.withValues(alpha: 0.15),
                            borderRadius: BorderRadius.circular(6),
                            border: Border.all(color: Colors.redAccent),
                          ),
                          child: Text(
                            _error!,
                            style: const TextStyle(
                              color: Colors.redAccent,
                              fontSize: 12,
                            ),
                          ),
                        ),
                      ],
                      if (_appliedResult != null) ...[
                        const SizedBox(height: 8),
                        Container(
                          padding: const EdgeInsets.all(8),
                          decoration: BoxDecoration(
                            color: Colors.green.withValues(alpha: 0.15),
                            borderRadius: BorderRadius.circular(6),
                            border: Border.all(color: Colors.greenAccent),
                          ),
                          child: Text(
                            'Migration committed to database "${_appliedResult!.database}" '
                            '(${_appliedResult!.recordsInserted} records imported).',
                            style: const TextStyle(
                              color: Colors.greenAccent,
                              fontSize: 12,
                              fontWeight: FontWeight.w600,
                            ),
                          ),
                        ),
                      ],
                      const SizedBox(height: 8),
                      SizedBox(
                        height: 220,
                        child: report == null
                            ? const Center(
                                child: Text(
                                  'Run Preflight Dry-Run to inspect preserved, translated, unsupported, and manual-action items before importing.',
                                  style: TextStyle(
                                    color: Colors.white54,
                                    fontSize: 12.5,
                                  ),
                                  textAlign: TextAlign.center,
                                ),
                              )
                            : _buildReportView(report),
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 8),
              Wrap(
                alignment: WrapAlignment.end,
                spacing: 8,
                runSpacing: 6,
                children: [
                  TextButton(
                    onPressed: () => Navigator.of(context).pop(_appliedResult),
                    child: const Text('Close'),
                  ),
                  OutlinedButton.icon(
                    key: const Key('migration_preflight_btn'),
                    onPressed: _isRunningPreflight ? null : _runPreflight,
                    icon: const Icon(Icons.fact_check_outlined, size: 15),
                    label: Text(
                      _isRunningPreflight
                          ? 'Analyzing...'
                          : 'Run Preflight (Dry-Run)',
                    ),
                  ),
                  ElevatedButton.icon(
                    key: const Key('migration_apply_btn'),
                    onPressed:
                        (_isApplying || report == null) ? null : _applyMigration,
                    icon: const Icon(Icons.playlist_add_check, size: 16),
                    label: Text(
                      _isApplying ? 'Importing...' : 'Apply Migration',
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildReportView(MigrationReportModel report) {
    final tabs = <({String label, List<MigrationReportItemModel> items, Color color})>[
      (
        label: 'Manual Action (${report.manualActionRequired.length})',
        items: report.manualActionRequired,
        color: Colors.amberAccent,
      ),
      (
        label: 'Preserved (${report.preserved.length})',
        items: report.preserved,
        color: Colors.greenAccent,
      ),
      (
        label: 'Translated (${report.translated.length})',
        items: report.translated,
        color: Colors.lightBlueAccent,
      ),
      (
        label: 'Unsupported (${report.unsupported.length})',
        items: report.unsupported,
        color: Colors.orangeAccent,
      ),
      (
        label: 'Rejected (${report.rejected.length})',
        items: report.rejected,
        color: Colors.redAccent,
      ),
    ];
    final current = tabs[_selectedCategoryTab.clamp(0, tabs.length - 1)];

    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: const Color(0xFF0F172A),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: Colors.white12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Wrap(
            spacing: 8,
            runSpacing: 6,
            children: [
              _badge('Solution: ${report.solutionName}', const Color(0xFF38BDF8)),
              _badge('Tables: ${report.counts['tables'] ?? 0}', Colors.white70),
              _badge('Fields: ${report.counts['fields'] ?? 0}', Colors.white70),
              _badge('Layouts: ${report.counts['layouts'] ?? 0}', Colors.white70),
              _badge('Scripts: ${report.counts['scripts'] ?? 0}', Colors.white70),
              _badge('Records: ${report.counts['records_ready'] ?? 0}', Colors.white70),
              if (report.triggersQuarantined)
                _badge('Triggers Quarantined', Colors.amberAccent),
              if (report.accountsDisabled)
                _badge('Accounts Disabled Until Password Set', Colors.amberAccent),
            ],
          ),
          const SizedBox(height: 10),
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Row(
              children: List.generate(tabs.length, (i) {
                final selected = i == _selectedCategoryTab;
                return Padding(
                  padding: const EdgeInsets.only(right: 6),
                  child: ChoiceChip(
                    label: Text(
                      tabs[i].label,
                      style: TextStyle(
                        color: selected ? Colors.black : tabs[i].color,
                        fontSize: 12,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    selected: selected,
                    selectedColor: tabs[i].color,
                    onSelected: (_) => setState(() => _selectedCategoryTab = i),
                  ),
                );
              }),
            ),
          ),
          const SizedBox(height: 8),
          Expanded(
            child: current.items.isEmpty
                ? const Center(
                    child: Text(
                      'No items in this category.',
                      style: TextStyle(color: Colors.white38, fontSize: 12.5),
                    ),
                  )
                : ListView.separated(
                    itemCount: current.items.length,
                    separatorBuilder: (_, _) =>
                        const Divider(color: Colors.white10, height: 1),
                    itemBuilder: (ctx, idx) {
                      final it = current.items[idx];
                      return ListTile(
                        dense: true,
                        title: Text(
                          '[${it.domain.toUpperCase()}] ${it.sourceName}'
                          '${it.targetName.isNotEmpty && it.targetName != it.sourceName ? ' → ${it.targetName}' : ''}',
                          style: TextStyle(
                            color: current.color,
                            fontSize: 12.5,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        subtitle: Text(
                          it.detail,
                          style: const TextStyle(
                            color: Colors.white70,
                            fontSize: 12,
                          ),
                        ),
                      );
                    },
                  ),
          ),
        ],
      ),
    );
  }

  Widget _badge(String text, Color color) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(4),
        border: Border.all(color: color.withValues(alpha: 0.4)),
      ),
      child: Text(
        text,
        style: TextStyle(
          color: color,
          fontSize: 11,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }
}
