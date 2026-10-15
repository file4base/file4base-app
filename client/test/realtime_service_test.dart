import 'dart:convert';

import 'package:file4base_client/core/realtime/realtime_service.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('realtimeUri', () {
    test('swaps http for ws and https for wss, keeping the host', () {
      expect(realtimeUri('http://127.0.0.1:8080').toString(), 'ws://127.0.0.1:8080/api/v1/realtime');
      expect(realtimeUri('https://app.example.com').toString(), 'wss://app.example.com/api/v1/realtime');
    });
  });

  group('RealtimeService.handleMessage', () {
    String ev(String type, {String resource = 'orders', String origin = 'other'}) =>
        jsonEncode({'type': type, 'resource': resource, 'origin': origin, 'database': 'db', 'at': 1});

    test('emits data/schema events from other sessions', () {
      final s = RealtimeService();
      addTearDown(s.dispose);
      final data = s.handleMessage(ev('data'));
      expect(data, isNotNull);
      expect(data!.isData, isTrue);
      expect(data.resource, 'orders');
      expect(s.handleMessage(ev('schema', resource: 'layouts'))!.isSchema, isTrue);
    });

    test('drops this client\'s own events once the welcome gives its origin', () {
      final s = RealtimeService();
      addTearDown(s.dispose);
      expect(s.handleMessage(jsonEncode({'type': 'welcome', 'origin': 'me'})), isNull);
      expect(s.ownOrigin, 'me');
      expect(s.handleMessage(ev('data', origin: 'me')), isNull, reason: 'own change ignored');
      expect(s.handleMessage(ev('data', origin: 'someone')), isNotNull);
    });

    test('ignores unknown types and malformed messages', () {
      final s = RealtimeService();
      addTearDown(s.dispose);
      expect(s.handleMessage(ev('presence')), isNull);
      expect(s.handleMessage('not json'), isNull);
    });
  });
}
