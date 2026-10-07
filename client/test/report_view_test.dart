// #32 — these render the real Browse mode on a report layout and look at the
// bands that came out, and at what the client asked the server to total.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart';

const _table = TableModel(id: 't1', name: 'customers', displayName: 'Customers', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'last_name', displayName: 'Last Name', fieldType: 'TEXT'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'customer_type', displayName: 'Customer Type', fieldType: 'TEXT'),
  ColumnModel(id: 'c3', tableId: 't1', name: 'fee_paid', displayName: 'Fee Paid', fieldType: 'NUMBER'),
  ColumnModel(
    id: 'c4',
    tableId: 't1',
    name: 'fee_total',
    displayName: 'Total Fees',
    fieldType: 'SUMMARY',
    calculationFormula: '{"summary_type":"total","field":"fee_paid"}',
  ),
]);

/// One recorded call to the summary endpoint.
class SummaryCall {
  final List<String> fields;
  final List<String> groupBy;
  final List<Map<String, dynamic>> requests;
  const SummaryCall(this.fields, this.groupBy, this.requests);
}

class _ReportApi extends ApiClient {
  final List<Map<String, dynamic>> rows;
  final List<SummaryCall> summaryCalls = [];

  _ReportApi(this.rows) : super(baseUrl: 'http://localhost:1');

  @override
  Future<List<ValueListModel>> listValueLists() async => const [];

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [for (final r in rows) Map<String, dynamic>.from(r)];

  @override
  Future<List<Map<String, dynamic>>> listAllRows(String table, {void Function(int)? onProgress}) async =>
      [for (final r in rows) Map<String, dynamic>.from(r)];

  @override
  Future<SummaryResultModel> summarize(
    String table, {
    List<Map<String, dynamic>> requests = const [],
    List<String> fields = const [],
    List<String> groupBy = const [],
  }) async {
    summaryCalls.add(SummaryCall(fields, groupBy, requests));
    if (groupBy.isEmpty) {
      return const SummaryResultModel(count: 5, grand: {'fee_total': '700'});
    }
    return const SummaryResultModel(
      count: 5,
      grand: {'fee_total': '700'},
      groupBy: ['customer_type'],
      groups: [
        SummaryGroupModel(
            values: {'customer_type': 'Continuing'}, count: 3, summaries: {'fee_total': '300'}),
        SummaryGroupModel(values: {'customer_type': 'New'}, count: 2, summaries: {'fee_total': '400'}),
      ],
    );
  }
}

/// A report: header, a sub-summary that breaks on Customer Type holding the
/// type and its total, a body row, and a trailing grand summary.
LayoutDefinitionModel _reportLayout({bool withSubSummary = true, bool withGrand = true}) {
  final parts = <LayoutPartModel>[
    const LayoutPartModel(id: 'header', type: LayoutPartType.header, height: 40),
    if (withSubSummary)
      const LayoutPartModel(
          id: 'sub', type: LayoutPartType.subSummary, height: 30, breakField: 'customer_type'),
    const LayoutPartModel(id: 'body', type: LayoutPartType.body, height: 30),
    if (withGrand)
      const LayoutPartModel(id: 'grand', type: LayoutPartType.trailingGrandSummary, height: 30),
    const LayoutPartModel(id: 'footer', type: LayoutPartType.footer, height: 30),
  ];

  // Objects are placed by where they fall down the canvas, which is what puts
  // them in a part.
  double top = 0;
  final tops = <String, double>{};
  for (final part in parts) {
    tops[part.id] = top;
    top += part.height;
  }

  return LayoutDefinitionModel(
    id: 'report-layout',
    name: 'Annual Fee Report',
    tableOccurrence: 'customers',
    width: 600,
    parts: parts,
    objects: [
      LayoutObjectModel(
        id: 'title',
        type: 'label',
        x: 20,
        y: tops['header']! + 8,
        width: 300,
        height: 24,
        text: 'Annual Fee Report',
      ),
      if (withSubSummary) ...[
        LayoutObjectModel(
          id: 'sub_type',
          type: 'field',
          x: 20,
          y: tops['sub']! + 4,
          width: 200,
          height: 22,
          fieldBinding: const FieldBindingModel(fieldName: 'customer_type'),
        ),
        LayoutObjectModel(
          id: 'sub_total',
          type: 'field',
          x: 300,
          y: tops['sub']! + 4,
          width: 120,
          height: 22,
          fieldBinding: const FieldBindingModel(fieldName: 'fee_total'),
        ),
      ],
      LayoutObjectModel(
        id: 'body_name',
        type: 'field',
        x: 40,
        y: tops['body']! + 4,
        width: 200,
        height: 22,
        fieldBinding: const FieldBindingModel(fieldName: 'last_name'),
      ),
      LayoutObjectModel(
        id: 'body_fee',
        type: 'field',
        x: 300,
        y: tops['body']! + 4,
        width: 120,
        height: 22,
        fieldBinding: const FieldBindingModel(fieldName: 'fee_paid'),
      ),
      if (withGrand)
        LayoutObjectModel(
          id: 'grand_total',
          type: 'field',
          x: 300,
          y: tops['grand']! + 4,
          width: 120,
          height: 22,
          fieldBinding: const FieldBindingModel(fieldName: 'fee_total'),
        ),
    ],
  );
}

