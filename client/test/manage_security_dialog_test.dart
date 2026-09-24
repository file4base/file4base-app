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
      expect(find.text('Gestionar Seguridad'), findsOneWidget);
      expect(find.text('solucion_contable'), findsOneWidget);

      // Check Tabs
      expect(find.text('Cuentas'), findsOneWidget);
      expect(find.text('Privilegios de presentaciones'), findsOneWidget);
      expect(find.text('Privilegios ampliados'), findsOneWidget);

      // Check Table headers
      expect(find.text('#'), findsOneWidget);
      expect(find.text('ESTADO'), findsOneWidget);
      expect(find.text('NOMBRE DE CUENTA'), findsOneWidget);
      expect(find.text('CONJUNTO DE PRIVILEGIOS'), findsOneWidget);
      expect(find.text('OPCIONES'), findsOneWidget);

      // Check Users Listed
      expect(find.text('admin_root'), findsOneWidget);
      expect(find.text('operator_bob'), findsOneWidget);
      expect(find.text('Tú / Activa'), findsOneWidget); // Current user badge

      // Check Sequential numbers
      expect(find.text('1'), findsOneWidget);
      expect(find.text('2'), findsWidgets);

      // Check Status labels
      expect(find.text('Activa'), findsOneWidget);
      expect(find.text('Inactiva'), findsOneWidget);

      // Check Role Badges
      expect(find.text('[Acceso total] Owner'), findsOneWidget);
      expect(find.text('[Acceso restringido] User'), findsOneWidget);

      // Check New Account button
      expect(find.text('Nueva cuenta...'), findsOneWidget);
    });
  });
}
