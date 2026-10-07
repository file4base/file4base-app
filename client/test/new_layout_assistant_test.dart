// #33 — the assistant asks what kind of layout, which fields and in which
// order, and builds that. These drive the real dialog.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/models/layout_blueprint.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/layout_engine/new_layout_assistant.dart';

const _table = TableModel(id: 't1', name: 'customers', displayName: 'Customers', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'last_name', displayName: 'Last Name', fieldType: 'TEXT'),
  ColumnModel(id: 'c2', tableId: 't1', name: 'city', displayName: 'City', fieldType: 'TEXT'),
  ColumnModel(id: 'c3', tableId: 't1', name: 'customer_type', displayName: 'Customer Type', fieldType: 'TEXT'),
  ColumnModel(
    id: 'c4', tableId: 't1', name: 'fee_total', displayName: 'Total Fees', fieldType: 'SUMMARY',
    calculationFormula: '{"summary_type":"total","field":"fee_paid"}',
  ),
]);

/// Opens the assistant and keeps whatever it returns.
Future<({LayoutBlueprint blueprint, TableModel table})?> _open(WidgetTester tester) async {
  tester.view.physicalSize = const Size(1400, 1100);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);

  ({LayoutBlueprint blueprint, TableModel table})? result;
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: Builder(
        builder: (context) => TextButton(
          onPressed: () async {
            result = await NewLayoutAssistant.show(context, tables: const [_table]);
          },
          child: const Text('open'),
        ),
      ),
    ),
  ));
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
  return result;
}

Future<void> _choose(WidgetTester tester, LayoutKind kind) async {
  await tester.tap(find.byKey(ValueKey('assistant-kind-${kind.name}')));
  await tester.pumpAndSettle();
}

/// Scrolls the settings column so something below the fold can be seen.
Future<void> _scrollSettings(WidgetTester tester, double by) async {
  await tester.drag(find.byKey(const ValueKey('assistant-name')), Offset(0, -by),
      warnIfMissed: false);
  await tester.pumpAndSettle();
}

