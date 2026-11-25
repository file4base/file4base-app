import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/related_records.dart';
import 'package:file4base_client/features/schema_manager/widgets/specify_relationship_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

// Each side of a relationship has its own options, as in FileMaker: allow
// creation, delete related records and sort records, each acting on the
// records of the side it is set on.

const _tables = [
  TableModel(id: 'tbl-p', name: 'projects', displayName: 'Projects', columns: [
    ColumnModel(id: 'p-id', tableId: 'tbl-p', name: 'id', displayName: 'id', fieldType: 'TEXT', isPrimaryKey: true),
    ColumnModel(id: 'p-title', tableId: 'tbl-p', name: 'title', displayName: 'title', fieldType: 'TEXT'),
  ]),
  TableModel(id: 'tbl-t', name: 'tasks', displayName: 'Tasks', columns: [
    ColumnModel(id: 't-project', tableId: 'tbl-t', name: 'project_id', displayName: 'project_id', fieldType: 'TEXT'),
    ColumnModel(id: 't-due', tableId: 'tbl-t', name: 'due', displayName: 'due', fieldType: 'DATE'),
  ]),
];

const _occurrences = [
  TableOccurrenceModel(id: 'occ-p', baseTableId: 'tbl-p', name: 'Projects', xPos: 80, yPos: 80),
  TableOccurrenceModel(id: 'occ-t', baseTableId: 'tbl-t', name: 'Tasks', xPos: 420, yPos: 80),
];

const _relationship = RelationshipModel(
  id: 'rel-1',
  name: 'Projects_Tasks',
  leftOccurrenceId: 'occ-p',
  leftColumnId: 'p-id',
  rightOccurrenceId: 'occ-t',
  rightColumnId: 't-project',
  cascadeDelete: true,
  sortRelated: 'due,-project_id',
  leftAllowCreation: true,
);

class _Harness {
  Map<String, dynamic>? result;

  Future<void> open(WidgetTester tester, {RelationshipModel? initial}) async {
    await tester.binding.setSurfaceSize(const Size(1400, 1200));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => ElevatedButton(
            onPressed: () async {
              result = await SpecifyRelationshipDialog.show(
                context,
                occurrences: _occurrences,
                tables: _tables,
                initialRelationship: initial,
                preselectedLeftOccurrenceId: 'occ-p',
                preselectedRightOccurrenceId: 'occ-t',
              );
            },
            child: const Text('open'),
          ),
        ),
      ),
    ));
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
  }

  Future<void> ok(WidgetTester tester) async {
    await tester.tap(find.widgetWithText(ElevatedButton, 'OK'));
    await tester.pumpAndSettle();
  }
}

bool _checked(WidgetTester tester, String key) => tester.widget<CheckboxListTile>(find.byKey(ValueKey(key))).value!;

Finder _inColumn(String side, Finder finder) =>
    find.descendant(of: find.byKey(ValueKey('relationship-$side-options')), matching: finder);

