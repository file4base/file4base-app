// #32 — a report lays the found set out as bands: a sub-summary before (or
// after) the records of each group, and grand summaries around the lot.

import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/report_view.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';

/// A report layout: header, optional leading sub-summary, body, optional
/// trailing sub-summary, optional trailing grand summary, footer.
LayoutDefinitionModel _layout({
  String? leadingBreak,
  String? trailingBreak,
  bool grandLeading = false,
  bool grandTrailing = false,
  List<String> extraLeadingBreaks = const [],
}) {
  return LayoutDefinitionModel(
    id: 'report',
    name: 'Annual Fee Report',
    tableOccurrence: 'customers',
    parts: [
      const LayoutPartModel(id: 'header', type: LayoutPartType.header, height: 40),
      if (grandLeading)
        const LayoutPartModel(
            id: 'grand_lead', type: LayoutPartType.leadingGrandSummary, height: 30),
      if (leadingBreak != null)
        LayoutPartModel(
            id: 'sub_lead', type: LayoutPartType.subSummary, height: 24, breakField: leadingBreak),
      for (final (i, field) in extraLeadingBreaks.indexed)
        LayoutPartModel(
            id: 'sub_lead_$i', type: LayoutPartType.subSummary, height: 24, breakField: field),
      const LayoutPartModel(id: 'body', type: LayoutPartType.body, height: 24),
      if (trailingBreak != null)
        LayoutPartModel(
            id: 'sub_trail', type: LayoutPartType.subSummary, height: 24, breakField: trailingBreak),
      if (grandTrailing)
        const LayoutPartModel(
            id: 'grand_trail', type: LayoutPartType.trailingGrandSummary, height: 30),
      const LayoutPartModel(id: 'footer', type: LayoutPartType.footer, height: 30),
    ],
  );
}

final _records = [
  {'id': '1', 'customer_type': 'Continuing', 'city': 'Dublin', 'fee_paid': '100'},
  {'id': '2', 'customer_type': 'Continuing', 'city': 'London', 'fee_paid': '100'},
  {'id': '3', 'customer_type': 'Continuing', 'city': 'London', 'fee_paid': '100'},
  {'id': '4', 'customer_type': 'New', 'city': 'Toronto', 'fee_paid': '200'},
  {'id': '5', 'customer_type': 'New', 'city': 'Toronto', 'fee_paid': '200'},
];

/// Figures as the server returns them: level n is grouped by the first n break
/// fields, level 0 is the grand totals.
ReportFigures _figures({List<String> groupBy = const []}) {
  const grand = SummaryResultModel(count: 5, grand: {'fee_total': '700'});
  if (groupBy.isEmpty) return {0: grand};

  if (groupBy.length == 1) {
    return {
      0: grand,
      1: const SummaryResultModel(
        count: 5,
        grand: {'fee_total': '700'},
        groupBy: ['customer_type'],
        groups: [
          SummaryGroupModel(
              values: {'customer_type': 'Continuing'}, count: 3, summaries: {'fee_total': '300'}),
          SummaryGroupModel(
              values: {'customer_type': 'New'}, count: 2, summaries: {'fee_total': '400'}),
        ],
      ),
    };
  }

  return {
    0: grand,
    1: const SummaryResultModel(
      groupBy: ['customer_type'],
      groups: [
        SummaryGroupModel(
            values: {'customer_type': 'Continuing'}, count: 3, summaries: {'fee_total': '300'}),
        SummaryGroupModel(
            values: {'customer_type': 'New'}, count: 2, summaries: {'fee_total': '400'}),
      ],
    ),
    2: const SummaryResultModel(
      groupBy: ['customer_type', 'city'],
      groups: [
        SummaryGroupModel(
            values: {'customer_type': 'Continuing', 'city': 'Dublin'},
            count: 1,
            summaries: {'fee_total': '100'}),
        SummaryGroupModel(
            values: {'customer_type': 'Continuing', 'city': 'London'},
            count: 2,
            summaries: {'fee_total': '200'}),
        SummaryGroupModel(
            values: {'customer_type': 'New', 'city': 'Toronto'},
            count: 2,
            summaries: {'fee_total': '400'}),
      ],
    ),
  };
}

/// A readable shape of the report: one line per band.
List<String> _shape(List<ReportBand> bands) => [
      for (final band in bands)
        if (band.record != null)
          'body ${band.record!['id']}'
        else
          '${band.part.id} ${band.breakValues.values.join("/")} = ${band.summaries['fee_total']}'
              .trim(),
    ];

