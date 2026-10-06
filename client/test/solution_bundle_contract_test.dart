import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:file4base_client/core/models/file_options_model.dart';
import 'package:file4base_client/core/models/solution_models.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:msgpack_dart/msgpack_dart.dart' as msgpack;

/// Shared fixtures of docs/specs/solution_bundle_format.md. The Go tests
/// (server/internal/schema/bundle_golden_test.go) write the server file and
/// read the client files; run both suites with UPDATE_GOLDENS=1 to rewrite them.
const _fixtures = '../server/internal/schema/testdata';
final _update = Platform.environment['UPDATE_GOLDENS'] == '1';

Uint8List _newDatabaseFile() => SolutionPackage.newDatabase(
      solutionName: 'Invoices Pro',
      connection: const DatabaseConnectionConfig(database: 'invoices_db', user: 'alice'),
      ownerUsername: 'alice',
    ).toMsgPackWithTime('2026-10-06T09:00:00.000Z');

/// A file saved by the 1.0 client: obfuscated user and password, a
/// remembered password in File Options and created/updated timestamps.
Uint8List _legacyClientFile() => msgpack.serialize({
      'format': 'file4base_solution',
      'version': '1.0',
      'solution_name': 'Old Solution',
      'database_connection': {
        'engine': 'postgres',
        'host': 'localhost',
        'port': 5432,
        'database': 'old_db',
        'user': _legacyEncode('alice'),
        'password': _legacyEncode('alice-secret'),
        'ssl_mode': 'disable',
      },
      'tables': [
        {
          'id': 't-notes',
          'name': 'notes',
          'display_name': 'Notes',
          'description': '',
          'columns': [
            {'id': 'c-id', 'table_id': 't-notes', 'name': 'id', 'display_name': 'ID', 'field_type': 'TEXT', 'is_nullable': false, 'is_primary_key': true},
            {'id': 'c-body', 'table_id': 't-notes', 'name': 'body', 'display_name': 'Body', 'field_type': 'TEXT', 'is_nullable': true, 'is_primary_key': false},
          ],
        },
      ],
      'table_occurrences': [
        {'id': 'o-notes', 'base_table_id': 't-notes', 'name': 'notes', 'x_pos': 0, 'y_pos': 12.5},
      ],
      'layouts': [
        {
          'id': 'l-notes',
          'name': 'Notes Layout',
          'table_occurrence_id': 'o-notes',
          'definition': {'width': 900, 'objects': []},
        },
      ],
      'users': [
        {'id': 'u1', 'username': 'alice', 'role': 'owner', 'is_active': true, 'permissions': []},
      ],
      'scripts': [],
      'file_options': {'auto_login_enabled': true, 'default_username': 'alice', 'default_password': 'alice-secret'},
      'page_setup': {},
      'created_at': '2026-01-01T00:00:00.000Z',
      'updated_at': '2026-01-01T00:00:00.000Z',
    });

String _legacyEncode(String plain) {
  final key = utf8.encode('file4base_secure_credentials_key_v1');
  final data = utf8.encode(plain);
  return 'enc:${base64.encode([for (var i = 0; i < data.length; i++) data[i] ^ key[i % key.length]])}';
}

extension on SolutionPackage {
  Uint8List toMsgPackWithTime(String exportedAt) => SolutionPackage(
        solutionName: solutionName,
        databaseConnection: databaseConnection,
        users: users,
        exportedAt: exportedAt,
      ).toMsgPack();
}

void _checkFixture(String name, Uint8List current) {
  final file = File('$_fixtures/$name');
  if (_update) file.writeAsBytesSync(current);
  expect(file.existsSync(), isTrue, reason: 'run with UPDATE_GOLDENS=1 to create $name');
  expect(msgpack.deserialize(file.readAsBytesSync()), msgpack.deserialize(current),
      reason: 'the client encoding of $name changed; update the fixture and the Go contract test together');
}

void main() {
  test('reads the solution file written by the server', () {
    final pkg = SolutionPackage.fromMsgPack(File('$_fixtures/server_solution_v2.f4p').readAsBytesSync());
    expect(pkg.version, '2.0');
    expect(pkg.solutionName, 'Invoices Pro');
    expect(pkg.exportedAt, '2026-10-06T09:00:00.123456Z');
    expect(pkg.databaseConnection.database, 'invoices_db');
    expect(pkg.databaseConnection.user, 'alice');
    expect(pkg.tables.single['name'], 'invoices');
    expect((pkg.tables.single['columns'] as List)[1]['default_value'], '{"data_enabled":true,"data_value":"Open"}');
    expect(pkg.tableOccurrences.single['y_pos'], 60.5);
    final layout = pkg.layouts.single;
    final definition = layout['definition'] as Map<String, dynamic>;
    expect(definition['width'], 900);
    final button = (definition['objects'] as List).single as Map<String, dynamic>;
    expect(button['tab_order'], isA<int>(), reason: 'integers stay integers across languages');
    expect(button['y'], 10.5);
    final script = pkg.scripts.single;
    expect((script['steps'] as List)[1]['parent_step_id'], 's-if');
    final bob = pkg.users.single;
    expect(bob['is_active'], false);
    expect((bob['permissions'] as List).single['access_level'], 'read_only');
    expect(pkg.fileOptions.autoLoginEnabled, isTrue);
    expect(pkg.fileOptions.defaultUsername, 'alice');
  });

  test('the initial file of a new database matches the shared fixture and carries no password', () {
    final bytes = _newDatabaseFile();
    _checkFixture('client_new_database_v2.f4p', bytes);
    final map = msgpack.deserialize(bytes) as Map;
    expect(map['version'], '2.0');
    expect(utf8.decode(bytes, allowMalformed: true).toLowerCase(), isNot(contains('password')));
  });

  test('a 1.0 client file is read without exposing its password', () {
    final bytes = _legacyClientFile();
    _checkFixture('client_legacy_v1.f4p', bytes);
    final pkg = SolutionPackage.fromMsgPack(bytes);
    expect(pkg.databaseConnection.user, 'alice', reason: 'the old obfuscated username is still read');
    expect(pkg.fileOptions.defaultPassword, isEmpty, reason: 'a remembered password is never read from a file');
    expect(pkg.exportedAt, '2026-01-01T00:00:00.000Z');
  });

  test('saving never writes a password, even one remembered in File Options', () {
    final pkg = SolutionPackage(
      solutionName: 'X',
      databaseConnection: const DatabaseConnectionConfig(database: 'x_db', user: 'alice'),
      fileOptions: const FileOptionsModel(autoLoginEnabled: true, defaultUsername: 'alice', defaultPassword: 'alice-secret'),
    );
    final text = utf8.decode(pkg.toMsgPack(), allowMalformed: true);
    expect(text, isNot(contains('alice-secret')));
    expect(text.toLowerCase(), isNot(contains('password')));
  });

  test('a file of another kind is rejected', () {
    expect(() => SolutionPackage.fromMsgPack(msgpack.serialize({'format': 'file4base_data'})), throwsFormatException);
  });
}
