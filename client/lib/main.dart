import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'core/api/api_client.dart';
import 'core/models/solution_models.dart';
import 'core/services/auto_save_service.dart';
import 'core/services/solution_storage.dart';
import 'core/system/environment_checker.dart';
import 'core/widgets/file4base_menu_bar.dart';
import 'core/widgets/file4base_status_sidebar.dart' show File4BaseStatusSidebar, LayoutTool;
import 'features/about/about_dialog.dart';
import 'features/auth/database_login_dialog.dart';
import 'features/connection/server_connection_dialog.dart';
import 'features/data_browser/data_browser_widget.dart';
import 'features/layout_engine/layout_designer_widget.dart';
import 'features/layout_engine/layout_preview_widget.dart';
import 'features/layout_engine/models/layout_definition.dart';
import 'features/preflight/preflight_dialog.dart';
import 'features/schema_manager/manage_database_dialog.dart';
import 'features/security/manage_security_dialog.dart';
import 'features/solution_manager/new_database_dialog.dart';
import 'features/solution_manager/open_solution_dialog.dart';
import 'features/solution_manager/save_copy_dialog.dart';

enum OperationalMode {
  browse,
  find,
  layout,
  preview,
}

class ServerUrlNotifier extends Notifier<String> {
  @override
  String build() {
    if (kIsWeb) {
      final origin = Uri.base.origin;
      if (origin.isNotEmpty && !origin.startsWith('null') && !origin.startsWith('file')) {
        return origin;
      }
    }
    return 'http://localhost:8080';
  }

  void setUrl(String url) => state = url;
}

final serverUrlProvider =
    NotifierProvider<ServerUrlNotifier, String>(ServerUrlNotifier.new);

final apiClientProvider = Provider<ApiClient>((ref) {
  final baseUrl = ref.watch(serverUrlProvider);
  final client = ApiClient(baseUrl: baseUrl);
  ref.onDispose(() => client.close());
  return client;
});

class OperationalModeNotifier extends Notifier<OperationalMode> {
  @override
  OperationalMode build() => OperationalMode.browse;

  void setMode(OperationalMode mode) => state = mode;
}

final operationalModeProvider =
    NotifierProvider<OperationalModeNotifier, OperationalMode>(
  OperationalModeNotifier.new,
);

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(const ProviderScope(child: File4BaseApp()));
}

class File4BaseApp extends StatelessWidget {
  const File4BaseApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'File4Base',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xFF1E88E5),
          brightness: Brightness.light,
        ),
        useMaterial3: true,
      ),
      darkTheme: ThemeData(
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xFF1E88E5),
          brightness: Brightness.dark,
        ),
        useMaterial3: true,
      ),
      home: const WorkspaceShell(),
    );
  }
}

class WorkspaceShell extends ConsumerStatefulWidget {
  const WorkspaceShell({super.key});

  @override
  ConsumerState<WorkspaceShell> createState() => _WorkspaceShellState();
}

class _WorkspaceShellState extends ConsumerState<WorkspaceShell> {
  String _serverStatus = 'Checking...';
  String _activeSolutionFileName = 'Untitled.f4p';
  String _activeSolutionName = 'Untitled Solution';
  String _activeDatabaseName = 'file4base_dev';
  StorageDirectoryRef? _activeSolutionDirectory;
  List<TableModel> _tables = [];
  TableModel? _selectedTable;
  LayoutDefinitionModel? _activeLayout;
  bool _isLoadingTables = false;
  bool _isToolbarVisible = true;
  final GlobalKey<DataBrowserWidgetState> _dataBrowserKey = GlobalKey<DataBrowserWidgetState>();
  final GlobalKey<LayoutDesignerWidgetState> _layoutDesignerKey = GlobalKey<LayoutDesignerWidgetState>();
  int _currentRecordIndex = 0;
  int _totalRecords = 0;
  bool _isFindOmit = false;
  LayoutTool _activeLayoutTool = LayoutTool.pointer;
  UserModel? _currentUser;
  List<LayoutModel> _serverLayouts = [];
  Map<String, String> _userPermissions = {};

  @override
  void initState() {
    super.initState();
    // Configure auto-save service with the solution bytes builder
    AutoSaveService.instance.configure(
      buildSolutionBytes: _exportCurrentSolutionBytes,
      directory: null, // will be set when user picks a directory
      baseName: 'Untitled',
    );
    WidgetsBinding.instance.addPostFrameCallback((_) {
      PreflightDialog.showIfNeeded(context, onProceed: () {
        _startAuthSequence();
      });
    });
  }

  @override
  void dispose() {
    AutoSaveService.instance.dispose();
    super.dispose();
  }

