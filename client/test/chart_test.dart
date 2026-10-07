// #37 — a chart draws the figures #32 works out. These check what it takes
// from them, and that it draws.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/chart_view.dart';
import 'package:file4base_client/features/layout_engine/models/chart_definition.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';

const _figures = SummaryResultModel(
  count: 16,
  grand: {'fee_total': '2300', 'fee_count': '16'},
  groupBy: ['customer_type'],
  groups: [
    SummaryGroupModel(
      values: {'customer_type': 'Continuing'},
      count: 9,
      summaries: {'fee_total': '900', 'fee_count': '9'},
    ),
    SummaryGroupModel(
      values: {'customer_type': 'New'},
      count: 7,
      summaries: {'fee_total': '1400', 'fee_count': '7'},
    ),
  ],
);

const _config = ChartConfigModel(
  chartType: ChartType.column,
  title: 'Fees by customer type',
  categoryField: 'customer_type',
  series: [
    ChartSeriesModel(field: 'fee_total', label: 'Total Fees'),
    ChartSeriesModel(field: 'fee_count', label: 'Customers'),
  ],
);

Future<void> _pump(WidgetTester tester, ChartConfigModel config, ChartData data) async {
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: Center(
        child: SizedBox(width: 400, height: 260, child: ChartView(config: config, data: data)),
      ),
    ),
  ));
  await tester.pumpAndSettle();
}

