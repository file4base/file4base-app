import 'dart:convert';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/security/manage_security_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  group('UserModel tests', () {
    test('serializes and deserializes isActive properly', () {
      final user = UserModel(
        id: 'user-1',
        username: 'john_doe',
        role: 'admin',
        isActive: false,
      );

      final json = user.toJson();
      expect(json['id'], 'user-1');
      expect(json['username'], 'john_doe');
      expect(json['role'], 'admin');
      expect(json['is_active'], false);

      final decoded = UserModel.fromJson(json);
      expect(decoded.id, 'user-1');
      expect(decoded.username, 'john_doe');
      expect(decoded.role, 'admin');
      expect(decoded.isActive, false);
    });

    test('defaults isActive to true when omitted', () {
      final user = UserModel.fromJson({
        'id': 'user-2',
        'username': 'jane_doe',
        'role': 'owner',
      });
      expect(user.isActive, true);
    });

    test('copyWith updates isActive', () {
      final user = UserModel(id: '1', username: 'test', role: 'user', isActive: true);
      final updated = user.copyWith(isActive: false);
      expect(updated.isActive, false);
      expect(updated.username, 'test');
    });
  });

  group('ManageSecurityDialog Widget Tests', () {
    testWidgets('renders security dialog with table, sequence numbers, and active toggles', (tester) async {
      tester.view.physicalSize = const Size(1280, 800);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      final mockClient = MockClient((request) async {
        if (request.url.path == '/api/v1/security/users') {
          return http.Response(
            jsonEncode([
              {
                'id': 'owner-1',
                'username': 'admin_root',
                'role': 'owner',
                'is_active': true,
              },
              {
                'id': 'user-2',
                'username': 'operator_bob',
                'role': 'user',
                'is_active': false,
              },
            ]),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        if (request.url.path == '/api/v1/schemas/layouts') {
          return http.Response(
            jsonEncode([
              {'id': 'lay-1', 'name': 'Facturación', 'table_occurrence_id': 'to-1'},
              {'id': 'lay-2', 'name': 'Clientes', 'table_occurrence_id': 'to-2'},
            ]),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        if (request.url.path.contains('/permissions')) {
          return http.Response('[]', 200, headers: {'content-type': 'application/json'});
        }
        return http.Response('Not found', 404);
      });

      final apiClient = ApiClient(baseUrl: 'http://test-server:8080', httpClient: mockClient);
      const currentUser = UserModel(id: 'owner-1', username: 'admin_root', role: 'owner', isActive: true);

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ManageSecurityDialog(
              apiClient: apiClient,
              currentUser: currentUser,
              databaseName: 'solucion_contable',
            ),
          ),
        ),
      );

      await tester.pumpAndSettle();

      // Check Window Title and Database Badge
      expect(find.text('Manage Security'), findsOneWidget);
      expect(find.text('solucion_contable'), findsOneWidget);

      // Check Tabs
      expect(find.text('Accounts'), findsOneWidget);
      expect(find.text('Layout privileges'), findsOneWidget);
      expect(find.text('Extended privileges'), findsOneWidget);

      // Check Table headers
      expect(find.text('#'), findsOneWidget);
      expect(find.text('STATUS'), findsOneWidget);
      expect(find.text('ACCOUNT NAME'), findsOneWidget);
      expect(find.text('PRIVILEGE SET'), findsOneWidget);
      expect(find.text('OPTIONS'), findsOneWidget);

      // Check Users Listed
      expect(find.text('admin_root'), findsOneWidget);
      expect(find.text('operator_bob'), findsOneWidget);
      expect(find.text('You / Active'), findsOneWidget); // Current user badge

      // Check Sequential numbers
      expect(find.text('1'), findsOneWidget);
      expect(find.text('2'), findsWidgets);

      // Each status appears in both the filter and its matching account row.
      expect(find.text('Active'), findsNWidgets(2));
      expect(find.text('Inactive'), findsNWidgets(2));

      // Check Role Badges
      expect(find.text('[Full Access] Owner'), findsOneWidget);
      expect(find.text('[Restricted Access] User'), findsOneWidget);

      // Check New Account button
      expect(find.text('New account...'), findsOneWidget);
    });

    testWidgets('scopes API requests to databaseName and calls onModified on changes', (tester) async {
      tester.view.physicalSize = const Size(1280, 800);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      String? requestedDbParam;
      String? requestedHeaderDb;
      bool onModifiedCalled = false;

      final mockClient = MockClient((request) async {
        if (request.url.path == '/api/v1/security/users') {
          requestedDbParam = request.url.queryParameters['database'];
          requestedHeaderDb = request.headers['X-Database-Name'];
          return http.Response(
            jsonEncode([
              {
                'id': 'owner-1',
                'username': 'admin_root',
                'role': 'owner',
                'is_active': true,
              },
              {
                'id': 'user-2',
                'username': 'operator_bob',
                'role': 'user',
                'is_active': true,
              },
            ]),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        if (request.url.path.contains('/api/v1/security/users/user-2')) {
          return http.Response('{"status":"updated"}', 200, headers: {'content-type': 'application/json'});
        }
        if (request.url.path == '/api/v1/schemas/layouts') {
          return http.Response('[]', 200, headers: {'content-type': 'application/json'});
        }
        if (request.url.path.contains('/permissions')) {
          return http.Response('[]', 200, headers: {'content-type': 'application/json'});
        }
        return http.Response('Not found', 404);
      });

      final apiClient = ApiClient(baseUrl: 'http://test-server:8080', httpClient: mockClient);
      const currentUser = UserModel(id: 'owner-1', username: 'admin_root', role: 'owner', isActive: true);

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ManageSecurityDialog(
              apiClient: apiClient,
              currentUser: currentUser,
              databaseName: 'isolated_payroll_db',
              onModified: () {
                onModifiedCalled = true;
              },
            ),
          ),
        ),
      );

      await tester.pumpAndSettle();

      // Verify that databaseName was transmitted to the API
      expect(requestedDbParam, 'isolated_payroll_db');
      expect(requestedHeaderDb, 'isolated_payroll_db');

      // Tap toggle on user-2 to deactivate
      final switchFinder = find.byType(Switch);
      expect(switchFinder, findsNWidgets(2));
      await tester.tap(switchFinder.at(1));
      await tester.pumpAndSettle();

      // Verify onModified was called
      expect(onModifiedCalled, isTrue);
    });
  });

  group('Extended privileges tab (#39)', () {
    /// Serves the dialog's reads and records what it writes.
    MockClient privilegeClient({
      required List<Map<String, dynamic>> privileges,
      required List<Map<String, dynamic>> written,
    }) {
      return MockClient((request) async {
        if (request.url.path == '/api/v1/security/privileges') {
          if (request.method == 'PUT') {
            final body = jsonDecode(request.body) as Map<String, dynamic>;
            written.addAll((body['privileges'] as List<dynamic>).cast<Map<String, dynamic>>());
            for (final p in written) {
              privileges.removeWhere((existing) => existing['role'] == p['role']);
              privileges.add(p);
            }
          }
          return http.Response(
            jsonEncode({
              'capabilities': ['bulk_export', 'bulk_import'],
              'privileges': privileges,
            }),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        if (request.url.path == '/api/v1/security/users') {
          return http.Response(
            jsonEncode([
              {'id': 'owner-1', 'username': 'admin_root', 'role': 'owner', 'is_active': true},
            ]),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        if (request.url.path == '/api/v1/schemas/layouts' || request.url.path.contains('/permissions')) {
          return http.Response('[]', 200, headers: {'content-type': 'application/json'});
        }
        return http.Response('Not found', 404);
      });
    }

    Future<void> openTab(WidgetTester tester, MockClient client, {String role = 'owner'}) async {
      tester.view.physicalSize = const Size(1280, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      final apiClient = ApiClient(baseUrl: 'http://test-server:8080', httpClient: client);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ManageSecurityDialog(
              apiClient: apiClient,
              currentUser: UserModel(id: 'owner-1', username: 'admin_root', role: role, isActive: true),
              databaseName: 'bakery',
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('Extended privileges'));
      await tester.pumpAndSettle();
    }

    testWidgets('shows the capabilities that are enforced and no access method', (tester) async {
      final written = <Map<String, dynamic>>[];
      await openTab(
        tester,
        privilegeClient(
          privileges: [
            {'role': 'owner', 'bulk_export': true, 'bulk_import': true},
            {'role': 'admin', 'bulk_export': true, 'bulk_import': false},
            {'role': 'user', 'bulk_export': false, 'bulk_import': false},
          ],
          written: written,
        ),
      );

      expect(find.text('Bulk record export'), findsOneWidget);
      expect(find.text('Bulk record import'), findsOneWidget);

      // The access methods the tab used to claim are gone: they were never
      // enforced and could not be (#39).
      expect(find.textContaining('WebDirect'), findsNothing);
      expect(find.textContaining('REST API'), findsNothing);
      expect(find.textContaining('does not restrict access by client'), findsOneWidget);

      // Two capabilities by three roles, each switch reflecting what is stored.
      final switches = tester.widgetList<Switch>(find.byType(Switch)).toList();
      expect(switches.length, 6);
      expect(switches.map((s) => s.value).toList(), [true, true, false, true, false, false]);

      // The owner's switches are locked on.
      expect(switches[0].onChanged, isNull);
      expect(switches[3].onChanged, isNull);
      expect(switches[1].onChanged, isNotNull);
    });

    testWidgets('a change is stored at once, not kept in the dialog', (tester) async {
      final written = <Map<String, dynamic>>[];
      await openTab(
        tester,
        privilegeClient(
          privileges: [
            {'role': 'owner', 'bulk_export': true, 'bulk_import': true},
            {'role': 'admin', 'bulk_export': true, 'bulk_import': true},
            {'role': 'user', 'bulk_export': true, 'bulk_import': true},
          ],
          written: written,
        ),
      );

      // Withhold bulk export from the user role.
      await tester.tap(find.byType(Switch).at(2));
      await tester.pumpAndSettle();

      expect(written, [
        {'role': 'user', 'bulk_export': false, 'bulk_import': true},
      ]);
      expect(tester.widget<Switch>(find.byType(Switch).at(2)).value, isFalse);
      expect(find.textContaining('withheld from users'), findsOneWidget);
    });

    testWidgets('an account that is not an owner can only read the grants', (tester) async {
      final written = <Map<String, dynamic>>[];
      await openTab(
        tester,
        privilegeClient(
          privileges: [
            {'role': 'owner', 'bulk_export': true, 'bulk_import': true},
            {'role': 'admin', 'bulk_export': true, 'bulk_import': true},
            {'role': 'user', 'bulk_export': false, 'bulk_import': true},
          ],
          written: written,
        ),
        role: 'admin',
      );

      expect(find.text('Only an owner can change these privileges.'), findsOneWidget);
      for (final s in tester.widgetList<Switch>(find.byType(Switch))) {
        expect(s.onChanged, isNull);
      }
      expect(written, isEmpty);
    });
  });
}
