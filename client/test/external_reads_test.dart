// #107 — records of another solution's table, read through an external
// occurrence. These render the real Browse mode against a fake server and
// look at what came out, and at what the client asked for.

import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/auth/database_login_dialog.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/data_browser/external_access.dart';
import 'package:file4base_client/features/data_browser/external_layout.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

Map<String, dynamic> _column(String table, String name, {bool key = false}) => {
      'id': '$table-$name',
      'table_id': table,
      'name': name,
      'display_name': name,
      'field_type': 'TEXT',
      'is_primary_key': key,
    };

final Map<String, dynamic> _orders = {
  'id': 't-orders',
  'name': 'orders',
  'display_name': 'Orders',
  'columns': [_column('t-orders', 'id', key: true), _column('t-orders', 'customer_code')],
};

/// Customers, as the other solution's catalog has it.
final Map<String, dynamic> _customers = {
  'id': 't-peer-customers',
  'name': 'customers',
  'display_name': 'Customers',
  'columns': [_column('t-peer-customers', 'id', key: true), _column('t-peer-customers', 'full_name')],
};

final List<Map<String, dynamic>> _occurrences = [
  {'id': 'occ-orders', 'base_table_id': 't-orders', 'name': 'Orders'},
  {
    'id': 'occ-cust',
    'name': 'Customers',
    'external': {'file_reference_id': 'fr-1', 'file_reference': 'Peer', 'table': 'customers', 'database': 'peer_db'},
  },
];

final _relationships = [
  {
    'id': 'rel-x',
    'name': 'orders_customers',
    'left_occurrence_id': 'occ-orders',
    'left_column_id': 't-orders-customer_code',
    'right_occurrence_id': 'occ-cust',
  },
];

Map<String, dynamic> _unavailable(String reason) => {
      'type': 'https://file4base.org/errors/external-unavailable',
      'title': 'External Table Unavailable',
      'status': 424,
      'detail': 'cannot read peer_db',
      'code': 'external_unavailable',
      'reason': reason,
      'reference': 'Peer',
      'database': 'peer_db',
    };

/// A server with Orders, whose orders relate to the customers of peer_db.
class _Server {
  /// Why peer_db cannot be read, or null once it can.
  String? unavailable;

  /// Why the records of the external layout cannot be read while its table
  /// can (a table read before, and kept), or null once they can.
  String? recordsUnavailable;

  /// Whether Orders has a value list taken from a field of peer_db.
  bool externalValueList = false;

  /// What the account in peer_db may do with its customers, as the table of
  /// the occurrence says; null says nothing.
  Map<String, bool>? privileges = const {'read': true, 'create': false, 'edit': false, 'delete': false};

  /// Why the next writes into peer_db are refused, or null once they are not.
  String? writeUnavailable;

  /// The writes into peer_db, as method, path and body.
  final writes = <String>[];
  final List<Map<String, dynamic>> orders = [
    for (var i = 1; i <= 3; i++) {'id': 'o-$i', 'customer_code': 'C-$i'},
  ];
  final requests = <String>[];
  final links = <Map<String, dynamic>>[];

  static var _sessions = 0;

  ApiClient client() => ApiClient(baseUrl: 'http://f4b.test', httpClient: MockClient(_handle))
      // Each test its own session, so what one signs in to is not another's.
      .forPeerSession('session-${_sessions++}');

  http.Response _json(Object body, [int status = 200]) =>
      http.Response(jsonEncode(body), status, headers: {'content-type': 'application/json'});

