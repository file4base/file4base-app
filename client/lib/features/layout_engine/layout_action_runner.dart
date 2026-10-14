import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';

/// Steps a button can run directly (single step action), with their labels.
/// Parameters use the same names as the Script Workspace steps.
const kButtonActionSteps = <String, String>{
  'new_record': 'New Record',
  'duplicate_record': 'Duplicate Record',
  'delete_record': 'Delete Record',
  'commit_records': 'Commit (Save) Record',
  'revert_record': 'Revert Record',
  'go_to_record': 'Go to Record',
  'enter_browse_mode': 'Enter Browse Mode',
  'enter_find_mode': 'Enter Find Mode',
  'perform_find': 'Perform Find',
  'show_all_records': 'Show All Records',
  'enter_preview_mode': 'Enter Preview Mode',
  'go_to_layout': 'Go to Layout',
  'set_field': 'Set Field',
  'show_dialog': 'Show Custom Dialog',
  'open_url': 'Open URL',
  'adjust_window': 'Adjust Window',
  're_login': 'Re-Login (Switch User)',
  'close_file': 'Close File',
  'exit_application': 'Exit Application',
};

/// Default parameters of a single step action when it is chosen.
Map<String, dynamic> defaultButtonStepParams(String stepType) => switch (stepType) {
      'go_to_record' => {'target': 'next'},
      'show_dialog' => {'title': 'Message', 'message': ''},
      'set_field' => {'field': '', 'value': ''},
      'go_to_layout' => {'layout_name': ''},
      'open_url' => {'url': 'https://'},
      'adjust_window' => {'window_action': 'Restore'},
      _ => <String, dynamic>{},
    };

/// One-line description of a button action, e.g. "Perform Script: Archive".
String describeButtonAction(ButtonActionModel a) {
  if (a.isPerformScript) {
    final p = a.parameter?.isNotEmpty == true ? ' ("${a.parameter}")' : '';
    return 'Perform Script: ${a.scriptName ?? a.scriptId ?? '(no script selected)'}$p';
  }
  final label = kButtonActionSteps[a.stepType] ?? a.stepType ?? '?';
  final detail = switch (a.stepType) {
    'go_to_record' => ' [${a.params['target'] ?? 'next'}]',
    'go_to_layout' => ' [${a.params['layout_name'] ?? ''}]',
    'set_field' => ' [${a.params['field'] ?? ''}]',
    'open_url' => ' [${a.params['url'] ?? ''}]',
    'adjust_window' => ' [${a.params['window_action'] ?? 'Restore'}]',
    _ => '',
  };
  return '$label$detail';
}

/// Operations a layout button can trigger in Browse mode. Implemented by the
/// data browser, which owns the current record and the found set.
abstract class LayoutActionHost {
  Future<void> actionNewRecord();
  Future<void> actionDuplicateRecord();
  Future<void> actionDeleteRecord({required bool confirm});
  Future<void> actionCommitRecord();
  Future<void> actionRevertRecord();

  /// [target] is `first`, `previous`, `next`, `last` (English or Spanish
  /// names, as written by the Script Workspace) or a 1-based record number.
  Future<void> actionGoToRecord(String target);
  Future<void> actionEnterFindMode();
  Future<void> actionPerformFind();
  Future<void> actionShowAllRecords();
  Future<void> actionEnterPreviewMode();
  Future<void> actionEnterBrowseMode() async {}

  /// Returns false when no layout has that name.
  Future<bool> actionGoToLayout(String layoutName);

  /// Returns false when the current table has no such field.
  Future<bool> actionSetField(String field, String value);
  Future<void> actionShowDialog(String title, String message);
  Future<void> actionOpenUrl(String url);

  /// Window & Kiosk session lifecycle actions.
  Future<void> actionAdjustWindow(String windowAction) async {}
  Future<void> actionReLogin() async {}
  Future<void> actionCloseFile() async {}
  Future<void> actionExitApplication() async {}

  /// Resolves `{{field}}`, `{{CurrentDate}}`, ... in a step parameter.
  String actionResolveText(String text);
}

