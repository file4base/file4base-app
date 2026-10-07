// #33 — the layout assistant makes five different layouts from the same
// ingredients. These check what comes out, without opening a dialog.

import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/features/layout_engine/models/layout_blueprint.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';

const _fields = <BlueprintField>[
  (name: 'last_name', label: 'Last Name', isSummary: false),
  (name: 'city', label: 'City', isSummary: false),
  (name: 'annual_fee', label: 'Annual Fee', isSummary: false),
];

const _summaries = <BlueprintField>[
  (name: 'fee_total', label: 'Total Annual Fees', isSummary: true),
];

LayoutBlueprint _blueprint(
  LayoutKind kind, {
  List<BlueprintField> fields = _fields,
  String? breakField,
  List<BlueprintField> summaryFields = const [],
  LabelStock? stock,
  LayoutTheme theme = LayoutTheme.enlightened,
}) =>
    LayoutBlueprint(
      name: 'Customers ${kind.label}',
      kind: kind,
      tableOccurrence: 'customers',
      fields: fields,
      theme: theme,
      breakField: breakField,
      summaryFields: summaryFields,
      stock: stock,
    );

List<String> _fieldNames(LayoutDefinitionModel layout) => [
      for (final o in layout.objects)
        if (o.type == 'field') o.fieldBinding!.fieldName,
    ];

