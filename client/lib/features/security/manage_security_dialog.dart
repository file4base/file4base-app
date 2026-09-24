import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';

class ManageSecurityDialog extends StatefulWidget {
  final ApiClient apiClient;
  final UserModel currentUser;
  final String? databaseName;

  const ManageSecurityDialog({
    super.key,
    required this.apiClient,
    required this.currentUser,
    this.databaseName,
  });

  static Future<void> show(
    BuildContext context,
    ApiClient apiClient,
    UserModel currentUser, {
    String? databaseName,
  }) {
    return showDialog(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => ManageSecurityDialog(
        apiClient: apiClient,
        currentUser: currentUser,
        databaseName: databaseName,
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
  Map<String, List<UserLayoutPermissionModel>> _userPermissionsMap = {};

  bool _isLoading = true;
  String? _errorMessage;
  String _searchQuery = '';
  String _statusFilter = 'all'; // 'all', 'active', 'inactive'

  // Extended privileges mockup state
  final Map<String, bool> _extPrivWebDirect = {'owner': true, 'admin': true, 'user': true};
  final Map<String, bool> _extPrivRest = {'owner': true, 'admin': true, 'user': false};
  final Map<String, bool> _extPrivDesktop = {'owner': true, 'admin': true, 'user': true};
  final Map<String, bool> _extPrivExport = {'owner': true, 'admin': true, 'user': false};

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 3, vsync: this);
    _loadData();
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
      final users = await widget.apiClient.listUsers();
      final layouts = await widget.apiClient.listLayouts();

      // Preload permissions for all users
      final permsMap = <String, List<UserLayoutPermissionModel>>{};
      for (final u in users) {
        try {
          final p = await widget.apiClient.getUserPermissions(u.id);
          permsMap[u.id] = p;
        } catch (_) {
          permsMap[u.id] = [];
        }
      }

      if (mounted) {
        setState(() {
          _users = users;
          _layouts = layouts;
          _userPermissionsMap = permsMap;
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

  bool _isLastActiveOwner(UserModel user) {
    if (user.role != 'owner' || !user.isActive) return false;
    final activeOwners = _users.where((u) => u.role == 'owner' && u.isActive).length;
    return activeOwners <= 1;
  }

  Future<void> _toggleUserActive(UserModel user, bool newActive) async {
    if (_isCurrentUser(user)) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('No puedes desactivar tu propia cuenta activa.'),
          backgroundColor: Colors.orange,
        ),
      );
      return;
    }

    if (!newActive && _isLastActiveOwner(user)) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Debe existir al menos una cuenta con rol Owner (Acceso total) activa en la base de datos.'),
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
      );

      setState(() {
        final index = _users.indexWhere((u) => u.id == user.id);
        if (index != -1) {
          _users[index] = _users[index].copyWith(isActive: newActive);
        }
      });

      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Cuenta "${user.username}" ${newActive ? 'activada' : 'desactivada'} correctamente.'),
            backgroundColor: newActive ? Colors.green.shade700 : Colors.blueGrey,
            duration: const Duration(seconds: 2),
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Error al cambiar estado de cuenta: $e'),
            backgroundColor: Colors.red,
          ),
        );
      }
    }
  }

  Future<void> _showChangePasswordDialog(UserModel user) async {
    final passwordController = TextEditingController();
    final confirmController = TextEditingController();
    bool obscure = true;
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
                child: Text('Cambiar contraseña: ${user.username}', style: const TextStyle(fontSize: 16)),
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
                  'Introduce la nueva contraseña para la cuenta "${user.username}".',
                  style: const TextStyle(fontSize: 12, color: Colors.grey),
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: passwordController,
                  obscureText: obscure,
                  decoration: InputDecoration(
                    labelText: 'Nueva contraseña',
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
                  decoration: const InputDecoration(
                    labelText: 'Confirmar contraseña',
                    border: OutlineInputBorder(),
                    isDense: true,
                    prefixIcon: const Icon(Icons.lock_reset, size: 18),
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
              child: const Text('Cancelar'),
            ),
            ElevatedButton.icon(
              style: ElevatedButton.styleFrom(
                backgroundColor: const Color(0xFF1E88E5),
                foregroundColor: Colors.white,
              ),
              icon: const Icon(Icons.check, size: 18),
              label: const Text('Guardar contraseña'),
              onPressed: () async {
                final p1 = passwordController.text;
                final p2 = confirmController.text;
                if (p1.isEmpty) {
                  setDlgState(() => dialogError = 'La contraseña no puede estar vacía.');
                  return;
                }
                if (p1 != p2) {
                  setDlgState(() => dialogError = 'Las contraseñas no coinciden.');
                  return;
                }

                try {
                  await widget.apiClient.updateUser(
                    user.id,
                    role: user.role,
                    password: p1,
                    isActive: user.isActive,
                  );
                  if (ctx.mounted) Navigator.of(ctx).pop();
                  if (mounted) {
                    ScaffoldMessenger.of(context).showSnackBar(
                      SnackBar(
                        content: Text('Contraseña de "${user.username}" actualizada correctamente.'),
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
    String selectedRole = userToEdit?.role ?? 'user';
    bool isActive = userToEdit?.isActive ?? true;
    Map<String, String> layoutPerms = {};

    // Load initial layout permissions
    for (final l in _layouts) {
      layoutPerms[l.id] = (selectedRole == 'owner' || selectedRole == 'admin') ? 'read_write' : 'read_write';
    }
    if (!isNew && _userPermissionsMap.containsKey(userToEdit.id)) {
      for (final p in _userPermissionsMap[userToEdit.id]!) {
        layoutPerms[p.layoutId] = p.accessLevel;
      }
    }

    String? dialogError;
    bool isSaving = false;

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
                        isNew ? 'Nueva cuenta de usuario' : 'Editar cuenta: ${userToEdit.username}',
                        style: const TextStyle(fontSize: 16, fontWeight: FontWeight.bold),
                      ),
                      Text(
                        'Base de datos activa: ${widget.databaseName ?? 'file4base_dev'}',
                        style: const TextStyle(fontSize: 11, color: Colors.grey),
                      ),
                    ],
                  ),
                ),
              ],
            ),
            content: SizedBox(
              width: 580,
              height: 480,
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
                                    labelText: 'Nombre de cuenta / Usuario *',
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
                                  value: selectedRole,
                                  decoration: const InputDecoration(
                                    labelText: 'Conjunto de privilegios *',
                                    border: OutlineInputBorder(),
                                    isDense: true,
                                    prefixIcon: Icon(Icons.admin_panel_settings_outlined, size: 18),
                                  ),
                                  items: const [
                                    DropdownMenuItem(
                                      value: 'owner',
                                      child: Text('[Acceso total] Owner'),
                                    ),
                                    DropdownMenuItem(
                                      value: 'admin',
                                      child: Text('[Entrada datos y diseño] Admin'),
                                    ),
                                    DropdownMenuItem(
                                      value: 'user',
                                      child: Text('[Acceso restringido] User'),
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
                                  decoration: InputDecoration(
                                    labelText: isNew ? 'Contraseña inicial *' : 'Nueva contraseña (opcional)',
                                    hintText: isNew ? 'Obligatorio' : 'Dejar en blanco para no cambiar',
                                    border: const OutlineInputBorder(),
                                    isDense: true,
                                    prefixIcon: const Icon(Icons.lock_outline, size: 18),
                                  ),
                                  obscureText: true,
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
                                          isActive ? 'Cuenta Activa' : 'Desactivada',
                                          style: TextStyle(
                                            fontSize: 12,
                                            fontWeight: FontWeight.bold,
                                            color: isActive ? Colors.green : Colors.grey,
                                          ),
                                        ),
                                      ),
                                      Switch(
                                        value: isActive,
                                        activeColor: Colors.green,
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

                          const SizedBox(height: 20),
                          const Divider(),
                          const SizedBox(height: 8),

                          // Layout Permissions header
                          Row(
                            children: [
                              const Icon(Icons.dashboard_customize_outlined, size: 18, color: Color(0xFF1E88E5)),
                              const SizedBox(width: 8),
                              const Text(
                                'Privilegios de acceso por Presentación (Layouts)',
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
                                child: const Text('Todo L/E', style: TextStyle(fontSize: 11)),
                              ),
                              TextButton(
                                onPressed: () {
                                  setDlgState(() {
                                    for (final l in _layouts) {
                                      layoutPerms[l.id] = 'read_only';
                                    }
                                  });
                                },
                                child: const Text('Solo Lectura', style: TextStyle(fontSize: 11)),
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
                                child: Text('No hay presentaciones definidas en esta solución todavía.', style: TextStyle(color: Colors.grey, fontSize: 12)),
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
                                separatorBuilder: (_, __) => Divider(height: 1, color: Colors.grey.shade800),
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
                                              label: Text('L/E', style: TextStyle(fontSize: 10)),
                                              tooltip: 'Lectura y Escritura',
                                            ),
                                            ButtonSegment(
                                              value: 'read_only',
                                              label: Text('Lectura', style: TextStyle(fontSize: 10)),
                                              tooltip: 'Solo Lectura',
                                            ),
                                            ButtonSegment(
                                              value: 'none',
                                              label: Text('Sin acceso', style: TextStyle(fontSize: 10)),
                                              tooltip: 'Sin acceso',
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
                child: const Text('Cancelar'),
              ),
              ElevatedButton.icon(
                style: ElevatedButton.styleFrom(
                  backgroundColor: const Color(0xFF1E88E5),
                  foregroundColor: Colors.white,
                ),
                icon: const Icon(Icons.check, size: 18),
                label: Text(isNew ? 'Crear cuenta' : 'Guardar cambios'),
                onPressed: isSaving
                    ? null
                    : () async {
                        final uname = usernameController.text.trim();
                        final pass = passwordController.text.trim();

                        if (uname.isEmpty) {
                          setDlgState(() => dialogError = 'El nombre de usuario no puede estar vacío.');
                          return;
                        }
                        if (isNew && pass.isEmpty) {
                          setDlgState(() => dialogError = 'La contraseña es requerida para cuentas nuevas.');
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
                            );
                            targetId = created.id;
                          } else {
                            targetId = userToEdit.id;
                            await widget.apiClient.updateUser(
                              targetId,
                              role: selectedRole,
                              password: pass.isNotEmpty ? pass : null,
                              isActive: isActive,
                            );
                          }

                          // Save layout permissions
                          final permsList = layoutPerms.entries.map((e) {
                            return {
                              'layout_id': e.key,
                              'access_level': e.value,
                            };
                          }).toList();
                          await widget.apiClient.setUserPermissions(targetId, permsList);

                          if (ctx.mounted) Navigator.of(ctx).pop();
                          await _loadData();

                          if (mounted) {
                            ScaffoldMessenger.of(context).showSnackBar(
                              SnackBar(
                                content: Text('Cuenta "$uname" guardada correctamente.'),
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
    final cloneName = '${user.username}_copia';
    final passwordController = TextEditingController();
    String? dialogError;

    await showDialog(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDlgState) => AlertDialog(
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
          title: Row(
            children: [
              const Icon(Icons.content_copy_outlined, color: Color(0xFF1E88E5), size: 22),
              const SizedBox(width: 8),
              Text('Duplicar cuenta: ${user.username}', style: const TextStyle(fontSize: 16)),
            ],
          ),
          content: SizedBox(
            width: 400,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Se creará un duplicado con rol "${user.role.toUpperCase()}" y los mismos privilegios de presentaciones.',
                  style: const TextStyle(fontSize: 12, color: Colors.grey),
                ),
                const SizedBox(height: 16),
                Text('Nuevo nombre de cuenta: $cloneName', style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                const SizedBox(height: 12),
                TextField(
                  controller: passwordController,
                  obscureText: true,
                  decoration: const InputDecoration(
                    labelText: 'Contraseña para la nueva cuenta *',
                    border: OutlineInputBorder(),
                    isDense: true,
                    prefixIcon: Icon(Icons.lock_outline, size: 18),
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
              child: const Text('Cancelar'),
            ),
            ElevatedButton.icon(
              style: ElevatedButton.styleFrom(
                backgroundColor: const Color(0xFF1E88E5),
                foregroundColor: Colors.white,
              ),
              icon: const Icon(Icons.copy, size: 18),
              label: const Text('Duplicar'),
              onPressed: () async {
                final pass = passwordController.text.trim();
                if (pass.isEmpty) {
                  setDlgState(() => dialogError = 'Introduce una contraseña para el usuario duplicado.');
                  return;
                }

                try {
                  final newUser = await widget.apiClient.createUser(
                    username: cloneName,
                    password: pass,
                    role: user.role,
                    isActive: true,
                  );

                  // Copy permissions from original
                  final originalPerms = _userPermissionsMap[user.id] ?? [];
                  if (originalPerms.isNotEmpty) {
                    final payload = originalPerms.map((p) => {
                      'layout_id': p.layoutId,
                      'access_level': p.accessLevel,
                    }).toList();
                    await widget.apiClient.setUserPermissions(newUser.id, payload);
                  }

                  if (ctx.mounted) Navigator.of(ctx).pop();
                  await _loadData();

                  if (mounted) {
                    ScaffoldMessenger.of(context).showSnackBar(
                      SnackBar(
                        content: Text('Cuenta duplicada con éxito: $cloneName'),
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
          content: Text('No puedes eliminar tu propia cuenta en sesión.'),
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
            content: Text('No se puede eliminar el único usuario Owner (Acceso total) de la base de datos.'),
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
        title: Row(
          children: const [
            Icon(Icons.warning_amber_rounded, color: Colors.red, size: 24),
            SizedBox(width: 8),
            Text('Eliminar cuenta de usuario'),
          ],
        ),
        content: Text(
          '¿Estás completamente seguro de que deseas eliminar permanentemente la cuenta "${user.username}"?\nEsta acción no se puede deshacer.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancelar')),
          ElevatedButton(
            style: ElevatedButton.styleFrom(backgroundColor: Colors.red, foregroundColor: Colors.white),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Eliminar definitivamente'),
          ),
        ],
      ),
    );

    if (confirm != true) return;

    try {
      await widget.apiClient.deleteUser(user.id);
      await _loadData();
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Cuenta "${user.username}" eliminada correctamente.'),
            backgroundColor: Colors.blueGrey,
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Error al eliminar usuario: $e'),
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
        label = '[Acceso total] Owner';
        icon = Icons.verified_user_outlined;
        break;
      case 'admin':
        bg = Colors.blue.shade900.withValues(alpha: 0.25);
        fg = Colors.blue.shade300;
        label = '[Entrada datos y diseño] Admin';
        icon = Icons.design_services_outlined;
        break;
      default:
        bg = Colors.teal.shade900.withValues(alpha: 0.25);
        fg = Colors.teal.shade300;
        label = '[Acceso restringido] User';
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
    final activeDb = widget.databaseName ?? 'file4base_dev';

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
                            'Gestionar Seguridad',
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
                        'Cuentas, conjuntos de privilegios y permisos de acceso por base de datos',
                        style: TextStyle(fontSize: 11, color: Colors.grey),
                      ),
                    ],
                  ),
                  const Spacer(),
                  IconButton(
                    onPressed: () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close, size: 20),
                    tooltip: 'Cerrar',
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
                        const Text('Cuentas'),
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
                        Text('Privilegios de presentaciones'),
                      ],
                    ),
                  ),
                  const Tab(
                    child: Row(
                      children: [
                        Icon(Icons.public, size: 16),
                        SizedBox(width: 6),
                        Text('Privilegios ampliados'),
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
                    'Total: ${_users.length} cuentas  •  $_activeCount activas  •  $_inactiveCount inactivas',
                    style: const TextStyle(fontSize: 12, color: Colors.grey),
                  ),
                  const Spacer(),
                  OutlinedButton(
                    onPressed: () => Navigator.of(context).pop(),
                    style: OutlinedButton.styleFrom(
                      padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 12),
                    ),
                    child: const Text('Cerrar'),
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
                    hintText: 'Buscar cuentas por nombre o rol...',
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
                    label: Text('Todas', style: TextStyle(fontSize: 11)),
                  ),
                  ButtonSegment(
                    value: 'active',
                    label: Text('Activas', style: TextStyle(fontSize: 11)),
                  ),
                  ButtonSegment(
                    value: 'inactive',
                    label: Text('Inactivas', style: TextStyle(fontSize: 11)),
                  ),
                ],
                selected: {_statusFilter},
                onSelectionChanged: (set) => setState(() => _statusFilter = set.first),
              ),
              const SizedBox(width: 12),

              // New Account Button
              ElevatedButton.icon(
                onPressed: () => _showAccountDialog(),
                style: ElevatedButton.styleFrom(
                  backgroundColor: const Color(0xFF1E88E5),
                  foregroundColor: Colors.white,
                  padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                ),
                icon: const Icon(Icons.person_add_alt, size: 18),
                label: const Text('Nueva cuenta...'),
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
                        children: const [
                          SizedBox(width: 40, child: Text('#', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.grey))),
                          SizedBox(width: 140, child: Text('ESTADO', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.grey))),
                          Expanded(flex: 3, child: Text('NOMBRE DE CUENTA', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.grey))),
                          Expanded(flex: 3, child: Text('CONJUNTO DE PRIVILEGIOS', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.grey))),
                          Expanded(flex: 2, child: Text('PRESENTACIONES', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.grey))),
                          SizedBox(width: 180, child: Text('OPCIONES', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.grey))),
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
                                    _searchQuery.isNotEmpty ? 'No hay cuentas que coincidan con "$_searchQuery"' : 'No hay cuentas registradas',
                                    style: const TextStyle(color: Colors.grey, fontSize: 13),
                                  ),
                                ],
                              ),
                            )
                          : ListView.separated(
                              itemCount: filtered.length,
                              separatorBuilder: (_, __) => Divider(height: 1, color: Colors.grey.shade900),
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
                                                fit: BoxFit.contain,
                                                child: Switch(
                                                  value: user.isActive,
                                                  activeColor: Colors.green,
                                                  materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
                                                  onChanged: (isCurrent || (user.isActive && isLastOwner))
                                                      ? null
                                                      : (val) => _toggleUserActive(user, val),
                                                ),
                                              ),
                                            ),
                                            const SizedBox(width: 6),
                                            Text(
                                              user.isActive ? 'Activa' : 'Inactiva',
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
                                                            'Tú / Activa',
                                                            style: TextStyle(fontSize: 9, fontWeight: FontWeight.bold, color: Colors.greenAccent),
                                                          ),
                                                        ),
                                                      ],
                                                    ],
                                                  ),
                                                  if (user.createdAt != null)
                                                    Text(
                                                      'Creado: ${user.createdAt!.toLocal().toString().split('.').first}',
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
                                              ? 'Sin presentaciones'
                                              : (user.role == 'owner'
                                                  ? 'Todas (${_layouts.length})'
                                                  : '$accessibleLayouts de ${_layouts.length} accesibles'),
                                          style: const TextStyle(fontSize: 12, color: Colors.grey),
                                        ),
                                      ),

                                      // 6. Action Icons
                                      SizedBox(
                                        width: 180,
                                        child: Row(
                                          mainAxisAlignment: MainAxisAlignment.end,
                                          children: [
                                            // Change Password
                                            IconButton(
                                              visualDensity: VisualDensity.compact,
                                              padding: const EdgeInsets.all(4),
                                              constraints: const BoxConstraints(),
                                              icon: const Icon(Icons.key_outlined, size: 18),
                                              tooltip: 'Cambiar contraseña',
                                              onPressed: () => _showChangePasswordDialog(user),
                                            ),
                                            const SizedBox(width: 4),

                                            // Edit Account
                                            IconButton(
                                              visualDensity: VisualDensity.compact,
                                              padding: const EdgeInsets.all(4),
                                              constraints: const BoxConstraints(),
                                              icon: const Icon(Icons.edit_outlined, size: 18),
                                              tooltip: 'Editar cuenta y privilegios',
                                              onPressed: () => _showAccountDialog(userToEdit: user),
                                            ),
                                            const SizedBox(width: 4),

                                            // Duplicate Account
                                            IconButton(
                                              visualDensity: VisualDensity.compact,
                                              padding: const EdgeInsets.all(4),
                                              constraints: const BoxConstraints(),
                                              icon: const Icon(Icons.content_copy_outlined, size: 18),
                                              tooltip: 'Duplicar cuenta',
                                              onPressed: () => _duplicateUser(user),
                                            ),
                                            const SizedBox(width: 4),

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
                                              tooltip: isCurrent ? 'No puedes eliminarte a ti mismo' : 'Eliminar cuenta',
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
                'Matriz de Privilegios de Presentaciones',
                style: TextStyle(fontWeight: FontWeight.bold, fontSize: 14),
              ),
              const Spacer(),
              Text(
                '${_layouts.length} presentaciones en base de datos',
                style: const TextStyle(fontSize: 12, color: Colors.grey),
              ),
            ],
          ),
          const SizedBox(height: 6),
          const Text(
            'Visualiza qué nivel de acceso tienen las cuentas registradas sobre cada pantalla o presentación.',
            style: TextStyle(fontSize: 12, color: Colors.grey),
          ),
          const SizedBox(height: 14),

          Expanded(
            child: _layouts.isEmpty
                ? const Center(child: Text('No hay presentaciones registradas en esta solución.', style: TextStyle(color: Colors.grey)))
                : Container(
                    decoration: BoxDecoration(
                      border: Border.all(color: Colors.grey.shade800),
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: ListView.separated(
                      itemCount: _layouts.length,
                      separatorBuilder: (_, __) => Divider(height: 1, color: Colors.grey.shade800),
                      itemBuilder: (ctx, i) {
                        final l = _layouts[i];
                        return ExpansionTile(
                          leading: const Icon(Icons.dashboard_customize_outlined, color: Color(0xFF1E88E5)),
                          title: Text(l.name, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                          subtitle: Text('ID: ${l.id}  •  Ocurrencia: ${l.tableOccurrenceId}', style: const TextStyle(fontSize: 11, color: Colors.grey)),
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
                                  String badgeText = 'Lectura y Escritura';
                                  if (p.accessLevel == 'read_only') {
                                    badgeColor = Colors.blue;
                                    badgeText = 'Solo Lectura';
                                  } else if (p.accessLevel == 'none') {
                                    badgeColor = Colors.red;
                                    badgeText = 'Sin acceso';
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
    return Padding(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: const [
              Icon(Icons.public, size: 20, color: Color(0xFF1E88E5)),
              SizedBox(width: 8),
              Text(
                'Privilegios Ampliados de Red y Protocolo (Extended Privileges)',
                style: TextStyle(fontWeight: FontWeight.bold, fontSize: 14),
              ),
            ],
          ),
          const SizedBox(height: 6),
          const Text(
            'Controla qué métodos de acceso a datos y clientes remotos están permitidos para cada conjunto de privilegios.',
            style: TextStyle(fontSize: 12, color: Colors.grey),
          ),
          const SizedBox(height: 16),

          Expanded(
            child: ListView(
              children: [
                _buildExtPrivCard(
                  code: 'fmapp',
                  title: 'Acceso mediante File4Base Desktop',
                  description: 'Permite abrir y usar la base de datos a través de la aplicación cliente nativa (macOS / Linux / Windows).',
                  icon: Icons.desktop_windows_outlined,
                  states: _extPrivDesktop,
                ),
                _buildExtPrivCard(
                  code: 'fmwebdirect',
                  title: 'Acceso mediante File4Base WebDirect',
                  description: 'Permite abrir y usar la base de datos de manera directa a través de exploradores web modernos vía Nginx.',
                  icon: Icons.language_outlined,
                  states: _extPrivWebDirect,
                ),
                _buildExtPrivCard(
                  code: 'fmrest',
                  title: 'Acceso mediante API REST / Data API',
                  description: 'Permite consultas JSON, ingestión de registros y automatizaciones backend a través de endpoints REST seguros.',
                  icon: Icons.api_outlined,
                  states: _extPrivRest,
                ),
                _buildExtPrivCard(
                  code: 'fmexport',
                  title: 'Exportación masiva de registros',
                  description: 'Permite exportar datos a formatos Excel, CSV, JSON y paquetes de solución empaquetados .f4b.',
                  icon: Icons.file_download_outlined,
                  states: _extPrivExport,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildExtPrivCard({
    required String code,
    required String title,
    required String description,
    required IconData icon,
    required Map<String, bool> states,
  }) {
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8), side: BorderSide(color: Colors.grey.shade800)),
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(icon, size: 28, color: const Color(0xFF1E88E5)),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Text(title, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                      const SizedBox(width: 8),
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
                        decoration: BoxDecoration(color: Colors.grey.shade800, borderRadius: BorderRadius.circular(4)),
                        child: Text(code, style: const TextStyle(fontSize: 10, fontFamily: 'monospace')),
                      ),
                    ],
                  ),
                  const SizedBox(height: 4),
                  Text(description, style: const TextStyle(fontSize: 11, color: Colors.grey)),
                  const SizedBox(height: 10),
                  Row(
                    children: [
                      _buildExtToggle('Owner', states['owner'] ?? true, (val) => setState(() => states['owner'] = val)),
                      const SizedBox(width: 16),
                      _buildExtToggle('Admin', states['admin'] ?? true, (val) => setState(() => states['admin'] = val)),
                      const SizedBox(width: 16),
                      _buildExtToggle('User', states['user'] ?? false, (val) => setState(() => states['user'] = val)),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildExtToggle(String roleName, bool value, ValueChanged<bool> onChanged) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(roleName, style: const TextStyle(fontSize: 11, fontWeight: FontWeight.w600)),
        const SizedBox(width: 4),
        Switch(
          value: value,
          activeColor: const Color(0xFF1E88E5),
          onChanged: onChanged,
        ),
      ],
    );
  }
}
