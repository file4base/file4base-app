// A relationship joining an occurrence to itself has that occurrence at both
// ends, so a portal (or a related field) names the end it shows (#167):
// Portal Setup offers the choice for such a relationship only, the layout
// keeps it, and Browse mode asks the server for that end.

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/related_records.dart';
import 'package:file4base_client/features/layout_engine/layout_designer_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _parts = [
  LayoutPartModel(id: 'h', type: 'header', height: 40),
  LayoutPartModel(id: 'b', type: 'body', height: 400),
];

const _tasks = TableModel(id: 'tbl-t', name: 'tasks', displayName: 'Tasks', columns: [
  ColumnModel(id: 't-id', tableId: 'tbl-t', name: 'id', displayName: 'id', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 't-code', tableId: 'tbl-t', name: 'code', displayName: 'code', fieldType: 'TEXT'),
  ColumnModel(id: 't-parent', tableId: 'tbl-t', name: 'parent_code', displayName: 'parent_code', fieldType: 'TEXT'),
]);

const _notes = TableModel(id: 'tbl-n', name: 'notes', displayName: 'Notes', columns: [
  ColumnModel(id: 'n-task', tableId: 'tbl-n', name: 'task_code', displayName: 'task_code', fieldType: 'TEXT'),
]);

const _tasksOccurrence = TableOccurrenceModel(id: 'occ-t', baseTableId: 'tbl-t', name: 'Tasks', xPos: 0, yPos: 0);
const _notesOccurrence = TableOccurrenceModel(id: 'occ-n', baseTableId: 'tbl-n', name: 'Notes', xPos: 300, yPos: 0);

/// Tasks::code = Tasks::parent_code: from a task, the right end is its
/// subtasks and the left end its parent.
const _subtasks = RelationshipModel(
  id: 'rel-sub',
  name: 'Task_Subtasks',
  leftOccurrenceId: 'occ-t',
  leftColumnId: 't-code',
  rightOccurrenceId: 'occ-t',
  rightColumnId: 't-parent',
);

const _taskNotes = RelationshipModel(
  id: 'rel-notes',
  name: 'Task_Notes',
  leftOccurrenceId: 'occ-t',
  leftColumnId: 't-code',
  rightOccurrenceId: 'occ-n',
  rightColumnId: 'n-task',
);

class _GraphApi extends ApiClient {
  _GraphApi() : super(baseUrl: 'http://localhost:1');

  @override
  Future<List<RelationshipModel>> listRelationships() async => const [_subtasks, _taskNotes];

  @override
  Future<List<TableOccurrenceModel>> listOccurrences() async => const [_tasksOccurrence, _notesOccurrence];
}

Future<LayoutDefinitionModel? Function()> _pumpDesigner(WidgetTester tester, PortalConfigModel portal) async {
  tester.view.physicalSize = const Size(1600, 1200);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  LayoutDefinitionModel? last;
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: LayoutDesignerWidget(
        table: _tasks,
        tables: const [_tasks, _notes],
        apiClient: _GraphApi(),
        initialLayout: LayoutDefinitionModel(
          id: 'l1',
          name: 'Tasks',
          tableOccurrence: 'tasks',
          parts: _parts,
          objects: [
            LayoutObjectModel(
                id: 'p1', type: 'portal', x: 60, y: 60, width: 360, height: 160, portalConfig: portal.toJson()),
          ],
        ),
        onSaved: () {},
        onLayoutChanged: (l) => last = l,
      ),
    ),
  ));
  await tester.pumpAndSettle();
  // Select the portal, then open the inspector's Data tab.
  await tester.tap(find.byIcon(Icons.table_rows).first);
  await tester.pump();
  await tester.tap(find.byIcon(Icons.storage_outlined).first);
  await tester.pumpAndSettle();
  return () => last;
}

PortalConfigModel _portalOf(LayoutDefinitionModel layout) =>
    layout.objects.singleWhere((o) => o.type == 'portal').portal;

/// Lets the auto-save debounce fire (it fails silently: no server).
Future<void> _settleAutoSave(WidgetTester tester) => tester.pump(const Duration(seconds: 2));

RelatedContext _context() => RelatedContext(
      apiClient: ApiClient(baseUrl: 'http://localhost:1'),
      fromTable: 'tasks',
      recordId: 'task-1',
      relationshipsById: const {'rel-sub': _subtasks, 'rel-notes': _taskNotes},
      occurrencesById: const {'occ-t': _tasksOccurrence, 'occ-n': _notesOccurrence},
      tablesById: const {'tbl-t': _tasks, 'tbl-n': _notes},
      formatValue: (_, raw) => '${raw ?? ''}',
    );

