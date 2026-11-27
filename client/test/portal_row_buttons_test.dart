// #237 — a button in a portal row runs with that row active, as a click in
// a row outside its fields selects it in FileMaker: the row is the active
// portal row and its related record the current record of the portal's
// occurrence, while the layout's record stays the current record. A script
// parameter the server evaluates reads the row; Delete Portal Row, Set
// Field and Go to Related Record act on it; a popover button opens its
// popover in the row.
import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/data_browser/related_records.dart';
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

Map<String, dynamic> _step(int n, String type, [Map<String, dynamic> params = const {}]) =>
    {'id': 's$n', 'sequence_idx': n, 'step_type': type, 'params': params, 'is_enabled': true};

ScriptModel _script(String id, List<Map<String, dynamic>> steps) =>
    ScriptModel.fromJson({'id': id, 'name': id, 'is_active': true, 'steps': steps});

class _Api extends ApiClient {
  _Api([this.scripts = const {}]) : super(baseUrl: 'http://localhost:1');
  final Map<String, ScriptModel> scripts;
  final List<String> ran = [];
  final List<(String, Map<String, dynamic>, bool)> updates = [];
  final List<String> deletes = [];
  final List<({String? recordId, String? occurrence, Map<String, String>? portalRow})> evaluated = [];
  final List<Map<String, dynamic>> projects = [
    {'id': 'p-1', 'code': 'P'},
    {'id': 'p-2', 'code': 'Q'},
  ];
  final List<Map<String, dynamic>> children = [
    {'id': 'r1', 'code': 'P', 'label': 'Plan'},
    {'id': 'r2', 'code': 'P', 'label': 'Build'},
    {'id': 'r3', 'code': 'P', 'label': 'Ship'},
    {'id': 'q1', 'code': 'Q', 'label': 'Draft'},
  ];

  List<Map<String, dynamic>> _related(String id) {
    final code = projects.firstWhere((p) => p['id'] == id)['code'];
    return [for (final r in children.where((c) => c['code'] == code)) Map<String, dynamic>.of(r)];
  }

  @override
  Future<List<RelationshipModel>> listRelationships() async => const [_relationship];

  @override
  Future<List<TableOccurrenceModel>> listOccurrences() async => const [
        TableOccurrenceModel(id: 'occ-projects', baseTableId: 't-projects', name: 'Projects', xPos: 0, yPos: 0),
        TableOccurrenceModel(id: 'occ-steps', baseTableId: 't-steps', name: 'Steps', xPos: 0, yPos: 0),
      ];

  @override
  Future<List<TableModel>> listTables() async => const [_projects, _steps];

  @override
  Future<List<ValueListModel>> listValueLists() async => const [];

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [for (final r in table == 'steps' ? children : projects) Map<String, dynamic>.of(r)];

  @override
  Future<List<Map<String, dynamic>>> listAllRows(String table, {void Function(int fetched)? onProgress}) =>
      listRows(table);

  @override
  Future<List<Map<String, dynamic>>> listRelatedRows(String table, String id,
          {required String relationshipId,
          String? occurrence,
          String? side,
          List<String>? sort,
          int limit = 100,
          int offset = 0}) async =>
      _related(id);

  @override
  Future<Map<String, List<Map<String, dynamic>>>> listRelatedRowsBatch(String table, List<String> ids,
          {required String relationshipId, String? occurrence, String? side, List<String>? sort, int limit = 100}) async =>
      {for (final id in ids) id: _related(id)};

  /// The server knows `Tag ( text )`; the client does not.
  @override
  Future<List<EvaluationModel>> evaluateExpressions(String table,
      {required List<String> expressions,
      String? recordId,
      String resultType = 'Text',
      Map<String, String>? variables,
      Map<String, Object?>? get,
      Map<String, Object?>? values,
      String? occurrence,
      Map<String, String>? portalRow}) async {
    evaluated.add((recordId: recordId, occurrence: occurrence, portalRow: portalRow));
    final row = children.where((c) => c['id'] == portalRow?['record_id']).firstOrNull;
    return [for (final e in expressions) EvaluationModel(expression: e, text: '${row?['label'] ?? 'first'}!')];
  }

