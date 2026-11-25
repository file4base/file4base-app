import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter/material.dart';

/// What *If no exact match, then* does, as `looked_up_no_match` keeps it.
abstract final class LookupNoMatch {
  static const doNotCopy = 'do_not_copy';
  static const copyNextLower = 'copy_next_lower';
  static const copyNextHigher = 'copy_next_higher';
  static const useConstant = 'use_constant';

  static const labels = <String, String>{
    doNotCopy: 'do not copy',
    copyNextLower: 'copy next lower value',
    copyNextHigher: 'copy next higher value',
    useConstant: 'use',
  };
}

/// A field's *Looked-up value* option: the value of [field] of the first
/// record related through [occurrence], reached from [context], an
/// occurrence of the field's own table (empty: the default one).
@immutable
class LookupSpec {
  const LookupSpec({
    this.context = '',
    this.occurrence = '',
    this.field = '',
    this.dontCopyIfEmpty = false,
    this.noMatch = LookupNoMatch.doNotCopy,
    this.noMatchValue = '',
  });

  /// Reads the `looked_up_*` keys of a field's options, and the
  /// `Occurrence::Field` text older clients kept in `looked_up_target`.
  factory LookupSpec.fromOptions(Map<String, dynamic> options) {
    var occurrence = options['looked_up_occurrence']?.toString().trim() ?? '';
    var field = options['looked_up_field']?.toString().trim() ?? '';
    final legacy = options['looked_up_target']?.toString() ?? '';
    if (occurrence.isEmpty && field.isEmpty && legacy.contains('::')) {
      final at = legacy.indexOf('::');
      occurrence = legacy.substring(0, at).trim();
      field = legacy.substring(at + 2).trim();
    }
    final noMatch = options['looked_up_no_match']?.toString() ?? '';
    return LookupSpec(
      context: options['looked_up_context']?.toString().trim() ?? '',
      occurrence: occurrence,
      field: field,
      dontCopyIfEmpty: options['looked_up_dont_copy_if_empty'] == true,
      noMatch: LookupNoMatch.labels.containsKey(noMatch) ? noMatch : LookupNoMatch.doNotCopy,
      noMatchValue: options['looked_up_no_match_value']?.toString() ?? '',
    );
  }

  final String context;
  final String occurrence;
  final String field;
  final bool dontCopyIfEmpty;
  final String noMatch;
  final String noMatchValue;

  /// Whether it names what to copy, which the server requires of an enabled
  /// lookup.
  bool get isComplete => occurrence.isNotEmpty && field.isNotEmpty;

  /// The `looked_up_*` keys, always all of them.
  Map<String, dynamic> toOptions() => {
        'looked_up_context': context,
        'looked_up_occurrence': occurrence,
        'looked_up_field': field,
        'looked_up_dont_copy_if_empty': dontCopyIfEmpty,
        'looked_up_no_match': noMatch,
        'looked_up_no_match_value': noMatch == LookupNoMatch.useConstant ? noMatchValue : '',
      };
}

/// The occurrence a field's formula or lookup starts from when its options
/// name none: the occurrence of [tableId] named like the table, else its
/// first. Null when the table has none.
TableOccurrenceModel? defaultOccurrenceOf(
  String tableId,
  String tableName,
  List<TableOccurrenceModel> occurrences,
) {
  final own = occurrences.where((o) => o.baseTableId == tableId).toList();
  if (own.isEmpty) return null;
  return own.where((o) => o.name.toLowerCase() == tableName.toLowerCase()).firstOrNull ?? own.first;
}

/// The occurrences the relationship graph reaches from [startId], in either
/// direction and through any number of relationships, nearest first; the
/// start itself is not one of them.
List<TableOccurrenceModel> reachableOccurrences(
  String startId,
  List<TableOccurrenceModel> occurrences,
  List<RelationshipModel> relationships,
) {
  final byId = {for (final o in occurrences) o.id: o};
  final seen = <String>{startId};
  final queue = <String>[startId];
  final reached = <TableOccurrenceModel>[];
  while (queue.isNotEmpty) {
    final at = queue.removeAt(0);
    for (final r in relationships) {
      final next = r.leftOccurrenceId == at
          ? r.rightOccurrenceId
          : r.rightOccurrenceId == at
              ? r.leftOccurrenceId
              : null;
      if (next == null || !seen.add(next)) continue;
      final occ = byId[next];
      if (occ == null) continue;
      reached.add(occ);
      queue.add(next);
    }
  }
  return reached;
}

