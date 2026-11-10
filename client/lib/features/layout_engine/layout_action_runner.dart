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
        halted = false,
        message = null;

  /// Halt Script: this script and every script that called it stop, which is
  /// what Claris defines the step as doing. It is not a failure, so
  /// [completed] stays true, but no later step of any caller may run (#95).
  const LayoutActionResult.haltedAll()
      : completed = true,
        halted = true,
        message = null;

  const LayoutActionResult.stopped(this.message)
      : completed = false,
        halted = false;
  final bool completed;

  /// True when a Halt Script step ended the whole execution session.
  final bool halted;
  final String? message;
}

/// What happened to one step of a script.
enum StepOutcomeKind {
  /// Ran, and the host was happy.
  ran,

  /// Not run: the step is disabled, or it sits in a branch that was not
  /// taken.
  skipped,

  /// A branch or loop step: it moved the execution state rather than doing
  /// anything to the records.
  controlFlow,

  /// Exit Script: this script returns to its caller, which carries on.
  exited,

  /// Halt Script: this script and every caller stop.
  halted,

  /// The step could not run; [StepOutcome.message] says why.
  stopped,
}

/// The result of advancing one step of a [ScriptExecution].
class StepOutcome {
  const StepOutcome(this.kind, [this.message]);
  final StepOutcomeKind kind;
  final String? message;
}

/// Runs button actions and stored scripts on the client.
///
/// Scripts run step by step in order, supporting record, navigation, field,
/// dialog, variable (`Set Variable`), and conditional (`If` / `Else If` /
/// `Else` / `End If`) steps.
class LayoutActionRunner {

  LayoutActionRunner({
    required this.apiClient,
    required this.host,
    this.peerTokenProvider,
  });
  final ApiClient apiClient;
  final LayoutActionHost host;

  /// Supplies a session token for another solution, so an `Import Records` or
  /// `Export Records` step whose transfer reaches a second solution can be
  /// authorized (#96).
  ///
  /// It is asked for the solution's database name and answers a token, or
  /// null when the user has not signed in to that solution. A null provider,
  /// or a null answer, stops the script: a transfer is never run on
  /// authority this session does not have, and never quietly skipped.
  final Future<String?> Function(String solution)? peerTokenProvider;

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

