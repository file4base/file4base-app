import 'package:flutter/material.dart';
import '../api/api_client.dart';
import '../../main.dart';

// ─── Layout Tool Enum ─────────────────────────────────────────────────────────

enum LayoutTool {
  pointer,
  text,
  line,
  rectangle,
  roundedRect,
  oval,
  field,
  part,
  button,
  portal,
  format,
  rotate,
}

// ─── Main Sidebar Widget (StatefulWidget for tool selection) ──────────────────

class File4BaseStatusSidebar extends StatefulWidget {
  final List<TableModel> tables;
  final TableModel? selectedTable;
  final ValueChanged<TableModel?> onTableSelected;
  final OperationalMode mode;
  final int currentRecordIndex;
  final int totalRecords;
  final bool isUnsorted;
  final VoidCallback onPreviousRecord;
  final VoidCallback onNextRecord;
  final ValueChanged<int> onGoToRecord;
  final VoidCallback onManageDatabase;
  final bool isFindOmit;
  final ValueChanged<bool>? onToggleOmit;
  final VoidCallback? onPerformFind;
  final VoidCallback? onShowAllRecords;
  final VoidCallback? onNewRecord;
  final VoidCallback? onDeleteRecord;
  final int layoutCount;
  final int currentLayoutIndex;
  final ValueChanged<int>? onLayoutChanged;

  const File4BaseStatusSidebar({
    super.key,
    required this.tables,
    required this.selectedTable,
    required this.onTableSelected,
    required this.mode,
    required this.currentRecordIndex,
    required this.totalRecords,
    this.isUnsorted = true,
    required this.onPreviousRecord,
    required this.onNextRecord,
    required this.onGoToRecord,
    required this.onManageDatabase,
    this.isFindOmit = false,
    this.onToggleOmit,
    this.onPerformFind,
    this.onShowAllRecords,
    this.onNewRecord,
    this.onDeleteRecord,
    this.layoutCount = 1,
    this.currentLayoutIndex = 0,
    this.onLayoutChanged,
  });

  @override
  State<File4BaseStatusSidebar> createState() => _File4BaseStatusSidebarState();
}

class _File4BaseStatusSidebarState extends State<File4BaseStatusSidebar> {
  LayoutTool _selectedTool = LayoutTool.pointer;
  double _strokeWidth = 1.0;

  @override
  Widget build(BuildContext context) {
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final bgColor = isDark ? const Color(0xFF1E232B) : const Color(0xFFEBE9E4);
    final borderColor =
        isDark ? const Color(0xFF38404B) : const Color(0xFFB8B5AE);

    return Container(
      width: 120,
      decoration: BoxDecoration(
        color: bgColor,
        border: Border(right: BorderSide(color: borderColor, width: 1.0)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _buildTableSelector(context, isDark, borderColor),
          const SizedBox(height: 8),
          if (widget.mode == OperationalMode.browse)
            _buildBrowseNavigator(context, isDark)
          else if (widget.mode == OperationalMode.find)
            _buildFindNavigator(context, isDark)
          else if (widget.mode == OperationalMode.layout)
            _buildLayoutToolbox(context, isDark, borderColor)
          else
            _buildPreviewNavigator(context, isDark),
          const Spacer(),
          _buildManageDatabaseButton(context, isDark),
        ],
      ),
    );
  }

  // ─── Table Selector ─────────────────────────────────────────────────────────

