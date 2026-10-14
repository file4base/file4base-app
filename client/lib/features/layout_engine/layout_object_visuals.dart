import 'dart:convert';
import 'dart:typed_data';

import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter/material.dart';

/// Rendering shared by Layout, Browse, Find and Preview modes for the drawn
/// objects of a layout: shapes (`rect`, `rounded_rect`, `oval`), lines and
/// embedded media, plus the merge text of labels, buttons and shapes.

/// Parses `#RRGGBB`, `#AARRGGBB` or `#RGB`. Returns null for anything else,
/// so a malformed value typed in the inspector never breaks rendering.
Color? parseLayoutColor(String? value) {
  if (value == null) return null;
  var hex = value.trim();
  if (!hex.startsWith('#')) return null;
  hex = hex.substring(1);
  if (hex.length == 3) hex = hex.split('').map((c) => '$c$c').join();
  if (hex.length == 6) hex = 'FF$hex';
  if (hex.length != 8) return null;
  final v = int.tryParse(hex, radix: 16);
  return v == null ? null : Color(v);
}

/// Normalizes a color typed by the user (`fff`, `#1e88e5`) to `#RRGGBB`, or
/// returns null when it is not a valid color.
String? normalizeLayoutColor(String value) {
  var v = value.trim();
  if (v.isEmpty) return null;
  if (!v.startsWith('#')) v = '#$v';
  final c = parseLayoutColor(v);
  if (c == null) return null;
  final rgb = c.toARGB32() & 0xFFFFFF;
  return '#${rgb.toRadixString(16).padLeft(6, '0').toUpperCase()}';
}

/// Merge symbols that can be inserted in layout text (Insert menu).
class LayoutMergeSymbols {
  static const currentDate = '{{CurrentDate}}';
  static const currentTime = '{{CurrentTime}}';
  static const currentUser = '{{CurrentUser}}';
  static const pageNumber = '{{PageNumber}}';

  static String field(String fieldName) => '{{$fieldName}}';
}

final _mergePattern = RegExp(r'\{\{\s*([^{}]+?)\s*\}\}');

/// True when a line holds nothing a reader would call content: no letters and
/// no digits, so only spaces and the punctuation that joined the fields.
final _hasContent = RegExp(r'[0-9\p{L}]', unicode: true);