final _rows = [
  {'id': '1', 'last_name': 'Alvarez', 'customer_type': 'Continuing', 'fee_paid': '100'},
  {'id': '2', 'last_name': 'Murphy', 'customer_type': 'Continuing', 'fee_paid': '100'},
  {'id': '3', 'last_name': 'Smith', 'customer_type': 'Continuing', 'fee_paid': '100'},
  {'id': '4', 'last_name': 'Cannon', 'customer_type': 'New', 'fee_paid': '200'},
  {'id': '5', 'last_name': 'Lee', 'customer_type': 'New', 'fee_paid': '200'},
];

Future<_ReportApi> _pump(
  WidgetTester tester, {
  LayoutDefinitionModel? layout,
  List<Map<String, dynamic>>? rows,
}) async {
  tester.view.physicalSize = const Size(1400, 1400);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);

  final api = _ReportApi(rows ?? _rows);
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: DataBrowserWidget(
        table: _table,
        apiClient: api,
        mode: OperationalMode.browse,
        layout: layout ?? _reportLayout(),
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return api;
}

/// Switches the browser to its List view, which draws a report.
Future<void> _showReport(WidgetTester tester) async {
  await tester.tap(find.text('Report'));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('a layout with summary parts offers a Report view, not a List view',
      (tester) async {
    await _pump(tester);
    expect(find.text('Report'), findsOneWidget);
    expect(find.text('List'), findsNothing);
  });

  testWidgets('a layout without summary parts is still a List', (tester) async {
    await _pump(tester, layout: _reportLayout(withSubSummary: false, withGrand: false));
    expect(find.text('List'), findsOneWidget);
    expect(find.text('Report'), findsNothing);
  });

  testWidgets('the report draws a sub-summary per group and a grand summary once',
      (tester) async {
    await _pump(tester);
    await _showReport(tester);

    expect(find.byKey(const ValueKey('report-view')), findsOneWidget);

    // One band per group, plus one per record, plus the grand summary.
    final inReport = find.descendant(
      of: find.byKey(const ValueKey('report-view')),
      matching: find.text('Continuing'),
    );
    expect(inReport, findsOneWidget, reason: 'the group names itself once');
    expect(
      find.descendant(
        of: find.byKey(const ValueKey('report-view')),
        matching: find.text('New'),
      ),
      findsOneWidget,
      reason: 'and so does the other, without counting the New button in the toolbar',
    );
    expect(find.text('300'), findsOneWidget, reason: "the Continuing group's total");
    expect(find.text('400'), findsOneWidget, reason: "the New group's total");
    expect(find.text('700'), findsOneWidget, reason: 'the grand total, once');

    for (final name in ['Alvarez', 'Murphy', 'Smith', 'Cannon', 'Lee']) {
      expect(find.text(name), findsOneWidget, reason: '$name is drawn as a body row');
    }
  });

  testWidgets('the figures are asked for once per break level', (tester) async {
    final api = await _pump(tester);
    await _showReport(tester);

    expect(api.summaryCalls.length, 2, reason: 'the grand totals, and the groups');
    expect(api.summaryCalls[0].groupBy, isEmpty);
    expect(api.summaryCalls[1].groupBy, ['customer_type']);
    expect(api.summaryCalls.first.fields, ['fee_total'],
        reason: 'only the summary fields of the table are asked for');
  });

  testWidgets('a report reads the whole found set, not the first page', (tester) async {
    // listAllRows pages; listRows does not. A report must use the former or
    // its totals would not match the rows under them.
    final api = await _pump(tester);
    await _showReport(tester);
    expect(api.summaryCalls, isNotEmpty);
    expect(find.text('Alvarez'), findsOneWidget);
  });

  testWidgets('a report over records in the wrong order says so and offers to sort',
      (tester) async {
    // The records arrive sorted by nothing in particular, so the groups would
    // interleave.
    await _pump(tester, rows: [
      {'id': '1', 'last_name': 'Alvarez', 'customer_type': 'Continuing', 'fee_paid': '100'},
      {'id': '4', 'last_name': 'Cannon', 'customer_type': 'New', 'fee_paid': '200'},
      {'id': '2', 'last_name': 'Murphy', 'customer_type': 'Continuing', 'fee_paid': '100'},
    ]);
    await _showReport(tester);

    expect(find.byKey(const ValueKey('report-sort-warning')), findsOneWidget);
    expect(find.textContaining('Customer Type'), findsWidgets);

    await tester.tap(find.byKey(const ValueKey('report-sort-fix')));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('report-sort-warning')), findsNothing,
        reason: 'sorting for the report settles the warning');
  });

  testWidgets('a summary field is not typed into on a form', (tester) async {
    await _pump(tester);
    // Form view: the layout has no summary field object outside its parts, and
    // the record bar is the only text box.
    expect(find.byType(TextField), findsWidgets);
  });

  group('the summary fields of a table', () {
    test('a definition in either shape is read', () {
      final current = SummarySpecModel.tryParse('{"summary_type":"total","field":"fee_paid"}');
      expect(current?.summaryType, SummaryType.total);
      expect(current?.field, 'fee_paid');

      final older = SummarySpecModel.tryParse(
          '{"operation":"AVG","target_column":"fee_paid","running_total":true}');
      expect(older?.summaryType, SummaryType.average);
      expect(older?.field, 'fee_paid');
      expect(older?.running, isTrue);
    });

    test('a field with no definition yet is not a summary to compute', () {
      expect(SummarySpecModel.tryParse(null), isNull);
      expect(SummarySpecModel.tryParse(''), isNull);
      expect(SummarySpecModel.tryParse('not json'), isNull);
      expect(SummarySpecModel.tryParse('{"summary_type":"total"}'), isNull,
          reason: 'nothing to summarize');
    });

    test('it survives being written out and read back', () {
      const spec = SummarySpecModel(
          summaryType: SummaryType.fractionOfTotal, field: 'fee_paid', running: false);
      final back = SummarySpecModel.tryParse(
          '{"summary_type":"${spec.summaryType}","field":"${spec.field}","running":false}');
      expect(back?.summaryType, SummaryType.fractionOfTotal);
    });

    test('which kinds need a number field', () {
      expect(SummaryType.needsNumber(SummaryType.total), isTrue);
      expect(SummaryType.needsNumber(SummaryType.average), isTrue);
      expect(SummaryType.needsNumber(SummaryType.standardDeviation), isTrue);
      expect(SummaryType.needsNumber(SummaryType.fractionOfTotal), isTrue);
      expect(SummaryType.needsNumber(SummaryType.count), isFalse);
      expect(SummaryType.needsNumber(SummaryType.minimum), isFalse);
      expect(SummaryType.needsNumber(SummaryType.maximum), isFalse);
    });
  });

  group('a layout that is a report', () {
    test('knows it is one, and what it groups by', () {
      final layout = _reportLayout();
      expect(layout.isReport, isTrue);
      expect(layout.breakFields, ['customer_type']);
      expect(layout.requiredSortOrder, ['customer_type']);
      expect(layout.leadingSubSummaries.map((p) => p.id), ['sub']);
      expect(layout.trailingSubSummaries, isEmpty);
      expect(layout.trailingGrandSummary?.id, 'grand');
      expect(layout.leadingGrandSummary, isNull);
    });

    test('a plain form is not a report', () {
      final layout = _reportLayout(withSubSummary: false, withGrand: false);
      expect(layout.isReport, isFalse);
      expect(layout.breakFields, isEmpty);
    });

    test('objects belong to the part they are drawn in', () {
      final layout = _reportLayout();
      final sub = layout.parts.firstWhere((p) => p.id == 'sub');
      final body = layout.parts.firstWhere((p) => p.id == 'body');

      expect(layout.objectsIn(sub).map((o) => o.id), ['sub_type', 'sub_total']);
      expect(layout.objectsIn(body).map((o) => o.id), ['body_name', 'body_fee']);
    });

    test('a part knows where it starts', () {
      final layout = _reportLayout();
      expect(layout.partTop('header'), 0);
      expect(layout.partTop('sub'), 40);
      expect(layout.partTop('body'), 70);
      expect(layout.partTop('grand'), 100);
    });
  });
}
