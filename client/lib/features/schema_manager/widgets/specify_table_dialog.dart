import 'package:flutter/material.dart';
import '../../../core/api/api_client.dart';

class SpecifyTableDialog extends StatefulWidget {
  final List<TableModel> tables;
  final List<TableOccurrenceModel> existingOccurrences;
  final String activeDatabaseName;
  final TableOccurrenceModel? initialOccurrence;

  const SpecifyTableDialog({
    super.key,
    required this.tables,
    required this.existingOccurrences,
    this.activeDatabaseName = 'Current file',
    this.initialOccurrence,
  });

  static Future<Map<String, dynamic>?> show(
    BuildContext context, {
    required List<TableModel> tables,
    required List<TableOccurrenceModel> existingOccurrences,
    String activeDatabaseName = 'Current file',
    TableOccurrenceModel? initialOccurrence,
  }) {
    return showDialog<Map<String, dynamic>>(
      context: context,
      barrierDismissible: true,
      builder: (ctx) => SpecifyTableDialog(
        tables: tables,
        existingOccurrences: existingOccurrences,
        activeDatabaseName: activeDatabaseName,
        initialOccurrence: initialOccurrence,
      ),
    );
  }

  @override
  State<SpecifyTableDialog> createState() => _SpecifyTableDialogState();
}

class _SpecifyTableDialogState extends State<SpecifyTableDialog> {
  TableModel? _selectedTable;
  late final TextEditingController _nameController;
  String _selectedDataSource = 'Current file';

  @override
  void initState() {
    super.initState();
    if (widget.initialOccurrence != null) {
      _selectedTable = widget.tables.where((t) => t.id == widget.initialOccurrence!.baseTableId).firstOrNull ??
          (widget.tables.isNotEmpty ? widget.tables.first : null);
      _nameController = TextEditingController(text: widget.initialOccurrence!.name);
    } else {
      _selectedTable = widget.tables.isNotEmpty ? widget.tables.first : null;
      final defaultName = _selectedTable != null ? _suggestOccurrenceName(_selectedTable!) : '';
      _nameController = TextEditingController(text: defaultName);
    }
  }

  @override
  void dispose() {
    _nameController.dispose();
    super.dispose();
  }

  String _suggestOccurrenceName(TableModel table) {
    final baseName = table.displayName.trim().isNotEmpty ? table.displayName.trim() : table.name;
    final existsExact = widget.existingOccurrences.any(
      (o) => o.name.toLowerCase() == baseName.toLowerCase(),
    );
    if (!existsExact) {
      return baseName;
    }
    int count = 2;
    while (widget.existingOccurrences.any((o) => o.name.toLowerCase() == '$baseName $count'.toLowerCase())) {
      count++;
    }
    return '$baseName $count';
  }

  void _onTableSelected(TableModel table) {
    setState(() {
      _selectedTable = table;
      if (widget.initialOccurrence == null) {
        _nameController.text = _suggestOccurrenceName(table);
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return AlertDialog(
      title: Row(
        children: [
          Icon(Icons.table_chart_outlined, color: theme.colorScheme.primary, size: 22),
          const SizedBox(width: 8),
          const Text('Specify Table', style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
        ],
      ),
      content: SizedBox(
        width: 440,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'Select a table from this file or another data source to include in the graph. The same table can appear more than once.',
              style: TextStyle(fontSize: 12.5, color: theme.textTheme.bodySmall?.color ?? Colors.grey[700], height: 1.3),
            ),
            const SizedBox(height: 16),
            Row(
              children: [
                const Text('Data source:', style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500)),
                const SizedBox(width: 10),
                Expanded(
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 2),
                    decoration: BoxDecoration(
                      border: Border.all(color: theme.dividerColor),
                      borderRadius: BorderRadius.circular(6),
                    ),
                    child: DropdownButtonHideUnderline(
                      child: DropdownButton<String>(
                        value: _selectedDataSource,
                        isDense: true,
                        isExpanded: true,
                        items: [
                          DropdownMenuItem(
                            value: 'Current file',
                            child: Text('Current file ("${widget.activeDatabaseName}")', style: const TextStyle(fontSize: 13)),
                          ),
                        ],
                        onChanged: (val) {
                          if (val != null) setState(() => _selectedDataSource = val);
                        },
                      ),
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 14),
            Container(
              height: 180,
              decoration: BoxDecoration(
                border: Border.all(color: theme.dividerColor),
                borderRadius: BorderRadius.circular(6),
                color: theme.cardColor,
              ),
              child: widget.tables.isEmpty
                  ? const Center(
                      child: Text('No tables available', style: TextStyle(color: Colors.grey, fontSize: 13)),
                    )
                  : ListView.builder(
                      itemCount: widget.tables.length,
                      itemBuilder: (ctx, idx) {
                        final tbl = widget.tables[idx];
                        final isSelected = _selectedTable?.id == tbl.id;
                        return InkWell(
                          onTap: () => _onTableSelected(tbl),
                          child: Container(
                            color: isSelected ? theme.colorScheme.primary.withOpacity(0.15) : Colors.transparent,
                            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                            child: Row(
                              children: [
                                Icon(
                                  Icons.table_view,
                                  size: 16,
                                  color: isSelected ? theme.colorScheme.primary : Colors.grey[600],
                                ),
                                const SizedBox(width: 8),
                                Expanded(
                                  child: Text(
                                    tbl.displayName.isNotEmpty ? tbl.displayName : tbl.name,
                                    style: TextStyle(
                                      fontSize: 13,
                                      fontWeight: isSelected ? FontWeight.bold : FontWeight.normal,
                                      color: isSelected ? theme.colorScheme.primary : null,
                                    ),
                                  ),
                                ),
                                Text(
                                  '${tbl.columns.length} fields',
                                  style: TextStyle(fontSize: 11, color: Colors.grey[600]),
                                ),
                              ],
                            ),
                          ),
                        );
                      },
                    ),
            ),
            const SizedBox(height: 16),
            Row(
              children: [
                const SizedBox(
                  width: 70,
                  child: Text('Name:', style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500)),
                ),
                Expanded(
                  child: TextField(
                    controller: _nameController,
                    decoration: const InputDecoration(
                      isDense: true,
                      contentPadding: EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                      border: OutlineInputBorder(),
                    ),
                    style: const TextStyle(fontSize: 13),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
      actions: [
        OutlinedButton(
          onPressed: () => Navigator.of(context).pop(null),
          child: const Text('Cancel'),
        ),
        ElevatedButton(
          onPressed: _selectedTable == null || _nameController.text.trim().isEmpty
              ? null
              : () {
                  Navigator.of(context).pop({
                    'base_table_id': _selectedTable!.id,
                    'name': _nameController.text.trim(),
                  });
                },
          child: const Text('OK'),
        ),
      ],
    );
  }
}
