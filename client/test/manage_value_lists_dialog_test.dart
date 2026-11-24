// A value list taken from a field gets FileMaker's options in Manage Value
// Lists: a second field, the sort, and including only related values.

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/schema_manager/manage_value_lists_dialog.dart';
import 'package:file4base_client/main.dart' show apiClientProvider;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

const _customers = TableModel(id: 't1', name: 'customers', displayName: 'Customers', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
]);

const _codes = TableModel(id: 't2', name: 'codes', displayName: 'Codes', columns: [
  ColumnModel(id: 'c1', tableId: 't2', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'c-code', tableId: 't2', name: 'code', displayName: 'Code', fieldType: 'TEXT'),
  ColumnModel(id: 'c-label', tableId: 't2', name: 'label', displayName: 'Label', fieldType: 'TEXT'),
]);

class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');
  final List<ValueListModel> created = [];

  @override
  Future<List<ValueListModel>> listValueLists() async => created;

  @override
  Future<List<TableModel>> listTables() async => const [_customers, _codes];

  @override
  Future<List<TableOccurrenceModel>> listOccurrences() async => const [
        TableOccurrenceModel(id: 'o-customers', baseTableId: 't1', name: 'Customers', xPos: 0, yPos: 0),
        TableOccurrenceModel(id: 'o-codes', baseTableId: 't2', name: 'Customer Codes', xPos: 0, yPos: 0),
      ];

  @override
  Future<ValueListModel> createValueList(ValueListModel list) async {
    created.add(list);
    return list;
  }
}

Future<void> _choose(WidgetTester tester, Key dropdown, String item) async {
  await tester.ensureVisible(find.byKey(dropdown));
  await tester.tap(find.byKey(dropdown));
  await tester.pumpAndSettle();
  await tester.tap(find.text(item).last);
  await tester.pumpAndSettle();
}

Future<void> _check(WidgetTester tester, Key box) async {
  await tester.ensureVisible(find.byKey(box));
  await tester.tap(find.byKey(box));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('a list from a field saves its second field, sort and related values', (tester) async {
    tester.view.physicalSize = const Size(1600, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    final api = _Api();
    await tester.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(api)],
      child: const MaterialApp(home: Scaffold(body: ManageValueListsDialog())),
    ));
    await tester.pumpAndSettle();

    await tester.tap(find.text('New Value List...'));
    await tester.pumpAndSettle();
    await tester.enterText(find.widgetWithText(TextField, 'Value List Name'), 'Customer Codes');
    await tester.tap(find.text('From a field'));
    await tester.pumpAndSettle();

    await _choose(tester, const ValueKey('value-list-source-table'), 'Codes');
    await _choose(tester, const ValueKey('value-list-source-column-t2'), 'Code');
    await _check(tester, const ValueKey('value-list-use-second'));
    await _choose(tester, const ValueKey('value-list-second-column-t2'), 'Label');
    await _check(tester, const ValueKey('value-list-show-second-only'));
    await tester.tap(find.descendant(
        of: find.byKey(const ValueKey('value-list-sort-by')), matching: find.text('Second field')));
    await tester.pumpAndSettle();
    await _check(tester, const ValueKey('value-list-related'));
    await _choose(tester, const ValueKey('value-list-related-occurrence'), 'Customers');

    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();

    final saved = api.created.single;
    expect(saved.name, 'Customer Codes');
    expect(saved.sourceColumnId, 'c-code');
    expect(saved.secondColumnId, 'c-label');
    expect(saved.showSecondOnly, isTrue);
    expect(saved.sortBy, ValueListModel.sortSecond);
    expect(saved.relatedOccurrenceId, 'o-customers');
    expect(saved.sourceOccurrenceId, 'o-codes',
        reason: 'the only occurrence of the list table is chosen for the user');
  });
}
