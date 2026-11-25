import 'dart:convert';
import 'package:web/web.dart' as web;

const String _storageKey = 'file4base.preferences.v1';

Map<String, dynamic> _readAll() {
  try {
    final raw = web.window.localStorage.getItem(_storageKey);
    if (raw == null || raw.trim().isEmpty) return <String, dynamic>{};
    final decoded = jsonDecode(raw);
    if (decoded is Map) {
      return Map<String, dynamic>.from(decoded);
    }
  } on Object catch (_) {}
  return <String, dynamic>{};
}

void _writeAll(Map<String, dynamic> data) {
  try {
    web.window.localStorage.setItem(_storageKey, jsonEncode(data));
  } on Object catch (_) {}
}

String? platformReadPreference(String key) {
  final value = _readAll()[key];
  return value is String ? value : null;
}

Future<void> platformWritePreference(String key, String value) async {
  final data = _readAll()..[key] = value;
  _writeAll(data);
}

Future<void> platformRemovePreference(String key) async {
  final data = _readAll()..remove(key);
  _writeAll(data);
}
