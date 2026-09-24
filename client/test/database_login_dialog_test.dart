import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/auth/database_login_dialog.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets('DatabaseLoginDialog displays Cargar de disco duro and Cancelar and requires password',
      (WidgetTester tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    final mockClient = MockClient((request) async {
      if (request.url.path.contains('/api/v1/solutions/databases')) {
        return http.Response(
          '{"databases":["file4base_dev","invoices_db"],"active":"file4base_dev"}',
          200,
          headers: {'content-type': 'application/json'},
        );
      }
      return http.Response('{"status":"ok"}', 200);
    });

    final apiClient = ApiClient(baseUrl: 'http://localhost:8080', httpClient: mockClient);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: DatabaseLoginDialog(apiClient: apiClient),
        ),
      ),
    );
    await tester.pumpAndSettle();

    // Verify dialog header
    expect(find.text('File4Base Connect & Login'), findsOneWidget);

    // Verify Cargar de disco duro button exists
    expect(find.text('Cargar de disco duro...'), findsOneWidget);

    // Verify Cancelar button exists
    expect(find.text('Cancelar'), findsAtLeastNWidgets(1));

    // Verify Password field starts empty
    final passwordFieldFinder = find.widgetWithText(TextFormField, 'Password');
    expect(passwordFieldFinder, findsOneWidget);

    // Click Conectar y Entrar without entering password
    await tester.tap(find.text('Conectar y Entrar'));
    await tester.pumpAndSettle();

    // Verify password validation error is shown
    expect(find.text('Password is required'), findsOneWidget);
  });
}
