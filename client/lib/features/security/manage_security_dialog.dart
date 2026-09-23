import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';

class ManageSecurityDialog extends StatefulWidget {
  final ApiClient apiClient;
  final UserModel currentUser;

  const ManageSecurityDialog({
    super.key,
    required this.apiClient,
    required this.currentUser,
  });

  static Future<void> show(BuildContext context, ApiClient apiClient, UserModel currentUser) {
    return showDialog(
      context: context,
      builder: (ctx) => ManageSecurityDialog(apiClient: apiClient, currentUser: currentUser),
    );
  }

  @override
  State<ManageSecurityDialog> createState() => _ManageSecurityDialogState();
}

class _ManageSecurityDialogState extends State<ManageSecurityDialog> {
  List<UserModel> _users = [];
  List<LayoutModel> _layouts = [];
  UserModel? _selectedUser;
  bool _isLoading = true;
  bool _isSaving = false;
  String? _errorMessage;

  // Selected user form controllers
  final _usernameController = TextEditingController();
  final _passwordController = TextEditingController();
  String _selectedRole = 'user';
  Map<String, String> _layoutPermissions = {}; // layoutId -> 'read_write' | 'read_only' | 'none'
  bool _isNewUser = false;

  @override
  void initState() {
    super.initState();
    _loadData();
  }

