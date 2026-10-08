// #51 — stepping through a script: what runs, what is skipped, where it
// stops, and that the debugger never pretends to run what the runner refuses.

import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/models/script_models.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/script_workspace/script_debugger.dart';
import 'package:file4base_client/features/script_workspace/script_debugger_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// A host that records what the steps did, so the test can tell running a
/// step from merely marking it.
class _RecordingHost implements LayoutActionHost {
  final List<String> did = [];
  bool hasLayout = true;
  bool hasField = true;

  @override
  Future<void> actionNewRecord() async => did.add('new_record');
  @override
  Future<void> actionDuplicateRecord() async => did.add('duplicate_record');
  @override
  Future<void> actionDeleteRecord({required bool confirm}) async => did.add('delete_record');
  @override
  Future<void> actionCommitRecord() async => did.add('commit_records');
  @override
  Future<void> actionRevertRecord() async => did.add('revert_record');
  @override
  Future<void> actionGoToRecord(String target) async => did.add('go_to_record:$target');
  @override
  Future<void> actionEnterFindMode() async => did.add('enter_find_mode');
  @override
  Future<void> actionPerformFind() async => did.add('perform_find');
  @override
  Future<void> actionShowAllRecords() async => did.add('show_all_records');
  @override
  Future<void> actionEnterPreviewMode() async => did.add('enter_preview_mode');
  @override
  Future<bool> actionGoToLayout(String layoutName) async {
    did.add('go_to_layout:$layoutName');
    return hasLayout;
  }

  @override
  Future<bool> actionSetField(String field, String value) async {
    did.add('set_field:$field=$value');
    return hasField;
  }

  @override
  Future<void> actionShowDialog(String title, String message) async => did.add('show_dialog:$title');
  @override
  Future<void> actionOpenUrl(String url) async => did.add('open_url:$url');
  @override
  String actionResolveText(String text) => text;
}

ScriptStepModel _step(int sequence, String type,
        {Map<String, dynamic> params = const {}, bool enabled = true}) =>
    ScriptStepModel(
      id: 's$sequence',
      sequenceIdx: sequence,
      stepType: type,
      params: params,
      isEnabled: enabled,
    );

ScriptDebugSession _session(List<ScriptStepModel> steps, _RecordingHost host) {
  final script = ScriptModel(id: 'sc1', name: 'Greet', steps: steps);
  return ScriptDebugSession(
    script: script,
    runner: LayoutActionRunner(
      apiClient: ApiClient(baseUrl: 'http://test-server:8080'),
      host: host,
    ),
  )..begin();
}

