// #107 — Perform Script into another solution.
//
// A fake server holds two solutions, Dispatch and Results, and answers each
// request as the solution its bearer token belongs to. So every assertion
// below can say not only what ran but *where* it ran — which is the whole
// point: a script of another file must never be answered by a same-named
// script of this one, and a script performed over there must never touch the
// window on screen here.

import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _dispatch = 'dispatch_db';
const _results = 'results_db';
const _dispatchToken = 'dispatch-token';
const _resultsToken = 'results-token';

/// Records what the window on screen was asked to do. In these tests the
/// window is Dispatch's, so anything a Results script does must not appear
/// here.
class _ScreenHost implements LayoutActionHost {
  final List<String> did = [];
  final List<String> dialogs = [];

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
  Future<void> actionGoToRecord(String target) async => did.add('go_to_record');
  @override
  Future<void> actionEnterFindMode() async => did.add('enter_find_mode');
  @override
  Future<void> actionPerformFind() async => did.add('perform_find');
  @override
  Future<void> actionShowAllRecords() async => did.add('show_all_records');
  @override
  Future<void> actionEnterPreviewMode() async => did.add('enter_preview_mode');
  @override
  Future<void> actionEnterBrowseMode() async => did.add('enter_browse_mode');
  @override
  Future<bool> actionGoToLayout(String layoutName) async {
    did.add('go_to_layout');
    return true;
  }

  @override
  Future<bool> actionSetField(String field, String value) async {
    did.add('set_field:$field');
    return true;
  }

  @override
  Future<void> actionShowDialog(String title, String message) async => dialogs.add(message);
  @override
  Future<void> actionOpenUrl(String url) async => did.add('open_url');
  @override
  Future<void> actionAdjustWindow(String windowAction) async => did.add('adjust_window');
  @override
  Future<void> actionReLogin() async => did.add('re_login');
  @override
  Future<void> actionCloseFile() async => did.add('close_file');
  @override
  Future<void> actionExitApplication() async => did.add('exit_application');

  /// The window on screen has one field, `notes`.
  @override
  String actionResolveText(String text) => text.trim() == '{{notes}}' ? 'from the Dispatch window' : text;
}

Map<String, dynamic> _step(int n, String type, [Map<String, dynamic> params = const {}]) => {
      'id': 's$n',
      'sequence_idx': n,
      'step_type': type,
      'params': params,
      'is_enabled': true,
    };

Map<String, dynamic> _script(String id, String name, List<Map<String, dynamic>> steps) =>
    {'id': id, 'name': name, 'is_active': true, 'steps': steps};

/// One solution as the fake server holds it.
class _Solution {
  _Solution(this.database);
  final String database;
  final Map<String, Map<String, dynamic>> scripts = {};

  /// File reference name → solution database, '' when unmapped.
  final Map<String, String> fileReferences = {};
  final Map<String, Map<String, dynamic>> transfers = {};
}

/// Two solutions behind one server, told apart by the bearer token.
class _TwoSolutions {
  _TwoSolutions() {
    solutions = {_dispatchToken: dispatch, _resultsToken: results};
  }

  final dispatch = _Solution(_dispatch);
  final results = _Solution(_results);
  late final Map<String, _Solution> solutions;

  /// Every request: which solution it went to, and what it asked.
  final List<String> log = [];

  /// The bodies of the transfer runs, with the token they were sent on.
  final List<Map<String, dynamic>> runs = [];

