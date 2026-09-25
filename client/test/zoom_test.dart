import 'package:file4base_client/core/providers/zoom_provider.dart';
import 'package:file4base_client/core/widgets/file4base_menu_bar.dart';
import 'package:file4base_client/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('ZoomNotifier Unit Tests', () {
    test('Default zoom level is 1.0 (100%)', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);

      final zoom = container.read(zoomProvider);
      expect(zoom, 1.0);

      final notifier = container.read(zoomProvider.notifier);
      expect(notifier.percentage, 100);
      expect(notifier.canZoomIn, true);
      expect(notifier.canZoomOut, true);
    });

    test('zoomIn increases through standard low-code steps up to 400%', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);

      final notifier = container.read(zoomProvider.notifier);

      // 1.0 -> 1.5
      notifier.zoomIn();
      expect(container.read(zoomProvider), 1.5);
      expect(notifier.percentage, 150);

      // 1.5 -> 2.0
      notifier.zoomIn();
      expect(container.read(zoomProvider), 2.0);
      expect(notifier.percentage, 200);

      // 2.0 -> 3.0
      notifier.zoomIn();
      expect(container.read(zoomProvider), 3.0);
      expect(notifier.percentage, 300);

      // 3.0 -> 4.0
      notifier.zoomIn();
      expect(container.read(zoomProvider), 4.0);
      expect(notifier.percentage, 400);
      expect(notifier.canZoomIn, false);

      // Remains capped at 4.0
      notifier.zoomIn();
      expect(container.read(zoomProvider), 4.0);
    });

    test('zoomOut decreases through standard steps down to 25%', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);

      final notifier = container.read(zoomProvider.notifier);

      // 1.0 -> 0.75
      notifier.zoomOut();
      expect(container.read(zoomProvider), 0.75);
      expect(notifier.percentage, 75);

      // 0.75 -> 0.50
      notifier.zoomOut();
      expect(container.read(zoomProvider), 0.50);
      expect(notifier.percentage, 50);

      // 0.50 -> 0.25
      notifier.zoomOut();
      expect(container.read(zoomProvider), 0.25);
      expect(notifier.percentage, 25);
      expect(notifier.canZoomOut, false);

      // Remains capped at 0.25
      notifier.zoomOut();
      expect(container.read(zoomProvider), 0.25);
    });

    test('resetZoom resets any level back to 1.0 (100%)', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);

      final notifier = container.read(zoomProvider.notifier);
      notifier.setZoom(3.0);
      expect(container.read(zoomProvider), 3.0);

      notifier.resetZoom();
      expect(container.read(zoomProvider), 1.0);
      expect(notifier.percentage, 100);
    });

    test('setZoom clamps values between 0.25 and 4.0', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);

      final notifier = container.read(zoomProvider.notifier);
      notifier.setZoom(10.0);
      expect(container.read(zoomProvider), 4.0);

      notifier.setZoom(0.05);
      expect(container.read(zoomProvider), 0.25);
    });
  });

  group('File4BaseMenuBar Zoom Items Widget Tests', () {
    testWidgets('renders Zoom In, Zoom Out, Actual Size, and Zoom Level submenu', (tester) async {
      bool zoomInCalled = false;
      bool zoomOutCalled = false;
      bool resetZoomCalled = false;

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: File4BaseMenuBar(
              activeMode: OperationalMode.browse,
              onModeChanged: (_) {},
              onManageDatabase: () {},
              onOpenRemote: () {},
              onAbout: () {},
              isToolbarVisible: true,
              onToggleToolbar: (_) {},
              onZoomIn: () => zoomInCalled = true,
              onZoomOut: () => zoomOutCalled = true,
              onResetZoom: () => resetZoomCalled = true,
              zoomLevel: 1.0,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Open View menu
      final viewMenuFinder = find.text('View');
      expect(viewMenuFinder, findsOneWidget);
      await tester.tap(viewMenuFinder);
      await tester.pumpAndSettle();

      // Verify zoom options appear
      expect(find.text('Zoom In'), findsOneWidget);
      expect(find.text('Zoom Out'), findsOneWidget);
      expect(find.text('Actual Size (100%)'), findsOneWidget);
      expect(find.text('Zoom Level'), findsOneWidget);

      // Tap Zoom In
      await tester.tap(find.text('Zoom In'));
      await tester.pumpAndSettle();
      expect(zoomInCalled, true);

      // Re-open View menu and tap Zoom Out
      await tester.tap(viewMenuFinder);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Zoom Out'));
      await tester.pumpAndSettle();
      expect(zoomOutCalled, true);

      // Re-open View menu and tap Actual Size
      await tester.tap(viewMenuFinder);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Actual Size (100%)'));
      await tester.pumpAndSettle();
      expect(resetZoomCalled, true);
    });
  });
}
