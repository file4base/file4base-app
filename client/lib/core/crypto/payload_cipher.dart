import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

/// Authenticated AES-256-GCM payload envelope cipher shared between the
/// File4Base Client (Web/Desktop) and `file4base-api` (`server/internal/crypto`).
///
/// Format:
///   [5 bytes magic: "F4BE\x01"] + [12 bytes random GCM nonce] + [ciphertext] + [16 bytes GCM tag]
/// Associated Data (AAD):
///   "F4BE\x01"
class PayloadCipher {

  /// Creates an AES-256-GCM cipher from a channel secret string.
  factory PayloadCipher({String secret = ''}) {
    final normalized = secret.trim().isEmpty ? defaultChannelSecret : secret.trim();
    final key = sha256(Uint8List.fromList(utf8.encode('file4base-channel-v1:$normalized')));
    final roundKeys = _expandKey256(key);
    final zeroBlock = Uint8List(16);
    final h = _aesEncryptBlock(zeroBlock, roundKeys);
    return PayloadCipher._(key, roundKeys, h);
  }

  PayloadCipher._(this._key, this._roundKeys, this._h);
  static const String headerEncrypted = 'X-File4Base-Encrypted';
  static const String headerAcceptEncryption = 'Accept-Encryption';
  static const String headerOriginalContentType = 'X-Original-Content-Type';
  static const String cipherName = 'aes-256-gcm';
  static const String defaultChannelSecret = 'file4base-default-channel-key-2026';

  static final Uint8List _envelopeMagic = Uint8List.fromList([0x46, 0x34, 0x42, 0x45, 0x01]);
  static const int _nonceSize = 12;
  static const int _tagSize = 16;

  final Uint8List _key;
  final Uint8List _roundKeys;
  final Uint8List _h;

  /// Non-secret 16-hex-character key fingerprint matching the server's
  /// `/api/v1/crypto/handshake` `key_fingerprint`.
  String get keyFingerprint {
    final digest = sha256(_key);
    final sb = StringBuffer();
    for (var i = 0; i < 8; i++) {
      sb.write(digest[i].toRadixString(16).padLeft(2, '0'));
    }
    return sb.toString();
  }

  /// Returns true if [payload] starts with the `F4BE\x01` envelope header.
  static bool isEncryptedEnvelope(Uint8List payload) {
    if (payload.length < _envelopeMagic.length + _nonceSize + _tagSize) {
      return false;
    }
    for (var i = 0; i < _envelopeMagic.length; i++) {
      if (payload[i] != _envelopeMagic[i]) return false;
    }
    return true;
  }

  /// Seals [plaintext] into an `F4BE\x01` AES-256-GCM envelope.
  Uint8List encrypt(Uint8List plaintext, {Uint8List? nonce}) {
    final iv = nonce ?? _randomNonce();
    if (iv.length != _nonceSize) {
      throw ArgumentError('GCM nonce must be 12 bytes');
    }

    // J0 = iv || 0x00000001
    final j0 = Uint8List(16);
    j0.setRange(0, 12, iv);
    j0[15] = 1;

    // Encrypt plaintext using GCTR starting at inc32(J0)
    final ciphertext = _gctr(_inc32(j0), plaintext);

    // Compute GHASH over AAD ("F4BE\x01") and ciphertext
    final tagMask = _aesEncryptBlock(j0, _roundKeys);
    final s = _ghash(_envelopeMagic, ciphertext);
    final tag = Uint8List(16);
    for (var i = 0; i < 16; i++) {
      tag[i] = s[i] ^ tagMask[i];
    }

    final out = Uint8List(_envelopeMagic.length + _nonceSize + ciphertext.length + _tagSize);
    var offset = 0;
    out.setRange(offset, offset + _envelopeMagic.length, _envelopeMagic);
    offset += _envelopeMagic.length;
    out.setRange(offset, offset + _nonceSize, iv);
    offset += _nonceSize;
    out.setRange(offset, offset + ciphertext.length, ciphertext);
    offset += ciphertext.length;
    out.setRange(offset, offset + _tagSize, tag);
    return out;
  }

