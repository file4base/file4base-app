import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/layout_object_visuals.dart';
import 'package:file4base_client/features/layout_engine/layout_preview_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

// A migrated layout keeps the repetition a box shows, the date order of the
// solution, which boxes take typing, and hides per record what its
// conditions hide, in List view as in Form view. Synthetic data only.
const _table = TableModel(id: 't1', name: 'contacts', displayName: 'Contacts', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'first_name', displayName: 'First name', fieldType: 'varchar'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'amount', displayName: 'Amount', fieldType: 'NUMBER'),
]);

class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');

  final evaluations = <({List<String> recordIds, String? occurrence, List<String> expressions})>[];
  final updates = <Map<String, dynamic>>[];

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [
        {'id': '1', 'first_name': 'Ana', 'amount': '500'},
        {'id': '2', 'first_name': 'Bea', 'amount': '7'},
      ];

  @override
  Future<List<Map<String, dynamic>>> listAllRows(String table, {void Function(int fetched)? onProgress}) =>
      listRows(table);

  @override
  Future<List<RelationshipModel>> listRelationships() async => throw Exception('no graph');

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> data, {bool overrideValidation = false, bool script = false}) async {
    updates.add(data);
    return {'id': id, ...data};
  }

  @override
  Future<Map<String, List<EvaluationModel>?>> evaluateForRecords(String table,
      {required List<String> expressions,
      required List<String> recordIds,
      String? occurrence,
      String resultType = 'Text'}) async {
    evaluations.add((recordIds: recordIds, occurrence: occurrence, expressions: expressions));
    // "amount > 100", answered as the server would for these records.
    return {
      for (final id in recordIds)
        id: [for (final e in expressions) EvaluationModel(expression: e, text: id == '1' ? '1' : '0')],
    };
  }
}

LayoutDefinitionModel _listLayout() => const LayoutDefinitionModel(
      id: 'l',
      name: 'L',
      tableOccurrence: 'occ-contacts',
      defaultView: 'list',
      parts: [LayoutPartModel(id: 'b', type: 'body', height: 40)],
      objects: [
        LayoutObjectModel(
          id: 'name',
          type: 'field',
          x: 10,
          y: 5,
          width: 200,
          height: 24,
          hideWhen: 'amount > 100',
          fieldBinding: FieldBindingModel(fieldName: 'first_name'),
        ),
        LayoutObjectModel(
          id: 'amount',
          type: 'field',
          x: 220,
          y: 5,
          width: 120,
          height: 24,
          fieldBinding: FieldBindingModel(fieldName: 'amount', allowBrowseEntry: false),
        ),
      ],
    );

Future<_Api> _pumpList(WidgetTester tester, {LayoutDefinitionModel? layout}) async {
  tester.view.physicalSize = const Size(1400, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final api = _Api();
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: DataBrowserWidget(table: _table, apiClient: api, mode: OperationalMode.browse, layout: layout ?? _listLayout()),
    ),
  ));
  await tester.pumpAndSettle();
  await tester.tap(find.text('List'));
  await tester.pumpAndSettle();
  return api;
}

