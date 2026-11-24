import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/data_browser/external_layout.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

// A layout or a layout field of an occurrence of another solution's table
// (#107) is never bound to a local table or column: its records cannot be read
// from here yet.

const _local = TableOccurrenceModel(id: 'o-contacts', baseTableId: 't1', name: 'Contacts', xPos: 0, yPos: 0);

TableOccurrenceModel _external({String database = '', String table = ''}) => TableOccurrenceModel.fromJson({
      'id': 'o-remote',
      'name': 'Remote Occ',
      'x_pos': 0,
      'y_pos': 0,
      'external': {'file_reference_id': 'fr-1', 'file_reference': 'Catalog', 'table': table, 'database': database},
    });

LayoutModel _layoutOn(String occurrenceId) =>
    LayoutModel(id: 'l', name: 'Remote List', tableOccurrenceId: occurrenceId, definition: const {});

final _table = TableModel(id: 't1', name: 'contacts', displayName: 'Contacts', columns: const [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'status', displayName: 'Status', fieldType: 'varchar'),
]);

class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [
        {'id': '1', 'status': 'open'},
      ];
}

LayoutObjectModel _field(String id, double y, FieldBindingModel binding) => LayoutObjectModel(
      id: id,
      type: 'field',
      x: 100,
      y: y,
      width: 320,
      height: 30,
      fieldBinding: binding,
    );

void main() {
  test('a layout on an external occurrence is recognised, a local one is not', () {
    final remote = _external();
    final occurrences = [_local, remote];
    expect(externalOccurrenceOfLayout(_layoutOn('o-remote'), occurrences), same(remote));
    expect(externalOccurrenceOfLayout(_layoutOn('o-contacts'), occurrences), isNull);
    expect(externalOccurrenceOfLayout(_layoutOn('o-unknown'), occurrences), isNull);
  });

  testWidgets('the placeholder names the file reference and says the data is not available yet', (tester) async {
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(body: ExternalLayoutPlaceholder(layoutName: 'Remote List', occurrence: _external())),
    ));
    expect(find.textContaining('Layout "Remote List" shows "Remote Occ"'), findsOneWidget);
    expect(find.textContaining('file reference "Catalog" is not mapped'), findsOneWidget);
    expect(find.textContaining('not available yet'), findsOneWidget);
    expect(find.textContaining('record commands are disabled'), findsOneWidget);

    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: ExternalLayoutPlaceholder(
          layoutName: 'Remote List',
          occurrence: _external(database: 'catalog_db', table: 'items'),
        ),
      ),
    ));
    expect(find.textContaining('solution "catalog_db", table "items", through the file reference "Catalog"'),
        findsOneWidget);
  });

  test('only a field of an external occurrence gets the unavailable notice', () {
    final keys = externalOccurrenceKeys([_local, _external()]);
    expect(keys, {'o-remote', 'remote occ'});
    expect(
      externalFieldNotice(const FieldBindingModel(fieldName: 'status', tableOccurrence: 'Remote Occ'), keys),
      'Remote Occ::status is in another solution (not available yet)',
    );
    expect(externalFieldNotice(const FieldBindingModel(fieldName: 'status', tableOccurrence: 'Contacts'), keys), isNull);
    expect(externalFieldNotice(const FieldBindingModel(fieldName: 'status'), keys), isNull);
  });

  testWidgets('a field of an external occurrence is not bound to the local column of the same name', (tester) async {
    tester.view.physicalSize = const Size(1400, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    final layout = LayoutDefinitionModel(
      id: 'l',
      name: 'Contacts',
      tableOccurrence: 'Contacts',
      objects: [
        _field('local', 60, const FieldBindingModel(fieldName: 'status', tableOccurrence: 'Contacts')),
        _field('remote', 140, const FieldBindingModel(fieldName: 'status', tableOccurrence: 'Remote Occ')),
      ],
    );
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: DataBrowserWidget(
          table: _table,
          apiClient: _Api(),
          mode: OperationalMode.browse,
          layout: layout,
          externalOccurrences: externalOccurrenceKeys([_local, _external()]),
        ),
      ),
    ));
    await tester.pumpAndSettle();

    expect(find.text('open'), findsOneWidget, reason: 'only the local field shows the local value');
    final notice = find.text('Remote Occ::status is in another solution (not available yet)');
    expect(notice, findsOneWidget);
    expect(find.ancestor(of: notice, matching: find.byType(EditableText)), findsNothing,
        reason: 'the external field takes no entry');
  });
}
