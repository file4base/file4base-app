/// Transfers between solutions (#96): a named, persisted record transfer and
/// the report one run of it produces.
///
/// A definition holds no credentials and no connection details. Which
/// accounts may run it is decided when it runs, from a session of each end,
/// so nothing in these models is ever a secret.
library;

/// One end of a transfer: a table of a solution.
///
/// An empty [database] means the solution that holds the definition, so a
/// definition carried in a solution bundle keeps working in the copy it is
/// restored into.
class TransferEndModel {
  const TransferEndModel({required this.table, this.database = ''});

  factory TransferEndModel.fromJson(Map<String, dynamic> json) => TransferEndModel(
        table: json['table'] as String? ?? '',
        database: json['database'] as String? ?? '',
      );

  final String database;
  final String table;

  /// Whether this end is the solution holding the definition.
  bool get isLocal => database.trim().isEmpty;

  /// "solution.table", or just the table for the local solution.
  String get label => isLocal ? table : '$database.$table';

  Map<String, dynamic> toJson() => {
        if (database.isNotEmpty) 'database': database,
        'table': table,
      };
}

/// One field of the source sent to one field of the destination.
class TransferMappingModel {
  const TransferMappingModel({required this.sourceField, required this.destinationField});

  factory TransferMappingModel.fromJson(Map<String, dynamic> json) => TransferMappingModel(
        sourceField: json['source_field'] as String? ?? '',
        destinationField: json['destination_field'] as String? ?? '',
      );

  final String sourceField;
  final String destinationField;

  Map<String, dynamic> toJson() => {
        'source_field': sourceField,
        'destination_field': destinationField,
      };
}

/// What the destination does with the records the source sends.
abstract final class TransferMode {
  /// Every source record becomes a new destination record.
  static const add = 'add';

  /// The destination record whose match fields hold the same values is
  /// updated. This is the mode that makes a transfer replayable.
  static const updateMatching = 'update_matching';

  /// FileMaker's "Replace field contents in found set, in order": the n-th
  /// source record replaces the n-th record of the destination found set
  /// (#191). It matches on nothing.
  static const replaceFoundSet = 'replace_found_set';
}

/// What happens to a source record that matches no destination record.
abstract final class TransferUnmatched {
  static const add = 'add';
  static const skip = 'skip';
}

/// What happens to a source value the destination field cannot hold.
abstract final class TransferRejected {
  /// Refuse the whole transfer: nothing is written, and every rejected row
  /// is reported. The default.
  static const abort = 'abort';

  /// Leave the rejected rows out, commit the rest, and quarantine the
  /// rejected rows in the run report.
  static const skip = 'skip';
}

/// A persisted transfer definition.
class TransferDefinitionModel {
  const TransferDefinitionModel({
    required this.id,
    required this.name,
    required this.source,
    required this.destination,
    this.mappings = const [],
    this.matchFields = const [],
    this.mode = TransferMode.updateMatching,
    this.onUnmatched = TransferUnmatched.add,
    this.onRejected = TransferRejected.abort,
    this.dateOrder = 'iso',
    this.hasFoundSet = false,
    this.performAutoEnter = false,
    this.autoEnterFields = const [],
  });

  factory TransferDefinitionModel.fromJson(Map<String, dynamic> json) {
    final foundSet = json['found_set'];
    return TransferDefinitionModel(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      source: TransferEndModel.fromJson(
          (json['source'] as Map<String, dynamic>?) ?? const <String, dynamic>{}),
      destination: TransferEndModel.fromJson(
          (json['destination'] as Map<String, dynamic>?) ?? const <String, dynamic>{}),
      mappings: ((json['mappings'] as List<dynamic>?) ?? const [])
          .map((item) => TransferMappingModel.fromJson(item as Map<String, dynamic>))
          .toList(),
      matchFields: ((json['match_fields'] as List<dynamic>?) ?? const [])
          .map((item) => item.toString())
          .toList(),
      mode: json['mode'] as String? ?? TransferMode.updateMatching,
      onUnmatched: json['on_unmatched'] as String? ?? TransferUnmatched.add,
      onRejected: json['on_rejected'] as String? ?? TransferRejected.abort,
      dateOrder: json['date_order'] as String? ?? 'iso',
      hasFoundSet: foundSet is List && foundSet.isNotEmpty,
      performAutoEnter: json['perform_auto_enter'] as bool? ?? false,
      autoEnterFields: ((json['auto_enter_fields'] as List<dynamic>?) ?? const [])
          .map((item) => item.toString())
          .toList(),
    );
  }

