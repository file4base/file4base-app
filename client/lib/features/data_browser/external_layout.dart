import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/external_access.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter/material.dart';

/// The external occurrence a layout is based on, or null when the layout
/// shows records of one of this solution's tables (#107).
///
/// Such a layout is never bound to a local table: its records live in
/// another solution, and are read there.
TableOccurrenceModel? externalOccurrenceOfLayout(
  LayoutModel layout,
  List<TableOccurrenceModel> occurrences,
) {
  for (final o in occurrences) {
    if (o.id == layout.tableOccurrenceId) return o.isExternal ? o : null;
  }
  return null;
}

/// The names and ids of the external occurrences among [occurrences],
/// lowercased, as a layout field binding names its occurrence.
Set<String> externalOccurrenceKeys(List<TableOccurrenceModel> occurrences) => {
      for (final o in occurrences)
        if (o.isExternal) ...{o.id.toLowerCase(), o.name.toLowerCase()},
    };

/// What a field object of an external occurrence shows instead of a value, or
/// null when [binding] is a field of this solution (#107).
///
/// Such a field is never bound to a local column that shares its name: its
/// value lives in another solution, and only a field read through a
/// relationship says which record of it.
String? externalFieldNotice(FieldBindingModel? binding, Set<String> externalOccurrences) {
  final occurrence = binding?.tableOccurrence;
  // A field read through a relationship is read in the other solution.
  if (binding == null || binding.isRelated || occurrence == null ||
      !externalOccurrences.contains(occurrence.toLowerCase())) {
    return null;
  }
  return '$occurrence::${binding.fieldName} is in another solution (not available yet)';
}

/// A layout on an external occurrence: its records, read in the other
/// solution as the account this session holds there (#107), browsed with
/// [builder] once the table's fields are read from that solution, or why
/// they cannot be.
class ExternalLayoutView extends StatefulWidget {
  const ExternalLayoutView({
    required this.layoutName,
    required this.occurrence,
    required this.apiClient,
    required this.builder,
    this.onTable,
    this.designing = false,
    super.key,
  });

  final String layoutName;
  final TableOccurrenceModel occurrence;
  final ApiClient apiClient;

  /// Layout mode: [builder] designs the layout with the table's fields, and
  /// the placeholder says the layout can be designed once they are read.
  final bool designing;

  /// Browses the occurrence's table through a client whose record calls
  /// read the occurrence's records.
  final Widget Function(BuildContext context, TableModel table, ApiClient client) builder;

  /// Told the occurrence's table once it is read, with the privileges the
  /// record commands follow, and null when it cannot be.
  final ValueChanged<TableModel?>? onTable;

  @override
  State<ExternalLayoutView> createState() => _ExternalLayoutViewState();
}

class _ExternalLayoutViewState extends State<ExternalLayoutView> {
  late ExternalAccess _access;
  late ApiClient _client;
  TableModel? _table;
  ExternalUnavailable? _problem;
  Object? _error;

  bool get _bound =>
      widget.occurrence.external!.database.isNotEmpty && widget.occurrence.external!.table.isNotEmpty;

  @override
  void initState() {
    super.initState();
    _attach();
  }

  @override
  void didUpdateWidget(ExternalLayoutView old) {
    super.didUpdateWidget(old);
    if (old.occurrence.id != widget.occurrence.id || old.apiClient != widget.apiClient) {
      _access.removeListener(_load);
      _attach();
    }
  }

  @override
  void dispose() {
    _access.removeListener(_load);
    super.dispose();
  }

  void _attach() {
    _access = ExternalAccess.of(widget.apiClient)..addListener(_load);
    _client = widget.apiClient.forOccurrence(widget.occurrence.id);
    _table = null;
    _load();
  }

  void _load() {
    if (!_bound) return;
    setState(() {
      _problem = null;
      _error = null;
    });
    unawaited(_access.table(widget.occurrence).then((table) {
      if (!mounted) return;
      setState(() => _table = table);
      widget.onTable?.call(table);
    }, onError: (Object e) {
      if (!mounted) return;
      widget.onTable?.call(null);
      final problem = ExternalUnavailable.of(e);
      setState(() {
        _table = null;
        _problem = problem;
        _error = problem == null ? e : null;
      });
      if (problem != null) _access.askOnce(context, problem);
    }));
  }

  @override
  Widget build(BuildContext context) {
    final table = _table;
    if (table != null) return widget.builder(context, table, _client);
    if (_bound && _problem == null && _error == null) {
      return const Center(child: CircularProgressIndicator());
    }
    return ExternalLayoutPlaceholder(
      layoutName: widget.layoutName,
      occurrence: widget.occurrence,
      problem: _problem,
      error: _error,
      access: _access,
      designing: widget.designing,
    );
  }
}

/// What a layout on an external occurrence shows when its records cannot be
/// read: the reference, why, and what to do.
class ExternalLayoutPlaceholder extends StatelessWidget {
  const ExternalLayoutPlaceholder({
    required this.layoutName,
    required this.occurrence,
    this.problem,
    this.error,
    this.access,
    this.designing = false,
    super.key,
  });

  final String layoutName;
  final TableOccurrenceModel occurrence;

  /// In Layout mode: the layout is designed with the fields of that table,
  /// so it waits for them rather than for records.
  final bool designing;

  /// Why the other solution could not be read, when it was tried.
  final ExternalUnavailable? problem;
  final Object? error;
  final ExternalAccess? access;

  @override
  Widget build(BuildContext context) {
    final external = occurrence.external!;
    final problem = this.problem;
    final String why;
    if (problem != null) {
      why = problem.message;
    } else if (error != null) {
      why = 'Its records could not be read: $error';
    } else if (external.database.isEmpty) {
      why = 'The file reference "${external.fileReference}" is not mapped to a solution yet. '
          'Map it in File > Manage > File References to show its records.';
    } else if (external.table.isEmpty) {
      why = 'Its records live in the solution "${external.database}", through the file reference '
          '"${external.fileReference}", whose table is not known yet. Bind it in File > Manage > File References.';
    } else {
      why = 'Its records live in the solution "${external.database}", table "${external.table}", '
          'through the file reference "${external.fileReference}".';
    }
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 520),
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(problem?.isNotSignedIn ?? false ? Icons.lock_outline : Icons.link_off, size: 48),
              const SizedBox(height: 16),
              Text(
                'Layout "$layoutName" shows "${occurrence.name}", a table of another solution.',
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 8),
              Text(
                designing
                    ? '$why It can be designed once the fields of that table can be read.'
                    : '$why This layout shows no records and record commands are disabled.',
                textAlign: TextAlign.center,
              ),
              if (problem != null && problem.isNotSignedIn && access != null) ...[
                const SizedBox(height: 16),
                FilledButton.icon(
                  key: const ValueKey('external-layout-sign-in'),
                  onPressed: () => unawaited(access!.signIn(context, problem)),
                  icon: const Icon(Icons.login, size: 18),
                  label: Text('Sign in to ${problem.database}…'),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