  MockClient get client => MockClient((request) async {
        const json = {'content-type': 'application/json'};
        final path = request.url.path;

        if (path == '/api/v1/auth/login') {
          return http.Response(
              jsonEncode({
                'status': 'ok',
                'database': _dispatch,
                'token': _dispatchToken,
                'user': {'id': 'u1', 'username': 'alice', 'role': 'owner', 'is_active': true},
              }),
              200,
              headers: json);
        }

        final token = (request.headers['authorization'] ?? '').replaceFirst('Bearer ', '');
        final solution = solutions[token];
        if (solution == null) return http.Response('{"detail":"no session"}', 401, headers: json);
        log.add('${solution.database} ${request.method} $path');

        final segments = path.split('/').where((s) => s.isNotEmpty).map(Uri.decodeComponent).toList();

        if (path == '/api/v1/schemas/scripts') {
          return http.Response(jsonEncode(solution.scripts.values.toList()), 200, headers: json);
        }
        if (path.startsWith('/api/v1/schemas/scripts/')) {
          final script = solution.scripts[segments.last];
          if (script == null) return http.Response('{"detail":"no such script"}', 404, headers: json);
          return http.Response(jsonEncode(script), 200, headers: json);
        }

        if (path.startsWith('/api/v1/file-references/') && segments.last == 'resolve') {
          final name = segments[segments.length - 2];
          final key = solution.fileReferences.keys
              .where((k) => k.toLowerCase() == name.toLowerCase())
              .firstOrNull;
          if (key == null) {
            return http.Response('{"title":"File Reference Not Found","detail":"no file reference $name"}', 404,
                headers: json);
          }
          final database = solution.fileReferences[key]!;
          if (database.isEmpty) {
            return http.Response('{"title":"File Reference Not Mapped","detail":"$name names no solution"}', 409,
                headers: json);
          }
          return http.Response(
              jsonEncode({
                'name': key,
                'database': database,
                'calling_database': solution.database,
                'local': database == solution.database,
              }),
              200,
              headers: json);
        }

        if (path.startsWith('/api/v1/transfers/')) {
          final isRun = segments.last == 'run';
          final key = isRun ? segments[segments.length - 2] : segments.last;
          final definition = solution.transfers[key] ??
              solution.transfers.values.where((t) => t['id'] == key).firstOrNull;
          if (definition == null) return http.Response('{"detail":"no transfer"}', 404, headers: json);
          if (!isRun) return http.Response(jsonEncode(definition), 200, headers: json);
          final body = request.body.isEmpty ? <String, dynamic>{} : jsonDecode(request.body) as Map<String, dynamic>;
          runs.add({'on': solution.database, 'peer_token': body['peer_token']});
          return http.Response(
              jsonEncode({
                'transfer': definition['name'],
                'rows': 2,
                'added': 0,
                'updated': 2,
                'skipped': 0,
                'rejected': <dynamic>[],
                'committed': true,
              }),
              200,
              headers: json);
        }

        return http.Response('{"detail":"unexpected $path"}', 404, headers: json);
      });
}

/// Signs in to Dispatch and returns a runner on its window. [peerSessions]
/// are the other solutions the user has signed in to.
Future<LayoutActionRunner> _signedIn(
  _TwoSolutions server,
  _ScreenHost host, {
  Map<String, String> peerSessions = const {},
}) async {
  final api = ApiClient(baseUrl: 'http://test-server:8080', httpClient: server.client);
  await api.login(username: 'alice', password: 'secret', database: _dispatch);
  return LayoutActionRunner(
    apiClient: api,
    host: host,
    peerTokenProvider: (solution) async => peerSessions[solution],
  );
}

Future<LayoutActionResult> _run(LayoutActionRunner runner, String id, String name) =>
    runner.run(ButtonActionModel.performScript(id: id, name: name));

/// The Dispatch script every test starts from: call "Receive" in the file
/// "Results", then commit the record on screen.
Map<String, dynamic> _callerScript({String reference = 'Results'}) => _script('d1', 'Send', [
      _step(1, 'perform_script', {'script_name': 'Receive', 'file_reference': reference}),
      _step(2, 'commit_records'),
    ]);

/// A Dispatch script that shares the name of the one in Results. It must
/// never run in its place.
Map<String, dynamic> get _impostor => _script('d2', 'Receive', [_step(1, 'new_record')]);

