import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:flutter_test/flutter_test.dart';

class _Host implements LayoutActionHost {
  final fields = <String, String>{};
  final dialogs = <List<String>>[];
  @override
  Future<bool> actionSetField(String field, String value) async {
    fields[field] = value;
    return true;
  }

  @override
  String actionResolveText(String text) => text;
  @override
  Future<void> actionShowDialog(String title, String message) async {
    dialogs.add([title, message]);
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

void main() {
  test('imported field calculations evaluate before writing', () async {
    final host = _Host();
    final runner = LayoutActionRunner(apiClient: ApiClient(), host: host);
    final result = await runner.runStep('Set Field', {
      'field': 'status',
      'value': '"ready"',
      'value_is_calculation': true,
    }, depth: 0);
    expect(result.completed, isTrue);
    expect(host.fields['status'], 'ready');
    final variable = await runner.runStep(
      'Set Field',
      {
        'field': 'status',
        'value': r'$batch',
        'value_is_calculation': true,
      },
      depth: 0,
      localVars: {r'$batch': 'arrived'},
    );
    expect(variable.completed, isTrue);
    expect(host.fields['status'], 'arrived');
  });
  test(
    'unsupported imported expression stops before changing the record',
    () async {
      final host = _Host();
      final runner = LayoutActionRunner(apiClient: ApiClient(), host: host);
      final result = await runner.runStep('Set Field', {
        'field': 'status',
        'value': 'UnknownFunction(3)',
        'value_is_calculation': true,
      }, depth: 0);
      expect(result.completed, isFalse);
      expect(host.fields, isEmpty);
    },
  );
  test('ordinary literal actions retain their text', () async {
    final host = _Host();
    final runner = LayoutActionRunner(apiClient: ApiClient(), host: host);
    await runner.runStep('Set Field', {
      'field': 'status',
      'value': 'ready',
    }, depth: 0);
    expect(host.fields['status'], 'ready');
    await runner.runStep('Show Custom Dialog', {
      'title': '"Transfer"',
      'title_is_calculation': true,
      'message': '"Ready"',
      'message_is_calculation': true,
    }, depth: 0);
    expect(host.dialogs, [
      ['Transfer', 'Ready'],
    ]);
  });
}
