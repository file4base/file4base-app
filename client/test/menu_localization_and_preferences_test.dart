import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart'
    show CustomMenuItemModel, CustomMenuModel, MenuSetModel;
import 'package:file4base_client/core/providers/locale_provider.dart';
import 'package:file4base_client/core/services/preferences_storage.dart';
import 'package:file4base_client/core/widgets/file4base_menu_bar.dart';
import 'package:file4base_client/features/preferences/preferences_dialog.dart';
import 'package:file4base_client/l10n/app_localizations.dart';
import 'package:file4base_client/l10n/l10n.dart';
import 'package:file4base_client/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

Widget _buildLocalizedShell({
  required PreferencesStore store,
  OperationalMode mode = OperationalMode.browse,
  MenuSetModel? activeMenuSet,
  VoidCallback? onOpenServerSettings,
}) {
  return ProviderScope(
    overrides: [
      preferencesStoreProvider.overrideWithValue(store),
    ],
    child: Consumer(
      builder: (context, ref, _) {
        final locale = ref.watch(appLocaleProvider);
        return MaterialApp(
          locale: locale,
          supportedLocales: AppLocalizations.supportedLocales,
          localizationsDelegates: AppLocalizations.localizationsDelegates,
          home: Builder(
            builder: (shellContext) => Scaffold(
              body: File4BaseMenuBar(
                activeMode: mode,
                activeMenuSet: activeMenuSet,
                onModeChanged: (_) {},
                onManageDatabase: () {},
                onOpenRemote: () {},
                onPreferences: () {
                  unawaited(
                    PreferencesDialog.show(
                      shellContext,
                      currentServerUrl: 'http://127.0.0.1:8080',
                      onOpenServerSettings: onOpenServerSettings,
                    ),
                  );
                },
                onAbout: () {},
                isToolbarVisible: true,
                onToggleToolbar: (_) {},
              ),
            ),
          ),
        );
      },
    ),
  );
}

