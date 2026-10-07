// #37 — a chart on a layout, rendered in real Browse mode, drawing the figures
// the server worked out over the found set.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/chart_view.dart';
import 'package:file4base_client/features/layout_engine/models/chart_definition.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart';

const _table = TableModel(id: 't1', name: 'customers', displayName: 'Customers', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'last_name', displayName: 'Last Name', fieldType: 'TEXT'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'customer_type', displayName: 'Customer Type', fieldType: 'TEXT'),
  ColumnModel(id: 'c3', tableId: 't1', name: 'fee_paid', displayName: 'Fee Paid', fieldType: 'NUMBER'),
  ColumnModel(
    id: 'c4', tableId: 't1', name: 'fee_total', displayName: 'Total Fees', fieldType: 'SUMMARY',
    calculationFormula: '{"summary_type":"total","field":"fee_paid"}',
  ),
]);

class _ChartApi extends ApiClient {
  final List<({List<String> fields, List<String> groupBy})> summaryCalls = [];

  _ChartApi() : super(baseUrl: 'http://localhost:1');

  @override
  Future<List<ValueListModel>> listValueLists() async => const [];

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [
        {'id': '1', 'last_name': 'Alvarez', 'customer_type': 'Continuing', 'fee_paid': '100'},
        {'id': '2', 'last_name': 'Cannon', 'customer_type': 'New', 'fee_paid': '200'},
      ];

  @override
  Future<SummaryResultModel> summarize(
    String table, {
    List<Map<String, dynamic>> requests = const [],
    List<String> fields = const [],
    List<String> groupBy = const [],
  }) async {
    summaryCalls.add((fields: fields, groupBy: groupBy));
    return const SummaryResultModel(
      count: 16,
      grand: {'fee_total': '2300'},
      groupBy: ['customer_type'],
      groups: [
        SummaryGroupModel(
            values: {'customer_type': 'Continuing'}, count: 9, summaries: {'fee_total': '900'}),
        SummaryGroupModel(values: {'customer_type': 'New'}, count: 7, summaries: {'fee_total': '1400'}),
      ],
    );
  }
}

LayoutDefinitionModel _layoutWithChart({ChartConfigModel? config}) => LayoutDefinitionModel(
      id: 'l1',
      name: 'Customers Form',
      tableOccurrence: 'customers',
      width: 700,
      parts: const [
        LayoutPartModel(id: 'h', type: LayoutPartType.header, height: 40),
        LayoutPartModel(id: 'b', type: LayoutPartType.body, height: 400),
        LayoutPartModel(id: 'f', type: LayoutPartType.footer, height: 40),
      ],
      objects: [
        LayoutObjectModel(
          id: 'chart_1',
          type: 'chart',
          x: 40,
          y: 60,
          width: 420,
          height: 260,
          chartConfig: (config ??
                  const ChartConfigModel(
                    chartType: ChartType.column,
                    title: 'Fees by customer type',
                    categoryField: 'customer_type',
                    series: [ChartSeriesModel(field: 'fee_total', label: 'Total Fees')],
                  ))
              .toJson(),
        ),
      ],
    );

Future<_ChartApi> _pump(WidgetTester tester, {ChartConfigModel? config}) async {
  tester.view.physicalSize = const Size(1400, 1100);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);

  final api = _ChartApi();
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: DataBrowserWidget(
        table: _table,
        apiClient: api,
        mode: OperationalMode.browse,
        layout: _layoutWithChart(config: config),
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return api;
}

void main() {
  testWidgets('a chart on a layout draws, with its title', (tester) async {
    await _pump(tester);
    expect(find.byKey(const ValueKey('chart-chart_1')), findsOneWidget);
    expect(find.byKey(const ValueKey('chart')), findsOneWidget);
    expect(find.text('Fees by customer type'), findsOneWidget);
  });

  testWidgets('it asks the server for its own grouping', (tester) async {
    final api = await _pump(tester);
    expect(api.summaryCalls, isNotEmpty);
    expect(api.summaryCalls.first.groupBy, ['customer_type'],
        reason: 'a chart groups by its own category field');
    expect(api.summaryCalls.first.fields, ['fee_total'],
        reason: 'only the fields its series name');
  });

  testWidgets('a chart with no setup says what to do instead of drawing nothing',
      (tester) async {
    final api = await _pump(tester, config: const ChartConfigModel());
    expect(find.textContaining('Chart Setup'), findsOneWidget);
    expect(api.summaryCalls, isEmpty, reason: 'nothing to ask for');
  });

  testWidgets('a scatter reads the records rather than asking for figures', (tester) async {
    final api = await _pump(
      tester,
      config: const ChartConfigModel(
        chartType: ChartType.scatter,
        categoryField: 'fee_paid',
        series: [ChartSeriesModel(field: 'fee_paid')],
      ),
    );
    expect(find.byKey(const ValueKey('chart')), findsOneWidget);
    expect(api.summaryCalls, isEmpty);
  });

  testWidgets('switching to a layout with charts reads their figures', (tester) async {
    // Switching layouts does not re-fetch records, so the charts have to be
    // read when the layout changes or they sit on "Reading the figures…".
    tester.view.physicalSize = const Size(1400, 1100);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    final api = _ChartApi();
    final plain = LayoutDefinitionModel(
      id: 'plain',
      name: 'Plain',
      tableOccurrence: 'customers',
      width: 700,
      parts: const [LayoutPartModel(id: 'b', type: LayoutPartType.body, height: 200)],
      objects: const [
        LayoutObjectModel(
          id: 'f1', type: 'field', x: 20, y: 20, width: 200, height: 24,
          fieldBinding: FieldBindingModel(fieldName: 'last_name'),
        ),
      ],
    );

    Widget app(LayoutDefinitionModel layout) => MaterialApp(
          home: Scaffold(
            body: DataBrowserWidget(
              table: _table,
              apiClient: api,
              mode: OperationalMode.browse,
              layout: layout,
            ),
          ),
        );

    await tester.pumpWidget(app(plain));
    await tester.pumpAndSettle();
    expect(api.summaryCalls, isEmpty, reason: 'a layout with no chart asks for nothing');

    await tester.pumpWidget(app(_layoutWithChart()));
    await tester.pumpAndSettle();

    expect(api.summaryCalls, isNotEmpty, reason: 'the chart on the new layout was read');
    expect(find.byKey(const ValueKey('chart')), findsOneWidget);
  });

  testWidgets('a chart shows nothing in Find mode, and says so', (tester) async {
    tester.view.physicalSize = const Size(1400, 1100);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: DataBrowserWidget(
          table: _table,
          apiClient: _ChartApi(),
          mode: OperationalMode.find,
          layout: _layoutWithChart(),
        ),
      ),
    ));
    await tester.pumpAndSettle();
    expect(find.textContaining('nothing in Find mode'), findsOneWidget);
  });
}
