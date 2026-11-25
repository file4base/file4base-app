// The steps that write a field by name keep the occurrence it is qualified
// with — `Occurrence::Field`, or the `field_table` a migrated step carries —
// and hand a field of another occurrence to the window as a related field.
// None of them writes the field of the same name of the record in hand.

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:flutter_test/flutter_test.dart';

/// A window on `Items`, with a related `Lines` record.
class _Host implements LayoutActionHost, RelatedFieldHost, AutoEnterActionHost {
  final own = <String, String>{'note': 'own note'};
  final related = <String, String>{'note': 'related note'};
  final relatedWrites = <String>[];
  final autoEnter = <String>[];
  int relatedCode = 0;

  @override
  Future<bool> actionSetField(String field, String value) async {
    if (!own.containsKey(field)) return false;
    own[field] = value;
    return true;
  }

  @override
  String actionResolveText(String text) {
    final m = RegExp(r'^\{\{(.+)\}\}$').firstMatch(text);
    return m == null ? text : (own[m.group(1)] ?? text);
  }

  @override
  Future<bool> actionIsOwnOccurrence(String occurrence) async => occurrence.toLowerCase() == 'items';

  @override
  Future<String?> actionRelatedFieldText(String occurrence, String field) async =>
      occurrence == 'Lines' ? related[field] : null;

  @override
  Future<int> actionSetRelatedField(String occurrence, String field, String value) async {
    if (relatedCode != 0) return relatedCode;
    if (occurrence != 'Lines') return 103;
    if (!related.containsKey(field)) return 102;
    relatedWrites.add('$occurrence::$field');
    related[field] = value;
    return 0;
  }

  @override
  Future<int> actionSetNextSerialValue(String field, String value) async {
    autoEnter.add('serial $field');
    return 0;
  }

