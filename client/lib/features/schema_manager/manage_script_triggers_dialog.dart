// #26 — File > Manage > Script Triggers: every trigger of the solution's
// layouts and layout objects, for review.
//
// A FileMaker migration imports every trigger switched off, so that no
// imported script runs on its own. This is where the developer reads them,
// grouped by layout, and enables the ones they trust — one by one, a
// selection at a time, or every trigger nothing keeps from running. A
// trigger that cannot run as File4Base stores it says why, and what to do.

import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter/material.dart';

/// What the developer asked for on closing the review: to open a trigger's
/// script, or its layout on the object it is set on.
class ScriptTriggerReviewResult {
  const ScriptTriggerReviewResult({this.changed = false, this.openScript, this.openLayout});

  /// A trigger was enabled or disabled, so open layouts must be read again.
  final bool changed;
  final ScriptTriggerModel? openScript;
  final ScriptTriggerModel? openLayout;
}

/// The status filter's choices.
enum ScriptTriggerStatusFilter { all, enabled, disabledByMigration, disabled, problem }

class ManageScriptTriggersDialog extends StatefulWidget {
  const ManageScriptTriggersDialog({
    required this.apiClient,
    this.solutionName = '',
    this.canEdit = true,
    this.initialLayoutId,
    super.key,
  });

  final ApiClient apiClient;

  /// The solution signed in to, named in the title.
  final String solutionName;

  /// Only an owner or an administrator may enable or disable a trigger.
  final bool canEdit;

  /// Shows only this layout's triggers at first (from Layout Setup).
  final String? initialLayoutId;

  static Future<ScriptTriggerReviewResult> show(
    BuildContext context, {
    required ApiClient apiClient,
    String solutionName = '',
    bool canEdit = true,
    String? initialLayoutId,
  }) async {
    final result = await showDialog<ScriptTriggerReviewResult>(
      context: context,
      builder: (_) => ManageScriptTriggersDialog(
        apiClient: apiClient,
        solutionName: solutionName,
        canEdit: canEdit,
        initialLayoutId: initialLayoutId,
      ),
    );
    return result ?? const ScriptTriggerReviewResult();
  }

  @override
  State<ManageScriptTriggersDialog> createState() => _ManageScriptTriggersDialogState();
}

class _ManageScriptTriggersDialogState extends State<ManageScriptTriggersDialog> {
  List<ScriptTriggerModel> _triggers = const [];
  bool _loading = true;
  bool _working = false;
  String? _error;
  String? _notice;
  bool _changed = false;

  final Set<String> _selected = {};
  String? _focusedId;

  final _search = TextEditingController();
  ScriptTriggerStatusFilter _status = ScriptTriggerStatusFilter.all;
  String _event = '';
  String _script = '';
  String _layout = '';

