import 'dart:math' as math;
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'core/api/api_client.dart';
import 'core/providers/zoom_provider.dart';
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
import 'features/layout_engine/manage_layouts_dialog.dart';
import 'features/layout_engine/models/layout_definition.dart';
import 'core/models/file_options_model.dart';
import 'core/models/page_setup_model.dart';
import 'core/theme/theme_model.dart';
import 'core/theme/theme_provider.dart';
import 'features/preflight/preflight_dialog.dart';
import 'features/schema_manager/manage_database_dialog.dart';
import 'features/script_workspace/script_workspace_dialog.dart';
import 'features/security/change_password_dialog.dart';
import 'features/security/manage_security_dialog.dart';
import 'features/data_browser/export_records_dialog.dart';
import 'features/solution_manager/file_options_dialog.dart';
import 'features/solution_manager/new_database_dialog.dart';
import 'features/solution_manager/open_solution_dialog.dart';
import 'features/solution_manager/page_setup_dialog.dart';
import 'features/solution_manager/save_copy_dialog.dart';
import 'features/theme_manager/manage_themes_dialog.dart';

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

/// Counts the times the server rejected the current session (HTTP 401 on a
/// request other than sign-in). The workspace listens to it to lock itself.
class SessionExpiredNotifier extends Notifier<int> {
  @override
  int build() => 0;

  void notify() => state = state + 1;
}

final sessionExpiredProvider =
    NotifierProvider<SessionExpiredNotifier, int>(SessionExpiredNotifier.new);

