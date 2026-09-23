import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/system/environment_checker.dart';
import 'package:file4base_client/main.dart';

void main() {
  setUp(() {
    EnvironmentChecker.isTestMode = true;
  });

  tearDown(() {
    EnvironmentChecker.isTestMode = false;
  });

  testWidgets('App renders canonical menu, left status sidebar, and switches mode',
      (WidgetTester tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    await tester.pumpWidget(
      const ProviderScope(
        child: File4BaseApp(),
      ),
    );
    await tester.pump();

    // Verify Title and Welcome Screen
    expect(find.text('File'), findsOneWidget);
    expect(find.text('View'), findsOneWidget);
    expect(find.text('Welcome to File4Base'), findsOneWidget);
    expect(find.text('Manage\nDatabase...'), findsOneWidget);

    // Verify mode indicator (present in status bar and sidebar)
    expect(find.text('Browse'), findsAtLeastNWidgets(1));

    // Tap View menu in MenuBar to switch to Layout Mode
    await tester.tap(find.text('View'));
    await tester.pumpAndSettle();

    final layoutMenuItem = find.text('Layout Mode');
    expect(layoutMenuItem, findsOneWidget);
    await tester.tap(layoutMenuItem);
    await tester.pumpAndSettle();

    // Verify status bar now reflects Layout mode
    expect(find.text('Layout'), findsAtLeastNWidgets(1));

    // Open About Dialog via Help menu
    await tester.tap(find.text('Help'));
    await tester.pumpAndSettle();

    final aboutMenuItem = find.text('About File4Base');
    expect(aboutMenuItem, findsOneWidget);
    await tester.tap(aboutMenuItem);
    await tester.pumpAndSettle();

    expect(find.text('About File4Base'), findsAtLeastNWidgets(1));
    expect(find.text('OK'), findsOneWidget);

    await tester.tap(find.text('OK'));
    await tester.pumpAndSettle();
  });
}
