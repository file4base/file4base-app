import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart';
import 'solution_storage_stub.dart'
    if (dart.library.js_interop) 'solution_storage_web.dart';

class PickedSolutionFile {
  final String name;
  final Uint8List bytes;

  const PickedSolutionFile({required this.name, required this.bytes});
}

class SolutionStorageService {
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
        allowedExtensions: ['f4b', 'f4data', 'msgpack'],
      );
    }
  }

  /// Prompts the user to pick a solution file (.f4b) or data file (.f4data).
  static Future<PickedSolutionFile?> pickFile({
    List<String> allowedExtensions = const ['f4b', 'f4data', 'msgpack'],
  }) async {
    final files = await FilePicker.pickFiles(
      type: FileType.custom,
      allowedExtensions: allowedExtensions,
    );

    if (files.isEmpty) {
      return null;
    }

    final file = files.first;
    final bytes = await file.xFile.readAsBytes();
    return PickedSolutionFile(name: file.name, bytes: bytes);
  }
}
