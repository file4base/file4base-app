import 'package:flutter/material.dart';

import '../../core/api/api_client.dart';

/// Developer Utilities dialog (`Tools > Developer Utilities...`, #53).
///
/// Gives the owner of a solution server-backed utilities to prepare a copy for
/// distribution or a new environment:
/// 1. Clone the solution without records into a new database on the server.
/// 2. Rename the solution and update internal references to the old name.
/// 3. Configure Kiosk Mode (full-screen mode hiding toolbars/menus for standard users)
///    and AES-256-GCM solution package encryption.
/// 4. Permanently lock the solution's design (either on the clone or in place).
class DeveloperUtilitiesDialog extends StatefulWidget {
  final ApiClient apiClient;
  final SolutionMetaModel initialMeta;
  final ValueChanged<SolutionMetaModel>? onMetaChanged;
  final ValueChanged<CloneResultModel>? onSolutionCloned;
  final ValueChanged<bool>? onKioskModeChanged;

  const DeveloperUtilitiesDialog({
    super.key,
    required this.apiClient,
    required this.initialMeta,
    this.onMetaChanged,
    this.onSolutionCloned,
    this.onKioskModeChanged,
  });

  static Future<void> show(
    BuildContext context, {
    required ApiClient apiClient,
    required SolutionMetaModel initialMeta,
    ValueChanged<SolutionMetaModel>? onMetaChanged,
    ValueChanged<CloneResultModel>? onSolutionCloned,
    ValueChanged<bool>? onKioskModeChanged,
  }) {
    return showDialog<void>(
      context: context,
      builder: (_) => DeveloperUtilitiesDialog(
        apiClient: apiClient,
        initialMeta: initialMeta,
        onMetaChanged: onMetaChanged,
        onSolutionCloned: onSolutionCloned,
        onKioskModeChanged: onKioskModeChanged,
      ),
    );
  }

  @override
  State<DeveloperUtilitiesDialog> createState() =>
      _DeveloperUtilitiesDialogState();
}

class _DeveloperUtilitiesDialogState extends State<DeveloperUtilitiesDialog> {
  late SolutionMetaModel _meta;
  late final TextEditingController _renameCtrl;
  late final TextEditingController _cloneDbCtrl;
  late final TextEditingController _cloneNameCtrl;
  late final TextEditingController _encryptionPassphraseCtrl;
  bool _lockCloneDesign = false;
  bool _confirmLockCurrent = false;
  bool _kioskMode = false;
  bool _encryptionEnabled = true;
  bool _busy = false;
  String? _statusMessage;
  bool _isError = false;

  @override
  void initState() {
    super.initState();
    _meta = widget.initialMeta;
    _renameCtrl = TextEditingController(text: _meta.solutionName);
    _cloneDbCtrl = TextEditingController(text: '${_meta.database}_copy');
    _cloneNameCtrl = TextEditingController(text: '${_meta.solutionName} Copy');
    final opts = _meta.fileOptions ?? const <String, dynamic>{};
    _kioskMode = opts['kiosk_mode'] == true;
    _encryptionEnabled = opts['encryption_enabled'] != false;
    _encryptionPassphraseCtrl = TextEditingController(
      text: opts['encryption_hint'] as String? ?? '',
    );
  }

  @override
  void dispose() {
    _renameCtrl.dispose();
    _cloneDbCtrl.dispose();
    _cloneNameCtrl.dispose();
    _encryptionPassphraseCtrl.dispose();
    super.dispose();
  }

