import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';

import 'models/layout_definition.dart';

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

/// Replaces merge symbols in [text]: `{{CurrentDate}}`, `{{CurrentTime}}`,
/// `{{CurrentUser}}`, `{{PageNumber}}` and `{{field_name}}` (value of that
/// field in [record], matched case-insensitively). Unknown symbols are kept.
String resolveLayoutMergeText(
  String text, {
  Map<String, dynamic>? record,
  String? userName,
  int? pageNumber,
  DateTime? now,
}) {
  if (!text.contains('{{')) return text;
  final t = now ?? DateTime.now();
  return text.replaceAllMapped(_mergePattern, (m) {
    final key = m.group(1)!;
    switch (key.toLowerCase()) {
      case 'currentdate':
        return '${t.year.toString().padLeft(4, '0')}-${t.month.toString().padLeft(2, '0')}-${t.day.toString().padLeft(2, '0')}';
      case 'currenttime':
        return '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';
      case 'currentuser':
        return userName ?? m.group(0)!;
      case 'pagenumber':
        return pageNumber?.toString() ?? m.group(0)!;
    }
    if (record != null) {
      for (final entry in record.entries) {
        if (entry.key.toLowerCase() == key.toLowerCase()) {
          return entry.value?.toString() ?? '';
        }
      }
    }
    return m.group(0)!;
  });
}

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
  return TextStyle(
    fontSize: style.fontSize > 0 ? style.fontSize : 13,
    fontWeight: style.fontWeight == 'bold' ? FontWeight.bold : FontWeight.normal,
    color: parseLayoutColor(style.textColor) ?? fallbackColor ?? Colors.black87,
  );
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
  final LayoutObjectModel obj;
  final String text;

  /// Overrides the border, e.g. to show the selection in Layout mode.
  final BorderSide? borderOverride;

  const LayoutShapeView({super.key, required this.obj, this.text = '', this.borderOverride});

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
  final LayoutObjectModel obj;
  final Color? colorOverride;

  const LayoutLineView({super.key, required this.obj, this.colorOverride});

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
  final Color color;
  final double width;

  const _LayoutLinePainter({required this.color, required this.width});

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
  final LayoutMediaModel media;

  const LayoutMediaView({super.key, required this.media});

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
}) {
  if (obj.isShape) {
    return LayoutShapeView(
      obj: obj,
      text: resolveLayoutMergeText(obj.text, record: record, userName: userName, pageNumber: pageNumber),
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
  final LayoutDefinitionModel layout;

  const LayoutBackgroundView({super.key, required this.layout});

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
  final String effect;
  final Widget child;

  const LayoutEntryTransition({super.key, required this.effect, required this.child});

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
          scale: Tween(begin: 0.92, end: 1.0).animate(curve),
          child: FadeTransition(opacity: curve, child: widget.child),
        ),
      _ => widget.child,
    };
  }
}
