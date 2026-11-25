import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

// A migrated grouped button is a place to click over what it draws, and a
// button whose script could not be told is kept off. Synthetic data only.
const _table = TableModel(id: 't1', name: 'notes', displayName: 'Notes', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'title', displayName: 'Title', fieldType: 'varchar'),
]);

class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');

  int scriptReads = 0;

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [
        {'id': '1', 'title': 'First'},
      ];

  @override
  Future<List<ScriptModel>> listScripts({String? database}) async {
    scriptReads++;
    return const [];
  }
}

void main() {
  testWidgets('a switched-off grouped button draws its members and says why it does not run', (tester) async {
    tester.view.physicalSize = const Size(1400, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    final api = _Api();
    final layout = LayoutDefinitionModel.fromJson(const {
      'id': 'l',
      'name': 'L',
      'table_occurrence': 'notes',
      'objects': [
        {'id': 'm', 'type': 'label', 'x': 20, 'y': 20, 'width': 160, 'height': 20, 'text': 'Open the note'},
        {
          'id': 'g',
          'type': 'button',
          'x': 20,
          'y': 20,
          'width': 160,
          'height': 20,
          'text': '',
          'style': {'fill_color': '#00000000', 'border_width': 0},
          'action': {'type': 'perform_script', 'script_name': 'Deleted Script', 'enabled': false},
        },
      ],
    });
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(body: DataBrowserWidget(table: _table, apiClient: api, mode: OperationalMode.browse, layout: layout)),
    ));
    await tester.pumpAndSettle();

    expect(find.text('Open the note'), findsOneWidget);
    expect(find.text('Button'), findsNothing, reason: 'a button with an action and no label shows nothing of its own');
    await tester.tapAt(tester.getCenter(find.text('Open the note')));
    await tester.pumpAndSettle();
    expect(find.textContaining('switched off'), findsOneWidget);
    expect(api.scriptReads, 0, reason: 'a switched-off button runs nothing');
  });
}
