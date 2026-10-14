import 'dart:convert';
import 'dart:io';

import 'package:file4base_client/core/system/environment_checker.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('EnvironmentChecker Tests', () {
    test('checkPlatformArchitecture returns satisfied', () async {
      final arch = await EnvironmentChecker.checkPlatformArchitecture();
      expect(arch.status, RequirementStatus.satisfied);
      expect(arch.title, 'Target Architecture');
    });

    test('checkDocker in test mode returns satisfied', () async {
      EnvironmentChecker.isTestMode = true;
      final docker = await EnvironmentChecker.checkDocker();
      expect(docker.status, RequirementStatus.satisfied);
      expect(docker.detail, 'Connecting to remote/local server');
      EnvironmentChecker.isTestMode = false;
    });

    test('resolveDockerExecutable returns null or valid executable path', () async {
      final exe = await EnvironmentChecker.resolveDockerExecutable();
      if (exe != null) {
        expect(File(exe).existsSync(), isTrue);
      }
    });

    test('checkDocker fast-path succeeds when server returns 200 health', () async {
      // Spawn a lightweight local HTTP server simulating File4Base /healthz
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      server.listen((request) {
        if (request.uri.path == '/healthz') {
          request.response
            ..statusCode = HttpStatus.ok
            ..headers.contentType = ContentType.json
            ..write(jsonEncode({
              'status': 'pass',
              'engine': 'postgres',
              'database': 'connected',
              'uptime_seconds': 120,
            }))
            ..close();
        } else {
          request.response
            ..statusCode = HttpStatus.notFound
            ..close();
        }
      });

      try {
        final serverUrl = 'http://localhost:${server.port}';
        final req = await EnvironmentChecker.checkDocker(serverUrl: serverUrl);
        expect(req.status, RequirementStatus.satisfied);
        expect(req.title, 'File4Base Server & Database');
        expect(req.description, contains('postgres'));
        expect(req.detail, contains('Healthy connection'));
      } finally {
        await server.close();
      }
    });

    test('checkDocker fast-path succeeds when server returns 200 health with MariaDB', () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      server.listen((request) {
        if (request.uri.path == '/healthz') {
          request.response
            ..statusCode = HttpStatus.ok
            ..headers.contentType = ContentType.json
            ..write(jsonEncode({
              'status': 'pass',
              'engine': 'mariadb',
              'database': 'connected',
              'uptime_seconds': 45,
            }))
            ..close();
        } else {
          request.response
            ..statusCode = HttpStatus.notFound
            ..close();
        }
      });

      try {
        final serverUrl = 'http://localhost:${server.port}';
        final req = await EnvironmentChecker.checkDocker(serverUrl: serverUrl);
        expect(req.status, RequirementStatus.satisfied);
        expect(req.description, contains('mariadb'));
      } finally {
        await server.close();
      }
    });

    test('checkProjectContainers returns valid state structure', () async {
      final status = await EnvironmentChecker.checkProjectContainers();
      expect(status.description, isNotEmpty);
    });
  });
}
