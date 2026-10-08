// #38 — the Import Records dialog, driven for real: it previews a file,
// matches its columns to the fields, and says what is missing.

import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_io/import_records_dialog.dart';

const _table = TableModel(id: 't1', name: 'customers', displayName: 'Customers', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'last_name', displayName: 'Last Name', fieldType: 'TEXT'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'city', displayName: 'City', fieldType: 'TEXT'),
  ColumnModel(id: 'c3', tableId: 't1', name: 'fee_paid', displayName: 'Fee Paid', fieldType: 'NUMBER'),
  ColumnModel(
    id: 'c4', tableId: 't1', name: 'annual_fee', displayName: 'Annual Fee',
    fieldType: 'CALCULATION', calculationFormula: '{"formula":"1","result_type":"Number"}',
  ),
  ColumnModel(
    id: 'c5', tableId: 't1', name: 'fee_total', displayName: 'Total Fees', fieldType: 'SUMMARY',
    calculationFormula: '{"summary_type":"total","field":"fee_paid"}',
  ),
]);

class _ImportApi extends ApiClient {
  final ImportPreviewModel preview;
  final List<Map<String, dynamic>> imports = [];
  Object? importError;

  _ImportApi({required this.preview}) : super(baseUrl: 'http://localhost:1');

  @override
  Future<ImportPreviewModel> previewImport(
    String table, {
    required String fileName,
    required Uint8List bytes,
    String? format,
    bool hasHeader = true,
    String? delimiter,
    String? sheet,
    String? recordElement,
  }) async =>
      preview;

  @override
  Future<ImportReportModel> importRecords(
    String table, {
    required String fileName,
    required Uint8List bytes,
    required Map<String, dynamic> options,
    String? format,
    bool hasHeader = true,
    String? delimiter,
    String? sheet,
    String? recordElement,
  }) async {
    imports.add(options);
    if (importError != null) throw importError!;
    final mappings = (options['mappings'] as List).length;
    return ImportReportModel(rows: 2, added: 2, fields: List.filled(mappings, 'f'));
  }
}

const _preview = ImportPreviewModel(
  columns: ['Last Name', 'Town', 'Fee Paid', 'Notes'],
  rows: [
    ['Durand', 'Paris', '100', 'nothing'],
    ['Smith', 'New York', '200', 'nothing'],
  ],
  rowCount: 2,
);

Future<_ImportApi> _pump(WidgetTester tester, {ImportPreviewModel? preview}) async {
  tester.view.physicalSize = const Size(1400, 1100);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);

  final api = _ImportApi(preview: preview ?? _preview);
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: ImportRecordsDialog(
        table: _table,
        apiClient: api,
        fileName: 'customers.csv',
        bytes: Uint8List.fromList(utf8.encode('Last Name,Town\nDurand,Paris\n')),
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return api;
}

