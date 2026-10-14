import 'package:file4base_client/core/models/page_setup_model.dart';
import 'package:file4base_client/core/widgets/file4base_menu_bar.dart';
import 'package:file4base_client/features/layout_engine/layout_object_visuals.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/layout_engine/view_format_tools.dart';
import 'package:file4base_client/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('LayoutObjectStyle & ConditionalFormatRule', () {
    test('serializes and deserializes typography, data format, ruler, and conditional rules', () {
      const rule = ConditionalFormatRule(
        operator: '<',
        value: '0',
        textColor: '#EF4444',
        fillColor: '#FFF5F5',
        bold: true,
        italic: true,
        underline: true,
      );
      const style = LayoutObjectStyle(
        fontSize: 16,
        fontWeight: 'bold',
        fontFamily: 'Georgia',
        fontStyle: 'italic',
        underline: true,
        strikethrough: true,
        textCase: 'upper',
        lineSpacing: 1.5,
        indentLeft: 18,
        indentRight: 12,
        firstLineIndent: 6,
        tabStopsPt: [36, 72, 144],
        numberFormat: 'currency',
        decimalPlaces: 2,
        currencySymbol: '€',
        useThousandsSeparator: true,
        dateFormat: 'dd/mm/yyyy',
        timeFormat: '24h',
        conditionalRules: [rule],
      );

      final json = style.toJson();
      final roundTrip = LayoutObjectStyle.fromJson(json);

      expect(roundTrip.fontFamily, 'Georgia');
      expect(roundTrip.fontStyle, 'italic');
      expect(roundTrip.underline, isTrue);
      expect(roundTrip.strikethrough, isTrue);
      expect(roundTrip.textCase, 'upper');
      expect(roundTrip.lineSpacing, 1.5);
      expect(roundTrip.indentLeft, 18);
      expect(roundTrip.indentRight, 12);
      expect(roundTrip.firstLineIndent, 6);
      expect(roundTrip.tabStopsPt, [36, 72, 144]);
      expect(roundTrip.numberFormat, 'currency');
      expect(roundTrip.decimalPlaces, 2);
      expect(roundTrip.currencySymbol, '€');
      expect(roundTrip.useThousandsSeparator, isTrue);
      expect(roundTrip.dateFormat, 'dd/mm/yyyy');
      expect(roundTrip.timeFormat, '24h');
      expect(roundTrip.conditionalRules.length, 1);
      expect(roundTrip.conditionalRules.first.operator, '<');
      expect(roundTrip.conditionalRules.first.textColor, '#EF4444');
    });

    test('evaluates conditional formatting rules on field values', () {
      const negativeRule = ConditionalFormatRule(
        operator: '<',
        value: '0',
        textColor: '#EF4444',
        bold: true,
      );
      const baseStyle = LayoutObjectStyle(
        textColor: '#21262D',
        fontWeight: 'normal',
        conditionalRules: [negativeRule],
      );

      final negativeEffective = baseStyle.effectiveForValue(-42.5);
      expect(negativeEffective.textColor, '#EF4444');
      expect(negativeEffective.fontWeight, 'bold');

      final positiveEffective = baseStyle.effectiveForValue(100);
      expect(positiveEffective.textColor, '#21262D');
      expect(positiveEffective.fontWeight, 'normal');
    });
  });

  group('formatLayoutDisplayValue & applyLayoutTextCase', () {
    test('formats currency, decimal, percent, and boolean values without mutating raw value', () {
      const currencyStyle = LayoutObjectStyle(
        numberFormat: 'currency',
        decimalPlaces: 2,
        currencySymbol: '€',
        useThousandsSeparator: true,
      );
      expect(formatLayoutDisplayValue('12345.6', currencyStyle), contains('12,345.60'));
      expect(formatLayoutDisplayValue('12345.6', currencyStyle), contains('€'));

      const percentStyle = LayoutObjectStyle(
        numberFormat: 'percent',
        decimalPlaces: 1,
      );
      expect(formatLayoutDisplayValue('0.256', percentStyle), '25.6%');

      const boolStyle = LayoutObjectStyle(numberFormat: 'boolean');
      expect(formatLayoutDisplayValue('1', boolStyle), 'Yes');
      expect(formatLayoutDisplayValue('0', boolStyle), 'No');
    });

    test('formats dates, times, and text case transformations', () {
      const dateStyle = LayoutObjectStyle(dateFormat: 'dd/mm/yyyy');
      expect(formatLayoutDisplayValue('2026-10-14', dateStyle), '14/10/2026');

      const time12hStyle = LayoutObjectStyle(timeFormat: '12h');
      expect(formatLayoutDisplayValue('14:35:00', time12hStyle), '2:35 PM');

      expect(applyLayoutTextCase('hello file4base world', 'title'), 'Hello File4base World');
      expect(applyLayoutTextCase('hello', 'upper'), 'HELLO');
      expect(applyLayoutTextCase('HELLO', 'lower'), 'hello');
    });
  });

  group('View & Format Widgets', () {
    testWidgets('File4BaseFormattingBar renders controls and emits style updates', (tester) async {
      LayoutObjectStyle? updated;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: File4BaseFormattingBar(
              style: const LayoutObjectStyle(),
              targetLabel: 'Field: total_amount',
              onStyleChanged: (s) => updated = s,
            ),
          ),
        ),
      );

      expect(find.text('Field: total_amount'), findsOneWidget);
      await tester.tap(find.byIcon(Icons.format_bold));
      await tester.pump();
      expect(updated?.fontWeight, 'bold');
    });

    testWidgets('File4BaseTextRuler and PageMarginsOverlay render without errors', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Column(
              children: [
                File4BaseTextRuler(
                  style: const LayoutObjectStyle(tabStopsPt: [72, 144]),
                  width: 600,
                  onStyleChanged: (_) {},
                ),
                const Expanded(
                  child: Stack(
                    children: [
                      PageMarginsOverlay(
                        pageSetup: PageSetupModel(),
                        canvasWidth: 800,
                        canvasHeight: 600,
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      );

      expect(find.byType(File4BaseTextRuler), findsOneWidget);
      expect(find.byType(PageMarginsOverlay), findsOneWidget);
    });

    testWidgets('View menu toggles Page Margins, Formatting Bar, and Text Ruler', (tester) async {
      bool? marginsToggled;
      bool? formatBarToggled;
      bool? rulerToggled;

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: File4BaseMenuBar(
              activeMode: OperationalMode.layout,
              onModeChanged: (_) {},
              onManageDatabase: () {},
              onOpenRemote: () {},
              onAbout: () {},
              isToolbarVisible: true,
              onToggleToolbar: (_) {},
              isPageMarginsVisible: false,
              onTogglePageMargins: (v) => marginsToggled = v,
              isFormattingBarVisible: false,
              onToggleFormattingBar: (v) => formatBarToggled = v,
              isTextRulerVisible: false,
              onToggleTextRuler: (v) => rulerToggled = v,
            ),
          ),
        ),
      );

      await tester.tap(find.text('View'));
      await tester.pumpAndSettle();

      expect(find.text('Page Margins'), findsOneWidget);
      expect(find.text('Formatting Bar'), findsOneWidget);
      expect(find.text('Text Ruler'), findsOneWidget);

      await tester.tap(find.text('Page Margins'));
      await tester.pumpAndSettle();
      expect(marginsToggled, isTrue);

      await tester.tap(find.text('View'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Formatting Bar'));
      await tester.pumpAndSettle();
      expect(formatBarToggled, isTrue);

      await tester.tap(find.text('View'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Text Ruler'));
      await tester.pumpAndSettle();
      expect(rulerToggled, isTrue);
    });
  });
}