/// Outcome of running a button action. [message] explains why a script
/// stopped early (unsupported step, missing script, ...).
class LayoutActionResult {

  const LayoutActionResult.ok()
      : completed = true,
        message = null;

  const LayoutActionResult.stopped(this.message) : completed = false;
  final bool completed;
  final String? message;
}

/// Runs button actions and stored scripts on the client.
///
/// Scripts run step by step in order, supporting record, navigation, field,
/// dialog, variable (`Set Variable`), and conditional (`If` / `Else If` /
/// `Else` / `End If`) steps.
class LayoutActionRunner {

  LayoutActionRunner({required this.apiClient, required this.host});
  final ApiClient apiClient;
  final LayoutActionHost host;

  static const maxScriptDepth = 8;

  /// Session-scoped `$$global` script variables (including `$$lastLayout`).
  static final Map<String, String> globalVariables = <String, String>{};

  /// Normalizes both snake_case (`go_to_layout`) and FileMaker Title Case
  /// (`Go to Layout`, `Delete Portal Row`) step names into canonical keys.
  static String normalizeStepType(String raw) {
    var s = raw.trim();
    final bracketIdx = s.indexOf('[');
    if (bracketIdx > 0) {
      s = s.substring(0, bracketIdx).trim();
    }
    s = s.toLowerCase().replaceAll(RegExp(r'[\s/-]+'), '_');
    return switch (s) {
      'new_record_request' => 'new_record',
      'duplicate_record_request' => 'duplicate_record',
      'delete_record_request' => 'delete_record',
      'commit_records_requests' => 'commit_records',
      'revert_record_request' => 'revert_record',
      'go_to_record_request_page' => 'go_to_record',
      'show_custom_dialog' => 'show_dialog',
      'pause_resume_script' => 'pause_script',
      'move_resize_window' => 'adjust_window',
      'relogin' => 're_login',
      _ => s,
    };
  }

  Future<LayoutActionResult> run(ButtonActionModel action) async {
    if (action.isPerformScript) {
      if (action.scriptId == null && (action.scriptName == null || action.scriptName!.isEmpty)) {
        return const LayoutActionResult.stopped('This button has no script selected.');
      }
      return _performScript(id: action.scriptId, name: action.scriptName, depth: 0);
    }
    final step = action.stepType;
    if (step == null) return const LayoutActionResult.stopped('This button has no step selected.');
    return runStep(step, action.params, depth: 0);
  }

  Future<LayoutActionResult> _performScript({required int depth, String? id, String? name}) async {
    if (depth >= maxScriptDepth) {
      return const LayoutActionResult.stopped('Perform Script is nested too deeply (possible recursion).');
    }
    ScriptModel? script;
    try {
      if (id != null && id.isNotEmpty) {
        script = await apiClient.getScript(id);
      } else {
        final all = await apiClient.listScripts();
        script = all.where((s) => s.name.toLowerCase() == name!.toLowerCase()).firstOrNull;
        if (script != null) script = await apiClient.getScript(script.id);
      }
    } catch (e) {
      return LayoutActionResult.stopped('Could not load script "${name ?? id}": $e');
    }
    if (script == null) return LayoutActionResult.stopped('Script "${name ?? id}" not found.');

    final localVars = <String, String>{};
    final ifStack = <_IfFrame>[];

    bool isExecuting() => ifStack.every((f) => f.active);

    final steps = List<ScriptStepModel>.from(script.steps)..sort((a, b) => a.sequenceIdx.compareTo(b.sequenceIdx));
    final hasEndIf = steps.any((s) => s.isEnabled && normalizeStepType(s.stepType) == 'end_if');
    for (final step in steps) {
      if (!step.isEnabled) continue;
      final norm = normalizeStepType(step.stepType);

      if (norm == 'if' && hasEndIf) {
        final parentActive = isExecuting();
        final cond = parentActive && _evaluateCondition(_extractCondition(step.params), localVars);
        ifStack.add(_IfFrame(parentActive: parentActive, branchTaken: cond, active: cond));
        continue;
      }
      if (norm == 'else_if' && hasEndIf) {
        if (ifStack.isNotEmpty) {
          final top = ifStack.last;
          if (!top.parentActive || top.branchTaken) {
            top.active = false;
          } else {
            final cond = _evaluateCondition(_extractCondition(step.params), localVars);
            top.branchTaken = cond;
            top.active = cond;
          }
        }
        continue;
      }
      if (norm == 'else' && hasEndIf) {
        if (ifStack.isNotEmpty) {
          final top = ifStack.last;
          if (!top.parentActive || top.branchTaken) {
            top.active = false;
          } else {
            top.branchTaken = true;
            top.active = true;
          }
        }
        continue;
      }
      if (norm == 'end_if' && hasEndIf) {
        if (ifStack.isNotEmpty) {
          ifStack.removeLast();
        }
        continue;
      }

      if (!isExecuting()) continue;

      if (norm == 'halt_script' || norm == 'exit_script') break;
      final result = await runStep(step.stepType, step.params, depth: depth, localVars: localVars);
      if (!result.completed) {
        return LayoutActionResult.stopped('Script "${script.name}" stopped at step ${step.sequenceIdx}: ${result.message}');
      }
    }
    return const LayoutActionResult.ok();
  }

