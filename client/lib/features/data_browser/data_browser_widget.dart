import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import '../../main.dart';

class DataBrowserWidget extends StatefulWidget {
  final TableModel table;
  final ApiClient apiClient;
  final OperationalMode mode;
  final void Function(int currentIndex, int totalRecords)? onRecordChanged;

  const DataBrowserWidget({
    super.key,
    required this.table,
    required this.apiClient,
    required this.mode,
    this.onRecordChanged,
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
      setState(() => _currentIndex--);
      widget.onRecordChanged?.call(_currentIndex, _records.length);
    }
  }

  void nextRecord() {
    if (_records.isNotEmpty && _currentIndex < _records.length - 1) {
      setState(() => _currentIndex++);
      widget.onRecordChanged?.call(_currentIndex, _records.length);
    }
  }

  void goToRecord(int index) {
    if (_records.isNotEmpty && index >= 0 && index < _records.length) {
      setState(() => _currentIndex = index);
      widget.onRecordChanged?.call(_currentIndex, _records.length);
    }
  }

  void toggleOmit(bool val) {
    setState(() => _omit = val);
  }

  // Find Mode criteria per field
  final Map<String, TextEditingController> _findControllers = {};
  bool _omit = false;

  @override
  void initState() {
    super.initState();
    _initFindControllers();
    _fetchRecords();
  }

