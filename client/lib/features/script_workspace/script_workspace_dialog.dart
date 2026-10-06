import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/api/api_client.dart';
import '../../core/theme/theme_model.dart';
import '../../core/theme/theme_provider.dart';
import 'calculation_builder_dialog.dart';

class ScriptWorkspaceDialog extends ConsumerStatefulWidget {
  final ApiClient apiClient;
  final String? databaseName;

  const ScriptWorkspaceDialog({
    super.key,
    required this.apiClient,
    this.databaseName,
  });

  static Future<void> show(
    BuildContext context,
    ApiClient apiClient, {
    String? databaseName,
  }) {
    return showDialog(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => ScriptWorkspaceDialog(
        apiClient: apiClient,
        databaseName: databaseName,
      ),
    );
  }

  @override
  ConsumerState<ScriptWorkspaceDialog> createState() => _ScriptWorkspaceDialogState();
}

class _ScriptWorkspaceDialogState extends ConsumerState<ScriptWorkspaceDialog> {
  bool _isLoading = true;
  String? _errorMessage;

  List<ScriptModel> _scripts = [];
  List<TableModel> _tables = [];
  List<LayoutModel> _layouts = [];

  // Active open tabs (script IDs)
  final List<String> _openScriptIds = [];
  String? _activeScriptId;

  // Selected step index in active script
  int? _selectedStepIdx;

  // Search filters
  String _scriptFilter = '';
  String _catalogFilter = '';

  // Has unsaved changes
  bool _hasUnsavedChanges = false;

  @override
  void initState() {
    super.initState();
    _loadInitialData();
  }

  Future<void> _loadInitialData() async {
    setState(() {
      _isLoading = true;
      _errorMessage = null;
    });

    try {
      final db = widget.databaseName;
      final results = await Future.wait([
        widget.apiClient.listScripts(database: db).catchError((_) => <ScriptModel>[]),
        widget.apiClient.listTables().catchError((_) => <TableModel>[]),
        widget.apiClient.listLayouts().catchError((_) => <LayoutModel>[]),
      ]);

      var scriptsList = results[0] as List<ScriptModel>;
      final tablesList = results[1] as List<TableModel>;
      final layoutsList = results[2] as List<LayoutModel>;

      // If no scripts exist on server yet, populate canonical demo scripts
      if (scriptsList.isEmpty) {
        scriptsList = _generateDefaultDemoScripts();
      }

      setState(() {
        _scripts = scriptsList;
        _tables = tablesList;
        _layouts = layoutsList;
        _isLoading = false;

        if (_scripts.isNotEmpty) {
          _openScriptIds.add(_scripts.first.id);
          _activeScriptId = _scripts.first.id;
          if (_scripts.first.steps.isNotEmpty) {
            _selectedStepIdx = 0;
          }
        }
      });
    } catch (e) {
      setState(() {
        _errorMessage = e.toString();
        _scripts = _generateDefaultDemoScripts();
        _isLoading = false;
        if (_scripts.isNotEmpty) {
          _openScriptIds.add(_scripts.first.id);
          _activeScriptId = _scripts.first.id;
          if (_scripts.first.steps.isNotEmpty) {
            _selectedStepIdx = 0;
          }
        }
      });
    }
  }

  List<ScriptModel> _generateDefaultDemoScripts() {
    return [
      ScriptModel(
        id: 'script-invoice-001',
        name: 'on_invoice_created',
        contextTable: 'Invoices',
        isActive: true,
        steps: [
          const ScriptStepModel(
            id: 's1',
            sequenceIdx: 1,
            stepType: 'go_to_layout',
            params: {'layout_name': 'Invoices_Detail'},
          ),
          const ScriptStepModel(
            id: 's2',
            sequenceIdx: 2,
            stepType: 'set_variable',
            params: {'variable': r'$subtotal', 'calc': 'Sum(Items.price)'},
          ),
          const ScriptStepModel(
            id: 's3',
            sequenceIdx: 3,
            stepType: 'if',
            params: {'condition': 'Invoices::Total > 5000'},
          ),
          const ScriptStepModel(
            id: 's4',
            sequenceIdx: 4,
            stepType: 'set_field',
            params: {'field': 'Invoices::RequiresApproval', 'value': 'TRUE'},
          ),
          const ScriptStepModel(
            id: 's5',
            sequenceIdx: 5,
            stepType: 'perform_rest_api',
            params: {'method': 'POST', 'url': 'https://api.erp.internal/notifications', 'body': '{"alert": "High value invoice"}'},
          ),
          const ScriptStepModel(
            id: 's6',
            sequenceIdx: 6,
            stepType: 'else',
          ),
          const ScriptStepModel(
            id: 's7',
            sequenceIdx: 7,
            stepType: 'set_field',
            params: {'field': 'Invoices::RequiresApproval', 'value': 'FALSE'},
          ),
          const ScriptStepModel(
            id: 's8',
            sequenceIdx: 8,
            stepType: 'end_if',
          ),
          const ScriptStepModel(
            id: 's9',
            sequenceIdx: 9,
            stepType: 'commit_records',
            params: {'validate': true},
          ),
        ],
      ),
      ScriptModel(
        id: 'script-nav-002',
        name: 'navigate_to_client_record',
        contextTable: 'Customers',
        isActive: true,
        steps: [
          const ScriptStepModel(
            id: 's10',
            sequenceIdx: 1,
            stepType: 'enter_find_mode',
            params: {'pause': false},
          ),
          const ScriptStepModel(
            id: 's11',
            sequenceIdx: 2,
            stepType: 'set_field',
            params: {'field': 'Customers::Status', 'value': '"Active"'},
          ),
          const ScriptStepModel(
            id: 's12',
            sequenceIdx: 3,
            stepType: 'go_to_record',
            params: {'target': 'Next'},
          ),
          const ScriptStepModel(
            id: 's13',
            sequenceIdx: 4,
            stepType: 'show_dialog',
            params: {'title': 'Customer loaded', 'message': 'Customer record is ready to view.', 'button_ok': 'OK'},
          ),
        ],
      ),
      ScriptModel(
        id: 'script-export-003',
        name: 'batch_update_products',
        contextTable: 'Products',
        isActive: false,
        steps: [
          const ScriptStepModel(
            id: 's20',
            sequenceIdx: 1,
            stepType: 'loop',
          ),
          const ScriptStepModel(
            id: 's21',
            sequenceIdx: 2,
            stepType: 'set_variable',
            params: {'variable': r'$counter', 'calc': r'$counter + 1'},
          ),
          const ScriptStepModel(
            id: 's22',
            sequenceIdx: 3,
            stepType: 'exit_loop_if',
            params: {'condition': r'$counter >= 100'},
          ),
          const ScriptStepModel(
            id: 's23',
            sequenceIdx: 4,
            stepType: 'end_loop',
          ),
        ],
      ),
    ];
  }

  ScriptModel? get _activeScript {
    if (_activeScriptId == null) return null;
    final idx = _scripts.indexWhere((s) => s.id == _activeScriptId);
    return idx != -1 ? _scripts[idx] : null;
  }

  ScriptStepModel? get _activeStep {
    final script = _activeScript;
    if (script == null || _selectedStepIdx == null) return null;
    if (_selectedStepIdx! >= 0 && _selectedStepIdx! < script.steps.length) {
      return script.steps[_selectedStepIdx!];
    }
    return null;
  }

