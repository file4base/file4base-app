import 'package:flutter/material.dart';
import '../../core/api/api_client.dart';
import 'models/layout_definition.dart';

class LayoutDesignerWidget extends StatefulWidget {
  final TableModel table;
  final ApiClient apiClient;
  final LayoutDefinitionModel initialLayout;
  final VoidCallback onSaved;

  const LayoutDesignerWidget({
    super.key,
    required this.table,
    required this.apiClient,
    required this.initialLayout,
    required this.onSaved,
  });

  @override
  State<LayoutDesignerWidget> createState() => _LayoutDesignerWidgetState();
}

class _LayoutDesignerWidgetState extends State<LayoutDesignerWidget> {
  late LayoutDefinitionModel _layout;
  String? _selectedObjectId;
  bool _snapToGrid = true;
  bool _isSaving = false;

  @override
  void initState() {
    super.initState();
    _layout = widget.initialLayout;
  }

  @override
  void didUpdateWidget(covariant LayoutDesignerWidget oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.initialLayout.id != widget.initialLayout.id) {
      _layout = widget.initialLayout;
      _selectedObjectId = null;
    }
  }

  LayoutObjectModel? get _selectedObject {
    if (_selectedObjectId == null) return null;
    try {
      return _layout.objects.firstWhere((o) => o.id == _selectedObjectId);
    } catch (_) {
      return null;
    }
  }

  double _snap(double value) {
    if (!_snapToGrid) return value;
    return (value / 8.0).roundToDouble() * 8.0;
  }

  void _addObject(String type) {
    final newId = 'obj_${DateTime.now().millisecondsSinceEpoch}';
    LayoutObjectModel newObj;

    if (type == 'field') {
      final availableCols = widget.table.columns.where((c) => !c.isPrimaryKey).toList();
      final defaultCol = availableCols.isNotEmpty ? availableCols.first.name : 'field';
      newObj = LayoutObjectModel(
        id: newId,
        type: 'field',
        x: _snap(120),
        y: _snap(120),
        width: 240,
        height: 36,
        fieldBinding: FieldBindingModel(fieldName: defaultCol),
      );
    } else if (type == 'button') {
      newObj = LayoutObjectModel(
        id: newId,
        type: 'button',
        x: _snap(120),
        y: _snap(120),
        width: 140,
        height: 36,
        text: 'Perform Action',
        style: const LayoutObjectStyle(fillColor: '#1E88E5', textColor: '#FFFFFF', cornerRadius: 6),
      );
    } else if (type == 'portal') {
      newObj = LayoutObjectModel(
        id: newId,
        type: 'portal',
        x: _snap(80),
        y: _snap(160),
        width: 480,
        height: 180,
        text: 'Portal (Related Records)',
        style: const LayoutObjectStyle(fillColor: '#F5F5F7', borderColor: '#B0BEC5'),
      );
    } else {
      newObj = LayoutObjectModel(
        id: newId,
        type: 'label',
        x: _snap(120),
        y: _snap(120),
        width: 160,
        height: 28,
        text: 'New Label',
        style: const LayoutObjectStyle(fontSize: 14, fontWeight: 'bold'),
      );
    }

    setState(() {
      _layout = LayoutDefinitionModel(
        id: _layout.id,
        name: _layout.name,
        tableOccurrence: _layout.tableOccurrence,
        width: _layout.width,
        theme: _layout.theme,
        defaultView: _layout.defaultView,
        parts: _layout.parts,
        objects: [..._layout.objects, newObj],
      );
      _selectedObjectId = newId;
    });
  }

  void _deleteSelectedObject() {
    if (_selectedObjectId == null) return;
    setState(() {
      _layout = LayoutDefinitionModel(
        id: _layout.id,
        name: _layout.name,
        tableOccurrence: _layout.tableOccurrence,
        width: _layout.width,
        theme: _layout.theme,
        defaultView: _layout.defaultView,
        parts: _layout.parts,
        objects: _layout.objects.where((o) => o.id != _selectedObjectId).toList(),
      );
      _selectedObjectId = null;
    });
  }

  Future<void> _saveLayout() async {
    setState(() => _isSaving = true);
    try {
      await widget.apiClient.createLayout(
        _layout.name,
        toId: widget.table.id,
        definition: _layout.toJson(),
      );
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Layout "${_layout.name}" saved successfully!')),
        );
        widget.onSaved();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed to save layout: $e'), backgroundColor: Colors.red),
        );
      }
    } finally {
      if (mounted) setState(() => _isSaving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        // Designer Toolbar
        _buildDesignerToolbar(context),
        const Divider(height: 1),
        // Designer Body (Canvas + Inspector)
        Expanded(
          child: Row(
            children: [
              // Canvas Workspace
              Expanded(
                child: Container(
                  color: Theme.of(context).colorScheme.surfaceContainerLowest,
                  child: InteractiveViewer(
                    constrained: false,
                    boundaryMargin: const EdgeInsets.all(200),
                    minScale: 0.5,
                    maxScale: 2.0,
                    child: Padding(
                      padding: const EdgeInsets.all(40.0),
                      child: _buildCanvas(context),
                    ),
                  ),
                ),
              ),
              const VerticalDivider(width: 1),
              // Inspector Sidebar
              SizedBox(
                width: 300,
                child: _buildInspector(context),
              ),
            ],
          ),
        ),
      ],
    );
  }

  Widget _buildDesignerToolbar(BuildContext context) {
    return Container(
      height: 48,
      padding: const EdgeInsets.symmetric(horizontal: 16.0),
      color: Theme.of(context).colorScheme.surface,
      child: Row(
        children: [
          const Icon(Icons.design_services, size: 20, color: Color(0xFF1E88E5)),
          const SizedBox(width: 8),
          Text(
            'Layout Mode: ${_layout.name}',
            style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
          ),
          const SizedBox(width: 16),
          const VerticalDivider(width: 1, indent: 8, endIndent: 8),
          const SizedBox(width: 8),
          // Tool items
          OutlinedButton.icon(
            icon: const Icon(Icons.text_fields, size: 14),
            label: const Text('Add Label', style: TextStyle(fontSize: 12)),
            onPressed: () => _addObject('label'),
          ),
          const SizedBox(width: 8),
          OutlinedButton.icon(
            icon: const Icon(Icons.input, size: 14),
            label: const Text('Add Field', style: TextStyle(fontSize: 12)),
            onPressed: () => _addObject('field'),
          ),
          const SizedBox(width: 8),
          OutlinedButton.icon(
            icon: const Icon(Icons.smart_button, size: 14),
            label: const Text('Add Button', style: TextStyle(fontSize: 12)),
            onPressed: () => _addObject('button'),
          ),
          const SizedBox(width: 8),
          OutlinedButton.icon(
            icon: const Icon(Icons.table_view, size: 14),
            label: const Text('Add Portal', style: TextStyle(fontSize: 12)),
            onPressed: () => _addObject('portal'),
          ),
          const Spacer(),
          // Grid snap toggle
          Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Text('Snap 8px', style: TextStyle(fontSize: 12)),
              Switch(
                value: _snapToGrid,
                onChanged: (val) => setState(() => _snapToGrid = val),
              ),
            ],
          ),
          const SizedBox(width: 12),
          FilledButton.icon(
            icon: _isSaving
                ? const SizedBox(width: 14, height: 14, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                : const Icon(Icons.save, size: 16),
            label: const Text('Save Layout', style: TextStyle(fontSize: 12)),
            onPressed: _isSaving ? null : _saveLayout,
          ),
        ],
      ),
    );
  }

  Widget _buildCanvas(BuildContext context) {
    double totalHeight = 0;
    for (var p in _layout.parts) {
      totalHeight += p.height;
    }
    if (totalHeight < 600) totalHeight = 600;

    return Container(
      width: _layout.width,
      height: totalHeight,
      decoration: BoxDecoration(
        color: Colors.white,
        border: Border.all(color: Colors.black26, width: 1),
        boxShadow: const [
          BoxShadow(color: Colors.black12, blurRadius: 10, offset: Offset(0, 4)),
        ],
      ),
      child: Stack(
        children: [
          // Background grid lines (8px)
          if (_snapToGrid)
            CustomPaint(
              size: Size(_layout.width, totalHeight),
              painter: GridPainter(),
            ),
          // Structural Parts (Header, Body, Footer)
          ..._buildParts(totalHeight),
          // Placed Layout Objects
          ..._layout.objects.map((obj) => _buildCanvasObject(context, obj)),
        ],
      ),
    );
  }

  List<Widget> _buildParts(double totalHeight) {
    final widgets = <Widget>[];
    double currentY = 0;

    for (var part in _layout.parts) {
      final partY = currentY;
      final partH = part.height;
      widgets.add(
        Positioned(
          left: 0,
          right: 0,
          top: partY,
          height: partH,
          child: Container(
            decoration: BoxDecoration(
              border: Border(bottom: BorderSide(color: Colors.blue.withOpacity(0.4), width: 1)),
            ),
            child: Align(
              alignment: Alignment.bottomLeft,
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                color: Colors.blue.withOpacity(0.12),
                child: Text(
                  part.type.toUpperCase(),
                  style: TextStyle(
                    fontSize: 9,
                    fontWeight: FontWeight.bold,
                    color: Colors.blue.shade800,
                  ),
                ),
              ),
            ),
          ),
        ),
      );
      currentY += partH;
    }
    return widgets;
  }

  Widget _buildCanvasObject(BuildContext context, LayoutObjectModel obj) {
    final isSelected = obj.id == _selectedObjectId;

    return Positioned(
      left: obj.x,
      top: obj.y,
      width: obj.width,
      height: obj.height,
      child: GestureDetector(
        onTap: () {
          setState(() => _selectedObjectId = obj.id);
        },
        onPanUpdate: (details) {
          setState(() {
            final newX = _snap(obj.x + details.delta.dx).clamp(0.0, _layout.width - obj.width);
            final newY = _snap(obj.y + details.delta.dy).clamp(0.0, 1200.0);
            final updatedObj = obj.copyWith(x: newX, y: newY);
            _layout = LayoutDefinitionModel(
              id: _layout.id,
              name: _layout.name,
              tableOccurrence: _layout.tableOccurrence,
              width: _layout.width,
              theme: _layout.theme,
              defaultView: _layout.defaultView,
              parts: _layout.parts,
              objects: _layout.objects.map((o) => o.id == obj.id ? updatedObj : o).toList(),
            );
          });
        },
        child: Container(
          decoration: BoxDecoration(
            border: isSelected
                ? Border.all(color: const Color(0xFF1E88E5), width: 2)
                : Border.all(color: Colors.grey.withOpacity(0.3)),
            borderRadius: BorderRadius.circular(obj.style.cornerRadius),
            color: _getObjectColor(obj),
          ),
          child: Stack(
            children: [
              _buildObjectPreview(obj),
              if (isSelected)
                Positioned(
                  right: 0,
                  bottom: 0,
                  child: Container(
                    width: 8,
                    height: 8,
                    color: const Color(0xFF1E88E5),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }

  Color _getObjectColor(LayoutObjectModel obj) {
    if (obj.style.fillColor != null && obj.style.fillColor!.startsWith('#')) {
      final hex = obj.style.fillColor!.replaceAll('#', '');
      if (hex.length == 6) {
        return Color(int.parse('0xFF$hex'));
      }
    }
    if (obj.type == 'portal') return const Color(0xFFF8F9FA);
    if (obj.type == 'button') return const Color(0xFF1E88E5);
    return Colors.white;
  }

  Widget _buildObjectPreview(LayoutObjectModel obj) {
    switch (obj.type) {
      case 'label':
        return Padding(
          padding: const EdgeInsets.symmetric(horizontal: 4.0),
          child: Align(
            alignment: _getAlignment(obj.style.textAlign),
            child: Text(
              obj.text,
              style: TextStyle(
                fontSize: obj.style.fontSize,
                fontWeight: obj.style.fontWeight == 'bold' ? FontWeight.bold : FontWeight.normal,
                color: Colors.black87,
              ),
              overflow: TextOverflow.ellipsis,
            ),
          ),
        );
      case 'field':
        return Container(
          padding: const EdgeInsets.symmetric(horizontal: 8.0, vertical: 4.0),
          decoration: BoxDecoration(
            border: Border.all(color: Colors.grey.shade400),
            borderRadius: BorderRadius.circular(4),
            color: Colors.white,
          ),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  '::${obj.fieldBinding?.fieldName ?? "field"}',
                  style: const TextStyle(fontSize: 12, color: Colors.blueGrey, fontStyle: FontStyle.italic),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              const Icon(Icons.tune, size: 12, color: Colors.grey),
            ],
          ),
        );
      case 'button':
        return Center(
          child: Text(
            obj.text,
            style: const TextStyle(fontSize: 12, color: Colors.white, fontWeight: FontWeight.bold),
            overflow: TextOverflow.ellipsis,
          ),
        );
      case 'portal':
        return Container(
          padding: const EdgeInsets.all(6),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  const Icon(Icons.table_rows, size: 14, color: Colors.indigo),
                  const SizedBox(width: 4),
                  Text(
                    obj.text,
                    style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: Colors.indigo),
                  ),
                ],
              ),
              const Divider(height: 8),
              const Expanded(
                child: Center(
                  child: Text('Drop related fields here (1:N Portal)', style: TextStyle(fontSize: 11, color: Colors.grey)),
                ),
              ),
            ],
          ),
        );
      default:
        return Center(child: Text(obj.type, style: const TextStyle(fontSize: 11)));
    }
  }

  Alignment _getAlignment(String align) {
    switch (align) {
      case 'right':
        return Alignment.centerRight;
      case 'center':
        return Alignment.center;
      default:
        return Alignment.centerLeft;
    }
  }

  Widget _buildInspector(BuildContext context) {
    final sel = _selectedObject;
    return Container(
      color: Theme.of(context).colorScheme.surface,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Container(
            padding: const EdgeInsets.all(12),
            color: Theme.of(context).colorScheme.surfaceContainerHighest.withOpacity(0.5),
            child: const Row(
              children: [
                Icon(Icons.tune, size: 16),
                SizedBox(width: 8),
                Text('Object Inspector', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
              ],
            ),
          ),
          if (sel == null)
            const Expanded(
              child: Center(
                child: Text('Select an element on the canvas to inspect its properties.',
                    textAlign: TextAlign.center, style: TextStyle(color: Colors.grey, fontSize: 12)),
              ),
            )
          else
            Expanded(
              child: ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  Text('Type: ${sel.type.toUpperCase()}', style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
                  const SizedBox(height: 12),
                  // Coordinates (X, Y, W, H)
                  Row(
                    children: [
                      Expanded(
                        child: TextField(
                          decoration: const InputDecoration(labelText: 'X (px)', isDense: true),
                          keyboardType: TextInputType.number,
                          controller: TextEditingController(text: sel.x.round().toString()),
                          onSubmitted: (val) {
                            final n = double.tryParse(val);
                            if (n != null) _updateSelectedObject(sel.copyWith(x: n));
                          },
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: TextField(
                          decoration: const InputDecoration(labelText: 'Y (px)', isDense: true),
                          keyboardType: TextInputType.number,
                          controller: TextEditingController(text: sel.y.round().toString()),
                          onSubmitted: (val) {
                            final n = double.tryParse(val);
                            if (n != null) _updateSelectedObject(sel.copyWith(y: n));
                          },
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 8),
                  Row(
                    children: [
                      Expanded(
                        child: TextField(
                          decoration: const InputDecoration(labelText: 'Width', isDense: true),
                          keyboardType: TextInputType.number,
                          controller: TextEditingController(text: sel.width.round().toString()),
                          onSubmitted: (val) {
                            final n = double.tryParse(val);
                            if (n != null) _updateSelectedObject(sel.copyWith(width: n));
                          },
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: TextField(
                          decoration: const InputDecoration(labelText: 'Height', isDense: true),
                          keyboardType: TextInputType.number,
                          controller: TextEditingController(text: sel.height.round().toString()),
                          onSubmitted: (val) {
                            final n = double.tryParse(val);
                            if (n != null) _updateSelectedObject(sel.copyWith(height: n));
                          },
                        ),
                      ),
                    ],
                  ),
                  const Divider(height: 24),
                  if (sel.type == 'label' || sel.type == 'button') ...[
                    TextField(
                      decoration: const InputDecoration(labelText: 'Text Content', isDense: true),
                      controller: TextEditingController(text: sel.text),
                      onChanged: (val) => _updateSelectedObject(sel.copyWith(text: val)),
                    ),
                    const SizedBox(height: 12),
                  ],
                  if (sel.type == 'field') ...[
                    DropdownButtonFormField<String>(
                      value: sel.fieldBinding?.fieldName,
                      decoration: const InputDecoration(labelText: 'Bound Column', isDense: true),
                      items: widget.table.columns.map((c) {
                        return DropdownMenuItem(value: c.name, child: Text('${c.displayName} (${c.fieldType})', style: const TextStyle(fontSize: 12)));
                      }).toList(),
                      onChanged: (newField) {
                        if (newField != null) {
                          _updateSelectedObject(sel.copyWith(
                            fieldBinding: FieldBindingModel(fieldName: newField),
                          ));
                        }
                      },
                    ),
                    const SizedBox(height: 12),
                  ],
                  // Delete Button
                  OutlinedButton.icon(
                    icon: const Icon(Icons.delete_outline, size: 16, color: Colors.red),
                    label: const Text('Delete Object', style: TextStyle(color: Colors.red, fontSize: 12)),
                    onPressed: _deleteSelectedObject,
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }

  void _updateSelectedObject(LayoutObjectModel updated) {
    setState(() {
      _layout = LayoutDefinitionModel(
        id: _layout.id,
        name: _layout.name,
        tableOccurrence: _layout.tableOccurrence,
        width: _layout.width,
        theme: _layout.theme,
        defaultView: _layout.defaultView,
        parts: _layout.parts,
        objects: _layout.objects.map((o) => o.id == updated.id ? updated : o).toList(),
      );
    });
  }
}

class GridPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = Colors.black.withOpacity(0.06)
      ..strokeWidth = 0.5;

    for (double x = 0; x < size.width; x += 16) {
      canvas.drawLine(Offset(x, 0), Offset(x, size.height), paint);
    }
    for (double y = 0; y < size.height; y += 16) {
      canvas.drawLine(Offset(0, y), Offset(size.width, y), paint);
    }
  }

  @override
  bool shouldRepaint(covariant CustomPainter oldDelegate) => false;
}
