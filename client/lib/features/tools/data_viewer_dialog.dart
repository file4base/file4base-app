// #50 — Tools > Data Viewer. What the values actually are while you work:
// the fields of the record in hand, and expressions you watch, evaluated by
// the same engine that fills the calculation fields.

import 'package:flutter/material.dart';

import '../../core/api/api_client.dart';

/// One watched expression and what it last came to.
class WatchedExpression {
  final String expression;
  final EvaluationModel? result;

  const WatchedExpression({required this.expression, this.result});

  WatchedExpression withResult(EvaluationModel? result) =>
      WatchedExpression(expression: expression, result: result);
}

class DataViewerDialog extends StatefulWidget {
  final ApiClient apiClient;
  final TableModel? table;

  /// The record in hand, and its id. The viewer evaluates against that
  /// record; without one every field reads as empty, which the Current tab
  /// says rather than showing an empty list.
  final Map<String, dynamic>? record;
  final String? recordId;

  /// Expressions carried between openings, so the watch list survives
  /// closing the dialog.
  final List<String> initialWatches;
  final ValueChanged<List<String>>? onWatchesChanged;

  const DataViewerDialog({
    super.key,
    required this.apiClient,
    required this.table,
    this.record,
    this.recordId,
    this.initialWatches = const [],
    this.onWatchesChanged,
  });

  static Future<void> show(
    BuildContext context, {
    required ApiClient apiClient,
    required TableModel? table,
    Map<String, dynamic>? record,
    String? recordId,
    List<String> initialWatches = const [],
    ValueChanged<List<String>>? onWatchesChanged,
  }) {
    return showDialog<void>(
      context: context,
      builder: (_) => DataViewerDialog(
        apiClient: apiClient,
        table: table,
        record: record,
        recordId: recordId,
        initialWatches: initialWatches,
        onWatchesChanged: onWatchesChanged,
      ),
    );
  }

  @override
  State<DataViewerDialog> createState() => _DataViewerDialogState();
}

class _DataViewerDialogState extends State<DataViewerDialog> with SingleTickerProviderStateMixin {
  late final TabController _tabs = TabController(length: 3, vsync: this);
  late List<WatchedExpression> _watches =
      widget.initialWatches.map((e) => WatchedExpression(expression: e)).toList();

  final TextEditingController _newWatch = TextEditingController();
  String _resultType = 'Text';
  bool _busy = false;
  String? _error;

  static const List<String> _resultTypes = ['Text', 'Number', 'Date', 'Timestamp', 'Boolean'];

  @override
  void initState() {
    super.initState();
    if (_watches.isNotEmpty) _evaluateAll();
  }

  @override
  void dispose() {
    _tabs.dispose();
    _newWatch.dispose();
    super.dispose();
  }