  Widget _buildTableSelector(
      BuildContext context, bool isDark, Color borderColor) {
    return Container(
      margin: const EdgeInsets.fromLTRB(6, 6, 6, 0),
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: isDark ? const Color(0xFF272D37) : Colors.white,
        borderRadius: BorderRadius.circular(3),
        border: Border.all(color: borderColor),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.05),
            blurRadius: 1,
            offset: const Offset(0, 1),
          ),
        ],
      ),
      child: DropdownButtonHideUnderline(
        child: DropdownButton<String>(
          value: widget.selectedTable?.id,
          isDense: true,
          isExpanded: true,
          icon: const Icon(Icons.arrow_drop_down, size: 16),
          style: TextStyle(
            fontSize: 11,
            fontWeight: FontWeight.w600,
            color: isDark ? Colors.white : Colors.black87,
          ),
          items: widget.tables.map((t) {
            return DropdownMenuItem<String>(
              value: t.id,
              child: Text(t.displayName,
                  maxLines: 1, overflow: TextOverflow.ellipsis),
            );
          }).toList(),
          onChanged: (newId) {
            if (newId != null) {
              final match = widget.tables.firstWhere((t) => t.id == newId);
              widget.onTableSelected(match);
            }
          },
        ),
      ),
    );
  }

  // ─── Browse Mode ─────────────────────────────────────────────────────────────

  Widget _buildBrowseNavigator(BuildContext context, bool isDark) {
    final recordNumber =
        widget.totalRecords > 0 ? widget.currentRecordIndex + 1 : 0;

    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 8.0),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          Tooltip(
            message: 'Click top half for previous, bottom half for next',
            child: GestureDetector(
              onTapUp: (details) {
                if (details.localPosition.dy < 24) {
                  widget.onPreviousRecord();
                } else {
                  widget.onNextRecord();
                }
              },
              child: CustomPaint(
                size: const Size(64, 48),
                painter:
                    _File4BaseBookPainter(isDark: isDark, hasBookmark: true),
              ),
            ),
          ),
          const SizedBox(height: 4),
          Container(
            width: 48,
            padding: const EdgeInsets.symmetric(vertical: 2),
            decoration: BoxDecoration(
              color: isDark ? const Color(0xFF272D37) : Colors.white,
              border: Border.all(
                color: isDark
                    ? const Color(0xFF4A5568)
                    : const Color(0xFF9E9E9E),
              ),
              borderRadius: BorderRadius.circular(2),
            ),
            child: Text(
              '$recordNumber',
              textAlign: TextAlign.center,
              style: const TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.bold,
                  fontFamily: 'monospace'),
            ),
          ),
          const SizedBox(height: 6),
          if (widget.totalRecords > 1)
            SizedBox(
              height: 24,
              child: SliderTheme(
                data: SliderThemeData(
                  trackHeight: 2,
                  thumbShape:
                      const RoundSliderThumbShape(enabledThumbRadius: 5),
                  overlayShape:
                      const RoundSliderOverlayShape(overlayRadius: 10),
                  activeTrackColor: const Color(0xFF1E88E5),
                  inactiveTrackColor:
                      isDark ? Colors.white24 : Colors.black12,
                  thumbColor: const Color(0xFF1E88E5),
                ),
                child: Slider(
                  value: widget.currentRecordIndex
                      .toDouble()
                      .clamp(0.0, (widget.totalRecords - 1).toDouble()),
                  min: 0,
                  max: (widget.totalRecords - 1).toDouble(),
                  onChanged: (val) => widget.onGoToRecord(val.round()),
                ),
              ),
            ),
          const SizedBox(height: 6),
          Align(
            alignment: Alignment.centerLeft,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text('Records:',
                    style:
                        TextStyle(fontSize: 11, fontWeight: FontWeight.bold)),
                Text('${widget.totalRecords}',
                    style: const TextStyle(
                        fontSize: 12, fontWeight: FontWeight.w600)),
                const SizedBox(height: 8),
                Text(widget.isUnsorted ? 'Unsorted' : 'Sorted',
                    style:
                        const TextStyle(fontSize: 11, color: Colors.grey)),
              ],
            ),
          ),
          const SizedBox(height: 12),
          const Divider(height: 1),
          const SizedBox(height: 8),
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceEvenly,
            children: [
              IconButton(
                visualDensity: VisualDensity.compact,
                padding: EdgeInsets.zero,
                iconSize: 18,
                icon: const Icon(Icons.add_circle_outline),
                tooltip: 'New Record',
                onPressed: widget.onNewRecord,
              ),
              IconButton(
                visualDensity: VisualDensity.compact,
                padding: EdgeInsets.zero,
                iconSize: 18,
                icon: const Icon(Icons.delete_outline),
                tooltip: 'Delete Record',
                onPressed:
                    widget.totalRecords > 0 ? widget.onDeleteRecord : null,
              ),
            ],
          ),
        ],
      ),
    );
  }

  // ─── Find Mode ───────────────────────────────────────────────────────────────

  Widget _buildFindNavigator(BuildContext context, bool isDark) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 8.0),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          CustomPaint(
            size: const Size(64, 48),
            painter:
                _File4BaseBookPainter(isDark: isDark, hasBookmark: true),
          ),
          const SizedBox(height: 6),
          const Align(
            alignment: Alignment.centerLeft,
            child: Text('Requests:',
                style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold)),
          ),
          const Align(
            alignment: Alignment.centerLeft,
            child: Text('1',
                style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600)),
          ),
          const SizedBox(height: 10),
          Row(
            children: [
              SizedBox(
                height: 20,
                width: 20,
                child: Checkbox(
                  value: widget.isFindOmit,
                  onChanged: (val) =>
                      widget.onToggleOmit?.call(val ?? false),
                ),
              ),
              const SizedBox(width: 6),
              const Text('Omit', style: TextStyle(fontSize: 11)),
            ],
          ),
          const SizedBox(height: 12),
          SizedBox(
            width: double.infinity,
            child: FilledButton(
              style: FilledButton.styleFrom(
                visualDensity: VisualDensity.compact,
                padding: const EdgeInsets.symmetric(vertical: 6),
                backgroundColor: const Color(0xFF1E88E5),
              ),
              onPressed: widget.onPerformFind,
              child: const Text('Find',
                  style:
                      TextStyle(fontSize: 12, fontWeight: FontWeight.bold)),
            ),
          ),
          const SizedBox(height: 6),
          SizedBox(
            width: double.infinity,
            child: OutlinedButton(
              style: OutlinedButton.styleFrom(
                visualDensity: VisualDensity.compact,
                padding: const EdgeInsets.symmetric(vertical: 6),
              ),
              onPressed: widget.onShowAllRecords,
              child:
                  const Text('Cancel', style: TextStyle(fontSize: 11)),
            ),
          ),
        ],
      ),
    );
  }

  // ─── Layout Mode Toolbox ──────────────────────────────────────────────────────

  Widget _buildLayoutToolbox(
      BuildContext context, bool isDark, Color borderColor) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        // Layout selector
        _buildLayoutSelector(context, isDark, borderColor),
        const SizedBox(height: 4),
        // Thumbnail
        _buildLayoutThumbnail(isDark),
        const SizedBox(height: 4),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 8),
          child: Text('Layouts:',
              style: TextStyle(
                  fontSize: 11,
                  fontWeight: FontWeight.bold,
                  color: isDark ? Colors.white70 : Colors.black87)),
        ),
        Padding(
          padding: const EdgeInsets.only(left: 8, bottom: 6),
          child: Text('${widget.layoutCount}',
              style: const TextStyle(
                  fontSize: 12, fontWeight: FontWeight.w600)),
        ),
        Divider(height: 1, color: borderColor),
        const SizedBox(height: 4),
        // ── Drawing tools (2-column grid) ─────────────────────────────────
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 4),
          child: Column(
            children: [
              _buildToolRow([
                _ToolDef(
                    tool: LayoutTool.pointer,
                    icon: Icons.near_me,
                    label: 'Pointer'),
                _ToolDef(
                    tool: LayoutTool.text,
                    iconWidget: const Text('A',
                        style: TextStyle(
                            fontSize: 15, fontWeight: FontWeight.bold)),
                    label: 'Text'),
              ], isDark),
              _buildToolRow([
                _ToolDef(
                    tool: LayoutTool.line,
                    icon: Icons.remove,
                    label: 'Line'),
                _ToolDef(
                    tool: LayoutTool.rectangle,
                    icon: Icons.crop_square,
                    label: 'Rectangle'),
              ], isDark),
              _buildToolRow([
                _ToolDef(
                    tool: LayoutTool.roundedRect,
                    icon: Icons.rounded_corner,
                    label: 'Rounded Rect'),
                _ToolDef(
                    tool: LayoutTool.oval,
                    icon: Icons.circle_outlined,
                    label: 'Oval'),
              ], isDark),
            ],
          ),
        ),
        Divider(height: 1, color: borderColor),
        // ── Object tools ───────────────────────────────────────────────────
        Padding(
          padding: const EdgeInsets.fromLTRB(4, 4, 4, 0),
          child: Column(
            children: [
              _buildToolRow([
                _ToolDef(
                    tool: LayoutTool.field,
                    icon: Icons.input,
                    label: 'Field'),
                _ToolDef(
                    tool: LayoutTool.part,
                    iconWidget: const Text('☰',
                        style: TextStyle(fontSize: 14)),
                    label: 'Part'),
              ], isDark),
              _buildToolRow([
                _ToolDef(
                    tool: LayoutTool.button,
                    icon: Icons.smart_button,
                    label: 'Button'),
                _ToolDef(
                    tool: LayoutTool.portal,
                    icon: Icons.table_rows,
                    label: 'Portal'),
              ], isDark),
              _buildToolRow([
                _ToolDef(
                    tool: LayoutTool.format,
                    icon: Icons.format_color_fill,
                    label: 'Format'),
                _ToolDef(
                    tool: LayoutTool.rotate,
                    icon: Icons.rotate_right,
                    label: 'Rotate'),
              ], isDark),
            ],
          ),
        ),
        Divider(height: 1, color: borderColor),
        const SizedBox(height: 6),
        _buildStrokeControl(isDark),
      ],
    );
  }

  Widget _buildLayoutSelector(
      BuildContext context, bool isDark, Color borderColor) {
    final layoutNames =
        List.generate(widget.layoutCount, (i) => 'Layout #${i + 1}');
    final currentName = widget.layoutCount > 0
        ? 'Layout #${widget.currentLayoutIndex + 1}'
        : 'No Layout';

    return Container(
      margin: const EdgeInsets.fromLTRB(6, 4, 6, 0),
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: isDark ? const Color(0xFF272D37) : Colors.white,
        borderRadius: BorderRadius.circular(3),
        border: Border.all(color: borderColor),
      ),
      child: DropdownButtonHideUnderline(
        child: DropdownButton<String>(
          value: currentName,
          isDense: true,
          isExpanded: true,
          icon: const Icon(Icons.arrow_drop_down, size: 16),
          style: TextStyle(
            fontSize: 11,
            fontWeight: FontWeight.w600,
            color: isDark ? Colors.white : Colors.black87,
          ),
          items: layoutNames.map((name) {
            return DropdownMenuItem<String>(
              value: name,
              child: Text(name, maxLines: 1, overflow: TextOverflow.ellipsis),
            );
          }).toList(),
          onChanged: (val) {
            if (val != null) {
              final idx = layoutNames.indexOf(val);
              widget.onLayoutChanged?.call(idx);
            }
          },
        ),
      ),
    );
  }

  Widget _buildLayoutThumbnail(bool isDark) {
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 8),
      height: 56,
      decoration: BoxDecoration(
        color: isDark ? const Color(0xFF272D37) : Colors.white,
        border: Border.all(
          color: isDark
              ? const Color(0xFF4A5568)
              : const Color(0xFFB8B5AE),
        ),
        borderRadius: BorderRadius.circular(2),
      ),
      child: CustomPaint(
        painter: _LayoutThumbnailPainter(isDark: isDark),
        child: const SizedBox.expand(),
      ),
    );
  }

  Widget _buildToolRow(List<_ToolDef> tools, bool isDark) {
    return Row(
      children: tools.map((def) {
        final isSelected = _selectedTool == def.tool;
        const selBg = Color(0xFF1E88E5);
        final normalFg = isDark ? Colors.white70 : Colors.black87;

        return Expanded(
          child: Tooltip(
            message: def.label,
            child: GestureDetector(
              onTap: () => setState(() => _selectedTool = def.tool),
              child: Container(
                height: 28,
                margin: const EdgeInsets.all(1),
                decoration: BoxDecoration(
                  color: isSelected ? selBg : Colors.transparent,
                  borderRadius: BorderRadius.circular(3),
                  border: Border.all(
                    color: isSelected
                        ? const Color(0xFF1565C0)
                        : (isDark
                            ? const Color(0xFF38404B)
                            : const Color(0xFFB8B5AE)),
                  ),
                ),
                child: Center(
                  child: def.iconWidget != null
                      ? DefaultTextStyle(
                          style: TextStyle(
                            color:
                                isSelected ? Colors.white : normalFg,
                            fontSize: 14,
                            fontWeight: FontWeight.bold,
                          ),
                          child: def.iconWidget!,
                        )
                      : Icon(
                          def.icon,
                          size: 16,
                          color: isSelected ? Colors.white : normalFg,
                        ),
                ),
              ),
            ),
          ),
        );
      }).toList(),
    );
  }

  Widget _buildStrokeControl(bool isDark) {
    final labelColor = isDark ? Colors.white60 : Colors.black54;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 1),
            decoration: BoxDecoration(
              border: Border.all(
                color: isDark
                    ? const Color(0xFF4A5568)
                    : const Color(0xFFB8B5AE),
              ),
              borderRadius: BorderRadius.circular(2),
            ),
            child: Text('100',
                style: TextStyle(
                    fontSize: 10,
                    fontFamily: 'monospace',
                    color: labelColor)),
          ),
          const SizedBox(height: 4),
          Row(
            children: [
              Icon(Icons.line_weight, size: 14, color: labelColor),
              const SizedBox(width: 4),
              Expanded(
                child: SliderTheme(
                  data: SliderThemeData(
                    trackHeight: 1.5,
                    thumbShape:
                        const RoundSliderThumbShape(enabledThumbRadius: 4),
                    overlayShape:
                        const RoundSliderOverlayShape(overlayRadius: 8),
                    activeTrackColor: const Color(0xFF1E88E5),
                    inactiveTrackColor:
                        isDark ? Colors.white24 : Colors.black12,
                    thumbColor: const Color(0xFF1E88E5),
                  ),
                  child: Slider(
                    value: _strokeWidth,
                    min: 0.5,
                    max: 6.0,
                    onChanged: (v) => setState(() => _strokeWidth = v),
                  ),
                ),
              ),
              Text('${_strokeWidth.toStringAsFixed(0)} pt',
                  style: TextStyle(fontSize: 9, color: labelColor)),
            ],
          ),
        ],
      ),
    );
  }

  // ─── Preview Mode ─────────────────────────────────────────────────────────────

  Widget _buildPreviewNavigator(BuildContext context, bool isDark) {
    return const Padding(
      padding: EdgeInsets.symmetric(horizontal: 8.0),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('Preview Mode',
              style: TextStyle(
                  fontSize: 11,
                  fontWeight: FontWeight.bold,
                  color: Color(0xFF1E88E5))),
          SizedBox(height: 6),
          Text('Page: 1 of 1', style: TextStyle(fontSize: 11)),
          SizedBox(height: 8),
          Text('Margins: 0.5 in',
              style: TextStyle(fontSize: 10, color: Colors.grey)),
        ],
      ),
    );
  }

  // ─── Manage Database Button ──────────────────────────────────────────────────

  Widget _buildManageDatabaseButton(BuildContext context, bool isDark) {
    return Padding(
      padding: const EdgeInsets.all(8.0),
      child: OutlinedButton(
        style: OutlinedButton.styleFrom(
          visualDensity: VisualDensity.compact,
          padding: const EdgeInsets.symmetric(vertical: 8, horizontal: 4),
          backgroundColor:
              isDark ? const Color(0xFF272D37) : Colors.white,
          side: BorderSide(
            color: isDark
                ? const Color(0xFF4A5568)
                : const Color(0xFFB8B5AE),
          ),
          shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(4)),
        ),
        onPressed: widget.onManageDatabase,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.storage,
                size: 18,
                color: isDark
                    ? const Color(0xFF90CAF9)
                    : const Color(0xFF1E88E5)),
            const SizedBox(height: 3),
            const Text(
              'Manage\nDatabase...',
              textAlign: TextAlign.center,
              style: TextStyle(
                  fontSize: 10, fontWeight: FontWeight.bold, height: 1.1),
            ),
          ],
        ),
      ),
    );
  }
}

