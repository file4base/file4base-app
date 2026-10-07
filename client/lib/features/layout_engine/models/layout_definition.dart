import 'dart:math' as math;

import 'chart_definition.dart';
/// The kinds of band a layout is divided into (#32).
///
/// A report is these parts in order: the header, a leading grand summary, then
/// for each group a leading sub-summary, its body rows and a trailing
/// sub-summary, then a trailing grand summary and the footer.
///
/// Whether a sub-summary is leading or trailing is decided by **where it sits**
/// — above the body or below it — as it is in FileMaker, rather than by two
/// separate part types.
class LayoutPartType {
  static const String header = 'header';
  static const String leadingGrandSummary = 'leading_grand_summary';
  static const String subSummary = 'subsummary';
  static const String body = 'body';
  static const String trailingGrandSummary = 'trailing_grand_summary';
  static const String footer = 'footer';

  /// What each part is called in the designer's gutter and menus.
  static const Map<String, String> labels = {
    header: 'Header',
    leadingGrandSummary: 'Leading Grand Summary',
    subSummary: 'Sub-summary',
    body: 'Body',
    trailingGrandSummary: 'Trailing Grand Summary',
    footer: 'Footer',
  };

  /// The parts a layout has exactly one of, in the order they must appear.
  static const List<String> fixedOrder = [
    header,
    leadingGrandSummary,
    subSummary,
    body,
    subSummary,
    trailingGrandSummary,
    footer,
  ];

  static bool isGrandSummary(String type) =>
      type == leadingGrandSummary || type == trailingGrandSummary;

  /// A part whose content is a summary rather than a record.
  static bool isSummaryPart(String type) => type == subSummary || isGrandSummary(type);

  static String label(String type) => labels[type] ?? type;
}

class LayoutPartModel {
  final String id;

  /// One of [LayoutPartType]. Older layouts may also hold `top_navigation` or
  /// `title_header`, which render as a header.
  final String type;

  final double height;

  /// For a sub-summary, the field whose change starts a new group. A
  /// sub-summary without one summarizes nothing and says so.
  final String? breakField;

  const LayoutPartModel({
    required this.id,
    required this.type,
    required this.height,
    this.breakField,
  });

  factory LayoutPartModel.fromJson(Map<String, dynamic> json) {
    return LayoutPartModel(
      id: json['id'] as String? ?? 'part_${DateTime.now().millisecondsSinceEpoch}',
      type: json['type'] as String? ?? 'body',
      height: (json['height'] as num?)?.toDouble() ?? 120.0,
      breakField: json['break_field'] as String?,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'type': type,
        'height': height,
        if (breakField != null) 'break_field': breakField,
      };

  LayoutPartModel copyWith({
    String? id,
    String? type,
    double? height,
    String? breakField,
    bool clearBreakField = false,
  }) {
    return LayoutPartModel(
      id: id ?? this.id,
      type: type ?? this.type,
      height: height ?? this.height,
      breakField: clearBreakField ? null : (breakField ?? this.breakField),
    );
  }

  bool get isSubSummary => type == LayoutPartType.subSummary;
  bool get isGrandSummary => LayoutPartType.isGrandSummary(type);
  bool get isSummaryPart => LayoutPartType.isSummaryPart(type);
  bool get isBody => type == LayoutPartType.body;
}

/// How a field is presented on a layout. Everything but [editBox] is filled
/// from a value list (#31).
class FieldControlStyle {
  static const String editBox = 'edit_box';
  static const String dropDownList = 'drop_down_list';
  static const String popUpMenu = 'pop_up_menu';
  static const String checkboxSet = 'checkbox_set';
  static const String radioButtonSet = 'radio_button_set';

  /// The styles offered in the inspector, with the label each one shows.
  static const Map<String, String> labels = {
    editBox: 'Edit box',
    dropDownList: 'Drop-down list',
    popUpMenu: 'Pop-up menu',
    checkboxSet: 'Checkbox set',
    radioButtonSet: 'Radio button set',
  };

  /// True when the style needs a value list to show anything.
  static bool needsValueList(String style) => style != editBox;

  /// A style that lets the record hold more than one of the values.
  static bool isMultiValue(String style) => style == checkboxSet;
}

class FieldBindingModel {
  /// The table occurrence the field is read from. Null, or the layout's own
  /// occurrence, means the record in hand; any other occurrence makes this a
  /// **related field**, read through [relationshipId] (#36).
  final String? tableOccurrence;

