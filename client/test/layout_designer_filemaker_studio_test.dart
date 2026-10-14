import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/layout_designer_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('LayoutDesignerWidget renders full FileMaker-style Layout Studio with tools, parts, and inspector',
      (tester) async {
    tester.view.physicalSize = const Size(1600, 1000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    const mockTable = TableModel(
      id: 'tbl-1',
      name: 'customers',
      displayName: 'Customers',
      columns: [
        ColumnModel(id: 'col-1', tableId: 'tbl-1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
        ColumnModel(id: 'col-2', tableId: 'tbl-1', name: 'company_name', displayName: 'Company', fieldType: 'varchar'),
        ColumnModel(id: 'col-3', tableId: 'tbl-1', name: 'balance', displayName: 'Balance', fieldType: 'numeric'),
      ],
    );

    const mockLayoutDef = LayoutDefinitionModel(
      id: 'layout-1',
      name: 'Customers Form',
      tableOccurrence: 'customers',
      parts: [
        LayoutPartModel(id: 'header_part', type: 'header', height: 60),
        LayoutPartModel(id: 'body_part', type: 'body', height: 400),
        LayoutPartModel(id: 'footer_part', type: 'footer', height: 40),
      ],
      objects: [
        LayoutObjectModel(
          id: 'title_lbl',
          type: 'label',
          x: 24,
          y: 16,
          width: 200,
          height: 28,
          text: 'Customer Details',
        ),
        LayoutObjectModel(
          id: 'field_company',
          type: 'field',
          x: 40,
          y: 80,
          width: 220,
          height: 32,
          fieldBinding: FieldBindingModel(fieldName: 'company_name'),
        ),
      ],
    );

    final mockApiClient = ApiClient(baseUrl: 'http://localhost:8080');
    var exitLayoutCalled = false;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: LayoutDesignerWidget(
            table: mockTable,
            tables: const [mockTable],
            apiClient: mockApiClient,
            initialLayout: mockLayoutDef,
            layouts: [
              LayoutModel(
                id: 'layout-1',
                name: 'Customers Form',
                tableOccurrenceId: 'tbl-1',
                definition: mockLayoutDef.toJson(),
              ),
            ],
            onSaved: () {},
            onExitLayout: () {
              exitLayoutCalled = true;
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    // 1. Verify Top Bar 1: New Layout / Report, Layout Tools label, Manage, Show/Hide Panes
    expect(find.text('New Layout / Report'), findsOneWidget);
    expect(find.text('Layout Tools'), findsOneWidget);
    expect(find.text('Manage'), findsOneWidget);

    // 2. Verify Top Bar 2: Layout Context Bar with Table Occurrence, Theme, Snap 8px, and Exit Layout
    expect(find.text('Layout:'), findsOneWidget);
    expect(find.text('Customers Form'), findsWidgets);
    expect(find.text('Table: Customers'), findsOneWidget);
    expect(find.text('Theme:'), findsOneWidget);
    expect(find.text('Enlightened'), findsOneWidget);
    expect(find.text('Snap 8px'), findsOneWidget);
    expect(find.text('Exit Layout'), findsOneWidget);

    // 3. Verify Left Pane: Fields tab, Search input, columns list, + New Field button, Drag Preferences
    expect(find.text('Fields'), findsOneWidget);
    expect(find.text('Objects'), findsOneWidget);
    expect(find.text('New Field'), findsOneWidget);
    expect(find.text('Drag Preferences'), findsOneWidget);
    expect(find.text('Company'), findsOneWidget);
    expect(find.text('Balance'), findsOneWidget);

    // 4. Verify Left Margin Part Labels
    expect(find.text('Header'), findsWidgets);
    expect(find.text('Body'), findsWidgets);
    expect(find.text('Footer'), findsWidgets);

    // 5. Verify Objects on Canvas
    expect(find.text('Customer Details'), findsOneWidget);
    expect(find.text(':: company_name'), findsOneWidget);

    // 6. Test Object Selection & Inspector Display
    await tester.tap(find.text('Customer Details'));
    await tester.pumpAndSettle();

    // Inspector should show LABEL badge and Position & Size fields
    expect(find.text('LABEL'), findsOneWidget);
    expect(find.text('POSITION'), findsOneWidget);
    expect(find.text('AUTOSIZING (ANCHORS)'), findsOneWidget);
    expect(find.text('ARRANGE & ALIGN'), findsOneWidget);

    // 7. Switch Inspector Tab to Styles (Palette icon)
    await tester.tap(find.byIcon(Icons.palette_outlined).first);
    await tester.pumpAndSettle();
    expect(find.text('FILL COLOR'), findsOneWidget);
    expect(find.text('LINE (BORDER) COLOR'), findsOneWidget);
    expect(find.text('LINE WIDTH & CORNERS'), findsOneWidget);

    // 8. Switch Inspector Tab to Typography (Text fields icon)
    await tester.tap(find.byIcon(Icons.text_fields).first);
    await tester.pumpAndSettle();
    expect(find.text('TEXT CONTENT'), findsOneWidget);
    expect(find.text('TEXT COLOR'), findsOneWidget);
    expect(find.text('FONT & SIZE'), findsOneWidget);
    expect(find.text('ALIGNMENT'), findsOneWidget);

    // 9. Test Exit Layout Button
    await tester.tap(find.text('Exit Layout'));
    await tester.pumpAndSettle();
    expect(exitLayoutCalled, isTrue);
  });
}
