import 'dart:typed_data';
import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import '../../core/models/solution_models.dart';
import '../../core/services/solution_storage.dart';

class NewDatabaseDialogResult {
  final String fileName;
  final String databaseName;
  final String databasePassword;
  final SolutionPackage package;
  final StorageDirectoryRef? directoryRef;

  const NewDatabaseDialogResult({
    required this.fileName,
    required this.databaseName,
    required this.databasePassword,
    required this.package,
    this.directoryRef,
  });
}

class NewDatabaseDialog extends StatefulWidget {
  final ApiClient apiClient;

  const NewDatabaseDialog({super.key, required this.apiClient});

  static Future<NewDatabaseDialogResult?> show(BuildContext context, ApiClient apiClient) {
    return showDialog<NewDatabaseDialogResult>(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => NewDatabaseDialog(apiClient: apiClient),
    );
  }

  @override
  State<NewDatabaseDialog> createState() => _NewDatabaseDialogState();
}

class _NewDatabaseDialogState extends State<NewDatabaseDialog> {
  final _formKey = GlobalKey<FormState>();
  final _fileNameController = TextEditingController(text: 'my_solution.f4b');
  final _solutionNameController = TextEditingController(text: 'My Solution');
  final _dbNameController = TextEditingController(text: 'my_solution_db');
  final _hostController = TextEditingController(text: 'localhost');
  final _portController = TextEditingController(text: '5432');
  final _userController = TextEditingController(text: 'admin');
  final _passwordController = TextEditingController(text: 'admin');
  StorageDirectoryRef? _selectedDirectory;
  bool _autoCreateDb = true;
  bool _isCreating = false;
  String? _errorMessage;

