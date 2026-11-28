// #133 — remaining FileMaker script steps, options, error capture, loop exit,
// dynamic targets, window model, and runner/debugger parity.

import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/script_workspace/script_debugger.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

class _FullExtendedHost implements LayoutActionHost, ExtendedLayoutActionHost, RelatedFieldHost {
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
  Future<void> actionEnterFindMode() async {
    findMode = true;
    log.add('enter_find_mode');
  }
  @override
  Future<void> actionEnterFindModeEx({List<Map<String, dynamic>>? requests, bool restore = false}) async {
    findMode = true;
    log.add('enter_find_mode:restore=$restore,reqs=${requests?.length ?? 0}');
  }

  @override
  Future<void> actionPerformFind() async {
    findMode = false;
    log.add('perform_find');
  }
  @override
  Future<int> actionPerformFindEx({List<Map<String, dynamic>>? requests, bool restore = false}) async {
    findMode = false;
    log.add('perform_find:restore=$restore,reqs=${requests?.length ?? 0}');
    return nextFindCount ?? records.length;
  }

  @override
  Future<void> actionShowAllRecords() async => log.add('show_all_records');
  @override
  Future<void> actionEnterPreviewMode() async => log.add('enter_preview_mode');
  @override
  Future<void> actionEnterBrowseMode() async {
    findMode = false;
    log.add('enter_browse_mode');
  }

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
    selectedField = select ? field : null;
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
  Future<int> actionGoToRelatedRecord({
    required String relatedTable,
    String? layoutName,
    bool showOnlyRelated = true,
    bool newWindow = false,
    bool matchFoundSet = false,
  }) async {
    log.add('gtrr:$relatedTable|layout=$layoutName|only=$showOnlyRelated|newWin=$newWindow');
    if (layoutName != null && layoutName.isNotEmpty) {
      return await actionGoToLayout(layoutName) ? 0 : 105;
    }
    return 0;
  }

  @override
  String? get goToRelatedRecordMessage => null;

