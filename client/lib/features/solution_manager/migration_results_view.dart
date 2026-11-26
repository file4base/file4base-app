// The end of a FileMaker migration (#198): what it created, what to review,
// and the accounts that cannot sign in until a password is set.
//
// The conversion report's manual actions, unsupported and rejected items and
// the import's own warnings are listed together, grouped by the kind of object
// they are about, so that a developer reads "Scripts: 4 to review" rather than
// one long list. Each group, and the whole list, can be copied as text. The
// script triggers a migration switches off are reviewed in File > Manage >
// Script Triggers, which this view opens rather than repeats.

import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/security/set_password_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// How serious an item to review is.
enum MigrationReviewSeverity {
  rejected('Rejected', Colors.redAccent),
  manualAction('Manual action', Colors.amberAccent),
  unsupported('Unsupported', Colors.orangeAccent),
  notWorking('Not working yet', Colors.yellowAccent);

  const MigrationReviewSeverity(this.label, this.color);
  final String label;
  final Color color;
}

/// One thing the migration asks the developer to look at.
class MigrationReviewItem {
  const MigrationReviewItem({
    required this.severity,
    required this.kind,
    required this.name,
    required this.detail,
  });

  final MigrationReviewSeverity severity;

  /// The report's domain: `script`, `field`, `trigger`...
  final String kind;
  final String name;
  final String detail;

  String toText() => '[${severity.label}] ${name.isEmpty ? '' : '$name: '}$detail';
}

/// The items of one kind.
class MigrationReviewGroup {
  const MigrationReviewGroup(this.kind, this.items);
  final String kind;
  final List<MigrationReviewItem> items;

  String get label => migrationKindLabel(kind);

  int count(MigrationReviewSeverity s) => items.where((i) => i.severity == s).length;

  String toText() => [
        '$label (${items.length})',
        for (final item in items) '  ${item.toText()}',
      ].join('\n');
}

const _kindOrder = [
  'trigger', 'script', 'layout', 'field', 'table', 'occurrence', 'relationship',
  'value_list', 'custom_function', 'account', 'data', 'container', 'file_reference',
  'data_source', 'external', 'theme', 'file', 'design', 'solution',
];

const _kindLabels = {
  'trigger': 'Script triggers',
  'script': 'Scripts',
  'layout': 'Layouts',
  'field': 'Fields',
  'table': 'Tables',
  'occurrence': 'Table occurrences',
  'relationship': 'Relationships',
  'value_list': 'Value lists',
  'custom_function': 'Custom functions',
  'account': 'Accounts',
  'data': 'Records',
  'container': 'Container data',
  'file_reference': 'File references',
  'data_source': 'Data sources',
  'external': 'External files',
  'theme': 'Themes',
  'file': 'File',
  'design': 'Design',
  'solution': 'Solution',
};

/// The heading of a group of [kind].
String migrationKindLabel(String kind) {
  final known = _kindLabels[kind];
  if (known != null) return known;
  final words = kind.trim().replaceAll('_', ' ');
  if (words.isEmpty) return 'Other';
  return words[0].toUpperCase() + words.substring(1);
}

