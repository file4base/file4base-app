// #37 — drawing a chart. The numbers come from the summary figures #32 works
// out, except for a scatter, which plots two numbers per record.

import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../../core/api/api_client.dart';
import 'models/chart_definition.dart';

/// The colours series take when none was chosen, in order.
const kChartPalette = <Color>[
  Color(0xFF1E88E5),
  Color(0xFF43A047),
  Color(0xFFF4511E),
  Color(0xFF8E24AA),
  Color(0xFFFDD835),
  Color(0xFF00ACC1),
  Color(0xFF6D4C41),
  Color(0xFFD81B60),
];

Color chartSeriesColor(ChartSeriesModel series, int index) {
  final hex = series.color;
  if (hex != null && hex.startsWith('#') && (hex.length == 7 || hex.length == 9)) {
    final value = int.tryParse(hex.substring(1), radix: 16);
    if (value != null) {
      return Color(hex.length == 7 ? (0xFF000000 | value) : value);
    }
  }
  return kChartPalette[index % kChartPalette.length];
}

/// Builds what a grouped chart draws from the figures of a report (#32).
///
/// Each group is a category along the bottom; each series is one summary
/// field's figure for that group.
ChartData chartDataFromSummary(
  ChartConfigModel config,
  SummaryResultModel? figures, {
  String Function(String fieldName, Object? value)? formatCategory,
}) {
  final series = config.drawnSeries;
  final category = config.categoryField;
  if (figures == null || series.isEmpty || category == null || category.isEmpty) {
    return const ChartData();
  }

  return ChartData(
    seriesLabels: [for (final s in series) s.label.isEmpty ? s.field : s.label],
    points: [
      for (final group in figures.groups)
        ChartPoint(
          category: formatCategory == null
              ? (group.values[category]?.toString() ?? '')
              : formatCategory(category, group.values[category]),
          values: [
            for (final s in series) double.tryParse(group.summaries[s.field]?.toString() ?? ''),
          ],
        ),
    ],
  );
}

/// Builds what a scatter chart draws: one point per record, two numbers each.
ChartData chartDataFromRecords(
  ChartConfigModel config,
  List<Map<String, dynamic>> records,
) {
  final series = config.drawnSeries;
  final xField = config.categoryField;
  if (series.isEmpty || xField == null || xField.isEmpty) return const ChartData();
  final yField = series.first.field;

  final points = <ChartPoint>[];
  for (final record in records) {
    final x = double.tryParse(record[xField]?.toString() ?? '');
    final y = double.tryParse(record[yField]?.toString() ?? '');
    // A record missing either number is not a point; it is left out rather
    // than drawn at zero, which would be a lie about the data.
    if (x == null || y == null) continue;
    points.add(ChartPoint(category: x.toString(), values: [y]));
  }

  return ChartData(
    seriesLabels: [series.first.label.isEmpty ? yField : series.first.label],
    points: points,
  );
}

/// How a figure is written on a chart: whole numbers without decimals, the
/// rest to two.
String chartValueText(double value) {
  if (value == value.roundToDouble() && value.abs() < 1e15) return value.toInt().toString();
  return value.toStringAsFixed(2);
}

/// Draws a chart into the space it is given.
class ChartView extends StatelessWidget {
  final ChartConfigModel config;
  final ChartData data;
  final bool isDark;

  /// Shown instead of the chart when there is nothing to draw.
  final String? notice;

  const ChartView({
    super.key,
    required this.config,
    required this.data,
    this.isDark = false,
    this.notice,
  });

