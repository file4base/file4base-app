import 'package:flutter/material.dart';

enum AppThemeId {
  light,
  dark,
  monokai,
  dracula,
  nord,
  solarizedLight,
  solarizedDark,
}

class AppThemeDefinition {
  final AppThemeId id;
  final String name;
  final String description;
  final String category;
  final bool isDark;

  // Base canvas & surfaces
  final Color background;
  final Color surface;
  final Color surfaceContainer;
  final Color border;

  // Typography
  final Color textPrimary;
  final Color textSecondary;

  // Accents
  final Color primaryAccent;
  final Color secondaryAccent;
  final Color successColor;
  final Color warningColor;
  final Color errorColor;

  // Script & Code Categories
  final Color navColor;
  final Color recordsColor;
  final Color controlColor;
  final Color fieldsColor;
  final Color integrationColor;

  const AppThemeDefinition({
    required this.id,
    required this.name,
    required this.description,
    required this.category,
    required this.isDark,
    required this.background,
    required this.surface,
    required this.surfaceContainer,
    required this.border,
    required this.textPrimary,
    required this.textSecondary,
    required this.primaryAccent,
    required this.secondaryAccent,
    this.successColor = const Color(0xFF10B981),
    this.warningColor = const Color(0xFFF59E0B),
    this.errorColor = const Color(0xFFEF4444),
    required this.navColor,
    required this.recordsColor,
    required this.controlColor,
    required this.fieldsColor,
    required this.integrationColor,
  });

  ThemeData toThemeData() {
    final brightness = isDark ? Brightness.dark : Brightness.light;
    return ThemeData(
      useMaterial3: true,
      brightness: brightness,
      scaffoldBackgroundColor: background,
      canvasColor: background,
      cardColor: surface,
      dividerColor: border,
      colorScheme: ColorScheme(
        brightness: brightness,
        primary: primaryAccent,
        onPrimary: isDark ? const Color(0xFF0B1120) : Colors.white,
        secondary: secondaryAccent,
        onSecondary: isDark ? const Color(0xFF0B1120) : Colors.white,
        error: errorColor,
        onError: Colors.white,
        surface: surface,
        onSurface: textPrimary,
        surfaceContainerHighest: surfaceContainer,
        onSurfaceVariant: textSecondary,
        outline: border,
      ),
      appBarTheme: AppBarTheme(
        backgroundColor: surface,
        foregroundColor: textPrimary,
        elevation: 0,
        iconTheme: IconThemeData(color: textPrimary),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: surface,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(12),
          side: BorderSide(color: border),
        ),
      ),
    );
  }
}

class AppThemes {
  static const AppThemeDefinition light = AppThemeDefinition(
    id: AppThemeId.light,
    name: 'Light (Clean Slate)',
    description: 'Tema estándar blanco limpio de alta legibilidad, ideal para entornos de oficina y diseño claro.',
    category: 'Estándar',
    isDark: false,
    background: Color(0xFFF8FAFC), // Slate 50
    surface: Color(0xFFFFFFFF),    // Pure white
    surfaceContainer: Color(0xFFF1F5F9), // Slate 100
    border: Color(0xFFCBD5E1),     // Slate 300
    textPrimary: Color(0xFF0F172A), // Slate 900
    textSecondary: Color(0xFF64748B), // Slate 500
    primaryAccent: Color(0xFF0284C7), // Sky 600
    secondaryAccent: Color(0xFF2563EB), // Blue 600
    successColor: Color(0xFF059669),
    warningColor: Color(0xFFD97706),
    errorColor: Color(0xFFDC2626),
    navColor: Color(0xFF7C3AED),    // Violet 600
    recordsColor: Color(0xFF059669),// Emerald 600
    controlColor: Color(0xFFD97706),// Amber 600
    fieldsColor: Color(0xFF0284C7), // Sky 600
    integrationColor: Color(0xFFE11D48), // Rose 600
  );

