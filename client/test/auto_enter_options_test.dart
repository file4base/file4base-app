// Auto-Enter field options: the Field Options dialog, the Lookup dialog,
// Browse mode's prohibit-modification rule, the script steps, and the API
// calls behind them. Synthetic data only.

import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/schema_manager/field_options_dialog.dart';
import 'package:file4base_client/features/schema_manager/lookup_options_dialog.dart';
import 'package:file4base_client/features/script_workspace/calculation_builder_dialog.dart';
import 'package:file4base_client/main.dart' show OperationalMode, apiClientProvider;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

// ─── Field Options dialog ────────────────────────────────────────────────────

const _items = TableModel(id: 't1', name: 'items', displayName: 'Items', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'code', displayName: 'Code', fieldType: 'TEXT'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'vendor_id', displayName: 'Vendor ID', fieldType: 'TEXT'),
  ColumnModel(id: 'c3', tableId: 't1', name: 'qty', displayName: 'Qty', fieldType: 'NUMBER'),
]);

const _vendors = TableModel(id: 't2', name: 'vendors', displayName: 'Vendors', columns: [
  ColumnModel(id: 'v0', tableId: 't2', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'v1', tableId: 't2', name: 'vendor_id', displayName: 'Vendor ID', fieldType: 'TEXT'),
  ColumnModel(id: 'v2', tableId: 't2', name: 'label', displayName: 'Label', fieldType: 'TEXT'),
]);

const _notes = TableModel(id: 't3', name: 'notes', displayName: 'Notes', columns: [
  ColumnModel(id: 'n1', tableId: 't3', name: 'body', displayName: 'Body', fieldType: 'TEXT'),
]);

const _occurrences = [
  TableOccurrenceModel(id: 'o1', baseTableId: 't1', name: 'items', xPos: 0, yPos: 0),
  TableOccurrenceModel(id: 'o2', baseTableId: 't2', name: 'vendors', xPos: 0, yPos: 0),
  TableOccurrenceModel(id: 'o3', baseTableId: 't3', name: 'notes', xPos: 0, yPos: 0),
  TableOccurrenceModel(id: 'o4', baseTableId: 't1', name: 'items_copy', xPos: 0, yPos: 0),
];

const _relationships = [
  RelationshipModel(
    id: 'r1',
    name: '',
    leftOccurrenceId: 'o1',
    leftColumnId: 'c2',
    rightOccurrenceId: 'o2',
    rightColumnId: 'v1',
  ),
];

class _SchemaApi extends ApiClient {
  _SchemaApi() : super(baseUrl: 'http://localhost:1');
  final saved = <Map<String, dynamic>>[];

  @override
  Future<List<TableOccurrenceModel>> listOccurrences() async => _occurrences;

  @override
  Future<List<RelationshipModel>> listRelationships() async => _relationships;

  @override
  Future<List<TableModel>> listTables() async => const [_items, _vendors, _notes];

  @override
  Future<ColumnModel> updateColumn(
    String tableId,
    String columnId, {
    required String displayName,
    String? defaultValue,
    bool updateDefaultValue = false,
    String? calculationFormula,
    bool updateCalculationFormula = false,
    String? validationRules,
    bool updateValidationRules = false,
    int? maxRepetitions,
  }) async {
    saved.add({
      'default_value': defaultValue,
      'calculation_formula': calculationFormula,
      'update_calculation_formula': updateCalculationFormula,
    });
    final column = _items.columns.firstWhere((c) => c.id == columnId);
    return ColumnModel(
      id: column.id,
      tableId: column.tableId,
      name: column.name,
      displayName: column.displayName,
      fieldType: column.fieldType,
      defaultValue: defaultValue,
      calculationFormula: calculationFormula,
      validationRules: validationRules,
    );
  }

  Map<String, dynamic> get lastOptions =>
      jsonDecode(saved.last['default_value'] as String) as Map<String, dynamic>;
}

ColumnModel _column(String name, {String? defaultValue, String? calculationFormula}) {
  final c = _items.columns.firstWhere((c) => c.name == name);
  return ColumnModel(
    id: c.id,
    tableId: c.tableId,
    name: c.name,
    displayName: c.displayName,
    fieldType: c.fieldType,
    defaultValue: defaultValue,
    calculationFormula: calculationFormula,
  );
}

Future<_SchemaApi> _openDialog(WidgetTester tester, ColumnModel column) async {
  tester.view.physicalSize = const Size(1600, 1200);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final api = _SchemaApi();
  await tester.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(api)],
    child: MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => TextButton(
            onPressed: () => FieldOptionsDialog.show(context, table: _items, column: column),
            child: const Text('open'),
          ),
        ),
      ),
    ),
  ));
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
  return api;
}

