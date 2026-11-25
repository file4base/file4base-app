// #107 — the client side of external table occurrences: the relationship
// graph's cards and badges, Specify Table's data sources, the relationship
// dialog's fields of another solution, Manage File References, the notice
// after a sign-in, the peer token of script steps, Layout mode on another
// solution's table and value lists taken from one.
//
// A fake server holds two solutions, survey_db (signed in to) and catalog_db
// (read through the file reference "Catalog"), and answers each request as
// the solution its bearer token belongs to.

import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/auth/linked_sign_in_notice.dart';
import 'package:file4base_client/features/data_browser/external_access.dart';
import 'package:file4base_client/features/data_browser/external_layout.dart';
import 'package:file4base_client/features/layout_engine/layout_designer_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/schema_manager/manage_file_references_dialog.dart';
import 'package:file4base_client/features/schema_manager/manage_value_lists_dialog.dart';
import 'package:file4base_client/features/schema_manager/widgets/relationship_graph_widget.dart';
import 'package:file4base_client/features/schema_manager/widgets/specify_relationship_dialog.dart';
import 'package:file4base_client/features/schema_manager/widgets/specify_table_dialog.dart';
import 'package:file4base_client/main.dart' show apiClientProvider;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _peerToken = 'catalog-session';

Map<String, dynamic> _column(String table, String name, {bool key = false}) => {
      'id': '$table-$name',
      'table_id': table,
      'name': name,
      'display_name': name,
      'field_type': 'TEXT',
      'is_primary_key': key,
    };

final Map<String, dynamic> _answersJson = {
  'id': 't-answers',
  'name': 'answers',
  'display_name': 'Answers',
  'columns': [_column('t-answers', 'id', key: true), _column('t-answers', 'item_sku')],
};

/// Items, as catalog_db's catalog has it.
final Map<String, dynamic> _itemsJson = {
  'id': 't-peer-items',
  'name': 'items',
  'display_name': 'Items',
  'columns': [
    _column('t-peer-items', 'id', key: true),
    // Migrated from the FileMaker field with id 7.
    {..._column('t-peer-items', 'sku'), 'source_field_id': '7'},
    _column('t-peer-items', 'label'),
  ],
};

final _answers = TableModel.fromJson(_answersJson);

const _answersOcc = TableOccurrenceModel(id: 'occ-answers', baseTableId: 't-answers', name: 'Answers', xPos: 40, yPos: 40);

TableOccurrenceModel _ext(String id, String name, {String reference = 'Catalog', String database = 'catalog_db', String table = 'items', double x = 420, double y = 40}) =>
    TableOccurrenceModel.fromJson({
      'id': id,
      'name': name,
      'x_pos': x,
      'y_pos': y,
      'external': {'file_reference_id': 'fr-$id', 'file_reference': reference, 'table': table, 'database': database},
    });

final _itemsOcc = _ext('occ-items', 'Items');
final _pricesOcc = _ext('occ-prices', 'Prices', reference: 'Price List', database: '', table: '', y: 360);

class _Server {
  static var _sessions = 0;

  /// Whether this session is signed in to catalog_db.
  bool linked = false;

  /// Whether binding refuses the account signed in to catalog_db, as one
  /// that is neither its owner nor an administrator.
  bool notAdmin = false;

  /// Whether catalog_db no longer accepts the session held there (its
  /// account was disabled, say), until the next sign-in.
  bool peerEnded = false;

  final requests = <String>[];
  final bodies = <String, Map<String, dynamic>>{};
  final List<Map<String, dynamic>> references = [
    {'id': 'fr-1', 'name': 'Catalog', 'database': 'catalog_db', 'paths': ['file:Catalog.fmp12']},
    {'id': 'fr-2', 'name': 'Price List', 'database': '', 'paths': <String>[]},
  ];
  final List<Map<String, dynamic>> valueLists = [];

  ApiClient client() => ApiClient(baseUrl: 'http://f4b.test', httpClient: MockClient(_handle))
      // Each test its own session, so what one signs in to is not another's.
      .forPeerSession('survey-session-${_sessions++}');

