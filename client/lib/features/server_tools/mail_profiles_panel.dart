import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter/material.dart';

/// Server Tools > Send Mail (SMTP) (#133): the SMTP profiles the Send Mail
/// script step sends through — the server's, and one per solution that
/// takes its place. A profile never holds its password: it names an
/// environment variable of the server (FILE4BASE_SMTP_…) that does, and the
/// panel only shows whether that variable is set.
class MailProfilesPanel extends StatefulWidget {
  const MailProfilesPanel({required this.apiClient, super.key});
  final ApiClient apiClient;

  @override
  State<MailProfilesPanel> createState() => _MailProfilesPanelState();
}

class _MailProfilesPanelState extends State<MailProfilesPanel> {
  List<Map<String, dynamic>> _profiles = [];
  List<String> _serverSolutions = [];
  String _prefix = 'FILE4BASE_SMTP_';
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    try {
      final res = await widget.apiClient.listMailProfiles();
      if (!mounted) return;
      setState(() {
        _profiles = [for (final p in (res['profiles'] as List? ?? const [])) Map<String, dynamic>.from(p as Map)];
        _prefix = res['password_env_prefix']?.toString() ?? _prefix;
        _serverSolutions = [for (final s in (res['server_profile_solutions'] as List? ?? const [])) s.toString()];
        _loading = false;
        _error = null;
      });
    } on Object catch (e) {
      if (mounted) {
        setState(() {
          _loading = false;
          _error = e is ApiException ? e.detail : '$e';
        });
      }
    }
  }

  void _toast(String message, {bool error = false}) {
    ScaffoldMessenger.maybeOf(context)?.showSnackBar(
      SnackBar(content: Text(message), backgroundColor: error ? Colors.red.shade700 : Colors.green.shade700),
    );
  }

  Future<void> _edit(Map<String, dynamic>? profile) async {
    final saved = await showDialog<bool>(
      context: context,
      builder: (_) => _MailProfileEditor(apiClient: widget.apiClient, profile: profile, prefix: _prefix),
    );
    if (saved == true) await _load();
  }

  Future<void> _delete(Map<String, dynamic> profile) async {
    final solution = profile['solution']?.toString() ?? '';
    try {
      await widget.apiClient.deleteMailProfile(solution: solution);
      _toast('SMTP profile removed.');
      await _load();
    } on ApiException catch (e) {
      _toast(e.detail, error: true);
    }
  }

  Future<void> _setServerAccess(String solution, {required bool allowed}) async {
    try {
      await widget.apiClient.setServerMailAccess(solution: solution, allowed: allowed);
      _toast(allowed ? '$solution can send through the server profile.' : '$solution no longer sends through the server profile.');
      await _load();
    } on ApiException catch (e) {
      _toast(e.detail, error: true);
    }
  }

  Future<void> _allowSolution() async {
    final ctrl = TextEditingController();
    final solution = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Allow a solution to use the server profile'),
        content: TextField(controller: ctrl, autofocus: true, decoration: const InputDecoration(labelText: 'Solution (database)')),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
          FilledButton(onPressed: () => Navigator.pop(ctx, ctrl.text.trim()), child: const Text('Allow')),
        ],
      ),
    );
    ctrl.dispose();
    if (solution == null || solution.isEmpty) return;
    await _setServerAccess(solution, allowed: true);
  }

  Future<void> _test(Map<String, dynamic> profile) async {
    final ctrl = TextEditingController();
    final to = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Send a test message'),
        content: TextField(controller: ctrl, autofocus: true, decoration: const InputDecoration(labelText: 'To')),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
          FilledButton(onPressed: () => Navigator.pop(ctx, ctrl.text.trim()), child: const Text('Send')),
        ],
      ),
    );
    ctrl.dispose();
    if (to == null || to.isEmpty) return;
    try {
      await widget.apiClient.testMailProfile(to: to, solution: profile['solution']?.toString() ?? '');
      _toast('Test message sent to $to.');
    } on ApiException catch (e) {
      _toast(e.detail, error: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) return const Center(child: CircularProgressIndicator());
    return ListView(
      padding: const EdgeInsets.all(24),
      children: [
        Row(
          children: [
            const Expanded(
              child: Text('Send Mail through SMTP', style: TextStyle(fontSize: 15, fontWeight: FontWeight.bold)),
            ),
            FilledButton.icon(
              onPressed: () => _edit(null),
              icon: const Icon(Icons.add, size: 16),
              label: const Text('Add profile'),
            ),
          ],
        ),
        const SizedBox(height: 8),
        Text(
          'Scripts send mail through the profile of their solution, or else through the server profile '
          'if a server administrator allowed their solution to (no solution is allowed by default). '
          'A Send Mail step "via SMTP server", one e-mail per record, attachments, or "via e-mail client" '
          'without a dialog need one. The password is never stored here: put it in an environment variable '
          'of the server whose name starts with $_prefix, and name that variable in the profile.',
          style: Theme.of(context).textTheme.bodySmall,
        ),
        if (_error != null) Padding(padding: const EdgeInsets.only(top: 12), child: Text(_error!, style: const TextStyle(color: Colors.red))),
        const SizedBox(height: 16),
        if (_profiles.isEmpty)
          const Card(child: ListTile(leading: Icon(Icons.info_outline), title: Text('No SMTP profile: scripts cannot send mail themselves.'))),
        for (final p in _profiles)
          Card(
            child: ListTile(
              leading: Icon((p['solution']?.toString() ?? '').isEmpty ? Icons.dns_outlined : Icons.folder_outlined),
              title: Text((p['solution']?.toString() ?? '').isEmpty ? 'Server (the solutions allowed below)' : 'Solution ${p['solution']}'),
              subtitle: Text([
                '${p['host']}:${p['port']} (${p['security']})',
                'from ${p['from_address']}',
                if ((p['username']?.toString() ?? '').isNotEmpty)
                  'as ${p['username']}, password in ${p['password_env']}${p['password_available'] == true ? '' : ' (NOT SET on the server)'}',
                '${p['max_per_hour']} messages per account per hour',
              ].join(' · ')),
              trailing: Wrap(
                children: [
                  IconButton(tooltip: 'Send a test message', icon: const Icon(Icons.send_outlined), onPressed: () => _test(p)),
                  IconButton(tooltip: 'Edit', icon: const Icon(Icons.edit_outlined), onPressed: () => _edit(p)),
                  IconButton(tooltip: 'Remove', icon: const Icon(Icons.delete_outline), onPressed: () => _delete(p)),
                ],
              ),
            ),
          ),
        const SizedBox(height: 24),
        Row(
          children: [
            const Expanded(
              child: Text('Solutions allowed to use the server profile', style: TextStyle(fontWeight: FontWeight.bold)),
            ),
            OutlinedButton.icon(
              onPressed: _allowSolution,
              icon: const Icon(Icons.add, size: 16),
              label: const Text('Allow a solution'),
            ),
          ],
        ),
        const SizedBox(height: 8),
        if (_serverSolutions.isEmpty)
          const Card(child: ListTile(leading: Icon(Icons.lock_outline), title: Text('None: only solutions with their own profile send mail.'))),
        for (final s in _serverSolutions)
          Card(
            child: ListTile(
              leading: const Icon(Icons.folder_outlined),
              title: Text(s),
              trailing: IconButton(
                tooltip: 'Withdraw',
                icon: const Icon(Icons.remove_circle_outline),
                onPressed: () => _setServerAccess(s, allowed: false),
              ),
            ),
          ),
      ],
    );
  }
}

