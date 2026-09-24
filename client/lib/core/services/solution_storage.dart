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
  /// Prompts user to pick a destination directory on their hard drive.
  /// On Desktop, opens native folder picker (Finder / Explorer).
  /// On Chrome/Web, opens File System Access folder picker.
  static Future<StorageDirectoryRef?> pickDirectory() async {
    final res = await platformPickDirectory();
    if (res == null) return null;
    return StorageDirectoryRef(
      displayName: res.displayName,
      handleOrPath: res.handleOrPath,
    );
  }

  /// Saves BOTH .f4p (solution definition & encoded credentials) and
  /// .f4data (database records) into the given directory or prompts the user.
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

    if (directoryRef != null) {
      return await platformSaveFilesToDirectory(
        directoryHandleOrPath: directoryRef.handleOrPath,
        files: files,
      );
    }

    // Prompt user to select destination folder
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
    try {
      return await platformSaveFilesToDirectory(
        directoryHandleOrPath: directoryRef.handleOrPath,
        files: {f4pName: bytes},
      );
    } catch (_) {
      await saveFile(filename: f4pName, bytes: bytes);
      return true;
    }
  }

  /// Saves a file with the given filename and bytes.
  /// On Web, triggers a browser file download.
  /// On Desktop, prompts save dialog and saves the bytes.
  static Future<void> saveFile({
    required String filename,
    required Uint8List bytes,
  }) async {
    if (kIsWeb) {
      triggerBrowserDownload(bytes, filename);
    } else {
      await FilePicker.saveFile(
        dialogTitle: 'Save Solution File',
        fileName: filename,
        bytes: bytes,
        type: FileType.custom,
        allowedExtensions: ['f4p', 'f4b', 'f4data', 'msgpack'],
      );
    }
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
