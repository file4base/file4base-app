import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import 'models/layout_definition.dart';

class LayoutPreviewWidget extends StatefulWidget {
  final TableModel table;
  final ApiClient apiClient;
  final LayoutDefinitionModel layout;

  const LayoutPreviewWidget({
    super.key,
    required this.table,
    required this.apiClient,
    required this.layout,
  });

  @override
  State<LayoutPreviewWidget> createState() => _LayoutPreviewWidgetState();
}

class _LayoutPreviewWidgetState extends State<LayoutPreviewWidget> {
  List<Map<String, dynamic>> _records = [];
  bool _isLoading = true;

  @override
  void initState() {
    super.initState();
    _fetchRecords();
  }

  @override
  void didUpdateWidget(covariant LayoutPreviewWidget oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.table.id != widget.table.id) {
      _fetchRecords();
    }
  }

  Future<void> _fetchRecords() async {
    try {
      final rows = await widget.apiClient.listRows(widget.table.name);
      if (mounted) {
        setState(() {
          _records = rows;
          _isLoading = false;
        });
      }
    } catch (_) {
      if (mounted) setState(() => _isLoading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_isLoading) {
      return const Center(child: CircularProgressIndicator());
    }

    return Column(
      children: [
        // Preview control bar
        Container(
          height: 48,
          padding: const EdgeInsets.symmetric(horizontal: 16),
          color: Theme.of(context).colorScheme.surface,
          child: Row(
            children: [
              const Icon(Icons.print, size: 20, color: Colors.purple),
              const SizedBox(width: 8),
              Text(
                'Preview Mode: ${widget.layout.name}',
                style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
              ),
              const SizedBox(width: 16),
              Chip(
                label: Text('${_records.length} Records to Print', style: const TextStyle(fontSize: 11)),
                padding: EdgeInsets.zero,
              ),
              const Spacer(),
              FilledButton.tonalIcon(
                icon: const Icon(Icons.picture_as_pdf, size: 16),
                label: const Text('Export PDF / Print', style: TextStyle(fontSize: 12)),
                onPressed: () {
                  ScaffoldMessenger.of(context).showSnackBar(
                    const SnackBar(content: Text('Print / PDF Export simulated successfully.')),
                  );
                },
              ),
            ],
          ),
        ),
        const Divider(height: 1),
        // Document Page Preview (Simulating Sheet paper)
        Expanded(
          child: Container(
            color: Theme.of(context).colorScheme.surfaceContainerLowest,
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(32),
              child: Center(
                child: Container(
                  width: 800,
                  decoration: BoxDecoration(
                    color: Colors.white,
                    boxShadow: const [
                      BoxShadow(color: Colors.black26, blurRadius: 16, offset: Offset(0, 6)),
                    ],
                    border: Border.all(color: Colors.grey.shade300),
                  ),
                  padding: const EdgeInsets.symmetric(horizontal: 48, vertical: 40),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      // Header part
                      _buildHeader(context),
                      const SizedBox(height: 16),
                      const Divider(thickness: 1.5),
                      const SizedBox(height: 16),
                      // Records body
                      if (_records.isEmpty)
                        const Padding(
                          padding: EdgeInsets.all(32.0),
                          child: Center(child: Text('No records in table found to preview.', style: TextStyle(color: Colors.grey))),
                        )
                      else
                        _buildRecordTable(context),
                      const SizedBox(height: 32),
                      const Divider(),
                      // Footer part
                      _buildFooter(context),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildHeader(BuildContext context) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              widget.table.displayName,
              style: const TextStyle(fontSize: 22, fontWeight: FontWeight.bold, color: Colors.black87),
            ),
            const SizedBox(height: 4),
            Text(
              'Layout: ${widget.layout.name} | Occurrence: ${widget.layout.tableOccurrence}',
              style: TextStyle(fontSize: 12, color: Colors.grey.shade600),
            ),
          ],
        ),
        Image.asset(
          'assets/branding/file4base-icon-64.png',
          width: 36,
          height: 36,
          errorBuilder: (_, __, ___) => const Icon(Icons.table_chart, size: 36, color: Colors.blue),
        ),
      ],
    );
  }

  Widget _buildRecordTable(BuildContext context) {
    final columns = widget.table.columns.where((c) => !c.isPrimaryKey).toList();

    return Table(
      border: TableBorder(
        horizontalInside: BorderSide(color: Colors.grey.shade200),
        bottom: BorderSide(color: Colors.grey.shade400),
      ),
      children: [
        // Table Header
        TableRow(
          decoration: BoxDecoration(color: Colors.grey.shade100),
          children: [
            const Padding(
              padding: EdgeInsets.all(8.0),
              child: Text('#', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.black87)),
            ),
            ...columns.map((c) => Padding(
                  padding: const EdgeInsets.all(8.0),
                  child: Text(c.displayName, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: Colors.black87)),
                )),
          ],
        ),
        // Record Rows
        ..._records.asMap().entries.map((entry) {
          final idx = entry.key + 1;
          final row = entry.value;
          return TableRow(
            children: [
              Padding(
                padding: const EdgeInsets.all(8.0),
                child: Text('$idx', style: const TextStyle(fontSize: 12, color: Colors.black54)),
              ),
              ...columns.map((c) {
                final val = row[c.name]?.toString() ?? '';
                return Padding(
                  padding: const EdgeInsets.all(8.0),
                  child: Text(val, style: const TextStyle(fontSize: 12, color: Colors.black87)),
                );
              }),
            ],
          );
        }),
      ],
    );
  }

  Widget _buildFooter(BuildContext context) {
    final now = DateTime.now();
    final dateStr = '${now.year}-${now.month.toString().padLeft(2, '0')}-${now.day.toString().padLeft(2, '0')}';

    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Text('Generated on $dateStr by File4Base', style: TextStyle(fontSize: 11, color: Colors.grey.shade500)),
        Text('Page 1 of 1', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey.shade600)),
      ],
    );
  }
}
