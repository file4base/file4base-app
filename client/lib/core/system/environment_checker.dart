import 'dart:convert' show jsonDecode, utf8;
import 'dart:io' show Directory, File, HttpClient, Link, Platform, Process;
import 'package:flutter/foundation.dart' show kIsWeb;

enum RequirementStatus {
  checking,
  satisfied,
  missing,
  daemonNotRunning,
  error,
}

class SystemRequirement {

  const SystemRequirement({
    required this.title,
    required this.description,
    required this.status,
    this.detail,
    this.actionLabel,
    this.downloadUrl,
  });
  final String title;
  final String description;
  final RequirementStatus status;
  final String? detail;
  final String? actionLabel;
  final String? downloadUrl;

  SystemRequirement copyWith({
    String? title,
    String? description,
    RequirementStatus? status,
    String? detail,
    String? actionLabel,
    String? downloadUrl,
  }) {
    return SystemRequirement(
      title: title ?? this.title,
      description: description ?? this.description,
      status: status ?? this.status,
      detail: detail ?? this.detail,
      actionLabel: actionLabel ?? this.actionLabel,
      downloadUrl: downloadUrl ?? this.downloadUrl,
    );
  }
}

class EnvironmentChecker {
  static bool isTestMode = false;

  /// Resolves the docker CLI executable location across macOS, Linux, and Windows.
  static Future<String?> resolveDockerExecutable() async {
    if (kIsWeb) return null;
    final whichCmd = Platform.isWindows ? 'where' : 'which';
    try {
      final res = await Process.run(whichCmd, ['docker']);
      if (res.exitCode == 0) {
        final line = res.stdout.toString().split('\n').first.trim();
        if (line.isNotEmpty && (File(line).existsSync() || Link(line).existsSync())) return line;
      }
    } catch (_) {}

    final homeDir = Platform.environment['HOME'] ?? '';
    final candidates = [
      if (Platform.isMacOS) ...[
        '/usr/local/bin/docker',
        '/opt/homebrew/bin/docker',
        '/Applications/Docker.app/Contents/Resources/bin/docker',
        if (homeDir.isNotEmpty) '$homeDir/.docker/bin/docker',
        '/usr/bin/docker',
      ],
      if (Platform.isLinux) ...[
        '/usr/bin/docker',
        '/usr/local/bin/docker',
        '/snap/bin/docker',
        if (homeDir.isNotEmpty) '$homeDir/.docker/bin/docker',
      ],
      if (Platform.isWindows) ...[
        r'C:\Program Files\Docker\Docker\resources\bin\docker.exe',
        r'C:\Program Files\Docker\Docker\DockerCli.exe',
      ],
    ];

    for (final path in candidates) {
      try {
        if (File(path).existsSync() || Link(path).existsSync()) {
          return path;
        }
      } catch (_) {}
    }
    return null;
  }

  /// Checks whether Docker Desktop app or daemon socket exists on the machine.
  static bool isDockerAppInstalled() {
    if (kIsWeb) return false;
    final homeDir = Platform.environment['HOME'] ?? '';
    if (Platform.isMacOS) {
      return Directory('/Applications/Docker.app').existsSync() ||
          File('/var/run/docker.sock').existsSync() ||
          Link('/var/run/docker.sock').existsSync() ||
          (homeDir.isNotEmpty && File('$homeDir/.docker/run/docker.sock').existsSync());
    } else if (Platform.isWindows) {
      return Directory(r'C:\Program Files\Docker\Docker').existsSync();
    } else if (Platform.isLinux) {
      return File('/var/run/docker.sock').existsSync() ||
          Link('/var/run/docker.sock').existsSync();
    }
    return false;
  }

