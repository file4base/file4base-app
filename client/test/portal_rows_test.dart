// #26 — Go to Portal Row in Browse mode: a portal of the layout has an
// active row, which the step moves, a click sets and
// Get(ActivePortalRowNumber) reads; Delete Portal Row deletes its related
// record, never the record in hand.
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _projects = TableModel(id: 't-projects', name: 'projects', displayName: 'Projects', columns: [
  ColumnModel(id: 'p0', tableId: 't-projects', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'p1', tableId: 't-projects', name: 'code', displayName: 'Code', fieldType: 'TEXT'),
]);

const _steps = TableModel(id: 't-steps', name: 'steps', displayName: 'Steps', columns: [
  ColumnModel(id: 's0', tableId: 't-steps', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 's1', tableId: 't-steps', name: 'code', displayName: 'Code', fieldType: 'TEXT'),
  ColumnModel(id: 's2', tableId: 't-steps', name: 'label', displayName: 'Label', fieldType: 'TEXT'),
]);

const _relationship = RelationshipModel(
  id: 'rel-1',
  name: 'projects_steps',
  leftOccurrenceId: 'occ-projects',
  leftColumnId: 'p1',
  rightOccurrenceId: 'occ-steps',
  rightColumnId: 's1',
);

/// Steps reached from steps: a portal of it on a Projects layout is two
/// relationships away.
const _nextSteps = RelationshipModel(
  id: 'rel-2',
  name: 'steps_next',
  leftOccurrenceId: 'occ-steps',
  leftColumnId: 's2',
  rightOccurrenceId: 'occ-next',
  rightColumnId: 's1',
);

class _Api extends ApiClient {
  _Api(this.scripts) : super(baseUrl: 'http://localhost:1');
  final Map<String, ScriptModel> scripts;
  final List<String> ran = [];
  final List<(String, Map<String, dynamic>, bool)> updates = [];
  final List<String> deletes = [];
  final List<Map<String, dynamic>> children = [
    {'id': 'r1', 'code': 'P', 'label': 'Plan'},
    {'id': 'r2', 'code': 'P', 'label': 'Build'},
    {'id': 'r3', 'code': 'P', 'label': 'Ship'},
  ];

  /// The steps of the second project.
  final List<Map<String, dynamic>> others = [
    {'id': 'q1', 'code': 'Q', 'label': 'Draft'},
    {'id': 'q2', 'code': 'Q', 'label': 'Review'},
  ];

  @override
  Future<List<RelationshipModel>> listRelationships() async => const [_relationship, _nextSteps];

  @override
  Future<List<TableOccurrenceModel>> listOccurrences() async => const [
        TableOccurrenceModel(id: 'occ-projects', baseTableId: 't-projects', name: 'Projects', xPos: 0, yPos: 0),
        TableOccurrenceModel(id: 'occ-steps', baseTableId: 't-steps', name: 'Steps', xPos: 0, yPos: 0),
        TableOccurrenceModel(id: 'occ-next', baseTableId: 't-steps', name: 'Next Steps', xPos: 0, yPos: 0),
      ];

  @override
  Future<List<TableModel>> listTables() async => const [_projects, _steps];

  @override
  Future<List<ValueListModel>> listValueLists() async => const [];

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [
        {'id': 'p-1', 'code': 'P'},
        {'id': 'p-2', 'code': 'Q'},
      ];

  @override
  Future<List<Map<String, dynamic>>> listRelatedRows(String table, String id,
          {required String relationshipId,
          String? occurrence,
          String? side,
          List<String>? sort,
          int limit = 100,
          int offset = 0}) async =>
      [for (final r in id == 'p-2' ? others : children) Map<String, dynamic>.of(r)];

  /// Whether a portal along a path was given only its first records.
  bool alongMore = false;

