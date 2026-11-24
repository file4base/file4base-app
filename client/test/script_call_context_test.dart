import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/script_workspace/script_debugger.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

class _Host implements LayoutActionHost {
  final fields = <String, String>{};
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

Map<String, dynamic> _step(int n, String type, Map<String, dynamic> params) => {
  'id': 'step-$n',
  'sequence_idx': n,
  'step_type': type,
  'params': params,
  'is_enabled': true,
};
Map<String, dynamic> _script(
  String id,
  String name,
  List<Map<String, dynamic>> steps,
) => {
  'id': id,
  'name': name,
  'is_active': true,
  'steps': steps,
};
Map<String, dynamic> _field(int n, String field, String expression) => _step(
  n,
  'Set Field',
  {'field': field, 'value': expression, 'value_is_calculation': true},
);

LayoutActionRunner _runner(
  _Host host,
  Map<String, Map<String, dynamic>> scripts, {
  List<String>? requests,
}) {
  final api = ApiClient(
    baseUrl: 'http://test-server:8080',
    httpClient: MockClient((request) async {
      requests?.add(request.url.path);
      final body = request.url.path == '/api/v1/schemas/scripts'
          ? scripts.values.toList()
          : scripts[request.url.path.split('/').last];
      return http.Response(
        jsonEncode(body ?? {'detail': 'not found'}),
        body == null ? 404 : 200,
        headers: {'content-type': 'application/json'},
      );
    }),
  );
  return LayoutActionRunner(apiClient: api, host: host);
}

Map<String, Map<String, dynamic>> _nested() => {
  'parent': _script('parent', 'Parent', [
    _step(1, 'Set Variable', {'name': r'$secret', 'value': '"caller-only"'}),
    _step(2, 'Set Variable', {'name': r'$$secret', 'value': '"global-only"'}),
    _step(3, 'Perform Script', {
      'script_name': 'Child',
      'script_parameter': 'Get ( ScriptParameter )',
    }),
    _field(4, 'returned', 'Get ( ScriptResult )'),
    _field(5, 'caller_parameter', 'Get ( ScriptParameter )'),
    _step(6, 'If', {'condition': 'Get ( ScriptResult ) = "root"'}),
    _field(7, 'branch', '"matched"'),
    _step(8, 'End If', {}),
    _step(9, 'Exit Script', {'result': 'Get ( ScriptResult )'}),
    _field(10, 'after_exit', '"wrong"'),
  ]),
  'child': _script('child', 'Child', [
    _step(1, 'Set Variable', {
      'name': r'$argument',
      'value': 'Get ( ScriptParameter )',
    }),
    _field(2, 'child_parameter', r'$argument'),
    _field(3, 'child_local', r'$secret'),
    _step(4, 'Exit Script', {'result': r'$argument'}),
  ]),
};

void main() {
  test(
    'script calls carry arguments and results with independent local frames',
    () async {
      final host = _Host();
      final result = await _runner(host, _nested()).run(
        const ButtonActionModel.performScript(
          id: 'parent',
          name: 'Parent',
          parameter: '"root"',
        ),
      );
      expect(result.completed, isTrue, reason: result.message);
      expect(result.scriptResult, 'root');
      expect(host.fields, {
        'child_parameter': 'root',
        'child_local': '',
        'returned': 'root',
        'caller_parameter': 'root',
        'branch': 'matched',
      });
    },
  );

  test(
    'no explicit result preserves the prior result; explicit empty clears it',
    () async {
      final host = _Host();
      final scripts = {
        'parent': _script('parent', 'Parent', [
          _step(1, 'Perform Script', {'script_name': 'Value'}),
          _step(2, 'Perform Script', {'script_name': 'No result'}),
          _field(3, 'retained', 'Get ( ScriptResult )'),
          _step(4, 'Perform Script', {'script_name': 'Empty'}),
          _field(5, 'cleared', 'Get ( ScriptResult )'),
          _step(6, 'Exit Script', {'result': 'Get ( ScriptResult )'}),
        ]),
        'value': _script('value', 'Value', [
          _step(1, 'Exit Script', {'result': '"  value  "'}),
        ]),
        'none': _script('none', 'No result', [_step(1, 'Exit Script', {})]),
        'empty': _script('empty', 'Empty', [
          _step(1, 'Exit Script', {'result': '""'}),
        ]),
      };
      final result = await _runner(host, scripts).run(
        const ButtonActionModel.performScript(id: 'parent', name: 'Parent'),
      );
      expect(result.completed, isTrue, reason: result.message);
      expect(host.fields, {'retained': '  value  ', 'cleared': ''});
      expect(result.scriptResult, '');
    },
  );

  test(
    'unsupported button argument stops before loading or running the script',
    () async {
      final host = _Host();
      final requests = <String>[];
      final result = await _runner(host, _nested(), requests: requests).run(
        const ButtonActionModel.performScript(
          id: 'parent',
          name: 'Parent',
          parameter: 'UnknownFunction(1)',
        ),
      );
      expect(result.completed, isFalse);
      expect(requests, isEmpty);
      expect(host.fields, isEmpty);
    },
  );

  test(
    'unsupported return expression stops rather than claiming a result',
    () async {
      final host = _Host();
      final scripts = {
        'parent': _script('parent', 'Parent', [
          _step(1, 'Exit Script', {'result': 'UnknownFunction(1)'}),
          _field(2, 'after', '"wrong"'),
        ]),
      };
      final result = await _runner(host, scripts).run(
        const ButtonActionModel.performScript(id: 'parent', name: 'Parent'),
      );
      expect(result.completed, isFalse);
      expect(host.fields, isEmpty);
    },
  );

  test(
    'normal execution and debugger expose the same parameter and result',
    () async {
      final scripts = _nested();
      final normalHost = _Host();
      final normal = await _runner(normalHost, scripts).run(
        const ButtonActionModel.performScript(
          id: 'parent',
          name: 'Parent',
          parameter: '"root"',
        ),
      );
      final debugHost = _Host();
      final debug = ScriptDebugSession(
        script: ScriptModel.fromJson(scripts['parent']!),
        runner: _runner(debugHost, scripts),
        parameter: 'root',
      );
      await debug.continueRun();
      expect(debug.finished, isTrue);
      expect(debugHost.fields, normalHost.fields);
      expect(debug.scriptResult, normal.scriptResult);
      debug.restart();
      expect(debug.scriptResult, isNull);
      expect(debug.lastScriptResult, '');
      await debug.continueRun();
      expect(debug.scriptResult, 'root');
    },
  );
}
