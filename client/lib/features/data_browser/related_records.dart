// #36 — a layout can show the other side of a relationship: a field of a
// related record, and a portal, which is the list of them.
//
// The record the layout is on is the one in hand; the server follows the
// relationship from it and returns what matches.

import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/external_access.dart';
import 'package:file4base_client/features/layout_engine/layout_object_visuals.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter/material.dart';

/// What the far side of a relationship is, resolved from the catalog the
/// client already holds.
class RelatedTarget {

  const RelatedTarget({
    required this.relationship,
    required this.occurrence,
    required this.table,
  });
  final RelationshipModel relationship;

  /// The occurrence whose records are read, and the table behind it. For an
  /// occurrence of another solution's table (#107) the table only has its
  /// name until its fields are read from that solution.
  final TableOccurrenceModel occurrence;
  final TableModel table;

  /// Whether the records are another solution's: they are read there, and
  /// not written from here yet.
  bool get isExternal => occurrence.isExternal;
}

/// Every occurrence reachable from [fromTable] by following one relationship,
/// which is what a related field or a portal on its layout can show.
///
/// A relationship that joins a table to itself appears twice, once for each
/// end, because either end can be the one shown.
List<RelatedTarget> reachableTargets({
  required String fromTable,
  required Iterable<RelationshipModel> relationships,
  required Map<String, TableOccurrenceModel> occurrencesById,
  required Map<String, TableModel> tablesById,
}) {
  final targets = <RelatedTarget>[];
  for (final rel in relationships) {
    final left = occurrencesById[rel.leftOccurrenceId];
    final right = occurrencesById[rel.rightOccurrenceId];
    if (left == null || right == null) continue;
    final leftTable = tablesById[left.baseTableId];
    final rightTable = tablesById[right.baseTableId];
    if (leftTable == null || rightTable == null) continue;

    if (leftTable.name == fromTable) {
      targets.add(RelatedTarget(relationship: rel, occurrence: right, table: rightTable));
    }
    if (rightTable.name == fromTable) {
      targets.add(RelatedTarget(relationship: rel, occurrence: left, table: leftTable));
    }
  }
  return targets;
}

/// The catalog a portal or a related field needs to reach the other side of a
/// relationship, and the record it starts from.
class RelatedContext {

  const RelatedContext({
    required this.apiClient,
    required this.fromTable,
    required this.recordId,
    required this.relationshipsById,
    required this.occurrencesById,
    required this.tablesById,
    required this.formatValue,
    this.fromDatabase,
    this.batched = false,
  });
  final ApiClient apiClient;

  /// Physical name of the table the layout is on.
  final String fromTable;

  /// The solution [fromTable] is in when the layout is on an occurrence of
  /// another solution's table (#107), null when it is this solution's.
  final String? fromDatabase;

  /// Whether the record is one of a list: its related field is read with
  /// those of the other records shown, in one request (no N+1).
  final bool batched;

  /// The record in hand, or null when the found set is empty.
  final String? recordId;

  final Map<String, RelationshipModel> relationshipsById;
  final Map<String, TableOccurrenceModel> occurrencesById;
  final Map<String, TableModel> tablesById;

  /// What a stored value looks like in a field box, which the data browser
  /// already knows how to work out.
  final String Function(ColumnModel column, Object? raw) formatValue;

  TableModel? _tableOfOccurrence(TableOccurrenceModel occurrence) {
    final external = occurrence.external;
    if (external == null) return tablesById[occurrence.baseTableId];
    // Named as it is in the other solution, so it is never taken for a
    // table of this one with the same name.
    return TableModel(id: occurrence.id, name: external.table, displayName: occurrence.name);
  }

  /// Whether [occurrence], whose table is [table], is an occurrence of the
  /// table the layout is on.
  bool _isFrom(TableOccurrenceModel occurrence, TableModel table) =>
      table.name == fromTable &&
      (occurrence.isExternal ? occurrence.external!.database == fromDatabase : fromDatabase == null);

