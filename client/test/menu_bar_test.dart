import 'package:file4base_client/core/widgets/file4base_menu_bar.dart';
import 'package:file4base_client/features/about/about_dialog.dart';
import 'package:file4base_client/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('File4BaseMenuBar renders 10 canonical menus and switches Records to Requests in Find mode',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    var currentMode = OperationalMode.browse;
    var manageDatabaseCalled = false;
    var openRemoteCalled = false;
    var aboutCalled = false;
    var toolbarVisible = true;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(
            builder: (context, setState) {
              return File4BaseMenuBar(
                activeMode: currentMode,
                onModeChanged: (mode) {
                  setState(() => currentMode = mode);
                },
                onManageDatabase: () => manageDatabaseCalled = true,
                onOpenRemote: () => openRemoteCalled = true,
                onAbout: () => aboutCalled = true,
                isToolbarVisible: toolbarVisible,
                onToggleToolbar: (val) => setState(() => toolbarVisible = val),
              );
            },
          ),
        ),
      ),
    );
    await tester.pump();

    // Verify all 10 root menus are present in Browse mode
    expect(find.text('File'), findsOneWidget);
    expect(find.text('Edit'), findsOneWidget);
    expect(find.text('View'), findsOneWidget);
    expect(find.text('Insert'), findsOneWidget);
    expect(find.text('Format'), findsOneWidget);
    expect(find.text('Records'), findsOneWidget);
    expect(find.text('Scripts'), findsOneWidget);
    expect(find.text('Tools'), findsOneWidget);
    expect(find.text('Window'), findsOneWidget);
    expect(find.text('Help'), findsOneWidget);

    // Re-render in Find mode
    currentMode = OperationalMode.find;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(
            builder: (context, setState) {
              return File4BaseMenuBar(
                activeMode: currentMode,
                onModeChanged: (mode) {
                  setState(() => currentMode = mode);
                },
                onManageDatabase: () => manageDatabaseCalled = true,
                onOpenRemote: () => openRemoteCalled = true,
                onAbout: () => aboutCalled = true,
                isToolbarVisible: toolbarVisible,
                onToggleToolbar: (val) => setState(() => toolbarVisible = val),
              );
            },
          ),
        ),
      ),
    );
    await tester.pump();

    // Records should now be replaced by Requests
    expect(find.text('Records'), findsNothing);
    expect(find.text('Requests'), findsOneWidget);
    expect(manageDatabaseCalled, isFalse);
    expect(openRemoteCalled, isFalse);
    expect(aboutCalled, isFalse);
  });

  testWidgets('File4BaseMenuBar displays Sign In when unauthenticated and Sign Out when authenticated',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    var signInCalled = false;
    var signOutCalled = false;

    // 1. Unauthenticated test
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.browse,
            isAuthenticated: false,
            onSignIn: () => signInCalled = true,
            onSignOut: () => signOutCalled = true,
            onModeChanged: (_) {},
            onManageDatabase: () {},
            onOpenRemote: () {},
            onAbout: () {},
            isToolbarVisible: true,
            onToggleToolbar: (_) {},
          ),
        ),
      ),
    );
    await tester.pump();

    // Tap File menu to open dropdown
    await tester.tap(find.text('File'));
    await tester.pumpAndSettle();

    // Verify Sign In is displayed
    expect(find.text('Sign In...'), findsOneWidget);
    expect(find.text('Sign Out (Lock Session)'), findsNothing);

    await tester.tap(find.text('Sign In...'));
    await tester.pumpAndSettle();
    expect(signInCalled, isTrue);

    // 2. Authenticated test
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.browse,
            onSignIn: () => signInCalled = true,
            onSignOut: () => signOutCalled = true,
            onModeChanged: (_) {},
            onManageDatabase: () {},
            onOpenRemote: () {},
            onAbout: () {},
            isToolbarVisible: true,
            onToggleToolbar: (_) {},
          ),
        ),
      ),
    );
    await tester.pump();

    // Tap File menu to open dropdown
    await tester.tap(find.text('File'));
    await tester.pumpAndSettle();

    // Verify Sign Out is displayed
    expect(find.text('Sign Out (Lock Session)'), findsOneWidget);
    expect(find.text('Sign In...'), findsNothing);

    await tester.tap(find.text('Sign Out (Lock Session)'));
    await tester.pumpAndSettle();
    expect(signOutCalled, isTrue);
  });

  testWidgets('File4BaseMenuBar Help menu renders all functional items and opens dialogs',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    var aboutCalled = false;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.browse,
            onSignIn: () {},
            onSignOut: () {},
            onModeChanged: (_) {},
            onManageDatabase: () {},
            onOpenRemote: () {},
            onAbout: () => aboutCalled = true,
            isToolbarVisible: true,
            onToggleToolbar: (_) {},
          ),
        ),
      ),
    );
    await tester.pump();

    // Tap Help menu
    await tester.tap(find.text('Help'));
    await tester.pumpAndSettle();

    // Verify all items are present
    expect(find.text('File4Base Help'), findsOneWidget);
    expect(find.text('Resource Center'), findsOneWidget);
    expect(find.text('Product Documentation'), findsOneWidget);
    expect(find.text('File4Base Community'), findsOneWidget);
    expect(find.text('Service & Support...'), findsOneWidget);
    expect(find.text('Check for Updates...'), findsOneWidget);
    expect(find.text('About File4Base'), findsOneWidget);

    // Tap About File4Base
    await tester.tap(find.text('About File4Base'));
    await tester.pumpAndSettle();
    expect(aboutCalled, isTrue);

    // Reopen Help and tap Service & Support...
    await tester.tap(find.text('Help'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Service & Support...'));
    await tester.pumpAndSettle();

    // Verify IssuesGuideDialog opens
    expect(find.text('Service & Support — GitHub Issues'), findsOneWidget);
    expect(find.text('View Existing Issues'), findsOneWidget);
    expect(find.text('Open GitHub Issues'), findsOneWidget);
    expect(find.textContaining('[Web Client]'), findsOneWidget);
    expect(find.textContaining('WebDirect'), findsNothing);

    // Close Issues dialog
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(find.text('Service & Support — GitHub Issues'), findsNothing);

    // Reopen Help and tap Check for Updates...
    await tester.tap(find.text('Help'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Check for Updates...'));
    await tester.pumpAndSettle();

    // Verify CheckUpdatesDialog opens
    expect(find.text('Check for Updates'), findsOneWidget);
    expect(find.text('File4Base v0.4.23'), findsOneWidget);
    expect(find.text('View Releases on GitHub'), findsOneWidget);

    // Close Updates dialog
    await tester.tap(find.text('OK'));
    await tester.pumpAndSettle();
    expect(find.text('Check for Updates'), findsNothing);
  });

  testWidgets('File menu has removed Close, Sharing, and Save/Send Records As, and triggers callbacks', (tester) async {
    var changePasswordCalled = false;
    var exportRecordsCalled = false;
    var printCalled = false;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.browse,
            onChangePassword: () => changePasswordCalled = true,
            onExportRecords: () => exportRecordsCalled = true,
            onPrint: () => printCalled = true,
            onModeChanged: (_) {},
            onManageDatabase: () {},
            onOpenRemote: () {},
            onAbout: () {},
            isToolbarVisible: true,
            onToggleToolbar: (_) {},
          ),
        ),
      ),
    );
    await tester.pump();

    // Open File menu
    await tester.tap(find.text('File'));
    await tester.pumpAndSettle();

    // 1. Verify removed items
    expect(find.text('Close'), findsNothing);
    expect(find.text('Sharing'), findsNothing);
    expect(find.text('Save/Send Records As'), findsNothing);

    // 2. Verify present items
    expect(find.text('Change Password...'), findsOneWidget);
    expect(find.text('Export Records...'), findsOneWidget);
    expect(find.text('Print...'), findsOneWidget);

    // 3. Test Change Password callback
    await tester.tap(find.text('Change Password...'));
    await tester.pumpAndSettle();
    expect(changePasswordCalled, isTrue);

    // Reopen File menu & test Export Records callback
    await tester.tap(find.text('File'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Export Records...'));
    await tester.pumpAndSettle();
    expect(exportRecordsCalled, isTrue);

    // Reopen File menu & test Print callback
    await tester.tap(find.text('File'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Print...'));
    await tester.pumpAndSettle();
    expect(printCalled, isTrue);
  });

  testWidgets('File menu has Quit item and triggers onQuit callback', (tester) async {
    var quitCalled = false;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.browse,
            onQuit: () => quitCalled = true,
            onModeChanged: (_) {},
            onManageDatabase: () {},
            onOpenRemote: () {},
            onAbout: () {},
            isToolbarVisible: true,
            onToggleToolbar: (_) {},
          ),
        ),
      ),
    );
    await tester.pump();

    // Open File menu
    await tester.tap(find.text('File'));
    await tester.pumpAndSettle();

    // Verify Quit button is rendered
    expect(find.text('Quit'), findsOneWidget);

    // Tap Quit
    await tester.tap(find.text('Quit'));
    await tester.pumpAndSettle();
    expect(quitCalled, isTrue);
  });

  testWidgets('AboutFile4BaseDialog uses File4Base Web Client name and surfaces third-party license notices',
      (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: AboutFile4BaseDialog(serverStatus: 'Connected'),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('WebDirect'), findsNothing);

    // Switch to System Info tab
    await tester.tap(find.text('System Info'));
    await tester.pumpAndSettle();
    expect(find.text('Flutter 3.x (File4Base Web Client & Desktop Native)'), findsOneWidget);
    expect(find.textContaining('WebDirect'), findsNothing);

    // Switch to Credits tab
    await tester.tap(find.text('Credits').first);
    await tester.pumpAndSettle();
    expect(find.text('Third-Party License Notices'), findsOneWidget);
    expect(find.textContaining('THIRD_PARTY_NOTICES/pdfium/LICENSE'), findsOneWidget);
    expect(find.text('View Open-Source Licenses'), findsOneWidget);
    expect(find.textContaining('WebDirect'), findsNothing);
  });

  testWidgets('Edit menu Undo, Redo, Cut, Copy, Paste, Clear, and Select All operate on focused TextField',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    final controller = TextEditingController(text: 'Hello File4Base');

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Column(
            children: [
              File4BaseMenuBar(
                activeMode: OperationalMode.browse,
                onModeChanged: (_) {},
                onManageDatabase: () {},
                onOpenRemote: () {},
                onAbout: () {},
                isToolbarVisible: true,
                onToggleToolbar: (_) {},
              ),
              Padding(
                padding: const EdgeInsets.all(16),
                child: TextField(
                  key: const ValueKey('test-field'),
                  controller: controller,
                ),
              ),
            ],
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    // Focus the text field and type new text
    await tester.tap(find.byKey(const ValueKey('test-field')));
    await tester.pumpAndSettle();
    controller.text = 'Updated Recipe';
    await tester.pump();

    // 1. Undo restores 'Hello File4Base'
    await tester.tap(find.text('Edit'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Undo'));
    await tester.pumpAndSettle();
    expect(controller.text, 'Hello File4Base');

    // 2. Redo restores 'Updated Recipe'
    await tester.tap(find.text('Edit'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Redo'));
    await tester.pumpAndSettle();
    expect(controller.text, 'Updated Recipe');

    // 3. Select All selects the entire text
    await tester.tap(find.text('Edit'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Select All'));
    await tester.pumpAndSettle();
    expect(controller.selection.baseOffset, 0);
    expect(controller.selection.extentOffset, 'Updated Recipe'.length);

    // 4. Cut removes the selected text and stores it in clipboard
    await tester.tap(find.text('Edit'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Cut'));
    await tester.pumpAndSettle();
    expect(controller.text, isEmpty);

    // 5. Paste restores the cut text
    await tester.tap(find.text('Edit'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste'));
    await tester.pumpAndSettle();
    expect(controller.text, 'Updated Recipe');

    // 6. Clear empties the text field
    await tester.tap(find.text('Edit'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Clear'));
    await tester.pumpAndSettle();
    expect(controller.text, isEmpty);
  });

  testWidgets('Server Tools menu appears after Tools and dispatches onServerToolSelected for all 9 tools',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    final selectedTools = <String>[];

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.browse,
            onModeChanged: (_) {},
            onManageDatabase: () {},
            onOpenRemote: () {},
            onAbout: () {},
            isToolbarVisible: true,
            onToggleToolbar: (_) {},
            onServerToolSelected: selectedTools.add,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Tools'), findsOneWidget);
    expect(find.text('Server Tools'), findsOneWidget);

    final menuItems = <(String, String)>[
      ('Containers & Services...', 'containers'),
      ('Backups & Snapshots...', 'backups'),
      ('Domain & SSL/TLS Certificates (ACME)...', 'domain_tls'),
      ('API Telemetry & Live Metrics...', 'telemetry'),
      ('Connected Clients & Sessions...', 'sessions'),
      ('Scheduled Tasks & Cron Scripts...', 'schedules'),
      ('Audit & Security Logs...', 'audit_logs'),
      ('Database Maintenance & Optimizer...', 'maintenance'),
      ('Server Notifications, SMTP & Webhooks...', 'notifications'),
    ];

    for (final (label, key) in menuItems) {
      await tester.tap(find.text('Server Tools'));
      await tester.pumpAndSettle();
      expect(find.text(label), findsOneWidget);
      await tester.tap(find.text(label));
      await tester.pumpAndSettle();
      expect(selectedTools.last, key);
    }
    expect(selectedTools.length, 9);
  });
}
