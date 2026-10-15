import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/server_tools/server_tools_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class _AccessApi extends ApiClient {
  _AccessApi({required this.serverAdmin, this.configured = true}) : super(baseUrl: 'http://localhost:1');
  final bool serverAdmin;
  final bool configured;
  final requested = <String>[];

  @override
  Future<Map<String, dynamic>> getServerAccess() async => {
        'server_admin': serverAdmin,
        'admin_database_configured': configured,
        'database_tools': true,
      };

  @override
  Future<List<Map<String, dynamic>>> listServerBackups() async {
    requested.add('backups');
    return const [];
  }

  @override
  Future<Map<String, dynamic>> listServerContainers({bool refresh = false}) async {
    requested.add('containers');
    return {'containers': <dynamic>[]};
  }
}

Future<void> _open(WidgetTester tester, ApiClient api) async {
  // The test font draws every glyph as a square, so the fixed-width header
  // overflows; only that layout warning is ignored.
  final previous = FlutterError.onError;
  FlutterError.onError = (details) {
    if (details.exceptionAsString().contains('RenderFlex overflowed')) return;
    previous?.call(details);
  };
  addTearDown(() => FlutterError.onError = previous);
  tester.view.physicalSize = const Size(1400, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  await tester.pumpWidget(MaterialApp(home: Scaffold(body: ServerToolsDialog(apiClient: api))));
  await tester.pump();
  await tester.pump();
}

void main() {
  testWidgets('a database owner only sees the tools of their own database', (tester) async {
    final api = _AccessApi(serverAdmin: false);
    await _open(tester, api);

    expect(find.text('Backups & Snapshots'), findsOneWidget);
    expect(find.text('Database Maintenance'), findsOneWidget);
    for (final serverWide in ['Containers & Services', 'Connected Clients', 'Audit & Security Logs', 'Domain & SSL/TLS (ACME)']) {
      expect(find.text(serverWide), findsNothing, reason: serverWide);
    }
    expect(find.textContaining('owners of the server administration database'), findsOneWidget);
    // It opened the backups instead of the (server-wide) containers section.
    expect(api.requested, ['backups']);
  });

  testWidgets('without FILE4BASE_ADMIN_DATABASE the window says how to enable the server tools', (tester) async {
    await _open(tester, _AccessApi(serverAdmin: false, configured: false));
    expect(find.textContaining('FILE4BASE_ADMIN_DATABASE'), findsOneWidget);
  });

  testWidgets('the server administrator sees every tool', (tester) async {
    final api = _AccessApi(serverAdmin: true);
    await _open(tester, api);
    expect(find.text('Containers & Services'), findsOneWidget);
    expect(find.text('Connected Clients'), findsOneWidget);
    expect(find.textContaining('owners of the server administration database'), findsNothing);
    expect(api.requested, ['containers']);
  });
}
