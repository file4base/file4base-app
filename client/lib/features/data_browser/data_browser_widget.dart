import 'dart:async';
import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import '../../main.dart';

class DataBrowserWidget extends StatefulWidget {
  final TableModel table;
  final ApiClient apiClient;
  final OperationalMode mode;
  final ValueChanged<OperationalMode>? onModeChanged;
  final void Function(int currentIndex, int totalRecords)? onRecordChanged;
  final VoidCallback? onTableModified;

  const DataBrowserWidget({
    super.key,
    required this.table,
    required this.apiClient,
    required this.mode,
    this.onModeChanged,
    this.onRecordChanged,
    this.onTableModified,
  });

  @override
  State<DataBrowserWidget> createState() => DataBrowserWidgetState();
}

class DataBrowserWidgetState extends State<DataBrowserWidget> {
  List<Map<String, dynamic>> _records = [];
  bool _isLoading = true;
  String? _error;
  int _currentIndex = 0;
  bool _isFoundSet = false;
  String? _lastFocusedFindField;

  int get currentIndex => _currentIndex;
  int get totalRecords => _records.length;
  bool get isOmit => _omit;
  bool get isFoundSet => _isFoundSet;

  void createNewRecord() => _createNewRecord();
  void deleteCurrentRecord() => _deleteCurrentRecord();
  void performFind() => _performFind();
  void fetchRecords() => _fetchRecords();

  void previousRecord() {
    if (_records.isNotEmpty && _currentIndex > 0) {
      _saveCurrentRecord().then((_) {
        if (!mounted) return;
        setState(() => _currentIndex--);
        _rebuildFieldControllers();
        widget.onRecordChanged?.call(_currentIndex, _records.length);
      });
    }
  }

  void nextRecord() {
    if (_records.isNotEmpty && _currentIndex < _records.length - 1) {
      _saveCurrentRecord().then((_) {
        if (!mounted) return;
        setState(() => _currentIndex++);
        _rebuildFieldControllers();
        widget.onRecordChanged?.call(_currentIndex, _records.length);
      });
    }
  }

  void goToRecord(int index) {
    if (_records.isNotEmpty && index >= 0 && index < _records.length) {
      _saveCurrentRecord().then((_) {
        if (!mounted) return;
        setState(() => _currentIndex = index);
        _rebuildFieldControllers();
        widget.onRecordChanged?.call(_currentIndex, _records.length);
      });
    }
  }

  void toggleOmit(bool val) => setState(() => _omit = val);

  // ─── Find mode ────────────────────────────────────────────────────────────
  final Map<String, TextEditingController> _findControllers = {};
  bool _omit = false;

  // ─── Per-field edit state ─────────────────────────────────────────────────
  final Map<String, TextEditingController> _fieldControllers = {};
  final Map<String, FocusNode> _fieldFocusNodes = {};
  final Map<String, Timer?> _fieldDebounceTimers = {};
  // null = idle/saved, true = saving, false = error
  final Map<String, bool?> _fieldSaving = {};

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
      final val = record[col.name]?.toString() ?? '';
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

  Future<void> _saveCurrentRecord() async {
    for (final col in widget.table.columns) {
      if (col.isPrimaryKey) continue;
      final ctrl = _fieldControllers[col.name];
      if (ctrl == null) continue;
      _fieldDebounceTimers[col.name]?.cancel();
      final cached = _records.isNotEmpty
          ? (_records[_currentIndex][col.name]?.toString() ?? '')
          : '';
      if (ctrl.text != cached) {
        await _saveField(col.name, ctrl.text);
      }
    }
  }

  @override
  void initState() {
    super.initState();
    _initFindControllers();
    _fetchRecords();
  }

