// #36 — a layout shows the other side of a relationship. These render the real
// Browse mode and look at what came out, and at what the client asked the
// server for.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/data_browser_widget.dart';
import 'package:file4base_client/features/data_browser/related_records.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/main.dart';

const _companies = TableModel(id: 't-companies', name: 'companies', displayName: 'Companies', columns: [
  ColumnModel(id: 'cc0', tableId: 't-companies', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'cc1', tableId: 't-companies', name: 'company', displayName: 'Company', fieldType: 'TEXT'),
  ColumnModel(id: 'cc2', tableId: 't-companies', name: 'company_address', displayName: 'Company Address', fieldType: 'TEXT'),
]);

const _customers = TableModel(id: 't-customers', name: 'customers', displayName: 'Customers', columns: [
  ColumnModel(id: 'uc0', tableId: 't-customers', name: 'id', displayName: 'ID', fieldType: 'TEXT', isPrimaryKey: true),
  ColumnModel(id: 'uc1', tableId: 't-customers', name: 'company', displayName: 'Company', fieldType: 'TEXT'),
  ColumnModel(id: 'uc2', tableId: 't-customers', name: 'last_name', displayName: 'Last Name', fieldType: 'TEXT'),
]);

const _companiesOccurrence =
    TableOccurrenceModel(id: 'occ-companies', baseTableId: 't-companies', name: 'Companies', xPos: 100, yPos: 100);
const _customersOccurrence =
    TableOccurrenceModel(id: 'occ-customers', baseTableId: 't-customers', name: 'Customers', xPos: 300, yPos: 100);

const _relationship = RelationshipModel(
  id: 'rel-1',
  name: 'companies_customers',
  leftOccurrenceId: 'occ-companies',
  leftColumnId: 'cc1',
  rightOccurrenceId: 'occ-customers',
  rightColumnId: 'uc1',
  allowCreation: true,
);

/// One recorded call to the related-records endpoint.
class RelatedCall {
  final String table;
  final String id;
  final String relationshipId;
  final String? occurrence;
  final List<String>? sort;
  const RelatedCall(this.table, this.id, this.relationshipId, this.occurrence, this.sort);
}

class _RelatedApi extends ApiClient {
  final List<Map<String, dynamic>> parents;
  final List<Map<String, dynamic>> children;
  final List<RelationshipModel> relationships;

  final List<RelatedCall> relatedCalls = [];
  final List<Map<String, dynamic>> created = [];
  final List<(String, String, Map<String, dynamic>)> updates = [];
  final List<String> deletes = [];

  _RelatedApi({
    required this.parents,
    required this.children,
    this.relationships = const [_relationship],
  }) : super(baseUrl: 'http://localhost:1');

  @override
  Future<List<RelationshipModel>> listRelationships() async => relationships;

  @override
  Future<List<TableOccurrenceModel>> listOccurrences() async =>
      [_companiesOccurrence, _customersOccurrence];

  @override
  Future<List<TableModel>> listTables() async => [_companies, _customers];

  @override
  Future<List<ValueListModel>> listValueLists() async => const [];

  @override
  Future<List<Map<String, dynamic>>> listRows(String table,
          {int limit = 100, int offset = 0, String? sortBy, bool sortAsc = true, List<String>? sort}) async =>
      [for (final r in parents) Map<String, dynamic>.from(r)];

  @override
  Future<List<Map<String, dynamic>>> listRelatedRows(String table, String id,
      {required String relationshipId,
      String? occurrence,
      List<String>? sort,
      int limit = 100,
      int offset = 0}) async {
    relatedCalls.add(RelatedCall(table, id, relationshipId, occurrence, sort));
    return [for (final r in children) Map<String, dynamic>.from(r)];
  }

  @override
  Future<Map<String, dynamic>> createRelatedRow(String table, String id,
      {required String relationshipId,
      String? occurrence,
      Map<String, dynamic> values = const {}}) async {
    created.add(Map.of(values));
    final row = {'id': 'new-row', 'company': 'Favorite Bakery', ...values};
    children.add(row);
    return row;
  }