  /// Opens an `F4BE\x01` AES-256-GCM envelope (raw binary or base64-encoded)
  /// and verifies its authentication tag in constant time.
  Uint8List decrypt(Uint8List payload) {
    var raw = payload;
    if (!isEncryptedEnvelope(raw)) {
      try {
        final decoded = base64.decode(utf8.decode(payload).trim());
        raw = Uint8List.fromList(decoded);
      } catch (_) {
        throw const FormatException('Invalid or unauthenticated encrypted payload');
      }
      if (!isEncryptedEnvelope(raw)) {
        throw const FormatException('Invalid or unauthenticated encrypted payload');
      }
    }

    final ivStart = _envelopeMagic.length;
    final ctStart = ivStart + _nonceSize;
    final tagStart = raw.length - _tagSize;

    final iv = Uint8List.sublistView(raw, ivStart, ctStart);
    final ciphertext = Uint8List.sublistView(raw, ctStart, tagStart);
    final receivedTag = Uint8List.sublistView(raw, tagStart);

    final j0 = Uint8List(16);
    j0.setRange(0, 12, iv);
    j0[15] = 1;

    final tagMask = _aesEncryptBlock(j0, _roundKeys);
    final s = _ghash(_envelopeMagic, ciphertext);
    var diff = 0;
    for (var i = 0; i < 16; i++) {
      final expected = s[i] ^ tagMask[i];
      diff |= expected ^ receivedTag[i];
    }
    if (diff != 0) {
      throw const FormatException('Invalid or unauthenticated encrypted payload');
    }

    return _gctr(_inc32(j0), ciphertext);
  }

  static Uint8List _randomNonce() {
    final rng = Random.secure();
    final out = Uint8List(_nonceSize);
    for (var i = 0; i < _nonceSize; i++) {
      out[i] = rng.nextInt(256);
    }
    return out;
  }

  static Uint8List _inc32(Uint8List counterBlock) {
    final out = Uint8List.fromList(counterBlock);
    var c = ((out[12] << 24) | (out[13] << 16) | (out[14] << 8) | out[15]) & 0xffffffff;
    c = (c + 1) & 0xffffffff;
    out[12] = (c >> 24) & 0xff;
    out[13] = (c >> 16) & 0xff;
    out[14] = (c >> 8) & 0xff;
    out[15] = c & 0xff;
    return out;
  }

  Uint8List _gctr(Uint8List icb, Uint8List input) {
    if (input.isEmpty) return Uint8List(0);
    final out = Uint8List(input.length);
    var cb = Uint8List.fromList(icb);
    for (var offset = 0; offset < input.length; offset += 16) {
      final keystream = _aesEncryptBlock(cb, _roundKeys);
      final chunk = min(16, input.length - offset);
      for (var i = 0; i < chunk; i++) {
        out[offset + i] = input[offset + i] ^ keystream[i];
      }
      cb = _inc32(cb);
    }
    return out;
  }

  Uint8List _ghash(Uint8List aad, Uint8List ciphertext) {
    var y = Uint8List(16);

    void processBytes(Uint8List data) {
      for (var offset = 0; offset < data.length; offset += 16) {
        final chunk = min(16, data.length - offset);
        for (var i = 0; i < chunk; i++) {
          y[i] ^= data[offset + i];
        }
        y = _gfMul(y, _h);
      }
    }

    processBytes(aad);
    processBytes(ciphertext);

    final lenBlock = ByteData(16);
    final aadBits = aad.length * 8;
    final ctBits = ciphertext.length * 8;
    lenBlock.setUint32(0, (aadBits ~/ 0x100000000) & 0xffffffff);
    lenBlock.setUint32(4, aadBits & 0xffffffff);
    lenBlock.setUint32(8, (ctBits ~/ 0x100000000) & 0xffffffff);
    lenBlock.setUint32(12, ctBits & 0xffffffff);

    final lenBytes = lenBlock.buffer.asUint8List();
    for (var i = 0; i < 16; i++) {
      y[i] ^= lenBytes[i];
    }
    return _gfMul(y, _h);
  }