  void _openScript(String scriptId) {
    setState(() {
      if (!_openScriptIds.contains(scriptId)) {
        _openScriptIds.add(scriptId);
      }
      _activeScriptId = scriptId;
      final script = _activeScript;
      if (script != null && script.steps.isNotEmpty) {
        _selectedStepIdx = 0;
      } else {
        _selectedStepIdx = null;
      }
    });
  }

  void _closeTab(String scriptId) {
    setState(() {
      final index = _openScriptIds.indexOf(scriptId);
      _openScriptIds.remove(scriptId);
      if (_activeScriptId == scriptId) {
        if (_openScriptIds.isNotEmpty) {
          final nextIdx = (index < _openScriptIds.length) ? index : _openScriptIds.length - 1;
          _activeScriptId = _openScriptIds[nextIdx];
        } else {
          _activeScriptId = null;
          _selectedStepIdx = null;
        }
      }
    });
  }

  void _addNewScript() {
    final newId = 'script-${DateTime.now().millisecondsSinceEpoch}';
    final newScript = ScriptModel(
      id: newId,
      name: 'New_Script_${_scripts.length + 1}',
      contextTable: _tables.isNotEmpty ? _tables.first.name : '',
      isActive: true,
      steps: [
        const ScriptStepModel(
          id: 'step-1',
          sequenceIdx: 1,
          stepType: 'new_record',
        ),
      ],
    );

    setState(() {
      _scripts.add(newScript);
      _openScriptIds.add(newId);
      _activeScriptId = newId;
      _selectedStepIdx = 0;
      _hasUnsavedChanges = true;
    });
  }

  void _duplicateActiveScript() {
    final current = _activeScript;
    if (current == null) return;

    final newId = 'script-${DateTime.now().millisecondsSinceEpoch}';
    final cloned = current.copyWith(
      id: newId,
      name: '${current.name}_Copy',
      steps: current.steps.map((s) => s.copyWith(id: 'step-${DateTime.now().microsecondsSinceEpoch}')).toList(),
    );

    setState(() {
      _scripts.add(cloned);
      _openScriptIds.add(newId);
      _activeScriptId = newId;
      _selectedStepIdx = cloned.steps.isNotEmpty ? 0 : null;
      _hasUnsavedChanges = true;
    });
  }

  void _deleteScript(String scriptId) {
    setState(() {
      _scripts.removeWhere((s) => s.id == scriptId);
      _closeTab(scriptId);
      _hasUnsavedChanges = true;
    });
  }

  void _toggleScriptActive(String scriptId) {
    setState(() {
      final idx = _scripts.indexWhere((s) => s.id == scriptId);
      if (idx != -1) {
        final current = _scripts[idx];
        _scripts[idx] = current.copyWith(isActive: !current.isActive);
        _hasUnsavedChanges = true;
      }
    });
  }

  void _addStepToActiveScript(ScriptCatalogItem item) {
    final script = _activeScript;
    if (script == null) return;

    final newStep = ScriptStepModel(
      id: 'step-${DateTime.now().microsecondsSinceEpoch}',
      sequenceIdx: script.steps.length + 1,
      stepType: item.stepType,
      params: Map<String, dynamic>.from(item.defaultParams),
      isEnabled: true,
    );

    final updatedSteps = List<ScriptStepModel>.from(script.steps);
    int insertIndex = updatedSteps.length;
    if (_selectedStepIdx != null && _selectedStepIdx! >= 0 && _selectedStepIdx! < updatedSteps.length) {
      insertIndex = _selectedStepIdx! + 1;
      updatedSteps.insert(insertIndex, newStep);
    } else {
      updatedSteps.add(newStep);
    }

    _updateActiveScriptSteps(updatedSteps);
    setState(() {
      _selectedStepIdx = insertIndex;
    });
  }

  void _updateActiveScriptSteps(List<ScriptStepModel> updatedSteps) {
    final script = _activeScript;
    if (script == null) return;

    // Normalize sequence indices
    final normalized = <ScriptStepModel>[];
    for (int i = 0; i < updatedSteps.length; i++) {
      normalized.add(updatedSteps[i].copyWith(sequenceIdx: i + 1));
    }

    final scriptIdx = _scripts.indexWhere((s) => s.id == script.id);
    if (scriptIdx != -1) {
      setState(() {
        _scripts[scriptIdx] = script.copyWith(steps: normalized);
        _hasUnsavedChanges = true;
      });
    }
  }

  void _toggleStepEnabled(int index) {
    final script = _activeScript;
    if (script == null || index < 0 || index >= script.steps.length) return;

    final updatedSteps = List<ScriptStepModel>.from(script.steps);
    final current = updatedSteps[index];
    updatedSteps[index] = current.copyWith(isEnabled: !current.isEnabled);
    _updateActiveScriptSteps(updatedSteps);
  }

  void _removeStep(int index) {
    final script = _activeScript;
    if (script == null || index < 0 || index >= script.steps.length) return;

    final updatedSteps = List<ScriptStepModel>.from(script.steps);
    updatedSteps.removeAt(index);
    _updateActiveScriptSteps(updatedSteps);
    setState(() {
      if (updatedSteps.isEmpty) {
        _selectedStepIdx = null;
      } else if (_selectedStepIdx != null && _selectedStepIdx! >= updatedSteps.length) {
        _selectedStepIdx = updatedSteps.length - 1;
      }
    });
  }

  void _moveStep(int oldIndex, int newIndex) {
    final script = _activeScript;
    if (script == null) return;

    final updatedSteps = List<ScriptStepModel>.from(script.steps);
    if (oldIndex < newIndex) {
      newIndex -= 1;
    }
    final item = updatedSteps.removeAt(oldIndex);
    updatedSteps.insert(newIndex, item);
    _updateActiveScriptSteps(updatedSteps);
    setState(() {
      _selectedStepIdx = newIndex;
    });
  }

  void _updateStepParam(String key, dynamic value) {
    final script = _activeScript;
    final step = _activeStep;
    if (script == null || step == null || _selectedStepIdx == null) return;

    final updatedParams = Map<String, dynamic>.from(step.params);
    updatedParams[key] = value;

    final updatedSteps = List<ScriptStepModel>.from(script.steps);
    updatedSteps[_selectedStepIdx!] = step.copyWith(params: updatedParams);
    _updateActiveScriptSteps(updatedSteps);
  }

  Future<void> _openCalculationDialogForParam(String paramKey, {String? defaultContext}) async {
    final step = _activeStep;
    if (step == null) return;

    final initialVal = step.params[paramKey]?.toString() ?? '';
    final tables = _tables.map((t) => t.name).toList();
    final Map<String, List<String>> fieldsMap = {};
    for (final t in _tables) {
      fieldsMap[t.name] = t.columns.map((c) => c.name).toList();
    }

    final formula = await CalculationBuilderDialog.show(
      context,
      initialFormula: initialVal,
      contextTable: defaultContext ?? _activeScript?.contextTable,
      availableTables: tables,
      fieldsByTable: fieldsMap,
    );

    if (formula != null) {
      _updateStepParam(paramKey, formula);
    }
  }

