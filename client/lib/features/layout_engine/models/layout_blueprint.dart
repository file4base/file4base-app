// #33 — the layout assistant. A new layout is one of five kinds, each laid out
// differently from the same ingredients: the fields chosen, in the order they
// were chosen, dressed in a theme.
//
// These are pure functions over the chosen settings, so what the assistant
// produces can be read and tested without opening a dialog.

import 'dart:math' as math;

import 'layout_definition.dart';

/// The kinds of layout the assistant can make.
enum LayoutKind {
  /// One record at a time, fields stacked with their labels beside them.
  form,

  /// One row per record, with column headings in the header.
  list,

  /// A list that groups: a sub-summary per group and a grand total (#32).
  report,

  /// One label, repeated across and down a sheet of label stock.
  labels,

  /// The three standard bands and nothing in them.
  blank,
}

extension LayoutKindInfo on LayoutKind {
  String get label => switch (this) {
        LayoutKind.form => 'Form',
        LayoutKind.list => 'List',
        LayoutKind.report => 'Report with grouped data',
        LayoutKind.labels => 'Labels',
        LayoutKind.blank => 'Blank layout',
      };

  String get description => switch (this) {
        LayoutKind.form => 'One record at a time, each field labelled. Good for entering data.',
        LayoutKind.list => 'One row per record, with column headings. Good for scanning a found set.',
        LayoutKind.report =>
          'A list that breaks into groups, with a subtotal under each and a grand total at the end.',
        LayoutKind.labels => 'One label, repeated across and down a sheet. Printed, not browsed.',
        LayoutKind.blank => 'The header, body and footer, empty. You place everything yourself.',
      };

  /// Whether this kind shows one record or many.
  String get defaultView => switch (this) {
        LayoutKind.form => 'form',
        LayoutKind.blank => 'form',
        _ => 'list',
      };

  bool get needsFields => this != LayoutKind.blank;
  bool get needsBreakField => this == LayoutKind.report;
  bool get needsLabelStock => this == LayoutKind.labels;
}

/// How a generated layout looks: the colours and type the assistant dresses it
/// in (#33).
///
/// These are **layout** themes — they style the layout being made. They are
/// not the application themes in Manage Themes, which colour the window
/// File4Base itself is drawn in.
class LayoutTheme {
  final String id;
  final String name;
  final String description;

  /// Header band fill, and the colour of the text on it.
  final String headerFill;
  final String headerText;

  /// Title and column heading sizes.
  final double titleSize;
  final double headingSize;
  final double bodySize;

  /// Field boxes: their border, and whether they are drawn with one at all.
  final String? fieldBorder;
  final double fieldBorderWidth;
  final double cornerRadius;

  /// Fill of a summary band, so a subtotal stands out from the rows.
  final String summaryFill;

  const LayoutTheme({
    required this.id,
    required this.name,
    required this.description,
    required this.headerFill,
    required this.headerText,
    this.titleSize = 18,
    this.headingSize = 11,
    this.bodySize = 12,
    this.fieldBorder,
    this.fieldBorderWidth = 1,
    this.cornerRadius = 4,
    this.summaryFill = '#F2F5F9',
  });

  static const enlightened = LayoutTheme(
    id: 'Enlightened',
    name: 'Enlightened',
    description: 'Blue headings on white, boxed fields. The File4Base default.',
    headerFill: '#FFFFFF',
    headerText: '#1E88E5',
    fieldBorder: '#C9CFD8',
  );

  static const coolGrey = LayoutTheme(
    id: 'Cool Grey',
    name: 'Cool Grey',
    description: 'A grey header band with white text, and quiet field borders.',
    headerFill: '#4A5568',
    headerText: '#FFFFFF',
    fieldBorder: '#D7DCE3',
    summaryFill: '#EDF1F6',
  );

  static const classic = LayoutTheme(
    id: 'Classic',
    name: 'Classic',
    description: 'Serif-weight headings, heavier boxes. Looks like a printed form.',
    headerFill: '#FFFFFF',
    headerText: '#1A1A1A',
    titleSize: 20,
    headingSize: 12,
    fieldBorder: '#4A4A4A',
    fieldBorderWidth: 1.2,
    cornerRadius: 0,
    summaryFill: '#EFEFEF',
  );

  static const minimal = LayoutTheme(
    id: 'Minimal',
    name: 'Minimal',
    description: 'No field boxes at all. Best for labels and printed reports.',
    headerFill: '#FFFFFF',
    headerText: '#333333',
    titleSize: 16,
    fieldBorder: null,
    fieldBorderWidth: 0,
    cornerRadius: 0,
    summaryFill: '#F5F5F5',
  );

