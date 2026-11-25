import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/schema_manager/widgets/relationship_graph_widget.dart';
import 'package:file4base_client/features/schema_manager/widgets/specify_relationship_dialog.dart';
import 'package:file4base_client/features/schema_manager/widgets/specify_table_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

// An occurrence of another solution's table (#external occurrences, phase 1):
// it has no local base table, only a data source and the table it is bound
// to there, if any.

const _tables = [
  TableModel(
    id: 't-visits',
    name: 'visits',
    displayName: 'Visits',
    columns: [
      ColumnModel(id: 'c-id', tableId: 't-visits', name: 'id', displayName: 'id', fieldType: 'TEXT', isNullable: false, isPrimaryKey: true),
      ColumnModel(id: 'c-site', tableId: 't-visits', name: 'site_code', displayName: 'site_code', fieldType: 'TEXT'),
    ],
  ),
];

final _occurrences = [
  const TableOccurrenceModel(id: 'o-visits', baseTableId: 't-visits', name: 'Visits', xPos: 40, yPos: 40),
  TableOccurrenceModel.fromJson(const {
    'id': 'o-sites',
    'name': 'Sites',
    'x_pos': 400,
    'y_pos': 40,
    'external': {'file_reference_id': 'fr-1', 'file_reference': 'Catalog', 'table': '', 'database': ''},
  }),
];

final _relationship = RelationshipModel.fromJson(const {
  'id': 'r-1',
  'name': 'Visits_Sites',
  'left_occurrence_id': 'o-visits',
  'left_column_id': 'c-site',
  'right_occurrence_id': 'o-sites',
  'right_column_id': '',
  'operator': '=',
  'predicates': [
    {'left_column_id': 'c-site', 'left_column_name': 'site_code', 'operator': '=', 'right_column_id': '', 'right_column_name': 'Code'},
  ],
});

void main() {
  test('an external occurrence has no base table and keeps its data source', () {
    final occ = _occurrences[1];
    expect(occ.isExternal, isTrue);
    expect(occ.baseTableId, isNull);
    expect(occ.external!.fileReference, 'Catalog');
    expect(occ.external!.table, isEmpty);

    final json = occ.toJson();
    expect(json.containsKey('base_table_id'), isFalse);
    expect(json['external'], {'file_reference_id': 'fr-1', 'file_reference': 'Catalog'});

    // A local occurrence is written as it always was.
    expect(_occurrences[0].isExternal, isFalse);
    expect(_occurrences[0].toJson().containsKey('external'), isFalse);
    expect(_occurrences[0].toJson()['base_table_id'], 't-visits');
  });

  test('a criterion side on an external occurrence is sent by field name', () {
    final json = _relationship.predicates.single.toJson();
    expect(json['right_column_id'], '');
    expect(json['right_column_name'], 'Code');
    // A local side is sent by id only, as before.
    expect(json.containsKey('left_column_name'), isFalse);
  });

  testWidgets('the graph draws an external occurrence with an italic title naming its data source', (tester) async {
    await tester.binding.setSurfaceSize(const Size(1200, 800));
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: RelationshipGraphWidget(
          tables: _tables,
          occurrences: _occurrences,
          relationships: [_relationship],
          apiClient: ApiClient(baseUrl: 'http://localhost:8080'),
          onSchemaChanged: () {},
        ),
      ),
    ));
    await tester.pump();

    final title = tester.widget<Text>(find.byKey(const ValueKey('external-occurrence-title-o-sites')));
    expect(title.style?.fontStyle, FontStyle.italic);
    expect(title.textSpan!.toPlainText(), contains('Catalog'));
    expect(title.textSpan!.toPlainText(), contains('Sites'));
    // Its fields are the ones its relationships match on, by name.
    expect(find.text('Code'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('the occurrence dialog does not offer to turn an external occurrence into a local one', (tester) async {
    Map<String, dynamic>? result;
    await tester.pumpWidget(MaterialApp(
      home: Builder(
        builder: (context) => TextButton(
          onPressed: () async {
            result = await SpecifyTableDialog.show(
              context,
              tables: _tables,
              existingOccurrences: _occurrences,
              initialOccurrence: _occurrences[1],
            );
          },
          child: const Text('open'),
        ),
      ),
    ));
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    expect(find.text('Catalog'), findsOneWidget);
    expect(find.text('(not bound yet)'), findsOneWidget);
    // No local table to pick.
    expect(find.text('Visits'), findsNothing);

    await tester.enterText(find.byType(TextField), 'Sites 2');
    await tester.tap(find.text('OK'));
    await tester.pumpAndSettle();
    expect(result, {'name': 'Sites 2'});
  });

  testWidgets('editing a relationship to an external occurrence keeps its criteria by name', (tester) async {
    await tester.binding.setSurfaceSize(const Size(1200, 1000));
    Map<String, dynamic>? result;
    await tester.pumpWidget(MaterialApp(
      home: Builder(
        builder: (context) => TextButton(
          onPressed: () async {
            result = await SpecifyRelationshipDialog.show(
              context,
              occurrences: _occurrences,
              tables: _tables,
              initialRelationship: _relationship,
            );
          },
          child: const Text('open'),
        ),
      ),
    ));
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    expect(find.text('Sites::Code'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('relationship-right-allow-creation')));
    await tester.pump();
    await tester.tap(find.text('OK'));
    await tester.pumpAndSettle();

    expect(result, isNotNull);
    expect(result!['allow_creation'], isTrue);
    final predicates = result!['predicates'] as List<RelationshipPredicateModel>;
    expect(predicates.single.toJson(), {
      'left_column_id': 'c-site',
      'operator': '=',
      'right_column_id': '',
      'right_column_name': 'Code',
    });
  });
}
