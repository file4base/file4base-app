// A script step that writes `Occurrence::Field`, where the occurrence is not
// the layout's, writes the related record as FileMaker does: the active
// portal row of that occurrence, else the first related record in the
// relationship's order, else one created through the relationship when it
// allows creation (error 101 when it does not). It never writes the field of
// the same name of the record in hand.

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

// Both tables have a `note`: the related one must never land in the parent's.
const _companies = TableModel(id: 't-companies', name: 'companies', displayName: 'Companies', columns: [
  ColumnModel(id: 'cc0', tableId: 't-companies', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'cc1', tableId: 't-companies', name: 'company', displayName: 'Company', fieldType: 'TEXT'),
  ColumnModel(id: 'cc2', tableId: 't-companies', name: 'note', displayName: 'Note', fieldType: 'TEXT'),
]);

const _customers = TableModel(id: 't-customers', name: 'customers', displayName: 'Customers', columns: [
  ColumnModel(id: 'uc0', tableId: 't-customers', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'uc1', tableId: 't-customers', name: 'company', displayName: 'Company', fieldType: 'TEXT'),
  ColumnModel(id: 'uc2', tableId: 't-customers', name: 'last_name', displayName: 'Last Name', fieldType: 'TEXT'),
  ColumnModel(id: 'uc3', tableId: 't-customers', name: 'note', displayName: 'Note', fieldType: 'TEXT'),
]);

RelationshipModel _relationship({required bool allowCreation}) => RelationshipModel(
      id: 'rel-1',
      name: 'companies_customers',
      leftOccurrenceId: 'occ-companies',
      leftColumnId: 'cc1',
      rightOccurrenceId: 'occ-customers',
      rightColumnId: 'uc1',
      allowCreation: allowCreation,
    );

class _Api extends ApiClient {
  _Api({required this.children, required this.allowCreation}) : super(baseUrl: 'http://localhost:1');

  final List<Map<String, dynamic>> children;
  final bool allowCreation;
  final Map<String, dynamic> parent = {'id': 'co-1', 'company': 'Nordic Vault', 'note': 'parent note'};
  final List<(String, String, Map<String, dynamic>)> updates = [];
  final List<Map<String, dynamic>> created = [];
  final List<(int, List<String>?)> relatedReads = [];

  @override
  Future<List<RelationshipModel>> listRelationships() async => [_relationship(allowCreation: allowCreation)];

  @override
  Future<List<TableOccurrenceModel>> listOccurrences() async => const [
        TableOccurrenceModel(id: 'occ-companies', baseTableId: 't-companies', name: 'Companies', xPos: 0, yPos: 0),
        TableOccurrenceModel(id: 'occ-customers', baseTableId: 't-customers', name: 'Customers', xPos: 0, yPos: 0),
      ];

  @override
  Future<List<TableModel>> listTables() async => [_companies, _customers];

  @override
  Future<List<ValueListModel>> listValueLists() async => const [];

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [Map<String, dynamic>.of(parent)];

  @override
  Future<Map<String, dynamic>> getRow(String table, String id) async {
    if (table == 'companies') return Map<String, dynamic>.of(parent);
    return Map<String, dynamic>.of(children.firstWhere((c) => c['id'] == id));
  }

  @override
  Future<List<Map<String, dynamic>>> listRelatedRows(String table, String id,
      {required String relationshipId,
      String? occurrence,
      String? side,
      List<String>? sort,
      int limit = 100,
      int offset = 0}) async {
    relatedReads.add((limit, sort));
    return [for (final r in children.take(limit)) Map<String, dynamic>.of(r)];
  }

  @override
  Future<Map<String, dynamic>> createRelatedRow(String table, String id,
      {required String relationshipId, String? occurrence, String? side, Map<String, dynamic> values = const {}}) async {
    created.add(Map.of(values));
    final row = {'id': 'cu-new', 'company': 'Nordic Vault', ...values};
    children.add(row);
    return row;
  }

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> values,
      {bool script = false}) async {
    updates.add((table, id, Map.of(values)));
    if (table == 'companies') {
      parent.addAll(values);
      return Map<String, dynamic>.of(parent);
    }
    final i = children.indexWhere((c) => c['id'] == id);
    if (i >= 0) children[i] = {...children[i], ...values};
    return {'id': id, ...values};
  }

  /// The writes into the related table, as `id note`.
  List<String> get relatedUpdates => [
        for (final u in updates)
          if (u.$1 == 'customers') '${u.$2} ${u.$3['note'] ?? u.$3}',
      ];

  bool get parentWritten => updates.any((u) => u.$1 == 'companies');
}

