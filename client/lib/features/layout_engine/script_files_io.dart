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

  /// A path is in a script folder only if the file it reaches is: a
  /// symbolic link (or Windows junction) inside a folder that leads out of
  /// it is refused. Links are resolved on the file, or for a file not made
  /// yet on its nearest folder that exists.
  @override
  String? resolve(String raw) {
    final path = super.resolve(raw);
    if (path == null) return null;
    final real = realPath(nativePath(path));
    if (real == null) return null;
    for (final root in [_temporaryDir.path, _documentsDir.path]) {
      final realRoot = realPath(root);
      if (realRoot != null && _inside(real, realRoot)) return path;
    }
    return null;
  }

  /// The path [native] reaches once every link of it is followed, or null
  /// when it cannot be told (a dangling link).
  static String? realPath(String native) {
    final missing = <String>[];
    var current = native;
    while (true) {
      if (FileSystemEntity.typeSync(current, followLinks: false) != FileSystemEntityType.notFound) {
        try {
          final resolved = Directory(current).resolveSymbolicLinksSync();
          return [resolved, ...missing.reversed].join(Platform.pathSeparator);
        } on FileSystemException {
          return null;
        }
      }
      final parent = File(current).parent.path;
      if (parent == current) return null;
      missing.add(current.substring(parent.length).replaceAll(RegExp(r'^[\\/]+'), ''));
      current = parent;
    }
  }

  static bool _inside(String path, String root) {
    final sep = Platform.pathSeparator;
    var p = path;
    var r = root.endsWith(sep) ? root : '$root$sep';
    if (Platform.isWindows) {
      p = p.toLowerCase();
      r = r.toLowerCase();
    }
    return p.startsWith(r) && p.length > r.length;
  }

  @override
  Future<void> clear() async {
    final temporary = _temporary;
    _temporary = null;
    if (temporary != null && temporary.existsSync()) {
      try {
        await temporary.delete(recursive: true);
      } on FileSystemException {
        // A file still open elsewhere; the folder is no longer the script's.
      }
    }
    await super.clear();
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
