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

  testWidgets('App renders branding, mode selector and switches mode', (WidgetTester tester) async {
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
    expect(find.text('File4Base'), findsOneWidget);
    expect(find.text('Welcome to File4Base'), findsOneWidget);
    expect(find.text('Manage Database...'), findsOneWidget);

    // Verify 4 Mode buttons in SegmentedButton
    final segmentedButton = find.byType(SegmentedButton<OperationalMode>);
    expect(find.descendant(of: segmentedButton, matching: find.text('Browse')), findsOneWidget);
    final findModeBtn = find.descendant(of: segmentedButton, matching: find.text('Find'));
    final layoutModeBtn = find.descendant(of: segmentedButton, matching: find.text('Layout'));
    final previewModeBtn = find.descendant(of: segmentedButton, matching: find.text('Preview'));

    expect(findModeBtn, findsOneWidget);
    expect(layoutModeBtn, findsOneWidget);
    expect(previewModeBtn, findsOneWidget);

    // Tap Find Mode button
    await tester.tap(findModeBtn);
    await tester.pump();

    // Tap Layout Mode button
    await tester.tap(layoutModeBtn);
    await tester.pump();

    // Tap About Dialog
    await tester.tap(find.byIcon(Icons.info_outline));
    await tester.pumpAndSettle();

    expect(find.text('GNU General Public License v3.0 (GPL-3.0)'), findsOneWidget);
    expect(find.text('Close'), findsOneWidget);

    await tester.tap(find.text('Close'));
    await tester.pumpAndSettle();
  });
}