  @override
  void dispose() {
    _usernameController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  Future<void> _loadData() async {
    setState(() {
      _isLoading = true;
      _errorMessage = null;
    });

    try {
      final users = await widget.apiClient.listUsers();
      final layouts = await widget.apiClient.listLayouts();

      if (mounted) {
        setState(() {
          _users = users;
          _layouts = layouts;
          _isLoading = false;
          if (users.isNotEmpty) {
            _selectUser(users.first);
          } else {
            _startNewUser();
          }
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

  Future<void> _selectUser(UserModel user) async {
    setState(() {
      _selectedUser = user;
      _isNewUser = false;
      _usernameController.text = user.username;
      _passwordController.clear();
      _selectedRole = user.role;
      _layoutPermissions = {};
      for (final l in _layouts) {
        _layoutPermissions[l.id] = user.role == 'owner' || user.role == 'admin' ? 'read_write' : 'read_write';
      }
    });

    try {
      final perms = await widget.apiClient.getUserPermissions(user.id);
      if (mounted) {
        setState(() {
          for (final p in perms) {
            _layoutPermissions[p.layoutId] = p.accessLevel;
          }
        });
      }
    } catch (_) {}
  }

  void _startNewUser() {
    setState(() {
      _selectedUser = null;
      _isNewUser = true;
      _usernameController.clear();
      _passwordController.clear();
      _selectedRole = 'user';
      _layoutPermissions = {for (final l in _layouts) l.id: 'read_write'};
    });
  }

  Future<void> _saveUser() async {
    final username = _usernameController.text.trim();
    final password = _passwordController.text.trim();

    if (username.isEmpty) {
      setState(() => _errorMessage = 'Username cannot be empty');
      return;
    }

    if (_isNewUser && password.isEmpty) {
      setState(() => _errorMessage = 'Password is required for new users');
      return;
    }

    setState(() {
      _isSaving = true;
      _errorMessage = null;
    });

    try {
      String targetUserId;

      if (_isNewUser) {
        final created = await widget.apiClient.createUser(
          username: username,
          password: password,
          role: _selectedRole,
        );
        targetUserId = created.id;
      } else {
        targetUserId = _selectedUser!.id;
        await widget.apiClient.updateUser(
          targetUserId,
          password: password.isNotEmpty ? password : null,
          role: _selectedRole,
        );
      }

      // Save permissions
      final permsList = _layoutPermissions.entries.map((e) {
        return {
          'layout_id': e.key,
          'access_level': e.value,
        };
      }).toList();

      await widget.apiClient.setUserPermissions(targetUserId, permsList);

      await _loadData();
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('User "$username" saved successfully.'),
            backgroundColor: Colors.green.shade700,
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isSaving = false;
          _errorMessage = e.toString().replaceFirst('Exception: ', '');
        });
      }
    } finally {
      if (mounted) setState(() => _isSaving = false);
    }
  }

  Future<void> _deleteUser() async {
    if (_selectedUser == null || _isNewUser) return;

    final confirm = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete User Account'),
        content: Text('Are you sure you want to permanently delete user "${_selectedUser!.username}"?'),
        actions: [
          TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancel')),
          ElevatedButton(
            style: ElevatedButton.styleFrom(backgroundColor: Colors.red, foregroundColor: Colors.white),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );

    if (confirm != true) return;

    setState(() => _isSaving = true);
    try {
      await widget.apiClient.deleteUser(_selectedUser!.id);
      await _loadData();
    } catch (e) {
      if (mounted) {
        setState(() {
          _isSaving = false;
          _errorMessage = e.toString().replaceFirst('Exception: ', '');
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Dialog(
      backgroundColor: Colors.transparent,
      child: Container(
        width: 820,
        height: 600,
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.surface,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: Colors.grey.shade700),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: 0.4),
              blurRadius: 24,
              offset: const Offset(0, 8),
            ),
          ],
        ),
        child: Column(
          children: [
            // Title Bar
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
              decoration: BoxDecoration(
                color: const Color(0xFF1E88E5).withValues(alpha: 0.1),
                borderRadius: const BorderRadius.vertical(top: Radius.circular(12)),
                border: Border(bottom: BorderSide(color: Colors.grey.shade800)),
              ),
              child: Row(
                children: [
                  const Icon(Icons.security, color: Color(0xFF1E88E5), size: 22),
                  const SizedBox(width: 10),
                  const Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('Manage Security & Privileges', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
                      Text('User accounts, roles, and per-layout access levels', style: TextStyle(fontSize: 11, color: Colors.grey)),
                    ],
                  ),
                  const Spacer(),
                  IconButton(
                    onPressed: () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close, size: 20),
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

            // Main Content: Master-Detail
            Expanded(
              child: _isLoading
                  ? const Center(child: CircularProgressIndicator())
                  : Row(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        // Left Column: User Accounts List
                        Container(
                          width: 260,
                          decoration: BoxDecoration(
                            border: Border(right: BorderSide(color: Colors.grey.shade800)),
                            color: Colors.black12,
                          ),
                          child: Column(
                            children: [
                              Padding(
                                padding: const EdgeInsets.all(12),
                                child: Row(
                                  children: [
                                    const Text('USER ACCOUNTS', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey, letterSpacing: 0.5)),
                                    const Spacer(),
                                    IconButton.outlined(
                                      iconSize: 18,
                                      padding: const EdgeInsets.all(4),
                                      tooltip: 'New User Account',
                                      onPressed: _startNewUser,
                                      icon: const Icon(Icons.person_add_alt),
                                    ),
                                  ],
                                ),
                              ),
                              const Divider(height: 1),
                              Expanded(
                                child: ListView.builder(
                                  itemCount: _users.length,
                                  itemBuilder: (context, idx) {
                                    final u = _users[idx];
                                    final isSelected = !_isNewUser && _selectedUser?.id == u.id;
                                    return ListTile(
                                      selected: isSelected,
                                      selectedTileColor: const Color(0xFF1E88E5).withValues(alpha: 0.15),
                                      leading: CircleAvatar(
                                        radius: 14,
                                        backgroundColor: u.role == 'owner'
                                            ? Colors.amber.shade700
                                            : (u.role == 'admin' ? Colors.blue.shade700 : Colors.grey.shade700),
                                        child: Text(
                                          u.username.isNotEmpty ? u.username[0].toUpperCase() : 'U',
                                          style: const TextStyle(fontSize: 12, fontWeight: FontWeight.bold, color: Colors.white),
                                        ),
                                      ),
                                      title: Text(u.username, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.bold)),
                                      subtitle: Text(u.role.toUpperCase(), style: TextStyle(fontSize: 10, color: u.role == 'owner' ? Colors.amber : Colors.grey)),
                                      trailing: u.role == 'owner' ? const Icon(Icons.star, size: 14, color: Colors.amber) : null,
                                      onTap: () => _selectUser(u),
                                    );
                                  },
                                ),
                              ),
                            ],
                          ),
                        ),

                        // Right Column: User Editor & Layout Permissions
                        Expanded(
                          child: SingleChildScrollView(
                            padding: const EdgeInsets.all(20),
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Row(
                                  children: [
                                    Text(
                                      _isNewUser ? 'Create New User Account' : 'Account Details: ${_selectedUser?.username}',
                                      style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 15),
                                    ),
                                    const Spacer(),
                                    if (!_isNewUser && _selectedUser?.role != 'owner')
                                      TextButton.icon(
                                        onPressed: _isSaving ? null : _deleteUser,
                                        style: TextButton.styleFrom(foregroundColor: Colors.red),
                                        icon: const Icon(Icons.delete_outline, size: 18),
                                        label: const Text('Delete User'),
                                      ),
                                  ],
                                ),
                                const SizedBox(height: 16),

                                Row(
                                  children: [
                                    Expanded(
                                      child: TextField(
                                        controller: _usernameController,
                                        enabled: _isNewUser || _selectedUser?.role != 'owner',
                                        decoration: const InputDecoration(
                                          labelText: 'Username',
                                          border: OutlineInputBorder(),
                                          isDense: true,
                                          prefixIcon: Icon(Icons.person, size: 18),
                                        ),
                                      ),
                                    ),
                                    const SizedBox(width: 12),
                                    Expanded(
                                      child: DropdownButtonFormField<String>(
                                        value: _selectedRole,
                                        decoration: const InputDecoration(
                                          labelText: 'Access Role',
                                          border: OutlineInputBorder(),
                                          isDense: true,
                                          prefixIcon: Icon(Icons.admin_panel_settings_outlined, size: 18),
                                        ),
                                        items: const [
                                          DropdownMenuItem(value: 'owner', child: Text('Owner (Full Master)')),
                                          DropdownMenuItem(value: 'admin', child: Text('Admin (Designer & Schema)')),
                                          DropdownMenuItem(value: 'user', child: Text('Standard User (Layout Restricted)')),
                                        ],
                                        onChanged: _selectedUser?.role == 'owner' && _users.where((u) => u.role == 'owner').length <= 1
                                            ? null
                                            : (v) => setState(() => _selectedRole = v ?? 'user'),
                                      ),
                                    ),
                                  ],
                                ),
                                const SizedBox(height: 12),
                                TextField(
                                  controller: _passwordController,
                                  decoration: InputDecoration(
                                    labelText: _isNewUser ? 'Password' : 'New Password (leave empty to keep current)',
                                    border: const OutlineInputBorder(),
                                    isDense: true,
                                    prefixIcon: const Icon(Icons.lock_outline, size: 18),
                                  ),
                                  obscureText: true,
                                ),

                                const SizedBox(height: 24),
                                const Text(
                                  'LAYOUT ACCESS PERMISSIONS',
                                  style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey, letterSpacing: 0.5),
                                ),
                                const SizedBox(height: 4),
                                const Text(
                                  'Specify whether this user can read & write, view only, or cannot access each layout.',
                                  style: TextStyle(fontSize: 11, color: Colors.grey),
                                ),
                                const SizedBox(height: 10),

                                if (_layouts.isEmpty)
                                  Container(
                                    padding: const EdgeInsets.all(16),
                                    decoration: BoxDecoration(
                                      border: Border.all(color: Colors.grey.shade800),
                                      borderRadius: BorderRadius.circular(6),
                                    ),
                                    child: const Center(
                                      child: Text('No layouts created in active database yet.', style: TextStyle(fontSize: 12, color: Colors.grey)),
                                    ),
                                  )
                                else
                                  Container(
                                    decoration: BoxDecoration(
                                      border: Border.all(color: Colors.grey.shade800),
                                      borderRadius: BorderRadius.circular(6),
                                    ),
                                    child: Column(
                                      children: _layouts.map((l) {
                                        final currentLevel = _layoutPermissions[l.id] ?? 'read_write';
                                        return Container(
                                          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                                          decoration: BoxDecoration(
                                            border: Border(bottom: BorderSide(color: Colors.grey.shade800)),
                                          ),
                                          child: Row(
                                            children: [
                                              const Icon(Icons.dashboard_customize_outlined, size: 18, color: Color(0xFF1E88E5)),
                                              const SizedBox(width: 10),
                                              Expanded(
                                                child: Text(
                                                  l.name,
                                                  style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
                                                ),
                                              ),
                                              SegmentedButton<String>(
                                                segments: const [
                                                  ButtonSegment(
                                                    value: 'read_write',
                                                    label: Text('Read & Write', style: TextStyle(fontSize: 11)),
                                                    icon: Icon(Icons.edit, size: 14),
                                                  ),
                                                  ButtonSegment(
                                                    value: 'read_only',
                                                    label: Text('Read Only', style: TextStyle(fontSize: 11)),
                                                    icon: Icon(Icons.visibility, size: 14),
                                                  ),
                                                  ButtonSegment(
                                                    value: 'none',
                                                    label: Text('No Access', style: TextStyle(fontSize: 11)),
                                                    icon: Icon(Icons.block, size: 14),
                                                  ),
                                                ],
                                                selected: {currentLevel},
                                                onSelectionChanged: (val) {
                                                  setState(() {
                                                    _layoutPermissions[l.id] = val.first;
                                                  });
                                                },
                                              ),
                                            ],
                                          ),
                                        );
                                      }).toList(),
                                    ),
                                  ),

                                const SizedBox(height: 24),
                                Align(
                                  alignment: Alignment.centerRight,
                                  child: ElevatedButton.icon(
                                    onPressed: _isSaving ? null : _saveUser,
                                    style: ElevatedButton.styleFrom(
                                      backgroundColor: const Color(0xFF1E88E5),
                                      foregroundColor: Colors.white,
                                      padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 12),
                                    ),
                                    icon: _isSaving
                                        ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                                        : const Icon(Icons.check, size: 18),
                                    label: Text(_isSaving ? 'Saving...' : 'Save User & Privileges'),
                                  ),
                                ),
                              ],
                            ),
                          ),
                        ),
                      ],
                    ),
            ),
          ],
        ),
      ),
    );
  }
}
