import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/widgets/file4base_status_sidebar.dart';
import 'package:file4base_client/main.dart';

void main() {
  testWidgets('File4BaseStatusSidebar displays LAYOUT dropdown and responds to selection',
      (WidgetTester tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    final mockLayout1 = LayoutModel(
      id: 'layout-1',
      name: 'Customers Form',
      tableOccurrenceId: 'to-1',
      definition: {
        'name': 'Customers Form',
        'table_occurrence': 'customers',
        'parts': [],
        'objects': [],
      },
    );
    final mockLayout2 = LayoutModel(
      id: 'layout-2',
      name: 'Customers List',
      tableOccurrenceId: 'to-1',
      definition: {
        'name': 'Customers List',
        'table_occurrence': 'customers',
        'parts': [],
        'objects': [],
      },
    );

    final mockTable = TableModel(
      id: 'tbl-1',
      name: 'customers',
      displayName: 'Customers',
      columns: [
        const ColumnModel(id: 'col-1', tableId: 'tbl-1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
        const ColumnModel(id: 'col-2', tableId: 'tbl-1', name: 'name', displayName: 'Name', fieldType: 'varchar'),
      ],
    );

    LayoutModel? selectedLayout = mockLayout1;
    bool manageLayoutsCalled = false;
    bool newLayoutCalled = false;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(
            builder: (context, setState) {
              return File4BaseStatusSidebar(
                layouts: [mockLayout1, mockLayout2],
                selectedLayout: selectedLayout,
                onLayoutSelected: (layout) {
                  setState(() => selectedLayout = layout);
                },
                onManageLayouts: () => manageLayoutsCalled = true,
                onNewLayout: () => newLayoutCalled = true,
                tables: [mockTable],
                selectedTable: mockTable,
                onTableSelected: (_) {},
                mode: OperationalMode.browse,
                currentRecordIndex: 0,
                totalRecords: 10,
                onPreviousRecord: () {},
                onNextRecord: () {},
                onGoToRecord: (_) {},
                onManageDatabase: () {},
              );
            },
          ),
        ),
      ),
    );
    await tester.pump();

    // Verify 'LAYOUT' section header is present (not TABLE)
    expect(find.text('LAYOUT'), findsOneWidget);
    expect(find.text('Customers Form'), findsOneWidget);

    // Tap dropdown to open it
    await tester.tap(find.text('Customers Form'));
    await tester.pumpAndSettle();

    // Verify layouts and actions appear
    expect(find.text('Customers List'), findsOneWidget);
    expect(find.text('New Layout...'), findsOneWidget);
    expect(find.text('Manage Layouts...'), findsOneWidget);

    // Select Customers List
    await tester.tap(find.text('Customers List'));
    await tester.pumpAndSettle();

    expect(selectedLayout?.id, equals('layout-2'));
  });
}
