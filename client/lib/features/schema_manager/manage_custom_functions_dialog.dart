import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/script_workspace/calculation_builder_dialog.dart';
import 'package:file4base_client/main.dart' show apiClientProvider;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// File > Manage > Custom Functions (docs/specs/custom_functions.md).
///
/// A custom function is a named formula with parameters that every formula
/// of the solution may call — fields, validation, scripts, buttons. As in
/// FileMaker, renaming one rewrites the formulas that call it, and a change
/// that would break them, or deleting one they call, is refused unless the
/// person says to go ahead.
class ManageCustomFunctionsDialog extends ConsumerStatefulWidget {
  const ManageCustomFunctionsDialog({super.key});

  static Future<void> show(BuildContext context) {
    return showDialog(
      context: context,
      builder: (_) => const ManageCustomFunctionsDialog(),
    );
  }

  @override
  ConsumerState<ManageCustomFunctionsDialog> createState() => _ManageCustomFunctionsDialogState();
}

class _ManageCustomFunctionsDialogState extends ConsumerState<ManageCustomFunctionsDialog> {
  List<CustomFunctionModel> _functions = [];
  bool _loading = true;
  String? _error;
  String? _selectedId;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  ApiClient get _client => ref.read(apiClientProvider);

  CustomFunctionModel? get _selected => _functions.where((f) => f.id == _selectedId).firstOrNull;

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final list = await _client.listCustomFunctions();
      if (!mounted) return;
      setState(() {
        _functions = list;
        _loading = false;
        if (_selected == null) _selectedId = null;
      });
    } on Object catch (e) {
      if (!mounted) return;
      setState(() {
        _error = '$e';
        _loading = false;
      });
    }
  }

  void _say(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
  }

  /// The formulas a conflict lists, and whether the person goes ahead.
  Future<bool> _confirmConflict(ApiException e, String action) async {
    final usages = CustomFunctionUsage.listFrom(e.raw['usages']);
    final go = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Custom function in use'),
        content: SizedBox(
          width: 480,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(e.detail),
              const SizedBox(height: 12),
              ConstrainedBox(
                constraints: const BoxConstraints(maxHeight: 240),
                child: ListView(
                  shrinkWrap: true,
                  children: [
                    for (final u in usages)
                      Text('• ${u.describe}', key: const Key('customFunctionUsage'), style: const TextStyle(fontSize: 12)),
                  ],
                ),
              ),
            ],
          ),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          FilledButton(
            key: const Key('customFunctionForce'),
            style: FilledButton.styleFrom(backgroundColor: Colors.red.shade700),
            onPressed: () => Navigator.pop(ctx, true),
            child: Text('$action anyway'),
          ),
        ],
      ),
    );
    return go ?? false;
  }

  /// A conflict the person may override, as a 409 with the formulas.
  bool _isConflict(Object e) => e is ApiException && e.statusCode == 409;

  Future<void> _edit({CustomFunctionModel? existing}) async {
    final edited = await showDialog<CustomFunctionModel>(
      context: context,
      builder: (_) => CustomFunctionEditor(existing: existing, others: _functions),
    );
    if (edited == null) return;
    try {
      final saved = existing == null
          ? await _client.createCustomFunction(edited)
          : await _save(edited.copyWith(id: existing.id));
      if (saved == null) return;
      _selectedId = saved.id;
      await _load();
    } on Object catch (e) {
      _say('Could not save the custom function: ${e is ApiException ? e.detail : e}');
    }
  }

  /// Saves a change, asking first when it would break formulas calling it.
  Future<CustomFunctionModel?> _save(CustomFunctionModel fn) async {
    try {
      return await _client.updateCustomFunction(fn);
    } on ApiException catch (e) {
      if (!_isConflict(e) || !mounted) rethrow;
      if (!await _confirmConflict(e, 'Save')) return null;
      return _client.updateCustomFunction(fn, force: true);
    }
  }

  Future<void> _duplicate(CustomFunctionModel fn) async {
    final taken = {for (final f in _functions) f.name.toLowerCase()};
    var name = '${fn.name}Copy';
    for (var n = 2; taken.contains(name.toLowerCase()); n++) {
      name = '${fn.name}Copy$n';
    }
    try {
      final copy = await _client.createCustomFunction(fn.copyWith(id: '', name: name));
      _selectedId = copy.id;
      await _load();
    } on Object catch (e) {
      _say('Could not duplicate the custom function: ${e is ApiException ? e.detail : e}');
    }
  }

  Future<void> _delete(CustomFunctionModel fn) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete custom function'),
        content: Text('Delete "${fn.name}"?'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          FilledButton(
            key: const Key('customFunctionConfirmDelete'),
            style: FilledButton.styleFrom(backgroundColor: Colors.red.shade700),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      try {
        await _client.deleteCustomFunction(fn.id);
      } on ApiException catch (e) {
        if (!_isConflict(e) || !mounted) rethrow;
        if (!await _confirmConflict(e, 'Delete')) return;
        await _client.deleteCustomFunction(fn.id, force: true);
      }
      _selectedId = null;
      await _load();
    } on Object catch (e) {
      _say('Could not delete the custom function: ${e is ApiException ? e.detail : e}');
    }
  }

  @override
  Widget build(BuildContext context) {
    final selected = _selected;
    return Dialog(
      child: SizedBox(
        width: 820,
        height: 560,
        child: Column(
          children: [
            ListTile(
              leading: const Icon(Icons.functions),
              title: const Text('Manage Custom Functions', style: TextStyle(fontWeight: FontWeight.bold)),
              subtitle: const Text(
                'Named formulas every calculation of the solution may call',
                style: TextStyle(fontSize: 12),
              ),
              trailing: IconButton(
                icon: const Icon(Icons.close),
                onPressed: () => Navigator.of(context).pop(),
              ),
            ),
            const Divider(height: 1),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      '${_functions.length} ${_functions.length == 1 ? 'custom function' : 'custom functions'}',
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 12, color: Colors.grey),
                    ),
                  ),
                  Flexible(
                    flex: 4,
                    child: Wrap(
                      alignment: WrapAlignment.end,
                      spacing: 8,
                      runSpacing: 4,
                      children: [
                        FilledButton.icon(
                          key: const Key('customFunctionNew'),
                          icon: const Icon(Icons.add, size: 16),
                          label: const Text('New...'),
                          onPressed: _edit,
                        ),
                        OutlinedButton(
                          key: const Key('customFunctionEdit'),
                          onPressed: selected == null ? null : () => _edit(existing: selected),
                          child: const Text('Edit...'),
                        ),
                        OutlinedButton(
                          key: const Key('customFunctionDuplicate'),
                          onPressed: selected == null ? null : () => _duplicate(selected),
                          child: const Text('Duplicate'),
                        ),
                        OutlinedButton(
                          key: const Key('customFunctionDelete'),
                          onPressed: selected == null ? null : () => _delete(selected),
                          child: const Text('Delete'),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
            const Divider(height: 1),
            Expanded(child: _buildBody()),
          ],
        ),
      ),
    );
  }

  Widget _buildBody() {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error != null) {
      return Center(child: Text('Could not read the custom functions: $_error', style: const TextStyle(color: Colors.red)));
    }
    if (_functions.isEmpty) {
      return const Center(child: Text('No custom functions yet.', style: TextStyle(color: Colors.grey)));
    }
    return ListView.separated(
      itemCount: _functions.length,
      separatorBuilder: (_, _) => const Divider(height: 1),
      itemBuilder: (context, i) {
        final fn = _functions[i];
        return ListTile(
          dense: true,
          selected: fn.id == _selectedId,
          title: Text(fn.signature, style: const TextStyle(fontFamily: 'monospace')),
          subtitle: Text(
            fn.formula.replaceAll('\n', ' '),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: const TextStyle(fontSize: 11),
          ),
          trailing: Text(
            fn.fullAccessOnly ? 'Full access only' : 'All accounts',
            style: const TextStyle(fontSize: 11, color: Colors.grey),
          ),
          onTap: () => setState(() => _selectedId = fn.id),
          onLongPress: () => _edit(existing: fn),
        );
      },
    );
  }
}

