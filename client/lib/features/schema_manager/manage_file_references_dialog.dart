// #107 — File > Manage > File References: the named links from this
// solution to other solutions, FileMaker's external data sources. A
// reference is mapped to a solution of this server; binding it resolves the
// table each external table occurrence through it reads there, and verifying
// it checks that table and the fields used through it are still there.
//
// Binding and verifying read the other solution's tables and fields, so they
// run on the session this one opened there, of an owner or an administrator.

import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/external_access.dart';
import 'package:flutter/material.dart';

/// What can be done about a failed bind, verify, save or delete.
enum FileReferenceRemedy { none, signIn, signInAsAdmin, edit }

/// A failure, said so that the user knows which file reference and which
/// solution it is about, and what to do next.
class FileReferenceProblem {
  const FileReferenceProblem(this.message, [this.remedy = FileReferenceRemedy.none]);
  final String message;
  final FileReferenceRemedy remedy;
}

/// Describes [error], met while working on [reference].
FileReferenceProblem describeFileReferenceError(Object error, FileReferenceModel reference) {
  final name = '"${reference.name}"';
  final db = reference.database;
  if (error is! ApiException) {
    return FileReferenceProblem('$name: ${error.toString().replaceFirst('Exception: ', '')}');
  }
  final detail = error.detail;
  switch (error.statusCode) {
    case 400 when error.title == 'Peer Token Required':
      return FileReferenceProblem('Sign in to $db to read its tables for $name.', FileReferenceRemedy.signIn);
    case 403 when detail.contains('owner or an administrator'):
      return FileReferenceProblem(
        'Binding and verifying $name read the tables and fields of $db, which needs an owner or '
        'administrator account there. Sign in to $db with such an account.',
        FileReferenceRemedy.signInAsAdmin,
      );
    case 403 when error.title == 'Peer Token Rejected':
      return FileReferenceProblem('Your session in $db has ended. Sign in to $db again.', FileReferenceRemedy.signIn);
    case 403:
      return FileReferenceProblem('$name: $detail. Only an owner or an administrator of this solution manages file references.');
    case 404:
      return FileReferenceProblem('$name is no longer in this solution; the list was read again.');
    case 409 when error.title == 'File Reference In Use':
      return FileReferenceProblem(
        '$name is still in use: $detail. Delete the table occurrences that go through it in the '
        'relationship graph first.',
      );
    case 409 when error.title == 'File Reference Already Exists':
      return FileReferenceProblem('A file reference named $name already exists. Choose another name.', FileReferenceRemedy.edit);
    case 409 when error.title == 'File Reference Not Mapped':
      return FileReferenceProblem('$name is not mapped to a solution. Choose its solution with Edit…, then Bind.', FileReferenceRemedy.edit);
    case 422:
      return FileReferenceProblem('$name: $detail.', FileReferenceRemedy.edit);
    case 503:
      return FileReferenceProblem(
        'The solution $db of $name is not reachable on this server. Check that it is installed and '
        'open, or map $name to another solution with Edit….',
        FileReferenceRemedy.edit,
      );
    default:
      return FileReferenceProblem('$name: ${detail.isNotEmpty ? detail : error.title}.');
  }
}

/// The token of the session this one holds in [database], asking for an
/// account there when it holds none.
Future<String?> fileReferencePeerToken(BuildContext context, ApiClient apiClient, String database) =>
    ExternalAccess.of(apiClient).peerToken(context, database);

class ManageFileReferencesDialog extends StatefulWidget {
  const ManageFileReferencesDialog({
    required this.apiClient,
    this.canEdit = true,
    this.solutionName = '',
    this.onChanged,
    super.key,
  });

  final ApiClient apiClient;

  /// Called on closing when a reference was added, changed, bound or
  /// deleted.
  final VoidCallback? onChanged;

  /// Only an owner or an administrator may add, change, bind or verify a
  /// reference; anyone else sees where each one goes.
  final bool canEdit;

  /// The solution signed in to, named in the title.
  final String solutionName;

  /// Shows the dialog; true when a reference was added, changed, bound or
  /// deleted, so what reads through one should be read again.
  static Future<bool> show(BuildContext context, {required ApiClient apiClient, bool canEdit = true, String solutionName = ''}) async {
    var changed = false;
    await showDialog<void>(
      context: context,
      builder: (_) => ManageFileReferencesDialog(
        apiClient: apiClient,
        canEdit: canEdit,
        solutionName: solutionName,
        onChanged: () => changed = true,
      ),
    );
    return changed;
  }

