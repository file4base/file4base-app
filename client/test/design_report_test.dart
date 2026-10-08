// #49 — the design report: what the solution is made of, as a page to read
// or as text for version control.

import 'dart:convert';
import 'dart:typed_data';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/widgets/file4base_menu_bar.dart';
import 'package:file4base_client/features/tools/design_report_dialog.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

ApiClient _client({
  required List<String> requested,
  int status = 200,
  String body = '<!doctype html><html><body>Design</body></html>',
  String disposition = 'attachment; filename="Bakery_design.html"',
}) {
  final mock = MockClient((request) async {
    requested.add(request.url.toString());
    if (status != 200) {
      return http.Response(
        jsonEncode({'title': 'Forbidden', 'detail': 'your role does not allow this operation'}),
        status,
        headers: {'content-type': 'application/problem+json'},
      );
    }
    return http.Response(body, 200, headers: {
      'content-type': 'text/html; charset=utf-8',
      'content-disposition': disposition,
    });
  });
  return ApiClient(baseUrl: 'http://test-server:8080', httpClient: mock);
}

void main() {
  group('designReport', () {
    test('asks for the format and reads the name the server gave the file', () async {
      final requested = <String>[];
      final api = _client(requested: requested);

      final report = await api.designReport(format: 'xml', solution: 'Favorite Bakery');
      expect(requested.single, contains('/api/v1/solutions/design-report'));
      expect(requested.single, contains('format=xml'));
      expect(requested.single, contains('solution=Favorite%20Bakery'));
      expect(report.fileName, 'Bakery_design.html');
      expect(report.bytes, isA<Uint8List>());
    });

    test('falls back to a name of its own when the server sends no header', () async {
      final api = _client(requested: [], disposition: '');
      final report = await api.designReport(format: 'json');
      expect(report.fileName, 'solution_design.json');
    });
  });

  group('DesignReportDialog', () {
    final saved = <String>[];
    Future<void> pump(WidgetTester tester, ApiClient api,
        {DesignReportFormat initial = DesignReportFormat.html}) async {
      tester.view.physicalSize = const Size(1200, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: DesignReportDialog(
            apiClient: api,
            solutionName: 'Favorite Bakery',
            initialFormat: initial,
            save: (fileName, bytes) async => saved.add(fileName),
          ),
        ),
      ));
      await tester.pumpAndSettle();
    }

    testWidgets('offers the three forms and says what is never written', (tester) async {
      await pump(tester, _client(requested: []));

      expect(find.text('HTML — a report to read and print'), findsOneWidget);
      expect(find.text('XML — the design as text'), findsOneWidget);
      expect(find.text('JSON — the design as text'), findsOneWidget);
      expect(find.textContaining('No records, and no password'), findsOneWidget);
      expect(find.textContaining('cannot be diffed'), findsOneWidget);

      final html = tester.widget<RadioListTile<DesignReportFormat>>(
          find.byKey(const ValueKey('design-format-html')));
      expect(html.value, DesignReportFormat.html);
    });

    testWidgets('saving asks the server for the format that is chosen', (tester) async {
      final requested = <String>[];
      await pump(tester, _client(requested: requested), initial: DesignReportFormat.xml);

      await tester.tap(find.byKey(const ValueKey('design-save')));
      await tester.pumpAndSettle();

      expect(requested.single, contains('format=xml'));
      expect(saved, ['Bakery_design.html'], reason: 'the server names the file');
      expect(find.byKey(const ValueKey('design-saved')), findsOneWidget);
    });

    testWidgets('choosing another form changes what is asked for', (tester) async {
      final requested = <String>[];
      await pump(tester, _client(requested: requested));

      await tester.tap(find.byKey(const ValueKey('design-format-json')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('design-save')));
      await tester.pumpAndSettle();

      expect(requested.single, contains('format=json'));
    });

    testWidgets('a refusal is reported rather than read as success', (tester) async {
      await pump(tester, _client(requested: [], status: 403));

      await tester.tap(find.byKey(const ValueKey('design-save')));
      await tester.pumpAndSettle();

      expect(find.byKey(const ValueKey('design-error')), findsOneWidget);
      expect(find.byKey(const ValueKey('design-saved')), findsNothing);
    });
  });

  group('Tools menu (#48)', () {
    testWidgets('the report is enabled and what does not exist is disabled', (tester) async {
      tester.view.physicalSize = const Size(1400, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      var reportOpened = false;
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: File4BaseMenuBar(
            activeMode: OperationalMode.browse,
            onModeChanged: (_) {},
            onManageDatabase: () {},
            onOpenRemote: () {},
            onAbout: () {},
            isToolbarVisible: true,
            onToggleToolbar: (_) {},
            onDesignReport: () => reportOpened = true,
            onSaveDesignAsText: () {},
          ),
        ),
      ));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Tools'));
      await tester.pumpAndSettle();

      MenuItemButton item(String label) => tester.widget<MenuItemButton>(
          find.ancestor(of: find.text(label), matching: find.byType(MenuItemButton)));

      // Nothing opens a "roadmap" dialog any more.
      expect(item('Script Debugger').onPressed, isNull);
      expect(item('Data Viewer').onPressed, isNull);
      expect(item('Developer Utilities...').onPressed, isNull);

      expect(item('Database Design Report...').onPressed, isNotNull);
      expect(item('Save Design as XML...').onPressed, isNotNull);

      await tester.tap(find.text('Database Design Report...'));
      await tester.pumpAndSettle();
      expect(reportOpened, isTrue);
    });
  });
}
