// #99 — reading a FileMaker design file whatever it was written in.
//
// FileMaker Pro on Windows writes its XML Database Design Report as UTF-16
// with a byte order mark. Decoding those bytes as UTF-8 with malformed bytes
// allowed does not fail: it produces replacement characters with the file's
// NUL bytes interleaved between them, which is neither the document nor an
// error, so the server rejected it as invalid FileMaker XML and the user was
// told their perfectly good export was not a design report.

import 'dart:convert';

/// Why a design or record file could not be read as text.
class XmlEncodingException implements Exception {
  const XmlEncodingException(this.message);
  final String message;

  @override
  String toString() => message;
}

/// Decodes the bytes of an XML or HTML file into text.
///
/// The byte order mark decides first, then a look at the bytes themselves
/// (UTF-16 text is full of NULs where UTF-8 has none), then the `encoding` of
/// the XML declaration. An encoding File4Base cannot read is an
/// [XmlEncodingException] saying what to do about it — never silently replaced
/// bytes.
String decodeXmlBytes(List<int> bytes) {
  final data = bytes;
  if (data.isEmpty) return '';

  // UTF-32, which no FileMaker export uses and which would otherwise be
  // mistaken for UTF-16 by the NUL check below.
  if (data.length >= 4) {
    final b = data;
    final utf32le = b[0] == 0xFF && b[1] == 0xFE && b[2] == 0x00 && b[3] == 0x00;
    final utf32be = b[0] == 0x00 && b[1] == 0x00 && b[2] == 0xFE && b[3] == 0xFF;
    if (utf32le || utf32be) {
      throw const XmlEncodingException(
        'This file is UTF-32, which File4Base cannot read. Re-save the design '
        'report as UTF-8 or UTF-16 and select it again.',
      );
    }
  }

  if (data.length >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF) {
    return _decodeUtf8(data.sublist(3));
  }
  if (data.length >= 2 && data[0] == 0xFF && data[1] == 0xFE) {
    return _decodeUtf16(data.sublist(2), littleEndian: true);
  }
  if (data.length >= 2 && data[0] == 0xFE && data[1] == 0xFF) {
    return _decodeUtf16(data.sublist(2), littleEndian: false);
  }

  // No mark: a UTF-16 document still gives itself away, because every
  // character of its ASCII markup carries a NUL beside it.
  final endian = _sniffUtf16(data);
  if (endian != null) {
    return _decodeUtf16(data, littleEndian: endian);
  }

  try {
    return _decodeUtf8(data);
  } on XmlEncodingException {
    // The bytes are not UTF-8. The declaration is the last thing that can say
    // what they are; it is ASCII-compatible in every encoding handled here.
    final declared = _declaredEncoding(data);
    switch (declared) {
      case 'iso-8859-1':
      case 'latin1':
      case 'latin-1':
      case 'windows-1252':
      case 'cp1252':
        return latin1.decode(data, allowInvalid: true);
      case '':
        throw const XmlEncodingException(
          'This file is not valid UTF-8 and declares no encoding, so File4Base '
          'cannot tell how to read it. Re-save the design report as UTF-8 or '
          'UTF-16 and select it again.',
        );
      default:
        throw XmlEncodingException(
          'This file declares the encoding "$declared", which File4Base cannot '
          'read. Re-save the design report as UTF-8 or UTF-16 and select it '
          'again.',
        );
    }
  }
}

String _decodeUtf8(List<int> data) {
  try {
    return utf8.decode(data);
  } on FormatException {
    throw const XmlEncodingException('not utf-8');
  }
}

/// True for little-endian, false for big-endian, null when the bytes do not
/// look like UTF-16 at all.
bool? _sniffUtf16(List<int> data) {
  final look = data.length < 64 ? data.length : 64;
  if (look < 4) return null;
  var evenNuls = 0;
  var oddNuls = 0;
  for (var i = 0; i < look; i++) {
    if (data[i] != 0) continue;
    if (i.isEven) {
      evenNuls++;
    } else {
      oddNuls++;
    }
  }
  final threshold = look ~/ 8;
  if (oddNuls > threshold && evenNuls == 0) return true; // "<\0?\0x\0m\0l\0"
  if (evenNuls > threshold && oddNuls == 0) return false; // "\0<\0?\0x\0m\0l"
  return null;
}

String _decodeUtf16(List<int> data, {required bool littleEndian}) {
  final units = <int>[];
  for (var i = 0; i + 1 < data.length; i += 2) {
    units.add(littleEndian ? data[i] | (data[i + 1] << 8) : (data[i] << 8) | data[i + 1]);
  }
  // Dart strings are UTF-16, so the code units are the string, surrogate
  // pairs included.
  return String.fromCharCodes(units);
}

/// The `encoding="…"` of an XML declaration, lower-cased, or '' when the file
/// declares none.
String _declaredEncoding(List<int> data) {
  final head = data.length < 256 ? data : data.sublist(0, 256);
  final ascii = String.fromCharCodes(head.map((b) => b < 0x80 ? b : 0x20));
  final match = RegExp(r'''encoding\s*=\s*["']([^"']+)["']''', caseSensitive: false).firstMatch(ascii);
  return match == null ? '' : match.group(1)!.trim().toLowerCase();
}
