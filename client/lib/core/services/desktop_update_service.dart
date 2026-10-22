import 'dart:async';
import 'dart:convert' show ChunkedConversionSink;
import 'dart:io' show Directory, File, Platform, Process;

import 'package:crypto/crypto.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:http/http.dart' as http;

export 'package:file4base_client/core/models/client_update_policy.dart';

/// What the server knows about the newest published desktop client.
class DesktopUpdateInfo {
  const DesktopUpdateInfo({
    required this.platform,
    required this.clientVersion,
    required this.latestVersion,
    required this.updateAvailable,
    this.assetName = '',
    this.downloadUrl = '',
    this.sha256 = '',
    this.sizeBytes = 0,
    this.releaseUrl = '',
    this.releaseNotes = '',
  });

  factory DesktopUpdateInfo.fromJson(Map<String, dynamic> json) {
    return DesktopUpdateInfo(
      platform: json['platform']?.toString() ?? '',
      clientVersion: json['client_version']?.toString() ?? '',
      latestVersion: json['latest_version']?.toString() ?? '',
      updateAvailable: json['update_available'] == true,
      assetName: json['asset_name']?.toString() ?? '',
      downloadUrl: json['download_url']?.toString() ?? '',
      sha256: (json['sha256']?.toString() ?? '').toLowerCase(),
      sizeBytes: (json['size_bytes'] as num?)?.toInt() ?? 0,
      releaseUrl: json['release_url']?.toString() ?? '',
      releaseNotes: json['release_notes']?.toString() ?? '',
    );
  }

  final String platform;
  final String clientVersion;
  final String latestVersion;
  final bool updateAvailable;
  final String assetName;
  final String downloadUrl;
  final String sha256;
  final int sizeBytes;
  final String releaseUrl;
  final String releaseNotes;

  /// Whether this update can be installed from inside the app.
  ///
  /// A download is only offered together with its SHA-256: without a digest to
  /// check the bytes against, the app sends the user to the release page rather
  /// than run an unverified installer.
  bool get canInstallInApp =>
      updateAvailable && downloadUrl.isNotEmpty && sha256.length == 64;

  /// Human-readable download size, empty when the server did not report one.
  String get sizeLabel {
    if (sizeBytes <= 0) return '';
    const mib = 1024 * 1024;
    if (sizeBytes >= mib) {
      return '${(sizeBytes / mib).toStringAsFixed(1)} MB';
    }
    return '${(sizeBytes / 1024).toStringAsFixed(0)} KB';
  }
}

/// Raised when a download does not match the digest the server published.
class ChecksumMismatchException implements Exception {
  const ChecksumMismatchException({required this.expected, required this.actual});

  final String expected;
  final String actual;

  @override
  String toString() =>
      'The download does not match the published checksum (expected $expected, got $actual).';
}

/// Checks for, downloads and verifies updates of the **installed desktop
/// client**, then hands the verified file to the platform's own installer.
///
/// This is the counterpart of the container-stack update: `docker compose pull`
/// replaces the API and the Web Client, but never the natively compiled binary
/// a user installed from a .dmg, a Setup.exe or a .deb. The web build updates
/// itself by reloading (see `web/index.html`), so everything here is a no-op
/// when [isSupported] is false.
class DesktopUpdateService {
  DesktopUpdateService({http.Client? downloadClient})
      : _downloadClient = downloadClient ?? http.Client();

  /// Plain client for the release host: the download is an external asset, not
  /// an API call, so it must not go through the encrypted API channel.
  final http.Client _downloadClient;

  /// Whether this build can update itself in place. False on web, where a
  /// reload is the update.
  static bool get isSupported {
    if (kIsWeb) return false;
    return Platform.isMacOS || Platform.isWindows || Platform.isLinux;
  }

  /// The platform key the update endpoint expects.
  static String get platformKey {
    if (kIsWeb) return 'web';
    if (Platform.isMacOS) return 'macos';
    if (Platform.isWindows) return 'windows';
    if (Platform.isLinux) return 'linux';
    return Platform.operatingSystem;
  }

