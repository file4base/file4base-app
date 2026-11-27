import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/models/page_setup_model.dart';
import 'package:file4base_client/core/widgets/validation_override.dart';
import 'package:file4base_client/features/data_browser/external_access.dart';
import 'package:file4base_client/features/data_browser/external_layout.dart';
import 'package:file4base_client/features/data_browser/mail_compose_dialog.dart';
import 'package:file4base_client/features/data_browser/related_records.dart';
import 'package:file4base_client/features/data_browser/report_view.dart';
import 'package:file4base_client/features/layout_engine/chart_view.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/layout_object_visuals.dart';
import 'package:file4base_client/features/layout_engine/layout_preview_widget.dart';
import 'package:file4base_client/features/layout_engine/models/chart_definition.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/layout_engine/script_trigger_runtime.dart';
import 'package:file4base_client/features/layout_engine/view_format_tools.dart';
import 'package:file4base_client/main.dart';
import 'package:flutter/foundation.dart' show defaultTargetPlatform, kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import 'package:printing/printing.dart';
import 'package:url_launcher/url_launcher.dart';

class DataBrowserWidget extends StatefulWidget {

  const DataBrowserWidget({
    required this.table, required this.apiClient, required this.mode, super.key,
    this.layout,
    this.onModeChanged,
    this.onRecordChanged,
    this.onTableModified,
    this.currentUserName,
    this.onGoToLayout,
    this.onFoundSetChanged,
    this.showPageMargins = false,
    this.showTextRuler = false,
    this.pageSetup = const PageSetupModel(),
    this.onFocusedFieldChanged,
    this.onObjectStyleChanged,
    this.onReLogin,
    this.onCloseFile,
    this.onExitApplication,
    this.onAdjustWindow,
    this.onGoHome,
    this.externalOccurrences = const {},
    this.externalDatabase,
    this.privileges,
  });
  final TableModel table;
  final ApiClient apiClient;
  final OperationalMode mode;
  final LayoutDefinitionModel? layout;
  final ValueChanged<OperationalMode>? onModeChanged;
  final void Function(int currentIndex, int totalRecords)? onRecordChanged;
  final VoidCallback? onTableModified;

  /// Signed-in user, shown by `{{CurrentUser}}` in layout text.
  final String? currentUserName;

  /// Switches to the layout with this name (button "Go to Layout" steps).
  /// Returns false when there is no such layout.
  final bool Function(String layoutName)? onGoToLayout;

  /// Reports the find requests that define the current found set, empty when
  /// every record is showing. Preview needs them so the printed sheet is the
  /// found set rather than the whole table (#32).
  final ValueChanged<List<Map<String, dynamic>>>? onFoundSetChanged;

  final bool showPageMargins;
  final bool showTextRuler;
  final PageSetupModel pageSetup;
  final VoidCallback? onFocusedFieldChanged;
  final void Function(String objectId, LayoutObjectStyle newStyle)? onObjectStyleChanged;
  final VoidCallback? onReLogin;
  final VoidCallback? onCloseFile;
  final VoidCallback? onExitApplication;
  final ValueChanged<String>? onAdjustWindow;
  final VoidCallback? onGoHome;

  /// Lowercased names and ids of the occurrences of another solution's table
  /// (#107). A field object of one of them is shown as unavailable, never
  /// bound to a local column of the same name.
  final Set<String> externalOccurrences;

  /// The solution [table] is in when the layout is on an occurrence of
  /// another solution's table (#107): its records are read there, through
  /// [apiClient], and relationships are followed from them. Null for a table
  /// of this solution.
  final String? externalDatabase;

  /// What the session may do with [table]'s records, which enables the record
  /// commands. For another solution's table it is what the account held there
  /// may do (#107), and without it its records are only read. Null for this
  /// solution's table enables them all, the server deciding.
  final RecordPrivileges? privileges;

  /// What [obj] shows when it is a field of an external occurrence, else null.
  String? externalFieldNoticeOf(LayoutObjectModel obj) =>
      obj.type == 'field' ? externalFieldNotice(obj.fieldBinding, externalOccurrences) : null;

  @override
  State<DataBrowserWidget> createState() => DataBrowserWidgetState();
}

/// One level of a sort order: a field and its direction.
class SortLevel {

  const SortLevel(this.field, this.ascending);
  final String field;
  final bool ascending;

  SortLevel get flipped => SortLevel(field, !ascending);

  /// The form the data API takes in its `sort` parameter: a leading "-" sorts
  /// the field descending.
  String get queryValue => ascending ? field : '-$field';

  @override
  bool operator ==(Object other) =>
      other is SortLevel && other.field == field && other.ascending == ascending;

  @override
  int get hashCode => Object.hash(field, ascending);

  @override
  String toString() => queryValue;
}

/// One find request: what was typed into each field, and whether the request
/// omits what it matches instead of finding it.
class FindRequestDraft {

  FindRequestDraft({Map<String, String>? values, this.omit = false})
      : values = values ?? <String, String>{};
  final Map<String, String> values;
  bool omit;

  FindRequestDraft copy() =>
      FindRequestDraft(values: Map<String, String>.from(values), omit: omit);

  bool get isEmpty => values.values.every((v) => v.trim().isEmpty);
}

