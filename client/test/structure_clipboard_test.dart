import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/tools/structure_clipboard_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets(
      'StructurePastePreviewDialog renders collisions, missing dependencies, and returns selected strategy',
      (tester) async {
    await tester.binding.setSurfaceSize(const Size(1280, 900));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    const preview = ClipboardPreviewModel(
      kind: 'layout',
      objectName: 'Invoice Detail',
      hasCollision: true,
      collisions: ['Invoice Detail'],
      missingDependencies: [
        ClipboardDependencyModel(
          kind: 'field',
          name: 'LineItems::Total',
          detail: 'Element field_1',
        ),
        ClipboardDependencyModel(
          kind: 'script',
          name: 'Print Invoice',
          detail: 'Button btn_print',
        ),
      ],
    );

    String? result;

    await tester.pumpWidget(
      ProviderScope(
        child: MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (context) => ElevatedButton(
                onPressed: () async {
                  result = await StructurePastePreviewDialog.show(
                    context,
                    preview: preview,
                    envelopeJson: '{"kind":"layout","name":"Invoice Detail"}',
                  );
                },
                child: const Text('Open Preview'),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('Open Preview'));
    await tester.pumpAndSettle();

    expect(find.text('Paste layout: Invoice Detail'), findsOneWidget);
    expect(find.text('Name Collision Detected (Invoice Detail)'), findsOneWidget);
    expect(find.text('Missing Dependencies in Destination (2)'), findsOneWidget);
    expect(find.textContaining('[field] LineItems::Total'), findsOneWidget);
    expect(find.textContaining('[script] Print Invoice'), findsOneWidget);

    // Select Replace existing item
    await tester.tap(find.byKey(const Key('clipboard_collision_replace_radio')));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('clipboard_confirm_paste_button')));
    await tester.pumpAndSettle();

    expect(result, equals('replace'));
  });
}
