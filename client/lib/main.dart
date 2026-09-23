import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'core/api/api_client.dart';
import 'features/preflight/preflight_dialog.dart';

enum OperationalMode {
  browse,
  find,
  layout,
  preview,
}

final apiClientProvider = Provider<ApiClient>((ref) {
  final client = ApiClient();
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
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          _serverStatus = 'Offline';
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final mode = ref.watch(operationalModeProvider);

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
          appBar: AppBar(
            title: const Row(
              children: [
                Icon(Icons.table_chart, size: 22),
                SizedBox(width: 8),
                Text('File4Base', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 18)),
              ],
            ),
            actions: [
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 8.0),
                child: SegmentedButton<OperationalMode>(
                  segments: const [
                    ButtonSegment(value: OperationalMode.browse, label: Text('Browse')),
                    ButtonSegment(value: OperationalMode.find, label: Text('Find')),
                    ButtonSegment(value: OperationalMode.layout, label: Text('Layout')),
                    ButtonSegment(value: OperationalMode.preview, label: Text('Preview')),
                  ],
                  selected: {mode},
                  onSelectionChanged: (newSelection) {
                    ref.read(operationalModeProvider.notifier).setMode(newSelection.first);
                  },
                ),
              ),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16.0),
                child: Row(
                  children: [
                    Icon(
                      _serverStatus.startsWith('Online') ? Icons.cloud_done : Icons.cloud_off,
                      size: 16,
                      color: _serverStatus.startsWith('Online') ? Colors.green : Colors.orange,
                    ),
                    const SizedBox(width: 6),
                    Text(_serverStatus, style: const TextStyle(fontSize: 12)),
                  ],
                ),
              ),
            ],
          ),
          body: Container(
            color: Theme.of(context).colorScheme.surfaceContainerHighest.withOpacity(0.3),
            child: Center(
              child: Card(
                elevation: 2,
                child: Padding(
                  padding: const EdgeInsets.all(24.0),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(
                        _getModeIcon(mode),
                        size: 48,
                        color: Theme.of(context).colorScheme.primary,
                      ),
                      const SizedBox(height: 12),
                      Text(
                        _getModeTitle(mode),
                        style: const TextStyle(fontSize: 20, fontWeight: FontWeight.bold),
                      ),
                      const SizedBox(height: 8),
                      Text(
                        _getModeDescription(mode),
                        textAlign: TextAlign.center,
                        style: TextStyle(color: Theme.of(context).colorScheme.onSurfaceVariant),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  IconData _getModeIcon(OperationalMode mode) {
    switch (mode) {
      case OperationalMode.browse:
        return Icons.visibility;
      case OperationalMode.find:
        return Icons.search;
      case OperationalMode.layout:
        return Icons.design_services;
      case OperationalMode.preview:
        return Icons.print;
    }
  }

  String _getModeTitle(OperationalMode mode) {
    switch (mode) {
      case OperationalMode.browse:
        return 'Browse Mode Active';
      case OperationalMode.find:
        return 'Find Mode Active';
      case OperationalMode.layout:
        return 'Layout Designer Active';
      case OperationalMode.preview:
        return 'Preview / Print Mode Active';
    }
  }

  String _getModeDescription(OperationalMode mode) {
    switch (mode) {
      case OperationalMode.browse:
        return 'Record viewing and inline data entry.\nSwitch views: Form, List, or Table.';
      case OperationalMode.find:
        return 'Enter search criteria into fields using operators (=, !, >, <, *, ...).';
      case OperationalMode.layout:
        return 'WYSIWYG layout designer with snap-to-grid, parts, and property inspector.';
      case OperationalMode.preview:
        return 'Print preview calculating exact page breaks and summary totals.';
    }
  }
}