  @override
  Future<Map<String, dynamic>> updateRow(String table, String id, Map<String, dynamic> values) async {
    updates.add((table, id, Map.of(values)));
    final i = children.indexWhere((c) => c['id'] == id);
    if (i >= 0) children[i] = {...children[i], ...values};
    return {'id': id, ...values};
  }

  @override
  Future<void> deleteRow(String table, String id) async {
    deletes.add('$table/$id');
    children.removeWhere((c) => c['id'] == id);
  }
}

/// A Companies layout with a portal of Customers, holding one Last Name field.
LayoutDefinitionModel _layoutWithPortal({
  PortalConfigModel portal = const PortalConfigModel(relationshipId: 'rel-1', occurrence: 'Customers'),
  bool withRelatedField = false,
}) {
  return LayoutDefinitionModel(
    id: 'layout-companies',
    name: 'Companies Form',
    tableOccurrence: 'companies',
    parts: const [
      LayoutPartModel(id: 'h', type: 'header', height: 40),
      LayoutPartModel(id: 'b', type: 'body', height: 400),
      LayoutPartModel(id: 'f', type: 'footer', height: 40),
    ],
    objects: [
      const LayoutObjectModel(
        id: 'fld_company',
        type: 'field',
        x: 40,
        y: 50,
        width: 200,
        height: 30,
        fieldBinding: FieldBindingModel(fieldName: 'company'),
      ),
      if (withRelatedField)
        const LayoutObjectModel(
          id: 'fld_related',
          type: 'field',
          x: 40,
          y: 90,
          width: 200,
          height: 30,
          fieldBinding: FieldBindingModel(
            fieldName: 'last_name',
            relationshipId: 'rel-1',
            tableOccurrence: 'Customers',
          ),
        ),
      LayoutObjectModel(
        id: 'portal_customers',
        type: 'portal',
        x: 40,
        y: 160,
        width: 400,
        height: 150,
        portalConfig: portal.toJson(),
      ),
      // Inside the portal, so it is a portal row's field, not a layout field.
      const LayoutObjectModel(
        id: 'fld_in_portal',
        type: 'field',
        x: 50,
        y: 165,
        width: 200,
        height: 24,
        fieldBinding: FieldBindingModel(fieldName: 'last_name'),
      ),
    ],
  );
}

