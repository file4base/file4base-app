import 'dart:io';

enum RequirementStatus {
  checking,
  satisfied,
  missing,
  daemonNotRunning,
  error,
}

class SystemRequirement {
  final String title;
  final String description;
  final RequirementStatus status;
  final String? detail;
  final String? actionLabel;
  final String? downloadUrl;

  const SystemRequirement({
    required this.title,
    required this.description,
    required this.status,
    this.detail,
    this.actionLabel,
    this.downloadUrl,
  });

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
  // Can be mocked or bypassed during automated widget testing
  static bool isTestMode = false;

  static Future<SystemRequirement> checkDocker() async {
    if (isTestMode) {
      return const SystemRequirement(
        title: 'Docker Engine & CLI',
        description: 'Test mode active',
        status: RequirementStatus.satisfied,
        detail: 'Bypassed for widget test',
      );
    }

    try {
      // 1. Check if docker CLI binary is in PATH
      final whichCmd = Platform.isWindows ? 'where' : 'which';
      final whichResult = await Process.run(whichCmd, ['docker']);

      if (whichResult.exitCode != 0) {
        return SystemRequirement(
          title: 'Docker Engine & CLI',
          description: 'Docker is required to run the local PostgreSQL/MariaDB databases and services.',
          status: RequirementStatus.missing,
          detail: 'Docker executable not found on your system.',
          actionLabel: 'Download Docker Desktop',
          downloadUrl: _getDockerDownloadUrl(),
        );
      }

      // 2. Check if Docker daemon is running
      final pingResult = await Process.run('docker', ['info']);
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

      // Extract Docker version
      final versionResult = await Process.run('docker', ['--version']);
      final versionText = versionResult.stdout.toString().trim();

      return SystemRequirement(
        title: 'Docker Engine & CLI',
        description: 'Docker is installed and running.',
        status: RequirementStatus.satisfied,
        detail: versionText.isNotEmpty ? versionText : 'Docker active',
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

  static Future<SystemRequirement> checkPlatformArchitecture() async {
    if (isTestMode) {
      return const SystemRequirement(
        title: 'Target Architecture',
        description: 'Test Platform',
        status: RequirementStatus.satisfied,
        detail: 'Active',
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

  static Future<bool> startDockerDesktop() async {
    try {
      if (Platform.isMacOS) {
        await Process.run('open', ['-a', 'Docker']);
        return true;
      } else if (Platform.isWindows) {
        await Process.run('cmd', ['/c', 'start', '', 'C:\\Program Files\\Docker\\Docker\\Docker Desktop.exe']);
        return true;
      } else if (Platform.isLinux) {
        await Process.run('systemctl', ['start', 'docker']);
        return true;
      }
    } catch (_) {}
    return false;
  }

  static Future<bool> startProjectContainers(String projectDir) async {
    try {
      final result = await Process.run(
        'docker',
        ['compose', 'up', '-d', 'postgres'],
        workingDirectory: projectDir,
      );
      return result.exitCode == 0;
    } catch (_) {
      return false;
    }
  }
}
