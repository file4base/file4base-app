import 'package:file4base_client/features/layout_engine/layout_object_visuals.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

// What a migrated FileMaker layout carries (part fills, placeholders, hide
// conditions, fields it cannot read) survives the designer and is drawn.
void main() {
  final migrated = <String, dynamic>{
    'name': 'Synthetic',
    'width': 600,
    'default_view': 'list',
    'parts': [
      {'id': 'p1', 'type': 'top_navigation', 'height': 40, 'fill_color': '#112233'},
      {'id': 'p2', 'type': 'body', 'height': 100},
      {'id': 'p3', 'type': 'footer', 'height': 30, 'fill_color': '#80FFFFFF'},
    ],
    'objects': [
      {
        'id': 'o1',
        'type': 'field',
        'x': 10,
        'y': 50,
        'width': 100,
        'height': 20,
        'hide_when': 'archived = 1',
        'hide_in_find': true,
        'field_binding': {
          'field_name': 'title',
          'control_style': 'drop_down_calendar',
          'placeholder': 'dd/mm/yyyy',
        },
      },
      {
        'id': 'o2',
        'type': 'field',
        'x': 10,
        'y': 80,
        'width': 100,
        'height': 20,
        'field_binding': {
          'field_name': 'quantity',
          'table_occurrence': 'Far Away',
          'unresolved_relationship': true,
        },
      },
      {
        'id': 'o3',
        'type': 'field',
        'x': 10,
        'y': 110,
        'width': 100,
        'height': 20,
        'field_binding': {'field_name': 'gone', 'missing': true},
      },
    ],
  };

  test('a migrated layout round-trips through the model', () {
    final layout = LayoutDefinitionModel.fromJson(migrated);
    final again = LayoutDefinitionModel.fromJson(layout.toJson());
    expect(again.parts.first.fillColor, '#112233');
    expect(again.parts[1].fillColor, isNull);
    final field = again.objects.first;
    expect(field.hideWhen, 'archived = 1');
    expect(field.hideInFind, isTrue);
    expect(field.fieldBinding!.placeholder, 'dd/mm/yyyy');
    expect(field.fieldBinding!.controlStyle, FieldControlStyle.dropDownCalendar);
    expect(again.objects[1].fieldBinding!.unresolvedRelationship, isTrue);
    expect(again.objects[2].fieldBinding!.missing, isTrue);
  });

  test('a drop-down calendar needs no value list', () {
    expect(FieldControlStyle.needsValueList(FieldControlStyle.dropDownCalendar), isFalse);
    expect(FieldControlStyle.needsValueList(FieldControlStyle.popUpMenu), isTrue);
    const binding = FieldBindingModel(fieldName: 'd', controlStyle: FieldControlStyle.dropDownCalendar);
    expect(binding.effectiveControlStyle(hasValueList: false), FieldControlStyle.dropDownCalendar);
  });

  test('choosing another field settles what was unreadable', () {
    final binding = FieldBindingModel.fromJson(const {
      'field_name': 'quantity',
      'table_occurrence': 'Far Away',
      'unresolved_relationship': true,
    });
    expect(binding.copyWith(controlStyle: FieldControlStyle.editBox).unresolvedRelationship, isTrue);
    expect(binding.copyWith(fieldName: 'other').unresolvedRelationship, isFalse);
    expect(binding.copyWith(clearRelationship: true).unresolvedRelationship, isFalse);
  });

  test('a field that cannot be read says why', () {
    final layout = LayoutDefinitionModel.fromJson(migrated);
    expect(layoutFieldUnreadableNotice(layout.objects.first.fieldBinding), isNull);
    expect(layoutFieldUnreadableNotice(layout.objects[1].fieldBinding), contains('Far Away::quantity'));
    expect(layoutFieldUnreadableNotice(layout.objects[2].fieldBinding), contains('missing'));
  });

  test('a condition is true the way FileMaker reads one', () {
    for (final t in ['1', '2', '-1', '0.5', 'true', ' 1 ']) {
      expect(isLayoutConditionTrue(t), isTrue, reason: t);
    }
    for (final t in ['', '0', '0.0', 'false', 'abc']) {
      expect(isLayoutConditionTrue(t), isFalse, reason: t);
    }
  });

  test('a label draws a box only when it has one', () {
    expect(layoutBoxDecoration(const LayoutObjectStyle()), isNull);
    final filled = layoutBoxDecoration(const LayoutObjectStyle(fillColor: '#FF0000', borderWidth: 0));
    expect(filled!.color, const Color(0xFFFF0000));
    expect(filled.border, isNull);
    final lined = layoutBoxDecoration(const LayoutObjectStyle(borderColor: '#000000', borderWidth: 2));
    expect(lined!.border, isNotNull);
  });

  testWidgets('parts with a fill are drawn as bands', (tester) async {
    final layout = LayoutDefinitionModel.fromJson(migrated);
    await tester.pumpWidget(Directionality(
      textDirection: TextDirection.ltr,
      child: SizedBox(width: 600, height: 170, child: LayoutBackgroundView(layout: layout)),
    ));
    final bands = tester.widgetList<ColoredBox>(find.byType(ColoredBox)).map((b) => b.color).toList();
    expect(bands, containsAll([const Color(0xFF112233), const Color(0x80FFFFFF)]));
    final footer = tester.getTopLeft(find.byWidgetPredicate((w) => w is ColoredBox && w.color == const Color(0x80FFFFFF)));
    expect(footer.dy, 140);
  });
}
