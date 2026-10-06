import 'package:flutter/material.dart';
import '../../core/models/page_setup_model.dart';

class PageSetupDialog extends StatefulWidget {
  final PageSetupModel initialPageSetup;

  const PageSetupDialog({
    super.key,
    required this.initialPageSetup,
  });

  static Future<PageSetupModel?> show(
    BuildContext context, {
    required PageSetupModel initialPageSetup,
  }) {
    return showDialog<PageSetupModel>(
      context: context,
      barrierDismissible: true,
      builder: (ctx) => PageSetupDialog(initialPageSetup: initialPageSetup),
    );
  }

  @override
  State<PageSetupDialog> createState() => _PageSetupDialogState();
}

class _PageSetupDialogState extends State<PageSetupDialog> {
  late String _printer;
  late String _paperSizeName;
  late double _paperWidthMm;
  late double _paperHeightMm;
  late bool _isLandscape;

  late TextEditingController _marginTopCtrl;
  late TextEditingController _marginBottomCtrl;
  late TextEditingController _marginLeftCtrl;
  late TextEditingController _marginRightCtrl;

  static const Map<String, ({double width, double height, String desc})> _standardSizes = {
    'A4': (width: 210.0, height: 297.0, desc: 'A4 (210 × 297 mm)'),
    'US Letter': (width: 215.9, height: 279.4, desc: 'US Letter (8.5 × 11 in / 215.9 × 279.4 mm)'),
    'US Legal': (width: 215.9, height: 355.6, desc: 'US Legal (8.5 × 14 in / 215.9 × 355.6 mm)'),
    'A3': (width: 297.0, height: 420.0, desc: 'A3 (297 × 420 mm)'),
    'A5': (width: 148.0, height: 210.0, desc: 'A5 (148 × 210 mm)'),
    'B5': (width: 176.0, height: 250.0, desc: 'B5 (176 × 250 mm)'),
    'Custom': (width: 210.0, height: 297.0, desc: 'Custom...'),
  };

  static const List<String> _printers = [
    'Any Printer',
    'System Default Printer',
    'PDF Document Writer',
    'Network Office Printer',
  ];

  @override
  void initState() {
    super.initState();
    final p = widget.initialPageSetup;
    _printer = p.printer;
    _paperSizeName = _standardSizes.containsKey(p.paperSizeName) ? p.paperSizeName : 'A4';
    _paperWidthMm = p.paperWidthMm;
    _paperHeightMm = p.paperHeightMm;
    _isLandscape = p.isLandscape;

    _marginTopCtrl = TextEditingController(text: p.marginTopMm.toStringAsFixed(1));
    _marginBottomCtrl = TextEditingController(text: p.marginBottomMm.toStringAsFixed(1));
    _marginLeftCtrl = TextEditingController(text: p.marginLeftMm.toStringAsFixed(1));
    _marginRightCtrl = TextEditingController(text: p.marginRightMm.toStringAsFixed(1));
  }

  @override
  void dispose() {
    _marginTopCtrl.dispose();
    _marginBottomCtrl.dispose();
    _marginLeftCtrl.dispose();
    _marginRightCtrl.dispose();
    super.dispose();
  }

  void _onPaperSizeChanged(String? name) {
    if (name == null) return;
    setState(() {
      _paperSizeName = name;
      if (name != 'Custom' && _standardSizes.containsKey(name)) {
        _paperWidthMm = _standardSizes[name]!.width;
        _paperHeightMm = _standardSizes[name]!.height;
      }
    });
  }

  double _parseMargin(TextEditingController ctrl, double fallback) {
    final v = double.tryParse(ctrl.text.replaceAll(',', '.'));
    return (v != null && v >= 0) ? v : fallback;
  }

  PageSetupModel _getCurrentModel() {
    return PageSetupModel(
      printer: _printer,
      paperSizeName: _paperSizeName,
      paperWidthMm: _paperWidthMm,
      paperHeightMm: _paperHeightMm,
      isLandscape: _isLandscape,
      marginTopMm: _parseMargin(_marginTopCtrl, 15.0),
      marginBottomMm: _parseMargin(_marginBottomCtrl, 15.0),
      marginLeftMm: _parseMargin(_marginLeftCtrl, 15.0),
      marginRightMm: _parseMargin(_marginRightCtrl, 15.0),
    );
  }

  void _restoreDefaults() {
    setState(() {
      _printer = 'Any Printer';
      _paperSizeName = 'A4';
      _paperWidthMm = 210.0;
      _paperHeightMm = 297.0;
      _isLandscape = false;
      _marginTopCtrl.text = '15.0';
      _marginBottomCtrl.text = '15.0';
      _marginLeftCtrl.text = '15.0';
      _marginRightCtrl.text = '15.0';
    });
  }

