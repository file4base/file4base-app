// #47 — File > Manage > Data Sources. A named connection to another SQL
// database records can be imported from. It holds no password: one is typed
// when the import runs, or taken from an environment variable of the server.

import 'package:flutter/material.dart';

import '../../core/api/api_client.dart';

class ManageDataSourcesDialog extends StatefulWidget {
  final ApiClient apiClient;

  /// Only an owner may add, change or remove a source; everyone who may see
  /// the list can still test a connection.
  final bool canEdit;

  const ManageDataSourcesDialog({super.key, required this.apiClient, required this.canEdit});

  static Future<void> show(BuildContext context, {required ApiClient apiClient, required bool canEdit}) {
    return showDialog<void>(
      context: context,
      builder: (_) => ManageDataSourcesDialog(apiClient: apiClient, canEdit: canEdit),
    );
  }

  @override
  State<ManageDataSourcesDialog> createState() => _ManageDataSourcesDialogState();
}

class _ManageDataSourcesDialogState extends State<ManageDataSourcesDialog> {
  List<DataSourceModel> _sources = const [];
  List<DataSourceEngine> _engines = const [];
  bool _busy = true;
  String? _error;
  String? _notice;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final result = await widget.apiClient.listDataSources();
      if (!mounted) return;
      setState(() {
        _sources = result.sources;
        _engines = result.engines;
        _busy = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Row(
        children: [
          Icon(Icons.storage_outlined, color: Color(0xFF1E88E5)),
          SizedBox(width: 8),
          Text('External SQL Data Sources', style: TextStyle(fontSize: 16, fontWeight: FontWeight.bold)),
        ],
      ),
      content: SizedBox(
        width: 720,
        height: 420,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text(
              'Records can be imported from PostgreSQL, MySQL / MariaDB and Microsoft SQL Server. '
              'A source holds no password: one is typed when the import runs, or read from an '
              'environment variable of the server.',
              style: TextStyle(fontSize: 11.5, color: Colors.grey),
            ),
            const SizedBox(height: 10),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Text(_error!, style: TextStyle(fontSize: 11.5, color: Colors.red.shade700)),
              ),
            if (_notice != null)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Text(_notice!, style: TextStyle(fontSize: 11.5, color: Colors.green.shade700)),
              ),
            Expanded(
              child: _busy
                  ? const Center(child: CircularProgressIndicator())
                  : _sources.isEmpty
                      ? const Center(
                          child: Text('No data source is registered yet.',
                              style: TextStyle(fontSize: 13, color: Colors.grey)),
                        )
                      : ListView.separated(
                          itemCount: _sources.length,
                          separatorBuilder: (_, __) => const Divider(height: 1),
                          itemBuilder: (context, index) {
                            final source = _sources[index];
                            return ListTile(
                              dense: true,
                              title: Text(source.name, style: const TextStyle(fontSize: 13)),
                              subtitle: Text(
                                '${source.summary}'
                                '${source.username.isEmpty ? '' : ' · ${source.username}'}'
                                '${source.passwordEnv.isEmpty ? '' : ' · password from ${source.passwordEnv}'}',
                                style: const TextStyle(fontSize: 11, color: Colors.grey),
                              ),
                              trailing: Row(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  IconButton(
                                    tooltip: 'Test connection',
                                    icon: const Icon(Icons.network_check, size: 18),
                                    onPressed: () => _test(source),
                                  ),
                                  if (widget.canEdit) ...[
                                    IconButton(
                                      tooltip: 'Edit',
                                      icon: const Icon(Icons.edit_outlined, size: 18),
                                      onPressed: () => _edit(source),
                                    ),
                                    IconButton(
                                      tooltip: 'Delete',
                                      icon: Icon(Icons.delete_outline, size: 18, color: Colors.red.shade400),
                                      onPressed: () => _delete(source),
                                    ),
                                  ],
                                ],
                              ),
                            );
                          },
                        ),
            ),
            if (!widget.canEdit)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(
                  'Only an owner can add or change a data source: it names a machine the server connects to.',
                  style: TextStyle(fontSize: 11, color: Colors.orange.shade800),
                ),
              ),
          ],
        ),
      ),
      actions: [
        if (widget.canEdit)
          OutlinedButton.icon(
            icon: const Icon(Icons.add, size: 16),
            label: const Text('New data source...'),
            onPressed: _busy ? null : () => _edit(null),
          ),
        FilledButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Done')),
      ],
    );
  }

  /// Listing the tables is the test: a source that answers its catalog is a
  /// source an import can read.
  Future<void> _test(DataSourceModel source) async {
    final password = await _askPassword(source);
    if (password == null || !mounted) return;
    setState(() {
      _busy = true;
      _error = null;
      _notice = null;
    });
    try {
      final tables = await widget.apiClient.listDataSourceTables(source.id, password: password);
      if (!mounted) return;
      setState(() {
        _busy = false;
        _notice = '${source.name}: connected, ${tables.length} table(s).';
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = '${source.name}: ${e.toString().replaceFirst('Exception: ', '')}';
      });
    }
  }

  Future<String?> _askPassword(DataSourceModel source) async {
    if (source.passwordEnv.isNotEmpty) return '';
    return showDialog<String>(
      context: context,
      builder: (_) => _PasswordPrompt(sourceName: source.name),
    );
  }

  Future<void> _edit(DataSourceModel? source) async {
    final edited = await showDialog<DataSourceModel>(
      context: context,
      builder: (_) => _DataSourceEditor(source: source, engines: _engines),
    );
    if (edited == null || !mounted) return;

    setState(() {
      _busy = true;
      _error = null;
      _notice = null;
    });
    try {
      if (source == null) {
        await widget.apiClient.createDataSource(edited);
      } else {
        await widget.apiClient.updateDataSource(source.id, edited);
      }
      await _load();
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  Future<void> _delete(DataSourceModel source) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete Data Source', style: TextStyle(fontSize: 15)),
        content: Text('Delete "${source.name}"? Records already imported stay as they are.',
            style: const TextStyle(fontSize: 13)),
        actions: [
          TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Colors.red.shade700),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    try {
      await widget.apiClient.deleteDataSource(source.id);
      await _load();
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = e.toString().replaceFirst('Exception: ', ''));
    }
  }
}

