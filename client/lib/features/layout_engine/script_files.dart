import 'dart:typed_data';

import 'package:file4base_client/features/layout_engine/script_files_stub.dart'
    if (dart.library.io) 'package:file4base_client/features/layout_engine/script_files_io.dart' as platform;
import 'package:pdf/pdf.dart';
import 'package:pdf/widgets.dart' as pw;

/// Where a File4Base script reads and writes files (#133).
///
/// FileMaker scripts name files by path: Save Records as PDF writes one,
/// Send Mail attaches it, Insert File reads it. A File4Base script reaches
/// two folders of its own, never the rest of the user's disk:
///
/// * the **temporary folder**, `Get ( TemporaryPath )`: a folder made for
///   this run of File4Base (on the desktop, in the system's temporary
///   folder; in the browser, in memory). Like FileMaker's, it is the place
///   for a file a script makes only to send or insert it.
/// * the **documents folder**, `Get ( DocumentsPath )` and
///   `Get ( DesktopPath )`: `Documents/File4Base` in the user's home folder
///   on the desktop (in memory in the browser). A script never writes on the
///   desktop itself; its desktop is this folder.
///
/// A path is written as FileMaker writes it: `/C:/Users/…/` on Windows,
/// `/Users/…/` elsewhere, optionally after `file:`, `filemac:`, `filewin:`
/// or `filelinux:`. A path relative to the file (`file:Report.pdf`) is in the
/// documents folder, as the database file's folder of FileMaker has no
/// counterpart here. A path list (one path per line) uses its first path
/// that lies in one of the two folders. Any other place is refused: the step
/// reports FileMaker's error 800 (cannot create the file) or 100 (file
/// missing), so a script never writes over, or sends, a file of the user's
/// that it was not given.
abstract class ScriptFileStore {
  static ScriptFileStore? _instance;

  /// The store of this platform: the disk on the desktop, memory in the
  /// browser.
  static ScriptFileStore get instance => _instance ??= platform.createPlatformScriptFileStore();
  static set instance(ScriptFileStore store) => _instance = store;

  /// `Get ( TemporaryPath )`, a FileMaker path that ends with a slash.
  String get temporaryPath;

  /// `Get ( DocumentsPath )` and `Get ( DesktopPath )`, a FileMaker path
  /// that ends with a slash.
  String get documentsPath;

  /// Writes [bytes] at [path], a path [resolve] answered.
  Future<void> write(String path, Uint8List bytes);

  /// The file at [path], a path [resolve] answered, or null when there is
  /// none.
  Future<Uint8List?> read(String path);

  /// The native path of [path] for the operating system, to open it.
  String nativePath(String path) => path;

  /// The pages of the PDFs saved during this run, by path, so Save Records
  /// as PDF can append to one.
  final Map<String, List<PdfSheetPage>> savedPdfPages = {};

  /// The file a FileMaker path list names, or null when none of its paths
  /// lies in one of the script folders.
  String? resolve(String raw) =>
      resolveScriptPath(raw, roots: [temporaryPath, documentsPath], relativeRoot: documentsPath);
}

final RegExp _prefix = RegExp('^(file|filemac|filewin|filelinux):', caseSensitive: false);

/// Resolves a FileMaker path list against the script folders [roots]
/// (FileMaker paths ending with a slash); a relative path is in
/// [relativeRoot]. Null when no path of the list is in one of them.
String? resolveScriptPath(String raw, {required List<String> roots, required String relativeRoot}) {
  for (final line in raw.split(RegExp('[\r\n\u00b6]'))) {
    var path = line.trim();
    if (path.isEmpty) continue;
    path = path.replaceFirst(_prefix, '').replaceAll(r'\', '/');
    final absolute = path.startsWith('/') || RegExp('^[A-Za-z]:/').hasMatch(path);
    if (absolute) {
      if (!path.startsWith('/')) path = '/$path';
    } else {
      path = relativeRoot + path;
    }
    final normalized = _normalize(path);
    if (normalized == null || normalized.endsWith('/')) continue;
    for (final root in roots) {
      if (_within(normalized, root)) return normalized;
    }
  }
  return null;
}

/// [path] without `.` and `..` segments or doubled slashes, or null when a
/// `..` climbs above the top.
String? _normalize(String path) {
  final out = <String>[];
  final parts = path.split('/');
  for (var i = 0; i < parts.length; i++) {
    final part = parts[i];
    if (part.isEmpty || part == '.') {
      if (i == parts.length - 1) out.add('');
      continue;
    }
    if (part == '..') {
      if (out.isEmpty) return null;
      out.removeLast();
      continue;
    }
    out.add(part);
  }
  return '/${out.join('/')}';
}

bool _within(String path, String root) {
  // Windows paths are not case-sensitive; the drive letter tells them.
  final windows = RegExp('^/[A-Za-z]:/').hasMatch(root);
  final p = windows ? path.toLowerCase() : path;
  final r = windows ? root.toLowerCase() : root;
  return p.startsWith(r) && p.length > r.length;
}

/// The folders of the browser, and of tests: files are kept in memory for
/// the session.
class MemoryScriptFileStore extends ScriptFileStore {
  MemoryScriptFileStore({
    this.temporaryPath = '/File4Base/Temporary/',
    this.documentsPath = '/File4Base/Documents/',
  });

  @override
  final String temporaryPath;
  @override
  final String documentsPath;

  final Map<String, Uint8List> files = {};

  @override
  Future<void> write(String path, Uint8List bytes) async => files[path] = bytes;

  @override
  Future<Uint8List?> read(String path) async => files[path];
}

/// One page of a PDF that File4Base draws: an image of the sheet, the size
/// of the paper.
class PdfSheetPage {
  const PdfSheetPage({required this.width, required this.height, required this.png});

  /// Paper size, in points.
  final double width;
  final double height;
  final Uint8List png;
}

/// The PDF of [pages], in order.
Future<Uint8List> assemblePdf(List<PdfSheetPage> pages, {String title = ''}) async {
  final doc = pw.Document(title: title, creator: 'File4Base');
  for (final page in pages) {
    final format = PdfPageFormat(page.width, page.height, marginAll: 0);
    final image = pw.MemoryImage(page.png);
    doc.addPage(pw.Page(
      pageFormat: format,
      margin: pw.EdgeInsets.zero,
      build: (_) => pw.Align(
        alignment: pw.Alignment.topLeft,
        child: pw.Image(image, width: format.width, fit: pw.BoxFit.fitWidth),
      ),
    ));
  }
  return doc.save();
}
