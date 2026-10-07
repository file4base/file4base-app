// #33 — merge fields inside layout text. A line whose fields are all empty
// collapses, which is what lets a three-line address print as two.

import 'package:flutter_test/flutter_test.dart';
import 'package:file4base_client/features/layout_engine/layout_object_visuals.dart';

const _record = {
  'first_name': 'Marie',
  'last_name': 'Durand',
  'home_address_1': '14 Avenue Foch',
  'home_address_2': '',
  'city': 'Paris',
  'country': 'France',
  'customer_since': '2009-09-18T00:00:00Z',
  'phone': null,
};

void main() {
  group('substitution', () {
    test('a field symbol takes the record value', () {
      expect(
        resolveLayoutMergeText('{{first_name}} {{last_name}}', record: _record),
        'Marie Durand',
      );
    });

    test('field names are matched without regard to case', () {
      expect(resolveLayoutMergeText('{{First_Name}}', record: _record), 'Marie');
    });

    test('a symbol nothing answers is left as it is', () {
      expect(
        resolveLayoutMergeText('{{no_such_field}}', record: _record),
        '{{no_such_field}}',
        reason: 'a mistyped field name must be visible, not silently blank',
      );
    });

    test('the built-in symbols resolve', () {
      final at = DateTime(2026, 10, 7, 14, 30);
      expect(resolveLayoutMergeText('{{CurrentDate}}', now: at), '2026-10-07');
      expect(resolveLayoutMergeText('{{CurrentTime}}', now: at), '14:30');
      expect(resolveLayoutMergeText('{{CurrentUser}}', userName: 'bakery'), 'bakery');
      expect(resolveLayoutMergeText('Page {{PageNumber}}', pageNumber: 3), 'Page 3');
    });

    test('text with no symbols is untouched', () {
      expect(resolveLayoutMergeText('Dear customer,', record: _record), 'Dear customer,');
    });

    test('a value is written the way the field is shown', () {
      // A DATE comes back from the API as a full timestamp; a letter must read
      // as a date.
      expect(
        resolveLayoutMergeText(
          'Customer since {{customer_since}}',
          record: _record,
          formatField: (field, value) {
            final text = value?.toString() ?? '';
            return text.length > 10 && text[10] == 'T' ? text.substring(0, 10) : text;
          },
        ),
        'Customer since 2009-09-18',
      );
    });
  });

  group('collapsing a line whose fields are empty', () {
    const address = '{{first_name}} {{last_name}}\n'
        '{{home_address_1}}\n'
        '{{home_address_2}}\n'
        '{{city}}, {{country}}';

    test('the empty line goes', () {
      expect(
        resolveLayoutMergeText(address, record: _record, collapseEmptyLines: true),
        'Marie Durand\n14 Avenue Foch\nParis, France',
      );
    });

    test('without collapsing, the blank line stays', () {
      expect(
        resolveLayoutMergeText(address, record: _record),
        'Marie Durand\n14 Avenue Foch\n\nParis, France',
      );
    });

    test('a line left holding only punctuation goes too', () {
      expect(
        resolveLayoutMergeText('{{city}}, {{country}}',
            record: {'city': '', 'country': ''}, collapseEmptyLines: true),
        '',
        reason: 'a lone ", " is not an address line',
      );
    });

    test('a null field counts as empty', () {
      expect(
        resolveLayoutMergeText('{{phone}}', record: _record, collapseEmptyLines: true),
        '',
      );
    });

    test('a line carrying words of its own is kept', () {
      expect(
        resolveLayoutMergeText('Phone: {{phone}}', record: _record, collapseEmptyLines: true),
        'Phone: ',
        reason: 'the word Phone was meant to be there',
      );
    });

    test('a line with no merge fields at all is kept, even when blank', () {
      expect(
        resolveLayoutMergeText('{{city}}\n\n{{country}}',
            record: _record, collapseEmptyLines: true),
        'Paris\n\nFrance',
        reason: 'a blank line the author typed is spacing, not a collapsed field',
      );
    });

    test('a line keeps its value when only some fields are empty', () {
      expect(
        resolveLayoutMergeText('{{home_address_2}} {{city}}',
            record: _record, collapseEmptyLines: true),
        ' Paris',
      );
    });

    test('an unanswered symbol is not treated as empty', () {
      expect(
        resolveLayoutMergeText('{{no_such_field}}', record: _record, collapseEmptyLines: true),
        '{{no_such_field}}',
      );
    });

    test('everything empty leaves nothing', () {
      expect(
        resolveLayoutMergeText('{{home_address_2}}\n{{phone}}',
            record: _record, collapseEmptyLines: true),
        '',
      );
    });
  });
}
