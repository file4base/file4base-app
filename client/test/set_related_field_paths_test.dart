// A related field a script step writes is reached from the layout's own
// occurrence through the graph: another occurrence of the layout's table (a
// self-join) is a related one, a relationship of another occurrence of that
// table is not a path, the active portal row is the one clicked on the
// record whose portal it is, and only while it is still related.

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _employees = TableModel(id: 't-emp', name: 'employees', displayName: 'Employees', columns: [
  ColumnModel(id: 'e0', tableId: 't-emp', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'e1', tableId: 't-emp', name: 'manager_id', displayName: 'Manager', fieldType: 'TEXT'),
  ColumnModel(id: 'e2', tableId: 't-emp', name: 'dept_id', displayName: 'Department', fieldType: 'TEXT'),
  ColumnModel(id: 'e3', tableId: 't-emp', name: 'note', displayName: 'Note', fieldType: 'TEXT'),
  ColumnModel(id: 'e4', tableId: 't-emp', name: 'hired', displayName: 'Hired', fieldType: 'DATE'),
]);

const _departments = TableModel(id: 't-dep', name: 'departments', displayName: 'Departments', columns: [
  ColumnModel(id: 'd0', tableId: 't-dep', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'd1', tableId: 't-dep', name: 'name', displayName: 'Name', fieldType: 'TEXT'),
]);

// Managers is the layout's occurrence; Employees and Staff are two more
// occurrences of the same table. Only Managers ⇢ Employees starts from the
// layout; Staff ⇢ Departments does not.
const _occurrences = [
  TableOccurrenceModel(id: 'occ-managers', baseTableId: 't-emp', name: 'Managers', xPos: 0, yPos: 0),
  TableOccurrenceModel(id: 'occ-employees', baseTableId: 't-emp', name: 'Employees', xPos: 0, yPos: 0),
  TableOccurrenceModel(id: 'occ-staff', baseTableId: 't-emp', name: 'Staff', xPos: 0, yPos: 0),
  TableOccurrenceModel(id: 'occ-departments', baseTableId: 't-dep', name: 'Departments', xPos: 0, yPos: 0),
];

const _relationships = [
  RelationshipModel(
      id: 'rel-team',
      name: 'team',
      leftOccurrenceId: 'occ-managers',
      leftColumnId: 'e0',
      rightOccurrenceId: 'occ-employees',
      rightColumnId: 'e1'),
  RelationshipModel(
      id: 'rel-dept',
      name: 'dept',
      leftOccurrenceId: 'occ-staff',
      leftColumnId: 'e2',
      rightOccurrenceId: 'occ-departments',
      rightColumnId: 'd0'),
];

class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');

  final Map<String, Map<String, dynamic>> managers = {
    'm1': {'id': 'm1', 'manager_id': '', 'note': 'manager one', 'hired': '2000-01-01'},
    'm2': {'id': 'm2', 'manager_id': '', 'note': 'manager two', 'hired': '2000-01-01'},
  };

  /// The team of each manager.
  final Map<String, List<Map<String, dynamic>>> teams = {
    'm1': [
      {'id': 'e1', 'manager_id': 'm1', 'note': 'one', 'hired': '2010-05-05'},
      {'id': 'e2', 'manager_id': 'm1', 'note': 'two', 'hired': '2011-05-05'},
    ],
    'm2': [
      {'id': 'e3', 'manager_id': 'm2', 'note': 'three', 'hired': '2012-05-05'},
      {'id': 'e4', 'manager_id': 'm2', 'note': 'four', 'hired': '2013-05-05'},
    ],
  };

  final List<String> updates = [];
  final List<(String, int)> relatedReads = [];

  @override
  Future<List<RelationshipModel>> listRelationships() async => _relationships;

  @override
  Future<List<TableOccurrenceModel>> listOccurrences() async => _occurrences;

  @override
  Future<List<TableModel>> listTables() async => [_employees, _departments];

  @override
  Future<List<ValueListModel>> listValueLists() async => const [];

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [for (final m in managers.values) Map<String, dynamic>.of(m)];

  @override
  Future<Map<String, dynamic>> getRow(String table, String id) async => Map<String, dynamic>.of(managers[id]!);

  List<Map<String, dynamic>> _related(String id, String relationshipId) =>
      relationshipId == 'rel-team' ? [for (final r in teams[id] ?? const <Map<String, dynamic>>[]) Map.of(r)] : [];

  @override
  Future<List<Map<String, dynamic>>> listRelatedRows(String table, String id,
      {required String relationshipId,
      String? occurrence,
      String? side,
      List<String>? sort,
      int limit = 100,
      int offset = 0}) async {
    relatedReads.add((relationshipId, limit));
    return _related(id, relationshipId).take(limit).toList();
  }

  @override
  Future<Map<String, List<Map<String, dynamic>>>> listRelatedRowsBatch(String table, List<String> ids,
          {required String relationshipId, String? occurrence, String? side, List<String>? sort, int limit = 100}) async =>
      {for (final id in ids) id: _related(id, relationshipId)};

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> values,
      {bool script = false, bool overrideValidation = false}) async {
    updates.add('$table/$id ${values.entries.map((e) => '${e.key}=${e.value}').join(',')}');
    final manager = managers[id];
    if (manager != null) {
      manager.addAll(values);
      return Map<String, dynamic>.of(manager);
    }
    for (final team in teams.values) {
      for (final r in team) {
        if (r['id'] == id) r.addAll(values);
      }
    }
    return {'id': id, ...values};
  }
}