  /// The relationship followed to reach [tableOccurrence]. A related field
  /// needs one: the same two tables can be joined more than once, so the
  /// occurrence alone does not say which join to follow.
  final String? relationshipId;

  final String fieldName;

  /// One of [FieldControlStyle].
  final String controlStyle;

  /// The value list that fills the control, when the style needs one.
  final String? valueListId;

  final bool allowBrowseEntry;
  final bool allowFindEntry;

  const FieldBindingModel({
    this.tableOccurrence,
    this.relationshipId,
    required this.fieldName,
    this.controlStyle = FieldControlStyle.editBox,
    this.valueListId,
    this.allowBrowseEntry = true,
    this.allowFindEntry = true,
  });

  /// True when the field is read through a relationship rather than from the
  /// record in hand.
  bool get isRelated => relationshipId != null && relationshipId!.isNotEmpty;

  /// How the field is written on a layout: `Companies::company_address` for a
  /// related field, the bare field name otherwise.
  String get qualifiedName =>
      isRelated && (tableOccurrence?.isNotEmpty ?? false) ? '$tableOccurrence::$fieldName' : fieldName;

  /// The style actually used: a control that needs a value list but has none
  /// falls back to an edit box, so deleting a list cannot break a layout.
  String effectiveControlStyle({required bool hasValueList}) {
    if (FieldControlStyle.needsValueList(controlStyle) && !hasValueList) {
      return FieldControlStyle.editBox;
    }
    return controlStyle;
  }

  FieldBindingModel copyWith({
    String? tableOccurrence,
    String? relationshipId,
    bool clearRelationship = false,
    String? fieldName,
    String? controlStyle,
    String? valueListId,
    bool clearValueList = false,
    bool? allowBrowseEntry,
    bool? allowFindEntry,
  }) =>
      FieldBindingModel(
        tableOccurrence: clearRelationship ? null : (tableOccurrence ?? this.tableOccurrence),
        relationshipId: clearRelationship ? null : (relationshipId ?? this.relationshipId),
        fieldName: fieldName ?? this.fieldName,
        controlStyle: controlStyle ?? this.controlStyle,
        valueListId: clearValueList ? null : (valueListId ?? this.valueListId),
        allowBrowseEntry: allowBrowseEntry ?? this.allowBrowseEntry,
        allowFindEntry: allowFindEntry ?? this.allowFindEntry,
      );

  factory FieldBindingModel.fromJson(Map<String, dynamic> json) {
    return FieldBindingModel(
      tableOccurrence: json['table_occurrence'] as String?,
      relationshipId: json['relationship_id'] as String?,
      fieldName: json['field_name'] as String? ?? '',
      controlStyle: json['control_style'] as String? ?? FieldControlStyle.editBox,
      valueListId: json['value_list_id'] as String?,
      allowBrowseEntry: json['allow_browse_entry'] as bool? ?? true,
      allowFindEntry: json['allow_find_entry'] as bool? ?? true,
    );
  }

  Map<String, dynamic> toJson() => {
        if (tableOccurrence != null) 'table_occurrence': tableOccurrence,
        if (relationshipId != null) 'relationship_id': relationshipId,
        'field_name': fieldName,
        'control_style': controlStyle,
        if (valueListId != null) 'value_list_id': valueListId,
        'allow_browse_entry': allowBrowseEntry,
        'allow_find_entry': allowFindEntry,
      };
}

class LayoutObjectStyle {
  final String? fillColor;
  final String? borderColor;
  final double borderWidth;
  final double cornerRadius;
  final double fontSize;
  final String? fontWeight;
  final String? textColor;
  final String textAlign;

  const LayoutObjectStyle({
    this.fillColor,
    this.borderColor,
    this.borderWidth = 1.0,
    this.cornerRadius = 4.0,
    this.fontSize = 14.0,
    this.fontWeight = 'normal',
    this.textColor,
    this.textAlign = 'left',
  });

  factory LayoutObjectStyle.fromJson(Map<String, dynamic> json) {
    return LayoutObjectStyle(
      fillColor: json['fill_color'] as String?,
      borderColor: json['border_color'] as String?,
      borderWidth: (json['border_width'] as num?)?.toDouble() ?? 1.0,
      cornerRadius: (json['corner_radius'] as num?)?.toDouble() ?? 4.0,
      fontSize: (json['font_size'] as num?)?.toDouble() ?? 14.0,
      fontWeight: json['font_weight'] as String? ?? 'normal',
      textColor: json['text_color'] as String?,
      textAlign: json['text_align'] as String? ?? 'left',
    );
  }

