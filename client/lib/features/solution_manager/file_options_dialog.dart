import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import '../../core/models/file_options_model.dart';
import '../security/change_password_dialog.dart';

class FileOptionsDialog extends StatefulWidget {
  final FileOptionsModel initialOptions;
  final List<LayoutModel> layouts;
  final UserModel? currentUser;
  final ApiClient? apiClient;

  const FileOptionsDialog({
    super.key,
    required this.initialOptions,
    required this.layouts,
    this.currentUser,
    this.apiClient,
  });

  static Future<FileOptionsModel?> show(
    BuildContext context, {
    required FileOptionsModel initialOptions,
    required List<LayoutModel> layouts,
    UserModel? currentUser,
    ApiClient? apiClient,
  }) {
    return showDialog<FileOptionsModel>(
      context: context,
      barrierDismissible: true,
      builder: (ctx) => FileOptionsDialog(
        initialOptions: initialOptions,
        layouts: layouts,
        currentUser: currentUser,
        apiClient: apiClient,
      ),
    );
  }

  @override
  State<FileOptionsDialog> createState() => _FileOptionsDialogState();
}

class _FileOptionsDialogState extends State<FileOptionsDialog> with SingleTickerProviderStateMixin {
  late TabController _tabController;

  // --- Tab 1: Open ---
  late bool _autoLoginEnabled;
  late bool _isGuestLogin;
  late TextEditingController _usernameController;
  late TextEditingController _passwordController;
  bool _obscurePassword = true;

  late bool _switchLayoutOnOpen;
  String? _selectedLayoutId;

  late bool _hideAllToolbars;

  // --- Tab 2: Script Triggers ---
  late bool _onFirstWindowOpenEnabled;
  late TextEditingController _onFirstWindowOpenScriptCtrl;
  late TextEditingController _onFirstWindowOpenParamCtrl;

  late bool _onLastWindowCloseEnabled;
  late TextEditingController _onLastWindowCloseScriptCtrl;
  late TextEditingController _onLastWindowCloseParamCtrl;

  late bool _onWindowOpenEnabled;
  late TextEditingController _onWindowOpenScriptCtrl;
  late TextEditingController _onWindowOpenParamCtrl;

  late bool _onWindowCloseEnabled;
  late TextEditingController _onWindowCloseScriptCtrl;
  late TextEditingController _onWindowCloseParamCtrl;

