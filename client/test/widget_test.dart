import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/main.dart';

void main() {
  testWidgets('App renders mode selector and switches mode', (WidgetTester tester) async {
    // Set a wide desktop test resolution
    tester.view.physicalSize = const Size(1280, 800);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    await tester.pumpWidget(
      const ProviderScope(
        child: File4BaseApp(),
      ),
    );

    // Initial state: Browse Mode
    expect(find.text('File4Base'), findsOneWidget);
    expect(find.text('Browse Mode Active'), findsOneWidget);
    expect(find.text('Browse'), findsOneWidget);

    // Tap Find Mode button
    await tester.tap(find.text('Find'));
    await tester.pump();

    // Switched to Find Mode
    expect(find.text('Find Mode Active'), findsOneWidget);

    // Tap Layout Mode button
    await tester.tap(find.text('Layout'));
    await tester.pump();

    // Switched to Layout Mode
    expect(find.text('Layout Designer Active'), findsOneWidget);
  });
}
