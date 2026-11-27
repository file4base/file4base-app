import 'dart:async';
import 'dart:typed_data';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/layout_engine/script_files.dart';
import 'package:file4base_client/features/layout_engine/script_runtime_state.dart';
import 'package:http/http.dart' as http;

export 'package:file4base_client/features/layout_engine/script_files.dart';
export 'package:file4base_client/features/layout_engine/script_runtime_state.dart';

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

/// Where a calculation is evaluated (#26): the occurrence the window's layout
/// is based on, and the record in hand. Implemented by the data browser.
abstract class CalculationContextHost {
  /// The names a field of this window's own occurrence is qualified with —
  /// its table occurrence and its table — lower-cased.
  Set<String> get actionOwnOccurrenceNames;

  /// [expression] evaluated by the server, with the solution's custom
  /// functions, against the record in hand when there is one, related fields
  /// read through the relationships, as a calculation field is: a script
  /// step's formula the runner cannot evaluate itself. [variables] are the
  /// script's `$` and `$$` variables, [get] the answers of the Get functions
  /// only the client knows. Null when the window has no table to evaluate it
  /// in.
  Future<EvaluationModel?> actionEvaluateInScript(
    String expression, {
    Map<String, String> variables = const {},
    Map<String, Object?> get = const {},
  });
}

/// A field of another table occurrence than the window's (`Occurrence::Field`).
/// FileMaker writes it in a related record, never in the field of the same
/// name of the record in hand. Implemented by the data browser.
abstract class RelatedFieldHost {
  /// Whether [occurrence] is the occurrence the window's layout shows.
  Future<bool> actionIsOwnOccurrence(String occurrence);

  /// What [field] of [occurrence] holds in the related record a step would
  /// write (see [actionSetRelatedField]): empty when there is none, null
  /// when the field cannot be read.
  Future<String?> actionRelatedFieldText(String occurrence, String field);

  /// Writes [value] into [field] of [occurrence], another occurrence than
  /// the window's: in the active portal row when it is a row of that
  /// occurrence, otherwise in the first related record through the
  /// relationship, in the relationship's sort order; with none, in a record
  /// created through the relationship when it allows creation. Returns
  /// FileMaker's error code: 0 when written, 101 when there is no related
  /// record and none may be created, 102 when there is no such field, 103
  /// when no relationship leads to [occurrence], 201 for a field that cannot
  /// be modified.
  Future<int> actionSetRelatedField(String occurrence, String field, String value);
}

/// Script steps of the auto-enter options: Set Next Serial Value and
/// Relookup Field Contents. Implemented by the data browser; a host without
/// it makes the steps fail with FileMaker's error 3 (command unavailable).
abstract class AutoEnterActionHost {
  /// Sets the value the next record gets in the serial number [field] of the
  /// current table. Returns a FileMaker error code: 0 when it was set.
  Future<int> actionSetNextSerialValue(String field, String value);

  /// Copies again the looked-up values of the found set whose lookups start
  /// from the match field [field], after asking when [dialog] is true, and
  /// reloads the records. Returns a FileMaker error code: 0 when it ran, 1
  /// when the user cancelled.
  Future<int> actionRelookupFieldContents(String field, {bool dialog = false});
}

/// Script steps that make a document or hand one over (#133): Save Records
/// as PDF, and the compose window of Send Mail. Implemented by the data
/// browser; a host without it makes Save Records as PDF fail with error 3.
abstract class DocumentActionHost {
  /// The pages of the layout in hand for [records]: `current` (the record
  /// in hand), `browsed` (every record of the found set) or `blank` (the
  /// layout with no data). An empty list when there is no record to draw;
  /// null when the layout cannot be drawn.
  Future<List<PdfSheetPage>?> actionRenderRecordsPdf(String records);

  /// Makes each record of the found set the current one in turn, in order,
  /// and calls [visit] with its 1-based position and the found count; stops
  /// when [visit] answers false. The current record is the same afterwards.
  /// Answers false when the found set could not be read.
  Future<bool> actionForEachFoundRecord(Future<bool> Function(int position, int count) visit);

  /// Shows File4Base's compose window for [mail], sent as [from] with the
  /// files [attachments] (their names). Answers the message as the user
  /// left it, or null when they cancelled.
  Future<OutgoingMail?> actionComposeMail(OutgoingMail mail, {required String from, List<String> attachments = const []});

  /// Opens a document a script saved: in its application on the desktop, as
  /// a download in the browser.
  Future<void> actionOpenDocument(String path, String nativePath, Uint8List bytes);

  /// Tells the user something without stopping the script.
  Future<void> actionNotify(String message);
}

/// The found set an `Import Records` step works on (#191), as FileMaker's
/// does: the records it replaces in order, or the only ones it updates when
/// they match, and the imported records it shows once it is done.
/// Implemented by the data browser. Without a window on the destination
/// table, a replace, or an update converted from FileMaker, stops the
/// script; an update designed here works on the whole table.
abstract class ImportRecordsHost {
  /// The table of the window's layout, in this solution; "" when the window
  /// shows another solution's table, whose records a transfer into this
  /// solution never writes.
  String get actionImportTable;

  /// Commits the record being edited, as FileMaker does before it imports;
  /// false when it stays uncommitted (a validation or OnRecordCommit
  /// refused it).
  Future<bool> actionCommitBeforeImport();

  /// The ids of every record of the found set, in found-set order, records
  /// not read yet included; null when the found set cannot be read.
  Future<List<String>?> actionFoundSetIds();

  /// Makes [ids] the found set, unsorted, in that order, as FileMaker shows
  /// the records an import added or updated; false when they could not be
  /// read.
  Future<bool> actionShowImportedRecords(List<String> ids);
}

/// Outcome of running a button action. [message] explains why a script
/// stopped early (unsupported step, missing script, ...).
class LayoutActionResult {

  const LayoutActionResult.ok({this.scriptResult})
      : completed = true,
        halted = false,
        exitLoop = false,
        message = null;

  /// Signals `ScriptExecution` to leave the innermost `Loop` (used by
  /// `Go to Record/Request/Page` and `Go to Portal Row` with `Exit after last: On`).
  const LayoutActionResult.exitLoop()
      : completed = true,
        halted = false,
        exitLoop = true,
        message = null,
        scriptResult = null;

  /// Halt Script: this script and every script that called it stop, which is
  /// what Claris defines the step as doing. It is not a failure, so
  /// [completed] stays true, but no later step of any caller may run (#95).
  const LayoutActionResult.haltedAll()
      : completed = true,
        halted = true,
        exitLoop = false,
        message = null,
        scriptResult = null;

  const LayoutActionResult.stopped(this.message)
      : completed = false,
        halted = false,
        exitLoop = false,
        scriptResult = null;
  final bool completed;

  /// True when a Halt Script step ended the whole execution session.
  final bool halted;

  /// True when a navigation step hit a boundary with `Exit after last: On`.
  final bool exitLoop;
  final String? message;

  /// Null means the script returned no explicit result. An explicit empty
  /// string is a result and clears the caller's previous sub-script result.
  final String? scriptResult;
}

