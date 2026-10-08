// #49 — Tools > Database Design Report. What the solution is made of, as a
// page to read or as text that can live in version control.

import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../core/api/api_client.dart';
import '../../core/services/solution_storage.dart';

/// The forms the report takes.
enum DesignReportFormat { html, xml, json }

extension DesignReportFormatInfo on DesignReportFormat {
  String get wire => switch (this) {
        DesignReportFormat.html => 'html',
        DesignReportFormat.xml => 'xml',
        DesignReportFormat.json => 'json',
      };

  String get label => switch (this) {
        DesignReportFormat.html => 'HTML — a report to read and print',
        DesignReportFormat.xml => 'XML — the design as text',
        DesignReportFormat.json => 'JSON — the design as text',
      };

  String get note => switch (this) {
        DesignReportFormat.html =>
          'One page: tables and fields, the relationships, every layout and script, '
              'the value lists, the accounts and what they may do.',
        DesignReportFormat.xml =>
          'The same design, parseable, with each layout exactly as it is stored. '
              'No timestamp inside, so committing an unchanged solution shows no diff.',
        DesignReportFormat.json =>
          'The same design as JSON, keys in order. A .f4p file is MessagePack and '
              'cannot be diffed; this can.',
      };
}

class DesignReportDialog extends StatefulWidget {
  final ApiClient apiClient;
  final String? solutionName;
  final DesignReportFormat initialFormat;

  /// Where the document goes. The platform's own save dialog by default;
  /// a test supplies its own, since there is no file system to save to.
  final Future<void> Function(String fileName, Uint8List bytes)? save;

  const DesignReportDialog({
    super.key,
    required this.apiClient,
    this.solutionName,
    this.initialFormat = DesignReportFormat.html,
    this.save,
  });

  static Future<void> show(
    BuildContext context, {
    required ApiClient apiClient,
    String? solutionName,
    DesignReportFormat initialFormat = DesignReportFormat.html,
  }) {
    return showDialog<void>(
      context: context,
      builder: (_) => DesignReportDialog(
        apiClient: apiClient,
        solutionName: solutionName,
        initialFormat: initialFormat,
      ),
    );
  }

  @override
  State<DesignReportDialog> createState() => _DesignReportDialogState();
}

class _DesignReportDialogState extends State<DesignReportDialog> {
  late DesignReportFormat _format = widget.initialFormat;
  bool _busy = false;
  String? _error;
  String? _saved;

  Future<void> _save() async {
    setState(() {
      _busy = true;
      _error = null;
      _saved = null;
    });
    try {
      final report = await widget.apiClient.designReport(
        format: _format.wire,
        solution: widget.solutionName,
      );
      final save = widget.save ??
          (String fileName, Uint8List bytes) =>
              SolutionStorageService.saveFile(filename: fileName, bytes: bytes);
      await save(report.fileName, report.bytes);
      if (!mounted) return;
      setState(() {
        _busy = false;
        _saved = report.fileName;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Row(
        children: [
          Icon(Icons.description_outlined, color: Color(0xFF1E88E5)),
          SizedBox(width: 8),
          Text('Database Design Report', style: TextStyle(fontSize: 16, fontWeight: FontWeight.bold)),
        ],
      ),
      content: SizedBox(
        width: 560,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'The design of ${widget.solutionName?.isNotEmpty == true ? widget.solutionName : 'this solution'}: '
              'tables and fields with their options, the relationships graph, the layouts and what '
              'they show, the scripts and their steps, value lists, accounts, privileges, saved finds '
              'and data sources.',
              style: const TextStyle(fontSize: 12),
            ),
            const SizedBox(height: 10),
            Container(
              padding: const EdgeInsets.all(10),
              decoration: BoxDecoration(
                color: Colors.blue.withValues(alpha: 0.08),
                borderRadius: BorderRadius.circular(6),
                border: Border.all(color: Colors.blue.withValues(alpha: 0.4)),
              ),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(Icons.lock_outline, size: 16, color: Colors.blue.shade800),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      'No records, and no password, password hash or stored credential, in any '
                      'format. A report is something you can attach to an email.',
                      style: TextStyle(fontSize: 11.5, color: Colors.blue.shade900),
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 14),
            // A RadioListTile needs a RadioGroup ancestor to do anything, as
            // the import dialog found out the hard way (#38).
            RadioGroup<DesignReportFormat>(
              groupValue: _format,
              onChanged: (value) {
                if (_busy) return;
                setState(() {
                  _format = value ?? _format;
                  _saved = null;
                });
              },
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  for (final format in DesignReportFormat.values)
                    RadioListTile<DesignReportFormat>(
                      key: ValueKey('design-format-${format.wire}'),
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                      value: format,
                      title: Text(format.label, style: const TextStyle(fontSize: 12.5)),
                      subtitle: Text(format.note, style: const TextStyle(fontSize: 11, color: Colors.grey)),
                    ),
                ],
              ),
            ),
            if (_error != null) ...[
              const SizedBox(height: 8),
              Text(_error!, key: const ValueKey('design-error'),
                  style: TextStyle(fontSize: 11.5, color: Colors.red.shade700)),
            ],
            if (_saved != null) ...[
              const SizedBox(height: 8),
              Text('Saved $_saved.', key: const ValueKey('design-saved'),
                  style: TextStyle(fontSize: 11.5, color: Colors.green.shade700)),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _busy ? null : () => Navigator.of(context).pop(),
          child: const Text('Close'),
        ),
        FilledButton(
          key: const ValueKey('design-save'),
          onPressed: _busy ? null : _save,
          child: Text(_busy ? 'Working…' : 'Save...'),
        ),
      ],
    );
  }
}
