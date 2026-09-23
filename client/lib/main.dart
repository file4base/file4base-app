import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'core/api/api_client.dart';
import 'core/widgets/file4base_menu_bar.dart';
import 'features/about/about_dialog.dart';
import 'features/connection/server_connection_dialog.dart';
import 'features/data_browser/data_browser_widget.dart';
import 'features/layout_engine/layout_designer_widget.dart';
import 'features/layout_engine/layout_preview_widget.dart';
import 'features/layout_engine/models/layout_definition.dart';
import 'features/preflight/preflight_dialog.dart';
import 'features/schema_manager/manage_database_dialog.dart';

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
  List<TableModel> _tables = [];
  TableModel? _selectedTable;
  LayoutDefinitionModel? _activeLayout;
  bool _isLoadingTables = false;
  bool _isToolbarVisible = true;
  final GlobalKey<DataBrowserWidgetState> _dataBrowserKey = GlobalKey<DataBrowserWidgetState>();

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      PreflightDialog.showIfNeeded(context, onProceed: () {
        _checkServer();
      });
    });
  }

  Future<void> _checkServer() async {
    final client = ref.read(apiClientProvider);
    try {
      final health = await client.checkHealth();
      if (mounted) {
        setState(() {
          _serverStatus = 'Online (${health['engine']})';
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

  Future<void> _loadTables() async {
    final client = ref.read(apiClientProvider);
    setState(() => _isLoadingTables = true);
    try {
      final tables = await client.listTables();
      if (mounted) {
        setState(() {
          _tables = tables;
          _isLoadingTables = false;
          if (tables.isNotEmpty) {
            if (_selectedTable == null || !tables.any((t) => t.id == _selectedTable!.id)) {
              _selectedTable = tables.first;
              _initDefaultLayout(tables.first);
            }
          } else {
            _selectedTable = null;
            _activeLayout = null;
          }
        });
      }
    } catch (_) {
      if (mounted) setState(() => _isLoadingTables = false);
    }
  }

  void _initDefaultLayout(TableModel table) {
    _activeLayout = LayoutDefinitionModel.defaultForTable(
      table.displayName,
      table.columns.map((c) => c.name).toList(),
    );
  }

  @override
  Widget build(BuildContext context) {
    final mode = ref.watch(operationalModeProvider);
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final brandLogo = isDark
        ? 'assets/branding/file4base-dark.png'
        : 'assets/branding/file4base-light.png';

    return CallbackShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.keyB, meta: true): () {
          ref.read(operationalModeProvider.notifier).setMode(OperationalMode.browse);
        },
        const SingleActivator(LogicalKeyboardKey.keyF, meta: true): () {
          ref.read(operationalModeProvider.notifier).setMode(OperationalMode.find);
        },
        const SingleActivator(LogicalKeyboardKey.keyL, meta: true): () {
          ref.read(operationalModeProvider.notifier).setMode(OperationalMode.layout);
        },
        const SingleActivator(LogicalKeyboardKey.keyU, meta: true): () {
          ref.read(operationalModeProvider.notifier).setMode(OperationalMode.preview);
        },
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
                  onModeChanged: (newMode) => ref.read(operationalModeProvider.notifier).setMode(newMode),
                  onManageDatabase: () async {
                    await ManageDatabaseDialog.show(context);
                    _loadTables();
                  },
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
                  onNewRecord: () => _dataBrowserKey.currentState?.createNewRecord(),
                  onDuplicateRecord: () => _dataBrowserKey.currentState?.createNewRecord(),
                  onDeleteRecord: () => _dataBrowserKey.currentState?.deleteCurrentRecord(),
                  onShowAllRecords: () => _dataBrowserKey.currentState?.fetchRecords(),
                  onPerformFind: () => _dataBrowserKey.currentState?.performFind(),
                  isToolbarVisible: _isToolbarVisible,
                  onToggleToolbar: (visible) => setState(() => _isToolbarVisible = visible),
                ),
                if (_isToolbarVisible)
                  _buildStatusToolbar(context, mode, isDark, brandLogo),
                Expanded(
                  child: _buildBody(context, mode),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildStatusToolbar(BuildContext context, OperationalMode mode, bool isDark, String brandLogo) {
    final theme = Theme.of(context);
    return Container(
      height: 48,
      padding: const EdgeInsets.symmetric(horizontal: 12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        border: Border(
          bottom: BorderSide(
            color: theme.dividerColor.withOpacity(0.15),
            width: 1.0,
          ),
        ),
      ),
      child: Row(
        children: [
          ClipRRect(
            borderRadius: BorderRadius.circular(6.0),
            child: Image.asset(
              brandLogo,
              width: 24,
              height: 24,
              fit: BoxFit.cover,
              errorBuilder: (context, error, stackTrace) => Image.asset(
                'assets/branding/file4base-icon-64.png',
                width: 24,
                height: 24,
                errorBuilder: (_, __, ___) => const Icon(Icons.table_chart, size: 20),
              ),
            ),
          ),
          const SizedBox(width: 8),
          const Text('File4Base', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
          if (_tables.isNotEmpty && _selectedTable != null) ...[
            const SizedBox(width: 14),
            Container(
              height: 30,
              padding: const EdgeInsets.symmetric(horizontal: 8),
              decoration: BoxDecoration(
                color: theme.colorScheme.surfaceContainerHighest.withOpacity(0.5),
                borderRadius: BorderRadius.circular(6),
              ),
              child: DropdownButtonHideUnderline(
                child: DropdownButton<String>(
                  value: _selectedTable!.id,
                  isDense: true,
                  icon: const Icon(Icons.arrow_drop_down, size: 18),
                  items: _tables.map((t) {
                    return DropdownMenuItem(
                      value: t.id,
                      child: Text(t.displayName, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600)),
                    );
                  }).toList(),
                  onChanged: (newId) {
                    if (newId != null) {
                      final match = _tables.firstWhere((t) => t.id == newId);
                      setState(() {
                        _selectedTable = match;
                        _initDefaultLayout(match);
                      });
                    }
                  },
                ),
              ),
            ),
          ],
          const Spacer(),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 6.0),
            child: SegmentedButton<OperationalMode>(
              segments: const [
                ButtonSegment(value: OperationalMode.browse, label: Text('Browse', style: TextStyle(fontSize: 12))),
                ButtonSegment(value: OperationalMode.find, label: Text('Find', style: TextStyle(fontSize: 12))),
                ButtonSegment(value: OperationalMode.layout, label: Text('Layout', style: TextStyle(fontSize: 12))),
                ButtonSegment(value: OperationalMode.preview, label: Text('Preview', style: TextStyle(fontSize: 12))),
              ],
              selected: {mode},
              onSelectionChanged: (newSelection) {
                ref.read(operationalModeProvider.notifier).setMode(newSelection.first);
              },
            ),
          ),
          FilledButton.tonalIcon(
            style: FilledButton.styleFrom(visualDensity: VisualDensity.compact),
            icon: const Icon(Icons.storage, size: 15),
            label: const Text('Manage Database...', style: TextStyle(fontSize: 12)),
            onPressed: () async {
              await ManageDatabaseDialog.show(context);
              _loadTables();
            },
          ),
          const SizedBox(width: 6),
          IconButton(
            icon: const Icon(Icons.info_outline, size: 18),
            tooltip: 'About File4Base',
            onPressed: () => AboutFile4BaseDialog.show(context, serverStatus: _serverStatus),
          ),
          const SizedBox(width: 4),
          Tooltip(
            message: 'Click to configure Server Host & Port',
            child: InkWell(
              borderRadius: BorderRadius.circular(14),
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
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                decoration: BoxDecoration(
                  color: _serverStatus.startsWith('Online')
                      ? Colors.green.withOpacity(0.12)
                      : Colors.orange.withOpacity(0.12),
                  borderRadius: BorderRadius.circular(14),
                  border: Border.all(
                    color: _serverStatus.startsWith('Online')
                        ? Colors.green.withOpacity(0.3)
                        : Colors.orange.withOpacity(0.3),
                  ),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      _serverStatus.startsWith('Online') ? Icons.cloud_done : Icons.cloud_off,
                      size: 14,
                      color: _serverStatus.startsWith('Online') ? Colors.green : Colors.orange,
                    ),
                    const SizedBox(width: 5),
                    Text(_serverStatus, style: const TextStyle(fontSize: 11, fontWeight: FontWeight.w600)),
                    const SizedBox(width: 3),
                    const Icon(Icons.settings, size: 11, color: Colors.grey),
                  ],
                ),
              ),
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
        );
      case OperationalMode.layout:
        return LayoutDesignerWidget(
          key: ValueKey('layout_${_selectedTable!.id}'),
          table: _selectedTable!,
          apiClient: client,
          initialLayout: _activeLayout ??
              LayoutDefinitionModel.defaultForTable(
                _selectedTable!.displayName,
                _selectedTable!.columns.map((c) => c.name).toList(),
              ),
          onSaved: () {
            _loadTables();
          },
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
                  'Open-source FileMaker Pro alternative with dynamic schema, relational occurrences, visual layouts, and 4 operational modes.',
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
