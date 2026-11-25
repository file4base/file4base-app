// #26 — the record, mode and field triggers of a layout run in Browse and
// Find mode as FileMaker runs them: a pre-event trigger whose script exits
// with False cancels its event, and a trigger never runs again from its own
// script.
import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/layout_engine/script_trigger_runtime.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

final _table = TableModel(id: 't1', name: 'tasks', displayName: 'Tasks', columns: const [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'title', displayName: 'Title', fieldType: 'varchar'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'notes', displayName: 'Notes', fieldType: 'varchar'),
]);

Map<String, dynamic> _step(int n, String type, [Map<String, dynamic> params = const {}]) =>
    {'id': 's$n', 'sequence_idx': n, 'step_type': type, 'params': params, 'is_enabled': true};

/// Scripts by id: each one either exits with [result] or runs [steps].
ScriptModel _script(String id, {String? result, List<Map<String, dynamic>>? steps}) => ScriptModel.fromJson({
      'id': id,
      'name': id,
      'is_active': true,
      'steps': steps ?? [if (result != null) _step(1, 'Exit Script', {'result': result})],
    });

class _Api extends ApiClient {
  _Api(this.scripts) : super(baseUrl: 'http://localhost:1');
  final Map<String, ScriptModel> scripts;

  /// The scripts run, in order.
  final List<String> ran = [];
  final List<Map<String, dynamic>> updates = [];
  final Map<String, Map<String, dynamic>> stored = {
    '1': {'id': '1', 'title': 'First', 'notes': 'one'},
    '2': {'id': '2', 'title': 'Second', 'notes': 'two'},
    '3': {'id': '3', 'title': 'Third', 'notes': 'three'},
  };

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      stored.values.map((r) => Map<String, dynamic>.of(r)).toList();

  @override
  Future<Map<String, dynamic>> getRow(String table, String id) async => Map<String, dynamic>.of(stored[id]!);

  @override
  Future<List<ScriptModel>> listScripts({String? database}) async => scripts.values.toList();

  @override
  Future<ScriptModel> getScript(String id, {String? database}) async {
    ran.add(id);
    return scripts[id]!;
  }

  @override
  Future<List<ValueListModel>> listValueLists() async => const [];

  /// The finds run.
  final List<List<Map<String, dynamic>>> finds = [];