  @override
  State<ManageFileReferencesDialog> createState() => _ManageFileReferencesDialogState();
}

class _ManageFileReferencesDialogState extends State<ManageFileReferencesDialog> {
  List<FileReferenceModel> _references = const [];
  bool _loading = true;
  String? _error;
  String? _selectedId;
  final Map<String, ExternalBindingReport> _reports = {};
  final Map<String, FileReferenceProblem> _problems = {};
  final Map<String, Map<String, String>> _tableChoices = {};
  final Map<String, List<String>> _peerTables = {};
  final Set<String> _working = {};
  bool _changed = false;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void dispose() {
    // Views that read through a remapped or rebound reference read again.
    if (_changed) {
      ExternalAccess.of(widget.apiClient).refresh();
      widget.onChanged?.call();
    }
    super.dispose();
  }

  FileReferenceModel? get _selected => _references.where((r) => r.id == _selectedId).firstOrNull;

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final refs = await widget.apiClient.listFileReferences();
      if (!mounted) return;
      setState(() {
        _references = refs..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
        if (_selected == null) _selectedId = refs.isEmpty ? null : refs.first.id;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = 'The file references of this solution could not be read: '
            '${e is ApiException ? e.detail : e}';
      });
    }
  }

  Future<void> _edit([FileReferenceModel? existing]) async {
    final saved = await FileReferenceEditor.show(context, apiClient: widget.apiClient, existing: existing);
    if (saved == null || !mounted) return;
    _changed = true;
    _reports.remove(saved.id);
    _problems.remove(saved.id);
    // Tables chosen for its occurrences were tables of the solution it was
    // mapped to: they mean nothing in another one.
    _tableChoices.remove(saved.id);
    _peerTables.remove(saved.id);
    _selectedId = saved.id;
    await _load();
  }

  Future<void> _delete(FileReferenceModel ref) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete File Reference'),
        content: Text('Delete the file reference "${ref.name}"? Scripts that name it stop at that step '
            'until it is added again.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Colors.red.shade700),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    try {
      await widget.apiClient.deleteFileReference(ref.id);
      _changed = true;
      _reports.remove(ref.id);
      _problems.remove(ref.id);
      if (_selectedId == ref.id) _selectedId = null;
      await _load();
    } catch (e) {
      if (!mounted) return;
      setState(() => _problems[ref.id] = describeFileReferenceError(e, ref));
    }
  }

  /// Signs in to the reference's solution, as another account when [again].
  Future<void> _signIn(FileReferenceModel ref, {bool again = false}) async {
    if (again) widget.apiClient.forgetLinked(ref.database);
    final ok = await ExternalAccess.of(widget.apiClient).signInTo(context, ref.database);
    if (ok && mounted) setState(() => _problems.remove(ref.id));
  }

  Future<void> _run(FileReferenceModel ref, {required bool bind}) async {
    if (!ref.isMapped) {
      if (!bind) {
        // Nothing to read: every occurrence through it is unbound.
        await _call(ref, () => widget.apiClient.verifyFileReference(ref.id));
        return;
      }
      setState(() => _problems[ref.id] = FileReferenceProblem(
            '"${ref.name}" is not mapped to a solution. Choose its solution with Edit…, then Bind.',
            FileReferenceRemedy.edit,
          ));
      return;
    }
    final token = await fileReferencePeerToken(context, widget.apiClient, ref.database);
    if (!mounted) return;
    if (token == null) {
      setState(() => _problems[ref.id] = FileReferenceProblem(
            '${bind ? 'Binding' : 'Verifying'} "${ref.name}" reads the tables of ${ref.database}, and this '
            'session is not signed in to it. Sign in to ${ref.database}.',
            FileReferenceRemedy.signIn,
          ));
      return;
    }
    await _call(
      ref,
      () => bind
          ? widget.apiClient.bindFileReference(ref.id,
              database: ref.database, peerToken: token, tables: _tableChoices[ref.id] ?? const {})
          : widget.apiClient.verifyFileReference(ref.id, peerToken: token),
      peerToken: token,
    );
    if (bind) _changed = true;
  }

  Future<void> _call(FileReferenceModel ref, Future<ExternalBindingReport> Function() call, {String? peerToken}) async {
    setState(() {
      _working.add(ref.id);
      _problems.remove(ref.id);
    });
    try {
      final report = await call();
      if (!mounted) return;
      setState(() => _reports[ref.id] = report);
      // An occurrence no field singles a table out for is bound by hand,
      // from the tables of that solution.
      final needsTables = report.occurrences.any((o) =>
          o.status == ExternalOccurrenceStatus.mismatch ||
          (o.status == ExternalOccurrenceStatus.unbound && o.candidates.isEmpty));
      if (needsTables && peerToken != null && !_peerTables.containsKey(ref.id)) {
        final tables = await widget.apiClient.forPeerSession(peerToken).listTables();
        if (mounted) setState(() => _peerTables[ref.id] = [for (final t in tables) t.name]);
      }
    } catch (e) {
      if (!mounted) return;
      final problem = describeFileReferenceError(e, ref);
      if (e is ApiException && e.title == 'Peer Token Rejected') ExternalAccess.of(widget.apiClient).peerRejected(ref.database);
      setState(() => _problems[ref.id] = problem);
      if (e is ApiException && e.statusCode == 404) unawaited(_load());
    } finally {
      if (mounted) setState(() => _working.remove(ref.id));
    }
  }

  @override
  Widget build(BuildContext context) {
    final selected = _selected;
    return Dialog(
      child: SizedBox(
        width: 820,
        height: 560,
        child: Column(
          children: [
            ListTile(
              leading: const Icon(Icons.link),
              title: const Text('Manage File References', style: TextStyle(fontWeight: FontWeight.bold)),
              subtitle: Text(
                'The other solutions ${widget.solutionName.isEmpty ? 'this solution' : '"${widget.solutionName}"'} '
                'reads tables from and performs scripts in',
                style: const TextStyle(fontSize: 12),
              ),
              trailing: IconButton(icon: const Icon(Icons.close), onPressed: () => Navigator.of(context).pop()),
            ),
            const Divider(height: 1),
            Expanded(
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  SizedBox(width: 300, child: _buildList()),
                  const VerticalDivider(width: 1),
                  Expanded(
                    child: selected == null
                        ? const Center(
                            child: Text('Select a file reference.', style: TextStyle(fontSize: 12, color: Colors.grey)))
                        : _buildDetail(selected),
                  ),
                ],
              ),
            ),
            const Divider(height: 1),
            Padding(
              padding: const EdgeInsets.all(12),
              child: Row(
                children: [
                  if (widget.canEdit)
                    FilledButton.icon(
                      key: const ValueKey('file-reference-new'),
                      icon: const Icon(Icons.add, size: 16),
                      label: const Text('New…'),
                      onPressed: () => _edit(),
                    ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(
                      widget.canEdit
                          ? 'A file reference names a solution of this server. Bind it to find the table '
                              'each occurrence through it reads, and verify it after that solution changes.'
                          : 'Only an owner or an administrator can add, bind or verify a file reference.',
                      style: const TextStyle(fontSize: 11, color: Colors.grey),
                    ),
                  ),
                  TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Close')),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildList() {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error != null) {
      return Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(_error!, style: const TextStyle(fontSize: 12)),
            const SizedBox(height: 8),
            OutlinedButton(onPressed: _load, child: const Text('Retry')),
          ],
        ),
      );
    }
    if (_references.isEmpty) {
      return const Padding(
        padding: EdgeInsets.all(24),
        child: Text(
          'No file references yet.\n\nAdd one to show a table of another solution in the '
          'relationship graph, or to perform its scripts.',
          textAlign: TextAlign.center,
          style: TextStyle(fontSize: 12, color: Colors.grey),
        ),
      );
    }
    return ListView.separated(
      itemCount: _references.length,
      separatorBuilder: (_, _) => const Divider(height: 1),
      itemBuilder: (_, i) {
        final ref = _references[i];
        return ListTile(
          key: ValueKey('file-reference-${ref.id}'),
          dense: true,
          selected: ref.id == _selectedId,
          leading: Icon(ref.isMapped ? Icons.link : Icons.link_off, size: 18),
          title: Text(ref.name, style: const TextStyle(fontSize: 13)),
          subtitle: Text(ref.isMapped ? ref.database : 'Not mapped', style: const TextStyle(fontSize: 11)),
          trailing: _reportBadge(_reports[ref.id]),
          onTap: () => setState(() => _selectedId = ref.id),
        );
      },
    );
  }

  Widget? _reportBadge(ExternalBindingReport? report) {
    if (report == null || report.occurrences.isEmpty) return null;
    final bound = report.occurrences.where((o) => o.status == ExternalOccurrenceStatus.bound).length;
    final ok = bound == report.occurrences.length;
    return Text('$bound/${report.occurrences.length} bound',
        style: TextStyle(fontSize: 11, color: ok ? Colors.green.shade700 : Colors.orange.shade800));
  }

  Widget _buildDetail(FileReferenceModel ref) {
    final report = _reports[ref.id];
    final problem = _problems[ref.id];
    final working = _working.contains(ref.id);
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        Text(ref.name, style: const TextStyle(fontSize: 15, fontWeight: FontWeight.bold)),
        const SizedBox(height: 8),
        _line('Solution:', ref.isMapped ? ref.database : 'Not mapped yet'),
        if (ref.paths.isNotEmpty) _line('FileMaker paths:', ref.paths.join('\n')),
        const SizedBox(height: 8),
        if (widget.canEdit)
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              OutlinedButton(
                key: const ValueKey('file-reference-edit'),
                onPressed: working ? null : () => _edit(ref),
                child: const Text('Edit…'),
              ),
              FilledButton(
                key: const ValueKey('file-reference-bind'),
                onPressed: working ? null : () => _run(ref, bind: true),
                child: const Text('Bind'),
              ),
              OutlinedButton(
                key: const ValueKey('file-reference-verify'),
                onPressed: working ? null : () => _run(ref, bind: false),
                child: const Text('Verify'),
              ),
              TextButton(
                key: const ValueKey('file-reference-delete'),
                onPressed: working ? null : () => _delete(ref),
                child: Text('Delete', style: TextStyle(color: Colors.red.shade700)),
              ),
            ],
          ),
        if (working) const Padding(padding: EdgeInsets.only(top: 12), child: LinearProgressIndicator()),
        if (problem != null) _problemNotice(ref, problem),
        if (report != null) ..._reportView(ref, report),
      ],
    );
  }

  Widget _line(String label, String value) => Padding(
        padding: const EdgeInsets.only(bottom: 6),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SizedBox(width: 120, child: Text(label, style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w500))),
            Expanded(child: SelectableText(value, style: const TextStyle(fontSize: 12))),
          ],
        ),
      );

  Widget _problemNotice(FileReferenceModel ref, FileReferenceProblem problem) {
    final Widget? action = switch (problem.remedy) {
      FileReferenceRemedy.signIn => TextButton(
          key: const ValueKey('file-reference-sign-in'),
          onPressed: () => _signIn(ref),
          child: Text('Sign in to ${ref.database}…'),
        ),
      FileReferenceRemedy.signInAsAdmin => TextButton(
          key: const ValueKey('file-reference-sign-in'),
          onPressed: () => _signIn(ref, again: true),
          child: Text('Sign in to ${ref.database} as another account…'),
        ),
      FileReferenceRemedy.edit when widget.canEdit =>
        TextButton(onPressed: () => _edit(ref), child: const Text('Edit…')),
      _ => null,
    };
    return Container(
      key: const ValueKey('file-reference-problem'),
      margin: const EdgeInsets.only(top: 12),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: Colors.amber.withValues(alpha: 0.12),
        border: Border.all(color: Colors.amber),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(problem.message, style: const TextStyle(fontSize: 12)),
          ?action,
        ],
      ),
    );
  }

  List<Widget> _reportView(FileReferenceModel ref, ExternalBindingReport report) {
    if (report.occurrences.isEmpty) {
      return const [
        Padding(
          padding: EdgeInsets.only(top: 16),
          child: Text(
            'No table occurrence goes through this file reference yet. Add one in the relationship '
            'graph: Table…, then choose this file reference as the data source.',
            style: TextStyle(fontSize: 12, color: Colors.grey),
          ),
        ),
      ];
    }
    return [
      const SizedBox(height: 16),
      Text(
        report.allBound
            ? 'Every table occurrence through "${ref.name}" is bound to ${report.database}.'
            : 'Some table occurrences through "${ref.name}" need attention.',
        style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
      ),
      for (final occ in report.occurrences) _occurrenceStatus(ref, occ),
    ];
  }

  Widget _occurrenceStatus(FileReferenceModel ref, ExternalOccurrenceStatus occ) {
    final (IconData icon, Color color, String label) = switch (occ.status) {
      ExternalOccurrenceStatus.bound => (Icons.check_circle_outline, Colors.green.shade700, 'Bound to ${occ.table}'),
      ExternalOccurrenceStatus.mismatch => (
          Icons.warning_amber_outlined,
          Colors.orange.shade800,
          'Does not match ${occ.table.isEmpty ? ref.database : occ.table}'
        ),
      _ => (Icons.help_outline, Colors.grey.shade700, 'Not bound'),
    };
    final String? remedy;
    if (occ.status == ExternalOccurrenceStatus.mismatch) {
      remedy = 'Rename the field in ${ref.database}, or change what uses it in this solution, then Verify again; '
          'or choose another table of ${ref.database} for it, then Bind (it keeps its relationships).';
    } else if (occ.status != ExternalOccurrenceStatus.unbound) {
      remedy = null;
    } else if (!ref.isMapped) {
      remedy = 'Map "${ref.name}" to a solution with Edit…, then Bind.';
    } else if (occ.candidates.length > 1) {
      remedy = 'Several tables of ${ref.database} hold every field it uses. Choose its table, then Bind.';
    } else {
      remedy = 'No table of ${ref.database} holds every field it uses. Choose its table, then Bind; '
          'the fields that differ are listed after that.';
    }
    final choices = occ.status != ExternalOccurrenceStatus.mismatch && occ.candidates.isNotEmpty
        ? occ.candidates
        : (_peerTables[ref.id] ?? const <String>[]);
    return Container(
      key: ValueKey('file-reference-occurrence-${occ.name}'),
      margin: const EdgeInsets.only(top: 10),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        border: Border.all(color: Theme.of(context).dividerColor),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, size: 16, color: color),
              const SizedBox(width: 6),
              Expanded(
                child: Text(occ.name,
                    style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600, fontStyle: FontStyle.italic)),
              ),
              Text(label, style: TextStyle(fontSize: 11, color: color)),
            ],
          ),
          for (final p in occ.problems)
            Padding(
              padding: const EdgeInsets.only(top: 4, left: 22),
              child: Text('• $p', style: const TextStyle(fontSize: 11.5)),
            ),
          if (remedy != null)
            Padding(
              padding: const EdgeInsets.only(top: 4, left: 22),
              child: Text(remedy, style: TextStyle(fontSize: 11.5, color: Colors.grey.shade700)),
            ),
          if (occ.status != ExternalOccurrenceStatus.bound && ref.isMapped && choices.isNotEmpty && widget.canEdit)
            Padding(
              padding: const EdgeInsets.only(top: 6, left: 22),
              child: DropdownButton<String>(
                key: ValueKey('file-reference-table-${occ.name}'),
                isDense: true,
                hint: const Text('Choose a table', style: TextStyle(fontSize: 12)),
                value: choices.contains(_tableChoices[ref.id]?[occ.name]) ? _tableChoices[ref.id]![occ.name] : null,
                items: [
                  for (final t in choices) DropdownMenuItem(value: t, child: Text(t, style: const TextStyle(fontSize: 12))),
                ],
                onChanged: (t) => setState(() {
                  if (t != null) (_tableChoices[ref.id] ??= {})[occ.name] = t;
                }),
              ),
            ),
        ],
      ),
    );
  }
}

