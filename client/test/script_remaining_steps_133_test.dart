// #133 — remaining FileMaker script steps, options, error capture, loop exit,
// dynamic targets, window model, and runner/debugger parity.

import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/script_workspace/script_debugger.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

class _FullExtendedHost implements LayoutActionHost, ExtendedLayoutActionHost {
  _FullExtendedHost({
    List<Map<String, String>>? records,
    Set<String>? knownLayouts,
    Set<String>? knownObjects,
  })  : records = records ?? <Map<String, String>>[{'name': 'Rec1', 'status': ''}],
        knownLayouts = knownLayouts ?? <String>{'MainLayout', 'DetailLayout', 'ReportLayout'},
        knownObjects = knownObjects ?? <String>{'ObjTitle'};

  final List<String> log = <String>[];
  String currentLayout = 'MainLayout';
  final Set<String> knownLayouts;
  final Set<String> knownObjects;
  final List<Map<String, String>> records;
  final List<Map<String, String>> omitted = <Map<String, String>>[];
  int recordIdx = 0;
  int portalIdx = 0;
  int portalCount = 2;
  String? focusedField;
  String clipboard = '';
  int nextDialogButton = 1;
  Map<String, String> nextDialogInputs = const <String, String>{};
  int? nextFindCount;
  String? nextUrlBody;

  Map<String, String> get currentRecord => records.isEmpty ? <String, String>{} : records[recordIdx];

  @override
  String? get actionActiveFieldName => focusedField;

  @override
  int get actionFoundCount => records.length;

  @override
  int get actionCurrentRecordNumber => records.isEmpty ? 0 : recordIdx + 1;

  @override
  int get actionTotalRecordCount => records.length + omitted.length;

  @override
  String actionResolveText(String text) {
    final trimmed = text.trim();
    if (trimmed == '{{LayoutName}}') return currentLayout;
    final m = RegExp(r'^\{\{(.+)\}\}$').firstMatch(trimmed);
    if (m != null) {
      final key = m.group(1)!;
      if (currentRecord.containsKey(key)) return currentRecord[key]!;
      return text;
    }
    return text;
  }

  @override
  Future<void> actionNewRecord() async => log.add('new_record');
  @override
  Future<void> actionDuplicateRecord() async => log.add('duplicate_record');
  @override
  Future<void> actionDeleteRecord({required bool confirm}) async => log.add('delete_record:$confirm');
  @override
  Future<void> actionCommitRecord() async => log.add('commit_records');
  @override
  Future<void> actionCommitRecordEx({bool dialog = true, bool skipValidation = false, bool forceCommit = false}) async {
    log.add('commit_records:dialog=$dialog,skip=$skipValidation,force=$forceCommit');
  }
  @override
  Future<void> actionRevertRecord() async => log.add('revert_record');
  @override
  Future<void> actionGoToRecord(String target) async => actionGoToRecordEx(target);

  @override
  Future<GoToRecordOutcome> actionGoToRecordEx(String target) async {
    log.add('go_to_record:$target');
    if (records.isEmpty) return const GoToRecordOutcome(moved: false, atBoundary: true, errorCode: 101);
    switch (target.toLowerCase()) {
      case 'first':
        recordIdx = 0;
        return const GoToRecordOutcome(moved: true);
      case 'last':
        recordIdx = records.length - 1;
        return const GoToRecordOutcome(moved: true);
      case 'next':
        if (recordIdx + 1 >= records.length) {
          return const GoToRecordOutcome(moved: false, atBoundary: true, errorCode: 101);
        }
        recordIdx++;
        return const GoToRecordOutcome(moved: true);
      case 'previous':
        if (recordIdx - 1 < 0) {
          return const GoToRecordOutcome(moved: false, atBoundary: true, errorCode: 101);
        }
        recordIdx--;
        return const GoToRecordOutcome(moved: true);
      default:
        final n = int.tryParse(target);
        if (n == null || n < 1 || n > records.length) {
          return const GoToRecordOutcome(moved: false, atBoundary: true, errorCode: 101);
        }
        recordIdx = n - 1;
        return const GoToRecordOutcome(moved: true);
    }
  }

  @override
  Future<void> actionEnterFindMode() async => log.add('enter_find_mode');
  @override
  Future<void> actionEnterFindModeEx({List<Map<String, dynamic>>? requests, bool restore = false}) async {
    log.add('enter_find_mode:restore=$restore,reqs=${requests?.length ?? 0}');
  }

