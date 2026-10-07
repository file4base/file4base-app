// #32 — a report: the found set drawn through a layout's parts, with records
// grouped under their sub-summaries and totalled.
//
// A report is read-only. It is what Preview and the printed sheet show, and
// what List view shows for a layout that has summary parts.

import 'package:flutter/material.dart';

import '../../core/api/api_client.dart';
import '../layout_engine/layout_object_visuals.dart';
import '../layout_engine/models/layout_definition.dart';

/// One band of a report: a layout part drawn once, against a record or against
/// a set of figures.
class ReportBand {
  final LayoutPartModel part;

  /// The record a body band draws. Null for a summary band.
  final Map<String, dynamic>? record;

  /// What each summary field comes to over this band's scope.
  final Map<String, dynamic> summaries;

  /// The break values this band is for, so a sub-summary can show the field it
  /// broke on.
  final Map<String, dynamic> breakValues;

  /// How many records this band covers. Zero for a body band.
  final int count;

  const ReportBand({
    required this.part,
    this.record,
    this.summaries = const {},
    this.breakValues = const {},
    this.count = 0,
  });
}

/// The figures a report needs: one grouping per break level, where level `n`
/// is grouped by the first `n` break fields and level 0 is the grand totals.
typedef ReportFigures = Map<int, SummaryResultModel>;

/// Lays a sorted found set out as report bands.
///
/// The records must already be sorted by the break fields, outermost first;
/// [reportSortMismatch] says when they are not.
List<ReportBand> buildReportBands({
  required LayoutDefinitionModel layout,
  required List<Map<String, dynamic>> records,
  required ReportFigures figures,
  required Map<String, SummarySpecModel> specs,
}) {
  final breakFields = layout.breakFields;
  final leading = layout.leadingSubSummaries;
  final trailing = layout.trailingSubSummaries;
  final bands = <ReportBand>[];

  final grand = figures[0] ?? const SummaryResultModel();

  // A running total counts down the records as they are drawn, so it belongs
  // to the order they are in rather than to a group.
  final running = <String, double>{};
  Map<String, dynamic> runningValues(Map<String, dynamic> record) {
    final out = <String, dynamic>{};
    specs.forEach((name, spec) {
      if (!spec.running) return;
      final raw = record[spec.field];
      final value = double.tryParse(raw?.toString() ?? '') ?? 0;
      running[name] = (running[name] ?? 0) + value;
      out[name] = _trimNumber(running[name]!);
    });
    return out;
  }

  /// The level a sub-summary part groups at: how many break fields, counting
  /// from the outside, are needed to identify its group.
  int levelOf(LayoutPartModel part) {
    final field = part.breakField;
    if (field == null) return -1;
    final index = breakFields.indexOf(field);
    return index < 0 ? -1 : index + 1;
  }

  /// The group figures for a record at a given level.
  SummaryGroupModel? groupAt(int level, Map<String, dynamic> record) =>
      figures[level]?.groupOf(record);

  Map<String, dynamic> breakValuesAt(int level, Map<String, dynamic> record) => {
        for (final field in breakFields.take(level)) field: record[field],
      };

  void emitSubSummaries(
    List<LayoutPartModel> parts,
    int level,
    Map<String, dynamic> record,
  ) {
    for (final part in parts) {
      if (levelOf(part) != level) continue;
      final group = groupAt(level, record);
      bands.add(ReportBand(
        part: part,
        summaries: group?.summaries ?? const {},
        breakValues: breakValuesAt(level, record),
        count: group?.count ?? 0,
      ));
    }
  }

  if (layout.leadingGrandSummary != null) {
    bands.add(ReportBand(
      part: layout.leadingGrandSummary!,
      summaries: grand.grand,
      count: grand.count,
    ));
  }

  final body = layout.parts.where((p) => p.isBody).firstOrNull;
  final levels = breakFields.length;

  Map<String, dynamic>? previousRecord;
  List<String>? previousKeys;

  for (final record in records) {
    final keys = [for (final field in breakFields) record[field]?.toString() ?? ''];

    // The outermost break field whose value changed. Every group from there
    // inwards ends here and a new one starts.
    var changedAt = levels;
    if (previousKeys == null) {
      changedAt = 0;
    } else {
      for (var i = 0; i < keys.length; i++) {
        if (keys[i] != previousKeys[i]) {
          changedAt = i;
          break;
        }
      }
    }

    // Groups end innermost first, and are totalled over the records that were
    // in them — so the band is built from the record that just ended, not the
    // one that starts the next group.
    if (previousRecord != null) {
      for (var level = levels; level > changedAt; level--) {
        emitSubSummaries(trailing, level, previousRecord);
      }
    }
    // And start outermost first.
    for (var level = changedAt + 1; level <= levels; level++) {
      emitSubSummaries(leading, level, record);
    }

    if (body != null) {
      bands.add(ReportBand(
        part: body,
        record: record,
        summaries: runningValues(record),
      ));
    }

    previousRecord = record;
    previousKeys = keys;
  }

  // The last group has nothing after it to close it, so it is closed here.
  if (previousRecord != null) {
    for (var level = levels; level > 0; level--) {
      emitSubSummaries(trailing, level, previousRecord);
    }
  }

  if (layout.trailingGrandSummary != null) {
    bands.add(ReportBand(
      part: layout.trailingGrandSummary!,
      summaries: grand.grand,
      count: grand.count,
    ));
  }

  return bands;
}