  static const all = [enlightened, coolGrey, classic, minimal];

  static LayoutTheme byId(String? id) =>
      all.where((t) => t.id == id).firstOrNull ?? enlightened;
}

/// A sheet of labels: how big one label is and how they sit on the page.
///
/// Sizes are in millimetres, as the stock is sold.
class LabelStock {
  final String id;
  final String name;

  final double pageWidthMm;
  final double pageHeightMm;
  final double labelWidthMm;
  final double labelHeightMm;
  final int across;
  final int down;
  final double marginLeftMm;
  final double marginTopMm;
  final double gutterXMm;
  final double gutterYMm;

  const LabelStock({
    required this.id,
    required this.name,
    required this.pageWidthMm,
    required this.pageHeightMm,
    required this.labelWidthMm,
    required this.labelHeightMm,
    required this.across,
    required this.down,
    this.marginLeftMm = 0,
    this.marginTopMm = 0,
    this.gutterXMm = 0,
    this.gutterYMm = 0,
  });

  static const double mmToPt = 72.0 / 25.4;

  double get labelWidthPt => labelWidthMm * mmToPt;
  double get labelHeightPt => labelHeightMm * mmToPt;
  double get gutterXPt => gutterXMm * mmToPt;
  double get gutterYPt => gutterYMm * mmToPt;
  double get marginLeftPt => marginLeftMm * mmToPt;
  double get marginTopPt => marginTopMm * mmToPt;

  int get perSheet => across * down;

  /// How the size reads in the picker.
  String get sizeLabel =>
      '${labelWidthMm.toStringAsFixed(0)} × ${labelHeightMm.toStringAsFixed(0)} mm · '
      '$across across, $down down';

  static const averyL7160 = LabelStock(
    id: 'avery_l7160',
    name: 'Avery L7160 (A4)',
    pageWidthMm: 210, pageHeightMm: 297,
    labelWidthMm: 63.5, labelHeightMm: 38.1,
    across: 3, down: 7,
    marginLeftMm: 7.2, marginTopMm: 15.1, gutterXMm: 2.5, gutterYMm: 0,
  );

  static const averyL7163 = LabelStock(
    id: 'avery_l7163',
    name: 'Avery L7163 (A4)',
    pageWidthMm: 210, pageHeightMm: 297,
    labelWidthMm: 99.1, labelHeightMm: 38.1,
    across: 2, down: 7,
    marginLeftMm: 5.0, marginTopMm: 15.1, gutterXMm: 2.5, gutterYMm: 0,
  );

  static const avery5160 = LabelStock(
    id: 'avery_5160',
    name: 'Avery 5160 (US Letter)',
    pageWidthMm: 215.9, pageHeightMm: 279.4,
    labelWidthMm: 66.7, labelHeightMm: 25.4,
    across: 3, down: 10,
    marginLeftMm: 4.8, marginTopMm: 12.7, gutterXMm: 3.2, gutterYMm: 0,
  );

  static const avery5163 = LabelStock(
    id: 'avery_5163',
    name: 'Avery 5163 (US Letter)',
    pageWidthMm: 215.9, pageHeightMm: 279.4,
    labelWidthMm: 101.6, labelHeightMm: 50.8,
    across: 2, down: 5,
    marginLeftMm: 4.2, marginTopMm: 12.7, gutterXMm: 4.8, gutterYMm: 0,
  );

  static const all = [averyL7160, averyL7163, avery5160, avery5163];

  static LabelStock byId(String? id) =>
      all.where((s) => s.id == id).firstOrNull ?? averyL7160;

