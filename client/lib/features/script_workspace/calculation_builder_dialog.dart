import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/theme/theme_model.dart';
import '../../core/theme/theme_provider.dart';

class CalculationBuilderDialog extends ConsumerStatefulWidget {
  final String initialFormula;
  final String? contextTable;
  final List<String> availableTables;
  final Map<String, List<String>> fieldsByTable;

  const CalculationBuilderDialog({
    super.key,
    this.initialFormula = '',
    this.contextTable,
    this.availableTables = const [],
    this.fieldsByTable = const {},
  });

  static Future<String?> show(
    BuildContext context, {
    String initialFormula = '',
    String? contextTable,
    List<String> availableTables = const [],
    Map<String, List<String>> fieldsByTable = const {},
  }) {
    return showDialog<String>(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => CalculationBuilderDialog(
        initialFormula: initialFormula,
        contextTable: contextTable,
        availableTables: availableTables,
        fieldsByTable: fieldsByTable,
      ),
    );
  }

  @override
  ConsumerState<CalculationBuilderDialog> createState() => _CalculationBuilderDialogState();
}

class _CalculationBuilderDialogState extends ConsumerState<CalculationBuilderDialog> {
  late TextEditingController _formulaController;
  late FocusNode _formulaFocusNode;
  String _selectedTable = '';
  String _fieldSearch = '';
  String _functionSearch = '';
  String _functionCategory = 'All';

  static const List<String> _operators = [
    '+', '-', '*', '/', '^', '&',
    '=', '≠', '>', '<', '≥', '≤',
    'AND', 'OR', 'NOT', '(', ')', '" "'
  ];

  static const Map<String, List<Map<String, String>>> _functionsByCategory = {
    'Aggregate': [
      {'name': 'Sum', 'syntax': 'Sum( field )', 'desc': 'Sums all nonempty values.'},
      {'name': 'Average', 'syntax': 'Average( field )', 'desc': 'Calculates the average of the values.'},
      {'name': 'Count', 'syntax': 'Count( field )', 'desc': 'Counts valid, nonempty values.'},
      {'name': 'Max', 'syntax': 'Max( field )', 'desc': 'Returns the maximum value.'},
      {'name': 'Min', 'syntax': 'Min( field )', 'desc': 'Returns the minimum value.'},
    ],
    'Logical': [
      {'name': 'If', 'syntax': 'If( condition ; result1 ; result2 )', 'desc': 'Evaluates a condition and returns a value according to whether it is true or false.'},
      {'name': 'IsEmpty', 'syntax': 'IsEmpty( field )', 'desc': 'Returns 1 (true) if the field is empty.'},
      {'name': 'IsValid', 'syntax': 'IsValid( field )', 'desc': 'Returns 1 if the field contains a valid value.'},
      {'name': 'Case', 'syntax': 'Case( test1 ; result1 ; [test2 ; result2] ; default )', 'desc': 'Evaluates multiple conditions in sequence.'},
    ],
    'Text': [
      {'name': 'Length', 'syntax': 'Length( text )', 'desc': 'Returns the length in characters.'},
      {'name': 'Lower', 'syntax': 'Lower( text )', 'desc': 'Converts text to lowercase.'},
      {'name': 'Upper', 'syntax': 'Upper( text )', 'desc': 'Converts text to uppercase.'},
      {'name': 'Trim', 'syntax': 'Trim( text )', 'desc': 'Removes leading and trailing spaces.'},
      {'name': 'Substitute', 'syntax': 'Substitute( text ; search ; replacement )', 'desc': 'Replaces text with the specified text.'},
    ],
    'Date & Time': [
      {'name': 'Get(CurrentDate)', 'syntax': 'Get(CurrentDate)', 'desc': 'Returns the current system date.'},
      {'name': 'Get(CurrentTime)', 'syntax': 'Get(CurrentTime)', 'desc': 'Returns the current system time.'},
      {'name': 'Get(CurrentTimestamp)', 'syntax': 'Get(CurrentTimestamp)', 'desc': 'Returns the current timestamp.'},
    ],
    'System': [
      {'name': 'Get(AccountName)', 'syntax': 'Get(AccountName)', 'desc': 'Returns the active account name.'},
      {'name': 'Get(LayoutName)', 'syntax': 'Get(LayoutName)', 'desc': 'Returns the active layout name.'},
      {'name': 'Get(RecordID)', 'syntax': 'Get(RecordID)', 'desc': 'Returns the unique record identifier.'},
      {'name': 'Get(LastError)', 'syntax': 'Get(LastError)', 'desc': 'Returns the error code from the last action.'},
    ],
  };