  final String id;
  final String name;
  final TransferEndModel source;
  final TransferEndModel destination;
  final List<TransferMappingModel> mappings;
  final List<String> matchFields;
  final String mode;
  final String onUnmatched;
  final String onRejected;
  final String dateOrder;

  /// Whether the transfer sends a found set rather than the whole table.
  final bool hasFoundSet;

  /// Whether the destination fields' auto-enter options are performed, on
  /// [autoEnterFields] only when it names any.
  final bool performAutoEnter;
  final List<String> autoEnterFields;

  /// Whether a run works on the destination's found set: replacing it in
  /// order, or updating the matching records it holds.
  bool get usesDestinationFoundSet => mode != TransferMode.add;

  /// The other solution this transfer reaches, or an empty string when both
  /// ends are the solution holding the definition.
  ///
  /// It is the solution a run needs a second session token for, and it is why
  /// a transfer can never be run on authority this session does not have.
  String get peerDatabase {
    if (!source.isLocal) return source.database;
    if (!destination.isLocal) return destination.database;
    return '';
  }

  /// One line for a script step or a dialog, e.g.
  /// "Send answers: answers → results_db.project_answers (update matching on code)".
  String describe() {
    final keys = mode == TransferMode.replaceFoundSet
        ? ' (replace the found set, in order)'
        : matchFields.isEmpty
            ? ''
            : ' (update matching on ${matchFields.join(', ')})';
    return '$name: ${source.label} → ${destination.label}$keys';
  }
}

/// One source value the destination field would not hold.
class TransferRejectionModel {
  const TransferRejectionModel({
    required this.row,
    required this.sourceField,
    required this.destinationField,
    required this.value,
    required this.reason,
  });

  factory TransferRejectionModel.fromJson(Map<String, dynamic> json) => TransferRejectionModel(
        row: (json['row'] as num?)?.toInt() ?? 0,
        sourceField: json['source_field'] as String? ?? '',
        destinationField: json['destination_field'] as String? ?? '',
        value: json['value'] as String? ?? '',
        reason: json['reason'] as String? ?? '',
      );

  /// The 1-based position of the record in the source found set.
  final int row;
  final String sourceField;
  final String destinationField;
  final String value;
  final String reason;

  String describe() => 'row $row: $sourceField → $destinationField, "$value" $reason';
}

/// What one run of a transfer did.
class TransferRunReport {
  const TransferRunReport({
    required this.transfer,
    this.sourceDatabase = '',
    this.sourceTable = '',
    this.destinationDatabase = '',
    this.destinationTable = '',
    this.mode = '',
    this.onRejected = '',
    this.rows = 0,
    this.added = 0,
    this.updated = 0,
    this.skipped = 0,
    this.rejected = const [],
    this.committed = false,
    this.error,
    this.recordIds = const [],
  });

  factory TransferRunReport.fromJson(Map<String, dynamic> json) => TransferRunReport(
        transfer: json['transfer'] as String? ?? '',
        sourceDatabase: json['source_database'] as String? ?? '',
        sourceTable: json['source_table'] as String? ?? '',
        destinationDatabase: json['destination_database'] as String? ?? '',
        destinationTable: json['destination_table'] as String? ?? '',
        mode: json['mode'] as String? ?? '',
        onRejected: json['on_rejected'] as String? ?? '',
        rows: (json['rows'] as num?)?.toInt() ?? 0,
        added: (json['added'] as num?)?.toInt() ?? 0,
        updated: (json['updated'] as num?)?.toInt() ?? 0,
        skipped: (json['skipped'] as num?)?.toInt() ?? 0,
        rejected: ((json['rejected'] as List<dynamic>?) ?? const [])
            .map((item) => TransferRejectionModel.fromJson(item as Map<String, dynamic>))
            .toList(),
        committed: json['committed'] as bool? ?? false,
        error: json['error'] as String?,
        recordIds: ((json['record_ids'] as List<dynamic>?) ?? const [])
            .map((item) => item.toString())
            .toList(),
      );

  final String transfer;
  final String sourceDatabase;
  final String sourceTable;
  final String destinationDatabase;
  final String destinationTable;
  final String mode;
  final String onRejected;

