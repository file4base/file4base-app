import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

class ServerConnectionDialog extends StatefulWidget {
  final String currentUrl;
  final ValueChanged<String> onConnect;

  const ServerConnectionDialog({
    super.key,
    required this.currentUrl,
    required this.onConnect,
  });

  static Future<void> show(BuildContext context, {
    required String currentUrl,
    required ValueChanged<String> onConnect,
  }) async {
    await showDialog(
      context: context,
      builder: (context) => ServerConnectionDialog(
        currentUrl: currentUrl,
        onConnect: onConnect,
      ),
    );
  }

  @override
  State<ServerConnectionDialog> createState() => _ServerConnectionDialogState();
}

class _ServerConnectionDialogState extends State<ServerConnectionDialog> {
  late TextEditingController _hostController;
  late TextEditingController _portController;
  String _protocol = 'http';
  bool _isTesting = false;
  String? _testResult;
  bool _testSuccess = false;

  @override
  void initState() {
    super.initState();
    final uri = Uri.tryParse(widget.currentUrl) ?? Uri.parse('http://localhost:8080');
    _protocol = uri.scheme.isNotEmpty ? uri.scheme : 'http';
    _hostController = TextEditingController(text: uri.host.isNotEmpty ? uri.host : 'localhost');
    _portController = TextEditingController(text: uri.hasPort ? uri.port.toString() : '8080');
  }

  @override
  void dispose() {
    _hostController.dispose();
    _portController.dispose();
    super.dispose();
  }

  String get _constructedUrl {
    final host = _hostController.text.trim();
    final port = _portController.text.trim();
    if (port.isEmpty) {
      return '$_protocol://$host';
    }
    return '$_protocol://$host:$port';
  }

  Future<void> _testConnection() async {
    setState(() {
      _isTesting = true;
      _testResult = null;
    });

    final targetUrl = _constructedUrl;
    try {
      final response = await http
          .get(Uri.parse('$targetUrl/healthz'))
          .timeout(const Duration(seconds: 4));

      if (response.statusCode >= 200 && response.statusCode < 300) {
        final data = jsonDecode(response.body) as Map<String, dynamic>;
        setState(() {
          _isTesting = false;
          _testSuccess = true;
          _testResult = 'Connection successful! Engine: ${data['engine'] ?? 'PostgreSQL'}';
        });
      } else {
        setState(() {
          _isTesting = false;
          _testSuccess = false;
          _testResult = 'Server returned HTTP ${response.statusCode}';
        });
      }
    } catch (e) {
      setState(() {
        _isTesting = false;
        _testSuccess = false;
        _testResult = 'Cannot reach server at $targetUrl.\n'
            '• Check if File4Base Server or Docker is started.\n'
            '• Verify port is exposed and not blocked by firewall.';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Row(
        children: [
          Icon(Icons.dns, size: 24, color: Color(0xFF1E88E5)),
          SizedBox(width: 10),
          Text('Server Host & Port Settings', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
        ],
      ),
      content: SizedBox(
        width: 460,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Configure the File4Base Server connection. By default, desktop clients connect to localhost:8080.',
              style: TextStyle(fontSize: 12, color: Colors.grey),
            ),
            const SizedBox(height: 16),
            Row(
              children: [
                SizedBox(
                  width: 90,
                  child: DropdownButtonFormField<String>(
                    value: _protocol,
                    decoration: const InputDecoration(labelText: 'Proto', isDense: true),
                    items: const [
                      DropdownMenuItem(value: 'http', child: Text('http://')),
                      DropdownMenuItem(value: 'https', child: Text('https://')),
                    ],
                    onChanged: (val) {
                      if (val != null) setState(() => _protocol = val);
                    },
                  ),
                ),
                const SizedBox(width: 8),
                Expanded(
                  flex: 3,
                  child: TextField(
                    controller: _hostController,
                    decoration: const InputDecoration(
                      labelText: 'Server Host / IP',
                      hintText: 'localhost',
                      isDense: true,
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                Expanded(
                  flex: 2,
                  child: TextField(
                    controller: _portController,
                    decoration: const InputDecoration(
                      labelText: 'Port',
                      hintText: '8080',
                      isDense: true,
                    ),
                    keyboardType: TextInputType.number,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),
            Row(
              children: [
                OutlinedButton.icon(
                  icon: _isTesting
                      ? const SizedBox(width: 14, height: 14, child: CircularProgressIndicator(strokeWidth: 2))
                      : const Icon(Icons.network_check, size: 16),
                  label: const Text('Test Connection', style: TextStyle(fontSize: 12)),
                  onPressed: _isTesting ? null : _testConnection,
                ),
                const Spacer(),
                TextButton(
                  child: const Text('Reset to Default', style: TextStyle(fontSize: 12)),
                  onPressed: () {
                    setState(() {
                      _protocol = 'http';
                      _hostController.text = 'localhost';
                      _portController.text = '8080';
                      _testResult = null;
                    });
                  },
                ),
              ],
            ),
            if (_testResult != null) ...[
              const SizedBox(height: 12),
              Container(
                width: double.infinity,
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: _testSuccess ? Colors.green.withOpacity(0.1) : Colors.orange.withOpacity(0.1),
                  borderRadius: BorderRadius.circular(8),
                  border: Border.all(
                    color: _testSuccess ? Colors.green.withOpacity(0.4) : Colors.orange.withOpacity(0.4),
                  ),
                ),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(
                      _testSuccess ? Icons.check_circle : Icons.warning_amber_rounded,
                      size: 18,
                      color: _testSuccess ? Colors.green : Colors.orange.shade800,
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        _testResult!,
                        style: TextStyle(
                          fontSize: 11,
                          color: _testSuccess ? Colors.green.shade900 : Colors.orange.shade900,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: () {
            widget.onConnect(_constructedUrl);
            Navigator.of(context).pop();
          },
          child: const Text('Connect'),
        ),
      ],
    );
  }
}
