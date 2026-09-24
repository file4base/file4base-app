import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/models/file_options_model.dart';
import 'package:file4base_client/core/models/page_setup_model.dart';
import 'package:file4base_client/core/widgets/file4base_menu_bar.dart';
import 'package:file4base_client/features/solution_manager/file_options_dialog.dart';
import 'package:file4base_client/features/solution_manager/page_setup_dialog.dart';
import 'package:file4base_client/main.dart';

void main() {
  group('FileOptionsModel tests', () {
    test('Default values and copyWith', () {
      const model = FileOptionsModel();
      expect(model.autoLoginEnabled, isFalse);
      expect(model.isGuestLogin, isFalse);
      expect(model.defaultUsername, 'admin');
      expect(model.switchLayoutOnOpen, isFalse);
      expect(model.hideAllToolbars, isFalse);
      expect(model.onFirstWindowOpenEnabled, isFalse);
      expect(model.onLastWindowCloseEnabled, isFalse);

      final updated = model.copyWith(
        autoLoginEnabled: true,
        isGuestLogin: true,
        hideAllToolbars: true,
        switchLayoutOnOpen: true,
        startupLayoutId: 'lay-100',
        startupLayoutName: 'Customers Detail',
        onFirstWindowOpenEnabled: true,
        onFirstWindowOpenScript: 'StartupRoutine',
      );

      expect(updated.autoLoginEnabled, isTrue);
      expect(updated.isGuestLogin, isTrue);
      expect(updated.hideAllToolbars, isTrue);
      expect(updated.startupLayoutId, 'lay-100');
      expect(updated.startupLayoutName, 'Customers Detail');
      expect(updated.onFirstWindowOpenEnabled, isTrue);
      expect(updated.onFirstWindowOpenScript, 'StartupRoutine');
    });

    test('toJson and fromJson roundtrip', () {
      const original = FileOptionsModel(
        autoLoginEnabled: true,
        isGuestLogin: false,
        defaultUsername: 'poweruser',
        defaultPassword: 'secretpassword',
        switchLayoutOnOpen: true,
        startupLayoutId: 'lay-2',
        startupLayoutName: 'Invoices',
        hideAllToolbars: true,
        onFirstWindowOpenEnabled: true,
        onFirstWindowOpenScript: 'InitApp',
        onFirstWindowOpenParam: '{"env":"prod"}',
        onLastWindowCloseEnabled: true,
        onLastWindowCloseScript: 'CleanupApp',
      );

      final json = original.toJson();
      final parsed = FileOptionsModel.fromJson(json);

      expect(parsed.autoLoginEnabled, isTrue);
      expect(parsed.isGuestLogin, isFalse);
      expect(parsed.defaultUsername, 'poweruser');
      expect(parsed.defaultPassword, 'secretpassword');
      expect(parsed.switchLayoutOnOpen, isTrue);
      expect(parsed.startupLayoutId, 'lay-2');
      expect(parsed.startupLayoutName, 'Invoices');
      expect(parsed.hideAllToolbars, isTrue);
      expect(parsed.onFirstWindowOpenEnabled, isTrue);
      expect(parsed.onFirstWindowOpenScript, 'InitApp');
      expect(parsed.onFirstWindowOpenParam, '{"env":"prod"}');
      expect(parsed.onLastWindowCloseEnabled, isTrue);
      expect(parsed.onLastWindowCloseScript, 'CleanupApp');
    });
  });

  group('PageSetupModel calculations', () {
    test('A4 portrait calculations', () {
      const setup = PageSetupModel(
        printer: 'Any Printer',
        paperSizeName: 'A4',
        paperWidthMm: 210.0,
        paperHeightMm: 297.0,
        isLandscape: false,
        marginTopMm: 20.0,
        marginBottomMm: 20.0,
        marginLeftMm: 15.0,
        marginRightMm: 15.0,
      );

      expect(setup.effectiveWidthMm, 210.0);
      expect(setup.effectiveHeightMm, 297.0);
      expect(setup.printableWidthMm, 180.0); // 210 - 15 - 15
      expect(setup.printableHeightMm, 257.0); // 297 - 20 - 20

      // Conversion points checks
      expect(setup.totalWidthPt, closeTo(595.28, 0.5));
      expect(setup.totalHeightPt, closeTo(841.89, 0.5));
    });

    test('Landscape orientation swaps width and height', () {
      const setup = PageSetupModel(
        printer: 'Office LaserJet',
        paperSizeName: 'A4',
        paperWidthMm: 210.0,
        paperHeightMm: 297.0,
        isLandscape: true,
        marginTopMm: 10.0,
        marginBottomMm: 10.0,
        marginLeftMm: 10.0,
        marginRightMm: 10.0,
      );

      expect(setup.effectiveWidthMm, 297.0);
      expect(setup.effectiveHeightMm, 210.0);
      expect(setup.printableWidthMm, 277.0);
      expect(setup.printableHeightMm, 190.0);
    });

    test('toJson and fromJson roundtrip', () {
      const original = PageSetupModel(
        printer: 'PDF Document Writer',
        paperSizeName: 'US Letter',
        paperWidthMm: 215.9,
        paperHeightMm: 279.4,
        isLandscape: true,
        marginTopMm: 12.5,
        marginBottomMm: 12.5,
        marginLeftMm: 10.0,
        marginRightMm: 10.0,
      );

      final json = original.toJson();
      final parsed = PageSetupModel.fromJson(json);

      expect(parsed.printer, 'PDF Document Writer');
      expect(parsed.paperSizeName, 'US Letter');
      expect(parsed.paperWidthMm, 215.9);
      expect(parsed.paperHeightMm, 279.4);
      expect(parsed.isLandscape, isTrue);
      expect(parsed.marginTopMm, 12.5);
      expect(parsed.marginLeftMm, 10.0);
    });
  });

  group('MenuBar Send Mail and Save a Copy As cleanup', () {
    testWidgets('Send Mail is removed and Save a Copy As is not duplicated', (tester) async {
      tester.view.physicalSize = const Size(1440, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      bool fileOptionsClicked = false;
      bool pageSetupClicked = false;

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: File4BaseMenuBar(
              activeMode: OperationalMode.browse,
              onModeChanged: (_) {},
              onManageDatabase: () {},
              onOpenRemote: () {},
              onAbout: () {},
              onFileOptions: () => fileOptionsClicked = true,
              onPageSetup: () => pageSetupClicked = true,
              isToolbarVisible: true,
              onToggleToolbar: (_) {},
            ),
          ),
        ),
      );
      await tester.pump();

      // Open the File menu
      await tester.tap(find.text('File'));
      await tester.pumpAndSettle();

      // Verify "Send Mail..." is completely removed
      expect(find.text('Send Mail...'), findsNothing);

      // Verify "Save a Copy As..." is present exactly once
      expect(find.text('Save a Copy As...'), findsOneWidget);

      // Verify "File Options..." is present and invokes callback
      expect(find.text('File Options...'), findsOneWidget);
      await tester.tap(find.text('File Options...'));
      await tester.pumpAndSettle();
      expect(fileOptionsClicked, isTrue);

      // Re-open File menu to check Page Setup
      await tester.tap(find.text('File'));
      await tester.pumpAndSettle();
      expect(find.text('Page Setup...'), findsOneWidget);
      await tester.tap(find.text('Page Setup...'));
      await tester.pumpAndSettle();
      expect(pageSetupClicked, isTrue);
    });
  });

  group('Dialog widget tests', () {
    testWidgets('FileOptionsDialog renders tabs and controls', (tester) async {
      tester.view.physicalSize = const Size(1200, 800);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: FileOptionsDialog(
              initialOptions: FileOptionsModel(),
              layouts: [],
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Verify tabs
      expect(find.text('«Abrir» (Open)'), findsOneWidget);
      expect(find.text('«Activadores de guión» (Script Triggers)'), findsOneWidget);

      // Check Open tab contents
      expect(find.text('Iniciar sesión como:'), findsOneWidget);
      expect(find.text('Cambiar contraseña...'), findsOneWidget);
      expect(find.text('Ocultar todas las barras de herramientas'), findsOneWidget);

      // Switch to Script Triggers tab
      await tester.tap(find.text('«Activadores de guión» (Script Triggers)'));
      await tester.pumpAndSettle();

      expect(find.text('OnFirstWindowOpen'), findsOneWidget);
      expect(find.text('OnLastWindowClose'), findsOneWidget);
      expect(find.text('OnWindowOpen'), findsOneWidget);
      expect(find.text('OnWindowClose'), findsOneWidget);
      expect(find.text('OnFileAVPlayerChange'), findsOneWidget);
    });

    testWidgets('PageSetupDialog renders printers, orientation, and margin calculation', (tester) async {
      tester.view.physicalSize = const Size(1200, 800);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: PageSetupDialog(
              initialPageSetup: PageSetupModel(),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('Ajustar página / Configurar impresión (Page Setup)'), findsOneWidget);
      expect(find.text('Formatear para / Impresora:'), findsOneWidget);
      expect(find.text('Tamaño del papel:'), findsOneWidget);
      expect(find.text('Orientación:'), findsOneWidget);
      expect(find.text('Márgenes (mm):'), findsOneWidget);
      expect(find.text('Cálculo de Límites de Página'), findsOneWidget);
      expect(find.text('Área útil'), findsOneWidget);
    });
  });
}
