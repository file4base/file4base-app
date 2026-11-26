// #191 — the migration review offers a transfer for each Import Records step
// that imports from another FileMaker file, shows the source table it
// inferred and how sure it is, and creates nothing until confirmed.

import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/solution_manager/migration_import_steps_panel.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// Two synthetic files: "Ledger" became another solution, "Shop" is this one.
List<Map<String, dynamic>> _steps() => [
  {
    'step_id': 's-pull',
    'script_id': 'sc-1',
    'script_name': 'Pull ledger',
    'step_index': 2,
    'source_file': 'Ledger',
    'target_table': 'orders',
    'method': 'update',
    'add_remaining': true,
    'auto_enter_fields': ['stamp'],
    'mappings': [
      {'position': 1, 'target_field': 'code', 'match': true},
      {'position': 2, 'target_field': 'total'},
    ],
    'suggested_database': 'ledger',
    'suggested_by': 'database_name',
  },
  {
    'step_id': 's-guess',
    'script_id': 'sc-2',
    'script_name': 'Pull days',
    'step_index': 1,
    'source_file': 'Ledger',
    'target_table': 'orders',
    'method': 'add',
    'mappings': [
      {'position': 1, 'target_field': 'note'},
    ],
    'suggested_database': 'ledger',
    'suggested_by': 'database_name',
  },
  {
    'step_id': 's-self',
    'script_id': 'sc-3',
    'script_name': 'Notes from customers',
    'step_index': 1,
    'source_file': 'Shop',
    'target_table': 'orders',
    'method': 'replace',
    'mappings': [
      {'position': 1, 'target_field': 'note'},
    ],
    'suggested_database': 'shop',
    'suggested_by': 'this_file',
  },
];

class _FakeServer {
  final infers = <Map<String, dynamic>>[];
  final confirms = <Map<String, dynamic>>[];

  static http.Response _json(Object body) =>
      http.Response(jsonEncode(body), 200, headers: {'content-type': 'application/json'});

  MockClient get client => MockClient((req) async {
    final path = req.url.path;
    if (path == '/api/v1/transfers/import-steps' && req.method == 'GET') {
      final done = {
        for (final c in confirms)
          for (final i in c['items'] as List) (i as Map)['step_id'],
      };
      return _json({'steps': _steps().where((s) => !done.contains(s['step_id'])).toList()});
    }
    if (path == '/api/v1/databases') {
      return _json({
        'databases': ['ledger', 'shop', 'archive'],
      });
    }
    if (path == '/api/v1/transfers/import-steps/infer') {
      final body = Map<String, dynamic>.from(jsonDecode(req.body) as Map);
      infers.add(body);
      if (body['source_database'] == 'ledger') {
        return _json({
          'source_database': 'ledger',
          'tables': ['days', 'entries'],
          'steps': [
            {
              'step_id': 's-pull',
              'table': 'entries',
              'confidence': 'high',
              'reason': 'the only table whose fields fit every position by type; names agree',
              'candidates': [
                {'table': 'entries', 'fields': 3, 'types_matched': 2, 'names_matched': 2, 'positions': 2, 'fits': true},
              ],
            },
            {
              'step_id': 's-guess',
              'reason': '2 tables fit every position by type and names do not tell them apart',
              'candidates': [
                {'table': 'days', 'fields': 3, 'types_matched': 1, 'positions': 1, 'fits': true},
                {'table': 'entries', 'fields': 3, 'types_matched': 1, 'positions': 1, 'fits': true},
              ],
            },
          ],
        });
      }
      return _json({
        'source_database': '',
        'tables': ['customers'],
        'steps': [
          {
            'step_id': 's-self',
            'table': 'customers',
            'confidence': 'medium',
            'reason': 'the only table whose fields fit every position by type',
            'candidates': [
              {'table': 'customers', 'fields': 2, 'types_matched': 1, 'positions': 1, 'fits': true},
            ],
          },
        ],
      });
    }
    if (path == '/api/v1/transfers/import-steps/confirm') {
      final body = Map<String, dynamic>.from(jsonDecode(req.body) as Map);
      confirms.add(body);
      return _json({
        'steps': [
          for (final i in body['items'] as List)
            {'step_id': (i as Map)['step_id'], 'transfer': 'Import orders (${i['step_id']})'},
        ],
      });
    }
    return http.Response('Not found', 404);
  });
}

