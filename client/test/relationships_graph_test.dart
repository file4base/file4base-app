import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/schema_manager/widgets/specify_table_dialog.dart';
import 'package:file4base_client/features/schema_manager/widgets/specify_relationship_dialog.dart';
import 'package:file4base_client/features/schema_manager/widgets/relationship_graph_widget.dart';

class MockApiClient extends ApiClient {
  MockApiClient() : super(baseUrl: 'http://localhost:8080');

  @override
  Future<TableOccurrenceModel> createOccurrence({
    required String baseTableId,
    required String name,
    double xPos = 100.0,
    double yPos = 100.0,
  }) async {
    return TableOccurrenceModel(
      id: 'occ-new',
      baseTableId: baseTableId,
      name: name,
      xPos: xPos,
      yPos: yPos,
    );
  }

  @override
  Future<TableOccurrenceModel> updateOccurrence(
    String id, {
    String? name,
    double? xPos,
    double? yPos,
  }) async {
    return TableOccurrenceModel(
      id: id,
      baseTableId: 'tbl-1',
      name: name ?? 'Updated',
      xPos: xPos ?? 100.0,
      yPos: yPos ?? 100.0,
    );
  }

  @override
  Future<void> deleteOccurrence(String id) async {}

  @override
  Future<RelationshipModel> createRelationship({
    required String leftOccurrenceId,
    required String leftColumnId,
    required String rightOccurrenceId,
    required String rightColumnId,
    String operator = '=',
    String? name,
    bool allowCreation = false,
    bool cascadeDelete = false,
    String? sortRelated,
  }) async {
    return RelationshipModel(
      id: 'rel-new',
      name: name ?? 'rel',
      leftOccurrenceId: leftOccurrenceId,
      leftColumnId: leftColumnId,
      rightOccurrenceId: rightOccurrenceId,
      rightColumnId: rightColumnId,
      operator: operator,
      allowCreation: allowCreation,
      cascadeDelete: cascadeDelete,
      sortRelated: sortRelated,
    );
  }

  @override
  Future<RelationshipModel> updateRelationship(
    String id, {
    String? name,
    String? operator,
    bool? allowCreation,
    bool? cascadeDelete,
    String? sortRelated,
  }) async {
    return RelationshipModel(
      id: id,
      name: name ?? 'rel',
      leftOccurrenceId: 'occ-1',
      leftColumnId: 'col-1',
      rightOccurrenceId: 'occ-2',
      rightColumnId: 'col-2',
      operator: operator ?? '=',
      allowCreation: allowCreation ?? false,
      cascadeDelete: cascadeDelete ?? false,
      sortRelated: sortRelated,
    );
  }

  @override
  Future<void> deleteRelationship(String id) async {}
}

