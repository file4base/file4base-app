import 'dart:convert';
import 'dart:io';

File? _resolvePreferencesFile() {
  try {
    final env = Platform.environment;
    String? baseDir;
    if (Platform.isMacOS) {
      final home = env['HOME'];
      if (home != null && home.isNotEmpty) {
        baseDir = '$home/Library/Application Support/File4Base';
      }
    } else if (Platform.isWindows) {
      final appData = env['APPDATA'] ?? env['USERPROFILE'];
      if (appData != null && appData.isNotEmpty) {
        baseDir = '$appData${Platform.pathSeparator}File4Base';
      }
    } else {
      final xdg = env['XDG_CONFIG_HOME'];
      if (xdg != null && xdg.isNotEmpty) {
        baseDir = '$xdg/file4base';
      } else {
        final home = env['HOME'];
        if (home != null && home.isNotEmpty) {
          baseDir = '$home/.config/file4base';
        }
      }
    }
    if (baseDir == null || baseDir.isEmpty) return null;
    return File('$baseDir${Platform.pathSeparator}preferences.json');
  } on Object catch (_) {
    return null;
  }
}

Map<String, dynamic> _readAll() {
  try {
    final file = _resolvePreferencesFile();
    if (file == null || !file.existsSync()) return <String, dynamic>{};
    final raw = file.readAsStringSync();
    if (raw.trim().isEmpty) return <String, dynamic>{};
    final decoded = jsonDecode(raw);
    if (decoded is Map) {
      return Map<String, dynamic>.from(decoded);
    }
  } on Object catch (_) {}
  return <String, dynamic>{};
}

Future<void> _writeAll(Map<String, dynamic> data) async {
  try {
    final file = _resolvePreferencesFile();
    if (file == null) return;
    if (!file.parent.existsSync()) {
      await file.parent.create(recursive: true);
    }
    const encoder = JsonEncoder.withIndent('  ');
    await file.writeAsString(encoder.convert(data));
  } on Object catch (_) {}
}

String? platformReadPreference(String key) {
  final value = _readAll()[key];
  return value is String ? value : null;
}

Future<void> platformWritePreference(String key, String value) async {
  final data = _readAll()..[key] = value;
  await _writeAll(data);
}

Future<void> platformRemovePreference(String key) async {
  final data = _readAll()..remove(key);
  await _writeAll(data);
}
