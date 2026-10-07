import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;
import 'dart:typed_data';
import 'package:http/http.dart' as http;
import '../models/script_models.dart';

export '../models/script_models.dart';

/// RFC 9457 Problem Details Exception
class ApiException implements Exception {
  final int statusCode;
  final String title;
  final String detail;
  final String? type;
  final String? instance;
  final String? traceId;
  final Map<String, dynamic> raw;

  ApiException({
    required this.statusCode,
    required this.title,
    required this.detail,
    this.type,
    this.instance,
    this.traceId,
    this.raw = const {},
  });

  factory ApiException.fromResponse(http.Response response) {
    String title = 'HTTP ${response.statusCode} Error';
    String detail = response.body;
    String? type;
    String? instance;
    String? traceId = response.headers['x-trace-id'];
    Map<String, dynamic> raw = {};

    if (response.body.isNotEmpty) {
      try {
        final decoded = jsonDecode(response.body);
        if (decoded is Map<String, dynamic>) {
          raw = decoded;
          title = decoded['title'] as String? ?? title;
          detail = decoded['detail'] as String? ?? detail;
          type = decoded['type'] as String?;
          instance = decoded['instance'] as String?;
          traceId = decoded['trace_id'] as String? ?? traceId;
        }
      } catch (_) {}
    }

    return ApiException(
      statusCode: response.statusCode,
      title: title,
      detail: detail,
      type: type,
      instance: instance,
      traceId: traceId,
      raw: raw,
    );
  }

  @override
  String toString() {
    final tid = traceId != null ? ' [Trace ID: $traceId]' : '';
    return 'ApiException ($statusCode): $title - $detail$tid';
  }
}

/// W3C TraceContext implementation (W3C Recommendation / OpenTelemetry compliant)
class TraceContext {
  final String traceId;
  final String spanId;

  TraceContext({required this.traceId, required this.spanId});

  static String _generateHex(int byteCount) {
    math.Random rng;
    try {
      rng = math.Random.secure();
    } catch (_) {
      rng = math.Random();
    }
    final buffer = StringBuffer();
    for (int i = 0; i < byteCount; i++) {
      buffer.write(rng.nextInt(256).toRadixString(16).padLeft(2, '0'));
    }
    return buffer.toString();
  }

  factory TraceContext.create() {
    return TraceContext(
      traceId: _generateHex(16), // 16 bytes = 32 hex chars
      spanId: _generateHex(8),   // 8 bytes = 16 hex chars
    );
  }

  String get traceparent => '00-$traceId-$spanId-01';

  Map<String, String> toHeaders() => {
    'traceparent': traceparent,
    'X-Trace-ID': traceId,
  };
}

class TableModel {
  final String id;
  final String name;
  final String displayName;
  final String? description;
  final List<ColumnModel> columns;

  const TableModel({
    required this.id,
    required this.name,
    required this.displayName,
    this.description,
    this.columns = const [],
  });

  factory TableModel.fromJson(Map<String, dynamic> json) {
    var rawCols = json['columns'] as List<dynamic>? ?? [];
    return TableModel(
      id: json['id'] as String,
      name: json['name'] as String,
      displayName: json['display_name'] as String,
      description: json['description'] as String?,
      columns: rawCols.map((c) => ColumnModel.fromJson(c as Map<String, dynamic>)).toList(),
    );
  }
}

class ColumnModel {
  final String id;
  final String tableId;
  final String name;
  final String displayName;
  final String fieldType;
  final bool isNullable;
  final bool isPrimaryKey;
  final String? defaultValue;
  final String? calculationFormula;
  final String? validationRules;

  const ColumnModel({
    required this.id,
    required this.tableId,
    required this.name,
    required this.displayName,
    required this.fieldType,
    this.isNullable = true,
    this.isPrimaryKey = false,
    this.defaultValue,
    this.calculationFormula,
    this.validationRules,
  });

  factory ColumnModel.fromJson(Map<String, dynamic> json) {
    return ColumnModel(
      id: json['id'] as String,
      tableId: json['table_id'] as String,
      name: json['name'] as String,
      displayName: json['display_name'] as String,
      fieldType: json['field_type'] as String,
      isNullable: json['is_nullable'] as bool? ?? true,
      isPrimaryKey: json['is_primary_key'] as bool? ?? false,
      defaultValue: json['default_value'] as String?,
      calculationFormula: json['calculation_formula'] as String?,
      validationRules: json['validation_rules'] as String?,
    );
  }

  ColumnModel copyWith({
    String? id,
    String? tableId,
    String? name,
    String? displayName,
    String? fieldType,
    bool? isNullable,
    bool? isPrimaryKey,
    String? defaultValue,
    String? calculationFormula,
    String? validationRules,
  }) {
    return ColumnModel(
      id: id ?? this.id,
      tableId: tableId ?? this.tableId,
      name: name ?? this.name,
      displayName: displayName ?? this.displayName,
      fieldType: fieldType ?? this.fieldType,
      isNullable: isNullable ?? this.isNullable,
      isPrimaryKey: isPrimaryKey ?? this.isPrimaryKey,
      defaultValue: defaultValue ?? this.defaultValue,
      calculationFormula: calculationFormula ?? this.calculationFormula,
      validationRules: validationRules ?? this.validationRules,
    );
  }
}

