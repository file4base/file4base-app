// #33 — a sheet of labels. The layout is one label; this lays them out across
// and down the page, one record each, which is the only way a labels layout
// means anything.

import 'package:flutter/material.dart';

import 'layout_object_visuals.dart';
import 'models/layout_blueprint.dart';
import 'models/layout_definition.dart';

/// Draws one page of labels.
class LabelSheetView extends StatelessWidget {
  final LayoutDefinitionModel layout;
  final LabelStock stock;

  /// The records on this page, at most [LabelStock.perSheet] of them.
  final List<Map<String, dynamic>> records;

  final String? userName;

  /// How a field's stored value is written inside merge text.
  final String Function(String fieldName, Object? value)? formatField;

  const LabelSheetView({
    super.key,
    required this.layout,
    required this.stock,
    required this.records,
    this.userName,
    this.formatField,
  });

  /// The records split into pages, one page per sheet of labels.
  static List<List<Map<String, dynamic>>> paginate(
    List<Map<String, dynamic>> records,
    LabelStock stock,
  ) {
    final perSheet = stock.perSheet;
    if (perSheet <= 0) return [records];
    return [
      for (var i = 0; i < records.length; i += perSheet)
        records.sublist(i, (i + perSheet).clamp(0, records.length)),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final cellWidth = stock.labelWidthPt + stock.gutterXPt;
    final cellHeight = stock.labelHeightPt + stock.gutterYPt;

    return SizedBox(
      key: const ValueKey('label-sheet'),
      width: stock.marginLeftPt + stock.across * cellWidth,
      height: stock.marginTopPt + stock.down * cellHeight,
      child: Stack(
        children: [
          for (final (i, record) in records.indexed)
            if (i < stock.perSheet)
              Positioned(
                left: stock.marginLeftPt + (i % stock.across) * cellWidth,
                top: stock.marginTopPt + (i ~/ stock.across) * cellHeight,
                width: stock.labelWidthPt,
                height: stock.labelHeightPt,
                child: _label(record, i),
              ),
        ],
      ),
    );
  }

  Widget _label(Map<String, dynamic> record, int index) {
    return SizedBox(
      key: ValueKey('label-$index'),
      width: stock.labelWidthPt,
      height: stock.labelHeightPt,
      child: Stack(
        clipBehavior: Clip.hardEdge,
        children: [
          for (final obj in layout.objects)
            Positioned(
              left: obj.x,
              top: obj.y,
              width: obj.width,
              height: obj.height,
              child: _object(obj, record),
            ),
        ],
      ),
    );
  }

  Widget _object(LayoutObjectModel obj, Map<String, dynamic> record) {
    if (obj.type == 'label') {
      final text = resolveLayoutMergeText(
        obj.text,
        record: record,
        userName: userName,
        formatField: formatField,
        // The point of a label: an empty field takes its line with it, so a
        // two-line address does not print with a gap in the middle.
        collapseEmptyLines: true,
      );
      return Align(
        alignment: _alignment(obj.style.textAlign),
        child: Text(
          text,
          textAlign: _textAlign(obj.style.textAlign),
          style: TextStyle(
            fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 11,
            fontWeight: obj.style.fontWeight == 'bold' ? FontWeight.bold : FontWeight.normal,
            color: obj.style.textColor != null ? parseLayoutColor(obj.style.textColor) : Colors.black,
            height: 1.25,
          ),
        ),
      );
    }

    if (obj.type == 'field') {
      final name = obj.fieldBinding?.fieldName ?? '';
      final raw = record[name];
      final text = formatField == null ? (raw?.toString() ?? '') : formatField!(name, raw);
      return Align(
        alignment: _alignment(obj.style.textAlign),
        child: Text(
          text,
          textAlign: _textAlign(obj.style.textAlign),
          overflow: TextOverflow.ellipsis,
          style: TextStyle(
            fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 11,
            fontWeight: obj.style.fontWeight == 'bold' ? FontWeight.bold : FontWeight.normal,
          ),
        ),
      );
    }

    return buildDrawnLayoutObject(obj, record: record, formatField: formatField) ??
        const SizedBox.shrink();
  }

  Alignment _alignment(String align) => switch (align) {
        'center' => Alignment.center,
        'right' => Alignment.centerRight,
        _ => Alignment.centerLeft,
      };

  TextAlign _textAlign(String align) => switch (align) {
        'center' => TextAlign.center,
        'right' => TextAlign.right,
        _ => TextAlign.left,
      };
}