/// Replaces merge symbols in [text]: `{{CurrentDate}}`, `{{CurrentTime}}`,
/// `{{CurrentUser}}`, `{{PageNumber}}` and `{{field_name}}` (value of that
/// field in [record], matched case-insensitively). Unknown symbols are kept.
///
/// [formatField] decides how a field's stored value is written — a DATE comes
/// back from the API as a full timestamp and must read as a date — and
/// defaults to the value as it is.
///
/// With [collapseEmptyLines], a line that **held merge fields and came out
/// with nothing in it** is removed rather than printed blank (#33). That is
/// what lets a three-line address print as two when the second line is empty,
/// which is the whole point of a mailing label. A line carrying words of its
/// own is kept, because those words were meant to be there.
String resolveLayoutMergeText(
  String text, {
  Map<String, dynamic>? record,
  String? userName,
  String? layoutName,
  int? pageNumber,
  DateTime? now,
  String Function(String fieldName, Object? value)? formatField,
  bool collapseEmptyLines = false,
}) {
  if (!text.contains('{{')) return text;
  final t = now ?? DateTime.now();

  String resolveLine(String line) {
    var sawMerge = false;
    var sawValue = false;

    final resolved = line.replaceAllMapped(_mergePattern, (m) {
      final key = m.group(1)!.trim();
      String keep(String value) {
        if (value.isNotEmpty) sawValue = true;
        return value;
      }

      switch (key.toLowerCase()) {
        case 'currentdate':
          sawMerge = true;
          return keep('${t.year.toString().padLeft(4, '0')}-'
              '${t.month.toString().padLeft(2, '0')}-'
              '${t.day.toString().padLeft(2, '0')}');
        case 'currenttime':
          sawMerge = true;
          return keep('${t.hour.toString().padLeft(2, '0')}:'
              '${t.minute.toString().padLeft(2, '0')}');
        case 'currentuser':
          sawMerge = true;
          return userName == null ? m.group(0)! : keep(userName);
        case 'layoutname':
        case 'layout_name':
          sawMerge = true;
          return layoutName == null ? m.group(0)! : keep(layoutName);
        case 'pagenumber':
          sawMerge = true;
          return pageNumber == null ? m.group(0)! : keep(pageNumber.toString());
      }
      if (record != null) {
        final normKey = key.toLowerCase().replaceAll(' ', '_');
        for (final entry in record.entries) {
          final ek = entry.key.toLowerCase();
          if (ek == key.toLowerCase() || ek == normKey) {
            sawMerge = true;
            final value = formatField == null
                ? (entry.value?.toString() ?? '')
                : formatField(entry.key, entry.value);
            return keep(value);
          }
        }
      }
      // A symbol nothing answers is left as it is, so a mistyped field name is
      // visible on the layout rather than silently blank.
      return m.group(0)!;
    });

    if (!collapseEmptyLines || !sawMerge || sawValue) return resolved;
    // Every merge field on this line came out empty: the line goes unless it
    // carries words of its own.
    return _hasContent.hasMatch(resolved) ? resolved : _collapsed;
  }

  final lines = text.split('\n').map(resolveLine).toList();
  if (!collapseEmptyLines) return lines.join('\n');
  return lines.where((l) => l != _collapsed).join('\n');
}

/// Marks a line that collapsed, so it can be told apart from a line that was
/// legitimately empty in the text to begin with.
const _collapsed = '\u0000collapsed';

/// Orders tab stops (fields and buttons) the way the Tab key visits them in
/// Browse and Find modes: objects with an explicit `tabOrder` first, by that
/// number, then the others in reading order (top to bottom, left to right).
List<LayoutObjectModel> sortLayoutTabStops(Iterable<LayoutObjectModel> stops) {
  int reading(LayoutObjectModel a, LayoutObjectModel b) {
    final dy = a.y.compareTo(b.y);
    return dy != 0 ? dy : a.x.compareTo(b.x);
  }

  final list = stops.toList()
    ..sort((a, b) {
      if (a.tabOrder != null && b.tabOrder != null) {
        final c = a.tabOrder!.compareTo(b.tabOrder!);
        return c != 0 ? c : reading(a, b);
      }
      if (a.tabOrder != null) return -1;
      if (b.tabOrder != null) return 1;
      return reading(a, b);
    });
  return list;
}

TextStyle layoutTextStyle(LayoutObjectStyle style, {Color? fallbackColor}) {
  final decorations = <TextDecoration>[
    if (style.underline) TextDecoration.underline,
    if (style.strikethrough) TextDecoration.lineThrough,
  ];
  return TextStyle(
    fontFamily: (style.fontFamily != null && style.fontFamily!.isNotEmpty)
        ? style.fontFamily
        : null,
    fontSize: style.fontSize > 0 ? style.fontSize : 13,
    fontWeight: style.fontWeight == 'bold' ? FontWeight.bold : FontWeight.normal,
    fontStyle: style.fontStyle == 'italic' ? FontStyle.italic : FontStyle.normal,
    decoration: decorations.isEmpty
        ? TextDecoration.none
        : TextDecoration.combine(decorations),
    height: (style.lineSpacing - 1.2).abs() > 0.01 ? style.lineSpacing : null,
    color: parseLayoutColor(style.textColor) ?? fallbackColor ?? Colors.black87,
  );
}

