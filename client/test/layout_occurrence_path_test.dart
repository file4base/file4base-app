import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/layout_designer_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
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
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> data, {bool overrideValidation = false, bool script = false}) async {
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


/// Orders with the key that reaches a customer, and through it a region.
const _keyedOrders = TableModel(id: 't1', name: 'orders', displayName: 'Orders', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'label', displayName: 'Label', fieldType: 'varchar'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'customer', displayName: 'Customer', fieldType: 'varchar'),
  ColumnModel(id: 'c3', tableId: 't1', name: 'status', displayName: 'Status', fieldType: 'varchar'),
]);
const _keyedRegions = TableModel(id: 't3', name: 'regions', displayName: 'Regions', columns: [
  ColumnModel(id: 'r0', tableId: 't3', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'r1', tableId: 't3', name: 'label', displayName: 'Label', fieldType: 'varchar'),
  ColumnModel(id: 'r2', tableId: 't3', name: 'status', displayName: 'Status', fieldType: 'varchar'),
  ColumnModel(id: 'r3', tableId: 't3', name: 'region_code', displayName: 'Region Code', fieldType: 'varchar'),
]);

/// The region each customer key reaches, read along the path from the
/// order's stored key, as the server does.
class _KeyedApi extends _Api {
  final Map<String, dynamic> order = {'id': '1', 'label': 'Order one', 'customer': 'A'};
  static const regions = {
    'A': {'id': '9', 'label': 'North'},
    'B': {'id': '8', 'label': 'West'},
    'C': {'id': '7', 'label': 'East'},
  };

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [Map<String, dynamic>.of(order)];

  @override
  Future<List<TableModel>> listTables() async => [_keyedOrders, _keyedRegions];

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> data,
      {bool overrideValidation = false, bool script = false}) async {
    if (table == 'orders') order.addAll(data);
    return super.updateRow(table, id, data, overrideValidation: overrideValidation, script: script);
  }

  @override
  Future<Map<String, Map<String, dynamic>?>> firstRelatedAlong(String table, List<String> ids,
      {required List<String> path, String? context}) async {
    asked.add((ids: ids, path: path, context: context));
    final region = regions[order['customer']];
    return {for (final id in ids) id: region == null ? null : Map<String, dynamic>.of(region)};
  }
}

const _keyedLayout = LayoutDefinitionModel(
  id: 'l',
  name: 'L',
  tableOccurrence: 'ORD__Orders',
  parts: [LayoutPartModel(id: 'b', type: 'body', height: 200)],
  objects: [
    LayoutObjectModel(
        id: 'customer', type: 'field', x: 10, y: 5, width: 200, height: 24,
        fieldBinding: FieldBindingModel(fieldName: 'customer')),
    LayoutObjectModel(
      id: 'region',
      type: 'field',
      x: 10,
      y: 60,
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

Future<_KeyedApi> _pumpKeyed(WidgetTester tester) async {
  tester.view.physicalSize = const Size(1400, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final api = _KeyedApi();
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
        body: DataBrowserWidget(table: _keyedOrders, apiClient: api, mode: OperationalMode.browse, layout: _keyedLayout)),
  ));
  await tester.pumpAndSettle();
  return api;
}

Finder _box(String text) => find.byWidgetPredicate((w) => w is TextField && w.controller?.text == text);

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
  testWidgets('a key changed in the record makes a field along a path read and write the record it reaches now',
      (tester) async {
    final api = await _pumpKeyed(tester);
    expect(_box('North'), findsOneWidget);

    await tester.enterText(_box('A'), 'B');
    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();
    expect(api.order['customer'], 'B');
    expect(_box('West'), findsOneWidget, reason: "the order now reaches customer B's region");

    await tester.enterText(find.byKey(const ValueKey('related-field-edit')), 'South');
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle();
    expect(api.updates.where((u) => u.$1 == 'regions').map((u) => u.$2), ['8'],
        reason: "written to B's region, never A's");
  });

  testWidgets('a field along a path is not written when the record it reaches changed since it was shown',
      (tester) async {
    final api = await _pumpKeyed(tester);
    expect(_box('North'), findsOneWidget);
    // Another user, or a script, changes the key.
    api.order['customer'] = 'C';

    await tester.enterText(find.byKey(const ValueKey('related-field-edit')), 'South');
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle();
    expect(api.updates.where((u) => u.$1 == 'regions'), isEmpty, reason: 'the region shown (9) is no longer related');
    expect(find.textContaining('was not saved'), findsOneWidget);
    expect(_box('East'), findsOneWidget, reason: 'it shows the record it reaches now');
  });

  testWidgets('rebinding a field along a path offers the fields at its end, and keeps the path', (tester) async {
    tester.view.physicalSize = const Size(1600, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    // The field inspector draws its check boxes over a coloured panel and
    // lets a long occurrence name overflow, for any field: those two
    // drawing notices are not what is tested here.
    final report = FlutterError.onError;
    FlutterError.onError = (details) {
      final text = details.exceptionAsString();
      if (text.contains('ListTile background color') || text.contains('overflowed by')) return;
      report?.call(details);
    };
    addTearDown(() => FlutterError.onError = report);
    LayoutDefinitionModel? last;
    final key = GlobalKey<LayoutDesignerWidgetState>();
    await tester.pumpWidget(ProviderScope(
        child: MaterialApp(
      home: Scaffold(
        body: LayoutDesignerWidget(
          key: key,
          table: _keyedOrders,
          tables: const [_keyedOrders, _keyedRegions],
          apiClient: _KeyedApi(),
          initialLayout: _keyedLayout,
          onSaved: () {},
          onLayoutChanged: (l) => last = l,
        ),
      ),
    )));
    await tester.pumpAndSettle();
    key.currentState!.selectObject('region');
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('field-occurrence-path')), findsOneWidget);

    final picker = find.byKey(const ValueKey('inspector-field-column'));
    await tester.ensureVisible(picker);
    await tester.tap(picker);
    await tester.pumpAndSettle();
    expect(find.text('Region Code (varchar)'), findsWidgets, reason: "the end table's fields are offered");
    expect(find.text('Customer (varchar)'), findsNothing, reason: "not the layout table's");
    await tester.tap(find.text('Status (varchar)').last);
    await tester.pumpAndSettle();

    final binding = last!.objects.singleWhere((o) => o.id == 'region').fieldBinding!;
    expect(binding.fieldName, 'status');
    expect(binding.occurrencePath, ['ORD_CUS__Customers', 'ORD_CUS_REG__Regions']);
    expect(binding.tableOccurrence, 'ORD_CUS_REG__Regions');
    await tester.pump(const Duration(seconds: 2)); // the auto-save debounce
  });
}