void main() {
  testWidgets("shows each side's options in its own column, under its occurrence's name", (tester) async {
    await _Harness().open(tester);

    expect(_inColumn('left', find.text('Projects')), findsOneWidget);
    expect(_inColumn('right', find.text('Tasks')), findsOneWidget);
    expect(_inColumn('left', find.text('Allow creation of records in "Projects" via this relationship')),
        findsOneWidget);
    expect(_inColumn('left', find.text('Delete related records in "Projects" when a record is deleted in "Tasks"')),
        findsOneWidget);
    expect(_inColumn('right', find.text('Allow creation of records in "Tasks" via this relationship')),
        findsOneWidget);
    expect(_inColumn('right', find.text('Delete related records in "Tasks" when a record is deleted in "Projects"')),
        findsOneWidget);
    expect(_inColumn('right', find.text('Sort records of "Tasks"')), findsOneWidget);
  });

  testWidgets('an option set on one side is returned for that side only', (tester) async {
    final h = _Harness();
    await h.open(tester);

    await tester.tap(find.byKey(const ValueKey('relationship-left-cascade-delete')));
    await tester.tap(find.byKey(const ValueKey('relationship-right-allow-creation')));
    await tester.tap(find.byKey(const ValueKey('relationship-left-sort')));
    await tester.pump();
    await h.ok(tester);

    expect(h.result!['left_cascade_delete'], isTrue);
    expect(h.result!['cascade_delete'], isFalse, reason: 'deleting a task must not delete its project');
    expect(h.result!['allow_creation'], isTrue);
    expect(h.result!['left_allow_creation'], isFalse);
    expect(h.result!['left_sort_related'], 'asc', reason: 'turned on here, it sorts by the match field');
    expect(h.result!['sort_related'], isNull, reason: 'nothing to send for a side that had no sort');
  });

  testWidgets("editing loads each side's options and keeps a written sort", (tester) async {
    final h = _Harness();
    await h.open(tester, initial: _relationship);

    expect(_checked(tester, 'relationship-left-allow-creation'), isTrue);
    expect(_checked(tester, 'relationship-left-cascade-delete'), isFalse);
    expect(_checked(tester, 'relationship-right-allow-creation'), isFalse);
    expect(_checked(tester, 'relationship-right-cascade-delete'), isTrue);
    expect(_checked(tester, 'relationship-right-sort'), isTrue);
    expect(_inColumn('right', find.text('By due, -project_id')), findsOneWidget);

    await h.ok(tester);
    expect(h.result!['sort_related'], 'due,-project_id', reason: 'OK keeps the sort it had');
    expect(h.result!['left_allow_creation'], isTrue);
    expect(h.result!['cascade_delete'], isTrue);
  });

  testWidgets('turning a sort off clears it', (tester) async {
    final h = _Harness();
    await h.open(tester, initial: _relationship);

    await tester.tap(find.byKey(const ValueKey('relationship-right-sort')));
    await tester.pump();
    await h.ok(tester);
    expect(h.result!['sort_related'], '');
  });

  test('a portal may create records only on a side whose option allows it', () {
    final targets = reachableTargets(
      fromTable: 'tasks',
      relationships: const [_relationship],
      occurrencesById: {for (final o in _occurrences) o.id: o},
      tablesById: {for (final t in _tables) t.id: t},
    );
    expect(targets.single.occurrence.name, 'Projects');
    expect(targets.single.allowsCreation, isTrue, reason: "the left side's allow-creation");

    final fromProjects = reachableTargets(
      fromTable: 'projects',
      relationships: const [_relationship],
      occurrencesById: {for (final o in _occurrences) o.id: o},
      tablesById: {for (final t in _tables) t.id: t},
    );
    expect(fromProjects.single.allowsCreation, isFalse, reason: "the right side's is off");
  });

  test('a relationship joining an occurrence to itself is looked through by its end', () {
    const staff = TableModel(id: 'tbl-s', name: 'staff', displayName: 'Staff', columns: [
      ColumnModel(id: 's-code', tableId: 'tbl-s', name: 'code', displayName: 'code', fieldType: 'TEXT'),
      ColumnModel(id: 's-boss', tableId: 'tbl-s', name: 'boss_code', displayName: 'boss_code', fieldType: 'TEXT'),
    ]);
    const occurrence = TableOccurrenceModel(id: 'occ-s', baseTableId: 'tbl-s', name: 'Staff', xPos: 0, yPos: 0);
    const bosses = RelationshipModel(
      id: 'rel-s',
      name: 'Staff_Bosses',
      leftOccurrenceId: 'occ-s',
      leftColumnId: 's-code',
      rightOccurrenceId: 'occ-s',
      rightColumnId: 's-boss',
      leftAllowCreation: true,
    );
    final targets = reachableTargets(
      fromTable: 'staff',
      relationships: const [bosses],
      occurrencesById: const {'occ-s': occurrence},
      tablesById: const {'tbl-s': staff},
    );
    expect([for (final t in targets) t.side], ['right', 'left']);
    expect([for (final t in targets) t.allowsCreation], [false, true], reason: "each end's own option");
    expect([for (final t in targets) t.requestSide], ['right', 'left'], reason: 'the occurrence alone cannot tell them apart');
    expect(reachableTargets(
      fromTable: 'tasks',
      relationships: const [_relationship],
      occurrencesById: {for (final o in _occurrences) o.id: o},
      tablesById: {for (final t in _tables) t.id: t},
    ).single.requestSide, isNull, reason: 'two occurrences need no end');
  });

  test('the left side options travel in JSON, and default to off', () {
    final back = RelationshipModel.fromJson(_relationship.toJson());
    expect(back.leftAllowCreation, isTrue);
    expect(back.leftCascadeDelete, isFalse);
    expect(back.sortRelated, 'due,-project_id');
    final old = RelationshipModel.fromJson(const {
      'id': 'r',
      'left_occurrence_id': 'occ-p',
      'right_occurrence_id': 'occ-t',
      'allow_creation': true,
    });
    expect(old.leftAllowCreation, isFalse);
    expect(old.leftCascadeDelete, isFalse);
    expect(old.leftSortRelated, isNull);
  });
}
