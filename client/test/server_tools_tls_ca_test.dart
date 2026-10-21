import 'dart:typed_data';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/server_tools/server_tools_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// A server administrator's API whose TLS panel is served by the persisted
/// File4Base Local CA.
class _TlsApi extends ApiClient {
  _TlsApi({this.caAvailable = true}) : super(baseUrl: 'http://localhost:1');

  final bool caAvailable;
  int caDownloads = 0;
  int bundleDownloads = 0;

  @override
  Future<Map<String, dynamic>> getServerAccess() async => {
        'server_admin': true,
        'admin_database_configured': true,
        'database_tools': true,
      };

  @override
  Future<Map<String, dynamic>> listServerContainers({bool refresh = false}) async => {'containers': <dynamic>[]};

  @override
  Future<Map<String, dynamic>> getServerTlsConfig() async => {
        'domain': 'localhost',
        'mode': 'self_signed',
        'cert_source': caAvailable ? 'local_ca' : 'ephemeral',
        'cert_subject': 'CN=localhost,O=File4Base',
        'cert_issuer': 'CN=File4Base Local CA,O=File4Base',
        'cert_not_after': '2027-01-01T00:00:00Z',
        'cert_days_left': 390,
        'cert_sha256': 'AA11BB22',
        'ca_available': caAvailable,
        'ca_subject': 'CN=File4Base Local CA,O=File4Base',
        'ca_not_after': '2036-01-01T00:00:00Z',
        'ca_sha256': 'CC33DD44',
      };

  @override
  Future<Uint8List> downloadServerCaCertificate() async {
    caDownloads++;
    return Uint8List.fromList('-----BEGIN CERTIFICATE-----\n'.codeUnits);
  }

  @override
  Future<Uint8List> downloadServerCertificateBundle() async {
    bundleDownloads++;
    return Uint8List.fromList('-----BEGIN CERTIFICATE-----\n'.codeUnits);
  }
}

Future<void> _openTlsPanel(WidgetTester tester, ApiClient api) async {
  // The test font draws every glyph as a square, so the fixed-width header
  // overflows; the dialog's own section list also trips a Material styling
  // assertion. Only those two layout warnings are ignored.
  final previous = FlutterError.onError;
  FlutterError.onError = (details) {
    final message = details.exceptionAsString();
    if (message.contains('RenderFlex overflowed')) return;
    if (message.contains('ListTile background color or ink splashes may be invisible')) return;
    previous?.call(details);
  };
  addTearDown(() => FlutterError.onError = previous);
  tester.view.physicalSize = const Size(1400, 1600);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);

  await tester.pumpWidget(MaterialApp(home: Scaffold(body: ServerToolsDialog(apiClient: api))));
  await tester.pump();
  await tester.pump();
  await tester.tap(find.text('Domain & SSL/TLS (ACME)'));
  await tester.pump();
  await tester.pump();
}

void main() {
  testWidgets('the TLS panel offers the local CA certificate with install instructions', (tester) async {
    final api = _TlsApi();
    await _openTlsPanel(tester, api);

    expect(find.text('File4Base Local CA — install once per machine'), findsOneWidget);
    expect(find.text('Download CA Certificate'), findsOneWidget);
    expect(find.text('Download Certificate Chain (PEM)'), findsOneWidget);
    expect(find.textContaining('no private key ever leaves the server'), findsOneWidget);

    // Per-platform instructions, including Firefox's own trust store.
    for (final platform in ['macOS: ', 'Windows: ', 'Linux: ', 'Firefox: ']) {
      expect(
        find.byWidgetPredicate((w) => w is RichText && w.text.toPlainText().startsWith(platform)),
        findsOneWidget,
        reason: platform,
      );
    }
    expect(find.textContaining('CN=File4Base Local CA'), findsWidgets);
  });

  testWidgets('the download buttons ask the server for the certificates', (tester) async {
    final api = _TlsApi();
    await _openTlsPanel(tester, api);

    await tester.ensureVisible(find.text('Download CA Certificate'));
    await tester.pump();
    await tester.tap(find.text('Download CA Certificate'), warnIfMissed: false);
    await tester.pump();
    await tester.pump();
    expect(api.caDownloads, 1);

    await tester.ensureVisible(find.text('Download Certificate Chain (PEM)'));
    await tester.pump();
    await tester.tap(find.text('Download Certificate Chain (PEM)'), warnIfMissed: false);
    await tester.pump();
    await tester.pump();
    expect(api.bundleDownloads, 1);
  });

  testWidgets('without a persisted authority the panel offers no download', (tester) async {
    await _openTlsPanel(tester, _TlsApi(caAvailable: false));

    expect(find.text('Download CA Certificate'), findsNothing);
    expect(find.text('File4Base Local CA — install once per machine'), findsNothing);
  });
}