  /// Asks the server what the newest published client is.
  ///
  /// Returns null when this build cannot update itself, and rethrows nothing:
  /// a failed check is not worth interrupting anyone over, so callers get null.
  Future<DesktopUpdateInfo?> check(
    ApiClient api, {
    required String currentVersion,
  }) async {
    if (!isSupported) return null;
    try {
      final raw = await api.fetchDesktopUpdate(
        platform: platformKey,
        clientVersion: currentVersion,
      );
      return DesktopUpdateInfo.fromJson(raw);
    } catch (_) {
      return null;
    }
  }

  /// Downloads [info]'s asset to a private temporary directory, hashing it as
  /// it arrives, and returns the file only once the digest matches.
  ///
  /// [onProgress] receives (received, total); total is 0 while unknown.
  /// Throws [ChecksumMismatchException] on a mismatch, after deleting the file.
  Future<File> download(
    DesktopUpdateInfo info, {
    void Function(int received, int total)? onProgress,
  }) async {
    if (!info.canInstallInApp) {
      throw StateError('This update has no verifiable download.');
    }

    final dir = await Directory.systemTemp.createTemp('file4base-update-');
    final target = File('${dir.path}/${_safeFileName(info.assetName)}');

    final request = http.Request('GET', Uri.parse(info.downloadUrl));
    final response = await _downloadClient.send(request);
    if (response.statusCode != 200) {
      await dir.delete(recursive: true);
      throw http.ClientException(
        'The release server answered ${response.statusCode}.',
        Uri.parse(info.downloadUrl),
      );
    }

    final total = response.contentLength ?? info.sizeBytes;
    var received = 0;
    var digest = '';
    final sink = target.openWrite();
    // The digest is computed from the same bytes that reach the disk, so a
    // truncated or tampered download cannot pass verification.
    final hashSink = sha256.startChunkedConversion(
      ChunkedConversionSink<Digest>.withCallback(
        (digests) => digest = digests.single.toString(),
      ),
    );

    try {
      await for (final chunk in response.stream) {
        sink.add(chunk);
        hashSink.add(chunk);
        received += chunk.length;
        onProgress?.call(received, total);
      }
      await sink.flush();
    } catch (_) {
      // A download that was interrupted (or cancelled by closing the client)
      // leaves no partial installer behind for anyone to run.
      await sink.close();
      hashSink.close();
      await dir.delete(recursive: true);
      rethrow;
    }
    await sink.close();
    hashSink.close();

    if (digest != info.sha256) {
      await dir.delete(recursive: true);
      throw ChecksumMismatchException(expected: info.sha256, actual: digest);
    }
    return target;
  }

  /// Hands a verified download to the platform's own installer.
  ///
  /// Nothing is installed silently: each platform opens its normal installer UI
  /// (the mounted .dmg, the Setup wizard, the system package installer) and the
  /// user confirms there, which is also what keeps the app from needing
  /// elevated rights of its own.
  Future<void> launchInstaller(File downloaded) async {
    final path = downloaded.path;
    if (Platform.isMacOS) {
      await Process.start('/usr/bin/open', <String>[path]);
      return;
    }
    if (Platform.isWindows) {
      if (path.toLowerCase().endsWith('.exe')) {
        await Process.start(path, const <String>[]);
      } else {
        await Process.start('cmd', <String>['/c', 'start', '', path]);
      }
      return;
    }
    // Linux: let the desktop environment route the .deb / .tar.gz to whatever
    // handles it (the software centre, the archive manager).
    await Process.start('xdg-open', <String>[path]);
  }

  void close() => _downloadClient.close();

  /// Keeps a release asset name usable as a file name on every target.
  static String _safeFileName(String name) {
    final cleaned = name.replaceAll(RegExp('[^A-Za-z0-9._-]'), '_');
    return cleaned.isEmpty ? 'file4base-update' : cleaned;
  }
}
