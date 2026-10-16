import 'dart:async';
import 'package:file4base_client/core/system/environment_checker.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';


class PreflightDialog extends StatefulWidget {

  const PreflightDialog({required this.onProceed, super.key, this.serverUrl});
  final VoidCallback onProceed;
  final String? serverUrl;

  static Future<void> showIfNeeded(
    BuildContext context, {
    required VoidCallback onProceed,
    String? serverUrl,
  }) async {
    final dockerReq = await EnvironmentChecker.checkDocker(serverUrl: serverUrl);
    final archReq = await EnvironmentChecker.checkPlatformArchitecture();

    // If requirements are satisfied, we can proceed automatically or show a quick status
    if (dockerReq.status == RequirementStatus.satisfied && archReq.status == RequirementStatus.satisfied) {
      onProceed();
      return;
    }

    if (context.mounted) {
      await showDialog(
        context: context,
        barrierDismissible: false,
        builder: (ctx) => PreflightDialog(onProceed: onProceed, serverUrl: serverUrl),
      );
    }
  }

  @override
  State<PreflightDialog> createState() => _PreflightDialogState();
}

class _PreflightDialogState extends State<PreflightDialog> {
  bool _isLoading = true;
  SystemRequirement? _dockerReq;
  SystemRequirement? _archReq;
  String? _actionMessage;

  @override
  void initState() {
    super.initState();
    unawaited(_checkRequirements());
  }

  Future<void> _checkRequirements() async {
    setState(() {
      _isLoading = true;
      _actionMessage = null;
    });

    final docker = await EnvironmentChecker.checkDocker(serverUrl: widget.serverUrl);
    final arch = await EnvironmentChecker.checkPlatformArchitecture();

    if (mounted) {
      setState(() {
        _dockerReq = docker;
        _archReq = arch;
        _isLoading = false;
      });
    }
  }

  Future<void> _handleDockerAction() async {
    if (_dockerReq == null) return;

    if (_dockerReq!.status == RequirementStatus.missing) {
      if (_dockerReq!.downloadUrl != null) {
        final uri = Uri.parse(_dockerReq!.downloadUrl!);
        if (await canLaunchUrl(uri)) {
          await launchUrl(uri);
        }
      }
    } else if (_dockerReq!.status == RequirementStatus.daemonNotRunning) {
      setState(() {
        _isLoading = true;
        _actionMessage = 'Starting Docker Desktop & File4Base containers...';
      });
      await EnvironmentChecker.startDockerAndContainers(
        onProgress: (status) {
          if (mounted) {
            setState(() {
              _actionMessage = status;
            });
          }
        },
      );
      await _checkRequirements();
    }
  }

  @override
  Widget build(BuildContext context) {
    final isSatisfied = _dockerReq?.status == RequirementStatus.satisfied;

    return AlertDialog(
      title: const Row(
        children: [
          Icon(Icons.health_and_safety, color: Color(0xFF1E88E5)),
          SizedBox(width: 8),
          Text('System Requirements Check'),
        ],
      ),
      content: SizedBox(
        width: 520,
        child: _isLoading
            ? const Padding(
                padding: EdgeInsets.all(24.0),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    CircularProgressIndicator(),
                    SizedBox(height: 12),
                    Text('Checking local environment and Docker status...'),
                  ],
                ),
              )
            : Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text(
                    'File4Base runs a local database engine (PostgreSQL/MariaDB) inside Docker to give you a complete, standalone low-code database experience.',
                    style: TextStyle(fontSize: 13),
                  ),
                  const SizedBox(height: 16),
                  if (_archReq != null) ...[
                    _buildRequirementTile(
                      icon: Icons.computer,
                      title: _archReq!.title,
                      description: '${_archReq!.description} - ${_archReq!.detail}',
                      isOk: true,
                    ),
                    const Divider(),
                  ],
                  if (_dockerReq != null) ...[
                    _buildRequirementTile(
                      icon: Icons.developer_board,
                      title: _dockerReq!.title,
                      description: _dockerReq!.description,
                      detail: _dockerReq!.detail,
                      isOk: isSatisfied,
                      isWarning: _dockerReq!.status == RequirementStatus.daemonNotRunning,
                    ),
                  ],
                  if (_actionMessage != null) ...[
                    const SizedBox(height: 8),
                    Text(
                      _actionMessage!,
                      style: const TextStyle(fontSize: 12, color: Colors.blue),
                    ),
                  ],
                ],
              ),
      ),
      actions: [
        TextButton(
          onPressed: _isLoading ? null : _checkRequirements,
          child: const Text('Re-check'),
        ),
        if (!isSatisfied && _dockerReq?.actionLabel != null)
          FilledButton.tonal(
            onPressed: _handleDockerAction,
            child: Text(_dockerReq!.actionLabel!),
          ),
        FilledButton(
          onPressed: () {
            Navigator.of(context).pop();
            widget.onProceed();
          },
          child: Text(isSatisfied ? 'Continue' : 'Proceed anyway'),
        ),
      ],
    );
  }

  Widget _buildRequirementTile({
    required IconData icon,
    required String title,
    required String description,
    required bool isOk, String? detail,
    bool isWarning = false,
  }) {
    final Color color = isOk ? Colors.green : (isWarning ? Colors.orange : Colors.red);
    final statusIcon = isOk ? Icons.check_circle : (isWarning ? Icons.warning : Icons.error);

    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, size: 28, color: Theme.of(context).colorScheme.primary),
        const SizedBox(width: 12),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(title, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 14)),
              const SizedBox(height: 2),
              Text(description, style: const TextStyle(fontSize: 12)),
              if (detail != null && detail.isNotEmpty) ...[
                const SizedBox(height: 2),
                Text(
                  detail,
                  style: TextStyle(fontSize: 11, color: color, fontWeight: FontWeight.w500),
                ),
              ],
            ],
          ),
        ),
        Icon(statusIcon, color: color, size: 20),
      ],
    );
  }
}