  http.Response _json(Object body, [int status = 200]) =>
      http.Response(jsonEncode(body), status, headers: {'content-type': 'application/json'});

  http.Response _problem(int status, String title, String detail) =>
      _json({'title': title, 'status': status, 'detail': detail}, status);

  Map<String, dynamic> _report(String id) => {
        'file_reference_id': id,
        'file_reference': id == 'fr-1' ? 'Catalog' : 'Price List',
        'database': id == 'fr-1' ? 'catalog_db' : '',
        'occurrences': [
          if (id == 'fr-1')
            {'id': 'occ-items', 'name': 'Items', 'table': 'items', 'status': 'bound', 'fields': ['sku']}
          else
            {'id': 'occ-prices', 'name': 'Prices', 'table': '', 'status': 'unbound', 'problems': ['not mapped']},
        ],
      };

  Future<http.Response> _handle(http.Request request) async {
    final path = request.url.path;
    final peer = request.headers['Authorization'] == 'Bearer $_peerToken';
    requests.add('${request.method} $path${peer ? ' (catalog_db)' : ''}');
    if (request.body.isNotEmpty) {
      final decoded = jsonDecode(request.body);
      if (decoded is Map<String, dynamic>) bodies['${request.method} $path'] = decoded;
    }
    if (peer && peerEnded) return _problem(401, 'Unauthorized', 'session ended');
    switch ((request.method, path)) {
      case ('GET', '/api/v1/auth/session'):
        return _json({'capabilities': <String>[]});
      case ('GET', '/api/v1/schemas/tables'):
        return _json(peer ? [_itemsJson] : [_answersJson]);
      case ('GET', '/api/v1/schemas/occurrences'):
        return _json([
          {'id': 'occ-answers', 'base_table_id': 't-answers', 'name': 'Answers'},
          {
            'id': 'occ-items',
            'name': 'Items',
            'external': {'file_reference_id': 'fr-1', 'file_reference': 'Catalog', 'table': 'items', 'database': 'catalog_db'},
          },
        ]);
      case ('GET', '/api/v1/value-lists'):
        return _json(valueLists);
      case ('POST', '/api/v1/value-lists'):
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        final saved = {...body, 'id': 'vl-${valueLists.length + 1}'};
        valueLists.add(saved);
        return _json(saved, 201);
      case ('GET', '/api/v1/occurrences/occ-items/table'):
        return linked
            ? _json(_itemsJson)
            : _json({
                'type': 'https://file4base.org/errors/external-unavailable',
                'title': 'External Table Unavailable',
                'status': 424,
                'detail': 'not signed in to catalog_db',
                'code': 'external_unavailable',
                'reason': 'not_signed_in',
                'reference': 'Catalog',
                'database': 'catalog_db',
              }, 424);
      case ('POST', '/api/v1/auth/link'):
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        if (body['password'] != 'secret') {
          return _problem(401, 'Authentication Failed', 'invalid credentials');
        }
        linked = true;
        peerEnded = false;
        return _json({
          'database': body['database'],
          'references': ['Catalog'],
          'status': 'linked',
          'username': body['username'],
          'token': _peerToken,
        });
      case ('POST', '/api/v1/auth/login'):
        return _json({
          'status': 'ok',
          'database': 'survey_db',
          'token': 'survey-login-${_sessions++}',
          'user': {'id': 'u-1', 'username': 'ann', 'role': 'owner'},
          'linked': [
            {'database': 'catalog_db', 'references': ['Catalog'], 'status': 'refused', 'username': 'ann'},
            {'database': 'ledger_db', 'references': ['Ledger'], 'status': 'linked', 'username': 'ann', 'token': 'ledger-session'},
          ],
        });
      case ('GET', '/api/v1/databases'):
        return _json({
          'databases': ['catalog_db', 'survey_db'],
        });
      case ('GET', '/api/v1/file-references'):
        return _json(references);
      case ('POST', '/api/v1/file-references'):
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        final saved = {...body, 'id': 'fr-${references.length + 1}'};
        references.add(saved);
        return _json(saved, 201);
      case ('DELETE', '/api/v1/file-references/fr-1'):
        return _problem(409, 'File Reference In Use', '1 table occurrence goes through it');
      case ('POST', '/api/v1/file-references/fr-1/bind'):
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        if (body['peer_token'] != _peerToken) return _problem(403, 'Peer Token Rejected', 'not a session of catalog_db');
        if (notAdmin) {
          return _problem(403, 'Forbidden', 'binding needs a peer_token of an owner or an administrator of catalog_db');
        }
        return _json(_report('fr-1'));
      case ('POST', '/api/v1/file-references/fr-2/verify'):
        return _json(_report('fr-2'));
    }
    return _problem(404, 'Not Found', path);
  }
}

