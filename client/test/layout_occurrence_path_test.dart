import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

// A field of an occurrence several relationships away from the layout's
// (anchor–buoy) shows the first record at the end of the chain, read for
// every row of a list in one request, never a same-named local column.
// Synthetic data only.
const _orders = TableModel(id: 't1', name: 'orders', displayName: 'Orders', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'label', displayName: 'Label', fieldType: 'varchar'),
]);
const _regions = TableModel(id: 't3', name: 'regions', displayName: 'Regions', columns: [
  ColumnModel(id: 'r0', tableId: 't3', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'r1', tableId: 't3', name: 'label', displayName: 'Label', fieldType: 'varchar'),
]);

class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');

  final asked = <({List<String> ids, List<String> path, String? context})>[];
  final updates = <(String, String, Map<String, dynamic>)>[];

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> data, {bool script = false}) async {
    updates.add((table, id, data));
    return {'id': id, ...data};
  }

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [
        {'id': '1', 'label': 'Order one'},
        {'id': '2', 'label': 'Order two'},
      ];

  @override
  Future<List<TableModel>> listTables() async => [_orders, _regions];

  @override
  Future<List<TableOccurrenceModel>> listOccurrences() async => const [
        TableOccurrenceModel(id: 'o1', baseTableId: 't1', name: 'ORD__Orders', xPos: 0, yPos: 0),
        TableOccurrenceModel(id: 'o2', baseTableId: 't2', name: 'ORD_CUS__Customers', xPos: 0, yPos: 0),
        TableOccurrenceModel(id: 'o3', baseTableId: 't3', name: 'ORD_CUS_REG__Regions', xPos: 0, yPos: 0),
      ];

  @override
  Future<List<RelationshipModel>> listRelationships() async => const [
        RelationshipModel(id: 'rel1', name: 'a', leftOccurrenceId: 'o1', leftColumnId: 'x', rightOccurrenceId: 'o2', rightColumnId: 'y'),
        RelationshipModel(id: 'rel2', name: 'b', leftOccurrenceId: 'o2', leftColumnId: 'x', rightOccurrenceId: 'o3', rightColumnId: 'y'),
      ];

  @override
  Future<Map<String, Map<String, dynamic>?>> firstRelatedAlong(String table, List<String> ids,
      {required List<String> path, String? context}) async {
    asked.add((ids: ids, path: path, context: context));
    return {for (final id in ids) id: id == '1' ? {'id': '9', 'label': 'North'} : null};
  }
}

void main() {
  test('a field along a path round-trips and reads a related record', () {
    final binding = FieldBindingModel.fromJson(const {
      'field_name': 'label',
      'table_occurrence': 'ORD_CUS_REG__Regions',
      'occurrence_path': ['ORD_CUS__Customers', 'ORD_CUS_REG__Regions'],
    });
    expect(binding.isAlongPath, isTrue);
    expect(binding.readsRelated, isTrue);
    expect(FieldBindingModel.fromJson(binding.toJson()).occurrencePath, ['ORD_CUS__Customers', 'ORD_CUS_REG__Regions']);
    expect(binding.qualifiedName, 'ORD_CUS_REG__Regions::label');
  });

  testWidgets('List view shows the first record at the end of the path, for every row in one request', (tester) async {
    tester.view.physicalSize = const Size(1400, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    final api = _Api();
    const layout = LayoutDefinitionModel(
      id: 'l',
      name: 'L',
      tableOccurrence: 'ORD__Orders',
      defaultView: 'list',
      parts: [LayoutPartModel(id: 'b', type: 'body', height: 40)],
      objects: [
        LayoutObjectModel(
          id: 'region',
          type: 'field',
          x: 10,
          y: 5,
          width: 200,
          height: 24,
          fieldBinding: FieldBindingModel(
            fieldName: 'label',
            tableOccurrence: 'ORD_CUS_REG__Regions',
            occurrencePath: ['ORD_CUS__Customers', 'ORD_CUS_REG__Regions'],
          ),
        ),
      ],
    );
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(body: DataBrowserWidget(table: _orders, apiClient: api, mode: OperationalMode.browse, layout: layout)),
    ));
    await tester.pumpAndSettle();
    await tester.tap(find.text('List'));
    await tester.pumpAndSettle();

    expect(find.text('North'), findsOneWidget);
    expect(find.text('Order one'), findsNothing, reason: 'never the label of the record in hand');
    final listAsk = api.asked.last;
    expect(listAsk.ids, containsAll(['1', '2']));
    expect(listAsk.path, ['ORD_CUS__Customers', 'ORD_CUS_REG__Regions']);
    expect(listAsk.context, 'ORD__Orders');

    // Typed into, it writes that record, in its own table.
    await tester.enterText(find.byKey(const ValueKey('related-field-edit')).first, 'South');
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle();
    expect(api.updates, hasLength(1));
    expect(api.updates.single.$1, 'regions');
    expect(api.updates.single.$2, '9');
    expect(api.updates.single.$3, {'label': 'South'});
    expect(find.byKey(const ValueKey('related-field-edit')), findsOneWidget,
        reason: 'a row with nothing at the end of the path has nothing to edit');
  });
}
