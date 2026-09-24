import 'dart:convert';
import 'dart:math' as math;
import 'dart:typed_data';
import 'package:http/http.dart' as http;

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
      name: json['name'] as String,
      xPos: (json['x_pos'] as num?)?.toDouble() ?? 100.0,
      yPos: (json['y_pos'] as num?)?.toDouble() ?? 100.0,
    );
  }
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
  final String? fileName;
  final String? solutionName;
  final dynamic directoryRef;

  const AuthResult({
    required this.status,
    required this.database,
    required this.user,
    this.fileName,
    this.solutionName,
    this.directoryRef,
  });

  factory AuthResult.fromJson(Map<String, dynamic> json) {
    return AuthResult(
      status: json['status'] as String? ?? 'ok',
      database: json['database'] as String? ?? '',
      user: UserModel.fromJson(json['user'] as Map<String, dynamic>),
      fileName: json['file_name'] as String?,
      solutionName: json['solution_name'] as String?,
    );
  }

  AuthResult copyWith({
    String? status,
    String? database,
    UserModel? user,
    String? fileName,
    String? solutionName,
    dynamic directoryRef,
  }) {
    return AuthResult(
      status: status ?? this.status,
      database: database ?? this.database,
      user: user ?? this.user,
      fileName: fileName ?? this.fileName,
      solutionName: solutionName ?? this.solutionName,
      directoryRef: directoryRef ?? this.directoryRef,
    );
  }
}

class ApiClient {
  final String baseUrl;
  final http.Client _httpClient;

  ApiClient({
    this.baseUrl = 'http://localhost:8080',
    http.Client? httpClient,
  }) : _httpClient = httpClient ?? http.Client();

  Map<String, String> _headers({Map<String, String>? extra, String? contentType}) {
    final trace = TraceContext.create();
    final headers = <String, String>{
      'Accept': 'application/json',
      ...trace.toHeaders(),
    };
    if (contentType != null) {
      headers['Content-Type'] = contentType;
    }
    if (extra != null) {
      headers.addAll(extra);
    }
    return headers;
  }

  void _checkResponse(http.Response response) {
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

  Future<List<Map<String, dynamic>>> listRows(String table, {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true}) async {
    final uri = Uri.parse('$baseUrl/api/v1/data/$table').replace(queryParameters: {
      'limit': limit.toString(),
      'offset': offset.toString(),
      if (sortBy != null) 'sort_by': sortBy,
      'sort_asc': sortAsc.toString(),
    });

    final response = await _httpClient.get(uri, headers: _headers());
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => Map<String, dynamic>.from(item as Map)).toList();
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

  Future<List<TableOccurrenceModel>> listOccurrences() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/schemas/occurrences'),
      headers: _headers(),
    );
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => TableOccurrenceModel.fromJson(item as Map<String, dynamic>)).toList();
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

  Future<Uint8List> exportSolution({String? name, String? host, int? port, String? user, String? password}) async {
    final queryParams = <String, String>{};
    if (name != null) queryParams['name'] = name;
    if (host != null) queryParams['host'] = host;
    if (port != null) queryParams['port'] = port.toString();
    if (user != null) queryParams['user'] = user;
    if (password != null) queryParams['password'] = password;

    final uri = Uri.parse('$baseUrl/api/v1/solutions/export').replace(queryParameters: queryParams.isEmpty ? null : queryParams);
    final response = await _httpClient.get(uri, headers: _headers());
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
    _checkResponse(response);
    return AuthResult.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<List<UserModel>> listUsers() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/security/users'),
      headers: _headers(),
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
  }) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/security/users'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({
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
  }) async {
    final Map<String, dynamic> body = {'role': role};
    if (password != null && password.isNotEmpty) {
      body['password'] = password;
    }
    if (isActive != null) {
      body['is_active'] = isActive;
    }

    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/security/users/$id'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode(body),
    );
    _checkResponse(response);
  }

  Future<void> deleteUser(String id) async {
    final response = await _httpClient.delete(
      Uri.parse('$baseUrl/api/v1/security/users/$id'),
      headers: _headers(),
    );
    _checkResponse(response);
  }

  Future<List<UserLayoutPermissionModel>> getUserPermissions(String userId) async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/security/users/$userId/permissions'),
      headers: _headers(),
    );
    _checkResponse(response);
    final list = jsonDecode(response.body) as List<dynamic>;
    return list.map((item) => UserLayoutPermissionModel.fromJson(item as Map<String, dynamic>)).toList();
  }

  Future<void> setUserPermissions(String userId, List<Map<String, String>> permissions) async {
    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/security/users/$userId/permissions'),
      headers: _headers(contentType: 'application/json'),
      body: jsonEncode({'permissions': permissions}),
    );
    _checkResponse(response);
  }

  void close() {
    _httpClient.close();
  }
}