  Map<String, dynamic> toJson() => {
        if (fillColor != null) 'fill_color': fillColor,
        if (borderColor != null) 'border_color': borderColor,
        'border_width': borderWidth,
        'corner_radius': cornerRadius,
        'font_size': fontSize,
        'font_weight': fontWeight,
        if (textColor != null) 'text_color': textColor,
        'text_align': textAlign,
      };

  /// Copies the style. The `clear*` flags reset a nullable color to "none",
  /// which a plain null argument cannot express.
  LayoutObjectStyle copyWith({
    String? fillColor,
    String? borderColor,
    double? borderWidth,
    double? cornerRadius,
    double? fontSize,
    String? fontWeight,
    String? textColor,
    String? textAlign,
    bool clearFillColor = false,
    bool clearBorderColor = false,
    bool clearTextColor = false,
  }) {
    return LayoutObjectStyle(
      fillColor: clearFillColor ? null : (fillColor ?? this.fillColor),
      borderColor: clearBorderColor ? null : (borderColor ?? this.borderColor),
      borderWidth: borderWidth ?? this.borderWidth,
      cornerRadius: cornerRadius ?? this.cornerRadius,
      fontSize: fontSize ?? this.fontSize,
      fontWeight: fontWeight ?? this.fontWeight,
      textColor: clearTextColor ? null : (textColor ?? this.textColor),
      textAlign: textAlign ?? this.textAlign,
    );
  }
}

/// Action run when a button is clicked in Browse mode.
///
/// `type` is `single_step` (one script step, `stepType` + `params`, using the
/// same step types and parameters as the Script Workspace) or `perform_script`
/// (a stored script referenced by `scriptId` / `scriptName`, with an optional
/// `parameter`).
class ButtonActionModel {
  final String type;
  final String? stepType;
  final Map<String, dynamic> params;
  final String? scriptId;
  final String? scriptName;
  final String? parameter;

  const ButtonActionModel({
    required this.type,
    this.stepType,
    this.params = const {},
    this.scriptId,
    this.scriptName,
    this.parameter,
  });

  const ButtonActionModel.singleStep(String step, {Map<String, dynamic> params = const {}})
      : this(type: 'single_step', stepType: step, params: params);

  const ButtonActionModel.performScript({required String id, required String name, String? parameter})
      : this(type: 'perform_script', scriptId: id, scriptName: name, parameter: parameter);

  bool get isPerformScript => type == 'perform_script';

  factory ButtonActionModel.fromJson(Map<String, dynamic> json) {
    return ButtonActionModel(
      type: json['type'] as String? ?? 'single_step',
      stepType: json['step_type'] as String?,
      params: json['params'] is Map ? Map<String, dynamic>.from(json['params'] as Map) : const {},
      scriptId: json['script_id'] as String?,
      scriptName: json['script_name'] as String?,
      parameter: json['parameter'] as String?,
    );
  }

  Map<String, dynamic> toJson() => {
        'type': type,
        if (stepType != null) 'step_type': stepType,
        if (params.isNotEmpty) 'params': params,
        if (scriptId != null) 'script_id': scriptId,
        if (scriptName != null) 'script_name': scriptName,
        if (parameter != null && parameter!.isNotEmpty) 'parameter': parameter,
      };
}

/// Picture, PDF, audio/video or file embedded in a layout object.
///
/// The content is stored inline as base64 (`data`) so the layout stays
/// self-contained, or referenced by `url`. `fit` applies to pictures:
/// `contain`, `cover` or `fill`.
class LayoutMediaModel {
  final String kind; // 'image', 'pdf', 'video', 'audio', 'file'
  final String name;
  final String? mimeType;
  final String? data;
  final String? url;
  final String fit;

  const LayoutMediaModel({
    required this.kind,
    required this.name,
    this.mimeType,
    this.data,
    this.url,
    this.fit = 'contain',
  });

  bool get isImage => kind == 'image';

  factory LayoutMediaModel.fromJson(Map<String, dynamic> json) {
    return LayoutMediaModel(
      kind: json['kind'] as String? ?? 'file',
      name: json['name'] as String? ?? '',
      mimeType: json['mime_type'] as String?,
      data: json['data'] as String?,
      url: json['url'] as String?,
      fit: json['fit'] as String? ?? 'contain',
    );
  }

  Map<String, dynamic> toJson() => {
        'kind': kind,
        'name': name,
        if (mimeType != null) 'mime_type': mimeType,
        if (data != null) 'data': data,
        if (url != null) 'url': url,
        'fit': fit,
      };