void main() {
  setUp(LayoutActionRunner.globalVariables.clear);

  group('a reference that goes nowhere stops the script, and never runs a local namesake', () {
    test('an unmapped reference, as the migration creates it', () async {
      final server = _TwoSolutions();
      server.dispatch.scripts['d1'] = _callerScript();
      server.dispatch.scripts['d2'] = _impostor;
      server.dispatch.fileReferences['Results'] = '';

      final host = _ScreenHost();
      final result = await _run(await _signedIn(server, host), 'd1', 'Send');

      expect(result.completed, isFalse);
      expect(result.message, contains('not mapped to a solution yet'));
      expect(host.did, isEmpty, reason: 'the local "Receive" did not run, and nothing after the call did');
    });

    test('a reference this solution does not have', () async {
      final server = _TwoSolutions();
      server.dispatch.scripts['d1'] = _callerScript(reference: 'Archive');
      server.dispatch.scripts['d2'] = _impostor;

      final host = _ScreenHost();
      final result = await _run(await _signedIn(server, host), 'd1', 'Send');

      expect(result.completed, isFalse);
      expect(result.message, contains('no file reference of that name'));
      expect(host.did, isEmpty);
    });

    test('a mapped reference to a solution this session is not signed in to', () async {
      final server = _TwoSolutions();
      server.dispatch.scripts['d1'] = _callerScript();
      server.dispatch.scripts['d2'] = _impostor;
      server.dispatch.fileReferences['Results'] = _results;

      final host = _ScreenHost();
      final result = await _run(await _signedIn(server, host), 'd1', 'Send');

      expect(result.completed, isFalse);
      expect(result.message, contains('not signed in to it'));
      expect(result.message, contains(_results));
      expect(host.did, isEmpty);
      expect(server.log.where((l) => l.startsWith(_results)), isEmpty,
          reason: 'nothing was asked of a solution nobody signed in to');
    });
  });

  test('with a session there, the script runs in that solution, and a transfer in it reaches back', () async {
    final server = _TwoSolutions();
    server.dispatch.scripts['d1'] = _callerScript();
    server.dispatch.scripts['d2'] = _impostor;
    server.dispatch.fileReferences['Results'] = _results;
    // Results' "Receive" returns the answers to Dispatch: a transfer whose
    // destination is the calling solution.
    server.results.scripts['r1'] = _script('r1', 'Receive', [
      _step(1, 'set_variable', {'name': r'$$returned', 'value': '"yes"'}),
      _step(2, 'export_records', {'transfer': 'Return answers'}),
      _step(3, 'show_dialog', {'title': 'Done', 'message': 'Answers returned'}),
    ]);
    server.results.transfers['Return answers'] = {
      'id': 'tr-return',
      'name': 'Return answers',
      'source': {'table': 'project_answers'},
      'destination': {'database': _dispatch, 'table': 'answers'},
      'mappings': [
        {'source_field': 'code', 'destination_field': 'answer_code'},
      ],
      'match_fields': ['answer_code'],
      'mode': 'update_matching',
      'on_rejected': 'abort',
    };

    final host = _ScreenHost();
    final runner = await _signedIn(server, host, peerSessions: {_results: _resultsToken});
    final result = await _run(runner, 'd1', 'Send');

    expect(result.completed, isTrue, reason: result.message);
    expect(host.did, ['commit_records'],
        reason: 'the Results script touched no window, and the caller carried on after it');
    expect(host.dialogs, ['Answers returned'], reason: 'a dialog needs no window of its own');

    // "Receive" was read from Results, never from Dispatch.
    expect(server.log, contains('$_results GET /api/v1/schemas/scripts'));
    expect(server.log.where((l) => l.startsWith('$_dispatch GET /api/v1/schemas/scripts/d2')), isEmpty);

    // The transfer ran on the Results session, and reached back to Dispatch
    // with the session the user is working in there — not one invented.
    expect(server.runs, [
      {'on': _results, 'peer_token': _dispatchToken},
    ]);
    expect(LayoutActionRunner.globalVariables[r'$$returned'], 'yes');
    expect(LayoutActionRunner.globalVariables[r'$$transferupdated'], '2');
  });

  test('a step that acts on a window stops the script instead of acting on the wrong one', () async {
    final server = _TwoSolutions();
    server.dispatch.scripts['d1'] = _callerScript();
    server.dispatch.fileReferences['Results'] = _results;
    server.results.scripts['r1'] = _script('r1', 'Receive', [
      _step(1, 'new_record'),
      _step(2, 'set_field', {'field': 'notes', 'value': '"x"'}),
    ]);

    final host = _ScreenHost();
    final runner = await _signedIn(server, host, peerSessions: {_results: _resultsToken});
    final result = await _run(runner, 'd1', 'Send');

    expect(result.completed, isFalse);
    expect(result.message, contains('in the solution $_results'));
    expect(result.message, contains('acts on a window'));
    expect(host.did, isEmpty,
        reason: 'no record was created in the Dispatch window on behalf of a Results script');
  });

  test('a field read in the other solution is not answered from the window on screen', () async {
    final server = _TwoSolutions();
    server.dispatch.scripts['d1'] = _callerScript();
    server.dispatch.fileReferences['Results'] = _results;
    server.results.scripts['r1'] = _script('r1', 'Receive', [
      // `notes` is a field of the Dispatch window. Results cannot read it.
      _step(1, 'set_variable', {'name': r'$n', 'value': 'notes'}),
    ]);

    final host = _ScreenHost();
    final runner = await _signedIn(server, host, peerSessions: {_results: _resultsToken});
    final result = await _run(runner, 'd1', 'Send');

    expect(result.completed, isFalse);
    expect(result.message, contains('cannot evaluate'));
  });

  test('Halt Script over there stops the caller here too', () async {
    final server = _TwoSolutions();
    server.dispatch.scripts['d1'] = _callerScript();
    server.dispatch.fileReferences['Results'] = _results;
    server.results.scripts['r1'] = _script('r1', 'Receive', [_step(1, 'halt_script')]);

    final host = _ScreenHost();
    final runner = await _signedIn(server, host, peerSessions: {_results: _resultsToken});
    final result = await _run(runner, 'd1', 'Send');

    expect(result.halted, isTrue);
    expect(host.did, isEmpty, reason: 'the caller did not commit after the halt');
  });

  test('a script the other solution does not have is reported with that solution', () async {
    final server = _TwoSolutions();
    server.dispatch.scripts['d1'] = _callerScript();
    server.dispatch.scripts['d2'] = _impostor;
    server.dispatch.fileReferences['Results'] = _results;

    final host = _ScreenHost();
    final runner = await _signedIn(server, host, peerSessions: {_results: _resultsToken});
    final result = await _run(runner, 'd1', 'Send');

    expect(result.completed, isFalse);
    expect(result.message, contains('"Receive" not found in the solution $_results'));
    expect(host.did, isEmpty, reason: 'the Dispatch "Receive" still did not run');
  });

  test('a reference that names this very solution is an ordinary Perform Script', () async {
    final server = _TwoSolutions();
    server.dispatch.scripts['d1'] = _callerScript(reference: 'Self');
    server.dispatch.scripts['d2'] = _impostor; // here it is the right script
    server.dispatch.fileReferences['Self'] = _dispatch;

    final host = _ScreenHost();
    final result = await _run(await _signedIn(server, host), 'd1', 'Send');

    expect(result.completed, isTrue, reason: result.message);
    expect(host.did, ['new_record', 'commit_records'],
        reason: 'a script of this solution has this solution\'s window');
  });

  test('a Perform Script with no file reference is unchanged', () async {
    final server = _TwoSolutions();
    server.dispatch.scripts['d1'] = _script('d1', 'Send', [
      _step(1, 'perform_script', {'script_name': 'Receive'}),
    ]);
    server.dispatch.scripts['d2'] = _impostor;

    final host = _ScreenHost();
    final result = await _run(await _signedIn(server, host), 'd1', 'Send');

    expect(result.completed, isTrue, reason: result.message);
    expect(host.did, ['new_record']);
  });
}