Future<List<String>> _pump(WidgetTester tester, _FakeServer server) async {
  tester.view.physicalSize = const Size(1280, 1100);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final signedIn = <String>[];
  await tester.pumpWidget(
    MaterialApp(
      theme: ThemeData.dark(),
      home: Scaffold(
        body: SingleChildScrollView(
          child: MigrationImportStepsPanel(
            apiClient: ApiClient(baseUrl: 'http://localhost:8080', httpClient: server.client),
            database: 'shop',
            peerToken: (context, db) async {
              signedIn.add(db);
              return 'peer-$db';
            },
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
  return signedIn;
}

void main() {
  testWidgets('shows each inference with its confidence and creates nothing on its own', (tester) async {
    final server = _FakeServer();
    final signedIn = await _pump(tester, server);

    expect(find.textContaining('Import Records from other FileMaker files (3 waiting)'), findsOneWidget);
    expect(find.textContaining('stays disabled until it has a transfer'), findsOneWidget);
    // Another solution is read with a session of it; this one is not.
    expect(signedIn, ['ledger']);
    expect(server.infers.map((b) => b['source_database']), unorderedEquals(['ledger', '']));
    expect(server.infers.firstWhere((b) => b['source_database'] == 'ledger')['peer_token'], 'peer-ledger');

    final pull = find.byKey(const Key('import-step-s-pull'));
    expect(find.descendant(of: pull, matching: find.text('Inferred, high confidence')), findsOneWidget);
    final self = find.byKey(const Key('import-step-s-self'));
    expect(find.descendant(of: self, matching: find.text('Inferred, medium confidence')), findsOneWidget);
    final guess = find.byKey(const Key('import-step-s-guess'));
    expect(find.descendant(of: guess, matching: find.text('Not inferred')), findsOneWidget);
    expect(find.descendant(of: guess, matching: find.textContaining('names do not tell them apart')), findsOneWidget);
    // A step without an inference cannot be created until a table is picked.
    final create = tester.widget<OutlinedButton>(find.byKey(const Key('import-step-create-s-guess')));
    expect(create.onPressed, isNull);

    expect(server.confirms, isEmpty);
  });

  testWidgets('the bulk confirmation covers only the unambiguous inferences, after a dialog', (tester) async {
    final server = _FakeServer();
    await _pump(tester, server);

    await tester.tap(find.byKey(const Key('import-steps-create-inferred')));
    await tester.pumpAndSettle();
    expect(find.text('Create 2 transfers?'), findsOneWidget);
    expect(find.textContaining('Pull ledger, step 2: ledger.entries → orders (high confidence)'), findsOneWidget);
    expect(
      find.textContaining('Notes from customers, step 1: shop.customers → orders (medium confidence)'),
      findsOneWidget,
    );
    expect(find.textContaining('Pull days'), findsOneWidget, reason: 'only on its row, not in the dialog');

    // Cancelling creates nothing.
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(server.confirms, isEmpty);

    await tester.tap(find.byKey(const Key('import-steps-create-inferred')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('import-steps-confirm')));
    await tester.pumpAndSettle();

    expect(server.confirms, hasLength(1));
    final items = server.confirms.single['items'] as List;
    expect(items, [
      {'step_id': 's-pull', 'source_database': 'ledger', 'source_table': 'entries'},
      {'step_id': 's-self', 'source_database': '', 'source_table': 'customers'},
    ]);
    expect(server.confirms.single['peer_tokens'], {'ledger': 'peer-ledger'});
    expect(find.byKey(const Key('import-step-s-pull')), findsNothing);
    expect(find.byKey(const Key('import-step-s-self')), findsNothing);
    expect(find.byKey(const Key('import-step-s-guess')), findsOneWidget);
    expect(find.textContaining('2 transfers created'), findsOneWidget);
    expect(find.byKey(const Key('import-steps-create-inferred')), findsNothing);
  });

  testWidgets('a table picked by hand overrides the inference and leaves the bulk action', (tester) async {
    final server = _FakeServer();
    await _pump(tester, server);

    // Pick a table for the step that had none.
    await tester.tap(find.byKey(const Key('import-step-table-s-guess')));
    await tester.pumpAndSettle();
    await tester.tap(find.textContaining('days — 1/1 types').last);
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('import-step-create-s-guess')));
    await tester.pumpAndSettle();
    expect((server.confirms.single['items'] as List).single, {
      'step_id': 's-guess',
      'source_database': 'ledger',
      'source_table': 'days',
    });
    expect(find.byKey(const Key('import-step-s-guess')), findsNothing);

    // Overriding an inferred step takes it out of the bulk confirmation.
    await tester.tap(find.byKey(const Key('import-step-table-s-pull')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('days').last);
    await tester.pumpAndSettle();
    expect(find.textContaining('the inferred table is overridden'), findsOneWidget);
    await tester.tap(find.byKey(const Key('import-steps-create-inferred')));
    await tester.pumpAndSettle();
    expect(find.text('Create 1 transfer?'), findsOneWidget);
    expect(find.textContaining('Notes from customers, step 1'), findsNWidgets(2));
  });

  testWidgets('choosing another solution asks it again', (tester) async {
    final server = _FakeServer();
    final signedIn = await _pump(tester, server);

    await tester.tap(find.byKey(const Key('import-steps-solution-Ledger')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('archive').last);
    await tester.pumpAndSettle();
    expect(signedIn, ['ledger', 'archive']);
    expect(server.infers.last['source_database'], 'archive');
  });

  testWidgets('shows nothing to a developer who may not list the steps', (tester) async {
    tester.view.physicalSize = const Size(1280, 1100);
    addTearDown(tester.view.resetPhysicalSize);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: MigrationImportStepsPanel(
            apiClient: ApiClient(
              baseUrl: 'http://localhost:8080',
              httpClient: MockClient((req) async => http.Response('{"error":"forbidden"}', 403)),
            ),
            database: 'shop',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('import-steps-panel')), findsNothing);
  });
}