/// Applies text casing transformation (`none`, `upper`, `lower`, `title`).
String applyLayoutTextCase(String text, String textCase) {
  switch (textCase) {
    case 'upper':
      return text.toUpperCase();
    case 'lower':
      return text.toLowerCase();
    case 'title':
      return text
          .split(' ')
          .map((w) => w.isEmpty
              ? w
              : '${w[0].toUpperCase()}${w.substring(1).toLowerCase()}')
          .join(' ');
    default:
      return text;
  }
}

/// Formats a raw field value according to [style]'s Number, Date, Time, or
/// Timestamp display format without altering the stored value.
String formatLayoutDisplayValue(Object? rawValue, LayoutObjectStyle style) {
  final raw = (rawValue?.toString() ?? '').trim();
  if (raw.isEmpty) return '';

  // 1. Number formatting
  final numFmt = style.numberFormat;
  if (numFmt != null && numFmt != 'general') {
    if (numFmt == 'boolean') {
      final lower = raw.toLowerCase();
      if (lower == 'true' || lower == '1' || lower == 'yes' || lower == 't') {
        return 'Yes';
      }
      if (lower == 'false' || lower == '0' || lower == 'no' || lower == 'f') {
        return 'No';
      }
    }
    final parsed = double.tryParse(raw.replaceAll(',', '.'));
    if (parsed != null) {
      final value = numFmt == 'percent' ? parsed * 100.0 : parsed;
      final decimals = style.decimalPlaces.clamp(0, 8);
      final fixed = value.abs().toStringAsFixed(decimals);
      final parts = fixed.split('.');
      var intPart = parts[0];
      if (style.useThousandsSeparator) {
        final buf = StringBuffer();
        for (var i = 0; i < intPart.length; i++) {
          if (i > 0 && (intPart.length - i) % 3 == 0) {
            buf.write(',');
          }
          buf.write(intPart[i]);
        }
        intPart = buf.toString();
      }
      final sign = value < 0 ? '-' : '';
      final decPart = parts.length > 1 && decimals > 0 ? '.${parts[1]}' : '';
      final numberStr = '$sign$intPart$decPart';
      if (numFmt == 'currency') {
        return '${style.currencySymbol}$numberStr';
      }
      if (numFmt == 'percent') {
        return '$numberStr%';
      }
      return numberStr;
    }
  }

  // 2. Date / Time / Timestamp formatting
  final dateFmt = style.dateFormat;
  final timeFmt = style.timeFormat;
  if ((dateFmt != null && dateFmt != 'as_entered') ||
      (timeFmt != null && timeFmt != 'as_entered')) {
    var dt = DateTime.tryParse(raw);
    if (dt == null && RegExp(r'^\d{1,2}:\d{2}(:\d{2})?$').hasMatch(raw)) {
      final padded = raw.indexOf(':') == 1 ? '0$raw' : raw;
      dt = DateTime.tryParse('2000-01-01T$padded');
    }
    if (dt != null) {
      const monthsShort = [
        'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
        'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec',
      ];
      const monthsFull = [
        'January', 'February', 'March', 'April', 'May', 'June',
        'July', 'August', 'September', 'October', 'November', 'December',
      ];
      const weekdays = [
        'Monday', 'Tuesday', 'Wednesday', 'Thursday',
        'Friday', 'Saturday', 'Sunday',
      ];
      String pad2(int n) => n.toString().padLeft(2, '0');
      String? datePart;
      if (dateFmt != null && dateFmt != 'as_entered') {
        datePart = switch (dateFmt) {
          'dd/mm/yyyy' => '${pad2(dt.day)}/${pad2(dt.month)}/${dt.year}',
          'mm/dd/yyyy' => '${pad2(dt.month)}/${pad2(dt.day)}/${dt.year}',
          'yyyy-mm-dd' => '${dt.year}-${pad2(dt.month)}-${pad2(dt.day)}',
          'd_mmm_yyyy' => '${dt.day} ${monthsShort[dt.month - 1]} ${dt.year}',
          'full' =>
            '${weekdays[dt.weekday - 1]}, ${dt.day} ${monthsFull[dt.month - 1]} ${dt.year}',
          _ => '${dt.year}-${pad2(dt.month)}-${pad2(dt.day)}',
        };
      }
      String? timePart;
      if (timeFmt != null && timeFmt != 'as_entered') {
        final h12 = dt.hour % 12 == 0 ? 12 : dt.hour % 12;
        final ampm = dt.hour >= 12 ? 'PM' : 'AM';
        timePart = switch (timeFmt) {
          '24h' => '${pad2(dt.hour)}:${pad2(dt.minute)}',
          '24h_sec' => '${pad2(dt.hour)}:${pad2(dt.minute)}:${pad2(dt.second)}',
          '12h' => '$h12:${pad2(dt.minute)} $ampm',
          _ => '${pad2(dt.hour)}:${pad2(dt.minute)}',
        };
      }
      if (datePart != null && timePart != null) return '$datePart $timePart';
      if (datePart != null) return datePart;
      if (timePart != null) return timePart;
    }
  }

  return applyLayoutTextCase(raw, style.textCase);
}

