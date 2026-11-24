/// Runtime state and extended host contract shared by `LayoutActionRunner`,
/// `ScriptExecution`, and `ScriptDebugSession` (#133).
library;

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

/// Result of a `Show Custom Dialog` step supporting custom buttons and input
/// fields.
class ScriptDialogResult {
  const ScriptDialogResult({
    this.buttonIndex = 1,
    this.inputValues = const <String, String>{},
  });

  /// 1-based index of the button clicked (`Get(LastMessageChoice)`).
  final int buttonIndex;

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

/// Shared execution state for a script call tree (propagated from caller to
/// nested `Perform Script` invocations, matching FileMaker semantics).
class ScriptSessionState {
  /// Whether `Set Error Capture [On]` is active.
  bool errorCapture = false;

  /// Most recent FileMaker error code (`Get(LastError)`).
  int lastError = 0;

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
  Future<GoToRecordOutcome> actionGoToPortalRow(String target);
  Future<void> actionDeletePortalRow({required bool confirm});
  Future<bool> actionReplaceFieldContents({
    required String field,
    required String replaceType,
    String? value,
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
