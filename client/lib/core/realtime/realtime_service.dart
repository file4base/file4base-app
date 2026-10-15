import 'dart:async';
import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter/foundation.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

/// One change notification from the server: [type] is `data` or `schema`,
/// [resource] is the table name (data) or the kind of object (schema).
@immutable
class RealtimeEvent {
  const RealtimeEvent({required this.type, required this.resource, required this.origin});
  final String type;
  final String resource;
  final String origin;

  bool get isData => type == 'data';
  bool get isSchema => type == 'schema';
}

/// Builds the WebSocket URL for the change-notification endpoint from the REST
/// base URL: the same host, with http(s) swapped for ws(s). An empty or
/// relative base (the web client, served same-origin) resolves against the
/// current page.
Uri realtimeUri(String baseUrl) {
  final base = baseUrl.trim().isEmpty ? Uri.base : Uri.parse(baseUrl.trim());
  final resolved = base.hasScheme ? base : Uri.base;
  final wsScheme = resolved.scheme == 'https' ? 'wss' : 'ws';
  return Uri(
    scheme: wsScheme,
    host: resolved.host,
    port: resolved.hasPort ? resolved.port : null,
    path: '/api/v1/realtime',
  );
}

/// Subscribes to the server's change-notification WebSocket and re-publishes
/// the events, dropping the ones this client caused itself (same `origin`).
/// It reconnects on its own while [connect] is in effect.
class RealtimeService {
  RealtimeService({WebSocketChannel Function(Uri)? connectChannel})
      : _connectChannel = connectChannel ?? WebSocketChannel.connect;

  final WebSocketChannel Function(Uri) _connectChannel;
  final _controller = StreamController<RealtimeEvent>.broadcast();

  WebSocketChannel? _channel;
  StreamSubscription<dynamic>? _sub;
  String? _token;
  String? _baseUrl;
  String? _ownOrigin;
  Timer? _reconnect;
  int _attempt = 0;
  bool _active = false;

  /// Events from other sessions for this client's database.
  Stream<RealtimeEvent> get events => _controller.stream;

  /// This client's own origin id (known after the welcome message).
  String? get ownOrigin => _ownOrigin;

  /// Starts (or restarts) the connection for a signed-in session.
  void connect({required ApiClient api, required String token}) {
    final base = api.baseUrl;
    if (_active && token == _token && base == _baseUrl) return;
    disconnect();
    _active = true;
    _token = token;
    _baseUrl = base;
    _open();
  }

  void _open() {
    if (!_active || _token == null) return;
    final Uri uri;
    try {
      uri = realtimeUri(_baseUrl ?? '');
    } on FormatException {
      return;
    }
    try {
      final channel = _connectChannel(uri);
      _channel = channel;
      channel.sink.add(jsonEncode({'token': _token}));
      _sub = channel.stream.listen(
        _onMessage,
        onDone: _scheduleReconnect,
        onError: (_) => _scheduleReconnect(),
        cancelOnError: true,
      );
    } on Exception {
      _scheduleReconnect();
    }
  }

  void _onMessage(dynamic raw) {
    _attempt = 0; // a message proves the link is healthy
    final ev = handleMessage(raw is String ? raw : '');
    if (ev != null) _controller.add(ev);
  }

  /// Parses one server message: stores the welcome's origin, drops this
  /// client's own events and anything that is not a data/schema event, and
  /// returns the event to publish (or null). Exposed for testing.
  @visibleForTesting
  RealtimeEvent? handleMessage(String raw) {
    Map<String, dynamic> msg;
    try {
      msg = jsonDecode(raw) as Map<String, dynamic>;
    } on Object {
      return null;
    }
    final type = msg['type'] as String?;
    if (type == 'welcome') {
      _ownOrigin = msg['origin'] as String?;
      return null;
    }
    if (type != 'data' && type != 'schema') return null;
    final origin = (msg['origin'] as String?) ?? '';
    if (_ownOrigin != null && origin == _ownOrigin) return null; // my own change
    return RealtimeEvent(
      type: type!,
      resource: (msg['resource'] as String?) ?? '',
      origin: origin,
    );
  }

  void _scheduleReconnect() {
    _sub?.cancel();
    _sub = null;
    _channel = null;
    if (!_active) return;
    _attempt = (_attempt + 1).clamp(1, 6);
    final delay = Duration(seconds: [1, 2, 4, 8, 15, 30][_attempt - 1]);
    _reconnect?.cancel();
    _reconnect = Timer(delay, _open);
  }

  /// Stops the connection (e.g. on sign-out).
  void disconnect() {
    _active = false;
    _reconnect?.cancel();
    _reconnect = null;
    _sub?.cancel();
    _sub = null;
    _ownOrigin = null;
    try {
      _channel?.sink.close();
    } on Object {
      // ignore close errors
    }
    _channel = null;
  }

  void dispose() {
    disconnect();
    _controller.close();
  }
}
