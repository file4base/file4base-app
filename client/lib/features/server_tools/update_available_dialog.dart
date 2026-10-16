import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/api/api_client.dart';

/// Notifies a server administrator that a newer File4Base version is available
/// and lets them confirm the deployment.
///
/// Because the API runs inside a container with no access to the host's Docker,
/// it cannot pull and recreate the stack itself. On confirmation it takes a
/// pre-update backup snapshot (server side) and returns the exact command the
/// operator runs on the host — shown here with a copy button.
class UpdateAvailableDialog extends StatefulWidget {
  const UpdateAvailableDialog({
    required this.apiClient,
    required this.currentVersion,
    required this.latestVersion,
    this.releaseNotes,
    this.releaseUrl,
    this.activeDatabase,
    super.key,
  });

  final ApiClient apiClient;
  final String currentVersion;
  final String latestVersion;
  final String? releaseNotes;
  final String? releaseUrl;
  final String? activeDatabase;

  static Future<void> show(
    BuildContext context,
    ApiClient apiClient, {
    required String currentVersion,
    required String latestVersion,
    String? releaseNotes,
    String? releaseUrl,
    String? activeDatabase,
  }) {
    return showDialog<void>(
      context: context,
      barrierDismissible: true,
      builder: (_) => UpdateAvailableDialog(
        apiClient: apiClient,
        currentVersion: currentVersion,
        latestVersion: latestVersion,
        releaseNotes: releaseNotes,
        releaseUrl: releaseUrl,
        activeDatabase: activeDatabase,
      ),
    );
  }

  @override
  State<UpdateAvailableDialog> createState() => _UpdateAvailableDialogState();
}

class _UpdateAvailableDialogState extends State<UpdateAvailableDialog> {
  bool _deploying = false;
  String? _error;
  // Populated after a successful confirmation.
  String? _deployCommand;
  String? _preUpdateBackup;
  String? _deployedVersion;

  bool get _done => _deployCommand != null;

  Future<void> _confirmDeploy() async {
    setState(() {
      _deploying = true;
      _error = null;
    });
    try {
      final res = await widget.apiClient.applyServerContainerUpdate(
        database: widget.activeDatabase,
      );
      if (!mounted) return;
      setState(() {
        _deployCommand = (res['docker_pull_command'] as String?)?.trim().isNotEmpty == true
            ? res['docker_pull_command'] as String
            : 'docker compose pull && docker compose up -d';
        _preUpdateBackup = res['pre_update_backup'] as String?;
        _deployedVersion = (res['updated_version'] as String?) ?? widget.latestVersion;
        _deploying = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _deploying = false;
        _error = e is ApiException
            ? e.detail
            : 'Could not start the update: $e';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AlertDialog(
      icon: const Icon(Icons.system_update_alt, color: Color(0xFFF59E0B), size: 28),
      title: Text(_done ? 'Finish the update on the host' : 'A new version is available'),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 520),
        child: _done ? _buildDeployResult(theme) : _buildOffer(theme),
      ),
      actions: _buildActions(),
    );
  }

  Widget _buildOffer(ThemeData theme) {
    return SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              _versionChip(widget.currentVersion, 'Current', theme.colorScheme.surfaceContainerHighest, theme.colorScheme.onSurface),
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 10),
                child: Icon(Icons.arrow_forward, size: 18),
              ),
              _versionChip(widget.latestVersion, 'Available', const Color(0xFFF59E0B), Colors.white),
            ],
          ),
          const SizedBox(height: 16),
          if ((widget.releaseNotes ?? '').trim().isNotEmpty) ...[
            Text('Release notes', style: theme.textTheme.labelLarge),
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
                  widget.releaseNotes!.trim(),
                  style: theme.textTheme.bodySmall,
                ),
              ),
            ),
            const SizedBox(height: 10),
          ],
          if ((widget.releaseUrl ?? '').trim().isNotEmpty) ...[
            Text('Release page', style: theme.textTheme.labelLarge),
            const SizedBox(height: 2),
            SelectableText(widget.releaseUrl!.trim(), style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.primary)),
            const SizedBox(height: 10),
          ],
          Text(
            'Confirming takes an automatic pre-update backup, then gives you the exact command to run on the host to pull and restart the containers. Nothing is changed until you run it.',
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
          if (_error != null) ...[
            const SizedBox(height: 10),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(8),
              decoration: BoxDecoration(
                color: theme.colorScheme.errorContainer,
                borderRadius: BorderRadius.circular(6),
              ),
              child: Text(_error!, style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onErrorContainer)),
            ),
          ],
        ],
      ),
    );
  }

  Widget _buildDeployResult(ThemeData theme) {
    return SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'A pre-update backup was taken. To finish updating to v$_deployedVersion, run this on the Docker host (where docker-compose.yml lives):',
            style: theme.textTheme.bodyMedium,
          ),
          const SizedBox(height: 10),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(10),
            decoration: BoxDecoration(
              color: theme.colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(6),
            ),
            child: Row(
              children: [
                Expanded(
                  child: SelectableText(
                    _deployCommand!,
                    style: const TextStyle(fontFamily: 'monospace', fontSize: 12.5),
                  ),
                ),
                IconButton(
                  tooltip: 'Copy command',
                  icon: const Icon(Icons.copy, size: 18),
                  onPressed: () {
                    Clipboard.setData(ClipboardData(text: _deployCommand!));
                    ScaffoldMessenger.of(context).showSnackBar(
                      const SnackBar(content: Text('Command copied'), duration: Duration(seconds: 2)),
                    );
                  },
                ),
              ],
            ),
          ),
          if ((_preUpdateBackup ?? '').isNotEmpty) ...[
            const SizedBox(height: 10),
            Text('Pre-update backup: $_preUpdateBackup',
                style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
          ],
          const SizedBox(height: 10),
          Text(
            'After it finishes, reload this page to pick up the new version.',
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        ],
      ),
    );
  }

  List<Widget> _buildActions() {
    if (_done) {
      return [
        FilledButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Done'),
        ),
      ];
    }
    return [
      TextButton(
        onPressed: _deploying ? null : () => Navigator.of(context).pop(),
        child: const Text('Later'),
      ),
      FilledButton.icon(
        onPressed: _deploying ? null : _confirmDeploy,
        icon: _deploying
            ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
            : const Icon(Icons.rocket_launch, size: 18),
        label: Text(_deploying ? 'Preparing…' : 'Deploy update'),
      ),
    ];
  }

  Widget _versionChip(String version, String label, Color bg, Color fg) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(label, style: TextStyle(fontSize: 10, color: Theme.of(context).colorScheme.onSurfaceVariant)),
        const SizedBox(height: 2),
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
          decoration: BoxDecoration(color: bg, borderRadius: BorderRadius.circular(6)),
          child: Text('v$version', style: TextStyle(fontWeight: FontWeight.bold, color: fg)),
        ),
      ],
    );
  }
}
