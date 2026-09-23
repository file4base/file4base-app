import 'dart:io';
import 'dart:typed_data';
import 'package:file_picker/file_picker.dart';

class PlatformDirectoryResult {
  final String displayName;
  final dynamic handleOrPath;
  const PlatformDirectoryResult({required this.displayName, required this.handleOrPath});
}

void triggerBrowserDownload(Uint8List bytes, String filename) {
  // Desktop target stub - handled via file picker/local disk writing
}

Future<PlatformDirectoryResult?> platformPickDirectory() async {
  final path = await FilePicker.getDirectoryPath(
    dialogTitle: 'Select Destination Folder on Hard Drive',
  );
  if (path == null || path.isEmpty) return null;
  final name = path.split(RegExp(r'[/\\]')).where((s) => s.isNotEmpty).lastOrNull ?? path;
  return PlatformDirectoryResult(displayName: name, handleOrPath: path);
}

Future<bool> platformSaveFilesToDirectory({
  required dynamic directoryHandleOrPath,
  required Map<String, Uint8List> files,
}) async {
  if (directoryHandleOrPath is! String || directoryHandleOrPath.isEmpty) {
    return false;
  }
  final dir = Directory(directoryHandleOrPath);
  if (!dir.existsSync()) {
    dir.createSync(recursive: true);
  }
  for (final entry in files.entries) {
    final filePath = '$directoryHandleOrPath/${entry.key}';
    final file = File(filePath);
    file.writeAsBytesSync(entry.value);
  }
  return true;
}