TextAlign layoutTextAlign(String align) => switch (align) {
      'center' => TextAlign.center,
      'right' => TextAlign.right,
      'justify' => TextAlign.justify,
      _ => TextAlign.left,
    };

Alignment layoutTextAlignment(String align) => switch (align) {
      'center' => Alignment.center,
      'right' => Alignment.centerRight,
      _ => Alignment.centerLeft,
    };

/// Outline of a drawn shape, used for its fill, border and media clipping.
ShapeBorder layoutShapeBorder(LayoutObjectModel obj, {BorderSide side = BorderSide.none}) {
  switch (obj.type) {
    case 'oval':
      return OvalBorder(side: side);
    case 'rect':
      return RoundedRectangleBorder(side: side);
    default:
      return RoundedRectangleBorder(
        side: side,
        borderRadius: BorderRadius.circular(obj.style.cornerRadius),
      );
  }
}

/// A rectangle, rounded rectangle or oval with its fill, line (border)
/// color and width, optional embedded media and optional text.
class LayoutShapeView extends StatelessWidget {

  const LayoutShapeView({required this.obj, super.key, this.text = '', this.borderOverride});
  final LayoutObjectModel obj;
  final String text;

  /// Overrides the border, e.g. to show the selection in Layout mode.
  final BorderSide? borderOverride;