/// Asks for the password of one connection, for one use.
class _PasswordPrompt extends StatefulWidget {
  final String sourceName;

  const _PasswordPrompt({required this.sourceName});

  @override
  State<_PasswordPrompt> createState() => _PasswordPromptState();
}

class _PasswordPromptState extends State<_PasswordPrompt> {
  final TextEditingController _controller = TextEditingController();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text('Password for ${widget.sourceName}', style: const TextStyle(fontSize: 15)),
      content: TextField(
        controller: _controller,
        autofocus: true,
        obscureText: true,
        decoration: const InputDecoration(
          labelText: 'Password',
          helperText: 'Used for this connection only; it is not stored.',
          border: OutlineInputBorder(),
          isDense: true,
        ),
        onSubmitted: (value) => Navigator.of(context).pop(value),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(_controller.text),
          child: const Text('Connect'),
        ),
      ],
    );
  }
}

/// The settings of one connection. There is no password field: the catalog
/// stores none.
class _DataSourceEditor extends StatefulWidget {
  final DataSourceModel? source;
  final List<DataSourceEngine> engines;

  const _DataSourceEditor({required this.source, required this.engines});

  @override
  State<_DataSourceEditor> createState() => _DataSourceEditorState();
}

class _DataSourceEditorState extends State<_DataSourceEditor> {
  late final TextEditingController _name = TextEditingController(text: widget.source?.name ?? '');
  late final TextEditingController _host = TextEditingController(text: widget.source?.host ?? '');
  late final TextEditingController _port =
      TextEditingController(text: (widget.source?.port ?? 0) > 0 ? '${widget.source!.port}' : '');
  late final TextEditingController _database = TextEditingController(text: widget.source?.database ?? '');
  late final TextEditingController _username = TextEditingController(text: widget.source?.username ?? '');
  late final TextEditingController _schema = TextEditingController(text: widget.source?.schema ?? '');
  late final TextEditingController _passwordEnv =
      TextEditingController(text: widget.source?.passwordEnv ?? '');
  late String _engine = widget.source?.engine ?? (widget.engines.isEmpty ? 'postgres' : widget.engines.first.engine);
  late bool _tls = widget.source?.tls ?? false;