class DataBrowserWidgetState extends State<DataBrowserWidget>
    implements
        LayoutActionHost,
        ExtendedLayoutActionHost,
        AutoEnterActionHost,
        CalculationContextHost,
        RelatedFieldHost,
        PortalRowHost,
        ScriptTriggerHost,
        DocumentActionHost,
        ImportRecordsHost {
  /// Without privileges, this solution's records take every command, the
  /// server deciding, and another solution's are only read.
  RecordPrivileges get _privileges =>
      widget.privileges ?? (widget.externalDatabase != null ? const RecordPrivileges(read: true) : RecordPrivileges.all);
  bool get _canCreate => _privileges.create;
  bool get _canEdit => _privileges.edit;
  bool get _canDelete => _privileges.delete;

  /// Whether the current record's fields take entry: a new one's when
  /// records may be created, a saved one's when they may be edited.
  bool get _entryAllowed => _onDraft ? _canCreate : _canEdit;

  /// Runs a write, signing in to the solution of an external occurrence and
  /// trying once more when the session is not signed in to it (#107).
  Future<T> _write<T>(Future<T> Function() call) => _externalAccess.retryingSignIn(context, call);

  /// A record write: one of data entry asks the person whether to keep
  /// values breaking a validation rule that allows it, and sends it again
  /// with the override if they do; a script step's is refused as it is.
  Future<T> _entryWrite<T>(
    Future<T> Function({required bool overrideValidation}) call, {
    required bool script,
  }) {
    if (script) return _write(() => call(overrideValidation: false));
    return writeAllowingOverride(context, ({required overrideValidation}) => _write(() => call(overrideValidation: overrideValidation)));
  }

  List<Map<String, dynamic>> _records = [];
  bool _isLoading = true;
  String? _error;
  int _currentIndex = 0;
  bool _isFoundSet = false;

  /// The find requests behind the found set, so that the records of it not
  /// read yet can be (Replace Field Contents covers them all); null when the
  /// found set is every record, or [_foundSetInHand].
  List<Map<String, dynamic>>? _foundSetFind;

  /// The found set is the records in hand and nothing else (Show Omitted
  /// Only builds it from them).
  bool _foundSetInHand = false;

  /// Records omitted from the found set since it was found.
  final Set<String> _omittedIds = <String>{};
  String? _lastFocusedFindField;
  String? _lastFocusedBrowseField;
  String _viewMode = 'form'; // 'form' (layout), 'list' (record list) or 'table' (spreadsheet grid)

  /// Name of the currently focused or last focused field in Browse or Find mode.
  String? get activeFieldName => _lastFocusedBrowseField ?? _lastFocusedFindField;

  /// Layout object bound to [activeFieldName], or the first field object on the layout.
  LayoutObjectModel? get activeFieldObject {
    final layout = widget.layout;
    if (layout == null || layout.objects.isEmpty) return null;
    final targetField = activeFieldName;
    if (targetField != null) {
      final match = layout.objects
          .where((o) =>
              o.type == 'field' &&
              (o.fieldBinding?.fieldName.toLowerCase() == targetField.toLowerCase()))
          .firstOrNull;
      if (match != null) return match;
    }
    return layout.objects.where((o) => o.type == 'field').firstOrNull ??
        layout.objects.first;
  }

  /// Active style of the focused field in Browse/Find mode.
  LayoutObjectStyle get activeStyle =>
      activeFieldObject?.style ?? const LayoutObjectStyle();

  // Found set bookkeeping for the record bar
  int _totalInTable = 0;
  List<Map<String, dynamic>> _unsortedRecords = [];
  /// The sort order, outermost field first. Empty means the records are in
  /// the order they are stored in.
  List<SortLevel> _sortOrder = const [];

  String? get _sortField => _sortOrder.isEmpty ? null : _sortOrder.first.field;
  bool get _sortAscending => _sortOrder.isEmpty || _sortOrder.first.ascending;
  final TextEditingController _recordNumberCtrl = TextEditingController();
  final FocusNode _recordNumberFocus = FocusNode();
  double? _sliderDragValue; // record shown by the slider while it is dragged

  int get currentIndex => _currentIndex;
  int get totalRecords => _records.length;

  /// The record in hand, as the Data Viewer shows it (#50), and its id. Null
  /// when the found set is empty or the record has not been committed yet.
  Map<String, dynamic>? get currentRecord {
    if (_records.isEmpty || _currentIndex >= _records.length) return null;
    return _records[_currentIndex];
  }

  String? get currentRecordId {
    final record = currentRecord;
    if (record == null || record[_draftKey] == true) return null;
    final id = record['id']?.toString();
    return (id == null || id.isEmpty) ? null : id;
  }
  bool get isOmit => _omit;
  bool get isFoundSet => _isFoundSet;

  void createNewRecord() => _createNewRecord();
  void duplicateRecord() => actionDuplicateRecord();

  /// Records > Relookup Field Contents: copies the looked-up values again
  /// into the found set, from the active field as the match field, after
  /// asking.
  Future<void> relookupFieldContents() async {
    final field = activeFieldName;
    final col = field == null ? null : _columnNamed(field);
    if (col == null) {
      _notify('Click into the match field of the lookups to relookup first.');
      return;
    }
    final (code, updated) = await _relookup(col, dialog: true);
    if (code == 0) {
      _notify('Relookup Field Contents changed $updated ${updated == 1 ? 'record' : 'records'}.');
    }
  }
  void sortRecords() => _showSortDialog();
  void deleteCurrentRecord() => _deleteCurrentRecord();
  void performFind() => _performFind();

  // ─── Find requests (Requests menu) ─────────────────────────────────────────

  /// Adds an empty request after the current one and moves to it.
  void newFindRequest() {
    _captureCurrentFindRequest();
    setState(() {
      _findRequests.insert(_findRequestIndex + 1, FindRequestDraft());
      _findRequestIndex += 1;
    });
    _applyCurrentFindRequestToControllers();
  }

  /// Copies the current request and moves to the copy.
  void duplicateFindRequest() {
    _captureCurrentFindRequest();
    setState(() {
      _findRequests.insert(_findRequestIndex + 1, _currentFindRequest.copy());
      _findRequestIndex += 1;
    });
    _applyCurrentFindRequestToControllers();
  }

  /// Removes the current request. The last one is emptied instead, so Find
  /// mode always has a request to type into.
  void deleteFindRequest() {
    setState(() {
      if (_findRequests.length == 1) {
        _findRequests[0] = FindRequestDraft();
      } else {
        _findRequests.removeAt(_findRequestIndex);
        if (_findRequestIndex >= _findRequests.length) {
          _findRequestIndex = _findRequests.length - 1;
        }
      }
    });
    _applyCurrentFindRequestToControllers();
  }

  void deleteAllFindRequests() {
    setState(() {
      _findRequests
        ..clear()
        ..add(FindRequestDraft());
      _findRequestIndex = 0;
    });
    _applyCurrentFindRequestToControllers();
  }

  /// `first`, `previous`, `next`, `last`, or a 1-based request number.
  void goToFindRequest(String target) {
    if (_findRequests.isEmpty) return;
    final t = target.trim().toLowerCase();
    final index = switch (t) {
      'first' => 0,
      'previous' || 'prev' => _findRequestIndex - 1,
      'next' => _findRequestIndex + 1,
      'last' => _findRequests.length - 1,
      _ => (int.tryParse(t) ?? (_findRequestIndex + 1)) - 1,
    };
    _loadFindRequest(index.clamp(0, _findRequests.length - 1));
  }

  void _applyCurrentFindRequestToControllers() {
    final request = _currentFindRequest;
    _findControllers.forEach((field, controller) {
      controller.text = request.values[field] ?? '';
    });
    if (mounted) setState(() {});
  }

  void fetchRecords() => _fetchRecords();

  /// Reloads after a change made in another session (realtime sync). It keeps
  /// the record you are on, and never interrupts editing or a find: while a
  /// field has focus the reload waits until you leave it.
  void reloadFromRemote() {
    if (widget.mode != OperationalMode.browse) {
      _pendingRemoteReload = false;
      return;
    }
    if (_aFieldIsFocused) {
      _pendingRemoteReload = true;
      return;
    }
    _pendingRemoteReload = false;
    unawaited(_fetchRecords(preserveCurrent: true));
  }

  bool _pendingRemoteReload = false;
  bool get _aFieldIsFocused => _fieldFocusNodes.values.any((f) => f.hasFocus);

  void previousRecord() {
    if (_records.isNotEmpty && _currentIndex > 0) {
      unawaited(_saveCurrentRecord().then((saved) {
        if (!saved || !mounted) return;
        setState(() => _currentIndex--);
        _rebuildFieldControllers();
        _recordChanged();
      }));
    }
  }

  void nextRecord() {
    if (_records.isNotEmpty && _currentIndex < _records.length - 1) {
      unawaited(_saveCurrentRecord().then((saved) {
        if (!saved || !mounted) return;
        setState(() => _currentIndex++);
        _rebuildFieldControllers();
        _recordChanged();
      }));
    }
  }

  /// Saves the current record and moves to [index]. Completes with whether
  /// the record changed (false when saving failed or [index] is invalid).
  Future<bool> goToRecord(int index) async {
    if (_records.isEmpty || index < 0 || index >= _records.length) return false;
    final saved = await _saveCurrentRecord();
    if (!saved || !mounted) return false;
    setState(() => _currentIndex = index);
    _rebuildFieldControllers();
    _recordChanged();
    return true;
  }

  void toggleOmit(bool val) => setState(() => _currentFindRequest.omit = val);

  /// Writes the current record to the server: pending field edits, and a new
  /// record that has not been created yet.
  Future<void> commitRecord() => actionCommitRecord();

  /// Discards uncommitted changes; a new record that was never committed is
  /// thrown away.
  Future<void> revertRecord() => actionRevertRecord();

  /// `first`, `previous`, `next`, `last`, or a 1-based record number.
  Future<void> goToRecordNamed(String target) => actionGoToRecord(target);

  /// Restores the order the records are stored in.
  void unsort() => _applySort(null, true);

  /// True while the current record is a new one that has not been committed.
  bool get hasUncommittedRecord => _onDraft;

  // ─── Find mode ────────────────────────────────────────────────────────────
  final Map<String, TextEditingController> _findControllers = {};

  /// Find mode holds several requests. The controllers above edit the one at
  /// [_findRequestIndex]; the others keep their values here.
  final List<FindRequestDraft> _findRequests = [FindRequestDraft()];
  int _findRequestIndex = 0;

  FindRequestDraft get _currentFindRequest => _findRequests[_findRequestIndex];
  bool get _omit => _currentFindRequest.omit;

  int get findRequestCount => _findRequests.length;
  int get findRequestNumber => _findRequestIndex + 1;

  /// Copies what is in the field boxes into the request being edited. A
  /// criterion on a field this layout has no box for is left as it is, so a
  /// saved find naming a field the layout does not show keeps it (#34).
  void _captureCurrentFindRequest() {
    final values = _currentFindRequest.values;
    for (final field in _findControllers.keys) {
      values.remove(field);
    }
    _findControllers.forEach((field, controller) {
      final text = controller.text.trim();
      if (text.isNotEmpty) values[field] = controller.text;
    });
  }

  /// The requests as they stand, for saving them under a name (#34). The
  /// criteria travel as typed, so a saved find can be opened and changed.
  List<SavedFindRequestModel> currentFindRequests() {
    _captureCurrentFindRequest();
    final requests = <SavedFindRequestModel>[];
    for (final request in _findRequests) {
      final values = <String, String>{};
      request.values.forEach((field, value) {
        if (value.trim().isNotEmpty) values[field] = value.trim();
      });
      if (values.isEmpty) continue;
      requests.add(SavedFindRequestModel(values: values, omit: request.omit));
    }
    return requests;
  }

  /// Puts a saved find's requests into Find mode and performs it, so what
  /// ran is also what the boxes show if the user goes back to change it.
  Future<void> runSavedFind(SavedFindModel find) async {
    if (find.requests.isEmpty) return;
    setState(() {
      _findRequests
        ..clear()
        ..addAll(find.requests.map((r) => FindRequestDraft(
              values: Map<String, String>.from(r.values),
              omit: r.omit,
            )));
      _findRequestIndex = 0;
    });
    _applyCurrentFindRequestToControllers();
    await _performFind();
  }

  /// Puts a request's values into the field boxes.
  void _loadFindRequest(int index) {
    if (index < 0 || index >= _findRequests.length) return;
    _captureCurrentFindRequest();
    final request = _findRequests[index];
    _findControllers.forEach((field, controller) {
      controller.text = request.values[field] ?? '';
    });
    setState(() => _findRequestIndex = index);
  }

  // ─── Per-field edit state ─────────────────────────────────────────────────
  final Map<String, TextEditingController> _fieldControllers = {};
  final Map<String, FocusNode> _fieldFocusNodes = {};
  final Map<String, Timer?> _fieldDebounceTimers = {};
  // null = idle/saved, true = saving, false = error
  final Map<String, bool?> _fieldSaving = {};

  /// The mode last asked of the parent. The parent switches
  /// [DataBrowserWidget.mode] on its next build, so a script step that runs
  /// right after Enter Find Mode reads this instead of the stale mode.
  OperationalMode? _requestedMode;

  OperationalMode get _mode => _requestedMode ?? widget.mode;

  /// Switches the window to [mode], once the layout's OnModeExit trigger,
  /// and the triggers of the active field, let it leave the mode it is in
  /// (#26); false when they keep it there. Without such triggers the mode
  /// is asked of the parent before this returns.
  Future<bool> _requestMode(OperationalMode mode) async {
    if (!await _mayLeaveModeFor(mode)) return false;
    _applyMode(mode);
    return true;
  }

  /// Whether the layout's OnModeExit trigger, and the active field's, let
  /// the window leave its mode for [mode].
  Future<bool> _mayLeaveModeFor(OperationalMode mode) async {
    if (widget.onModeChanged == null) return false;
    if (mode == _mode) return true;
    final box = _activeBox;
    final exit = _trigger('OnModeExit');
    if (exit != null || (box != null && _boxHasTriggers(box.object))) {
      if (!await _leaveMode()) return false;
      if (!mounted) return false;
    }
    return true;
  }

  void _applyMode(OperationalMode mode) {
    if (widget.onModeChanged == null) return;
    final changed = mode != _mode;
    // Find mode opens on one blank request, set up now rather than when the
    // parent rebuilds: the script steps that follow write into it first.
    if (mode == OperationalMode.find && _mode != OperationalMode.find) _initFindControllers();
    _requestedMode = mode;
    widget.onModeChanged!.call(mode);
    if (changed && _recordLoadArmed) _enteredMode(mode);
  }

  /// How many times each portal, by object id, was asked to reload its
  /// related records (Refresh Portal, Refresh Window). A portal reloads when
  /// its count changes.
  final Map<String, int> _portalRefreshes = {};

  /// The panel brought in front of each tab or slide control, by control
  /// id; a control not in it shows its default panel.
  final Map<String, int> _selectedPanels = {};

  /// The objects hidden by their "Hide object when" calculation, by layout,
  /// record, the record's values and how fresh the answer is; and the last
  /// answer for each layout and record, shown while it is evaluated again.
  final Map<String, Set<String>> _hiddenObjects = {};
  final Map<String, Set<String>> _lastHiddenObjects = {};

  /// What is waiting to be evaluated: the key of each answer, with its
  /// layout and record. The asks made while a list is drawn go together.
  final Map<String, ({LayoutDefinitionModel layout, String recordId, String base})> _hiddenQueue = {};
  final Set<String> _hiddenPending = {};

  /// An answer is evaluated again once the records are read again (which
  /// is when related records may have changed), and the current record's
  /// once it is committed or moved to.
  int _hiddenGeneration = 0;
  final Map<String, int> _hiddenRecordGeneration = {};

  /// Forgets the answers for [recordId], or for every record.
  void _invalidateHidden([String? recordId]) {
    if (recordId == null) {
      _hiddenGeneration++;
      _hiddenRecordGeneration.clear();
      _hiddenObjects.clear();
    } else {
      _hiddenRecordGeneration[recordId] = (_hiddenRecordGeneration[recordId] ?? 0) + 1;
    }
  }

  /// The objects of [layout] hidden on [record]. The answer comes from the
  /// server, which evaluates each calculation from the layout's occurrence
  /// as a calculation field would; until it does, the record's last answer
  /// holds, and on a record seen for the first time every object with a
  /// condition is hidden, so nothing hidden is ever shown or typed into.
  ///
  /// The answer also holds `<object id>#<index>@<repetition>` for each
  /// conditional format whose calculation is true on [record] in that
  /// repetition (see [_conditions]); until it is known, none applies. The
  /// objects of a portal's rows are hidden per row, by the portal.
  Set<String> _hiddenOn(LayoutDefinitionModel layout, Map<String, dynamic> record) {
    final conditions = _conditions(layout);
    final recordId = record['id']?.toString() ?? '';
    if (conditions.isEmpty ||
        recordId.isEmpty ||
        record[_draftKey] == true) {
      return const {};
    }
    final base = '${layout.id}|$recordId';
    final values = Object.hashAllUnordered(record.entries.map((e) => Object.hash(e.key, e.value?.toString())));
    final key = '$base|$_hiddenGeneration|${_hiddenRecordGeneration[recordId] ?? 0}|$values';
    final known = _hiddenObjects[key];
    if (known != null) return known;
    if (_hiddenPending.add(key)) {
      if (_hiddenQueue.isEmpty) scheduleMicrotask(() => unawaited(_evaluateHidden()));
      _hiddenQueue[key] = (layout: layout, recordId: recordId, base: base);
    }
    return _lastHiddenObjects[base] ?? _conditionallyHidden;
  }

  /// [objects] of [layout] less those hidden on [record].
  /// Objects on a panel not in front are left out too, and so is what sits
  /// on the panels of a control that is hidden.
  List<LayoutObjectModel> _shownOn(LayoutDefinitionModel layout, Map<String, dynamic> record, List<LayoutObjectModel> objects) {
    final hidden = _hiddenOn(layout, record);
    final off = objectsOffPanel(layout.objects, selected: _selectedPanels, hidden: hidden);
    return hidden.isEmpty && off.isEmpty
        ? objects
        : [for (final o in objects) if (!hidden.contains(o.id) && !off.contains(o.id)) o];
  }

  /// The calculations [layout] evaluates against each record, by the key
  /// its answer is kept under, with what their Self reads: each object's
  /// hide condition, by the object's id, and each formula of the
  /// conditional formats of a field, a text or a button, in each
  /// repetition a field shows, by
  /// `<object id>#<index of the rule>@<repetition>`. Self is the value a
  /// field object shows, and the text a text or a button shows, as in
  /// FileMaker. The conditions of a portal's row objects are its rows',
  /// evaluated there against each related record.
  List<(String, String, ConditionSelf?)> _conditions(LayoutDefinitionModel layout) {
    if (!identical(layout, _conditionsLayout) || !identical(widget.table, _conditionsTable)) {
      final inPortals = _portalContents(layout).$2;
      _conditionsLayout = layout;
      _conditionsTable = widget.table;
      _conditionsOf = [
        for (final o in layout.objects)
          if (!inPortals.contains(o.id)) ...[
            if ((o.hideWhen ?? '').trim().isNotEmpty) (o.id, o.hideWhen!, _selvesOf(o).first.$2),
            if (o.type == 'field' || layoutObjectShowsText(o))
              for (final (i, rule) in o.style.conditionalRules.indexed)
                if (rule.isFormula && rule.value.trim().isNotEmpty)
                  for (final (n, self) in _selvesOf(o)) ('${o.id}#$i@$n', rule.value, self),
          ],
      ];
      _conditionallyHidden = {
        for (final o in layout.objects)
          if ((o.hideWhen ?? '').trim().isNotEmpty && !inPortals.contains(o.id)) o.id,
      };
    }
    return _conditionsOf;
  }

  /// A value typed into a related field was saved: the conditions of the
  /// records shown, which may read it, are evaluated again.
  void _relatedFieldWritten() {
    if (mounted) setState(() => _hiddenGeneration++);
  }

  /// What [_conditions] last answered, and for which layout and table.
  LayoutDefinitionModel? _conditionsLayout;
  TableModel? _conditionsTable;
  List<(String, String, ConditionSelf?)> _conditionsOf = const [];

  /// The objects of the layout [_conditions] last answered for that have a
  /// hide condition, outside the portals' rows.
  Set<String> _conditionallyHidden = const {};

  /// The field [obj] shows in each of the repetitions it shows, by
  /// repetition: what Self reads in its conditions. A related field, or one
  /// several relationships away, shows one value, its first related
  /// record's; a text or a button shows its text (see [layoutTextSelf]);
  /// any other object has no Self.
  List<(int, ConditionSelf?)> _selvesOf(LayoutObjectModel obj) {
    if (layoutTextSelf(obj) case final text?) return [(1, ConditionSelf.text(text))];
    final binding = obj.type == 'field' ? obj.fieldBinding : null;
    if (binding == null || binding.fieldName.isEmpty) return const [(1, null)];
    if (binding.readsRelated) {
      final occurrence = binding.isAlongPath ? binding.occurrencePath.last : binding.tableOccurrence;
      return [(1, occurrence == null || occurrence.isEmpty ? null : ConditionSelf(binding.fieldName, occurrence: occurrence))];
    }
    final col = widget.table.columns.where((c) => c.name.toLowerCase() == binding.fieldName.toLowerCase()).firstOrNull;
    if (col == null) return const [(1, null)];
    return [for (final n in binding.repetitionNumbers(col.maxRepetitions)) (n, ConditionSelf(col.name, repetition: n))];
  }

  /// Whether the formula of [obj]'s conditional format at an index is true
  /// on [record] in [repetition]; null when [obj] has no such format.
  bool Function(int index)? _formulaTrueOn(LayoutObjectModel obj, Map<String, dynamic> record, {int repetition = 1}) {
    final layout = widget.layout;
    if (layout == null || !obj.style.conditionalRules.any((r) => r.isFormula)) return null;
    final truths = _hiddenOn(layout, record);
    return (i) => truths.contains('${obj.id}#$i@$repetition');
  }

  /// [obj]'s style on [record], in [repetition], with the conditional
  /// formats that apply.
  LayoutObjectStyle _styleOn(LayoutObjectModel obj, Map<String, dynamic> record, Object? rawValue, {int repetition = 1}) =>
      obj.style.effectiveForValue(rawValue, formulaTrue: _formulaTrueOn(obj, record, repetition: repetition));

  /// Evaluates what is queued: for each layout, every condition against
  /// every record waiting, in as few requests as the endpoint allows.
  Future<void> _evaluateHidden() async {
    final queued = Map.of(_hiddenQueue);
    _hiddenQueue.clear();
    final byLayout = <String, List<String>>{};
    for (final e in queued.entries) {
      byLayout.putIfAbsent(e.value.layout.id, () => []).add(e.key);
    }
    final answers = <String, Set<String>>{};
    for (final keys in byLayout.values) {
      final layout = queued[keys.first]!.layout;
      final objects = _conditions(layout);
      final recordOf = {for (final k in keys) k: queued[k]!.recordId};
      final ids = recordOf.values.toSet().toList();
      final hidden = <String, Set<String>>{for (final id in ids) id: {}};
      final failed = <String>{};
      for (var r = 0; r < ids.length; r += ApiClient.maxEvaluatedRecords) {
        final chunkIds = ids.sublist(r, math.min(r + ApiClient.maxEvaluatedRecords, ids.length));
        for (var i = 0; i < objects.length; i += 50) {
          final chunk = objects.sublist(i, math.min(i + 50, objects.length));
          try {
            final results = await widget.apiClient.evaluateForRecords(
              widget.table.name,
              expressions: [for (final (_, expression, _) in chunk) expression],
              self: [for (final (_, _, self) in chunk) self],
              recordIds: chunkIds,
              occurrence: layout.tableOccurrence,
              resultType: 'Number',
            );
            for (final id in chunkIds) {
              for (final (j, result) in (results[id] ?? const <EvaluationModel>[]).indexed) {
                if (j >= chunk.length) continue;
                final key = chunk[j].$1;
                // A hide condition that cannot be evaluated hides its
                // object, as in FileMaker; a format's applies nothing.
                if (result.error.isEmpty ? isLayoutConditionTrue(result.text) : _conditionallyHidden.contains(key)) {
                  hidden[id]!.add(key);
                }
              }
            }
          } on Exception catch (_) {
            // Unanswered, every object with a hide condition stays hidden.
            failed.addAll(chunkIds);
          }
          if (!mounted) return;
        }
      }
      for (final k in keys) {
        answers[k] = failed.contains(recordOf[k]) ? _conditionallyHidden : hidden[recordOf[k]]!;
      }
    }
    if (!mounted) return;
    if (_hiddenObjects.length > 2000) _hiddenObjects.clear();
    setState(() {
      for (final e in answers.entries) {
        _hiddenPending.remove(e.key);
        _hiddenObjects[e.key] = e.value;
        _lastHiddenObjects[queued[e.key]!.base] = e.value;
      }
    });
  }

  // ─── Reports (#32) ─────────────────────────────────────────────────────────

  /// True when the layout in hand groups or totals rather than just listing
  /// records, which is what makes List view draw a report.
  bool get _layoutIsReport => widget.layout?.isReport ?? false;

  /// The summary fields of this table, by name.
  Map<String, SummarySpecModel> get _summarySpecs => {
        for (final col in widget.table.columns)
          if (col.fieldType == 'SUMMARY')
            col.name: ?SummarySpecModel.tryParse(col.calculationFormula),
      };

  /// The figures of the report: one grouping per break level, level 0 being
  /// the grand totals.
  ReportFigures _reportFigures = const {};
  bool _loadingReport = false;
  String? _reportError;

  /// What the figures now held were asked for: the table, the summary fields,
  /// the break levels and the found set. Null while there are none.
  ///
  /// Switching to the Report view re-reads the records (the first read may
  /// have been one page, and a report totals the whole found set) and reading
  /// records asks for the figures again — so the same question went to the
  /// server twice for one report, once per break level each time. The figures
  /// do not depend on the rows, only on this signature, so a load that would
  /// ask the same question is left alone.
  String? _reportFiguresKey;

  /// Reads the figures of the report from the server, which works them out
  /// over the same found set the find returned.
  ///
  /// One request per break level: level `n` is grouped by the first `n` break
  /// fields, and a sub-summary needs the group at its own level.
  Future<void> _loadReportFigures() async {
    final layout = widget.layout;
    if (layout == null || !layout.isReport) return;

    final fields = _summarySpecs.keys.toList();
    final breaks = layout.breakFields;
    final requests = _isFoundSet ? _findRequestPayload() : const <Map<String, dynamic>>[];

    final key = jsonEncode([widget.table.name, fields, breaks, requests]);
    if (key == _reportFiguresKey && (_loadingReport || _reportFigures.isNotEmpty)) {
      return;
    }
    _reportFiguresKey = key;

    setState(() {
      _loadingReport = true;
      _reportError = null;
    });
    try {
      final figures = <int, SummaryResultModel>{};
      for (var level = 0; level <= breaks.length; level++) {
        figures[level] = await widget.apiClient.summarize(
          widget.table.name,
          requests: requests,
          fields: fields,
          groupBy: breaks.take(level).toList(),
        );
      }
      if (!mounted) return;
      setState(() {
        _reportFigures = figures;
        _loadingReport = false;
      });
    } catch (e) {
      // The question was not answered, so it is not remembered as asked: a
      // retry must reach the server.
      _reportFiguresKey = null;
      if (!mounted) return;
      setState(() {
        _reportFigures = const {};
        _loadingReport = false;
        _reportError = e.toString();
      });
    }
  }

  /// Puts the found set in the order this report needs: by its break fields,
  /// outermost first, keeping any further levels the reader had set.
  void _sortForReport() {
    final layout = widget.layout;
    if (layout == null) return;
    final required = layout.requiredSortOrder;
    if (required.isEmpty) return;

    final order = [for (final field in required) SortLevel(field, true)];
    for (final level in _sortOrder) {
      if (!required.contains(level.field)) order.add(level);
    }
    _applySortOrder(order);
  }

  // ─── Charts (#37) ──────────────────────────────────────────────────────────

  /// The figures each chart draws, by the id of its object. A chart groups by
  /// its own category field, which need not be what the layout groups by.
  final Map<String, SummaryResultModel> _chartFigures = {};

  List<LayoutObjectModel> get _chartObjects =>
      widget.layout?.objects.where((o) => o.type == 'chart').toList() ?? const [];

  /// Reads the figures of every chart on the layout, over the found set.
  Future<void> _loadChartFigures() async {
    final charts = _chartObjects.where((o) => o.chart.isBound).toList();
    if (charts.isEmpty) return;
    final requests = _isFoundSet ? _findRequestPayload() : const <Map<String, dynamic>>[];

    for (final object in charts) {
      final config = object.chart;
      // A scatter plots the records themselves, so it needs no figures.
      if (!ChartType.isGrouped(config.chartType)) continue;
      try {
        final figures = await widget.apiClient.summarize(
          widget.table.name,
          requests: requests,
          fields: [for (final s in config.drawnSeries) s.field],
          groupBy: [config.categoryField!],
        );
        if (!mounted) return;
        setState(() => _chartFigures[object.id] = figures);
      } catch (_) {
        // The chart draws its notice; the rest of the layout is unaffected.
        if (mounted) setState(() => _chartFigures.remove(object.id));
      }
    }
  }

  /// Draws one chart object.
  Widget _buildChart(LayoutObjectModel object, bool isDark) {
    final config = object.chart;
    if (!config.isBound) {
      return ChartView(
        config: config,
        data: const ChartData(),
        isDark: isDark,
        notice: 'Chart: choose a field and a series in Chart Setup',
      );
    }

    // A series must name a summary field for a grouped chart; a plain field
    // has no figure over a group.
    final data = ChartType.isGrouped(config.chartType)
        ? chartDataFromSummary(
            config,
            _chartFigures[object.id],
            formatCategory: _mergeFieldText,
          )
        : chartDataFromRecords(
            config,
            _records.where((r) => r[_draftKey] != true).toList(),
          );

    return ChartView(
      config: config,
      data: data,
      isDark: isDark,
      notice: data.isEmpty && _chartFigures[object.id] == null
          ? 'Reading the figures…'
          : null,
    );
  }

  // ─── Relationships (#36) ───────────────────────────────────────────────────

  /// The relationship graph of this database, which is what a portal and a
  /// related field follow to reach the other side.
  Map<String, RelationshipModel> _relationships = const {};
  Map<String, TableOccurrenceModel> _occurrences = const {};
  Map<String, TableModel> _tablesById = const {};

  /// Everything a portal or a related field on this layout needs. Null until
  /// the catalog has been read, and whenever it could not be.
  RelatedContext? get _relatedContext {
    if (_relationships.isEmpty || _occurrences.isEmpty || _tablesById.isEmpty) return null;
    return RelatedContext(
      apiClient: widget.apiClient,
      fromTable: widget.table.name,
      recordId: _records.isEmpty || _currentIndex >= _records.length
          ? null
          : _records[_currentIndex]['id']?.toString(),
      relationshipsById: _relationships,
      occurrencesById: _occurrences,
      tablesById: _tablesById,
      formatValue: fieldText,
      fromDatabase: widget.externalDatabase,
      layoutOccurrence: widget.layout?.tableOccurrence,
      revision: _recordRevision,
    );
  }

  /// Counts the writes of the record in hand's fields: see
  /// [RelatedContext.revision].
  int _recordRevision = 0;

  /// Reads the relationship graph once. A layout with no portal and no related
  /// field never needs it, so a database whose graph cannot be read still
  /// browses: those objects draw a notice instead.
  Future<void> _loadRelationshipGraph() async {
    try {
      final results = await Future.wait([
        widget.apiClient.listRelationships(),
        widget.apiClient.listOccurrences(),
        widget.apiClient.listTables(),
      ]);
      if (!mounted) return;
      setState(() {
        _relationships = {
          for (final r in results[0] as List<RelationshipModel>) r.id: r,
        };
        _occurrences = {
          for (final o in results[1] as List<TableOccurrenceModel>) o.id: o,
        };
        _tablesById = {
          for (final t in results[2] as List<TableModel>) t.id: t,
        };
      });
    } catch (_) {
      // Left empty on purpose: see _relatedContext.
    }
  }

  /// True when this layout shows anything from the other side of a
  /// relationship, which is the only reason to read the graph.
  bool get _layoutUsesRelationships {
    final objects = widget.layout?.objects;
    if (objects == null) return false;
    return objects.any((o) => o.type == 'portal' || (o.fieldBinding?.readsRelated ?? false));
  }

  // ─── Value lists (#31) ─────────────────────────────────────────────────────

  /// The value lists of the database, and the entries each offers, resolved
  /// on demand. A list taken from a field is resolved by the server, so it
  /// reflects the data as it is now; one that includes only related values is
  /// resolved again for each record it is shown on.
  final Map<String, ValueListModel> _valueLists = {};
  final Map<String, List<ValueListItem>> _valueListItems = {};

  /// The lists that offer more entries than the server gave.
  final Set<String> _truncatedValueLists = {};

  /// Why a list taken from another solution's table (#107) offers nothing:
  /// that solution cannot be read, most often because the session is not
  /// signed in to it yet. The field offers to sign in.
  final Map<String, ExternalUnavailable> _valueListProblems = {};

  /// The external occurrences of this session: signing in to a solution
  /// there resolves again the lists that could not read it.
  late final ExternalAccess _externalAccess = ExternalAccess.of(widget.apiClient);

  void _onExternalSignedIn() {
    for (final id in _valueListProblems.keys.toList()) {
      final list = _valueLists[id];
      if (list != null) unawaited(_resolveValueList(list));
    }
  }

  /// Keeps why [list] could not be read from another solution, and asks
  /// once to sign in to it. Returns false for any other failure.
  bool _valueListUnavailable(ValueListModel list, Object error) {
    final problem = ExternalUnavailable.of(error);
    if (problem == null || !mounted) return false;
    setState(() => _valueListProblems[list.id] = problem);
    _externalAccess.askOnce(context, problem);
    return true;
  }

  /// For a list of related values: the record its entries are for, and the
  /// latest request, so an answer that arrives late never replaces a newer one.
  final Map<String, String?> _relatedValueListRecords = {};
  final Map<String, int> _relatedValueListRequests = {};

  Future<void> _loadValueLists() async {
    try {
      final lists = await widget.apiClient.listValueLists();
      if (!mounted) return;
      for (final list in lists) {
        _valueLists[list.id] = list;
        unawaited(_resolveValueList(list));
      }
    } catch (_) {
      // A database whose value lists cannot be read still browses: every
      // control falls back to an edit box.
    }
  }

  Future<void> _resolveValueList(ValueListModel list) async {
    if (!list.isFromField) {
      if (mounted) {
        setState(() => _valueListItems[list.id] = [for (final v in list.customValueList) ValueListItem(v)]);
      }
      return;
    }
    if (list.isRelated) {
      // Only a list the layout shows is worth a request per record.
      final shown = widget.layout?.objects.any((o) => o.fieldBinding?.valueListId == list.id) ?? false;
      if (!shown) return;
      final recordId = _records.isEmpty ? null : _records[_currentIndex]['id']?.toString();
      // Another record's entries are not offered while this one's are read;
      // the same record's stay until the new ones arrive.
      if (!_relatedValueListRecords.containsKey(list.id) || _relatedValueListRecords[list.id] != recordId) {
        _relatedValueListRecords[list.id] = recordId;
        _valueListItems[list.id] = const [];
        _truncatedValueLists.remove(list.id);
      }
      final request = (_relatedValueListRequests[list.id] ?? 0) + 1;
      _relatedValueListRequests[list.id] = request;
      if (recordId == null) return;
      try {
        final offer =
            await widget.apiClient.valueListItems(list.id, table: widget.table.name, recordId: recordId);
        if (mounted && _relatedValueListRequests[list.id] == request) _applyValueListOffer(list.id, offer);
      } catch (e) {
        // The control keeps what it offered: at least the stored value.
        _valueListUnavailable(list, e);
      }
      return;
    }
    try {
      final offer = await widget.apiClient.valueListItems(list.id);
      if (mounted) _applyValueListOffer(list.id, offer);
    } catch (e) {
      if (!_valueListUnavailable(list, e) && mounted) setState(() => _valueListItems[list.id] = const []);
    }
  }

  void _applyValueListOffer(String id, ValueListOffer offer) {
    setState(() {
      _valueListItems[id] = offer.items;
      _valueListProblems.remove(id);
      offer.truncated ? _truncatedValueLists.add(id) : _truncatedValueLists.remove(id);
    });
  }

  /// Resolves again the lists whose entries depend on the current record.
  void _resolveRelatedValueLists() {
    for (final list in _valueLists.values) {
      if (list.isRelated) unawaited(_resolveValueList(list));
    }
  }

  /// The entries a field object offers, or null when it is a plain edit box.
  /// A list of related values stays a control while it offers nothing: what
  /// it offers depends on the record.
  List<ValueListItem>? _itemsFor(FieldBindingModel binding) {
    final id = binding.valueListId;
    if (id == null || !FieldControlStyle.needsValueList(binding.controlStyle)) return null;
    final items = _valueListItems[id];
    if (items == null || (items.isEmpty && !(_valueLists[id]?.isRelated ?? false))) return null;
    return items;
  }

  /// The icon of a field whose value list [problem] keeps from reading another
  /// solution: a sign-in when the session is only not signed in to it.
  Widget _valueListProblemIcon(ExternalUnavailable problem) {
    if (!problem.isNotSignedIn) {
      return Tooltip(
        message: problem.message,
        child: const Icon(Icons.warning_amber, size: 14, color: Colors.amber),
      );
    }
    return IconButton(
      key: const ValueKey('value-list-sign-in'),
      icon: const Icon(Icons.login, size: 14, color: Colors.amber),
      tooltip: 'Sign in to ${problem.database}…',
      visualDensity: VisualDensity.compact,
      onPressed: () => unawaited(_externalAccess.signIn(context, problem)),
    );
  }

  /// What an entry shows: its second field only, or its value with the second
  /// field's beside it.
  static Widget _valueListLabel(ValueListItem item, {required bool showSecondOnly}) {
    const style = TextStyle(fontSize: 13);
    // A stored value the list does not offer has no second field to show.
    if (showSecondOnly && item.second != null) return Text(item.secondText, style: style);
    if (item.second == null) return Text(item.value, style: style);
    return Text.rich(
      TextSpan(children: [
        TextSpan(text: item.value),
        TextSpan(text: '  ${item.second}', style: const TextStyle(color: Colors.grey)),
      ]),
      style: style,
    );
  }

  /// Draws the control a value list fills: a drop-down, a pop-up menu, a set
  /// of radio buttons or a set of checkboxes.
  ///
  /// A checkbox set holds several values; they are kept in the field as a
  /// newline-separated list, which is how FileMaker stores them too.
  ///
  /// Each entry stores its value; with [showSecondOnly] it shows its second
  /// field instead, and so does a value already stored that the list offers.
  /// Two entries may store the same value with different second values.
  /// [truncated] says the list has more entries than it offers. [look] is
  /// what the field's conditional formats that apply give it.
  Widget _buildValueListControl({
    required String style,
    required List<ValueListItem> items,
    required String current,
    required ValueChanged<String> onChanged,
    bool showSecondOnly = false,
    bool truncated = false,
    ConditionalFormatRule? look,
  }) {
    final values = [for (final item in items) item.value];
    // An entry whose value an earlier entry also stores gets its own key.
    final seen = <String, int>{};
    String keyOf(String prefix, String value) {
      final n = seen[value] = (seen[value] ?? -1) + 1;
      return n == 0 ? '$prefix-$value' : '$prefix-$value#$n';
    }
    const more = Text(
      'More values than shown',
      key: ValueKey('value-list-truncated'),
      style: TextStyle(fontSize: 12, color: Colors.grey, fontStyle: FontStyle.italic),
    );
    switch (style) {
      case FieldControlStyle.checkboxSet:
        final stored = <String>{
          for (final v in current.split('\n'))
            if (v.trim().isNotEmpty) v.trim(),
        };
        // A stored value the list does not offer (now) stays checked, after
        // the list's, as FileMaker shows it; toggling another keeps it.
        final shown = [
          ...items,
          for (final v in stored)
            if (!values.contains(v)) ValueListItem(v),
        ];
        void toggle(String value) {
          final next = Set<String>.from(stored);
          next.contains(value) ? next.remove(value) : next.add(value);
          onChanged({for (final item in shown) if (next.contains(item.value)) item.value}.join('\n'));
        }
        return withConditionalLook(look, SingleChildScrollView(
          child: Wrap(
            spacing: 12,
            runSpacing: 2,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              for (final item in shown)
                InkWell(
                  key: ValueKey(keyOf('checkbox', item.value)),
                  onTap: () => toggle(item.value),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Checkbox(
                        visualDensity: VisualDensity.compact,
                        materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
                        value: stored.contains(item.value),
                        onChanged: (_) => toggle(item.value),
                      ),
                      Flexible(child: _valueListLabel(item, showSecondOnly: showSecondOnly)),
                    ],
                  ),
                ),
              if (truncated) more,
            ],
          ),
        ));

      case FieldControlStyle.radioButtonSet:
        return withConditionalLook(look, SingleChildScrollView(
          child: Wrap(
            spacing: 12,
            runSpacing: 2,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              for (final item in items)
                InkWell(
                  key: ValueKey(keyOf('radio', item.value)),
                  onTap: () => onChanged(item.value),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Radio<String>(
                        visualDensity: VisualDensity.compact,
                        materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
                        value: item.value,
                        groupValue: current.trim(),
                        onChanged: (v) => onChanged(v ?? ''),
                      ),
                      Flexible(child: _valueListLabel(item, showSecondOnly: showSecondOnly)),
                    ],
                  ),
                ),
              if (truncated) more,
            ],
          ),
        ));

      default:
        // Drop-down list and pop-up menu are the same control; the record may
        // hold a value the list no longer offers, so it is kept selectable.
        // A list showing only its second field shows it for the stored value
        // too; a value it no longer offers is shown as it is.
        //
        // Entries are told apart by their position, as two of them may store
        // the same value: -1 is the empty entry and -2 the note that the list
        // has more values than shown.
        final offered = List<ValueListItem>.from(items);
        final value = current.trim();
        if (value.isNotEmpty && !values.contains(value)) offered.insert(0, ValueListItem(value));
        const dash = Text('—', style: TextStyle(fontSize: 13));
        return DropdownButtonFormField<int>(
          key: const ValueKey('value-list-control'),
          isExpanded: true,
          initialValue: value.isEmpty ? null : offered.indexWhere((item) => item.value == value),
          decoration: InputDecoration(
            isDense: true,
            contentPadding: const EdgeInsets.symmetric(horizontal: 8, vertical: 8),
            border: const OutlineInputBorder(),
            fillColor: parseLayoutColor(look?.fillColor),
            filled: parseLayoutColor(look?.fillColor) != null,
          ),
          hint: dash,
          items: [
            const DropdownMenuItem<int>(value: -1, child: dash),
            for (final (i, item) in offered.indexed)
              DropdownMenuItem<int>(
                value: i,
                child: _valueListLabel(item, showSecondOnly: showSecondOnly),
              ),
            if (truncated) const DropdownMenuItem<int>(value: -2, enabled: false, child: more),
          ],
          selectedItemBuilder: (_) => [
            dash,
            for (final item in offered)
              Text(
                showSecondOnly && item.second != null ? item.secondText : item.value,
                style: conditionalTextStyle(const TextStyle(fontSize: 13), look),
              ),
            if (truncated) more,
          ],
          onChanged: (i) => onChanged(i == null || i < 0 ? '' : offered[i].value),
        );
    }
  }

  /// A calculation field is filled by its formula, so it cannot be typed into
  /// (#30). The column is still shown, with its computed value.
  bool _isComputed(String fieldName) {
    final col = widget.table.columns.where((c) => c.name == fieldName).firstOrNull;
    return col != null && (col.fieldType == 'CALCULATION' || col.fieldType == 'SUMMARY');
  }

  /// Whether a field cannot be typed into in Browse mode: a calculation or a
  /// summary, or a field whose auto-enter options prohibit modification
  /// during data entry. A script may still set the latter (Set Field).
  bool _isEntryLocked(String fieldName) => _isComputed(fieldName) || _prohibitedFields.contains(fieldName);

  /// The fields prohibiting modification, read once per table definition
  /// rather than for each box drawn.
  Set<String> get _prohibitedFields {
    final columns = widget.table.columns;
    if (!identical(columns, _prohibitedOf)) {
      _prohibitedOf = columns;
      _prohibited = {for (final c in columns) if (c.prohibitsModification) c.name};
    }
    return _prohibited;
  }

  List<ColumnModel>? _prohibitedOf;
  Set<String> _prohibited = const {};

  /// What a stored value looks like in a field box. A DATE column comes back
  /// from the API as a full RFC3339 timestamp ("2011-01-15T00:00:00Z"); the
  /// user entered a date and must see and edit a date.
  static String fieldText(ColumnModel col, Object? raw) {
    final text = raw?.toString() ?? '';
    if (col.fieldType == 'DATE' && text.length > 10 && text[10] == 'T') {
      return text.substring(0, 10);
    }
    return text;
  }

  /// The record key under which the server sends the original texts of a
  /// record's values (#124): what a migrated FileMaker field said, such as
  /// "12-A" in a Number field, beside the typed value (12) that sorting,
  /// finds, calculations and scripts use.
  static const sourceTextKey = '__source_text';

  /// The original text of [field] in [record], when it has one.
  static String? sourceText(Map<String, dynamic> record, String field) {
    final texts = record[sourceTextKey];
    if (texts is Map) {
      final text = texts[field];
      if (text is String) return text;
    }
    return null;
  }

  /// What a field of a record shows: its original text when it has one,
  /// otherwise its stored value as [fieldText] writes it.
  static String recordFieldText(ColumnModel col, Map<String, dynamic> record) =>
      sourceText(record, col.name) ?? fieldText(col, record[col.name]);

  String _recordColumnText(String colName, Map<String, dynamic> record) {
    final col = widget.table.columns.where((c) => c.name == colName).firstOrNull;
    return col == null ? (record[colName]?.toString() ?? '') : recordFieldText(col, record);
  }

  void _rebuildFieldControllers() {
    for (final c in _fieldControllers.values) {
      c.dispose();
    }
    for (final f in _fieldFocusNodes.values) {
      f.dispose();
    }
    for (final t in _fieldDebounceTimers.values) {
      t?.cancel();
    }
    _fieldControllers.clear();
    _fieldFocusNodes.clear();
    _fieldDebounceTimers.clear();
    _fieldSaving.clear();
    _resolveRelatedValueLists();

    if (_records.isEmpty) return;
    final record = _records[_currentIndex];
    // Moving to a record evaluates its hide conditions again.
    final recordId = record['id']?.toString();
    if (recordId != null) _invalidateHidden(recordId);

    for (final col in widget.table.columns) {
      if (col.isPrimaryKey) continue;
      _attachFieldController(col, col.name, _keyText(col, col.name, record));
      // The other repetitions a layout object shows of a repeating field
      // are edited under their own record key, "field[n]".
      for (var n = 2; n <= _repetitionsOnLayout(col); n++) {
        final key = ColumnModel.repetitionKey(col.name, n);
        _attachFieldController(col, key, _keyText(col, key, record));
      }
    }
  }

  /// The most repetitions of [col] an object of the current layout shows.
  int _repetitionsOnLayout(ColumnModel col) {
    if (!col.isRepeating) return 1;
    var most = 1;
    for (final o in widget.layout?.objects ?? const <LayoutObjectModel>[]) {
      final binding = o.fieldBinding;
      if (o.type != 'field' || binding == null || binding.readsRelated) continue;
      if (binding.fieldName.toLowerCase() != col.name.toLowerCase()) continue;
      most = math.max(most, binding.repetitionNumbers(col.maxRepetitions).last);
    }
    return most;
  }

  /// The field a record key names: "field[n]" is repetition n of field.
  ColumnModel? _columnForKey(String key) {
    final open = key.indexOf('[');
    final name = open > 0 && key.endsWith(']') ? key.substring(0, open) : key;
    return widget.table.columns.where((c) => c.name == name).firstOrNull;
  }

  /// What the box of record key [key] of [col] shows for [record].
  static String keyFieldText(ColumnModel col, String key, Map<String, dynamic> record) =>
      key == col.name ? recordFieldText(col, record) : fieldText(col, record[key]);

  void _attachFieldController(ColumnModel col, String key, String val) {
    final ctrl = TextEditingController(text: val);
    final fn = FocusNode();

    _fieldControllers[key] = ctrl;
    _fieldFocusNodes[key] = fn;
    _fieldDebounceTimers[key] = null;
    _fieldSaving[key] = null;

    fn.addListener(() {
      if (fn.hasFocus) {
        _lastFocusedBrowseField = col.name;
        _deactivatePortal();
        if (mounted) setState(() {});
        widget.onFocusedFieldChanged?.call();
      } else {
        _fieldDebounceTimers[key]?.cancel();
        // A field its triggers validate and save is saved as it is left,
        // by [_exitBox], once OnObjectValidate lets it.
        if (_keyGuarded(key) || _holdFieldSaves) return;
        if (col.fieldType == 'DATE' &&
            ctrl.text.trim().isNotEmpty &&
            _normalizeDateInput(ctrl.text, key) == null) {
          if (mounted) {
            setState(() => _fieldSaving[key] = false);
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(content: Text('Enter a valid date as ${_dateHint(key)} or YYYY-MM-DD.')),
            );
          }
          return;
        }
        unawaited(_saveField(key, ctrl.text));
        if (mounted) setState(() {});
        // A remote change arrived while this field was being edited: now
        // that it is saved and focus has left, reload.
        if (_pendingRemoteReload && !_aFieldIsFocused) {
          WidgetsBinding.instance.addPostFrameCallback((_) => reloadFromRemote());
        }
      }
    });
  }

  void _onFieldChanged(String colName, String newVal, {LayoutObjectModel? object}) {
    if (object != null) _triggers.post(_trigger('OnObjectModify', objectId: object.id));
    _fieldDebounceTimers[colName]?.cancel();
    if (_keyGuarded(colName) || _holdFieldSaves) return;
    _fieldDebounceTimers[colName] = Timer(
      const Duration(milliseconds: 800),
      () => _saveField(colName, newVal),
    );
  }

  /// Writes one field of the current record. [script] marks a script step's
  /// write (Set Field and the like), which may set a field that prohibits
  /// modification during data entry; a new record a script set a value in
  /// is committed as a script's write too.
  Future<bool> _saveField(String colName, String newVal, {bool script = false}) async {
    if (_records.isEmpty) return true;
    final column = _columnForKey(colName);
    final current = _records[_currentIndex];
    if (column != null && current[_draftKey] != true && newVal == _keyText(column, colName, current)) {
      // Leaving a field unchanged writes nothing. In particular it must not
      // replace a migrated value's original text with its reading (#124).
      return true;
    }
    Object? valueToSave = newVal;
    if (column?.fieldType == 'DATE') {
      if (newVal.trim().isEmpty) {
        valueToSave = null;
      } else {
        valueToSave = _normalizeDateInput(newVal, colName);
        // Do not submit partial input such as "23/06/" while the user is
        // typing. A complete valid value is sent as canonical ISO date.
        if (valueToSave == null) return false;
      }
    }
    final record = _records[_currentIndex];
    if (record[_draftKey] == true) {
      // A new record is sent as a whole when it is committed
      record[colName] = valueToSave;
      if (script) _scriptSetDrafts.add(record);
      return true;
    }
    final id = record['id']?.toString();
    if (id == null) return true;

    _recordWritten = true;
    _records[_currentIndex][colName] = valueToSave;
    if (mounted) setState(() => _fieldSaving[colName] = true);
    try {
      await _entryWrite(
        script: script,
        ({required overrideValidation}) => widget.apiClient.updateRow(
          widget.table.name,
          id,
          {colName: valueToSave},
          script: script,
          overrideValidation: overrideValidation,
        ),
      );
      // The field now holds what was typed; the server dropped its original
      // text, and so does the record in hand.
      final texts = record[sourceTextKey];
      if (texts is Map) texts.remove(colName);
      _invalidateHidden(id);
      if (mounted) {
        setState(() {
          _fieldSaving[colName] = null;
          _recordRevision++;
        });
      }
      // The field may be a match field of a list of related values.
      if (mounted && identical(record, _records.elementAtOrNull(_currentIndex))) _resolveRelatedValueLists();
      return true;
    } catch (e) {
      if (mounted) {
        setState(() => _fieldSaving[colName] = false);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Error saving "${_displayName(colName)}": $e'),
            backgroundColor: Colors.red,
            duration: const Duration(seconds: 3),
          ),
        );
      }
      return false;
    }
  }

  /// The order a date of the field under record key [key] is typed and
  /// shown in, `dmy`, `mdy` or `iso`: the date format of the layout's first
  /// box of the field that has one, else the layout's own order (the
  /// solution it was migrated from), else null.
  String? _explicitDateOrder(String key) {
    final column = _columnForKey(key)?.name.toLowerCase() ?? key.toLowerCase();
    final layout = widget.layout;
    for (final o in layout?.objects ?? const <LayoutObjectModel>[]) {
      final binding = o.fieldBinding;
      if (o.type != 'field' || binding == null || binding.readsRelated) continue;
      if (binding.fieldName.toLowerCase() != column) continue;
      final order = dateOrderOfFormat(o.style.dateFormat);
      if (order != null) return order;
    }
    return layout?.dateOrder;
  }

  /// [_explicitDateOrder], or the device's order.
  String _dateOrderOf(String? key) {
    final order = key == null ? widget.layout?.dateOrder : _explicitDateOrder(key);
    if (order == 'dmy' || order == 'mdy') return order!;
    return deviceDateOrder();
  }

  /// Lets a date be picked for record key [key] and saves it, written as
  /// the box writes dates.
  Future<void> _pickDate(String key, TextEditingController? ctrl) async {
    final current = _normalizeDateInput(ctrl?.text ?? '', key);
    final picked = await showDatePicker(
      context: context,
      initialDate: (current == null ? null : DateTime.tryParse(current)) ?? DateTime.now(),
      firstDate: DateTime(1),
      lastDate: DateTime(9999, 12, 31),
    );
    if (picked == null || !mounted) return;
    final iso = '${picked.year.toString().padLeft(4, '0')}-${picked.month.toString().padLeft(2, '0')}-'
        '${picked.day.toString().padLeft(2, '0')}';
    final text = dateInOrder(iso, _explicitDateOrder(key));
    ctrl?.text = text;
    _fieldDebounceTimers[key]?.cancel();
    await _saveField(key, text);
  }

  String _dateHint(String key) => _dateOrderOf(key) == 'mdy' ? 'MM/DD/YYYY' : 'DD/MM/YYYY';

  /// What the box of record key [key] holds while it is edited: a date in
  /// the order it is typed in when the layout says which, as it was stored
  /// otherwise.
  String _keyText(ColumnModel col, String key, Map<String, dynamic> record) {
    final text = keyFieldText(col, key, record);
    if (col.fieldType != 'DATE' || sourceText(record, key) != null) return text;
    return dateInOrder(text, _explicitDateOrder(key));
  }

  /// Converts a complete date, ISO or in the field's order ([_dateOrderOf]),
  /// into the ISO representation expected by the database. Invalid and
  /// partial dates return null.
  String? _normalizeDateInput(String input, [String? key]) =>
      normalizeDateInput(input, order: _dateOrderOf(key));

  String _displayName(String colName) {
    final col = _columnForKey(colName);
    if (col == null) return colName;
    return col.name == colName ? col.displayName : '${col.displayName}${colName.substring(col.name.length)}';
  }

  /// Marks a new record that exists only here until it is committed.
  static const _draftKey = '__f4b_draft';

  /// The new records a script set a field of: they are committed as a
  /// script's write, which may set a field prohibiting modification.
  final Set<Map<String, dynamic>> _scriptSetDrafts = Set.identity();

  bool get _onDraft => _records.isNotEmpty && _records[_currentIndex][_draftKey] == true;

  /// Commits the current record: pending field edits, or the whole new
  /// record. Returns false when the server rejected it (for example a field
  /// validation rule); the user stays on the record and sees why.
  Future<bool> _saveCurrentRecord() async {
    // FileMaker leaves the active field first (OnObjectValidate,
    // OnObjectSave, OnObjectExit), then runs OnRecordCommit, whose False
    // keeps the record uncommitted (#26): a new record is not created, and
    // what is typed and not saved yet stays unsaved. A field saved when it
    // was left stays saved.
    if (!await _leaveActiveBox()) return false;
    if (_recordDirty && !await _triggers.fire(_trigger('OnRecordCommit'))) {
      // The record stays uncommitted: what is typed waits for the next
      // commit, rather than being saved by a field's pending save.
      _holdFieldSaves = true;
      for (final timer in _fieldDebounceTimers.values) {
        timer?.cancel();
      }
      return false;
    }
    _holdFieldSaves = false;
    // Every box: the fields, and the repetitions of repeating fields.
    for (final key in _fieldControllers.keys.toList()) {
      final col = _columnForKey(key);
      if (col == null || col.isPrimaryKey) continue;
      final ctrl = _fieldControllers[key];
      if (ctrl == null) continue;
      _fieldDebounceTimers[key]?.cancel();
      final cached = _records.isNotEmpty
          ? _keyText(col, key, _records[_currentIndex])
          : '';
      if (ctrl.text != cached) {
        if (col.fieldType == 'DATE' &&
            ctrl.text.trim().isNotEmpty &&
            _normalizeDateInput(ctrl.text, key) == null) {
          if (mounted) {
            setState(() => _fieldSaving[key] = false);
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(content: Text('Enter a valid date as ${_dateHint(key)} or YYYY-MM-DD.')),
            );
          }
          return false;
        }
        await _saveField(key, ctrl.text);
      }
    }
    if (_onDraft) return _commitDraft();
    _recordWritten = false;
    return true;
  }

  /// Creates the new record on the server, like FileMaker commits a new
  /// record: validation rules apply to the record as a whole, and fields the
  /// user left empty are not sent, so their auto-enter values apply.
  Future<bool> _commitDraft() async {
    final draft = _records[_currentIndex];
    final values = <String, dynamic>{
      for (final col in widget.table.columns)
        for (var n = 1; n <= _repetitionsOnLayout(col); n++)
          if (!col.isPrimaryKey && (draft[ColumnModel.repetitionKey(col.name, n)]?.toString() ?? '').isNotEmpty)
            ColumnModel.repetitionKey(col.name, n): draft[ColumnModel.repetitionKey(col.name, n)],
    };
    try {
      final script = _scriptSetDrafts.contains(draft);
      final created = await _entryWrite(
        script: script,
        ({required overrideValidation}) => widget.apiClient.insertRow(
          widget.table.name,
          values,
          script: script,
          overrideValidation: overrideValidation,
        ),
      );
      _scriptSetDrafts.remove(draft);
      // Committing a new record loads no other record.
      _loadedRecordKey = created['id']?.toString();
      _recordWritten = false;
      if (mounted) {
        setState(() {
          _records[_currentIndex] = created;
          // The boxes show what the server entered (serial numbers,
          // stamps, calculated and looked-up values), so a later save does
          // not write the draft's empty text back over them.
          _showRecordInFields(created);
        });
      }
      return true;
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('The new record was not saved: $e'), backgroundColor: Colors.red),
        );
      }
      return false;
    }
  }

  /// Puts [record]'s values in the field boxes already shown, without
  /// rebuilding them, so the box being typed in keeps its focus.
  void _showRecordInFields(Map<String, dynamic> record) {
    for (final entry in _fieldControllers.entries) {
      final col = _columnForKey(entry.key);
      if (col == null || col.isPrimaryKey) continue;
      _fieldDebounceTimers[entry.key]?.cancel();
      final text = keyFieldText(col, entry.key, record);
      if (entry.value.text != text) entry.value.text = text;
    }
  }

  void _discardDraft() {
    _records.removeAt(_currentIndex);
    if (_currentIndex >= _records.length) _currentIndex = _records.length - 1;
    if (_currentIndex < 0) _currentIndex = 0;
    _rebuildFieldControllers();
    _recordChanged();
  }

  // ─── Script triggers of the layout and of its fields (#26) ────────────────

  late final ScriptTriggerRuntime _triggers = ScriptTriggerRuntime(
    perform: (trigger) => _actionRunner().run(trigger),
    onStopped: (message) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(message), backgroundColor: Colors.orange.shade800),
      );
    },
  );

  /// The enabled trigger of [event] set on the layout, or on its object
  /// [objectId], that runs in the window's mode (or [mode]). Preview mode
  /// runs none.
  ButtonActionModel? _trigger(String event, {String? objectId, OperationalMode? mode}) {
    final m = mode ?? _mode;
    if (m != OperationalMode.browse && m != OperationalMode.find) return null;
    return widget.layout?.runningTrigger(event, objectId: objectId, mode: m == OperationalMode.find ? 'find' : 'browse');
  }

  @override
  int actionTriggerMark() => _triggers.mark();

  @override
  Future<void> actionSettleTriggers(int since) => _triggers.settle(since);

  /// Whether the field object has a trigger that runs as it is entered or
  /// left.
  bool _boxHasTriggers(LayoutObjectModel object) => const [
        'OnObjectEnter',
        'OnObjectExit',
        'OnObjectValidate',
        'OnObjectSave',
      ].any((e) => _trigger(e, objectId: object.id) != null);

  /// Whether the field object's value is validated and saved by its
  /// triggers as it is left, rather than saved as it is typed.
  bool _guarded(LayoutObjectModel object) =>
      _trigger('OnObjectValidate', objectId: object.id) != null || _trigger('OnObjectSave', objectId: object.id) != null;

  /// Whether the box of record key [key] is [_guarded] by an object of the
  /// layout showing that field.
  bool _keyGuarded(String key) {
    final col = _columnForKey(key);
    final layout = widget.layout;
    if (col == null || layout == null || layout.scriptTriggers.isEmpty) return false;
    return layout.objects.any((o) =>
        o.type == 'field' &&
        o.fieldBinding != null &&
        !o.fieldBinding!.isRelated &&
        o.fieldBinding!.fieldName.toLowerCase() == col.name.toLowerCase() &&
        _guarded(o));
  }

  /// The field box FileMaker calls the active field: the one entered last,
  /// by the user or a script step, until it is left.
  TriggerBox? _activeBox;

  /// What the focus did, handled one change after the other.
  Future<void> _focusChain = Future<void>.value();

  void _boxFocusChanged(TriggerBox box, bool focused) {
    _focusChain = _focusChain
        .then((_) => focused ? _enterBox(box) : _leaveBoxOnBlur(box))
        .catchError((Object _) {});
  }

  /// The user put the focus in [box]: the active field is left, unless its
  /// triggers keep it active, and [box] is entered.
  Future<void> _enterBox(TriggerBox box) async {
    if (!mounted || box.same(_activeBox) || !box.hasFocus()) return;
    final previous = _activeBox;
    if (previous != null && !await _exitBox(previous)) {
      await _refocus(previous);
      return;
    }
    _activateBox(box);
    await _triggers.fire(_trigger('OnObjectEnter', objectId: box.object.id));
  }

  /// The focus left [box] for something that is not a field box.
  Future<void> _leaveBoxOnBlur(TriggerBox box) async {
    if (!mounted || !box.same(_activeBox) || box.hasFocus()) return;
    if (!await _exitBox(box)) {
      await _refocus(box);
      return;
    }
    if (box.same(_activeBox)) _activeBox = null;
  }

  /// Puts the focus back in a box its triggers keep active, and lets the
  /// focus changes that makes settle before the next one is handled.
  Future<void> _refocus(TriggerBox box) async {
    if (!mounted) return;
    box.focus();
    await Future<void>.delayed(Duration.zero);
  }

  /// Leaves [box] as FileMaker leaves a field: a changed value is validated
  /// (OnObjectValidate) and saved, then OnObjectSave and OnObjectExit run.
  /// False when one of them, or the save, keeps the field active.
  Future<bool> _exitBox(TriggerBox box) async {
    final id = box.object.id;
    if (!box.find && _guarded(box.object) && box.modified()) {
      // The script validates the value typed, read from the record.
      final unstage = box.stage?.call();
      final valid = await _triggers.fire(_trigger('OnObjectValidate', objectId: id));
      unstage?.call();
      if (!valid) return false;
      if (!await box.save()) return false;
      if (!await _triggers.fire(_trigger('OnObjectSave', objectId: id))) return false;
    }
    return _triggers.fire(_trigger('OnObjectExit', objectId: id));
  }

  /// Leaves the active field before the record is committed or the mode
  /// left; false when its triggers keep it active.
  Future<bool> _leaveActiveBox() async {
    final box = _activeBox;
    if (box == null) return true;
    if (!_boxHasTriggers(box.object)) {
      _activeBox = null;
      return true;
    }
    if (!await _exitBox(box)) {
      if (!box.hasFocus()) box.focus();
      return false;
    }
    if (box.same(_activeBox)) _activeBox = null;
    if (box.hasFocus()) FocusManager.instance.primaryFocus?.unfocus();
    return true;
  }

  void _activateBox(TriggerBox box) {
    _activeBox = box;
    final portal = box.portal;
    if (portal != null) {
      final rows = _portalRowController(portal);
      _makePortalActive(portal.id);
      rows.activate(box.rowNumber);
    } else {
      _deactivatePortal();
    }
  }

  /// The box of the field object [obj] of the record in hand, under its
  /// record [key].
  TriggerBox _fieldBox(LayoutObjectModel obj, ColumnModel col, String key) => TriggerBox(
        object: obj,
        key: key,
        modified: () {
          final ctrl = _fieldControllers[key];
          if (ctrl == null || _records.isEmpty) return false;
          return ctrl.text != keyFieldText(col, key, _records[_currentIndex]);
        },
        stage: () {
          final ctrl = _fieldControllers[key];
          if (ctrl == null || _records.isEmpty) return () {};
          final record = _records[_currentIndex];
          final had = record.containsKey(key);
          final before = record[key];
          final texts = record[sourceTextKey];
          final text = texts is Map ? texts.remove(key) : null;
          final typed = ctrl.text;
          final staged = col.fieldType == 'DATE' && typed.trim().isEmpty
              ? null
              : (col.fieldType == 'DATE' ? _normalizeDateInput(typed) ?? typed : typed);
          record[key] = staged;
          return () {
            // A value the script set meanwhile stays.
            if (record[key] != staged) return;
            if (had) {
              record[key] = before;
            } else {
              record.remove(key);
            }
            if (text != null && texts is Map) texts[key] = text;
          };
        },
        save: () async {
          final ctrl = _fieldControllers[key];
          if (ctrl == null) return true;
          _fieldDebounceTimers[key]?.cancel();
          if (col.fieldType == 'DATE' && ctrl.text.trim().isNotEmpty && _normalizeDateInput(ctrl.text) == null) {
            if (mounted) {
              setState(() => _fieldSaving[key] = false);
              ScaffoldMessenger.of(context).showSnackBar(
                const SnackBar(content: Text('Enter a valid date as DD/MM/YYYY or YYYY-MM-DD.')),
              );
            }
            return false;
          }
          return _saveField(key, ctrl.text);
        },
        focus: () => _fieldFocusNodes[key]?.requestFocus(),
        hasFocus: () => _fieldFocusNodes[key]?.hasFocus ?? false,
      );

  /// The box of a field object in Find mode: its text is a criterion.
  TriggerBox _findBox(LayoutObjectModel obj, String key, FocusNode node) => TriggerBox(
        object: obj,
        key: key,
        find: true,
        modified: () => false,
        save: () async => true,
        focus: node.requestFocus,
        hasFocus: () => node.hasFocus,
      );

  final Map<String, FocusNode> _findFocusNodes = {};

  /// Tells the triggers when the focus enters or leaves [box].
  Widget _withTriggers(TriggerBox Function() box, Widget child) => Focus(
        canRequestFocus: false,
        skipTraversal: true,
        onFocusChange: (focused) => _boxFocusChanged(box(), focused),
        child: child,
      );

  /// A value-list control of a field its triggers validate and save: the
  /// value chosen is validated and saved at once, as it is in FileMaker.
  Future<void> _validateAndSave(TriggerBox box) async {
    if (!box.modified()) return;
    if (!await _triggers.fire(_trigger('OnObjectValidate', objectId: box.object.id))) {
      if (mounted) setState(_showRecordInFieldsAgain);
      return;
    }
    if (!await box.save()) return;
    await _triggers.fire(_trigger('OnObjectSave', objectId: box.object.id));
  }

  void _showRecordInFieldsAgain() {
    if (_records.isNotEmpty) _showRecordInFields(_records[_currentIndex]);
  }

  // Record triggers: the record loaded last, and whether it was written
  // since.
  static final Object _notLoaded = Object();
  Object? _loadedRecordKey;
  bool _recordWritten = false;

  /// Set once the window has run its OnLayoutEnter and OnModeEnter, before
  /// which no OnRecordLoad runs.
  bool _recordLoadArmed = false;

  /// The record in hand changed, or may have: tells the window, and runs
  /// OnRecordLoad when it is another record.
  void _recordChanged() {
    widget.onRecordChanged?.call(_currentIndex, _records.length);
    _noteRecordShown();
  }

  /// Runs OnRecordLoad when the record in hand is not the one loaded last,
  /// or [again] for a layout gone to or a mode entered.
  void _noteRecordShown({bool again = false}) {
    if (again) _loadedRecordKey = _notLoaded;
    // Records being read (another table's layout gone to): the record is
    // loaded once they are, not the one shown before.
    if (_isLoading) return;
    final record = currentRecord;
    final Object? key = record == null ? null : (record[_draftKey] == true ? record : record['id']?.toString());
    if (key == _loadedRecordKey) return;
    _loadedRecordKey = key;
    _recordWritten = false;
    _holdFieldSaves = false;
    _activeBox = null;
    _deactivatePortal(keepParent: key is String ? key : null);
    if (key != null && _recordLoadArmed && _mode == OperationalMode.browse) {
      _triggers.post(_trigger('OnRecordLoad'));
    }
  }

  /// Set when OnRecordCommit kept the record uncommitted: its fields are no
  /// longer saved as they are typed or left, until it is committed,
  /// reverted or left.
  bool _holdFieldSaves = false;

  /// Whether the record in hand has changes to commit: a new record, a
  /// field written since it was loaded, or text typed and not saved yet.
  bool get _recordDirty {
    if (_records.isEmpty) return false;
    if (_recordWritten || _onDraft) return true;
    final record = _records[_currentIndex];
    for (final entry in _fieldControllers.entries) {
      final col = _columnForKey(entry.key);
      if (col == null || col.isPrimaryKey) continue;
      if (entry.value.text != keyFieldText(col, entry.key, record)) return true;
    }
    return false;
  }

  /// Leaves the mode the window is in: the active field, then OnModeExit,
  /// whose False keeps the window in its mode.
  Future<bool> _leaveMode() async {
    if (!await _leaveActiveBox()) return false;
    return _triggers.fire(_trigger('OnModeExit'));
  }

  /// Asked by the window before it switches this layout to another mode
  /// from its toolbar or menus: false when the layout's OnModeExit trigger,
  /// or the active field's, keeps it in the mode it is in.
  Future<bool> confirmModeExit(OperationalMode to) async {
    if (_mode == to || !_recordLoadArmed) return true;
    return _leaveMode();
  }

  /// The window has entered [mode]: OnModeEnter runs, and in Browse mode the
  /// record in hand is loaded.
  void _enteredMode(OperationalMode mode) {
    _activeBox = null;
    _deactivatePortal();
    _triggers.post(_trigger('OnModeEnter', mode: mode));
    if (mode != OperationalMode.browse) return;
    _loadedRecordKey = _notLoaded;
    // The records of a find, or of Show All Records, may still be read.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && !_isLoading && identical(_loadedRecordKey, _notLoaded)) _noteRecordShown();
    });
  }

  // Portal rows (#26): the controller of each portal of the layout in hand,
  // by object id, and the portal whose row is active.
  final Map<String, PortalRowController> _portalRows = {};
  String? _activePortalId;

  /// The portal whose row is active, while it shows the related records of
  /// the record in hand: a portal still showing the record left has none.
  PortalRowController? get _activePortal {
    final rows = _activePortalId == null ? null : _portalRows[_activePortalId];
    if (rows == null || !_portalOfCurrentRecord(rows)) return null;
    return rows;
  }

  bool _portalOfCurrentRecord(PortalRowController rows) {
    final parent = rows.parentId;
    final current = currentRecord?['id']?.toString();
    return parent == null || current == null || parent == current;
  }

  /// The controller of [portal]; in a list, of the portal of the list row
  /// of record [listRecordId], which runs no object triggers.
  PortalRowController _portalRowController(LayoutObjectModel portal, {String? listRecordId}) {
    final key = listRecordId == null ? portal.id : '${portal.id}@$listRecordId';
    return _portalRows.putIfAbsent(key, () {
      final rows = PortalRowController(portal)
        ..onActivated = () => _makePortalActive(key);
      if (listRecordId == null) {
        rows
          ..onCellFocus = _boxFocusChanged
          ..guarded = _guarded
          ..onCellModified = (content) => _triggers.post(_trigger('OnObjectModify', objectId: content.id));
      }
      return rows;
    });
  }

  void _makePortalActive(String key) {
    for (final entry in _portalRows.entries) {
      if (entry.key != key) entry.value.activate(0);
    }
    _activePortalId = key;
  }

  /// Leaves the active portal row, unless [keepParent] is the record whose
  /// portal it is (a list row's portal clicked into, then its record gone to).
  void _deactivatePortal({String? keepParent}) {
    final active = _activePortalId == null ? null : _portalRows[_activePortalId];
    if (keepParent != null && active != null && active.activeRow > 0 && active.parentId == keepParent) {
      _makePortalActive(_activePortalId!);
      return;
    }
    for (final rows in _portalRows.values) {
      rows.activate(0);
    }
    _activePortalId = null;
  }

  @override
  ActivePortalRow? actionActivePortalRow({String? occurrence}) {
    final rows = _activePortal;
    final record = rows?.activeRecord;
    final target = rows?.target;
    if (rows == null || record == null || target == null) return null;
    if (occurrence != null) {
      final wanted = occurrence.trim().toLowerCase();
      if (wanted != target.occurrence.name.toLowerCase() && wanted != target.occurrence.id.toLowerCase()) return null;
    }
    final rowId = record['id']?.toString() ?? '';
    final write = rows.writeRow;
    return ActivePortalRow(
      portalObjectId: rows.object.id,
      portalName: rows.object.name ?? '',
      occurrence: target.occurrence.name,
      table: target.table.name,
      rowNumber: rows.activeRow,
      recordId: rowId,
      record: Map<String, dynamic>.unmodifiable(record),
      write: write == null ? null : (values, {script = false}) => write(rowId, values, script: script),
    );
  }

  @override
  int get actionActivePortalRowNumber => _activePortal?.activeRow ?? 0;

  @override
  String get actionActiveFieldTableName {
    final box = _activeBox;
    final portal = box?.portal;
    if (portal != null) return _portalRows[portal.id]?.target?.occurrence.name ?? '';
    if (box != null || _aFieldIsFocused) {
      final occurrence = widget.layout?.tableOccurrence;
      return occurrence == null || occurrence.isEmpty ? widget.table.name : occurrence;
    }
    return '';
  }

  @override
  void initState() {
    super.initState();
    _initFindControllers();
    _externalAccess.addListener(_onExternalSignedIn);
    unawaited(_loadValueLists());
    if (_layoutUsesRelationships) unawaited(_loadRelationshipGraph());
    unawaited(_fetchRecords().then((_) async {
      // A window opens on OnLayoutEnter, then OnModeEnter, then
      // OnRecordLoad (#26).
      if (mounted && widget.mode == OperationalMode.browse) await _runLayoutTrigger(widget.layout?.activeOnLayoutEnter);
      if (!mounted) return;
      await _triggers.fire(_trigger('OnModeEnter'));
      if (!mounted) return;
      _recordLoadArmed = true;
      _noteRecordShown(again: true);
    }));
    HardwareKeyboard.instance.addHandler(_handleRecordShortcut);
  }

  bool _runningLayoutTrigger = false;

  /// The runner of this window's buttons and triggers. A step that reaches
  /// another solution runs on the session this one opened there, and asks
  /// to sign in to it when there is none (#107).
  LayoutActionRunner _actionRunner() => LayoutActionRunner(
        apiClient: widget.apiClient,
        host: this,
        peerTokenProvider: (solution) => _externalAccess.peerToken(context, solution),
      );

  /// Runs an OnLayoutEnter / OnLayoutExit script. A trigger started while
  /// another one runs (e.g. the script itself goes to another layout) is
  /// skipped, so layouts that switch to each other cannot loop forever. A
  /// trigger switched off — as every migrated one arrives — never runs.
  Future<void> _runLayoutTrigger(ButtonActionModel? action) async {
    if (action == null || !action.enabled || _runningLayoutTrigger) return;
    _runningLayoutTrigger = true;
    try {
      final result = await _actionRunner().run(action);
      if (!result.completed && mounted && result.message != null) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(result.message!), backgroundColor: Colors.orange.shade800),
        );
      }
    } finally {
      _runningLayoutTrigger = false;
    }
  }

  /// Cmd/Ctrl + Up / Down moves to the previous / next record in Browse
  /// mode, also while a field is being edited.
  bool _handleRecordShortcut(KeyEvent event) {
    if (event is! KeyDownEvent || widget.mode != OperationalMode.browse || !mounted) return false;
    final kb = HardwareKeyboard.instance;
    if (!(kb.isMetaPressed || kb.isControlPressed) || kb.isShiftPressed || kb.isAltPressed) return false;
    if (ModalRoute.of(context)?.isCurrent == false) return false; // a dialog is open
    if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
      previousRecord();
      return true;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
      nextRecord();
      return true;
    }
    return false;
  }

  @override
  void didUpdateWidget(covariant DataBrowserWidget oldWidget) {
    super.didUpdateWidget(oldWidget);
    // The parent has built with the mode it holds now. A mode this widget
    // asked for has already been set up by [_requestMode].
    final requestedHere = _requestedMode != null && _requestedMode == widget.mode;
    _requestedMode = null;
    final columnsChanged = oldWidget.table.columns.length != widget.table.columns.length ||
        !_sameColumns(oldWidget.table.columns, widget.table.columns);
    final layoutChanged = oldWidget.layout != widget.layout ||
        oldWidget.layout?.id != widget.layout?.id ||
        oldWidget.layout?.objects.length != widget.layout?.objects.length ||
        oldWidget.mode != widget.mode;
    if (oldWidget.layout?.id != widget.layout?.id) {
      // Another layout: its own fields and portals, none of them active.
      _activeBox = null;
      _activePortalId = null;
      _portalRows.clear();
    }
    if (oldWidget.layout?.id != widget.layout?.id && widget.mode == OperationalMode.browse) {
      final exit = oldWidget.layout?.activeOnLayoutExit;
      final enter = widget.layout?.activeOnLayoutEnter;
      final load = _recordLoadArmed && _trigger('OnRecordLoad') != null;
      if (exit != null || enter != null || load) {
        WidgetsBinding.instance.addPostFrameCallback((_) async {
          await _runLayoutTrigger(exit);
          if (mounted) await _runLayoutTrigger(enter);
          // The record is loaded on the layout gone to.
          if (mounted) _noteRecordShown(again: true);
        });
      }
    } else if (oldWidget.mode != widget.mode && !requestedHere && _recordLoadArmed) {
      // The mode was switched from the window's toolbar or menus.
      final mode = widget.mode;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && _mode == mode) _enteredMode(mode);
      });
    }
    if (oldWidget.table.id != widget.table.id || columnsChanged || layoutChanged) {
      final onlyRequestedMode = requestedHere &&
          oldWidget.table.id == widget.table.id &&
          !columnsChanged &&
          oldWidget.layout == widget.layout;
      if (!onlyRequestedMode) _initFindControllers();
      _rebuildFieldControllers();
      if (oldWidget.table.id != widget.table.id) {
        unawaited(_fetchRecords());
      }
    }
    // Switching to a layout that shows the other side of a relationship for
    // the first time: the graph has not been read yet (#36).
    if (layoutChanged && _layoutUsesRelationships && _relationships.isEmpty) {
      unawaited(_loadRelationshipGraph());
    }
    // A new layout brings its own charts and its own grouping, and neither is
    // read by fetching records — which switching layouts does not do anyway
    // (#32, #37).
    if (layoutChanged && oldWidget.layout?.id != widget.layout?.id) {
      _chartFigures.clear();
      unawaited(_loadChartFigures());
      if (_layoutIsReport) {
        _reportFigures = const {};
        unawaited(_loadReportFigures());
      }
    }
  }

  bool _sameColumns(List<ColumnModel> a, List<ColumnModel> b) {
    if (a.length != b.length) return false;
    for (var i = 0; i < a.length; i++) {
      if (a[i].id != b[i].id || a[i].displayName != b[i].displayName || a[i].name != b[i].name) {
        return false;
      }
    }
    return true;
  }

  void _initFindControllers() {
    _findControllers.clear();
    for (final col in widget.table.columns) {
      _findControllers[col.name] = TextEditingController();
    }
    // A new table means the previous requests no longer refer to anything.
    _findRequests
      ..clear()
      ..add(FindRequestDraft());
    _findRequestIndex = 0;
  }

  @override
  void dispose() {
    _externalAccess.removeListener(_onExternalSignedIn);
    HardwareKeyboard.instance.removeHandler(_handleRecordShortcut);
    _recordNumberCtrl.dispose();
    _recordNumberFocus.dispose();
    for (final c in _findControllers.values) {
      c.dispose();
    }
    for (final f in _findFocusNodes.values) {
      f.dispose();
    }
    for (final c in _fieldControllers.values) {
      c.dispose();
    }
    for (final f in _fieldFocusNodes.values) {
      f.dispose();
    }
    for (final t in _fieldDebounceTimers.values) {
      t?.cancel();
    }
    super.dispose();
  }

  Future<void> _fetchRecords({bool preserveCurrent = false, bool loadAll = false, bool retried = false}) async {
    final keepId = preserveCurrent && _records.isNotEmpty
        ? _records[_currentIndex]['id']?.toString()
        : null;
    final keepFoundSet = loadAll && _isFoundSet;
    final findRequests =
        keepFoundSet ? (_foundSetFind ?? _findRequestPayload()) : const <Map<String, dynamic>>[];
    setState(() {
      _isLoading = true;
      _error = null;
      if (!preserveCurrent && !keepFoundSet) _isFoundSet = false;
      if (!keepFoundSet) {
        _foundSetFind = null;
        _foundSetInHand = false;
        _omittedIds.clear();
      }
    });
    try {
      // A report covers the whole found set: its totals would not match the
      // rows under them if it only read the first page (#32).
      // The record list must also be able to browse beyond the first page
      // (#85). If a find is active, load every row in that found set.
      late final List<Map<String, dynamic>> rows;
      if (_layoutIsReport || loadAll) {
        rows = keepFoundSet
            ? await widget.apiClient.executeFindAll(widget.table.name, findRequests)
            : await widget.apiClient.listAllRows(widget.table.name);
      } else {
        rows = await widget.apiClient.listRows(widget.table.name);
      }
      if (keepFoundSet) rows.removeWhere((r) => _omittedIds.contains(r['id']?.toString()));
      if (mounted) {
        setState(() {
          _records = rows;
          _unsortedRecords = List.of(rows);
          // Read again, related records may have changed too.
          _invalidateHidden();
          if (!keepFoundSet) _totalInTable = rows.length;
          _sortOrder = const [];
          final keptIdx = keepId == null ? -1 : rows.indexWhere((r) => r['id']?.toString() == keepId);
          _currentIndex = keptIdx >= 0 ? keptIdx : 0;
          _isLoading = false;
        });
        _rebuildFieldControllers();
        _recordChanged();
        if (_layoutIsReport) unawaited(_loadReportFigures());
        unawaited(_loadChartFigures());
        if (!keepFoundSet) widget.onFoundSetChanged?.call(const []);
      }
    } catch (e) {
      if (!mounted) return;
      final problem = ExternalUnavailable.of(e);
      setState(() { _error = problem?.message ?? e.toString(); _isLoading = false; });
      // Another solution's records (#107) the session is not signed in to
      // yet: sign in to it, then read them again, once.
      if (problem != null && problem.isNotSignedIn && !retried &&
          await _externalAccess.signIn(context, problem) && mounted) {
        await _fetchRecords(preserveCurrent: preserveCurrent, loadAll: loadAll, retried: true);
      }
    }
  }

  /// New Record: an empty record shown at the end of the found set. It is
  /// created on the server when committed (see [_commitDraft]).
  Future<void> _createNewRecord() async {
    if (!_canCreate || !await _saveCurrentRecord() || !mounted) return;
    setState(() {
      _records.add(<String, dynamic>{_draftKey: true});
      _currentIndex = _records.length - 1;
    });
    _rebuildFieldControllers();
    _recordChanged();
  }

  Future<void> _deleteCurrentRecord() async {
    if (_records.isEmpty || (!_onDraft && !_canDelete)) return;
    for (final t in _fieldDebounceTimers.values) {
      t?.cancel();
    }
    if (_onDraft) {
      setState(_discardDraft);
      return;
    }
    final id = _records[_currentIndex]['id']?.toString();
    if (id == null) return;
    try {
      await _write(() => widget.apiClient.deleteRow(widget.table.name, id));
      await _fetchRecords();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Error deleting record: $e'), backgroundColor: Colors.red),
        );
      }
    }
  }

  /// Translates what the user typed into one find request's criteria. The
  /// operators are the ones the Find mode toolbar offers.
  /// The key a criterion on a *related* field is held under, in the find
  /// controllers and in a saved find's values (#46). An own field is held
  /// under its plain name, so the two cannot collide: a field name is
  /// lower-case letters, digits and underscores, and never contains a colon.
  /// [side] is the end of a relationship joining an occurrence to itself
  /// ([RelationshipSide]): the same field read from either end is two
  /// criteria. A key without one, as saved before, reads the right end.
  static String relatedCriterionKey(String relationshipId, String occurrence, String field, {String? side}) =>
      'rel:$relationshipId:$occurrence:$field${side == null ? '' : ':$side'}';

  /// Reads back what [relatedCriterionKey] wrote, or null for an own field.
  static ({String relationshipId, String occurrence, String field, String? side})? parseRelatedCriterionKey(
      String key) {
    if (!key.startsWith('rel:')) return null;
    final parts = key.split(':');
    if (parts.length < 4) return null;
    String? side;
    if (parts.length > 4 && (parts.last == RelationshipSide.left || parts.last == RelationshipSide.right)) {
      side = parts.removeLast();
    }
    return (
      relationshipId: parts[1],
      occurrence: parts[2],
      // A field name has no colon, but rejoin anyway rather than lose text.
      field: parts.sublist(3).join(':'),
      side: side,
    );
  }

  static List<Map<String, dynamic>> criteriaFor(Map<String, String> values) {
    final criteria = <Map<String, dynamic>>[];
    values.forEach((key, raw) {
      final related = parseRelatedCriterionKey(key);
      final fieldName = related?.field ?? key;
      var text = raw.trim();
      if (text.isEmpty) return;

      // Standard today's date formula: // -> current ISO date
      final todayStr = DateTime.now().toIso8601String().split('T').first;
      if (text == '//') {
        text = todayStr;
      } else if (text.contains('//')) {
        text = text.replaceAll('//', todayStr);
      }

      if (text == '=') {
        // Find empty field
        criteria.add({'field_name': fieldName, 'operator': 'IS_EMPTY', 'value': ''});
      } else if (text == '*') {
        // Find non-empty field
        criteria.add({'field_name': fieldName, 'operator': 'IS_NOT_EMPTY', 'value': ''});
      } else if (text.contains('...')) {
        final parts = text.split('...');
        criteria.add({'field_name': fieldName, 'operator': 'RANGE', 'value': parts[0].trim(), 'value_to': parts[1].trim()});
      } else if (text.startsWith('>=')) {
        criteria.add({'field_name': fieldName, 'operator': '>=', 'value': text.substring(2).trim()});
      } else if (text.startsWith('<=')) {
        criteria.add({'field_name': fieldName, 'operator': '<=', 'value': text.substring(2).trim()});
      } else if (text.startsWith('>')) {
        criteria.add({'field_name': fieldName, 'operator': '>', 'value': text.substring(1).trim()});
      } else if (text.startsWith('<')) {
        criteria.add({'field_name': fieldName, 'operator': '<', 'value': text.substring(1).trim()});
      } else if (text.startsWith('!=')) {
        criteria.add({'field_name': fieldName, 'operator': '!=', 'value': text.substring(2).trim()});
      } else if (text.startsWith('!')) {
        criteria.add({'field_name': fieldName, 'operator': '!=', 'value': text.substring(1).trim()});
      } else if (text.startsWith('==')) {
        criteria.add({'field_name': fieldName, 'operator': '==', 'value': text.substring(2).trim()});
      } else if (text.startsWith('=')) {
        criteria.add({'field_name': fieldName, 'operator': '=', 'value': text.substring(1).trim()});
      } else if (text.contains('*') || text.contains('@') || text.contains('?')) {
        final wildcard = text.replaceAll('*', '%').replaceAll('@', '_').replaceAll('?', '_');
        criteria.add({'field_name': fieldName, 'operator': 'LIKE', 'value': wildcard});
      } else {
        criteria.add({'field_name': fieldName, 'operator': 'LIKE', 'value': '%$text%'});
      }

      // A criterion on a related field names the relationship it reaches
      // through, so the server finds the records whose related records match.
      if (related != null && criteria.isNotEmpty) {
        criteria.last['relationship_id'] = related.relationshipId;
        if (related.occurrence.isNotEmpty) {
          criteria.last['occurrence'] = related.occurrence;
        }
        if (related.side != null) {
          criteria.last['side'] = related.side;
        }
      }
    });
    return criteria;
  }

  /// Every request that has something in it, in the order they were created.
  /// An omitting request subtracts from what the others found.
  List<Map<String, dynamic>> _findRequestPayload() {
    _captureCurrentFindRequest();
    final payload = <Map<String, dynamic>>[];
    for (final request in _findRequests) {
      final criteria = criteriaFor(request.values);
      if (criteria.isEmpty) continue;
      payload.add({'criteria': criteria, 'omit': request.omit});
    }
    return payload;
  }

  Future<void> _performFind() async {
    final requests = _findRequestPayload();
    // OnModeExit decides before the found set is replaced (#26).
    if (!await _mayLeaveModeFor(OperationalMode.browse) || !mounted) return;

    setState(() { _isLoading = true; _error = null; });
    try {
      final results = _layoutIsReport
          ? await widget.apiClient.executeFindAll(widget.table.name, requests)
          : await widget.apiClient.executeFind(widget.table.name, requests);
      if (!mounted) return;

      if (results.isEmpty) {
        setState(() { _isLoading = false; });
        await showDialog(
          context: context,
          builder: (ctx) => AlertDialog(
            title: const Row(
              children: [
                Icon(Icons.search_off_rounded, color: Color(0xFFE53935)),
                SizedBox(width: 10),
                Text('No Records Found', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
              ],
            ),
            content: const Text(
              'No records match the specified find criteria.\n\nYou can modify your search terms or cancel to return to all records.',
              style: TextStyle(fontSize: 13),
            ),
            actions: [
              OutlinedButton(
                onPressed: () {
                  Navigator.of(ctx).pop();
                  unawaited(_fetchRecords());
                  _applyMode(OperationalMode.browse);
                },
                child: const Text('Show All Records (Cancel)'),
              ),
              FilledButton(
                onPressed: () => Navigator.of(ctx).pop(),
                child: const Text('Modify Criteria'),
              ),
            ],
          ),
        );
      } else {
        setState(() {
          _records = results;
          _unsortedRecords = List.of(results);
          _sortOrder = const [];
          _currentIndex = 0;
          _isFoundSet = true;
          _foundSetFind = requests;
          _foundSetInHand = false;
          _omittedIds.clear();
          _isLoading = false;
        });
        _rebuildFieldControllers();
        _recordChanged();
        _applyMode(OperationalMode.browse);
        // A report totals the found set, so its figures are read again for
        // the set the find just returned (#32).
        if (_layoutIsReport) unawaited(_loadReportFigures());
        unawaited(_loadChartFigures());
        widget.onFoundSetChanged?.call(requests);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Row(
              children: [
                const Icon(Icons.check_circle_outline, color: Colors.white, size: 18),
                const SizedBox(width: 8),
                Text('Found ${results.length} record(s) matching criteria'),
              ],
            ),
            backgroundColor: const Color(0xFF2E7D32),
            action: SnackBarAction(
              label: 'Show All',
              textColor: Colors.white,
              onPressed: _fetchRecords,
            ),
          ),
        );
      }
    } catch (e) {
      if (mounted) setState(() { _error = e.toString(); _isLoading = false; });
    }
  }

  @override
  Widget build(BuildContext context) {
    final hasCustomLayoutForm = widget.layout != null &&
        widget.layout!.objects.isNotEmpty &&
        _viewMode == 'form';

    Widget content;
    if (_isLoading) {
      content = const Center(child: CircularProgressIndicator());
    } else if (widget.mode == OperationalMode.find) {
      content = _buildFindModeForm();
    } else if (hasCustomLayoutForm) {
      content = _buildLayoutCanvasView(widget.layout!);
    } else if (_viewMode == 'table' && _records.isNotEmpty) {
      content = _buildTableView();
    } else if (_viewMode == 'list' && _records.isNotEmpty) {
      content = _buildListView();
    } else if (_error != null) {
      content = Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.error_outline, size: 48, color: Colors.red),
            const SizedBox(height: 12),
            Text('Error: $_error', style: const TextStyle(color: Colors.red)),
            const SizedBox(height: 12),
            FilledButton.icon(
              icon: const Icon(Icons.refresh),
              label: const Text('Retry'),
              onPressed: _fetchRecords,
            ),
          ],
        ),
      );
    } else if (_records.isEmpty) {
      content = _buildEmptyState();
    } else {
      content = _buildBrowseModeContent();
    }

    return Column(
      children: [
        _buildRecordToolbar(),
        if (_error != null && hasCustomLayoutForm)
          MaterialBanner(
            content: Text('Could not load records: $_error', style: const TextStyle(fontSize: 12)),
            leading: const Icon(Icons.warning_amber, color: Colors.orange, size: 20),
            backgroundColor: Colors.amber.shade50,
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
            actions: [
              TextButton(
                onPressed: _fetchRecords,
                child: const Text('Retry', style: TextStyle(fontSize: 12)),
              ),
            ],
          ),
        const Divider(height: 1),
        Expanded(child: content),
      ],
    );
  }

  /// Moving between find requests, and adding or removing one. Several
  /// requests are OR-ed together; a request marked Omit subtracts.
  Widget _buildRequestNavigator() {
    final single = _findRequests.length == 1;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        IconButton(
          tooltip: 'Previous request',
          visualDensity: VisualDensity.compact,
          icon: const Icon(Icons.chevron_left, size: 18),
          onPressed: _findRequestIndex == 0 ? null : () => _loadFindRequest(_findRequestIndex - 1),
        ),
        Text(
          'Request $findRequestNumber of $findRequestCount',
          style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
        ),
        IconButton(
          tooltip: 'Next request',
          visualDensity: VisualDensity.compact,
          icon: const Icon(Icons.chevron_right, size: 18),
          onPressed: _findRequestIndex == _findRequests.length - 1
              ? null
              : () => _loadFindRequest(_findRequestIndex + 1),
        ),
        IconButton(
          tooltip: 'New request',
          visualDensity: VisualDensity.compact,
          icon: const Icon(Icons.add, size: 18),
          onPressed: newFindRequest,
        ),
        IconButton(
          tooltip: 'Delete this request',
          visualDensity: VisualDensity.compact,
          icon: const Icon(Icons.remove, size: 18),
          onPressed: single ? null : deleteFindRequest,
        ),
      ],
    );
  }

  Widget _buildRecordToolbar() {
    if (widget.mode == OperationalMode.browse) return _buildBrowseRecordBar();
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
      color: Theme.of(context).colorScheme.surfaceContainerHighest.withValues(alpha: 0.5),
      // The find controls are wider than a narrow window, so they scroll
      // sideways instead of overflowing.
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Row(
          children: [
            if (widget.mode == OperationalMode.find) ...[
              FilledButton.icon(
                icon: const Icon(Icons.search, size: 18),
                label: const Text('Perform Find (Enter)'),
                onPressed: _performFind,
              ),
              const SizedBox(width: 12),
              FilterChip(
                label: const Text('Omit (NOT)'),
                selected: _omit,
                onSelected: (val) => setState(() => _currentFindRequest.omit = val),
              ),
              const SizedBox(width: 8),
              _buildRequestNavigator(),
              const SizedBox(width: 8),
              _buildOperatorsMenu(),
              const SizedBox(width: 8),
              OutlinedButton.icon(
                icon: const Icon(Icons.clear_all, size: 16),
                label: const Text('Clear Criteria'),
                onPressed: () {
                  for (final c in _findControllers.values) {
                    c.clear();
                  }
                  _captureCurrentFindRequest();
                  setState(() {});
                },
              ),
              const SizedBox(width: 8),
              OutlinedButton.icon(
                icon: const Icon(Icons.close, size: 16),
                label: const Text('Cancel Find'),
                onPressed: () {
                  deleteAllFindRequests();
                  unawaited(_requestMode(OperationalMode.browse));
                  unawaited(_fetchRecords());
                },
              ),
            ],
          ],
        ),
      ),
    );
  }

  // ─── Browse mode record bar ────────────────────────────────────────────────

  static final bool _isApplePlatform =
      defaultTargetPlatform == TargetPlatform.macOS || defaultTargetPlatform == TargetPlatform.iOS;
  static String get _modKey => _isApplePlatform ? '⌘' : 'Ctrl+';

  /// Saving state of the current record, from the per-field save status.
  ({IconData icon, Color color, String label}) get _saveStatus {
    if (_fieldSaving.values.any((v) => v == false)) {
      return (icon: Icons.error_outline, color: Colors.red, label: 'Not saved');
    }
    if (_fieldSaving.values.any((v) => v == true)) {
      return (icon: Icons.sync, color: Colors.blue, label: 'Saving...');
    }
    // A new record exists only here until it is committed; saying "Saved"
    // would invite the user to close the window and lose it.
    if (_onDraft) {
      return (icon: Icons.fiber_new_outlined, color: Colors.orange.shade800, label: 'Not committed');
    }
    return (icon: Icons.check_circle_outline, color: Colors.green.shade700, label: 'Saved');
  }

  String get _foundSetSummary {
    final n = _records.length;
    final sort = _sortOrder.isEmpty
        ? 'Unsorted'
        : 'Sorted by ${_sortOrder.map((l) => '${_displayName(l.field)} ${l.ascending ? '↑' : '↓'}').join(', ')}';
    if (_isFoundSet) return '$n found of $_totalInTable · $sort';
    return '$n ${n == 1 ? 'record' : 'records'} · $sort';
  }

  Future<bool> _goToRecordIndex(int index) async {
    if (_records.isEmpty) return false;
    return goToRecord(index.clamp(0, _records.length - 1));
  }

  /// List / Table views: a button of a row acts on that row's record. The
  /// row is made the current record first, and nothing runs when that is
  /// refused (a commit trigger, a validation): the button would otherwise
  /// act on the record left.
  Future<void> _clickRowButton(int recordIndex, LayoutObjectModel obj, String label) async {
    final clicked = _records.elementAtOrNull(recordIndex)?['id']?.toString();
    if (recordIndex != _currentIndex && !await _goToRecordIndex(recordIndex)) return;
    // The record clicked, not whatever is at its place now (a commit
    // trigger may have sorted or reloaded the list).
    if (!mounted || recordIndex != _currentIndex || currentRecord?['id']?.toString() != clicked) return;
    await _handleLayoutButtonClick(obj, label);
  }

  /// List / Table views: a click on a field of another record makes it the
  /// current record and puts the cursor in that field, ready to type. The
  /// field is focused once the record has changed and its editable field is
  /// built, not before.
  Future<void> _editFieldOfRecord(int recordIndex, String fieldName) async {
    if (recordIndex != _currentIndex && !await _goToRecordIndex(recordIndex)) return;
    if (!mounted) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _fieldFocusNodes[fieldName]?.requestFocus();
    });
  }

  Widget _buildBrowseRecordBar() {
    final theme = Theme.of(context);
    final hasRecords = _records.isNotEmpty;
    final atFirst = !hasRecords || _currentIndex == 0;
    final atLast = !hasRecords || _currentIndex >= _records.length - 1;
    final current = hasRecords ? _currentIndex + 1 : 0;
    if (!_recordNumberFocus.hasFocus && _recordNumberCtrl.text != '$current') {
      _recordNumberCtrl.text = '$current';
    }
    final status = _saveStatus;

    Widget navButton(IconData icon, String tip, VoidCallback? onPressed) => IconButton(
          icon: Icon(icon, size: 20),
          tooltip: tip,
          visualDensity: VisualDensity.compact,
          onPressed: onPressed,
        );

    return Container(
      padding: const EdgeInsets.fromLTRB(12, 6, 12, 6),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        border: Border(bottom: BorderSide(color: theme.dividerColor.withValues(alpha: 0.4))),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          // Row 1: position in the found set
          Row(
            children: [
              navButton(Icons.first_page, 'First record', atFirst ? null : () => _goToRecordIndex(0)),
              navButton(Icons.chevron_left, 'Previous record ($_modKey↑)', atFirst ? null : previousRecord),
              const Text('Record ', style: TextStyle(fontSize: 13)),
              _RecordNumberField(
                controller: _recordNumberCtrl,
                focusNode: _recordNumberFocus,
                enabled: hasRecords,
                onSubmitted: (v) {
                  final n = int.tryParse(v.trim());
                  if (n != null) unawaited(_goToRecordIndex(n - 1));
                },
              ),
              Text(' of ${_records.length}', style: const TextStyle(fontSize: 13)),
              navButton(Icons.chevron_right, 'Next record ($_modKey↓)', atLast ? null : nextRecord),
              navButton(Icons.last_page, 'Last record', atLast ? null : () => _goToRecordIndex(_records.length - 1)),
              if (_records.length > 1)
                SizedBox(
                  width: 140,
                  // Dragging only previews the number; the record changes on release,
                  // after the current record is saved.
                  child: Slider(
                    value: (_sliderDragValue ?? _currentIndex.toDouble()).clamp(0, (_records.length - 1).toDouble()),
                    max: (_records.length - 1).toDouble(),
                    divisions: _records.length - 1,
                    label: '${(_sliderDragValue ?? _currentIndex.toDouble()).round() + 1}',
                    onChanged: (v) => setState(() => _sliderDragValue = v),
                    onChangeEnd: (v) {
                      setState(() => _sliderDragValue = null);
                      unawaited(_goToRecordIndex(v.round()));
                    },
                  ),
                ),
              const SizedBox(width: 8),
              Flexible(
                child: Text(
                  _foundSetSummary,
                  style: TextStyle(fontSize: 12, color: theme.colorScheme.onSurfaceVariant),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              if (_isFoundSet)
                Tooltip(
                  message: 'Show all records',
                  child: IconButton(
                    icon: const Icon(Icons.filter_alt_off_outlined, size: 18),
                    visualDensity: VisualDensity.compact,
                    onPressed: _fetchRecords,
                  ),
                ),
              const Spacer(),
              if (hasRecords)
                Tooltip(
                  message: 'Changes are saved automatically',
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(status.icon, size: 15, color: status.color),
                      const SizedBox(width: 4),
                      Text(status.label, style: TextStyle(fontSize: 12, color: status.color)),
                    ],
                  ),
                ),
            ],
          ),
          const SizedBox(height: 4),
          // Row 2: record actions and view
          Row(
            children: [
              FilledButton.icon(
                icon: const Icon(Icons.add, size: 16),
                label: const Text('New'),
                style: FilledButton.styleFrom(visualDensity: VisualDensity.compact),
                onPressed: _canCreate ? _createNewRecord : null,
              ),
              const SizedBox(width: 6),
              _barButton(Icons.copy, 'Duplicate', hasRecords && _canCreate ? actionDuplicateRecord : null),
              _barButton(Icons.delete_outline, 'Delete',
                  hasRecords && (_canDelete || _onDraft) ? () => actionDeleteRecord(confirm: true) : null,
                  color: Colors.red.shade700),
              const SizedBox(width: 10),
              _barButton(Icons.search, 'Find', () => unawaited(_requestMode(OperationalMode.find))),
              _barButton(Icons.sort, 'Sort', hasRecords ? _showSortDialog : null),
              _barButton(Icons.list_alt, 'Show All', _fetchRecords),
              const Spacer(),
              SegmentedButton<String>(
                segments: [
                  const ButtonSegment(value: 'form', icon: Icon(Icons.dashboard_outlined, size: 14), label: Text('Form', style: TextStyle(fontSize: 11))),
                  // A layout with summary parts lists its records as a report,
                  // so the segment says what it will show (#32).
                  ButtonSegment(
                    value: 'list',
                    icon: Icon(_layoutIsReport ? Icons.summarize_outlined : Icons.view_list_outlined, size: 14),
                    label: Text(_layoutIsReport ? 'Report' : 'List', style: const TextStyle(fontSize: 11)),
                  ),
                  const ButtonSegment(value: 'table', icon: Icon(Icons.table_chart_outlined, size: 14), label: Text('Table', style: TextStyle(fontSize: 11))),
                ],
                selected: {_viewMode},
                onSelectionChanged: (val) {
                  setState(() => _viewMode = val.first);
                  if (val.first == 'list') {
                    unawaited(() async {
                      if (await _saveCurrentRecord() && mounted) {
                        await _fetchRecords(preserveCurrent: true, loadAll: true);
                      }
                    }());
                  }
                  if (val.first == 'list' && _layoutIsReport && _reportFigures.isEmpty) {
                    unawaited(_loadReportFigures());
                  }
                },
                style: SegmentedButton.styleFrom(
                  visualDensity: VisualDensity.compact,
                  padding: const EdgeInsets.symmetric(horizontal: 6),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _barButton(IconData icon, String label, VoidCallback? onPressed, {Color? color}) {
    return Padding(
      padding: const EdgeInsets.only(right: 6),
      child: OutlinedButton.icon(
        icon: Icon(icon, size: 16, color: onPressed == null ? null : color),
        label: Text(label, style: TextStyle(color: onPressed == null ? null : color)),
        style: OutlinedButton.styleFrom(visualDensity: VisualDensity.compact),
        onPressed: onPressed,
      ),
    );
  }

  // ─── Sort ────────────────────────────────────────────────────────────────

  int _compareValues(dynamic a, dynamic b) {
    if (a == null || a.toString().isEmpty) return (b == null || b.toString().isEmpty) ? 0 : 1;
    if (b == null || b.toString().isEmpty) return -1;
    final na = num.tryParse(a.toString());
    final nb = num.tryParse(b.toString());
    if (na != null && nb != null) return na.compareTo(nb);
    return a.toString().toLowerCase().compareTo(b.toString().toLowerCase());
  }

  /// Sorts the found set by [field] (null restores the original order),
  /// keeping the current record selected.
  void _applySort(String? field, bool ascending) =>
      _applySortOrder(field == null ? const [] : [SortLevel(field, ascending)]);

  /// Orders the found set by every level in turn: ties on the first field are
  /// broken by the second, and so on.
  void _applySortOrder(List<SortLevel> order) {
    final currentId = _records.isNotEmpty ? _records[_currentIndex]['id'] : null;
    final sorted = List.of(_unsortedRecords);
    if (order.isNotEmpty) {
      sorted.sort((a, b) {
        for (final level in order) {
          final cmp = level.ascending
              ? _compareValues(a[level.field], b[level.field])
              : _compareValues(b[level.field], a[level.field]);
          if (cmp != 0) return cmp;
        }
        return 0;
      });
    }
    setState(() {
      _records = sorted;
      _sortOrder = List.unmodifiable(order);
      final idx = _records.indexWhere((r) => r['id'] == currentId);
      _currentIndex = idx < 0 ? 0 : idx;
    });
    _rebuildFieldControllers();
    _recordChanged();
  }

  Future<void> _showSortDialog() async {
    await _saveCurrentRecord();
    if (!mounted) return;
    final cols = widget.table.columns.where((c) => !c.isPrimaryKey).toList();
    if (cols.isEmpty) return;

    final order = List.of(_sortOrder);
    final result = await showDialog<String>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDlg) {
          final available = cols.where((c) => !order.any((l) => l.field == c.name)).toList();
          return AlertDialog(
            title: const Text('Sort Records'),
            content: SizedBox(
              width: 420,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const Text(
                    'Records are ordered by the first field; ties are broken by the next one.',
                    style: TextStyle(fontSize: 12, color: Colors.grey),
                  ),
                  const SizedBox(height: 12),
                  if (order.isEmpty)
                    const Padding(
                      padding: EdgeInsets.symmetric(vertical: 16),
                      child: Text('No sort order yet. Add a field below.',
                          style: TextStyle(fontSize: 12, color: Colors.grey)),
                    )
                  else
                    ConstrainedBox(
                      constraints: const BoxConstraints(maxHeight: 220),
                      child: ListView.builder(
                        shrinkWrap: true,
                        itemCount: order.length,
                        itemBuilder: (_, i) {
                          final level = order[i];
                          return ListTile(
                            key: ValueKey('sort-level-${level.field}'),
                            dense: true,
                            contentPadding: EdgeInsets.zero,
                            leading: Text('${i + 1}.'),
                            title: Text(_displayName(level.field)),
                            trailing: Row(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                IconButton(
                                  tooltip: level.ascending ? 'Ascending' : 'Descending',
                                  icon: Icon(level.ascending ? Icons.arrow_upward : Icons.arrow_downward, size: 16),
                                  onPressed: () => setDlg(() => order[i] = level.flipped),
                                ),
                                IconButton(
                                  tooltip: 'Move up',
                                  icon: const Icon(Icons.keyboard_arrow_up, size: 18),
                                  onPressed: i == 0
                                      ? null
                                      : () => setDlg(() => order.insert(i - 1, order.removeAt(i))),
                                ),
                                IconButton(
                                  tooltip: 'Move down',
                                  icon: const Icon(Icons.keyboard_arrow_down, size: 18),
                                  onPressed: i == order.length - 1
                                      ? null
                                      : () => setDlg(() => order.insert(i + 1, order.removeAt(i))),
                                ),
                                IconButton(
                                  tooltip: 'Remove from the sort order',
                                  icon: const Icon(Icons.close, size: 16),
                                  onPressed: () => setDlg(() => order.removeAt(i)),
                                ),
                              ],
                            ),
                          );
                        },
                      ),
                    ),
                  const Divider(),
                  Row(
                    children: [
                      Expanded(
                        child: DropdownButtonFormField<String>(
                          key: const ValueKey('sort-add-field'),
                          decoration: const InputDecoration(
                              labelText: 'Add a field', border: OutlineInputBorder(), isDense: true),
                          items: available
                              .map((c) => DropdownMenuItem(value: c.name, child: Text(c.displayName)))
                              .toList(),
                          onChanged: available.isEmpty
                              ? null
                              : (v) {
                                  if (v != null) setDlg(() => order.add(SortLevel(v, true)));
                                },
                        ),
                      ),
                      const SizedBox(width: 8),
                      TextButton(
                        onPressed: order.isEmpty ? null : () => setDlg(order.clear),
                        child: const Text('Clear All'),
                      ),
                    ],
                  ),
                ],
              ),
            ),
            actions: [
              TextButton(onPressed: () => Navigator.of(ctx).pop('unsort'), child: const Text('Unsort')),
              TextButton(onPressed: () => Navigator.of(ctx).pop(), child: const Text('Cancel')),
              FilledButton(
                onPressed: order.isEmpty ? null : () => Navigator.of(ctx).pop('sort'),
                child: const Text('Sort'),
              ),
            ],
          );
        },
      ),
    );
    if (result == 'sort') _applySortOrder(order);
    if (result == 'unsort') _applySortOrder(const []);
  }

  // ─── List and Table views ──────────────────────────────────────────────────

  List<ColumnModel> get _visibleColumns => widget.table.columns.where((c) => !c.isPrimaryKey).toList();

  void _openRecordInForm(int index) {
    unawaited(_goToRecordIndex(index));
    setState(() => _viewMode = 'form');
  }

  Widget _buildListView() {
    // A layout with summary parts is a report: its records are drawn through
    // its parts, grouped and totalled, rather than listed (#32).
    if (_layoutIsReport) return _buildReportView();

    if (widget.layout != null &&
        widget.layout!.objects.isNotEmpty &&
        widget.layout!.defaultView == 'list') {
      return _buildCustomLayoutListView(widget.layout!);
    }

    final cols = _visibleColumns;
    final theme = Theme.of(context);
    return ListView.separated(
      padding: const EdgeInsets.symmetric(vertical: 8),
      itemCount: _records.length,
      separatorBuilder: (_, _) => const Divider(height: 1),
      itemBuilder: (ctx, i) {
        final r = _records[i];
        final values = cols.map((c) => recordFieldText(c, r)).where((v) => v.isNotEmpty).toList();
        final title = values.isNotEmpty ? values.first : '(empty record)';
        final subtitle = values.skip(1).take(4).join(' · ');
        return ListTile(
          selected: i == _currentIndex,
          selectedTileColor: theme.colorScheme.primary.withValues(alpha: 0.08),
          leading: CircleAvatar(radius: 14, child: Text('${i + 1}', style: const TextStyle(fontSize: 11))),
          title: Text(title, overflow: TextOverflow.ellipsis),
          subtitle: subtitle.isEmpty ? null : Text(subtitle, overflow: TextOverflow.ellipsis),
          trailing: const Icon(Icons.chevron_right, size: 18),
          onTap: () => _openRecordInForm(i),
        );
      },
    );
  }

  /// The objects drawn inside each portal of [layout], by portal id, and the
  /// ids of all of them. They belong to the portal's rows, where each row
  /// redraws them against its own related record (#36), and are not drawn a
  /// second time on the layout itself.
  (Map<String, List<LayoutObjectModel>>, Set<String>) _portalContents(LayoutDefinitionModel layout) {
    final portals = layout.objects.where((o) => o.type == 'portal').toList();
    final contents = <String, List<LayoutObjectModel>>{
      for (final portal in portals)
        portal.id: layout.objects.where((o) => o.isRowObjectOf(portal)).toList(),
    };
    return (contents, {for (final objects in contents.values) for (final o in objects) o.id});
  }

  Widget _buildCustomLayoutListView(LayoutDefinitionModel layout) {
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final theme = Theme.of(context);
    // Every part above the body (a top navigation, a title header, a
    // header…) is drawn once above the rows, and every part below it once
    // under them; the body repeats, once per record.
    final bodyIndex = layout.parts.indexWhere((p) => p.isBody);
    var headerHeight = 0.0;
    var bodyHeight = 0.0;
    for (final (i, p) in layout.parts.indexed) {
      if (bodyIndex < 0 ? p.type == LayoutPartType.header : i < bodyIndex) headerHeight += p.height;
      if (i == bodyIndex) bodyHeight = p.height;
    }
    final bodyFill = bodyIndex < 0 ? null : parseLayoutColor(layout.parts[bodyIndex].fillColor);
    final (portalContents, insidePortal) = _portalContents(layout);
    final offPanel = objectsOffPanel(layout.objects, selected: _selectedPanels);
    final shown = layout.objects.where((o) => !insidePortal.contains(o.id) && !offPanel.contains(o.id));
    final headerObjects = shown.where((o) => o.y < headerHeight).toList();
    final bodyEnd = bodyHeight > 0 ? headerHeight + bodyHeight : double.infinity;
    final bodyObjects = shown.where((o) => o.y >= headerHeight && o.y < bodyEnd).toList();
    final footerObjects = shown.where((o) => o.y >= bodyEnd).toList();
    var footerHeight = 0.0;
    if (bodyIndex >= 0) {
      for (final p in layout.parts.skip(bodyIndex + 1)) {
        footerHeight += p.height;
      }
    }

    var maxBodyBottom = headerHeight + 48.0;
    for (final o in bodyObjects) {
      if (o.y + o.height > maxBodyBottom) {
        maxBodyBottom = o.y + o.height;
      }
    }
    final rowHeight = bodyHeight > 0 ? bodyHeight : math.max<double>(46.0, maxBodyBottom - headerHeight + 8.0);

    // A band drawn once: its parts' fills and its objects, from [top].
    Widget band(double top, double height, List<LayoutObjectModel> objects) => SizedBox(
          width: layout.width,
          height: height,
          child: Stack(
            children: [
              Positioned(
                top: -top,
                left: 0,
                right: 0,
                height: layout.height,
                child: Stack(children: layoutPartFills(layout)),
              ),
              for (final obj in objects)
                Positioned(
                  left: obj.x,
                  top: obj.y - top,
                  width: obj.width,
                  height: obj.height,
                  child: _buildLayoutObject(obj, _currentRecord, isDark, false,
                      portalContents: portalContents[obj.id] ?? const []),
                ),
            ],
          ),
        );

    return SingleChildScrollView(
      padding: const EdgeInsets.all(20),
      child: Center(
        child: Container(
          width: layout.width,
          decoration: BoxDecoration(
            color: isDark ? const Color(0xFF161B22) : Colors.white,
            borderRadius: BorderRadius.circular(8),
            border: Border.all(
              color: isDark ? const Color(0xFF38404B) : const Color(0xFFD1CFCA),
            ),
            boxShadow: [
              BoxShadow(
                color: Colors.black.withValues(alpha: isDark ? 0.35 : 0.08),
                blurRadius: 10,
                offset: const Offset(0, 3),
              ),
            ],
          ),
          clipBehavior: Clip.antiAlias,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (headerHeight > 0) band(0, headerHeight, headerObjects),
              for (final (i, rec) in _records.indexed)
                // No double tap on the whole row: it would delay every single
                // click (and so the start of editing) by the double-click
                // interval. The open button at the end shows it in Form view.
                InkWell(
                  onTap: () => _goToRecordIndex(i),
                  child: Container(
                    width: layout.width,
                    height: rowHeight,
                    decoration: BoxDecoration(
                      color: i == _currentIndex
                          ? theme.colorScheme.primary.withValues(alpha: 0.08)
                          : bodyFill ??
                              (i.isOdd
                                  ? (isDark ? const Color(0xFF1B2129) : const Color(0xFFF9FAFB))
                                  : Colors.transparent),
                      border: Border(
                        bottom: BorderSide(
                          color: isDark ? const Color(0xFF30363D) : const Color(0xFFE5E7EB),
                        ),
                      ),
                    ),
                    child: Stack(
                      children: [
                        // Each row hides what its record's conditions hide.
                        for (final obj in _shownOn(layout, rec, bodyObjects))
                          Positioned(
                            left: obj.x,
                            top: obj.y - headerHeight,
                            width: obj.width,
                            height: obj.height,
                            child: _buildListRowObject(obj, rec, i, isDark,
                                portalContents: portalContents[obj.id] ?? const []),
                          ),
                        Positioned(
                          right: 2,
                          top: 0,
                          bottom: 0,
                          child: Center(
                            child: IconButton(
                              icon: const Icon(Icons.open_in_full, size: 16),
                              tooltip: 'Open in Form view',
                              visualDensity: VisualDensity.compact,
                              onPressed: () => _openRecordInForm(i),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              if (footerHeight > 0 && bodyEnd.isFinite) band(bodyEnd, footerHeight, footerObjects),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildListRowObject(
    LayoutObjectModel obj,
    Map<String, dynamic> rec,
    int recordIndex,
    bool isDark, {
    List<LayoutObjectModel> portalContents = const [],
  }) {
    if (obj.type == 'field') {
      final external = widget.externalFieldNoticeOf(obj);
      if (external != null) {
        return relatedNotice(external, isDark: isDark, radius: obj.style.cornerRadius);
      }
      final unreadable = layoutFieldUnreadableNotice(obj.fieldBinding);
      if (unreadable != null) {
        return relatedNotice(unreadable, isDark: isDark, radius: obj.style.cornerRadius);
      }
      // A field of a related record, read with those of the other rows in
      // one request.
      if (obj.fieldBinding?.readsRelated ?? false) {
        final related = _relatedContext?.forRecord(rec['id']?.toString(), batched: true);
        if (related == null) {
          return relatedNotice('<${obj.fieldBinding!.qualifiedName}>', isDark: isDark, radius: obj.style.cornerRadius);
        }
        return RelatedFieldView(
          key: ValueKey('related-${obj.id}-${related.recordId}-$_relatedWrites'),
          object: obj,
          related: related,
          isDark: isDark,
          editable: widget.mode == OperationalMode.browse,
          formulaTrue: _formulaTrueOn(obj, rec),
          onWritten: _relatedFieldWritten,
        );
      }
      final fieldName = obj.fieldBinding?.fieldName ?? obj.text;
      final col = widget.table.columns
          .where((c) => c.name.toLowerCase() == fieldName.toLowerCase())
          .firstOrNull;
      if (col != null && col.storageType == 'CONTAINER') {
        // The repetition the box starts at, with its conditional formats'
        // fill behind the picture.
        final repetition = obj.fieldBinding?.repetitionNumbers(col.maxRepetitions).first ?? 1;
        final value = rec[ColumnModel.repetitionKey(col.name, repetition)];
        final bytes = _tryDecodeBase64Image(value?.toString());
        final look = obj.style.conditionalLook(value, formulaTrue: _formulaTrueOn(obj, rec, repetition: repetition));
        return InkWell(
          onTap: () async {
            if (obj.action != null) {
              await _clickRowButton(recordIndex, obj, col.displayName);
            } else {
              unawaited(_goToRecordIndex(recordIndex));
              _openRecordInForm(recordIndex);
            }
          },
          child: Container(
            decoration: BoxDecoration(
              color: parseLayoutColor(look?.fillColor) ?? (isDark ? const Color(0xFF21262D) : const Color(0xFFF3F4F6)),
              border: Border.all(
                color: isDark ? const Color(0xFF38404B) : const Color(0xFFD1D5DB),
              ),
              borderRadius: BorderRadius.circular(4),
            ),
            clipBehavior: Clip.antiAlias,
            child: bytes != null && bytes.isNotEmpty
                ? Image.memory(bytes, fit: BoxFit.cover, gaplessPlayback: true)
                : const Icon(Icons.image_outlined, size: 18, color: Colors.grey),
          ),
        );
      }
      // The record key of what the box shows: the repetition it starts at.
      final repetition = col == null ? 1 : obj.fieldBinding?.repetitionNumbers(col.maxRepetitions).first ?? 1;
      final key = col == null ? fieldName : ColumnModel.repetitionKey(col.name, repetition);
      final rawVal = rec[key];
      final effStyle = _styleOn(obj, rec, rawVal, repetition: repetition);
      // A field FileMaker would not let be entered in Browse mode is shown,
      // never typed into, in the list as in the form.
      final enterable = col != null &&
          !col.isPrimaryKey &&
          !_isEntryLocked(col.name) &&
          _entryAllowed &&
          (obj.fieldBinding?.allowBrowseEntry ?? true);
      if (col != null && enterable && recordIndex == _currentIndex) {
        final ctrl = _fieldControllers[key];
        final fn = _fieldFocusNodes[key];
        return TextField(
          controller: ctrl,
          focusNode: fn,
          textAlign: _parseTextAlign(effStyle.textAlign),
          style: layoutTextStyle(
            effStyle,
            fallbackColor: isDark ? Colors.white : Colors.black87,
          ),
          decoration: InputDecoration(
            isDense: true,
            contentPadding: EdgeInsets.only(
              left: 6 + effStyle.indentLeft,
              right: 6 + effStyle.indentRight,
              top: 3,
              bottom: 3,
            ),
            fillColor: effStyle.fillColor != null
                ? _parseColor(effStyle.fillColor!)
                : (isDark ? const Color(0xFF0D1117) : Colors.white),
            filled: true,
            border: OutlineInputBorder(
              borderRadius: BorderRadius.circular(effStyle.cornerRadius),
              borderSide: effStyle.borderWidth <= 0
                  ? BorderSide.none
                  : BorderSide(
                      color: effStyle.borderColor != null
                          ? _parseColor(effStyle.borderColor!)
                          : (isDark ? const Color(0xFF38404B) : const Color(0xFFD1D5DB)),
                      width: effStyle.borderWidth,
                    ),
            ),
          ),
          onChanged: (v) => _onFieldChanged(key, v),
          onEditingComplete: () {
            _fieldDebounceTimers[key]?.cancel();
            unawaited(_saveField(key, ctrl?.text ?? ''));
            fn?.nextFocus();
          },
        );
      }
      final rawText = col != null ? _keyText(col, key, rec) : (rawVal?.toString() ?? '');
      final hasCustomFormat = layoutStyleHasDisplayFormat(effStyle);
      // An original text is shown as it was written, not reformatted.
      final hasOriginal = col != null && sourceText(rec, key) != null;
      final valText = hasCustomFormat && !hasOriginal
          ? formatLayoutDisplayValue(rawVal ?? rawText, effStyle)
          : rawText;
      final fillColor = effStyle.fillColor != null
          ? _parseColor(effStyle.fillColor!)
          : (isDark ? const Color(0xFF0D1117) : Colors.white);
      return GestureDetector(
        onTap: !enterable ? null : () => _editFieldOfRecord(recordIndex, key),
        child: Container(
          alignment: _parseAlignment(effStyle.textAlign),
          padding: EdgeInsets.only(
            left: 8 + effStyle.indentLeft,
            right: 8 + effStyle.indentRight,
            top: 4,
            bottom: 4,
          ),
          decoration: BoxDecoration(
            color: fillColor,
            border: effStyle.borderWidth <= 0
                ? null
                : Border.all(
                    color: effStyle.borderColor != null
                        ? _parseColor(effStyle.borderColor!)
                        : (isDark ? const Color(0xFF38404B) : const Color(0xFFD1D5DB)),
                    width: effStyle.borderWidth,
                  ),
            borderRadius: BorderRadius.circular(effStyle.cornerRadius),
          ),
          child: Text(
            valText,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            textAlign: _parseTextAlign(effStyle.textAlign),
            style: layoutTextStyle(
              effStyle,
              fallbackColor: isDark ? Colors.white : Colors.black87,
            ),
          ),
        ),
      );
    }
    if (obj.type == 'button') {
      final btnText = _resolveTextForRecord(obj.text.isEmpty && obj.action == null ? 'Button' : obj.text, rec);
      // Its conditional formats, Self being its text.
      final look = obj.style.conditionalLook(btnText, formulaTrue: _formulaTrueOn(obj, rec));
      if (obj.style.fillColor == '#00000000' && obj.style.borderWidth <= 0) {
        // A button without a fill (a grouped button, an icon) is a place
        // to click over what is drawn under it, which a fill would hide.
        return InkWell(
          onTap: () => _clickRowButton(recordIndex, obj, btnText),
          child: btnText.isEmpty
              ? const SizedBox.expand()
              : Container(
                  color: parseLayoutColor(look?.fillColor),
                  alignment: Alignment.center,
                  child: Text(btnText,
                      overflow: TextOverflow.ellipsis, style: conditionalTextStyle(layoutTextStyle(obj.style), look)),
                ),
        );
      }
      return ElevatedButton(
        style: look == null
            ? null
            : ElevatedButton.styleFrom(
                backgroundColor: parseLayoutColor(look.fillColor),
                foregroundColor: parseLayoutColor(look.textColor),
              ),
        onPressed: () => _clickRowButton(recordIndex, obj, btnText),
        child: Text(btnText, overflow: TextOverflow.ellipsis, style: conditionalTextStyle(const TextStyle(), look)),
      );
    }
    // A portal of a row shows that row's related records, read with those
    // of the other rows in one request.
    return _buildLayoutObject(obj, rec, isDark, false, portalContents: portalContents, listRow: true);
  }

  /// The found set drawn through the layout's parts (#32).
  Widget _buildReportView() {
    final layout = widget.layout!;
    final isDark = Theme.of(context).brightness == Brightness.dark;

    if (_reportError != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.summarize_outlined, size: 40, color: Colors.orange),
              const SizedBox(height: 10),
              Text('The report could not be totalled: $_reportError',
                  textAlign: TextAlign.center, style: const TextStyle(fontSize: 12)),
              const SizedBox(height: 10),
              FilledButton.icon(
                icon: const Icon(Icons.refresh, size: 16),
                label: const Text('Try again'),
                onPressed: () => unawaited(_loadReportFigures()),
              ),
            ],
          ),
        ),
      );
    }

    if (_loadingReport && _reportFigures.isEmpty) {
      return const Center(child: CircularProgressIndicator());
    }

    return ReportView(
      key: const ValueKey('report-view'),
      layout: layout,
      table: widget.table,
      records: _records.where((r) => r[_draftKey] != true).toList(),
      figures: _reportFigures,
      specs: _summarySpecs,
      formatValue: fieldText,
      sortedBy: [for (final level in _sortOrder) level.field],
      onSortForReport: _sortForReport,
      isDark: isDark,
    );
  }

  Widget _buildTableView() {
    final cols = _visibleColumns;
    final theme = Theme.of(context);
    final headerStyle = TextStyle(fontSize: 12, fontWeight: FontWeight.bold, color: theme.colorScheme.onSurface);
    const colWidth = 180.0;

    Widget headerCell(ColumnModel c) {
      final sorted = _sortField == c.name;
      return InkWell(
        onTap: () => _applySort(c.name, !sorted || !_sortAscending),
        child: Container(
          width: colWidth,
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 10),
          child: Row(
            children: [
              Expanded(child: Text(c.displayName, style: headerStyle, overflow: TextOverflow.ellipsis)),
              if (sorted) Icon(_sortAscending ? Icons.arrow_upward : Icons.arrow_downward, size: 14),
            ],
          ),
        ),
      );
    }

    return Scrollbar(
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: SizedBox(
          width: 56 + cols.length * colWidth,
          child: Column(
            children: [
              ColoredBox(
                color: theme.colorScheme.surfaceContainerHighest,
                child: Row(children: [
                  const SizedBox(width: 56, child: Center(child: Text('#', style: TextStyle(fontSize: 12)))),
                  ...cols.map(headerCell),
                ]),
              ),
              Expanded(
                child: ListView.builder(
                  itemCount: _records.length,
                  itemExtent: 36,
                  itemBuilder: (ctx, i) {
                    final r = _records[i];
                    final selected = i == _currentIndex;
                    return Material(
                      color: selected
                          ? theme.colorScheme.primary.withValues(alpha: 0.10)
                          : (i.isOdd ? theme.colorScheme.surfaceContainerLowest : Colors.transparent),
                      // No double tap on the whole row: it would delay every
                      // single click by the double-click interval. Opening the
                      // record in Form view is a double click on its number.
                      child: InkWell(
                        onTap: () => _goToRecordIndex(i),
                        child: Row(children: [
                          Tooltip(
                            message: 'Double click to open in Form view',
                            waitDuration: const Duration(milliseconds: 600),
                            child: GestureDetector(
                              behavior: HitTestBehavior.opaque,
                              onTap: () => _goToRecordIndex(i),
                              onDoubleTap: () => _openRecordInForm(i),
                              child: SizedBox(
                                width: 56,
                                height: double.infinity,
                                child: Center(
                                  child: Text('${i + 1}',
                                      style: TextStyle(fontSize: 11, color: theme.colorScheme.onSurfaceVariant)),
                                ),
                              ),
                            ),
                          ),
                          ...cols.map((c) {
                            final computed = _isEntryLocked(c.name) || !_entryAllowed;
                            if (selected && !computed) {
                              final ctrl = _fieldControllers[c.name];
                              final fn = _fieldFocusNodes[c.name];
                              return Container(
                                width: colWidth,
                                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 3),
                                alignment: Alignment.centerLeft,
                                child: TextField(
                                  controller: ctrl,
                                  focusNode: fn,
                                  style: const TextStyle(fontSize: 12),
                                  decoration: const InputDecoration(
                                    isDense: true,
                                    contentPadding: EdgeInsets.symmetric(horizontal: 6, vertical: 6),
                                    border: OutlineInputBorder(),
                                  ),
                                  onChanged: (v) => _onFieldChanged(c.name, v),
                                  onEditingComplete: () {
                                    _fieldDebounceTimers[c.name]?.cancel();
                                    unawaited(_saveField(c.name, ctrl?.text ?? ''));
                                    fn?.nextFocus();
                                  },
                                ),
                              );
                            }
                            return GestureDetector(
                              behavior: HitTestBehavior.opaque,
                              onTap: () => computed ? _goToRecordIndex(i) : _editFieldOfRecord(i, c.name),
                              child: Container(
                                width: colWidth,
                                padding: const EdgeInsets.symmetric(horizontal: 10),
                                alignment: Alignment.centerLeft,
                                child: Text(_recordColumnText(c.name, r),
                                    style: const TextStyle(fontSize: 12), overflow: TextOverflow.ellipsis),
                              ),
                            );
                          }),
                        ]),
                      ),
                    );
                  },
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildOperatorsMenu({String? targetField}) {
    return PopupMenuButton<String>(
      tooltip: 'Formulas & Operators Reference',
      icon: const Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.functions, size: 16),
          SizedBox(width: 4),
          Text('Operators', style: TextStyle(fontSize: 12)),
          Icon(Icons.arrow_drop_down, size: 16),
        ],
      ),
      onSelected: (op) {
        final fieldToUse = targetField ?? _lastFocusedFindField ?? (widget.table.columns.isNotEmpty ? widget.table.columns.first.name : null);
        if (fieldToUse != null) {
          final ctrl = _findControllers[fieldToUse];
          if (ctrl != null) {
            final cur = ctrl.text;
            ctrl.text = '$cur$op';
            ctrl.selection = TextSelection.fromPosition(TextPosition(offset: ctrl.text.length));
          }
        }
      },
      itemBuilder: (ctx) => const [
        PopupMenuItem(value: '*', child: Text('*  Wildcard (zero or more characters)')),
        PopupMenuItem(value: '...', child: Text('...  Range (e.g. 10...50 or 2024-01-01...2024-12-31)')),
        PopupMenuItem(value: '=', child: Text('=  Exact word match (or = alone for empty field)')),
        PopupMenuItem(value: '==', child: Text('==  Strict entire field match')),
        PopupMenuItem(value: '!', child: Text('!  Not equal / omit')),
        PopupMenuItem(value: '>', child: Text('>  Greater than')),
        PopupMenuItem(value: '<', child: Text('<  Less than')),
        PopupMenuItem(value: '>=', child: Text('>=  Greater than or equal')),
        PopupMenuItem(value: '<=', child: Text('<=  Less than or equal')),
        PopupMenuItem(value: '//', child: Text("//  Today's date formula")),
        PopupMenuItem(value: '@', child: Text('@  Single character wildcard')),
      ],
    );
  }

  Widget _buildEmptyState() {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(Icons.inbox, size: 48, color: Colors.grey),
          const SizedBox(height: 12),
          Text('No records in table "${widget.table.displayName}"'),
          const SizedBox(height: 12),
          FilledButton.icon(
            icon: const Icon(Icons.add),
            label: const Text('Create First Record'),
            onPressed: _canCreate ? _createNewRecord : null,
          ),
        ],
      ),
    );
  }


  Widget _buildBrowseModeContent() {
    if (_viewMode == 'form' && widget.layout != null && widget.layout!.objects.isNotEmpty) {
      return _buildLayoutCanvasView(widget.layout!);
    }
    return _buildStandardCardContent();
  }

  Widget _buildLayoutCanvasView(LayoutDefinitionModel layout, {bool isFindMode = false}) {
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final record = _records.isNotEmpty && !isFindMode
        ? _records[_currentIndex]
        : <String, dynamic>{};

    // The canvas has the layout's size: it ends at the bottom of the footer
    // (or of an object placed below it). Layouts without parts fit their objects.
    var maxObjY = 0.0;
    for (final obj in layout.objects) {
      final bottom = obj.y + obj.height;
      if (bottom > maxObjY) maxObjY = bottom;
    }
    final canvasHeight = layout.height > 0
        ? math.max<double>(layout.height, maxObjY)
        : math.max<double>(maxObjY + 60.0, 520.0);
    final canvasWidth = layout.width;
    final hasCustomBackground = layout.backgroundColor != null || layout.backgroundImage != null;
    final tabRank = {
      for (final (i, o) in sortLayoutTabStops(layout.objects.where((o) => o.isTabStop)).indexed) o.id: i + 1,
    };

    final (portalContents, insidePortal) = _portalContents(layout);
    final hidden = isFindMode ? const <String>{} : _hiddenOn(layout, record);
    final off = objectsOffPanel(layout.objects, selected: _selectedPanels, hidden: hidden);
    final topLevel = layout.objects
        .where((o) => !insidePortal.contains(o.id) && !hidden.contains(o.id) && !off.contains(o.id))
        .toList();

    return LayoutEntryTransition(
      key: ValueKey('layout-entry-${layout.id}'),
      effect: layout.transition,
      child: SingleChildScrollView(
      padding: const EdgeInsets.all(20),
      child: Center(
        child: SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (widget.showTextRuler)
                SizedBox(
                  width: canvasWidth,
                  child: File4BaseTextRuler(
                    style: activeStyle,
                    width: activeFieldObject?.width ?? canvasWidth,
                    onStyleChanged: (newStyle) {
                      final activeObj = activeFieldObject;
                      if (activeObj != null) {
                        widget.onObjectStyleChanged?.call(activeObj.id, newStyle);
                      }
                    },
                  ),
                ),
              if (!isFindMode &&
                  !layout.objects.any((o) =>
                      o.type == 'field' &&
                      widget.externalFieldNoticeOf(o) == null &&
                      (o.fieldBinding?.allowBrowseEntry ?? true) &&
                      !_isEntryLocked(o.fieldBinding?.fieldName ?? o.text)))
                Container(
                  width: math.max<double>(canvasWidth, 560.0),
                  margin: const EdgeInsets.only(bottom: 8),
                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                  decoration: BoxDecoration(
                    color: Colors.blue.withValues(alpha: 0.10),
                    borderRadius: BorderRadius.circular(6),
                    border: Border.all(color: Colors.blue.shade400, width: 0.8),
                  ),
                  child: Row(
                    children: [
                      const Icon(Icons.info_outline, size: 16, color: Color(0xFF1E88E5)),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(
                          '"${layout.name}" is a read-only / print / chart layout. Switch to a Form layout or Table View to enter and edit data.',
                          style: const TextStyle(fontSize: 12),
                        ),
                      ),
                      const SizedBox(width: 8),
                      TextButton.icon(
                        style: TextButton.styleFrom(
                          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                          minimumSize: const Size(0, 26),
                        ),
                        icon: const Icon(Icons.table_chart_outlined, size: 14),
                        label: const Text('Table View', style: TextStyle(fontSize: 11)),
                        onPressed: () => setState(() => _viewMode = 'table'),
                      ),
                      if (widget.onGoToLayout != null)
                        FilledButton.tonalIcon(
                          style: FilledButton.styleFrom(
                            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                            minimumSize: const Size(0, 26),
                          ),
                          icon: const Icon(Icons.edit_note, size: 14),
                          label: Text('${widget.table.displayName} Form',
                              style: const TextStyle(fontSize: 11)),
                          onPressed: () {
                            final ok = widget.onGoToLayout!.call('${widget.table.displayName} Form');
                            if (!ok) setState(() => _viewMode = 'table');
                          },
                        ),
                    ],
                  ),
                ),
              // Header banner above canvas (Find mode instructions; the record
              // bar above shows the record position in Browse mode)
              if (isFindMode)
              Container(
                width: canvasWidth,
                margin: const EdgeInsets.only(bottom: 8),
                padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
                decoration: BoxDecoration(
                  color: isDark ? const Color(0xFF1E232B) : Colors.white,
                  borderRadius: BorderRadius.circular(6),
                  border: Border.all(
                    color: isDark ? const Color(0xFF38404B) : const Color(0xFFD1CFCA),
                  ),
                ),
                child: Row(
                  children: [
                    Icon(
                      isFindMode ? Icons.manage_search : Icons.view_quilt,
                      size: 16,
                      color: const Color(0xFF1E88E5),
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        isFindMode
                            ? 'Find Mode on "${layout.name}" • Enter criteria in fields and press Perform Find'
                            : _records.isEmpty
                                ? '${widget.table.displayName} • ${layout.name} • (0 records in table)'
                                : '${widget.table.displayName} • ${layout.name} • Record ${_currentIndex + 1} of ${_records.length}',
                        style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                    const SizedBox(width: 8),
                    if (!isFindMode && _records.isEmpty)
                      FilledButton.icon(
                        style: FilledButton.styleFrom(
                          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                          minimumSize: const Size(0, 28),
                        ),
                        icon: const Icon(Icons.add, size: 14),
                        label: const Text('Create First Record', style: TextStyle(fontSize: 11)),
                        onPressed: _canCreate ? _createNewRecord : null,
                      )
                    else if (!isFindMode)
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                        decoration: BoxDecoration(
                          color: Colors.green.withValues(alpha: 0.12),
                          borderRadius: BorderRadius.circular(4),
                          border: Border.all(color: Colors.green.shade600, width: 0.8),
                        ),
                        child: const Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Icon(Icons.sync, size: 12, color: Colors.green),
                            SizedBox(width: 4),
                            Text('Auto-saved to DB',
                                style: TextStyle(
                                    fontSize: 10,
                                    fontWeight: FontWeight.bold,
                                    color: Colors.green)),
                          ],
                        ),
                      )
                    else
                      FilledButton.tonalIcon(
                        style: FilledButton.styleFrom(
                          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                          minimumSize: const Size(0, 28),
                        ),
                        icon: const Icon(Icons.search, size: 14),
                        label: const Text('Perform Find', style: TextStyle(fontSize: 11)),
                        onPressed: _performFind,
                      ),
                  ],
                ),
              ),

              // Layout Canvas Surface
              Container(
                width: canvasWidth,
                height: canvasHeight,
                clipBehavior: hasCustomBackground ? Clip.antiAlias : Clip.none,
                decoration: BoxDecoration(
                  color: isDark ? const Color(0xFF161B22) : Colors.white,
                  borderRadius: BorderRadius.circular(8),
                  border: Border.all(
                    color: isDark ? const Color(0xFF38404B) : const Color(0xFFD1CFCA),
                  ),
                  boxShadow: [
                    BoxShadow(
                      color: Colors.black.withValues(alpha: isDark ? 0.35 : 0.08),
                      blurRadius: 10,
                      offset: const Offset(0, 3),
                    ),
                  ],
                ),
                // The Tab key follows the layout's tab order (Set Tab Order in Layout mode).
                child: FocusTraversalGroup(
                  policy: OrderedTraversalPolicy(),
                  child: Stack(
                    clipBehavior: Clip.none,
                    children: [
                      if (hasCustomBackground) Positioned.fill(child: LayoutBackgroundView(layout: layout)),
                      // Each part's own fill (drawn with the background when the
                      // layout has one), then subtle part boundaries
                      if (!hasCustomBackground) ...layoutPartFills(layout),
                      ..._buildPartDividers(layout, canvasWidth, isDark),
                      if (widget.showPageMargins)
                        PageMarginsOverlay(
                          canvasWidth: canvasWidth,
                          canvasHeight: canvasHeight,
                          pageSetup: widget.pageSetup,
                        ),

                      // Render layout objects
                      ...topLevel.map((obj) {
                        var child = _buildLayoutObject(obj, record, isDark, isFindMode,
                            portalContents: portalContents[obj.id] ?? const []);
                        final rank = tabRank[obj.id];
                        if (rank != null) {
                          child = FocusTraversalOrder(order: NumericFocusOrder(rank.toDouble()), child: child);
                        }
                        return Positioned(
                          left: obj.x,
                          top: obj.y,
                          width: obj.width,
                          height: obj.height,
                          child: child,
                        );
                      }),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
      ),
    );
  }

  Widget _buildLayoutObject(
      LayoutObjectModel obj, Map<String, dynamic> record, bool isDark, bool isFindMode,
      {List<LayoutObjectModel> portalContents = const [], bool listRow = false}) {
    switch (obj.type) {
      case 'label':
        final resolvedText = applyLayoutTextCase(
          isFindMode ? obj.text : _resolveTextForRecord(obj.text, record),
          obj.style.textCase,
        );
        final isMultilineLabel = obj.height > 36 || resolvedText.contains('\n');
        // Its conditional formats, Self being its text.
        final labelStyle = isFindMode ? obj.style : _styleOn(obj, record, resolvedText);
        final textWidget = Text(
          resolvedText,
          textAlign: _parseTextAlign(obj.style.textAlign),
          style: layoutTextStyle(
            labelStyle,
            fallbackColor: isDark ? Colors.white : Colors.black87,
          ),
        );
        return Container(
          alignment: isMultilineLabel ? Alignment.topLeft : _parseAlignment(obj.style.textAlign),
          decoration: layoutBoxDecoration(labelStyle),
          padding: EdgeInsets.only(
            left: 4 + obj.style.indentLeft,
            right: 4 + obj.style.indentRight,
            top: 2,
            bottom: 2,
          ),
          child: isMultilineLabel
              ? SingleChildScrollView(child: textWidget)
              : textWidget,
        );

      case 'field':
        // A field of another solution's table (#107) cannot be read yet: it
        // is shown as unavailable, in every mode, and takes no entry.
        final external = widget.externalFieldNoticeOf(obj);
        if (external != null) {
          return relatedNotice(external, isDark: isDark, radius: obj.style.cornerRadius);
        }
        final unreadable = layoutFieldUnreadableNotice(obj.fieldBinding);
        if (unreadable != null) {
          return relatedNotice(unreadable, isDark: isDark, radius: obj.style.cornerRadius);
        }
        // A field of a related record: `Companies::company_address` on a
        // Customers layout (#36). It is read through the relationship, not
        // from the record in hand, so none of what follows applies.
        if (obj.fieldBinding?.readsRelated ?? false) {
          if (isFindMode && obj.fieldBinding!.isAlongPath) {
            // A find follows one relationship; a field several away is not
            // a criterion yet.
            return relatedNotice('${obj.fieldBinding!.qualifiedName} takes no criteria',
                isDark: isDark, radius: obj.style.cornerRadius);
          }
          if (isFindMode) {
            // A criterion here finds the records whose *related* records
            // match it (#46), so the box is a real criterion box.
            final binding = obj.fieldBinding!;
            if (!binding.allowFindEntry) {
              return relatedNotice('${binding.qualifiedName} takes no criteria',
                  isDark: isDark, radius: obj.style.cornerRadius);
            }
            final key = relatedCriterionKey(
              binding.relationshipId ?? '',
              binding.tableOccurrence ?? '',
              binding.fieldName,
              side: binding.relationshipSide,
            );
            final controller = _findControllers.putIfAbsent(key, () {
              return TextEditingController(text: _currentFindRequest.values[key] ?? '');
            });
            return TextField(
              controller: controller,
              textAlign: _parseTextAlign(obj.style.textAlign),
              style: TextStyle(fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 13),
              decoration: InputDecoration(
                isDense: true,
                hintText: 'Search ${binding.qualifiedName}...',
                hintStyle: TextStyle(fontSize: 11, color: Colors.grey.shade400),
                contentPadding: const EdgeInsets.symmetric(horizontal: 8, vertical: 8),
                border: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(obj.style.cornerRadius),
                ),
                suffixIcon: const Icon(Icons.link, size: 14, color: Colors.grey),
              ),
              onTap: () => setState(() => _lastFocusedFindField = key),
              onSubmitted: (_) => _performFind(),
            );
          }
          final related = _relatedContext;
          if (related == null) {
            return relatedNotice('<${obj.fieldBinding!.qualifiedName}>',
                isDark: isDark, radius: obj.style.cornerRadius);
          }
          return RelatedFieldView(
            key: ValueKey('related-${obj.id}-${related.recordId}-$_relatedWrites'),
            object: obj,
            related: related,
            isDark: isDark,
            editable: widget.mode == OperationalMode.browse,
            formulaTrue: _formulaTrueOn(obj, record),
            onWritten: _relatedFieldWritten,
          );
        }

        final fieldName = obj.fieldBinding?.fieldName ?? obj.text;
        final col = widget.table.columns
            .where((c) => c.name.toLowerCase() == fieldName.toLowerCase())
            .firstOrNull;

        if (col == null) {
          return Container(
            alignment: Alignment.centerLeft,
            padding: const EdgeInsets.symmetric(horizontal: 8),
            decoration: BoxDecoration(
              color: Colors.amber.withValues(alpha: 0.1),
              border: Border.all(color: Colors.amber),
              borderRadius: BorderRadius.circular(obj.style.cornerRadius),
            ),
            child: Text(
              '<$fieldName>',
              style: const TextStyle(fontSize: 11, color: Colors.amber),
            ),
          );
        }

        if (isFindMode) {
          final findCtrl = _findControllers[col.name];
          // A field the layout does not let be entered in Find mode takes
          // no criterion.
          final findable = obj.fieldBinding?.allowFindEntry ?? true;
          final findNode = listRow ? null : _findFocusNodes.putIfAbsent(obj.id, FocusNode.new);
          final findField = TextField(
            controller: findCtrl,
            focusNode: findNode,
            readOnly: !findable,
            enabled: findable,
            onChanged: findNode == null
                ? null
                : (_) => _triggers.post(_trigger('OnObjectModify', objectId: obj.id)),
            textAlign: _parseTextAlign(obj.style.textAlign),
            style: layoutTextStyle(
              obj.style,
              fallbackColor: isDark ? Colors.white : Colors.black87,
            ),
            decoration: InputDecoration(
              isDense: true,
              hintText: findable ? 'Search ${col.displayName}...' : null,
              hintStyle: TextStyle(fontSize: 11, color: Colors.grey.shade400),
              contentPadding: EdgeInsets.only(
                left: 8 + obj.style.indentLeft,
                right: 8 + obj.style.indentRight,
                top: 8,
                bottom: 8,
              ),
              border: OutlineInputBorder(
                borderRadius: BorderRadius.circular(obj.style.cornerRadius),
              ),
              suffixIcon: const Icon(Icons.search, size: 14, color: Colors.grey),
            ),
            onTap: () {
              setState(() => _lastFocusedFindField = col.name);
              widget.onFocusedFieldChanged?.call();
            },
            onSubmitted: (_) => _performFind(),
          );
          if (findNode == null) return findField;
          return _withTriggers(() => _findBox(obj, col.name, findNode), findField);
        }

        if (_records.isEmpty) {
          return InkWell(
            onTap: () async {
              await _createNewRecord();
              if (!mounted) return;
              WidgetsBinding.instance.addPostFrameCallback((_) {
                _fieldFocusNodes[col.name]?.requestFocus();
              });
            },
            borderRadius: BorderRadius.circular(obj.style.cornerRadius),
            child: IgnorePointer(
              child: TextField(
                textAlign: _parseTextAlign(obj.style.textAlign),
                style: TextStyle(
                  fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 13,
                ),
                decoration: InputDecoration(
                  isDense: true,
                  hintText: _canCreate ? 'Click to create record & enter data...' : null,
                  hintStyle: TextStyle(fontSize: 10.5, fontStyle: FontStyle.italic, color: Colors.grey.shade400),
                  contentPadding: const EdgeInsets.symmetric(horizontal: 8, vertical: 8),
                  fillColor: obj.style.fillColor != null ? _parseColor(obj.style.fillColor!) : null,
                  filled: obj.style.fillColor != null,
                  border: OutlineInputBorder(
                    borderRadius: BorderRadius.circular(obj.style.cornerRadius),
                  ),
                ),
              ),
            ),
          );
        }

        if (col.isPrimaryKey) {
          final pkVal = record[col.name]?.toString() ?? '';
          // Shown its own way, with the conditional formats that apply.
          final look = obj.style.conditionalLook(record[col.name],
              formulaTrue: _formulaTrueOn(obj, record,
                  repetition: obj.fieldBinding?.repetitionNumbers(col.maxRepetitions).first ?? 1));
          return Container(
            alignment: Alignment.centerLeft,
            padding: const EdgeInsets.symmetric(horizontal: 8),
            decoration: BoxDecoration(
              color: parseLayoutColor(look?.fillColor) ?? (isDark ? const Color(0xFF272D37) : Colors.grey.shade100),
              border: Border.all(
                  color: isDark ? const Color(0xFF38404B) : Colors.grey.shade300),
              borderRadius: BorderRadius.circular(obj.style.cornerRadius),
            ),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    pkVal,
                    style: conditionalTextStyle(
                      TextStyle(
                        fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 12,
                        fontFamily: 'monospace',
                      ),
                      look,
                    ),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                const Icon(Icons.key, size: 14, color: Colors.grey),
              ],
            ),
          );
        }

        // A repeating field shows its repetitions in the object's box,
        // which they share equally: stacked, or side by side.
        final numbers = obj.fieldBinding?.repetitionNumbers(col.maxRepetitions) ?? const [1];
        final shown = numbers.length;
        if (shown > 1) {
          final horizontal = obj.fieldBinding?.repetitionOrientation == RepetitionOrientation.horizontal;
          final cell = horizontal
              ? obj.copyWith(width: obj.width / shown)
              : obj.copyWith(height: obj.height / shown);
          return Flex(
            direction: horizontal ? Axis.horizontal : Axis.vertical,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (final n in numbers)
                Expanded(
                  child: Padding(
                    padding: EdgeInsets.only(
                      top: !horizontal && n > numbers.first ? 1 : 0,
                      left: horizontal && n > numbers.first ? 1 : 0,
                    ),
                    child: _buildFieldCell(cell, col, record, isDark, ColumnModel.repetitionKey(col.name, n),
                        trigger: listRow ? null : obj, repetition: n),
                  ),
                ),
            ],
          );
        }
        // A box of one repetition past the first edits that repetition.
        return _buildFieldCell(obj, col, record, isDark, ColumnModel.repetitionKey(col.name, numbers.first),
            trigger: listRow ? null : obj, repetition: numbers.first);

      case 'button':
      case 'popover_button':
        // A button with an action and no label (a grouped button, an icon)
        // shows nothing of its own.
        final btnText = _resolveTextForRecord(obj.text.isEmpty && obj.action == null ? 'Button' : obj.text, record);
        // Its conditional formats, Self being its text.
        final btnStyle = isFindMode ? obj.style : _styleOn(obj, record, btnText);
        final btnBorder = parseLayoutColor(btnStyle.borderColor);
        if (obj.style.fillColor == '#00000000' && obj.style.borderWidth <= 0) {
          // A place to click over what is drawn under it (an icon), which
          // a fill would hide: only a button with a label takes one.
          return InkWell(
            onTap: isFindMode && obj.action == null ? null : () => _handleLayoutButtonClick(obj, btnText),
            child: Container(
              color: obj.text.isEmpty ? null : parseLayoutColor(btnStyle.fillColor),
              alignment: _parseAlignment(obj.style.textAlign),
              padding: const EdgeInsets.symmetric(horizontal: 2),
              child: Text(
                btnText,
                style: TextStyle(
                  fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 12,
                  color: parseLayoutColor(btnStyle.textColor) ?? const Color(0xFF0066CC),
                  fontWeight: btnStyle.fontWeight == 'bold' ? FontWeight.bold : null,
                  fontStyle: btnStyle.fontStyle == 'italic' ? FontStyle.italic : null,
                  decoration: btnText.startsWith('http') || btnStyle.underline
                      ? TextDecoration.underline
                      : TextDecoration.none,
                ),
                overflow: TextOverflow.ellipsis,
              ),
            ),
          );
        }
        return ElevatedButton(
          style: ElevatedButton.styleFrom(
            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
            backgroundColor: parseLayoutColor(btnStyle.fillColor),
            foregroundColor: parseLayoutColor(btnStyle.textColor),
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(obj.style.cornerRadius),
              side: btnBorder == null || btnStyle.borderWidth <= 0
                  ? BorderSide.none
                  : BorderSide(color: btnBorder, width: btnStyle.borderWidth),
            ),
          ),
          onPressed: isFindMode && obj.action == null ? null : () => _handleLayoutButtonClick(obj, btnText),
          child: Text(
            btnText,
            style: TextStyle(
              fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 12,
              fontWeight: btnStyle.fontWeight == 'bold'
                  ? FontWeight.bold
                  : FontWeight.w600,
              fontStyle: btnStyle.fontStyle == 'italic' ? FontStyle.italic : null,
              decoration: btnStyle.underline ? TextDecoration.underline : null,
            ),
            overflow: TextOverflow.ellipsis,
          ),
        );

      case 'web_viewer':
        return _buildWebViewerObject(obj, record, isDark, isFindMode);

      case 'chart':
        // A chart draws the figures worked out over the found set (#37).
        if (isFindMode) {
          return relatedNotice('A chart shows nothing in Find mode', isDark: isDark,
              radius: obj.style.cornerRadius);
        }
        return Container(
          key: ValueKey('chart-${obj.id}'),
          decoration: BoxDecoration(
            color: obj.style.fillColor != null
                ? _parseColor(obj.style.fillColor!)
                : (isDark ? const Color(0xFF1B2029) : Colors.white),
            border: Border.all(
              color: obj.style.borderColor != null
                  ? _parseColor(obj.style.borderColor!)
                  : (isDark ? const Color(0xFF38404B) : const Color(0xFFD7DCE3)),
              width: obj.style.borderWidth,
            ),
            borderRadius: BorderRadius.circular(obj.style.cornerRadius),
          ),
          clipBehavior: Clip.antiAlias,
          child: _buildChart(obj, isDark),
        );

      case 'tab_control':
      case 'slide_control':
        return LayoutPanelControlView(
          obj: obj,
          selected: _selectedPanels[obj.id] ?? obj.defaultPanel,
          onSelect: (i) => setState(() => _selectedPanels[obj.id] = i),
        );

      case 'portal':
        // A portal shows the related records of the record in hand, one row
        // each, with the objects drawn inside it repeated per row (#36).
        if (isFindMode) {
          return relatedNotice('A portal shows nothing in Find mode', isDark: isDark,
              radius: obj.style.cornerRadius);
        }
        // In a list, each row's portal shows the related records of that
        // row's record, not of the record in hand.
        final related =
            listRow ? _relatedContext?.forRecord(record['id']?.toString(), batched: true) : _relatedContext;
        if (related == null) {
          return relatedNotice(
            obj.portal.isBound
                ? 'Portal: the relationship graph could not be read'
                : 'Portal: choose a relationship in Portal Setup',
            isDark: isDark,
            radius: obj.style.cornerRadius,
          );
        }
        return PortalView(
          key: ValueKey(listRow ? 'portal-${obj.id}-${related.recordId}' : 'portal-${obj.id}'),
          object: obj,
          contents: portalContents,
          related: related,
          isDark: isDark,
          refreshGeneration: _portalRefreshes[obj.id] ?? 0,
          onChanged: () => unawaited(_fetchRecords()),
          rowController: _portalRowController(obj, listRecordId: listRow ? related.recordId : null),
        );

      default:
        final drawn = buildDrawnLayoutObject(obj,
            record: isFindMode ? null : record, userName: widget.currentUserName, pageNumber: 1);
        if (drawn != null) return drawn;
        return Container(
          decoration: BoxDecoration(
            color: obj.style.fillColor != null
                ? _parseColor(obj.style.fillColor!)
                : Colors.transparent,
            border: Border.all(
              color: obj.style.borderColor != null
                  ? _parseColor(obj.style.borderColor!)
                  : Colors.grey.shade400,
              width: obj.style.borderWidth,
            ),
            borderRadius: BorderRadius.circular(obj.style.cornerRadius),
          ),
        );
    }
  }

  Uint8List? _tryDecodeBase64Image(String? raw) {
    if (raw == null) return null;
    var trimmed = raw.trim();
    if (trimmed.isEmpty) return null;
    final commaIdx = trimmed.indexOf('base64,');
    if (commaIdx >= 0) {
      trimmed = trimmed.substring(commaIdx + 7).trim();
    }
    try {
      return base64Decode(trimmed);
    } catch (_) {
      return null;
    }
  }

  /// One box of a field object: the field itself, or one repetition of a
  /// repeating field, under its record [key].
  ///
  /// [trigger] is the field object whose script triggers the box runs (#26),
  /// null in a list row.
  Widget _buildFieldCell(
    LayoutObjectModel obj,
    ColumnModel col,
    Map<String, dynamic> record,
    bool isDark,
    String key, {
    LayoutObjectModel? trigger,
    int repetition = 1,
  }) {
    if (col.storageType == 'CONTAINER') {
      return _buildContainerFieldObject(obj, col, record, isDark, key: key, repetition: repetition);
    }
    final cell = _buildFieldBox(obj, col, record, isDark, key, trigger: trigger, repetition: repetition);
    if (trigger == null) return cell;
    return _withTriggers(() => _fieldBox(trigger, col, key), cell);
  }

  Widget _buildFieldBox(
    LayoutObjectModel obj,
    ColumnModel col,
    Map<String, dynamic> record,
    bool isDark,
    String key, {
    LayoutObjectModel? trigger,
    int repetition = 1,
  }) {

    final ctrl = _fieldControllers[key];
    final fn = _fieldFocusNodes[key];
    final saving = _fieldSaving[key];
    // Shown like a calculation: read, not typed into. So is a field the
    // layout does not let be entered in Browse mode.
    final locked = _isEntryLocked(col.name) || !_entryAllowed;
    final computed = locked || !(obj.fieldBinding?.allowBrowseEntry ?? true);

    // A field bound to a value list is filled from a control rather than
    // typed into (#31).
    final offered = computed ? null : _itemsFor(obj.fieldBinding!);
    final formulaTrue = _formulaTrueOn(obj, record, repetition: repetition);
    if (offered != null) {
      return _buildValueListControl(
        look: obj.style.conditionalLook(record[key] ?? ctrl?.text, formulaTrue: formulaTrue),
        style: obj.fieldBinding!.effectiveControlStyle(hasValueList: true),
        items: offered,
        showSecondOnly: _valueLists[obj.fieldBinding!.valueListId]?.showSecondOnly ?? false,
        truncated: _truncatedValueLists.contains(obj.fieldBinding!.valueListId),
        current: ctrl?.text ?? '',
        onChanged: (value) {
          ctrl?.text = value;
          _onFieldChanged(key, value, object: trigger);
          if (trigger != null && _guarded(trigger)) unawaited(_validateAndSave(_fieldBox(trigger, col, key)));
        },
      );
    }

    // A list of another solution that cannot be read leaves an edit box,
    // which says why and offers to sign in to that solution.
    final listProblem = computed ? null : _valueListProblems[obj.fieldBinding!.valueListId];

    final rawVal = record[key] ?? ctrl?.text;
    final effStyle = obj.style.effectiveForValue(rawVal, formulaTrue: formulaTrue);
    final isFocused = fn?.hasFocus ?? false;
    final hasDisplayFormatting = layoutStyleHasDisplayFormat(effStyle);

    // A drop-down calendar picks its date from a calendar, as FileMaker's
    // does, and can still be typed into.
    final calendar = !computed &&
            col.fieldType == 'DATE' &&
            obj.fieldBinding?.controlStyle == FieldControlStyle.dropDownCalendar
        ? IconButton(
            key: ValueKey('calendar-${obj.id}'),
            icon: const Icon(Icons.calendar_today, size: 14),
            tooltip: 'Choose a date',
            visualDensity: VisualDensity.compact,
            onPressed: () => unawaited(_pickDate(key, ctrl)),
          )
        : null;

    final isMultilineField = obj.height > 44;
    final verticalPad = obj.height < 32
        ? math.max(1.0, (obj.height - 16.0) / 2.0)
        : 8.0;

    final textField = TextField(
      controller: ctrl,
      focusNode: fn,
      readOnly: computed,
      maxLines: isMultilineField ? null : 1,
      expands: isMultilineField,
      textAlign: _parseTextAlign(effStyle.textAlign),
      textAlignVertical: isMultilineField ? TextAlignVertical.top : TextAlignVertical.center,
      style: layoutTextStyle(
        effStyle,
        fallbackColor: isDark ? Colors.white : Colors.black87,
      ),
      decoration: InputDecoration(
        isDense: true,
        hintText: obj.fieldBinding?.placeholder,
        hintStyle: TextStyle(fontSize: effStyle.fontSize > 0 ? effStyle.fontSize : 13, color: Colors.grey.shade500),
        contentPadding: EdgeInsets.only(
          left: 8 + effStyle.indentLeft,
          right: 8 + effStyle.indentRight,
          top: verticalPad,
          bottom: verticalPad,
        ),
        fillColor: effStyle.fillColor != null
            ? _parseColor(effStyle.fillColor!)
            : null,
        filled: effStyle.fillColor != null,
        // A line of width 0 is no line: a zero-width side would still draw
        // a hairline.
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(effStyle.cornerRadius),
          borderSide: effStyle.borderWidth <= 0
              ? BorderSide.none
              : BorderSide(
                  color: effStyle.borderColor != null
                      ? _parseColor(effStyle.borderColor!)
                      : Colors.grey,
                  width: effStyle.borderWidth,
                ),
        ),
        suffixIcon: locked
            ? const Tooltip(
                message: 'Filled in by its formula',
                child: Icon(Icons.functions, size: 14, color: Colors.grey),
              )
            : saving == true
                ? const Padding(
                    padding: EdgeInsets.all(8),
                    child: SizedBox(
                      width: 12,
                      height: 12,
                      child: CircularProgressIndicator(strokeWidth: 1.5),
                    ),
                  )
                : saving == false
                    ? const Icon(Icons.error_outline, size: 14, color: Colors.red)
                    : listProblem != null
                        ? _valueListProblemIcon(listProblem)
                        : calendar,
      ),
      onChanged: computed ? null : (v) => _onFieldChanged(key, v, object: trigger),
      onEditingComplete: computed
          ? null
          : () {
              _fieldDebounceTimers[key]?.cancel();
              // A field its triggers validate and save is saved as it is
              // left (#26).
              if (!_keyGuarded(key)) unawaited(_saveField(key, ctrl?.text ?? ''));
              fn?.nextFocus();
            },
    );

    if (!hasDisplayFormatting) return textField;

    // A formatted field shows its formatted value until it is edited. The
    // text field stays at the same place in the tree in both states, so
    // the click that focuses it also opens its text input: one click is
    // enough to start typing. (Building a new text field on focus left it
    // focused but without input, so a second click was needed.)
    final showFormatted = !isFocused;
    final formatted = formatLayoutDisplayValue(
      rawVal ?? (ctrl?.text ?? ''),
      effStyle,
    );
    return Stack(
        fit: StackFit.expand,
        children: [
          Opacity(
            opacity: showFormatted ? 0 : 1,
            child: IgnorePointer(ignoring: showFormatted, child: textField),
          ),
          if (showFormatted)
          GestureDetector(
            behavior: HitTestBehavior.opaque,
            onTap: () {
              if (!computed) fn?.requestFocus();
            },
            child: Container(
              alignment: isMultilineField
                  ? Alignment.topLeft
                  : _parseAlignment(effStyle.textAlign),
              padding: EdgeInsets.only(
                left: 8 + effStyle.indentLeft,
                right: 8 + effStyle.indentRight,
                top: verticalPad,
                bottom: verticalPad,
              ),
              decoration: BoxDecoration(
                color: effStyle.fillColor != null
                    ? _parseColor(effStyle.fillColor!)
                    : (isDark ? const Color(0xFF0D1117) : Colors.white),
                borderRadius: BorderRadius.circular(effStyle.cornerRadius),
                border: effStyle.borderWidth <= 0
                    ? null
                    : Border.all(
                        color: effStyle.borderColor != null
                            ? _parseColor(effStyle.borderColor!)
                            : Colors.grey.shade400,
                        width: effStyle.borderWidth,
                      ),
              ),
              child: Text(
                formatted,
                maxLines: isMultilineField ? null : 1,
                overflow: TextOverflow.ellipsis,
                textAlign: _parseTextAlign(effStyle.textAlign),
                style: layoutTextStyle(
                  effStyle,
                  fallbackColor: isDark ? Colors.white : Colors.black87,
                ),
              ),
            ),
          ),
        ],
      );
  }

  Widget _buildContainerFieldObject(
    LayoutObjectModel obj,
    ColumnModel col,
    Map<String, dynamic> record,
    bool isDark, {
    String? key,
    int repetition = 1,
  }) {
    final k = key ?? col.name;
    final rawVal = record[k]?.toString() ?? _fieldControllers[k]?.text ?? '';
    final bytes = _tryDecodeBase64Image(rawVal);
    final isViewLayout = widget.layout?.name.toLowerCase().contains('view') ?? false;
    // Its conditional formats give their fill behind the picture.
    final look = obj.style.conditionalLook(record[k], formulaTrue: _formulaTrueOn(obj, record, repetition: repetition));
    final child = Container(
      decoration: BoxDecoration(
        color: parseLayoutColor(look?.fillColor) ??
            (isViewLayout ? Colors.transparent : (isDark ? const Color(0xFF1E232B) : const Color(0xFFF8F9FA))),
        border: isViewLayout
            ? null
            : Border.all(
                color: isDark ? const Color(0xFF38404B) : const Color(0xFFD1D5DB),
                width: obj.style.borderWidth > 0 ? obj.style.borderWidth : 1,
              ),
        borderRadius: BorderRadius.circular(obj.style.cornerRadius),
      ),
      clipBehavior: Clip.antiAlias,
      alignment: isViewLayout ? Alignment.topLeft : Alignment.center,
      child: bytes != null && bytes.isNotEmpty
          ? Image.memory(
              bytes,
              fit: BoxFit.contain,
              alignment: isViewLayout ? Alignment.topLeft : Alignment.center,
              gaplessPlayback: true,
            )
          : Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(Icons.image_outlined, size: 28, color: Colors.grey.shade500),
                  if (obj.height > 60) ...[
                    const SizedBox(height: 4),
                    Text(
                      col.displayName,
                      style: TextStyle(fontSize: 11, color: Colors.grey.shade500),
                    ),
                  ],
                ],
              ),
            ),
    );
    if (obj.action != null) {
      return InkWell(
        onTap: () => _handleLayoutButtonClick(obj, col.displayName),
        child: child,
      );
    }
    return child;
  }

  Widget _buildWebViewerObject(
    LayoutObjectModel obj,
    Map<String, dynamic> record,
    bool isDark,
    bool isFindMode,
  ) {
    final resolvedUrl = isFindMode ? obj.text : _resolveTextForRecord(obj.text, record).trim();
    final photoBytes = _tryDecodeBase64Image(record['photo']?.toString());
    final recipeTitle = record['name']?.toString() ?? '';
    final recipeDesc = record['description']?.toString() ?? '';
    final recipeAuthor = record['author']?.toString() ?? '';

    return Container(
      key: ValueKey('web-viewer-${obj.id}'),
      decoration: BoxDecoration(
        color: isDark ? const Color(0xFF161B22) : Colors.white,
        border: Border.all(
          color: isDark ? const Color(0xFF38404B) : const Color(0xFFD1D5DB),
        ),
        borderRadius: BorderRadius.circular(obj.style.cornerRadius),
      ),
      clipBehavior: Clip.antiAlias,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            decoration: BoxDecoration(
              color: isDark ? const Color(0xFF21262D) : const Color(0xFFF3F4F6),
              border: Border(
                bottom: BorderSide(
                  color: isDark ? const Color(0xFF30363D) : const Color(0xFFE5E7EB),
                ),
              ),
            ),
            child: Row(
              children: [
                const Icon(Icons.public, size: 16, color: Color(0xFF0066CC)),
                const SizedBox(width: 8),
                Expanded(
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
                    decoration: BoxDecoration(
                      color: isDark ? const Color(0xFF0D1117) : Colors.white,
                      borderRadius: BorderRadius.circular(4),
                      border: Border.all(
                        color: isDark ? const Color(0xFF30363D) : const Color(0xFFD1D5DB),
                      ),
                    ),
                    child: Text(
                      resolvedUrl.isNotEmpty ? resolvedUrl : '(No URL specified for this record)',
                      style: TextStyle(
                        fontSize: 12,
                        color: resolvedUrl.isNotEmpty
                            ? const Color(0xFF0066CC)
                            : Colors.grey.shade500,
                      ),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                if (resolvedUrl.startsWith('http'))
                  FilledButton.tonalIcon(
                    onPressed: () => actionOpenUrl(resolvedUrl),
                    icon: const Icon(Icons.open_in_new, size: 15),
                    label: const Text('Open in Browser', style: TextStyle(fontSize: 12)),
                  ),
              ],
            ),
          ),
          Expanded(
            child: resolvedUrl.isEmpty
                ? Center(
                    child: Text(
                      'No URL is set on this record.',
                      style: TextStyle(fontSize: 13, color: Colors.grey.shade500),
                    ),
                  )
                : SingleChildScrollView(
                    padding: const EdgeInsets.all(24),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        if (recipeTitle.isNotEmpty) ...[
                          Text(
                            recipeTitle,
                            style: const TextStyle(fontSize: 24, fontWeight: FontWeight.bold),
                          ),
                          if (recipeAuthor.isNotEmpty)
                            Padding(
                              padding: const EdgeInsets.only(top: 4),
                              child: Text(
                                'Recipe by $recipeAuthor',
                                style: TextStyle(fontSize: 13, color: Colors.grey.shade600),
                              ),
                            ),
                          const SizedBox(height: 12),
                        ],
                        if (recipeDesc.isNotEmpty) ...[
                          Text(
                            '"$recipeDesc"',
                            style: const TextStyle(fontSize: 14, fontStyle: FontStyle.italic),
                          ),
                          const SizedBox(height: 16),
                        ],
                        if (photoBytes != null && photoBytes.isNotEmpty)
                          ConstrainedBox(
                            constraints: const BoxConstraints(maxHeight: 340, maxWidth: 520),
                            child: ClipRRect(
                              borderRadius: BorderRadius.circular(6),
                              child: Image.memory(photoBytes, fit: BoxFit.contain, gaplessPlayback: true),
                            ),
                          ),
                        const SizedBox(height: 16),
                        InkWell(
                          onTap: () => actionOpenUrl(resolvedUrl),
                          child: Text(
                            resolvedUrl,
                            style: const TextStyle(
                              fontSize: 13,
                              color: Color(0xFF0066CC),
                              decoration: TextDecoration.underline,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
          ),
        ],
      ),
    );
  }

  /// Runs the button's assigned action. Buttons from older layouts without an
  /// action keep the previous behaviour, guessed from their label.
  Future<void> _handleLayoutButtonClick(LayoutObjectModel obj, String label) async {
    if (obj.type == 'popover_button' && (widget.layout?.objects.any((o) => o.panelControl == obj.id) ?? false)) {
      // A popover button opens its popover, and closes it.
      setState(() => _selectedPanels[obj.id] = (_selectedPanels[obj.id] ?? obj.defaultPanel) == 0 ? -1 : 0);
      return;
    }
    final action = obj.action;
    if (action == null) {
      _handleLegacyButtonLabel(label);
      return;
    }
    if (!action.enabled) {
      // A migrated button whose script could not be told is kept off
      // until one is chosen in Layout mode.
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(
        content: Text(action.isPerformScript
            ? 'This button is switched off: choose its script in Layout mode.'
            : 'This button is switched off: File4Base does not run its step yet.'),
        backgroundColor: Colors.orange.shade800,
      ));
      return;
    }
    final result = await _actionRunner().run(action);
    if (!result.completed && mounted && result.message != null) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(result.message!), backgroundColor: Colors.orange.shade800),
      );
    }
  }

  // ─── LayoutActionHost (button actions and scripts) ─────────────────────────

  Map<String, dynamic> get _currentRecord => _records.isNotEmpty ? _records[_currentIndex] : const {};

  String _resolveTextForRecord(String text, Map<String, dynamic> rec) => resolveLayoutMergeText(
        text,
        record: rec,
        userName: widget.currentUserName,
        layoutName: widget.layout?.name,
        pageNumber: 1,
        formatField: _mergeFieldText,
        collapseEmptyLines: true,
      );

  @override
  String actionResolveText(String text) => _resolveTextForRecord(text, _currentRecord);

  @override
  Set<String> get actionOwnOccurrenceNames => {
        for (final name in [
          widget.layout?.tableOccurrence,
          _layoutOccurrence?.name,
          // The table's names stand for the layout's occurrence only where
          // they are not another occurrence's (a self-join).
          if (_isOwnOccurrence(widget.table.name)) widget.table.name,
          if (_isOwnOccurrence(widget.table.displayName)) widget.table.displayName,
        ])
          if (name != null && name.trim().isNotEmpty) name.trim().toLowerCase(),
      };

  @override
  Future<EvaluationModel?> actionEvaluateInScript(
    String expression, {
    Map<String, String> variables = const {},
    Map<String, Object?> get = const {},
  }) async {
    // A new record not committed yet exists only here: the server reads
    // its values from the request, and has no stored record to read.
    final draft = _onDraft;
    final recordId = _records.isEmpty || draft ? '' : (_currentRecord['id']?.toString() ?? '');
    final results = await widget.apiClient.evaluateExpressions(
      widget.table.name,
      expressions: [expression],
      recordId: recordId.isEmpty ? null : recordId,
      variables: variables,
      get: get,
      values: draft ? _draftValues(_currentRecord) : null,
    );
    return results.firstOrNull;
  }

  /// The field values of a new record not committed yet, as the server
  /// reads them: its own keys, and values that are not text, left out.
  static Map<String, Object?> _draftValues(Map<String, dynamic> record) => {
        for (final e in record.entries)
          if (!e.key.startsWith('__') &&
              e.key != 'id' &&
              (e.value == null || e.value is String || e.value is num || e.value is bool))
            e.key: e.value,
      };

  /// What a field's value looks like inside merge text.
  String _mergeFieldText(String fieldName, Object? value) {
    final col = widget.table.columns.where((c) => c.name == fieldName).firstOrNull;
    return col == null ? (value?.toString() ?? '') : fieldText(col, value);
  }

  @override
  Future<void> actionNewRecord() => _createNewRecord();

  @override
  Future<void> actionDuplicateRecord() async {
    if (!_canCreate || _records.isEmpty) return;
    if (!await _saveCurrentRecord()) return;
    final source = _records[_currentIndex];
    final sourceId = source['id']?.toString() ?? '';
    if (sourceId.isEmpty) return;
    try {
      // The server copies the record and its auto-enter options enter new
      // serial numbers and stamps, as FileMaker's Duplicate Record does. A
      // record of another solution is duplicated there, with its serials.
      final created = await _write(() => widget.apiClient.duplicateRow(widget.table.name, sourceId));
      if (!mounted) return;
      // The copy joins the found set in hand, as the current record.
      setState(() {
        _records.add(created);
        _unsortedRecords.add(created);
        _currentIndex = _records.length - 1;
      });
      _rebuildFieldControllers();
      _recordChanged();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Error duplicating record: $e'), backgroundColor: Colors.red),
        );
      }
    }
  }

  @override
  Future<void> actionDeleteRecord({required bool confirm}) async {
    if (_records.isEmpty || (!_onDraft && !_canDelete)) return;
    if (confirm) {
      final ok = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Delete Record'),
          content: const Text('Permanently delete this entire record?'),
          actions: [
            TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancel')),
            FilledButton(
              style: FilledButton.styleFrom(backgroundColor: Colors.red),
              onPressed: () => Navigator.of(ctx).pop(true),
              child: const Text('Delete'),
            ),
          ],
        ),
      );
      if (ok != true) return;
    }
    await _deleteCurrentRecord();
  }

  @override
  Future<void> actionCommitRecord() async {
    FocusManager.instance.primaryFocus?.unfocus();
    await _saveCurrentRecord();
  }

  @override
  Future<void> actionRevertRecord() async {
    // OnRecordRevert runs before a changed record is reverted, and False
    // keeps its changes (#26).
    if (_recordDirty && !await _triggers.fire(_trigger('OnRecordRevert'))) return;
    _activeBox = null;
    _holdFieldSaves = false;
    for (final timer in _fieldDebounceTimers.values) {
      timer?.cancel();
    }
    FocusManager.instance.primaryFocus?.unfocus();
    if (!mounted) return;
    // Reverting a new record that was never committed discards it
    setState(_onDraft ? _discardDraft : _rebuildFieldControllers);
  }

  @override
  Future<void> actionGoToRecord(String target) async {
    if (_records.isEmpty) return;
    final t = target.trim().toLowerCase();
    final index = switch (t) {
      'first' || 'primero' => 0,
      'previous' || 'prev' || 'anterior' => _currentIndex - 1,
      'next' || 'siguiente' => _currentIndex + 1,
      'last' || 'último' || 'ultimo' => _records.length - 1,
      _ => (int.tryParse(t) ?? (_currentIndex + 2)) - 1,
    };
    unawaited(goToRecord(index.clamp(0, _records.length - 1)));
  }

  @override
  Future<void> actionEnterFindMode() async => _requestMode(OperationalMode.find);

  @override
  Future<void> actionEnterBrowseMode() async => _requestMode(OperationalMode.browse);

  @override
  Future<void> actionPerformFind() => _performFind();

  @override
  Future<void> actionShowAllRecords() async {
    if (!await _requestMode(OperationalMode.browse)) return;
    await _fetchRecords();
  }

  @override
  Future<void> actionEnterPreviewMode() async => _requestMode(OperationalMode.preview);

  @override
  Future<void> actionAdjustWindow(String windowAction) async =>
      widget.onAdjustWindow?.call(windowAction);

  @override
  Future<void> actionReLogin() async => widget.onReLogin?.call();

  @override
  Future<void> actionCloseFile() async => widget.onCloseFile?.call();

  @override
  Future<void> actionExitApplication() async => widget.onExitApplication?.call();

  @override
  Future<bool> actionGoToLayout(String layoutName) async => widget.onGoToLayout?.call(layoutName) ?? false;

  @override
  Future<bool> actionSetField(String field, String value) async {
    final col = widget.table.columns.where((c) => c.name.toLowerCase() == field.toLowerCase()).firstOrNull;
    if (col == null || col.isPrimaryKey || _records.isEmpty) return col != null && !col.isPrimaryKey;
    _fieldDebounceTimers[col.name]?.cancel();
    _fieldControllers[col.name]?.text = value;
    await _saveField(col.name, value, script: true);
    return true;
  }

  // ---- RelatedFieldHost ----

  /// The active portal row (#26), on the record whose portal it is (in a
  /// list, the record of that list row): a step writing a field of that
  /// portal's occurrence writes this row, as FileMaker writes the active
  /// portal row, while it is still related. Going to a field of the record
  /// itself leaves the portal.
  ({String recordId, String occurrenceId, String rowId})? get _activePortalRow {
    final rows = _activePortal;
    final rowId = rows?.activeRecord?['id']?.toString() ?? '';
    final recordId = rows?.parentId ?? '';
    final target = rows?.target;
    if (target == null || recordId.isEmpty || rowId.isEmpty) return null;
    return (recordId: recordId, occurrenceId: target.occurrence.id, rowId: rowId);
  }

  /// Counts the writes of related records, so the related fields shown read
  /// theirs again.
  int _relatedWrites = 0;

  /// Whether [occurrence], as a step names it, is the occurrence this layout
  /// is on. Only the layout's occurrence is: another occurrence of the same
  /// table (a self-join) is a related one. The table's own name stands for
  /// the layout's occurrence only where no occurrence of the graph has that
  /// name, or the layout does not say its occurrence. Any other name, even
  /// one the graph does not know, is never taken for the layout's own.
  bool _isOwnOccurrence(String occurrence) {
    final key = _occurrenceKey(occurrence);
    if (key.isEmpty) return true;
    final layoutOccurrence = widget.layout?.tableOccurrence ?? '';
    final own = <String>{
      _occurrenceKey(layoutOccurrence),
      _occurrenceKey(_layoutOccurrence?.name ?? ''),
    }..remove('');
    if (own.contains(key)) return true;
    if (key != _occurrenceKey(widget.table.name)) return false;
    if (own.isEmpty) return true;
    return _occurrences.isNotEmpty && _occurrenceNamed(occurrence) == null;
  }

  TableOccurrenceModel? _occurrenceNamed(String occurrence) {
    final key = _occurrenceKey(occurrence);
    return _occurrences[occurrence] ?? _occurrences.values.where((o) => _occurrenceKey(o.name) == key).firstOrNull;
  }

  /// The occurrence this layout is on, once the graph is read.
  TableOccurrenceModel? get _layoutOccurrence {
    final raw = widget.layout?.tableOccurrence ?? '';
    return raw.trim().isEmpty ? null : _occurrenceNamed(raw);
  }

  /// The relationship from the layout's occurrence to [occurrence] in the
  /// graph, and the end of it [occurrence] is. A layout that does not say
  /// its occurrence follows the relationships of its table.
  RelatedTarget? _relationshipTo(TableOccurrenceModel occurrence) {
    final from = _layoutOccurrence;
    if (from == null) {
      return (widget.layout?.tableOccurrence ?? '').trim().isEmpty ? _relatedContext?.resolve(null, occurrence.id) : null;
    }
    final external = occurrence.external;
    final table = external == null
        ? _tablesById[occurrence.baseTableId]
        : TableModel(id: occurrence.id, name: external.table, displayName: occurrence.name);
    if (table == null) return null;
    for (final rel in _relationships.values) {
      if (rel.leftOccurrenceId == from.id && rel.rightOccurrenceId == occurrence.id) {
        return RelatedTarget(relationship: rel, occurrence: occurrence, table: table);
      }
      if (rel.rightOccurrenceId == from.id && rel.leftOccurrenceId == occurrence.id) {
        return RelatedTarget(relationship: rel, occurrence: occurrence, table: table, side: 'left');
      }
    }
    return null;
  }

  @override
  Future<bool> actionIsOwnOccurrence(String occurrence) async {
    if (_isOwnOccurrence(occurrence)) return true;
    if (_occurrences.isEmpty) await _loadRelationshipGraph();
    return _isOwnOccurrence(occurrence);
  }

  /// Where a step writes [field] of [occurrence], another occurrence: the
  /// relationship to it, the column, and the related record — the active
  /// portal row of that occurrence, else the first related record in the
  /// relationship's order, else none. `code` is FileMaker's error when there
  /// is nowhere to write.
  Future<({int code, RelatedContext? related, RelatedTarget? target, ColumnModel? column, String? rowId, Map<String, dynamic>? row})>
      _relatedSlot(String occurrence, String field) async {
    const none = (code: 102, related: null, target: null, column: null, rowId: null, row: null);
    // A record not committed yet has no related records: it is committed
    // first, as FileMaker does before following a relationship from it, so
    // what is read and what is written are the same record's.
    if (_onDraft && !await _saveCurrentRecord()) {
      return (code: 3, related: null, target: null, column: null, rowId: null, row: null);
    }
    if (_occurrences.isEmpty) await _loadRelationshipGraph();
    final related = _relatedContext;
    final occ = _occurrenceNamed(occurrence);
    if (related == null || occ == null) return none;
    final target = _relationshipTo(occ);
    if (target == null) return (code: 103, related: null, target: null, column: null, rowId: null, row: null);
    TableModel table;
    try {
      table = target.isExternal ? await ExternalAccess.of(widget.apiClient).table(target.occurrence) : target.table;
    } on Object catch (_) {
      return (code: 3, related: null, target: null, column: null, rowId: null, row: null);
    }
    final name = field.trim().toLowerCase();
    final column = table.columns.where((c) => c.name.toLowerCase() == name).firstOrNull ??
        table.columns.where((c) => c.displayName.toLowerCase() == name).firstOrNull;
    if (column == null) return none;
    final recordId = related.recordId;
    if (recordId == null) return (code: 101, related: related, target: target, column: column, rowId: null, row: null);
    var active = _activePortalRow;
    if (active != null && (active.recordId != recordId || active.occurrenceId != target.occurrence.id)) active = null;
    // The active row is written only while it is still related: a write may
    // have changed a match field since it was clicked.
    final rows = await related.rows(target, limit: active == null ? 1 : 1000);
    if (active != null) {
      final activeRow = rows.where((r) => r['id']?.toString() == active!.rowId).firstOrNull;
      if (activeRow != null) {
        return (code: 0, related: related, target: target, column: column, rowId: active.rowId, row: activeRow);
      }
      _deactivatePortal();
    }
    final first = rows.firstOrNull;
    return (code: 0, related: related, target: target, column: column, rowId: first?['id']?.toString(), row: first);
  }

  @override
  Future<String?> actionRelatedFieldText(String occurrence, String field) async {
    try {
      final slot = await _relatedSlot(occurrence, field);
      final column = slot.column;
      if (column == null) return null;
      final rowId = slot.rowId;
      if (rowId == null) return '';
      return recordFieldText(column, slot.row ?? const {});
    } on Object catch (_) {
      return null;
    }
  }

  @override
  Future<int> actionSetRelatedField(String occurrence, String field, String value) async {
    if (!mounted) return 3;
    if (_mode != OperationalMode.browse) return 3;
    try {
      final slot = await _relatedSlot(occurrence, field);
      if (slot.code != 0) return slot.code;
      final related = slot.related!;
      final target = slot.target!;
      final column = slot.column!;
      if (column.isPrimaryKey || isComputedColumn(column)) return 201;
      final rowId = slot.rowId;
      if (rowId == null) {
        if (!target.allowsCreation) return 101;
        await _write(() => related.apiClient.createRelatedRow(
              related.fromTable,
              related.recordId!,
              relationshipId: target.relationship.id,
              occurrence: target.occurrence.id,
              side: target.requestSide,
              values: {column.name: value},
            ));
      } else {
        await _write(() => related.clientFor(target).updateRow(target.table.name, rowId, {column.name: value}, script: true));
      }
    } on Object catch (e) {
      return _errorCodeOf(e);
    }
    if (!mounted) return 0;
    setState(() => _relatedWrites++);
    // A calculation of the record in hand, and the portals, may show it.
    await actionRefreshWindow();
    return 0;
  }

  // ---- AutoEnterActionHost ----

  /// The column a script step or a menu names: `Table::Field` or `Field`, by
  /// its name or the name shown. A field of another occurrence is not one of
  /// this table's, whatever its name.
  ColumnModel? _columnNamed(String field) {
    final cut = field.indexOf('::');
    if (cut >= 0 && !_isOwnOccurrence(field.substring(0, cut))) return null;
    final name = (cut < 0 ? field : field.substring(cut + 2)).trim().toLowerCase();
    if (name.isEmpty) return null;
    return widget.table.columns.where((c) => c.name.toLowerCase() == name).firstOrNull ??
        widget.table.columns.where((c) => c.displayName.toLowerCase() == name).firstOrNull;
  }

  /// FileMaker's error code for a write the server refused.
  static int _errorCodeOf(Object error) {
    if (error is! ApiException) return 3;
    return switch (error.statusCode) {
      403 => 200,
      404 => 102,
      400 || 422 => 509,
      _ => 3,
    };
  }

  void _notify(String message, {bool error = false}) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(message), backgroundColor: error ? Colors.red : null),
    );
  }

  @override
  Future<int> actionSetNextSerialValue(String field, String value) async {
    // A field named Occurrence::Field is that occurrence's table's, which
    // may not be the layout's, nor even this solution's.
    final cut = field.indexOf('::');
    final occurrence = cut < 0 ? null : field.substring(0, cut).trim();
    final name = cut < 0 ? field.trim() : field.substring(cut + 2).trim();
    if (occurrence != null && occurrence.isNotEmpty && _occurrences.isEmpty) await _loadRelationshipGraph();
    if (!mounted) return 3;
    if (occurrence == null || !_namesOtherOccurrence(name, occurrence)) {
      final col = _columnNamed(name);
      if (col == null) return 102;
      try {
        await _write(() => widget.apiClient.setNextSerialValue(widget.table.name, col.name, value));
        return 0;
      } on Object catch (e) {
        return _errorCodeOf(e);
      }
    }
    final key = _occurrenceKey(occurrence);
    final target = _occurrences.values.where((o) => _occurrenceKey(o.name) == key).firstOrNull;
    if (target == null) {
      _notify('Set Next Serial Value: there is no table occurrence "$occurrence".', error: true);
      return 102;
    }
    var fieldName = name;
    final base = target.baseTableId == null ? null : _tablesById[target.baseTableId];
    if (target.external == null) {
      final col = base?.columns
              .where((c) => c.name.toLowerCase() == name.toLowerCase() || c.displayName.toLowerCase() == name.toLowerCase())
              .firstOrNull;
      if (base == null || col == null) {
        _notify('Set Next Serial Value: $occurrence has no field "$name".', error: true);
        return 102;
      }
      fieldName = col.name;
    }
    try {
      // The occurrence's records route sets it in its table, in the
      // solution it is in, as the account signed in there.
      final client = widget.apiClient.forOccurrence(target.id);
      await _write(() => client.setNextSerialValue(base?.name ?? target.name, fieldName, value));
      return 0;
    } on Object catch (e) {
      _notify('Set Next Serial Value could not set $occurrence::$name: $e', error: true);
      return _errorCodeOf(e);
    }
  }

  @override
  Future<int> actionRelookupFieldContents(String field, {bool dialog = false}) async {
    final col = _columnNamed(field);
    if (col == null) return 102;
    final (code, _) = await _relookup(col, dialog: dialog);
    return code;
  }

  /// Relookup Field Contents from the match field [col] over the found set
  /// in hand, which keeps it: only its records are sent, and those the server
  /// changed are shown as they are now. Returns FileMaker's error code and how
  /// many records the server changed.
  Future<(int, int)> _relookup(ColumnModel col, {required bool dialog}) async {
    if (!await _saveCurrentRecord()) return (3, 0);
    final ids = [
      for (final r in _records)
        if (r[_draftKey] != true && (r['id']?.toString() ?? '').isNotEmpty) r['id'].toString(),
    ];
    if (ids.isEmpty) return (401, 0);
    if (dialog) {
      if (!mounted) return (1, 0);
      final n = ids.length;
      final ok = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Relookup Field Contents'),
          content: Text('Relookup $n ${n == 1 ? 'record' : 'records'} using the matching field '
              '"${col.displayName}"?'),
          actions: [
            TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancel')),
            FilledButton(onPressed: () => Navigator.of(ctx).pop(true), child: const Text('OK')),
          ],
        ),
      );
      if (ok != true) return (1, 0);
    }
    try {
      final result = await _write(() => widget.apiClient.relookupFieldContents(widget.table.name, col.name, ids));
      // The found set stays as it is, in its order: the records the server
      // changed are put in place of what was read of them.
      if (mounted && result.records.isNotEmpty) {
        final byId = {for (final r in result.records) r['id']?.toString(): r};
        List<Map<String, dynamic>> refreshed(List<Map<String, dynamic>> rows) =>
            [for (final r in rows) byId[r['id']?.toString()] ?? r];
        setState(() {
          _records = refreshed(_records);
          _unsortedRecords = refreshed(_unsortedRecords);
        });
        _rebuildFieldControllers();
      }
      return (0, result.updated);
    } on Object catch (e) {
      if (dialog) _notify('Relookup Field Contents failed: $e', error: true);
      return (_errorCodeOf(e), 0);
    }
  }

  @override
  Future<void> actionShowDialog(String title, String message) async {
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(title),
        content: Text(message),
        actions: [FilledButton(onPressed: () => Navigator.of(ctx).pop(), child: const Text('OK'))],
      ),
    );
  }

  @override
  Future<void> actionOpenUrl(String url) async {
    final uri = Uri.tryParse(url);
    if (uri == null || !(uri.isScheme('http') || uri.isScheme('https') || uri.isScheme('mailto'))) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Cannot open "$url".')));
      }
      return;
    }
    await launchUrl(uri, mode: LaunchMode.externalApplication);
  }

  // ---- ExtendedLayoutActionHost (#133) ----

  @override
  String? get actionActiveFieldName => activeFieldName;

  @override
  int get actionFoundCount => _records.length;

  @override
  int get actionCurrentRecordNumber => _records.isEmpty ? 0 : _currentIndex + 1;

  @override
  int get actionTotalRecordCount => _totalInTable > 0 ? _totalInTable : _records.length;

  @override
  Future<GoToRecordOutcome> actionGoToRecordEx(String target) async {
    if (_records.isEmpty) {
      return const GoToRecordOutcome(moved: false, atBoundary: true, errorCode: 101);
    }
    final t = target.trim().toLowerCase();
    int requested;
    switch (t) {
      case 'first' || 'primero':
        requested = 0;
      case 'previous' || 'prev' || 'anterior':
        requested = _currentIndex - 1;
        if (requested < 0) {
          return const GoToRecordOutcome(moved: false, atBoundary: true, errorCode: 101);
        }
      case 'next' || 'siguiente':
        requested = _currentIndex + 1;
        if (requested >= _records.length) {
          return const GoToRecordOutcome(moved: false, atBoundary: true, errorCode: 101);
        }
      case 'last' || 'último' || 'ultimo':
        requested = _records.length - 1;
      default:
        final parsed = int.tryParse(t);
        if (parsed == null || parsed < 1 || parsed > _records.length) {
          return const GoToRecordOutcome(moved: false, atBoundary: true, errorCode: 101);
        }
        requested = parsed - 1;
    }
    await goToRecord(requested.clamp(0, _records.length - 1));
    return const GoToRecordOutcome(moved: true);
  }

  @override
  Future<bool> actionGoToLayoutByNumber(int layoutNumber) async {
    try {
      final layouts = await widget.apiClient.listLayouts();
      if (layoutNumber < 1 || layoutNumber > layouts.length) return false;
      return await actionGoToLayout(layouts[layoutNumber - 1].name);
    } catch (_) {
      return false;
    }
  }

  List<Map<String, dynamic>> _buildApiFindRequests(List<Map<String, dynamic>>? requests) {
    if (requests == null || requests.isEmpty) return const [];
    final out = <Map<String, dynamic>>[];
    for (final req in requests) {
      final omit = req['action']?.toString().toLowerCase() == 'omit' || req['omit'] == true;
      final rawCriteria = req['criteria'];
      final criteria = <Map<String, dynamic>>[];
      if (rawCriteria is List) {
        for (final item in rawCriteria.whereType<Map<dynamic, dynamic>>()) {
          final field = item['field']?.toString().trim() ?? '';
          final val = item['value']?.toString() ?? '';
          if (field.isEmpty) continue;
          criteria.add({'field_name': field, 'operator': '=', 'value': val});
        }
      }
      if (criteria.isNotEmpty) {
        out.add({'criteria': criteria, 'omit': omit});
      }
    }
    return out;
  }

  @override
  Future<void> actionEnterFindModeEx({List<Map<String, dynamic>>? requests, bool restore = false}) async {
    if (!await _requestMode(OperationalMode.find)) return;
    if (restore && requests != null && requests.isNotEmpty) {
      final drafts = <FindRequestDraft>[];
      for (final req in requests) {
        final omit = req['action']?.toString().toLowerCase() == 'omit' || req['omit'] == true;
        final values = <String, String>{};
        final rawCriteria = req['criteria'];
        if (rawCriteria is List) {
          for (final item in rawCriteria.whereType<Map<dynamic, dynamic>>()) {
            final field = item['field']?.toString().trim() ?? '';
            if (field.isNotEmpty) {
              values[field] = item['value']?.toString() ?? '';
            }
          }
        }
        drafts.add(FindRequestDraft(values: values, omit: omit));
      }
      if (drafts.isNotEmpty && mounted) {
        setState(() {
          _findRequests
            ..clear()
            ..addAll(drafts);
          _findRequestIndex = 0;
        });
      }
    }
  }

  @override
  Future<int> actionPerformFindEx({List<Map<String, dynamic>>? requests, bool restore = false}) async {
    final payload = (restore && requests != null && requests.isNotEmpty)
        ? _buildApiFindRequests(requests)
        : _findRequestPayload();
    if (payload.isEmpty) {
      await _performFind();
      return _records.length;
    }
    // OnModeExit decides before the found set is replaced (#26); its False
    // cancels the find.
    if (!await _mayLeaveModeFor(OperationalMode.browse)) return -1;
    try {
      final results = _layoutIsReport
          ? await widget.apiClient.executeFindAll(widget.table.name, payload)
          : await widget.apiClient.executeFind(widget.table.name, payload);
      if (!mounted) return results.length;
      if (results.isEmpty) {
        return 0;
      }
      setState(() {
        _unsortedRecords = List<Map<String, dynamic>>.from(results);
        _records = List<Map<String, dynamic>>.from(results);
        _currentIndex = 0;
        _isFoundSet = true;
        _foundSetFind = payload;
        _foundSetInHand = false;
        _omittedIds.clear();
      });
      _rebuildFieldControllers();
      _applyMode(OperationalMode.browse);
      _recordChanged();
      return results.length;
    } catch (_) {
      return 0;
    }
  }

  @override
  Future<void> actionSortRecords({List<Map<String, dynamic>>? sortOrder, bool restore = false, bool dialog = true}) async {
    if (restore && sortOrder != null && sortOrder.isNotEmpty) {
      final levels = <SortLevel>[];
      for (final item in sortOrder) {
        final field = item['field']?.toString().trim() ?? '';
        if (field.isEmpty) continue;
        final asc = item['order']?.toString().toLowerCase() != 'descending';
        levels.add(SortLevel(field, asc));
      }
      if (levels.isNotEmpty) {
        _applySortOrder(levels);
        return;
      }
    }
    if (dialog && mounted) {
      await _showSortDialog();
    } else if (_sortOrder.isNotEmpty) {
      _applySortOrder(_sortOrder);
    }
  }

  @override
  Future<void> actionUnsortRecords() async {
    _applySortOrder(const []);
  }

  @override
  Future<bool> actionOmitRecord() async {
    if (_records.isEmpty || _currentIndex < 0 || _currentIndex >= _records.length) {
      return false;
    }
    final removedId = _records[_currentIndex]['id'];
    // Omitting from the whole table makes a found set no find describes:
    // the table's records not read yet are read first, so it holds them.
    final wholeTable = !_isFoundSet;
    if (wholeTable && (!await _readWholeFoundSet() || !mounted)) return false;
    final at = removedId == null ? _currentIndex : _records.indexWhere((r) => r['id'] == removedId);
    if (at < 0) return false;
    setState(() {
      if (wholeTable) _foundSetInHand = true;
      if (removedId != null) _omittedIds.add(removedId.toString());
      _currentIndex = at;
      _records.removeAt(at);
      _unsortedRecords.removeWhere((r) => r['id'] == removedId);
      if (_currentIndex >= _records.length && _records.isNotEmpty) {
        _currentIndex = _records.length - 1;
      }
      _isFoundSet = true;
    });
    _rebuildFieldControllers();
    _recordChanged();
    return true;
  }

  @override
  Future<void> actionOmitMultipleRecords(int count) async {
    for (var i = 0; i < count && _records.isNotEmpty; i++) {
      await actionOmitRecord();
    }
  }

  @override
  String get actionImportTable => widget.externalDatabase == null ? widget.table.name : '';

  @override
  Future<bool> actionCommitBeforeImport() async {
    if (_records.isEmpty) return true;
    FocusManager.instance.primaryFocus?.unfocus();
    return _saveCurrentRecord();
  }

  @override
  Future<List<String>?> actionFoundSetIds() async {
    if (!await _readWholeFoundSet() || !mounted) return null;
    return _records.map((r) => r['id']?.toString()).whereType<String>().where((id) => id.isNotEmpty).toList();
  }

  @override
  Future<bool> actionShowImportedRecords(List<String> ids) async {
    final List<Map<String, dynamic>> all;
    try {
      all = await widget.apiClient.listAllRows(widget.table.name);
    } on Exception {
      return false;
    }
    if (!mounted) return false;
    final byId = {for (final r in all) r['id']?.toString(): r};
    final imported = [for (final id in ids) ?byId[id]];
    setState(() {
      _sortOrder = const [];
      _unsortedRecords = List<Map<String, dynamic>>.from(imported);
      _records = List<Map<String, dynamic>>.from(imported);
      _currentIndex = 0;
      _isFoundSet = true;
      _foundSetFind = null;
      _foundSetInHand = true;
      _omittedIds.clear();
    });
    _rebuildFieldControllers();
    _recordChanged();
    return true;
  }

  @override
  Future<void> actionShowOmittedOnly() async {
    try {
      final all = await widget.apiClient.listRows(widget.table.name, limit: 1000);
      final currentIds = _records.map((r) => r['id']).toSet();
      final omitted = all.where((r) => !currentIds.contains(r['id'])).toList();
      if (!mounted) return;
      setState(() {
        _unsortedRecords = List<Map<String, dynamic>>.from(omitted);
        _records = List<Map<String, dynamic>>.from(omitted);
        _currentIndex = 0;
        _isFoundSet = true;
        _foundSetFind = null;
        _foundSetInHand = true;
        _omittedIds.clear();
      });
      _rebuildFieldControllers();
      _recordChanged();
    } catch (_) {
      // Preserve current found set if server reload fails.
    }
  }

  @override
  Future<void> actionDeleteAllRecords({required bool confirm}) async {
    if (_records.isEmpty || !_canDelete) return;
    if (confirm && mounted) {
      final ok = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Delete All Records'),
          content: Text('Permanently delete all ${_records.length} record(s) in the found set?'),
          actions: [
            TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancel')),
            FilledButton(
              style: FilledButton.styleFrom(backgroundColor: Colors.red),
              onPressed: () => Navigator.of(ctx).pop(true),
              child: const Text('Delete All'),
            ),
          ],
        ),
      );
      if (ok != true) return;
    }
    final ids = _records.map((r) => r['id']?.toString()).whereType<String>().where((id) => id.isNotEmpty).toList();
    for (final id in ids) {
      await _write(() => widget.apiClient.deleteRow(widget.table.name, id));
    }
    await _fetchRecords();
  }

  @override
  Future<void> actionCommitRecordEx({bool dialog = true, bool skipValidation = false, bool forceCommit = false}) async {
    await actionCommitRecord();
  }

  @override
  Future<bool> actionGoToField(String field, {bool select = false}) async {
    final col = widget.table.columns.where((c) => c.name.toLowerCase() == field.toLowerCase()).firstOrNull;
    if (col == null) return false;
    // The active field is left, and the field gone to entered, with their
    // triggers (#26). A trigger that keeps the active field active leaves
    // the step without effect.
    final object = widget.layout?.objects
        .where((o) =>
            o.type == 'field' &&
            o.fieldBinding != null &&
            !o.fieldBinding!.isRelated &&
            o.fieldBinding!.fieldName.toLowerCase() == col.name.toLowerCase())
        .firstOrNull;
    if (object != null && !col.isPrimaryKey && _mode != OperationalMode.preview) {
      final box = _mode == OperationalMode.find
          ? _findBox(object, col.name, _findFocusNodes.putIfAbsent(object.id, FocusNode.new))
          : _fieldBox(object, col, col.name);
      if (!box.same(_activeBox)) {
        final previous = _activeBox;
        if (previous != null && !await _exitBox(previous)) return true;
        _activateBox(box);
        await _triggers.fire(_trigger('OnObjectEnter', objectId: object.id));
      }
    }
    _lastFocusedBrowseField = col.name;
    widget.onFocusedFieldChanged?.call();
    if (select) {
      final ctrl = _fieldControllers[col.name];
      if (ctrl != null) {
        ctrl.selection = TextSelection(baseOffset: 0, extentOffset: ctrl.text.length);
      }
    }
    return true;
  }

  @override
  Future<void> actionGoToNextField() async {
    final editable = widget.table.columns.where((c) => !c.isPrimaryKey).map((c) => c.name).toList();
    if (editable.isEmpty) return;
    final idx = editable.indexWhere((n) => n.toLowerCase() == (_lastFocusedBrowseField ?? '').toLowerCase());
    final next = editable[(idx + 1) % editable.length];
    _lastFocusedBrowseField = next;
    widget.onFocusedFieldChanged?.call();
  }

  @override
  Future<bool> actionGoToObject(String objectName) async {
    final layout = widget.layout;
    if (layout == null) return false;
    final found = layout.objects.where((o) =>
        o.id.toLowerCase() == objectName.toLowerCase() ||
        (o.fieldBinding?.fieldName.toLowerCase() == objectName.toLowerCase())).firstOrNull;
    if (found == null) return false;
    if (found.fieldBinding != null) {
      _lastFocusedBrowseField = found.fieldBinding!.fieldName;
      widget.onFocusedFieldChanged?.call();
    }
    return true;
  }

  @override
  Future<bool> actionGoToRelatedRecord({
    required String relatedTable,
    String? layoutName,
    bool showOnlyRelated = true,
    bool newWindow = false,
  }) async {
    if (layoutName != null && layoutName.isNotEmpty) {
      return actionGoToLayout(layoutName);
    }
    return false;
  }

  /// Go to Portal Row (#26): in the active portal, or the first portal of
  /// the layout when none is active, makes a row the active one. The field
  /// active in the portal stays the active field of the new row (left and
  /// entered with its triggers), otherwise the first field of the row that
  /// takes entry is; [select] selects the whole row instead. Past either
  /// end, or for a row the portal does not have, the step sets error 101.
  @override
  Future<GoToRecordOutcome> actionGoToPortalRow(String target, {bool select = false}) async {
    const missing = GoToRecordOutcome(moved: false, atBoundary: true, errorCode: 101);
    final layout = widget.layout;
    if (layout == null || _mode != OperationalMode.browse) return missing;
    var rows = _activePortal;
    if (rows == null) {
      final first = layout.objects.where((o) => o.type == 'portal').firstOrNull;
      if (first == null) return missing;
      final inList = _viewMode == 'list';
      final current = currentRecord?['id']?.toString();
      rows = _portalRowController(first, listRecordId: inList ? current : null);
    }
    // A portal still showing the record left reads the record in hand's
    // related records first: never a row of another record's.
    if (!_portalOfCurrentRecord(rows)) {
      rows.loading();
      await WidgetsBinding.instance.endOfFrame;
    }
    await rows.whenLoaded();
    if (!mounted || !_portalOfCurrentRecord(rows)) return missing;
    final count = rows.rows.length;
    final current = rows.activeRow;
    final t = target.trim().toLowerCase();
    final int row;
    switch (t) {
      case 'first':
        row = 1;
      case 'last':
        row = count;
      case 'previous' || 'prev':
        if (current == 1) return missing;
        row = current == 0 ? 1 : current - 1;
      case 'next':
        if (current >= count && !rows.truncated) return missing;
        row = current + 1;
      default:
        final n = num.tryParse(t)?.truncate();
        if (n == null) return missing;
        row = n;
    }
    // A portal that read only its first related records does not have its
    // last one: a row past those it has, or the last one, is refused (any
    // row when the page read is not in the portal's order), and
    // no row is left active, so a step after it (Delete Portal Row) never
    // acts on another row than the one asked for. Next past them ends a
    // loop like the last row does.
    if (rows.unordered || (rows.truncated && (t == 'last' || row > count))) {
      _deactivatePortal();
      return GoToRecordOutcome(moved: false, atBoundary: t == 'next', errorCode: 3);
    }
    if (count == 0 || row < 1 || row > count) return missing;

    final previous = _activeBox;
    final inPortal = previous?.portal?.id == rows.object.id ? previous!.object : null;
    if (previous != null) {
      if (!await _exitBox(previous)) return const GoToRecordOutcome(moved: false, errorCode: 20);
      if (previous.same(_activeBox)) _activeBox = null;
    }
    _makePortalActive(_portalRows.entries.firstWhere((e) => e.value == rows).key);
    rows.activate(row);
    if (select) {
      if (previous?.hasFocus() ?? false) FocusManager.instance.primaryFocus?.unfocus();
      return const GoToRecordOutcome(moved: true);
    }
    final fields = rows.enterableFields?.call() ?? const <LayoutObjectModel>[];
    final field = inPortal != null && fields.any((f) => f.id == inPortal.id) ? inPortal : fields.firstOrNull;
    final box = field == null ? null : await rows.focusCell?.call(row, field.id);
    if (box != null && mounted) {
      _activateBox(box);
      await _triggers.fire(_trigger('OnObjectEnter', objectId: box.object.id));
    }
    return const GoToRecordOutcome(moved: true);
  }

  /// Delete Portal Row (#26): deletes the related record of the active
  /// portal row, never the record in hand, which is the portal's parent.
  /// Without an active row the step sets error 101.
  @override
  Future<int> actionDeletePortalRow({required bool confirm}) async {
    final rows = _activePortal;
    final record = rows?.activeRecord;
    final delete = rows?.deleteRow;
    final rowId = record?['id']?.toString() ?? '';
    if (rows == null || delete == null || rowId.isEmpty) return 101;
    final code = await delete(rowId, confirm: confirm);
    if (code == 0) {
      if (_activeBox?.portal?.id == rows.object.id) _activeBox = null;
      _deactivatePortal();
    }
    return code;
  }

  @override
  Future<ReplaceOutcome> actionReplaceFieldContents({
    required String field,
    String? table,
    FutureOr<String?> Function(int position, int count)? valueFor,
  }) async {
    // A step that names a table occurrence is placed against the graph,
    // read once if this layout never needed it.
    if (table != null && table.trim().isNotEmpty && _occurrences.isEmpty) await _loadRelationshipGraph();
    if (!mounted) return const ReplaceOutcome(errorCode: 3, detail: 'the window is closed');
    if (_namesOtherOccurrence(field, table)) return const ReplaceOutcome(relatedField: true);
    final col = widget.table.columns.where((c) => c.name.toLowerCase() == field.toLowerCase()).firstOrNull;
    if (col == null || col.isPrimaryKey) {
      return ReplaceOutcome(errorCode: 102, detail: 'the field "$field" is not in this table');
    }
    if (const {'CALCULATION', 'SUMMARY'}.contains(col.fieldType.toUpperCase())) {
      return ReplaceOutcome(errorCode: 201, detail: 'the field "$field" is a ${col.fieldType.toLowerCase()} field');
    }
    // The record in hand is committed first: its contents may be the value,
    // and a new record must exist before it can be replaced.
    if (!await _saveCurrentRecord() || !mounted) {
      return const ReplaceOutcome(errorCode: 509, detail: 'the record in hand could not be committed');
    }
    final inHand = _records.isNotEmpty ? _records[_currentIndex] : const <String, dynamic>{};
    final contents = inHand[col.name]?.toString() ?? '';
    final startId = inHand['id'];
    if (!await _readWholeFoundSet()) {
      return const ReplaceOutcome(errorCode: -1, detail: 'the records of the found set could not be read');
    }
    final records = List.of(_records);
    final count = records.length;
    var replaced = 0;
    try {
      for (var i = 0; i < count; i++) {
        final rec = records[i];
        final id = rec['id']?.toString();
        if (id == null || id.isEmpty) continue;
        // The record is the current one while its value is worked out.
        _currentIndex = i;
        final text = valueFor == null ? contents : await valueFor(i + 1, count);
        if (!mounted) return ReplaceOutcome(count: count, replaced: replaced);
        if (text == null) return ReplaceOutcome(count: count, replaced: replaced, unevaluatedAt: i + 1);
        Object? toSave = text;
        if (col.fieldType == 'DATE' && text.trim().isNotEmpty) {
          toSave = _normalizeDateInput(text, col.name);
          if (toSave == null) {
            return ReplaceOutcome(errorCode: 500, detail: '"$text" is not a date', count: count, replaced: replaced);
          }
        } else if (col.fieldType == 'DATE') {
          toSave = null;
        }
        try {
          await _write(() => widget.apiClient.updateRow(widget.table.name, id, {col.name: toSave}, script: true));
        } on Exception catch (e) {
          return ReplaceOutcome(
            errorCode: _fileMakerWriteError(e),
            detail: e is ApiException ? e.detail : e.toString(),
            count: count,
            replaced: replaced,
          );
        }
        rec[col.name] = toSave;
        final texts = rec[sourceTextKey];
        if (texts is Map) texts.remove(col.name);
        replaced++;
        if (!mounted) return ReplaceOutcome(count: count, replaced: replaced);
      }
      return ReplaceOutcome(count: count, replaced: replaced);
    } finally {
      if (mounted) {
        setState(() {
          final idx = _records.indexWhere((r) => r['id'] == startId);
          _currentIndex = idx < 0 ? 0 : idx;
        });
        _rebuildFieldControllers();
        _recordChanged();
      }
    }
  }

  /// Reads the records of the found set that are not in hand yet: the found
  /// set on screen may be its first page only. The records in hand stay as
  /// they are, with the rest after them, in the found set's sort order.
  Future<bool> _readWholeFoundSet() async {
    if (_isFoundSet && _foundSetInHand) return true;
    final List<Map<String, dynamic>> rows;
    try {
      final find = _isFoundSet ? _foundSetFind : null;
      if (_isFoundSet && find == null) return true;
      rows = find != null
          ? await widget.apiClient.executeFindAll(widget.table.name, find)
          : await widget.apiClient.listAllRows(widget.table.name);
    } on Exception {
      return false;
    }
    if (!mounted) return false;
    final known = {for (final r in _unsortedRecords) r['id']?.toString()};
    final rest = rows.where((r) {
      final id = r['id']?.toString();
      return !known.contains(id) && !_omittedIds.contains(id);
    }).toList();
    if (rest.isEmpty) return true;
    _unsortedRecords = [..._unsortedRecords, ...rest];
    if (!_isFoundSet) _totalInTable = _unsortedRecords.length;
    _applySortOrder(_sortOrder);
    return true;
  }

  /// The FileMaker error a write the server rejected reports.
  static int _fileMakerWriteError(Object error) {
    if (error is! ApiException) return -1;
    final text = '${error.title} ${error.detail}'.toLowerCase();
    return switch (error.statusCode) {
      400 || 422 => text.contains('unique') ? 504 : 509,
      403 => 200,
      404 => 101,
      409 || 423 => 301,
      _ => -1,
    };
  }

  @override
  Future<ScriptDialogResult> actionShowDialogEx({
    required String title,
    required String message,
    List<Map<String, dynamic>>? buttons,
    List<Map<String, dynamic>>? inputFields,
  }) async {
    if (!mounted) return const ScriptDialogResult(buttonIndex: ScriptDialogResult.dismissed);
    final effectiveButtons = (buttons == null || buttons.isEmpty)
        ? const [<String, dynamic>{'label': 'OK', 'commit': true}]
        : buttons;
    final controllers = <String, TextEditingController>{};
    for (final inp in inputFields ?? const <Map<String, dynamic>>[]) {
      final key = inp['field']?.toString() ?? '';
      if (key.isNotEmpty) {
        controllers[key] = TextEditingController(text: inp['value']?.toString() ?? '');
      }
    }
    final choice = await showDialog<int>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(title),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (message.isNotEmpty) Text(message),
              for (final inp in inputFields ?? const <Map<String, dynamic>>[])
                if ((inp['field']?.toString() ?? '').isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: TextField(
                      controller: controllers[inp['field'].toString()],
                      obscureText: inp['password'] == true,
                      decoration: InputDecoration(
                        labelText: (inp['label']?.toString().isNotEmpty ?? false)
                            ? inp['label'].toString()
                            : inp['field'].toString(),
                        border: const OutlineInputBorder(),
                        isDense: true,
                      ),
                    ),
                  ),
            ],
          ),
        ),
        actions: [
          for (var i = effectiveButtons.length - 1; i >= 0; i--)
            if (i == 0)
              FilledButton(
                onPressed: () => Navigator.of(ctx).pop(1),
                child: Text(switch (effectiveButtons[i]['label']?.toString() ?? '') {
                  '' => 'OK',
                  final label => label,
                }),
              )
            else
              TextButton(
                onPressed: () => Navigator.of(ctx).pop(i + 1),
                child: Text(effectiveButtons[i]['label']?.toString() ?? 'Cancel'),
              ),
        ],
      ),
    );
    final values = <String, String>{
      for (final e in controllers.entries) e.key: e.value.text,
    };
    for (final c in controllers.values) {
      c.dispose();
    }
    // Closing the dialog without a button (Escape, a click outside it) is
    // not the first button: a script asking "Replace?" must not go ahead.
    return ScriptDialogResult(buttonIndex: choice ?? ScriptDialogResult.dismissed, inputValues: values);
  }

  @override
  Future<void> actionPrint({bool dialog = true, Map<String, dynamic>? printSettings}) async {
    await _requestMode(OperationalMode.preview);
  }

  @override
  Future<void> actionSaveRecordsAsPdf({String? path, bool dialog = true, bool append = false}) async {
    await _requestMode(OperationalMode.preview);
  }

  // ─── Documents of a script: Save Records as PDF, Send Mail (#133) ─────────

  @override
  Future<List<PdfSheetPage>?> actionRenderRecordsPdf(String records) async {
    if (!await _saveCurrentRecord() || !mounted) return null;
    final List<Map<String, dynamic>> rows;
    switch (records) {
      case 'current':
        rows = _records.isEmpty ? const [] : [Map<String, dynamic>.from(_currentRecord)];
      case 'blank':
        rows = [<String, dynamic>{}];
      default:
        if (!await _readWholeFoundSet() || !mounted) return null;
        rows = List.of(_records);
    }
    if (rows.isEmpty) return const [];
    return _drawOffscreen(rows);
  }

  /// Draws [rows] with the Preview mode sheet, out of sight, and captures
  /// its pages: the window stays in Browse mode, as in FileMaker.
  Future<List<PdfSheetPage>?> _drawOffscreen(List<Map<String, dynamic>> rows) async {
    final overlay = Overlay.maybeOf(context, rootOverlay: true);
    if (overlay == null) return null;
    LayoutPreviewWidgetState? preview;
    final layout = widget.layout ??
        LayoutDefinitionModel.defaultForTable(
          widget.table.displayName,
          widget.table.columns.map((c) => (name: c.name, label: c.displayName)).toList(),
        );
    final entry = OverlayEntry(
      builder: (_) => Positioned(
        // Laid out and painted, so it can be captured, but off the screen.
        left: -20000,
        top: 0,
        width: 1400,
        height: 1800,
        child: IgnorePointer(
          child: Material(
            child: LayoutPreviewWidget(
              table: widget.table,
              apiClient: widget.apiClient,
              layout: layout,
              pageSetup: widget.pageSetup,
              currentUserName: widget.currentUserName,
              findRequests: _foundSetFind ?? const [],
              records: rows,
              onAttach: (state) => preview = state,
            ),
          ),
        ),
      ),
    );
    overlay.insert(entry);
    try {
      for (var i = 0; i < 200 && (preview == null || !preview!.isReady); i++) {
        await Future<void>.delayed(const Duration(milliseconds: 50));
      }
      if (preview == null || !preview!.isReady) return null;
      return await preview!.captureSheets();
    } on Object {
      return null;
    } finally {
      entry.remove();
    }
  }

  @override
  Future<bool> actionForEachFoundRecord(Future<bool> Function(int position, int count) visit) async {
    if (!await _saveCurrentRecord() || !mounted) return false;
    final startId = _records.isNotEmpty ? _records[_currentIndex]['id'] : null;
    if (!await _readWholeFoundSet() || !mounted) return false;
    final count = _records.length;
    try {
      for (var i = 0; i < count; i++) {
        _currentIndex = i;
        if (!await visit(i + 1, count) || !mounted) break;
      }
      return true;
    } finally {
      if (mounted) {
        setState(() {
          final idx = _records.indexWhere((r) => r['id'] == startId);
          _currentIndex = idx < 0 ? 0 : idx;
        });
        _rebuildFieldControllers();
      }
    }
  }

  @override
  Future<OutgoingMail?> actionComposeMail(OutgoingMail mail, {required String from, List<String> attachments = const []}) {
    if (!mounted) return Future.value();
    return MailComposeDialog.show(context, mail: mail, from: from, attachments: attachments);
  }

  @override
  Future<void> actionOpenDocument(String path, String nativePath, Uint8List bytes) async {
    final name = path.substring(path.lastIndexOf('/') + 1);
    try {
      if (kIsWeb) {
        await Printing.sharePdf(bytes: bytes, filename: name);
      } else {
        await launchUrl(Uri.file(nativePath));
      }
    } on Object {
      await actionNotify('Saved $nativePath');
    }
  }

  @override
  Future<void> actionNotify(String message) async {
    if (!mounted) return;
    ScaffoldMessenger.maybeOf(context)?.showSnackBar(
      SnackBar(content: Text(message), duration: const Duration(seconds: 12), showCloseIcon: true),
    );
  }

  @override
  Future<void> actionExportFieldContents({required String field, String? path}) async {
    // No-op in headless/browser without explicit user save dialog.
  }

  @override
  Future<bool> actionInsertFile({required String field, String? path, bool isPicture = false}) async {
    return true;
  }

  @override
  Future<void> actionCopyToClipboard(String text) async {
    try {
      await Clipboard.setData(ClipboardData(text: text));
    } catch (_) {
      // Clipboard may be unavailable in headless tests.
    }
  }

  @override
  Future<String?> actionReadFromClipboard() async {
    try {
      final data = await Clipboard.getData(Clipboard.kTextPlain);
      return data?.text;
    } catch (_) {
      return null;
    }
  }

  @override
  Future<void> actionBeep() async {
    try {
      await SystemSound.play(SystemSoundType.alert);
    } catch (_) {
      // Audio feedback is a documented no-op when unsupported.
    }
  }

  @override
  Future<void> actionRefreshWindow({bool flushJoinCache = false, bool flushExternalCache = false}) async {
    // The found set, its order and the record in hand stay as they are:
    // refreshing redraws what the window shows, it does not find again.
    await _reloadCurrentRecord();
    await actionRefreshPortal('');
  }

  /// Reads the record in hand again from the server and shows it, in place.
  /// A record not yet committed, or one with an edit still being saved, is
  /// left alone so that no typing is lost.
  Future<void> _reloadCurrentRecord() async {
    if (_mode != OperationalMode.browse || _records.isEmpty || _onDraft) return;
    if (_fieldDebounceTimers.values.any((t) => t?.isActive ?? false)) return;
    final index = _currentIndex;
    final id = _records[index]['id']?.toString();
    if (id == null) return;
    Map<String, dynamic> row;
    try {
      row = await widget.apiClient.getRow(widget.table.name, id);
    } on Exception {
      // A record that cannot be read again keeps what it showed.
      return;
    }
    if (!mounted || index >= _records.length || _records[index]['id']?.toString() != id) return;
    setState(() => _records[index] = row);
    for (final col in widget.table.columns) {
      final ctrl = _fieldControllers[col.name];
      if (ctrl != null) ctrl.text = recordFieldText(col, row);
    }
  }

  @override
  Future<bool> actionRefreshPortal(String objectName) async {
    final objects = widget.layout?.objects ?? const <LayoutObjectModel>[];
    final wanted = objectName.trim().toLowerCase();
    final List<LayoutObjectModel> portals;
    if (wanted.isEmpty) {
      portals = objects.where((o) => o.type == 'portal').toList();
    } else {
      final found = objects
          .where((o) => (o.name ?? '').toLowerCase() == wanted || o.id.toLowerCase() == wanted)
          .firstOrNull;
      if (found == null) return false;
      if (found.type != 'portal') {
        // FileMaker refreshes any other named object as Refresh Object does:
        // what it shows is read again. The found set is not touched.
        await _reloadCurrentRecord();
        return true;
      }
      portals = [found];
    }
    if (mounted && portals.isNotEmpty) {
      setState(() {
        for (final portal in portals) {
          _portalRefreshes[portal.id] = (_portalRefreshes[portal.id] ?? 0) + 1;
        }
      });
    }
    return true;
  }

  static String _occurrenceKey(String raw) =>
      raw.trim().toLowerCase().replaceAll(RegExp('[^a-z0-9]+'), '_').replaceAll(RegExp(r'^_+|_+$'), '');

  /// Whether [table], a table occurrence a script step names, is another
  /// occurrence than the one this layout shows.
  ///
  /// With the relationship graph read, any name but the layout's own
  /// occurrence (or its base table) is another one. Without it, only a field
  /// the layout shows through a relationship from that occurrence says so,
  /// and a name the window cannot place is taken as the layout's own, as
  /// FileMaker requires the target to be on the layout.
  bool _namesOtherOccurrence(String field, String? table) {
    if (table == null || table.trim().isEmpty) return false;
    final key = _occurrenceKey(table);
    final graph = _occurrences;
    if (_isOwnOccurrence(table)) return false;
    if (graph.isNotEmpty) return true;
    final objects = widget.layout?.objects ?? const <LayoutObjectModel>[];
    return objects.any((o) {
      final binding = o.fieldBinding;
      if (binding == null || !binding.readsRelated) return false;
      if (binding.fieldName.toLowerCase() != field.toLowerCase()) return false;
      final occurrence = binding.tableOccurrence ?? '';
      return _occurrenceKey(occurrence) == key || _occurrenceKey(graph[occurrence]?.name ?? '') == key;
    });
  }

  @override
  String? actionFieldType(String field, {String? table}) {
    if (_namesOtherOccurrence(field, table)) {
      // A related field's type is its own table's.
      final occurrence = _occurrenceNamed(table!);
      final other = occurrence == null || occurrence.external != null ? null : _tablesById[occurrence.baseTableId];
      final name = field.trim().toLowerCase();
      return (other?.columns.where((c) => c.name.toLowerCase() == name).firstOrNull ??
              other?.columns.where((c) => c.displayName.toLowerCase() == name).firstOrNull)
          ?.fieldType;
    }
    return widget.table.columns.where((c) => c.name.toLowerCase() == field.toLowerCase()).firstOrNull?.fieldType;
  }

  @override
  Future<FieldInsertOutcome> actionInsertIntoField({
    required String field,
    required String text,
    required bool replaceAll,
    String? table,
  }) async {
    // A step that names a table occurrence is placed against the graph,
    // read once if this layout never needed it.
    if (table != null && table.trim().isNotEmpty && _occurrences.isEmpty) await _loadRelationshipGraph();
    if (!mounted) return FieldInsertOutcome.noSuchField;
    if (_namesOtherOccurrence(field, table)) return FieldInsertOutcome.relatedField;
    final col = widget.table.columns.where((c) => c.name.toLowerCase() == field.toLowerCase()).firstOrNull;
    if (col == null || col.isPrimaryKey) return FieldInsertOutcome.noSuchField;

    // The new content, and the caret after the inserted text, as typing
    // leaves it.
    TextEditingValue insert(String existing, TextSelection? selection, {required bool active}) {
      if (replaceAll) {
        return TextEditingValue(text: text, selection: TextSelection.collapsed(offset: text.length));
      }
      // The selection is replaced; a caret is a selection of nothing. A field
      // that is not the active one gets the text at its end.
      final sel = selection;
      final range = active && sel != null && sel.isValid && sel.end <= existing.length
          ? sel
          : TextSelection.collapsed(offset: existing.length);
      return TextEditingValue(
        text: existing.replaceRange(range.start, range.end, text),
        selection: TextSelection.collapsed(offset: range.start + text.length),
      );
    }

    if (_mode == OperationalMode.find) {
      // A find criterion, never the record behind the request.
      final ctrl = _findControllers[col.name];
      final existing = ctrl?.text ?? _currentFindRequest.values[col.name] ?? '';
      final value = insert(existing, ctrl?.selection, active: _lastFocusedFindField == col.name);
      ctrl?.value = value;
      setState(() => _currentFindRequest.values[col.name] = value.text);
      return FieldInsertOutcome.written;
    }

    if (_records.isEmpty) return FieldInsertOutcome.written;
    final ctrl = _fieldControllers[col.name];
    final existing = ctrl?.text ?? recordFieldText(col, _records[_currentIndex]);
    final value = insert(existing, ctrl?.selection, active: _lastFocusedBrowseField == col.name);
    _fieldDebounceTimers[col.name]?.cancel();
    ctrl?.value = value;
    await _saveField(col.name, value.text, script: true);
    return FieldInsertOutcome.written;
  }

  @override
  Future<void> actionSendMail({
    String to = '',
    String cc = '',
    String bcc = '',
    String subject = '',
    String message = '',
  }) async {
    if (to.isEmpty) return;
    final uri = Uri(
      scheme: 'mailto',
      path: to,
      queryParameters: <String, String>{
        if (cc.isNotEmpty) 'cc': cc,
        if (bcc.isNotEmpty) 'bcc': bcc,
        if (subject.isNotEmpty) 'subject': subject,
        if (message.isNotEmpty) 'body': message,
      },
    );
    await actionOpenUrl(uri.toString());
  }

  @override
  Future<void> actionSetUiVisibility(String element, String state) async {
    // Tracked by the runner's window/session state.
  }

  @override
  Future<String?> actionInsertFromUrl({
    required String url,
    bool verifySsl = true,
    String? curlOptions,
  }) async {
    final uri = Uri.tryParse(url);
    if (uri == null || !(uri.isScheme('http') || uri.isScheme('https'))) {
      return null;
    }
    final resp = await http.get(uri);
    if (resp.statusCode >= 400) return null;
    return resp.body;
  }

  void _handleLegacyButtonLabel(String label) {
    final lower = label.toLowerCase();
    if (lower.contains('new') || lower.contains('nuevo')) {
      createNewRecord();
    } else if (lower.contains('delete') ||
        lower.contains('borrar') ||
        lower.contains('eliminar')) {
      deleteCurrentRecord();
    } else if (lower.contains('first') || lower.contains('primero')) {
      unawaited(goToRecord(0));
    } else if (lower.contains('last') ||
        lower.contains('ultimo') ||
        lower.contains('último')) {
      unawaited(goToRecord(_records.length - 1));
    } else if (lower.contains('next') || lower.contains('siguiente')) {
      nextRecord();
    } else if (lower.contains('prev') || lower.contains('anterior')) {
      previousRecord();
    } else if (lower.contains('find') || lower.contains('buscar')) {
      unawaited(_requestMode(OperationalMode.find));
    } else if (lower.contains('print') ||
        lower.contains('imprimir') ||
        lower.contains('preview')) {
      unawaited(_requestMode(OperationalMode.preview));
    } else if (lower.contains('save') || lower.contains('guardar')) {
      unawaited(_saveCurrentRecord());
    } else if (lower.contains('home') ||
        lower.contains('inicio') ||
        lower.contains('volver') ||
        lower.contains('regresar')) {
      widget.onGoHome?.call();
    } else if (lower.contains('re-login') ||
        lower.contains('relogin') ||
        lower.contains('switch user') ||
        lower.contains('cambiar usuario')) {
      widget.onReLogin?.call();
    } else if (lower.contains('exit') ||
        lower.contains('salir') ||
        lower.contains('close') ||
        lower.contains('cerrar') ||
        lower.contains('logout')) {
      (widget.onExitApplication ?? widget.onCloseFile)?.call();
    } else {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('Button clicked: "$label"'),
          duration: const Duration(seconds: 1),
        ),
      );
    }
  }

  TextAlign _parseTextAlign(String align) {
    switch (align) {
      case 'center':
        return TextAlign.center;
      case 'right':
        return TextAlign.right;
      default:
        return TextAlign.left;
    }
  }

  Alignment _parseAlignment(String align) {
    switch (align) {
      case 'center':
        return Alignment.center;
      case 'right':
        return Alignment.centerRight;
      default:
        return Alignment.centerLeft;
    }
  }

  Color _parseColor(String colorStr, [Color fallback = Colors.black87]) {
    try {
      if (colorStr.startsWith('#')) {
        final hex = colorStr.substring(1);
        if (hex.length == 6) return Color(int.parse('0xFF$hex'));
        if (hex.length == 8) return Color(int.parse('0x$hex'));
      }
    } catch (_) {}
    return fallback;
  }

  List<Widget> _buildPartDividers(
      LayoutDefinitionModel layout, double width, bool isDark) {
    final widgets = <Widget>[];
    double currentY = 0;
    for (final part in layout.parts) {
      currentY += part.height;
      widgets.add(
        Positioned(
          top: currentY,
          left: 0,
          right: 0,
          child: Container(
            height: 1,
            color: isDark
                ? Colors.white.withValues(alpha: 0.08)
                : Colors.black.withValues(alpha: 0.06),
          ),
        ),
      );
    }
    return widgets;
  }

  Widget _buildStandardCardContent() {
    final record = _records[_currentIndex];

    return Padding(
      padding: const EdgeInsets.all(24),
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 720),
          child: Card(
            elevation: 1,
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  // Header with record info and live status
                  Row(
                    children: [
                      Icon(Icons.badge_outlined, size: 20, color: Theme.of(context).colorScheme.primary),
                      const SizedBox(width: 8),
                      Text(
                        '${widget.table.displayName} • Record ${_currentIndex + 1} of ${_records.length}',
                        style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 15),
                      ),
                      const Spacer(),
                      // Status pill
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                        decoration: BoxDecoration(
                          color: Colors.green.withValues(alpha: 0.12),
                          borderRadius: BorderRadius.circular(4),
                          border: Border.all(color: Colors.green.shade600, width: 0.8),
                        ),
                        child: const Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Icon(Icons.sync, size: 12, color: Colors.green),
                            SizedBox(width: 4),
                            Text('Auto-saved to DB', style: TextStyle(fontSize: 10, fontWeight: FontWeight.bold, color: Colors.green)),
                          ],
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 16),
                  const Divider(),
                  const SizedBox(height: 16),
                  // Column fields
                  ...widget.table.columns.map((col) {
                    if (col.isPrimaryKey) {
                      return Padding(
                        padding: const EdgeInsets.only(bottom: 16),
                        child: Row(
                          children: [
                            SizedBox(
                              width: 160,
                              child: Text(col.displayName, style: const TextStyle(fontWeight: FontWeight.bold)),
                            ),
                            Expanded(
                              child: TextFormField(
                                initialValue: fieldText(col, record[col.name]),
                                readOnly: true,
                                decoration: const InputDecoration(
                                  border: OutlineInputBorder(),
                                  isDense: true,
                                  suffixIcon: Tooltip(
                                    message: 'Primary Key',
                                    child: Icon(Icons.key, size: 16),
                                  ),
                                ),
                              ),
                            ),
                          ],
                        ),
                      );
                    }

                    final ctrl = _fieldControllers[col.name];
                    final fn = _fieldFocusNodes[col.name];
                    final saving = _fieldSaving[col.name];

                    return Padding(
                      padding: const EdgeInsets.only(bottom: 16),
                      child: Row(
                        children: [
                          SizedBox(
                            width: 160,
                            child: Row(
                              children: [
                                Expanded(
                                  child: Text(
                                    col.displayName,
                                    style: const TextStyle(fontWeight: FontWeight.bold),
                                    overflow: TextOverflow.ellipsis,
                                  ),
                                ),
                              ],
                            ),
                          ),
                          Expanded(
                            child: TextField(
                              controller: ctrl,
                              focusNode: fn,
                              readOnly: _isEntryLocked(col.name),
                              decoration: InputDecoration(
                                border: const OutlineInputBorder(),
                                isDense: true,
                                suffixIcon: saving == true
                                    ? const Padding(
                                        padding: EdgeInsets.all(10),
                                        child: SizedBox(
                                          width: 14,
                                          height: 14,
                                          child: CircularProgressIndicator(strokeWidth: 1.5),
                                        ),
                                      )
                                    : saving == false
                                        ? const Tooltip(
                                            message: 'Save error — check connection',
                                            child: Icon(Icons.error_outline, size: 16, color: Colors.red),
                                          )
                                        : null,
                              ),
                              onChanged: (v) => _onFieldChanged(col.name, v),
                              onEditingComplete: () {
                                _fieldDebounceTimers[col.name]?.cancel();
                                unawaited(_saveField(col.name, ctrl?.text ?? ''));
                                fn?.nextFocus();
                              },
                            ),
                          ),
                        ],
                      ),
                    );
                  }),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildFindModeForm() {
    if (_viewMode == 'form' && widget.layout != null && widget.layout!.objects.isNotEmpty) {
      return _buildLayoutCanvasView(widget.layout!, isFindMode: true);
    }
    return _buildStandardFindModeForm();
  }

  Widget _buildStandardFindModeForm() {
    final isDark = Theme.of(context).brightness == Brightness.dark;

    return SingleChildScrollView(
      padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 20),
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 780),
          child: Card(
            elevation: 1.5,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(12),
              side: BorderSide(
                color: Theme.of(context).colorScheme.outlineVariant.withValues(alpha: 0.6),
              ),
            ),
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  // Find Mode Header Banner
                  Container(
                    padding: const EdgeInsets.all(16),
                    decoration: BoxDecoration(
                      color: Theme.of(context).colorScheme.primaryContainer.withValues(alpha: isDark ? 0.3 : 0.4),
                      borderRadius: BorderRadius.circular(8),
                      border: Border.all(
                        color: Theme.of(context).colorScheme.primary.withValues(alpha: 0.2),
                      ),
                    ),
                    child: Row(
                      children: [
                        Container(
                          padding: const EdgeInsets.all(10),
                          decoration: BoxDecoration(
                            color: Theme.of(context).colorScheme.primary.withValues(alpha: 0.15),
                            shape: BoxShape.circle,
                          ),
                          child: Icon(
                            Icons.manage_search_rounded,
                            size: 24,
                            color: Theme.of(context).colorScheme.primary,
                          ),
                        ),
                        const SizedBox(width: 14),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                'Find Mode — ${widget.table.displayName}',
                                style: TextStyle(
                                  fontWeight: FontWeight.bold,
                                  fontSize: 16,
                                  color: Theme.of(context).colorScheme.onSurface,
                                ),
                              ),
                              const SizedBox(height: 3),
                              Text(
                                'Enter values or formulas in any field to search. Records matching all criteria will be displayed in Browse Mode.',
                                style: TextStyle(
                                  fontSize: 12,
                                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                                ),
                              ),
                            ],
                          ),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 16),

                  // Quick Operator Helper Chips
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'Quick Operators & Formulas:',
                        style: TextStyle(
                          fontSize: 11,
                          fontWeight: FontWeight.w600,
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                      ),
                      const SizedBox(height: 6),
                      Wrap(
                        spacing: 6,
                        runSpacing: 6,
                        children: [
                          _buildQuickOpChip('*', 'Wildcard', '*'),
                          _buildQuickOpChip('...', 'Range', '...'),
                          _buildQuickOpChip('=', 'Exact', '='),
                          _buildQuickOpChip('==', 'Strict', '=='),
                          _buildQuickOpChip('>', 'Greater', '>'),
                          _buildQuickOpChip('<', 'Less', '<'),
                          _buildQuickOpChip('//', 'Today', '//'),
                          _buildQuickOpChip('!', 'Not Equal', '!'),
                          _buildQuickOpChip('= (empty)', 'Empty Field', '='),
                          _buildQuickOpChip('* (non-empty)', 'Non-Empty', '*'),
                        ],
                      ),
                    ],
                  ),
                  const SizedBox(height: 20),
                  const Divider(),
                  const SizedBox(height: 16),

                  // Field Inputs
                  ...widget.table.columns.map((col) {
                    final ctrl = _findControllers[col.name];

                    return Padding(
                      padding: const EdgeInsets.only(bottom: 16),
                      child: Row(
                        children: [
                          SizedBox(
                            width: 170,
                            child: Row(
                              children: [
                                Expanded(
                                  child: Text(
                                    col.displayName,
                                    style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13),
                                    overflow: TextOverflow.ellipsis,
                                  ),
                                ),
                                Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 2),
                                  decoration: BoxDecoration(
                                    color: Theme.of(context).colorScheme.surfaceContainerHighest,
                                    borderRadius: BorderRadius.circular(4),
                                  ),
                                  child: Text(
                                    col.fieldType,
                                    style: TextStyle(
                                      fontSize: 9,
                                      fontWeight: FontWeight.bold,
                                      color: Theme.of(context).colorScheme.outline,
                                    ),
                                  ),
                                ),
                                const SizedBox(width: 8),
                              ],
                            ),
                          ),
                          Expanded(
                            child: Focus(
                              onFocusChange: (hasF) {
                                if (hasF) _lastFocusedFindField = col.name;
                              },
                              child: TextField(
                                controller: ctrl,
                                decoration: InputDecoration(
                                  border: const OutlineInputBorder(),
                                  isDense: true,
                                  prefixIcon: Icon(
                                    Icons.search,
                                    size: 16,
                                    color: Theme.of(context).colorScheme.primary,
                                  ),
                                  hintText: _findHintForType(col.fieldType),
                                  suffixIcon: Row(
                                    mainAxisSize: MainAxisSize.min,
                                    children: [
                                      _buildOperatorsMenu(targetField: col.name),
                                      if (ctrl != null && ctrl.text.isNotEmpty)
                                        IconButton(
                                          icon: const Icon(Icons.clear, size: 16),
                                          tooltip: 'Clear field',
                                          onPressed: () {
                                            ctrl.clear();
                                            setState(() {});
                                          },
                                        ),
                                    ],
                                  ),
                                ),
                                onSubmitted: (_) => _performFind(),
                              ),
                            ),
                          ),
                        ],
                      ),
                    );
                  }),

                  const SizedBox(height: 8),
                  const Divider(),
                  const SizedBox(height: 16),

                  // Bottom Action Buttons
                  Row(
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: [
                      OutlinedButton.icon(
                        icon: const Icon(Icons.close, size: 16),
                        label: const Text('Cancel Find'),
                        onPressed: () {
                          deleteAllFindRequests();
                          unawaited(_requestMode(OperationalMode.browse));
                          unawaited(_fetchRecords());
                        },
                      ),
                      const SizedBox(width: 10),
                      OutlinedButton.icon(
                        icon: const Icon(Icons.clear_all, size: 16),
                        label: const Text('Clear Criteria'),
                        onPressed: () {
                          for (final c in _findControllers.values) {
                            c.clear();
                          }
                          _captureCurrentFindRequest();
                          setState(() {});
                        },
                      ),
                      const SizedBox(width: 10),
                      FilledButton.icon(
                        icon: const Icon(Icons.search, size: 18),
                        label: const Text('Perform Find (Enter)'),
                        onPressed: _performFind,
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildQuickOpChip(String label, String tooltip, String insertText) {
    return ActionChip(
      visualDensity: VisualDensity.compact,
      label: Text(label, style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold)),
      tooltip: tooltip,
      onPressed: () {
        final field = _lastFocusedFindField ?? (widget.table.columns.isNotEmpty ? widget.table.columns.first.name : null);
        if (field != null) {
          final ctrl = _findControllers[field];
          if (ctrl != null) {
            final cur = ctrl.text;
            ctrl.text = '$cur$insertText';
            ctrl.selection = TextSelection.fromPosition(TextPosition(offset: ctrl.text.length));
          }
        }
      },
    );
  }

  String _findHintForType(String fieldType) {
    switch (fieldType) {
      case 'NUMBER':
        return 'e.g. >100, 10...50, 0, = (empty)';
      case 'DATE':
      case 'TIMESTAMP':
        return 'e.g. 2026-09-24, 2024-01-01...2024-12-31, // (today)';
      default:
        return 'e.g. Mario*, =Exact, !=Excluded, * (non-empty)';
    }
  }
}

/// Record number box of the Browse record bar: type a number and press Enter
/// to jump to that record.
class _RecordNumberField extends StatelessWidget {

  const _RecordNumberField({
    required this.controller,
    required this.focusNode,
    required this.enabled,
    required this.onSubmitted,
  });
  final TextEditingController controller;
  final FocusNode focusNode;
  final bool enabled;
  final ValueChanged<String> onSubmitted;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 56,
      height: 30,
      child: TextField(
        controller: controller,
        focusNode: focusNode,
        enabled: enabled,
        textAlign: TextAlign.center,
        keyboardType: TextInputType.number,
        inputFormatters: [FilteringTextInputFormatter.digitsOnly],
        style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
        decoration: const InputDecoration(
          isDense: true,
          contentPadding: EdgeInsets.symmetric(horizontal: 4, vertical: 6),
          border: OutlineInputBorder(),
        ),
        onSubmitted: onSubmitted,
      ),
    );
  }
}
