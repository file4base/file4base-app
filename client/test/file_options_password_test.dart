import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/models/file_options_model.dart';
import 'package:file4base_client/features/solution_manager/file_options_dialog.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _user = UserModel(id: 'u1', username: 'alice', role: 'owner');

/// Opens File Options with a remembered password and an API whose user
/// update answers with [respond].
Future<GlobalKey<State>> _pump(WidgetTester tester, Future<http.Response> Function(http.Request) respond,
    {UserModel? user = _user, bool withApi = true}) async {
  tester.view.physicalSize = const Size(1200, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final key = GlobalKey<State>();
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: FileOptionsDialog(
        key: key,
        initialOptions: const FileOptionsModel(autoLoginEnabled: true, defaultUsername: 'alice', defaultPassword: 'old-pass'),
        layouts: const [],
        currentUser: user,
        apiClient: withApi ? ApiClient(baseUrl: 'http://localhost:8080', httpClient: MockClient(respond)) : null,
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return key;
}

Future<void> _changePassword(WidgetTester tester, String password) async {
  await tester.tap(find.text('Change password...'));
  await tester.pumpAndSettle();
  await tester.enterText(find.widgetWithText(TextFormField, 'New Password'), password);
  await tester.enterText(find.widgetWithText(TextFormField, 'Confirm New Password'), password);
  await tester.tap(find.text('Update Password'));
  await tester.pumpAndSettle();
}

String _rememberedPassword(WidgetTester tester) =>
    tester.widget<TextField>(find.byType(TextField).at(1)).controller!.text;

void main() {
  for (final (name, respond) in <(String, Future<http.Response> Function(http.Request))>[
    ('403', (_) async => http.Response('{"title":"Forbidden","detail":"not allowed"}', 403)),
    ('500', (_) async => http.Response('{"title":"Internal error"}', 500)),
    ('transport failure', (_) async => throw http.ClientException('connection refused')),
  ]) {
    testWidgets('a failed password change ($name) is not reported as success (#20)', (tester) async {
      await _pump(tester, respond);
      await _changePassword(tester, 'new-secret');
      expect(find.text('Password updated successfully.'), findsNothing);
      expect(find.text('Update Password'), findsOneWidget, reason: 'the form stays open');
      await tester.tap(find.text('Cancel').last);
      await tester.pumpAndSettle();
      expect(_rememberedPassword(tester), 'old-pass');
    });
  }

  testWidgets('a blank new password is rejected before calling the server (#20)', (tester) async {
    var calls = 0;
    await _pump(tester, (_) async {
      calls++;
      return http.Response('{}', 200);
    });
    await _changePassword(tester, '');
    expect(calls, 0);
    expect(find.text('Password updated successfully.'), findsNothing);
  });

  testWidgets('without a signed-in user the password cannot be "changed" (#20)', (tester) async {
    await _pump(tester, (_) async => http.Response('{}', 200), user: null);
    await tester.tap(find.text('Change password...'));
    await tester.pumpAndSettle();
    expect(find.text('Password updated successfully.'), findsNothing);
    expect(find.text('Sign in to the database to change the password.'), findsOneWidget);
  });

  testWidgets('a password accepted by the server updates the remembered password', (tester) async {
    await _pump(tester, (r) async => http.Response('{"id":"u1","username":"alice","role":"owner","is_active":true}', 200,
        headers: {'content-type': 'application/json'}));
    await _changePassword(tester, 'new-secret');
    expect(find.text('Password updated successfully.'), findsOneWidget);
    expect(_rememberedPassword(tester), 'new-secret');
  });
}