/// Adding or changing one file reference: its name, the solution it is
/// mapped to, and FileMaker's paths to the file.
class FileReferenceEditor extends StatefulWidget {
  const FileReferenceEditor({required this.apiClient, this.existing, super.key});

  final ApiClient apiClient;
  final FileReferenceModel? existing;

  /// The reference as saved, or null when cancelled.
  static Future<FileReferenceModel?> show(BuildContext context,
      {required ApiClient apiClient, FileReferenceModel? existing}) {
    return showDialog<FileReferenceModel>(
      context: context,
      builder: (_) => FileReferenceEditor(apiClient: apiClient, existing: existing),
    );
  }

  @override
  State<FileReferenceEditor> createState() => _FileReferenceEditorState();
}

class _FileReferenceEditorState extends State<FileReferenceEditor> {
  late final TextEditingController _name;
  late final TextEditingController _paths;
  String _database = '';
  List<String> _solutions = const [];
  bool _saving = false;
  String? _error;

  static const _unmapped = '';

  @override
  void initState() {
    super.initState();
    final existing = widget.existing;
    _name = TextEditingController(text: existing?.name ?? '');
    _paths = TextEditingController(text: existing?.paths.join('\n') ?? '');
    _database = existing?.database ?? _unmapped;
    unawaited(_loadSolutions());
  }

