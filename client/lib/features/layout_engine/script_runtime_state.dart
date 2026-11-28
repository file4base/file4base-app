/// Runtime state and extended host contract shared by `LayoutActionRunner`,
/// `ScriptExecution`, and `ScriptDebugSession` (#133).
library;

import 'dart:async';

/// Outcome of navigating records or portal rows, so `Go to Record/Request/Page`
/// and `Go to Portal Row` can report boundary conditions (`Exit after last`)
/// and FileMaker error code 101 deterministically.
class GoToRecordOutcome {
  const GoToRecordOutcome({
    required this.moved,
    this.atBoundary = false,
    this.errorCode = 0,
  });

  /// True when the current record/row changed (or targeted an existing row).
  final bool moved;

  /// True when `next` was requested on the last record or `previous` on the
  /// first record, or the found set is empty.
  final bool atBoundary;

  /// FileMaker error code (`0` on success, `101` when record/row is missing).
  final int errorCode;
}

/// Outcome of `Replace Field Contents` over the found set.
class ReplaceOutcome {
  const ReplaceOutcome({
    this.errorCode = 0,
    this.detail = '',
    this.count = 0,
    this.replaced = 0,
    this.relatedField = false,
    this.unevaluatedAt = 0,
  });

  /// FileMaker error code: 0 when every record was replaced, otherwise the
  /// error of the record the replace stopped at.
  final int errorCode;

  /// What went wrong, as the server or the window put it.
  final String detail;

  /// Records in the whole found set.
  final int count;

  /// Records written before the replace finished or stopped.
  final int replaced;

  /// The field belongs to another table occurrence than the layout's, and
  /// nothing was written.
  final bool relatedField;

  /// 1-based position of the record whose value could not be worked out,
  /// where the replace stopped; 0 when there was none.
  final int unevaluatedAt;
}

/// Result of a `Show Custom Dialog` step supporting custom buttons and input
/// fields.
class ScriptDialogResult {
  const ScriptDialogResult({
    this.buttonIndex = 1,
    this.inputValues = const <String, String>{},
  });

  /// 1-based index of the button clicked (`Get(LastMessageChoice)`), or
  /// [dismissed] when the dialog was closed without a button: that is never
  /// the first button, which is usually the one that goes ahead.
  final int buttonIndex;

  /// [buttonIndex] of a dialog closed without choosing a button.
  static const int dismissed = 0;

  /// Values entered into dialog input fields, keyed by field/variable name.
  final Map<String, String> inputValues;
}

/// One window tracked by the deterministic script window model (`New Window`,
/// `Select Window`, `Close Window`, `Get(WindowName)`).
class ScriptWindowEntry {
  ScriptWindowEntry({
    required this.name,
    required this.layoutName,
    this.style = 'Document',
    this.top,
    this.left,
    this.width,
    this.height,
  });

  String name;
  String layoutName;
  String style;
  double? top;
  double? left;
  double? width;
  double? height;
}

/// One script error recorded while `Set Error Logging [On]` is active.
///
/// It carries the columns FileMaker writes to `ScriptErrors.log` that the
/// client knows: when, which script and step, the error code, and the result
/// of the step's custom debug calculation.
class ScriptErrorLogEntry {
  const ScriptErrorLogEntry({
    required this.time,
    required this.scriptName,
    required this.stepName,
    required this.line,
    required this.errorCode,
    this.customInfo = '',
  });

  final DateTime time;
  final String scriptName;
  final String stepName;

  /// 1-based position of the step in its script.
  final int line;
  final int errorCode;
  final String customInfo;

  /// The entry as one tab-separated line, in FileMaker's column order.
  String toLogLine() => [
        time.toIso8601String(),
        scriptName,
        '$stepName ($line)',
        '$errorCode',
        customInfo,
      ].join('\t');
}

/// `Set Error Logging` state of one solution session.
///
/// FileMaker keeps logging for the open file until a script turns it off or
/// the file closes, across every script run in between. File4Base keeps it
/// for the signed-in session of the solution, which is what an open file is
/// here: a button script after the startup script that turned logging on is
/// still logged.
class ScriptErrorLogging {
  /// Whether `Set Error Logging [On]` is active.
  bool enabled = false;

  /// The custom debug calculation, evaluated when an error is logged. Empty
  /// when the step has none.
  String customInfo = '';

  /// The errors logged, oldest first. Bounded by [maxEntries]; the oldest
  /// entries go first.
  final List<ScriptErrorLogEntry> entries = <ScriptErrorLogEntry>[];

  static const maxEntries = 1000;

  void add(ScriptErrorLogEntry entry) {
    entries.add(entry);
    if (entries.length > maxEntries) entries.removeAt(0);
  }
}

/// Where a script's insertion into a field landed.
enum FieldInsertOutcome {
  /// Written: into the find request in Find mode, the record otherwise.
  written,

