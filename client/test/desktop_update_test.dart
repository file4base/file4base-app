import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/models/client_update_policy.dart';
import 'package:file4base_client/core/models/file_options_model.dart';
import 'package:file4base_client/core/services/desktop_update_service.dart';
import 'package:file4base_client/features/server_tools/client_update_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// A verified update of the installed desktop client, as the API reports it.
DesktopUpdateInfo infoFor(List<int> payload, {String? sha, int? size}) {
  return DesktopUpdateInfo(
    platform: 'macos',
    clientVersion: '0.19.2',
    latestVersion: '0.20.0',
    updateAvailable: true,
    assetName: 'File4Base-macOS.dmg',
    downloadUrl: 'https://releases.test/File4Base-macOS.dmg',
    sha256: sha ?? sha256.convert(payload).toString(),
    sizeBytes: size ?? payload.length,
    releaseUrl: 'https://github.com/file4base/file4base-app/releases/tag/v0.20.0',
    releaseNotes: 'Adds the in-app desktop updater.',
  );
}

void main() {
  group('ClientUpdatePolicy', () {
    test('round-trips through File Options JSON', () {
      for (final policy in ClientUpdatePolicy.values) {
        final restored = FileOptionsModel.fromJson(
          FileOptionsModel(clientUpdatePolicy: policy).toJson(),
        );
        expect(restored.clientUpdatePolicy, policy);
      }
    });

    test('defaults to prompt for solutions saved before the key existed', () {
      expect(FileOptionsModel.fromJson(const {}).clientUpdatePolicy,
          ClientUpdatePolicy.prompt);
      expect(const FileOptionsModel().clientUpdatePolicy, ClientUpdatePolicy.prompt);
    });

    test('accepts both spellings of admin-only and off', () {
      expect(ClientUpdatePolicy.fromJson('admin-only'), ClientUpdatePolicy.adminOnly);
      expect(ClientUpdatePolicy.fromJson('admin_only'), ClientUpdatePolicy.adminOnly);
      expect(ClientUpdatePolicy.fromJson('OFF'), ClientUpdatePolicy.off);
      expect(ClientUpdatePolicy.fromJson('disabled'), ClientUpdatePolicy.off);
      expect(ClientUpdatePolicy.fromJson('nonsense'), ClientUpdatePolicy.prompt);
    });
  });

  group('DesktopUpdateInfo', () {
    test('parses the endpoint payload', () {
      final info = DesktopUpdateInfo.fromJson(<String, dynamic>{
        'platform': 'linux',
        'client_version': '0.19.0',
        'latest_version': '0.20.0',
        'update_available': true,
        'asset_name': 'File4Base-Linux-x64.deb',
        'download_url': 'https://releases.test/deb',
        'sha256': 'A' * 64,
        'size_bytes': 3 * 1024 * 1024,
        'release_url': 'https://releases.test/page',
        'release_notes': 'notes',
      });

      expect(info.platform, 'linux');
      expect(info.updateAvailable, isTrue);
      expect(info.sha256, 'a' * 64, reason: 'digests are compared lower-case');
      expect(info.sizeLabel, '3.0 MB');
      expect(info.canInstallInApp, isTrue);
    });

    test('refuses an in-app install without a usable digest', () {
      // No checksum published: the app must not run an unverified installer.
      expect(infoFor(const [1, 2, 3], sha: '').canInstallInApp, isFalse);
      // A truncated or malformed digest is no digest either.
      expect(infoFor(const [1, 2, 3], sha: 'abc123').canInstallInApp, isFalse);
    });

    test('refuses an in-app install with no download URL', () {
      const info = DesktopUpdateInfo(
        platform: 'macos',
        clientVersion: '0.19.2',
        latestVersion: '0.20.0',
        updateAvailable: true,
        releaseUrl: 'https://releases.test/page',
      );
      expect(info.canInstallInApp, isFalse);
    });

    test('an up-to-date client is never installable', () {
      final payload = utf8.encode('installer');
      final info = DesktopUpdateInfo(
        platform: 'macos',
        clientVersion: '0.20.0',
        latestVersion: '0.20.0',
        updateAvailable: false,
        assetName: 'File4Base-macOS.dmg',
        downloadUrl: 'https://releases.test/dmg',
        sha256: sha256.convert(payload).toString(),
      );
      expect(info.canInstallInApp, isFalse);
    });
  });

  group('DesktopUpdateService.check', () {
    test('asks the update endpoint for this platform and version', () async {
      Uri? asked;
      final api = ApiClient(
        baseUrl: 'http://test-server:8080',
        httpClient: MockClient((request) async {
          asked = request.url;
          return http.Response(
            jsonEncode(<String, dynamic>{
              'platform': 'macos',
              'client_version': '0.19.2',
              'latest_version': '0.20.0',
              'update_available': true,
              'release_url': 'https://releases.test/page',
            }),
            200,
            headers: const {'content-type': 'application/json'},
          );
        }),
      );

      final raw = await api.fetchDesktopUpdate(platform: 'macos', clientVersion: '0.19.2');

      expect(asked?.path, '/api/v1/updates/desktop');
      expect(asked?.queryParameters['platform'], 'macos');
      expect(asked?.queryParameters['client_version'], '0.19.2');
      expect(DesktopUpdateInfo.fromJson(raw).latestVersion, '0.20.0');
    });

    test('a failed check is silent rather than disruptive', () async {
      final api = ApiClient(
        baseUrl: 'http://test-server:8080',
        httpClient: MockClient((_) async => http.Response('boom', 500)),
      );
      final service = DesktopUpdateService(downloadClient: MockClient((_) async => http.Response('', 404)));
      addTearDown(service.close);

      // Null either because the check failed or because the test host is not a
      // supported desktop target; both mean "do not bother anyone".
      expect(await service.check(api, currentVersion: '0.19.2'), isNull);
    });
  });

  group('DesktopUpdateService.download', () {
    test('verifies the download against the published checksum', () async {
      final payload = utf8.encode('a pretend installer payload');
      final info = infoFor(payload);
      final progress = <int>[];
      final service = DesktopUpdateService(
        downloadClient: MockClient((_) async => http.Response.bytes(payload, 200)),
      );
      addTearDown(service.close);

      final file = await service.download(
        info,
        onProgress: (received, _) => progress.add(received),
      );
      addTearDown(() => file.parent.deleteSync(recursive: true));

      expect(file.existsSync(), isTrue);
      expect(file.readAsBytesSync(), payload);
      expect(file.path, endsWith('File4Base-macOS.dmg'));
      expect(progress, isNotEmpty);
      expect(progress.last, payload.length);
    });

    test('discards a download whose bytes do not match the checksum', () async {
      final info = infoFor(utf8.encode('the published installer'));
      final service = DesktopUpdateService(
        downloadClient: MockClient(
          (_) async => http.Response.bytes(utf8.encode('something else entirely'), 200),
        ),
      );
      addTearDown(service.close);

      Set<String> updateTempDirs() => Directory.systemTemp
          .listSync()
          .whereType<Directory>()
          .map((d) => d.path)
          .where((p) => p.contains('file4base-update-'))
          .toSet();

      final before = updateTempDirs();
      await expectLater(
        service.download(info),
        throwsA(isA<ChecksumMismatchException>()),
      );
      // Nothing is left on disk for anyone to run by hand.
      expect(updateTempDirs().difference(before), isEmpty);
    });

    test('a release server error leaves no partial installer', () async {
      final info = infoFor(utf8.encode('payload'));
      final service = DesktopUpdateService(
        downloadClient: MockClient((_) async => http.Response('gone', 404)),
      );
      addTearDown(service.close);

      await expectLater(service.download(info), throwsA(isA<http.ClientException>()));
    });

    test('refuses to download an update with no verifiable digest', () async {
      final service = DesktopUpdateService(
        downloadClient: MockClient((_) async => http.Response('', 200)),
      );
      addTearDown(service.close);

      await expectLater(
        service.download(infoFor(const [1, 2, 3], sha: '')),
        throwsA(isA<StateError>()),
      );
    });
  });

  group('ClientUpdateDialog', () {
    testWidgets('offers the update, downloads it and reports it verified',
        (tester) async {
      tester.view.physicalSize = const Size(900, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      final payload = utf8.encode('a pretend installer payload');
      final info = infoFor(payload);
      final service = DesktopUpdateService(
        downloadClient: MockClient((_) async => http.Response.bytes(payload, 200)),
      );
      addTearDown(service.close);

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (ctx) => ElevatedButton(
                onPressed: () => ClientUpdateDialog.show(ctx, info, service: service),
                child: const Text('open'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();

      // The offer names both versions and never mentions a command to run.
      expect(find.text('A new version of File4Base is available'), findsOneWidget);
      expect(find.text('v0.19.2'), findsOneWidget);
      expect(find.text('v0.20.0'), findsOneWidget);
      expect(find.textContaining('docker'), findsNothing);
      expect(find.text('Download and install'), findsOneWidget);

      // The download does real file IO, which only runs under runAsync.
      await tester.runAsync(() async {
        await tester.tap(find.text('Download and install'));
        await Future<void>.delayed(const Duration(milliseconds: 250));
      });
      await tester.pumpAndSettle();

      expect(find.text('Ready to install'), findsOneWidget);
      expect(find.textContaining('matches its published SHA-256'), findsOneWidget);
      expect(find.byKey(const Key('client_update_install_button')), findsOneWidget);
    });

    testWidgets('a mismatched download is reported, not installed', (tester) async {
      tester.view.physicalSize = const Size(900, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      final info = infoFor(utf8.encode('the published installer'));
      final service = DesktopUpdateService(
        downloadClient: MockClient(
          (_) async => http.Response.bytes(utf8.encode('tampered'), 200),
        ),
      );
      addTearDown(service.close);

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (ctx) => ElevatedButton(
                onPressed: () => ClientUpdateDialog.show(ctx, info, service: service),
                child: const Text('open'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();
      await tester.runAsync(() async {
        await tester.tap(find.text('Download and install'));
        await Future<void>.delayed(const Duration(milliseconds: 250));
      });
      await tester.pumpAndSettle();

      expect(find.text('The update could not be downloaded'), findsOneWidget);
      expect(find.textContaining('does not match the published checksum'), findsOneWidget);
      expect(find.byKey(const Key('client_update_install_button')), findsNothing);
      expect(find.text('Open release page'), findsOneWidget);
    });

    testWidgets('without a verifiable installer it only offers the release page',
        (tester) async {
      tester.view.physicalSize = const Size(900, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      final info = infoFor(const [1, 2, 3], sha: '');
      final service = DesktopUpdateService(
        downloadClient: MockClient((_) async => http.Response('', 404)),
      );
      addTearDown(service.close);

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (ctx) => ElevatedButton(
                onPressed: () => ClientUpdateDialog.show(ctx, info, service: service),
                child: const Text('open'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();

      expect(find.text('Download and install'), findsNothing);
      expect(find.text('Release page'), findsOneWidget);
      expect(find.textContaining('no verified installer'), findsOneWidget);
    });
  });
}