  @override
  void dispose() {
    _name.dispose();
    _paths.dispose();
    super.dispose();
  }

  Future<void> _loadSolutions() async {
    try {
      final res = await widget.apiClient.listDatabases();
      final dbs = [for (final d in res['databases'] as List<dynamic>? ?? const []) d.toString()];
      if (mounted) setState(() => _solutions = dbs);
    } catch (_) {
      // The solution can still be kept as it is; the list says none.
    }
  }

  List<String> get _pathList =>
      [for (final l in _paths.text.split('\n')) if (l.trim().isNotEmpty) l.trim()];

  /// The one installed solution a FileMaker path names, as migration
  /// suggests it; never chosen on its own (#107 never guesses).
  String? get _suggestion {
    final names = <String>{};
    for (final path in _pathList) {
      final file = path.split(RegExp(r'[:/\\]')).last.replaceAll(RegExp(r'\.fmp12$', caseSensitive: false), '');
      for (final s in _solutions) {
        if (s.toLowerCase() == file.toLowerCase()) names.add(s);
      }
    }
    return names.length == 1 && names.single != _database ? names.single : null;
  }

  Future<void> _save() async {
    final name = _name.text.trim();
    if (name.isEmpty) {
      setState(() => _error = 'Give the file reference a name: the one scripts and occurrences use.');
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final existing = widget.existing;
      final saved = existing == null
          ? await widget.apiClient.createFileReference(name: name, database: _database, paths: _pathList)
          : await widget.apiClient.updateFileReference(existing.id, name: name, database: _database, paths: _pathList);
      if (mounted) Navigator.of(context).pop(saved);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = describeFileReferenceError(e, FileReferenceModel(id: '', name: name, database: _database)).message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final options = {..._solutions, if (_database.isNotEmpty) _database}.toList()..sort();
    final suggestion = _suggestion;
    return AlertDialog(
      title: Text(widget.existing == null ? 'New File Reference' : 'Edit File Reference'),
      content: SizedBox(
        width: 440,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              TextField(
                key: const ValueKey('file-reference-name'),
                controller: _name,
                autofocus: true,
                decoration: const InputDecoration(labelText: 'Name', border: OutlineInputBorder(), isDense: true),
              ),
              const SizedBox(height: 14),
              DropdownButtonFormField<String>(
                key: const ValueKey('file-reference-solution'),
                initialValue: _database,
                decoration: const InputDecoration(labelText: 'Solution', border: OutlineInputBorder(), isDense: true),
                items: [
                  const DropdownMenuItem(value: _unmapped, child: Text('Not mapped yet')),
                  for (final s in options) DropdownMenuItem(value: s, child: Text(s)),
                ],
                onChanged: (v) => setState(() => _database = v ?? _unmapped),
              ),
              if (suggestion != null)
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton(
                    key: const ValueKey('file-reference-suggestion'),
                    onPressed: () => setState(() => _database = suggestion),
                    child: Text('Its path names the solution $suggestion. Use it'),
                  ),
                ),
              const SizedBox(height: 14),
              TextField(
                key: const ValueKey('file-reference-paths'),
                controller: _paths,
                minLines: 2,
                maxLines: 5,
                onChanged: (_) => setState(() {}),
                decoration: const InputDecoration(
                  labelText: 'FileMaker paths (optional)',
                  helperText: 'One per line, as FileMaker lists them. They only suggest the solution.',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
              ),
              if (_error != null) ...[
                const SizedBox(height: 12),
                Text(_error!, key: const ValueKey('file-reference-editor-error'),
                    style: const TextStyle(fontSize: 12, color: Colors.red)),
              ],
            ],
          ),
        ),
      ),
      actions: [
        TextButton(onPressed: _saving ? null : () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(onPressed: _saving ? null : _save, child: const Text('OK')),
      ],
    );
  }
}
