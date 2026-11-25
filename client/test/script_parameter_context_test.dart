import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// A window on a record of `Plants`, whose `name` is "own". A related
/// `Supplier::name` exists only through the relationship: the server knows
/// it, the record in hand does not.
class _Host implements LayoutActionHost {
  final fields = <String, String>{};

  @override
  Future<bool> actionSetField(String field, String value) async {
    fields[field] = value;
    return true;
  }

  @override
  String actionResolveText(String text) => text == '{{name}}' ? 'own' : text;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _ContextHost extends _Host implements CalculationContextHost {
  final evaluated = <String>[];

  @override
  Set<String> get actionOwnOccurrenceNames => {'plants'};

  @override
  Future<String?> actionEvaluateCalculation(String expression) async {
    evaluated.add(expression);
    return expression.trim() == 'Supplier::name' ? 'related' : null;
  }
}

Map<String, dynamic> _script(String id, String name, List<Map<String, dynamic>> steps) => {
      'id': id,
      'name': name,
      'is_active': true,
      'steps': steps,
    };

Map<String, dynamic> _step(int n, String type, Map<String, dynamic> params) => {
      'id': 'step-$n',
      'sequence_idx': n,
      'step_type': type,
      'params': params,
      'is_enabled': true,
    };

LayoutActionRunner _runner(_Host host) {
  final scripts = {
    'child': _script('child', 'Child', [
      _step(1, 'Set Field', {'field': 'got', 'value': 'Get ( ScriptParameter )', 'value_is_calculation': true}),
    ]),
    'parent': _script('parent', 'Parent', [
      _step(1, 'Perform Script', {'script_name': 'Child', 'script_parameter': 'Supplier::name'}),
    ]),
  };
  final api = ApiClient(
    baseUrl: 'http://test-server:8080',
    httpClient: MockClient((request) async {
      final body = request.url.path == '/api/v1/schemas/scripts'
          ? scripts.values.toList()
          : scripts[request.url.path.split('/').last];
      return http.Response(jsonEncode(body ?? {'detail': 'not found'}), body == null ? 404 : 200,
          headers: {'content-type': 'application/json'});
    }),
  );
  return LayoutActionRunner(apiClient: api, host: host);
}

void main() {
  group('a parameter reading a related field is evaluated in the related context (#26)', () {
    test('a button parameter on a related field is not read from the same-named field of the record', () async {
      final host = _ContextHost();
      final result = await _runner(host)
          .run(const ButtonActionModel.performScript(id: 'child', name: 'Child', parameter: 'Supplier::name'));
      expect(result.completed, isTrue);
      expect(host.fields['got'], 'related');
      expect(host.evaluated, ['Supplier::name']);
    });

    test("a field of the window's own occurrence is still read from the record", () async {
      final host = _ContextHost();
      final result = await _runner(host)
          .run(const ButtonActionModel.performScript(id: 'child', name: 'Child', parameter: 'Plants::name'));
      expect(result.completed, isTrue);
      expect(host.fields['got'], 'own');
      expect(host.evaluated, isEmpty);
    });

    test('a Perform Script step passes the related value too', () async {
      final host = _ContextHost();
      final result = await _runner(host).run(const ButtonActionModel.performScript(id: 'parent', name: 'Parent'));
      expect(result.completed, isTrue);
      expect(host.fields['got'], 'related');
    });

    test('without a context to evaluate it in, the script stops rather than read the wrong field', () async {
      final host = _Host();
      final result = await _runner(host)
          .run(const ButtonActionModel.performScript(id: 'child', name: 'Child', parameter: 'Supplier::name'));
      expect(result.completed, isFalse);
      expect(host.fields, isNot(contains('got')));
    });

    test('a parameter the server cannot evaluate stops the script', () async {
      final host = _ContextHost();
      final result = await _runner(host)
          .run(const ButtonActionModel.performScript(id: 'child', name: 'Child', parameter: 'Elsewhere::name'));
      expect(result.completed, isFalse);
      expect(host.fields, isNot(contains('got')));
    });
  });
}
