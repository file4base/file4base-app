import 'package:flutter_riverpod/flutter_riverpod.dart';

/// ZoomNotifier manages the application canvas magnification level.
/// Canonical low-code zoom levels: 25%, 50%, 75%, 100%, 150%, 200%, 300%, 400%.
class ZoomNotifier extends Notifier<double> {
  static const List<double> zoomSteps = [0.25, 0.50, 0.75, 1.0, 1.50, 2.0, 3.0, 4.0];

  @override
  double build() => 1.0;

  void zoomIn() {
    for (final step in zoomSteps) {
      if (step > state + 0.01) {
        state = step;
        return;
      }
    }
  }

  void zoomOut() {
    for (int i = zoomSteps.length - 1; i >= 0; i--) {
      if (zoomSteps[i] < state - 0.01) {
        state = zoomSteps[i];
        return;
      }
    }
  }

  void resetZoom() {
    state = 1.0;
  }

  void setZoom(double level) {
    state = level.clamp(zoomSteps.first, zoomSteps.last);
  }

  bool get canZoomIn => state < zoomSteps.last - 0.01;
  bool get canZoomOut => state > zoomSteps.first + 0.01;
  int get percentage => (state * 100).round();
}

final zoomProvider = NotifierProvider<ZoomNotifier, double>(ZoomNotifier.new);
