import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/widgets/file4base_menu_bar.dart';
import 'package:file4base_client/main.dart';

void main() {
  testWidgets('File4BaseMenuBar renders 10 canonical menus and switches Records to Requests in Find mode',
      (WidgetTester tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    OperationalMode currentMode = OperationalMode.browse;
    bool manageDatabaseCalled = false;
    bool openRemoteCalled = false;
    bool aboutCalled = false;
    bool toolbarVisible = true;

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
      (WidgetTester tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    bool signInCalled = false;
    bool signOutCalled = false;

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
            isAuthenticated: true,
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
      (WidgetTester tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    bool aboutCalled = false;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.browse,
            isAuthenticated: true,
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

  testWidgets('File menu has removed Close, Sharing, and Save/Send Records As, and triggers callbacks', (WidgetTester tester) async {
    bool changePasswordCalled = false;
    bool exportRecordsCalled = false;
    bool printCalled = false;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.browse,
            isAuthenticated: true,
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

  testWidgets('File menu has Quit item and triggers onQuit callback', (WidgetTester tester) async {
    bool quitCalled = false;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.browse,
            isAuthenticated: true,
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
}
