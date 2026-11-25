// Field Options > Validation: "Validated by calculation", "Validate only if
// field has been modified" and "Allow user to override during data entry".
// Synthetic data only.

import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/schema_manager/field_options_dialog.dart';
import 'package:file4base_client/features/script_workspace/calculation_builder_dialog.dart';
import 'package:file4base_client/main.dart' show apiClientProvider;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

const _orders = TableModel(id: 't1', name: 'orders', displayName: 'Orders', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'quantity', displayName: 'Quantity', fieldType: 'NUMBER'),
]);

class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');
  String? savedRules;
  final checked = <String>[];

  @override
  Future<List<TableOccurrenceModel>> listOccurrences() async =>
      const [TableOccurrenceModel(id: 'o1', baseTableId: 't1', name: 'orders', xPos: 0, yPos: 0)];

  @override
  Future<List<RelationshipModel>> listRelationships() async => const [];

  @override
  Future<List<TableModel>> listTables() async => const [_orders];

  @override
  Future<List<CustomFunctionModel>> listCustomFunctions() async =>
      const [CustomFunctionModel(id: 'f1', name: 'MaxOf', parameters: ['n'], formula: 'n * 10')];

  @override
  Future<FormulaCheckModel> checkFormula(String formula,
      {String? table, String? function, List<String>? parameters}) async {
    checked.add('$formula in $table');
    return const FormulaCheckModel(valid: true);
  }

  @override
  Future<ColumnModel> updateColumn(
    String tableId,
    String columnId, {
    required String displayName,
    String? defaultValue,
    bool updateDefaultValue = false,
    String? calculationFormula,
    bool updateCalculationFormula = false,
    String? validationRules,
    bool updateValidationRules = false,
    int? maxRepetitions,
  }) async {
    savedRules = validationRules;
    return _orders.columns[1];
  }

  Map<String, dynamic> get rules => jsonDecode(savedRules!) as Map<String, dynamic>;
}

Future<_Api> _open(WidgetTester tester, {String? rules}) async {
  tester.view.physicalSize = const Size(1600, 1200);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final api = _Api();
  const c = ColumnModel(id: 'c1', tableId: 't1', name: 'quantity', displayName: 'Quantity', fieldType: 'NUMBER');
  final column = ColumnModel(
    id: c.id,
    tableId: c.tableId,
    name: c.name,
    displayName: c.displayName,
    fieldType: c.fieldType,
    validationRules: rules,
  );
  await tester.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(api)],
    child: MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => TextButton(
            onPressed: () => FieldOptionsDialog.show(context, table: _orders, column: column),
            child: const Text('open'),
          ),
        ),
      ),
    ),
  ));
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
  await tester.tap(find.text('Validation'));
  await tester.pumpAndSettle();
  return api;
}

Future<void> _tap(WidgetTester tester, Finder finder) async {
  await tester.pumpAndSettle();
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

Future<void> _ok(WidgetTester tester) async {
  await tester.tap(find.widgetWithText(FilledButton, 'OK'));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('the validation formula is written with the calculation editor and saved with its options',
      (tester) async {
    final api = await _open(tester);
    await _tap(tester, find.byKey(const Key('validationByCalculation')));
    expect(find.byType(CalculationBuilderDialog), findsOneWidget, reason: 'turning it on asks for the formula');
    final editor = find.descendant(
        of: find.byType(CalculationBuilderDialog),
        matching: find.byWidgetPredicate((w) => w is TextField && w.maxLines == null));
    await tester.enterText(editor, 'Self <= MaxOf ( 3 )');
    await tester.tap(find.byKey(const Key('calculationOk')));
    await tester.pumpAndSettle();
    expect(api.checked, ['Self <= MaxOf ( 3 ) in orders'], reason: 'the server checks it before it is kept');
    expect(find.text('Self <= MaxOf ( 3 )'), findsOneWidget);

    await _tap(tester, find.byKey(const Key('validationOnlyIfModified')));
    await _tap(tester, find.byKey(const Key('validationAllowOverride')));
    await _ok(tester);
    final rules = api.rules;
    expect(rules['calculation_enabled'], isTrue);
    expect(rules['calculation'], 'Self <= MaxOf ( 3 )');
    expect(rules['calculation_context'], 'orders');
    expect(rules['calculation_always'], isTrue);
    expect(rules['allow_override'], isTrue);
  });

  testWidgets('stored rules this dialog does not show are kept', (tester) async {
    final api = await _open(tester, rules: '{"calculation_enabled":true,"calculation":"Self > 0","future_rule":"kept"}');
    expect(tester.widget<Checkbox>(find.byKey(const Key('validationByCalculation'))).value, isTrue);
    expect(tester.widget<Checkbox>(find.byKey(const Key('validationOnlyIfModified'))).value, isTrue);
    await _ok(tester);
    expect(api.rules['future_rule'], 'kept');
    expect(api.rules['calculation'], 'Self > 0');
    expect(api.rules['calculation_always'], isFalse);
  });
}