  Future<LayoutActionResult> runStep(
    String stepType,
    Map<String, dynamic> params, {
    required int depth,
    Map<String, String>? localVars,
  }) async {
    final vars = localVars ?? <String, String>{};
    String p(String key, [String fallback = '']) =>
        _resolveParamsText(params[key]?.toString() ?? fallback, vars);

    final norm = normalizeStepType(stepType);
    switch (norm) {
      case 'comment':
        return const LayoutActionResult.ok();
      case 'if':
      case 'else_if':
      case 'else':
      case 'end_if':
        return LayoutActionResult.stopped('Step "$stepType" is not supported on a button.');
      case 'set_variable':
        if (!params.containsKey('value') && !params.containsKey('formula') && !params.containsKey('text') && !params.containsKey('calc')) {
          return LayoutActionResult.stopped('Step "$stepType" is not supported without a value.');
        }
        _execSetVariable(params, vars);
        return const LayoutActionResult.ok();
      case 'new_record':
        await host.actionNewRecord();
      case 'duplicate_record':
        await host.actionDuplicateRecord();
      case 'delete_record':
      case 'delete_portal_row':
        await host.actionDeleteRecord(confirm: params['dialog'] != false);
      case 'commit_records':
        await host.actionCommitRecord();
      case 'revert_record':
        await host.actionRevertRecord();
      case 'go_to_record':
        await host.actionGoToRecord(p('target', 'next'));
      case 'enter_browse_mode':
        await host.actionEnterBrowseMode();
      case 'enter_find_mode':
        await host.actionEnterFindMode();
      case 'perform_find':
        await host.actionPerformFind();
      case 'show_all_records':
        await host.actionShowAllRecords();
      case 'enter_preview_mode':
        await host.actionEnterPreviewMode();
      case 'adjust_window':
        await host.actionAdjustWindow(p('window_action', 'Restore'));
      case 're_login':
        await host.actionReLogin();
      case 'close_file':
        await host.actionCloseFile();
      case 'exit_application':
        await host.actionExitApplication();
      case 'go_to_layout':
        var rawTarget = params['layout_name']?.toString().trim() ?? '';
        if (rawTarget.isEmpty) {
          rawTarget = _extractLayoutFromExpression(params['expression']?.toString() ?? '');
        }
        var layout = _resolveParamsText(rawTarget, vars).trim();
        if (layout.isEmpty && rawTarget.startsWith(r'$')) {
          layout = _lookupVariable(rawTarget, vars) ?? 'View Recipe';
        }
        if (layout.isEmpty) return const LayoutActionResult.stopped('Go to Layout has no layout selected.');
        final currentLayout = host.actionResolveText('{{LayoutName}}');
        if (currentLayout.isNotEmpty && currentLayout != '{{LayoutName}}') {
          _setVariable(r'$$lastLayout', currentLayout, vars);
        }
        if (!await host.actionGoToLayout(layout)) {
          return LayoutActionResult.stopped('Layout "$layout" not found.');
        }
      case 'set_field':
        final field = params['field']?.toString() ?? '';
        if (field.isEmpty) return const LayoutActionResult.stopped('Set Field has no field selected.');
        if (!await host.actionSetField(field, p('value'))) {
          return LayoutActionResult.stopped('Field "$field" is not in this table.');
        }
      case 'show_dialog':
        await host.actionShowDialog(p('title', 'Message'), p('message'));
      case 'open_url':
        var rawUrl = params['url']?.toString() ?? '';
        if (rawUrl.isEmpty) {
          rawUrl = params['expression']?.toString() ?? '';
        }
        final url = _resolveExpressionValue(rawUrl, vars);
        if (url.isEmpty) return const LayoutActionResult.stopped('Open URL has no URL.');
        await host.actionOpenUrl(url);
      case 'pause_script':
        final seconds = num.tryParse(params['duration_seconds']?.toString() ?? '') ?? 0;
        if (seconds > 0) await Future<void>.delayed(Duration(milliseconds: (seconds * 1000).round()));
      case 'perform_script':
        var name = params['script_name']?.toString() ?? '';
        if (name.isEmpty) {
          name = _extractQuoted(params['expression']?.toString() ?? '');
        }
        if (name.isEmpty) return const LayoutActionResult.stopped('Perform Script has no script name.');
        return _performScript(name: name, depth: depth + 1);
      default:
        return LayoutActionResult.stopped(
            'step "$stepType" is not supported by the client script runner yet '
            '(it needs the calculation engine / server script runner).');
    }
    return const LayoutActionResult.ok();
  }

