import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import '../solution_manager/new_database_dialog.dart';

class DatabaseLoginDialog extends StatefulWidget {
  final ApiClient apiClient;

  const DatabaseLoginDialog({super.key, required this.apiClient});

  static Future<AuthResult?> show(BuildContext context, ApiClient apiClient) {
    return showDialog<AuthResult>(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => DatabaseLoginDialog(apiClient: apiClient),
    );
  }

  @override
  State<DatabaseLoginDialog> createState() => _DatabaseLoginDialogState();
}

class _DatabaseLoginDialogState extends State<DatabaseLoginDialog> {
  final _formKey = GlobalKey<FormState>();
  final _usernameController = TextEditingController(text: 'owner');
  final _passwordController = TextEditingController(text: 'owner');
  List<String> _databases = [];
  String? _selectedDatabase;
  bool _isLoadingDatabases = true;
  bool _isLoggingIn = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _loadDatabases();
  }

  @override
  void dispose() {
    _usernameController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  Future<void> _loadDatabases() async {
    setState(() {
      _isLoadingDatabases = true;
      _errorMessage = null;
    });

    try {
      final res = await widget.apiClient.listDatabases();
      final List<String> dbs = (res['databases'] as List<dynamic>?)
              ?.map((e) => e.toString())
              .toList() ??
          [];
      final String? active = res['active']?.toString();

      if (mounted) {
        setState(() {
          _databases = dbs;
          if (active != null && dbs.contains(active)) {
            _selectedDatabase = active;
          } else if (dbs.contains('file4base_dev')) {
            _selectedDatabase = 'file4base_dev';
          } else if (dbs.isNotEmpty) {
            _selectedDatabase = dbs.first;
          } else {
            _selectedDatabase = 'file4base_dev';
          }
          _isLoadingDatabases = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _databases = ['file4base_dev'];
          _selectedDatabase = 'file4base_dev';
          _isLoadingDatabases = false;
        });
      }
    }
  }

  Future<void> _handleNewDatabase() async {
    final result = await NewDatabaseDialog.show(context, widget.apiClient);
    if (result != null && mounted) {
      await _loadDatabases();
      setState(() {
        _selectedDatabase = result.databaseName;
      });
    }
  }

  Future<void> _handleLogin() async {
    if (!_formKey.currentState!.validate()) return;

    setState(() {
      _isLoggingIn = true;
      _errorMessage = null;
    });

    try {
      final username = _usernameController.text.trim();
      final password = _passwordController.text.trim();
      final database = _selectedDatabase ?? 'file4base_dev';

      final auth = await widget.apiClient.login(
        username: username,
        password: password,
        database: database,
      );

      if (mounted) {
        Navigator.of(context).pop(auth);
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isLoggingIn = false;
          _errorMessage = e.toString().replaceFirst('Exception: ', '').replaceFirst('Authentication failed: ', '');
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Dialog(
      backgroundColor: Colors.transparent,
      child: Container(
        width: 440,
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.surface,
          borderRadius: BorderRadius.circular(16),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: 0.35),
              blurRadius: 24,
              offset: const Offset(0, 8),
            ),
          ],
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            // Header
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 20),
              decoration: BoxDecoration(
                color: const Color(0xFF1E88E5).withValues(alpha: 0.1),
                borderRadius: const BorderRadius.vertical(top: Radius.circular(16)),
              ),
              child: Row(
                children: [
                  Container(
                    padding: const EdgeInsets.all(10),
                    decoration: BoxDecoration(
                      color: const Color(0xFF1E88E5).withValues(alpha: 0.2),
                      borderRadius: BorderRadius.circular(10),
                    ),
                    child: const Icon(Icons.storage, color: Color(0xFF1E88E5), size: 28),
                  ),
                  const SizedBox(width: 16),
                  const Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'File4Base Login',
                          style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
                        ),
                        Text(
                          'Select Database & Authenticate',
                          style: TextStyle(fontSize: 12, color: Colors.grey),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),

            // Form Body
            Padding(
              padding: const EdgeInsets.all(24.0),
              child: Form(
                key: _formKey,
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
                            Expanded(
                              child: Text(
                                _errorMessage!,
                                style: const TextStyle(color: Colors.red, fontSize: 13),
                              ),
                            ),
                          ],
                        ),
                      ),

                    // Database Selector
                    const Text(
                      'POSTGRESQL DATABASE',
                      style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey, letterSpacing: 0.5),
                    ),
                    const SizedBox(height: 6),
                    Row(
                      children: [
                        Expanded(
                          child: _isLoadingDatabases
                              ? const LinearProgressIndicator()
                              : DropdownButtonFormField<String>(
                                  value: _selectedDatabase,
                                  decoration: const InputDecoration(
                                    border: OutlineInputBorder(),
                                    isDense: true,
                                    prefixIcon: Icon(Icons.dataset_outlined, size: 20),
                                  ),
                                  items: _databases.map((db) {
                                    return DropdownMenuItem<String>(
                                      value: db,
                                      child: Text(db, style: const TextStyle(fontWeight: FontWeight.w600)),
                                    );
                                  }).toList(),
                                  onChanged: (val) {
                                    if (val != null) setState(() => _selectedDatabase = val);
                                  },
                                ),
                        ),
                        const SizedBox(width: 8),
                        IconButton.outlined(
                          tooltip: 'Create New Database in PostgreSQL',
                          onPressed: _handleNewDatabase,
                          icon: const Icon(Icons.add, size: 20),
                        ),
                      ],
                    ),

                    const SizedBox(height: 16),
                    const Text(
                      'USER CREDENTIALS',
                      style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey, letterSpacing: 0.5),
                    ),
                    const SizedBox(height: 6),
                    TextFormField(
                      controller: _usernameController,
                      decoration: const InputDecoration(
                        labelText: 'Username',
                        border: OutlineInputBorder(),
                        isDense: true,
                        prefixIcon: Icon(Icons.person_outline, size: 20),
                      ),
                      validator: (v) => (v == null || v.trim().isEmpty) ? 'Username required' : null,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _passwordController,
                      decoration: const InputDecoration(
                        labelText: 'Password',
                        border: OutlineInputBorder(),
                        isDense: true,
                        prefixIcon: Icon(Icons.lock_outline, size: 20),
                      ),
                      obscureText: true,
                      validator: (v) => (v == null || v.trim().isEmpty) ? 'Password required' : null,
                      onFieldSubmitted: (_) => _handleLogin(),
                    ),

                    const SizedBox(height: 12),
                    Container(
                      padding: const EdgeInsets.all(8),
                      decoration: BoxDecoration(
                        color: Colors.blue.withValues(alpha: 0.08),
                        borderRadius: BorderRadius.circular(6),
                      ),
                      child: const Row(
                        children: [
                          Icon(Icons.info_outline, size: 16, color: Color(0xFF1E88E5)),
                          SizedBox(width: 8),
                          Expanded(
                            child: Text(
                              'Default initial administrator is "owner" with password "owner".',
                              style: TextStyle(fontSize: 11, color: Colors.grey),
                            ),
                          ),
                        ],
                      ),
                    ),

                    const SizedBox(height: 24),
                    SizedBox(
                      width: double.infinity,
                      height: 44,
                      child: ElevatedButton.icon(
                        onPressed: _isLoggingIn ? null : _handleLogin,
                        style: ElevatedButton.styleFrom(
                          backgroundColor: const Color(0xFF1E88E5),
                          foregroundColor: Colors.white,
                          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                        ),
                        icon: _isLoggingIn
                            ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                            : const Icon(Icons.login, size: 20),
                        label: Text(
                          _isLoggingIn ? 'Authenticating...' : 'Connect & Log In',
                          style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 14),
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