/// Taps what may be below the fold of the Auto-Enter tab.
Future<void> _tap(WidgetTester tester, Finder finder) async {
  // Typing scrolls the caret into view once the frame settles; wait for it
  // so it does not scroll the target back out.
  await tester.pumpAndSettle();
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

Future<void> _ok(WidgetTester tester) async {
  await tester.tap(find.widgetWithText(FilledButton, 'OK'));
  await tester.pumpAndSettle();
}

Future<void> _choose(WidgetTester tester, Key dropdown, String item) async {
  await _tap(tester, find.byKey(dropdown));
  await tester.tap(find.text(item).last);
  await tester.pumpAndSettle();
}

// ─── Browse mode and the script host ─────────────────────────────────────────

const _tickets = TableModel(id: 't9', name: 'tickets', displayName: 'Tickets', columns: [
  ColumnModel(id: 'k0', tableId: 't9', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'k1', tableId: 't9', name: 'title', displayName: 'Title', fieldType: 'TEXT'),
  ColumnModel(
    id: 'k2',
    tableId: 't9',
    name: 'ref',
    displayName: 'Ref',
    fieldType: 'TEXT',
    defaultValue: '{"serial_enabled":true,"serial_next":"T-0001","prohibit_modification":true}',
  ),
]);

class _BrowseApi extends ApiClient {
  _BrowseApi() : super(baseUrl: 'http://localhost:1');
  final rows = <Map<String, dynamic>>[
    {'id': '1', 'title': 'First', 'ref': 'T-0001'},
    {'id': '2', 'title': 'Second', 'ref': 'T-0002'},
  ];
  final calls = <String>[];
  int listed = 0;

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
      {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async {
    listed++;
    return [for (final r in rows) Map<String, dynamic>.from(r)];
  }

  @override
  Future<List<TableOccurrenceModel>> listOccurrences() async => const [
        TableOccurrenceModel(id: 'b1', baseTableId: 't9', name: 'tickets', xPos: 0, yPos: 0),
        TableOccurrenceModel(id: 'b2', baseTableId: 't2', name: 'Ticket Vendors', xPos: 0, yPos: 0),
        TableOccurrenceModel(
          id: 'b3',
          baseTableId: null,
          name: 'Remote Customers',
          xPos: 0,
          yPos: 0,
          external: ExternalOccurrenceModel(fileReferenceId: 'f1', fileReference: 'Remote', table: 'customers'),
        ),
      ];

  @override
  Future<List<RelationshipModel>> listRelationships() async => const [];

  @override
  Future<List<TableModel>> listTables() async => const [_tickets, _vendors];

  @override
  ApiClient forOccurrence(String occurrenceId) => _OccurrenceCalls(calls, occurrenceId);

  @override
  Future<Map<String, dynamic>> insertRow(String table, Map<String, dynamic> record, {bool script = false, bool overrideValidation = false}) async {
    calls.add('insert script=$script ${jsonEncode(record)}');
    // The server enters the serial number of a record that gives none.
    final created = {'id': '${rows.length + 1}', 'ref': 'T-000${rows.length + 1}', ...record};
    rows.add(created);
    return created;
  }

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> updates,
      {bool script = false, bool overrideValidation = false}) async {
    calls.add('update $id script=$script ${jsonEncode(updates)}');
    return {'id': id, ...updates};
  }

  @override
  Future<Map<String, dynamic>> duplicateRow(String table, String id) async {
    calls.add('duplicate $id');
    final created = {...rows.firstWhere((r) => r['id'] == id), 'id': '${rows.length + 1}', 'ref': 'T-0003'};
    rows.add(created);
    return created;
  }

  @override
  Future<String> setNextSerialValue(String table, String field, String nextValue) async {
    calls.add('serial $table.$field=$nextValue');
    return nextValue;
  }

  @override
  Future<({int updated, List<Map<String, dynamic>> records})> relookupFieldContents(
      String table, String matchField, List<String> recordIds) async {
    calls.add('relookup $table.$matchField ${recordIds.join(',')}');
    final changed = [
      for (final r in rows)
        if (recordIds.contains(r['id'])) {...r, 'title': 'Re-${r['id']}'},
    ];
    return (updated: changed.length, records: changed);
  }
}

/// The calls a data browser makes through an occurrence's records route.
class _OccurrenceCalls extends ApiClient {
  _OccurrenceCalls(this.calls, this.occurrence) : super(baseUrl: 'http://localhost:1');
  final List<String> calls;
  final String occurrence;

  @override
  Future<String> setNextSerialValue(String table, String field, String nextValue) async {
    calls.add('serial@$occurrence $table.$field=$nextValue');
    return nextValue;
  }
}

/// The form's boxes for the fields after the primary key, in order.
Finder _formField(WidgetTester tester, int n) {
  final all = find.byType(TextField);
  final count = tester.widgetList(all).length;
  return all.at(count - _tickets.columns.length + 1 + n);
}

Future<(DataBrowserWidgetState, _BrowseApi)> _browse(WidgetTester tester) async {
  tester.view.physicalSize = const Size(1400, 1000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final api = _BrowseApi();
  final key = GlobalKey<DataBrowserWidgetState>();
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(body: DataBrowserWidget(key: key, table: _tickets, apiClient: api, mode: OperationalMode.browse)),
  ));
  await tester.pumpAndSettle();
  return (key.currentState!, api);
}

