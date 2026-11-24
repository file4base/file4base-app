import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter/material.dart';

/// The external occurrence a layout is based on, or null when the layout
/// shows records of one of this solution's tables (#107).
///
/// Such a layout must never be bound to a local table: its records live in
/// another solution, which cannot be read from here yet.
TableOccurrenceModel? externalOccurrenceOfLayout(
  LayoutModel layout,
  List<TableOccurrenceModel> occurrences,
) {
  for (final o in occurrences) {
    if (o.id == layout.tableOccurrenceId) return o.isExternal ? o : null;
  }
  return null;
}

/// The names and ids of the external occurrences among [occurrences],
/// lowercased, as a layout field binding names its occurrence.
Set<String> externalOccurrenceKeys(List<TableOccurrenceModel> occurrences) => {
      for (final o in occurrences)
        if (o.isExternal) ...{o.id.toLowerCase(), o.name.toLowerCase()},
    };

/// What a field object of an external occurrence shows instead of a value, or
/// null when [binding] is a field of this solution (#107).
///
/// Such a field is never bound to a local column that shares its name: its
/// value lives in another solution, which cannot be read from here yet.
String? externalFieldNotice(FieldBindingModel? binding, Set<String> externalOccurrences) {
  final occurrence = binding?.tableOccurrence;
  if (binding == null || occurrence == null || !externalOccurrences.contains(occurrence.toLowerCase())) {
    return null;
  }
  return '$occurrence::${binding.fieldName} is in another solution (not available yet)';
}

/// What a layout on an external occurrence shows in place of records.
class ExternalLayoutPlaceholder extends StatelessWidget {
  const ExternalLayoutPlaceholder({
    required this.layoutName,
    required this.occurrence,
    super.key,
  });

  final String layoutName;
  final TableOccurrenceModel occurrence;

  @override
  Widget build(BuildContext context) {
    final external = occurrence.external!;
    final where = external.database.isEmpty
        ? 'The file reference "${external.fileReference}" is not mapped to a solution yet.'
        : 'Its records live in the solution "${external.database}"'
            '${external.table.isEmpty ? '' : ', table "${external.table}"'}, '
            'through the file reference "${external.fileReference}".';
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 520),
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.link_off, size: 48),
              const SizedBox(height: 16),
              Text(
                'Layout "$layoutName" shows "${occurrence.name}", a table of another solution.',
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 8),
              Text(
                '$where Reading records of another solution is not available yet, '
                'so this layout shows no records and record commands are disabled.',
                textAlign: TextAlign.center,
              ),
            ],
          ),
        ),
      ),
    );
  }
}
