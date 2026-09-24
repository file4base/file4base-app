import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/api/api_client.dart';
import '../../main.dart';

class FieldOptionsDialog extends ConsumerStatefulWidget {
  final TableModel table;
  final ColumnModel column;

  const FieldOptionsDialog({
    super.key,
    required this.table,
    required this.column,
  });

  static Future<ColumnModel?> show(
    BuildContext context, {
    required TableModel table,
    required ColumnModel column,
  }) async {
    return showDialog<ColumnModel>(
      context: context,
      barrierDismissible: true,
      builder: (ctx) => FieldOptionsDialog(table: table, column: column),
    );
  }

  @override
  ConsumerState<FieldOptionsDialog> createState() => _FieldOptionsDialogState();
}

class _FieldOptionsDialogState extends ConsumerState<FieldOptionsDialog>
    with SingleTickerProviderStateMixin {
  late TabController _tabController;
  bool _isSaving = false;

  // --- Auto-Enter State ---
  bool _creationEnabled = false;
  String _creationType = 'Date';

  bool _modificationEnabled = false;
  String _modificationType = 'Date';

  bool _serialEnabled = false;
  String _serialGenerate = 'creation'; // 'creation' or 'commit'
  final TextEditingController _serialNextController = TextEditingController(text: '1');
  final TextEditingController _serialIncrementController = TextEditingController(text: '1');

  bool _valueFromLastVisited = false;

  bool _dataEnabled = false;
  final TextEditingController _dataValueController = TextEditingController();

  bool _calculatedValueEnabled = false;
  final TextEditingController _calculatedValueFormulaController = TextEditingController();
  bool _doNotReplaceExisting = false;

  bool _lookedUpValueEnabled = false;
  final TextEditingController _lookedUpTargetController = TextEditingController();

  bool _prohibitModification = false;

  // --- Validation State ---
  bool _notEmptyEnabled = false;
  String _notEmptyTiming = 'always'; // 'always' or 'entry'

  bool _uniqueValueEnabled = false;
  bool _existingValueEnabled = false;

  bool _strictDataTypeEnabled = false;
  String _strictDataType = 'Date';

  bool _rangeEnabled = false;
  final TextEditingController _rangeMinController = TextEditingController();
  final TextEditingController _rangeMaxController = TextEditingController();

  bool _maxLengthEnabled = false;
  final TextEditingController _maxLengthController = TextEditingController();

  bool _customMessageEnabled = false;
  final TextEditingController _customMessageController = TextEditingController();

  // --- Storage State ---
  bool _useGlobalStorage = false;
  String _indexingOption = 'all'; // 'none', 'minimal', 'all'
  bool _autoIndex = true;
  String _containerStorage = 'internal'; // 'internal' or 'external'

  // --- Calculation & Summary State ---
  final TextEditingController _calculationFormulaController = TextEditingController();
  String _calculationResultType = 'Text';

  String _summaryOperation = 'SUM';
  String? _summaryTargetColumn;
  bool _summaryRunningTotal = false;

  @override
  void initState() {
    super.initState();
    // Default to tab 3 if Calculation or Summary field, else Auto-Enter (tab 0)
    int initialIndex = 0;
    if (widget.column.fieldType == 'CALCULATION' || widget.column.fieldType == 'SUMMARY') {
      initialIndex = 3;
    }
    _tabController = TabController(length: 4, vsync: this, initialIndex: initialIndex);
    _initValuesFromColumn();
  }

  void _initValuesFromColumn() {
    // 1. Auto-Enter & Storage from defaultValue
    if (widget.column.defaultValue != null && widget.column.defaultValue!.isNotEmpty) {
      try {
        final Map<String, dynamic> data = jsonDecode(widget.column.defaultValue!);
        _creationEnabled = data['creation_enabled'] as bool? ?? false;
        _creationType = data['creation_type'] as String? ?? 'Date';
        _modificationEnabled = data['modification_enabled'] as bool? ?? false;
        _modificationType = data['modification_type'] as String? ?? 'Date';
        _serialEnabled = data['serial_enabled'] as bool? ?? false;
        _serialGenerate = data['serial_generate'] as String? ?? 'creation';
        _serialNextController.text = (data['serial_next'] ?? '1').toString();
        _serialIncrementController.text = (data['serial_increment'] ?? '1').toString();
        _valueFromLastVisited = data['value_last_visited'] as bool? ?? false;
        _dataEnabled = data['data_enabled'] as bool? ?? false;
        _dataValueController.text = (data['data_value'] ?? '').toString();
        _calculatedValueEnabled = data['calculated_enabled'] as bool? ?? false;
        _calculatedValueFormulaController.text = (data['calculated_formula'] ?? '').toString();
        _doNotReplaceExisting = data['do_not_replace'] as bool? ?? false;
        _lookedUpValueEnabled = data['looked_up_enabled'] as bool? ?? false;
        _lookedUpTargetController.text = (data['looked_up_target'] ?? '').toString();
        _prohibitModification = data['prohibit_modification'] as bool? ?? false;

        // Storage
        _useGlobalStorage = data['storage_global'] as bool? ?? false;
        _indexingOption = data['indexing'] as String? ?? 'all';
        _autoIndex = data['auto_index'] as bool? ?? true;
        _containerStorage = data['container_storage'] as String? ?? 'internal';
      } catch (_) {
        // Plain string default
        _dataEnabled = true;
        _dataValueController.text = widget.column.defaultValue!;
      }
    }

    // 2. Validation from validationRules
    if (widget.column.validationRules != null && widget.column.validationRules!.isNotEmpty) {
      try {
        final Map<String, dynamic> rules = jsonDecode(widget.column.validationRules!);
        _notEmptyEnabled = rules['not_empty'] as bool? ?? false;
        _notEmptyTiming = rules['not_empty_timing'] as String? ?? 'always';
        _uniqueValueEnabled = rules['unique'] as bool? ?? false;
        _existingValueEnabled = rules['existing_value'] as bool? ?? false;
        _strictDataTypeEnabled = rules['strict_type_enabled'] as bool? ?? false;
        _strictDataType = rules['strict_type'] as String? ?? 'Date';
        _rangeEnabled = rules['range_enabled'] as bool? ?? false;
        _rangeMinController.text = (rules['range_min'] ?? '').toString();
        _rangeMaxController.text = (rules['range_max'] ?? '').toString();
        _maxLengthEnabled = rules['max_length_enabled'] as bool? ?? false;
        _maxLengthController.text = (rules['max_length'] ?? '').toString();
        _customMessageEnabled = rules['custom_message_enabled'] as bool? ?? false;
        _customMessageController.text = (rules['custom_message'] ?? '').toString();
      } catch (_) {
        _customMessageEnabled = true;
        _customMessageController.text = widget.column.validationRules!;
      }
    }

    // 3. Calculation & Summary from calculationFormula
    if (widget.column.calculationFormula != null && widget.column.calculationFormula!.isNotEmpty) {
      try {
        final Map<String, dynamic> calc = jsonDecode(widget.column.calculationFormula!);
        if (calc.containsKey('operation')) {
          _summaryOperation = calc['operation'] as String? ?? 'SUM';
          _summaryTargetColumn = calc['target_column'] as String?;
          _summaryRunningTotal = calc['running_total'] as bool? ?? false;
        } else {
          _calculationFormulaController.text = (calc['formula'] ?? '').toString();
          _calculationResultType = calc['result_type'] as String? ?? 'Text';
        }
      } catch (_) {
        _calculationFormulaController.text = widget.column.calculationFormula!;
      }
    } else {
      if (_calculatedValueFormulaController.text.isNotEmpty) {
        _calculationFormulaController.text = _calculatedValueFormulaController.text;
      }
    }

    // Fallback summary target column
    if (_summaryTargetColumn == null && widget.table.columns.isNotEmpty) {
      final potential = widget.table.columns.where((c) => c.name != widget.column.name && !c.isPrimaryKey);
      if (potential.isNotEmpty) {
        _summaryTargetColumn = potential.first.name;
      }
    }
  }

  @override
  void dispose() {
    _tabController.dispose();
    _serialNextController.dispose();
    _serialIncrementController.dispose();
    _dataValueController.dispose();
    _calculatedValueFormulaController.dispose();
    _lookedUpTargetController.dispose();
    _rangeMinController.dispose();
    _rangeMaxController.dispose();
    _maxLengthController.dispose();
    _customMessageController.dispose();
    _calculationFormulaController.dispose();
    super.dispose();
  }

  Future<void> _handleSave() async {
    setState(() => _isSaving = true);

    try {
      // 1. Pack Auto-Enter & Storage JSON
      final autoEnterStorageMap = <String, dynamic>{
        'creation_enabled': _creationEnabled,
        'creation_type': _creationType,
        'modification_enabled': _modificationEnabled,
        'modification_type': _modificationType,
        'serial_enabled': _serialEnabled,
        'serial_generate': _serialGenerate,
        'serial_next': int.tryParse(_serialNextController.text.trim()) ?? 1,
        'serial_increment': int.tryParse(_serialIncrementController.text.trim()) ?? 1,
        'value_last_visited': _valueFromLastVisited,
        'data_enabled': _dataEnabled,
        'data_value': _dataValueController.text,
        'calculated_enabled': _calculatedValueEnabled,
        'calculated_formula': _calculatedValueFormulaController.text.trim(),
        'do_not_replace': _doNotReplaceExisting,
        'looked_up_enabled': _lookedUpValueEnabled,
        'looked_up_target': _lookedUpTargetController.text.trim(),
        'prohibit_modification': _prohibitModification,
        // Storage
        'storage_global': _useGlobalStorage,
        'indexing': _indexingOption,
        'auto_index': _autoIndex,
        'container_storage': _containerStorage,
      };
      final defaultValueJson = jsonEncode(autoEnterStorageMap);

      // 2. Pack Validation JSON
      final validationMap = <String, dynamic>{
        'not_empty': _notEmptyEnabled,
        'not_empty_timing': _notEmptyTiming,
        'unique': _uniqueValueEnabled,
        'existing_value': _existingValueEnabled,
        'strict_type_enabled': _strictDataTypeEnabled,
        'strict_type': _strictDataType,
        'range_enabled': _rangeEnabled,
        'range_min': _rangeMinController.text.trim(),
        'range_max': _rangeMaxController.text.trim(),
        'max_length_enabled': _maxLengthEnabled,
        'max_length': int.tryParse(_maxLengthController.text.trim()),
        'custom_message_enabled': _customMessageEnabled,
        'custom_message': _customMessageController.text.trim(),
      };
      final validationRulesJson = jsonEncode(validationMap);

      // 3. Pack Calculation / Summary JSON
      String? calculationFormulaStr;
      if (widget.column.fieldType == 'SUMMARY') {
        calculationFormulaStr = jsonEncode({
          'operation': _summaryOperation,
          'target_column': _summaryTargetColumn,
          'running_total': _summaryRunningTotal,
        });
      } else if (widget.column.fieldType == 'CALCULATION' || _calculationFormulaController.text.isNotEmpty) {
        calculationFormulaStr = jsonEncode({
          'formula': _calculationFormulaController.text.trim(),
          'result_type': _calculationResultType,
        });
      } else if (_calculatedValueFormulaController.text.isNotEmpty) {
        calculationFormulaStr = _calculatedValueFormulaController.text.trim();
      }

      final client = ref.read(apiClientProvider);
      final updatedCol = await client.updateColumn(
        widget.table.id,
        widget.column.id,
        displayName: widget.column.displayName,
        defaultValue: defaultValueJson,
        updateDefaultValue: true,
        calculationFormula: calculationFormulaStr,
        updateCalculationFormula: true,
        validationRules: validationRulesJson,
        updateValidationRules: true,
      );

      if (mounted) {
        Navigator.of(context).pop(updatedCol);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Field options for "${widget.column.displayName}" saved successfully.'),
            backgroundColor: Colors.teal.shade700,
            behavior: SnackBarBehavior.floating,
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        setState(() => _isSaving = false);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Error saving field options: $e'),
            backgroundColor: Colors.red.shade700,
            behavior: SnackBarBehavior.floating,
          ),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 40, vertical: 24),
      backgroundColor: Colors.transparent,
      child: Center(
        child: Container(
          width: 720,
          height: 620,
          decoration: BoxDecoration(
            color: isDark ? const Color(0xFF1E2024) : const Color(0xFFF7F8FA),
            borderRadius: BorderRadius.circular(14),
            border: Border.all(
              color: isDark ? Colors.white.withOpacity(0.12) : Colors.black.withOpacity(0.15),
              width: 1,
            ),
            boxShadow: [
              BoxShadow(
                color: Colors.black.withOpacity(isDark ? 0.6 : 0.25),
                blurRadius: 28,
                offset: const Offset(0, 14),
              ),
            ],
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              _buildHeader(theme, isDark),
              _buildTabBar(theme, isDark),
              Expanded(
                child: Container(
                  margin: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                  padding: const EdgeInsets.all(16),
                  decoration: BoxDecoration(
                    color: isDark ? const Color(0xFF282B30) : Colors.white,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(
                      color: isDark ? Colors.white.withOpacity(0.08) : Colors.black.withOpacity(0.08),
                    ),
                  ),
                  child: TabBarView(
                    controller: _tabController,
                    children: [
                      _buildAutoEnterTab(theme, isDark),
                      _buildValidationTab(theme, isDark),
                      _buildStorageTab(theme, isDark),
                      _buildCalculationTab(theme, isDark),
                    ],
                  ),
                ),
              ),
              _buildFooter(theme, isDark),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildHeader(ThemeData theme, bool isDark) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
      decoration: BoxDecoration(
        color: isDark ? const Color(0xFF22252A) : const Color(0xFFECEEF2),
        borderRadius: const BorderRadius.vertical(top: Radius.circular(13)),
        border: Border(
          bottom: BorderSide(
            color: isDark ? Colors.white.withOpacity(0.08) : Colors.black.withOpacity(0.08),
          ),
        ),
      ),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(Icons.tune, size: 18, color: theme.colorScheme.primary),
          const SizedBox(width: 8),
          Text(
            'Options for Field "${widget.column.displayName}"',
            style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 15),
          ),
          const SizedBox(width: 8),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
            decoration: BoxDecoration(
              color: theme.colorScheme.primary.withOpacity(0.12),
              borderRadius: BorderRadius.circular(4),
            ),
            child: Text(
              widget.column.fieldType,
              style: TextStyle(
                fontSize: 10,
                fontWeight: FontWeight.bold,
                color: theme.colorScheme.primary,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildTabBar(ThemeData theme, bool isDark) {
    return Padding(
      padding: const EdgeInsets.only(top: 10, left: 16, right: 16),
      child: Center(
        child: Container(
          decoration: BoxDecoration(
            color: isDark ? const Color(0xFF181A1D) : const Color(0xFFE4E7ED),
            borderRadius: BorderRadius.circular(8),
          ),
          padding: const EdgeInsets.all(3),
          child: TabBar(
            controller: _tabController,
            isScrollable: true,
            tabAlignment: TabAlignment.center,
            indicatorSize: TabBarIndicatorSize.tab,
            indicator: BoxDecoration(
              color: isDark ? const Color(0xFF333842) : Colors.white,
              borderRadius: BorderRadius.circular(6),
              boxShadow: [
                BoxShadow(
                  color: Colors.black.withOpacity(0.08),
                  blurRadius: 4,
                  offset: const Offset(0, 1),
                ),
              ],
            ),
            labelColor: theme.colorScheme.onSurface,
            unselectedLabelColor: theme.colorScheme.onSurface.withOpacity(0.6),
            labelStyle: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13),
            tabs: const [
              Tab(text: 'Auto-Enter'),
              Tab(text: 'Validation'),
              Tab(text: 'Storage'),
              Tab(text: 'Calculation & Summary'),
            ],
          ),
        ),
      ),
    );
  }

  // ==========================================
  // TAB 1: AUTO-ENTER
  // ==========================================
  Widget _buildAutoEnterTab(ThemeData theme, bool isDark) {
    final creationTypes = ['Date', 'Time', 'Timestamp', 'Name', 'Account Name'];

    return SingleChildScrollView(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Automatically enter the following data into this field:',
            style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500),
          ),
          const SizedBox(height: 12),

          // Creation
          Row(
            children: [
              Checkbox(
                value: _creationEnabled,
                onChanged: (v) => setState(() => _creationEnabled = v ?? false),
              ),
              const SizedBox(width: 80, child: Text('Creation')),
              const SizedBox(width: 8),
              SizedBox(
                width: 180,
                height: 32,
                child: DropdownButtonFormField<String>(
                  value: _creationType,
                  isDense: true,
                  decoration: const InputDecoration(
                    contentPadding: EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                    border: OutlineInputBorder(),
                  ),
                  items: creationTypes.map((t) => DropdownMenuItem(value: t, child: Text(t, style: const TextStyle(fontSize: 12)))).toList(),
                  onChanged: _creationEnabled ? (v) => setState(() => _creationType = v!) : null,
                ),
              ),
            ],
          ),

          // Modification
          Row(
            children: [
              Checkbox(
                value: _modificationEnabled,
                onChanged: (v) => setState(() => _modificationEnabled = v ?? false),
              ),
              const SizedBox(width: 80, child: Text('Modification')),
              const SizedBox(width: 8),
              SizedBox(
                width: 180,
                height: 32,
                child: DropdownButtonFormField<String>(
                  value: _modificationType,
                  isDense: true,
                  decoration: const InputDecoration(
                    contentPadding: EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                    border: OutlineInputBorder(),
                  ),
                  items: creationTypes.map((t) => DropdownMenuItem(value: t, child: Text(t, style: const TextStyle(fontSize: 12)))).toList(),
                  onChanged: _modificationEnabled ? (v) => setState(() => _modificationType = v!) : null,
                ),
              ),
            ],
          ),

          // Serial number
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Checkbox(
                value: _serialEnabled,
                onChanged: (v) => setState(() => _serialEnabled = v ?? false),
              ),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Padding(
                      padding: EdgeInsets.only(top: 8),
                      child: Text('Serial number'),
                    ),
                    Padding(
                      padding: const EdgeInsets.only(left: 8.0, top: 4),
                      child: Row(
                        children: [
                          const Text('Generate:', style: TextStyle(fontSize: 12, color: Colors.grey)),
                          const SizedBox(width: 8),
                          Radio<String>(
                            value: 'creation',
                            groupValue: _serialGenerate,
                            onChanged: _serialEnabled ? (v) => setState(() => _serialGenerate = v!) : null,
                          ),
                          const Text('On creation', style: TextStyle(fontSize: 12)),
                          const SizedBox(width: 12),
                          Radio<String>(
                            value: 'commit',
                            groupValue: _serialGenerate,
                            onChanged: _serialEnabled ? (v) => setState(() => _serialGenerate = v!) : null,
                          ),
                          const Text('On commit', style: TextStyle(fontSize: 12)),
                        ],
                      ),
                    ),
                    Padding(
                      padding: const EdgeInsets.only(left: 8.0, top: 4),
                      child: Row(
                        children: [
                          const Text('next value', style: TextStyle(fontSize: 12, color: Colors.grey)),
                          const SizedBox(width: 8),
                          SizedBox(
                            width: 80,
                            height: 30,
                            child: TextField(
                              controller: _serialNextController,
                              enabled: _serialEnabled,
                              style: const TextStyle(fontSize: 12),
                              decoration: const InputDecoration(
                                contentPadding: EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                                border: OutlineInputBorder(),
                              ),
                            ),
                          ),
                          const SizedBox(width: 16),
                          const Text('increment by', style: TextStyle(fontSize: 12, color: Colors.grey)),
                          const SizedBox(width: 8),
                          SizedBox(
                            width: 60,
                            height: 30,
                            child: TextField(
                              controller: _serialIncrementController,
                              enabled: _serialEnabled,
                              style: const TextStyle(fontSize: 12),
                              decoration: const InputDecoration(
                                contentPadding: EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                                border: OutlineInputBorder(),
                              ),
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),

          const SizedBox(height: 6),

          // Value from last visited record
          Row(
            children: [
              Checkbox(
                value: _valueFromLastVisited,
                onChanged: (v) => setState(() => _valueFromLastVisited = v ?? false),
              ),
              const Text('Value from last visited record'),
            ],
          ),

          // Data (Default static)
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Checkbox(
                value: _dataEnabled,
                onChanged: (v) => setState(() => _dataEnabled = v ?? false),
              ),
              const Padding(
                padding: EdgeInsets.only(top: 8.0),
                child: Text('Data:'),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: SizedBox(
                  height: 54,
                  child: TextField(
                    controller: _dataValueController,
                    enabled: _dataEnabled,
                    maxLines: 2,
                    style: const TextStyle(fontSize: 12),
                    decoration: const InputDecoration(
                      hintText: 'Default text or static value',
                      contentPadding: EdgeInsets.all(8),
                      border: OutlineInputBorder(),
                    ),
                  ),
                ),
              ),
            ],
          ),

          const SizedBox(height: 10),

          // Calculated value
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Checkbox(
                value: _calculatedValueEnabled,
                onChanged: (v) => setState(() => _calculatedValueEnabled = v ?? false),
              ),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        const Text('Calculated value'),
                        const SizedBox(width: 12),
                        OutlinedButton(
                          style: OutlinedButton.styleFrom(
                            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 2),
                            visualDensity: VisualDensity.compact,
                          ),
                          onPressed: _calculatedValueEnabled
                              ? () {
                                  _tabController.animateTo(3);
                                }
                              : null,
                          child: const Text('Specify...', style: TextStyle(fontSize: 11)),
                        ),
                      ],
                    ),
                    if (_calculatedValueEnabled)
                      Padding(
                        padding: const EdgeInsets.only(top: 6.0),
                        child: TextField(
                          controller: _calculatedValueFormulaController,
                          style: const TextStyle(fontSize: 12, fontFamily: 'monospace'),
                          decoration: const InputDecoration(
                            hintText: 'e.g. UPPER(first_name) || " " || UPPER(last_name)',
                            contentPadding: EdgeInsets.symmetric(horizontal: 8, vertical: 6),
                            border: OutlineInputBorder(),
                          ),
                        ),
                      ),
                    Padding(
                      padding: const EdgeInsets.only(top: 4.0),
                      child: Row(
                        children: [
                          Checkbox(
                            value: _doNotReplaceExisting,
                            onChanged: _calculatedValueEnabled
                                ? (v) => setState(() => _doNotReplaceExisting = v ?? false)
                                : null,
                          ),
                          const Text('Do not replace existing value of field (if any)', style: TextStyle(fontSize: 12)),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),

          // Looked-up value
          Row(
            children: [
              Checkbox(
                value: _lookedUpValueEnabled,
                onChanged: (v) => setState(() => _lookedUpValueEnabled = v ?? false),
              ),
              const Text('Looked-up value'),
              const SizedBox(width: 12),
              OutlinedButton(
                style: OutlinedButton.styleFrom(
                  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 2),
                  visualDensity: VisualDensity.compact,
                ),
                onPressed: _lookedUpValueEnabled ? () {} : null,
                child: const Text('Specify...', style: TextStyle(fontSize: 11)),
              ),
            ],
          ),

          const Divider(height: 24),

          // Prohibit modification of value during data entry
          Row(
            children: [
              Checkbox(
                value: _prohibitModification,
                onChanged: (v) => setState(() => _prohibitModification = v ?? false),
              ),
              const Text(
                'Prohibit modification of value during data entry',
                style: TextStyle(fontWeight: FontWeight.w500),
              ),
            ],
          ),
        ],
      ),
    );
  }

  // ==========================================
  // TAB 2: VALIDATION
  // ==========================================
  Widget _buildValidationTab(ThemeData theme, bool isDark) {
    final strictDataTypes = ['Date', 'Time of Day', '4-Digit Year', 'Numeric Only', 'Text Only'];

    return SingleChildScrollView(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Validate data in this field:',
            style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500),
          ),
          const SizedBox(height: 12),

          // Not empty
          Row(
            children: [
              Checkbox(
                value: _notEmptyEnabled,
                onChanged: (v) => setState(() => _notEmptyEnabled = v ?? false),
              ),
              const Text('Not empty'),
              if (_notEmptyEnabled) ...[
                const SizedBox(width: 20),
                Radio<String>(
                  value: 'always',
                  groupValue: _notEmptyTiming,
                  onChanged: (v) => setState(() => _notEmptyTiming = v!),
                ),
                const Text('Always', style: TextStyle(fontSize: 12)),
                const SizedBox(width: 8),
                Radio<String>(
                  value: 'entry',
                  groupValue: _notEmptyTiming,
                  onChanged: (v) => setState(() => _notEmptyTiming = v!),
                ),
                const Text('Only during data entry', style: TextStyle(fontSize: 12)),
              ],
            ],
          ),

          // Unique value
          Row(
            children: [
              Checkbox(
                value: _uniqueValueEnabled,
                onChanged: (v) => setState(() => _uniqueValueEnabled = v ?? false),
              ),
              const Text('Unique value'),
              const SizedBox(width: 32),
              Checkbox(
                value: _existingValueEnabled,
                onChanged: (v) => setState(() => _existingValueEnabled = v ?? false),
              ),
              const Text('Existing value'),
            ],
          ),

          // Strict data type
          Row(
            children: [
              Checkbox(
                value: _strictDataTypeEnabled,
                onChanged: (v) => setState(() => _strictDataTypeEnabled = v ?? false),
              ),
              const Text('Strict data type:'),
              const SizedBox(width: 12),
              SizedBox(
                width: 160,
                height: 32,
                child: DropdownButtonFormField<String>(
                  value: _strictDataType,
                  isDense: true,
                  decoration: const InputDecoration(
                    contentPadding: EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                    border: OutlineInputBorder(),
                  ),
                  items: strictDataTypes.map((t) => DropdownMenuItem(value: t, child: Text(t, style: const TextStyle(fontSize: 12)))).toList(),
                  onChanged: _strictDataTypeEnabled ? (v) => setState(() => _strictDataType = v!) : null,
                ),
              ),
            ],
          ),

          const SizedBox(height: 6),

          // Range
          Row(
            children: [
              Checkbox(
                value: _rangeEnabled,
                onChanged: (v) => setState(() => _rangeEnabled = v ?? false),
              ),
              const Text('Range:'),
              const SizedBox(width: 8),
              const Text('from', style: TextStyle(fontSize: 12, color: Colors.grey)),
              const SizedBox(width: 6),
              SizedBox(
                width: 80,
                height: 30,
                child: TextField(
                  controller: _rangeMinController,
                  enabled: _rangeEnabled,
                  style: const TextStyle(fontSize: 12),
                  decoration: const InputDecoration(
                    contentPadding: EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                    border: OutlineInputBorder(),
                  ),
                ),
              ),
              const SizedBox(width: 8),
              const Text('to', style: TextStyle(fontSize: 12, color: Colors.grey)),
              const SizedBox(width: 6),
              SizedBox(
                width: 80,
                height: 30,
                child: TextField(
                  controller: _rangeMaxController,
                  enabled: _rangeEnabled,
                  style: const TextStyle(fontSize: 12),
                  decoration: const InputDecoration(
                    contentPadding: EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                    border: OutlineInputBorder(),
                  ),
                ),
              ),
            ],
          ),

          const SizedBox(height: 6),

          // Max length
          Row(
            children: [
              Checkbox(
                value: _maxLengthEnabled,
                onChanged: (v) => setState(() => _maxLengthEnabled = v ?? false),
              ),
              const Text('Maximum number of characters:'),
              const SizedBox(width: 12),
              SizedBox(
                width: 80,
                height: 30,
                child: TextField(
                  controller: _maxLengthController,
                  enabled: _maxLengthEnabled,
                  keyboardType: TextInputType.number,
                  style: const TextStyle(fontSize: 12),
                  decoration: const InputDecoration(
                    contentPadding: EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                    border: OutlineInputBorder(),
                  ),
                ),
              ),
            ],
          ),

          const Divider(height: 24),

          // Custom message
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Checkbox(
                value: _customMessageEnabled,
                onChanged: (v) => setState(() => _customMessageEnabled = v ?? false),
              ),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Padding(
                      padding: EdgeInsets.only(top: 8.0),
                      child: Text('Display custom message if validation fails:'),
                    ),
                    const SizedBox(height: 6),
                    TextField(
                      controller: _customMessageController,
                      enabled: _customMessageEnabled,
                      maxLines: 2,
                      style: const TextStyle(fontSize: 12),
                      decoration: const InputDecoration(
                        hintText: 'e.g. Please enter a valid 9-digit corporate VAT number.',
                        contentPadding: EdgeInsets.all(8),
                        border: OutlineInputBorder(),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  // ==========================================
  // TAB 3: STORAGE & INDEXING
  // ==========================================
  Widget _buildStorageTab(ThemeData theme, bool isDark) {
    final isContainer = widget.column.fieldType == 'CONTAINER';

    return SingleChildScrollView(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Global Storage & Indexing Settings:',
            style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500),
          ),
          const SizedBox(height: 12),

          // Global storage
          Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: isDark ? Colors.black.withOpacity(0.2) : Colors.black.withOpacity(0.02),
              borderRadius: BorderRadius.circular(8),
              border: Border.all(
                color: isDark ? Colors.white.withOpacity(0.06) : Colors.black.withOpacity(0.06),
              ),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Checkbox(
                      value: _useGlobalStorage,
                      onChanged: (v) => setState(() => _useGlobalStorage = v ?? false),
                    ),
                    const Text('Use global storage (one value for all records)', style: TextStyle(fontWeight: FontWeight.w600)),
                  ],
                ),
                Padding(
                  padding: const EdgeInsets.only(left: 40.0),
                  child: Text(
                    'A global field stores a single fixed value shared across all records in the table. Ideal for company logos, VAT tax rates, or temporary session states.',
                    style: TextStyle(fontSize: 11, color: isDark ? Colors.grey.shade400 : Colors.grey.shade700),
                  ),
                ),
              ],
            ),
          ),

          const SizedBox(height: 16),

          // Indexing
          const Text('Indexing Options:', style: TextStyle(fontWeight: FontWeight.w600)),
          const SizedBox(height: 6),
          Padding(
            padding: const EdgeInsets.only(left: 8.0),
            child: Column(
              children: [
                Row(
                  children: [
                    Radio<String>(
                      value: 'none',
                      groupValue: _indexingOption,
                      onChanged: (v) => setState(() => _indexingOption = v!),
                    ),
                    const Text('None'),
                    const SizedBox(width: 24),
                    Radio<String>(
                      value: 'minimal',
                      groupValue: _indexingOption,
                      onChanged: (v) => setState(() => _indexingOption = v!),
                    ),
                    const Text('Minimal'),
                    const SizedBox(width: 24),
                    Radio<String>(
                      value: 'all',
                      groupValue: _indexingOption,
                      onChanged: (v) => setState(() => _indexingOption = v!),
                    ),
                    const Text('All (Full-text & Fast Search)'),
                  ],
                ),
                Row(
                  children: [
                    Checkbox(
                      value: _autoIndex,
                      onChanged: (v) => setState(() => _autoIndex = v ?? false),
                    ),
                    const Text('Automatically create indexes as needed', style: TextStyle(fontSize: 12)),
                  ],
                ),
              ],
            ),
          ),

          const Divider(height: 24),

          // Container Storage
          Row(
            children: [
              Icon(Icons.perm_media_outlined, size: 18, color: isContainer ? theme.colorScheme.primary : Colors.grey),
              const SizedBox(width: 8),
              Text(
                'Container Storage (Files / Binaries):',
                style: TextStyle(
                  fontWeight: FontWeight.w600,
                  color: isContainer ? null : Colors.grey,
                ),
              ),
              if (!isContainer)
                const Padding(
                  padding: EdgeInsets.only(left: 8.0),
                  child: Text('(Available for CONTAINER fields)', style: TextStyle(fontSize: 11, color: Colors.grey)),
                ),
            ],
          ),
          const SizedBox(height: 8),
          Padding(
            padding: const EdgeInsets.only(left: 8.0),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Radio<String>(
                      value: 'internal',
                      groupValue: _containerStorage,
                      onChanged: isContainer ? (v) => setState(() => _containerStorage = v!) : null,
                    ),
                    Text(
                      'Store container data internally (database bytea / blob)',
                      style: TextStyle(fontSize: 12, color: isContainer ? null : Colors.grey),
                    ),
                  ],
                ),
                Row(
                  children: [
                    Radio<String>(
                      value: 'external',
                      groupValue: _containerStorage,
                      onChanged: isContainer ? (v) => setState(() => _containerStorage = v!) : null,
                    ),
                    Text(
                      'Store container data externally (dedicated uploads filesystem folder)',
                      style: TextStyle(fontSize: 12, color: isContainer ? null : Colors.grey),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  // ==========================================
  // TAB 4: CALCULATION & SUMMARY
  // ==========================================
  Widget _buildCalculationTab(ThemeData theme, bool isDark) {
    final resultTypes = ['Text', 'Number', 'Date', 'Time', 'Timestamp'];
    final isCalculation = widget.column.fieldType == 'CALCULATION';
    final isSummary = widget.column.fieldType == 'SUMMARY';

    return SingleChildScrollView(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Section 1: Calculation Formula
          Row(
            children: [
              Icon(Icons.functions, size: 18, color: theme.colorScheme.primary),
              const SizedBox(width: 8),
              Text(
                'Calculation Formula Editor',
                style: TextStyle(
                  fontWeight: FontWeight.bold,
                  fontSize: 14,
                  color: isCalculation ? theme.colorScheme.primary : null,
                ),
              ),
              if (isCalculation)
                const Padding(
                  padding: EdgeInsets.only(left: 8.0),
                  child: Chip(
                    label: Text('Field Type: Calculation', style: TextStyle(fontSize: 10)),
                    padding: EdgeInsets.zero,
                    visualDensity: VisualDensity.compact,
                  ),
                ),
            ],
          ),
          const SizedBox(height: 8),

          // Function shortcuts
          Wrap(
            spacing: 6,
            runSpacing: 4,
            children: [
              _buildFunctionChip('UPPER()', 'UPPER()'),
              _buildFunctionChip('LOWER()', 'LOWER()'),
              _buildFunctionChip('CONCAT(a, b)', ' || '),
              _buildFunctionChip('SUM(a, b)', ' + '),
              _buildFunctionChip('ROUND(n, 2)', 'ROUND(field, 2)'),
              _buildFunctionChip('IF(cond, a, b)', 'CASE WHEN cond THEN a ELSE b END'),
              _buildFunctionChip('CURRENT_DATE', 'CURRENT_DATE'),
            ],
          ),
          const SizedBox(height: 8),

          TextField(
            controller: _calculationFormulaController,
            maxLines: 4,
            style: const TextStyle(fontFamily: 'monospace', fontSize: 13),
            decoration: const InputDecoration(
              hintText: 'Enter formula, e.g. price * quantity * (1 + tax_rate / 100)',
              contentPadding: EdgeInsets.all(10),
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 8),

          Row(
            children: [
              const Text('Calculation result is:', style: TextStyle(fontSize: 12, fontWeight: FontWeight.w500)),
              const SizedBox(width: 12),
              SizedBox(
                width: 140,
                height: 32,
                child: DropdownButtonFormField<String>(
                  value: _calculationResultType,
                  isDense: true,
                  decoration: const InputDecoration(
                    contentPadding: EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                    border: OutlineInputBorder(),
                  ),
                  items: resultTypes.map((t) => DropdownMenuItem(value: t, child: Text(t, style: const TextStyle(fontSize: 12)))).toList(),
                  onChanged: (v) => setState(() => _calculationResultType = v!),
                ),
              ),
            ],
          ),

          const Divider(height: 28),

          // Section 2: Summary Field
          Row(
            children: [
              Icon(Icons.query_stats, size: 18, color: Colors.teal),
              const SizedBox(width: 8),
              Text(
                'Summary Aggregate Options',
                style: TextStyle(
                  fontWeight: FontWeight.bold,
                  fontSize: 14,
                  color: isSummary ? Colors.teal : null,
                ),
              ),
              if (isSummary)
                const Padding(
                  padding: EdgeInsets.only(left: 8.0),
                  child: Chip(
                    label: Text('Field Type: Summary', style: TextStyle(fontSize: 10)),
                    padding: EdgeInsets.zero,
                    visualDensity: VisualDensity.compact,
                  ),
                ),
            ],
          ),
          const SizedBox(height: 12),

          Row(
            children: [
              const Text('Operation:', style: TextStyle(fontSize: 12, fontWeight: FontWeight.w500)),
              const SizedBox(width: 8),
              SizedBox(
                width: 160,
                height: 32,
                child: DropdownButtonFormField<String>(
                  value: _summaryOperation,
                  isDense: true,
                  decoration: const InputDecoration(
                    contentPadding: EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                    border: OutlineInputBorder(),
                  ),
                  items: const [
                    DropdownMenuItem(value: 'SUM', child: Text('Total of (Sum)', style: TextStyle(fontSize: 12))),
                    DropdownMenuItem(value: 'AVG', child: Text('Average of', style: TextStyle(fontSize: 12))),
                    DropdownMenuItem(value: 'COUNT', child: Text('Count of', style: TextStyle(fontSize: 12))),
                    DropdownMenuItem(value: 'MIN', child: Text('Minimum of', style: TextStyle(fontSize: 12))),
                    DropdownMenuItem(value: 'MAX', child: Text('Maximum of', style: TextStyle(fontSize: 12))),
                  ],
                  onChanged: (v) => setState(() => _summaryOperation = v!),
                ),
              ),
              const SizedBox(width: 16),
              const Text('of field:', style: TextStyle(fontSize: 12, fontWeight: FontWeight.w500)),
              const SizedBox(width: 8),
              Expanded(
                child: SizedBox(
                  height: 32,
                  child: DropdownButtonFormField<String>(
                    value: _summaryTargetColumn,
                    isDense: true,
                    decoration: const InputDecoration(
                      contentPadding: EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                      border: OutlineInputBorder(),
                    ),
                    items: widget.table.columns
                        .where((c) => c.name != widget.column.name)
                        .map((c) => DropdownMenuItem(value: c.name, child: Text('${c.displayName} (${c.fieldType})', style: const TextStyle(fontSize: 12))))
                        .toList(),
                    onChanged: (v) => setState(() => _summaryTargetColumn = v),
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),

          Row(
            children: [
              Checkbox(
                value: _summaryRunningTotal,
                onChanged: (v) => setState(() => _summaryRunningTotal = v ?? false),
              ),
              const Text('Running total', style: TextStyle(fontSize: 12)),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildFunctionChip(String label, String insertSnippet) {
    return ActionChip(
      label: Text(label, style: const TextStyle(fontSize: 11)),
      padding: EdgeInsets.zero,
      visualDensity: VisualDensity.compact,
      onPressed: () {
        final text = _calculationFormulaController.text;
        final sel = _calculationFormulaController.selection;
        if (sel.isValid && sel.start >= 0) {
          final newText = text.replaceRange(sel.start, sel.end, insertSnippet);
          _calculationFormulaController.text = newText;
          _calculationFormulaController.selection = TextSelection.collapsed(offset: sel.start + insertSnippet.length);
        } else {
          _calculationFormulaController.text = '$text$insertSnippet';
        }
      },
    );
  }

  // ==========================================
  // FOOTER (CANCEL / OK)
  // ==========================================
  Widget _buildFooter(ThemeData theme, bool isDark) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
      decoration: BoxDecoration(
        color: isDark ? const Color(0xFF22252A) : const Color(0xFFECEEF2),
        borderRadius: const BorderRadius.vertical(bottom: Radius.circular(13)),
        border: Border(
          top: BorderSide(
            color: isDark ? Colors.white.withOpacity(0.08) : Colors.black.withOpacity(0.08),
          ),
        ),
      ),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.end,
        children: [
          OutlinedButton(
            onPressed: _isSaving ? null : () => Navigator.of(context).pop(),
            style: OutlinedButton.styleFrom(
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 10),
            ),
            child: const Text('Cancel'),
          ),
          const SizedBox(width: 12),
          FilledButton(
            onPressed: _isSaving ? null : _handleSave,
            style: FilledButton.styleFrom(
              padding: const EdgeInsets.symmetric(horizontal: 28, vertical: 10),
            ),
            child: _isSaving
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white),
                  )
                : const Text('OK'),
          ),
        ],
      ),
    );
  }
}