  RelatedContext forRecord(String? recordId, {bool batched = false}) => RelatedContext(
        apiClient: apiClient,
        fromTable: fromTable,
        fromDatabase: fromDatabase,
        recordId: recordId,
        relationshipsById: relationshipsById,
        occurrencesById: occurrencesById,
        tablesById: tablesById,
        formatValue: formatValue,
        batched: batched,
      );

  /// The related records of the record in hand that [target] shows, at most
  /// [limit].
  Future<List<Map<String, dynamic>>> rows(RelatedTarget target, {List<String>? sort, int limit = 100}) {
    final recordId = this.recordId!;
    if (batched && limit == 1 && (sort == null || sort.isEmpty)) {
      return RelatedBatcher.of(apiClient).first(fromTable, recordId, target);
    }
    return apiClient.listRelatedRows(
      fromTable,
      recordId,
      relationshipId: target.relationship.id,
      occurrence: target.occurrence.id,
      sort: sort,
      limit: limit,
    );
  }

  /// Resolves which side of [relationshipId] a layout object reads, the same
  /// way the server does: [occurrence] names the side wanted, and without it
  /// the side that is not this layout's table is taken.
  ///
  /// Returns null when the relationship, the occurrence or the table behind it
  /// is not in the catalog — a layout pointing at something that has been
  /// deleted must still draw.
  RelatedTarget? resolve(String? relationshipId, String? occurrence) {
    RelatedTarget? tryRelationship(RelationshipModel rel) {
      final left = occurrencesById[rel.leftOccurrenceId];
      final right = occurrencesById[rel.rightOccurrenceId];
      if (left == null || right == null) return null;

      final leftTable = _tableOfOccurrence(left);
      final rightTable = _tableOfOccurrence(right);
      if (leftTable == null || rightTable == null) return null;

      final fromLeft = _isFrom(left, leftTable);
      final fromRight = _isFrom(right, rightTable);
      bool wantsRight;
      if (occurrence != null && occurrence.isNotEmpty) {
        if ((occurrence == right.id || occurrence.toLowerCase() == right.name.toLowerCase()) && fromLeft) {
          wantsRight = true;
        } else if ((occurrence == left.id || occurrence.toLowerCase() == left.name.toLowerCase()) && fromRight) {
          wantsRight = false;
        } else {
          return null;
        }
      } else if (fromLeft && fromRight) {
        // A self-join: which end is wanted is not something to guess.
        return null;
      } else if (fromLeft) {
        wantsRight = true;
      } else if (fromRight) {
        wantsRight = false;
      } else {
        return null;
      }

      return RelatedTarget(
        relationship: rel,
        occurrence: wantsRight ? right : left,
        table: wantsRight ? rightTable : leftTable,
      );
    }

    if (relationshipId != null && relationshipId.isNotEmpty) {
      final rel = relationshipsById[relationshipId];
      if (rel != null) {
        final match = tryRelationship(rel);
        if (match != null) return match;
      }
    }

    if (occurrence != null && occurrence.isNotEmpty) {
      for (final rel in relationshipsById.values) {
        final match = tryRelationship(rel);
        if (match != null) return match;
      }
    }
    return null;
  }
}

/// Reads the first related record of every record of a list in one request
/// per relationship (#107): each related field asks for its record, and the
/// asks made while a list is drawn go together.
class RelatedBatcher {
  RelatedBatcher._(this.apiClient);

  factory RelatedBatcher.of(ApiClient apiClient) => _byClient[apiClient] ??= RelatedBatcher._(apiClient);
  static final Expando<RelatedBatcher> _byClient = Expando();

  final ApiClient apiClient;
  final Map<(String, String, String), Map<String, Completer<List<Map<String, dynamic>>>>> _pending = {};

  Future<List<Map<String, dynamic>>> first(String table, String recordId, RelatedTarget target) {
    if (_pending.isEmpty) scheduleMicrotask(() => unawaited(_flush()));
    final waiting = _pending.putIfAbsent((table, target.relationship.id, target.occurrence.id), () => {});
    return waiting.putIfAbsent(recordId, Completer.new).future;
  }

