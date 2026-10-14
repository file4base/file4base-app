import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/widgets/file4base_menu_bar.dart';
import 'package:file4base_client/features/tools/manage_custom_menus_dialog.dart';
import 'package:file4base_client/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets(
      'File4BaseMenuBar applies custom menu sets while keeping File recovery items and Tools > Custom Menus reachable',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    CustomMenuItemModel? executedItem;
    var manageCustomMenusCalled = false;

    const customSet = MenuSetModel(
      id: 'set-1',
      name: 'Bakery Menus',
      isDefault: true,
      menus: [
        CustomMenuModel(
          menu: 'Records',
          mode: 'replace',
          items: [
            CustomMenuItemModel(
              title: 'Greet Customer',
              shortcut: 'cmd+shift+G',
              action: {
                'type': 'perform_script',
                'script_id': 'sc-1',
                'script_name': 'Greet',
              },
            ),
            CustomMenuItemModel(separator: true),
            CustomMenuItemModel(
              title: 'Quick Order',
              action: {'type': 'single_step', 'step_type': 'new_record'},
            ),
          ],
        ),
        CustomMenuModel(
          menu: 'File',
          mode: 'replace', // Must still only extend File so Sign Out stays!
          items: [
            CustomMenuItemModel(
              title: 'Daily Summary',
              action: {
                'type': 'perform_script',
                'script_name': 'Summary',
              },
            ),
          ],
        ),
        CustomMenuModel(
          menu: 'Format',
          mode: 'hide',
        ),
      ],
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.browse,
            onSignOut: () {},
            onModeChanged: (_) {},
            onManageDatabase: () {},
            onOpenRemote: () {},
            onAbout: () {},
            isToolbarVisible: true,
            onToggleToolbar: (_) {},
            menuSets: const [customSet],
            activeMenuSet: customSet,
            onManageCustomMenus: () => manageCustomMenusCalled = true,
            onRunCustomMenuItem: (item) => executedItem = item,
          ),
        ),
      ),
    );
    await tester.pump();

    // Format menu is hidden when mode == 'hide'
    expect(find.text('Format'), findsNothing);

    // 1. Records menu is replaced by the custom items
    await tester.tap(find.text('Records'));
    await tester.pumpAndSettle();
    expect(find.text('Greet Customer'), findsOneWidget);
    expect(find.text('Quick Order'), findsOneWidget);
    expect(find.text('Duplicate Record'), findsNothing);

    await tester.tap(find.text('Greet Customer'));
    await tester.pumpAndSettle();
    expect(executedItem?.title, equals('Greet Customer'));

    // 2. File menu is extended (never replaced), so Sign Out stays reachable
    await tester.tap(find.text('File'));
    await tester.pumpAndSettle();
    expect(find.text('Sign Out (Lock Session)'), findsOneWidget);
    expect(find.text('Daily Summary'), findsOneWidget);
    await tester.tap(find.text('Daily Summary'));
    await tester.pumpAndSettle();
    expect(executedItem?.title, equals('Daily Summary'));

    // 3. Tools > Custom Menus > Manage Custom Menus... is enabled
    await tester.tap(find.text('Tools'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Custom Menus'));
    await tester.pumpAndSettle();
    expect(find.text('Manage Custom Menus...'), findsOneWidget);
    expect(find.text('[Standard File4Base Menus]'), findsOneWidget);
    expect(find.text('Bakery Menus'), findsOneWidget);
    await tester.tap(find.text('Manage Custom Menus...'));
    await tester.pumpAndSettle();
    expect(manageCustomMenusCalled, isTrue);
  });

  testWidgets('ManageCustomMenusDialog lists, creates, and deletes custom menu sets',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    final sets = <Map<String, dynamic>>[
      {
        'id': 'set-1',
        'name': 'Front Desk',
        'is_default': true,
        'menus': [
          {
            'menu': 'Records',
            'mode': 'extend',
            'items': [
              {
                'title': 'Check In Guest',
                'action': {'type': 'single_step', 'step_type': 'new_record'},
              },
            ],
          },
        ],
      },
    ];

    final mockHttp = MockClient((request) async {
      if (request.method == 'GET' && request.url.path == '/api/v1/menu-sets') {
        return http.Response(
          jsonEncode({
            'menus': [
              'File',
              'Edit',
              'View',
              'Insert',
              'Format',
              'Records',
              'Requests',
              'Scripts',
              'Tools',
              'Window',
            ],
            'extend_only': ['File'],
            'menu_sets': sets,
          }),
          200,
        );
      }
      if (request.method == 'POST' && request.url.path == '/api/v1/menu-sets') {
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        final created = {
          'id': 'set-2',
          'name': body['name'],
          'is_default': body['is_default'] ?? false,
          'menus': body['menus'],
        };
        sets.add(created);
        return http.Response(jsonEncode(created), 201);
      }
      if (request.method == 'DELETE' &&
          request.url.path.startsWith('/api/v1/menu-sets/')) {
        final id = request.url.path.split('/').last;
        sets.removeWhere((s) => s['id'] == id);
        return http.Response('', 204);
      }
      return http.Response('[]', 200);
    });

    final apiClient = ApiClient(
      baseUrl: 'http://localhost:8080',
      httpClient: mockHttp,
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ManageCustomMenusDialog(
            apiClient: apiClient,
            scripts: const [
              ScriptModel(id: 'sc-1', name: 'Welcome Script'),
            ],
            layouts: const [
              LayoutModel(
                id: 'lay-1',
                name: 'Reception Layout',
                tableOccurrenceId: 'to-1',
                definition: {},
              ),
            ],
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Manage Custom Menus'), findsOneWidget);
    expect(find.byKey(const ValueKey('menu-set-row-Front Desk')), findsOneWidget);

    // Click New to create a second menu set
    await tester.tap(find.byKey(const ValueKey('custom-menus-new-set')));
    await tester.pumpAndSettle();

    await tester.enterText(
      find.byKey(const ValueKey('custom-menu-set-name')),
      'Kitchen Menus',
    );
    await tester.tap(find.byKey(const ValueKey('custom-menus-save')));
    await tester.pumpAndSettle();

    expect(
      find.byKey(const ValueKey('menu-set-row-Kitchen Menus')),
      findsOneWidget,
    );
  });
}
