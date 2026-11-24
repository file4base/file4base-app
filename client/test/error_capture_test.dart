// #133 — Set Error Capture, Get(LastError) and the errors FileMaker reports
// instead of stopping: a loop over the found set that ends on Exit after
// last, a find that matches nothing, a missing layout.

import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// A found set of [count] records, where no find matches anything.
class _FoundSetHost implements LayoutActionHost {
  _FoundSetHost(this.count);
  int count;
  int current = 0;
  final did = <String>[];
  final fields = <String, String>{};

  @override
  Future<int> actionGoToRecord(String target) async {
    if (count == 0) return 101;
    final index = switch (target) {
      'first' => 0,
      'last' => count - 1,
      'previous' => current - 1,
      'next' => current + 1,
      _ => int.parse(target) - 1,
    };
    if (index < 0 || index >= count) return 101;
    current = index;
    did.add('record:${index + 1}');
    return 0;
  }

  @override
  Future<int> actionPerformFind({bool reportNoMatch = true}) async {
    did.add(reportNoMatch ? 'find:dialog' : 'find:quiet');
    count = 0;
    current = 0;
    return 401;
  }

  @override
  Future<bool> actionGoToLayout(String layoutName) async => false;

  @override
  Future<bool> actionSetField(String field, String value) async {
    fields[field] = value;
    return true;
  }