/// True when the found set is not in the order this report needs, so the
/// groups would interleave instead of reading one after another.
bool reportSortMismatch(LayoutDefinitionModel layout, List<String> sortedBy) {
  final required = layout.requiredSortOrder;
  if (required.isEmpty) return false;
  if (sortedBy.length < required.length) return true;
  for (var i = 0; i < required.length; i++) {
    if (sortedBy[i] != required[i]) return true;
  }
  return false;
}

String _trimNumber(double value) {
  if (value == value.roundToDouble() && value.abs() < 1e15) {
    return value.toInt().toString();
  }
  return value.toString();
}

/// How a summary figure is shown. An average or a standard deviation comes
/// back at the engine's full precision, which is not a thing to read in a
/// report, so it is shown to two decimals unless it is exact.
String formatSummaryValue(Object? raw, String? summaryType) {
  final text = raw?.toString() ?? '';
  if (text.isEmpty) return '';
  final value = double.tryParse(text);
  if (value == null) return text;

  if (summaryType == SummaryType.fractionOfTotal) {
    return '${(value * 100).toStringAsFixed(1)}%';
  }
  if (value == value.roundToDouble() && value.abs() < 1e15) {
    return value.toInt().toString();
  }
  if (summaryType == SummaryType.average || summaryType == SummaryType.standardDeviation) {
    return value.toStringAsFixed(2);
  }
  return text;
}

/// Draws a found set as a report.
class ReportView extends StatelessWidget {
  final LayoutDefinitionModel layout;
  final TableModel table;

  /// The found set, already sorted by the report's break fields.
  final List<Map<String, dynamic>> records;

  /// The figures, one grouping per break level (see [ReportFigures]).
  final ReportFigures figures;

  /// The summary fields of the table, by name.
  final Map<String, SummarySpecModel> specs;

  /// What a stored value looks like in a field box.
  final String Function(ColumnModel column, Object? raw) formatValue;

  /// The fields the found set is sorted by, outermost first, so the report can
  /// say when it is in the wrong order.
  final List<String> sortedBy;

  /// Called when the reader asks for the found set to be sorted the way the
  /// report needs. Null hides the offer.
  final VoidCallback? onSortForReport;

  final bool isDark;

  /// Leaves out the warning and the page chrome, for printing.
  final bool forPrint;