/// What a migration asks to review, grouped by kind: rejected, manual-action
/// and unsupported items of the conversion report, and the import's warnings.
List<MigrationReviewGroup> groupMigrationReviewItems(MigrationApplyResultModel result) {
  String nameOf(MigrationReportItemModel it) =>
      it.targetName.isNotEmpty && it.targetName != it.sourceName ? '${it.sourceName} → ${it.targetName}' : it.sourceName;
  final items = <MigrationReviewItem>[
    for (final it in result.report.rejected)
      MigrationReviewItem(severity: MigrationReviewSeverity.rejected, kind: it.domain, name: nameOf(it), detail: it.detail),
    for (final it in result.report.manualActionRequired)
      MigrationReviewItem(severity: MigrationReviewSeverity.manualAction, kind: it.domain, name: nameOf(it), detail: it.detail),
    for (final it in result.report.unsupported)
      MigrationReviewItem(severity: MigrationReviewSeverity.unsupported, kind: it.domain, name: nameOf(it), detail: it.detail),
    // The import names its warnings in prose; the only kind it has today is
    // a value list restored as saved that offers no values.
    for (final w in result.importReport.warnings)
      MigrationReviewItem(
        severity: MigrationReviewSeverity.notWorking,
        kind: w.toLowerCase().startsWith('value list') ? 'value_list' : 'solution',
        name: '',
        detail: w,
      ),
  ];
  final byKind = <String, List<MigrationReviewItem>>{};
  for (final item in items) {
    byKind.putIfAbsent(item.kind.trim().isEmpty ? 'other' : item.kind.trim(), () => []).add(item);
  }
  int rank(String k) {
    final i = _kindOrder.indexOf(k);
    return i < 0 ? _kindOrder.length : i;
  }

  final kinds = byKind.keys.toList()
    ..sort((a, b) {
      final r = rank(a).compareTo(rank(b));
      return r != 0 ? r : a.compareTo(b);
    });
  return [for (final k in kinds) MigrationReviewGroup(k, byKind[k]!)];
}

class MigrationResultsView extends StatefulWidget {
  const MigrationResultsView({
    required this.result,
    required this.apiClient,
    this.onReviewTriggers,
    this.fullReport,
    this.notice,
    super.key,
  });

  final MigrationApplyResultModel result;
  final ApiClient apiClient;

  /// Opens File > Manage > Script Triggers. Null hides the button.
  final Future<void> Function()? onReviewTriggers;

  /// The complete conversion report, shown folded at the end.
  final Widget? fullReport;

  /// A line from the caller, such as what to do after the trigger review.
  final String? notice;

  @override
  State<MigrationResultsView> createState() => _MigrationResultsViewState();
}

class _MigrationResultsViewState extends State<MigrationResultsView> {
  late final List<MigrationReviewGroup> _groups = groupMigrationReviewItems(widget.result);

  /// The accounts named by the import, as the server lists them now.
  Map<String, UserModel> _accounts = const {};
  bool _loadingAccounts = false;
  String? _accountsError;

  /// Which copy button was just used ('all' or a kind), for its feedback.
  String? _copied;
  Timer? _copiedTimer;

  List<String> get _pendingNames => widget.result.importReport.accountsPendingPassword;

  @override
  void initState() {
    super.initState();
    if (_pendingNames.isNotEmpty) unawaited(_loadAccounts());
  }

  @override
  void dispose() {
    _copiedTimer?.cancel();
    super.dispose();
  }

  Future<void> _loadAccounts() async {
    setState(() {
      _loadingAccounts = true;
      _accountsError = null;
    });
    try {
      final users = await widget.apiClient.listUsers();
      if (!mounted) return;
      setState(() {
        _accounts = {for (final u in users) u.username.toLowerCase(): u};
        _loadingAccounts = false;
      });
    } on Object catch (e) {
      if (!mounted) return;
      setState(() {
        _accountsError = e.toString().replaceFirst('ApiException ', '');
        _loadingAccounts = false;
      });
    }
  }

  Future<void> _setPassword(UserModel user) async {
    final updated = await SetPasswordDialog.show(context, apiClient: widget.apiClient, user: user);
    if (updated == null || !mounted) return;
    setState(() => _accounts = {..._accounts, updated.username.toLowerCase(): updated});
  }

  Future<void> _copy(String key, String text) async {
    await Clipboard.setData(ClipboardData(text: text));
    if (!mounted) return;
    _copiedTimer?.cancel();
    setState(() => _copied = key);
    _copiedTimer = Timer(const Duration(seconds: 2), () {
      if (mounted) setState(() => _copied = null);
    });
  }

  String _allAsText() {
    final r = widget.result;
    final solution = r.report.solutionName.isEmpty ? '' : ' of "${r.report.solutionName}"';
    return [
      'FileMaker migration$solution into database "${r.database}": items to review',
      '',
      for (final g in _groups) ...[g.toText(), ''],
    ].join('\n').trimRight();
  }