  /// A stock of a size the user typed in, laid out to fill an A4 sheet.
  factory LabelStock.custom({required double widthMm, required double heightMm}) {
    const pageWidth = 210.0, pageHeight = 297.0;
    const margin = 8.0;
    final across = math.max(1, ((pageWidth - margin * 2) / widthMm).floor());
    final down = math.max(1, ((pageHeight - margin * 2) / heightMm).floor());
    return LabelStock(
      id: 'custom',
      name: 'Custom size',
      pageWidthMm: pageWidth, pageHeightMm: pageHeight,
      labelWidthMm: widthMm, labelHeightMm: heightMm,
      across: across, down: down,
      marginLeftMm: margin, marginTopMm: margin,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'page_width_mm': pageWidthMm,
        'page_height_mm': pageHeightMm,
        'label_width_mm': labelWidthMm,
        'label_height_mm': labelHeightMm,
        'across': across,
        'down': down,
        'margin_left_mm': marginLeftMm,
        'margin_top_mm': marginTopMm,
        'gutter_x_mm': gutterXMm,
        'gutter_y_mm': gutterYMm,
      };

  static LabelStock? fromJson(Map<String, dynamic>? json) {
    if (json == null) return null;
    double num_(String key, [double fallback = 0]) =>
        (json[key] as num?)?.toDouble() ?? fallback;
    return LabelStock(
      id: json['id'] as String? ?? 'custom',
      name: json['name'] as String? ?? 'Custom size',
      pageWidthMm: num_('page_width_mm', 210),
      pageHeightMm: num_('page_height_mm', 297),
      labelWidthMm: num_('label_width_mm', 63.5),
      labelHeightMm: num_('label_height_mm', 38.1),
      across: (json['across'] as num?)?.toInt() ?? 3,
      down: (json['down'] as num?)?.toInt() ?? 7,
      marginLeftMm: num_('margin_left_mm'),
      marginTopMm: num_('margin_top_mm'),
      gutterXMm: num_('gutter_x_mm'),
      gutterYMm: num_('gutter_y_mm'),
    );
  }
}

/// One field the assistant was told to put on the layout.
typedef BlueprintField = ({String name, String label, bool isSummary});

/// Everything the assistant was asked for.
class LayoutBlueprint {
  final String name;
  final LayoutKind kind;
  final String tableOccurrence;

  /// The fields to show, in the order they are to appear.
  final List<BlueprintField> fields;

  final LayoutTheme theme;

  /// Report: the field whose change starts a new group.
  final String? breakField;

  /// Report: the summary fields to subtotal with.
  final List<BlueprintField> summaryFields;

  /// Labels: the stock the sheet is cut from.
  final LabelStock? stock;

  const LayoutBlueprint({
    required this.name,
    required this.kind,
    required this.tableOccurrence,
    this.fields = const [],
    this.theme = LayoutTheme.enlightened,
    this.breakField,
    this.summaryFields = const [],
    this.stock,
  });

  LayoutDefinitionModel build() => switch (kind) {
        LayoutKind.form => _buildForm(),
        LayoutKind.list => _buildList(),
        LayoutKind.report => _buildReport(),
        LayoutKind.labels => _buildLabels(),
        LayoutKind.blank => _buildBlank(),
      };

  LayoutDefinitionModel _definition({
    required List<LayoutPartModel> parts,
    required List<LayoutObjectModel> objects,
    required double width,
  }) {
    return LayoutDefinitionModel(
      id: 'layout_${DateTime.now().millisecondsSinceEpoch}',
      name: name,
      tableOccurrence: tableOccurrence,
      width: width,
      theme: theme.id,
      defaultView: kind.defaultView,
      parts: parts,
      objects: objects,
      labelStock: kind == LayoutKind.labels ? stock?.toJson() : null,
    );
  }

  LayoutObjectStyle get _headingStyle => LayoutObjectStyle(
        fontSize: theme.headingSize,
        fontWeight: 'bold',
        textColor: theme.headerText,
      );

  LayoutObjectStyle _fieldStyle({String align = 'left', bool bold = false}) => LayoutObjectStyle(
        fontSize: theme.bodySize,
        fontWeight: bold ? 'bold' : 'normal',
        borderColor: theme.fieldBorder,
        borderWidth: theme.fieldBorderWidth,
        cornerRadius: theme.cornerRadius,
        textAlign: align,
      );

  LayoutObjectModel _title(double y, double width) => LayoutObjectModel(
        id: 'title',
        type: 'label',
        x: 24,
        y: y,
        width: width - 48,
        height: 28,
        text: name,
        style: LayoutObjectStyle(
          fontSize: theme.titleSize,
          fontWeight: 'bold',
          textColor: theme.headerText,
        ),
      );

  // ─── Form ────────────────────────────────────────────────────────────────