/// The *Lookup for Field* dialog of a field's Auto-Enter options, as
/// FileMaker's: the occurrence of this table to start from, the related
/// occurrence and field to copy from, and what to do when nothing matches.
class LookupOptionsDialog extends StatefulWidget {
  const LookupOptionsDialog({
    required this.fieldName,
    required this.tableId,
    required this.tableName,
    required this.occurrences,
    required this.relationships,
    required this.loadFields,
    this.initial = const LookupSpec(),
    super.key,
  });

  final String fieldName;
  final String tableId;
  final String tableName;
  final List<TableOccurrenceModel> occurrences;
  final List<RelationshipModel> relationships;

  /// The fields of an occurrence's table, which for an occurrence of another
  /// solution's table only the server knows.
  final Future<List<ColumnModel>> Function(TableOccurrenceModel occurrence) loadFields;
  final LookupSpec initial;

  static Future<LookupSpec?> show(
    BuildContext context, {
    required String fieldName,
    required String tableId,
    required String tableName,
    required List<TableOccurrenceModel> occurrences,
    required List<RelationshipModel> relationships,
    required Future<List<ColumnModel>> Function(TableOccurrenceModel occurrence) loadFields,
    LookupSpec initial = const LookupSpec(),
  }) {
    return showDialog<LookupSpec>(
      context: context,
      builder: (ctx) => LookupOptionsDialog(
        fieldName: fieldName,
        tableId: tableId,
        tableName: tableName,
        occurrences: occurrences,
        relationships: relationships,
        loadFields: loadFields,
        initial: initial,
      ),
    );
  }

  @override
  State<LookupOptionsDialog> createState() => _LookupOptionsDialogState();
}

class _LookupOptionsDialogState extends State<LookupOptionsDialog> {
  late String _start;
  late String _occurrence;
  late String _field;
  late bool _dontCopyIfEmpty;
  late String _noMatch;
  late final TextEditingController _constant;

  final Map<String, Future<List<ColumnModel>>> _fields = {};

  String get _defaultStart =>
      defaultOccurrenceOf(widget.tableId, widget.tableName, widget.occurrences)?.name ?? '';

  List<TableOccurrenceModel> get _starts =>
      widget.occurrences.where((o) => o.baseTableId == widget.tableId).toList();

  TableOccurrenceModel? get _startOccurrence =>
      _starts.where((o) => o.name == _start).firstOrNull;

  List<TableOccurrenceModel> get _related {
    final start = _startOccurrence;
    if (start == null) return const [];
    return reachableOccurrences(start.id, widget.occurrences, widget.relationships);
  }

  TableOccurrenceModel? get _source => _related.where((o) => o.name == _occurrence).firstOrNull;

  @override
  void initState() {
    super.initState();
    final initial = widget.initial;
    _start = initial.context.isNotEmpty ? initial.context : _defaultStart;
    _occurrence = initial.occurrence;
    _field = initial.field;
    _dontCopyIfEmpty = initial.dontCopyIfEmpty;
    _noMatch = initial.noMatch;
    _constant = TextEditingController(text: initial.noMatchValue);
  }

  @override
  void dispose() {
    _constant.dispose();
    super.dispose();
  }

  Future<List<ColumnModel>> _fieldsOf(TableOccurrenceModel occurrence) =>
      _fields.putIfAbsent(occurrence.id, () => widget.loadFields(occurrence));

  void _ok() {
    // The default start is kept as "the default", so renaming the table's
    // occurrences does not leave the lookup naming one that is gone.
    final start = widget.initial.context.isEmpty && _start == _defaultStart ? '' : _start;
    Navigator.of(context).pop(LookupSpec(
      context: start,
      occurrence: _occurrence,
      field: _field,
      dontCopyIfEmpty: _dontCopyIfEmpty,
      noMatch: _noMatch,
      noMatchValue: _constant.text,
    ));
  }

