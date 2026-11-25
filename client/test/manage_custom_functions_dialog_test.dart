// File > Manage > Custom Functions: the list, the editor, the conflicts the
// server reports, and the calculation editor's custom functions and check.

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/schema_manager/manage_custom_functions_dialog.dart';
import 'package:file4base_client/features/script_workspace/calculation_builder_dialog.dart';
import 'package:file4base_client/main.dart' show apiClientProvider;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

ApiException _conflict() => ApiException(
      statusCode: 409,
      title: 'Custom Function In Use',
      detail: 'Formulas call Doubled',
      raw: const {
        'usages': [
          {'kind': 'calculation', 'id': 'c1', 'name': 'orders::total'},
          {'kind': 'script_step', 'id': 's1', 'name': 'Totals', 'detail': 'step 2'},
        ],
      },
    );

class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');
  final functions = <CustomFunctionModel>[
    const CustomFunctionModel(id: 'f1', name: 'Doubled', parameters: ['n'], formula: 'n * 2'),
    const CustomFunctionModel(id: 'f2', name: 'Secret', formula: '42', access: 'full_access'),
  ];
  final calls = <String>[];
  bool inUse = false;
  FormulaCheckModel check = const FormulaCheckModel(valid: true);
  final checked = <({String formula, String? function, List<String>? parameters})>[];

  @override
  Future<List<CustomFunctionModel>> listCustomFunctions() async => List.of(functions);

  @override
  Future<CustomFunctionModel> createCustomFunction(CustomFunctionModel fn) async {
    calls.add('create ${fn.name}(${fn.parameters.join(';')}) ${fn.access}: ${fn.formula}');
    final saved = fn.copyWith(id: 'f${functions.length + 1}');
    functions.add(saved);
    return saved;
  }

  @override
  Future<CustomFunctionModel> updateCustomFunction(CustomFunctionModel fn, {bool force = false}) async {
    calls.add('update ${fn.id} ${fn.name} force=$force');
    if (inUse && !force) throw _conflict();
    functions[functions.indexWhere((f) => f.id == fn.id)] = fn;
    return fn;
  }

  @override
  Future<void> deleteCustomFunction(String id, {bool force = false}) async {
    calls.add('delete $id force=$force');
    if (inUse && !force) throw _conflict();
    functions.removeWhere((f) => f.id == id);
  }

  @override
  Future<FormulaCheckModel> checkFormula(String formula,
      {String? table, String? function, List<String>? parameters}) async {
    checked.add((formula: formula, function: function, parameters: parameters));
    return check;
  }
}

Future<_Api> _open(WidgetTester tester, Widget child, {_Api? api}) async {
  tester.view.physicalSize = const Size(1600, 1200);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final client = api ?? _Api();
  await tester.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client)],
    child: MaterialApp(home: Scaffold(body: child)),
  ));
  await tester.pumpAndSettle();
  return client;
}

