import 'dart:async';
import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import '../../main.dart';

class DataBrowserWidget extends StatefulWidget {
  final TableModel table;
  final ApiClient apiClient;
  final OperationalMode mode;
  final void Function(int currentIndex, int totalRecords)? onRecordChanged;
  final VoidCallback? onTableModified;

  const DataBrowserWidget({
    super.key,
    required this.table,
    required this.apiClient,
    required this.mode,
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

  int get currentIndex => _currentIndex;
  int get totalRecords => _records.length;
  bool get isOmit => _omit;

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
    setState(() { _isLoading = true; _error = null; });
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
      final text = controller.text.trim();
      if (text.isEmpty) return;
      if (text.contains('...')) {
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
      } else if (text.startsWith('!')) {
        criteria.add({'field_name': fieldName, 'operator': '!=', 'value': text.substring(1).trim()});
      } else if (text.startsWith('=')) {
        criteria.add({'field_name': fieldName, 'operator': '=', 'value': text.substring(1).trim()});
      } else if (text.contains('*')) {
        criteria.add({'field_name': fieldName, 'operator': 'LIKE', 'value': text.replaceAll('*', '%')});
      } else {
        criteria.add({'field_name': fieldName, 'operator': 'LIKE', 'value': '%$text%'});
      }
    });