  /// The width the report has to fit into, when that is narrower than the
  /// layout. A report wider than the paper is scaled down rather than having
  /// its rightmost columns fall off the sheet (#32).
  final double? fitWidth;

  const ReportView({
    super.key,
    required this.layout,
    required this.table,
    required this.records,
    required this.figures,
    required this.specs,
    required this.formatValue,
    this.sortedBy = const [],
    this.onSortForReport,
    this.isDark = false,
    this.forPrint = false,
    this.fitWidth,
  });

  ColumnModel? _column(String name) =>
      table.columns.where((c) => c.name.toLowerCase() == name.toLowerCase()).firstOrNull;

  @override
  Widget build(BuildContext context) {
    final bands = buildReportBands(
      layout: layout,
      records: records,
      figures: figures,
      specs: specs,
    );
    final mismatch = !forPrint && reportSortMismatch(layout, sortedBy);

    // Scaled to the sheet when the layout is wider than the paper, so the
    // whole report prints instead of its right-hand columns being cut off.
    final scale = fitWidth != null && fitWidth! > 0 && layout.width > fitWidth!
        ? fitWidth! / layout.width
        : 1.0;

    return SingleChildScrollView(
      padding: EdgeInsets.all(forPrint ? 0 : 20),
      child: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (mismatch) _sortWarning(context),
            if (scale < 1.0)
              SizedBox(
                width: layout.width * scale,
                height: _reportHeight(bands) * scale,
                child: FittedBox(
                  fit: BoxFit.fill,
                  alignment: Alignment.topLeft,
                  child: SizedBox(
                    width: layout.width,
                    height: _reportHeight(bands),
                    child: _sheet(bands),
                  ),
                ),
              )
            else
              _sheet(bands),
          ],
        ),
      ),
    );
  }

  /// How tall the report is once its bands are laid out.
  double _reportHeight(List<ReportBand> bands) {
    var height = 0.0;
    for (final part in layout.parts) {
      if (part.type == LayoutPartType.header || part.type == LayoutPartType.footer) {
        height += part.height;
      }
    }
    for (final band in bands) {
      height += band.part.height;
    }
    return height;
  }

  Widget _sheet(List<ReportBand> bands) {
    return Container(
              width: layout.width,
              decoration: forPrint
                  ? null
                  : BoxDecoration(
                      color: isDark ? const Color(0xFF161B22) : Colors.white,
                      borderRadius: BorderRadius.circular(8),
                      border: Border.all(
                        color: isDark ? const Color(0xFF38404B) : const Color(0xFFD1CFCA),
                      ),
                    ),
              clipBehavior: forPrint ? Clip.none : Clip.antiAlias,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  for (final part in layout.parts)
                    if (part.type == LayoutPartType.header) _band(ReportBand(part: part), 'header'),
                  for (final (i, band) in bands.indexed) _band(band, '$i'),
                  for (final part in layout.parts)
                    if (part.type == LayoutPartType.footer) _band(ReportBand(part: part), 'footer'),
                ],
              ),
            );
  }

  Widget _sortWarning(BuildContext context) {
    final needed = layout.requiredSortOrder
        .map((f) => _column(f)?.displayName ?? f)
        .join(', then ');
    return Container(
      key: const ValueKey('report-sort-warning'),
      width: layout.width,
      margin: const EdgeInsets.only(bottom: 10),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: Colors.orange.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(6),
        border: Border.all(color: Colors.orange.shade700),
      ),
      child: Row(
        children: [
          Icon(Icons.sort, size: 16, color: Colors.orange.shade800),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              'This report groups by $needed. The records are not in that order, '
              'so the groups below are split up rather than totalled once each.',
              style: TextStyle(fontSize: 11.5, color: Colors.orange.shade900),
            ),
          ),
          if (onSortForReport != null)
            TextButton(
              key: const ValueKey('report-sort-fix'),
              onPressed: onSortForReport,
              child: const Text('Sort for this report', style: TextStyle(fontSize: 11.5)),
            ),
        ],
      ),
    );
  }

  /// Draws one band. [slot] makes its key unique: the same group can open
  /// more than once when the records are not in the report's order, which is
  /// what the warning above is about — it must still draw rather than fail on
  /// two bands sharing a key.
  Widget _band(ReportBand band, String slot) {
    final part = band.part;
    final top = layout.partTop(part.id);
    final objects = layout.objectsIn(part);

    return Container(
      key: ValueKey('band-$slot-${part.id}'),
      height: part.height,
      width: layout.width,
      decoration: BoxDecoration(
        color: part.isSummaryPart
            ? (isDark ? Colors.white.withValues(alpha: 0.04) : const Color(0xFFF2F5F9))
            : null,
        border: part.isSummaryPart
            ? Border(
                top: BorderSide(
                  color: isDark ? const Color(0xFF38404B) : const Color(0xFFD7DCE3),
                ),
              )
            : null,
      ),
      child: Stack(
        clipBehavior: Clip.none,
        children: [
          for (final obj in objects)
            Positioned(
              left: obj.x,
              top: obj.y - top,
              width: obj.width,
              height: obj.height,
              child: _object(obj, band),
            ),
        ],
      ),
    );
  }

  Widget _object(LayoutObjectModel obj, ReportBand band) {
    if (obj.type == 'label') {
      return Container(
        alignment: _alignment(obj.style.textAlign),
        padding: const EdgeInsets.symmetric(horizontal: 4),
        child: Text(
          obj.text,
          textAlign: _textAlign(obj.style.textAlign),
          overflow: TextOverflow.ellipsis,
          style: TextStyle(
            fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 13,
            fontWeight: obj.style.fontWeight == 'bold' ? FontWeight.bold : FontWeight.normal,
            color: obj.style.textColor != null
                ? parseLayoutColor(obj.style.textColor)
                : (isDark ? Colors.white : Colors.black87),
          ),
        ),
      );
    }

    if (obj.type == 'field') {
      return _field(obj, band);
    }

    return buildDrawnLayoutObject(obj, record: band.record) ?? const SizedBox.shrink();
  }

  /// What a field shows in this band.
  ///
  /// A summary field shows the band's own figure: the group's in a
  /// sub-summary, the whole found set's in a grand summary, and its running
  /// value in the body. A plain field shows the record in a body band, and in
  /// a summary band the value the group broke on — which is how a sub-summary
  /// names the group it is totalling.
  Widget _field(LayoutObjectModel obj, ReportBand band) {
    final name = obj.fieldBinding?.fieldName ?? '';
    final column = _column(name);
    final spec = specs[name];

    String text;
    if (spec != null) {
      text = formatSummaryValue(band.summaries[name], spec.summaryType);
    } else if (band.record != null) {
      text = column == null ? '' : formatValue(column, band.record![name]);
    } else if (band.breakValues.containsKey(name)) {
      final raw = band.breakValues[name];
      text = column == null ? (raw?.toString() ?? '') : formatValue(column, raw);
    } else {
      text = '';
    }

    final emphasised = band.part.isSummaryPart;
    return Container(
      alignment: _alignment(obj.style.textAlign),
      padding: const EdgeInsets.symmetric(horizontal: 4),
      child: Text(
        text,
        textAlign: _textAlign(obj.style.textAlign),
        overflow: TextOverflow.ellipsis,
        style: TextStyle(
          fontSize: obj.style.fontSize > 0 ? obj.style.fontSize : 13,
          fontWeight: obj.style.fontWeight == 'bold' || (emphasised && spec != null)
              ? FontWeight.bold
              : FontWeight.normal,
          color: obj.style.textColor != null
              ? parseLayoutColor(obj.style.textColor)
              : (isDark ? Colors.white : Colors.black87),
        ),
      ),
    );
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
