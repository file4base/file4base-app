import 'dart:convert';
import 'package:flutter/material.dart';
import '../theme/theme_model.dart';

class ScriptStepModel {
  final String id;
  final String scriptId;
  final int sequenceIdx;
  final String stepType;
  final Map<String, dynamic> params;
  final bool isEnabled;
  final String? parentStepId;

  const ScriptStepModel({
    required this.id,
    this.scriptId = '',
    required this.sequenceIdx,
    required this.stepType,
    this.params = const {},
    this.isEnabled = true,
    this.parentStepId,
  });

  ScriptStepModel copyWith({
    String? id,
    String? scriptId,
    int? sequenceIdx,
    String? stepType,
    Map<String, dynamic>? params,
    bool? isEnabled,
    String? parentStepId,
  }) {
    return ScriptStepModel(
      id: id ?? this.id,
      scriptId: scriptId ?? this.scriptId,
      sequenceIdx: sequenceIdx ?? this.sequenceIdx,
      stepType: stepType ?? this.stepType,
      params: params ?? this.params,
      isEnabled: isEnabled ?? this.isEnabled,
      parentStepId: parentStepId ?? this.parentStepId,
    );
  }

  factory ScriptStepModel.fromJson(Map<String, dynamic> json) {
    Map<String, dynamic> parsedParams = {};
    final rawParams = json['params'];
    if (rawParams is Map) {
      parsedParams = Map<String, dynamic>.from(rawParams);
    } else if (rawParams is String && rawParams.isNotEmpty) {
      try {
        final decoded = jsonDecode(rawParams);
        if (decoded is Map) {
          parsedParams = Map<String, dynamic>.from(decoded);
        }
      } catch (_) {}
    }

    return ScriptStepModel(
      id: json['id'] as String? ?? '',
      scriptId: json['script_id'] as String? ?? '',
      sequenceIdx: json['sequence_idx'] as int? ?? 1,
      stepType: json['step_type'] as String? ?? 'comment',
      params: parsedParams,
      isEnabled: json['is_enabled'] as bool? ?? true,
      parentStepId: json['parent_step_id'] as String?,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'script_id': scriptId,
        'sequence_idx': sequenceIdx,
        'step_type': stepType,
        'params': params,
        'is_enabled': isEnabled,
        if (parentStepId != null) 'parent_step_id': parentStepId,
      };

  String get category {
    switch (stepType) {
      case 'go_to_layout':
      case 'go_to_record':
      case 'enter_find_mode':
      case 'go_to_field':
        return 'navigation';
      case 'new_record':
      case 'commit_records':
      case 'delete_record':
      case 'revert_record':
      case 'duplicate_record':
        return 'records';
      case 'if':
      case 'else':
      case 'end_if':
      case 'loop':
      case 'exit_loop_if':
      case 'end_loop':
      case 'perform_script':
      case 'pause_script':
      case 'halt_script':
        return 'control';
      case 'set_variable':
      case 'set_field':
      case 'clear_field':
        return 'fields';
      case 'perform_rest_api':
      case 'insert_from_url':
      case 'send_webhook':
      case 'show_dialog':
        return 'integration';
      default:
        return 'other';
    }
  }

  Color get categoryColor {
    switch (category) {
      case 'navigation':
        return const Color(0xFFA855F7); // Violet
      case 'fields':
        return const Color(0xFF38BDF8); // Cyan
      case 'control':
        return const Color(0xFFF59E0B); // Amber
      case 'records':
        return const Color(0xFF10B981); // Emerald
      case 'integration':
        return const Color(0xFFF43F5E); // Rose
      default:
        return Colors.blueGrey;
    }
  }

  Color getCategoryColor([AppThemeDefinition? theme]) {
    if (theme == null) return categoryColor;
    switch (category) {
      case 'navigation':
        return theme.navColor;
      case 'fields':
        return theme.fieldsColor;
      case 'control':
        return theme.controlColor;
      case 'records':
        return theme.recordsColor;
      case 'integration':
        return theme.integrationColor;
      default:
        return theme.textSecondary;
    }
  }

  String get displayName {
    switch (stepType) {
      case 'go_to_layout':
        return 'Go to Layout';
      case 'go_to_record':
        return 'Go to Record';
      case 'enter_find_mode':
        return 'Enter Find Mode';
      case 'go_to_field':
        return 'Go to Field';
      case 'new_record':
        return 'New Record';
      case 'commit_records':
        return 'Commit Records';
      case 'delete_record':
        return 'Delete Current Record';
      case 'revert_record':
        return 'Revert Record';
      case 'duplicate_record':
        return 'Duplicate Record';
      case 'if':
        return 'If';
      case 'else':
        return 'Else';
      case 'end_if':
        return 'End If';
      case 'loop':
        return 'Loop';
      case 'exit_loop_if':
        return 'Exit Loop If';
      case 'end_loop':
        return 'End Loop';
      case 'set_variable':
        return 'Set Variable';
      case 'set_field':
        return 'Set Field';
      case 'clear_field':
        return 'Clear Field';
      case 'perform_script':
        return 'Perform Script';
      case 'pause_script':
        return 'Pause / Resume Script';
      case 'halt_script':
        return 'Halt Script';
      case 'perform_rest_api':
        return 'Perform REST API (cURL)';
      case 'insert_from_url':
        return 'Insert from URL';
      case 'send_webhook':
        return 'Send Webhook Notification';
      case 'show_dialog':
        return 'Show Custom Dialog';
      default:
        return stepType;
    }
  }

  String get previewText {
    switch (stepType) {
      case 'go_to_layout':
        return '[${params['layout_name'] ?? 'Original layout'}]';
      case 'go_to_record':
        return '[${params['target'] ?? 'Next'}]';
      case 'enter_find_mode':
        return '[Pause: ${params['pause'] == true ? 'On' : 'Off'}]';
      case 'set_variable':
        final vName = params['variable']?.toString() ?? r'$var';
        final cExpr = params['calc']?.toString() ?? '';
        return '[$vName = $cExpr]';
      case 'set_field':
        return '[${params['field'] ?? 'Field'}; ${params['value'] ?? ''}]';
      case 'if':
        return '[${params['condition'] ?? 'Condition'}]';
      case 'exit_loop_if':
        return '[${params['condition'] ?? 'Condition'}]';
      case 'perform_script':
        return '[${params['script_name'] ?? 'Script'}; Parameter: ${params['param'] ?? ''}]';
      case 'commit_records':
        return '[With validation enabled]';
      case 'show_dialog':
        return '["${params['message'] ?? 'Message'}"; Buttons: "${params['button_ok'] ?? 'OK'}"]';
      case 'perform_rest_api':
        return '[${params['method'] ?? 'POST'} ${params['url'] ?? 'https://...'}]';
      case 'insert_from_url':
        return '[URL: ${params['url'] ?? 'https://...'}]';
      case 'send_webhook':
        return '[Endpoint: ${params['endpoint'] ?? ''}]';
      default:
        return '';
    }
  }
}

class ScriptModel {
  final String id;
  final String name;
  final String contextTable;
  final String? folderId;
  final bool isActive;
  final List<ScriptStepModel> steps;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  const ScriptModel({
    required this.id,
    required this.name,
    this.contextTable = '',
    this.folderId,
    this.isActive = true,
    this.steps = const [],
    this.createdAt,
    this.updatedAt,
  });

