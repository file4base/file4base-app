import 'dart:convert';
import 'package:http/http.dart' as http;

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

  const ColumnModel({
    required this.id,
    required this.tableId,
    required this.name,
    required this.displayName,
    required this.fieldType,
    this.isNullable = true,
    this.isPrimaryKey = false,
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

class ApiClient {
  final String baseUrl;
  final http.Client _httpClient;

  ApiClient({
    this.baseUrl = 'http://localhost:8080',
    http.Client? httpClient,
  }) : _httpClient = httpClient ?? http.Client();

  Future<Map<String, dynamic>> checkHealth() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/healthz'),
      headers: {'Accept': 'application/json'},
    );

    if (response.statusCode >= 200 && response.statusCode < 300) {
      return jsonDecode(response.body) as Map<String, dynamic>;
    } else {
      throw Exception('Server health check failed with status: ${response.statusCode}');
    }
  }

  Future<List<TableModel>> listTables() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/schemas/tables'),
      headers: {'Accept': 'application/json'},
    );

    if (response.statusCode >= 200 && response.statusCode < 300) {
      final list = jsonDecode(response.body) as List<dynamic>;
      return list.map((item) => TableModel.fromJson(item as Map<String, dynamic>)).toList();
    } else {
      throw Exception('Failed to list tables: ${response.statusCode}');
    }
  }

  Future<TableModel> createTable(String displayName, {String? customName}) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/schemas/tables'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'display_name': displayName,
        if (customName != null && customName.isNotEmpty) 'custom_name': customName,
      }),
    );

    if (response.statusCode == 201) {
      return TableModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
    } else {
      throw Exception('Failed to create table: ${response.body}');
    }
  }

  Future<ColumnModel> addColumn(String tableId, {
    required String name,
    required String displayName,
    required String fieldType,
    bool isNullable = true,
  }) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/schemas/tables/$tableId/columns'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'name': name,
        'display_name': displayName,
        'field_type': fieldType,
        'is_nullable': isNullable,
      }),
    );

    if (response.statusCode == 201) {
      return ColumnModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
    } else {
      throw Exception('Failed to add column: ${response.body}');
    }
  }

  Future<List<Map<String, dynamic>>> listRows(String table, {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true}) async {
    final uri = Uri.parse('$baseUrl/api/v1/data/$table').replace(queryParameters: {
      'limit': limit.toString(),
      'offset': offset.toString(),
      if (sortBy != null) 'sort_by': sortBy,
      'sort_asc': sortAsc.toString(),
    });

    final response = await _httpClient.get(uri, headers: {'Accept': 'application/json'});
    if (response.statusCode >= 200 && response.statusCode < 300) {
      final list = jsonDecode(response.body) as List<dynamic>;
      return list.map((item) => Map<String, dynamic>.from(item as Map)).toList();
    } else {
      throw Exception('Failed to list records: ${response.body}');
    }
  }

  Future<Map<String, dynamic>> insertRow(String table, Map<String, dynamic> record) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/data/$table'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode(record),
    );

    if (response.statusCode == 201) {
      return Map<String, dynamic>.from(jsonDecode(response.body) as Map);
    } else {
      throw Exception('Failed to insert record: ${response.body}');
    }
  }

  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> updates) async {
    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/data/$table/$id'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode(updates),
    );

    if (response.statusCode >= 200 && response.statusCode < 300) {
      return Map<String, dynamic>.from(jsonDecode(response.body) as Map);
    } else {
      throw Exception('Failed to update record: ${response.body}');
    }
  }

  Future<void> deleteRow(String table, String id) async {
    final response = await _httpClient.delete(Uri.parse('$baseUrl/api/v1/data/$table/$id'));
    if (response.statusCode >= 200 && response.statusCode < 300) {
      return;
    } else {
      throw Exception('Failed to delete record: ${response.body}');
    }
  }

  Future<List<Map<String, dynamic>>> executeFind(String table, List<Map<String, dynamic>> requests) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/data/$table/find'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'requests': requests,
        'options': {'limit': 500, 'offset': 0},
      }),
    );

    if (response.statusCode >= 200 && response.statusCode < 300) {
      final list = jsonDecode(response.body) as List<dynamic>;
      return list.map((item) => Map<String, dynamic>.from(item as Map)).toList();
    } else {
      throw Exception('Failed to execute find: ${response.body}');
    }
  }

  Future<List<TableOccurrenceModel>> listOccurrences() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/schemas/occurrences'),
      headers: {'Accept': 'application/json'},
    );
    if (response.statusCode >= 200 && response.statusCode < 300) {
      final list = jsonDecode(response.body) as List<dynamic>;
      return list.map((item) => TableOccurrenceModel.fromJson(item as Map<String, dynamic>)).toList();
    } else {
      throw Exception('Failed to list table occurrences: ${response.body}');
    }
  }

  Future<List<LayoutModel>> listLayouts() async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/schemas/layouts'),
      headers: {'Accept': 'application/json'},
    );
    if (response.statusCode >= 200 && response.statusCode < 300) {
      final list = jsonDecode(response.body) as List<dynamic>;
      return list.map((item) => LayoutModel.fromJson(item as Map<String, dynamic>)).toList();
    } else {
      throw Exception('Failed to list layouts: ${response.body}');
    }
  }

  Future<LayoutModel> getLayout(String id) async {
    final response = await _httpClient.get(
      Uri.parse('$baseUrl/api/v1/schemas/layouts/$id'),
      headers: {'Accept': 'application/json'},
    );
    if (response.statusCode >= 200 && response.statusCode < 300) {
      return LayoutModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
    } else {
      throw Exception('Failed to get layout: ${response.body}');
    }
  }

  Future<LayoutModel> createLayout(String name, {String? toId, Map<String, dynamic>? definition}) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/v1/schemas/layouts'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'name': name,
        'table_occurrence_id': toId ?? '',
        'definition': definition ?? {},
      }),
    );
    if (response.statusCode == 201) {
      return LayoutModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
    } else {
      throw Exception('Failed to create layout: ${response.body}');
    }
  }

  Future<LayoutModel> updateLayout(String id, String name, Map<String, dynamic> definition) async {
    final response = await _httpClient.put(
      Uri.parse('$baseUrl/api/v1/schemas/layouts/$id'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'name': name,
        'definition': definition,
      }),
    );
    if (response.statusCode >= 200 && response.statusCode < 300) {
      return LayoutModel.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
    } else {
      throw Exception('Failed to update layout: ${response.body}');
    }
  }

  Future<void> deleteLayout(String id) async {
    final response = await _httpClient.delete(Uri.parse('$baseUrl/api/v1/schemas/layouts/$id'));
    if (response.statusCode >= 200 && response.statusCode < 300) {
      return;
    } else {
      throw Exception('Failed to delete layout: ${response.body}');
    }
  }

  void close() {
    _httpClient.close();
  }
}