  @override
  Future<List<Map<String, dynamic>>> executeFind(String table, List<Map<String, dynamic>> requests,
      {int limit = 500, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async {
    finds.add(requests);
    return [Map<String, dynamic>.of(stored['2']!)];
  }

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> data,
      {bool script = false, bool overrideValidation = false}) async {
    updates.add({'id': id, ...data});
    stored[id]!.addAll(data);
    return Map<String, dynamic>.of(stored[id]!);
  }
}

/// A trigger of the list, enabled.
Map<String, dynamic> _trigger(String id, String event, String script, {String? object, List<String>? modes}) => {
      'id': id,
      'event': event,
      'type': 'perform_script',
      'script_id': script,
      'script_name': script,
      'enabled': true,
      'object_id': ?object,
      'modes': ?modes,
    };

LayoutDefinitionModel _layout(List<Map<String, dynamic>> triggers) => LayoutDefinitionModel.fromJson({
      'id': 'l1',
      'name': 'Tasks',
      'table_occurrence': 'Tasks',
      'objects': [
        {'id': 'f-title', 'type': 'field', 'x': 40, 'y': 40, 'width': 220, 'height': 30, 'field_binding': {'field_name': 'title'}},
        {'id': 'f-notes', 'type': 'field', 'x': 40, 'y': 90, 'width': 220, 'height': 30, 'field_binding': {'field_name': 'notes'}},
      ],
      'script_triggers': triggers,
    });

Future<DataBrowserWidgetState> _pump(WidgetTester tester, _Api api, LayoutDefinitionModel layout) async {
  tester.view.physicalSize = const Size(1400, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final key = GlobalKey<DataBrowserWidgetState>();
  var mode = OperationalMode.browse;
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: StatefulBuilder(
        builder: (context, setState) => DataBrowserWidget(
          key: key,
          table: _table,
          apiClient: api,
          mode: mode,
          layout: layout,
          onModeChanged: (m) => setState(() => mode = m),
        ),
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return key.currentState!;
}

Finder _box(String text) => find.byWidgetPredicate((w) => w is TextField && w.controller?.text == text);

/// Runs [work] while the frames and timers it waits for go by.
Future<T> _settled<T>(WidgetTester tester, Future<T> work) async {
  await tester.pumpAndSettle();
  return work;
}

void main() {
  group('script results', () {
    test('False, zero and text without a number cancel; no result, True and numbers do not', () {
      expect(scriptResultCancels(null), isFalse);
      expect(scriptResultCancels(''), isFalse);
      expect(scriptResultCancels('0'), isTrue);
      expect(scriptResultCancels('abc'), isTrue);
      expect(scriptResultCancels('1'), isFalse);
      expect(scriptResultCancels('a1'), isFalse);
      expect(scriptResultCancels('-2.5'), isFalse);
    });

    test('Exit Script [Result: False] returns 0, and True 1', () async {
      final api = _Api({'no': _script('no', result: 'False'), 'yes': _script('yes', result: 'True')});
      final runner = LayoutActionRunner(apiClient: api, host: _NoHost());
      expect((await runner.run(const ButtonActionModel.performScript(id: 'no', name: 'no'))).scriptResult, '0');
      expect((await runner.run(const ButtonActionModel.performScript(id: 'yes', name: 'yes'))).scriptResult, '1');
    });
  });

  group('the runtime', () {
    test('a trigger does not run again while its own script runs, and nesting is capped', () async {
      late ScriptTriggerRuntime runtime;
      var runs = 0;
      final trigger = ButtonActionModel.fromJson(_trigger('t', 'OnRecordLoad', 's'));
      runtime = ScriptTriggerRuntime(perform: (t) async {
        runs++;
        // The script sets its own event off again.
        expect(await runtime.fire(t), isTrue, reason: 'the event goes ahead, the trigger does not run');
        return const LayoutActionResult.ok();
      });
      expect(await runtime.fire(trigger), isTrue);
      expect(runs, 1);

      var depth = 0;
      final nested = ScriptTriggerRuntime(perform: (t) async => const LayoutActionResult.ok());
      late ScriptTriggerRuntime deep;
      deep = ScriptTriggerRuntime(perform: (t) async {
        depth++;
        await deep.fire(ButtonActionModel.fromJson(_trigger('t$depth', 'OnObjectEnter', 's')));
        return const LayoutActionResult.ok();
      });
      await deep.fire(ButtonActionModel.fromJson(_trigger('t0', 'OnObjectEnter', 's')));
      expect(depth, ScriptTriggerRuntime.maxNesting);
      expect(await nested.fire(null), isTrue);
    });

    test('a trigger set off again by something else while its script runs gives the answer of that run', () async {
      final gate = Completer<void>();
      var runs = 0;
      final runtime = ScriptTriggerRuntime(perform: (t) async {
        runs++;
        await gate.future;
        return const LayoutActionResult.ok(scriptResult: '0');
      });
      final commit = ButtonActionModel.fromJson(_trigger('t', 'OnRecordCommit', 's'));
      final first = runtime.fire(commit);
      final second = runtime.fire(commit);
      gate.complete();
      expect(await first, isFalse);
      expect(await second, isFalse, reason: 'a second commit is refused too, never let through');
      expect(runs, 1);
    });

    test('post-event triggers set off together run one after the other, and settle waits for them', () async {
      final order = <String>[];
      final runtime = ScriptTriggerRuntime(perform: (t) async {
        order.add('start ${t.triggerId}');
        await Future<void>.delayed(const Duration(milliseconds: 5));
        order.add('end ${t.triggerId}');
        return const LayoutActionResult.ok();
      });
      final mark = runtime.mark();
      runtime
        ..post(ButtonActionModel.fromJson(_trigger('a', 'OnModeEnter', 's')))
        ..post(ButtonActionModel.fromJson(_trigger('b', 'OnRecordLoad', 's')));
      await runtime.settle(mark);
      expect(order, ['start a', 'end a', 'start b', 'end b']);
    });

    test('the layout runs only enabled list triggers, in the modes they name', () {
      final layout = _layout([
        _trigger('load', 'OnRecordLoad', 's'),
        {..._trigger('off', 'OnRecordCommit', 's'), 'enabled': false},
        _trigger('enter', 'OnObjectEnter', 's', object: 'f-title', modes: ['browse', 'find']),
        _trigger('findonly', 'OnObjectExit', 's', object: 'f-title', modes: ['find']),
        {..._trigger('old', 'OnRecordRevert', 's')}..remove('enabled'),
      ]);
      expect(layout.runningTrigger('OnRecordLoad')?.triggerId, 'load');
      expect(layout.runningTrigger('OnRecordCommit'), isNull, reason: 'switched off');
      expect(layout.runningTrigger('OnRecordRevert'), isNull, reason: 'a list entry runs only when it says enabled');
      expect(layout.runningTrigger('OnObjectEnter', objectId: 'f-title', mode: 'find')?.triggerId, 'enter');
      expect(layout.runningTrigger('OnObjectExit', objectId: 'f-title'), isNull, reason: 'Find mode only');
      expect(layout.runningTrigger('OnObjectExit', objectId: 'f-title', mode: 'find')?.triggerId, 'findonly');
      expect(layout.runningTrigger('OnRecordLoad', mode: 'find'), isNull, reason: 'no record loads in Find mode');
    });

    test('object and record triggers can be enabled in place; a button is not a field', () {
      final layout = LayoutDefinitionModel.fromJson({
        'id': 'l',
        'name': 'L',
        'objects': [
          {'id': 'f', 'type': 'field', 'x': 0, 'y': 0, 'width': 10, 'height': 10, 'field_binding': {'field_name': 'title'}},
          {'id': 'b', 'type': 'button', 'x': 0, 'y': 0, 'width': 10, 'height': 10},
        ],
        'script_triggers': [
          {..._trigger('a', 'OnObjectValidate', 's', object: 'f'), 'enabled': false},
          {..._trigger('b', 'OnObjectEnter', 's', object: 'b'), 'enabled': false},
          {..._trigger('c', 'OnRecordCommit', 's'), 'enabled': false},
          {..._trigger('d', 'OnObjectKeystroke', 's', object: 'f'), 'enabled': false},
        ],
      });
      final byId = {for (final t in layout.scriptTriggers) t.triggerId!: t};
      expect(layout.whyTriggerCannotRun(byId['a']!), isNull);
      expect(layout.whyTriggerCannotRun(byId['b']!), contains('fields'));
      expect(layout.whyTriggerCannotRun(byId['c']!), isNull);
      expect(layout.whyTriggerCannotRun(byId['d']!), contains('OnObjectKeystroke'));
      final on = layout.enableStoredTrigger('a').enableStoredTrigger('c');
      expect(on.runningTrigger('OnObjectValidate', objectId: 'f')?.triggerId, 'a');
      expect(on.runningTrigger('OnRecordCommit')?.triggerId, 'c');
      expect(on.scriptTriggers, hasLength(4), reason: 'switched where they are');
    });
  });

  group('in the window', () {
    testWidgets('a layout opens on OnModeEnter then OnRecordLoad, which runs for each record gone to',
        (tester) async {
      final api = _Api({'mode': _script('mode'), 'load': _script('load')});
      final state = await _pump(tester, api, _layout([
        _trigger('t1', 'OnRecordLoad', 'load'),
        _trigger('t2', 'OnModeEnter', 'mode'),
      ]));
      expect(api.ran, ['mode', 'load']);
      await _settled(tester, state.goToRecord(1));
      expect(api.ran, ['mode', 'load', 'load']);
      await _settled(tester, state.goToRecord(1));
      expect(api.ran, hasLength(3), reason: 'the same record is not loaded again');
    });

    testWidgets('a trigger whose script sets its own event off does not run again', (tester) async {
      final api = _Api({
        'next': _script('next', steps: [_step(1, 'Go to Record/Request/Page', {'target': 'next'})]),
      });
      final state = await _pump(tester, api, _layout([_trigger('t', 'OnRecordLoad', 'next')]));
      await tester.pumpAndSettle();
      expect(api.ran, ['next'], reason: 'the record it goes to is loaded without the trigger');
      expect(state.currentIndex, 1);
    });

    testWidgets('OnRecordCommit False keeps a changed record, and runs only when it changed', (tester) async {
      final api = _Api({'commit': _script('commit', result: 'False')});
      final state = await _pump(tester, api, _layout([_trigger('t', 'OnRecordCommit', 'commit')]));
      expect(await _settled(tester, state.goToRecord(1)), isTrue, reason: 'nothing changed: no commit');
      expect(api.ran, isEmpty);

      await tester.enterText(_box('Second'), 'Changed');
      expect(await _settled(tester, state.goToRecord(2)), isFalse);
      expect(api.ran, ['commit']);
      expect(state.currentIndex, 1, reason: 'the user stays on the record');
    });

    testWidgets('OnRecordRevert False keeps the changes', (tester) async {
      final api = _Api({'revert': _script('revert', result: '0')});
      final state = await _pump(tester, api, _layout([_trigger('t', 'OnRecordRevert', 'revert')]));
      await tester.enterText(_box('First'), 'Typed');
      await _settled(tester, state.actionRevertRecord());
      expect(api.ran, ['revert']);
      expect(_box('Typed'), findsOneWidget);
    });

    testWidgets('OnObjectEnter, OnObjectModify and OnObjectExit run as a field is entered, typed in and left',
        (tester) async {
      final api = _Api({'enter': _script('enter'), 'modify': _script('modify'), 'exit': _script('exit')});
      await _pump(tester, api, _layout([
        _trigger('a', 'OnObjectEnter', 'enter', object: 'f-title'),
        _trigger('b', 'OnObjectModify', 'modify', object: 'f-title'),
        _trigger('c', 'OnObjectExit', 'exit', object: 'f-title'),
      ]));
      await tester.tap(_box('First'));
      await tester.pumpAndSettle();
      expect(api.ran, ['enter']);
      await tester.enterText(_box('First'), 'First!');
      await tester.pumpAndSettle();
      expect(api.ran, ['enter', 'modify']);
      await tester.tap(_box('one'));
      await tester.pumpAndSettle();
      expect(api.ran, ['enter', 'modify', 'exit']);
    });

    testWidgets('OnObjectExit False keeps the field active', (tester) async {
      final api = _Api({'exit': _script('exit', result: 'False'), 'enter': _script('enter')});
      await _pump(tester, api, _layout([
        _trigger('a', 'OnObjectExit', 'exit', object: 'f-title'),
        _trigger('b', 'OnObjectEnter', 'enter', object: 'f-notes'),
      ]));
      await tester.tap(_box('First'));
      await tester.pumpAndSettle();
      await tester.tap(_box('one'));
      await tester.pumpAndSettle();
      expect(api.ran, ['exit']);
      final title = tester.widget<TextField>(_box('First'));
      expect(title.focusNode?.hasFocus, isTrue, reason: 'the focus went back');
      expect(api.ran, isNot(contains('enter')), reason: 'the other field was not entered');
    });

    testWidgets('OnObjectValidate False keeps a changed value unsaved; True saves it, then OnObjectSave runs',
        (tester) async {
      final scripts = {'validate': _script('validate', result: 'False'), 'save': _script('save')};
      final api = _Api(scripts);
      await _pump(tester, api, _layout([
        _trigger('a', 'OnObjectValidate', 'validate', object: 'f-title'),
        _trigger('b', 'OnObjectSave', 'save', object: 'f-title'),
      ]));
      await tester.tap(_box('First'));
      await tester.enterText(_box('First'), 'Renamed');
      await tester.pump(const Duration(seconds: 2));
      expect(api.updates, isEmpty, reason: 'not saved as it is typed');
      await tester.tap(_box('one'));
      await tester.pumpAndSettle();
      expect(api.ran, ['validate']);
      expect(api.updates, isEmpty);
      expect(tester.widget<TextField>(_box('Renamed')).focusNode?.hasFocus, isTrue);

      scripts['validate'] = _script('validate', result: '1');
      await tester.tap(_box('one'));
      await tester.pumpAndSettle();
      expect(api.ran, ['validate', 'validate', 'save']);
      expect(api.updates.single, {'id': '1', 'title': 'Renamed'});
    });

    testWidgets('OnModeExit False keeps the window in Browse mode; OnModeEnter runs in the mode entered',
        (tester) async {
      final scripts = {'exit': _script('exit', result: 'False'), 'enter': _script('enter')};
      final api = _Api(scripts);
      final state = await _pump(tester, api, _layout([
        _trigger('a', 'OnModeExit', 'exit'),
        _trigger('b', 'OnModeEnter', 'enter', modes: ['find']),
      ]));
      expect(api.ran, isEmpty, reason: 'OnModeEnter is set for Find mode only');
      await _settled(tester, state.actionEnterFindMode());
      expect(api.ran, ['exit']);
      expect(find.textContaining('Search Title'), findsNothing, reason: 'still in Browse mode');

      scripts['exit'] = _script('exit');
      await _settled(tester, state.actionEnterFindMode());
      await tester.pumpAndSettle();
      expect(api.ran, ['exit', 'exit', 'enter']);
    });

    testWidgets('a field trigger set for Find mode runs on the find request box', (tester) async {
      final api = _Api({'enter': _script('enter'), 'browse': _script('browse')});
      final state = await _pump(tester, api, _layout([
        _trigger('a', 'OnObjectEnter', 'enter', object: 'f-title', modes: ['browse', 'find']),
        _trigger('b', 'OnObjectEnter', 'browse', object: 'f-notes'),
      ]));
      await _settled(tester, state.actionEnterFindMode());
      await tester.tap(find.byWidgetPredicate((w) => w is TextField && w.decoration?.hintText == 'Search Title...'));
      await tester.pumpAndSettle();
      await tester.tap(find.byWidgetPredicate((w) => w is TextField && w.decoration?.hintText == 'Search Notes...'));
      await tester.pumpAndSettle();
      expect(api.ran, ['enter'], reason: 'the Notes trigger runs in Browse mode only');
    });

    testWidgets('Go to Field leaves the active field and enters the one gone to, with their triggers',
        (tester) async {
      final api = _Api({'exit': _script('exit'), 'enter': _script('enter')});
      final state = await _pump(tester, api, _layout([
        _trigger('a', 'OnObjectExit', 'exit', object: 'f-title'),
        _trigger('b', 'OnObjectEnter', 'enter', object: 'f-notes'),
      ]));
      final runner = LayoutActionRunner(apiClient: api, host: state);
      await _settled(tester, runner.runStep('go_to_field', {'field': 'title'}, depth: 0));
      await _settled(tester, runner.runStep('go_to_field', {'field': 'notes'}, depth: 0));
      expect(api.ran, ['exit', 'enter']);
      expect(runner.resolveValue('Get(ActiveFieldTableName)', {}), 'Tasks');
    });
    testWidgets('after OnRecordCommit False, a field typed in is saved with the record only', (tester) async {
      final scripts = {'commit': _script('commit', result: 'False')};
      final api = _Api(scripts);
      final state = await _pump(tester, api, _layout([_trigger('t', 'OnRecordCommit', 'commit')]));
      await tester.enterText(_box('First'), 'Typed');
      final moved = state.goToRecord(1);
      await tester.pump();
      expect(await moved, isFalse);
      await tester.pump(const Duration(seconds: 2));
      expect(api.updates, isEmpty, reason: "the field's pending save was dropped");
      await tester.enterText(_box('Typed'), 'Typed more');
      await tester.pump(const Duration(seconds: 2));
      expect(api.updates, isEmpty, reason: 'held until the record is committed');

      scripts['commit'] = _script('commit');
      expect(await _settled(tester, state.goToRecord(1)), isTrue);
      expect(api.updates.single, {'id': '1', 'title': 'Typed more'});
    });

    testWidgets('OnObjectValidate reads the value typed, and the record keeps its own when it refuses', (tester) async {
      // The script's result is the field: a number lets the value in, 0 and
      // text refuse it, so it says which value the script read.
      final api = _Api({'validate': _script('validate', result: 'title')});
      final state = await _pump(tester, api, _layout([_trigger('a', 'OnObjectValidate', 'validate', object: 'f-title')]));
      await tester.tap(_box('First'));
      await tester.enterText(_box('First'), '7');
      await tester.tap(_box('one'));
      await tester.pumpAndSettle();
      expect(api.updates.single, {'id': '1', 'title': '7'}, reason: 'the script read 7, not First');

      await tester.tap(_box('7'));
      await tester.enterText(_box('7'), '0');
      await tester.tap(_box('one'));
      await tester.pumpAndSettle();
      expect(api.updates, hasLength(1), reason: 'the script read 0, not 7, and refused it');
      expect(state.currentRecord!['title'], '7', reason: 'the value staged for the script is taken back');
    });

    testWidgets('OnRecordLoad on a layout of another table waits for its records', (tester) async {
      final gate = Completer<void>();
      final seen = <String?>[];
      final key = GlobalKey<DataBrowserWidgetState>();
      final api = _GatedApi({'load': _script('load')}, gate, () => seen.add(key.currentState?.currentRecord?['id']?.toString()));
      final notes = TableModel(id: 't2', name: 'notes', displayName: 'Notes', columns: const [
        ColumnModel(id: 'n0', tableId: 't2', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
        ColumnModel(id: 'n1', tableId: 't2', name: 'title', displayName: 'Title', fieldType: 'varchar'),
      ]);
      final notesLayout = LayoutDefinitionModel.fromJson({
        'id': 'l2',
        'name': 'Notes',
        'table_occurrence': 'Notes',
        'objects': [
          {'id': 'n-title', 'type': 'field', 'x': 40, 'y': 40, 'width': 220, 'height': 30, 'field_binding': {'field_name': 'title'}},
        ],
        'script_triggers': [_trigger('t', 'OnRecordLoad', 'load')],
      });
      var table = _table;
      var layout = _layout(const []);
      late StateSetter rebuild;
      tester.view.physicalSize = const Size(1400, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(builder: (context, setState) {
            rebuild = setState;
            return DataBrowserWidget(
                key: key, table: table, apiClient: api, mode: OperationalMode.browse, layout: layout, onModeChanged: (_) {});
          }),
        ),
      ));
      await tester.pumpAndSettle();
      rebuild(() {
        table = notes;
        layout = notesLayout;
      });
      // Not settled: the records being read show a progress indicator.
      for (var i = 0; i < 5; i++) {
        await tester.pump(const Duration(milliseconds: 50));
      }
      expect(api.ran, isEmpty, reason: 'its records are still being read');
      gate.complete();
      await tester.pumpAndSettle();
      expect(api.ran, ['load']);
      expect(seen, ['n1'], reason: 'the record of the layout gone to, not the one shown before');
    });

    testWidgets('a script Perform Find asks OnModeExit before the found set is replaced', (tester) async {
      final api = _Api({'exit': _script('exit', result: 'False')});
      final state = await _pump(tester, api, _layout([_trigger('a', 'OnModeExit', 'exit', modes: ['find'])]));
      await _settled(tester, state.actionEnterFindMode());
      final runner = LayoutActionRunner(apiClient: api, host: state);
      final session = ScriptSessionState();
      await _settled(
          tester,
          runner.runStep(
              'perform_find',
              {
                'restore': true,
                'find_requests': [
                  {
                    'criteria': [
                      {'field': 'title', 'value': 'Second'},
                    ],
                  },
                ],
              },
              depth: 0,
              callContext: ScriptCallContext(session: session)));
      expect(api.ran, ['exit']);
      expect(session.lastError, 20);
      expect(api.finds, isEmpty, reason: 'nothing was found: the window stays in Find mode');
      expect(find.textContaining('Search Title'), findsWidgets);
    });
  });
}

/// An API whose second table's records are read once [gate] opens, and
/// that tells [onScript] when a script is read, that is, as it runs.
class _GatedApi extends _Api {
  _GatedApi(super.scripts, this.gate, this.onScript);
  final Completer<void> gate;
  final void Function() onScript;

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
      {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async {
    if (table != 'notes') return super.listRows(table, limit: limit, offset: offset, sortBy: sortBy, sortAsc: sortAsc, sort: sort);
    await gate.future;
    return [
      {'id': 'n1', 'title': 'Note'},
    ];
  }

  @override
  Future<ScriptModel> getScript(String id, {String? database}) {
    onScript();
    return super.getScript(id, database: database);
  }
}

class _NoHost implements LayoutActionHost {
  @override
  dynamic noSuchMethod(Invocation invocation) => Future<void>.value();

  @override
  String actionResolveText(String text) => text;
}
