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

/// A transfer definition as the server answers it (#96). The default is a
/// transfer inside one solution; naming [destinationDatabase] makes it reach
/// a second one, which is what needs a peer session token.
Map<String, dynamic> _transferJson({
  String name = 'Send answers',
  String sourceTable = 'answers',
  String destinationTable = 'project_answers',
  String destinationDatabase = '',
}) =>
    {
      'id': 'tr-$name',
      'name': name,
      'source': {'table': sourceTable},
      'destination': {
        if (destinationDatabase.isNotEmpty) 'database': destinationDatabase,
        'table': destinationTable,
      },
      'mappings': [
        {'source_field': 'answer_code', 'destination_field': 'code'},
      ],
      'match_fields': ['code'],
      'mode': 'update_matching',
      'on_unmatched': 'add',
      'on_rejected': 'abort',
      'date_order': 'iso',
    };

/// A run report as the server answers it.
Map<String, dynamic> _reportJson({
  String transfer = 'Send answers',
  int added = 0,
  int updated = 0,
  int skipped = 0,
  List<Map<String, dynamic>> rejected = const [],
  bool committed = true,
  String? error,
}) =>
    {
      'transfer': transfer,
      'source_database': 'dispatch_db',
      'source_table': 'answers',
      'destination_database': 'results_db',
      'destination_table': 'project_answers',
      'mode': 'update_matching',
      'on_rejected': 'abort',
      'rows': added + updated + skipped + rejected.length,
      'added': added,
      'updated': updated,
      'skipped': skipped,
      'rejected': rejected,
      'committed': committed,
      if (error != null) 'error': error,
    };