  static const AppThemeDefinition dark = AppThemeDefinition(
    id: AppThemeId.dark,
    name: 'Dark Modern (Sophisticated)',
    description: 'Modo oscuro medianoche con paneles grafito y acentos cian, diseñado para máxima comodidad visual.',
    category: 'Desarrollo y Oscuro',
    isDark: true,
    background: Color(0xFF0B1120), // Deep Midnight
    surface: Color(0xFF111827),    // Dark surface
    surfaceContainer: Color(0xFF1E293B), // Slate 800
    border: Color(0xFF1F2937),     // Gray 800
    textPrimary: Color(0xFFF8FAFC), // Slate 50
    textSecondary: Color(0xFF94A3B8), // Slate 400
    primaryAccent: Color(0xFF38BDF8), // Cyan 400
    secondaryAccent: Color(0xFF60A5FA), // Blue 400
    successColor: Color(0xFF10B981),
    warningColor: Color(0xFFF59E0B),
    errorColor: Color(0xFFEF4444),
    navColor: Color(0xFFA855F7),    // Violet 500
    recordsColor: Color(0xFF10B981),// Emerald 500
    controlColor: Color(0xFFF59E0B),// Amber 500
    fieldsColor: Color(0xFF38BDF8), // Cyan 400
    integrationColor: Color(0xFFF43F5E), // Rose 500
  );

  static const AppThemeDefinition monokai = AppThemeDefinition(
    id: AppThemeId.monokai,
    name: 'Monokai Pro (Code Studio)',
    description: 'La legendaria paleta de desarrollo Monokai: fondo carbón cálido con contrastes vivos rosa, verde y naranja.',
    category: 'Desarrollo y Oscuro',
    isDark: true,
    background: Color(0xFF272822), // Monokai Charcoal
    surface: Color(0xFF1E1F1C),    // Monokai Panel
    surfaceContainer: Color(0xFF3E3D32), // Monokai Line
    border: Color(0xFF49483E),     // Monokai Border
    textPrimary: Color(0xFFF8F8F2), // Monokai Off-white
    textSecondary: Color(0xFF908E78), // Monokai Dim text
    primaryAccent: Color(0xFFFD971F), // Monokai Orange
    secondaryAccent: Color(0xFFA6E22E), // Monokai Green
    successColor: Color(0xFFA6E22E), // Green
    warningColor: Color(0xFFFD971F), // Orange
    errorColor: Color(0xFFF92672),   // Monokai Pink
    navColor: Color(0xFFAE81FF),    // Monokai Purple
    recordsColor: Color(0xFFA6E22E),// Monokai Green
    controlColor: Color(0xFFFD971F),// Monokai Orange
    fieldsColor: Color(0xFF66D9EF), // Monokai Cyan
    integrationColor: Color(0xFFF92672), // Monokai Pink
  );

  static const AppThemeDefinition dracula = AppThemeDefinition(
    id: AppThemeId.dracula,
    name: 'Dracula Midnight',
    description: 'El célebre tema de vampiros con fondo índigo oscuro, acentos púrpura neón y resaltados pastel.',
    category: 'Desarrollo y Oscuro',
    isDark: true,
    background: Color(0xFF282A36), // Dracula Background
    surface: Color(0xFF21222C),    // Dracula Surface
    surfaceContainer: Color(0xFF44475A), // Current Line
    border: Color(0xFF6272A4),     // Dracula Comment
    textPrimary: Color(0xFFF8F8F2), // Dracula Foreground
    textSecondary: Color(0xFF8BE9FD), // Dracula Cyan / secondary
    primaryAccent: Color(0xFFBD93F9), // Dracula Purple
    secondaryAccent: Color(0xFFFF79C6), // Dracula Pink
    successColor: Color(0xFF50FA7B), // Dracula Green
    warningColor: Color(0xFFF1FA8C), // Dracula Yellow
    errorColor: Color(0xFFFF5555),   // Dracula Red
    navColor: Color(0xFFBD93F9),    // Purple
    recordsColor: Color(0xFF50FA7B),// Green
    controlColor: Color(0xFFF1FA8C),// Yellow
    fieldsColor: Color(0xFF8BE9FD), // Cyan
    integrationColor: Color(0xFFFF79C6), // Pink
  );

