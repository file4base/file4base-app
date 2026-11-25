@TestOn('vm')
library;

import 'dart:io';
import 'dart:typed_data';

import 'package:file4base_client/features/layout_engine/script_files_io.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  late Directory base;
  late Directory temporary;
  late Directory documents;
  late Directory outside;

  setUp(() {
    base = Directory.systemTemp.createTempSync('f4b-files-');
    temporary = Directory('${base.path}/tmp')..createSync();
    documents = Directory('${base.path}/docs')..createSync();
    outside = Directory('${base.path}/outside')..createSync();
  });

  tearDown(() {
    if (base.existsSync()) base.deleteSync(recursive: true);
  });

  String fm(Directory d, String rest) => '${IoScriptFileStore.fileMakerPath(d.path)}$rest';

  test('a path in a script folder is accepted, a link leading out of it is not', () {
    final store = IoScriptFileStore(temporary: temporary, documents: documents);
    expect(store.resolve(fm(documents, 'Invoice.pdf')), isNotNull);
    expect(store.resolve(fm(temporary, 'sub/new.pdf')), isNotNull, reason: 'a folder not made yet');

    Link('${documents.path}/escape').createSync(outside.path);
    expect(store.resolve(fm(documents, 'escape/new.pdf')), isNull, reason: 'a new file behind a link to a folder outside');
    File('${outside.path}/secret.txt').writeAsStringSync('x');
    Link('${temporary.path}/file-link').createSync('${outside.path}/secret.txt');
    expect(store.resolve(fm(temporary, 'file-link')), isNull, reason: 'a link to a file outside');
    Link('${temporary.path}/dangling').createSync('${base.path}/nowhere');
    expect(store.resolve(fm(temporary, 'dangling')), isNull, reason: 'a dangling link');

    Directory('${documents.path}/real').createSync();
    Link('${documents.path}/inside').createSync('${documents.path}/real');
    expect(store.resolve(fm(documents, 'inside/a.pdf')), isNotNull, reason: 'a link that stays inside');
  }, skip: Platform.isWindows ? 'symbolic links need privileges on Windows' : false);

  test('clear deletes the temporary folder of the session', () async {
    final store = IoScriptFileStore(temporary: temporary, documents: documents);
    await store.write(fm(temporary, 'a.pdf'), Uint8List.fromList([1]));
    expect(File('${temporary.path}/a.pdf').existsSync(), isTrue);
    await store.clear();
    expect(temporary.existsSync(), isFalse);
    expect(store.temporaryPath, isNot(IoScriptFileStore.fileMakerPath(temporary.path)), reason: 'a new folder for the next session');
    await store.clear();
  });
}
