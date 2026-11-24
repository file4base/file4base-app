// The window side of Refresh Portal, Refresh Window and Insert Current Date:
// what a script step changes in the data browser, and what it must leave
// alone (the found set, the stored record behind a find request).
import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

final _table = TableModel(id: 't1', name: 'invoices', displayName: 'Invoices', columns: const [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'status', displayName: 'Status', fieldType: 'varchar'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'notes', displayName: 'Notes', fieldType: 'varchar'),
]);

class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');
  int listCalls = 0;
  final Map<String, Map<String, dynamic>> stored = {
    '1': {'id': '1', 'status': 'open', 'notes': 'first'},
    '2': {'id': '2', 'status': 'cancelled', 'notes': 'second'},
    '3': {'id': '3', 'status': 'cancelled', 'notes': 'pending'},
  };
  final List<List<Map<String, dynamic>>> finds = [];
  final List<Map<String, dynamic>> updates = [];

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
      {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async {
    listCalls++;
    return stored.values.map((r) => Map<String, dynamic>.of(r)).toList();
  }

  @override
  Future<Map<String, dynamic>> getRow(String table, String id) async => Map<String, dynamic>.of(stored[id]!);

  @override
  Future<List<Map<String, dynamic>>> executeFind(String table, List<Map<String, dynamic>> requests,
      {int limit = 500, int offset = 0, String? sortBy}) async {
    finds.add(requests);
    return stored.values.where((r) => r['status'] == 'cancelled').map((r) => Map<String, dynamic>.of(r)).toList();
  }

  // The relationship graph: this layout's occurrence and one other.
  @override
  Future<List<TableOccurrenceModel>> listOccurrences() async => const [
        TableOccurrenceModel(id: 'to1', baseTableId: 't1', name: 'Invoices', xPos: 0, yPos: 0),
        TableOccurrenceModel(id: 'to2', baseTableId: 't2', name: 'Payments', xPos: 0, yPos: 0),
      ];

  @override
  Future<List<RelationshipModel>> listRelationships() async => const [];

  @override
  Future<List<TableModel>> listTables() async => [_table];

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> data) async {
    updates.add({'id': id, ...data});
    stored[id]!.addAll(data);
    return Map<String, dynamic>.of(stored[id]!);
  }
}

final _layout = LayoutDefinitionModel(id: 'l', name: 'Invoices', tableOccurrence: 'to1', objects: const [
  LayoutObjectModel(id: 'f1', type: 'field', x: 40, y: 40, width: 220, height: 30, fieldBinding: FieldBindingModel(fieldName: 'status')),
  LayoutObjectModel(id: 'f2', type: 'field', x: 40, y: 90, width: 220, height: 30, fieldBinding: FieldBindingModel(fieldName: 'notes')),
  LayoutObjectModel(id: 'label', type: 'text', x: 300, y: 40, width: 120, height: 30, name: 'StatusLabel', text: 'Status'),
]);

