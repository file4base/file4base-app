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
List<Map<String, dynamic>> _steps({bool extra = false}) => [
  if (extra) ...[
    {
      'step_id': 's-low',
      'script_id': 'sc-4',
      'script_name': 'Pull counts',
      'step_index': 1,
      'source_file': 'Ledger',
      'target_table': 'orders',
      'method': 'update',
      'mappings': [
        {'position': 1, 'target_field': 'code', 'match': true},
        {'position': 2, 'target_field': 'qty'},
      ],
      'suggested_database': 'ledger',
      'suggested_by': 'database_name',
    },
    {
      'step_id': 's-name',
      'script_id': 'sc-5',
      'script_name': 'Notes by name',
      'step_index': 1,
      'source_file': 'Ledger',
      'target_table': 'orders',
      'method': 'add',
      'match_by_name': true,
      'mappings': [
        {'position': 0, 'target_field': 'code'},
        {'position': 0, 'target_field': 'note'},
      ],
      'suggested_database': 'ledger',
      'suggested_by': 'database_name',
    },
  ],
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
    'enabled_in_filemaker': true,
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
    'enabled_in_filemaker': false,
  },
];

class _FakeServer {
  _FakeServer({this.hidden = 0, this.rejectInfers = 0, this.failLists = 0, this.extra = false});

  /// Whether a low-confidence step and one matching names wait too.
  final bool extra;

  /// Tables of Ledger the signed-in account may not export from.
  final int hidden;

  /// How many infers against Ledger refuse the session first.
  int rejectInfers;

  /// How many listings of the steps fail first.
  int failLists;

  final infers = <Map<String, dynamic>>[];
  final confirms = <Map<String, dynamic>>[];

  static http.Response _json(Object body) =>
      http.Response(jsonEncode(body), 200, headers: {'content-type': 'application/json'});

