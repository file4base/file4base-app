// #33 — a labels layout is one label; the sheet lays them out across and down
// the page, one record each.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/features/layout_engine/label_sheet_view.dart';
import 'package:file4base_client/features/layout_engine/models/layout_blueprint.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';

final _stock = LabelStock.averyL7160; // 3 across, 7 down = 21 to a sheet

LayoutDefinitionModel _labelLayout() => LayoutBlueprint(
      name: 'Mailing Labels',
      kind: LayoutKind.labels,
      tableOccurrence: 'customers',
      stock: _stock,
      fields: const [
        (name: 'last_name', label: 'Last Name', isSummary: false),
        (name: 'home_address_1', label: 'Home Address 1', isSummary: false),
        (name: 'home_address_2', label: 'Home Address 2', isSummary: false),
        (name: 'city', label: 'City', isSummary: false),
      ],
    ).build();

List<Map<String, dynamic>> _records(int n) => [
      for (var i = 0; i < n; i++)
        {
          'id': '$i',
          'last_name': 'Customer $i',
          'home_address_1': '$i Main Street',
          // Every other one has no second address line.
          'home_address_2': i.isEven ? '' : 'Flat $i',
          'city': 'Paris',
        },
    ];

Future<void> _pump(WidgetTester tester, List<Map<String, dynamic>> records) async {
  tester.view.physicalSize = const Size(1400, 1600);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);

  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: SingleChildScrollView(
        child: LabelSheetView(layout: _labelLayout(), stock: _stock, records: records),
      ),
    ),
  ));
  await tester.pumpAndSettle();
}

void main() {
  group('pagination', () {
    test('the records are split into sheets', () {
      final pages = LabelSheetView.paginate(_records(50), _stock);
      expect(pages.length, 3, reason: '21 to a sheet');
      expect(pages[0].length, 21);
      expect(pages[1].length, 21);
      expect(pages[2].length, 8);
    });

    test('exactly one sheet fills one sheet', () {
      expect(LabelSheetView.paginate(_records(21), _stock).length, 1);
    });

    test('no records is no sheets', () {
      expect(LabelSheetView.paginate(const [], _stock), isEmpty);
    });
  });

  testWidgets('a sheet draws one label per record, up to what fits', (tester) async {
    await _pump(tester, _records(5));
    expect(find.byKey(const ValueKey('label-sheet')), findsOneWidget);
    for (var i = 0; i < 5; i++) {
      expect(find.byKey(ValueKey('label-$i')), findsOneWidget);
    }
    expect(find.byKey(const ValueKey('label-5')), findsNothing);
  });

  testWidgets('a label whose second address line is empty loses that line', (tester) async {
    await _pump(tester, _records(2));

    // Record 0 has no second line; record 1 does.
    expect(find.text('Customer 0\n0 Main Street\nParis'), findsOneWidget);
    expect(find.text('Customer 1\n1 Main Street\nFlat 1\nParis'), findsOneWidget);
  });

  testWidgets('the labels sit in a grid across and down', (tester) async {
    await _pump(tester, _records(7));

    Offset at(int i) => tester.getTopLeft(find.byKey(ValueKey('label-$i')));

    // Three across, then the next row.
    expect(at(1).dx, greaterThan(at(0).dx));
    expect(at(1).dy, at(0).dy);
    expect(at(2).dx, greaterThan(at(1).dx));
    expect(at(3).dx, at(0).dx, reason: 'the fourth label starts the second row');
    expect(at(3).dy, greaterThan(at(0).dy));
  });

  testWidgets('a label is the size of the stock', (tester) async {
    await _pump(tester, _records(1));
    final size = tester.getSize(find.byKey(const ValueKey('label-0')));
    expect(size.width, closeTo(_stock.labelWidthPt, 0.01));
    expect(size.height, closeTo(_stock.labelHeightPt, 0.01));
  });

  testWidgets('a sheet with no records draws nothing but the sheet', (tester) async {
    await _pump(tester, const []);
    expect(find.byKey(const ValueKey('label-sheet')), findsOneWidget);
    expect(find.byKey(const ValueKey('label-0')), findsNothing);
  });
}
