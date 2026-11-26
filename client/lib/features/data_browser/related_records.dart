// #36 — a layout can show the other side of a relationship: a field of a
// related record, and a portal, which is the list of them.
//
// The record the layout is on is the one in hand; the server follows the
// relationship from it and returns what matches.

import 'dart:async';
import 'dart:math' as math;

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/external_access.dart';
import 'package:file4base_client/features/layout_engine/layout_object_visuals.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/layout_engine/script_trigger_runtime.dart';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';

/// What the far side of a relationship is, resolved from the catalog the
/// client already holds.
class RelatedTarget {

  const RelatedTarget({
    required this.relationship,
    required this.occurrence,
    required this.table,
    this.side = 'right',
    this.path = const [],
  });

  /// The relationship followed; for a target along [path], the last one.
  final RelationshipModel relationship;

  /// The occurrences reached in turn from the layout's when [occurrence] is
  /// several relationships away (#26); empty when [relationship] alone
  /// reaches it.
  final List<String> path;

  /// Whether [occurrence] is reached along [path].
  bool get isAlongPath => path.isNotEmpty;

  /// The end of the relationship looked through, `left` or `right`: what
  /// tells the two ends apart when it joins an occurrence to itself.
  final String side;

  /// [side] as a request names it: only when the occurrence alone does not
  /// say which end is meant.
  String? get requestSide => relationship.leftOccurrenceId == relationship.rightOccurrenceId ? side : null;

  /// The occurrence whose records are read, and the table behind it. For an
  /// occurrence of another solution's table (#107) the table only has its
  /// name until its fields are read from that solution.
  final TableOccurrenceModel occurrence;
  final TableModel table;

  /// Whether the records are another solution's: they are read and written
  /// there, as the account the session holds in it.
  bool get isExternal => occurrence.isExternal;

