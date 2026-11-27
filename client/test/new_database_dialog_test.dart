import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/solution_manager/new_database_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test('owner password policy rejects blank, short and guessable passwords', () {
    String? check(String p) => ownerPasswordProblem(p, user: 'admin', database: 'my_solution_db');
    expect(check(''), isNotNull);
    expect(check('   '), isNotNull);
    expect(check('admin'), isNotNull);
    expect(check('abc12'), isNotNull);
    expect(check('MY_SOLUTION_DB'), isNotNull);
    expect(ownerPasswordProblem('adminadmin', user: 'adminadmin', database: 'x'), isNotNull);
    expect(check('correct horse'), isNull);
  });

  testWidgets('an untouched New Database dialog cannot create a database with a predictable password (#9)',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    final created = <http.Request>[];
    final apiClient = ApiClient(
      baseUrl: 'http://localhost:8080',
      httpClient: MockClient((request) async {
        if (request.method == 'POST') created.add(request);
        return http.Response('{}', 200, headers: {'content-type': 'application/json'});
      }),
    );

    await tester.pumpWidget(MaterialApp(home: Scaffold(body: NewDatabaseDialog(apiClient: apiClient))));
    await tester.pumpAndSettle();

    final password = tester.widget<TextFormField>(find.widgetWithText(TextFormField, 'Password'));
    expect(password.controller!.text, isEmpty, reason: 'no default password');

    await tester.tap(find.text('Create & Save Solution'));
    await tester.pumpAndSettle();
    expect(find.text('Choose a password for the owner account'), findsOneWidget);
    expect(created, isEmpty, reason: 'nothing may be sent to the server');

    await tester.enterText(find.widgetWithText(TextFormField, 'Password'), 'admin');
    await tester.tap(find.text('Create & Save Solution'));
    await tester.pumpAndSettle();
    expect(find.text('Use at least $kMinOwnerPasswordLength characters'), findsOneWidget);
    expect(created, isEmpty);
  });

  testWidgets('a database reserved for the administrators asks for the setup code (#236)', (tester) async {
    tester.view.physicalSize = const Size(1440, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    final bodies = <Map<String, dynamic>>[];
    final apiClient = ApiClient(
      baseUrl: 'http://localhost:8080',
      httpClient: MockClient((request) async {
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        bodies.add(body);
        final detail = body.containsKey('setup_code')
            ? 'the setup code is not valid; it is printed in the server log at startup'
            : "database 'my_solution_db' is reserved: its owners become server administrators, "
                'so only a super-owner can create it, or enter the setup code printed in the server log at startup';
        return http.Response(
            jsonEncode({'title': 'Reserved Database', 'status': 403, 'detail': detail, 'setup_code_required': true}), 403,
            headers: {'content-type': 'application/problem+json'});
      }),
    );

    await tester.pumpWidget(MaterialApp(home: Scaffold(body: NewDatabaseDialog(apiClient: apiClient))));
    await tester.pumpAndSettle();
    final setupCode = find.byKey(const Key('new-database-setup-code'));
    expect(setupCode, findsNothing, reason: 'not asked for an ordinary database');

    await tester.enterText(find.widgetWithText(TextFormField, 'Password'), 'correct horse battery');
    await tester.tap(find.text('Create & Save Solution'));
    await tester.pumpAndSettle();
    expect(bodies.single.containsKey('setup_code'), isFalse);
    expect(find.textContaining('or enter the setup code printed in the server log'), findsOneWidget);
    expect(setupCode, findsOneWidget);

    await tester.tap(find.text('Create & Save Solution'));
    await tester.pumpAndSettle();
    expect(find.text('Enter the setup code'), findsOneWidget);
    expect(bodies, hasLength(1), reason: 'no request without the code');

    await tester.enterText(setupCode, 'abcd-efgh-ijkl-mnop');
    await tester.tap(find.text('Create & Save Solution'));
    await tester.pumpAndSettle();
    expect(bodies.last['setup_code'], 'abcd-efgh-ijkl-mnop');
    expect(find.text('The setup code is not valid; it is printed in the server log at startup.'), findsOneWidget);
  });
}