void main() {
  test('one step runs at a time, and the session says which is next', () async {
    final host = _RecordingHost();
    final session = _session([
      _step(1, 'new_record'),
      _step(2, 'commit_records'),
    ], host);

    expect(session.currentStep?.sequence, 1);
    expect(session.steps.first.state, DebugStepState.current);
    expect(session.message, isNull);

    await session.step();
    expect(host.did, ['new_record'], reason: 'the step really ran');
    expect(session.steps[0].state, DebugStepState.done);
    expect(session.steps[1].state, DebugStepState.current);
    expect(session.finished, isFalse);

    await session.step();
    expect(host.did, ['new_record', 'commit_records']);
    expect(session.finished, isTrue);
    expect(session.message, contains('ran to the end'));
  });

  test('a disabled step is skipped, exactly as running the script skips it', () async {
    final host = _RecordingHost();
    final session = _session([
      _step(1, 'new_record', enabled: false),
      _step(2, 'commit_records'),
    ], host);

    await session.continueRun();
    expect(host.did, ['commit_records']);
    expect(session.steps[0].state, DebugStepState.skipped);
    expect(session.steps[1].state, DebugStepState.done);
  });

  test('a step the runner refuses stops the script and says which one', () async {
    final host = _RecordingHost();
    final session = _session([
      _step(1, 'new_record'),
      _step(2, 'set_variable', params: {'name': r'$total'}),
      _step(3, 'commit_records'),
    ], host);

    await session.continueRun();

    expect(host.did, ['new_record'], reason: 'nothing after the refused step ran');
    expect(session.steps[1].state, DebugStepState.failed);
    expect(session.steps[2].state, isNot(DebugStepState.done));
    expect(session.finished, isTrue);
    expect(session.message, contains('Step 2'));
    expect(session.message, contains('not supported'));
  });

  test('a step that fails on its own terms stops the script too', () async {
    final host = _RecordingHost()..hasLayout = false;
    final session = _session([
      _step(1, 'go_to_layout', params: {'layout_name': 'Nowhere'}),
      _step(2, 'commit_records'),
    ], host);

    await session.continueRun();
    expect(session.steps[0].state, DebugStepState.failed);
    expect(session.message, contains('Nowhere'));
    expect(host.did, ['go_to_layout:Nowhere']);
  });

  test('Continue stops at a breakpoint, and again at the next one', () async {
    final host = _RecordingHost();
    final session = _session([
      _step(1, 'new_record'),
      _step(2, 'commit_records'),
      _step(3, 'show_all_records'),
      _step(4, 'revert_record'),
    ], host);

    session.toggleBreakpoint(3);
    await session.continueRun();

    expect(host.did, ['new_record', 'commit_records']);
    expect(session.currentStep?.sequence, 3);
    expect(session.message, contains('breakpoint on step 3'));
    expect(session.finished, isFalse);

    // Continuing from a breakpoint must not stop on that same breakpoint.
    await session.continueRun();
    expect(host.did, ['new_record', 'commit_records', 'show_all_records', 'revert_record']);
    expect(session.finished, isTrue);
  });

  test('halt ends the script where it is', () async {
    final host = _RecordingHost();
    final session = _session([
      _step(1, 'new_record'),
      _step(2, 'halt_script'),
      _step(3, 'commit_records'),
    ], host);

    await session.continueRun();
    expect(host.did, ['new_record']);
    expect(session.finished, isTrue);
    expect(session.message, contains('ended at step 2'));
  });

  test('stopping ends the session, and starting over keeps the breakpoints', () async {
    final host = _RecordingHost();
    final session = _session([
      _step(1, 'new_record'),
      _step(2, 'commit_records'),
    ], host);

    session.toggleBreakpoint(2);
    await session.step();
    session.stop();
    expect(session.finished, isTrue);
    expect(session.message, contains('Stopped at step 2'));

    session.restart();
    expect(session.finished, isFalse);
    expect(session.currentStep?.sequence, 1);
    expect(session.steps.every((s) => s.state != DebugStepState.done), isTrue);
    expect(session.steps[1].breakpoint, isTrue, reason: 'breakpoints survive starting over');
  });

  test('the steps are shown in their running order, whatever order they came in', () {
    final session = _session([
      _step(3, 'commit_records'),
      _step(1, 'new_record'),
      _step(2, 'show_dialog'),
    ], _RecordingHost());

    expect([for (final step in session.steps) step.sequence], [1, 2, 3]);
  });

  group('The dialog', () {
    testWidgets('lists the steps, steps through them and marks where it is', (tester) async {
      tester.view.physicalSize = const Size(1400, 1000);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      final host = _RecordingHost();
      final mock = MockClient((request) async {
        if (request.url.path == '/api/v1/schemas/scripts') {
          return http.Response(
            jsonEncode([
              {'id': 'sc1', 'name': 'Greet customer', 'is_active': true, 'steps': []},
            ]),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        if (request.url.path == '/api/v1/schemas/scripts/sc1') {
          return http.Response(
            jsonEncode({
              'id': 'sc1',
              'name': 'Greet customer',
              'is_active': true,
              'steps': [
                {'id': 's1', 'sequence_idx': 1, 'step_type': 'new_record', 'is_enabled': true},
                {'id': 's2', 'sequence_idx': 2, 'step_type': 'set_variable', 'is_enabled': true},
              ],
            }),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        return http.Response('Not found', 404);
      });

      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: ScriptDebuggerDialog(
            apiClient: ApiClient(baseUrl: 'http://test-server:8080', httpClient: mock),
            host: host,
          ),
        ),
      ));
      await tester.pumpAndSettle();

      expect(find.text('new_record'), findsOneWidget);
      expect(find.text('set_variable'), findsOneWidget);
      expect(find.textContaining('About to run step 1'), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('debug-step')));
      await tester.pumpAndSettle();
      expect(host.did, ['new_record']);
      expect(find.textContaining('About to run step 2'), findsOneWidget);

      // The step the runner will not take stops the script and says so,
      // rather than being marked as run.
      await tester.tap(find.byKey(const ValueKey('debug-step')));
      await tester.pumpAndSettle();
      expect(host.did, ['new_record'], reason: 'the refused step did nothing');
      expect(find.textContaining('Step 2 stopped the script'), findsOneWidget);
      expect(find.textContaining('not supported'), findsOneWidget);

      // With the session finished, Step and Continue are off and Start over
      // is offered.
      MenuOrButton(Key key) => tester.widget(find.byKey(key));
      expect((MenuOrButton(const ValueKey('debug-step')) as OutlinedButton).onPressed, isNull);
      expect((MenuOrButton(const ValueKey('debug-continue')) as FilledButton).onPressed, isNull);
      expect((MenuOrButton(const ValueKey('debug-restart')) as TextButton).onPressed, isNotNull);
    });

    testWidgets('without a layout on screen it says where to start it from', (tester) async {
      tester.view.physicalSize = const Size(1400, 1000);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      final mock = MockClient((request) async {
        final body = request.url.path.endsWith('/sc1')
            ? jsonEncode({'id': 'sc1', 'name': 'Greet customer', 'is_active': true, 'steps': []})
            : jsonEncode([
                {'id': 'sc1', 'name': 'Greet customer', 'is_active': true, 'steps': []},
              ]);
        return http.Response(body, 200, headers: {'content-type': 'application/json'});
      });

      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: ScriptDebuggerDialog(
            apiClient: ApiClient(baseUrl: 'http://test-server:8080', httpClient: mock),
            host: null,
          ),
        ),
      ));
      await tester.pumpAndSettle();

      expect(find.textContaining('open a layout in Browse mode'), findsOneWidget);
    });
  });
}
