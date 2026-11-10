// #93, #94, #95 — what a script really does: which branch runs, what a
// variable holds, and how far a Halt Script reaches.
//
// The host is instrumented, not replaced: these tests drive the real
// LayoutActionRunner and record the operations it asks the host to perform,
// so a branch that "reports success" without doing anything is visible.

import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/script_workspace/script_debugger.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

class _RecordingHost implements LayoutActionHost {
  final List<String> did = [];
  final Map<String, String> fields = {};

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
    return true;
  }

  @override
  Future<bool> actionSetField(String field, String value) async {
    did.add('set_field:$field=$value');
    fields[field] = value;
    return true;
  }

  @override
  Future<void> actionShowDialog(String title, String message) async => did.add('show_dialog:$title');
  @override
  Future<void> actionOpenUrl(String url) async => did.add('open_url:$url');
  @override
  Future<void> actionEnterBrowseMode() async => did.add('enter_browse_mode');
  @override
  Future<void> actionAdjustWindow(String windowMode) async => did.add('adjust_window:$windowMode');
  @override
  Future<void> actionReLogin() async => did.add('re_login');
  @override
  Future<void> actionCloseFile() async => did.add('close_file');
  @override
  Future<void> actionExitApplication() async => did.add('exit_application');

  /// Only the fields this host has resolve; anything else comes back as the
  /// token itself, which is how the runner tells "no such field" from "empty".
  @override
  String actionResolveText(String text) {
    final match = RegExp(r'^\{\{(.+)\}\}$').firstMatch(text.trim());
    if (match != null) {
      return fields[match.group(1)] ?? text;
    }
    return text;
  }
}

Map<String, dynamic> _stepJson(int sequence, String type, [Map<String, dynamic> params = const {}]) => {
      'id': 's$sequence',
      'sequence_idx': sequence,
      'step_type': type,
      'params': params,
      'is_enabled': true,
    };

/// A runner whose API serves the given scripts by id and by name.
LayoutActionRunner _runner(_RecordingHost host, Map<String, Map<String, dynamic>> scripts) {
  final mock = MockClient((request) async {
    final path = request.url.path;
    if (path == '/api/v1/schemas/scripts') {
      return http.Response(jsonEncode(scripts.values.toList()), 200,
          headers: {'content-type': 'application/json'});
    }
    final id = path.split('/').last;
    final script = scripts[id];
    if (script == null) return http.Response('Not found', 404);
    return http.Response(jsonEncode(script), 200, headers: {'content-type': 'application/json'});
  });
  return LayoutActionRunner(
    apiClient: ApiClient(baseUrl: 'http://test-server:8080', httpClient: mock),
    host: host,
  );
}

Map<String, dynamic> _script(String id, String name, List<Map<String, dynamic>> steps) =>
    {'id': id, 'name': name, 'is_active': true, 'steps': steps};

Future<LayoutActionResult> _runScript(LayoutActionRunner runner, String id, String name) =>
    runner.run(ButtonActionModel.performScript(id: id, name: name));

