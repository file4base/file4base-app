import 'dart:io';
import 'dart:typed_data';

import 'package:file4base_client/features/layout_engine/script_files.dart';

/// The desktop folders of a script: a temporary folder made for this run,
/// and `Documents/File4Base` in the user's home folder.
ScriptFileStore createPlatformScriptFileStore() => IoScriptFileStore();

/// Script folders on the disk (#133).
class IoScriptFileStore extends ScriptFileStore {
  IoScriptFileStore({Directory? temporary, Directory? documents})
      : _temporary = temporary,
        _documents = documents;

  Directory? _temporary;
  Directory? _documents;

  Directory get _temporaryDir =>
      // createTempSync makes a folder only this user can open.
      _temporary ??= Directory.systemTemp.createTempSync('file4base-');

  Directory get _documentsDir {
    if (_documents != null) return _documents!;
    final home = Platform.environment[Platform.isWindows ? 'USERPROFILE' : 'HOME'];
    _documents = home == null || home.isEmpty
        ? Directory('${_temporaryDir.path}${Platform.pathSeparator}Documents')
        : Directory([home, 'Documents', 'File4Base'].join(Platform.pathSeparator));
    return _documents!;
  }

  /// The FileMaker form of a native folder: forward slashes, a leading
  /// slash before a drive letter, a trailing slash.
  static String fileMakerPath(String native) {
    var p = native.replaceAll(r'\', '/');
    if (!p.startsWith('/')) p = '/$p';
    return p.endsWith('/') ? p : '$p/';
  }

  @override
  String get temporaryPath => fileMakerPath(_temporaryDir.path);

  @override
  String get documentsPath => fileMakerPath(_documentsDir.path);

  @override
  String nativePath(String path) {
    if (Platform.isWindows && RegExp('^/[A-Za-z]:/').hasMatch(path)) {
      return path.substring(1).replaceAll('/', r'\');
    }
    return path;
  }

  @override
  Future<void> write(String path, Uint8List bytes) async {
    final file = File(nativePath(path));
    await file.parent.create(recursive: true);
    await file.writeAsBytes(bytes, flush: true);
  }

  @override
  Future<Uint8List?> read(String path) async {
    final file = File(nativePath(path));
    if (!file.existsSync()) return null;
    return file.readAsBytes();
  }
}