final apiClientProvider = Provider<ApiClient>((ref) {
  final baseUrl = ref.watch(serverUrlProvider);
  final client = ApiClient(baseUrl: baseUrl);
  client.onUnauthorized = () => ref.read(sessionExpiredProvider.notifier).notify();
  ref.onDispose(() {
    client.onUnauthorized = null;
    client.close();
  });
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

class File4BaseApp extends ConsumerWidget {
  const File4BaseApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final activeTheme = ref.watch(appThemeProvider);
    return MaterialApp(
      title: 'File4Base',
      debugShowCheckedModeBanner: false,
      locale: const Locale('en'),
      supportedLocales: const [Locale('en')],
      theme: activeTheme.toThemeData(),
      darkTheme: activeTheme.isDark ? activeTheme.toThemeData() : AppThemes.dark.toThemeData(),
      themeMode: activeTheme.isDark ? ThemeMode.dark : ThemeMode.light,
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
  String _activeDatabaseName = '';
  StorageDirectoryRef? _activeSolutionDirectory;
  List<TableModel> _tables = [];
  TableModel? _selectedTable;
  LayoutDefinitionModel? _activeLayout;
  bool _isLoadingTables = false;
  bool _isToolbarVisible = true;
  final GlobalKey<DataBrowserWidgetState> _dataBrowserKey = GlobalKey<DataBrowserWidgetState>();
  final GlobalKey<LayoutDesignerWidgetState> _layoutDesignerKey = GlobalKey<LayoutDesignerWidgetState>();
  LayoutPreviewWidgetState? _previewState;
  int _currentRecordIndex = 0;
  int _totalRecords = 0;
  bool _isFindOmit = false;
  LayoutTool _activeLayoutTool = LayoutTool.pointer;
  double _layoutStrokeWidth = 1.0;
  UserModel? _currentUser;
  List<LayoutModel> _serverLayouts = [];
  Map<String, String> _userPermissions = {};
  FileOptionsModel _fileOptions = const FileOptionsModel();
  PageSetupModel _pageSetup = const PageSetupModel();

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
      final serverUrl = ref.read(serverUrlProvider);
      PreflightDialog.showIfNeeded(context, serverUrl: serverUrl, onProceed: () {
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
          _currentUser = UserModel(id: 'test-owner', username: 'admin', role: 'owner');
          _activeDatabaseName = 'test_db';
          _serverStatus = 'Online (Test)';
        });
      }
      return;
    }

    final client = ref.read(apiClientProvider);
    Map<String, dynamic>? health;
    try {
      health = await client.checkHealth();
      if (mounted) {
        setState(() {
          _serverStatus = 'Online (${health?['engine']})';
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
        if (auth.fileName != null) {
          _activeSolutionFileName = auth.fileName!;
        }
        if (auth.solutionName != null) {
          _activeSolutionName = auth.solutionName!;
        }
        if (auth.directoryRef != null && auth.directoryRef is StorageDirectoryRef) {
          _activeSolutionDirectory = auth.directoryRef as StorageDirectoryRef;
        }
        final engineName = health?['engine']?.toString().toUpperCase() == 'MARIADB' ? 'MariaDB' : 'PostgreSQL';
        _serverStatus = 'Online ($engineName - ${auth.user.username})';
      });
      await _loadUserPermissions();
      await _loadTables();
      if (auth.fileName != null) {
        AutoSaveService.instance.configure(
          buildSolutionBytes: _exportCurrentSolutionBytes,
          directory: _activeSolutionDirectory,
          baseName: _activeSolutionFileName,
        );
      }
      AutoSaveService.instance.markDirty();
    } else {
      // User cancelled login or was not authenticated:
      // Keep application strictly locked, clear any tables/records, and do NOT load database data!
      if (mounted) {
        setState(() {
          _currentUser = null;
          _tables = [];
          _selectedTable = null;
          _activeLayout = null;
          _serverLayouts = [];
          _serverStatus = 'Online (Locked)';
        });
      }
    }
  }

  /// Locks the workspace after the server rejected the session (expired,
  /// revoked, or the server was restarted). Does nothing when already signed out.
  void _handleSessionExpired() {
    if (!mounted || _currentUser == null) return;
    setState(() {
      _currentUser = null;
      _tables = [];
      _selectedTable = null;
      _activeLayout = null;
      _serverLayouts = [];
      _userPermissions = {};
      _serverStatus = 'Online (Locked)';
    });
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(
        content: Text('Your session has expired. Please sign in again.'),
        duration: Duration(seconds: 4),
      ),
    );
  }

  void _handleSignOut() {
    // Close the server session; the token is forgotten immediately
    ref.read(apiClientProvider).logout();
    setState(() {
      _currentUser = null;
      _tables = [];
      _selectedTable = null;
      _activeLayout = null;
      _serverLayouts = [];
      _serverStatus = 'Online (Locked)';
    });
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(
        content: Text('Signed out. Database access is protected.'),
        duration: Duration(seconds: 2),
      ),
    );
  }

  Future<void> _checkServer() async {
    final client = ref.read(apiClientProvider);
    try {
      final health = await client.checkHealth();
      if (mounted) {
        setState(() {
          _serverStatus = _currentUser != null
              ? 'Online (${health['engine']} - ${_currentUser!.username})'
              : 'Online (${health['engine']} - Locked)';
          if (health['active_database'] != null && _activeDatabaseName.isEmpty) {
            _activeDatabaseName = health['active_database'].toString();
          }
        });
        if (_currentUser != null) {
          _loadTables();
        }
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
      final perms = await client.getUserPermissions(_currentUser!.id, database: _activeDatabaseName);
      if (mounted) {
        setState(() {
          _userPermissions = {for (final p in perms) p.layoutId: p.accessLevel};
        });
      }
    } catch (_) {}
  }

  Future<void> _loadTables({String? targetLayoutId}) async {
    if (_currentUser == null) {
      if (mounted) {
        setState(() {
          _tables = [];
          _selectedTable = null;
          _activeLayout = null;
          _serverLayouts = [];
          _isLoadingTables = false;
        });
      }
      return;
    }

    final client = ref.read(apiClientProvider);
    setState(() => _isLoadingTables = true);
    try {
      final tables = await client.listTables();
      List<LayoutModel> layouts = [];
      try {
        layouts = await client.listLayouts();
      } catch (_) {}

      // Ensure every table has at least one layout on server
      if (tables.isNotEmpty) {
        for (final table in tables) {
          final hasLayout = layouts.any((l) {
            final toName = l.definition['table_occurrence']?.toString().toLowerCase();
            return toName == table.name.toLowerCase() ||
                toName == table.displayName.toLowerCase() ||
                l.tableOccurrenceId == table.id ||
                l.name.toLowerCase() == '${table.displayName} form'.toLowerCase() ||
                l.name.toLowerCase() == table.displayName.toLowerCase();
          });

          if (!hasLayout) {
            final defaultDef = LayoutDefinitionModel.defaultForTable(
              table.displayName,
              table.columns.map((c) => c.name).toList(),
            );
            try {
              final created = await client.createLayout(
                '${table.displayName} Form',
                toId: table.id,
                definition: defaultDef.toJson(),
              );
              layouts.add(created);
            } catch (_) {}
          }
        }
      }

      if (mounted) {
        setState(() {
          _tables = tables;
          _serverLayouts = layouts;
          _isLoadingTables = false;

          if (layouts.isNotEmpty) {
            LayoutModel? selectedLayoutModel;
            if (targetLayoutId != null) {
              selectedLayoutModel = layouts.where((l) => l.id == targetLayoutId).firstOrNull;
            }
            selectedLayoutModel ??= (_activeLayout != null
                ? layouts.where((l) => l.id == _activeLayout!.id).firstOrNull
                : null);
            selectedLayoutModel ??= layouts.first;

            _applyLayout(selectedLayoutModel, tables);
          } else if (tables.isNotEmpty) {
            _selectedTable = tables.first;
            _activeLayout = LayoutDefinitionModel.defaultForTable(
              _selectedTable!.displayName,
              _selectedTable!.columns.map((c) => c.name).toList(),
            );
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

  void _applyLayout(LayoutModel layoutModel, [List<TableModel>? tablesList]) {
    final tables = tablesList ?? _tables;
    final toName = layoutModel.definition['table_occurrence']?.toString().toLowerCase();
    TableModel? matchTable;
    if (toName != null) {
      matchTable = tables.where((t) =>
          t.displayName.toLowerCase() == toName ||
          t.name.toLowerCase() == toName).firstOrNull;
    }
    matchTable ??= tables.where((t) => t.id == layoutModel.tableOccurrenceId).firstOrNull;
    matchTable ??= tables.where((t) =>
        layoutModel.name.toLowerCase().contains(t.displayName.toLowerCase()) ||
        layoutModel.name.toLowerCase().contains(t.name.toLowerCase())).firstOrNull;
    matchTable ??= (_selectedTable != null && tables.any((t) => t.id == _selectedTable!.id))
        ? _selectedTable
        : (tables.isNotEmpty ? tables.first : null);

    _selectedTable = matchTable;

    try {
      _activeLayout = LayoutDefinitionModel.fromJson(layoutModel.definition).copyWith(
        id: layoutModel.id,
        name: layoutModel.name,
      );
    } catch (_) {
      if (matchTable != null) {
        _activeLayout = LayoutDefinitionModel.defaultForTable(
          matchTable.displayName,
          matchTable.columns.map((c) => c.name).toList(),
        ).copyWith(id: layoutModel.id, name: layoutModel.name);
      }
    }
  }

  void _selectLayout(LayoutModel layout) {
    setState(() {
      _applyLayout(layout);
    });
  }

  Future<void> _handleManageLayouts() async {
    final currentModel = _serverLayouts.where((l) => l.id == _activeLayout?.id).firstOrNull ??
        (_serverLayouts.isNotEmpty ? _serverLayouts.first : null);

    await ManageLayoutsDialog.show(
      context,
      layouts: _serverLayouts,
      tables: _tables,
      activeLayout: currentModel,
      onSelectLayout: (l) => _selectLayout(l),
      onLayoutsChanged: () => _loadTables(targetLayoutId: _activeLayout?.id),
    );
  }

  Future<void> _handleNewLayout() async {
    if (_tables.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Please create a table first before creating a layout.')),
      );
      return;
    }

    final nameCtrl = TextEditingController(text: '${_selectedTable?.displayName ?? "New"} Form');
    TableModel? selectedTable = _selectedTable ?? _tables.first;

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDlgState) => AlertDialog(
          title: const Row(
            children: [
              Icon(Icons.add_to_photos, color: Color(0xFF1E88E5)),
              SizedBox(width: 8),
              Text('New Layout / Presentation'),
            ],
          ),
          content: SizedBox(
            width: 400,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text('Layout Name:', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                const SizedBox(height: 6),
                TextField(
                  controller: nameCtrl,
                  autofocus: true,
                  decoration: const InputDecoration(border: OutlineInputBorder(), isDense: true),
                ),
                const SizedBox(height: 16),
                const Text('Show records from table:', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                const SizedBox(height: 6),
                DropdownButtonFormField<String>(
                  value: selectedTable?.id,
                  decoration: const InputDecoration(border: OutlineInputBorder(), isDense: true),
                  items: _tables.map((t) => DropdownMenuItem(value: t.id, child: Text(t.displayName))).toList(),
                  onChanged: (id) {
                    if (id != null) {
                      setDlgState(() => selectedTable = _tables.firstWhere((t) => t.id == id));
                    }
                  },
                ),
              ],
            ),
          ),
          actions: [
            TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancel')),
            FilledButton(onPressed: () => Navigator.of(ctx).pop(true), child: const Text('Create Layout')),
          ],
        ),
      ),
    );

    if (confirmed == true && selectedTable != null) {
      final name = nameCtrl.text.trim().isEmpty ? 'Untitled Layout' : nameCtrl.text.trim();
      final client = ref.read(apiClientProvider);
      final def = LayoutDefinitionModel.defaultForTable(
        selectedTable!.displayName,
        selectedTable!.columns.map((c) => c.name).toList(),
      ).copyWith(name: name);

      try {
        final created = await client.createLayout(
          name,
          toId: selectedTable!.id,
          definition: def.toJson(),
        );
        await _loadTables(targetLayoutId: created.id);
        _changeMode(OperationalMode.layout);
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('Created layout "$name" in Layout Mode'),
              backgroundColor: Colors.green.shade700,
            ),
          );
        }
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('Failed to create layout: $e'), backgroundColor: Colors.red),
          );
        }
      }
    }
  }

  Future<void> _handleRenameActiveLayout() async {
    if (_activeLayout == null) return;
    final nameCtrl = TextEditingController(text: _activeLayout!.name);

    final newName = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Row(
          children: [
            Icon(Icons.edit, color: Color(0xFF1E88E5)),
            SizedBox(width: 8),
            Text('Rename Current Layout'),
          ],
        ),
        content: SizedBox(
          width: 360,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('Enter new layout name:', style: TextStyle(fontSize: 13)),
              const SizedBox(height: 8),
              TextField(
                controller: nameCtrl,
                autofocus: true,
                decoration: const InputDecoration(border: OutlineInputBorder(), isDense: true),
                onSubmitted: (v) => Navigator.of(ctx).pop(v.trim()),
              ),
            ],
          ),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.of(ctx).pop(), child: const Text('Cancel')),
          FilledButton(onPressed: () => Navigator.of(ctx).pop(nameCtrl.text.trim()), child: const Text('Rename')),
        ],
      ),
    );

    if (newName != null && newName.isNotEmpty && newName != _activeLayout!.name) {
      final client = ref.read(apiClientProvider);
      try {
        final updatedDef = _activeLayout!.copyWith(name: newName).toJson();
        await client.updateLayout(_activeLayout!.id, newName, updatedDef);
        await _loadTables(targetLayoutId: _activeLayout!.id);
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('Layout renamed to "$newName"'),
              backgroundColor: Colors.green.shade700,
            ),
          );
        }
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('Failed to rename layout: $e'), backgroundColor: Colors.red),
          );
        }
      }
    }
  }

  Future<void> _changeMode(OperationalMode newMode) async {
    if (_currentUser == null) {
      _startAuthSequence();
      return;
    }

    final currentMode = ref.read(operationalModeProvider);
    if (currentMode == newMode) return;

    // If leaving Layout Mode, ensure any pending modifications are committed and saved
    if (currentMode == OperationalMode.layout) {
      final designer = _layoutDesignerKey.currentState;
      if (designer != null) {
        try {
          final savedLayout = await designer.commitAndSave();
          if (mounted) {
            setState(() {
              _activeLayout = savedLayout;
            });
          }
        } catch (_) {}
      }
    }

    if (newMode == OperationalMode.layout && _currentUser != null && _currentUser!.role == 'user') {
      final activeId = _activeLayout?.id ?? '';
      final perm = _userPermissions[activeId] ?? 'read_write';
      if (perm == 'read_only' || perm == 'none') {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('Access Restricted: User "${_currentUser!.username}" has $perm access to this layout. Layout editing is disabled.'),
              backgroundColor: Colors.orange.shade800,
            ),
          );
        }
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
          username: result.user,
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
            _currentUser = UserModel(id: 'owner', username: result.user, role: 'owner');
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
            content: Text('Created solution "${result.package.solutionName}" ($locText) with active database "${result.databaseName}". Initial owner: "${result.user}".'),
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

    String? startupTargetLayoutId;
    if (result.package != null) {
      _fileOptions = result.package!.fileOptions;
      _pageSetup = result.package!.pageSetup;
      if (_fileOptions.hideAllToolbars) {
        _isToolbarVisible = false;
      }
      if (_fileOptions.switchLayoutOnOpen && _fileOptions.startupLayoutId.isNotEmpty) {
        startupTargetLayoutId = _fileOptions.startupLayoutId;
      }
    }

    await _loadUserPermissions();
    await _loadTables(targetLayoutId: startupTargetLayoutId);
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
    List<Map<String, dynamic>> usersList = [];

    try {
      occurrences = await client.listOccurrences();
    } catch (_) {}
    try {
      layouts = await client.listLayouts();
    } catch (_) {}
    try {
      final users = await client.listUsers(database: _activeDatabaseName);
      for (final u in users) {
        List<UserLayoutPermissionModel> perms = [];
        try {
          perms = await client.getUserPermissions(u.id, database: _activeDatabaseName);
        } catch (_) {}
        usersList.add({
          'id': u.id,
          'username': u.username,
          'role': u.role,
          'is_active': u.isActive,
          'permissions': perms.map((p) => {
            'layout_id': p.layoutId,
            'layout_name': p.layoutName,
            'access_level': p.accessLevel,
          }).toList(),
        });
      }
    } catch (_) {}

    final dbConfig = DatabaseConnectionConfig(
      database: _activeDatabaseName,
      engine: 'postgres',
      host: 'localhost',
      port: 5432,
      user: _currentUser?.username ?? 'admin',
      password: '',
    );

    final pkg = SolutionPackage.fromLiveData(
      solutionName: _activeSolutionName,
      dbConfig: dbConfig,
      tables: _tables,
      occurrences: occurrences,
      layouts: layouts,
      users: usersList,
      fileOptions: _fileOptions,
      pageSetup: _pageSetup,
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

  Future<void> _handleFileOptions() async {
    final client = ref.read(apiClientProvider);
    final updated = await FileOptionsDialog.show(
      context,
      initialOptions: _fileOptions,
      layouts: _serverLayouts,
      currentUser: _currentUser,
      apiClient: client,
    );
    if (updated != null && mounted) {
      setState(() {
        _fileOptions = updated;
        if (updated.hideAllToolbars) {
          _isToolbarVisible = false;
        }
      });
      AutoSaveService.instance.markDirty();
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('File options updated successfully.'),
          backgroundColor: Colors.green,
        ),
      );
    }
  }

  Future<void> _handlePageSetup() async {
    final updated = await PageSetupDialog.show(
      context,
      initialPageSetup: _pageSetup,
    );
    if (updated != null && mounted) {
      setState(() {
        _pageSetup = updated;
      });
      AutoSaveService.instance.markDirty();
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('Page setup saved: ${updated.paperSizeName} (${updated.isLandscape ? "Landscape" : "Portrait"}) via ${updated.printer}.'),
          backgroundColor: Colors.green,
        ),
      );
    }
  }

  Future<void> _handleChangePassword() async {
    if (_currentUser == null) {
      _startAuthSequence();
      return;
    }
    final client = ref.read(apiClientProvider);
    final changed = await ChangePasswordDialog.show(
      context,
      apiClient: client,
      currentUser: _currentUser!,
      databaseName: _activeDatabaseName,
    );
    if (changed == true && mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('Password for "${_currentUser!.username}" updated successfully.'),
          backgroundColor: Colors.green.shade700,
        ),
      );
    }
  }

  Future<void> _handleExportRecords() async {
    if (_currentUser == null) {
      _startAuthSequence();
      return;
    }
    if (_tables.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('No tables available to export.'),
          backgroundColor: Colors.orange,
        ),
      );
      return;
    }
    final client = ref.read(apiClientProvider);
    await ExportRecordsDialog.show(
      context,
      apiClient: client,
      tables: _tables,
      initialTable: _selectedTable,
    );
  }

  /// File > Print: prints the page shown in Preview mode (switching to it
  /// first), as a PDF of the Page Setup paper size.
  Future<void> _handlePrint() async {
    if (ref.read(operationalModeProvider) != OperationalMode.preview) {
      _changeMode(OperationalMode.preview);
    }
    // Wait for the preview to load its records and lay out the sheet.
    for (var i = 0; i < 50 && (_previewState == null || !_previewState!.isReady); i++) {
      await Future<void>.delayed(const Duration(milliseconds: 100));
    }
    await _previewState?.printPages(allRecords: false);
  }

  Future<void> _handleQuit() async {
    // 1. If currently in Layout mode, commit pending layout changes
    final mode = ref.read(operationalModeProvider);
    if (mode == OperationalMode.layout) {
      try {
        await _layoutDesignerKey.currentState?.saveLayout();
      } catch (_) {}
    }

    // 2. If authenticated and dirty/active, flush auto-save to disk
    try {
      await AutoSaveService.instance.flushNow();
    } catch (_) {}

    // 3. Confirm exit if needed, or close immediately
    if (!mounted) return;
    final shouldQuit = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Row(
          children: [
            Icon(Icons.exit_to_app, color: Colors.redAccent),
            SizedBox(width: 8),
            Text('Quit File4Base'),
          ],
        ),
        content: const Text(
          'All changes have been saved. Are you sure you want to close the application?',
          style: TextStyle(fontSize: 14),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Colors.redAccent),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Quit'),
          ),
        ],
      ),
    );

    if (shouldQuit == true) {
      SolutionStorageService.triggerQuit();
    }
  }

  @override
  Widget build(BuildContext context) {
    final mode = ref.watch(operationalModeProvider);
    ref.listen<int>(sessionExpiredProvider, (previous, next) => _handleSessionExpired());

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
        const SingleActivator(LogicalKeyboardKey.keyP, meta: true, shift: true): () => _handlePageSetup(),
        const SingleActivator(LogicalKeyboardKey.keyQ, meta: true): () => _handleQuit(),
        const SingleActivator(LogicalKeyboardKey.keyQ, control: true): () => _handleQuit(),
        const SingleActivator(LogicalKeyboardKey.equal, meta: true): () => ref.read(zoomProvider.notifier).zoomIn(),
        const SingleActivator(LogicalKeyboardKey.equal, control: true): () => ref.read(zoomProvider.notifier).zoomIn(),
        const SingleActivator(LogicalKeyboardKey.add, meta: true): () => ref.read(zoomProvider.notifier).zoomIn(),
        const SingleActivator(LogicalKeyboardKey.add, control: true): () => ref.read(zoomProvider.notifier).zoomIn(),
        const SingleActivator(LogicalKeyboardKey.minus, meta: true): () => ref.read(zoomProvider.notifier).zoomOut(),
        const SingleActivator(LogicalKeyboardKey.minus, control: true): () => ref.read(zoomProvider.notifier).zoomOut(),
        const SingleActivator(LogicalKeyboardKey.digit0, meta: true): () => ref.read(zoomProvider.notifier).resetZoom(),
        const SingleActivator(LogicalKeyboardKey.digit0, control: true): () => ref.read(zoomProvider.notifier).resetZoom(),
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
                  isAuthenticated: _currentUser != null,
                  onSignIn: _startAuthSequence,
                  onSignOut: _handleSignOut,
                  onModeChanged: _changeMode,
                  onManageDatabase: () async {
                    if (_currentUser == null) {
                      _startAuthSequence();
                      return;
                    }
                    await ManageDatabaseDialog.show(context);
                    _loadTables();
                  },
                  onManageLayouts: () {
                    if (_currentUser == null) {
                      _startAuthSequence();
                      return;
                    }
                    _handleManageLayouts();
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
                    await ManageSecurityDialog.show(
                      context,
                      client,
                      _currentUser!,
                      databaseName: _activeDatabaseName,
                      onModified: () async {
                        AutoSaveService.instance.markDirty();
                      },
                    );
                    await _loadUserPermissions();
                    await _loadTables();
                  },
                  onManageScripts: () async {
                    if (_currentUser == null) {
                      _startAuthSequence();
                      return;
                    }
                    final client = ref.read(apiClientProvider);
                    await ScriptWorkspaceDialog.show(
                      context,
                      client,
                      databaseName: _activeDatabaseName,
                    );
                  },
                  onScriptWorkspace: () async {
                    if (_currentUser == null) {
                      _startAuthSequence();
                      return;
                    }
                    final client = ref.read(apiClientProvider);
                    await ScriptWorkspaceDialog.show(
                      context,
                      client,
                      databaseName: _activeDatabaseName,
                    );
                  },
                  onManageThemes: () => ManageThemesDialog.show(context),
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
                  onFileOptions: _handleFileOptions,
                  onChangePassword: _handleChangePassword,
                  onPageSetup: _handlePageSetup,
                  onPrint: _handlePrint,
                  onQuit: _handleQuit,
                  onExportRecords: _handleExportRecords,
                  onSaveLayout: () => _layoutDesignerKey.currentState?.saveLayout(),
                  onNewRecord: () => _dataBrowserKey.currentState?.createNewRecord(),
                  onDuplicateRecord: () => _dataBrowserKey.currentState?.duplicateRecord(),
                  onSortRecords: mode == OperationalMode.browse ? () => _dataBrowserKey.currentState?.sortRecords() : null,
                  onDeleteRecord: () => _dataBrowserKey.currentState?.deleteCurrentRecord(),
                  onShowAllRecords: () {
                    _changeMode(OperationalMode.browse);
                    _dataBrowserKey.currentState?.fetchRecords();
                  },
                  onPerformFind: () => _dataBrowserKey.currentState?.performFind(),
                  isToolbarVisible: _isToolbarVisible,
                  onToggleToolbar: (visible) => setState(() => _isToolbarVisible = visible),
                  onZoomIn: () => ref.read(zoomProvider.notifier).zoomIn(),
                  onZoomOut: () => ref.read(zoomProvider.notifier).zoomOut(),
                  onResetZoom: () => ref.read(zoomProvider.notifier).resetZoom(),
                  onSelectZoom: (level) => ref.read(zoomProvider.notifier).setZoom(level),
                  zoomLevel: ref.watch(zoomProvider),
                  currentUserName: _currentUser?.username,
                  onInsertMedia: mode == OperationalMode.layout
                      ? (kind) => _layoutDesignerKey.currentState?.insertMedia(kind)
                      : null,
                  onInsertSymbol: mode == OperationalMode.layout
                      ? (symbol) => _layoutDesignerKey.currentState?.insertText(symbol)
                      : null,
                  onInsertMergeField: mode == OperationalMode.layout
                      ? () => _layoutDesignerKey.currentState?.insertMergeField()
                      : null,
                ),
                // Main Workspace: Classic File4Base Left Status Sidebar + Content Area
                Expanded(
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      if (_isToolbarVisible && mode != OperationalMode.layout && _currentUser != null)
                        File4BaseStatusSidebar(
                          layouts: _serverLayouts,
                          selectedLayout: _serverLayouts.where((l) => l.id == _activeLayout?.id).firstOrNull ??
                              (_serverLayouts.isNotEmpty ? _serverLayouts.first : null),
                          onLayoutSelected: (layout) => _selectLayout(layout),
                          onNewLayout: _handleNewLayout,
                          onManageLayouts: _handleManageLayouts,
                          onRenameLayout: _handleRenameActiveLayout,
                          tables: _tables,
                          selectedTable: _selectedTable,
                          onTableSelected: (newTable) {
                            if (newTable != null) {
                              setState(() {
                                _selectedTable = newTable;
                                final matchLayout = _serverLayouts.where((l) {
                                  final toName = l.definition['table_occurrence']?.toString().toLowerCase();
                                  return toName == newTable.name.toLowerCase() ||
                                      toName == newTable.displayName.toLowerCase() ||
                                      l.tableOccurrenceId == newTable.id;
                                }).firstOrNull;
                                if (matchLayout != null) {
                                  _applyLayout(matchLayout);
                                } else {
                                  _activeLayout = LayoutDefinitionModel.defaultForTable(
                                    newTable.displayName,
                                    newTable.columns.map((c) => c.name).toList(),
                                  );
                                }
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
                          onShowAllRecords: () {
                            _changeMode(OperationalMode.browse);
                            _dataBrowserKey.currentState?.fetchRecords();
                          },
                          onNewRecord: () => _dataBrowserKey.currentState?.createNewRecord(),
                          onDeleteRecord: () => _dataBrowserKey.currentState?.deleteCurrentRecord(),
                          selectedTool: _activeLayoutTool,
                          onToolSelected: (tool) => setState(() => _activeLayoutTool = tool),
                          strokeWidth: _layoutStrokeWidth,
                          onStrokeWidthChanged: (w) {
                            setState(() => _layoutStrokeWidth = w);
                            _layoutDesignerKey.currentState?.applyStrokeWidth(w);
                          },
                        ),
                      Expanded(
                        child: _buildZoomableBody(context, mode, ref.watch(zoomProvider)),
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
    final zoomLevel = ref.watch(zoomProvider);
    final percent = (zoomLevel * 100).round();

    return Container(
      height: 24,
      padding: const EdgeInsets.symmetric(horizontal: 8),
      decoration: BoxDecoration(
        color: isDark ? const Color(0xFF161B22) : const Color(0xFFEBE9E4),
        border: Border(top: BorderSide(color: borderColor, width: 1.0)),
      ),
      child: Row(
        children: [
          // Zoom indicator (% of magnification + dropdown menu)
          PopupMenuButton<double>(
            tooltip: 'Zoom level ($percent%)',
            initialValue: zoomLevel,
            elevation: 4,
            padding: EdgeInsets.zero,
            color: isDark ? const Color(0xFF1E293B) : Colors.white,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(6),
              side: BorderSide(color: borderColor),
            ),
            onSelected: (val) => ref.read(zoomProvider.notifier).setZoom(val),
            itemBuilder: (context) => [
              for (final step in ZoomNotifier.zoomSteps.reversed)
                PopupMenuItem<double>(
                  value: step,
                  height: 28,
                  child: Row(
                    children: [
                      SizedBox(
                        width: 16,
                        child: (step - zoomLevel).abs() < 0.001
                            ? const Icon(Icons.check, size: 14, color: Color(0xFF0284C7))
                            : null,
                      ),
                      const SizedBox(width: 6),
                      Text(
                        '${(step * 100).round()}%${(step - 1.0).abs() < 0.001 ? " (100%)" : ""}',
                        style: TextStyle(
                          fontSize: 11,
                          fontWeight: (step - zoomLevel).abs() < 0.001
                              ? FontWeight.bold
                              : FontWeight.normal,
                        ),
                      ),
                    ],
                  ),
                ),
            ],
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 1),
              decoration: BoxDecoration(
                border: Border.all(color: Colors.grey.withValues(alpha: 0.3)),
                borderRadius: BorderRadius.circular(2),
                color: isDark ? const Color(0xFF21262D) : const Color(0xFFF3F4F6),
              ),
              child: Text(
                '$percent',
                style: const TextStyle(fontSize: 10, fontFamily: 'monospace', fontWeight: FontWeight.bold),
              ),
            ),
          ),
          const SizedBox(width: 4),

          // Zoom Out (-) Button
          Tooltip(
            message: 'Zoom Out (Cmd -)',
            child: InkWell(
              borderRadius: BorderRadius.circular(2),
              onTap: zoomLevel > ZoomNotifier.zoomSteps.first + 0.001
                  ? () => ref.read(zoomProvider.notifier).zoomOut()
                  : null,
              child: Padding(
                padding: const EdgeInsets.all(1.0),
                child: Icon(
                  Icons.zoom_out,
                  size: 14,
                  color: zoomLevel > ZoomNotifier.zoomSteps.first + 0.001
                      ? (isDark ? Colors.white70 : Colors.black87)
                      : Colors.grey.withValues(alpha: 0.35),
                ),
              ),
            ),
          ),
          const SizedBox(width: 2),

          // Zoom In (+) Button
          Tooltip(
            message: 'Zoom In (Cmd +)',
            child: InkWell(
              borderRadius: BorderRadius.circular(2),
              onTap: zoomLevel < ZoomNotifier.zoomSteps.last - 0.001
                  ? () => ref.read(zoomProvider.notifier).zoomIn()
                  : null,
              child: Padding(
                padding: const EdgeInsets.all(1.0),
                child: Icon(
                  Icons.zoom_in,
                  size: 14,
                  color: zoomLevel < ZoomNotifier.zoomSteps.last - 0.001
                      ? (isDark ? Colors.white70 : Colors.black87)
                      : Colors.grey.withValues(alpha: 0.35),
                ),
              ),
            ),
          ),
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
                if (_activeDatabaseName.isNotEmpty) ...[
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

  Widget _buildZoomableBody(BuildContext context, OperationalMode mode, double zoomLevel) {
    final child = _buildBody(context, mode);
    // In layout mode, the full designer studio manages its own scroll/canvas space
    if (mode == OperationalMode.layout || (zoomLevel - 1.0).abs() < 0.001) {
      return child;
    }

    return LayoutBuilder(
      builder: (context, constraints) {
        final scaledWidth = constraints.maxWidth * zoomLevel;
        final scaledHeight = constraints.maxHeight * zoomLevel;

        return Scrollbar(
          thumbVisibility: true,
          child: SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Scrollbar(
              thumbVisibility: true,
              child: SingleChildScrollView(
                scrollDirection: Axis.vertical,
                child: SizedBox(
                  width: math.max(constraints.maxWidth, scaledWidth),
                  height: math.max(constraints.maxHeight, scaledHeight),
                  child: Align(
                    alignment: Alignment.topLeft,
                    child: Transform.scale(
                      scale: zoomLevel,
                      alignment: Alignment.topLeft,
                      child: SizedBox(
                        width: constraints.maxWidth,
                        height: constraints.maxHeight,
                        child: child,
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        );
      },
    );
  }

  Widget _buildBody(BuildContext context, OperationalMode mode) {
    if (_currentUser == null) {
      return _buildProtectedWorkspace(context);
    }

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
          layout: _activeLayout,
          mode: mode,
          onModeChanged: _changeMode,
          onTableModified: _loadTables,
          currentUserName: _currentUser?.username,
          onGoToLayout: (name) {
            final match = _serverLayouts
                .where((l) => l.name.toLowerCase() == name.toLowerCase() || l.id == name)
                .firstOrNull;
            if (match != null) _selectLayout(match);
            return match != null;
          },
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
          tables: _tables,
          apiClient: client,
          initialLayout: _activeLayout ??
              LayoutDefinitionModel.defaultForTable(
                _selectedTable!.displayName,
                _selectedTable!.columns.map((c) => c.name).toList(),
              ),
          layouts: _serverLayouts,
          onLayoutSelected: (layout) => _selectLayout(layout),
          onNewLayout: _handleNewLayout,
          onManageLayouts: _handleManageLayouts,
          onRenameLayout: _handleRenameActiveLayout,
          onExitLayout: () => _changeMode(OperationalMode.browse),
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
            await ManageSecurityDialog.show(
              context,
              client,
              _currentUser!,
              databaseName: _activeDatabaseName,
              onModified: () async {
                AutoSaveService.instance.markDirty();
              },
            );
            await _loadUserPermissions();
            await _loadTables();
          },
          onManageScripts: () async {
            final client = ref.read(apiClientProvider);
            await ScriptWorkspaceDialog.show(
              context,
              client,
              databaseName: _activeDatabaseName,
            );
          },
          onManageThemes: () => ManageThemesDialog.show(context),
          onLayoutChanged: (updated) {
            _activeLayout = updated;
          },
          onSaved: () => _loadTables(targetLayoutId: _activeLayout?.id),
          onAutoSaveDirty: AutoSaveService.instance.markDirty,
          activeTool: _activeLayoutTool,
          onToolChanged: (tool) {
            if (mounted && tool != _activeLayoutTool) setState(() => _activeLayoutTool = tool);
          },
        );
      case OperationalMode.preview:
        return LayoutPreviewWidget(
          key: ValueKey('preview_${_selectedTable!.id}_${_activeLayout?.id}_${_activeLayout?.name}_${_activeLayout?.objects.length}'),
          table: _selectedTable!,
          apiClient: client,
          layout: _activeLayout ??
              LayoutDefinitionModel.defaultForTable(
                _selectedTable!.displayName,
                _selectedTable!.columns.map((c) => c.name).toList(),
              ),
          pageSetup: _pageSetup,
          onPageSetup: _handlePageSetup,
          currentUserName: _currentUser?.username,
          onAttach: (state) => _previewState = state,
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

  Widget _buildProtectedWorkspace(BuildContext context) {
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    return Container(
      color: theme.colorScheme.surfaceContainerHighest.withOpacity(0.2),
      child: Center(
        child: SingleChildScrollView(
          child: Card(
            elevation: 4,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(16),
              side: BorderSide(
                color: isDark ? Colors.grey.shade800 : Colors.grey.shade300,
              ),
            ),
            child: Container(
              width: 540,
              padding: const EdgeInsets.symmetric(horizontal: 36.0, vertical: 32.0),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Container(
                    width: 72,
                    height: 72,
                    decoration: BoxDecoration(
                      color: const Color(0xFF1E88E5).withOpacity(0.12),
                      shape: BoxShape.circle,
                      border: Border.all(
                        color: const Color(0xFF1E88E5).withOpacity(0.35),
                        width: 2,
                      ),
                    ),
                    child: const Icon(
                      Icons.shield_outlined,
                      size: 40,
                      color: Color(0xFF1E88E5),
                    ),
                  ),
                  const SizedBox(height: 20),
                  const Text(
                    'Protected Solution / Sign In',
                    style: TextStyle(fontSize: 22, fontWeight: FontWeight.bold),
                    textAlign: TextAlign.center,
                  ),
                  const SizedBox(height: 8),
                  Text(
                    'This database is protected against unauthorized access. Sign in with an authorized account to browse records, layouts, and schemas.',
                    textAlign: TextAlign.center,
                    style: TextStyle(
                      color: theme.colorScheme.onSurfaceVariant,
                      fontSize: 13,
                      height: 1.4,
                    ),
                  ),
                  const SizedBox(height: 18),
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
                    decoration: BoxDecoration(
                      color: isDark ? const Color(0xFF161B22) : const Color(0xFFF3F4F6),
                      borderRadius: BorderRadius.circular(8),
                      border: Border.all(
                        color: isDark ? const Color(0xFF30363D) : const Color(0xFFE5E7EB),
                      ),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const Icon(Icons.dns_outlined, size: 14, color: Colors.blueAccent),
                        const SizedBox(width: 8),
                        Text(
                          'Database: $_activeDatabaseName',
                          style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 24),
                  SizedBox(
                    width: double.infinity,
                    child: FilledButton.icon(
                      icon: const Icon(Icons.login, size: 18),
                      label: const Text('Sign In to Database'),
                      style: FilledButton.styleFrom(
                        padding: const EdgeInsets.symmetric(vertical: 14),
                        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                      ),
                      onPressed: _startAuthSequence,
                    ),
                  ),
                  const SizedBox(height: 12),
                  Row(
                    children: [
                      Expanded(
                        child: OutlinedButton.icon(
                          icon: const Icon(Icons.folder_open, size: 16),
                          label: const Text('Open Solution (.f4p)'),
                          style: OutlinedButton.styleFrom(
                            padding: const EdgeInsets.symmetric(vertical: 12),
                            shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                          ),
                          onPressed: _handleOpenSolution,
                        ),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: OutlinedButton.icon(
                          icon: const Icon(Icons.add_box_outlined, size: 16),
                          label: const Text('New Solution...'),
                          style: OutlinedButton.styleFrom(
                            padding: const EdgeInsets.symmetric(vertical: 12),
                            shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                          ),
                          onPressed: _handleNewDatabase,
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 14),
                  TextButton.icon(
                    icon: const Icon(Icons.cloud_outlined, size: 16),
                    label: const Text('Conectar a Servidor Remoto...'),
                    onPressed: () {
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
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
