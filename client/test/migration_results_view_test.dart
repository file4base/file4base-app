// #198 — the end of a FileMaker migration shows what to review, grouped by
// kind, and the accounts that cannot sign in until a password is set.

import 'dart:convert';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/security/manage_security_dialog.dart';
import 'package:file4base_client/features/solution_manager/filemaker_migration_dialog.dart';
import 'package:file4base_client/features/solution_manager/migration_results_view.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// A synthetic apply response: two quarantined triggers, an unsupported
/// script step, a rejected field, a value list that offers no values, and
/// two accounts migrated without a password.
Map<String, dynamic> _applyResponse() => {
      'status': 'migrated',
      'database': 'greenhouse',
      'records_inserted': 12,
      'import_report': {
        'solution_name': 'Greenhouse',
        'tables_created': 2,
        'columns_created': 9,
        'layouts_created': 3,
        'scripts_created': 2,
        'value_lists_created': 1,
        'accounts_created': 2,
        'accounts_pending_password': ['botanist', 'gardener'],
        'warnings': ['value list "Species" was restored as saved but offers no values: field "latin" is a summary'],
      },
      'report': {
        'source_format': 'filemaker_ddr_xml',
        'solution_name': 'Greenhouse',
        'triggers_quarantined': true,
        'accounts_disabled': true,
        'counts': {'tables': 2, 'fields': 9, 'layouts': 3, 'scripts': 2, 'records_ready': 12},
        'preserved': [],
        'translated': [],
        'unsupported': [
          {
            'category': 'unsupported',
            'domain': 'script',
            'source_name': 'Water Plants > Perform AppleScript (step 3)',
            'target_name': 'Water Plants',
            'detail': 'Unsupported FileMaker script step',
          },
        ],
        'rejected': [
          {
            'category': 'rejected',
            'domain': 'field',
            'source_name': 'Plants::Photo',
            'detail': 'Container stored externally without its file',
          },
        ],
        'manual_action_required': [
          {
            'category': 'manual_action_required',
            'domain': 'trigger',
            'source_name': 'Plant Form (OnRecordLoad -> Water Plants)',
            'target_name': 'Plant Form',
            'detail': 'Layout script trigger quarantined on import',
          },
          {
            'category': 'manual_action_required',
            'domain': 'trigger',
            'source_name': 'Plant List (OnLayoutEnter -> Sort)',
            'target_name': 'Plant List',
            'detail': 'Layout script trigger quarantined on import',
          },
        ],
      },
    };

/// The server, as far as these tests need it. Records the account updates.
class _FakeServer {
  final users = <Map<String, dynamic>>[
    {'id': 'u-owner', 'username': 'olivia', 'role': 'owner', 'is_active': true, 'password_pending': false},
    {'id': 'u-bot', 'username': 'botanist', 'role': 'user', 'is_active': false, 'password_pending': true},
    {'id': 'u-gar', 'username': 'gardener', 'role': 'user', 'is_active': false, 'password_pending': true},
  ];
  final updates = <String, Map<String, dynamic>>{};

  MockClient get client => MockClient((req) async {
        final path = req.url.path;
        if (path == '/api/v1/security/users' && req.method == 'GET') {
          return http.Response(jsonEncode(users), 200, headers: {'content-type': 'application/json'});
        }
        if (path.startsWith('/api/v1/security/users/') && req.method == 'PUT') {
          final id = path.split('/').last;
          final body = Map<String, dynamic>.from(jsonDecode(req.body) as Map);
          updates[id] = body;
          final u = users.firstWhere((u) => u['id'] == id);
          if (body['password'] != null) u['password_pending'] = false;
          if (body['is_active'] != null) u['is_active'] = body['is_active'];
          return http.Response(jsonEncode({'id': id, 'status': 'updated'}), 200,
              headers: {'content-type': 'application/json'});
        }
        if (path.endsWith('/api/v1/solutions/migrate/preflight')) {
          return http.Response(jsonEncode({'status': 'preflight_ok', 'report': _applyResponse()['report']}), 200,
              headers: {'content-type': 'application/json'});
        }
        if (path.endsWith('/api/v1/solutions/migrate/apply')) {
          return http.Response(jsonEncode(_applyResponse()), 201, headers: {'content-type': 'application/json'});
        }
        if (path == '/api/v1/schemas/layouts') {
          return http.Response('[]', 200, headers: {'content-type': 'application/json'});
        }
        if (path.contains('/permissions')) {
          return http.Response('[]', 200, headers: {'content-type': 'application/json'});
        }
        return http.Response('Not found', 404);
      });
}

