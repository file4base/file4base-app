import 'dart:convert';
import 'package:http/http.dart' as http;

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

  void close() {
    _httpClient.close();
  }
}
