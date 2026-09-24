import 'dart:async';
import 'dart:typed_data';
import 'package:flutter/foundation.dart';
import 'solution_storage.dart';

/// Status of the auto-save process for display in the status bar.
enum AutoSaveStatus {
  idle,    // No pending changes
  dirty,   // Has unsaved structural changes (waiting for debounce)
  saving,  // Currently writing to disk
  saved,   // Successfully persisted
  error,   // Write failed
}

/// Callback type: builds MessagePack bytes for the .f4p solution file.
typedef SolutionBytesBuilder = Future<Uint8List> Function();

/// AutoSaveService: debounced auto-save of structural solution data.
///
/// - Structural data (layouts, schemas, tables, occurrences) is saved automatically
///   to the local .f4p file within [debounceDelay] after the last change.
/// - Database row data is NEVER auto-saved; use explicit Export Data instead.
class AutoSaveService {
  AutoSaveService._();
  static final AutoSaveService instance = AutoSaveService._();

  // ─── Configuration ────────────────────────────────────────────────────────

  SolutionBytesBuilder? _buildSolutionBytes;
  StorageDirectoryRef? _directory;
  String _baseName = 'Untitled';
  Duration _debounceDelay = const Duration(seconds: 3);

  /// True if auto-save is configured and a directory is known.
  bool get isConfigured => _buildSolutionBytes != null && _directory != null;

  /// Configure the service. Call after the user selects a directory or loads a solution.
  void configure({
    required SolutionBytesBuilder buildSolutionBytes,
    required StorageDirectoryRef? directory,
    required String baseName,
    Duration debounceDelay = const Duration(seconds: 3),
  }) {
    _buildSolutionBytes = buildSolutionBytes;
    _directory = directory;
    _baseName = baseName.replaceAll(RegExp(r'\.(f4p|f4b|f4data)$'), '');
    _debounceDelay = debounceDelay;
  }

  /// Update directory and base name (called after Save As or first save).
  void updateLocation({
    required StorageDirectoryRef directory,
    required String baseName,
  }) {
    _directory = directory;
    _baseName = baseName.replaceAll(RegExp(r'\.(f4p|f4b|f4data)$'), '');
  }

  // ─── Status Stream ────────────────────────────────────────────────────────

  final _statusController = StreamController<AutoSaveStatus>.broadcast();
  Stream<AutoSaveStatus> get statusStream => _statusController.stream;

  AutoSaveStatus _currentStatus = AutoSaveStatus.idle;
  AutoSaveStatus get currentStatus => _currentStatus;

  DateTime? _lastSavedAt;
  DateTime? get lastSavedAt => _lastSavedAt;

  String? _lastError;
  String? get lastError => _lastError;

  void _emitStatus(AutoSaveStatus status) {
    _currentStatus = status;
    if (!_statusController.isClosed) {
      _statusController.add(status);
    }
  }

  // ─── Debounce Timer ──────────────────────────────────────────────────────

  Timer? _debounceTimer;

  /// Mark solution as having unsaved structural changes.
  /// Auto-save fires after [debounceDelay] unless another call resets the timer.
  void markDirty() {
    if (_buildSolutionBytes == null) return;

    _debounceTimer?.cancel();
    _emitStatus(AutoSaveStatus.dirty);

    if (_directory == null) {
      // No directory yet — remain dirty until user does Save As
      return;
    }

    _debounceTimer = Timer(_debounceDelay, _performAutoSave);
  }

  /// Flush any pending dirty state immediately (e.g., on Cmd+S).
  /// Returns true on success, false on error or if nothing to flush.
  Future<bool> flushNow() async {
    _debounceTimer?.cancel();
    _debounceTimer = null;

    if (_currentStatus == AutoSaveStatus.idle ||
        _currentStatus == AutoSaveStatus.saved) {
      return true;
    }

    if (_directory == null || _buildSolutionBytes == null) {
      return false;
    }

    return _performAutoSave();
  }

  Future<bool> _performAutoSave() async {
    if (_buildSolutionBytes == null || _directory == null) return false;

    _emitStatus(AutoSaveStatus.saving);

    try {
      final bytes = await _buildSolutionBytes!();
      final fileName = '$_baseName.f4p';

      await SolutionStorageService.saveSolutionFile(
        filename: fileName,
        bytes: bytes,
        directoryRef: _directory!,
      );

      _lastSavedAt = DateTime.now();
      _lastError = null;
      _emitStatus(AutoSaveStatus.saved);
      debugPrint('[AutoSave] Saved "$fileName" at $_lastSavedAt');
      return true;
    } catch (e) {
      _lastError = e.toString();
      _emitStatus(AutoSaveStatus.error);
      debugPrint('[AutoSave] Error: $e');
      return false;
    }
  }

  /// Dispose resources. Call when the app is closing.
  void dispose() {
    _debounceTimer?.cancel();
    _statusController.close();
  }
}
