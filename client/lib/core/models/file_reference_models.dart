/// File references (#107): the named links from this solution's scripts to
/// other solutions. A `Perform Script` step that names one calls a script of
/// that solution, and never a script of this one that happens to share the
/// name.
library;

/// Where a file reference goes, and from where.
class FileReferenceResolution {
  const FileReferenceResolution({
    required this.name,
    required this.database,
    required this.callingDatabase,
    this.local = false,
  });

  factory FileReferenceResolution.fromJson(Map<String, dynamic> json) => FileReferenceResolution(
        name: json['name'] as String? ?? '',
        database: json['database'] as String? ?? '',
        callingDatabase: json['calling_database'] as String? ?? '',
        local: json['local'] as bool? ?? false,
      );

  final String name;

  /// The solution the reference is mapped to.
  final String database;

  /// The solution whose script is reaching out: the one this session is
  /// signed in to. A script performed in [database] that reaches back is
  /// reaching this solution.
  final String callingDatabase;

  /// Whether the reference names the calling solution itself.
  final bool local;
}

/// A file reference of this solution (#107): its name, as FileMaker's
/// scripts and external table occurrences use it, the solution it is mapped
/// to, and FileMaker's paths to the file, which only suggest that solution.
class FileReferenceModel {
  const FileReferenceModel({
    required this.id,
    required this.name,
    this.database = '',
    this.paths = const [],
  });

  factory FileReferenceModel.fromJson(Map<String, dynamic> json) => FileReferenceModel(
        id: json['id'] as String? ?? '',
        name: json['name'] as String? ?? '',
        database: json['database'] as String? ?? '',
        paths: [for (final p in json['paths'] as List<dynamic>? ?? const []) p.toString()],
      );

  final String id;
  final String name;

  /// The solution's database, or empty while the reference is not mapped.
  final String database;
  final List<String> paths;

  bool get isMapped => database.trim().isNotEmpty;
}

/// One solution a sign-in reached for the solution it signed in to (#107):
/// one its external occurrences, file references or transfers read.
class LinkedSolutionModel {
  const LinkedSolutionModel({
    required this.database,
    this.references = const [],
    this.status = linked,
    this.username = '',
    this.token,
    this.expiresAt,
  });

  factory LinkedSolutionModel.fromJson(Map<String, dynamic> json) => LinkedSolutionModel(
        database: json['database'] as String? ?? '',
        references: [for (final r in json['references'] as List<dynamic>? ?? const []) r.toString()],
        status: json['status'] as String? ?? linked,
        username: json['username'] as String? ?? '',
        token: json['token'] as String?,
        expiresAt: DateTime.tryParse(json['expires_at'] as String? ?? ''),
      );

  /// The statuses the server gives.
  static const linked = 'linked';
  static const refused = 'refused';
  static const mfaRequired = 'mfa_required';
  static const missing = 'missing';
  static const throttled = 'throttled';

  final String database;

  /// The file references of the solution signed in to that name [database].
  final List<String> references;
  final String status;
  final String username;

  /// The bearer token of the session opened there. Kept in memory only.
  final String? token;
  final DateTime? expiresAt;

  bool get isLinked => status == linked && (token?.isNotEmpty ?? false);

  /// Whether signing in by hand could still link it: the same account was
  /// refused, needs a code, or was throttled. A missing solution cannot be.
  bool get canSignIn => status == refused || status == mfaRequired || status == throttled;

  /// Why it is not linked, in a few words.
  String get reason {
    switch (status) {
      case refused:
        return 'your account and password were not accepted there';
      case mfaRequired:
        return 'it asks for a verification code';
      case throttled:
        return 'too many attempts; try again in a moment';
      case missing:
        return 'it is not on this server';
      default:
        return 'signed in';
    }
  }
}

/// The verification of one external occurrence through a file reference.
class ExternalOccurrenceStatus {
  const ExternalOccurrenceStatus({
    required this.id,
    required this.name,
    this.table = '',
    this.status = unbound,
    this.fields = const [],
    this.resolved = const {},
    this.candidates = const [],
    this.problems = const [],
  });

  factory ExternalOccurrenceStatus.fromJson(Map<String, dynamic> json) => ExternalOccurrenceStatus(
        id: json['id'] as String? ?? '',
        name: json['name'] as String? ?? '',
        table: json['table'] as String? ?? '',
        status: json['status'] as String? ?? unbound,
        fields: [for (final f in json['fields'] as List<dynamic>? ?? const []) f.toString()],
        resolved: {
          for (final e in (json['resolved'] as Map<String, dynamic>? ?? const {}).entries) e.key: e.value.toString(),
        },
        candidates: [for (final c in json['candidates'] as List<dynamic>? ?? const []) c.toString()],
        problems: [for (final p in json['problems'] as List<dynamic>? ?? const []) p.toString()],
      );

  static const unbound = 'unbound';
  static const bound = 'bound';
  static const mismatch = 'mismatch';

  final String id;
  final String name;
  final String table;
  final String status;
  final List<String> fields;
  final Map<String, String> resolved;

  /// The tables of the other solution that hold every field used through
  /// the occurrence, when its table is not known.
  final List<String> candidates;

  /// What does not match, in sentences.
  final List<String> problems;
}

/// What binding or verifying a file reference found.
class ExternalBindingReport {
  const ExternalBindingReport({
    required this.fileReferenceId,
    required this.fileReference,
    this.database = '',
    this.occurrences = const [],
  });

  factory ExternalBindingReport.fromJson(Map<String, dynamic> json) => ExternalBindingReport(
        fileReferenceId: json['file_reference_id'] as String? ?? '',
        fileReference: json['file_reference'] as String? ?? '',
        database: json['database'] as String? ?? '',
        occurrences: [
          for (final o in json['occurrences'] as List<dynamic>? ?? const [])
            ExternalOccurrenceStatus.fromJson(o as Map<String, dynamic>),
        ],
      );

  final String fileReferenceId;
  final String fileReference;
  final String database;
  final List<ExternalOccurrenceStatus> occurrences;

  bool get allBound => occurrences.every((o) => o.status == ExternalOccurrenceStatus.bound);
}