void main() {
  testWidgets('the file is read and what is in it is reported', (tester) async {
    await _pump(tester);
    expect(find.textContaining('customers.csv'), findsOneWidget);
    expect(find.textContaining('2 row(s), 4 column(s)'), findsOneWidget);
  });

  testWidgets('each column gets a row, with a sample of its values', (tester) async {
    await _pump(tester);
    for (final column in _preview.columns) {
      expect(find.text(column), findsOneWidget, reason: column);
    }
    expect(find.text('Durand'), findsOneWidget, reason: 'the first value is shown as a sample');
  });

  testWidgets('columns are matched to the fields they look like', (tester) async {
    await _pump(tester);

    // "Last Name" matches the label, "Fee Paid" matches both.
    final lastName = tester.widget<DropdownButtonFormField<String>>(
        find.byKey(const ValueKey('import-map-0')));
    expect(lastName.initialValue, 'last_name');

    final feePaid = tester.widget<DropdownButtonFormField<String>>(
        find.byKey(const ValueKey('import-map-2')));
    expect(feePaid.initialValue, 'fee_paid');

    // "Town" and "Notes" match nothing, so they are left out rather than
    // guessed at.
    final town = tester.widget<DropdownButtonFormField<String>>(
        find.byKey(const ValueKey('import-map-1')));
    expect(town.initialValue, isNull);
  });

  testWidgets('a calculation and a summary field are not offered as targets',
      (tester) async {
    await _pump(tester);
    await tester.tap(find.byKey(const ValueKey('import-map-1')));
    await tester.pumpAndSettle();

    expect(find.text('City (TEXT)'), findsWidgets);
    expect(find.textContaining('Annual Fee'), findsNothing,
        reason: 'a calculation\'s formula owns its value');
    expect(find.textContaining('Total Fees'), findsNothing,
        reason: 'a summary has no value in a record');
  });

  testWidgets('importing sends the mapping and the settings', (tester) async {
    final api = await _pump(tester);
    await tester.tap(find.byKey(const ValueKey('import-start')));
    await tester.pumpAndSettle();

    expect(api.imports, hasLength(1));
    final options = api.imports.single;
    expect(options['action'], 'add');
    expect(options['date_order'], 'iso');

    final mappings = (options['mappings'] as List).cast<Map<String, dynamic>>();
    expect(mappings.map((m) => m['field']), containsAll(['last_name', 'fee_paid']));
    expect(mappings.map((m) => m['field']), isNot(contains('')),
        reason: 'a column that goes nowhere is not sent at all');
  });

  testWidgets('the report says what was done', (tester) async {
    await _pump(tester);
    await tester.tap(find.byKey(const ValueKey('import-start')));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('import-report')), findsOneWidget);
    expect(find.text('2 record(s) added'), findsOneWidget);
    expect(find.byKey(const ValueKey('import-done')), findsOneWidget);
  });

  testWidgets('an import that fails says why and stays open', (tester) async {
    final api = await _pump(tester);
    api.importError = Exception('row 2, Fee Paid: "abc" is not a number');

    await tester.tap(find.byKey(const ValueKey('import-start')));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('import-error')), findsOneWidget);
    expect(find.textContaining('row 2'), findsOneWidget);
    expect(find.byKey(const ValueKey('import-report')), findsNothing,
        reason: 'nothing was imported, so there is nothing to report');
  });

  group('what stops the import, said rather than left dead', () {
    testWidgets('no column sent anywhere', (tester) async {
      await _pump(tester);
      await tester.tap(find.text('Clear'));
      await tester.pumpAndSettle();

      expect(find.text('Send at least one column to a field.'), findsOneWidget);
      final button = tester.widget<FilledButton>(find.byKey(const ValueKey('import-start')));
      expect(button.onPressed, isNull);
    });

    testWidgets('a file with no rows', (tester) async {
      await _pump(tester,
          preview: const ImportPreviewModel(columns: ['a'], rows: [], rowCount: 0));
      expect(find.text('The file holds no rows.'), findsOneWidget);
    });

    testWidgets('updating with nothing to match on', (tester) async {
      await _pump(tester);
      await tester.ensureVisible(find.byKey(const ValueKey('import-action-update')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('import-action-update')));
      await tester.pumpAndSettle();

      expect(find.text('Choose a field to match records on.'), findsOneWidget);
      final button = tester.widget<FilledButton>(find.byKey(const ValueKey('import-start')));
      expect(button.onPressed, isNull);
    });
  });

  testWidgets('updating sends the fields to match on', (tester) async {
    final api = await _pump(tester);
    await tester.ensureVisible(find.byKey(const ValueKey('import-action-update')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('import-action-update')));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byKey(const ValueKey('import-match-last_name')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('import-match-last_name')));
    await tester.pumpAndSettle();

    await tester.ensureVisible(find.byKey(const ValueKey('import-add-unmatched')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('import-add-unmatched')));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const ValueKey('import-start')));
    await tester.pumpAndSettle();

    final options = api.imports.single;
    expect(options['action'], 'update_matching');
    expect(options['match_fields'], ['last_name']);
    expect(options['add_unmatched'], isTrue);
  });

  testWidgets('a file read in part says so', (tester) async {
    await _pump(tester,
        preview: const ImportPreviewModel(
            columns: ['Last Name'], rows: [['Durand']], rowCount: 1, truncated: true));
    expect(find.textContaining('the file was longer and was cut'), findsOneWidget);
  });

  testWidgets('a workbook offers its sheets', (tester) async {
    await _pump(tester,
        preview: const ImportPreviewModel(
          columns: ['Last Name'],
          rows: [['Durand']],
          rowCount: 1,
          sheets: ['Customers', 'Notes'],
        ));
    expect(find.byKey(const ValueKey('import-sheet')), findsOneWidget);
  });

  testWidgets('a plain text file offers no sheet picker', (tester) async {
    await _pump(tester);
    expect(find.byKey(const ValueKey('import-sheet')), findsNothing);
  });
}