  @override
  Future<({List<Map<String, dynamic>> records, bool more})> listRelatedAlong(String table, String id,
          {required List<String> path, String? context, List<String>? sort}) async =>
      (records: [for (final r in children) Map<String, dynamic>.of(r)], more: alongMore);

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> values,
      {bool script = false, bool overrideValidation = false}) async {
    updates.add((id, Map.of(values), script));
    final i = children.indexWhere((c) => c['id'] == id);
    children[i] = {...children[i], ...values};
    return Map<String, dynamic>.of(children[i]);
  }

  @override
  Future<void> deleteRow(String table, String id) async {
    deletes.add('$table/$id');
    children.removeWhere((c) => c['id'] == id);
    others.removeWhere((c) => c['id'] == id);
  }

  @override
  Future<ScriptModel> getScript(String id, {String? database}) async {
    ran.add(id);
    return scripts[id]!;
  }
}

LayoutDefinitionModel _layout({List<Map<String, dynamic>> triggers = const [], PortalConfigModel? portal}) =>
    LayoutDefinitionModel.fromJson({
      'id': 'l-projects',
      'name': 'Projects',
      'table_occurrence': 'Projects',
      'objects': [
        {'id': 'f-code', 'type': 'field', 'x': 40, 'y': 20, 'width': 200, 'height': 30, 'field_binding': {'field_name': 'code'}},
        {
          'id': 'portal-steps',
          'type': 'portal',
          'name': 'StepsPortal',
          'x': 40,
          'y': 80,
          'width': 400,
          'height': 150,
          'portal_config': (portal ?? const PortalConfigModel(relationshipId: 'rel-1', occurrence: 'Steps', allowDeletion: true))
              .toJson(),
        },
        {'id': 'f-label', 'type': 'field', 'x': 50, 'y': 85, 'width': 200, 'height': 24, 'field_binding': {'field_name': 'label'}},
      ],
      'script_triggers': triggers,
    });

