// The Import Records steps of a FileMaker migration that import from another
// FileMaker file (#191). FileMaker saved them with their source fields by
// position only and without the source table, so each one waits, disabled,
// for a transfer. This panel of the migration review asks which solution
// each file became, proposes the source table whose fields fit every
// position, and creates a transfer only for the steps the developer
// confirms: nothing is created or enabled on its own.

import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/external_access.dart';
import 'package:flutter/material.dart';

/// Signs in to another solution and answers a session token of it, or null
/// when the developer cancels.
typedef ImportStepsPeerToken = Future<String?> Function(BuildContext context, String database);

class MigrationImportStepsPanel extends StatefulWidget {
  const MigrationImportStepsPanel({required this.apiClient, required this.database, this.peerToken, super.key});

  final ApiClient apiClient;

  /// The solution the migration went into, which the steps belong to.
  final String database;

  /// How to sign in to another solution; the session's external access by
  /// default.
  final ImportStepsPeerToken? peerToken;

  @override
  State<MigrationImportStepsPanel> createState() => _MigrationImportStepsPanelState();
}

/// The steps that read one FileMaker file, and the solution chosen for it.
class _FileGroup {
  _FileGroup(this.file);

  /// The file name, empty for steps whose path is computed.
  final String file;
  final List<ImportStepModel> steps = [];
  String? solution;
  bool inferring = false;
  String? error;
  List<String> tables = const [];
}

class _MigrationImportStepsPanelState extends State<MigrationImportStepsPanel> {
  bool _loading = true;
  String? _loadError;
  List<_FileGroup> _groups = const [];
  List<String> _databases = const [];

  final Map<String, ImportStepInference> _inference = {};

