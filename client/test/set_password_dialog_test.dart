import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/security/set_password_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _user = UserModel(id: 'u-1', username: 'olivia', role: 'owner');

Future<void> _open(WidgetTester tester, MockClient client, {required bool own}) async {
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: Builder(
        builder: (ctx) => ElevatedButton(
          onPressed: () => SetPasswordDialog.show(
            ctx,
            apiClient: ApiClient(baseUrl: 'http://localhost:8080', httpClient: client),
            user: _user,
            isOwnAccount: own,
          ),
          child: const Text('Open'),
        ),
      ),
    ),
  ));
  await tester.tap(find.text('Open'));
  await tester.pumpAndSettle();
}

Future<void> _save(WidgetTester tester, String password) async {
  await tester.enterText(find.byKey(const Key('set-password-new')), password);
  await tester.enterText(find.byKey(const Key('set-password-confirm')), password);
  await tester.tap(find.byKey(const Key('set-password-save')));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('your own password takes the current one (#209)', (tester) async {
    final bodies = <Map<String, dynamic>>[];
    var answer = http.Response(
        '{"title":"Forbidden","status":403,"detail":"the current password is incorrect"}', 403,
        headers: {'content-type': 'application/problem+json'});
    await _open(tester, MockClient((r) async {
      bodies.add(jsonDecode(r.body) as Map<String, dynamic>);
      return answer;
    }), own: true);

    expect(find.byKey(const Key('set-password-current')), findsOneWidget);
    await _save(tester, 'Fern-2026-long');
    expect(find.text('Enter your current password.'), findsOneWidget);
    expect(bodies, isEmpty);

    await tester.enterText(find.byKey(const Key('set-password-current')), 'not-it');
    await tester.tap(find.byKey(const Key('set-password-save')));
    await tester.pumpAndSettle();
    expect(bodies.single['current_password'], 'not-it');
    expect(find.text('The current password is incorrect.'), findsOneWidget);
    expect(find.byKey(const Key('set-password-save')), findsOneWidget, reason: 'the dialog stays open');

    answer = http.Response('{"id":"u-1","status":"updated"}', 200, headers: {'content-type': 'application/json'});
    await tester.enterText(find.byKey(const Key('set-password-current')), 'old-secret');
    await tester.tap(find.byKey(const Key('set-password-save')));
    await tester.pumpAndSettle();
    expect(bodies.last, {'role': '', 'password': 'Fern-2026-long', 'current_password': 'old-secret'});
    expect(find.byKey(const Key('set-password-save')), findsNothing);
  });

  testWidgets("another account's password is set without its current one", (tester) async {
    final bodies = <Map<String, dynamic>>[];
    await _open(tester, MockClient((r) async {
      bodies.add(jsonDecode(r.body) as Map<String, dynamic>);
      return http.Response('{"id":"u-1","status":"updated"}', 200, headers: {'content-type': 'application/json'});
    }), own: false);

    expect(find.byKey(const Key('set-password-current')), findsNothing);
    await _save(tester, 'Fern-2026-long');
    expect(bodies.single, {'role': '', 'password': 'Fern-2026-long'});
  });
}