/// A runner whose API serves scripts, transfer definitions by name, and one
/// run report. A transfer the map does not hold answers 404, exactly as the
/// server does for a name no definition has.
LayoutActionRunner _transferRunner(
  _RecordingHost host, {
  Map<String, Map<String, dynamic>> scripts = const {},
  Map<String, Map<String, dynamic>> transfers = const {},
  Map<String, dynamic>? report,
  int reportStatus = 200,
  Future<String?> Function(String solution)? peerTokenProvider,
}) {
  final mock = MockClient((request) async {
    final path = request.url.path;
    const jsonHeaders = {'content-type': 'application/json'};

    if (path.startsWith('/api/v1/transfers')) {
      final segments = path.split('/').where((s) => s.isNotEmpty).toList();
      // /api/v1/transfers/<name>/run  →  the run; /api/v1/transfers/<name> → read
      final isRun = segments.last == 'run';
      final key = Uri.decodeComponent(isRun ? segments[segments.length - 2] : segments.last);
      if (isRun) {
        return http.Response(jsonEncode(report ?? _reportJson()), reportStatus,
            headers: jsonHeaders);
      }
      // The step asks by name; a definition is served under its name and
      // under the id the fixture gives it.
      final definition = transfers[key] ??
          transfers.values.where((t) => t['id'] == key).firstOrNull;
      if (definition == null) {
        return http.Response(
            jsonEncode({'title': 'Transfer Not Found', 'detail': 'no transfer named $key'}),
            404,
            headers: jsonHeaders);
      }
      return http.Response(jsonEncode(definition), 200, headers: jsonHeaders);
    }

    if (path == '/api/v1/schemas/scripts') {
      return http.Response(jsonEncode(scripts.values.toList()), 200, headers: jsonHeaders);
    }
    final script = scripts[path.split('/').last];
    if (script == null) return http.Response('Not found', 404);
    return http.Response(jsonEncode(script), 200, headers: jsonHeaders);
  });
  return LayoutActionRunner(
    apiClient: ApiClient(baseUrl: 'http://test-server:8080', httpClient: mock),
    host: host,
    peerTokenProvider: peerTokenProvider,
  );
}

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

  // ─── Loop / Exit Loop If / End Loop (#96) ───────────────────────────────
  group('a Loop repeats its steps until an Exit Loop If lets it out (#96)', () {
    test('the body runs, then the Exit Loop If that now holds lets it out', () async {
      final host = _RecordingHost();
      final runner = _runner(host, {
        'sc1': _script('sc1', 'One turn', [
          _stepJson(1, 'loop'),
          _stepJson(2, 'new_record'),
          _stepJson(3, 'set_variable', {'name': r'$done', 'value': '"1"'}),
          _stepJson(4, 'exit_loop_if', {'condition': r'$done = "1"'}),
          _stepJson(5, 'end_loop'),
          _stepJson(6, 'commit_records'),
        ]),
      });
      final result = await _runScript(runner, 'sc1', 'One turn');
      expect(result.completed, isTrue, reason: result.message);
      expect(host.did, ['new_record', 'commit_records'],
          reason: 'one turn of the body, then the step after End Loop');
    });

    test('a loop with no way out is stopped rather than left running', () async {
      final host = _RecordingHost();
      final runner = _runner(host, {
        'sc1': _script('sc1', 'Forever', [
          _stepJson(1, 'loop'),
          _stepJson(2, 'end_loop'),
          _stepJson(3, 'commit_records'),
        ]),
      });
      final result = await _runScript(runner, 'sc1', 'Forever');
      expect(result.completed, isFalse);
      expect(result.message, contains('without reaching an Exit Loop If'));
      expect(host.did, isEmpty, reason: 'the script never reached the step after the loop');
    });

    test('a loop whose condition holds at once runs its body no times', () async {
      final host = _RecordingHost();
      final runner = _runner(host, {
        'sc1': _script('sc1', 'Nothing to do', [
          _stepJson(1, 'loop'),
          _stepJson(2, 'exit_loop_if', {'condition': '1 = 1'}),
          _stepJson(3, 'new_record'),
          _stepJson(4, 'end_loop'),
          _stepJson(5, 'commit_records'),
        ]),
      });
      final result = await _runScript(runner, 'sc1', 'Nothing to do');
      expect(result.completed, isTrue, reason: result.message);
      expect(host.did, ['commit_records'],
          reason: 'the body never ran, and the step after End Loop did');
    });

    test('Exit Loop If leaves only the loop it is in, not the one around it', () async {
      final host = _RecordingHost();
      final runner = _runner(host, {
        'sc1': _script('sc1', 'Nested', [
          _stepJson(1, 'loop'),
          _stepJson(2, 'loop'),
          _stepJson(3, 'exit_loop_if', {'condition': '1 = 1'}),
          _stepJson(4, 'new_record'),
          _stepJson(5, 'end_loop'),
          // Out of the inner loop, still inside the outer one.
          _stepJson(6, 'duplicate_record'),
          _stepJson(7, 'exit_loop_if', {'condition': '1 = 1'}),
          _stepJson(8, 'end_loop'),
          _stepJson(9, 'commit_records'),
        ]),
      });
      final result = await _runScript(runner, 'sc1', 'Nested');
      expect(result.completed, isTrue, reason: result.message);
      expect(host.did, ['duplicate_record', 'commit_records'],
          reason: 'the inner body was skipped, the outer body ran once');
    });

    test('a condition File4Base cannot evaluate stops the loop instead of guessing', () async {
      final host = _RecordingHost();
      final runner = _runner(host, {
        'sc1': _script('sc1', 'Unreadable', [
          _stepJson(1, 'loop'),
          _stepJson(2, 'exit_loop_if', {'condition': 'Sum(unknown_field) > 2'}),
          _stepJson(3, 'new_record'),
          _stepJson(4, 'end_loop'),
        ]),
      });
      final result = await _runScript(runner, 'sc1', 'Unreadable');
      expect(result.completed, isFalse);
      expect(result.message, contains('not one File4Base can evaluate'));
      expect(host.did, isEmpty, reason: 'nothing ran on a condition nobody could read');
    });

    test('a loop inside a branch that was not taken does not run at all', () async {
      final host = _RecordingHost();
      final runner = _runner(host, {
        'sc1': _script('sc1', 'Dead branch', [
          _stepJson(1, 'if', {'condition': '1 = 0'}),
          _stepJson(2, 'loop'),
          _stepJson(3, 'new_record'),
          _stepJson(4, 'end_loop'),
          _stepJson(5, 'end_if'),
          _stepJson(6, 'commit_records'),
        ]),
      });
      final result = await _runScript(runner, 'sc1', 'Dead branch');
      expect(result.completed, isTrue, reason: result.message);
      expect(host.did, ['commit_records']);
    });

    test('an End Loop with no Loop stops the script', () async {
      final host = _RecordingHost();
      final runner = _runner(host, {
        'sc1': _script('sc1', 'Unbalanced', [
          _stepJson(1, 'new_record'),
          _stepJson(2, 'end_loop'),
        ]),
      });
      final result = await _runScript(runner, 'sc1', 'Unbalanced');
      expect(result.completed, isFalse);
      expect(result.message, contains('no Loop to close'));
    });

    test('the debugger loops exactly as a run does', () async {
      final scripts = {
        'sc1': _script('sc1', 'Twice', [
          _stepJson(1, 'loop'),
          _stepJson(2, 'new_record'),
          _stepJson(3, 'exit_loop_if', {'condition': '1 = 1'}),
          _stepJson(4, 'end_loop'),
          _stepJson(5, 'commit_records'),
        ]),
      };

      final runHost = _RecordingHost();
      final ran = await _runScript(_runner(runHost, scripts), 'sc1', 'Twice');
      expect(ran.completed, isTrue, reason: ran.message);

      final debugHost = _RecordingHost();
      final runner = _runner(debugHost, scripts);
      final session = ScriptDebugSession(
        script: await runner.apiClient.getScript('sc1'),
        runner: runner,
      )..begin();
      await session.continueRun();

      expect(debugHost.did, runHost.did,
          reason: 'stepping through a loop has the same effects as running it');
      expect(runHost.did, ['new_record', 'commit_records']);
    });
  });

  // ─── Import Records / Export Records (#96) ──────────────────────────────
  group('Import Records and Export Records run a named transfer (#96)', () {
    test('a step that names no transfer stops the script', () async {
      final host = _RecordingHost();
      final runner = _transferRunner(host, scripts: {
        'sc1': _script('sc1', 'Sync', [
          _stepJson(1, 'import_records'),
          _stepJson(2, 'commit_records'),
        ]),
      });
      final result = await _runScript(runner, 'sc1', 'Sync');
      expect(result.completed, isFalse);
      expect(result.message, contains('has no transfer named in it'));
      expect(host.did, isEmpty);
    });

    test('a transfer this solution does not have stops the script, and never falls '
        'back to a script of the same name', () async {
      final host = _RecordingHost();
      final runner = _transferRunner(host, scripts: {
        'sc1': _script('sc1', 'Sync', [
          _stepJson(1, 'import_records', {'transfer': 'Send answers'}),
          _stepJson(2, 'commit_records'),
        ]),
        // A script with the very name the step asks for. It must not run.
        'sc2': _script('sc2', 'Send answers', [_stepJson(1, 'new_record')]),
      });
      final result = await _runScript(runner, 'sc1', 'Sync');
      expect(result.completed, isFalse);
      expect(result.message, contains('there is no transfer named "Send answers"'));
      expect(host.did, isEmpty,
          reason: 'the same-named script was not run in its place');
    });

    test('a transfer reaching another solution stops without a session for it', () async {
      final host = _RecordingHost();
      final runner = _transferRunner(
        host,
        scripts: {
          'sc1': _script('sc1', 'Sync', [
            _stepJson(1, 'export_records', {'transfer': 'Send answers'}),
          ]),
        },
        transfers: {'Send answers': _transferJson(destinationDatabase: 'results_db')},
      );
      final result = await _runScript(runner, 'sc1', 'Sync');
      expect(result.completed, isFalse);
      expect(result.message, contains('results_db'));
      expect(result.message, contains('not signed in to it'));
    });

    test('with a session for each end it runs, and the counts land in variables', () async {
      final host = _RecordingHost();
      final asked = <String>[];
      final runner = _transferRunner(
        host,
        scripts: {
          'sc1': _script('sc1', 'Sync', [
            _stepJson(1, 'export_records', {'transfer': 'Send answers'}),
            _stepJson(2, 'if', {'condition': r'$$transferRejected = 0'}),
            _stepJson(3, 'commit_records'),
            _stepJson(4, 'end_if'),
          ]),
        },
        transfers: {'Send answers': _transferJson(destinationDatabase: 'results_db')},
        report: _reportJson(added: 2, updated: 1),
        peerTokenProvider: (solution) async {
          asked.add(solution);
          return 'peer-token';
        },
      );
      final result = await _runScript(runner, 'sc1', 'Sync');
      expect(result.completed, isTrue, reason: result.message);
      expect(asked, ['results_db'],
          reason: 'the far solution is the one a token was asked for');
      expect(host.did, ['commit_records'],
          reason: 'nothing was rejected, so the branch after the transfer ran');
      expect(LayoutActionRunner.globalVariables[r'$$transferadded'], '2');
      expect(LayoutActionRunner.globalVariables[r'$$transferupdated'], '1');
      expect(LayoutActionRunner.globalVariables[r'$$transferrejected'], '0');
      expect(LayoutActionRunner.globalVariables[r'$$transfercommitted'], '1');
    });

    test('a refused transfer stops the script and says which rows were rejected',
        () async {
      final host = _RecordingHost();
      final runner = _transferRunner(
        host,
        scripts: {
          'sc1': _script('sc1', 'Sync', [
            _stepJson(1, 'export_records', {'transfer': 'Send answers'}),
            _stepJson(2, 'commit_records'),
          ]),
        },
        transfers: {'Send answers': _transferJson(destinationDatabase: 'results_db')},
        report: _reportJson(
          committed: false,
          error: 'transfer refused: rows the destination cannot hold',
          rejected: [
            {
              'row': 3,
              'source_field': 'score_text',
              'destination_field': 'score',
              'value': 'non renseigné',
              'reason': 'is not a number',
            },
          ],
        ),
        reportStatus: 422,
        peerTokenProvider: (solution) async => 'peer-token',
      );
      final result = await _runScript(runner, 'sc1', 'Sync');
      expect(result.completed, isFalse);
      expect(result.message, contains('was refused'));
      expect(result.message, contains('row 3'));
      expect(result.message, contains('score_text'));
      expect(result.message, contains('non renseigné'));
      expect(host.did, isEmpty,
          reason: 'nothing was written, so the step after it did not run');
    });

    test('a step pointed the wrong way is refused rather than run backwards', () async {
      final host = _RecordingHost();
      final runner = _transferRunner(
        host,
        scripts: {
          'sc1': _script('sc1', 'Sync', [
            // The transfer sends records out, but the step brings them in.
            _stepJson(1, 'import_records', {'transfer': 'Send answers'}),
          ]),
        },
        transfers: {'Send answers': _transferJson(destinationDatabase: 'results_db')},
        peerTokenProvider: (solution) async => 'peer-token',
      );
      final result = await _runScript(runner, 'sc1', 'Sync');
      expect(result.completed, isFalse);
      expect(result.message, contains('Import Records brings records into this solution'));
    });

    test('a transfer inside one solution needs no second session', () async {
      final host = _RecordingHost();
      final runner = _transferRunner(
        host,
        scripts: {
          'sc1': _script('sc1', 'Archive', [
            _stepJson(1, 'import_records', {'transfer': 'Archive inbox'}),
            _stepJson(2, 'commit_records'),
          ]),
        },
        transfers: {'Archive inbox': _transferJson()},
        report: _reportJson(added: 1),
      );
      final result = await _runScript(runner, 'sc1', 'Archive');
      expect(result.completed, isTrue, reason: result.message);
      expect(host.did, ['commit_records']);
    });

    test('the transfer can be named by a variable', () async {
      final host = _RecordingHost();
      final runner = _transferRunner(
        host,
        scripts: {
          'sc1': _script('sc1', 'Sync', [
            _stepJson(1, 'set_variable', {'name': r'$which', 'value': '"Archive inbox"'}),
            _stepJson(2, 'import_records', {'transfer': r'$which'}),
            _stepJson(3, 'commit_records'),
          ]),
        },
        transfers: {'Archive inbox': _transferJson()},
        report: _reportJson(added: 1),
      );
      final result = await _runScript(runner, 'sc1', 'Sync');
      expect(result.completed, isTrue, reason: result.message);
      expect(host.did, ['commit_records']);
    });

    test('a variable that holds no transfer name stops the script', () async {
      final host = _RecordingHost();
      final runner = _transferRunner(host, scripts: {
        'sc1': _script('sc1', 'Sync', [
          _stepJson(1, 'import_records', {'transfer': r'$which'}),
        ]),
      });
      final result = await _runScript(runner, 'sc1', 'Sync');
      expect(result.completed, isFalse);
      expect(result.message, contains('holds nothing here'));
    });

    test('a transfer step on a button is refused, as every script step is', () async {
      final host = _RecordingHost();
      final runner = _transferRunner(host, scripts: {});
      for (final step in ['loop', 'exit_loop_if', 'end_loop']) {
        final result = await runner.runStep(step, {}, depth: 0);
        expect(result.completed, isFalse, reason: step);
        expect(result.message, contains('not supported on a button'), reason: step);
      }
    });
  });
}
