// #222 — a conditional format by calculation is drawn wherever its object
// is: an edit box of the form, each repetition of a repeating box, each row
// of List view and every control a value list fills. Its `Self` is the
// object's field, which the client names to the server with each
// expression. Portal rows and related fields are in portal_test.dart.
// Synthetic data only.

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _table = TableModel(
  id: 't1',
  name: 'orders',
  displayName: 'Orders',
  columns: [
    ColumnModel(
      id: 'c0',
      tableId: 't1',
      name: 'id',
      displayName: 'ID',
      fieldType: 'TEXT',
      isPrimaryKey: true,
    ),
    ColumnModel(
      id: 'c1',
      tableId: 't1',
      name: 'amount',
      displayName: 'Amount',
      fieldType: 'NUMBER',
    ),
    ColumnModel(
      id: 'c2',
      tableId: 't1',
      name: 'marks',
      displayName: 'Marks',
      fieldType: 'NUMBER',
      maxRepetitions: 3,
    ),
    ColumnModel(
      id: 'c3',
      tableId: 't1',
      name: 'status',
      displayName: 'Status',
      fieldType: 'TEXT',
    ),
  ],
);

const _statuses = ValueListModel(
  id: 'vl-status',
  name: 'Statuses',
  customValues: 'Open\nClosed',
);

const _red = Color(0xFFFF0000);
const _yellow = Color(0xFFFFFF00);

/// What a rule draws while it is true: red text on yellow.
ConditionalFormatRule _rule(String formula) => ConditionalFormatRule(
  operator: 'formula',
  value: formula,
  textColor: '#FF0000',
  fillColor: '#FFFF00',
);

class _Api extends ApiClient {
  _Api(this.rows) : super(baseUrl: 'http://localhost:1');

  final List<Map<String, dynamic>> rows;

  /// Each request: the records asked about, and each expression with its Self.
  final requests = <({List<String> recordIds, List<String> asked})>[];

  @override
  Future<List<Map<String, dynamic>>> listRows(
    String table, {
    int limit = 100,
    int offset = 0,
    String? sortBy,
    bool sortAsc = true,
    List<String>? sort,
  }) async => [for (final r in rows) Map<String, dynamic>.from(r)];

  @override
  Future<List<Map<String, dynamic>>> listAllRows(
    String table, {
    void Function(int fetched)? onProgress,
  }) => listRows(table);

  @override
  Future<List<RelationshipModel>> listRelationships() async =>
      throw Exception('no graph');

  @override
  Future<List<ValueListModel>> listValueLists() async => const [_statuses];

  @override
  Future<ValueListOffer> valueListItems(
    String id, {
    String? table,
    String? recordId,
  }) async => (
    items: [for (final v in _statuses.customValueList) ValueListItem(v)],
    truncated: false,
  );

  @override
  Future<Map<String, dynamic>> updateRow(
    String table,
    String id,
    Map<String, dynamic> data, {
    bool overrideValidation = false,
    bool script = false,
  }) async => {'id': id, ...data};

  @override
  Future<Map<String, List<EvaluationModel>?>> evaluateForRecords(
    String table, {
    required List<String> expressions,
    required List<String> recordIds,
    String? occurrence,
    String resultType = 'Text',
    List<ConditionSelf?>? self,
  }) async {
    ConditionSelf? selfOf(int i) =>
        self == null || i >= self.length ? null : self[i];
    requests.add((
      recordIds: recordIds,
      asked: [
        for (final (i, e) in expressions.indexed)
          '$e with ${selfOf(i)?.toJson()}',
      ],
    ));
    // `Self > 100` and `Self = "Open"`, as the server answers them: Self is
    // the field named, in the repetition named, of the record evaluated.
    EvaluationModel answer(String id, int i) {
      final e = expressions[i];
      final s = selfOf(i);
      if (s == null) {
        return EvaluationModel(expression: e, error: 'Self has no field here');
      }
      final value =
          '${rows.firstWhere((r) => r['id'] == id)[ColumnModel.repetitionKey(s.field, s.repetition)] ?? ''}';
      final truth = switch (e) {
        'Self > 100' => (num.tryParse(value) ?? 0) > 100,
        'Self = "Open"' => value == 'Open',
        _ => false,
      };
      return EvaluationModel(expression: e, text: truth ? '1' : '0');
    }

    return {
      for (final id in recordIds)
        id: [
          for (final i in expressions.indexed.map((e) => e.$1)) answer(id, i),
        ],
    };
  }
}

