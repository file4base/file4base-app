import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/solution_manager/new_database_dialog.dart';
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
}
