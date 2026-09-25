import 'dart:convert';
import 'package:flutter/material.dart';

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

  String get displayName {
    switch (stepType) {
      case 'go_to_layout':
        return 'Ir a Presentación';
      case 'go_to_record':
        return 'Ir a Registro';
      case 'enter_find_mode':
        return 'Entrar en Modo Buscar';
      case 'go_to_field':
        return 'Ir al Campo';
      case 'new_record':
        return 'Nuevo Registro';
      case 'commit_records':
        return 'Guardar Registros (Commit)';
      case 'delete_record':
        return 'Eliminar Registro Actual';
      case 'revert_record':
        return 'Revertir Registro';
      case 'duplicate_record':
        return 'Duplicar Registro';
      case 'if':
        return 'Si';
      case 'else':
        return 'Sino';
      case 'end_if':
        return 'Fin Si';
      case 'loop':
        return 'Bucle (Loop)';
      case 'exit_loop_if':
        return 'Salir del Bucle si';
      case 'end_loop':
        return 'Fin de Bucle';
      case 'set_variable':
        return 'Establecer Variable';
      case 'set_field':
        return 'Establecer Campo';
      case 'clear_field':
        return 'Limpiar Campo';
      case 'perform_script':
        return 'Ejecutar Script';
      case 'pause_script':
        return 'Pausar / Continuar Script';
      case 'halt_script':
        return 'Detener Script';
      case 'perform_rest_api':
        return 'Ejecutar API REST (cURL)';
      case 'insert_from_url':
        return 'Insertar desde URL';
      case 'send_webhook':
        return 'Enviar Notificación Webhook';
      case 'show_dialog':
        return 'Mostrar Cuadro de Diálogo';
      default:
        return stepType;
    }
  }

  String get previewText {
    switch (stepType) {
      case 'go_to_layout':
        return '[${params['layout_name'] ?? 'Presentación original'}]';
      case 'go_to_record':
        return '[${params['target'] ?? 'Siguiente'}]';
      case 'enter_find_mode':
        return '[Pausar: ${params['pause'] == true ? 'Activado' : 'Desactivado'}]';
      case 'set_variable':
        final vName = params['variable']?.toString() ?? r'$var';
        final cExpr = params['calc']?.toString() ?? '';
        return '[$vName = $cExpr]';
      case 'set_field':
        return '[${params['field'] ?? 'Campo'}; ${params['value'] ?? ''}]';
      case 'if':
        return '[${params['condition'] ?? 'Condición'}]';
      case 'exit_loop_if':
        return '[${params['condition'] ?? 'Condición'}]';
      case 'perform_script':
        return '[${params['script_name'] ?? 'Script'}; Parámetro: ${params['param'] ?? ''}]';
      case 'commit_records':
        return '[Con validaciones activas]';
      case 'show_dialog':
        return '["${params['message'] ?? 'Mensaje'}"; Botones: "${params['button_ok'] ?? 'Aceptar'}"]';
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
      label: '+ Ir a Presentación',
      category: 'NAVEGACIÓN',
      description: 'Cambia a una presentación específica de la base de datos.',
      defaultParams: {'layout_name': ''},
    ),
    ScriptCatalogItem(
      stepType: 'go_to_record',
      label: '+ Ir a Registro [ Siguiente | ID ]',
      category: 'NAVEGACIÓN',
      description: 'Navega al primer, último, siguiente o registro específico.',
      defaultParams: {'target': 'Siguiente'},
    ),
    ScriptCatalogItem(
      stepType: 'enter_find_mode',
      label: '+ Entrar en Modo Buscar',
      category: 'NAVEGACIÓN',
      description: 'Pasa a modo de búsqueda para especificar criterios.',
      defaultParams: {'pause': false},
    ),
    ScriptCatalogItem(
      stepType: 'go_to_field',
      label: '+ Ir al Campo',
      category: 'NAVEGACIÓN',
      description: 'Sitúa el foco del cursor en un campo específico.',
      defaultParams: {'field': ''},
    ),

    // Records
    ScriptCatalogItem(
      stepType: 'new_record',
      label: '+ Nuevo Registro',
      category: 'REGISTROS',
      description: 'Crea un nuevo registro en la tabla actual.',
    ),
    ScriptCatalogItem(
      stepType: 'commit_records',
      label: '+ Guardar Registros (Commit)',
      category: 'REGISTROS',
      description: 'Confirma los cambios pendientes en el servidor.',
      defaultParams: {'validate': true},
    ),
    ScriptCatalogItem(
      stepType: 'delete_record',
      label: '+ Eliminar Registro Actual',
      category: 'REGISTROS',
      description: 'Borra de forma permanente el registro activo.',
      defaultParams: {'dialog': true},
    ),
    ScriptCatalogItem(
      stepType: 'revert_record',
      label: '+ Revertir Registro',
      category: 'REGISTROS',
      description: 'Descarta las modificaciones no guardadas en el registro.',
    ),

    // Control & Logic
    ScriptCatalogItem(
      stepType: 'if',
      label: '+ Si / Sino / Fin Si',
      category: 'CONTROL Y LÓGICA',
      description: 'Ejecuta pasos condicionalmente si el cálculo es verdadero.',
      defaultParams: {'condition': ''},
    ),
    ScriptCatalogItem(
      stepType: 'loop',
      label: '+ Bucle (Loop) / Salir de Bucle si',
      category: 'CONTROL Y LÓGICA',
      description: 'Repite un conjunto de pasos hasta que se cumpla una condición.',
    ),
    ScriptCatalogItem(
      stepType: 'set_variable',
      label: '+ Establecer Variable',
      category: 'CONTROL Y LÓGICA',
      description: r'Asigna un cálculo o valor a una variable local ($) o global ($$).',
      defaultParams: {'variable': r'$var', 'calc': ''},
    ),
    ScriptCatalogItem(
      stepType: 'set_field',
      label: '+ Establecer Campo',
      category: 'CONTROL Y LÓGICA',
      description: 'Asigna el resultado de un cálculo al campo especificado.',
      defaultParams: {'field': '', 'value': ''},
    ),
    ScriptCatalogItem(
      stepType: 'perform_script',
      label: '+ Ejecutar Script',
      category: 'CONTROL Y LÓGICA',
      description: 'Llama a otro script pasando un parámetro opcional.',
      defaultParams: {'script_name': '', 'param': ''},
    ),
    ScriptCatalogItem(
      stepType: 'pause_script',
      label: '+ Pausar / Continuar Script',
      category: 'CONTROL Y LÓGICA',
      description: 'Detiene temporalmente el script durante N segundos o indefinido.',
      defaultParams: {'duration_seconds': 0},
    ),

    // Integration & Data
    ScriptCatalogItem(
      stepType: 'perform_rest_api',
      label: '+ Ejecutar API REST (cURL)',
      category: 'INTEGRACIÓN Y DATOS',
      description: 'Realiza peticiones HTTP GET, POST, PUT a servicios externos.',
      defaultParams: {'method': 'POST', 'url': 'https://api.example.com'},
    ),
    ScriptCatalogItem(
      stepType: 'insert_from_url',
      label: '+ Insertar desde URL',
      category: 'INTEGRACIÓN Y DATOS',
      description: 'Descarga contenido de una URL y lo almacena en un campo o variable.',
      defaultParams: {'url': 'https://', 'target': r'$response'},
    ),
    ScriptCatalogItem(
      stepType: 'send_webhook',
      label: '+ Enviar Notificación Webhook',
      category: 'INTEGRACIÓN Y DATOS',
      description: 'Emite un payload JSON hacia un webhook registrado.',
      defaultParams: {'endpoint': ''},
    ),
    ScriptCatalogItem(
      stepType: 'show_dialog',
      label: '+ Mostrar Cuadro de Diálogo',
      category: 'INTEGRACIÓN Y DATOS',
      description: 'Muestra una ventana modal de alerta con botones configurables.',
      defaultParams: {'title': 'Aviso', 'message': '', 'button_ok': 'Aceptar'},
    ),
  ];
}