Future<DataBrowserWidgetState> _pump(WidgetTester tester, _Api api, {LayoutDefinitionModel? layout}) async {
  tester.view.physicalSize = const Size(1400, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final key = GlobalKey<DataBrowserWidgetState>();
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: DataBrowserWidget(
        key: key,
        table: _projects,
        apiClient: api,
        mode: OperationalMode.browse,
        layout: layout ?? _layout(),
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return key.currentState!;
}

Finder _cell(String row) => find.byKey(ValueKey('portal-cell-$row-label'));

bool _focused(WidgetTester tester, String row) => tester.widget<TextField>(_cell(row)).focusNode?.hasFocus ?? false;

Future<T> _settled<T>(WidgetTester tester, Future<T> work) async {
  await tester.pumpAndSettle();
  return work;
}

void main() {
  testWidgets('Go to Portal Row never takes the last row read for the last record of a portal with more',
      (tester) async {
    const along = PortalConfigModel(occurrence: 'Next Steps', occurrencePath: ['Steps', 'Next Steps'], allowDeletion: true);
    final api = _Api(const {})..alongMore = true;
    final state = await _pump(tester, api, layout: _layout(portal: along));
    expect(_cell('r3'), findsOneWidget);

    expect((await _settled(tester, state.actionGoToPortalRow('2'))).errorCode, 0);
    final last = await _settled(tester, state.actionGoToPortalRow('last'));
    expect((last.moved, last.errorCode), (false, 3), reason: 'row 3 is not the last related record');
    expect(await _settled(tester, state.actionDeletePortalRow(confirm: false)), 101,
        reason: 'no row is left active for a step after it');
    expect(api.deletes, isEmpty);

    expect((await _settled(tester, state.actionGoToPortalRow('3'))).errorCode, 0, reason: 'a row it has');
    final next = await _settled(tester, state.actionGoToPortalRow('next'));
    expect((next.moved, next.atBoundary, next.errorCode), (false, true, 3), reason: 'Next past the rows read ends a loop');
    expect((await _settled(tester, state.actionGoToPortalRow('7'))).errorCode, 3);

    // All the related records: the last row is the last one.
    api.alongMore = false;
    final whole = await _pump(tester, api, layout: _layout(portal: along.copyWith(sort: 'label')));
    final ok = await _settled(tester, whole.actionGoToPortalRow('last'));
    expect((ok.moved, ok.errorCode), (true, 0));
  });

  testWidgets('Go to Portal Row moves through the first portal, and stops at either end with error 101',
      (tester) async {
    final api = _Api(const {});
    final state = await _pump(tester, api);
    final runner = LayoutActionRunner(apiClient: api, host: state);
    final session = ScriptSessionState();
    Future<int> go(String target, {bool exitAfterLast = false}) async {
      final context = ScriptCallContext(session: session);
      await _settled(tester, runner.runStep('go_to_portal_row', {'target': target, 'exit_after_last': exitAfterLast},
          depth: 0, callContext: context));
      return session.lastError;
    }

    String row() => runner.resolveValue('Get(ActivePortalRowNumber)', {})!;

    expect(row(), '0', reason: 'no row is active before one is gone to');
    expect(await go('next'), 0);
    expect(row(), '1', reason: 'next with no active row goes to the first');
    expect(_focused(tester, 'r1'), isTrue, reason: 'the first field of the row is entered');
    expect(await go('next'), 0);
    expect(row(), '2');
    expect(_focused(tester, 'r2'), isTrue);
    expect(await go('last'), 0);
    expect(row(), '3');
    expect(await go('next'), 101);
    expect(row(), '3', reason: 'past the end it stays');
    expect(await go('first'), 0);
    expect(await go('previous'), 101);
    expect(await go('2'), 0, reason: 'by calculation');
    expect(row(), '2');
    expect(await go('7'), 101);
    expect(row(), '2');
    expect(runner.resolveValue('Get(ActiveFieldTableName)', {}), 'Steps');

    final last = await _settled(tester, runner.runStep('go_to_portal_row', {'target': 'last'}, depth: 0));
    expect(last.exitLoop, isFalse);
    final past = await _settled(
        tester, runner.runStep('go_to_portal_row', {'target': 'next', 'exit_after_last': true}, depth: 0));
    expect(past.exitLoop, isTrue, reason: 'Exit after last leaves the loop');
  });

  testWidgets('with Select the whole row is selected, and no field entered', (tester) async {
    final api = _Api(const {});
    final state = await _pump(tester, api);
    final outcome = await _settled(tester, state.actionGoToPortalRow('2', select: true));
    expect(outcome.errorCode, 0);
    expect(state.actionActivePortalRowNumber, 2);
    expect(_focused(tester, 'r2'), isFalse);
  });

  testWidgets('a click on a row makes it active; the active row is offered to Set Field', (tester) async {
    final api = _Api(const {});
    final state = await _pump(tester, api);
    await tester.tap(_cell('r3'));
    await tester.pumpAndSettle();
    expect(state.actionActivePortalRowNumber, 3);

    final active = state.actionActivePortalRow()!;
    expect(active.portalObjectId, 'portal-steps');
    expect(active.portalName, 'StepsPortal');
    expect(active.occurrence, 'Steps');
    expect(active.table, 'steps');
    expect(active.recordId, 'r3');
    expect(active.record['label'], 'Ship');
    expect(state.actionActivePortalRow(occurrence: 'steps'), isNotNull, reason: 'named without case');
    expect(state.actionActivePortalRow(occurrence: 'Other'), isNull);

    final written = await _settled(tester, active.write!({'label': 'Release'}, script: true));
    expect(written['label'], 'Release');
    expect(api.updates.single.$1, 'r3');
    expect(api.updates.single.$2, {'label': 'Release'});
    expect(api.updates.single.$3, isTrue, reason: "written as a script's write");
    expect(find.text('Release'), findsOneWidget, reason: 'the portal shows what was written');

    // Entering a field outside the portal leaves no row active.
    await tester.tap(find.byWidgetPredicate((w) => w is TextField && w.controller?.text == 'P'));
    await tester.pumpAndSettle();
    expect(state.actionActivePortalRowNumber, 0);
    expect(state.actionActivePortalRow(), isNull);
  });

  testWidgets('Go to Portal Row keeps the field active in the portal, and runs its exit and enter triggers',
      (tester) async {
    Map<String, dynamic> trigger(String id, String event, String script) => {
          'id': id,
          'event': event,
          'type': 'perform_script',
          'script_id': script,
          'script_name': script,
          'enabled': true,
          'object_id': 'f-label',
        };
    ScriptModel script(String id, [String? result]) => ScriptModel.fromJson({
          'id': id,
          'name': id,
          'is_active': true,
          'steps': [
            if (result != null)
              {'id': 'x', 'sequence_idx': 1, 'step_type': 'Exit Script', 'params': {'result': result}, 'is_enabled': true},
          ],
        });
    final scripts = {'enter': script('enter'), 'exit': script('exit')};
    final api = _Api(scripts);
    final state = await _pump(tester, api,
        layout: _layout(triggers: [trigger('a', 'OnObjectEnter', 'enter'), trigger('b', 'OnObjectExit', 'exit')]));
    await tester.tap(_cell('r1'));
    await tester.pumpAndSettle();
    expect(api.ran, ['enter']);

    expect((await _settled(tester, state.actionGoToPortalRow('next'))).errorCode, 0);
    expect(api.ran, ['enter', 'exit', 'enter']);
    expect(_focused(tester, 'r2'), isTrue);

    scripts['exit'] = script('exit', 'False');
    final kept = await _settled(tester, state.actionGoToPortalRow('next'));
    expect(kept.errorCode, 20, reason: 'OnObjectExit False cancels the step');
    expect(state.actionActivePortalRowNumber, 2);
  });

  testWidgets('Delete Portal Row deletes the active row only, and needs one', (tester) async {
    final api = _Api(const {});
    final state = await _pump(tester, api);
    expect(await state.actionDeletePortalRow(confirm: false), 101, reason: 'no active row');
    await _settled(tester, state.actionGoToPortalRow('2', select: true));
    expect(await _settled(tester, state.actionDeletePortalRow(confirm: false)), 0);
    expect(api.deletes, ['steps/r2']);
    expect(state.actionActivePortalRowNumber, 0);
  });

  testWidgets('a portal row of the record left is never the active row of the record gone to', (tester) async {
    final api = _Api(const {});
    final state = await _pump(tester, api);
    await _settled(tester, state.actionGoToPortalRow('2', select: true));
    expect(state.actionActivePortalRowNumber, 2);
    await _settled(tester, state.goToRecord(1));
    expect(state.actionActivePortalRowNumber, 0);
    expect(await _settled(tester, state.actionDeletePortalRow(confirm: false)), 101);
    expect(api.deletes, isEmpty, reason: "the step of the record left is not deleted");

    // Gone to straight after another record, before its portal is drawn again:
    // the row is one of that record's.
    await _settled(tester, state.goToRecord(0));
    expect(await state.goToRecord(1), isTrue);
    final went = state.actionGoToPortalRow('first', select: true);
    await tester.pumpAndSettle();
    expect((await went).errorCode, 0);
    expect(state.actionActivePortalRow()!.recordId, 'q1');
    expect(await _settled(tester, state.actionDeletePortalRow(confirm: false)), 0);
    expect(api.deletes, ['steps/q1']);
  });

  testWidgets('Set Field on a field of the portal writes the row Go to Portal Row made active', (tester) async {
    final api = _Api(const {});
    final state = await _pump(tester, api);
    final runner = LayoutActionRunner(apiClient: api, host: state);
    await _settled(tester, state.actionGoToPortalRow('2', select: true));
    final session = ScriptSessionState();
    await _settled(
        tester,
        runner.runStep('Set Field', {'field': 'label', 'field_table': 'Steps', 'value': '"Built"', 'value_is_calculation': true},
            depth: 0, callContext: ScriptCallContext(session: session)));
    expect(session.lastError, 0);
    expect(api.updates.single.$1, 'r2', reason: 'the active portal row, not the first related record');
  });
}
