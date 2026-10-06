import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import '../../core/models/page_setup_model.dart';
import '../../core/services/solution_storage.dart';
import 'layout_object_visuals.dart';
import 'models/layout_definition.dart';

class LayoutPreviewWidget extends StatefulWidget {
  final TableModel table;
  final ApiClient apiClient;
  final LayoutDefinitionModel layout;
  final PageSetupModel pageSetup;
  final VoidCallback? onPageSetup;
  final String? currentUserName;

  const LayoutPreviewWidget({
    super.key,
    required this.table,
    required this.apiClient,
    required this.layout,
    this.pageSetup = const PageSetupModel(),
    this.onPageSetup,
    this.currentUserName,
  });

  @override
  State<LayoutPreviewWidget> createState() => _LayoutPreviewWidgetState();
}

class _LayoutPreviewWidgetState extends State<LayoutPreviewWidget> {
  List<Map<String, dynamic>> _records = [];
  bool _isLoading = true;
  int _currentRecordIndex = 0;

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
          if (_currentRecordIndex >= rows.length && rows.isNotEmpty) {
            _currentRecordIndex = rows.length - 1;
          }
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
          color: Theme.of(context).colorScheme.surface,
          child: SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Row(
              mainAxisSize: MainAxisSize.min,
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
                if (_records.length > 1) ...[
                  const SizedBox(width: 8),
                  IconButton(
                    icon: const Icon(Icons.navigate_before, size: 18),
                    tooltip: 'Previous Record',
                    onPressed: _currentRecordIndex > 0
                        ? () => setState(() => _currentRecordIndex--)
                        : null,
                  ),
                  Text(
                    'Record ${_currentRecordIndex + 1} of ${_records.length}',
                    style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 11),
                  ),
                  IconButton(
                    icon: const Icon(Icons.navigate_next, size: 18),
                    tooltip: 'Next Record',
                    onPressed: _currentRecordIndex < _records.length - 1
                        ? () => setState(() => _currentRecordIndex++)
                        : null,
                  ),
                ],
                const SizedBox(width: 8),
                Chip(
                  avatar: Icon(
                    widget.pageSetup.isLandscape ? Icons.crop_landscape : Icons.crop_portrait,
                    size: 14,
                    color: const Color(0xFF1E88E5),
                  ),
                  label: Text(
                    '${widget.pageSetup.paperSizeName} (${widget.pageSetup.isLandscape ? "Landscape" : "Portrait"}) - ${widget.pageSetup.printableWidthMm.toStringAsFixed(0)}×${widget.pageSetup.printableHeightMm.toStringAsFixed(0)} mm',
                    style: const TextStyle(fontSize: 11),
                  ),
                  padding: EdgeInsets.zero,
                ),
                const SizedBox(width: 16),
                OutlinedButton.icon(
                  icon: const Icon(Icons.settings_overscan, size: 16),
                  label: const Text('Page Setup...', style: TextStyle(fontSize: 12)),
                  onPressed: widget.onPageSetup,
                ),
                const SizedBox(width: 8),
                FilledButton.tonalIcon(
                  icon: const Icon(Icons.picture_as_pdf, size: 16),
                  label: const Text('Export PDF / Print', style: TextStyle(fontSize: 12)),
                  onPressed: () {
                    SolutionStorageService.triggerPrint();
                  },
                ),
              ],
            ),
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
                  width: widget.pageSetup.totalWidthPt.clamp(500.0, 1200.0),
                  constraints: BoxConstraints(
                    minHeight: widget.pageSetup.totalHeightPt.clamp(600.0, 1600.0),
                  ),
                  decoration: BoxDecoration(
                    color: Colors.white,
                    boxShadow: const [
                      BoxShadow(color: Colors.black26, blurRadius: 16, offset: Offset(0, 6)),
                    ],
                    border: Border.all(color: Colors.grey.shade300),
                  ),
                  padding: EdgeInsets.fromLTRB(
                    widget.pageSetup.marginLeftPt.clamp(10.0, 100.0),
                    widget.pageSetup.marginTopPt.clamp(10.0, 100.0),
                    widget.pageSetup.marginRightPt.clamp(10.0, 100.0),
                    widget.pageSetup.marginBottomPt.clamp(10.0, 100.0),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      // Header part
                      _buildHeader(context),
                      const SizedBox(height: 16),
                      const Divider(thickness: 1.5),
                      const SizedBox(height: 16),
                      // Records body: Render exact layout if objects exist, else fallback to tabular
                      if (widget.layout.objects.isNotEmpty)
                        _buildLayoutPreviewCanvas(context)
                      else if (_records.isEmpty)
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

  Widget _buildLayoutPreviewCanvas(BuildContext context) {
    final record = _records.isNotEmpty
        ? _records[_currentRecordIndex]
        : <String, dynamic>{};

    double maxObjY = 300.0;
    for (final obj in widget.layout.objects) {
      final bottom = obj.y + obj.height;
      if (bottom > maxObjY) maxObjY = bottom;
    }
    final canvasHeight = maxObjY + 40.0;
    final canvasWidth = widget.layout.width;

    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Container(
        width: canvasWidth,
        height: canvasHeight,
        decoration: BoxDecoration(
          color: Colors.white,
          borderRadius: BorderRadius.circular(4),
          border: Border.all(color: Colors.grey.shade300, width: 0.8),
        ),
        child: Stack(
          clipBehavior: Clip.none,
          children: [
            ...widget.layout.objects.map((obj) {
              return Positioned(
                left: obj.x,
                top: obj.y,
                width: obj.width,
                height: obj.height,
                child: _buildPreviewLayoutObject(obj, record),
              );
            }),
          ],
        ),
      ),
    );
  }

  Widget _buildPreviewLayoutObject(LayoutObjectModel obj, Map<String, dynamic> record) {
    switch (obj.type) {
      case 'label':
        return Container(
          alignment: _parseAlignment(obj.style.textAlign),
          padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
          child: Text(
            resolveLayoutMergeText(obj.text,
                record: record, userName: widget.currentUserName, pageNumber: _currentRecordIndex + 1),
            textAlign: _parseTextAlign(obj.style.textAlign),
            style: TextStyle(
              fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 13,
              fontWeight: obj.style.fontWeight == 'bold' ? FontWeight.bold : FontWeight.normal,
              color: obj.style.textColor != null ? _parseColor(obj.style.textColor!) : Colors.black87,
            ),
          ),
        );

      case 'field':
        final fieldName = obj.fieldBinding?.fieldName ?? obj.text;
        final col = widget.table.columns
            .where((c) => c.name.toLowerCase() == fieldName.toLowerCase())
            .firstOrNull;

        final rawVal = col != null ? (record[col.name]?.toString() ?? '') : '';
        return Container(
          alignment: _parseAlignment(obj.style.textAlign),
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
          decoration: BoxDecoration(
            color: obj.style.fillColor != null ? _parseColor(obj.style.fillColor!) : Colors.grey.shade50,
            borderRadius: BorderRadius.circular(obj.style.cornerRadius),
            border: Border.all(
              color: obj.style.borderColor != null ? _parseColor(obj.style.borderColor!) : Colors.grey.shade400,
              width: obj.style.borderWidth,
            ),
          ),
          child: Text(
            rawVal.isNotEmpty ? rawVal : (col == null ? '<$fieldName>' : ''),
            textAlign: _parseTextAlign(obj.style.textAlign),
            style: TextStyle(
              fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 13,
              fontWeight: obj.style.fontWeight == 'bold' ? FontWeight.bold : FontWeight.normal,
              color: col == null ? Colors.amber.shade800 : Colors.black87,
              fontFamily: col?.isPrimaryKey == true ? 'monospace' : null,
            ),
            overflow: TextOverflow.ellipsis,
          ),
        );

      case 'button':
      case 'popover_button':
        final btnText = resolveLayoutMergeText(obj.text.isEmpty ? 'Button' : obj.text,
            record: record, userName: widget.currentUserName);
        return Container(
          alignment: Alignment.center,
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
          decoration: BoxDecoration(
            color: obj.style.fillColor != null ? _parseColor(obj.style.fillColor!) : Colors.grey.shade200,
            borderRadius: BorderRadius.circular(obj.style.cornerRadius),
            border: Border.all(
              color: obj.style.borderColor != null ? _parseColor(obj.style.borderColor!) : Colors.grey.shade400,
              width: obj.style.borderWidth,
            ),
          ),
          child: Text(
            btnText,
            style: TextStyle(
              fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 12,
              fontWeight: FontWeight.w600,
              color: parseLayoutColor(obj.style.textColor) ?? Colors.black87,
            ),
            overflow: TextOverflow.ellipsis,
          ),
        );

      case 'portal':
        return Container(
          decoration: BoxDecoration(
            color: Colors.grey.shade50,
            border: Border.all(color: Colors.grey.shade400),
            borderRadius: BorderRadius.circular(obj.style.cornerRadius),
          ),
          padding: const EdgeInsets.all(8),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  const Icon(Icons.table_rows, size: 14, color: Color(0xFF1E88E5)),
                  const SizedBox(width: 4),
                  Text(
                    obj.text.isEmpty ? 'Portal / Related Records' : obj.text,
                    style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.black87),
                  ),
                ],
              ),
              const Divider(height: 12),
              const Expanded(
                child: Center(
                  child: Text('Print Preview: Related Records',
                      style: TextStyle(fontSize: 10, color: Colors.black54)),
                ),
              ),
            ],
          ),
        );

      default:
        final drawn = buildDrawnLayoutObject(obj,
            record: record, userName: widget.currentUserName, pageNumber: _currentRecordIndex + 1);
        if (drawn != null) return drawn;
        return Container(
          decoration: BoxDecoration(
            color: obj.style.fillColor != null ? _parseColor(obj.style.fillColor!) : Colors.transparent,
            border: Border.all(
              color: obj.style.borderColor != null ? _parseColor(obj.style.borderColor!) : Colors.grey.shade400,
              width: obj.style.borderWidth,
            ),
            borderRadius: BorderRadius.circular(obj.style.cornerRadius),
          ),
        );
    }
  }

  Alignment _parseAlignment(String align) {
    switch (align) {
      case 'center':
        return Alignment.center;
      case 'right':
        return Alignment.centerRight;
      default:
        return Alignment.centerLeft;
    }
  }

  TextAlign _parseTextAlign(String align) {
    switch (align) {
      case 'center':
        return TextAlign.center;
      case 'right':
        return TextAlign.right;
      default:
        return TextAlign.left;
    }
  }

  Color _parseColor(String colorStr, [Color fallback = Colors.black87]) {
    try {
      if (colorStr.startsWith('#')) {
        final hex = colorStr.substring(1);
        if (hex.length == 6) return Color(int.parse('0xFF$hex'));
        if (hex.length == 8) return Color(int.parse('0x$hex'));
      }
    } catch (_) {}
    return fallback;
  }

  Widget _buildHeader(BuildContext context) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                widget.table.displayName,
                style: const TextStyle(fontSize: 22, fontWeight: FontWeight.bold, color: Colors.black87),
                overflow: TextOverflow.ellipsis,
              ),
              const SizedBox(height: 4),
              Text(
                'Layout: ${widget.layout.name} | Occurrence: ${widget.layout.tableOccurrence}',
                style: TextStyle(fontSize: 12, color: Colors.grey.shade600),
                overflow: TextOverflow.ellipsis,
              ),
            ],
          ),
        ),
        const SizedBox(width: 16),
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
        Expanded(
          child: Text(
            'Generated on $dateStr by File4Base',
            style: TextStyle(fontSize: 11, color: Colors.grey.shade500),
            overflow: TextOverflow.ellipsis,
          ),
        ),
        const SizedBox(width: 8),
        Text(
          _records.isNotEmpty ? 'Record ${_currentRecordIndex + 1} of ${_records.length}' : 'Page 1 of 1',
          style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.grey.shade600),
        ),
      ],
    );
  }
}

