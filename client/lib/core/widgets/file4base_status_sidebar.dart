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
  button,
  popoverButton,
  buttonBar,
  tabControl,
  portal,
  chart,
  webViewer,
  part,
  format,
  rotate,
  tabOrder, // Set Tab Order mode: click fields/buttons in Tab key sequence
}

// ─── Main Sidebar Widget (StatefulWidget for tool selection) ──────────────────

class File4BaseStatusSidebar extends StatefulWidget {
  final List<LayoutModel> layouts;
  final LayoutModel? selectedLayout;
  final ValueChanged<LayoutModel> onLayoutSelected;
  final VoidCallback? onNewLayout;
  final VoidCallback? onManageLayouts;
  final VoidCallback? onRenameLayout;
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

  /// Layout mode tool palette. The palette shows [selectedTool] and reports
  /// picks through [onToolSelected], so it stays in sync with the designer.
  final LayoutTool selectedTool;
  final ValueChanged<LayoutTool>? onToolSelected;

  /// Line width applied to the selected object (and to new drawings).
  final double strokeWidth;
  final ValueChanged<double>? onStrokeWidthChanged;

  const File4BaseStatusSidebar({
    super.key,
    this.layouts = const [],
    this.selectedLayout,
    required this.onLayoutSelected,
    this.onNewLayout,
    this.onManageLayouts,
    this.onRenameLayout,
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
    this.selectedTool = LayoutTool.pointer,
    this.onToolSelected,
    this.strokeWidth = 1.0,
    this.onStrokeWidthChanged,
  });

  @override
  State<File4BaseStatusSidebar> createState() => _File4BaseStatusSidebarState();
}

class _File4BaseStatusSidebarState extends State<File4BaseStatusSidebar> {
  LayoutTool get _selectedTool => widget.selectedTool;
  double get _strokeWidth => widget.strokeWidth.clamp(0.5, 6.0);

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
          _buildLayoutSelector(context, isDark, borderColor),
          const SizedBox(height: 8),
          // Browse mode: record navigation lives in the record bar above the
          // layout, so the sidebar only offers the layout selector.
          if (widget.mode == OperationalMode.browse)
            const SizedBox.shrink()
          else if (widget.mode == OperationalMode.find)
            _buildFindNavigator(context, isDark, borderColor)
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

  // ─── Top Layout Selector ───────────────────────────────────────────────────