  MockClient get client => MockClient((req) async {
    final path = req.url.path;
    if (path == '/api/v1/transfers/import-steps' && req.method == 'GET') {
      if (failLists > 0) {
        failLists--;
        return http.Response('{"title":"Internal Server Error","detail":"the catalog is busy"}', 500);
      }
      final done = {
        for (final c in confirms)
          for (final i in c['items'] as List) (i as Map)['step_id'],
      };
      return _json({'steps': _steps(extra: extra).where((s) => !done.contains(s['step_id'])).toList()});
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
        if (rejectInfers > 0) {
          rejectInfers--;
          return http.Response('{"title":"Peer Token Rejected","detail":"the session of ledger has ended"}', 403);
        }
        return _json({
          'source_database': 'ledger',
          'tables': ['days', 'entries'],
          'steps': [
            {
              'step_id': 's-pull',
              'table': 'entries',
              'confidence': 'high',
              'reason': 'the only table whose fields fit every position by type; names agree',
              if (hidden > 0) 'tables_hidden': hidden,
              'candidates': [
                {
                  'table': 'entries',
                  'fields': 3,
                  'types_matched': 2,
                  'names_matched': 2,
                  'positions': 2,
                  'fits': true,
                  'pairs': [
                    {'position': 1, 'source_field': 'code', 'target_field': 'code', 'match': true},
                    {'position': 2, 'source_field': 'amount', 'target_field': 'total'},
                  ],
                },
                {
                  'table': 'days',
                  'fields': 3,
                  'types_matched': 1,
                  'positions': 2,
                  'deleted_positions': 1,
                  'pairs': [
                    {'position': 1, 'source_field': 'label', 'target_field': 'code', 'match': true},
                    {'position': 2, 'target_field': 'total', 'deleted': true},
                  ],
                },
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
            if (extra) ...[
              {
                'step_id': 's-low',
                'table': 'entries',
                'confidence': 'low',
                'reason':
                    "no table has a field of the target field's type at every position; this is the only one "
                    'FileMaker could import from, and 1 value(s) go into a field of another type',
                'candidates': [
                  {
                    'table': 'entries',
                    'fields': 3,
                    'types_matched': 1,
                    'positions': 2,
                    'possible': true,
                    'pairs': [
                      {'position': 1, 'source_field': 'code', 'target_field': 'code', 'match': true},
                      {
                        'position': 2,
                        'source_field': 'count',
                        'target_field': 'qty',
                        'source_type': 'TEXT',
                        'target_type': 'NUMBER',
                      },
                    ],
                  },
                ],
              },
              {
                'step_id': 's-name',
                'table': 'entries',
                'confidence': 'high',
                'by_name': true,
                'reason':
                    'the step pairs its fields by matching names, and this is the only table with a field '
                    'named as each of its 2 target field(s), of the same type',
                'candidates': [
                  {
                    'table': 'entries',
                    'fields': 3,
                    'types_matched': 2,
                    'names_matched': 2,
                    'positions': 2,
                    'fits': true,
                    'possible': true,
                    'pairs': [
                      {'position': 0, 'source_field': 'CODE', 'target_field': 'code'},
                      {'position': 0, 'source_field': 'NOTE', 'target_field': 'note'},
                    ],
                  },
                ],
              },
            ],
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
              {
                'table': 'customers',
                'fields': 2,
                'types_matched': 1,
                'positions': 1,
                'fits': true,
                'pairs': [
                  {'position': 1, 'source_field': 'name', 'target_field': 'note'},
                ],
              },
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
    // The pairing of the picked table is shown on the row.
    expect(
      find.descendant(
        of: pull,
        matching: find.text('Source position → target field: 1 code → code (match), 2 amount → total'),
      ),
      findsOneWidget,
    );
    expect(find.descendant(of: self, matching: find.textContaining('Destructive')), findsOneWidget);
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
    // High and medium confidence apart, each step with its method, its
    // pairing, whether it is enabled, and replace marked destructive.
    final dialog = find.byType(AlertDialog);
    final highTitle = tester.getTopLeft(find.textContaining('High confidence:')).dy;
    final mediumTitle = tester.getTopLeft(find.textContaining('Medium confidence:')).dy;
    final pullItem = find.byKey(const Key('import-steps-confirm-s-pull'));
    final selfItem = find.byKey(const Key('import-steps-confirm-s-self'));
    expect(highTitle < tester.getTopLeft(pullItem).dy, isTrue);
    expect(tester.getTopLeft(pullItem).dy < mediumTitle, isTrue);
    expect(mediumTitle < tester.getTopLeft(selfItem).dy, isTrue);
    expect(
      find.descendant(of: pullItem, matching: find.text('• Pull ledger, step 2: ledger.entries → orders')),
      findsOneWidget,
    );
    expect(
      find.descendant(
        of: pullItem,
        matching: find.text(
          'update matching records on code, adding the others; 1 code → code (match), 2 amount → total',
        ),
      ),
      findsOneWidget,
    );
    expect(find.descendant(of: pullItem, matching: find.textContaining('is enabled')), findsOneWidget);
    expect(find.descendant(of: pullItem, matching: find.textContaining('Destructive')), findsNothing);
    expect(
      find.descendant(of: selfItem, matching: find.text('• Notes from customers, step 1: shop.customers → orders')),
      findsOneWidget,
    );
    expect(find.descendant(of: selfItem, matching: find.textContaining('1 name → note')), findsOneWidget);
    expect(find.descendant(of: selfItem, matching: find.textContaining('Destructive')), findsOneWidget);
    expect(find.descendant(of: selfItem, matching: find.textContaining('stays disabled')), findsOneWidget);
    expect(find.descendant(of: dialog, matching: find.textContaining('Pull days')), findsNothing);

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
    await tester.tap(find.textContaining('days — 1/2 types').last);
    await tester.pumpAndSettle();
    expect(find.textContaining('the inferred table is overridden'), findsOneWidget);
    await tester.tap(find.byKey(const Key('import-steps-create-inferred')));
    await tester.pumpAndSettle();
    expect(find.text('Create 1 transfer?'), findsOneWidget);
    expect(find.textContaining('Notes from customers, step 1'), findsNWidgets(2));
  });

  testWidgets('a low-confidence inference names its converted pairs and is confirmed only on its own', (tester) async {
    final server = _FakeServer(extra: true);
    await _pump(tester, server);

    final low = find.byKey(const Key('import-step-s-low'));
    expect(
      find.descendant(of: low, matching: find.text('Inferred, low confidence: confirm on its own')),
      findsOneWidget,
    );
    expect(
      find.descendant(
        of: low,
        matching: find.text('Source position → target field: 1 code → code (match), 2 count (TEXT) → qty (NUMBER)'),
      ),
      findsOneWidget,
    );
    expect(find.byKey(const Key('import-step-converted-s-low')), findsOneWidget);
    expect(find.byKey(const Key('import-step-converted-s-pull')), findsNothing);

    // Matching names: no position, and the run pairs the names again.
    final byName = find.byKey(const Key('import-step-s-name'));
    expect(
      find.descendant(
        of: byName,
        matching: find.text('Source field (by name) → target field: CODE → code, NOTE → note'),
      ),
      findsOneWidget,
    );
    expect(find.descendant(of: byName, matching: find.textContaining('paired by matching names')), findsOneWidget);
    expect(find.descendant(of: byName, matching: find.textContaining('each time it runs')), findsOneWidget);

    // The bulk confirmation leaves the low one out.
    await tester.tap(find.byKey(const Key('import-steps-create-inferred')));
    await tester.pumpAndSettle();
    expect(find.text('Create 3 transfers?'), findsOneWidget);
    expect(find.byKey(const Key('import-steps-confirm-s-low')), findsNothing);
    expect(find.byKey(const Key('import-steps-confirm-s-name')), findsOneWidget);
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();

    // On its own, it is created as confirmed.
    await tester.tap(find.byKey(const Key('import-step-create-s-low')));
    await tester.pumpAndSettle();
    expect((server.confirms.single['items'] as List).single, {
      'step_id': 's-low',
      'source_database': 'ledger',
      'source_table': 'entries',
    });
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

  testWidgets('a picked table shows its pairing, a deleted field included', (tester) async {
    final server = _FakeServer();
    await _pump(tester, server);
    await tester.tap(find.byKey(const Key('import-step-table-s-pull')));
    await tester.pumpAndSettle();
    await tester.tap(find.textContaining('days — 1/2 types').last);
    await tester.pumpAndSettle();
    expect(
      find.text('Source position → target field: 1 label → code (match), 2 (deleted field) → total'),
      findsOneWidget,
    );
  });

  testWidgets('an inference over a list the account could not fully see is confirmed alone', (tester) async {
    final server = _FakeServer(hidden: 1);
    await _pump(tester, server);
    final pull = find.byKey(const Key('import-step-s-pull'));
    expect(
      find.descendant(of: pull, matching: find.textContaining('1 table(s) of ledger were not weighed')),
      findsOneWidget,
    );
    await tester.tap(find.byKey(const Key('import-steps-create-inferred')));
    await tester.pumpAndSettle();
    expect(find.text('Create 1 transfer?'), findsOneWidget);
    expect(find.byKey(const Key('import-steps-confirm-s-pull')), findsNothing);
    expect(find.byKey(const Key('import-steps-confirm-s-self')), findsOneWidget);
  });

  testWidgets('a session the other solution refused is asked for again on Retry', (tester) async {
    final server = _FakeServer(rejectInfers: 1);
    final signedIn = await _pump(tester, server);
    expect(find.text('the session of ledger has ended'), findsOneWidget);
    expect(find.byKey(const Key('import-step-s-pull')), findsOneWidget);
    await tester.tap(find.text('Retry'));
    await tester.pumpAndSettle();
    expect(signedIn, ['ledger', 'ledger'], reason: 'the refused token was not sent again');
    expect(find.text('Inferred, high confidence'), findsOneWidget);
  });

  testWidgets('a listing that fails is said, and Retry lists again', (tester) async {
    final server = _FakeServer(failLists: 1);
    await _pump(tester, server);
    expect(find.byKey(const Key('import-steps-panel')), findsNothing);
    expect(find.textContaining('the catalog is busy'), findsOneWidget);
    expect(find.textContaining('stay disabled'), findsOneWidget);
    await tester.tap(find.byKey(const Key('import-steps-retry')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('import-steps-panel')), findsOneWidget);
    expect(find.byKey(const Key('import-steps-load-error')), findsNothing);
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
