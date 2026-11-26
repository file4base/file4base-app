import 'package:flutter/foundation.dart' show kIsWeb;

/// The API address the desktop app uses until the user chooses another one.
const String desktopDefaultServerUrl = 'http://127.0.0.1:8080';

/// The File4Base Server address a client starts with.
///
/// The Web Client is served by the Nginx of `file4base-web`, which forwards
/// `/api/` and `/healthz` to the API, so it calls the API on the origin it was
/// loaded from. Same-origin requests need no CORS allowance, and the server
/// grants none by default (#202). The desktop app is not a browser and talks
/// to the API directly.
///
/// [web] and [page] stand for `kIsWeb` and `Uri.base`, for tests.
String defaultServerUrl({bool? web, Uri? page}) {
  if (web ?? kIsWeb) {
    final origin = page ?? Uri.base;
    if (origin.scheme == 'http' || origin.scheme == 'https') {
      return origin.origin;
    }
  }
  return desktopDefaultServerUrl;
}
