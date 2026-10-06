import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/models/page_setup_model.dart';
import 'package:file4base_client/core/widgets/file4base_status_sidebar.dart';
import 'package:file4base_client/main.dart' show OperationalMode;

final _table = TableModel(id: 't1', name: 'contacts', displayName: 'Contacts', columns: const []);
final _layouts = [
  LayoutModel(id: 'l1', name: 'Contacts Form', tableOccurrenceId: 'o1', definition: const {}),
];

Future<_Probe> _pumpSidebar(WidgetTester tester, {bool omit = false}) async {
  tester.view.physicalSize = const Size(900, 700);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final probe = _Probe();
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: Row(children: [
        File4BaseStatusSidebar(
          layouts: _layouts,
          selectedLayout: _layouts.first,
          onLayoutSelected: (_) {},
          tables: [_table],
          selectedTable: _table,
          onTableSelected: (_) {},
          mode: OperationalMode.find,
          currentRecordIndex: 0,
          totalRecords: 3,
          onPreviousRecord: () {},
          onNextRecord: () {},
          onGoToRecord: (_) {},
          onManageDatabase: () {},
          isFindOmit: omit,
          onToggleOmit: (v) => probe.omitToggles.add(v),
          onPerformFind: () => probe.finds++,
          onShowAllRecords: () => probe.cancels++,
        ),
        const Expanded(child: SizedBox()),
      ]),
    ),
  ));
  await tester.pumpAndSettle();
  return probe;
}

class _Probe {
  final omitToggles = <bool>[];
  int finds = 0;
  int cancels = 0;
}

void main() {
  testWidgets('the Find sidebar is a titled card aligned with the Layout card', (tester) async {
    await _pumpSidebar(tester);
    expect(tester.takeException(), isNull);

    expect(find.text('FIND'), findsOneWidget);
    // The Find panel uses the same card and margins as the Layout selector
    final layoutCard = tester.getRect(find.ancestor(of: find.text('LAYOUT'), matching: find.byType(Container)).first);
    final findCard = tester.getRect(find.ancestor(of: find.text('FIND'), matching: find.byType(Container)).first);
    expect(findCard.left, layoutCard.left);
    expect(findCard.right, layoutCard.right);
    expect(findCard.top, greaterThan(layoutCard.bottom - 1));

    // Every control sits inside the card
    for (final label in ['Omit', 'Perform Find', 'Cancel Find']) {
      final r = tester.getRect(find.text(label));
      expect(findCard.contains(r.topLeft), isTrue, reason: '$label starts inside the card');
      expect(findCard.contains(r.bottomRight), isTrue, reason: '$label ends inside the card');
    }

    // No fabricated request counter (there is only ever one find request)
    expect(find.text('Requests:'), findsNothing);
    expect(find.text('1'), findsNothing);
  });

  testWidgets('Omit toggles from the checkbox and from its label', (tester) async {
    final probe = await _pumpSidebar(tester);
    await tester.tap(find.byType(Checkbox));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Omit'));
    await tester.pumpAndSettle();
    expect(probe.omitToggles, [true, true], reason: 'the whole row is clickable');

    final checked = await _pumpSidebar(tester, omit: true);
    await tester.tap(find.text('Omit'));
    await tester.pumpAndSettle();
    expect(checked.omitToggles, [false]);
  });

  testWidgets('Perform Find and Cancel Find run their actions', (tester) async {
    final probe = await _pumpSidebar(tester);
    await tester.tap(find.text('Perform Find'));
    await tester.tap(find.text('Cancel Find'));
    await tester.pumpAndSettle();
    expect(probe.finds, 1);
    expect(probe.cancels, 1);
  });

  testWidgets('the Preview sidebar reports the real paper, not fixed numbers', (tester) async {
    tester.view.physicalSize = const Size(900, 700);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    var setupOpened = 0;
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: Row(children: [
          File4BaseStatusSidebar(
            layouts: _layouts,
            selectedLayout: _layouts.first,
            onLayoutSelected: (_) {},
            tables: [_table],
            selectedTable: _table,
            onTableSelected: (_) {},
            mode: OperationalMode.preview,
            currentRecordIndex: 0,
            totalRecords: 3,
            onPreviousRecord: () {},
            onNextRecord: () {},
            onGoToRecord: (_) {},
            onManageDatabase: () {},
            pageSetup: const PageSetupModel(
              paperSizeName: 'US Letter',
              paperWidthMm: 215.9,
              paperHeightMm: 279.4,
              isLandscape: true,
              marginTopMm: 12.7,
              marginBottomMm: 12.7,
              marginLeftMm: 20,
              marginRightMm: 20,
            ),
            onPageSetup: () => setupOpened++,
          ),
          const Expanded(child: SizedBox()),
        ]),
      ),
    ));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);

    // Values follow Page Setup: landscape US Letter minus its margins
    expect(find.text('US Letter · Landscape'), findsOneWidget);
    expect(find.text('239.4 × 190.5 mm'), findsOneWidget);
    expect(find.text('T 12.7  B 12.7\nL 20  R 20'), findsOneWidget);

    // The fixed values the panel used to show are gone
    expect(find.text('Page: 1 of 1'), findsNothing);
    expect(find.text('Margins: 0.5 in'), findsNothing);

    await tester.tap(find.text('Page Setup...'));
    await tester.pumpAndSettle();
    expect(setupOpened, 1);
  });
}