Future<_RelatedApi> _pump(
  WidgetTester tester, {
  LayoutDefinitionModel? layout,
  List<RelationshipModel> relationships = const [_relationship],
  List<Map<String, dynamic>>? children,
}) async {
  tester.view.physicalSize = const Size(1600, 1000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);

  final api = _RelatedApi(
    parents: [
      {'id': 'co-1', 'company': 'Favorite Bakery', 'company_address': '12 Mill Lane'},
    ],
    children: children ??
        [
          {'id': 'cu-1', 'company': 'Favorite Bakery', 'last_name': 'Soto'},
          {'id': 'cu-2', 'company': 'Favorite Bakery', 'last_name': 'Alvarez'},
        ],
    relationships: relationships,
  );

  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: DataBrowserWidget(
        table: _companies,
        apiClient: api,
        mode: OperationalMode.browse,
        layout: layout ?? _layoutWithPortal(),
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return api;
}

void main() {
  testWidgets('a portal shows one row per related record', (tester) async {
    final api = await _pump(tester);

    expect(find.byKey(const ValueKey('portal')), findsOneWidget);
    expect(find.byKey(const ValueKey('portal-row-cu-1')), findsOneWidget);
    expect(find.byKey(const ValueKey('portal-row-cu-2')), findsOneWidget);

    final cell = tester.widget<TextField>(find.byKey(const ValueKey('portal-cell-cu-1-last_name')));
    expect(cell.controller?.text, 'Soto');
    final second = tester.widget<TextField>(find.byKey(const ValueKey('portal-cell-cu-2-last_name')));
    expect(second.controller?.text, 'Alvarez');

    expect(api.relatedCalls.single.table, 'companies');
    expect(api.relatedCalls.single.id, 'co-1', reason: 'the record in hand is the one followed');
    expect(api.relatedCalls.single.relationshipId, 'rel-1');
    expect(api.relatedCalls.single.occurrence, 'occ-customers',
        reason: 'the side to read is named, so a self-join would work too');
  });

  testWidgets('a field drawn inside a portal is not also drawn on the layout', (tester) async {
    await _pump(tester);

    // The Last Name field is inside the portal, so it exists once per row and
    // not once more on the layout itself.
    expect(find.byKey(const ValueKey('portal-cell-cu-1-last_name')), findsOneWidget);
    expect(find.byKey(const ValueKey('portal-cell-cu-2-last_name')), findsOneWidget);

    // The layout's own fields: Company, plus one record-number box in the
    // record bar. Two portal cells make four text boxes in all.
    expect(tester.widgetList<TextField>(find.byType(TextField)).length, 4);
  });

  testWidgets('typing in a portal row writes to the related record', (tester) async {
    final api = await _pump(tester);

    await tester.enterText(find.byKey(const ValueKey('portal-cell-cu-1-last_name')), 'Soto Diaz');
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle(const Duration(seconds: 1));

    expect(api.updates, isNotEmpty);
    final (table, id, values) = api.updates.last;
    expect(table, 'customers', reason: 'the related record is written, not the one in hand');
    expect(id, 'cu-1');
    expect(values['last_name'], 'Soto Diaz');
  });

  testWidgets('a portal that allows creation offers a row that creates a related record',
      (tester) async {
    final api = await _pump(tester,
        layout: _layoutWithPortal(
          portal: const PortalConfigModel(
            relationshipId: 'rel-1',
            occurrence: 'Customers',
            allowCreation: true,
          ),
        ));

    expect(find.byKey(const ValueKey('portal-new-row')), findsOneWidget);

    await tester.enterText(find.byKey(const ValueKey('portal-new-cell')), 'Mendez');
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle();

    expect(api.created.single['last_name'], 'Mendez');
    expect(api.created.single.containsKey('company'), isFalse,
        reason: 'the match field belongs to the relationship, not to the client');
    expect(find.byKey(const ValueKey('portal-row-new-row')), findsOneWidget);

    final addRow = tester.widget<TextField>(find.byKey(const ValueKey('portal-new-cell')));
    expect(addRow.controller?.text, '',
        reason: 'the row is empty again, ready for the next record');
  });

  testWidgets('a portal that does not allow creation offers no such row', (tester) async {
    await _pump(tester);
    expect(find.byKey(const ValueKey('portal-new-row')), findsNothing);
  });

  testWidgets('a portal that allows deletion deletes the row\'s related record', (tester) async {
    final api = await _pump(tester,
        layout: _layoutWithPortal(
          portal: const PortalConfigModel(
            relationshipId: 'rel-1',
            occurrence: 'Customers',
            allowDeletion: true,
          ),
        ));

    await tester.tap(find.byKey(const ValueKey('portal-delete-cu-1')));
    await tester.pumpAndSettle();

    expect(api.deletes, ['customers/cu-1']);
    expect(find.byKey(const ValueKey('portal-row-cu-1')), findsNothing);
    expect(find.byKey(const ValueKey('portal-row-cu-2')), findsOneWidget);
  });

  testWidgets('a portal asks for the sort order it was set up with', (tester) async {
    final api = await _pump(tester,
        layout: _layoutWithPortal(
          portal: const PortalConfigModel(
            relationshipId: 'rel-1',
            occurrence: 'Customers',
            sort: 'last_name, -id',
          ),
        ));

    expect(api.relatedCalls.single.sort, ['last_name', '-id']);
  });

  testWidgets('a portal with no relationship says so instead of showing nothing', (tester) async {
    await _pump(tester, layout: _layoutWithPortal(portal: const PortalConfigModel()));

    expect(find.textContaining('choose a relationship'), findsOneWidget);
    expect(find.byKey(const ValueKey('portal-row-cu-1')), findsNothing);
  });

  testWidgets('a portal whose relationship was deleted still draws', (tester) async {
    await _pump(tester, relationships: const []);

    expect(find.byKey(const ValueKey('portal-row-cu-1')), findsNothing);
    // The rest of the layout is unharmed: deleting a relationship must not
    // make a layout unusable.
    expect(find.byType(TextField), findsWidgets);
  });

  testWidgets('a portal with no related records says so', (tester) async {
    await _pump(tester, children: []);

    expect(find.text('No related records'), findsOneWidget);
  });

  testWidgets('a related field shows the first matching record, and is not typed into',
      (tester) async {
    final api = await _pump(tester, layout: _layoutWithPortal(withRelatedField: true));

    expect(find.byKey(const ValueKey('related-field')), findsOneWidget);
    expect(
      find.descendant(
        of: find.byKey(const ValueKey('related-field')),
        matching: find.text('Soto'),
      ),
      findsOneWidget,
      reason: 'it shows the first related record, as text rather than as a box to type in',
    );

    // It asked for one record only: a related field shows one.
    final forField = api.relatedCalls.where((c) => c.sort == null);
    expect(forField, isNotEmpty);
  });

  group('resolving which side of a relationship a layout reads', () {
    RelatedContext context(String fromTable) => RelatedContext(
          apiClient: ApiClient(baseUrl: 'http://localhost:1'),
          fromTable: fromTable,
          recordId: 'r1',
          relationshipsById: const {'rel-1': _relationship},
          occurrencesById: const {
            'occ-companies': _companiesOccurrence,
            'occ-customers': _customersOccurrence,
          },
          tablesById: const {'t-companies': _companies, 't-customers': _customers},
          formatValue: (col, raw) => raw?.toString() ?? '',
        );

    test('the named occurrence wins', () {
      final target = context('companies').resolve('rel-1', 'Customers');
      expect(target?.table.name, 'customers');
    });

    test('without one, the other table is taken', () {
      expect(context('companies').resolve('rel-1', null)?.table.name, 'customers');
      expect(context('customers').resolve('rel-1', null)?.table.name, 'companies');
    });

    test('a relationship that is not there resolves to nothing', () {
      expect(context('companies').resolve('rel-gone', null), isNull);
      expect(context('companies').resolve(null, null), isNull);
    });

    test('an occurrence that is not one of the two resolves to nothing', () {
      expect(context('companies').resolve('rel-1', 'Invoices'), isNull);
    });

    test('a table the relationship does not reach resolves to nothing', () {
      expect(context('invoices').resolve('rel-1', null), isNull);
    });
  });

  group('the occurrences a layout can reach', () {
    test('both ends of a relationship are offered to the table on the other side', () {
      final fromCompanies = reachableTargets(
        fromTable: 'companies',
        relationships: const [_relationship],
        occurrencesById: const {
          'occ-companies': _companiesOccurrence,
          'occ-customers': _customersOccurrence,
        },
        tablesById: const {'t-companies': _companies, 't-customers': _customers},
      );
      expect(fromCompanies.map((t) => t.occurrence.name), ['Customers']);

      final fromCustomers = reachableTargets(
        fromTable: 'customers',
        relationships: const [_relationship],
        occurrencesById: const {
          'occ-companies': _companiesOccurrence,
          'occ-customers': _customersOccurrence,
        },
        tablesById: const {'t-companies': _companies, 't-customers': _customers},
      );
      expect(fromCustomers.map((t) => t.occurrence.name), ['Companies']);
    });

    test('a table with no relationships reaches nothing', () {
      expect(
        reachableTargets(
          fromTable: 'invoices',
          relationships: const [_relationship],
          occurrencesById: const {
            'occ-companies': _companiesOccurrence,
            'occ-customers': _customersOccurrence,
          },
          tablesById: const {'t-companies': _companies, 't-customers': _customers},
        ),
        isEmpty,
      );
    });
  });

  group('portal setup', () {
    test('the row height falls back to the portal divided by its rows', () {
      expect(const PortalConfigModel(rowCount: 5).effectiveRowHeight(150), 30);
      expect(const PortalConfigModel(rowCount: 5, rowHeight: 40).effectiveRowHeight(150), 40);
      expect(const PortalConfigModel(rowCount: 0).effectiveRowHeight(10), 24,
          reason: 'a row never collapses to nothing');
    });

    test('it survives being written out and read back', () {
      const config = PortalConfigModel(
        relationshipId: 'rel-1',
        occurrence: 'Customers',
        initialRow: 2,
        rowCount: 8,
        rowHeight: 26,
        showScrollBar: false,
        allowCreation: true,
        allowDeletion: true,
        sort: 'last_name',
      );
      final back = PortalConfigModel.fromJson(config.toJson());

      expect(back.relationshipId, 'rel-1');
      expect(back.occurrence, 'Customers');
      expect(back.initialRow, 2);
      expect(back.rowCount, 8);
      expect(back.rowHeight, 26);
      expect(back.showScrollBar, isFalse);
      expect(back.allowCreation, isTrue);
      expect(back.allowDeletion, isTrue);
      expect(back.sortFields, ['last_name']);
    });

    test('an unset portal knows it shows nothing', () {
      expect(const PortalConfigModel().isBound, isFalse);
      expect(const PortalConfigModel(relationshipId: 'rel-1').isBound, isTrue);
    });
  });

  group('what belongs to a portal', () {
    const portal = LayoutObjectModel(id: 'p', type: 'portal', x: 40, y: 160, width: 400, height: 150);

    test('an object drawn inside it belongs to its rows', () {
      const inside = LayoutObjectModel(id: 'a', type: 'field', x: 50, y: 165, width: 200, height: 24);
      expect(portal.contains(inside), isTrue);
    });

    test('an object that overhangs the edge does not', () {
      const overhanging =
          LayoutObjectModel(id: 'b', type: 'field', x: 50, y: 165, width: 500, height: 24);
      expect(portal.contains(overhanging), isFalse);
    });

    test('an object elsewhere on the layout does not', () {
      const elsewhere = LayoutObjectModel(id: 'c', type: 'field', x: 40, y: 50, width: 200, height: 30);
      expect(portal.contains(elsewhere), isFalse);
    });

    test('the portal does not contain itself', () {
      expect(portal.contains(portal), isFalse);
    });
  });

  group('a qualified field binding', () {
    test('writes itself as Occurrence::field', () {
      const binding = FieldBindingModel(
        fieldName: 'company_address',
        relationshipId: 'rel-1',
        tableOccurrence: 'Companies',
      );
      expect(binding.isRelated, isTrue);
      expect(binding.qualifiedName, 'Companies::company_address');
    });

    test('a field of the record in hand is just its name', () {
      const binding = FieldBindingModel(fieldName: 'company', tableOccurrence: 'customers');
      expect(binding.isRelated, isFalse);
      expect(binding.qualifiedName, 'company');
    });

    test('it survives being written out and read back', () {
      const binding = FieldBindingModel(
        fieldName: 'company_address',
        relationshipId: 'rel-1',
        tableOccurrence: 'Companies',
        valueListId: 'vl-1',
        controlStyle: FieldControlStyle.dropDownList,
      );
      final back = FieldBindingModel.fromJson(binding.toJson());
      expect(back.relationshipId, 'rel-1');
      expect(back.tableOccurrence, 'Companies');
      expect(back.valueListId, 'vl-1');
    });

    test('unsetting the relationship makes it a field of the record in hand', () {
      const binding = FieldBindingModel(
        fieldName: 'company_address',
        relationshipId: 'rel-1',
        tableOccurrence: 'Companies',
      );
      expect(binding.copyWith(clearRelationship: true).isRelated, isFalse);
    });

    test('toggling an entry option keeps the relationship and the value list', () {
      const binding = FieldBindingModel(
        fieldName: 'customer_type',
        relationshipId: 'rel-1',
        tableOccurrence: 'Companies',
        valueListId: 'vl-1',
      );
      final toggled = binding.copyWith(allowBrowseEntry: false);
      expect(toggled.relationshipId, 'rel-1');
      expect(toggled.valueListId, 'vl-1');
      expect(toggled.allowBrowseEntry, isFalse);
    });
  });
}