void main() {
  group('what a grouped chart takes from the figures', () {
    test('a category per group, a value per series', () {
      final data = chartDataFromSummary(_config, _figures);

      expect(data.seriesLabels, ['Total Fees', 'Customers']);
      expect([for (final p in data.points) p.category], ['Continuing', 'New']);
      expect(data.points[0].values, [900, 9]);
      expect(data.points[1].values, [1400, 7]);
      expect(data.maxValue, 1400);
    });

    test('a series with no label falls back to its field name', () {
      const plain = ChartConfigModel(
        categoryField: 'customer_type',
        series: [ChartSeriesModel(field: 'fee_total')],
      );
      expect(chartDataFromSummary(plain, _figures).seriesLabels, ['fee_total']);
    });

    test('a figure that is not a number is a gap, not a zero', () {
      const figures = SummaryResultModel(
        groupBy: ['customer_type'],
        groups: [
          SummaryGroupModel(
              values: {'customer_type': 'Continuing'}, count: 1, summaries: {'fee_total': null}),
        ],
      );
      final data = chartDataFromSummary(_config, figures);
      expect(data.points.single.values.first, isNull);
    });

    test('a category is written the way its field is shown', () {
      const figures = SummaryResultModel(
        groupBy: ['date_paid'],
        groups: [
          SummaryGroupModel(
              values: {'date_paid': '2011-03-01T00:00:00Z'},
              count: 1,
              summaries: {'fee_total': '100'}),
        ],
      );
      const byDate = ChartConfigModel(
        categoryField: 'date_paid',
        series: [ChartSeriesModel(field: 'fee_total')],
      );
      final data = chartDataFromSummary(byDate, figures,
          formatCategory: (field, value) => value.toString().split('T').first);
      expect(data.points.single.category, '2011-03-01');
    });

    test('no figures, no category or no series means nothing to draw', () {
      expect(chartDataFromSummary(_config, null).isEmpty, isTrue);
      expect(chartDataFromSummary(const ChartConfigModel(), _figures).isEmpty, isTrue);
      expect(
        chartDataFromSummary(
          const ChartConfigModel(categoryField: 'customer_type'),
          _figures,
        ).isEmpty,
        isTrue,
      );
    });
  });

  group('a pie shows one series', () {
    test('it draws the first when several are set up', () {
      const pie = ChartConfigModel(
        chartType: ChartType.pie,
        categoryField: 'customer_type',
        series: [
          ChartSeriesModel(field: 'fee_total'),
          ChartSeriesModel(field: 'fee_count'),
        ],
      );
      expect(pie.drawnSeries.map((s) => s.field), ['fee_total']);

      final data = chartDataFromSummary(pie, _figures);
      expect(data.firstSeriesTotal, 2300, reason: '900 and 1400');
    });

    test('other kinds draw every series', () {
      expect(_config.drawnSeries.length, 2);
    });
  });

  group('a scatter reads the records, not the groups', () {
    const scatter = ChartConfigModel(
      chartType: ChartType.scatter,
      categoryField: 'fee_paid',
      series: [ChartSeriesModel(field: 'annual_fee')],
    );

    test('one point per record with both numbers', () {
      final data = chartDataFromRecords(scatter, [
        {'fee_paid': '100', 'annual_fee': '100'},
        {'fee_paid': '200', 'annual_fee': '200'},
      ]);
      expect(data.points.length, 2);
      expect(data.points.first.values.first, 100);
    });

    test('a record missing either number is left out, not drawn at zero', () {
      final data = chartDataFromRecords(scatter, [
        {'fee_paid': '100', 'annual_fee': '100'},
        {'fee_paid': '', 'annual_fee': '200'},
        {'fee_paid': '300', 'annual_fee': null},
        {'fee_paid': 'not a number', 'annual_fee': '50'},
      ]);
      expect(data.points.length, 1, reason: 'only the complete record is a point');
    });
  });

  group('how a figure is written', () {
    test('a whole number keeps no decimals', () {
      expect(chartValueText(900), '900');
      expect(chartValueText(900.0), '900');
    });
    test('the rest get two', () => expect(chartValueText(133.333), '133.33'));
  });

  group('series colours', () {
    test('a series takes the next palette colour when none was chosen', () {
      expect(chartSeriesColor(const ChartSeriesModel(field: 'a'), 0), kChartPalette[0]);
      expect(chartSeriesColor(const ChartSeriesModel(field: 'b'), 1), kChartPalette[1]);
    });

    test('the palette repeats rather than running out', () {
      expect(chartSeriesColor(const ChartSeriesModel(field: 'x'), kChartPalette.length),
          kChartPalette[0]);
    });

    test('a chosen colour is used', () {
      expect(chartSeriesColor(const ChartSeriesModel(field: 'a', color: '#FF0000'), 0),
          const Color(0xFFFF0000));
    });

    test('a colour that does not parse falls back to the palette', () {
      expect(chartSeriesColor(const ChartSeriesModel(field: 'a', color: 'red'), 0),
          kChartPalette[0]);
    });
  });

  group('drawing', () {
    testWidgets('a chart with data draws, with its title', (tester) async {
      await _pump(tester, _config, chartDataFromSummary(_config, _figures));
      expect(find.byKey(const ValueKey('chart')), findsOneWidget);
      expect(find.text('Fees by customer type'), findsOneWidget);
      expect(find.byKey(const ValueKey('chart-notice')), findsNothing);
    });

    testWidgets('every chart type draws without failing', (tester) async {
      for (final type in ChartType.all) {
        final config = _config.copyWith(chartType: type);
        final data = type == ChartType.scatter
            ? chartDataFromRecords(config, [
                {'customer_type': '1', 'fee_total': '900'},
                {'customer_type': '2', 'fee_total': '1400'},
              ])
            : chartDataFromSummary(config, _figures);
        await _pump(tester, config, data);
        expect(find.byKey(const ValueKey('chart')), findsOneWidget, reason: type);
      }
    });

    testWidgets('a chart with nothing to draw says so instead of drawing nothing',
        (tester) async {
      await _pump(tester, _config, const ChartData());
      expect(find.byKey(const ValueKey('chart-notice')), findsOneWidget);
    });

    testWidgets('a notice replaces the chart when one is given', (tester) async {
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: SizedBox(
            width: 300,
            height: 200,
            child: ChartView(
              config: _config,
              data: chartDataFromSummary(_config, _figures),
              notice: 'Choose a field to chart',
            ),
          ),
        ),
      ));
      await tester.pumpAndSettle();
      expect(find.text('Choose a field to chart'), findsOneWidget);
    });

    testWidgets('the legend names the series, and a pie names its slices', (tester) async {
      await _pump(tester, _config, chartDataFromSummary(_config, _figures));
      expect(find.text('Total Fees'), findsOneWidget);
      expect(find.text('Customers'), findsOneWidget);

      const pie = ChartConfigModel(
        chartType: ChartType.pie,
        categoryField: 'customer_type',
        series: [ChartSeriesModel(field: 'fee_total')],
      );
      await _pump(tester, pie, chartDataFromSummary(pie, _figures));
      expect(find.text('Continuing'), findsOneWidget);
      expect(find.text('New'), findsOneWidget);
    });

    testWidgets('a chart drawn very small does not fail', (tester) async {
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: SizedBox(
            width: 30,
            height: 24,
            child: ChartView(config: _config, data: chartDataFromSummary(_config, _figures)),
          ),
        ),
      ));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
    });
  });

  group('the setup survives being written out and read back', () {
    test('every field of it', () {
      const config = ChartConfigModel(
        chartType: ChartType.pie,
        title: 'Share of fees',
        categoryField: 'customer_type',
        series: [ChartSeriesModel(field: 'fee_total', label: 'Fees', color: '#FF0000')],
        showValues: false,
        showLegend: false,
        showPercentages: true,
      );
      final back = ChartConfigModel.fromJson(config.toJson());

      expect(back.chartType, ChartType.pie);
      expect(back.title, 'Share of fees');
      expect(back.categoryField, 'customer_type');
      expect(back.series.single.field, 'fee_total');
      expect(back.series.single.label, 'Fees');
      expect(back.series.single.color, '#FF0000');
      expect(back.showValues, isFalse);
      expect(back.showLegend, isFalse);
      expect(back.showPercentages, isTrue);
    });

    test('a chart object keeps its setup through the layout JSON', () {
      const object = LayoutObjectModel(
        id: 'chart_1',
        type: 'chart',
        x: 10,
        y: 10,
        width: 300,
        height: 200,
        chartConfig: {
          'chart_type': 'line',
          'category_field': 'city',
          'series': [
            {'field': 'fee_total'}
          ],
        },
      );
      final layout = LayoutDefinitionModel(
        id: 'l', name: 'l', tableOccurrence: 'customers', objects: const [object]);

      final back = LayoutDefinitionModel.fromJson(layout.toJson());
      final chart = back.objects.single.chart;
      expect(chart.chartType, ChartType.line);
      expect(chart.categoryField, 'city');
      expect(chart.series.single.field, 'fee_total');
      expect(chart.isBound, isTrue);
    });

    test('an unset chart knows it draws nothing', () {
      expect(const ChartConfigModel().isBound, isFalse);
      expect(
        const ChartConfigModel(categoryField: 'city').isBound,
        isFalse,
        reason: 'a category with no series is not a chart',
      );
    });
  });
}
