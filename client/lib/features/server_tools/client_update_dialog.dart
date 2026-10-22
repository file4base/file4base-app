import 'dart:async';
import 'dart:io' show File;

import 'package:file4base_client/core/services/desktop_update_service.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

/// Offers an update of the **installed desktop client** and carries it out:
/// download, SHA-256 verification, then handing the file to the platform's own
/// installer.
///
/// Deliberately not the same dialog as `UpdateAvailableDialog`: that one
/// updates the container stack and ends in a shell command for an operator,
/// while this one must work for someone who only ever sees a program. Nothing
/// here asks for a terminal.
class ClientUpdateDialog extends StatefulWidget {
  const ClientUpdateDialog({
    required this.info,
    this.service,
    super.key,
  });

  final DesktopUpdateInfo info;

  /// Injectable for tests; a real run creates its own service.
  final DesktopUpdateService? service;

  static Future<void> show(
    BuildContext context,
    DesktopUpdateInfo info, {
    DesktopUpdateService? service,
  }) {
    return showDialog<void>(
      context: context,
      barrierDismissible: true,
      builder: (_) => ClientUpdateDialog(info: info, service: service),
    );
  }

  @override
  State<ClientUpdateDialog> createState() => _ClientUpdateDialogState();
}

enum _Stage { offer, downloading, verified, failed }

class _ClientUpdateDialogState extends State<ClientUpdateDialog> {
  late final DesktopUpdateService _service =
      widget.service ?? DesktopUpdateService();

  _Stage _stage = _Stage.offer;
  int _received = 0;
  int _total = 0;
  String? _error;
  File? _downloaded;

  @override
  void dispose() {
    // Only close a service this dialog created; an injected one outlives it.
    if (widget.service == null) {
      _service.close();
    }
    super.dispose();
  }

  double? get _progress {
    if (_total <= 0) return null;
    return (_received / _total).clamp(0.0, 1.0);
  }

  Future<void> _startDownload() async {
    setState(() {
      _stage = _Stage.downloading;
      _error = null;
      _received = 0;
      _total = widget.info.sizeBytes;
    });
    try {
      final file = await _service.download(
        widget.info,
        onProgress: (received, total) {
          if (!mounted) return;
          setState(() {
            _received = received;
            _total = total;
          });
        },
      );
      if (!mounted) return;
      setState(() {
        _downloaded = file;
        _stage = _Stage.verified;
      });
    } on ChecksumMismatchException catch (e) {
      if (!mounted) return;
      setState(() {
        _stage = _Stage.failed;
        _error = '$e The file was discarded. Download it from the release page '
            'instead.';
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _stage = _Stage.failed;
        _error = 'The download did not finish: $e';
      });
    }
  }

