import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/api/api_client.dart';

/// Helper and preview dialog for copying and pasting design objects (`table`,
/// `columns`, `layout`, `script`) between solutions as reviewable JSON text on
/// the clipboard (#54).
class StructureClipboardService {
  static String? _lastCopiedJson;

  /// Returns the most recently copied structure JSON text in memory, used as a
  /// fallback when the platform clipboard is unavailable (e.g. headless tests).
  static String? get lastCopiedJson => _lastCopiedJson;

  /// Overrides the in-memory clipboard text (useful in widget tests).
  static void setClipboardTextForTesting(String? jsonText) {
    _lastCopiedJson = jsonText;
  }

  /// Copies a design object (`table`, `columns`, `layout`, or `script`) as
  /// formatted JSON to the system clipboard.
  static Future<String?> copyStructure(
    BuildContext context, {
    required ApiClient apiClient,
    required String kind,
    String? tableId,
    List<String>? columnIds,
    String? layoutId,
    String? scriptId,
    String? database,
    String? label,
  }) async {
    try {
      final envelope = await apiClient.copyStructure(
        kind: kind,
        tableId: tableId,
        columnIds: columnIds,
        layoutId: layoutId,
        scriptId: scriptId,
        database: database,
      );
      final formatted = const JsonEncoder.withIndent('  ').convert(envelope);
      _lastCopiedJson = formatted;
      try {
        await Clipboard.setData(ClipboardData(text: formatted));
      } catch (_) {}
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              'Copied ${label ?? kind} structure to clipboard as JSON.',
            ),
            duration: const Duration(seconds: 2),
          ),
        );
      }
      return formatted;
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Failed to copy structure: $e'),
            backgroundColor: Colors.red,
          ),
        );
      }
      return null;
    }
  }

  /// Reads a structure clipboard envelope from the system clipboard (or prompts
  /// the user to paste the JSON if the clipboard cannot be read), previews
  /// collisions and missing dependencies against the destination solution, and
  /// pastes the object.
  static Future<ClipboardPasteReportModel?> pasteStructure(
    BuildContext context, {
    required ApiClient apiClient,
    String? expectedKind,
    String? targetTableId,
    String? database,
  }) async {
    String? rawText;
    try {
      final clip = await Clipboard.getData(Clipboard.kTextPlain);
      rawText = clip?.text;
    } catch (_) {}
    if (rawText == null || rawText.trim().isEmpty) {
      rawText = _lastCopiedJson;
    }

    Map<String, dynamic>? envelope = _tryParseEnvelope(rawText);
    if (envelope == null && context.mounted) {
      rawText = await _promptForEnvelopeText(context, initialText: rawText);
      if (rawText == null) return null;
      envelope = _tryParseEnvelope(rawText);
    }
    if (envelope == null) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text(
              'Clipboard does not contain a valid File4Base structure JSON envelope.',
            ),
            backgroundColor: Colors.orange,
          ),
        );
      }
      return null;
    }

    final kind = (envelope['kind'] as String? ?? '').toLowerCase();
    if (expectedKind != null &&
        expectedKind.isNotEmpty &&
        kind != expectedKind.toLowerCase()) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              'Clipboard holds a "$kind" structure, but "$expectedKind" was expected here.',
            ),
            backgroundColor: Colors.orange,
          ),
        );
      }
      return null;
    }

    try {
      final preview = await apiClient.previewStructureClipboard(
        envelope: envelope,
        targetTableId: targetTableId,
        database: database,
      );

      String onCollision = 'rename';
      if (preview.hasCollision || preview.missingDependencies.isNotEmpty) {
        if (!context.mounted) return null;
        final decision = await StructurePastePreviewDialog.show(
          context,
          preview: preview,
          envelopeJson: const JsonEncoder.withIndent('  ').convert(envelope),
        );
        if (decision == null) return null;
        onCollision = decision;
      }

      final report = await apiClient.pasteStructureClipboard(
        envelope: envelope,
        targetTableId: targetTableId,
        onCollision: onCollision,
        database: database,
      );
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              'Pasted ${report.kind} "${report.createdName}".',
            ),
            duration: const Duration(seconds: 2),
          ),
        );
      }
      return report;
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Failed to paste structure: $e'),
            backgroundColor: Colors.red,
          ),
        );
      }
      return null;
    }
  }

  static Map<String, dynamic>? _tryParseEnvelope(String? raw) {
    if (raw == null || raw.trim().isEmpty) return null;
    try {
      final decoded = jsonDecode(raw);
      if (decoded is Map && decoded['kind'] is String) {
        return Map<String, dynamic>.from(decoded);
      }
    } catch (_) {}
    return null;
  }

  static Future<String?> _promptForEnvelopeText(
    BuildContext context, {
    String? initialText,
  }) {
    final ctrl = TextEditingController(text: initialText ?? '');
    return showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Paste Structure JSON'),
        content: SizedBox(
          width: 520,
          child: TextField(
            key: const Key('paste_structure_json_field'),
            controller: ctrl,
            maxLines: 12,
            style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
            decoration: const InputDecoration(
              hintText: '{"format": "file4base_clipboard", "kind": "table", ...}',
              border: OutlineInputBorder(),
            ),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(),
            child: const Text('Cancel'),
          ),
          FilledButton(
            key: const Key('paste_structure_json_continue_button'),
            onPressed: () => Navigator.of(ctx).pop(ctrl.text),
            child: const Text('Continue'),
          ),
        ],
      ),
    );
  }
}