  Future<void> _flush() async {
    final batches = Map.of(_pending);
    _pending.clear();
    for (final MapEntry(key: (table, relationship, occurrence), value: waiting) in batches.entries) {
      final ids = waiting.keys.toList();
      for (var start = 0; start < ids.length; start += ApiClient.maxRelatedBatch) {
        final chunk = ids.sublist(start, (start + ApiClient.maxRelatedBatch).clamp(0, ids.length));
        try {
          final rows = await apiClient.listRelatedRowsBatch(table, chunk,
              relationshipId: relationship, occurrence: occurrence, limit: 1);
          for (final id in chunk) {
            waiting[id]!.complete(rows[id] ?? const []);
          }
        } catch (e, st) {
          for (final id in chunk) {
            waiting[id]!.completeError(e, st);
          }
        }
      }
    }
  }
}

/// A calculation or summary field is filled in by the database, so it is shown
/// rather than typed into.
bool isComputedColumn(ColumnModel col) =>
    col.fieldType == 'CALCULATION' || col.fieldType == 'SUMMARY';

/// The box a layout object that could not be resolved is drawn as, so a broken
/// binding is visible on the layout instead of silently absent.
Widget relatedNotice(String message, {required bool isDark, double radius = 4}) {
  return Container(
    alignment: Alignment.center,
    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
    decoration: BoxDecoration(
      color: Colors.amber.withValues(alpha: 0.10),
      border: Border.all(color: Colors.amber),
      borderRadius: BorderRadius.circular(radius),
    ),
    child: Text(
      message,
      textAlign: TextAlign.center,
      style: const TextStyle(fontSize: 11, color: Colors.amber),
    ),
  );
}

/// A field of a related record shown on a layout: `Companies::company_address`
/// on a Customers layout.
///
/// It shows the value of the **first** related record, which is what a
/// relationship pointing at one record means. It is shown, not typed into: a
/// related record is edited in a portal, where it is clear which record is
/// being changed.
class RelatedFieldView extends StatefulWidget {

  const RelatedFieldView({
    required this.object, required this.related, required this.isDark, super.key,
  });
  final LayoutObjectModel object;
  final RelatedContext related;
  final bool isDark;

  @override
  State<RelatedFieldView> createState() => _RelatedFieldViewState();
}

class _RelatedFieldViewState extends State<RelatedFieldView> {
  Map<String, dynamic>? _row;
  bool _loading = false;
  bool _failed = false;

  /// The fields of another solution's table, once read (#107), and why that
  /// solution could not be read.
  TableModel? _externalTable;
  ExternalUnavailable? _problem;

  ExternalAccess get _access => ExternalAccess.of(widget.related.apiClient);

  @override
  void initState() {
    super.initState();
    _access.addListener(_reload);
    unawaited(_load());
  }

  @override
  void dispose() {
    _access.removeListener(_reload);
    super.dispose();
  }

  void _reload() => unawaited(_load());

  @override
  void didUpdateWidget(RelatedFieldView old) {
    super.didUpdateWidget(old);
    if (old.related.recordId != widget.related.recordId ||
        old.object.fieldBinding?.relationshipId != widget.object.fieldBinding?.relationshipId) {
      unawaited(_load());
    }
  }