  @override
  Widget build(BuildContext context) {
    final foreground = isDark ? Colors.white70 : Colors.black87;

    if (notice != null || data.isEmpty) {
      return Center(
        key: const ValueKey('chart-notice'),
        child: Padding(
          padding: const EdgeInsets.all(8),
          child: Text(
            notice ?? 'Nothing to chart yet',
            textAlign: TextAlign.center,
            style: TextStyle(fontSize: 11, color: isDark ? Colors.white54 : Colors.black54),
          ),
        ),
      );
    }

    return LayoutBuilder(builder: (context, constraints) {
      // A chart object can be dragged small. The title and the legend are the
      // first things to go, so what is left is still a chart rather than an
      // overflow.
      final showTitle = config.title.isNotEmpty && constraints.maxHeight > 90;
      final hasLegend = config.showLegend &&
          (data.seriesLabels.length > 1 || config.chartType == ChartType.pie);
      final showLegend = hasLegend && constraints.maxHeight > 110 && constraints.maxWidth > 120;

      return Column(
      key: const ValueKey('chart'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (showTitle)
          Padding(
            padding: const EdgeInsets.only(top: 4, bottom: 2),
            child: Text(
              config.title,
              textAlign: TextAlign.center,
              style: TextStyle(fontSize: 12, fontWeight: FontWeight.bold, color: foreground),
            ),
          ),
        Expanded(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(6, 2, 8, 2),
            child: CustomPaint(
              painter: _ChartPainter(config: config, data: data, isDark: isDark),
              child: const SizedBox.expand(),
            ),
          ),
        ),
        if (showLegend) _legend(foreground, constraints.maxWidth),
      ],
      );
    });
  }

  Widget _legend(Color foreground, double maxWidth) {
    // A pie's legend names its slices; every other chart's names its series.
    final entries = config.chartType == ChartType.pie
        ? [for (final p in data.points) p.category]
        : data.seriesLabels;

    return Padding(
      padding: const EdgeInsets.only(bottom: 4, left: 6, right: 6),
      child: Wrap(
        alignment: WrapAlignment.center,
        spacing: 10,
        runSpacing: 2,
        children: [
          for (final (i, label) in entries.indexed)
            // Each entry is capped so a long category name shortens instead of
            // pushing the legend past the chart's edge.
            ConstrainedBox(
              constraints: BoxConstraints(maxWidth: math.max(40, maxWidth - 16)),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Container(
                    width: 8,
                    height: 8,
                    decoration: BoxDecoration(
                      color: config.chartType == ChartType.pie
                          ? kChartPalette[i % kChartPalette.length]
                          : chartSeriesColor(config.drawnSeries[i], i),
                      borderRadius: BorderRadius.circular(2),
                    ),
                  ),
                  const SizedBox(width: 3),
                  Flexible(
                    child: Text(
                      label,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 9, color: foreground),
                    ),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class _ChartPainter extends CustomPainter {
  final ChartConfigModel config;
  final ChartData data;
  final bool isDark;

  _ChartPainter({required this.config, required this.data, required this.isDark});

  Color get _axis => isDark ? Colors.white24 : Colors.black26;
  Color get _text => isDark ? Colors.white70 : Colors.black87;

  @override
  void paint(Canvas canvas, Size size) {
    if (data.isEmpty || size.width < 20 || size.height < 20) return;
    switch (config.chartType) {
      case ChartType.pie:
        _paintPie(canvas, size);
      case ChartType.bar:
        _paintBars(canvas, size, horizontal: true);
      case ChartType.line:
      case ChartType.area:
        _paintLine(canvas, size, filled: config.chartType == ChartType.area);
      case ChartType.scatter:
        _paintScatter(canvas, size);
      default:
        _paintBars(canvas, size, horizontal: false);
    }
  }

  void _label(
    Canvas canvas,
    String text,
    Offset at, {
    double fontSize = 8,
    TextAlign align = TextAlign.center,
    double maxWidth = 80,
    int maxLines = 1,
  }) {
    if (text.isEmpty) return;
    final painter = TextPainter(
      text: TextSpan(text: text, style: TextStyle(fontSize: fontSize, color: _text)),
      textDirection: TextDirection.ltr,
      textAlign: align,
      maxLines: maxLines,
      ellipsis: '…',
    )..layout(maxWidth: maxWidth);
    final dx = switch (align) {
      TextAlign.center => at.dx - painter.width / 2,
      TextAlign.right => at.dx - painter.width,
      _ => at.dx,
    };
    painter.paint(canvas, Offset(dx, at.dy));
  }

  /// The space the plot itself gets, once the axis labels have theirs.
  Rect _plotArea(Size size, {required bool horizontal}) {
    final left = horizontal ? 58.0 : 34.0;
    const right = 6.0;
    const top = 8.0;
    final bottom = horizontal ? 16.0 : 20.0;
    return Rect.fromLTRB(left, top, size.width - right, size.height - bottom);
  }

  void _paintAxes(Canvas canvas, Rect plot) {
    final paint = Paint()
      ..color = _axis
      ..strokeWidth = 1;
    canvas.drawLine(plot.bottomLeft, plot.bottomRight, paint);
    canvas.drawLine(plot.topLeft, plot.bottomLeft, paint);
  }

  void _paintBars(Canvas canvas, Size size, {required bool horizontal}) {
    final plot = _plotArea(size, horizontal: horizontal);
    if (plot.width <= 0 || plot.height <= 0) return;
    _paintAxes(canvas, plot);

    final max = math.max(data.maxValue, 1e-9);
    final seriesCount = data.seriesLabels.length;
    final groupCount = data.points.length;
    if (groupCount == 0) return;

    final groupSpan = (horizontal ? plot.height : plot.width) / groupCount;
    final barSpan = groupSpan / (seriesCount + 0.5);

    for (final (g, point) in data.points.indexed) {
      for (var s = 0; s < seriesCount; s++) {
        final value = s < point.values.length ? point.values[s] : null;
        if (value == null || value <= 0) continue;
        final length = (value / max) * (horizontal ? plot.width : plot.height);
        final paint = Paint()..color = chartSeriesColor(config.drawnSeries[s], s);

        final Rect rect;
        if (horizontal) {
          final top = plot.top + g * groupSpan + s * barSpan + barSpan * 0.25;
          rect = Rect.fromLTWH(plot.left, top, length, barSpan * 0.8);
        } else {
          final left = plot.left + g * groupSpan + s * barSpan + barSpan * 0.25;
          rect = Rect.fromLTWH(left, plot.bottom - length, barSpan * 0.8, length);
        }
        canvas.drawRRect(
          RRect.fromRectAndRadius(rect, const Radius.circular(2)),
          paint,
        );

        if (config.showValues && barSpan > 14) {
          _label(
            canvas,
            chartValueText(value),
            horizontal
                ? Offset(rect.right + 3, rect.center.dy - 5)
                : Offset(rect.center.dx, rect.top - 10),
            align: horizontal ? TextAlign.left : TextAlign.center,
            maxWidth: horizontal ? 50 : barSpan * 2,
          );
        }
      }

      // The category, along the bottom or up the side.
      final centre = plot.top + (g + 0.5) * groupSpan;
      if (horizontal) {
        _label(canvas, point.category, Offset(plot.left - 4, centre - 5),
            align: TextAlign.right, maxWidth: 52);
      } else {
        _label(canvas, point.category,
            Offset(plot.left + (g + 0.5) * groupSpan, plot.bottom + 4),
            maxWidth: groupSpan);
      }
    }

    _paintValueAxis(canvas, plot, max, horizontal: horizontal);
  }

  /// The scale up the side (or along the bottom for a bar chart): nothing and
  /// the largest value, which is enough to read a figure against.
  void _paintValueAxis(Canvas canvas, Rect plot, double max, {required bool horizontal}) {
    if (horizontal) {
      _label(canvas, '0', Offset(plot.left, plot.bottom + 3), maxWidth: 30);
      _label(canvas, chartValueText(max), Offset(plot.right, plot.bottom + 3),
          align: TextAlign.right, maxWidth: 50);
    } else {
      _label(canvas, chartValueText(max), Offset(plot.left - 4, plot.top - 2),
          align: TextAlign.right, maxWidth: 30);
      _label(canvas, '0', Offset(plot.left - 4, plot.bottom - 9),
          align: TextAlign.right, maxWidth: 30);
    }
  }

  void _paintLine(Canvas canvas, Size size, {required bool filled}) {
    final plot = _plotArea(size, horizontal: false);
    if (plot.width <= 0 || plot.height <= 0) return;
    _paintAxes(canvas, plot);

    final max = math.max(data.maxValue, 1e-9);
    final count = data.points.length;
    if (count == 0) return;
    final step = count == 1 ? 0.0 : plot.width / (count - 1);

    for (var s = 0; s < data.seriesLabels.length; s++) {
      final colour = chartSeriesColor(config.drawnSeries[s], s);
      final path = Path();
      final dots = <Offset>[];
      var started = false;

      for (var i = 0; i < count; i++) {
        final values = data.points[i].values;
        final value = s < values.length ? values[s] : null;
        if (value == null) continue;
        final x = count == 1 ? plot.center.dx : plot.left + i * step;
        final y = plot.bottom - (value / max) * plot.height;
        dots.add(Offset(x, y));
        if (!started) {
          path.moveTo(x, y);
          started = true;
        } else {
          path.lineTo(x, y);
        }
      }
      if (dots.isEmpty) continue;

      if (filled) {
        final area = Path.from(path)
          ..lineTo(dots.last.dx, plot.bottom)
          ..lineTo(dots.first.dx, plot.bottom)
          ..close();
        canvas.drawPath(area, Paint()..color = colour.withValues(alpha: 0.22));
      }
      canvas.drawPath(
        path,
        Paint()
          ..color = colour
          ..strokeWidth = 1.8
          ..style = PaintingStyle.stroke,
      );
      for (final dot in dots) {
        canvas.drawCircle(dot, 2.5, Paint()..color = colour);
      }
      if (config.showValues && step > 24) {
        for (final (i, dot) in dots.indexed) {
          final values = data.points[i].values;
          final value = s < values.length ? values[s] : null;
          if (value == null) continue;
          _label(canvas, chartValueText(value), Offset(dot.dx, dot.dy - 12), maxWidth: step);
        }
      }
    }

    for (var i = 0; i < count; i++) {
      final x = count == 1 ? plot.center.dx : plot.left + i * step;
      _label(canvas, data.points[i].category, Offset(x, plot.bottom + 4),
          maxWidth: step <= 0 ? plot.width : step);
    }
    _paintValueAxis(canvas, plot, max, horizontal: false);
  }

  void _paintScatter(Canvas canvas, Size size) {
    final plot = _plotArea(size, horizontal: false);
    if (plot.width <= 0 || plot.height <= 0) return;
    _paintAxes(canvas, plot);

    var maxX = 0.0, maxY = 0.0;
    for (final point in data.points) {
      final x = double.tryParse(point.category) ?? 0;
      final y = point.values.isEmpty ? null : point.values.first;
      if (x > maxX) maxX = x;
      if (y != null && y > maxY) maxY = y;
    }
    maxX = math.max(maxX, 1e-9);
    maxY = math.max(maxY, 1e-9);

    final paint = Paint()..color = chartSeriesColor(config.drawnSeries.first, 0);
    for (final point in data.points) {
      final x = double.tryParse(point.category) ?? 0;
      final y = point.values.isEmpty ? null : point.values.first;
      if (y == null) continue;
      canvas.drawCircle(
        Offset(plot.left + (x / maxX) * plot.width, plot.bottom - (y / maxY) * plot.height),
        2.5,
        paint,
      );
    }

    _label(canvas, chartValueText(maxY), Offset(plot.left - 4, plot.top - 2),
        align: TextAlign.right, maxWidth: 30);
    _label(canvas, '0', Offset(plot.left - 4, plot.bottom - 9),
        align: TextAlign.right, maxWidth: 30);
    _label(canvas, chartValueText(maxX), Offset(plot.right, plot.bottom + 4),
        align: TextAlign.right, maxWidth: 50);
  }

  void _paintPie(Canvas canvas, Size size) {
    final total = data.firstSeriesTotal;
    if (total <= 0) return;

    final radius = math.min(size.width, size.height) / 2 - 10;
    if (radius <= 4) return;
    final centre = Offset(size.width / 2, size.height / 2);
    final box = Rect.fromCircle(center: centre, radius: radius);

    var start = -math.pi / 2;
    for (final (i, point) in data.points.indexed) {
      final value = point.values.isEmpty ? null : point.values.first;
      if (value == null || value <= 0) continue;
      final sweep = (value / total) * 2 * math.pi;
      canvas.drawArc(
        box,
        start,
        sweep,
        true,
        Paint()..color = kChartPalette[i % kChartPalette.length],
      );

      if (config.showValues && sweep > 0.25) {
        final middle = start + sweep / 2;
        final at = centre + Offset(math.cos(middle), math.sin(middle)) * (radius * 0.62);
        final share = (value / total) * 100;
        _label(
          canvas,
          config.showPercentages
              ? '${chartValueText(value)}\n(${share.toStringAsFixed(0)}%)'
              : chartValueText(value),
          Offset(at.dx, at.dy - 5),
          fontSize: 8.5,
          maxWidth: radius,
          maxLines: config.showPercentages ? 2 : 1,
        );
      }
      start += sweep;
    }
  }

  @override
  bool shouldRepaint(_ChartPainter old) =>
      old.data != data || old.config != config || old.isDark != isDark;
}
