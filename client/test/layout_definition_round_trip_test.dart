import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter_test/flutter_test.dart';

/// A stored layout with, at every level, keys the models read, keys only the
/// server or a migration uses (`portal`, `concealed`, `sort_fields`,
/// `filter_problem`) and keys no version knows.
Map<String, dynamic> storedLayout() => {
      'id': 'l1',
      'name': 'Orders',
      'table_occurrence': 'Orders',
      'width': 800,
      'theme': 'Enlightened',
      'default_view': 'form',
      'future_layout_key': {'a': 1},
      'parts': [
        {'id': 'b', 'type': 'body', 'height': 400, 'future_part_key': true},
      ],
      'objects': [
        {
          'id': 'p1',
          'type': 'portal',
          'x': 0,
          'y': 100,
          'width': 300,
          'height': 100,
          'name': 'Lines',
          'portal_config': {
            'relationship_id': 'r1',
            'occurrence': 'Lines',
            'initial_row': 1,
            'row_count': 4,
            'sort': '-qty',
            'filter': '0',
            'filter_problem': true,
            'sort_fields': [
              {'field_name': 'qty', 'table_occurrence': 'Lines', 'order': 'descending'},
              {'field_name': 'title', 'table_occurrence': 'Orders', 'order': 'ascending'},
            ],
            'future_portal_key': 'x',
          },
          'style': {'border_width': 1, 'future_style_key': 3},
        },
        {
          'id': 'f1',
          'type': 'field',
          'x': 10,
          'y': 110,
          'width': 50,
          'height': 20,
          'portal': 'p1',
          'field_binding': {
            'field_name': 'qty',
            'table_occurrence': 'Lines',
            'control_style': 'edit_box',
            'concealed': true,
            'future_binding_key': [1, 2],
          },
          'style': {
            'conditional_rules': [
              {'operator': '>', 'value': '3', 'bold': true, 'future_rule_key': 'r'},
            ],
          },
          'future_object_key': 'o',
        },
        {
          'id': 'f2',
          'type': 'field',
          'x': 100,
          'y': 110,
          'width': 50,
          'height': 20,
          'portal': '',
          'field_binding': {'field_name': 'note'},
        },
        {
          'id': 'f3',
          'type': 'field',
          'x': 200,
          'y': 110,
          'width': 50,
          'height': 20,
          'field_binding': {'field_name': 'price', 'table_occurrence': 'Lines'},
        },
        {
          'id': 'm1',
          'type': 'media',
          'x': 400,
          'y': 10,
          'width': 50,
          'height': 50,
          'media': {'kind': 'image', 'name': 'logo.png', 'url': 'https://example.com/logo.png', 'future_media_key': 1},
        },
      ],
    };

/// Every key [expected] has, at every depth, is in [actual] with the same
/// value (a number may come back as a double).
void expectKept(Object? actual, Object? expected, [String path = r'$']) {
  if (expected is Map) {
    expect(actual, isA<Map>(), reason: path);
    final map = actual! as Map;
    for (final e in expected.entries) {
      expect(map.containsKey(e.key), isTrue, reason: '$path.${e.key} was dropped');
      expectKept(map[e.key], e.value, '$path.${e.key}');
    }
  } else if (expected is List) {
    expect(actual, isA<List>(), reason: path);
    final list = actual! as List;
    expect(list.length, expected.length, reason: path);
    for (var i = 0; i < expected.length; i++) {
      expectKept(list[i], expected[i], '$path[$i]');
    }
  } else {
    expect(actual, expected, reason: path);
  }
}

LayoutObjectModel objectOf(LayoutDefinitionModel layout, String id) => layout.objects.firstWhere((o) => o.id == id);

void main() {
  test('a layout read and saved again keeps every stored key', () {
    final stored = storedLayout();
    final layout = LayoutDefinitionModel.fromJson(storedLayout());
    expectKept(layout.toJson(), stored);
    // And again, as a second save would.
    expectKept(LayoutDefinitionModel.fromJson(layout.toJson()).toJson(), stored);
    expect(objectOf(layout, 'f1').fieldBinding!.concealed, isTrue);
  });

  test("editing a portal's setup keeps what it does not change", () {
    final layout = LayoutDefinitionModel.fromJson(storedLayout());
    final portal = objectOf(layout, 'p1');
    // As the designer's inspector edits a portal.
    Map<String, dynamic> edit(PortalConfigModel Function(PortalConfigModel) change) =>
        portal.copyWith(portalConfig: change(portal.portal).toJson()).portalConfig!;

    final rows = edit((p) => p.copyWith(rowCount: 6));
    expect(rows['row_count'], 6);
    expectKept(rows, {...portal.portalConfig!, 'row_count': 6});

    final sorted = edit((p) => p.copyWith(sort: 'title'));
    expect(sorted.containsKey('sort_fields'), isFalse, reason: "FileMaker's sort order no longer holds");
    expect(sorted['filter_problem'], isTrue);

    final filtered = edit((p) => p.copyWith(filter: 'qty > 1'));
    expect(filtered.containsKey('filter_problem'), isFalse);
    expect(filtered['sort_fields'], isNotNull);

    final rebound = edit((p) => p.copyWith(relationshipId: 'r2', occurrence: 'Other'));
    expect(rebound.containsKey('sort_fields'), isFalse);
    expect(rebound['future_portal_key'], 'x');
  });

  test('an object moved or resized leaves the portal it said for the one its bounds put it in', () {
    final layout = LayoutDefinitionModel.fromJson(storedLayout());
    final row = objectOf(layout, 'f1');
    expect(row.copyWith(text: 'x').toJson()['portal'], 'p1');
    expect(row.copyWith(x: row.x).toJson()['portal'], 'p1', reason: 'not moved');
    final moved = row.copyWith(x: 20).toJson();
    expect(moved.containsKey('portal'), isFalse);
    expect(moved['future_object_key'], 'o');
    expect(row.copyWith(height: 30).toJson().containsKey('portal'), isFalse);
  });

  test("a portal's rows are the objects inside it that do not say otherwise", () {
    final layout = LayoutDefinitionModel.fromJson(storedLayout());
    final portal = objectOf(layout, 'p1');
    final rows = [for (final o in layout.objects) if (o.isRowObjectOf(portal)) o.id];
    expect(rows, ['f1', 'f3'], reason: 'f2 lies inside the portal but says it is in none');
  });
}