void main() {
  test('a leading sub-summary opens each group', () {
    final bands = buildReportBands(
      layout: _layout(leadingBreak: 'customer_type'),
      records: _records,
      figures: _figures(groupBy: ['customer_type']),
      specs: const {},
    );

    expect(_shape(bands), [
      'sub_lead Continuing = 300',
      'body 1',
      'body 2',
      'body 3',
      'sub_lead New = 400',
      'body 4',
      'body 5',
    ]);
  });

  test('a trailing sub-summary closes each group, including the last', () {
    final bands = buildReportBands(
      layout: _layout(trailingBreak: 'customer_type'),
      records: _records,
      figures: _figures(groupBy: ['customer_type']),
      specs: const {},
    );

    expect(_shape(bands), [
      'body 1',
      'body 2',
      'body 3',
      'sub_trail Continuing = 300',
      'body 4',
      'body 5',
      'sub_trail New = 400',
    ], reason: 'the last group has nothing after it, so it is closed at the end');
  });

  test('a trailing sub-summary totals the group that ended, not the one starting', () {
    final bands = buildReportBands(
      layout: _layout(trailingBreak: 'customer_type'),
      records: _records,
      figures: _figures(groupBy: ['customer_type']),
      specs: const {},
    );
    final firstTrailing = bands.firstWhere((b) => b.part.id == 'sub_trail');
    expect(firstTrailing.breakValues['customer_type'], 'Continuing');
    expect(firstTrailing.summaries['fee_total'], '300');
    expect(firstTrailing.count, 3);
  });

  test('grand summaries wrap the whole report', () {
    final bands = buildReportBands(
      layout: _layout(
        leadingBreak: 'customer_type',
        grandLeading: true,
        grandTrailing: true,
      ),
      records: _records,
      figures: _figures(groupBy: ['customer_type']),
      specs: const {},
    );

    expect(bands.first.part.id, 'grand_lead');
    expect(bands.first.summaries['fee_total'], '700');
    expect(bands.last.part.id, 'grand_trail');
    expect(bands.last.summaries['fee_total'], '700');
    expect(bands.last.count, 5);
  });

  test('two break fields nest, outer first and closing inner first', () {
    final layout = _layout(leadingBreak: 'customer_type', extraLeadingBreaks: ['city']);
    expect(layout.breakFields, ['customer_type', 'city']);

    final bands = buildReportBands(
      layout: layout,
      records: _records,
      figures: _figures(groupBy: ['customer_type', 'city']),
      specs: const {},
    );

    expect(_shape(bands), [
      'sub_lead Continuing = 300',
      'sub_lead_0 Continuing/Dublin = 100',
      'body 1',
      'sub_lead_0 Continuing/London = 200',
      'body 2',
      'body 3',
      'sub_lead New = 400',
      'sub_lead_0 New/Toronto = 400',
      'body 4',
      'body 5',
    ]);
  });

  test('an empty found set draws the grand summaries and no groups', () {
    final bands = buildReportBands(
      layout: _layout(leadingBreak: 'customer_type', grandTrailing: true),
      records: const [],
      figures: _figures(groupBy: ['customer_type']),
      specs: const {},
    );
    expect(_shape(bands), ['grand_trail  = 700']);
  });

  test('a layout with no summary parts is just its records', () {
    final bands = buildReportBands(
      layout: _layout(),
      records: _records,
      figures: _figures(),
      specs: const {},
    );
    expect(_shape(bands), ['body 1', 'body 2', 'body 3', 'body 4', 'body 5']);
  });

  test('a sub-summary whose break field is not set summarizes nothing', () {
    final layout = LayoutDefinitionModel(
      id: 'r',
      name: 'r',
      tableOccurrence: 'customers',
      parts: const [
        LayoutPartModel(id: 'sub', type: LayoutPartType.subSummary, height: 24),
        LayoutPartModel(id: 'body', type: LayoutPartType.body, height: 24),
      ],
    );
    expect(layout.breakFields, isEmpty);

    final bands = buildReportBands(
      layout: layout,
      records: _records,
      figures: _figures(),
      specs: const {},
    );
    expect(_shape(bands), ['body 1', 'body 2', 'body 3', 'body 4', 'body 5'],
        reason: 'it draws nothing rather than a band per record');
  });

  group('running totals', () {
    test('a running summary adds up down the records', () {
      final bands = buildReportBands(
        layout: _layout(),
        records: _records,
        figures: _figures(),
        specs: const {
          'fee_running': SummarySpecModel(
              summaryType: SummaryType.total, field: 'fee_paid', running: true),
        },
      );

      expect([for (final b in bands) b.summaries['fee_running']],
          ['100', '200', '300', '500', '700']);
    });

    test('a summary that is not running is left to the group figures', () {
      final bands = buildReportBands(
        layout: _layout(),
        records: _records,
        figures: _figures(),
        specs: const {
          'fee_total': SummarySpecModel(summaryType: SummaryType.total, field: 'fee_paid'),
        },
      );
      expect(bands.first.summaries.containsKey('fee_total'), isFalse);
    });
  });

  group('the order a report needs', () {
    final layout = _layout(leadingBreak: 'customer_type', extraLeadingBreaks: ['city']);

    test('sorted by the break fields, outermost first, is right', () {
      expect(reportSortMismatch(layout, ['customer_type', 'city']), isFalse);
      expect(reportSortMismatch(layout, ['customer_type', 'city', 'last_name']), isFalse,
          reason: 'sorting further within a group does not break it');
    });

    test('a different order, or too few fields, is wrong', () {
      expect(reportSortMismatch(layout, ['city', 'customer_type']), isTrue);
      expect(reportSortMismatch(layout, ['customer_type']), isTrue);
      expect(reportSortMismatch(layout, const []), isTrue);
    });

    test('a layout that groups by nothing needs no order', () {
      expect(reportSortMismatch(_layout(), const []), isFalse);
    });
  });

  group('how a figure is shown', () {
    test('a whole number keeps no decimals', () {
      expect(formatSummaryValue('700', SummaryType.total), '700');
      expect(formatSummaryValue('700.0000', SummaryType.total), '700');
    });

    test('an average is shown to two decimals, not to the engine\'s precision', () {
      expect(formatSummaryValue('133.3333333333333333', SummaryType.average), '133.33');
      expect(formatSummaryValue('12.5', SummaryType.standardDeviation), '12.50');
    });

    test('a fraction of the total is a percentage', () {
      expect(formatSummaryValue('0.5', SummaryType.fractionOfTotal), '50.0%');
      expect(formatSummaryValue('1', SummaryType.fractionOfTotal), '100.0%');
    });

    test('nothing is shown as nothing', () {
      expect(formatSummaryValue(null, SummaryType.total), '');
      expect(formatSummaryValue('', SummaryType.total), '');
    });

    test('a figure that is not a number is passed through', () {
      expect(formatSummaryValue('Smith', SummaryType.maximum), 'Smith');
    });
  });
}