/// Edit Custom Function: the name, the parameters, the formula (written
/// with the calculation editor, which checks it with the server) and who
/// may see it. Returns the function as edited, not saved.
class CustomFunctionEditor extends StatefulWidget {
  const CustomFunctionEditor({required this.others, super.key, this.existing});

  final CustomFunctionModel? existing;

  /// The solution's custom functions, to refuse a name already taken.
  final List<CustomFunctionModel> others;

  @override
  State<CustomFunctionEditor> createState() => _CustomFunctionEditorState();
}

class _CustomFunctionEditorState extends State<CustomFunctionEditor> {
  late final TextEditingController _name;
  late final TextEditingController _formula;
  final TextEditingController _parameter = TextEditingController();
  late List<String> _parameters;
  late String _access;
  String? _problem;

  /// A name a formula can call: no space, no operator or punctuation of the
  /// calculation language, not starting with a digit.
  static final _identifier = RegExp(r'^[^\s\d"();:+\-*/&=<>≠≤≥^,\[\]{}$¶][^\s"();:+\-*/&=<>≠≤≥^,\[\]{}$¶]*$');

  @override
  void initState() {
    super.initState();
    final fn = widget.existing;
    _name = TextEditingController(text: fn?.name ?? '');
    _formula = TextEditingController(text: fn?.formula ?? '');
    _parameters = [...?fn?.parameters];
    _access = fn?.access ?? 'all';
  }