  Future<void> _load() async {
    final binding = widget.object.fieldBinding;
    final recordId = widget.related.recordId;
    if (binding == null || recordId == null) {
      setState(() => _row = null);
      return;
    }
    final target = widget.related.resolve(binding.relationshipId, binding.tableOccurrence);
    if (target == null) {
      setState(() => _row = null);
      return;
    }

    setState(() {
      _loading = true;
      _failed = false;
      _problem = null;
    });
    try {
      if (target.isExternal) _externalTable = await _access.table(target.occurrence);
      final rows = await widget.related.rows(target, limit: 1);
      if (!mounted) return;
      setState(() {
        _row = rows.isEmpty ? null : rows.first;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      final problem = ExternalUnavailable.of(e);
      setState(() {
        _row = null;
        _loading = false;
        _failed = problem == null;
        _problem = problem;
      });
      if (problem != null) _access.askOnce(context, problem);
    }
  }

  @override
  Widget build(BuildContext context) {
    final obj = widget.object;
    final binding = obj.fieldBinding!;
    final target = widget.related.resolve(binding.relationshipId, binding.tableOccurrence);

    if (target == null) {
      return relatedNotice('<${binding.qualifiedName}>',
          isDark: widget.isDark, radius: obj.style.cornerRadius);
    }
    final problem = _problem;
    if (problem != null) {
      return ExternalProblemNotice(problem: problem, access: _access, compact: true);
    }
    if (target.isExternal && _externalTable == null) {
      return relatedNotice(_loading ? '…' : '<${binding.qualifiedName}>',
          isDark: widget.isDark, radius: obj.style.cornerRadius);
    }
    final col = (_externalTable ?? target.table).columns
        .where((c) => c.name.toLowerCase() == binding.fieldName.toLowerCase())
        .firstOrNull;
    if (col == null) {
      return relatedNotice('<${binding.qualifiedName}>',
          isDark: widget.isDark, radius: obj.style.cornerRadius);
    }

    final text = _row == null ? '' : widget.related.formatValue(col, _row![col.name]);

    return Container(
      key: const ValueKey('related-field'),
      alignment: Alignment.centerLeft,
      padding: const EdgeInsets.symmetric(horizontal: 8),
      decoration: BoxDecoration(
        color: obj.style.fillColor != null
            ? parseLayoutColor(obj.style.fillColor)
            : (widget.isDark ? const Color(0xFF21262E) : const Color(0xFFF6F8FA)),
        border: Border.all(
          color: obj.style.borderColor != null
              ? (parseLayoutColor(obj.style.borderColor) ?? Colors.grey)
              : (widget.isDark ? const Color(0xFF38404B) : Colors.grey.shade300),
          width: obj.style.borderWidth,
        ),
        borderRadius: BorderRadius.circular(obj.style.cornerRadius),
      ),
      child: Row(
        children: [
          Expanded(
            child: Text(
              text,
              textAlign: _textAlign(obj.style.textAlign),
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 13,
                fontWeight: obj.style.fontWeight == 'bold' ? FontWeight.bold : FontWeight.normal,
                color: obj.style.textColor != null ? parseLayoutColor(obj.style.textColor) : null,
              ),
            ),
          ),
          if (_loading)
            const SizedBox(
              width: 12,
              height: 12,
              child: CircularProgressIndicator(strokeWidth: 1.5),
            )
          else
            Tooltip(
              message: _failed
                  ? 'The related record could not be read'
                  : target.isExternal
                      ? 'From ${target.occurrence.name}, in ${target.occurrence.external!.database}, '
                          'through "${target.relationship.name}"'
                      : 'From ${target.occurrence.name} through "${target.relationship.name}" — '
                          'shown here, edited in a portal',
              child: Icon(
                _failed ? Icons.error_outline : Icons.link,
                size: 13,
                color: _failed ? Colors.red : Colors.grey,
              ),
            ),
        ],
      ),
    );
  }
}

/// A portal: one row per related record, with the layout objects drawn inside
/// the portal repeated against each of them.
class PortalView extends StatefulWidget {

  const PortalView({
    required this.object, required this.contents, required this.related, required this.isDark, super.key,
    this.onChanged,
    this.refreshGeneration = 0,
  });
  final LayoutObjectModel object;

  /// The objects drawn inside the portal, in layout coordinates. Each row
  /// redraws them against its own record.
  final List<LayoutObjectModel> contents;

  final RelatedContext related;
  final bool isDark;

  /// Called when a related record has been created or deleted, so the layout
  /// can refresh anything that counts them.
  final VoidCallback? onChanged;