const _layout = LayoutDefinitionModel(
  id: 'layout-companies',
  name: 'Companies Form',
  tableOccurrence: 'Companies',
  parts: [
    LayoutPartModel(id: 'h', type: 'header', height: 40),
    LayoutPartModel(id: 'b', type: 'body', height: 400),
    LayoutPartModel(id: 'f', type: 'footer', height: 40),
  ],
  objects: [
    LayoutObjectModel(
        id: 'fld_note', type: 'field', x: 40, y: 50, width: 200, height: 30, fieldBinding: FieldBindingModel(fieldName: 'note')),
    LayoutObjectModel(
      id: 'portal_customers',
      type: 'portal',
      x: 40,
      y: 160,
      width: 400,
      height: 150,
      portalConfig: {'relationship_id': 'rel-1', 'occurrence': 'Customers'},
    ),
    LayoutObjectModel(
        id: 'fld_in_portal',
        type: 'field',
        x: 50,
        y: 165,
        width: 200,
        height: 24,
        fieldBinding: FieldBindingModel(fieldName: 'last_name')),
  ],
);

List<Map<String, dynamic>> _twoCustomers() => [
      {'id': 'cu-1', 'company': 'Nordic Vault', 'last_name': 'Soto', 'note': 'first'},
      {'id': 'cu-2', 'company': 'Nordic Vault', 'last_name': 'Alvarez', 'note': 'second'},
    ];

Future<(DataBrowserWidgetState, _Api)> _pump(WidgetTester tester,
    {List<Map<String, dynamic>>? children, bool allowCreation = false}) async {
  tester.view.physicalSize = const Size(1600, 1000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  // The system clipboard Paste reads.
  tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, (call) async {
    if (call.method == 'Clipboard.getData') return <String, dynamic>{'text': 'pasted'};
    return null;
  });
  addTearDown(() => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, null));
  final api = _Api(children: children ?? _twoCustomers(), allowCreation: allowCreation);
  final key = GlobalKey<DataBrowserWidgetState>();
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: DataBrowserWidget(key: key, table: _companies, apiClient: api, mode: OperationalMode.browse, layout: _layout),
    ),
  ));
  await tester.pumpAndSettle();
  return (key.currentState!, api);
}

Future<(LayoutActionResult, ScriptSessionState)> _run(
    WidgetTester tester, DataBrowserWidgetState state, _Api api, String step, Map<String, dynamic> params,
    {bool capture = false}) async {
  final session = ScriptSessionState()
    ..errorCapture = capture
    ..clipboard = 'pasted';
  final result = await LayoutActionRunner(apiClient: api, host: state)
      .runStep(step, params, depth: 0, callContext: ScriptCallContext(session: session));
  await tester.pumpAndSettle();
  return (result, session);
}

String _today() {
  final now = DateTime.now();
  return '${now.year.toString().padLeft(4, '0')}-${now.month.toString().padLeft(2, '0')}-${now.day.toString().padLeft(2, '0')}';
}

/// Each step that writes a field by name, the related field it names, and
/// what the related record then holds, from what it held (`before`).
List<(String, Map<String, dynamic>, String Function(String before))> _steps() => [
      ('Set Field', {'field': 'note', 'field_table': 'Customers', 'value': '"set"', 'value_is_calculation': true}, (_) => 'set'),
      ('Set Field', {'field': 'Customers::note', 'value': '"set"', 'value_is_calculation': true}, (_) => 'set'),
      (
        'Set Field By Name',
        {'target_field': '"Customers::" & "note"', 'target_field_is_calculation': true, 'value': '"by name"', 'value_is_calculation': true},
        (_) => 'by name'
      ),
      ('Insert Calculated Result', {'field': 'note', 'field_table': 'Customers', 'value': '"+"', 'select': false}, (b) => '$b+'),
      ('Insert Current Date', {'field': 'note', 'field_table': 'Customers', 'select': true}, (_) => _today()),
      ('Paste', {'field': 'Customers::note'}, (_) => 'pasted'),
      ('Clear', {'field': 'note', 'field_table': 'Customers'}, (_) => ''),
      ('Cut', {'field': 'Customers::note'}, (_) => ''),
    ];

