import 'dart:async';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/services/auto_save_service.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:qr/qr.dart';

class ManageSecurityDialog extends StatefulWidget {

  const ManageSecurityDialog({
    required this.apiClient, required this.currentUser, super.key,
    this.databaseName,
    this.onModified,
  });
  final ApiClient apiClient;
  final UserModel currentUser;
  final String? databaseName;
  final VoidCallback? onModified;

  static Future<void> show(
    BuildContext context,
    ApiClient apiClient,
    UserModel currentUser, {
    String? databaseName,
    VoidCallback? onModified,
  }) {
    return showDialog(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => ManageSecurityDialog(
        apiClient: apiClient,
        currentUser: currentUser,
        databaseName: databaseName,
        onModified: onModified,
      ),
    );
  }

  @override
  State<ManageSecurityDialog> createState() => _ManageSecurityDialogState();
}

class _ManageSecurityDialogState extends State<ManageSecurityDialog> with SingleTickerProviderStateMixin {
  late TabController _tabController;
  List<UserModel> _users = [];
  List<LayoutModel> _layouts = [];
  List<TableModel> _tables = [];
  Map<String, List<UserLayoutPermissionModel>> _userPermissionsMap = {};
  Map<String, List<UserTablePermissionModel>> _userTablePermissionsMap = {};

  /// The capabilities each role holds (#39). Defaults are what a server
  /// without them reports: every role holds everything.
  List<RolePrivilegesModel> _rolePrivileges = const [
    RolePrivilegesModel(role: 'owner'),
    RolePrivilegesModel(role: 'admin'),
    RolePrivilegesModel(role: 'user'),
  ];
  bool _savingPrivileges = false;

