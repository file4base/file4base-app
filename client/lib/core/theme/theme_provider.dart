import 'dart:convert';

import 'package:file4base_client/core/theme/theme_model.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

class AppThemeNotifier extends Notifier<AppThemeDefinition> {
  final List<AppThemeDefinition> _customThemes = [];
  String? _lastCopiedThemeJson;

  @override
  AppThemeDefinition build() {
    // Default to Light (Clean Slate) as requested by the user
    return AppThemes.light;
  }

  List<AppThemeDefinition> get availableThemes => [
        ...AppThemes.allThemes,
        ..._customThemes,
      ];

  List<AppThemeDefinition> get customThemes =>
      List<AppThemeDefinition>.unmodifiable(_customThemes);

  String? get lastCopiedThemeJson => _lastCopiedThemeJson;

  void setTheme(AppThemeId themeId) {
    state = AppThemes.byId(themeId);
  }

  void setThemeDefinition(AppThemeDefinition theme) {
    state = theme;
  }

  void setThemeByName(String name) {
    final customMatch = _customThemes.where(
      (t) =>
          t.uniqueKey.toLowerCase() == name.toLowerCase() ||
          t.name.toLowerCase() == name.toLowerCase(),
    );
    if (customMatch.isNotEmpty) {
      state = customMatch.first;
      return;
    }
    state = AppThemes.byName(name);
  }

  String copyThemeToJson(AppThemeDefinition theme) {
    final encoded = const JsonEncoder.withIndent('  ').convert(theme.toJson());
    _lastCopiedThemeJson = encoded;
    return encoded;
  }

  AppThemeDefinition importThemeFromJson(String rawJson, {bool apply = true}) {
    final decoded = jsonDecode(rawJson);
    if (decoded is! Map) {
      throw const FormatException('Theme JSON root must be an object.');
    }
    final imported =
        AppThemeDefinition.fromJson(Map<String, dynamic>.from(decoded));
    final idx = _customThemes.indexWhere(
      (t) =>
          t.uniqueKey.toLowerCase() == imported.uniqueKey.toLowerCase() ||
          t.name.toLowerCase() == imported.name.toLowerCase(),
    );
    if (idx >= 0) {
      _customThemes[idx] = imported;
    } else {
      _customThemes.add(imported);
    }
    if (apply) {
      state = imported;
    }
    return imported;
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