  LayoutMediaModel copyWith({String? fit}) => LayoutMediaModel(
        kind: kind,
        name: name,
        mimeType: mimeType,
        data: data,
        url: url,
        fit: fit ?? this.fit,
      );
}

/// A portal: the region of a layout that repeats, once per related record
/// (#36).
///
/// The fields a row shows are the layout objects drawn **inside** the portal,
/// as they are in FileMaker: the portal's first row is the band they are
/// placed in, and each row redraws them against its own record.
class PortalConfigModel {
  /// The relationship followed to reach the records the rows show.
  final String? relationshipId;

  /// The occurrence whose records the rows show. It is what tells the two ends
  /// apart when a relationship joins a table to itself.
  final String? occurrence;

  /// First related record shown, counting from 1, so a portal can start past
  /// the first row.
  final int initialRow;

  /// How many rows the portal draws. The rest are reached by scrolling.
  final int rowCount;

  /// Height of one row. Null means the portal's own height divided by
  /// [rowCount], which is what dragging the portal taller does.
  final double? rowHeight;

  final bool showScrollBar;

  /// Offer an empty row at the end that creates a related record. It only does
  /// anything when the relationship allows creation; the server refuses
  /// otherwise.
  final bool allowCreation;

  /// Offer a button on each row that deletes its related record.
  final bool allowDeletion;

  /// Order of the rows, in the form the data API takes (`last_name`,
  /// `-fee_paid`). Empty means the relationship's own "Sort related records".
  final String sort;

  const PortalConfigModel({
    this.relationshipId,
    this.occurrence,
    this.initialRow = 1,
    this.rowCount = 5,
    this.rowHeight,
    this.showScrollBar = true,
    this.allowCreation = false,
    this.allowDeletion = false,
    this.sort = '',
  });

  /// True when the portal knows which records to show.
  bool get isBound => relationshipId != null && relationshipId!.isNotEmpty;

  /// The sort order to ask the server for, split into the fields it takes.
  List<String> get sortFields =>
      sort.split(',').map((f) => f.trim()).where((f) => f.isNotEmpty).toList();

  /// Height of one row inside a portal [portalHeight] tall.
  double effectiveRowHeight(double portalHeight) {
    if (rowHeight != null && rowHeight! > 0) return rowHeight!;
    final rows = rowCount > 0 ? rowCount : 1;
    return math.max(24.0, portalHeight / rows);
  }

  factory PortalConfigModel.fromJson(Map<String, dynamic> json) {
    return PortalConfigModel(
      relationshipId: json['relationship_id'] as String?,
      occurrence: json['occurrence'] as String?,
      initialRow: (json['initial_row'] as num?)?.toInt() ?? 1,
      rowCount: (json['row_count'] as num?)?.toInt() ?? 5,
      rowHeight: (json['row_height'] as num?)?.toDouble(),
      showScrollBar: json['show_scroll_bar'] as bool? ?? true,
      allowCreation: json['allow_creation'] as bool? ?? false,
      allowDeletion: json['allow_deletion'] as bool? ?? false,
      sort: json['sort'] as String? ?? '',
    );
  }

  Map<String, dynamic> toJson() => {
        if (relationshipId != null) 'relationship_id': relationshipId,
        if (occurrence != null) 'occurrence': occurrence,
        'initial_row': initialRow,
        'row_count': rowCount,
        if (rowHeight != null) 'row_height': rowHeight,
        'show_scroll_bar': showScrollBar,
        'allow_creation': allowCreation,
        'allow_deletion': allowDeletion,
        if (sort.isNotEmpty) 'sort': sort,
      };

  PortalConfigModel copyWith({
    String? relationshipId,
    String? occurrence,
    bool clearRelationship = false,
    int? initialRow,
    int? rowCount,
    double? rowHeight,
    bool clearRowHeight = false,
    bool? showScrollBar,
    bool? allowCreation,
    bool? allowDeletion,
    String? sort,
  }) {
    return PortalConfigModel(
      relationshipId: clearRelationship ? null : (relationshipId ?? this.relationshipId),
      occurrence: clearRelationship ? null : (occurrence ?? this.occurrence),
      initialRow: initialRow ?? this.initialRow,
      rowCount: rowCount ?? this.rowCount,
      rowHeight: clearRowHeight ? null : (rowHeight ?? this.rowHeight),
      showScrollBar: showScrollBar ?? this.showScrollBar,
      allowCreation: allowCreation ?? this.allowCreation,
      allowDeletion: allowDeletion ?? this.allowDeletion,
      sort: sort ?? this.sort,
    );
  }
}

class LayoutObjectModel {
  final String id;
  final String type; // 'field', 'label', 'button', 'button_bar', 'portal', 'tab_control', 'slide_control', 'popover_button', 'chart', 'web_viewer', 'rect', 'rounded_rect', 'oval', 'line', 'media'
  final double x;
  final double y;
  final double width;
  final double height;
  final String text; // label or button text
  final String? name; // object name / identifier
  final String? tooltip;
  final FieldBindingModel? fieldBinding;
  final LayoutObjectStyle style;
  final Map<String, bool>? anchors; // top, bottom, left, right
  final bool isLocked;
  final Map<String, dynamic>? portalConfig;

