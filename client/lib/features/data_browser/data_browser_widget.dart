import 'dart:async';
import 'dart:math' as math;
import 'package:flutter/foundation.dart' show defaultTargetPlatform;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:url_launcher/url_launcher.dart';
import '../../core/api/api_client.dart';
import '../../main.dart';
import '../layout_engine/layout_action_runner.dart';
import '../layout_engine/layout_object_visuals.dart';
import '../layout_engine/models/layout_definition.dart';

class DataBrowserWidget extends StatefulWidget {
  final TableModel table;
  final ApiClient apiClient;
  final OperationalMode mode;
  final LayoutDefinitionModel? layout;
  final ValueChanged<OperationalMode>? onModeChanged;
  final void Function(int currentIndex, int totalRecords)? onRecordChanged;
  final VoidCallback? onTableModified;

  /// Signed-in user, shown by `{{CurrentUser}}` in layout text.
  final String? currentUserName;

  /// Switches to the layout with this name (button "Go to Layout" steps).
  /// Returns false when there is no such layout.
  final bool Function(String layoutName)? onGoToLayout;

  const DataBrowserWidget({
    super.key,
    required this.table,
    required this.apiClient,
    required this.mode,
    this.layout,
    this.onModeChanged,
    this.onRecordChanged,
    this.onTableModified,
    this.currentUserName,
    this.onGoToLayout,
  });

  @override
  State<DataBrowserWidget> createState() => DataBrowserWidgetState();
}

/// One level of a sort order: a field and its direction.
class SortLevel {
  final String field;
  final bool ascending;

  const SortLevel(this.field, this.ascending);

  SortLevel get flipped => SortLevel(field, !ascending);

  /// The form the data API takes in its `sort` parameter: a leading "-" sorts
  /// the field descending.
  String get queryValue => ascending ? field : '-$field';

  @override
  bool operator ==(Object other) =>
      other is SortLevel && other.field == field && other.ascending == ascending;

  @override
  int get hashCode => Object.hash(field, ascending);

  @override
  String toString() => queryValue;
}

class DataBrowserWidgetState extends State<DataBrowserWidget> implements LayoutActionHost {
  List<Map<String, dynamic>> _records = [];
  bool _isLoading = true;
  String? _error;
  int _currentIndex = 0;
  bool _isFoundSet = false;
  String? _lastFocusedFindField;
  String _viewMode = 'form'; // 'form' (layout), 'list' (record list) or 'table' (spreadsheet grid)

  // Found set bookkeeping for the record bar
  int _totalInTable = 0;
  List<Map<String, dynamic>> _unsortedRecords = [];
  /// The sort order, outermost field first. Empty means the records are in
  /// the order they are stored in.
  List<SortLevel> _sortOrder = const [];

  String? get _sortField => _sortOrder.isEmpty ? null : _sortOrder.first.field;
  bool get _sortAscending => _sortOrder.isEmpty ? true : _sortOrder.first.ascending;
  final TextEditingController _recordNumberCtrl = TextEditingController();
  final FocusNode _recordNumberFocus = FocusNode();
  double? _sliderDragValue; // record shown by the slider while it is dragged

  int get currentIndex => _currentIndex;
  int get totalRecords => _records.length;
  bool get isOmit => _omit;
  bool get isFoundSet => _isFoundSet;

  void createNewRecord() => _createNewRecord();
  void duplicateRecord() => actionDuplicateRecord();
  void sortRecords() => _showSortDialog();
  void deleteCurrentRecord() => _deleteCurrentRecord();
  void performFind() => _performFind();
  void fetchRecords() => _fetchRecords();

  void previousRecord() {
    if (_records.isNotEmpty && _currentIndex > 0) {
      _saveCurrentRecord().then((saved) {
        if (!saved || !mounted) return;
        setState(() => _currentIndex--);
        _rebuildFieldControllers();
        widget.onRecordChanged?.call(_currentIndex, _records.length);
      });
    }
  }

  void nextRecord() {
    if (_records.isNotEmpty && _currentIndex < _records.length - 1) {
      _saveCurrentRecord().then((saved) {
        if (!saved || !mounted) return;
        setState(() => _currentIndex++);
        _rebuildFieldControllers();
        widget.onRecordChanged?.call(_currentIndex, _records.length);
      });
    }
  }

  void goToRecord(int index) {
    if (_records.isNotEmpty && index >= 0 && index < _records.length) {
      _saveCurrentRecord().then((saved) {
        if (!saved || !mounted) return;
        setState(() => _currentIndex = index);
        _rebuildFieldControllers();
        widget.onRecordChanged?.call(_currentIndex, _records.length);
      });
    }
  }

  void toggleOmit(bool val) => setState(() => _omit = val);

  /// Writes the current record to the server: pending field edits, and a new
  /// record that has not been created yet.
  Future<void> commitRecord() => actionCommitRecord();

  /// Discards uncommitted changes; a new record that was never committed is
  /// thrown away.
  Future<void> revertRecord() => actionRevertRecord();

  /// `first`, `previous`, `next`, `last`, or a 1-based record number.
  Future<void> goToRecordNamed(String target) => actionGoToRecord(target);

  /// Restores the order the records are stored in.
  void unsort() => _applySort(null, true);

  /// True while the current record is a new one that has not been committed.
  bool get hasUncommittedRecord => _onDraft;

  // ─── Find mode ────────────────────────────────────────────────────────────
  final Map<String, TextEditingController> _findControllers = {};
  bool _omit = false;

  // ─── Per-field edit state ─────────────────────────────────────────────────
  final Map<String, TextEditingController> _fieldControllers = {};
  final Map<String, FocusNode> _fieldFocusNodes = {};
  final Map<String, Timer?> _fieldDebounceTimers = {};
  // null = idle/saved, true = saving, false = error
  final Map<String, bool?> _fieldSaving = {};

  /// What a stored value looks like in a field box. A DATE column comes back
  /// from the API as a full RFC3339 timestamp ("2011-01-15T00:00:00Z"); the
  /// user entered a date and must see and edit a date.
  static String fieldText(ColumnModel col, Object? raw) {
    final text = raw?.toString() ?? '';
    if (col.fieldType == 'DATE' && text.length > 10 && text[10] == 'T') {
      return text.substring(0, 10);
    }
    return text;
  }

  String _columnText(String colName, Object? raw) {
    final col = widget.table.columns.where((c) => c.name == colName).firstOrNull;
    return col == null ? (raw?.toString() ?? '') : fieldText(col, raw);
  }

  void _rebuildFieldControllers() {
    for (final c in _fieldControllers.values) c.dispose();
    for (final f in _fieldFocusNodes.values) f.dispose();
    for (final t in _fieldDebounceTimers.values) t?.cancel();
    _fieldControllers.clear();
    _fieldFocusNodes.clear();
    _fieldDebounceTimers.clear();
    _fieldSaving.clear();

    if (_records.isEmpty) return;
    final record = _records[_currentIndex];

    for (final col in widget.table.columns) {
      if (col.isPrimaryKey) continue;
      final val = fieldText(col, record[col.name]);
      final ctrl = TextEditingController(text: val);
      final fn = FocusNode();

      _fieldControllers[col.name] = ctrl;
      _fieldFocusNodes[col.name] = fn;
      _fieldDebounceTimers[col.name] = null;
      _fieldSaving[col.name] = null;

      fn.addListener(() {
        if (!fn.hasFocus) {
          _fieldDebounceTimers[col.name]?.cancel();
          _saveField(col.name, ctrl.text);
        }
      });
    }
  }

  void _onFieldChanged(String colName, String newVal) {
    _fieldDebounceTimers[colName]?.cancel();
    _fieldDebounceTimers[colName] = Timer(
      const Duration(milliseconds: 800),
      () => _saveField(colName, newVal),
    );
  }