void _bigView(WidgetTester tester) {
  tester.view.physicalSize = const Size(1600, 1200);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
}

/// Fills and submits the sign-in dialog of another solution.
Future<void> _signIn(WidgetTester tester) async {
  await tester.pumpAndSettle();
  await tester.enterText(find.widgetWithText(TextFormField, 'Username'), 'ann');
  await tester.enterText(find.widgetWithText(TextFormField, 'Password'), 'secret');
  await tester.tap(find.text('Sign In'));
  await tester.pumpAndSettle();
}

Future<void> _choose(WidgetTester tester, Key dropdown, String item) async {
  await tester.ensureVisible(find.byKey(dropdown));
  await tester.tap(find.byKey(dropdown));
  await tester.pumpAndSettle();
  await tester.tap(find.text(item).last);
  await tester.pumpAndSettle();
}

/// A screen with one button that opens [open] and keeps what it returns.
Widget _opener(Future<Object?> Function(BuildContext) open, void Function(Object?) done) => MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => TextButton(onPressed: () async => done(await open(context)), child: const Text('Open')),
        ),
      ),
    );

String _badge(WidgetTester tester, String occurrence) {
  final badge = find.byKey(ValueKey('external-status-$occurrence'));
  expect(badge, findsOneWidget);
  return tester.widgetList<Text>(find.descendant(of: badge, matching: find.byType(Text))).map((t) => t.data).join();
}