void main() {
  group('a numeric condition picks the branch its arithmetic picks (#93)', () {
    Future<List<String>> operationsFor(String condition) async {
      final host = _RecordingHost();
      final runner = _runner(host, {
        'sc1': _script('sc1', 'Branch', [
          _stepJson(1, 'if', {'condition': condition}),
          _stepJson(2, 'new_record'),
          _stepJson(3, 'end_if'),
        ]),
      });
      final result = await _runScript(runner, 'sc1', 'Branch');
      expect(result.completed, isTrue, reason: result.message);
      return host.did;
    }

    test('1 = 0 is false, so the branch does not run', () async {
      expect(await operationsFor('1 = 0'), isEmpty);
    });

    test('2 > 1 is true, so the branch runs', () async {
      expect(await operationsFor('2 > 1'), ['new_record']);
    });

    test('10 > 9 compares numbers, not text', () async {
      expect(await operationsFor('10 > 9'), ['new_record']);
    });

    test('1 <= 1 is read as one operator, not as < and =', () async {
      expect(await operationsFor('1 <= 1'), ['new_record']);
    });

    test('a non-zero number on its own holds', () async {
      expect(await operationsFor('1'), ['new_record']);
    });

    test('zero does not', () async {
      expect(await operationsFor('0'), isEmpty);
    });

    test('"yes" = "yes" still holds', () async {
      expect(await operationsFor('"yes" = "yes"'), ['new_record']);
    });
  });

  test('a condition File4Base cannot evaluate stops the script (#93)', () async {
    final host = _RecordingHost();
    final runner = _runner(host, {
      'sc1': _script('sc1', 'Branch', [
        _stepJson(1, 'if', {'condition': 'quantity * rate'}),
        _stepJson(2, 'new_record'),
        _stepJson(3, 'end_if'),
      ]),
    });

    final result = await _runScript(runner, 'sc1', 'Branch');
    expect(result.completed, isFalse, reason: 'a guessed branch is worse than a stop');
    expect(result.message, contains('step 1'));
    expect(host.did, isEmpty);
  });

  test('a numeric Set Variable value reaches the field it is written to (#93)', () async {
    final host = _RecordingHost()..fields['count'] = '';
    final runner = _runner(host, {
      'sc1': _script('sc1', 'Count', [
        _stepJson(1, 'set_variable', {'name': r'$count', 'value': '2'}),
        _stepJson(2, 'set_field', {'field': 'count', 'value': r'$count'}),
      ]),
    });

    final result = await _runScript(runner, 'sc1', 'Count');
    expect(result.completed, isTrue, reason: result.message);
    expect(host.fields['count'], '2');
  });

  test('a Set Variable value that cannot be evaluated stops the script (#93)', () async {
    final host = _RecordingHost();
    final runner = _runner(host, {
      'sc1': _script('sc1', 'Count', [
        _stepJson(1, 'set_variable', {'name': r'$count', 'value': 'Sum(unknown)'}),
        _stepJson(2, 'new_record'),
      ]),
    });

    final result = await _runScript(runner, 'sc1', 'Count');
    expect(result.completed, isFalse);
    expect(host.did, isEmpty);
  });

  test('Halt Script in a child stops the caller too (#95)', () async {
    final host = _RecordingHost();
    final runner = _runner(host, {
      'parent': _script('parent', 'Parent', [
        _stepJson(1, 'perform_script', {'script_name': 'Child'}),
        _stepJson(2, 'new_record'),
      ]),
      'child': _script('child', 'Child', [
        _stepJson(1, 'halt_script'),
      ]),
    });

    final result = await _runScript(runner, 'parent', 'Parent');
    expect(result.halted, isTrue, reason: 'the halt is the outcome, not an ordinary return');
    expect(result.completed, isTrue, reason: 'a halt is not a failure');
    expect(host.did, isEmpty, reason: 'no step of the caller may run after a halt');
  });

  test('Halt Script two levels down stops every ancestor (#95)', () async {
    final host = _RecordingHost();
    final runner = _runner(host, {
      'parent': _script('parent', 'Parent', [
        _stepJson(1, 'perform_script', {'script_name': 'Middle'}),
        _stepJson(2, 'new_record'),
      ]),
      'middle': _script('middle', 'Middle', [
        _stepJson(1, 'perform_script', {'script_name': 'Child'}),
        _stepJson(2, 'commit_records'),
      ]),
      'child': _script('child', 'Child', [
        _stepJson(1, 'halt_script'),
      ]),
    });

    final result = await _runScript(runner, 'parent', 'Parent');
    expect(result.halted, isTrue);
    expect(host.did, isEmpty);
  });

  test('Exit Script returns to its caller, which carries on (#95)', () async {
    final host = _RecordingHost();
    final runner = _runner(host, {
      'parent': _script('parent', 'Parent', [
        _stepJson(1, 'perform_script', {'script_name': 'Child'}),
        _stepJson(2, 'new_record'),
      ]),
      'child': _script('child', 'Child', [
        _stepJson(1, 'commit_records'),
        _stepJson(2, 'exit_script'),
        _stepJson(3, 'revert_record'),
      ]),
    });

    final result = await _runScript(runner, 'parent', 'Parent');
    expect(result.completed, isTrue, reason: result.message);
    expect(result.halted, isFalse);
    expect(host.did, ['commit_records', 'new_record'],
        reason: 'the child returned, and nothing after its Exit Script ran');
  });

  test('the debugger steps the conditional script a normal run runs (#94)', () async {
    // Normal run: the branch is taken and the record is created.
    final runHost = _RecordingHost();
    final scripts = {
      'sc1': _script('sc1', 'Greet', [
        _stepJson(1, 'if', {'condition': '"yes" = "yes"'}),
        _stepJson(2, 'new_record'),
        _stepJson(3, 'end_if'),
      ]),
    };
    final runResult = await _runScript(_runner(runHost, scripts), 'sc1', 'Greet');
    expect(runResult.completed, isTrue, reason: runResult.message);
    expect(runHost.did, ['new_record']);

    // Debugging the same script does the same thing, step by step.
    final debugHost = _RecordingHost();
    final runner = _runner(debugHost, scripts);
    final session = ScriptDebugSession(
      script: await runner.apiClient.getScript('sc1'),
      runner: runner,
    )..begin();

    await session.continueRun();
    expect(session.message, contains('ran to the end'));
    expect(debugHost.did, runHost.did,
        reason: 'stepping has the same effects as running, in the same order');
    expect(session.steps.every((s) => s.state == DebugStepState.done), isTrue);
  });

  test('the debugger skips the branch a normal run skips, and shares its variables (#94)',
      () async {
    final host = _RecordingHost();
    final runner = _runner(host, {
      'sc1': _script('sc1', 'Greet', [
        _stepJson(1, 'set_variable', {'name': r'$mode', 'value': '"skip"'}),
        _stepJson(2, 'if', {'condition': r'$mode = "run"'}),
        _stepJson(3, 'new_record'),
        _stepJson(4, 'end_if'),
        _stepJson(5, 'commit_records'),
      ]),
    });
    final session = ScriptDebugSession(
      script: await runner.apiClient.getScript('sc1'),
      runner: runner,
    )..begin();

    await session.continueRun();
    expect(host.did, ['commit_records'],
        reason: 'the branch was not taken, and the step after End If still ran');
    expect(session.steps[2].state, DebugStepState.skipped);
    expect(session.localVariables[r'$mode'], 'skip',
        reason: 'the variables a step set are the ones the next step reads');
  });
}