Future<void> _tap(WidgetTester tester, Finder f) async {
  await tester.ensureVisible(f);
  await tester.tap(f);
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('the list shows each function with its parameters and access', (tester) async {
    await _open(tester, const ManageCustomFunctionsDialog());
    expect(find.text('Doubled ( n )'), findsOneWidget);
    expect(find.text('Secret'), findsOneWidget);
    expect(find.text('Full access only'), findsOneWidget);
    expect(find.text('2 custom functions'), findsOneWidget);
  });

  testWidgets('a new function is saved with its name, parameters, formula and access', (tester) async {
    final api = await _open(tester, const ManageCustomFunctionsDialog());
    await _tap(tester, find.byKey(const Key('customFunctionNew')));
    await tester.enterText(find.byKey(const Key('customFunctionName')), 'Margin');
    for (final p in ['price', 'cost']) {
      await tester.enterText(find.byKey(const Key('customFunctionParameter')), p);
      await _tap(tester, find.byKey(const Key('customFunctionAddParameter')));
    }
    await tester.enterText(find.byKey(const Key('customFunctionFormula')), 'price - cost');
    await _tap(tester, find.byKey(const Key('customFunctionFullAccess')));
    await _tap(tester, find.byKey(const Key('customFunctionOk')));
    expect(api.calls, ['create Margin(price;cost) full_access: price - cost']);
    expect(find.text('Margin ( price ; cost )'), findsOneWidget);
  });

  testWidgets('the editor refuses a name already taken or not a name', (tester) async {
    final api = await _open(tester, const ManageCustomFunctionsDialog());
    await _tap(tester, find.byKey(const Key('customFunctionNew')));
    await tester.enterText(find.byKey(const Key('customFunctionName')), 'doubled');
    await tester.enterText(find.byKey(const Key('customFunctionFormula')), '1');
    await _tap(tester, find.byKey(const Key('customFunctionOk')));
    expect(find.textContaining('already a custom function'), findsOneWidget);
    await tester.enterText(find.byKey(const Key('customFunctionName')), '2nd');
    await _tap(tester, find.byKey(const Key('customFunctionOk')));
    expect(find.textContaining('not a valid function name'), findsOneWidget);
    expect(api.calls, isEmpty);
  });

  testWidgets('a change the server says breaks formulas lists them, and is saved only when asked', (tester) async {
    final api = await _open(tester, const ManageCustomFunctionsDialog())
      ..inUse = true;
    await _tap(tester, find.text('Doubled ( n )'));
    await _tap(tester, find.byKey(const Key('customFunctionEdit')));
    await tester.enterText(find.byKey(const Key('customFunctionName')), 'Twice');
    await _tap(tester, find.byKey(const Key('customFunctionOk')));
    expect(find.byKey(const Key('customFunctionUsage')), findsNWidgets(2));
    expect(find.textContaining('Calculation field: orders::total'), findsOneWidget);
    expect(find.textContaining('Script: Totals (step 2)'), findsOneWidget);
    await _tap(tester, find.byKey(const Key('customFunctionForce')));
    expect(api.calls, ['update f1 Twice force=false', 'update f1 Twice force=true']);
    expect(find.text('Twice ( n )'), findsOneWidget);
  });

  testWidgets('deleting a function formulas call asks again, and Cancel keeps it', (tester) async {
    final api = await _open(tester, const ManageCustomFunctionsDialog())
      ..inUse = true;
    await _tap(tester, find.text('Doubled ( n )'));
    await _tap(tester, find.byKey(const Key('customFunctionDelete')));
    await _tap(tester, find.byKey(const Key('customFunctionConfirmDelete')));
    await _tap(tester, find.text('Cancel'));
    expect(api.calls, ['delete f1 force=false']);
    expect(find.text('Doubled ( n )'), findsOneWidget);
  });

  testWidgets('Duplicate creates a copy under a free name', (tester) async {
    final api = await _open(tester, const ManageCustomFunctionsDialog());
    await _tap(tester, find.text('Doubled ( n )'));
    await _tap(tester, find.byKey(const Key('customFunctionDuplicate')));
    expect(api.calls, ['create DoubledCopy(n) all: n * 2']);
  });

  group('the calculation editor', () {
    testWidgets('lists the custom functions as a category of their own', (tester) async {
      await _open(tester, const CalculationBuilderDialog());
      await _tap(tester, find.text('All').first);
      await _tap(tester, find.text('Custom Functions').last);
      expect(find.text('Doubled'), findsOneWidget);
      expect(find.text('Custom function (full access only)'), findsOneWidget);
      expect(find.text('Sum'), findsNothing);
    });

    testWidgets('OK has the server check the formula, and an error keeps the editor open', (tester) async {
      final api = await _open(
        tester,
        const CalculationBuilderDialog(
          initialFormula: 'Doubled ( 1 ; 2 )',
          functionName: 'Margin',
          functionParameters: ['price'],
        ),
      );
      api.check = const FormulaCheckModel(valid: false, message: 'Doubled takes 1 argument, not 2', position: 0);
      await _tap(tester, find.byKey(const Key('calculationOk')));
      expect(find.byType(CalculationBuilderDialog), findsOneWidget);
      expect(find.textContaining('Doubled takes 1 argument, not 2 (at character 1)'), findsOneWidget);
      expect(api.checked.single.function, 'Margin');
      expect(api.checked.single.parameters, ['price']);
      // A custom function's formula reads its parameters, not fields.
      expect(find.text('Parameters'), findsOneWidget);
      expect(find.text('price'), findsOneWidget);
    });
  });
}