  Future<http.Response> _handle(http.Request request) async {
    final path = request.url.path;
    requests.add('${request.method} $path');
    final external = unavailable;
    switch ((request.method, path)) {
      case ('GET', '/api/v1/schemas/tables'):
        return _json([_orders]);
      case ('GET', '/api/v1/schemas/occurrences'):
        return _json(_occurrences);
      case ('GET', '/api/v1/schemas/relationships'):
        return _json(_relationships);
      case ('GET', '/api/v1/value-lists'):
        return _json([
          if (externalValueList)
            {'id': 'vl-cust', 'name': 'Customer names', 'kind': 'field', 'source_occurrence_id': 'occ-cust'},
        ]);
      case ('GET', '/api/v1/value-lists/vl-cust/values'):
        return external != null
            ? _json(_unavailable(external), 424)
            : _json({
                'values': ['Ada Lovelace', 'Grace Hopper'],
              });
      case ('GET', '/api/v1/data/orders'):
        return _json(orders);
      case ('GET', '/api/v1/occurrences/occ-cust/table'):
        return external != null
            ? _json(_unavailable(external), 424)
            : _json({..._customers, 'privileges': ?privileges});
      case ('GET', '/api/v1/occurrences/occ-cust/records'):
        final reason = external ?? recordsUnavailable;
        return reason != null
            ? _json(_unavailable(reason), 424)
            : _json([
                {'id': 'c-1', 'full_name': 'Ada Lovelace'},
              ]);
      case ('POST', '/api/v1/data/orders/related'):
        if (external != null) return _json(_unavailable(external), 424);
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        return _json({
          'records': {
            for (final id in body['record_ids'] as List<dynamic>)
              id: [
                {'id': 'c-$id', 'full_name': 'Customer of $id'},
              ],
          },
        });
      case ('POST', '/api/v1/auth/link'):
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        links.add(body);
        if (body['password'] != 'secret') {
          return _json({'title': 'Authentication Failed', 'status': 401, 'detail': 'invalid credentials'}, 401);
        }
        unavailable = null;
        recordsUnavailable = null;
        writeUnavailable = null;
        return _json({'database': 'peer_db', 'status': 'linked', 'username': body['username']});
    }
    final related = RegExp(r'^/api/v1/data/orders/([^/]+)/related$').firstMatch(path);
    final customerRecords = path.startsWith('/api/v1/occurrences/occ-cust/records');
    if (request.method != 'GET' && (customerRecords || related != null)) {
      final refused = writeUnavailable;
      if (refused != null) return _json(_unavailable(refused), 424);
      writes.add('${request.method} ${request.url} ${request.body}');
      if (request.method == 'DELETE') return http.Response('', 204);
      final body = jsonDecode(request.body) as Map<String, dynamic>;
      return _json({'id': 'c-new', ...body}, request.method == 'POST' ? 201 : 200);
    }
    if (related != null && request.method == 'GET') {
      if (external != null) return _json(_unavailable(external), 424);
      return _json([
        {'id': 'c-1', 'full_name': 'Ada Lovelace'},
        {'id': 'c-2', 'full_name': 'Grace Hopper'},
      ]);
    }
    return _json({'title': 'Not Found', 'status': 404, 'detail': path}, 404);
  }
}

final _orderTable = TableModel.fromJson(_orders);

LayoutObjectModel _object(String id, String type, double y, {FieldBindingModel? field, PortalConfigModel? portal}) =>
    LayoutObjectModel(
      id: id,
      type: type,
      x: 40,
      y: y,
      width: 360,
      height: type == 'portal' ? 150 : 28,
      fieldBinding: field,
      portalConfig: portal?.toJson(),
    );

const _customerName = FieldBindingModel(fieldName: 'full_name', relationshipId: 'rel-x', tableOccurrence: 'Customers');

/// An Orders layout with a customer's name, and a portal of its customers
/// when [portal].
LayoutDefinitionModel _ordersLayout({bool portal = false, bool portalDeletion = false, String defaultView = 'form'}) =>
    LayoutDefinitionModel(
      id: 'l-orders',
      name: 'Orders',
      tableOccurrence: 'Orders',
      defaultView: defaultView,
      parts: const [LayoutPartModel(id: 'b', type: 'body', height: 400)],
      objects: [
        _object('code', 'field', 10, field: const FieldBindingModel(fieldName: 'customer_code')),
        if (!portal) _object('name', 'field', 50, field: _customerName),
        if (portal) ...[
          _object('portal', 'portal', 100,
              portal: PortalConfigModel(
                  relationshipId: 'rel-x', occurrence: 'Customers', allowCreation: true, allowDeletion: portalDeletion)),
          _object('in-portal', 'field', 105, field: const FieldBindingModel(fieldName: 'full_name')),
        ],
      ],
    );