void main() {
  testWidgets('each step writes the first related record, in the relationship order, not the record in hand',
      (tester) async {
    for (final (step, params, expected) in _steps()) {
      final (state, api) = await _pump(tester);
      final (result, session) = await _run(tester, state, api, step, params);
      expect(result.completed, isTrue, reason: '$step $params: ${result.message}');
      expect(session.lastError, 0, reason: '$step $params');
      expect(api.relatedUpdates, ['cu-1 ${expected('first')}'], reason: '$step $params');
      expect(api.parentWritten, isFalse, reason: '$step $params: the same-named field of the record in hand');
      expect(api.parent['note'], 'parent note');
      expect(api.relatedReads.any((r) => r.$1 == 1 && r.$2 == null), isTrue,
          reason: 'the first related record, in the order the relationship sorts them');
      if (step == 'Cut') expect(session.clipboard, 'first');
    }
  });

  testWidgets('each step writes the active portal row of that occurrence', (tester) async {
    for (final (step, params, expected) in _steps()) {
      final (state, api) = await _pump(tester);
      await tester.tap(find.byKey(const ValueKey('portal-cell-cu-2-last_name')));
      await tester.pumpAndSettle();
      final (result, session) = await _run(tester, state, api, step, params);
      expect(result.completed, isTrue, reason: '$step $params: ${result.message}');
      expect(api.relatedUpdates, ['cu-2 ${expected('second')}'], reason: '$step $params');
      expect(api.parentWritten, isFalse, reason: '$step $params');
      if (step == 'Cut') expect(session.clipboard, 'second');
    }
  });

  testWidgets('going to a field of the record leaves the portal row: the first related record is written',
      (tester) async {
    final (state, api) = await _pump(tester);
    await tester.tap(find.byKey(const ValueKey('portal-cell-cu-2-last_name')));
    await tester.pumpAndSettle();
    await tester.tap(find.byWidgetPredicate((w) => w is TextField && w.controller?.text == 'parent note'));
    await tester.pumpAndSettle();
    await _run(tester, state, api, 'Set Field', {'field': 'Customers::note', 'value': '"x"', 'value_is_calculation': true});
    expect(api.relatedUpdates, ['cu-1 x']);
  });

  testWidgets('with no related record, one is created when the relationship allows it', (tester) async {
    for (final (step, params, expected) in _steps().where((s) => s.$1 != 'Cut' && s.$1 != 'Clear')) {
      final (state, api) = await _pump(tester, children: [], allowCreation: true);
      final (result, _) = await _run(tester, state, api, step, params);
      expect(result.completed, isTrue, reason: '$step $params: ${result.message}');
      expect(api.created, [
        {'note': expected('')},
      ], reason: '$step $params');
      expect(api.parentWritten, isFalse, reason: '$step $params');
    }
  });

  testWidgets('with no related record and no creation allowed, the step fails with 101 and writes nothing',
      (tester) async {
    for (final (step, params, _) in _steps()) {
      final (state, api) = await _pump(tester, children: []);
      final (result, session) = await _run(tester, state, api, step, params, capture: true);
      expect(result.completed, isTrue, reason: 'error capture is on');
      expect(session.lastError, 101, reason: '$step $params');
      expect(api.created, isEmpty, reason: '$step $params');
      expect(api.updates, isEmpty, reason: '$step $params');
      expect(api.parent['note'], 'parent note');

      final (uncaptured, _) = await _run(tester, state, api, step, params);
      expect(uncaptured.completed, isFalse, reason: '$step $params');
      expect(uncaptured.message, contains('does not allow creating one'));
    }
  });

  testWidgets("the layout's own occurrence is the record in hand", (tester) async {
    final (state, api) = await _pump(tester);
    final (result, _) = await _run(
        tester, state, api, 'Set Field', {'field': 'note', 'field_table': 'Companies', 'value': '"own"', 'value_is_calculation': true});
    expect(result.completed, isTrue, reason: result.message);
    expect(api.parent['note'], 'own');
    expect(api.relatedUpdates, isEmpty);
  });

  testWidgets('a field of another occurrence is never one of the layout table', (tester) async {
    final (state, api) = await _pump(tester);
    expect(await state.actionRelookupFieldContents('Customers::note'), 102);
    final (insert, _) = await _run(tester, state, api, 'Insert File', {'field': 'note', 'field_table': 'Customers'}, capture: true);
    expect(insert.completed, isTrue);
    expect(api.updates, isEmpty);
    final (unknown, session) = await _run(
        tester, state, api, 'Set Field', {'field': 'Nowhere::note', 'value': '"x"', 'value_is_calculation': true},
        capture: true);
    expect(unknown.completed, isTrue);
    expect(session.lastError, 102);
    expect(api.updates, isEmpty);
  });
}