  /// Multiplication in GF(2^128) with reduction polynomial x^128 + x^7 + x^2 + x + 1 (0xe1).
  static Uint8List _gfMul(Uint8List x, Uint8List y) {
    final z = Uint8List(16);
    final v = Uint8List.fromList(y);

    for (var i = 0; i < 128; i++) {
      final byteIdx = i >> 3;
      final bitIdx = 7 - (i & 7);
      if (((x[byteIdx] >> bitIdx) & 1) != 0) {
        for (var j = 0; j < 16; j++) {
          z[j] ^= v[j];
        }
      }
      final lsb = v[15] & 1;
      for (var j = 15; j > 0; j--) {
        v[j] = ((v[j] >> 1) | ((v[j - 1] & 1) << 7)) & 0xff;
      }
      v[0] = (v[0] >> 1) & 0xff;
      if (lsb != 0) {
        v[0] ^= 0xe1;
      }
    }
    return z;
  }

  // ─── FIPS 180-4 SHA-256 ────────────────────────────────────────────────────

  static const List<int> _sha256K = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
  ];

  static int _rotr(int x, int n) => ((x & 0xffffffff) >>> n) | ((x << (32 - n)) & 0xffffffff);

  static Uint8List sha256(Uint8List data) {
    var h0 = 0x6a09e667;
    var h1 = 0xbb67ae85;
    var h2 = 0x3c6ef372;
    var h3 = 0xa54ff53a;
    var h4 = 0x510e527f;
    var h5 = 0x9b05688c;
    var h6 = 0x1f83d9ab;
    var h7 = 0x5be0cd19;

    final bitLen = data.length * 8;
    final padLen = ((data.length + 9 + 63) ~/ 64) * 64;
    final padded = Uint8List(padLen);
    padded.setRange(0, data.length, data);
    padded[data.length] = 0x80;
    final bd = ByteData.sublistView(padded);
    bd.setUint32(padLen - 8, (bitLen ~/ 0x100000000) & 0xffffffff);
    bd.setUint32(padLen - 4, bitLen & 0xffffffff);

    final w = Uint32List(64);
    for (var chunkOffset = 0; chunkOffset < padLen; chunkOffset += 64) {
      for (var i = 0; i < 16; i++) {
        w[i] = bd.getUint32(chunkOffset + i * 4);
      }
      for (var i = 16; i < 64; i++) {
        final s0 = _rotr(w[i - 15], 7) ^ _rotr(w[i - 15], 18) ^ ((w[i - 15] & 0xffffffff) >>> 3);
        final s1 = _rotr(w[i - 2], 17) ^ _rotr(w[i - 2], 19) ^ ((w[i - 2] & 0xffffffff) >>> 10);
        w[i] = (w[i - 16] + s0 + w[i - 7] + s1) & 0xffffffff;
      }

      var a = h0;
      var b = h1;
      var c = h2;
      var d = h3;
      var e = h4;
      var f = h5;
      var g = h6;
      var h = h7;
      for (var i = 0; i < 64; i++) {
        final s1 = _rotr(e, 6) ^ _rotr(e, 11) ^ _rotr(e, 25);
        final ch = (e & f) ^ ((~e & 0xffffffff) & g);
        final temp1 = (h + s1 + ch + _sha256K[i] + w[i]) & 0xffffffff;
        final s0 = _rotr(a, 2) ^ _rotr(a, 13) ^ _rotr(a, 22);
        final maj = (a & b) ^ (a & c) ^ (b & c);
        final temp2 = (s0 + maj) & 0xffffffff;

        h = g;
        g = f;
        f = e;
        e = (d + temp1) & 0xffffffff;
        d = c;
        c = b;
        b = a;
        a = (temp1 + temp2) & 0xffffffff;
      }

      h0 = (h0 + a) & 0xffffffff;
      h1 = (h1 + b) & 0xffffffff;
      h2 = (h2 + c) & 0xffffffff;
      h3 = (h3 + d) & 0xffffffff;
      h4 = (h4 + e) & 0xffffffff;
      h5 = (h5 + f) & 0xffffffff;
      h6 = (h6 + g) & 0xffffffff;
      h7 = (h7 + h) & 0xffffffff;
    }

    final out = ByteData(32);
    out.setUint32(0, h0);
    out.setUint32(4, h1);
    out.setUint32(8, h2);
    out.setUint32(12, h3);
    out.setUint32(16, h4);
    out.setUint32(20, h5);
    out.setUint32(24, h6);
    out.setUint32(28, h7);
    return out.buffer.asUint8List();
  }

  // ─── FIPS 197 AES-256 Block Cipher ─────────────────────────────────────────

  static const List<int> _sbox = [
    0x63, 0x7c, 0x77, 0x7b, 0xf2, 0x6b, 0x6f, 0xc5, 0x30, 0x01, 0x67, 0x2b, 0xfe, 0xd7, 0xab, 0x76,
    0xca, 0x82, 0xc9, 0x7d, 0xfa, 0x59, 0x47, 0xf0, 0xad, 0xd4, 0xa2, 0xaf, 0x9c, 0xa4, 0x72, 0xc0,
    0xb7, 0xfd, 0x93, 0x26, 0x36, 0x3f, 0xf7, 0xcc, 0x34, 0xa5, 0xe5, 0xf1, 0x71, 0xd8, 0x31, 0x15,
    0x04, 0xc7, 0x23, 0xc3, 0x18, 0x96, 0x05, 0x9a, 0x07, 0x12, 0x80, 0xe2, 0xeb, 0x27, 0xb2, 0x75,
    0x09, 0x83, 0x2c, 0x1a, 0x1b, 0x6e, 0x5a, 0xa0, 0x52, 0x3b, 0xd6, 0xb3, 0x29, 0xe3, 0x2f, 0x84,
    0x53, 0xd1, 0x00, 0xed, 0x20, 0xfc, 0xb1, 0x5b, 0x6a, 0xcb, 0xbe, 0x39, 0x4a, 0x4c, 0x58, 0xcf,
    0xd0, 0xef, 0xaa, 0xfb, 0x43, 0x4d, 0x33, 0x85, 0x45, 0xf9, 0x02, 0x7f, 0x50, 0x3c, 0x9f, 0xa8,
    0x51, 0xa3, 0x40, 0x8f, 0x92, 0x9d, 0x38, 0xf5, 0xbc, 0xb6, 0xda, 0x21, 0x10, 0xff, 0xf3, 0xd2,
    0xcd, 0x0c, 0x13, 0xec, 0x5f, 0x97, 0x44, 0x17, 0xc4, 0xa7, 0x7e, 0x3d, 0x64, 0x5d, 0x19, 0x73,
    0x60, 0x81, 0x4f, 0xdc, 0x22, 0x2a, 0x90, 0x88, 0x46, 0xee, 0xb8, 0x14, 0xde, 0x5e, 0x0b, 0xdb,
    0xe0, 0x32, 0x3a, 0x0a, 0x49, 0x06, 0x24, 0x5c, 0xc2, 0xd3, 0xac, 0x62, 0x91, 0x95, 0xe4, 0x79,
    0xe7, 0xc8, 0x37, 0x6d, 0x8d, 0xd5, 0x4e, 0xa9, 0x6c, 0x56, 0xf4, 0xea, 0x65, 0x7a, 0xae, 0x08,
    0xba, 0x78, 0x25, 0x2e, 0x1c, 0xa6, 0xb4, 0xc6, 0xe8, 0xdd, 0x74, 0x1f, 0x4b, 0xbd, 0x8b, 0x8a,
    0x70, 0x3e, 0xb5, 0x66, 0x48, 0x03, 0xf6, 0x0e, 0x61, 0x35, 0x57, 0xb9, 0x86, 0xc1, 0x1d, 0x9e,
    0xe1, 0xf8, 0x98, 0x11, 0x69, 0xd9, 0x8e, 0x94, 0x9b, 0x1e, 0x87, 0xe9, 0xce, 0x55, 0x28, 0xdf,
    0x8c, 0xa1, 0x89, 0x0d, 0xbf, 0xe6, 0x42, 0x68, 0x41, 0x99, 0x2d, 0x0f, 0xb0, 0x54, 0xbb, 0x16,
  ];

  static const List<int> _rcon = [
    0x00, 0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40,
  ];

  static Uint8List _expandKey256(Uint8List key) {
    final w = Uint8List(240); // 15 round keys * 16 bytes
    w.setRange(0, 32, key);

    final temp = Uint8List(4);
    var bytesGenerated = 32;
    var rconIteration = 1;

    while (bytesGenerated < 240) {
      for (var i = 0; i < 4; i++) {
        temp[i] = w[bytesGenerated - 4 + i];
      }

      if (bytesGenerated % 32 == 0) {
        final t = temp[0];
        temp[0] = _sbox[temp[1]] ^ _rcon[rconIteration];
        temp[1] = _sbox[temp[2]];
        temp[2] = _sbox[temp[3]];
        temp[3] = _sbox[t];
        rconIteration++;
      } else if (bytesGenerated % 32 == 16) {
        for (var i = 0; i < 4; i++) {
          temp[i] = _sbox[temp[i]];
        }
      }

      for (var i = 0; i < 4; i++) {
        w[bytesGenerated] = w[bytesGenerated - 32] ^ temp[i];
        bytesGenerated++;
      }
    }
    return w;
  }

  static int _xtime(int x) => ((x << 1) ^ (((x >> 7) & 1) * 0x1b)) & 0xff;

  static Uint8List _aesEncryptBlock(Uint8List input, Uint8List roundKeys) {
    final s = Uint8List.fromList(input);

    // AddRoundKey(0)
    for (var i = 0; i < 16; i++) {
      s[i] ^= roundKeys[i];
    }

    final tmp = Uint8List(16);
    for (var round = 1; round <= 14; round++) {
      // SubBytes
      for (var i = 0; i < 16; i++) {
        s[i] = _sbox[s[i]];
      }

      // ShiftRows
      tmp.setAll(0, s);
      s[1] = tmp[5];
      s[5] = tmp[9];
      s[9] = tmp[13];
      s[13] = tmp[1];

      s[2] = tmp[10];
      s[6] = tmp[14];
      s[10] = tmp[2];
      s[14] = tmp[6];

      s[3] = tmp[15];
      s[7] = tmp[3];
      s[11] = tmp[7];
      s[15] = tmp[11];

      // MixColumns (skip on final round 14)
      if (round < 14) {
        for (var c = 0; c < 16; c += 4) {
          final a0 = s[c];
          final a1 = s[c + 1];
          final a2 = s[c + 2];
          final a3 = s[c + 3];
          final t = a0 ^ a1 ^ a2 ^ a3;
          s[c] ^= t ^ _xtime(a0 ^ a1);
          s[c + 1] ^= t ^ _xtime(a1 ^ a2);
          s[c + 2] ^= t ^ _xtime(a2 ^ a3);
          s[c + 3] ^= t ^ _xtime(a3 ^ a0);
        }
      }

      // AddRoundKey(round)
      final rkOffset = round * 16;
      for (var i = 0; i < 16; i++) {
        s[i] ^= roundKeys[rkOffset + i];
      }
    }

    return s;
  }
}
