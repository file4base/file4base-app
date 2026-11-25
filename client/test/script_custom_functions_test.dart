import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// A window whose formulas the runner cannot evaluate go to "the server":
/// the solution has the custom functions `Doubled ( n )` and `IsBig ( n )`.
class _Host implements LayoutActionHost, CalculationContextHost {
  final fields = <String, String>{};
  final requests = <({String expression, Map<String, String> variables, Map<String, Object?> get})>[];

  @override
  Future<bool> actionSetField(String field, String value) async {
    fields[field] = value;
    return true;
  }

  @override
  String actionResolveText(String text) => text;

  @override
  Set<String> get actionOwnOccurrenceNames => {'orders'};

  @override
  Future<EvaluationModel?> actionEvaluateInScript(
    String expression, {
    Map<String, String> variables = const {},
    Map<String, Object?> get = const {},
  }) async {
    requests.add((expression: expression, variables: variables, get: get));
    final n = num.tryParse(variables[r'$n'] ?? '') ?? 0;
    return switch (expression) {
      r'Doubled ( $n )' => EvaluationModel(expression: expression, text: '${(n * 2).toInt()}'),
      r'IsBig ( $n )' => EvaluationModel(expression: expression, text: n > 10 ? '1' : '0'),
      'Doubled ( Get ( ScriptParameter ) )' =>
        EvaluationModel(expression: expression, text: '${(num.parse('${get['ScriptParameter']}') * 2).toInt()}'),
      r'Let ( $$seen = 7 ; Doubled ( 2 ) )' =>
        EvaluationModel(expression: expression, text: '4', variables: const {r'$$seen': '7'}),
      _ => EvaluationModel(expression: expression, error: 'the function Tripled is not known'),
    };
  }

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

Future<(LayoutActionResult, _Host, LayoutActionRunner)> _run(
  List<Map<String, dynamic>> steps, {
  String parameter = '',
}) async {
  final script = {'id': 's', 'name': 'Totals', 'is_active': true, 'steps': steps};
  final api = ApiClient(
    baseUrl: 'http://test-server:8080',
    httpClient: MockClient((request) async {
      final body = request.url.path == '/api/v1/schemas/scripts' ? [script] : script;
      return http.Response(jsonEncode(body), 200, headers: {'content-type': 'application/json'});
    }),
  );
  final host = _Host();
  final runner = LayoutActionRunner(apiClient: api, host: host);
  final result = await runner.run(ButtonActionModel.performScript(id: 's', name: 'Totals', parameter: parameter));
  return (result, host, runner);
}

void main() {
  group('a script step calls the custom functions through the server', () {
    test('Set Variable and Set Field take the value the server gives, with the variables sent', () async {
      final (result, host, _) = await _run([
        _step(1, 'Set Variable', {'name': r'$n', 'value': '6'}),
        _step(2, 'Set Variable', {'name': r'$d', 'value': r'Doubled ( $n )'}),
        _step(3, 'Set Field', {'field': 'total', 'value': r'$d', 'value_is_calculation': true}),
      ]);
      expect(result.completed, isTrue, reason: result.message);
      expect(host.fields['total'], '12');
      // The steps the runner evaluates itself make no request.
      expect(host.requests, hasLength(1));
      expect(host.requests.single.variables[r'$n'], '6');
    });

    test('an If condition calling a custom function picks the branch the server says', () async {
      final (result, host, _) = await _run([
        _step(1, 'Set Variable', {'name': r'$n', 'value': '3'}),
        _step(2, 'If', {'condition': r'IsBig ( $n )'}),
        _step(3, 'Set Field', {'field': 'size', 'value': 'big'}),
        _step(4, 'Else', {}),
        _step(5, 'Set Field', {'field': 'size', 'value': 'small'}),
        _step(6, 'End If', {}),
      ]);
      expect(result.completed, isTrue, reason: result.message);
      expect(host.fields['size'], 'small');
    });

    test('a loop evaluates its exit condition again on every turn', () async {
      final (result, host, _) = await _run([
        _step(1, 'Set Variable', {'name': r'$n', 'value': '0'}),
        _step(2, 'Loop', {}),
        _step(3, 'Set Variable', {'name': r'$n', 'value': r'$n + 4'}),
        _step(4, 'Exit Loop If', {'condition': r'IsBig ( $n )'}),
        _step(5, 'End Loop', {}),
        _step(6, 'Set Field', {'field': 'n', 'value': r'$n', 'value_is_calculation': true}),
      ]);
      expect(result.completed, isTrue, reason: result.message);
      expect(host.fields['n'], '12');
      expect(host.requests, hasLength(3), reason: 'one request per evaluation');
    });

    test('the Get functions only the client knows are sent with the formula', () async {
      final (result, host, _) = await _run([
        _step(1, 'Set Field', {
          'field': 'twice',
          'value': 'Doubled ( Get ( ScriptParameter ) )',
          'value_is_calculation': true,
        }),
      ], parameter: '21');
      expect(result.completed, isTrue, reason: result.message);
      expect(host.fields['twice'], '42');
      expect(host.requests.single.get['ScriptParameter'], '21');
    });

    test('the variables a formula sets with Let are kept', () async {
      final (result, _, runner) = await _run([
        _step(1, 'Set Variable', {'name': r'$x', 'value': r'Let ( $$seen = 7 ; Doubled ( 2 ) )'}),
      ]);
      expect(result.completed, isTrue, reason: result.message);
      expect(runner.globalVariables[r'$$seen'], '7');
    });

    test('a formula that fails stops the script, naming the step and the formula', () async {
      final (result, host, _) = await _run([
        _step(1, 'Set Field', {'field': 'a', 'value': 'Tripled ( 2 )', 'value_is_calculation': true}),
        _step(2, 'Set Field', {'field': 'b', 'value': 'after'}),
      ]);
      expect(result.completed, isFalse);
      expect(result.message, contains('Set Field'));
      expect(result.message, contains('"Tripled ( 2 )"'));
      expect(result.message, contains('the function Tripled is not known'));
      expect(host.fields, isEmpty);
    });
  });
}
