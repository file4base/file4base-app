import 'dart:math' as math;

import 'package:file4base_client/core/models/page_setup_model.dart';
import 'package:file4base_client/features/layout_engine/layout_object_visuals.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter/material.dart';

/// Standard font families available in Format > Font and the Formatting Bar.
const List<String> kLayoutFontFamilies = [
  'Inter',
  'Helvetica',
  'Arial',
  'Georgia',
  'Times New Roman',
  'Courier New',
  'monospace',
];

/// Standard font sizes (pt) available in Format > Size and the Formatting Bar.
const List<double> kLayoutFontSizes = [
  9,
  10,
  11,
  12,
  13,
  14,
  16,
  18,
  20,
  24,
  28,
  36,
  48,
];

/// Standard color swatches used across Format > Text Color and the Formatting Bar.
const Map<String, String> kFormatColorSwatches = {
  '#000000': 'Black',
  '#21262D': 'Charcoal',
  '#616161': 'Dark Grey',
  '#9E9E9E': 'Grey',
  '#F5F5F7': 'Light Grey',
  '#FFFFFF': 'White',
  '#1E88E5': 'Blue',
  '#0D47A1': 'Navy',
  '#00ACC1': 'Cyan',
  '#2E7D32': 'Green',
  '#F59E0B': 'Amber',
  '#F57C00': 'Orange',
  '#EF4444': 'Red',
  '#8E24AA': 'Purple',
};

// ─────────────────────────────────────────────────────────────────────────────
// 1. Formatting Bar (View > Formatting Bar)
// ─────────────────────────────────────────────────────────────────────────────

/// Horizontal Formatting Bar positioned at the top of the workspace.
/// Allows rapid typography, alignment, line spacing, fill/border color, and
/// data/conditional format adjustments without opening the side Inspector.
class File4BaseFormattingBar extends StatelessWidget {

  const File4BaseFormattingBar({
    required this.style,
    required this.onStyleChanged,
    super.key,
    this.targetLabel,
    this.onOpenNumberFormat,
    this.onOpenDateFormat,
    this.onOpenTimeFormat,
    this.onOpenConditionalFormatting,
    this.onOpenTextFormatDialog,
  });

  final LayoutObjectStyle style;
  final ValueChanged<LayoutObjectStyle> onStyleChanged;
  final String? targetLabel;
  final VoidCallback? onOpenNumberFormat;
  final VoidCallback? onOpenDateFormat;
  final VoidCallback? onOpenTimeFormat;
  final VoidCallback? onOpenConditionalFormatting;
  final VoidCallback? onOpenTextFormatDialog;