  Future<void> _saveField(String colName, String newVal) async {
    if (_records.isEmpty) return;
    final record = _records[_currentIndex];
    if (record[_draftKey] == true) {
      // A new record is sent as a whole when it is committed
      record[colName] = newVal;
      return;
    }
    final id = record['id']?.toString();
    if (id == null) return;

    _records[_currentIndex][colName] = newVal;
    if (mounted) setState(() => _fieldSaving[colName] = true);
    try {
      await widget.apiClient.updateRow(widget.table.name, id, {colName: newVal});
      if (mounted) setState(() => _fieldSaving[colName] = null);
    } catch (e) {
      if (mounted) {
        setState(() => _fieldSaving[colName] = false);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Error saving "${_displayName(colName)}": $e'),
            backgroundColor: Colors.red,
            duration: const Duration(seconds: 3),
          ),
        );
      }
    }
  }

  String _displayName(String colName) {
    try {
      return widget.table.columns.firstWhere((c) => c.name == colName).displayName;
    } catch (_) {
      return colName;
    }
  }

  /// Marks a new record that exists only here until it is committed.
  static const _draftKey = '__f4b_draft';

  bool get _onDraft => _records.isNotEmpty && _records[_currentIndex][_draftKey] == true;

  /// Commits the current record: pending field edits, or the whole new
  /// record. Returns false when the server rejected it (for example a field
  /// validation rule); the user stays on the record and sees why.
  Future<bool> _saveCurrentRecord() async {
    for (final col in widget.table.columns) {
      if (col.isPrimaryKey) continue;
      final ctrl = _fieldControllers[col.name];
      if (ctrl == null) continue;
      _fieldDebounceTimers[col.name]?.cancel();
      final cached = _records.isNotEmpty
          ? fieldText(col, _records[_currentIndex][col.name])
          : '';
      if (ctrl.text != cached) {
        await _saveField(col.name, ctrl.text);
      }
    }
    if (_onDraft) return _commitDraft();
    return true;
  }

  /// Creates the new record on the server, like FileMaker commits a new
  /// record: validation rules apply to the record as a whole, and fields the
  /// user left empty are not sent, so their auto-enter values apply.
  Future<bool> _commitDraft() async {
    final draft = _records[_currentIndex];
    final values = <String, dynamic>{
      for (final col in widget.table.columns)
        if (!col.isPrimaryKey && (draft[col.name]?.toString() ?? '').isNotEmpty) col.name: draft[col.name],
    };
    try {
      final created = await widget.apiClient.insertRow(widget.table.name, values);
      if (mounted) setState(() => _records[_currentIndex] = created);
      return true;
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('The new record was not saved: $e'), backgroundColor: Colors.red),
        );
      }
      return false;
    }
  }

  void _discardDraft() {
    _records.removeAt(_currentIndex);
    if (_currentIndex >= _records.length) _currentIndex = _records.length - 1;
    if (_currentIndex < 0) _currentIndex = 0;
    _rebuildFieldControllers();
    widget.onRecordChanged?.call(_currentIndex, _records.length);
  }

  @override
  void initState() {
    super.initState();
    _initFindControllers();
    _fetchRecords().then((_) {
      if (mounted && widget.mode == OperationalMode.browse) _runLayoutTrigger(widget.layout?.onLayoutEnter);
    });
    HardwareKeyboard.instance.addHandler(_handleRecordShortcut);
  }

  bool _runningLayoutTrigger = false;

  /// Runs an OnLayoutEnter / OnLayoutExit script. A trigger started while
  /// another one runs (e.g. the script itself goes to another layout) is
  /// skipped, so layouts that switch to each other cannot loop forever.
  Future<void> _runLayoutTrigger(ButtonActionModel? action) async {
    if (action == null || _runningLayoutTrigger) return;
    _runningLayoutTrigger = true;
    try {
      final result = await LayoutActionRunner(apiClient: widget.apiClient, host: this).run(action);
      if (!result.completed && mounted && result.message != null) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(result.message!), backgroundColor: Colors.orange.shade800),
        );
      }
    } finally {
      _runningLayoutTrigger = false;
    }
  }

  /// Cmd/Ctrl + Up / Down moves to the previous / next record in Browse
  /// mode, also while a field is being edited.
  bool _handleRecordShortcut(KeyEvent event) {
    if (event is! KeyDownEvent || widget.mode != OperationalMode.browse || !mounted) return false;
    final kb = HardwareKeyboard.instance;
    if (!(kb.isMetaPressed || kb.isControlPressed) || kb.isShiftPressed || kb.isAltPressed) return false;
    if (ModalRoute.of(context)?.isCurrent == false) return false; // a dialog is open
    if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
      previousRecord();
      return true;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
      nextRecord();
      return true;
    }
    return false;
  }

  @override
  void didUpdateWidget(covariant DataBrowserWidget oldWidget) {
    super.didUpdateWidget(oldWidget);
    final columnsChanged = oldWidget.table.columns.length != widget.table.columns.length ||
        !_sameColumns(oldWidget.table.columns, widget.table.columns);
    final layoutChanged = oldWidget.layout != widget.layout ||
        oldWidget.layout?.id != widget.layout?.id ||
        oldWidget.layout?.objects.length != widget.layout?.objects.length ||
        oldWidget.mode != widget.mode;
    if (oldWidget.layout?.id != widget.layout?.id && widget.mode == OperationalMode.browse) {
      final exit = oldWidget.layout?.onLayoutExit;
      final enter = widget.layout?.onLayoutEnter;
      if (exit != null || enter != null) {
        WidgetsBinding.instance.addPostFrameCallback((_) async {
          await _runLayoutTrigger(exit);
          if (mounted) await _runLayoutTrigger(enter);
        });
      }
    }
    if (oldWidget.table.id != widget.table.id || columnsChanged || layoutChanged) {
      _initFindControllers();
      _rebuildFieldControllers();
      if (oldWidget.table.id != widget.table.id) {
        _fetchRecords();
      }
    }
  }

  bool _sameColumns(List<ColumnModel> a, List<ColumnModel> b) {
    if (a.length != b.length) return false;
    for (int i = 0; i < a.length; i++) {
      if (a[i].id != b[i].id || a[i].displayName != b[i].displayName || a[i].name != b[i].name) {
        return false;
      }
    }
    return true;
  }

  void _initFindControllers() {
    _findControllers.clear();
    for (var col in widget.table.columns) {
      _findControllers[col.name] = TextEditingController();
    }
  }

  @override
  void dispose() {
    HardwareKeyboard.instance.removeHandler(_handleRecordShortcut);
    _recordNumberCtrl.dispose();
    _recordNumberFocus.dispose();
    for (var c in _findControllers.values) {
      c.dispose();
    }
    for (var c in _fieldControllers.values) {
      c.dispose();
    }
    for (var f in _fieldFocusNodes.values) {
      f.dispose();
    }
    for (var t in _fieldDebounceTimers.values) {
      t?.cancel();
    }
    super.dispose();
  }

  Future<void> _fetchRecords() async {
    setState(() { _isLoading = true; _error = null; _isFoundSet = false; });
    try {
      final rows = await widget.apiClient.listRows(widget.table.name);
      if (mounted) {
        setState(() {
          _records = rows;
          _unsortedRecords = List.of(rows);
          _totalInTable = rows.length;
          _sortOrder = const [];
          _currentIndex = 0;
          _isLoading = false;
        });
        _rebuildFieldControllers();
        widget.onRecordChanged?.call(_currentIndex, _records.length);
      }
    } catch (e) {
      if (mounted) setState(() { _error = e.toString(); _isLoading = false; });
    }
  }

  /// New Record: an empty record shown at the end of the found set. It is
  /// created on the server when committed (see [_commitDraft]).
  Future<void> _createNewRecord() async {
    if (!await _saveCurrentRecord() || !mounted) return;
    setState(() {
      _records.add(<String, dynamic>{_draftKey: true});
      _currentIndex = _records.length - 1;
    });
    _rebuildFieldControllers();
    widget.onRecordChanged?.call(_currentIndex, _records.length);
  }

  Future<void> _deleteCurrentRecord() async {
    if (_records.isEmpty) return;
    for (final t in _fieldDebounceTimers.values) {
      t?.cancel();
    }
    if (_onDraft) {
      setState(_discardDraft);
      return;
    }
    final id = _records[_currentIndex]['id']?.toString();
    if (id == null) return;
    try {
      await widget.apiClient.deleteRow(widget.table.name, id);
      await _fetchRecords();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Error deleting record: $e'), backgroundColor: Colors.red),
        );
      }
    }
  }

  Future<void> _performFind() async {
    final criteria = <Map<String, dynamic>>[];
    _findControllers.forEach((fieldName, controller) {
      String text = controller.text.trim();
      if (text.isEmpty) return;

      // Standard today's date formula: // -> current ISO date
      final todayStr = DateTime.now().toIso8601String().split('T').first;
      if (text == '//') {
        text = todayStr;
      } else if (text.contains('//')) {
        text = text.replaceAll('//', todayStr);
      }

      if (text == '=') {
        // Find empty field
        criteria.add({'field_name': fieldName, 'operator': 'IS_EMPTY', 'value': ''});
      } else if (text == '*') {
        // Find non-empty field
        criteria.add({'field_name': fieldName, 'operator': 'IS_NOT_EMPTY', 'value': ''});
      } else if (text.contains('...')) {
        final parts = text.split('...');
        criteria.add({'field_name': fieldName, 'operator': 'RANGE', 'value': parts[0].trim(), 'value_to': parts[1].trim()});
      } else if (text.startsWith('>=')) {
        criteria.add({'field_name': fieldName, 'operator': '>=', 'value': text.substring(2).trim()});
      } else if (text.startsWith('<=')) {
        criteria.add({'field_name': fieldName, 'operator': '<=', 'value': text.substring(2).trim()});
      } else if (text.startsWith('>')) {
        criteria.add({'field_name': fieldName, 'operator': '>', 'value': text.substring(1).trim()});
      } else if (text.startsWith('<')) {
        criteria.add({'field_name': fieldName, 'operator': '<', 'value': text.substring(1).trim()});
      } else if (text.startsWith('!=')) {
        criteria.add({'field_name': fieldName, 'operator': '!=', 'value': text.substring(2).trim()});
      } else if (text.startsWith('!')) {
        criteria.add({'field_name': fieldName, 'operator': '!=', 'value': text.substring(1).trim()});
      } else if (text.startsWith('==')) {
        criteria.add({'field_name': fieldName, 'operator': '==', 'value': text.substring(2).trim()});
      } else if (text.startsWith('=')) {
        criteria.add({'field_name': fieldName, 'operator': '=', 'value': text.substring(1).trim()});
      } else if (text.contains('*') || text.contains('@') || text.contains('?')) {
        String wildcard = text.replaceAll('*', '%').replaceAll('@', '_').replaceAll('?', '_');
        criteria.add({'field_name': fieldName, 'operator': 'LIKE', 'value': wildcard});
      } else {
        criteria.add({'field_name': fieldName, 'operator': 'LIKE', 'value': '%$text%'});
      }
    });

    setState(() { _isLoading = true; _error = null; });
    try {
      final results = await widget.apiClient.executeFind(widget.table.name, [
        {'criteria': criteria, 'omit': _omit}
      ]);
      if (!mounted) return;

      if (results.isEmpty) {
        setState(() { _isLoading = false; });
        await showDialog(
          context: context,
          builder: (ctx) => AlertDialog(
            title: const Row(
              children: [
                Icon(Icons.search_off_rounded, color: Color(0xFFE53935)),
                SizedBox(width: 10),
                Text('No Records Found', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
              ],
            ),
            content: const Text(
              'No records match the specified find criteria.\n\nYou can modify your search terms or cancel to return to all records.',
              style: TextStyle(fontSize: 13),
            ),
            actions: [
              OutlinedButton(
                onPressed: () {
                  Navigator.of(ctx).pop();
                  _fetchRecords();
                  widget.onModeChanged?.call(OperationalMode.browse);
                },
                child: const Text('Show All Records (Cancel)'),
              ),
              FilledButton(
                onPressed: () => Navigator.of(ctx).pop(),
                child: const Text('Modify Criteria'),
              ),
            ],
          ),
        );
      } else {
        setState(() {
          _records = results;
          _unsortedRecords = List.of(results);
          _sortOrder = const [];
          _currentIndex = 0;
          _isFoundSet = true;
          _isLoading = false;
        });
        _rebuildFieldControllers();
        widget.onRecordChanged?.call(_currentIndex, _records.length);
        widget.onModeChanged?.call(OperationalMode.browse);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Row(
              children: [
                const Icon(Icons.check_circle_outline, color: Colors.white, size: 18),
                const SizedBox(width: 8),
                Text('Found ${results.length} record(s) matching criteria'),
              ],
            ),
            backgroundColor: const Color(0xFF2E7D32),
            duration: const Duration(seconds: 4),
            action: SnackBarAction(
              label: 'Show All',
              textColor: Colors.white,
              onPressed: _fetchRecords,
            ),
          ),
        );
      }
    } catch (e) {
      if (mounted) setState(() { _error = e.toString(); _isLoading = false; });
    }
  }

  @override
  Widget build(BuildContext context) {
    final hasCustomLayoutForm = widget.layout != null &&
        widget.layout!.objects.isNotEmpty &&
        _viewMode == 'form';

    Widget content;
    if (_isLoading) {
      content = const Center(child: CircularProgressIndicator());
    } else if (widget.mode == OperationalMode.find) {
      content = _buildFindModeForm();
    } else if (hasCustomLayoutForm) {
      content = _buildLayoutCanvasView(widget.layout!, isFindMode: false);
    } else if (_viewMode == 'table' && _records.isNotEmpty) {
      content = _buildTableView();
    } else if (_viewMode == 'list' && _records.isNotEmpty) {
      content = _buildListView();
    } else if (_error != null) {
      content = Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.error_outline, size: 48, color: Colors.red),
            const SizedBox(height: 12),
            Text('Error: $_error', style: const TextStyle(color: Colors.red)),
            const SizedBox(height: 12),
            FilledButton.icon(
              icon: const Icon(Icons.refresh),
              label: const Text('Retry'),
              onPressed: _fetchRecords,
            ),
          ],
        ),
      );
    } else if (_records.isEmpty) {
      content = _buildEmptyState();
    } else {
      content = _buildBrowseModeContent();
    }

    return Column(
      children: [
        _buildRecordToolbar(),
        if (_error != null && hasCustomLayoutForm)
          MaterialBanner(
            content: Text('Could not load records: $_error', style: const TextStyle(fontSize: 12)),
            leading: const Icon(Icons.warning_amber, color: Colors.orange, size: 20),
            backgroundColor: Colors.amber.shade50,
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
            actions: [
              TextButton(
                onPressed: _fetchRecords,
                child: const Text('Retry', style: TextStyle(fontSize: 12)),
              ),
            ],
          ),
        const Divider(height: 1),
        Expanded(child: content),
      ],
    );
  }

  Widget _buildRecordToolbar() {
    if (widget.mode == OperationalMode.browse) return _buildBrowseRecordBar();
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16.0, vertical: 6.0),
      color: Theme.of(context).colorScheme.surfaceContainerHighest.withValues(alpha: 0.5),
      child: Row(
        children: [
          if (widget.mode == OperationalMode.find) ...[
            FilledButton.icon(
              icon: const Icon(Icons.search, size: 18),
              label: const Text('Perform Find (Enter)'),
              onPressed: _performFind,
            ),
            const SizedBox(width: 12),
            FilterChip(
              label: const Text('Omit (NOT)'),
              selected: _omit,
              onSelected: (val) => setState(() => _omit = val),
            ),
            const SizedBox(width: 8),
            _buildOperatorsMenu(),
            const SizedBox(width: 8),
            OutlinedButton.icon(
              icon: const Icon(Icons.clear_all, size: 16),
              label: const Text('Clear Criteria'),
              onPressed: () {
                for (var c in _findControllers.values) c.clear();
              },
            ),
            const SizedBox(width: 8),
            OutlinedButton.icon(
              icon: const Icon(Icons.close, size: 16),
              label: const Text('Cancel Find'),
              onPressed: () {
                widget.onModeChanged?.call(OperationalMode.browse);
                _fetchRecords();
              },
            ),
          ],
        ],
      ),
    );
  }

  // ─── Browse mode record bar ────────────────────────────────────────────────

  static final bool _isApplePlatform =
      defaultTargetPlatform == TargetPlatform.macOS || defaultTargetPlatform == TargetPlatform.iOS;
  static String get _modKey => _isApplePlatform ? '⌘' : 'Ctrl+';

  /// Saving state of the current record, from the per-field save status.
  ({IconData icon, Color color, String label}) get _saveStatus {
    if (_fieldSaving.values.any((v) => v == false)) {
      return (icon: Icons.error_outline, color: Colors.red, label: 'Not saved');
    }
    if (_fieldSaving.values.any((v) => v == true)) {
      return (icon: Icons.sync, color: Colors.blue, label: 'Saving...');
    }
    // A new record exists only here until it is committed; saying "Saved"
    // would invite the user to close the window and lose it.
    if (_onDraft) {
      return (icon: Icons.fiber_new_outlined, color: Colors.orange.shade800, label: 'Not committed');
    }
    return (icon: Icons.check_circle_outline, color: Colors.green.shade700, label: 'Saved');
  }

  String get _foundSetSummary {
    final n = _records.length;
    final sort = _sortOrder.isEmpty
        ? 'Unsorted'
        : 'Sorted by ${_sortOrder.map((l) => '${_displayName(l.field)} ${l.ascending ? '↑' : '↓'}').join(', ')}';
    if (_isFoundSet) return '$n found of $_totalInTable · $sort';
    return '$n ${n == 1 ? 'record' : 'records'} · $sort';
  }

  void _goToRecordIndex(int index) {
    if (_records.isEmpty) return;
    goToRecord(index.clamp(0, _records.length - 1));
  }

  Widget _buildBrowseRecordBar() {
    final theme = Theme.of(context);
    final hasRecords = _records.isNotEmpty;
    final atFirst = !hasRecords || _currentIndex == 0;
    final atLast = !hasRecords || _currentIndex >= _records.length - 1;
    final current = hasRecords ? _currentIndex + 1 : 0;
    if (!_recordNumberFocus.hasFocus && _recordNumberCtrl.text != '$current') {
      _recordNumberCtrl.text = '$current';
    }
    final status = _saveStatus;

    Widget navButton(IconData icon, String tip, VoidCallback? onPressed) => IconButton(
          icon: Icon(icon, size: 20),
          tooltip: tip,
          visualDensity: VisualDensity.compact,
          onPressed: onPressed,
        );

    return Container(
      padding: const EdgeInsets.fromLTRB(12, 6, 12, 6),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        border: Border(bottom: BorderSide(color: theme.dividerColor.withValues(alpha: 0.4))),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          // Row 1: position in the found set
          Row(
            children: [
              navButton(Icons.first_page, 'First record', atFirst ? null : () => _goToRecordIndex(0)),
              navButton(Icons.chevron_left, 'Previous record (${_modKey}↑)', atFirst ? null : previousRecord),
              const Text('Record ', style: TextStyle(fontSize: 13)),
              _RecordNumberField(
                controller: _recordNumberCtrl,
                focusNode: _recordNumberFocus,
                enabled: hasRecords,
                onSubmitted: (v) {
                  final n = int.tryParse(v.trim());
                  if (n != null) _goToRecordIndex(n - 1);
                },
              ),
              Text(' of ${_records.length}', style: const TextStyle(fontSize: 13)),
              navButton(Icons.chevron_right, 'Next record (${_modKey}↓)', atLast ? null : nextRecord),
              navButton(Icons.last_page, 'Last record', atLast ? null : () => _goToRecordIndex(_records.length - 1)),
              if (_records.length > 1)
                SizedBox(
                  width: 140,
                  // Dragging only previews the number; the record changes on release,
                  // after the current record is saved.
                  child: Slider(
                    value: (_sliderDragValue ?? _currentIndex.toDouble()).clamp(0, (_records.length - 1).toDouble()),
                    min: 0,
                    max: (_records.length - 1).toDouble(),
                    divisions: _records.length - 1,
                    label: '${(_sliderDragValue ?? _currentIndex.toDouble()).round() + 1}',
                    onChanged: (v) => setState(() => _sliderDragValue = v),
                    onChangeEnd: (v) {
                      setState(() => _sliderDragValue = null);
                      _goToRecordIndex(v.round());
                    },
                  ),
                ),
              const SizedBox(width: 8),
              Flexible(
                child: Text(
                  _foundSetSummary,
                  style: TextStyle(fontSize: 12, color: theme.colorScheme.onSurfaceVariant),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              if (_isFoundSet)
                Tooltip(
                  message: 'Show all records',
                  child: IconButton(
                    icon: const Icon(Icons.filter_alt_off_outlined, size: 18),
                    visualDensity: VisualDensity.compact,
                    onPressed: _fetchRecords,
                  ),
                ),
              const Spacer(),
              if (hasRecords)
                Tooltip(
                  message: 'Changes are saved automatically',
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(status.icon, size: 15, color: status.color),
                      const SizedBox(width: 4),
                      Text(status.label, style: TextStyle(fontSize: 12, color: status.color)),
                    ],
                  ),
                ),
            ],
          ),
          const SizedBox(height: 4),
          // Row 2: record actions and view
          Row(
            children: [
              FilledButton.icon(
                icon: const Icon(Icons.add, size: 16),
                label: const Text('New'),
                style: FilledButton.styleFrom(visualDensity: VisualDensity.compact),
                onPressed: _createNewRecord,
              ),
              const SizedBox(width: 6),
              _barButton(Icons.copy, 'Duplicate', hasRecords ? actionDuplicateRecord : null),
              _barButton(Icons.delete_outline, 'Delete',
                  hasRecords ? () => actionDeleteRecord(confirm: true) : null,
                  color: Colors.red.shade700),
              const SizedBox(width: 10),
              _barButton(Icons.search, 'Find', () => widget.onModeChanged?.call(OperationalMode.find)),
              _barButton(Icons.sort, 'Sort', hasRecords ? _showSortDialog : null),
              _barButton(Icons.list_alt, 'Show All', _fetchRecords),
              const Spacer(),
              SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: 'form', icon: Icon(Icons.dashboard_outlined, size: 14), label: Text('Form', style: TextStyle(fontSize: 11))),
                  ButtonSegment(value: 'list', icon: Icon(Icons.view_list_outlined, size: 14), label: Text('List', style: TextStyle(fontSize: 11))),
                  ButtonSegment(value: 'table', icon: Icon(Icons.table_chart_outlined, size: 14), label: Text('Table', style: TextStyle(fontSize: 11))),
                ],
                selected: {_viewMode},
                onSelectionChanged: (val) => setState(() => _viewMode = val.first),
                style: SegmentedButton.styleFrom(
                  visualDensity: VisualDensity.compact,
                  padding: const EdgeInsets.symmetric(horizontal: 6),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _barButton(IconData icon, String label, VoidCallback? onPressed, {Color? color}) {
    return Padding(
      padding: const EdgeInsets.only(right: 6),
      child: OutlinedButton.icon(
        icon: Icon(icon, size: 16, color: onPressed == null ? null : color),
        label: Text(label, style: TextStyle(color: onPressed == null ? null : color)),
        style: OutlinedButton.styleFrom(visualDensity: VisualDensity.compact),
        onPressed: onPressed,
      ),
    );
  }

  // ─── Sort ────────────────────────────────────────────────────────────────

  int _compareValues(dynamic a, dynamic b) {
    if (a == null || a.toString().isEmpty) return (b == null || b.toString().isEmpty) ? 0 : 1;
    if (b == null || b.toString().isEmpty) return -1;
    final na = num.tryParse(a.toString());
    final nb = num.tryParse(b.toString());
    if (na != null && nb != null) return na.compareTo(nb);
    return a.toString().toLowerCase().compareTo(b.toString().toLowerCase());
  }

  /// Sorts the found set by [field] (null restores the original order),
  /// keeping the current record selected.
  void _applySort(String? field, bool ascending) =>
      _applySortOrder(field == null ? const [] : [SortLevel(field, ascending)]);

  /// Orders the found set by every level in turn: ties on the first field are
  /// broken by the second, and so on.
  void _applySortOrder(List<SortLevel> order) {
    final currentId = _records.isNotEmpty ? _records[_currentIndex]['id'] : null;
    final sorted = List.of(_unsortedRecords);
    if (order.isNotEmpty) {
      sorted.sort((a, b) {
        for (final level in order) {
          final cmp = level.ascending
              ? _compareValues(a[level.field], b[level.field])
              : _compareValues(b[level.field], a[level.field]);
          if (cmp != 0) return cmp;
        }
        return 0;
      });
    }
    setState(() {
      _records = sorted;
      _sortOrder = List.unmodifiable(order);
      final idx = _records.indexWhere((r) => r['id'] == currentId);
      _currentIndex = idx < 0 ? 0 : idx;
    });
    _rebuildFieldControllers();
    widget.onRecordChanged?.call(_currentIndex, _records.length);
  }

  Future<void> _showSortDialog() async {
    await _saveCurrentRecord();
    if (!mounted) return;
    final cols = widget.table.columns.where((c) => !c.isPrimaryKey).toList();
    if (cols.isEmpty) return;

    var order = List.of(_sortOrder);
    final result = await showDialog<String>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDlg) {
          final available = cols.where((c) => !order.any((l) => l.field == c.name)).toList();
          return AlertDialog(
            title: const Text('Sort Records'),
            content: SizedBox(
              width: 420,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const Text(
                    'Records are ordered by the first field; ties are broken by the next one.',
                    style: TextStyle(fontSize: 12, color: Colors.grey),
                  ),
                  const SizedBox(height: 12),
                  if (order.isEmpty)
                    const Padding(
                      padding: EdgeInsets.symmetric(vertical: 16),
                      child: Text('No sort order yet. Add a field below.',
                          style: TextStyle(fontSize: 12, color: Colors.grey)),
                    )
                  else
                    ConstrainedBox(
                      constraints: const BoxConstraints(maxHeight: 220),
                      child: ListView.builder(
                        shrinkWrap: true,
                        itemCount: order.length,
                        itemBuilder: (_, i) {
                          final level = order[i];
                          return ListTile(
                            key: ValueKey('sort-level-${level.field}'),
                            dense: true,
                            contentPadding: EdgeInsets.zero,
                            leading: Text('${i + 1}.'),
                            title: Text(_displayName(level.field)),
                            trailing: Row(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                IconButton(
                                  tooltip: level.ascending ? 'Ascending' : 'Descending',
                                  icon: Icon(level.ascending ? Icons.arrow_upward : Icons.arrow_downward, size: 16),
                                  onPressed: () => setDlg(() => order[i] = level.flipped),
                                ),
                                IconButton(
                                  tooltip: 'Move up',
                                  icon: const Icon(Icons.keyboard_arrow_up, size: 18),
                                  onPressed: i == 0
                                      ? null
                                      : () => setDlg(() => order.insert(i - 1, order.removeAt(i))),
                                ),
                                IconButton(
                                  tooltip: 'Move down',
                                  icon: const Icon(Icons.keyboard_arrow_down, size: 18),
                                  onPressed: i == order.length - 1
                                      ? null
                                      : () => setDlg(() => order.insert(i + 1, order.removeAt(i))),
                                ),
                                IconButton(
                                  tooltip: 'Remove from the sort order',
                                  icon: const Icon(Icons.close, size: 16),
                                  onPressed: () => setDlg(() => order.removeAt(i)),
                                ),
                              ],
                            ),
                          );
                        },
                      ),
                    ),
                  const Divider(),
                  Row(
                    children: [
                      Expanded(
                        child: DropdownButtonFormField<String>(
                          key: const ValueKey('sort-add-field'),
                          initialValue: null,
                          decoration: const InputDecoration(
                              labelText: 'Add a field', border: OutlineInputBorder(), isDense: true),
                          items: available
                              .map((c) => DropdownMenuItem(value: c.name, child: Text(c.displayName)))
                              .toList(),
                          onChanged: available.isEmpty
                              ? null
                              : (v) {
                                  if (v != null) setDlg(() => order.add(SortLevel(v, true)));
                                },
                        ),
                      ),
                      const SizedBox(width: 8),
                      TextButton(
                        onPressed: order.isEmpty ? null : () => setDlg(order.clear),
                        child: const Text('Clear All'),
                      ),
                    ],
                  ),
                ],
              ),
            ),
            actions: [
              TextButton(onPressed: () => Navigator.of(ctx).pop('unsort'), child: const Text('Unsort')),
              TextButton(onPressed: () => Navigator.of(ctx).pop(), child: const Text('Cancel')),
              FilledButton(
                onPressed: order.isEmpty ? null : () => Navigator.of(ctx).pop('sort'),
                child: const Text('Sort'),
              ),
            ],
          );
        },
      ),
    );
    if (result == 'sort') _applySortOrder(order);
    if (result == 'unsort') _applySortOrder(const []);
  }

  // ─── List and Table views ──────────────────────────────────────────────────

  List<ColumnModel> get _visibleColumns => widget.table.columns.where((c) => !c.isPrimaryKey).toList();

  void _openRecordInForm(int index) {
    _goToRecordIndex(index);
    setState(() => _viewMode = 'form');
  }

  Widget _buildListView() {
    final cols = _visibleColumns;
    final theme = Theme.of(context);
    return ListView.separated(
      padding: const EdgeInsets.symmetric(vertical: 8),
      itemCount: _records.length,
      separatorBuilder: (_, _) => const Divider(height: 1),
      itemBuilder: (ctx, i) {
        final r = _records[i];
        final values = cols.map((c) => fieldText(c, r[c.name])).where((v) => v.isNotEmpty).toList();
        final title = values.isNotEmpty ? values.first : '(empty record)';
        final subtitle = values.skip(1).take(4).join(' · ');
        return ListTile(
          selected: i == _currentIndex,
          selectedTileColor: theme.colorScheme.primary.withValues(alpha: 0.08),
          leading: CircleAvatar(radius: 14, child: Text('${i + 1}', style: const TextStyle(fontSize: 11))),
          title: Text(title, overflow: TextOverflow.ellipsis),
          subtitle: subtitle.isEmpty ? null : Text(subtitle, overflow: TextOverflow.ellipsis),
          trailing: const Icon(Icons.chevron_right, size: 18),
          onTap: () => _openRecordInForm(i),
        );
      },
    );
  }

  Widget _buildTableView() {
    final cols = _visibleColumns;
    final theme = Theme.of(context);
    final headerStyle = TextStyle(fontSize: 12, fontWeight: FontWeight.bold, color: theme.colorScheme.onSurface);
    const colWidth = 180.0;

    Widget headerCell(ColumnModel c) {
      final sorted = _sortField == c.name;
      return InkWell(
        onTap: () => _applySort(c.name, sorted ? !_sortAscending : true),
        child: Container(
          width: colWidth,
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 10),
          child: Row(
            children: [
              Expanded(child: Text(c.displayName, style: headerStyle, overflow: TextOverflow.ellipsis)),
              if (sorted) Icon(_sortAscending ? Icons.arrow_upward : Icons.arrow_downward, size: 14),
            ],
          ),
        ),
      );
    }

    return Scrollbar(
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: SizedBox(
          width: 56 + cols.length * colWidth,
          child: Column(
            children: [
              Container(
                color: theme.colorScheme.surfaceContainerHighest,
                child: Row(children: [
                  const SizedBox(width: 56, child: Center(child: Text('#', style: TextStyle(fontSize: 12)))),
                  ...cols.map(headerCell),
                ]),
              ),
              Expanded(
                child: ListView.builder(
                  itemCount: _records.length,
                  itemExtent: 36,
                  itemBuilder: (ctx, i) {
                    final r = _records[i];
                    final selected = i == _currentIndex;
                    return Material(
                      color: selected
                          ? theme.colorScheme.primary.withValues(alpha: 0.10)
                          : (i.isOdd ? theme.colorScheme.surfaceContainerLowest : Colors.transparent),
                      child: InkWell(
                        onTap: () => _goToRecordIndex(i),
                        onDoubleTap: () => _openRecordInForm(i),
                        child: Row(children: [
                          SizedBox(
                            width: 56,
                            child: Center(
                              child: Text('${i + 1}', style: TextStyle(fontSize: 11, color: theme.colorScheme.onSurfaceVariant)),
                            ),
                          ),
                          ...cols.map((c) => Container(
                                width: colWidth,
                                padding: const EdgeInsets.symmetric(horizontal: 10),
                                alignment: Alignment.centerLeft,
                                child: Text(_columnText(c.name, r[c.name]),
                                    style: const TextStyle(fontSize: 12), overflow: TextOverflow.ellipsis),
                              )),
                        ]),
                      ),
                    );
                  },
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildOperatorsMenu({String? targetField}) {
    return PopupMenuButton<String>(
      tooltip: 'Formulas & Operators Reference',
      icon: const Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.functions, size: 16),
          SizedBox(width: 4),
          Text('Operators', style: TextStyle(fontSize: 12)),
          Icon(Icons.arrow_drop_down, size: 16),
        ],
      ),
      onSelected: (op) {
        final fieldToUse = targetField ?? _lastFocusedFindField ?? (widget.table.columns.isNotEmpty ? widget.table.columns.first.name : null);
        if (fieldToUse != null) {
          final ctrl = _findControllers[fieldToUse];
          if (ctrl != null) {
            final cur = ctrl.text;
            ctrl.text = '$cur$op';
            ctrl.selection = TextSelection.fromPosition(TextPosition(offset: ctrl.text.length));
          }
        }
      },
      itemBuilder: (ctx) => const [
        PopupMenuItem(value: '*', child: Text('*  Wildcard (zero or more characters)')),
        PopupMenuItem(value: '...', child: Text('...  Range (e.g. 10...50 or 2024-01-01...2024-12-31)')),
        PopupMenuItem(value: '=', child: Text('=  Exact word match (or = alone for empty field)')),
        PopupMenuItem(value: '==', child: Text('==  Strict entire field match')),
        PopupMenuItem(value: '!', child: Text('!  Not equal / omit')),
        PopupMenuItem(value: '>', child: Text('>  Greater than')),
        PopupMenuItem(value: '<', child: Text('<  Less than')),
        PopupMenuItem(value: '>=', child: Text('>=  Greater than or equal')),
        PopupMenuItem(value: '<=', child: Text('<=  Less than or equal')),
        PopupMenuItem(value: '//', child: Text('//  Today\'s date formula')),
        PopupMenuItem(value: '@', child: Text('@  Single character wildcard')),
      ],
    );
  }

  Widget _buildEmptyState() {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(Icons.inbox, size: 48, color: Colors.grey),
          const SizedBox(height: 12),
          Text('No records in table "${widget.table.displayName}"'),
          const SizedBox(height: 12),
          FilledButton.icon(
            icon: const Icon(Icons.add),
            label: const Text('Create First Record'),
            onPressed: _createNewRecord,
          ),
        ],
      ),
    );
  }


  Widget _buildBrowseModeContent() {
    if (_viewMode == 'form' && widget.layout != null && widget.layout!.objects.isNotEmpty) {
      return _buildLayoutCanvasView(widget.layout!, isFindMode: false);
    }
    return _buildStandardCardContent();
  }

  Widget _buildLayoutCanvasView(LayoutDefinitionModel layout, {bool isFindMode = false}) {
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final record = _records.isNotEmpty && !isFindMode
        ? _records[_currentIndex]
        : <String, dynamic>{};

    // The canvas has the layout's size: it ends at the bottom of the footer
    // (or of an object placed below it). Layouts without parts fit their objects.
    double maxObjY = 0.0;
    for (final obj in layout.objects) {
      final bottom = obj.y + obj.height;
      if (bottom > maxObjY) maxObjY = bottom;
    }
    final canvasHeight =
        layout.height > 0 ? math.max(layout.height, maxObjY) : math.max(maxObjY + 60.0, 520.0);
    final canvasWidth = layout.width;
    final hasCustomBackground = layout.backgroundColor != null || layout.backgroundImage != null;
    final tabRank = {
      for (final (i, o) in sortLayoutTabStops(layout.objects.where((o) => o.isTabStop)).indexed) o.id: i + 1,
    };

    return LayoutEntryTransition(
      key: ValueKey('layout-entry-${layout.id}'),
      effect: layout.transition,
      child: SingleChildScrollView(
      scrollDirection: Axis.vertical,
      padding: const EdgeInsets.all(20),
      child: Center(
        child: SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              // Header banner above canvas (Find mode instructions; the record
              // bar above shows the record position in Browse mode)
              if (isFindMode)
              Container(
                width: canvasWidth,
                margin: const EdgeInsets.only(bottom: 8),
                padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
                decoration: BoxDecoration(
                  color: isDark ? const Color(0xFF1E232B) : Colors.white,
                  borderRadius: BorderRadius.circular(6),
                  border: Border.all(
                    color: isDark ? const Color(0xFF38404B) : const Color(0xFFD1CFCA),
                  ),
                ),
                child: Row(
                  children: [
                    Icon(
                      isFindMode ? Icons.manage_search : Icons.view_quilt,
                      size: 16,
                      color: const Color(0xFF1E88E5),
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        isFindMode
                            ? 'Find Mode on "${layout.name}" • Enter criteria in fields and press Perform Find'
                            : _records.isEmpty
                                ? '${widget.table.displayName} • ${layout.name} • (0 records in table)'
                                : '${widget.table.displayName} • ${layout.name} • Record ${_currentIndex + 1} of ${_records.length}',
                        style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                    const SizedBox(width: 8),
                    if (!isFindMode && _records.isEmpty)
                      FilledButton.icon(
                        style: FilledButton.styleFrom(
                          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                          minimumSize: const Size(0, 28),
                        ),
                        icon: const Icon(Icons.add, size: 14),
                        label: const Text('Create First Record', style: TextStyle(fontSize: 11)),
                        onPressed: _createNewRecord,
                      )
                    else if (!isFindMode)
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                        decoration: BoxDecoration(
                          color: Colors.green.withValues(alpha: 0.12),
                          borderRadius: BorderRadius.circular(4),
                          border: Border.all(color: Colors.green.shade600, width: 0.8),
                        ),
                        child: const Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Icon(Icons.sync, size: 12, color: Colors.green),
                            SizedBox(width: 4),
                            Text('Auto-saved to DB',
                                style: TextStyle(
                                    fontSize: 10,
                                    fontWeight: FontWeight.bold,
                                    color: Colors.green)),
                          ],
                        ),
                      )
                    else
                      FilledButton.tonalIcon(
                        style: FilledButton.styleFrom(
                          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                          minimumSize: const Size(0, 28),
                        ),
                        icon: const Icon(Icons.search, size: 14),
                        label: const Text('Perform Find', style: TextStyle(fontSize: 11)),
                        onPressed: _performFind,
                      ),
                  ],
                ),
              ),

              // Layout Canvas Surface
              Container(
                width: canvasWidth,
                height: canvasHeight,
                clipBehavior: hasCustomBackground ? Clip.antiAlias : Clip.none,
                decoration: BoxDecoration(
                  color: isDark ? const Color(0xFF161B22) : Colors.white,
                  borderRadius: BorderRadius.circular(8),
                  border: Border.all(
                    color: isDark ? const Color(0xFF38404B) : const Color(0xFFD1CFCA),
                  ),
                  boxShadow: [
                    BoxShadow(
                      color: Colors.black.withValues(alpha: isDark ? 0.35 : 0.08),
                      blurRadius: 10,
                      offset: const Offset(0, 3),
                    ),
                  ],
                ),
                // The Tab key follows the layout's tab order (Set Tab Order in Layout mode).
                child: FocusTraversalGroup(
                  policy: OrderedTraversalPolicy(),
                  child: Stack(
                    clipBehavior: Clip.none,
                    children: [
                      if (hasCustomBackground) Positioned.fill(child: LayoutBackgroundView(layout: layout)),
                      // Subtle part boundaries
                      ..._buildPartDividers(layout, canvasWidth, isDark),

                      // Render layout objects
                      ...layout.objects.map((obj) {
                        Widget child = _buildLayoutObject(obj, record, isDark, isFindMode);
                        final rank = tabRank[obj.id];
                        if (rank != null) {
                          child = FocusTraversalOrder(order: NumericFocusOrder(rank.toDouble()), child: child);
                        }
                        return Positioned(
                          left: obj.x,
                          top: obj.y,
                          width: obj.width,
                          height: obj.height,
                          child: child,
                        );
                      }),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
      ),
    );
  }

  Widget _buildLayoutObject(
      LayoutObjectModel obj, Map<String, dynamic> record, bool isDark, bool isFindMode) {
    switch (obj.type) {
      case 'label':
        return Container(
          alignment: _parseAlignment(obj.style.textAlign),
          padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
          child: Text(
            isFindMode ? obj.text : actionResolveText(obj.text),
            textAlign: _parseTextAlign(obj.style.textAlign),
            style: TextStyle(
              fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 13,
              fontWeight: obj.style.fontWeight == 'bold'
                  ? FontWeight.bold
                  : FontWeight.normal,
              color: obj.style.textColor != null
                  ? _parseColor(obj.style.textColor!)
                  : (isDark ? Colors.white : Colors.black87),
            ),
          ),
        );

      case 'field':
        final fieldName = obj.fieldBinding?.fieldName ?? obj.text;
        final col = widget.table.columns
            .where((c) => c.name.toLowerCase() == fieldName.toLowerCase())
            .firstOrNull;

        if (col == null) {
          return Container(
            alignment: Alignment.centerLeft,
            padding: const EdgeInsets.symmetric(horizontal: 8),
            decoration: BoxDecoration(
              color: Colors.amber.withValues(alpha: 0.1),
              border: Border.all(color: Colors.amber),
              borderRadius: BorderRadius.circular(obj.style.cornerRadius),
            ),
            child: Text(
              '<$fieldName>',
              style: const TextStyle(fontSize: 11, color: Colors.amber),
            ),
          );
        }

        if (isFindMode) {
          final findCtrl = _findControllers[col.name];
          return TextField(
            controller: findCtrl,
            textAlign: _parseTextAlign(obj.style.textAlign),
            style: TextStyle(
              fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 13,
            ),
            decoration: InputDecoration(
              isDense: true,
              hintText: 'Search ${col.displayName}...',
              hintStyle: TextStyle(fontSize: 11, color: Colors.grey.shade400),
              contentPadding:
                  const EdgeInsets.symmetric(horizontal: 8, vertical: 8),
              border: OutlineInputBorder(
                borderRadius: BorderRadius.circular(obj.style.cornerRadius),
              ),
              suffixIcon: const Icon(Icons.search, size: 14, color: Colors.grey),
            ),
            onTap: () => setState(() => _lastFocusedFindField = col.name),
            onSubmitted: (_) => _performFind(),
          );
        }

        if (_records.isEmpty) {
          return InkWell(
            onTap: _createNewRecord,
            borderRadius: BorderRadius.circular(obj.style.cornerRadius),
            child: IgnorePointer(
              child: TextField(
                textAlign: _parseTextAlign(obj.style.textAlign),
                style: TextStyle(
                  fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 13,
                ),
                decoration: InputDecoration(
                  isDense: true,
                  hintText: 'Click to create record & enter data...',
                  hintStyle: TextStyle(fontSize: 10.5, fontStyle: FontStyle.italic, color: Colors.grey.shade400),
                  contentPadding: const EdgeInsets.symmetric(horizontal: 8, vertical: 8),
                  fillColor: obj.style.fillColor != null ? _parseColor(obj.style.fillColor!) : null,
                  filled: obj.style.fillColor != null,
                  border: OutlineInputBorder(
                    borderRadius: BorderRadius.circular(obj.style.cornerRadius),
                  ),
                ),
              ),
            ),
          );
        }

        if (col.isPrimaryKey) {
          final pkVal = record[col.name]?.toString() ?? '';
          return Container(
            alignment: Alignment.centerLeft,
            padding: const EdgeInsets.symmetric(horizontal: 8),
            decoration: BoxDecoration(
              color: isDark ? const Color(0xFF272D37) : Colors.grey.shade100,
              border: Border.all(
                  color: isDark ? const Color(0xFF38404B) : Colors.grey.shade300),
              borderRadius: BorderRadius.circular(obj.style.cornerRadius),
            ),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    pkVal,
                    style: TextStyle(
                      fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 12,
                      fontFamily: 'monospace',
                    ),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                const Icon(Icons.key, size: 14, color: Colors.grey),
              ],
            ),
          );
        }

        final ctrl = _fieldControllers[col.name];
        final fn = _fieldFocusNodes[col.name];
        final saving = _fieldSaving[col.name];

        return TextField(
          controller: ctrl,
          focusNode: fn,
          textAlign: _parseTextAlign(obj.style.textAlign),
          style: TextStyle(
            fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 13,
            fontWeight: obj.style.fontWeight == 'bold'
                ? FontWeight.bold
                : FontWeight.normal,
            color: obj.style.textColor != null
                ? _parseColor(obj.style.textColor!)
                : null,
          ),
          decoration: InputDecoration(
            isDense: true,
            contentPadding:
                const EdgeInsets.symmetric(horizontal: 8, vertical: 8),
            fillColor: obj.style.fillColor != null
                ? _parseColor(obj.style.fillColor!)
                : null,
            filled: obj.style.fillColor != null,
            border: OutlineInputBorder(
              borderRadius: BorderRadius.circular(obj.style.cornerRadius),
              borderSide: BorderSide(
                color: obj.style.borderColor != null
                    ? _parseColor(obj.style.borderColor!)
                    : Colors.grey,
                width: obj.style.borderWidth,
              ),
            ),
            suffixIcon: saving == true
                ? const Padding(
                    padding: EdgeInsets.all(8),
                    child: SizedBox(
                      width: 12,
                      height: 12,
                      child: CircularProgressIndicator(strokeWidth: 1.5),
                    ),
                  )
                : saving == false
                    ? const Icon(Icons.error_outline, size: 14, color: Colors.red)
                    : null,
          ),
          onChanged: (v) => _onFieldChanged(col.name, v),
          onEditingComplete: () {
            _fieldDebounceTimers[col.name]?.cancel();
            _saveField(col.name, ctrl?.text ?? '');
            fn?.nextFocus();
          },
        );

      case 'button':
      case 'popover_button':
        final btnText = actionResolveText(obj.text.isEmpty ? 'Button' : obj.text);
        final btnBorder = parseLayoutColor(obj.style.borderColor);
        return ElevatedButton(
          style: ElevatedButton.styleFrom(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
            backgroundColor: parseLayoutColor(obj.style.fillColor),
            foregroundColor: parseLayoutColor(obj.style.textColor),
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(obj.style.cornerRadius),
              side: btnBorder == null || obj.style.borderWidth <= 0
                  ? BorderSide.none
                  : BorderSide(color: btnBorder, width: obj.style.borderWidth),
            ),
          ),
          onPressed: isFindMode && obj.action == null ? null : () => _handleLayoutButtonClick(obj, btnText),
          child: Text(
            btnText,
            style: TextStyle(
              fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 12,
              fontWeight: obj.style.fontWeight == 'bold'
                  ? FontWeight.bold
                  : FontWeight.w600,
            ),
            overflow: TextOverflow.ellipsis,
          ),
        );

      case 'portal':
        return Container(
          decoration: BoxDecoration(
            color: isDark ? const Color(0xFF272D37) : Colors.grey.shade50,
            border: Border.all(
                color: isDark ? const Color(0xFF38404B) : Colors.grey.shade300),
            borderRadius: BorderRadius.circular(obj.style.cornerRadius),
          ),
          padding: const EdgeInsets.all(8),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  const Icon(Icons.table_rows, size: 14, color: Color(0xFF1E88E5)),
                  const SizedBox(width: 4),
                  Text(
                    obj.text.isEmpty ? 'Portal / Related Records' : obj.text,
                    style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold),
                  ),
                ],
              ),
              const Divider(height: 12),
              const Expanded(
                child: Center(
                  child: Text('No related records',
                      style: TextStyle(fontSize: 10, color: Colors.grey)),
                ),
              ),
            ],
          ),
        );

      default:
        final drawn = buildDrawnLayoutObject(obj,
            record: isFindMode ? null : record, userName: widget.currentUserName, pageNumber: 1);
        if (drawn != null) return drawn;
        return Container(
          decoration: BoxDecoration(
            color: obj.style.fillColor != null
                ? _parseColor(obj.style.fillColor!)
                : Colors.transparent,
            border: Border.all(
              color: obj.style.borderColor != null
                  ? _parseColor(obj.style.borderColor!)
                  : Colors.grey.shade400,
              width: obj.style.borderWidth,
            ),
            borderRadius: BorderRadius.circular(obj.style.cornerRadius),
          ),
        );
    }
  }

  /// Runs the button's assigned action. Buttons from older layouts without an
  /// action keep the previous behaviour, guessed from their label.
  Future<void> _handleLayoutButtonClick(LayoutObjectModel obj, String label) async {
    final action = obj.action;
    if (action == null) {
      _handleLegacyButtonLabel(label);
      return;
    }
    final result = await LayoutActionRunner(apiClient: widget.apiClient, host: this).run(action);
    if (!result.completed && mounted && result.message != null) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(result.message!), backgroundColor: Colors.orange.shade800),
      );
    }
  }

  // ─── LayoutActionHost (button actions and scripts) ─────────────────────────

  Map<String, dynamic> get _currentRecord => _records.isNotEmpty ? _records[_currentIndex] : const {};

  @override
  String actionResolveText(String text) =>
      resolveLayoutMergeText(text, record: _currentRecord, userName: widget.currentUserName, pageNumber: 1);

  @override
  Future<void> actionNewRecord() => _createNewRecord();

  @override
  Future<void> actionDuplicateRecord() async {
    if (_records.isEmpty) return;
    if (!await _saveCurrentRecord()) return;
    final source = _records[_currentIndex];
    final copy = <String, dynamic>{
      for (final col in widget.table.columns)
        if (!col.isPrimaryKey) col.name: source[col.name],
    };
    try {
      await widget.apiClient.insertRow(widget.table.name, copy);
      await _fetchRecords();
      if (_records.isNotEmpty && mounted) {
        setState(() => _currentIndex = _records.length - 1);
        _rebuildFieldControllers();
        widget.onRecordChanged?.call(_currentIndex, _records.length);
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Error duplicating record: $e'), backgroundColor: Colors.red),
        );
      }
    }
  }

  @override
  Future<void> actionDeleteRecord({required bool confirm}) async {
    if (_records.isEmpty) return;
    if (confirm) {
      final ok = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Delete Record'),
          content: const Text('Permanently delete this entire record?'),
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
      if (ok != true) return;
    }
    await _deleteCurrentRecord();
  }

  @override
  Future<void> actionCommitRecord() async {
    FocusManager.instance.primaryFocus?.unfocus();
    await _saveCurrentRecord();
  }

  @override
  Future<void> actionRevertRecord() async {
    FocusManager.instance.primaryFocus?.unfocus();
    if (!mounted) return;
    // Reverting a new record that was never committed discards it
    setState(_onDraft ? _discardDraft : _rebuildFieldControllers);
  }

  @override
  Future<void> actionGoToRecord(String target) async {
    if (_records.isEmpty) return;
    final t = target.trim().toLowerCase();
    final index = switch (t) {
      'first' || 'primero' => 0,
      'previous' || 'prev' || 'anterior' => _currentIndex - 1,
      'next' || 'siguiente' => _currentIndex + 1,
      'last' || 'último' || 'ultimo' => _records.length - 1,
      _ => (int.tryParse(t) ?? (_currentIndex + 2)) - 1,
    };
    goToRecord(index.clamp(0, _records.length - 1));
  }

  @override
  Future<void> actionEnterFindMode() async => widget.onModeChanged?.call(OperationalMode.find);

  @override
  Future<void> actionPerformFind() => _performFind();

  @override
  Future<void> actionShowAllRecords() async {
    widget.onModeChanged?.call(OperationalMode.browse);
    await _fetchRecords();
  }

  @override
  Future<void> actionEnterPreviewMode() async => widget.onModeChanged?.call(OperationalMode.preview);

  @override
  Future<bool> actionGoToLayout(String layoutName) async => widget.onGoToLayout?.call(layoutName) ?? false;

  @override
  Future<bool> actionSetField(String field, String value) async {
    final col = widget.table.columns.where((c) => c.name.toLowerCase() == field.toLowerCase()).firstOrNull;
    if (col == null || col.isPrimaryKey || _records.isEmpty) return col != null && !col.isPrimaryKey;
    _fieldDebounceTimers[col.name]?.cancel();
    _fieldControllers[col.name]?.text = value;
    await _saveField(col.name, value);
    return true;
  }

  @override
  Future<void> actionShowDialog(String title, String message) async {
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(title),
        content: Text(message),
        actions: [FilledButton(onPressed: () => Navigator.of(ctx).pop(), child: const Text('OK'))],
      ),
    );
  }

  @override
  Future<void> actionOpenUrl(String url) async {
    final uri = Uri.tryParse(url);
    if (uri == null || !(uri.isScheme('http') || uri.isScheme('https') || uri.isScheme('mailto'))) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Cannot open "$url".')));
      }
      return;
    }
    await launchUrl(uri, mode: LaunchMode.externalApplication);
  }

  void _handleLegacyButtonLabel(String label) {
    final lower = label.toLowerCase();
    if (lower.contains('new') || lower.contains('nuevo')) {
      createNewRecord();
    } else if (lower.contains('delete') ||
        lower.contains('borrar') ||
        lower.contains('eliminar')) {
      deleteCurrentRecord();
    } else if (lower.contains('first') || lower.contains('primero')) {
      goToRecord(0);
    } else if (lower.contains('last') ||
        lower.contains('ultimo') ||
        lower.contains('último')) {
      goToRecord(_records.length - 1);
    } else if (lower.contains('next') || lower.contains('siguiente')) {
      nextRecord();
    } else if (lower.contains('prev') || lower.contains('anterior')) {
      previousRecord();
    } else if (lower.contains('find') || lower.contains('buscar')) {
      widget.onModeChanged?.call(OperationalMode.find);
    } else if (lower.contains('print') ||
        lower.contains('imprimir') ||
        lower.contains('preview')) {
      widget.onModeChanged?.call(OperationalMode.preview);
    } else if (lower.contains('save') || lower.contains('guardar')) {
      _saveCurrentRecord();
    } else {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('Button clicked: "$label"'),
          duration: const Duration(seconds: 1),
        ),
      );
    }
  }

  TextAlign _parseTextAlign(String align) {
    switch (align) {
      case 'center':
        return TextAlign.center;
      case 'right':
        return TextAlign.right;
      default:
        return TextAlign.left;
    }
  }

  Alignment _parseAlignment(String align) {
    switch (align) {
      case 'center':
        return Alignment.center;
      case 'right':
        return Alignment.centerRight;
      default:
        return Alignment.centerLeft;
    }
  }

  Color _parseColor(String colorStr, [Color fallback = Colors.black87]) {
    try {
      if (colorStr.startsWith('#')) {
        final hex = colorStr.substring(1);
        if (hex.length == 6) return Color(int.parse('0xFF$hex'));
        if (hex.length == 8) return Color(int.parse('0x$hex'));
      }
    } catch (_) {}
    return fallback;
  }

  List<Widget> _buildPartDividers(
      LayoutDefinitionModel layout, double width, bool isDark) {
    final widgets = <Widget>[];
    double currentY = 0;
    for (final part in layout.parts) {
      currentY += part.height;
      widgets.add(
        Positioned(
          top: currentY,
          left: 0,
          right: 0,
          child: Container(
            height: 1,
            color: isDark
                ? Colors.white.withValues(alpha: 0.08)
                : Colors.black.withValues(alpha: 0.06),
          ),
        ),
      );
    }
    return widgets;
  }

  Widget _buildStandardCardContent() {
    final record = _records[_currentIndex];

    return Padding(
      padding: const EdgeInsets.all(24.0),
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 720),
          child: Card(
            elevation: 1,
            child: Padding(
              padding: const EdgeInsets.all(24.0),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  // Header with record info and live status
                  Row(
                    children: [
                      Icon(Icons.badge_outlined, size: 20, color: Theme.of(context).colorScheme.primary),
                      const SizedBox(width: 8),
                      Text(
                        '${widget.table.displayName} • Record ${_currentIndex + 1} of ${_records.length}',
                        style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 15),
                      ),
                      const Spacer(),
                      // Status pill
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                        decoration: BoxDecoration(
                          color: Colors.green.withValues(alpha: 0.12),
                          borderRadius: BorderRadius.circular(4),
                          border: Border.all(color: Colors.green.shade600, width: 0.8),
                        ),
                        child: const Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Icon(Icons.sync, size: 12, color: Colors.green),
                            SizedBox(width: 4),
                            Text('Auto-saved to DB', style: TextStyle(fontSize: 10, fontWeight: FontWeight.bold, color: Colors.green)),
                          ],
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 16),
                  const Divider(),
                  const SizedBox(height: 16),
                  // Column fields
                  ...widget.table.columns.map((col) {
                    if (col.isPrimaryKey) {
                      return Padding(
                        padding: const EdgeInsets.only(bottom: 16),
                        child: Row(
                          crossAxisAlignment: CrossAxisAlignment.center,
                          children: [
                            SizedBox(
                              width: 160,
                              child: Text(col.displayName, style: const TextStyle(fontWeight: FontWeight.bold)),
                            ),
                            Expanded(
                              child: TextFormField(
                                initialValue: fieldText(col, record[col.name]),
                                readOnly: true,
                                decoration: const InputDecoration(
                                  border: OutlineInputBorder(),
                                  isDense: true,
                                  suffixIcon: Tooltip(
                                    message: 'Primary Key',
                                    child: Icon(Icons.key, size: 16),
                                  ),
                                ),
                              ),
                            ),
                          ],
                        ),
                      );
                    }

                    final ctrl = _fieldControllers[col.name];
                    final fn = _fieldFocusNodes[col.name];
                    final saving = _fieldSaving[col.name];

                    return Padding(
                      padding: const EdgeInsets.only(bottom: 16),
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.center,
                        children: [
                          SizedBox(
                            width: 160,
                            child: Row(
                              children: [
                                Expanded(
                                  child: Text(
                                    col.displayName,
                                    style: const TextStyle(fontWeight: FontWeight.bold),
                                    overflow: TextOverflow.ellipsis,
                                  ),
                                ),
                              ],
                            ),
                          ),
                          Expanded(
                            child: TextField(
                              controller: ctrl,
                              focusNode: fn,
                              decoration: InputDecoration(
                                border: const OutlineInputBorder(),
                                isDense: true,
                                suffixIcon: saving == true
                                    ? const Padding(
                                        padding: EdgeInsets.all(10),
                                        child: SizedBox(
                                          width: 14,
                                          height: 14,
                                          child: CircularProgressIndicator(strokeWidth: 1.5),
                                        ),
                                      )
                                    : saving == false
                                        ? const Tooltip(
                                            message: 'Save error — check connection',
                                            child: Icon(Icons.error_outline, size: 16, color: Colors.red),
                                          )
                                        : null,
                              ),
                              onChanged: (v) => _onFieldChanged(col.name, v),
                              onEditingComplete: () {
                                _fieldDebounceTimers[col.name]?.cancel();
                                _saveField(col.name, ctrl?.text ?? '');
                                fn?.nextFocus();
                              },
                            ),
                          ),
                        ],
                      ),
                    );
                  }),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildFindModeForm() {
    if (_viewMode == 'form' && widget.layout != null && widget.layout!.objects.isNotEmpty) {
      return _buildLayoutCanvasView(widget.layout!, isFindMode: true);
    }
    return _buildStandardFindModeForm();
  }

  Widget _buildStandardFindModeForm() {
    final isDark = Theme.of(context).brightness == Brightness.dark;

    return SingleChildScrollView(
      padding: const EdgeInsets.symmetric(horizontal: 24.0, vertical: 20.0),
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 780),
          child: Card(
            elevation: 1.5,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(12),
              side: BorderSide(
                color: Theme.of(context).colorScheme.outlineVariant.withValues(alpha: 0.6),
              ),
            ),
            child: Padding(
              padding: const EdgeInsets.all(24.0),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  // Find Mode Header Banner
                  Container(
                    padding: const EdgeInsets.all(16.0),
                    decoration: BoxDecoration(
                      color: Theme.of(context).colorScheme.primaryContainer.withValues(alpha: isDark ? 0.3 : 0.4),
                      borderRadius: BorderRadius.circular(8),
                      border: Border.all(
                        color: Theme.of(context).colorScheme.primary.withValues(alpha: 0.2),
                      ),
                    ),
                    child: Row(
                      children: [
                        Container(
                          padding: const EdgeInsets.all(10),
                          decoration: BoxDecoration(
                            color: Theme.of(context).colorScheme.primary.withValues(alpha: 0.15),
                            shape: BoxShape.circle,
                          ),
                          child: Icon(
                            Icons.manage_search_rounded,
                            size: 24,
                            color: Theme.of(context).colorScheme.primary,
                          ),
                        ),
                        const SizedBox(width: 14),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                'Find Mode — ${widget.table.displayName}',
                                style: TextStyle(
                                  fontWeight: FontWeight.bold,
                                  fontSize: 16,
                                  color: Theme.of(context).colorScheme.onSurface,
                                ),
                              ),
                              const SizedBox(height: 3),
                              Text(
                                'Enter values or formulas in any field to search. Records matching all criteria will be displayed in Browse Mode.',
                                style: TextStyle(
                                  fontSize: 12,
                                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                                ),
                              ),
                            ],
                          ),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 16),

                  // Quick Operator Helper Chips
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'Quick Operators & Formulas:',
                        style: TextStyle(
                          fontSize: 11,
                          fontWeight: FontWeight.w600,
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                      ),
                      const SizedBox(height: 6),
                      Wrap(
                        spacing: 6,
                        runSpacing: 6,
                        children: [
                          _buildQuickOpChip('*', 'Wildcard', '*'),
                          _buildQuickOpChip('...', 'Range', '...'),
                          _buildQuickOpChip('=', 'Exact', '='),
                          _buildQuickOpChip('==', 'Strict', '=='),
                          _buildQuickOpChip('>', 'Greater', '>'),
                          _buildQuickOpChip('<', 'Less', '<'),
                          _buildQuickOpChip('//', 'Today', '//'),
                          _buildQuickOpChip('!', 'Not Equal', '!'),
                          _buildQuickOpChip('= (empty)', 'Empty Field', '='),
                          _buildQuickOpChip('* (non-empty)', 'Non-Empty', '*'),
                        ],
                      ),
                    ],
                  ),
                  const SizedBox(height: 20),
                  const Divider(),
                  const SizedBox(height: 16),

                  // Field Inputs
                  ...widget.table.columns.map((col) {
                    final ctrl = _findControllers[col.name];

                    return Padding(
                      padding: const EdgeInsets.only(bottom: 16),
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.center,
                        children: [
                          SizedBox(
                            width: 170,
                            child: Row(
                              children: [
                                Expanded(
                                  child: Text(
                                    col.displayName,
                                    style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13),
                                    overflow: TextOverflow.ellipsis,
                                  ),
                                ),
                                Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 2),
                                  decoration: BoxDecoration(
                                    color: Theme.of(context).colorScheme.surfaceContainerHighest,
                                    borderRadius: BorderRadius.circular(4),
                                  ),
                                  child: Text(
                                    col.fieldType,
                                    style: TextStyle(
                                      fontSize: 9,
                                      fontWeight: FontWeight.bold,
                                      color: Theme.of(context).colorScheme.outline,
                                    ),
                                  ),
                                ),
                                const SizedBox(width: 8),
                              ],
                            ),
                          ),
                          Expanded(
                            child: Focus(
                              onFocusChange: (hasF) {
                                if (hasF) _lastFocusedFindField = col.name;
                              },
                              child: TextField(
                                controller: ctrl,
                                decoration: InputDecoration(
                                  border: const OutlineInputBorder(),
                                  isDense: true,
                                  prefixIcon: Icon(
                                    Icons.search,
                                    size: 16,
                                    color: Theme.of(context).colorScheme.primary,
                                  ),
                                  hintText: _findHintForType(col.fieldType),
                                  suffixIcon: Row(
                                    mainAxisSize: MainAxisSize.min,
                                    children: [
                                      _buildOperatorsMenu(targetField: col.name),
                                      if (ctrl != null && ctrl.text.isNotEmpty)
                                        IconButton(
                                          icon: const Icon(Icons.clear, size: 16),
                                          tooltip: 'Clear field',
                                          onPressed: () {
                                            ctrl.clear();
                                            setState(() {});
                                          },
                                        ),
                                    ],
                                  ),
                                ),
                                onSubmitted: (_) => _performFind(),
                              ),
                            ),
                          ),
                        ],
                      ),
                    );
                  }),

                  const SizedBox(height: 8),
                  const Divider(),
                  const SizedBox(height: 16),

                  // Bottom Action Buttons
                  Row(
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: [
                      OutlinedButton.icon(
                        icon: const Icon(Icons.close, size: 16),
                        label: const Text('Cancel Find'),
                        onPressed: () {
                          widget.onModeChanged?.call(OperationalMode.browse);
                          _fetchRecords();
                        },
                      ),
                      const SizedBox(width: 10),
                      OutlinedButton.icon(
                        icon: const Icon(Icons.clear_all, size: 16),
                        label: const Text('Clear Criteria'),
                        onPressed: () {
                          for (var c in _findControllers.values) c.clear();
                          setState(() {});
                        },
                      ),
                      const SizedBox(width: 10),
                      FilledButton.icon(
                        icon: const Icon(Icons.search, size: 18),
                        label: const Text('Perform Find (Enter)'),
                        onPressed: _performFind,
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildQuickOpChip(String label, String tooltip, String insertText) {
    return ActionChip(
      visualDensity: VisualDensity.compact,
      label: Text(label, style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold)),
      tooltip: tooltip,
      onPressed: () {
        final field = _lastFocusedFindField ?? (widget.table.columns.isNotEmpty ? widget.table.columns.first.name : null);
        if (field != null) {
          final ctrl = _findControllers[field];
          if (ctrl != null) {
            final cur = ctrl.text;
            ctrl.text = '$cur$insertText';
            ctrl.selection = TextSelection.fromPosition(TextPosition(offset: ctrl.text.length));
          }
        }
      },
    );
  }

  String _findHintForType(String fieldType) {
    switch (fieldType) {
      case 'NUMBER':
        return 'e.g. >100, 10...50, 0, = (empty)';
      case 'DATE':
      case 'TIMESTAMP':
        return 'e.g. 2026-09-24, 2024-01-01...2024-12-31, // (today)';
      default:
        return 'e.g. Mario*, =Exact, !=Excluded, * (non-empty)';
    }
  }
}

/// Record number box of the Browse record bar: type a number and press Enter
/// to jump to that record.
class _RecordNumberField extends StatelessWidget {
  final TextEditingController controller;
  final FocusNode focusNode;
  final bool enabled;
  final ValueChanged<String> onSubmitted;

  const _RecordNumberField({
    required this.controller,
    required this.focusNode,
    required this.enabled,
    required this.onSubmitted,
  });

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 56,
      height: 30,
      child: TextField(
        controller: controller,
        focusNode: focusNode,
        enabled: enabled,
        textAlign: TextAlign.center,
        keyboardType: TextInputType.number,
        inputFormatters: [FilteringTextInputFormatter.digitsOnly],
        style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
        decoration: const InputDecoration(
          isDense: true,
          contentPadding: EdgeInsets.symmetric(horizontal: 4, vertical: 6),
          border: OutlineInputBorder(),
        ),
        onSubmitted: onSubmitted,
      ),
    );
  }
}
