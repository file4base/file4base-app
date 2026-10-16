import 'dart:async';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter/material.dart';

/// "Button Setup": the label of a layout button and what a click on it runs
/// in Browse mode (nothing, a single step, or a stored script).
///
/// Opened from Layout mode with a double click on the button, its context
/// menu, Enter, or the inspector. Returns the updated button, or null when
/// cancelled.
class ButtonSetupDialog extends StatefulWidget {

  const ButtonSetupDialog({
    required this.button, required this.apiClient, super.key,
    this.layoutNames = const [],
    this.columns = const [],
    this.contextTable = '',
    this.onOpenScriptWorkspace,
  });
  final LayoutObjectModel button;
  final ApiClient apiClient;
  final List<String> layoutNames;
  final List<ColumnModel> columns;
  final String contextTable;

  /// Opens the Script Workspace; completes when it closes.
  final Future<void> Function()? onOpenScriptWorkspace;

  static Future<LayoutObjectModel?> show(
    BuildContext context, {
    required LayoutObjectModel button,
    required ApiClient apiClient,
    List<String> layoutNames = const [],
    List<ColumnModel> columns = const [],
    String contextTable = '',
    Future<void> Function()? onOpenScriptWorkspace,
  }) {
    return showDialog<LayoutObjectModel>(
      context: context,
      builder: (_) => ButtonSetupDialog(
        button: button,
        apiClient: apiClient,
        layoutNames: layoutNames,
        columns: columns,
        contextTable: contextTable,
        onOpenScriptWorkspace: onOpenScriptWorkspace,
      ),
    );
  }

  @override
  State<ButtonSetupDialog> createState() => _ButtonSetupDialogState();
}

class _ButtonSetupDialogState extends State<ButtonSetupDialog> {
  late final TextEditingController _label = TextEditingController(text: widget.button.text);
  late final TextEditingController _parameter = TextEditingController(text: widget.button.action?.parameter ?? '');
  final Map<String, TextEditingController> _paramCtrls = {};

  late String _kind = widget.button.action == null
      ? 'none'
      : (widget.button.action!.isPerformScript ? 'perform_script' : 'single_step');
  late String _stepType = widget.button.action?.stepType ?? 'new_record';
  late Map<String, dynamic> _params = Map<String, dynamic>.from(widget.button.action?.params ?? const {});
  late String? _scriptId = widget.button.action?.scriptId;
  late String? _scriptName = widget.button.action?.scriptName;

  List<ScriptModel> _scripts = const [];
  bool _loadingScripts = true;
  String? _scriptsError;

  @override
  void initState() {
    super.initState();
    unawaited(_loadScripts());
  }