  /// While set, a save waits for it.
  Completer<void>? gate;
  int reads = 0;

  @override
  Future<Map<String, dynamic>> getRow(String table, String id) async {
    reads++;
    return Map<String, dynamic>.of((table == 'steps' ? children : projects).firstWhere((r) => r['id'] == id));
  }

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> values,
      {bool script = false, bool overrideValidation = false}) async {
    await gate?.future;
    updates.add((id, Map.of(values), script));
    final i = children.indexWhere((c) => c['id'] == id);
    children[i] = {...children[i], ...values};
    return Map<String, dynamic>.of(children[i]);
  }

  @override
  Future<void> deleteRow(String table, String id) async {
    deletes.add('$table/$id');
    children.removeWhere((c) => c['id'] == id);
  }

  @override
  Future<ScriptModel> getScript(String id, {String? database}) async {
    ran.add(id);
    return scripts[id]!;
  }
}

/// A Projects layout whose portal of steps has, in its row, the label and
/// a button running [action] (a popover button when [popover]).
LayoutDefinitionModel _projectsLayout(Map<String, dynamic>? action, {bool popover = false}) =>
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
          'portal_config': const PortalConfigModel(relationshipId: 'rel-1', occurrence: 'Steps').toJson(),
        },
        {'id': 'f-label', 'type': 'field', 'x': 50, 'y': 85, 'width': 200, 'height': 24, 'field_binding': {'field_name': 'label'}},
        {
          'id': 'b-row',
          'type': popover ? 'popover_button' : 'button',
          'x': 300,
          'y': 85,
          'width': 60,
          'height': 24,
          'text': 'Go',
          if (popover) 'default_panel': -1,
          if (action != null) 'action': action,
        },
        if (popover) ...[
          {
            'id': 'pop-back',
            'type': 'rect',
            'x': 500,
            'y': 80,
            'width': 220,
            'height': 60,
            'panel': {'control': 'b-row', 'index': 0},
          },
          {
            'id': 'pop-label',
            'type': 'field',
            'x': 510,
            'y': 90,
            'width': 200,
            'height': 24,
            'panel': {'control': 'b-row', 'index': 0},
            'field_binding': {'field_name': 'label', 'table_occurrence': 'Steps', 'relationship_id': 'rel-1'},
          },
        ],
      ],
    });

final _stepDetail = LayoutDefinitionModel.fromJson({
  'id': 'l-steps',
  'name': 'Step Detail',
  'table_occurrence': 'Steps',
  'objects': [
    {'id': 'f-step-label', 'type': 'field', 'x': 40, 'y': 20, 'width': 200, 'height': 30, 'field_binding': {'field_name': 'label'}},
  ],
});

/// A window that goes to the layouts it knows, as the app's does.
class _Window extends StatefulWidget {
  const _Window({required this.api, required this.layout, required this.browser, super.key});
  final _Api api;
  final LayoutDefinitionModel layout;
  final GlobalKey<DataBrowserWidgetState> browser;

  @override
  State<_Window> createState() => _WindowState();
}

class _WindowState extends State<_Window> {
  late LayoutDefinitionModel layout = widget.layout;

  @override
  Widget build(BuildContext context) => MaterialApp(
        home: Scaffold(
          body: DataBrowserWidget(
            key: widget.browser,
            table: layout.tableOccurrence == 'Steps' ? _steps : _projects,
            apiClient: widget.api,
            mode: OperationalMode.browse,
            layout: layout,
            onGoToLayout: (name) {
              final to = [widget.layout, _stepDetail].where((l) => l.name == name).firstOrNull;
              if (to != null) setState(() => layout = to);
              return to != null;
            },
          ),
        ),
      );
}

