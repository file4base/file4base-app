import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter/material.dart';

/// Server Tools > Schedules (#188, #204): the server's backup and
/// maintenance schedules, as FileMaker Server's Schedules page lists them,
/// with their next and last runs, and the actions on each.
class ServerSchedulesPanel extends StatefulWidget {
  const ServerSchedulesPanel({required this.apiClient, super.key});

  final ApiClient apiClient;

  @override
  State<ServerSchedulesPanel> createState() => _ServerSchedulesPanelState();
}

class _ServerSchedulesPanelState extends State<ServerSchedulesPanel> {
  static const double _actionsWidth = 4 * 48;

  List<Map<String, dynamic>> _schedules = [];
  String _timeZone = '';
  bool _schedulerRunning = true;
  bool _loading = true;
  String? _error;
  Timer? _poll;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final res = await widget.apiClient.listServerSchedules();
      if (!mounted) return;
      final list = [
        for (final s in res['schedules'] as List<dynamic>? ?? const []) Map<String, dynamic>.from(s as Map),
      ];
      setState(() {
        _schedules = list;
        _timeZone = res['time_zone']?.toString() ?? '';
        _schedulerRunning = res['scheduler_running'] as bool? ?? true;
        _loading = false;
        _error = null;
      });
      // The list follows the runs: quickly while one is going or waiting,
      // slowly otherwise.
      final busy = list.any((s) => s['running'] == true || s['queued'] == true);
      _poll?.cancel();
      _poll = Timer(Duration(seconds: busy ? 3 : 30), () => unawaited(_load()));
    } on Exception catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = _message(e);
      });
    }
  }

  String _message(Object e) => e.toString().replaceFirst('Exception: ', '');

  void _toast(String text, {bool isError = false}) {
    if (!mounted) return;
    ScaffoldMessenger.maybeOf(context)?.showSnackBar(
      SnackBar(content: Text(text), backgroundColor: isError ? Colors.red.shade700 : null),
    );
  }

  Future<void> _act(Future<void> Function() action) async {
    try {
      await action();
    } on Exception catch (e) {
      _toast(_message(e), isError: true);
    }
    await _load();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final muted = theme.colorScheme.onSurfaceVariant;
    return Padding(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('Schedules', style: TextStyle(fontSize: 15, fontWeight: FontWeight.bold)),
                    const SizedBox(height: 2),
                    Text(
                      'Backups of the solutions, kept to a number of copies, and their maintenance. '
                      'Frequencies are read in the server time zone${_timeZone.isEmpty ? '' : ' ($_timeZone)'}, '
                      "run times are shown in this computer's; a run the server was down for is skipped, not run late.",
                      style: TextStyle(fontSize: 12, color: muted),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 12),
              FilledButton.icon(
                onPressed: () => _edit(null),
                icon: const Icon(Icons.add_alarm, size: 16),
                label: const Text('New Schedule'),
              ),
            ],
          ),
          if (!_schedulerRunning)
            Padding(
              padding: const EdgeInsets.only(top: 10),
              child: Text(
                'This server process does not run schedules (FILE4BASE_SERVER_SCHEDULER=false, or no persistent control store).',
                style: TextStyle(fontSize: 12, color: theme.colorScheme.error),
              ),
            ),
          const SizedBox(height: 12),
          Expanded(child: _buildList(theme, muted)),
        ],
      ),
    );
  }

  Widget _buildList(ThemeData theme, Color muted) {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error != null) return Center(child: Text(_error!, style: TextStyle(color: theme.colorScheme.error)));
    if (_schedules.isEmpty) {
      return Center(
        child: Text('No schedule yet. "New Schedule" backs up the solutions every night.', style: TextStyle(color: muted)),
      );
    }
    final header = TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: muted);
    return DecoratedBox(
      decoration: BoxDecoration(
        border: Border.all(color: theme.dividerColor),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            child: Row(
              children: [
                const SizedBox(width: 46),
                Expanded(flex: 3, child: Text('SCHEDULE', style: header)),
                Expanded(flex: 2, child: Text('FREQUENCY', style: header)),
                Expanded(flex: 2, child: Text('NEXT RUN', style: header)),
                Expanded(flex: 3, child: Text('LAST RUN', style: header)),
                const SizedBox(width: _actionsWidth),
              ],
            ),
          ),
          Divider(height: 1, color: theme.dividerColor),
          Expanded(
            child: ListView.separated(
              itemCount: _schedules.length,
              separatorBuilder: (_, _) => Divider(height: 1, color: theme.dividerColor),
              itemBuilder: (ctx, i) => _buildRow(_schedules[i], theme, muted),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildRow(Map<String, dynamic> s, ThemeData theme, Color muted) {
    final id = s['id']?.toString() ?? '';
    final name = s['name']?.toString() ?? '';
    final type = s['type']?.toString() ?? '';
    final enabled = s['enabled'] as bool? ?? false;
    final running = s['running'] == true;
    final queued = s['queued'] == true;
    final dbs = [for (final d in s['databases'] as List<dynamic>? ?? const []) d.toString()];
    final what = type == 'backup'
        ? 'Backup, keeps ${s['keep'] ?? 0} · ${dbs.isEmpty ? 'all solutions' : dbs.join(', ')}'
        : '${s['operation'] == 'analyze' ? 'Analyze' : 'Vacuum & analyze'} · ${dbs.isEmpty ? 'all solutions' : dbs.join(', ')}';
    final small = TextStyle(fontSize: 11, color: muted);
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      child: Row(
        children: [
          SizedBox(
            width: 46,
            child: Tooltip(
              message: enabled ? 'Enabled: click to disable' : 'Disabled: click to enable',
              child: Switch(
                value: enabled,
                onChanged: (v) => _act(() async {
                  await widget.apiClient.updateServerSchedule(id, {'enabled': v});
                }),
              ),
            ),
          ),
          Expanded(
            flex: 3,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(name, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13), overflow: TextOverflow.ellipsis),
                Text(what, style: small, overflow: TextOverflow.ellipsis),
              ],
            ),
          ),
          Expanded(flex: 2, child: Text(describeCron(s['cron']?.toString() ?? ''), style: const TextStyle(fontSize: 12))),
          Expanded(
            flex: 2,
            child: Text(
              running
                  ? 'Running…'
                  : queued
                  ? 'Waiting to run'
                  : enabled
                  ? formatRunTime(s['next_run_at']?.toString())
                  : 'Disabled',
              style: const TextStyle(fontSize: 12),
            ),
          ),
          Expanded(flex: 3, child: _lastRun(s['last_run'], theme, small)),
          SizedBox(
            width: _actionsWidth,
            child: Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                IconButton(
                  tooltip: 'Run now',
                  icon: const Icon(Icons.play_arrow, size: 18),
                  onPressed: running || queued
                      ? null
                      : () => _act(() async {
                          await widget.apiClient.runServerScheduleNow(id);
                          _toast('"$name" will run now.');
                        }),
                ),
                IconButton(tooltip: 'History', icon: const Icon(Icons.history, size: 18), onPressed: () => _history(s)),
                IconButton(tooltip: 'Edit', icon: const Icon(Icons.edit_outlined, size: 18), onPressed: () => _edit(s)),
                IconButton(
                  tooltip: 'Delete',
                  icon: Icon(Icons.delete_outline, size: 18, color: theme.colorScheme.error),
                  onPressed: () => _delete(id, name),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _lastRun(Object? raw, ThemeData theme, TextStyle small) {
    if (raw is! Map) return Text('Never run', style: small);
    final run = Map<String, dynamic>.from(raw);
    final status = run['status']?.toString() ?? '';
    final error = run['error']?.toString() ?? '';
    final color = switch (status) {
      'success' => Colors.green.shade700,
      'failed' => theme.colorScheme.error,
      'skipped' => Colors.orange.shade800,
      _ => small.color,
    };
    final facts = <String>[
      formatRunTime(run['started_at']?.toString()),
      if (status == 'success' || status == 'failed') formatDuration((run['duration_ms'] as num?)?.toInt() ?? 0),
      if (((run['size_bytes'] as num?)?.toInt() ?? 0) > 0) formatBytes((run['size_bytes'] as num).toInt()),
    ];
    return Tooltip(
      message: error,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(statusLabel(status), style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: color)),
          Text(error.isEmpty ? facts.join(' · ') : error, style: small, maxLines: 1, overflow: TextOverflow.ellipsis),
        ],
      ),
    );
  }

  Future<void> _delete(String id, String name) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete Schedule'),
        content: Text('Delete "$name" and its history? The backups it made are kept.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Delete')),
        ],
      ),
    );
    if (ok != true) return;
    await _act(() => widget.apiClient.deleteServerSchedule(id));
  }

  Future<void> _history(Map<String, dynamic> s) async {
    List<Map<String, dynamic>> runs;
    try {
      runs = await widget.apiClient.listServerScheduleRuns(s['id'].toString());
    } on Exception catch (e) {
      _toast(_message(e), isError: true);
      return;
    }
    if (!mounted) return;
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('History of ${s['name']}'),
        content: SizedBox(
          width: 560,
          height: 360,
          child: runs.isEmpty
              ? const Center(child: Text('No run yet.'))
              : ListView(
                  children: [
                    for (final r in runs)
                      ListTile(
                        dense: true,
                        title: Text(
                          '${statusLabel(r['status']?.toString() ?? '')} · ${formatRunTime(r['started_at']?.toString())}'
                          '${r['trigger'] == 'manual' ? ' (run now)' : ''}',
                        ),
                        subtitle: Text(
                          [
                            if ((r['error']?.toString() ?? '').isNotEmpty) r['error'].toString(),
                            if (((r['duration_ms'] as num?)?.toInt() ?? 0) > 0)
                              formatDuration((r['duration_ms'] as num).toInt()),
                            if (((r['size_bytes'] as num?)?.toInt() ?? 0) > 0) formatBytes((r['size_bytes'] as num).toInt()),
                          ].join(' · '),
                        ),
                      ),
                  ],
                ),
        ),
        actions: [TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Close'))],
      ),
    );
  }

  Future<void> _edit(Map<String, dynamic>? existing) async {
    var solutions = <String>[];
    try {
      final res = await widget.apiClient.listDatabases();
      solutions = [for (final d in res['databases'] as List<dynamic>? ?? const []) d.toString()];
    } on Exception {
      // The schedule can still back up all solutions.
    }
    if (!mounted) return;
    final saved = await showDialog<bool>(
      context: context,
      builder: (ctx) => _ScheduleDialog(apiClient: widget.apiClient, existing: existing, solutions: solutions),
    );
    if (saved == true) await _load();
  }
}

