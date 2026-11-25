// Replace Field Contents in the data browser: it covers the whole found set,
// not only the rows read so far, stops at a write the server rejects, and
// never writes a related field into the layout's own field of that name.
// A dialog closed without a button is never its first button.
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _table = TableModel(id: 't1', name: 'items', displayName: 'Items', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'code', displayName: 'Code', fieldType: 'varchar'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'mark', displayName: 'Mark', fieldType: 'varchar'),
]);

/// 150 records: more than the first page the window reads (100).
class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');
  final Map<String, Map<String, dynamic>> stored = {
    for (var i = 1; i <= 150; i++) '$i': {'id': '$i', 'code': 'C$i', 'mark': i.isEven ? 'even' : 'odd'},
  };
  final List<String> written = [];
  String? rejectId;

  List<Map<String, dynamic>> _page(Iterable<Map<String, dynamic>> rows, int limit, int offset) =>
      rows.skip(offset).take(limit).map(Map<String, dynamic>.of).toList();

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      _page(stored.values, limit, offset);

  @override
  Future<List<Map<String, dynamic>>> executeFind(String table, List<Map<String, dynamic>> requests,
          {int limit = 500, int offset = 0, String? sortBy}) async =>
      _page(stored.values.where((r) => r['mark'] == 'even'), limit < 50 ? limit : 50, offset);

  @override
  Future<List<Map<String, dynamic>>> executeFindAll(String table, List<Map<String, dynamic>> requests) async =>
      stored.values.where((r) => r['mark'] == 'even').map(Map<String, dynamic>.of).toList();

  @override
  Future<List<TableOccurrenceModel>> listOccurrences() async => const [
        TableOccurrenceModel(id: 'to1', baseTableId: 't1', name: 'Items', xPos: 0, yPos: 0),
        TableOccurrenceModel(id: 'to2', baseTableId: 't2', name: 'Lines', xPos: 0, yPos: 0),
      ];

  @override
  Future<List<RelationshipModel>> listRelationships() async => const [];

  @override
  Future<List<TableModel>> listTables() async => [_table];

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> data,
      {bool script = false}) async {
    if (id == rejectId) {
      throw ApiException(statusCode: 422, title: 'Validation Failed', detail: 'code must be unique');
    }
    written.add(id);
    stored[id]!.addAll(data);
    return Map<String, dynamic>.of(stored[id]!);
  }
}

const _layout = LayoutDefinitionModel(id: 'l', name: 'Items', tableOccurrence: 'to1', objects: [
  LayoutObjectModel(id: 'f1', type: 'field', x: 40, y: 40, width: 220, height: 30, fieldBinding: FieldBindingModel(fieldName: 'code')),
  LayoutObjectModel(id: 'f2', type: 'field', x: 40, y: 90, width: 220, height: 30, fieldBinding: FieldBindingModel(fieldName: 'mark')),
]);

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

void main() {
  testWidgets('a calculation is replaced in every record, beyond the first page read', (tester) async {
    final api = _Api();
    final state = await _pump(tester, api);
    expect(state.totalRecords, 100, reason: 'the window reads the first page');
    await state.goToRecord(4);
    final session = ScriptSessionState();
    final res = await LayoutActionRunner(apiClient: api, host: state).runStep('Replace Field Contents', {
      'field': 'mark',
      'replace_type': 'calculation',
      'value': 'code & "-x"',
    }, depth: 0, callContext: ScriptCallContext(session: session));
    await tester.pumpAndSettle();
    expect(res.completed, isTrue, reason: res.message);
    expect(session.lastError, 0);
    expect(api.written, hasLength(150));
    expect(api.stored['1']!['mark'], 'C1-x');
    expect(api.stored['150']!['mark'], 'C150-x', reason: 'a record not read before is replaced');
    expect(state.totalRecords, 150);
    expect(state.currentIndex, 4, reason: 'the same record is current afterwards');
  });

  testWidgets('a found set is replaced as a whole, without the records omitted from it', (tester) async {
    final api = _Api();
    final state = await _pump(tester, api);
    await state.actionEnterFindMode();
    await state.actionPerformFind();
    await tester.pumpAndSettle();
    expect(state.isFoundSet, isTrue);
    expect(state.totalRecords, 50, reason: 'the first 50 of the 75 even records');
    await state.actionOmitRecord();
    await tester.pumpAndSettle();
    final res = await LayoutActionRunner(apiClient: api, host: state).runStep('Replace Field Contents', {
      'field': 'mark',
      'replace_type': 'serial_numbers',
      'serial_initial': '1',
    }, depth: 0);
    await tester.pumpAndSettle();
    expect(res.completed, isTrue, reason: res.message);
    expect(api.written, hasLength(74));
    expect(api.written, isNot(contains('2')), reason: 'the omitted record stays out');
    expect(api.stored['2']!['mark'], 'even');
    expect(api.stored['150']!['mark'], '74');
    expect(api.stored['1']!['mark'], 'odd', reason: 'outside the found set');
  });

  testWidgets('a write the server rejects stops the replace with its error', (tester) async {
    final api = _Api()..rejectId = '3';
    final state = await _pump(tester, api);
    final session = ScriptSessionState()..errorCapture = true;
    final res = await LayoutActionRunner(apiClient: api, host: state).runStep('Replace Field Contents', {
      'field': 'code',
      'value': '"same"',
    }, depth: 0, callContext: ScriptCallContext(session: session));
    await tester.pumpAndSettle();
    expect(res.completed, isTrue, reason: 'error capture is on');
    expect(session.lastError, 504, reason: 'not unique');
    expect(api.written, ['1', '2'], reason: 'nothing after the rejected record');
    expect(api.stored['4']!['code'], 'C4');
  });

  testWidgets('a related field is refused, and the local field of that name is left alone', (tester) async {
    final api = _Api();
    final state = await _pump(tester, api);
    final session = ScriptSessionState();
    final res = await LayoutActionRunner(apiClient: api, host: state).runStep('Replace Field Contents', {
      'field': 'mark',
      'field_table': 'Lines',
      'value': '"new"',
    }, depth: 0, callContext: ScriptCallContext(session: session));
    await tester.pumpAndSettle();
    expect(res.completed, isFalse);
    expect(res.message, contains('Lines::mark'));
    expect(session.lastError, 102);
    expect(api.written, isEmpty);
  });

  testWidgets('a dialog closed without a button answers no button', (tester) async {
    final api = _Api();
    final state = await _pump(tester, api);
    final pending = state.actionShowDialogEx(
      title: 'Replace Field Contents',
      message: 'Replace?',
      buttons: const [
        {'label': 'Replace', 'commit': true},
        {'label': 'Cancel', 'commit': false},
      ],
    );
    await tester.pumpAndSettle();
    expect(find.text('Replace'), findsOneWidget);
    await tester.tapAt(const Offset(5, 5));
    await tester.pumpAndSettle();
    expect((await pending).buttonIndex, ScriptDialogResult.dismissed);
  });
}
