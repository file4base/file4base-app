import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import '../../core/models/solution_models.dart';
import '../../core/services/solution_storage.dart';

enum OpenSolutionType { serverDatabase, localFile }

class OpenSolutionResult {
  final OpenSolutionType type;
  final String databaseName;
  final UserModel user;
  final AuthResult auth;
  final String? fileName;
  final String? solutionName;
  final StorageDirectoryRef? directoryRef;

  const OpenSolutionResult({
    required this.type,
    required this.databaseName,
    required this.user,
    required this.auth,
    this.fileName,
    this.solutionName,
    this.directoryRef,
  });
}

class OpenSolutionDialog extends StatefulWidget {
  final ApiClient apiClient;
  final int initialTab; // 0: Server Databases, 1: Local File

  const OpenSolutionDialog({
    super.key,
    required this.apiClient,
    this.initialTab = 0,
  });

  static Future<OpenSolutionResult?> show(
    BuildContext context,
    ApiClient apiClient, {
    int initialTab = 0,
  }) {
    return showDialog<OpenSolutionResult>(
      context: context,
      barrierDismissible: true,
      builder: (ctx) => OpenSolutionDialog(
        apiClient: apiClient,
        initialTab: initialTab,
      ),
    );
  }

  @override
  State<OpenSolutionDialog> createState() => _OpenSolutionDialogState();
}

class _OpenSolutionDialogState extends State<OpenSolutionDialog> with SingleTickerProviderStateMixin {
  late TabController _tabController;

  // Server Databases State
  List<String> _databases = [];
  String? _activeDb;
  String? _selectedServerDb;
  bool _isLoadingDatabases = true;
  bool _isConnectingServer = false;
  String? _serverError;
  final _serverUserCtrl = TextEditingController(text: 'admin');
  final _serverPassCtrl = TextEditingController(text: 'admin');

