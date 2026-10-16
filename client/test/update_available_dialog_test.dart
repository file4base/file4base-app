import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/server_tools/update_available_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  group('UpdateAvailableDialog', () {
    testWidgets('shows the offer and, on confirm, surfaces the deploy command', (tester) async {
      tester.view.physicalSize = const Size(900, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      var updateCalled = false;
      final mockClient = MockClient((request) async {
        if (request.url.path == '/api/v1/server/containers/update' && request.method == 'POST') {
          updateCalled = true;
          return http.Response(
            jsonEncode({
              'status': 'updated',
              'updated_version': '0.16.0',
              'pre_update_backup': 'bkp_sales_20261016.f4b.json',
              'docker_pull_command': 'docker compose pull && docker compose up -d',
            }),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        return http.Response('Not found', 404);
      });
      final apiClient = ApiClient(baseUrl: 'http://test-server:8080', httpClient: mockClient);

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (ctx) => ElevatedButton(
                onPressed: () => UpdateAvailableDialog.show(
                  ctx,
                  apiClient,
                  currentVersion: '0.15.2',
                  latestVersion: '0.16.0',
                  releaseNotes: 'Adds the update notice system.',
                  releaseUrl: 'https://github.com/file4base/file4base-app/releases/tag/v0.16.0',
                  activeDatabase: 'sales',
                ),
                child: const Text('open'),
              ),
            ),
          ),
        ),
      );

      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();

      // The offer shows both versions and the release notes.
      expect(find.text('A new version is available'), findsOneWidget);
      expect(find.text('v0.15.2'), findsOneWidget);
      expect(find.text('v0.16.0'), findsOneWidget);
      expect(find.textContaining('Adds the update notice system.'), findsOneWidget);
      expect(find.text('Deploy update'), findsOneWidget);

      // Confirming calls the update endpoint and shows the host command.
      await tester.tap(find.text('Deploy update'));
      await tester.pumpAndSettle();

      expect(updateCalled, isTrue);
      expect(find.textContaining('docker compose pull && docker compose up -d'), findsOneWidget);
      expect(find.textContaining('bkp_sales_20261016.f4b.json'), findsOneWidget);
      expect(find.byIcon(Icons.copy), findsOneWidget);
      expect(find.text('Done'), findsOneWidget);
    });

    testWidgets('a failed deploy shows the error and keeps the offer', (tester) async {
      tester.view.physicalSize = const Size(900, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      final mockClient = MockClient((request) async {
        if (request.url.path == '/api/v1/server/containers/update') {
          return http.Response(
            jsonEncode({'title': 'Forbidden', 'detail': 'only a super-owner can perform this operation', 'status': 403}),
            403,
            headers: {'content-type': 'application/problem+json'},
          );
        }
        return http.Response('Not found', 404);
      });
      final apiClient = ApiClient(baseUrl: 'http://test-server:8080', httpClient: mockClient);

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (ctx) => ElevatedButton(
                onPressed: () => UpdateAvailableDialog.show(
                  ctx,
                  apiClient,
                  currentVersion: '0.15.2',
                  latestVersion: '0.16.0',
                ),
                child: const Text('open'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Deploy update'));
      await tester.pumpAndSettle();

      expect(find.textContaining('super-owner'), findsOneWidget);
      // Still on the offer screen (can retry or dismiss).
      expect(find.text('Deploy update'), findsOneWidget);
    });
  });
}