  @override
  Future<void> actionPerformFind() async => log.add('perform_find');
  @override
  Future<int> actionPerformFindEx({List<Map<String, dynamic>>? requests, bool restore = false}) async {
    log.add('perform_find:restore=$restore,reqs=${requests?.length ?? 0}');
    return nextFindCount ?? records.length;
  }

  @override
  Future<void> actionShowAllRecords() async => log.add('show_all_records');
  @override
  Future<void> actionEnterPreviewMode() async => log.add('enter_preview_mode');
  @override
  Future<void> actionEnterBrowseMode() async => log.add('enter_browse_mode');

  @override
  Future<bool> actionGoToLayout(String layoutName) async {
    if (!knownLayouts.contains(layoutName)) return false;
    currentLayout = layoutName;
    log.add('go_to_layout:$layoutName');
    return true;
  }

  @override
  Future<bool> actionGoToLayoutByNumber(int layoutNumber) async {
    final list = knownLayouts.toList();
    if (layoutNumber < 1 || layoutNumber > list.length) return false;
    return actionGoToLayout(list[layoutNumber - 1]);
  }

  @override
  Future<bool> actionSetField(String field, String value) async {
    if (records.isEmpty || !currentRecord.containsKey(field)) return false;
    currentRecord[field] = value;
    log.add('set_field:$field=$value');
    return true;
  }

  @override
  Future<void> actionShowDialog(String title, String message) async => log.add('show_dialog:$title|$message');

  @override
  Future<ScriptDialogResult> actionShowDialogEx({
    required String title,
    required String message,
    List<Map<String, dynamic>>? buttons,
    List<Map<String, dynamic>>? inputFields,
  }) async {
    log.add('show_dialog_ex:$title|buttons=${buttons?.length ?? 0}|inputs=${inputFields?.length ?? 0}');
    return ScriptDialogResult(buttonIndex: nextDialogButton, inputValues: nextDialogInputs);
  }

  @override
  Future<void> actionOpenUrl(String url) async => log.add('open_url:$url');
  @override
  Future<void> actionAdjustWindow(String windowAction) async => log.add('adjust_window:$windowAction');
  @override
  Future<void> actionReLogin() async => log.add('re_login');
  @override
  Future<void> actionCloseFile() async => log.add('close_file');
  @override
  Future<void> actionExitApplication() async => log.add('exit_application');

  @override
  Future<void> actionSortRecords({List<Map<String, dynamic>>? sortOrder, bool restore = false, bool dialog = true}) async {
    log.add('sort_records:restore=$restore,levels=${sortOrder?.length ?? 0},dialog=$dialog');
  }

  @override
  Future<void> actionUnsortRecords() async => log.add('unsort_records');

  @override
  Future<bool> actionOmitRecord() async {
    if (records.isEmpty) return false;
    omitted.add(records.removeAt(recordIdx));
    if (recordIdx >= records.length && records.isNotEmpty) recordIdx = records.length - 1;
    log.add('omit_record');
    return true;
  }

  @override
  Future<void> actionOmitMultipleRecords(int count) async {
    for (var i = 0; i < count && records.isNotEmpty; i++) {
      await actionOmitRecord();
    }
  }

  @override
  Future<void> actionShowOmittedOnly() async {
    final prev = List<Map<String, String>>.from(records);
    records
      ..clear()
      ..addAll(omitted);
    omitted
      ..clear()
      ..addAll(prev);
    recordIdx = 0;
    log.add('show_omitted_only');
  }

  @override
  Future<void> actionDeleteAllRecords({required bool confirm}) async {
    records.clear();
    log.add('delete_all_records:$confirm');
  }

  @override
  Future<bool> actionGoToField(String field, {bool select = false}) async {
    if (records.isEmpty || !currentRecord.containsKey(field)) return false;
    focusedField = field;
    log.add('go_to_field:$field:select=$select');
    return true;
  }

  @override
  Future<void> actionGoToNextField() async {
    focusedField = 'status';
    log.add('go_to_next_field');
  }

  @override
  Future<bool> actionGoToObject(String objectName) async {
    if (!knownObjects.contains(objectName)) return false;
    log.add('go_to_object:$objectName');
    return true;
  }

