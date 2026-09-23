import 'dart:js_interop';
import 'dart:js_interop_unsafe';
import 'dart:typed_data';
import 'package:web/web.dart' as web;

class PlatformDirectoryResult {
  final String displayName;
  final dynamic handleOrPath;
  const PlatformDirectoryResult({required this.displayName, required this.handleOrPath});
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

Future<PlatformDirectoryResult?> platformPickDirectory() async {
  try {
    final windowObj = web.window as JSObject;
    if (!windowObj.has('showDirectoryPicker')) {
      return null;
    }
    final promise = windowObj.callMethod<JSPromise>('showDirectoryPicker'.toJS);
    final dirHandle = await promise.toDart as JSObject;
    String name = 'Selected Folder';
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
      final options = {'create': true}.jsify() as JSObject;
      final filePromise = dirHandle.callMethod<JSPromise>(
        'getFileHandle'.toJS,
        entry.key.toJS,
        options,
      );
      final fileHandle = await filePromise.toDart as JSObject;
      final writablePromise = fileHandle.callMethod<JSPromise>('createWritable'.toJS);
      final writable = await writablePromise.toDart as JSObject;
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