  bool _isLoading = true;
  String? _errorMessage;
  String _searchQuery = '';
  String _statusFilter = 'all'; // 'all', 'active', 'inactive'

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 3, vsync: this);
    unawaited(_loadData());
  }

  @override
  void dispose() {
    _tabController.dispose();
    super.dispose();
  }

  Future<void> _loadData() async {
    setState(() {
      _isLoading = true;
      _errorMessage = null;
    });

    try {
      final users = await widget.apiClient.listUsers(database: widget.databaseName);
      final layouts = await widget.apiClient.listLayouts();
      var tables = <TableModel>[];
      try {
        tables = await widget.apiClient.listTables();
      } catch (_) {}
      await _loadRolePrivileges();

      // Preload layout and table permissions for all users
      final permsMap = <String, List<UserLayoutPermissionModel>>{};
      final tablePermsMap = <String, List<UserTablePermissionModel>>{};
      for (final u in users) {
        try {
          final p = await widget.apiClient.getUserPermissions(u.id, database: widget.databaseName);
          permsMap[u.id] = p;
        } catch (_) {
          permsMap[u.id] = [];
        }
        try {
          final tp = await widget.apiClient.getUserTablePermissions(u.id, database: widget.databaseName);
          tablePermsMap[u.id] = tp;
        } catch (_) {
          tablePermsMap[u.id] = [];
        }
      }

      if (mounted) {
        setState(() {
          _users = users;
          _layouts = layouts;
          _tables = tables;
          _userPermissionsMap = permsMap;
          _userTablePermissionsMap = tablePermsMap;
          _isLoading = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isLoading = false;
          _errorMessage = e.toString().replaceFirst('Exception: ', '');
        });
      }
    }
  }

  /// Reads the stored capability grants. A server from before #39 answers
  /// 404, which leaves the defaults in place rather than failing the dialog.
  Future<void> _loadRolePrivileges() async {
    try {
      final privileges = await widget.apiClient.listRolePrivileges(database: widget.databaseName);
      if (!mounted || privileges.isEmpty) return;
      setState(() => _rolePrivileges = privileges);
    } catch (_) {
      // Keep the defaults; the server is what enforces them anyway.
    }
  }

  List<UserModel> get _filteredUsers {
    return _users.where((u) {
      if (_statusFilter == 'active' && !u.isActive) return false;
      if (_statusFilter == 'inactive' && u.isActive) return false;
      if (_searchQuery.isNotEmpty) {
        final query = _searchQuery.toLowerCase();
        final matchName = u.username.toLowerCase().contains(query);
        final matchRole = u.role.toLowerCase().contains(query);
        return matchName || matchRole;
      }
      return true;
    }).toList();
  }

  int get _activeCount => _users.where((u) => u.isActive).length;
  int get _inactiveCount => _users.where((u) => !u.isActive).length;

  bool _isCurrentUser(UserModel user) => user.id == widget.currentUser.id || user.username == widget.currentUser.username;

  /// Whether the signed-in account may manage super-ownership.
  bool get _viewerIsSuperOwner => widget.currentUser.superOwner;

  /// Width of the account-row action column. A super-owner gets one extra
  /// control (the super-owner toggle), so the column widens to fit it.
  double get _optionsColumnWidth => _viewerIsSuperOwner ? 260 : 225;

  Future<void> _toggleSuperOwner(UserModel user) async {
    final grant = !user.superOwner;
    try {
      await widget.apiClient.setUserSuperOwner(user.id, grant, database: widget.databaseName);
      widget.onModified?.call();
      AutoSaveService.instance.markDirty();
      setState(() {
        final index = _users.indexWhere((u) => u.id == user.id);
        if (index != -1) {
          _users[index] = _users[index].copyWith(superOwner: grant);
        }
      });
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(grant
                ? 'Account "${user.username}" is now a super-owner (full server-wide control). Their sessions were signed out.'
                : 'Super-owner revoked from "${user.username}". Their sessions were signed out.'),
            backgroundColor: grant ? Colors.amber.shade800 : Colors.blueGrey,
            duration: const Duration(seconds: 3),
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Error changing super-owner status: $e'),
            backgroundColor: Colors.red,
          ),
        );
      }
    }
  }

  bool _isLastActiveOwner(UserModel user) {
    if (user.role != 'owner' || !user.isActive) return false;
    final activeOwners = _users.where((u) => u.role == 'owner' && u.isActive).length;
    return activeOwners <= 1;
  }

  Future<void> _toggleUserActive(UserModel user, bool newActive) async {
    if (_isCurrentUser(user)) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('You cannot deactivate your current account.'),
          backgroundColor: Colors.orange,
        ),
      );
      return;
    }

    if (!newActive && _isLastActiveOwner(user)) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('The database must have at least one active Owner account with full access.'),
          backgroundColor: Colors.red,
        ),
      );
      return;
    }

    try {
      await widget.apiClient.updateUser(
        user.id,
        role: user.role,
        isActive: newActive,
        database: widget.databaseName,
      );
      widget.onModified?.call();
      AutoSaveService.instance.markDirty();

      setState(() {
        final index = _users.indexWhere((u) => u.id == user.id);
        if (index != -1) {
          _users[index] = _users[index].copyWith(isActive: newActive);
        }
      });

      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Account "${user.username}" ${newActive ? 'activated' : 'deactivated'} successfully.'),
            backgroundColor: newActive ? Colors.green.shade700 : Colors.blueGrey,
            duration: const Duration(seconds: 2),
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Error changing account status: $e'),
            backgroundColor: Colors.red,
          ),
        );
      }
    }
  }

  Future<void> _showChangePasswordDialog(UserModel user) async {
    final passwordController = TextEditingController();
    final confirmController = TextEditingController();
    var obscure = true;
    String? dialogError;

    await showDialog(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDlgState) => AlertDialog(
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
          title: Row(
            children: [
              const Icon(Icons.key_outlined, color: Color(0xFF1E88E5), size: 22),
              const SizedBox(width: 8),
              Expanded(
                child: Text('Change password: ${user.username}', style: const TextStyle(fontSize: 16)),
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
                  'Enter the new password for account "${user.username}".',
                  style: const TextStyle(fontSize: 12, color: Colors.grey),
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: passwordController,
                  obscureText: obscure,
                  decoration: InputDecoration(
                    labelText: 'New password',
                    border: const OutlineInputBorder(),
                    isDense: true,
                    prefixIcon: const Icon(Icons.lock_outline, size: 18),
                    suffixIcon: IconButton(
                      icon: Icon(obscure ? Icons.visibility_off : Icons.visibility, size: 18),
                      onPressed: () => setDlgState(() => obscure = !obscure),
                    ),
                  ),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: confirmController,
                  obscureText: obscure,
                  decoration: InputDecoration(
                    labelText: 'Confirm password',
                    border: const OutlineInputBorder(),
                    isDense: true,
                    prefixIcon: const Icon(Icons.lock_reset, size: 18),
                    suffixIcon: IconButton(
                      icon: Icon(obscure ? Icons.visibility_off : Icons.visibility, size: 18),
                      onPressed: () => setDlgState(() => obscure = !obscure),
                    ),
                  ),
                ),
                if (dialogError != null) ...[
                  const SizedBox(height: 12),
                  Text(dialogError!, style: const TextStyle(color: Colors.red, fontSize: 12)),
                ],
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(ctx).pop(),
              child: const Text('Cancel'),
            ),
            ElevatedButton.icon(
              style: ElevatedButton.styleFrom(
                backgroundColor: const Color(0xFF1E88E5),
                foregroundColor: Colors.white,
              ),
              icon: const Icon(Icons.check, size: 18),
              label: const Text('Save password'),
              onPressed: () async {
                final p1 = passwordController.text;
                final p2 = confirmController.text;
                if (p1.isEmpty) {
                  setDlgState(() => dialogError = 'Password cannot be empty.');
                  return;
                }
                if (p1 != p2) {
                  setDlgState(() => dialogError = 'Passwords do not match.');
                  return;
                }

                try {
                  await widget.apiClient.updateUser(
                    user.id,
                    role: user.role,
                    password: p1,
                    isActive: user.isActive,
                    database: widget.databaseName,
                  );
                  widget.onModified?.call();
                  AutoSaveService.instance.markDirty();
                  if (ctx.mounted) Navigator.of(ctx).pop();
                  if (mounted) {
                    ScaffoldMessenger.of(context).showSnackBar(
                      SnackBar(
                        content: Text('Password for "${user.username}" updated successfully.'),
                        backgroundColor: Colors.green.shade700,
                      ),
                    );
                  }
                } catch (e) {
                  setDlgState(() => dialogError = e.toString().replaceFirst('Exception: ', ''));
                }
              },
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _showAccountDialog({UserModel? userToEdit}) async {
    final isNew = userToEdit == null;
    final usernameController = TextEditingController(text: userToEdit?.username ?? '');
    final passwordController = TextEditingController();
    var selectedRole = userToEdit?.role ?? 'user';
    var isActive = userToEdit?.isActive ?? true;
    var isSuperOwner = userToEdit?.superOwner ?? false;
    final layoutPerms = <String, String>{};
    final tablePerms = <String, UserTablePermissionModel>{};

    // Load initial layout permissions
    for (final l in _layouts) {
      layoutPerms[l.id] = (selectedRole == 'owner' || selectedRole == 'admin') ? 'read_write' : 'read_write';
    }
    if (!isNew && _userPermissionsMap.containsKey(userToEdit.id)) {
      for (final p in _userPermissionsMap[userToEdit.id]!) {
        layoutPerms[p.layoutId] = p.accessLevel;
      }
    }

    // Load initial table permissions
    for (final t in _tables) {
      tablePerms[t.name] = UserTablePermissionModel(
        tableName: t.name,
        canModifySchema: selectedRole == 'owner' || selectedRole == 'admin',
      );
    }
    if (!isNew && _userTablePermissionsMap.containsKey(userToEdit.id)) {
      for (final tp in _userTablePermissionsMap[userToEdit.id]!) {
        tablePerms[tp.tableName] = tp;
      }
    }

    String? dialogError;
    var isSaving = false;
    var obscurePassword = true;

    await showDialog(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDlgState) {
          final isOwnerLocked = !isNew && userToEdit.role == 'owner' && _users.where((u) => u.role == 'owner').length <= 1;

          return AlertDialog(
            shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
            title: Row(
              children: [
                Icon(
                  isNew ? Icons.person_add_alt_1_outlined : Icons.manage_accounts_outlined,
                  color: const Color(0xFF1E88E5),
                  size: 24,
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        isNew ? 'New user account' : 'Edit account: ${userToEdit.username}',
                        style: const TextStyle(fontSize: 16, fontWeight: FontWeight.bold),
                      ),
                      if ((widget.databaseName ?? '').isNotEmpty)
                        Text(
                          'Active database: ${widget.databaseName}',
                          style: const TextStyle(fontSize: 11, color: Colors.grey),
                        ),
                    ],
                  ),
                ),
              ],
            ),
            content: SizedBox(
              width: 620,
              height: 540,
              child: isSaving
                  ? const Center(child: CircularProgressIndicator())
                  : SingleChildScrollView(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          if (dialogError != null)
                            Container(
                              width: double.infinity,
                              margin: const EdgeInsets.only(bottom: 12),
                              padding: const EdgeInsets.all(8),
                              decoration: BoxDecoration(
                                color: Colors.red.shade900.withValues(alpha: 0.3),
                                borderRadius: BorderRadius.circular(6),
                                border: Border.all(color: Colors.red.shade700),
                              ),
                              child: Text(dialogError!, style: const TextStyle(color: Colors.red, fontSize: 12)),
                            ),

                          // User info row
                          Row(
                            children: [
                              Expanded(
                                flex: 3,
                                child: TextField(
                                  controller: usernameController,
                                  enabled: isNew || (userToEdit.role != 'owner'),
                                  decoration: const InputDecoration(
                                    labelText: 'Account name / Username *',
                                    border: OutlineInputBorder(),
                                    isDense: true,
                                    prefixIcon: Icon(Icons.person_outline, size: 18),
                                  ),
                                ),
                              ),
                              const SizedBox(width: 12),
                              Expanded(
                                flex: 3,
                                child: DropdownButtonFormField<String>(
                                  initialValue: selectedRole,
                                  decoration: const InputDecoration(
                                    labelText: 'Privilege set *',
                                    border: OutlineInputBorder(),
                                    isDense: true,
                                    prefixIcon: Icon(Icons.admin_panel_settings_outlined, size: 18),
                                  ),
                                  items: const [
                                    DropdownMenuItem(
                                      value: 'owner',
                                      child: Text('[Full Access] Owner'),
                                    ),
                                    DropdownMenuItem(
                                      value: 'admin',
                                      child: Text('[Data Entry and Design] Admin'),
                                    ),
                                    DropdownMenuItem(
                                      value: 'user',
                                      child: Text('[Restricted Access] User'),
                                    ),
                                  ],
                                  onChanged: isOwnerLocked ? null : (v) => setDlgState(() => selectedRole = v ?? 'user'),
                                ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 12),

                          // Password & status
                          Row(
                            children: [
                              Expanded(
                                flex: 4,
                                child: TextField(
                                  controller: passwordController,
                                  obscureText: obscurePassword,
                                  decoration: InputDecoration(
                                    labelText: isNew ? 'Initial password *' : 'New password (optional)',
                                    hintText: isNew ? 'Required' : 'Leave blank to keep the current password',
                                    border: const OutlineInputBorder(),
                                    isDense: true,
                                    prefixIcon: const Icon(Icons.lock_outline, size: 18),
                                    suffixIcon: IconButton(
                                      icon: Icon(
                                        obscurePassword ? Icons.visibility_off : Icons.visibility,
                                        size: 18,
                                      ),
                                      onPressed: () => setDlgState(() => obscurePassword = !obscurePassword),
                                      tooltip: obscurePassword ? 'Show password' : 'Hide password',
                                    ),
                                  ),
                                ),
                              ),
                              const SizedBox(width: 12),
                              Expanded(
                                flex: 3,
                                child: Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                                  decoration: BoxDecoration(
                                    border: Border.all(color: Colors.grey.shade700),
                                    borderRadius: BorderRadius.circular(6),
                                  ),
                                  child: Row(
                                    children: [
                                      Expanded(
                                        child: Text(
                                          isActive ? 'Active account' : 'Inactive',
                                          style: TextStyle(
                                            fontSize: 12,
                                            fontWeight: FontWeight.bold,
                                            color: isActive ? Colors.green : Colors.grey,
                                          ),
                                        ),
                                      ),
                                      Switch(
                                        value: isActive,
                                        activeThumbColor: Colors.green,
                                        onChanged: (!isNew && _isCurrentUser(userToEdit))
                                            ? null
                                            : (v) => setDlgState(() => isActive = v),
                                      ),
                                    ],
                                  ),
                                ),
                              ),
                            ],
                          ),

                          // Super-owner on/off switch — grants full, server-wide
                          // access to all Server Tools. A super-owner sees it on
                          // any account (never their own). Super-owner is a
                          // privilege on top of Owner, so turning it on for a
                          // non-owner promotes the account to Owner first.
                          if (_viewerIsSuperOwner && !isNew) ...[
                            const SizedBox(height: 12),
                            Container(
                              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                              decoration: BoxDecoration(
                                border: Border.all(color: isSuperOwner ? Colors.amber.shade700 : Colors.grey.shade700),
                                borderRadius: BorderRadius.circular(6),
                                color: isSuperOwner ? Colors.amber.withValues(alpha: 0.08) : null,
                              ),
                              child: Row(
                                children: [
                                  Icon(isSuperOwner ? Icons.shield : Icons.shield_outlined,
                                      size: 20, color: isSuperOwner ? Colors.amber.shade700 : Colors.grey),
                                  const SizedBox(width: 10),
                                  Expanded(
                                    child: Column(
                                      crossAxisAlignment: CrossAxisAlignment.start,
                                      children: [
                                        const Text('Super-owner', style: TextStyle(fontSize: 13, fontWeight: FontWeight.bold)),
                                        Text(
                                          _isCurrentUser(userToEdit)
                                              ? 'You cannot change your own super-owner status'
                                              : selectedRole == 'owner'
                                                  ? 'ON = full access to all Server Tools (every database); OFF = normal owner'
                                                  : 'ON promotes this account to Owner and grants full Server Tools access',
                                          style: const TextStyle(fontSize: 10.5, color: Colors.grey),
                                        ),
                                      ],
                                    ),
                                  ),
                                  Switch(
                                    value: isSuperOwner,
                                    activeThumbColor: Colors.amber.shade700,
                                    onChanged: _isCurrentUser(userToEdit)
                                        ? null
                                        : (v) async {
                                            try {
                                              // Super-owner implies Owner: promote first when granting.
                                              final promoted = v && selectedRole != 'owner';
                                              if (promoted) {
                                                await widget.apiClient.updateUser(userToEdit.id, role: 'owner', database: widget.databaseName);
                                              }
                                              await widget.apiClient.setUserSuperOwner(userToEdit.id, v, database: widget.databaseName);
                                              widget.onModified?.call();
                                              AutoSaveService.instance.markDirty();
                                              setDlgState(() {
                                                isSuperOwner = v;
                                                if (v) selectedRole = 'owner';
                                              });
                                              final idx = _users.indexWhere((u) => u.id == userToEdit.id);
                                              if (idx != -1) {
                                                setState(() => _users[idx] = _users[idx].copyWith(
                                                      superOwner: v,
                                                      role: v ? 'owner' : _users[idx].role,
                                                    ));
                                              }
                                              if (mounted) {
                                                ScaffoldMessenger.of(context).showSnackBar(SnackBar(
                                                  content: Text(v
                                                      ? (promoted
                                                          ? 'Promoted to Owner and granted super-owner — full Server Tools access. Sessions signed out.'
                                                          : 'Super-owner granted — full Server Tools access. Sessions signed out.')
                                                      : 'Super-owner revoked. Sessions signed out.'),
                                                  backgroundColor: v ? Colors.amber.shade800 : Colors.blueGrey,
                                                  duration: const Duration(seconds: 3),
                                                ));
                                              }
                                            } catch (e) {
                                              setDlgState(() => dialogError = e is ApiException ? e.detail : 'Error: $e');
                                            }
                                          },
                                  ),
                                ],
                              ),
                            ),
                          ],

                          const SizedBox(height: 20),
                          const Divider(),
                          const SizedBox(height: 8),

                          // Table Access & Modification Privileges header
                          Row(
                            children: [
                              const Icon(Icons.table_chart_outlined, size: 18, color: Color(0xFF1E88E5)),
                              const SizedBox(width: 8),
                              const Text(
                                'Table access & modification privileges',
                                style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
                              ),
                              const Spacer(),
                              TextButton(
                                onPressed: () {
                                  setDlgState(() {
                                    for (final t in _tables) {
                                      tablePerms[t.name] = UserTablePermissionModel(
                                        tableName: t.name,
                                        canModifySchema: selectedRole == 'owner' || selectedRole == 'admin',
                                      );
                                    }
                                  });
                                },
                                child: const Text('All Read/Write', style: TextStyle(fontSize: 11)),
                              ),
                              TextButton(
                                onPressed: () {
                                  setDlgState(() {
                                    for (final t in _tables) {
                                      tablePerms[t.name] = UserTablePermissionModel(
                                        tableName: t.name,
                                        accessLevel: 'read_only',
                                        canCreate: false,
                                        canEdit: false,
                                        canDelete: false,
                                      );
                                    }
                                  });
                                },
                                child: const Text('Read Only', style: TextStyle(fontSize: 11)),
                              ),
                            ],
                          ),
                          const SizedBox(height: 8),

                          if (_tables.isEmpty)
                            Container(
                              padding: const EdgeInsets.all(14),
                              width: double.infinity,
                              decoration: BoxDecoration(
                                border: Border.all(color: Colors.grey.shade800),
                                borderRadius: BorderRadius.circular(6),
                              ),
                              child: const Center(
                                child: Text('No tables are defined in this solution yet.', style: TextStyle(color: Colors.grey, fontSize: 12)),
                              ),
                            )
                          else
                            Container(
                              decoration: BoxDecoration(
                                border: Border.all(color: Colors.grey.shade800),
                                borderRadius: BorderRadius.circular(6),
                              ),
                              child: ListView.separated(
                                shrinkWrap: true,
                                physics: const NeverScrollableScrollPhysics(),
                                itemCount: _tables.length,
                                separatorBuilder: (_, _) => Divider(height: 1, color: Colors.grey.shade800),
                                itemBuilder: (ctx, i) {
                                  final t = _tables[i];
                                  final curPerm = tablePerms[t.name] ??
                                      UserTablePermissionModel(tableName: t.name);
                                  final isReadWrite = curPerm.accessLevel == 'read_write';
                                  return Padding(
                                    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                                    child: Column(
                                      crossAxisAlignment: CrossAxisAlignment.start,
                                      children: [
                                        Row(
                                          children: [
                                            const Icon(Icons.table_rows_outlined, size: 16, color: Colors.grey),
                                            const SizedBox(width: 8),
                                            Expanded(
                                              child: Text(
                                                t.displayName.isNotEmpty ? '${t.displayName} (${t.name})' : t.name,
                                                style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
                                              ),
                                            ),
                                            SegmentedButton<String>(
                                              showSelectedIcon: false,
                                              segments: const [
                                                ButtonSegment(
                                                  value: 'read_write',
                                                  label: Text('R/W', style: TextStyle(fontSize: 10)),
                                                  tooltip: 'Read and Write',
                                                ),
                                                ButtonSegment(
                                                  value: 'read_only',
                                                  label: Text('Read', style: TextStyle(fontSize: 10)),
                                                  tooltip: 'Read Only',
                                                ),
                                                ButtonSegment(
                                                  value: 'none',
                                                  label: Text('No access', style: TextStyle(fontSize: 10)),
                                                  tooltip: 'No access',
                                                ),
                                              ],
                                              selected: {curPerm.accessLevel},
                                              onSelectionChanged: (val) {
                                                final nextAccess = val.first;
                                                final allowWrite = nextAccess == 'read_write';
                                                setDlgState(() {
                                                  tablePerms[t.name] = curPerm.copyWith(
                                                    accessLevel: nextAccess,
                                                    canCreate: allowWrite,
                                                    canEdit: allowWrite,
                                                    canDelete: allowWrite,
                                                    canModifySchema: allowWrite && curPerm.canModifySchema,
                                                  );
                                                });
                                              },
                                            ),
                                          ],
                                        ),
                                        if (isReadWrite)
                                          Padding(
                                            padding: const EdgeInsets.only(left: 24, top: 4),
                                            child: Wrap(
                                              spacing: 12,
                                              runSpacing: 4,
                                              children: [
                                                FilterChip(
                                                  label: const Text('Create records', style: TextStyle(fontSize: 10)),
                                                  selected: curPerm.canCreate,
                                                  visualDensity: VisualDensity.compact,
                                                  onSelected: (v) {
                                                    setDlgState(() {
                                                      tablePerms[t.name] = curPerm.copyWith(canCreate: v);
                                                    });
                                                  },
                                                ),
                                                FilterChip(
                                                  label: const Text('Edit records', style: TextStyle(fontSize: 10)),
                                                  selected: curPerm.canEdit,
                                                  visualDensity: VisualDensity.compact,
                                                  onSelected: (v) {
                                                    setDlgState(() {
                                                      tablePerms[t.name] = curPerm.copyWith(canEdit: v);
                                                    });
                                                  },
                                                ),
                                                FilterChip(
                                                  label: const Text('Delete records', style: TextStyle(fontSize: 10)),
                                                  selected: curPerm.canDelete,
                                                  visualDensity: VisualDensity.compact,
                                                  onSelected: (v) {
                                                    setDlgState(() {
                                                      tablePerms[t.name] = curPerm.copyWith(canDelete: v);
                                                    });
                                                  },
                                                ),
                                                FilterChip(
                                                  label: const Text('Modify schema', style: TextStyle(fontSize: 10)),
                                                  selected: curPerm.canModifySchema,
                                                  visualDensity: VisualDensity.compact,
                                                  onSelected: (v) {
                                                    setDlgState(() {
                                                      tablePerms[t.name] = curPerm.copyWith(canModifySchema: v);
                                                    });
                                                  },
                                                ),
                                              ],
                                            ),
                                          ),
                                      ],
                                    ),
                                  );
                                },
                              ),
                            ),

                          const SizedBox(height: 16),
                          const Divider(),
                          const SizedBox(height: 8),

                          // Layout Permissions header
                          Row(
                            children: [
                              const Icon(Icons.dashboard_customize_outlined, size: 18, color: Color(0xFF1E88E5)),
                              const SizedBox(width: 8),
                              const Text(
                                'Layout access privileges',
                                style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
                              ),
                              const Spacer(),
                              TextButton(
                                onPressed: () {
                                  setDlgState(() {
                                    for (final l in _layouts) {
                                      layoutPerms[l.id] = 'read_write';
                                    }
                                  });
                                },
                                child: const Text('All Read/Write', style: TextStyle(fontSize: 11)),
                              ),
                              TextButton(
                                onPressed: () {
                                  setDlgState(() {
                                    for (final l in _layouts) {
                                      layoutPerms[l.id] = 'read_only';
                                    }
                                  });
                                },
                                child: const Text('Read Only', style: TextStyle(fontSize: 11)),
                              ),
                            ],
                          ),
                          const SizedBox(height: 8),

                          if (_layouts.isEmpty)
                            Container(
                              padding: const EdgeInsets.all(16),
                              width: double.infinity,
                              decoration: BoxDecoration(
                                border: Border.all(color: Colors.grey.shade800),
                                borderRadius: BorderRadius.circular(6),
                              ),
                              child: const Center(
                                child: Text('No layouts are defined in this solution yet.', style: TextStyle(color: Colors.grey, fontSize: 12)),
                              ),
                            )
                          else
                            Container(
                              decoration: BoxDecoration(
                                border: Border.all(color: Colors.grey.shade800),
                                borderRadius: BorderRadius.circular(6),
                              ),
                              child: ListView.separated(
                                shrinkWrap: true,
                                physics: const NeverScrollableScrollPhysics(),
                                itemCount: _layouts.length,
                                separatorBuilder: (_, _) => Divider(height: 1, color: Colors.grey.shade800),
                                itemBuilder: (ctx, i) {
                                  final l = _layouts[i];
                                  final curPerm = layoutPerms[l.id] ?? 'read_write';
                                  return Padding(
                                    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                                    child: Row(
                                      children: [
                                        const Icon(Icons.grid_view, size: 16, color: Colors.grey),
                                        const SizedBox(width: 8),
                                        Expanded(
                                          child: Text(l.name, style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600)),
                                        ),
                                        SegmentedButton<String>(
                                          showSelectedIcon: false,
                                          segments: const [
                                            ButtonSegment(
                                              value: 'read_write',
                                              label: Text('R/W', style: TextStyle(fontSize: 10)),
                                              tooltip: 'Read and Write',
                                            ),
                                            ButtonSegment(
                                              value: 'read_only',
                                              label: Text('Read', style: TextStyle(fontSize: 10)),
                                              tooltip: 'Read Only',
                                            ),
                                            ButtonSegment(
                                              value: 'none',
                                              label: Text('No access', style: TextStyle(fontSize: 10)),
                                              tooltip: 'No access',
                                            ),
                                          ],
                                          selected: {curPerm},
                                          onSelectionChanged: (val) {
                                            setDlgState(() => layoutPerms[l.id] = val.first);
                                          },
                                        ),
                                      ],
                                    ),
                                  );
                                },
                              ),
                            ),
                        ],
                      ),
                    ),
            ),
            actions: [
              TextButton(
                onPressed: isSaving ? null : () => Navigator.of(ctx).pop(),
                child: const Text('Cancel'),
              ),
              ElevatedButton.icon(
                style: ElevatedButton.styleFrom(
                  backgroundColor: const Color(0xFF1E88E5),
                  foregroundColor: Colors.white,
                ),
                icon: const Icon(Icons.check, size: 18),
                label: Text(isNew ? 'Create account' : 'Save changes'),
                onPressed: isSaving
                    ? null
                    : () async {
                        final uname = usernameController.text.trim();
                        final pass = passwordController.text.trim();

                        if (uname.isEmpty) {
                          setDlgState(() => dialogError = 'Username cannot be empty.');
                          return;
                        }
                        if (isNew && pass.isEmpty) {
                          setDlgState(() => dialogError = 'A password is required for new accounts.');
                          return;
                        }

                        setDlgState(() {
                          isSaving = true;
                          dialogError = null;
                        });

                        try {
                          String targetId;
                          if (isNew) {
                            final created = await widget.apiClient.createUser(
                              username: uname,
                              password: pass,
                              role: selectedRole,
                              isActive: isActive,
                              database: widget.databaseName,
                            );
                            targetId = created.id;
                          } else {
                            targetId = userToEdit.id;
                            await widget.apiClient.updateUser(
                              targetId,
                              role: selectedRole,
                              password: pass.isNotEmpty ? pass : null,
                              isActive: isActive,
                              database: widget.databaseName,
                            );
                          }

                          // Save layout permissions
                          final permsList = layoutPerms.entries.map((e) {
                            return {
                              'layout_id': e.key,
                              'access_level': e.value,
                            };
                          }).toList();
                          await widget.apiClient.setUserPermissions(targetId, permsList, database: widget.databaseName);

                          // Save table permissions
                          if (tablePerms.isNotEmpty) {
                            try {
                              await widget.apiClient.setUserTablePermissions(
                                targetId,
                                tablePerms.values.toList(),
                                database: widget.databaseName,
                              );
                            } catch (_) {}
                          }

                          widget.onModified?.call();
                          AutoSaveService.instance.markDirty();

                          if (ctx.mounted) Navigator.of(ctx).pop();
                          await _loadData();

                          if (mounted) {
                            ScaffoldMessenger.of(context).showSnackBar(
                              SnackBar(
                                content: Text('Account "$uname" saved successfully.'),
                                backgroundColor: Colors.green.shade700,
                              ),
                            );
                          }
                        } catch (e) {
                          setDlgState(() {
                            isSaving = false;
                            dialogError = e.toString().replaceFirst('Exception: ', '');
                          });
                        }
                      },
              ),
            ],
          );
        },
      ),
    );
  }

  Future<void> _duplicateUser(UserModel user) async {
    final cloneName = '${user.username}_copy';
    final passwordController = TextEditingController();
    String? dialogError;
    var obscurePassword = true;

    await showDialog(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDlgState) => AlertDialog(
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
          title: Row(
            children: [
              const Icon(Icons.content_copy_outlined, color: Color(0xFF1E88E5), size: 22),
              const SizedBox(width: 8),
              Text('Duplicate account: ${user.username}', style: const TextStyle(fontSize: 16)),
            ],
          ),
          content: SizedBox(
            width: 400,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'A duplicate will be created with the "${user.role.toUpperCase()}" role and the same table and layout privileges.',
                  style: const TextStyle(fontSize: 12, color: Colors.grey),
                ),
                const SizedBox(height: 16),
                Text('New account name: $cloneName', style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                const SizedBox(height: 12),
                TextField(
                  controller: passwordController,
                  obscureText: obscurePassword,
                  decoration: InputDecoration(
                    labelText: 'Password for the new account *',
                    border: const OutlineInputBorder(),
                    isDense: true,
                    prefixIcon: const Icon(Icons.lock_outline, size: 18),
                    suffixIcon: IconButton(
                      icon: Icon(
                        obscurePassword ? Icons.visibility_off : Icons.visibility,
                        size: 18,
                      ),
                      onPressed: () => setDlgState(() => obscurePassword = !obscurePassword),
                      tooltip: obscurePassword ? 'Show password' : 'Hide password',
                    ),
                  ),
                ),
                if (dialogError != null) ...[
                  const SizedBox(height: 10),
                  Text(dialogError!, style: const TextStyle(color: Colors.red, fontSize: 12)),
                ],
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(ctx).pop(),
              child: const Text('Cancel'),
            ),
            ElevatedButton.icon(
              style: ElevatedButton.styleFrom(
                backgroundColor: const Color(0xFF1E88E5),
                foregroundColor: Colors.white,
              ),
              icon: const Icon(Icons.copy, size: 18),
              label: const Text('Duplicate'),
              onPressed: () async {
                final pass = passwordController.text.trim();
                if (pass.isEmpty) {
                  setDlgState(() => dialogError = 'Enter a password for the duplicated account.');
                  return;
                }

                try {
                  final newUser = await widget.apiClient.createUser(
                    username: cloneName,
                    password: pass,
                    role: user.role,
                    database: widget.databaseName,
                  );

                  // Copy layout permissions from original
                  final originalPerms = _userPermissionsMap[user.id] ?? [];
                  if (originalPerms.isNotEmpty) {
                    final payload = originalPerms.map((p) => {
                      'layout_id': p.layoutId,
                      'access_level': p.accessLevel,
                    }).toList();
                    await widget.apiClient.setUserPermissions(newUser.id, payload, database: widget.databaseName);
                  }

                  // Copy table permissions from original
                  final originalTablePerms = _userTablePermissionsMap[user.id] ?? [];
                  if (originalTablePerms.isNotEmpty) {
                    try {
                      await widget.apiClient.setUserTablePermissions(
                        newUser.id,
                        originalTablePerms,
                        database: widget.databaseName,
                      );
                    } catch (_) {}
                  }

                  widget.onModified?.call();
                  AutoSaveService.instance.markDirty();

                  if (ctx.mounted) Navigator.of(ctx).pop();
                  await _loadData();

                  if (mounted) {
                    ScaffoldMessenger.of(context).showSnackBar(
                      SnackBar(
                        content: Text('Account duplicated successfully: $cloneName'),
                        backgroundColor: Colors.green.shade700,
                      ),
                    );
                  }
                } catch (e) {
                  setDlgState(() => dialogError = e.toString().replaceFirst('Exception: ', ''));
                }
              },
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _deleteUser(UserModel user) async {
    if (_isCurrentUser(user)) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('You cannot delete your current account.'),
          backgroundColor: Colors.red,
        ),
      );
      return;
    }

    if (user.role == 'owner') {
      final owners = _users.where((u) => u.role == 'owner').length;
      if (owners <= 1) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('The only Owner account with full access cannot be deleted.'),
            backgroundColor: Colors.red,
          ),
        );
        return;
      }
    }

    final confirm = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
        title: const Row(
          children: [
            Icon(Icons.warning_amber_rounded, color: Colors.red, size: 24),
            SizedBox(width: 8),
            Text('Delete user account'),
          ],
        ),
        content: Text(
          'Permanently delete account "${user.username}"?\nThis action cannot be undone.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancel')),
          ElevatedButton(
            style: ElevatedButton.styleFrom(backgroundColor: Colors.red, foregroundColor: Colors.white),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Delete permanently'),
          ),
        ],
      ),
    );

    if (confirm != true) return;

    try {
      await widget.apiClient.deleteUser(user.id, database: widget.databaseName);
      widget.onModified?.call();
      AutoSaveService.instance.markDirty();
      await _loadData();
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Account "${user.username}" deleted successfully.'),
            backgroundColor: Colors.blueGrey,
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Error deleting user: $e'),
            backgroundColor: Colors.red,
          ),
        );
      }
    }
  }

  Widget _buildRoleBadge(String role) {
    Color bg;
    Color fg;
    String label;
    IconData icon;

    switch (role) {
      case 'owner':
        bg = Colors.amber.shade900.withValues(alpha: 0.25);
        fg = Colors.amber.shade400;
        label = '[Full Access] Owner';
        icon = Icons.verified_user_outlined;
      case 'admin':
        bg = Colors.blue.shade900.withValues(alpha: 0.25);
        fg = Colors.blue.shade300;
        label = '[Data Entry and Design] Admin';
        icon = Icons.design_services_outlined;
      default:
        bg = Colors.teal.shade900.withValues(alpha: 0.25);
        fg = Colors.teal.shade300;
        label = '[Restricted Access] User';
        icon = Icons.person_outline;
    }

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(6),
        border: Border.all(color: fg.withValues(alpha: 0.4)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 14, color: fg),
          const SizedBox(width: 5),
          Flexible(
            child: Text(
              label,
              style: TextStyle(fontSize: 11, fontWeight: FontWeight.w600, color: fg),
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final activeDb = widget.databaseName ?? '';

    return Dialog(
      backgroundColor: Colors.transparent,
      child: Container(
        width: 960,
        height: 680,
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.surface,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: Colors.grey.shade700),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: 0.5),
              blurRadius: 28,
              offset: const Offset(0, 10),
            ),
          ],
        ),
        child: Column(
          children: [
            // Top Window Bar
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
              decoration: BoxDecoration(
                color: const Color(0xFF1E88E5).withValues(alpha: 0.08),
                borderRadius: const BorderRadius.vertical(top: Radius.circular(12)),
                border: Border(bottom: BorderSide(color: Colors.grey.shade800)),
              ),
              child: Row(
                children: [
                  const Icon(Icons.shield_outlined, color: Color(0xFF1E88E5), size: 24),
                  const SizedBox(width: 10),
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          const Text(
                            'Manage Security',
                            style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16),
                          ),
                          const SizedBox(width: 10),
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                            decoration: BoxDecoration(
                              color: Colors.blue.withValues(alpha: 0.15),
                              borderRadius: BorderRadius.circular(12),
                              border: Border.all(color: Colors.blue.shade400.withValues(alpha: 0.4)),
                            ),
                            child: Row(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                const Icon(Icons.dns_outlined, size: 12, color: Colors.blueAccent),
                                const SizedBox(width: 4),
                                Text(
                                  activeDb,
                                  style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.blueAccent),
                                ),
                              ],
                            ),
                          ),
                        ],
                      ),
                      const Text(
                        'Accounts, privilege sets, and access permissions for each database',
                        style: TextStyle(fontSize: 11, color: Colors.grey),
                      ),
                    ],
                  ),
                  const Spacer(),
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
                color: Colors.black12,
                border: Border(bottom: BorderSide(color: Colors.grey.shade800)),
              ),
              child: TabBar(
                controller: _tabController,
                isScrollable: true,
                tabAlignment: TabAlignment.start,
                labelColor: const Color(0xFF1E88E5),
                unselectedLabelColor: Colors.grey,
                indicatorColor: const Color(0xFF1E88E5),
                tabs: [
                  Tab(
                    child: Row(
                      children: [
                        const Icon(Icons.people_outline, size: 16),
                        const SizedBox(width: 6),
                        const Text('Accounts'),
                        const SizedBox(width: 6),
                        Container(
                          padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
                          decoration: BoxDecoration(
                            color: Colors.grey.shade800,
                            borderRadius: BorderRadius.circular(10),
                          ),
                          child: Text('${_users.length}', style: const TextStyle(fontSize: 10, fontWeight: FontWeight.bold)),
                        ),
                      ],
                    ),
                  ),
                  const Tab(
                    child: Row(
                      children: [
                        Icon(Icons.dashboard_customize_outlined, size: 16),
                        SizedBox(width: 6),
                        Text('Layout privileges'),
                      ],
                    ),
                  ),
                  const Tab(
                    child: Row(
                      children: [
                        Icon(Icons.public, size: 16),
                        SizedBox(width: 6),
                        Text('Extended privileges'),
                      ],
                    ),
                  ),
                ],
              ),
            ),

            if (_errorMessage != null)
              Container(
                width: double.infinity,
                padding: const EdgeInsets.all(8),
                color: Colors.red.shade900.withValues(alpha: 0.3),
                child: Text(_errorMessage!, style: const TextStyle(color: Colors.red, fontSize: 12), textAlign: TextAlign.center),
              ),

            // Main Tab View
            Expanded(
              child: _isLoading
                  ? const Center(child: CircularProgressIndicator())
                  : TabBarView(
                      controller: _tabController,
                      children: [
                        // Tab 1: Accounts List / Table
                        _buildAccountsTab(),

                        // Tab 2: Layout Privileges
                        _buildLayoutPrivilegesTab(),

                        // Tab 3: Extended Privileges
                        _buildExtendedPrivilegesTab(),
                      ],
                    ),
            ),

            // Bottom Status / Footer Bar
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
              decoration: BoxDecoration(
                color: Colors.black12,
                borderRadius: const BorderRadius.vertical(bottom: Radius.circular(12)),
                border: Border(top: BorderSide(color: Colors.grey.shade800)),
              ),
              child: Row(
                children: [
                  Text(
                    'Total: ${_users.length} accounts  •  $_activeCount active  •  $_inactiveCount inactive',
                    style: const TextStyle(fontSize: 12, color: Colors.grey),
                  ),
                  const Spacer(),
                  OutlinedButton(
                    onPressed: () => Navigator.of(context).pop(),
                    style: OutlinedButton.styleFrom(
                      padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 12),
                    ),
                    child: const Text('Close'),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildAccountsTab() {
    final filtered = _filteredUsers;

    return Padding(
      padding: const EdgeInsets.all(16),
      child: Column(
        children: [
          // Toolbar: Search + Filter + New Button
          Row(
            children: [
              // Search input
              Expanded(
                child: TextField(
                  onChanged: (val) => setState(() => _searchQuery = val.trim()),
                  decoration: InputDecoration(
                    hintText: 'Search accounts by name or role...',
                    hintStyle: const TextStyle(fontSize: 12),
                    prefixIcon: const Icon(Icons.search, size: 18),
                    border: OutlineInputBorder(borderRadius: BorderRadius.circular(8)),
                    contentPadding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                    isDense: true,
                  ),
                ),
              ),
              const SizedBox(width: 12),

              // Filter pills
              SegmentedButton<String>(
                showSelectedIcon: false,
                segments: const [
                  ButtonSegment(
                    value: 'all',
                    label: Text('All', style: TextStyle(fontSize: 11)),
                  ),
                  ButtonSegment(
                    value: 'active',
                    label: Text('Active', style: TextStyle(fontSize: 11)),
                  ),
                  ButtonSegment(
                    value: 'inactive',
                    label: Text('Inactive', style: TextStyle(fontSize: 11)),
                  ),
                ],
                selected: {_statusFilter},
                onSelectionChanged: (set) => setState(() => _statusFilter = set.first),
              ),
              const SizedBox(width: 12),

              // New Account Button
              ElevatedButton.icon(
                onPressed: _showAccountDialog,
                style: ElevatedButton.styleFrom(
                  backgroundColor: const Color(0xFF1E88E5),
                  foregroundColor: Colors.white,
                  padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                ),
                icon: const Icon(Icons.person_add_alt, size: 18),
                label: const Text('New account...'),
              ),
            ],
          ),
          const SizedBox(height: 14),

          // Accounts Table
          Expanded(
            child: Container(
              decoration: BoxDecoration(
                border: Border.all(color: Colors.grey.shade800),
                borderRadius: BorderRadius.circular(8),
              ),
              child: ClipRRect(
                borderRadius: BorderRadius.circular(8),
                child: Column(
                  children: [
                    // Table Header
                    Container(
                      color: Colors.black26,
                      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                      child: Row(
                        children: [
                          const SizedBox(width: 40, child: Text('#', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.grey))),
                          SizedBox(width: 140, child: Text('STATUS', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.grey))),
                          Expanded(flex: 3, child: Text('ACCOUNT NAME', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.grey))),
                          Expanded(flex: 3, child: Text('PRIVILEGE SET', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.grey))),
                          Expanded(flex: 2, child: Text('LAYOUTS', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.grey))),
                          SizedBox(width: _optionsColumnWidth, child: const Text('OPTIONS', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.grey))),
                        ],
                      ),
                    ),
                    const Divider(height: 1),

                    // Table Body
                    Expanded(
                      child: filtered.isEmpty
                          ? Center(
                              child: Column(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  const Icon(Icons.search_off, size: 36, color: Colors.grey),
                                  const SizedBox(height: 8),
                                  Text(
                                    _searchQuery.isNotEmpty ? 'No accounts match "$_searchQuery"' : 'No accounts registered',
                                    style: const TextStyle(color: Colors.grey, fontSize: 13),
                                  ),
                                ],
                              ),
                            )
                          : ListView.separated(
                              itemCount: filtered.length,
                              separatorBuilder: (_, _) => Divider(height: 1, color: Colors.grey.shade900),
                              itemBuilder: (ctx, idx) {
                                final user = filtered[idx];
                                final isCurrent = _isCurrentUser(user);
                                final isLastOwner = _isLastActiveOwner(user);
                                final perms = _userPermissionsMap[user.id] ?? [];
                                final accessibleLayouts = perms.where((p) => p.accessLevel != 'none').length;

                                return Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                                  color: idx % 2 == 0 ? Colors.transparent : Colors.black.withValues(alpha: 0.05),
                                  child: Row(
                                    children: [
                                      // 1. Numerical Sequence
                                      SizedBox(
                                        width: 40,
                                        child: Text(
                                          '${idx + 1}',
                                          style: const TextStyle(fontSize: 12, fontWeight: FontWeight.bold, color: Colors.grey),
                                        ),
                                      ),

                                      // 2. Active Toggle / Switch
                                      SizedBox(
                                        width: 140,
                                        child: Row(
                                          children: [
                                            SizedBox(
                                              width: 44,
                                              height: 28,
                                              child: FittedBox(
                                                child: Switch(
                                                  value: user.isActive,
                                                  activeThumbColor: Colors.green,
                                                  materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
                                                  onChanged: (isCurrent || (user.isActive && isLastOwner))
                                                      ? null
                                                      : (val) => _toggleUserActive(user, val),
                                                ),
                                              ),
                                            ),
                                            const SizedBox(width: 6),
                                            Text(
                                              user.isActive ? 'Active' : 'Inactive',
                                              style: TextStyle(
                                                fontSize: 11,
                                                fontWeight: FontWeight.bold,
                                                color: user.isActive ? Colors.green : Colors.grey,
                                              ),
                                            ),
                                          ],
                                        ),
                                      ),

                                      // 3. Account Name
                                      Expanded(
                                        flex: 3,
                                        child: Row(
                                          children: [
                                            CircleAvatar(
                                              radius: 14,
                                              backgroundColor: user.role == 'owner'
                                                  ? Colors.amber.shade800
                                                  : (user.role == 'admin' ? Colors.blue.shade800 : Colors.teal.shade800),
                                              child: Text(
                                                user.username.isNotEmpty ? user.username[0].toUpperCase() : 'U',
                                                style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.white),
                                              ),
                                            ),
                                            const SizedBox(width: 8),
                                            Flexible(
                                              child: Column(
                                                crossAxisAlignment: CrossAxisAlignment.start,
                                                children: [
                                                  Row(
                                                    mainAxisSize: MainAxisSize.min,
                                                    children: [
                                                      Flexible(
                                                        child: Text(
                                                          user.username,
                                                          overflow: TextOverflow.ellipsis,
                                                          style: TextStyle(
                                                            fontSize: 13,
                                                            fontWeight: FontWeight.bold,
                                                            color: user.isActive ? null : Colors.grey,
                                                          ),
                                                        ),
                                                      ),
                                                      if (isCurrent) ...[
                                                        const SizedBox(width: 6),
                                                        Container(
                                                          padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
                                                          decoration: BoxDecoration(
                                                            color: Colors.green.shade900.withValues(alpha: 0.3),
                                                            borderRadius: BorderRadius.circular(10),
                                                            border: Border.all(color: Colors.green.shade600, width: 0.8),
                                                          ),
                                                          child: const Text(
                                                            'You / Active',
                                                            style: TextStyle(fontSize: 9, fontWeight: FontWeight.bold, color: Colors.greenAccent),
                                                          ),
                                                        ),
                                                      ],
                                                      if (user.mfaEnabled) ...[
                                                        const SizedBox(width: 6),
                                                        Container(
                                                          padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
                                                          decoration: BoxDecoration(
                                                            color: Colors.teal.shade900.withValues(alpha: 0.35),
                                                            borderRadius: BorderRadius.circular(10),
                                                            border: Border.all(color: Colors.tealAccent.shade400, width: 0.8),
                                                          ),
                                                          child: const Row(
                                                            mainAxisSize: MainAxisSize.min,
                                                            children: [
                                                              Icon(Icons.verified_user, size: 10, color: Colors.tealAccent),
                                                              SizedBox(width: 3),
                                                              Text(
                                                                '2FA / MFA',
                                                                style: TextStyle(fontSize: 9, fontWeight: FontWeight.bold, color: Colors.tealAccent),
                                                              ),
                                                            ],
                                                          ),
                                                        ),
                                                      ],
                                                    ],
                                                  ),
                                                  if (user.createdAt != null)
                                                    Text(
                                                      'Created: ${user.createdAt!.toLocal().toString().split('.').first}',
                                                      style: const TextStyle(fontSize: 10, color: Colors.grey),
                                                    ),
                                                ],
                                              ),
                                            ),
                                          ],
                                        ),
                                      ),

                                      // 4. Privilege Set Badge
                                      Expanded(
                                        flex: 3,
                                        child: Align(
                                          alignment: Alignment.centerLeft,
                                          child: _buildRoleBadge(user.role),
                                        ),
                                      ),

                                      // 5. Layout Privileges Summary
                                      Expanded(
                                        flex: 2,
                                        child: Text(
                                          _layouts.isEmpty
                                              ? 'No layouts'
                                              : (user.role == 'owner'
                                                  ? 'All (${_layouts.length})'
                                                  : '$accessibleLayouts of ${_layouts.length} accessible'),
                                          style: const TextStyle(fontSize: 12, color: Colors.grey),
                                        ),
                                      ),

                                      // 6. Action Icons
                                      SizedBox(
                                        width: _optionsColumnWidth,
                                        child: Row(
                                          mainAxisAlignment: MainAxisAlignment.end,
                                          children: [
                                            // Super-owner (server-wide control).
                                            // Only a super-owner may grant or
                                            // revoke it, and only on an owner.
                                            if (_viewerIsSuperOwner && user.isOwner) ...[
                                              IconButton(
                                                visualDensity: VisualDensity.compact,
                                                padding: const EdgeInsets.all(4),
                                                constraints: const BoxConstraints(),
                                                icon: Icon(
                                                  user.superOwner ? Icons.shield : Icons.shield_outlined,
                                                  size: 18,
                                                  color: user.superOwner ? Colors.amberAccent : null,
                                                ),
                                                tooltip: isCurrent
                                                    ? 'You are a super-owner (another super-owner must revoke it)'
                                                    : user.superOwner
                                                        ? 'Super-owner — click to revoke server-wide control'
                                                        : 'Grant super-owner (full server-wide control)',
                                                onPressed: isCurrent ? null : () => _toggleSuperOwner(user),
                                              ),
                                              const SizedBox(width: 2),
                                            ],

                                            // Google Authenticator MFA
                                            IconButton(
                                              visualDensity: VisualDensity.compact,
                                              padding: const EdgeInsets.all(4),
                                              constraints: const BoxConstraints(),
                                              icon: Icon(
                                                user.mfaEnabled ? Icons.phonelink_lock : Icons.qr_code_2_outlined,
                                                size: 18,
                                                color: user.mfaEnabled ? Colors.tealAccent : null,
                                              ),
                                              tooltip: user.mfaEnabled
                                                  ? 'MFA Active (Google Authenticator) — Click to manage'
                                                  : 'Activate MFA with Google Authenticator',
                                              onPressed: () => _showMfaDialog(user),
                                            ),
                                            const SizedBox(width: 2),

                                            // Change Password
                                            IconButton(
                                              visualDensity: VisualDensity.compact,
                                              padding: const EdgeInsets.all(4),
                                              constraints: const BoxConstraints(),
                                              icon: const Icon(Icons.key_outlined, size: 18),
                                              tooltip: 'Change password',
                                              onPressed: () => _showChangePasswordDialog(user),
                                            ),
                                            const SizedBox(width: 2),

                                            // Edit Account
                                            IconButton(
                                              visualDensity: VisualDensity.compact,
                                              padding: const EdgeInsets.all(4),
                                              constraints: const BoxConstraints(),
                                              icon: const Icon(Icons.edit_outlined, size: 18),
                                              tooltip: 'Edit account and privileges',
                                              onPressed: () => _showAccountDialog(userToEdit: user),
                                            ),
                                            const SizedBox(width: 2),

                                            // Duplicate Account
                                            IconButton(
                                              visualDensity: VisualDensity.compact,
                                              padding: const EdgeInsets.all(4),
                                              constraints: const BoxConstraints(),
                                              icon: const Icon(Icons.content_copy_outlined, size: 18),
                                              tooltip: 'Duplicate account',
                                              onPressed: () => _duplicateUser(user),
                                            ),
                                            const SizedBox(width: 2),

                                            // Delete Account
                                            IconButton(
                                              visualDensity: VisualDensity.compact,
                                              padding: const EdgeInsets.all(4),
                                              constraints: const BoxConstraints(),
                                              icon: Icon(
                                                Icons.delete_outline,
                                                size: 18,
                                                color: (isCurrent || (user.role == 'owner' && _users.where((u) => u.role == 'owner').length <= 1))
                                                    ? Colors.grey.shade700
                                                    : Colors.redAccent,
                                              ),
                                              tooltip: isCurrent ? 'You cannot delete your own account' : 'Delete account',
                                              onPressed: (isCurrent || (user.role == 'owner' && _users.where((u) => u.role == 'owner').length <= 1))
                                                  ? null
                                                  : () => _deleteUser(user),
                                            ),
                                          ],
                                        ),
                                      ),
                                    ],
                                  ),
                                );
                              },
                            ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildLayoutPrivilegesTab() {
    return Padding(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(Icons.visibility_outlined, size: 20, color: Color(0xFF1E88E5)),
              const SizedBox(width: 8),
              const Text(
                'Layout Privileges Matrix',
                style: TextStyle(fontWeight: FontWeight.bold, fontSize: 14),
              ),
              const Spacer(),
              Text(
                '${_layouts.length} layouts in the database',
                style: const TextStyle(fontSize: 12, color: Colors.grey),
              ),
            ],
          ),
          const SizedBox(height: 6),
          const Text(
            'View the access level each registered account has for every layout.',
            style: TextStyle(fontSize: 12, color: Colors.grey),
          ),
          const SizedBox(height: 14),

          Expanded(
            child: _layouts.isEmpty
                ? const Center(child: Text('No layouts are registered in this solution.', style: TextStyle(color: Colors.grey)))
                : Container(
                    decoration: BoxDecoration(
                      border: Border.all(color: Colors.grey.shade800),
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: ListView.separated(
                      itemCount: _layouts.length,
                      separatorBuilder: (_, _) => Divider(height: 1, color: Colors.grey.shade800),
                      itemBuilder: (ctx, i) {
                        final l = _layouts[i];
                        return ExpansionTile(
                          leading: const Icon(Icons.dashboard_customize_outlined, color: Color(0xFF1E88E5)),
                          title: Text(l.name, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                          subtitle: Text('ID: ${l.id}  •  Occurrence: ${l.tableOccurrenceId}', style: const TextStyle(fontSize: 11, color: Colors.grey)),
                          children: [
                            Container(
                              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                              color: Colors.black12,
                              child: Column(
                                children: _users.map((u) {
                                  final perms = _userPermissionsMap[u.id] ?? [];
                                  final p = perms.firstWhere(
                                    (item) => item.layoutId == l.id,
                                    orElse: () => UserLayoutPermissionModel(id: '', userId: u.id, layoutId: l.id, accessLevel: u.role == 'owner' || u.role == 'admin' ? 'read_write' : 'read_write'),
                                  );

                                  Color badgeColor = Colors.green;
                                  var badgeText = 'Read and Write';
                                  if (p.accessLevel == 'read_only') {
                                    badgeColor = Colors.blue;
                                    badgeText = 'Read Only';
                                  } else if (p.accessLevel == 'none') {
                                    badgeColor = Colors.red;
                                    badgeText = 'No access';
                                  }

                                  return Padding(
                                    padding: const EdgeInsets.symmetric(vertical: 4),
                                    child: Row(
                                      children: [
                                        Text(u.username, style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600)),
                                        const SizedBox(width: 8),
                                        Text('(${u.role.toUpperCase()})', style: const TextStyle(fontSize: 10, color: Colors.grey)),
                                        const Spacer(),
                                        Container(
                                          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                                          decoration: BoxDecoration(
                                            color: badgeColor.withValues(alpha: 0.2),
                                            borderRadius: BorderRadius.circular(4),
                                            border: Border.all(color: badgeColor.withValues(alpha: 0.5)),
                                          ),
                                          child: Text(badgeText, style: TextStyle(fontSize: 11, color: badgeColor, fontWeight: FontWeight.bold)),
                                        ),
                                      ],
                                    ),
                                  );
                                }).toList(),
                              ),
                            ),
                          ],
                        );
                      },
                    ),
                  ),
          ),
        ],
      ),
    );
  }

  Widget _buildExtendedPrivilegesTab() {
    final isOwner = widget.currentUser.role == 'owner';
    return Padding(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Row(
            children: [
              Icon(Icons.shield_outlined, size: 20, color: Color(0xFF1E88E5)),
              SizedBox(width: 8),
              Text(
                'Extended Privileges',
                style: TextStyle(fontWeight: FontWeight.bold, fontSize: 14),
              ),
            ],
          ),
          const SizedBox(height: 6),
          const Text(
            'Which bulk operations each account role may perform. Enforced by the '
            'server on every request, whichever client makes it.',
            style: TextStyle(fontSize: 12, color: Colors.grey),
          ),
          const SizedBox(height: 12),
          Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: Colors.blue.withValues(alpha: 0.08),
              borderRadius: BorderRadius.circular(8),
              border: Border.all(color: Colors.blue.withValues(alpha: 0.4)),
            ),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(Icons.info_outline, size: 18, color: Colors.blue.shade800),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    'File4Base restricts access by account role, by table and layout, and '
                    'by the privileges below. It does not restrict access by client: the '
                    'desktop client, the Web Client and a direct REST call all speak to the '
                    'same API with the same session token, so the server cannot tell them '
                    'apart and a setting that claimed to would restrict nothing.',
                    style: TextStyle(fontSize: 12, color: Colors.blue.shade900),
                  ),
                ),
              ],
            ),
          ),
          if (!isOwner) ...[
            const SizedBox(height: 10),
            Row(
              children: [
                Icon(Icons.lock_outline, size: 16, color: Colors.orange.shade700),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    'Only an owner can change these privileges.',
                    style: TextStyle(fontSize: 12, color: Colors.orange.shade800),
                  ),
                ),
              ],
            ),
          ],
          const SizedBox(height: 16),

          Expanded(
            child: ListView(
              children: [
                for (final capability in Capability.all)
                  _buildCapabilityCard(capability, isOwner),
              ],
            ),
          ),
        ],
      ),
    );
  }

  /// One capability, with a switch per role. The owner's switch is shown on
  /// and locked: an owner always holds every privilege, so that they cannot
  /// lock themselves out of their own database.
  Widget _buildCapabilityCard(String capability, bool isOwner) {
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(8),
        side: BorderSide(color: Colors.grey.shade800),
      ),
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(
                  capability == Capability.bulkExport
                      ? Icons.file_download_outlined
                      : Icons.file_upload_outlined,
                  size: 28,
                  color: const Color(0xFF1E88E5),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Text(
                            Capability.label(capability),
                            style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
                          ),
                          const SizedBox(width: 8),
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
                            decoration: BoxDecoration(
                              color: Colors.grey.shade800,
                              borderRadius: BorderRadius.circular(4),
                            ),
                            child: Text(
                              capability,
                              style: const TextStyle(
                                fontSize: 10,
                                fontFamily: 'monospace',
                                color: Colors.white70,
                              ),
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 4),
                      Text(
                        Capability.description(capability),
                        style: const TextStyle(fontSize: 11, color: Colors.grey),
                      ),
                    ],
                  ),
                ),
              ],
            ),
            const Divider(height: 22),
            Wrap(
              spacing: 24,
              runSpacing: 4,
              children: [
                for (final privileges in _rolePrivileges)
                  _buildCapabilitySwitch(privileges, capability, isOwner),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildCapabilitySwitch(RolePrivilegesModel privileges, String capability, bool isOwner) {
    final locked = privileges.role == 'owner' || !isOwner || _savingPrivileges;
    return SizedBox(
      width: 180,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Switch(
            value: privileges.allows(capability),
            onChanged: locked ? null : (value) => _setCapability(privileges.role, capability, value),
          ),
          const SizedBox(width: 4),
          Text(_roleLabel(privileges.role), style: const TextStyle(fontSize: 12)),
          if (privileges.role == 'owner') ...[
            const SizedBox(width: 4),
            Tooltip(
              message: 'An owner always holds every privilege.',
              child: Icon(Icons.lock_outline, size: 14, color: Colors.grey.shade600),
            ),
          ],
        ],
      ),
    );
  }

  String _roleLabel(String role) {
    switch (role) {
      case 'owner':
        return 'Owner';
      case 'admin':
        return 'Admin';
      case 'user':
        return 'User';
      default:
        return role;
    }
  }

  /// Grants or withholds one capability for one role and stores it at once:
  /// a switch that only changed a local variable would be the very problem
  /// this tab had (#39).
  Future<void> _setCapability(String role, String capability, bool allowed) async {
    final updated = _rolePrivileges
        .map((p) => p.role == role ? p.withCapability(capability, allowed) : p)
        .toList();
    setState(() {
      _rolePrivileges = updated;
      _savingPrivileges = true;
    });
    try {
      final stored = await widget.apiClient.setRolePrivileges(
        [updated.firstWhere((p) => p.role == role)],
        database: widget.databaseName,
      );
      if (!mounted) return;
      setState(() {
        _rolePrivileges = stored.isEmpty ? updated : stored;
        _savingPrivileges = false;
      });
      widget.onModified?.call();
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('${Capability.label(capability)} '
              '${allowed ? 'granted to' : 'withheld from'} ${_roleLabel(role).toLowerCase()}s.'),
          backgroundColor: Colors.green.shade700,
        ),
      );
    } catch (e) {
      if (!mounted) return;
      setState(() => _savingPrivileges = false);
      await _loadRolePrivileges();
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(e.toString().replaceFirst('Exception: ', '')),
          backgroundColor: Colors.red.shade700,
        ),
      );
    }
  }

  /// Opens the Google Authenticator (TOTP RFC 6238) setup / management modal for [user].
  Future<void> _showMfaDialog(UserModel user) async {
    MfaSetupModel? setup;
    String? loadError;
    try {
      setup = await widget.apiClient.setupUserMfa(user.id);
    } catch (e) {
      loadError = e.toString().replaceFirst('Exception: ', '');
    }
    if (!mounted) return;

    final codeCtrl = TextEditingController();
    var errorText = loadError;
    var busy = false;

    await showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDialogState) {
          final isEnabled = user.mfaEnabled;
          final uri = setup?.otpAuthUri ?? '';
          final secret = setup?.secret ?? '';

          return AlertDialog(
            backgroundColor: const Color(0xFF1A1E24),
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(12),
              side: BorderSide(color: Colors.grey.shade800),
            ),
            title: Row(
              children: [
                Icon(
                  isEnabled ? Icons.verified_user : Icons.phonelink_lock_outlined,
                  color: isEnabled ? Colors.tealAccent : Colors.blueAccent,
                  size: 22,
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    'Google Authenticator (MFA) — ${user.username}',
                    style: const TextStyle(fontSize: 16, fontWeight: FontWeight.bold),
                  ),
                ),
              ],
            ),
            content: SizedBox(
              width: 440,
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Container(
                      padding: const EdgeInsets.all(10),
                      decoration: BoxDecoration(
                        color: (isEnabled ? Colors.teal : Colors.blueGrey).withValues(alpha: 0.15),
                        borderRadius: BorderRadius.circular(8),
                        border: Border.all(
                          color: isEnabled ? Colors.tealAccent.withValues(alpha: 0.5) : Colors.blueGrey.shade700,
                        ),
                      ),
                      child: Row(
                        children: [
                          Icon(
                            isEnabled ? Icons.check_circle : Icons.info_outline,
                            size: 18,
                            color: isEnabled ? Colors.tealAccent : Colors.amberAccent,
                          ),
                          const SizedBox(width: 8),
                          Expanded(
                            child: Text(
                              isEnabled
                                  ? 'Multi-Factor Authentication (TOTP) is currently ACTIVE for this account.'
                                  : 'Scan the QR code below with Google Authenticator (or Authy / 1Password) and enter the 6-digit code to activate MFA.',
                              style: const TextStyle(fontSize: 12),
                            ),
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(height: 16),
                    if (uri.isNotEmpty) ...[
                      Center(
                        child: Container(
                          padding: const EdgeInsets.all(14),
                          decoration: BoxDecoration(
                            color: Colors.white,
                            borderRadius: BorderRadius.circular(10),
                          ),
                          child: CustomPaint(
                            size: const Size(188, 188),
                            painter: _QrCodePainter(uri),
                          ),
                        ),
                      ),
                      const SizedBox(height: 12),
                      const Text(
                        'Manual Setup Key (Base32):',
                        style: TextStyle(fontSize: 11, color: Colors.grey, fontWeight: FontWeight.bold),
                      ),
                      const SizedBox(height: 4),
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                        decoration: BoxDecoration(
                          color: Colors.black38,
                          borderRadius: BorderRadius.circular(6),
                          border: Border.all(color: Colors.grey.shade800),
                        ),
                        child: Row(
                          children: [
                            Expanded(
                              child: SelectableText(
                                secret,
                                style: const TextStyle(
                                  fontFamily: 'monospace',
                                  fontSize: 13,
                                  fontWeight: FontWeight.bold,
                                  letterSpacing: 1.4,
                                  color: Colors.tealAccent,
                                ),
                              ),
                            ),
                            IconButton(
                              icon: const Icon(Icons.copy, size: 16),
                              tooltip: 'Copy secret key',
                              visualDensity: VisualDensity.compact,
                              onPressed: () {
                                unawaited(Clipboard.setData(ClipboardData(text: secret)));
                                ScaffoldMessenger.of(context).showSnackBar(
                                  const SnackBar(content: Text('MFA Base32 secret copied to clipboard')),
                                );
                              },
                            ),
                          ],
                        ),
                      ),
                      const SizedBox(height: 14),
                      TextField(
                        controller: codeCtrl,
                        autofocus: true,
                        keyboardType: TextInputType.number,
                        maxLength: 6,
                        style: const TextStyle(
                          fontSize: 18,
                          fontWeight: FontWeight.bold,
                          letterSpacing: 6,
                        ),
                        decoration: InputDecoration(
                          labelText: '6-Digit Verification Code',
                          hintText: '123456',
                          counterText: '',
                          prefixIcon: const Icon(Icons.pin_outlined),
                          border: OutlineInputBorder(borderRadius: BorderRadius.circular(8)),
                        ),
                      ),
                    ],
                    if (errorText != null) ...[
                      const SizedBox(height: 10),
                      Text(
                        errorText!,
                        style: const TextStyle(color: Colors.redAccent, fontSize: 12),
                      ),
                    ],
                  ],
                ),
              ),
            ),
            actions: [
              TextButton(
                onPressed: busy ? null : () => Navigator.of(ctx).pop(),
                child: const Text('Cancel'),
              ),
              if (isEnabled)
                OutlinedButton.icon(
                  style: OutlinedButton.styleFrom(
                    foregroundColor: Colors.redAccent,
                    side: const BorderSide(color: Colors.redAccent),
                  ),
                  onPressed: busy
                      ? null
                      : () async {
                          setDialogState(() {
                            busy = true;
                            errorText = null;
                          });
                          try {
                            await widget.apiClient.disableUserMfa(user.id);
                            if (!ctx.mounted) return;
                            Navigator.of(ctx).pop();
                            await _loadData();
                            if (!mounted) return;
                            ScaffoldMessenger.of(context).showSnackBar(
                              SnackBar(
                                content: Text('MFA disabled for ${user.username}.'),
                                backgroundColor: Colors.orange.shade800,
                              ),
                            );
                          } catch (e) {
                            setDialogState(() {
                              busy = false;
                              errorText = e.toString().replaceFirst('Exception: ', '');
                            });
                          }
                        },
                  icon: const Icon(Icons.lock_open, size: 16),
                  label: const Text('Disable MFA'),
                ),
              FilledButton.icon(
                style: FilledButton.styleFrom(
                  backgroundColor: Colors.teal.shade600,
                  foregroundColor: Colors.white,
                ),
                onPressed: busy || setup == null
                    ? null
                    : () async {
                        final code = codeCtrl.text.trim();
                        if (code.length != 6) {
                          setDialogState(() {
                            errorText = 'Please enter the 6-digit code from Google Authenticator.';
                          });
                          return;
                        }
                        setDialogState(() {
                          busy = true;
                          errorText = null;
                        });
                        try {
                          await widget.apiClient.enableUserMfa(
                            user.id,
                            code: code,
                            secret: secret,
                          );
                          if (!ctx.mounted) return;
                          Navigator.of(ctx).pop();
                          await _loadData();
                          if (!mounted) return;
                          ScaffoldMessenger.of(context).showSnackBar(
                            SnackBar(
                              content: Text('Google Authenticator MFA activated for ${user.username}!'),
                              backgroundColor: Colors.teal.shade700,
                            ),
                          );
                        } catch (e) {
                          setDialogState(() {
                            busy = false;
                            errorText = e.toString().replaceFirst('Exception: ', '');
                          });
                        }
                      },
                icon: const Icon(Icons.verified_user, size: 16),
                label: Text(isEnabled ? 'Verify & Update MFA' : 'Verify & Activate MFA'),
              ),
            ],
          );
        },
      ),
    );
  }
}

class _QrCodePainter extends CustomPainter {

  _QrCodePainter(this.data);
  final String data;

  @override
  void paint(Canvas canvas, Size size) {
    final qrCode = QrCode.fromData(
      data: data,
      errorCorrectLevel: QrErrorCorrectLevel.M,
    );
    final qrImage = QrImage(qrCode);
    final moduleCount = qrImage.moduleCount;
    if (moduleCount <= 0) return;

    final cellSize = size.width / moduleCount;
    final darkPaint = Paint()
      ..color = const Color(0xFF111827)
      ..style = PaintingStyle.fill;

    for (var r = 0; r < moduleCount; r++) {
      for (var c = 0; c < moduleCount; c++) {
        if (qrImage.isDark(r, c)) {
          canvas.drawRect(
            Rect.fromLTWH(c * cellSize, r * cellSize, cellSize + 0.4, cellSize + 0.4),
            darkPaint,
          );
        }
      }
    }
  }

  @override
  bool shouldRepaint(covariant _QrCodePainter oldDelegate) => oldDelegate.data != data;
}