  @override
  void dispose() {
    _fileNameController.dispose();
    _solutionNameController.dispose();
    _dbNameController.dispose();
    _hostController.dispose();
    _portController.dispose();
    _userController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  void _onSolutionNameChanged(String val) {
    final sanitized = val.toLowerCase().trim().replaceAll(RegExp(r'[^a-z0-9_]'), '_');
    if (sanitized.isNotEmpty) {
      _fileNameController.text = '$sanitized.f4b';
      _dbNameController.text = '${sanitized}_db';
    }
  }

  Future<void> _handlePickDirectory() async {
    final picked = await SolutionStorageService.pickDirectory();
    if (picked != null && mounted) {
      setState(() {
        _selectedDirectory = picked;
      });
    }
  }

  Future<void> _handleCreate() async {
    if (!_formKey.currentState!.validate()) return;

    setState(() {
      _isCreating = true;
      _errorMessage = null;
    });

    try {
      final fileName = _fileNameController.text.trim();
      final solutionName = _solutionNameController.text.trim();
      final dbName = _dbNameController.text.trim().toLowerCase();
      final host = _hostController.text.trim();
      final port = int.tryParse(_portController.text.trim()) ?? 5432;
      final user = _userController.text.trim().isNotEmpty ? _userController.text.trim() : dbName;
      final password = _passwordController.text.trim().isNotEmpty ? _passwordController.text.trim() : dbName;

      // 1. Create database in PostgreSQL if requested
      if (_autoCreateDb) {
        await widget.apiClient.createDatabase(dbName, user: user, password: password);
      } else {
        await widget.apiClient.switchDatabase(dbName);
      }

      // 2. Build connection configuration (credentials encoded when serialized)
      final dbConfig = DatabaseConnectionConfig(
        engine: 'postgres',
        host: host,
        port: port,
        database: dbName,
        user: user,
        password: password,
      );

      // 3. Create initial solution package in MessagePack format
      final pkg = SolutionPackage(
        solutionName: solutionName,
        databaseConnection: dbConfig,
        tables: const [],
        tableOccurrences: const [],
        layouts: const [],
      );

      final f4bBytes = pkg.toMsgPack();

      // 4. Export initial active database data (.f4data)
      Uint8List f4dataBytes;
      try {
        f4dataBytes = await widget.apiClient.exportDatabaseData();
      } catch (_) {
        f4dataBytes = Uint8List(0);
      }

      final baseName = fileName.replaceAll(RegExp(r'\.(f4b|f4data)$'), '');

      // 5. Save BOTH .f4b and .f4data to user's selected hard drive folder
      await SolutionStorageService.saveDualSolutionFiles(
        baseName: baseName,
        f4bBytes: f4bBytes,
        f4dataBytes: f4dataBytes,
        directoryRef: _selectedDirectory,
      );

      if (mounted) {
        Navigator.of(context).pop(NewDatabaseDialogResult(
          fileName: '$baseName.f4b',
          databaseName: dbName,
          databasePassword: password,
          package: pkg,
          directoryRef: _selectedDirectory,
        ));
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isCreating = false;
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
            child: const Icon(Icons.note_add_outlined, color: Color(0xFF1E88E5), size: 24),
          ),
          const SizedBox(width: 12),
          const Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('New Database Solution', style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
              Text('MessagePack (.f4b) & PostgreSQL Connection', style: TextStyle(fontSize: 12, color: Colors.grey)),
            ],
          ),
        ],
      ),
      content: SizedBox(
        width: 520,
        child: Form(
          key: _formKey,
          child: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (_errorMessage != null)
                  Container(
                    margin: const EdgeInsets.only(bottom: 16),
                    padding: const EdgeInsets.all(10),
                    decoration: BoxDecoration(
                      color: Colors.red.shade900.withValues(alpha: 0.2),
                      border: Border.all(color: Colors.red.shade400),
                      borderRadius: BorderRadius.circular(6),
                    ),
                    child: Row(
                      children: [
                        const Icon(Icons.error_outline, color: Colors.red, size: 20),
                        const SizedBox(width: 8),
                        Expanded(child: Text(_errorMessage!, style: const TextStyle(color: Colors.red, fontSize: 13))),
                      ],
                    ),
                  ),

                // Section 1: Destination Folder on Hard Drive
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
                                  ? 'Saves both .f4b (config/layouts) & .f4data (records) in this folder'
                                  : 'Click to select where on your hard drive to record your solution',
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
                // Section 2: Solution File
                Text('SOLUTION DEFINITION', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey.shade600, letterSpacing: 0.5)),
                const SizedBox(height: 8),
                TextFormField(
                  controller: _solutionNameController,
                  decoration: const InputDecoration(
                    labelText: 'Solution Name',
                    hintText: 'e.g. Invoicing & Contacts',
                    border: OutlineInputBorder(),
                    isDense: true,
                  ),
                  onChanged: _onSolutionNameChanged,
                  validator: (v) => (v == null || v.trim().isEmpty) ? 'Required' : null,
                ),
                const SizedBox(height: 10),
                TextFormField(
                  controller: _fileNameController,
                  decoration: const InputDecoration(
                    labelText: 'Base File Name (.f4b & .f4data)',
                    hintText: 'e.g. invoices.f4b',
                    helperText: 'Creates <name>.f4b (layouts & encoded credentials) and <name>.f4data (active database rows)',
                    border: OutlineInputBorder(),
                    isDense: true,
                    prefixIcon: Icon(Icons.file_present_outlined, size: 20),
                  ),
                  validator: (v) {
                    if (v == null || v.trim().isEmpty) return 'Required';
                    return null;
                  },
                ),

                const SizedBox(height: 20),
                // Section 3: PostgreSQL Database
                Text('POSTGRESQL DATABASE CONNECTION', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey.shade600, letterSpacing: 0.5)),
                const SizedBox(height: 4),
                const Text('Credentials will be encoded and stored securely inside the .f4b MessagePack file', style: TextStyle(fontSize: 11, color: Colors.grey)),
                const SizedBox(height: 8),
                TextFormField(
                  controller: _dbNameController,
                  decoration: const InputDecoration(
                    labelText: 'Database Name',
                    hintText: 'e.g. invoices_db',
                    helperText: 'Physical database name in PostgreSQL',
                    border: OutlineInputBorder(),
                    isDense: true,
                    prefixIcon: Icon(Icons.storage_outlined, size: 20),
                  ),
                  validator: (v) {
                    if (v == null || v.trim().isEmpty) return 'Required';
                    if (!RegExp(r'^[a-z][a-z0-9_]*$').hasMatch(v.trim())) {
                      return 'Must be lowercase letters, numbers, and underscores';
                    }
                    return null;
                  },
                ),
                const SizedBox(height: 10),
                Row(
                  children: [
                    Expanded(
                      flex: 3,
                      child: TextFormField(
                        controller: _hostController,
                        decoration: const InputDecoration(
                          labelText: 'Host / Access',
                          border: OutlineInputBorder(),
                          isDense: true,
                        ),
                        validator: (v) => (v == null || v.trim().isEmpty) ? 'Required' : null,
                      ),
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      flex: 1,
                      child: TextFormField(
                        controller: _portController,
                        decoration: const InputDecoration(
                          labelText: 'Port',
                          border: OutlineInputBorder(),
                          isDense: true,
                        ),
                        keyboardType: TextInputType.number,
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 10),
                Row(
                  children: [
                    Expanded(
                      child: TextFormField(
                        controller: _userController,
                        decoration: const InputDecoration(
                          labelText: 'User',
                          hintText: 'admin',
                          helperText: 'Account username (e.g. admin)',
                          border: OutlineInputBorder(),
                          isDense: true,
                        ),
                      ),
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: TextFormField(
                        controller: _passwordController,
                        decoration: const InputDecoration(
                          labelText: 'Password',
                          hintText: '••••••',
                          helperText: 'Account password',
                          border: OutlineInputBorder(),
                          isDense: true,
                        ),
                        obscureText: true,
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  dense: true,
                  title: const Text('Create physical database in PostgreSQL if it does not exist', style: TextStyle(fontSize: 13)),
                  subtitle: const Text('Executes CREATE DATABASE and initializes system catalog schema', style: TextStyle(fontSize: 11, color: Colors.grey)),
                  value: _autoCreateDb,
                  onChanged: (val) => setState(() => _autoCreateDb = val ?? true),
                ),
              ],
            ),
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: _isCreating ? null : () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        ElevatedButton.icon(
          onPressed: _isCreating ? null : _handleCreate,
          style: ElevatedButton.styleFrom(
            backgroundColor: const Color(0xFF1E88E5),
            foregroundColor: Colors.white,
          ),
          icon: _isCreating
              ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
              : const Icon(Icons.check, size: 18),
          label: Text(_isCreating ? 'Creating...' : 'Create & Save Solution'),
        ),
      ],
    );
  }
}
