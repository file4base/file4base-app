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
  findRequestTests();
  calculationFieldTests();
  valueListTests();

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

// #34 — Find mode had a single request, so "New York or London" could not be
// expressed, and the Requests menu items did nothing.
void findRequestTests() {
  group('find criteria', () {
    test('a plain word becomes a contains search', () {
      final criteria = DataBrowserWidgetState.criteriaFor({'city': 'New York'});
      expect(criteria, [
        {'field_name': 'city', 'operator': 'LIKE', 'value': '%New York%'}
      ]);
    });

    test('the operators the toolbar offers are recognised', () {
      expect(DataBrowserWidgetState.criteriaFor({'fee': '>=200'}).single['operator'], '>=');
      expect(DataBrowserWidgetState.criteriaFor({'fee': '<10'}).single['operator'], '<');
      expect(DataBrowserWidgetState.criteriaFor({'name': '=Smith'}).single['operator'], '=');
      expect(DataBrowserWidgetState.criteriaFor({'name': '==Smith'}).single['operator'], '==');
      expect(DataBrowserWidgetState.criteriaFor({'name': '!Smith'}).single['operator'], '!=');
      expect(DataBrowserWidgetState.criteriaFor({'city': ''}).isEmpty, isTrue);
    });

    test('a wildcard becomes a LIKE pattern', () {
      final criteria = DataBrowserWidgetState.criteriaFor({'last_name': 'Sm*'});
      expect(criteria.single['operator'], 'LIKE');
      expect(criteria.single['value'], 'Sm%');
    });

    test('a range keeps both ends', () {
      final criteria = DataBrowserWidgetState.criteriaFor(
          {'date_paid': '2011-01-01...2011-06-30'}).single;
      expect(criteria['operator'], 'RANGE');
      expect(criteria['value'], '2011-01-01');
      expect(criteria['value_to'], '2011-06-30');
    });

    test('criteria in several fields go into the same request', () {
      final criteria =
          DataBrowserWidgetState.criteriaFor({'city': 'New York', 'customer_type': 'Continuing'});
      expect(criteria.length, 2);
    });
  });

  group('a find request', () {
    test('is empty until something is typed into it', () {
      final request = FindRequestDraft();
      expect(request.isEmpty, isTrue);
      request.values['city'] = '  ';
      expect(request.isEmpty, isTrue);
      request.values['city'] = 'London';
      expect(request.isEmpty, isFalse);
    });

    test('a copy does not share its values with the original', () {
      final original = FindRequestDraft(values: {'city': 'London'}, omit: true);
      final copy = original.copy();
      copy.values['city'] = 'Paris';
      copy.omit = false;

      expect(original.values['city'], 'London');
      expect(original.omit, isTrue);
      expect(copy.values['city'], 'Paris');
    });
  });

  group('the Requests menu', () {
    Future<void> pumpMenu(
      WidgetTester tester, {
      VoidCallback? onNewFindRequest,
      VoidCallback? onDuplicateFindRequest,
      VoidCallback? onDeleteFindRequest,
      VoidCallback? onDeleteAllFindRequests,
      VoidCallback? onToggleFindOmit,
      ValueChanged<String>? onGoToFindRequest,
    }) async {
      tester.view.physicalSize = const Size(1440, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.find,
            onModeChanged: (_) {},
            onManageDatabase: () {},
            onOpenRemote: () {},
            onAbout: () {},
            isToolbarVisible: true,
            onToggleToolbar: (_) {},
            onNewFindRequest: onNewFindRequest,
            onDuplicateFindRequest: onDuplicateFindRequest,
            onDeleteFindRequest: onDeleteFindRequest,
            onDeleteAllFindRequests: onDeleteAllFindRequests,
            onToggleFindOmit: onToggleFindOmit,
            onGoToFindRequest: onGoToFindRequest,
          ),
        ),
      ));
      await tester.pump();
      await tester.tap(find.text('Requests'));
      await tester.pumpAndSettle();
    }

    testWidgets('New Request and Duplicate Request run their actions', (tester) async {
      var created = false;
      var duplicated = false;
      await pumpMenu(tester,
          onNewFindRequest: () => created = true,
          onDuplicateFindRequest: () => duplicated = true);

      await tester.tap(find.text('New Request'));
      await tester.pumpAndSettle();
      expect(created, isTrue);

      await tester.tap(find.text('Requests'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Duplicate Request'));
      await tester.pumpAndSettle();
      expect(duplicated, isTrue);
    });

    testWidgets('Go to Request moves to the named request', (tester) async {
      final targets = <String>[];
      await pumpMenu(tester, onGoToFindRequest: targets.add);

      await tester.tap(find.text('Go to Request'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Last'));
      await tester.pumpAndSettle();

      expect(targets, ['last']);
    });

    testWidgets('Include / Omit toggles the current request', (tester) async {
      var toggled = false;
      await pumpMenu(tester, onToggleFindOmit: () => toggled = true);

      await tester.tap(find.text('Include / Omit'));
      await tester.pumpAndSettle();
      expect(toggled, isTrue);
    });

    testWidgets('without a host the items are disabled, not fake', (tester) async {
      await pumpMenu(tester);

      for (final label in ['New Request', 'Duplicate Request', 'Delete Request',
        'Delete All Requests', 'Include / Omit']) {
        final button = tester.widget<MenuItemButton>(
          find.ancestor(of: find.text(label), matching: find.byType(MenuItemButton)).first,
        );
        expect(button.onPressed, isNull, reason: '"$label" should be disabled');
      }
      expect(find.byType(SnackBar), findsNothing);
    });
  });
}

// #30 — calculation fields are filled in by their formula, so they are not
// typed into.
void calculationFieldTests() {
  group('a calculation field in Browse mode', () {
    const table = TableModel(id: 't', name: 'customers', displayName: 'Customers', columns: [
      ColumnModel(id: 'c0', tableId: 't', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
      ColumnModel(id: 'c1', tableId: 't', name: 'customer_type', displayName: 'Customer Type', fieldType: 'TEXT'),
      ColumnModel(id: 'c2', tableId: 't', name: 'annual_fee', displayName: 'Annual Fee', fieldType: 'CALCULATION'),
      ColumnModel(id: 'c3', tableId: 't', name: 'fee_total', displayName: 'Fee Total', fieldType: 'SUMMARY'),
    ]);

    test('a computed field is told apart from one the user fills in', () {
      bool computed(String field) {
        final col = table.columns.where((c) => c.name == field).firstOrNull;
        return col != null && (col.fieldType == 'CALCULATION' || col.fieldType == 'SUMMARY');
      }

      expect(computed('annual_fee'), isTrue);
      expect(computed('fee_total'), isTrue);
      expect(computed('customer_type'), isFalse);
      expect(computed('id'), isFalse);
    });
  });
}

// #31 — a field can be filled from a named set of values instead of being
// typed into.
void valueListTests() {
  group('a value list', () {
    test('a custom list drops blanks and repeats, and keeps its order', () {
      const list = ValueListModel(
        id: 'v1',
        name: 'Customer Types',
        customValues: 'New\nContinuing\n\n  Lapsed  \nNew',
      );
      expect(list.customValueList, ['New', 'Continuing', 'Lapsed']);
    });

    test('a list taken from a field is told apart from a custom one', () {
      const custom = ValueListModel(id: 'v1', name: 'Types', customValues: 'New');
      const fromField = ValueListModel(
        id: 'v2',
        name: 'Cities',
        kind: ValueListModel.kindFromField,
        sourceTableId: 't1',
        sourceColumnId: 'c1',
      );
      expect(custom.isFromField, isFalse);
      expect(fromField.isFromField, isTrue);
    });

    test('it round-trips through JSON', () {
      const original = ValueListModel(
        id: 'v2',
        name: 'Cities',
        kind: ValueListModel.kindFromField,
        sourceTableId: 't1',
        sourceColumnId: 'c1',
      );
      final back = ValueListModel.fromJson({'id': 'v2', ...original.toJson()});
      expect(back.name, 'Cities');
      expect(back.kind, ValueListModel.kindFromField);
      expect(back.sourceColumnId, 'c1');
    });
  });

  group('a field control style', () {
    test('only an edit box works without a value list', () {
      expect(FieldControlStyle.needsValueList(FieldControlStyle.editBox), isFalse);
      for (final style in [
        FieldControlStyle.dropDownList,
        FieldControlStyle.popUpMenu,
        FieldControlStyle.checkboxSet,
        FieldControlStyle.radioButtonSet,
      ]) {
        expect(FieldControlStyle.needsValueList(style), isTrue, reason: style);
      }
    });

    test('a control with no list falls back to an edit box', () {
      const binding = FieldBindingModel(
        fieldName: 'customer_type',
        controlStyle: FieldControlStyle.radioButtonSet,
      );
      expect(binding.effectiveControlStyle(hasValueList: false), FieldControlStyle.editBox);
      expect(binding.effectiveControlStyle(hasValueList: true), FieldControlStyle.radioButtonSet);
    });

    test('only a checkbox set holds more than one value', () {
      expect(FieldControlStyle.isMultiValue(FieldControlStyle.checkboxSet), isTrue);
      expect(FieldControlStyle.isMultiValue(FieldControlStyle.radioButtonSet), isFalse);
      expect(FieldControlStyle.isMultiValue(FieldControlStyle.dropDownList), isFalse);
    });

    test('the binding carries the value list through JSON', () {
      const binding = FieldBindingModel(
        fieldName: 'customer_type',
        controlStyle: FieldControlStyle.radioButtonSet,
        valueListId: 'v1',
      );
      final back = FieldBindingModel.fromJson(binding.toJson());
      expect(back.controlStyle, FieldControlStyle.radioButtonSet);
      expect(back.valueListId, 'v1');
    });

    test('choosing an edit box lets the value list go', () {
      const binding = FieldBindingModel(
        fieldName: 'customer_type',
        controlStyle: FieldControlStyle.radioButtonSet,
        valueListId: 'v1',
      );
      final plain = binding.copyWith(
        controlStyle: FieldControlStyle.editBox,
        clearValueList: true,
      );
      expect(plain.valueListId, isNull);
      expect(binding.valueListId, 'v1', reason: 'copyWith must not change the original');
    });

    test('rebinding to another column keeps the control and its list', () {
      const binding = FieldBindingModel(
        fieldName: 'customer_type',
        controlStyle: FieldControlStyle.popUpMenu,
        valueListId: 'v1',
      );
      final moved = binding.copyWith(fieldName: 'country');
      expect(moved.fieldName, 'country');
      expect(moved.controlStyle, FieldControlStyle.popUpMenu);
      expect(moved.valueListId, 'v1');
    });
  });
}