/// The frequencies the dialog offers; anything else is a cron expression.
enum ScheduleFrequency { hourly, daily, weekly, custom }

/// The parts of a cron expression the dialog edits.
class ScheduleTiming {
  ScheduleTiming(this.frequency, {this.minute = 0, this.hour = 2, Set<int>? days, this.cron = ''})
    : days = days ?? {1, 2, 3, 4, 5, 6, 0};

  /// Reads [expr] as one of the frequencies, or as custom.
  factory ScheduleTiming.parse(String expr) {
    final f = expr.trim().split(RegExp(r'\s+'));
    if (f.length == 5 && f[2] == '*' && f[3] == '*') {
      final minute = int.tryParse(f[0]);
      final hour = int.tryParse(f[1]);
      if (minute != null && f[1] == '*' && f[4] == '*') return ScheduleTiming(ScheduleFrequency.hourly, minute: minute);
      if (minute != null && hour != null && f[4] == '*') {
        return ScheduleTiming(ScheduleFrequency.daily, minute: minute, hour: hour);
      }
      final days = f[4].split(',').map(int.tryParse).toList();
      if (minute != null && hour != null && days.every((d) => d != null && d >= 0 && d <= 7)) {
        return ScheduleTiming(
          ScheduleFrequency.weekly,
          minute: minute,
          hour: hour,
          days: {for (final d in days) d! % 7},
        );
      }
    }
    return ScheduleTiming(ScheduleFrequency.custom, cron: expr);
  }

