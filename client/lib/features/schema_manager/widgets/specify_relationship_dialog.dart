import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter/material.dart';

class SpecifyRelationshipDialog extends StatefulWidget {

  const SpecifyRelationshipDialog({
    required this.occurrences, required this.tables, super.key,
    this.initialRelationship,
    this.preselectedLeftOccurrenceId,
    this.preselectedRightOccurrenceId,
    this.preselectedLeftColumnId,
    this.preselectedRightColumnId,
  });
  final List<TableOccurrenceModel> occurrences;
  final List<TableModel> tables;
  final RelationshipModel? initialRelationship;
  final String? preselectedLeftOccurrenceId;
  final String? preselectedRightOccurrenceId;
  final String? preselectedLeftColumnId;
  final String? preselectedRightColumnId;

  static Future<Map<String, dynamic>?> show(
    BuildContext context, {
    required List<TableOccurrenceModel> occurrences,
    required List<TableModel> tables,
    RelationshipModel? initialRelationship,
    String? preselectedLeftOccurrenceId,
    String? preselectedRightOccurrenceId,
    String? preselectedLeftColumnId,
    String? preselectedRightColumnId,
  }) {
    return showDialog<Map<String, dynamic>>(
      context: context,
      builder: (ctx) => SpecifyRelationshipDialog(
        occurrences: occurrences,
        tables: tables,
        initialRelationship: initialRelationship,
        preselectedLeftOccurrenceId: preselectedLeftOccurrenceId,
        preselectedRightOccurrenceId: preselectedRightOccurrenceId,
        preselectedLeftColumnId: preselectedLeftColumnId,
        preselectedRightColumnId: preselectedRightColumnId,
      ),
    );
  }

  @override
  State<SpecifyRelationshipDialog> createState() => _SpecifyRelationshipDialogState();
}

class _SpecifyRelationshipDialogState extends State<SpecifyRelationshipDialog> {
  TableOccurrenceModel? _leftOccurrence;
  TableOccurrenceModel? _rightOccurrence;
  ColumnModel? _leftColumn;
  ColumnModel? _rightColumn;

  String _operator = '=';

  /// The match criteria, in order. A record is related when it meets all of
  /// them. The field lists and the operator above edit the selected one; Add
  /// appends what they show, Change replaces the selected criterion with it.
  final List<RelationshipPredicateModel> _predicates = [];
  int? _selectedPredicate;

  /// A message about the last Add or Change that could not be applied.
  String? _notice;
  /// How the two sides are named in the option labels, so they say which
  /// table they act on instead of "this table" (#36).
  String get _leftSideName => _occurrenceName(_leftOccurrence, 'the left table');
  String get _rightSideName => _occurrenceName(_rightOccurrence, 'the right table');

  String _occurrenceName(TableOccurrenceModel? occurrence, String fallback) {
    final name = occurrence?.name.trim() ?? '';
    return name.isEmpty ? fallback : '"$name"';
  }

  /// Each side's options, as FileMaker sets them on the side they act on.
  final _left = _SideOptions();
  final _right = _SideOptions();
  late final TextEditingController _nameController;

  final List<String> _operators = ['=', '≠', '<', '≤', '>', '≥', '×'];