// ─── Script steps ────────────────────────────────────────────────────────────

class _Host implements LayoutActionHost, AutoEnterActionHost {
  final calls = <String>[];
  int code = 0;

  @override
  Future<int> actionSetNextSerialValue(String field, String value) async {
    calls.add('serial $field=$value');
    return code;
  }

  @override
  Future<int> actionRelookupFieldContents(String field, {bool dialog = false}) async {
    calls.add('relookup $field dialog=$dialog');
    return code;
  }

  @override
  String actionResolveText(String text) => text;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _PlainHost implements LayoutActionHost {
  @override
  String actionResolveText(String text) => text;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

LayoutActionRunner _runner(LayoutActionHost host) =>
    LayoutActionRunner(apiClient: ApiClient(baseUrl: 'http://localhost:1'), host: host);

void main() {
  group('serialOptionsError', () {
    test('takes a whole increment of at least 1 and a next value with a digit', () {
      expect(serialOptionsError('1', '1'), isNull);
      expect(serialOptionsError('INV0009', '5'), isNull);
      expect(serialOptionsError('1', '0'), isNotNull);
      expect(serialOptionsError('1', '1.5'), isNotNull);
      expect(serialOptionsError('1', ''), isNotNull);
      expect(serialOptionsError('ABC', '1'), isNotNull);
      expect(serialOptionsError('', '1'), isNotNull);
    });

    test('a number field counts in whole numbers', () {
      expect(serialOptionsError('100', '1', fieldType: 'NUMBER'), isNull);
      expect(serialOptionsError('A100', '1', fieldType: 'NUMBER'), isNotNull);
      expect(serialOptionsError('1.5', '1', fieldType: 'NUMBER'), isNotNull);
      expect(serialOptionsError('A100', '1'), isNull, reason: 'a text field keeps its prefix');
    });
  });

  group('LookupSpec and the relationship graph', () {
    test('reads the legacy Occurrence::Field text when the new keys are absent', () {
      final spec = LookupSpec.fromOptions(const {'looked_up_target': 'vendors::label'});
      expect(spec.occurrence, 'vendors');
      expect(spec.field, 'label');
      expect(spec.noMatch, LookupNoMatch.doNotCopy);
      expect(spec.isComplete, isTrue);
    });

    test('the new keys win over the legacy text', () {
      final spec = LookupSpec.fromOptions(const {
        'looked_up_target': 'old::thing',
        'looked_up_occurrence': 'vendors',
        'looked_up_field': 'label',
        'looked_up_no_match': 'use_constant',
        'looked_up_no_match_value': '?',
      });
      expect((spec.occurrence, spec.field, spec.noMatch, spec.noMatchValue), ('vendors', 'label', 'use_constant', '?'));
    });

    test('the occurrences reached are the related ones, in either direction', () {
      expect(reachableOccurrences('o1', _occurrences, _relationships).map((o) => o.name), ['vendors']);
      expect(reachableOccurrences('o2', _occurrences, _relationships).map((o) => o.name), ['items']);
      expect(reachableOccurrences('o3', _occurrences, _relationships), isEmpty);
    });

    test('the default occurrence is the one named like the table', () {
      expect(defaultOccurrenceOf('t1', 'items', _occurrences)?.id, 'o1');
      expect(defaultOccurrenceOf('t9', 'none', _occurrences), isNull);
    });
  });

  group('Field Options > Auto-Enter', () {
    testWidgets('stamps are chosen from Date, Time, Timestamp, Name and Account Name', (tester) async {
      final api = await _openDialog(tester, _column('code'));
      await _tap(tester, find.byKey(const ValueKey('auto-enter-creation')));
      await _tap(tester, find.byKey(const ValueKey('auto-enter-creation-type')));
      for (final type in kAutoEnterStampTypes) {
        expect(find.text(type), findsWidgets, reason: type);
      }
      await tester.tap(find.text('Account Name').last);
      await tester.pumpAndSettle();

      await _tap(tester, find.byKey(const ValueKey('auto-enter-modification')));
      await _choose(tester, const ValueKey('auto-enter-modification-type'), 'Timestamp');
      await _ok(tester);

      final options = api.lastOptions;
      expect(options['creation_enabled'], isTrue);
      expect(options['creation_type'], 'Account Name');
      expect(options['modification_enabled'], isTrue);
      expect(options['modification_type'], 'Timestamp');
    });

    testWidgets('a serial number is checked before it is saved', (tester) async {
      final api = await _openDialog(tester, _column('qty'));
      await _tap(tester, find.byKey(const ValueKey('auto-enter-serial')));

      await tester.enterText(find.byKey(const ValueKey('auto-enter-serial-increment')), '0');
      await _ok(tester);
      expect(api.saved, isEmpty);
      expect(find.byKey(const ValueKey('auto-enter-serial-error')), findsOneWidget);

      await tester.enterText(find.byKey(const ValueKey('auto-enter-serial-increment')), '2');
      await tester.enterText(find.byKey(const ValueKey('auto-enter-serial-next')), 'Q10');
      await _ok(tester);
      expect(api.saved, isEmpty, reason: 'a number field counts in whole numbers');
      expect(find.text('The next value of a number field is a whole number.'), findsOneWidget);

      await tester.enterText(find.byKey(const ValueKey('auto-enter-serial-next')), '10');
      await _tap(tester, find.byKey(const ValueKey('auto-enter-serial-commit')));
      await _ok(tester);
      final options = api.lastOptions;
      expect(options['serial_enabled'], isTrue);
      expect(options['serial_generate'], 'commit');
      expect(options['serial_next'], '10');
      expect(options['serial_increment'], 2);
    });

    testWidgets('the next serial value is saved only when it was edited; prohibit round-trips', (tester) async {
      const stored = '{"serial_enabled":true,"serial_generate":"creation","serial_next":"A0042",'
          '"serial_increment":1,"prohibit_modification":true,"custom_key":"kept"}';
      final api = await _openDialog(tester, _column('code', defaultValue: stored));

      final serialNext = tester.widget<TextField>(find.byKey(const ValueKey('auto-enter-serial-next')));
      expect(serialNext.controller!.text, 'A0042');
      final prohibit = tester.widget<Checkbox>(find.byKey(const ValueKey('auto-enter-prohibit')));
      expect(prohibit.value, isTrue);

      await _ok(tester);
      var options = api.lastOptions;
      expect(options.containsKey('serial_next'), isFalse,
          reason: 'the live counter is not moved back by a dialog left open');
      expect(options['serial_increment'], 1);
      expect(options['serial_generate'], 'creation');
      expect(options['calculated_skip_if_empty'], isTrue);
      expect(options['prohibit_modification'], isTrue);
      expect(options['custom_key'], 'kept', reason: 'keys the dialog does not show are kept');
      for (final key in const [
        'looked_up_context',
        'looked_up_occurrence',
        'looked_up_field',
        'looked_up_dont_copy_if_empty',
        'looked_up_no_match',
        'looked_up_no_match_value',
      ]) {
        expect(options.containsKey(key), isTrue, reason: key);
      }

      // A second time, editing the next value and lifting the prohibition.
      await tester.pumpWidget(const SizedBox());
      final again = await _openDialog(tester, _column('code', defaultValue: stored));
      await tester.enterText(find.byKey(const ValueKey('auto-enter-serial-next')), 'A0100');
      await _tap(tester, find.byKey(const ValueKey('auto-enter-prohibit')));
      await _ok(tester);
      options = again.lastOptions;
      expect(options['serial_next'], 'A0100');
      expect(options['prohibit_modification'], isFalse);
    });

    testWidgets('the auto-enter formula is kept apart from the calculation formula', (tester) async {
      const stored = '{"calculated_enabled":true,"calculated_formula":"Upper ( code )", '
          '"calculated_skip_if_empty":false,"do_not_replace":true}';
      final api = await _openDialog(
          tester, _column('code', defaultValue: stored, calculationFormula: 'Upper ( code )'));
      expect(tester.widget<Checkbox>(find.byKey(const ValueKey('auto-enter-skip-if-empty'))).value, isFalse);

      await _choose(tester, const ValueKey('auto-enter-calculated-context'), 'items_copy');
      await _ok(tester);
      final options = api.lastOptions;
      expect(options['calculated_formula'], 'Upper ( code )');
      expect(options['calculated_context'], 'items_copy');
      expect(options['calculated_skip_if_empty'], isFalse);
      expect(options['do_not_replace'], isTrue);
      expect(api.saved.last['calculation_formula'], isNull,
          reason: 'a text field has no calculation formula; the copy older clients made is dropped');
    });

    testWidgets('Specify... opens the formula editor rather than another tab', (tester) async {
      final api = await _openDialog(tester, _column('code'));
      await _tap(tester, find.byKey(const ValueKey('auto-enter-calculated-specify')));
      expect(find.byType(CalculationBuilderDialog), findsOneWidget);

      final editor = find.descendant(
          of: find.byType(CalculationBuilderDialog),
          matching: find.byWidgetPredicate((w) => w is TextField && w.maxLines == null));
      await tester.enterText(editor, 'Get ( UUID )');
      await tester.tap(find.descendant(of: find.byType(CalculationBuilderDialog), matching: find.text('OK')));
      await tester.pumpAndSettle();
      expect(find.byType(CalculationBuilderDialog), findsNothing);

      await _ok(tester);
      final options = api.lastOptions;
      expect(options['calculated_enabled'], isTrue);
      expect(options['calculated_formula'], 'Get ( UUID )');
    });

    testWidgets('an enabled lookup names what it copies before it is saved', (tester) async {
      final api = await _openDialog(tester, _column('code'));
      await _tap(tester, find.byKey(const ValueKey('auto-enter-looked-up')));
      expect(find.byType(LookupOptionsDialog), findsOneWidget, reason: 'turning it on asks what to look up');

      await tester.tap(find.text('Cancel').last);
      await tester.pumpAndSettle();
      expect(tester.widget<Checkbox>(find.byKey(const ValueKey('auto-enter-looked-up'))).value, isFalse,
          reason: 'cancelled with nothing to look up, it stays off');
      await _ok(tester);
      expect(api.lastOptions['looked_up_enabled'], isFalse);
    });

    testWidgets('the lookup dialog offers related occurrences and their fields', (tester) async {
      final api = await _openDialog(tester, _column('code'));
      await _tap(tester, find.byKey(const ValueKey('auto-enter-looked-up-specify')));

      // Starting with table: the occurrences of this table only.
      await _tap(tester, find.byKey(const ValueKey('lookup-start')));
      expect(find.text('items_copy'), findsWidgets);
      expect(find.text('notes'), findsNothing);
      await tester.tap(find.text('items').last);
      await tester.pumpAndSettle();

      // Lookup from related table: what the graph reaches.
      await _tap(tester, find.byKey(const ValueKey('lookup-occurrence')));
      expect(find.text('notes'), findsNothing, reason: 'not related to items');
      await tester.tap(find.text('vendors').last);
      await tester.pumpAndSettle();

      expect(find.byKey(const ValueKey('lookup-field-vendor_id')), findsOneWidget);
      await _tap(tester, find.byKey(const ValueKey('lookup-field-label')));
      await _tap(tester, find.byKey(const ValueKey('lookup-dont-copy-empty')));
      await _tap(tester, find.byKey(const ValueKey('lookup-no-match-use_constant')));
      await tester.enterText(find.byKey(const ValueKey('lookup-constant')), 'n/a');
      await _tap(tester, find.byKey(const ValueKey('lookup-ok')));

      expect(find.byType(LookupOptionsDialog), findsNothing);
      expect(find.text('from vendors::label'), findsOneWidget);
      expect(tester.widget<Checkbox>(find.byKey(const ValueKey('auto-enter-looked-up'))).value, isTrue);

      await _ok(tester);
      final options = api.lastOptions;
      expect(options['looked_up_enabled'], isTrue);
      expect(options['looked_up_context'], '', reason: 'the default start stays the default');
      expect(options['looked_up_occurrence'], 'vendors');
      expect(options['looked_up_field'], 'label');
      expect(options['looked_up_dont_copy_if_empty'], isTrue);
      expect(options['looked_up_no_match'], 'use_constant');
      expect(options['looked_up_no_match_value'], 'n/a');
      expect(options.containsKey('looked_up_target'), isFalse);
    });

    testWidgets('a legacy lookup is shown and saved with the new keys', (tester) async {
      final api = await _openDialog(
          tester, _column('code', defaultValue: '{"looked_up_enabled":true,"looked_up_target":"vendors::label"}'));
      expect(find.text('from vendors::label'), findsOneWidget);
      await _ok(tester);
      final options = api.lastOptions;
      expect((options['looked_up_occurrence'], options['looked_up_field']), ('vendors', 'label'));
      expect(options.containsKey('looked_up_target'), isFalse);
    });
  });

  group('Browse mode', () {
    testWidgets('a field prohibiting modification is read-only; Set Field still writes it', (tester) async {
      final (state, api) = await _browse(tester);
      expect(tester.widget<TextField>(_formField(tester, 0)).readOnly, isFalse, reason: 'title takes entry');
      expect(tester.widget<TextField>(_formField(tester, 1)).readOnly, isTrue, reason: 'ref is prohibited');

      expect(await state.actionSetField('ref', 'T-0099'), isTrue);
      await tester.pumpAndSettle();
      expect(api.calls.last, 'update 1 script=true {"ref":"T-0099"}');

      await tester.enterText(_formField(tester, 0), 'Renamed');
      await state.actionCommitRecord();
      await tester.pumpAndSettle();
      expect(api.calls.last, 'update 1 script=false {"title":"Renamed"}', reason: 'typing is data entry');
    });

    testWidgets('a new record a script set a prohibited field of is committed as a script write', (tester) async {
      final (state, api) = await _browse(tester);
      state.createNewRecord();
      await tester.pumpAndSettle();
      await state.actionSetField('ref', 'T-0500');
      await state.actionCommitRecord();
      await tester.pumpAndSettle();
      expect(api.calls.last, 'insert script=true {"ref":"T-0500"}');

      state.createNewRecord();
      await tester.pumpAndSettle();
      await tester.enterText(_formField(tester, 0), 'Typed');
      await state.actionCommitRecord();
      await tester.pumpAndSettle();
      expect(api.calls.last, 'insert script=false {"title":"Typed"}');
    });

    testWidgets('Duplicate Record asks the server, which enters new serial numbers', (tester) async {
      final (state, api) = await _browse(tester);
      await state.actionDuplicateRecord();
      await tester.pumpAndSettle();
      expect(api.calls, contains('duplicate 1'));
      expect(api.calls.where((c) => c.startsWith('insert')), isEmpty);
      expect(state.totalRecords, 3);
      expect(state.currentRecord?['ref'], 'T-0003');
    });

    testWidgets('the host sets the next serial value and relooks up the found set', (tester) async {
      final (state, api) = await _browse(tester);
      expect(await state.actionSetNextSerialValue('tickets::Ref', 'T-0100'), 0);
      expect(api.calls.last, 'serial tickets.ref=T-0100');
      expect(await state.actionSetNextSerialValue('missing', '1'), 102);

      expect(await state.actionRelookupFieldContents('title'), 0);
      await tester.pumpAndSettle();
      expect(api.calls.last, 'relookup tickets.title 1,2');
    });

    testWidgets('a committed record shows what the server entered, and a later save keeps it', (tester) async {
      final (state, api) = await _browse(tester);
      state.createNewRecord();
      await tester.pumpAndSettle();
      await tester.enterText(_formField(tester, 0), 'Typed');
      await state.actionCommitRecord();
      await tester.pumpAndSettle();
      expect(api.calls.last, 'insert script=false {"title":"Typed"}');
      expect(tester.widget<TextField>(_formField(tester, 1)).controller!.text, 'T-0003');
      await state.actionCommitRecord();
      await tester.pumpAndSettle();
      expect(api.calls.where((c) => c.startsWith('update')), isEmpty, reason: 'the serial is not written back blank');
      expect(state.currentRecord?['ref'], 'T-0003');
    });

    testWidgets('Relookup sends only the found set and keeps it', (tester) async {
      final (state, api) = await _browse(tester);
      await state.actionOmitRecord();
      await tester.pumpAndSettle();
      final listed = api.listed;
      expect(await state.actionRelookupFieldContents('title'), 0);
      await tester.pumpAndSettle();
      expect(api.calls.last, 'relookup tickets.title 2');
      expect(api.listed, listed, reason: 'the found set is not replaced by the first page of the table');
      expect(state.totalRecords, 1);
      expect(state.currentRecord?['title'], 'Re-2');
      expect(tester.widget<TextField>(_formField(tester, 0)).controller!.text, 'Re-2');
    });

    testWidgets("Set Next Serial Value on Occurrence::Field sets it in that occurrence's table", (tester) async {
      final (state, api) = await _browse(tester);
      expect(await state.actionSetNextSerialValue('Ticket Vendors::Label', 'V-9'), 0);
      expect(api.calls.last, 'serial@b2 vendors.label=V-9');
      expect(await state.actionSetNextSerialValue('Remote Customers::ref', 'R-1'), 0);
      expect(api.calls.last, 'serial@b3 Remote Customers.ref=R-1', reason: 'another solution checks its own field');
      final before = api.calls.length;
      expect(await state.actionSetNextSerialValue('Ticket Vendors::nope', '1'), 102);
      await tester.pumpAndSettle();
      expect(find.text('Set Next Serial Value: Ticket Vendors has no field "nope".'), findsOneWidget);
      expect(await state.actionSetNextSerialValue('Nowhere::ref', '1'), 102);
      expect(api.calls.length, before, reason: 'nothing is set on the layout table instead');
    });

    testWidgets('Relookup with a dialog asks first, naming the records and the field', (tester) async {
      final (state, api) = await _browse(tester);
      final run = state.actionRelookupFieldContents('title', dialog: true);
      await tester.pumpAndSettle();
      expect(find.text('Relookup 2 records using the matching field "Title"?'), findsOneWidget);
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();
      expect(await run, 1, reason: 'cancelled');
      expect(api.calls.where((c) => c.startsWith('relookup')), isEmpty);
    });
  });

  group('Script steps', () {
    test('Set Next Serial Value calls the host with the field and the value', () async {
      final host = _Host();
      final result = await _runner(host).runStep(
          'Set Next Serial Value', {'field': 'invoice_no', 'value': '"INV" & "0100"', 'value_is_calculation': true},
          depth: 0);
      expect(result.completed, isTrue, reason: result.message);
      expect(host.calls, ['serial invoice_no=INV0100']);
    });

    test('Relookup Field Contents calls the host, without a dialog by default', () async {
      final host = _Host();
      final runner = _runner(host);
      expect((await runner.runStep('relookup_field_contents', {'field': 'vendor_id'}, depth: 0)).completed, isTrue);
      expect((await runner.runStep('Relookup Field Contents', {'field': 'vendor_id', 'no_dialog': false}, depth: 0))
          .completed, isTrue);
      expect(host.calls, ['relookup vendor_id dialog=false', 'relookup vendor_id dialog=true']);
    });

    test('a failure sets Get ( LastError ) and is captured', () async {
      final host = _Host()..code = 102;
      final context = ScriptCallContext()..session.errorCapture = true;
      final result = await _runner(host)
          .runStep('set_next_serial_value', {'field': 'nope', 'value': '1'}, depth: 0, callContext: context);
      expect(result.completed, isTrue);
      expect(context.session.lastError, 102);

      host.code = 0;
      await _runner(host)
          .runStep('relookup_field_contents', {'field': 'vendor_id'}, depth: 0, callContext: context);
      expect(context.session.lastError, 0);
    });

    test('a host without the steps fails them with error 3', () async {
      final context = ScriptCallContext();
      final result = await _runner(_PlainHost())
          .runStep('relookup_field_contents', {'field': 'vendor_id'}, depth: 0, callContext: context);
      expect(result.completed, isFalse);
      expect(context.session.lastError, 3);
    });

    test('the steps are named and filed like FileMaker', () {
      const serial = ScriptStepModel(id: 's', scriptId: 'x', sequenceIdx: 0, stepType: 'set_next_serial_value');
      const relookup = ScriptStepModel(id: 'r', scriptId: 'x', sequenceIdx: 1, stepType: 'relookup_field_contents');
      expect(serial.displayName, 'Set Next Serial Value');
      expect(relookup.displayName, 'Relookup Field Contents');
      expect(serial.category, 'fields');
      expect(relookup.category, 'records');
      expect(LayoutActionRunner.normalizeStepType('Set Next Serial Value'), 'set_next_serial_value');
      expect(LayoutActionRunner.normalizeStepType('Relookup Field Contents'), 'relookup_field_contents');
      final catalog = ScriptCatalogItem.catalog.map((i) => i.stepType);
      expect(catalog, containsAll(['set_next_serial_value', 'relookup_field_contents']));
    });
  });

  group('ApiClient', () {
    Future<(ApiClient, List<http.Request>)> client(Object Function(http.Request) answer, {int status = 200}) async {
      final requests = <http.Request>[];
      final api = ApiClient(
        baseUrl: 'http://test-server:8080',
        httpClient: MockClient((request) async {
          requests.add(request);
          return http.Response(jsonEncode(answer(request)), status, headers: {'content-type': 'application/json'});
        }),
      );
      return (api, requests);
    }

    test('a script write carries X-File4Base-Entry: script; data entry does not', () async {
      final (api, requests) = await client((r) => {'id': '1'});
      await api.insertRow('items', {'code': 'A'}, script: true);
      await api.updateRow('items', '1', {'code': 'B'}, script: true);
      await api.insertRow('items', {'code': 'C'});
      await api.updateRow('items', '1', {'code': 'D'});
      expect(requests.map((r) => r.headers[ApiClient.entryHeader]), ['script', 'script', null, null]);
      expect(requests.map((r) => '${r.method} ${r.url.path}'),
          ['POST /api/v1/data/items', 'PUT /api/v1/data/items/1', 'POST /api/v1/data/items', 'PUT /api/v1/data/items/1']);
    });

    test('duplicateRow posts to the duplicate endpoint and returns the new record', () async {
      final (api, requests) = await client((r) => {'id': '7', 'code': 'A'}, status: 201);
      final created = await api.duplicateRow('items', '3');
      expect(created['id'], '7');
      expect('${requests.single.method} ${requests.single.url.path}', 'POST /api/v1/data/items/3/duplicate');
    });

    test('the next serial value is read and set', () async {
      final (api, requests) = await client((r) => {
            'field': 'code',
            'next_value': r.method == 'PUT' ? (jsonDecode(r.body) as Map)['next_value'] : 'A0042',
          });
      expect(await api.getNextSerialValue('items', 'code'), 'A0042');
      expect(await api.setNextSerialValue('items', 'code', 'A0100'), 'A0100');
      expect(requests.map((r) => '${r.method} ${r.url.path}'),
          ['GET /api/v1/data/items/serials/code', 'PUT /api/v1/data/items/serials/code']);
      expect(jsonDecode(requests.last.body), {'next_value': 'A0100'});
    });

    test('relookupFieldContents sends the match field and the records', () async {
      final (api, requests) = await client((r) => {
            'updated': 1,
            'records': [
              {'id': '2', 'vendor_id': 'V2'},
            ],
          });
      final result = await api.relookupFieldContents('items', 'vendor_id', ['1', '2']);
      expect(result.updated, 1);
      expect(result.records.single['id'], '2');
      expect('${requests.single.method} ${requests.single.url.path}', 'POST /api/v1/data/items/relookup');
      expect(jsonDecode(requests.single.body), {'field': 'vendor_id', 'record_ids': ['1', '2']});
    });

    test("an occurrence's client duplicates, sets serials and relooks up through its records route", () async {
      final (api, requests) = await client((r) => r.url.path.endsWith('/relookup')
          ? {'updated': 0, 'records': <Object>[]}
          : {'id': '9', 'field': 'ref', 'next_value': 'R-1'});
      final occ = api.forOccurrence('o 7');
      await occ.duplicateRow('ignored', '3');
      await occ.getNextSerialValue('ignored', 'ref');
      await occ.setNextSerialValue('ignored', 'ref', 'R-1');
      await occ.relookupFieldContents('ignored', 'code', ['3']);
      expect(requests.map((r) => '${r.method} ${r.url.path}'), [
        'POST /api/v1/occurrences/o%207/records/3/duplicate',
        'GET /api/v1/occurrences/o%207/records/serials/ref',
        'PUT /api/v1/occurrences/o%207/records/serials/ref',
        'POST /api/v1/occurrences/o%207/records/relookup',
      ]);
    });

    test('importRecords passes perform_auto_enter through its options', () async {
      final (api, requests) = await client((r) => {'rows': 0, 'added': 0, 'fields': <String>[]});
      await api.importRecords('items', options: {'perform_auto_enter': true}, fileName: 'x.csv');
      final body = jsonDecode(requests.single.body) as Map<String, dynamic>;
      expect((body['options'] as Map)['perform_auto_enter'], isTrue);
    });

    test('a column says whether it prohibits modification', () {
      expect(_tickets.columns[2].prohibitsModification, isTrue);
      expect(_tickets.columns[1].prohibitsModification, isFalse);
      expect(
        const ColumnModel(id: 'x', tableId: 't', name: 'x', displayName: 'x', fieldType: 'TEXT', defaultValue: 'plain')
            .prohibitsModification,
        isFalse,
      );
    });
  });
}
