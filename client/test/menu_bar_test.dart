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
}