  @override
  void dispose() {
    _name.dispose();
    _formula.dispose();
    _parameter.dispose();
    super.dispose();
  }

  void _addParameter() {
    final p = _parameter.text.trim();
    if (p.isEmpty) return;
    if (!_identifier.hasMatch(p)) {
      setState(() => _problem = '"$p" is not a valid parameter name.');
      return;
    }
    if (_parameters.any((x) => x.toLowerCase() == p.toLowerCase())) {
      setState(() => _problem = 'There is already a parameter "$p".');
      return;
    }
    setState(() {
      _parameters.add(p);
      _parameter.clear();
      _problem = null;
    });
  }

  Future<void> _editFormula() async {
    final formula = await CalculationBuilderDialog.show(
      context,
      initialFormula: _formula.text,
      functionName: _name.text.trim(),
      functionParameters: _parameters,
    );
    if (formula != null) setState(() => _formula.text = formula);
  }

  void _ok() {
    final name = _name.text.trim();
    String? problem;
    if (name.isEmpty) {
      problem = 'A custom function needs a name.';
    } else if (name != widget.existing?.name && !_identifier.hasMatch(name)) {
      problem = '"$name" is not a valid function name: no spaces or operators, not starting with a digit.';
    } else if (widget.others.any((f) => f.id != widget.existing?.id && f.name.toLowerCase() == name.toLowerCase())) {
      problem = 'There is already a custom function named "$name".';
    } else if (_formula.text.trim().isEmpty) {
      problem = 'A custom function needs a formula.';
    }
    if (problem != null) {
      setState(() => _problem = problem);
      return;
    }
    Navigator.of(context).pop(CustomFunctionModel(
      id: widget.existing?.id ?? '',
      name: name,
      parameters: _parameters,
      formula: _formula.text,
      access: _access,
    ));
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(widget.existing == null ? 'New Custom Function' : 'Edit Custom Function'),
      content: SizedBox(
        width: 560,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              TextField(
                key: const Key('customFunctionName'),
                controller: _name,
                decoration: const InputDecoration(labelText: 'Function name'),
              ),
              const SizedBox(height: 12),
              const Text('Function parameters', style: TextStyle(fontSize: 12, fontWeight: FontWeight.bold)),
              Wrap(
                spacing: 6,
                children: [
                  for (final p in _parameters)
                    InputChip(
                      label: Text(p),
                      onDeleted: () => setState(() => _parameters.remove(p)),
                    ),
                ],
              ),
              Row(
                children: [
                  Expanded(
                    child: TextField(
                      key: const Key('customFunctionParameter'),
                      controller: _parameter,
                      decoration: const InputDecoration(hintText: 'Parameter name'),
                      onSubmitted: (_) => _addParameter(),
                    ),
                  ),
                  TextButton(
                    key: const Key('customFunctionAddParameter'),
                    onPressed: _addParameter,
                    child: const Text('Add'),
                  ),
                ],
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  const Expanded(child: Text('Formula', style: TextStyle(fontSize: 12, fontWeight: FontWeight.bold))),
                  TextButton.icon(
                    key: const Key('customFunctionEditFormula'),
                    icon: const Icon(Icons.functions, size: 16),
                    label: const Text('Specify...'),
                    onPressed: _editFormula,
                  ),
                ],
              ),
              TextField(
                key: const Key('customFunctionFormula'),
                controller: _formula,
                minLines: 4,
                maxLines: 10,
                style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
                decoration: const InputDecoration(border: OutlineInputBorder()),
              ),
              const SizedBox(height: 12),
              const Text('Available to', style: TextStyle(fontSize: 12, fontWeight: FontWeight.bold)),
              RadioGroup<String>(
                groupValue: _access,
                onChanged: (v) => setState(() => _access = v ?? 'all'),
                child: const Column(
                  children: [
                    RadioListTile<String>(
                      dense: true,
                      value: 'all',
                      title: Text('All accounts'),
                    ),
                    RadioListTile<String>(
                      key: Key('customFunctionFullAccess'),
                      dense: true,
                      value: 'full_access',
                      title: Text('Only accounts assigned full access privileges'),
                    ),
                  ],
                ),
              ),
              if (_problem != null)
                Text(_problem!, key: const Key('customFunctionProblem'), style: const TextStyle(color: Colors.red)),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(key: const Key('customFunctionOk'), onPressed: _ok, child: const Text('OK')),
      ],
    );
  }
}
