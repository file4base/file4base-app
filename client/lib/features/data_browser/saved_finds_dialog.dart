import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';

/// Asks for the name a find is saved under, and warns when the name is
/// already taken on this table so the save does not come back refused (#34).
class SaveFindDialog extends StatefulWidget {
  final String tableLabel;
  final List<SavedFindModel> existing;
  final List<SavedFindRequestModel> requests;

  const SaveFindDialog({
    super.key,
    required this.tableLabel,
    required this.existing,
    required this.requests,
  });

  static Future<String?> show(
    BuildContext context, {
    required String tableLabel,
    required List<SavedFindModel> existing,
    required List<SavedFindRequestModel> requests,
  }) {
    return showDialog<String>(
      context: context,
      builder: (_) => SaveFindDialog(
        tableLabel: tableLabel,
        existing: existing,
        requests: requests,
      ),
    );
  }

  @override
  State<SaveFindDialog> createState() => _SaveFindDialogState();
}

class _SaveFindDialogState extends State<SaveFindDialog> {
  final TextEditingController _name = TextEditingController();

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  bool get _nameTaken => widget.existing
      .any((f) => f.name.trim().toLowerCase() == _name.text.trim().toLowerCase());

  @override
  Widget build(BuildContext context) {
    final requests = widget.requests;
    return AlertDialog(
      title: const Row(
        children: [
          Icon(Icons.bookmark_add_outlined, size: 20, color: Color(0xFF1E88E5)),
          SizedBox(width: 8),
          Text('Save Current Find', style: TextStyle(fontSize: 16, fontWeight: FontWeight.bold)),
        ],
      ),
      content: SizedBox(
        width: 460,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'On ${widget.tableLabel}, ${requests.length} request(s).',
              style: const TextStyle(fontSize: 12, color: Colors.grey),
            ),
            const SizedBox(height: 10),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(10),
              decoration: BoxDecoration(
                borderRadius: BorderRadius.circular(6),
                border: Border.all(color: Colors.grey.shade400),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  for (final request in requests)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 2),
                      child: Text(
                        '${request.omit ? 'Omit' : 'Find'}: '
                        '${request.values.entries.map((e) => '${e.key} ${e.value}').join(', ')}',
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                ],
              ),
            ),
            const SizedBox(height: 14),
            TextField(
              controller: _name,
              autofocus: true,
              decoration: const InputDecoration(
                labelText: 'Name',
                border: OutlineInputBorder(),
                isDense: true,
              ),
              onChanged: (_) => setState(() {}),
              onSubmitted: (_) => _submit(),
            ),
            if (_nameTaken) ...[
              const SizedBox(height: 8),
              Text(
                'This table already has a find called "${_name.text.trim()}".',
                style: TextStyle(fontSize: 12, color: Colors.orange.shade800),
              ),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(
          onPressed: _name.text.trim().isEmpty || _nameTaken ? null : _submit,
          child: const Text('Save'),
        ),
      ],
    );
  }

  void _submit() {
    final name = _name.text.trim();
    if (name.isEmpty || _nameTaken) return;
    Navigator.of(context).pop(name);
  }
}

/// Lists the finds saved on a table so they can be renamed or deleted. It
/// reports what the server answers rather than assuming the change took.
class ManageSavedFindsDialog extends StatefulWidget {
  final ApiClient apiClient;
  final String tableLabel;
  final List<SavedFindModel> finds;

  const ManageSavedFindsDialog({
    super.key,
    required this.apiClient,
    required this.tableLabel,
    required this.finds,
  });

  /// Returns true when anything changed, so the caller reloads its list.
  static Future<bool> show(
    BuildContext context, {
    required ApiClient apiClient,
    required String tableLabel,
    required List<SavedFindModel> finds,
  }) async {
    final changed = await showDialog<bool>(
      context: context,
      builder: (_) => ManageSavedFindsDialog(
        apiClient: apiClient,
        tableLabel: tableLabel,
        finds: finds,
      ),
    );
    return changed ?? false;
  }

  @override
  State<ManageSavedFindsDialog> createState() => _ManageSavedFindsDialogState();
}