  @override
  void initState() {
    super.initState();
    _formulaController = TextEditingController(text: widget.initialFormula);
    _formulaFocusNode = FocusNode();

    if (widget.contextTable != null && widget.contextTable!.isNotEmpty) {
      _selectedTable = widget.contextTable!;
    } else if (widget.availableTables.isNotEmpty) {
      _selectedTable = widget.availableTables.first;
    }
  }

  @override
  void dispose() {
    _formulaController.dispose();
    _formulaFocusNode.dispose();
    super.dispose();
  }

  void _insertAtCursor(String text) {
    final currentText = _formulaController.text;
    final selection = _formulaController.selection;
    if (selection.start >= 0 && selection.end >= selection.start) {
      final newText = currentText.replaceRange(selection.start, selection.end, text);
      _formulaController.value = TextEditingValue(
        text: newText,
        selection: TextSelection.collapsed(offset: selection.start + text.length),
      );
    } else {
      _formulaController.text = currentText + text;
      _formulaController.selection = TextSelection.collapsed(offset: _formulaController.text.length);
    }
    _formulaFocusNode.requestFocus();
  }

  List<String> _getFilteredFields() {
    final fields = widget.fieldsByTable[_selectedTable] ?? [];
    if (_fieldSearch.isEmpty) return fields;
    return fields.where((f) => f.toLowerCase().contains(_fieldSearch.toLowerCase())).toList();
  }

  List<Map<String, String>> _getFilteredFunctions() {
    List<Map<String, String>> list = [];
    if (_functionCategory == 'All') {
      for (final cat in _functionsByCategory.values) {
        list.addAll(cat);
      }
    } else {
      list = _functionsByCategory[_functionCategory] ?? [];
    }

    if (_functionSearch.isNotEmpty) {
      list = list.where((f) =>
        f['name']!.toLowerCase().contains(_functionSearch.toLowerCase()) ||
        f['desc']!.toLowerCase().contains(_functionSearch.toLowerCase())
      ).toList();
    }
    return list;
  }