void main() {
  group('Relationship Models Unit Tests', () {
    test('TableOccurrenceModel serialization, copyWith, and json conversion', () {
      final occ = TableOccurrenceModel(
        id: 'occ-123',
        baseTableId: 'tbl-456',
        name: 'CLIENTES 2',
        xPos: 250.0,
        yPos: 180.0,
      );

      final json = occ.toJson();
      expect(json['id'], 'occ-123');
      expect(json['base_table_id'], 'tbl-456');
      expect(json['name'], 'CLIENTES 2');
      expect(json['x_pos'], 250.0);
      expect(json['y_pos'], 180.0);

      final fromJson = TableOccurrenceModel.fromJson(json);
      expect(fromJson.id, occ.id);
      expect(fromJson.name, occ.name);
      expect(fromJson.xPos, occ.xPos);

      final copied = occ.copyWith(name: 'CLIENTES 3', xPos: 300.0);
      expect(copied.name, 'CLIENTES 3');
      expect(copied.xPos, 300.0);
      expect(copied.yPos, 180.0);
      expect(copied.id, 'occ-123');
    });

    test('RelationshipModel serialization and defaults', () {
      final rel = RelationshipModel(
        id: 'rel-1',
        name: 'Clientes_Facturas',
        leftOccurrenceId: 'occ-1',
        leftColumnId: 'col-1',
        rightOccurrenceId: 'occ-2',
        rightColumnId: 'col-2',
        operator: '=',
        allowCreation: true,
        cascadeDelete: false,
        sortRelated: 'asc',
      );

      final json = rel.toJson();
      expect(json['name'], 'Clientes_Facturas');
      expect(json['operator'], '=');
      expect(json['allow_creation'], true);
      expect(json['cascade_delete'], false);
      expect(json['sort_related'], 'asc');

      final fromJson = RelationshipModel.fromJson(json);
      expect(fromJson.id, 'rel-1');
      expect(fromJson.allowCreation, true);
      expect(fromJson.operator, '=');
    });
  });

  group('SpecifyTableDialog Widget Tests', () {
    final testTables = [
      TableModel(
        id: 'tbl-1',
        name: 'clientes',
        displayName: 'Clientes',
        columns: [
          ColumnModel(
            id: 'col-1',
            tableId: 'tbl-1',
            name: '_kp_ID_clientes',
            displayName: '_kp_ID_clientes',
            fieldType: 'VARCHAR',
            isNullable: false,
            isPrimaryKey: true,
          ),
        ],
      ),
      TableModel(
        id: 'tbl-2',
        name: 'facturas',
        displayName: 'Facturas',
        columns: [
          ColumnModel(
            id: 'col-2',
            tableId: 'tbl-2',
            name: '_kp_ID_factura',
            displayName: '_kp_ID_factura',
            fieldType: 'VARCHAR',
            isNullable: false,
            isPrimaryKey: true,
          ),
        ],
      ),
    ];

    final existingOccs = [
      TableOccurrenceModel(
        id: 'occ-1',
        baseTableId: 'tbl-1',
        name: 'Clientes',
        xPos: 100,
        yPos: 100,
      ),
    ];

    testWidgets('renders dialog with title, table list, and suggests Clientes 2', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SpecifyTableDialog(
              tables: testTables,
              existingOccurrences: existingOccs,
              activeDatabaseName: 'demo.fmp',
            ),
          ),
        ),
      );

      expect(find.text('Especificar tabla'), findsOneWidget);
      expect(find.text('Fuente de datos:'), findsOneWidget);
      expect(find.textContaining('demo.fmp'), findsOneWidget);
      expect(find.text('Clientes'), findsWidgets);
      expect(find.text('Facturas'), findsOneWidget);

      // Since 'Clientes' already exists, suggested name should be 'Clientes 2'
      expect(find.text('Clientes 2'), findsOneWidget);

      expect(find.text('Cancelar'), findsOneWidget);
      expect(find.text('Aceptar'), findsOneWidget);
    });

    testWidgets('selecting Facturas updates suggested name to Facturas', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SpecifyTableDialog(
              tables: testTables,
              existingOccurrences: existingOccs,
              activeDatabaseName: 'demo.fmp',
            ),
          ),
        ),
      );

      await tester.tap(find.text('Facturas'));
      await tester.pumpAndSettle();

      expect(find.text('Facturas'), findsWidgets);
    });
  });

  group('SpecifyRelationshipDialog Widget Tests', () {
    final testTables = [
      TableModel(
        id: 'tbl-1',
        name: 'clientes',
        displayName: 'Clientes',
        columns: [
          ColumnModel(
            id: 'col-1',
            tableId: 'tbl-1',
            name: '_kp_ID_clientes',
            displayName: '_kp_ID_clientes',
            fieldType: 'VARCHAR',
            isNullable: false,
            isPrimaryKey: true,
          ),
        ],
      ),
      TableModel(
        id: 'tbl-2',
        name: 'facturas',
        displayName: 'Facturas',
        columns: [
          ColumnModel(
            id: 'col-2',
            tableId: 'tbl-2',
            name: '_kf_CLIENTE_factura',
            displayName: '_kf_CLIENTE_factura',
            fieldType: 'VARCHAR',
            isNullable: false,
          ),
        ],
      ),
    ];

    final occs = [
      TableOccurrenceModel(id: 'occ-1', baseTableId: 'tbl-1', name: 'CLIENTES', xPos: 100, yPos: 100),
      TableOccurrenceModel(id: 'occ-2', baseTableId: 'tbl-2', name: 'clientes_FACTURAS', xPos: 350, yPos: 100),
    ];

    testWidgets('renders dialog with occurrences, fields, criteria, and canonical options', (tester) async {
      await tester.binding.setSurfaceSize(const Size(1200, 900));
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SpecifyRelationshipDialog(
              occurrences: occs,
              tables: testTables,
            ),
          ),
        ),
      );

      expect(find.text('Especificar relación'), findsOneWidget);
      expect(find.text('CLIENTES'), findsWidgets);
      expect(find.text('clientes_FACTURAS'), findsWidgets);
      expect(find.text('='), findsWidgets);
      expect(find.text('Permitir la creación de registros en esta tabla a través de esta relación'), findsOneWidget);
      expect(find.text('Eliminar registros relacionados en esta tabla cuando se elimine un registro en la otra tabla'), findsOneWidget);
      expect(find.text('Ordenar registros relacionados'), findsOneWidget);
    });
  });

  group('RelationshipGraphWidget Canvas Tests', () {
    final testTables = [
      TableModel(
        id: 'tbl-1',
        name: 'clientes',
        displayName: 'Clientes',
        columns: [
          ColumnModel(
            id: 'col-1',
            tableId: 'tbl-1',
            name: '_kp_ID_clientes',
            displayName: '_kp_ID_clientes',
            fieldType: 'VARCHAR',
            isNullable: false,
            isPrimaryKey: true,
          ),
        ],
      ),
      TableModel(
        id: 'tbl-2',
        name: 'facturas',
        displayName: 'Facturas',
        columns: [
          ColumnModel(
            id: 'col-2',
            tableId: 'tbl-2',
            name: '_kf_CLIENTE_factura',
            displayName: '_kf_CLIENTE_factura',
            fieldType: 'VARCHAR',
            isNullable: false,
          ),
        ],
      ),
    ];

    final occs = [
      TableOccurrenceModel(id: 'occ-1', baseTableId: 'tbl-1', name: 'CLIENTES', xPos: 80, yPos: 80),
      TableOccurrenceModel(id: 'occ-2', baseTableId: 'tbl-2', name: 'clientes_FACTURAS', xPos: 320, yPos: 80),
      TableOccurrenceModel(id: 'occ-3', baseTableId: 'tbl-1', name: 'CLIENTES 2', xPos: 200, yPos: 250),
    ];

    final rels = [
      RelationshipModel(
        id: 'rel-1',
        name: 'Clientes_Facturas',
        leftOccurrenceId: 'occ-1',
        leftColumnId: 'col-1',
        rightOccurrenceId: 'occ-2',
        rightColumnId: 'col-2',
        operator: '=',
      ),
    ];

    testWidgets('renders toolbar, TO boxes, fields, and operator badge [ = ]', (tester) async {
      await tester.binding.setSurfaceSize(const Size(1200, 800));

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: RelationshipGraphWidget(
              tables: testTables,
              occurrences: occs,
              relationships: rels,
              apiClient: MockApiClient(),
              onSchemaChanged: () {},
            ),
          ),
        ),
      );

      // Verify Toolbar
      expect(find.text('Tabla...'), findsOneWidget);
      expect(find.text('Relación...'), findsOneWidget);
      expect(find.text('3 ocurrencias  •  1 relaciones'), findsOneWidget);

      // Verify Table Occurrence Boxes
      expect(find.text('CLIENTES'), findsOneWidget);
      expect(find.text('clientes_FACTURAS'), findsOneWidget);
      expect(find.text('CLIENTES 2'), findsOneWidget);
      expect(find.text('_kp_ID_clientes'), findsWidgets);

      // Verify Operator Badge
      expect(find.text('='), findsOneWidget);

      // Tap on CLIENTES 2 to select it (amber halo)
      await tester.tap(find.text('CLIENTES 2'));
      await tester.pumpAndSettle();

      // Tap on Operator Badge to select the relationship
      await tester.tap(find.text('='));
      await tester.pumpAndSettle();
    });
  });
}
