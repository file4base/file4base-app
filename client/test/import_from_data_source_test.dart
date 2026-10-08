// #47 — importing records from another SQL database instead of a file.

import 'dart:typed_data';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_io/import_records_dialog.dart';
import 'package:file4base_client/features/data_io/manage_data_sources_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _table = TableModel(id: 't1', name: 'members', displayName: 'Members', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'full_name', displayName: 'Full Name', fieldType: 'TEXT'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'city', displayName: 'City', fieldType: 'TEXT'),
]);

const _source = DataSourceModel(
  id: 'src-1',
  name: 'Legacy members',
  engine: 'postgres',
  host: 'db.example',
  port: 5432,
  database: 'legacy',
  username: 'reader',
);

class _SourceApi extends ApiClient {
  _SourceApi({this.sources = const [_source], this.tables = const ['people', 'orders']})
      : super(baseUrl: 'http://localhost:1');

  final List<DataSourceModel> sources;
  final List<String> tables;

  /// What the dialog asked for, so the test can read it back.
  ExternalSourceRef? previewedWith;
  ExternalSourceRef? importedWith;
  String? listedPassword;
  Uint8List? previewedBytes;

  @override
  Future<({List<DataSourceEngine> engines, List<DataSourceModel> sources})> listDataSources() async => (
        sources: sources,
        engines: const [
          DataSourceEngine(engine: 'postgres', label: 'PostgreSQL', defaultPort: 5432),
          DataSourceEngine(engine: 'mysql', label: 'MySQL / MariaDB', defaultPort: 3306),
        ],
      );

  @override
  Future<List<String>> listDataSourceTables(String id, {String password = ''}) async {
    listedPassword = password;
    return tables;
  }

  @override
  Future<ImportPreviewModel> previewImport(
    String table, {
    String fileName = '',
    Uint8List? bytes,
    String? format,
    bool hasHeader = true,
    String? delimiter,
    String? sheet,
    String? recordElement,
    ExternalSourceRef? dataSource,
  }) async {
    previewedWith = dataSource;
    previewedBytes = bytes;
    return const ImportPreviewModel(
      columns: ['full_name', 'city'],
      rows: [
        ['Sophie Tang', 'Hong Kong'],
        ['Gerard LeFranc', 'Paris'],
      ],
      rowCount: 2,
    );
  }

  @override
  Future<ImportReportModel> importRecords(
    String table, {
    String fileName = '',
    Uint8List? bytes,
    required Map<String, dynamic> options,
    String? format,
    bool hasHeader = true,
    String? delimiter,
    String? sheet,
    String? recordElement,
    ExternalSourceRef? dataSource,
  }) async {
    importedWith = dataSource;
    return const ImportReportModel(rows: 2, added: 2, fields: ['full_name', 'city']);
  }
}

Future<_SourceApi> pumpDialog(WidgetTester tester, {_SourceApi? api}) async {
  tester.view.physicalSize = const Size(1400, 1100);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);

  final client = api ?? _SourceApi();
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: ImportRecordsDialog(
        table: _table,
        apiClient: client,
        initialSource: ImportSourceKind.external,
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return client;
}