  @override
  Future<bool> actionGoToRelatedRecord({
    required String relatedTable,
    String? layoutName,
    bool showOnlyRelated = true,
    bool newWindow = false,
  }) async {
    log.add('gtrr:$relatedTable|layout=$layoutName|only=$showOnlyRelated|newWin=$newWindow');
    if (layoutName != null && layoutName.isNotEmpty) {
      return actionGoToLayout(layoutName);
    }
    return true;
  }

  @override
  Future<GoToRecordOutcome> actionGoToPortalRow(String target) async {
    log.add('go_to_portal_row:$target');
    if (target.toLowerCase() == 'first') {
      portalIdx = 0;
      return const GoToRecordOutcome(moved: true);
    }
    if (target.toLowerCase() == 'next') {
      if (portalIdx + 1 >= portalCount) {
        return const GoToRecordOutcome(moved: false, atBoundary: true, errorCode: 101);
      }
      portalIdx++;
      return const GoToRecordOutcome(moved: true);
    }
    return const GoToRecordOutcome(moved: true);
  }

  @override
  Future<void> actionDeletePortalRow({required bool confirm}) async => log.add('delete_portal_row:$confirm');

  @override
  Future<bool> actionReplaceFieldContents({required String field, required String replaceType, String? value}) async {
    for (final rec in records) {
      if (!rec.containsKey(field)) return false;
      rec[field] = value ?? '';
    }
    log.add('replace_field_contents:$field=$value');
    return true;
  }

  @override
  Future<void> actionPrint({bool dialog = true, Map<String, dynamic>? printSettings}) async {
    log.add('print:dialog=$dialog');
  }

  @override
  Future<void> actionSaveRecordsAsPdf({String? path, bool dialog = true, bool append = false}) async {
    log.add('save_pdf:$path:append=$append');
  }

  @override
  Future<void> actionExportFieldContents({required String field, String? path}) async {
    log.add('export_field:$field:$path');
  }

  @override
  Future<bool> actionInsertFile({required String field, String? path, bool isPicture = false}) async {
    log.add('${isPicture ? 'insert_picture' : 'insert_file'}:$field:$path');
    return true;
  }

  @override
  Future<void> actionCopyToClipboard(String text) async {
    clipboard = text;
    log.add('copy:$text');
  }

  @override
  Future<String?> actionReadFromClipboard() async => clipboard;

  @override
  Future<void> actionBeep() async => log.add('beep');

  @override
  Future<void> actionRefreshWindow({bool flushJoinCache = false, bool flushExternalCache = false}) async {
    log.add('refresh_window:join=$flushJoinCache,ext=$flushExternalCache');
  }

  /// Portals on the layout; [knownObjects] that are not portals are other
  /// objects.
  Set<String> knownPortals = <String>{'Lines'};

  /// The type of each field, by name; a field not listed is text.
  Map<String, String> fieldTypes = <String, String>{};

  @override
  Future<bool> actionRefreshPortal(String objectName) async {
    if (objectName.isNotEmpty && !knownPortals.contains(objectName) && !knownObjects.contains(objectName)) {
      return false;
    }
    log.add('refresh_portal:$objectName');
    return true;
  }

  @override
  String? actionFieldType(String field) =>
      currentRecord.containsKey(field) ? (fieldTypes[field] ?? 'TEXT') : null;

  @override
  Future<void> actionSendMail({String to = '', String cc = '', String bcc = '', String subject = '', String message = ''}) async {
    log.add('send_mail:$to|$subject|$message');
  }

  @override
  Future<void> actionSetUiVisibility(String element, String state) async {
    log.add('ui:$element=$state');
  }

  @override
  Future<String?> actionInsertFromUrl({required String url, bool verifySsl = true, String? curlOptions}) async {
    log.add('insert_from_url:$url');
    return nextUrlBody;
  }
}

Map<String, dynamic> _step(int seq, String type, [Map<String, dynamic> params = const <String, dynamic>{}]) => {
      'id': 'st$seq',
      'sequence_idx': seq,
      'step_type': type,
      'params': params,
      'is_enabled': true,
    };

LayoutActionRunner _runner(_FullExtendedHost host, Map<String, Map<String, dynamic>> scripts) {
  final mock = MockClient((req) async {
    if (req.url.path == '/api/v1/schemas/scripts') {
      return http.Response(jsonEncode(scripts.values.toList()), 200, headers: {'content-type': 'application/json'});
    }
    final script = scripts[req.url.path.split('/').last];
    if (script == null) return http.Response('Not found', 404);
    return http.Response(jsonEncode(script), 200, headers: {'content-type': 'application/json'});
  });
  return LayoutActionRunner(
    apiClient: ApiClient(baseUrl: 'http://test:8080', httpClient: mock),
    host: host,
  );
}

