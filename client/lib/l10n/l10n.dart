import 'package:file4base_client/l10n/app_localizations.dart';
import 'package:flutter/widgets.dart';

/// Supported menu and interface languages in File4Base.
class SupportedAppLanguage {
  const SupportedAppLanguage({
    required this.code,
    required this.nativeName,
    required this.badge,
  });

  final String code;
  final String nativeName;
  final String badge;

  Locale get locale => Locale(code);

  /// Localized name of this language according to the currently active [l10n].
  String localizedName(AppLocalizations l10n) {
    switch (code) {
      case 'fr':
        return l10n.languageNameFr;
      case 'es':
        return l10n.languageNameEs;
      case 'de':
        return l10n.languageNameDe;
      case 'it':
        return l10n.languageNameIt;
      case 'nl':
        return l10n.languageNameNl;
      case 'pt':
        return l10n.languageNamePt;
      case 'en':
      default:
        return l10n.languageNameEn;
    }
  }

  static const SupportedAppLanguage english = SupportedAppLanguage(
    code: 'en',
    nativeName: 'English',
    badge: 'EN',
  );

  static const SupportedAppLanguage french = SupportedAppLanguage(
    code: 'fr',
    nativeName: 'Français',
    badge: 'FR',
  );

  static const SupportedAppLanguage spanish = SupportedAppLanguage(
    code: 'es',
    nativeName: 'Español',
    badge: 'ES',
  );

  static const SupportedAppLanguage german = SupportedAppLanguage(
    code: 'de',
    nativeName: 'Deutsch',
    badge: 'DE',
  );

  static const SupportedAppLanguage italian = SupportedAppLanguage(
    code: 'it',
    nativeName: 'Italiano',
    badge: 'IT',
  );

  static const SupportedAppLanguage dutch = SupportedAppLanguage(
    code: 'nl',
    nativeName: 'Nederlands',
    badge: 'NL',
  );

  static const SupportedAppLanguage portuguese = SupportedAppLanguage(
    code: 'pt',
    nativeName: 'Português',
    badge: 'PT',
  );

  static const List<SupportedAppLanguage> all = [
    english,
    french,
    spanish,
    german,
    italian,
    dutch,
    portuguese,
  ];

  static SupportedAppLanguage fromCode(String? code) {
    final normalized = code?.trim().toLowerCase();
    for (final lang in all) {
      if (lang.code == normalized) return lang;
    }
    return english;
  }

  static bool isSupportedCode(String? code) {
    final normalized = code?.trim().toLowerCase();
    return all.any((lang) => lang.code == normalized);
  }
}

/// Safe access to [AppLocalizations] that falls back to English when a widget
/// is mounted in a test harness without localization delegates.
extension AppLocalizationsContextX on BuildContext {
  AppLocalizations get l10n =>
      AppLocalizations.of(this) ?? lookupAppLocalizations(const Locale('en'));
}

/// Returns the localized display label for a standard hex color swatch key.
String localizedFormatColorName(
  AppLocalizations l10n,
  String hex,
  String fallback,
) {
  switch (hex.toUpperCase()) {
    case '#000000':
      return l10n.colorBlack;
    case '#21262D':
      return l10n.colorCharcoal;
    case '#616161':
      return l10n.colorDarkGrey;
    case '#9E9E9E':
      return l10n.colorGrey;
    case '#F5F5F7':
      return l10n.colorLightGrey;
    case '#FFFFFF':
      return l10n.colorWhite;
    case '#1E88E5':
      return l10n.colorBlue;
    case '#0D47A1':
      return l10n.colorNavy;
    case '#00ACC1':
      return l10n.colorCyan;
    case '#2E7D32':
      return l10n.colorGreen;
    case '#F59E0B':
      return l10n.colorAmber;
    case '#F57C00':
      return l10n.colorOrange;
    case '#EF4444':
      return l10n.colorRed;
    case '#8E24AA':
      return l10n.colorPurple;
    default:
      return fallback;
  }
}