void main() {
  group('Import from an external SQL data source', () {
    testWidgets('lists the tables of the source and previews the one chosen', (tester) async {
      final api = await pumpDialog(tester);

      expect(find.text('From a SQL data source'), findsOneWidget);
      expect(find.textContaining('Legacy members'), findsWidgets);

      // The password is typed for this import and travels with the request.
      await tester.enterText(find.widgetWithText(TextField, 'Password'), 'hunter2');
      await tester.tap(find.byKey(const ValueKey('import-read-source')));
      await tester.pumpAndSettle();
      expect(api.listedPassword, 'hunter2');

      // Choosing a table previews it, and the preview names the source.
      await tester.tap(find.byKey(const ValueKey('import-source-table')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('orders').last);
      await tester.pumpAndSettle();

      expect(api.previewedWith?.id, 'src-1');
      expect(api.previewedWith?.table, 'orders');
      expect(api.previewedWith?.password, 'hunter2');
      expect(api.previewedBytes, isNull,
          reason: 'nothing is uploaded: the server reads the source');
      expect(find.textContaining('2 row(s), 2 column(s)'), findsOneWidget);

      // The mapping is guessed from the column names, and importing sends
      // the same source reference.
      await tester.tap(find.byKey(const ValueKey('import-start')));
      await tester.pumpAndSettle();
      expect(api.importedWith?.table, 'orders');
      // The report names the source it read from, not a file.
      expect(find.text('2 row(s) read from Legacy members · orders'), findsOneWidget);
    });

    testWidgets('a SQL source is not asked whether its first row is a header', (tester) async {
      final api = await pumpDialog(tester);
      await tester.enterText(find.widgetWithText(TextField, 'Password'), 'hunter2');
      await tester.tap(find.byKey(const ValueKey('import-read-source')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('import-source-table')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('people').last);
      await tester.pumpAndSettle();

      expect(api.previewedWith?.table, 'people');
      // A result set names its own columns.
      expect(find.byKey(const ValueKey('import-has-header')), findsNothing);
      expect(find.textContaining('Dates are written'), findsOneWidget);
    });

    testWidgets('a SELECT is read instead of a table when asked', (tester) async {
      final api = await pumpDialog(tester);

      await tester.tap(find.text('A SELECT'));
      await tester.pumpAndSettle();
      await tester.enterText(
          find.widgetWithText(TextField, 'SELECT statement'), 'SELECT * FROM people');
      await tester.tap(find.byKey(const ValueKey('import-read-source')));
      await tester.pumpAndSettle();

      expect(api.previewedWith?.query, 'SELECT * FROM people');
      expect(api.previewedWith?.table, isEmpty);
    });

    testWidgets('with no source registered it says who can add one', (tester) async {
      await pumpDialog(tester, api: _SourceApi(sources: const []));
      expect(find.textContaining('No data source is registered'), findsWidgets);
      final importButton = tester.widget<FilledButton>(find.byKey(const ValueKey('import-start')));
      expect(importButton.onPressed, isNull);
    });

    testWidgets('switching back to a file asks for one again', (tester) async {
      await pumpDialog(tester);
      await tester.tap(find.text('From a file'));
      await tester.pumpAndSettle();
      expect(find.text('Choose a file...'), findsOneWidget);
      expect(find.byKey(const ValueKey('import-blocker')), findsOneWidget);
    });
  });

  group('Manage data sources', () {
    testWidgets('an owner can add one, and the editor has no password field', (tester) async {
      tester.view.physicalSize = const Size(1400, 1100);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: ManageDataSourcesDialog(apiClient: _SourceApi(), canEdit: true),
        ),
      ));
      await tester.pumpAndSettle();

      expect(find.text('Legacy members'), findsOneWidget);
      expect(find.textContaining('PostgreSQL · db.example:5432 · legacy'), findsOneWidget);

      await tester.tap(find.text('New data source...'));
      await tester.pumpAndSettle();
      expect(find.text('New Data Source'), findsOneWidget);
      // The catalog stores no password, so the editor offers none: only the
      // name of an environment variable the server reads.
      expect(find.widgetWithText(TextField, 'Password'), findsNothing);
      expect(find.textContaining('environment variable'), findsWidgets);
    });

    testWidgets('a user who is not an owner reads the list and is told why', (tester) async {
      tester.view.physicalSize = const Size(1400, 1100);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: ManageDataSourcesDialog(apiClient: _SourceApi(), canEdit: false),
        ),
      ));
      await tester.pumpAndSettle();

      expect(find.text('New data source...'), findsNothing);
      expect(find.textContaining('Only an owner can add or change a data source'), findsOneWidget);
      expect(find.byTooltip('Test connection'), findsOneWidget);
    });
  });
}
