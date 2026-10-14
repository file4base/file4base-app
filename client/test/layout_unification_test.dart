import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/widgets/file4base_status_sidebar.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/layout_preview_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('File4BaseStatusSidebar displays LAYOUT dropdown and responds to selection',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    const mockLayout1 = LayoutModel(
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
    const mockLayout2 = LayoutModel(
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

    const mockTable = TableModel(
      id: 'tbl-1',
      name: 'customers',
      displayName: 'Customers',
      columns: [
        ColumnModel(id: 'col-1', tableId: 'tbl-1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
        ColumnModel(id: 'col-2', tableId: 'tbl-1', name: 'name', displayName: 'Name', fieldType: 'varchar'),
      ],
    );

    LayoutModel? selectedLayout = mockLayout1;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(
            builder: (context, setState) {
              return File4BaseStatusSidebar(
                layouts: const [mockLayout1, mockLayout2],
                selectedLayout: selectedLayout,
                onLayoutSelected: (layout) {
                  setState(() => selectedLayout = layout);
                },
                tables: const [mockTable],
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

  testWidgets('DataBrowserWidget renders custom layout objects in Browse Mode and Find Mode',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    const mockTable = TableModel(
      id: 'tbl-1',
      name: 'customers',
      displayName: 'Customers',
      columns: [
        ColumnModel(id: 'col-1', tableId: 'tbl-1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
        ColumnModel(id: 'col-2', tableId: 'tbl-1', name: 'company_name', displayName: 'Company', fieldType: 'varchar'),
      ],
    );

    const mockLayout = LayoutDefinitionModel(
      id: 'layout-custom-1',
      name: 'Custom Invoice Form',
      tableOccurrence: 'customers',
      width: 800,
      theme: 'Classic',
      parts: [
        LayoutPartModel(id: 'part-1', type: 'body', height: 400),
      ],
      objects: [
        LayoutObjectModel(
          id: 'obj-lbl-1',
          type: 'label',
          x: 40,
          y: 40,
          width: 200,
          height: 30,
          text: 'CUSTOMER PROFILE HEADER',
        ),
        LayoutObjectModel(
          id: 'obj-fld-1',
          type: 'field',
          x: 40,
          y: 80,
          width: 300,
          height: 36,
          fieldBinding: FieldBindingModel(fieldName: 'company_name'),
        ),
        LayoutObjectModel(
          id: 'obj-btn-1',
          type: 'button',
          x: 40,
          y: 130,
          width: 140,
          height: 36,
          text: 'Process Order',
        ),
      ],
    );

    final mockApiClient = ApiClient(baseUrl: 'http://localhost:8080');

    // 1. Browse Mode: Verify layout canvas and objects are rendered even with 0 records
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: DataBrowserWidget(
            table: mockTable,
            apiClient: mockApiClient,
            mode: OperationalMode.browse,
            layout: mockLayout,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    // Verify label is visible
    expect(find.text('CUSTOMER PROFILE HEADER'), findsOneWidget);
    // Verify button is visible
    expect(find.text('Process Order'), findsOneWidget);
    // Verify field hint/container is present
    expect(find.byType(TextField), findsWidgets);

    // 2. Find Mode: Verify criteria fields are rendered
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: DataBrowserWidget(
            table: mockTable,
            apiClient: mockApiClient,
            mode: OperationalMode.find,
            layout: mockLayout,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('CUSTOMER PROFILE HEADER'), findsOneWidget);
    expect(find.text('Process Order'), findsOneWidget);
    expect(find.text('Perform Find'), findsOneWidget);
  });

  testWidgets('LayoutPreviewWidget renders custom layout objects on printed page',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    const mockTable = TableModel(
      id: 'tbl-1',
      name: 'customers',
      displayName: 'Customers',
      columns: [
        ColumnModel(id: 'col-1', tableId: 'tbl-1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
        ColumnModel(id: 'col-2', tableId: 'tbl-1', name: 'company_name', displayName: 'Company', fieldType: 'varchar'),
      ],
    );

    const mockLayout = LayoutDefinitionModel(
      id: 'layout-custom-1',
      name: 'Printable Invoice Layout',
      tableOccurrence: 'customers',
      width: 700,
      theme: 'Classic',
      parts: [
        LayoutPartModel(id: 'part-1', type: 'body', height: 400),
      ],
      objects: [
        LayoutObjectModel(
          id: 'obj-lbl-1',
          type: 'label',
          x: 20,
          y: 20,
          width: 250,
          height: 30,
          text: 'OFFICIAL TAX INVOICE',
        ),
        LayoutObjectModel(
          id: 'obj-fld-1',
          type: 'field',
          x: 20,
          y: 60,
          width: 280,
          height: 32,
          fieldBinding: FieldBindingModel(fieldName: 'company_name'),
        ),
      ],
    );

    final mockApiClient = ApiClient(baseUrl: 'http://localhost:8080');

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: LayoutPreviewWidget(
            table: mockTable,
            apiClient: mockApiClient,
            layout: mockLayout,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Preview Mode: Printable Invoice Layout'), findsOneWidget);
    expect(find.text('OFFICIAL TAX INVOICE'), findsOneWidget);
  });
}