  Future<void> _install() async {
    final file = _downloaded;
    if (file == null) return;
    try {
      await _service.launchInstaller(file);
      if (!mounted) return;
      Navigator.of(context).pop();
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _stage = _Stage.failed;
        _error = 'Could not start the installer: $e';
      });
    }
  }

  Future<void> _openReleasePage() async {
    final url = widget.info.releaseUrl.trim();
    if (url.isEmpty) return;
    final uri = Uri.tryParse(url);
    if (uri == null) return;
    await launchUrl(uri, mode: LaunchMode.externalApplication);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AlertDialog(
      icon: const Icon(Icons.system_update_alt, color: Color(0xFF1E88E5), size: 28),
      title: Text(_titleForStage()),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 520),
        child: SingleChildScrollView(child: _buildBody(theme)),
      ),
      actions: _buildActions(),
    );
  }

  String _titleForStage() {
    switch (_stage) {
      case _Stage.offer:
        return 'A new version of File4Base is available';
      case _Stage.downloading:
        return 'Downloading File4Base v${widget.info.latestVersion}';
      case _Stage.verified:
        return 'Ready to install';
      case _Stage.failed:
        return 'The update could not be downloaded';
    }
  }

  Widget _buildBody(ThemeData theme) {
    switch (_stage) {
      case _Stage.offer:
        return _buildOffer(theme);
      case _Stage.downloading:
        return _buildDownloading(theme);
      case _Stage.verified:
        return _buildVerified(theme);
      case _Stage.failed:
        return _buildFailed(theme);
    }
  }

  Widget _buildOffer(ThemeData theme) {
    final info = widget.info;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            _versionChip(
              info.clientVersion,
              'Installed',
              theme.colorScheme.surfaceContainerHighest,
              theme.colorScheme.onSurface,
            ),
            const Padding(
              padding: EdgeInsets.symmetric(horizontal: 10),
              child: Icon(Icons.arrow_forward, size: 18),
            ),
            _versionChip(info.latestVersion, 'Available', const Color(0xFF1E88E5), Colors.white),
          ],
        ),
        const SizedBox(height: 16),
        if (info.releaseNotes.trim().isNotEmpty) ...[
          Text("What's new", style: theme.textTheme.labelLarge),
          const SizedBox(height: 4),
          Container(
            width: double.infinity,
            constraints: const BoxConstraints(maxHeight: 180),
            padding: const EdgeInsets.all(10),
            decoration: BoxDecoration(
              color: theme.colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(6),
            ),
            child: SingleChildScrollView(
              child: SelectableText(
                info.releaseNotes.trim(),
                style: theme.textTheme.bodySmall,
              ),
            ),
          ),
          const SizedBox(height: 12),
        ],
        if (info.canInstallInApp)
          Text(
            'File4Base will download the installer'
            '${info.sizeLabel.isNotEmpty ? " (${info.sizeLabel})" : ""}, check it '
            'against its published checksum, and then open it. Your databases '
            'and solutions are not touched.',
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          )
        else
          Text(
            'This release has no verified installer for this platform yet, so '
            'the download is not offered inside the app. The release page has '
            'every file.',
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
      ],
    );
  }

  Widget _buildDownloading(ThemeData theme) {
    final pct = _progress;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(widget.info.assetName, style: theme.textTheme.bodySmall),
        const SizedBox(height: 10),
        LinearProgressIndicator(value: pct),
        const SizedBox(height: 8),
        Text(
          pct == null
              ? '${_mib(_received)} MB downloaded'
              : '${_mib(_received)} / ${_mib(_total)} MB  •  ${(pct * 100).toStringAsFixed(0)}%',
          style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
        ),
      ],
    );
  }

  Widget _buildVerified(ThemeData theme) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            const Icon(Icons.verified_user, size: 18, color: Color(0xFF2E7D32)),
            const SizedBox(width: 6),
            Expanded(
              child: Text(
                'The download matches its published SHA-256 checksum.',
                style: theme.textTheme.bodyMedium,
              ),
            ),
          ],
        ),
        const SizedBox(height: 12),
        Text(
          'Installing closes this window and opens the File4Base installer. '
          'Quit File4Base when the installer asks you to, then start the new '
          'version.',
          style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
        ),
      ],
    );
  }

  Widget _buildFailed(ThemeData theme) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: theme.colorScheme.errorContainer,
        borderRadius: BorderRadius.circular(6),
      ),
      child: Text(
        _error ?? 'Unknown error.',
        style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onErrorContainer),
      ),
    );
  }

  List<Widget> _buildActions() {
    switch (_stage) {
      case _Stage.offer:
        return [
          if (widget.info.releaseUrl.trim().isNotEmpty)
            TextButton(
              onPressed: _openReleasePage,
              child: const Text('Release page'),
            ),
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('Later'),
          ),
          if (widget.info.canInstallInApp)
            FilledButton.icon(
              onPressed: _startDownload,
              icon: const Icon(Icons.download, size: 18),
              label: const Text('Download and install'),
            ),
        ];
      case _Stage.downloading:
        return [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('Cancel'),
          ),
        ];
      case _Stage.verified:
        return [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('Later'),
          ),
          FilledButton.icon(
            key: const Key('client_update_install_button'),
            onPressed: _install,
            icon: const Icon(Icons.rocket_launch, size: 18),
            label: const Text('Install now'),
          ),
        ];
      case _Stage.failed:
        return [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('Close'),
          ),
          if (widget.info.releaseUrl.trim().isNotEmpty)
            FilledButton(
              onPressed: _openReleasePage,
              child: const Text('Open release page'),
            ),
        ];
    }
  }

  static String _mib(int bytes) => (bytes / (1024 * 1024)).toStringAsFixed(1);

  Widget _versionChip(String version, String label, Color bg, Color fg) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          label,
          style: TextStyle(fontSize: 10, color: Theme.of(context).colorScheme.onSurfaceVariant),
        ),
        const SizedBox(height: 2),
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
          decoration: BoxDecoration(color: bg, borderRadius: BorderRadius.circular(6)),
          child: Text(
            'v$version',
            style: TextStyle(fontWeight: FontWeight.bold, color: fg),
          ),
        ),
      ],
    );
  }
}