  /// For an object of type `chart`, what it draws (#37).
  final Map<String, dynamic>? chartConfig;
  final ButtonActionModel? action; // buttons: what a click runs in Browse mode
  final int? tabOrder; // position in the Tab key sequence (1-based); null = reading order
  final LayoutMediaModel? media; // picture / document shown inside the object

  const LayoutObjectModel({
    required this.id,
    required this.type,
    required this.x,
    required this.y,
    required this.width,
    required this.height,
    this.text = '',
    this.name,
    this.tooltip,
    this.fieldBinding,
    this.style = const LayoutObjectStyle(),
    this.anchors,
    this.isLocked = false,
    this.portalConfig,
    this.chartConfig,
    this.action,
    this.tabOrder,
    this.media,
  });

  /// Objects that take keyboard focus in Browse mode and so have a tab order.
  bool get isTabStop => type == 'field' || type == 'button' || type == 'popover_button';

  /// The portal's setup, for an object of type `portal` (#36).
  PortalConfigModel get portal => PortalConfigModel.fromJson(portalConfig ?? const {});

  /// The chart's setup, for an object of type `chart` (#37).
  ChartConfigModel get chart => ChartConfigModel.fromJson(chartConfig ?? const {});

  double get right => x + width;
  double get bottom => y + height;

  /// True when [other] is drawn inside this object, which is what puts a field
  /// in a portal's row.
  bool contains(LayoutObjectModel other) =>
      other.id != id &&
      other.x >= x &&
      other.y >= y &&
      other.right <= right &&
      other.bottom <= bottom;

  /// Objects that display their `text` (labels, buttons and drawn shapes).
  bool get hasEditableText =>
      type == 'label' || type == 'button' || type == 'popover_button' || isShape;

  bool get isShape => type == 'rect' || type == 'rounded_rect' || type == 'oval';

