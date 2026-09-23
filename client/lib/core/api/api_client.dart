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

  void close() {
    _httpClient.close();
  }
}