  /// The table has no such field.
  noSuchField,

  /// The step names a field of another table occurrence than the layout's,
  /// which the window cannot write.
  relatedField,
}

/// Shared execution state for a script call tree (propagated from caller to
/// nested `Perform Script` invocations, matching FileMaker semantics).
class ScriptSessionState {
  /// Whether `Set Error Capture [On]` is active.
  bool errorCapture = false;

  /// Most recent FileMaker error code (`Get(LastError)`).
  int get lastError => _lastError;
  set lastError(int code) {
    _lastError = code;
    if (code != 0) errorRaised = true;
  }

  int _lastError = 0;

  /// Set whenever a step reports a nonzero error. The script execution
  /// clears it before each step and reads it after, so `Set Error Logging`
  /// logs each error once, by the step that raised it.
  bool errorRaised = false;

  /// Most recent `Show Custom Dialog` button index (`Get(LastMessageChoice)`).
  int lastMessageChoice = 1;

  /// Whether `Allow User Abort` is enabled.
  bool allowUserAbort = true;

  /// Whether `Freeze Window` is active.
  bool windowFrozen = false;

  /// Active field focused by `Go to Field` / `Go to Next Field`.
  String activeField = '';

  /// Active layout object focused by `Go to Object`.
  String activeObject = '';

  /// Deterministic script clipboard used by `Copy` / `Paste` / `Cut`.
  String clipboard = '';

  /// Active custom menu set recorded by `Install Menu Set`.
  String activeMenuSet = '';

  /// Most recent `Print Setup` configuration.
  Map<String, dynamic> printSettings = <String, dynamic>{};

  /// UI chrome visibility state (`toolbars`, `menubar`, `text_ruler`).
  final Map<String, String> uiVisibility = <String, String>{};

  /// Deterministic stack of open windows (last entry is the frontmost window).
  final List<ScriptWindowEntry> windows = <ScriptWindowEntry>[];

  /// Ensures at least the initial window is tracked.
  ScriptWindowEntry ensureCurrentWindow(String layoutName) {
    if (windows.isEmpty) {
      windows.add(ScriptWindowEntry(name: 'Main', layoutName: layoutName));
    }
    return windows.last;
  }

  String get currentWindowName => windows.isEmpty ? 'Main' : windows.last.name;
}

/// Optional extended host capabilities for full FileMaker script execution
/// (#133). Implemented by `DataBrowserWidgetState` and extended test hosts;
/// hosts that only implement `LayoutActionHost` stay source-compatible and fall
/// back to deterministic runner defaults.
abstract class ExtendedLayoutActionHost {
  Future<GoToRecordOutcome> actionGoToRecordEx(String target);
  Future<bool> actionGoToLayoutByNumber(int layoutNumber);
  Future<void> actionEnterFindModeEx({List<Map<String, dynamic>>? requests, bool restore = false});
  /// The number of records found; -1 when OnModeExit kept the window in
  /// Find mode, and nothing was found; -2 when the server refused the find.
  Future<int> actionPerformFindEx({List<Map<String, dynamic>>? requests, bool restore = false});
  Future<void> actionSortRecords({List<Map<String, dynamic>>? sortOrder, bool restore = false, bool dialog = true});
  Future<void> actionUnsortRecords();
  Future<bool> actionOmitRecord();
  Future<void> actionOmitMultipleRecords(int count);
  Future<void> actionShowOmittedOnly();
  Future<void> actionDeleteAllRecords({required bool confirm});
  Future<void> actionCommitRecordEx({bool dialog = true, bool skipValidation = false, bool forceCommit = false});
  Future<bool> actionGoToField(String field, {bool select = false});
  Future<void> actionGoToNextField();
  Future<bool> actionGoToObject(String objectName);
  Future<bool> actionGoToRelatedRecord({
    required String relatedTable,
    String? layoutName,
    bool showOnlyRelated = true,
    bool newWindow = false,
  });
  /// Go to Portal Row: makes a row of the active portal (or of the first
  /// portal of the layout) the active one, keeping the active field when it
  /// is in that portal; [select] selects the whole row instead. Error 101
  /// past either end, [GoToRecordOutcome.atBoundary] set.
  Future<GoToRecordOutcome> actionGoToPortalRow(String target, {bool select = false});
  /// Deletes the active portal row only, never the parent record; returns
  /// the FileMaker error code (0 when a row was deleted).
  Future<int> actionDeletePortalRow({required bool confirm});
  /// Replace Field Contents: writes [field] in every record of the found
  /// set, including the records not read yet, in found-set order. Each
  /// record is the current one while [valueFor] works out its value (its
  /// 1-based position and the found count are given), so a calculation
  /// reads that record; a null [valueFor] writes the field's contents in the
  /// record in hand to all of them. [table] is the table occurrence the step
  /// names: a field of another occurrence than the layout's is refused, never
  /// written in the layout's table. The replace stops at the first value
  /// that cannot be worked out and at the first write the server rejects;
  /// the current record is the same one afterwards.
  Future<ReplaceOutcome> actionReplaceFieldContents({
    required String field,
    String? table,
    FutureOr<String?> Function(int position, int count)? valueFor,
  });
  Future<ScriptDialogResult> actionShowDialogEx({
    required String title,
    required String message,
    List<Map<String, dynamic>>? buttons,
    List<Map<String, dynamic>>? inputFields,
  });
  Future<void> actionPrint({bool dialog = true, Map<String, dynamic>? printSettings});
  Future<void> actionSaveRecordsAsPdf({String? path, bool dialog = true, bool append = false});
  Future<void> actionExportFieldContents({required String field, String? path});
  Future<bool> actionInsertFile({required String field, String? path, bool isPicture = false});
  Future<void> actionCopyToClipboard(String text);
  Future<String?> actionReadFromClipboard();
  Future<void> actionBeep();
  Future<void> actionRefreshWindow({bool flushJoinCache = false, bool flushExternalCache = false});