  /// A panel of the sidebar: the same card every mode uses, so the sections
  /// line up whatever mode is active.
  Widget _sidebarCard({
    required bool isDark,
    required Color borderColor,
    required IconData icon,
    required String title,
    required List<Widget> children,
  }) {
    return Container(
      margin: const EdgeInsets.fromLTRB(6, 6, 6, 0),
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
      decoration: BoxDecoration(
        color: isDark ? const Color(0xFF272D37) : Colors.white,
        borderRadius: BorderRadius.circular(4),
        border: Border.all(color: borderColor),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.05),
            blurRadius: 1,
            offset: const Offset(0, 1),
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          Row(
            children: [
              Icon(
                icon,
                size: 13,
                color: isDark ? const Color(0xFF90CAF9) : const Color(0xFF1E88E5),
              ),
              const SizedBox(width: 4),
              Expanded(
                child: Text(
                  title,
                  style: TextStyle(
                    fontSize: 9,
                    fontWeight: FontWeight.bold,
                    letterSpacing: 0.5,
                    color: isDark ? Colors.grey.shade400 : Colors.grey.shade600,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 2),
          ...children,
        ],
      ),
    );
  }

  Widget _buildLayoutSelector(
      BuildContext context, bool isDark, Color borderColor) {
    return _sidebarCard(
      isDark: isDark,
      borderColor: borderColor,
      icon: Icons.view_quilt,
      title: 'LAYOUT',
      children: [
          DropdownButtonHideUnderline(
            child: DropdownButton<String>(
              value: widget.selectedLayout != null &&
                      widget.layouts.any((l) => l.id == widget.selectedLayout!.id)
                  ? widget.selectedLayout!.id
                  : (widget.layouts.isNotEmpty ? widget.layouts.first.id : null),
              isDense: true,
              isExpanded: true,
              icon: const Icon(Icons.arrow_drop_down, size: 16),
              style: TextStyle(
                fontSize: 11,
                fontWeight: FontWeight.w600,
                color: isDark ? Colors.white : Colors.black87,
              ),
              selectedItemBuilder: (context) {
                final allItems = [
                  ...widget.layouts.map((l) => Text(
                        l.name,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 11,
                          fontWeight: FontWeight.w600,
                          color: isDark ? Colors.white : Colors.black87,
                        ),
                      )),
                  const SizedBox.shrink(),
                  const SizedBox.shrink(),
                ];
                return allItems;
              },
              items: [
                ...widget.layouts.map((l) {
                  return DropdownMenuItem<String>(
                    value: l.id,
                    child: Text(
                      l.name,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  );
                }),
                DropdownMenuItem<String>(
                  value: '__manage__',
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Icon(Icons.settings, size: 12, color: Color(0xFF1E88E5)),
                      const SizedBox(width: 4),
                      Expanded(
                        child: Text(
                          'Manage Layouts...',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontSize: 10,
                            color: const Color(0xFF1E88E5),
                            fontWeight: FontWeight.bold,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
                DropdownMenuItem<String>(
                  value: '__new__',
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Icon(Icons.add, size: 12, color: Colors.green),
                      const SizedBox(width: 4),
                      Expanded(
                        child: Text(
                          'New Layout...',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontSize: 10,
                            color: Colors.green,
                            fontWeight: FontWeight.bold,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ],
              onChanged: (newId) {
                if (newId == '__manage__') {
                  widget.onManageLayouts?.call();
                } else if (newId == '__new__') {
                  widget.onNewLayout?.call();
                } else if (newId != null) {
                  final match =
                      widget.layouts.where((l) => l.id == newId).firstOrNull;
                  if (match != null) {
                    widget.onLayoutSelected(match);
                  }
                }
              },
            ),
          ),
      ],
    );
  }

  // ─── Find Mode ───────────────────────────────────────────────────────────────

  Widget _buildFindNavigator(
      BuildContext context, bool isDark, Color borderColor) {
    return _sidebarCard(
      isDark: isDark,
      borderColor: borderColor,
      icon: Icons.manage_search,
      title: 'FIND',
      children: [
        Text(
          'Type what to match in each field.',
          style: TextStyle(
            fontSize: 9,
            height: 1.25,
            color: isDark ? Colors.grey.shade400 : Colors.grey.shade600,
          ),
        ),
        const SizedBox(height: 8),
        // Omit: find the records that do NOT match the criteria
        Tooltip(
          message: 'Find the records that do not match the criteria',
          child: InkWell(
            onTap: widget.onToggleOmit == null
                ? null
                : () => widget.onToggleOmit!(!widget.isFindOmit),
            borderRadius: BorderRadius.circular(3),
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 2),
              child: Row(
                children: [
                  SizedBox(
                    height: 18,
                    width: 18,
                    child: Checkbox(
                      value: widget.isFindOmit,
                      visualDensity: VisualDensity.compact,
                      materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
                      onChanged: widget.onToggleOmit == null
                          ? null
                          : (val) => widget.onToggleOmit!(val ?? false),
                    ),
                  ),
                  const SizedBox(width: 8),
                  const Expanded(
                    child: Text('Omit', style: TextStyle(fontSize: 11)),
                  ),
                ],
              ),
            ),
          ),
        ),
        const SizedBox(height: 8),
        FilledButton(
          style: FilledButton.styleFrom(
            visualDensity: VisualDensity.compact,
            padding: const EdgeInsets.symmetric(vertical: 6),
            backgroundColor: const Color(0xFF1E88E5),
            foregroundColor: Colors.white,
            shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(4)),
          ),
          onPressed: widget.onPerformFind,
          child: const Text('Perform Find',
              style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold)),
        ),
        const SizedBox(height: 4),
        OutlinedButton(
          style: OutlinedButton.styleFrom(
            visualDensity: VisualDensity.compact,
            padding: const EdgeInsets.symmetric(vertical: 6),
            side: BorderSide(color: borderColor),
            shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(4)),
          ),
          onPressed: widget.onShowAllRecords,
          child: const Text('Cancel Find', style: TextStyle(fontSize: 11)),
        ),
        const SizedBox(height: 2),
      ],
    );
  }

  // ─── Layout Mode Toolbox ──────────────────────────────────────────────────────

  Widget _buildLayoutToolbox(
      BuildContext context, bool isDark, Color borderColor) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const SizedBox(height: 4),
        // Thumbnail
        _buildLayoutThumbnail(isDark),
        const SizedBox(height: 4),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 8),
          child: Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              Text('Layouts:',
                  style: TextStyle(
                      fontSize: 10,
                      fontWeight: FontWeight.bold,
                      color: isDark ? Colors.white70 : Colors.black87)),
              Text('${widget.layouts.length}',
                  style: const TextStyle(
                      fontSize: 11, fontWeight: FontWeight.w600)),
            ],
          ),
        ),
        const SizedBox(height: 4),
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
                    tool: LayoutTool.tabOrder,
                    icon: Icons.keyboard_tab,
                    label: 'Set Tab Order'),
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
              onTap: () => widget.onToolSelected?.call(def.tool),
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
                    onChanged: widget.onStrokeWidthChanged,
                  ),
                ),
              ),
              Text('${_strokeWidth.toStringAsFixed(1)} pt',
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