class _MailProfileEditor extends StatefulWidget {
  const _MailProfileEditor({required this.apiClient, required this.profile, required this.prefix});
  final ApiClient apiClient;
  final Map<String, dynamic>? profile;
  final String prefix;

  @override
  State<_MailProfileEditor> createState() => _MailProfileEditorState();
}

class _MailProfileEditorState extends State<_MailProfileEditor> {
  late final Map<String, TextEditingController> _c = {
    for (final k in const ['solution', 'host', 'port', 'username', 'password_env', 'from_name', 'from_address', 'reply_to', 'max_per_hour'])
      k: TextEditingController(text: widget.profile?[k]?.toString() ?? ''),
  };
  late String _security = widget.profile?['security']?.toString() ?? 'starttls';
  String? _error;
  bool _saving = false;

  @override
  void dispose() {
    for (final c in _c.values) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _save() async {
    setState(() => _saving = true);
    try {
      await widget.apiClient.putMailProfile({
        for (final e in _c.entries)
          if (e.key != 'port' && e.key != 'max_per_hour') e.key: e.value.text.trim(),
        'port': int.tryParse(_c['port']!.text.trim()) ?? 0,
        'max_per_hour': int.tryParse(_c['max_per_hour']!.text.trim()) ?? 0,
        'security': _security,
      });
      if (mounted) Navigator.pop(context, true);
    } on ApiException catch (e) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = e.detail;
        });
      }
    }
  }

  Widget _field(String key, String label, {String? hint}) => Padding(
        padding: const EdgeInsets.only(bottom: 8),
        child: TextField(
          controller: _c[key],
          enabled: !(key == 'solution' && widget.profile != null),
          decoration: InputDecoration(labelText: label, hintText: hint, isDense: true, border: const OutlineInputBorder()),
        ),
      );

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(widget.profile == null ? 'New SMTP profile' : 'SMTP profile'),
      content: SizedBox(
        width: 480,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              _field('solution', 'Solution (database)', hint: 'Empty: the server profile, for the solutions allowed to use it'),
              _field('host', 'SMTP server'),
              Row(children: [
                Expanded(child: _field('port', 'Port', hint: '587, 465 or 25')),
                const SizedBox(width: 8),
                Expanded(
                  child: Padding(
                    padding: const EdgeInsets.only(bottom: 8),
                    child: DropdownButtonFormField<String>(
                      initialValue: _security,
                      decoration: const InputDecoration(labelText: 'Security', isDense: true, border: OutlineInputBorder()),
                      items: const [
                        DropdownMenuItem(value: 'starttls', child: Text('STARTTLS')),
                        DropdownMenuItem(value: 'tls', child: Text('TLS (SMTPS)')),
                        DropdownMenuItem(value: 'none', child: Text('None')),
                      ],
                      onChanged: (v) => setState(() => _security = v ?? 'starttls'),
                    ),
                  ),
                ),
              ]),
              _field('username', 'User name'),
              _field('password_env', 'Password variable on the server', hint: '${widget.prefix}PASSWORD'),
              _field('from_name', 'Sender name'),
              _field('from_address', 'Sender address'),
              _field('reply_to', 'Reply-to address'),
              _field('max_per_hour', 'Messages per account per hour', hint: '200'),
              if (_error != null) Text(_error!, style: const TextStyle(color: Colors.red)),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context), child: const Text('Cancel')),
        FilledButton(onPressed: _saving ? null : _save, child: const Text('Save')),
      ],
    );
  }
}