  @override
  void initState() {
    super.initState();
    if (widget.initialRelationship != null) {
      final rel = widget.initialRelationship!;
      _leftOccurrence = widget.occurrences.where((o) => o.id == rel.leftOccurrenceId).firstOrNull ??
          (widget.occurrences.isNotEmpty ? widget.occurrences.first : null);
      _rightOccurrence = widget.occurrences.where((o) => o.id == rel.rightOccurrenceId).firstOrNull ??
          (widget.occurrences.length > 1 ? widget.occurrences[1] : widget.occurrences.firstOrNull);

      final leftTable = _getTableForOccurrence(_leftOccurrence);
      _leftColumn = leftTable?.columns.where((c) => c.id == rel.leftColumnId).firstOrNull;

      final rightTable = _getTableForOccurrence(_rightOccurrence);
      _rightColumn = rightTable?.columns.where((c) => c.id == rel.rightColumnId).firstOrNull;

      _operator = rel.operator;
      _predicates.addAll(rel.predicates);
      _selectedPredicate = 0;
      _left.load(allowCreation: rel.leftAllowCreation, cascadeDelete: rel.leftCascadeDelete, sort: rel.leftSortRelated);
      _right.load(allowCreation: rel.allowCreation, cascadeDelete: rel.cascadeDelete, sort: rel.sortRelated);
      _nameController = TextEditingController(text: rel.name);
    } else {
      if (widget.preselectedLeftOccurrenceId != null) {
        _leftOccurrence = widget.occurrences.where((o) => o.id == widget.preselectedLeftOccurrenceId).firstOrNull;
      }
      _leftOccurrence ??= widget.occurrences.isNotEmpty ? widget.occurrences.first : null;

      if (widget.preselectedRightOccurrenceId != null) {
        _rightOccurrence = widget.occurrences.where((o) => o.id == widget.preselectedRightOccurrenceId).firstOrNull;
      }
      _rightOccurrence ??= (widget.occurrences.length > 1 ? widget.occurrences[1] : widget.occurrences.firstOrNull);

      final leftTable = _getTableForOccurrence(_leftOccurrence);
      if (widget.preselectedLeftColumnId != null) {
        _leftColumn = leftTable?.columns.where((c) => c.id == widget.preselectedLeftColumnId).firstOrNull;
      }
      _leftColumn ??= leftTable?.columns.isNotEmpty == true ? leftTable!.columns.first : null;

      final rightTable = _getTableForOccurrence(_rightOccurrence);
      if (widget.preselectedRightColumnId != null) {
        _rightColumn = rightTable?.columns.where((c) => c.id == widget.preselectedRightColumnId).firstOrNull;
      }
      _rightColumn ??= rightTable?.columns.isNotEmpty == true ? rightTable!.columns.first : null;

      final first = _editingCriterion;
      if (first != null && _selfMatchMessage(first) == null) {
        _predicates.add(first);
        _selectedPredicate = 0;
      }

      final defaultName = _generateName();
      _nameController = TextEditingController(text: defaultName);
    }
  }

  @override
  void dispose() {
    _nameController.dispose();
    super.dispose();
  }

  TableModel? _getTableForOccurrence(TableOccurrenceModel? occ) {
    if (occ == null) return null;
    return widget.tables.where((t) => t.id == occ.baseTableId).firstOrNull;
  }

  String _generateName() {
    final lName = _leftOccurrence?.name ?? 'left';
    final rName = _rightOccurrence?.name ?? 'right';
    return '${lName}_to_$rName';
  }

  bool get _isEditing => widget.initialRelationship != null;

  /// Whether a side is an occurrence of another solution's table. Its fields
  /// are not in this solution's catalog, so its criteria are kept as they
  /// are, by field name, rather than edited from field lists.
  bool get _hasExternalSide => (_leftOccurrence?.isExternal ?? false) || (_rightOccurrence?.isExternal ?? false);

  /// The criterion the field lists and the operator currently show.
  RelationshipPredicateModel? get _editingCriterion {
    if (_leftColumn == null || _rightColumn == null) return null;
    return RelationshipPredicateModel(
      leftColumnId: _leftColumn!.id,
      rightColumnId: _rightColumn!.id,
      operator: _operator,
    );
  }

  String? _selfMatchMessage(RelationshipPredicateModel criterion) {
    if (_leftOccurrence != null &&
        _leftOccurrence!.id == _rightOccurrence?.id &&
        criterion.leftColumnId == criterion.rightColumnId) {
      return 'A field cannot be matched with itself.';
    }
    return null;
  }

  /// Whether the field lists show something other than the selected
  /// criterion, which Change (or OK) would apply.
  bool get _hasPendingChange {
    if (_hasExternalSide) return false;
    final editing = _editingCriterion;
    final selected = _selectedPredicate;
    return editing != null && selected != null && _predicates[selected] != editing;
  }

  void _selectPredicate(int index) {
    final criterion = _predicates[index];
    setState(() {
      _selectedPredicate = index;
      _notice = null;
      _leftColumn = _getTableForOccurrence(_leftOccurrence)
              ?.columns
              .where((c) => c.id == criterion.leftColumnId)
              .firstOrNull ??
          _leftColumn;
      _rightColumn = _getTableForOccurrence(_rightOccurrence)
              ?.columns
              .where((c) => c.id == criterion.rightColumnId)
              .firstOrNull ??
          _rightColumn;
      _operator = _operators.contains(criterion.operator) ? criterion.operator : '=';
    });
  }

