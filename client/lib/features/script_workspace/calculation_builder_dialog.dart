import 'package:flutter/material.dart';

class CalculationBuilderDialog extends StatefulWidget {
  final String initialFormula;
  final String? contextTable;
  final List<String> availableTables;
  final Map<String, List<String>> fieldsByTable;

  const CalculationBuilderDialog({
    super.key,
    this.initialFormula = '',
    this.contextTable,
    this.availableTables = const [],
    this.fieldsByTable = const {},
  });

  static Future<String?> show(
    BuildContext context, {
    String initialFormula = '',
    String? contextTable,
    List<String> availableTables = const [],
    Map<String, List<String>> fieldsByTable = const {},
  }) {
    return showDialog<String>(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => CalculationBuilderDialog(
        initialFormula: initialFormula,
        contextTable: contextTable,
        availableTables: availableTables,
        fieldsByTable: fieldsByTable,
      ),
    );
  }

  @override
  State<CalculationBuilderDialog> createState() => _CalculationBuilderDialogState();
}

class _CalculationBuilderDialogState extends State<CalculationBuilderDialog> {
  late TextEditingController _formulaController;
  late FocusNode _formulaFocusNode;
  String _selectedTable = '';
  String _fieldSearch = '';
  String _functionSearch = '';
  String _functionCategory = 'Todas';

  static const List<String> _operators = [
    '+', '-', '*', '/', '^', '&',
    '=', '≠', '>', '<', '≥', '≤',
    'AND', 'OR', 'NOT', '(', ')', '" "'
  ];

  static const Map<String, List<Map<String, String>>> _functionsByCategory = {
    'Agregación': [
      {'name': 'Sum', 'syntax': 'Sum( campo )', 'desc': 'Suma todos los valores no vacíos.'},
      {'name': 'Average', 'syntax': 'Average( campo )', 'desc': 'Calcula la media de los valores.'},
      {'name': 'Count', 'syntax': 'Count( campo )', 'desc': 'Cuenta los valores válidos no vacíos.'},
      {'name': 'Max', 'syntax': 'Max( campo )', 'desc': 'Determina el valor máximo.'},
      {'name': 'Min', 'syntax': 'Min( campo )', 'desc': 'Determina el valor mínimo.'},
    ],
    'Lógica': [
      {'name': 'If', 'syntax': 'If( condición ; resultado1 ; resultado2 )', 'desc': 'Evalúa una condición y retorna un valor según sea verdadera o falsa.'},
      {'name': 'IsEmpty', 'syntax': 'IsEmpty( campo )', 'desc': 'Devuelve 1 (verdadero) si el campo está vacío.'},
      {'name': 'IsValid', 'syntax': 'IsValid( campo )', 'desc': 'Devuelve 1 si el campo contiene un valor válido.'},
      {'name': 'Case', 'syntax': 'Case( prueba1 ; result1 ; [prueba2 ; result2] ; default )', 'desc': 'Evalúa múltiples condiciones secuenciales.'},
    ],
    'Texto': [
      {'name': 'Length', 'syntax': 'Length( texto )', 'desc': 'Retorna la longitud en caracteres.'},
      {'name': 'Lower', 'syntax': 'Lower( texto )', 'desc': 'Convierte el texto a minúsculas.'},
      {'name': 'Upper', 'syntax': 'Upper( texto )', 'desc': 'Convierte el texto a mayúsculas.'},
      {'name': 'Trim', 'syntax': 'Trim( texto )', 'desc': 'Elimina espacios al inicio y al final.'},
      {'name': 'Substitute', 'syntax': 'Substitute( texto ; buscar ; reemplazar )', 'desc': 'Sustituye texto por otro especificado.'},
    ],
    'Fecha & Hora': [
      {'name': 'Get(CurrentDate)', 'syntax': 'Get(CurrentDate)', 'desc': 'Devuelve la fecha actual del sistema.'},
      {'name': 'Get(CurrentTime)', 'syntax': 'Get(CurrentTime)', 'desc': 'Devuelve la hora actual del sistema.'},
      {'name': 'Get(CurrentTimestamp)', 'syntax': 'Get(CurrentTimestamp)', 'desc': 'Devuelve marca temporal completa.'},
    ],
    'Sistema': [
      {'name': 'Get(AccountName)', 'syntax': 'Get(AccountName)', 'desc': 'Nombre de la cuenta del usuario activo.'},
      {'name': 'Get(LayoutName)', 'syntax': 'Get(LayoutName)', 'desc': 'Nombre de la presentación activa.'},
      {'name': 'Get(RecordID)', 'syntax': 'Get(RecordID)', 'desc': 'Identificador único del registro.'},
      {'name': 'Get(LastError)', 'syntax': 'Get(LastError)', 'desc': 'Código de error de la última acción.'},
    ],
  };

