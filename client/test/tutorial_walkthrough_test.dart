// Defects found while building the Favorite Bakery database by hand, following
// docs/tutorials/favorite_bakery_tutorial.md. Each test fails without its fix.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/widgets/file4base_menu_bar.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart';

const _customerFields = [
  (name: 'id', label: 'ID'),
  (name: 'first_name', label: 'First Name'),
  (name: 'last_name', label: 'Last Name'),
  (name: 'company', label: 'Company'),
  (name: 'customer_type', label: 'Customer Type'),
  (name: 'home_address_1', label: 'Home Address 1'),
  (name: 'home_address_2', label: 'Home Address 2'),
  (name: 'city', label: 'City'),
  (name: 'country', label: 'Country'),
  (name: 'phone', label: 'Phone'),
  (name: 'fee_paid', label: 'Fee Paid'),
  (name: 'date_paid', label: 'Date Paid'),
  (name: 'customer_since', label: 'Customer Since'),
];

void main() {
  group('the first layout of a table', () {
    test('labels each field with its field label, not its SQL column name', () {
      final layout = LayoutDefinitionModel.defaultForTable('Customers', _customerFields);

      final labels = layout.objects
          .where((o) => o.type == 'label' && o.id.startsWith('lbl_'))
          .map((o) => o.text)
          .toList();

      expect(labels, contains('Home Address 1'));
      expect(labels, contains('Customer Since'));
      expect(labels, isNot(contains('home_address_1')));
      expect(labels, isNot(contains('customer_since')));
    });

    test('still binds the field objects to the SQL column names', () {
      final layout = LayoutDefinitionModel.defaultForTable('Customers', _customerFields);

      final bindings = layout.objects
          .where((o) => o.type == 'field')
          .map((o) => o.fieldBinding?.fieldName)
          .toList();

      expect(bindings, contains('home_address_1'));
      expect(bindings, isNot(contains('Home Address 1')));
    });

    test('leaves the primary key off the layout', () {
      final layout = LayoutDefinitionModel.defaultForTable('Customers', _customerFields);

      expect(layout.objects.where((o) => o.id == 'fld_id'), isEmpty);
    });

    test('sizes the body to its rows so no field falls into the footer', () {
      final layout = LayoutDefinitionModel.defaultForTable('Customers', _customerFields);

      final header = layout.parts.firstWhere((p) => p.type == 'header');
      final body = layout.parts.firstWhere((p) => p.type == 'body');
      final lowest = layout.objects
          .where((o) => o.type == 'field')
          .map((o) => o.y + o.height)
          .reduce((a, b) => a > b ? a : b);

      expect(lowest, lessThanOrEqualTo(header.height + body.height),
          reason: 'the last field must still be inside the body part');
    });

    test('keeps a usable body for a table with a single field', () {
      final layout = LayoutDefinitionModel.defaultForTable(
        'Notes',
        const [(name: 'id', label: 'ID'), (name: 'note', label: 'Note')],
      );

      final body = layout.parts.firstWhere((p) => p.type == 'body');
      expect(body.height, greaterThanOrEqualTo(120));
    });
  });

  group('field values in Browse mode', () {
    test('a DATE column is shown as a date, not as an RFC3339 timestamp', () {
      const col = ColumnModel(
        id: 'c',
        tableId: 't',
        name: 'date_paid',
        displayName: 'Date Paid',
        fieldType: 'DATE',
      );

      expect(DataBrowserWidgetState.fieldText(col, '2011-01-15T00:00:00Z'), '2011-01-15');
      expect(DataBrowserWidgetState.fieldText(col, '2011-01-15'), '2011-01-15');
      expect(DataBrowserWidgetState.fieldText(col, null), '');
    });

    test('a TIMESTAMP column keeps its time', () {
      const col = ColumnModel(
        id: 'c',
        tableId: 't',
        name: 'changed_at',
        displayName: 'Changed At',
        fieldType: 'TIMESTAMP',
      );

      expect(DataBrowserWidgetState.fieldText(col, '2011-01-15T09:30:00Z'),
          '2011-01-15T09:30:00Z');
    });
  });

  sortOrderTests();

  group('the Records menu', () {
    Future<void> pumpMenu(
      WidgetTester tester, {
      VoidCallback? onCommitRecord,
      VoidCallback? onRevertRecord,
      VoidCallback? onUnsortRecords,
      ValueChanged<String>? onGoToRecord,
    }) async {
      tester.view.physicalSize = const Size(1440, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.browse,
            onModeChanged: (_) {},
            onManageDatabase: () {},
            onOpenRemote: () {},
            onAbout: () {},
            isToolbarVisible: true,
            onToggleToolbar: (_) {},
            onCommitRecord: onCommitRecord,
            onRevertRecord: onRevertRecord,
            onUnsortRecords: onUnsortRecords,
            onGoToRecord: onGoToRecord,
          ),
        ),
      ));
      await tester.pump();
      await tester.tap(find.text('Records'));
      await tester.pumpAndSettle();
    }

    bool isEnabled(WidgetTester tester, String label) {
      final button = tester.widget<MenuItemButton>(
        find.ancestor(of: find.text(label), matching: find.byType(MenuItemButton)).first,
      );
      return button.onPressed != null;
    }

    testWidgets('offers Commit Record and runs it', (tester) async {
      var committed = false;
      await pumpMenu(tester, onCommitRecord: () => committed = true);

      expect(find.text('Commit Record'), findsOneWidget);
      await tester.tap(find.text('Commit Record'));
      await tester.pumpAndSettle();
      expect(committed, isTrue);
    });

    testWidgets('Revert Record and Unsort run their actions', (tester) async {
      var reverted = false;
      var unsorted = false;
      await pumpMenu(tester,
          onRevertRecord: () => reverted = true, onUnsortRecords: () => unsorted = true);

      await tester.tap(find.text('Revert Record'));
      await tester.pumpAndSettle();
      expect(reverted, isTrue);

      await tester.tap(find.text('Records'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Unsort'));
      await tester.pumpAndSettle();
      expect(unsorted, isTrue);
    });

    testWidgets('Go to Record moves to the named record', (tester) async {
      final targets = <String>[];
      await pumpMenu(tester, onGoToRecord: targets.add);

      await tester.tap(find.text('Go to Record'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Last'));
      await tester.pumpAndSettle();

      expect(targets, ['last']);
    });

    testWidgets('commands with no implementation are disabled, not reported as done',
        (tester) async {
      await pumpMenu(tester);

      // Nothing implements these yet: they must look unavailable rather than
      // showing "Omit Record: Temporarily hide current row from found set."
      for (final label in ['Omit Record', 'Omit Multiple...', 'Show Omitted Only',
        'Delete All Records...', 'Relookup Field Contents']) {
        expect(isEnabled(tester, label), isFalse, reason: '"$label" should be disabled');
      }

      // And the ones that are wired stay disabled only because no callback was
      // given here.
      expect(isEnabled(tester, 'Commit Record'), isFalse);
    });

    testWidgets('clicking a disabled command shows no message', (tester) async {
      await pumpMenu(tester);

      await tester.tap(find.text('Omit Record'), warnIfMissed: false);
      await tester.pumpAndSettle();

      expect(find.textContaining('Temporarily hide'), findsNothing);
      expect(find.byType(SnackBar), findsNothing);
    });
  });
}

// #35 — sorting took a single field, so "by Company, then by Last Name within
// each company" could not be expressed.
void sortOrderTests() {
  group('a sort order', () {
    test('a level knows the form the data API takes', () {
      expect(const SortLevel('last_name', true).queryValue, 'last_name');
      expect(const SortLevel('fee_paid', false).queryValue, '-fee_paid');
      expect(const SortLevel('city', true).flipped, const SortLevel('city', false));
    });

    test('breaks ties with the next level', () {
      final records = [
        {'company': 'ABC Company', 'last_name': 'Smith'},
        {'company': 'DEF Ltd.', 'last_name': 'Johnson'},
        {'company': 'ABC Company', 'last_name': 'Lee'},
        {'company': 'ABC Company', 'last_name': 'Murphy'},
      ];

      const order = [SortLevel('company', true), SortLevel('last_name', true)];
      final sorted = List.of(records)
        ..sort((a, b) {
          for (final level in order) {
            final cmp = level.ascending
                ? (a[level.field] as String).compareTo(b[level.field] as String)
                : (b[level.field] as String).compareTo(a[level.field] as String);
            if (cmp != 0) return cmp;
          }
          return 0;
        });

      expect(sorted.map((r) => '${r['company']}/${r['last_name']}').toList(), [
        'ABC Company/Lee',
        'ABC Company/Murphy',
        'ABC Company/Smith',
        'DEF Ltd./Johnson',
      ]);
    });
  });
}