  /// How many records the source found set held.
  final int rows;

  final int added;
  final int updated;
  final int skipped;
  final List<TransferRejectionModel> rejected;

  /// Whether the destination transaction was committed. A refused transfer
  /// leaves the destination exactly as it was, so the same run can be
  /// replayed once the source is corrected.
  final bool committed;

  /// Why the transfer was refused, when it was.
  final String? error;

  /// The destination records the run added or updated, when the run asked
  /// for them: the found set an Import Records step shows once done (#191).
  final List<String> recordIds;

  int get rejectedCount => rejected.length;

  /// "2 added, 1 updated, 0 skipped, 1 rejected of 4 records".
  String describeCounts() =>
      '$added added, $updated updated, $skipped skipped, $rejectedCount rejected of $rows records';

  /// What to tell someone whose transfer was refused: the counts, the reason,
  /// and the first rejected rows, which are the ones they have to fix.
  String describeRefusal() {
    final parts = <String>[
      if (error != null && error!.isNotEmpty) error! else 'the transfer was refused',
      describeCounts(),
    ];
    for (final rejection in rejected.take(5)) {
      parts.add(rejection.describe());
    }
    if (rejected.length > 5) {
      parts.add('and ${rejected.length - 5} more');
    }
    return parts.join('; ');
  }
}

/// A migrated Import Records step that imports from another FileMaker file
/// and waits, disabled, for the transfer the migration review creates (#191).
class ImportStepModel {
  const ImportStepModel({
    required this.stepId,
    required this.scriptId,
    required this.scriptName,
    required this.stepIndex,
    this.sourceFile = '',
    this.targetTable = '',
    this.method = '',
    this.addRemaining = false,
    this.autoEnterFields = const [],
    this.mappings = const [],
    this.suggestedDatabase = '',
    this.suggestedBy = '',
    this.problem = '',
  });

  factory ImportStepModel.fromJson(Map<String, dynamic> json) => ImportStepModel(
        stepId: json['step_id'] as String? ?? '',
        scriptId: json['script_id'] as String? ?? '',
        scriptName: json['script_name'] as String? ?? '',
        stepIndex: (json['step_index'] as num?)?.toInt() ?? 0,
        sourceFile: json['source_file'] as String? ?? '',
        targetTable: json['target_table'] as String? ?? '',
        method: json['method'] as String? ?? '',
        addRemaining: json['add_remaining'] as bool? ?? false,
        autoEnterFields: ((json['auto_enter_fields'] as List<dynamic>?) ?? const [])
            .map((item) => item.toString())
            .toList(),
        mappings: ((json['mappings'] as List<dynamic>?) ?? const [])
            .map((item) => ImportStepMappingModel.fromJson(item as Map<String, dynamic>))
            .toList(),
        suggestedDatabase: json['suggested_database'] as String? ?? '',
        suggestedBy: json['suggested_by'] as String? ?? '',
        problem: json['problem'] as String? ?? '',
      );

  final String stepId;
  final String scriptId;
  final String scriptName;

  /// The step's 1-based place in its script.
  final int stepIndex;

  /// The FileMaker file it read, empty when its path is computed.
  final String sourceFile;
  final String targetTable;

  /// FileMaker's method: `add`, `update` (matching records) or `replace`
  /// (the found set, in order).
  final String method;
  final bool addRemaining;

  /// The fields auto-enter is performed on; `*` is every field.
  final List<String> autoEnterFields;
  final List<ImportStepMappingModel> mappings;

  /// The solution the source file most likely became, and why:
  /// `this_file`, `file_reference` or `database_name`.
  final String suggestedDatabase;
  final String suggestedBy;

  /// Why no transfer can be created for the step as it is.
  final String problem;

  /// "update matching on code, adding the others", for the review.
  String describeMethod() {
    switch (method) {
      case 'update':
        final keys = mappings.where((m) => m.match).map((m) => m.targetField).join(', ');
        return 'update matching records on ${keys.isEmpty ? 'no field' : keys}'
            '${addRemaining ? ', adding the others' : ''}';
      case 'replace':
        return 'replace the found set, in order${addRemaining ? ', adding the remaining records' : ''}';
      default:
        return 'add new records';
    }
  }
}

/// One target field a step writes or matches on, fed from a source position.
class ImportStepMappingModel {
  const ImportStepMappingModel({required this.position, required this.targetField, this.match = false});