  /// Saves every script. Scripts that only exist locally (new scripts and the
  /// demo scripts, whose ids are not server ids) are created on the server and
  /// then take the id the server assigned, so the next save updates them
  /// instead of creating duplicates. Returns false when a script failed.
  Future<bool> _saveAllChanges() async {
    setState(() => _isLoading = true);
    final db = widget.databaseName;
    final failed = <String>[];

    try {
      for (final script in List<ScriptModel>.from(_scripts)) {
        final payload = {
          'id': script.id,
          'name': script.name,
          'context_table': script.contextTable,
          'folder_id': script.folderId,
          'is_active': script.isActive,
          'steps': script.steps.map((s) => s.toJson()).toList(),
        };

        try {
          await widget.apiClient.updateScript(script.id, payload, database: db);
        } catch (_) {
          // The script does not exist on the server yet: create it and adopt
          // the server id.
          try {
            final created = await widget.apiClient.createScript(payload, database: db);
            _replaceScriptId(script.id, created.id);
          } catch (_) {
            failed.add(script.name);
          }
        }
      }

      setState(() {
        _hasUnsavedChanges = failed.isNotEmpty;
        _isLoading = false;
      });

      if (failed.isNotEmpty) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('Could not save: ${failed.join(', ')}'),
              backgroundColor: Colors.red,
            ),
          );
        }
        return false;
      }

      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('Scripts saved to the database successfully.'),
            backgroundColor: Color(0xFF10B981),
            duration: Duration(seconds: 2),
          ),
        );
      }
    } catch (e) {
      setState(() => _isLoading = false);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Error saving scripts: $e'),
            backgroundColor: Colors.red,
          ),
        );
      }
      return false;
    }
    return true;
  }

  void _replaceScriptId(String oldId, String newId) {
    if (oldId == newId) return;
    _scripts = _scripts
        .map((sc) => sc.id == oldId
            ? sc.copyWith(id: newId, steps: sc.steps.map((st) => st.copyWith(scriptId: newId)).toList())
            : sc)
        .toList();
    final openIdx = _openScriptIds.indexOf(oldId);
    if (openIdx >= 0) _openScriptIds[openIdx] = newId;
    if (_activeScriptId == oldId) _activeScriptId = newId;
  }

  /// Closing with unsaved changes asks whether to save them first, so new
  /// scripts are not lost (they would not be available to layout buttons).
  Future<void> _close() async {
    if (_hasUnsavedChanges) {
      final choice = await showDialog<String>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Unsaved scripts'),
          content: const Text('Save the changes to your scripts before closing?'),
          actions: [
            TextButton(onPressed: () => Navigator.of(ctx).pop('cancel'), child: const Text('Cancel')),
            TextButton(onPressed: () => Navigator.of(ctx).pop('discard'), child: const Text("Don't save")),
            FilledButton(onPressed: () => Navigator.of(ctx).pop('save'), child: const Text('Save')),
          ],
        ),
      );
      if (choice == null || choice == 'cancel') return;
      if (choice == 'save' && !await _saveAllChanges()) return;
    }
    if (mounted) Navigator.of(context).pop();
  }

  void _runActiveScript() {
    final script = _activeScript;
    if (script == null) return;
    final theme = ref.read(appThemeProvider);

    // Show simulated execution results dialog
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: theme.surface,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(12),
          side: BorderSide(color: theme.successColor),
        ),
        title: Row(
          children: [
            Icon(Icons.play_circle_fill, color: theme.successColor, size: 24),
            const SizedBox(width: 10),
            Text('Execution: ${script.name}', style: TextStyle(color: theme.textPrimary, fontSize: 16)),
          ],
        ),
        content: SizedBox(
          width: 500,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: theme.surfaceContainer,
                  borderRadius: BorderRadius.circular(8),
                  border: Border.all(color: theme.border),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('Status: SUCCESS (0 errors)', style: TextStyle(color: theme.successColor, fontWeight: FontWeight.bold, fontSize: 13)),
                    const SizedBox(height: 6),
                    Text('Steps executed: ${script.steps.where((s) => s.isEnabled).length} of ${script.steps.length}', style: TextStyle(color: theme.textSecondary, fontSize: 12)),
                    Text('Table context: ${script.contextTable.isEmpty ? "Default" : script.contextTable}', style: TextStyle(color: theme.textSecondary, fontSize: 12)),
                    Divider(color: theme.border, height: 16),
                    Text('File4Base engine execution trace:', style: TextStyle(color: theme.primaryAccent, fontSize: 11, fontWeight: FontWeight.bold)),
                    const SizedBox(height: 4),
                    ...script.steps.where((s) => s.isEnabled).take(5).map((s) => Padding(
                      padding: const EdgeInsets.symmetric(vertical: 2),
                      child: Text('✔ ${s.sequenceIdx.toString().padLeft(2, "0")} ${s.displayName} ${s.previewText}',
                        style: TextStyle(color: theme.textPrimary, fontSize: 11, fontFamily: 'monospace'),
                      ),
                    )),
                    if (script.steps.where((s) => s.isEnabled).length > 5)
                      Text('... (remaining steps completed)', style: TextStyle(color: theme.textSecondary, fontSize: 10, fontStyle: FontStyle.italic)),
                  ],
                ),
              ),
            ],
          ),
        ),
        actions: [
          ElevatedButton(
            style: ElevatedButton.styleFrom(backgroundColor: theme.successColor, foregroundColor: Colors.white),
            onPressed: () => Navigator.of(ctx).pop(),
            child: const Text('Close'),
          ),
        ],
      ),
    );
  }

  void _debugActiveScript() {
    final script = _activeScript;
    if (script == null) return;
    final theme = ref.read(appThemeProvider);

    int debugStep = 0;
    final activeSteps = script.steps.where((s) => s.isEnabled).toList();
    final Map<String, String> debugVariables = {
      r'$subtotal': '5240.00',
      r'$counter': '1',
      'Get(LastError)': '0',
    };

    showDialog(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDbgState) {
          final currentStep = (debugStep < activeSteps.length) ? activeSteps[debugStep] : null;

          return AlertDialog(
            backgroundColor: theme.surface,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(12),
              side: BorderSide(color: theme.primaryAccent),
            ),
            title: Row(
              children: [
                Icon(Icons.bug_report, color: theme.primaryAccent, size: 24),
                const SizedBox(width: 10),
                Text('Script debugger: ${script.name}', style: TextStyle(color: theme.textPrimary, fontSize: 16)),
              ],
            ),
            content: SizedBox(
              width: 620,
              height: 380,
              child: Column(
                children: [
                  // Step progression
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                    decoration: BoxDecoration(
                      color: theme.surfaceContainer,
                      borderRadius: BorderRadius.circular(8),
                      border: Border.all(color: theme.border),
                    ),
                    child: Row(
                      children: [
                        Text('Step ${debugStep + 1} of ${activeSteps.length}', style: TextStyle(color: theme.textPrimary, fontWeight: FontWeight.bold, fontSize: 12)),
                        const Spacer(),
                        if (currentStep != null)
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                            decoration: BoxDecoration(
                              color: currentStep.getCategoryColor(theme).withValues(alpha: 0.2),
                              borderRadius: BorderRadius.circular(4),
                            ),
                            child: Text(currentStep.displayName, style: TextStyle(color: currentStep.getCategoryColor(theme), fontSize: 11, fontWeight: FontWeight.bold)),
                          ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 10),
                  // Step details
                  Expanded(
                    child: Container(
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: theme.surfaceContainer,
                        borderRadius: BorderRadius.circular(8),
                        border: Border.all(color: theme.border),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text('Current step:', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                          const SizedBox(height: 4),
                          if (currentStep != null)
                            Row(
                              children: [
                                Icon(Icons.arrow_right_alt, color: theme.primaryAccent, size: 20),
                                const SizedBox(width: 6),
                                Expanded(
                                  child: Text(
                                    '${currentStep.displayName} ${currentStep.previewText}',
                                    style: TextStyle(color: theme.textPrimary, fontFamily: 'monospace', fontSize: 13, fontWeight: FontWeight.bold),
                                  ),
                                ),
                              ],
                            )
                          else
                            Text('Script execution finished.', style: TextStyle(color: theme.successColor, fontSize: 14, fontWeight: FontWeight.bold)),
                          Divider(color: theme.border, height: 20),
                          Text('Variables and inspection:', style: TextStyle(color: theme.primaryAccent, fontSize: 11, fontWeight: FontWeight.bold)),
                          const SizedBox(height: 6),
                          ...debugVariables.entries.map((e) => Padding(
                            padding: const EdgeInsets.symmetric(vertical: 2),
                            child: Row(
                              children: [
                                Text('${e.key}: ', style: TextStyle(color: theme.controlColor, fontFamily: 'monospace', fontSize: 12)),
                                Text(e.value, style: TextStyle(color: theme.textPrimary, fontFamily: 'monospace', fontSize: 12)),
                              ],
                            ),
                          )),
                        ],
                      ),
                    ),
                  ),
                ],
              ),
            ),
            actions: [
              OutlinedButton.icon(
                icon: const Icon(Icons.skip_next, size: 18),
                label: const Text('Next step'),
                style: OutlinedButton.styleFrom(
                  foregroundColor: theme.primaryAccent,
                  side: BorderSide(color: theme.primaryAccent),
                ),
                onPressed: currentStep != null ? () {
                  setDbgState(() {
                    debugStep++;
                    if (currentStep.stepType == 'set_variable') {
                      final v = currentStep.params['variable']?.toString() ?? r'$var';
                      final c = currentStep.params['calc']?.toString() ?? '1';
                      debugVariables[v] = c;
                    }
                  });
                } : null,
              ),
              ElevatedButton(
                style: ElevatedButton.styleFrom(backgroundColor: theme.primaryAccent, foregroundColor: theme.isDark ? const Color(0xFF0B1120) : Colors.white),
                onPressed: () => Navigator.of(ctx).pop(),
                child: const Text('Close'),
              ),
            ],
          );
        },
      ),
    );
  }

  // Calculate indentation depth based on if/loop nesting
  int _calculateStepIndent(List<ScriptStepModel> steps, int index) {
    int depth = 0;
    for (int i = 0; i < index; i++) {
      final t = steps[i].stepType;
      if (t == 'if' || t == 'loop') {
        depth++;
      } else if (t == 'end_if' || t == 'end_loop') {
        depth = depth > 0 ? depth - 1 : 0;
      }
    }
    final currentType = steps[index].stepType;
    if ((currentType == 'else' || currentType == 'end_if' || currentType == 'end_loop') && depth > 0) {
      depth--;
    }
    return depth;
  }

  @override
  Widget build(BuildContext context) {
    final theme = ref.watch(appThemeProvider);
    final activeDb = widget.databaseName ?? '';
    final script = _activeScript;
    final activeStep = _activeStep;

    return Dialog(
      backgroundColor: Colors.transparent,
      child: Container(
        width: 1260,
        height: 780,
        decoration: BoxDecoration(
          color: theme.surface,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: theme.border),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: theme.isDark ? 0.6 : 0.2),
              blurRadius: 36,
              offset: const Offset(0, 16),
            ),
          ],
        ),
        child: Column(
          children: [
            // Top Window Bar
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
              decoration: BoxDecoration(
                color: theme.surfaceContainer,
                borderRadius: const BorderRadius.vertical(top: Radius.circular(12)),
                border: Border(bottom: BorderSide(color: theme.border)),
              ),
              child: Row(
                children: [
                  Icon(Icons.code_rounded, color: theme.primaryAccent, size: 22),
                  const SizedBox(width: 10),
                  Text(
                    'Script Workspace',
                    style: TextStyle(color: theme.textPrimary, fontSize: 15, fontWeight: FontWeight.bold),
                  ),
                  const SizedBox(width: 12),
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 3),
                    decoration: BoxDecoration(
                      color: theme.primaryAccent.withValues(alpha: 0.15),
                      borderRadius: BorderRadius.circular(6),
                      border: Border.all(color: theme.primaryAccent.withValues(alpha: 0.3)),
                    ),
                    child: Text(
                      'Database: $activeDb',
                      style: TextStyle(color: theme.primaryAccent, fontSize: 11, fontWeight: FontWeight.w500),
                    ),
                  ),
                  if (_hasUnsavedChanges) ...[
                    const SizedBox(width: 10),
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                      decoration: BoxDecoration(
                        color: theme.warningColor.withValues(alpha: 0.2),
                        borderRadius: BorderRadius.circular(4),
                        border: Border.all(color: theme.warningColor),
                      ),
                      child: Text('Unsaved', style: TextStyle(color: theme.warningColor, fontSize: 10, fontWeight: FontWeight.bold)),
                    ),
                  ],
                  const Spacer(),
                  // Run Button
                  ElevatedButton.icon(
                    icon: const Icon(Icons.play_arrow, size: 16),
                    label: const Text('Run', style: TextStyle(fontSize: 12, fontWeight: FontWeight.bold)),
                    style: ElevatedButton.styleFrom(
                      backgroundColor: theme.successColor,
                      foregroundColor: Colors.white,
                      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                      minimumSize: Size.zero,
                    ),
                    onPressed: script != null ? _runActiveScript : null,
                  ),
                  const SizedBox(width: 8),
                  // Debug Button
                  ElevatedButton.icon(
                    icon: const Icon(Icons.bug_report, size: 16),
                    label: const Text('Debug', style: TextStyle(fontSize: 12, fontWeight: FontWeight.bold)),
                    style: ElevatedButton.styleFrom(
                      backgroundColor: theme.secondaryAccent,
                      foregroundColor: Colors.white,
                      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                      minimumSize: Size.zero,
                    ),
                    onPressed: script != null ? _debugActiveScript : null,
                  ),
                  const SizedBox(width: 8),
                  // Save Button
                  ElevatedButton.icon(
                    icon: const Icon(Icons.save, size: 16),
                    label: const Text('Save', style: TextStyle(fontSize: 12, fontWeight: FontWeight.bold)),
                    style: ElevatedButton.styleFrom(
                      backgroundColor: theme.primaryAccent,
                      foregroundColor: theme.isDark ? const Color(0xFF0B1120) : Colors.white,
                      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                      minimumSize: Size.zero,
                    ),
                    onPressed: _hasUnsavedChanges ? _saveAllChanges : null,
                  ),
                  const SizedBox(width: 12),
                  IconButton(
                    icon: Icon(Icons.close, color: theme.textSecondary, size: 20),
                    onPressed: _close,
                    tooltip: 'Close window',
                    splashRadius: 18,
                  ),
                ],
              ),
            ),

            if (_isLoading)
              LinearProgressIndicator(
                minHeight: 2,
                backgroundColor: theme.surfaceContainer,
                valueColor: AlwaysStoppedAnimation<Color>(theme.primaryAccent),
              ),
            if (_errorMessage != null)
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
                color: theme.errorColor.withValues(alpha: 0.15),
                child: Row(
                  children: [
                    Icon(Icons.warning_amber_rounded, color: theme.errorColor, size: 14),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        'Notice: $_errorMessage',
                        style: TextStyle(color: theme.errorColor, fontSize: 11),
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                  ],
                ),
              ),

            // Main 3-panel split view
            Expanded(
              child: Row(
                children: [
                  // 1. LEFT PANEL: Scripts Explorer
                  Container(
                    width: 250,
                    decoration: BoxDecoration(
                      color: theme.surface,
                      border: Border(right: BorderSide(color: theme.border)),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        // Search bar & toolbar
                        Container(
                          padding: const EdgeInsets.all(10),
                          decoration: BoxDecoration(
                            border: Border(bottom: BorderSide(color: theme.border)),
                          ),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Row(
                                children: [
                                  Text('Scripts', style: TextStyle(color: theme.textPrimary, fontSize: 12, fontWeight: FontWeight.bold)),
                                  const Spacer(),
                                  InkWell(
                                    onTap: _addNewScript,
                                    borderRadius: BorderRadius.circular(4),
                                    child: Container(
                                      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 3),
                                      decoration: BoxDecoration(
                                        color: theme.surfaceContainer,
                                        borderRadius: BorderRadius.circular(4),
                                        border: Border.all(color: theme.border),
                                      ),
                                      child: Row(
                                        mainAxisSize: MainAxisSize.min,
                                        children: [
                                          Icon(Icons.add, size: 13, color: theme.primaryAccent),
                                          const SizedBox(width: 3),
                                          Text('Script', style: TextStyle(color: theme.primaryAccent, fontSize: 10, fontWeight: FontWeight.bold)),
                                        ],
                                      ),
                                    ),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 8),
                              SizedBox(
                                height: 30,
                                child: TextField(
                                  style: TextStyle(color: theme.textPrimary, fontSize: 11),
                                  decoration: InputDecoration(
                                    hintText: 'Search scripts...',
                                    hintStyle: TextStyle(color: theme.textSecondary, fontSize: 11),
                                    prefixIcon: Icon(Icons.search, size: 15, color: theme.textSecondary),
                                    contentPadding: EdgeInsets.zero,
                                    filled: true,
                                    fillColor: theme.surfaceContainer,
                                    border: OutlineInputBorder(borderRadius: BorderRadius.circular(6), borderSide: BorderSide.none),
                                  ),
                                  onChanged: (v) => setState(() => _scriptFilter = v),
                                ),
                              ),
                            ],
                          ),
                        ),

                        // Scripts List
                        Expanded(
                          child: ListView(
                            padding: const EdgeInsets.symmetric(vertical: 4),
                            children: _scripts
                                .where((s) => _scriptFilter.isEmpty || s.name.toLowerCase().contains(_scriptFilter.toLowerCase()))
                                .map((s) {
                              final isSelected = s.id == _activeScriptId;

                              return InkWell(
                                onTap: () => _openScript(s.id),
                                child: Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                                  decoration: BoxDecoration(
                                    color: isSelected ? theme.primaryAccent.withValues(alpha: 0.15) : Colors.transparent,
                                    border: isSelected ? Border(left: BorderSide(color: theme.primaryAccent, width: 3)) : null,
                                  ),
                                  child: Row(
                                    children: [
                                      Icon(
                                        Icons.description_outlined,
                                        size: 16,
                                        color: s.isActive ? theme.primaryAccent : theme.textSecondary,
                                      ),
                                      const SizedBox(width: 8),
                                      Expanded(
                                        child: Text(
                                          s.name,
                                          style: TextStyle(
                                            color: isSelected ? theme.primaryAccent : (s.isActive ? theme.textPrimary : theme.textSecondary),
                                            fontSize: 12,
                                            fontWeight: isSelected ? FontWeight.bold : FontWeight.normal,
                                          ),
                                          overflow: TextOverflow.ellipsis,
                                        ),
                                      ),
                                      // Step count badge
                                      Container(
                                        padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 1),
                                        decoration: BoxDecoration(
                                          color: theme.surfaceContainer,
                                          borderRadius: BorderRadius.circular(4),
                                        ),
                                        child: Text(
                                          '${s.steps.length}',
                                          style: TextStyle(color: theme.textSecondary, fontSize: 10),
                                        ),
                                      ),
                                      // Context menu popup
                                      PopupMenuButton<String>(
                                        padding: EdgeInsets.zero,
                                        icon: Icon(Icons.more_vert, size: 16, color: theme.textSecondary),
                                        color: theme.surface,
                                        itemBuilder: (ctx) => [
                                          PopupMenuItem(
                                            value: 'toggle',
                                            child: Text(s.isActive ? 'Deactivate script' : 'Activate script', style: TextStyle(color: theme.textPrimary, fontSize: 12)),
                                          ),
                                          PopupMenuItem(
                                            value: 'duplicate',
                                            child: Text('Duplicate', style: TextStyle(color: theme.textPrimary, fontSize: 12)),
                                          ),
                                          PopupMenuItem(
                                            value: 'delete',
                                            child: Text('Delete', style: TextStyle(color: theme.errorColor, fontSize: 12)),
                                          ),
                                        ],
                                        onSelected: (val) {
                                          if (val == 'toggle') _toggleScriptActive(s.id);
                                          if (val == 'duplicate') _duplicateActiveScript();
                                          if (val == 'delete') _deleteScript(s.id);
                                        },
                                      ),
                                    ],
                                  ),
                                ),
                              );
                            }).toList(),
                          ),
                        ),
                      ],
                    ),
                  ),

                  // 2. CENTER PANEL: Sequential Step Editor & Bottom Inspector
                  Expanded(
                    child: Column(
                      children: [
                        // Tabs bar
                        Container(
                          height: 38,
                          decoration: BoxDecoration(
                            color: theme.surfaceContainer,
                            border: Border(bottom: BorderSide(color: theme.border)),
                          ),
                          child: ListView(
                            scrollDirection: Axis.horizontal,
                            children: _openScriptIds.map((sid) {
                              final tabScript = _scripts.firstWhere((s) => s.id == sid, orElse: () => _scripts.first);
                              final isActiveTab = sid == _activeScriptId;

                              return InkWell(
                                onTap: () => setState(() => _activeScriptId = sid),
                                child: Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 14),
                                  decoration: BoxDecoration(
                                    color: isActiveTab ? theme.surface : Colors.transparent,
                                    border: Border(
                                      top: isActiveTab ? BorderSide(color: theme.primaryAccent, width: 2) : BorderSide.none,
                                      right: BorderSide(color: theme.border),
                                    ),
                                  ),
                                  child: Row(
                                    children: [
                                      Icon(Icons.code, size: 14, color: isActiveTab ? theme.primaryAccent : theme.textSecondary),
                                      const SizedBox(width: 8),
                                      Text(
                                        tabScript.name,
                                        style: TextStyle(
                                          color: isActiveTab ? theme.textPrimary : theme.textSecondary,
                                          fontSize: 12,
                                          fontWeight: isActiveTab ? FontWeight.bold : FontWeight.normal,
                                        ),
                                      ),
                                      const SizedBox(width: 10),
                                      InkWell(
                                        onTap: () => _closeTab(sid),
                                        child: Icon(Icons.close, size: 14, color: theme.textSecondary),
                                      ),
                                    ],
                                  ),
                                ),
                              );
                            }).toList(),
                          ),
                        ),

                        // Editor body
                        if (script == null)
                          Expanded(
                            child: Center(
                              child: Column(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  Icon(Icons.terminal, size: 48, color: theme.textSecondary),
                                  const SizedBox(height: 12),
                                  Text('No script selected.', style: TextStyle(color: theme.textSecondary, fontSize: 14)),
                                  const SizedBox(height: 12),
                                  ElevatedButton.icon(
                                    icon: const Icon(Icons.add, size: 16),
                                    label: const Text('Create new script'),
                                    style: ElevatedButton.styleFrom(backgroundColor: theme.primaryAccent),
                                    onPressed: _addNewScript,
                                  ),
                                ],
                              ),
                            ),
                          )
                        else
                          Expanded(
                            child: Column(
                              children: [
                                // Subheader with script metadata
                                Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                                  decoration: BoxDecoration(
                                    color: theme.surface,
                                    border: Border(bottom: BorderSide(color: theme.border)),
                                  ),
                                  child: SingleChildScrollView(
                                    scrollDirection: Axis.horizontal,
                                    child: Row(
                                      children: [
                                        Text('Name: ', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                                        SizedBox(
                                          width: 160,
                                          height: 28,
                                          child: TextFormField(
                                            initialValue: script.name,
                                            style: TextStyle(color: theme.textPrimary, fontSize: 12, fontWeight: FontWeight.bold),
                                            decoration: InputDecoration(
                                              contentPadding: const EdgeInsets.symmetric(horizontal: 6),
                                              filled: true,
                                              fillColor: theme.surfaceContainer,
                                              border: OutlineInputBorder(borderSide: BorderSide(color: theme.border)),
                                            ),
                                            onChanged: (val) {
                                              final idx = _scripts.indexWhere((s) => s.id == script.id);
                                              if (idx != -1) {
                                                setState(() {
                                                  _scripts[idx] = script.copyWith(name: val);
                                                  _hasUnsavedChanges = true;
                                                });
                                              }
                                            },
                                          ),
                                        ),
                                        const SizedBox(width: 16),
                                        Text('Context (Table): ', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                                        Container(
                                          height: 28,
                                          padding: const EdgeInsets.symmetric(horizontal: 8),
                                          decoration: BoxDecoration(
                                            color: theme.surfaceContainer,
                                            borderRadius: BorderRadius.circular(4),
                                            border: Border.all(color: theme.border),
                                          ),
                                          child: DropdownButtonHideUnderline(
                                            child: DropdownButton<String>(
                                              value: _tables.any((t) => t.name == script.contextTable) ? script.contextTable : (_tables.isNotEmpty ? _tables.first.name : null),
                                              hint: Text('No table', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                                              dropdownColor: theme.surface,
                                              style: TextStyle(color: theme.primaryAccent, fontSize: 11, fontWeight: FontWeight.bold),
                                              items: _tables.map((t) => DropdownMenuItem(value: t.name, child: Text(t.name))).toList(),
                                              onChanged: (val) {
                                                if (val != null) {
                                                  final idx = _scripts.indexWhere((s) => s.id == script.id);
                                                  if (idx != -1) {
                                                    setState(() {
                                                      _scripts[idx] = script.copyWith(contextTable: val);
                                                      _hasUnsavedChanges = true;
                                                    });
                                                  }
                                                }
                                              },
                                            ),
                                          ),
                                        ),
                                        const SizedBox(width: 16),
                                        Text(
                                          '${script.steps.length} steps',
                                          style: TextStyle(color: theme.textSecondary, fontSize: 11),
                                        ),
                                      ],
                                    ),
                                  ),
                                ),

                                // Sequential Steps Reorderable List
                                Expanded(
                                  child: Container(
                                    color: theme.background,
                                    child: script.steps.isEmpty
                                        ? Center(
                                            child: Text('This script has no steps. Add steps from the catalog on the right.', style: TextStyle(color: theme.textSecondary, fontSize: 12)),
                                          )
                                        : ReorderableListView.builder(
                                            padding: const EdgeInsets.symmetric(vertical: 6),
                                            itemCount: script.steps.length,
                                            onReorder: _moveStep,
                                            itemBuilder: (context, idx) {
                                              final step = script.steps[idx];
                                              final isSelected = _selectedStepIdx == idx;
                                              final indent = _calculateStepIndent(script.steps, idx);
                                              final catColor = step.getCategoryColor(theme);

                                              return InkWell(
                                                key: ValueKey(step.id),
                                                onTap: () => setState(() => _selectedStepIdx = idx),
                                                child: Container(
                                                  margin: const EdgeInsets.symmetric(vertical: 1),
                                                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                                                  decoration: BoxDecoration(
                                                    color: isSelected ? theme.primaryAccent.withValues(alpha: 0.15) : Colors.transparent,
                                                    border: isSelected
                                                        ? Border(left: BorderSide(color: theme.primaryAccent, width: 3))
                                                        : null,
                                                  ),
                                                  child: Row(
                                                    children: [
                                                      // Indentation margin & guide
                                                      SizedBox(width: indent * 20.0),
                                                      if (indent > 0)
                                                        Container(
                                                          width: 2,
                                                          height: 20,
                                                          color: theme.border,
                                                          margin: const EdgeInsets.only(right: 8),
                                                        ),

                                                      // 2-digit Sequence number
                                                      Container(
                                                        width: 24,
                                                        alignment: Alignment.centerRight,
                                                        child: Text(
                                                          (idx + 1).toString().padLeft(2, '0'),
                                                          style: TextStyle(
                                                            color: isSelected ? theme.primaryAccent : theme.textSecondary,
                                                            fontSize: 11,
                                                            fontFamily: 'monospace',
                                                            fontWeight: FontWeight.bold,
                                                          ),
                                                        ),
                                                      ),
                                                      const SizedBox(width: 10),

                                                      // Active toggle checkbox
                                                      InkWell(
                                                        onTap: () => _toggleStepEnabled(idx),
                                                        child: Icon(
                                                          step.isEnabled ? Icons.check_box : Icons.check_box_outline_blank,
                                                          size: 16,
                                                          color: step.isEnabled ? theme.successColor : theme.textSecondary,
                                                        ),
                                                      ),
                                                      const SizedBox(width: 8),

                                                      // Category badge
                                                      Container(
                                                        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                                                        decoration: BoxDecoration(
                                                          color: catColor.withValues(alpha: 0.15),
                                                          borderRadius: BorderRadius.circular(4),
                                                          border: Border.all(color: catColor.withValues(alpha: 0.4)),
                                                        ),
                                                        child: Text(
                                                          step.category.toUpperCase(),
                                                          style: TextStyle(color: catColor, fontSize: 9, fontWeight: FontWeight.bold),
                                                        ),
                                                      ),
                                                      const SizedBox(width: 10),

                                                      // Instruction Name
                                                      Text(
                                                        step.displayName,
                                                        style: TextStyle(
                                                          color: step.isEnabled ? theme.textPrimary : theme.textSecondary,
                                                          fontSize: 12,
                                                          fontWeight: FontWeight.bold,
                                                        ),
                                                      ),
                                                      const SizedBox(width: 8),

                                                      // Parameter Preview
                                                      Expanded(
                                                        child: Text(
                                                          step.previewText,
                                                          style: TextStyle(
                                                            color: step.isEnabled ? theme.primaryAccent : theme.textSecondary,
                                                            fontSize: 12,
                                                            fontFamily: 'monospace',
                                                          ),
                                                          overflow: TextOverflow.ellipsis,
                                                        ),
                                                      ),

                                                      // Quick delete step
                                                      IconButton(
                                                        icon: Icon(Icons.close, size: 14, color: theme.textSecondary),
                                                        padding: EdgeInsets.zero,
                                                        constraints: const BoxConstraints(),
                                                        splashRadius: 14,
                                                        onPressed: () => _removeStep(idx),
                                                      ),
                                                    ],
                                                  ),
                                                ),
                                              );
                                            },
                                          ),
                                  ),
                                ),

                                // Bottom Contextual Parameter Inspector
                                Container(
                                  height: 140,
                                  decoration: BoxDecoration(
                                    color: theme.surface,
                                    border: Border(top: BorderSide(color: theme.border)),
                                  ),
                                  padding: const EdgeInsets.all(12),
                                  child: activeStep == null
                                      ? Center(
                                          child: Text('Select a step from the list above to configure its parameters.', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                                        )
                                      : _buildContextualInspector(activeStep, theme),
                                ),
                              ],
                            ),
                          ),
                      ],
                    ),
                  ),

                  // 3. RIGHT PANEL: Step Catalog
                  Container(
                    width: 270,
                    decoration: BoxDecoration(
                      color: theme.surface,
                      border: Border(left: BorderSide(color: theme.border)),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        // Header and Search
                        Container(
                          padding: const EdgeInsets.all(10),
                          decoration: BoxDecoration(
                            border: Border(bottom: BorderSide(color: theme.border)),
                          ),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text('Step catalog', style: TextStyle(color: theme.textPrimary, fontSize: 12, fontWeight: FontWeight.bold)),
                              const SizedBox(height: 8),
                              SizedBox(
                                height: 30,
                                child: TextField(
                                  style: TextStyle(color: theme.textPrimary, fontSize: 11),
                                  decoration: InputDecoration(
                                    hintText: 'Filter steps...',
                                    hintStyle: TextStyle(color: theme.textSecondary, fontSize: 11),
                                    prefixIcon: Icon(Icons.search, size: 15, color: theme.textSecondary),
                                    contentPadding: EdgeInsets.zero,
                                    filled: true,
                                    fillColor: theme.surfaceContainer,
                                    border: OutlineInputBorder(borderRadius: BorderRadius.circular(6), borderSide: BorderSide.none),
                                  ),
                                  onChanged: (v) => setState(() => _catalogFilter = v),
                                ),
                              ),
                            ],
                          ),
                        ),

                        // Catalog categories & items
                        Expanded(
                          child: ListView(
                            padding: const EdgeInsets.symmetric(vertical: 4),
                            children: _buildCatalogItems(theme),
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),

            // Bottom Status Bar
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
              decoration: BoxDecoration(
                color: theme.surfaceContainer,
                borderRadius: const BorderRadius.vertical(bottom: Radius.circular(12)),
                border: Border(top: BorderSide(color: theme.border)),
              ),
              child: Row(
                children: [
                  Icon(Icons.table_chart_outlined, color: theme.primaryAccent, size: 14),
                  const SizedBox(width: 6),
                  Text(
                    'Context: ${script?.contextTable.isNotEmpty == true ? script!.contextTable : "Global"}',
                    style: TextStyle(color: theme.textSecondary, fontSize: 11),
                  ),
                  const SizedBox(width: 16),
                  Icon(Icons.list_alt, color: theme.successColor, size: 14),
                  const SizedBox(width: 6),
                  Text(
                    'Steps: ${script?.steps.length ?? 0}',
                    style: TextStyle(color: theme.textSecondary, fontSize: 11),
                  ),
                  const Spacer(),
                  Icon(Icons.bolt, color: theme.primaryAccent, size: 14),
                  const SizedBox(width: 6),
                  Text(
                    'Engine: File4Base Script Engine v1.0 (Go/CEL)',
                    style: TextStyle(color: theme.textSecondary, fontSize: 11),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  // Build Contextual Inspector for active step
  Widget _buildContextualInspector(ScriptStepModel step, AppThemeDefinition theme) {
    switch (step.stepType) {
      case 'set_variable':
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.tune, color: theme.primaryAccent, size: 16),
                const SizedBox(width: 6),
                Text('Parameters: ${step.displayName}', style: TextStyle(color: theme.textPrimary, fontSize: 12, fontWeight: FontWeight.bold)),
              ],
            ),
            const SizedBox(height: 10),
            Row(
              children: [
                Text('Variable name: ', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                SizedBox(
                  width: 140,
                  height: 30,
                  child: TextFormField(
                    key: ValueKey('${step.id}-var'),
                    initialValue: step.params['variable']?.toString() ?? r'$var',
                    style: TextStyle(color: theme.primaryAccent, fontSize: 12, fontFamily: 'monospace'),
                    decoration: InputDecoration(
                      contentPadding: const EdgeInsets.symmetric(horizontal: 8),
                      filled: true,
                      fillColor: theme.surfaceContainer,
                      border: OutlineInputBorder(borderSide: BorderSide(color: theme.border)),
                    ),
                    onChanged: (v) => _updateStepParam('variable', v),
                  ),
                ),
                const SizedBox(width: 16),
                Text('Value / Calculation: ', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                Expanded(
                  child: SizedBox(
                    height: 30,
                    child: TextFormField(
                      key: ValueKey('${step.id}-calc'),
                      initialValue: step.params['calc']?.toString() ?? '',
                      style: TextStyle(color: theme.textPrimary, fontSize: 12, fontFamily: 'monospace'),
                      decoration: InputDecoration(
                        contentPadding: const EdgeInsets.symmetric(horizontal: 8),
                        filled: true,
                        fillColor: theme.surfaceContainer,
                        border: OutlineInputBorder(borderSide: BorderSide(color: theme.border)),
                      ),
                      onChanged: (v) => _updateStepParam('calc', v),
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                ElevatedButton.icon(
                  icon: const Icon(Icons.functions, size: 14),
                  label: const Text('fx Specify...', style: TextStyle(fontSize: 11)),
                  style: ElevatedButton.styleFrom(
                    backgroundColor: theme.primaryAccent,
                    foregroundColor: theme.isDark ? const Color(0xFF0B1120) : Colors.white,
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                  ),
                  onPressed: () => _openCalculationDialogForParam('calc'),
                ),
              ],
            ),
          ],
        );

      case 'set_field':
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.tune, color: theme.primaryAccent, size: 16),
                const SizedBox(width: 6),
                Text('Parameters: ${step.displayName}', style: TextStyle(color: theme.textPrimary, fontSize: 12, fontWeight: FontWeight.bold)),
              ],
            ),
            const SizedBox(height: 10),
            Row(
              children: [
                Text('Target field: ', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                SizedBox(
                  width: 200,
                  height: 30,
                  child: TextFormField(
                    key: ValueKey('${step.id}-field'),
                    initialValue: step.params['field']?.toString() ?? '',
                    style: TextStyle(color: theme.fieldsColor, fontSize: 12, fontFamily: 'monospace'),
                    decoration: InputDecoration(
                      hintText: 'Table::Field',
                      hintStyle: TextStyle(color: theme.textSecondary, fontSize: 11),
                      contentPadding: const EdgeInsets.symmetric(horizontal: 8),
                      filled: true,
                      fillColor: theme.surfaceContainer,
                      border: OutlineInputBorder(borderSide: BorderSide(color: theme.border)),
                    ),
                    onChanged: (v) => _updateStepParam('field', v),
                  ),
                ),
                const SizedBox(width: 16),
                Text('Calculated value: ', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                Expanded(
                  child: SizedBox(
                    height: 30,
                    child: TextFormField(
                      key: ValueKey('${step.id}-value'),
                      initialValue: step.params['value']?.toString() ?? '',
                      style: TextStyle(color: theme.textPrimary, fontSize: 12, fontFamily: 'monospace'),
                      decoration: InputDecoration(
                        contentPadding: const EdgeInsets.symmetric(horizontal: 8),
                        filled: true,
                        fillColor: theme.surfaceContainer,
                        border: OutlineInputBorder(borderSide: BorderSide(color: theme.border)),
                      ),
                      onChanged: (v) => _updateStepParam('value', v),
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                ElevatedButton.icon(
                  icon: const Icon(Icons.functions, size: 14),
                  label: const Text('fx Specify...', style: TextStyle(fontSize: 11)),
                  style: ElevatedButton.styleFrom(
                    backgroundColor: theme.primaryAccent,
                    foregroundColor: theme.isDark ? const Color(0xFF0B1120) : Colors.white,
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                  ),
                  onPressed: () => _openCalculationDialogForParam('value'),
                ),
              ],
            ),
          ],
        );

      case 'go_to_layout':
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.tune, color: theme.navColor, size: 16),
                const SizedBox(width: 6),
                Text('Parameters: ${step.displayName}', style: TextStyle(color: theme.textPrimary, fontSize: 12, fontWeight: FontWeight.bold)),
              ],
            ),
            const SizedBox(height: 10),
            Row(
              children: [
                Text('Target layout: ', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                Container(
                  height: 30,
                  padding: const EdgeInsets.symmetric(horizontal: 10),
                  decoration: BoxDecoration(
                    color: theme.surfaceContainer,
                    borderRadius: BorderRadius.circular(4),
                    border: Border.all(color: theme.border),
                  ),
                  child: DropdownButtonHideUnderline(
                    child: DropdownButton<String>(
                      value: _layouts.any((l) => l.name == step.params['layout_name'])
                          ? step.params['layout_name']
                          : (_layouts.isNotEmpty ? _layouts.first.name : null),
                      hint: Text('Select layout...', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                      dropdownColor: theme.surface,
                      style: TextStyle(color: theme.textPrimary, fontSize: 12),
                      items: _layouts.map((l) => DropdownMenuItem(value: l.name, child: Text(l.name))).toList(),
                      onChanged: (v) {
                        if (v != null) _updateStepParam('layout_name', v);
                      },
                    ),
                  ),
                ),
              ],
            ),
          ],
        );

      case 'if':
      case 'exit_loop_if':
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.tune, color: theme.controlColor, size: 16),
                const SizedBox(width: 6),
                Text('Parameters: ${step.displayName}', style: TextStyle(color: theme.textPrimary, fontSize: 12, fontWeight: FontWeight.bold)),
              ],
            ),
            const SizedBox(height: 10),
            Row(
              children: [
                Text('Logical condition: ', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                Expanded(
                  child: SizedBox(
                    height: 30,
                    child: TextFormField(
                      key: ValueKey('${step.id}-cond'),
                      initialValue: step.params['condition']?.toString() ?? '',
                      style: TextStyle(color: theme.controlColor, fontSize: 12, fontFamily: 'monospace'),
                      decoration: InputDecoration(
                        hintText: 'e.g. Invoices::Total > 5000',
                        hintStyle: TextStyle(color: theme.textSecondary, fontSize: 11),
                        contentPadding: const EdgeInsets.symmetric(horizontal: 8),
                        filled: true,
                        fillColor: theme.surfaceContainer,
                        border: OutlineInputBorder(borderSide: BorderSide(color: theme.border)),
                      ),
                      onChanged: (v) => _updateStepParam('condition', v),
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                ElevatedButton.icon(
                  icon: const Icon(Icons.functions, size: 14),
                  label: const Text('fx Specify...', style: TextStyle(fontSize: 11)),
                  style: ElevatedButton.styleFrom(
                    backgroundColor: theme.primaryAccent,
                    foregroundColor: theme.isDark ? const Color(0xFF0B1120) : Colors.white,
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                  ),
                  onPressed: () => _openCalculationDialogForParam('condition'),
                ),
              ],
            ),
          ],
        );

      case 'perform_rest_api':
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.tune, color: theme.integrationColor, size: 16),
                const SizedBox(width: 6),
                Text('Parameters: ${step.displayName}', style: TextStyle(color: theme.textPrimary, fontSize: 12, fontWeight: FontWeight.bold)),
              ],
            ),
            const SizedBox(height: 10),
            Row(
              children: [
                Container(
                  height: 30,
                  padding: const EdgeInsets.symmetric(horizontal: 8),
                  decoration: BoxDecoration(
                    color: theme.surfaceContainer,
                    borderRadius: BorderRadius.circular(4),
                    border: Border.all(color: theme.border),
                  ),
                  child: DropdownButtonHideUnderline(
                    child: DropdownButton<String>(
                      value: step.params['method']?.toString() ?? 'POST',
                      dropdownColor: theme.surface,
                      style: TextStyle(color: theme.integrationColor, fontSize: 11, fontWeight: FontWeight.bold),
                      items: ['GET', 'POST', 'PUT', 'DELETE'].map((m) => DropdownMenuItem(value: m, child: Text(m))).toList(),
                      onChanged: (v) {
                        if (v != null) _updateStepParam('method', v);
                      },
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: SizedBox(
                    height: 30,
                    child: TextFormField(
                      key: ValueKey('${step.id}-url'),
                      initialValue: step.params['url']?.toString() ?? '',
                      style: TextStyle(color: theme.textPrimary, fontSize: 12, fontFamily: 'monospace'),
                      decoration: InputDecoration(
                        hintText: 'https://api.example.com/v1/endpoint',
                        hintStyle: TextStyle(color: theme.textSecondary, fontSize: 11),
                        contentPadding: const EdgeInsets.symmetric(horizontal: 8),
                        filled: true,
                        fillColor: theme.surfaceContainer,
                        border: OutlineInputBorder(borderSide: BorderSide(color: theme.border)),
                      ),
                      onChanged: (v) => _updateStepParam('url', v),
                    ),
                  ),
                ),
              ],
            ),
          ],
        );

      default:
        final catColor = step.getCategoryColor(theme);
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.tune, color: catColor, size: 16),
                const SizedBox(width: 6),
                Text('Parameters: ${step.displayName}', style: TextStyle(color: theme.textPrimary, fontSize: 12, fontWeight: FontWeight.bold)),
              ],
            ),
            const SizedBox(height: 10),
            Text(
              'This step requires no additional parameters or runs with its default options.',
              style: TextStyle(color: theme.textSecondary, fontSize: 12),
            ),
          ],
        );
    }
  }

  // Build Catalog categorized list
  List<Widget> _buildCatalogItems(AppThemeDefinition theme) {
    final Map<String, List<ScriptCatalogItem>> categorized = {};
    for (final item in ScriptCatalogItem.catalog) {
      if (_catalogFilter.isNotEmpty &&
          !item.label.toLowerCase().contains(_catalogFilter.toLowerCase()) &&
          !item.description.toLowerCase().contains(_catalogFilter.toLowerCase())) {
        continue;
      }
      categorized.putIfAbsent(item.category, () => []).add(item);
    }

    final List<Widget> widgets = [];
    for (final entry in categorized.entries) {
      widgets.add(
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
          color: theme.surfaceContainer,
          child: Text(
            entry.key,
            style: TextStyle(
              color: theme.textSecondary,
              fontSize: 10,
              fontWeight: FontWeight.bold,
              letterSpacing: 0.5,
            ),
          ),
        ),
      );

      for (final item in entry.value) {
        widgets.add(
          InkWell(
            onTap: () => _addStepToActiveScript(item),
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
              decoration: BoxDecoration(
                border: Border(bottom: BorderSide(color: theme.border.withValues(alpha: 0.5), width: 0.5)),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Icon(Icons.add_circle_outline, size: 13, color: theme.primaryAccent),
                      const SizedBox(width: 6),
                      Expanded(
                        child: Text(
                          item.label.replaceAll('+ ', ''),
                          style: TextStyle(color: theme.textPrimary, fontSize: 11, fontWeight: FontWeight.w500),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 2),
                  Padding(
                    padding: const EdgeInsets.only(left: 19),
                    child: Text(
                      item.description,
                      style: TextStyle(color: theme.textSecondary, fontSize: 9),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
              ),
            ),
          ),
        );
      }
    }
    return widgets;
  }
}
