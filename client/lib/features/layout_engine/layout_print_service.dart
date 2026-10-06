import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/rendering.dart';
import 'package:flutter/widgets.dart';
import 'package:pdf/pdf.dart';
import 'package:pdf/widgets.dart' as pw;

import '../../core/models/page_setup_model.dart';

/// Builds print-ready PDFs from the Preview mode sheet.
///
/// The sheet widget (wrapped in a [RepaintBoundary]) is captured as an image
/// at print resolution and placed on PDF pages of the Page Setup paper size,
/// so the printout contains only the previewed page, never the rest of the
/// application window. Content taller than one sheet continues on the next
/// pages.
class LayoutPrintService {
  /// Capture resolution: 3 device pixels per point (~216 dpi).
  static const double pixelRatio = 3.0;

  /// Captures the widget behind [boundaryKey] (a RepaintBoundary).
  static Future<ui.Image> capture(GlobalKey boundaryKey) async {
    final boundary = boundaryKey.currentContext?.findRenderObject() as RenderRepaintBoundary?;
    if (boundary == null) {
      throw StateError('The preview page is not on screen.');
    }
    return boundary.toImage(pixelRatio: pixelRatio);
  }

  static PdfPageFormat pageFormat(PageSetupModel setup) =>
      PdfPageFormat(setup.totalWidthPt, setup.totalHeightPt, marginAll: 0);

  /// Adds [sheet] (captured at [pixelRatio]) to [doc], scaled to the page
  /// width and split over as many pages as its height needs.
  static Future<void> addSheet(pw.Document doc, ui.Image sheet, PageSetupModel setup) async {
    final format = pageFormat(setup);
    // Source pixels that fit on one page once the image is scaled to the page width.
    final pxPerPage = (format.height * sheet.width / format.width).floor();
    for (var top = 0; top < sheet.height; top += pxPerPage) {
      final sliceHeight = (sheet.height - top).clamp(1, pxPerPage);
      // Ignore a sliver at the bottom (rounding of the sheet height).
      if (top > 0 && sliceHeight < 4 * pixelRatio) break;
      final png = await _slicePng(sheet, top, sliceHeight);
      final image = pw.MemoryImage(png);
      doc.addPage(pw.Page(
        pageFormat: format,
        margin: pw.EdgeInsets.zero,
        build: (_) => pw.Align(
          alignment: pw.Alignment.topLeft,
          child: pw.Image(image, width: format.width, fit: pw.BoxFit.fitWidth),
        ),
      ));
    }
  }

  static Future<Uint8List> _slicePng(ui.Image source, int top, int height) async {
    ui.Image slice = source;
    if (top != 0 || height != source.height) {
      final recorder = ui.PictureRecorder();
      final canvas = ui.Canvas(recorder);
      canvas.drawImageRect(
        source,
        ui.Rect.fromLTWH(0, top.toDouble(), source.width.toDouble(), height.toDouble()),
        ui.Rect.fromLTWH(0, 0, source.width.toDouble(), height.toDouble()),
        ui.Paint(),
      );
      slice = await recorder.endRecording().toImage(source.width, height);
    }
    final bytes = await slice.toByteData(format: ui.ImageByteFormat.png);
    if (!identical(slice, source)) slice.dispose();
    return bytes!.buffer.asUint8List();
  }
}
