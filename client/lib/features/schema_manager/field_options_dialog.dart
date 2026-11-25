import 'dart:async';
import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/schema_manager/lookup_options_dialog.dart';
import 'package:file4base_client/features/script_workspace/calculation_builder_dialog.dart';
import 'package:file4base_client/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// The values a creation or modification stamp can enter, as in FileMaker.
const kAutoEnterStampTypes = ['Date', 'Time', 'Timestamp', 'Name', 'Account Name'];

/// Why a serial number's [next] value and [increment] cannot be saved for a
/// field of [fieldType], or null when they can: the increment is a whole
/// number of at least 1, the next value has a digit to count with, and a
/// number field's next value is a whole number. The server checks the same.
String? serialOptionsError(String next, String increment, {String fieldType = 'TEXT'}) {
  final step = int.tryParse(increment.trim());
  if (step == null || step < 1) return 'Increment by a whole number of at least 1.';
  final value = next.trim();
  if (!RegExp(r'\d').hasMatch(value)) return 'The next value needs a number to count with.';
  if (fieldType == 'NUMBER' && !RegExp(r'^-?\d+$').hasMatch(value)) {
    return 'The next value of a number field is a whole number.';
  }
  return null;
}

/// Why [text] is not a valid maximum number of repetitions, or null when it
/// is one: a whole number from 1 to [ColumnModel.repetitionsLimit], as in
/// FileMaker. A summary field cannot repeat.
String? repetitionsError(String text, {String fieldType = 'TEXT'}) {
  final n = int.tryParse(text.trim());
  if (n == null || n < 1 || n > ColumnModel.repetitionsLimit) {
    return 'Enter a whole number from 1 to ${ColumnModel.repetitionsLimit}.';
  }
  if (n > 1 && fieldType == 'SUMMARY') {
    return 'A summary field cannot have repetitions.';
  }
  return null;
}

class FieldOptionsDialog extends ConsumerStatefulWidget {

  const FieldOptionsDialog({
    required this.table, required this.column, super.key,
  });
  final TableModel table;
  final ColumnModel column;

