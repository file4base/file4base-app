// Setting an account's password, from File > Manage > Security and from the
// end of a FileMaker migration (#198).
//
// An account a migration or a solution import created has no usable password
// and is switched off: it cannot sign in until an administrator sets one. The
// same dialog that changes a password does that, and offers to switch the
// account on at the same time.

import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter/material.dart';

class SetPasswordDialog extends StatefulWidget {
  const SetPasswordDialog({
    required this.apiClient,
    required this.user,
    this.databaseName,
    super.key,
  });

  final ApiClient apiClient;
  final UserModel user;

  /// The database the account is in, when it is not the session's.
  final String? databaseName;

  /// Shows the dialog. Returns the account as it now is when a password was
  /// saved, or null when the dialog was cancelled.
  static Future<UserModel?> show(
    BuildContext context, {
    required ApiClient apiClient,
    required UserModel user,
    String? databaseName,
  }) {
    return showDialog<UserModel>(
      context: context,
      builder: (_) => SetPasswordDialog(apiClient: apiClient, user: user, databaseName: databaseName),
    );
  }

  @override
  State<SetPasswordDialog> createState() => _SetPasswordDialogState();
}

class _SetPasswordDialogState extends State<SetPasswordDialog> {
  final _password = TextEditingController();
  final _confirm = TextEditingController();
  bool _obscure = true;
  bool _saving = false;
  String? _error;

  /// Switch the account on with its password. Offered only for an account
  /// that is off; on by default when it is off only for want of a password.
  late bool _activate = widget.user.passwordPending;

  @override
  void dispose() {
    _password.dispose();
    _confirm.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final p1 = _password.text;
    if (p1.isEmpty) {
      setState(() => _error = 'Password cannot be empty.');
      return;
    }
    if (p1 != _confirm.text) {
      setState(() => _error = 'Passwords do not match.');
      return;
    }
    final user = widget.user;
    final active = user.isActive || _activate;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await widget.apiClient.updateUser(
        user.id,
        role: user.role,
        password: p1,
        isActive: active,
        database: widget.databaseName,
      );
      if (!mounted) return;
      Navigator.of(context).pop(user.copyWith(isActive: active, passwordPending: false));
    } on Object catch (e) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = e.toString().replaceFirst('Exception: ', '').replaceFirst('ApiException ', '');
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final user = widget.user;
    final pending = user.passwordPending;
    return AlertDialog(
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      title: Row(
        children: [
          const Icon(Icons.key_outlined, color: Color(0xFF1E88E5), size: 22),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              '${pending ? 'Set password' : 'Change password'}: ${user.username}',
              style: const TextStyle(fontSize: 16),
            ),
          ),
        ],
      ),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              pending
                  ? 'Account "${user.username}" was created without a password and cannot sign in until one is set. '
                      'Give the password to its holder over a channel you trust.'
                  : 'Enter the new password for account "${user.username}".',
              style: const TextStyle(fontSize: 12, color: Colors.grey),
            ),
            const SizedBox(height: 16),
            TextField(
              key: const Key('set-password-new'),
              controller: _password,
              obscureText: _obscure,
              autofocus: true,
              enabled: !_saving,
              decoration: InputDecoration(
                labelText: 'New password',
                border: const OutlineInputBorder(),
                isDense: true,
                prefixIcon: const Icon(Icons.lock_outline, size: 18),
                suffixIcon: IconButton(
                  icon: Icon(_obscure ? Icons.visibility_off : Icons.visibility, size: 18),
                  tooltip: _obscure ? 'Show password' : 'Hide password',
                  onPressed: () => setState(() => _obscure = !_obscure),
                ),
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const Key('set-password-confirm'),
              controller: _confirm,
              obscureText: _obscure,
              enabled: !_saving,
              onSubmitted: (_) => _saving ? null : _save(),
              decoration: InputDecoration(
                labelText: 'Confirm password',
                border: const OutlineInputBorder(),
                isDense: true,
                prefixIcon: const Icon(Icons.lock_reset, size: 18),
                suffixIcon: IconButton(
                  icon: Icon(_obscure ? Icons.visibility_off : Icons.visibility, size: 18),
                  tooltip: _obscure ? 'Show password' : 'Hide password',
                  onPressed: () => setState(() => _obscure = !_obscure),
                ),
              ),
            ),
            if (!user.isActive) ...[
              const SizedBox(height: 8),
              CheckboxListTile(
                key: const Key('set-password-activate'),
                contentPadding: EdgeInsets.zero,
                dense: true,
                controlAffinity: ListTileControlAffinity.leading,
                value: _activate,
                onChanged: _saving ? null : (v) => setState(() => _activate = v ?? false),
                title: const Text('Activate the account so that it can sign in', style: TextStyle(fontSize: 12.5)),
                subtitle: _activate
                    ? null
                    : const Text('It stays inactive and cannot sign in until it is activated.',
                        style: TextStyle(fontSize: 11)),
              ),
            ],
            if (_error != null) ...[
              const SizedBox(height: 12),
              Text(_error!, style: const TextStyle(color: Colors.red, fontSize: 12)),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _saving ? null : () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        ElevatedButton.icon(
          key: const Key('set-password-save'),
          style: ElevatedButton.styleFrom(
            backgroundColor: const Color(0xFF1E88E5),
            foregroundColor: Colors.white,
          ),
          icon: _saving
              ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
              : const Icon(Icons.check, size: 18),
          label: const Text('Save password'),
          onPressed: _saving ? null : _save,
        ),
      ],
    );
  }
}
