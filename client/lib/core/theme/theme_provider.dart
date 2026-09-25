import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'theme_model.dart';

class AppThemeNotifier extends Notifier<AppThemeDefinition> {
  @override
  AppThemeDefinition build() {
    // Default to Light (Clean Slate) as requested by the user
    return AppThemes.light;
  }

  void setTheme(AppThemeId themeId) {
    state = AppThemes.byId(themeId);
  }

  void setThemeByName(String name) {
    state = AppThemes.byName(name);
  }

  void toggleDarkLight() {
    if (state.isDark) {
      state = AppThemes.light;
    } else {
      state = AppThemes.dark;
    }
  }
}

final appThemeProvider = NotifierProvider<AppThemeNotifier, AppThemeDefinition>(
  AppThemeNotifier.new,
);