  @override
  Widget build(BuildContext context) {
    final starts = _starts;
    final related = _related;
    final source = _source;
    const label = TextStyle(fontSize: 12, fontWeight: FontWeight.w500);
    // A start or a related occurrence the options name but the graph no
    // longer has is still offered, so it is seen rather than silently lost.
    final startNames = {for (final o in starts) o.name, if (_start.isNotEmpty) _start};
    final relatedNames = {for (final o in related) o.name, if (_occurrence.isNotEmpty) _occurrence};

    return AlertDialog(
      title: Text('Lookup for Field "${widget.fieldName}"', style: const TextStyle(fontSize: 15)),
      content: SizedBox(
        width: 520,
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Row(
                children: [
                  const SizedBox(width: 180, child: Text('Starting with table:', style: label)),
                  Expanded(
                    child: DropdownButton<String>(
                      key: const ValueKey('lookup-start'),
                      isExpanded: true,
                      value: _start.isEmpty ? null : _start,
                      hint: const Text('No occurrence of this table', style: TextStyle(fontSize: 12)),
                      items: [
                        for (final name in startNames)
                          DropdownMenuItem(value: name, child: Text(name, style: const TextStyle(fontSize: 12))),
                      ],
                      onChanged: (v) => setState(() {
                        _start = v ?? _start;
                        if (!_related.any((o) => o.name == _occurrence)) {
                          _occurrence = '';
                          _field = '';
                        }
                      }),
                    ),
                  ),
                ],
              ),
              Row(
                children: [
                  const SizedBox(width: 180, child: Text('Lookup from related table:', style: label)),
                  Expanded(
                    child: DropdownButton<String>(
                      key: const ValueKey('lookup-occurrence'),
                      isExpanded: true,
                      value: _occurrence.isEmpty ? null : _occurrence,
                      hint: Text(
                        related.isEmpty ? 'No table occurrence is related to "$_start"' : 'Choose an occurrence',
                        style: const TextStyle(fontSize: 12),
                      ),
                      items: [
                        for (final name in relatedNames)
                          DropdownMenuItem(value: name, child: Text(name, style: const TextStyle(fontSize: 12))),
                      ],
                      onChanged: (v) => setState(() {
                        if (v != _occurrence) _field = '';
                        _occurrence = v ?? '';
                      }),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              const Text('Copy value from field:', style: label),
              const SizedBox(height: 4),
              Container(
                height: 150,
                decoration: BoxDecoration(
                  border: Border.all(color: Colors.grey.withValues(alpha: 0.5)),
                  borderRadius: BorderRadius.circular(4),
                ),
                child: source == null
                    ? Center(
                        child: Text(
                          _occurrence.isEmpty
                              ? 'Choose the related table first.'
                              : '"$_occurrence" is not related to "$_start".',
                          style: const TextStyle(fontSize: 12, color: Colors.grey),
                        ),
                      )
                    : FutureBuilder<List<ColumnModel>>(
                        future: _fieldsOf(source),
                        builder: (context, snapshot) {
                          if (snapshot.hasError) {
                            return Center(
                              child: Text('Cannot read the fields of "${source.name}".',
                                  style: const TextStyle(fontSize: 12, color: Colors.red)),
                            );
                          }
                          final fields = snapshot.data;
                          if (fields == null) return const Center(child: CircularProgressIndicator());
                          return ListView(
                            children: [
                              for (final f in fields.where((f) => !f.isPrimaryKey))
                                ListTile(
                                  key: ValueKey('lookup-field-${f.name}'),
                                  dense: true,
                                  visualDensity: VisualDensity.compact,
                                  selected: f.name == _field,
                                  selectedTileColor: Theme.of(context).colorScheme.primary.withValues(alpha: 0.12),
                                  title: Text('${source.name}::${f.displayName}', style: const TextStyle(fontSize: 12)),
                                  trailing: Text(f.fieldType, style: const TextStyle(fontSize: 10, color: Colors.grey)),
                                  onTap: () => setState(() => _field = f.name),
                                ),
                            ],
                          );
                        },
                      ),
              ),
              CheckboxListTile(
                key: const ValueKey('lookup-dont-copy-empty'),
                dense: true,
                contentPadding: EdgeInsets.zero,
                controlAffinity: ListTileControlAffinity.leading,
                title: const Text("Don't copy contents if empty", style: TextStyle(fontSize: 12)),
                value: _dontCopyIfEmpty,
                onChanged: (v) => setState(() => _dontCopyIfEmpty = v ?? false),
              ),
              const Text('If no exact match, then:', style: label),
              RadioGroup<String>(
                groupValue: _noMatch,
                onChanged: (v) => setState(() => _noMatch = v ?? LookupNoMatch.doNotCopy),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    for (final entry in LookupNoMatch.labels.entries)
                      Row(
                        children: [
                          Radio<String>(key: ValueKey('lookup-no-match-${entry.key}'), value: entry.key),
                          Text(entry.value, style: const TextStyle(fontSize: 12)),
                          if (entry.key == LookupNoMatch.useConstant) ...[
                            const SizedBox(width: 8),
                            Expanded(
                              child: SizedBox(
                                height: 30,
                                child: TextField(
                                  key: const ValueKey('lookup-constant'),
                                  controller: _constant,
                                  enabled: _noMatch == LookupNoMatch.useConstant,
                                  style: const TextStyle(fontSize: 12),
                                  decoration: const InputDecoration(
                                    contentPadding: EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                                    border: OutlineInputBorder(),
                                  ),
                                ),
                              ),
                            ),
                          ],
                        ],
                      ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(
          key: const ValueKey('lookup-ok'),
          onPressed: _occurrence.isNotEmpty && _field.isNotEmpty ? _ok : null,
          child: const Text('OK'),
        ),
      ],
    );
  }
}