  /// Changes when a script asks this portal to reload its related records
  /// (Refresh Portal, Refresh Window), whether or not the record changed.
  final int refreshGeneration;

  @override
  State<PortalView> createState() => _PortalViewState();
}

class _PortalViewState extends State<PortalView> {
  final Map<String, TextEditingController> _controllers = {};
  final Map<String, Timer?> _debounce = {};
  final Map<String, bool> _saving = {};

  List<Map<String, dynamic>> _rows = const [];
  bool _loading = false;
  String? _error;

  /// The fields of another solution's table, once read (#107), and why that
  /// solution could not be read. Its records are shown, not written.
  TableModel? _externalTable;
  ExternalUnavailable? _problem;

  ExternalAccess get _access => ExternalAccess.of(widget.related.apiClient);

  /// The row being typed into before it exists: a related record is created
  /// only once something has been entered.
  bool _creating = false;

  /// What has been typed into that row. It is cleared once the record exists,
  /// so the row is empty again for the next one.
  final TextEditingController _newRowController = TextEditingController();

  PortalConfigModel get _config => widget.object.portal;

  @override
  void initState() {
    super.initState();
    _access.addListener(_reload);
    unawaited(_load());
  }

  void _reload() => unawaited(_load());

  @override
  void didUpdateWidget(PortalView old) {
    super.didUpdateWidget(old);
    if (old.related.recordId != widget.related.recordId ||
        old.refreshGeneration != widget.refreshGeneration ||
        old.object.portalConfig.toString() != widget.object.portalConfig.toString()) {
      unawaited(_load());
    }
  }

  @override
  void dispose() {
    _access.removeListener(_reload);
    for (final t in _debounce.values) {
      t?.cancel();
    }
    for (final c in _controllers.values) {
      c.dispose();
    }
    _newRowController.dispose();
    super.dispose();
  }

  RelatedTarget? get _target =>
      widget.related.resolve(_config.relationshipId, _config.occurrence);

  Future<void> _load() async {
    final recordId = widget.related.recordId;
    final target = _target;
    if (recordId == null || target == null) {
      if (mounted) setState(() => _rows = const []);
      return;
    }

    setState(() {
      _loading = true;
      _error = null;
      _problem = null;
    });
    try {
      if (target.isExternal) _externalTable = await _access.table(target.occurrence);
      final rows = await widget.related.rows(target, sort: _config.sortFields, limit: ApiClient.maxPageSize);
      if (!mounted) return;
      setState(() {
        _rows = rows;
        _loading = false;
      });
      _syncControllers();
    } catch (e) {
      if (!mounted) return;
      final problem = ExternalUnavailable.of(e);
      setState(() {
        _rows = const [];
        _loading = false;
        _problem = problem;
        _error = problem == null ? e.toString() : null;
      });
      if (problem != null) _access.askOnce(context, problem);
    }
  }

  /// Whether the portal's records can be changed from here: another
  /// solution's cannot yet.
  bool get _writable => !(_target?.isExternal ?? false);

  String _cellKey(String rowId, String field) => '$rowId\u0000$field';

  /// Keeps one controller per cell on screen, and drops the ones whose record
  /// is no longer in the portal.
  void _syncControllers() {
    final target = _target;
    if (target == null) return;
    final live = <String>{};
    for (final row in _rows) {
      final rowId = row['id']?.toString() ?? '';
      if (rowId.isEmpty) continue;
      for (final obj in widget.contents) {
        final field = obj.fieldBinding?.fieldName;
        if (obj.type != 'field' || field == null) continue;
        final col = _column(target, field);
        if (col == null) continue;
        final key = _cellKey(rowId, field);
        live.add(key);
        final text = widget.related.formatValue(col, row[col.name]);
        final existing = _controllers[key];
        if (existing == null) {
          _controllers[key] = TextEditingController(text: text);
        } else if (existing.text != text && _debounce[key] == null) {
          // Do not pull the text out from under somebody still typing.
          existing.text = text;
        }
      }
    }
    for (final key in _controllers.keys.toList()) {
      if (!live.contains(key)) {
        _debounce.remove(key)?.cancel();
        _controllers.remove(key)?.dispose();
        _saving.remove(key);
      }
    }
  }