  factory LayoutObjectModel.fromJson(Map<String, dynamic> json) {
    return LayoutObjectModel(
      id: json['id'] as String? ?? 'obj_${DateTime.now().millisecondsSinceEpoch}',
      type: json['type'] as String? ?? 'label',
      x: (json['x'] as num?)?.toDouble() ?? 0.0,
      y: (json['y'] as num?)?.toDouble() ?? 0.0,
      width: (json['width'] as num?)?.toDouble() ?? 120.0,
      height: (json['height'] as num?)?.toDouble() ?? 32.0,
      text: json['text'] as String? ?? '',
      name: json['name'] as String?,
      tooltip: json['tooltip'] as String?,
      fieldBinding: json['field_binding'] != null
          ? FieldBindingModel.fromJson(json['field_binding'] as Map<String, dynamic>)
          : null,
      style: json['style'] != null
          ? LayoutObjectStyle.fromJson(json['style'] as Map<String, dynamic>)
          : const LayoutObjectStyle(),
      anchors: (json['anchors'] as Map<String, dynamic>?)?.map(
        (k, v) => MapEntry(k, v as bool),
      ),
      isLocked: json['is_locked'] as bool? ?? false,
      portalConfig: json['portal_config'] as Map<String, dynamic>?,
      chartConfig: json['chart_config'] is Map
          ? Map<String, dynamic>.from(json['chart_config'] as Map)
          : null,
      action: json['action'] is Map
          ? ButtonActionModel.fromJson(Map<String, dynamic>.from(json['action'] as Map))
          : null,
      tabOrder: (json['tab_order'] as num?)?.toInt(),
      media: json['media'] is Map
          ? LayoutMediaModel.fromJson(Map<String, dynamic>.from(json['media'] as Map))
          : null,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'type': type,
        'x': x,
        'y': y,
        'width': width,
        'height': height,
        'text': text,
        if (name != null) 'name': name,
        if (tooltip != null) 'tooltip': tooltip,
        if (fieldBinding != null) 'field_binding': fieldBinding!.toJson(),
        'style': style.toJson(),
        if (anchors != null) 'anchors': anchors,
        if (isLocked) 'is_locked': true,
        if (portalConfig != null) 'portal_config': portalConfig,
        if (chartConfig != null) 'chart_config': chartConfig,
        if (action != null) 'action': action!.toJson(),
        if (tabOrder != null) 'tab_order': tabOrder,
        if (media != null) 'media': media!.toJson(),
      };

  /// Copies the object. The `clear*` flags remove an optional value, which a
  /// plain null argument cannot express.
  LayoutObjectModel copyWith({
    String? id,
    String? type,
    double? x,
    double? y,
    double? width,
    double? height,
    String? text,
    String? name,
    String? tooltip,
    FieldBindingModel? fieldBinding,
    LayoutObjectStyle? style,
    Map<String, bool>? anchors,
    bool? isLocked,
    Map<String, dynamic>? portalConfig,
    Map<String, dynamic>? chartConfig,
    ButtonActionModel? action,
    int? tabOrder,
    LayoutMediaModel? media,
    bool clearAction = false,
    bool clearTabOrder = false,
    bool clearMedia = false,
    bool clearPortalConfig = false,
  }) {
    return LayoutObjectModel(
      id: id ?? this.id,
      type: type ?? this.type,
      x: x ?? this.x,
      y: y ?? this.y,
      width: width ?? this.width,
      height: height ?? this.height,
      text: text ?? this.text,
      name: name ?? this.name,
      tooltip: tooltip ?? this.tooltip,
      fieldBinding: fieldBinding ?? this.fieldBinding,
      style: style ?? this.style,
      anchors: anchors ?? this.anchors,
      isLocked: isLocked ?? this.isLocked,
      portalConfig: clearPortalConfig ? null : (portalConfig ?? this.portalConfig),
      chartConfig: chartConfig ?? this.chartConfig,
      action: clearAction ? null : (action ?? this.action),
      tabOrder: clearTabOrder ? null : (tabOrder ?? this.tabOrder),
      media: clearMedia ? null : (media ?? this.media),
    );
  }
}

/// Effects played when a layout is shown in Browse mode.
const kLayoutTransitions = <String, String>{
  'none': 'None',
  'fade': 'Fade in',
  'slide_left': 'Slide from right',
  'slide_up': 'Slide from bottom',
  'zoom': 'Zoom in',
};

class LayoutDefinitionModel {
  final String id;
  final String name;
  final String tableOccurrence;
  final double width;
  final String theme;
  final String defaultView;
  final List<LayoutPartModel> parts;
  final List<LayoutObjectModel> objects;

  /// Layout background (#RRGGBB); null = white.
  final String? backgroundColor;

  /// Picture drawn behind every object; its `fit` is `cover`, `contain`,
  /// `fill` or `tile`.
  final LayoutMediaModel? backgroundImage;

  /// Script triggers: Perform Script actions run in Browse mode when the
  /// layout is shown (OnLayoutEnter) or left for another layout (OnLayoutExit).
  final ButtonActionModel? onLayoutEnter;
  final ButtonActionModel? onLayoutExit;

  /// Effect played when the layout is shown, a key of [kLayoutTransitions].
  final String transition;

  /// For a labels layout, the sheet its labels are cut from: how big one is
  /// and how many sit across and down the page (#33). Null for every other
  /// kind of layout.
  final Map<String, dynamic>? labelStock;

  const LayoutDefinitionModel({
    required this.id,
    required this.name,
    required this.tableOccurrence,
    this.width = 1024.0,
    this.theme = 'Enlightened',
    this.defaultView = 'form',
    this.parts = const [],
    this.objects = const [],
    this.backgroundColor,
    this.backgroundImage,
    this.onLayoutEnter,
    this.onLayoutExit,
    this.transition = 'none',
    this.labelStock,
  });

  /// True when this layout is a sheet of labels rather than something browsed.
  bool get isLabels => labelStock != null;

  /// Total height of the layout: the sum of its parts (the footer ends it).
  double get height => parts.fold<double>(0.0, (acc, p) => acc + p.height);

  // ─── Report structure (#32) ──────────────────────────────────────────────

  /// Where the body sits among the parts. A layout without one reports -1,
  /// and nothing above or below it counts as leading or trailing.
  int get bodyIndex => parts.indexWhere((p) => p.isBody);

