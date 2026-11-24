import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/export_records_dialog.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:file4base_client/features/schema_manager/field_options_dialog.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('a column reads its maximum number of repetitions', () {
    final repeating = ColumnModel.fromJson(const {
      'id': 'c1',
      'table_id': 't1',
      'name': 'reading',
      'display_name': 'Reading',
      'field_type': 'NUMBER',
      'max_repetitions': 4,
    });
    expect(repeating.maxRepetitions, 4);
    expect(repeating.isRepeating, isTrue);

    final single = ColumnModel.fromJson(const {
      'id': 'c2',
      'table_id': 't1',
      'name': 'label',
      'display_name': 'Label',
      'field_type': 'TEXT',
    });
    expect(single.maxRepetitions, 1);
    expect(single.isRepeating, isFalse);
  });

  test('repetition n of a field is keyed field[n], the first being the field', () {
    expect(ColumnModel.repetitionKey('reading', 1), 'reading');
    expect(ColumnModel.repetitionKey('reading', 3), 'reading[3]');
  });

  test('the maximum number of repetitions is validated', () {
    expect(repetitionsError('1'), isNull);
    expect(repetitionsError(' 32000 '), isNull);
    expect(repetitionsError('0'), isNotNull);
    expect(repetitionsError('32001'), isNotNull);
    expect(repetitionsError('2.5'), isNotNull);
    expect(repetitionsError(''), isNotNull);
    expect(repetitionsError('3', fieldType: 'SUMMARY'), isNotNull);
    expect(repetitionsError('1', fieldType: 'SUMMARY'), isNull);
  });

  test('a layout object shows the repetitions it names, all by default', () {
    const all = FieldBindingModel(fieldName: 'reading');
    expect(all.shownRepetitions(5), 5);
    expect(all.shownRepetitions(1), 1);
    expect(all.repetitionOrientation, RepetitionOrientation.vertical);

    final some = FieldBindingModel.fromJson(const {
      'field_name': 'reading',
      'repetitions_shown': 2,
      'repetition_orientation': 'horizontal',
    });
    expect(some.shownRepetitions(5), 2);
    expect(some.shownRepetitions(1), 1);
    expect(some.repetitionOrientation, RepetitionOrientation.horizontal);
    expect(FieldBindingModel.fromJson(some.toJson()).repetitionsShown, 2);
    expect(some.toJson()['repetition_orientation'], 'horizontal');
    expect(all.toJson().containsKey('repetition_orientation'), isFalse);
  });

  test('a repeating field exports as one cell separated by 0x1D', () {
    const col = ColumnModel(
      id: 'c1',
      tableId: 't1',
      name: 'reading',
      displayName: 'Reading',
      fieldType: 'NUMBER',
      maxRepetitions: 4,
    );
    expect(repetitionsCell(const {'reading': '4', 'reading[3]': '7.5'}, col), '4\x1d\x1d7.5');
    expect(repetitionsCell(const {'reading[2]': '1'}, col), '\x1d1');
    expect(repetitionsCell(const <String, dynamic>{}, col), '');
    const single = ColumnModel(id: 'c2', tableId: 't1', name: 'label', displayName: 'Label', fieldType: 'TEXT');
    expect(repetitionsCell(const {'label': 'x'}, single), 'x');
  });
}
