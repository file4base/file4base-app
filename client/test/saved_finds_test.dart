import 'dart:convert';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/widgets/file4base_menu_bar.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/data_browser/saved_finds_dialog.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// Saved finds: naming a set of find requests and running it again (#34).
void main() {
  SavedFindModel savedFind(String name, List<Map<String, dynamic>> requests) {
    return SavedFindModel.fromJson({
      'id': 'find-${name.toLowerCase()}',
      'name': name,
      'table_name': 'customers',
      'requests': requests,
      'created_by': 'u-1',
    });
  }

  group('SavedFindModel', () {
    test('reads the requests as they were typed, omit included', () {
      final model = savedFind('New York or London', [
        {
          'values': {'city': 'New York'},
          'omit': false,
        },
        {
          'values': {'status': 'New'},
          'omit': true,
        },
      ]);

      expect(model.requests, hasLength(2));
      expect(model.requests.first.values['city'], 'New York');
      expect(model.requests.first.omit, isFalse);
      expect(model.requests.last.omit, isTrue);
      expect(model.summary, contains('omit status New'));
    });

    test('a find from a solution file has no owner account', () {
      final model = SavedFindModel.fromJson({
        'id': 'f1',
        'name': 'Paris',
        'table_name': 'customers',
        'requests': [
          {
            'values': {'city': 'Paris'},
          },
        ],
      });
      expect(model.createdBy, isEmpty);
      expect(model.requests.single.omit, isFalse);
    });

    test('a request serializes back to what the server stores', () {
      const request = SavedFindRequestModel(values: {'city': 'Paris'}, omit: true);
      expect(request.toJson(), {
        'values': {'city': 'Paris'},
        'omit': true,
      });
    });
  });

  group('Records > Saved Finds menu', () {
    Future<void> openRecordsMenu(WidgetTester tester, Widget menuBar) async {
      tester.view.physicalSize = const Size(1400, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());
      await tester.pumpWidget(MaterialApp(home: Scaffold(body: menuBar)));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Records'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Saved Finds'));
      await tester.pumpAndSettle();
    }

    testWidgets('lists the finds of the table and runs the one chosen', (tester) async {
      SavedFindModel? ran;
      await openRecordsMenu(
        tester,
        File4BaseMenuBar(
          activeMode: OperationalMode.browse,
          onModeChanged: (_) {},
          onManageDatabase: () {},
          onOpenRemote: () {},
          onAbout: () {},
          isToolbarVisible: true,
          onToggleToolbar: (_) {},
          savedFinds: [
            savedFind('New York or London', [
              {
                'values': {'city': 'New York'},
              },
            ]),
            savedFind('Paid in 2011', [
              {
                'values': {'paid': '2011'},
              },
            ]),
          ],
          onRunSavedFind: (f) => ran = f,
          onSaveCurrentFind: () {},
          onManageSavedFinds: () {},
        ),
      );

      expect(find.text('New York or London'), findsOneWidget);
      expect(find.text('Paid in 2011'), findsOneWidget);
      expect(find.text('No saved finds for this table'), findsNothing);

      await tester.tap(find.text('Paid in 2011'));
      await tester.pumpAndSettle();
      expect(ran?.name, 'Paid in 2011');
    });

    testWidgets('says so when the table has none, and cannot edit nothing', (tester) async {
      await openRecordsMenu(
        tester,
        File4BaseMenuBar(
          activeMode: OperationalMode.browse,
          onModeChanged: (_) {},
          onManageDatabase: () {},
          onOpenRemote: () {},
          onAbout: () {},
          isToolbarVisible: true,
          onToggleToolbar: (_) {},
          savedFinds: const [],
          onSaveCurrentFind: () {},
          onManageSavedFinds: () {},
        ),
      );

      expect(find.text('No saved finds for this table'), findsOneWidget);
      final edit = tester.widget<MenuItemButton>(
        find.ancestor(of: find.text('Edit Saved Finds...'), matching: find.byType(MenuItemButton)),
      );
      expect(edit.onPressed, isNull);
    });
  });

  group('SaveFindDialog', () {
    testWidgets('shows what will be saved and refuses a name already taken', (tester) async {
      String? saved;
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => TextButton(
              onPressed: () async {
                saved = await SaveFindDialog.show(
                  context,
                  tableLabel: 'Customers',
                  existing: [savedFind('Paris', const [])],
                  requests: const [
                    SavedFindRequestModel(values: {'city': 'New York'}),
                    SavedFindRequestModel(values: {'status': 'New'}, omit: true),
                  ],
                );
              },
              child: const Text('open'),
            ),
          ),
        ),
      ));
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();

      // The requests are shown, so the name is given to something visible.
      expect(find.text('On Customers, 2 request(s).'), findsOneWidget);
      expect(find.text('Find: city New York'), findsOneWidget);
      expect(find.text('Omit: status New'), findsOneWidget);

      // Saving is refused until there is a name, and refused again for one
      // the table already has.
      FilledButton saveButton() => tester.widget<FilledButton>(
            find.ancestor(of: find.text('Save'), matching: find.byType(FilledButton)),
          );
      expect(saveButton().onPressed, isNull);

      await tester.enterText(find.byType(TextField), 'paris');
      await tester.pumpAndSettle();
      expect(find.textContaining('already has a find called'), findsOneWidget);
      expect(saveButton().onPressed, isNull);

      await tester.enterText(find.byType(TextField), 'New York or London');
      await tester.pumpAndSettle();
      expect(saveButton().onPressed, isNotNull);
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(saved, 'New York or London');
    });
  });

  group('ManageSavedFindsDialog', () {
    testWidgets('renames through the server and reports a refusal', (tester) async {
      final requests = <String>[];
      final client = MockClient((request) async {
        requests.add('${request.method} ${request.url.path}');
        if (request.method == 'PUT' && request.url.path.endsWith('/find-paris')) {
          final body = jsonDecode(request.body) as Map<String, dynamic>;
          return http.Response(
            jsonEncode({
              'id': 'find-paris',
              'name': body['name'],
              'table_name': 'customers',
              'requests': body['requests'],
            }),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        if (request.method == 'DELETE') {
          return http.Response(
            jsonEncode({'title': 'Forbidden', 'detail': 'this find was saved by another account'}),
            403,
            headers: {'content-type': 'application/problem+json'},
          );
        }
        return http.Response('Not found', 404);
      });

      final apiClient = ApiClient(baseUrl: 'http://test-server:8080', httpClient: client);
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: ManageSavedFindsDialog(
            apiClient: apiClient,
            tableLabel: 'Customers',
            finds: [
              savedFind('Paris', [
                {
                  'values': {'city': 'Paris'},
                },
              ]),
            ],
          ),
        ),
      ));
      await tester.pumpAndSettle();
      expect(find.text('Paris'), findsOneWidget);
      expect(find.textContaining('city Paris'), findsOneWidget);

      // Rename
      await tester.tap(find.byTooltip('Rename'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).last, 'Paris customers');
      await tester.tap(find.text('Rename'));
      await tester.pumpAndSettle();
      expect(find.text('Paris customers'), findsOneWidget);
      expect(requests, contains('PUT /api/v1/saved-finds/find-paris'));

      // A refused delete says so and leaves the find in the list.
      await tester.tap(find.byTooltip('Delete'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'Delete'));
      await tester.pumpAndSettle();
      expect(find.textContaining('saved by another account'), findsOneWidget);
      expect(find.text('Paris customers'), findsOneWidget);
    });
  });

  group('Criteria on a related field (#46)', () {
    test('a related key round-trips through its parser', () {
      final key = DataBrowserWidgetState.relatedCriterionKey('rel-1', 'occ-2', 'company_address');
      expect(key, 'rel:rel-1:occ-2:company_address');

      final parsed = DataBrowserWidgetState.parseRelatedCriterionKey(key);
      expect(parsed?.relationshipId, 'rel-1');
      expect(parsed?.occurrence, 'occ-2');
      expect(parsed?.field, 'company_address');

      // An own field is not a related key, so the two cannot be confused.
      expect(DataBrowserWidgetState.parseRelatedCriterionKey('company_address'), isNull);
      expect(DataBrowserWidgetState.parseRelatedCriterionKey('rel:incomplete'), isNull);
    });

    test('a criterion on a related field names the relationship it reaches through', () {
      final criteria = DataBrowserWidgetState.criteriaFor({
        'last_name': 'Durand',
        DataBrowserWidgetState.relatedCriterionKey('rel-1', 'occ-2', 'company_address'): '*Paris*',
      });

      final own = criteria.firstWhere((c) => c['field_name'] == 'last_name');
      expect(own.containsKey('relationship_id'), isFalse);

      final related = criteria.firstWhere((c) => c['field_name'] == 'company_address');
      expect(related['relationship_id'], 'rel-1');
      expect(related['occurrence'], 'occ-2');
      // The operators are the ones an own field takes.
      expect(related['operator'], 'LIKE');
      expect(related['value'], '%Paris%');
    });

    test('an occurrence is left out when the relationship has only one side to read', () {
      final criteria = DataBrowserWidgetState.criteriaFor({
        DataBrowserWidgetState.relatedCriterionKey('rel-1', '', 'company'): '=DEF Ltd.',
      });
      expect(criteria.single['relationship_id'], 'rel-1');
      expect(criteria.single.containsKey('occurrence'), isFalse);
      expect(criteria.single['operator'], '=');
      expect(criteria.single['value'], 'DEF Ltd.');
    });
  });
}
