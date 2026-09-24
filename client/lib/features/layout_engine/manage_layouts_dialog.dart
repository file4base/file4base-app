import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/api/api_client.dart';
import '../../main.dart';
import 'models/layout_definition.dart';

/// ManageLayoutsDialog allows users to view all layouts in the solution,
/// create new layouts, rename existing layouts, duplicate layouts,
/// and switch between them.
class ManageLayoutsDialog extends ConsumerStatefulWidget {
  final List<LayoutModel> layouts;
  final List<TableModel> tables;
  final LayoutModel? activeLayout;
  final ValueChanged<LayoutModel> onSelectLayout;
  final VoidCallback onLayoutsChanged;

  const ManageLayoutsDialog({
    super.key,
    required this.layouts,
    required this.tables,
    required this.activeLayout,
    required this.onSelectLayout,
    required this.onLayoutsChanged,
  });

  static Future<void> show(
    BuildContext context, {
    required List<LayoutModel> layouts,
    required List<TableModel> tables,
    required LayoutModel? activeLayout,
    required ValueChanged<LayoutModel> onSelectLayout,
    required VoidCallback onLayoutsChanged,
  }) async {
    await showDialog(
      context: context,
      barrierDismissible: true,
      builder: (ctx) => ManageLayoutsDialog(
        layouts: layouts,
        tables: tables,
        activeLayout: activeLayout,
        onSelectLayout: onSelectLayout,
        onLayoutsChanged: onLayoutsChanged,
      ),
    );
  }

  @override
  ConsumerState<ManageLayoutsDialog> createState() => _ManageLayoutsDialogState();
}

class _ManageLayoutsDialogState extends ConsumerState<ManageLayoutsDialog> {
  late List<LayoutModel> _layouts;
  String? _selectedLayoutId;
  bool _isLoading = false;

  @override
  void initState() {
    super.initState();
    _layouts = List.from(widget.layouts);
    _selectedLayoutId = widget.activeLayout?.id ??
        (_layouts.isNotEmpty ? _layouts.first.id : null);
  }

  Future<void> _refreshLayouts() async {
    setState(() => _isLoading = true);
    final client = ref.read(apiClientProvider);
    try {
      final updated = await client.listLayouts();
      if (mounted) {
        setState(() {
          _layouts = updated;
          if (_selectedLayoutId != null &&
              !_layouts.any((l) => l.id == _selectedLayoutId)) {
            _selectedLayoutId = _layouts.isNotEmpty ? _layouts.first.id : null;
          }
          _isLoading = false;
        });
        widget.onLayoutsChanged();
      }
    } catch (_) {
      if (mounted) setState(() => _isLoading = false);
    }
  }

  String _resolveTableName(LayoutModel layout) {
    final toName = layout.definition['table_occurrence']?.toString();
    if (toName != null && toName.isNotEmpty) {
      return toName;
    }
    final match = widget.tables
        .where((t) =>
            t.id == layout.tableOccurrenceId ||
            t.displayName.toLowerCase() == layout.name.toLowerCase())
        .firstOrNull;
    return match?.displayName ?? 'Default Table';
  }