  /// Whether a record of the other side may create records here: the
  /// relationship's allow-creation of the side looked through, as FileMaker
  /// sets it per side.
  /// No record is created through several relationships.
  bool get allowsCreation =>
      !isAlongPath && (side == 'left' ? relationship.leftAllowCreation : relationship.allowCreation);
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
      targets.add(RelatedTarget(relationship: rel, occurrence: left, table: leftTable, side: 'left'));
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
    this.layoutOccurrence,
    this.revision = 0,
  });
  final ApiClient apiClient;

  /// Changes whenever the record in hand is written, so what is read
  /// through its match fields (a field along a path) is read again: a key
  /// changed in the record reaches another related record.
  final int revision;

  /// The occurrence the layout is on (its id or name): where a field read
  /// along several relationships starts from. Null reads from the table's
  /// own occurrence.
  final String? layoutOccurrence;

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

  /// The client that writes [target]'s records. Another solution's are
  /// written through their occurrence; so is any target when the layout is
  /// on another solution's table, since [apiClient]'s record calls are then
  /// that layout's occurrence's.
  ApiClient clientFor(RelatedTarget target) =>
      target.isExternal || fromDatabase != null ? apiClient.forOccurrence(target.occurrence.id) : apiClient;

  /// The layout's record a portal's rows are evaluated in: the record in
  /// hand, seen from the layout's occurrence (the table's own when none is
  /// named). Null with no record, and on a layout of another solution's
  /// table, whose portal rows are evaluated on their own.
  EvaluationContext? get rowContext {
    final id = recordId;
    if (id == null || id.isEmpty || fromDatabase != null) return null;
    var occurrence = layoutOccurrence;
    if (occurrence == null || occurrence.isEmpty) {
      final own = [
        for (final o in occurrencesById.values)
          if (!o.isExternal && tablesById[o.baseTableId]?.name == fromTable) o,
      ];
      occurrence = (own.where((o) => o.name.toLowerCase() == fromTable.toLowerCase()).firstOrNull ?? own.firstOrNull)?.id;
    }
    return occurrence == null ? null : (occurrence: occurrence, recordId: id);
  }

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
        layoutOccurrence: layoutOccurrence,
        revision: revision,
      );

  /// The occurrence a field along [path] is read from, the last one, with
  /// its table; null when it is not in the catalog, or is another
  /// solution's, which a path is not followed into yet.
  ({TableOccurrenceModel occurrence, TableModel table})? endOfPath(List<String> path) {
    if (path.isEmpty) return null;
    final name = path.last.toLowerCase();
    final occurrence = occurrencesById.values.where((o) => o.name.toLowerCase() == name || o.id == path.last).firstOrNull;
    if (occurrence == null || occurrence.isExternal) return null;
    final table = tablesById[occurrence.baseTableId];
    return table == null ? null : (occurrence: occurrence, table: table);
  }

  /// The first record at the end of [path] related to the record in hand,
  /// or null. Read with those of the other records of a list when
  /// [batched].
  Future<Map<String, dynamic>?> firstAlong(List<String> path) {
    final recordId = this.recordId!;
    if (batched) return RelatedBatcher.of(apiClient).firstAlong(fromTable, recordId, layoutOccurrence, path);
    return apiClient
        .firstRelatedAlong(fromTable, [recordId], path: path, context: layoutOccurrence)
        .then((records) => records[recordId]);
  }

  /// What a portal along [path] shows: the last occurrence of [path], with
  /// its table and the relationship that reaches it from the one before.
  /// Null when one of them is not in the catalog, or is another solution's,
  /// which a path is not followed into yet.
  RelatedTarget? alongPath(List<String> path) {
    final end = endOfPath(path);
    if (end == null || path.length < 2) return null;
    final before = path[path.length - 2].toLowerCase();
    final previous =
        occurrencesById.values.where((o) => o.name.toLowerCase() == before || o.id == path[path.length - 2]).firstOrNull;
    if (previous == null) return null;
    for (final rel in relationshipsById.values) {
      final right = rel.rightOccurrenceId == end.occurrence.id && rel.leftOccurrenceId == previous.id;
      final left = rel.leftOccurrenceId == end.occurrence.id && rel.rightOccurrenceId == previous.id;
      if (right || left) {
        return RelatedTarget(
            relationship: rel, occurrence: end.occurrence, table: end.table, side: right ? 'right' : 'left', path: path);
      }
    }
    return null;
  }

  /// The related records of the record in hand that [target] shows, at most
  /// [limit]. Those along a path are read for that record alone: at most
  /// [ApiClient.maxRecordsAlong] of them.
  Future<List<Map<String, dynamic>>> rows(RelatedTarget target, {List<String>? sort, int limit = 100}) =>
      rowsPage(target, sort: sort, limit: limit).then((page) => page.records);

  /// [rows], and whether there may be more related records than those
  /// read: what the server says along a path, and a full page otherwise.
  Future<({List<Map<String, dynamic>> records, bool more})> rowsPage(RelatedTarget target,
      {List<String>? sort, int limit = 100}) async {
    final recordId = this.recordId!;
    if (target.isAlongPath) {
      return apiClient.listRelatedAlong(fromTable, recordId, path: target.path, context: layoutOccurrence, sort: sort);
    }
    final records = await _rowsOneAway(target, recordId, sort: sort, limit: limit);
    return (records: records, more: records.length >= limit);
  }

  Future<List<Map<String, dynamic>>> _rowsOneAway(RelatedTarget target, String recordId,
      {List<String>? sort, int limit = 100}) {
    if (batched) {
      return RelatedBatcher.of(apiClient).rows(fromTable, recordId, target, sort: sort, limit: limit);
    }
    return apiClient.listRelatedRows(
      fromTable,
      recordId,
      relationshipId: target.relationship.id,
      occurrence: target.occurrence.id,
      side: target.requestSide,
      sort: sort,
      limit: limit,
    );
  }

  /// Resolves which side of [relationshipId] a layout object reads, the same
  /// way the server does: [occurrence] names the side wanted, and without it
  /// the side that is not this layout's table is taken. A relationship that
  /// joins an occurrence to itself has it at both ends, so [side] (one of
  /// [RelationshipSide]) names the end; without it the right end is read.
  ///
  /// Returns null when the relationship, the occurrence or the table behind it
  /// is not in the catalog — a layout pointing at something that has been
  /// deleted must still draw.
  RelatedTarget? resolve(String? relationshipId, String? occurrence, {String? side}) {
    RelatedTarget? tryRelationship(RelationshipModel rel) {
      final left = occurrencesById[rel.leftOccurrenceId];
      final right = occurrencesById[rel.rightOccurrenceId];
      if (left == null || right == null) return null;

      final leftTable = _tableOfOccurrence(left);
      final rightTable = _tableOfOccurrence(right);
      if (leftTable == null || rightTable == null) return null;

      final fromLeft = _isFrom(left, leftTable);
      final fromRight = _isFrom(right, rightTable);
      final selfJoin = left.id == right.id && fromLeft && fromRight;
      final named = occurrence == null || occurrence.isEmpty ||
          occurrence == left.id || occurrence.toLowerCase() == left.name.toLowerCase();
      bool wantsRight;
      if (selfJoin && named) {
        // Both ends are this occurrence: the end stored, the right one
        // without, as Portal Setup shows it.
        wantsRight = side != RelationshipSide.left;
      } else if (occurrence != null && occurrence.isNotEmpty) {
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
        side: wantsRight ? 'right' : 'left',
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

/// Reads the related records of every record of a list in one request per
/// relationship, order and page size (#107): each related field or portal of
/// a row asks for its record, and the asks made while a list is drawn go
/// together.
class RelatedBatcher {
  RelatedBatcher._(this.apiClient);

  factory RelatedBatcher.of(ApiClient apiClient) => _byClient[apiClient] ??= RelatedBatcher._(apiClient);
  static final Expando<RelatedBatcher> _byClient = Expando();

  final ApiClient apiClient;
  final Map<_BatchKey, Map<String, Completer<List<Map<String, dynamic>>>>> _pending = {};
  final Map<({String table, String context, String path}), Map<String, Completer<Map<String, dynamic>?>>> _pendingAlong = {};

  /// The first record at the end of [path] related to [recordId], read with
  /// the others asked for while the list is drawn.
  Future<Map<String, dynamic>?> firstAlong(String table, String recordId, String? context, List<String> path) {
    if (_pending.isEmpty && _pendingAlong.isEmpty) scheduleMicrotask(() => unawaited(_flush()));
    final key = (table: table, context: context ?? '', path: path.join('\u0000'));
    final waiting = _pendingAlong.putIfAbsent(key, () => {});
    return waiting.putIfAbsent(recordId, Completer.new).future;
  }

  /// At most [limit] related records of [recordId] that [target] shows, in
  /// the order [sort] gives (the relationship's own when empty).
  Future<List<Map<String, dynamic>>> rows(String table, String recordId, RelatedTarget target,
      {List<String>? sort, int limit = 100}) {
    if (_pending.isEmpty && _pendingAlong.isEmpty) scheduleMicrotask(() => unawaited(_flush()));
    final key = (
      table: table,
      relationship: target.relationship.id,
      occurrence: target.occurrence.id,
      side: target.requestSide,
      sort: (sort ?? const []).join(','),
      limit: limit,
    );
    final waiting = _pending.putIfAbsent(key, () => {});
    return waiting.putIfAbsent(recordId, Completer.new).future;
  }

  Future<void> _flush() async {
    final batches = Map.of(_pending);
    _pending.clear();
    final along = Map.of(_pendingAlong);
    _pendingAlong.clear();
    for (final MapEntry(:key, value: waiting) in along.entries) {
      final ids = waiting.keys.toList();
      for (var start = 0; start < ids.length; start += ApiClient.maxRelatedBatch) {
        final chunk = ids.sublist(start, (start + ApiClient.maxRelatedBatch).clamp(0, ids.length));
        try {
          final records = await apiClient.firstRelatedAlong(key.table, chunk,
              path: key.path.split('\u0000'), context: key.context);
          for (final id in chunk) {
            waiting[id]!.complete(records[id]);
          }
        } catch (e, st) {
          for (final id in chunk) {
            waiting[id]!.completeError(e, st);
          }
        }
      }
    }
    for (final MapEntry(:key, value: waiting) in batches.entries) {
      final ids = waiting.keys.toList();
      for (var start = 0; start < ids.length; start += ApiClient.maxRelatedBatch) {
        final chunk = ids.sublist(start, (start + ApiClient.maxRelatedBatch).clamp(0, ids.length));
        try {
          final rows = await apiClient.listRelatedRowsBatch(key.table, chunk,
              relationshipId: key.relationship,
              occurrence: key.occurrence,
              side: key.side,
              sort: key.sort.isEmpty ? null : [key.sort],
              limit: key.limit);
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

typedef _BatchKey = ({String table, String relationship, String occurrence, String? side, String sort, int limit});

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
    this.editable = false,
    this.formulaTrue,
    this.onWritten,
  });
  final LayoutObjectModel object;
  final RelatedContext related;
  final bool isDark;

  /// Whether a field several relationships away may be typed into: what it
  /// writes is the first record at the end of the chain, the one shown.
  final bool editable;

  /// Whether the calculation of the object's conditional format at an
  /// index is true on the record in hand, evaluated from the layout's
  /// occurrence with Self as the value shown. Null applies none.
  final bool Function(int index)? formulaTrue;

  /// Called once a value typed into the field is saved, for what reads it
  /// (its conditional formats, the layout's conditions) to be evaluated
  /// again.
  final VoidCallback? onWritten;

  @override
  State<RelatedFieldView> createState() => _RelatedFieldViewState();
}

class _RelatedFieldViewState extends State<RelatedFieldView> {
  Map<String, dynamic>? _row;
  bool _loading = false;
  bool _failed = false;

  /// Counts the reads started, so the answer to one the field has since
  /// moved on from (another record, relationship or end) is dropped.
  int _generation = 0;

  /// The fields of another solution's table, once read (#107), and why that
  /// solution could not be read.
  TableModel? _externalTable;
  ExternalUnavailable? _problem;

  ExternalAccess get _access => ExternalAccess.of(widget.related.apiClient);

  /// What is typed into a field along a path, and what it held when read.
  final TextEditingController _edit = TextEditingController();
  final FocusNode _editFocus = FocusNode();
  String _editShown = '';
  bool _saving = false;

  /// The record shown when the field was entered: what is typed is meant
  /// for it, even if the field reads another one meanwhile.
  String? _editTarget;

  @override
  void initState() {
    super.initState();
    _access.addListener(_reload);
    _editFocus.addListener(() {
      if (_editFocus.hasFocus) {
        _editTarget = _row?['id']?.toString();
      } else {
        unawaited(_saveAlongPath());
      }
    });
    unawaited(_load());
  }

  @override
  void dispose() {
    _access.removeListener(_reload);
    _edit.dispose();
    _editFocus.dispose();
    super.dispose();
  }

  /// Writes what was typed to the first record at the end of the path. That
  /// record is read again first: a match field changed since it was shown
  /// (here, by a script, by another user) relates another record, and the
  /// one shown must not be written then.
  Future<void> _saveAlongPath() async {
    final binding = widget.object.fieldBinding;
    final row = _row;
    final end = binding == null ? null : widget.related.endOfPath(binding.occurrencePath);
    final col = end?.table.columns.where((c) => c.name.toLowerCase() == binding!.fieldName.toLowerCase()).firstOrNull;
    final id = _editTarget ?? row?['id']?.toString();
    // What was typed, before anything is awaited: the box may be redrawn.
    final value = _edit.text;
    if (_saving || end == null || col == null || id == null || value == _editShown) return;
    _saving = true;
    final messenger = ScaffoldMessenger.maybeOf(context);
    try {
      final current = await widget.related.firstAlong(binding!.occurrencePath);
      if (!mounted) return;
      if (current?['id']?.toString() != id) {
        final text = current == null ? '' : widget.related.formatValue(col, current[col.name]);
        _generation++; // a read still running is of the record left
        final currentId = current?['id']?.toString();
        _editTarget = _editFocus.hasFocus ? currentId : null;
        setState(() {
          _row = current;
          _edit.text = text;
          _editShown = text;
        });
        messenger?.showSnackBar(SnackBar(
            content: Text('${binding.qualifiedName} was not saved: the related record changed since it was shown.')));
        return;
      }
      final saved = await widget.related.apiClient.updateRow(end.table.name, id, {col.name: value});
      if (!mounted) return;
      if (!_editFocus.hasFocus) _editTarget = null;
      setState(() {
        _row = {...current!, ...saved};
        _editShown = value;
        if (!_editFocus.hasFocus) _edit.text = value;
      });
      widget.onWritten?.call();
    } on Exception catch (e) {
      if (!mounted) return;
      _edit.text = _editShown;
      messenger?.showSnackBar(SnackBar(content: Text('${binding!.qualifiedName} was not saved: $e')));
    } finally {
      _saving = false;
    }
  }

  void _reload() => unawaited(_load());

  @override
  void didUpdateWidget(RelatedFieldView old) {
    super.didUpdateWidget(old);
    if (old.related.recordId != widget.related.recordId ||
        old.related.fromTable != widget.related.fromTable ||
        old.object.id != widget.object.id ||
        old.object.fieldBinding?.relationshipId != widget.object.fieldBinding?.relationshipId ||
        old.object.fieldBinding?.tableOccurrence != widget.object.fieldBinding?.tableOccurrence ||
        old.object.fieldBinding?.relationshipSide != widget.object.fieldBinding?.relationshipSide ||
        old.related.layoutOccurrence != widget.related.layoutOccurrence ||
        old.object.fieldBinding?.occurrencePath.join('/') != widget.object.fieldBinding?.occurrencePath.join('/') ||
        // The record in hand was written: a changed match field reaches
        // another record along the path.
        (old.related.revision != widget.related.revision && (widget.object.fieldBinding?.isAlongPath ?? false))) {
      unawaited(_load());
    }
  }

  Future<void> _load() async {
    final generation = ++_generation;
    bool stale() => !mounted || generation != _generation;
    final binding = widget.object.fieldBinding;
    final recordId = widget.related.recordId;
    if (binding == null || recordId == null) {
      setState(() => _row = null);
      return;
    }
    if (binding.isAlongPath) {
      if (widget.related.endOfPath(binding.occurrencePath) == null) {
        setState(() => _row = null);
        return;
      }
      setState(() {
        _loading = true;
        _failed = false;
        _problem = null;
      });
      try {
        final row = await widget.related.firstAlong(binding.occurrencePath);
        if (stale()) return;
        setState(() {
          _row = row;
          _loading = false;
        });
      } on Exception catch (_) {
        if (stale()) return;
        setState(() {
          _row = null;
          _loading = false;
          _failed = true;
        });
      }
      return;
    }
    final target = widget.related.resolve(binding.relationshipId, binding.tableOccurrence, side: binding.relationshipSide);
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
      final table = target.isExternal ? await _access.table(target.occurrence) : null;
      if (stale()) return;
      if (table != null) _externalTable = table;
      final rows = await widget.related.rows(target, limit: 1);
      if (stale()) return;
      setState(() {
        _row = rows.isEmpty ? null : rows.first;
        _loading = false;
      });
    } catch (e) {
      if (stale()) return;
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
    if (binding.isAlongPath) return _buildAlongPath(obj, binding);
    final target = widget.related.resolve(binding.relationshipId, binding.tableOccurrence, side: binding.relationshipSide);

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

    return _box(
      obj,
      text,
      _lookOf(obj, col),
      _failed
          ? 'The related record could not be read'
          : target.isExternal
              ? 'From ${target.occurrence.name}, in ${target.occurrence.external!.database}, '
                  'through "${target.relationship.name}"'
              : 'From ${target.occurrence.name} through "${target.relationship.name}" — '
                  'shown here, edited in a portal',
    );
  }

  /// A field of an occurrence several relationships away: the first record
  /// at the end of the chain, as FileMaker shows it.
  Widget _buildAlongPath(LayoutObjectModel obj, FieldBindingModel binding) {
    final end = widget.related.endOfPath(binding.occurrencePath);
    final col = end?.table.columns.where((c) => c.name.toLowerCase() == binding.fieldName.toLowerCase()).firstOrNull;
    if (end == null || col == null) {
      return relatedNotice('<${binding.qualifiedName}>', isDark: widget.isDark, radius: obj.style.cornerRadius);
    }
    final text = _row == null ? '' : widget.related.formatValue(col, _row![col.name]);
    final look = _lookOf(obj, col);
    final editable = widget.editable &&
        _row != null &&
        widget.related.fromDatabase == null &&
        binding.allowBrowseEntry &&
        !isComputedColumn(col) &&
        !col.isRepeating &&
        // Text and numbers, typed as they are stored; a date, a time or a
        // file is still changed on a layout of its own table.
        !const {'DATE', 'TIME', 'TIMESTAMP', 'CONTAINER', 'BOOLEAN'}.contains(col.storageType.toUpperCase());
    if (editable) {
      if (!_editFocus.hasFocus && !_saving && _edit.text != text) {
        _edit.text = text;
        _editShown = text;
      }
      return Tooltip(
        message: 'From ${end.occurrence.name}, through ${binding.occurrencePath.join(' → ')}: '
            'the first related record, edited here',
        child: TextField(
          key: const ValueKey('related-field-edit'),
          controller: _edit,
          focusNode: _editFocus,
          textAlign: _textAlign(obj.style.textAlign),
          style: conditionalTextStyle(TextStyle(fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 13), look),
          decoration: InputDecoration(
            isDense: true,
            contentPadding: const EdgeInsets.symmetric(horizontal: 8, vertical: 8),
            border: OutlineInputBorder(borderRadius: BorderRadius.circular(obj.style.cornerRadius)),
            fillColor: parseLayoutColor(look?.fillColor),
            filled: parseLayoutColor(look?.fillColor) != null,
            suffixIcon: const Icon(Icons.link, size: 13, color: Colors.grey),
          ),
          onSubmitted: (_) => unawaited(_saveAlongPath()),
        ),
      );
    }
    return _box(
      obj,
      text,
      look,
      _failed
          ? 'The related record could not be read'
          : 'From ${end.occurrence.name}, through ${binding.occurrencePath.join(' → ')}: '
              'the first related record',
    );
  }

  /// What the object's conditional formats that apply give the value shown.
  ConditionalFormatRule? _lookOf(LayoutObjectModel obj, ColumnModel col) =>
      obj.style.conditionalLook(_row?[col.name], formulaTrue: widget.formulaTrue);

  Widget _box(LayoutObjectModel obj, String text, ConditionalFormatRule? look, String tooltip) {
    return Container(
      key: const ValueKey('related-field'),
      alignment: Alignment.centerLeft,
      padding: const EdgeInsets.symmetric(horizontal: 8),
      decoration: BoxDecoration(
        color: parseLayoutColor(look?.fillColor) ??
            (obj.style.fillColor != null
                ? parseLayoutColor(obj.style.fillColor)
                : (widget.isDark ? const Color(0xFF21262E) : const Color(0xFFF6F8FA))),
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
              style: conditionalTextStyle(
                TextStyle(
                  fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 13,
                  fontWeight: obj.style.fontWeight == 'bold' ? FontWeight.bold : FontWeight.normal,
                  color: obj.style.textColor != null ? parseLayoutColor(obj.style.textColor) : null,
                ),
                look,
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
              message: tooltip,
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

/// The active row of a portal of the layout in hand, which Go to Portal Row
/// moves, a click on a row sets and Get(ActivePortalRowNumber) reads. The
/// window that shows the portal holds it, one per portal object, and the
/// portal's view fills it in: the rows it shows, and how to put the focus in
/// one of their fields, write one or delete one.
class PortalRowController extends ChangeNotifier {
  PortalRowController(this.object);

  /// The portal object.
  final LayoutObjectModel object;

  List<Map<String, dynamic>> _rows = const [];
  int _active = 0;
  RelatedTarget? _target;
  String? _parentId;
  bool _truncated = false;
  bool _unordered = false;
  Completer<void> _loaded = Completer<void>();

  /// Whether the portal has more related records than [rows]: it read at
  /// most a page of them. Its last row is then not the last record.
  bool get truncated => _truncated;

  /// Whether the rows read are not the first ones in the portal's order:
  /// a portal along a path reads its first page in the path's order, then
  /// sorts that page. No row is then at its place.
  bool get unordered => _unordered;

  /// The related records the portal shows, its first row first (past the
  /// rows its "initial row" skips).
  List<Map<String, dynamic>> get rows => _rows;

  /// The occurrence, table and relationship the rows come from, once known.
  RelatedTarget? get target => _target;

  /// The record in hand the rows are related to.
  String? get parentId => _parentId;

  /// The active row, 1-based; 0 when none is.
  int get activeRow => _active;

  /// The related record of the active row.
  Map<String, dynamic>? get activeRecord => _active > 0 && _active <= _rows.length ? _rows[_active - 1] : null;

  /// Makes row [row] the active one (0: none).
  void activate(int row) {
    final n = row < 0 || row > _rows.length ? 0 : row;
    if (n == _active) return;
    _active = n;
    notifyListeners();
  }

  /// Completes once the portal has read the rows of the record in hand, or
  /// after [timeout] when it is not on screen to read them.
  Future<void> whenLoaded({Duration timeout = const Duration(seconds: 5)}) =>
      _loaded.future.timeout(timeout, onTimeout: () {});

  /// Called by the view: the portal is reading its rows again.
  void loading() {
    if (_loaded.isCompleted) _loaded = Completer<void>();
  }

  /// Called by the view with the rows it shows. The active row stays the
  /// same record's while it is still in the portal.
  void showRows(List<Map<String, dynamic>> rows,
      {RelatedTarget? target, String? parentId, bool truncated = false, bool unordered = false}) {
    final activeId = activeRecord?['id']?.toString();
    final sameParent = parentId == _parentId;
    _rows = rows;
    _truncated = truncated;
    _unordered = unordered;
    _target = target;
    _parentId = parentId;
    if (_active > 0) {
      final at = sameParent && activeId != null ? rows.indexWhere((r) => r['id']?.toString() == activeId) : -1;
      _active = at + 1;
    }
    if (!_loaded.isCompleted) _loaded.complete();
    notifyListeners();
  }

  /// Puts the focus in the field object [objectId] of row [row], scrolled
  /// into view; answers the box, or null when the row shows no such field
  /// that takes entry. Set by the view.
  Future<TriggerBox?> Function(int row, String objectId)? focusCell;

  /// The objects of the portal's rows that take entry, in tab order.
  List<LayoutObjectModel> Function()? enterableFields;

  /// Writes [values] into the related record [rowId]. Set by the view.
  Future<Map<String, dynamic>> Function(String rowId, Map<String, dynamic> values, {bool script})? writeRow;

  /// Deletes the related record [rowId], after asking when [confirm];
  /// answers FileMaker's error code. Set by the view.
  Future<int> Function(String rowId, {required bool confirm})? deleteRow;

  /// Set by the window: told when the user makes a row active, and when a
  /// field box of a row gains or loses the focus.
  VoidCallback? onActivated;
  void Function(TriggerBox box, bool focused)? onCellFocus;

  /// Set by the window: whether the field object's value is validated and
  /// saved by its triggers when it is left, rather than as it is typed; and
  /// told when the user changes a field box's content.
  bool Function(LayoutObjectModel content)? guarded;
  void Function(LayoutObjectModel content)? onCellModified;
}

/// A portal: one row per related record, with the layout objects drawn inside
/// the portal repeated against each of them.
class PortalView extends StatefulWidget {

  const PortalView({
    required this.object, required this.contents, required this.related, required this.isDark, super.key,
    this.onChanged,
    this.refreshGeneration = 0,
    this.rowController,
  });
  final LayoutObjectModel object;

  /// The active row of the portal, held by the window (#26). In a list
  /// row it is the portal of that row's record.
  final PortalRowController? rowController;

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
  final Map<String, FocusNode> _focusNodes = {};
  final ScrollController _scroll = ScrollController();
  final Map<String, Timer?> _debounce = {};
  final Map<String, bool> _saving = {};

  List<Map<String, dynamic>> _rows = const [];
  bool _loading = false;
  String? _error;

  /// Whether more related records than [_rows] were there to read.
  bool _truncated = false;

  /// Counts the reads started, so the answer to one the portal has since
  /// moved on from (another record, relationship, end or layout) is dropped.
  int _generation = 0;

  /// The fields of another solution's table, once read (#107), with what the
  /// account held there may do with its records, and why that solution could
  /// not be read.
  TableModel? _externalTable;
  ExternalUnavailable? _problem;

  ExternalAccess get _access => ExternalAccess.of(widget.related.apiClient);

  /// The row being typed into before it exists: a related record is created
  /// only once something has been entered.
  bool _creating = false;

  /// The conditional formats of the row objects whose calculation is true
  /// on a row, as `<object id>#<index of the rule>`, by read, row and the
  /// row's values: each row drawn is evaluated once for each version of its
  /// record, the rows drawn together in one request. Until a row's answer
  /// comes, its last one holds.
  final Map<String, Set<String>> _rowTruths = {};
  final Map<String, Set<String>> _lastRowTruths = {};
  final Map<String, String> _rowQueue = {};
  final Set<String> _rowPending = {};

  /// What has been typed into that row. It is cleared once the record exists,
  /// so the row is empty again for the next one.
  final TextEditingController _newRowController = TextEditingController();

  PortalConfigModel get _config => widget.object.portal;

  @override
  void initState() {
    super.initState();
    _access.addListener(_reload);
    _attachController(widget.rowController);
    unawaited(_load());
  }

  void _attachController(PortalRowController? controller) {
    if (controller == null) return;
    controller
      ..focusCell = _focusCell
      ..enterableFields = _enterableFields
      ..writeRow = _writeRow
      ..deleteRow = _deleteRowCode
      ..addListener(_rowsChanged);
  }

  void _detachController(PortalRowController? controller) {
    if (controller == null) return;
    controller.removeListener(_rowsChanged);
    if (controller.focusCell == _focusCell) {
      controller
        ..focusCell = null
        ..enterableFields = null
        ..writeRow = null
        ..deleteRow = null;
    }
  }

  void _rowsChanged() {
    if (mounted) setState(() {});
  }

  /// The rows shown, past those the "initial row" skips.
  List<Map<String, dynamic>> get _visibleRows {
    final skip = (_config.initialRow - 1).clamp(0, _rows.length);
    return _rows.skip(skip).toList();
  }

  void _publishRows() => widget.rowController?.showRows(_visibleRows,
      target: _target,
      parentId: widget.related.recordId,
      truncated: _truncated,
      unordered: _truncated && _config.isAlongPath && _config.sortFields.isNotEmpty);

  void _reload() => unawaited(_load());

  @override
  void didUpdateWidget(PortalView old) {
    super.didUpdateWidget(old);
    if (old.related.recordId != widget.related.recordId ||
        old.related.fromTable != widget.related.fromTable ||
        old.object.id != widget.object.id ||
        old.refreshGeneration != widget.refreshGeneration ||
        // The record in hand was written: a changed match field relates
        // other records.
        old.related.revision != widget.related.revision ||
        old.object.portalConfig.toString() != widget.object.portalConfig.toString()) {
      unawaited(_load());
    }
    if (old.rowController != widget.rowController) {
      _detachController(old.rowController);
      _attachController(widget.rowController);
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _publishRows();
      });
    }
  }

  @override
  void dispose() {
    _detachController(widget.rowController);
    _access.removeListener(_reload);
    for (final f in _focusNodes.values) {
      f.dispose();
    }
    _scroll.dispose();
    for (final t in _debounce.values) {
      t?.cancel();
    }
    for (final c in _controllers.values) {
      c.dispose();
    }
    _newRowController.dispose();
    super.dispose();
  }

  RelatedTarget? get _target => _config.isAlongPath
      ? widget.related.alongPath(_config.occurrencePath)
      : widget.related.resolve(_config.relationshipId, _config.occurrence, side: _config.relationshipSide);

  Future<void> _load() async {
    final generation = ++_generation;
    bool stale() => !mounted || generation != _generation;
    final recordId = widget.related.recordId;
    final target = _target;
    if (recordId == null || target == null) {
      _truncated = false;
      if (mounted) setState(() => _rows = const []);
      _publishRows();
      return;
    }

    widget.rowController?.loading();
    setState(() {
      _loading = true;
      _error = null;
      _problem = null;
    });
    try {
      final table = target.isExternal ? await _access.table(target.occurrence) : null;
      if (stale()) return;
      if (table != null) _externalTable = table;
      final page = await widget.related.rowsPage(target, sort: _config.sortFields, limit: ApiClient.maxPageSize);
      if (stale()) return;
      var rows = page.records;
      _truncated = page.more;
      if (_config.filter.trim().isNotEmpty && rows.isNotEmpty) {
        rows = await _filtered(target, rows, _config.filter.trim());
        if (stale()) return;
      }
      setState(() {
        _rows = rows;
        _loading = false;
      });
      _syncControllers();
      _publishRows();
    } catch (e) {
      if (stale()) return;
      final problem = ExternalUnavailable.of(e);
      _truncated = false;
      setState(() {
        _rows = const [];
        _loading = false;
        _problem = problem;
        _error = problem == null ? e.toString() : null;
      });
      _publishRows();
      if (problem != null) _access.askOnce(context, problem);
    }
  }

  /// The [rows] [filter] is true for, evaluated against each related
  /// record in the layout's record ([RelatedContext.rowContext]), as
  /// FileMaker filters portal records: a field of the layout reads the
  /// record in hand. Another solution's records are read there. A filter
  /// that cannot be evaluated throws, so the portal shows none of them
  /// rather than all.
  Future<List<Map<String, dynamic>>> _filtered(
      RelatedTarget target, List<Map<String, dynamic>> rows, String filter) async {
    if (filter == '0') return const [];
    final ids = [
      for (final r in rows)
        if ((r['id']?.toString() ?? '').isNotEmpty) r['id'].toString(),
    ];
    final keep = <String>{};
    final client = widget.related.clientFor(target);
    for (var i = 0; i < ids.length; i += ApiClient.maxEvaluatedRecords) {
      final chunk = ids.sublist(i, math.min(i + ApiClient.maxEvaluatedRecords, ids.length));
      final results = await client.evaluateForRecords(target.table.name,
          expressions: [filter],
          recordIds: chunk,
          occurrence: target.occurrence.id,
          resultType: 'Number',
          context: widget.related.rowContext);
      for (final id in chunk) {
        final answer = results[id];
        if (answer == null || answer.isEmpty) continue;
        if (answer.first.error.isNotEmpty) throw StateError('the portal filter cannot be evaluated: ${answer.first.error}');
        if (isLayoutConditionTrue(answer.first.text)) keep.add(id);
      }
    }
    return [for (final r in rows) if (keep.contains(r['id']?.toString())) r];
  }

  /// What may be done with the portal's records: for another solution's,
  /// what the account the session holds there may do, read with its fields;
  /// for this solution's, everything, the server deciding.
  RecordPrivileges get _privileges {
    final target = _target;
    if (target == null) return const RecordPrivileges();
    if (!target.isExternal) return RecordPrivileges.all;
    return _externalTable?.privileges ?? const RecordPrivileges(read: true);
  }

  /// Runs a write, signing in to the other solution and trying once more
  /// when the session is not signed in to it.
  Future<T> _write<T>(Future<T> Function() call) => _access.retryingSignIn(context, call);

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
        } else if (existing.text != text && _debounce[key] == null && !(_focusNodes[key]?.hasFocus ?? false)) {
          // Do not pull the text out from under somebody still typing.
          existing.text = text;
        }
        _focusNodes.putIfAbsent(key, FocusNode.new);
      }
    }
    for (final key in _controllers.keys.toList()) {
      if (!live.contains(key)) {
        _debounce.remove(key)?.cancel();
        _controllers.remove(key)?.dispose();
        _focusNodes.remove(key)?.dispose();
        _saving.remove(key);
      }
    }
  }

  /// The field objects of the rows a user can type into, in the order of
  /// the layout (left to right, top to bottom).
  List<LayoutObjectModel> _enterableFields() {
    final target = _target;
    if (target == null || !_privileges.edit) return const [];
    final fields = [
      for (final o in widget.contents)
        if (o.type == 'field' && o.fieldBinding != null)
          if (_column(target, o.fieldBinding!.fieldName) case final col?)
            if (!col.isPrimaryKey && !isComputedColumn(col)) o,
    ]..sort((a, b) => a.y != b.y ? a.y.compareTo(b.y) : a.x.compareTo(b.x));
    return fields;
  }

  /// Go to Portal Row: brings row [row] into view and puts the focus in its
  /// box of [objectId].
  Future<TriggerBox?> _focusCell(int row, String objectId) async {
    final target = _target;
    final visible = _visibleRows;
    if (target == null || row < 1 || row > visible.length) return null;
    final content = widget.contents.where((o) => o.id == objectId).firstOrNull;
    final field = content?.fieldBinding?.fieldName;
    if (content == null || field == null) return null;
    final rowId = visible[row - 1]['id']?.toString() ?? '';
    final key = _cellKey(rowId, field);
    final node = _focusNodes[key];
    if (node == null) return null;
    if (node.context == null && _scroll.hasClients) {
      // The row is not built: bring it into view first.
      final rowHeight = _config.effectiveRowHeight(widget.object.height);
      _scroll.jumpTo(((row - 1) * rowHeight).clamp(0.0, _scroll.position.maxScrollExtent));
      SchedulerBinding.instance.scheduleFrame();
      await SchedulerBinding.instance.endOfFrame;
    }
    if (!mounted || node.context == null) return null;
    final box = _cellBox(content, rowId, row, field);
    node.requestFocus();
    return box;
  }

  /// A field box of a portal row, as the window's script triggers see it.
  TriggerBox _cellBox(LayoutObjectModel content, String rowId, int rowNumber, String field) {
    final key = _cellKey(rowId, field);
    return TriggerBox(
      object: content,
      key: field,
      portal: widget.object,
      rowNumber: rowNumber,
      rowId: rowId,
      modified: () {
        final target = _target;
        final controller = _controllers[key];
        final row = _rows.where((r) => r['id']?.toString() == rowId).firstOrNull;
        final col = target == null ? null : _column(target, field);
        if (controller == null || row == null || col == null) return false;
        return controller.text != widget.related.formatValue(col, row[col.name]);
      },
      save: () async {
        final controller = _controllers[key];
        if (controller == null) return true;
        _debounce.remove(key)?.cancel();
        return _saveCell(rowId, field, controller.text);
      },
      focus: () => _focusNodes[key]?.requestFocus(),
      hasFocus: () => _focusNodes[key]?.hasFocus ?? false,
    );
  }

  Future<Map<String, dynamic>> _writeRow(String rowId, Map<String, dynamic> values, {bool script = false}) async {
    final target = _target;
    if (target == null) throw StateError('The portal shows no related records.');
    final updated =
        await _write(() => widget.related.clientFor(target).updateRow(target.table.name, rowId, values, script: script));
    if (mounted) {
      setState(() {
        final i = _rows.indexWhere((r) => r['id']?.toString() == rowId);
        if (i >= 0) {
          final rows = List<Map<String, dynamic>>.from(_rows);
          rows[i] = updated;
          _rows = rows;
        }
      });
      // The boxes of the fields written show what was written, even the
      // one being typed in.
      for (final field in values.keys) {
        final col = _column(target, field);
        final controller = _controllers[_cellKey(rowId, col?.name ?? field)];
        if (col != null && controller != null) controller.text = widget.related.formatValue(col, updated[col.name]);
      }
      _syncControllers();
      _publishRows();
    }
    return updated;
  }

  /// Delete Portal Row: the related record [rowId], after asking when
  /// [confirm]. 0 once deleted, 1 when the user cancelled, 101 when it could
  /// not be.
  Future<int> _deleteRowCode(String rowId, {required bool confirm}) async {
    if (!_privileges.delete) return 200;
    if (confirm) {
      final ok = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Delete Portal Row'),
          content: const Text('Permanently delete this related record?'),
          actions: [
            TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancel')),
            FilledButton(
              style: FilledButton.styleFrom(backgroundColor: Colors.red),
              onPressed: () => Navigator.of(ctx).pop(true),
              child: const Text('Delete'),
            ),
          ],
        ),
      );
      if (ok != true) return 1;
    }
    return await _deleteRow(rowId) ? 0 : 101;
  }

  ColumnModel? _column(RelatedTarget target, String field) => (_externalTable ?? target.table).columns
      .where((c) => c.name.toLowerCase() == field.toLowerCase())
      .firstOrNull;

  /// The column of the row's record [content] shows: none for an object
  /// other than a field, or for a field of another occurrence than the
  /// portal's, even when one has its name.
  ColumnModel? _rowColumn(RelatedTarget target, LayoutObjectModel content) {
    if (content.type != 'field') return null;
    final occurrence = content.fieldBinding?.tableOccurrence ?? '';
    final ofRow = occurrence.isEmpty ||
        occurrence == target.occurrence.id ||
        occurrence.toLowerCase() == target.occurrence.name.toLowerCase();
    return ofRow ? _column(target, content.fieldBinding?.fieldName ?? '') : null;
  }

  /// The calculations of the row objects' conditions, by the key their
  /// answer is kept under, each with its Self: each object's hide
  /// condition, by the object's id, and each formula of the conditional
  /// formats of a field, a text or a button, by `<object id>#<index of the
  /// rule>`. Self is the field a field object shows of the row's record, or
  /// the text a text or a button shows.
  List<(String, String, ConditionSelf?)> _rowConditions(RelatedTarget target) {
    // The target and the objects are made anew on each build: they are
    // told apart by what the conditions depend on.
    final of = Object.hash(
      target.table.name,
      target.occurrence.id,
      Object.hashAll([
        for (final content in widget.contents)
          if ((content.hideWhen ?? '').trim().isNotEmpty || content.style.conditionalRules.any((r) => r.isFormula))
            Object.hash(content.id, content.type, content.text, content.hideWhen, content.fieldBinding?.fieldName,
                content.fieldBinding?.tableOccurrence,
                Object.hashAll([for (final rule in content.style.conditionalRules) rule.isFormula ? rule.value : null])),
      ]),
    );
    if (of != _rowConditionsOf) {
      _rowConditionsOf = of;
      _rowConditionsList = [
        for (final content in widget.contents) ...[
          if ((content.hideWhen ?? '').trim().isNotEmpty) (content.id, content.hideWhen!, _rowSelf(target, content)),
          // A field of another occurrence than the row's is not drawn.
          if (layoutObjectShowsText(content) || _rowColumn(target, content) != null)
            for (final (i, rule) in content.style.conditionalRules.indexed)
              if (rule.isFormula && rule.value.trim().isNotEmpty) ('${content.id}#$i', rule.value, _rowSelf(target, content)),
        ],
      ];
      _rowHideable = {
        for (final content in widget.contents)
          if ((content.hideWhen ?? '').trim().isNotEmpty) content.id,
      };
      // Answers to other conditions do not hold for these.
      _rowTruths.clear();
      _lastRowTruths.clear();
    }
    return _rowConditionsList;
  }

  /// What Self reads in the conditions of [content] on a row: the field of
  /// the row's record it shows, or the text a text or a button shows. Null,
  /// Self unknown, for anything else, and for a text merging a field.
  ConditionSelf? _rowSelf(RelatedTarget target, LayoutObjectModel content) {
    if (_rowColumn(target, content) case final col?) return ConditionSelf(col.name);
    if (layoutTextSelf(content) case final text?) return ConditionSelf.text(text);
    return null;
  }

  /// What [_rowConditions] last answered, and for which conditions.
  int? _rowConditionsOf;
  List<(String, String, ConditionSelf?)> _rowConditionsList = const [];

  /// The row objects with a hide condition, which a row hides until its
  /// answer is in, so nothing hidden is ever shown or typed into.
  Set<String> _rowHideable = const {};

  /// The conditions true on [row], evaluated against the row's record in
  /// the layout's record ([RelatedContext.rowContext]), as FileMaker
  /// evaluates those of a portal row: a field of the layout reads the
  /// record in hand, one of the portal's occurrence the row. They are the
  /// ids of the objects it hides, and the keys of the formats that
  /// apply. Asked while the row is drawn, so only the rows on screen are
  /// evaluated. Until the row's answer is in, its last one holds, and a
  /// row seen for the first time hides every object with a condition.
  /// The conditions are those [_rowList] last read, once for every row.
  Set<String> _truthsOn(Map<String, dynamic> row) {
    final rowId = row['id']?.toString() ?? '';
    if (rowId.isEmpty || _rowConditionsList.isEmpty) return const {};
    final values = Object.hashAllUnordered(row.entries.map((e) => Object.hash(e.key, e.value?.toString())));
    final key = '$_generation|$rowId|$values';
    final known = _rowTruths[key];
    if (known != null) return known;
    if (_rowPending.add(key)) {
      if (_rowQueue.isEmpty) scheduleMicrotask(() => unawaited(_evaluateRows()));
      _rowQueue[key] = rowId;
    }
    return _lastRowTruths[rowId] ?? _rowHideable;
  }

  /// Evaluates the rows queued, in as few requests as the endpoint allows.
  Future<void> _evaluateRows() async {
    final queued = Map.of(_rowQueue);
    _rowQueue.clear();
    final target = _target;
    final conditions = target == null ? const <(String, String, ConditionSelf?)>[] : _rowConditions(target);
    final ids = queued.values.toSet().toList();
    final truths = {for (final id in ids) id: <String>{}};
    if (target != null && conditions.isNotEmpty) {
      final client = widget.related.clientFor(target);
      for (var r = 0; r < ids.length; r += ApiClient.maxEvaluatedRecords) {
        final chunkIds = ids.sublist(r, math.min(r + ApiClient.maxEvaluatedRecords, ids.length));
        for (var i = 0; i < conditions.length; i += 50) {
          final chunk = conditions.sublist(i, math.min(i + 50, conditions.length));
          try {
            final results = await client.evaluateForRecords(
              target.table.name,
              expressions: [for (final (_, expression, _) in chunk) expression],
              self: [for (final (_, _, self) in chunk) self],
              recordIds: chunkIds,
              occurrence: target.occurrence.id,
              resultType: 'Number',
              context: widget.related.rowContext,
            );
            for (final id in chunkIds) {
              for (final (j, result) in (results[id] ?? const <EvaluationModel>[]).indexed) {
                if (j >= chunk.length) continue;
                final key = chunk[j].$1;
                // A hide condition that cannot be evaluated hides its
                // object, as in FileMaker; a format's applies nothing.
                if (result.error.isEmpty ? isLayoutConditionTrue(result.text) : _rowHideable.contains(key)) {
                  truths[id]!.add(key);
                }
              }
            }
          } on Exception catch (_) {
            for (final id in chunkIds) {
              truths[id]!.addAll([for (final (key, _, _) in chunk) if (_rowHideable.contains(key)) key]);
            }
          }
          if (!mounted) return;
        }
      }
    }
    if (!mounted) return;
    if (_rowTruths.length > 2000) _rowTruths.clear();
    setState(() {
      for (final e in queued.entries) {
        _rowPending.remove(e.key);
        _rowTruths[e.key] = truths[e.value]!;
        _lastRowTruths[e.value] = truths[e.value]!;
      }
    });
  }

  void _onCellChanged(LayoutObjectModel content, String rowId, String field, String value) {
    final key = _cellKey(rowId, field);
    final controller = widget.rowController;
    controller?.onCellModified?.call(content);
    // A field whose triggers validate and save it is saved when it is left.
    if (controller?.guarded?.call(content) ?? false) return;
    _debounce[key]?.cancel();
    _debounce[key] = Timer(const Duration(milliseconds: 600), () {
      _debounce[key] = null;
      unawaited(_saveCell(rowId, field, value));
    });
  }

  /// Saves one cell; false when it could not be saved.
  Future<bool> _saveCell(String rowId, String field, String value) async {
    final target = _target;
    if (target == null) return true;
    final key = _cellKey(rowId, field);
    setState(() => _saving[key] = true);
    try {
      final updated = await _write(() => widget.related.clientFor(target).updateRow(target.table.name, rowId, {field: value}));
      if (!mounted) return true;
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
      _publishRows();
      return true;
    } catch (e) {
      if (!mounted) return false;
      setState(() => _saving[key] = false);
      _report('The related record could not be saved: $e');
      return false;
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
      await _write(() => widget.related.apiClient.createRelatedRow(
            widget.related.fromTable,
            recordId,
            relationshipId: target.relationship.id,
            occurrence: target.occurrence.id,
            side: target.requestSide,
            values: value.isEmpty ? const {} : {field: value},
          ));
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

  Future<bool> _deleteRow(String rowId) async {
    final target = _target;
    if (target == null) return false;
    try {
      await _write(() => widget.related.clientFor(target).deleteRow(target.table.name, rowId));
      if (!mounted) return true;
      await _load();
      widget.onChanged?.call();
      return true;
    } catch (e) {
      if (!mounted) return false;
      _report('The related record could not be deleted: $e');
      return false;
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
        _config.isAlongPath
            ? 'Portal: the occurrences ${_config.occurrencePath.join(' → ')} are not all there'
            : 'Portal: the relationship it shows is no longer there',
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
    final visible = _visibleRows;
    // The rows' conditions, read once for all of them.
    _rowConditions(target);

    // Each row is drawn, and its conditional formats asked for, only once
    // it scrolls into view.
    final children = <Widget>[
      for (final (i, row) in visible.indexed)
        Builder(builder: (_) => _row(target, row, rowHeight, striped: i.isOdd, number: i + 1)),
    ];
    // The empty last row is offered only where the relationship allows
    // creating the records it would create: on the side the portal shows.
    if (_config.allowCreation && target.allowsCreation && _privileges.create) {
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
      controller: _scroll,
      primary: false,
      padding: EdgeInsets.zero,
      children: children,
    );

    return _config.showScrollBar ? Scrollbar(child: list) : list;
  }

  Widget _row(RelatedTarget target, Map<String, dynamic> row, double rowHeight,
      {required bool striped, required int number}) {
    final rowId = row['id']?.toString() ?? '';
    final controller = widget.rowController;
    final active = controller != null && controller.activeRow == number;
    // The row's own answers: the objects it hides, the formats that apply.
    final truths = _truthsOn(row);
    final container = Container(
      key: ValueKey('portal-row-$rowId'),
      height: rowHeight,
      decoration: BoxDecoration(
        color: active
            ? Theme.of(context).colorScheme.primary.withValues(alpha: 0.12)
            : striped
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
            if (!truths.contains(content.id))
              Positioned(
                left: content.x - widget.object.x,
                top: content.y - widget.object.y,
                width: content.width,
                height: content.height,
                child: _cell(target, content, rowId, row, number, truths),
              ),
          if (_config.allowDeletion && _privileges.delete && rowId.isNotEmpty)
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
    if (controller == null) return container;
    // A click anywhere in the row makes it the active one (#26).
    return Listener(
      behavior: HitTestBehavior.translucent,
      onPointerDown: (_) {
        controller.activate(number);
        controller.onActivated?.call();
      },
      child: container,
    );
  }

  /// A cell of a portal row: a field of the related record, or any other
  /// object drawn inside the portal, which repeats, with the conditional
  /// formats [truths] says apply on the row.
  Widget _cell(RelatedTarget target, LayoutObjectModel content, String rowId,
      Map<String, dynamic> row, int number, Set<String> truths) {
    bool formulaTrue(int i) => truths.contains('${content.id}#$i');
    if (content.type != 'field') {
      return _staticContent(content, content.style.conditionalLook(content.text, formulaTrue: formulaTrue));
    }

    final field = content.fieldBinding?.fieldName ?? '';
    final col = _rowColumn(target, content);
    if (col == null) {
      final occurrence = content.fieldBinding?.tableOccurrence ?? '';
      final ofRow = occurrence.isEmpty ||
          occurrence == target.occurrence.id ||
          occurrence.toLowerCase() == target.occurrence.name.toLowerCase();
      final name = ofRow ? field : (content.fieldBinding?.qualifiedName ?? field);
      return relatedNotice('<$name>', isDark: widget.isDark, radius: content.style.cornerRadius);
    }

    final text = widget.related.formatValue(col, row[col.name]);
    final look = content.style.conditionalLook(row[col.name], formulaTrue: formulaTrue);
    final fill = parseLayoutColor(look?.fillColor);
    if (col.isPrimaryKey || isComputedColumn(col) || !_privileges.edit) {
      return Container(
        alignment: Alignment.centerLeft,
        padding: const EdgeInsets.symmetric(horizontal: 6),
        color: fill,
        child: Text(
          text,
          overflow: TextOverflow.ellipsis,
          style: conditionalTextStyle(
            TextStyle(
              fontSize: content.style.fontSize > 0 ? content.style.fontSize : 12,
              fontFamily: col.isPrimaryKey ? 'monospace' : null,
              color: Colors.grey.shade600,
            ),
            look,
          ),
        ),
      );
    }

    final key = _cellKey(rowId, field);
    final controller = _controllers[key] ?? TextEditingController(text: text);
    _controllers[key] = controller;
    final focusNode = _focusNodes.putIfAbsent(key, FocusNode.new);
    final rows = widget.rowController;
    final guarded = rows?.guarded?.call(content) ?? false;

    final textField = TextField(
      key: ValueKey('portal-cell-$rowId-$field'),
      controller: controller,
      focusNode: focusNode,
      style: conditionalTextStyle(TextStyle(fontSize: content.style.fontSize > 0 ? content.style.fontSize : 12), look),
      textAlign: _textAlign(content.style.textAlign),
      decoration: InputDecoration(
        isDense: true,
        contentPadding: const EdgeInsets.symmetric(horizontal: 6, vertical: 6),
        border: const OutlineInputBorder(borderSide: BorderSide.none),
        fillColor: fill,
        filled: fill != null,
        suffixIcon: _saving[key] == true
            ? const Padding(
                padding: EdgeInsets.all(6),
                child: SizedBox(width: 10, height: 10, child: CircularProgressIndicator(strokeWidth: 1.5)),
              )
            : _saving[key] == false
                ? const Icon(Icons.error_outline, size: 13, color: Colors.red)
                : null,
      ),
      onChanged: (v) => _onCellChanged(content, rowId, field, v),
      onEditingComplete: () {
        _debounce[key]?.cancel();
        _debounce[key] = null;
        // A field its triggers validate and save is saved as it is left.
        if (guarded) {
          focusNode.unfocus();
          return;
        }
        unawaited(_saveCell(rowId, field, controller.text));
      },
    );
    if (rows == null) return textField;
    return Focus(
      canRequestFocus: false,
      skipTraversal: true,
      onFocusChange: (focused) => rows.onCellFocus?.call(_cellBox(content, rowId, number, field), focused),
      child: textField,
    );
  }

  /// An object of a portal row other than a field, with the conditional
  /// formats that apply ([look]).
  Widget _staticContent(LayoutObjectModel content, ConditionalFormatRule? look) {
    if (content.type == 'label') {
      return Container(
        alignment: Alignment.centerLeft,
        padding: const EdgeInsets.symmetric(horizontal: 4),
        color: parseLayoutColor(look?.fillColor),
        child: Text(
          content.text,
          overflow: TextOverflow.ellipsis,
          style: conditionalTextStyle(
            TextStyle(
              fontSize: content.style.fontSize > 0 ? content.style.fontSize : 12,
              fontWeight: content.style.fontWeight == 'bold' ? FontWeight.bold : FontWeight.normal,
            ),
            look,
          ),
        ),
      );
    }
    if (content.type == 'button' || content.type == 'popover_button') {
      // Drawn as on the layout. What it runs would need the row's record
      // as the current one, which a script does not have here yet.
      final fill = parseLayoutColor(look?.fillColor ?? content.style.fillColor);
      final line = parseLayoutColor(content.style.borderColor);
      return InkWell(
        onTap: () => _report('A button in a portal row does not run yet.'),
        child: Container(
          alignment: Alignment.center,
          decoration: BoxDecoration(
            color: fill,
            border: line == null || content.style.borderWidth <= 0
                ? null
                : Border.all(color: line, width: content.style.borderWidth),
            borderRadius: BorderRadius.circular(content.style.cornerRadius),
          ),
          child: content.text.isEmpty
              ? null
              : Text(content.text,
                  overflow: TextOverflow.ellipsis, style: conditionalTextStyle(layoutTextStyle(content.style), look)),
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