  /// Why the criterion in the field lists cannot be added or applied, if
  /// it cannot. [replacing] is the index it would replace.
  String? _criterionProblem(RelationshipPredicateModel? criterion, {int? replacing}) {
    if (criterion == null) return 'Select a field on each side.';
    final self = _selfMatchMessage(criterion);
    if (self != null) return self;
    for (var i = 0; i < _predicates.length; i++) {
      if (i != replacing && _predicates[i] == criterion) {
        return 'This criterion is already in the list.';
      }
    }
    return null;
  }

  void _addPredicate() {
    final criterion = _editingCriterion;
    final problem = _criterionProblem(criterion);
    setState(() {
      _notice = problem;
      if (problem == null) {
        _predicates.add(criterion!);
        _selectedPredicate = _predicates.length - 1;
      }
    });
  }

  void _changePredicate() {
    final index = _selectedPredicate;
    if (index == null) return;
    final criterion = _editingCriterion;
    final problem = _criterionProblem(criterion, replacing: index);
    setState(() {
      _notice = problem;
      if (problem == null) _predicates[index] = criterion!;
    });
  }

  void _deletePredicate() {
    final index = _selectedPredicate;
    if (index == null) return;
    setState(() {
      _notice = null;
      _predicates.removeAt(index);
      _selectedPredicate = _predicates.isEmpty ? null : (index < _predicates.length ? index : _predicates.length - 1);
    });
    if (_selectedPredicate != null) _selectPredicate(_selectedPredicate!);
  }

  void _movePredicate(int delta) {
    final index = _selectedPredicate;
    if (index == null) return;
    final target = index + delta;
    if (target < 0 || target >= _predicates.length) return;
    setState(() {
      _notice = null;
      final moved = _predicates.removeAt(index);
      _predicates.insert(target, moved);
      _selectedPredicate = target;
    });
  }

  /// An occurrence over another table makes the criteria's fields on that
  /// side meaningless, so the list starts again from the field lists.
  void _occurrenceChanged({required bool tableChanged}) {
    _nameController.text = _generateName();
    _notice = null;
    if (!tableChanged) return;
    _predicates.clear();
    _selectedPredicate = null;
    final first = _editingCriterion;
    if (first != null && _selfMatchMessage(first) == null) {
      _predicates.add(first);
      _selectedPredicate = 0;
    }
  }

  /// The criteria OK saves: the list, with a pending edit of the selected
  /// criterion applied as Change would apply it.
  List<RelationshipPredicateModel> get _resultPredicates {
    final result = List<RelationshipPredicateModel>.of(_predicates);
    if (_hasExternalSide) return result;
    final index = _selectedPredicate;
    if (_hasPendingChange && _criterionProblem(_editingCriterion, replacing: index) == null) {
      result[index!] = _editingCriterion!;
    }
    return result;
  }

  /// Why OK is unavailable, if it is.
  String? get _blockingProblem {
    if (_leftOccurrence == null || _rightOccurrence == null) return 'Select a table occurrence on each side.';
    if (_hasExternalSide) {
      if (!_isEditing) return 'A relationship to an occurrence of another file cannot be created here yet.';
      return _predicates.isEmpty ? 'Add at least one match criterion.' : null;
    }
    for (final (occurrence, column) in [(_leftOccurrence, _leftColumn), (_rightOccurrence, _rightColumn)]) {
      if ((_getTableForOccurrence(occurrence)?.columns ?? []).isEmpty) {
        return 'The table of "${occurrence!.name}" has no fields to match.';
      }
      if (column == null) return 'Select a field on each side.';
    }
    if (_hasPendingChange) {
      final pending = _criterionProblem(_editingCriterion, replacing: _selectedPredicate);
      if (pending != null) return pending;
    }
    final predicates = _resultPredicates;
    if (predicates.isEmpty) return 'Add at least one match criterion.';
    for (final criterion in predicates) {
      final self = _selfMatchMessage(criterion);
      if (self != null) return self;
    }
    return null;
  }