  late bool _onFileAVPlayerChangeEnabled;
  late TextEditingController _onFileAVPlayerChangeScriptCtrl;
  late TextEditingController _onFileAVPlayerChangeParamCtrl;

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 2, vsync: this);

    final opt = widget.initialOptions;

    _autoLoginEnabled = opt.autoLoginEnabled;
    _isGuestLogin = opt.isGuestLogin;
    _usernameController = TextEditingController(text: opt.defaultUsername);
    _passwordController = TextEditingController(text: opt.defaultPassword);

    _switchLayoutOnOpen = opt.switchLayoutOnOpen;
    _selectedLayoutId = opt.startupLayoutId.isNotEmpty
        ? opt.startupLayoutId
        : (widget.layouts.isNotEmpty ? widget.layouts.first.id : null);

    _hideAllToolbars = opt.hideAllToolbars;

    _onFirstWindowOpenEnabled = opt.onFirstWindowOpenEnabled;
    _onFirstWindowOpenScriptCtrl = TextEditingController(text: opt.onFirstWindowOpenScript);
    _onFirstWindowOpenParamCtrl = TextEditingController(text: opt.onFirstWindowOpenParam);

    _onLastWindowCloseEnabled = opt.onLastWindowCloseEnabled;
    _onLastWindowCloseScriptCtrl = TextEditingController(text: opt.onLastWindowCloseScript);
    _onLastWindowCloseParamCtrl = TextEditingController(text: opt.onLastWindowCloseParam);

    _onWindowOpenEnabled = opt.onWindowOpenEnabled;
    _onWindowOpenScriptCtrl = TextEditingController(text: opt.onWindowOpenScript);
    _onWindowOpenParamCtrl = TextEditingController(text: opt.onWindowOpenParam);

    _onWindowCloseEnabled = opt.onWindowCloseEnabled;
    _onWindowCloseScriptCtrl = TextEditingController(text: opt.onWindowCloseScript);
    _onWindowCloseParamCtrl = TextEditingController(text: opt.onWindowCloseParam);

    _onFileAVPlayerChangeEnabled = opt.onFileAVPlayerChangeEnabled;
    _onFileAVPlayerChangeScriptCtrl = TextEditingController(text: opt.onFileAVPlayerChangeScript);
    _onFileAVPlayerChangeParamCtrl = TextEditingController(text: opt.onFileAVPlayerChangeParam);
  }

  @override
  void dispose() {
    _tabController.dispose();
    _usernameController.dispose();
    _passwordController.dispose();

    _onFirstWindowOpenScriptCtrl.dispose();
    _onFirstWindowOpenParamCtrl.dispose();
    _onLastWindowCloseScriptCtrl.dispose();
    _onLastWindowCloseParamCtrl.dispose();
    _onWindowOpenScriptCtrl.dispose();
    _onWindowOpenParamCtrl.dispose();
    _onWindowCloseScriptCtrl.dispose();
    _onWindowCloseParamCtrl.dispose();
    _onFileAVPlayerChangeScriptCtrl.dispose();
    _onFileAVPlayerChangeParamCtrl.dispose();
    super.dispose();
  }

  /// Changes the signed-in account's password. Uses the shared Change
  /// Password dialog, which reports success only after the server accepted
  /// the new password; the remembered password follows only then.
  Future<void> _handleChangePassword() async {
    final api = widget.apiClient;
    final user = widget.currentUser;
    if (api == null || user == null) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Sign in to the database to change the password.'),
          backgroundColor: Colors.red,
        ),
      );
      return;
    }
    final changed = await ChangePasswordDialog.show(
      context,
      apiClient: api,
      currentUser: user,
      onPasswordChanged: (p) => _passwordController.text = p,
    );
    if (changed == true && mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Password updated successfully.')),
      );
    }
  }

  void _onSave() {
    String startupLayoutName = '';
    if (_switchLayoutOnOpen && _selectedLayoutId != null) {
      final found = widget.layouts.where((l) => l.id == _selectedLayoutId).firstOrNull;
      if (found != null) {
        startupLayoutName = found.name;
      }
    }

    final updated = widget.initialOptions.copyWith(
      autoLoginEnabled: _autoLoginEnabled,
      isGuestLogin: _isGuestLogin,
      defaultUsername: _usernameController.text.trim(),
      defaultPassword: _passwordController.text,
      switchLayoutOnOpen: _switchLayoutOnOpen,
      startupLayoutId: _switchLayoutOnOpen ? (_selectedLayoutId ?? '') : '',
      startupLayoutName: startupLayoutName,
      hideAllToolbars: _hideAllToolbars,
      onFirstWindowOpenEnabled: _onFirstWindowOpenEnabled,
      onFirstWindowOpenScript: _onFirstWindowOpenScriptCtrl.text.trim(),
      onFirstWindowOpenParam: _onFirstWindowOpenParamCtrl.text.trim(),
      onLastWindowCloseEnabled: _onLastWindowCloseEnabled,
      onLastWindowCloseScript: _onLastWindowCloseScriptCtrl.text.trim(),
      onLastWindowCloseParam: _onLastWindowCloseParamCtrl.text.trim(),
      onWindowOpenEnabled: _onWindowOpenEnabled,
      onWindowOpenScript: _onWindowOpenScriptCtrl.text.trim(),
      onWindowOpenParam: _onWindowOpenParamCtrl.text.trim(),
      onWindowCloseEnabled: _onWindowCloseEnabled,
      onWindowCloseScript: _onWindowCloseScriptCtrl.text.trim(),
      onWindowCloseParam: _onWindowCloseParamCtrl.text.trim(),
      onFileAVPlayerChangeEnabled: _onFileAVPlayerChangeEnabled,
      onFileAVPlayerChangeScript: _onFileAVPlayerChangeScriptCtrl.text.trim(),
      onFileAVPlayerChangeParam: _onFileAVPlayerChangeParamCtrl.text.trim(),
    );

    Navigator.of(context).pop(updated);
  }

  @override
  Widget build(BuildContext context) {
    return Dialog(
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 680, maxHeight: 650),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // Header
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 16),
              child: Row(
                children: [
                  const Icon(Icons.settings_applications, color: Color(0xFF1E88E5), size: 24),
                  const SizedBox(width: 10),
                  const Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('File Options', style: TextStyle(fontSize: 17, fontWeight: FontWeight.bold)),
                        Text('Startup settings, credentials, and script triggers', style: TextStyle(fontSize: 12, color: Colors.grey)),
                      ],
                    ),
                  ),
                  IconButton(
                    onPressed: () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close, size: 20),
                    tooltip: 'Close',
                  ),
                ],
              ),
            ),

            // Tab bar
            Container(
              decoration: BoxDecoration(
                border: Border(bottom: BorderSide(color: Colors.grey.withValues(alpha: 0.2))),
              ),
              child: TabBar(
                controller: _tabController,
                isScrollable: true,
                tabAlignment: TabAlignment.start,
                indicatorColor: const Color(0xFF1E88E5),
                indicatorWeight: 3,
                labelColor: const Color(0xFF1E88E5),
                unselectedLabelColor: Colors.grey,
                tabs: const [
                  Tab(icon: Icon(Icons.login, size: 18), text: 'Open'),
                  Tab(icon: Icon(Icons.bolt, size: 18), text: 'Script Triggers'),
                ],
              ),
            ),

            // Tab views
            Expanded(
              child: TabBarView(
                controller: _tabController,
                children: [
                  _buildOpenTab(),
                  _buildScriptTriggersTab(),
                ],
              ),
            ),

            const Divider(height: 1),

            // Footer
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.end,
                children: [
                  OutlinedButton(
                    onPressed: () => Navigator.of(context).pop(),
                    child: const Text('Cancel'),
                  ),
                  const SizedBox(width: 12),
                  FilledButton(
                    onPressed: _onSave,
                    style: FilledButton.styleFrom(backgroundColor: const Color(0xFF1E88E5)),
                    child: const Text('OK'),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildOpenTab() {
    return SingleChildScrollView(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Section: Sign in as
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              border: Border.all(color: Colors.grey.withValues(alpha: 0.3)),
              borderRadius: BorderRadius.circular(8),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Checkbox(
                      value: _autoLoginEnabled,
                      onChanged: (val) => setState(() => _autoLoginEnabled = val ?? false),
                    ),
                    const Expanded(
                      child: Text(
                        'Sign in as:',
                        style: TextStyle(fontWeight: FontWeight.bold, fontSize: 14),
                      ),
                    ),
                  ],
                ),
                Padding(
                  padding: const EdgeInsets.only(left: 36),
                  child: Column(
                    children: [
                      // Radio Account & Password
                      RadioListTile<bool>(
                        dense: true,
                        contentPadding: EdgeInsets.zero,
                        title: const Text('Account name and password:'),
                        value: false,
                        groupValue: _isGuestLogin,
                        onChanged: _autoLoginEnabled
                            ? (val) => setState(() => _isGuestLogin = val ?? false)
                            : null,
                      ),
                      Padding(
                        padding: const EdgeInsets.only(left: 32, bottom: 8),
                        child: Row(
                          children: [
                            Expanded(
                              child: TextField(
                                controller: _usernameController,
                                enabled: _autoLoginEnabled && !_isGuestLogin,
                                decoration: const InputDecoration(
                                  labelText: 'Account',
                                  isDense: true,
                                  border: OutlineInputBorder(),
                                ),
                              ),
                            ),
                            const SizedBox(width: 12),
                            Expanded(
                              child: TextField(
                                controller: _passwordController,
                                enabled: _autoLoginEnabled && !_isGuestLogin,
                                obscureText: _obscurePassword,
                                decoration: InputDecoration(
                                  labelText: 'Password',
                                  isDense: true,
                                  border: const OutlineInputBorder(),
                                  suffixIcon: IconButton(
                                    icon: Icon(_obscurePassword ? Icons.visibility_off : Icons.visibility, size: 18),
                                    onPressed: () => setState(() => _obscurePassword = !_obscurePassword),
                                  ),
                                ),
                              ),
                            ),
                          ],
                        ),
                      ),
                      // Radio Guest Account
                      RadioListTile<bool>(
                        dense: true,
                        contentPadding: EdgeInsets.zero,
                        title: const Text('Guest account'),
                        value: true,
                        groupValue: _isGuestLogin,
                        onChanged: _autoLoginEnabled
                            ? (val) => setState(() => _isGuestLogin = val ?? true)
                            : null,
                      ),
                    ],
                  ),
                ),
                const SizedBox(height: 8),
                Padding(
                  padding: const EdgeInsets.only(left: 36),
                  child: OutlinedButton.icon(
                    onPressed: _handleChangePassword,
                    icon: const Icon(Icons.key, size: 16),
                    label: const Text('Change password...', style: TextStyle(fontSize: 12)),
                  ),
                ),
              ],
            ),
          ),

          const SizedBox(height: 16),

          // Section: Startup layout
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              border: Border.all(color: Colors.grey.withValues(alpha: 0.3)),
              borderRadius: BorderRadius.circular(8),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Checkbox(
                      value: _switchLayoutOnOpen,
                      onChanged: (val) => setState(() => _switchLayoutOnOpen = val ?? false),
                    ),
                    const Expanded(
                      child: Text(
                        'Startup layout (switch to layout on open):',
                        style: TextStyle(fontWeight: FontWeight.bold, fontSize: 14),
                      ),
                    ),
                  ],
                ),
                Padding(
                  padding: const EdgeInsets.only(left: 36, top: 4),
                  child: DropdownButtonFormField<String>(
                    initialValue: widget.layouts.any((l) => l.id == _selectedLayoutId)
                        ? _selectedLayoutId
                        : (widget.layouts.isNotEmpty ? widget.layouts.first.id : null),
                    decoration: const InputDecoration(
                      isDense: true,
                      border: OutlineInputBorder(),
                      helperText: 'Select the layout to display when the database opens.',
                    ),
                    items: widget.layouts.map((l) {
                      return DropdownMenuItem<String>(
                        value: l.id,
                        child: Text('${l.name} (${l.id})'),
                      );
                    }).toList(),
                    onChanged: _switchLayoutOnOpen
                        ? (val) => setState(() => _selectedLayoutId = val)
                        : null,
                  ),
                ),
              ],
            ),
          ),

          const SizedBox(height: 16),

          // Section: Hide all toolbars
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              border: Border.all(color: Colors.grey.withValues(alpha: 0.3)),
              borderRadius: BorderRadius.circular(8),
            ),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Checkbox(
                  value: _hideAllToolbars,
                  onChanged: (val) => setState(() => _hideAllToolbars = val ?? false),
                ),
                const SizedBox(width: 8),
                const Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'Hide all toolbars',
                        style: TextStyle(fontWeight: FontWeight.bold, fontSize: 14),
                      ),
                      SizedBox(height: 4),
                      Text(
                        'Display the application full screen or without native menus and toolbars.',
                        style: TextStyle(fontSize: 12, color: Colors.grey),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildScriptTriggersTab() {
    return SingleChildScrollView(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: const Color(0xFF1E88E5).withValues(alpha: 0.08),
              borderRadius: BorderRadius.circular(6),
              border: Border.all(color: const Color(0xFF1E88E5).withValues(alpha: 0.2)),
            ),
            child: const Row(
              children: [
                Icon(Icons.info_outline, color: Color(0xFF1E88E5), size: 20),
                SizedBox(width: 10),
                Expanded(
                  child: Text(
                    'Assign scripts to file events, such as OnFirstWindowOpen when the first window opens or OnLastWindowClose when the last window closes.',
                    style: TextStyle(fontSize: 12, height: 1.3),
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 16),

          _buildTriggerTile(
            eventName: 'OnFirstWindowOpen',
            eventDescription: 'Runs when the first window of a database file opens.',
            enabled: _onFirstWindowOpenEnabled,
            onChanged: (v) => setState(() => _onFirstWindowOpenEnabled = v ?? false),
            scriptCtrl: _onFirstWindowOpenScriptCtrl,
            paramCtrl: _onFirstWindowOpenParamCtrl,
          ),
          const SizedBox(height: 12),

          _buildTriggerTile(
            eventName: 'OnLastWindowClose',
            eventDescription: 'Runs when the last open window of a database file closes.',
            enabled: _onLastWindowCloseEnabled,
            onChanged: (v) => setState(() => _onLastWindowCloseEnabled = v ?? false),
            scriptCtrl: _onLastWindowCloseScriptCtrl,
            paramCtrl: _onLastWindowCloseParamCtrl,
          ),
          const SizedBox(height: 12),

          _buildTriggerTile(
            eventName: 'OnWindowOpen',
            eventDescription: 'Runs whenever a window of the file opens.',
            enabled: _onWindowOpenEnabled,
            onChanged: (v) => setState(() => _onWindowOpenEnabled = v ?? false),
            scriptCtrl: _onWindowOpenScriptCtrl,
            paramCtrl: _onWindowOpenParamCtrl,
          ),
          const SizedBox(height: 12),

          _buildTriggerTile(
            eventName: 'OnWindowClose',
            eventDescription: 'Runs whenever a window of the file closes.',
            enabled: _onWindowCloseEnabled,
            onChanged: (v) => setState(() => _onWindowCloseEnabled = v ?? false),
            scriptCtrl: _onWindowCloseScriptCtrl,
            paramCtrl: _onWindowCloseParamCtrl,
          ),
          const SizedBox(height: 12),

          _buildTriggerTile(
            eventName: 'OnFileAVPlayerChange',
            eventDescription: 'Runs when the media player state changes during playback.',
            enabled: _onFileAVPlayerChangeEnabled,
            onChanged: (v) => setState(() => _onFileAVPlayerChangeEnabled = v ?? false),
            scriptCtrl: _onFileAVPlayerChangeScriptCtrl,
            paramCtrl: _onFileAVPlayerChangeParamCtrl,
          ),
        ],
      ),
    );
  }

  Widget _buildTriggerTile({
    required String eventName,
    required String eventDescription,
    required bool enabled,
    required ValueChanged<bool?> onChanged,
    required TextEditingController scriptCtrl,
    required TextEditingController paramCtrl,
  }) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        border: Border.all(color: enabled ? const Color(0xFF1E88E5).withValues(alpha: 0.5) : Colors.grey.withValues(alpha: 0.3)),
        borderRadius: BorderRadius.circular(8),
        color: enabled ? const Color(0xFF1E88E5).withValues(alpha: 0.03) : null,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Checkbox(
                value: enabled,
                onChanged: onChanged,
              ),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(eventName, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                    Text(eventDescription, style: const TextStyle(fontSize: 11, color: Colors.grey)),
                  ],
                ),
              ),
            ],
          ),
          if (enabled) ...[
            const SizedBox(height: 8),
            Padding(
              padding: const EdgeInsets.only(left: 36),
              child: Row(
                children: [
                  Expanded(
                    flex: 3,
                    child: TextField(
                      controller: scriptCtrl,
                      decoration: const InputDecoration(
                        labelText: 'Script name',
                        hintText: 'e.g. Initialize_Session',
                        isDense: true,
                        border: OutlineInputBorder(),
                        prefixIcon: Icon(Icons.code, size: 16),
                      ),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    flex: 2,
                    child: TextField(
                      controller: paramCtrl,
                      decoration: const InputDecoration(
                        labelText: 'Optional parameter',
                        hintText: 'e.g. {"role":"admin"}',
                        isDense: true,
                        border: OutlineInputBorder(),
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ],
        ],
      ),
    );
  }
}
