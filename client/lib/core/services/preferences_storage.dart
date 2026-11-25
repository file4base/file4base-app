import 'package:file4base_client/core/services/preferences_storage_io.dart'
    if (dart.library.js_interop) 'preferences_storage_web.dart';

/// Key-value persistence interface for client-side application preferences.
abstract class PreferencesStore {
  String? readString(String key);
  Future<void> writeString(String key, String value);
  Future<void> remove(String key);
}

/// Cross-platform store backed by a JSON file in the OS config directory on
/// desktop platforms and `window.localStorage` on web.
class PlatformPreferencesStore implements PreferencesStore {
  @override
  String? readString(String key) => platformReadPreference(key);

  @override
  Future<void> writeString(String key, String value) =>
      platformWritePreference(key, value);

  @override
  Future<void> remove(String key) => platformRemovePreference(key);
}

/// Deterministic in-memory store for unit and widget tests.
class InMemoryPreferencesStore implements PreferencesStore {
  InMemoryPreferencesStore([Map<String, String>? initial])
      : _values = Map<String, String>.from(initial ?? const {});

  final Map<String, String> _values;

  @override
  String? readString(String key) => _values[key];

  @override
  Future<void> writeString(String key, String value) async {
    _values[key] = value;
  }

  @override
  Future<void> remove(String key) async {
    _values.remove(key);
  }
}
