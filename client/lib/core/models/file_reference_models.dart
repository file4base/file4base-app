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
