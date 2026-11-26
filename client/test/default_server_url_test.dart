import 'package:file4base_client/core/api/default_server_url.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('defaultServerUrl (#202)', () {
    test('the desktop app talks to the API port directly', () {
      expect(defaultServerUrl(web: false), desktopDefaultServerUrl);
      expect(defaultServerUrl(web: false, page: Uri.parse('https://files.example.lan:3443/')), desktopDefaultServerUrl);
    });

    test('the Web Client calls the API on the origin it was loaded from', () {
      expect(defaultServerUrl(web: true, page: Uri.parse('http://localhost:3000/#/login')), 'http://localhost:3000');
      expect(defaultServerUrl(web: true, page: Uri.parse('https://files.example.lan:3443/index.html?x=1')),
          'https://files.example.lan:3443');
      expect(defaultServerUrl(web: true, page: Uri.parse('https://files.example.lan/')), 'https://files.example.lan');
    });

    test('a page that is not served over http(s) keeps the desktop default', () {
      expect(defaultServerUrl(web: true, page: Uri.parse('file:///tmp/index.html')), desktopDefaultServerUrl);
    });
  });
}
