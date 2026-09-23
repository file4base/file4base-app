import 'dart:typed_data';
import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import '../../core/services/solution_storage.dart';

enum SaveCopyType {
  solutionOnly, // .f4b
  databaseData, // .f4data
}

class SaveCopyDialog extends StatefulWidget {
  final ApiClient apiClient;
  final String activeSolutionName;
  final String activeDatabaseName;
  final Future<Uint8List> Function() onExportSolution;

  const SaveCopyDialog({
    super.key,
    required this.apiClient,
    required this.activeSolutionName,
    required this.activeDatabaseName,
    required this.onExportSolution,
  });

  static Future<void> show(
    BuildContext context, {
    required ApiClient apiClient,
    required String activeSolutionName,
    required String activeDatabaseName,
    required Future<Uint8List> Function() onExportSolution,
  }) {
    return showDialog(
      context: context,
      builder: (ctx) => SaveCopyDialog(
        apiClient: apiClient,
        activeSolutionName: activeSolutionName,
        activeDatabaseName: activeDatabaseName,
        onExportSolution: onExportSolution,
      ),
    );
  }

  @override
  State<SaveCopyDialog> createState() => _SaveCopyDialogState();
}

class _SaveCopyDialogState extends State<SaveCopyDialog> {
  SaveCopyType _selectedType = SaveCopyType.solutionOnly;
  late final TextEditingController _fileNameController;
  bool _isSaving = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _fileNameController = TextEditingController(
      text: '${widget.activeSolutionName}_copy.f4b',
    );
  }

  @override
  void dispose() {
    _fileNameController.dispose();
    super.dispose();
  }

  void _onTypeChanged(SaveCopyType? type) {
    if (type == null) return;
    setState(() {
      _selectedType = type;
      if (type == SaveCopyType.solutionOnly) {
        _fileNameController.text = '${widget.activeSolutionName}_copy.f4b';
      } else {
        _fileNameController.text = '${widget.activeDatabaseName}_data.f4data';
      }
    });
  }

  Future<void> _handleSave() async {
    setState(() {
      _isSaving = true;
      _errorMessage = null;
    });

    try {
      final fileName = _fileNameController.text.trim();
      Uint8List bytes;

      if (_selectedType == SaveCopyType.solutionOnly) {
        bytes = await widget.onExportSolution();
      } else {
        bytes = await widget.apiClient.exportDatabaseData();
      }

      await SolutionStorageService.saveFile(
        filename: fileName,
        bytes: bytes,
      );

      if (mounted) {
        Navigator.of(context).pop();
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Saved copy as "$fileName" in MessagePack format.'),
            backgroundColor: Colors.green.shade700,
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isSaving = false;
          _errorMessage = e.toString().replaceFirst('Exception: ', '');
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Row(
        children: [
          Container(
            padding: const EdgeInsets.all(6),
            decoration: BoxDecoration(
              color: const Color(0xFF1E88E5).withValues(alpha: 0.15),
              borderRadius: BorderRadius.circular(6),
            ),
            child: const Icon(Icons.file_copy_outlined, color: Color(0xFF1E88E5), size: 24),
          ),
          const SizedBox(width: 12),
          const Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('Save a Copy As...', style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
              Text('MessagePack Dual-File Export', style: TextStyle(fontSize: 12, color: Colors.grey)),
            ],
          ),
        ],
      ),
      content: SizedBox(
        width: 480,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (_errorMessage != null)
              Container(
                margin: const EdgeInsets.only(bottom: 12),
                padding: const EdgeInsets.all(8),
                decoration: BoxDecoration(
                  color: Colors.red.shade900.withValues(alpha: 0.2),
                  borderRadius: BorderRadius.circular(4),
                ),
                child: Text(_errorMessage!, style: const TextStyle(color: Colors.red, fontSize: 13)),
              ),

            RadioListTile<SaveCopyType>(
              value: SaveCopyType.solutionOnly,
              groupValue: _selectedType,
              onChanged: _onTypeChanged,
              title: const Text('File4Base Solution (.f4b)', style: TextStyle(fontWeight: FontWeight.bold)),
              subtitle: const Text('Layouts, schemas, occurrences, UI definitions, and DB connection parameters in MessagePack.'),
            ),
            RadioListTile<SaveCopyType>(
              value: SaveCopyType.databaseData,
              groupValue: _selectedType,
              onChanged: _onTypeChanged,
              title: const Text('Database Data File (.f4data)', style: TextStyle(fontWeight: FontWeight.bold)),
              subtitle: const Text('Second file containing all records and table rows from PostgreSQL in MessagePack.'),
            ),
            const SizedBox(height: 16),
            TextFormField(
              controller: _fileNameController,
              decoration: const InputDecoration(
                labelText: 'Destination File Name',
                border: OutlineInputBorder(),
                isDense: true,
                prefixIcon: Icon(Icons.save_as_outlined, size: 20),
              ),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _isSaving ? null : () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        ElevatedButton.icon(
          onPressed: _isSaving ? null : _handleSave,
          style: ElevatedButton.styleFrom(
            backgroundColor: const Color(0xFF1E88E5),
            foregroundColor: Colors.white,
          ),
          icon: _isSaving
              ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
              : const Icon(Icons.download, size: 18),
          label: Text(_isSaving ? 'Exporting...' : 'Save Copy'),
        ),
      ],
    );
  }
}