  ColumnModel? _column(RelatedTarget target, String field) => (_externalTable ?? target.table).columns
      .where((c) => c.name.toLowerCase() == field.toLowerCase())
      .firstOrNull;

  void _onCellChanged(String rowId, String field, String value) {
    final key = _cellKey(rowId, field);
    _debounce[key]?.cancel();
    _debounce[key] = Timer(const Duration(milliseconds: 600), () {
      _debounce[key] = null;
      unawaited(_saveCell(rowId, field, value));
    });
  }

  Future<void> _saveCell(String rowId, String field, String value) async {
    final target = _target;
    if (target == null) return;
    final key = _cellKey(rowId, field);
    setState(() => _saving[key] = true);
    try {
      final updated = await widget.related.apiClient
          .updateRow(target.table.name, rowId, {field: value});
      if (!mounted) return;
      setState(() {
        _saving.remove(key);
        final i = _rows.indexWhere((r) => r['id']?.toString() == rowId);
        if (i >= 0) {
          final rows = List<Map<String, dynamic>>.from(_rows);
          rows[i] = updated;
          _rows = rows;
        }
      });
      // A calculation of the related record may have changed with it.
      _syncControllers();
    } catch (e) {
      if (!mounted) return;
      setState(() => _saving[key] = false);
      _report('The related record could not be saved: $e');
    }
  }

  /// Creates the related record the empty last row stands for, and puts what
  /// was typed into it.
  Future<void> _createRow(String field, String value) async {
    final recordId = widget.related.recordId;
    final target = _target;
    if (recordId == null || target == null || _creating) return;

    setState(() => _creating = true);
    try {
      await widget.related.apiClient.createRelatedRow(
        widget.related.fromTable,
        recordId,
        relationshipId: target.relationship.id,
        occurrence: target.occurrence.id,
        values: value.isEmpty ? const {} : {field: value},
      );
      if (!mounted) return;
      setState(() => _creating = false);
      _newRowController.clear();
      await _load();
      widget.onChanged?.call();
    } catch (e) {
      if (!mounted) return;
      setState(() => _creating = false);
      _report('The related record could not be created: $e');
    }
  }

  Future<void> _deleteRow(String rowId) async {
    final target = _target;
    if (target == null) return;
    try {
      await widget.related.apiClient.deleteRow(target.table.name, rowId);
      if (!mounted) return;
      await _load();
      widget.onChanged?.call();
    } catch (e) {
      if (!mounted) return;
      _report('The related record could not be deleted: $e');
    }
  }

  void _report(String message) {
    if (!mounted) return;
    final messenger = ScaffoldMessenger.maybeOf(context);
    messenger?.showSnackBar(
      SnackBar(content: Text(message), backgroundColor: Colors.orange.shade800),
    );
  }

  @override
  Widget build(BuildContext context) {
    final obj = widget.object;
    final border = widget.isDark ? const Color(0xFF38404B) : Colors.grey.shade300;

    Widget body;
    final target = _target;
    if (!_config.isBound) {
      body = relatedNotice(
        'Portal: choose a relationship in Portal Setup',
        isDark: widget.isDark,
      );
    } else if (target == null) {
      body = relatedNotice(
        'Portal: the relationship it shows is no longer there',
        isDark: widget.isDark,
      );
    } else if (widget.related.recordId == null) {
      body = _message('No record in hand');
    } else if (_problem != null) {
      body = ExternalProblemNotice(problem: _problem!, access: _access);
    } else if (_error != null) {
      body = relatedNotice('Portal: $_error', isDark: widget.isDark);
    } else {
      body = _rowList(target);
    }

    return Container(
      key: const ValueKey('portal'),
      decoration: BoxDecoration(
        color: obj.style.fillColor != null
            ? parseLayoutColor(obj.style.fillColor)
            : (widget.isDark ? const Color(0xFF1B2029) : Colors.white),
        border: Border.all(
          color: obj.style.borderColor != null
              ? (parseLayoutColor(obj.style.borderColor) ?? border)
              : border,
          width: obj.style.borderWidth,
        ),
        borderRadius: BorderRadius.circular(obj.style.cornerRadius),
      ),
      clipBehavior: Clip.antiAlias,
      child: body,
    );
  }

