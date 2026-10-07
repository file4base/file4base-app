// #37 — a chart on a layout. The Chart Tool was in the toolbar and drew
// nothing; there was no chart object and no setup.
//
// A chart draws the figures #32 works out: the found set grouped by a field,
// with one summary field per series. A scatter chart is the exception — it
// plots two numbers per record, so it reads the records themselves.

/// The kinds of chart that can be drawn.
class ChartType {
  static const String column = 'column';
  static const String bar = 'bar';
  static const String line = 'line';
  static const String area = 'area';
  static const String pie = 'pie';
  static const String scatter = 'scatter';

  static const Map<String, String> labels = {
    column: 'Column',
    bar: 'Bar',
    line: 'Line',
    area: 'Area',
    pie: 'Pie',
    scatter: 'Scatter',
  };

  static const List<String> all = [column, bar, line, area, pie, scatter];

  /// A pie shows one series as shares of a whole; the rest can show several.
  static bool isSingleSeries(String type) => type == pie;

  /// Charts that read grouped figures rather than the records themselves.
  static bool isGrouped(String type) => type != scatter;

  /// Charts whose categories run up the side rather than along the bottom.
  static bool isHorizontal(String type) => type == bar;

  static String label(String type) => labels[type] ?? type;
}

/// One series of a chart: where its numbers come from and how it is drawn.
class ChartSeriesModel {
  /// For a grouped chart, the summary field whose figure each column is. For a
  /// scatter, the number field plotted up the side.
  final String field;

  /// What the legend calls it. Empty falls back to the field name.
  final String label;

  /// `#RRGGBB`, or null to take the next colour of the default palette.
  final String? color;

  const ChartSeriesModel({required this.field, this.label = '', this.color});

  factory ChartSeriesModel.fromJson(Map<String, dynamic> json) => ChartSeriesModel(
        field: json['field'] as String? ?? '',
        label: json['label'] as String? ?? '',
        color: json['color'] as String?,
      );

  Map<String, dynamic> toJson() => {
        'field': field,
        if (label.isNotEmpty) 'label': label,
        if (color != null) 'color': color,
      };

  ChartSeriesModel copyWith({String? field, String? label, String? color}) => ChartSeriesModel(
        field: field ?? this.field,
        label: label ?? this.label,
        color: color ?? this.color,
      );
}

/// A chart's setup: what it draws and how.
class ChartConfigModel {
  /// One of [ChartType].
  final String chartType;

  final String title;

  /// Grouped charts: the field whose values are the categories along the
  /// bottom — the same thing a report breaks on. Scatter: the number field
  /// plotted along the bottom.
  final String? categoryField;

  final List<ChartSeriesModel> series;

  final bool showValues;
  final bool showLegend;

  /// Pie: label each slice with its share as well as its value.
  final bool showPercentages;

  const ChartConfigModel({
    this.chartType = ChartType.column,
    this.title = '',
    this.categoryField,
    this.series = const [],
    this.showValues = true,
    this.showLegend = true,
    this.showPercentages = false,
  });

  /// True when the chart knows what to draw.
  bool get isBound =>
      (categoryField?.isNotEmpty ?? false) && series.any((s) => s.field.isNotEmpty);

  /// The series actually drawn: a pie shows one.
  List<ChartSeriesModel> get drawnSeries {
    final named = series.where((s) => s.field.isNotEmpty).toList();
    if (ChartType.isSingleSeries(chartType) && named.length > 1) return [named.first];
    return named;
  }

  factory ChartConfigModel.fromJson(Map<String, dynamic> json) => ChartConfigModel(
        chartType: json['chart_type'] as String? ?? ChartType.column,
        title: json['title'] as String? ?? '',
        categoryField: json['category_field'] as String?,
        series: [
          for (final s in (json['series'] as List? ?? const []))
            ChartSeriesModel.fromJson(Map<String, dynamic>.from(s as Map)),
        ],
        showValues: json['show_values'] as bool? ?? true,
        showLegend: json['show_legend'] as bool? ?? true,
        showPercentages: json['show_percentages'] as bool? ?? false,
      );

  Map<String, dynamic> toJson() => {
        'chart_type': chartType,
        if (title.isNotEmpty) 'title': title,
        if (categoryField != null) 'category_field': categoryField,
        'series': [for (final s in series) s.toJson()],
        'show_values': showValues,
        'show_legend': showLegend,
        if (showPercentages) 'show_percentages': showPercentages,
      };

  ChartConfigModel copyWith({
    String? chartType,
    String? title,
    String? categoryField,
    bool clearCategoryField = false,
    List<ChartSeriesModel>? series,
    bool? showValues,
    bool? showLegend,
    bool? showPercentages,
  }) =>
      ChartConfigModel(
        chartType: chartType ?? this.chartType,
        title: title ?? this.title,
        categoryField: clearCategoryField ? null : (categoryField ?? this.categoryField),
        series: series ?? this.series,
        showValues: showValues ?? this.showValues,
        showLegend: showLegend ?? this.showLegend,
        showPercentages: showPercentages ?? this.showPercentages,
      );
}

/// One point of a chart: a category and a number per series.
class ChartPoint {
  final String category;
  final List<double?> values;

  const ChartPoint({required this.category, required this.values});
}

/// The numbers a chart draws, worked out from the figures or the records.
class ChartData {
  final List<ChartPoint> points;
  final List<String> seriesLabels;

  const ChartData({this.points = const [], this.seriesLabels = const []});

  bool get isEmpty => points.isEmpty || seriesLabels.isEmpty;

  /// The largest value drawn, which sets the top of the axis. Zero when
  /// everything is empty or negative.
  double get maxValue {
    var max = 0.0;
    for (final point in points) {
      for (final value in point.values) {
        if (value != null && value > max) max = value;
      }
    }
    return max;
  }

  /// The smallest value drawn, so an axis that must show negatives does.
  double get minValue {
    var min = 0.0;
    for (final point in points) {
      for (final value in point.values) {
        if (value != null && value < min) min = value;
      }
    }
    return min;
  }

  /// The total of the first series, which is what a pie divides up.
  double get firstSeriesTotal {
    var total = 0.0;
    for (final point in points) {
      final value = point.values.isEmpty ? null : point.values.first;
      if (value != null && value > 0) total += value;
    }
    return total;
  }
}