  @override
  Widget build(BuildContext context) {
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final barBg = isDark ? const Color(0xFF1C2128) : const Color(0xFFF3F2EE);
    final borderColor = isDark ? const Color(0xFF30363D) : const Color(0xFFD5D3CD);
    final currentFamily =
        (style.fontFamily != null && kLayoutFontFamilies.contains(style.fontFamily))
            ? style.fontFamily!
            : '';
    final currentSize = kLayoutFontSizes.contains(style.fontSize)
        ? style.fontSize
        : 14.0;

    return Container(
      height: 36,
      padding: const EdgeInsets.symmetric(horizontal: 10),
      decoration: BoxDecoration(
        color: barBg,
        border: Border(bottom: BorderSide(color: borderColor)),
      ),
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Row(
          children: [
            // Target object/field indicator badge
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
              decoration: BoxDecoration(
                color: const Color(0xFF0284C7).withValues(alpha: 0.12),
                borderRadius: BorderRadius.circular(4),
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Icon(Icons.text_format, size: 13, color: Color(0xFF0284C7)),
                  const SizedBox(width: 4),
                  Text(
                    targetLabel ?? 'Formatting',
                    style: const TextStyle(
                      fontSize: 11,
                      fontWeight: FontWeight.w600,
                      color: Color(0xFF0284C7),
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(width: 8),

            // Font Family Selector
            Container(
              height: 24,
              padding: const EdgeInsets.symmetric(horizontal: 6),
              decoration: BoxDecoration(
                color: isDark ? const Color(0xFF262C36) : Colors.white,
                borderRadius: BorderRadius.circular(4),
                border: Border.all(color: borderColor),
              ),
              child: DropdownButtonHideUnderline(
                child: DropdownButton<String>(
                  value: currentFamily,
                  isDense: true,
                  icon: const Icon(Icons.arrow_drop_down, size: 15),
                  style: TextStyle(
                    fontSize: 11,
                    color: isDark ? Colors.white : Colors.black87,
                  ),
                  items: [
                    const DropdownMenuItem(
                      value: '',
                      child: Text('System Default', style: TextStyle(fontSize: 11)),
                    ),
                    for (final fam in kLayoutFontFamilies)
                      DropdownMenuItem(
                        value: fam,
                        child: Text(
                          fam,
                          style: TextStyle(fontSize: 11, fontFamily: fam),
                        ),
                      ),
                  ],
                  onChanged: (fam) {
                    if (fam == null) return;
                    if (fam.isEmpty) {
                      onStyleChanged(style.copyWith(clearFontFamily: true));
                    } else {
                      onStyleChanged(style.copyWith(fontFamily: fam));
                    }
                  },
                ),
              ),
            ),
            const SizedBox(width: 6),

            // Font Size Selector
            Container(
              height: 24,
              padding: const EdgeInsets.symmetric(horizontal: 6),
              decoration: BoxDecoration(
                color: isDark ? const Color(0xFF262C36) : Colors.white,
                borderRadius: BorderRadius.circular(4),
                border: Border.all(color: borderColor),
              ),
              child: DropdownButtonHideUnderline(
                child: DropdownButton<double>(
                  value: currentSize,
                  isDense: true,
                  icon: const Icon(Icons.arrow_drop_down, size: 15),
                  style: TextStyle(
                    fontSize: 11,
                    color: isDark ? Colors.white : Colors.black87,
                  ),
                  items: [
                    for (final s in kLayoutFontSizes)
                      DropdownMenuItem(
                        value: s,
                        child: Text('${s.round()} pt', style: const TextStyle(fontSize: 11)),
                      ),
                  ],
                  onChanged: (s) {
                    if (s != null) {
                      onStyleChanged(style.copyWith(fontSize: s));
                    }
                  },
                ),
              ),
            ),
            const SizedBox(width: 8),
            _divider(borderColor),
            const SizedBox(width: 6),

            // Bold / Italic / Underline / Strikethrough toggles
            _toggleIconBtn(
              icon: Icons.format_bold,
              tooltip: 'Bold (⌘B)',
              active: style.fontWeight == 'bold',
              onTap: () => onStyleChanged(
                style.copyWith(
                  fontWeight: style.fontWeight == 'bold' ? 'normal' : 'bold',
                ),
              ),
            ),
            _toggleIconBtn(
              icon: Icons.format_italic,
              tooltip: 'Italic (⌘I)',
              active: style.fontStyle == 'italic',
              onTap: () => onStyleChanged(
                style.copyWith(
                  fontStyle: style.fontStyle == 'italic' ? 'normal' : 'italic',
                ),
              ),
            ),
            _toggleIconBtn(
              icon: Icons.format_underlined,
              tooltip: 'Underline (⌘U)',
              active: style.underline,
              onTap: () => onStyleChanged(
                style.copyWith(underline: !style.underline),
              ),
            ),
            _toggleIconBtn(
              icon: Icons.format_strikethrough,
              tooltip: 'Strikethrough',
              active: style.strikethrough,
              onTap: () => onStyleChanged(
                style.copyWith(strikethrough: !style.strikethrough),
              ),
            ),
            const SizedBox(width: 6),
            _divider(borderColor),
            const SizedBox(width: 6),

            // Text Color Swatch Button
            _colorMenuButton(
              context: context,
              label: 'Text Color',
              icon: Icons.format_color_text,
              currentColor: style.textColor,
              defaultPreview: isDark ? Colors.white : Colors.black87,
              onSelect: (hex) => onStyleChanged(
                hex == null
                    ? style.copyWith(clearTextColor: true)
                    : style.copyWith(textColor: hex),
              ),
            ),
            const SizedBox(width: 4),

            // Fill Color Swatch Button
            _colorMenuButton(
              context: context,
              label: 'Fill Color',
              icon: Icons.format_color_fill,
              currentColor: style.fillColor,
              defaultPreview: Colors.transparent,
              onSelect: (hex) => onStyleChanged(
                hex == null
                    ? style.copyWith(clearFillColor: true)
                    : style.copyWith(fillColor: hex),
              ),
            ),
            const SizedBox(width: 4),

            // Border Color Swatch Button
            _colorMenuButton(
              context: context,
              label: 'Border Color',
              icon: Icons.border_color,
              currentColor: style.borderColor,
              defaultPreview: Colors.grey,
              onSelect: (hex) => onStyleChanged(
                hex == null
                    ? style.copyWith(clearBorderColor: true)
                    : style.copyWith(borderColor: hex),
              ),
            ),
            const SizedBox(width: 6),
            _divider(borderColor),
            const SizedBox(width: 6),

            // Alignment buttons
            _toggleIconBtn(
              icon: Icons.format_align_left,
              tooltip: 'Align Left',
              active: style.textAlign == 'left',
              onTap: () => onStyleChanged(style.copyWith(textAlign: 'left')),
            ),
            _toggleIconBtn(
              icon: Icons.format_align_center,
              tooltip: 'Align Center',
              active: style.textAlign == 'center',
              onTap: () => onStyleChanged(style.copyWith(textAlign: 'center')),
            ),
            _toggleIconBtn(
              icon: Icons.format_align_right,
              tooltip: 'Align Right',
              active: style.textAlign == 'right',
              onTap: () => onStyleChanged(style.copyWith(textAlign: 'right')),
            ),
            _toggleIconBtn(
              icon: Icons.format_align_justify,
              tooltip: 'Justify',
              active: style.textAlign == 'justify',
              onTap: () => onStyleChanged(style.copyWith(textAlign: 'justify')),
            ),
            const SizedBox(width: 6),

            // Line Spacing Popup
            PopupMenuButton<double>(
              tooltip: 'Line Spacing (${style.lineSpacing.toStringAsFixed(2)}x)',
              onSelected: (spacing) =>
                  onStyleChanged(style.copyWith(lineSpacing: spacing)),
              itemBuilder: (ctx) => [
                for (final entry in const [
                  (1.0, 'Single (1.0x)'),
                  (1.15, '1.15x'),
                  (1.2, 'Standard (1.2x)'),
                  (1.5, 'One and a Half (1.5x)'),
                  (2.0, 'Double (2.0x)'),
                ])
                  PopupMenuItem<double>(
                    value: entry.$1,
                    height: 30,
                    child: Row(
                      children: [
                        SizedBox(
                          width: 18,
                          child: (style.lineSpacing - entry.$1).abs() < 0.02
                              ? const Icon(Icons.check, size: 14, color: Color(0xFF0284C7))
                              : null,
                        ),
                        Text(entry.$2, style: const TextStyle(fontSize: 12)),
                      ],
                    ),
                  ),
              ],
              child: Container(
                height: 24,
                padding: const EdgeInsets.symmetric(horizontal: 5),
                decoration: BoxDecoration(
                  borderRadius: BorderRadius.circular(4),
                ),
                child: const Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(Icons.format_line_spacing, size: 15),
                    Icon(Icons.arrow_drop_down, size: 13),
                  ],
                ),
              ),
            ),

            const SizedBox(width: 6),
            _divider(borderColor),
            const SizedBox(width: 6),

            // Quick Data & Conditional Formatting actions
            if (onOpenNumberFormat != null)
              _actionChipBtn(
                icon: Icons.numbers,
                label: style.numberFormat != null && style.numberFormat != 'general'
                    ? 'Num (${style.numberFormat})'
                    : 'Number...',
                active: style.numberFormat != null && style.numberFormat != 'general',
                onTap: onOpenNumberFormat!,
              ),
            if (onOpenDateFormat != null) ...[
              const SizedBox(width: 4),
              _actionChipBtn(
                icon: Icons.calendar_today_outlined,
                label: style.dateFormat != null && style.dateFormat != 'as_entered'
                    ? 'Date (${style.dateFormat})'
                    : 'Date...',
                active: style.dateFormat != null && style.dateFormat != 'as_entered',
                onTap: onOpenDateFormat!,
              ),
            ],
            if (onOpenTimeFormat != null) ...[
              const SizedBox(width: 4),
              _actionChipBtn(
                icon: Icons.access_time,
                label: 'Time...',
                active: style.timeFormat != null && style.timeFormat != 'as_entered',
                onTap: onOpenTimeFormat!,
              ),
            ],
            if (onOpenConditionalFormatting != null) ...[
              const SizedBox(width: 4),
              _actionChipBtn(
                icon: Icons.auto_fix_high,
                label: style.conditionalRules.isNotEmpty
                    ? 'Conditional (${style.conditionalRules.length})'
                    : 'Conditional...',
                active: style.conditionalRules.isNotEmpty,
                onTap: onOpenConditionalFormatting!,
              ),
            ],
            if (onOpenTextFormatDialog != null) ...[
              const SizedBox(width: 4),
              _actionChipBtn(
                icon: Icons.tune,
                label: 'Text Format...',
                active: false,
                onTap: onOpenTextFormatDialog!,
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _divider(Color color) =>
      Container(width: 1, height: 18, color: color);

  Widget _toggleIconBtn({
    required IconData icon,
    required String tooltip,
    required bool active,
    required VoidCallback onTap,
  }) {
    return Tooltip(
      message: tooltip,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(4),
        child: Container(
          width: 25,
          height: 24,
          margin: const EdgeInsets.symmetric(horizontal: 1),
          decoration: BoxDecoration(
            color: active
                ? const Color(0xFF0284C7).withValues(alpha: 0.18)
                : Colors.transparent,
            borderRadius: BorderRadius.circular(4),
            border: Border.all(
              color: active ? const Color(0xFF0284C7) : Colors.transparent,
            ),
          ),
          child: Icon(
            icon,
            size: 15,
            color: active ? const Color(0xFF0284C7) : null,
          ),
        ),
      ),
    );
  }

  Widget _actionChipBtn({
    required IconData icon,
    required String label,
    required bool active,
    required VoidCallback onTap,
  }) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(4),
      child: Container(
        height: 24,
        padding: const EdgeInsets.symmetric(horizontal: 7),
        decoration: BoxDecoration(
          color: active
              ? const Color(0xFF0284C7).withValues(alpha: 0.16)
              : Colors.transparent,
          borderRadius: BorderRadius.circular(4),
          border: Border.all(
            color: active
                ? const Color(0xFF0284C7)
                : Colors.grey.withValues(alpha: 0.35),
          ),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              icon,
              size: 12,
              color: active ? const Color(0xFF0284C7) : null,
            ),
            const SizedBox(width: 4),
            Text(
              label,
              style: TextStyle(
                fontSize: 10.5,
                fontWeight: active ? FontWeight.bold : FontWeight.w500,
                color: active ? const Color(0xFF0284C7) : null,
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _colorMenuButton({
    required BuildContext context,
    required String label,
    required IconData icon,
    required String? currentColor,
    required Color defaultPreview,
    required ValueChanged<String?> onSelect,
  }) {
    final preview = parseLayoutColor(currentColor) ?? defaultPreview;
    return PopupMenuButton<String>(
      tooltip: label,
      onSelected: (val) => onSelect(val == '_none_' ? null : val),
      itemBuilder: (ctx) => [
        PopupMenuItem<String>(
          value: '_none_',
          height: 28,
          child: Row(
            children: [
              const Icon(Icons.block, size: 14, color: Colors.red),
              const SizedBox(width: 8),
              Text('Default / None ($label)', style: const TextStyle(fontSize: 11)),
            ],
          ),
        ),
        const PopupMenuDivider(height: 4),
        for (final entry in kFormatColorSwatches.entries)
          PopupMenuItem<String>(
            value: entry.key,
            height: 28,
            child: Row(
              children: [
                Container(
                  width: 14,
                  height: 14,
                  decoration: BoxDecoration(
                    color: parseLayoutColor(entry.key),
                    border: Border.all(color: Colors.black26),
                    borderRadius: BorderRadius.circular(3),
                  ),
                ),
                const SizedBox(width: 8),
                Text('${entry.value} (${entry.key})',
                    style: const TextStyle(fontSize: 11)),
              ],
            ),
          ),
      ],
      child: Container(
        height: 24,
        padding: const EdgeInsets.symmetric(horizontal: 5),
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(4),
        ),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(icon, size: 14),
            Container(
              width: 14,
              height: 3,
              decoration: BoxDecoration(
                color: preview,
                border: Border.all(color: Colors.black26, width: 0.5),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Text Ruler (View > Text Ruler)
// ─────────────────────────────────────────────────────────────────────────────

/// Interactive graduated ruler displayed above the editing area when
/// View > Text Ruler is enabled. Lets the user adjust Left Indent, Right Indent,
/// and Tab Stops with the mouse.
class File4BaseTextRuler extends StatelessWidget {

  const File4BaseTextRuler({
    required this.style,
    required this.onStyleChanged,
    super.key,
    this.width = 800,
    this.leftOffset = 0,
  });

  final LayoutObjectStyle style;
  final ValueChanged<LayoutObjectStyle> onStyleChanged;
  final double width;
  final double leftOffset;

  @override
  Widget build(BuildContext context) {
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final bg = isDark ? const Color(0xFF1F242D) : const Color(0xFFFAF9F5);
    final borderColor = isDark ? const Color(0xFF363C46) : const Color(0xFFCCC9C0);

    return Container(
      height: 26,
      decoration: BoxDecoration(
        color: bg,
        border: Border(bottom: BorderSide(color: borderColor)),
      ),
      child: Row(
        children: [
          if (leftOffset > 0)
            Container(
              width: leftOffset,
              height: 26,
              alignment: Alignment.center,
              decoration: BoxDecoration(
                color: isDark ? const Color(0xFF181C23) : const Color(0xFFEBE9E4),
                border: Border(right: BorderSide(color: borderColor)),
              ),
              child: const Text(
                'pt / in',
                style: TextStyle(fontSize: 9, fontWeight: FontWeight.bold, color: Colors.grey),
              ),
            ),
          Expanded(
            child: LayoutBuilder(
              builder: (context, constraints) {
                final rulerWidth = math.max(width, constraints.maxWidth);
                final leftIndent = style.indentLeft.clamp(0.0, rulerWidth - 40);
                final rightIndent = style.indentRight.clamp(0.0, rulerWidth - 40);
                final rightHandleX = (width - rightIndent).clamp(20.0, rulerWidth - 8);

                return GestureDetector(
                  behavior: HitTestBehavior.opaque,
                  onTapUp: (details) {
                    // Clicking on the ruler toggles a tab stop at the nearest 8pt
                    final x = (details.localPosition.dx / 8.0).round() * 8.0;
                    if (x < 8 || x > width - 8) return;
                    final existing = List<double>.from(style.tabStopsPt);
                    final matchIdx = existing.indexWhere((t) => (t - x).abs() <= 6);
                    if (matchIdx >= 0) {
                      existing.removeAt(matchIdx);
                    } else {
                      existing.add(x);
                      existing.sort();
                    }
                    onStyleChanged(style.copyWith(tabStopsPt: existing));
                  },
                  child: Stack(
                    clipBehavior: Clip.none,
                    children: [
                      CustomPaint(
                        size: Size(rulerWidth, 26),
                        painter: _TextRulerPainter(
                          isDark: isDark,
                          activeWidth: width,
                          indentLeft: leftIndent,
                          indentRight: rightIndent,
                          tabStops: style.tabStopsPt,
                        ),
                      ),
                      // Left Indent Handle
                      Positioned(
                        left: leftIndent - 6,
                        top: 0,
                        bottom: 0,
                        child: GestureDetector(
                          onHorizontalDragUpdate: (d) {
                            final next = (style.indentLeft + d.delta.dx)
                                .clamp(0.0, math.max(0.0, width - style.indentRight - 32));
                            onStyleChanged(
                              style.copyWith(indentLeft: next.roundToDouble()),
                            );
                          },
                          child: Tooltip(
                            message:
                                'Left Indent: ${style.indentLeft.round()} pt (drag to adjust)',
                            child: MouseRegion(
                              cursor: SystemMouseCursors.resizeLeftRight,
                              child: SizedBox(
                                width: 14,
                                height: 26,
                                child: CustomPaint(
                                  painter: _RulerMarkerPainter(
                                    color: const Color(0xFF0284C7),
                                    isLeft: true,
                                  ),
                                ),
                              ),
                            ),
                          ),
                        ),
                      ),
                      // Right Indent Handle
                      Positioned(
                        left: rightHandleX - 6,
                        top: 0,
                        bottom: 0,
                        child: GestureDetector(
                          onHorizontalDragUpdate: (d) {
                            final next = (style.indentRight - d.delta.dx)
                                .clamp(0.0, math.max(0.0, width - style.indentLeft - 32));
                            onStyleChanged(
                              style.copyWith(indentRight: next.roundToDouble()),
                            );
                          },
                          child: Tooltip(
                            message:
                                'Right Indent: ${style.indentRight.round()} pt (drag to adjust)',
                            child: MouseRegion(
                              cursor: SystemMouseCursors.resizeLeftRight,
                              child: SizedBox(
                                width: 14,
                                height: 26,
                                child: CustomPaint(
                                  painter: _RulerMarkerPainter(
                                    color: const Color(0xFF0284C7),
                                    isLeft: false,
                                  ),
                                ),
                              ),
                            ),
                          ),
                        ),
                      ),
                      // Info badge on far right
                      Positioned(
                        right: 8,
                        top: 5,
                        child: IgnorePointer(
                          child: Container(
                            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
                            decoration: BoxDecoration(
                              color: isDark ? Colors.black38 : Colors.white70,
                              borderRadius: BorderRadius.circular(3),
                            ),
                            child: Text(
                              'L: ${style.indentLeft.round()}pt · R: ${style.indentRight.round()}pt'
                              '${style.tabStopsPt.isEmpty ? " · Click ruler to add Tab Stop" : " · Tabs: ${style.tabStopsPt.map((e) => e.round()).join(", ")}pt"}',
                              style: TextStyle(
                                fontSize: 9.5,
                                color: isDark ? Colors.white70 : Colors.black54,
                              ),
                            ),
                          ),
                        ),
                      ),
                    ],
                  ),
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

class _TextRulerPainter extends CustomPainter {

  _TextRulerPainter({
    required this.isDark,
    required this.activeWidth,
    required this.indentLeft,
    required this.indentRight,
    required this.tabStops,
  });

  final bool isDark;
  final double activeWidth;
  final double indentLeft;
  final double indentRight;
  final List<double> tabStops;

  @override
  void paint(Canvas canvas, Size size) {
    // Highlight active editable span between left and right indent
    final activeLeft = indentLeft.clamp(0.0, activeWidth);
    final activeRight = (activeWidth - indentRight).clamp(activeLeft, activeWidth);
    final spanPaint = Paint()
      ..color = isDark
          ? const Color(0xFF0284C7).withValues(alpha: 0.10)
          : const Color(0xFF0284C7).withValues(alpha: 0.07);
    canvas.drawRect(
      Rect.fromLTRB(activeLeft, 0, activeRight, size.height),
      spanPaint,
    );

    final tickPaint = Paint()
      ..color = isDark ? Colors.white38 : Colors.black45
      ..strokeWidth = 1;

    final textStyle = TextStyle(
      fontSize: 8.5,
      color: isDark ? Colors.white60 : Colors.black54,
      fontFamily: 'monospace',
    );

    // 72 points = 1 inch; ticks every 9pt (1/8 in)
    for (double x = 0; x <= size.width; x += 9) {
      final isInch = (x % 72).abs() < 0.1;
      final isHalfInch = (x % 36).abs() < 0.1;
      final isQuarterInch = (x % 18).abs() < 0.1;
      final tickHeight = isInch
          ? 11.0
          : isHalfInch
              ? 7.5
              : isQuarterInch
                  ? 5.0
                  : 3.0;
      canvas.drawLine(
        Offset(x, size.height - tickHeight),
        Offset(x, size.height),
        tickPaint,
      );

      if (isInch && x > 0) {
        final inches = (x / 72).round();
        final tp = TextPainter(
          text: TextSpan(text: '$inches" (${x.round()})', style: textStyle),
          textDirection: TextDirection.ltr,
        )..layout();
        tp.paint(canvas, Offset(x + 3, 2));
      }
    }

    // Draw Tab Stops as L-shaped markers
    final tabPaint = Paint()
      ..color = const Color(0xFFD97706)
      ..strokeWidth = 2.0
      ..style = PaintingStyle.stroke;
    for (final tabX in tabStops) {
      final path = Path()
        ..moveTo(tabX, size.height - 11)
        ..lineTo(tabX, size.height - 2)
        ..lineTo(tabX + 6, size.height - 2);
      canvas.drawPath(path, tabPaint);
    }
  }

  @override
  bool shouldRepaint(covariant _TextRulerPainter old) =>
      old.isDark != isDark ||
      old.activeWidth != activeWidth ||
      old.indentLeft != indentLeft ||
      old.indentRight != indentRight ||
      old.tabStops != tabStops;
}

class _RulerMarkerPainter extends CustomPainter {

  _RulerMarkerPainter({required this.color, required this.isLeft});
  final Color color;
  final bool isLeft;

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color
      ..style = PaintingStyle.fill;
    final path = Path();
    if (isLeft) {
      path
        ..moveTo(2, size.height - 12)
        ..lineTo(11, size.height - 12)
        ..lineTo(6.5, size.height - 2)
        ..close();
    } else {
      path
        ..moveTo(3, size.height - 12)
        ..lineTo(12, size.height - 12)
        ..lineTo(7.5, size.height - 2)
        ..close();
    }
    canvas.drawPath(path, paint);
  }

  @override
  bool shouldRepaint(covariant _RulerMarkerPainter old) =>
      old.color != color || old.isLeft != isLeft;
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Page Margins Overlay (View > Page Margins)
// ─────────────────────────────────────────────────────────────────────────────

/// Draws dashed page margin boundaries and printable page break lines on the
/// layout canvas according to the active [PageSetupModel].
class PageMarginsOverlay extends StatelessWidget {

  const PageMarginsOverlay({
    required this.pageSetup,
    required this.canvasWidth,
    required this.canvasHeight,
    super.key,
  });

  final PageSetupModel pageSetup;
  final double canvasWidth;
  final double canvasHeight;

  @override
  Widget build(BuildContext context) {
    final orientation = pageSetup.isLandscape ? 'Landscape' : 'Portrait';
    final pw = pageSetup.printableWidthPt.round();
    final ph = pageSetup.printableHeightPt.round();

    return IgnorePointer(
      child: SizedBox(
        width: canvasWidth,
        height: canvasHeight,
        child: Stack(
          clipBehavior: Clip.none,
          children: [
            CustomPaint(
              size: Size(canvasWidth, canvasHeight),
              painter: _PageMarginsPainter(pageSetup: pageSetup),
            ),
            Positioned(
              left: pageSetup.marginLeftPt + 6,
              top: math.max(4, pageSetup.marginTopPt - 18),
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                decoration: BoxDecoration(
                  color: const Color(0xFF0284C7).withValues(alpha: 0.90),
                  borderRadius: BorderRadius.circular(3),
                ),
                child: Text(
                  'Printable Area · ${pageSetup.paperSizeName} $orientation ($pw × $ph pt · Margins ${pageSetup.marginLeftMm.round()}mm)',
                  style: const TextStyle(
                    fontSize: 9.5,
                    fontWeight: FontWeight.w600,
                    color: Colors.white,
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _PageMarginsPainter extends CustomPainter {

  _PageMarginsPainter({required this.pageSetup});
  final PageSetupModel pageSetup;

  @override
  void paint(Canvas canvas, Size size) {
    final left = pageSetup.marginLeftPt;
    final top = pageSetup.marginTopPt;
    final right = left + pageSetup.printableWidthPt;
    final bottom = top + pageSetup.printableHeightPt;

    // Subtle shaded non-printable margin zones
    final shadePaint = Paint()
      ..color = const Color(0xFF0284C7).withValues(alpha: 0.05)
      ..style = PaintingStyle.fill;

    if (left > 0) {
      canvas.drawRect(
        Rect.fromLTRB(0, 0, math.min(left, size.width), size.height),
        shadePaint,
      );
    }
    if (top > 0) {
      canvas.drawRect(
        Rect.fromLTRB(left, 0, size.width, math.min(top, size.height)),
        shadePaint,
      );
    }
    if (right < size.width) {
      canvas.drawRect(
        Rect.fromLTRB(right, top, size.width, size.height),
        shadePaint,
      );
    }

    // Dashed printable boundary lines
    final dashPaint = Paint()
      ..color = const Color(0xFF0284C7).withValues(alpha: 0.80)
      ..strokeWidth = 1.2
      ..style = PaintingStyle.stroke;

    // Left margin line
    _drawDashedLine(canvas, Offset(left, 0), Offset(left, size.height), dashPaint);
    // Right printable boundary line
    _drawDashedLine(canvas, Offset(right, 0), Offset(right, size.height), dashPaint);
    // Top margin line
    _drawDashedLine(canvas, Offset(0, top), Offset(size.width, top), dashPaint);
    // Bottom printable page break lines (every printableHeightPt)
    final pageH = pageSetup.printableHeightPt;
    if (pageH > 40) {
      for (double y = bottom; y <= size.height + pageH; y += pageH) {
        if (y <= size.height) {
          _drawDashedLine(canvas, Offset(0, y), Offset(size.width, y), dashPaint);
        }
      }
    }
  }

  void _drawDashedLine(Canvas canvas, Offset p1, Offset p2, Paint paint) {
    const dashLen = 6.0;
    const gapLen = 4.0;
    final dx = p2.dx - p1.dx;
    final dy = p2.dy - p1.dy;
    final total = math.sqrt(dx * dx + dy * dy);
    if (total <= 0) return;
    final ux = dx / total;
    final uy = dy / total;
    var dist = 0.0;
    while (dist < total) {
      final end = math.min(dist + dashLen, total);
      canvas.drawLine(
        Offset(p1.dx + ux * dist, p1.dy + uy * dist),
        Offset(p1.dx + ux * end, p1.dy + uy * end),
        paint,
      );
      dist += dashLen + gapLen;
    }
  }

  @override
  bool shouldRepaint(covariant _PageMarginsPainter old) =>
      old.pageSetup.paperSizeName != pageSetup.paperSizeName ||
      old.pageSetup.isLandscape != pageSetup.isLandscape ||
      old.pageSetup.marginLeftMm != pageSetup.marginLeftMm ||
      old.pageSetup.marginRightMm != pageSetup.marginRightMm ||
      old.pageSetup.marginTopMm != pageSetup.marginTopMm ||
      old.pageSetup.marginBottomMm != pageSetup.marginBottomMm;
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Format Menu Modal Dialogs (Text Format, Number, Date, Time, Timestamp, Conditional)
// ─────────────────────────────────────────────────────────────────────────────

class FormatDialogs {
  /// Opens the complete Text Format dialog (Font, Size, Style, Color, Alignment, Line Spacing, Indents).
  static Future<LayoutObjectStyle?> showTextFormatDialog(
    BuildContext context, {
    required LayoutObjectStyle initialStyle,
    String? targetName,
  }) {
    var current = initialStyle;
    return showDialog<LayoutObjectStyle>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setModalState) {
          return AlertDialog(
            title: Row(
              children: [
                const Icon(Icons.font_download_outlined, color: Color(0xFF0284C7), size: 20),
                const SizedBox(width: 8),
                Text(
                  targetName != null ? 'Text Format — $targetName' : 'Text Format',
                  style: const TextStyle(fontSize: 16, fontWeight: FontWeight.bold),
                ),
              ],
            ),
            content: SizedBox(
              width: 480,
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          flex: 2,
                          child: DropdownButtonFormField<String>(
                            initialValue: (current.fontFamily != null &&
                                    kLayoutFontFamilies.contains(current.fontFamily))
                                ? current.fontFamily!
                                : '',
                            decoration: const InputDecoration(
                              labelText: 'Font Family',
                              border: OutlineInputBorder(),
                              isDense: true,
                            ),
                            items: [
                              const DropdownMenuItem(value: '', child: Text('System Default')),
                              for (final f in kLayoutFontFamilies)
                                DropdownMenuItem(value: f, child: Text(f)),
                            ],
                            onChanged: (v) {
                              if (v == null) return;
                              setModalState(() {
                                current = v.isEmpty
                                    ? current.copyWith(clearFontFamily: true)
                                    : current.copyWith(fontFamily: v);
                              });
                            },
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: DropdownButtonFormField<double>(
                            initialValue: kLayoutFontSizes.contains(current.fontSize)
                                ? current.fontSize
                                : 14.0,
                            decoration: const InputDecoration(
                              labelText: 'Size (pt)',
                              border: OutlineInputBorder(),
                              isDense: true,
                            ),
                            items: [
                              for (final s in kLayoutFontSizes)
                                DropdownMenuItem(value: s, child: Text('${s.round()} pt')),
                            ],
                            onChanged: (v) {
                              if (v != null) {
                                setModalState(() => current = current.copyWith(fontSize: v));
                              }
                            },
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 14),
                    const Text('Style & Case',
                        style: TextStyle(fontSize: 12, fontWeight: FontWeight.bold)),
                    const SizedBox(height: 6),
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      children: [
                        FilterChip(
                          label: const Text('Bold'),
                          selected: current.fontWeight == 'bold',
                          onSelected: (b) => setModalState(
                            () => current = current.copyWith(fontWeight: b ? 'bold' : 'normal'),
                          ),
                        ),
                        FilterChip(
                          label: const Text('Italic'),
                          selected: current.fontStyle == 'italic',
                          onSelected: (b) => setModalState(
                            () => current = current.copyWith(fontStyle: b ? 'italic' : 'normal'),
                          ),
                        ),
                        FilterChip(
                          label: const Text('Underline'),
                          selected: current.underline,
                          onSelected: (b) => setModalState(
                            () => current = current.copyWith(underline: b),
                          ),
                        ),
                        FilterChip(
                          label: const Text('Strikethrough'),
                          selected: current.strikethrough,
                          onSelected: (b) => setModalState(
                            () => current = current.copyWith(strikethrough: b),
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 12),
                    Row(
                      children: [
                        Expanded(
                          child: DropdownButtonFormField<String>(
                            initialValue: current.textAlign,
                            decoration: const InputDecoration(
                              labelText: 'Alignment',
                              border: OutlineInputBorder(),
                              isDense: true,
                            ),
                            items: const [
                              DropdownMenuItem(value: 'left', child: Text('Left')),
                              DropdownMenuItem(value: 'center', child: Text('Center')),
                              DropdownMenuItem(value: 'right', child: Text('Right')),
                              DropdownMenuItem(value: 'justify', child: Text('Justify')),
                            ],
                            onChanged: (v) {
                              if (v != null) {
                                setModalState(() => current = current.copyWith(textAlign: v));
                              }
                            },
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: DropdownButtonFormField<double>(
                            initialValue: const [1.0, 1.15, 1.2, 1.5, 2.0]
                                    .contains(current.lineSpacing)
                                ? current.lineSpacing
                                : 1.2,
                            decoration: const InputDecoration(
                              labelText: 'Line Spacing',
                              border: OutlineInputBorder(),
                              isDense: true,
                            ),
                            items: const [
                              DropdownMenuItem(value: 1.0, child: Text('Single (1.0x)')),
                              DropdownMenuItem(value: 1.15, child: Text('1.15x')),
                              DropdownMenuItem(value: 1.2, child: Text('Standard (1.2x)')),
                              DropdownMenuItem(value: 1.5, child: Text('1.5x')),
                              DropdownMenuItem(value: 2.0, child: Text('Double (2.0x)')),
                            ],
                            onChanged: (v) {
                              if (v != null) {
                                setModalState(() => current = current.copyWith(lineSpacing: v));
                              }
                            },
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 12),
                    DropdownButtonFormField<String>(
                      initialValue: current.textCase,
                      decoration: const InputDecoration(
                        labelText: 'Text Case Transformation',
                        border: OutlineInputBorder(),
                        isDense: true,
                      ),
                      items: const [
                        DropdownMenuItem(value: 'none', child: Text('Normal (As Entered)')),
                        DropdownMenuItem(value: 'upper', child: Text('UPPERCASE')),
                        DropdownMenuItem(value: 'lower', child: Text('lowercase')),
                        DropdownMenuItem(value: 'title', child: Text('Title Case')),
                      ],
                      onChanged: (v) {
                        if (v != null) {
                          setModalState(() => current = current.copyWith(textCase: v));
                        }
                      },
                    ),
                    const SizedBox(height: 14),
                    const Text('Text Color',
                        style: TextStyle(fontSize: 12, fontWeight: FontWeight.bold)),
                    const SizedBox(height: 6),
                    Wrap(
                      spacing: 6,
                      runSpacing: 6,
                      children: [
                        for (final e in kFormatColorSwatches.entries)
                          InkWell(
                            onTap: () => setModalState(
                              () => current = current.copyWith(textColor: e.key),
                            ),
                            child: Container(
                              width: 24,
                              height: 24,
                              decoration: BoxDecoration(
                                color: parseLayoutColor(e.key),
                                shape: BoxShape.circle,
                                border: Border.all(
                                  color: current.textColor == e.key
                                      ? const Color(0xFF0284C7)
                                      : Colors.black26,
                                  width: current.textColor == e.key ? 2.5 : 1,
                                ),
                              ),
                            ),
                          ),
                      ],
                    ),
                    const SizedBox(height: 16),
                    Container(
                      width: double.infinity,
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: parseLayoutColor(current.fillColor) ?? Colors.grey.withValues(alpha: 0.08),
                        border: Border.all(color: Colors.grey.withValues(alpha: 0.3)),
                        borderRadius: BorderRadius.circular(6),
                      ),
                      child: Text(
                        applyLayoutTextCase(
                          'Sample File4Base Text — 1,234.56 €',
                          current.textCase,
                        ),
                        textAlign: layoutTextAlign(current.textAlign),
                        style: layoutTextStyle(current),
                      ),
                    ),
                  ],
                ),
              ),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(ctx).pop(),
                child: const Text('Cancel'),
              ),
              FilledButton(
                onPressed: () => Navigator.of(ctx).pop(current),
                child: const Text('Apply'),
              ),
            ],
          );
        },
      ),
    );
  }

  /// Opens the Number Format dialog (General, Decimal, Currency, Percentage, Boolean).
  static Future<LayoutObjectStyle?> showNumberFormatDialog(
    BuildContext context, {
    required LayoutObjectStyle initialStyle,
  }) {
    var current = initialStyle;
    final symbolCtrl = TextEditingController(text: current.currencySymbol);
    return showDialog<LayoutObjectStyle>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setModalState) {
          final fmt = current.numberFormat ?? 'general';
          final sample = formatLayoutDisplayValue('12345.678', current);
          return AlertDialog(
            title: const Row(
              children: [
                Icon(Icons.numbers, color: Color(0xFF0284C7), size: 20),
                SizedBox(width: 8),
                Text('Number Format', style: TextStyle(fontSize: 16, fontWeight: FontWeight.bold)),
              ],
            ),
            content: SizedBox(
              width: 420,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  DropdownButtonFormField<String>(
                    initialValue: fmt,
                    decoration: const InputDecoration(
                      labelText: 'Format Style',
                      border: OutlineInputBorder(),
                      isDense: true,
                    ),
                    items: const [
                      DropdownMenuItem(value: 'general', child: Text('General (As entered)')),
                      DropdownMenuItem(value: 'decimal', child: Text('Fixed Decimal')),
                      DropdownMenuItem(value: 'currency', child: Text('Currency (€ / \$ / £)')),
                      DropdownMenuItem(value: 'percent', child: Text('Percentage (%)')),
                      DropdownMenuItem(value: 'boolean', child: Text('Boolean (Yes / No)')),
                    ],
                    onChanged: (v) {
                      if (v != null) {
                        setModalState(() => current = current.copyWith(numberFormat: v));
                      }
                    },
                  ),
                  if (fmt == 'decimal' || fmt == 'currency' || fmt == 'percent') ...[
                    const SizedBox(height: 12),
                    Row(
                      children: [
                        Expanded(
                          child: DropdownButtonFormField<int>(
                            initialValue: current.decimalPlaces.clamp(0, 6),
                            decoration: const InputDecoration(
                              labelText: 'Fixed number of decimals',
                              border: OutlineInputBorder(),
                              isDense: true,
                            ),
                            items: [
                              for (var d = 0; d <= 6; d++)
                                DropdownMenuItem(value: d, child: Text('$d decimals')),
                            ],
                            onChanged: (d) {
                              if (d != null) {
                                setModalState(() => current = current.copyWith(decimalPlaces: d));
                              }
                            },
                          ),
                        ),
                        if (fmt == 'currency') ...[
                          const SizedBox(width: 12),
                          SizedBox(
                            width: 110,
                            child: TextField(
                              controller: symbolCtrl,
                              decoration: const InputDecoration(
                                labelText: 'Currency Symbol',
                                border: OutlineInputBorder(),
                                isDense: true,
                              ),
                              onChanged: (v) => setModalState(
                                () => current = current.copyWith(currencySymbol: v),
                              ),
                            ),
                          ),
                        ],
                      ],
                    ),
                    const SizedBox(height: 8),
                    CheckboxListTile(
                      contentPadding: EdgeInsets.zero,
                      dense: true,
                      title: const Text('Use thousands separator (,)',
                          style: TextStyle(fontSize: 12)),
                      value: current.useThousandsSeparator,
                      onChanged: (v) => setModalState(
                        () => current = current.copyWith(useThousandsSeparator: v ?? true),
                      ),
                    ),
                  ],
                  const SizedBox(height: 12),
                  Container(
                    width: double.infinity,
                    padding: const EdgeInsets.all(10),
                    decoration: BoxDecoration(
                      color: Colors.blueGrey.withValues(alpha: 0.08),
                      borderRadius: BorderRadius.circular(6),
                    ),
                    child: Text(
                      'Sample Preview (12345.678):  $sample',
                      style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
                    ),
                  ),
                ],
              ),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(ctx).pop(),
                child: const Text('Cancel'),
              ),
              FilledButton(
                onPressed: () => Navigator.of(ctx).pop(current),
                child: const Text('Apply'),
              ),
            ],
          );
        },
      ),
    );
  }

  /// Opens the Date Format dialog.
  static Future<LayoutObjectStyle?> showDateFormatDialog(
    BuildContext context, {
    required LayoutObjectStyle initialStyle,
  }) {
    var current = initialStyle;
    return showDialog<LayoutObjectStyle>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setModalState) {
          final fmt = current.dateFormat ?? 'as_entered';
          final sample = formatLayoutDisplayValue('2026-10-14', current);
          return AlertDialog(
            title: const Row(
              children: [
                Icon(Icons.calendar_today_outlined, color: Color(0xFF0284C7), size: 20),
                SizedBox(width: 8),
                Text('Date Format', style: TextStyle(fontSize: 16, fontWeight: FontWeight.bold)),
              ],
            ),
            content: SizedBox(
              width: 400,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  DropdownButtonFormField<String>(
                    initialValue: fmt,
                    decoration: const InputDecoration(
                      labelText: 'Date Display Format',
                      border: OutlineInputBorder(),
                      isDense: true,
                    ),
                    items: const [
                      DropdownMenuItem(value: 'as_entered', child: Text('As entered')),
                      DropdownMenuItem(value: 'dd/mm/yyyy', child: Text('14/10/2026 (DD/MM/YYYY)')),
                      DropdownMenuItem(value: 'mm/dd/yyyy', child: Text('10/14/2026 (MM/DD/YYYY)')),
                      DropdownMenuItem(value: 'yyyy-mm-dd', child: Text('2026-10-14 (ISO YYYY-MM-DD)')),
                      DropdownMenuItem(value: 'd_mmm_yyyy', child: Text('14 Oct 2026 (D MMM YYYY)')),
                      DropdownMenuItem(value: 'full', child: Text('Wednesday, 14 October 2026')),
                    ],
                    onChanged: (v) {
                      if (v != null) {
                        setModalState(() => current = current.copyWith(dateFormat: v));
                      }
                    },
                  ),
                  const SizedBox(height: 12),
                  Container(
                    width: double.infinity,
                    padding: const EdgeInsets.all(10),
                    decoration: BoxDecoration(
                      color: Colors.blueGrey.withValues(alpha: 0.08),
                      borderRadius: BorderRadius.circular(6),
                    ),
                    child: Text(
                      'Sample Preview:  $sample',
                      style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
                    ),
                  ),
                ],
              ),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(ctx).pop(),
                child: const Text('Cancel'),
              ),
              FilledButton(
                onPressed: () => Navigator.of(ctx).pop(current),
                child: const Text('Apply'),
              ),
            ],
          );
        },
      ),
    );
  }

  /// Opens the Time or Timestamp Format dialog.
  static Future<LayoutObjectStyle?> showTimeFormatDialog(
    BuildContext context, {
    required LayoutObjectStyle initialStyle,
    bool includeDate = false,
  }) {
    var current = initialStyle;
    return showDialog<LayoutObjectStyle>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setModalState) {
          final dateFmt = current.dateFormat ?? 'as_entered';
          final timeFmt = current.timeFormat ?? 'as_entered';
          final sample = formatLayoutDisplayValue('2026-10-14T14:35:08', current);
          return AlertDialog(
            title: Row(
              children: [
                const Icon(Icons.access_time, color: Color(0xFF0284C7), size: 20),
                const SizedBox(width: 8),
                Text(
                  includeDate ? 'Timestamp Format' : 'Time Format',
                  style: const TextStyle(fontSize: 16, fontWeight: FontWeight.bold),
                ),
              ],
            ),
            content: SizedBox(
              width: 420,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (includeDate) ...[
                    DropdownButtonFormField<String>(
                      initialValue: dateFmt,
                      decoration: const InputDecoration(
                        labelText: 'Date Portion',
                        border: OutlineInputBorder(),
                        isDense: true,
                      ),
                      items: const [
                        DropdownMenuItem(value: 'as_entered', child: Text('As entered')),
                        DropdownMenuItem(value: 'dd/mm/yyyy', child: Text('DD/MM/YYYY')),
                        DropdownMenuItem(value: 'mm/dd/yyyy', child: Text('MM/DD/YYYY')),
                        DropdownMenuItem(value: 'yyyy-mm-dd', child: Text('YYYY-MM-DD')),
                        DropdownMenuItem(value: 'd_mmm_yyyy', child: Text('D MMM YYYY')),
                      ],
                      onChanged: (v) {
                        if (v != null) {
                          setModalState(() => current = current.copyWith(dateFormat: v));
                        }
                      },
                    ),
                    const SizedBox(height: 12),
                  ],
                  DropdownButtonFormField<String>(
                    initialValue: timeFmt,
                    decoration: const InputDecoration(
                      labelText: 'Time Display Format',
                      border: OutlineInputBorder(),
                      isDense: true,
                    ),
                    items: const [
                      DropdownMenuItem(value: 'as_entered', child: Text('As entered')),
                      DropdownMenuItem(value: '24h', child: Text('14:35 (24-hour HH:mm)')),
                      DropdownMenuItem(value: '24h_sec', child: Text('14:35:08 (24-hour HH:mm:ss)')),
                      DropdownMenuItem(value: '12h', child: Text('2:35 PM (12-hour AM/PM)')),
                    ],
                    onChanged: (v) {
                      if (v != null) {
                        setModalState(() => current = current.copyWith(timeFormat: v));
                      }
                    },
                  ),
                  const SizedBox(height: 12),
                  Container(
                    width: double.infinity,
                    padding: const EdgeInsets.all(10),
                    decoration: BoxDecoration(
                      color: Colors.blueGrey.withValues(alpha: 0.08),
                      borderRadius: BorderRadius.circular(6),
                    ),
                    child: Text(
                      'Sample Preview:  $sample',
                      style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
                    ),
                  ),
                ],
              ),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(ctx).pop(),
                child: const Text('Cancel'),
              ),
              FilledButton(
                onPressed: () => Navigator.of(ctx).pop(current),
                child: const Text('Apply'),
              ),
            ],
          );
        },
      ),
    );
  }

  /// Opens the Conditional Formatting dialog where rules can be added/edited.
  static Future<LayoutObjectStyle?> showConditionalFormattingDialog(
    BuildContext context, {
    required LayoutObjectStyle initialStyle,
    String? targetName,
  }) {
    var rules = List<ConditionalFormatRule>.from(initialStyle.conditionalRules);
    return showDialog<LayoutObjectStyle>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setModalState) {
          return AlertDialog(
            title: Row(
              children: [
                const Icon(Icons.auto_fix_high, color: Color(0xFF0284C7), size: 20),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    targetName != null
                        ? 'Conditional Formatting — $targetName'
                        : 'Conditional Formatting',
                    style: const TextStyle(fontSize: 16, fontWeight: FontWeight.bold),
                  ),
                ),
              ],
            ),
            content: SizedBox(
              width: 560,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text(
                    'Automatically override text color, background fill, or font weight when the field value satisfies a condition (e.g., Value < 0 shown in red).',
                    style: TextStyle(fontSize: 12, color: Colors.grey),
                  ),
                  const SizedBox(height: 12),
                  if (rules.isEmpty)
                    Container(
                      width: double.infinity,
                      padding: const EdgeInsets.all(16),
                      decoration: BoxDecoration(
                        border: Border.all(color: Colors.grey.withValues(alpha: 0.3)),
                        borderRadius: BorderRadius.circular(6),
                      ),
                      child: const Text(
                        'No conditional formatting rules defined yet. Click "Add Rule" below.',
                        textAlign: TextAlign.center,
                        style: TextStyle(fontSize: 12, color: Colors.grey),
                      ),
                    )
                  else
                    ConstrainedBox(
                      constraints: const BoxConstraints(maxHeight: 280),
                      child: ListView.separated(
                        shrinkWrap: true,
                        itemCount: rules.length,
                        separatorBuilder: (_, _) => const SizedBox(height: 8),
                        itemBuilder: (ctx, i) {
                          final r = rules[i];
                          return Container(
                            padding: const EdgeInsets.all(10),
                            decoration: BoxDecoration(
                              border: Border.all(color: Colors.grey.withValues(alpha: 0.35)),
                              borderRadius: BorderRadius.circular(6),
                            ),
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Row(
                                  children: [
                                    const Text('Value is ', style: TextStyle(fontSize: 12)),
                                    const SizedBox(width: 6),
                                    DropdownButton<String>(
                                      value: r.operator,
                                      isDense: true,
                                      items: const [
                                        DropdownMenuItem(value: '<', child: Text('less than (<)')),
                                        DropdownMenuItem(value: '<=', child: Text('less or equal (<=)')),
                                        DropdownMenuItem(value: '=', child: Text('equal to (=)')),
                                        DropdownMenuItem(value: '!=', child: Text('not equal (!=)')),
                                        DropdownMenuItem(value: '>', child: Text('greater than (>)')),
                                        DropdownMenuItem(value: '>=', child: Text('greater or equal (>=)')),
                                        DropdownMenuItem(value: 'contains', child: Text('contains')),
                                        DropdownMenuItem(value: 'is_empty', child: Text('is empty')),
                                        DropdownMenuItem(value: 'is_not_empty', child: Text('is not empty')),
                                      ],
                                      onChanged: (op) {
                                        if (op != null) {
                                          setModalState(() => rules[i] = r.copyWith(operator: op));
                                        }
                                      },
                                    ),
                                    const SizedBox(width: 8),
                                    if (r.operator != 'is_empty' && r.operator != 'is_not_empty')
                                      Expanded(
                                        child: TextFormField(
                                          initialValue: r.value,
                                          decoration: const InputDecoration(
                                            hintText: '0',
                                            isDense: true,
                                            border: OutlineInputBorder(),
                                            contentPadding: EdgeInsets.symmetric(
                                              horizontal: 8,
                                              vertical: 6,
                                            ),
                                          ),
                                          style: const TextStyle(fontSize: 12),
                                          onChanged: (val) =>
                                              setModalState(() => rules[i] = r.copyWith(value: val)),
                                        ),
                                      )
                                    else
                                      const Spacer(),
                                    IconButton(
                                      icon: const Icon(Icons.delete_outline, size: 16, color: Colors.red),
                                      onPressed: () => setModalState(() => rules.removeAt(i)),
                                    ),
                                  ],
                                ),
                                const SizedBox(height: 8),
                                Wrap(
                                  spacing: 8,
                                  crossAxisAlignment: WrapCrossAlignment.center,
                                  children: [
                                    FilterChip(
                                      label: const Text('Bold', style: TextStyle(fontSize: 11)),
                                      selected: r.bold,
                                      visualDensity: VisualDensity.compact,
                                      onSelected: (b) =>
                                          setModalState(() => rules[i] = r.copyWith(bold: b)),
                                    ),
                                    FilterChip(
                                      label: const Text('Italic', style: TextStyle(fontSize: 11)),
                                      selected: r.italic,
                                      visualDensity: VisualDensity.compact,
                                      onSelected: (b) =>
                                          setModalState(() => rules[i] = r.copyWith(italic: b)),
                                    ),
                                    const Text('Text:', style: TextStyle(fontSize: 11)),
                                    for (final hex in const ['#EF4444', '#2E7D32', '#1E88E5', '#F57C00', '#000000'])
                                      InkWell(
                                        onTap: () => setModalState(
                                          () => rules[i] = r.copyWith(textColor: hex),
                                        ),
                                        child: Container(
                                          width: 18,
                                          height: 18,
                                          decoration: BoxDecoration(
                                            color: parseLayoutColor(hex),
                                            shape: BoxShape.circle,
                                            border: Border.all(
                                              color: r.textColor == hex
                                                  ? const Color(0xFF0284C7)
                                                  : Colors.black26,
                                              width: r.textColor == hex ? 2.5 : 1,
                                            ),
                                          ),
                                        ),
                                      ),
                                    const SizedBox(width: 4),
                                    const Text('Fill:', style: TextStyle(fontSize: 11)),
                                    for (final hex in const ['#FEE2E2', '#DCFCE7', '#DBEAFE', '#FEF3C7'])
                                      InkWell(
                                        onTap: () => setModalState(
                                          () => rules[i] = r.copyWith(fillColor: hex),
                                        ),
                                        child: Container(
                                          width: 18,
                                          height: 18,
                                          decoration: BoxDecoration(
                                            color: parseLayoutColor(hex),
                                            borderRadius: BorderRadius.circular(3),
                                            border: Border.all(
                                              color: r.fillColor == hex
                                                  ? const Color(0xFF0284C7)
                                                  : Colors.black26,
                                              width: r.fillColor == hex ? 2.5 : 1,
                                            ),
                                          ),
                                        ),
                                      ),
                                  ],
                                ),
                              ],
                            ),
                          );
                        },
                      ),
                    ),
                  const SizedBox(height: 12),
                  Row(
                    children: [
                      OutlinedButton.icon(
                        icon: const Icon(Icons.add, size: 16),
                        label: const Text('Add Rule'),
                        onPressed: () {
                          setModalState(() {
                            rules.add(
                              const ConditionalFormatRule(
                                operator: '<',
                                value: '0',
                                textColor: '#EF4444',
                                bold: true,
                              ),
                            );
                          });
                        },
                      ),
                      const SizedBox(width: 8),
                      if (rules.isNotEmpty)
                        TextButton(
                          onPressed: () => setModalState(rules.clear),
                          child: const Text('Clear All'),
                        ),
                    ],
                  ),
                ],
              ),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(ctx).pop(),
                child: const Text('Cancel'),
              ),
              FilledButton(
                onPressed: () => Navigator.of(ctx).pop(
                  initialStyle.copyWith(conditionalRules: rules),
                ),
                child: const Text('Save Rules'),
              ),
            ],
          );
        },
      ),
    );
  }
}