Future<void> _pump(WidgetTester tester, Widget body) async {
  tester.view.physicalSize = const Size(1400, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  await tester.pumpWidget(MaterialApp(home: Scaffold(body: body)));
  await tester.pumpAndSettle();
}

/// The record command button labelled [label].
ButtonStyleButton _button(WidgetTester tester, String label) => tester.widget<ButtonStyleButton>(find
    .ancestor(of: find.text(label), matching: find.byWidgetPredicate((w) => w is ButtonStyleButton))
    .first);

Widget _browser(ApiClient api, LayoutDefinitionModel layout) => DataBrowserWidget(
      table: _orderTable,
      apiClient: api,
      mode: OperationalMode.browse,
      layout: layout,
    );

void main() {
  testWidgets("a portal of another solution's table read-only there shows its records, and writes nothing",
      (tester) async {
    final server = _Server();
    await _pump(tester, _browser(server.client(), _ordersLayout(portal: true)));

    expect(find.text('Ada Lovelace'), findsOneWidget);
    expect(find.text('Grace Hopper'), findsOneWidget);
    expect(server.requests, contains('GET /api/v1/occurrences/occ-cust/table'),
        reason: "the fields are the other solution's");
    expect(find.byKey(const ValueKey('portal-cell-c-1-full_name')), findsNothing,
        reason: 'no cell takes entry: the account there may only read');
    expect(find.byKey(const ValueKey('portal-new-row')), findsNothing, reason: 'nor create');
    expect(find.byKey(const ValueKey('portal-delete-c-1')), findsNothing, reason: 'nor delete');
  });

  testWidgets("a portal of another solution's table edits, creates and deletes its records there", (tester) async {
    final server = _Server()..privileges = const {'read': true, 'create': true, 'edit': true, 'delete': true};
    await _pump(tester, _browser(server.client(), _ordersLayout(portal: true, portalDeletion: true)));

    await tester.enterText(find.byKey(const ValueKey('portal-cell-c-1-full_name')), 'Ada King');
    await tester.pump(const Duration(milliseconds: 700));
    await tester.pumpAndSettle();
    expect(server.writes, ['PUT http://f4b.test/api/v1/occurrences/occ-cust/records/c-1 {"full_name":"Ada King"}'],
        reason: 'written through the occurrence, as the account there');

    server.writes.clear();
    await tester.enterText(find.byKey(const ValueKey('portal-new-cell')), 'Mary');
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle();
    expect(server.writes, [
      'POST http://f4b.test/api/v1/data/orders/o-1/related?relationship=rel-x&occurrence=occ-cust {"full_name":"Mary"}',
    ], reason: 'created through the relationship, the server setting the keys');

    server.writes.clear();
    await tester.tap(find.byKey(const ValueKey('portal-delete-c-1')));
    await tester.pumpAndSettle();
    expect(server.writes, ['DELETE http://f4b.test/api/v1/occurrences/occ-cust/records/c-1 ']);
  });

  testWidgets("a portal of another solution's table edits only what the account there may", (tester) async {
    final server = _Server()..privileges = const {'read': true, 'create': false, 'edit': true, 'delete': false};
    await _pump(tester, _browser(server.client(), _ordersLayout(portal: true, portalDeletion: true)));

    expect(find.byKey(const ValueKey('portal-cell-c-1-full_name')), findsOneWidget);
    expect(find.byKey(const ValueKey('portal-new-row')), findsNothing);
    expect(find.byKey(const ValueKey('portal-delete-c-1')), findsNothing);
  });

  testWidgets('a write into a solution not signed in to signs in, then writes once more', (tester) async {
    final server = _Server()
      ..privileges = const {'read': true, 'create': true, 'edit': true, 'delete': true}
      ..writeUnavailable = ExternalUnavailable.notSignedIn;
    await _pump(tester, _browser(server.client(), _ordersLayout(portal: true)));

    await tester.enterText(find.byKey(const ValueKey('portal-cell-c-1-full_name')), 'Ada King');
    await tester.pump(const Duration(milliseconds: 700));
    // The cell shows it is saving while the dialog is open.
    await tester.pump(const Duration(milliseconds: 500));
    expect(find.text('Open peer_db (used by this solution)'), findsOneWidget, reason: 'the link sign-in');
    await tester.enterText(find.widgetWithText(TextFormField, 'Username'), 'alice');
    await tester.enterText(find.widgetWithText(TextFormField, 'Password'), 'secret');
    await tester.tap(find.text('Sign In'));
    await tester.pumpAndSettle();

    expect(server.writes, ['PUT http://f4b.test/api/v1/occurrences/occ-cust/records/c-1 {"full_name":"Ada King"}']);
  });

  testWidgets('a related field of another solution shows its first related record', (tester) async {
    final server = _Server();
    await _pump(tester, _browser(server.client(), _ordersLayout()));
    expect(find.text('Ada Lovelace'), findsOneWidget);
  });

  testWidgets('not signed in: the login dialog opens for that solution, and the field reads once signed in',
      (tester) async {
    final server = _Server()..unavailable = ExternalUnavailable.notSignedIn;
    final api = server.client();
    ExternalAccess.of(api).solutionName = 'Orders';
    await _pump(tester, _browser(api, _ordersLayout()));

    // Asked once, by itself, with the existing dialog.
    expect(find.text('Open peer_db (used by Orders)'), findsOneWidget);
    await tester.enterText(find.widgetWithText(TextFormField, 'Username'), 'alice');
    await tester.enterText(find.widgetWithText(TextFormField, 'Password'), 'wrong');
    await tester.tap(find.text('Sign In'));
    await tester.pumpAndSettle();
    expect(find.text('Open peer_db (used by Orders)'), findsOneWidget, reason: 'a refused password keeps it open');

    await tester.enterText(find.widgetWithText(TextFormField, 'Password'), 'secret');
    await tester.tap(find.text('Sign In'));
    await tester.pumpAndSettle();

    expect(find.text('Open peer_db (used by Orders)'), findsNothing);
    expect(server.links.last, {'database': 'peer_db', 'username': 'alice', 'password': 'secret'});
    expect(find.text('Ada Lovelace'), findsOneWidget, reason: 'read again once signed in');
  });

  testWidgets('not signed in, dialog dismissed: the field offers to sign in', (tester) async {
    final server = _Server()..unavailable = ExternalUnavailable.notSignedIn;
    await _pump(tester, _browser(server.client(), _ordersLayout()));
    await tester.tap(find.text('Cancel').last);
    await tester.pumpAndSettle();

    expect(find.text('<Not Signed In>'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('external-sign-in')));
    await tester.pumpAndSettle();
    expect(find.textContaining('Open peer_db'), findsOneWidget);
  });

  testWidgets('no access: the portal names the solution and the remedy, and asks for nothing', (tester) async {
    final server = _Server()..unavailable = ExternalUnavailable.noAccess;
    await _pump(tester, _browser(server.client(), _ordersLayout(portal: true)));

    expect(find.textContaining('Open peer_db'), findsNothing);
    expect(find.textContaining('Your account in "peer_db" (through "Peer") may not read these records'),
        findsOneWidget);
    expect(find.byKey(const ValueKey('external-sign-in')), findsNothing);
  });

  testWidgets("a list reads every record's related field in one request", (tester) async {
    final server = _Server();
    await _pump(tester, _browser(server.client(), _ordersLayout(defaultView: 'list')));
    server.requests.clear(); // the form showed the first record's
    await tester.tap(find.text('List'));
    await tester.pumpAndSettle();

    for (final order in server.orders) {
      expect(find.text('Customer of ${order['id']}'), findsOneWidget);
    }
    expect(server.requests.where((r) => r == 'POST /api/v1/data/orders/related'), hasLength(1));
    expect(server.requests.where((r) => r.endsWith('/related') && r.startsWith('GET')), isEmpty,
        reason: 'no request per record');
  });

  testWidgets("a layout on another solution's table read-only there browses its records, and writes nothing",
      (tester) async {
    final server = _Server();
    final api = server.client();
    final occurrence = TableOccurrenceModel.fromJson(_occurrences[1]);
    final layout = LayoutDefinitionModel(
      id: 'l-customers',
      name: 'Customers',
      tableOccurrence: 'Customers',
      objects: [_object('name', 'field', 10, field: const FieldBindingModel(fieldName: 'full_name', tableOccurrence: 'Customers'))],
    );
    await _pump(
      tester,
      ExternalLayoutView(
        layoutName: 'Customers',
        occurrence: occurrence,
        apiClient: api,
        builder: (context, table, client) => DataBrowserWidget(
          table: table,
          apiClient: client,
          mode: OperationalMode.browse,
          layout: layout,
          externalDatabase: 'peer_db',
          privileges: table.privileges,
        ),
      ),
    );

    expect(server.requests, contains('GET /api/v1/occurrences/occ-cust/records'),
        reason: "the occurrence's records, not a local table's");
    expect(server.requests.where((r) => r.startsWith('GET /api/v1/data/customers')), isEmpty);
    final field = find.byWidgetPredicate((w) => w is TextField && w.controller?.text == 'Ada Lovelace');
    expect(field, findsOneWidget);
    expect(tester.widget<TextField>(field).readOnly, isTrue, reason: 'shown, not typed into');
    expect(_button(tester, 'New').onPressed, isNull);
  });

  testWidgets("a layout on another solution's table edits and creates its records there", (tester) async {
    final server = _Server()..privileges = const {'read': true, 'create': true, 'edit': true, 'delete': false};
    final layout = LayoutDefinitionModel(
      id: 'l-customers',
      name: 'Customers',
      tableOccurrence: 'Customers',
      objects: [_object('name', 'field', 10, field: const FieldBindingModel(fieldName: 'full_name', tableOccurrence: 'Customers'))],
    );
    TableModel? told;
    await _pump(
      tester,
      ExternalLayoutView(
        layoutName: 'Customers',
        occurrence: TableOccurrenceModel.fromJson(_occurrences[1]),
        apiClient: server.client(),
        onTable: (table) => told = table,
        builder: (context, table, client) => DataBrowserWidget(
          table: table,
          apiClient: client,
          mode: OperationalMode.browse,
          layout: layout,
          externalDatabase: 'peer_db',
          privileges: table.privileges,
        ),
      ),
    );
    expect(told?.privileges?.edit, isTrue, reason: 'what the menu commands follow');

    final field = find.byWidgetPredicate((w) => w is TextField && w.controller?.text == 'Ada Lovelace');
    expect(tester.widget<TextField>(field).readOnly, isFalse);
    await tester.enterText(field, 'Ada King');
    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();
    expect(server.writes, ['PUT http://f4b.test/api/v1/occurrences/occ-cust/records/c-1 {"full_name":"Ada King"}']);

    expect(_button(tester, 'New').onPressed, isNotNull);
    expect(_button(tester, 'Delete').onPressed, isNull,
        reason: 'the account there may not delete');
  });

  testWidgets("a layout on another solution's table says why it cannot read it", (tester) async {
    final server = _Server()..unavailable = ExternalUnavailable.notSignedIn;
    final api = server.client();
    await _pump(
      tester,
      ExternalLayoutView(
        layoutName: 'Customers',
        occurrence: TableOccurrenceModel.fromJson(_occurrences[1]),
        apiClient: api,
        builder: (context, table, client) => const Text('browser'),
      ),
    );
    await tester.tap(find.text('Cancel').last);
    await tester.pumpAndSettle();

    expect(find.text('browser'), findsNothing);
    expect(find.textContaining('Not signed in to "peer_db" (through "Peer")'), findsOneWidget);
    expect(find.textContaining('record commands are disabled'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('external-layout-sign-in')));
    await tester.pumpAndSettle();
    await tester.enterText(find.widgetWithText(TextFormField, 'Username'), 'alice');
    await tester.enterText(find.widgetWithText(TextFormField, 'Password'), 'secret');
    await tester.tap(find.text('Sign In'));
    await tester.pumpAndSettle();
    expect(find.text('browser'), findsOneWidget);
  });

  testWidgets('the sign-in dialog for another solution asks only for its account', (tester) async {
    final server = _Server();
    await _pump(
      tester,
      Builder(
        builder: (context) => TextButton(
          onPressed: () => DatabaseLoginDialog.showLink(context, server.client(), database: 'peer_db', usedBy: 'Orders'),
          child: const Text('open'),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    expect(find.text('Open peer_db (used by Orders)'), findsOneWidget);
    expect(find.text('Load from disk...'), findsNothing);
    expect(find.widgetWithText(TextFormField, 'API Server URL'), findsNothing);
    expect(server.requests.where((r) => r.contains('/solutions/databases')), isEmpty,
        reason: 'the solution is the one named, not chosen');
  });
  testWidgets("a list row's portal shows that row's related records, all rows in one request", (tester) async {
    final server = _Server();
    await _pump(tester, _browser(server.client(), _ordersLayout(portal: true, defaultView: 'list')));
    server.requests.clear(); // the form showed the first record's
    await tester.tap(find.text('List'));
    await tester.pumpAndSettle();

    for (final order in server.orders) {
      expect(find.text('Customer of ${order['id']}'), findsOneWidget, reason: 'the row of ${order['id']}');
    }
    expect(find.text('Ada Lovelace'), findsNothing, reason: "not the selected record's portal on every row");
    expect(server.requests.where((r) => r == 'POST /api/v1/data/orders/related'), hasLength(1));
    expect(server.requests.where((r) => r.endsWith('/related') && r.startsWith('GET')), isEmpty,
        reason: 'no request per row');
  });

  testWidgets('a value list of another solution not signed in to offers to sign in, then its values',
      (tester) async {
    final server = _Server()
      ..unavailable = ExternalUnavailable.notSignedIn
      ..externalValueList = true;
    final layout = LayoutDefinitionModel(
      id: 'l-orders',
      name: 'Orders',
      tableOccurrence: 'Orders',
      parts: const [LayoutPartModel(id: 'b', type: 'body', height: 400)],
      objects: [
        _object('code', 'field', 10,
            field: const FieldBindingModel(
                fieldName: 'customer_code', controlStyle: FieldControlStyle.popUpMenu, valueListId: 'vl-cust')),
      ],
    );
    await _pump(tester, _browser(server.client(), layout));
    expect(find.textContaining('Open peer_db'), findsOneWidget, reason: 'asked once, by itself');
    await tester.tap(find.text('Cancel').last);
    await tester.pumpAndSettle();

    final signIn = find.byKey(const ValueKey('value-list-sign-in'));
    expect(signIn, findsOneWidget, reason: 'not a silent edit box');
    expect(tester.widget<IconButton>(signIn).tooltip, 'Sign in to peer_db…');
    await tester.tap(signIn);
    await tester.pumpAndSettle();
    await tester.enterText(find.widgetWithText(TextFormField, 'Username'), 'alice');
    await tester.enterText(find.widgetWithText(TextFormField, 'Password'), 'secret');
    await tester.tap(find.text('Sign In'));
    await tester.pumpAndSettle();

    expect(signIn, findsNothing);
    expect(server.requests.where((r) => r == 'GET /api/v1/value-lists/vl-cust/values'), hasLength(2),
        reason: 'read again once signed in');
  });

  testWidgets("a kept external layout whose records need a sign-in signs in, then reads them once more",
      (tester) async {
    final server = _Server()..recordsUnavailable = ExternalUnavailable.notSignedIn;
    final layout = LayoutDefinitionModel(
      id: 'l-customers',
      name: 'Customers',
      tableOccurrence: 'Customers',
      objects: [_object('name', 'field', 10, field: const FieldBindingModel(fieldName: 'full_name', tableOccurrence: 'Customers'))],
    );
    await _pump(
      tester,
      ExternalLayoutView(
        layoutName: 'Customers',
        occurrence: TableOccurrenceModel.fromJson(_occurrences[1]),
        apiClient: server.client(),
        builder: (context, table, client) => DataBrowserWidget(
          table: table,
          apiClient: client,
          mode: OperationalMode.browse,
          layout: layout,
          externalDatabase: 'peer_db',
        ),
      ),
    );

    expect(find.textContaining('Open peer_db'), findsOneWidget, reason: 'the link sign-in, not only an error');
    await tester.enterText(find.widgetWithText(TextFormField, 'Username'), 'alice');
    await tester.enterText(find.widgetWithText(TextFormField, 'Password'), 'secret');
    await tester.tap(find.text('Sign In'));
    await tester.pumpAndSettle();

    expect(server.requests.where((r) => r == 'GET /api/v1/occurrences/occ-cust/records'), hasLength(2));
    expect(find.byWidgetPredicate((w) => w is TextField && w.controller?.text == 'Ada Lovelace'), findsOneWidget);
  });

  testWidgets('a kept external layout whose sign-in is dismissed says why, and reads once', (tester) async {
    final server = _Server()..recordsUnavailable = ExternalUnavailable.notSignedIn;
    await _pump(
      tester,
      ExternalLayoutView(
        layoutName: 'Customers',
        occurrence: TableOccurrenceModel.fromJson(_occurrences[1]),
        apiClient: server.client(),
        builder: (context, table, client) => DataBrowserWidget(
          table: table,
          apiClient: client,
          mode: OperationalMode.browse,
          layout: LayoutDefinitionModel(id: 'l-customers', name: 'Customers', tableOccurrence: 'Customers'),
          externalDatabase: 'peer_db',
        ),
      ),
    );
    await tester.tap(find.text('Cancel').last);
    await tester.pumpAndSettle();

    expect(server.requests.where((r) => r == 'GET /api/v1/occurrences/occ-cust/records'), hasLength(1));
    expect(find.textContaining('Not signed in to "peer_db"'), findsWidgets);
  });
}