  @override
  Widget build(BuildContext context) {
    final borderColor = parseLayoutColor(obj.style.borderColor);
    final side = borderOverride ??
        (borderColor == null || obj.style.borderWidth <= 0
            ? BorderSide.none
            : BorderSide(color: borderColor, width: obj.style.borderWidth));

    return Container(
      decoration: ShapeDecoration(
        color: parseLayoutColor(obj.style.fillColor),
        shape: layoutShapeBorder(obj, side: side),
      ),
      child: ClipPath(
        clipper: ShapeBorderClipper(shape: layoutShapeBorder(obj)),
        child: Stack(
          fit: StackFit.expand,
          children: [
            if (obj.media != null) LayoutMediaView(media: obj.media!),
            if (text.isNotEmpty)
              Padding(
                padding: const EdgeInsets.all(6),
                child: Align(
                  alignment: obj.style.textAlign == 'justify'
                      ? Alignment.center
                      : obj.style.textAlign == 'left'
                          ? Alignment.centerLeft
                          : obj.style.textAlign == 'right'
                              ? Alignment.centerRight
                              : Alignment.center,
                  child: Text(
                    text,
                    textAlign: layoutTextAlign(obj.style.textAlign),
                    style: layoutTextStyle(obj.style),
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// A straight line drawn across the longer side of its box, horizontal or
/// vertical, centered, with the object's line color and width.
class LayoutLineView extends StatelessWidget {

  const LayoutLineView({required this.obj, super.key, this.colorOverride});
  final LayoutObjectModel obj;
  final Color? colorOverride;

  @override
  Widget build(BuildContext context) {
    return CustomPaint(
      painter: _LayoutLinePainter(
        color: colorOverride ?? parseLayoutColor(obj.style.borderColor) ?? Colors.black54,
        width: obj.style.borderWidth <= 0 ? 1 : obj.style.borderWidth,
      ),
      child: const SizedBox.expand(),
    );
  }
}

class _LayoutLinePainter extends CustomPainter {

  const _LayoutLinePainter({required this.color, required this.width});
  final Color color;
  final double width;

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color
      ..strokeWidth = width
      ..strokeCap = StrokeCap.butt;
    if (size.height > size.width) {
      canvas.drawLine(Offset(size.width / 2, 0), Offset(size.width / 2, size.height), paint);
    } else {
      canvas.drawLine(Offset(0, size.height / 2), Offset(size.width, size.height / 2), paint);
    }
  }

  @override
  bool shouldRepaint(covariant _LayoutLinePainter old) => old.color != color || old.width != width;
}

/// Shows an embedded picture, or a tile with the icon and name of a PDF,
/// audio/video or file attachment.
class LayoutMediaView extends StatelessWidget {

  const LayoutMediaView({required this.media, super.key});
  final LayoutMediaModel media;

  static final Map<String, Uint8List> _decoded = {};

  Uint8List? _bytes() {
    final data = media.data;
    if (data == null || data.isEmpty) return null;
    final key = '${data.length}:${data.hashCode}';
    return _decoded.putIfAbsent(key, () {
      if (_decoded.length > 64) _decoded.clear();
      try {
        return base64Decode(data);
      } catch (_) {
        return Uint8List(0);
      }
    });
  }

  BoxFit get _fit => switch (media.fit) {
        'cover' => BoxFit.cover,
        'fill' => BoxFit.fill,
        _ => BoxFit.contain,
      };

  @override
  Widget build(BuildContext context) {
    if (media.isImage) {
      final bytes = _bytes();
      if (bytes != null && bytes.isNotEmpty) {
        return Image.memory(bytes, fit: _fit, gaplessPlayback: true,
            errorBuilder: (_, _, _) => _tile(Icons.broken_image_outlined));
      }
      if (media.url != null && media.url!.isNotEmpty) {
        return Image.network(media.url!, fit: _fit,
            errorBuilder: (_, _, _) => _tile(Icons.broken_image_outlined));
      }
      return _tile(Icons.image_outlined);
    }
    return _tile(switch (media.kind) {
      'pdf' => Icons.picture_as_pdf_outlined,
      'video' => Icons.movie_outlined,
      'audio' => Icons.audiotrack_outlined,
      _ => Icons.insert_drive_file_outlined,
    });
  }

  Widget _tile(IconData icon) {
    return Container(
      color: const Color(0x0F000000),
      padding: const EdgeInsets.all(4),
      child: FittedBox(
        fit: BoxFit.scaleDown,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 28, color: Colors.blueGrey),
            const SizedBox(height: 2),
            ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 160),
              child: Text(
                media.name.isEmpty ? media.kind.toUpperCase() : media.name,
                style: const TextStyle(fontSize: 10, color: Colors.blueGrey),
                overflow: TextOverflow.ellipsis,
                textAlign: TextAlign.center,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Renders the drawn object types (shapes, lines, media) for Browse, Find
/// and Preview modes, or returns null for other object types.
Widget? buildDrawnLayoutObject(
  LayoutObjectModel obj, {
  Map<String, dynamic>? record,
  String? userName,
  int? pageNumber,
  String Function(String fieldName, Object? value)? formatField,
}) {
  if (obj.isShape) {
    return LayoutShapeView(
      obj: obj,
      text: resolveLayoutMergeText(
        obj.text,
        record: record,
        userName: userName,
        pageNumber: pageNumber,
        formatField: formatField,
        // A line whose merge fields are all empty takes its line with it (#33).
        collapseEmptyLines: true,
      ),
    );
  }
  if (obj.type == 'line') return LayoutLineView(obj: obj);
  if (obj.type == 'media' && obj.media != null) {
    final border = parseLayoutColor(obj.style.borderColor);
    return Container(
      decoration: BoxDecoration(
        color: parseLayoutColor(obj.style.fillColor),
        border: border == null || obj.style.borderWidth <= 0
            ? null
            : Border.all(color: border, width: obj.style.borderWidth),
      ),
      child: LayoutMediaView(media: obj.media!),
    );
  }
  return null;
}

/// Background of a whole layout: its color (white by default) and its
/// background picture. Fills the canvas behind every object.
class LayoutBackgroundView extends StatelessWidget {

  const LayoutBackgroundView({required this.layout, super.key});
  final LayoutDefinitionModel layout;

  static final Map<String, Uint8List> _decoded = {};

  static Uint8List? _bytes(LayoutMediaModel media) {
    final data = media.data;
    if (data == null || data.isEmpty) return null;
    final key = '${data.length}:${data.hashCode}';
    return _decoded.putIfAbsent(key, () {
      if (_decoded.length > 16) _decoded.clear();
      try {
        return base64Decode(data);
      } catch (_) {
        return Uint8List(0);
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final image = layout.backgroundImage;
    final bytes = image != null && image.isImage ? _bytes(image) : null;
    ImageProvider? provider;
    if (bytes != null && bytes.isNotEmpty) {
      provider = MemoryImage(bytes);
    } else if (image?.url != null && image!.isImage) {
      provider = NetworkImage(image.url!);
    }
    return DecoratedBox(
      decoration: BoxDecoration(
        color: parseLayoutColor(layout.backgroundColor) ?? Colors.white,
        image: provider == null
            ? null
            : DecorationImage(
                image: provider,
                fit: switch (image!.fit) {
                  'contain' => BoxFit.contain,
                  'fill' => BoxFit.fill,
                  'tile' => BoxFit.none,
                  _ => BoxFit.cover,
                },
                alignment: image.fit == 'tile' ? Alignment.topLeft : Alignment.center,
                repeat: image.fit == 'tile' ? ImageRepeat.repeat : ImageRepeat.noRepeat,
              ),
      ),
      child: const SizedBox.expand(),
    );
  }
}

/// Plays a layout's [effect] (a key of [kLayoutTransitions]) once when it is
/// first shown. Give it a key per layout so switching layouts replays it.
class LayoutEntryTransition extends StatefulWidget {

  const LayoutEntryTransition({required this.effect, required this.child, super.key});
  final String effect;
  final Widget child;

  @override
  State<LayoutEntryTransition> createState() => _LayoutEntryTransitionState();
}

class _LayoutEntryTransitionState extends State<LayoutEntryTransition> with SingleTickerProviderStateMixin {
  late final AnimationController _ctrl = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 350),
    value: widget.effect == 'none' ? 1.0 : 0.0,
  )..forward();

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final curve = CurvedAnimation(parent: _ctrl, curve: Curves.easeOutCubic);
    return switch (widget.effect) {
      'fade' => FadeTransition(opacity: curve, child: widget.child),
      'slide_left' => SlideTransition(
          position: Tween(begin: const Offset(0.25, 0), end: Offset.zero).animate(curve),
          child: FadeTransition(opacity: curve, child: widget.child),
        ),
      'slide_up' => SlideTransition(
          position: Tween(begin: const Offset(0, 0.15), end: Offset.zero).animate(curve),
          child: FadeTransition(opacity: curve, child: widget.child),
        ),
      'zoom' => ScaleTransition(
          scale: Tween<double>(begin: 0.92, end: 1.0).animate(curve),
          child: FadeTransition(opacity: curve, child: widget.child),
        ),
      _ => widget.child,
    };
  }
}
