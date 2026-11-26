// A relationship joining an occurrence to itself has that occurrence at both
// ends, so a portal (or a related field) names the end it shows (#167):
// Portal Setup offers the choice for such a relationship only, the layout
// keeps it, and Browse mode asks the server for that end.

import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/data_browser/related_records.dart';
import 'package:file4base_client/features/layout_engine/layout_designer_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/script_workspace/calculation_builder_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
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
  // The calculation editor reads the theme through Riverpod.
  await tester.pumpWidget(ProviderScope(
      child: MaterialApp(
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
  )));
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

/// Answers each related read when the test says so, by the end it asked for.
class _SlowApi extends ApiClient {
  _SlowApi() : super(baseUrl: 'http://localhost:1');

  final Map<String?, Completer<List<Map<String, dynamic>>>> pending = {};

  @override
  Future<List<Map<String, dynamic>>> listRelatedRows(String table, String id,
      {required String relationshipId, String? occurrence, String? side, List<String>? sort, int limit = 100,
      int offset = 0}) {
    return (pending[side] = Completer()).future;
  }
}

RelatedContext _context([ApiClient? api]) => RelatedContext(
      apiClient: api ?? ApiClient(baseUrl: 'http://localhost:1'),
      fromTable: 'tasks',
      recordId: 'task-1',
      relationshipsById: const {'rel-sub': _subtasks, 'rel-notes': _taskNotes},
      occurrencesById: const {'occ-t': _tasksOccurrence, 'occ-n': _notesOccurrence},
      tablesById: const {'tbl-t': _tasks, 'tbl-n': _notes},
      formatValue: (_, raw) => '${raw ?? ''}',
    );

void main() {
  group('Portal Setup', () {
    testWidgets('sets the portal filter in the calculation editor', (tester) async {
      final last = await _pumpDesigner(tester, const PortalConfigModel(relationshipId: 'rel-notes', occurrence: 'Notes'));
      final button = find.byKey(const ValueKey('p1-portal-filter-calc'));
      await tester.ensureVisible(button);
      await tester.tap(button);
      await tester.pumpAndSettle();
      final dialog = tester.widget<CalculationBuilderDialog>(find.byType(CalculationBuilderDialog));
      expect(dialog.contextTable, 'Notes', reason: 'a portal filter is evaluated from the portal occurrence');
      expect(dialog.fieldsByTable.keys, containsAll(<String>['Tasks', 'Notes']));
      // The formula box is the one that grows with its text.
      final formula = find.descendant(
          of: find.byType(CalculationBuilderDialog), matching: find.byWidgetPredicate((w) => w is TextField && w.expands));
      await tester.enterText(formula, 'done = 0');
      await tester.tap(find.text('OK'));
      await tester.pumpAndSettle();
      expect(_portalOf(last()!).filter, 'done = 0');
      await _settleAutoSave(tester);
    });

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

    testWidgets('keeps the end when the same relationship is chosen again', (tester) async {
      final last = await _pumpDesigner(
          tester, const PortalConfigModel(relationshipId: 'rel-sub', occurrence: 'Tasks', relationshipSide: 'left'));
      final dropdown = find.byKey(const ValueKey('inspector-portal-relationship'));
      await tester.ensureVisible(dropdown);
      await tester.tap(dropdown);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Tasks  ·  through "Task_Subtasks"').last);
      await tester.pumpAndSettle();
      final choice = find.byKey(const ValueKey('portal-relationship-side'));
      expect(tester.widget<SegmentedButton<String>>(choice).selected, {'left'});
      expect(last() == null || _portalOf(last()!).relationshipSide == 'left', isTrue,
          reason: 'choosing the relationship shown changes nothing');

      await tester.tap(dropdown);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Notes  ·  through "Task_Notes"').last);
      await tester.pumpAndSettle();
      expect(_portalOf(last()!).relationshipSide, isNull, reason: 'another relationship starts from its default end');
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
      final byId = related.resolve('rel-sub', null)!;
      expect(byId.side, 'right', reason: 'the relationship alone shows the right end, as Portal Setup does');
      expect(byId.requestSide, 'right');
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

  group('a portal', () {
    LayoutObjectModel portal(String side) => LayoutObjectModel(
          id: 'p1', type: 'portal', x: 0, y: 0, width: 300, height: 120,
          portalConfig: PortalConfigModel(relationshipId: 'rel-sub', occurrence: 'Tasks', relationshipSide: side)
              .toJson(),
        );
    const cell = LayoutObjectModel(
        id: 'f1', type: 'field', x: 4, y: 4, width: 200, height: 24, fieldBinding: FieldBindingModel(fieldName: 'code'));

    testWidgets('drops the answer to a read it has moved on from', (tester) async {
      final api = _SlowApi();
      final related = _context(api);
      Widget view(String side) => MaterialApp(
            home: Scaffold(
              body: PortalView(object: portal(side), contents: const [cell], related: related, isDark: false),
            ),
          );
      await tester.pumpWidget(view('right'));
      await tester.pump();
      expect(api.pending.keys, contains('right'));

      // The end changes while the right end's records are still on their way.
      await tester.pumpWidget(view('left'));
      await tester.pump();
      api.pending['left']!.complete([
        {'id': 'parent', 'code': 'PARENT'},
      ]);
      await tester.pump();
      api.pending['right']!.complete([
        {'id': 'sub', 'code': 'SUB'},
      ]);
      await tester.pumpAndSettle();
      expect(find.text('PARENT'), findsOneWidget);
      expect(find.text('SUB'), findsNothing, reason: 'the right end is not what the portal shows any more');
    });
  });

  group('Find mode', () {
    test('keeps a criterion on each end of a self-join apart, and sends the end', () {
      final left = DataBrowserWidgetState.relatedCriterionKey('rel-sub', 'Tasks', 'code', side: RelationshipSide.left);
      final right = DataBrowserWidgetState.relatedCriterionKey('rel-sub', 'Tasks', 'code', side: RelationshipSide.right);
      expect(left, isNot(right));
      expect(DataBrowserWidgetState.parseRelatedCriterionKey(left)?.side, 'left');
      expect(DataBrowserWidgetState.parseRelatedCriterionKey(left)?.field, 'code');
      expect(DataBrowserWidgetState.parseRelatedCriterionKey(DataBrowserWidgetState.relatedCriterionKey('rel-sub', 'Tasks', 'code'))?.side,
          isNull, reason: 'a key saved before reads the right end, as before');

      final criteria = DataBrowserWidgetState.criteriaFor({left: 'A1', right: 'B2'});
      expect(criteria, hasLength(2));
      expect(criteria.map((c) => c['side']), containsAll(['left', 'right']));
      expect(criteria.every((c) => c['relationship_id'] == 'rel-sub' && c['occurrence'] == 'Tasks'), isTrue);
      expect(DataBrowserWidgetState.criteriaFor({DataBrowserWidgetState.relatedCriterionKey('rel-notes', 'Notes', 'task_code'): 'x'})
          .single.containsKey('side'), isFalse);
    });
  });
}