/// Dialog shown before pasting when the destination solution has a name
/// collision or missing dependencies (#54).
class StructurePastePreviewDialog extends StatefulWidget {
  final ClipboardPreviewModel preview;
  final String envelopeJson;

  const StructurePastePreviewDialog({
    super.key,
    required this.preview,
    required this.envelopeJson,
  });

  /// Returns `'rename'`, `'replace'`, or `null` if cancelled.
  static Future<String?> show(
    BuildContext context, {
    required ClipboardPreviewModel preview,
    required String envelopeJson,
  }) {
    return showDialog<String>(
      context: context,
      builder: (_) => StructurePastePreviewDialog(
        preview: preview,
        envelopeJson: envelopeJson,
      ),
    );
  }

  @override
  State<StructurePastePreviewDialog> createState() =>
      _StructurePastePreviewDialogState();
}

class _StructurePastePreviewDialogState
    extends State<StructurePastePreviewDialog> {
  String _onCollision = 'rename';
  bool _showJson = false;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final preview = widget.preview;

    return AlertDialog(
      title: Row(
        children: [
          const Icon(Icons.content_paste_go, size: 20),
          const SizedBox(width: 8),
          Expanded(
            child: Text('Paste ${preview.kind}: ${preview.objectName}'),
          ),
        ],
      ),
      content: SizedBox(
        width: 520,
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            mainAxisSize: MainAxisSize.min,
            children: [
              if (preview.hasCollision) ...[
                Material(
                  color: theme.colorScheme.secondaryContainer,
                  borderRadius: BorderRadius.circular(6),
                  child: Padding(
                    padding: const EdgeInsets.all(10),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'Name Collision Detected (${preview.collisions.join(", ")})',
                          style: const TextStyle(
                            fontWeight: FontWeight.w600,
                            fontSize: 13,
                          ),
                        ),
                        const SizedBox(height: 6),
                        RadioListTile<String>(
                          key: const Key('clipboard_collision_rename_radio'),
                          dense: true,
                          contentPadding: EdgeInsets.zero,
                          value: 'rename',
                          groupValue: _onCollision,
                          onChanged: (v) =>
                              setState(() => _onCollision = v ?? 'rename'),
                          title: const Text(
                            'Keep both and rename the pasted copy',
                            style: TextStyle(fontSize: 13),
                          ),
                        ),
                        RadioListTile<String>(
                          key: const Key('clipboard_collision_replace_radio'),
                          dense: true,
                          contentPadding: EdgeInsets.zero,
                          value: 'replace',
                          groupValue: _onCollision,
                          onChanged: (v) =>
                              setState(() => _onCollision = v ?? 'replace'),
                          title: const Text(
                            'Update / replace the existing object in destination',
                            style: TextStyle(fontSize: 13),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
                const SizedBox(height: 12),
              ],
              if (preview.missingDependencies.isNotEmpty) ...[
                Container(
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: theme.colorScheme.errorContainer,
                    borderRadius: BorderRadius.circular(6),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'Missing Dependencies in Destination (${preview.missingDependencies.length})',
                        style: TextStyle(
                          fontWeight: FontWeight.w600,
                          fontSize: 13,
                          color: theme.colorScheme.onErrorContainer,
                        ),
                      ),
                      const SizedBox(height: 6),
                      for (final dep in preview.missingDependencies)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 4),
                          child: Row(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Icon(
                                Icons.warning_amber_rounded,
                                size: 15,
                                color: theme.colorScheme.onErrorContainer,
                              ),
                              const SizedBox(width: 6),
                              Expanded(
                                child: Text(
                                  '[${dep.kind}] ${dep.name} — ${dep.detail}',
                                  style: TextStyle(
                                    fontSize: 12,
                                    color: theme.colorScheme.onErrorContainer,
                                  ),
                                ),
                              ),
                            ],
                          ),
                        ),
                    ],
                  ),
                ),
                const SizedBox(height: 12),
              ],
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton.icon(
                  onPressed: () => setState(() => _showJson = !_showJson),
                  icon: Icon(
                    _showJson ? Icons.expand_less : Icons.code,
                    size: 16,
                  ),
                  label: Text(
                    _showJson
                        ? 'Hide Clipboard JSON'
                        : 'Review Clipboard JSON Text',
                  ),
                ),
              ),
              if (_showJson)
                Container(
                  constraints: const BoxConstraints(maxHeight: 180),
                  padding: const EdgeInsets.all(8),
                  decoration: BoxDecoration(
                    border: Border.all(color: theme.dividerColor),
                    borderRadius: BorderRadius.circular(6),
                  ),
                  child: SingleChildScrollView(
                    child: SelectableText(
                      widget.envelopeJson,
                      style: const TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 11,
                      ),
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton.icon(
          key: const Key('clipboard_confirm_paste_button'),
          onPressed: () => Navigator.of(context).pop(_onCollision),
          icon: const Icon(Icons.content_paste, size: 16),
          label: const Text('Paste'),
        ),
      ],
    );
  }
}