/// A named set of values a field can be filled from (#31).
///
/// A value list only decides what a field *offers*. It does not restrict what
/// can be stored: that is the "existing value" validation rule.
class ValueListModel {
  /// A fixed list the designer typed.
  static const String kindCustom = 'custom';

  /// The values a field already holds, so the list grows with the data.
  static const String kindFromField = 'field';

  final String id;
  final String name;
  final String kind;

  /// One value per line, for a custom list.
  final String customValues;

  final String? sourceTableId;
  final String? sourceColumnId;

  const ValueListModel({
    required this.id,
    required this.name,
    this.kind = kindCustom,
    this.customValues = '',
    this.sourceTableId,
    this.sourceColumnId,
  });

  bool get isFromField => kind == kindFromField;

  /// The values of a custom list, in order, without blanks or repeats. A list
  /// taken from a field is resolved by the server instead.
  List<String> get customValueList {
    final seen = <String>{};
    final values = <String>[];
    for (final line in customValues.replaceAll('\r\n', '\n').split('\n')) {
      final value = line.trim();
      if (value.isEmpty || !seen.add(value)) continue;
      values.add(value);
    }
    return values;
  }

  factory ValueListModel.fromJson(Map<String, dynamic> json) => ValueListModel(
        id: json['id'] as String,
        name: json['name'] as String? ?? '',
        kind: json['kind'] as String? ?? kindCustom,
        customValues: json['custom_values'] as String? ?? '',
        sourceTableId: json['source_table_id'] as String?,
        sourceColumnId: json['source_column_id'] as String?,
      );

  Map<String, dynamic> toJson() => {
        'name': name,
        'kind': kind,
        'custom_values': customValues,
        'source_table_id': sourceTableId,
        'source_column_id': sourceColumnId,
      };

  ValueListModel copyWith({
    String? name,
    String? kind,
    String? customValues,
    String? sourceTableId,
    String? sourceColumnId,
  }) =>
      ValueListModel(
        id: id,
        name: name ?? this.name,
        kind: kind ?? this.kind,
        customValues: customValues ?? this.customValues,
        sourceTableId: sourceTableId ?? this.sourceTableId,
        sourceColumnId: sourceColumnId ?? this.sourceColumnId,
      );
}

class TableOccurrenceModel {
  final String id;
  final String baseTableId;
  final String name;
  final double xPos;
  final double yPos;

  const TableOccurrenceModel({
    required this.id,
    required this.baseTableId,
    required this.name,
    required this.xPos,
    required this.yPos,
  });

  factory TableOccurrenceModel.fromJson(Map<String, dynamic> json) {
    return TableOccurrenceModel(
      id: json['id'] as String,
      baseTableId: json['base_table_id'] as String,
      name: json['name'] as String? ?? '',
      xPos: (json['x_pos'] as num?)?.toDouble() ?? 100.0,
      yPos: (json['y_pos'] as num?)?.toDouble() ?? 100.0,
    );
  }

  Map<String, dynamic> toJson() => {
    'id': id,
    'base_table_id': baseTableId,
    'name': name,
    'x_pos': xPos,
    'y_pos': yPos,
  };

  TableOccurrenceModel copyWith({
    String? id,
    String? baseTableId,
    String? name,
    double? xPos,
    double? yPos,
  }) {
    return TableOccurrenceModel(
      id: id ?? this.id,
      baseTableId: baseTableId ?? this.baseTableId,
      name: name ?? this.name,
      xPos: xPos ?? this.xPos,
      yPos: yPos ?? this.yPos,
    );
  }
}

class RelationshipModel {
  final String id;
  final String name;
  final String leftOccurrenceId;
  final String leftColumnId;
  final String rightOccurrenceId;
  final String rightColumnId;
  final String operator;
  final bool allowCreation;
  final bool cascadeDelete;
  final String? sortRelated;

  const RelationshipModel({
    required this.id,
    required this.name,
    required this.leftOccurrenceId,
    required this.leftColumnId,
    required this.rightOccurrenceId,
    required this.rightColumnId,
    this.operator = '=',
    this.allowCreation = false,
    this.cascadeDelete = false,
    this.sortRelated,
  });

  factory RelationshipModel.fromJson(Map<String, dynamic> json) {
    return RelationshipModel(
      id: json['id'] as String,
      name: json['name'] as String? ?? '',
      leftOccurrenceId: json['left_occurrence_id'] as String,
      leftColumnId: json['left_column_id'] as String,
      rightOccurrenceId: json['right_occurrence_id'] as String,
      rightColumnId: json['right_column_id'] as String,
      operator: json['operator'] as String? ?? '=',
      allowCreation: json['allow_creation'] as bool? ?? false,
      cascadeDelete: json['cascade_delete'] as bool? ?? false,
      sortRelated: json['sort_related'] as String?,
    );
  }