  int get _reviewCount => _groups.fold(0, (n, g) => n + g.items.length);

  bool get _hasTriggers => _groups.any((g) => g.kind == 'trigger');

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _buildSummary(),
        if (widget.notice != null) ...[
          const SizedBox(height: 8),
          _panel(
            borderColor: const Color(0xFF38BDF8),
            child: Text(widget.notice!, style: const TextStyle(color: Color(0xFFE2E8F0), fontSize: 12)),
          ),
        ],
        if (_pendingNames.isNotEmpty) ...[
          const SizedBox(height: 10),
          _buildPendingAccounts(),
        ],
        const SizedBox(height: 10),
        _buildReview(),
        if (widget.fullReport != null) ...[
          const SizedBox(height: 10),
          _panel(
            padding: EdgeInsets.zero,
            child: Theme(
              data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
              child: ExpansionTile(
                key: const Key('migration-full-report'),
                dense: true,
                iconColor: Colors.white70,
                collapsedIconColor: Colors.white54,
                title: const Text('Full conversion report',
                    style: TextStyle(color: Colors.white70, fontSize: 12.5, fontWeight: FontWeight.w600)),
                childrenPadding: const EdgeInsets.fromLTRB(8, 0, 8, 8),
                children: [widget.fullReport!],
              ),
            ),
          ),
        ],
      ],
    );
  }

  Widget _buildSummary() {
    final r = widget.result;
    final c = r.importReport.counts;
    int both(String k) => (c['${k}_created'] ?? 0) + (c['${k}_updated'] ?? 0);
    final badges = <(String, int)>[
      ('Tables', c['tables_created'] ?? r.report.counts['tables'] ?? 0),
      ('Fields', c['columns_created'] ?? r.report.counts['fields'] ?? 0),
      ('Relationships', both('relationships')),
      ('Layouts', both('layouts')),
      ('Scripts', both('scripts')),
      ('Value lists', both('value_lists')),
      ('Custom functions', c['custom_functions_created'] ?? 0),
      ('Accounts', c['accounts_created'] ?? 0),
      ('Records', r.recordsInserted),
    ];
    return _panel(
      borderColor: Colors.greenAccent.withValues(alpha: 0.6),
      color: Colors.green.withValues(alpha: 0.10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              const Icon(Icons.check_circle_outline, color: Colors.greenAccent, size: 18),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  'Migration committed to database "${r.database}"'
                  '${r.report.solutionName.isEmpty ? '' : ' as "${r.report.solutionName}"'}.',
                  style: const TextStyle(color: Colors.greenAccent, fontSize: 12.5, fontWeight: FontWeight.w600),
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: [
              for (final (label, n) in badges)
                if (n > 0 || label == 'Tables' || label == 'Records') _badge('$label: $n', Colors.white70),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildPendingAccounts() {
    final names = _pendingNames;
    final waiting = names.where((n) => _accounts[n.toLowerCase()]?.passwordPending ?? true).length;
    return _panel(
      key: const Key('migration-pending-accounts'),
      borderColor: Colors.amberAccent.withValues(alpha: 0.6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              const Icon(Icons.key_off_outlined, color: Colors.amberAccent, size: 18),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  waiting == 0
                      ? 'Accounts migrated without a password (${names.length}): all have one now'
                      : 'Accounts waiting for a password ($waiting of ${names.length})',
                  style: const TextStyle(color: Colors.amberAccent, fontSize: 12.5, fontWeight: FontWeight.w700),
                ),
              ),
            ],
          ),
          const SizedBox(height: 4),
          const Text(
            'FileMaker does not export passwords, so these accounts were created inactive and without one: '
            'they cannot sign in until an administrator sets a password. This can also be done later in '
            'File > Manage > Security, where they are marked "No password".',
            style: TextStyle(color: Colors.white70, fontSize: 11.5),
          ),
          const SizedBox(height: 6),
          if (_loadingAccounts)
            const Padding(
              padding: EdgeInsets.all(8),
              child: Center(child: SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2))),
            )
          else if (_accountsError != null)
            Row(
              children: [
                Expanded(
                  child: Text('The accounts could not be read: $_accountsError',
                      style: const TextStyle(color: Colors.redAccent, fontSize: 11.5)),
                ),
                TextButton(onPressed: _loadAccounts, child: const Text('Retry')),
              ],
            )
          else
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 220),
              child: ListView.separated(
                shrinkWrap: true,
                itemCount: names.length,
                separatorBuilder: (_, _) => const Divider(color: Colors.white10, height: 1),
                itemBuilder: (_, i) => _accountRow(names[i]),
              ),
            ),
        ],
      ),
    );
  }

  Widget _accountRow(String name) {
    final user = _accounts[name.toLowerCase()];
    final (String status, Color color) = switch (user) {
      null => ('Not found', Colors.white38),
      UserModel(passwordPending: true) => ('Cannot sign in', Colors.amberAccent),
      UserModel(isActive: false) => ('Password set, inactive', Colors.white54),
      _ => ('Can sign in', Colors.greenAccent),
    };
    return Padding(
      key: Key('pending-account-$name'),
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        children: [
          Expanded(
            child: Text.rich(
              TextSpan(
                children: [
                  TextSpan(text: name, style: const TextStyle(color: Colors.white, fontWeight: FontWeight.w600)),
                  if (user != null) TextSpan(text: '  ${user.role}', style: const TextStyle(color: Colors.white54)),
                ],
              ),
              style: const TextStyle(fontSize: 12.5),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          _badge(status, color),
          const SizedBox(width: 8),
          if (user != null)
            OutlinedButton.icon(
              key: Key('set-password-$name'),
              style: OutlinedButton.styleFrom(
                visualDensity: VisualDensity.compact,
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
              ),
              onPressed: () => _setPassword(user),
              icon: const Icon(Icons.key_outlined, size: 14),
              label: Text(user.passwordPending ? 'Set password...' : 'Change...', style: const TextStyle(fontSize: 11.5)),
            ),
        ],
      ),
    );
  }

  Widget _buildReview() {
    final count = _reviewCount;
    return _panel(
      key: const Key('migration-review'),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Icon(count == 0 ? Icons.task_alt : Icons.rule_rounded,
                  color: count == 0 ? Colors.greenAccent : Colors.amberAccent, size: 18),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  count == 0 ? 'Nothing to review' : 'To review ($count)',
                  style: TextStyle(
                    color: count == 0 ? Colors.greenAccent : Colors.amberAccent,
                    fontSize: 12.5,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
              if (widget.onReviewTriggers != null && _hasTriggers)
                Padding(
                  padding: const EdgeInsets.only(left: 6),
                  child: OutlinedButton.icon(
                    key: const Key('migration-review-triggers'),
                    style: OutlinedButton.styleFrom(
                      visualDensity: VisualDensity.compact,
                      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                    ),
                    onPressed: widget.onReviewTriggers,
                    icon: const Icon(Icons.bolt, size: 14),
                    label: const Text('Review script triggers...', style: TextStyle(fontSize: 11.5)),
                  ),
                ),
              if (count > 0)
                Padding(
                  padding: const EdgeInsets.only(left: 6),
                  child: TextButton.icon(
                    key: const Key('migration-copy-all'),
                    style: TextButton.styleFrom(visualDensity: VisualDensity.compact),
                    onPressed: () => _copy('all', _allAsText()),
                    icon: Icon(_copied == 'all' ? Icons.check : Icons.copy_all_outlined, size: 14),
                    label: Text(_copied == 'all' ? 'Copied' : 'Copy all', style: const TextStyle(fontSize: 11.5)),
                  ),
                ),
            ],
          ),
          if (count == 0)
            const Padding(
              padding: EdgeInsets.only(top: 4),
              child: Text('Every object was migrated without a manual action, an unsupported part or a warning.',
                  style: TextStyle(color: Colors.white70, fontSize: 11.5)),
            )
          else
            Theme(
              data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
              child: Column(
                children: [for (final g in _groups) _groupTile(g)],
              ),
            ),
        ],
      ),
    );
  }

  Widget _groupTile(MigrationReviewGroup g) {
    final summary = [
      for (final s in MigrationReviewSeverity.values)
        if (g.count(s) > 0) '${g.count(s)} ${s.label.toLowerCase()}',
    ].join(' · ');
    final copied = _copied == g.kind;
    return ExpansionTile(
      key: Key('migration-group-${g.kind}'),
      dense: true,
      tilePadding: const EdgeInsets.symmetric(horizontal: 4),
      childrenPadding: const EdgeInsets.only(left: 8, right: 4, bottom: 6),
      initiallyExpanded: _groups.length == 1,
      expandedCrossAxisAlignment: CrossAxisAlignment.stretch,
      iconColor: Colors.white70,
      collapsedIconColor: Colors.white54,
      title: Text('${g.label} (${g.items.length})',
          style: const TextStyle(color: Colors.white, fontSize: 12.5, fontWeight: FontWeight.w600)),
      subtitle: Text(summary, style: const TextStyle(color: Colors.white54, fontSize: 11)),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          IconButton(
            key: Key('migration-copy-${g.kind}'),
            visualDensity: VisualDensity.compact,
            iconSize: 16,
            color: copied ? Colors.greenAccent : Colors.white54,
            tooltip: copied ? 'Copied' : 'Copy ${g.label.toLowerCase()} as text',
            icon: Icon(copied ? Icons.check : Icons.copy_outlined),
            onPressed: () => _copy(g.kind, g.toText()),
          ),
          const Icon(Icons.expand_more, color: Colors.white54, size: 18),
        ],
      ),
      children: [
        if (g.kind == 'trigger')
          const Padding(
            padding: EdgeInsets.only(bottom: 6),
            child: Text(
              'Imported script triggers are off so that no FileMaker script runs on its own. '
              'Enable the ones you trust in File > Manage > Script Triggers.',
              style: TextStyle(color: Colors.white60, fontSize: 11.5),
            ),
          ),
        ConstrainedBox(
          constraints: const BoxConstraints(maxHeight: 240),
          child: ListView.separated(
            shrinkWrap: true,
            itemCount: g.items.length,
            separatorBuilder: (_, _) => const Divider(color: Colors.white10, height: 1),
            itemBuilder: (_, i) => _itemRow(g.items[i]),
          ),
        ),
      ],
    );
  }

  Widget _itemRow(MigrationReviewItem it) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(width: 104, child: Align(alignment: Alignment.topLeft, child: _badge(it.severity.label, it.severity.color))),
          const SizedBox(width: 8),
          Expanded(
            child: SelectableText.rich(
              TextSpan(
                children: [
                  if (it.name.isNotEmpty)
                    TextSpan(
                      text: '${it.name}\n',
                      style: const TextStyle(color: Colors.white, fontWeight: FontWeight.w600),
                    ),
                  TextSpan(text: it.detail, style: const TextStyle(color: Colors.white70)),
                ],
              ),
              style: const TextStyle(fontSize: 12),
            ),
          ),
        ],
      ),
    );
  }

  Widget _panel({required Widget child, Key? key, Color? borderColor, Color? color, EdgeInsets? padding}) {
    // A Material rather than a decorated box, so that the expansion tiles
    // inside paint their ink on it.
    return Material(
      key: key,
      color: color ?? const Color(0xFF0F172A),
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(8),
        side: BorderSide(color: borderColor ?? Colors.white12),
      ),
      child: Padding(padding: padding ?? const EdgeInsets.all(10), child: child),
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
      child: Text(text, style: TextStyle(color: color, fontSize: 11, fontWeight: FontWeight.w600)),
    );
  }
}
