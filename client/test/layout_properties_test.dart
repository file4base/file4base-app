import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/widgets/file4base_status_sidebar.dart';
import 'package:file4base_client/features/layout_engine/layout_designer_widget.dart';
import 'package:file4base_client/features/layout_engine/layout_object_visuals.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';

final _table = TableModel(id: 't1', name: 'contacts', displayName: 'Contacts', columns: const [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
]);

const _parts = [
  LayoutPartModel(id: 'h', type: 'header', height: 60),
  LayoutPartModel(id: 'b', type: 'body', height: 200),
  LayoutPartModel(id: 'f', type: 'footer', height: 40),
];

Future<LayoutDefinitionModel? Function()> _pump(WidgetTester tester) async {
  tester.view.physicalSize = const Size(1600, 1000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  LayoutDefinitionModel? last;
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: LayoutDesignerWidget(
        table: _table,
        tables: [_table],
        apiClient: ApiClient(baseUrl: 'http://localhost:1'),
        initialLayout: const LayoutDefinitionModel(
            id: 'l1', name: 'Contacts', tableOccurrence: 'contacts', width: 700, parts: _parts),
        onSaved: () {},
        onLayoutChanged: (l) => last = l,
        activeTool: LayoutTool.pointer,
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return () => last;
}

Future<void> _submitField(WidgetTester tester, String label, String value) async {
  final field = find.widgetWithText(TextField, label);
  await tester.ensureVisible(field);
  await tester.enterText(field, value);
  await tester.testTextInput.receiveAction(TextInputAction.done);
  await tester.pump();
}

void main() {
  test('layout properties survive a JSON round trip and stay out of old layouts', () {
    const layout = LayoutDefinitionModel(
      id: 'l1',
      name: 'Contacts',
      tableOccurrence: 'contacts',
      parts: _parts,
      backgroundColor: '#F5F5F7',
      backgroundImage: LayoutMediaModel(kind: 'image', name: 'bg.png', mimeType: 'image/png', data: 'AA==', fit: 'tile'),
      onLayoutEnter: ButtonActionModel.performScript(id: 's1', name: 'Welcome'),
      onLayoutExit: ButtonActionModel.performScript(id: 's2', name: 'Goodbye'),
      transition: 'slide_left',
    );
    final back = LayoutDefinitionModel.fromJson(layout.toJson());
    expect(back.height, 300);
    expect(back.backgroundColor, '#F5F5F7');
    expect(back.backgroundImage!.fit, 'tile');
    expect(back.onLayoutEnter!.scriptId, 's1');
    expect(back.onLayoutExit!.scriptName, 'Goodbye');
    expect(back.transition, 'slide_left');

    final plain = const LayoutDefinitionModel(id: 'l2', name: 'P', tableOccurrence: 'contacts').toJson();
    expect(plain.keys, isNot(contains('background_color')));
    expect(plain.keys, isNot(contains('transition')));
    expect(LayoutDefinitionModel.fromJson(plain).transition, 'none');
  });

  testWidgets('the canvas ends at the footer and the layout panel resizes it', (tester) async {
    final last = await _pump(tester);
    expect(tester.getSize(find.byType(LayoutBackgroundView)).height, closeTo(300, 2),
        reason: 'no blank area below the footer');

    expect(find.text('LAYOUT SIZE'), findsOneWidget, reason: 'layout properties show when nothing is selected');
    await _submitField(tester, 'Height', '220');
    expect(last()!.height, 220);
    expect(last()!.parts.firstWhere((p) => p.type == 'body').height, 120);
    await tester.pump();
    expect(tester.getSize(find.byType(LayoutBackgroundView)).height, closeTo(220, 2));

    await _submitField(tester, 'Width', '640');
    expect(last()!.width, 640);
    await tester.pump(const Duration(seconds: 2));
  });

  testWidgets('choosing a background color paints the canvas', (tester) async {
    final last = await _pump(tester);
    final swatch = find.byTooltip('Navy');
    await tester.ensureVisible(swatch);
    await tester.tap(swatch);
    await tester.pump();
    expect(last()!.backgroundColor, '#0D47A1');
    final box = tester.widget<DecoratedBox>(
        find.descendant(of: find.byType(LayoutBackgroundView), matching: find.byType(DecoratedBox)));
    expect((box.decoration as BoxDecoration).color, const Color(0xFF0D47A1));
    await tester.pump(const Duration(seconds: 2));
  });
}