  LayoutDefinitionModel _buildForm() {
    const width = 900.0;
    const headerHeight = 60.0;
    const footerHeight = 40.0;
    const firstRowY = 20.0;
    const rowHeight = 48.0;

    final rows = fields.where((f) => !f.isSummary).toList();
    final bodyHeight = math.max(120.0, firstRowY * 2 + rows.length * rowHeight);

    final objects = <LayoutObjectModel>[_title(16, width)];
    var y = headerHeight + firstRowY;
    for (final field in rows) {
      objects.add(LayoutObjectModel(
        id: 'lbl_${field.name}',
        type: 'label',
        x: 40,
        y: y + 6,
        width: 140,
        height: 24,
        text: field.label,
        style: LayoutObjectStyle(
          fontSize: theme.headingSize + 2,
          fontWeight: 'bold',
          textAlign: 'right',
        ),
      ));
      objects.add(LayoutObjectModel(
        id: 'fld_${field.name}',
        type: 'field',
        x: 190,
        y: y,
        width: 280,
        height: 36,
        fieldBinding: FieldBindingModel(fieldName: field.name),
        style: _fieldStyle(),
      ));
      y += rowHeight;
    }

    return _definition(
      width: width,
      parts: [
        const LayoutPartModel(id: 'header_part', type: LayoutPartType.header, height: headerHeight),
        LayoutPartModel(id: 'body_part', type: LayoutPartType.body, height: bodyHeight),
        const LayoutPartModel(id: 'footer_part', type: LayoutPartType.footer, height: footerHeight),
      ],
      objects: objects,
    );
  }

  // ─── Columns, shared by list and report ──────────────────────────────────

  /// Column positions across the width, wide enough to read.
  List<({BlueprintField field, double x, double width})> _columns(double width) {
    final shown = fields.where((f) => !f.isSummary).toList();
    if (shown.isEmpty) return const [];
    const left = 24.0;
    final usable = width - left * 2;
    final columnWidth = math.max(70.0, usable / shown.length);
    return [
      for (final (i, field) in shown.indexed)
        (field: field, x: left + i * columnWidth, width: columnWidth - 10),
    ];
  }

  /// The width a list needs to show its columns without squeezing them.
  double get _listWidth {
    final count = fields.where((f) => !f.isSummary).length;
    return math.min(1400.0, math.max(700.0, 48.0 + count * 150.0));
  }

  List<LayoutObjectModel> _columnHeadings(double width, double y) => [
        for (final column in _columns(width))
          LayoutObjectModel(
            id: 'head_${column.field.name}',
            type: 'label',
            x: column.x,
            y: y,
            width: column.width,
            height: 20,
            text: column.field.label,
            style: _headingStyle,
          ),
      ];

  List<LayoutObjectModel> _rowFields(double width, double y) => [
        for (final column in _columns(width))
          LayoutObjectModel(
            id: 'row_${column.field.name}',
            type: 'field',
            x: column.x,
            y: y,
            width: column.width,
            height: 22,
            fieldBinding: FieldBindingModel(fieldName: column.field.name),
            style: _fieldStyle(),
          ),
      ];

  // ─── List ────────────────────────────────────────────────────────────────

  LayoutDefinitionModel _buildList() {
    final width = _listWidth;
    const headerHeight = 76.0;
    const bodyHeight = 30.0;
    const footerHeight = 30.0;

    return _definition(
      width: width,
      parts: const [
        LayoutPartModel(id: 'header_part', type: LayoutPartType.header, height: headerHeight),
        LayoutPartModel(id: 'body_part', type: LayoutPartType.body, height: bodyHeight),
        LayoutPartModel(id: 'footer_part', type: LayoutPartType.footer, height: footerHeight),
      ],
      objects: [
        _title(14, width),
        ..._columnHeadings(width, headerHeight - 24),
        ..._rowFields(width, headerHeight + 4),
        LayoutObjectModel(
          id: 'footer_count',
          type: 'label',
          x: 24,
          y: headerHeight + bodyHeight + 8,
          width: 300,
          height: 18,
          text: '{{CurrentDate}}',
          style: LayoutObjectStyle(fontSize: theme.headingSize - 1),
        ),
      ],
    );
  }

  // ─── Report ──────────────────────────────────────────────────────────────