Future<DataBrowserWidgetState> _pump(WidgetTester tester, _Api api, LayoutDefinitionModel layout) async {
  tester.view.physicalSize = const Size(1400, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final key = GlobalKey<DataBrowserWidgetState>();
  await tester.pumpWidget(_Window(key: UniqueKey(), api: api, layout: layout, browser: key));
  await tester.pumpAndSettle();
  return key.currentState!;
}

Finder _button(int row) => find.byKey(ValueKey('portal-button-b-row-$row'));

/// Taps the button of [row] and lets what it runs finish.
Future<void> _press(WidgetTester tester, int row) async {
  await tester.tap(_button(row));
  for (var i = 0; i < 20; i++) {
    await tester.pump(const Duration(milliseconds: 50));
  }
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('a row button runs its script with that row active and the layout record current', (tester) async {
    final api = _Api({
      'mark': _script('mark', [
        _step(1, 'Set Field', {
          'field': 'label',
          'field_table': 'Steps',
          'value': 'Get ( ScriptParameter ) & "/" & Get ( ActivePortalRowNumber ) & "/" & Get ( LayoutTableName )',
          'value_is_calculation': true,
        }),
      ]),
    });
    final state = await _pump(tester, api, _projectsLayout({
      'type': 'perform_script',
      'script_id': 'mark',
      'script_name': 'mark',
      'parameter': 'Tag ( Steps::label )',
    }));
    expect(_button(2), findsOneWidget);

    await _press(tester, 2);
    expect(api.ran, ['mark']);
    // The parameter, evaluated by the server on the layout's record with
    // the row as the current record of the portal's occurrence.
    final asked = api.evaluated.single;
    expect(asked.recordId, 'p-1');
    expect(asked.occurrence, 'occ-projects');
    expect(asked.portalRow, {'occurrence': 'occ-steps', 'record_id': 'r2'});
    final (id, values, script) = api.updates.single;
    expect((id, values['label'], script), ('r2', 'Build!/2/Projects', true),
        reason: "Set Field writes the row clicked; Get(LayoutTableName) stays the layout's");
    expect(state.currentRecord?['id'], 'p-1', reason: 'the layout record stays the current one');
    expect(state.actionActivePortalRowNumber, 2);
  });

  testWidgets('a single-step Delete Portal Row button deletes its own row, never the record in hand',
      (tester) async {
    final api = _Api();
    final state = await _pump(
        tester, api, _projectsLayout({'type': 'single_step', 'step_type': 'delete_portal_row', 'params': {'dialog': false}}));
    await _press(tester, 3);
    expect(api.deletes, ['steps/r3']);
    expect(state.currentRecord?['id'], 'p-1');
    expect(_button(3), findsNothing, reason: 'the portal reads its rows again');
  });

  testWidgets('a Delete Portal Row on the second record keeps it current and the found set whole', (tester) async {
    final api = _Api();
    final state = await _pump(
        tester, api, _projectsLayout({'type': 'single_step', 'step_type': 'delete_portal_row', 'params': {'dialog': false}}));
    await state.actionGoToRecord('next');
    await tester.pumpAndSettle();
    expect(state.currentRecord?['id'], 'p-2');
    await _press(tester, 1);
    expect(api.deletes, ['steps/q1']);
    expect(state.currentRecord?['id'], 'p-2', reason: 'the record in hand is read again in place');
    expect(state.actionFoundCount, 2);
    expect(api.reads, greaterThan(0));
  });

  testWidgets('a save already sent ends before the button runs', (tester) async {
    final api = _Api({
      'noop': _script('noop', [_step(1, 'Set Variable', {'name': r'$x', 'value': '1'})]),
    });
    await _pump(tester, api, _projectsLayout({'type': 'perform_script', 'script_id': 'noop', 'script_name': 'noop'}));
    api.gate = Completer<void>();
    await tester.enterText(find.byKey(const ValueKey('portal-cell-r1-label')), 'Plan it');
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pump();
    await tester.tap(_button(1));
    for (var i = 0; i < 10; i++) {
      await tester.pump(const Duration(milliseconds: 50));
    }
    expect(api.ran, isEmpty, reason: 'the script waits for the save in flight');
    api.gate!.complete();
    await _press(tester, 1);
    expect(api.updates.first.$2['label'], 'Plan it');
    expect(api.ran.first, 'noop');
  });

  testWidgets('what was typed in a row is written before the button runs, and the record is not committed',
      (tester) async {
    final api = _Api({
      'noop': _script('noop', [_step(1, 'Set Variable', {'name': r'$x', 'value': '1'})]),
    });
    await _pump(tester, api, _projectsLayout({'type': 'perform_script', 'script_id': 'noop', 'script_name': 'noop'}));
    await tester.enterText(find.byKey(const ValueKey('portal-cell-r1-label')), 'Plan it');
    await tester.pump();
    expect(api.updates, isEmpty, reason: 'still waiting to be saved');
    await _press(tester, 1);
    final (id, values, script) = api.updates.first;
    expect((id, values['label'], script), ('r1', 'Plan it', false));
    expect(api.ran, ['noop']);
  });

  testWidgets('Go to Related Record from a row shows the related records with that row current', (tester) async {
    final api = _Api();
    final key = GlobalKey<DataBrowserWidgetState>();
    tester.view.physicalSize = const Size(1400, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    await tester.pumpWidget(_Window(
      api: api,
      browser: key,
      layout: _projectsLayout({
        'type': 'single_step',
        'step_type': 'go_to_related_record',
        'params': {'related_table': 'Steps', 'layout_name': 'Step Detail', 'show_only_related': true},
      }),
    ));
    await tester.pumpAndSettle();
    await _press(tester, 2);
    final state = key.currentState!;
    expect(state.widget.layout?.name, 'Step Detail');
    expect(state.currentRecord?['id'], 'r2', reason: "the row's related record is made current");
    expect(state.actionFoundCount, 3, reason: 'the records related to the project, not every step');
  });

  testWidgets('Go to Related Record without a row active, and matching the found set', (tester) async {
    final api = _Api();
    final state = await _pump(tester, api, _projectsLayout(null));
    final went = state.actionGoToRelatedRecord(relatedTable: 'Steps', layoutName: 'Step Detail', matchFoundSet: true);
    for (var i = 0; i < 20; i++) {
      await tester.pump(const Duration(milliseconds: 50));
    }
    expect(await went, 0);
    expect(state.currentRecord?['id'], 'r1', reason: 'the first related record');
    expect(state.actionFoundCount, 4, reason: 'the steps of both projects of the found set');

    // On a layout of another table than the related one: refused, and said.
    final back = await _pump(tester, api, _projectsLayout(null));
    expect(await back.actionGoToRelatedRecord(relatedTable: 'Steps'), 3);
    expect(back.goToRelatedRecordMessage, contains('layout of their table'));
    expect(await back.actionGoToRelatedRecord(relatedTable: 'Nowhere', layoutName: 'Step Detail'), 101);

    // A layout with neither portal nor related field reads the graph then.
    final bare = await _pump(
        tester,
        api,
        LayoutDefinitionModel.fromJson({
          'id': 'l-bare',
          'name': 'Projects',
          'table_occurrence': 'Projects',
          'objects': [
            {'id': 'f-code', 'type': 'field', 'x': 40, 'y': 20, 'width': 200, 'height': 30, 'field_binding': {'field_name': 'code'}},
          ],
        }));
    final bareWent = bare.actionGoToRelatedRecord(relatedTable: 'Steps', layoutName: 'Step Detail');
    for (var i = 0; i < 20; i++) {
      await tester.pump(const Duration(milliseconds: 50));
    }
    expect(await bareWent, 0);
    expect(bare.currentRecord?['id'], 'r1');
  });

  testWidgets("a popover button in a row opens its popover with that row's related record", (tester) async {
    final api = _Api();
    final state = await _pump(tester, api, _projectsLayout(null, popover: true));
    Finder shown(String rowId) =>
        find.byWidgetPredicate((w) => w is RelatedFieldView && w.object.id == 'pop-label' && w.row?['id'] == rowId);
    expect(find.byWidgetPredicate((w) => w is RelatedFieldView && w.object.id == 'pop-label'), findsNothing);

    await _press(tester, 3);
    expect(shown('r3'), findsOneWidget);
    expect(find.descendant(of: shown('r3'), matching: find.text('Ship')), findsOneWidget);
    expect(state.actionActivePortalRowNumber, 3);

    await _press(tester, 1);
    expect(shown('r1'), findsOneWidget, reason: "another row's button shows that row");
    await _press(tester, 1);
    expect(find.byWidgetPredicate((w) => w is RelatedFieldView && w.object.id == 'pop-label'), findsNothing,
        reason: 'the same row closes it');
  });
}