  String _extractCondition(Map<String, dynamic> params) {
    final c = params['condition']?.toString().trim() ?? '';
    if (c.isNotEmpty) return c;
    return params['expression']?.toString().trim() ?? '';
  }

  void _execSetVariable(Map<String, dynamic> params, Map<String, String> localVars) {
    var name = params['name']?.toString().trim() ?? '';
    var valExpr = params['value']?.toString().trim() ?? '';
    if (name.isEmpty) {
      final expr = params['expression']?.toString() ?? '';
      final parts = expr.split(';');
      if (parts.isNotEmpty) {
        name = parts.first.trim();
      }
      if (parts.length > 1) {
        var rest = parts.sublist(1).join(';').trim();
        if (rest.toLowerCase().startsWith('value:')) {
          rest = rest.substring('value:'.length).trim();
        }
        valExpr = rest;
      }
    }
    if (name.isEmpty) return;
    final resolved = _resolveExpressionValue(valExpr, localVars);
    _setVariable(name, resolved, localVars);
  }

  void _setVariable(String name, String value, Map<String, String> localVars) {
    final key = name.trim().toLowerCase();
    if (key.startsWith(r'$$')) {
      globalVariables[key] = value;
    } else {
      localVars[key] = value;
    }
  }

  String? _lookupVariable(String name, Map<String, String> localVars) {
    final key = name.trim().toLowerCase();
    if (key.startsWith(r'$$')) {
      return globalVariables[key];
    }
    return localVars[key] ?? globalVariables['\$$key'];
  }

  String _resolveParamsText(String raw, Map<String, String> localVars) {
    final trimmed = raw.trim();
    if (trimmed.startsWith(r'$')) {
      return _lookupVariable(trimmed, localVars) ?? '';
    }
    return host.actionResolveText(raw);
  }

