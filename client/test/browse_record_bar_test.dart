import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/main.dart' show OperationalMode;

final _table = TableModel(id: 't1', name: 'contacts', displayName: 'Contacts', columns: const [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'first_name', displayName: 'First name', fieldType: 'varchar'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'city', displayName: 'City', fieldType: 'varchar'),
]);

class _RowsApi extends ApiClient {
  _RowsApi() : super(baseUrl: 'http://localhost:1');

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async => [
        {'id': '1', 'first_name': 'Carla', 'city': 'Bilbao'},
        {'id': '2', 'first_name': 'Ana', 'city': 'Madrid'},
        {'id': '3', 'first_name': 'Bea', 'city': 'Cádiz'},
      ];
}

void main() {
  testWidgets('Browse record bar shows position, found set and switches to the table view', (tester) async {
    tester.view.physicalSize = const Size(1400, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    final key = GlobalKey<DataBrowserWidgetState>();
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: DataBrowserWidget(key: key, table: _table, apiClient: _RowsApi(), mode: OperationalMode.browse),
      ),
    ));
    await tester.pumpAndSettle();

    expect(find.text('Record '), findsOneWidget);
    expect(find.text(' of 3'), findsOneWidget);
    expect(find.text('3 records · Unsorted'), findsOneWidget);
    for (final label in ['New', 'Duplicate', 'Delete', 'Find', 'Sort', 'Show All', 'Form', 'List', 'Table']) {
      expect(find.text(label), findsWidgets, reason: label);
    }

    await tester.tap(find.byTooltip('Next record (Ctrl+↓)').evaluate().isNotEmpty
        ? find.byTooltip('Next record (Ctrl+↓)')
        : find.byTooltip('Next record (⌘↓)'));
    await tester.pumpAndSettle();
    expect(key.currentState!.currentIndex, 1);

    await tester.tap(find.text('Table'));
    await tester.pumpAndSettle();
    expect(find.text('Madrid'), findsOneWidget);
    expect(find.text('Cádiz'), findsOneWidget);

    // Clicking a column header sorts the found set and keeps the current record.
    await tester.tap(find.text('First name'));
    await tester.pumpAndSettle();
    expect(find.text('3 records · Sorted by First name ↑'), findsOneWidget);
    final names = tester
        .widgetList<Text>(find.textContaining(RegExp(r'^(Ana|Bea|Carla)$')))
        .map((t) => t.data)
        .toList();
    expect(names, ['Ana', 'Bea', 'Carla']);
    expect(key.currentState!.currentIndex, 0, reason: 'Ana (the current record) is now first');
  });
}
