import 'dart:convert';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/script_workspace/calculation_builder_dialog.dart';
import 'package:file4base_client/features/script_workspace/script_workspace_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  group('Script Models Unit Tests', () {
    test('ScriptStepModel serialization and category assignment', () {
      final step = ScriptStepModel(
        id: 's-100',
        scriptId: 'scr-1',
        sequenceIdx: 1,
        stepType: 'go_to_layout',
        params: {'layout_name': 'Invoices_Detail'},
        isEnabled: true,
      );

      expect(step.category, 'navigation');
      expect(step.displayName, 'Go to Layout');
      expect(step.previewText, '[Invoices_Detail]');

      final json = step.toJson();
      expect(json['step_type'], 'go_to_layout');
      expect(json['sequence_idx'], 1);

      final decoded = ScriptStepModel.fromJson(json);
      expect(decoded.id, 's-100');
      expect(decoded.stepType, 'go_to_layout');
      expect(decoded.params['layout_name'], 'Invoices_Detail');
      expect(decoded.isEnabled, true);
    });

    test('ScriptModel serialization and steps list', () {
      final script = ScriptModel(
        id: 'scr-100',
        name: 'test_script',
        contextTable: 'Invoices',
        isActive: true,
        steps: [
          const ScriptStepModel(
            id: 'st-1',
            sequenceIdx: 1,
            stepType: 'set_variable',
            params: {'variable': r'$subtotal', 'calc': '100'},
          ),
          const ScriptStepModel(
            id: 'st-2',
            sequenceIdx: 2,
            stepType: 'commit_records',
          ),
        ],
      );

      final json = script.toJson();
      expect(json['name'], 'test_script');
      expect(json['context_table'], 'Invoices');
      expect((json['steps'] as List).length, 2);

      final decoded = ScriptModel.fromJson(json);
      expect(decoded.id, 'scr-100');
      expect(decoded.name, 'test_script');
      expect(decoded.steps.length, 2);
      expect(decoded.steps[0].category, 'fields');
      expect(decoded.steps[1].category, 'records');
    });

    test('ScriptCatalogItem contains categories and script instructions', () {
      final catalog = ScriptCatalogItem.catalog;
      expect(catalog.isNotEmpty, true);

      final categories = catalog.map((c) => c.category).toSet();
      expect(categories.contains('NAVIGATION'), true);
      expect(categories.contains('RECORDS'), true);
      expect(categories.contains('CONTROL & LOGIC'), true);
      expect(categories.contains('INTEGRATION & DATA'), true);

      expect(catalog.any((c) => c.stepType == 'go_to_layout'), true);
      expect(catalog.any((c) => c.stepType == 'set_variable'), true);
      expect(catalog.any((c) => c.stepType == 'if'), true);
      expect(catalog.any((c) => c.stepType == 'loop'), true);
      expect(catalog.any((c) => c.stepType == 'perform_rest_api'), true);
    });
  });

  group('CalculationBuilderDialog Widget Tests', () {
    testWidgets('renders formula builder with operators, tables and functions', (tester) async {
      tester.view.physicalSize = const Size(1200, 750);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      await tester.pumpWidget(
        const ProviderScope(
          child: MaterialApp(
            home: Scaffold(
              body: CalculationBuilderDialog(
                initialFormula: 'Sum(Items::Price)',
                contextTable: 'Invoices',
                availableTables: ['Invoices', 'Items', 'Customers'],
                fieldsByTable: {
                  'Invoices': ['id', 'total', 'customer_id'],
                  'Items': ['id', 'invoice_id', 'price', 'quantity'],
                },
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('Specify Calculation'), findsOneWidget);
      expect(find.text('Context: Invoices'), findsOneWidget);
      expect(find.text('Table occurrence'), findsOneWidget);
      expect(find.text('Built-in functions'), findsOneWidget);
      expect(find.text('Formula / Calculation:'), findsOneWidget);
      expect(find.text('Sum(Items::Price)'), findsOneWidget);
      expect(find.text('OK'), findsOneWidget);
    });
  });

  group('ScriptWorkspaceDialog Widget Tests', () {
    testWidgets('renders 3-panel workspace with scripts explorer, steps editor and catalog', (tester) async {
      tester.view.physicalSize = const Size(1300, 800);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      final mockClient = MockClient((request) async {
        if (request.url.path == '/api/v1/schemas/scripts') {
          return http.Response(
            jsonEncode([
              {
                'id': 's-1',
                'name': 'test_workflow',
                'context_table': 'Invoices',
                'is_active': true,
                'steps': [
                  {
                    'id': 'st-1',
                    'sequence_idx': 1,
                    'step_type': 'go_to_layout',
                    'params': {'layout_name': 'Invoices_Detail'},
                    'is_enabled': true,
                  },
                  {
                    'id': 'st-2',
                    'sequence_idx': 2,
                    'step_type': 'set_variable',
                    'params': {'variable': r'$subtotal', 'calc': '100'},
                    'is_enabled': true,
                  },
                ],
              }
            ]),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        if (request.url.path == '/api/v1/schemas/tables') {
          return http.Response(
            jsonEncode([
              {
                'id': 't-1',
                'name': 'Invoices',
                'display_name': 'Invoices',
                'columns': [],
              }
            ]),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        if (request.url.path == '/api/v1/schemas/layouts') {
          return http.Response(
            jsonEncode([
              {
                'id': 'l-1',
                'name': 'Invoices_Detail',
                'table_occurrence_id': 'to-1',
                'definition': {},
              }
            ]),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        return http.Response('[]', 200, headers: {'content-type': 'application/json'});
      });

      final apiClient = ApiClient(baseUrl: 'http://localhost:8080', httpClient: mockClient);

      await tester.pumpWidget(
        ProviderScope(
          child: MaterialApp(
            home: Scaffold(
              body: ScriptWorkspaceDialog(
                apiClient: apiClient,
                databaseName: 'test_db',
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Top bar elements
      expect(find.text('Script Workspace'), findsOneWidget);
      expect(find.text('Database: test_db'), findsOneWidget);
      expect(find.text('Run'), findsOneWidget);
      expect(find.text('Step Preview'), findsOneWidget);
      expect(find.text('Save'), findsOneWidget);

      // Run/Debug must not fake an execution (#24): Run is disabled and the
      // step preview says that nothing is executed.
      final run = tester.widget<ButtonStyleButton>(
          find.ancestor(of: find.text('Run'), matching: find.byWidgetPredicate((w) => w is ButtonStyleButton)));
      expect(run.onPressed, isNull);
      await tester.tap(find.text('Step Preview'));
      await tester.pumpAndSettle();
      expect(find.textContaining('Preview only: no step is executed'), findsOneWidget);
      expect(find.textContaining('SUCCESS'), findsNothing);
      while (tester.widget<OutlinedButton>(find.widgetWithText(OutlinedButton, 'Next step')).onPressed != null) {
        await tester.tap(find.text('Next step'));
        await tester.pumpAndSettle();
      }
      expect(find.text('End of script (preview, nothing was executed).'), findsOneWidget);
      await tester.tap(find.text('Close').last);
      await tester.pumpAndSettle();

      // Left panel
      expect(find.text('Scripts'), findsOneWidget);
      expect(find.text('test_workflow'), findsWidgets);

      // Center panel (step list and inspector)
      expect(find.text('01'), findsOneWidget);
      expect(find.text('02'), findsOneWidget);
      expect(find.text('Go to Layout'), findsWidgets);
      expect(find.text('Set Variable'), findsWidgets);
      expect(find.text('[Invoices_Detail]'), findsOneWidget);

      // Right panel (catalog)
      expect(find.text('Step catalog'), findsOneWidget);
      expect(find.text('NAVIGATION'), findsNWidgets(2));
      expect(find.text('RECORDS'), findsOneWidget);
      expect(find.text('CONTROL & LOGIC'), findsOneWidget);

      // Scroll catalog to verify INTEGRATION & DATA
      final catalogScrollable = find.byType(Scrollable).last;
      await tester.scrollUntilVisible(
        find.text('INTEGRATION & DATA'),
        100,
        scrollable: catalogScrollable,
      );
      expect(find.text('INTEGRATION & DATA'), findsOneWidget);
    });
  });

  group('ScriptWorkspaceDialog saving', () {
    /// Simulates the server: PUT only accepts ids it assigned (400 otherwise,
    /// like local ids such as "script-123"), POST assigns a new id.
    MockClient fakeServer(Map<String, Map<String, dynamic>> stored, List<String> posted) {
      return MockClient((request) async {
        final path = request.url.path;
        if (path == '/api/v1/schemas/scripts' && request.method == 'GET') {
          return http.Response(jsonEncode(stored.values.toList()), 200);
        }
        if (path == '/api/v1/schemas/scripts' && request.method == 'POST') {
          final body = jsonDecode(request.body) as Map<String, dynamic>;
          final id = 'srv-${stored.length + 1}';
          stored[id] = {...body, 'id': id};
          posted.add(body['name'] as String);
          return http.Response(jsonEncode(stored[id]), 201);
        }
        if (path.startsWith('/api/v1/schemas/scripts/') && request.method == 'PUT') {
          final id = path.split('/').last;
          if (!stored.containsKey(id)) return http.Response('{"title":"Script Error"}', 400);
          stored[id] = jsonDecode(request.body) as Map<String, dynamic>;
          return http.Response(jsonEncode(stored[id]), 200);
        }
        return http.Response('[]', 200);
      });
    }

    Future<void> openWorkspace(WidgetTester tester, ApiClient api) async {
      tester.view.physicalSize = const Size(1400, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());
      await tester.pumpWidget(ProviderScope(
        child: MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: Center(
                child: TextButton(
                  onPressed: () => ScriptWorkspaceDialog.show(context, api),
                  child: const Text('open'),
                ),
              ),
            ),
          ),
        ),
      ));
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();
    }

    testWidgets('saving twice does not create duplicate scripts on the server', (tester) async {
      final stored = <String, Map<String, dynamic>>{};
      final posted = <String>[];
      await openWorkspace(tester, ApiClient(baseUrl: 'http://localhost', httpClient: fakeServer(stored, posted)));

      await tester.tap(find.text('Script').first); // new script
      await tester.pumpAndSettle();
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      final createdFirstSave = posted.length;
      expect(createdFirstSave, greaterThan(0));

      await tester.tap(find.text('Script').first); // one more script
      await tester.pumpAndSettle();
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(posted.length, createdFirstSave + 1, reason: 'only the new script is created on the second save');
      expect(stored.length, posted.length);
    });

    testWidgets('closing with unsaved scripts asks to save them', (tester) async {
      final stored = <String, Map<String, dynamic>>{};
      final posted = <String>[];
      await openWorkspace(tester, ApiClient(baseUrl: 'http://localhost', httpClient: fakeServer(stored, posted)));

      await tester.tap(find.text('Script').first);
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('Close window'));
      await tester.pumpAndSettle();
      expect(find.text('Unsaved scripts'), findsOneWidget);

      await tester.tap(find.descendant(of: find.byType(AlertDialog), matching: find.text('Save')));
      await tester.pumpAndSettle();
      expect(posted, isNotEmpty);
      expect(find.byType(ScriptWorkspaceDialog), findsNothing);
    });
  });
}
