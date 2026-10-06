import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/models/page_setup_model.dart';
import 'package:file4base_client/features/layout_engine/layout_print_service.dart';
import 'package:pdf/widgets.dart' as pw;

Future<ui.Image> _image(int w, int h) {
  final recorder = ui.PictureRecorder();
  ui.Canvas(recorder).drawRect(ui.Rect.fromLTWH(0, 0, w.toDouble(), h.toDouble()), ui.Paint()..color = Colors.white);
  return recorder.endRecording().toImage(w, h);
}

void main() {
  const a4 = PageSetupModel();

  test('the PDF page has the Page Setup paper size', () {
    final format = LayoutPrintService.pageFormat(a4);
    expect(format.width, closeTo(a4.totalWidthPt, 0.01));
    expect(format.height, closeTo(a4.totalHeightPt, 0.01));
    final landscape = LayoutPrintService.pageFormat(a4.copyWith(isLandscape: true));
    expect(landscape.width, greaterThan(landscape.height));
  });

  testWidgets('a sheet that fits the paper gives one page, a taller one continues on more pages', (tester) async {
    await tester.runAsync(() async {
      // Same proportions as an A4 sheet: exactly one page.
      final oneSheet = await _image(595, 842);
      final single = pw.Document();
      await LayoutPrintService.addSheet(single, oneSheet, a4);
      expect(single.document.pdfPageList.pages, hasLength(1));

      // Three A4 heights of content: three pages.
      final tall = await _image(595, 842 * 3);
      final multi = pw.Document();
      await LayoutPrintService.addSheet(multi, tall, a4);
      expect(multi.document.pdfPageList.pages, hasLength(3));
      expect((await multi.save()).length, greaterThan(0));
    });
  });

  testWidgets('capture returns only the boundary content, at print resolution', (tester) async {
    final key = GlobalKey();
    await tester.pumpWidget(Center(
      child: RepaintBoundary(key: key, child: Container(width: 100, height: 50, color: Colors.white)),
    ));
    await tester.runAsync(() async {
      final img = await LayoutPrintService.capture(key);
      expect(img.width, (100 * LayoutPrintService.pixelRatio).round());
      expect(img.height, (50 * LayoutPrintService.pixelRatio).round());
    });
  });
}