  Map<String, dynamic> toJson() => {
    'id': id,
    'name': name,
    'left_occurrence_id': leftOccurrenceId,
    'left_column_id': leftColumnId,
    'right_occurrence_id': rightOccurrenceId,
    'right_column_id': rightColumnId,
    'operator': operator,
    'allow_creation': allowCreation,
    'cascade_delete': cascadeDelete,
    if (sortRelated != null) 'sort_related': sortRelated,
  };
}

class LayoutModel {
  final String id;
  final String name;
  final String tableOccurrenceId;
  final Map<String, dynamic> definition;

  const LayoutModel({
    required this.id,
    required this.name,
    required this.tableOccurrenceId,
    required this.definition,
  });

  factory LayoutModel.fromJson(Map<String, dynamic> json) {
    var rawDef = json['definition'];
    Map<String, dynamic> defMap = {};
    if (rawDef is Map<String, dynamic>) {
      defMap = rawDef;
    } else if (rawDef is String) {
      try {
        defMap = jsonDecode(rawDef) as Map<String, dynamic>;
      } catch (_) {}
    }
    return LayoutModel(
      id: json['id'] as String,
      name: json['name'] as String,
      tableOccurrenceId: json['table_occurrence_id'] as String? ?? '',
      definition: defMap,
    );
  }
}

class UserLayoutPermissionModel {
  final String id;
  final String userId;
  final String layoutId;
  final String layoutName;
  final String accessLevel; // 'read_write', 'read_only', 'none'

  const UserLayoutPermissionModel({
    required this.id,
    required this.userId,
    required this.layoutId,
    this.layoutName = '',
    this.accessLevel = 'read_write',
  });

  factory UserLayoutPermissionModel.fromJson(Map<String, dynamic> json) {
    return UserLayoutPermissionModel(
      id: json['id'] as String? ?? '',
      userId: json['user_id'] as String? ?? '',
      layoutId: json['layout_id'] as String? ?? '',
      layoutName: json['layout_name'] as String? ?? '',
      accessLevel: json['access_level'] as String? ?? 'read_write',
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'user_id': userId,
        'layout_id': layoutId,
        'access_level': accessLevel,
      };
}

class UserModel {
  final String id;
  final String username;
  final String role; // 'owner', 'admin', 'user'
  final bool isActive;
  final DateTime? createdAt;
  final DateTime? updatedAt;
  final List<UserLayoutPermissionModel> permissions;

  const UserModel({
    required this.id,
    required this.username,
    required this.role,
    this.isActive = true,
    this.createdAt,
    this.updatedAt,
    this.permissions = const [],
  });

  bool get isOwner => role == 'owner';
  bool get isAdmin => role == 'admin' || role == 'owner';

  UserModel copyWith({
    String? id,
    String? username,
    String? role,
    bool? isActive,
    DateTime? createdAt,
    DateTime? updatedAt,
    List<UserLayoutPermissionModel>? permissions,
  }) {
    return UserModel(
      id: id ?? this.id,
      username: username ?? this.username,
      role: role ?? this.role,
      isActive: isActive ?? this.isActive,
      createdAt: createdAt ?? this.createdAt,
      updatedAt: updatedAt ?? this.updatedAt,
      permissions: permissions ?? this.permissions,
    );
  }

  factory UserModel.fromJson(Map<String, dynamic> json) {
    var rawPerms = json['permissions'];
    List<UserLayoutPermissionModel> perms = [];
    if (rawPerms is List) {
      perms = rawPerms.map((p) => UserLayoutPermissionModel.fromJson(p as Map<String, dynamic>)).toList();
    }

    return UserModel(
      id: json['id'] as String? ?? '',
      username: json['username'] as String? ?? '',
      role: json['role'] as String? ?? 'user',
      isActive: json['is_active'] as bool? ?? true,
      createdAt: json['created_at'] != null ? DateTime.tryParse(json['created_at'].toString()) : null,
      updatedAt: json['updated_at'] != null ? DateTime.tryParse(json['updated_at'].toString()) : null,
      permissions: perms,
    );
  }

  Map<String, dynamic> toJson() => {
    'id': id,
    'username': username,
    'role': role,
    'is_active': isActive,
    if (createdAt != null) 'created_at': createdAt!.toIso8601String(),
    if (updatedAt != null) 'updated_at': updatedAt!.toIso8601String(),
    'permissions': permissions.map((p) => p.toJson()).toList(),
  };
}

class AuthResult {
  final String status;
  final String database;
  final UserModel user;

  /// Bearer token of the session opened by the server for this sign-in.
  final String? token;
  final String? fileName;
  final String? solutionName;
  final dynamic directoryRef;

  const AuthResult({
    required this.status,
    required this.database,
    required this.user,
    this.token,
    this.fileName,
    this.solutionName,
    this.directoryRef,
  });

  factory AuthResult.fromJson(Map<String, dynamic> json) {
    return AuthResult(
      status: json['status'] as String? ?? 'ok',
      database: json['database'] as String? ?? '',
      user: UserModel.fromJson(json['user'] as Map<String, dynamic>),
      token: json['token'] as String?,
      fileName: json['file_name'] as String?,
      solutionName: json['solution_name'] as String?,
    );
  }

