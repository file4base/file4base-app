import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/layout_object_visuals.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

final _table = TableModel(id: 't1', name: 'contacts', displayName: 'Contacts', columns: const [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'first_name', displayName: 'First name', fieldType: 'varchar'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'amount', displayName: 'Amount', fieldType: 'NUMBER'),
]);

class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [
        {'id': '1', 'first_name': 'Ana', 'amount': '1234.5'},
        {'id': '2', 'first_name': 'Bea', 'amount': '7'},
      ];

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> data) async => {'id': id, ...data};
}

LayoutDefinitionModel _layout({required bool formatted}) => LayoutDefinitionModel(
      id: 'l',
      name: 'L',
      tableOccurrence: 'contacts',
      objects: [
        LayoutObjectModel(
          id: 'f',
          type: 'field',
          x: 100,
          y: 100,
          width: 220,
          height: 30,
          fieldBinding: FieldBindingModel(fieldName: formatted ? 'amount' : 'first_name'),
          style: LayoutObjectStyle(numberFormat: formatted ? 'decimal' : null, useThousandsSeparator: formatted),
        ),
      ],
    );

Future<void> _pump(WidgetTester tester, LayoutDefinitionModel layout) async {
  tester.view.physicalSize = const Size(1400, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: DataBrowserWidget(table: _table, apiClient: _Api(), mode: OperationalMode.browse, layout: layout),
    ),
  ));
  await tester.pumpAndSettle();
}

/// Text of the editable field that currently has the focus.
String _focusedFieldText() {
  final ctx = FocusManager.instance.primaryFocus?.context;
  final editable = ctx?.findAncestorWidgetOfExactType<EditableText>() ??
      (ctx?.widget is EditableText ? ctx!.widget as EditableText : null);
  return editable?.controller.text ?? '<no editable field focused>';
}

void main() {
  test('only real formats count as display formatting', () {
    expect(layoutStyleHasDisplayFormat(const LayoutObjectStyle()), isFalse,
        reason: 'unset formats and text case "none" display the value as entered');
    expect(layoutStyleHasDisplayFormat(const LayoutObjectStyle(numberFormat: 'general')), isFalse);
    expect(layoutStyleHasDisplayFormat(const LayoutObjectStyle(numberFormat: 'currency')), isTrue);
    expect(layoutStyleHasDisplayFormat(const LayoutObjectStyle(textCase: 'upper')), isTrue);
    expect(layoutStyleHasDisplayFormat(const LayoutObjectStyle(dateFormat: 'as_entered')), isFalse);
  });

  testWidgets('one click on a field in Browse mode is enough to start typing', (tester) async {
    await _pump(tester, _layout(formatted: false));
    await tester.tapAt(tester.getCenter(find.text('Ana')));
    await tester.pumpAndSettle();
    tester.testTextInput.enterText('Ana María');
    await tester.pump();
    expect(_focusedFieldText(), 'Ana María');
  });

  testWidgets('a formatted field shows its format and takes typing after one click', (tester) async {
    await _pump(tester, _layout(formatted: true));
    expect(find.text('1,234.50'), findsOneWidget, reason: 'formatted value while not being edited');

    await tester.tapAt(tester.getCenter(find.text('1,234.50')));
    await tester.pumpAndSettle();
    tester.testTextInput.enterText('99');
    await tester.pump();
    expect(_focusedFieldText(), '99');
  });

  testWidgets('one click on another record in Table view selects it and starts editing that field', (tester) async {
    await _pump(tester, _layout(formatted: false));
    final state = tester.state<DataBrowserWidgetState>(find.byType(DataBrowserWidget));
    await tester.tap(find.text('Table'));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Bea'));
    await tester.pumpAndSettle();
    expect(state.currentIndex, 1);
    tester.testTextInput.enterText('Beatriz');
    await tester.pump();
    expect(_focusedFieldText(), 'Beatriz');
  });
}
