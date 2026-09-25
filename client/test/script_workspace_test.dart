import 'dart:convert';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/models/script_models.dart';
import 'package:file4base_client/features/script_workspace/calculation_builder_dialog.dart';
import 'package:file4base_client/features/script_workspace/script_workspace_dialog.dart';
import 'package:flutter/material.dart';
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
      expect(step.displayName, 'Ir a Presentación');
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

    test('ScriptCatalogItem contains categories and FileMaker instructions', () {
      final catalog = ScriptCatalogItem.catalog;
      expect(catalog.isNotEmpty, true);

      final categories = catalog.map((c) => c.category).toSet();
      expect(categories.contains('NAVEGACIÓN'), true);
      expect(categories.contains('REGISTROS'), true);
      expect(categories.contains('CONTROL Y LÓGICA'), true);
      expect(categories.contains('INTEGRACIÓN Y DATOS'), true);

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
        const MaterialApp(
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
      );
      await tester.pumpAndSettle();

      expect(find.text('Especificar cálculo'), findsOneWidget);
      expect(find.text('Contexto: Invoices'), findsOneWidget);
      expect(find.text('Ocurrencia de tabla'), findsOneWidget);
      expect(find.text('Funciones integradas'), findsOneWidget);
      expect(find.text('Fórmula / Cálculo:'), findsOneWidget);
      expect(find.text('Sum(Items::Price)'), findsOneWidget);
      expect(find.text('Aceptar'), findsOneWidget);
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
        MaterialApp(
          home: Scaffold(
            body: ScriptWorkspaceDialog(
              apiClient: apiClient,
              databaseName: 'test_db',
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Top bar elements
      expect(find.text('Espacio de trabajo de guiones'), findsOneWidget);
      expect(find.text('Base: test_db'), findsOneWidget);
      expect(find.text('Ejecutar'), findsOneWidget);
      expect(find.text('Depurar'), findsOneWidget);
      expect(find.text('Guardar'), findsOneWidget);

      // Left panel
      expect(find.text('Guiones'), findsOneWidget);
      expect(find.text('test_workflow'), findsWidgets);

      // Center panel (step list and inspector)
      expect(find.text('01'), findsOneWidget);
      expect(find.text('02'), findsOneWidget);
      expect(find.text('Ir a Presentación'), findsWidgets);
      expect(find.text('Establecer Variable'), findsWidgets);
      expect(find.text('[Invoices_Detail]'), findsOneWidget);

      // Right panel (catalog)
      expect(find.text('Catálogo de pasos'), findsOneWidget);
      expect(find.text('NAVEGACIÓN'), findsOneWidget);
      expect(find.text('REGISTROS'), findsOneWidget);
      expect(find.text('CONTROL Y LÓGICA'), findsOneWidget);

      // Scroll catalog to verify INTEGRACIÓN Y DATOS
      final catalogScrollable = find.byType(Scrollable).last;
      await tester.scrollUntilVisible(
        find.text('INTEGRACIÓN Y DATOS'),
        100,
        scrollable: catalogScrollable,
      );
      expect(find.text('INTEGRACIÓN Y DATOS'), findsOneWidget);
    });
  });
}
