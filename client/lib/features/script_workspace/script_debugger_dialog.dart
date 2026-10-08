// #51 — Tools > Script Debugger. Runs a script one step at a time against
// the records on screen, marks the step about to run, and says where it
// stopped.

import 'package:flutter/material.dart';

import '../../core/api/api_client.dart';
import '../../core/models/script_models.dart';
import '../layout_engine/layout_action_runner.dart';
import 'script_debugger.dart';

class ScriptDebuggerDialog extends StatefulWidget {
  final ApiClient apiClient;

  /// What the steps act on: the data browser, the same host the layout
  /// buttons run against, so debugging a script does what running it does.
  final LayoutActionHost? host;

  const ScriptDebuggerDialog({super.key, required this.apiClient, required this.host});

  static Future<void> show(
    BuildContext context, {
    required ApiClient apiClient,
    required LayoutActionHost? host,
  }) {
    return showDialog<void>(
      context: context,
      builder: (_) => ScriptDebuggerDialog(apiClient: apiClient, host: host),
    );
  }

  @override
  State<ScriptDebuggerDialog> createState() => _ScriptDebuggerDialogState();
}

class _ScriptDebuggerDialogState extends State<ScriptDebuggerDialog> {
  List<ScriptModel> _scripts = const [];
  ScriptModel? _chosen;
  ScriptDebugSession? _session;
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadScripts();
  }

  Future<void> _loadScripts() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final scripts = await widget.apiClient.listScripts();
      if (!mounted) return;
      setState(() {
        _scripts = scripts;
        _loading = false;
      });
      if (scripts.isNotEmpty) await _choose(scripts.first.id);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  /// Loads the script with its steps and puts the session at the top.
  Future<void> _choose(String id) async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final script = await widget.apiClient.getScript(id);
      if (!mounted) return;
      final host = widget.host;
      setState(() {
        _chosen = script;
        _session = host == null
            ? null
            : (ScriptDebugSession(
                script: script,
                runner: LayoutActionRunner(apiClient: widget.apiClient, host: host),
              )..begin());
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  Future<void> _step() async {
    final session = _session;
    if (session == null) return;
    await session.step();
    if (mounted) setState(() {});
  }

  Future<void> _continue() async {
    final session = _session;
    if (session == null) return;
    await session.continueRun();
    if (mounted) setState(() {});
  }

  @override
  Widget build(BuildContext context) {
    final session = _session;
    return AlertDialog(
      title: Row(
        children: [
          const Icon(Icons.bug_report_outlined, color: Color(0xFF1E88E5)),
          const SizedBox(width: 8),
          const Text('Script Debugger', style: TextStyle(fontSize: 16, fontWeight: FontWeight.bold)),
          const Spacer(),
          if (_scripts.isNotEmpty)
            SizedBox(
              width: 260,
              child: DropdownButtonFormField<String>(
                key: const ValueKey('debug-script'),
                initialValue: _chosen?.id,
                isExpanded: true,
                decoration: const InputDecoration(border: OutlineInputBorder(), isDense: true),
                items: [
                  for (final script in _scripts)
                    DropdownMenuItem(
                      value: script.id,
                      child: Text(script.name, style: const TextStyle(fontSize: 12)),
                    ),
                ],
                onChanged: session?.running == true ? null : (id) => id == null ? null : _choose(id),
              ),
            ),
        ],
      ),
      content: SizedBox(
        width: 760,
        height: 480,
        child: _body(session),
      ),
      actions: _actions(session),
    );
  }

  Widget _body(ScriptDebugSession? session) {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error != null) {
      return Center(
        child: Text(_error!, key: const ValueKey('debug-error'),
            style: TextStyle(fontSize: 12.5, color: Colors.red.shade700)),
      );
    }
    if (_scripts.isEmpty) {
      return const Center(
        child: Text('This solution has no scripts yet.',
            style: TextStyle(fontSize: 13, color: Colors.grey)),
      );
    }
    if (widget.host == null || session == null) {
      return const Center(
        child: Padding(
          padding: EdgeInsets.symmetric(horizontal: 40),
          child: Text(
            'A script runs against the records on screen, so open a layout in Browse mode '
            'and start the debugger from there.',
            textAlign: TextAlign.center,
            style: TextStyle(fontSize: 13, color: Colors.grey),
          ),
        ),
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _statusLine(session),
        const SizedBox(height: 8),
        Expanded(
          child: session.steps.isEmpty
              ? const Center(
                  child: Text('This script has no steps.',
                      style: TextStyle(fontSize: 13, color: Colors.grey)),
                )
              : ListView.builder(
                  key: const ValueKey('debug-steps'),
                  itemCount: session.steps.length,
                  itemBuilder: (context, index) => _stepRow(session, session.steps[index]),
                ),
        ),
        const SizedBox(height: 6),
        Text(
          'Steps run against the records on screen, exactly as a layout button runs them. '
          'A step the runner does not take yet stops the script and says so; Set Variable, '
          'If and Loop are among those, which is why there is nothing to watch in the Data Viewer.',
          style: TextStyle(fontSize: 10.5, color: Colors.grey.shade600),
        ),
      ],
    );
  }

  Widget _statusLine(ScriptDebugSession session) {
    final message = session.message;
    final colour = session.steps.any((s) => s.state == DebugStepState.failed)
        ? Colors.red.shade700
        : (session.finished ? Colors.green.shade700 : Colors.blue.shade700);
    return Container(
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: colour.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(6),
        border: Border.all(color: colour.withValues(alpha: 0.4)),
      ),
      child: Row(
        children: [
          Icon(
            session.finished ? Icons.flag_outlined : Icons.play_circle_outline,
            size: 16,
            color: colour,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message ??
                  (session.currentStep == null
                      ? 'Nothing to run.'
                      : 'About to run step ${session.currentStep!.sequence}: ${session.currentStep!.type}.'),
              key: const ValueKey('debug-status'),
              style: TextStyle(fontSize: 12, color: colour),
            ),
          ),
        ],
      ),
    );
  }

  Widget _stepRow(ScriptDebugSession session, DebugStep step) {
    final (icon, colour) = switch (step.state) {
      DebugStepState.current => (Icons.play_arrow, Colors.blue.shade700),
      DebugStepState.done => (Icons.check, Colors.green.shade700),
      DebugStepState.skipped => (Icons.remove, Colors.grey),
      DebugStepState.failed => (Icons.error_outline, Colors.red.shade700),
      DebugStepState.pending => (Icons.circle_outlined, Colors.grey.shade400),
    };

    return InkWell(
      onTap: session.running ? null : () => setState(() => session.toggleBreakpoint(step.sequence)),
      child: Container(
        padding: const EdgeInsets.symmetric(vertical: 5, horizontal: 4),
        color: step.state == DebugStepState.current ? Colors.blue.withValues(alpha: 0.06) : null,
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SizedBox(
              width: 24,
              child: Tooltip(
                message: step.breakpoint ? 'Stop here' : 'Click to stop here',
                child: Icon(
                  step.breakpoint ? Icons.circle : Icons.circle_outlined,
                  size: 11,
                  color: step.breakpoint ? Colors.red.shade600 : Colors.grey.shade300,
                ),
              ),
            ),
            SizedBox(width: 22, child: Icon(icon, size: 14, color: colour)),
            SizedBox(
              width: 28,
              child: Text('${step.sequence}', style: const TextStyle(fontSize: 11.5, color: Colors.grey)),
            ),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    step.type,
                    style: TextStyle(
                      fontSize: 12.5,
                      fontFamily: 'monospace',
                      decoration: step.enabled ? null : TextDecoration.lineThrough,
                      color: step.enabled ? null : Colors.grey,
                      fontWeight:
                          step.state == DebugStepState.current ? FontWeight.bold : FontWeight.normal,
                    ),
                  ),
                  if (step.summary.isNotEmpty)
                    Text(step.summary,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(fontSize: 10.5, color: Colors.grey)),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  List<Widget> _actions(ScriptDebugSession? session) {
    if (session == null) {
      return [FilledButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Close'))];
    }
    final busy = session.running;
    return [
      TextButton(
        key: const ValueKey('debug-restart'),
        onPressed: busy || !session.started ? null : () => setState(session.restart),
        child: const Text('Start over'),
      ),
      TextButton(
        key: const ValueKey('debug-stop'),
        onPressed: busy || session.finished ? null : () => setState(session.stop),
        child: const Text('Stop'),
      ),
      OutlinedButton.icon(
        key: const ValueKey('debug-step'),
        icon: const Icon(Icons.skip_next, size: 16),
        label: const Text('Step'),
        onPressed: busy || session.finished ? null : _step,
      ),
      FilledButton.icon(
        key: const ValueKey('debug-continue'),
        icon: const Icon(Icons.play_arrow, size: 16),
        label: const Text('Continue'),
        onPressed: busy || session.finished ? null : _continue,
      ),
      TextButton(onPressed: busy ? null : () => Navigator.of(context).pop(), child: const Text('Close')),
    ];
  }
}