  @override
  void didUpdateWidget(covariant DataBrowserWidget oldWidget) {
    super.didUpdateWidget(oldWidget);
    final columnsChanged = oldWidget.table.columns.length != widget.table.columns.length ||
        !_sameColumns(oldWidget.table.columns, widget.table.columns);
    if (oldWidget.table.id != widget.table.id || columnsChanged) {
      _initFindControllers();
      _rebuildFieldControllers();
      _fetchRecords();
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
        setState(() { _records = rows; _currentIndex = 0; _isLoading = false; });
        _rebuildFieldControllers();
        widget.onRecordChanged?.call(_currentIndex, _records.length);
      }
    } catch (e) {
      if (mounted) setState(() { _error = e.toString(); _isLoading = false; });
    }
  }

  Future<void> _createNewRecord() async {
    await _saveCurrentRecord();
    try {
      final newRecord = <String, dynamic>{};
      for (var col in widget.table.columns) {
        if (!col.isPrimaryKey) {
          newRecord[col.name] = col.fieldType == 'NUMBER'
              ? 0
              : col.fieldType == 'BOOLEAN'
                  ? false
                  : '';
        }
      }
      await widget.apiClient.insertRow(widget.table.name, newRecord);
      await _fetchRecords();
      if (_records.isNotEmpty) {
        setState(() => _currentIndex = _records.length - 1);
        _rebuildFieldControllers();
        widget.onRecordChanged?.call(_currentIndex, _records.length);
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Error creating record: $e'), backgroundColor: Colors.red),
        );
      }
    }
  }

  Future<void> _deleteCurrentRecord() async {
    if (_records.isEmpty) return;
    for (final t in _fieldDebounceTimers.values) {
      t?.cancel();
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

      // FileMaker today's date formula: // -> current ISO date
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
    return Column(
      children: [
        _buildRecordToolbar(),
        const Divider(height: 1),
        Expanded(
          child: _isLoading
              ? const Center(child: CircularProgressIndicator())
              : _error != null
                  ? Center(child: Text('Error: $_error', style: const TextStyle(color: Colors.red)))
                  : widget.mode == OperationalMode.find
                      ? _buildFindModeForm()
                      : _records.isEmpty
                          ? _buildEmptyState()
                          : _buildBrowseModeContent(),
        ),
      ],
    );
  }

  Widget _buildRecordToolbar() {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16.0, vertical: 6.0),
      color: Theme.of(context).colorScheme.surfaceContainerHighest.withValues(alpha: 0.5),
      child: Row(
        children: [
          if (widget.mode == OperationalMode.browse) ...[
            IconButton(
              icon: const Icon(Icons.first_page),
              tooltip: 'First Record',
              onPressed: _records.isNotEmpty && _currentIndex > 0
                  ? () => _saveCurrentRecord().then((_) {
                        if (!mounted) return;
                        setState(() => _currentIndex = 0);
                        _rebuildFieldControllers();
                        widget.onRecordChanged?.call(_currentIndex, _records.length);
                      })
                  : null,
            ),
            IconButton(
              icon: const Icon(Icons.navigate_before),
              tooltip: 'Previous Record',
              onPressed: _records.isNotEmpty && _currentIndex > 0 ? previousRecord : null,
            ),
            Text(
              _records.isNotEmpty ? '${_currentIndex + 1} of ${_records.length}' : '0 of 0',
              style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
            ),
            IconButton(
              icon: const Icon(Icons.navigate_next),
              tooltip: 'Next Record',
              onPressed: _records.isNotEmpty && _currentIndex < _records.length - 1 ? nextRecord : null,
            ),
            IconButton(
              icon: const Icon(Icons.last_page),
              tooltip: 'Last Record',
              onPressed: _records.isNotEmpty && _currentIndex < _records.length - 1
                  ? () => _saveCurrentRecord().then((_) {
                        if (!mounted) return;
                        setState(() => _currentIndex = _records.length - 1);
                        _rebuildFieldControllers();
                        widget.onRecordChanged?.call(_currentIndex, _records.length);
                      })
                  : null,
            ),
            const SizedBox(width: 16),
            FilledButton.icon(
              icon: const Icon(Icons.add, size: 16),
              label: const Text('New Record'),
              onPressed: _createNewRecord,
            ),
            const SizedBox(width: 8),
            OutlinedButton.icon(
              icon: const Icon(Icons.delete_outline, size: 16),
              label: const Text('Delete'),
              onPressed: _records.isNotEmpty ? _deleteCurrentRecord : null,
            ),
            const SizedBox(width: 8),
            IconButton(
              icon: const Icon(Icons.refresh),
              tooltip: 'Show All Records',
              onPressed: _fetchRecords,
            ),
            if (_isFoundSet) ...[
              const SizedBox(width: 10),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                decoration: BoxDecoration(
                  color: const Color(0xFF1E88E5).withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(16),
                  border: Border.all(color: const Color(0xFF1E88E5).withValues(alpha: 0.3)),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Icon(Icons.filter_alt, size: 14, color: Color(0xFF1E88E5)),
                    const SizedBox(width: 4),
                    Text(
                      'Found: ${_records.length}',
                      style: const TextStyle(fontSize: 12, fontWeight: FontWeight.bold, color: Color(0xFF1E88E5)),
                    ),
                    const SizedBox(width: 6),
                    InkWell(
                      onTap: _fetchRecords,
                      borderRadius: BorderRadius.circular(10),
                      child: const Tooltip(
                        message: 'Clear filter and show all records',
                        child: Icon(Icons.close, size: 14, color: Color(0xFF1E88E5)),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ] else if (widget.mode == OperationalMode.find) ...[
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
                        '${widget.table.displayName} • Ficha ${_currentIndex + 1} de ${_records.length}',
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
                                initialValue: record[col.name]?.toString() ?? '',
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