  Future<void> _evaluateAll() async {
    final table = widget.table;
    if (table == null || _watches.isEmpty) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final results = await widget.apiClient.evaluateExpressions(
        table.name,
        expressions: [for (final watch in _watches) watch.expression],
        recordId: widget.recordId,
        resultType: _resultType,
      );
      if (!mounted) return;
      setState(() {
        for (var i = 0; i < _watches.length && i < results.length; i++) {
          _watches[i] = _watches[i].withResult(results[i]);
        }
        _busy = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  void _addWatch() {
    final expression = _newWatch.text.trim();
    if (expression.isEmpty) return;
    setState(() {
      _watches = [..._watches, WatchedExpression(expression: expression)];
      _newWatch.clear();
    });
    widget.onWatchesChanged?.call([for (final watch in _watches) watch.expression]);
    _evaluateAll();
  }

  void _removeWatch(int index) {
    setState(() => _watches = [
          for (var i = 0; i < _watches.length; i++)
            if (i != index) _watches[i],
        ]);
    widget.onWatchesChanged?.call([for (final watch in _watches) watch.expression]);
  }

  @override
  Widget build(BuildContext context) {
    final table = widget.table;
    return AlertDialog(
      title: Row(
        children: [
          const Icon(Icons.travel_explore_outlined, color: Color(0xFF1E88E5)),
          const SizedBox(width: 8),
          const Text('Data Viewer', style: TextStyle(fontSize: 16, fontWeight: FontWeight.bold)),
          const Spacer(),
          if (table != null)
            Text(
              widget.recordId == null
                  ? '${table.displayName} · no record'
                  : '${table.displayName} · record ${widget.recordId}',
              style: const TextStyle(fontSize: 11, color: Colors.grey),
            ),
        ],
      ),
      content: SizedBox(
        width: 720,
        height: 460,
        child: table == null
            ? const Center(
                child: Text('Choose a table to look at its values.',
                    style: TextStyle(fontSize: 13, color: Colors.grey)),
              )
            : Column(
                children: [
                  TabBar(
                    controller: _tabs,
                    labelStyle: const TextStyle(fontSize: 12.5, fontWeight: FontWeight.bold),
                    tabs: const [
                      Tab(text: 'Current record'),
                      Tab(text: 'Watch'),
                      Tab(text: 'Variables'),
                    ],
                  ),
                  const SizedBox(height: 8),
                  Expanded(
                    child: TabBarView(
                      controller: _tabs,
                      children: [_currentTab(table), _watchTab(table), _variablesTab()],
                    ),
                  ),
                ],
              ),
      ),
      actions: [
        if (table != null)
          TextButton.icon(
            key: const ValueKey('viewer-refresh'),
            icon: const Icon(Icons.refresh, size: 16),
            label: const Text('Re-evaluate'),
            onPressed: _busy ? null : _evaluateAll,
          ),
        FilledButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Close')),
      ],
    );
  }

  /// The fields of the record in hand, with what they hold.
  Widget _currentTab(TableModel table) {
    final record = widget.record;
    if (record == null) {
      return const Center(
        child: Text('No record is in hand. Browse to one to see its fields.',
            style: TextStyle(fontSize: 13, color: Colors.grey)),
      );
    }
    return ListView(
      key: const ValueKey('viewer-current'),
      children: [
        for (final column in table.columns)
          _valueRow(
            name: column.name,
            label: column.displayName,
            type: column.fieldType,
            value: record[column.name],
          ),
      ],
    );
  }

  Widget _valueRow({
    required String name,
    required String label,
    required String type,
    required Object? value,
  }) {
    final text = value?.toString() ?? '';
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 3),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 220,
            child: Text.rich(TextSpan(children: [
              TextSpan(text: label, style: const TextStyle(fontSize: 12)),
              TextSpan(text: '  $name', style: const TextStyle(fontSize: 10.5, color: Colors.grey)),
            ])),
          ),
          SizedBox(width: 90, child: Text(type, style: const TextStyle(fontSize: 10.5, color: Colors.grey))),
          Expanded(
            child: text.isEmpty
                ? const Text('empty', style: TextStyle(fontSize: 11.5, fontStyle: FontStyle.italic, color: Colors.grey))
                : SelectableText(text, style: const TextStyle(fontSize: 12, fontFamily: 'monospace')),
          ),
        ],
      ),
    );
  }

  /// Expressions typed here, evaluated against the record in hand.
  Widget _watchTab(TableModel table) {
    return Column(
      key: const ValueKey('viewer-watch'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Expanded(
              child: TextField(
                key: const ValueKey('viewer-expression'),
                controller: _newWatch,
                style: const TextStyle(fontSize: 12, fontFamily: 'monospace'),
                decoration: const InputDecoration(
                  labelText: 'Expression',
                  hintText: 'fee_paid * 2',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
                onSubmitted: (_) => _addWatch(),
              ),
            ),
            const SizedBox(width: 8),
            SizedBox(
              width: 140,
              child: DropdownButtonFormField<String>(
                key: const ValueKey('viewer-result-type'),
                initialValue: _resultType,
                isExpanded: true,
                decoration: const InputDecoration(
                    labelText: 'Read as', border: OutlineInputBorder(), isDense: true),
                items: [
                  for (final type in _resultTypes)
                    DropdownMenuItem(value: type, child: Text(type, style: const TextStyle(fontSize: 12))),
                ],
                onChanged: (value) {
                  setState(() => _resultType = value ?? _resultType);
                  _evaluateAll();
                },
              ),
            ),
            const SizedBox(width: 8),
            FilledButton(
              key: const ValueKey('viewer-add'),
              onPressed: _busy ? null : _addWatch,
              child: const Text('Watch'),
            ),
          ],
        ),
        if (_error != null)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(_error!, key: const ValueKey('viewer-error'),
                style: TextStyle(fontSize: 11.5, color: Colors.red.shade700)),
          ),
        const SizedBox(height: 10),
        Expanded(
          child: _watches.isEmpty
              ? const Center(
                  child: Text(
                    'Nothing is watched yet. Type a formula above: it is evaluated by the same '
                    'engine that fills the calculation fields, against the record in hand.',
                    textAlign: TextAlign.center,
                    style: TextStyle(fontSize: 12, color: Colors.grey),
                  ),
                )
              : ListView.separated(
                  itemCount: _watches.length,
                  separatorBuilder: (_, __) => const Divider(height: 1),
                  itemBuilder: (context, index) => _watchRow(index, _watches[index]),
                ),
        ),
      ],
    );
  }

  Widget _watchRow(int index, WatchedExpression watch) {
    final result = watch.result;
    Widget value;
    if (result == null) {
      value = Text(_busy ? 'evaluating…' : 'not evaluated yet',
          style: const TextStyle(fontSize: 11.5, fontStyle: FontStyle.italic, color: Colors.grey));
    } else if (result.failed) {
      value = Text(result.error,
          key: ValueKey('viewer-result-error-$index'),
          style: TextStyle(fontSize: 11.5, color: Colors.red.shade700));
    } else if (result.isEmpty) {
      value = const Text('empty',
          style: TextStyle(fontSize: 11.5, fontStyle: FontStyle.italic, color: Colors.grey));
    } else {
      value = SelectableText(result.text,
          key: ValueKey('viewer-result-$index'),
          style: const TextStyle(fontSize: 12.5, fontFamily: 'monospace'));
    }

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            flex: 4,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(watch.expression, style: const TextStyle(fontSize: 12, fontFamily: 'monospace')),
                if (result != null && result.unknownFields.isNotEmpty)
                  Text(
                    '${result.unknownFields.join(', ')} '
                    '${result.unknownFields.length == 1 ? 'is not a field' : 'are not fields'} '
                    'of this table, and reads as empty',
                    style: TextStyle(fontSize: 10.5, color: Colors.orange.shade800),
                  ),
              ],
            ),
          ),
          const SizedBox(width: 10),
          Expanded(flex: 3, child: value),
          IconButton(
            tooltip: 'Stop watching',
            icon: const Icon(Icons.close, size: 16),
            onPressed: () => _removeWatch(index),
          ),
        ],
      ),
    );
  }

  /// Variables are a script's, and no script step sets one yet. The tab says
  /// so rather than showing an empty list that looks like "none are set".
  Widget _variablesTab() {
    return Center(
      key: const ValueKey('viewer-variables'),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 30),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.hourglass_empty, size: 28, color: Colors.grey.shade500),
            const SizedBox(height: 10),
            const Text(
              'No script step sets a variable yet.',
              style: TextStyle(fontSize: 13, fontWeight: FontWeight.bold),
            ),
            const SizedBox(height: 6),
            const Text(
              'Set Variable, If and Loop stop a script with a message today, so there is nothing '
              'to show here rather than nothing set. When the script runner takes them, this tab '
              'shows what a script holds while it runs, and the Script Debugger steps through it.',
              textAlign: TextAlign.center,
              style: TextStyle(fontSize: 11.5, color: Colors.grey),
            ),
          ],
        ),
      ),
    );
  }
}