  @override
  Widget build(BuildContext context) {
    final current = _getCurrentModel();

    return Dialog(
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 760, maxHeight: 680),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // Header
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 16),
              child: Row(
                children: [
                  const Icon(Icons.print_outlined, color: Color(0xFF1E88E5), size: 24),
                  const SizedBox(width: 10),
                  const Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('Page Setup', style: TextStyle(fontSize: 17, fontWeight: FontWeight.bold)),
                        Text('Set the printer, paper size, orientation, and margins for layouts and reports', style: TextStyle(fontSize: 12, color: Colors.grey)),
                      ],
                    ),
                  ),
                  IconButton(
                    onPressed: () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close, size: 20),
                    tooltip: 'Close',
                  ),
                ],
              ),
            ),
            const Divider(height: 1),

            // Content
            Expanded(
              child: SingleChildScrollView(
                padding: const EdgeInsets.all(20),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    // Left Column: Configuration Controls
                    Expanded(
                      flex: 6,
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          // 1. Printer selection
                          const Text('Printer:', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                          const SizedBox(height: 6),
                          DropdownButtonFormField<String>(
                            value: _printers.contains(_printer) ? _printer : _printers.first,
                            isExpanded: true,
                            decoration: const InputDecoration(
                              isDense: true,
                              border: OutlineInputBorder(),
                              helperText: 'Any Printer is recommended for compatibility across computers.',
                            ),
                            items: _printers.map((p) {
                              final label = p == 'Any Printer'
                                  ? 'Any Printer (Recommended)'
                                  : p == 'System Default Printer'
                                      ? 'System Default Printer'
                                      : p == 'PDF Document Writer'
                                          ? 'PDF Document Writer'
                                          : 'Network Office Printer';
                              return DropdownMenuItem(value: p, child: Text(label, style: const TextStyle(fontSize: 12), overflow: TextOverflow.ellipsis));
                            }).toList(),
                            onChanged: (val) {
                              if (val != null) setState(() => _printer = val);
                            },
                          ),
                          const SizedBox(height: 16),

                          // 2. Paper size
                          const Text('Paper size:', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                          const SizedBox(height: 6),
                          DropdownButtonFormField<String>(
                            value: _paperSizeName,
                            isExpanded: true,
                            decoration: const InputDecoration(
                              isDense: true,
                              border: OutlineInputBorder(),
                            ),
                            items: _standardSizes.entries.map((e) {
                              return DropdownMenuItem(value: e.key, child: Text(e.value.desc, style: const TextStyle(fontSize: 12), overflow: TextOverflow.ellipsis));
                            }).toList(),
                            onChanged: _onPaperSizeChanged,
                          ),
                          if (_paperSizeName == 'Custom') ...[
                            const SizedBox(height: 8),
                            Row(
                              children: [
                                Expanded(
                                  child: TextFormField(
                                    initialValue: _paperWidthMm.toStringAsFixed(1),
                                    decoration: const InputDecoration(
                                      labelText: 'Width (mm)',
                                      isDense: true,
                                      border: OutlineInputBorder(),
                                    ),
                                    keyboardType: TextInputType.number,
                                    onChanged: (v) {
                                      final val = double.tryParse(v);
                                      if (val != null && val > 0) setState(() => _paperWidthMm = val);
                                    },
                                  ),
                                ),
                                const SizedBox(width: 8),
                                Expanded(
                                  child: TextFormField(
                                    initialValue: _paperHeightMm.toStringAsFixed(1),
                                    decoration: const InputDecoration(
                                      labelText: 'Height (mm)',
                                      isDense: true,
                                      border: OutlineInputBorder(),
                                    ),
                                    keyboardType: TextInputType.number,
                                    onChanged: (v) {
                                      final val = double.tryParse(v);
                                      if (val != null && val > 0) setState(() => _paperHeightMm = val);
                                    },
                                  ),
                                ),
                              ],
                            ),
                          ],
                          const SizedBox(height: 16),

                          // 3. Orientation
                          const Text('Orientation:', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                          const SizedBox(height: 8),
                          Row(
                            children: [
                              Expanded(
                                child: InkWell(
                                  onTap: () => setState(() => _isLandscape = false),
                                  borderRadius: BorderRadius.circular(8),
                                  child: Container(
                                    padding: const EdgeInsets.symmetric(vertical: 12, horizontal: 8),
                                    decoration: BoxDecoration(
                                      borderRadius: BorderRadius.circular(8),
                                      border: Border.all(
                                        color: !_isLandscape ? const Color(0xFF1E88E5) : Colors.grey.withValues(alpha: 0.3),
                                        width: !_isLandscape ? 2 : 1,
                                      ),
                                      color: !_isLandscape ? const Color(0xFF1E88E5).withValues(alpha: 0.08) : null,
                                    ),
                                    child: Row(
                                      mainAxisAlignment: MainAxisAlignment.center,
                                      children: [
                                        Icon(
                                          Icons.crop_portrait,
                                          color: !_isLandscape ? const Color(0xFF1E88E5) : Colors.grey,
                                          size: 20,
                                        ),
                                        const SizedBox(width: 6),
                                        Expanded(
                                          child: Text(
                                            'Portrait',
                                            style: TextStyle(
                                              fontSize: 11,
                                              fontWeight: !_isLandscape ? FontWeight.bold : FontWeight.normal,
                                              color: !_isLandscape ? const Color(0xFF1E88E5) : null,
                                            ),
                                          ),
                                        ),
                                      ],
                                    ),
                                  ),
                                ),
                              ),
                              const SizedBox(width: 12),
                              Expanded(
                                child: InkWell(
                                  onTap: () => setState(() => _isLandscape = true),
                                  borderRadius: BorderRadius.circular(8),
                                  child: Container(
                                    padding: const EdgeInsets.symmetric(vertical: 10, horizontal: 6),
                                    decoration: BoxDecoration(
                                      borderRadius: BorderRadius.circular(8),
                                      border: Border.all(
                                        color: _isLandscape ? const Color(0xFF1E88E5) : Colors.grey.withValues(alpha: 0.3),
                                        width: _isLandscape ? 2 : 1,
                                      ),
                                      color: _isLandscape ? const Color(0xFF1E88E5).withValues(alpha: 0.08) : null,
                                    ),
                                    child: Row(
                                      mainAxisAlignment: MainAxisAlignment.center,
                                      children: [
                                        Icon(
                                          Icons.crop_landscape,
                                          color: _isLandscape ? const Color(0xFF1E88E5) : Colors.grey,
                                          size: 20,
                                        ),
                                        const SizedBox(width: 6),
                                        Expanded(
                                          child: Text(
                                            'Landscape',
                                            style: TextStyle(
                                              fontSize: 11,
                                              fontWeight: _isLandscape ? FontWeight.bold : FontWeight.normal,
                                              color: _isLandscape ? const Color(0xFF1E88E5) : null,
                                            ),
                                          ),
                                        ),
                                      ],
                                    ),
                                  ),
                                ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 16),

                          // 4. Margin calculation (mm)
                          const Text('Margins (mm):', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                          const SizedBox(height: 8),
                          Row(
                            children: [
                              Expanded(
                                child: TextField(
                                  controller: _marginTopCtrl,
                                  keyboardType: const TextInputType.numberWithOptions(decimal: true),
                                  decoration: const InputDecoration(
                                    labelText: 'Top',
                                    suffixText: 'mm',
                                    isDense: true,
                                    border: OutlineInputBorder(),
                                  ),
                                  onChanged: (_) => setState(() {}),
                                ),
                              ),
                              const SizedBox(width: 8),
                              Expanded(
                                child: TextField(
                                  controller: _marginBottomCtrl,
                                  keyboardType: const TextInputType.numberWithOptions(decimal: true),
                                  decoration: const InputDecoration(
                                    labelText: 'Bottom',
                                    suffixText: 'mm',
                                    isDense: true,
                                    border: OutlineInputBorder(),
                                  ),
                                  onChanged: (_) => setState(() {}),
                                ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 8),
                          Row(
                            children: [
                              Expanded(
                                child: TextField(
                                  controller: _marginLeftCtrl,
                                  keyboardType: const TextInputType.numberWithOptions(decimal: true),
                                  decoration: const InputDecoration(
                                    labelText: 'Left',
                                    suffixText: 'mm',
                                    isDense: true,
                                    border: OutlineInputBorder(),
                                  ),
                                  onChanged: (_) => setState(() {}),
                                ),
                              ),
                              const SizedBox(width: 8),
                              Expanded(
                                child: TextField(
                                  controller: _marginRightCtrl,
                                  keyboardType: const TextInputType.numberWithOptions(decimal: true),
                                  decoration: const InputDecoration(
                                    labelText: 'Right',
                                    suffixText: 'mm',
                                    isDense: true,
                                    border: OutlineInputBorder(),
                                  ),
                                  onChanged: (_) => setState(() {}),
                                ),
                              ),
                            ],
                          ),
                        ],
                      ),
                    ),

                    const SizedBox(width: 24),

                    // Right Column: Live Schematic & Printable Area Calculation
                    Expanded(
                      flex: 5,
                      child: Container(
                        padding: const EdgeInsets.all(16),
                        decoration: BoxDecoration(
                          color: Theme.of(context).colorScheme.surfaceContainerHighest.withValues(alpha: 0.3),
                          borderRadius: BorderRadius.circular(10),
                          border: Border.all(color: Colors.grey.withValues(alpha: 0.25)),
                        ),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            const Row(
                              children: [
                                Icon(Icons.analytics_outlined, color: Color(0xFF1E88E5), size: 18),
                                SizedBox(width: 6),
                                Expanded(
                                  child: Text(
                                    'Page Boundaries',
                                    style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
                                  ),
                                ),
                              ],
                            ),
                            const SizedBox(height: 12),

                            // Graphical representation of sheet
                            Center(
                              child: Container(
                                width: _isLandscape ? 170 : 120,
                                height: _isLandscape ? 120 : 170,
                                decoration: BoxDecoration(
                                  color: Colors.white,
                                  border: Border.all(color: Colors.grey.shade400, width: 1.5),
                                  boxShadow: const [
                                    BoxShadow(color: Colors.black12, blurRadius: 4, offset: Offset(0, 2)),
                                  ],
                                ),
                                child: Stack(
                                  children: [
                                    // Margin outline (printable boundary)
                                    Positioned(
                                      top: (current.marginTopMm / current.effectiveHeightMm * (_isLandscape ? 120 : 170)).clamp(2.0, 30.0),
                                      bottom: (current.marginBottomMm / current.effectiveHeightMm * (_isLandscape ? 120 : 170)).clamp(2.0, 30.0),
                                      left: (current.marginLeftMm / current.effectiveWidthMm * (_isLandscape ? 170 : 120)).clamp(2.0, 30.0),
                                      right: (current.marginRightMm / current.effectiveWidthMm * (_isLandscape ? 170 : 120)).clamp(2.0, 30.0),
                                      child: Container(
                                        decoration: BoxDecoration(
                                          color: const Color(0xFF1E88E5).withValues(alpha: 0.1),
                                          border: Border.all(color: const Color(0xFF1E88E5), width: 1, style: BorderStyle.solid),
                                        ),
                                        child: const Center(
                                          child: Text(
                                            'Printable area',
                                            style: TextStyle(fontSize: 9, color: Color(0xFF1E88E5), fontWeight: FontWeight.bold),
                                          ),
                                        ),
                                      ),
                                    ),
                                  ],
                                ),
                              ),
                            ),
                            const SizedBox(height: 16),

                            // Calculation metrics table
                            _buildMetricRow('Total dimensions:', '${current.effectiveWidthMm.toStringAsFixed(1)} × ${current.effectiveHeightMm.toStringAsFixed(1)} mm'),
                            _buildMetricRow('In PostScript points (pt):', '${current.totalWidthPt.toStringAsFixed(0)} × ${current.totalHeightPt.toStringAsFixed(0)} pt'),
                            const Divider(height: 12),
                            _buildMetricRow('Printable width:', '${current.printableWidthMm.toStringAsFixed(1)} mm (${current.printableWidthPt.toStringAsFixed(0)} pt)', isBold: true),
                            _buildMetricRow('Printable height:', '${current.printableHeightMm.toStringAsFixed(1)} mm (${current.printableHeightPt.toStringAsFixed(0)} pt)', isBold: true),
                            const SizedBox(height: 12),

                            Container(
                              padding: const EdgeInsets.all(8),
                              decoration: BoxDecoration(
                                color: Colors.blue.withValues(alpha: 0.08),
                                borderRadius: BorderRadius.circular(6),
                              ),
                              child: const Text(
                                'File4Base uses these settings to calculate page boundaries, section breaks, and printable width in layouts and reports.',
                                style: TextStyle(fontSize: 11, height: 1.3, color: Colors.blueGrey),
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),

            const Divider(height: 1),

            // Footer actions
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
              child: Row(
                children: [
                  TextButton.icon(
                    onPressed: _restoreDefaults,
                    icon: const Icon(Icons.restore, size: 16),
                    label: const Text('Defaults', style: TextStyle(fontSize: 12)),
                  ),
                  const Spacer(),
                  OutlinedButton(
                    onPressed: () => Navigator.of(context).pop(),
                    child: const Text('Cancel'),
                  ),
                  const SizedBox(width: 12),
                  FilledButton(
                    onPressed: () => Navigator.of(context).pop(current),
                    style: FilledButton.styleFrom(backgroundColor: const Color(0xFF1E88E5)),
                    child: const Text('OK'),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildMetricRow(String label, String value, {bool isBold = false}) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 3),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            flex: 5,
            child: Text(
              label,
              style: TextStyle(
                fontSize: 11,
                color: isBold ? Colors.black87 : Colors.grey.shade700,
              ),
            ),
          ),
          const SizedBox(width: 8),
          Expanded(
            flex: 6,
            child: Text(
              value,
              textAlign: TextAlign.end,
              style: TextStyle(
                fontSize: 11,
                fontWeight: isBold ? FontWeight.bold : FontWeight.w500,
                color: isBold ? const Color(0xFF1E88E5) : null,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
