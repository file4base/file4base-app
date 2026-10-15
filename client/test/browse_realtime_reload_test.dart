import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

final _table = TableModel(id: 't1', name: 'contacts', displayName: 'Contacts', columns: const [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'first_name', displayName: 'First name', fieldType: 'varchar'),
]);

class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');
  int listCalls = 0;
  List<Map<String, dynamic>> rows = [
    {'id': '1', 'first_name': 'Ana'},
    {'id': '2', 'first_name': 'Bea'},
  ];

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async {
    listCalls++;
    return List.of(rows);
  }

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> data) async => {'id': id, ...data};
}

final _layout = LayoutDefinitionModel(id: 'l', name: 'L', tableOccurrence: 'contacts', objects: const [
  LayoutObjectModel(id: 'f', type: 'field', x: 40, y: 40, width: 220, height: 30, fieldBinding: FieldBindingModel(fieldName: 'first_name')),
]);

Future<DataBrowserWidgetState> _pump(WidgetTester tester, _Api api) async {
  tester.view.physicalSize = const Size(1400, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final key = GlobalKey<DataBrowserWidgetState>();
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(body: DataBrowserWidget(key: key, table: _table, apiClient: api, mode: OperationalMode.browse, layout: _layout)),
  ));
  await tester.pumpAndSettle();
  return key.currentState!;
}

void main() {
  testWidgets('a remote change reloads and keeps the current record', (tester) async {
    final api = _Api();
    final state = await _pump(tester, api);
    state.goToRecord(1); // Bea
    await tester.pumpAndSettle();
    final callsBefore = api.listCalls;

    // Another session inserted a record before the current one.
    api.rows = [
      {'id': '0', 'first_name': 'Zoe'},
      {'id': '1', 'first_name': 'Ana'},
      {'id': '2', 'first_name': 'Bea'},
    ];
    state.reloadFromRemote();
    await tester.pumpAndSettle();

    expect(api.listCalls, greaterThan(callsBefore), reason: 'it refetched');
    expect(state.totalRecords, 3);
    expect(state.currentIndex, 2, reason: 'still on Bea, now at index 2');
  });

  testWidgets('a remote change does not interrupt editing; it reloads after the field is left', (tester) async {
    final api = _Api();
    final state = await _pump(tester, api);
    final callsBefore = api.listCalls;

    // Start editing the first record's field.
    await tester.tap(find.text('Ana'));
    await tester.pumpAndSettle();
    tester.testTextInput.enterText('Ana Maria');
    await tester.pump();

    // A remote change arrives mid-edit: it must be deferred.
    state.reloadFromRemote();
    await tester.pump();
    expect(api.listCalls, callsBefore, reason: 'no reload while the field is focused');

    // Leaving the field triggers the deferred reload.
    FocusManager.instance.primaryFocus?.unfocus();
    await tester.pumpAndSettle();
    expect(api.listCalls, greaterThan(callsBefore), reason: 'reloaded after blur');
  });
}
