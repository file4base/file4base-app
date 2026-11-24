// #31 — a field bound to a value list is filled from a control instead of
// being typed into. These render the real Browse mode and look at what came
// out.

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _customerTypes = ValueListModel(
  id: 'vl-types',
  name: 'Customer Types',
  customValues: 'New\nContinuing',
);

const _table = TableModel(id: 't1', name: 'customers', displayName: 'Customers', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'customer_type', displayName: 'Customer Type', fieldType: 'TEXT'),
]);

// A list taken from a field: the server answers it, here from [_fieldItems].
const _customerCodes = ValueListModel(
  id: 'vl-codes',
  name: 'Customer Codes',
  kind: ValueListModel.kindFromField,
  sourceTableId: 't2',
  sourceColumnId: 'c-code',
  secondColumnId: 'c-label',
);

const _codesByLabel = ValueListModel(
  id: 'vl-codes-by-label',
  name: 'Customer Codes by Label',
  kind: ValueListModel.kindFromField,
  sourceTableId: 't2',
  sourceColumnId: 'c-code',
  secondColumnId: 'c-label',
  showSecondOnly: true,
);

// Only the codes related to the record it is shown on.
const _relatedCodes = ValueListModel(
  id: 'vl-related',
  name: 'Related Codes',
  kind: ValueListModel.kindFromField,
  sourceTableId: 't2',
  sourceColumnId: 'c-code',
  secondColumnId: 'c-label',
  showSecondOnly: true,
  sourceOccurrenceId: 'o-codes',
  relatedOccurrenceId: 'o-customers',
);

const _fieldItems = [
  ValueListItem('N', second: 'Newcomer'),
  ValueListItem('C', second: 'Regular'),
];

class _ValueListApi extends ApiClient {

  _ValueListApi({required this.lists, required this.rows, this.fieldItems = _fieldItems, this.truncated = false})
      : super(baseUrl: 'http://localhost:1');
  final List<ValueListModel> lists;
  final List<Map<String, dynamic>> rows;
  final List<ValueListItem> fieldItems;
  final bool truncated;
  final List<Map<String, dynamic>> updates = [];

  /// The record each request for a list of related values was made for.
  final List<String?> relatedRequests = [];

  @override
  Future<List<ValueListModel>> listValueLists() async => lists;

  @override
  Future<ValueListOffer> valueListItems(String id, {String? table, String? recordId}) async {
    final list = lists.firstWhere((l) => l.id == id);
    if (!list.isFromField) return (items: [for (final v in list.customValueList) ValueListItem(v)], truncated: false);
    if (!list.isRelated) return (items: fieldItems, truncated: truncated);
    relatedRequests.add(table == null ? null : '$table/$recordId');
    // Each record has one related code, r1's until its first change.
    final first = recordId == 'r1' && updates.isEmpty;
    return (items: first ? [_fieldItems.first] : [_fieldItems.last], truncated: false);
  }

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [for (final r in rows) Map<String, dynamic>.from(r)];

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> values) async {
    updates.add(Map.of(values));
    return {'id': id, ...values};
  }
}

LayoutDefinitionModel _layoutWith(String controlStyle, {String? valueListId}) {
  return LayoutDefinitionModel(
    id: 'layout-1',
    name: 'Customers Form',
    tableOccurrence: 'customers',
    parts: const [
      LayoutPartModel(id: 'h', type: 'header', height: 40),
      LayoutPartModel(id: 'b', type: 'body', height: 300),
      LayoutPartModel(id: 'f', type: 'footer', height: 40),
    ],
    objects: [
      LayoutObjectModel(
        id: 'fld_customer_type',
        type: 'field',
        x: 40,
        y: 60,
        width: 260,
        height: 120,
        fieldBinding: FieldBindingModel(
          fieldName: 'customer_type',
          controlStyle: controlStyle,
          valueListId: valueListId,
        ),
      ),
    ],
  );
}