  static Future<SystemRequirement> checkDocker({String? serverUrl}) async {
    if (isTestMode || kIsWeb) {
      return const SystemRequirement(
        title: 'Docker Engine & CLI',
        description: 'Docker check bypassed (Web/Test Mode)',
        status: RequirementStatus.satisfied,
        detail: 'Connecting to remote/local server',
      );
    }

    // 1. Fast-path: Check if backend server is already reachable and healthy!
    // If the server and database are already up and running (e.g. in Docker Compose or remote),
    // there is no need to block the user or require host-level Docker CLI access.
    final targetUrl = serverUrl ?? 'http://localhost:8080';
    try {
      final uri = Uri.parse('$targetUrl/healthz');
      final client = HttpClient()..connectionTimeout = const Duration(seconds: 2);
      final request = await client.getUrl(uri);
      final response = await request.close();
      if (response.statusCode == 200) {
        final body = await response.transform(utf8.decoder).join();
        client.close();
        final data = jsonDecode(body) as Map<String, dynamic>;
        final engine = data['engine']?.toString() ?? 'database';
        final dbStatus = data['database']?.toString() ?? 'connected';
        return SystemRequirement(
          title: 'File4Base Server & Database',
          description: 'Backend services and database engine ($engine) are active.',
          status: RequirementStatus.satisfied,
          detail: 'Healthy connection ($dbStatus) at $targetUrl',
        );
      }
      client.close();
    } catch (_) {
      // Backend not yet reachable via HTTP; proceed to check Docker engine & CLI
    }

    // 2. Resolve Docker executable path
    final dockerExe = await resolveDockerExecutable();
    if (dockerExe == null) {
      if (isDockerAppInstalled()) {
        return SystemRequirement(
          title: 'Docker Desktop',
          description: 'Docker Desktop is installed on this computer.',
          status: RequirementStatus.daemonNotRunning,
          detail: 'Docker app found. Click below to launch Docker Desktop and start containers.',
          actionLabel: 'Launch Docker Desktop',
          downloadUrl: _getDockerDownloadUrl(),
        );
      }
      return SystemRequirement(
        title: 'Docker Engine & CLI',
        description: 'Docker is required to run the local PostgreSQL/MariaDB databases and services.',
        status: RequirementStatus.missing,
        detail: 'Docker executable not found in PATH or standard install locations.',
        actionLabel: 'Download Docker Desktop',
        downloadUrl: _getDockerDownloadUrl(),
      );
    }

    // 3. Verify Docker daemon is running
    try {
      final pingResult = await Process.run(dockerExe, ['info']);
      if (pingResult.exitCode != 0) {
        return SystemRequirement(
          title: 'Docker Engine & CLI',
          description: 'Docker is installed, but the Docker daemon / Docker Desktop is not currently running.',
          status: RequirementStatus.daemonNotRunning,
          detail: 'Please start Docker Desktop or the dockerd service.',
          actionLabel: 'Launch Docker Desktop',
          downloadUrl: _getDockerDownloadUrl(),
        );
      }

      // Check if project containers are already running
      final containerStatus = await checkProjectContainers(dockerExe: dockerExe);
      if (containerStatus.isRunning) {
        return SystemRequirement(
          title: 'Docker Engine & Containers',
          description: 'Docker and File4Base services are active.',
          status: RequirementStatus.satisfied,
          detail: containerStatus.description,
        );
      }

      final versionResult = await Process.run(dockerExe, ['--version']);
      final versionText = versionResult.stdout.toString().trim();

      return SystemRequirement(
        title: 'Docker Engine & CLI',
        description: 'Docker is installed and running.',
        status: RequirementStatus.satisfied,
        detail: versionText.isNotEmpty ? versionText : 'Docker active ($dockerExe)',
      );
    } catch (e) {
      return SystemRequirement(
        title: 'Docker Engine & CLI',
        description: 'Error verifying Docker installation: $e',
        status: RequirementStatus.error,
        detail: e.toString(),
        actionLabel: 'Download Docker Desktop',
        downloadUrl: _getDockerDownloadUrl(),
      );
    }
  }

  /// Checks if any File4Base database (postgres or mariadb) and api/web containers are running.
  static Future<({bool isRunning, String description})> checkProjectContainers({String? dockerExe}) async {
    if (kIsWeb) return (isRunning: true, description: 'Web client');
    try {
      final exe = dockerExe ?? await resolveDockerExecutable();
      if (exe == null) return (isRunning: false, description: 'Docker CLI not found');

      final res = await Process.run(exe, ['ps', '--format', '{{.Names}}']);
      if (res.exitCode != 0) return (isRunning: false, description: 'Cannot query containers');

      final names = res.stdout.toString().split('\n').map((e) => e.trim()).where((e) => e.isNotEmpty).toList();
      final hasPostgres = names.contains('file4base-postgres');
      final hasMariaDb = names.contains('file4base-mariadb');
      final hasApi = names.contains('file4base-api');
      final hasWeb = names.contains('file4base-web');

      // Either Postgres OR MariaDB is sufficient as the database engine!
      final hasDb = hasPostgres || hasMariaDb;
      final activeDbName = hasPostgres ? 'PostgreSQL' : (hasMariaDb ? 'MariaDB' : 'None');

      if (hasDb && hasApi) {
        final webNote = hasWeb ? ', Web' : '';
        return (
          isRunning: true,
          description: 'Containers running ($activeDbName, API$webNote)',
        );
      } else if (hasDb) {
        return (
          isRunning: true,
          description: 'Database container running ($activeDbName)',
        );
      }
      return (isRunning: false, description: 'No File4Base containers running');
    } catch (e) {
      return (isRunning: false, description: e.toString());
    }
  }

  static Future<SystemRequirement> checkPlatformArchitecture() async {
    if (isTestMode) {
      return const SystemRequirement(
        title: 'Target Architecture',
        description: 'Test Platform',
        status: RequirementStatus.satisfied,
        detail: 'Active',
      );
    }

    if (kIsWeb) {
      return const SystemRequirement(
        title: 'Target Architecture',
        description: 'Web Browser Client',
        status: RequirementStatus.satisfied,
        detail: 'Connected via HTTP/WebSocket',
      );
    }

    final os = Platform.operatingSystem;
    final arch = _getMacAppleSiliconOrArchitecture();

    return SystemRequirement(
      title: 'Target Architecture',
      description: 'Host OS: $os ($arch)',
      status: RequirementStatus.satisfied,
      detail: 'Native desktop support active.',
    );
  }

