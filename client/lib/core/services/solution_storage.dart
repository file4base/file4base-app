import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart';
import 'solution_storage_stub.dart'
    if (dart.library.js_interop) 'solution_storage_web.dart';

class PickedSolutionFile {
  final String name;
  final Uint8List bytes;

  const PickedSolutionFile({required this.name, required this.bytes});
}

class StorageDirectoryRef {
  final String displayName;
  final dynamic handleOrPath;

  const StorageDirectoryRef({
    required this.displayName,
    required this.handleOrPath,
  });
}

class SolutionStorageService {
  static StorageDirectoryRef? _defaultDirectoryRef;

  /// Returns the persistent reference to the destination folder selected by the
  /// user, so all subsequent exports and saves write to the same directory.
  static StorageDirectoryRef? get defaultDirectory => _defaultDirectoryRef;

  /// Sets (or clears) the persistent destination directory reference.
  static void setDefaultDirectory(StorageDirectoryRef? directoryRef) {
    _defaultDirectoryRef = directoryRef;
  }

  /// Prompts user to pick a destination directory on their hard drive and
  /// stores it as the persistent default directory reference.
  /// On Desktop, opens native folder picker (Finder / Explorer).
  /// On Chrome/Web, opens File System Access folder picker.
  static Future<StorageDirectoryRef?> pickDirectory({
    bool rememberAsDefault = true,
  }) async {
    final res = await platformPickDirectory();
    if (res == null) return null;
    final ref = StorageDirectoryRef(
      displayName: res.displayName,
      handleOrPath: res.handleOrPath,
    );
    if (rememberAsDefault) {
      _defaultDirectoryRef = ref;
    }
    return ref;
  }

  /// Saves BOTH .f4p (solution definition, without passwords) and
  /// .f4data (database records) into the given directory, the remembered
  /// default directory, or prompts the user once.
  /// NOTE: Prefer [saveSolutionFile] for structure-only auto-saves.
  static Future<bool> saveDualSolutionFiles({
    required String baseName,
    required Uint8List f4bBytes,
    required Uint8List f4dataBytes,
    StorageDirectoryRef? directoryRef,
  }) async {
    final sanitized = baseName.replaceAll(RegExp(r'\.(f4p|f4b|f4data)$'), '');
    final f4pName = '$sanitized.f4p';
    final f4dataName = '$sanitized.f4data';

    final files = {
      f4pName: f4bBytes,
      f4dataName: f4dataBytes,
    };

    final effectiveDir = directoryRef ?? _defaultDirectoryRef;
    if (effectiveDir != null) {
      _defaultDirectoryRef = effectiveDir;
      return await platformSaveFilesToDirectory(
        directoryHandleOrPath: effectiveDir.handleOrPath,
        files: files,
      );
    }

    // Prompt user to select destination folder (and remember it)
    final pickedDir = await pickDirectory();
    if (pickedDir != null) {
      return await platformSaveFilesToDirectory(
        directoryHandleOrPath: pickedDir.handleOrPath,
        files: files,
      );
    }

    // Fallback if directory picking cancelled or not supported: individual saves
    await saveFile(filename: f4pName, bytes: f4bBytes);
    await saveFile(filename: f4dataName, bytes: f4dataBytes);
    return true;
  }

  /// Saves a single .f4p solution file (structure only, no database data) to
  /// the given directory. Used by [AutoSaveService] for automatic persistence.
  static Future<bool> saveSolutionFile({
    required String filename,
    required Uint8List bytes,
    required StorageDirectoryRef directoryRef,
  }) async {
    final sanitized = filename.replaceAll(RegExp(r'\.(f4p|f4b|f4data)$'), '');
    final f4pName = '$sanitized.f4p';
    _defaultDirectoryRef = directoryRef;
    try {
      return await platformSaveFilesToDirectory(
        directoryHandleOrPath: directoryRef.handleOrPath,
        files: {f4pName: bytes},
      );
    } catch (_) {
      await saveFile(filename: f4pName, bytes: bytes, directoryRef: directoryRef);
      return true;
    }
  }

  /// Saves a file with the given filename and bytes.
  /// When a destination directory has been chosen (either passed via
  /// [directoryRef] or remembered in [defaultDirectory]), writes directly into
  /// that directory without re-prompting or falling back to browser Downloads.
  static Future<void> saveFile({
    required String filename,
    required Uint8List bytes,
    StorageDirectoryRef? directoryRef,
  }) async {
    final effectiveDir = directoryRef ?? _defaultDirectoryRef;
    if (effectiveDir != null) {
      _defaultDirectoryRef = effectiveDir;
      try {
        final written = await platformSaveFilesToDirectory(
          directoryHandleOrPath: effectiveDir.handleOrPath,
          files: {filename: bytes},
        );
        if (written) return;
      } catch (_) {}
    }

    if (kIsWeb) {
      triggerBrowserDownload(bytes, filename);
    } else {
      // The filter follows the file being saved: solution files, but also a
      // record export, a design report or the design as text.
      final extension = filename.contains('.') ? filename.split('.').last.toLowerCase() : '';
      final allowed = extension.isEmpty
          ? const ['f4p', 'f4b', 'f4data', 'msgpack']
          : [extension];
      final savedUri = await FilePicker.saveFile(
        dialogTitle: 'Save File',
        fileName: filename,
        bytes: bytes,
        type: FileType.custom,
        allowedExtensions: allowed,
      );
      if (savedUri != null) {
        String rawPath;
        try {
          rawPath = savedUri.toFilePath();
        } catch (_) {
          rawPath = savedUri.path;
        }
        if (rawPath.isNotEmpty) {
          final normalized = rawPath.replaceAll('\\', '/');
          final slashIdx = normalized.lastIndexOf('/');
          if (slashIdx > 0) {
            final dirPath = rawPath.substring(0, slashIdx);
            final dirName = dirPath.split(RegExp(r'[/\\]')).where((s) => s.isNotEmpty).lastOrNull ?? dirPath;
            _defaultDirectoryRef = StorageDirectoryRef(
              displayName: dirName,
              handleOrPath: dirPath,
            );
          }
        }
      }
    }
  }

  /// Triggers platform printing. On Web Direct, opens native browser print dialog.
  static void triggerPrint() {
    triggerPlatformPrint();
  }

  /// Triggers platform quit/close.
  /// On Desktop, terminates process cleanly.
  /// On Web Direct, closes the browser tab/window.
  static void triggerQuit() {
    triggerPlatformQuit();
  }

  /// Prompts the user to pick a solution file (.f4p new, .f4b legacy) or data file (.f4data).
  /// On Desktop, uses native OS file picker.
  /// On Web, uses HTML5 file chooser without throwing UnimplementedError.
  static Future<PickedSolutionFile?> pickFile({
    List<String> allowedExtensions = const ['f4p', 'f4b', 'f4data', 'msgpack'],
  }) async {
    final res = await platformPickFile(allowedExtensions: allowedExtensions);
    if (res == null) return null;
    return PickedSolutionFile(name: res.name, bytes: res.bytes);
  }
}