void main() {
  group('Portal Setup', () {
    testWidgets('offers the end of a relationship joining an occurrence to itself, and keeps it', (tester) async {
      final last = await _pumpDesigner(tester, const PortalConfigModel(relationshipId: 'rel-sub', occurrence: 'Tasks'));
      final choice = find.byKey(const ValueKey('portal-relationship-side'));
      await tester.ensureVisible(choice);
      expect(tester.widget<SegmentedButton<String>>(choice).selected, {'right'},
          reason: 'a portal that names no end shows the right one, as before');

      await tester.tap(find.descendant(of: choice, matching: find.text('Left end')));
      await tester.pumpAndSettle();
      expect(_portalOf(last()!).relationshipSide, 'left');
      expect(_portalOf(last()!).toJson()['relationship_side'], 'left');
      expect(tester.widget<SegmentedButton<String>>(choice).selected, {'left'});

      await tester.tap(find.descendant(of: choice, matching: find.text('Right end')));
      await tester.pumpAndSettle();
      expect(_portalOf(last()!).relationshipSide, 'right');
      await _settleAutoSave(tester);
    });

    testWidgets('lists a relationship joining an occurrence to itself once', (tester) async {
      await _pumpDesigner(tester, const PortalConfigModel(relationshipId: 'rel-sub', occurrence: 'Tasks'));
      await tester.tap(find.byKey(const ValueKey('inspector-portal-relationship')));
      await tester.pumpAndSettle();
      expect(find.text('Tasks  ·  through "Task_Subtasks"'), findsWidgets);
      expect(find.text('Notes  ·  through "Task_Notes"'), findsWidgets);
      expect(find.textContaining('left end'), findsNothing, reason: 'the end is chosen beside the list');
      await _settleAutoSave(tester);
    });

    testWidgets('offers no end for a relationship between two occurrences', (tester) async {
      await _pumpDesigner(tester, const PortalConfigModel(relationshipId: 'rel-notes', occurrence: 'Notes'));
      expect(find.byKey(const ValueKey('inspector-portal-relationship')), findsOneWidget);
      expect(find.byKey(const ValueKey('portal-relationship-side')), findsNothing);
      await _settleAutoSave(tester);
    });
  });

  group('the end a layout object reads', () {
    test('is the stored one for a relationship joining an occurrence to itself', () {
      final related = _context();
      final right = related.resolve('rel-sub', 'Tasks')!;
      expect(right.side, 'right', reason: 'no end named reads the right one');
      expect(right.requestSide, 'right');

      final left = related.resolve('rel-sub', 'Tasks', side: RelationshipSide.left)!;
      expect(left.side, 'left');
      expect(left.requestSide, 'left', reason: 'it is what list, create and batch requests send');
      expect(left.occurrence.id, 'occ-t');

      expect(related.resolve('rel-sub', null, side: RelationshipSide.left)?.side, 'left',
          reason: 'the end alone is enough');
      expect(related.resolve('rel-sub', null), isNull, reason: 'without it, which end is wanted is not guessed');
    });

    test('is ignored for a relationship between two occurrences', () {
      final target = _context().resolve('rel-notes', 'Notes', side: RelationshipSide.left)!;
      expect(target.occurrence.name, 'Notes');
      expect(target.requestSide, isNull);
    });

    test('travels in the layout, and is left out when not set', () {
      const portal = PortalConfigModel(relationshipId: 'rel-sub', occurrence: 'Tasks', relationshipSide: 'left');
      expect(PortalConfigModel.fromJson(portal.toJson()).relationshipSide, 'left');
      expect(const PortalConfigModel(relationshipId: 'rel-sub').toJson().containsKey('relationship_side'), isFalse);
      expect(PortalConfigModel.fromJson(const {'relationship_side': 'middle'}).relationshipSide, isNull,
          reason: 'anything but left or right reads the right end');
      expect(portal.copyWith(clearRelationship: true).relationshipSide, isNull,
          reason: 'another relationship starts from its default end');

      const field = FieldBindingModel(
          fieldName: 'code', relationshipId: 'rel-sub', tableOccurrence: 'Tasks', relationshipSide: 'left');
      expect(FieldBindingModel.fromJson(field.toJson()).relationshipSide, 'left');
    });
  });
}
