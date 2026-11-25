import 'dart:async';
import 'dart:ui';

import 'package:file4base_client/core/services/preferences_storage.dart';
import 'package:file4base_client/l10n/l10n.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Preference key used to persist the user's selected menu language.
const String kMenuLanguagePreferenceKey = 'menu_language';

/// Provides the platform-backed preferences store (overridable in tests).
final preferencesStoreProvider = Provider<PreferencesStore>(
  (ref) => PlatformPreferencesStore(),
);

/// Manages the active application [Locale] for menus and preferences.
class AppLocaleNotifier extends Notifier<Locale> {
  @override
  Locale build() {
    final store = ref.watch(preferencesStoreProvider);
    final savedCode = store.readString(kMenuLanguagePreferenceKey);
    return SupportedAppLanguage.fromCode(savedCode).locale;
  }

  /// Updates the active locale immediately and persists it when [persist] is true.
  void setLocale(Locale locale, {bool persist = true}) {
    final resolved = SupportedAppLanguage.fromCode(locale.languageCode).locale;
    if (state != resolved) {
      state = resolved;
    }
    if (persist) {
      final store = ref.read(preferencesStoreProvider);
      unawaited(store.writeString(kMenuLanguagePreferenceKey, resolved.languageCode));
    }
  }

  /// Convenience setter using an ISO 639-1 language code (`en`, `fr`, `es`,
  /// `de`, `it`, `nl`, `pt`).
  void setLanguageCode(String code, {bool persist = true}) {
    setLocale(SupportedAppLanguage.fromCode(code).locale, persist: persist);
  }

  /// Resets the menu language back to the English default.
  void resetToDefault({bool persist = true}) {
    setLocale(SupportedAppLanguage.english.locale, persist: persist);
  }
}

final appLocaleProvider = NotifierProvider<AppLocaleNotifier, Locale>(
  AppLocaleNotifier.new,
);
