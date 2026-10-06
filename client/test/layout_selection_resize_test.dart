import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/widgets/file4base_status_sidebar.dart';
import 'package:file4base_client/features/layout_engine/layout_designer_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';

final _table = TableModel(id: 't1', name: 'contacts', displayName: 'Contacts', columns: const [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
]);

const _a = LayoutObjectModel(id: 'a', type: 'label', x: 100, y: 120, width: 120, height: 30, text: 'Alpha');
const _b = LayoutObjectModel(id: 'b', type: 'label', x: 300, y: 120, width: 120, height: 30, text: 'Beta');

Future<LayoutDefinitionModel? Function()> _pump(WidgetTester tester, List<LayoutObjectModel> objects) async {
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
        initialLayout: LayoutDefinitionModel(id: 'l1', name: 'Contacts', tableOccurrence: 'contacts', objects: objects),
        onSaved: () {},
        onLayoutChanged: (l) => last = l,
        activeTool: LayoutTool.pointer,
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return () => last;
}

/// A real mouse click: the button stays down longer than the tap deadline.
Future<void> _click(WidgetTester tester, Offset at) async {
  final g = await tester.startGesture(at, kind: PointerDeviceKind.mouse);
  await tester.pump(const Duration(milliseconds: 200));
  await g.up();
  await tester.pump(const Duration(milliseconds: 400));
}

/// A real mouse drag: press, hold, then move in small steps.
Future<void> _mouseDrag(WidgetTester tester, Offset from, Offset by) async {
  final g = await tester.startGesture(from, kind: PointerDeviceKind.mouse);
  await tester.pump(const Duration(milliseconds: 200));
  const steps = 8;
  for (var i = 0; i < steps; i++) {
    await g.moveBy(by / steps.toDouble());
    await tester.pump(const Duration(milliseconds: 16));
  }
  await g.up();
  await tester.pump();
}

Rect _objectBox(WidgetTester tester, String text) =>
    tester.getRect(find.ancestor(of: find.text(text), matching: find.byType(GestureDetector)).first);

void main() {
  testWidgets('Shift-click selects several objects and dragging moves them together', (tester) async {
    final last = await _pump(tester, [_a, _b]);
    await _click(tester, tester.getCenter(find.text('Alpha')));
    await tester.sendKeyDownEvent(LogicalKeyboardKey.shiftLeft);
    await _click(tester, tester.getCenter(find.text('Beta')));
    await tester.sendKeyUpEvent(LogicalKeyboardKey.shiftLeft);

    await _mouseDrag(tester, tester.getCenter(find.text('Alpha')), const Offset(40, 48));
    final objs = {for (final o in last()!.objects) o.id: o};
    expect(objs['a']!.y, greaterThan(150));
    expect(objs['b']!.y, objs['a']!.y, reason: 'the second selected object moves with the first');
    await tester.pump(const Duration(seconds: 2));
  });

  testWidgets('dragging a marquee on the empty canvas selects the objects it touches', (tester) async {
    final last = await _pump(tester, [_a, _b]);
    final a = _objectBox(tester, 'Alpha');
    final b = _objectBox(tester, 'Beta');
    await _mouseDrag(tester, a.topLeft - const Offset(20, 20), b.bottomRight - a.topLeft + const Offset(40, 40));
    await _mouseDrag(tester, tester.getCenter(find.text('Beta')), const Offset(0, 40));
    final objs = {for (final o in last()!.objects) o.id: o};
    expect(objs['a']!.y, greaterThan(150));
    expect(objs['b']!.y, greaterThan(150));
    await tester.pump(const Duration(seconds: 2));
  });

  testWidgets('dragging a resize handle resizes the selected object', (tester) async {
    final last = await _pump(tester, [_a]);
    await _click(tester, tester.getCenter(find.text('Alpha')));
    final box = _objectBox(tester, 'Alpha');

    await _mouseDrag(tester, box.bottomRight, const Offset(40, 32));
    var a = last()!.objects.single;
    expect(a.width, greaterThan(140));
    expect(a.height, greaterThan(50));

    await _mouseDrag(tester, Offset(box.left, box.center.dy), const Offset(-40, 0));
    a = last()!.objects.single;
    expect(a.x, lessThan(80), reason: 'the left handle moves the left edge');
    await tester.pump(const Duration(seconds: 2));
  });
}
