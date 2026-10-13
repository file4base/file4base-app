import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/solution_manager/filemaker_migration_dialog.dart';

void main() {
  test('isFmp12Binary detects .fmp12 extension and HBAM2 header', () {
    expect(
      FileMakerMigrationDialog.isFmp12Binary(
        'Specimens.fmp12',
        Uint8List.fromList([1, 2, 3]),
      ),
      isTrue,
    );
    final hbam = Uint8List.fromList([
      0x00,
      0x01,
      0x00,
      0x00,
      0x00,
      ...utf8.encode('HBAM2 FileMaker Pro 12.0'),
    ]);
    expect(FileMakerMigrationDialog.isFmp12Binary('unknown.bin', hbam), isTrue);
    expect(
      FileMakerMigrationDialog.isFmp12Binary(
        'Specimens.xml',
        Uint8List.fromList(utf8.encode('<FMPReport/>')),
      ),
      isFalse,
    );
  });

  testWidgets('FileMakerMigrationDialog runs preflight and displays report', (
    tester,
  ) async {
    final mockHttp = MockClient((req) async {
      if (req.url.path.endsWith('/api/v1/solutions/migrate/preflight')) {
        return http.Response(
          jsonEncode({
            'status': 'preflight_ok',
            'report': {
              'source_format': 'filemaker_ddr_xml',
              'solution_name': 'HerbariumArchive',
              'triggers_quarantined': true,
              'accounts_disabled': true,
              'counts': {
                'tables': 2,
                'fields': 5,
                'layouts': 1,
                'scripts': 1,
                'records_ready': 10,
              },
              'preserved': [
                {
                  'category': 'preserved',
                  'domain': 'table',
                  'source_name': 'Specimens',
                  'target_name': 'specimens',
                  'detail': 'Base table with 5 migrated fields',
                },
              ],
              'translated': [],
              'unsupported': [
                {
                  'category': 'unsupported',
                  'domain': 'script',
                  'source_name': 'Init > Perform AppleScript (step 2)',
                  'target_name': 'Init',
                  'detail': 'Unsupported FileMaker script step',
                },
              ],
              'rejected': [],
              'manual_action_required': [
                {
                  'category': 'manual_action_required',
                  'domain': 'trigger',
                  'source_name': 'Specimen Form (OnRecordLoad -> Init)',
                  'target_name': 'Specimen Form',
                  'detail': 'Layout script trigger quarantined on import',
                },
              ],
            },
          }),
          200,
          headers: {'content-type': 'application/json'},
        );
      }
      return http.Response('{}', 200);
    });

    final api = ApiClient(baseUrl: 'http://localhost:8080', httpClient: mockHttp);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: FileMakerMigrationDialog(
            apiClient: api,
            initialFmp12FileName: 'HerbariumArchive.fmp12',
          ),
        ),
      ),
    );

    expect(find.textContaining('HerbariumArchive.fmp12'), findsOneWidget);

    await tester.enterText(
      find.byKey(const Key('migration_design_xml_input')),
      '<FMPReport><File name="HerbariumArchive.fmp12"/></FMPReport>',
    );
    await tester.tap(find.byKey(const Key('migration_preflight_btn')));
    await tester.pumpAndSettle();

    expect(find.text('Solution: HerbariumArchive'), findsOneWidget);
    expect(find.text('Triggers Quarantined'), findsOneWidget);
    expect(
      find.textContaining('Layout script trigger quarantined on import'),
      findsOneWidget,
    );
  });
}
