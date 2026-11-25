import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/schema_manager/widgets/relationship_graph_widget.dart';
import 'package:file4base_client/features/schema_manager/widgets/specify_relationship_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

// Multi-criteria relationships: synthetic Accounts and Visits tables.

const _tables = [
  TableModel(
    id: 'tbl-a',
    name: 'accounts',
    displayName: 'Accounts',
    columns: [
      ColumnModel(id: 'a-code', tableId: 'tbl-a', name: 'code', displayName: 'Code', fieldType: 'VARCHAR', isNullable: false),
      ColumnModel(id: 'a-region', tableId: 'tbl-a', name: 'region', displayName: 'Region', fieldType: 'VARCHAR'),
      ColumnModel(id: 'a-since', tableId: 'tbl-a', name: 'since', displayName: 'Since', fieldType: 'INTEGER'),
    ],
  ),
  TableModel(
    id: 'tbl-v',
    name: 'visits',
    displayName: 'Visits',
    columns: [
      ColumnModel(id: 'v-code', tableId: 'tbl-v', name: 'account_code', displayName: 'Account Code', fieldType: 'VARCHAR', isNullable: false),
      ColumnModel(id: 'v-area', tableId: 'tbl-v', name: 'area', displayName: 'Area', fieldType: 'VARCHAR'),
      ColumnModel(id: 'v-day', tableId: 'tbl-v', name: 'day', displayName: 'Day', fieldType: 'INTEGER'),
    ],
  ),
];

const _occurrences = [
  TableOccurrenceModel(id: 'occ-a', baseTableId: 'tbl-a', name: 'Accounts', xPos: 80, yPos: 80),
  TableOccurrenceModel(id: 'occ-v', baseTableId: 'tbl-v', name: 'Visits', xPos: 420, yPos: 80),
];

const _threeCriteria = RelationshipModel(
  id: 'rel-1',
  name: 'Accounts_to_Visits',
  leftOccurrenceId: 'occ-a',
  leftColumnId: 'a-code',
  rightOccurrenceId: 'occ-v',
  rightColumnId: 'v-code',
  predicates: [
    RelationshipPredicateModel(leftColumnId: 'a-code', rightColumnId: 'v-code'),
    RelationshipPredicateModel(leftColumnId: 'a-region', rightColumnId: 'v-area', operator: '≠'),
    RelationshipPredicateModel(leftColumnId: 'a-since', rightColumnId: 'v-day', operator: '<'),
  ],
);

/// Opens the dialog from a button and keeps what it returns.
class _Harness {
  Map<String, dynamic>? result;

