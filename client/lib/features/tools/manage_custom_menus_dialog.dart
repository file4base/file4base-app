// #52 — Tools > Custom Menus > Manage Custom Menus.
// Allows a solution's owner or administrator to define custom menu sets,
// choose the solution's default menu set, and assign a menu set per layout.

import 'dart:async';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter/material.dart';

class ManageCustomMenusDialog extends StatefulWidget {

  const ManageCustomMenusDialog({
    required this.apiClient, super.key,
    this.scripts = const [],
    this.layouts = const [],
    this.onChanged,
  });
  final ApiClient apiClient;
  final List<ScriptModel> scripts;
  final List<LayoutModel> layouts;
  final VoidCallback? onChanged;

  static Future<void> show(
    BuildContext context, {
    required ApiClient apiClient,
    List<ScriptModel> scripts = const [],
    List<LayoutModel> layouts = const [],
    VoidCallback? onChanged,
  }) {
    return showDialog<void>(
      context: context,
      builder: (_) => ManageCustomMenusDialog(
        apiClient: apiClient,
        scripts: scripts,
        layouts: layouts,
        onChanged: onChanged,
      ),
    );
  }

  @override
  State<ManageCustomMenusDialog> createState() => _ManageCustomMenusDialogState();
}

class _ManageCustomMenusDialogState extends State<ManageCustomMenusDialog> {
  bool _loading = true;
  bool _saving = false;
  String? _error;

  MenuSetsListingModel _listing = const MenuSetsListingModel();
  List<ScriptModel> _scripts = const [];
  List<LayoutModel> _layouts = const [];

  MenuSetModel? _selected;
  final TextEditingController _nameController = TextEditingController();
  bool _isDefault = false;
  List<CustomMenuModel> _editingMenus = [];

  static const List<MapEntry<String, String>> _singleSteps = [
    MapEntry('new_record', 'New Record'),
    MapEntry('duplicate_record', 'Duplicate Record'),
    MapEntry('delete_record', 'Delete Record'),
    MapEntry('commit_records', 'Commit Records'),
    MapEntry('revert_record', 'Revert Record'),
    MapEntry('show_all_records', 'Show All Records'),
    MapEntry('enter_browse_mode', 'Enter Browse Mode'),
    MapEntry('enter_find_mode', 'Enter Find Mode'),
    MapEntry('perform_find', 'Perform Find'),
  ];

  @override
  void initState() {
    super.initState();
    _scripts = List<ScriptModel>.from(widget.scripts);
    _layouts = List<LayoutModel>.from(widget.layouts);
    unawaited(_load());
  }