  @override
  void dispose() {
    _label.dispose();
    _parameter.dispose();
    for (final c in _paramCtrls.values) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _loadScripts() async {
    setState(() => _loadingScripts = true);
    try {
      final list = await widget.apiClient.listScripts();
      list.sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
      if (!mounted) return;
      setState(() {
        _scripts = list;
        _scriptsError = null;
        // Keep the stored name in sync when the script was renamed.
        final current = list.where((s) => s.id == _scriptId).firstOrNull;
        if (current != null) _scriptName = current.name;
      });
    } catch (e) {
      if (mounted) setState(() => _scriptsError = 'Could not load the scripts: $e');
    } finally {
      if (mounted) setState(() => _loadingScripts = false);
    }
  }

  Future<void> _openWorkspace() async {
    await widget.onOpenScriptWorkspace?.call();
    if (mounted) await _loadScripts();
  }

  /// Creates an empty script with the given name, assigns it to the button
  /// and opens the Script Workspace so its steps can be added.
  Future<void> _newScript() async {
    final name = await showDialog<String>(context: context, builder: (_) => const _ScriptNameDialog());
    if (name == null || name.trim().isEmpty || !mounted) return;
    try {
      final created = await widget.apiClient.createScript({
        'name': name.trim(),
        'context_table': widget.contextTable,
        'is_active': true,
        'steps': <Map<String, dynamic>>[],
      });
      if (!mounted) return;
      setState(() {
        _kind = 'perform_script';
        _scriptId = created.id;
        _scriptName = created.name;
      });
      await _loadScripts();
      if (widget.onOpenScriptWorkspace != null && mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(
          content: Text('Script "${created.name}" created. Add its steps in the Script Workspace.'),
        ));
        await _openWorkspace();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Could not create the script: $e'), backgroundColor: Colors.red),
        );
      }
    }
  }

  TextEditingController _paramCtrl(String key) =>
      _paramCtrls.putIfAbsent('$_stepType.$key', () => TextEditingController(text: _params[key]?.toString() ?? ''));

  ButtonActionModel? _buildAction() {
    switch (_kind) {
      case 'single_step':
        final params = Map<String, dynamic>.from(_params);
        for (final e in _paramCtrls.entries) {
          final parts = e.key.split('.');
          if (parts.first == _stepType) params[parts.last] = e.value.text;
        }
        return ButtonActionModel.singleStep(_stepType, params: params);
      case 'perform_script':
        return ButtonActionModel(
          type: 'perform_script',
          scriptId: _scriptId,
          scriptName: _scriptName,
          parameter: _parameter.text.trim().isEmpty ? null : _parameter.text.trim(),
        );
      default:
        return null;
    }
  }

  void _accept() {
    if (_kind == 'perform_script' && _scriptId == null) {
      ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Choose a script, or create a new one.')));
      return;
    }
    final action = _buildAction();
    Navigator.of(context).pop(action == null
        ? widget.button.copyWith(text: _label.text, clearAction: true)
        : widget.button.copyWith(text: _label.text, action: action));
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Row(
        children: [
          Icon(Icons.smart_button, color: Color(0xFF1E88E5)),
          SizedBox(width: 8),
          Text('Button Setup'),
        ],
      ),
      content: SizedBox(
        width: 440,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              TextField(
                controller: _label,
                decoration: const InputDecoration(labelText: 'Button label', border: OutlineInputBorder(), isDense: true),
              ),
              const SizedBox(height: 16),
              const Text('When clicked in Browse mode', style: TextStyle(fontWeight: FontWeight.w600, fontSize: 13)),
              RadioGroup<String>(
                groupValue: _kind,
                onChanged: (v) => setState(() => _kind = v ?? 'none'),
                child: const Column(
                  children: [
                    RadioListTile<String>(
                      value: 'none',
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                      title: Text('Do nothing'),
                    ),
                    RadioListTile<String>(
                      value: 'single_step',
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                      title: Text('Run a single step'),
                      subtitle: Text('New record, go to record, go to layout, ...'),
                    ),
                    RadioListTile<String>(
                      value: 'perform_script',
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                      title: Text('Perform a script'),
                      subtitle: Text('A script from the Script Workspace'),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 8),
              if (_kind == 'single_step') ..._singleStepFields(),
              if (_kind == 'perform_script') ..._scriptFields(),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(onPressed: _accept, child: const Text('OK')),
      ],
    );
  }

  List<Widget> _scriptFields() {
    return [
      if (_loadingScripts)
        const Padding(padding: EdgeInsets.symmetric(vertical: 12), child: LinearProgressIndicator())
      else
        DropdownButtonFormField<String>(
          key: ValueKey('script-${_scripts.length}-$_scriptId'),
          initialValue: _scripts.any((s) => s.id == _scriptId) ? _scriptId : null,
          isExpanded: true,
          decoration: InputDecoration(
            labelText: 'Script',
            border: const OutlineInputBorder(),
            isDense: true,
            helperText: _scriptsError ??
                (_scripts.isEmpty ? 'There are no scripts yet. Create one with "New script".' : null),
            helperMaxLines: 3,
          ),
          items: _scripts
              .map((s) => DropdownMenuItem(
                    value: s.id,
                    child: Text('${s.name}  (${s.steps.length} steps)', overflow: TextOverflow.ellipsis),
                  ))
              .toList(),
          onChanged: (id) {
            final s = _scripts.where((x) => x.id == id).firstOrNull;
            if (s != null) {
              setState(() {
                _scriptId = s.id;
                _scriptName = s.name;
              });
            }
          },
        ),
      const SizedBox(height: 10),
      TextField(
        controller: _parameter,
        decoration: const InputDecoration(
          labelText: 'Script parameter (optional)',
          border: OutlineInputBorder(),
          isDense: true,
        ),
      ),
      const SizedBox(height: 10),
      Wrap(
        spacing: 8,
        runSpacing: 8,
        children: [
          OutlinedButton.icon(
            icon: const Icon(Icons.add, size: 16),
            label: const Text('New script...'),
            onPressed: _newScript,
          ),
          OutlinedButton.icon(
            icon: const Icon(Icons.edit_outlined, size: 16),
            label: const Text('Edit scripts...'),
            onPressed: widget.onOpenScriptWorkspace == null ? null : _openWorkspace,
          ),
          TextButton.icon(
            icon: const Icon(Icons.refresh, size: 16),
            label: const Text('Reload'),
            onPressed: _loadScripts,
          ),
        ],
      ),
    ];
  }

  List<Widget> _singleStepFields() {
    Widget text(String key, String label) => Padding(
          padding: const EdgeInsets.only(top: 10),
          child: TextField(
            controller: _paramCtrl(key),
            decoration: InputDecoration(labelText: label, border: const OutlineInputBorder(), isDense: true),
          ),
        );

    Widget dropdown(String key, String label, Map<String, String> options) => Padding(
          padding: const EdgeInsets.only(top: 10),
          child: DropdownButtonFormField<String>(
            key: ValueKey('$_stepType-$key'),
            initialValue: options.containsKey(_params[key]?.toString()) ? _params[key].toString() : null,
            isExpanded: true,
            decoration: InputDecoration(labelText: label, border: const OutlineInputBorder(), isDense: true),
            items: options.entries.map((e) => DropdownMenuItem(value: e.key, child: Text(e.value))).toList(),
            onChanged: (v) => setState(() => _params[key] = v),
          ),
        );

    return [
      DropdownButtonFormField<String>(
        key: const ValueKey('step'),
        initialValue: kButtonActionSteps.containsKey(_stepType) ? _stepType : null,
        isExpanded: true,
        decoration: const InputDecoration(labelText: 'Step', border: OutlineInputBorder(), isDense: true),
        items: kButtonActionSteps.entries.map((e) => DropdownMenuItem(value: e.key, child: Text(e.value))).toList(),
        onChanged: (v) {
          if (v == null) return;
          setState(() {
            _stepType = v;
            _params = defaultButtonStepParams(v);
          });
        },
      ),
      ...switch (_stepType) {
        'go_to_record' => [
            dropdown('target', 'Record', const {'first': 'First', 'previous': 'Previous', 'next': 'Next', 'last': 'Last'}),
          ],
        'go_to_layout' => [
            dropdown('layout_name', 'Layout', {for (final n in widget.layoutNames) n: n}),
          ],
        'set_field' => [
            dropdown('field', 'Field', {
              for (final c in widget.columns.where((c) => !c.isPrimaryKey)) c.name: c.displayName,
            }),
            text('value', 'Value (text, {{field}} or {{CurrentDate}})'),
          ],
        'show_dialog' => [text('title', 'Title'), text('message', 'Message')],
        'open_url' => [text('url', 'URL')],
        _ => const <Widget>[],
      },
    ];
  }
}

/// Asks for the name of a new script. Owns its controller so it stays valid
/// during the closing animation.
class _ScriptNameDialog extends StatefulWidget {
  const _ScriptNameDialog();

  @override
  State<_ScriptNameDialog> createState() => _ScriptNameDialogState();
}

class _ScriptNameDialogState extends State<_ScriptNameDialog> {
  final _ctrl = TextEditingController();

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('New script'),
      content: TextField(
        controller: _ctrl,
        autofocus: true,
        decoration: const InputDecoration(labelText: 'Script name', hintText: 'New contact'),
        onSubmitted: (v) => Navigator.of(context).pop(v),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(onPressed: () => Navigator.of(context).pop(_ctrl.text), child: const Text('Create')),
      ],
    );
  }
}
