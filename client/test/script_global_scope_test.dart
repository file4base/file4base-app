import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test(
    'same session shares globals; solutions, sessions and servers do not',
    () {
      final api = ApiClient(baseUrl: 'http://first');
      final dispatch = api.forPeerSession('dispatch');
      final results = api.forPeerSession('results');
      final secondSession = api.forPeerSession('another-dispatch-session');
      final otherServer = ApiClient(
        baseUrl: 'http://second',
      ).forPeerSession('dispatch');
      addTearDown(() {
        for (final client in [dispatch, results, secondSession, otherServer]) {
          client.clearSession();
        }
        api.close();
      });

      dispatch.scriptGlobalVariables[r'$$same'] = 'Dispatch value';
      results.scriptGlobalVariables[r'$$same'] = 'Results value';
      expect(
        api.forPeerSession('dispatch').scriptGlobalVariables[r'$$same'],
        'Dispatch value',
      );
      expect(
        api.forPeerSession('results').scriptGlobalVariables[r'$$same'],
        'Results value',
      );
      expect(secondSession.scriptGlobalVariables, isEmpty);
      expect(otherServer.scriptGlobalVariables, isEmpty);
      expect(api.scriptGlobalVariables, isEmpty);

      final retainedMap = dispatch.scriptGlobalVariables;
      dispatch.clearSession();
      expect(retainedMap, isEmpty);
      expect(api.forPeerSession('dispatch').scriptGlobalVariables, isEmpty);
      expect(results.scriptGlobalVariables[r'$$same'], 'Results value');
    },
  );

  for (final end in [
    'clear',
    'logout',
    'unauthorized',
    'server',
    'close',
    'login',
  ]) {
    test(
      '$end clears globals and leaves no values for the next session',
      () async {
        final transport = MockClient((request) async {
          if (request.url.path.endsWith('/login')) {
            return http.Response(
              jsonEncode({
                'status': 'ok',
                'database': 'dispatch',
                'token': 'new-session',
                'user': {
                  'id': 'u',
                  'username': 'test',
                  'role': 'owner',
                  'is_active': true,
                },
              }),
              200,
            );
          }
          if (request.url.path.endsWith('/logout'))
            return http.Response('{}', 200);
          return http.Response('{"detail":"expired"}', 401);
        });
        final base = ApiClient(
          baseUrl: 'http://lifecycle-$end',
          httpClient: transport,
        );
        final api = base.forPeerSession('old-session');
        final retained = api.scriptGlobalVariables;
        retained[r'$$value'] = 'must disappear';
        addTearDown(() {
          api.clearSession();
          base.forPeerSession('old-session').clearSession();
          base.close();
        });

        switch (end) {
          case 'clear':
            api.clearSession();
          case 'logout':
            await api.logout();
          case 'unauthorized':
            await expectLater(api.checkHealth(), throwsA(isA<ApiException>()));
          case 'server':
            api.updateBaseUrl('http://another-server');
          case 'close':
            api.close();
          case 'login':
            await api.login(username: 'test', password: 'synthetic');
        }
        expect(retained, isEmpty);
        expect(api.scriptGlobalVariables, isEmpty);
        expect(
          base.forPeerSession('old-session').scriptGlobalVariables,
          isEmpty,
        );
      },
    );
  }

  test('unauthenticated clients have separate temporary globals', () {
    final first = ApiClient();
    final second = ApiClient();
    addTearDown(first.close);
    addTearDown(second.close);
    first.scriptGlobalVariables[r'$$value'] = 'temporary';
    expect(second.scriptGlobalVariables, isEmpty);
    first.clearSession();
    expect(first.scriptGlobalVariables, isEmpty);
  });
}