  Widget _message(String text) => Center(
        child: Text(text, style: const TextStyle(fontSize: 11, color: Colors.grey)),
      );

  Widget _rowList(RelatedTarget target) {
    final rowHeight = _config.effectiveRowHeight(widget.object.height);
    // "Initial row" starts the portal past the first related record.
    final skip = (_config.initialRow - 1).clamp(0, _rows.length);
    final visible = _rows.skip(skip).toList();

    final children = <Widget>[
      for (final (i, row) in visible.indexed)
        _row(target, row, rowHeight, striped: i.isOdd),
    ];
    if (_config.allowCreation && _writable) {
      children.add(_newRow(target, rowHeight));
    }

    if (children.isEmpty) {
      return Stack(
        children: [
          _message(_loading ? 'Reading related records…' : 'No related records'),
          if (_loading) const SizedBox.shrink(),
        ],
      );
    }

    final list = ListView(
      primary: false,
      padding: EdgeInsets.zero,
      children: children,
    );

    return _config.showScrollBar ? Scrollbar(child: list) : list;
  }

  Widget _row(RelatedTarget target, Map<String, dynamic> row, double rowHeight,
      {required bool striped}) {
    final rowId = row['id']?.toString() ?? '';
    return Container(
      key: ValueKey('portal-row-$rowId'),
      height: rowHeight,
      decoration: BoxDecoration(
        color: striped
            ? (widget.isDark ? Colors.white.withValues(alpha: 0.03) : Colors.black.withValues(alpha: 0.02))
            : null,
        border: Border(
          bottom: BorderSide(
            color: widget.isDark ? const Color(0xFF2B313B) : Colors.grey.shade200,
          ),
        ),
      ),
      child: Stack(
        clipBehavior: Clip.none,
        children: [
          for (final content in widget.contents)
            Positioned(
              left: content.x - widget.object.x,
              top: content.y - widget.object.y,
              width: content.width,
              height: content.height,
              child: _cell(target, content, rowId, row),
            ),
          if (_config.allowDeletion && _writable && rowId.isNotEmpty)
            Positioned(
              right: 2,
              top: 0,
              bottom: 0,
              child: IconButton(
                key: ValueKey('portal-delete-$rowId'),
                padding: EdgeInsets.zero,
                constraints: const BoxConstraints(minWidth: 24, minHeight: 24),
                iconSize: 14,
                tooltip: 'Delete this related record',
                icon: const Icon(Icons.remove_circle_outline, color: Colors.redAccent),
                onPressed: () => _deleteRow(rowId),
              ),
            ),
        ],
      ),
    );
  }