void main() {
  group('AppLocaleNotifier persistence', () {
    test('loads persisted language code and falls back to English for unknown codes', () {
      final frStore = InMemoryPreferencesStore({kMenuLanguagePreferenceKey: 'fr'});
      final frContainer = ProviderContainer(
        overrides: [preferencesStoreProvider.overrideWithValue(frStore)],
      );
      addTearDown(frContainer.dispose);
      expect(frContainer.read(appLocaleProvider), const Locale('fr'));

      final invalidStore = InMemoryPreferencesStore({kMenuLanguagePreferenceKey: 'zz'});
      final invalidContainer = ProviderContainer(
        overrides: [preferencesStoreProvider.overrideWithValue(invalidStore)],
      );
      addTearDown(invalidContainer.dispose);
      expect(invalidContainer.read(appLocaleProvider), const Locale('en'));
    });

    test('persists language changes and supports non-persisted live previews', () {
      final store = InMemoryPreferencesStore();
      final container = ProviderContainer(
        overrides: [preferencesStoreProvider.overrideWithValue(store)],
      );
      addTearDown(container.dispose);

      expect(container.read(appLocaleProvider), const Locale('en'));

      container.read(appLocaleProvider.notifier).setLanguageCode('de', persist: false);
      expect(container.read(appLocaleProvider), const Locale('de'));
      expect(store.readString(kMenuLanguagePreferenceKey), isNull);

      container.read(appLocaleProvider.notifier).setLanguageCode('es');
      expect(container.read(appLocaleProvider), const Locale('es'));
      expect(store.readString(kMenuLanguagePreferenceKey), 'es');

      container.read(appLocaleProvider.notifier).resetToDefault();
      expect(container.read(appLocaleProvider), const Locale('en'));
      expect(store.readString(kMenuLanguagePreferenceKey), 'en');
    });
  });

  group('File4BaseMenuBar multi-language rendering', () {
    final expectedByLocale = <String, Map<String, String>>{
      'en': {
        'file': 'File',
        'edit': 'Edit',
        'view': 'View',
        'insert': 'Insert',
        'format': 'Format',
        'records': 'Records',
        'requests': 'Requests',
        'scripts': 'Scripts',
        'tools': 'Tools',
        'serverTools': 'Server Tools',
        'window': 'Window',
        'help': 'Help',
        'preferences': 'Preferences...',
      },
      'fr': {
        'file': 'Fichier',
        'edit': 'Édition',
        'view': 'Affichage',
        'insert': 'Insertion',
        'format': 'Format',
        'records': 'Enregistrements',
        'requests': 'Requêtes',
        'scripts': 'Scripts',
        'tools': 'Outils',
        'serverTools': 'Outils serveur',
        'window': 'Fenêtre',
        'help': 'Aide',
        'preferences': 'Préférences...',
      },
      'es': {
        'file': 'Archivo',
        'edit': 'Edición',
        'view': 'Ver',
        'insert': 'Insertar',
        'format': 'Formato',
        'records': 'Registros',
        'requests': 'Peticiones',
        'scripts': 'Guiones',
        'tools': 'Herramientas',
        'serverTools': 'Herramientas del servidor',
        'window': 'Ventana',
        'help': 'Ayuda',
        'preferences': 'Preferencias...',
      },
      'de': {
        'file': 'Datei',
        'edit': 'Bearbeiten',
        'view': 'Ansicht',
        'insert': 'Einfügen',
        'format': 'Format',
        'records': 'Datensätze',
        'requests': 'Abfragen',
        'scripts': 'Skripte',
        'tools': 'Werkzeuge',
        'serverTools': 'Server-Werkzeuge',
        'window': 'Fenster',
        'help': 'Hilfe',
        'preferences': 'Einstellungen...',
      },
      'it': {
        'file': 'File',
        'edit': 'Modifica',
        'view': 'Visualizza',
        'insert': 'Inserisci',
        'format': 'Formato',
        'records': 'Record',
        'requests': 'Richieste',
        'scripts': 'Script',
        'tools': 'Strumenti',
        'serverTools': 'Strumenti server',
        'window': 'Finestra',
        'help': 'Aiuto',
        'preferences': 'Preferenze...',
      },
      'nl': {
        'file': 'Bestand',
        'edit': 'Bewerken',
        'view': 'Weergave',
        'insert': 'Invoegen',
        'format': 'Opmaak',
        'records': 'Records',
        'requests': 'Zoekopdrachten',
        'scripts': 'Scripts',
        'tools': 'Hulpmiddelen',
        'serverTools': 'Serverhulpmiddelen',
        'window': 'Venster',
        'help': 'Help',
        'preferences': 'Voorkeuren...',
      },
      'pt': {
        'file': 'Arquivo',
        'edit': 'Editar',
        'view': 'Visualizar',
        'insert': 'Inserir',
        'format': 'Formatar',
        'records': 'Registros',
        'requests': 'Solicitações',
        'scripts': 'Scripts',
        'tools': 'Ferramentas',
        'serverTools': 'Ferramentas do servidor',
        'window': 'Janela',
        'help': 'Ajuda',
        'preferences': 'Preferências...',
      },
    };

    for (final lang in SupportedAppLanguage.all) {
      testWidgets('renders all root menus and Edit > Preferences in ${lang.nativeName} (${lang.code})',
          (tester) async {
        tester.view.physicalSize = const Size(1600, 900);
        tester.view.devicePixelRatio = 1.0;
        addTearDown(tester.view.resetPhysicalSize);

        final expected = expectedByLocale[lang.code]!;
        final store = InMemoryPreferencesStore({kMenuLanguagePreferenceKey: lang.code});

        await tester.pumpWidget(_buildLocalizedShell(store: store));
        await tester.pumpAndSettle();

        expect(find.text(expected['file']!), findsOneWidget);
        expect(find.text(expected['edit']!), findsOneWidget);
        expect(find.text(expected['view']!), findsOneWidget);
        expect(find.text(expected['insert']!), findsOneWidget);
        expect(find.text(expected['format']!), findsOneWidget);
        expect(find.text(expected['records']!), findsOneWidget);
        expect(find.text(expected['scripts']!), findsOneWidget);
        expect(find.text(expected['tools']!), findsOneWidget);
        expect(find.text(expected['serverTools']!), findsOneWidget);
        expect(find.text(expected['window']!), findsOneWidget);
        expect(find.text(expected['help']!), findsOneWidget);

        // Open Edit menu and verify localized Preferences item.
        await tester.tap(find.text(expected['edit']!));
        await tester.pumpAndSettle();
        expect(find.text(expected['preferences']!), findsOneWidget);

        // Switch to Find mode and verify localized Requests menu replaces Records.
        await tester.pumpWidget(
          _buildLocalizedShell(store: store, mode: OperationalMode.find),
        );
        await tester.pumpAndSettle();
        expect(find.text(expected['requests']!), findsOneWidget);
      });
    }

    testWidgets('custom menu sets still match canonical English menu names when localized',
        (tester) async {
      tester.view.physicalSize = const Size(1600, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      const menuSet = MenuSetModel(
        id: 'set-1',
        name: 'Kiosk Lite',
        isDefault: true,
        menus: [
          CustomMenuModel(menu: 'Insert', mode: 'hide'),
          CustomMenuModel(
            menu: 'Scripts',
            items: [
              CustomMenuItemModel(
                title: 'Run Report',
                action: {'type': 'run_script', 'script': 'GenerateReport'},
              ),
            ],
          ),
        ],
      );

      final store = InMemoryPreferencesStore({kMenuLanguagePreferenceKey: 'es'});
      await tester.pumpWidget(
        _buildLocalizedShell(store: store, activeMenuSet: menuSet),
      );
      await tester.pumpAndSettle();

      // Insert ("Insertar") should be hidden even though UI is in Spanish.
      expect(find.text('Insertar'), findsNothing);
      expect(find.text('Archivo'), findsOneWidget);

      // Scripts ("Guiones") should contain the extended custom item.
      await tester.tap(find.text('Guiones'));
      await tester.pumpAndSettle();
      expect(find.text('Run Report'), findsOneWidget);
    });
  });

  group('PreferencesDialog language management', () {
    testWidgets('live-previews language, rolls back on Cancel, and persists on Apply',
        (tester) async {
      tester.view.physicalSize = const Size(1600, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      var serverSettingsOpened = false;
      final store = InMemoryPreferencesStore();

      await tester.pumpWidget(
        _buildLocalizedShell(
          store: store,
          onOpenServerSettings: () => serverSettingsOpened = true,
        ),
      );
      await tester.pumpAndSettle();

      // Open Edit > Preferences...
      await tester.tap(find.text('Edit'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Preferences...'));
      await tester.pumpAndSettle();

      expect(find.text('Preferences'), findsOneWidget);
      expect(find.text('Menu Language'), findsOneWidget);

      // Select Spanish: live preview updates menus and dialog immediately.
      await tester.tap(find.byKey(const ValueKey('preferences_lang_es')));
      await tester.pumpAndSettle();
      expect(find.text('Preferencias'), findsOneWidget);
      expect(find.text('Archivo'), findsOneWidget);
      expect(store.readString(kMenuLanguagePreferenceKey), isNull);

      // Cancel rolls back to English without persisting.
      await tester.tap(find.text('Cancelar'));
      await tester.pumpAndSettle();
      expect(find.text('File'), findsOneWidget);
      expect(find.text('Archivo'), findsNothing);
      expect(store.readString(kMenuLanguagePreferenceKey), isNull);

      // Re-open Preferences, select French, and Apply.
      await tester.tap(find.text('Edit'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Preferences...'));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const ValueKey('preferences_lang_fr')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Appliquer'));
      await tester.pumpAndSettle();

      expect(find.text('Fichier'), findsOneWidget);
      expect(find.text('Édition'), findsOneWidget);
      expect(find.text('Aide'), findsOneWidget);
      expect(store.readString(kMenuLanguagePreferenceKey), 'fr');

      // Open localized Édition > Préférences... and test Server Settings button.
      await tester.tap(find.text('Édition'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Préférences...'));
      await tester.pumpAndSettle();

      expect(find.text('Préférences'), findsOneWidget);
      await tester.tap(find.text('Paramètres du serveur...'));
      await tester.pumpAndSettle();
      expect(serverSettingsOpened, isTrue);
    });
  });
}