// ─── Tool Definition Helper ───────────────────────────────────────────────────

class _ToolDef {
  final LayoutTool tool;
  final IconData? icon;
  final Widget? iconWidget;
  final String label;

  const _ToolDef(
      {required this.tool, this.icon, this.iconWidget, required this.label});
}

// ─── Painters ─────────────────────────────────────────────────────────────────

/// Classic File4Base spiral-bound notebook / flip-book painter
class _File4BaseBookPainter extends CustomPainter {
  final bool isDark;
  final bool hasBookmark;

  const _File4BaseBookPainter({required this.isDark, this.hasBookmark = true});

  @override
  void paint(Canvas canvas, Size size) {
    final pageBg = isDark ? const Color(0xFF2D3748) : Colors.white;
    final pageBorder =
        isDark ? const Color(0xFF4A5568) : const Color(0xFF718096);
    final ringColor =
        isDark ? const Color(0xFFCBD5E0) : const Color(0xFF2D3748);
    final lineRuleColor =
        isDark ? const Color(0xFF4A5568) : const Color(0xFFE2E8F0);
    const bookmarkColor = Color(0xFF3182CE);

    const leftOffset = 10.0;
    final pageWidth = size.width - leftOffset - (hasBookmark ? 8 : 2);
    final pageRect = Rect.fromLTWH(leftOffset, 2, pageWidth, size.height - 4);

    canvas.drawRRect(
      RRect.fromRectAndRadius(
          pageRect.translate(2, 2), const Radius.circular(2)),
      Paint()..color = Colors.black.withValues(alpha: 0.12),
    );

    final pageRRect =
        RRect.fromRectAndRadius(pageRect, const Radius.circular(2));
    canvas.drawRRect(pageRRect, Paint()..color = pageBg);

    final linePaint = Paint()
      ..color = lineRuleColor
      ..strokeWidth = 1.0;
    for (double y = pageRect.top + 10; y < pageRect.bottom - 4; y += 8) {
      canvas.drawLine(Offset(pageRect.left + 4, y),
          Offset(pageRect.right - 4, y), linePaint);
    }

    if (hasBookmark) {
      final tabPath = Path()
        ..moveTo(pageRect.right - 1, pageRect.top + 10)
        ..lineTo(pageRect.right + 7, pageRect.top + 14)
        ..lineTo(pageRect.right + 7, pageRect.top + 28)
        ..lineTo(pageRect.right - 1, pageRect.top + 24)
        ..close();
      canvas.drawPath(tabPath, Paint()..color = bookmarkColor);
    }

    canvas.drawRRect(
        pageRRect,
        Paint()
          ..color = pageBorder
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1.0);

    final ringPaint = Paint()
      ..color = ringColor
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.6;
    final holePaint = Paint()
      ..color = isDark ? Colors.black : const Color(0xFF4A5568);

    const ringCount = 5;
    final step = (pageRect.height - 10) / (ringCount - 1);
    for (int i = 0; i < ringCount; i++) {
      final y = pageRect.top + 5 + i * step;
      canvas.drawCircle(Offset(pageRect.left + 3, y), 1.5, holePaint);
      canvas.drawPath(
          Path()
            ..moveTo(pageRect.left - 4, y - 2)
            ..cubicTo(pageRect.left - 8, y - 3, pageRect.left - 4, y + 3,
                pageRect.left + 3, y + 1),
          ringPaint);
    }
  }