  @override
  Future<int> actionRelookupFieldContents(String field, {bool dialog = false}) async {
    autoEnter.add('relookup $field');
    return 0;
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

/// A window whose dialogs answer with what the user typed into each input.
class _DialogHost extends _Host implements ExtendedLayoutActionHost {
  List<Map<String, dynamic>>? shownInputs;

  @override
  Future<ScriptDialogResult> actionShowDialogEx({
    required String title,
    required String message,
    List<Map<String, dynamic>>? buttons,
    List<Map<String, dynamic>>? inputFields,
  }) async {
    shownInputs = inputFields;
    return ScriptDialogResult(inputValues: {
      for (final input in inputFields ?? const <Map<String, dynamic>>[]) input['field'].toString(): 'typed ${input['field']}',
    });
  }
}

/// A window that cannot write a related field.
class _PlainHost implements LayoutActionHost {
  final own = <String, String>{'note': 'own note'};

  @override
  Future<bool> actionSetField(String field, String value) async {
    if (!own.containsKey(field)) return false;
    own[field] = value;
    return true;
  }

  @override
  String actionResolveText(String text) => text == '{{note}}' ? own['note']! : text;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

Future<(LayoutActionResult, ScriptSessionState)> _run(LayoutActionHost host, String step, Map<String, dynamic> params,
    {bool capture = false}) async {
  final session = ScriptSessionState()
    ..errorCapture = capture
    ..clipboard = 'pasted';
  final result = await LayoutActionRunner(apiClient: ApiClient(baseUrl: 'http://localhost:1'), host: host)
      .runStep(step, params, depth: 0, callContext: ScriptCallContext(session: session));
  return (result, session);
}

void main() {
  // Both ways a step names the related field.
  const forms = <Map<String, dynamic>>[
    {'field': 'note', 'field_table': 'Lines'},
    {'field': 'Lines::note'},
  ];

  final steps = <(String, Map<String, dynamic>, String)>[
    ('Set Field', {'value': '"set"', 'value_is_calculation': true}, 'set'),
    ('Insert Calculated Result', {'value': '"+"', 'select': false}, 'related note+'),
    ('Paste', {}, 'pasted'),
    ('Clear', {}, ''),
    ('Cut', {}, ''),
  ];

  for (final (step, extra, expected) in steps) {
    for (final form in forms) {
      test('$step on $form writes the related field, not the record in hand', () async {
        final host = _Host();
        final (result, session) = await _run(host, step, {...form, ...extra});
        expect(result.completed, isTrue, reason: result.message);
        expect(host.related['note'], expected);
        expect(host.relatedWrites, ['Lines::note']);
        expect(host.own['note'], 'own note');
        if (step == 'Cut') expect(session.clipboard, 'related note');
      });
    }
  }

  test('Set Field By Name keeps the occurrence of the name it computes', () async {
    final host = _Host();
    final (result, _) = await _run(host, 'Set Field By Name', {
      'target_field': '"Lines::" & "Note"',
      'target_field_is_calculation': true,
      'value': '"by name"',
      'value_is_calculation': true,
    });
    expect(result.completed, isTrue, reason: result.message);
    expect(host.related['note'], 'by name');
    expect(host.own['note'], 'own note');
  });

  test('Insert Current Date writes the related field', () async {
    final host = _Host();
    final (result, _) = await _run(host, 'Insert Current Date', {'field': 'note', 'field_table': 'Lines', 'select': true});
    expect(result.completed, isTrue, reason: result.message);
    expect(host.related['note'], matches(RegExp(r'^\d{4}-\d{2}-\d{2}$')));
    expect(host.own['note'], 'own note');
  });

  test('Copy reads the related field', () async {
    final (_, session) = await _run(_Host(), 'Copy', {'field': 'Lines::note'});
    expect(session.clipboard, 'related note');
  });

  test("a field of the window's own occurrence is the record in hand's", () async {
    final host = _Host();
    final (result, _) = await _run(host, 'Set Field', {'field': 'Items::note', 'value': '"own"', 'value_is_calculation': true});
    expect(result.completed, isTrue, reason: result.message);
    expect(host.own['note'], 'own');
    expect(host.relatedWrites, isEmpty);
  });

  test("the window's error goes into Get(LastError), and nothing is written", () async {
    for (final code in [101, 102, 103, 201]) {
      final host = _Host()..relatedCode = code;
      final (captured, session) =
          await _run(host, 'Set Field', {'field': 'Lines::note', 'value': '"x"', 'value_is_calculation': true}, capture: true);
      expect(captured.completed, isTrue);
      expect(session.lastError, code);
      final (stopped, _) = await _run(host, 'Set Field', {'field': 'Lines::note', 'value': '"x"', 'value_is_calculation': true});
      expect(stopped.completed, isFalse);
      expect(stopped.message, contains('Nothing was written'));
      expect(host.own['note'], 'own note');
      expect(host.related['note'], 'related note');
    }
  });

  test('a window that cannot write a related field writes nothing, not the field of the same name', () async {
    for (final step in ['Set Field', 'Paste', 'Clear', 'Insert Calculated Result']) {
      for (final form in forms) {
        final host = _PlainHost();
        final (result, session) =
            await _run(host, step, {...form, 'value': '"x"', 'value_is_calculation': true}, capture: true);
        expect(result.completed, isTrue);
        expect(session.lastError, 102, reason: '$step $form');
        expect(host.own['note'], 'own note', reason: '$step $form');
      }
    }
  });

  test('Set Next Serial Value and Relookup Field Contents get the occurrence of field_table', () async {
    final host = _Host();
    await _run(host, 'Set Next Serial Value', {'field': 'ref', 'field_table': 'Lines', 'value': '7'});
    await _run(host, 'Relookup Field Contents', {'field': 'code', 'field_table': 'Lines'});
    expect(host.autoEnter, ['serial Lines::ref', 'relookup Lines::code']);
  });

  test('a custom dialog input on a related field shows and commits the related field', () async {
    final host = _DialogHost();
    final (result, _) = await _run(host, 'Show Custom Dialog', {
      'title': 'T',
      'message': 'M',
      'buttons': [
        {'label': 'OK', 'commit': true},
      ],
      'input_fields': [
        {'field': 'note', 'field_table': 'Lines', 'label': 'Related'},
        {'field': 'note', 'label': 'Own'},
      ],
    });
    expect(result.completed, isTrue, reason: result.message);
    expect(host.shownInputs?.map((i) => '${i['field']}=${i['value']}'), ['Lines::note=related note', 'note=own note']);
    expect(host.related['note'], 'typed Lines::note');
    expect(host.own['note'], 'typed note', reason: 'each input goes back into its own field');
  });

  test('Insert File into a related field writes nothing', () async {
    final host = _Host();
    final (result, session) = await _run(host, 'Insert File', {'field': 'note', 'field_table': 'Lines'}, capture: true);
    expect(result.completed, isTrue);
    expect(session.lastError, 102);
    expect(host.related['note'], 'related note');
    expect(host.own['note'], 'own note');
  });
}
