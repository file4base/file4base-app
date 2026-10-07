import 'package:flutter/material.dart';
import '../../../core/api/api_client.dart';

class SpecifyRelationshipDialog extends StatefulWidget {
  final List<TableOccurrenceModel> occurrences;
  final List<TableModel> tables;
  final RelationshipModel? initialRelationship;
  final String? preselectedLeftOccurrenceId;
  final String? preselectedRightOccurrenceId;
  final String? preselectedLeftColumnId;
  final String? preselectedRightColumnId;

  const SpecifyRelationshipDialog({
    super.key,
    required this.occurrences,
    required this.tables,
    this.initialRelationship,
    this.preselectedLeftOccurrenceId,
    this.preselectedRightOccurrenceId,
    this.preselectedLeftColumnId,
    this.preselectedRightColumnId,
  });

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
      barrierDismissible: true,
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
  /// How the two sides are named in the option labels, so they say which
  /// table they act on instead of "this table" (#36).
  String get _leftSideName => _occurrenceName(_leftOccurrence, 'the other table');
  String get _rightSideName => _occurrenceName(_rightOccurrence, 'this table');

  String _occurrenceName(TableOccurrenceModel? occurrence, String fallback) {
    final name = occurrence?.name.trim() ?? '';
    return name.isEmpty ? fallback : '"$name"';
  }

  bool _allowCreation = false;
  bool _cascadeDelete = false;
  bool _sortRelated = false;
  late final TextEditingController _nameController;

  final List<String> _operators = ['=', '≠', '<', '≤', '>', '≥'];

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
      _allowCreation = rel.allowCreation;
      _cascadeDelete = rel.cascadeDelete;
      _sortRelated = rel.sortRelated != null && rel.sortRelated!.isNotEmpty;
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
    return '${lName}_to_${rName}';
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
                    color: isSelected ? theme.colorScheme.primary.withOpacity(0.15) : Colors.transparent,
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
                            onChanged: (occ) {
                              if (occ != null) {
                                setState(() {
                                  _leftOccurrence = occ;
                                  final tbl = _getTableForOccurrence(occ);
                                  _leftColumn = tbl?.columns.isNotEmpty == true ? tbl!.columns.first : null;
                                  _nameController.text = _generateName();
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
                        onSelected: (col) => setState(() => _leftColumn = col),
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
                          color: theme.colorScheme.primary.withOpacity(0.1),
                          borderRadius: BorderRadius.circular(8),
                          border: Border.all(color: theme.colorScheme.primary.withOpacity(0.3)),
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
                              if (op != null) setState(() => _operator = op);
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
                            onChanged: (occ) {
                              if (occ != null) {
                                setState(() {
                                  _rightOccurrence = occ;
                                  final tbl = _getTableForOccurrence(occ);
                                  _rightColumn = tbl?.columns.isNotEmpty == true ? tbl!.columns.first : null;
                                  _nameController.text = _generateName();
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
                        onSelected: (col) => setState(() => _rightColumn = col),
                      ),
                    ],
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),
            const Divider(),
            const SizedBox(height: 8),
            // These options act on the right-hand occurrence: the
            // relationship is drawn left to right, and File4Base reads that as
            // parent to child (#36). They name the tables rather than saying
            // "this table", which said nothing in a dialog showing two.
            Text(
              'These options act on $_rightSideName, the side this relationship points at.',
              style: TextStyle(fontSize: 11, color: Colors.grey.shade600),
            ),
            const SizedBox(height: 4),
            CheckboxListTile(
              dense: true,
              contentPadding: EdgeInsets.zero,
              value: _allowCreation,
              title: Text(
                'Allow records to be created in $_rightSideName through this relationship',
                style: const TextStyle(fontSize: 12.5),
              ),
              controlAffinity: ListTileControlAffinity.leading,
              onChanged: (val) => setState(() => _allowCreation = val ?? false),
            ),
            CheckboxListTile(
              dense: true,
              contentPadding: EdgeInsets.zero,
              value: _cascadeDelete,
              title: Text(
                'Delete the matching records in $_rightSideName when a record is deleted in $_leftSideName',
                style: const TextStyle(fontSize: 12.5),
              ),
              controlAffinity: ListTileControlAffinity.leading,
              onChanged: (val) => setState(() => _cascadeDelete = val ?? false),
            ),
            CheckboxListTile(
              dense: true,
              contentPadding: EdgeInsets.zero,
              value: _sortRelated,
              title: Text(
                'Sort the related records of $_rightSideName by their match field',
                style: const TextStyle(fontSize: 12.5),
              ),
              subtitle: const Text(
                'A portal can override this with a sort order of its own, in Portal Setup.',
                style: TextStyle(fontSize: 10.5, color: Colors.grey),
              ),
              controlAffinity: ListTileControlAffinity.leading,
              onChanged: (val) => setState(() => _sortRelated = val ?? false),
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
          onPressed: () => Navigator.of(context).pop(null),
          child: const Text('Cancel'),
        ),
        ElevatedButton(
          onPressed: _leftOccurrence == null ||
                  _rightOccurrence == null ||
                  _leftColumn == null ||
                  _rightColumn == null ||
                  (_leftOccurrence!.id == _rightOccurrence!.id && _leftColumn!.id == _rightColumn!.id)
              ? null
              : () {
                  Navigator.of(context).pop({
                    'name': _nameController.text.trim().isNotEmpty ? _nameController.text.trim() : _generateName(),
                    'left_occurrence_id': _leftOccurrence!.id,
                    'left_column_id': _leftColumn!.id,
                    'right_occurrence_id': _rightOccurrence!.id,
                    'right_column_id': _rightColumn!.id,
                    'operator': _operator,
                    'allow_creation': _allowCreation,
                    'cascade_delete': _cascadeDelete,
                    'sort_related': _sortRelated ? 'asc' : null,
                  });
                },
          child: const Text('OK'),
        ),
      ],
    );
  }
}