    final execution = ScriptExecution(script: script, runner: this, depth: depth);
    while (!execution.atEnd) {
      final sequence = execution.currentSequence;
      final outcome = await execution.advance();
      switch (outcome.kind) {
        case StepOutcomeKind.stopped:
          return LayoutActionResult.stopped(
              'Script "${script.name}" stopped at step $sequence: ${outcome.message}');
        case StepOutcomeKind.halted:
          // Every caller stops too, so the halt travels up instead of
          // looking like an ordinary return (#95).
          return const LayoutActionResult.haltedAll();
        case StepOutcomeKind.exited:
          return const LayoutActionResult.ok();
        case StepOutcomeKind.ran:
        case StepOutcomeKind.skipped:
        case StepOutcomeKind.controlFlow:
          break;
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
      case 'loop':
      case 'exit_loop_if':
      case 'end_loop':
        // Control flow belongs to a script, which has the steps around it to
        // branch over or repeat. On its own, on a button, it means nothing.
        return LayoutActionResult.stopped('Step "$stepType" is not supported on a button.');
      case 'import_records':
      case 'export_records':
        return _execTransfer(norm, params, vars);
      case 'set_variable':
        if (!params.containsKey('value') && !params.containsKey('formula') && !params.containsKey('text') && !params.containsKey('calc')) {
          return LayoutActionResult.stopped('Step "$stepType" is not supported without a value.');
        }
        return _execSetVariable(params, vars);
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

  /// Names of the step parameters that can hold the transfer an
  /// `Import Records` / `Export Records` step runs.
  static const _transferNameKeys = <String>['transfer', 'transfer_name', 'name'];

  /// Runs the transfer definition an `Import Records` or `Export Records`
  /// step names (#96).
  ///
  /// The transfer is resolved by name against this solution, and an
  /// unresolved one stops the script. It is never answered with something
  /// else that happens to share the name — in particular never with a local
  /// script, which is the fallback the issue asks us not to have.
  ///
  /// The counts of the run are left in `$$transferAdded`,
  /// `$$transferUpdated`, `$$transferSkipped`, `$$transferRejected` and
  /// `$$transferCommitted`, so the steps after it can test what happened with
  /// an ordinary `If`.
  Future<LayoutActionResult> _execTransfer(
    String norm,
    Map<String, dynamic> params,
    Map<String, String> localVars,
  ) async {
    final label = norm == 'export_records' ? 'Export Records' : 'Import Records';

    var name = '';
    for (final key in _transferNameKeys) {
      final raw = params[key]?.toString().trim() ?? '';
      if (raw.isNotEmpty) {
        name = _resolveParamsText(raw, localVars).trim();
        if (name.isEmpty && raw.startsWith(r'$')) {
          return LayoutActionResult.stopped(
              '$label names the transfer in $raw, which holds nothing here.');
        }
        break;
      }
    }
    if (name.isEmpty) {
      name = _extractQuoted(params['expression']?.toString() ?? '').trim();
    }
    if (name.isEmpty) {
      return LayoutActionResult.stopped(
          '$label has no transfer named in it. A transfer definition says which two '
          'solutions the records move between, which fields map to which, and which '
          'keys records are matched on; a step converted from FileMaker carries none '
          'of that, so one has to be created and named here.');
    }

    TransferDefinitionModel definition;
    try {
      definition = await apiClient.getTransfer(name);
    } on ApiException catch (e) {
      if (e.statusCode == 404) {
        return LayoutActionResult.stopped(
            '$label: there is no transfer named "$name" in this solution.');
      }
      return LayoutActionResult.stopped(
          '$label: the transfer "$name" could not be read: ${e.detail}');
    } catch (e) {
      return LayoutActionResult.stopped(
          '$label: the transfer "$name" could not be read: $e');
    }

    // The two steps are the two directions of the same thing, and a step
    // pointed the wrong way is a mistake worth naming rather than running
    // backwards.
    if (norm == 'import_records' && !definition.destination.isLocal) {
      return LayoutActionResult.stopped(
          'Import Records brings records into this solution, but the transfer "$name" '
          'sends them to ${definition.destination.label}. Use Export Records, or a '
          'transfer whose destination is this solution.');
    }
    if (norm == 'export_records' && !definition.source.isLocal) {
      return LayoutActionResult.stopped(
          'Export Records sends records out of this solution, but the transfer "$name" '
          'reads them from ${definition.source.label}. Use Import Records, or a '
          'transfer whose source is this solution.');
    }

    String? peerToken;
    final peer = definition.peerDatabase;
    if (peer.isNotEmpty) {
      peerToken = await peerTokenProvider?.call(peer);
      if (peerToken == null || peerToken.isEmpty) {
        return LayoutActionResult.stopped(
            '$label: the transfer "$name" reaches the solution $peer, and this session '
            'is not signed in to it. Sign in to $peer and run the script again; '
            'File4Base never moves records on authority it has not been given.');
      }
    }

    TransferRunReport report;
    try {
      report = await apiClient.runTransfer(definition.id, peerToken: peerToken);
    } on ApiException catch (e) {
      return LayoutActionResult.stopped('$label "$name" could not run: ${e.detail}');
    } catch (e) {
      return LayoutActionResult.stopped('$label "$name" could not run: $e');
    }

    _setVariable(r'$$transferAdded', report.added.toString(), localVars);
    _setVariable(r'$$transferUpdated', report.updated.toString(), localVars);
    _setVariable(r'$$transferSkipped', report.skipped.toString(), localVars);
    _setVariable(r'$$transferRejected', report.rejectedCount.toString(), localVars);
    _setVariable(r'$$transferCommitted', report.committed ? '1' : '0', localVars);

    if (!report.committed) {
      // Nothing was written, so the script stops here rather than carrying on
      // as though the records had moved.
      return LayoutActionResult.stopped('$label "$name" was refused: ${report.describeRefusal()}');
    }
    return const LayoutActionResult.ok();
  }

  /// The condition of an `If` / `Else If` / `Exit Loop If` step.
  String extractCondition(Map<String, dynamic> params) {
    final c = params['condition']?.toString().trim() ?? '';
    if (c.isNotEmpty) return c;
    return params['expression']?.toString().trim() ?? '';
  }

  LayoutActionResult _execSetVariable(Map<String, dynamic> params, Map<String, String> localVars) {
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
    if (name.isEmpty) {
      return const LayoutActionResult.stopped('Set Variable has no variable name.');
    }
    final resolved = resolveValue(valExpr, localVars);
    if (resolved == null) {
      // Guessing an unreadable value as empty wrote an empty variable and let
      // the script carry on as though it had a value (#93).
      return LayoutActionResult.stopped(
          'Set Variable cannot evaluate "$valExpr": File4Base reads a number, '
          r'a quoted string, a $variable, a merge token or a field name.');
    }
    _setVariable(name, resolved, localVars);
    return const LayoutActionResult.ok();
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

  String _resolveExpressionValue(String raw, Map<String, String> localVars) =>
      resolveValue(raw, localVars) ?? '';

  /// The value of one expression, or null when it is not an expression this
  /// runner can evaluate.
  ///
  /// It reads a number as the number it is, a quoted string, a `$variable`, a
  /// `{{merge}}` token, `Get(LayoutName)`, and a qualified or unqualified
  /// field reference. Anything else — arithmetic, a function call, a field
  /// this layout does not have — is null, so the caller stops the script
  /// rather than treating it as an empty field: an empty string made
  /// `1 = 0` true and wrote empty variables (#93).
  String? resolveValue(String raw, Map<String, String> localVars) {
    final expr = raw.trim();
    if (expr.isEmpty) return '';
    if ((expr.startsWith('"') && expr.endsWith('"') && expr.length >= 2) ||
        (expr.startsWith("'") && expr.endsWith("'") && expr.length >= 2)) {
      return host.actionResolveText(expr.substring(1, expr.length - 1));
    }
    // A numeric literal is its own value, not the name of a field.
    if (num.tryParse(expr) != null) return expr;
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
    if (!RegExp(r'^[A-Za-z_][A-Za-z0-9_ ]*$').hasMatch(fieldPart)) {
      return null; // not a field name: an expression File4Base does not evaluate
    }
    final snakeField = fieldPart.toLowerCase().replaceAll(RegExp('[^a-z0-9]+'), '_');
    final mergeToken = '{{$snakeField}}';
    final resolved = host.actionResolveText(mergeToken);
    if (resolved == mergeToken) {
      return null; // no such field in this context
    }
    return resolved;
  }

  /// Whether a condition holds, or null when the condition is not one this
  /// runner can evaluate.
  ///
  /// It reads `and` / `or` / `not`, `IsEmpty(...)`, the comparisons `=`, `<>`
  /// (`≠`, `!=`), `<`, `<=` (`≤`), `>`, `>=` (`≥`) — numerically when both
  /// sides are numbers and as text otherwise — and a bare value, which holds
  /// when it is a non-zero number or a non-empty text. Null is the answer for
  /// anything else, and a script stops on it rather than taking a branch
  /// chosen by guesswork (#93).
  bool? evaluateCondition(String rawExpr, Map<String, String> localVars) {
    var expr = rawExpr.trim();
    if (expr.isEmpty) return true;
    while (expr.startsWith('(') && expr.endsWith(')')) {
      expr = expr.substring(1, expr.length - 1).trim();
    }
    final orParts = expr.split(RegExp(r'\s+or\s+', caseSensitive: false));
    if (orParts.length > 1) {
      var answer = false;
      for (final part in orParts) {
        final one = evaluateCondition(part, localVars);
        if (one == null) return null;
        answer = answer || one;
      }
      return answer;
    }
    final andParts = expr.split(RegExp(r'\s+and\s+', caseSensitive: false));
    if (andParts.length > 1) {
      var answer = true;
      for (final part in andParts) {
        final one = evaluateCondition(part, localVars);
        if (one == null) return null;
        answer = answer && one;
      }
      return answer;
    }
    if (expr.toLowerCase().startsWith('not ')) {
      final inner = evaluateCondition(expr.substring(4), localVars);
      return inner == null ? null : !inner;
    }
    if (expr.startsWith('!') && !expr.startsWith('!=')) {
      final inner = evaluateCondition(expr.substring(1), localVars);
      return inner == null ? null : !inner;
    }
    final isEmptyMatch = RegExp(r'^isempty\s*\(\s*(.+)\s*\)$', caseSensitive: false).firstMatch(expr);
    if (isEmptyMatch != null) {
      final inner = isEmptyMatch.group(1)!;
      // A field this context does not have is not an empty field, so the
      // question cannot be answered.
      final value = resolveValue(inner, localVars);
      return value?.trim().isEmpty;
    }

    // Two-character operators are looked for first, so `<=` is never read as
    // `<` or as `=`, which is how ordering comparisons went unevaluated.
    for (final op in <String>['<=', '>=', '!=', '<>', '≠', '≤', '≥', '=', '<', '>']) {
      final idx = expr.indexOf(op);
      if (idx <= 0) continue;
      final left = resolveValue(expr.substring(0, idx), localVars);
      final right = resolveValue(expr.substring(idx + op.length), localVars);
      if (left == null || right == null) return null;
      return _compareValues(op, left, right);
    }

    final value = resolveValue(expr, localVars);
    if (value == null) return null;
    final number = num.tryParse(value.trim());
    if (number != null) return number != 0;
    final text = value.trim();
    return text.isNotEmpty && text.toLowerCase() != 'false';
  }

  bool? _compareValues(String op, String left, String right) {
    final l = num.tryParse(left.trim());
    final r = num.tryParse(right.trim());
    final numeric = l != null && r != null;
    switch (op) {
      case '=':
        return numeric ? l == r : left == right;
      case '!=':
      case '<>':
      case '≠':
        return numeric ? l != r : left != right;
      case '<':
        return numeric ? l < r : left.compareTo(right) < 0;
      case '>':
        return numeric ? l > r : left.compareTo(right) > 0;
      case '<=':
      case '≤':
        return numeric ? l <= r : left.compareTo(right) <= 0;
      case '>=':
      case '≥':
        return numeric ? l >= r : left.compareTo(right) >= 0;
    }
    return null;
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

/// One script being executed: the program counter, the If frames, the local
/// variables and the halt state.
///
/// Normal execution and the debugger both advance this one state machine, so
/// stepping through a script has the same effects, in the same order, as
/// running it — and a construct the debugger shows is a construct the runner
/// really interprets (#94).
class ScriptExecution {

  ScriptExecution({required this.script, required this.runner, this.depth = 0, Map<String, String>? localVars})
      : steps = List<ScriptStepModel>.from(script.steps)
          ..sort((a, b) => a.sequenceIdx.compareTo(b.sequenceIdx)),
        localVars = localVars ?? <String, String>{} {
    _hasEndIf = steps.any((s) => s.isEnabled && LayoutActionRunner.normalizeStepType(s.stepType) == 'end_if');
    _hasEndLoop = steps.any((s) => s.isEnabled && LayoutActionRunner.normalizeStepType(s.stepType) == 'end_loop');
  }
  final ScriptModel script;
  final LayoutActionRunner runner;
  final int depth;

  /// The script's steps in the order they run.
  final List<ScriptStepModel> steps;

  /// The script's `$local` variables, shared by every step of this run.
  final Map<String, String> localVars;

  final List<_IfFrame> _ifStack = <_IfFrame>[];
  final List<_LoopFrame> _loopStack = <_LoopFrame>[];
  late final bool _hasEndIf;
  late final bool _hasEndLoop;
  int _pc = 0;

  /// A loop with no way out would hang the application rather than report
  /// anything, so one is stopped after this many turns and says so. It is far
  /// above any found set a person loops over by hand.
  static const maxLoopIterations = 100000;

  /// The index of the step about to run.
  int get pc => _pc;
  bool get atEnd => _pc >= steps.length;

  /// The `sequence_idx` of the step about to run, for a message about it.
  int get currentSequence => atEnd ? steps.length : steps[_pc].sequenceIdx;

  /// False while execution is inside a branch that was not taken.
  bool get isExecuting => _ifStack.every((f) => f.active);

  /// Runs the step the execution is on and moves to the next one.
  Future<StepOutcome> advance() async {
    if (atEnd) return const StepOutcome(StepOutcomeKind.exited);
    final step = steps[_pc];
    _pc++;

    if (!step.isEnabled) return const StepOutcome(StepOutcomeKind.skipped);
    final norm = LayoutActionRunner.normalizeStepType(step.stepType);

    // Branch bookkeeping happens whether or not the branch is being taken: an
    // End If has to close the frame its If opened. A script with no End If at
    // all is not a conditional script, and its If falls through to the step
    // runner, which says so.
    if (_hasEndIf) {
      switch (norm) {
        case 'if':
          final parentActive = isExecuting;
          var taken = false;
          if (parentActive) {
            final condition = runner.evaluateCondition(runner.extractCondition(step.params), localVars);
            if (condition == null) {
              return StepOutcome(StepOutcomeKind.stopped,
                  'the condition ${_describeCondition(step.params)} is not one File4Base can evaluate');
            }
            taken = condition;
          }
          _ifStack.add(_IfFrame(parentActive: parentActive, branchTaken: taken, active: taken));
          return const StepOutcome(StepOutcomeKind.controlFlow);
        case 'else_if':
          if (_ifStack.isNotEmpty) {
            final top = _ifStack.last;
            if (!top.parentActive || top.branchTaken) {
              top.active = false;
            } else {
              final condition = runner.evaluateCondition(runner.extractCondition(step.params), localVars);
              if (condition == null) {
                return StepOutcome(StepOutcomeKind.stopped,
                    'the condition ${_describeCondition(step.params)} is not one File4Base can evaluate');
              }
              top
                ..branchTaken = condition
                ..active = condition;
            }
          }
          return const StepOutcome(StepOutcomeKind.controlFlow);
        case 'else':
          if (_ifStack.isNotEmpty) {
            final top = _ifStack.last;
            if (!top.parentActive || top.branchTaken) {
              top.active = false;
            } else {
              top
                ..branchTaken = true
                ..active = true;
            }
          }
          return const StepOutcome(StepOutcomeKind.controlFlow);
        case 'end_if':
          if (_ifStack.isNotEmpty) _ifStack.removeLast();
          return const StepOutcome(StepOutcomeKind.controlFlow);
      }
    }

    if (!isExecuting) return const StepOutcome(StepOutcomeKind.skipped);

    // Loop bookkeeping sits after the branch check, so a loop inside a branch
    // that was not taken is passed over whole — its End Loop with it — rather
    // than repeating a branch nobody entered. A script with no End Loop at
    // all is not a loop script, and its Loop falls through to the step
    // runner, which says so.
    if (_hasEndLoop) {
      switch (norm) {
        case 'loop':
          // The step after Loop is where each turn begins. The If depth is
          // remembered with it, so leaving the loop early also leaves the
          // branches that were opened inside it.
          _loopStack.add(_LoopFrame(firstStep: _pc, ifDepth: _ifStack.length));
          return const StepOutcome(StepOutcomeKind.controlFlow);

        case 'exit_loop_if':
          if (_loopStack.isEmpty) {
            return const StepOutcome(StepOutcomeKind.stopped,
                'Exit Loop If is not inside a Loop');
          }
          final condition = runner.evaluateCondition(
              runner.extractCondition(step.params), localVars);
          if (condition == null) {
            return StepOutcome(StepOutcomeKind.stopped,
                'the condition ${_describeCondition(step.params)} is not one File4Base can evaluate');
          }
          if (condition) {
            return _leaveLoop();
          }
          return const StepOutcome(StepOutcomeKind.controlFlow);

        case 'end_loop':
          if (_loopStack.isEmpty) {
            return const StepOutcome(StepOutcomeKind.stopped, 'End Loop has no Loop to close');
          }
          final frame = _loopStack.last;
          frame.turns++;
          if (frame.turns > maxLoopIterations) {
            return const StepOutcome(StepOutcomeKind.stopped,
                'this Loop has run $maxLoopIterations times without reaching an Exit Loop If, '
                'so it was stopped rather than left running');
          }
          // Back to the top. Any branch opened inside this turn of the loop
          // is closed, so the next turn starts with the same branch state as
          // the first one.
          _truncateIfStack(frame.ifDepth);
          _pc = frame.firstStep;
          return const StepOutcome(StepOutcomeKind.controlFlow);
      }
    }

    if (norm == 'halt_script') return const StepOutcome(StepOutcomeKind.halted);
    if (norm == 'exit_script') return const StepOutcome(StepOutcomeKind.exited);

    final result = await runner.runStep(step.stepType, step.params, depth: depth, localVars: localVars);
    if (result.halted) return const StepOutcome(StepOutcomeKind.halted);
    if (!result.completed) return StepOutcome(StepOutcomeKind.stopped, result.message);
    return const StepOutcome(StepOutcomeKind.ran);
  }

  /// Leaves the innermost loop: execution carries on after its End Loop.
  ///
  /// The End Loop is found by counting the loops that open and close in
  /// between, so a nested loop's End Loop is never mistaken for this one.
  StepOutcome _leaveLoop() {
    final frame = _loopStack.removeLast();
    _truncateIfStack(frame.ifDepth);

    var depth = 0;
    for (var i = _pc; i < steps.length; i++) {
      if (!steps[i].isEnabled) continue;
      switch (LayoutActionRunner.normalizeStepType(steps[i].stepType)) {
        case 'loop':
          depth++;
        case 'end_loop':
          if (depth == 0) {
            _pc = i + 1;
            return const StepOutcome(StepOutcomeKind.controlFlow);
          }
          depth--;
      }
    }
    // A Loop whose End Loop is missing: the script ends here rather than
    // running the rest of it outside a loop it never left.
    _pc = steps.length;
    return const StepOutcome(StepOutcomeKind.controlFlow);
  }

  /// Drops the If frames opened above [depth], which is what leaving or
  /// restarting a loop does to the branches opened inside it.
  void _truncateIfStack(int depth) {
    while (_ifStack.length > depth) {
      _ifStack.removeLast();
    }
  }

  String _describeCondition(Map<String, dynamic> params) {
    final condition = runner.extractCondition(params).trim();
    return condition.isEmpty ? '(empty)' : '"$condition"';
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

/// One `Loop` being executed: where its turns begin, how many have run, and
/// how deep the If frames were when it was entered.
class _LoopFrame {

  _LoopFrame({required this.firstStep, required this.ifDepth});

  /// The index of the step just after the `Loop`, where each turn starts.
  final int firstStep;

  /// The If-frame depth outside the loop, restored on every turn and on the
  /// way out.
  final int ifDepth;

  int turns = 0;
}
