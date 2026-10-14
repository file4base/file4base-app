import 'dart:async';
import 'dart:js_interop';
import 'dart:js_interop_unsafe';
import 'dart:typed_data';
import 'package:web/web.dart' as web;

class PlatformDirectoryResult {
  const PlatformDirectoryResult({required this.displayName, required this.handleOrPath});
  final String displayName;
  final dynamic handleOrPath;
}

class PlatformFileResult {
  const PlatformFileResult({required this.name, required this.bytes});
  final String name;
  final Uint8List bytes;
}

void triggerBrowserDownload(Uint8List bytes, String filename) {
  final blob = web.Blob([bytes.toJS].toJS);
  final url = web.URL.createObjectURL(blob);
  final anchor = web.document.createElement('a') as web.HTMLAnchorElement;
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  web.URL.revokeObjectURL(url);
}

void triggerPlatformPrint() {
  web.window.print();
}

void triggerPlatformQuit() {
  try {
    // Attempt standard window.close()
    web.window.close();
  } catch (_) {}

  // If the browser blocked window.close() (standard security for scripts not opened by window.open),
  // navigate to about:blank or display a clean 'Session Closed' page:
  try {
    final doc = web.document;
    doc.body?.innerHTML = '''
      <div style="display:flex;flex-direction:column;align-items:center;justify-content:center;height:100vh;background:#0f172a;color:#f8fafc;font-family:-apple-system,BlinkMacSystemFont,sans-serif;margin:0;">
        <div style="background:#1e293b;padding:32px 48px;border-radius:12px;border:1px solid #334155;text-align:center;box-shadow:0 20px 25px -5px rgba(0,0,0,0.5);">
          <div style="font-size:48px;margin-bottom:12px;">🔒</div>
          <h2 style="margin:0 0 8px 0;font-size:22px;color:#38bdf8;">File4Base Closed</h2>
          <p style="margin:0 0 16px 0;font-size:14px;color:#94a3b8;">All changes were safely saved to database and solution files.</p>
          <p style="margin:0;font-size:13px;color:#64748b;">You can safely close this browser tab now.</p>
        </div>
      </div>
    '''.toJS;
    try {
      web.window.close();
    } catch (_) {}
  } catch (_) {}
}

Future<PlatformFileResult?> platformPickFile({
  List<String> allowedExtensions = const ['f4p', 'f4b', 'f4data', 'msgpack'],
}) async {
  final completer = Completer<PlatformFileResult?>();

  final input = web.document.createElement('input') as web.HTMLInputElement;
  input.type = 'file';
  if (allowedExtensions.isNotEmpty) {
    input.accept = allowedExtensions.map((e) => '.$e').join(',');
  }
  input.style.display = 'none';

  web.document.body?.append(input);

  void cleanup() {
    input.remove();
  }

  input.addEventListener(
    'change',
    ((web.Event event) {
      final files = input.files;
      if (files == null || files.length == 0) {
        cleanup();
        if (!completer.isCompleted) completer.complete(null);
        return;
      }

      final file = files.item(0);
      if (file == null) {
        cleanup();
        if (!completer.isCompleted) completer.complete(null);
        return;
      }

      final reader = web.FileReader();
      reader.addEventListener(
        'load',
        ((web.Event _) {
          try {
            final arrayBuffer = reader.result! as JSArrayBuffer;
            final bytes = arrayBuffer.toDart.asUint8List();
            cleanup();
            if (!completer.isCompleted) {
              completer.complete(PlatformFileResult(name: file.name, bytes: bytes));
            }
          } catch (e) {
            cleanup();
            if (!completer.isCompleted) {
              completer.completeError('Failed to read file bytes: $e');
            }
          }
        }).toJS,
      );

      reader.addEventListener(
        'error',
        ((web.Event _) {
          cleanup();
          if (!completer.isCompleted) {
            completer.completeError('Error reading file from disk');
          }
        }).toJS,
      );

      reader.readAsArrayBuffer(file);
    }).toJS,
  );

  input.addEventListener(
    'cancel',
    ((web.Event _) {
      cleanup();
      if (!completer.isCompleted) completer.complete(null);
    }).toJS,
  );

  input.click();

  return completer.future;
}

Future<PlatformDirectoryResult?> platformPickDirectory() async {
  try {
    final windowObj = web.window as JSObject;
    if (!windowObj.has('showDirectoryPicker')) {
      return null;
    }
    final promise = windowObj.callMethod<JSPromise>('showDirectoryPicker'.toJS);
    final dirHandle = (await promise.toDart)! as JSObject;
    var name = 'Selected Folder';
    try {
      final nameProp = dirHandle.getProperty<JSString>('name'.toJS);
      name = nameProp.toDart;
    } catch (_) {}
    return PlatformDirectoryResult(displayName: name, handleOrPath: dirHandle);
  } catch (_) {
    return null;
  }
}

Future<bool> platformSaveFilesToDirectory({
  required dynamic directoryHandleOrPath,
  required Map<String, Uint8List> files,
}) async {
  if (directoryHandleOrPath == null) {
    for (final entry in files.entries) {
      triggerBrowserDownload(entry.value, entry.key);
    }
    return true;
  }

  try {
    final dirHandle = directoryHandleOrPath as JSObject;
    for (final entry in files.entries) {
      final options = {'create': true}.jsify()! as JSObject;
      final filePromise = dirHandle.callMethod<JSPromise>(
        'getFileHandle'.toJS,
        entry.key.toJS,
        options,
      );
      final fileHandle = (await filePromise.toDart)! as JSObject;
      final writablePromise = fileHandle.callMethod<JSPromise>('createWritable'.toJS);
      final writable = (await writablePromise.toDart)! as JSObject;
      final writePromise = writable.callMethod<JSPromise>('write'.toJS, entry.value.toJS);
      await writePromise.toDart;
      final closePromise = writable.callMethod<JSPromise>('close'.toJS);
      await closePromise.toDart;
    }
    return true;
  } catch (_) {
    for (final entry in files.entries) {
      triggerBrowserDownload(entry.value, entry.key);
    }
    return false;
  }
}