/// The browser inside a parent that follows the mode it asks for, as the app
/// shell does.
Future<DataBrowserWidgetState> _pump(WidgetTester tester, _Api api) async {
  tester.view.physicalSize = const Size(1400, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final key = GlobalKey<DataBrowserWidgetState>();
  var mode = OperationalMode.browse;
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: StatefulBuilder(
        builder: (context, setState) => DataBrowserWidget(
          key: key,
          table: _table,
          apiClient: api,
          mode: mode,
          layout: _layout,
          onModeChanged: (m) => setState(() => mode = m),
        ),
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return key.currentState!;
}

TextEditingController _controllerShowing(WidgetTester tester, String text) => tester
    .widget<EditableText>(find.byWidgetPredicate((w) => w is EditableText && w.controller.text == text))
    .controller;

void main() {
  testWidgets('Refresh Portal on another object and Refresh Window keep the found set and the record in hand',
      (tester) async {
    final api = _Api();
    final state = await _pump(tester, api);
    await state.actionEnterFindMode();
    await state.actionPerformFind();
    await tester.pumpAndSettle();
    await state.goToRecord(1);
    await tester.pumpAndSettle();
    expect(state.isFoundSet, isTrue);
    expect(state.totalRecords, 2);
    final listCalls = api.listCalls;

    // Another user changed the record in hand.
    api.stored['3']!['notes'] = 'checked';
    expect(await state.actionRefreshPortal('StatusLabel'), isTrue);
    await tester.pumpAndSettle();
    expect(api.listCalls, listCalls, reason: 'nothing is found again');
    expect(state.isFoundSet, isTrue);
    expect(state.totalRecords, 2);
    expect(state.currentIndex, 1);
    expect(find.text('checked'), findsWidgets, reason: 'the record in hand is read again');

    api.stored['3']!['notes'] = 'checked twice';
    await state.actionRefreshWindow(flushJoinCache: true);
    await tester.pumpAndSettle();
    expect(api.listCalls, listCalls);
    expect(state.isFoundSet, isTrue);
    expect(state.totalRecords, 2);
    expect(state.currentIndex, 1);
    expect(find.text('checked twice'), findsWidgets);

    expect(await state.actionRefreshPortal('NoSuchObject'), isFalse);
  });

  testWidgets('Insert Current Date in Find mode fills the find request, not the stored record', (tester) async {
    final api = _Api();
    final state = await _pump(tester, api);
    final runner = LayoutActionRunner(apiClient: api, host: state);
    final script = ScriptModel.fromJson({
      'id': 's',
      'name': 'FindToday',
      'is_active': true,
      'steps': [
        {'id': 'a', 'sequence_idx': 1, 'step_type': 'Enter Find Mode', 'params': {'pause': false}, 'is_enabled': true},
        {
          'id': 'b',
          'sequence_idx': 2,
          'step_type': 'Insert Current Date',
          'params': {'field': 'notes', 'select': true},
          'is_enabled': true,
        },
        {'id': 'c', 'sequence_idx': 3, 'step_type': 'Perform Find', 'params': <String, dynamic>{}, 'is_enabled': true},
      ],
    });
    // The steps run back to back, before the window rebuilds in Find mode,
    // as they do when a button runs the script.
    final execution = ScriptExecution(script: script, runner: runner, session: ScriptSessionState());
    while (!execution.atEnd) {
      await execution.advance();
    }
    await tester.pumpAndSettle();

    final now = DateTime.now();
    final today = '${now.year.toString().padLeft(4, '0')}-'
        '${now.month.toString().padLeft(2, '0')}-'
        '${now.day.toString().padLeft(2, '0')}';
    expect(api.updates, isEmpty, reason: 'no record is saved');
    expect(api.stored['1']!['notes'], 'first');
    expect(api.finds, hasLength(1));
    expect(jsonEncode(api.finds.single), allOf(contains('notes'), contains(today)));
  });

  testWidgets('without Select entire contents the text replaces the selection or goes in at the caret',
      (tester) async {
    final api = _Api();
    final state = await _pump(tester, api);
    await state.goToRecord(2); // notes: pending
    await tester.pumpAndSettle();

    // Select All, then insert: the selection is replaced.
    await state.actionGoToField('notes', select: true);
    expect(await state.actionInsertIntoField(field: 'notes', text: 'D', replaceAll: false), FieldInsertOutcome.written);
    await tester.pumpAndSettle();
    expect(api.stored['3']!['notes'], 'D');

    // A caret inside the text: the text goes in there, and the caret after it.
    final ctrl = _controllerShowing(tester, 'D');
    ctrl.value = const TextEditingValue(text: 'abcd', selection: TextSelection.collapsed(offset: 2));
    await state.actionInsertIntoField(field: 'notes', text: 'X', replaceAll: false);
    await tester.pumpAndSettle();
    expect(ctrl.text, 'abXcd');
    expect(ctrl.selection, const TextSelection.collapsed(offset: 3));
    expect(api.stored['3']!['notes'], 'abXcd');

    // A field that is not the active one gets the text at its end.
    await state.actionInsertIntoField(field: 'status', text: '!', replaceAll: false);
    await tester.pumpAndSettle();
    expect(api.stored['3']!['status'], 'cancelled!');
  });

  testWidgets('a field of another table occurrence is refused, not written in the layout table', (tester) async {
    final api = _Api();
    final state = await _pump(tester, api);

    expect(await state.actionInsertIntoField(field: 'notes', text: 'D', replaceAll: true, table: 'Payments'),
        FieldInsertOutcome.relatedField);
    expect(await state.actionInsertIntoField(field: 'notes', text: 'D', replaceAll: true, table: 'Ghost'),
        FieldInsertOutcome.relatedField, reason: 'a name the graph does not place is not the layout table');
    await tester.pumpAndSettle();
    expect(api.updates, isEmpty);

    // The layout's own occurrence, by its graph name or its base table.
    expect(await state.actionInsertIntoField(field: 'notes', text: 'D', replaceAll: true, table: 'Invoices'),
        FieldInsertOutcome.written);
    expect(await state.actionInsertIntoField(field: 'status', text: 'E', replaceAll: true, table: 'invoices'),
        FieldInsertOutcome.written);
    await tester.pumpAndSettle();
    expect(api.stored['1']!['notes'], 'D');
    expect(api.stored['1']!['status'], 'E');
  });
}
