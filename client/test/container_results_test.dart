import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('a container calculation is stored and shown as a container', () {
    const result = ColumnModel(
      id: 'c1',
      tableId: 't1',
      name: 'shown',
      displayName: 'Shown',
      fieldType: 'CALCULATION',
      calculationFormula: '{"formula":"If ( side = \\"a\\" ; front ; back )","result_type":"Container"}',
    );
    expect(result.storageType, 'CONTAINER');

    const text = ColumnModel(
      id: 'c2',
      tableId: 't1',
      name: 'caption',
      displayName: 'Caption',
      fieldType: 'CALCULATION',
      calculationFormula: '{"formula":"GetAsText ( front )","result_type":"Text"}',
    );
    expect(text.storageType, 'TEXT');

    const field = ColumnModel(id: 'c3', tableId: 't1', name: 'front', displayName: 'Front', fieldType: 'CONTAINER');
    expect(field.storageType, 'CONTAINER');
  });
}