  AuthResult copyWith({
    String? status,
    String? database,
    UserModel? user,
    String? token,
    String? fileName,
    String? solutionName,
    dynamic directoryRef,
  }) {
    return AuthResult(
      status: status ?? this.status,
      database: database ?? this.database,
      user: user ?? this.user,
      token: token ?? this.token,
      fileName: fileName ?? this.fileName,
      solutionName: solutionName ?? this.solutionName,
      directoryRef: directoryRef ?? this.directoryRef,
    );
  }
}

class ApiClient {
  final String baseUrl;
  final http.Client _httpClient;

  /// Bearer token of the current server session (set by [login]).
  /// It is kept in memory only and sent on every request.
  String? _authToken;

  /// Invoked when the server answers 401 to a request other than sign-in,
  /// i.e. the session expired or was revoked and the user must sign in again.
  void Function()? onUnauthorized;

  ApiClient({
    this.baseUrl = 'http://localhost:8080',
    http.Client? httpClient,
  }) : _httpClient = httpClient ?? http.Client();

  /// Whether this client currently holds a session token.
  bool get isAuthenticated => _authToken != null && _authToken!.isNotEmpty;

  /// The current session token, if any.
  String? get authToken => _authToken;

  /// Drops the session token locally without contacting the server.
  void clearSession() {
    _authToken = null;
  }

  Map<String, String> _headers({Map<String, String>? extra, String? contentType}) {
    final trace = TraceContext.create();
    final headers = <String, String>{
      'Accept': 'application/json',
      ...trace.toHeaders(),
    };
    final token = _authToken;
    if (token != null && token.isNotEmpty) {
      headers['Authorization'] = 'Bearer $token';
    }
    if (contentType != null) {
      headers['Content-Type'] = contentType;
    }
    if (extra != null) {
      headers.addAll(extra);
    }
    return headers;
  }

  void _checkResponse(http.Response response, {bool isSignIn = false}) {
    if (response.statusCode == 401 && !isSignIn) {
      // The session expired or was revoked on the server
      _authToken = null;
      onUnauthorized?.call();
    }
    if (response.statusCode >= 400) {
      throw ApiException.fromResponse(response);
    }
  }