  Future<void> _startAuthSequence() async {
    if (EnvironmentChecker.isTestMode) {
      if (mounted) {
        setState(() {
          _currentUser = UserModel(id: 'test-owner', username: 'file4base_dev', role: 'owner');
          _activeDatabaseName = 'file4base_dev';
          _serverStatus = 'Online (Test)';
        });
      }
      return;
    }

    final client = ref.read(apiClientProvider);
    try {
      final health = await client.checkHealth();
      if (mounted) {
        setState(() {
          _serverStatus = 'Online (${health['engine']})';
          if (health['active_database'] != null) {
            _activeDatabaseName = health['active_database'].toString();
          }
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          _serverStatus = 'Offline';
        });
      }
    }

    if (!mounted) return;

    final auth = await DatabaseLoginDialog.show(context, client);
    if (auth != null && mounted) {
      setState(() {
        _currentUser = auth.user;
        _activeDatabaseName = auth.database;
        _serverStatus = 'Online (PostgreSQL - ${auth.user.username})';
      });
      await _loadUserPermissions();
      await _loadTables();
    } else {
      await _checkServer();
    }
  }

  Future<void> _checkServer() async {
    final client = ref.read(apiClientProvider);
    try {
      final health = await client.checkHealth();
      if (mounted) {
        setState(() {
          _serverStatus = 'Online (${health['engine']})';
          if (health['active_database'] != null) {
            _activeDatabaseName = health['active_database'].toString();
          }
        });
        _loadTables();
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          _serverStatus = 'Offline';
        });
      }
    }
  }

  Future<void> _loadUserPermissions() async {
    if (_currentUser == null) return;
    if (_currentUser!.role == 'owner' || _currentUser!.role == 'admin') {
      if (mounted) setState(() => _userPermissions = {});
      return;
    }
    final client = ref.read(apiClientProvider);
    try {
      final perms = await client.getUserPermissions(_currentUser!.id);
      if (mounted) {
        setState(() {
          _userPermissions = {for (final p in perms) p.layoutId: p.accessLevel};
        });
      }
    } catch (_) {}
  }

  Future<void> _loadTables() async {
    final client = ref.read(apiClientProvider);
    setState(() => _isLoadingTables = true);
    try {
      final tables = await client.listTables();
      List<LayoutModel> layouts = [];
      try {
        layouts = await client.listLayouts();
      } catch (_) {}

      if (mounted) {
        setState(() {
          _tables = tables;
          _serverLayouts = layouts;
          _isLoadingTables = false;
          if (tables.isNotEmpty) {
            if (_selectedTable != null && tables.any((t) => t.id == _selectedTable!.id)) {
              _selectedTable = tables.firstWhere((t) => t.id == _selectedTable!.id);
            } else {
              _selectedTable = tables.first;
            }
            _resolveActiveLayoutForTable(_selectedTable!);
          } else {
            _selectedTable = null;
            _activeLayout = null;
          }
        });
        // Structural state updated from server — mark dirty for auto-save
        AutoSaveService.instance.markDirty();
      }
    } catch (_) {
      if (mounted) setState(() => _isLoadingTables = false);
    }
  }

  void _resolveActiveLayoutForTable(TableModel table) {
    final match = _serverLayouts.where((l) => l.tableOccurrenceId == table.id || l.name.toLowerCase() == table.displayName.toLowerCase()).firstOrNull;
    if (match != null && match.definition.isNotEmpty) {
      try {
        _activeLayout = LayoutDefinitionModel.fromJson(match.definition).copyWith(
          id: match.id,
          name: match.name,
        );
        return;
      } catch (_) {}
    }
    _activeLayout = LayoutDefinitionModel.defaultForTable(
      table.displayName,
      table.columns.map((c) => c.name).toList(),
    );
  }

  List<LayoutModel> get _activeTableLayouts {
    if (_selectedTable == null) return [];
    return _serverLayouts.where((l) => l.tableOccurrenceId == _selectedTable!.id || l.name.toLowerCase() == _selectedTable!.displayName.toLowerCase()).toList();
  }

  void _changeMode(OperationalMode newMode) {
    if (newMode == OperationalMode.layout && _currentUser != null && _currentUser!.role == 'user') {
      final activeId = _activeLayout?.id ?? '';
      final perm = _userPermissions[activeId] ?? 'read_write';
      if (perm == 'read_only' || perm == 'none') {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Access Restricted: User "${_currentUser!.username}" has $perm access to this layout. Layout editing is disabled.'),
            backgroundColor: Colors.orange.shade800,
          ),
        );
        return;
      }
    }
    ref.read(operationalModeProvider.notifier).setMode(newMode);
  }

  Future<void> _handleNewDatabase() async {
    final client = ref.read(apiClientProvider);
    final result = await NewDatabaseDialog.show(context, client);
    if (result != null && mounted) {
      try {
        final auth = await client.login(
          username: result.databaseName,
          password: result.databasePassword,
          database: result.databaseName,
        );
        if (mounted) {
          setState(() {
            _currentUser = auth.user;
            _activeDatabaseName = auth.database;
            _activeSolutionFileName = result.fileName;
            _activeSolutionName = result.package.solutionName;
            _activeSolutionDirectory = result.directoryRef;
            _serverStatus = 'Online (PostgreSQL - ${auth.user.username})';
          });
          await _loadUserPermissions();
        }
      } catch (_) {
        if (mounted) {
          setState(() {
            _currentUser = UserModel(id: 'owner', username: result.databaseName, role: 'owner');
            _activeSolutionFileName = result.fileName;
            _activeSolutionName = result.package.solutionName;
            _activeDatabaseName = result.databaseName;
            _activeSolutionDirectory = result.directoryRef;
          });
        }
      }
      await _loadTables();
      AutoSaveService.instance.configure(
        buildSolutionBytes: _exportCurrentSolutionBytes,
        directory: result.directoryRef,
        baseName: result.fileName,
      );
      AutoSaveService.instance.markDirty();
      if (mounted) {
        final locText = result.directoryRef != null ? ' to "${result.directoryRef!.displayName}"' : '';
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Created solution "${result.package.solutionName}" ($locText) with active database "${result.databaseName}". Initial owner: "${result.databaseName}".'),
            backgroundColor: Colors.green.shade700,
          ),
        );
      }
    }
  }

  Future<void> _handleOpenSolution({int initialTab = 0}) async {
    final client = ref.read(apiClientProvider);
    final result = await OpenSolutionDialog.show(context, client, initialTab: initialTab);
    if (result == null || !mounted) return;

    setState(() {
      _activeDatabaseName = result.databaseName;
      _currentUser = result.user;
      if (result.fileName != null) {
        _activeSolutionFileName = result.fileName!;
      }
      if (result.solutionName != null) {
        _activeSolutionName = result.solutionName!;
      }
      _serverStatus = 'Online (PostgreSQL - ${result.user.username})';
    });

    await _loadUserPermissions();
    await _loadTables();
    AutoSaveService.instance.markDirty();

    if (mounted) {
      final originDesc = result.type == OpenSolutionType.serverDatabase ? 'server database' : 'local file "${result.fileName}"';
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('Opened $originDesc: "${result.databaseName}" as user "${result.user.username}".'),
          backgroundColor: Colors.green.shade700,
        ),
      );
    }
  }

  Future<Uint8List> _exportCurrentSolutionBytes() async {
    final client = ref.read(apiClientProvider);
    List<TableOccurrenceModel> occurrences = [];
    List<LayoutModel> layouts = [];
    try {
      occurrences = await client.listOccurrences();
    } catch (_) {}
    try {
      layouts = await client.listLayouts();
    } catch (_) {}

    final dbConfig = DatabaseConnectionConfig(
      database: _activeDatabaseName,
      engine: 'postgres',
      host: 'localhost',
      port: 5432,
      user: 'file4base',
      password: 'dev_password',
    );

    final pkg = SolutionPackage.fromLiveData(
      solutionName: _activeSolutionName,
      dbConfig: dbConfig,
      tables: _tables,
      occurrences: occurrences,
      layouts: layouts,
    );

    return pkg.toMsgPack();
  }

  /// Explicit Save (Cmd+S): flushes auto-save immediately and shows a
  /// warning dialog explaining that database row data is NOT included.
  Future<void> _handleSave() async {
    final mode = ref.read(operationalModeProvider);
    if (mode == OperationalMode.layout) {
      await _layoutDesignerKey.currentState?.saveLayout();
    }

    if (_activeSolutionFileName == 'Untitled.f4p') {
      await _handleSaveAs();
      return;
    }

    // Flush the debounce timer — write to disk right now
    final flushed = await AutoSaveService.instance.flushNow();

    if (!mounted) return;

    // Show informational dialog about what is/isn't saved
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Row(
          children: [
            Icon(Icons.save_outlined, color: Color(0xFF1E88E5)),
            SizedBox(width: 10),
            Text('Solution Saved'),
          ],
        ),
        content: SizedBox(
          width: 420,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              // Success row
              Container(
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: flushed ? Colors.green.shade900.withValues(alpha: 0.25) : Colors.orange.shade900.withValues(alpha: 0.25),
                  borderRadius: BorderRadius.circular(6),
                  border: Border.all(color: flushed ? Colors.green.shade600 : Colors.orange.shade600, width: 0.8),
                ),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(flushed ? Icons.check_circle_outline : Icons.warning_amber_outlined,
                        color: flushed ? Colors.green : Colors.orange, size: 20),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            flushed ? '"$_activeSolutionFileName" saved' : 'Could not write to disk',
                            style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
                          ),
                          const SizedBox(height: 2),
                          Text(
                            flushed
                                ? 'Layouts, schemas, tables, and occurrences are saved.'
                                : 'Check that the save folder is still accessible.',
                            style: const TextStyle(fontSize: 12, color: Colors.grey),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 12),
              // Warning row
              Container(
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: Colors.amber.shade900.withValues(alpha: 0.2),
                  borderRadius: BorderRadius.circular(6),
                  border: Border.all(color: Colors.amber.shade600, width: 0.8),
                ),
                child: const Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(Icons.table_rows_outlined, color: Colors.amber, size: 20),
                    SizedBox(width: 10),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            'Database rows NOT included',
                            style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13, color: Colors.amber),
                          ),
                          SizedBox(height: 2),
                          Text(
                            'Record data (rows) is never auto-saved. To save a data snapshot, use File → Export Data...',
                            style: TextStyle(fontSize: 12, color: Colors.grey),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(),
            child: const Text('OK'),
          ),
          ElevatedButton.icon(
            onPressed: () {
              Navigator.of(ctx).pop();
              _handleExportData();
            },
            style: ElevatedButton.styleFrom(
              backgroundColor: const Color(0xFF1E88E5),
              foregroundColor: Colors.white,
            ),
            icon: const Icon(Icons.file_download_outlined, size: 16),
            label: const Text('Export Data Now'),
          ),
        ],
      ),
    );
  }

  Future<void> _handleSaveAs() async {
    final initialName = _activeSolutionFileName.replaceAll(RegExp(r'\.(f4p|f4b|f4data)$'), '');
    final ctrl = TextEditingController(text: initialName == 'Untitled' ? 'MySolution' : initialName);
    StorageDirectoryRef? pickedDir = _activeSolutionDirectory;

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (context, setDlgState) {
          return AlertDialog(
            title: const Row(
              children: [
                Icon(Icons.save_as_outlined, color: Color(0xFF1E88E5)),
                SizedBox(width: 8),
                Text('Save Solution As...'),
              ],
            ),
            content: SizedBox(
              width: 480,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('DESTINATION FOLDER ON HARD DRIVE', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey, letterSpacing: 0.5)),
                  const SizedBox(height: 6),
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                    decoration: BoxDecoration(
                      border: Border.all(color: pickedDir != null ? const Color(0xFF1E88E5) : Colors.grey.shade700),
                      borderRadius: BorderRadius.circular(6),
                      color: pickedDir != null ? const Color(0xFF1E88E5).withValues(alpha: 0.08) : Colors.black12,
                    ),
                    child: Row(
                      children: [
                        Icon(pickedDir != null ? Icons.folder : Icons.folder_open, color: pickedDir != null ? const Color(0xFF1E88E5) : Colors.grey, size: 20),
                        const SizedBox(width: 8),
                        Expanded(
                          child: Text(
                            pickedDir?.displayName ?? 'No folder selected (will prompt on save)',
                            style: TextStyle(
                              fontSize: 13,
                              fontWeight: pickedDir != null ? FontWeight.bold : FontWeight.normal,
                              color: pickedDir != null ? Colors.white : Colors.grey,
                            ),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                        OutlinedButton(
                          onPressed: () async {
                            final dir = await SolutionStorageService.pickDirectory();
                            if (dir != null) {
                              setDlgState(() => pickedDir = dir);
                            }
                          },
                          child: Text(pickedDir == null ? 'Choose Folder...' : 'Change...'),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 16),
                  const Text('SOLUTION FILE BASE NAME', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey, letterSpacing: 0.5)),
                  const SizedBox(height: 6),
                  TextField(
                    controller: ctrl,
                    autofocus: true,
                    decoration: const InputDecoration(
                      labelText: 'Base File Name',
                      helperText: 'Saves <name>.f4p (layouts/config). Use File → Export Data... to save database rows.',
                      border: OutlineInputBorder(),
                      isDense: true,
                      prefixIcon: Icon(Icons.file_present_outlined, size: 20),
                    ),
                  ),
                ],
              ),
            ),
            actions: [
              TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancel')),
              ElevatedButton(
                onPressed: () {
                  if (ctrl.text.trim().isNotEmpty) {
                    Navigator.of(ctx).pop(true);
                  }
                },
                child: const Text('Save Solution File'),
              ),
            ],
          );
        },
      ),
    );

    if (confirmed == true && ctrl.text.trim().isNotEmpty) {
      final baseName = ctrl.text.trim().replaceAll(RegExp(r'\.(f4p|f4b|f4data)$'), '');
      setState(() {
        _activeSolutionFileName = '$baseName.f4p';
        _activeSolutionName = baseName;
        _activeSolutionDirectory = pickedDir;
      });
      if (pickedDir != null) {
        AutoSaveService.instance.updateLocation(
          directory: pickedDir!,
          baseName: baseName,
        );
      }
      await _handleSave();
    }
  }

  void _handleSaveCopyAs() {
    final client = ref.read(apiClientProvider);
    SaveCopyDialog.show(
      context,
      apiClient: client,
      activeSolutionName: _activeSolutionName,
      activeDatabaseName: _activeDatabaseName,
      onExportSolution: _exportCurrentSolutionBytes,
    );
  }

  /// Export database row data (.f4data) explicitly — never auto-saved.
  Future<void> _handleExportData() async {
    final client = ref.read(apiClientProvider);
    try {
      final bytes = await client.exportDatabaseData();
      final baseName = _activeSolutionName.replaceAll(RegExp(r'\.(f4p|f4b|f4data)$'), '');
      final filename = '${baseName}_data.f4data';

      // Try to save to the existing solution directory, or let user pick
      if (_activeSolutionDirectory != null) {
        await SolutionStorageService.saveFile(filename: filename, bytes: bytes);
      } else {
        await SolutionStorageService.saveFile(filename: filename, bytes: bytes);
      }

      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Database data exported as "$filename" in MessagePack format.'),
            backgroundColor: Colors.green.shade700,
            duration: const Duration(seconds: 4),
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Export failed: $e'),
            backgroundColor: Colors.red,
          ),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final mode = ref.watch(operationalModeProvider);

    return CallbackShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.keyB, meta: true): () => _changeMode(OperationalMode.browse),
        const SingleActivator(LogicalKeyboardKey.keyF, meta: true): () => _changeMode(OperationalMode.find),
        const SingleActivator(LogicalKeyboardKey.keyL, meta: true): () => _changeMode(OperationalMode.layout),
        const SingleActivator(LogicalKeyboardKey.keyU, meta: true): () => _changeMode(OperationalMode.preview),
        const SingleActivator(LogicalKeyboardKey.keyS, meta: true): () => _handleSave(),
        const SingleActivator(LogicalKeyboardKey.keyS, meta: true, shift: true): () => _handleSaveAs(),
        const SingleActivator(LogicalKeyboardKey.keyO, meta: true): () => _handleOpenSolution(),
        const SingleActivator(LogicalKeyboardKey.keyN, meta: true): () => _handleNewDatabase(),
      },
      child: Focus(
        autofocus: true,
        child: Scaffold(
          body: SafeArea(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                File4BaseMenuBar(
                  activeMode: mode,
                  onModeChanged: _changeMode,
                  onManageDatabase: () async {
                    await ManageDatabaseDialog.show(context);
                    _loadTables();
                  },
                  onManageSecurity: () async {
                    if (_currentUser == null) {
                      _startAuthSequence();
                      return;
                    }
                    if (_currentUser!.role == 'user') {
                      ScaffoldMessenger.of(context).showSnackBar(
                        const SnackBar(
                          content: Text('Access Denied: Only Owner or Admin accounts can manage security and user accounts.'),
                          backgroundColor: Colors.red,
                        ),
                      );
                      return;
                    }
                    final client = ref.read(apiClientProvider);
                    await ManageSecurityDialog.show(context, client, _currentUser!);
                    await _loadUserPermissions();
                    await _loadTables();
                  },
                  onSwitchDatabaseOrLogin: _startAuthSequence,
                  onOpenRemote: () {
                    final currentUrl = ref.read(serverUrlProvider);
                    ServerConnectionDialog.show(
                      context,
                      currentUrl: currentUrl,
                      onConnect: (newUrl) {
                        ref.read(serverUrlProvider.notifier).setUrl(newUrl);
                        _checkServer();
                      },
                    );
                  },
                  onAbout: () => AboutFile4BaseDialog.show(context, serverStatus: _serverStatus),
                  onNewDatabase: _handleNewDatabase,
                  onOpenSolution: _handleOpenSolution,
                  onSave: _handleSave,
                  onSaveAs: _handleSaveAs,
                  onSaveCopyAs: _handleSaveCopyAs,
                  onExportData: _handleExportData,
                  onSaveLayout: () => _layoutDesignerKey.currentState?.saveLayout(),
                  onNewRecord: () => _dataBrowserKey.currentState?.createNewRecord(),
                  onDuplicateRecord: () => _dataBrowserKey.currentState?.createNewRecord(),
                  onDeleteRecord: () => _dataBrowserKey.currentState?.deleteCurrentRecord(),
                  onShowAllRecords: () => _dataBrowserKey.currentState?.fetchRecords(),
                  onPerformFind: () => _dataBrowserKey.currentState?.performFind(),
                  isToolbarVisible: _isToolbarVisible,
                  onToggleToolbar: (visible) => setState(() => _isToolbarVisible = visible),
                ),
                // Main Workspace: Classic File4Base Left Status Sidebar + Content Area
                Expanded(
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      if (_isToolbarVisible)
                        File4BaseStatusSidebar(
                          tables: _tables,
                          selectedTable: _selectedTable,
                          onTableSelected: (newTable) {
                            if (newTable != null) {
                              setState(() {
                                _selectedTable = newTable;
                                _resolveActiveLayoutForTable(newTable);
                              });
                            }
                          },
                          mode: mode,
                          currentRecordIndex: _currentRecordIndex,
                          totalRecords: _totalRecords,
                          isUnsorted: true,
                          onPreviousRecord: () => _dataBrowserKey.currentState?.previousRecord(),
                          onNextRecord: () => _dataBrowserKey.currentState?.nextRecord(),
                          onGoToRecord: (index) => _dataBrowserKey.currentState?.goToRecord(index),
                          onManageDatabase: () async {
                            await ManageDatabaseDialog.show(context);
                            _loadTables();
                          },
                          isFindOmit: _isFindOmit,
                          onToggleOmit: (val) {
                            setState(() => _isFindOmit = val);
                            _dataBrowserKey.currentState?.toggleOmit(val);
                          },
                          onPerformFind: () => _dataBrowserKey.currentState?.performFind(),
                          onShowAllRecords: () => _dataBrowserKey.currentState?.fetchRecords(),
                          onNewRecord: () => _dataBrowserKey.currentState?.createNewRecord(),
                          onDeleteRecord: () => _dataBrowserKey.currentState?.deleteCurrentRecord(),
                          layoutCount: _activeTableLayouts.isEmpty ? 1 : _activeTableLayouts.length,
                          currentLayoutIndex: () {
                            final list = _activeTableLayouts;
                            if (list.isEmpty || _activeLayout == null) return 0;
                            final idx = list.indexWhere((l) => l.id == _activeLayout!.id);
                            return idx >= 0 ? idx : 0;
                          }(),
                          onLayoutChanged: (idx) {
                            final list = _activeTableLayouts;
                            if (idx >= 0 && idx < list.length) {
                              setState(() {
                                try {
                                  _activeLayout = LayoutDefinitionModel.fromJson(list[idx].definition).copyWith(
                                    id: list[idx].id,
                                    name: list[idx].name,
                                  );
                                } catch (_) {}
                              });
                            }
                          },
                        ),
                      Expanded(
                        child: _buildBody(context, mode),
                      ),
                    ],
                  ),
                ),
                // Bottom Status Bar (Mode indicator, Zoom 100%, and Server status)
                _buildBottomStatusBar(context, mode),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildBottomStatusBar(BuildContext context, OperationalMode mode) {
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final borderColor = isDark ? const Color(0xFF38404B) : const Color(0xFFD1CFCA);
    final modeLabel = switch (mode) {
      OperationalMode.browse => 'Browse',
      OperationalMode.find => 'Find',
      OperationalMode.layout => 'Layout',
      OperationalMode.preview => 'Preview',
    };

    return Container(
      height: 24,
      padding: const EdgeInsets.symmetric(horizontal: 8),
      decoration: BoxDecoration(
        color: isDark ? const Color(0xFF161B22) : const Color(0xFFEBE9E4),
        border: Border(top: BorderSide(color: borderColor, width: 1.0)),
      ),
      child: Row(
        children: [
          // Zoom indicator
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 1),
            decoration: BoxDecoration(
              border: Border.all(color: Colors.grey.withValues(alpha: 0.3)),
              borderRadius: BorderRadius.circular(2),
            ),
            child: const Text('100', style: TextStyle(fontSize: 10, fontFamily: 'monospace')),
          ),
          const SizedBox(width: 4),
          const Icon(Icons.zoom_out, size: 14, color: Colors.grey),
          const SizedBox(width: 2),
          const Icon(Icons.zoom_in, size: 14, color: Colors.grey),
          const SizedBox(width: 8),
          const VerticalDivider(width: 1, indent: 4, endIndent: 4),
          const SizedBox(width: 8),

          // Active Mode indicator
          Text(
            modeLabel,
            style: TextStyle(
              fontSize: 11,
              fontWeight: FontWeight.bold,
              color: isDark ? const Color(0xFF90CAF9) : const Color(0xFF1E88E5),
            ),
          ),
          const SizedBox(width: 12),
          const Text(
            'For Help, choose Help > File4Base Help',
            style: TextStyle(fontSize: 10, color: Colors.grey),
          ),
          const SizedBox(width: 8),
          const VerticalDivider(width: 1, indent: 4, endIndent: 4),
          const SizedBox(width: 8),

          // Active MessagePack File & PostgreSQL Database info
          Tooltip(
            message: 'Active Solution (.f4p MessagePack) & PostgreSQL Database',
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Icon(Icons.file_present_outlined, size: 13, color: Color(0xFF1E88E5)),
                const SizedBox(width: 4),
                Text(
                  _activeSolutionFileName,
                  style: const TextStyle(fontSize: 10, fontWeight: FontWeight.bold),
                ),
                const SizedBox(width: 6),
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 1),
                  decoration: BoxDecoration(
                    color: const Color(0xFF1E88E5).withValues(alpha: 0.12),
                    borderRadius: BorderRadius.circular(3),
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Icon(Icons.storage, size: 10, color: Color(0xFF1E88E5)),
                      const SizedBox(width: 3),
                      Text(
                        _activeDatabaseName,
                        style: const TextStyle(fontSize: 9, fontWeight: FontWeight.bold, color: Color(0xFF1E88E5)),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),

          const SizedBox(width: 8),
          const VerticalDivider(width: 1, indent: 4, endIndent: 4),
          const SizedBox(width: 8),

          // Auto-save status indicator
          StreamBuilder<AutoSaveStatus>(
            stream: AutoSaveService.instance.statusStream,
            initialData: AutoSaveService.instance.currentStatus,
            builder: (context, snapshot) {
              final status = snapshot.data ?? AutoSaveStatus.idle;
              final (icon, label, color) = switch (status) {
                AutoSaveStatus.saving => (Icons.sync, 'Saving...', Colors.blue),
                AutoSaveStatus.saved  => (Icons.cloud_done_outlined, 'Saved', Colors.green),
                AutoSaveStatus.dirty  => (Icons.edit_note_outlined, 'Unsaved', Colors.orange),
                AutoSaveStatus.error  => (Icons.cloud_off_outlined, 'Save Error', Colors.red),
                AutoSaveStatus.idle   => (Icons.cloud_done_outlined, 'Auto-save', Colors.grey),
              };
              return Tooltip(
                message: status == AutoSaveStatus.error
                    ? 'Auto-save error: ${AutoSaveService.instance.lastError ?? "unknown"}'
                    : status == AutoSaveStatus.saved
                        ? 'Last saved: ${AutoSaveService.instance.lastSavedAt?.toLocal().toString().substring(11, 19) ?? ""}'
                        : 'Structure auto-saves every 3 seconds. Use File → Export Data... for row data.',
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    if (status == AutoSaveStatus.saving)
                      const SizedBox(
                        width: 10, height: 10,
                        child: CircularProgressIndicator(strokeWidth: 1.5, color: Colors.blue),
                      )
                    else
                      Icon(icon, size: 11, color: color),
                    const SizedBox(width: 3),
                    Text(label, style: TextStyle(fontSize: 9, fontWeight: FontWeight.bold, color: color)),
                  ],
                ),
              );
            },
          ),

          const SizedBox(width: 8),
          const VerticalDivider(width: 1, indent: 4, endIndent: 4),
          const SizedBox(width: 8),

          // User info chip & switch user trigger
          InkWell(
            onTap: _startAuthSequence,
            borderRadius: BorderRadius.circular(4),
            child: Tooltip(
              message: _currentUser != null
                  ? 'Authenticated as ${_currentUser!.username} (${_currentUser!.role}). Click to switch user or database.'
                  : 'Not authenticated. Click to login.',
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 1),
                decoration: BoxDecoration(
                  color: _currentUser != null
                      ? (_currentUser!.role == 'owner'
                          ? Colors.purple.withValues(alpha: 0.15)
                          : (_currentUser!.role == 'admin'
                              ? Colors.blue.withValues(alpha: 0.15)
                              : Colors.teal.withValues(alpha: 0.15)))
                      : Colors.orange.withValues(alpha: 0.15),
                  borderRadius: BorderRadius.circular(3),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      _currentUser?.role == 'owner'
                          ? Icons.workspace_premium
                          : (_currentUser?.role == 'admin'
                              ? Icons.admin_panel_settings
                              : (_currentUser != null ? Icons.person : Icons.login)),
                      size: 11,
                      color: _currentUser != null
                          ? (_currentUser!.role == 'owner'
                              ? Colors.purple
                              : (_currentUser!.role == 'admin' ? Colors.blue : Colors.teal))
                          : Colors.orange,
                    ),
                    const SizedBox(width: 3),
                    Text(
                      _currentUser != null
                          ? '${_currentUser!.username} (${_currentUser!.role})'
                          : 'Login',
                      style: TextStyle(
                        fontSize: 9,
                        fontWeight: FontWeight.bold,
                        color: _currentUser != null
                            ? (_currentUser!.role == 'owner'
                                ? Colors.purple
                                : (_currentUser!.role == 'admin' ? Colors.blue : Colors.teal))
                            : Colors.orange,
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),

          const Spacer(),

          // Server Connection Status
          InkWell(
            onTap: () {
              final currentUrl = ref.read(serverUrlProvider);
              ServerConnectionDialog.show(
                context,
                currentUrl: currentUrl,
                onConnect: (newUrl) {
                  ref.read(serverUrlProvider.notifier).setUrl(newUrl);
                  _checkServer();
                },
              );
            },
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(
                  _serverStatus.startsWith('Online') ? Icons.cloud_done : Icons.cloud_off,
                  size: 13,
                  color: _serverStatus.startsWith('Online') ? Colors.green : Colors.orange,
                ),
                const SizedBox(width: 4),
                Text(
                  _serverStatus,
                  style: const TextStyle(fontSize: 10, fontWeight: FontWeight.w600),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildBody(BuildContext context, OperationalMode mode) {
    if (_isLoadingTables) {
      return const Center(child: CircularProgressIndicator());
    }

    if (_tables.isEmpty || _selectedTable == null) {
      return _buildWelcomeScreen(context, mode);
    }

    final client = ref.read(apiClientProvider);

    switch (mode) {
      case OperationalMode.browse:
      case OperationalMode.find:
        return DataBrowserWidget(
          key: _dataBrowserKey,
          table: _selectedTable!,
          apiClient: client,
          mode: mode,
          onTableModified: _loadTables,
          onRecordChanged: (idx, total) {
            if (mounted) {
              setState(() {
                _currentRecordIndex = idx;
                _totalRecords = total;
              });
            }
          },
        );
      case OperationalMode.layout:
        return LayoutDesignerWidget(
          key: _layoutDesignerKey,
          table: _selectedTable!,
          apiClient: client,
          initialLayout: _activeLayout ??
              LayoutDefinitionModel.defaultForTable(
                _selectedTable!.displayName,
                _selectedTable!.columns.map((c) => c.name).toList(),
              ),
          onSaved: _loadTables,
          onAutoSaveDirty: AutoSaveService.instance.markDirty,
          activeTool: _activeLayoutTool,
        );
      case OperationalMode.preview:
        return LayoutPreviewWidget(
          key: ValueKey('preview_${_selectedTable!.id}'),
          table: _selectedTable!,
          apiClient: client,
          layout: _activeLayout ??
              LayoutDefinitionModel.defaultForTable(
                _selectedTable!.displayName,
                _selectedTable!.columns.map((c) => c.name).toList(),
              ),
        );
    }
  }

  Widget _buildWelcomeScreen(BuildContext context, OperationalMode mode) {
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final brandLogo = isDark
        ? 'assets/branding/file4base-dark.png'
        : 'assets/branding/file4base-light.png';

    return Container(
      color: Theme.of(context).colorScheme.surfaceContainerHighest.withOpacity(0.3),
      child: Center(
        child: Card(
          elevation: 3,
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
          child: Container(
            width: 520,
            padding: const EdgeInsets.all(32.0),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                ClipRRect(
                  borderRadius: BorderRadius.circular(16),
                  child: Image.asset(
                    brandLogo,
                    width: 72,
                    height: 72,
                    fit: BoxFit.contain,
                    errorBuilder: (_, __, ___) => Image.asset(
                      'assets/branding/file4base-icon-128.png',
                      width: 72,
                      height: 72,
                      errorBuilder: (_, __, ___) => const Icon(Icons.table_chart, size: 64, color: Colors.blue),
                    ),
                  ),
                ),
                const SizedBox(height: 16),
                const Text(
                  'Welcome to File4Base',
                  style: TextStyle(fontSize: 22, fontWeight: FontWeight.bold),
                ),
                const SizedBox(height: 8),
                Text(
                  'Open-source relational database engine with dynamic schema, relational occurrences, visual layouts, and 4 operational modes.',
                  textAlign: TextAlign.center,
                  style: TextStyle(color: Theme.of(context).colorScheme.onSurfaceVariant, fontSize: 13, height: 1.4),
                ),
                const SizedBox(height: 24),
                FilledButton.icon(
                  icon: const Icon(Icons.add_circle_outline, size: 18),
                  label: const Text('Create First Database Table'),
                  style: FilledButton.styleFrom(
                    padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
                  ),
                  onPressed: () async {
                    await ManageDatabaseDialog.show(context);
                    _loadTables();
                  },
                ),
                const SizedBox(height: 24),
                const Divider(),
                const SizedBox(height: 8),
                Row(
                  mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                  children: [
                    _buildShortcutBadge('⌘B', 'Browse'),
                    _buildShortcutBadge('⌘F', 'Find'),
                    _buildShortcutBadge('⌘L', 'Layout'),
                    _buildShortcutBadge('⌘U', 'Preview'),
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildShortcutBadge(String shortcut, String label) {
    return Column(
      children: [
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
          decoration: BoxDecoration(
            color: Colors.grey.withOpacity(0.15),
            borderRadius: BorderRadius.circular(6),
            border: Border.all(color: Colors.grey.withOpacity(0.3)),
          ),
          child: Text(shortcut, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
        ),
        const SizedBox(height: 4),
        Text(label, style: const TextStyle(fontSize: 11, color: Colors.grey)),
      ],
    );
  }
}
