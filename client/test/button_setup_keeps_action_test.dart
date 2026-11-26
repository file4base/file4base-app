import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/button_setup_dialog.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class _ScriptsApi extends ApiClient {
  _ScriptsApi() : super(baseUrl: 'http://localhost:1');

  @override
  Future<List<ScriptModel>> listScripts({String? database}) async =>
      [const ScriptModel(id: 's1', name: 'Archive'), const ScriptModel(id: 's2', name: 'Restore')];
}

Future<LayoutObjectModel?> _edit(WidgetTester tester, LayoutObjectModel button, Future<void> Function() change) async {
  tester.view.physicalSize = const Size(1400, 1000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  LayoutObjectModel? result;
  await tester.pumpWidget(MaterialApp(
    home: Builder(
      builder: (context) => TextButton(
        onPressed: () async => result = await ButtonSetupDialog.show(context, button: button, apiClient: _ScriptsApi()),
        child: const Text('open'),
      ),
    ),
  ));
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
  await change();
  await tester.tap(find.text('OK'));
  await tester.pumpAndSettle();
  return result;
}

void main() {
  // A migrated button kept switched off, because its step names a field that
  // was not imported.
  final switchedOff = LayoutObjectModel(
    id: 'b1',
    type: 'button', x: 0, y: 0, width: 120, height: 30,
    text: 'Clear',
    action: ButtonActionModel.fromJson(const {
      'type': 'single_step',
      'step_type': 'set_field',
      'params': {'field': 'missing_field', 'value': ''},
      'enabled': false,
      'field_problem': 'names a field that is not among the imported fields',
    }),
  );

  testWidgets('renaming a switched-off button in Button Setup keeps it switched off', (tester) async {
    final saved = await _edit(tester, switchedOff, () async {
      await tester.enterText(find.widgetWithText(TextField, 'Clear'), 'Clear all');
    });
    expect(saved!.text, 'Clear all');
    expect(saved.action!.enabled, isFalse);
    expect(saved.action!.extra['field_problem'], isNotNull);
  });

  testWidgets('a switched-off script button stays off when only its label changes', (tester) async {
    final button = LayoutObjectModel(
      id: 'b2',
      type: 'button', x: 0, y: 0, width: 120, height: 30,
      text: 'Archive',
      action: ButtonActionModel.fromJson(const {
        'type': 'perform_script',
        'script_id': 's1',
        'script_name': 'Archive',
        'enabled': false,
      }),
    );
    final saved = await _edit(tester, button, () async {
      await tester.enterText(find.widgetWithText(TextField, 'Archive'), 'Archive now');
    });
    expect(saved!.text, 'Archive now');
    expect(saved.action!.enabled, isFalse);
  });

  testWidgets('choosing another script in Button Setup gives the button a new action', (tester) async {
    final button = LayoutObjectModel(
      id: 'b3',
      type: 'button', x: 0, y: 0, width: 120, height: 30,
      text: 'Go',
      action: ButtonActionModel.fromJson(const {
        'type': 'perform_script',
        'script_id': 's1',
        'script_name': 'Archive',
        'enabled': false,
      }),
    );
    final saved = await _edit(tester, button, () async {
      expect(find.byType(DropdownButtonFormField<String>), findsOneWidget);
      await tester.tap(find.text('Archive  (0 steps)'));
      await tester.pumpAndSettle();
      await tester.tap(find.textContaining('Restore').last);
      await tester.pumpAndSettle();
    });
    expect(saved!.action!.scriptId, 's2');
    expect(saved.action!.enabled, isTrue);
  });
}