  /// Diagnostic Health Check (/healthz - IETF draft format + backward-compatible fields)
  Future<Map<String, dynamic>> checkHealth() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/healthz'),
      headers: _headers(),
    );
    _checkResponse(response);
    return jsonDecode(response.body) as Map<String, dynamic>;
  }

  /// Cloud-native Kubernetes Liveness probe (/healthz/liveness or /livez)
  Future<bool> checkLiveness() async {
    try {
      final response = await _httpClient.get(
        Uri.parse('$baseUrl/healthz/liveness'),
        headers: _headers(),
      );
      return response.statusCode == 200;
    } catch (_) {
      return false;
    }
  }

  /// Cloud-native Kubernetes Readiness probe (/healthz/readiness or /readyz)
  Future<bool> checkReadiness() async {
    try {
      final response = await _httpClient.get(
        Uri.parse('$baseUrl/healthz/readiness'),
        headers: _headers(),
      );
      return response.statusCode == 200;
    } catch (_) {
      return false;
    }
  }

  /// Cloud-native Kubernetes Startup probe (/healthz/startup or /startupz)
  Future<bool> checkStartup() async {
    try {
      final response = await _httpClient.get(
        Uri.parse('$baseUrl/healthz/startup'),
        headers: _headers(),
      );
      return response.statusCode == 200;
    } catch (_) {
      return false;
    }
  }

  Future<List<TableModel>> listTables() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/schemas/tables'),
      headers: _headers(),
    );
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => TableModel.fromJson(item as Map<String, dynamic>)).toList();
  }

  Future<TableModel> createTable(String displayName, {String? customName}) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/schemas/tables'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({
        'display_name': displayName,
        if (customName != null && customName.isNotEmpty) 'custom_name': customName,
      }),
    );
    _checkResponse(response);
    return TableModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<void> deleteTable(String tableId) async {
    final response = await _httpClient.delete(
      Uri.parse('$baseUrl/api/v1/schemas/tables/$tableId'),
      headers: _headers(),
    );
    _checkResponse(response);
  }

  Future<TableModel> renameTable(String tableId, String newDisplayName) async {
    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/schemas/tables/$tableId/rename'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({'display_name': newDisplayName}),
    );
    _checkResponse(response);
    return TableModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<TableModel> duplicateTable(String tableId) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/schemas/tables/$tableId/duplicate'),
      headers: _headers(),
    );
    _checkResponse(response);
    return TableModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<void> truncateTable(String tableId) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/schemas/tables/$tableId/truncate'),
      headers: _headers(),
    );
    _checkResponse(response);
  }

  Future<ColumnModel> addColumn(String tableId, {
    required String name,
    required String displayName,
    required String fieldType,
    bool isNullable = true,
  }) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/schemas/tables/$tableId/columns'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({
        'name': name,
        'display_name': displayName,
        'field_type': fieldType,
        'is_nullable': isNullable,
      }),
    );
    _checkResponse(response);
    return ColumnModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<ColumnModel> updateColumn(
    String tableId,
    String columnId, {
    required String displayName,
    String? defaultValue,
    bool updateDefaultValue = false,
    String? calculationFormula,
    bool updateCalculationFormula = false,
    String? validationRules,
    bool updateValidationRules = false,
  }) async {
    final Map<String, dynamic> payload = {
      'display_name': displayName,
    };
    if (updateDefaultValue) {
      payload['default_value'] = defaultValue;
    }
    if (updateCalculationFormula) {
      payload['calculation_formula'] = calculationFormula;
    }
    if (updateValidationRules) {
      payload['validation_rules'] = validationRules;
    }

    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/schemas/tables/$tableId/columns/$columnId'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode(payload),
    );
    _checkResponse(response);
    return ColumnModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<void> deleteColumn(String tableId, String columnId) async {
    final response = await _httpClient.delete(
      Uri.parse('$baseUrl/api/v1/schemas/tables/$tableId/columns/$columnId'),
      headers: _headers(),
    );
    _checkResponse(response);
  }

  /// Lists rows. [sort] orders by each entry in turn, outermost first, in the
  /// form the data API takes: `last_name` ascending, `-fee_paid` descending.
  /// [sortBy]/[sortAsc] are the single-field form and are ignored when [sort]
  /// is given.
  Future<List<Map<String, dynamic>>> listRows(String table,
      {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async {
    final uri = Uri.parse('$baseUrl/api/v1/data/$table').replace(queryParameters: {
      'limit': limit.toString(),
      'offset': offset.toString(),
      if (sortBy != null) 'sort_by': sortBy,
      'sort_asc': sortAsc.toString(),
      if (sort != null && sort.isNotEmpty) 'sort': sort.join(','),
    });

    final response = await _httpClient.get(uri, headers: _headers());
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => Map<String, dynamic>.from(item as Map)).toList();
  }

  /// Largest page the server returns for [listRows] (`limit` is clamped to it).
  static const maxPageSize = 1000;

  /// Every row of [table], fetched page by page in a stable order (by `id`).
  /// Throws if any page fails, so callers never get a silently partial set.
  /// [onProgress] receives the number of rows fetched so far.
  Future<List<Map<String, dynamic>>> listAllRows(String table, {void Function(int fetched)? onProgress}) async {
    final all = <Map<String, dynamic>>[];
    while (true) {
      final page = await listRows(table, limit: maxPageSize, offset: all.length, sortBy: 'id', sortAsc: true);
      all.addAll(page);
      onProgress?.call(all.length);
      if (page.length < maxPageSize) return all;
    }
  }

  Future<Map<String, dynamic>> insertRow(String table, Map<String, dynamic> record) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/data/$table'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode(record),
    );
    _checkResponse(response);
    return Map<String, dynamic>.from(jsonDecode(response.body) as Map);
  }

  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> updates) async {
    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/data/$table/$id'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode(updates),
    );
    _checkResponse(response);
    return Map<String, dynamic>.from(jsonDecode(response.body) as Map);
  }

  Future<void> deleteRow(String table, String id) async {
    final response = await _httpClient.delete(
      Uri.parse('$baseUrl/api/v1/data/$table/$id'),
      headers: _headers(),
    );
    _checkResponse(response);
  }

  Future<List<Map<String, dynamic>>> executeFind(String table, List<Map<String, dynamic>> requests) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/data/$table/find'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({
        'requests': requests,
        'options': {'limit': 500, 'offset': 0},
      }),
    );
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => Map<String, dynamic>.from(item as Map)).toList();
  }

  // ─── Value lists (#31) ─────────────────────────────────────────────────────

  Future<List<ValueListModel>> listValueLists() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/value-lists'),
      headers: _headers(),
    );
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => ValueListModel.fromJson(item as Map<String, dynamic>)).toList();
  }

  /// The values a list offers right now: the lines of a custom list, or the
  /// distinct values the field it reads holds.
  Future<List<String>> valueListValues(String id) async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/value-lists/$id/values'),
      headers: _headers(),
    );
    _checkResponse(response);
    final body = jsonDecode(response.body) as Map<String, dynamic>;
    return (body['values'] as List<dynamic>? ?? []).map((v) => v.toString()).toList();
  }

  Future<ValueListModel> createValueList(ValueListModel list) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/value-lists'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode(list.toJson()),
    );
    _checkResponse(response);
    return ValueListModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<ValueListModel> updateValueList(ValueListModel list) async {
    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/value-lists/${list.id}'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode(list.toJson()),
    );
    _checkResponse(response);
    return ValueListModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<void> deleteValueList(String id) async {
    final response = await _httpClient.delete(
      Uri.parse('$baseUrl/api/v1/value-lists/$id'),
      headers: _headers(),
    );
    _checkResponse(response);
  }

  /// The records of a related table that match [id] in [table]: what a portal
  /// shows and what a related field on a layout reads (#36).
  ///
  /// [occurrence] names the side whose records are wanted. It may be left out
  /// unless the relationship joins a table to itself, where there is no other
  /// side to infer. [sort] takes the same form as in [listRows]; without it the
  /// relationship's own "Sort related records" applies.
  Future<List<Map<String, dynamic>>> listRelatedRows(
    String table,
    String id, {
    required String relationshipId,
    String? occurrence,
    List<String>? sort,
    int limit = 100,
    int offset = 0,
  }) async {
    final uri = Uri.parse('$baseUrl/api/v1/data/$table/$id/related').replace(queryParameters: {
      'relationship': relationshipId,
      if (occurrence != null && occurrence.isNotEmpty) 'occurrence': occurrence,
      'limit': limit.toString(),
      'offset': offset.toString(),
      if (sort != null && sort.isNotEmpty) 'sort': sort.join(','),
    });

    final response = await _httpClient.get(uri, headers: _headers());
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => Map<String, dynamic>.from(item as Map)).toList();
  }

  /// Creates a record on the other side of a relationship, with its match
  /// field filled from [id] so the new record belongs to it (#36).
  ///
  /// The server refuses this unless the relationship allows records to be
  /// created through it.
  Future<Map<String, dynamic>> createRelatedRow(
    String table,
    String id, {
    required String relationshipId,
    String? occurrence,
    Map<String, dynamic> values = const {},
  }) async {
    final uri = Uri.parse('$baseUrl/api/v1/data/$table/$id/related').replace(queryParameters: {
      'relationship': relationshipId,
      if (occurrence != null && occurrence.isNotEmpty) 'occurrence': occurrence,
    });

    final response = await _httpClient.post(
      uri,
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode(values),
    );
    _checkResponse(response);
    return Map<String, dynamic>.from(jsonDecode(response.body) as Map);
  }

  Future<List<TableOccurrenceModel>> listOccurrences() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/schemas/occurrences'),
      headers: _headers(),
    );
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => TableOccurrenceModel.fromJson(item as Map<String, dynamic>)).toList();
  }

  Future<TableOccurrenceModel> createOccurrence({
    required String baseTableId,
    required String name,
    double xPos = 100.0,
    double yPos = 100.0,
  }) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/schemas/occurrences'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({
        'base_table_id': baseTableId,
        'name': name,
        'x_pos': xPos,
        'y_pos': yPos,
      }),
    );
    _checkResponse(response);
    return TableOccurrenceModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<TableOccurrenceModel> updateOccurrence(
    String id, {
    String? name,
    double? xPos,
    double? yPos,
  }) async {
    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/schemas/occurrences/$id'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({
        if (name != null) 'name': name,
        if (xPos != null) 'x_pos': xPos,
        if (yPos != null) 'y_pos': yPos,
      }),
    );
    _checkResponse(response);
    return TableOccurrenceModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<void> deleteOccurrence(String id) async {
    final response = await _httpClient.delete(
      Uri.parse('$baseUrl/api/v1/schemas/occurrences/$id'),
      headers: _headers(),
    );
    _checkResponse(response);
  }

  Future<List<RelationshipModel>> listRelationships() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/schemas/relationships'),
      headers: _headers(),
    );
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => RelationshipModel.fromJson(item as Map<String, dynamic>)).toList();
  }

  Future<RelationshipModel> createRelationship({
    required String leftOccurrenceId,
    required String leftColumnId,
    required String rightOccurrenceId,
    required String rightColumnId,
    String operator = '=',
    String? name,
    bool allowCreation = false,
    bool cascadeDelete = false,
    String? sortRelated,
  }) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/schemas/relationships'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({
        'left_occurrence_id': leftOccurrenceId,
        'left_column_id': leftColumnId,
        'right_occurrence_id': rightOccurrenceId,
        'right_column_id': rightColumnId,
        'operator': operator,
        if (name != null) 'name': name,
        'allow_creation': allowCreation,
        'cascade_delete': cascadeDelete,
        if (sortRelated != null) 'sort_related': sortRelated,
      }),
    );
    _checkResponse(response);
    return RelationshipModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<RelationshipModel> updateRelationship(
    String id, {
    String? name,
    String? operator,
    bool? allowCreation,
    bool? cascadeDelete,
    String? sortRelated,
  }) async {
    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/schemas/relationships/$id'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({
        if (name != null) 'name': name,
        if (operator != null) 'operator': operator,
        if (allowCreation != null) 'allow_creation': allowCreation,
        if (cascadeDelete != null) 'cascade_delete': cascadeDelete,
        if (sortRelated != null) 'sort_related': sortRelated,
      }),
    );
    _checkResponse(response);
    return RelationshipModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<void> deleteRelationship(String id) async {
    final response = await _httpClient.delete(
      Uri.parse('$baseUrl/api/v1/schemas/relationships/$id'),
      headers: _headers(),
    );
    _checkResponse(response);
  }

  Future<List<LayoutModel>> listLayouts() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/schemas/layouts'),
      headers: _headers(),
    );
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => LayoutModel.fromJson(item as Map<String, dynamic>)).toList();
  }

  Future<LayoutModel> getLayout(String id) async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/schemas/layouts/$id'),
      headers: _headers(),
    );
    _checkResponse(response);
    return LayoutModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<LayoutModel> createLayout(String name, {String? toId, Map<String, dynamic>? definition}) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/schemas/layouts'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({
        'name': name,
        'table_occurrence_id': toId ?? '',
        'definition': definition ?? {},
      }),
    );
    _checkResponse(response);
    return LayoutModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<LayoutModel> updateLayout(String id, String name, Map<String, dynamic> definition) async {
    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/schemas/layouts/$id'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({
        'name': name,
        'definition': definition,
      }),
    );
    _checkResponse(response);
    return LayoutModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<void> deleteLayout(String id) async {
    final response = await _httpClient.delete(
      Uri.parse('$baseUrl/api/v1/schemas/layouts/$id'),
      headers: _headers(),
    );
    _checkResponse(response);
  }

  Future<Map<String, dynamic>> listDatabases() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/databases'),
      headers: _headers(),
    );
    _checkResponse(response);
    return jsonDecode(response.body) as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> createDatabase(String name, {String? user, String? password}) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/databases'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({
        'database': name,
        if (user != null && user.isNotEmpty) 'user': user,
        if (password != null && password.isNotEmpty) 'password': password,
      }),
    );
    _checkResponse(response);
    return jsonDecode(response.body) as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> switchDatabase(String name) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/databases/switch'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({'database': name}),
    );
    _checkResponse(response);
    return jsonDecode(response.body) as Map<String, dynamic>;
  }

  /// Drops a database. Unless the current session is an owner session on that
  /// same database, the server requires the credentials of one of its owners.
  Future<Map<String, dynamic>> deleteDatabase(String name, {String? ownerUsername, String? ownerPassword}) async {
    final hasCredentials = ownerUsername != null && ownerUsername.isNotEmpty && ownerPassword != null && ownerPassword.isNotEmpty;
    final response = await _httpClient.delete(
      Uri.parse('$baseUrl/api/v1/databases/${Uri.encodeComponent(name)}'),
      headers: _headers(contentType: hasCredentials ? 'application/json' : null),
      body: hasCredentials ? jsonEncode({'username': ownerUsername, 'password': ownerPassword}) : null,
    );
    _checkResponse(response);
    return jsonDecode(response.body) as Map<String, dynamic>;
  }

  /// The solution file (.f4p) of the signed-in database, written by the
  /// server (docs/specs/solution_bundle_format.md). [fileOptions] and
  /// [pageSetup] are client settings stored in the file; passwords are never
  /// included.
  Future<Uint8List> exportSolution({
    required String solutionName,
    Map<String, dynamic>? fileOptions,
    Map<String, dynamic>? pageSetup,
  }) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/solutions/export'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({
        'solution_name': solutionName,
        if (fileOptions != null) 'file_options': fileOptions,
        if (pageSetup != null) 'page_setup': pageSetup,
      }),
    );
    _checkResponse(response);
    return response.bodyBytes;
  }

  Future<Map<String, dynamic>> importSolution(Uint8List bytes) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/solutions/import'),
      headers: _headers(contentType: 'application/x-msgpack'),
      body: bytes,
    );
    _checkResponse(response);
    return jsonDecode(response.body) as Map<String, dynamic>;
  }

  Future<Uint8List> exportDatabaseData() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/solutions/export-data'),
      headers: _headers(),
    );
    _checkResponse(response);
    return response.bodyBytes;
  }

  Future<Map<String, dynamic>> importDatabaseData(Uint8List bytes) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/solutions/import-data'),
      headers: _headers(contentType: 'application/x-msgpack'),
      body: bytes,
    );
    _checkResponse(response);
    return jsonDecode(response.body) as Map<String, dynamic>;
  }

  // ─── Security & Authentication ─────────────────────────────────────────────

  Future<AuthResult> login({
    required String username,
    required String password,
    String? database,
  }) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/auth/login'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({
        'username': username,
        'password': password,
        'database': database ?? '',
      }),
    );
    _checkResponse(response, isSignIn: true);
    final result = AuthResult.fromJson(jsonDecode(response.body) as Map<String, dynamic>);

    // Every later request runs under the session that was just opened. A
    // session replaced by a new sign-in is closed on the server as well.
    final previousToken = _authToken;
    _authToken = result.token;
    if (previousToken != null && previousToken.isNotEmpty && previousToken != result.token) {
      unawaited(_revokeToken(previousToken));
    }
    return result;
  }

  /// Signs out: closes the server session and forgets the token.
  Future<void> logout() async {
    final token = _authToken;
    _authToken = null;
    if (token != null && token.isNotEmpty) {
      await _revokeToken(token);
    }
  }

  Future<void> _revokeToken(String token) async {
    try {
      final trace = TraceContext.create();
      await _httpClient.post(
        Uri.parse('$baseUrl/api/v1/auth/logout'),
        headers: <String, String>{
          'Accept': 'application/json',
          'Authorization': 'Bearer $token',
          ...trace.toHeaders(),
        },
      );
    } catch (_) {
      // Best effort: the session also expires on its own
    }
  }

  Future<List<UserModel>> listUsers({String? database}) async {
    final query = (database != null && database.isNotEmpty) ? '?database=${Uri.encodeComponent(database)}' : '';
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/security/users$query'),
      headers: _headers(extra: database != null ? {'X-Database-Name': database} : null),
    );
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => UserModel.fromJson(item as Map<String, dynamic>)).toList();
  }

  Future<UserModel> createUser({
    required String username,
    required String password,
    required String role,
    bool isActive = true,
    String? database,
  }) async {
    final query = (database != null && database.isNotEmpty) ? '?database=${Uri.encodeComponent(database)}' : '';
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/security/users$query'),
      headers: _headers(contentType: 'application/json', extra: database != null ? {'X-Database-Name': database} : null),
      body: jsonEncode({
        'database': database ?? '',
        'username': username,
        'password': password,
        'role': role,
        'is_active': isActive,
      }),
    );
    _checkResponse(response);
    return UserModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<void> updateUser(
    String id, {
    String? password,
    required String role,
    bool? isActive,
    String? database,
  }) async {
    final Map<String, dynamic> body = {'role': role};
    if (database != null && database.isNotEmpty) {
      body['database'] = database;
    }
    if (password != null && password.isNotEmpty) {
      body['password'] = password;
    }
    if (isActive != null) {
      body['is_active'] = isActive;
    }

    final query = (database != null && database.isNotEmpty) ? '?database=${Uri.encodeComponent(database)}' : '';
    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/security/users/$id$query'),
      headers: _headers(contentType: 'application/json', extra: database != null ? {'X-Database-Name': database} : null),
      body: jsonEncode(body),
    );
    _checkResponse(response);
  }

  Future<void> deleteUser(String id, {String? database}) async {
    final query = (database != null && database.isNotEmpty) ? '?database=${Uri.encodeComponent(database)}' : '';
    final response = await _httpClient.delete(
      Uri.parse('$baseUrl/api/v1/security/users/$id$query'),
      headers: _headers(extra: database != null ? {'X-Database-Name': database} : null),
    );
    _checkResponse(response);
  }

  Future<List<UserLayoutPermissionModel>> getUserPermissions(String userId, {String? database}) async {
    final query = (database != null && database.isNotEmpty) ? '?database=${Uri.encodeComponent(database)}' : '';
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/security/users/$userId/permissions$query'),
      headers: _headers(extra: database != null ? {'X-Database-Name': database} : null),
    );
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => UserLayoutPermissionModel.fromJson(item as Map<String, dynamic>)).toList();
  }

  Future<void> setUserPermissions(String userId, List<Map<String, String>> permissions, {String? database}) async {
    final query = (database != null && database.isNotEmpty) ? '?database=${Uri.encodeComponent(database)}' : '';
    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/security/users/$userId/permissions$query'),
      headers: _headers(contentType: 'application/json', extra: database != null ? {'X-Database-Name': database} : null),
      body: jsonEncode({
        'database': database ?? '',
        'permissions': permissions,
      }),
    );
    _checkResponse(response);
  }

  // ==========================================
  // Script Workspace API
  // ==========================================

  Future<List<ScriptModel>> listScripts({String? database}) async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/schemas/scripts'),
      headers: _headers(extra: database != null ? {'X-Database-Name': database} : null),
    );
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => ScriptModel.fromJson(item as Map<String, dynamic>)).toList();
  }

  Future<ScriptModel> getScript(String id, {String? database}) async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/schemas/scripts/$id'),
      headers: _headers(extra: database != null ? {'X-Database-Name': database} : null),
    );
    _checkResponse(response);
    return ScriptModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<ScriptModel> createScript(Map<String, dynamic> data, {String? database}) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/schemas/scripts'),
      headers: _headers(contentType: 'application/json', extra: database != null ? {'X-Database-Name': database} : null),
      body: jsonEncode(data),
    );
    _checkResponse(response);
    return ScriptModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<ScriptModel> updateScript(String id, Map<String, dynamic> data, {String? database}) async {
    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/schemas/scripts/$id'),
      headers: _headers(contentType: 'application/json', extra: database != null ? {'X-Database-Name': database} : null),
      body: jsonEncode(data),
    );
    _checkResponse(response);
    return ScriptModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<void> deleteScript(String id, {String? database}) async {
    final response = await _httpClient.delete(
      Uri.parse('$baseUrl/api/v1/schemas/scripts/$id'),
      headers: _headers(extra: database != null ? {'X-Database-Name': database} : null),
    );
    _checkResponse(response);
  }

  Future<ScriptModel> duplicateScript(String id, {String? database}) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/schemas/scripts/$id/duplicate'),
      headers: _headers(extra: database != null ? {'X-Database-Name': database} : null),
    );
    _checkResponse(response);
    return ScriptModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  void close() {
    _httpClient.close();
  }
}