  @override
  Widget build(BuildContext context) {
    final theme = ref.watch(appThemeProvider);

    return Dialog(
      backgroundColor: Colors.transparent,
      child: Container(
        width: 1040,
        height: 660,
        decoration: BoxDecoration(
          color: theme.surface,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: theme.border),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: theme.isDark ? 0.6 : 0.2),
              blurRadius: 32,
              offset: const Offset(0, 12),
            ),
          ],
        ),
        child: Column(
          children: [
            // Top Window Bar
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
              decoration: BoxDecoration(
                color: theme.surfaceContainer,
                borderRadius: const BorderRadius.vertical(top: Radius.circular(12)),
                border: Border(bottom: BorderSide(color: theme.border)),
              ),
              child: Row(
                children: [
                  Icon(Icons.functions, color: theme.primaryAccent, size: 20),
                  const SizedBox(width: 10),
                  Text(
                    'Specify Calculation',
                    style: TextStyle(
                      color: theme.textPrimary,
                      fontSize: 15,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const Spacer(),
                  if (_selectedTable.isNotEmpty)
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                      decoration: BoxDecoration(
                        color: theme.primaryAccent.withValues(alpha: 0.15),
                        borderRadius: BorderRadius.circular(6),
                        border: Border.all(color: theme.primaryAccent.withValues(alpha: 0.3)),
                      ),
                      child: Text(
                        'Context: $_selectedTable',
                        style: TextStyle(color: theme.primaryAccent, fontSize: 12, fontWeight: FontWeight.w500),
                      ),
                    ),
                  const SizedBox(width: 12),
                  IconButton(
                    icon: Icon(Icons.close, color: theme.textSecondary, size: 20),
                    onPressed: () => Navigator.of(context).pop(),
                    tooltip: 'Close',
                    splashRadius: 18,
                  ),
                ],
              ),
            ),

            // Main 3-panel area
            Expanded(
              child: Row(
                children: [
                  // Left: Fields panel
                  Container(
                    width: 250,
                    decoration: BoxDecoration(
                      color: theme.surface,
                      border: Border(right: BorderSide(color: theme.border)),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        // Table selector
                        Container(
                          padding: const EdgeInsets.all(10),
                          decoration: BoxDecoration(
                            border: Border(bottom: BorderSide(color: theme.border)),
                          ),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text('Table occurrence', style: TextStyle(color: theme.textSecondary, fontSize: 11, fontWeight: FontWeight.bold)),
                              const SizedBox(height: 6),
                              Container(
                                padding: const EdgeInsets.symmetric(horizontal: 10),
                                decoration: BoxDecoration(
                                  color: theme.surfaceContainer,
                                  borderRadius: BorderRadius.circular(6),
                                  border: Border.all(color: theme.border),
                                ),
                                child: DropdownButtonHideUnderline(
                                  child: DropdownButton<String>(
                                    isExpanded: true,
                                    dropdownColor: theme.surface,
                                    value: _selectedTable.isNotEmpty ? _selectedTable : null,
                                    hint: Text('Select table...', style: TextStyle(color: theme.textSecondary, fontSize: 12)),
                                    style: TextStyle(color: theme.textPrimary, fontSize: 12),
                                    items: widget.availableTables.map((t) => DropdownMenuItem(value: t, child: Text(t))).toList(),
                                    onChanged: (val) {
                                      if (val != null) setState(() => _selectedTable = val);
                                    },
                                  ),
                                ),
                              ),
                              const SizedBox(height: 8),
                              // Field search
                              SizedBox(
                                height: 32,
                                child: TextField(
                                  style: TextStyle(color: theme.textPrimary, fontSize: 12),
                                  decoration: InputDecoration(
                                    hintText: 'Filter fields...',
                                    hintStyle: TextStyle(color: theme.textSecondary, fontSize: 11),
                                    prefixIcon: Icon(Icons.search, size: 16, color: theme.textSecondary),
                                    contentPadding: EdgeInsets.zero,
                                    filled: true,
                                    fillColor: theme.surfaceContainer,
                                    border: OutlineInputBorder(borderRadius: BorderRadius.circular(6), borderSide: BorderSide.none),
                                  ),
                                  onChanged: (v) => setState(() => _fieldSearch = v),
                                ),
                              ),
                            ],
                          ),
                        ),
                        // Fields list
                        Expanded(
                          child: ListView(
                            padding: const EdgeInsets.symmetric(vertical: 4),
                            children: _getFilteredFields().map((field) {
                              return InkWell(
                                onTap: () {
                                  final tablePrefix = _selectedTable.isNotEmpty ? '$_selectedTable::' : '';
                                  _insertAtCursor('$tablePrefix$field');
                                },
                                child: Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
                                  child: Row(
                                    children: [
                                      Icon(Icons.data_object, color: theme.fieldsColor, size: 14),
                                      const SizedBox(width: 8),
                                      Expanded(
                                        child: Text(
                                          field,
                                          style: TextStyle(color: theme.textPrimary, fontSize: 12),
                                          overflow: TextOverflow.ellipsis,
                                        ),
                                      ),
                                    ],
                                  ),
                                ),
                              );
                            }).toList(),
                          ),
                        ),
                      ],
                    ),
                  ),

                  // Center: Formula Editor & Operators
                  Expanded(
                    child: Container(
                      color: theme.background,
                      padding: const EdgeInsets.all(12),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          // Operators toolbar
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
                            margin: const EdgeInsets.only(bottom: 10),
                            decoration: BoxDecoration(
                              color: theme.surfaceContainer,
                              borderRadius: BorderRadius.circular(8),
                              border: Border.all(color: theme.border),
                            ),
                            child: Wrap(
                              spacing: 6,
                              runSpacing: 4,
                              children: _operators.map((op) {
                                return InkWell(
                                  onTap: () => _insertAtCursor(op == '" "' ? '""' : ' $op '),
                                  borderRadius: BorderRadius.circular(4),
                                  child: Container(
                                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
                                    decoration: BoxDecoration(
                                      color: theme.surface,
                                      borderRadius: BorderRadius.circular(4),
                                      border: Border.all(color: theme.border),
                                    ),
                                    child: Text(
                                      op,
                                      style: TextStyle(
                                        color: theme.primaryAccent,
                                        fontSize: 12,
                                        fontWeight: FontWeight.bold,
                                        fontFamily: 'monospace',
                                      ),
                                    ),
                                  ),
                                );
                              }).toList(),
                            ),
                          ),

                          // Formula Box
                          Text('Formula / Calculation:', style: TextStyle(color: theme.textSecondary, fontSize: 11, fontWeight: FontWeight.bold)),
                          const SizedBox(height: 6),
                          Expanded(
                            child: Container(
                              decoration: BoxDecoration(
                                color: theme.surface,
                                borderRadius: BorderRadius.circular(8),
                                border: Border.all(color: theme.border),
                              ),
                              padding: const EdgeInsets.all(12),
                              child: TextField(
                                controller: _formulaController,
                                focusNode: _formulaFocusNode,
                                maxLines: null,
                                expands: true,
                                style: TextStyle(
                                  color: theme.textPrimary,
                                  fontSize: 13,
                                  fontFamily: 'monospace',
                                  height: 1.4,
                                ),
                                decoration: InputDecoration(
                                  hintText: '// Type or insert fields, functions, and operators here...',
                                  hintStyle: TextStyle(color: theme.textSecondary, fontSize: 12),
                                  border: InputBorder.none,
                                ),
                              ),
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),

                  // Right: Functions Catalog
                  Container(
                    width: 280,
                    decoration: BoxDecoration(
                      color: theme.surface,
                      border: Border(left: BorderSide(color: theme.border)),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        // Categories and Search
                        Container(
                          padding: const EdgeInsets.all(10),
                          decoration: BoxDecoration(
                            border: Border(bottom: BorderSide(color: theme.border)),
                          ),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text('Built-in functions', style: TextStyle(color: theme.textSecondary, fontSize: 11, fontWeight: FontWeight.bold)),
                              const SizedBox(height: 6),
                              Container(
                                padding: const EdgeInsets.symmetric(horizontal: 10),
                                decoration: BoxDecoration(
                                  color: theme.surfaceContainer,
                                  borderRadius: BorderRadius.circular(6),
                                  border: Border.all(color: theme.border),
                                ),
                                child: DropdownButtonHideUnderline(
                                  child: DropdownButton<String>(
                                    isExpanded: true,
                                    dropdownColor: theme.surface,
                                    value: _functionCategory,
                                    style: TextStyle(color: theme.textPrimary, fontSize: 12),
                                    items: ['All', ..._functionsByCategory.keys].map((cat) => DropdownMenuItem(value: cat, child: Text(cat))).toList(),
                                    onChanged: (val) {
                                      if (val != null) setState(() => _functionCategory = val);
                                    },
                                  ),
                                ),
                              ),
                              const SizedBox(height: 8),
                              SizedBox(
                                height: 32,
                                child: TextField(
                                  style: TextStyle(color: theme.textPrimary, fontSize: 12),
                                  decoration: InputDecoration(
                                    hintText: 'Search functions...',
                                    hintStyle: TextStyle(color: theme.textSecondary, fontSize: 11),
                                    prefixIcon: Icon(Icons.search, size: 16, color: theme.textSecondary),
                                    contentPadding: EdgeInsets.zero,
                                    filled: true,
                                    fillColor: theme.surfaceContainer,
                                    border: OutlineInputBorder(borderRadius: BorderRadius.circular(6), borderSide: BorderSide.none),
                                  ),
                                  onChanged: (v) => setState(() => _functionSearch = v),
                                ),
                              ),
                            ],
                          ),
                        ),
                        // Functions list
                        Expanded(
                          child: ListView(
                            padding: const EdgeInsets.symmetric(vertical: 4),
                            children: _getFilteredFunctions().map((fn) {
                              return InkWell(
                                onTap: () => _insertAtCursor(fn['syntax']!),
                                child: Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                                  decoration: BoxDecoration(
                                    border: Border(bottom: BorderSide(color: theme.border.withValues(alpha: 0.5), width: 0.5)),
                                  ),
                                  child: Column(
                                    crossAxisAlignment: CrossAxisAlignment.start,
                                    children: [
                                      Text(
                                        fn['name']!,
                                        style: TextStyle(
                                          color: theme.controlColor,
                                          fontSize: 12,
                                          fontWeight: FontWeight.bold,
                                          fontFamily: 'monospace',
                                        ),
                                      ),
                                      const SizedBox(height: 2),
                                      Text(
                                        fn['desc']!,
                                        style: TextStyle(color: theme.textSecondary, fontSize: 10),
                                        maxLines: 2,
                                        overflow: TextOverflow.ellipsis,
                                      ),
                                    ],
                                  ),
                                ),
                              );
                            }).toList(),
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),

            // Bottom Actions Bar
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
              decoration: BoxDecoration(
                color: theme.surfaceContainer,
                borderRadius: const BorderRadius.vertical(bottom: Radius.circular(12)),
                border: Border(top: BorderSide(color: theme.border)),
              ),
              child: Row(
                children: [
                  Icon(Icons.info_outline, color: theme.textSecondary, size: 16),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      'Double-click fields or functions to insert them into the formula.',
                      style: TextStyle(color: theme.textSecondary, fontSize: 11),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  const SizedBox(width: 12),
                  OutlinedButton(
                    onPressed: () => Navigator.of(context).pop(),
                    style: OutlinedButton.styleFrom(
                      foregroundColor: theme.textSecondary,
                      side: BorderSide(color: theme.border),
                    ),
                    child: const Text('Cancel'),
                  ),
                  const SizedBox(width: 12),
                  ElevatedButton(
                    onPressed: () {
                      Navigator.of(context).pop(_formulaController.text);
                    },
                    style: ElevatedButton.styleFrom(
                      backgroundColor: theme.primaryAccent,
                      foregroundColor: theme.isDark ? const Color(0xFF0B1120) : Colors.white,
                    ),
                    child: const Text('OK'),
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