  /// The table picked for each step, the inferred one until changed.
  final Map<String, String> _picked = {};
  final Map<String, String> _stepErrors = {};
  final Map<String, String> _peerTokens = {};
  final Set<String> _creating = {};
  int _created = 0;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    try {
      final steps = await widget.apiClient.listImportSteps();
      var databases = const <String>[];
      try {
        final listing = await widget.apiClient.listDatabases();
        databases = [for (final d in listing['databases'] as List<dynamic>? ?? const []) d.toString()];
      } on Object {
        // The solution of each file can still be the suggested one.
      }
      if (!mounted) return;
      final byFile = <String, _FileGroup>{};
      for (final s in steps) {
        final group = byFile.putIfAbsent(s.sourceFile.toLowerCase(), () => _FileGroup(s.sourceFile));
        group.steps.add(s);
        if (group.solution == null && s.suggestedDatabase.isNotEmpty) group.solution = s.suggestedDatabase;
      }
      final groups = byFile.values.toList()
        ..sort((a, b) {
          if (a.file.isEmpty != b.file.isEmpty) return a.file.isEmpty ? 1 : -1;
          return a.file.toLowerCase().compareTo(b.file.toLowerCase());
        });
      final known = {...databases};
      for (final g in groups) {
        if (g.solution != null) known.add(g.solution!);
      }
      known.add(widget.database);
      setState(() {
        _groups = groups;
        _databases = known.toList()..sort();
        _loading = false;
      });
      for (final g in groups) {
        if (g.solution != null) unawaited(_infer(g));
      }
    } on Object catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _loadError = e.toString().replaceFirst('ApiException ', '');
      });
    }
  }

  bool _isLocal(String database) => database.toLowerCase() == widget.database.toLowerCase();

  /// A session of [database], asked for once and kept for the panel.
  Future<String?> _tokenFor(String database) async {
    final kept = _peerTokens[database];
    if (kept != null) return kept;
    final ask = widget.peerToken ?? (ctx, db) => ExternalAccess.of(widget.apiClient).peerToken(ctx, db);
    final token = await ask(context, database);
    if (token != null && token.isNotEmpty) _peerTokens[database] = token;
    return token;
  }

  Future<void> _infer(_FileGroup group) async {
    final solution = group.solution;
    if (solution == null) return;
    setState(() {
      group.inferring = true;
      group.error = null;
    });
    String? token;
    if (!_isLocal(solution)) {
      token = await _tokenFor(solution);
      if (!mounted) return;
      if (token == null || token.isEmpty) {
        setState(() {
          group.inferring = false;
          group.error = 'Not signed in to $solution: its tables cannot be read.';
        });
        return;
      }
    }
    try {
      final result = await widget.apiClient.inferImportSteps(
        sourceDatabase: _isLocal(solution) ? '' : solution,
        stepIds: [for (final s in group.steps) s.stepId],
        peerToken: token,
      );
      if (!mounted || group.solution != solution) return;
      setState(() {
        group.inferring = false;
        group.tables = result.tables;
        for (final inf in result.steps) {
          _inference[inf.stepId] = inf;
          if (inf.isInferred) {
            _picked[inf.stepId] = inf.table;
          } else {
            _picked.remove(inf.stepId);
          }
          if (inf.error.isNotEmpty) _stepErrors[inf.stepId] = inf.error;
        }
      });
    } on Object catch (e) {
      if (!mounted) return;
      setState(() {
        group.inferring = false;
        group.error = e.toString().replaceFirst('ApiException ', '');
      });
    }
  }

  /// The steps a bulk confirmation covers: inferred, the inferred table
  /// still the one picked, and nothing in the way.
  List<(ImportStepModel, _FileGroup)> get _unambiguous => [
    for (final g in _groups)
      if (g.solution != null && !g.inferring)
        for (final s in g.steps)
          if (s.problem.isEmpty &&
              (_inference[s.stepId]?.isInferred ?? false) &&
              _picked[s.stepId] == _inference[s.stepId]!.table &&
              !_creating.contains(s.stepId))
            (s, g),
  ];

  Future<void> _create(List<(ImportStepModel, _FileGroup)> items) async {
    if (items.isEmpty) return;
    final choices = [
      for (final (s, g) in items)
        ImportStepChoice(
          stepId: s.stepId,
          sourceDatabase: _isLocal(g.solution!) ? '' : g.solution!,
          sourceTable: _picked[s.stepId]!,
        ),
    ];
    final tokens = <String, String>{
      for (final c in choices)
        if (c.sourceDatabase.isNotEmpty && _peerTokens[c.sourceDatabase] != null)
          c.sourceDatabase: _peerTokens[c.sourceDatabase]!,
    };
    setState(() {
      for (final c in choices) {
        _creating.add(c.stepId);
        _stepErrors.remove(c.stepId);
      }
    });
    try {
      final outcomes = await widget.apiClient.confirmImportSteps(choices, peerTokens: tokens);
      if (!mounted) return;
      setState(() {
        for (final o in outcomes) {
          _creating.remove(o.stepId);
          if (o.created) {
            _created++;
            for (final g in _groups) {
              g.steps.removeWhere((s) => s.stepId == o.stepId);
            }
          } else {
            _stepErrors[o.stepId] = o.error.isEmpty ? 'The transfer was not created.' : o.error;
          }
        }
        _creating.removeAll([for (final c in choices) c.stepId]);
        _groups = _groups.where((g) => g.steps.isNotEmpty).toList();
      });
    } on Object catch (e) {
      if (!mounted) return;
      setState(() {
        for (final c in choices) {
          _creating.remove(c.stepId);
          _stepErrors[c.stepId] = e.toString().replaceFirst('ApiException ', '');
        }
      });
    }
  }

  Future<void> _confirmUnambiguous() async {
    final items = _unambiguous;
    if (items.isEmpty) return;
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Create ${items.length} transfer${items.length == 1 ? '' : 's'}?'),
        content: SizedBox(
          width: 520,
          child: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  'Each step below gets a transfer from the table File4Base inferred, by position, and runs '
                  'it from now on. A step FileMaker had enabled is enabled again; a disabled one stays '
                  'disabled.',
                ),
                const SizedBox(height: 8),
                for (final (s, g) in items)
                  Text(
                    '• ${s.scriptName}, step ${s.stepIndex}: ${g.solution}.${_picked[s.stepId]} → ${s.targetTable} '
                    '(${_confidenceLabel(_inference[s.stepId]!.confidence)})',
                    style: const TextStyle(fontSize: 12),
                  ),
              ],
            ),
          ),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancel')),
          FilledButton(
            key: const Key('import-steps-confirm'),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Create transfers'),
          ),
        ],
      ),
    );
    if (ok == true && mounted) await _create(items);
  }

  static String _confidenceLabel(String c) => switch (c) {
    'high' => 'high confidence',
    'medium' => 'medium confidence',
    _ => 'picked',
  };

  @override
  Widget build(BuildContext context) {
    if (_loading) return const SizedBox.shrink();
    // A developer who may not create transfers is told so by the report;
    // the panel has nothing to offer them.
    if (_loadError != null || (_groups.isEmpty && _created == 0)) return const SizedBox.shrink();
    final waiting = _groups.fold<int>(0, (n, g) => n + g.steps.length);
    final unambiguous = _unambiguous.length;
    return Padding(padding: const EdgeInsets.only(top: 10), child: _buildPanel(waiting, unambiguous));
  }

  Widget _buildPanel(int waiting, int unambiguous) {
    return Material(
      key: const Key('import-steps-panel'),
      color: const Color(0xFF0F172A),
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(8),
        side: BorderSide(color: Colors.amberAccent.withValues(alpha: 0.6)),
      ),
      child: Padding(
        padding: const EdgeInsets.all(10),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.move_down_outlined, color: Colors.amberAccent, size: 18),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    waiting == 0
                        ? 'Import Records from other FileMaker files: every step has its transfer'
                        : 'Import Records from other FileMaker files ($waiting waiting)',
                    style: const TextStyle(color: Colors.amberAccent, fontSize: 12.5, fontWeight: FontWeight.w700),
                  ),
                ),
                if (unambiguous > 0)
                  FilledButton.tonalIcon(
                    key: const Key('import-steps-create-inferred'),
                    style: FilledButton.styleFrom(
                      visualDensity: VisualDensity.compact,
                      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                    ),
                    onPressed: _confirmUnambiguous,
                    icon: const Icon(Icons.done_all, size: 14),
                    label: Text('Create the $unambiguous inferred...', style: const TextStyle(fontSize: 11.5)),
                  ),
              ],
            ),
            const SizedBox(height: 4),
            const Text(
              'FileMaker saved these steps with their source fields by position only ("source field 3") and '
              'without the source table. Each one stays disabled until it has a transfer. Choose the solution '
              'each file became: File4Base proposes the table whose fields fit every position by type, and '
              'creates a transfer only for the steps you confirm.',
              style: TextStyle(color: Colors.white70, fontSize: 11.5),
            ),
            if (_created > 0)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  '$_created transfer${_created == 1 ? '' : 's'} created and named in their steps.',
                  style: const TextStyle(color: Colors.greenAccent, fontSize: 11.5),
                ),
              ),
            for (final g in _groups) _groupBlock(g),
          ],
        ),
      ),
    );
  }

  Widget _groupBlock(_FileGroup g) {
    final key = g.file.isEmpty ? '(computed)' : g.file;
    return Padding(
      padding: const EdgeInsets.only(top: 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  g.file.isEmpty ? 'A file whose path is computed when the script runs' : 'FileMaker file "${g.file}"',
                  style: const TextStyle(color: Colors.white, fontSize: 12.5, fontWeight: FontWeight.w600),
                ),
              ),
              const Text('became ', style: TextStyle(color: Colors.white54, fontSize: 12)),
              DropdownButton<String>(
                key: Key('import-steps-solution-$key'),
                value: g.solution,
                hint: const Text('choose a solution', style: TextStyle(fontSize: 12)),
                isDense: true,
                style: const TextStyle(fontSize: 12, color: Colors.white),
                dropdownColor: const Color(0xFF1E293B),
                items: [
                  for (final db in _databases)
                    DropdownMenuItem(value: db, child: Text(_isLocal(db) ? '$db (this solution)' : db)),
                ],
                onChanged: (db) {
                  if (db == null || db == g.solution) return;
                  setState(() {
                    g.solution = db;
                    g.tables = const [];
                    for (final s in g.steps) {
                      _inference.remove(s.stepId);
                      _picked.remove(s.stepId);
                      _stepErrors.remove(s.stepId);
                    }
                  });
                  unawaited(_infer(g));
                },
              ),
            ],
          ),
          if (g.inferring) const Padding(padding: EdgeInsets.all(6), child: LinearProgressIndicator(minHeight: 2)),
          if (g.error != null)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Row(
                children: [
                  Expanded(
                    child: Text(g.error!, style: const TextStyle(color: Colors.redAccent, fontSize: 11.5)),
                  ),
                  TextButton(onPressed: () => _infer(g), child: const Text('Retry')),
                ],
              ),
            ),
          for (final s in g.steps) _stepRow(s, g),
        ],
      ),
    );
  }

  Widget _stepRow(ImportStepModel s, _FileGroup g) {
    final inf = _inference[s.stepId];
    final picked = _picked[s.stepId];
    final error = _stepErrors[s.stepId];
    final creating = _creating.contains(s.stepId);
    final positions = s.mappings.map((m) => m.position).fold<int>(0, (a, b) => a > b ? a : b);
    final candidates = inf?.candidates ?? const <ImportSourceCandidate>[];
    final tables = [...candidates.map((c) => c.table), ...g.tables.where((t) => !candidates.any((c) => c.table == t))];
    String describeTable(String table) {
      final c = candidates.where((c) => c.table == table).firstOrNull;
      if (c == null) return table;
      return '$table — ${c.describe()}${c.fits ? '' : ' (does not fit)'}';
    }

    return Container(
      key: Key('import-step-${s.stepId}'),
      margin: const EdgeInsets.only(top: 6, left: 8),
      padding: const EdgeInsets.all(8),
      decoration: BoxDecoration(
        color: Colors.white.withValues(alpha: 0.03),
        borderRadius: BorderRadius.circular(6),
        border: Border.all(color: Colors.white10),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text.rich(
            TextSpan(
              children: [
                TextSpan(
                  text: '${s.scriptName}, step ${s.stepIndex}',
                  style: const TextStyle(color: Colors.white, fontWeight: FontWeight.w600),
                ),
                TextSpan(
                  text:
                      '  into ${s.targetTable}: ${s.describeMethod()}; ${s.mappings.length} field(s) from '
                      'source position(s) up to $positions'
                      '${s.autoEnterFields.isEmpty ? '' : ', auto-enter on ${s.autoEnterFields.contains('*') ? 'every field' : s.autoEnterFields.join(', ')}'}',
                  style: const TextStyle(color: Colors.white70),
                ),
              ],
            ),
            style: const TextStyle(fontSize: 12),
          ),
          if (s.problem.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(
                'Cannot be given a transfer: ${s.problem}. Edit the step in the script workspace.',
                style: const TextStyle(color: Colors.orangeAccent, fontSize: 11.5),
              ),
            )
          else if (g.solution != null && inf != null) ...[
            const SizedBox(height: 4),
            Row(
              children: [
                if (inf.isInferred) ...[
                  _badge(
                    inf.confidence == 'high' ? 'Inferred, high confidence' : 'Inferred, medium confidence',
                    inf.confidence == 'high' ? Colors.greenAccent : Colors.lightBlueAccent,
                  ),
                  const SizedBox(width: 6),
                ] else ...[
                  _badge('Not inferred', Colors.white54),
                  const SizedBox(width: 6),
                ],
                Expanded(
                  child: Text(inf.reason, style: const TextStyle(color: Colors.white60, fontSize: 11.5)),
                ),
              ],
            ),
            const SizedBox(height: 4),
            Row(
              children: [
                const Text('Source table ', style: TextStyle(color: Colors.white54, fontSize: 12)),
                Flexible(
                  child: DropdownButton<String>(
                    key: Key('import-step-table-${s.stepId}'),
                    value: tables.contains(picked) ? picked : null,
                    hint: const Text('pick a table', style: TextStyle(fontSize: 12)),
                    isDense: true,
                    isExpanded: true,
                    style: const TextStyle(fontSize: 12, color: Colors.white),
                    dropdownColor: const Color(0xFF1E293B),
                    items: [
                      for (final t in tables)
                        DropdownMenuItem(
                          value: t,
                          child: Text(describeTable(t), overflow: TextOverflow.ellipsis),
                        ),
                    ],
                    onChanged: creating ? null : (t) => setState(() => _picked[s.stepId] = t!),
                  ),
                ),
                const SizedBox(width: 8),
                OutlinedButton(
                  key: Key('import-step-create-${s.stepId}'),
                  style: OutlinedButton.styleFrom(
                    visualDensity: VisualDensity.compact,
                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                  ),
                  onPressed: picked == null || creating ? null : () => _create([(s, g)]),
                  child: Text(creating ? 'Creating...' : 'Create transfer', style: const TextStyle(fontSize: 11.5)),
                ),
              ],
            ),
            if (picked != null && inf.isInferred && picked != inf.table)
              const Text(
                'Picked by hand: the inferred table is overridden.',
                style: TextStyle(color: Colors.amberAccent, fontSize: 11),
              ),
          ],
          if (error != null)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(error, style: const TextStyle(color: Colors.redAccent, fontSize: 11.5)),
            ),
        ],
      ),
    );
  }

  Widget _badge(String text, Color color) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(4),
        border: Border.all(color: color.withValues(alpha: 0.4)),
      ),
      child: Text(
        text,
        style: TextStyle(color: color, fontSize: 11, fontWeight: FontWeight.w600),
      ),
    );
  }
}
