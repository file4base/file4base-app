import 'dart:convert';
import 'dart:typed_data';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/services/solution_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  group('PayloadCipher (AES-256-GCM)', () {
    test('encrypts and decrypts binary and JSON payloads with F4BE\\x01 envelope', () {
      final cipher = PayloadCipher(secret: 'test-channel-secret-2026');
      expect(cipher.keyFingerprint.length, 16);

      final plaintext = Uint8List.fromList(utf8.encode('{"solution_name":"CRM","tables":["contacts"]}'));
      final sealed = cipher.encrypt(plaintext);

      expect(PayloadCipher.isEncryptedEnvelope(sealed), isTrue);
      // Magic header "F4BE\x01"
      expect(sealed.sublist(0, 5), equals([0x46, 0x34, 0x42, 0x45, 0x01]));

      final opened = cipher.decrypt(sealed);
      expect(utf8.decode(opened), equals('{"solution_name":"CRM","tables":["contacts"]}'));

      // Base64-encoded envelope also decrypts cleanly
      final b64Bytes = Uint8List.fromList(utf8.encode(base64.encode(sealed)));
      final openedB64 = cipher.decrypt(b64Bytes);
      expect(openedB64, equals(plaintext));
    });

    test('rejects tampered ciphertext or wrong channel secret', () {
      final cipherA = PayloadCipher(secret: 'secret-a');
      final cipherB = PayloadCipher(secret: 'secret-b');

      final sealed = cipherA.encrypt(Uint8List.fromList([1, 2, 3, 4, 5, 6, 7, 8]));
      expect(() => cipherB.decrypt(sealed), throwsFormatException);

      final tampered = Uint8List.fromList(sealed);
      tampered[tampered.length - 1] ^= 0x01;
      expect(() => cipherA.decrypt(tampered), throwsFormatException);
    });
  });

  group('ApiClient encrypted transport & table permissions', () {
    test('transparently decrypts AES-256-GCM API responses and encrypts requests', () async {
      final serverCipher = PayloadCipher(secret: 'shared-api-secret');
      Uint8List? receivedWireBody;
      String? receivedEncHeader;

      final mockHttp = MockClient((request) async {
        receivedEncHeader = request.headers[PayloadCipher.headerEncrypted];
        receivedWireBody = request.bodyBytes;
        final plainReq = serverCipher.decrypt(request.bodyBytes);
        final decodedReq = jsonDecode(utf8.decode(plainReq)) as Map<String, dynamic>;
        expect(decodedReq['solution_name'], equals('Inventory'));

        final respPayload = Uint8List.fromList([0x82, 0xa6, 0x66, 0x6f, 0x72, 0x6d, 0x61, 0x74]);
        final sealedResp = serverCipher.encrypt(respPayload);
        return http.Response.bytes(
          sealedResp,
          200,
          headers: {
            PayloadCipher.headerEncrypted: PayloadCipher.cipherName,
            PayloadCipher.headerOriginalContentType: 'application/x-msgpack',
            'content-type': 'application/octet-stream',
          },
        );
      });

      final api = ApiClient(
        baseUrl: 'http://127.0.0.1:8080',
        httpClient: mockHttp,
        channelSecret: 'shared-api-secret',
        encryptRequests: true,
      );

      final exported = await api.exportSolution(solutionName: 'Inventory');
      expect(receivedEncHeader, equals('aes-256-gcm'));
      expect(receivedWireBody, isNotNull);
      expect(PayloadCipher.isEncryptedEnvelope(receivedWireBody!), isTrue);
      expect(exported, equals([0x82, 0xa6, 0x66, 0x6f, 0x72, 0x6d, 0x61, 0x74]));
    });

    test('getUserTablePermissions and setUserTablePermissions serialize granular flags', () async {
      Map<String, dynamic>? sentBody;
      final mockHttp = MockClient((request) async {
        if (request.method == 'GET') {
          return http.Response(
            jsonEncode([
              {
                'id': 'tp-1',
                'user_id': 'u-1',
                'table_name': 'invoices',
                'access_level': 'read_write',
                'can_create': true,
                'can_edit': true,
                'can_delete': false,
                'can_modify_schema': false,
              }
            ]),
            200,
          );
        }
        sentBody = jsonDecode(request.body) as Map<String, dynamic>;
        return http.Response(jsonEncode({'status': 'updated'}), 200);
      });

      final api = ApiClient(httpClient: mockHttp);
      final perms = await api.getUserTablePermissions('u-1', database: 'crm_db');
      expect(perms.length, 1);
      expect(perms.first.tableName, 'invoices');
      expect(perms.first.canCreate, isTrue);
      expect(perms.first.canDelete, isFalse);

      await api.setUserTablePermissions('u-1', perms, database: 'crm_db');
      expect(sentBody?['database'], 'crm_db');
      final list = sentBody?['permissions'] as List<dynamic>;
      expect((list.first as Map)['table_name'], 'invoices');
      expect((list.first as Map)['can_delete'], isFalse);
    });
  });

  group('SolutionStorageService persistent default directory', () {
    test('remembers defaultDirectory once set', () {
      SolutionStorageService.setDefaultDirectory(null);
      expect(SolutionStorageService.defaultDirectory, isNull);

      const dir = StorageDirectoryRef(
        displayName: 'Exports',
        handleOrPath: '/Users/test/Exports',
      );
      SolutionStorageService.setDefaultDirectory(dir);
      expect(SolutionStorageService.defaultDirectory?.handleOrPath, equals('/Users/test/Exports'));
      SolutionStorageService.setDefaultDirectory(null);
    });
  });
}
