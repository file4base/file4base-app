// #99 — a design file is read in the encoding it was written in, not guessed
// at as UTF-8 and handed to the server as replacement characters.

import 'dart:convert';

import 'package:file4base_client/core/utils/xml_encoding.dart';
import 'package:flutter_test/flutter_test.dart';

const _designXml =
    '<?xml version="1.0" encoding="UTF-16"?><FMPReport><File name="Rapport.fmp12">'
    '<BaseTableCatalog><BaseTable name="Visités"><FieldCatalog>'
    '<Field name="Coût" dataType="Number"/></FieldCatalog></BaseTable>'
    '</BaseTableCatalog></FMPReport>';

List<int> _utf16(String text, {required bool littleEndian, bool bom = true}) {
  final bytes = <int>[];
  if (bom) {
    bytes.addAll(littleEndian ? [0xFF, 0xFE] : [0xFE, 0xFF]);
  }
  for (final unit in text.codeUnits) {
    if (littleEndian) {
      bytes.addAll([unit & 0xFF, unit >> 8]);
    } else {
      bytes.addAll([unit >> 8, unit & 0xFF]);
    }
  }
  return bytes;
}

void main() {
  test('UTF-16LE with a byte order mark reads as the document it is', () {
    expect(decodeXmlBytes(_utf16(_designXml, littleEndian: true)), _designXml);
  });

  test('UTF-16BE with a byte order mark reads too', () {
    expect(decodeXmlBytes(_utf16(_designXml, littleEndian: false)), _designXml);
  });

  test('UTF-16 without a byte order mark is recognized by its NUL bytes', () {
    expect(decodeXmlBytes(_utf16(_designXml, littleEndian: true, bom: false)), _designXml);
    expect(decodeXmlBytes(_utf16(_designXml, littleEndian: false, bom: false)), _designXml);
  });

  test('characters outside the basic plane survive UTF-16', () {
    const withEmoji = '<FMPReport><File name="Ωmega 🗂"/></FMPReport>';
    expect(decodeXmlBytes(_utf16(withEmoji, littleEndian: true)), withEmoji);
  });

  test('plain UTF-8 and UTF-8 with a byte order mark read unchanged', () {
    expect(decodeXmlBytes(utf8.encode(_designXml)), _designXml);
    expect(decodeXmlBytes([0xEF, 0xBB, 0xBF, ...utf8.encode(_designXml)]), _designXml);
  });

  test('a declared Latin-1 report reads as Latin-1', () {
    const latin =
        '<?xml version="1.0" encoding="ISO-8859-1"?><FMPReport><File name="Coût"/></FMPReport>';
    expect(decodeXmlBytes(latin1.encode(latin)), latin);
  });

  test('bytes that are neither UTF-8 nor a declared encoding are refused, not mangled', () {
    final broken = [0x3C, 0x3F, 0x78, 0x6D, 0x6C, 0x3F, 0x3E, 0xC3, 0x28, 0xA0, 0xFF, 0xFE, 0x41];
    expect(
      () => decodeXmlBytes(broken),
      throwsA(isA<XmlEncodingException>().having(
        (e) => e.message,
        'message',
        allOf(contains('UTF-8'), contains('Re-save')),
      )),
    );
  });

  test('UTF-32 is refused with what to do about it', () {
    final utf32 = <int>[0xFF, 0xFE, 0x00, 0x00];
    for (final unit in '<FMPReport/>'.codeUnits) {
      utf32.addAll([unit & 0xFF, unit >> 8, 0x00, 0x00]);
    }
    expect(
      () => decodeXmlBytes(utf32),
      throwsA(isA<XmlEncodingException>().having(
        (e) => e.message,
        'message',
        contains('UTF-32'),
      )),
    );
  });

  test('an empty file is empty text, not an error', () {
    expect(decodeXmlBytes(const []), '');
  });
}