  @override
  bool shouldRepaint(covariant _File4BaseBookPainter old) =>
      old.isDark != isDark || old.hasBookmark != hasBookmark;
}

/// Layout thumbnail preview painter
class _LayoutThumbnailPainter extends CustomPainter {
  final bool isDark;

  const _LayoutThumbnailPainter({required this.isDark});

  @override
  void paint(Canvas canvas, Size size) {
    final bgColor =
        isDark ? const Color(0xFF1E232B) : const Color(0xFFF5F5F5);
    final headerColor =
        isDark ? const Color(0xFF2A3240) : const Color(0xFFE8E8E8);
    final fieldColor =
        isDark ? const Color(0xFF38404B) : const Color(0xFFD0D0D0);
    final lineColor =
        isDark ? const Color(0xFF4A5568) : const Color(0xFFB0B0B0);

    canvas.drawRect(
        Rect.fromLTWH(0, 0, size.width, size.height),
        Paint()..color = bgColor);
    canvas.drawRect(
        Rect.fromLTWH(0, 0, size.width, 12),
        Paint()..color = headerColor);
    canvas.drawLine(Offset(0, 12), Offset(size.width, 12),
        Paint()..color = lineColor..strokeWidth = 0.5);
    for (int i = 0; i < 3; i++) {
      canvas.drawRRect(
          RRect.fromRectAndRadius(
              Rect.fromLTWH(6, 16.0 + i * 10, size.width - 12, 7),
              const Radius.circular(1)),
          Paint()..color = fieldColor);
    }
  }

  @override
  bool shouldRepaint(covariant _LayoutThumbnailPainter old) =>
      old.isDark != isDark;
}
