import 'package:file4base_client/core/theme/theme_model.dart';
import 'package:file4base_client/core/theme/theme_provider.dart';
import 'package:file4base_client/features/theme_manager/manage_themes_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('Theme Models and Definitions Unit Tests', () {
    test('Default theme is Light (Clean Slate) with white canvas', () {
      final defaultTheme = AppThemes.light;
      expect(defaultTheme.id, AppThemeId.light);
      expect(defaultTheme.isDark, false);
      expect(defaultTheme.background, const Color(0xFFF8FAFC));
      expect(defaultTheme.surface, const Color(0xFFFFFFFF));
    });

    test('Includes Monokai Pro, Dark Modern, Dracula, and Nord', () {
      expect(AppThemes.allThemes.length, greaterThanOrEqualTo(5));

      final monokai = AppThemes.byId(AppThemeId.monokai);
      expect(monokai.name.contains('Monokai'), true);
      expect(monokai.isDark, true);
      expect(monokai.background, const Color(0xFF272822));

      final dark = AppThemes.byId(AppThemeId.dark);
      expect(dark.name.contains('Dark'), true);
      expect(dark.isDark, true);

      final dracula = AppThemes.byId(AppThemeId.dracula);
      expect(dracula.name.contains('Dracula'), true);
      expect(dracula.isDark, true);

      final nord = AppThemes.byId(AppThemeId.nord);
      expect(nord.name.contains('Nord'), true);
      expect(nord.isDark, true);
    });

    test('AppThemes.byName lookups work case-insensitively', () {
      final theme1 = AppThemes.byName('monokai');
      expect(theme1.id, AppThemeId.monokai);

      final theme2 = AppThemes.byName('light');
      expect(theme2.id, AppThemeId.light);

      final theme3 = AppThemes.byName('dracula');
      expect(theme3.id, AppThemeId.dracula);
    });

    test('ThemeData generated matches brightness and colors', () {
      final lightThemeData = AppThemes.light.toThemeData();
      expect(lightThemeData.brightness, Brightness.light);
      expect(lightThemeData.scaffoldBackgroundColor, const Color(0xFFF8FAFC));

      final darkThemeData = AppThemes.dark.toThemeData();
      expect(darkThemeData.brightness, Brightness.dark);
      expect(darkThemeData.scaffoldBackgroundColor, const Color(0xFF0B1120));
    });
  });

  group('AppThemeNotifier Riverpod Tests', () {
    test('Initial provider state is Light (Clean Slate)', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);

      final initial = container.read(appThemeProvider);
      expect(initial.id, AppThemeId.light);
      expect(initial.isDark, false);
    });

    test('Can switch to Monokai, Dracula, and toggle dark/light', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);

      final notifier = container.read(appThemeProvider.notifier);

      notifier.setTheme(AppThemeId.monokai);
      expect(container.read(appThemeProvider).id, AppThemeId.monokai);

      notifier.setTheme(AppThemeId.dracula);
      expect(container.read(appThemeProvider).id, AppThemeId.dracula);

      notifier.toggleDarkLight();
      expect(container.read(appThemeProvider).id, AppThemeId.light);
    });
  });

  group('ManageThemesDialog Widget Tests', () {
    testWidgets('renders modal dialog with theme cards and live preview', (tester) async {
      tester.view.physicalSize = const Size(1200, 800);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      await tester.pumpWidget(
        const ProviderScope(
          child: MaterialApp(
            home: Scaffold(
              body: ManageThemesDialog(),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Top title and search bar
      expect(find.text('Gestionar temas (Manage Themes)'), findsOneWidget);
      expect(find.text('Filtrar temas...'), findsOneWidget);

      // Verify theme cards appear
      expect(find.text('Light (Clean Slate)'), findsWidgets);
      expect(find.text('Dark Modern (Sophisticated)'), findsOneWidget);
      expect(find.text('Monokai Pro (Code Studio)'), findsOneWidget);
      expect(find.text('Dracula Midnight'), findsOneWidget);
      expect(find.text('Nordic Frost (Nord)'), findsOneWidget);

      // Verify Live Preview panel elements
      expect(find.text('Vista previa:'), findsOneWidget);
      expect(find.text('✔ ACTIVO'), findsOneWidget);
      expect(find.text('Aceptar y Aplicar'), findsOneWidget);
      expect(find.text('Cerrar'), findsWidgets);
    });

    testWidgets('filtering themes updates the list of available cards', (tester) async {
      tester.view.physicalSize = const Size(1200, 800);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      await tester.pumpWidget(
        const ProviderScope(
          child: MaterialApp(
            home: Scaffold(
              body: ManageThemesDialog(),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Enter search term 'monokai'
      await tester.enterText(find.byType(TextField), 'monokai');
      await tester.pumpAndSettle();

      expect(find.text('Monokai Pro (Code Studio)'), findsOneWidget);
      expect(find.text('Dracula Midnight'), findsNothing);
      expect(find.text('Nordic Frost (Nord)'), findsNothing);
    });

    testWidgets('clicking a theme card selects and applies it', (tester) async {
      tester.view.physicalSize = const Size(1200, 800);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(() => tester.view.resetPhysicalSize());

      late ProviderContainer capturedContainer;

      await tester.pumpWidget(
        ProviderScope(
          child: Consumer(
            builder: (context, ref, child) {
              capturedContainer = ProviderScope.containerOf(context);
              return const MaterialApp(
                home: Scaffold(
                  body: ManageThemesDialog(),
                ),
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Tap on Monokai card
      final monokaiFinder = find.text('Monokai Pro (Code Studio)');
      expect(monokaiFinder, findsOneWidget);
      await tester.tap(monokaiFinder);
      await tester.pumpAndSettle();

      // Tap "Aplicar este tema" in the preview panel
      final applyButton = find.text('Aplicar este tema');
      expect(applyButton, findsOneWidget);
      await tester.tap(applyButton);
      await tester.pumpAndSettle();

      // Verify provider state updated
      expect(capturedContainer.read(appThemeProvider).id, AppThemeId.monokai);
    });
  });
}