  /// The y coordinate the given part starts at, which is what places the
  /// objects drawn inside it.
  double partTop(String partId) {
    double top = 0;
    for (final part in parts) {
      if (part.id == partId) return top;
      top += part.height;
    }
    return top;
  }

  /// The part an object is drawn in, decided by where its top edge falls, the
  /// way the designer's bands read.
  LayoutPartModel? partAt(double y) {
    double top = 0;
    for (final part in parts) {
      if (y < top + part.height) return part;
      top += part.height;
    }
    return parts.isEmpty ? null : parts.last;
  }

  /// The objects drawn inside one part, in layout coordinates.
  List<LayoutObjectModel> objectsIn(LayoutPartModel part) {
    final top = partTop(part.id);
    final bottom = top + part.height;
    return objects.where((o) => o.y >= top && o.y < bottom).toList();
  }

  /// The sub-summary parts above the body, outermost first: the ones printed
  /// before the records of a group.
  List<LayoutPartModel> get leadingSubSummaries {
    final body = bodyIndex;
    if (body < 0) return const [];
    return parts.sublist(0, body).where((p) => p.isSubSummary).toList();
  }

  /// The sub-summary parts below the body, innermost first: the ones printed
  /// after the records of a group.
  List<LayoutPartModel> get trailingSubSummaries {
    final body = bodyIndex;
    if (body < 0) return const [];
    return parts.sublist(body + 1).where((p) => p.isSubSummary).toList();
  }

  LayoutPartModel? get leadingGrandSummary =>
      parts.where((p) => p.type == LayoutPartType.leadingGrandSummary).firstOrNull;

  LayoutPartModel? get trailingGrandSummary =>
      parts.where((p) => p.type == LayoutPartType.trailingGrandSummary).firstOrNull;

  /// True when this layout is a report: it groups or totals rather than just
  /// listing records.
  bool get isReport => parts.any((p) => p.isSummaryPart);

  /// The break fields this report groups by, outermost first.
  ///
  /// A leading sub-summary groups from the outside in, reading down the parts;
  /// a trailing one groups from the inside out, reading up. A field used by
  /// both appears once.
  List<String> get breakFields {
    final fields = <String>[];
    void add(String? field) {
      if (field == null || field.isEmpty || fields.contains(field)) return;
      fields.add(field);
    }

    for (final part in leadingSubSummaries) {
      add(part.breakField);
    }
    for (final part in trailingSubSummaries.reversed) {
      add(part.breakField);
    }
    return fields;
  }

  /// The sort order the found set must be in for this report to read
  /// correctly: by every break field, outermost first.
  ///
  /// A report over records sorted any other way interleaves groups, so the
  /// renderer checks this rather than printing nonsense.
  List<String> get requiredSortOrder => breakFields;

  /// First layout of a table: one row per field, labelled with the field's
  /// label (what the user typed in New Field), not its SQL column name.
  ///
  /// [fields] carries both, so the layout shows "Home Address 1" while it binds
  /// to `home_address_1`. The body is sized to the rows it has to hold, so the
  /// fields of a wide table do not spill into the footer.
  factory LayoutDefinitionModel.defaultForTable(
    String toName,
    List<({String name, String label})> fields,
  ) {
    const headerHeight = 60.0;
    const footerHeight = 40.0;
    const firstRowY = 20.0;
    const rowHeight = 48.0;

    final rows = fields.where((f) => f.name != 'id').toList();
    final bodyHeight = math.max(120.0, firstRowY * 2 + rows.length * rowHeight);

    final parts = [
      const LayoutPartModel(id: 'header_part', type: 'header', height: headerHeight),
      LayoutPartModel(id: 'body_part', type: 'body', height: bodyHeight),
      const LayoutPartModel(id: 'footer_part', type: 'footer', height: footerHeight),
    ];

    final objects = <LayoutObjectModel>[
      LayoutObjectModel(
        id: 'title_label',
        type: 'label',
        x: 24,
        y: 16,
        width: 300,
        height: 28,
        text: '$toName Details',
        style: const LayoutObjectStyle(fontSize: 20, fontWeight: 'bold'),
      ),
    ];

    double currentY = headerHeight + firstRowY;
    for (final field in rows) {
      objects.add(LayoutObjectModel(
        id: 'lbl_${field.name}',
        type: 'label',
        x: 40,
        y: currentY + 6,
        width: 140,
        height: 24,
        text: field.label,
        style: const LayoutObjectStyle(fontSize: 13, fontWeight: 'bold', textAlign: 'right'),
      ));
      objects.add(LayoutObjectModel(
        id: 'fld_${field.name}',
        type: 'field',
        x: 190,
        y: currentY,
        width: 280,
        height: 36,
        fieldBinding: FieldBindingModel(fieldName: field.name),
      ));
      currentY += rowHeight;
    }

    return LayoutDefinitionModel(
      id: 'layout_${DateTime.now().millisecondsSinceEpoch}',
      name: '$toName Form',
      tableOccurrence: toName,
      width: 900.0,
      parts: parts,
      objects: objects,
    );
  }