  static String _getMacAppleSiliconOrArchitecture() {
    if (kIsWeb) return 'Web';
    if (Platform.isMacOS) {
      try {
        final result = Process.runSync('sysctl', ['-n', 'machdep.cpu.brand_string']);
        final brand = result.stdout.toString().trim();
        if (brand.toLowerCase().contains('apple')) {
          return 'Apple Silicon ($brand)';
        }
        return brand.isNotEmpty ? brand : 'macOS arm64/x86_64';
      } catch (_) {
        return 'macOS';
      }
    }
    return Platform.operatingSystem;
  }

  static String _getDockerDownloadUrl() {
    if (kIsWeb) return 'https://www.docker.com/products/docker-desktop/';
    if (Platform.isMacOS) {
      final isAppleSilicon = _getMacAppleSiliconOrArchitecture().contains('Apple');
      return isAppleSilicon
          ? 'https://desktop.docker.com/mac/main/arm64/Docker.dmg'
          : 'https://desktop.docker.com/mac/main/amd64/Docker.dmg';
    } else if (Platform.isWindows) {
      return 'https://desktop.docker.com/win/main/amd64/Docker%20Desktop%20Installer.exe';
    } else {
      return 'https://docs.docker.com/engine/install/';
    }
  }

  /// Attempts to launch Docker Desktop application or start dockerd service.
  static Future<bool> startDockerDesktop() async {
    if (kIsWeb) return false;
    try {
      if (Platform.isMacOS) {
        await Process.run('open', ['-a', 'Docker']);
        return true;
      } else if (Platform.isWindows) {
        await Process.run('cmd', ['/c', 'start', '', r'C:\Program Files\Docker\Docker\Docker Desktop.exe']);
        return true;
      } else if (Platform.isLinux) {
        await Process.run('systemctl', ['start', 'docker']);
        return true;
      }
    } catch (_) {}
    return false;
  }

  /// Polls for the Docker daemon to become responsive up to maxWaitSeconds.
  static Future<bool> waitForDockerDaemon({int maxWaitSeconds = 30}) async {
    if (kIsWeb) return false;
    final dockerExe = await resolveDockerExecutable();
    if (dockerExe == null) return false;

    final stopwatch = Stopwatch()..start();
    while (stopwatch.elapsed.inSeconds < maxWaitSeconds) {
      try {
        final res = await Process.run(dockerExe, ['info']);
        if (res.exitCode == 0) return true;
      } catch (_) {}
      await Future<void>.delayed(const Duration(seconds: 2));
    }
    return false;
  }

  /// Launches the project's docker compose services (postgres, api, web).
  /// Either PostgreSQL or MariaDB can be used. Default starts postgres and api.
  static Future<bool> startProjectContainers({String? projectDir, bool withWeb = true}) async {
    if (kIsWeb) return false;
    try {
      final dockerExe = await resolveDockerExecutable();
      if (dockerExe == null) return false;

      // Locate project directory containing docker-compose.yml if not provided
      var targetDir = projectDir;
      if (targetDir == null) {
        final candidates = [
          Directory.current.path,
          '..',
          Platform.environment['FILE4BASE_HOME'],
        ];
        for (final c in candidates) {
          if (c != null && File('$c/docker-compose.yml').existsSync()) {
            targetDir = c;
            break;
          }
        }
      }

      final services = withWeb ? ['postgres', 'api', 'web'] : ['postgres', 'api'];
      final result = await Process.run(
        dockerExe,
        ['compose', 'up', '-d', ...services],
        workingDirectory: targetDir,
      );
      return result.exitCode == 0;
    } catch (_) {
      return false;
    }
  }

  /// Launches Docker Desktop, waits for the daemon to start, and runs the project containers.
  static Future<bool> startDockerAndContainers({
    void Function(String status)? onProgress,
    String? projectDir,
  }) async {
    if (kIsWeb) return false;
    onProgress?.call('Starting Docker Desktop...');
    await startDockerDesktop();

    onProgress?.call('Waiting for Docker daemon to initialize...');
    final isReady = await waitForDockerDaemon();
    if (!isReady) {
      onProgress?.call('Docker daemon did not respond within timeout.');
      return false;
    }

    onProgress?.call('Starting File4Base containers...');
    final started = await startProjectContainers(projectDir: projectDir);
    if (started) {
      onProgress?.call('File4Base containers launched successfully.');
      return true;
    } else {
      onProgress?.call('Could not start File4Base containers automatically.');
      return false;
    }
  }
}