  LayoutDefinitionModel _buildReport() {
    final width = _listWidth;
    const headerHeight = 76.0;
    const subHeight = 34.0;
    const bodyHeight = 26.0;
    const grandHeight = 40.0;
    const footerHeight = 30.0;

    const subTop = headerHeight;
    const bodyTop = subTop + subHeight;
    const grandTop = bodyTop + bodyHeight;
    const footerTop = grandTop + grandHeight;

    final columns = _columns(width);
    // The subtotals sit under the last columns, where the figures they total
    // are, so they line up with the rows above them.
    final summarySlots = columns.length <= summaryFields.length
        ? columns
        : columns.sublist(columns.length - summaryFields.length);

    final objects = <LayoutObjectModel>[
      _title(14, width),
      ..._columnHeadings(width, headerHeight - 24),

      // Sub-summary: the group, how it is named and what it comes to.
      if (breakField != null)
        LayoutObjectModel(
          id: 'group_value',
          type: 'field',
          x: 24,
          y: subTop + 7,
          width: 260,
          height: 20,
          fieldBinding: FieldBindingModel(fieldName: breakField!),
          style: LayoutObjectStyle(fontSize: theme.bodySize + 1, fontWeight: 'bold'),
        ),
      for (final (i, summary) in summaryFields.indexed)
        if (i < summarySlots.length)
          LayoutObjectModel(
            id: 'group_${summary.name}',
            type: 'field',
            x: summarySlots[i].x,
            y: subTop + 7,
            width: summarySlots[i].width,
            height: 20,
            fieldBinding: FieldBindingModel(fieldName: summary.name),
            style: LayoutObjectStyle(
                fontSize: theme.bodySize + 1, fontWeight: 'bold', textAlign: 'right'),
          ),

      ..._rowFields(width, bodyTop + 2),

      LayoutObjectModel(
        id: 'grand_label',
        type: 'label',
        x: 24,
        y: grandTop + 10,
        width: 300,
        height: 20,
        text: 'Total, all records',
        style: LayoutObjectStyle(fontSize: theme.bodySize + 1, fontWeight: 'bold'),
      ),
      for (final (i, summary) in summaryFields.indexed)
        if (i < summarySlots.length)
          LayoutObjectModel(
            id: 'grand_${summary.name}',
            type: 'field',
            x: summarySlots[i].x,
            y: grandTop + 10,
            width: summarySlots[i].width,
            height: 20,
            fieldBinding: FieldBindingModel(fieldName: summary.name),
            style: LayoutObjectStyle(
                fontSize: theme.bodySize + 2, fontWeight: 'bold', textAlign: 'right'),
          ),

      LayoutObjectModel(
        id: 'footer_date',
        type: 'label',
        x: 24,
        y: footerTop + 6,
        width: 300,
        height: 18,
        text: '{{CurrentDate}}',
        style: LayoutObjectStyle(fontSize: theme.headingSize - 1),
      ),
    ];

    return _definition(
      width: width,
      parts: [
        const LayoutPartModel(id: 'header_part', type: LayoutPartType.header, height: headerHeight),
        LayoutPartModel(
          id: 'sub_part',
          type: LayoutPartType.subSummary,
          height: subHeight,
          breakField: breakField,
        ),
        const LayoutPartModel(id: 'body_part', type: LayoutPartType.body, height: bodyHeight),
        const LayoutPartModel(
            id: 'grand_part', type: LayoutPartType.trailingGrandSummary, height: grandHeight),
        const LayoutPartModel(id: 'footer_part', type: LayoutPartType.footer, height: footerHeight),
      ],
      objects: objects,
    );
  }

  // ─── Labels ──────────────────────────────────────────────────────────────

  /// One label: the body is the label, and the fields are a block of merge
  /// text so an empty line collapses instead of printing blank (#33).
  LayoutDefinitionModel _buildLabels() {
    final sheet = stock ?? LabelStock.averyL7160;
    final width = sheet.labelWidthPt;
    final height = sheet.labelHeightPt;

    final shown = fields.where((f) => !f.isSummary).toList();
    final text = shown.map((f) => '{{${f.name}}}').join('\n');

    return _definition(
      width: width,
      parts: [
        // A label has no header or footer: the sheet is nothing but labels.
        LayoutPartModel(id: 'body_part', type: LayoutPartType.body, height: height),
      ],
      objects: [
        LayoutObjectModel(
          id: 'label_text',
          type: 'label',
          x: 8,
          y: 6,
          width: width - 16,
          height: height - 12,
          text: text,
          style: LayoutObjectStyle(fontSize: theme.bodySize, textAlign: 'left'),
        ),
      ],
    );
  }

  // ─── Blank ───────────────────────────────────────────────────────────────

  LayoutDefinitionModel _buildBlank() {
    return _definition(
      width: 900,
      parts: const [
        LayoutPartModel(id: 'header_part', type: LayoutPartType.header, height: 60),
        LayoutPartModel(id: 'body_part', type: LayoutPartType.body, height: 400),
        LayoutPartModel(id: 'footer_part', type: LayoutPartType.footer, height: 40),
      ],
      objects: const [],
    );
  }
}
