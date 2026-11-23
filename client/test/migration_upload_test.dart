import 'dart:convert';
import 'dart:typed_data';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

// A native .fmp12 copy travels as a raw multipart part, not as base64 inside
// the JSON body the server limits to 64 MiB (#126).
void main() {
  const reportJson = {
    'status': 'preflight_ok',
    'report': {
      'source_format': 'filemaker_ddr_xml',
      'solution_name': 'Synthetic',
      'counts': {'tables': 1, 'records_ready': 1},
    },
  };

  test('a native copy is uploaded as the raw native part', () async {
    late http.Request sent;
    final client = ApiClient(
      baseUrl: 'http://f4b.test',
      httpClient: MockClient((request) async {
        sent = request;
        return http.Response(jsonEncode(reportJson), 200,
            headers: {'content-type': 'application/json'});
      }),
    );
    final native = Uint8List.fromList([0, 1, 2, 0xff, 0xfe, 72, 66, 65, 77, 55]);

    final report = await client.migratePreflight(
      designXml: '<FMPReport/>',
      solutionName: ' Synthetic ',
      fmp12Bytes: native,
      fmp12FileName: 'Visits.fmp12',
    );

    expect(report.solutionName, 'Synthetic');
    expect(sent.url.path, '/api/v1/solutions/migrate/preflight');
    expect(sent.headers['content-type'], startsWith('multipart/form-data; boundary='));
    expect(sent.headers.containsKey('X-File4Base-Encrypted'), isFalse);
    final body = latin1.decode(sent.bodyBytes);
    expect(body, contains('name="request"'));
    expect(body, contains('{"solution_name":"Synthetic"}'));
    expect(body, contains('name="design"; filename="design.xml"'));
    expect(body, contains('<FMPReport/>'));
    expect(body, contains('name="native"; filename="Visits.fmp12"'));
    expect(body, contains(latin1.decode(native)));
    expect(body, isNot(contains('fmp12_base64')));
    expect(body, isNot(contains(base64Encode(native))));
  });

  test('without a native copy the request stays JSON', () async {
    late http.Request sent;
    final client = ApiClient(
      baseUrl: 'http://f4b.test',
      httpClient: MockClient((request) async {
        sent = request;
        return http.Response(
          jsonEncode({
            'status': 'migrated',
            'records_inserted': 0,
            'report': reportJson['report'],
          }),
          201,
          headers: {'content-type': 'application/json'},
        );
      }),
    );

    await client.migrateApply(
      designXml: '<FMPReport/>',
      dataFiles: const {'Visits': '<FMPXMLRESULT/>'},
      targetDatabase: 'target',
    );

    expect(sent.url.path, '/api/v1/solutions/migrate/apply');
    expect(sent.headers['content-type'], startsWith('application/json'));
    final decoded = jsonDecode(sent.body) as Map<String, dynamic>;
    expect(decoded['design_xml'], '<FMPReport/>');
    expect(decoded['data_files'], {'Visits': '<FMPXMLRESULT/>'});
    expect(decoded['target_database'], 'target');
    expect(decoded.containsKey('fmp12_base64'), isFalse);
  });
}
