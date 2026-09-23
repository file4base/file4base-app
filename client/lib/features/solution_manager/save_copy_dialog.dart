import 'dart:typed_data';
import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import '../../core/services/solution_storage.dart';

enum SaveCopyType {
  bothFiles, // .f4b + .f4data (Recommended)
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
  SaveCopyType _selectedType = SaveCopyType.bothFiles;
  late final TextEditingController _fileNameController;
  StorageDirectoryRef? _selectedDirectory;
  bool _isSaving = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _fileNameController = TextEditingController(
      text: '${widget.activeSolutionName}_copy',
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
      final base = widget.activeSolutionName;
      if (type == SaveCopyType.bothFiles) {
        _fileNameController.text = '${base}_copy';
      } else if (type == SaveCopyType.solutionOnly) {
        _fileNameController.text = '${base}_copy.f4b';
      } else {
        _fileNameController.text = '${widget.activeDatabaseName}_data.f4data';
      }
    });
  }

  Future<void> _handlePickDirectory() async {
    final picked = await SolutionStorageService.pickDirectory();
    if (picked != null && mounted) {
      setState(() {
        _selectedDirectory = picked;
      });
    }
  }

  Future<void> _handleSave() async {
    setState(() {
      _isSaving = true;
      _errorMessage = null;
    });

    try {
      final name = _fileNameController.text.trim();

      if (_selectedType == SaveCopyType.bothFiles) {
        final f4bBytes = await widget.onExportSolution();
        final f4dataBytes = await widget.apiClient.exportDatabaseData();

        await SolutionStorageService.saveDualSolutionFiles(
          baseName: name,
          f4bBytes: f4bBytes,
          f4dataBytes: f4dataBytes,
          directoryRef: _selectedDirectory,
        );
      } else if (_selectedType == SaveCopyType.solutionOnly) {
        final bytes = await widget.onExportSolution();
        final filename = name.endsWith('.f4b') ? name : '$name.f4b';
        await SolutionStorageService.saveFile(
          filename: filename,
          bytes: bytes,
        );
      } else {
        final bytes = await widget.apiClient.exportDatabaseData();
        final filename = name.endsWith('.f4data') ? name : '$name.f4data';
        await SolutionStorageService.saveFile(
          filename: filename,
          bytes: bytes,
        );
      }

      if (mounted) {
        Navigator.of(context).pop();
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Successfully exported copy of "$name" in MessagePack format.'),
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
              Text('MessagePack Dual-File Export & Location', style: TextStyle(fontSize: 12, color: Colors.grey)),
            ],
          ),
        ],
      ),
      content: SizedBox(
        width: 500,
        child: SingleChildScrollView(
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

              // Destination Folder
              Text('SAVE LOCATION ON HARD DRIVE', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey.shade600, letterSpacing: 0.5)),
              const SizedBox(height: 6),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                decoration: BoxDecoration(
                  border: Border.all(color: _selectedDirectory != null ? const Color(0xFF1E88E5) : Colors.grey.shade700),
                  borderRadius: BorderRadius.circular(6),
                  color: _selectedDirectory != null ? const Color(0xFF1E88E5).withValues(alpha: 0.08) : Colors.black12,
                ),
                child: Row(
                  children: [
                    Icon(
                      _selectedDirectory != null ? Icons.folder : Icons.folder_open,
                      color: _selectedDirectory != null ? const Color(0xFF1E88E5) : Colors.grey,
                      size: 22,
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            _selectedDirectory != null ? _selectedDirectory!.displayName : 'No destination folder selected',
                            style: TextStyle(
                              fontSize: 13,
                              fontWeight: _selectedDirectory != null ? FontWeight.bold : FontWeight.normal,
                              color: _selectedDirectory != null ? Colors.white : Colors.grey,
                            ),
                            overflow: TextOverflow.ellipsis,
                          ),
                          Text(
                            _selectedDirectory != null
                                ? 'Files will be recorded directly into this folder'
                                : 'Click to select destination directory on disk',
                            style: const TextStyle(fontSize: 11, color: Colors.grey),
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(width: 8),
                    OutlinedButton.icon(
                      onPressed: _handlePickDirectory,
                      icon: const Icon(Icons.drive_file_move_outlined, size: 16),
                      label: Text(_selectedDirectory == null ? 'Choose Folder...' : 'Change...'),
                    ),
                  ],
                ),
              ),

              const SizedBox(height: 16),
              Text('EXPORT FORMAT', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey.shade600, letterSpacing: 0.5)),
              const SizedBox(height: 6),

              InkWell(
                onTap: () => _onTypeChanged(SaveCopyType.bothFiles),
                borderRadius: BorderRadius.circular(6),
                child: Container(
                  padding: const EdgeInsets.all(8),
                  decoration: BoxDecoration(
                    border: Border.all(color: _selectedType == SaveCopyType.bothFiles ? const Color(0xFF1E88E5) : Colors.grey.shade800),
                    borderRadius: BorderRadius.circular(6),
                    color: _selectedType == SaveCopyType.bothFiles ? const Color(0xFF1E88E5).withValues(alpha: 0.08) : Colors.transparent,
                  ),
                  child: Row(
                    children: [
                      Radio<SaveCopyType>(
                        value: SaveCopyType.bothFiles,
                        groupValue: _selectedType,
                        onChanged: _onTypeChanged,
                      ),
                      const SizedBox(width: 8),
                      const Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text('Both Files (.f4b + .f4data) - Recommended', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                            Text('Exports both solution config (layouts, schemas, encoded credentials) and PostgreSQL database records.', style: TextStyle(fontSize: 11, color: Colors.grey)),
                          ],
                        ),
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 6),
              InkWell(
                onTap: () => _onTypeChanged(SaveCopyType.solutionOnly),
                borderRadius: BorderRadius.circular(6),
                child: Container(
                  padding: const EdgeInsets.all(8),
                  decoration: BoxDecoration(
                    border: Border.all(color: _selectedType == SaveCopyType.solutionOnly ? const Color(0xFF1E88E5) : Colors.grey.shade800),
                    borderRadius: BorderRadius.circular(6),
                    color: _selectedType == SaveCopyType.solutionOnly ? const Color(0xFF1E88E5).withValues(alpha: 0.08) : Colors.transparent,
                  ),
                  child: Row(
                    children: [
                      Radio<SaveCopyType>(
                        value: SaveCopyType.solutionOnly,
                        groupValue: _selectedType,
                        onChanged: _onTypeChanged,
                      ),
                      const SizedBox(width: 8),
                      const Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text('File4Base Solution (.f4b) Only', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                            Text('Layouts, schemas, occurrences, and encoded DB connection parameters.', style: TextStyle(fontSize: 11, color: Colors.grey)),
                          ],
                        ),
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 6),
              InkWell(
                onTap: () => _onTypeChanged(SaveCopyType.databaseData),
                borderRadius: BorderRadius.circular(6),
                child: Container(
                  padding: const EdgeInsets.all(8),
                  decoration: BoxDecoration(
                    border: Border.all(color: _selectedType == SaveCopyType.databaseData ? const Color(0xFF1E88E5) : Colors.grey.shade800),
                    borderRadius: BorderRadius.circular(6),
                    color: _selectedType == SaveCopyType.databaseData ? const Color(0xFF1E88E5).withValues(alpha: 0.08) : Colors.transparent,
                  ),
                  child: Row(
                    children: [
                      Radio<SaveCopyType>(
                        value: SaveCopyType.databaseData,
                        groupValue: _selectedType,
                        onChanged: _onTypeChanged,
                      ),
                      const SizedBox(width: 8),
                      const Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text('Database Data File (.f4data) Only', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                            Text('Physical records and table rows from active database.', style: TextStyle(fontSize: 11, color: Colors.grey)),
                          ],
                        ),
                      ),
                    ],
                  ),
                ),
              ),

              const SizedBox(height: 16),
              TextFormField(
                controller: _fileNameController,
                decoration: const InputDecoration(
                  labelText: 'Base File Name',
                  border: OutlineInputBorder(),
                  isDense: true,
                  prefixIcon: Icon(Icons.save_as_outlined, size: 20),
                ),
              ),
            ],
          ),
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