  Future<void> _handleNewLayout() async {
    final nameCtrl = TextEditingController(text: 'New Layout');
    TableModel? selectedTable = widget.tables.isNotEmpty ? widget.tables.first : null;
    String layoutType = 'form'; // 'form' or 'list'

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDlgState) {
          return AlertDialog(
            title: const Row(
              children: [
                Icon(Icons.add_to_photos, color: Color(0xFF1E88E5)),
                SizedBox(width: 8),
                Text('New Layout / Presentation'),
              ],
            ),
            content: SizedBox(
              width: 440,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text(
                    'Layout Name:',
                    style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
                  ),
                  const SizedBox(height: 6),
                  TextField(
                    controller: nameCtrl,
                    autofocus: true,
                    decoration: const InputDecoration(
                      border: OutlineInputBorder(),
                      isDense: true,
                      hintText: 'e.g. Customers Form, Invoice Details',
                    ),
                  ),
                  const SizedBox(height: 16),
                  const Text(
                    'Show records from (Table Occurrence):',
                    style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
                  ),
                  const SizedBox(height: 6),
                  DropdownButtonFormField<String>(
                    value: selectedTable?.id,
                    decoration: const InputDecoration(
                      border: OutlineInputBorder(),
                      isDense: true,
                    ),
                    items: widget.tables.map((t) {
                      return DropdownMenuItem<String>(
                        value: t.id,
                        child: Text('${t.displayName} (${t.name})'),
                      );
                    }).toList(),
                    onChanged: (id) {
                      if (id != null) {
                        setDlgState(() {
                          selectedTable =
                              widget.tables.firstWhere((t) => t.id == id);
                        });
                      }
                    },
                  ),
                  const SizedBox(height: 16),
                  const Text(
                    'Layout Style:',
                    style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
                  ),
                  const SizedBox(height: 6),
                  SegmentedButton<String>(
                    segments: const [
                      ButtonSegment(
                        value: 'form',
                        label: Text('Form View'),
                        icon: Icon(Icons.dashboard_outlined),
                      ),
                      ButtonSegment(
                        value: 'list',
                        label: Text('List View'),
                        icon: Icon(Icons.view_list_outlined),
                      ),
                    ],
                    selected: {layoutType},
                    onSelectionChanged: (val) {
                      setDlgState(() => layoutType = val.first);
                    },
                  ),
                ],
              ),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(ctx).pop(false),
                child: const Text('Cancel'),
              ),
              FilledButton(
                onPressed: () => Navigator.of(ctx).pop(true),
                child: const Text('Create Layout'),
              ),
            ],
          );
        },
      ),
    );

    if (confirmed == true && selectedTable != null) {
      final name = nameCtrl.text.trim().isEmpty ? 'Untitled Layout' : nameCtrl.text.trim();
      final client = ref.read(apiClientProvider);

      final def = LayoutDefinitionModel.defaultForTable(
        selectedTable!.displayName,
        selectedTable!.columns.map((c) => c.name).toList(),
      ).copyWith(name: name, defaultView: layoutType);

      try {
        final created = await client.createLayout(
          name,
          toId: selectedTable!.id,
          definition: def.toJson(),
        );
        await _refreshLayouts();
        if (mounted) {
          widget.onSelectLayout(created);
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('Created layout "$name" successfully!'),
              backgroundColor: Colors.green.shade700,
            ),
          );
        }
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('Failed to create layout: $e'),
              backgroundColor: Colors.red,
            ),
          );
        }
      }
    }
  }

  Future<void> _handleRenameLayout(LayoutModel layout) async {
    final nameCtrl = TextEditingController(text: layout.name);

    final newName = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Row(
          children: [
            Icon(Icons.edit, color: Color(0xFF1E88E5)),
            SizedBox(width: 8),
            Text('Rename Layout'),
          ],
        ),
        content: SizedBox(
          width: 380,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('Enter new layout name:', style: TextStyle(fontSize: 13)),
              const SizedBox(height: 8),
              TextField(
                controller: nameCtrl,
                autofocus: true,
                decoration: const InputDecoration(
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
                onSubmitted: (v) => Navigator.of(ctx).pop(v.trim()),
              ),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(ctx).pop(nameCtrl.text.trim()),
            child: const Text('Rename'),
          ),
        ],
      ),
    );

    if (newName != null && newName.isNotEmpty && newName != layout.name) {
      final client = ref.read(apiClientProvider);
      try {
        final updatedDef = Map<String, dynamic>.from(layout.definition);
        updatedDef['name'] = newName;

        final updated = await client.updateLayout(
          layout.id,
          newName,
          updatedDef,
        );
        await _refreshLayouts();
        if (mounted) {
          widget.onSelectLayout(updated);
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('Layout renamed to "$newName"'),
              backgroundColor: Colors.green.shade700,
            ),
          );
        }
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('Failed to rename layout: $e'),
              backgroundColor: Colors.red,
            ),
          );
        }
      }
    }
  }

  Future<void> _handleDuplicateLayout(LayoutModel layout) async {
    final client = ref.read(apiClientProvider);
    final dupName = '${layout.name} Copy';
    final dupDef = Map<String, dynamic>.from(layout.definition);
    dupDef['name'] = dupName;

    try {
      final created = await client.createLayout(
        dupName,
        toId: layout.tableOccurrenceId,
        definition: dupDef,
      );
      await _refreshLayouts();
      if (mounted) {
        widget.onSelectLayout(created);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Duplicated layout as "$dupName"'),
            backgroundColor: Colors.green.shade700,
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Failed to duplicate layout: $e'),
            backgroundColor: Colors.red,
          ),
        );
      }
    }
  }

  Future<void> _handleDeleteLayout(LayoutModel layout) async {
    if (_layouts.length <= 1) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Cannot delete the only layout in the solution.'),
          backgroundColor: Colors.orange,
        ),
      );
      return;
    }

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Row(
          children: [
            Icon(Icons.warning_amber_rounded, color: Colors.red),
            SizedBox(width: 8),
            Text('Delete Layout'),
          ],
        ),
        content: Text(
          'Are you sure you want to delete layout "${layout.name}"?\nThis action cannot be undone.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Colors.red),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );

    if (confirmed == true) {
      final client = ref.read(apiClientProvider);
      try {
        await client.deleteLayout(layout.id);
        await _refreshLayouts();
        if (_layouts.isNotEmpty && mounted) {
          widget.onSelectLayout(_layouts.first);
        }
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('Layout "${layout.name}" deleted.'),
              backgroundColor: Colors.green.shade700,
            ),
          );
        }
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('Failed to delete layout: $e'),
              backgroundColor: Colors.red,
            ),
          );
        }
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final isDark = Theme.of(context).brightness == Brightness.dark;

    return Dialog(
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 820, maxHeight: 600),
        child: Padding(
          padding: const EdgeInsets.all(24.0),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              // Dialog Header
              Row(
                children: [
                  Container(
                    padding: const EdgeInsets.all(8),
                    decoration: BoxDecoration(
                      color: const Color(0xFF1E88E5).withValues(alpha: 0.12),
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: const Icon(Icons.view_quilt, color: Color(0xFF1E88E5), size: 24),
                  ),
                  const SizedBox(width: 12),
                  const Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'Manage Layouts',
                        style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
                      ),
                      Text(
                        'Create, rename, organize, and switch layouts (presentations)',
                        style: TextStyle(fontSize: 12, color: Colors.grey),
                      ),
                    ],
                  ),
                  const Spacer(),
                  FilledButton.icon(
                    icon: const Icon(Icons.add, size: 16),
                    label: const Text('New Layout...'),
                    onPressed: _handleNewLayout,
                  ),
                ],
              ),
              const SizedBox(height: 16),
              const Divider(height: 1),
              const SizedBox(height: 12),

              // Layouts list
              Expanded(
                child: _isLoading
                    ? const Center(child: CircularProgressIndicator())
                    : _layouts.isEmpty
                        ? Center(
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                const Icon(Icons.inbox, size: 48, color: Colors.grey),
                                const SizedBox(height: 8),
                                const Text('No layouts found.'),
                                const SizedBox(height: 12),
                                FilledButton(
                                  onPressed: _handleNewLayout,
                                  child: const Text('Create First Layout'),
                                ),
                              ],
                            ),
                          )
                        : Container(
                            decoration: BoxDecoration(
                              border: Border.all(
                                color: isDark
                                    ? const Color(0xFF38404B)
                                    : const Color(0xFFE0E0E0),
                              ),
                              borderRadius: BorderRadius.circular(8),
                            ),
                            child: ListView.separated(
                              itemCount: _layouts.length,
                              separatorBuilder: (_, __) =>
                                  const Divider(height: 1),
                              itemBuilder: (ctx, idx) {
                                final layout = _layouts[idx];
                                final isSelected =
                                    layout.id == _selectedLayoutId;
                                final tableName = _resolveTableName(layout);
                                final objCount =
                                    (layout.definition['objects'] as List<dynamic>?)
                                            ?.length ??
                                        0;

                                return ListTile(
                                  selected: isSelected,
                                  selectedTileColor: const Color(0xFF1E88E5)
                                      .withValues(alpha: 0.1),
                                  leading: Icon(
                                    Icons.dashboard_customize_outlined,
                                    color: isSelected
                                        ? const Color(0xFF1E88E5)
                                        : Colors.grey,
                                  ),
                                  title: Row(
                                    children: [
                                      Text(
                                        layout.name,
                                        style: TextStyle(
                                          fontWeight: isSelected
                                              ? FontWeight.bold
                                              : FontWeight.w600,
                                          fontSize: 14,
                                        ),
                                      ),
                                      const SizedBox(width: 8),
                                      Container(
                                        padding: const EdgeInsets.symmetric(
                                            horizontal: 6, vertical: 2),
                                        decoration: BoxDecoration(
                                          color: isDark
                                              ? const Color(0xFF272D37)
                                              : Colors.grey.shade200,
                                          borderRadius:
                                              BorderRadius.circular(4),
                                        ),
                                        child: Text(
                                          'Table: $tableName',
                                          style: const TextStyle(
                                              fontSize: 11, color: Colors.grey),
                                        ),
                                      ),
                                    ],
                                  ),
                                  subtitle: Text(
                                    '$objCount layout objects • ID: ${layout.id.length > 8 ? layout.id.substring(0, 8) : layout.id}',
                                    style: const TextStyle(fontSize: 11),
                                  ),
                                  trailing: Row(
                                    mainAxisSize: MainAxisSize.min,
                                    children: [
                                      Tooltip(
                                        message: 'Rename Layout',
                                        child: IconButton(
                                          icon: const Icon(Icons.edit,
                                              size: 18),
                                          onPressed: () =>
                                              _handleRenameLayout(layout),
                                        ),
                                      ),
                                      Tooltip(
                                        message: 'Duplicate Layout',
                                        child: IconButton(
                                          icon: const Icon(Icons.copy,
                                              size: 18),
                                          onPressed: () =>
                                              _handleDuplicateLayout(layout),
                                        ),
                                      ),
                                      Tooltip(
                                        message: 'Delete Layout',
                                        child: IconButton(
                                          icon: const Icon(
                                              Icons.delete_outline,
                                              size: 18),
                                          onPressed: _layouts.length > 1
                                              ? () =>
                                                  _handleDeleteLayout(layout)
                                              : null,
                                        ),
                                      ),
                                      const SizedBox(width: 4),
                                      FilledButton.tonal(
                                        onPressed: () {
                                          widget.onSelectLayout(layout);
                                          Navigator.of(context).pop();
                                        },
                                        child: const Text('Open'),
                                      ),
                                    ],
                                  ),
                                  onTap: () {
                                    setState(() =>
                                        _selectedLayoutId = layout.id);
                                  },
                                );
                              },
                            ),
                          ),
              ),

              const SizedBox(height: 16),
              const Divider(height: 1),
              const SizedBox(height: 12),

              // Bottom Actions
              Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Text(
                    '${_layouts.length} layouts in solution',
                    style: const TextStyle(fontSize: 12, color: Colors.grey),
                  ),
                  OutlinedButton(
                    onPressed: () => Navigator.of(context).pop(),
                    child: const Text('Done'),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}
