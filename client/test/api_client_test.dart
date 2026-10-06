import 'dart:convert';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  group('TraceContext', () {
    test('generates valid W3C traceparent string and headers', () {
      final trace = TraceContext.create();
      expect(trace.traceId.length, 32);
      expect(trace.spanId.length, 16);
      expect(trace.traceparent, startsWith('00-'));
      expect(trace.traceparent, endsWith('-01'));

      final headers = trace.toHeaders();
      expect(headers['traceparent'], trace.traceparent);
      expect(headers['X-Trace-ID'], trace.traceId);
    });
  });

  group('ApiException (RFC 9457)', () {
    test('parses application/problem+json response body correctly', () {
      final problemJson = jsonEncode({
        'type': 'https://tools.ietf.org/html/rfc9110#section-15.5.5',
        'title': 'Not Found',
        'status': 404,
        'detail': 'Resource with id xyz does not exist.',
        'instance': '/api/v1/schemas/tables/xyz',
        'trace_id': '4bf92f3577b34da6a3ce929d0e0e4736',
      });

      final response = http.Response(
        problemJson,
        404,
        headers: {
          'content-type': 'application/problem+json',
          'x-trace-id': '4bf92f3577b34da6a3ce929d0e0e4736',
        },
      );

      final exception = ApiException.fromResponse(response);
      expect(exception.statusCode, 404);
      expect(exception.title, 'Not Found');
      expect(exception.detail, 'Resource with id xyz does not exist.');
      expect(exception.instance, '/api/v1/schemas/tables/xyz');
      expect(exception.traceId, '4bf92f3577b34da6a3ce929d0e0e4736');
      expect(exception.toString(), contains('[Trace ID: 4bf92f3577b34da6a3ce929d0e0e4736]'));
    });

    test('falls back gracefully on non-json error responses', () {
      final response = http.Response(
        'Bad Gateway Error',
        502,
        headers: {'content-type': 'text/plain'},
      );

      final exception = ApiException.fromResponse(response);
      expect(exception.statusCode, 502);
      expect(exception.title, 'HTTP 502 Error');
      expect(exception.detail, 'Bad Gateway Error');
    });
  });

  group('ApiClient Tracing & Health Probes', () {
    test('propagates traceparent header on requests', () async {
      String? receivedTraceparent;
      String? receivedTraceId;

      final mockClient = MockClient((request) async {
        receivedTraceparent = request.headers['traceparent'];
        receivedTraceId = request.headers['x-trace-id'];

        if (request.url.path == '/healthz') {
          return http.Response(
            jsonEncode({
              'status': 'pass',
              'engine': 'postgres',
              'database': 'up',
            }),
            200,
            headers: {'content-type': 'application/health+json'},
          );
        }
        return http.Response('Not Found', 404);
      });

      final client = ApiClient(baseUrl: 'http://test-server:8080', httpClient: mockClient);
      final health = await client.checkHealth();

      expect(health['status'], 'pass');
      expect(health['engine'], 'postgres');
      expect(receivedTraceparent, isNotNull);
      expect(receivedTraceparent, startsWith('00-'));
      expect(receivedTraceId, isNotNull);
    });

    test('liveness and readiness probes return boolean success', () async {
      final mockClient = MockClient((request) async {
        if (request.url.path == '/healthz/liveness') {
          return http.Response(jsonEncode({'status': 'pass'}), 200);
        }
        if (request.url.path == '/healthz/readiness') {
          return http.Response(jsonEncode({'status': 'pass'}), 200);
        }
        return http.Response('Error', 503);
      });

      final client = ApiClient(baseUrl: 'http://test-server:8080', httpClient: mockClient);
      expect(await client.checkLiveness(), isTrue);
      expect(await client.checkReadiness(), isTrue);
    });

    test('throws ApiException on 4xx/5xx responses with RFC 9457 payload', () async {
      final mockClient = MockClient((request) async {
        return http.Response(
          jsonEncode({
            'type': 'https://tools.ietf.org/html/rfc9110#section-15.5.1',
            'title': 'Bad Request',
            'status': 400,
            'detail': 'Invalid table name provided',
            'trace_id': '1234567890abcdef1234567890abcdef',
          }),
          400,
          headers: {'content-type': 'application/problem+json'},
        );
      });

      final client = ApiClient(baseUrl: 'http://test-server:8080', httpClient: mockClient);
      expect(
        () => client.listTables(),
        throwsA(isA<ApiException>().having(
          (e) => e.detail,
          'detail',
          'Invalid table name provided',
        )),
      );
    });
  });

  group('ApiClient Session Handling', () {
    const userJson = {'id': 'u1', 'username': 'alice', 'role': 'owner', 'is_active': true};

    test('sends the session token returned by login on later requests', () async {
      final authHeaders = <String, String?>{};

      final mockClient = MockClient((request) async {
        authHeaders[request.url.path] = request.headers['authorization'];
        if (request.url.path == '/api/v1/auth/login') {
          return http.Response(
            jsonEncode({'status': 'ok', 'database': 'sales', 'user': userJson, 'token': 'tok-123'}),
            200,
          );
        }
        if (request.url.path == '/api/v1/schemas/tables') {
          return http.Response('[]', 200);
        }
        return http.Response('', 204);
      });

      final client = ApiClient(baseUrl: 'http://test-server:8080', httpClient: mockClient);
      expect(client.isAuthenticated, isFalse);

      final auth = await client.login(username: 'alice', password: 'secret', database: 'sales');
      expect(auth.token, 'tok-123');
      expect(client.isAuthenticated, isTrue);
      expect(authHeaders['/api/v1/auth/login'], isNull);

      await client.listTables();
      expect(authHeaders['/api/v1/schemas/tables'], 'Bearer tok-123');

      await client.logout();
      expect(client.isAuthenticated, isFalse);
      expect(authHeaders['/api/v1/auth/logout'], 'Bearer tok-123');

      await client.listTables();
      expect(authHeaders['/api/v1/schemas/tables'], isNull);
    });

    test('a 401 on a protected request drops the session and notifies', () async {
      var loggedIn = false;
      final mockClient = MockClient((request) async {
        if (request.url.path == '/api/v1/auth/login') {
          loggedIn = true;
          return http.Response(
            jsonEncode({'status': 'ok', 'database': 'sales', 'user': userJson, 'token': 'tok-123'}),
            200,
          );
        }
        return http.Response(
          jsonEncode({'title': 'Authentication Required', 'status': 401, 'detail': 'session expired'}),
          401,
        );
      });

      final client = ApiClient(baseUrl: 'http://test-server:8080', httpClient: mockClient);
      var notified = 0;
      client.onUnauthorized = () => notified++;

      await client.login(username: 'alice', password: 'secret', database: 'sales');
      expect(loggedIn, isTrue);
      expect(notified, 0);

      await expectLater(client.listTables(), throwsA(isA<ApiException>().having((e) => e.statusCode, 'statusCode', 401)));
      expect(notified, 1);
      expect(client.isAuthenticated, isFalse);
    });

    test('a failed login does not end the current session', () async {
      var attempts = 0;
      final mockClient = MockClient((request) async {
        if (request.url.path == '/api/v1/auth/login') {
          attempts++;
          if (attempts == 1) {
            return http.Response(
              jsonEncode({'status': 'ok', 'database': 'sales', 'user': userJson, 'token': 'tok-123'}),
              200,
            );
          }
          return http.Response(
            jsonEncode({'title': 'Authentication Failed', 'status': 401, 'detail': 'invalid username or password'}),
            401,
          );
        }
        return http.Response('[]', 200);
      });

      final client = ApiClient(baseUrl: 'http://test-server:8080', httpClient: mockClient);
      var notified = 0;
      client.onUnauthorized = () => notified++;

      await client.login(username: 'alice', password: 'secret', database: 'sales');
      await expectLater(
        client.login(username: 'alice', password: 'wrong', database: 'other'),
        throwsA(isA<ApiException>()),
      );
      expect(notified, 0);
      expect(client.authToken, 'tok-123');
    });

    test('deleteDatabase forwards the owner credentials of the target database', () async {
      String? body;
      final mockClient = MockClient((request) async {
        body = request.body;
        return http.Response(jsonEncode({'database': 'old_db', 'status': 'deleted'}), 200);
      });

      final client = ApiClient(baseUrl: 'http://test-server:8080', httpClient: mockClient);
      await client.deleteDatabase('old_db', ownerUsername: 'bob', ownerPassword: 'secret-b');

      final decoded = jsonDecode(body!) as Map<String, dynamic>;
      expect(decoded['username'], 'bob');
      expect(decoded['password'], 'secret-b');
    });
  });

  group('ApiClient.listAllRows (#6)', () {
    /// Serves [total] rows the way the server does: pages of at most 1000.
    MockClient server(int total, {int? failAtOffset, List<Uri>? log}) => MockClient((request) async {
          log?.add(request.url);
          final offset = int.parse(request.url.queryParameters['offset']!);
          if (offset == failAtOffset) return http.Response('{"title":"boom"}', 500);
          final limit = int.parse(request.url.queryParameters['limit']!).clamp(1, 1000);
          final end = (offset + limit).clamp(0, total);
          final rows = [for (var i = offset; i < end; i++) {'id': 'r${i.toString().padLeft(5, '0')}'}];
          return http.Response(jsonEncode(rows), 200, headers: {'content-type': 'application/json'});
        });

    for (final total in [0, 100, 101, 1000, 1001, 2500]) {
      test('returns all $total rows', () async {
        final log = <Uri>[];
        final api = ApiClient(baseUrl: 'http://x', httpClient: server(total, log: log));
        final rows = await api.listAllRows('items');
        expect(rows.map((r) => r['id']).toSet().length, total);
        expect(log.every((u) => u.queryParameters['sort_by'] == 'id'), isTrue, reason: 'stable order on every page');
      });
    }

    test('a failing later page fails the whole read', () async {
      final api = ApiClient(baseUrl: 'http://x', httpClient: server(2500, failAtOffset: 1000));
      expect(api.listAllRows('items'), throwsA(isA<Exception>()));
    });
  });
}