  // Local File State
  PickedSolutionFile? _pickedFile;
  SolutionPackage? _parsedPkg;
  bool _isOpeningFile = false;
  String? _fileError;
  final _fileUserCtrl = TextEditingController(text: 'admin');
  final _filePassCtrl = TextEditingController(text: 'admin');

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 2, vsync: this, initialIndex: widget.initialTab);
    _loadDatabases();
  }

  @override
  void dispose() {
    _tabController.dispose();
    _serverUserCtrl.dispose();
    _serverPassCtrl.dispose();
    _fileUserCtrl.dispose();
    _filePassCtrl.dispose();
    super.dispose();
  }

  Future<void> _loadDatabases() async {
    setState(() {
      _isLoadingDatabases = true;
      _serverError = null;
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
          _activeDb = active;
          if (active != null && dbs.contains(active)) {
            _selectedServerDb = active;
          } else if (dbs.isNotEmpty) {
            _selectedServerDb = dbs.first;
          }
          _isLoadingDatabases = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _serverError = 'Failed to load databases: $e';
          _isLoadingDatabases = false;
        });
      }
    }
  }

  Future<void> _handleServerConnect() async {
    if (_selectedServerDb == null) return;

    setState(() {
      _isConnectingServer = true;
      _serverError = null;
    });

    try {
      final user = _serverUserCtrl.text.trim();
      final pass = _serverPassCtrl.text.trim();
      final targetDb = _selectedServerDb!;

      final auth = await widget.apiClient.login(
        username: user,
        password: pass,
        database: targetDb,
      );

      if (mounted) {
        Navigator.of(context).pop(OpenSolutionResult(
          type: OpenSolutionType.serverDatabase,
          databaseName: targetDb,
          user: auth.user,
          auth: auth,
        ));
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isConnectingServer = false;
          _serverError = e.toString().replaceFirst('Exception: ', '').replaceFirst('Authentication failed: ', '');
        });
      }
    }
  }

  Future<void> _handlePickLocalFile() async {
    setState(() {
      _fileError = null;
    });

    try {
      final file = await SolutionStorageService.pickFile(allowedExtensions: ['f4b']);
      if (file == null) return;

      final pkg = SolutionPackage.fromMsgPack(file.bytes);

      setState(() {
        _pickedFile = file;
        _parsedPkg = pkg;

        // Pre-fill decoded credentials if available in the file package
        final pkgUser = pkg.databaseConnection.user;
        final pkgPass = DatabaseConnectionConfig.decodeCredential(pkg.databaseConnection.password);
        if (pkgUser.isNotEmpty) {
          _fileUserCtrl.text = pkgUser;
        }
        if (pkgPass.isNotEmpty) {
          _filePassCtrl.text = pkgPass;
        }
      });
    } catch (e) {
      setState(() {
        _fileError = 'Invalid or corrupt .f4b solution file: $e';
      });
    }
  }

  Future<void> _handleOpenFile() async {
    if (_pickedFile == null || _parsedPkg == null) return;

    setState(() {
      _isOpeningFile = true;
      _fileError = null;
    });

    try {
      final pkg = _parsedPkg!;
      final targetDb = pkg.databaseConnection.database;
      final user = _fileUserCtrl.text.trim();
      final pass = _filePassCtrl.text.trim();

      // 1. Switch or ensure database context on server
      try {
        await widget.apiClient.switchDatabase(targetDb);
      } catch (_) {
        // If database does not exist on server, create it
        await widget.apiClient.createDatabase(targetDb, user: user, password: pass);
      }

      // 2. Import solution schemas, tables, and layouts into active server database
      await widget.apiClient.importSolution(_pickedFile!.bytes);

      // 3. Authenticate user into this database
      final auth = await widget.apiClient.login(
        username: user,
        password: pass,
        database: targetDb,
      );

      if (mounted) {
        Navigator.of(context).pop(OpenSolutionResult(
          type: OpenSolutionType.localFile,
          databaseName: targetDb,
          fileName: _pickedFile!.name,
          solutionName: pkg.solutionName,
          user: auth.user,
          auth: auth,
        ));
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isOpeningFile = false;
          _fileError = e.toString().replaceFirst('Exception: ', '').replaceFirst('Authentication failed: ', '');
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Dialog(
      backgroundColor: Colors.transparent,
      child: Container(
        width: 540,
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
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // Header
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 18),
              decoration: BoxDecoration(
                color: const Color(0xFF1E88E5).withValues(alpha: 0.12),
                borderRadius: const BorderRadius.vertical(top: Radius.circular(16)),
              ),
              child: Row(
                children: [
                  Container(
                    padding: const EdgeInsets.all(8),
                    decoration: BoxDecoration(
                      color: const Color(0xFF1E88E5).withValues(alpha: 0.2),
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: const Icon(Icons.folder_open_outlined, color: Color(0xFF1E88E5), size: 26),
                  ),
                  const SizedBox(width: 14),
                  const Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('Open Solution or Database', style: TextStyle(fontSize: 17, fontWeight: FontWeight.bold)),
                        Text('Select from server catalog or open a local .f4b file', style: TextStyle(fontSize: 12, color: Colors.grey)),
                      ],
                    ),
                  ),
                  IconButton(
                    onPressed: () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close, size: 20),
                    tooltip: 'Close',
                  ),
                ],
              ),
            ),

            // Tabs Selector
            Container(
              decoration: BoxDecoration(
                border: Border(bottom: BorderSide(color: Colors.grey.withValues(alpha: 0.2))),
              ),
              child: TabBar(
                controller: _tabController,
                indicatorColor: const Color(0xFF1E88E5),
                indicatorWeight: 3,
                labelColor: const Color(0xFF1E88E5),
                unselectedLabelColor: Colors.grey,
                tabs: const [
                  Tab(icon: Icon(Icons.cloud_outlined, size: 18), text: 'Server Databases (API)'),
                  Tab(icon: Icon(Icons.file_present_outlined, size: 18), text: 'Local File (.f4b)'),
                ],
              ),
            ),

            // Tab Views Container
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 460),
              child: TabBarView(
                controller: _tabController,
                children: [
                  _buildServerTab(),
                  _buildLocalFileTab(),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildServerTab() {
    return SingleChildScrollView(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (_serverError != null) ...[
            Container(
              padding: const EdgeInsets.all(10),
              margin: const EdgeInsets.only(bottom: 14),
              decoration: BoxDecoration(
                color: Colors.red.shade900.withValues(alpha: 0.2),
                border: Border.all(color: Colors.red.shade400),
                borderRadius: BorderRadius.circular(6),
              ),
              child: Row(
                children: [
                  const Icon(Icons.error_outline, color: Colors.red, size: 18),
                  const SizedBox(width: 8),
                  Expanded(child: Text(_serverError!, style: const TextStyle(color: Colors.red, fontSize: 12))),
                ],
              ),
            ),
          ],

          Row(
            children: [
              const Text('DATABASES ON SERVER', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey, letterSpacing: 0.5)),
              const Spacer(),
              IconButton(
                onPressed: _isLoadingDatabases ? null : _loadDatabases,
                icon: const Icon(Icons.refresh, size: 18),
                tooltip: 'Refresh Server Databases',
                constraints: const BoxConstraints(minWidth: 32, minHeight: 32),
                padding: EdgeInsets.zero,
              ),
            ],
          ),
          const SizedBox(height: 6),

          if (_isLoadingDatabases)
            const Center(
              child: Padding(
                padding: EdgeInsets.all(24.0),
                child: CircularProgressIndicator(),
              ),
            )
          else if (_databases.isEmpty)
            Container(
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                border: Border.all(color: Colors.grey.withValues(alpha: 0.2)),
                borderRadius: BorderRadius.circular(8),
              ),
              child: const Center(
                child: Text('No databases found on server API.', style: TextStyle(color: Colors.grey, fontSize: 13)),
              ),
            )
          else
            Container(
              constraints: const BoxConstraints(maxHeight: 160),
              decoration: BoxDecoration(
                border: Border.all(color: Colors.grey.withValues(alpha: 0.3)),
                borderRadius: BorderRadius.circular(8),
              ),
              child: ListView.separated(
                shrinkWrap: true,
                itemCount: _databases.length,
                separatorBuilder: (_, __) => const Divider(height: 1),
                itemBuilder: (context, idx) {
                  final db = _databases[idx];
                  final isSelected = _selectedServerDb == db;
                  final isActive = _activeDb == db;

                  return ListTile(
                    dense: true,
                    selected: isSelected,
                    selectedTileColor: const Color(0xFF1E88E5).withValues(alpha: 0.15),
                    leading: Icon(Icons.dataset_outlined, color: isSelected ? const Color(0xFF1E88E5) : Colors.grey, size: 20),
                    title: Text(db, style: TextStyle(fontWeight: isSelected ? FontWeight.bold : FontWeight.normal, fontSize: 13)),
                    trailing: isActive
                        ? Container(
                            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                            decoration: BoxDecoration(
                              color: Colors.green.shade900.withValues(alpha: 0.3),
                              border: Border.all(color: Colors.green.shade400, width: 0.8),
                              borderRadius: BorderRadius.circular(4),
                            ),
                            child: const Text('ACTIVE', style: TextStyle(fontSize: 10, color: Colors.green, fontWeight: FontWeight.bold)),
                          )
                        : null,
                    onTap: () => setState(() => _selectedServerDb = db),
                  );
                },
              ),
            ),

          const SizedBox(height: 18),
          Text(
            'CREDENTIALS FOR: ${_selectedServerDb ?? "SELECT DATABASE"}',
            style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey, letterSpacing: 0.5),
          ),
          const SizedBox(height: 8),

          Row(
            children: [
              Expanded(
                child: TextField(
                  controller: _serverUserCtrl,
                  decoration: const InputDecoration(
                    labelText: 'Username',
                    hintText: 'admin',
                    border: OutlineInputBorder(),
                    isDense: true,
                    prefixIcon: Icon(Icons.person_outline, size: 20),
                  ),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: TextField(
                  controller: _serverPassCtrl,
                  obscureText: true,
                  decoration: const InputDecoration(
                    labelText: 'Password',
                    hintText: '••••••',
                    border: OutlineInputBorder(),
                    isDense: true,
                    prefixIcon: Icon(Icons.lock_outline, size: 20),
                  ),
                  onSubmitted: (_) => _handleServerConnect(),
                ),
              ),
            ],
          ),

          const SizedBox(height: 18),
          SizedBox(
            width: double.infinity,
            child: ElevatedButton.icon(
              onPressed: _selectedServerDb == null || _isConnectingServer ? null : _handleServerConnect,
              style: ElevatedButton.styleFrom(
                backgroundColor: const Color(0xFF1E88E5),
                foregroundColor: Colors.white,
                padding: const EdgeInsets.symmetric(vertical: 12),
              ),
              icon: _isConnectingServer
                  ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                  : const Icon(Icons.login, size: 18),
              label: Text(_isConnectingServer ? 'Authenticating...' : 'Open Database: ${_selectedServerDb ?? ""}'),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildLocalFileTab() {
    return SingleChildScrollView(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (_fileError != null) ...[
            Container(
              padding: const EdgeInsets.all(10),
              margin: const EdgeInsets.only(bottom: 14),
              decoration: BoxDecoration(
                color: Colors.red.shade900.withValues(alpha: 0.2),
                border: Border.all(color: Colors.red.shade400),
                borderRadius: BorderRadius.circular(6),
              ),
              child: Row(
                children: [
                  const Icon(Icons.error_outline, color: Colors.red, size: 18),
                  const SizedBox(width: 8),
                  Expanded(child: Text(_fileError!, style: const TextStyle(color: Colors.red, fontSize: 12))),
                ],
              ),
            ),
          ],

          const Text('LOCAL SOLUTION FILE (.f4b)', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey, letterSpacing: 0.5)),
          const SizedBox(height: 8),

          Container(
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(
              border: Border.all(color: _pickedFile != null ? const Color(0xFF1E88E5) : Colors.grey.withValues(alpha: 0.3)),
              borderRadius: BorderRadius.circular(8),
              color: _pickedFile != null ? const Color(0xFF1E88E5).withValues(alpha: 0.08) : Colors.black12,
            ),
            child: Row(
              children: [
                Icon(
                  _pickedFile != null ? Icons.file_present : Icons.drive_folder_upload_outlined,
                  color: _pickedFile != null ? const Color(0xFF1E88E5) : Colors.grey,
                  size: 28,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        _pickedFile != null ? _pickedFile!.name : 'No .f4b file selected',
                        style: TextStyle(fontWeight: _pickedFile != null ? FontWeight.bold : FontWeight.normal, fontSize: 13),
                        overflow: TextOverflow.ellipsis,
                      ),
                      Text(
                        _parsedPkg != null
                            ? 'Solution: "${_parsedPkg!.solutionName}" | Target DB: "${_parsedPkg!.databaseConnection.database}"'
                            : 'Select a solution file previously saved on your computer',
                        style: const TextStyle(fontSize: 11, color: Colors.grey),
                        overflow: TextOverflow.ellipsis,
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 8),
                OutlinedButton.icon(
                  onPressed: _handlePickLocalFile,
                  icon: const Icon(Icons.folder_open, size: 16),
                  label: Text(_pickedFile == null ? 'Choose File...' : 'Change...'),
                ),
              ],
            ),
          ),

          if (_parsedPkg != null) ...[
            const SizedBox(height: 18),
            Text(
              'DATABASE ACCESS CREDENTIALS FOR: "${_parsedPkg!.databaseConnection.database}"',
              style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey, letterSpacing: 0.5),
            ),
            const SizedBox(height: 4),
            const Text(
              'Enter your user credentials to access and restore this solution database',
              style: TextStyle(fontSize: 11, color: Colors.grey),
            ),
            const SizedBox(height: 8),

            Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _fileUserCtrl,
                    decoration: const InputDecoration(
                      labelText: 'Username',
                      hintText: 'admin',
                      border: OutlineInputBorder(),
                      isDense: true,
                      prefixIcon: Icon(Icons.person_outline, size: 20),
                    ),
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: TextField(
                    controller: _filePassCtrl,
                    obscureText: true,
                    decoration: const InputDecoration(
                      labelText: 'Password',
                      hintText: '••••••',
                      border: OutlineInputBorder(),
                      isDense: true,
                      prefixIcon: Icon(Icons.lock_outline, size: 20),
                    ),
                    onSubmitted: (_) => _handleOpenFile(),
                  ),
                ),
              ],
            ),

            const SizedBox(height: 18),
            SizedBox(
              width: double.infinity,
              child: ElevatedButton.icon(
                onPressed: _isOpeningFile ? null : _handleOpenFile,
                style: ElevatedButton.styleFrom(
                  backgroundColor: const Color(0xFF1E88E5),
                  foregroundColor: Colors.white,
                  padding: const EdgeInsets.symmetric(vertical: 12),
                ),
                icon: _isOpeningFile
                    ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                    : const Icon(Icons.check, size: 18),
                label: Text(_isOpeningFile ? 'Opening Solution...' : 'Open Solution: ${_parsedPkg!.solutionName}'),
              ),
            ),
          ],
        ],
      ),
    );
  }
}
