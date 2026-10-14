import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/widgets/file4base_menu_bar.dart';
import 'package:file4base_client/features/tools/developer_utilities_dialog.dart';
import 'package:file4base_client/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets(
      'Tools > Developer Utilities... triggers onDeveloperUtilities callback',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    var devUtilitiesCalled = false;

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
            onDeveloperUtilities: () => devUtilitiesCalled = true,
          ),
        ),
      ),
    );
    await tester.pump();

    await tester.tap(find.text('Tools'));
    await tester.pumpAndSettle();
    expect(find.text('Developer Utilities...'), findsOneWidget);
    await tester.tap(find.text('Developer Utilities...'));
    await tester.pumpAndSettle();
    expect(devUtilitiesCalled, isTrue);
  });

  testWidgets(
      'DeveloperUtilitiesDialog renames, clones without records, and permanently locks design',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    Map<String, dynamic>? lastRenameBody;
    Map<String, dynamic>? lastCloneBody;
    Map<String, dynamic>? lastLockBody;
    Map<String, dynamic>? lastOptionsBody;

    final mockHttp = MockClient((request) async {
      if (request.url.path == '/api/v1/solutions/rename' &&
          request.method == 'POST') {
        lastRenameBody = jsonDecode(request.body) as Map<String, dynamic>;
        return http.Response(
          jsonEncode({
            'database': 'bakery_db',
            'solution_name': lastRenameBody!['solution_name'],
            'design_locked': false,
          }),
          200,
        );
      }
      if (request.url.path == '/api/v1/solutions/clone' &&
          request.method == 'POST') {
        lastCloneBody = jsonDecode(request.body) as Map<String, dynamic>;
        return http.Response(
          jsonEncode({
            'status': 'cloned',
            'database': lastCloneBody!['target_database'],
            'solution_name': lastCloneBody!['solution_name'],
            'design_locked': lastCloneBody!['lock_design'],
            'owner_user': 'alice',
            'tables_created': 3,
          }),
          201,
        );
      }
      if (request.url.path == '/api/v1/solutions/options' &&
          request.method == 'PUT') {
        lastOptionsBody = jsonDecode(request.body) as Map<String, dynamic>;
        return http.Response(
          jsonEncode({
            'database': 'bakery_db',
            'solution_name': 'Bakery Suite',
            'design_locked': false,
            'file_options': lastOptionsBody!['file_options'],
          }),
          200,
        );
      }
      if (request.url.path == '/api/v1/solutions/lock-design' &&
          request.method == 'POST') {
        lastLockBody = jsonDecode(request.body) as Map<String, dynamic>;
        return http.Response(
          jsonEncode({
            'database': 'bakery_db',
            'solution_name': 'Bakery Suite',
            'design_locked': true,
          }),
          200,
        );
      }
      return http.Response('Not Found', 404);
    });

    final client =
        ApiClient(baseUrl: 'http://localhost:8080', httpClient: mockHttp);
    SolutionMetaModel? latestMeta;
    CloneResultModel? clonedResult;
    bool? kioskChanged;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: DeveloperUtilitiesDialog(
            apiClient: client,
            initialMeta: const SolutionMetaModel(
              database: 'bakery_db',
              solutionName: 'Bakery',
            ),
            onMetaChanged: (m) => latestMeta = m,
            onSolutionCloned: (c) => clonedResult = c,
            onKioskModeChanged: (k) => kioskChanged = k,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    // 1. Rename solution
    await tester.enterText(
      find.byKey(const Key('dev_util_rename_field')),
      'Bakery Suite',
    );
    await tester.tap(find.byKey(const Key('dev_util_rename_button')));
    await tester.pumpAndSettle();
    expect(lastRenameBody?['solution_name'], equals('Bakery Suite'));
    expect(latestMeta?.solutionName, equals('Bakery Suite'));

    // 2. Clone without records with locked design
    await tester.enterText(
      find.byKey(const Key('dev_util_clone_db_field')),
      'bakery_prod',
    );
    await tester.enterText(
      find.byKey(const Key('dev_util_clone_name_field')),
      'Bakery Production',
    );
    await tester.tap(find.byKey(const Key('dev_util_clone_lock_checkbox')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('dev_util_clone_button')));
    await tester.pumpAndSettle();
    expect(lastCloneBody?['target_database'], equals('bakery_prod'));
    expect(lastCloneBody?['solution_name'], equals('Bakery Production'));
    expect(lastCloneBody?['lock_design'], isTrue);
    expect(clonedResult?.database, equals('bakery_prod'));
    expect(clonedResult?.designLocked, isTrue);

    // 3. Enable Kiosk Mode & AES-256-GCM encryption
    await tester.ensureVisible(find.byKey(const Key('dev_util_kiosk_switch')));
    await tester.tap(find.byKey(const Key('dev_util_kiosk_switch')));
    await tester.pumpAndSettle();
    await tester.ensureVisible(
      find.byKey(const Key('dev_util_save_protection_button')),
    );
    await tester.tap(find.byKey(const Key('dev_util_save_protection_button')));
    await tester.pumpAndSettle();
    expect(lastOptionsBody?['file_options']?['kiosk_mode'], isTrue);
    expect(lastOptionsBody?['file_options']?['encryption_enabled'], isTrue);
    expect(kioskChanged, isTrue);

    // 4. Permanently lock current design
    await tester.ensureVisible(
      find.byKey(const Key('dev_util_lock_confirm_checkbox')),
    );
    await tester.tap(find.byKey(const Key('dev_util_lock_confirm_checkbox')));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byKey(const Key('dev_util_lock_button')));
    await tester.tap(find.byKey(const Key('dev_util_lock_button')));
    await tester.pumpAndSettle();
    expect(lastLockBody?['confirm'], isTrue);
    expect(latestMeta?.designLocked, isTrue);
    expect(find.text('Design Locked'), findsOneWidget);
  });

  testWidgets(
      'DeveloperUtilitiesDialog configures Kiosk Primary Layout, audits dead-end layouts, injects Home/Exit buttons, and triggers Kiosk simulation',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    final updatedLayouts = <String, Map<String, dynamic>>{};
    Map<String, dynamic>? savedFileOptions;

    final mockHttp = MockClient((request) async {
      if (request.url.path.startsWith('/api/v1/schemas/layouts/') &&
          request.method == 'PUT') {
        final id = request.url.pathSegments.last;
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        updatedLayouts[id] = body;
        return http.Response(
          jsonEncode({
            'id': id,
            'name': body['name'],
            'table_occurrence_id': 'to_1',
            'definition': body['definition'],
          }),
          200,
        );
      }
      if (request.url.path == '/api/v1/solutions/options' &&
          request.method == 'PUT') {
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        savedFileOptions =
            Map<String, dynamic>.from(body['file_options'] as Map);
        return http.Response(
          jsonEncode({
            'database': 'kiosk_db',
            'solution_name': 'Kiosk POS',
            'design_locked': false,
            'file_options': savedFileOptions,
          }),
          200,
        );
      }
      return http.Response('Not Found', 404);
    });

    final client =
        ApiClient(baseUrl: 'http://localhost:8080', httpClient: mockHttp);
    var simulationTriggered = false;

    final layouts = [
      const LayoutModel(
        id: 'lay_home',
        name: 'Welcome Menu',
        tableOccurrenceId: 'to_1',
        definition: {
          'id': 'lay_home',
          'name': 'Welcome Menu',
          'parts': [
            {'id': 'p1', 'type': 'body', 'height': 400}
          ],
          'objects': [],
        },
      ),
      const LayoutModel(
        id: 'lay_orders',
        name: 'Orders Detail',
        tableOccurrenceId: 'to_1',
        definition: {
          'id': 'lay_orders',
          'name': 'Orders Detail',
          'parts': [
            {'id': 'p1', 'type': 'body', 'height': 400}
          ],
          'objects': [],
        },
      ),
    ];

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: DeveloperUtilitiesDialog(
            apiClient: client,
            initialMeta: const SolutionMetaModel(
              database: 'kiosk_db',
              solutionName: 'Kiosk POS',
            ),
            layouts: layouts,
            onSimulateKiosk: () => simulationTriggered = true,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    // Enable Kiosk Mode
    await tester.ensureVisible(find.byKey(const Key('dev_util_kiosk_switch')));
    await tester.tap(find.byKey(const Key('dev_util_kiosk_switch')));
    await tester.pumpAndSettle();

    // Auto-inject Home & Exit buttons into layouts
    final injectBtn = find.byKey(const Key('dev_util_kiosk_inject_buttons'));
    expect(injectBtn, findsOneWidget);
    await tester.ensureVisible(injectBtn);
    await tester.tap(injectBtn);
    await tester.pumpAndSettle();

    expect(updatedLayouts.containsKey('lay_home'), isTrue);
    expect(updatedLayouts.containsKey('lay_orders'), isTrue);

    // Apply Kiosk & Encryption
    final applyBtn = find.byKey(const Key('dev_util_save_protection_button'));
    await tester.ensureVisible(applyBtn);
    await tester.tap(applyBtn);
    await tester.pumpAndSettle();

    expect(savedFileOptions?['kiosk_mode'], isTrue);
    expect(savedFileOptions?['kiosk_primary_layout_id'], equals('lay_home'));
    expect(savedFileOptions?['kiosk_require_login'], isTrue);
    expect(savedFileOptions?['auto_login_enabled'], isFalse);

    // Trigger Kiosk Mode simulation
    final simBtn = find.byKey(const Key('dev_util_kiosk_simulate_button'));
    await tester.ensureVisible(simBtn);
    await tester.tap(simBtn);
    await tester.pumpAndSettle();
    expect(simulationTriggered, isTrue);
  });
}
