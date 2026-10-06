import 'dart:convert';
import 'dart:typed_data';
import 'package:msgpack_dart/msgpack_dart.dart' as msgpack;
import 'file_options_model.dart';
import 'page_setup_model.dart';

/// Solution files (.f4p) follow docs/specs/solution_bundle_format.md, the
/// contract shared with the Go server. The server writes them (File > Save);
/// the client reads them and writes the initial file of a new database.
const kSolutionFormat = 'file4base_solution';
const kSolutionVersion = '2.0';

/// The database a solution belongs to. It never carries a password: files are
/// portable and anyone holding one could read it.
class DatabaseConnectionConfig {
  final String engine;
  final String host;
  final int port;
  final String database;

  /// Account that saved the file, used to prefill sign-in.
  final String user;
  final String sslMode;

  const DatabaseConnectionConfig({
    this.engine = 'postgres',
    this.host = 'localhost',
    this.port = 5432,
    required this.database,
    this.user = '',
    this.sslMode = 'disable',
  });

  Map<String, dynamic> toMap() => {
        'engine': engine,
        'host': host,
        'port': port,
        'database': database,
        'user': user,
        'ssl_mode': sslMode,
      };

  /// Reads the connection of a file. A password in an old (1.0) file is
  /// ignored.
  factory DatabaseConnectionConfig.fromMap(Map<dynamic, dynamic> map) {
    return DatabaseConnectionConfig(
      engine: map['engine']?.toString() ?? 'postgres',
      host: map['host']?.toString() ?? 'localhost',
      port: (map['port'] is num) ? (map['port'] as num).toInt() : 5432,
      database: map['database']?.toString() ?? '',
      user: _legacyDecode(map['user']?.toString() ?? ''),
      sslMode: map['ssl_mode']?.toString() ?? 'disable',
    );
  }

  /// 1.0 files obfuscated the username with a fixed key ("enc:" prefix).
  static String _legacyDecode(String value) {
    if (!value.startsWith('enc:')) return value;
    try {
      final raw = base64.decode(value.substring(4));
      final key = utf8.encode('file4base_secure_credentials_key_v1');
      return utf8.decode([for (var i = 0; i < raw.length; i++) raw[i] ^ key[i % key.length]]);
    } catch (_) {
      return '';
    }
  }
}

/// A solution file: the design of a database (tables, occurrences,
/// relationships, layouts, scripts, accounts without passwords) plus the
/// client settings (File Options, Page Setup).
class SolutionPackage {
  final String format;
  final String version;
  final String solutionName;
  final DatabaseConnectionConfig databaseConnection;
  final List<Map<String, dynamic>> tables;
  final List<Map<String, dynamic>> tableOccurrences;
  final List<Map<String, dynamic>> relationships;
  final List<Map<String, dynamic>> layouts;
  final List<Map<String, dynamic>> scripts;
  final List<Map<String, dynamic>> users;
  final FileOptionsModel fileOptions;
  final PageSetupModel pageSetup;
  final String exportedAt;

  SolutionPackage({
    this.format = kSolutionFormat,
    this.version = kSolutionVersion,
    required this.solutionName,
    required this.databaseConnection,
    this.tables = const [],
    this.tableOccurrences = const [],
    this.relationships = const [],
    this.layouts = const [],
    this.scripts = const [],
    this.users = const [],
    this.fileOptions = const FileOptionsModel(),
    this.pageSetup = const PageSetupModel(),
    String? exportedAt,
  }) : exportedAt = exportedAt ?? DateTime.now().toUtc().toIso8601String();

  Map<String, dynamic> toMap() {
    return {
      'format': format,
      'version': version,
      'solution_name': solutionName,
      'exported_at': exportedAt,
      'database_connection': databaseConnection.toMap(),
      'tables': tables,
      'table_occurrences': tableOccurrences,
      'relationships': relationships,
      'layouts': layouts,
      'scripts': scripts,
      'users': users,
      'file_options': fileOptions.toJson(),
      'page_setup': pageSetup.toJson(),
    };
  }

  factory SolutionPackage.fromMap(Map<dynamic, dynamic> map) {
    final format = map['format']?.toString() ?? '';
    if (format != kSolutionFormat) {
      throw FormatException('Not a File4Base solution file (format "$format")');
    }
    final rawDb = map['database_connection'];
    List<Map<String, dynamic>> parseList(dynamic raw) {
      if (raw is List) {
        return raw.whereType<Map>().map((item) => _stringKeyed(item)).toList();
      }
      return [];
    }

    return SolutionPackage(
      format: format,
      version: map['version']?.toString() ?? '1.0',
      solutionName: map['solution_name']?.toString() ?? 'Untitled Solution',
      databaseConnection:
          rawDb is Map ? DatabaseConnectionConfig.fromMap(rawDb) : const DatabaseConnectionConfig(database: ''),
      tables: parseList(map['tables']),
      tableOccurrences: parseList(map['table_occurrences']),
      relationships: parseList(map['relationships']),
      layouts: parseList(map['layouts']),
      scripts: parseList(map['scripts']),
      users: parseList(map['users']),
      fileOptions: map['file_options'] is Map
          ? FileOptionsModel.fromJson(_stringKeyed(map['file_options'] as Map))
          : const FileOptionsModel(),
      pageSetup: map['page_setup'] is Map
          ? PageSetupModel.fromJson(_stringKeyed(map['page_setup'] as Map))
          : const PageSetupModel(),
      exportedAt: (map['exported_at'] ?? map['updated_at'] ?? map['created_at'])?.toString(),
    );
  }

  /// Nested maps decoded from MessagePack have dynamic keys.
  static Map<String, dynamic> _stringKeyed(Map<dynamic, dynamic> m) => {
        for (final e in m.entries) e.key.toString(): _normalize(e.value),
      };

  static dynamic _normalize(dynamic v) {
    if (v is Map) return _stringKeyed(v);
    if (v is List) return v.map(_normalize).toList();
    return v;
  }

  Uint8List toMsgPack() => msgpack.serialize(toMap());

  static SolutionPackage fromMsgPack(Uint8List bytes) {
    final decoded = msgpack.deserialize(bytes);
    if (decoded is Map) {
      return SolutionPackage.fromMap(decoded);
    }
    throw const FormatException('Invalid MessagePack solution payload');
  }

  /// The initial file of a new database: no tables yet, and its first owner
  /// account (enabled, without a password; the server never restores one).
  factory SolutionPackage.newDatabase({
    required String solutionName,
    required DatabaseConnectionConfig connection,
    required String ownerUsername,
  }) {
    return SolutionPackage(
      solutionName: solutionName,
      databaseConnection: connection,
      users: [
        {'id': 'owner', 'username': ownerUsername, 'role': 'owner', 'is_active': true, 'permissions': <dynamic>[]},
      ],
    );
  }
}