  @override
  void dispose() {
    for (final c in [_name, _host, _port, _database, _username, _schema, _passwordEnv]) {
      c.dispose();
    }
    super.dispose();
  }

  int get _defaultPort =>
      widget.engines.where((e) => e.engine == _engine).map((e) => e.defaultPort).firstOrNull ?? 0;

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(widget.source == null ? 'New Data Source' : 'Edit Data Source',
          style: const TextStyle(fontSize: 15)),
      content: SizedBox(
        width: 520,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: _name,
                autofocus: true,
                decoration: const InputDecoration(
                    labelText: 'Name', border: OutlineInputBorder(), isDense: true),
              ),
              const SizedBox(height: 10),
              DropdownButtonFormField<String>(
                initialValue: _engine,
                isExpanded: true,
                decoration: const InputDecoration(
                    labelText: 'Engine', border: OutlineInputBorder(), isDense: true),
                items: [
                  for (final engine in widget.engines)
                    DropdownMenuItem(value: engine.engine, child: Text(engine.label)),
                ],
                onChanged: (value) => setState(() => _engine = value ?? _engine),
              ),
              const SizedBox(height: 10),
              Row(
                children: [
                  Expanded(
                    flex: 3,
                    child: TextField(
                      controller: _host,
                      decoration: const InputDecoration(
                          labelText: 'Host', border: OutlineInputBorder(), isDense: true),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: TextField(
                      controller: _port,
                      keyboardType: TextInputType.number,
                      decoration: InputDecoration(
                        labelText: 'Port',
                        hintText: _defaultPort > 0 ? '$_defaultPort' : null,
                        border: const OutlineInputBorder(),
                        isDense: true,
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 10),
              Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _database,
                      decoration: const InputDecoration(
                          labelText: 'Database', border: OutlineInputBorder(), isDense: true),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: TextField(
                      controller: _schema,
                      decoration: const InputDecoration(
                        labelText: 'Schema',
                        hintText: 'public / dbo',
                        border: OutlineInputBorder(),
                        isDense: true,
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 10),
              TextField(
                controller: _username,
                decoration: const InputDecoration(
                    labelText: 'Username', border: OutlineInputBorder(), isDense: true),
              ),
              const SizedBox(height: 10),
              TextField(
                controller: _passwordEnv,
                decoration: const InputDecoration(
                  labelText: 'Password from environment variable (optional)',
                  helperText: 'The name of a variable of the server, such as LEGACY_DB_PASSWORD. '
                      'Leave it empty to type the password at each import.',
                  helperMaxLines: 3,
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
              ),
              const SizedBox(height: 4),
              CheckboxListTile(
                dense: true,
                contentPadding: EdgeInsets.zero,
                controlAffinity: ListTileControlAffinity.leading,
                value: _tls,
                onChanged: (value) => setState(() => _tls = value ?? false),
                title: const Text('Encrypt the connection (TLS)', style: TextStyle(fontSize: 12.5)),
              ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(
          onPressed: _name.text.trim().isEmpty && _host.text.trim().isEmpty
              ? null
              : () => Navigator.of(context).pop(DataSourceModel(
                    id: widget.source?.id ?? '',
                    name: _name.text.trim(),
                    engine: _engine,
                    host: _host.text.trim(),
                    port: int.tryParse(_port.text.trim()) ?? 0,
                    database: _database.text.trim(),
                    username: _username.text.trim(),
                    schema: _schema.text.trim(),
                    tls: _tls,
                    passwordEnv: _passwordEnv.text.trim(),
                  )),
          child: const Text('Save'),
        ),
      ],
    );
  }
}
