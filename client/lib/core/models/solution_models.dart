import 'dart:typed_data';
import 'package:msgpack_dart/msgpack_dart.dart' as msgpack;
import '../api/api_client.dart';

/// DatabaseConnectionConfig represents DB connection settings stored inside the MessagePack .f4b file
class DatabaseConnectionConfig {
  final String engine;
  final String host;
  final int port;
  final String database;
  final String user;
  final String password;
  final String sslMode;

  const DatabaseConnectionConfig({
    this.engine = 'postgres',
    this.host = 'localhost',
    this.port = 5432,
    required this.database,
    this.user = 'file4base',
    this.password = 'dev_password',
    this.sslMode = 'disable',
  });

  Map<String, dynamic> toMap() {
    return {
      'engine': engine,
      'host': host,
      'port': port,
      'database': database,
      'user': user,
      'password': password,
      'ssl_mode': sslMode,
    };
  }

  factory DatabaseConnectionConfig.fromMap(Map<dynamic, dynamic> map) {
    return DatabaseConnectionConfig(
      engine: map['engine']?.toString() ?? 'postgres',
      host: map['host']?.toString() ?? 'localhost',
      port: (map['port'] is num) ? (map['port'] as num).toInt() : 5432,
      database: map['database']?.toString() ?? 'file4base_dev',
      user: map['user']?.toString() ?? 'file4base',
      password: map['password']?.toString() ?? 'dev_password',
      sslMode: map['ssl_mode']?.toString() ?? 'disable',
    );
  }
}

/// SolutionPackage holds all frontend layouts, schemas, table definitions, users,
/// and database connection parameters in a single portable MessagePack bundle (.f4b).
class SolutionPackage {
  final String format;
  final String version;
  final String solutionName;
  final DatabaseConnectionConfig databaseConnection;
  final List<Map<String, dynamic>> tables;
  final List<Map<String, dynamic>> tableOccurrences;
  final List<Map<String, dynamic>> layouts;
  final List<Map<String, dynamic>> users;
  final String createdAt;
  final String updatedAt;

  SolutionPackage({
    this.format = 'file4base_solution',
    this.version = '1.0',
    required this.solutionName,
    required this.databaseConnection,
    this.tables = const [],
    this.tableOccurrences = const [],
    this.layouts = const [],
    this.users = const [],
    String? createdAt,
    String? updatedAt,
  })  : createdAt = createdAt ?? DateTime.now().toUtc().toIso8601String(),
        updatedAt = updatedAt ?? DateTime.now().toUtc().toIso8601String();

  Map<String, dynamic> toMap() {
    return {
      'format': format,
      'version': version,
      'solution_name': solutionName,
      'database_connection': databaseConnection.toMap(),
      'tables': tables,
      'table_occurrences': tableOccurrences,
      'layouts': layouts,
      'users': users,
      'created_at': createdAt,
      'updated_at': updatedAt,
    };
  }

  factory SolutionPackage.fromMap(Map<dynamic, dynamic> map) {
    var rawDb = map['database_connection'];
    var dbConfig = rawDb is Map
        ? DatabaseConnectionConfig.fromMap(rawDb)
        : const DatabaseConnectionConfig(database: 'file4base_dev');

    List<Map<String, dynamic>> parseList(dynamic raw) {
      if (raw is List) {
        return raw.map((item) => item is Map ? Map<String, dynamic>.from(item) : <String, dynamic>{}).toList();
      }
      return [];
    }

    return SolutionPackage(
      format: map['format']?.toString() ?? 'file4base_solution',
      version: map['version']?.toString() ?? '1.0',
      solutionName: map['solution_name']?.toString() ?? 'Untitled Solution',
      databaseConnection: dbConfig,
      tables: parseList(map['tables']),
      tableOccurrences: parseList(map['table_occurrences']),
      layouts: parseList(map['layouts']),
      users: parseList(map['users']),
      createdAt: map['created_at']?.toString(),
      updatedAt: map['updated_at']?.toString(),
    );
  }

  /// Serializes this solution to MessagePack binary (.f4b)
  Uint8List toMsgPack() {
    return msgpack.serialize(toMap());
  }

  /// Deserializes a solution from MessagePack binary (.f4b)
  static SolutionPackage fromMsgPack(Uint8List bytes) {
    final decoded = msgpack.deserialize(bytes);
    if (decoded is Map) {
      return SolutionPackage.fromMap(decoded);
    }
    throw FormatException('Invalid MessagePack solution payload');
  }

  /// Factory from live UI data
  factory SolutionPackage.fromLiveData({
    required String solutionName,
    required DatabaseConnectionConfig dbConfig,
    required List<TableModel> tables,
    required List<TableOccurrenceModel> occurrences,
    required List<LayoutModel> layouts,
  }) {
    final tablesMap = tables.map((t) => {
      'id': t.id,
      'name': t.name,
      'display_name': t.displayName,
      'description': t.description ?? '',
      'columns': t.columns.map((c) => {
        'id': c.id,
        'table_id': c.tableId,
        'name': c.name,
        'display_name': c.displayName,
        'field_type': c.fieldType,
        'is_nullable': c.isNullable,
        'is_primary_key': c.isPrimaryKey,
      }).toList(),
    }).toList();

    final occMap = occurrences.map((o) => {
      'id': o.id,
      'base_table_id': o.baseTableId,
      'name': o.name,
      'x_pos': o.xPos,
      'y_pos': o.yPos,
    }).toList();

    final layMap = layouts.map((l) => {
      'id': l.id,
      'name': l.name,
      'table_occurrence_id': l.tableOccurrenceId,
      'definition': l.definition,
    }).toList();

    return SolutionPackage(
      solutionName: solutionName,
      databaseConnection: dbConfig,
      tables: tablesMap,
      tableOccurrences: occMap,
      layouts: layMap,
      users: [
        {
          'username': 'admin',
          'role': 'Full Access',
        }
      ],
    );
  }
}