/// Parameter, call-scoped result, and shared session state belonging to one
/// invocation. Each nested call gets its own parameter and local variables
/// while sharing [session] (error capture, last error, windows, clipboard)
/// across the call tree, matching FileMaker semantics (#133).
class ScriptCallContext {
  ScriptCallContext({
    this.parameter = '',
    this.scriptName = '',
    this.originalLayout = '',
    ScriptSessionState? session,
  }) : session = session ?? ScriptSessionState();
  final String parameter;
  final String scriptName;
  final String originalLayout;
  final ScriptSessionState session;
  String lastScriptResult = '';
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
    this.windowSolution,
    this.solutionName,
    ScriptFileStore? files,
  }) : files = files ?? ScriptFileStore.instance;
  final ApiClient apiClient;
  final LayoutActionHost host;

  /// The folders a script reads and writes files in (#133).
  final ScriptFileStore files;

  /// Whether this solution sends mail through SMTP, read once (#133).
  Future<MailStatus>? _mailStatus;

  Future<MailStatus> _loadMailStatus() => _mailStatus ??= apiClient.mailStatus().catchError((Object _) => const MailStatus());

  /// The solution this runner's scripts belong to, when it is not the one on
  /// screen (#107). It names the solution in messages; null means "this
  /// solution".
  final String? solutionName;

  /// Null when the script runs in the solution whose window it is shown in,
  /// which is every script but one performed from another solution (#107).
  ///
  /// When it is set, the script belongs to a solution with no window of its
  /// own here: the window on screen belongs to [windowSolution], so a step
  /// that acts on a window — New Record, Set Field, Go to Layout, … — would act
  /// on the wrong solution's table. Such a step stops the script and says so.
  final String? windowSolution;

  /// Whether this runner's steps may act on the window on screen.
  bool get hasWindow => windowSolution == null;

  /// The steps that act on the window on screen: its layout, its found set,
  /// its current record, or the session itself. A script performed from
  /// another solution has no window here, so it cannot run these (#107).
  static const windowSteps = <String>{
    'new_record',
    'duplicate_record',
    'delete_record',
    'delete_all_records',
    'delete_portal_row',
    'commit_records',
    'revert_record',
    'go_to_record',
    'go_to_field',
    'go_to_next_field',
    'go_to_object',
    'go_to_related_record',
    'go_to_portal_row',
    'enter_browse_mode',
    'enter_find_mode',
    'perform_find',
    'constrain_found_set',
    'extend_found_set',
    'modify_last_find',
    'show_all_records',
    'show_omitted_only',
    'omit_record',
    'omit_multiple_records',
    'sort_records',
    'unsort_records',
    'enter_preview_mode',
    'go_to_layout',
    'set_field',
    'set_field_by_name',
    'insert_calculated_result',
    'replace_field_contents',
    'set_next_serial_value',
    'relookup_field_contents',
    'copy',
    'paste',
    'cut',
    'clear',
    'insert_picture',
    'insert_file',
    'export_field_contents',
    'new_window',
    'select_window',
    'close_window',
    'adjust_window',
    'move_resize_window',
    'freeze_window',
    'refresh_window',
    'print',
    'save_records_as_pdf',
    'save_records_as_excel',
    're_login',
    'close_file',
    'exit_application',
  };

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

  /// Globals belong to this solution's authenticated session, including when
  /// called through a peer client. They never fall back to a caller's globals.
  Map<String, String> get globalVariables => apiClient.scriptGlobalVariables;

  /// `Set Error Logging` state of this solution's session. It is attached to
  /// the session's global variables, which live exactly as long as the
  /// session, so it outlasts the script run that turned it on and is
  /// dropped on sign-out.
  ScriptErrorLogging get errorLogging => _errorLogging[globalVariables] ??= ScriptErrorLogging();
  static final Expando<ScriptErrorLogging> _errorLogging = Expando<ScriptErrorLogging>('errorLogging');

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
      '#' || '#_(comment)' => 'comment',
      'new_record_request' => 'new_record',
      'duplicate_record_request' => 'duplicate_record',
      'delete_record_request' => 'delete_record',
      'commit_records_requests' => 'commit_records',
      'revert_record_request' => 'revert_record',
      'go_to_record_request_page' => 'go_to_record',
      'show_custom_dialog' => 'show_dialog',
      'pause_resume_script' => 'pause_script',
      'move_resize_window' => 'move_resize_window',
      'relogin' => 're_login',
      _ => s,
    };
  }

  /// Session state used when [runStep] is invoked directly (e.g. single-step
  /// button actions or unit tests without a [ScriptExecution]).
  final ScriptSessionState _defaultSession = ScriptSessionState();

  ExtendedLayoutActionHost? get _extHost => host is ExtendedLayoutActionHost ? host as ExtendedLayoutActionHost : null;
  AutoEnterActionHost? get _autoEnterHost => host is AutoEnterActionHost ? host as AutoEnterActionHost : null;

  RelatedFieldHost? get _relatedHost => host is RelatedFieldHost ? host as RelatedFieldHost : null;

  /// The field a step names: `Occurrence::Field`, or a bare name with the
  /// occurrence [table] (the step's `field_table`) gives. The occurrence is
  /// kept: a field of another occurrence is a related field, not the field
  /// of the same name of the record in hand.
  static ({String occurrence, String field}) fieldTarget(String raw, [String table = '']) {
    final text = raw.trim();
    final cut = text.indexOf('::');
    if (cut < 0) return (occurrence: table.trim(), field: text);
    return (occurrence: text.substring(0, cut).trim(), field: text.substring(cut + 2).trim());
  }

  static String _qualified(({String occurrence, String field}) target) =>
      target.occurrence.isEmpty ? target.field : '${target.occurrence}::${target.field}';

  /// Whether [occurrence] is the window's own; a host that cannot tell
  /// writes no field of a named occurrence.
  Future<bool> _isOwnOccurrence(String occurrence) async {
    if (occurrence.trim().isEmpty) return true;
    final related = _relatedHost;
    if (related != null) return related.actionIsOwnOccurrence(occurrence);
    final context = host is CalculationContextHost ? host as CalculationContextHost : null;
    return context != null && context.actionOwnOccurrenceNames.contains(occurrence.trim().toLowerCase());
  }

  /// Writes [value] into [target], in the record in hand or, for a field of
  /// another occurrence, in the related record FileMaker writes. Returns
  /// FileMaker's error code.
  Future<int> _writeField(({String occurrence, String field}) target, String value) async {
    if (!await _isOwnOccurrence(target.occurrence)) {
      final related = _relatedHost;
      if (related == null) return 102;
      return related.actionSetRelatedField(target.occurrence, target.field, value);
    }
    return await host.actionSetField(target.field, value) ? 0 : 102;
  }

  /// What [target] holds where [_writeField] would write it; null when it
  /// cannot be read.
  Future<String?> _readField(({String occurrence, String field}) target) async {
    if (!await _isOwnOccurrence(target.occurrence)) {
      return _relatedHost?.actionRelatedFieldText(target.occurrence, target.field);
    }
    final token = '{{${target.field}}}';
    final value = host.actionResolveText(token);
    return value == token ? null : value;
  }

  /// Why [step] wrote nothing into [target], for FileMaker's error [code].
  static String _writeRefusal(String step, ({String occurrence, String field}) target, int code) {
    final name = _qualified(target);
    return switch (code) {
      101 => '$step: there is no related record of ${target.occurrence} to write "$name" into, '
          'and the relationship does not allow creating one. Nothing was written.',
      103 => '$step: no relationship leads from this layout to ${target.occurrence}, so '
          '"$name" cannot be written. Nothing was written.',
      201 => '$step: "$name" cannot be modified. Nothing was written.',
      _ when target.occurrence.isEmpty => 'Field "${target.field}" is not in this table.',
      _ => '$step: "$name" is not a field this window can write. Nothing was written.',
    };
  }

  Future<LayoutActionResult> run(ButtonActionModel action) async {
    if (action.isPerformScript) {
      if (action.scriptId == null && (action.scriptName == null || action.scriptName!.isEmpty)) {
        return const LayoutActionResult.stopped('This button has no script selected.');
      }
      final parameter = await evaluateParameter(action.parameter ?? '', <String, String>{});
      if (parameter == null) {
        return const LayoutActionResult.stopped('Cannot evaluate the script parameter.');
      }
      return _performScript(id: action.scriptId, name: action.scriptName, depth: 0, parameter: parameter);
    }
    final step = action.stepType;
    if (step == null) return const LayoutActionResult.stopped('This button has no step selected.');
    final vars = <String, String>{};
    final failure = await prepareStepFormulas(step, action.params, vars);
    if (failure != null) return LayoutActionResult.stopped(failure);
    try {
      return await runStep(step, action.params, depth: 0, localVars: vars);
    } finally {
      _serverAnswers.clear();
    }
  }

  Future<LayoutActionResult> _performScript({
    required int depth,
    String? id,
    String? name,
    String parameter = '',
    ScriptSessionState? session,
  }) async {
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
    // A script performed in another solution is named with its solution, so a
    // message never leaves anyone guessing which "Receive" it was.
    final where = solutionName == null ? '' : ' in the solution $solutionName';
    if (script == null) return LayoutActionResult.stopped('Script "${name ?? id}" not found$where.');

    final execution = ScriptExecution(
      script: script,
      runner: this,
      depth: depth,
      parameter: parameter,
      session: session ?? ScriptSessionState(),
    );
    final triggers = host is ScriptTriggerHost ? host as ScriptTriggerHost : null;
    while (!execution.atEnd) {
      final sequence = execution.currentSequence;
      final mark = triggers?.actionTriggerMark();
      final outcome = await execution.advance();
      // The script triggers a step set off run before the next step (#26).
      if (mark != null) await triggers!.actionSettleTriggers(mark);
      switch (outcome.kind) {
        case StepOutcomeKind.stopped:
          return LayoutActionResult.stopped(
              'Script "${script.name}"$where stopped at step $sequence: ${outcome.message}');
        case StepOutcomeKind.halted:
          // Every caller stops too, so the halt travels up instead of
          // looking like an ordinary return (#95).
          return const LayoutActionResult.haltedAll();
        case StepOutcomeKind.exited:
          return LayoutActionResult.ok(scriptResult: execution.returnValue);
        case StepOutcomeKind.ran:
        case StepOutcomeKind.skipped:
        case StepOutcomeKind.controlFlow:
          break;
      }
    }
    return LayoutActionResult.ok(scriptResult: execution.returnValue);
  }

  Future<LayoutActionResult> runStep(
    String stepType,
    Map<String, dynamic> params, {
    required int depth,
    Map<String, String>? localVars,
    ScriptCallContext? callContext,
  }) async {
    final vars = localVars ?? <String, String>{};
    final context = callContext ?? ScriptCallContext(session: _defaultSession);
    final session = context.session;
    final ext = _extHost;
    String p(String key, [String fallback = '']) =>
        _resolveParamsText(params[key]?.toString() ?? fallback, vars);

    final norm = normalizeStepType(stepType);
    // Running such a step would act on the window on screen, which belongs to
    // the calling solution — a record created or a field set in the wrong
    // table. The script stops instead of doing that, or of skipping it.
    LayoutActionResult windowless() => LayoutActionResult.stopped(
        'Step "$stepType" acts on a window, and this script was performed from the '
        'solution $windowSolution, whose window is the one on screen. A script '
        'performed in another solution runs only the steps that need no window.');
    if (!hasWindow && windowSteps.contains(norm)) return windowless();

    // Helper: set FileMaker error code; if Set Error Capture [On] is active,
    // allow the script to continue and inspect Get(LastError).
    LayoutActionResult failOrCapture(int code, String message) {
      session.lastError = code;
      if (session.errorCapture) {
        return const LayoutActionResult.ok();
      }
      return LayoutActionResult.stopped(message);
    }

    String currentLayoutName() {
      final ln = host.actionResolveText('{{LayoutName}}');
      return ln == '{{LayoutName}}' ? '' : ln;
    }

    bool boolParam(String key, {bool defaultValue = false}) {
      final raw = params[key];
      if (raw is bool) return raw;
      if (raw is num) return raw != 0;
      if (raw is String) {
        final l = raw.trim().toLowerCase();
        if (l == 'true' || l == 'on' || l == '1' || l == 'yes') return true;
        if (l == 'false' || l == 'off' || l == '0' || l == 'no') return false;
      }
      return defaultValue;
    }

    List<Map<String, dynamic>>? mapListParam(String key) {
      final raw = params[key];
      if (raw is List) {
        return raw.whereType<Map<dynamic, dynamic>>().map(Map<String, dynamic>.from).toList();
      }
      return null;
    }

    ({String occurrence, String field}) normalizeFieldTarget(String rawField) {
      final target = fieldTarget(rawField);
      return (
        occurrence: target.occurrence,
        field: target.field.toLowerCase().replaceAll(RegExp('[^a-z0-9]+'), '_').replaceAll(RegExp(r'^_+|_+$'), ''),
      );
    }

    // The field a step names, with its occurrence.
    ({String occurrence, String field}) stepField([String key = 'field']) =>
        fieldTarget(params[key]?.toString() ?? '', params['field_table']?.toString() ?? '');

    switch (norm) {
      case 'comment':
        // Comments never alter Get(LastError).
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
      case 'set_error_capture':
        final rawState = params['state'] ?? params['expression'];
        if (rawState is bool) {
          session.errorCapture = rawState;
        } else {
          final s = rawState?.toString().trim().toLowerCase() ?? 'on';
          session.errorCapture = s != 'off' && s != 'false' && s != '0' && !s.contains('off') && !s.contains('désactiv');
        }
        session.lastError = 0;
        return const LayoutActionResult.ok();
      case 'set_error_logging':
        // Each nonzero error a later step reports is logged, with the custom
        // debug calculation evaluated at that moment, until Off.
        // A step whose state could not be read stays Off: logging is never
        // switched on that the script did not ask for.
        final logging = errorLogging..enabled = boolParam('state');
        logging.customInfo = logging.enabled ? (params['custom_debug_info']?.toString() ?? '') : '';
        session.lastError = 0;
        return const LayoutActionResult.ok();
      case 'allow_user_abort':
        session.allowUserAbort = boolParam('state', defaultValue: true);
        session.lastError = 0;
        return const LayoutActionResult.ok();
      case 'import_records':
      case 'export_records':
        session.lastError = 0;
        return _execTransfer(norm, params, vars, session);
      case 'set_variable':
        if (!params.containsKey('value') &&
            !params.containsKey('formula') &&
            !params.containsKey('text') &&
            !params.containsKey('calc') &&
            !params.containsKey('expression')) {
          return LayoutActionResult.stopped('Step "$stepType" is not supported without a value.');
        }
        final res = _execSetVariable(params, vars, callContext: context);
        if (res.completed) session.lastError = 0;
        return res;
      case 'new_record':
        session.lastError = 0;
        await host.actionNewRecord();
      case 'duplicate_record':
        session.lastError = 0;
        await host.actionDuplicateRecord();
      case 'delete_record':
        session.lastError = 0;
        await host.actionDeleteRecord(confirm: boolParam('dialog', defaultValue: true));
      case 'delete_all_records':
        session.lastError = 0;
        if (ext != null) {
          await ext.actionDeleteAllRecords(confirm: boolParam('dialog', defaultValue: true));
        } else {
          await host.actionDeleteRecord(confirm: boolParam('dialog', defaultValue: true));
        }
      case 'delete_portal_row':
        // Never fall back to Delete Record: the current record is the
        // portal's parent, and deleting it would cascade.
        session.lastError =
            ext == null ? 101 : await ext.actionDeletePortalRow(confirm: boolParam('dialog', defaultValue: true));
      case 'commit_records':
        session.lastError = 0;
        if (ext != null) {
          await ext.actionCommitRecordEx(
            dialog: boolParam('dialog', defaultValue: true),
            skipValidation: boolParam('skip_validation'),
            forceCommit: boolParam('force_commit'),
          );
        } else {
          await host.actionCommitRecord();
        }
      case 'revert_record':
        session.lastError = 0;
        await host.actionRevertRecord();
      case 'go_to_record':
        var target = p('target', 'next').trim();
        if (target.toLowerCase() == 'by_calculation' || params.containsKey('record_calculation')) {
          final calcExpr = params['record_calculation']?.toString() ?? '';
          if (calcExpr.isNotEmpty) {
            final evaluated = resolveValue(calcExpr, vars, callContext: context);
            if (evaluated == null) {
              return const LayoutActionResult.stopped('Go to Record calculation cannot be evaluated.');
            }
            target = evaluated;
          }
        }
        final exitAfterLast = boolParam('exit_after_last');
        if (ext != null) {
          final outcome = await ext.actionGoToRecordEx(target);
          session.lastError = outcome.errorCode;
          if (outcome.atBoundary && exitAfterLast) {
            return const LayoutActionResult.exitLoop();
          }
        } else {
          session.lastError = 0;
          await host.actionGoToRecord(target);
        }
      case 'go_to_portal_row':
        var target = p('target', 'next').trim();
        if (target.toLowerCase() == 'by_calculation' || params.containsKey('record_calculation')) {
          final calcExpr = params['record_calculation']?.toString() ?? '';
          if (calcExpr.isNotEmpty) {
            final evaluated = resolveValue(calcExpr, vars, callContext: context);
            if (evaluated == null) {
              return const LayoutActionResult.stopped('Go to Portal Row calculation cannot be evaluated.');
            }
            target = evaluated;
          }
        }
        final exitAfterLast = boolParam('exit_after_last');
        if (ext != null) {
          final outcome = await ext.actionGoToPortalRow(target, select: boolParam('select'));
          session.lastError = outcome.errorCode;
          if (outcome.atBoundary && exitAfterLast) {
            return const LayoutActionResult.exitLoop();
          }
        } else {
          session.lastError = 0;
        }
      case 'go_to_field':
        final field = params['field']?.toString().trim() ?? '';
        session.activeField = field;
        if (field.isNotEmpty && ext != null) {
          final ok = await ext.actionGoToField(field, select: boolParam('select'));
          if (!ok) return failOrCapture(102, 'Field "$field" is not on this layout.');
        }
        session.lastError = 0;
      case 'go_to_next_field':
        if (ext != null) {
          await ext.actionGoToNextField();
          session.activeField = ext.actionActiveFieldName ?? session.activeField;
        }
        session.lastError = 0;
      case 'go_to_object':
        final rawObj = params['object_name']?.toString() ?? '';
        final objName = params['object_name_is_calculation'] == true
            ? resolveValue(rawObj, vars, callContext: context)
            : _resolveParamsText(rawObj, vars);
        if (objName == null) {
          return const LayoutActionResult.stopped('Go to Object calculation cannot be evaluated.');
        }
        session.activeObject = objName;
        if (objName.isNotEmpty && ext != null) {
          final ok = await ext.actionGoToObject(objName);
          if (!ok) return failOrCapture(116, 'Layout object "$objName" not found.');
        }
        session.lastError = 0;
      case 'go_to_related_record':
        final relatedTable = p('related_table').trim();
        final layoutName = p('layout_name').trim();
        final showOnly = boolParam('show_only_related', defaultValue: true);
        final newWin = boolParam('new_window');
        if (newWin) {
          final winName = p('window_name').trim().isNotEmpty ? p('window_name').trim() : (layoutName.isNotEmpty ? layoutName : relatedTable);
          session.windows.add(ScriptWindowEntry(name: winName, layoutName: layoutName.isNotEmpty ? layoutName : currentLayoutName()));
        }
        if (ext != null) {
          final code = await ext.actionGoToRelatedRecord(
            relatedTable: relatedTable,
            layoutName: layoutName.isEmpty ? null : layoutName,
            showOnlyRelated: showOnly,
            newWindow: newWin,
            matchFoundSet: boolParam('match_found_set'),
          );
          switch (code) {
            case 0:
              break;
            case 105:
              return failOrCapture(105, 'Layout "$layoutName" not found.');
            case 101:
              return failOrCapture(101, 'No related records found for "$relatedTable".');
            default:
              return failOrCapture(code, ext.goToRelatedRecordMessage ?? 'Go to Related Record could not run.');
          }
        } else if (layoutName.isNotEmpty) {
          if (!await host.actionGoToLayout(layoutName)) {
            return failOrCapture(105, 'Layout "$layoutName" not found.');
          }
        }
        session.lastError = 0;
      case 'enter_browse_mode':
        session.lastError = 0;
        await host.actionEnterBrowseMode();
      case 'enter_find_mode':
        session.lastError = 0;
        final restore = boolParam('restore');
        final findReqs = mapListParam('find_requests');
        if (ext != null) {
          await ext.actionEnterFindModeEx(requests: findReqs, restore: restore);
        } else {
          await host.actionEnterFindMode();
        }
      case 'perform_find':
      case 'constrain_found_set':
      case 'extend_found_set':
      case 'modify_last_find':
        final restore = boolParam('restore');
        final findReqs = mapListParam('find_requests');
        if (ext != null) {
          final found = await ext.actionPerformFindEx(requests: findReqs, restore: restore);
          // A negative count: OnModeExit kept the window in Find mode.
          session.lastError = found < 0 ? 20 : (found == 0 ? 401 : 0);
        } else {
          session.lastError = 0;
          await host.actionPerformFind();
        }
      case 'show_all_records':
        session.lastError = 0;
        await host.actionShowAllRecords();
      case 'show_omitted_only':
        session.lastError = 0;
        if (ext != null) {
          await ext.actionShowOmittedOnly();
        }
      case 'omit_record':
        if (ext != null) {
          final ok = await ext.actionOmitRecord();
          session.lastError = ok ? 0 : 101;
        } else {
          session.lastError = 0;
        }
      case 'omit_multiple_records':
        final count = int.tryParse(p('count', '1')) ?? 1;
        if (ext != null) {
          await ext.actionOmitMultipleRecords(count);
        }
        session.lastError = 0;
      case 'sort_records':
        session.lastError = 0;
        if (ext != null) {
          await ext.actionSortRecords(
            sortOrder: mapListParam('sort_order'),
            restore: boolParam('restore'),
            dialog: boolParam('dialog', defaultValue: true),
          );
        }
      case 'unsort_records':
        session.lastError = 0;
        if (ext != null) {
          await ext.actionUnsortRecords();
        }
      case 'enter_preview_mode':
        session.lastError = 0;
        await host.actionEnterPreviewMode();
      case 'new_window':
        session.ensureCurrentWindow(currentLayoutName());
        var winName = params['window_name']?.toString() ?? '';
        if (params['window_name_is_calculation'] == true && winName.isNotEmpty) {
          final evaluated = resolveValue(winName, vars, callContext: context);
          if (evaluated == null) {
            return const LayoutActionResult.stopped('New Window name calculation cannot be evaluated.');
          }
          winName = evaluated;
        } else {
          winName = _resolveParamsText(winName, vars).trim();
        }
        if (winName.isEmpty) {
          winName = 'Window ${session.windows.length + 1}';
        }
        var targetLayout = params['layout_name']?.toString().trim() ?? '';
        if (params['layout_destination']?.toString() == 'by_name' && params['layout_calculation'] != null) {
          targetLayout = resolveValue(params['layout_calculation'].toString(), vars, callContext: context) ?? '';
        }
        final effectiveLayout = targetLayout.isNotEmpty ? targetLayout : currentLayoutName();
        session.windows.add(ScriptWindowEntry(
          name: winName,
          layoutName: effectiveLayout,
          style: params['window_style']?.toString() ?? 'Document',
          top: double.tryParse(params['top']?.toString() ?? ''),
          left: double.tryParse(params['left']?.toString() ?? ''),
          width: double.tryParse(params['width']?.toString() ?? ''),
          height: double.tryParse(params['height']?.toString() ?? ''),
        ));
        if (targetLayout.isNotEmpty) {
          if (!await host.actionGoToLayout(targetLayout)) {
            return failOrCapture(105, 'Layout "$targetLayout" not found.');
          }
        }
        session.lastError = 0;
      case 'select_window':
        session.ensureCurrentWindow(currentLayoutName());
        final targetKind = params['window_target']?.toString().toLowerCase() ?? '';
        var winName = params['window_name']?.toString() ?? '';
        if (params['window_name_is_calculation'] == true && winName.isNotEmpty) {
          final evaluated = resolveValue(winName, vars, callContext: context);
          if (evaluated == null) {
            return const LayoutActionResult.stopped('Select Window name calculation cannot be evaluated.');
          }
          winName = evaluated;
        } else {
          winName = _resolveParamsText(winName, vars).trim();
        }
        if (targetKind == 'current' || winName.isEmpty) {
          session.lastError = 0;
          break;
        }
        final idx = session.windows.indexWhere((w) => w.name.toLowerCase() == winName.toLowerCase());
        if (idx < 0) {
          return failOrCapture(112, 'Window "$winName" is missing.');
        }
        final selected = session.windows.removeAt(idx);
        session.windows.add(selected);
        if (selected.layoutName.isNotEmpty && selected.layoutName != currentLayoutName()) {
          await host.actionGoToLayout(selected.layoutName);
        }
        session.lastError = 0;
      case 'close_window':
        session.ensureCurrentWindow(currentLayoutName());
        var winName = params['window_name']?.toString() ?? '';
        if (params['window_name_is_calculation'] == true && winName.isNotEmpty) {
          final evaluated = resolveValue(winName, vars, callContext: context);
          if (evaluated == null) {
            return const LayoutActionResult.stopped('Close Window name calculation cannot be evaluated.');
          }
          winName = evaluated;
        } else {
          winName = _resolveParamsText(winName, vars).trim();
        }
        if (winName.isEmpty || params['window_target']?.toString().toLowerCase() == 'current') {
          if (session.windows.length > 1) {
            session.windows.removeLast();
          }
        } else {
          final idx = session.windows.indexWhere((w) => w.name.toLowerCase() == winName.toLowerCase());
          if (idx < 0) {
            return failOrCapture(112, 'Window "$winName" is missing.');
          }
          session.windows.removeAt(idx);
        }
        if (session.windows.isNotEmpty) {
          final front = session.windows.last;
          if (front.layoutName.isNotEmpty && front.layoutName != currentLayoutName()) {
            await host.actionGoToLayout(front.layoutName);
          }
        }
        session.lastError = 0;
      case 'adjust_window':
        session.lastError = 0;
        await host.actionAdjustWindow(p('window_action', 'Restore'));
      case 'move_resize_window':
        final win = session.ensureCurrentWindow(currentLayoutName());
        win
          ..top = double.tryParse(params['top']?.toString() ?? '') ?? win.top
          ..left = double.tryParse(params['left']?.toString() ?? '') ?? win.left
          ..width = double.tryParse(params['width']?.toString() ?? '') ?? win.width
          ..height = double.tryParse(params['height']?.toString() ?? '') ?? win.height;
        session.lastError = 0;
      case 'freeze_window':
        session.windowFrozen = true;
        session.lastError = 0;
      case 'refresh_window':
        session.windowFrozen = false;
        session.lastError = 0;
        if (ext != null) {
          await ext.actionRefreshWindow(
            flushJoinCache: boolParam('flush_join_cache'),
            flushExternalCache: boolParam('flush_external_cache'),
          );
        }
      case 'refresh_portal':
        final rawPortal = params['object_name']?.toString() ?? '';
        final portalName = params['object_name_is_calculation'] == true
            ? resolveValue(rawPortal, vars, callContext: context)
            : _resolveParamsText(rawPortal, vars);
        if (portalName == null) {
          return const LayoutActionResult.stopped('Refresh Portal calculation cannot be evaluated.');
        }
        // With no object named, FileMaker refreshes the active object; with
        // none active either, every portal of the layout is reloaded.
        final portal = portalName.trim().isNotEmpty ? portalName.trim() : session.activeObject;
        if (ext != null && !await ext.actionRefreshPortal(portal)) {
          return failOrCapture(116, 'Layout object "$portal" is not on this layout.');
        }
        session.lastError = 0;
      case 'beep':
        session.lastError = 0;
        if (ext != null) {
          await ext.actionBeep();
        }
      case 'print_setup':
        // Documented deterministic no-op on web/headless where OS print setup
        // dialogs are unavailable; retains settings on the session (#133).
        if (params['print_settings'] is Map) {
          session.printSettings = Map<String, dynamic>.from(params['print_settings'] as Map);
        }
        session.lastError = 0;
      case 'print':
        session.lastError = 0;
        if (ext != null) {
          await ext.actionPrint(
            dialog: boolParam('dialog', defaultValue: true),
            printSettings: params['print_settings'] is Map
                ? Map<String, dynamic>.from(params['print_settings'] as Map)
                : session.printSettings,
          );
        }
      case 'save_records_as_pdf':
        return _runSaveRecordsAsPdf(params, vars, context, failOrCapture, boolParam, currentLayoutName());
      case 'save_records_as_excel':
        session.lastError = 0;
        if (ext != null) {
          await ext.actionSaveRecordsAsPdf(
            path: params['path']?.toString(),
            dialog: boolParam('dialog', defaultValue: true),
            append: boolParam('append'),
          );
        }
      case 'install_menu_set':
        session.activeMenuSet = p('menu_set');
        session.lastError = 0;
      case 'show_hide_toolbars':
        final state = p('state', 'toggle').toLowerCase();
        session.uiVisibility['toolbars'] = state;
        if (ext != null) await ext.actionSetUiVisibility('toolbars', state);
        session.lastError = 0;
      case 'show_hide_menubar':
        final state = p('state', 'toggle').toLowerCase();
        session.uiVisibility['menubar'] = state;
        if (ext != null) await ext.actionSetUiVisibility('menubar', state);
        session.lastError = 0;
      case 'show_hide_text_ruler':
        final state = p('state', 'toggle').toLowerCase();
        session.uiVisibility['text_ruler'] = state;
        if (ext != null) await ext.actionSetUiVisibility('text_ruler', state);
        session.lastError = 0;
      case 're_login':
        session.lastError = 0;
        await host.actionReLogin();
      case 'close_file':
        session.lastError = 0;
        await host.actionCloseFile();
      case 'open_file':
        return _openFile(params['file_reference']?.toString().trim() ?? '', session, failOrCapture);
      case 'exit_application':
        session.lastError = 0;
        await host.actionExitApplication();
      case 'go_to_layout':
        final dest = params['layout_destination']?.toString().trim().toLowerCase() ?? '';
        final curBefore = currentLayoutName();
        if (curBefore.isNotEmpty) {
          _setVariable(r'$$lastLayout', curBefore, vars);
        }
        if (dest == 'original') {
          final original = context.originalLayout.isNotEmpty
              ? context.originalLayout
              : (_lookupVariable(r'$$lastLayout', vars) ?? curBefore);
          if (original.isEmpty) {
            return failOrCapture(105, 'Go to Layout [Original Layout] has no original layout.');
          }
          if (!await host.actionGoToLayout(original)) {
            return failOrCapture(105, 'Layout "$original" not found.');
          }
          session.ensureCurrentWindow(original).layoutName = original;
          session.lastError = 0;
          break;
        }
        if (dest == 'by_number') {
          final calcExpr = params['layout_calculation']?.toString() ?? params['expression']?.toString() ?? '';
          final evaluated = resolveValue(calcExpr, vars, callContext: context);
          final layoutNum = int.tryParse(evaluated?.trim() ?? '');
          if (layoutNum == null || layoutNum < 1) {
            return failOrCapture(105, 'Go to Layout by number could not evaluate "$calcExpr".');
          }
          final ok = ext != null
              ? await ext.actionGoToLayoutByNumber(layoutNum)
              : await host.actionGoToLayout(layoutNum.toString());
          if (!ok) {
            return failOrCapture(105, 'Layout #$layoutNum not found.');
          }
          session.ensureCurrentWindow(currentLayoutName()).layoutName = currentLayoutName();
          session.lastError = 0;
          break;
        }
        String layout;
        if (dest == 'by_name' || (params['layout_calculation']?.toString().trim().isNotEmpty ?? false)) {
          final calcExpr = params['layout_calculation']?.toString() ?? params['expression']?.toString() ?? '';
          final evaluated = resolveValue(calcExpr, vars, callContext: context);
          if (evaluated == null) {
            return failOrCapture(105, 'Go to Layout calculation "$calcExpr" cannot be evaluated.');
          }
          layout = evaluated.trim();
        } else {
          var rawTarget = params['layout_name']?.toString().trim() ?? '';
          if (rawTarget.isEmpty) {
            rawTarget = _extractLayoutFromExpression(params['expression']?.toString() ?? '');
          }
          layout = _resolveParamsText(rawTarget, vars).trim();
          if (layout.isEmpty && rawTarget.startsWith(r'$')) {
            layout = _lookupVariable(rawTarget, vars) ?? '';
          }
        }
        if (layout.isEmpty) return failOrCapture(105, 'Go to Layout has no layout selected.');
        if (!await host.actionGoToLayout(layout)) {
          return failOrCapture(105, 'Layout "$layout" not found.');
        }
        session.ensureCurrentWindow(layout).layoutName = layout;
        session.lastError = 0;
      case 'set_field':
        final target = stepField();
        if (target.field.isEmpty) return failOrCapture(102, 'Set Field has no field selected.');
        final value = params['value_is_calculation'] == true
            ? resolveValue(params['value']?.toString() ?? '', vars, callContext: context)
            : p('value');
        if (value == null) {
          return const LayoutActionResult.stopped('Set Field calculation is not supported in this context.');
        }
        final written = await _writeField(target, value);
        if (written != 0) return failOrCapture(written, _writeRefusal('Set Field', target, written));
        session.lastError = 0;
      case 'set_field_by_name':
        final rawTargetField = params['target_field']?.toString() ?? params['field']?.toString() ?? '';
        final resolvedTarget = params['target_field_is_calculation'] == true
            ? resolveValue(rawTargetField, vars, callContext: context)
            : (resolveValue(rawTargetField, vars, callContext: context) ?? _resolveParamsText(rawTargetField, vars));
        if (resolvedTarget == null || resolvedTarget.trim().isEmpty) {
          return failOrCapture(102, 'Set Field By Name could not resolve target field "$rawTargetField".');
        }
        final target = normalizeFieldTarget(resolvedTarget);
        final value = params['value_is_calculation'] == true
            ? resolveValue(params['value']?.toString() ?? '', vars, callContext: context)
            : (resolveValue(params['value']?.toString() ?? '', vars, callContext: context) ?? p('value'));
        if (value == null) {
          return const LayoutActionResult.stopped('Set Field By Name calculation is not supported in this context.');
        }
        final written = await _writeField(target, value);
        if (written != 0) {
          return failOrCapture(
              written,
              target.occurrence.isEmpty && written == 102
                  ? 'Field "$resolvedTarget" (${target.field}) is not in this table.'
                  : _writeRefusal('Set Field By Name', target, written));
        }
        session.lastError = 0;
      case 'insert_calculated_result':
        var target = stepField();
        if (target.field.isEmpty) {
          target = (occurrence: '', field: ext?.actionActiveFieldName ?? session.activeField);
        }
        if (target.field.isEmpty) {
          return failOrCapture(102, 'Insert Calculated Result has no target or active field.');
        }
        final calcVal = resolveValue((params['value'] ?? params['expression'])?.toString() ?? '', vars, callContext: context);
        if (calcVal == null) {
          return const LayoutActionResult.stopped('Insert Calculated Result calculation is not supported in this context.');
        }
        final selectAll = boolParam('select', defaultValue: true);
        var finalVal = calcVal;
        if (!selectAll) {
          final existing = await _readField(target);
          if (existing != null) {
            finalVal = '$existing$calcVal';
          }
        }
        final written = await _writeField(target, finalVal);
        if (written != 0) return failOrCapture(written, _writeRefusal('Insert Calculated Result', target, written));
        session.lastError = 0;
      case 'insert_current_date':
        final now = DateTime.now();
        final today = '${now.year.toString().padLeft(4, '0')}-'
            '${now.month.toString().padLeft(2, '0')}-'
            '${now.day.toString().padLeft(2, '0')}';
        final selectAll = boolParam('select');
        String textParam(String key) => params[key]?.toString().trim() ?? '';
        final variable = [textParam('target'), textParam('name')]
            .firstWhere((v) => v.startsWith(r'$'), orElse: () => '');
        if (variable.isNotEmpty) {
          // A variable target: Select entire contents replaces it, otherwise
          // the date is appended to what it holds.
          final existing = selectAll ? '' : (_lookupVariable(variable, vars, callContext: context) ?? '');
          _setVariable(variable, '$existing$today', vars);
          session.lastError = 0;
          break;
        }
        if (!hasWindow) return windowless();
        final named = fieldTarget(textParam('field'), textParam('field_table'));
        var field = named.field;
        if (field.isEmpty) {
          // Only a step that names no target at all writes the active field;
          // one whose target could not be read fails rather than writing
          // whatever field happens to be active.
          final named = params['target_unresolved'] == true ||
              ['target', 'name', 'target_field'].any((k) => textParam(k).isNotEmpty);
          if (named) {
            return failOrCapture(102, 'Insert Current Date names a target File4Base cannot read; '
                'choose its target field again.');
          }
          field = ext?.actionActiveFieldName ?? session.activeField;
        }
        if (field.isEmpty) {
          return failOrCapture(102, 'Insert Current Date has no target or active field.');
        }
        final table = named.occurrence;
        // Placing the occurrence first lets the window read the type of a
        // related field from its own table.
        final ownField = await _isOwnOccurrence(table);
        final fieldType = ext?.actionFieldType(field, table: table.isEmpty ? null : table)?.toUpperCase();
        if (fieldType != null && const {'CALCULATION', 'SUMMARY', 'CONTAINER'}.contains(fieldType)) {
          return failOrCapture(201, 'Insert Current Date cannot write the date into "$field", a '
              '${fieldType.toLowerCase()} field.');
        }
        // A date field holds one date, so the date replaces it; a timestamp
        // gets it at midnight. Elsewhere Select entire contents decides.
        final value = fieldType == 'TIMESTAMP' ? '$today 00:00:00' : today;
        final replaceAll = selectAll || fieldType == 'DATE' || fieldType == 'TIMESTAMP';
        // A field of another occurrence is written in the related record.
        Future<LayoutActionResult> insertRelated() async {
          final target = (occurrence: table, field: field);
          var text = value;
          if (!replaceAll) {
            final existing = await _readField(target);
            if (existing != null) text = '$existing$value';
          }
          final written = await _writeField(target, text);
          if (written != 0) return failOrCapture(written, _writeRefusal('Insert Current Date', target, written));
          session.lastError = 0;
          return const LayoutActionResult.ok();
        }
        if (!ownField) return insertRelated();
        if (ext != null) {
          final outcome = await ext.actionInsertIntoField(
              field: field, text: value, replaceAll: replaceAll, table: table.isEmpty ? null : table);
          switch (outcome) {
            case FieldInsertOutcome.written:
              break;
            case FieldInsertOutcome.noSuchField:
              return failOrCapture(102, 'Field "$field" is not in this table.');
            case FieldInsertOutcome.relatedField:
              return insertRelated();
          }
          session.lastError = 0;
          break;
        }
        if (table.isNotEmpty) return insertRelated();
        var plain = value;
        if (!replaceAll) {
          final token = '{{$field}}';
          final existing = host.actionResolveText(token);
          if (existing != token) plain = '$existing$value';
        }
        if (!await host.actionSetField(field, plain)) {
          return failOrCapture(102, 'Field "$field" is not in this table.');
        }
        session.lastError = 0;
      case 'select_all':
        // Selects what the active field holds. With no window on screen, or
        // no active field, there is nothing to select.
        final activeField = ext?.actionActiveFieldName ?? session.activeField;
        if (ext != null && activeField.isNotEmpty) {
          await ext.actionGoToField(activeField, select: true);
        }
        session.lastError = 0;
      case 'replace_field_contents':
        // The step's field may be a field of another table occurrence; the
        // occurrence goes with it to the window, which refuses to write the
        // layout's own field of the same name instead.
        var field = params['field']?.toString().trim() ?? '';
        var table = params['field_table']?.toString().trim() ?? '';
        final qualifier = field.indexOf('::');
        if (qualifier >= 0) {
          if (table.isEmpty) table = field.substring(0, qualifier).trim();
          field = field.substring(qualifier + 2).trim();
        }
        if (field.isEmpty) {
          field = ext?.actionActiveFieldName ?? session.activeField;
        }
        if (field.isEmpty) {
          return failOrCapture(102, 'Replace Field Contents has no target field.');
        }
        if (ext == null) {
          return failOrCapture(3, 'Replace Field Contents needs a window with a found set.');
        }
        final replaceType = params['replace_type']?.toString().toLowerCase() ?? 'current_contents';
        if (boolParam('dialog')) {
          // With dialog, FileMaker asks before it rewrites the found set. Only
          // Replace goes ahead: Cancel, or closing the dialog, leaves every
          // record as it was (error 1).
          final answer = await ext.actionShowDialogEx(
            title: 'Replace Field Contents',
            message: 'Permanently replace the contents of the field "$field" in all the records '
                'of the found set?',
            buttons: const [
              {'label': 'Replace', 'commit': true},
              {'label': 'Cancel', 'commit': false},
            ],
          );
          if (answer.buttonIndex != 1) {
            session.lastError = 1;
            return const LayoutActionResult.ok();
          }
        }
        return _replaceFoundSet(
          ext: ext,
          field: field,
          table: table,
          replaceType: replaceType,
          params: params,
          vars: vars,
          context: context,
          failOrCapture: failOrCapture,
        );
      case 'set_next_serial_value':
        final field = _qualified(stepField());
        if (field.isEmpty) return failOrCapture(102, 'Set Next Serial Value has no field selected.');
        final value = params['value_is_calculation'] == true
            ? resolveValue(params['value']?.toString() ?? '', vars, callContext: context)
            : p('value');
        if (value == null) {
          return const LayoutActionResult.stopped('Set Next Serial Value calculation is not supported in this context.');
        }
        final serialHost = _autoEnterHost;
        if (serialHost == null) return failOrCapture(3, 'Set Next Serial Value is not available here.');
        final code = await serialHost.actionSetNextSerialValue(field, value.trim());
        if (code != 0) {
          return failOrCapture(code, 'Set Next Serial Value could not set the next value of "$field".');
        }
        session.lastError = 0;
      case 'relookup_field_contents':
        var field = _qualified(stepField());
        if (field.isEmpty) {
          field = ext?.actionActiveFieldName ?? session.activeField;
        }
        if (field.isEmpty) {
          return failOrCapture(102, 'Relookup Field Contents has no match field: name one or go to it first.');
        }
        final lookupHost = _autoEnterHost;
        if (lookupHost == null) return failOrCapture(3, 'Relookup Field Contents is not available here.');
        final code = await lookupHost.actionRelookupFieldContents(
            field, dialog: !boolParam('no_dialog', defaultValue: true));
        if (code != 0) {
          return failOrCapture(code, 'Relookup Field Contents could not relookup from "$field".');
        }
        session.lastError = 0;
      case 'copy':
        var target = stepField();
        if (target.field.isEmpty) {
          target = (occurrence: '', field: ext?.actionActiveFieldName ?? session.activeField);
        }
        var copied = '';
        if (target.field.isNotEmpty) {
          copied = await _readField(target) ?? '';
        }
        session.clipboard = copied;
        if (ext != null) {
          await ext.actionCopyToClipboard(copied);
        }
        session.lastError = 0;
      case 'cut':
        var target = stepField();
        if (target.field.isEmpty) {
          target = (occurrence: '', field: ext?.actionActiveFieldName ?? session.activeField);
        }
        if (target.field.isNotEmpty) {
          session.clipboard = await _readField(target) ?? '';
          if (ext != null) await ext.actionCopyToClipboard(session.clipboard);
          final written = await _writeField(target, '');
          if (written != 0 && target.occurrence.isNotEmpty) {
            return failOrCapture(written, _writeRefusal('Cut', target, written));
          }
        }
        session.lastError = 0;
      case 'paste':
        var target = stepField();
        if (target.field.isEmpty) {
          target = (occurrence: '', field: ext?.actionActiveFieldName ?? session.activeField);
        }
        if (target.field.isEmpty) {
          return failOrCapture(102, 'Paste has no target or active field.');
        }
        final clip = (ext != null ? await ext.actionReadFromClipboard() : null) ?? session.clipboard;
        final written = await _writeField(target, clip);
        if (written != 0) return failOrCapture(written, _writeRefusal('Paste', target, written));
        session.lastError = 0;
      case 'clear':
        var target = stepField();
        if (target.field.isEmpty) {
          target = (occurrence: '', field: ext?.actionActiveFieldName ?? session.activeField);
        }
        if (target.field.isNotEmpty) {
          final written = await _writeField(target, '');
          if (written != 0) return failOrCapture(written, _writeRefusal('Clear', target, written));
        }
        session.lastError = 0;
      case 'export_field_contents':
        var field = params['field']?.toString().trim() ?? '';
        if (field.isEmpty) {
          field = ext?.actionActiveFieldName ?? session.activeField;
        }
        if (field.isEmpty) {
          return failOrCapture(102, 'Export Field Contents has no target field.');
        }
        if (ext != null) {
          await ext.actionExportFieldContents(field: field, path: params['path']?.toString());
        }
        session.lastError = 0;
      case 'insert_picture':
      case 'insert_file':
        final named = stepField();
        if (!await _isOwnOccurrence(named.occurrence)) {
          // A file goes into the record in hand only; a related container
          // field is not written instead of the field of the same name here.
          return failOrCapture(102, '${norm == 'insert_picture' ? 'Insert Picture' : 'Insert File'} writes '
              '"${_qualified(named)}", a field of another table occurrence, which File4Base cannot insert a '
              'file into yet. Nothing was written.');
        }
        var field = named.field;
        if (field.isEmpty) {
          field = ext?.actionActiveFieldName ?? session.activeField;
        }
        if (ext != null && field.isNotEmpty) {
          final ok = await ext.actionInsertFile(
            field: field,
            path: params['path']?.toString(),
            isPicture: norm == 'insert_picture',
          );
          if (!ok) return failOrCapture(100, 'Could not insert file into "$field".');
        }
        session.lastError = 0;
      case 'insert_from_url':
        final rawUrl = (params['url'] ?? params['expression'])?.toString() ?? '';
        final url = resolveValue(rawUrl, vars, callContext: context) ?? _resolveParamsText(rawUrl, vars);
        if (url.trim().isEmpty) {
          return failOrCapture(1631, 'Insert from URL has no URL.');
        }
        final targetVar = params['target']?.toString().trim() ?? '';
        final targetField = stepField();
        if (!hasWindow && targetVar.isEmpty) {
          return LayoutActionResult.stopped(
              'Step "$stepType" targeting a field acts on a window, and this script has no window here.');
        }
        String? fetched;
        try {
          if (ext != null) {
            fetched = await ext.actionInsertFromUrl(
              url: url,
              verifySsl: boolParam('verify_ssl', defaultValue: true),
              curlOptions: params['curl_options']?.toString(),
            );
          } else {
            final uri = Uri.tryParse(url);
            if (uri == null || !(uri.isScheme('http') || uri.isScheme('https'))) {
              return failOrCapture(1631, 'Insert from URL received invalid URL "$url".');
            }
            final resp = await http.get(uri);
            if (resp.statusCode >= 400) {
              return failOrCapture(1631, 'Insert from URL failed with HTTP ${resp.statusCode}.');
            }
            fetched = resp.body;
          }
        } catch (e) {
          return failOrCapture(1631, 'Insert from URL failed: $e');
        }
        if (fetched == null) {
          return failOrCapture(1631, 'Insert from URL could not fetch "$url".');
        }
        if (targetVar.startsWith(r'$')) {
          _setVariable(targetVar, fetched, vars);
        } else if (targetField.field.isNotEmpty) {
          final written = await _writeField(targetField, fetched);
          if (written != 0) return failOrCapture(written, _writeRefusal('Insert from URL', targetField, written));
        }
        session.lastError = 0;
      case 'send_mail':
        return _runSendMail(params, vars, context, failOrCapture);
      case 'show_dialog':
        final title = params['title_is_calculation'] == true
            ? resolveValue(params['title']?.toString() ?? '', vars, callContext: context)
            : p('title', 'Message');
        final message = params['message_is_calculation'] == true
            ? resolveValue(params['message']?.toString() ?? '', vars, callContext: context)
            : p('message');
        if (title == null || message == null) {
          return const LayoutActionResult.stopped('Dialog calculation is not supported in this context.');
        }
        final rawButtons = mapListParam('buttons');
        final rawInputs = mapListParam('input_fields');
        if (ext != null && (rawButtons != null || rawInputs != null)) {
          final resolvedButtons = rawButtons?.map((b) {
            final lbl = b['is_calculation'] == true
                ? (resolveValue(b['label']?.toString() ?? '', vars, callContext: context) ?? b['label']?.toString() ?? 'OK')
                : _resolveParamsText(b['label']?.toString() ?? 'OK', vars);
            return <String, dynamic>{'label': lbl, 'commit': b['commit'] != false};
          }).toList();
          List<Map<String, dynamic>>? resolvedInputs;
          if (rawInputs != null) {
            resolvedInputs = [];
            for (final inp in rawInputs) {
              final lbl = inp['label_is_calculation'] == true
                  ? (resolveValue(inp['label']?.toString() ?? '', vars, callContext: context) ?? '')
                  : _resolveParamsText(inp['label']?.toString() ?? '', vars);
              final raw = inp['field']?.toString() ?? '';
              // A field input is keyed by its occurrence too, so what is typed
              // goes back into that field: a related one into the related record.
              final target = raw.startsWith(r'$') ? (occurrence: '', field: raw) : fieldTarget(raw, inp['field_table']?.toString() ?? '');
              final fieldKey = _qualified(target);
              final current = raw.startsWith(r'$') ? (_lookupVariable(raw, vars) ?? '') : (await _readField(target) ?? '');
              resolvedInputs.add(<String, dynamic>{
                'field': fieldKey,
                'label': lbl,
                'value': current,
                'password': inp['password'] == true,
              });
            }
          }
          final dialogResult = await ext.actionShowDialogEx(
            title: title,
            message: message,
            buttons: resolvedButtons,
            inputFields: resolvedInputs,
          );
          session.lastMessageChoice = dialogResult.buttonIndex;
          final chosenIdx = dialogResult.buttonIndex - 1;
          // A dialog closed without a button commits nothing; neither does a
          // button that does not commit, such as Cancel.
          final shouldCommit = dialogResult.buttonIndex != ScriptDialogResult.dismissed &&
              (resolvedButtons == null ||
                  chosenIdx < 0 ||
                  chosenIdx >= resolvedButtons.length ||
                  resolvedButtons[chosenIdx]['commit'] != false);
          if (shouldCommit) {
            for (final entry in dialogResult.inputValues.entries) {
              if (entry.key.startsWith(r'$')) {
                _setVariable(entry.key, entry.value, vars);
              } else if (entry.key.isNotEmpty) {
                await _writeField(fieldTarget(entry.key), entry.value);
              }
            }
          }
        } else {
          await host.actionShowDialog(title, message);
          session.lastMessageChoice = 1;
        }
        session.lastError = 0;
      case 'open_url':
        var rawUrl = params['url']?.toString() ?? '';
        if (rawUrl.isEmpty) {
          rawUrl = params['expression']?.toString() ?? '';
        }
        final url = _resolveExpressionValue(rawUrl, vars, callContext: context);
        if (url.isEmpty) return failOrCapture(5, 'Open URL has no URL.');
        await host.actionOpenUrl(url);
        session.lastError = 0;
      case 'pause_script':
        final rawSec = params['duration_seconds']?.toString() ?? '';
        final resolvedSec = resolveValue(rawSec, vars, callContext: context) ?? rawSec;
        final seconds = num.tryParse(resolvedSec) ?? 0;
        if (seconds > 0) await Future<void>.delayed(Duration(milliseconds: (seconds * 1000).round()));
        session.lastError = 0;
      case 'perform_script':
        var name = params['script_name']?.toString() ?? '';
        if (name.isEmpty) {
          name = _extractQuoted(params['expression']?.toString() ?? '');
        }
        if (name.isEmpty && params['script_missing'] == true) {
          // FileMaker skips a call to a deleted script with error 104.
          return failOrCapture(104, _migrationNote(params, 'Perform Script names a script that no longer exists.'));
        }
        if (name.isEmpty) {
          return LayoutActionResult.stopped(_migrationNote(params, 'Perform Script has no script name.'));
        }
        final parameter = await evaluateParameter((params['script_parameter'] ?? params['parameter'])?.toString() ?? '', vars, callContext: context);
        if (parameter == null) {
          return const LayoutActionResult.stopped('Perform Script cannot evaluate its parameter.');
        }
        final reference = params['file_reference']?.toString().trim() ?? '';
        LayoutActionResult result;
        if (reference.isNotEmpty) {
          // A script in another file is resolved in the solution that file
          // became, and only there. Looking it up here would run whatever
          // local script shares the name (#107).
          result = await _performExternalScript(
            reference: reference,
            name: name,
            depth: depth + 1,
            parameter: parameter,
            session: session,
          );
        } else {
          result = await _performScript(
            name: name,
            depth: depth + 1,
            parameter: parameter,
            session: session,
          );
        }
        if (result.completed && !result.halted && result.scriptResult != null) {
          context.lastScriptResult = result.scriptResult!;
        }
        return result;
      default:
        return LayoutActionResult.stopped(
            'step "$stepType" is not supported by the client script runner yet '
            '(it needs the calculation engine / server script runner).');
    }
    return const LayoutActionResult.ok();
  }

  /// Open File: makes sure the solution the file reference [reference] is
  /// mapped to can be reached from this session.
  ///
  /// FileMaker runs the steps after Open File in the file that holds the
  /// script, and so does File4Base: the window on screen stays where it is,
  /// whether or not the step says Open hidden. What the step establishes is
  /// that the solution is installed on this server and that the user is
  /// signed in to it with an account of their own there, whose privileges
  /// then govern every later step that reaches it.
  Future<LayoutActionResult> _openFile(
    String reference,
    ScriptSessionState session,
    LayoutActionResult Function(int code, String message) failOrCapture,
  ) async {
    if (reference.isEmpty) {
      return failOrCapture(100, 'Open File names no file. Choose the file reference of the solution to open.');
    }
    FileReferenceResolution target;
    try {
      target = await apiClient.resolveFileReference(reference);
    } on ApiException catch (e) {
      if (e.statusCode == 404) {
        return failOrCapture(114, 'Open File opens the file "$reference", and this solution has no '
            'file reference of that name.');
      }
      if (e.statusCode == 409) {
        return failOrCapture(100, 'Open File opens the file "$reference", which is not mapped to a '
            'solution yet. Map the file reference to the solution that file became.');
      }
      return LayoutActionResult.stopped('Open File could not read the file reference "$reference": ${e.detail}');
    } catch (e) {
      return LayoutActionResult.stopped('Open File could not read the file reference "$reference": $e');
    }
    if (target.local) {
      // The reference names this very solution, which is open.
      session.lastError = 0;
      return const LayoutActionResult.ok();
    }

    bool installed;
    try {
      final listing = await apiClient.listDatabases();
      final names = listing['databases'];
      installed = names is List && names.any((n) => n.toString().toLowerCase() == target.database.toLowerCase());
    } catch (e) {
      return LayoutActionResult.stopped('Open File could not list the solutions installed on this server: $e');
    }
    if (!installed) {
      return failOrCapture(100, 'Open File opens the solution ${target.database} (file reference '
          '"$reference"), which is not installed on this server.');
    }

    final token = await peerTokenProvider?.call(target.database);
    if (token == null || token.isEmpty) {
      return failOrCapture(9, 'Open File opens the solution ${target.database} (file reference '
          '"$reference"), and this session is not signed in to it. Sign in to '
          '${target.database} with your account there, then run the script again.');
    }
    session.lastError = 0;
    return const LayoutActionResult.ok();
  }

  /// Performs the script [name] of the solution the file reference
  /// [reference] is mapped to (#107).
  ///
  /// Every way this can fail stops the script and says which it was. None of
  /// them falls back to a script of this solution with the same name: that is
  /// a different script, and running it is the one outcome worse than
  /// stopping.
  ///
  /// The script runs on a session the user opened in that solution, and with
  /// no window: the window on screen belongs to the calling solution. Steps
  /// that need no window — transfers, variables, If and Loop, Perform Script
  /// within that solution, Exit and Halt, dialogs — run; the first step that
  /// needs a window stops it. `Halt Script` in it stops every caller here
  /// too, exactly as within one solution (#95).
  ///
  /// `$$global` variables are one set per client session, shared by every
  /// solution a script reaches — unlike FileMaker, which keeps them per file.
  Future<LayoutActionResult> _performExternalScript({
    required String reference,
    required String name,
    required int depth,
    required String parameter,
    ScriptSessionState? session,
  }) async {
    if (depth >= maxScriptDepth) {
      return const LayoutActionResult.stopped('Perform Script is nested too deeply (possible recursion).');
    }

    FileReferenceResolution target;
    try {
      target = await apiClient.resolveFileReference(reference);
    } on ApiException catch (e) {
      if (e.statusCode == 404) {
        return LayoutActionResult.stopped(
            'Perform Script calls "$name" in the file "$reference", and this solution has no '
            'file reference of that name.');
      }
      if (e.statusCode == 409) {
        return LayoutActionResult.stopped(
            'Perform Script calls "$name" in the file "$reference", which is not mapped to a '
            'solution yet. Map the file reference to the solution that file became; until '
            'then the script cannot tell where "$name" is.');
      }
      return LayoutActionResult.stopped(
          'Perform Script could not read the file reference "$reference": ${e.detail}');
    } catch (e) {
      return LayoutActionResult.stopped(
          'Perform Script could not read the file reference "$reference": $e');
    }

    if (target.local) {
      // The reference names this very solution: an ordinary Perform Script,
      // with this solution's window.
      return _performScript(name: name, depth: depth, parameter: parameter, session: session);
    }

    final token = await peerTokenProvider?.call(target.database);
    if (token == null || token.isEmpty) {
      return LayoutActionResult.stopped(
          'Perform Script calls "$name" in the solution ${target.database} (file reference '
          '"$reference"), and this session is not signed in to it. Sign in to '
          '${target.database} and run the script again.');
    }

    final caller = this;
    final external = LayoutActionRunner(
      apiClient: apiClient.forPeerSession(token),
      host: _WindowlessHost(host),
      files: files,
      // Reaching back to the calling solution — a transfer in the called
      // script whose far end is the caller, say — uses the session the user is
      // already working in there. Any third solution is asked for as usual.
      peerTokenProvider: (solution) async {
        if (solution.toLowerCase() == target.callingDatabase.toLowerCase()) {
          return caller.apiClient.authToken;
        }
        return caller.peerTokenProvider?.call(solution);
      },
      windowSolution: windowSolution ?? target.callingDatabase,
      solutionName: target.database,
    );
    return external._performScript(name: name, depth: depth, parameter: parameter, session: session);
  }

  /// The explanation the FileMaker migration left on a step that cannot run
  /// as converted, or [fallback] when there is none.
  static String _migrationNote(Map<String, dynamic> params, String fallback) {
    final note = params['migration_note']?.toString().trim() ?? '';
    return note.isEmpty ? fallback : note;
  }

  /// The value of a Send Mail option: a calculation evaluated, a literal as
  /// written. Null for a calculation that cannot be evaluated — never sent
  /// as its own text.
  String? _mailValue(Map<String, dynamic> params, String key, Map<String, String> vars, ScriptCallContext context) {
    final raw = params[key]?.toString() ?? '';
    if (raw.isEmpty) return '';
    final value = resolveValue(raw, vars, callContext: context);
    if (value != null) return value;
    return params['${key}_is_calculation'] == true ? null : _resolveParamsText(raw, vars);
  }

  /// A path list of a step: a `$variable` entry stands for the variable's
  /// value, any other entry for itself.
  String _pathText(String raw, Map<String, String> vars, ScriptCallContext context) {
    final lines = <String>[];
    for (final line in raw.split(RegExp('[\r\n\u00b6]'))) {
      final t = line.trim();
      if (t.isEmpty) continue;
      lines.add(t.startsWith(r'$') ? (_lookupVariable(t, vars, callContext: context) ?? '') : t);
    }
    return lines.join('\n');
  }

  static const _pathModel = 'a File4Base script reads and writes files only in its temporary folder, '
      'Get ( TemporaryPath ), and its documents folder, Get ( DocumentsPath ) (also Get ( DesktopPath ))';

  /// Save Records as PDF (#133): the layout in hand, for the current record
  /// or the found set, drawn as Print draws it and written at the step's
  /// path, appended to a PDF saved earlier in this run when asked.
  Future<LayoutActionResult> _runSaveRecordsAsPdf(
    Map<String, dynamic> params,
    Map<String, String> vars,
    ScriptCallContext context,
    LayoutActionResult Function(int code, String message) failOrCapture,
    bool Function(String key, {bool defaultValue}) boolParam,
    String layoutName,
  ) async {
    final session = context.session;
    final docs = host is DocumentActionHost ? host as DocumentActionHost : null;
    if (params['save_to'] == 'variable') {
      return failOrCapture(3, _migrationNote(params, 'Save Records as PDF cannot save into a variable (error 3).'));
    }
    if (params['pdf_security'] == true) {
      // Never an unprotected PDF where the script asked for a protected one.
      return failOrCapture(3, _migrationNote(params,
          'Save Records as PDF cannot protect a PDF with a password, and does not write it unprotected (error 3).'));
    }
    if (docs == null) {
      return failOrCapture(3, 'Save Records as PDF needs a window to draw the records in (error 3).');
    }
    final paths = params['paths'] is List ? (params['paths'] as List).join('\n') : params['path']?.toString() ?? '';
    var text = _pathText(paths, vars, context);
    if (text.trim().isEmpty) {
      final name = (layoutName.isEmpty ? 'Untitled' : layoutName).replaceAll(RegExp(r'[\\/:*?"<>|]'), '_');
      text = '${files.documentsPath}$name.pdf';
    }
    final path = files.resolve(text);
    if (path == null) {
      return failOrCapture(800, 'Save Records as PDF cannot create "$text" (error 800): $_pathModel.');
    }
    final records = switch (params['records']?.toString()) {
      'current' => 'current',
      'blank' => 'blank',
      _ => 'browsed',
    };
    final drawn = await docs.actionRenderRecordsPdf(records);
    if (drawn == null) return failOrCapture(3, 'Save Records as PDF could not draw the layout (error 3).');
    if (drawn.isEmpty) return failOrCapture(401, 'Save Records as PDF has no record to save (error 401).');
    var pages = drawn;
    if (boolParam('append')) {
      final earlier = files.savedPdfPages[path];
      if (earlier != null) {
        pages = [...earlier, ...drawn];
      } else if (await files.read(path) != null) {
        return failOrCapture(3, 'Save Records as PDF can append only to a PDF saved by a script during this run '
            'of File4Base, and "$path" was not (error 3).');
      }
    }
    final Uint8List bytes;
    try {
      bytes = await assemblePdf(pages, title: layoutName);
      await files.write(path, bytes);
    } on Object catch (e) {
      return failOrCapture(800, 'Save Records as PDF could not write "$path" (error 800): $e');
    }
    files.savedPdfPages[path] = pages;
    session.lastError = 0;
    if (boolParam('auto_open')) {
      await docs.actionOpenDocument(path, files.nativePath(path), bytes);
    }
    if (boolParam('create_email')) {
      // FileMaker opens a new e-mail with the PDF attached for the user to
      // address: Send Mail with its dialog, and the file.
      return _runSendMail(const {'dialog': true}, vars, context, failOrCapture, attachedPaths: [path]);
    }
    return const LayoutActionResult.ok();
  }

  /// Send Mail (#133).
  ///
  /// With its dialog and via the e-mail client, the step opens a draft in
  /// the user's e-mail client, as FileMaker does. Everything a draft cannot
  /// do goes through the SMTP profile a server administrator set for the
  /// solution (or the server): a step sent via the SMTP server, one e-mail
  /// per record, attachments (with the dialog: in File4Base's compose
  /// window), and — a deliberate deviation — a step via the e-mail client
  /// without dialog, which FileMaker sends without asking. Without a
  /// profile, what an e-mail client can do still opens a draft (the user is
  /// told which files to attach); the rest fails with error 1506.
  Future<LayoutActionResult> _runSendMail(
    Map<String, dynamic> params,
    Map<String, String> vars,
    ScriptCallContext context,
    LayoutActionResult Function(int code, String message) failOrCapture, {
    List<String>? attachedPaths,
  }) async {
    final session = context.session;
    final docs = host is DocumentActionHost ? host as DocumentActionHost : null;
    final smtpStep = params['send_via'] == 'smtp';
    final dialog = params['dialog'] is bool ? params['dialog'] as bool : true;
    final perRecord = params['one_email_per_record'] == true;
    final collected = [for (final k in const ['to', 'cc', 'bcc']) if (params['collect_$k'] == true) k];

    ({OutgoingMail? mail, String? failed}) evaluate() {
      final parts = <String, String>{};
      for (final key in const ['to', 'cc', 'bcc', 'subject', 'message']) {
        final value = _mailValue(params, key, vars, context);
        if (value == null) return (mail: null, failed: key);
        parts[key] = value;
      }
      return (
        mail: OutgoingMail(
          to: parts['to']!, cc: parts['cc']!, bcc: parts['bcc']!,
          subject: parts['subject']!, message: parts['message']!,
        ),
        failed: null,
      );
    }

    LayoutActionResult unevaluated(String key) =>
        LayoutActionResult.stopped('Send Mail cannot evaluate its $key: ${params[key]}');

    // The files, before anything is sent or opened.
    final rawAttachments = attachedPaths ??
        [for (final e in (params['attachments'] is List ? params['attachments'] as List : const [])) e.toString()];
    final paths = <String>[];
    for (final raw in rawAttachments) {
      final text = attachedPaths != null ? raw : _pathText(raw, vars, context);
      final path = text.trim().isEmpty ? null : files.resolve(text);
      if (path == null) {
        return failOrCapture(100, 'Send Mail cannot attach "${text.isEmpty ? raw : text}" (error 100): $_pathModel.');
      }
      paths.add(path);
    }
    String fileName(String path) => path.substring(path.lastIndexOf('/') + 1);

    var messages = <OutgoingMail>[];
    if (perRecord || collected.isNotEmpty) {
      if (docs == null) {
        return failOrCapture(3, 'Send Mail goes through the records of the found set only in a window (error 3).');
      }
    }
    if (perRecord) {
      String? failed;
      final read = await docs!.actionForEachFoundRecord((position, count) async {
        final r = evaluate();
        if (r.mail == null) {
          failed = r.failed;
          return false;
        }
        if (r.mail!.hasRecipient) messages.add(r.mail!);
        return true;
      });
      if (failed != null) return unevaluated(failed!);
      if (!read) return failOrCapture(1506, 'Send Mail could not read the records of the found set (error 1506).');
    } else {
      final r = evaluate();
      if (r.mail == null) return unevaluated(r.failed!);
      var mail = r.mail!;
      if (collected.isNotEmpty) {
        final gathered = {for (final k in collected) k: <String>[]};
        String? failed;
        final read = await docs!.actionForEachFoundRecord((position, count) async {
          for (final k in collected) {
            final value = _mailValue(params, k, vars, context);
            if (value == null) {
              failed = k;
              return false;
            }
            for (final address in value.split(RegExp('[;,\r\n]'))) {
              final a = address.trim();
              if (a.isNotEmpty && !gathered[k]!.contains(a)) gathered[k]!.add(a);
            }
          }
          return true;
        });
        if (failed != null) return unevaluated(failed!);
        if (!read) return failOrCapture(1506, 'Send Mail could not read the records of the found set (error 1506).');
        mail = OutgoingMail(
          to: collected.contains('to') ? gathered['to']!.join('; ') : mail.to,
          cc: collected.contains('cc') ? gathered['cc']!.join('; ') : mail.cc,
          bcc: collected.contains('bcc') ? gathered['bcc']!.join('; ') : mail.bcc,
          subject: mail.subject,
          message: mail.message,
        );
      }
      messages.add(mail);
    }

    // A draft in the user's e-mail client, which the user sends: what a
    // dialog leads to when File4Base cannot show its own.
    Future<LayoutActionResult> draft() async {
      await _openMailDraft(messages.first);
      if (paths.isNotEmpty) {
        final list = paths.map((p) => files.nativePath(p)).join(', ');
        final notice = 'An e-mail draft cannot carry attachments: attach ${paths.length == 1 ? 'this file' : 'these files'} '
            'to the draft yourself: $list';
        if (docs != null) {
          await docs.actionNotify(notice);
        } else {
          await host.actionShowDialog('Send Mail', notice);
        }
      }
      session.lastError = 0;
      return const LayoutActionResult.ok();
    }

    // With its dialog, the user sees every message before it leaves. With
    // no window to show the compose window in, one message becomes a draft;
    // several cannot be shown, and none is sent.
    if (dialog && docs == null) {
      if (messages.length > 1 || perRecord) {
        return failOrCapture(1, 'Send Mail shows each message before sending it, and there is no window '
            'to show them in: nothing was sent (error 1).');
      }
      return draft();
    }

    final needsSmtp = smtpStep || perRecord || !dialog || paths.isNotEmpty;
    final status = needsSmtp ? await _loadMailStatus() : const MailStatus();
    if (!status.configured) {
      if (smtpStep || perRecord) {
        return failOrCapture(1506, 'Send Mail sends through SMTP, and no SMTP profile serves this solution: '
            'a server administrator sets one in Server Tools (error 1506).');
      }
      // Via the e-mail client: a draft the user sends, as with a dialog.
      return draft();
    }

    final fromName = _mailValue(params, 'smtp_from_name', vars, context);
    final from = _mailValue(params, 'smtp_from', vars, context);
    final replyTo = _mailValue(params, 'smtp_reply_to', vars, context);
    if (fromName == null) return unevaluated('smtp_from_name');
    if (from == null) return unevaluated('smtp_from');
    if (replyTo == null) return unevaluated('smtp_reply_to');

    if (dialog && docs != null) {
      // Each message in the compose window, one per record in turn; a
      // cancel sends none of them.
      final edited = <OutgoingMail>[];
      for (final m in messages) {
        final e = await docs.actionComposeMail(m, from: status.fromLabel, attachments: [for (final p in paths) fileName(p)]);
        if (e == null) {
          session.lastError = 1; // the user cancelled
          return const LayoutActionResult.ok();
        }
        edited.add(e);
      }
      messages = edited;
    }
    messages = messages.where((m) => m.hasRecipient).toList();
    if (messages.isEmpty) {
      return failOrCapture(1506, 'Send Mail has no recipient to send to (error 1506).');
    }
    final attachments = <MailAttachment>[];
    for (final path in paths) {
      final bytes = await files.read(path);
      if (bytes == null) return failOrCapture(100, 'Send Mail cannot attach "$path": there is no such file (error 100).');
      attachments.add(MailAttachment(name: fileName(path), bytes: bytes));
    }
    var sent = 0;
    try {
      for (var i = 0; i < messages.length; i += status.maxBatch) {
        final end = i + status.maxBatch < messages.length ? i + status.maxBatch : messages.length;
        sent += await apiClient.sendMail(
          messages: messages.sublist(i, end),
          attachments: attachments,
          fromName: fromName,
          from: from,
          replyTo: replyTo,
        );
      }
    } on ApiException catch (e) {
      final raw = e.raw['filemaker_error'];
      final code = raw is int && raw > 0 ? raw : 1506;
      final before = sent + (e.raw['sent'] is int ? e.raw['sent'] as int : 0);
      return failOrCapture(code, 'Send Mail: ${e.detail}${before > 0 ? ' ($before sent)' : ''} (error $code).');
    } on Object catch (e) {
      return failOrCapture(1506, 'Send Mail could not reach the server: $e (error 1506).');
    }
    session.lastError = 0;
    return const LayoutActionResult.ok();
  }

  /// Opens [mail] as a draft in the user's e-mail client.
  Future<void> _openMailDraft(OutgoingMail mail) async {
    final ext = _extHost;
    if (ext != null) {
      await ext.actionSendMail(to: mail.to, cc: mail.cc, bcc: mail.bcc, subject: mail.subject, message: mail.message);
    } else if (mail.to.isNotEmpty) {
      final uri = Uri(
        scheme: 'mailto',
        path: mail.to,
        queryParameters: <String, String>{
          if (mail.cc.isNotEmpty) 'cc': mail.cc,
          if (mail.bcc.isNotEmpty) 'bcc': mail.bcc,
          if (mail.subject.isNotEmpty) 'subject': mail.subject,
          if (mail.message.isNotEmpty) 'body': mail.message,
        },
      );
      await host.actionOpenUrl(uri.toString());
    }
  }

  /// Replace Field Contents over the whole found set. A calculation, or a
  /// serial number, is worked out for each record in turn, in found-set
  /// order: one value written to every record would be wrong for any
  /// calculation that reads a field. A rejected write stops the replace with
  /// its error, as FileMaker reports it; the records before it stay replaced.
  Future<LayoutActionResult> _replaceFoundSet({
    required ExtendedLayoutActionHost ext,
    required String field,
    required String table,
    required String replaceType,
    required Map<String, dynamic> params,
    required Map<String, String> vars,
    required ScriptCallContext context,
    required LayoutActionResult Function(int code, String message) failOrCapture,
  }) async {
    final session = context.session;
    final rawExpression = params['value'] ?? params['expression'];
    final expression = rawExpression?.toString() ?? '';
    FutureOr<String?> Function(int position, int count)? valueFor;
    // Why the server could not evaluate the calculation, when it was asked.
    var serverError = '';
    if (replaceType == 'serial_numbers') {
      final initial = resolveValue(params['serial_initial']?.toString() ?? '', vars, callContext: context);
      var serial = num.tryParse(initial?.trim() ?? '');
      if (serial == null) {
        return LayoutActionResult.stopped(
            'Replace Field Contents numbers the records from an initial value it cannot read: '
            '${params['serial_initial'] ?? '(none)'}');
      }
      num increment = 1;
      final rawIncrement = params['serial_increment']?.toString() ?? '';
      if (rawIncrement.trim().isNotEmpty) {
        final parsed = num.tryParse(resolveValue(rawIncrement, vars, callContext: context)?.trim() ?? '');
        if (parsed == null) {
          return LayoutActionResult.stopped(
              'Replace Field Contents cannot read the serial increment "$rawIncrement".');
        }
        increment = parsed;
      }
      valueFor = (position, count) {
        final value = _formatSerial(serial!);
        serial = serial! + increment;
        return value;
      };
    } else if (replaceType == 'calculation' || rawExpression != null) {
      if (expression.trim().isEmpty) {
        return failOrCapture(3, 'Replace Field Contents has no calculation.');
      }
      // Each record is the current one while its value is worked out: here
      // when this runner can, otherwise by the server in that record's
      // context, as every other step's formula.
      valueFor = (position, count) async {
        final local = resolveValue(expression, vars, callContext: context);
        if (local != null) return local;
        try {
          final evaluation = await _evaluateOnServer(expression, vars, callContext: context);
          if (evaluation == null) return null;
          if (evaluation.error.isNotEmpty) {
            serverError = evaluation.error;
            return null;
          }
          if (evaluation.unknownFields.isNotEmpty) {
            serverError = 'it reads fields this table does not have: ${evaluation.unknownFields.join(', ')}';
            return null;
          }
          _mergeVariables(evaluation.variables, vars);
          return evaluation.text;
        } on Object catch (e) {
          serverError = '$e';
          return null;
        }
      };
    }
    final outcome = await ext.actionReplaceFieldContents(
      field: field,
      table: table.isEmpty ? null : table,
      valueFor: valueFor,
    );
    final target = table.isEmpty ? field : '$table::$field';
    if (outcome.relatedField) {
      return failOrCapture(102, 'Replace Field Contents writes "$target", a field of another table '
          "occurrence than this layout's, which it cannot write yet. Nothing was written.");
    }
    if (outcome.unevaluatedAt > 0) {
      final why = serverError.isEmpty ? '' : ': $serverError';
      return LayoutActionResult.stopped(
          'Replace Field Contents cannot evaluate "$expression" for record ${outcome.unevaluatedAt} of '
          '${outcome.count}$why; ${_recordsReplaced(outcome.replaced)}.');
    }
    if (outcome.errorCode != 0) {
      final where = outcome.count > 0 ? ' at record ${outcome.replaced + 1} of ${outcome.count}' : '';
      final why = outcome.detail.isEmpty ? '' : ': ${outcome.detail}';
      return failOrCapture(outcome.errorCode,
          'Replace Field Contents of "$target" stopped$where$why; ${_recordsReplaced(outcome.replaced)}.');
    }
    session.lastError = 0;
    return const LayoutActionResult.ok();
  }

  static String _recordsReplaced(int n) => switch (n) {
        0 => 'no record was replaced',
        1 => 'the record before it was replaced',
        _ => 'the $n records before it were replaced',
      };

  static String _formatSerial(num value) =>
      value == value.truncate() ? value.truncate().toString() : value.toString();

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
  ///
  /// An `Import Records` step works on the window's found set when the
  /// window shows the transfer's destination table: the records a
  /// "replace_found_set" transfer replaces in order, and the only ones an
  /// "update_matching" transfer updates. The records it added or updated are
  /// then the window's found set, as in FileMaker (#191). With dialog, it
  /// asks first; cancelling sets error 1 and the script goes on.
  ///
  /// A record the destination's validation skipped does not undo the others,
  /// and the script goes on, as after FileMaker's Import Records: it sets
  /// error 729, "Errors occurred during import; records could not be
  /// imported" (FileMaker error codes), and without Set Error Capture the
  /// user is shown the import summary (#231). Values left out of records
  /// that were imported set no error.
  Future<LayoutActionResult> _execTransfer(
    String norm,
    Map<String, dynamic> params,
    Map<String, String> localVars,
    ScriptSessionState session,
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
    final note = params['migration_note']?.toString() ?? '';
    if (name.isEmpty && note.isNotEmpty) {
      return LayoutActionResult.stopped('$label has no transfer named in it. This step, converted from '
          'FileMaker, $note');
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

    final importHost =
        norm == 'import_records' && host is ImportRecordsHost ? host as ImportRecordsHost : null;
    final windowTable = importHost?.actionImportTable ?? '';
    final inWindow = importHost != null &&
        windowTable.isNotEmpty &&
        windowTable.toLowerCase() == definition.destination.table.toLowerCase();

    // FileMaker imports into the table of the window's layout, and replaces
    // or updates that window's found set. Without a window on the
    // destination table there is no such found set: replacing never widens
    // to the whole table, and neither does a step converted from FileMaker
    // that updates. A transfer designed here keeps its own contract, where
    // updating without a window works on every record.
    final source = params['import_source'];
    final converted = source is Map && source['kind'] == 'filemaker';
    if (norm == 'import_records' &&
        definition.usesDestinationFoundSet &&
        !inWindow &&
        (definition.mode == TransferMode.replaceFoundSet || converted)) {
      final shown = windowTable.isEmpty ? 'no table of this solution' : windowTable;
      return LayoutActionResult.stopped(
          '$label "$name" ${definition.mode == TransferMode.replaceFoundSet ? 'replaces' : 'updates'} '
          'the found set of ${definition.destination.table}, and the window it runs in shows $shown. '
          'Go to a layout of ${definition.destination.table} first, as FileMaker imports into the '
          "current layout's table.");
    }

    final dialog = params['dialog'];
    final withDialog = dialog == true || dialog?.toString().toLowerCase() == 'true';
    if (withDialog && _extHost == null) {
      // A script performed without a window of its own cannot ask, and a
      // transfer the user was to confirm is not run unconfirmed.
      return LayoutActionResult.stopped(
          '$label "$name" asks for confirmation (With dialog), and this script has no window to ask in.');
    }
    if (importHost != null && !await importHost.actionCommitBeforeImport()) {
      return LayoutActionResult.stopped('$label "$name": the record being edited could not be committed.');
    }
    if (withDialog) {
      final answer = await _extHost!.actionShowDialogEx(
        title: label,
        message: 'Run the transfer "$name"? It reads ${definition.source.label} and writes '
            '${definition.destination.label}.',
        buttons: [
          {'label': norm == 'export_records' ? 'Export' : 'Import', 'commit': true},
          const {'label': 'Cancel', 'commit': false},
        ],
      );
      if (answer.buttonIndex != 1) {
        session.lastError = 1; // the user cancelled, as FileMaker answers it
        return const LayoutActionResult.ok();
      }
    }

    List<String>? foundSet;
    if (inWindow && definition.usesDestinationFoundSet) {
      foundSet = await importHost.actionFoundSetIds();
      if (foundSet == null) {
        return LayoutActionResult.stopped(
            '$label "$name" works on the found set of ${definition.destination.table}, '
            'which could not be read.');
      }
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
      report = await apiClient.runTransfer(
        definition.id,
        peerToken: peerToken,
        destinationFoundSet: foundSet,
        returnRecordIds: inWindow,
      );
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
    if (inWindow && !await importHost.actionShowImportedRecords(report.recordIds)) {
      // The records were written, but the window still shows the found set
      // from before: a step after this one would act on the wrong records.
      return LayoutActionResult.stopped('$label "$name" wrote ${report.added + report.updated} record(s), '
          'which could not be shown as the found set.');
    }
    final problems = report.skippedErrors > 0 || report.fieldsSkipped > 0;
    if ((report.unpaired.isNotEmpty || problems && !session.errorCapture) && host is DocumentActionHost) {
      // FileMaker imports the fields whose names match and goes on; the
      // user is told which ones were left out, and what was skipped.
      // Set Error Capture keeps what was skipped to the script.
      final first = session.errorCapture ? const <String>[] : report.rejected.take(3).map((r) => r.describe());
      await (host as DocumentActionHost)
          .actionNotify('${['$label "$name": ${report.describeCounts()}', ...first].join('; ')}.');
    }
    if (report.skippedErrors > 0) {
      session.lastError = 729; // set last: notifying the user is not a step
    }
    return const LayoutActionResult.ok();
  }

  /// The condition of an `If` / `Else If` / `Exit Loop If` step.
  String extractCondition(Map<String, dynamic> params) {
    final c = params['condition']?.toString().trim() ?? '';
    if (c.isNotEmpty) return c;
    return params['expression']?.toString().trim() ?? '';
  }

  LayoutActionResult _execSetVariable(Map<String, dynamic> params, Map<String, String> localVars, {ScriptCallContext? callContext}) {
    var name = (params['name'] ?? params['variable'])?.toString().trim() ?? '';
    var valExpr = (params['value'] ?? params['calc'] ?? params['formula'] ?? params['text'])?.toString().trim() ?? '';
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
    var repetition = 1;
    final rawRep = params['repetition']?.toString().trim() ?? '';
    if (rawRep.isNotEmpty) {
      final evalRep = resolveValue(rawRep, localVars, callContext: callContext);
      final parsedRep = int.tryParse(evalRep?.trim() ?? '');
      if (parsedRep == null || parsedRep < 1) {
        return LayoutActionResult.stopped('Set Variable cannot evaluate repetition "$rawRep".');
      }
      repetition = parsedRep;
    }
    final resolved = resolveValue(valExpr, localVars, callContext: callContext);
    if (resolved == null) {
      // Guessing an unreadable value as empty wrote an empty variable and let
      // the script carry on as though it had a value (#93).
      return LayoutActionResult.stopped(
          'Set Variable cannot evaluate "$valExpr": File4Base reads a number, '
          r'a quoted string, a $variable, a merge token or a field name.');
    }
    _setVariable(name, resolved, localVars, repetition: repetition);
    return const LayoutActionResult.ok();
  }

  String _variableStorageKey(String name, int repetition) {
    final base = name.trim().toLowerCase();
    return repetition <= 1 ? base : '$base[$repetition]';
  }

  void _setVariable(String name, String value, Map<String, String> localVars, {int repetition = 1}) {
    final key = _variableStorageKey(name, repetition);
    if (key.startsWith(r'$$')) {
      globalVariables[key] = value;
    } else {
      localVars[key] = value;
    }
  }

  String? _lookupVariable(String name, Map<String, String> localVars, {ScriptCallContext? callContext}) {
    final trimmed = name.trim();
    final repMatch = RegExp(r'^(\$+[A-Za-z_][A-Za-z0-9_]*)\s*\[\s*(.+)\s*\]$').firstMatch(trimmed);
    if (repMatch != null) {
      final base = repMatch.group(1)!.toLowerCase();
      final repExpr = repMatch.group(2)!.trim();
      final evalRep = resolveValue(repExpr, localVars, callContext: callContext) ?? repExpr;
      final rep = int.tryParse(evalRep.trim()) ?? 1;
      final key = _variableStorageKey(base, rep);
      return key.startsWith(r'$$') ? globalVariables[key] : localVars[key];
    }
    final key = trimmed.toLowerCase();
    if (key.startsWith(r'$$')) {
      return globalVariables[key];
    }
    return localVars[key];
  }

  String _resolveParamsText(String raw, Map<String, String> localVars) {
    final trimmed = raw.trim();
    if (trimmed.startsWith(r'$')) {
      return _lookupVariable(trimmed, localVars) ?? '';
    }
    return host.actionResolveText(raw);
  }

  /// A script parameter's value: evaluated here when it can be, otherwise —
  /// a related field, a custom function, any other function — by the server
  /// in the record's context, with the script's variables. Null when neither
  /// can evaluate it.
  Future<String?> evaluateParameter(String raw, Map<String, String> localVars, {ScriptCallContext? callContext}) async {
    final local = resolveValue(raw, localVars, callContext: callContext);
    if (local != null) return local;
    try {
      final evaluation = await _evaluateOnServer(raw, localVars, callContext: callContext);
      if (evaluation != null && evaluation.error.isEmpty && evaluation.unknownFields.isEmpty) {
        _mergeVariables(evaluation.variables, localVars);
        return evaluation.text;
      }
    } on Object catch (_) {
      // Not evaluable: the caller says so.
    }
    return null;
  }

  /// Answers of the server, by formula text, to the formulas of the step
  /// being run that this runner cannot evaluate itself. [prepareStepFormulas]
  /// fills it before the step runs; it is cleared once the step has run.
  final Map<String, String> _serverAnswers = <String, String>{};

  /// The Get functions only the client can answer, sent with a formula the
  /// server evaluates for a script.
  static const _clientGetSelectors = <String>[
    'ScriptParameter',
    'ScriptResult',
    'ScriptName',
    'LastError',
    'LastMessageChoice',
    'LayoutName',
    'WindowName',
    'ActiveFieldName',
    'ActiveFieldTableName',
    'ActiveLayoutObjectName',
    'ActivePortalRowNumber',
    'LayoutTableName',
    'FoundCount',
    'RecordNumber',
    'TotalRecordCount',
    'AccountName',
  ];

  /// [formula] evaluated by the server with the script's variables and the
  /// client's Get answers, in one request. Null when the host has no table
  /// to evaluate it in.
  Future<EvaluationModel?> _evaluateOnServer(
    String formula,
    Map<String, String> localVars, {
    ScriptCallContext? callContext,
  }) async {
    final context = host is CalculationContextHost ? host as CalculationContextHost : null;
    if (context == null || formula.trim().isEmpty) return null;
    final variables = <String, String>{
      for (final e in globalVariables.entries)
        if (!e.key.contains('[')) e.key: e.value,
      for (final e in localVars.entries)
        if (!e.key.contains('[')) e.key: e.value,
    };
    final get = <String, Object?>{
      for (final selector in _clientGetSelectors)
        selector: resolveValue('Get ( $selector )', localVars, callContext: callContext) ?? '',
    };
    return context.actionEvaluateInScript(formula, variables: variables, get: get);
  }

  /// Keeps the variables a formula the server evaluated set with Let.
  void _mergeVariables(Map<String, String> variables, Map<String, String> localVars) {
    for (final e in variables.entries) {
      _setVariable(e.key, e.value, localVars);
    }
  }

  /// The formulas of a step: the ones its options say are calculations,
  /// and the ones that are always calculations for that step.
  List<({String formula, bool condition})> stepFormulas(String stepType, Map<String, dynamic> params) {
    final norm = normalizeStepType(stepType);
    final out = <({String formula, bool condition})>[];
    void add(Object? raw, {bool condition = false}) {
      final formula = raw?.toString().trim() ?? '';
      if (formula.isNotEmpty && !out.any((f) => f.formula == formula && f.condition == condition)) {
        out.add((formula: formula, condition: condition));
      }
    }

    switch (norm) {
      case 'if':
      case 'else_if':
      case 'exit_loop_if':
        add(extractCondition(params), condition: true);
      case 'exit_script':
        add(params['result'] ?? params['expression']);
      case 'set_variable':
        final name = (params['name'] ?? params['variable'])?.toString().trim() ?? '';
        if (name.isNotEmpty) add(params['value'] ?? params['calc'] ?? params['formula'] ?? params['text']);
      case 'insert_calculated_result':
        add(params['value'] ?? params['expression']);
      case 'go_to_layout':
        add(params['layout_calculation'] ?? params['expression']);
      case 'replace_field_contents':
        // Evaluated for each record of the found set, with that record in
        // hand (see _replaceFoundSet), not once before the step.
        return out;
    }
    add(params['record_calculation']);
    for (final key in params.keys) {
      if (key.endsWith('_is_calculation') && params[key] == true) {
        add(params[key.substring(0, key.length - '_is_calculation'.length)]);
      }
    }
    return out;
  }

  /// Has the server evaluate the formulas of the step about to run that
  /// this runner cannot, each in one request with the script's variables,
  /// so the step finds their values. Returns why the step must stop — the
  /// step and the formula named — or null.
  Future<String?> prepareStepFormulas(
    String stepType,
    Map<String, dynamic> params,
    Map<String, String> localVars, {
    ScriptCallContext? callContext,
  }) async {
    _serverAnswers.clear();
    if (host is! CalculationContextHost) return null;
    for (final f in stepFormulas(stepType, params)) {
      final local = f.condition
          ? evaluateCondition(f.formula, localVars, callContext: callContext) != null
          : resolveValue(f.formula, localVars, callContext: callContext) != null;
      if (local) continue;
      EvaluationModel? evaluation;
      try {
        evaluation = await _evaluateOnServer(f.formula, localVars, callContext: callContext);
      } on Object catch (e) {
        return '$stepType: the formula "${f.formula}" could not be evaluated: $e';
      }
      if (evaluation == null) return null;
      if (evaluation.error.isNotEmpty) {
        return '$stepType: the formula "${f.formula}" could not be evaluated: ${evaluation.error}';
      }
      if (evaluation.unknownFields.isNotEmpty) {
        return '$stepType: the formula "${f.formula}" reads fields this table does not have: '
            '${evaluation.unknownFields.join(', ')}';
      }
      _mergeVariables(evaluation.variables, localVars);
      _serverAnswers[f.formula] = evaluation.text;
    }
    return null;
  }

  /// Forgets the answers of the step that just ran.
  void clearStepFormulas() => _serverAnswers.clear();

  String _resolveExpressionValue(String raw, Map<String, String> localVars, {ScriptCallContext? callContext}) =>
      resolveValue(raw, localVars, callContext: callContext) ?? '';

  /// The value of one expression, or null when it is not an expression this
  /// runner can evaluate.
  ///
  /// It reads a number as the number it is, a quoted string, a `$variable`, a
  /// `{{merge}}` token, `Get(LayoutName)`, and a qualified or unqualified
  /// field reference. Anything else — arithmetic, a function call, a field
  /// this layout does not have — is null, so the caller stops the script
  /// rather than treating it as an empty field: an empty string made
  /// `1 = 0` true and wrote empty variables (#93).
  String? resolveValue(String raw, Map<String, String> localVars, {ScriptCallContext? callContext}) {
    final expr = raw.trim();
    if (expr.isEmpty) return '';
    final answer = _serverAnswers[expr];
    if (answer != null) return answer;
    if (expr.startsWith('(') && expr.endsWith(')') && _hasMatchingOuterParens(expr)) {
      return resolveValue(expr.substring(1, expr.length - 1), localVars, callContext: callContext);
    }
    if ((expr.startsWith('"') && expr.endsWith('"') && expr.length >= 2 && _isSingleQuotedLiteral(expr, '"')) ||
        (expr.startsWith("'") && expr.endsWith("'") && expr.length >= 2 && _isSingleQuotedLiteral(expr, "'"))) {
      final inner = expr.substring(1, expr.length - 1).replaceAll(r'\"', '"').replaceAll(r'\\', r'\');
      return host.actionResolveText(inner);
    }

    // String concatenation (`&`) at top level (outside quotes and parens).
    final concatParts = _splitTopLevel(expr, '&');
    if (concatParts.length > 1) {
      final buf = StringBuffer();
      for (final part in concatParts) {
        final v = resolveValue(part, localVars, callContext: callContext);
        if (v == null) return null;
        buf.write(v);
      }
      return buf.toString();
    }

    // Basic binary arithmetic (`+`, `-`, `*`, `/`) at top level.
    final arith = _evaluateTopLevelArithmetic(expr, localVars, callContext: callContext);
    if (arith.matched) return arith.value;

    // Built-in FileMaker Get(...) status functions (#133).
    final getMatch = RegExp(r'^get\s*\(\s*([a-z0-9_]+)\s*\)$', caseSensitive: false).firstMatch(expr);
    if (getMatch != null) {
      final fn = getMatch.group(1)!.toLowerCase();
      final session = callContext?.session ?? _defaultSession;
      final ext = _extHost;
      switch (fn) {
        case 'scriptparameter':
          return callContext?.parameter ?? '';
        case 'scriptresult':
          return callContext?.lastScriptResult ?? '';
        case 'lasterror':
          return session.lastError.toString();
        case 'lastmessagechoice':
          return session.lastMessageChoice.toString();
        case 'layoutname':
          final ln = host.actionResolveText('{{LayoutName}}');
          return ln == '{{LayoutName}}' ? '' : ln;
        case 'windowname':
          return session.currentWindowName;
        case 'scriptname':
          return callContext?.scriptName ?? '';
        case 'activefieldname':
          return ext?.actionActiveFieldName ?? session.activeField;
        case 'activelayoutobjectname':
          return session.activeObject;
        case 'activeportalrownumber':
          return host is PortalRowHost ? (host as PortalRowHost).actionActivePortalRowNumber.toString() : '0';
        case 'activefieldtablename':
          return host is PortalRowHost ? (host as PortalRowHost).actionActiveFieldTableName : '';
        case 'layouttablename':
          return host is PortalRowHost ? (host as PortalRowHost).actionLayoutTableName : '';
        case 'foundcount':
          return (ext?.actionFoundCount ?? 0).toString();
        case 'recordnumber':
          return (ext?.actionCurrentRecordNumber ?? 0).toString();
        case 'totalrecordcount':
          return (ext?.actionTotalRecordCount ?? 0).toString();
        case 'accountname':
          final acc = host.actionResolveText('{{AccountName}}');
          return acc == '{{AccountName}}' ? '' : acc;
        // The folders a script may write in (#133): see ScriptFileStore.
        case 'temporarypath':
          return files.temporaryPath;
        case 'documentspath':
        case 'desktoppath':
          return files.documentsPath;
      }
    }

    // A numeric literal is its own value, not the name of a field.
    if (num.tryParse(expr) != null) return expr;
    // FileMaker's Boolean constants, as in `Exit Script [Result: False]`.
    if (expr.toLowerCase() == 'true') return '1';
    if (expr.toLowerCase() == 'false') return '0';

    if (expr.startsWith(r'$')) {
      return _lookupVariable(expr, localVars, callContext: callContext) ?? '';
    }
    if (expr.contains('{{')) {
      return host.actionResolveText(expr);
    }
    // Qualified or unqualified field reference (e.g. `recipes::URL` or `URL`).
    // A field of another occurrence is a related field: it is not the field
    // of this record that has the same name, so it is not read here (#26).
    if (expr.contains('::')) {
      final occurrence = expr.split('::').first.trim().toLowerCase();
      final context = host is CalculationContextHost ? host as CalculationContextHost : null;
      if (context == null || !context.actionOwnOccurrenceNames.contains(occurrence)) return null;
    }
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

  bool _hasMatchingOuterParens(String expr) {
    var depth = 0;
    var inQuote = '';
    for (var i = 0; i < expr.length; i++) {
      final ch = expr[i];
      if (inQuote.isNotEmpty) {
        if (ch == r'\' && i + 1 < expr.length) {
          i++;
          continue;
        }
        if (ch == inQuote) inQuote = '';
        continue;
      }
      if (ch == '"' || ch == "'") {
        inQuote = ch;
      } else if (ch == '(') {
        depth++;
      } else if (ch == ')') {
        depth--;
        if (depth == 0 && i < expr.length - 1) return false;
      }
    }
    return depth == 0;
  }

  bool _isSingleQuotedLiteral(String expr, String quote) {
    for (var i = 1; i < expr.length - 1; i++) {
      final ch = expr[i];
      if (ch == r'\' && i + 1 < expr.length - 1) {
        i++;
        continue;
      }
      if (ch == quote) return false;
    }
    return true;
  }

  List<String> _splitTopLevel(String expr, String sep) {
    final parts = <String>[];
    var depth = 0;
    var inQuote = '';
    var start = 0;
    for (var i = 0; i < expr.length; i++) {
      final ch = expr[i];
      if (inQuote.isNotEmpty) {
        if (ch == r'\' && i + 1 < expr.length) {
          i++;
          continue;
        }
        if (ch == inQuote) inQuote = '';
        continue;
      }
      if (ch == '"' || ch == "'") {
        inQuote = ch;
      } else if (ch == '(' || ch == '[') {
        depth++;
      } else if (ch == ')' || ch == ']') {
        depth--;
      } else if (depth == 0 && ch == sep) {
        parts.add(expr.substring(start, i));
        start = i + 1;
      }
    }
    parts.add(expr.substring(start));
    return parts;
  }

  ({bool matched, String? value}) _evaluateTopLevelArithmetic(
    String expr,
    Map<String, String> localVars, {
    ScriptCallContext? callContext,
  }) {
    // Scan right-to-left for lowest precedence (+, -) then (*, /).
    for (final ops in const [<String>['+', '-'], <String>['*', '/']]) {
      var depth = 0;
      var inQuote = '';
      for (var i = expr.length - 1; i >= 1; i--) {
        final ch = expr[i];
        if (inQuote.isNotEmpty) {
          if (ch == inQuote && (i == 0 || expr[i - 1] != r'\')) inQuote = '';
          continue;
        }
        if (ch == '"' || ch == "'") {
          inQuote = ch;
        } else if (ch == ')' || ch == ']') {
          depth++;
        } else if (ch == '(' || ch == '[') {
          depth--;
        } else if (depth == 0 && ops.contains(ch)) {
          final leftStr = expr.substring(0, i).trim();
          final rightStr = expr.substring(i + 1).trim();
          if (leftStr.isEmpty || rightStr.isEmpty) continue;
          // Ignore unary sign right after another operator.
          final lastLeft = leftStr[leftStr.length - 1];
          if ('+-*/(&,='.contains(lastLeft)) continue;
          final leftVal = resolveValue(leftStr, localVars, callContext: callContext);
          final rightVal = resolveValue(rightStr, localVars, callContext: callContext);
          if (leftVal == null || rightVal == null) return (matched: true, value: null);
          final l = num.tryParse(leftVal.trim());
          final r = num.tryParse(rightVal.trim());
          if (l == null || r == null) return (matched: true, value: null);
          if (ch == '/' && r == 0) return (matched: true, value: null);
          final res = switch (ch) {
            '+' => l + r,
            '-' => l - r,
            '*' => l * r,
            '/' => l / r,
            _ => null,
          };
          if (res == null) return (matched: true, value: null);
          final formatted = (res is double && res == res.truncateToDouble())
              ? res.toInt().toString()
              : res.toString();
          return (matched: true, value: formatted);
        }
      }
    }
    return (matched: false, value: null);
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
  bool? evaluateCondition(String rawExpr, Map<String, String> localVars, {ScriptCallContext? callContext}) {
    var expr = rawExpr.trim();
    if (expr.isEmpty) return true;
    final answer = _serverAnswers[expr];
    if (answer != null) {
      // A condition is true when it comes to a number other than zero, as
      // in FileMaker.
      final n = num.tryParse(answer.trim());
      return n != null && n != 0;
    }
    while (expr.startsWith('(') && expr.endsWith(')')) {
      expr = expr.substring(1, expr.length - 1).trim();
    }
    final orParts = expr.split(RegExp(r'\s+or\s+', caseSensitive: false));
    if (orParts.length > 1) {
      var answer = false;
      for (final part in orParts) {
        final one = evaluateCondition(part, localVars, callContext: callContext);
        if (one == null) return null;
        answer = answer || one;
      }
      return answer;
    }
    final andParts = expr.split(RegExp(r'\s+and\s+', caseSensitive: false));
    if (andParts.length > 1) {
      var answer = true;
      for (final part in andParts) {
        final one = evaluateCondition(part, localVars, callContext: callContext);
        if (one == null) return null;
        answer = answer && one;
      }
      return answer;
    }
    if (expr.toLowerCase().startsWith('not ')) {
      final inner = evaluateCondition(expr.substring(4), localVars, callContext: callContext);
      return inner == null ? null : !inner;
    }
    if (expr.startsWith('!') && !expr.startsWith('!=')) {
      final inner = evaluateCondition(expr.substring(1), localVars, callContext: callContext);
      return inner == null ? null : !inner;
    }
    final isEmptyMatch = RegExp(r'^isempty\s*\(\s*(.+)\s*\)$', caseSensitive: false).firstMatch(expr);
    if (isEmptyMatch != null) {
      final inner = isEmptyMatch.group(1)!;
      // A field this context does not have is not an empty field, so the
      // question cannot be answered.
      final value = resolveValue(inner, localVars, callContext: callContext);
      return value?.trim().isEmpty;
    }

    // Two-character operators are looked for first, so `<=` is never read as
    // `<` or as `=`, which is how ordering comparisons went unevaluated.
    for (final op in <String>['<=', '>=', '!=', '<>', '≠', '≤', '≥', '=', '<', '>']) {
      final idx = expr.indexOf(op);
      if (idx <= 0) continue;
      final left = resolveValue(expr.substring(0, idx), localVars, callContext: callContext);
      final right = resolveValue(expr.substring(idx + op.length), localVars, callContext: callContext);
      if (left == null || right == null) return null;
      return _compareValues(op, left, right);
    }

    final value = resolveValue(expr, localVars, callContext: callContext);
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

  ScriptExecution({
    required this.script,
    required this.runner,
    this.depth = 0,
    Map<String, String>? localVars,
    String parameter = '',
    ScriptSessionState? session,
  })  : steps = List<ScriptStepModel>.from(script.steps)
          ..sort((a, b) => a.sequenceIdx.compareTo(b.sequenceIdx)),
        callContext = ScriptCallContext(
          parameter: parameter,
          scriptName: script.name,
          originalLayout: _initialLayoutOf(runner),
          session: session ?? ScriptSessionState(),
        ),
        localVars = localVars ?? <String, String>{} {
    _hasEndIf = steps.any((s) => s.isEnabled && LayoutActionRunner.normalizeStepType(s.stepType) == 'end_if');
    _hasEndLoop = steps.any((s) => s.isEnabled && LayoutActionRunner.normalizeStepType(s.stepType) == 'end_loop');
    callContext.session.ensureCurrentWindow(callContext.originalLayout);
  }

  static String _initialLayoutOf(LayoutActionRunner runner) {
    final ln = runner.host.actionResolveText('{{LayoutName}}');
    return ln == '{{LayoutName}}' ? '' : ln;
  }
  final ScriptModel script;
  final LayoutActionRunner runner;
  final int depth;

  /// The script's steps in the order they run.
  final List<ScriptStepModel> steps;

  /// The script's `$local` variables, shared by every step of this run.
  final Map<String, String> localVars;
  final ScriptCallContext callContext;
  String? _returnValue;
  String? get returnValue => _returnValue;

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
            final (failure, condition) = await _condition(step);
            if (failure != null) return StepOutcome(StepOutcomeKind.stopped, failure);
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
              final (failure, condition) = await _condition(step);
              if (failure != null) return StepOutcome(StepOutcomeKind.stopped, failure);
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
          final (failure, condition) = await _condition(step);
          if (failure != null) return StepOutcome(StepOutcomeKind.stopped, failure);
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
    // The formulas the runner cannot evaluate itself are evaluated by the
    // server, with the solution's custom functions, before the step runs.
    final failure = await runner.prepareStepFormulas(step.stepType, step.params, localVars, callContext: callContext);
    if (failure != null) return StepOutcome(StepOutcomeKind.stopped, failure);
    try {
      return await _runPrepared(step, norm);
    } finally {
      runner.clearStepFormulas();
    }
  }

  /// The condition of an If, Else If or Exit Loop If step, evaluated here or
  /// by the server: why the step must stop, or the condition (null when it
  /// cannot be evaluated).
  Future<(String?, bool?)> _condition(ScriptStepModel step) async {
    final failure = await runner.prepareStepFormulas(step.stepType, step.params, localVars, callContext: callContext);
    if (failure != null) return (failure, null);
    try {
      return (null, runner.evaluateCondition(runner.extractCondition(step.params), localVars, callContext: callContext));
    } finally {
      runner.clearStepFormulas();
    }
  }

  /// Runs a step whose formulas [LayoutActionRunner.prepareStepFormulas]
  /// has prepared.
  Future<StepOutcome> _runPrepared(ScriptStepModel step, String norm) async {
    if (norm == 'exit_script') {
      if (step.params.containsKey('result') || step.params.containsKey('expression')) {
        final expression = (step.params['result'] ?? step.params['expression'])?.toString() ?? '';
        final value = runner.resolveValue(expression, localVars, callContext: callContext);
        if (value == null) {
          return const StepOutcome(StepOutcomeKind.stopped, 'Exit Script cannot evaluate its result.');
        }
        _returnValue = value;
      }
      _pc = steps.length;
      return const StepOutcome(StepOutcomeKind.exited);
    }

    // `_pc` is already past this step, so it is the step's 1-based line.
    final line = _pc;
    final session = callContext.session..errorRaised = false;
    final result = await runner.runStep(step.stepType, step.params, depth: depth, localVars: localVars, callContext: callContext);
    if (session.errorRaised && runner.errorLogging.enabled) _logError(step, line);
    // A Perform Script step's own errors are logged by the script it ran;
    // clearing the flag keeps its caller from logging them a second time.
    session.errorRaised = false;
    if (result.halted) return const StepOutcome(StepOutcomeKind.halted);
    if (!result.completed) return StepOutcome(StepOutcomeKind.stopped, result.message);
    if (result.exitLoop && _loopStack.isNotEmpty) {
      return _leaveLoop();
    }
    return const StepOutcome(StepOutcomeKind.ran);
  }

  /// Records the error the step at [line] just reported (`Set Error Logging`).
  void _logError(ScriptStepModel step, int line) {
    final logging = runner.errorLogging;
    final expression = logging.customInfo.trim();
    // FileMaker writes "?" for a debug calculation it cannot evaluate.
    final info = expression.isEmpty
        ? ''
        : (runner.resolveValue(expression, localVars, callContext: callContext) ?? '?');
    logging.add(ScriptErrorLogEntry(
      time: DateTime.now(),
      scriptName: script.name,
      stepName: step.stepType,
      line: line,
      errorCode: callContext.session.lastError,
      customInfo: info,
    ));
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

/// The host of a script performed in another solution (#107): there is no
/// window of that solution on screen, so nothing here touches one.
///
/// The runner never calls the window operations on it — it stops on those
/// steps first ([LayoutActionRunner.windowSteps]) — so reaching one is a bug,
/// and it says so rather than acting on the calling solution's window.
/// Dialogs and URLs are not tied to a window and go to the real host. A merge
/// token naming a field is left unresolved, because the record on screen is
/// the calling solution's, not this one's; an expression that reads a field
/// therefore stops the script instead of reading the wrong record.
class _WindowlessHost implements LayoutActionHost {
  _WindowlessHost(this._screen);

  final LayoutActionHost _screen;

  Never _noWindow(String operation) => throw StateError(
      '$operation reached a script performed in another solution, which has no window here');

  @override
  Future<void> actionShowDialog(String title, String message) => _screen.actionShowDialog(title, message);

  @override
  Future<void> actionOpenUrl(String url) => _screen.actionOpenUrl(url);

  @override
  String actionResolveText(String text) => text;

  @override
  Future<void> actionNewRecord() async => _noWindow('New Record');
  @override
  Future<void> actionDuplicateRecord() async => _noWindow('Duplicate Record');
  @override
  Future<void> actionDeleteRecord({required bool confirm}) async => _noWindow('Delete Record');
  @override
  Future<void> actionCommitRecord() async => _noWindow('Commit Records');
  @override
  Future<void> actionRevertRecord() async => _noWindow('Revert Record');
  @override
  Future<void> actionGoToRecord(String target) async => _noWindow('Go to Record');
  @override
  Future<void> actionEnterFindMode() async => _noWindow('Enter Find Mode');
  @override
  Future<void> actionPerformFind() async => _noWindow('Perform Find');
  @override
  Future<void> actionShowAllRecords() async => _noWindow('Show All Records');
  @override
  Future<void> actionEnterPreviewMode() async => _noWindow('Enter Preview Mode');
  @override
  Future<void> actionEnterBrowseMode() async => _noWindow('Enter Browse Mode');
  @override
  Future<bool> actionGoToLayout(String layoutName) async => _noWindow('Go to Layout');
  @override
  Future<bool> actionSetField(String field, String value) async => _noWindow('Set Field');
  @override
  Future<void> actionAdjustWindow(String windowAction) async => _noWindow('Adjust Window');
  @override
  Future<void> actionReLogin() async => _noWindow('Re-Login');
  @override
  Future<void> actionCloseFile() async => _noWindow('Close File');
  @override
  Future<void> actionExitApplication() async => _noWindow('Exit Application');
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