    setState(() => _isLoading = true);
    try {
      final results = await widget.apiClient.executeFind(widget.table.name, [
        {'criteria': criteria, 'omit': _omit}
      ]);
      if (mounted) {
        setState(() { _records = results; _currentIndex = 0; _isLoading = false; });
        _rebuildFieldControllers();
        widget.onRecordChanged?.call(_currentIndex, _records.length);
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
          ] else if (widget.mode == OperationalMode.find) ...[
            FilledButton.icon(
              icon: const Icon(Icons.search),
              label: const Text('Perform Find (Enter)'),
              onPressed: _performFind,
            ),
            const SizedBox(width: 12),
            FilterChip(
              label: const Text('Omit (NOT)'),
              selected: _omit,
              onSelected: (val) => setState(() => _omit = val),
            ),
            const SizedBox(width: 12),
            OutlinedButton(
              onPressed: () {
                for (var c in _findControllers.values) c.clear();
              },
              child: const Text('Clear Criteria'),
            ),
            const Spacer(),
            Text(
              'Operators: = (exact)  ! (not)  > <  ... (range)  * (wildcard)',
              style: TextStyle(fontSize: 12, color: Colors.grey[700]),
            ),
          ],
        ],
      ),
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

  Future<void> _showAddFieldDialog() async {
    final nameCtrl = TextEditingController();
    final dispCtrl = TextEditingController();
    String selectedType = 'TEXT';

    await showDialog(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDialogState) => AlertDialog(
          title: Text('New Field for "${widget.table.displayName}"'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: dispCtrl,
                autofocus: true,
                decoration: const InputDecoration(
                  labelText: 'Field Label (e.g. Phone Number)',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
                onChanged: (val) {
                  if (nameCtrl.text.isEmpty ||
                      nameCtrl.text == val.toLowerCase().replaceAll(' ', '_')) {
                    nameCtrl.text = val.toLowerCase().replaceAll(' ', '_');
                  }
                },
              ),
              const SizedBox(height: 12),
              TextField(
                controller: nameCtrl,
                decoration: const InputDecoration(
                  labelText: 'Column Identifier (e.g. phone_number)',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
              ),
              const SizedBox(height: 12),
              DropdownButtonFormField<String>(
                initialValue: selectedType,
                decoration: const InputDecoration(
                  labelText: 'Field Type',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
                items: const [
                  DropdownMenuItem(value: 'TEXT', child: Text('Text')),
                  DropdownMenuItem(value: 'NUMBER', child: Text('Number')),
                  DropdownMenuItem(value: 'DATE', child: Text('Date')),
                  DropdownMenuItem(value: 'TIMESTAMP', child: Text('Timestamp')),
                  DropdownMenuItem(value: 'BOOLEAN', child: Text('Boolean')),
                ],
                onChanged: (val) {
                  if (val != null) {
                    setDialogState(() => selectedType = val);
                  }
                },
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () async {
                final name = nameCtrl.text.trim();
                final disp = dispCtrl.text.trim();
                if (name.isEmpty || disp.isEmpty) return;
                Navigator.pop(ctx);
                try {
                  await widget.apiClient.addColumn(
                    widget.table.id,
                    name: name,
                    displayName: disp,
                    fieldType: selectedType,
                  );
                  widget.onTableModified?.call();
                } catch (e) {
                  if (mounted) {
                    ScaffoldMessenger.of(context).showSnackBar(
                      SnackBar(content: Text('Error adding field: $e'), backgroundColor: Colors.red),
                    );
                  }
                }
              },
              child: const Text('Add Field'),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _showEditFieldDialog(ColumnModel col) async {
    final dispCtrl = TextEditingController(text: col.displayName);

    await showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Edit Field "${col.displayName}"'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('SQL Identifier: ${col.name}', style: const TextStyle(fontSize: 12, color: Colors.grey)),
            const SizedBox(height: 12),
            TextField(
              controller: dispCtrl,
              autofocus: true,
              decoration: const InputDecoration(
                labelText: 'Display Name / Label',
                border: OutlineInputBorder(),
                isDense: true,
              ),
            ),
          ],
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
          FilledButton(
            onPressed: () async {
              final newDisp = dispCtrl.text.trim();
              if (newDisp.isEmpty) return;
              Navigator.pop(ctx);
              try {
                await widget.apiClient.updateColumn(widget.table.id, col.id, displayName: newDisp);
                widget.onTableModified?.call();
              } catch (e) {
                if (mounted) {
                  ScaffoldMessenger.of(context).showSnackBar(
                    SnackBar(content: Text('Error updating field: $e'), backgroundColor: Colors.red),
                  );
                }
              }
            },
            child: const Text('Save'),
          ),
        ],
      ),
    );
  }

  Future<void> _showDeleteFieldDialog(ColumnModel col) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Delete Field "${col.displayName}"?'),
        content: Text('Are you sure you want to drop column "${col.name}"? All data stored in this field across all records will be permanently deleted.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Colors.red),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Delete Field'),
          ),
        ],
      ),
    );

    if (confirmed == true) {
      try {
        await widget.apiClient.deleteColumn(widget.table.id, col.id);
        widget.onTableModified?.call();
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('Error deleting field: $e'), backgroundColor: Colors.red),
          );
        }
      }
    }
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
                            const SizedBox(width: 40),
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
                          PopupMenuButton<String>(
                            icon: const Icon(Icons.more_vert, size: 18, color: Colors.grey),
                            tooltip: 'Field Options',
                            itemBuilder: (ctx) => [
                              const PopupMenuItem(
                                value: 'edit',
                                child: Row(
                                  children: [
                                    Icon(Icons.edit_outlined, size: 16),
                                    SizedBox(width: 8),
                                    Text('Rename Field...'),
                                  ],
                                ),
                              ),
                              const PopupMenuItem(
                                value: 'delete',
                                child: Row(
                                  children: [
                                    Icon(Icons.delete_outline, size: 16, color: Colors.red),
                                    SizedBox(width: 8),
                                    Text('Delete Field...', style: TextStyle(color: Colors.red)),
                                  ],
                                ),
                              ),
                            ],
                            onSelected: (action) {
                              if (action == 'edit') {
                                _showEditFieldDialog(col);
                              } else if (action == 'delete') {
                                _showDeleteFieldDialog(col);
                              }
                            },
                          ),
                        ],
                      ),
                    );
                  }),
                  const SizedBox(height: 8),
                  const Divider(),
                  const SizedBox(height: 12),
                  // Add field button
                  Align(
                    alignment: Alignment.centerLeft,
                    child: OutlinedButton.icon(
                      icon: const Icon(Icons.add, size: 16),
                      label: const Text('Add Field to Table...'),
                      onPressed: _showAddFieldDialog,
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildFindModeForm() {
    return Padding(
      padding: const EdgeInsets.all(24.0),
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 700),
          child: Card(
            elevation: 1,
            color: Colors.amber.withValues(alpha: 0.05),
            child: Padding(
              padding: const EdgeInsets.all(24.0),
              child: ListView.separated(
                itemCount: widget.table.columns.length,
                separatorBuilder: (_, __) => const SizedBox(height: 16),
                itemBuilder: (context, idx) {
                  final col = widget.table.columns[idx];
                  final ctrl = _findControllers[col.name];

                  return Row(
                    crossAxisAlignment: CrossAxisAlignment.center,
                    children: [
                      SizedBox(
                        width: 160,
                        child: Row(
                          children: [
                            const Icon(Icons.search, size: 14, color: Colors.amber),
                            const SizedBox(width: 4),
                            Text(col.displayName,
                                style: const TextStyle(fontWeight: FontWeight.bold)),
                          ],
                        ),
                      ),
                      Expanded(
                        child: TextField(
                          controller: ctrl,
                          decoration: InputDecoration(
                            border: const OutlineInputBorder(),
                            isDense: true,
                            hintText: 'Search criteria (e.g. >100, John*, 2024...)',
                            suffixIcon: IconButton(
                              icon: const Icon(Icons.clear, size: 16),
                              onPressed: () => ctrl?.clear(),
                            ),
                          ),
                          onSubmitted: (_) => _performFind(),
                        ),
                      ),
                    ],
                  );
                },
              ),
            ),
          ),
        ),
      ),
    );
  }
}