  factory ImportStepMappingModel.fromJson(Map<String, dynamic> json) => ImportStepMappingModel(
        position: (json['position'] as num?)?.toInt() ?? 0,
        targetField: json['target_field'] as String? ?? '',
        match: json['match'] as bool? ?? false,
      );

  final int position;
  final String targetField;
  final bool match;
}

/// A table of the source solution weighed for a step.
class ImportSourceCandidate {
  const ImportSourceCandidate({
    required this.table,
    this.fields = 0,
    this.typesMatched = 0,
    this.namesMatched = 0,
    this.positions = 0,
    this.fits = false,
  });

  factory ImportSourceCandidate.fromJson(Map<String, dynamic> json) => ImportSourceCandidate(
        table: json['table'] as String? ?? '',
        fields: (json['fields'] as num?)?.toInt() ?? 0,
        typesMatched: (json['types_matched'] as num?)?.toInt() ?? 0,
        namesMatched: (json['names_matched'] as num?)?.toInt() ?? 0,
        positions: (json['positions'] as num?)?.toInt() ?? 0,
        fits: json['fits'] as bool? ?? false,
      );

  final String table;
  final int fields;
  final int typesMatched;
  final int namesMatched;
  final int positions;

  /// Whether the table's fields fit every position the step reads by type.
  final bool fits;

  /// "3/3 types, 2 names" for the picker.
  String describe() => '$typesMatched/$positions types, $namesMatched names, $fields fields';
}

/// What a source solution says about one step: the inferred table, when one
/// alone fits, and every table weighed.
class ImportStepInference {
  const ImportStepInference({
    required this.stepId,
    this.table = '',
    this.confidence = '',
    this.reason = '',
    this.candidates = const [],
    this.error = '',
  });

  factory ImportStepInference.fromJson(Map<String, dynamic> json) => ImportStepInference(
        stepId: json['step_id'] as String? ?? '',
        table: json['table'] as String? ?? '',
        confidence: json['confidence'] as String? ?? '',
        reason: json['reason'] as String? ?? '',
        candidates: ((json['candidates'] as List<dynamic>?) ?? const [])
            .map((item) => ImportSourceCandidate.fromJson(item as Map<String, dynamic>))
            .toList(),
        error: json['error'] as String? ?? '',
      );

  final String stepId;

  /// The inferred source table, empty when none or several fit.
  final String table;

  /// `high` or `medium` when [table] is set.
  final String confidence;
  final String reason;
  final List<ImportSourceCandidate> candidates;
  final String error;

  bool get isInferred => table.isNotEmpty;
}

/// The inference of several steps against one source solution.
class ImportStepsInferResult {
  const ImportStepsInferResult({this.sourceDatabase = '', this.tables = const [], this.steps = const []});

  factory ImportStepsInferResult.fromJson(Map<String, dynamic> json) => ImportStepsInferResult(
        sourceDatabase: json['source_database'] as String? ?? '',
        tables: ((json['tables'] as List<dynamic>?) ?? const []).map((item) => item.toString()).toList(),
        steps: ((json['steps'] as List<dynamic>?) ?? const [])
            .map((item) => ImportStepInference.fromJson(item as Map<String, dynamic>))
            .toList(),
      );

  final String sourceDatabase;
  final List<String> tables;
  final List<ImportStepInference> steps;
}

/// The source a person confirmed for one step.
class ImportStepChoice {
  const ImportStepChoice({required this.stepId, required this.sourceDatabase, required this.sourceTable});

  final String stepId;

  /// The source solution; empty is this one.
  final String sourceDatabase;
  final String sourceTable;

  Map<String, dynamic> toJson() => {
        'step_id': stepId,
        'source_database': sourceDatabase,
        'source_table': sourceTable,
      };
}

/// What confirming one step did: the transfer it created, or why not.
class ImportStepOutcome {
  const ImportStepOutcome({required this.stepId, this.transfer = '', this.error = ''});

  factory ImportStepOutcome.fromJson(Map<String, dynamic> json) => ImportStepOutcome(
        stepId: json['step_id'] as String? ?? '',
        transfer: json['transfer'] as String? ?? '',
        error: json['error'] as String? ?? '',
      );

  final String stepId;
  final String transfer;
  final String error;

  bool get created => transfer.isNotEmpty && error.isEmpty;
}