  ScriptModel copyWith({
    String? id,
    String? name,
    String? contextTable,
    String? folderId,
    bool? isActive,
    List<ScriptStepModel>? steps,
    DateTime? createdAt,
    DateTime? updatedAt,
  }) {
    return ScriptModel(
      id: id ?? this.id,
      name: name ?? this.name,
      contextTable: contextTable ?? this.contextTable,
      folderId: folderId ?? this.folderId,
      isActive: isActive ?? this.isActive,
      steps: steps ?? this.steps,
      createdAt: createdAt ?? this.createdAt,
      updatedAt: updatedAt ?? this.updatedAt,
    );
  }

  factory ScriptModel.fromJson(Map<String, dynamic> json) {
    List<ScriptStepModel> stepList = [];
    final rawSteps = json['steps'];
    if (rawSteps is List) {
      stepList = rawSteps
          .map((s) => ScriptStepModel.fromJson(Map<String, dynamic>.from(s as Map)))
          .toList();
    }

    return ScriptModel(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      contextTable: json['context_table'] as String? ?? '',
      folderId: json['folder_id'] as String?,
      isActive: json['is_active'] as bool? ?? true,
      steps: stepList,
      createdAt: json['created_at'] != null ? DateTime.tryParse(json['created_at'].toString()) : null,
      updatedAt: json['updated_at'] != null ? DateTime.tryParse(json['updated_at'].toString()) : null,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'context_table': contextTable,
        if (folderId != null) 'folder_id': folderId,
        'is_active': isActive,
        'steps': steps.map((s) => s.toJson()).toList(),
        if (createdAt != null) 'created_at': createdAt!.toIso8601String(),
        if (updatedAt != null) 'updated_at': updatedAt!.toIso8601String(),
      };
}

class ScriptCatalogItem {
  final String stepType;
  final String label;
  final String category;
  final String description;
  final Map<String, dynamic> defaultParams;

  const ScriptCatalogItem({
    required this.stepType,
    required this.label,
    required this.category,
    required this.description,
    this.defaultParams = const {},
  });