  static Future<ColumnModel?> show(
    BuildContext context, {
    required TableModel table,
    required ColumnModel column,
  }) async {
    return showDialog<ColumnModel>(
      context: context,
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

  /// Whether the next serial value was typed into: only then is it saved,
  /// so a dialog left open while records are created does not move the
  /// counter back.
  bool _serialNextEdited = false;
  String? _serialError;

  bool _calculatedValueEnabled = false;
  final TextEditingController _calculatedValueFormulaController = TextEditingController();
  bool _doNotReplaceExisting = false;

  /// The occurrence of this table the auto-enter formula is evaluated from;
  /// empty is the default one.
  String _calculatedContext = '';

  /// Do not evaluate if all referenced fields are empty: on unless the
  /// options say otherwise, as in FileMaker.
  bool _calculatedSkipIfEmpty = true;

  bool _lookedUpValueEnabled = false;
  LookupSpec _lookup = const LookupSpec();

  bool _prohibitModification = false;

  /// Why the Auto-Enter options cannot be saved as they are.
  String? _autoEnterError;

  /// The options as they were read, so keys this dialog does not show are
  /// saved back unchanged.
  Map<String, dynamic> _storedOptions = const {};

  // The solution's graph, for the auto-enter formula's context and the
  // lookup. Empty until loaded, or when it cannot be read.
  List<TableOccurrenceModel> _occurrences = const [];
  List<RelationshipModel> _relationships = const [];
  List<TableModel> _tables = const [];

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
  late final TextEditingController _repetitionsController =
      TextEditingController(text: widget.column.maxRepetitions.toString());
  String? _repetitionsError;

  // --- Calculation & Summary State ---
  final TextEditingController _calculationFormulaController = TextEditingController();
  String _calculationResultType = 'Text';

  /// Unchecked computes the field each time a record is shown, which is how
  /// it reads related records; the occurrence it reads them from is kept.
  bool _storeCalculationResult = true;
  String? _calculationContext;

  /// One of [SummaryType] (#32).
  String _summaryOperation = SummaryType.total;
  String? _summaryTargetColumn;
  bool _summaryRunningTotal = false;

  @override
  void initState() {
    super.initState();
    // Default to tab 3 if Calculation or Summary field, else Auto-Enter (tab 0)
    var initialIndex = 0;
    if (widget.column.fieldType == 'CALCULATION' || widget.column.fieldType == 'SUMMARY') {
      initialIndex = 3;
    }
    _tabController = TabController(length: 4, vsync: this, initialIndex: initialIndex);
    _initValuesFromColumn();
    unawaited(_loadGraph());
  }

  Future<void> _loadGraph() async {
    final client = ref.read(apiClientProvider);
    try {
      final results = await Future.wait<Object>([
        client.listOccurrences(),
        client.listRelationships(),
        client.listTables(),
      ]);
      if (!mounted) return;
      setState(() {
        _occurrences = results[0] as List<TableOccurrenceModel>;
        _relationships = results[1] as List<RelationshipModel>;
        _tables = results[2] as List<TableModel>;
      });
    } on Object catch (_) {
      // Without the graph the formula editor and the lookup offer what they
      // can; the options already set are kept.
    }
  }

  /// The occurrences of this field's table, and the default one's name.
  List<TableOccurrenceModel> get _ownOccurrences =>
      _occurrences.where((o) => o.baseTableId == widget.table.id).toList();

  String get _defaultOccurrenceName =>
      defaultOccurrenceOf(widget.table.id, widget.table.name, _occurrences)?.name ?? widget.table.name;

  /// The fields of an occurrence's table: from the solution's tables, or for
  /// another solution's table, from the server.
  Future<List<ColumnModel>> _fieldsOf(TableOccurrenceModel occurrence) async {
    final local = occurrence.baseTableId == null
        ? null
        : _tables.where((t) => t.id == occurrence.baseTableId).firstOrNull ??
            (occurrence.baseTableId == widget.table.id ? widget.table : null);
    if (local != null && local.columns.isNotEmpty) return local.columns;
    final table = await ref.read(apiClientProvider).getOccurrenceTable(occurrence.id);
    return table.columns;
  }

  Future<void> _specifyCalculatedValue() async {
    final fieldsByOccurrence = <String, List<String>>{};
    for (final o in _occurrences) {
      final table = _tables.where((t) => t.id == o.baseTableId).firstOrNull ??
          (o.baseTableId == widget.table.id ? widget.table : null);
      if (table != null) fieldsByOccurrence[o.name] = [for (final c in table.columns) c.name];
    }
    final start = _calculatedContext.isNotEmpty ? _calculatedContext : _defaultOccurrenceName;
    fieldsByOccurrence.putIfAbsent(start, () => [for (final c in widget.table.columns) c.name]);
    final formula = await CalculationBuilderDialog.show(
      context,
      initialFormula: _calculatedValueFormulaController.text,
      contextTable: start,
      availableTables: fieldsByOccurrence.keys.toList(),
      fieldsByTable: fieldsByOccurrence,
    );
    if (!mounted) return;
    if (formula == null) {
      // Cancelled with no formula: the option stays off, as in FileMaker.
      if (_calculatedValueFormulaController.text.trim().isEmpty) {
        setState(() => _calculatedValueEnabled = false);
      }
      return;
    }
    setState(() {
      _calculatedValueFormulaController.text = formula.trim();
      if (formula.trim().isNotEmpty) _calculatedValueEnabled = true;
    });
  }

  Future<void> _specifyLookup() async {
    final spec = await LookupOptionsDialog.show(
      context,
      fieldName: widget.column.displayName,
      tableId: widget.table.id,
      tableName: widget.table.name,
      occurrences: _occurrences,
      relationships: _relationships,
      loadFields: _fieldsOf,
      initial: _lookup,
    );
    if (!mounted) return;
    if (spec == null) {
      if (!_lookup.isComplete) setState(() => _lookedUpValueEnabled = false);
      return;
    }
    setState(() {
      _lookup = spec;
      _lookedUpValueEnabled = true;
    });
  }

  void _initValuesFromColumn() {
    // 1. Auto-Enter & Storage from defaultValue
    if (widget.column.defaultValue != null && widget.column.defaultValue!.isNotEmpty) {
      try {
        final data = jsonDecode(widget.column.defaultValue!) as Map<String, dynamic>;
        _storedOptions = data;
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
        _calculatedContext = data['calculated_context']?.toString() ?? '';
        _calculatedSkipIfEmpty = data['calculated_skip_if_empty'] != false;
        _lookedUpValueEnabled = data['looked_up_enabled'] as bool? ?? false;
        _lookup = LookupSpec.fromOptions(data);
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
        final rules = jsonDecode(widget.column.validationRules!) as Map<String, dynamic>;
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
        final calc = jsonDecode(widget.column.calculationFormula!) as Map<String, dynamic>;
        // A summary's definition, in either the shape saved now or the one
        // saved before they were computed (#32).
        final summary = SummarySpecModel.tryParse(widget.column.calculationFormula);
        if (summary != null && widget.column.fieldType == 'SUMMARY') {
          _summaryOperation = summary.summaryType;
          _summaryTargetColumn = summary.field;
          _summaryRunningTotal = summary.running;
        } else if (calc.containsKey('operation')) {
          _summaryTargetColumn = calc['target_column'] as String?;
          _summaryRunningTotal = calc['running_total'] as bool? ?? false;
        } else {
          _calculationFormulaController.text = (calc['formula'] ?? '').toString();
          _calculationResultType = calc['result_type'] as String? ?? 'Text';
          _storeCalculationResult = calc['stored'] != false;
          _calculationContext = calc['context'] as String?;
        }
      } catch (_) {
        // Older clients also copied the auto-enter formula here; it is not
        // the field's own formula, and saving drops it.
        if (widget.column.calculationFormula != _calculatedValueFormulaController.text) {
          _calculationFormulaController.text = widget.column.calculationFormula!;
        }
      }
    }

    // Fallback summary target column
    // Only a field the summary can take is offered, so only such a field
    // may be the one selected: any other has no item and cannot be shown.
    if (_summaryTargetColumn == null && widget.table.columns.isNotEmpty) {
      final potential = _summarizableColumns.where((c) => !c.isPrimaryKey);
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
    _rangeMinController.dispose();
    _rangeMaxController.dispose();
    _maxLengthController.dispose();
    _customMessageController.dispose();
    _calculationFormulaController.dispose();
    _repetitionsController.dispose();
    super.dispose();
  }

  Future<void> _handleSave() async {
    final repetitionsProblem = repetitionsError(_repetitionsController.text, fieldType: widget.column.fieldType);
    if (repetitionsProblem != null) {
      setState(() => _repetitionsError = repetitionsProblem);
      _tabController.animateTo(2);
      return;
    }
    final maxRepetitions = int.parse(_repetitionsController.text.trim());
    final autoEnterProblem = _autoEnterProblem();
    setState(() {
      _serialError = autoEnterProblem.$1;
      _autoEnterError = autoEnterProblem.$2;
    });
    if (autoEnterProblem.$1 != null || autoEnterProblem.$2 != null) {
      _tabController.animateTo(0);
      return;
    }
    setState(() => _isSaving = true);

    try {
      // 1. Pack Auto-Enter & Storage JSON
      final defaultValueJson = jsonEncode(_fieldOptionsToSave());

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
        calculationFormulaStr = jsonEncode(SummarySpecModel(
          summaryType: _summaryOperation,
          field: _summaryTargetColumn ?? '',
          running: _summaryRunningTotal,
        ).toJson());
      } else if (widget.column.fieldType == 'CALCULATION' || _calculationFormulaController.text.isNotEmpty) {
        // The auto-enter formula is kept with the other auto-enter options,
        // not here: this is the calculation field's own formula.
        calculationFormulaStr = jsonEncode({
          'formula': _calculationFormulaController.text.trim(),
          'result_type': _calculationResultType,
          if (!_storeCalculationResult) 'stored': false,
          if (_calculationContext != null && _calculationContext!.isNotEmpty) 'context': _calculationContext,
        });
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
        maxRepetitions: maxRepetitions != widget.column.maxRepetitions ? maxRepetitions : null,
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

  /// What stops the Auto-Enter options from being saved: the serial
  /// number's problem, then any other.
  (String?, String?) _autoEnterProblem() {
    final serial = _serialEnabled
        ? serialOptionsError(_serialNextController.text, _serialIncrementController.text,
            fieldType: widget.column.fieldType)
        : null;
    String? other;
    if (_calculatedValueEnabled && _calculatedValueFormulaController.text.trim().isEmpty) {
      other = 'Specify the formula of the calculated value.';
    } else if (_lookedUpValueEnabled && !_lookup.isComplete) {
      other = 'Specify the table and the field the looked-up value is copied from.';
    }
    return (serial, other);
  }

  /// The field's options as saved in its `default_value`: the Auto-Enter and
  /// Storage tabs' keys over the keys read, which are kept. The next serial
  /// value is sent only when it was edited.
  Map<String, dynamic> _fieldOptionsToSave() {
    final options = Map<String, dynamic>.of(_storedOptions)
      ..remove('serial_next')
      // Older clients kept the lookup as one text; it is now its own keys.
      ..remove('looked_up_target')
      ..addAll({
        'creation_enabled': _creationEnabled,
        'creation_type': _creationType,
        'modification_enabled': _modificationEnabled,
        'modification_type': _modificationType,
        'serial_enabled': _serialEnabled,
        'serial_generate': _serialGenerate,
        if (_serialNextEdited) 'serial_next': _serialNextController.text.trim(),
        'serial_increment': int.tryParse(_serialIncrementController.text.trim()) ?? 1,
        'value_last_visited': _valueFromLastVisited,
        'data_enabled': _dataEnabled,
        'data_value': _dataValueController.text,
        'calculated_enabled': _calculatedValueEnabled,
        'calculated_formula': _calculatedValueFormulaController.text.trim(),
        'calculated_context': _calculatedContext,
        'do_not_replace': _doNotReplaceExisting,
        'calculated_skip_if_empty': _calculatedSkipIfEmpty,
        'looked_up_enabled': _lookedUpValueEnabled,
        ..._lookup.toOptions(),
        'prohibit_modification': _prohibitModification,
        // Storage
        'storage_global': _useGlobalStorage,
        'indexing': _indexingOption,
        'auto_index': _autoIndex,
        'container_storage': _containerStorage,
      });
    return options;
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
              color: isDark ? Colors.white.withValues(alpha: 0.12) : Colors.black.withValues(alpha: 0.15),
            ),
            boxShadow: [
              BoxShadow(
                color: Colors.black.withValues(alpha: isDark ? 0.6 : 0.25),
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
                      color: isDark ? Colors.white.withValues(alpha: 0.08) : Colors.black.withValues(alpha: 0.08),
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
            color: isDark ? Colors.white.withValues(alpha: 0.08) : Colors.black.withValues(alpha: 0.08),
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
              color: theme.colorScheme.primary.withValues(alpha: 0.12),
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
                  color: Colors.black.withValues(alpha: 0.08),
                  blurRadius: 4,
                  offset: const Offset(0, 1),
                ),
              ],
            ),
            labelColor: theme.colorScheme.onSurface,
            unselectedLabelColor: theme.colorScheme.onSurface.withValues(alpha: 0.6),
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
    const small = TextStyle(fontSize: 12);
    const hint = TextStyle(fontSize: 11, color: Colors.grey);
    final own = _ownOccurrences;
    final contextNames = {for (final o in own) o.name, if (_calculatedContext.isNotEmpty) _calculatedContext};

    Widget stamp({
      required String label,
      required Key checkKey,
      required Key typeKey,
      required bool enabled,
      required String type,
      required ValueChanged<bool> onEnabled,
      required ValueChanged<String> onType,
    }) {
      return Row(
        children: [
          Checkbox(key: checkKey, value: enabled, onChanged: (v) => onEnabled(v ?? false)),
          SizedBox(width: 90, child: Text(label)),
          SizedBox(
            width: 180,
            height: 32,
            child: DropdownButton<String>(
              key: typeKey,
              isExpanded: true,
              value: kAutoEnterStampTypes.contains(type) ? type : kAutoEnterStampTypes.first,
              items: [
                for (final t in kAutoEnterStampTypes) DropdownMenuItem(value: t, child: Text(t, style: small)),
              ],
              onChanged: enabled ? (v) => onType(v ?? type) : null,
            ),
          ),
        ],
      );
    }

    return SingleChildScrollView(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Automatically enter the following data into this field:',
            style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500),
          ),
          if (_autoEnterError != null)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Text(_autoEnterError!,
                  key: const ValueKey('auto-enter-error'),
                  style: TextStyle(fontSize: 12, color: theme.colorScheme.error)),
            ),
          const SizedBox(height: 8),

          stamp(
            label: 'Creation',
            checkKey: const ValueKey('auto-enter-creation'),
            typeKey: const ValueKey('auto-enter-creation-type'),
            enabled: _creationEnabled,
            type: _creationType,
            onEnabled: (v) => setState(() => _creationEnabled = v),
            onType: (v) => setState(() => _creationType = v),
          ),
          stamp(
            label: 'Modification',
            checkKey: const ValueKey('auto-enter-modification'),
            typeKey: const ValueKey('auto-enter-modification-type'),
            enabled: _modificationEnabled,
            type: _modificationType,
            onEnabled: (v) => setState(() => _modificationEnabled = v),
            onType: (v) => setState(() => _modificationType = v),
          ),

          // Serial number
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Checkbox(
                key: const ValueKey('auto-enter-serial'),
                value: _serialEnabled,
                onChanged: (v) => setState(() {
                  _serialEnabled = v ?? false;
                  if (!_serialEnabled) _serialError = null;
                }),
              ),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Padding(padding: EdgeInsets.only(top: 8), child: Text('Serial number')),
                    Padding(
                      padding: const EdgeInsets.only(left: 8, top: 4),
                      child: RadioGroup<String>(
                        groupValue: _serialGenerate,
                        onChanged: (v) {
                          if (_serialEnabled) setState(() => _serialGenerate = v ?? 'creation');
                        },
                        child: Row(
                          children: [
                            const Text('Generate:', style: hint),
                            const SizedBox(width: 8),
                            Radio<String>(
                              key: const ValueKey('auto-enter-serial-creation'),
                              value: 'creation',
                              enabled: _serialEnabled,
                            ),
                            const Text('On creation', style: small),
                            const SizedBox(width: 12),
                            Radio<String>(
                              key: const ValueKey('auto-enter-serial-commit'),
                              value: 'commit',
                              enabled: _serialEnabled,
                            ),
                            const Text('On commit', style: small),
                          ],
                        ),
                      ),
                    ),
                    Padding(
                      padding: const EdgeInsets.only(left: 8, top: 4),
                      child: Row(
                        children: [
                          const Text('next value', style: hint),
                          const SizedBox(width: 8),
                          SizedBox(
                            width: 110,
                            height: 30,
                            child: TextField(
                              key: const ValueKey('auto-enter-serial-next'),
                              controller: _serialNextController,
                              enabled: _serialEnabled,
                              style: small,
                              decoration: const InputDecoration(
                                contentPadding: EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                                border: OutlineInputBorder(),
                              ),
                              onChanged: (_) => setState(() {
                                _serialNextEdited = true;
                                _serialError = null;
                              }),
                            ),
                          ),
                          const SizedBox(width: 16),
                          const Text('increment by', style: hint),
                          const SizedBox(width: 8),
                          SizedBox(
                            width: 60,
                            height: 30,
                            child: TextField(
                              key: const ValueKey('auto-enter-serial-increment'),
                              controller: _serialIncrementController,
                              enabled: _serialEnabled,
                              style: small,
                              decoration: const InputDecoration(
                                contentPadding: EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                                border: OutlineInputBorder(),
                              ),
                              onChanged: (_) => setState(() => _serialError = null),
                            ),
                          ),
                        ],
                      ),
                    ),
                    if (_serialError != null)
                      Padding(
                        padding: const EdgeInsets.only(left: 8, top: 4),
                        child: Text(_serialError!,
                            key: const ValueKey('auto-enter-serial-error'),
                            style: TextStyle(fontSize: 11, color: theme.colorScheme.error)),
                      ),
                  ],
                ),
              ),
            ],
          ),

          const SizedBox(height: 6),

          // Value from last visited record: kept with the field, not entered.
          Row(
            children: [
              Checkbox(
                key: const ValueKey('auto-enter-last-visited'),
                value: _valueFromLastVisited,
                onChanged: (v) => setState(() => _valueFromLastVisited = v ?? false),
              ),
              const Text('Value from last visited record'),
              const SizedBox(width: 8),
              const Flexible(
                child: Text('(kept with the field; File4Base does not enter it)', style: hint),
              ),
            ],
          ),

          // Data
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Checkbox(
                key: const ValueKey('auto-enter-data'),
                value: _dataEnabled,
                onChanged: (v) => setState(() => _dataEnabled = v ?? false),
              ),
              const Padding(padding: EdgeInsets.only(top: 8), child: Text('Data:')),
              const SizedBox(width: 12),
              Expanded(
                child: SizedBox(
                  height: 54,
                  child: TextField(
                    key: const ValueKey('auto-enter-data-value'),
                    controller: _dataValueController,
                    enabled: _dataEnabled,
                    maxLines: 2,
                    style: small,
                    decoration: const InputDecoration(
                      hintText: 'A constant value',
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
                key: const ValueKey('auto-enter-calculated'),
                value: _calculatedValueEnabled,
                onChanged: (v) {
                  final on = v ?? false;
                  setState(() => _calculatedValueEnabled = on);
                  // As in FileMaker, turning it on asks for the formula.
                  if (on && _calculatedValueFormulaController.text.trim().isEmpty) {
                    unawaited(_specifyCalculatedValue());
                  }
                },
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
                          key: const ValueKey('auto-enter-calculated-specify'),
                          style: OutlinedButton.styleFrom(
                            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 2),
                            visualDensity: VisualDensity.compact,
                          ),
                          onPressed: _specifyCalculatedValue,
                          child: const Text('Specify...', style: TextStyle(fontSize: 11)),
                        ),
                        const SizedBox(width: 12),
                        const Text('Evaluate from:', style: hint),
                        const SizedBox(width: 6),
                        Expanded(
                          child: DropdownButton<String>(
                            key: const ValueKey('auto-enter-calculated-context'),
                            isExpanded: true,
                            value: _calculatedContext,
                            items: [
                              DropdownMenuItem(
                                value: '',
                                child: Text('$_defaultOccurrenceName (default)', style: small),
                              ),
                              for (final name in contextNames)
                                DropdownMenuItem(value: name, child: Text(name, style: small)),
                            ],
                            onChanged: _calculatedValueEnabled
                                ? (v) => setState(() => _calculatedContext = v ?? '')
                                : null,
                          ),
                        ),
                      ],
                    ),
                    if (_calculatedValueEnabled)
                      Padding(
                        padding: const EdgeInsets.only(top: 6),
                        child: TextField(
                          key: const ValueKey('auto-enter-calculated-formula'),
                          controller: _calculatedValueFormulaController,
                          maxLines: 3,
                          minLines: 1,
                          style: const TextStyle(fontSize: 12, fontFamily: 'monospace'),
                          decoration: const InputDecoration(
                            hintText: 'e.g. Upper ( first_name ) & " " & Upper ( last_name )',
                            contentPadding: EdgeInsets.symmetric(horizontal: 8, vertical: 6),
                            border: OutlineInputBorder(),
                          ),
                        ),
                      ),
                    Row(
                      children: [
                        Checkbox(
                          key: const ValueKey('auto-enter-do-not-replace'),
                          value: _doNotReplaceExisting,
                          onChanged: _calculatedValueEnabled
                              ? (v) => setState(() => _doNotReplaceExisting = v ?? false)
                              : null,
                        ),
                        const Flexible(child: Text('Do not replace existing value of field (if any)', style: small)),
                      ],
                    ),
                    Row(
                      children: [
                        Checkbox(
                          key: const ValueKey('auto-enter-skip-if-empty'),
                          value: _calculatedSkipIfEmpty,
                          onChanged: _calculatedValueEnabled
                              ? (v) => setState(() => _calculatedSkipIfEmpty = v ?? true)
                              : null,
                        ),
                        const Flexible(child: Text('Do not evaluate if all referenced fields are empty', style: small)),
                      ],
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
                key: const ValueKey('auto-enter-looked-up'),
                value: _lookedUpValueEnabled,
                onChanged: (v) {
                  final on = v ?? false;
                  setState(() => _lookedUpValueEnabled = on);
                  // As in FileMaker, turning it on asks what to look up.
                  if (on && !_lookup.isComplete) unawaited(_specifyLookup());
                },
              ),
              const Text('Looked-up value'),
              const SizedBox(width: 12),
              OutlinedButton(
                key: const ValueKey('auto-enter-looked-up-specify'),
                style: OutlinedButton.styleFrom(
                  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 2),
                  visualDensity: VisualDensity.compact,
                ),
                onPressed: _specifyLookup,
                child: const Text('Specify...', style: TextStyle(fontSize: 11)),
              ),
              const SizedBox(width: 12),
              if (_lookup.isComplete)
                Flexible(
                  child: Text(
                    'from ${_lookup.occurrence}::${_lookup.field}',
                    key: const ValueKey('auto-enter-looked-up-summary'),
                    style: small,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
            ],
          ),

          const Divider(height: 24),

          Row(
            children: [
              Checkbox(
                key: const ValueKey('auto-enter-prohibit'),
                value: _prohibitModification,
                onChanged: (v) => setState(() => _prohibitModification = v ?? false),
              ),
              const Flexible(
                child: Text(
                  'Prohibit modification of value during data entry',
                  style: TextStyle(fontWeight: FontWeight.w500),
                ),
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
                  initialValue: _strictDataType,
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
              color: isDark ? Colors.black.withValues(alpha: 0.2) : Colors.black.withValues(alpha: 0.02),
              borderRadius: BorderRadius.circular(8),
              border: Border.all(
                color: isDark ? Colors.white.withValues(alpha: 0.06) : Colors.black.withValues(alpha: 0.06),
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

          if (widget.column.fieldType != 'SUMMARY') ...[
            const SizedBox(height: 16),
            _buildRepetitionsSetting(isDark),
          ],

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
  /// The fields this summary can be taken over: not itself, never another
  /// summary, and only numbers when the kind needs them (#32).
  List<ColumnModel> get _summarizableColumns => widget.table.columns
      .where((c) => c.name != widget.column.name && c.fieldType != 'SUMMARY')
      .where((c) => !SummaryType.needsNumber(_summaryOperation) || c.storageType == 'NUMBER')
      .toList();

  /// FileMaker's "Maximum number of repetitions": a repeating field holds
  /// that many values, shown one under the other on a layout.
  Widget _buildRepetitionsSetting(bool isDark) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Padding(
          padding: EdgeInsets.only(top: 10),
          child: Text('Maximum number of repetitions:', style: TextStyle(fontWeight: FontWeight.w600)),
        ),
        const SizedBox(width: 12),
        SizedBox(
          width: 140,
          child: TextField(
            key: const ValueKey('max-repetitions'),
            controller: _repetitionsController,
            keyboardType: TextInputType.number,
            decoration: InputDecoration(
              isDense: true,
              border: const OutlineInputBorder(),
              errorText: _repetitionsError,
              errorMaxLines: 3,
              helperText: '1 to ${ColumnModel.repetitionsLimit}',
            ),
            onChanged: (v) => setState(
              () => _repetitionsError = repetitionsError(v, fieldType: widget.column.fieldType),
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildCalculationTab(ThemeData theme, bool isDark) {
    final resultTypes = ['Text', 'Number', 'Date', 'Time', 'Timestamp', 'Container'];
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
                  initialValue: _calculationResultType,
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
          Tooltip(
            message: 'Unchecked, the result is computed each time a record is shown. '
                'A calculation that reads related fields must be unstored; it cannot then be found or sorted by.',
            child: CheckboxListTile(
              dense: true,
              contentPadding: EdgeInsets.zero,
              controlAffinity: ListTileControlAffinity.leading,
              title: const Text('Store the result', style: TextStyle(fontSize: 12)),
              value: _storeCalculationResult,
              onChanged: (v) => setState(() => _storeCalculationResult = v ?? true),
            ),
          ),

          const Divider(height: 28),

          // Section 2: Summary Field
          Row(
            children: [
              const Icon(Icons.query_stats, size: 18, color: Colors.teal),
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
                  initialValue: _summaryOperation,
                  decoration: const InputDecoration(
                    contentPadding: EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                    border: OutlineInputBorder(),
                  ),
                  items: [
                    for (final entry in SummaryType.labels.entries)
                      DropdownMenuItem(
                        value: entry.key,
                        child: Text(entry.value, style: const TextStyle(fontSize: 12)),
                      ),
                  ],
                  onChanged: (v) {
                    if (v == null) return;
                    setState(() {
                      _summaryOperation = v;
                      // Totalling text is refused by the server, so a field
                      // the new kind cannot be taken over is let go of here
                      // rather than saved and bounced back.
                      if (SummaryType.needsNumber(v) &&
                          !_summarizableColumns.any((c) => c.name == _summaryTargetColumn)) {
                        _summaryTargetColumn = null;
                      }
                    });
                  },
                ),
              ),
              const SizedBox(width: 16),
              const Text('of field:', style: TextStyle(fontSize: 12, fontWeight: FontWeight.w500)),
              const SizedBox(width: 8),
              Expanded(
                child: SizedBox(
                  height: 32,
                  child: DropdownButtonFormField<String>(
                    initialValue: _summarizableColumns.any((c) => c.name == _summaryTargetColumn)
                        ? _summaryTargetColumn
                        : null,
                    decoration: const InputDecoration(
                      contentPadding: EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                      border: OutlineInputBorder(),
                    ),
                    items: _summarizableColumns
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
              const Expanded(
                child: Text(
                  'Running total — adds up down the records as they are drawn, '
                  'rather than giving one figure per group',
                  style: TextStyle(fontSize: 11),
                ),
              ),
            ],
          ),
          if (isSummary)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                'A summary has no value in a record: it is worked out over the found set '
                'when a report is drawn. Put it in a sub-summary part to total each group, '
                'or in a grand summary part to total the lot.',
                style: TextStyle(fontSize: 11, color: Colors.grey.shade600),
              ),
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
            color: isDark ? Colors.white.withValues(alpha: 0.08) : Colors.black.withValues(alpha: 0.08),
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
