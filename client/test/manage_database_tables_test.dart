import 'package:file4base_client/features/schema_manager/manage_database_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('ManageDatabaseDialog Tables tab renders table operations: rename, duplicate, empty, delete, and fields',
      (tester) async {
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    await tester.pumpWidget(
      const ProviderScope(
        child: MaterialApp(
          home: Scaffold(
            body: ManageDatabaseDialog(),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    // Verify Tab headers
    expect(find.text('Databases'), findsOneWidget);
    expect(find.text('Tables'), findsOneWidget);
    expect(find.text('Fields'), findsOneWidget);
    expect(find.text('Relationships Graph'), findsOneWidget);

    // Switch to Tables tab
    await tester.tap(find.text('Tables'));
    await tester.pumpAndSettle();

    // Verify Select All Checkbox
    expect(find.text('Select All'), findsOneWidget);
    expect(find.byType(Checkbox), findsWidgets);

    // Verify Table operations toolbar buttons
    expect(find.text('Create Table...'), findsWidgets);
    expect(find.text('Refresh'), findsOneWidget);
    expect(find.text('Rename...'), findsOneWidget);
    expect(find.text('Duplicate'), findsOneWidget);
    expect(find.text('Empty...'), findsOneWidget);
    expect(find.text('Delete...'), findsOneWidget);
    expect(find.text('Manage Fields ->'), findsOneWidget);
  });
}
