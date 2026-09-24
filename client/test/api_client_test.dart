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
}