  static const List<ScriptCatalogItem> catalog = [
    // Navigation
    ScriptCatalogItem(
      stepType: 'go_to_layout',
      label: '+ Go to Layout',
      category: 'NAVIGATION',
      description: 'Switches to a specific database layout.',
      defaultParams: {'layout_name': ''},
    ),
    ScriptCatalogItem(
      stepType: 'go_to_record',
      label: '+ Go to Record [ Next | ID ]',
      category: 'NAVIGATION',
      description: 'Navigates to the first, last, next, or specified record.',
      defaultParams: {'target': 'Next'},
    ),
    ScriptCatalogItem(
      stepType: 'enter_find_mode',
      label: '+ Enter Find Mode',
      category: 'NAVIGATION',
      description: 'Enters Find mode to specify search criteria.',
      defaultParams: {'pause': false},
    ),
    ScriptCatalogItem(
      stepType: 'go_to_field',
      label: '+ Go to Field',
      category: 'NAVIGATION',
      description: 'Moves keyboard focus to a specific field.',
      defaultParams: {'field': ''},
    ),

    // Records
    ScriptCatalogItem(
      stepType: 'new_record',
      label: '+ New Record',
      category: 'RECORDS',
      description: 'Creates a new record in the current table.',
    ),
    ScriptCatalogItem(
      stepType: 'commit_records',
      label: '+ Commit Records',
      category: 'RECORDS',
      description: 'Commits pending changes to the server.',
      defaultParams: {'validate': true},
    ),
    ScriptCatalogItem(
      stepType: 'delete_record',
      label: '+ Delete Current Record',
      category: 'RECORDS',
      description: 'Permanently deletes the current record.',
      defaultParams: {'dialog': true},
    ),
    ScriptCatalogItem(
      stepType: 'revert_record',
      label: '+ Revert Record',
      category: 'RECORDS',
      description: 'Discards unsaved changes to the record.',
    ),

    // Control & Logic
    ScriptCatalogItem(
      stepType: 'if',
      label: '+ If / Else / End If',
      category: 'CONTROL & LOGIC',
      description: 'Runs steps conditionally when the calculation is true.',
      defaultParams: {'condition': ''},
    ),
    ScriptCatalogItem(
      stepType: 'loop',
      label: '+ Loop / Exit Loop If',
      category: 'CONTROL & LOGIC',
      description: 'Repeats a set of steps until a condition is met.',
    ),
    ScriptCatalogItem(
      stepType: 'set_variable',
      label: '+ Set Variable',
      category: 'CONTROL & LOGIC',
      description: r'Assigns a calculation or value to a local ($) or global ($$) variable.',
      defaultParams: {'variable': r'$var', 'calc': ''},
    ),
    ScriptCatalogItem(
      stepType: 'set_field',
      label: '+ Set Field',
      category: 'CONTROL & LOGIC',
      description: 'Assigns the result of a calculation to the specified field.',
      defaultParams: {'field': '', 'value': ''},
    ),
    ScriptCatalogItem(
      stepType: 'perform_script',
      label: '+ Perform Script',
      category: 'CONTROL & LOGIC',
      description: 'Calls another script with an optional parameter.',
      defaultParams: {'script_name': '', 'param': ''},
    ),
    ScriptCatalogItem(
      stepType: 'pause_script',
      label: '+ Pause / Resume Script',
      category: 'CONTROL & LOGIC',
      description: 'Pauses the script for N seconds or indefinitely.',
      defaultParams: {'duration_seconds': 0},
    ),

    // Integration & Data
    ScriptCatalogItem(
      stepType: 'perform_rest_api',
      label: '+ Perform REST API (cURL)',
      category: 'INTEGRATION & DATA',
      description: 'Sends HTTP GET, POST, or PUT requests to external services.',
      defaultParams: {'method': 'POST', 'url': 'https://api.example.com'},
    ),
    ScriptCatalogItem(
      stepType: 'insert_from_url',
      label: '+ Insert from URL',
      category: 'INTEGRATION & DATA',
      description: 'Downloads content from a URL and stores it in a field or variable.',
      defaultParams: {'url': 'https://', 'target': r'$response'},
    ),
    ScriptCatalogItem(
      stepType: 'send_webhook',
      label: '+ Send Webhook Notification',
      category: 'INTEGRATION & DATA',
      description: 'Sends a JSON payload to a registered webhook.',
      defaultParams: {'endpoint': ''},
    ),
    ScriptCatalogItem(
      stepType: 'show_dialog',
      label: '+ Show Custom Dialog',
      category: 'INTEGRATION & DATA',
      description: 'Displays a modal alert with configurable buttons.',
      defaultParams: {'title': 'Notice', 'message': '', 'button_ok': 'OK'},
    ),
  ];
}