void main() {
  group('form', () {
    final layout = _blueprint(LayoutKind.form).build();

    test('one labelled row per field', () {
      expect(_fieldNames(layout), ['last_name', 'city', 'annual_fee']);
      expect(
        [for (final o in layout.objects) if (o.type == 'label') o.text],
        ['Customers Form', 'Last Name', 'City', 'Annual Fee'],
      );
    });

    test('it shows one record at a time', () {
      expect(layout.defaultView, 'form');
      expect(layout.isReport, isFalse);
      expect(layout.isLabels, isFalse);
    });

    test('the body is sized to the rows it holds', () {
      final body = layout.parts.firstWhere((p) => p.isBody);
      expect(body.height, greaterThanOrEqualTo(120));
    });
  });

  group('list', () {
    final layout = _blueprint(LayoutKind.list).build();

    test('column headings in the header, one row of fields in the body', () {
      final header = layout.parts.firstWhere((p) => p.type == LayoutPartType.header);
      final body = layout.parts.firstWhere((p) => p.isBody);

      expect(
        [for (final o in layout.objectsIn(header)) if (o.type == 'label') o.text],
        containsAll(['Last Name', 'City', 'Annual Fee']),
      );
      expect([for (final o in layout.objectsIn(body)) o.fieldBinding?.fieldName],
          ['last_name', 'city', 'annual_fee']);
    });

    test('the columns are in the order the fields were chosen', () {
      final body = layout.parts.firstWhere((p) => p.isBody);
      final row = layout.objectsIn(body).toList()..sort((a, b) => a.x.compareTo(b.x));
      expect([for (final o in row) o.fieldBinding!.fieldName],
          ['last_name', 'city', 'annual_fee']);
    });

    test('choosing the fields in a different order moves the columns', () {
      final reordered = _blueprint(LayoutKind.list, fields: _fields.reversed.toList()).build();
      final body = reordered.parts.firstWhere((p) => p.isBody);
      final row = reordered.objectsIn(body).toList()..sort((a, b) => a.x.compareTo(b.x));
      expect([for (final o in row) o.fieldBinding!.fieldName],
          ['annual_fee', 'city', 'last_name']);
    });

    test('it shows many records', () => expect(layout.defaultView, 'list'));

    test('the columns do not get squeezed as fields are added', () {
      final wide = _blueprint(LayoutKind.list, fields: [
        for (var i = 0; i < 8; i++) (name: 'f$i', label: 'Field $i', isSummary: false),
      ]).build();
      expect(wide.width, greaterThan(_blueprint(LayoutKind.list).build().width));
    });
  });

  group('report', () {
    final layout = _blueprint(
      LayoutKind.report,
      breakField: 'customer_type',
      summaryFields: _summaries,
    ).build();

    test('it is a report that groups by the break field', () {
      expect(layout.isReport, isTrue);
      expect(layout.breakFields, ['customer_type']);
      expect(layout.requiredSortOrder, ['customer_type']);
    });

    test('a sub-summary above the body and a grand summary below it', () {
      expect(layout.leadingSubSummaries.map((p) => p.breakField), ['customer_type']);
      expect(layout.trailingSubSummaries, isEmpty);
      expect(layout.trailingGrandSummary, isNotNull);
    });

    test('the sub-summary names its group and shows the subtotal', () {
      final sub = layout.parts.firstWhere((p) => p.isSubSummary);
      final inSub = layout.objectsIn(sub);
      expect([for (final o in inSub) o.fieldBinding?.fieldName],
          containsAll(['customer_type', 'fee_total']));
    });

    test('the grand summary shows the same summary field over the lot', () {
      final grand = layout.parts.firstWhere((p) => p.isGrandSummary);
      expect([for (final o in layout.objectsIn(grand)) o.fieldBinding?.fieldName],
          contains('fee_total'));
    });

    test('a report with no summary fields still groups, it just has no totals', () {
      final plain = _blueprint(LayoutKind.report, breakField: 'city').build();
      expect(plain.isReport, isTrue);
      expect(plain.breakFields, ['city']);
      final sub = plain.parts.firstWhere((p) => p.isSubSummary);
      expect([for (final o in plain.objectsIn(sub)) o.fieldBinding?.fieldName], ['city']);
    });

    test('a subtotal lines up with the column it totals', () {
      final sub = layout.parts.firstWhere((p) => p.isSubSummary);
      final body = layout.parts.firstWhere((p) => p.isBody);
      final total = layout.objectsIn(sub).firstWhere((o) => o.fieldBinding?.fieldName == 'fee_total');
      final lastColumn = layout.objectsIn(body).reduce((a, b) => a.x > b.x ? a : b);
      expect(total.x, lastColumn.x);
    });
  });

  group('labels', () {
    final layout = _blueprint(LayoutKind.labels, stock: LabelStock.averyL7160).build();

    test('the layout is one label, the size of the stock', () {
      expect(layout.isLabels, isTrue);
      expect(layout.width, closeTo(LabelStock.averyL7160.labelWidthPt, 0.01));
      expect(layout.height, closeTo(LabelStock.averyL7160.labelHeightPt, 0.01));
    });

    test('a label is all body: no header, no footer', () {
      expect(layout.parts.map((p) => p.type), [LayoutPartType.body]);
    });

    test('the fields are merge text, so an empty line collapses', () {
      final text = layout.objects.single;
      expect(text.type, 'label');
      expect(text.text, '{{last_name}}\n{{city}}\n{{annual_fee}}');
    });

    test('the stock survives being written out and read back', () {
      final back = LayoutDefinitionModel.fromJson(layout.toJson());
      expect(back.isLabels, isTrue);
      final stock = LabelStock.fromJson(back.labelStock)!;
      expect(stock.id, 'avery_l7160');
      expect(stock.across, 3);
      expect(stock.down, 7);
      expect(stock.perSheet, 21);
    });

    test('a custom size is fitted to an A4 sheet', () {
      final custom = LabelStock.custom(widthMm: 90, heightMm: 50);
      expect(custom.across, 2, reason: '210mm less margins fits two 90mm labels');
      expect(custom.down, 5, reason: '297mm less margins fits five 50mm labels');
      expect(custom.perSheet, 10);
    });

    test('a custom size bigger than the page still fits one', () {
      final huge = LabelStock.custom(widthMm: 400, heightMm: 400);
      expect(huge.across, 1);
      expect(huge.down, 1);
    });
  });

  group('blank', () {
    final layout = _blueprint(LayoutKind.blank).build();

    test('the three bands and nothing in them', () {
      expect(layout.parts.map((p) => p.type),
          [LayoutPartType.header, LayoutPartType.body, LayoutPartType.footer]);
      expect(layout.objects, isEmpty);
    });
  });

  group('theme', () {
    test('it is stored on the layout', () {
      final layout = _blueprint(LayoutKind.list, theme: LayoutTheme.coolGrey).build();
      expect(layout.theme, 'Cool Grey');
    });

    test('it changes how the generated objects look', () {
      final boxed = _blueprint(LayoutKind.form, theme: LayoutTheme.enlightened).build();
      final bare = _blueprint(LayoutKind.form, theme: LayoutTheme.minimal).build();

      final boxedField = boxed.objects.firstWhere((o) => o.type == 'field');
      final bareField = bare.objects.firstWhere((o) => o.type == 'field');

      expect(boxedField.style.borderColor, isNotNull);
      expect(bareField.style.borderColor, isNull, reason: 'Minimal draws no field boxes');
    });

    test('an unknown theme falls back to the default rather than failing', () {
      expect(LayoutTheme.byId('no such theme').id, 'Enlightened');
      expect(LayoutTheme.byId(null).id, 'Enlightened');
    });
  });

  group('what each kind needs to be asked', () {
    test('only a report needs a break field', () {
      expect(LayoutKind.report.needsBreakField, isTrue);
      for (final kind in [LayoutKind.form, LayoutKind.list, LayoutKind.labels, LayoutKind.blank]) {
        expect(kind.needsBreakField, isFalse, reason: kind.label);
      }
    });

    test('only labels need stock', () {
      expect(LayoutKind.labels.needsLabelStock, isTrue);
      expect(LayoutKind.list.needsLabelStock, isFalse);
    });

    test('only a blank layout needs no fields', () {
      expect(LayoutKind.blank.needsFields, isFalse);
      expect(LayoutKind.form.needsFields, isTrue);
    });
  });

  test('a layout made with no fields at all still works', () {
    for (final kind in LayoutKind.values) {
      final layout = _blueprint(kind, fields: const [], breakField: 'city').build();
      expect(layout.parts, isNotEmpty, reason: kind.label);
    }
  });
}
