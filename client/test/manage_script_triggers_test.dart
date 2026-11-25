// #26 — File > Manage > Script Triggers lists every trigger a migration
// imported switched off, grouped by layout, and enables the ones nothing
// keeps from running, after saying how many and what that means. A layout
// keeps its stored triggers when saved, and a switched-off trigger never
// runs.

import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/layout_designer_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/schema_manager/manage_script_triggers_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _missing = ScriptTriggerProblem(
  code: ScriptTriggerProblem.unresolvedScript,
  message: 'The script "Gone" this trigger runs is not in this solution.',
  remedy: 'Import or recreate the script, or point the trigger at another one.',
);
const _objectEvent = ScriptTriggerProblem(
  code: ScriptTriggerProblem.unsupportedEvent,
  message: 'File4Base does not run OnObjectSave triggers yet.',
  remedy: 'Keep it disabled; it is kept for when File4Base runs this event.',
);

List<ScriptTriggerModel> _triggers({bool enterOn = false}) => [
      ScriptTriggerModel(
        id: 't-enter',
        layoutId: 'l-card',
        layoutName: 'Plant Card',
        event: 'OnLayoutEnter',
        scriptId: 's-greet',
        scriptName: 'Greet',
        parameter: '"hello"',
        enabled: enterOn,
        quarantinedByMigration: !enterOn,
        canEnable: !enterOn,
      ),
      const ScriptTriggerModel(
        id: 't-exit',
        layoutId: 'l-card',
        layoutName: 'Plant Card',
        event: 'OnLayoutExit',
        scriptName: 'Gone',
        quarantinedByMigration: true,
        problems: [_missing],
      ),
      const ScriptTriggerModel(
        id: 't-save',
        layoutId: 'l-list',
        layoutName: 'Plant List',
        event: 'OnObjectSave',
        scope: 'object',
        objectId: 'obj-1',
        objectLabel: 'Plants::Height',
        objectType: 'Edit Box',
        scriptId: 's-water',
        scriptName: 'Water',
        quarantinedByMigration: true,
        problems: [_objectEvent],
      ),
      const ScriptTriggerModel(
        id: 't-exit-list',
        layoutId: 'l-list',
        layoutName: 'Plant List',
        event: 'OnLayoutExit',
        scriptId: 's-water',
        scriptName: 'Water',
        quarantinedByMigration: true,
        canEnable: true,
      ),
    ];

class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');

  List<ScriptTriggerModel> triggers = _triggers();
  final List<(List<String>, bool)> calls = [];
  bool refuse = false;

  @override
  Future<List<ScriptTriggerModel>> listScriptTriggers() async => triggers;

  @override
  Future<ScriptTriggerUpdate> setScriptTriggersEnabled(List<String> ids, {required bool enabled}) async {
    calls.add((ids, enabled));
    if (refuse) {
      throw ApiException(statusCode: 409, title: 'Script Trigger Cannot Be Enabled', detail: 'one of them cannot run');
    }
    triggers = [
      for (final t in triggers)
        ids.contains(t.id)
            ? ScriptTriggerModel(
                id: t.id,
                layoutId: t.layoutId,
                layoutName: t.layoutName,
                event: t.event,
                scriptId: t.scriptId,
                scriptName: t.scriptName,
                enabled: enabled,
                canEnable: !enabled,
              )
            : t,
    ];
    return ScriptTriggerUpdate(updated: ids.length, layouts: const ['l-card']);
  }
}

/// The server of Layout Setup: switching a trigger goes through the review
/// API, and the layout is read back with the trigger in its new place.
class _ScriptsApi extends ApiClient {
  _ScriptsApi(this.stored) : super(baseUrl: 'http://localhost:1');

  Map<String, dynamic> stored;
  final switched = <(List<String>, bool)>[];
  final saved = <Map<String, dynamic>>[];

  @override
  Future<List<ScriptModel>> listScripts({String? database}) async => const [ScriptModel(id: 's1', name: 'Greet')];

  @override
  Future<LayoutModel> updateLayout(String id, String name, Map<String, dynamic> definition) async {
    saved.add(definition);
    return LayoutModel(id: id, name: name, tableOccurrenceId: 'tbl', definition: definition);
  }