  factory LayoutDefinitionModel.fromJson(Map<String, dynamic> json) {
    var rawParts = json['parts'] as List<dynamic>? ?? [];
    var rawObjs = json['objects'] as List<dynamic>? ?? [];

    return LayoutDefinitionModel(
      id: json['id'] as String? ?? 'layout_default',
      name: json['name'] as String? ?? 'Default Layout',
      tableOccurrence: json['table_occurrence'] as String? ?? '',
      width: (json['width'] as num?)?.toDouble() ?? 1024.0,
      theme: json['theme'] as String? ?? 'Enlightened',
      defaultView: json['default_view'] as String? ?? 'form',
      parts: rawParts.map((p) => LayoutPartModel.fromJson(p as Map<String, dynamic>)).toList(),
      objects: rawObjs.map((o) => LayoutObjectModel.fromJson(o as Map<String, dynamic>)).toList(),
      backgroundColor: json['background_color'] as String?,
      backgroundImage: json['background_image'] is Map
          ? LayoutMediaModel.fromJson(Map<String, dynamic>.from(json['background_image'] as Map))
          : null,
      onLayoutEnter: json['on_layout_enter'] is Map
          ? ButtonActionModel.fromJson(Map<String, dynamic>.from(json['on_layout_enter'] as Map))
          : null,
      onLayoutExit: json['on_layout_exit'] is Map
          ? ButtonActionModel.fromJson(Map<String, dynamic>.from(json['on_layout_exit'] as Map))
          : null,
      transition: json['transition'] as String? ?? 'none',
      labelStock: json['label_stock'] is Map
          ? Map<String, dynamic>.from(json['label_stock'] as Map)
          : null,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'table_occurrence': tableOccurrence,
        'width': width,
        'theme': theme,
        'default_view': defaultView,
        'parts': parts.map((p) => p.toJson()).toList(),
        'objects': objects.map((o) => o.toJson()).toList(),
        if (backgroundColor != null) 'background_color': backgroundColor,
        if (backgroundImage != null) 'background_image': backgroundImage!.toJson(),
        if (onLayoutEnter != null) 'on_layout_enter': onLayoutEnter!.toJson(),
        if (onLayoutExit != null) 'on_layout_exit': onLayoutExit!.toJson(),
        if (transition != 'none') 'transition': transition,
        if (labelStock != null) 'label_stock': labelStock,
      };

  LayoutDefinitionModel copyWith({
    String? id,
    String? name,
    String? tableOccurrence,
    double? width,
    String? theme,
    String? defaultView,
    List<LayoutPartModel>? parts,
    List<LayoutObjectModel>? objects,
    String? backgroundColor,
    bool clearBackgroundColor = false,
    LayoutMediaModel? backgroundImage,
    bool clearBackgroundImage = false,
    ButtonActionModel? onLayoutEnter,
    bool clearOnLayoutEnter = false,
    ButtonActionModel? onLayoutExit,
    bool clearOnLayoutExit = false,
    String? transition,
    Map<String, dynamic>? labelStock,
    bool clearLabelStock = false,
  }) {
    return LayoutDefinitionModel(
      id: id ?? this.id,
      name: name ?? this.name,
      tableOccurrence: tableOccurrence ?? this.tableOccurrence,
      width: width ?? this.width,
      theme: theme ?? this.theme,
      defaultView: defaultView ?? this.defaultView,
      parts: parts ?? this.parts,
      objects: objects ?? this.objects,
      backgroundColor: clearBackgroundColor ? null : (backgroundColor ?? this.backgroundColor),
      backgroundImage: clearBackgroundImage ? null : (backgroundImage ?? this.backgroundImage),
      onLayoutEnter: clearOnLayoutEnter ? null : (onLayoutEnter ?? this.onLayoutEnter),
      onLayoutExit: clearOnLayoutExit ? null : (onLayoutExit ?? this.onLayoutExit),
      transition: transition ?? this.transition,
      labelStock: clearLabelStock ? null : (labelStock ?? this.labelStock),
    );
  }
}