  static const AppThemeDefinition nord = AppThemeDefinition(
    id: AppThemeId.nord,
    name: 'Nordic Frost (Nord)',
    description: 'Paleta ártica escandinava en azules polares, nieve y tonos fríos armónicos sin fatiga visual.',
    category: 'Moderno y Frío',
    isDark: true,
    background: Color(0xFF2E3440), // Polar Night 0
    surface: Color(0xFF3B4252),    // Polar Night 1
    surfaceContainer: Color(0xFF434C5E), // Polar Night 2
    border: Color(0xFF4C566A),     // Polar Night 3
    textPrimary: Color(0xFFECEFF4), // Snow Storm
    textSecondary: Color(0xFFD8DEE9), // Snow Storm Dim
    primaryAccent: Color(0xFF88C0D0), // Frost Cyan
    secondaryAccent: Color(0xFF81A1C1), // Frost Blue
    successColor: Color(0xFFA3BE8C), // Aurora Green
    warningColor: Color(0xFFEBCB8B), // Aurora Yellow
    errorColor: Color(0xFFBF616A),   // Aurora Red
    navColor: Color(0xFFB48EAD),    // Aurora Purple
    recordsColor: Color(0xFFA3BE8C),// Green
    controlColor: Color(0xFFEBCB8B),// Yellow
    fieldsColor: Color(0xFF88C0D0), // Frost
    integrationColor: Color(0xFFD08770), // Aurora Orange
  );

  static const AppThemeDefinition solarizedLight = AppThemeDefinition(
    id: AppThemeId.solarizedLight,
    name: 'Solarized Light',
    description: 'La formulación científica de Ethan Schoonover en tonos pergamino cálidos y tinta cian suave.',
    category: 'Clásicos y Cálidos',
    isDark: false,
    background: Color(0xFFFDF6E3), // Base 3
    surface: Color(0xFFEEE8D5),    // Base 2
    surfaceContainer: Color(0xFFE0D9C5),
    border: Color(0xFF93A1A1),     // Base 1
    textPrimary: Color(0xFF073642), // Base 02
    textSecondary: Color(0xFF586E75), // Base 01
    primaryAccent: Color(0xFF268BD2), // Blue
    secondaryAccent: Color(0xFF2AA198), // Cyan
    successColor: Color(0xFF859900), // Green
    warningColor: Color(0xFFB58900), // Yellow
    errorColor: Color(0xFFDC322F),   // Red
    navColor: Color(0xFF6C71C4),    // Violet
    recordsColor: Color(0xFF859900),// Green
    controlColor: Color(0xFFCB4B16),// Orange
    fieldsColor: Color(0xFF268BD2), // Blue
    integrationColor: Color(0xFFD33682), // Magenta
  );

  static const AppThemeDefinition solarizedDark = AppThemeDefinition(
    id: AppThemeId.solarizedDark,
    name: 'Solarized Dark',
    description: 'Variante oscura de Solarized con base verde azulado marino y contrastes precisos para terminal.',
    category: 'Clásicos y Cálidos',
    isDark: true,
    background: Color(0xFF002B36), // Base 03
    surface: Color(0xFF073642),    // Base 02
    surfaceContainer: Color(0xFF0A4452),
    border: Color(0xFF586E75),     // Base 01
    textPrimary: Color(0xFF93A1A1), // Base 1
    textSecondary: Color(0xFF839496), // Base 0
    primaryAccent: Color(0xFF2AA198), // Cyan
    secondaryAccent: Color(0xFF268BD2), // Blue
    successColor: Color(0xFF859900), // Green
    warningColor: Color(0xFFB58900), // Yellow
    errorColor: Color(0xFFDC322F),   // Red
    navColor: Color(0xFF6C71C4),    // Violet
    recordsColor: Color(0xFF859900),// Green
    controlColor: Color(0xFFCB4B16),// Orange
    fieldsColor: Color(0xFF2AA198), // Cyan
    integrationColor: Color(0xFFD33682), // Magenta
  );

  static const List<AppThemeDefinition> allThemes = [
    light,
    dark,
    monokai,
    dracula,
    nord,
    solarizedLight,
    solarizedDark,
  ];

  static AppThemeDefinition byId(AppThemeId id) {
    return allThemes.firstWhere((t) => t.id == id, orElse: () => light);
  }

  static AppThemeDefinition byName(String name) {
    return allThemes.firstWhere(
      (t) => t.id.name.toLowerCase() == name.toLowerCase(),
      orElse: () => light,
    );
  }
}