  @override
  Future<GoToRecordOutcome> actionGoToPortalRow(String target, {bool select = false}) async {
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
  Future<int> actionDeletePortalRow({required bool confirm}) async {
    log.add('delete_portal_row:$confirm');
    return 0;
  }

  /// Position of the record whose write the server rejects (0: none), and
  /// the FileMaker error it reports.
  int rejectWriteAt = 0;
  int rejectWriteError = 504;

  @override
  Future<ReplaceOutcome> actionReplaceFieldContents({
    required String field,
    String? table,
    FutureOr<String?> Function(int position, int count)? valueFor,
  }) async {
    if (table != null && relatedOccurrences.contains(table)) return const ReplaceOutcome(relatedField: true);
    if (records.isEmpty || !currentRecord.containsKey(field)) {
      return const ReplaceOutcome(errorCode: 102, detail: 'no such field');
    }
    final start = recordIdx;
    final contents = currentRecord[field] ?? '';
    final count = records.length;
    try {
      for (var i = 0; i < count; i++) {
        recordIdx = i;
        final value = valueFor == null ? contents : await valueFor(i + 1, count);
        if (value == null) return ReplaceOutcome(count: count, replaced: i, unevaluatedAt: i + 1);
        if (i + 1 == rejectWriteAt) {
          return ReplaceOutcome(errorCode: rejectWriteError, detail: 'rejected', count: count, replaced: i);
        }
        records[i][field] = value;
      }
    } finally {
      recordIdx = start;
    }
    log.add('replace_field_contents:$field');
    return ReplaceOutcome(count: count, replaced: count);
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

  /// Table occurrences shown on the layout that are not its own.
  Set<String> relatedOccurrences = <String>{};

  // Any occurrence but those in [relatedOccurrences] is the window's own; a
  // related field is not written here.
  @override
  Future<bool> actionIsOwnOccurrence(String occurrence) async => !relatedOccurrences.contains(occurrence);

  @override
  Future<String?> actionRelatedFieldText(String occurrence, String field) async => null;

  @override
  Future<int> actionSetRelatedField(String occurrence, String field, String value) async => 102;

  /// Find mode: insertions go to [findRequest], not the record.
  bool findMode = false;
  final Map<String, String> findRequest = <String, String>{};

  /// The field whose whole content is selected (Go to Field [Select], Select
  /// All); an insertion replaces it.
  String? selectedField;

  @override
  String? actionFieldType(String field, {String? table}) {
    if (table != null && relatedOccurrences.contains(table)) return null;
    return currentRecord.containsKey(field) ? (fieldTypes[field] ?? 'TEXT') : null;
  }

  @override
  Future<FieldInsertOutcome> actionInsertIntoField({
    required String field,
    required String text,
    required bool replaceAll,
    String? table,
  }) async {
    if (table != null && relatedOccurrences.contains(table)) return FieldInsertOutcome.relatedField;
    if (records.isEmpty || !currentRecord.containsKey(field)) return FieldInsertOutcome.noSuchField;
    final target = findMode ? findRequest : currentRecord;
    final replace = replaceAll || selectedField == field;
    target[field] = replace ? text : '${target[field] ?? ''}$text';
    if (selectedField == field) selectedField = null;
    log.add('${findMode ? 'find_request' : 'insert'}:$field=${target[field]}');
    return FieldInsertOutcome.written;
  }

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

// A 1x1 PNG, the image of a drawn page.
final Uint8List _png = base64Decode(
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==');

/// A window that draws, composes and opens documents (#133).
class _DocumentHost extends _FullExtendedHost implements DocumentActionHost {
  _DocumentHost({super.records});

  final List<String> renders = [];
  final List<({OutgoingMail mail, List<String> attachments})> composed = [];
  final List<String> opened = [];
  final List<String> notices = [];
  List<PdfSheetPage>? nextPages;
  OutgoingMail? Function(OutgoingMail mail) compose = (m) => m;

  @override
  Future<List<PdfSheetPage>?> actionRenderRecordsPdf(String records) async {
    renders.add(records);
    if (nextPages != null) return nextPages;
    final count = records == 'browsed' ? this.records.length : 1;
    return [for (var i = 0; i < count; i++) PdfSheetPage(width: 595, height: 842, png: _png)];
  }

  @override
  Future<bool> actionForEachFoundRecord(Future<bool> Function(int position, int count) visit) async {
    final before = recordIdx;
    for (var i = 0; i < records.length; i++) {
      recordIdx = i;
      if (!await visit(i + 1, records.length)) break;
    }
    recordIdx = before;
    return true;
  }

  @override
  Future<OutgoingMail?> actionComposeMail(OutgoingMail mail, {required String from, List<String> attachments = const []}) async {
    composed.add((mail: mail, attachments: attachments));
    return compose(mail);
  }

  @override
  Future<void> actionOpenDocument(String path, String nativePath, Uint8List bytes) async => opened.add(path);

  @override
  Future<void> actionNotify(String message) async => notices.add(message);
}

/// The server's mail endpoints, as a script sees them: never real mail.
class _MailServer {
  _MailServer({this.configured = false, this.maxBatch = 100, this.refuseWith});
  final bool configured;
  final int maxBatch;
  final int? refuseWith;
  final List<Map<String, dynamic>> requests = [];

  Future<http.Response> handle(http.Request req) async {
    const json = {'content-type': 'application/json'};
    if (req.url.path == '/api/v1/mail/status') {
      return http.Response(jsonEncode({
        'smtp_configured': configured,
        'scope': configured ? 'server' : '',
        'from_name': 'Example',
        'from_address': 'noreply@example.invalid',
        'max_batch': maxBatch,
      }), 200, headers: json);
    }
    if (req.url.path == '/api/v1/mail/send' && req.method == 'POST') {
      if (refuseWith != null) {
        return http.Response(jsonEncode({
          'title': 'Mail not sent',
          'status': 502,
          'detail': 'the SMTP server refused a recipient',
          'sent': 0,
          'filemaker_error': refuseWith,
        }), 502, headers: {'content-type': 'application/problem+json'});
      }
      final body = Map<String, dynamic>.from(jsonDecode(req.body) as Map);
      requests.add(body);
      return http.Response(jsonEncode({'sent': (body['messages'] as List).length}), 200, headers: json);
    }
    return http.Response('Not found', 404);
  }
}

LayoutActionRunner _mailRunner(_DocumentHost host, ScriptFileStore files, _MailServer server) => LayoutActionRunner(
      apiClient: ApiClient(baseUrl: 'http://test:8080', httpClient: MockClient(server.handle)),
      host: host,
      files: files,
    );

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

  test('a find the server refuses sets Get(LastError) to 3, not 401 (#149)', () async {
    final host = _FullExtendedHost()..nextFindCount = -2;
    final runner = _runner(host, {
      's1': {
        'id': 's1',
        'name': 'RefusedFind',
        'is_active': true,
        'steps': [
          _step(1, 'Set Error Capture', {'state': true}),
          _step(2, 'Perform Find', {'restore': true, 'find_requests': [{'action': 'find', 'criteria': [{'field': 'status', 'value': '>1'}]}]}),
          _step(3, 'Set Field', {'field': 'name', 'value': 'Get(LastError)', 'value_is_calculation': true}),
        ],
      },
    });

    final result = await runner.run(const ButtonActionModel.performScript(id: 's1', name: 'RefusedFind'));
    expect(result.completed, isTrue, reason: result.message);
    expect(host.records[0]['name'].toString(), '3');
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
          _step(15, 'Save Records as Excel', {'path': 'out.xlsx', 'dialog': false}),
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
      'replace_field_contents:status',
      'omit_record',
      'show_omitted_only',
      'insert_from_url:https://example.invalid/data',
      'print:dialog=false',
      'save_pdf:out.xlsx:append=false',
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
    expect(runner.errorLogging.entries.map((e) => '${e.line}:${e.errorCode}:${e.customInfo}').toList(),
        ['11:116:7', '12:102:7']);
    expect(runner.errorLogging.entries.first.toLogLine(), contains('Refresh Portal (11)\t116\t7'));
    expect(runner.errorLogging.enabled, isFalse);
  });

  group('Insert Current Date targets', () {
    final now = DateTime.now();
    final today = '${now.year.toString().padLeft(4, '0')}-'
        '${now.month.toString().padLeft(2, '0')}-'
        '${now.day.toString().padLeft(2, '0')}';

    Future<ScriptExecution> runSteps(LayoutActionRunner runner, List<Map<String, dynamic>> steps,
        {ScriptSessionState? session}) async {
      final script = ScriptModel.fromJson({'id': 's', 'name': 'S', 'is_active': true, 'steps': steps});
      final execution = ScriptExecution(script: script, runner: runner, session: session ?? ScriptSessionState());
      while (!execution.atEnd) {
        final outcome = await execution.advance();
        expect(outcome.kind, isNot(StepOutcomeKind.stopped), reason: outcome.message);
      }
      return execution;
    }

    test('in Find mode the date goes into the find request, never the record', () async {
      final host = _FullExtendedHost(records: [
        {'name': 'Alpha', 'visit_date': '2000-01-01'},
      ])
        ..fieldTypes = {'visit_date': 'DATE'};
      await runSteps(_runner(host, const {}), [
        _step(1, 'Enter Find Mode', {'pause': false}),
        _step(2, 'Insert Current Date', {'field': 'visit_date'}),
      ]);
      expect(host.findRequest['visit_date'], today);
      expect(host.records[0]['visit_date'], '2000-01-01', reason: 'the stored record is not written');
    });

    test('a target that was named but not read never falls back to the active field', () async {
      final host = _FullExtendedHost(records: [
        {'name': 'Alpha', 'status': 'keep'},
      ]);
      final execution = await runSteps(_runner(host, const {}), [
        _step(1, 'Set Error Capture', {'state': true}),
        _step(2, 'Go to Field', {'field': 'status'}),
        _step(3, 'Insert Current Date', {'target_unresolved': true, 'select': true}),
        _step(4, 'Set Variable', {'name': r'$unresolved', 'value': 'Get(LastError)'}),
        _step(5, 'Insert Current Date', {'target': 'Elsewhere::Notes', 'select': true}),
        _step(6, 'Set Variable', {'name': r'$unreadable', 'value': 'Get(LastError)'}),
        // The DDR form <Field>$today</Field>, decoded to name.
        _step(7, 'Insert Current Date', {'name': r'$today', 'select': true}),
      ]);
      expect(execution.localVars[r'$unresolved'], '102');
      expect(execution.localVars[r'$unreadable'], '102');
      expect(execution.localVars[r'$today'], today);
      expect(host.records[0]['status'], 'keep', reason: 'the active field is left alone');
    });

    test('a related field the window cannot write fails and writes nothing', () async {
      final host = _FullExtendedHost(records: [
        {'name': 'Alpha', 'posted_on': '2000-01-01'},
      ])
        ..relatedOccurrences = {'Payments'}
        ..fieldTypes = {'posted_on': 'DATE'};
      final execution = await runSteps(_runner(host, const {}), [
        _step(1, 'Set Error Capture', {'state': true}),
        _step(2, 'Insert Current Date', {'field': 'posted_on', 'field_table': 'Payments'}),
        _step(3, 'Set Variable', {'name': r'$e', 'value': 'Get(LastError)'}),
      ]);
      expect(execution.localVars[r'$e'], '102');
      expect(host.records[0]['posted_on'], '2000-01-01', reason: 'the layout table is not written instead');
      expect(host.log.where((l) => l.startsWith('insert:')), isEmpty);
    });

    test('after Select All the date replaces the selection even without Select entire contents', () async {
      final host = _FullExtendedHost(records: [
        {'name': 'Alpha', 'status': 'pending'},
      ]);
      await runSteps(_runner(host, const {}), [
        _step(1, 'Go to Field', {'field': 'status'}),
        _step(2, 'Select All'),
        _step(3, 'Insert Current Date', {'field': 'status', 'select': false}),
        _step(4, 'Insert Current Date', {'field': 'status', 'select': false}),
      ]);
      expect(host.records[0]['status'], '$today$today',
          reason: 'the selection is replaced once; the second insertion is at the caret after it');
    });
  });

  group('Set Error Logging', () {
    Future<void> runSteps(LayoutActionRunner runner, List<Map<String, dynamic>> steps) async {
      final script = ScriptModel.fromJson({'id': 's', 'name': 'S', 'is_active': true, 'steps': steps});
      // A fresh session each time, as each button click or trigger gets.
      final execution = ScriptExecution(script: script, runner: runner, session: ScriptSessionState());
      while (!execution.atEnd) {
        await execution.advance();
      }
    }

    final fail = _step(9, 'Go to Layout', {'layout_name': 'NoSuchLayout'});
    final capture = _step(1, 'Set Error Capture', {'state': true});

    test('stays on for the solution session until a script turns it off', () async {
      final host = _FullExtendedHost();
      final runner = _runner(host, const {});
      await runSteps(runner, [_step(1, 'Set Error Logging', {'state': true})]);
      await runSteps(runner, [capture, fail]);
      expect(runner.errorLogging.entries, hasLength(1), reason: 'a later script run is still logged');

      await runSteps(runner, [_step(1, 'Set Error Logging', {'state': false})]);
      await runSteps(runner, [capture, fail]);
      expect(runner.errorLogging.entries, hasLength(1), reason: 'nothing is logged after Off');

      final otherSession = _runner(_FullExtendedHost(), const {});
      expect(otherSession.errorLogging.enabled, isFalse, reason: 'another sign-in starts with logging off');
    });

    test('a step whose state was not read leaves logging off', () async {
      final runner = _runner(_FullExtendedHost(), const {});
      await runSteps(runner, [_step(1, 'Set Error Logging'), capture, fail]);
      expect(runner.errorLogging.enabled, isFalse);
      expect(runner.errorLogging.entries, isEmpty);
    });
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

  group('FileMaker step options read at conversion', () {
    test('Replace Field Contents with a calculation evaluates it for each record', () async {
      final host = _FullExtendedHost(records: [
        {'name': 'Alpha', 'status': ''},
        {'name': 'Beta', 'status': ''},
        {'name': 'Gamma', 'status': ''},
      ])
        ..recordIdx = 1;
      final runner = _runner(host, const {});
      final res = await runner.runStep('Replace Field Contents', {
        'field': 'status',
        'replace_type': 'calculation',
        'value': 'name & "-done"',
        'value_is_calculation': true,
      }, depth: 0);
      expect(res.completed, isTrue, reason: res.message);
      expect(host.records.map((r) => r['status']), ['Alpha-done', 'Beta-done', 'Gamma-done']);
      expect(host.recordIdx, 1, reason: 'the current record is the same one afterwards');
    });

    test('Replace Field Contents with serial numbers counts from the initial value', () async {
      final host = _FullExtendedHost(records: [
        {'name': 'Alpha', 'status': ''},
        {'name': 'Beta', 'status': ''},
      ]);
      final res = await _runner(host, const {}).runStep('Replace Field Contents', {
        'field': 'status',
        'replace_type': 'serial_numbers',
        'serial_initial': '10',
        'serial_increment': '5',
      }, depth: 0);
      expect(res.completed, isTrue, reason: res.message);
      expect(host.records.map((r) => r['status']), ['10', '15']);
    });

    test('Replace Field Contents with dialog writes nothing when cancelled', () async {
      final host = _FullExtendedHost(records: [
        {'name': 'Alpha', 'status': 'keep'},
      ])
        ..nextDialogButton = 2;
      final runner = _runner(host, const {});
      final session = ScriptSessionState();
      final res = await runner.runStep('Replace Field Contents', {
        'field': 'status',
        'replace_type': 'calculation',
        'value': '"new"',
        'dialog': true,
      }, depth: 0, callContext: ScriptCallContext(session: session));
      expect(res.completed, isTrue);
      expect(session.lastError, 1);
      expect(host.records[0]['status'], 'keep');
    });

    test('Replace Field Contents with dialog closed without a button is a cancel, not Replace', () async {
      final host = _FullExtendedHost(records: [
        {'name': 'Alpha', 'status': 'keep'},
      ])
        ..nextDialogButton = ScriptDialogResult.dismissed;
      final session = ScriptSessionState();
      final res = await _runner(host, const {}).runStep('Replace Field Contents', {
        'field': 'status',
        'replace_type': 'calculation',
        'value': '"new"',
        'dialog': true,
      }, depth: 0, callContext: ScriptCallContext(session: session));
      expect(res.completed, isTrue);
      expect(session.lastError, 1);
      expect(host.records[0]['status'], 'keep');
    });

    test('Replace Field Contents of a related field writes nothing, not the same-named local field', () async {
      for (final params in <Map<String, dynamic>>[
        {'field': 'status', 'field_table': 'Payments'},
        {'field': 'Payments::status'},
      ]) {
        final host = _FullExtendedHost(records: [
          {'name': 'Alpha', 'status': 'keep'},
        ])
          ..relatedOccurrences = {'Payments'};
        final session = ScriptSessionState();
        final res = await _runner(host, const {}).runStep('Replace Field Contents', {
          ...params,
          'replace_type': 'calculation',
          'value': '"new"',
        }, depth: 0, callContext: ScriptCallContext(session: session));
        expect(res.completed, isFalse, reason: '$params');
        expect(res.message, contains('Payments::status'));
        expect(res.message, contains('Nothing was written'));
        expect(session.lastError, 102);
        expect(host.records[0]['status'], 'keep', reason: '$params');
      }
    });

    test('Replace Field Contents stops at a write the server rejects and reports its error', () async {
      List<Map<String, String>> records() => [
            {'name': 'Alpha', 'status': ''},
            {'name': 'Beta', 'status': ''},
            {'name': 'Gamma', 'status': ''},
          ];
      final host = _FullExtendedHost(records: records())..rejectWriteAt = 2;
      final session = ScriptSessionState();
      final res = await _runner(host, const {}).runStep('Replace Field Contents', {
        'field': 'status',
        'replace_type': 'calculation',
        'value': 'name',
      }, depth: 0, callContext: ScriptCallContext(session: session));
      expect(res.completed, isFalse);
      expect(res.message, contains('record 2 of 3'));
      expect(res.message, contains('the record before it was replaced'));
      expect(session.lastError, 504);
      expect(host.records.map((r) => r['status']), ['Alpha', '', ''], reason: 'nothing after the rejected record');

      // With error capture on, the script goes on and can test the error.
      final captured = ScriptSessionState()..errorCapture = true;
      final again = await _runner(_FullExtendedHost(records: records())..rejectWriteAt = 1, const {})
          .runStep('Replace Field Contents', {'field': 'status', 'value': '"x"'},
              depth: 0, callContext: ScriptCallContext(session: captured));
      expect(again.completed, isTrue);
      expect(captured.lastError, 504);
    });

    test('Replace Field Contents with the current contents writes the record in hand to all', () async {
      final host = _FullExtendedHost(records: [
        {'name': 'Alpha', 'status': 'a'},
        {'name': 'Beta', 'status': 'b'},
      ])
        ..recordIdx = 1;
      final res = await _runner(host, const {}).runStep('Replace Field Contents', {
        'field': 'status',
        'replace_type': 'current_contents',
      }, depth: 0);
      expect(res.completed, isTrue, reason: res.message);
      expect(host.records.map((r) => r['status']), ['b', 'b']);
    });

    test('Show Custom Dialog commits its input fields only through a committing button', () async {
      Future<(String?, int)> answer(int button) async {
        final host = _FullExtendedHost(records: [
          {'name': 'Alpha', 'status': 'old'},
        ])
          ..nextDialogButton = button
          ..nextDialogInputs = {'status': 'typed'};
        final session = ScriptSessionState();
        final res = await _runner(host, const {}).runStep('Show Custom Dialog', {
          'title': 'Status',
          'buttons': [
            {'label': 'OK', 'commit': true},
            {'label': 'Cancel', 'commit': false},
          ],
          'input_fields': [
            {'field': 'status', 'label': 'Status'},
          ],
        }, depth: 0, callContext: ScriptCallContext(session: session));
        expect(res.completed, isTrue, reason: res.message);
        return (host.records[0]['status'], session.lastMessageChoice);
      }

      expect(await answer(1), ('typed', 1));
      expect(await answer(2), ('old', 2), reason: 'Cancel does not commit');
      expect(await answer(ScriptDialogResult.dismissed), ('old', 0),
          reason: 'closing the dialog is no button, and commits nothing');
    });

    test('Save Records as PDF into a variable reports error 3 with its migration note', () async {
      final host = _FullExtendedHost();
      final params = {
        'save_to': 'variable',
        'dialog': false,
        'migration_note': 'Save Records as PDF saves into a variable',
      };
      final stopped = await _runner(host, const {}).runStep('Save Records as PDF', params, depth: 0);
      expect(stopped.completed, isFalse);
      expect(stopped.message, contains('into a variable'));
      final session = ScriptSessionState()..errorCapture = true;
      final captured = await _runner(host, const {})
          .runStep('Save Records as PDF', params, depth: 0, callContext: ScriptCallContext(session: session));
      expect(captured.completed, isTrue);
      expect(session.lastError, 3);
    });

    test('Send Mail without an SMTP profile drafts what a client can carry, and never sends formula text', () async {
      final host = _FullExtendedHost();
      final runner = _runner(host, const {});
      final files = runner.files;
      final attached = await runner.runStep('Send Mail', {
        'to': '"a@example.invalid"',
        'attachments': [r'$report'],
      }, depth: 0, localVars: {r'$report': '${files.temporaryPath}Report.pdf'});
      expect(attached.completed, isTrue, reason: attached.message);
      expect(host.log, contains('send_mail:a@example.invalid||'));
      expect(host.log.where((l) => l.startsWith('show_dialog:Send Mail|An e-mail draft cannot carry attachments')),
          hasLength(1), reason: 'the user is told which file to attach');
      host.log.clear();
      final smtp = await runner.runStep('Send Mail', {'to': '"a@example.invalid"', 'send_via': 'smtp', 'dialog': false}, depth: 0);
      expect(smtp.completed, isFalse);
      expect(smtp.message, contains('SMTP'));
      expect(smtp.message, contains('1506'));
      final unreadable = await runner.runStep('Send Mail', {
        'to': 'NoSuchFunction ( 1 ; 2 )',
        'to_is_calculation': true,
      }, depth: 0);
      expect(unreadable.completed, isFalse);
      expect(host.log.where((l) => l.startsWith('send_mail')), isEmpty, reason: 'nothing was sent');
    });

    test('Perform Script of a deleted script fails with 104, and an unnamed one explains why', () async {
      final host = _FullExtendedHost();
      final runner = _runner(host, const {});
      final session = ScriptSessionState()..errorCapture = true;
      final missing = await runner.runStep('Perform Script', {'script_missing': true}, depth: 0,
          callContext: ScriptCallContext(session: session));
      expect(missing.completed, isTrue);
      expect(session.lastError, 104);
      final unnamed = await runner.runStep('Perform Script', {
        'script_unresolved': true,
        'file_reference': 'Ledger',
        'migration_note': 'Perform Script calls a script of the file "Ledger" that the document does not name',
      }, depth: 0);
      expect(unnamed.completed, isFalse);
      expect(unnamed.message, contains('does not name'));
    });

    test('a converted Import Records says what it did and why it needs a transfer', () async {
      final res = await _runner(_FullExtendedHost(), const {}).runStep('Import Records', {
        'migration_note': 'imports from the FileMaker file "Ledger" into orders',
      }, depth: 0);
      expect(res.completed, isFalse);
      expect(res.message, contains('converted from FileMaker, imports from the FileMaker file "Ledger"'));
    });

    test('the script editor shows the migration note', () {
      final step = ScriptStepModel.fromJson({
        'id': 'x',
        'step_type': 'Import Records',
        'params': {'migration_note': 'needs a transfer'},
      });
      expect(step.previewText, '[Needs attention: needs a transfer]');
    });
  });

  group('Save Records as PDF and Send Mail (#133)', () {
    test('a FileMaker path resolves only into the script folders', () {
      const roots = ['/File4Base/Temporary/', '/File4Base/Documents/'];
      String? resolve(String raw) => resolveScriptPath(raw, roots: roots, relativeRoot: roots[1]);
      expect(resolve('/File4Base/Temporary/Invoice.pdf'), '/File4Base/Temporary/Invoice.pdf');
      expect(resolve('filemac:/File4Base/Documents/Out/Report.pdf'), '/File4Base/Documents/Out/Report.pdf');
      expect(resolve('file:Report.pdf'), '/File4Base/Documents/Report.pdf', reason: 'relative: the documents folder');
      expect(resolve('filewin:/C:/Users/someone/Desktop/a.pdf'), isNull);
      expect(resolve('/File4Base/Temporary/../../etc/passwd'), isNull);
      expect(resolve('/elsewhere/a.pdf\n/File4Base/Temporary/b.pdf'), '/File4Base/Temporary/b.pdf',
          reason: 'a path list uses its first path in a script folder');
      expect(resolve(''), isNull);
      const windows = ['/C:/Users/someone/AppData/Local/Temp/file4base-1/', '/C:/Users/someone/Documents/File4Base/'];
      expect(
        resolveScriptPath('filewin:/c:/users/SOMEONE/Documents/File4Base/x.pdf', roots: windows, relativeRoot: windows[1]),
        isNotNull,
        reason: 'a Windows path compares without case',
      );
    });

    test('writes a real PDF at a path made with Get ( TemporaryPath ), and appends to it', () async {
      final mail = _MailServer();
      final host = _DocumentHost(records: [{'name': 'A', 'email': ''}, {'name': 'B', 'email': ''}]);
      final files = MemoryScriptFileStore();
      final runner = _mailRunner(host, files, mail);
      final out = runner.resolveValue('Get ( TemporaryPath ) & "Invoice.pdf"', const {});
      expect(out, '/File4Base/Temporary/Invoice.pdf');
      final vars = {r'$out': out!};
      final first = await runner.runStep('Save Records as PDF',
          {'path': r'$out', 'records': 'current', 'dialog': false}, depth: 0, localVars: vars);
      expect(first.completed, isTrue, reason: first.message);
      expect(host.renders, ['current']);
      final bytes = files.files[out]!;
      expect(ascii.decode(bytes.sublist(0, 5)), '%PDF-');
      expect(files.savedPdfPages[out], hasLength(1));

      final second = await runner.runStep('Save Records as PDF',
          {'path': r'$out', 'append': true, 'dialog': false}, depth: 0, localVars: vars);
      expect(second.completed, isTrue, reason: second.message);
      expect(host.renders, ['current', 'browsed'], reason: 'the found set by default');
      expect(files.savedPdfPages[out], hasLength(3), reason: 'one page, then two more appended');
      expect(files.files[out]!.length, greaterThan(bytes.length));
      expect(mail.requests, isEmpty);
    });

    test('reports 800 for a path outside the script folders, 401 without record, 3 to append to a foreign PDF', () async {
      final host = _DocumentHost();
      final files = MemoryScriptFileStore();
      final runner = _mailRunner(host, files, _MailServer());
      final session = ScriptSessionState()..errorCapture = true;
      final context = ScriptCallContext(session: session);

      final stopped = await runner.runStep('Save Records as PDF',
          {'path': 'filemac:/Users/someone/Desktop/Report.pdf', 'dialog': false}, depth: 0);
      expect(stopped.completed, isFalse);
      expect(stopped.message, contains('error 800'));
      expect(stopped.message, contains('Get ( TemporaryPath )'));
      await runner.runStep('Save Records as PDF',
          {'path': 'filemac:/Users/someone/Desktop/Report.pdf', 'dialog': false}, depth: 0, callContext: context);
      expect(session.lastError, 800);
      expect(files.files, isEmpty);

      host.nextPages = const [];
      await runner.runStep('Save Records as PDF', {'path': 'file:Empty.pdf'}, depth: 0, callContext: context);
      expect(session.lastError, 401);

      host.nextPages = null;
      files.files['/File4Base/Documents/Old.pdf'] = Uint8List.fromList(utf8.encode('%PDF-1.4'));
      await runner.runStep('Save Records as PDF', {'path': 'file:Old.pdf', 'append': true}, depth: 0, callContext: context);
      expect(session.lastError, 3);
      expect(utf8.decode(files.files['/File4Base/Documents/Old.pdf']!), '%PDF-1.4', reason: 'left as it was');
    });

    test('Create email hands the PDF to Send Mail: composed in File4Base and sent through SMTP', () async {
      final mail = _MailServer(configured: true);
      final host = _DocumentHost()
        ..compose = (m) => OutgoingMail(to: 'buyer@example.invalid', subject: 'Your invoice', message: m.message);
      final files = MemoryScriptFileStore();
      final runner = _mailRunner(host, files, mail);
      final res = await runner.runStep('Save Records as PDF',
          {'path': 'file:Invoice.pdf', 'records': 'current', 'create_email': true, 'auto_open': true}, depth: 0);
      expect(res.completed, isTrue, reason: res.message);
      expect(host.opened, ['/File4Base/Documents/Invoice.pdf']);
      expect(host.composed.single.attachments, ['Invoice.pdf']);
      final sent = mail.requests.single;
      expect(sent['messages'], [
        {'to': 'buyer@example.invalid', 'cc': '', 'bcc': '', 'subject': 'Your invoice', 'message': ''},
      ]);
      final attachment = (sent['attachments'] as List).single as Map;
      expect(attachment['name'], 'Invoice.pdf');
      expect(base64Decode(attachment['data'] as String), files.files['/File4Base/Documents/Invoice.pdf']);
    });

    test("one e-mail per record goes in batches, with the step's sender and reply-to", () async {
      final mail = _MailServer(configured: true, maxBatch: 2);
      final host = _DocumentHost(records: [
        {'name': 'Alpha', 'email': 'alpha@example.invalid'},
        {'name': 'Beta', 'email': ''},
        {'name': 'Gamma', 'email': 'gamma@example.invalid'},
        {'name': 'Delta', 'email': 'delta@example.invalid'},
      ])
        ..recordIdx = 1;
      final runner = _mailRunner(host, MemoryScriptFileStore(), mail);
      final res = await runner.runStep('Send Mail', {
        'send_via': 'smtp',
        'one_email_per_record': true,
        'dialog': false,
        'to': 'email',
        'to_is_calculation': true,
        'subject': '"Hello " & name',
        'subject_is_calculation': true,
        'message': '"Body"',
        'smtp_from_name': '"Sales"',
        'smtp_from': '"sales@example.invalid"',
        'smtp_reply_to': '"replies@example.invalid"',
        'smtp_server_in_step': true,
      }, depth: 0);
      expect(res.completed, isTrue, reason: res.message);
      expect(host.recordIdx, 1, reason: 'the current record is the same afterwards');
      expect(mail.requests, hasLength(2), reason: 'three messages, two per request');
      expect(mail.requests.first['from_name'], 'Sales');
      expect(mail.requests.first['from'], 'sales@example.invalid');
      expect(mail.requests.first['reply_to'], 'replies@example.invalid');
      final sent = [for (final r in mail.requests) ...(r['messages'] as List).cast<Map<String, dynamic>>()];
      expect(sent.map((m) => '${m['to']}|${m['subject']}'), [
        'alpha@example.invalid|Hello Alpha',
        'gamma@example.invalid|Hello Gamma',
        'delta@example.invalid|Hello Delta',
      ], reason: 'a record without address is skipped');
      expect(host.log.where((l) => l.startsWith('send_mail')), isEmpty, reason: 'no draft');
    });

    test('Send Mail reports the FileMaker errors: server refusal, missing file, cancelled, no profile', () async {
      final session = ScriptSessionState()..errorCapture = true;
      final context = ScriptCallContext(session: session);

      final refusing = _MailServer(configured: true, refuseWith: 1503);
      final host = _DocumentHost();
      final files = MemoryScriptFileStore();
      final runner = _mailRunner(host, files, refusing);
      await runner.runStep('Send Mail', {'send_via': 'smtp', 'dialog': false, 'to': '"a@example.invalid"'},
          depth: 0, callContext: context);
      expect(session.lastError, 1503);

      await runner.runStep('Send Mail', {'dialog': false, 'to': '"a@example.invalid"', 'attachments': ['file:Missing.pdf']},
          depth: 0, callContext: context);
      expect(session.lastError, 100, reason: 'a file the script folders do not have');
      await runner.runStep('Send Mail', {'dialog': false, 'to': '"a@example.invalid"', 'attachments': ['/etc/passwd']},
          depth: 0, callContext: context);
      expect(session.lastError, 100, reason: 'a file outside the script folders');

      final mail = _MailServer(configured: true);
      final cancelling = _DocumentHost()..compose = (_) => null;
      await _mailRunner(cancelling, files, mail)
          .runStep('Send Mail', {'send_via': 'smtp', 'to': '"a@example.invalid"'}, depth: 0, callContext: context);
      expect(session.lastError, 1, reason: 'the user cancelled the compose window');
      expect(mail.requests, isEmpty);

      final none = _MailServer();
      final draftHost = _DocumentHost();
      await _mailRunner(draftHost, files, none)
          .runStep('Send Mail', {'send_via': 'smtp', 'to': '"a@example.invalid"'}, depth: 0, callContext: context);
      expect(session.lastError, 1506);
      await _mailRunner(draftHost, files, none).runStep('Send Mail',
          {'dialog': false, 'to': '"a@example.invalid"', 'subject': '"S"'}, depth: 0, callContext: context);
      expect(session.lastError, 0, reason: 'without a profile, a draft as before');
      expect(draftHost.log, contains('send_mail:a@example.invalid|S|'));
      expect(none.requests, isEmpty);
    });

    test('a protected PDF is refused, never written unprotected', () async {
      final host = _DocumentHost();
      final files = MemoryScriptFileStore();
      final session = ScriptSessionState()..errorCapture = true;
      await _mailRunner(host, files, _MailServer()).runStep('Save Records as PDF',
          {'path': 'file:Locked.pdf', 'pdf_security': true}, depth: 0, callContext: ScriptCallContext(session: session));
      expect(session.lastError, 3);
      expect(files.files, isEmpty);
      expect(host.renders, isEmpty);
    });

    test('with its dialog and no window, Send Mail drafts one message and sends nothing', () async {
      final mail = _MailServer(configured: true);
      final windowless = _FullExtendedHost();
      final files = MemoryScriptFileStore();
      await files.write('/File4Base/Temporary/a.pdf', Uint8List.fromList([1, 2, 3]));
      final runner = LayoutActionRunner(
        apiClient: ApiClient(baseUrl: 'http://test:8080', httpClient: MockClient(mail.handle)),
        host: windowless,
        files: files,
      );
      final res = await runner.runStep('Send Mail', {
        'send_via': 'smtp',
        'to': '"a@example.invalid"',
        'subject': '"S"',
        'attachments': ['/File4Base/Temporary/a.pdf'],
      }, depth: 0);
      expect(res.completed, isTrue, reason: res.message);
      expect(mail.requests, isEmpty, reason: 'never sent without the dialog the step asks for');
      expect(windowless.log, contains('send_mail:a@example.invalid|S|'));
      expect(windowless.log.where((l) => l.startsWith('show_dialog:Send Mail|An e-mail draft cannot carry')), hasLength(1));
    });

    test('one e-mail per record with its dialog shows each message, and a cancel sends none', () async {
      final records = [
        {'name': 'Alpha', 'email': 'alpha@example.invalid'},
        {'name': 'Beta', 'email': 'beta@example.invalid'},
      ];
      final params = {'send_via': 'smtp', 'one_email_per_record': true, 'to': 'email', 'to_is_calculation': true};
      final mail = _MailServer(configured: true);
      final host = _DocumentHost(records: records);
      final res = await _mailRunner(host, MemoryScriptFileStore(), mail).runStep('Send Mail', params, depth: 0);
      expect(res.completed, isTrue, reason: res.message);
      expect(host.composed.map((c) => c.mail.to), ['alpha@example.invalid', 'beta@example.invalid']);
      expect((mail.requests.single['messages'] as List), hasLength(2));

      final cancelling = _DocumentHost(records: records)..compose = (m) => m.to.startsWith('beta') ? null : m;
      final none = _MailServer(configured: true);
      final session = ScriptSessionState();
      await _mailRunner(cancelling, MemoryScriptFileStore(), none)
          .runStep('Send Mail', params, depth: 0, callContext: ScriptCallContext(session: session));
      expect(session.lastError, 1);
      expect(none.requests, isEmpty);
    });

    test('the in-memory folders are emptied when the session ends', () async {
      final files = MemoryScriptFileStore();
      await files.write('/File4Base/Temporary/a.pdf', Uint8List.fromList([1]));
      files.savedPdfPages['/File4Base/Temporary/a.pdf'] = const [];
      await files.clear();
      expect(files.files, isEmpty);
      expect(files.savedPdfPages, isEmpty);
    });

    test('via the e-mail client without dialog sends through the profile when there is one', () async {
      final mail = _MailServer(configured: true);
      final host = _DocumentHost();
      final res = await _mailRunner(host, MemoryScriptFileStore(), mail)
          .runStep('Send Mail', {'dialog': false, 'to': '"a@example.invalid"', 'subject': '"S"', 'message': '"Line 1\rLine 2"'}, depth: 0);
      expect(res.completed, isTrue, reason: res.message);
      expect(host.composed, isEmpty, reason: 'no dialog');
      expect((mail.requests.single['messages'] as List).single, {'to': 'a@example.invalid', 'cc': '', 'bcc': '', 'subject': 'S', 'message': 'Line 1\rLine 2'});
      expect(host.log.where((l) => l.startsWith('send_mail')), isEmpty);
    });
  });
}