Future<_ValueListApi> _pump(
  WidgetTester tester,
  String controlStyle, {
  String? valueListId,
  List<ValueListModel> lists = const [_customerTypes],
  String current = 'Continuing',
  List<Map<String, dynamic>>? rows,
  List<ValueListItem> fieldItems = _fieldItems,
  bool truncated = false,
}) async {
  tester.view.physicalSize = const Size(1600, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);

  final api = _ValueListApi(
    lists: lists,
    fieldItems: fieldItems,
    truncated: truncated,
    rows: rows ??
        [
          {'id': 'r1', 'customer_type': current},
        ],
  );

  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: DataBrowserWidget(
        table: _table,
        apiClient: api,
        mode: OperationalMode.browse,
        layout: _layoutWith(controlStyle, valueListId: valueListId),
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return api;
}

/// How many text boxes the form itself has. The record bar contributes one
/// (the record number), which is not part of the layout.
int _formFields(WidgetTester tester) =>
    tester.widgetList<TextField>(find.byType(TextField)).length - 1;

void main() {
  testWidgets('a radio button set offers the list and writes what was chosen', (tester) async {
    final api = await _pump(tester, FieldControlStyle.radioButtonSet, valueListId: _customerTypes.id);

    expect(find.byKey(const ValueKey('radio-New')), findsOneWidget);
    expect(find.byKey(const ValueKey('radio-Continuing')), findsOneWidget);
    expect(find.byType(Radio<String>), findsNWidgets(2));

    final chosen = tester
        .widgetList<Radio<String>>(find.byType(Radio<String>))
        .where((r) => r.value == 'Continuing');
    expect(chosen.single.groupValue, 'Continuing', reason: 'the stored value is the one selected');

    await tester.tap(find.byKey(const ValueKey('radio-New')));
    await tester.pumpAndSettle(const Duration(seconds: 1));

    expect(api.updates.last['customer_type'], 'New');
  });

  testWidgets('a checkbox set can hold more than one value', (tester) async {
    final api = await _pump(
      tester,
      FieldControlStyle.checkboxSet,
      valueListId: _customerTypes.id,
      current: 'New',
    );

    expect(find.byType(Checkbox), findsNWidgets(2));

    await tester.tap(find.byKey(const ValueKey('checkbox-Continuing')));
    await tester.pumpAndSettle(const Duration(seconds: 1));

    expect(api.updates.last['customer_type'], 'New\nContinuing',
        reason: 'the chosen values are kept one per line, in the order of the list');
  });

  testWidgets('a drop-down offers the list', (tester) async {
    await _pump(tester, FieldControlStyle.dropDownList, valueListId: _customerTypes.id);

    expect(find.byKey(const ValueKey('value-list-control')), findsOneWidget);
    // The only text box left is the record-number one in the record bar: the
    // field itself is a control, not something to type into.
    expect(_formFields(tester), 0);
  });

  testWidgets('a control with no value list falls back to an edit box', (tester) async {
    await _pump(tester, FieldControlStyle.radioButtonSet);

    expect(find.byType(Radio<String>), findsNothing);
    expect(_formFields(tester), 1);
  });

  testWidgets('a control whose value list was deleted falls back to an edit box', (tester) async {
    await _pump(
      tester,
      FieldControlStyle.radioButtonSet,
      valueListId: 'vl-gone',
      lists: const [],
    );

    expect(find.byType(Radio<String>), findsNothing);
    expect(_formFields(tester), 1, reason: 'deleting a value list must not break a layout');
  });

  testWidgets('a value the list no longer offers is kept, not thrown away', (tester) async {
    await _pump(
      tester,
      FieldControlStyle.dropDownList,
      valueListId: _customerTypes.id,
      current: 'Lapsed',
    );

    final dropdown = tester.widget<DropdownButtonFormField<int>>(
      find.byKey(const ValueKey('value-list-control')),
    );
    expect(dropdown.initialValue, 0, reason: 'the stored value is kept as an entry of its own');
    expect(find.text('Lapsed'), findsOneWidget);
  });

  testWidgets('a second field is shown beside each value, and the value is what is stored', (tester) async {
    final api = await _pump(
      tester,
      FieldControlStyle.radioButtonSet,
      valueListId: _customerCodes.id,
      lists: const [_customerCodes],
      current: 'C',
    );

    expect(find.text('N  Newcomer'), findsOneWidget);
    expect(find.text('C  Regular'), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('radio-N')));
    await tester.pumpAndSettle(const Duration(seconds: 1));

    expect(api.updates.last['customer_type'], 'N');
  });

  testWidgets('showing the second field only shows it for the stored value too, and stores the first',
      (tester) async {
    final api = await _pump(
      tester,
      FieldControlStyle.popUpMenu,
      valueListId: _codesByLabel.id,
      lists: const [_codesByLabel],
      current: 'C',
    );

    expect(find.text('Regular'), findsOneWidget, reason: 'the stored C is shown as its label');
    expect(find.text('C'), findsNothing);

    await tester.tap(find.byKey(const ValueKey('value-list-control')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Newcomer').last);
    await tester.pumpAndSettle(const Duration(seconds: 1));

    expect(api.updates.last['customer_type'], 'N');
    expect(find.text('Newcomer'), findsOneWidget);
  });

  testWidgets('a checkbox set showing the second field only keeps the first field values', (tester) async {
    final api = await _pump(
      tester,
      FieldControlStyle.checkboxSet,
      valueListId: _codesByLabel.id,
      lists: const [_codesByLabel],
      current: 'N',
    );

    expect(find.text('Newcomer'), findsOneWidget);
    expect(find.text('Regular'), findsOneWidget);
    expect(find.text('N'), findsNothing);

    await tester.tap(find.byKey(const ValueKey('checkbox-C')));
    await tester.pumpAndSettle(const Duration(seconds: 1));

    expect(api.updates.last['customer_type'], 'N\nC');
  });

  testWidgets('a list of related values follows the current record', (tester) async {
    final api = await _pump(
      tester,
      FieldControlStyle.radioButtonSet,
      valueListId: _relatedCodes.id,
      lists: const [_relatedCodes],
      rows: [
        {'id': 'r1', 'customer_type': ''},
        {'id': 'r2', 'customer_type': ''},
      ],
    );

    expect(api.relatedRequests.last, 'customers/r1');
    expect(find.text('Newcomer'), findsOneWidget);
    expect(find.text('Regular'), findsNothing);

    await tester.tap(find.byTooltip('Last record'));
    await tester.pumpAndSettle();

    expect(api.relatedRequests.last, 'customers/r2');
    expect(find.text('Regular'), findsOneWidget);
    expect(find.text('Newcomer'), findsNothing);
  });

  testWidgets('a checkbox set keeps a stored value the list does not offer', (tester) async {
    final api = await _pump(
      tester,
      FieldControlStyle.checkboxSet,
      valueListId: _codesByLabel.id,
      lists: const [_codesByLabel],
      current: 'N\nRetired',
    );

    final retired = tester.widget<Checkbox>(
        find.descendant(of: find.byKey(const ValueKey('checkbox-Retired')), matching: find.byType(Checkbox)));
    expect(retired.value, isTrue, reason: 'it is shown, checked, after the values the list offers');
    expect(find.text('Retired'), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('checkbox-C')));
    await tester.pumpAndSettle(const Duration(seconds: 1));
    expect(api.updates.last['customer_type'], 'N\nC\nRetired', reason: 'only the value toggled changes');

    await tester.tap(find.byKey(const ValueKey('checkbox-N')));
    await tester.pumpAndSettle(const Duration(seconds: 1));
    expect(api.updates.last['customer_type'], 'C\nRetired');
  });

  testWidgets('a list of related values is read again once a field of the record is saved', (tester) async {
    final api = await _pump(
      tester,
      FieldControlStyle.radioButtonSet,
      valueListId: _relatedCodes.id,
      lists: const [_relatedCodes],
      current: '',
    );
    expect(find.text('Newcomer'), findsOneWidget);
    final before = api.relatedRequests.length;

    await tester.tap(find.byKey(const ValueKey('radio-N')));
    await tester.pumpAndSettle(const Duration(seconds: 1));

    expect(api.updates.last['customer_type'], 'N');
    expect(api.relatedRequests.length, greaterThan(before));
    expect(api.relatedRequests.last, 'customers/r1');
    expect(find.text('Regular'), findsOneWidget, reason: 'the record now relates to other values');
    expect(find.text('Newcomer'), findsNothing);
  });

  testWidgets('two entries storing the same value are both offered', (tester) async {
    const twins = [
      ValueListItem('N', second: 'Newcomer'),
      ValueListItem('N', second: 'Novice'),
      ValueListItem('C', second: 'Regular'),
    ];
    final api = await _pump(
      tester,
      FieldControlStyle.radioButtonSet,
      valueListId: _codesByLabel.id,
      lists: const [_codesByLabel],
      current: 'N',
      fieldItems: twins,
    );
    expect(find.text('Newcomer'), findsOneWidget);
    expect(find.text('Novice'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('radio-N#1')));
    await tester.pumpAndSettle(const Duration(seconds: 1));
    expect(api.updates, isEmpty, reason: 'Novice stores N, which the field already holds');
    await tester.tap(find.byKey(const ValueKey('radio-C')));
    await tester.pumpAndSettle(const Duration(seconds: 1));
    expect(api.updates.last['customer_type'], 'C');
  });

  testWidgets('a pop-up menu with two entries storing the same value opens and stores it', (tester) async {
    final api = await _pump(
      tester,
      FieldControlStyle.popUpMenu,
      valueListId: _codesByLabel.id,
      lists: const [_codesByLabel],
      current: 'C',
      fieldItems: const [
        ValueListItem('N', second: 'Newcomer'),
        ValueListItem('N', second: 'Novice'),
        ValueListItem('C', second: 'Regular'),
      ],
      truncated: true,
    );
    expect(find.text('Regular'), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('value-list-control')));
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('value-list-truncated')), findsWidgets,
        reason: 'a list cut by the server says so');
    await tester.tap(find.text('Novice').last);
    await tester.pumpAndSettle(const Duration(seconds: 1));

    expect(api.updates.last['customer_type'], 'N');
  });

  testWidgets('a radio button set says when the list has more values than shown', (tester) async {
    await _pump(
      tester,
      FieldControlStyle.radioButtonSet,
      valueListId: _customerCodes.id,
      lists: const [_customerCodes],
      truncated: true,
    );
    expect(find.byKey(const ValueKey('value-list-truncated')), findsOneWidget);
  });

  testWidgets('an edit box stays an edit box', (tester) async {
    await _pump(tester, FieldControlStyle.editBox, valueListId: _customerTypes.id);

    expect(find.byType(Radio<String>), findsNothing);
    expect(find.byKey(const ValueKey('value-list-control')), findsNothing);
    expect(_formFields(tester), 1);
  });
}