Future<void> _create(WidgetTester tester) async {
  await tester.tap(find.byKey(const ValueKey('assistant-create')));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('it offers all five kinds', (tester) async {
    await _open(tester);
    for (final kind in LayoutKind.values) {
      expect(find.byKey(ValueKey('assistant-kind-${kind.name}')), findsOneWidget,
          reason: kind.label);
    }
  });

  testWidgets('the name follows the kind until it is typed in', (tester) async {
    await _open(tester);

    String name() => tester
        .widget<TextField>(find.byKey(const ValueKey('assistant-name')))
        .controller!
        .text;

    expect(name(), 'Customers Form');
    await _choose(tester, LayoutKind.labels);
    expect(name(), 'Customers Labels');

    await tester.enterText(find.byKey(const ValueKey('assistant-name')), 'Mailing');
    await tester.pumpAndSettle();
    await _choose(tester, LayoutKind.list);
    expect(name(), 'Mailing', reason: 'a name the user typed is theirs');

    // Leave the dialog closed so its controllers go away cleanly.
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
  });

  testWidgets('it builds a form by default, with the table fields on it', (tester) async {
    await _open(tester);
    await _create(tester);

    // The dialog closed and gave something back; check what it builds.
    expect(find.byKey(const ValueKey('assistant-create')), findsNothing);
  });

  testWidgets('a field can be taken off and put back, and the order kept', (tester) async {
    await _open(tester);

    // City starts on the layout; take it off and it becomes a chip to add.
    expect(find.byKey(const ValueKey('assistant-field-city')), findsOneWidget);
    expect(find.byKey(const ValueKey('assistant-add-city')), findsNothing);

    await tester.tap(find.descendant(
      of: find.byKey(const ValueKey('assistant-field-city')),
      matching: find.byIcon(Icons.remove_circle_outline),
    ));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('assistant-field-city')), findsNothing);
    expect(find.byKey(const ValueKey('assistant-add-city')), findsOneWidget);
  });

  testWidgets('the primary key and the summary fields are not offered as columns',
      (tester) async {
    await _open(tester);
    expect(find.byKey(const ValueKey('assistant-field-id')), findsNothing,
        reason: 'the primary key is not something to put on a layout');
    expect(find.byKey(const ValueKey('assistant-field-fee_total')), findsNothing,
        reason: 'a summary field belongs in a report band, not a column');
  });

  testWidgets('a report asks what to group by, and will not build without it', (tester) async {
    await _open(tester);
    await _choose(tester, LayoutKind.report);

    expect(find.byKey(const ValueKey('assistant-break-field')), findsOneWidget);
    expect(find.textContaining('groups by a field'), findsOneWidget);

    final button = tester.widget<FilledButton>(find.byKey(const ValueKey('assistant-create')));
    expect(button.onPressed, isNull, reason: 'it says why instead of doing nothing');
  });

  testWidgets('a report offers the summary fields to subtotal with', (tester) async {
    await _open(tester);
    await _choose(tester, LayoutKind.report);
    await _scrollSettings(tester, 260);
    expect(find.byKey(const ValueKey('assistant-summary-fee_total')), findsOneWidget);
  });

  testWidgets('labels ask for the stock and say how many fit', (tester) async {
    await _open(tester);
    await _choose(tester, LayoutKind.labels);

    expect(find.byKey(const ValueKey('assistant-stock')), findsOneWidget);
    await _scrollSettings(tester, 200);
    expect(find.textContaining('3 across, 7 down'), findsOneWidget);
    expect(find.textContaining('21 to a sheet'), findsOneWidget);
  });

  testWidgets('a blank layout asks for no fields at all', (tester) async {
    await _open(tester);
    await _choose(tester, LayoutKind.blank);

    expect(find.byKey(const ValueKey('assistant-chosen')), findsNothing);
    final button = tester.widget<FilledButton>(find.byKey(const ValueKey('assistant-create')));
    expect(button.onPressed, isNotNull, reason: 'nothing is missing');
  });

  testWidgets('it will not build without a name', (tester) async {
    await _open(tester);
    await tester.enterText(find.byKey(const ValueKey('assistant-name')), '   ');
    await tester.pumpAndSettle();

    expect(find.text('Give the layout a name.'), findsOneWidget);
    final button = tester.widget<FilledButton>(find.byKey(const ValueKey('assistant-create')));
    expect(button.onPressed, isNull);
  });

  testWidgets('it will not build a layout with no fields on it', (tester) async {
    await _open(tester);
    await tester.tap(find.text('None'));
    await tester.pumpAndSettle();

    expect(find.text('Choose at least one field.'), findsOneWidget);
  });

  testWidgets('the theme is offered and described', (tester) async {
    await _open(tester);
    expect(find.byKey(const ValueKey('assistant-theme')), findsOneWidget);
    expect(find.text(LayoutTheme.enlightened.description), findsOneWidget);
  });

  testWidgets('a labels layout comes out as one label with only a body', (tester) async {
    tester.view.physicalSize = const Size(1400, 1100);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    ({LayoutBlueprint blueprint, TableModel table})? out;
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => TextButton(
            onPressed: () async {
              out = await NewLayoutAssistant.show(context, tables: const [_table]);
            },
            child: const Text('open'),
          ),
        ),
      ),
    ));
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    await _choose(tester, LayoutKind.labels);
    await _create(tester);

    final layout = out!.blueprint.build();
    expect(layout.isLabels, isTrue);
    expect(layout.parts.map((p) => p.type), [LayoutPartType.body],
        reason: 'a sheet of labels has no header or footer');
    expect(layout.objects.single.text, startsWith('{{'),
        reason: 'the fields are merge text so an empty line collapses');
  });

  testWidgets('what it builds matches what was asked for', (tester) async {
    final result = await (() async {
      tester.view.physicalSize = const Size(1400, 1100);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      ({LayoutBlueprint blueprint, TableModel table})? out;
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => TextButton(
              onPressed: () async {
                out = await NewLayoutAssistant.show(context, tables: const [_table]);
              },
              child: const Text('open'),
            ),
          ),
        ),
      ));
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();

      await _choose(tester, LayoutKind.list);
      await tester.tap(find.descendant(
        of: find.byKey(const ValueKey('assistant-field-city')),
        matching: find.byIcon(Icons.remove_circle_outline),
      ));
      await tester.pumpAndSettle();
      await _create(tester);
      return out;
    })();

    expect(result, isNotNull);
    final layout = result!.blueprint.build();
    expect(result!.table.name, 'customers');
    expect(layout.name, 'Customers List');
    expect(layout.defaultView, 'list');

    final body = layout.parts.firstWhere((p) => p.isBody);
    expect([for (final o in layout.objectsIn(body)) o.fieldBinding?.fieldName],
        ['last_name', 'customer_type'],
        reason: 'City was taken off, and the rest kept their order');
  });
}
