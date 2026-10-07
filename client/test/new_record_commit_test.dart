import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/main.dart' show OperationalMode;

final _table = TableModel(id: 't1', name: 'contacts', displayName: 'Contacts', columns: const [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'first_name', displayName: 'First name', fieldType: 'TEXT'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'city', displayName: 'City', fieldType: 'TEXT'),
]);

/// Records inserts; rejects them like a "not empty" rule on first_name.
class _ValidatingApi extends ApiClient {
  _ValidatingApi() : super(baseUrl: 'http://localhost:1');
  final rows = <Map<String, dynamic>>[
    {'id': '1', 'first_name': 'Carla', 'city': 'Bilbao'},
  ];
  final inserts = <Map<String, dynamic>>[];

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [for (final r in rows) Map<String, dynamic>.from(r)];

  @override
  Future<Map<String, dynamic>> insertRow(String table, Map<String, dynamic> record) async {
    inserts.add(Map.of(record));
    if ((record['first_name']?.toString() ?? '').isEmpty) {
      throw Exception('first_name requires a value');
    }
    final created = {'id': 'new-${inserts.length}', ...record};
    rows.add(created);
    return created;
  }
}

/// The form's n-th field (the record bar's text fields come first).
Finder _field(WidgetTester tester, int n) {
  final all = find.byType(TextField);
  final count = tester.widgetList(all).length;
  return all.at(count - _table.columns.length + 1 + n);
}

void main() {
  testWidgets('a new record is created on the server when committed, and validation errors keep it open (#15)',
      (tester) async {
    tester.view.physicalSize = const Size(1400, 1000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    final api = _ValidatingApi();
    final key = GlobalKey<DataBrowserWidgetState>();
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(body: DataBrowserWidget(key: key, table: _table, apiClient: api, mode: OperationalMode.browse)),
    ));
    await tester.pumpAndSettle();
    final state = key.currentState!;

    state.createNewRecord();
    await tester.pumpAndSettle();
    expect(api.inserts, isEmpty, reason: 'New Record does not send an empty record');
    expect(state.totalRecords, 2);
    expect(state.currentIndex, 1);

    // Committing with first_name empty: rejected, the record stays open
    await tester.enterText(_field(tester, 1), 'Madrid');
    await state.actionCommitRecord();
    await tester.pumpAndSettle();
    expect(api.inserts.single, {'city': 'Madrid'}, reason: 'empty fields are not sent, so auto-enter values apply');
    expect(find.textContaining('first_name requires a value'), findsOneWidget);
    expect(state.currentIndex, 1, reason: 'the user stays on the new record');
    state.previousRecord();
    await tester.pumpAndSettle();
    expect(state.currentIndex, 1, reason: 'cannot leave a new record the server rejected');
    expect(api.inserts, hasLength(2), reason: 'leaving tries to commit it first');

    // Filling the required field commits it
    await tester.enterText(_field(tester, 0), 'Ana');
    await state.actionCommitRecord();
    await tester.pumpAndSettle();
    expect(api.inserts.last, {'first_name': 'Ana', 'city': 'Madrid'});
    expect(api.rows, hasLength(2));

    // Reverting a new record that was never committed discards it
    state.createNewRecord();
    await tester.pumpAndSettle();
    expect(state.totalRecords, 3);
    await state.actionRevertRecord();
    await tester.pumpAndSettle();
    expect(state.totalRecords, 2);
    expect(api.inserts, hasLength(3), reason: 'the discarded record was never sent');
  });
}