  String _fieldLabel(TableOccurrenceModel? occurrence, String columnId, String? storedName) {
    final column = _getTableForOccurrence(occurrence)?.columns.where((c) => c.id == columnId).firstOrNull;
    final field = column == null
        ? (storedName ?? columnId)
        : (column.displayName.isNotEmpty ? column.displayName : column.name);
    return '${occurrence?.name ?? '?'}::$field';
  }

  Widget _buildCriteria(BuildContext context) {
    final theme = Theme.of(context);
    final selected = _selectedPredicate;
    final allEqual = _resultPredicates.every((p) => p.operator == '=');
    final problem = _blockingProblem;
    final editable = !_hasExternalSide;

    Widget actionButton(String label, IconData icon, VoidCallback? onPressed) => TextButton.icon(
          onPressed: onPressed,
          icon: Icon(icon, size: 16),
          label: Text(label, style: const TextStyle(fontSize: 12.5)),
          style: TextButton.styleFrom(visualDensity: VisualDensity.compact),
        );

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            const Expanded(
              child: Text(
                'Match criteria: a record is related when it meets every criterion.',
                style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey),
              ),
            ),
            actionButton('Add', Icons.add, editable ? _addPredicate : null),
            actionButton('Change', Icons.edit_outlined, !editable || selected == null ? null : _changePredicate),
            actionButton('Delete', Icons.delete_outline, !editable || selected == null ? null : _deletePredicate),
            IconButton(
              tooltip: 'Move up',
              visualDensity: VisualDensity.compact,
              iconSize: 18,
              icon: const Icon(Icons.arrow_upward),
              onPressed: !editable || selected == null || selected == 0 ? null : () => _movePredicate(-1),
            ),
            IconButton(
              tooltip: 'Move down',
              visualDensity: VisualDensity.compact,
              iconSize: 18,
              icon: const Icon(Icons.arrow_downward),
              onPressed: !editable || selected == null || selected >= _predicates.length - 1 ? null : () => _movePredicate(1),
            ),
          ],
        ),
        const SizedBox(height: 4),
        Container(
          constraints: const BoxConstraints(minHeight: 36, maxHeight: 132),
          decoration: BoxDecoration(
            border: Border.all(color: theme.dividerColor),
            borderRadius: BorderRadius.circular(6),
            color: theme.cardColor,
          ),
          child: _predicates.isEmpty
              ? const Padding(
                  padding: EdgeInsets.all(10),
                  child: Text('No criteria', style: TextStyle(color: Colors.grey, fontSize: 12)),
                )
              : ListView.builder(
                  shrinkWrap: true,
                  itemCount: _predicates.length,
                  itemBuilder: (ctx, idx) {
                    final criterion = _predicates[idx];
                    final isSelected = idx == selected;
                    return InkWell(
                      key: ValueKey('relationship-criterion-$idx'),
                      onTap: () => _selectPredicate(idx),
                      child: Container(
                        color: isSelected ? theme.colorScheme.primary.withValues(alpha: 0.15) : Colors.transparent,
                        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                        child: Row(
                          children: [
                            Expanded(
                              child: Text(
                                _fieldLabel(_leftOccurrence, criterion.leftColumnId, criterion.leftColumnName),
                                textAlign: TextAlign.right,
                                style: TextStyle(fontSize: 12.5, fontWeight: isSelected ? FontWeight.bold : null),
                              ),
                            ),
                            SizedBox(
                              width: 40,
                              child: Text(
                                criterion.operator,
                                textAlign: TextAlign.center,
                                style: const TextStyle(fontSize: 14, fontWeight: FontWeight.bold),
                              ),
                            ),
                            Expanded(
                              child: Text(
                                _fieldLabel(_rightOccurrence, criterion.rightColumnId, criterion.rightColumnName),
                                style: TextStyle(fontSize: 12.5, fontWeight: isSelected ? FontWeight.bold : null),
                              ),
                            ),
                          ],
                        ),
                      ),
                    );
                  },
                ),
        ),
        if (_hasExternalSide && _isEditing)
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Text(
              'One side is a table of another file: its criteria are kept as they are, by field name, '
              'and cannot be edited here yet.',
              style: TextStyle(fontSize: 11, color: Colors.grey.shade600),
            ),
          ),
        if (_hasPendingChange)
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Text(
              'The fields above differ from criterion ${selected! + 1}: click Change to apply them, '
              'or Add to add them as a new criterion. OK applies them to criterion ${selected + 1}.',
              style: TextStyle(fontSize: 11, color: Colors.grey.shade600),
            ),
          ),
        if ((_notice ?? problem) != null)
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Text(
              (_notice ?? problem)!,
              style: TextStyle(fontSize: 11.5, color: theme.colorScheme.error),
            ),
          ),
        if ((_left.allowCreation || _right.allowCreation) && !allEqual)
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Text(
              'Records can be created through a relationship only when every criterion uses =; '
              'File4Base will refuse to create them through this one.',
              style: TextStyle(fontSize: 11.5, color: Colors.orange.shade800),
            ),
          ),
      ],
    );
  }

  /// The options of one side, under the name of its occurrence. [name] and
  /// [otherName] are the two sides as the labels quote them.
  Widget _buildSideOptions({
    required String side,
    required _SideOptions options,
    required TableOccurrenceModel? occurrence,
    required String name,
    required String otherName,
  }) {
    const label = TextStyle(fontSize: 12.5);
    final title = occurrence?.name.trim() ?? '';
    final sortSpec = options.initialSort;
    return Column(
      key: ValueKey('relationship-$side-options'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          title.isEmpty ? (side == 'left' ? 'Left table' : 'Right table') : title,
          style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
          overflow: TextOverflow.ellipsis,
        ),
        CheckboxListTile(
          key: ValueKey('relationship-$side-allow-creation'),
          dense: true,
          contentPadding: EdgeInsets.zero,
          value: options.allowCreation,
          title: Text('Allow creation of records in $name via this relationship', style: label),
          controlAffinity: ListTileControlAffinity.leading,
          onChanged: (val) => setState(() => options.allowCreation = val ?? false),
        ),
        CheckboxListTile(
          key: ValueKey('relationship-$side-cascade-delete'),
          dense: true,
          contentPadding: EdgeInsets.zero,
          value: options.cascadeDelete,
          title: Text('Delete related records in $name when a record is deleted in $otherName', style: label),
          controlAffinity: ListTileControlAffinity.leading,
          onChanged: (val) => setState(() => options.cascadeDelete = val ?? false),
        ),
        CheckboxListTile(
          key: ValueKey('relationship-$side-sort'),
          dense: true,
          contentPadding: EdgeInsets.zero,
          value: options.sorted,
          title: Text('Sort records of $name', style: label),
          // A sort written out (a migrated one) is kept as it is; one
          // turned on here sorts by the match field.
          subtitle: Text(
            sortSpec != null && !_SideOptions.isMatchFieldSort(sortSpec)
                ? 'By ${sortSpec.split(',').map((f) => f.trim()).join(', ')}'
                : 'By the match field',
            style: const TextStyle(fontSize: 10.5, color: Colors.grey),
          ),
          controlAffinity: ListTileControlAffinity.leading,
          onChanged: (val) => setState(() => options.sorted = val ?? false),
        ),
      ],
    );
  }

  Widget _buildFieldList(
    BuildContext context, {
    required TableOccurrenceModel? occurrence,
    required ColumnModel? selectedColumn,
    required ValueChanged<ColumnModel> onSelected,
  }) {
    final theme = Theme.of(context);
    final table = _getTableForOccurrence(occurrence);
    final columns = table?.columns ?? [];

    return Container(
      height: 180,
      decoration: BoxDecoration(
        border: Border.all(color: theme.dividerColor),
        borderRadius: BorderRadius.circular(6),
        color: theme.cardColor,
      ),
      child: columns.isEmpty
          ? const Center(
              child: Text('No fields', style: TextStyle(color: Colors.grey, fontSize: 12)),
            )
          : ListView.builder(
              itemCount: columns.length,
              itemBuilder: (ctx, idx) {
                final col = columns[idx];
                final isSelected = selectedColumn?.id == col.id;
                return InkWell(
                  onTap: () => onSelected(col),
                  child: Container(
                    color: isSelected ? theme.colorScheme.primary.withValues(alpha: 0.15) : Colors.transparent,
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                    child: Row(
                      children: [
                        Icon(
                          col.isPrimaryKey ? Icons.key : Icons.commit,
                          size: 14,
                          color: isSelected
                              ? theme.colorScheme.primary
                              : (col.isPrimaryKey ? Colors.amber[700] : Colors.grey[600]),
                        ),
                        const SizedBox(width: 6),
                        Expanded(
                          child: Text(
                            col.displayName.isNotEmpty ? col.displayName : col.name,
                            style: TextStyle(
                              fontSize: 12.5,
                              fontWeight: isSelected ? FontWeight.bold : FontWeight.normal,
                              color: isSelected ? theme.colorScheme.primary : null,
                            ),
                          ),
                        ),
                        Text(
                          col.fieldType,
                          style: TextStyle(fontSize: 11, color: Colors.grey[500]),
                        ),
                      ],
                    ),
                  ),
                );
              },
            ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return AlertDialog(
      title: Row(
        children: [
          Icon(Icons.hub_outlined, color: theme.colorScheme.primary, size: 22),
          const SizedBox(width: 8),
          Text(
            widget.initialRelationship != null ? 'Edit Relationship' : 'Specify Relationship',
            style: const TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
          ),
        ],
      ),
      content: SizedBox(
        width: 650,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'Define a relationship between two table occurrences by selecting the fields to match.',
              style: TextStyle(fontSize: 12.5, color: theme.textTheme.bodySmall?.color ?? Colors.grey[700]),
            ),
            const SizedBox(height: 16),
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                // Left Occurrence & Fields
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                        decoration: BoxDecoration(
                          border: Border.all(color: theme.dividerColor),
                          borderRadius: BorderRadius.circular(6),
                        ),
                        child: DropdownButtonHideUnderline(
                          child: DropdownButton<TableOccurrenceModel>(
                            value: _leftOccurrence,
                            isDense: true,
                            isExpanded: true,
                            items: widget.occurrences.map((o) {
                              return DropdownMenuItem(
                                value: o,
                                child: Text(o.name, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600)),
                              );
                            }).toList(),
                            // An existing relationship keeps its occurrences.
                            onChanged: _isEditing
                                ? null
                                : (occ) {
                                    if (occ != null) {
                                      setState(() {
                                        final tableChanged = occ.baseTableId != _leftOccurrence?.baseTableId;
                                        _leftOccurrence = occ;
                                        final tbl = _getTableForOccurrence(occ);
                                        if (tableChanged) {
                                          _leftColumn = tbl?.columns.isNotEmpty == true ? tbl!.columns.first : null;
                                        }
                                        _occurrenceChanged(tableChanged: tableChanged);
                                      });
                                    }
                                  },
                          ),
                        ),
                      ),
                      const SizedBox(height: 8),
                      _buildFieldList(
                        context,
                        occurrence: _leftOccurrence,
                        selectedColumn: _leftColumn,
                        onSelected: (col) => setState(() {
                          _leftColumn = col;
                          _notice = null;
                        }),
                      ),
                    ],
                  ),
                ),

                // Center Operator Picker
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 12.0, vertical: 30.0),
                  child: Column(
                    children: [
                      const Text('Match', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey)),
                      const SizedBox(height: 8),
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                        decoration: BoxDecoration(
                          color: theme.colorScheme.primary.withValues(alpha: 0.1),
                          borderRadius: BorderRadius.circular(8),
                          border: Border.all(color: theme.colorScheme.primary.withValues(alpha: 0.3)),
                        ),
                        child: DropdownButtonHideUnderline(
                          child: DropdownButton<String>(
                            value: _operator,
                            items: _operators.map((op) {
                              return DropdownMenuItem(
                                value: op,
                                child: Text(op, style: const TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
                              );
                            }).toList(),
                            onChanged: (op) {
                              if (op != null) {
                                setState(() {
                                  _operator = op;
                                  _notice = null;
                                });
                              }
                            },
                          ),
                        ),
                      ),
                    ],
                  ),
                ),

                // Right Occurrence & Fields
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                        decoration: BoxDecoration(
                          border: Border.all(color: theme.dividerColor),
                          borderRadius: BorderRadius.circular(6),
                        ),
                        child: DropdownButtonHideUnderline(
                          child: DropdownButton<TableOccurrenceModel>(
                            value: _rightOccurrence,
                            isDense: true,
                            isExpanded: true,
                            items: widget.occurrences.map((o) {
                              return DropdownMenuItem(
                                value: o,
                                child: Text(o.name, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600)),
                              );
                            }).toList(),
                            onChanged: _isEditing
                                ? null
                                : (occ) {
                                    if (occ != null) {
                                      setState(() {
                                        final tableChanged = occ.baseTableId != _rightOccurrence?.baseTableId;
                                        _rightOccurrence = occ;
                                        final tbl = _getTableForOccurrence(occ);
                                        if (tableChanged) {
                                          _rightColumn = tbl?.columns.isNotEmpty == true ? tbl!.columns.first : null;
                                        }
                                        _occurrenceChanged(tableChanged: tableChanged);
                                      });
                                    }
                                  },
                          ),
                        ),
                      ),
                      const SizedBox(height: 8),
                      _buildFieldList(
                        context,
                        occurrence: _rightOccurrence,
                        selectedColumn: _rightColumn,
                        onSelected: (col) => setState(() {
                          _rightColumn = col;
                          _notice = null;
                        }),
                      ),
                    ],
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            _buildCriteria(context),
            const SizedBox(height: 12),
            const Divider(),
            const SizedBox(height: 8),
            // Each side has its own options, as in FileMaker's Edit
            // Relationship dialog: a column per occurrence, each option
            // acting on the records of the occurrence it is under.
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: _buildSideOptions(
                    side: 'left',
                    options: _left,
                    occurrence: _leftOccurrence,
                    name: _leftSideName,
                    otherName: _rightSideName,
                  ),
                ),
                const SizedBox(width: 16),
                Expanded(
                  child: _buildSideOptions(
                    side: 'right',
                    options: _right,
                    occurrence: _rightOccurrence,
                    name: _rightSideName,
                    otherName: _leftSideName,
                  ),
                ),
              ],
            ),
            const Text(
              'A portal can override a sort with one of its own, in Portal Setup.',
              style: TextStyle(fontSize: 10.5, color: Colors.grey),
            ),
          ],
        ),
      ),
    ),
      actions: [
        if (widget.initialRelationship != null)
          TextButton(
            onPressed: () {
              Navigator.of(context).pop({'delete': true, 'id': widget.initialRelationship!.id});
            },
            style: TextButton.styleFrom(foregroundColor: Colors.red),
            child: const Text('Delete relationship'),
          ),
        OutlinedButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        ElevatedButton(
          onPressed: _blockingProblem != null
              ? null
              : () {
                  final predicates = _resultPredicates;
                  Navigator.of(context).pop({
                    'name': _nameController.text.trim().isNotEmpty ? _nameController.text.trim() : _generateName(),
                    'left_occurrence_id': _leftOccurrence!.id,
                    'left_column_id': predicates.first.leftColumnId,
                    'right_occurrence_id': _rightOccurrence!.id,
                    'right_column_id': predicates.first.rightColumnId,
                    'operator': predicates.first.operator,
                    'predicates': predicates,
                    'allow_creation': _right.allowCreation,
                    'cascade_delete': _right.cascadeDelete,
                    'sort_related': _right.sortResult,
                    'left_allow_creation': _left.allowCreation,
                    'left_cascade_delete': _left.cascadeDelete,
                    'left_sort_related': _left.sortResult,
                  });
                },
          child: const Text('OK'),
        ),
      ],
    );
  }
}

/// The options of one side of a relationship, as the dialog edits them.
class _SideOptions {
  bool allowCreation = false;
  bool cascadeDelete = false;
  bool sorted = false;

  /// The sort the relationship had when the dialog opened, which OK keeps
  /// while the side stays sorted.
  String? initialSort;

  void load({required bool allowCreation, required bool cascadeDelete, String? sort}) {
    this.allowCreation = allowCreation;
    this.cascadeDelete = cascadeDelete;
    final spec = sort?.trim() ?? '';
    initialSort = spec.isEmpty ? null : spec;
    sorted = initialSort != null;
  }

  /// What OK sends for the sort: the one it had, or the match field when it
  /// is turned on here; an empty text clears one it had, and nothing is sent
  /// for a side that had none and still has none.
  String? get sortResult {
    if (sorted) return initialSort ?? 'asc';
    return initialSort == null ? null : '';
  }

  /// Whether [spec] is the bare direction that sorts by the match field.
  static bool isMatchFieldSort(String spec) =>
      const {'asc', 'ascending', 'desc', 'descending'}.contains(spec.trim().toLowerCase());
}