  /// A cell of a portal row: a field of the related record, or any other
  /// object drawn inside the portal, which repeats unchanged.
  Widget _cell(RelatedTarget target, LayoutObjectModel content, String rowId,
      Map<String, dynamic> row) {
    if (content.type != 'field') {
      return _staticContent(content);
    }

    final field = content.fieldBinding?.fieldName ?? '';
    final col = _column(target, field);
    if (col == null) {
      return relatedNotice('<$field>', isDark: widget.isDark, radius: content.style.cornerRadius);
    }

    final text = widget.related.formatValue(col, row[col.name]);
    if (col.isPrimaryKey || isComputedColumn(col) || !_writable) {
      return Container(
        alignment: Alignment.centerLeft,
        padding: const EdgeInsets.symmetric(horizontal: 6),
        child: Text(
          text,
          overflow: TextOverflow.ellipsis,
          style: TextStyle(
            fontSize: content.style.fontSize > 0 ? content.style.fontSize : 12,
            fontFamily: col.isPrimaryKey ? 'monospace' : null,
            color: Colors.grey.shade600,
          ),
        ),
      );
    }

    final key = _cellKey(rowId, field);
    final controller = _controllers[key] ?? TextEditingController(text: text);
    _controllers[key] = controller;

    return TextField(
      key: ValueKey('portal-cell-$rowId-$field'),
      controller: controller,
      style: TextStyle(fontSize: content.style.fontSize > 0 ? content.style.fontSize : 12),
      textAlign: _textAlign(content.style.textAlign),
      decoration: InputDecoration(
        isDense: true,
        contentPadding: const EdgeInsets.symmetric(horizontal: 6, vertical: 6),
        border: const OutlineInputBorder(borderSide: BorderSide.none),
        suffixIcon: _saving[key] == true
            ? const Padding(
                padding: EdgeInsets.all(6),
                child: SizedBox(width: 10, height: 10, child: CircularProgressIndicator(strokeWidth: 1.5)),
              )
            : _saving[key] == false
                ? const Icon(Icons.error_outline, size: 13, color: Colors.red)
                : null,
      ),
      onChanged: (v) => _onCellChanged(rowId, field, v),
      onEditingComplete: () {
        _debounce[key]?.cancel();
        _debounce[key] = null;
        unawaited(_saveCell(rowId, field, controller.text));
      },
    );
  }

  Widget _staticContent(LayoutObjectModel content) {
    if (content.type == 'label') {
      return Container(
        alignment: Alignment.centerLeft,
        padding: const EdgeInsets.symmetric(horizontal: 4),
        child: Text(
          content.text,
          overflow: TextOverflow.ellipsis,
          style: TextStyle(
            fontSize: content.style.fontSize > 0 ? content.style.fontSize : 12,
            fontWeight: content.style.fontWeight == 'bold' ? FontWeight.bold : FontWeight.normal,
          ),
        ),
      );
    }
    return buildDrawnLayoutObject(content) ?? const SizedBox.shrink();
  }

  /// The empty row at the end of a portal that allows creation: typing in it
  /// creates the related record.
  Widget _newRow(RelatedTarget target, double rowHeight) {
    final field = widget.contents
        .where((c) => c.type == 'field' && (c.fieldBinding?.fieldName.isNotEmpty ?? false))
        .firstOrNull;

    return Container(
      key: const ValueKey('portal-new-row'),
      height: rowHeight,
      alignment: Alignment.centerLeft,
      padding: const EdgeInsets.symmetric(horizontal: 6),
      child: _creating
          ? const Row(
              children: [
                SizedBox(width: 12, height: 12, child: CircularProgressIndicator(strokeWidth: 1.5)),
                SizedBox(width: 8),
                Text('Creating…', style: TextStyle(fontSize: 11, color: Colors.grey)),
              ],
            )
          : TextField(
              key: const ValueKey('portal-new-cell'),
              controller: _newRowController,
              style: const TextStyle(fontSize: 12),
              decoration: InputDecoration(
                isDense: true,
                contentPadding: const EdgeInsets.symmetric(horizontal: 6, vertical: 6),
                border: const OutlineInputBorder(borderSide: BorderSide.none),
                hintText: field == null
                    ? 'Put a field in the portal to add records here'
                    : 'Add a ${target.occurrence.name} record…',
                hintStyle: TextStyle(fontSize: 11, color: Colors.grey.shade500),
                prefixIcon: const Icon(Icons.add, size: 14, color: Colors.grey),
                prefixIconConstraints: const BoxConstraints(minWidth: 24, minHeight: 24),
              ),
              enabled: field != null,
              onSubmitted: (v) {
                if (v.trim().isEmpty) return;
                unawaited(_createRow(field!.fieldBinding!.fieldName, v));
              },
            ),
    );
  }
}

TextAlign _textAlign(String align) {
  switch (align) {
    case 'center':
      return TextAlign.center;
    case 'right':
      return TextAlign.right;
    default:
      return TextAlign.left;
  }
}