void main() {
  test('Set Error Capture + Get(LastError) drive control flow (#133)', () async {
    final host = _FullExtendedHost()..nextFindCount = 0;
    final runner = _runner(host, {
      's1': {
        'id': 's1',
        'name': 'ErrorFlow',
        'is_active': true,
        'steps': [
          _step(1, 'Set Error Capture', {'state': true}),
          _step(2, 'Perform Find', {'restore': true, 'find_requests': [{'action': 'find', 'criteria': [{'field': 'status', 'value': 'missing'}]}]}),
          _step(3, 'If', {'condition': 'Get(LastError) = 401'}),
          _step(4, 'Set Field', {'field': 'status', 'value': 'not_found'}),
          _step(5, 'End If'),
          _step(6, 'Go to Layout', {'layout_name': 'NonExistentLayout'}),
          _step(7, 'Set Variable', {'name': r'$err', 'value': 'Get(LastError)'}),
          _step(8, 'If', {'condition': r'$err = 105'}),
          _step(9, 'Set Field', {'field': 'name', 'value': 'captured_105'}),
          _step(10, 'End If'),
        ],
      },
    });

    final result = await runner.run(const ButtonActionModel.performScript(id: 's1', name: 'ErrorFlow'));
    expect(result.completed, isTrue, reason: result.message);
    expect(host.records[0]['status'], 'not_found');
    expect(host.records[0]['name'], 'captured_105');
  });

  test('Go to Record [Next; Exit after last: On] terminates loop identically in runner and debugger (#133)', () async {
    List<Map<String, dynamic>> buildSteps() => [
          _step(1, 'Go to Record/Request/Page', {'target': 'first'}),
          _step(2, 'Set Variable', {'name': r'$i', 'value': '0'}),
          _step(3, 'Loop'),
          _step(4, 'Set Variable', {'name': r'$i', 'value': r'$i + 1'}),
          _step(5, 'Set Field', {'field': 'status', 'value': r'"done_" & $i', 'value_is_calculation': true}),
          _step(6, 'Go to Record/Request/Page', {'target': 'next', 'exit_after_last': true}),
          _step(7, 'End Loop'),
          _step(8, 'Set Variable', {'name': r'$$loopDone', 'value': r'$i'}),
        ];

    // 1. Normal runner
    final runHost = _FullExtendedHost(records: [
      {'name': 'A', 'status': ''},
      {'name': 'B', 'status': ''},
      {'name': 'C', 'status': ''},
    ]);
    final runner = _runner(runHost, {
      'loop1': {'id': 'loop1', 'name': 'LoopAll', 'is_active': true, 'steps': buildSteps()},
    });
    final res = await runner.run(const ButtonActionModel.performScript(id: 'loop1', name: 'LoopAll'));
    expect(res.completed, isTrue, reason: res.message);
    expect(runHost.records.map((r) => r['status']).toList(), ['done_1', 'done_2', 'done_3']);
    expect(runner.globalVariables[r'$$loopdone'], '3');

    // 2. Debugger single-stepping must produce identical results
    final dbgHost = _FullExtendedHost(records: [
      {'name': 'A', 'status': ''},
      {'name': 'B', 'status': ''},
      {'name': 'C', 'status': ''},
    ]);
    final dbgRunner = _runner(dbgHost, const {});
    final scriptModel = ScriptModel.fromJson({
      'id': 'loop1',
      'name': 'LoopAll',
      'is_active': true,
      'steps': buildSteps(),
    });
    final session = ScriptDebugSession(script: scriptModel, runner: dbgRunner)..begin();
    var guard = 0;
    while (!session.finished && guard < 100) {
      await session.step();
      guard++;
    }
    expect(session.finished, isTrue);
    expect(dbgHost.records.map((r) => r['status']).toList(), ['done_1', 'done_2', 'done_3']);
    expect(dbgRunner.globalVariables[r'$$loopdone'], '3');
  });

  test('Go to Layout destinations, Set Variable repetitions, Set Field By Name, and Window model (#133)', () async {
    final host = _FullExtendedHost();
    final runner = _runner(host, {
      's1': {
        'id': 's1',
        'name': 'Dynamic',
        'is_active': true,
        'steps': [
          _step(1, 'Set Variable', {'name': r'$targets', 'value': '"DetailLayout"', 'repetition': '1'}),
          _step(2, 'Set Variable', {'name': r'$targets', 'value': '"ReportLayout"', 'repetition': '2'}),
          _step(3, 'Go to Layout', {'layout_destination': 'by_name', 'layout_calculation': r'$targets[2]'}),
          _step(4, 'Go to Layout', {'layout_destination': 'original'}),
          _step(5, 'New Window', {'window_name': 'Inspector', 'layout_name': 'DetailLayout'}),
          _step(6, 'Set Variable', {'name': r'$win', 'value': 'Get(WindowName)'}),
          _step(7, 'Set Field By Name', {'target_field': '"Visits::" & "status"', 'target_field_is_calculation': true, 'value': r'$win', 'value_is_calculation': true}),
          _step(8, 'Select Window', {'window_name': 'Main'}),
          _step(9, 'Close Window', {'window_name': 'Inspector'}),
        ],
      },
    });

    final res = await runner.run(const ButtonActionModel.performScript(id: 's1', name: 'Dynamic'));
    expect(res.completed, isTrue, reason: res.message);
    expect(host.records[0]['status'], 'Inspector');
    expect(host.currentLayout, 'MainLayout');
    expect(host.log, containsAllInOrder([
      'go_to_layout:ReportLayout',
      'go_to_layout:MainLayout',
      'go_to_layout:DetailLayout',
      'set_field:status=Inspector',
      'go_to_layout:MainLayout',
    ]));
  });

  test('Show Custom Dialog buttons, input fields, and Get(LastMessageChoice) (#133)', () async {
    final host = _FullExtendedHost()
      ..nextDialogButton = 1
      ..nextDialogInputs = {'status': 'approved', r'$note': 'checked'};
    final runner = _runner(host, {
      'dlg': {
        'id': 'dlg',
        'name': 'Prompt',
        'is_active': true,
        'steps': [
          _step(1, 'Show Custom Dialog', {
            'title': '"Approve"',
            'title_is_calculation': true,
            'message': '"Enter status"',
            'message_is_calculation': true,
            'buttons': [
              {'label': '"OK"', 'is_calculation': true, 'commit': true},
              {'label': '"Cancel"', 'is_calculation': true, 'commit': false},
            ],
            'input_fields': [
              {'field': 'status', 'label': '"Status"', 'label_is_calculation': true},
              {'field': r'$note', 'label': '"Note"', 'label_is_calculation': true},
            ],
          }),
          _step(2, 'If', {'condition': 'Get(LastMessageChoice) = 1'}),
          _step(3, 'Set Field', {'field': 'name', 'value': r'$note', 'value_is_calculation': true}),
          _step(4, 'End If'),
        ],
      },
    });

    final res = await runner.run(const ButtonActionModel.performScript(id: 'dlg', name: 'Prompt'));
    expect(res.completed, isTrue, reason: res.message);
    expect(host.records[0]['status'], 'approved');
    expect(host.records[0]['name'], 'checked');
  });

  test('Remaining steps: Sort, Omit, Show Omitted Only, Copy/Paste, Insert Calculated Result, Replace, Insert from URL, Print, Send Mail, UI chrome (#133)', () async {
    final host = _FullExtendedHost(records: [
      {'name': 'Alpha', 'status': 'old'},
      {'name': 'Beta', 'status': 'old'},
    ])
      ..nextUrlBody = 'fetched-body';

    final runner = _runner(host, {
      'all': {
        'id': 'all',
        'name': 'AllRemaining',
        'is_active': true,
        'steps': [
          _step(1, 'Freeze Window'),
          _step(2, 'Sort Records', {'restore': true, 'dialog': false, 'sort_order': [{'field': 'name', 'order': 'descending'}]}),
          _step(3, 'Go to Field', {'field': 'name', 'select': true}),
          _step(4, 'Copy'),
          _step(5, 'Go to Next Field'),
          _step(6, 'Paste'),
          _step(7, 'Go to Object', {'object_name': 'ObjTitle'}),
          _step(8, 'Insert Calculated Result', {'field': 'status', 'value': '"calc_val"'}),
          _step(9, 'Replace Field Contents', {'field': 'status', 'value': '"replaced"'}),
          _step(10, 'Omit Record'),
          _step(11, 'Show Omitted Only'),
          _step(12, 'Insert from URL', {'url': '"https://example.invalid/data"', 'target': r'$$fetched'}),
          _step(13, 'Print Setup', {'print_settings': {'orientation': 'landscape'}}),
          _step(14, 'Print', {'dialog': false}),
          _step(15, 'Save Records as PDF', {'path': 'out.pdf', 'dialog': false}),
          _step(16, 'Export Field Contents', {'field': 'name', 'path': 'name.txt'}),
          _step(17, 'Insert Picture', {'field': 'name', 'path': 'pic.png'}),
          _step(18, 'Insert File', {'field': 'name', 'path': 'doc.pdf'}),
          _step(19, 'Go to Related Record', {'related_table': 'Details', 'layout_name': 'DetailLayout'}),
          _step(20, 'Go to Portal Row', {'target': 'first'}),
          _step(21, 'Delete Portal Row', {'dialog': false}),
          _step(22, 'Send Mail', {'to': '"a@example.invalid"', 'subject': '"Hi"', 'message': '"Body"'}),
          _step(23, 'Beep'),
          _step(24, 'Install Menu Set', {'menu_set': 'Custom'}),
          _step(25, 'Show/Hide Toolbars', {'state': 'hide'}),
          _step(26, 'Show/Hide Menubar', {'state': 'hide'}),
          _step(27, 'Show/Hide Text Ruler', {'state': 'show'}),
          _step(28, 'Commit Records/Requests', {'dialog': false, 'skip_validation': true, 'force_commit': true}),
          _step(29, 'Refresh Window', {'flush_join_cache': true}),
          _step(30, 'Unsort Records'),
          _step(31, 'Delete All Records', {'dialog': false}),
        ],
      },
    });

    final res = await runner.run(const ButtonActionModel.performScript(id: 'all', name: 'AllRemaining'));
    expect(res.completed, isTrue, reason: res.message);
    expect(runner.globalVariables[r'$$fetched'], 'fetched-body');
    expect(host.log, containsAll(<String>[
      'sort_records:restore=true,levels=1,dialog=false',
      'copy:Alpha',
      'set_field:status=Alpha',
      'go_to_object:ObjTitle',
      'replace_field_contents:status=replaced',
      'omit_record',
      'show_omitted_only',
      'insert_from_url:https://example.invalid/data',
      'print:dialog=false',
      'save_pdf:out.pdf:append=false',
      'export_field:name:name.txt',
      'insert_picture:name:pic.png',
      'insert_file:name:doc.pdf',
      'gtrr:Details|layout=DetailLayout|only=true|newWin=false',
      'go_to_portal_row:first',
      'delete_portal_row:false',
      'send_mail:a@example.invalid|Hi|Body',
      'beep',
      'ui:toolbars=hide',
      'ui:menubar=hide',
      'ui:text_ruler=show',
      'commit_records:dialog=false,skip=true,force=true',
      'refresh_window:join=true,ext=false',
      'unsort_records',
      'delete_all_records:false',
    ]));
  });


  test('Insert Current Date, Select All, Refresh Portal and Set Error Logging', () async {
    final now = DateTime.now();
    final today = '${now.year.toString().padLeft(4, '0')}-'
        '${now.month.toString().padLeft(2, '0')}-'
        '${now.day.toString().padLeft(2, '0')}';
    final host = _FullExtendedHost(records: [
      {'name': 'Alpha', 'status': 'note ', 'visit_date': '2000-01-01', 'stamp': ''},
    ])
      ..fieldTypes = {'visit_date': 'DATE', 'stamp': 'TIMESTAMP'};
    final runner = _runner(host, const {});
    final script = ScriptModel.fromJson({
      'id': 'dates',
      'name': 'Dates',
      'is_active': true,
      'steps': [
        _step(1, 'Set Error Logging', {'state': true, 'custom_debug_info': r'$i'}),
        _step(2, 'Set Variable', {'name': r'$i', 'value': '7'}),
        _step(3, 'Insert Current Date', {'field': 'visit_date', 'select': false}),
        _step(4, 'Insert Current Date', {'field': 'status', 'select': false}),
        _step(5, 'Insert Current Date', {'field': 'stamp', 'select': true}),
        _step(6, 'Insert Current Date', {'target': r'$d', 'select': true}),
        _step(7, 'Go to Field', {'field': 'name'}),
        _step(8, 'Select All'),
        _step(9, 'Set Error Capture', {'state': true}),
        _step(10, 'Refresh Portal', {'object_name': 'Lines'}),
        _step(11, 'Refresh Portal', {'object_name': 'Missing'}),
        _step(12, 'Insert Current Date', {'field': 'nope'}),
        _step(13, 'Set Variable', {'name': r'$e', 'value': 'Get(LastError)'}),
        _step(14, 'Set Error Logging', {'state': false}),
        _step(15, 'Go to Layout', {'layout_name': 'NoSuchLayout'}),
      ],
    });
    final session = ScriptSessionState();
    final execution = ScriptExecution(script: script, runner: runner, session: session);
    while (!execution.atEnd) {
      final outcome = await execution.advance();
      expect(outcome.kind, isNot(StepOutcomeKind.stopped), reason: outcome.message);
    }

    final record = host.records[0];
    expect(record['visit_date'], today, reason: 'a date field holds one date: the date replaces it');
    expect(record['status'], 'note $today', reason: 'text without Select entire contents: appended');
    expect(record['stamp'], '$today 00:00:00');
    expect(execution.localVars[r'$d'], today);
    expect(execution.localVars[r'$e'], '102');
    expect(host.log, containsAll(<String>['go_to_field:name:select=true', 'refresh_portal:Lines']));
    expect(host.log, isNot(contains('refresh_portal:Missing')));

    // Logged: the two errors raised while logging was on, each once, with the
    // step's line and the debug calculation's value. Not logged: the error
    // raised after Off.
    expect(session.errorLog.map((e) => '${e.line}:${e.errorCode}:${e.customInfo}').toList(),
        ['11:116:7', '12:102:7']);
    expect(session.errorLog.first.toLogLine(), contains('Refresh Portal (11)\t116\t7'));
    expect(session.errorLogging, isFalse);
  });

  test('Open File resolves the file reference and checks the solution can be reached', () async {
    final signedIn = <String>{'remote_db'};
    final mock = MockClient((req) async {
      final segments = req.url.pathSegments;
      if (req.url.path == '/api/v1/databases') {
        return http.Response(jsonEncode({'databases': ['this_db', 'remote_db']}), 200,
            headers: {'content-type': 'application/json'});
      }
      if (segments.length == 5 && segments[2] == 'file-references' && segments[4] == 'resolve') {
        final target = switch (segments[3]) {
          'Self' => 'this_db',
          'Archive' => 'remote_db',
          'Retired' => 'gone_db',
          _ => null,
        };
        if (segments[3] == 'Unmapped') {
          return http.Response(jsonEncode({'title': 'File Reference Not Mapped', 'detail': 'unmapped'}), 409);
        }
        if (target == null) {
          return http.Response(jsonEncode({'title': 'File Reference Not Found', 'detail': 'none'}), 404);
        }
        return http.Response(
            jsonEncode({
              'name': segments[3],
              'database': target,
              'calling_database': 'this_db',
              'local': target == 'this_db',
            }),
            200,
            headers: {'content-type': 'application/json'});
      }
      return http.Response('Not found', 404);
    });
    final runner = LayoutActionRunner(
      apiClient: ApiClient(baseUrl: 'http://test:8080', httpClient: mock),
      host: _FullExtendedHost(),
      peerTokenProvider: (db) async => signedIn.contains(db) ? 'token-$db' : null,
    );

    Future<int> errorOf(String reference) async {
      final context = ScriptCallContext(session: ScriptSessionState()..errorCapture = true);
      final result = await runner.runStep('Open File', {'file_reference': reference, 'hidden': true},
          depth: 0, callContext: context);
      expect(result.completed, isTrue, reason: result.message);
      return context.session.lastError;
    }

    expect(await errorOf('Self'), 0, reason: 'this solution is open');
    expect(await errorOf('Archive'), 0, reason: 'installed, and the session is signed in to it');
    expect(await errorOf('Retired'), 100, reason: 'mapped to a solution not installed here');
    expect(await errorOf('Unmapped'), 100);
    expect(await errorOf('Unknown'), 114);
    expect(await errorOf(''), 100);

    signedIn.clear();
    expect(await errorOf('Archive'), 9, reason: 'the user has no session, so no privileges, there');
    final stopped = await runner.runStep('Open File', {'file_reference': 'Archive'}, depth: 0);
    expect(stopped.completed, isFalse);
    expect(stopped.message, contains('not signed in'));
  });
}