LayoutDefinitionModel _layout(
  FieldBindingModel binding,
  String formula, {
  String view = 'form',
}) => LayoutDefinitionModel(
  id: 'l',
  name: 'Orders',
  tableOccurrence: 'occ-orders',
  defaultView: view,
  parts: const [LayoutPartModel(id: 'b', type: 'body', height: 200)],
  objects: [
    LayoutObjectModel(
      id: 'box',
      type: 'field',
      x: 10,
      y: 10,
      width: 300,
      height: binding.fieldName == 'marks' ? 120 : 60,
      fieldBinding: binding,
      style: LayoutObjectStyle(conditionalRules: [_rule(formula)]),
    ),
  ],
);

Future<_Api> _pump(
  WidgetTester tester,
  LayoutDefinitionModel layout, {
  List<Map<String, dynamic>>? rows,
}) async {
  tester.view.physicalSize = const Size(1400, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final api = _Api(
    rows ??
        [
          {
            'id': '1',
            'amount': '500',
            'marks': '200',
            'marks[2]': '5',
            'marks[3]': '300',
            'status': 'Open',
          },
          {
            'id': '2',
            'amount': '7',
            'marks': '1',
            'marks[2]': '2',
            'marks[3]': '3',
            'status': 'Closed',
          },
        ],
  );
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: DataBrowserWidget(
          table: _table,
          apiClient: api,
          mode: OperationalMode.browse,
          layout: layout,
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
  if (layout.defaultView == 'list') {
    await tester.tap(find.text('List'));
    await tester.pumpAndSettle();
  }
  return api;
}

/// The edit box showing [text].
TextField _box(WidgetTester tester, String text) => tester.widget<TextField>(
  find.byWidgetPredicate((w) => w is TextField && w.controller?.text == text),
);

/// The colour [text] is drawn in, inside [of].
Color? _ink(WidgetTester tester, Finder of, String text) => tester
    .widget<RichText>(
      find.descendant(
        of: of,
        matching: find.byWidgetPredicate(
          (w) => w is RichText && w.text.toPlainText() == text,
        ),
      ),
    )
    .text
    .style
    ?.color;

/// The fill drawn behind [of], if any.
Color? _fillBehind(WidgetTester tester, Finder of) {
  final boxes = tester.widgetList<DecoratedBox>(
    find.ancestor(of: of, matching: find.byType(DecoratedBox)),
  );
  for (final b in boxes) {
    final d = b.decoration;
    if (d is BoxDecoration && d.color == _yellow) return d.color;
  }
  return null;
}

void main() {
  testWidgets(
    "an edit box is drawn by a rule on Self, and Self is the box's field",
    (tester) async {
      final api = await _pump(
        tester,
        _layout(const FieldBindingModel(fieldName: 'amount'), 'Self > 100'),
      );

      final box = _box(tester, '500');
      expect(box.style?.color, _red);
      expect(box.decoration?.fillColor, _yellow);
      expect(
        api.requests.expand((r) => r.asked).toSet(),
        {
          'Self > 100 with {field: amount}',
        },
        reason: 'the server is told which field Self is',
      );
    },
  );

  testWidgets('a rule on Self that is false leaves the box as it is', (
    tester,
  ) async {
    await _pump(
      tester,
      _layout(const FieldBindingModel(fieldName: 'amount'), 'Self > 100'),
      rows: [
        {'id': '2', 'amount': '7'},
      ],
    );

    final box = _box(tester, '7');
    expect(box.style?.color, isNot(_red));
    expect(box.decoration?.fillColor, isNot(_yellow));
  });

  testWidgets('each repetition of a repeating box is drawn by its own value', (
    tester,
  ) async {
    final api = await _pump(
      tester,
      _layout(const FieldBindingModel(fieldName: 'marks'), 'Self > 100'),
    );

    expect(_box(tester, '200').style?.color, _red);
    expect(
      _box(tester, '5').style?.color,
      isNot(_red),
      reason: 'repetition 2 holds 5',
    );
    expect(_box(tester, '300').style?.color, _red);
    expect(
      api.requests.expand((r) => r.asked).toSet(),
      {
        'Self > 100 with {field: marks}',
        'Self > 100 with {field: marks, repetition: 2}',
        'Self > 100 with {field: marks, repetition: 3}',
      },
      reason: 'one rule, evaluated once per repetition shown',
    );
  });

  testWidgets('each row of List view is drawn by its own record, asked once', (
    tester,
  ) async {
    final api = await _pump(
      tester,
      _layout(
        const FieldBindingModel(fieldName: 'amount', allowBrowseEntry: false),
        'Self > 100',
        view: 'list',
      ),
    );

    Color? colour(String text) =>
        tester.widget<Text>(find.text(text)).style?.color;
    expect(colour('500'), _red);
    expect(colour('7'), isNot(_red));
    final asked = api.requests.expand((r) => r.recordIds).toList();
    expect(asked.toSet(), {'1', '2'});

    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();
    expect(
      api.requests.expand((r) => r.recordIds).length,
      asked.length,
      reason:
          'an unchanged record is not asked again when the view is drawn again',
    );
  });

  group('a value-list control is drawn by its rule', () {
    for (final style in [
      FieldControlStyle.popUpMenu,
      FieldControlStyle.dropDownList,
    ]) {
      testWidgets(style, (tester) async {
        final api = await _pump(
          tester,
          _layout(
            FieldBindingModel(
              fieldName: 'status',
              controlStyle: style,
              valueListId: _statuses.id,
            ),
            'Self = "Open"',
          ),
        );

        final control = find.byKey(const ValueKey('value-list-control'));
        expect(control, findsOneWidget);
        final decorator = tester.widget<InputDecorator>(
          find.descendant(of: control, matching: find.byType(InputDecorator)),
        );
        expect(decorator.decoration.fillColor, _yellow);
        expect(decorator.decoration.filled, isTrue);
        expect(
          tester
              .widget<Text>(
                find.descendant(of: control, matching: find.text('Open')).first,
              )
              .style
              ?.color,
          _red,
        );
        expect(api.requests.expand((r) => r.asked).toSet(), {
          'Self = "Open" with {field: status}',
        });
      });
    }

    for (final (style, prefix) in [
      (FieldControlStyle.checkboxSet, 'checkbox'),
      (FieldControlStyle.radioButtonSet, 'radio'),
    ]) {
      testWidgets(style, (tester) async {
        await _pump(
          tester,
          _layout(
            FieldBindingModel(
              fieldName: 'status',
              controlStyle: style,
              valueListId: _statuses.id,
            ),
            'Self = "Open"',
          ),
        );

        final entry = find.byKey(ValueKey('$prefix-Closed'));
        expect(entry, findsOneWidget);
        expect(
          _ink(tester, entry, 'Closed'),
          _red,
          reason: 'every entry of the set is drawn, as FileMaker does',
        );
        expect(_fillBehind(tester, entry), _yellow);
      });

      testWidgets('$style, when the rule is false', (tester) async {
        await _pump(
          tester,
          _layout(
            FieldBindingModel(
              fieldName: 'status',
              controlStyle: style,
              valueListId: _statuses.id,
            ),
            'Self = "Open"',
          ),
          rows: [
            {'id': '2', 'status': 'Closed'},
          ],
        );

        final entry = find.byKey(ValueKey('$prefix-Open'));
        expect(_ink(tester, entry, 'Open'), isNot(_red));
        expect(_fillBehind(tester, entry), isNull);
      });
    }
  });
}