  ScheduleFrequency frequency;
  int minute;
  int hour;
  Set<int> days; // 0 = Sunday
  String cron;

  String toCron() => switch (frequency) {
    ScheduleFrequency.hourly => '$minute * * * *',
    ScheduleFrequency.daily => '$minute $hour * * *',
    ScheduleFrequency.weekly => '$minute $hour * * ${(days.toList()..sort()).join(',')}',
    ScheduleFrequency.custom => cron.trim(),
  };
}

const _dayNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

String _two(int n) => n.toString().padLeft(2, '0');

/// Says when a cron expression runs, in words where it can.
String describeCron(String expr) {
  final t = ScheduleTiming.parse(expr);
  return switch (t.frequency) {
    ScheduleFrequency.hourly => 'Every hour at :${_two(t.minute)}',
    ScheduleFrequency.daily => 'Daily at ${_two(t.hour)}:${_two(t.minute)}',
    ScheduleFrequency.weekly =>
      t.days.length == 7
          ? 'Daily at ${_two(t.hour)}:${_two(t.minute)}'
          : '${[for (final d in [1, 2, 3, 4, 5, 6, 0]) if (t.days.contains(d)) _dayNames[d]].join(', ')} '
                'at ${_two(t.hour)}:${_two(t.minute)}',
    ScheduleFrequency.custom => 'Cron: $expr',
  };
}