void _bigScreen(WidgetTester tester) {
  tester.view.physicalSize = const Size(1280, 1100);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
}

Future<void> _pumpView(
  WidgetTester tester,
  _FakeServer server, {
  Future<void> Function()? onReviewTriggers,
}) async {
  _bigScreen(tester);
  final api = ApiClient(baseUrl: 'http://localhost:8080', httpClient: server.client);
  await tester.pumpWidget(
    MaterialApp(
      theme: ThemeData.dark(),
      home: Scaffold(
        body: SingleChildScrollView(
          child: MigrationResultsView(
            result: MigrationApplyResultModel.fromJson(_applyResponse()),
            apiClient: api,
            onReviewTriggers: onReviewTriggers,
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  test('the apply response keeps the import report', () {
    final result = MigrationApplyResultModel.fromJson(_applyResponse());
    expect(result.importReport.accountsPendingPassword, ['botanist', 'gardener']);
    expect(result.importReport.warnings, hasLength(1));
    expect(result.importReport.counts['tables_created'], 2);
    expect(result.importReport.counts.containsKey('solution_name'), isFalse);

    // An older server sends no import report.
    final legacy = MigrationApplyResultModel.fromJson({'status': 'migrated', 'database': 'x', 'report': {}});
    expect(legacy.importReport.accountsPendingPassword, isEmpty);
    expect(legacy.importReport.warnings, isEmpty);
  });

  test('review items are grouped by kind, triggers first', () {
    final groups = groupMigrationReviewItems(MigrationApplyResultModel.fromJson(_applyResponse()));
    expect(groups.map((g) => g.kind), ['trigger', 'script', 'field', 'value_list']);
    expect(groups.first.label, 'Script triggers');
    expect(groups.first.items, hasLength(2));
    expect(groups.firstWhere((g) => g.kind == 'field').items.single.severity, MigrationReviewSeverity.rejected);
    expect(groups.last.items.single.severity, MigrationReviewSeverity.notWorking);
    expect(groups[1].toText(), contains('[Unsupported] Water Plants > Perform AppleScript (step 3) → Water Plants'));
    expect(migrationKindLabel('layout_object'), 'Layout object');
  });

  testWidgets('shows counts, groups and copies them as text', (tester) async {
    final copied = <String>[];
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, (call) async {
      if (call.method == 'Clipboard.setData') {
        copied.add((call.arguments as Map)['text'] as String);
      }
      return null;
    });
    addTearDown(() => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, null));

    var reviewed = 0;
    await _pumpView(tester, _FakeServer(), onReviewTriggers: () async => reviewed++);

    expect(find.textContaining('Migration committed to database "greenhouse"'), findsOneWidget);
    expect(find.text('Tables: 2'), findsOneWidget);
    expect(find.text('Fields: 9'), findsOneWidget);
    expect(find.text('Records: 12'), findsOneWidget);
    expect(find.text('To review (5)'), findsOneWidget);
    expect(find.text('Script triggers (2)'), findsOneWidget);
    expect(find.text('Scripts (1)'), findsOneWidget);
    expect(find.text('Fields (1)'), findsOneWidget);
    expect(find.text('Value lists (1)'), findsOneWidget);
    expect(find.text('1 rejected'), findsOneWidget);

    // Folded until opened, then readable.
    expect(find.textContaining('Unsupported FileMaker script step', findRichText: true), findsNothing);
    await tester.tap(find.text('Scripts (1)'));
    await tester.pumpAndSettle();
    expect(find.textContaining('Unsupported FileMaker script step', findRichText: true), findsOneWidget);

    await tester.tap(find.byKey(const Key('migration-copy-script')));
    await tester.pump();
    expect(copied.last, startsWith('Scripts (1)\n  [Unsupported] Water Plants'));

    await tester.tap(find.byKey(const Key('migration-copy-all')));
    await tester.pump();
    expect(find.text('Copied'), findsOneWidget);
    expect(copied.last, contains('FileMaker migration of "Greenhouse" into database "greenhouse"'));
    expect(copied.last, contains('Script triggers (2)'));
    expect(copied.last, contains('[Not working yet] value list "Species"'));
    await tester.pump(const Duration(seconds: 3));
    expect(find.text('Copied'), findsNothing);

    await tester.tap(find.byKey(const Key('migration-review-triggers')));
    await tester.pump();
    expect(reviewed, 1);
  });

  testWidgets('says so when the clipboard refuses the text', (tester) async {
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, (call) async {
      if (call.method == 'Clipboard.setData') throw PlatformException(code: 'denied');
      return null;
    });
    addTearDown(() => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, null));
    await _pumpView(tester, _FakeServer());
    await tester.tap(find.byKey(const Key('migration-copy-all')));
    await tester.pumpAndSettle();
    expect(find.textContaining('The clipboard is not available here'), findsOneWidget);
    expect(find.text('Copied'), findsNothing);
  });

  testWidgets('an account of the same name that has a password is left alone', (tester) async {
    final server = _FakeServer();
    server.users.firstWhere((u) => u['username'] == 'gardener')
      ..['password_pending'] = false
      ..['is_active'] = true;
    await _pumpView(tester, server);
    expect(find.byKey(const Key('set-password-botanist')), findsOneWidget);
    expect(find.byKey(const Key('set-password-gardener')), findsNothing);
    expect(find.text('Can sign in'), findsOneWidget);
  });

  testWidgets('lists the accounts waiting for a password and sets one', (tester) async {
    final server = _FakeServer();
    await _pumpView(tester, server);

    expect(find.text('Accounts waiting for a password (2 of 2)'), findsOneWidget);
    expect(find.textContaining('cannot sign in until an administrator sets a password'), findsOneWidget);
    expect(find.text('Cannot sign in'), findsNWidgets(2));
    expect(find.text('olivia'), findsNothing, reason: 'only the migrated accounts are listed');

    await tester.tap(find.byKey(const Key('set-password-botanist')));
    await tester.pumpAndSettle();
    expect(find.text('Set password: botanist'), findsOneWidget);
    expect(find.textContaining('cannot sign in until one is set'), findsOneWidget);

    // A blank password and a mismatch are refused without a request.
    await tester.enterText(find.byKey(const Key('set-password-new')), '   ');
    await tester.enterText(find.byKey(const Key('set-password-confirm')), '   ');
    await tester.tap(find.byKey(const Key('set-password-save')));
    await tester.pumpAndSettle();
    expect(find.text('Password cannot be empty.'), findsOneWidget);

    await tester.enterText(find.byKey(const Key('set-password-new')), 'Fern-2026-long');
    await tester.enterText(find.byKey(const Key('set-password-confirm')), 'Fern-2026-lonG');
    await tester.tap(find.byKey(const Key('set-password-save')));
    await tester.pumpAndSettle();
    expect(find.text('Passwords do not match.'), findsOneWidget);
    expect(server.updates, isEmpty);

    await tester.enterText(find.byKey(const Key('set-password-confirm')), 'Fern-2026-long');
    // Activating the account is offered and on by default.
    final activate = tester.widget<CheckboxListTile>(find.byKey(const Key('set-password-activate')));
    expect(activate.value, isTrue);
    await tester.tap(find.byKey(const Key('set-password-save')));
    await tester.pumpAndSettle();

    // The stored role is kept (an empty role), the account activated.
    expect(server.updates['u-bot'], {'role': '', 'password': 'Fern-2026-long', 'is_active': true});
    expect(find.text('Set password: botanist'), findsNothing);
    expect(find.text('Accounts waiting for a password (1 of 2)'), findsOneWidget);
    expect(find.text('Can sign in'), findsOneWidget);
    expect(find.text('Cannot sign in'), findsOneWidget);
  });

  testWidgets('a password can be set without activating the account', (tester) async {
    final server = _FakeServer();
    await _pumpView(tester, server);

    await tester.tap(find.byKey(const Key('set-password-gardener')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('set-password-new')), 'Moss-2026-long');
    await tester.enterText(find.byKey(const Key('set-password-confirm')), 'Moss-2026-long');
    await tester.tap(find.byKey(const Key('set-password-activate')));
    await tester.pumpAndSettle();
    expect(find.textContaining('stays inactive'), findsOneWidget);
    await tester.tap(find.byKey(const Key('set-password-save')));
    await tester.pumpAndSettle();

    expect(server.updates['u-gar'], {'role': '', 'password': 'Moss-2026-long'});
    expect(find.text('Password set, inactive'), findsOneWidget);
  });

  testWidgets('the migration dialog ends on the results', (tester) async {
    _bigScreen(tester);
    final api = ApiClient(baseUrl: 'http://localhost:8080', httpClient: _FakeServer().client);
    await tester.pumpWidget(
      MaterialApp(
        theme: ThemeData.dark(),
        home: Scaffold(body: FileMakerMigrationDialog(apiClient: api)),
      ),
    );

    await tester.enterText(
      find.byKey(const Key('migration_design_xml_input')),
      '<FMPReport><File name="Greenhouse.fmp12"/></FMPReport>',
    );
    await tester.tap(find.byKey(const Key('migration_preflight_btn')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('migration_apply_btn')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('migration-results')), findsOneWidget);
    expect(find.byKey(const Key('migration-pending-accounts')), findsOneWidget);
    expect(find.text('To review (5)'), findsOneWidget);
    expect(find.byKey(const Key('migration-review-triggers')), findsOneWidget);
    // Applying again is not offered; the conversion report stays at hand.
    expect(find.byKey(const Key('migration_apply_btn')), findsNothing);
    expect(find.byKey(const Key('migration_done_btn')), findsOneWidget);
    expect(find.byKey(const Key('migration-full-report')), findsOneWidget);
  });

  testWidgets('Manage Security marks and filters the accounts without a password', (tester) async {
    _bigScreen(tester);
    final api = ApiClient(baseUrl: 'http://localhost:8080', httpClient: _FakeServer().client);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ManageSecurityDialog(
            apiClient: api,
            currentUser: const UserModel(id: 'u-owner', username: 'olivia', role: 'owner'),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('password-pending-badge-botanist')), findsOneWidget);
    expect(find.byKey(const Key('password-pending-badge-olivia')), findsNothing);
    expect(find.textContaining('2 accounts have no password'), findsOneWidget);

    await tester.tap(find.byKey(const Key('password-pending-filter')));
    await tester.pumpAndSettle();
    expect(find.text('olivia'), findsNothing);
    expect(find.text('botanist'), findsOneWidget);
    expect(find.text('gardener'), findsOneWidget);

    await tester.tap(find.byTooltip('Set a password so that it can sign in').first);
    await tester.pumpAndSettle();
    expect(find.text('Set password: botanist'), findsOneWidget);
    await tester.enterText(find.byKey(const Key('set-password-new')), 'Fern-2026-long');
    await tester.enterText(find.byKey(const Key('set-password-confirm')), 'Fern-2026-long');
    await tester.tap(find.byKey(const Key('set-password-save')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('password-pending-badge-botanist')), findsNothing);
    expect(find.textContaining('1 account has no password'), findsOneWidget);

    await tester.tap(find.byKey(const Key('password-pending-filter')));
    await tester.pumpAndSettle();
    expect(find.text('olivia'), findsOneWidget);
  });
}