  @override
  String actionResolveText(String text) => text;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

Map<String, dynamic> _step(int n, String type, [Map<String, dynamic> params = const {}]) =>
    {'id': 's$n', 'sequence_idx': n, 'step_type': type, 'params': params, 'is_enabled': true};

LayoutActionRunner _runner(LayoutActionHost host, List<Map<String, dynamic>> scripts) {
  final mock = MockClient((request) async {
    if (request.url.path == '/api/v1/schemas/scripts') {
      return http.Response(jsonEncode(scripts), 200, headers: {'content-type': 'application/json'});
    }
    final id = request.url.path.split('/').last;
    final script = scripts.where((s) => s['id'] == id).firstOrNull;
    if (script == null) return http.Response('Not found', 404);
    return http.Response(jsonEncode(script), 200, headers: {'content-type': 'application/json'});
  });
  return LayoutActionRunner(apiClient: ApiClient(baseUrl: 'http://test-server:8080', httpClient: mock), host: host);
}

Map<String, dynamic> _script(String id, String name, List<Map<String, dynamic>> steps) =>
    {'id': id, 'name': name, 'is_active': true, 'steps': steps};

Future<LayoutActionResult> _perform(LayoutActionRunner runner, String id) =>
    runner.run(ButtonActionModel.performScript(id: id, name: id));

void main() {
  test('Exit after last ends the loop over the found set, with 101 in Get(LastError)', () async {
    final host = _FoundSetHost(3);
    final runner = _runner(host, [
      _script('walk', 'Walk', [
        _step(1, 'Go to Record/Request/Page', {'target': 'first'}),
        _step(2, 'Loop'),
        _step(3, 'Set Variable', {'name': r'$seen', 'value': 'Get(LastError)'}),
        _step(4, 'Go to Record/Request/Page', {'target': 'next', 'exit_after_last': true}),
        _step(5, 'End Loop'),
        _step(6, 'Set Field', {'field': 'error', 'value': 'Get(LastError)', 'value_is_calculation': true}),
      ]),
    ]);
    final result = await _perform(runner, 'walk');
    expect(result.completed, isTrue, reason: result.message);
    expect(host.did, ['record:1', 'record:2', 'record:3']);
    expect(host.fields['error'], '101');
  });

  test('without Exit after last, Next on the last record stays there and the script goes on', () async {
    final host = _FoundSetHost(1);
    final runner = _runner(host, [
      _script('s', 'S', [
        _step(1, 'Go to Record/Request/Page', {'target': 'next'}),
        _step(2, 'Set Field', {'field': 'error', 'value': 'Get(LastError)', 'value_is_calculation': true}),
        _step(3, 'Set Field', {'field': 'after', 'value': 'Get(LastError)', 'value_is_calculation': true}),
      ]),
    ]);
    final result = await _perform(runner, 's');
    expect(result.completed, isTrue, reason: result.message);
    expect(host.fields['error'], '101');
    expect(host.fields['after'], '0', reason: 'a step that succeeds clears the error');
  });

  test('a find that matches nothing shows no dialog under error capture and reports 401', () async {
    final host = _FoundSetHost(5);
    final runner = _runner(host, [
      _script('find', 'Find', [
        _step(1, 'Set Error Capture', {'state': true}),
        _step(2, 'Perform Find'),
        _step(3, 'If', {'condition': 'Get(LastError) = 401'}),
        _step(4, 'Set Field', {'field': 'outcome', 'value': '"none"', 'value_is_calculation': true}),
        _step(5, 'End If'),
        _step(6, 'Set Field', {'field': 'capture', 'value': 'Get(ErrorCaptureState)', 'value_is_calculation': true}),
      ]),
    ]);
    final result = await _perform(runner, 'find');
    expect(result.completed, isTrue, reason: result.message);
    expect(host.did, ['find:quiet']);
    expect(host.fields, {'outcome': 'none', 'capture': '1'});
    expect(runner.errorCapture, isFalse, reason: 'capture ends with the outermost script');
  });

  test('without error capture a find shows its dialog', () async {
    final host = _FoundSetHost(5);
    final runner = _runner(host, [
      _script('find', 'Find', [_step(1, 'Perform Find')]),
    ]);
    expect((await _perform(runner, 'find')).completed, isTrue);
    expect(host.did, ['find:dialog']);
  });

  test('a missing layout stops the script, unless error capture is on', () async {
    final steps = [
      _step(2, 'Go to Layout', {'layout_name': 'Gone'}),
      _step(3, 'Set Field', {'field': 'error', 'value': 'Get(LastError)', 'value_is_calculation': true}),
    ];
    final strict = _FoundSetHost(1);
    final stopped = await _perform(_runner(strict, [_script('s', 'S', steps)]), 's');
    expect(stopped.completed, isFalse);
    expect(stopped.message, contains('FileMaker error 105'));

    final captured = _FoundSetHost(1);
    final ran = await _perform(
        _runner(captured, [
          _script('s', 'S', [_step(1, 'Set Error Capture', {'state': true}), ...steps]),
        ]),
        's');
    expect(ran.completed, isTrue, reason: ran.message);
    expect(captured.fields['error'], '105');
  });

  test('error capture never hides a step File4Base cannot run', () async {
    final host = _FoundSetHost(1);
    final result = await _perform(
        _runner(host, [
          _script('s', 'S', [_step(1, 'Set Error Capture', {'state': true}), _step(2, 'Beep')]),
        ]),
        's');
    expect(result.completed, isFalse);
  });

  test('a sub-script that turns capture on leaves it on for its caller, and its last error', () async {
    final host = _FoundSetHost(0);
    final runner = _runner(host, [
      _script('main', 'Main', [
        _step(1, 'Perform Script', {'script_name': 'Arm'}),
        _step(2, 'Set Field', {'field': 'error', 'value': 'Get(LastError)', 'value_is_calculation': true}),
        _step(3, 'Go to Layout', {'layout_name': 'Gone'}),
        _step(4, 'Set Field', {'field': 'layout', 'value': 'Get(LastError)', 'value_is_calculation': true}),
      ]),
      _script('arm', 'Arm', [
        _step(1, 'Set Error Capture', {'state': true}),
        _step(2, 'Go to Record/Request/Page', {'target': 'first'}),
      ]),
    ]);
    final result = await _perform(runner, 'main');
    expect(result.completed, isTrue, reason: result.message);
    expect(host.fields, {'error': '101', 'layout': '105'});
  });
}