const _layout = LayoutDefinitionModel(
  id: 'layout-managers',
  name: 'Managers',
  tableOccurrence: 'Managers',
  defaultView: 'list',
  parts: [
    LayoutPartModel(id: 'h', type: 'header', height: 40),
    LayoutPartModel(id: 'b', type: 'body', height: 300),
    LayoutPartModel(id: 'f', type: 'footer', height: 40),
  ],
  objects: [
    LayoutObjectModel(
        id: 'fld_note', type: 'field', x: 40, y: 20, width: 200, height: 30, fieldBinding: FieldBindingModel(fieldName: 'note')),
    LayoutObjectModel(
      id: 'portal_team',
      type: 'portal',
      x: 40,
      y: 60,
      width: 400,
      height: 100,
      portalConfig: {'relationship_id': 'rel-team', 'occurrence': 'Employees'},
    ),
    LayoutObjectModel(
        id: 'fld_team_note',
        type: 'field',
        x: 50,
        y: 65,
        width: 200,
        height: 24,
        fieldBinding: FieldBindingModel(fieldName: 'note')),
  ],
);

Future<(DataBrowserWidgetState, _Api)> _pump(WidgetTester tester) async {
  tester.view.physicalSize = const Size(1600, 1400);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final api = _Api();
  final key = GlobalKey<DataBrowserWidgetState>();
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: DataBrowserWidget(key: key, table: _employees, apiClient: api, mode: OperationalMode.browse, layout: _layout),
    ),
  ));
  await tester.pumpAndSettle();
  return (key.currentState!, api);
}

Future<(LayoutActionResult, ScriptSessionState)> _run(
    WidgetTester tester, DataBrowserWidgetState state, _Api api, String step, Map<String, dynamic> params) async {
  final session = ScriptSessionState()..errorCapture = true;
  final result = await LayoutActionRunner(apiClient: api, host: state)
      .runStep(step, params, depth: 0, callContext: ScriptCallContext(session: session));
  await tester.pumpAndSettle();
  return (result, session);
}

Map<String, dynamic> _setNote(String occurrence, String value) =>
    {'field': 'note', 'field_table': occurrence, 'value': '"$value"', 'value_is_calculation': true};

void main() {
  testWidgets('another occurrence of the layout table is related, not the record in hand (self-join)', (tester) async {
    final (state, api) = await _pump(tester);
    var (_, session) = await _run(tester, state, api, 'Set Field', _setNote('Employees', 'first'));
    expect(session.lastError, 0);
    expect(api.updates, ['employees/e1 note=first'], reason: "the first of the manager's team, not the manager");
    expect(api.managers['m1']!['note'], 'manager one');

    await tester.tap(find.byKey(const ValueKey('portal-cell-e2-note')));
    await tester.pumpAndSettle();
    (_, session) = await _run(tester, state, api, 'Set Field', _setNote('Employees', 'active'));
    expect(session.lastError, 0);
    expect(api.updates.last, 'employees/e2 note=active', reason: 'the active portal row first');

    (_, session) = await _run(tester, state, api, 'Set Field', _setNote('Managers', 'own'));
    expect(api.updates.last, 'employees/m1 note=own', reason: "the layout's own occurrence is the record in hand");
  });

  testWidgets("a relationship that does not start from the layout's occurrence is no path: 103", (tester) async {
    final (state, api) = await _pump(tester);
    final (_, session) = await _run(
        tester, state, api, 'Set Field', {'field': 'Departments::name', 'value': '"x"', 'value_is_calculation': true});
    expect(session.lastError, 103);
    expect(api.updates, isEmpty);
    expect(api.relatedReads.where((r) => r.$1 == 'rel-dept'), isEmpty,
        reason: 'Staff ⇢ Departments joins another occurrence of the same table');
  });

  testWidgets('an active row no longer related is dropped for the first related record', (tester) async {
    final (state, api) = await _pump(tester);
    await tester.tap(find.byKey(const ValueKey('portal-cell-e2-note')));
    await tester.pumpAndSettle();
    // Another write moved the employee to another manager since.
    api.teams['m1']!.removeWhere((r) => r['id'] == 'e2');
    final (_, session) = await _run(tester, state, api, 'Set Field', _setNote('Employees', 'after'));
    expect(session.lastError, 0);
    expect(api.updates, ['employees/e1 note=after']);
  });

  testWidgets('in a list, the portal row clicked is active on the record of its list row', (tester) async {
    final (state, api) = await _pump(tester);
    await tester.tap(find.text('List'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('portal-cell-e4-note')));
    await tester.pumpAndSettle();
    await state.goToRecordNamed('2');
    await tester.pumpAndSettle();
    final (_, session) = await _run(tester, state, api, 'Set Field', _setNote('Employees', 'listed'));
    expect(session.lastError, 0);
    expect(api.updates, ['employees/e4 note=listed']);
  });

  testWidgets('Insert Current Date replaces the date of a related date field', (tester) async {
    final (state, api) = await _pump(tester);
    final (_, session) =
        await _run(tester, state, api, 'Insert Current Date', {'field': 'hired', 'field_table': 'Employees', 'select': false});
    expect(session.lastError, 0);
    final now = DateTime.now();
    final today = '${now.year.toString().padLeft(4, '0')}-${now.month.toString().padLeft(2, '0')}-'
        '${now.day.toString().padLeft(2, '0')}';
    expect(api.updates, ['employees/e1 hired=$today'], reason: 'a date field holds one date');
  });
}
