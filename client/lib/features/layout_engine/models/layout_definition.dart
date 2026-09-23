
class LayoutPartModel {
  final String id;
  final String type; // 'top_navigation', 'title_header', 'header', 'body', 'subsummary', 'footer'
  final double height;
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

  LayoutPartModel copyWith({double? height}) {
    return LayoutPartModel(
      id: id,
      type: type,
      height: height ?? this.height,
      breakField: breakField,
    );
  }
}

class FieldBindingModel {
  final String? tableOccurrence;
  final String fieldName;
  final String controlStyle; // 'edit_box', 'drop_down_list', 'checkbox_set', 'drop_down_calendar'
  final bool allowBrowseEntry;
  final bool allowFindEntry;

  const FieldBindingModel({
    this.tableOccurrence,
    required this.fieldName,
    this.controlStyle = 'edit_box',
    this.allowBrowseEntry = true,
    this.allowFindEntry = true,
  });

  factory FieldBindingModel.fromJson(Map<String, dynamic> json) {
    return FieldBindingModel(
      tableOccurrence: json['table_occurrence'] as String?,
      fieldName: json['field_name'] as String? ?? '',
      controlStyle: json['control_style'] as String? ?? 'edit_box',
      allowBrowseEntry: json['allow_browse_entry'] as bool? ?? true,
      allowFindEntry: json['allow_find_entry'] as bool? ?? true,
    );
  }

  Map<String, dynamic> toJson() => {
        if (tableOccurrence != null) 'table_occurrence': tableOccurrence,
        'field_name': fieldName,
        'control_style': controlStyle,
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
}

class LayoutObjectModel {
  final String id;
  final String type; // 'field', 'label', 'button', 'portal'
  final double x;
  final double y;
  final double width;
  final double height;
  final String text; // label or button text
  final FieldBindingModel? fieldBinding;
  final LayoutObjectStyle style;

  const LayoutObjectModel({
    required this.id,
    required this.type,
    required this.x,
    required this.y,
    required this.width,
    required this.height,
    this.text = '',
    this.fieldBinding,
    this.style = const LayoutObjectStyle(),
  });

  factory LayoutObjectModel.fromJson(Map<String, dynamic> json) {
    return LayoutObjectModel(
      id: json['id'] as String? ?? 'obj_${DateTime.now().millisecondsSinceEpoch}',
      type: json['type'] as String? ?? 'label',
      x: (json['x'] as num?)?.toDouble() ?? 0.0,
      y: (json['y'] as num?)?.toDouble() ?? 0.0,
      width: (json['width'] as num?)?.toDouble() ?? 120.0,
      height: (json['height'] as num?)?.toDouble() ?? 32.0,
      text: json['text'] as String? ?? '',
      fieldBinding: json['field_binding'] != null
          ? FieldBindingModel.fromJson(json['field_binding'] as Map<String, dynamic>)
          : null,
      style: json['style'] != null
          ? LayoutObjectStyle.fromJson(json['style'] as Map<String, dynamic>)
          : const LayoutObjectStyle(),
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
        if (fieldBinding != null) 'field_binding': fieldBinding!.toJson(),
        'style': style.toJson(),
      };

  LayoutObjectModel copyWith({
    double? x,
    double? y,
    double? width,
    double? height,
    String? text,
    FieldBindingModel? fieldBinding,
    LayoutObjectStyle? style,
  }) {
    return LayoutObjectModel(
      id: id,
      type: type,
      x: x ?? this.x,
      y: y ?? this.y,
      width: width ?? this.width,
      height: height ?? this.height,
      text: text ?? this.text,
      fieldBinding: fieldBinding ?? this.fieldBinding,
      style: style ?? this.style,
    );
  }
}

class LayoutDefinitionModel {
  final String id;
  final String name;
  final String tableOccurrence;
  final double width;
  final String theme;
  final String defaultView;
  final List<LayoutPartModel> parts;
  final List<LayoutObjectModel> objects;

  const LayoutDefinitionModel({
    required this.id,
    required this.name,
    required this.tableOccurrence,
    this.width = 1024.0,
    this.theme = 'Enlightened',
    this.defaultView = 'form',
    this.parts = const [],
    this.objects = const [],
  });

  factory LayoutDefinitionModel.defaultForTable(String toName, List<String> fieldNames) {
    final parts = [
      const LayoutPartModel(id: 'header_part', type: 'header', height: 60.0),
      const LayoutPartModel(id: 'body_part', type: 'body', height: 400.0),
      const LayoutPartModel(id: 'footer_part', type: 'footer', height: 40.0),
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

    double currentY = 80;
    for (var fName in fieldNames) {
      if (fName == 'id') continue;
      // Label
      objects.add(LayoutObjectModel(
        id: 'lbl_$fName',
        type: 'label',
        x: 40,
        y: currentY + 6,
        width: 140,
        height: 24,
        text: fName,
        style: const LayoutObjectStyle(fontSize: 13, fontWeight: 'bold', textAlign: 'right'),
      ));
      // Field Input
      objects.add(LayoutObjectModel(
        id: 'fld_$fName',
        type: 'field',
        x: 190,
        y: currentY,
        width: 280,
        height: 36,
        fieldBinding: FieldBindingModel(fieldName: fName),
      ));
      currentY += 48;
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
    );
  }
}