void main() {
  group('repetitions', () {
    test('a box keeps the first repetition it shows', () {
      final binding = FieldBindingModel.fromJson(const {'field_name': 'amount', 'first_repetition': 3, 'repetitions_shown': 1});
      expect(binding.firstRepetition, 3);
      expect(FieldBindingModel.fromJson(binding.toJson()).firstRepetition, 3);
      expect(binding.repetitionNumbers(5), [3]);
      expect(binding.copyWith(repetitionsShown: 2).repetitionNumbers(5), [3, 4]);
      expect(binding.copyWith(repetitionsShown: 9).repetitionNumbers(5), [3, 4, 5],
          reason: 'never past the last repetition of the field');
      expect(const FieldBindingModel(fieldName: 'a', repetitionsShown: 1).repetitionNumbers(4), [1]);
      expect(const FieldBindingModel(fieldName: 'a', firstRepetition: 2).repetitionNumbers(4), [2, 3, 4]);
      expect(const FieldBindingModel(fieldName: 'a').toJson().containsKey('first_repetition'), isFalse);
      expect(ColumnModel.repetitionKey('amount', 3), 'amount[3]');
    });
  });

  group('dates', () {
    test('a date is read and shown in the order of the solution', () {
      expect(normalizeDateInput('03/04/2026', order: 'mdy'), '2026-03-04');
      expect(normalizeDateInput('03/04/2026', order: 'dmy'), '2026-04-03');
      expect(normalizeDateInput('2026-04-03', order: 'mdy'), '2026-04-03');
      expect(normalizeDateInput('13/04/2026', order: 'mdy'), isNull, reason: 'there is no thirteenth month');
      expect(dateInOrder('2026-04-03', 'mdy'), '04/03/2026');
      expect(dateInOrder('2026-04-03', 'dmy'), '03/04/2026');
      expect(dateOrderOfFormat('mm/dd/yyyy'), 'mdy');
      expect(dateOrderOfFormat('dd/mm/yyyy'), 'dmy');
      expect(deviceDateOrder(const Locale('en', 'US')), 'mdy');
      expect(deviceDateOrder(const Locale('fr', 'FR')), 'dmy');
    });

    test('a layout keeps the date order it was migrated with', () {
      final layout = LayoutDefinitionModel.fromJson(const {'name': 'L', 'date_order': 'mdy', 'objects': []});
      expect(layout.dateOrder, 'mdy');
      expect(LayoutDefinitionModel.fromJson(layout.toJson()).dateOrder, 'mdy');
      expect(LayoutDefinitionModel.fromJson(const {'name': 'L', 'date_order': 'xyz', 'objects': []}).dateOrder, isNull);
    });
  });

  group('List view', () {
    testWidgets('hides on each row what its record hides, evaluated from the layout occurrence', (tester) async {
      final api = await _pumpList(tester);
      expect(find.text('Ana'), findsNothing, reason: 'hidden on the record whose amount is over 100');
      expect(find.text('Bea'), findsOneWidget);
      expect(api.evaluations, isNotEmpty);
      expect(api.evaluations.map((e) => e.occurrence), everyElement('occ-contacts'));
      expect(api.evaluations.expand((e) => e.recordIds).toSet(), {'1', '2'},
          reason: 'each record asked once, its answer kept while it is unchanged');
    });

    testWidgets('a conditional format by calculation colours the rows it is true for', (tester) async {
      final base = _listLayout();
      final layout = base.copyWith(objects: [
        for (final o in base.objects)
          if (o.id == 'amount')
            o.copyWith(
                style: o.style.copyWith(conditionalRules: const [
              ConditionalFormatRule(operator: 'formula', value: 'amount > 100', textColor: '#FF0000'),
            ]))
          else
            o.copyWith(clearHideWhen: true),
      ]);
      final api = await _pumpList(tester, layout: layout);
      Color? colour(String text) => tester.widget<Text>(find.text(text)).style?.color;
      expect(colour('500'), const Color(0xFFFF0000));
      expect(colour('7'), isNot(const Color(0xFFFF0000)));
      expect(api.evaluations.expand((e) => e.expressions).toSet(), {'amount > 100'},
          reason: 'evaluated by the server with the hide conditions, from the layout occurrence');
      expect(api.evaluations.map((e) => e.occurrence), everyElement('occ-contacts'));
    });

    testWidgets('a field that takes no entry in Browse mode cannot be typed into', (tester) async {
      final api = await _pumpList(tester);
      await tester.tap(find.text('7'));
      await tester.pumpAndSettle();
      final focused = FocusManager.instance.primaryFocus?.context?.widget;
      expect(focused is EditableText && focused.controller.text == '7', isFalse);
      expect(
          find.byWidgetPredicate((w) => w is TextField && w.controller?.text == '7' && (w.enabled ?? true) && !w.readOnly),
          findsNothing);
      expect(api.updates, isEmpty);
    });
  });

  testWidgets('Preview never shows a related field from a column of the layout table', (tester) async {
    tester.view.physicalSize = const Size(1400, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    const layout = LayoutDefinitionModel(
      id: 'p',
      name: 'P',
      tableOccurrence: 'contacts',
      parts: [LayoutPartModel(id: 'b', type: 'body', height: 60)],
      objects: [
        LayoutObjectModel(
          id: 'rel',
          type: 'field',
          x: 10,
          y: 5,
          width: 200,
          height: 24,
          fieldBinding: FieldBindingModel(fieldName: 'first_name', relationshipId: 'r1', tableOccurrence: 'Orders'),
        ),
        LayoutObjectModel(
          id: 'far',
          type: 'field',
          x: 10,
          y: 30,
          width: 200,
          height: 24,
          fieldBinding: FieldBindingModel(fieldName: 'amount', tableOccurrence: 'Far', unresolvedRelationship: true),
        ),
      ],
    );
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(body: LayoutPreviewWidget(table: _table, apiClient: _Api(), layout: layout)),
    ));
    await tester.pumpAndSettle();
    expect(find.text('Ana'), findsNothing, reason: 'first_name of this table is not Orders::first_name');
    expect(find.text('500'), findsNothing);
    expect(find.text('<Orders::first_name>'), findsWidgets);
    expect(find.textContaining('Far::amount'), findsWidgets);
  });
}