  @override
  void dispose() {
    _nameController.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final listing = await widget.apiClient.listMenuSets();
      if (_scripts.isEmpty) {
        try {
          _scripts = await widget.apiClient.listScripts();
        } catch (_) {}
      }
      if (_layouts.isEmpty) {
        try {
          _layouts = await widget.apiClient.listLayouts();
        } catch (_) {}
      }
      if (!mounted) return;
      setState(() {
        _loading = false;
        _listing = listing;
        if (_selected != null) {
          final updated = listing.menuSets
              .where((s) => s.id == _selected!.id)
              .firstOrNull;
          _selectSet(updated ?? listing.menuSets.firstOrNull);
        } else if (listing.menuSets.isNotEmpty) {
          _selectSet(listing.menuSets.first);
        } else {
          _startNewSet();
        }
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  void _selectSet(MenuSetModel? set) {
    _selected = set;
    if (set == null) {
      _startNewSet();
      return;
    }
    _nameController.text = set.name;
    _isDefault = set.isDefault;
    _editingMenus = set.menus
        .map(
          (m) => CustomMenuModel(
            menu: m.menu,
            mode: m.mode,
            items: List<CustomMenuItemModel>.from(m.items),
          ),
        )
        .toList();
  }

  void _startNewSet() {
    _selected = null;
    _nameController.text = '';
    _isDefault = _listing.menuSets.isEmpty;
    _editingMenus = [
      CustomMenuModel(
        menu: 'Records',
        items: [
          CustomMenuItemModel(
            title: _scripts.isNotEmpty ? _scripts.first.name : 'New Record',
            action: _scripts.isNotEmpty
                ? {
                    'type': 'perform_script',
                    'script_id': _scripts.first.id,
                    'script_name': _scripts.first.name,
                  }
                : {'type': 'single_step', 'step_type': 'new_record'},
          ),
        ],
      ),
    ];
  }

  bool _isExtendOnly(String menuName) {
    return _listing.extendOnly
        .any((m) => m.toLowerCase() == menuName.toLowerCase());
  }

  Future<void> _saveCurrentSet() async {
    final name = _nameController.text.trim();
    if (name.isEmpty) {
      setState(() => _error = 'Give the menu set a name.');
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      MenuSetModel saved;
      if (_selected == null) {
        saved = await widget.apiClient.createMenuSet(
          name: name,
          isDefault: _isDefault,
          menus: _editingMenus,
        );
      } else {
        saved = await widget.apiClient.updateMenuSet(
          _selected!.id,
          name: name,
          isDefault: _isDefault,
          menus: _editingMenus,
        );
      }
      widget.onChanged?.call();
      if (!mounted) return;
      _selected = saved;
      await _load();
      if (!mounted) return;
      setState(() => _saving = false);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  Future<void> _deleteCurrentSet() async {
    final target = _selected;
    if (target == null) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await widget.apiClient.deleteMenuSet(target.id);
      widget.onChanged?.call();
      if (!mounted) return;
      _selected = null;
      await _load();
      if (!mounted) return;
      setState(() => _saving = false);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  Future<void> _assignLayoutMenuSet(LayoutModel layout, String? menuSetId) async {
    final updatedDef = Map<String, dynamic>.from(layout.definition);
    if (menuSetId == null || menuSetId.isEmpty) {
      updatedDef.remove('menu_set_id');
    } else {
      updatedDef['menu_set_id'] = menuSetId;
    }
    try {
      final updated = await widget.apiClient.updateLayout(
        layout.id,
        layout.name,
        updatedDef,
      );
      if (!mounted) return;
      setState(() {
        final idx = _layouts.indexWhere((l) => l.id == layout.id);
        if (idx >= 0) {
          _layouts[idx] = updated;
        }
      });
      widget.onChanged?.call();
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = e.toString().replaceFirst('Exception: ', ''));
    }
  }

  void _addMenuOverride() {
    final used = _editingMenus.map((m) => m.menu.toLowerCase()).toSet();
    final available = _listing.menus
        .where((m) => !used.contains(m.toLowerCase()))
        .toList();
    if (available.isEmpty) return;
    setState(() {
      _editingMenus.add(
        CustomMenuModel(
          menu: available.first,
          items: const [
            CustomMenuItemModel(
              title: 'New Command',
              action: {'type': 'single_step', 'step_type': 'new_record'},
            ),
          ],
        ),
      );
    });
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Row(
        children: [
          Icon(Icons.menu_open, color: Color(0xFF1E88E5)),
          SizedBox(width: 8),
          Text(
            'Manage Custom Menus',
            style: TextStyle(fontSize: 16, fontWeight: FontWeight.bold),
          ),
        ],
      ),
      content: SizedBox(
        width: 780,
        height: 520,
        child: _loading
            ? const Center(child: CircularProgressIndicator())
            : Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Container(
                    padding: const EdgeInsets.all(10),
                    decoration: BoxDecoration(
                      color: Colors.blue.withValues(alpha: 0.08),
                      borderRadius: BorderRadius.circular(6),
                      border: Border.all(color: Colors.blue.withValues(alpha: 0.35)),
                    ),
                    child: const Text(
                      'A custom menu set adapts the menu bar to your solution. The File menu '
                      'may only be extended so Sign Out and Open Solution remain reachable, and '
                      'mode shortcuts (Cmd/Ctrl+B, F, L, U, Q) are always preserved.',
                      style: TextStyle(fontSize: 11.5),
                    ),
                  ),
                  const SizedBox(height: 10),
                  Expanded(
                    child: Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        // Left column: Menu sets list + per-layout assignment
                        SizedBox(
                          width: 230,
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.stretch,
                            children: [
                              Row(
                                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                                children: [
                                  const Text(
                                    'Menu Sets',
                                    style: TextStyle(
                                      fontSize: 12.5,
                                      fontWeight: FontWeight.bold,
                                    ),
                                  ),
                                  TextButton.icon(
                                    key: const ValueKey('custom-menus-new-set'),
                                    onPressed: _saving
                                        ? null
                                        : () => setState(_startNewSet),
                                    icon: const Icon(Icons.add, size: 15),
                                    label: const Text('New', style: TextStyle(fontSize: 12)),
                                  ),
                                ],
                              ),
                              Expanded(
                                child: DecoratedBox(
                                  decoration: BoxDecoration(
                                    border: Border.all(color: Colors.grey.shade300),
                                    borderRadius: BorderRadius.circular(6),
                                  ),
                                  child: _listing.menuSets.isEmpty
                                      ? const Center(
                                          child: Padding(
                                            padding: EdgeInsets.all(12),
                                            child: Text(
                                              'Standard File4Base Menus only.\nCreate a set on the right.',
                                              textAlign: TextAlign.center,
                                              style: TextStyle(
                                                fontSize: 11.5,
                                                color: Colors.grey,
                                              ),
                                            ),
                                          ),
                                        )
                                      : ListView.builder(
                                          itemCount: _listing.menuSets.length,
                                          itemBuilder: (ctx, i) {
                                            final set = _listing.menuSets[i];
                                            final isSelected = _selected?.id == set.id;
                                            return ListTile(
                                              key: ValueKey('menu-set-row-${set.name}'),
                                              dense: true,
                                              selected: isSelected,
                                              title: Text(
                                                set.name,
                                                style: const TextStyle(fontSize: 12.5),
                                              ),
                                              subtitle: set.isDefault
                                                  ? const Text(
                                                      'Solution default',
                                                      style: TextStyle(
                                                        fontSize: 10.5,
                                                        color: Color(0xFF1E88E5),
                                                      ),
                                                    )
                                                  : null,
                                              onTap: () => setState(() => _selectSet(set)),
                                            );
                                          },
                                        ),
                                ),
                              ),
                              if (_layouts.isNotEmpty) ...[
                                const SizedBox(height: 10),
                                const Text(
                                  'Layout Menu Sets',
                                  style: TextStyle(
                                    fontSize: 12,
                                    fontWeight: FontWeight.bold,
                                  ),
                                ),
                                const SizedBox(height: 4),
                                SizedBox(
                                  height: 120,
                                  child: DecoratedBox(
                                    decoration: BoxDecoration(
                                      border: Border.all(color: Colors.grey.shade300),
                                      borderRadius: BorderRadius.circular(6),
                                    ),
                                    child: ListView.builder(
                                      padding: const EdgeInsets.symmetric(
                                        horizontal: 8,
                                        vertical: 4,
                                      ),
                                      itemCount: _layouts.length,
                                      itemBuilder: (ctx, i) {
                                        final layout = _layouts[i];
                                        final assigned =
                                            layout.definition['menu_set_id'] as String?;
                                        final validAssigned = _listing.menuSets
                                                .any((s) => s.id == assigned)
                                            ? assigned
                                            : '';
                                        return Padding(
                                          padding: const EdgeInsets.symmetric(vertical: 2),
                                          child: Row(
                                            children: [
                                              Expanded(
                                                child: Text(
                                                  layout.name,
                                                  overflow: TextOverflow.ellipsis,
                                                  style: const TextStyle(fontSize: 11.5),
                                                ),
                                              ),
                                              DropdownButton<String>(
                                                key: ValueKey(
                                                  'layout-menu-set-${layout.name}',
                                                ),
                                                isDense: true,
                                                value: validAssigned ?? '',
                                                style: const TextStyle(
                                                  fontSize: 11,
                                                  color: Colors.black87,
                                                ),
                                                items: [
                                                  const DropdownMenuItem(
                                                    value: '',
                                                    child: Text('Default'),
                                                  ),
                                                  for (final s in _listing.menuSets)
                                                    DropdownMenuItem(
                                                      value: s.id,
                                                      child: Text(s.name),
                                                    ),
                                                ],
                                                onChanged: (val) =>
                                                    _assignLayoutMenuSet(layout, val),
                                              ),
                                            ],
                                          ),
                                        );
                                      },
                                    ),
                                  ),
                                ),
                              ],
                            ],
                          ),
                        ),
                        const SizedBox(width: 14),
                        // Right column: Editor for the selected/new menu set
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Row(
                                children: [
                                  Expanded(
                                    child: TextField(
                                      key: const ValueKey('custom-menu-set-name'),
                                      controller: _nameController,
                                      decoration: const InputDecoration(
                                        labelText: 'Menu Set Name',
                                        isDense: true,
                                        border: OutlineInputBorder(),
                                      ),
                                      style: const TextStyle(fontSize: 13),
                                    ),
                                  ),
                                  const SizedBox(width: 12),
                                  FilterChip(
                                    key: const ValueKey('custom-menu-set-default'),
                                    label: const Text(
                                      'Solution default',
                                      style: TextStyle(fontSize: 12),
                                    ),
                                    selected: _isDefault,
                                    onSelected: (v) => setState(() => _isDefault = v),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 8),
                              Row(
                                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                                children: [
                                  const Text(
                                    'Customized Menus',
                                    style: TextStyle(
                                      fontSize: 12.5,
                                      fontWeight: FontWeight.bold,
                                    ),
                                  ),
                                  TextButton.icon(
                                    key: const ValueKey('custom-menu-add-menu'),
                                    onPressed:
                                        _editingMenus.length >= _listing.menus.length
                                            ? null
                                            : _addMenuOverride,
                                    icon: const Icon(Icons.add, size: 15),
                                    label: const Text(
                                      'Add Menu',
                                      style: TextStyle(fontSize: 12),
                                    ),
                                  ),
                                ],
                              ),
                              Expanded(
                                child: ListView.builder(
                                  itemCount: _editingMenus.length,
                                  itemBuilder: (ctx, menuIdx) =>
                                      _buildMenuEditor(menuIdx),
                                ),
                              ),
                            ],
                          ),
                        ),
                      ],
                    ),
                  ),
                  if (_error != null) ...[
                    const SizedBox(height: 8),
                    Text(
                      _error!,
                      key: const ValueKey('custom-menus-error'),
                      style: TextStyle(fontSize: 12, color: Colors.red.shade700),
                    ),
                  ],
                ],
              ),
      ),
      actions: [
        if (_selected != null)
          TextButton.icon(
            key: const ValueKey('custom-menus-delete'),
            onPressed: _saving ? null : _deleteCurrentSet,
            icon: Icon(Icons.delete_outline, size: 16, color: Colors.red.shade700),
            label: Text('Delete Set', style: TextStyle(color: Colors.red.shade700)),
          ),
        TextButton(
          onPressed: _saving ? null : () => Navigator.of(context).pop(),
          child: const Text('Close'),
        ),
        FilledButton(
          key: const ValueKey('custom-menus-save'),
          onPressed: _saving || _loading ? null : _saveCurrentSet,
          child: Text(_saving ? 'Saving…' : 'Save Menu Set'),
        ),
      ],
    );
  }

  static List<CustomMenuItemModel> _standardItemsForMenu(String menuName) {
    switch (menuName.toLowerCase()) {
      case 'records':
        return const [
          CustomMenuItemModel(
            title: 'New Record',
            shortcut: 'cmd+N',
            action: {'type': 'single_step', 'step_type': 'new_record'},
          ),
          CustomMenuItemModel(
            title: 'Duplicate Record',
            shortcut: 'cmd+D',
            action: {'type': 'single_step', 'step_type': 'duplicate_record'},
          ),
          CustomMenuItemModel(
            title: 'Delete Record...',
            shortcut: 'cmd+E',
            action: {'type': 'single_step', 'step_type': 'delete_record'},
          ),
          CustomMenuItemModel(separator: true),
          CustomMenuItemModel(
            title: 'Commit Records',
            action: {'type': 'single_step', 'step_type': 'commit_records'},
          ),
          CustomMenuItemModel(
            title: 'Revert Record...',
            action: {'type': 'single_step', 'step_type': 'revert_record'},
          ),
          CustomMenuItemModel(separator: true),
          CustomMenuItemModel(
            title: 'Show All Records',
            shortcut: 'cmd+J',
            action: {'type': 'single_step', 'step_type': 'show_all_records'},
          ),
        ];
      case 'requests':
        return const [
          CustomMenuItemModel(
            title: 'Enter Find Mode',
            action: {'type': 'single_step', 'step_type': 'enter_find_mode'},
          ),
          CustomMenuItemModel(
            title: 'Perform Find',
            action: {'type': 'single_step', 'step_type': 'perform_find'},
          ),
          CustomMenuItemModel(
            title: 'Show All Records',
            action: {'type': 'single_step', 'step_type': 'show_all_records'},
          ),
        ];
      case 'view':
        return const [
          CustomMenuItemModel(
            title: 'Browse Mode',
            action: {'type': 'single_step', 'step_type': 'enter_browse_mode'},
          ),
          CustomMenuItemModel(
            title: 'Find Mode',
            action: {'type': 'single_step', 'step_type': 'enter_find_mode'},
          ),
        ];
      default:
        return const [
          CustomMenuItemModel(
            title: 'New Record',
            action: {'type': 'single_step', 'step_type': 'new_record'},
          ),
        ];
    }
  }

  Widget _buildMenuEditor(int menuIdx) {
    final menu = _editingMenus[menuIdx];
    final extendOnly = _isExtendOnly(menu.menu);
    final effectiveMode = extendOnly ? 'extend' : menu.mode;

    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(6),
        side: BorderSide(color: Colors.grey.shade300),
      ),
      child: Padding(
        padding: const EdgeInsets.all(10),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Wrap(
              spacing: 8,
              runSpacing: 4,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                DropdownButton<String>(
                  key: ValueKey('custom-menu-target-$menuIdx'),
                  value: _listing.menus.contains(menu.menu)
                      ? menu.menu
                      : _listing.menus.first,
                  isDense: true,
                  items: [
                    for (final m in _listing.menus)
                      DropdownMenuItem(
                        value: m,
                        child: Text(
                          m,
                          style: const TextStyle(
                            fontSize: 12.5,
                            fontWeight: FontWeight.bold,
                          ),
                        ),
                      ),
                  ],
                  onChanged: (next) {
                    if (next == null) return;
                    setState(() {
                      _editingMenus[menuIdx] = CustomMenuModel(
                        menu: next,
                        mode: _isExtendOnly(next) ? 'extend' : menu.mode,
                        items: menu.items,
                      );
                    });
                  },
                ),
                DropdownButton<String>(
                  key: ValueKey('custom-menu-mode-$menuIdx'),
                  value: effectiveMode,
                  isDense: true,
                  items: [
                    const DropdownMenuItem(
                      value: 'extend',
                      child: Text(
                        'Extend standard items',
                        style: TextStyle(fontSize: 12),
                      ),
                    ),
                    if (!extendOnly) ...[
                      const DropdownMenuItem(
                        value: 'replace',
                        child: Text(
                          'Replace standard items',
                          style: TextStyle(fontSize: 12),
                        ),
                      ),
                      const DropdownMenuItem(
                        value: 'hide',
                        child: Text(
                          'Hide entire menu',
                          style: TextStyle(fontSize: 12),
                        ),
                      ),
                    ],
                  ],
                  onChanged: extendOnly
                      ? null
                      : (next) {
                          if (next == null) return;
                          setState(() {
                            final nextItems = next == 'hide'
                                ? const <CustomMenuItemModel>[]
                                : (menu.items.isEmpty
                                    ? _standardItemsForMenu(menu.menu)
                                    : menu.items);
                            _editingMenus[menuIdx] = CustomMenuModel(
                              menu: menu.menu,
                              mode: next,
                              items: nextItems,
                            );
                          });
                        },
                ),
                if (effectiveMode != 'hide') ...[
                  TextButton.icon(
                    key: ValueKey('custom-menu-add-item-$menuIdx'),
                    onPressed: () {
                      setState(() {
                        final nextItems = List<CustomMenuItemModel>.from(menu.items)
                          ..add(
                            const CustomMenuItemModel(
                              title: 'New Item',
                              action: {
                                'type': 'single_step',
                                'step_type': 'new_record',
                              },
                            ),
                          );
                        _editingMenus[menuIdx] = CustomMenuModel(
                          menu: menu.menu,
                          mode: effectiveMode,
                          items: nextItems,
                        );
                      });
                    },
                    icon: const Icon(Icons.add, size: 14),
                    label: const Text('Item', style: TextStyle(fontSize: 11.5)),
                  ),
                  TextButton(
                    key: ValueKey('custom-menu-add-sep-$menuIdx'),
                    onPressed: () {
                      setState(() {
                        final nextItems = List<CustomMenuItemModel>.from(menu.items)
                          ..add(const CustomMenuItemModel(separator: true));
                        _editingMenus[menuIdx] = CustomMenuModel(
                          menu: menu.menu,
                          mode: effectiveMode,
                          items: nextItems,
                        );
                      });
                    },
                    child: const Text('Separator', style: TextStyle(fontSize: 11.5)),
                  ),
                  if (effectiveMode == 'replace')
                    TextButton.icon(
                      key: ValueKey('custom-menu-load-standard-$menuIdx'),
                      onPressed: () {
                        setState(() {
                          _editingMenus[menuIdx] = CustomMenuModel(
                            menu: menu.menu,
                            mode: effectiveMode,
                            items: _standardItemsForMenu(menu.menu),
                          );
                        });
                      },
                      icon: const Icon(Icons.playlist_add_check, size: 15),
                      label: const Text(
                        'Load Standard Items',
                        style: TextStyle(fontSize: 11.5),
                      ),
                    ),
                ],
                if (_editingMenus.length > 1)
                  IconButton(
                    icon: const Icon(Icons.close, size: 16),
                    tooltip: 'Remove menu override',
                    onPressed: () => setState(() => _editingMenus.removeAt(menuIdx)),
                  ),
              ],
            ),
            const Divider(height: 12),
            if (effectiveMode == 'hide')
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 6),
                child: Row(
                  children: [
                    Icon(Icons.visibility_off_outlined,
                        size: 16, color: Colors.orange.shade800),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        'The "${menu.menu}" menu will be hidden from the menu bar when this menu set is active.',
                        style: TextStyle(
                          fontSize: 11.5,
                          color: Colors.orange.shade900,
                        ),
                      ),
                    ),
                  ],
                ),
              )
            else
              for (var itemIdx = 0; itemIdx < menu.items.length; itemIdx++)
                _buildItemRow(menuIdx, itemIdx, menu.items[itemIdx]),
          ],
        ),
      ),
    );
  }

  Widget _buildItemRow(int menuIdx, int itemIdx, CustomMenuItemModel item) {
    final menu = _editingMenus[menuIdx];
    if (item.separator) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 2),
        child: Row(
          children: [
            const Expanded(child: Divider()),
            const Padding(
              padding: EdgeInsets.symmetric(horizontal: 8),
              child: Text(
                'Separator',
                style: TextStyle(fontSize: 11, color: Colors.grey),
              ),
            ),
            const Expanded(child: Divider()),
            IconButton(
              icon: const Icon(Icons.remove_circle_outline, size: 16),
              onPressed: () {
                setState(() {
                  final next = List<CustomMenuItemModel>.from(menu.items)
                    ..removeAt(itemIdx);
                  _editingMenus[menuIdx] = CustomMenuModel(
                    menu: menu.menu,
                    mode: menu.mode,
                    items: next,
                  );
                });
              },
            ),
          ],
        ),
      );
    }

    final actionType = item.action['type'] as String? ?? 'single_step';
    final stepType = item.action['step_type'] as String? ?? 'new_record';
    final scriptId = item.action['script_id'] as String? ?? '';
    final scriptName = item.action['script_name'] as String? ?? '';

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        children: [
          Expanded(
            flex: 3,
            child: TextFormField(
              key: ValueKey('menu-item-title-$menuIdx-$itemIdx-${item.title}'),
              initialValue: item.title,
              decoration: const InputDecoration(
                labelText: 'Title',
                isDense: true,
                border: OutlineInputBorder(),
              ),
              style: const TextStyle(fontSize: 12),
              onChanged: (val) => _updateItem(
                menuIdx,
                itemIdx,
                CustomMenuItemModel(
                  title: val,
                  shortcut: item.shortcut,
                  action: item.action,
                ),
              ),
            ),
          ),
          const SizedBox(width: 6),
          SizedBox(
            width: 105,
            child: TextFormField(
              key: ValueKey('menu-item-shortcut-$menuIdx-$itemIdx'),
              initialValue: item.shortcut,
              decoration: const InputDecoration(
                labelText: 'Shortcut',
                hintText: 'cmd+shift+G',
                isDense: true,
                border: OutlineInputBorder(),
              ),
              style: const TextStyle(fontSize: 11.5),
              onChanged: (val) => _updateItem(
                menuIdx,
                itemIdx,
                CustomMenuItemModel(
                  title: item.title,
                  shortcut: val,
                  action: item.action,
                ),
              ),
            ),
          ),
          const SizedBox(width: 6),
          DropdownButton<String>(
            key: ValueKey('menu-item-action-type-$menuIdx-$itemIdx'),
            value: actionType == 'perform_script' ? 'perform_script' : 'single_step',
            isDense: true,
            style: const TextStyle(fontSize: 11.5, color: Colors.black87),
            items: const [
              DropdownMenuItem(value: 'single_step', child: Text('Step')),
              DropdownMenuItem(value: 'perform_script', child: Text('Script')),
            ],
            onChanged: (next) {
              if (next == null) return;
              if (next == 'perform_script') {
                final firstScript = _scripts.firstOrNull;
                _updateItem(
                  menuIdx,
                  itemIdx,
                  CustomMenuItemModel(
                    title: item.title,
                    shortcut: item.shortcut,
                    action: {
                      'type': 'perform_script',
                      if (firstScript != null) 'script_id': firstScript.id,
                      'script_name': firstScript?.name ?? 'Default Script',
                    },
                  ),
                );
              } else {
                _updateItem(
                  menuIdx,
                  itemIdx,
                  CustomMenuItemModel(
                    title: item.title,
                    shortcut: item.shortcut,
                    action: const {
                      'type': 'single_step',
                      'step_type': 'new_record',
                    },
                  ),
                );
              }
            },
          ),
          const SizedBox(width: 6),
          Expanded(
            flex: 3,
            child: actionType == 'perform_script'
                ? (_scripts.isNotEmpty
                    ? DropdownButton<String>(
                        isExpanded: true,
                        isDense: true,
                        value: _scripts.any((s) => s.id == scriptId)
                            ? scriptId
                            : _scripts.first.id,
                        items: [
                          for (final s in _scripts)
                            DropdownMenuItem(
                              value: s.id,
                              child: Text(
                                s.name,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(fontSize: 11.5),
                              ),
                            ),
                        ],
                        onChanged: (nextId) {
                          final picked =
                              _scripts.where((s) => s.id == nextId).firstOrNull;
                          if (picked == null) return;
                          _updateItem(
                            menuIdx,
                            itemIdx,
                            CustomMenuItemModel(
                              title: item.title,
                              shortcut: item.shortcut,
                              action: {
                                'type': 'perform_script',
                                'script_id': picked.id,
                                'script_name': picked.name,
                              },
                            ),
                          );
                        },
                      )
                    : TextFormField(
                        initialValue: scriptName,
                        decoration: const InputDecoration(
                          labelText: 'Script name',
                          isDense: true,
                          border: OutlineInputBorder(),
                        ),
                        style: const TextStyle(fontSize: 11.5),
                        onChanged: (val) => _updateItem(
                          menuIdx,
                          itemIdx,
                          CustomMenuItemModel(
                            title: item.title,
                            shortcut: item.shortcut,
                            action: {
                              'type': 'perform_script',
                              'script_name': val,
                            },
                          ),
                        ),
                      ))
                : DropdownButton<String>(
                    isExpanded: true,
                    isDense: true,
                    value: _singleSteps.any((e) => e.key == stepType)
                        ? stepType
                        : _singleSteps.first.key,
                    items: [
                      for (final step in _singleSteps)
                        DropdownMenuItem(
                          value: step.key,
                          child: Text(
                            step.value,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(fontSize: 11.5),
                          ),
                        ),
                    ],
                    onChanged: (nextStep) {
                      if (nextStep == null) return;
                      _updateItem(
                        menuIdx,
                        itemIdx,
                        CustomMenuItemModel(
                          title: item.title,
                          shortcut: item.shortcut,
                          action: {
                            'type': 'single_step',
                            'step_type': nextStep,
                          },
                        ),
                      );
                    },
                  ),
          ),
          IconButton(
            icon: const Icon(Icons.remove_circle_outline, size: 16),
            onPressed: menu.items.length <= 1
                ? null
                : () {
                    setState(() {
                      final next = List<CustomMenuItemModel>.from(menu.items)
                        ..removeAt(itemIdx);
                      _editingMenus[menuIdx] = CustomMenuModel(
                        menu: menu.menu,
                        mode: menu.mode,
                        items: next,
                      );
                    });
                  },
          ),
        ],
      ),
    );
  }

  void _updateItem(int menuIdx, int itemIdx, CustomMenuItemModel updated) {
    setState(() {
      final menu = _editingMenus[menuIdx];
      final nextItems = List<CustomMenuItemModel>.from(menu.items);
      nextItems[itemIdx] = updated;
      _editingMenus[menuIdx] = CustomMenuModel(
        menu: menu.menu,
        mode: menu.mode,
        items: nextItems,
      );
    });
  }
}