  @override
  void didUpdateWidget(covariant DataBrowserWidget oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.table.id != widget.table.id) {
      _initFindControllers();
      _fetchRecords();
    }
  }

  void _initFindControllers() {
    _findControllers.clear();
    for (var col in widget.table.columns) {
      _findControllers[col.name] = TextEditingController();
    }
  }

  @override
  void dispose() {
    for (var ctrl in _findControllers.values) {
      ctrl.dispose();
    }
    super.dispose();
  }

  Future<void> _fetchRecords() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final rows = await widget.apiClient.listRows(widget.table.name);
      if (mounted) {
        setState(() {
          _records = rows;
          _currentIndex = rows.isNotEmpty ? 0 : 0;
          _isLoading = false;
        });
        widget.onRecordChanged?.call(_currentIndex, _records.length);
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e.toString();
          _isLoading = false;
        });
      }
    }
  }

  Future<void> _createNewRecord() async {
    try {
      final newRecord = <String, dynamic>{};
      for (var col in widget.table.columns) {
        if (!col.isPrimaryKey) {
          if (col.fieldType == 'NUMBER') {
            newRecord[col.name] = 0;
          } else if (col.fieldType == 'BOOLEAN') {
            newRecord[col.name] = false;
          } else {
            newRecord[col.name] = '';
          }
        }
      }

      await widget.apiClient.insertRow(widget.table.name, newRecord);
      await _fetchRecords();
      if (_records.isNotEmpty) {
        setState(() => _currentIndex = _records.length - 1);
        widget.onRecordChanged?.call(_currentIndex, _records.length);
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Error creating record: $e')),
        );
      }
    }
  }

  Future<void> _deleteCurrentRecord() async {
    if (_records.isEmpty) return;
    final rec = _records[_currentIndex];
    final id = rec['id']?.toString();
    if (id == null) return;

    try {
      await widget.apiClient.deleteRow(widget.table.name, id);
      await _fetchRecords();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Error deleting record: $e')),
        );
      }
    }
  }

  Future<void> _performFind() async {
    final criteria = <Map<String, dynamic>>[];
    _findControllers.forEach((fieldName, controller) {
      final text = controller.text.trim();
      if (text.isNotEmpty) {
        // Parse operators
        if (text.contains('...')) {
          final parts = text.split('...');
          criteria.add({
            'field_name': fieldName,
            'operator': 'RANGE',
            'value': parts[0].trim(),
            'value_to': parts[1].trim(),
          });
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
      }
    });

    setState(() => _isLoading = true);
    try {
      final results = await widget.apiClient.executeFind(widget.table.name, [
        {'criteria': criteria, 'omit': _omit}
      ]);
      if (mounted) {
        setState(() {
          _records = results;
          _currentIndex = 0;
          _isLoading = false;
        });
        widget.onRecordChanged?.call(_currentIndex, _records.length);
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e.toString();
          _isLoading = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        // Navigation Toolbar (Browse & Find commands)
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
      color: Theme.of(context).colorScheme.surfaceContainerHighest.withOpacity(0.5),
      child: Row(
        children: [
          if (widget.mode == OperationalMode.browse) ...[
            IconButton(
              icon: const Icon(Icons.first_page),
              tooltip: 'First Record',
              onPressed: _records.isNotEmpty && _currentIndex > 0
                  ? () => setState(() => _currentIndex = 0)
                  : null,
            ),
            IconButton(
              icon: const Icon(Icons.navigate_before),
              tooltip: 'Previous Record',
              onPressed: _records.isNotEmpty && _currentIndex > 0
                  ? () => setState(() => _currentIndex--)
                  : null,
            ),
            Text(
              _records.isNotEmpty
                  ? '${_currentIndex + 1} of ${_records.length}'
                  : '0 of 0',
              style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
            ),
            IconButton(
              icon: const Icon(Icons.navigate_next),
              tooltip: 'Next Record',
              onPressed: _records.isNotEmpty && _currentIndex < _records.length - 1
                  ? () => setState(() => _currentIndex++)
                  : null,
            ),
            IconButton(
              icon: const Icon(Icons.last_page),
              tooltip: 'Last Record',
              onPressed: _records.isNotEmpty && _currentIndex < _records.length - 1
                  ? () => setState(() => _currentIndex = _records.length - 1)
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
                for (var c in _findControllers.values) {
                  c.clear();
                }
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

  Widget _buildBrowseModeContent() {
    final record = _records[_currentIndex];

    return Padding(
      padding: const EdgeInsets.all(24.0),
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 700),
          child: Card(
            elevation: 1,
            child: Padding(
              padding: const EdgeInsets.all(24.0),
              child: ListView.separated(
                itemCount: widget.table.columns.length,
                separatorBuilder: (_, __) => const SizedBox(height: 16),
                itemBuilder: (context, idx) {
                  final col = widget.table.columns[idx];
                  final val = record[col.name]?.toString() ?? '';

                  return Row(
                    crossAxisAlignment: CrossAxisAlignment.center,
                    children: [
                      SizedBox(
                        width: 160,
                        child: Text(
                          col.displayName,
                          style: const TextStyle(fontWeight: FontWeight.bold),
                        ),
                      ),
                      Expanded(
                        child: TextFormField(
                          initialValue: val,
                          readOnly: col.isPrimaryKey,
                          decoration: InputDecoration(
                            border: const OutlineInputBorder(),
                            isDense: true,
                            suffixIcon: col.isPrimaryKey
                                ? const Tooltip(
                                    message: 'Primary Key',
                                    child: Icon(Icons.key, size: 16),
                                  )
                                : null,
                          ),
                          onFieldSubmitted: (newVal) async {
                            if (col.isPrimaryKey) return;
                            try {
                              await widget.apiClient.updateRow(
                                widget.table.name,
                                record['id'].toString(),
                                {col.name: newVal},
                              );
                              await _fetchRecords();
                            } catch (e) {
                              if (context.mounted) {
                                ScaffoldMessenger.of(context).showSnackBar(
                                  SnackBar(content: Text('Error updating: $e')),
                                );
                              }
                            }
                          },
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

  Widget _buildFindModeForm() {
    return Padding(
      padding: const EdgeInsets.all(24.0),
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 700),
          child: Card(
            elevation: 1,
            color: Colors.amber.withOpacity(0.05),
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
                            Text(
                              col.displayName,
                              style: const TextStyle(fontWeight: FontWeight.bold),
                            ),
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
