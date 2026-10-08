// #50 — the Data Viewer: the fields of the record in hand, and expressions
// evaluated by the engine that fills the calculation fields.

import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/widgets/file4base_menu_bar.dart';
import 'package:file4base_client/features/tools/data_viewer_dialog.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _table = TableModel(id: 't1', name: 'customers', displayName: 'Customers', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'last_name', displayName: 'Last Name', fieldType: 'TEXT'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'fee_paid', displayName: 'Fee Paid', fieldType: 'NUMBER'),
]);

const _record = {'id': 'r1', 'last_name': 'Durand', 'fee_paid': 100};

/// Answers each expression with something recognisable, and records what was
/// asked for.
ApiClient _client(List<Map<String, dynamic>> asked, {Map<String, Map<String, dynamic>>? answers}) {
  final mock = MockClient((request) async {
    final body = jsonDecode(request.body) as Map<String, dynamic>;
    asked.add(body);
    final expressions = (body['expressions'] as List<dynamic>).cast<String>();
    return http.Response(
      jsonEncode({
        'results': [
          for (final expression in expressions)
            answers?[expression] ??
                {
                  'expression': expression,
                  'value': 200,
                  'text': '200',
                  'result_type': 'NUMBER',
                  'fields': ['fee_paid'],
                },
        ],
      }),
      200,
      headers: {'content-type': 'application/json'},
    );
  });
  return ApiClient(baseUrl: 'http://test-server:8080', httpClient: mock);
}

Future<void> _pump(
  WidgetTester tester,
  ApiClient api, {
  Map<String, dynamic>? record = _record,
  String? recordId = 'r1',
  List<String> watches = const [],
  TableModel? table = _table,
}) async {
  tester.view.physicalSize = const Size(1400, 1000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: DataViewerDialog(
        apiClient: api,
        table: table,
        record: record,
        recordId: recordId,
        initialWatches: watches,
      ),
    ),
  ));
  await tester.pumpAndSettle();
}

void main() {
  group('Current record', () {
    testWidgets('lists every field with what it holds, and marks the empty ones', (tester) async {
      await _pump(tester, _client([]), record: {'id': 'r1', 'last_name': 'Durand', 'fee_paid': null});

      expect(find.textContaining('Last Name'), findsOneWidget);
      expect(find.text('Durand'), findsOneWidget);
      expect(find.text('NUMBER'), findsOneWidget);
      // A field holding nothing says so rather than showing a blank line.
      expect(find.text('empty'), findsWidgets);
    });

    testWidgets('with no record in hand it says so', (tester) async {
      await _pump(tester, _client([]), record: null, recordId: null);
      expect(find.textContaining('No record is in hand'), findsOneWidget);
    });
  });

  group('Watch', () {
    testWidgets('an expression is evaluated against the record in hand', (tester) async {
      final asked = <Map<String, dynamic>>[];
      await _pump(tester, _client(asked));

      await tester.tap(find.text('Watch').last);
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(const ValueKey('viewer-expression')), 'fee_paid * 2');
      await tester.tap(find.byKey(const ValueKey('viewer-add')));
      await tester.pumpAndSettle();

      expect(asked.single['expressions'], ['fee_paid * 2']);
      expect(asked.single['record_id'], 'r1');
      expect(find.byKey(const ValueKey('viewer-result-0')), findsOneWidget);
      expect(find.text('200'), findsOneWidget);
    });

    testWidgets('one expression that does not parse does not blank the others', (tester) async {
      final asked = <Map<String, dynamic>>[];
      final api = _client(asked, answers: {
        'fee_paid *': {'expression': 'fee_paid *', 'error': 'unexpected end (at position 10)'},
      });
      await _pump(tester, api, watches: ['fee_paid *', 'fee_paid + 1']);

      await tester.tap(find.text('Watch').last);
      await tester.pumpAndSettle();

      expect(find.byKey(const ValueKey('viewer-result-error-0')), findsOneWidget);
      expect(find.textContaining('position 10'), findsOneWidget);
      expect(find.byKey(const ValueKey('viewer-result-1')), findsOneWidget);
    });

    testWidgets('a field the table does not have is named, not hidden', (tester) async {
      final api = _client([], answers: {
        'postcode': {
          'expression': 'postcode',
          'text': '',
          'result_type': 'TEXT',
          'fields': ['postcode'],
          'unknown_fields': ['postcode'],
        },
      });
      await _pump(tester, api, watches: ['postcode']);
      await tester.tap(find.text('Watch').last);
      await tester.pumpAndSettle();

      expect(find.textContaining('is not a field of this table'), findsOneWidget);
      expect(find.text('empty'), findsWidgets);
    });

    testWidgets('the result type travels with the request', (tester) async {
      final asked = <Map<String, dynamic>>[];
      await _pump(tester, _client(asked), watches: ['fee_paid']);

      await tester.tap(find.text('Watch').last);
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('viewer-result-type')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Number').last);
      await tester.pumpAndSettle();

      expect(asked.last['result_type'], 'Number');
    });
  });

  group('Variables', () {
    testWidgets('says no script sets one yet rather than showing an empty list', (tester) async {
      await _pump(tester, _client([]));
      await tester.tap(find.text('Variables'));
      await tester.pumpAndSettle();

      expect(find.text('No script step sets a variable yet.'), findsOneWidget);
      expect(find.textContaining('Set Variable, If and Loop stop a script'), findsOneWidget);
    });
  });

  group('Tools menu', () {
    testWidgets('Data Viewer is enabled and opens', (tester) async {
      tester.view.physicalSize = const Size(1400, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      var opened = false;
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.browse,
            onModeChanged: (_) {},
            onManageDatabase: () {},
            onOpenRemote: () {},
            onAbout: () {},
            isToolbarVisible: true,
            onToggleToolbar: (_) {},
            onDataViewer: () => opened = true,
          ),
        ),
      ));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Tools'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Data Viewer'));
      await tester.pumpAndSettle();

      expect(opened, isTrue);
    });
  });
}
