import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart' show OperationalMode;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

// A migrated tab control shows one panel at a time: the default one first,
// another when its tab is clicked, and a control within a panel follows
// its panel. Synthetic data only.
const _table = TableModel(id: 't1', name: 'notes', displayName: 'Notes', columns: [
  ColumnModel(id: 'c0', tableId: 't1', name: 'id', displayName: 'ID', fieldType: 'int', isPrimaryKey: true),
  ColumnModel(id: 'c1', tableId: 't1', name: 'title', displayName: 'Title', fieldType: 'varchar'),
]);

class _Api extends ApiClient {
  _Api() : super(baseUrl: 'http://localhost:1');

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [
        {'id': '1', 'title': 'First'},
      ];
}

Map<String, dynamic> _label(String id, String text, String control, int index, {double y = 60}) => {
      'id': id,
      'type': 'label',
      'x': 30,
      'y': y,
      'width': 160,
      'height': 20,
      'text': text,
      'panel': {'control': control, 'index': index},
    };

final _layout = LayoutDefinitionModel.fromJson({
  'id': 'l',
  'name': 'L',
  'table_occurrence': 'notes',
  'objects': [
    {
      'id': 'tabs',
      'type': 'tab_control',
      'x': 20,
      'y': 20,
      'width': 400,
      'height': 200,
      'tabs': ['General', 'Details'],
      'default_panel': 1,
      'tab_height': 24,
    },
    _label('a', 'On the general tab', 'tabs', 0),
    {
      'id': 'inner',
      'type': 'slide_control',
      'x': 30,
      'y': 90,
      'width': 300,
      'height': 100,
      'tabs': ['', ''],
      'panel': {'control': 'tabs', 'index': 1},
    },
    _label('b', 'On the details tab', 'tabs', 1),
    _label('c', 'First slide', 'inner', 0, y: 100),
    _label('d', 'Second slide', 'inner', 1, y: 100),
  ],
});

void main() {
  test('panel tags and tab labels survive a round trip', () {
    final again = LayoutDefinitionModel.fromJson(_layout.toJson());
    final tabs = again.objects.firstWhere((o) => o.id == 'tabs');
    expect(tabs.tabs, ['General', 'Details']);
    expect(tabs.defaultPanel, 1);
    expect(tabs.tabHeight, 24);
    final c = again.objects.firstWhere((o) => o.id == 'c');
    expect((c.panelControl, c.panelIndex), ('inner', 0));
    expect(objectsOffPanel(again.objects), {'a', 'd'});
    expect(objectsOffPanel(again.objects, selected: {'tabs': 0}), {'inner', 'b', 'c', 'd'});
    expect(objectsOffPanel(again.objects, hidden: {'tabs'}), {'a', 'b', 'inner', 'c', 'd'});
  });

  testWidgets('Browse mode shows the panel in front and switches panels on a click', (tester) async {
    tester.view.physicalSize = const Size(1400, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(body: DataBrowserWidget(table: _table, apiClient: _Api(), mode: OperationalMode.browse, layout: _layout)),
    ));
    await tester.pumpAndSettle();

    expect(find.text('General'), findsOneWidget);
    expect(find.text('On the details tab'), findsOneWidget, reason: 'the default panel is in front');
    expect(find.text('First slide'), findsOneWidget);
    expect(find.text('On the general tab'), findsNothing);
    expect(find.text('Second slide'), findsNothing);

    await tester.tap(find.byKey(const ValueKey('panel-tab-inner-1')));
    await tester.pumpAndSettle();
    expect(find.text('Second slide'), findsOneWidget);
    expect(find.text('First slide'), findsNothing);

    await tester.tap(find.text('General'));
    await tester.pumpAndSettle();
    expect(find.text('On the general tab'), findsOneWidget);
    expect(find.text('On the details tab'), findsNothing);
    expect(find.text('Second slide'), findsNothing, reason: 'a control on a panel not in front is not shown, nor its panels');
  });
}