  @override
  void initState() {
    super.initState();
    _formulaController = TextEditingController(text: widget.initialFormula);
    _formulaFocusNode = FocusNode();

    if (widget.contextTable != null && widget.contextTable!.isNotEmpty) {
      _selectedTable = widget.contextTable!;
    } else if (widget.availableTables.isNotEmpty) {
      _selectedTable = widget.availableTables.first;
    }
  }

  @override
  void dispose() {
    _formulaController.dispose();
    _formulaFocusNode.dispose();
    super.dispose();
  }

  void _insertAtCursor(String text) {
    final currentText = _formulaController.text;
    final selection = _formulaController.selection;
    if (selection.start >= 0 && selection.end >= selection.start) {
      final newText = currentText.replaceRange(selection.start, selection.end, text);
      _formulaController.value = TextEditingValue(
        text: newText,
        selection: TextSelection.collapsed(offset: selection.start + text.length),
      );
    } else {
      _formulaController.text = currentText + text;
      _formulaController.selection = TextSelection.collapsed(offset: _formulaController.text.length);
    }
    _formulaFocusNode.requestFocus();
  }

  List<String> _getFilteredFields() {
    final fields = widget.fieldsByTable[_selectedTable] ?? [];
    if (_fieldSearch.isEmpty) return fields;
    return fields.where((f) => f.toLowerCase().contains(_fieldSearch.toLowerCase())).toList();
  }

  List<Map<String, String>> _getFilteredFunctions() {
    List<Map<String, String>> list = [];
    if (_functionCategory == 'Todas') {
      for (final cat in _functionsByCategory.values) {
        list.addAll(cat);
      }
    } else {
      list = _functionsByCategory[_functionCategory] ?? [];
    }

    if (_functionSearch.isNotEmpty) {
      list = list.where((f) =>
        f['name']!.toLowerCase().contains(_functionSearch.toLowerCase()) ||
        f['desc']!.toLowerCase().contains(_functionSearch.toLowerCase())
      ).toList();
    }
    return list;
  }