  @override
  void initState() {
    super.initState();
    _layout = widget.initialLayoutId ?? '';
    unawaited(_load());
  }

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final triggers = await widget.apiClient.listScriptTriggers();
      if (!mounted) return;
      setState(() {
        _triggers = triggers;
        _selected.removeWhere((id) => !triggers.any((t) => t.id == id));
        _loading = false;
      });
    } on Object catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = e is ApiException && e.statusCode == 403
            ? 'Only an owner or an administrator of this solution can review its script triggers.'
            : 'The script triggers of this solution could not be read: ${e is ApiException ? e.detail : e}';
      });
    }
  }

  void _close([ScriptTriggerReviewResult? result]) {
    Navigator.of(context).pop(result ??
        ScriptTriggerReviewResult(changed: _changed));
  }

  // ─── Filtering ───────────────────────────────────────────────────────────

  bool _matchesStatus(ScriptTriggerModel t) => switch (_status) {
        ScriptTriggerStatusFilter.all => true,
        ScriptTriggerStatusFilter.enabled => t.enabled,
        ScriptTriggerStatusFilter.disabledByMigration => !t.enabled && t.quarantinedByMigration,
        ScriptTriggerStatusFilter.disabled => !t.enabled,
        ScriptTriggerStatusFilter.problem => t.hasProblem,
      };

  List<ScriptTriggerModel> get _visible {
    final q = _search.text.trim().toLowerCase();
    return _triggers.where((t) {
      if (!_matchesStatus(t)) return false;
      if (_event.isNotEmpty && t.event != _event) return false;
      if (_script.isNotEmpty && (t.scriptName ?? '') != _script) return false;
      if (_layout.isNotEmpty && t.layoutId != _layout) return false;
      if (q.isEmpty) return true;
      return [t.layoutName, t.objectLabel, t.objectType, t.event, t.scriptName, t.parameter]
          .any((v) => v != null && v.toLowerCase().contains(q));
    }).toList();
  }

  // ─── Actions ─────────────────────────────────────────────────────────────

  Future<bool> _confirmEnable(int count) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        key: const ValueKey('trigger-enable-confirm'),
        title: Text('Enable $count Script Trigger${count == 1 ? '' : 's'}'),
        content: Text(
          'Enable $count trigger${count == 1 ? '' : 's'}? From now on, ${count == 1 ? 'its script runs' : 'their scripts run'} '
          'on user actions — when a layout is shown or left in Browse mode — for everyone using this solution.\n\n'
          'Review the scripts first: they were written for FileMaker. Triggers with a problem stay disabled.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          FilledButton(
            key: const ValueKey('trigger-enable-confirm-ok'),
            onPressed: () => Navigator.pop(ctx, true),
            child: Text('Enable $count'),
          ),
        ],
      ),
    );
    return confirmed == true;
  }

  Future<void> _enable(Iterable<ScriptTriggerModel> candidates) async {
    final ids = [for (final t in candidates) if (t.canEnable) t.id];
    final skipped = candidates.where((t) => !t.canEnable && !t.enabled).length;
    if (ids.isEmpty) {
      setState(() => _notice = 'None of these triggers can be enabled: each has a problem, shown below it.');
      return;
    }
    if (!await _confirmEnable(ids.length) || !mounted) return;
    await _apply(ids, enabled: true,
        done: '${ids.length} trigger${ids.length == 1 ? '' : 's'} enabled.'
            '${skipped > 0 ? ' $skipped with a problem stayed disabled.' : ''}');
  }

  Future<void> _disable(Iterable<ScriptTriggerModel> candidates) async {
    final ids = [for (final t in candidates) if (t.enabled) t.id];
    if (ids.isEmpty) {
      setState(() => _notice = 'These triggers are already disabled.');
      return;
    }
    await _apply(ids, enabled: false, done: '${ids.length} trigger${ids.length == 1 ? '' : 's'} disabled.');
  }

  Future<void> _apply(List<String> ids, {required bool enabled, required String done}) async {
    setState(() {
      _working = true;
      _notice = null;
    });
    try {
      await widget.apiClient.setScriptTriggersEnabled(ids, enabled: enabled);
      _changed = true;
      _selected.clear();
      if (mounted) setState(() => _notice = done);
    } on Object catch (e) {
      if (mounted) {
        setState(() => _notice = e is ApiException && e.statusCode == 409
            ? 'Nothing was changed: ${e.detail}'
            : 'Nothing was changed: ${e is ApiException ? e.detail : e}');
      }
    } finally {
      if (mounted) setState(() => _working = false);
    }
    await _load();
  }

  // ─── Building ────────────────────────────────────────────────────────────

  @override
  Widget build(BuildContext context) {
    final visible = _visible;
    final enableable = _triggers.where((t) => t.canEnable).length;
    final selected = _triggers.where((t) => _selected.contains(t.id)).toList();
    final focused = _triggers.where((t) => t.id == _focusedId).firstOrNull;
    return Dialog(
      child: SizedBox(
        width: 1100,
        height: 680,
        child: Column(
          children: [
            ListTile(
              leading: const Icon(Icons.bolt),
              title: const Text('Manage Script Triggers', style: TextStyle(fontWeight: FontWeight.bold)),
              subtitle: Text(
                'The triggers of the layouts of ${widget.solutionName.isEmpty ? 'this solution' : '"${widget.solutionName}"'}. '
                'Triggers imported from FileMaker arrive disabled: enable the ones you trust.',
                style: const TextStyle(fontSize: 12),
              ),
              trailing: IconButton(icon: const Icon(Icons.close), onPressed: _close),
            ),
            const Divider(height: 1),
            _buildSummary(),
            _buildFilters(),
            const Divider(height: 1),
            Expanded(child: _buildTable(visible)),
            if (focused != null) ...[
              const Divider(height: 1),
              _buildDetail(focused),
            ],
            if (_notice != null)
              Container(
                key: const ValueKey('trigger-notice'),
                width: double.infinity,
                color: Colors.amber.withValues(alpha: 0.15),
                padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                child: Text(_notice!, style: const TextStyle(fontSize: 12)),
              ),
            const Divider(height: 1),
            Padding(
              padding: const EdgeInsets.all(12),
              child: Row(
                children: [
                  Expanded(
                    child: Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: [
                        if (widget.canEdit) ...[
                          OutlinedButton(
                            key: const ValueKey('trigger-disable-selected'),
                            onPressed: _working || !selected.any((t) => t.enabled) ? null : () => _disable(selected),
                            child: const Text('Disable Selected'),
                          ),
                          OutlinedButton(
                            key: const ValueKey('trigger-enable-selected'),
                            onPressed: _working || selected.isEmpty ? null : () => _enable(selected),
                            child: const Text('Enable Selected'),
                          ),
                          FilledButton.icon(
                            key: const ValueKey('trigger-enable-all'),
                            icon: const Icon(Icons.done_all, size: 16),
                            label: Text('Enable All Without Problems ($enableable)'),
                            onPressed: _working || enableable == 0 ? null : () => _enable(_triggers),
                          ),
                        ],
                        if (selected.isNotEmpty)
                          Text('${selected.length} selected', style: const TextStyle(fontSize: 11, color: Colors.grey)),
                      ],
                    ),
                  ),
                  TextButton(onPressed: _close, child: const Text('Close')),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildSummary() {
    final enabled = _triggers.where((t) => t.enabled).length;
    final migrated = _triggers.where((t) => !t.enabled && t.quarantinedByMigration).length;
    final problems = _triggers.where((t) => t.hasProblem).length;
    Widget chip(String text, Color color) => Padding(
          padding: const EdgeInsets.only(right: 8),
          child: Chip(
            visualDensity: VisualDensity.compact,
            label: Text(text, style: TextStyle(fontSize: 11, color: color)),
          ),
        );
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
      child: Wrap(
        children: [
          chip('${_triggers.length} triggers', Colors.grey.shade700),
          chip('$enabled enabled', Colors.green.shade700),
          chip('$migrated disabled by migration', Colors.orange.shade800),
          chip('$problems with a problem', Colors.red.shade700),
        ],
      ),
    );
  }

  Widget _buildFilters() {
    final events = {for (final t in _triggers) t.event}.toList()..sort();
    final scripts = {for (final t in _triggers) if ((t.scriptName ?? '').isNotEmpty) t.scriptName!}.toList()
      ..sort((a, b) => a.toLowerCase().compareTo(b.toLowerCase()));
    final layouts = <String, String>{for (final t in _triggers) t.layoutId: t.layoutName};
    final layoutIds = layouts.keys.toList()..sort((a, b) => layouts[a]!.toLowerCase().compareTo(layouts[b]!.toLowerCase()));

    Widget dropdown<T>(String key, String label, T value, Map<T, String> items, ValueChanged<T> onChanged) => SizedBox(
          width: 190,
          child: DropdownButtonFormField<T>(
            key: ValueKey(key),
            initialValue: items.containsKey(value) ? value : items.keys.first,
            isExpanded: true,
            decoration: InputDecoration(labelText: label, isDense: true, border: const OutlineInputBorder()),
            items: [
              for (final e in items.entries)
                DropdownMenuItem(value: e.key, child: Text(e.value, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 12))),
            ],
            onChanged: (v) {
              if (v != null) setState(() => onChanged(v));
            },
          ),
        );

    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
      child: Wrap(
        spacing: 8,
        runSpacing: 8,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: [
          SizedBox(
            width: 220,
            child: TextField(
              key: const ValueKey('trigger-search'),
              controller: _search,
              decoration: const InputDecoration(
                isDense: true,
                prefixIcon: Icon(Icons.search, size: 18),
                hintText: 'Search layouts, objects, scripts…',
                border: OutlineInputBorder(),
              ),
              style: const TextStyle(fontSize: 12),
              onChanged: (_) => setState(() {}),
            ),
          ),
          dropdown<ScriptTriggerStatusFilter>('trigger-status-filter', 'Status', _status, const {
            ScriptTriggerStatusFilter.all: 'All statuses',
            ScriptTriggerStatusFilter.enabled: 'Enabled',
            ScriptTriggerStatusFilter.disabledByMigration: 'Disabled by migration',
            ScriptTriggerStatusFilter.disabled: 'Disabled',
            ScriptTriggerStatusFilter.problem: 'With a problem',
          }, (v) => _status = v),
          dropdown<String>('trigger-event-filter', 'Event', _event, {'': 'All events', for (final e in events) e: e}, (v) => _event = v),
          dropdown<String>('trigger-script-filter', 'Script', _script, {'': 'All scripts', for (final s in scripts) s: s}, (v) => _script = v),
          dropdown<String>('trigger-layout-filter', 'Layout', _layout, {'': 'All layouts', for (final id in layoutIds) id: layouts[id]!}, (v) => _layout = v),
        ],
      ),
    );
  }

  static const List<({String label, int flex})> _columns = [
    (label: 'Object', flex: 3),
    (label: 'Event', flex: 2),
    (label: 'Script', flex: 3),
    (label: 'Parameter', flex: 2),
    (label: 'Status', flex: 3),
  ];

  Widget _buildTable(List<ScriptTriggerModel> visible) {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(_error!, style: const TextStyle(fontSize: 12), textAlign: TextAlign.center),
              const SizedBox(height: 8),
              OutlinedButton(onPressed: _load, child: const Text('Retry')),
            ],
          ),
        ),
      );
    }
    if (_triggers.isEmpty) {
      return const Center(
        child: Text('This solution has no script triggers.', style: TextStyle(fontSize: 12, color: Colors.grey)),
      );
    }
    if (visible.isEmpty) {
      return const Center(
        child: Text('No trigger matches these filters.', style: TextStyle(fontSize: 12, color: Colors.grey)),
      );
    }

    // Grouped by layout, in layout name order, a header row per layout.
    final groups = <String, List<ScriptTriggerModel>>{};
    for (final t in visible) {
      groups.putIfAbsent(t.layoutId, () => []).add(t);
    }
    final order = groups.keys.toList()
      ..sort((a, b) => groups[a]!.first.layoutName.toLowerCase().compareTo(groups[b]!.first.layoutName.toLowerCase()));
    final rows = <Object>[];
    for (final id in order) {
      rows
        ..add(id)
        ..addAll(groups[id]!);
    }
    final allVisibleSelected = visible.every((t) => _selected.contains(t.id));

    return Column(
      children: [
        Container(
          color: Theme.of(context).colorScheme.surfaceContainerHighest.withValues(alpha: 0.5),
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
          child: Row(
            children: [
              SizedBox(
                width: 40,
                child: Checkbox(
                  key: const ValueKey('trigger-check-all'),
                  value: allVisibleSelected,
                  onChanged: (v) => setState(() {
                    for (final t in visible) {
                      v == true ? _selected.add(t.id) : _selected.remove(t.id);
                    }
                  }),
                ),
              ),
              for (final c in _columns)
                Expanded(flex: c.flex, child: Text(c.label, style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold))),
              const SizedBox(width: 88),
            ],
          ),
        ),
        Expanded(
          child: ListView.builder(
            itemCount: rows.length,
            itemBuilder: (_, i) {
              final row = rows[i];
              if (row is String) return _groupHeader(groups[row]!);
              return _triggerRow(row as ScriptTriggerModel);
            },
          ),
        ),
      ],
    );
  }

  Widget _groupHeader(List<ScriptTriggerModel> triggers) {
    final all = triggers.every((t) => _selected.contains(t.id));
    return Container(
      key: ValueKey('trigger-layout-${triggers.first.layoutId}'),
      color: Colors.grey.withValues(alpha: 0.08),
      padding: const EdgeInsets.symmetric(horizontal: 8),
      child: Row(
        children: [
          SizedBox(
            width: 40,
            child: Checkbox(
              value: all,
              onChanged: (v) => setState(() {
                for (final t in triggers) {
                  v == true ? _selected.add(t.id) : _selected.remove(t.id);
                }
              }),
            ),
          ),
          const Icon(Icons.dashboard_outlined, size: 14),
          const SizedBox(width: 6),
          Text(triggers.first.layoutName, style: const TextStyle(fontSize: 12, fontWeight: FontWeight.bold)),
          const SizedBox(width: 8),
          Text('${triggers.length} trigger${triggers.length == 1 ? '' : 's'}',
              style: const TextStyle(fontSize: 11, color: Colors.grey)),
        ],
      ),
    );
  }

  Color _statusColor(ScriptTriggerModel t) => switch (t.status) {
        ScriptTriggerStatus.enabled => Colors.green.shade700,
        ScriptTriggerStatus.disabledByMigration => Colors.orange.shade800,
        ScriptTriggerStatus.disabled => Colors.grey.shade700,
        ScriptTriggerStatus.problem => Colors.red.shade700,
      };

  Widget _triggerRow(ScriptTriggerModel t) {
    final object = t.isObjectTrigger
        ? '${t.objectLabel ?? 'Object'}${t.objectType != null && t.objectType != t.objectLabel ? ' (${t.objectType})' : ''}'
        : '(layout)';
    Widget cell(String text, int flex, {Color? color, String? tooltip}) => Expanded(
          flex: flex,
          child: Tooltip(
            message: tooltip ?? text,
            child: Text(text, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12, color: color)),
          ),
        );
    return InkWell(
      key: ValueKey('trigger-row-${t.id}'),
      onTap: () => setState(() => _focusedId = t.id),
      child: Container(
        color: t.id == _focusedId ? Theme.of(context).colorScheme.primary.withValues(alpha: 0.08) : null,
        padding: const EdgeInsets.symmetric(horizontal: 8),
        child: Row(
          children: [
            SizedBox(
              width: 40,
              child: Checkbox(
                key: ValueKey('trigger-check-${t.id}'),
                value: _selected.contains(t.id),
                onChanged: (v) => setState(() => v == true ? _selected.add(t.id) : _selected.remove(t.id)),
              ),
            ),
            cell(object, 3),
            cell(t.event, 2),
            cell((t.scriptName ?? '').isEmpty ? '(no script)' : t.scriptName!, 3,
                color: t.scriptId == null ? Colors.red.shade700 : null),
            cell(t.parameter ?? '', 2),
            cell(t.statusLabel, 3,
                color: _statusColor(t), tooltip: t.hasProblem ? t.problems.map((p) => p.message).join('\n') : t.statusLabel),
            SizedBox(
              width: 88,
              child: Row(
                children: [
                  IconButton(
                    key: ValueKey('trigger-open-script-${t.id}'),
                    tooltip: 'Open the script in Script Workspace',
                    iconSize: 16,
                    visualDensity: VisualDensity.compact,
                    icon: const Icon(Icons.code),
                    onPressed: t.scriptId == null
                        ? null
                        : () => _close(ScriptTriggerReviewResult(changed: _changed, openScript: t)),
                  ),
                  IconButton(
                    key: ValueKey('trigger-open-layout-${t.id}'),
                    tooltip: t.objectId != null ? 'Show the object in Layout mode' : 'Show the layout in Layout mode',
                    iconSize: 16,
                    visualDensity: VisualDensity.compact,
                    icon: const Icon(Icons.design_services_outlined),
                    onPressed: () => _close(ScriptTriggerReviewResult(changed: _changed, openLayout: t)),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildDetail(ScriptTriggerModel t) {
    return Container(
      key: const ValueKey('trigger-detail'),
      constraints: const BoxConstraints(maxHeight: 150),
      width: double.infinity,
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(16, 10, 16, 10),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '${t.layoutName}${t.isObjectTrigger ? ' › ${t.objectLabel ?? 'object'}' : ''} — ${t.event} runs '
              '${(t.scriptName ?? '').isEmpty ? 'no script' : '"${t.scriptName}"'}'
              '${(t.parameter ?? '').isEmpty ? '' : ' with the parameter ${t.parameter}'}',
              style: const TextStyle(fontSize: 12, fontWeight: FontWeight.bold),
            ),
            const SizedBox(height: 4),
            if (!t.hasProblem)
              Text(
                t.enabled
                    ? 'Enabled: its script runs in Browse mode ${t.event == 'OnLayoutExit' ? 'when the layout is left for another one' : 'when the layout is shown'}.'
                    : 'Nothing keeps this trigger from running once enabled.'
                        '${t.quarantinedByMigration ? ' It was imported from FileMaker and has not been reviewed yet.' : ''}',
                style: const TextStyle(fontSize: 12),
              ),
            for (final p in t.problems)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(Icons.error_outline, size: 14, color: Colors.red.shade700),
                    const SizedBox(width: 6),
                    Expanded(
                      child: Text.rich(
                        TextSpan(children: [
                          TextSpan(text: '${p.message} '),
                          const TextSpan(text: 'What to do: ', style: TextStyle(fontWeight: FontWeight.bold)),
                          TextSpan(text: p.remedy),
                        ]),
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }
}
