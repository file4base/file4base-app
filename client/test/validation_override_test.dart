// FileMaker's "Allow user to override during data entry": a value breaking
// a rule that allows it is kept when the person says so.

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/widgets/validation_override.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _table = TableModel(id: 't1', name: 'parts', displayName: 'Parts', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'code', displayName: 'Code', fieldType: 'TEXT'),
]);

ApiException _invalid({required bool overridable}) => ApiException(
      statusCode: 422,
      title: 'Validation Failed',
      detail: 'The code has three characters',
      raw: {
        'invalid_params': [
          {'name': 'code', 'reason': 'calculation', 'overridable': overridable},
        ],
      },
    );

/// Refuses a code that is not three characters long, unless the write
/// overrides the rule (when [overridable]).
class _Api extends ApiClient {
  _Api({this.overridable = true}) : super(baseUrl: 'http://localhost:1');
  final bool overridable;
  final rows = <Map<String, dynamic>>[
    {'id': '1', 'code': 'ABC'},
  ];
  final updates = <String>[];

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [for (final r in rows) Map<String, dynamic>.from(r)];

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> data,
      {bool script = false, bool overrideValidation = false}) async {
    updates.add('${data['code']} override=$overrideValidation');
    if ('${data['code']}'.length != 3 && !(overridable && overrideValidation)) {
      throw _invalid(overridable: overridable);
    }
    rows.first.addAll(data);
    return Map.of(rows.first);
  }
}

Future<DataBrowserWidgetState> _browse(WidgetTester tester, _Api api) async {
  tester.view.physicalSize = const Size(1400, 1000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  final key = GlobalKey<DataBrowserWidgetState>();
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(body: DataBrowserWidget(key: key, table: _table, apiClient: api, mode: OperationalMode.browse)),
  ));
  await tester.pumpAndSettle();
  return key.currentState!;
}

/// Types [text] in the code box, as data entry, and lets the save go.
Future<void> _type(WidgetTester tester, DataBrowserWidgetState state, String text) async {
  await tester.enterText(find.byType(TextField).last, text);
  await _settle(tester, const Duration(milliseconds: 900));
}

/// Lets the write and the dialog run. A field being saved shows a spinner,
/// so the frames are pumped rather than settled.
Future<void> _settle(WidgetTester tester, [Duration first = Duration.zero]) async {
  await tester.pump(first);
  for (var i = 0; i < 5; i++) {
    await tester.pump(const Duration(milliseconds: 100));
  }
}

void main() {
  group('overridableValidationMessage', () {
    test('is the message of a 422 whose every broken rule allows the override', () {
      expect(overridableValidationMessage(_invalid(overridable: true)), 'The code has three characters');
    });
    test('is null for a rule that does not allow it, or any other error', () {
      expect(overridableValidationMessage(_invalid(overridable: false)), isNull);
      expect(overridableValidationMessage(ApiException(statusCode: 409, title: 'x', detail: 'y')), isNull);
      expect(overridableValidationMessage(Exception('z')), isNull);
    });
  });

  testWidgets('Yes keeps the value: the write is sent again with the override', (tester) async {
    final api = _Api();
    final state = await _browse(tester, api);
    await _type(tester, state, 'ABCD');
    expect(find.textContaining('Allow this value?'), findsOneWidget);
    await tester.tap(find.byKey(const Key('validationOverrideYes')));
    await _settle(tester);
    expect(api.updates, ['ABCD override=false', 'ABCD override=true']);
    expect(api.rows.first['code'], 'ABCD');
  });

  testWidgets('No keeps the rule: nothing is saved', (tester) async {
    final api = _Api();
    final state = await _browse(tester, api);
    await _type(tester, state, 'ABCD');
    await tester.tap(find.byKey(const Key('validationOverrideNo')));
    await _settle(tester);
    expect(api.updates, ['ABCD override=false']);
    expect(api.rows.first['code'], 'ABC');
  });

  testWidgets('a rule that does not allow the override asks nothing', (tester) async {
    final api = _Api(overridable: false);
    final state = await _browse(tester, api);
    await _type(tester, state, 'ABCD');
    expect(find.textContaining('Allow this value?'), findsNothing);
    expect(api.updates, ['ABCD override=false']);
  });
}