class _ManageSavedFindsDialogState extends State<ManageSavedFindsDialog> {
  late List<SavedFindModel> _finds = List.of(widget.finds);
  bool _changed = false;
  String? _error;
  bool _busy = false;

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Row(
        children: [
          const Icon(Icons.bookmarks_outlined, size: 20, color: Color(0xFF1E88E5)),
          const SizedBox(width: 8),
          Text('Saved Finds — ${widget.tableLabel}',
              style: const TextStyle(fontSize: 16, fontWeight: FontWeight.bold)),
        ],
      ),
      content: SizedBox(
        width: 560,
        height: 360,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (_error != null) ...[
              Text(_error!, style: TextStyle(fontSize: 12, color: Colors.red.shade700)),
              const SizedBox(height: 8),
            ],
            Expanded(
              child: _finds.isEmpty
                  ? const Center(
                      child: Text('No finds are saved on this table.',
                          style: TextStyle(fontSize: 13, color: Colors.grey)),
                    )
                  : ListView.separated(
                      itemCount: _finds.length,
                      separatorBuilder: (_, __) => const Divider(height: 1),
                      itemBuilder: (context, index) {
                        final find = _finds[index];
                        return ListTile(
                          dense: true,
                          title: Text(find.name, style: const TextStyle(fontSize: 13)),
                          subtitle: Text(
                            find.summary,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(fontSize: 11, color: Colors.grey),
                          ),
                          trailing: Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              IconButton(
                                tooltip: 'Rename',
                                icon: const Icon(Icons.edit_outlined, size: 18),
                                onPressed: _busy ? null : () => _rename(find),
                              ),
                              IconButton(
                                tooltip: 'Delete',
                                icon: Icon(Icons.delete_outline, size: 18, color: Colors.red.shade400),
                                onPressed: _busy ? null : () => _delete(find),
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
      actions: [
        FilledButton(
          onPressed: () => Navigator.of(context).pop(_changed),
          child: const Text('Done'),
        ),
      ],
    );
  }

  Future<void> _rename(SavedFindModel find) async {
    final name = await showDialog<String>(
      context: context,
      builder: (_) => _RenameSavedFindDialog(currentName: find.name),
    );
    if (name == null || name.isEmpty || name == find.name) return;

    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final updated = await widget.apiClient
          .updateSavedFind(find.id, name: name, requests: find.requests);
      if (!mounted) return;
      setState(() {
        _finds = [
          for (final f in _finds) f.id == find.id ? updated : f,
        ];
        _changed = true;
        _busy = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString().replaceFirst('Exception: ', '');
        _busy = false;
      });
    }
  }

  Future<void> _delete(SavedFindModel find) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete Saved Find', style: TextStyle(fontSize: 15)),
        content: Text('Delete "${find.name}"? The records it finds are not affected.',
            style: const TextStyle(fontSize: 13)),
        actions: [
          TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Colors.red.shade700),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.apiClient.deleteSavedFind(find.id);
      if (!mounted) return;
      setState(() {
        _finds = [for (final f in _finds) if (f.id != find.id) f];
        _changed = true;
        _busy = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString().replaceFirst('Exception: ', '');
        _busy = false;
      });
    }
  }
}

/// Asks for a new name. It owns its controller, so the controller outlives
/// the dialog's closing animation and is disposed with it.
class _RenameSavedFindDialog extends StatefulWidget {
  final String currentName;

  const _RenameSavedFindDialog({required this.currentName});

  @override
  State<_RenameSavedFindDialog> createState() => _RenameSavedFindDialogState();
}

class _RenameSavedFindDialogState extends State<_RenameSavedFindDialog> {
  late final TextEditingController _controller =
      TextEditingController(text: widget.currentName);

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _submit() => Navigator.of(context).pop(_controller.text.trim());

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Rename Saved Find', style: TextStyle(fontSize: 15)),
      content: TextField(
        controller: _controller,
        autofocus: true,
        decoration: const InputDecoration(labelText: 'Name', border: OutlineInputBorder(), isDense: true),
        onSubmitted: (_) => _submit(),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(onPressed: _submit, child: const Text('Rename')),
      ],
    );
  }
}