  @override
  Future<ScriptTriggerUpdate> setScriptTriggersEnabled(List<String> ids, {required bool enabled}) async {
    switched.add((ids, enabled));
    final layout = LayoutDefinitionModel.fromJson(stored);
    stored = (enabled ? layout.enableStoredTrigger(ids.single) : layout).toJson();
    return ScriptTriggerUpdate(updated: ids.length, layouts: const ['l-card']);
  }

  @override
  Future<LayoutModel> getLayout(String id) async =>
      LayoutModel(id: id, name: 'Plant Card', tableOccurrenceId: 'tbl', definition: stored);
}

Finder _inRow(String id, String text) =>
    find.descendant(of: find.byKey(ValueKey('trigger-row-$id')), matching: find.text(text));

Future<ScriptTriggerReviewResult?> _open(WidgetTester tester, _Api api, {bool canEdit = true}) async {
  tester.view.physicalSize = const Size(1600, 1200);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  ScriptTriggerReviewResult? result;
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: Builder(
        builder: (context) => TextButton(
          onPressed: () async => result = await ManageScriptTriggersDialog.show(context, apiClient: api, canEdit: canEdit),
          child: const Text('open'),
        ),
      ),
    ),
  ));
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
  return result;
}

void main() {
  testWidgets('lists the triggers grouped by layout, with their status and problems', (tester) async {
    final api = _Api();
    await _open(tester, api);

    expect(find.text('Manage Script Triggers'), findsOneWidget);
    expect(find.byKey(const ValueKey('trigger-layout-l-card')), findsOneWidget);
    expect(find.byKey(const ValueKey('trigger-layout-l-list')), findsOneWidget);
    for (final id in ['t-enter', 't-exit', 't-save', 't-exit-list']) {
      expect(find.byKey(ValueKey('trigger-row-$id')), findsOneWidget);
    }
    expect(_inRow('t-enter', 'Disabled by migration'), findsOneWidget);
    expect(_inRow('t-exit-list', 'Disabled by migration'), findsOneWidget);
    expect(_inRow('t-exit', 'Problem: Script not found'), findsOneWidget);
    expect(_inRow('t-save', 'Problem: Event not run'), findsOneWidget);
    expect(_inRow('t-save', 'Plants::Height (Edit Box)'), findsOneWidget);
    expect(_inRow('t-enter', '(layout)'), findsOneWidget);

    // A problem reads in plain words, with what to do.
    await tester.tap(find.byKey(const ValueKey('trigger-row-t-exit')));
    await tester.pumpAndSettle();
    final detail = find.byKey(const ValueKey('trigger-detail'));
    expect(find.descendant(of: detail, matching: find.textContaining('is not in this solution', findRichText: true)), findsOneWidget);
    expect(find.descendant(of: detail, matching: find.textContaining('What to do', findRichText: true)), findsOneWidget);
  });

  testWidgets('filters by status and searches', (tester) async {
    final api = _Api();
    await _open(tester, api);

    await tester.tap(find.byKey(const ValueKey('trigger-status-filter')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('With a problem').last);
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('trigger-row-t-enter')), findsNothing);
    expect(find.byKey(const ValueKey('trigger-row-t-exit')), findsOneWidget);
    expect(find.byKey(const ValueKey('trigger-row-t-save')), findsOneWidget);

    await tester.enterText(find.byKey(const ValueKey('trigger-search')), 'height');
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('trigger-row-t-exit')), findsNothing);
    expect(find.byKey(const ValueKey('trigger-row-t-save')), findsOneWidget);
  });

  testWidgets('enables every trigger without a problem after confirming how many', (tester) async {
    final api = _Api();
    await _open(tester, api);

    await tester.tap(find.byKey(const ValueKey('trigger-enable-all')));
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('trigger-enable-confirm')), findsOneWidget);
    expect(find.textContaining('Enable 2 triggers?'), findsOneWidget);
    expect(find.textContaining('their scripts run on user actions'), findsOneWidget);

    // Cancelling changes nothing.
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(api.calls, isEmpty);

    await tester.tap(find.byKey(const ValueKey('trigger-enable-all')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('trigger-enable-confirm-ok')));
    await tester.pumpAndSettle();
    expect(api.calls, hasLength(1));
    expect(api.calls.single.$1, unorderedEquals(['t-enter', 't-exit-list']), reason: 'only the triggers that can run');
    expect(api.calls.single.$2, isTrue);
    expect(_inRow('t-enter', 'Enabled'), findsOneWidget);
    expect(_inRow('t-exit-list', 'Enabled'), findsOneWidget);
    expect(find.textContaining('2 triggers enabled.'), findsOneWidget);
  });

  testWidgets('enables and disables a selection; a refused batch says nothing changed', (tester) async {
    final api = _Api();
    await _open(tester, api);

    await tester.tap(find.byKey(const ValueKey('trigger-check-t-enter')));
    await tester.tap(find.byKey(const ValueKey('trigger-check-t-exit')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('trigger-enable-selected')));
    await tester.pumpAndSettle();
    expect(find.textContaining('Enable 1 trigger?'), findsOneWidget, reason: 'the one with a problem is left out');
    await tester.tap(find.byKey(const ValueKey('trigger-enable-confirm-ok')));
    await tester.pumpAndSettle();
    expect(api.calls.single.$1, ['t-enter']);
    expect(find.textContaining('1 with a problem stayed disabled'), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('trigger-check-t-enter')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('trigger-disable-selected')));
    await tester.pumpAndSettle();
    expect(api.calls.last.$1, ['t-enter']);
    expect(api.calls.last.$2, isFalse);

    api.refuse = true;
    await tester.tap(find.byKey(const ValueKey('trigger-check-t-exit-list')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('trigger-enable-selected')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('trigger-enable-confirm-ok')));
    await tester.pumpAndSettle();
    expect(find.textContaining('Nothing was changed'), findsOneWidget);
  });

  testWidgets('a user who cannot edit reviews without switching', (tester) async {
    await _open(tester, _Api(), canEdit: false);
    expect(find.byKey(const ValueKey('trigger-enable-all')), findsNothing);
    expect(find.byKey(const ValueKey('trigger-enable-selected')), findsNothing);
    expect(find.byKey(const ValueKey('trigger-row-t-enter')), findsOneWidget);
  });

  testWidgets('a row opens its script, or its object in Layout mode', (tester) async {
    final api = _Api();
    ScriptTriggerReviewResult? result;
    tester.view.physicalSize = const Size(1600, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => TextButton(
            onPressed: () async => result = await ManageScriptTriggersDialog.show(context, apiClient: api),
            child: const Text('open'),
          ),
        ),
      ),
    ));
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('trigger-open-layout-t-save')));
    await tester.pumpAndSettle();
    expect(result?.openLayout?.layoutId, 'l-list');
    expect(result?.openLayout?.objectId, 'obj-1');

    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('trigger-open-script-t-enter')));
    await tester.pumpAndSettle();
    expect(result?.openScript?.scriptId, 's-greet');
  });

  test('the API client lists and switches triggers', () async {
    final requests = <http.Request>[];
    final api = ApiClient(
      baseUrl: 'http://test-server:8080',
      httpClient: MockClient((req) async {
        requests.add(req);
        if (req.method == 'GET') {
          return http.Response(
              jsonEncode({
                'triggers': [
                  {
                    'id': 't1',
                    'layout_id': 'l1',
                    'layout_name': 'Card',
                    'event': 'OnRecordLoad',
                    'enabled': false,
                    'quarantined_by_migration': true,
                    'problems': [
                      {'code': 'unsupported_event', 'message': 'not run', 'remedy': 'keep it'},
                    ],
                  },
                ],
              }),
              200);
        }
        return http.Response(jsonEncode({'updated': 1, 'layouts': ['l1']}), 200);
      }),
    );
    final listed = await api.listScriptTriggers();
    expect(listed.single.statusLabel, 'Problem: Event not run');
    expect(listed.single.canEnable, isFalse);
    final update = await api.setScriptTriggersEnabled(['t1'], enabled: true);
    expect(update.updated, 1);
    expect(requests.last.method, 'PUT');
    expect(requests.last.url.path, '/api/v1/schemas/script-triggers');
    expect(jsonDecode(requests.last.body), {'ids': ['t1'], 'enabled': true});
  });

  testWidgets('Layout Setup shows the migrated triggers with a switch, and links to the review', (tester) async {
    tester.view.physicalSize = const Size(1600, 1400);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    const table = TableModel(id: 'tbl', name: 'plants', displayName: 'Plants', columns: [
      ColumnModel(id: 'c1', tableId: 'tbl', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
    ]);
    final definition = {
      'id': 'l-card',
      'name': 'Plant Card',
      'table_occurrence': 'plants',
      'objects': <dynamic>[],
      'script_triggers': [
        {'id': 'm1', 'event': 'OnLayoutEnter', 'type': 'perform_script', 'script_id': 's1', 'script_name': 'Greet', 'enabled': false, 'quarantined_by_migration': true},
        {'id': 'm2', 'event': 'OnRecordLoad', 'type': 'perform_script', 'script_id': 's1', 'script_name': 'Greet', 'enabled': false, 'quarantined_by_migration': true},
      ],
    };
    final api = _ScriptsApi(definition);
    String? reviewed;
    final triggersChanged = <String>[];
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: LayoutDesignerWidget(
          table: table,
          tables: const [table],
          apiClient: api,
          initialLayout: LayoutDefinitionModel.fromJson(definition),
          onSaved: () {},
          onReviewScriptTriggers: (id) => reviewed = id,
          onScriptTriggersChanged: triggersChanged.add,
          canSwitchScriptTriggers: true,
        ),
      ),
    ));
    await tester.pumpAndSettle();

    final enter = find.byKey(const ValueKey('trigger-switch-stored-m1'));
    final load = find.byKey(const ValueKey('trigger-switch-stored-m2'));
    await tester.ensureVisible(enter);
    expect(tester.widget<Switch>(enter).value, isFalse);
    expect(tester.widget<Switch>(load).onChanged, isNull, reason: 'File4Base does not run OnRecordLoad');
    expect(find.textContaining('Disabled by migration'), findsNWidgets(2));

    await tester.tap(enter);
    await tester.pumpAndSettle();
    expect(api.switched, hasLength(1), reason: 'switched by the admin-only review API');
    expect(api.switched.single.$1, ['m1']);
    expect(api.switched.single.$2, isTrue);
    for (final saved in api.saved) {
      expect(LayoutDefinitionModel.fromJson(saved).activeOnLayoutEnter, isNull,
          reason: 'the layout update never carries the enabling');
    }
    expect(triggersChanged, ['l-card']);
    expect(find.byKey(const ValueKey('trigger-switch-stored-m1')), findsNothing, reason: 'now in its slot');

    final review = find.byKey(const ValueKey('review-script-triggers'));
    await tester.ensureVisible(review);
    await tester.tap(review);
    expect(reviewed, 'l-card');
  });

  testWidgets('a user who is not an owner or an administrator cannot switch a trigger in Layout Setup', (tester) async {
    tester.view.physicalSize = const Size(1600, 1400);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    const table = TableModel(id: 'tbl', name: 'plants', displayName: 'Plants', columns: [
      ColumnModel(id: 'c1', tableId: 'tbl', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
    ]);
    final definition = {
      'id': 'l-card',
      'name': 'Plant Card',
      'objects': <dynamic>[],
      'script_triggers': [
        {'id': 'm1', 'event': 'OnLayoutEnter', 'type': 'perform_script', 'script_id': 's1', 'script_name': 'Greet', 'enabled': false, 'quarantined_by_migration': true},
      ],
    };
    final api = _ScriptsApi(definition);
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: LayoutDesignerWidget(
          table: table,
          tables: const [table],
          apiClient: api,
          initialLayout: LayoutDefinitionModel.fromJson(definition),
          onSaved: () {},
        ),
      ),
    ));
    await tester.pumpAndSettle();
    final enter = find.byKey(const ValueKey('trigger-switch-stored-m1'));
    await tester.ensureVisible(enter);
    expect(tester.widget<Switch>(enter).onChanged, isNull);
    expect(find.textContaining('Only an owner or an administrator'), findsOneWidget);
  });

  testWidgets('closing the review after a change reports the layouts it changed', (tester) async {
    final api = _Api();
    ScriptTriggerReviewResult? result;
    var closed = false;
    tester.view.physicalSize = const Size(1600, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => TextButton(
            onPressed: () async {
              result = await ManageScriptTriggersDialog.show(context, apiClient: api);
              closed = true;
            },
            child: const Text('open'),
          ),
        ),
      ),
    ));
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('trigger-check-t-enter')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('trigger-enable-selected')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('trigger-enable-confirm-ok')));
    await tester.pumpAndSettle();

    // Dismissed with Escape, not with its Close button.
    await tester.sendKeyEvent(LogicalKeyboardKey.escape);
    await tester.pumpAndSettle();
    expect(closed, isTrue);
    expect(result?.changed, isTrue);
    expect(result?.changedLayouts, {'l-card'});
  });

  group('layout definition', () {
    final stored = {
      'objects': <dynamic>[],
      'on_layout_exit': {'type': 'perform_script', 'script_id': 's2', 'script_name': 'Bye', 'enabled': false},
      'script_triggers': [
        {
          'id': 'm1',
          'event': 'OnLayoutEnter',
          'type': 'perform_script',
          'script_id': 's1',
          'script_name': 'Greet',
          'enabled': false,
          'quarantined_by_migration': true,
          'future_key': 42,
        },
        {'id': 'm2', 'event': 'OnObjectSave', 'type': 'perform_script', 'script_id': 's1', 'object_id': 'o1', 'enabled': false},
      ],
    };

    test('keeps stored triggers and their unknown keys when saved', () {
      final layout = LayoutDefinitionModel.fromJson(stored);
      final json = layout.toJson();
      final list = json['script_triggers'] as List;
      expect(list, hasLength(2));
      expect((list.first as Map)['future_key'], 42);
      expect((list.first as Map)['quarantined_by_migration'], isTrue);
      expect((json['on_layout_exit'] as Map)['enabled'], isFalse);
    });

    test('a switched-off trigger is not the one to run', () {
      final layout = LayoutDefinitionModel.fromJson(stored);
      expect(layout.onLayoutExit, isNotNull);
      expect(layout.activeOnLayoutExit, isNull);
      expect(layout.activeOnLayoutEnter, isNull);
      expect(layout.copyWith(onLayoutEnter: const ButtonActionModel.performScript(id: 's1', name: 'Greet')).activeOnLayoutEnter,
          isNotNull, reason: 'an action that predates the flag runs');
    });

    test('enabling moves a stored trigger into its slot; disabling moves it back', () {
      final layout = LayoutDefinitionModel.fromJson(stored).copyWith(clearOnLayoutExit: true);
      expect(layout.layoutLevelTriggers.map((t) => t.triggerId), ['m1']);
      expect(layout.triggersOfObject('o1').map((t) => t.triggerId), ['m2']);
      expect(layout.whyTriggerCannotRun(layout.scriptTriggers.first, scriptIds: {'s1'}), isNull);
      expect(layout.whyTriggerCannotRun(layout.scriptTriggers.first, scriptIds: {'other'}), contains('not in this solution'));
      expect(layout.whyTriggerCannotRun(layout.scriptTriggers.last), contains('layout objects'));

      final on = layout.enableStoredTrigger('m1');
      expect(on.activeOnLayoutEnter?.scriptId, 's1');
      expect(on.onLayoutEnter!.quarantinedByMigration, isFalse);
      expect(on.scriptTriggers.map((t) => t.triggerId), ['m2']);

      final off = on.disableSlotTrigger(enter: true);
      expect(off.onLayoutEnter, isNull);
      final back = off.scriptTriggers.where((t) => t.triggerId == 'm1').single;
      expect(back.enabled, isFalse);
      expect(back.event, 'OnLayoutEnter');
    });
  });
}
