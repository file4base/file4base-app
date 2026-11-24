import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:flutter_test/flutter_test.dart';

// A migrated value kept with its original text (#124) shows that text; the
// typed value stays what sorting, finds, calculations and scripts use.
void main() {
  const amount = ColumnModel(id: 'c1', tableId: 't1', name: 'amount', displayName: 'Amount', fieldType: 'NUMBER');
  const visited = ColumnModel(id: 'c2', tableId: 't1', name: 'visited', displayName: 'Visited', fieldType: 'DATE');

  test('a field with an original text shows it', () {
    final record = <String, dynamic>{
      'id': 'r1',
      'amount': '12',
      'visited': null,
      DataBrowserWidgetState.sourceTextKey: {'amount': '12-A', 'visited': 'soon'},
    };
    expect(DataBrowserWidgetState.recordFieldText(amount, record), '12-A');
    expect(DataBrowserWidgetState.recordFieldText(visited, record), 'soon');
    expect(record['amount'], '12');
  });

  test('a field without one shows its value as before', () {
    final record = <String, dynamic>{'id': 'r2', 'amount': 7, 'visited': '2026-11-13T00:00:00Z'};
    expect(DataBrowserWidgetState.recordFieldText(amount, record), '7');
    expect(DataBrowserWidgetState.recordFieldText(visited, record), '2026-11-13');
    expect(DataBrowserWidgetState.sourceText(record, 'amount'), isNull);
  });

  test('the dialog offers the locales the server accepts', () {
    expect(ApiClient.migrationSourceLocales, containsAll(<String>['fr-FR', 'es-ES', 'en-US']));
    expect(ApiClient.migrationSourceLocales.toSet().length, ApiClient.migrationSourceLocales.length);
  });
}
