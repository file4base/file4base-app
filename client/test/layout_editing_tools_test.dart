import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/widgets/file4base_status_sidebar.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/layout_designer_widget.dart';
import 'package:file4base_client/features/layout_engine/layout_object_visuals.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

const _table = TableModel(id: 't1', name: 'contacts', displayName: 'Contacts', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'first_name', displayName: 'First name', fieldType: 'varchar'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'city', displayName: 'City', fieldType: 'varchar'),
]);

/// 1x1 transparent PNG.
const _pngBase64 =
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=';

Future<LayoutDefinitionModel? Function()> _pumpDesigner(
  WidgetTester tester, {
  List<LayoutObjectModel> objects = const [],
  LayoutTool tool = LayoutTool.pointer,
  void Function(LayoutTool)? onToolChanged,
  GlobalKey<LayoutDesignerWidgetState>? key,
}) async {
  tester.view.physicalSize = const Size(1600, 1000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  LayoutDefinitionModel? last;
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: LayoutDesignerWidget(
        key: key,
        table: _table,
        tables: const [_table],
        apiClient: ApiClient(baseUrl: 'http://localhost:1'),
        initialLayout: LayoutDefinitionModel(id: 'l1', name: 'Contacts', tableOccurrence: 'contacts', objects: objects),
        onSaved: () {},
        onLayoutChanged: (l) => last = l,
        activeTool: tool,
        onToolChanged: onToolChanged,
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return () => last;
}

/// Lets the 1.5 s auto-save debounce fire (it fails silently: no server).
Future<void> _settleAutoSave(WidgetTester tester) => tester.pump(const Duration(seconds: 2));

void main() {
  group('Tool palette', () {
    testWidgets('status sidebar reports the picked tool instead of only highlighting it', (tester) async {
      LayoutTool? picked;
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: SizedBox(
            width: 220,
            height: 900,
            child: File4BaseStatusSidebar(
              onLayoutSelected: (_) {},
              tables: const [_table],
              selectedTable: _table,
              onTableSelected: (_) {},
              mode: OperationalMode.layout,
              currentRecordIndex: 0,
              totalRecords: 0,
              onPreviousRecord: () {},
              onNextRecord: () {},
              onGoToRecord: (_) {},
              onManageDatabase: () {},
              onToolSelected: (t) => picked = t,
            ),
          ),
        ),
      ));
      await tester.tap(find.byTooltip('Oval'));
      expect(picked, LayoutTool.oval);
      await tester.tap(find.byTooltip('Line'));
      expect(picked, LayoutTool.line);
      await tester.tap(find.byTooltip('Set Tab Order'));
      expect(picked, LayoutTool.tabOrder);
    });

    testWidgets('a tool chosen outside the designer creates its object and the designer reports the return to the pointer',
        (tester) async {
      final changes = <LayoutTool>[];
      final last = await _pumpDesigner(tester, tool: LayoutTool.line, onToolChanged: changes.add);
      await tester.tapAt(const Offset(700, 400));
      await tester.pump();
      final line = last()!.objects.single;
      expect(line.type, 'line');
      expect(line.height, greaterThanOrEqualTo(8), reason: 'lines keep a selectable thickness');
      expect(changes.last, LayoutTool.pointer);
      await _settleAutoSave(tester);
    });

    testWidgets('dragging with the oval tool draws an oval of the dragged size', (tester) async {
      final last = await _pumpDesigner(tester, tool: LayoutTool.oval);
      await tester.dragFrom(const Offset(600, 300), const Offset(160, 80));
      await tester.pump();
      final oval = last()!.objects.single;
      expect(oval.type, 'oval');
      expect(oval.width, closeTo(160, 8));
      expect(oval.height, closeTo(80, 8));
      final shape = tester.widget<Container>(
          find.descendant(of: find.byType(LayoutShapeView), matching: find.byType(Container)).first);
      expect((shape.decoration! as ShapeDecoration).shape, isA<OvalBorder>());
      await _settleAutoSave(tester);
    });

    testWidgets('dragging with the line tool draws a vertical line when the drag is vertical', (tester) async {
      final last = await _pumpDesigner(tester, tool: LayoutTool.line);
      await tester.dragFrom(const Offset(600, 250), const Offset(4, 160));
      await tester.pump();
      final line = last()!.objects.single;
      expect(line.type, 'line');
      expect(line.height, greaterThan(line.width));
      await _settleAutoSave(tester);
    });
  });

  group('Inspector', () {
    const rect = LayoutObjectModel(
      id: 'r1',
      type: 'rect',
      x: 100,
      y: 120,
      width: 160,
      height: 80,
      style: LayoutObjectStyle(borderColor: '#9E9E9E', fillColor: '#FFFFFF'),
    );

    testWidgets('line (border) color and "no fill" can be set from swatches', (tester) async {
      final last = await _pumpDesigner(tester, objects: [rect]);
      await tester.tap(find.byType(LayoutShapeView)); // select the rectangle on the canvas
      await tester.pump();
      await tester.tap(find.byIcon(Icons.palette_outlined).first);
      await tester.pumpAndSettle();

      // Second "Red" swatch belongs to LINE (BORDER) COLOR (the first one is FILL).
      await tester.tap(find.byTooltip('Red').at(1));
      await tester.pump();
      expect(last()!.objects.single.style.borderColor, '#EF4444');

      await tester.tap(find.byTooltip('None / Transparent').first);
      await tester.pump();
      expect(last()!.objects.single.style.fillColor, isNull);
      await _settleAutoSave(tester);
    });

    testWidgets('typing in the inspector text field keeps the cursor and does not delete the object', (tester) async {
      const label = LayoutObjectModel(id: 'l1', type: 'label', x: 100, y: 120, width: 160, height: 26);
      final last = await _pumpDesigner(tester, objects: [label]);
      await tester.tap(find.text('(Label)'));
      await tester.pump();
      await tester.tap(find.byIcon(Icons.text_fields).first);
      await tester.pumpAndSettle();

      final textField = find.widgetWithText(TextField, 'Text');
      await tester.tap(textField);
      await tester.enterText(textField, 'Hola');
      await tester.pump();
      expect(last()!.objects.single.text, 'Hola');

      // Backspace inside the text field edits the text, it must not delete the label.
      await tester.sendKeyEvent(LogicalKeyboardKey.backspace);
      await tester.pump();
      expect(last()!.objects, hasLength(1));
      await _settleAutoSave(tester);
    });

    testWidgets('double click edits the text of a shape in place', (tester) async {
      final last = await _pumpDesigner(tester, objects: [rect]);
      final center = tester.getCenter(find.byType(LayoutShapeView));
      await tester.tapAt(center);
      await tester.pump(const Duration(milliseconds: 50));
      await tester.tapAt(center);
      await tester.pump();

      final editor = find.descendant(of: find.byType(LayoutDesignerWidget), matching: find.byType(EditableText));
      expect(editor, findsWidgets);
      await tester.pump(); // the inline editor takes the focus after the frame
      tester.testTextInput.enterText('Inside the box');
      await tester.pump();
      await tester.tapAt(const Offset(900, 700)); // click elsewhere on the canvas commits
      await tester.pump();
      expect(last()!.objects.single.text, 'Inside the box');
      expect(find.text('Inside the box'), findsOneWidget);
      await _settleAutoSave(tester);
    });
  });

  group('Set Tab Order', () {
    const a = LayoutObjectModel(id: 'a', type: 'field', x: 100, y: 100, width: 120, height: 30,
        fieldBinding: FieldBindingModel(fieldName: 'first_name'));
    const b = LayoutObjectModel(id: 'b', type: 'field', x: 100, y: 200, width: 120, height: 30,
        fieldBinding: FieldBindingModel(fieldName: 'city'));
    const c = LayoutObjectModel(id: 'c', type: 'button', x: 300, y: 100, width: 100, height: 30, text: 'Go');

    test('explicit tab order wins, the rest follow in reading order', () {
      final sorted = sortLayoutTabStops([a, b, c.copyWith(tabOrder: 1)]);
      expect(sorted.map((o) => o.id), ['c', 'a', 'b']);
      expect(sortLayoutTabStops([b, c, a]).map((o) => o.id), ['a', 'c', 'b']);
    });

    testWidgets('clicking objects in Set Tab Order mode numbers them in click order', (tester) async {
      final last = await _pumpDesigner(tester, objects: [a, b, c], tool: LayoutTool.tabOrder);
      expect(find.textContaining('Set Tab Order: click'), findsOneWidget);
      // Click b, then c, then a.
      await tester.tap(find.text(':: city'));
      await tester.pump();
      await tester.tap(find.text('Go'));
      await tester.pump();
      await tester.tap(find.text(':: first_name'));
      await tester.pump();
      final order = {for (final o in last()!.objects) o.id: o.tabOrder};
      expect(order, {'b': 1, 'c': 2, 'a': 3});
      await _settleAutoSave(tester);
    });
  });

  group('Insert menu and merge text', () {
    testWidgets('Insert > Current Date adds a merge symbol label', (tester) async {
      final key = GlobalKey<LayoutDesignerWidgetState>();
      final last = await _pumpDesigner(tester, key: key);
      key.currentState!.insertText(LayoutMergeSymbols.currentDate);
      await tester.pump();
      expect(last()!.objects.single.text, '{{CurrentDate}}');
      await _settleAutoSave(tester);
    });

    test('merge symbols resolve to record values, date and user', () {
      final text = resolveLayoutMergeText('{{first_name}} / {{CurrentUser}} / {{CurrentDate}} / {{missing}}',
          record: {'first_name': 'Ana'}, userName: 'mario', now: DateTime(2026, 10, 6));
      expect(text, 'Ana / mario / 2026-10-06 / {{missing}}');
    });

    testWidgets('a picture inserted in a shape is drawn inside it', (tester) async {
      const shape = LayoutObjectModel(
        id: 's',
        type: 'rounded_rect',
        x: 0,
        y: 0,
        width: 100,
        height: 60,
        media: LayoutMediaModel(kind: 'image', name: 'logo.png', mimeType: 'image/png', data: _pngBase64),
      );
      await tester.pumpWidget(MaterialApp(home: Center(child: SizedBox(width: 100, height: 60, child: buildDrawnLayoutObject(shape)))));
      expect(find.byType(Image), findsOneWidget);
    });

    test('layout objects keep action, tab order and media through JSON', () {
      const obj = LayoutObjectModel(
        id: 'x',
        type: 'button',
        x: 1,
        y: 2,
        width: 3,
        height: 4,
        tabOrder: 2,
        action: ButtonActionModel.performScript(id: 's1', name: 'Archive', parameter: 'p'),
        media: LayoutMediaModel(kind: 'pdf', name: 'a.pdf', data: 'AAAA'),
      );
      final back = LayoutObjectModel.fromJson(obj.toJson());
      expect(back.tabOrder, 2);
      expect(back.action!.isPerformScript, isTrue);
      expect(back.action!.scriptName, 'Archive');
      expect(back.action!.parameter, 'p');
      expect(back.media!.kind, 'pdf');
      expect(back.copyWith(clearAction: true, clearTabOrder: true).action, isNull);
    });
  });

  group('Button actions', () {
    Future<GlobalKey<LayoutDesignerWidgetState>> pumpButtonLayout(
        WidgetTester tester, _MutableScriptsApi api, void Function(LayoutDefinitionModel) onChanged) async {
      tester.view.physicalSize = const Size(1600, 1000);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);
      final key = GlobalKey<LayoutDesignerWidgetState>();
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: LayoutDesignerWidget(
            key: key,
            table: _table,
            apiClient: api,
            initialLayout: const LayoutDefinitionModel(id: 'l1', name: 'L', tableOccurrence: 'contacts', objects: [
              LayoutObjectModel(id: 'b', type: 'button', x: 100, y: 100, width: 120, height: 34, text: 'Nuevo'),
            ]),
            onSaved: () {},
            onLayoutChanged: onChanged,
            // The Script Workspace creates a script while it is open.
            onManageScripts: () async => api.scripts.add(const ScriptModel(id: 'srv-1', name: 'Nuevo registro')),
          ),
        ),
      ));
      await tester.pumpAndSettle();
      return key;
    }

    Future<void> openButtonSetup(WidgetTester tester) async {
      final center = tester.getCenter(find.text('Nuevo'));
      await tester.tapAt(center);
      await tester.pump(const Duration(milliseconds: 50));
      await tester.tapAt(center);
      await tester.pumpAndSettle();
      expect(find.text('Button Setup'), findsOneWidget);
    }

    testWidgets('double click on a button opens Button Setup and assigns a Script Workspace script', (tester) async {
      final api = _MutableScriptsApi();
      LayoutDefinitionModel? last;
      await pumpButtonLayout(tester, api, (l) => last = l);
      expect(find.text('No action (double click to set up)'), findsOneWidget);

      await openButtonSetup(tester);
      await tester.tap(find.text('Perform a script'));
      await tester.pumpAndSettle();
      expect(find.textContaining('There are no scripts yet'), findsOneWidget);

      // Scripts created meanwhile in the Script Workspace show up when it closes.
      await tester.tap(find.text('Edit scripts...'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(InputDecorator, 'Script'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Nuevo registro  (0 steps)').last);
      await tester.pumpAndSettle();
      await tester.tap(find.text('OK'));
      await tester.pumpAndSettle();

      final action = last!.objects.single.action!;
      expect(action.isPerformScript, isTrue);
      expect(action.scriptId, 'srv-1');
      expect(find.text('Perform Script: Nuevo registro'), findsWidgets);
      await _settleAutoSave(tester);
    });

    testWidgets('"New script" in Button Setup creates the script and assigns it', (tester) async {
      final api = _MutableScriptsApi();
      LayoutDefinitionModel? last;
      await pumpButtonLayout(tester, api, (l) => last = l);
      await openButtonSetup(tester);
      await tester.tap(find.text('Perform a script'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('New script...'));
      await tester.pumpAndSettle();
      await tester.enterText(find.widgetWithText(TextField, 'Script name'), 'Alta de cliente');
      await tester.tap(find.text('Create'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('OK'));
      await tester.pumpAndSettle();
      final action = last!.objects.single.action!;
      expect(action.scriptName, 'Alta de cliente');
      expect(api.scripts.map((s) => s.name), contains('Alta de cliente'));
      await _settleAutoSave(tester);
    });

    test('a single step action runs on the host', () async {
      final host = _FakeHost();
      final runner = LayoutActionRunner(apiClient: ApiClient(baseUrl: 'http://localhost:1'), host: host);
      final result = await runner.run(const ButtonActionModel.singleStep('go_to_record', params: {'target': 'last'}));
      expect(result.completed, isTrue);
      expect(host.calls, ['goto:last']);
    });

    test('Perform Script runs the script steps in order and stops at unsupported steps', () async {
      final host = _FakeHost();
      final api = _FakeScriptsApi(const ScriptModel(id: 's1', name: 'Save and next', steps: [
        ScriptStepModel(id: '2', sequenceIdx: 2, stepType: 'go_to_record', params: {'target': 'Siguiente'}),
        ScriptStepModel(id: '1', sequenceIdx: 1, stepType: 'commit_records'),
        ScriptStepModel(id: '3', sequenceIdx: 3, stepType: 'set_field', params: {'field': 'city', 'value': '{{first_name}}'}),
        ScriptStepModel(id: '4', sequenceIdx: 4, stepType: 'comment', isEnabled: false),
        ScriptStepModel(id: '5', sequenceIdx: 5, stepType: 'if', params: {'condition': 'x'}),
        ScriptStepModel(id: '6', sequenceIdx: 6, stepType: 'new_record'),
      ]));
      final result = await LayoutActionRunner(apiClient: api, host: host)
          .run(const ButtonActionModel.performScript(id: 's1', name: 'Save and next'));
      expect(host.calls, ['commit', 'goto:Siguiente', 'set:city=resolved({{first_name}})']);
      expect(result.completed, isFalse);
      expect(result.message, contains('"if"'));
    });
  });
}

class _FakeScriptsApi extends ApiClient {
  _FakeScriptsApi(this.script) : super(baseUrl: 'http://localhost:1');
  final ScriptModel script;

  @override
  Future<ScriptModel> getScript(String id, {String? database}) async => script;

  @override
  Future<List<ScriptModel>> listScripts({String? database}) async => [script];
}

class _MutableScriptsApi extends ApiClient {
  _MutableScriptsApi() : super(baseUrl: 'http://localhost:1');
  final scripts = <ScriptModel>[];

  @override
  Future<List<ScriptModel>> listScripts({String? database}) async => List.of(scripts);

  @override
  Future<ScriptModel> createScript(Map<String, dynamic> data, {String? database}) async {
    final created = ScriptModel(id: 'created-${scripts.length + 1}', name: data['name'] as String);
    scripts.add(created);
    return created;
  }
}

class _FakeHost implements LayoutActionHost {
  final calls = <String>[];

  @override
  String actionResolveText(String text) => text.contains('{{') ? 'resolved($text)' : text;
  @override
  Future<void> actionNewRecord() async => calls.add('new');
  @override
  Future<void> actionDuplicateRecord() async => calls.add('duplicate');
  @override
  Future<void> actionDeleteRecord({required bool confirm}) async => calls.add('delete');
  @override
  Future<void> actionCommitRecord() async => calls.add('commit');
  @override
  Future<void> actionRevertRecord() async => calls.add('revert');
  @override
  Future<void> actionGoToRecord(String target) async => calls.add('goto:$target');
  @override
  Future<void> actionEnterFindMode() async => calls.add('find');
  @override
  Future<void> actionPerformFind() async => calls.add('perform_find');
  @override
  Future<void> actionShowAllRecords() async => calls.add('show_all');
  @override
  Future<void> actionEnterPreviewMode() async => calls.add('preview');
  @override
  Future<bool> actionGoToLayout(String layoutName) async {
    calls.add('layout:$layoutName');
    return true;
  }

  @override
  Future<bool> actionSetField(String field, String value) async {
    calls.add('set:$field=$value');
    return true;
  }

  @override
  Future<void> actionShowDialog(String title, String message) async => calls.add('dialog:$title');
  @override
  Future<void> actionOpenUrl(String url) async => calls.add('url:$url');
  @override
  Future<void> actionEnterBrowseMode() async => calls.add('browse');
  @override
  Future<void> actionAdjustWindow(String windowMode) async => calls.add('window:$windowMode');
  @override
  Future<void> actionReLogin() async => calls.add('re_login');
  @override
  Future<void> actionCloseFile() async => calls.add('close_file');
  @override
  Future<void> actionExitApplication() async => calls.add('exit_app');
}