  Future<void> open(WidgetTester tester, {RelationshipModel? initial}) async {
    await tester.binding.setSurfaceSize(const Size(1400, 1200));
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => ElevatedButton(
              onPressed: () async {
                result = await SpecifyRelationshipDialog.show(
                  context,
                  occurrences: _occurrences,
                  tables: _tables,
                  initialRelationship: initial,
                  preselectedLeftOccurrenceId: 'occ-a',
                  preselectedRightOccurrenceId: 'occ-v',
                );
              },
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
  }

  List<RelationshipPredicateModel> get predicates =>
      result!['predicates'] as List<RelationshipPredicateModel>;
}

Future<void> _pickOperator(WidgetTester tester, String op) async {
  await tester.tap(find.byType(DropdownButton<String>));
  await tester.pumpAndSettle();
  await tester.tap(find.text(op).last);
  await tester.pumpAndSettle();
}

ElevatedButton _okButton(WidgetTester tester) =>
    tester.widget<ElevatedButton>(find.widgetWithText(ElevatedButton, 'OK'));

void main() {
  group('RelationshipModel criteria', () {
    test('a relationship without a criteria list reads its own pair as the only criterion', () {
      final rel = RelationshipModel.fromJson({
        'id': 'rel-1',
        'left_occurrence_id': 'occ-a',
        'left_column_id': 'a-code',
        'right_occurrence_id': 'occ-v',
        'right_column_id': 'v-code',
        'operator': '≠',
      });
      expect(rel.predicates, [
        const RelationshipPredicateModel(leftColumnId: 'a-code', rightColumnId: 'v-code', operator: '≠'),
      ]);
    });

    test('every criterion is read and written in order', () {
      final rel = RelationshipModel.fromJson(_threeCriteria.toJson());
      expect(rel.predicates.map((p) => p.operator), ['=', '≠', '<']);
      expect(rel.predicates[2].leftColumnId, 'a-since');
      expect(_threeCriteria.toJson()['predicates'], [
        {'left_column_id': 'a-code', 'operator': '=', 'right_column_id': 'v-code'},
        {'left_column_id': 'a-region', 'operator': '≠', 'right_column_id': 'v-area'},
        {'left_column_id': 'a-since', 'operator': '<', 'right_column_id': 'v-day'},
      ]);
    });

    test('the field names the server returns are kept', () {
      final p = RelationshipPredicateModel.fromJson(const {
        'left_column_id': 'a-code',
        'left_column_name': 'code',
        'operator': '=',
        'right_column_id': 'v-code',
        'right_column_name': 'account_code',
      });
      expect(p.leftColumnName, 'code');
      expect(p.rightColumnName, 'account_code');
    });
  });

  group('SpecifyRelationshipDialog criteria', () {
    testWidgets('starts with one criterion from the selected fields', (tester) async {
      final h = _Harness();
      await h.open(tester);

      expect(find.text('Accounts::Code'), findsOneWidget);
      expect(find.text('Visits::Account Code'), findsOneWidget);
      expect(find.text('Add'), findsOneWidget);
      expect(find.text('Change'), findsOneWidget);
      expect(find.text('Delete'), findsOneWidget);

      await tester.tap(find.text('OK'));
      await tester.pumpAndSettle();
      expect(h.predicates, [const RelationshipPredicateModel(leftColumnId: 'a-code', rightColumnId: 'v-code')]);
      expect(h.result!['left_column_id'], 'a-code');
      expect(h.result!['operator'], '=');
    });

    testWidgets('Add appends the selected fields and operator as a criterion', (tester) async {
      final h = _Harness();
      await h.open(tester);

      await tester.tap(find.text('Region'));
      await tester.tap(find.text('Area'));
      await tester.pumpAndSettle();
      await _pickOperator(tester, '≠');
      await tester.tap(find.text('Add'));
      await tester.pumpAndSettle();

      expect(find.text('Accounts::Region'), findsOneWidget);
      expect(find.text('Visits::Area'), findsOneWidget);

      await tester.tap(find.text('OK'));
      await tester.pumpAndSettle();
      expect(h.predicates, [
        const RelationshipPredicateModel(leftColumnId: 'a-code', rightColumnId: 'v-code'),
        const RelationshipPredicateModel(leftColumnId: 'a-region', rightColumnId: 'v-area', operator: '≠'),
      ]);
      // The first criterion is also the relationship's own pair.
      expect(h.result!['left_column_id'], 'a-code');
      expect(h.result!['operator'], '=');
    });

    testWidgets('a criterion already in the list is not added twice', (tester) async {
      final h = _Harness();
      await h.open(tester);

      await tester.tap(find.text('Add'));
      await tester.pumpAndSettle();
      expect(find.text('This criterion is already in the list.'), findsOneWidget);
      expect(find.byKey(const ValueKey('relationship-criterion-1')), findsNothing);
    });

    testWidgets('Change replaces the selected criterion, and OK applies a pending change', (tester) async {
      final h = _Harness();
      await h.open(tester);

      await tester.tap(find.text('Since'));
      await tester.tap(find.text('Day'));
      await tester.pumpAndSettle();
      expect(find.textContaining('click Change to apply them'), findsOneWidget);

      await tester.tap(find.text('OK'));
      await tester.pumpAndSettle();
      expect(h.predicates, [const RelationshipPredicateModel(leftColumnId: 'a-since', rightColumnId: 'v-day')]);
    });

    testWidgets('Delete removes a criterion; an empty list cannot be saved', (tester) async {
      final h = _Harness();
      await h.open(tester);

      await tester.tap(find.text('Delete'));
      await tester.pumpAndSettle();
      expect(find.text('No criteria'), findsOneWidget);
      expect(find.text('Add at least one match criterion.'), findsOneWidget);
      expect(_okButton(tester).onPressed, isNull);

      await tester.tap(find.text('Add'));
      await tester.pumpAndSettle();
      expect(_okButton(tester).onPressed, isNotNull);
    });

    testWidgets('criteria can be reordered', (tester) async {
      final h = _Harness();
      await h.open(tester, initial: _threeCriteria);

      await tester.tap(find.text('Accounts::Since'));
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('Move up'));
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('Move up'));
      await tester.pumpAndSettle();

      await tester.tap(find.text('OK'));
      await tester.pumpAndSettle();
      expect(h.predicates.map((p) => p.operator), ['<', '=', '≠']);
      expect(h.result!['left_column_id'], 'a-since');
      expect(h.result!['operator'], '<');
    });

    testWidgets('editing shows every criterion and keeps the occurrences', (tester) async {
      final h = _Harness();
      await h.open(tester, initial: _threeCriteria);

      expect(find.text('Edit Relationship'), findsOneWidget);
      expect(find.text('Accounts::Region'), findsOneWidget);
      expect(find.text('Accounts::Since'), findsOneWidget);
      for (final dropdown in tester.widgetList<DropdownButton<TableOccurrenceModel>>(
          find.byType(DropdownButton<TableOccurrenceModel>))) {
        expect(dropdown.onChanged, isNull);
      }
    });

    testWidgets('creation through criteria that are not all = is flagged', (tester) async {
      final h = _Harness();
      await h.open(tester, initial: _threeCriteria);

      await tester.tap(find.byKey(const ValueKey('relationship-right-allow-creation')));
      await tester.pumpAndSettle();
      expect(find.textContaining('only when every criterion uses ='), findsOneWidget);
    });

    testWidgets('a field cannot be matched with itself', (tester) async {
      await tester.binding.setSurfaceSize(const Size(1400, 1200));
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: SpecifyRelationshipDialog(
              occurrences: _occurrences,
              tables: _tables,
              preselectedLeftOccurrenceId: 'occ-a',
              preselectedRightOccurrenceId: 'occ-a',
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('No criteria'), findsOneWidget);
      await tester.tap(find.text('Add'));
      await tester.pumpAndSettle();
      expect(find.text('A field cannot be matched with itself.'), findsOneWidget);
    });
  });

  group('RelationshipGraphWidget criteria', () {
    testWidgets('the badge shows every criterion', (tester) async {
      await tester.binding.setSurfaceSize(const Size(1200, 800));
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: RelationshipGraphWidget(
              tables: _tables,
              occurrences: _occurrences,
              relationships: const [_threeCriteria],
              apiClient: ApiClient(baseUrl: 'http://localhost:8080'),
              onSchemaChanged: () {},
            ),
          ),
        ),
      );

      expect(find.text('= ≠ <'), findsOneWidget);
      final tooltip = tester.widget<Tooltip>(
        find.ancestor(of: find.text('= ≠ <'), matching: find.byType(Tooltip)).first,
      );
      expect(
        tooltip.message,
        'Accounts::Code = Visits::Account Code\n'
        'and Accounts::Region ≠ Visits::Area\n'
        'and Accounts::Since < Visits::Day',
      );
    });
  });
}