void main() {
  group('relationship graph', () {
    testWidgets('a card says where its table stands and lists its fields as the other solution has them',
        (tester) async {
      _bigView(tester);
      final server = _Server();
      final relationships = [
        RelationshipModel.fromJson({
          'id': 'rel-1',
          'name': 'answers_items',
          'left_occurrence_id': 'occ-answers',
          'left_column_id': 't-answers-item_sku',
          'right_occurrence_id': 'occ-items',
          'right_column_id': '',
          'predicates': [
            {'left_column_id': 't-answers-item_sku', 'right_column_id': '', 'right_column_name': 'sku'},
            {'left_column_id': 't-answers-id', 'right_column_id': '', 'right_column_name': 'retired_code'},
          ],
        }),
      ];
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: RelationshipGraphWidget(
            tables: [_answers],
            occurrences: [_answersOcc, _itemsOcc, _pricesOcc],
            relationships: relationships,
            apiClient: server.client(),
            onSchemaChanged: () {},
          ),
        ),
      ));
      await tester.pumpAndSettle();

      expect(_badge(tester, 'occ-prices'), 'Unbound');
      expect(_badge(tester, 'occ-items'), 'Not signed in');
      // Not read yet: the fields its relationships match on.
      expect(find.byKey(const ValueKey('external-field-occ-items-sku')), findsOneWidget);
      expect(find.byKey(const ValueKey('external-field-occ-items-label')), findsNothing);

      await tester.tap(find.byKey(const ValueKey('external-status-occ-items')));
      // The card also takes a double click: the tap is the badge's once
      // that one can no longer come.
      await tester.pump(const Duration(milliseconds: 500));
      await _signIn(tester);

      expect(_badge(tester, 'occ-items'), 'OK');
      expect(find.byKey(const ValueKey('external-field-occ-items-sku')), findsOneWidget);
      expect(find.byKey(const ValueKey('external-field-occ-items-label')), findsOneWidget);
      // A field a criterion names that the other solution no longer has.
      expect(find.byKey(const ValueKey('external-field-occ-items-retired_code')), findsOneWidget);
    });

    test('the state of an external occurrence, and what its badge says', () {
      expect(externalOccurrenceState(_pricesOcc), ExternalOccurrenceState.unbound);
      expect(externalOccurrenceState(_itemsOcc), ExternalOccurrenceState.reading);
      expect(externalOccurrenceState(_itemsOcc, table: TableModel.fromJson(_itemsJson)), ExternalOccurrenceState.ok);
      ApiException unavailable(String reason) => ApiException(
            statusCode: 424,
            title: 'External Table Unavailable',
            detail: 'x',
            raw: {'code': 'external_unavailable', 'reason': reason, 'reference': 'Catalog', 'database': 'catalog_db'},
          );
      expect(externalOccurrenceState(_itemsOcc, error: unavailable('missing')), ExternalOccurrenceState.missing);
      expect(externalOccurrenceState(_itemsOcc, error: unavailable('schema_mismatch')),
          ExternalOccurrenceState.schemaMismatch);
      expect(externalStateLabel(ExternalOccurrenceState.schemaMismatch), 'Mismatch');
      expect(externalStateMessage(_pricesOcc, ExternalOccurrenceState.unbound),
          contains('"Price List" is not mapped to a solution. Map and bind it in File > Manage > File References'));
    });
  });

  group('Specify Table', () {
    testWidgets('a file reference lists the tables of its solution once signed in to it', (tester) async {
      _bigView(tester);
      final server = _Server();
      final client = server.client();
      Object? result;
      await tester.pumpWidget(_opener(
        (context) => SpecifyTableDialog.show(context,
            tables: [_answers], existingOccurrences: const [_answersOcc], activeDatabaseName: 'survey_db', apiClient: client),
        (r) => result = r,
      ));
      await tester.tap(find.text('Open'));
      await tester.pumpAndSettle();

      // Not mapped: says where to map it.
      await _choose(tester, const ValueKey('specify-table-data-source'), 'Price List (not mapped)');
      expect(find.textContaining('"Price List" is not mapped to a solution yet'), findsOneWidget);

      await _choose(tester, const ValueKey('specify-table-data-source'), 'Catalog (catalog_db)');
      expect(find.textContaining('Not signed in to catalog_db (through "Catalog")'), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('specify-table-sign-in')));
      await _signIn(tester);

      expect(server.requests, contains('GET /api/v1/schemas/tables (catalog_db)'));
      expect(find.text('Items'), findsWidgets);
      expect(find.text('Answers'), findsNothing, reason: 'this solution\'s tables are not offered for a reference');
      await tester.tap(find.text('OK'));
      await tester.pumpAndSettle();
      expect(result, isA<Map<String, dynamic>>());
      final map = result! as Map<String, dynamic>;
      expect(map['external'], {'file_reference': 'Catalog', 'table': 'items'});
      expect(map['name'], 'Items');
    });

    testWidgets('Add File Reference… adds one and selects it', (tester) async {
      _bigView(tester);
      final server = _Server();
      await tester.pumpWidget(_opener(
        (context) => SpecifyTableDialog.show(context,
            tables: [_answers], existingOccurrences: const [], apiClient: server.client()),
        (_) {},
      ));
      await tester.tap(find.text('Open'));
      await tester.pumpAndSettle();
      await _choose(tester, const ValueKey('specify-table-data-source'), 'Add File Reference…');
      expect(find.text('New File Reference'), findsOneWidget);
      await tester.enterText(find.byKey(const ValueKey('file-reference-name')), 'Archive');
      await tester.tap(find.text('OK').last);
      await tester.pumpAndSettle();
      expect(server.bodies['POST /api/v1/file-references']?['name'], 'Archive');
      expect(find.textContaining('"Archive" is not mapped to a solution yet'), findsOneWidget);
    });
  });

  group('Specify Relationship', () {
    testWidgets('the fields of an external side are read in its solution and named', (tester) async {
      _bigView(tester);
      final server = _Server();
      final client = server.client();
      Object? result;
      await tester.pumpWidget(_opener(
        (context) => SpecifyRelationshipDialog.show(context,
            occurrences: [_answersOcc, _itemsOcc],
            tables: [_answers],
            preselectedLeftOccurrenceId: 'occ-answers',
            preselectedRightOccurrenceId: 'occ-items',
            apiClient: client),
        (r) => result = r,
      ));
      await tester.tap(find.text('Open'));
      await tester.pumpAndSettle();

      expect(find.byKey(const ValueKey('relationship-external-sign-in')), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('relationship-external-sign-in')));
      await _signIn(tester);
      expect(find.text('label'), findsOneWidget, reason: 'the fields of items in catalog_db');

      await tester.tap(find.text('item_sku').first);
      await tester.tap(find.text('sku').first);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Add'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('OK'));
      await tester.pumpAndSettle();

      final predicates = (result! as Map<String, dynamic>)['predicates'] as List<RelationshipPredicateModel>;
      expect(
        predicates.any((p) => p.leftColumnId == 't-answers-item_sku' && p.rightColumnId.isEmpty && p.rightColumnName == 'sku'),
        isTrue,
        reason: 'a field of another solution is named, never given an id of this one',
      );
    });
  });

  group('fields known only by their FileMaker id', () {
    RelationshipModel relationship(String rightName) => RelationshipModel.fromJson({
          'id': 'rel-1',
          'name': 'answers_items',
          'left_occurrence_id': 'occ-answers',
          'left_column_id': 't-answers-item_sku',
          'right_occurrence_id': 'occ-items',
          'right_column_id': '',
          'predicates': [
            {'left_column_id': 't-answers-item_sku', 'right_column_id': '', 'right_column_name': rightName},
          ],
        });

    Future<List<RelationshipPredicateModel>> changeAndSave(WidgetTester tester, String rightName, {String? shown}) async {
      _bigView(tester);
      final server = _Server();
      final client = server.client();
      await client.linkSolution(database: 'catalog_db', username: 'ann', password: 'secret');
      Object? result;
      await tester.pumpWidget(_opener(
        (context) => SpecifyRelationshipDialog.show(context,
            occurrences: [_answersOcc, _itemsOcc],
            tables: [_answers],
            initialRelationship: relationship(rightName),
            apiClient: client),
        (r) => result = r,
      ));
      await tester.tap(find.text('Open'));
      await tester.pumpAndSettle();
      if (shown != null) expect(find.text(shown), findsWidgets);
      // Change something else, then save.
      final option = find.byKey(const ValueKey('relationship-left-cascade-delete'));
      await tester.ensureVisible(option);
      await tester.tap(option);
      await tester.pumpAndSettle();
      await tester.tap(find.text('OK').last);
      await tester.pumpAndSettle();
      final map = result! as Map<String, dynamic>;
      expect(map['left_cascade_delete'], isTrue);
      return map['predicates'] as List<RelationshipPredicateModel>;
    }

    testWidgets('a marker the other solution resolves shows that field, and saving keeps the marker', (tester) async {
      final predicates = await changeAndSave(tester, '#7', shown: 'Items::sku');
      expect(predicates, hasLength(1));
      expect(predicates.single.rightColumnName, '#7');
      expect(predicates.single.rightColumnId, isEmpty);
    });

    testWidgets('a marker nothing resolves is shown unresolved and saved untouched, never as the first field',
        (tester) async {
      _bigView(tester);
      final server = _Server();
      final client = server.client();
      await client.linkSolution(database: 'catalog_db', username: 'ann', password: 'secret');
      await tester.pumpWidget(_opener(
        (context) => SpecifyRelationshipDialog.show(context,
            occurrences: [_answersOcc, _itemsOcc], tables: [_answers], initialRelationship: relationship('#9'), apiClient: client),
        (_) {},
      ));
      await tester.tap(find.text('Open'));
      await tester.pumpAndSettle();
      expect(find.textContaining('field #9 (unresolved)'), findsWidgets);
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();

      final predicates = await changeAndSave(tester, '#9');
      expect(predicates.single.rightColumnName, '#9');
    });

    test('a marker is the one field migrated with its id; none or several is unresolved', () {
      final columns = TableModel.fromJson(_itemsJson).columns;
      expect(ColumnModel.externalField(columns, '#7')?.name, 'sku');
      expect(ColumnModel.externalField(columns, '#9'), isNull);
      expect(ColumnModel.externalField([...columns, columns[1].copyWith(name: 'sku_copy')], '#7'), isNull);
      expect(ColumnModel.externalField(columns, 'label')?.name, 'label');
      expect(ColumnModel.unresolvedLabel('#9'), 'field #9 (unresolved)');
    });
  });

  group('linked sessions', () {
    test('are kept by the exact name of their solution', () async {
      final server = _Server();
      final client = server.client();
      await client.linkSolution(database: 'Sales', username: 'ann', password: 'secret');
      expect(client.linkedToken('Sales'), _peerToken);
      expect(client.linkedToken('sales'), isNull, reason: 'on PostgreSQL Sales and sales are two solutions');
      client.forgetLinked('sales');
      expect(client.linkedToken('Sales'), _peerToken);
    });

    testWidgets('a token the other solution no longer accepts is dropped and the user asked to sign in again',
        (tester) async {
      _bigView(tester);
      final server = _Server();
      final client = server.client();
      await client.linkSolution(database: 'catalog_db', username: 'ann', password: 'secret');
      String? peer;
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => TextButton(
              onPressed: () async => peer = await ExternalAccess.of(client).peerToken(context, 'catalog_db'),
              child: const Text('Perform Script'),
            ),
          ),
        ),
      ));

      // Accepted: handed out with no dialog.
      await tester.tap(find.text('Perform Script'));
      await tester.pumpAndSettle();
      expect(peer, _peerToken);
      expect(find.byType(AlertDialog), findsNothing);

      // Refused now: not handed to the step, the user is asked again.
      server.peerEnded = true;
      peer = null;
      await tester.tap(find.text('Perform Script'));
      await tester.pumpAndSettle();
      expect(find.text('Sign In'), findsOneWidget);
      expect(client.linkedToken('catalog_db'), isNull);
      await _signIn(tester);
      expect(peer, _peerToken);
      expect(server.peerEnded, isFalse);
    });
  });

  group('Manage File References', () {
    Future<_Server> open(WidgetTester tester, {bool notAdmin = false}) async {
      _bigView(tester);
      final server = _Server()..notAdmin = notAdmin;
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(body: ManageFileReferencesDialog(apiClient: server.client(), solutionName: 'survey_db')),
      ));
      await tester.pumpAndSettle();
      return server;
    }

    testWidgets('lists the references; Bind signs in to the solution and shows what it bound', (tester) async {
      final server = await open(tester);
      expect(find.byKey(const ValueKey('file-reference-fr-1')), findsOneWidget);
      expect(find.byKey(const ValueKey('file-reference-fr-2')), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('file-reference-bind')));
      await _signIn(tester);

      expect(server.bodies['POST /api/v1/file-references/fr-1/bind']?['database'], 'catalog_db');
      expect(find.byKey(const ValueKey('file-reference-occurrence-Items')), findsOneWidget);
      expect(find.byKey(const ValueKey('file-reference-problem')), findsNothing);
    });

    testWidgets('an account that may not read the catalog is told to sign in as another one', (tester) async {
      await open(tester, notAdmin: true);
      await tester.tap(find.byKey(const ValueKey('file-reference-bind')));
      await _signIn(tester);
      final problem = find.byKey(const ValueKey('file-reference-problem'));
      expect(problem, findsOneWidget);
      expect(find.descendant(of: problem, matching: find.textContaining('owner or administrator account there')),
          findsOneWidget);
      expect(find.text('Sign in to catalog_db as another account…'), findsOneWidget);
    });

    testWidgets('a reference in use is not deleted, and says why', (tester) async {
      await open(tester);
      await tester.tap(find.byKey(const ValueKey('file-reference-delete')));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'Delete'));
      await tester.pumpAndSettle();
      expect(find.textContaining('"Catalog" is still in use: 1 table occurrence goes through it'), findsOneWidget);
      expect(find.byKey(const ValueKey('file-reference-fr-1')), findsOneWidget);
    });

    testWidgets('Verify on an unmapped reference lists what goes through it, unbound', (tester) async {
      await open(tester);
      await tester.tap(find.byKey(const ValueKey('file-reference-fr-2')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('file-reference-verify')));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('file-reference-occurrence-Prices')), findsOneWidget);
      expect(find.byType(AlertDialog), findsNothing, reason: 'nothing to sign in to');
    });

    testWidgets('the editor suggests the solution a FileMaker path names, never choosing it', (tester) async {
      final server = await open(tester);
      await tester.tap(find.byKey(const ValueKey('file-reference-new')));
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(const ValueKey('file-reference-name')), 'Catalog Copy');
      await tester.enterText(find.byKey(const ValueKey('file-reference-paths')), 'file:catalog_db.fmp12');
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('file-reference-suggestion')), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('file-reference-suggestion')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('OK'));
      await tester.pumpAndSettle();
      final body = server.bodies['POST /api/v1/file-references']!;
      expect(body['name'], 'Catalog Copy');
      expect(body['database'], 'catalog_db');
      expect(find.byKey(const ValueKey('file-reference-fr-3')), findsOneWidget);
    });

    test('errors name the reference, the solution and the remedy', () {
      const ref = FileReferenceModel(id: 'fr-1', name: 'Catalog', database: 'catalog_db');
      final unreachable = describeFileReferenceError(
          ApiException(statusCode: 503, title: 'Database Unavailable', detail: 'x'), ref);
      expect(unreachable.message, contains('The solution catalog_db of "Catalog" is not reachable'));
      expect(unreachable.remedy, FileReferenceRemedy.edit);
      final ended = describeFileReferenceError(
          ApiException(statusCode: 403, title: 'Peer Token Rejected', detail: 'x'), ref);
      expect(ended.remedy, FileReferenceRemedy.signIn);
    });
  });

  group('after a sign-in', () {
    testWidgets('the solutions the account could not be signed in to are listed, each with Sign in…',
        (tester) async {
      _bigView(tester);
      final server = _Server();
      final client = ApiClient(baseUrl: 'http://f4b.test', httpClient: MockClient(server._handle));
      await client.login(username: 'ann', password: 'pw', database: 'survey_db');

      expect(client.signInLinks, hasLength(2));
      expect(linkedNeedingSignIn(client.signInLinks).map((l) => l.database), ['catalog_db']);
      expect(client.linkedToken('ledger_db'), 'ledger-session', reason: 'kept for script steps and reads');
      expect(client.linkedToken('catalog_db'), isNull);

      String? peer;
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => Column(children: [
              TextButton(
                onPressed: () => showLinkedSignInNotice(context, client, solutionName: 'survey_db'),
                child: const Text('Signed in'),
              ),
              TextButton(
                onPressed: () async => peer = await ExternalAccess.of(client).peerToken(context, 'ledger_db'),
                child: const Text('Perform Script'),
              ),
            ]),
          ),
        ),
      ));
      await tester.tap(find.text('Perform Script'));
      await tester.pumpAndSettle();
      expect(peer, 'ledger-session', reason: 'a linked solution needs no dialog');
      expect(find.byType(AlertDialog), findsNothing);

      await tester.tap(find.text('Signed in'));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('linked-sign-in-notice')), findsOneWidget);
      expect(find.byKey(const ValueKey('linked-sign-in-catalog_db')), findsOneWidget);
      expect(find.byKey(const ValueKey('linked-sign-in-ledger_db')), findsNothing);

      await tester.tap(find.text('Sign in…'));
      await _signIn(tester);
      expect(find.byKey(const ValueKey('linked-sign-in-notice')), findsNothing);
      expect(client.linkedToken('catalog_db'), _peerToken);
    });
  });

  group('Layout mode', () {
    testWidgets('a layout on another solution\'s table is designed with that table\'s fields', (tester) async {
      _bigView(tester);
      final items = TableModel.fromJson(_itemsJson);
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: LayoutDesignerWidget(
            table: items,
            tables: [items],
            apiClient: ApiClient(baseUrl: 'http://localhost:1'),
            externalOccurrence: _itemsOcc,
            initialLayout: const LayoutDefinitionModel(
              id: 'l-items',
              name: 'Item List',
              tableOccurrence: 'Items',
              parts: [LayoutPartModel(id: 'body', type: 'body', height: 300)],
              objects: [],
            ),
            layouts: [LayoutModel(id: 'l-items', name: 'Item List', tableOccurrenceId: 'occ-items', definition: const {})],
            onSaved: () {},
            onExitLayout: () {},
          ),
        ),
      ));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('external-fields-source')), findsOneWidget);
      expect(find.text('"Items": fields of items in catalog_db'), findsOneWidget);
      expect(find.text('label'), findsWidgets);
    });

    testWidgets('until its fields can be read, the placeholder says it can be designed then', (tester) async {
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(body: ExternalLayoutPlaceholder(layoutName: 'Price Sheet', occurrence: _pricesOcc, designing: true)),
      ));
      expect(find.textContaining('It can be designed once the fields of that table can be read.'), findsOneWidget);
    });
  });

  group('value lists', () {
    testWidgets('a list from another solution\'s table names its fields, second field and related values',
        (tester) async {
      _bigView(tester);
      final server = _Server();
      final client = server.client();
      await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: const MaterialApp(home: Scaffold(body: ManageValueListsDialog())),
      ));
      await tester.pumpAndSettle();

      await tester.tap(find.text('New Value List...'));
      await tester.pumpAndSettle();
      await tester.enterText(find.widgetWithText(TextField, 'Value List Name'), 'Item labels');
      await tester.tap(find.text('From a field'));
      await tester.pumpAndSettle();

      await _choose(tester, const ValueKey('value-list-source-table'), 'Items (catalog_db)');
      expect(find.byKey(const ValueKey('value-list-external-problem')), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('value-list-external-sign-in')));
      await _signIn(tester);
      expect(find.byKey(const ValueKey('value-list-external-problem')), findsNothing);

      await _choose(tester, const ValueKey('value-list-source-column-occ:occ-items'), 'sku');
      await tester.ensureVisible(find.byKey(const ValueKey('value-list-use-second')));
      await tester.tap(find.byKey(const ValueKey('value-list-use-second')));
      await tester.pumpAndSettle();
      await _choose(tester, const ValueKey('value-list-second-column-occ:occ-items'), 'label');
      await tester.ensureVisible(find.byKey(const ValueKey('value-list-related')));
      await tester.tap(find.byKey(const ValueKey('value-list-related')));
      await tester.pumpAndSettle();
      await _choose(tester, const ValueKey('value-list-related-occurrence'), 'Answers');

      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      final body = server.bodies['POST /api/v1/value-lists']!;
      expect(body['source_occurrence_id'], 'occ-items');
      expect(body['source_column_name'], 'sku');
      expect(body['second_column_name'], 'label');
      expect(body['related_occurrence_id'], 'occ-answers');
      expect(body['source_table_id'], isNull);
      expect(body['source_column_id'], isNull);
    });
  });
}
