import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/related_records.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

// A portal's rows are evaluated in the layout's record (#222): the request
// names it, and another solution's rows are evaluated through their
// occurrence's records route.

const _companies = TableModel(
  id: 't-companies',
  name: 'companies',
  displayName: 'Companies',
);
const _customers = TableModel(
  id: 't-customers',
  name: 'customers',
  displayName: 'Customers',
);

RelatedContext _related({
  String? recordId = 'co-1',
  String? layoutOccurrence,
  String? fromDatabase,
}) => RelatedContext(
  apiClient: ApiClient(baseUrl: 'http://test-server:8080'),
  fromTable: 'companies',
  fromDatabase: fromDatabase,
  recordId: recordId,
  layoutOccurrence: layoutOccurrence,
  relationshipsById: const {},
  occurrencesById: const {
    'occ-anchor': TableOccurrenceModel(
      id: 'occ-anchor',
      baseTableId: 't-companies',
      name: 'CO__Companies',
      xPos: 0,
      yPos: 0,
    ),
    'occ-companies': TableOccurrenceModel(
      id: 'occ-companies',
      baseTableId: 't-companies',
      name: 'Companies',
      xPos: 0,
      yPos: 0,
    ),
    'occ-customers': TableOccurrenceModel(
      id: 'occ-customers',
      baseTableId: 't-customers',
      name: 'Customers',
      xPos: 0,
      yPos: 0,
    ),
  },
  tablesById: const {'t-companies': _companies, 't-customers': _customers},
  formatValue: (_, raw) => '$raw',
);

void main() {
  test(
    "the rows' context is the record in hand, seen from the layout's occurrence",
    () {
      expect(_related(layoutOccurrence: 'occ-anchor').rowContext, (
        occurrence: 'occ-anchor',
        recordId: 'co-1',
      ));
      expect(
        _related(layoutOccurrence: 'co__companies').rowContext,
        (occurrence: 'occ-anchor', recordId: 'co-1'),
        reason: 'named by its name, sent by its id',
      );
      expect(
        _related().rowContext,
        (occurrence: 'occ-companies', recordId: 'co-1'),
        reason: "without one, the table's own occurrence, named like it",
      );
      expect(
        _related(layoutOccurrence: 'Companies Display Name').rowContext,
        (occurrence: 'occ-companies', recordId: 'co-1'),
        reason:
            "an occurrence of no table of the layout's (a default layout's display name) is the table's own",
      );
      expect(
        _related(layoutOccurrence: 'occ-customers').rowContext,
        (occurrence: 'occ-companies', recordId: 'co-1'),
        reason: 'nor is one of another table',
      );
      expect(
        _related(recordId: null).rowContext,
        isNull,
        reason: 'no record in hand',
      );
      expect(
        _related(fromDatabase: 'peer').rowContext,
        isNull,
        reason:
            "a layout of another solution's table evaluates its portal rows on their own",
      );
    },
  );

  test(
    "another solution's rows on another solution's layout are not evaluated",
    () {
      const relationship = RelationshipModel(
        id: 'rel-1',
        name: 'companies_customers',
        leftOccurrenceId: 'occ-companies',
        leftColumnId: 'cc1',
        rightOccurrenceId: 'occ-customers',
        rightColumnId: 'uc1',
      );
      const local = RelatedTarget(
        relationship: relationship,
        occurrence: TableOccurrenceModel(
          id: 'occ-customers',
          baseTableId: 't-customers',
          name: 'Customers',
          xPos: 0,
          yPos: 0,
        ),
        table: _customers,
      );
      const external = RelatedTarget(
        relationship: relationship,
        occurrence: TableOccurrenceModel(
          id: 'occ-remote',
          baseTableId: '',
          name: 'Remote Customers',
          xPos: 0,
          yPos: 0,
          external: ExternalOccurrenceModel(
            fileReferenceId: 'ref-1',
            fileReference: 'Peer',
            table: 'customers',
            database: 'peer',
          ),
        ),
        table: _customers,
      );
      expect(_related().evaluatesRowsOf(external), isTrue);
      expect(
        _related(fromDatabase: 'peer').evaluatesRowsOf(local),
        isTrue,
        reason: 'evaluated on their own, through their occurrence',
      );
      expect(
        _related(fromDatabase: 'peer').evaluatesRowsOf(external),
        isFalse,
        reason: "no layout's record to evaluate them in: skipped, not refused",
      );
    },
  );

  test(
    "the context is sent, and an occurrence's rows go through its records route",
    () async {
      final requests = <http.Request>[];
      final api = ApiClient(
        baseUrl: 'http://test-server:8080',
        httpClient: MockClient((request) async {
          requests.add(request);
          return http.Response(
            jsonEncode({
              'records': {
                'cu-1': [
                  {'expression': 'x', 'text': '1'},
                ],
              },
            }),
            200,
            headers: {'content-type': 'application/json'},
          );
        }),
      );
      const context = (occurrence: 'occ-companies', recordId: 'co-1');

      final local = await api.evaluateForRecords(
        'customers',
        expressions: ['x'],
        recordIds: ['cu-1'],
        occurrence: 'occ-customers',
        context: context,
      );
      final external = await api
          .forOccurrence('occ remote')
          .evaluateForRecords(
            'customers',
            expressions: ['x'],
            recordIds: ['cu-1'],
            occurrence: 'occ remote',
            context: context,
          );
      await api.evaluateForRecords(
        'customers',
        expressions: ['x'],
        recordIds: ['cu-1'],
      );

      expect(local['cu-1']?.single.text, '1');
      expect(external['cu-1']?.single.text, '1');
      expect(
        [for (final r in requests) '${r.method} ${r.url.path}'],
        [
          'POST /api/v1/data/customers/evaluate',
          'POST /api/v1/occurrences/occ%20remote/records/evaluate',
          'POST /api/v1/data/customers/evaluate',
        ],
      );
      expect((jsonDecode(requests[0].body) as Map)['context'], {
        'occurrence': 'occ-companies',
        'record_id': 'co-1',
      });
      expect((jsonDecode(requests[1].body) as Map)['context'], {
        'occurrence': 'occ-companies',
        'record_id': 'co-1',
      });
      expect(
        (jsonDecode(requests[2].body) as Map).containsKey('context'),
        isFalse,
        reason: 'a record on its own',
      );
    },
  );
}