  Future<void> _renameSolution() async {
    final nextName = _renameCtrl.text.trim();
    if (nextName.isEmpty) {
      setState(() {
        _isError = true;
        _statusMessage = 'Enter a solution name before renaming.';
      });
      return;
    }
    setState(() {
      _busy = true;
      _statusMessage = null;
    });
    try {
      final updated = await widget.apiClient.renameSolution(nextName);
      if (!mounted) return;
      setState(() {
        _meta = updated;
        _busy = false;
        _isError = false;
        _statusMessage =
            'Solution renamed to "${updated.solutionName}". Layout and script references were updated.';
      });
      widget.onMetaChanged?.call(updated);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _isError = true;
        _statusMessage = e.toString();
      });
    }
  }

  Future<void> _cloneSolution() async {
    final targetDb = _cloneDbCtrl.text.trim().toLowerCase();
    final cloneName = _cloneNameCtrl.text.trim();
    if (targetDb.isEmpty) {
      setState(() {
        _isError = true;
        _statusMessage = 'Enter a target database name for the clone.';
      });
      return;
    }
    setState(() {
      _busy = true;
      _statusMessage = null;
    });
    try {
      final result = await widget.apiClient.cloneSolution(
        targetDatabase: targetDb,
        solutionName: cloneName.isEmpty ? targetDb : cloneName,
        lockDesign: _lockCloneDesign,
      );
      if (!mounted) return;
      setState(() {
        _busy = false;
        _isError = false;
        _statusMessage =
            'Created clone "${result.database}" (${result.tablesCreated} tables, 0 records${result.designLocked ? ', design locked' : ''}).';
      });
      widget.onSolutionCloned?.call(result);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _isError = true;
        _statusMessage = e.toString();
      });
    }
  }

  Future<void> _saveKioskAndEncryptionOptions() async {
    setState(() {
      _busy = true;
      _statusMessage = null;
    });
    final nextOpts = Map<String, dynamic>.from(_meta.fileOptions ?? const {});
    nextOpts['kiosk_mode'] = _kioskMode;
    nextOpts['encryption_enabled'] = _encryptionEnabled;
    nextOpts['encryption_cipher'] = 'AES-256-GCM';
    final hint = _encryptionPassphraseCtrl.text.trim();
    if (hint.isNotEmpty) {
      nextOpts['encryption_hint'] = hint;
    } else {
      nextOpts.remove('encryption_hint');
    }
    try {
      final updated = await widget.apiClient.updateSolutionOptions(
        fileOptions: nextOpts,
        pageSetup: _meta.pageSetup,
      );
      if (!mounted) return;
      setState(() {
        _meta = updated;
        _busy = false;
        _isError = false;
        _statusMessage =
            'Saved Kiosk Mode (${_kioskMode ? "enabled" : "disabled"}) and AES-256-GCM encryption (${_encryptionEnabled ? "enabled" : "disabled"}).';
      });
      widget.onMetaChanged?.call(updated);
      widget.onKioskModeChanged?.call(_kioskMode);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _isError = true;
        _statusMessage = e.toString();
      });
    }
  }

  Future<void> _lockCurrentDesign() async {
    if (!_confirmLockCurrent) return;
    setState(() {
      _busy = true;
      _statusMessage = null;
    });
    try {
      final updated = await widget.apiClient.lockSolutionDesign(confirm: true);
      if (!mounted) return;
      setState(() {
        _meta = updated;
        _busy = false;
        _isError = false;
        _statusMessage =
            'The design of "${updated.solutionName}" is now permanently locked.';
      });
      widget.onMetaChanged?.call(updated);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _isError = true;
        _statusMessage = e.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final locked = _meta.designLocked;

    return AlertDialog(
      title: Row(
        children: [
          const Icon(Icons.build_circle_outlined, size: 22),
          const SizedBox(width: 8),
          const Expanded(child: Text('Developer Utilities')),
          if (locked)
            Chip(
              avatar: const Icon(Icons.lock, size: 16),
              label: const Text('Design Locked'),
              visualDensity: VisualDensity.compact,
              backgroundColor: theme.colorScheme.errorContainer,
            ),
        ],
      ),
      content: SizedBox(
        width: 580,
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            mainAxisSize: MainAxisSize.min,
            children: [
              if (_statusMessage != null) ...[
                Container(
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: _isError
                        ? theme.colorScheme.errorContainer
                        : theme.colorScheme.secondaryContainer,
                    borderRadius: BorderRadius.circular(6),
                  ),
                  child: Text(
                    _statusMessage!,
                    style: TextStyle(
                      fontSize: 12,
                      color: _isError
                          ? theme.colorScheme.onErrorContainer
                          : theme.colorScheme.onSecondaryContainer,
                    ),
                  ),
                ),
                const SizedBox(height: 12),
              ],

              // 1. Clone without records
              _SectionCard(
                title: 'Clone Solution (Without Records)',
                subtitle:
                    'Creates a new database on the server with all tables, fields, relationships, layouts, scripts, value lists, custom menus, and accounts, and zero user records.',
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: TextField(
                            key: const Key('dev_util_clone_db_field'),
                            controller: _cloneDbCtrl,
                            enabled: !_busy,
                            decoration: const InputDecoration(
                              labelText: 'Target database name',
                              isDense: true,
                              border: OutlineInputBorder(),
                            ),
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: TextField(
                            key: const Key('dev_util_clone_name_field'),
                            controller: _cloneNameCtrl,
                            enabled: !_busy,
                            decoration: const InputDecoration(
                              labelText: 'Clone solution name',
                              isDense: true,
                              border: OutlineInputBorder(),
                            ),
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 8),
                    CheckboxListTile(
                      key: const Key('dev_util_clone_lock_checkbox'),
                      contentPadding: EdgeInsets.zero,
                      dense: true,
                      value: _lockCloneDesign,
                      onChanged: _busy
                          ? null
                          : (v) => setState(() => _lockCloneDesign = v ?? false),
                      title: const Text(
                        'Permanently lock the design of the cloned solution',
                        style: TextStyle(fontSize: 13),
                      ),
                    ),
                    Align(
                      alignment: Alignment.centerRight,
                      child: FilledButton.icon(
                        key: const Key('dev_util_clone_button'),
                        onPressed: _busy ? null : _cloneSolution,
                        icon: const Icon(Icons.copy_all, size: 16),
                        label: const Text('Create Clone'),
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 12),

              // 2. Rename solution
              _SectionCard(
                title: 'Rename Solution',
                subtitle:
                    'Updates the logical name of "${_meta.database}" and rewrites internal layout and script references to the previous name.',
                child: Row(
                  children: [
                    Expanded(
                      child: TextField(
                        key: const Key('dev_util_rename_field'),
                        controller: _renameCtrl,
                        enabled: !_busy && !locked,
                        decoration: const InputDecoration(
                          labelText: 'Solution name',
                          isDense: true,
                          border: OutlineInputBorder(),
                        ),
                      ),
                    ),
                    const SizedBox(width: 12),
                    FilledButton.tonal(
                      key: const Key('dev_util_rename_button'),
                      onPressed: (_busy || locked) ? null : _renameSolution,
                      child: const Text('Rename'),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 12),

              // 3. Kiosk Mode & Solution Encryption
              _SectionCard(
                title: 'Kiosk Mode & Solution Encryption',
                subtitle:
                    'Configure Kiosk Mode (opens the solution hiding File4Base toolbars and menus for non-admin users) and AES-256-GCM encryption for solution packages and API transport.',
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    SwitchListTile(
                      key: const Key('dev_util_kiosk_switch'),
                      contentPadding: EdgeInsets.zero,
                      dense: true,
                      value: _kioskMode,
                      onChanged: _busy
                          ? null
                          : (v) => setState(() => _kioskMode = v),
                      title: const Text(
                        'Activate Kiosk Mode (hide toolbars and menu bar for non-admin accounts)',
                        style: TextStyle(fontSize: 13),
                      ),
                    ),
                    SwitchListTile(
                      key: const Key('dev_util_encryption_switch'),
                      contentPadding: EdgeInsets.zero,
                      dense: true,
                      value: _encryptionEnabled,
                      onChanged: _busy
                          ? null
                          : (v) => setState(() => _encryptionEnabled = v),
                      title: const Text(
                        'Encrypt database bundle & transport with AES-256-GCM',
                        style: TextStyle(fontSize: 13),
                      ),
                    ),
                    if (_encryptionEnabled) ...[
                      const SizedBox(height: 6),
                      TextField(
                        key: const Key('dev_util_encryption_passphrase_field'),
                        controller: _encryptionPassphraseCtrl,
                        enabled: !_busy,
                        decoration: const InputDecoration(
                          labelText: 'Encryption key identifier / hint (optional)',
                          isDense: true,
                          border: OutlineInputBorder(),
                        ),
                      ),
                    ],
                    const SizedBox(height: 8),
                    Align(
                      alignment: Alignment.centerRight,
                      child: FilledButton.tonalIcon(
                        key: const Key('dev_util_save_protection_button'),
                        onPressed: _busy ? null : _saveKioskAndEncryptionOptions,
                        icon: const Icon(Icons.shield_outlined, size: 16),
                        label: const Text('Apply Kiosk & Encryption'),
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 12),

              // 4. Permanently lock design
              _SectionCard(
                title: 'Lock Solution Design (Remove Admin Access)',
                subtitle: locked
                    ? 'This solution’s design is permanently locked. Schema, layouts, scripts, value lists, and custom menus cannot be modified.'
                    : 'Permanently removes design access from this copy. Record entry, finds, imports, exports, and sign-in continue to work.',
                child: locked
                    ? const Row(
                        children: [
                          Icon(Icons.verified_user_outlined, size: 18),
                          SizedBox(width: 8),
                          Expanded(
                            child: Text(
                              'Design lock is active and irreversible on this database.',
                              style: TextStyle(fontSize: 12),
                            ),
                          ),
                        ],
                      )
                    : Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          CheckboxListTile(
                            key: const Key('dev_util_lock_confirm_checkbox'),
                            contentPadding: EdgeInsets.zero,
                            dense: true,
                            value: _confirmLockCurrent,
                            onChanged: _busy
                                ? null
                                : (v) => setState(
                                    () => _confirmLockCurrent = v ?? false),
                            title: const Text(
                              'I understand that locking the design cannot be undone on this copy',
                              style: TextStyle(fontSize: 13),
                            ),
                          ),
                          Align(
                            alignment: Alignment.centerRight,
                            child: FilledButton.icon(
                              key: const Key('dev_util_lock_button'),
                              style: FilledButton.styleFrom(
                                backgroundColor: theme.colorScheme.error,
                                foregroundColor: theme.colorScheme.onError,
                              ),
                              onPressed: (_busy || !_confirmLockCurrent)
                                  ? null
                                  : _lockCurrentDesign,
                              icon: const Icon(Icons.lock_outline, size: 16),
                              label: const Text('Permanently Lock Design'),
                            ),
                          ),
                        ],
                      ),
              ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: _busy ? null : () => Navigator.of(context).pop(),
          child: const Text('Close'),
        ),
      ],
    );
  }
}

class _SectionCard extends StatelessWidget {
  final String title;
  final String subtitle;
  final Widget child;

  const _SectionCard({
    required this.title,
    required this.subtitle,
    required this.child,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        border: Border.all(color: theme.dividerColor),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            title,
            style: theme.textTheme.titleSmall?.copyWith(
              fontWeight: FontWeight.w600,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            subtitle,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 12),
          child,
        ],
      ),
    );
  }
}
