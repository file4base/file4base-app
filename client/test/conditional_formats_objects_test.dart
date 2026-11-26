// #222 follow-up — conditional formats of objects other than an edit box:
// a text and a button, whose Self is the text they show (FileMaker's Self
// is "the content of the object"), and the boxes a field is drawn in its
// own way, a primary key's and a container's. Portal rows are in
// portal_test.dart. Synthetic data only.

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
      name: 'status',
      displayName: 'Status',
      fieldType: 'TEXT',
    ),
    ColumnModel(
      id: 'c2',
      tableId: 't1',
      name: 'photo',
      displayName: 'Photo',
      fieldType: 'CONTAINER',
    ),
  ],
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
  _Api() : super(baseUrl: 'http://localhost:1');

  final rows = [
    {'id': 'A-1', 'status': 'Open', 'photo': ''},
    {'id': 'B-2', 'status': 'Closed', 'photo': ''},
  ];

  /// Each expression asked, with its Self.
  final asked = <String>[];

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
  Future<List<ValueListModel>> listValueLists() async => const [];

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
    for (final (i, e) in expressions.indexed) {
      asked.add('$e with ${selfOf(i)?.toJson()}');
    }
    // `Self = "…"` and `IsEmpty(Self)`, as the server answers them: Self
    // is the text given, or the field named of the record evaluated.
    EvaluationModel answer(String id, int i) {
      final e = expressions[i];
      final s = selfOf(i);
      if (s == null) {
        return EvaluationModel(expression: e, error: 'Self is unknown here');
      }
      final value =
          s.text ?? '${rows.firstWhere((r) => r['id'] == id)[s.field] ?? ''}';
      final wanted = RegExp(r'^Self = "(.*)"$').firstMatch(e)?.group(1);
      final truth = e == 'IsEmpty(Self)' ? value.isEmpty : value == wanted;
      return EvaluationModel(expression: e, text: truth ? '1' : '0');
    }

    return {
      for (final id in recordIds)
        id: [for (final (i, _) in expressions.indexed) answer(id, i)],
    };
  }
}

LayoutDefinitionModel _layout(
  LayoutObjectModel object, {
  String view = 'form',
}) => LayoutDefinitionModel(
  id: 'l',
  name: 'Orders',
  tableOccurrence: 'occ-orders',
  defaultView: view,
  parts: const [LayoutPartModel(id: 'b', type: 'body', height: 200)],
  objects: [object],
);

LayoutObjectModel _object(
  String type,
  String formula, {
  String text = '',
  String? field,
}) => LayoutObjectModel(
  id: 'obj',
  type: type,
  x: 10,
  y: 10,
  width: 300,
  height: 60,
  text: text,
  fieldBinding: field == null ? null : FieldBindingModel(fieldName: field),
  style: LayoutObjectStyle(conditionalRules: [_rule(formula)]),
);

Future<_Api> _pump(WidgetTester tester, LayoutDefinitionModel layout) async {
  tester.view.physicalSize = const Size(1400, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final api = _Api();
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

/// The text widget drawing [text] in [style]'s font family, if given.
Text _text(WidgetTester tester, String text, {String? family}) =>
    tester.widget<Text>(
      find.byWidgetPredicate(
        (w) =>
            w is Text &&
            w.data == text &&
            (family == null || w.style?.fontFamily == family),
      ),
    );

/// Whether a box filled yellow holds [of].
bool _filledYellow(WidgetTester tester, Finder of) => tester
    .widgetList<Container>(
      find.ancestor(of: of, matching: find.byType(Container)),
    )
    .any(
      (c) =>
          c.decoration is BoxDecoration &&
          (c.decoration! as BoxDecoration).color == _yellow,
    );

void main() {
  testWidgets("a text is drawn by its rule, Self being the text it shows", (
    tester,
  ) async {
    final api = await _pump(
      tester,
      _layout(_object('label', 'Self = "Paid"', text: 'Paid')),
    );

    expect(_text(tester, 'Paid').style?.color, _red);
    expect(_filledYellow(tester, find.text('Paid')), isTrue);
    expect(api.asked.toSet(), {'Self = "Paid" with {text: Paid}'});
  });

  testWidgets('a rule false on a text leaves it as it is', (tester) async {
    await _pump(tester, _layout(_object('label', 'Self = "x"', text: 'Paid')));

    expect(_text(tester, 'Paid').style?.color, isNot(_red));
    expect(_filledYellow(tester, find.text('Paid')), isFalse);
  });

  testWidgets(
    "a text merging a field shows another text on each record: its Self is unknown",
    (tester) async {
      final api = await _pump(
        tester,
        _layout(_object('label', 'Self = "Open"', text: '{{status}}')),
      );

      expect(_text(tester, 'Open').style?.color, isNot(_red));
      expect(api.asked.toSet(), {'Self = "Open" with null'});
    },
  );

  testWidgets("a button is drawn by its rule, Self being its label", (
    tester,
  ) async {
    final api = await _pump(
      tester,
      _layout(_object('button', 'Self = "Send"', text: 'Send')),
    );

    final button = tester.widget<ElevatedButton>(find.byType(ElevatedButton));
    expect(button.style?.backgroundColor?.resolve({}), _yellow);
    expect(button.style?.foregroundColor?.resolve({}), _red);
    expect(api.asked.toSet(), {'Self = "Send" with {text: Send}'});
  });

  testWidgets('a popover button is drawn by its rule as a button is', (
    tester,
  ) async {
    await _pump(
      tester,
      _layout(_object('popover_button', 'Self = "More"', text: 'More')),
    );

    final button = tester.widget<ElevatedButton>(find.byType(ElevatedButton));
    expect(button.style?.backgroundColor?.resolve({}), _yellow);
  });

  testWidgets("a primary key's box is drawn by its rule, Self being the key", (
    tester,
  ) async {
    final api = await _pump(
      tester,
      _layout(_object('field', 'Self = "A-1"', field: 'id')),
    );

    final key = find.byWidgetPredicate(
      (w) => w is Text && w.data == 'A-1' && w.style?.fontFamily == 'monospace',
    );
    expect(_text(tester, 'A-1', family: 'monospace').style?.color, _red);
    expect(_filledYellow(tester, key), isTrue);
    expect(api.asked.toSet(), {'Self = "A-1" with {field: id}'});
  });

  for (final view in ['form', 'list']) {
    testWidgets("a container's box is drawn by its rule ($view view)", (
      tester,
    ) async {
      await _pump(
        tester,
        _layout(_object('field', 'IsEmpty(Self)', field: 'photo'), view: view),
      );

      final picture = find.byIcon(Icons.image_outlined);
      expect(picture, findsWidgets);
      expect(_filledYellow(tester, picture.first), isTrue);
    });
  }
}