String statusLabel(String status) => switch (status) {
  'success' => 'Completed',
  'failed' => 'Failed',
  'skipped' => 'Skipped',
  'running' => 'Running',
  _ => status,
};

/// A run time, in the local time of this computer.
String formatRunTime(String? iso) {
  final t = iso == null ? null : DateTime.tryParse(iso)?.toLocal();
  if (t == null) return '—';
  return '${t.year}-${_two(t.month)}-${_two(t.day)} ${_two(t.hour)}:${_two(t.minute)}';
}

String formatDuration(int ms) {
  if (ms < 1000) return '$ms ms';
  final s = ms ~/ 1000;
  if (s < 60) return '$s s';
  return '${s ~/ 60} min ${_two(s % 60)} s';
}

String formatBytes(int bytes) {
  if (bytes < 1024) return '$bytes B';
  if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
  if (bytes < 1024 * 1024 * 1024) return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
}

class _ScheduleDialog extends StatefulWidget {
  const _ScheduleDialog({required this.apiClient, required this.existing, required this.solutions});

  final ApiClient apiClient;
  final Map<String, dynamic>? existing;
  final List<String> solutions;

  @override
  State<_ScheduleDialog> createState() => _ScheduleDialogState();
}

class _ScheduleDialogState extends State<_ScheduleDialog> {
  late final TextEditingController _name;
  late final TextEditingController _cron;
  late final TextEditingController _keep;
  late String _type;
  late String _operation;
  late bool _allSolutions;
  late Set<String> _selected;
  late ScheduleTiming _timing;
  late bool _enabled;
  String? _error;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    final e = widget.existing;
    _name = TextEditingController(text: e?['name']?.toString() ?? 'Nightly backup');
    _type = e?['type']?.toString() ?? 'backup';
    _operation = e?['operation']?.toString().isNotEmpty ?? false ? e!['operation'].toString() : 'vacuum_analyze';
    final dbs = [for (final d in e?['databases'] as List<dynamic>? ?? const []) d.toString()];
    _allSolutions = dbs.isEmpty;
    _selected = dbs.toSet();
    _timing = ScheduleTiming.parse(e?['cron']?.toString() ?? '0 2 * * *');
    _cron = TextEditingController(text: e?['cron']?.toString() ?? '0 2 * * *');
    _keep = TextEditingController(text: (e?['keep'] as num?)?.toString() ?? '7');
    _enabled = e?['enabled'] as bool? ?? true;
  }

  @override
  void dispose() {
    _name.dispose();
    _cron.dispose();
    _keep.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (_timing.frequency == ScheduleFrequency.custom) _timing.cron = _cron.text;
    final keep = int.tryParse(_keep.text.trim());
    if (_type == 'backup' && (keep == null || keep < 1 || keep > 1000)) {
      // A backup schedule deletes what it does not keep: never guess.
      setState(() => _error = 'Keep from 1 to 1000 backups of each solution.');
      return;
    }
    final body = <String, dynamic>{
      'name': _name.text.trim(),
      'type': _type,
      'databases': _allSolutions ? <String>[] : _selected.toList(),
      'cron': _timing.toCron(),
      'enabled': _enabled,
      if (_type == 'backup') 'keep': keep,
      if (_type == 'maintenance') 'operation': _operation,
    };
    if (!_allSolutions && _selected.isEmpty) {
      setState(() => _error = 'Choose the solutions, or all of them.');
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final id = widget.existing?['id']?.toString();
      if (id == null) {
        await widget.apiClient.createServerSchedule(body);
      } else {
        await widget.apiClient.updateServerSchedule(id, body);
      }
      if (mounted) Navigator.pop(context, true);
    } on Exception catch (e) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    const gap = SizedBox(height: 12);
    final known = {...widget.solutions, ..._selected}.toList()..sort();
    return AlertDialog(
      title: Text(widget.existing == null ? 'New Schedule' : 'Edit Schedule'),
      content: SizedBox(
        width: 460,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              TextField(
                key: const ValueKey('schedule-name'),
                controller: _name,
                decoration: const InputDecoration(labelText: 'Name', border: OutlineInputBorder(), isDense: true),
              ),
              gap,
              SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: 'backup', label: Text('Backup'), icon: Icon(Icons.backup_outlined)),
                  ButtonSegment(value: 'maintenance', label: Text('Maintenance'), icon: Icon(Icons.build_outlined)),
                ],
                selected: {_type},
                onSelectionChanged: (v) => setState(() => _type = v.first),
              ),
              gap,
              CheckboxListTile(
                dense: true,
                contentPadding: EdgeInsets.zero,
                title: const Text('All solutions, including those created later'),
                value: _allSolutions,
                onChanged: (v) => setState(() => _allSolutions = v ?? true),
              ),
              if (!_allSolutions)
                Wrap(
                  spacing: 6,
                  runSpacing: 6,
                  children: [
                    for (final db in known)
                      FilterChip(
                        label: Text(db),
                        selected: _selected.contains(db),
                        onSelected: (on) => setState(() => on ? _selected.add(db) : _selected.remove(db)),
                      ),
                  ],
                ),
              gap,
              Wrap(
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  DropdownButton<ScheduleFrequency>(
                    value: _timing.frequency,
                    items: const [
                      DropdownMenuItem(value: ScheduleFrequency.hourly, child: Text('Every hour')),
                      DropdownMenuItem(value: ScheduleFrequency.daily, child: Text('Daily')),
                      DropdownMenuItem(value: ScheduleFrequency.weekly, child: Text('Weekly')),
                      DropdownMenuItem(value: ScheduleFrequency.custom, child: Text('Cron expression')),
                    ],
                    onChanged: (v) => setState(() {
                      if (v == ScheduleFrequency.custom) _cron.text = _timing.toCron();
                      _timing.frequency = v ?? ScheduleFrequency.daily;
                    }),
                  ),
                  const SizedBox(width: 12),
                  if (_timing.frequency == ScheduleFrequency.hourly)
                    _number('at minute', _timing.minute, 59, (v) => _timing.minute = v)
                  else if (_timing.frequency != ScheduleFrequency.custom) ...[
                    _number('at', _timing.hour, 23, (v) => _timing.hour = v),
                    const Text(' : '),
                    _number('', _timing.minute, 59, (v) => _timing.minute = v),
                  ],
                ],
              ),
              if (_timing.frequency == ScheduleFrequency.weekly)
                Wrap(
                  spacing: 4,
                  children: [
                    for (final d in [1, 2, 3, 4, 5, 6, 0])
                      FilterChip(
                        label: Text(_dayNames[d]),
                        selected: _timing.days.contains(d),
                        onSelected: (on) => setState(() => on ? _timing.days.add(d) : _timing.days.remove(d)),
                      ),
                  ],
                ),
              if (_timing.frequency == ScheduleFrequency.custom)
                TextField(
                  controller: _cron,
                  decoration: const InputDecoration(
                    labelText: 'Minute, hour, day of month, month, day of week',
                    hintText: '0 2 * * *',
                    border: OutlineInputBorder(),
                    isDense: true,
                  ),
                ),
              gap,
              if (_type == 'backup')
                TextField(
                  controller: _keep,
                  keyboardType: TextInputType.number,
                  decoration: const InputDecoration(
                    labelText: 'Backups to keep of each solution',
                    helperText: 'Older ones this schedule made are deleted; manual backups never are.',
                    border: OutlineInputBorder(),
                    isDense: true,
                  ),
                )
              else
                DropdownButtonFormField<String>(
                  initialValue: _operation,
                  decoration: const InputDecoration(labelText: 'Operation', border: OutlineInputBorder(), isDense: true),
                  items: const [
                    DropdownMenuItem(value: 'vacuum_analyze', child: Text('Vacuum & analyze (ANALYZE TABLE on MariaDB)')),
                    DropdownMenuItem(value: 'analyze', child: Text('Analyze')),
                  ],
                  onChanged: (v) => setState(() => _operation = v ?? 'vacuum_analyze'),
                ),
              SwitchListTile(
                dense: true,
                contentPadding: EdgeInsets.zero,
                title: const Text('Enabled'),
                value: _enabled,
                onChanged: (v) => setState(() => _enabled = v),
              ),
              if (_error != null) Text(_error!, style: TextStyle(color: theme.colorScheme.error)),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(onPressed: _saving ? null : () => Navigator.pop(context, false), child: const Text('Cancel')),
        FilledButton(onPressed: _saving ? null : _save, child: const Text('Save')),
      ],
    );
  }

  Widget _number(String label, int value, int max, void Function(int) set) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (label.isNotEmpty) Text('$label '),
        DropdownButton<int>(
          value: value,
          items: [for (var i = 0; i <= max; i++) DropdownMenuItem(value: i, child: Text(_two(i)))],
          onChanged: (v) => setState(() => set(v ?? 0)),
        ),
      ],
    );
  }
}
