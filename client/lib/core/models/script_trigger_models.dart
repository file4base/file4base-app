/// Script triggers under review (#26): every trigger of a solution's layouts
/// and layout objects, as File > Manage > Script Triggers lists them.
///
/// A trigger imported from FileMaker arrives switched off, so no imported
/// script runs on its own; the developer reviews them here and enables the
/// ones they trust.
library;

/// Why a trigger would not run as stored, and what to do about it.
class ScriptTriggerProblem {
  const ScriptTriggerProblem({required this.code, required this.message, required this.remedy});

  factory ScriptTriggerProblem.fromJson(Map<String, dynamic> json) => ScriptTriggerProblem(
        code: json['code'] as String? ?? '',
        message: json['message'] as String? ?? '',
        remedy: json['remedy'] as String? ?? '',
      );

  static const missingScript = 'missing_script';
  static const unresolvedScript = 'unresolved_script';
  static const unboundParameter = 'unbound_parameter';
  static const findModeOnly = 'find_mode_only';
  static const unsupportedEvent = 'unsupported_event';
  static const objectMissing = 'object_missing';
  static const eventTaken = 'event_taken';

  final String code;
  final String message;
  final String remedy;

  /// A short name for the problem, for a filter or a status column.
  String get label => switch (code) {
        missingScript => 'No script',
        unresolvedScript => 'Script not found',
        unboundParameter => 'Parameter',
        findModeOnly => 'Find mode only',
        unsupportedEvent => 'Event not run',
        objectMissing => 'Object not on layout',
        eventTaken => 'Event already used',
        _ => code,
      };
}

/// One trigger of a layout, or of one of its objects.
class ScriptTriggerModel {
  const ScriptTriggerModel({
    required this.id,
    required this.layoutId,
    required this.layoutName,
    required this.event,
    this.scope = 'layout',
    this.objectId,
    this.objectLabel,
    this.objectType,
    this.scriptId,
    this.scriptName,
    this.parameter,
    this.enabled = false,
    this.quarantinedByMigration = false,
    this.problems = const [],
    this.canEnable = false,
  });

  factory ScriptTriggerModel.fromJson(Map<String, dynamic> json) => ScriptTriggerModel(
        id: json['id'] as String? ?? '',
        layoutId: json['layout_id'] as String? ?? '',
        layoutName: json['layout_name'] as String? ?? '',
        event: json['event'] as String? ?? '',
        scope: json['scope'] as String? ?? 'layout',
        objectId: json['object_id'] as String?,
        objectLabel: json['object_label'] as String?,
        objectType: json['object_type'] as String?,
        scriptId: json['script_id'] as String?,
        scriptName: json['script_name'] as String?,
        parameter: json['parameter'] as String?,
        enabled: json['enabled'] as bool? ?? false,
        quarantinedByMigration: json['quarantined_by_migration'] as bool? ?? false,
        problems: [
          if (json['problems'] is List)
            for (final p in json['problems'] as List)
              if (p is Map) ScriptTriggerProblem.fromJson(Map<String, dynamic>.from(p)),
        ],
        canEnable: json['can_enable'] as bool? ?? false,
      );

  final String id;
  final String layoutId;
  final String layoutName;
  final String event;

  /// `layout` or `object`.
  final String scope;
  final String? objectId;
  final String? objectLabel;
  final String? objectType;
  final String? scriptId;
  final String? scriptName;
  final String? parameter;
  final bool enabled;
  final bool quarantinedByMigration;
  final List<ScriptTriggerProblem> problems;

  /// Off, and nothing keeps it from running once enabled.
  final bool canEnable;

  bool get isObjectTrigger => scope == 'object';
  bool get hasProblem => problems.isNotEmpty;

  /// The status a developer reads: enabled, disabled by the migration,
  /// disabled, or the first problem.
  ScriptTriggerStatus get status {
    if (hasProblem) return ScriptTriggerStatus.problem;
    if (enabled) return ScriptTriggerStatus.enabled;
    return quarantinedByMigration ? ScriptTriggerStatus.disabledByMigration : ScriptTriggerStatus.disabled;
  }

  String get statusLabel => switch (status) {
        ScriptTriggerStatus.enabled => 'Enabled',
        ScriptTriggerStatus.disabledByMigration => 'Disabled by migration',
        ScriptTriggerStatus.disabled => 'Disabled',
        ScriptTriggerStatus.problem => 'Problem: ${problems.first.label}',
      };
}

enum ScriptTriggerStatus { enabled, disabledByMigration, disabled, problem }

/// What a bulk enable or disable changed.
class ScriptTriggerUpdate {
  const ScriptTriggerUpdate({required this.updated, required this.layouts});

  factory ScriptTriggerUpdate.fromJson(Map<String, dynamic> json) => ScriptTriggerUpdate(
        updated: json['updated'] as int? ?? 0,
        layouts: [for (final l in json['layouts'] as List? ?? const []) l.toString()],
      );

  final int updated;
  final List<String> layouts;
}