  @override
  Widget build(BuildContext context) {
    return Dialog(
      backgroundColor: Colors.transparent,
      child: Container(
        width: 1040,
        height: 660,
        decoration: BoxDecoration(
          color: const Color(0xFF0F172A),
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: const Color(0xFF334155)),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: 0.6),
              blurRadius: 32,
              offset: const Offset(0, 12),
            ),
          ],
        ),
        child: Column(
          children: [
            // Top Window Bar
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
              decoration: const BoxDecoration(
                color: Color(0xFF1E293B),
                borderRadius: BorderRadius.vertical(top: Radius.circular(12)),
                border: Border(bottom: BorderSide(color: Color(0xFF334155))),
              ),
              child: Row(
                children: [
                  const Icon(Icons.functions, color: Color(0xFF38BDF8), size: 20),
                  const SizedBox(width: 10),
                  const Text(
                    'Especificar cálculo',
                    style: TextStyle(
                      color: Colors.white,
                      fontSize: 15,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const Spacer(),
                  if (_selectedTable.isNotEmpty)
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                      decoration: BoxDecoration(
                        color: const Color(0xFF0284C7).withValues(alpha: 0.2),
                        borderRadius: BorderRadius.circular(6),
                        border: Border.all(color: const Color(0xFF0284C7).withValues(alpha: 0.4)),
                      ),
                      child: Text(
                        'Contexto: $_selectedTable',
                        style: const TextStyle(color: Color(0xFF38BDF8), fontSize: 12, fontWeight: FontWeight.w500),
                      ),
                    ),
                  const SizedBox(width: 12),
                  IconButton(
                    icon: const Icon(Icons.close, color: Colors.grey, size: 20),
                    onPressed: () => Navigator.of(context).pop(),
                    tooltip: 'Cerrar',
                    splashRadius: 18,
                  ),
                ],
              ),
            ),

            // Main 3-panel area
            Expanded(
              child: Row(
                children: [
                  // Left: Fields panel
                  Container(
                    width: 250,
                    decoration: const BoxDecoration(
                      color: Color(0xFF111827),
                      border: Border(right: BorderSide(color: Color(0xFF1F2937))),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        // Table selector
                        Container(
                          padding: const EdgeInsets.all(10),
                          decoration: const BoxDecoration(
                            border: Border(bottom: BorderSide(color: Color(0xFF1F2937))),
                          ),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              const Text('Ocurrencia de tabla', style: TextStyle(color: Colors.grey, fontSize: 11, fontWeight: FontWeight.bold)),
                              const SizedBox(height: 6),
                              Container(
                                padding: const EdgeInsets.symmetric(horizontal: 10),
                                decoration: BoxDecoration(
                                  color: const Color(0xFF1E293B),
                                  borderRadius: BorderRadius.circular(6),
                                  border: Border.all(color: const Color(0xFF334155)),
                                ),
                                child: DropdownButtonHideUnderline(
                                  child: DropdownButton<String>(
                                    isExpanded: true,
                                    dropdownColor: const Color(0xFF1E293B),
                                    value: _selectedTable.isNotEmpty ? _selectedTable : null,
                                    hint: const Text('Seleccionar tabla...', style: TextStyle(color: Colors.grey, fontSize: 12)),
                                    style: const TextStyle(color: Colors.white, fontSize: 12),
                                    items: widget.availableTables.map((t) => DropdownMenuItem(value: t, child: Text(t))).toList(),
                                    onChanged: (val) {
                                      if (val != null) setState(() => _selectedTable = val);
                                    },
                                  ),
                                ),
                              ),
                              const SizedBox(height: 8),
                              // Field search
                              SizedBox(
                                height: 32,
                                child: TextField(
                                  style: const TextStyle(color: Colors.white, fontSize: 12),
                                  decoration: InputDecoration(
                                    hintText: 'Filtrar campos...',
                                    hintStyle: const TextStyle(color: Colors.grey, fontSize: 11),
                                    prefixIcon: const Icon(Icons.search, size: 16, color: Colors.grey),
                                    contentPadding: EdgeInsets.zero,
                                    filled: true,
                                    fillColor: const Color(0xFF1E293B),
                                    border: OutlineInputBorder(borderRadius: BorderRadius.circular(6), borderSide: BorderSide.none),
                                  ),
                                  onChanged: (v) => setState(() => _fieldSearch = v),
                                ),
                              ),
                            ],
                          ),
                        ),
                        // Fields list
                        Expanded(
                          child: ListView(
                            padding: const EdgeInsets.symmetric(vertical: 4),
                            children: _getFilteredFields().map((field) {
                              return InkWell(
                                onTap: () {
                                  final tablePrefix = _selectedTable.isNotEmpty ? '$_selectedTable::' : '';
                                  _insertAtCursor('$tablePrefix$field');
                                },
                                child: Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
                                  child: Row(
                                    children: [
                                      const Icon(Icons.data_object, color: Color(0xFF38BDF8), size: 14),
                                      const SizedBox(width: 8),
                                      Expanded(
                                        child: Text(
                                          field,
                                          style: const TextStyle(color: Color(0xFFE2E8F0), fontSize: 12),
                                          overflow: TextOverflow.ellipsis,
                                        ),
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

                  // Center: Formula Editor & Operators
                  Expanded(
                    child: Container(
                      color: const Color(0xFF0B1120),
                      padding: const EdgeInsets.all(12),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          // Operators toolbar
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
                            margin: const EdgeInsets.only(bottom: 10),
                            decoration: BoxDecoration(
                              color: const Color(0xFF1E293B),
                              borderRadius: BorderRadius.circular(8),
                              border: Border.all(color: const Color(0xFF334155)),
                            ),
                            child: Wrap(
                              spacing: 6,
                              runSpacing: 4,
                              children: _operators.map((op) {
                                return InkWell(
                                  onTap: () => _insertAtCursor(op == '" "' ? '""' : ' $op '),
                                  borderRadius: BorderRadius.circular(4),
                                  child: Container(
                                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
                                    decoration: BoxDecoration(
                                      color: const Color(0xFF0F172A),
                                      borderRadius: BorderRadius.circular(4),
                                      border: Border.all(color: const Color(0xFF334155)),
                                    ),
                                    child: Text(
                                      op,
                                      style: const TextStyle(
                                        color: Color(0xFF38BDF8),
                                        fontSize: 12,
                                        fontWeight: FontWeight.bold,
                                        fontFamily: 'monospace',
                                      ),
                                    ),
                                  ),
                                );
                              }).toList(),
                            ),
                          ),

                          // Formula Box
                          const Text('Fórmula / Cálculo:', style: TextStyle(color: Colors.grey, fontSize: 11, fontWeight: FontWeight.bold)),
                          const SizedBox(height: 6),
                          Expanded(
                            child: Container(
                              decoration: BoxDecoration(
                                color: const Color(0xFF030712),
                                borderRadius: BorderRadius.circular(8),
                                border: Border.all(color: const Color(0xFF1E293B)),
                              ),
                              padding: const EdgeInsets.all(12),
                              child: TextField(
                                controller: _formulaController,
                                focusNode: _formulaFocusNode,
                                maxLines: null,
                                expands: true,
                                style: const TextStyle(
                                  color: Color(0xFFF8FAFC),
                                  fontSize: 13,
                                  fontFamily: 'monospace',
                                  height: 1.4,
                                ),
                                decoration: const InputDecoration(
                                  hintText: '// Escribe o inserta campos, funciones y operadores aquí...',
                                  hintStyle: TextStyle(color: Color(0xFF475569), fontSize: 12),
                                  border: InputBorder.none,
                                ),
                              ),
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),

                  // Right: Functions Catalog
                  Container(
                    width: 280,
                    decoration: const BoxDecoration(
                      color: Color(0xFF111827),
                      border: Border(left: BorderSide(color: Color(0xFF1F2937))),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        // Categories and Search
                        Container(
                          padding: const EdgeInsets.all(10),
                          decoration: const BoxDecoration(
                            border: Border(bottom: BorderSide(color: Color(0xFF1F2937))),
                          ),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              const Text('Funciones integradas', style: TextStyle(color: Colors.grey, fontSize: 11, fontWeight: FontWeight.bold)),
                              const SizedBox(height: 6),
                              Container(
                                padding: const EdgeInsets.symmetric(horizontal: 10),
                                decoration: BoxDecoration(
                                  color: const Color(0xFF1E293B),
                                  borderRadius: BorderRadius.circular(6),
                                  border: Border.all(color: const Color(0xFF334155)),
                                ),
                                child: DropdownButtonHideUnderline(
                                  child: DropdownButton<String>(
                                    isExpanded: true,
                                    dropdownColor: const Color(0xFF1E293B),
                                    value: _functionCategory,
                                    style: const TextStyle(color: Colors.white, fontSize: 12),
                                    items: ['Todas', ..._functionsByCategory.keys].map((cat) => DropdownMenuItem(value: cat, child: Text(cat))).toList(),
                                    onChanged: (val) {
                                      if (val != null) setState(() => _functionCategory = val);
                                    },
                                  ),
                                ),
                              ),
                              const SizedBox(height: 8),
                              SizedBox(
                                height: 32,
                                child: TextField(
                                  style: const TextStyle(color: Colors.white, fontSize: 12),
                                  decoration: InputDecoration(
                                    hintText: 'Buscar función...',
                                    hintStyle: const TextStyle(color: Colors.grey, fontSize: 11),
                                    prefixIcon: const Icon(Icons.search, size: 16, color: Colors.grey),
                                    contentPadding: EdgeInsets.zero,
                                    filled: true,
                                    fillColor: const Color(0xFF1E293B),
                                    border: OutlineInputBorder(borderRadius: BorderRadius.circular(6), borderSide: BorderSide.none),
                                  ),
                                  onChanged: (v) => setState(() => _functionSearch = v),
                                ),
                              ),
                            ],
                          ),
                        ),
                        // Functions list
                        Expanded(
                          child: ListView(
                            padding: const EdgeInsets.symmetric(vertical: 4),
                            children: _getFilteredFunctions().map((fn) {
                              return InkWell(
                                onTap: () => _insertAtCursor(fn['syntax']!),
                                child: Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                                  decoration: const BoxDecoration(
                                    border: Border(bottom: BorderSide(color: Color(0xFF1F2937), width: 0.5)),
                                  ),
                                  child: Column(
                                    crossAxisAlignment: CrossAxisAlignment.start,
                                    children: [
                                      Text(
                                        fn['name']!,
                                        style: const TextStyle(
                                          color: Color(0xFFF59E0B),
                                          fontSize: 12,
                                          fontWeight: FontWeight.bold,
                                          fontFamily: 'monospace',
                                        ),
                                      ),
                                      const SizedBox(height: 2),
                                      Text(
                                        fn['desc']!,
                                        style: const TextStyle(color: Colors.grey, fontSize: 10),
                                        maxLines: 2,
                                        overflow: TextOverflow.ellipsis,
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
                ],
              ),
            ),

            // Bottom Actions Bar
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
              decoration: const BoxDecoration(
                color: Color(0xFF1E293B),
                borderRadius: BorderRadius.vertical(bottom: Radius.circular(12)),
                border: Border(top: BorderSide(color: Color(0xFF334155))),
              ),
              child: Row(
                children: [
                  const Icon(Icons.info_outline, color: Colors.grey, size: 16),
                  const SizedBox(width: 8),
                  const Expanded(
                    child: Text(
                      'Haz doble clic en campos o funciones para insertarlos en la fórmula.',
                      style: TextStyle(color: Colors.grey, fontSize: 11),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  const SizedBox(width: 12),
                  OutlinedButton(
                    onPressed: () => Navigator.of(context).pop(),
                    style: OutlinedButton.styleFrom(
                      foregroundColor: Colors.grey.shade300,
                      side: const BorderSide(color: Color(0xFF475569)),
                    ),
                    child: const Text('Cancelar'),
                  ),
                  const SizedBox(width: 12),
                  ElevatedButton(
                    onPressed: () {
                      Navigator.of(context).pop(_formulaController.text);
                    },
                    style: ElevatedButton.styleFrom(
                      backgroundColor: const Color(0xFF0284C7),
                      foregroundColor: Colors.white,
                    ),
                    child: const Text('Aceptar'),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