  /// Reloads the related records of the portal named [objectName] on the
  /// current layout, or of every portal when [objectName] is empty. Returns
  /// false when the layout has no portal of that name.
  Future<bool> actionRefreshPortal(String objectName);

  /// The File4Base type (`TEXT`, `NUMBER`, `DATE`, `TIMESTAMP`, ...) of
  /// [field] in the current table, or null when the table has no such field
  /// or [table] names another table occurrence than the layout's.
  String? actionFieldType(String field, {String? table});

  /// Puts [text] into [field] as typing would: into the current find request
  /// in Find mode — never into the record then — and into the record
  /// otherwise. With [replaceAll] the text replaces the content; without it,
  /// it replaces the selection of [field] when it is the active field, or
  /// goes in at its caret, or at the end. [table] is the table occurrence the
  /// step names, if any; a field of another occurrence than the layout's is
  /// not written.
  Future<FieldInsertOutcome> actionInsertIntoField({
    required String field,
    required String text,
    required bool replaceAll,
    String? table,
  });
  Future<void> actionSendMail({
    String to = '',
    String cc = '',
    String bcc = '',
    String subject = '',
    String message = '',
  });
  Future<void> actionSetUiVisibility(String element, String state);
  Future<String?> actionInsertFromUrl({
    required String url,
    bool verifySsl = true,
    String? curlOptions,
  });

  String? get actionActiveFieldName;
  int get actionFoundCount;
  int get actionCurrentRecordNumber;
  int get actionTotalRecordCount;
}

/// The active row of a portal on the layout: the related record a script
/// step acts on when it names a field of the portal's table occurrence, as
/// FileMaker's Set Field does (it writes the active portal row of a portal
/// of that occurrence, and the first related record otherwise).
class ActivePortalRow {
  const ActivePortalRow({
    required this.portalObjectId,
    required this.portalName,
    required this.occurrence,
    required this.table,
    required this.rowNumber,
    required this.recordId,
    required this.record,
    this.write,
  });

  /// The id of the portal object on the layout, and its object name.
  final String portalObjectId;
  final String portalName;

  /// The table occurrence the portal shows, and its base table.
  final String occurrence;
  final String table;

  /// 1-based number of the row in the portal, as Get(ActivePortalRowNumber)
  /// answers it.
  final int rowNumber;

  /// The id of the related record of the row, and its values as read.
  final String recordId;
  final Map<String, dynamic> record;

  /// Writes [values] (field name -> value) into the row's related record,
  /// through the session of the solution it belongs to, and shows the
  /// portal again with what was written. Completes with the record as the
  /// server has it; throws as the write does. [script] marks a script's
  /// write, which may set a field that prohibits modification.
  final Future<Map<String, dynamic>> Function(Map<String, dynamic> values, {bool script})? write;
}

/// A host whose layout has portals with an active row (Go to Portal Row, a
/// click on a row, a field of a row entered).
abstract class PortalRowHost {
  /// The active portal row, or null when no portal row is active. With
  /// [occurrence], only the active row of a portal that shows that table
  /// occurrence (named as in `Occurrence::Field`, compared without case).
  ActivePortalRow? actionActivePortalRow({String? occurrence});

  /// Get(ActivePortalRowNumber): the active row of the active portal, or 0.
  int get actionActivePortalRowNumber;

  /// Get(ActiveFieldTableName): the table occurrence of the active field,
  /// or empty when no field is active.
  String get actionActiveFieldTableName;
}

/// A host that runs script triggers (#26): the action runner lets the
/// triggers a script step set off finish before the script's next step, as
/// FileMaker runs them before carrying on.
abstract class ScriptTriggerHost {
  /// A mark to [actionSettleTriggers] from.
  int actionTriggerMark();

  /// Waits for the triggers started since [since].
  Future<void> actionSettleTriggers(int since);
}
