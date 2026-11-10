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
    final keys = matchFields.isEmpty ? '' : ' (update matching on ${matchFields.join(', ')})';
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