  String _resolveExpressionValue(String raw, Map<String, String> localVars) {
    final expr = raw.trim();
    if (expr.isEmpty) return '';
    if ((expr.startsWith('"') && expr.endsWith('"')) ||
        (expr.startsWith("'") && expr.endsWith("'"))) {
      return host.actionResolveText(expr.substring(1, expr.length - 1));
    }
    if (RegExp(r'^get\s*\(\s*layoutname\s*\)$', caseSensitive: false).hasMatch(expr)) {
      final ln = host.actionResolveText('{{LayoutName}}');
      return ln == '{{LayoutName}}' ? '' : ln;
    }
    if (expr.startsWith(r'$')) {
      return _lookupVariable(expr, localVars) ?? '';
    }
    if (expr.contains('{{')) {
      return host.actionResolveText(expr);
    }
    // Qualified or unqualified field reference (e.g. `recipes::URL` or `URL`)
    final fieldPart = expr.contains('::') ? expr.split('::').last.trim() : expr;
    final snakeField = fieldPart.toLowerCase().replaceAll(RegExp('[^a-z0-9]+'), '_');
    final mergeToken = '{{$snakeField}}';
    final resolved = host.actionResolveText(mergeToken);
    if (resolved == mergeToken) {
      return '';
    }
    return resolved;
  }

  bool _evaluateCondition(String rawExpr, Map<String, String> localVars) {
    var expr = rawExpr.trim();
    if (expr.isEmpty) return true;
    while (expr.startsWith('(') && expr.endsWith(')')) {
      expr = expr.substring(1, expr.length - 1).trim();
    }
    final orParts = expr.split(RegExp(r'\s+or\s+', caseSensitive: false));
    if (orParts.length > 1) {
      return orParts.any((p) => _evaluateCondition(p, localVars));
    }
    final andParts = expr.split(RegExp(r'\s+and\s+', caseSensitive: false));
    if (andParts.length > 1) {
      return andParts.every((p) => _evaluateCondition(p, localVars));
    }
    if (expr.toLowerCase().startsWith('not ')) {
      return !_evaluateCondition(expr.substring(4), localVars);
    }
    if (expr.startsWith('!')) {
      return !_evaluateCondition(expr.substring(1), localVars);
    }
    final isEmptyMatch = RegExp(r'^isempty\s*\(\s*(.+)\s*\)$', caseSensitive: false).firstMatch(expr);
    if (isEmptyMatch != null) {
      final inner = isEmptyMatch.group(1)!;
      final val = _resolveExpressionValue(inner, localVars);
      return val.trim().isEmpty;
    }
    if (expr.contains('!=')) {
      final parts = expr.split('!=');
      return _resolveExpressionValue(parts[0], localVars) !=
          _resolveExpressionValue(parts.sublist(1).join('!='), localVars);
    }
    if (expr.contains('≠')) {
      final parts = expr.split('≠');
      return _resolveExpressionValue(parts[0], localVars) !=
          _resolveExpressionValue(parts.sublist(1).join('≠'), localVars);
    }
    if (expr.contains('<>')) {
      final parts = expr.split('<>');
      return _resolveExpressionValue(parts[0], localVars) !=
          _resolveExpressionValue(parts.sublist(1).join('<>'), localVars);
    }
    if (expr.contains('=')) {
      final parts = expr.split('=');
      return _resolveExpressionValue(parts[0], localVars) ==
          _resolveExpressionValue(parts.sublist(1).join('='), localVars);
    }
    final val = _resolveExpressionValue(expr, localVars).trim();
    return val.isNotEmpty && val != '0' && val.toLowerCase() != 'false';
  }

  String _extractLayoutFromExpression(String expr) {
    final q = _extractQuoted(expr);
    if (q.isNotEmpty) return q;
    return expr.trim();
  }

  String _extractQuoted(String expr) {
    final q1 = expr.indexOf('"');
    if (q1 >= 0) {
      final q2 = expr.indexOf('"', q1 + 1);
      if (q2 > q1) {
        return expr.substring(q1 + 1, q2);
      }
    }
    return '';
  }
}

class _IfFrame {

  _IfFrame({
    required this.parentActive,
    required this.branchTaken,
    required this.active,
  });
  final bool parentActive;
  bool branchTaken;
  bool active;
}
