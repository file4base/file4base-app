import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../../core/api/api_client.dart';
import '../../core/widgets/file4base_status_sidebar.dart';
import 'models/layout_definition.dart';

// ─── Public API to allow sidebar to inject active tool ───────────────────────

class LayoutDesignerWidget extends StatefulWidget {
  final TableModel table;
  final ApiClient apiClient;
  final LayoutDefinitionModel initialLayout;
  final VoidCallback onSaved;
  final VoidCallback? onAutoSaveDirty;
  final LayoutTool activeTool;

  const LayoutDesignerWidget({
    super.key,
    required this.table,
    required this.apiClient,
    required this.initialLayout,
    required this.onSaved,
    this.onAutoSaveDirty,
    this.activeTool = LayoutTool.pointer,
  });

  @override
  State<LayoutDesignerWidget> createState() => LayoutDesignerWidgetState();
}

class LayoutDesignerWidgetState extends State<LayoutDesignerWidget> {
  late LayoutDefinitionModel _layout;
  String? _selectedObjectId;
  bool _snapToGrid = true;
  bool _isSaving = false;
  // Track if layout was ever persisted (has a server ID)
  bool _isPersisted = false;

  // ─── Auto-save debounce ───────────────────────────────────────────────────
  Timer? _autoSaveTimer;
  _LayoutSaveStatus _autoSaveStatus = _LayoutSaveStatus.idle;
  static const _autoSaveDelay = Duration(milliseconds: 1500);

  /// Mark the layout as having unsaved changes and schedule a debounced save.
  void _markLayoutDirty() {
    _autoSaveTimer?.cancel();
    setState(() => _autoSaveStatus = _LayoutSaveStatus.dirty);
    _autoSaveTimer = Timer(_autoSaveDelay, _performAutoSave);
  }

  /// Silent background save to the API — does NOT show a success snackbar.
  Future<void> _performAutoSave() async {
    if (_isSaving) return; // explicit save in progress — skip
    setState(() => _autoSaveStatus = _LayoutSaveStatus.saving);
    try {
      final newName = _nameCtrl.text.trim().isEmpty ? _layout.name : _nameCtrl.text.trim();
      _layout = _layout.copyWith(name: newName);

      if (_isPersisted) {
        await widget.apiClient.updateLayout(
          _layout.id,
          _layout.name,
          _layout.toJson(),
        );
      } else {
        final created = await widget.apiClient.createLayout(
          _layout.name,
          toId: widget.table.id,
          definition: _layout.toJson(),
        );
        _layout = _layout.copyWith(id: created.id);
        _isPersisted = true;
      }

      if (mounted) {
        setState(() => _autoSaveStatus = _LayoutSaveStatus.saved);
        widget.onAutoSaveDirty?.call();
      }
    } catch (_) {
      if (mounted) setState(() => _autoSaveStatus = _LayoutSaveStatus.error);
    }
  }
  // ─────────────────────────────────────────────────────────────────────────

  // Name editing
  late TextEditingController _nameCtrl;
  bool _isEditingName = false;

  // Drag state for each object (local position tracking)
  final Map<String, Offset> _dragStart = {};
  final Map<String, Offset> _objStartPos = {};

  @override
  void initState() {
    super.initState();
    _layout = widget.initialLayout;
    _nameCtrl = TextEditingController(text: _layout.name);
    // If the ID looks like it came from the server (not a local timestamp), it's persisted
    _isPersisted = !_layout.id.startsWith('layout_');
  }

  @override
  void didUpdateWidget(covariant LayoutDesignerWidget old) {
    super.didUpdateWidget(old);
    if (old.initialLayout.id != widget.initialLayout.id) {
      _layout = widget.initialLayout;
      _nameCtrl.text = _layout.name;
      _selectedObjectId = null;
      _isPersisted = !_layout.id.startsWith('layout_');
    }
  }

  @override
  void dispose() {
    _autoSaveTimer?.cancel();
    _nameCtrl.dispose();
    super.dispose();
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

  // ─── Tool → Object type mapping ────────────────────────────────────────────

  String? _toolToObjectType(LayoutTool tool) {
    switch (tool) {
      case LayoutTool.text:
        return 'label';
      case LayoutTool.rectangle:
        return 'rect';
      case LayoutTool.roundedRect:
        return 'rounded_rect';
      case LayoutTool.oval:
        return 'oval';
      case LayoutTool.line:
        return 'line';
      case LayoutTool.field:
        return 'field';
      case LayoutTool.button:
        return 'button';
      case LayoutTool.portal:
        return 'portal';
      default:
        return null; // pointer, format, rotate → no placement
    }
  }

  void _placeObjectAt(Offset localPos) {
    final type = _toolToObjectType(widget.activeTool);
    if (type == null) return;

    final newId = 'obj_${DateTime.now().millisecondsSinceEpoch}';
    final x = _snap(localPos.dx - 60);
    final y = _snap(localPos.dy - 18);

    LayoutObjectModel newObj;
    switch (type) {
      case 'field':
        final availableCols = widget.table.columns
            .where((c) => !c.isPrimaryKey)
            .toList();
        final defaultCol =
            availableCols.isNotEmpty ? availableCols.first.name : 'field';
        newObj = LayoutObjectModel(
          id: newId, type: 'field', x: x, y: y, width: 240, height: 36,
          fieldBinding: FieldBindingModel(fieldName: defaultCol),
        );
        break;
      case 'button':
        newObj = LayoutObjectModel(
          id: newId, type: 'button', x: x, y: y, width: 140, height: 36,
          text: 'Button',
          style: const LayoutObjectStyle(
              fillColor: '#1E88E5', textColor: '#FFFFFF', cornerRadius: 6),
        );
        break;
      case 'portal':
        newObj = LayoutObjectModel(
          id: newId, type: 'portal', x: x, y: y, width: 480, height: 180,
          text: 'Portal (Related Records)',
          style: const LayoutObjectStyle(
              fillColor: '#F5F5F7', borderColor: '#B0BEC5'),
        );
        break;
      case 'rect':
        newObj = LayoutObjectModel(
          id: newId, type: 'rect', x: x, y: y, width: 120, height: 60,
          style: const LayoutObjectStyle(borderColor: '#555555'),
        );
        break;
      case 'rounded_rect':
        newObj = LayoutObjectModel(
          id: newId, type: 'rounded_rect', x: x, y: y, width: 120, height: 60,
          style: const LayoutObjectStyle(borderColor: '#555555', cornerRadius: 12),
        );
        break;
      case 'oval':
        newObj = LayoutObjectModel(
          id: newId, type: 'oval', x: x, y: y, width: 100, height: 60,
          style: const LayoutObjectStyle(borderColor: '#555555'),
        );
        break;
      case 'line':
        newObj = LayoutObjectModel(
          id: newId, type: 'line', x: x, y: y, width: 120, height: 2,
          style: const LayoutObjectStyle(borderColor: '#333333', borderWidth: 2),
        );
        break;
      default:
        newObj = LayoutObjectModel(
          id: newId, type: 'label', x: x, y: y, width: 160, height: 28,
          text: 'New Label',
          style: const LayoutObjectStyle(fontSize: 14, fontWeight: 'bold'),
        );
    }

    setState(() {
      _layout = _layout.copyWith(objects: [..._layout.objects, newObj]);
      _selectedObjectId = newId;
    });
    _markLayoutDirty();
  }

  void _deleteSelectedObject() {
    if (_selectedObjectId == null) return;
    setState(() {
      _layout = _layout.copyWith(
        objects: _layout.objects.where((o) => o.id != _selectedObjectId).toList(),
      );
      _selectedObjectId = null;
    });
    _markLayoutDirty();
  }

  // ─── Save: Create (POST) first time, Update (PUT) thereafter ───────────────
  Future<void> saveLayout() async {
    // Commit name from the text field
    final newName = _nameCtrl.text.trim().isEmpty ? _layout.name : _nameCtrl.text.trim();
    _layout = _layout.copyWith(name: newName);

    setState(() => _isSaving = true);
    try {
      if (_isPersisted) {
        // Already exists on server → PUT
        await widget.apiClient.updateLayout(
          _layout.id,
          _layout.name,
          _layout.toJson(),
        );
      } else {
        // First save → POST
        final created = await widget.apiClient.createLayout(
          _layout.name,
          toId: widget.table.id,
          definition: _layout.toJson(),
        );
        // Update local id so next save uses PUT
        _layout = _layout.copyWith(id: created.id);
        _isPersisted = true;
      }
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Layout "${_layout.name}" saved successfully!'),
            backgroundColor: Colors.green.shade700,
          ),
        );
        widget.onSaved();
        // Notify parent to trigger auto-save of the .f4p solution file
        widget.onAutoSaveDirty?.call();
        if (mounted) setState(() => _autoSaveStatus = _LayoutSaveStatus.saved);
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Failed to save layout: $e'),
            backgroundColor: Colors.red,
          ),
        );
      }
    } finally {
      if (mounted) setState(() => _isSaving = false);
    }
  }

  // ─── Build ──────────────────────────────────────────────────────────────────

  @override
  Widget build(BuildContext context) {
    return KeyboardListener(
      focusNode: FocusNode(),
      autofocus: true,
      onKeyEvent: (event) {
        if (event is KeyDownEvent &&
            event.logicalKey == LogicalKeyboardKey.delete &&
            _selectedObjectId != null) {
          _deleteSelectedObject();
        }
      },
      child: Column(
        children: [
          _buildToolbar(context),
          const Divider(height: 1),
          Expanded(
            child: Row(
              children: [
                // Canvas
                Expanded(child: _buildCanvasArea(context)),
                const VerticalDivider(width: 1),
                // Inspector panel
                SizedBox(width: 280, child: _buildInspector(context)),
              ],
            ),
          ),
        ],
      ),
    );
  }

  // ─── Toolbar ────────────────────────────────────────────────────────────────

  Widget _buildToolbar(BuildContext context) {
    final isDark = Theme.of(context).brightness == Brightness.dark;
    return Container(
      height: 44,
      padding: const EdgeInsets.symmetric(horizontal: 12),
      color: Theme.of(context).colorScheme.surface,
      child: Row(
        children: [
          const Icon(Icons.design_services, size: 18, color: Color(0xFF1E88E5)),
          const SizedBox(width: 8),
          // Editable layout name
          _isEditingName
              ? SizedBox(
                  width: 180,
                  child: TextField(
                    controller: _nameCtrl,
                    autofocus: true,
                    style: const TextStyle(fontSize: 13, fontWeight: FontWeight.bold),
                    decoration: const InputDecoration(
                      isDense: true,
                      contentPadding:
                          EdgeInsets.symmetric(horizontal: 4, vertical: 4),
                      border: OutlineInputBorder(),
                    ),
                    onSubmitted: (_) => setState(() => _isEditingName = false),
                    onEditingComplete: () =>
                        setState(() => _isEditingName = false),
                  ),
                )
              : GestureDetector(
                  onDoubleTap: () => setState(() => _isEditingName = true),
                  child: Tooltip(
                    message: 'Double-click to rename',
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          _nameCtrl.text,
                          style: const TextStyle(
                              fontWeight: FontWeight.bold, fontSize: 13),
                        ),
                        const SizedBox(width: 4),
                        Icon(Icons.edit,
                            size: 12,
                            color: isDark
                                ? Colors.white38
                                : Colors.black38),
                      ],
                    ),
                  ),
                ),

          const SizedBox(width: 12),
          const VerticalDivider(width: 1, indent: 8, endIndent: 8),
          const SizedBox(width: 8),

          // Quick add buttons (also reachable from sidebar tools)
          _toolbarBtn(Icons.text_fields, 'Label', () => _addObject('label')),
          _toolbarBtn(Icons.input, 'Field', () => _addObject('field')),
          _toolbarBtn(Icons.smart_button, 'Button', () => _addObject('button')),
          _toolbarBtn(Icons.table_view, 'Portal', () => _addObject('portal')),

          const Spacer(),

          // Snap toggle
          const Text('Snap 8px', style: TextStyle(fontSize: 11)),
          const SizedBox(width: 4),
          Switch(
            value: _snapToGrid,
            onChanged: (val) => setState(() => _snapToGrid = val),
            materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
          ),
          const SizedBox(width: 12),

          // Auto-save status dot
          _buildAutoSaveIndicator(),
          const SizedBox(width: 12),

          // Save button (explicit)
          FilledButton.icon(
            icon: _isSaving
                ? const SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(
                        strokeWidth: 2, color: Colors.white))
                : const Icon(Icons.save, size: 16),
            label: Text(_isPersisted ? 'Update Layout' : 'Save Layout',
                style: const TextStyle(fontSize: 12)),
            onPressed: _isSaving ? null : saveLayout,
          ),
        ],
      ),
    );
  }

  Widget _toolbarBtn(IconData icon, String label, VoidCallback onPressed) {
    return Padding(
      padding: const EdgeInsets.only(right: 6),
      child: OutlinedButton.icon(
        icon: Icon(icon, size: 13),
        label: Text(label, style: const TextStyle(fontSize: 11)),
        style: OutlinedButton.styleFrom(
          visualDensity: VisualDensity.compact,
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        ),
        onPressed: onPressed,
      ),
    );
  }

  // ─── Canvas Area ─────────────────────────────────────────────────────────────

  Widget _buildCanvasArea(BuildContext context) {
    final cursorForTool = widget.activeTool == LayoutTool.pointer
        ? SystemMouseCursors.basic
        : SystemMouseCursors.precise;

    return MouseRegion(
      cursor: cursorForTool,
      child: Container(
        color: Theme.of(context).colorScheme.surfaceContainerLowest,
        child: InteractiveViewer(
          constrained: false,
          boundaryMargin: const EdgeInsets.all(200),
          minScale: 0.3,
          maxScale: 3.0,
          // Disable panning when a placement tool is active
          panEnabled: widget.activeTool == LayoutTool.pointer,
          child: Padding(
            padding: const EdgeInsets.all(40.0),
            child: _buildCanvas(context),
          ),
        ),
      ),
    );
  }

  Widget _buildCanvas(BuildContext context) {
    double totalHeight = 0;
    for (var p in _layout.parts) {
      totalHeight += p.height;
    }
    if (totalHeight < 600) totalHeight = 600;

    return GestureDetector(
      onTapDown: (details) {
        // If placement tool active, place object at click position
        if (widget.activeTool != LayoutTool.pointer &&
            widget.activeTool != LayoutTool.format &&
            widget.activeTool != LayoutTool.rotate) {
          _placeObjectAt(details.localPosition);
        } else {
          // Deselect on background click
          setState(() => _selectedObjectId = null);
        }
      },
      child: Container(
        width: _layout.width,
        height: totalHeight,
        decoration: BoxDecoration(
          color: Colors.white,
          border: Border.all(color: Colors.black26),
          boxShadow: const [
            BoxShadow(color: Colors.black12, blurRadius: 10, offset: Offset(0, 4)),
          ],
        ),
        child: Stack(
          clipBehavior: Clip.none,
          children: [
            if (_snapToGrid)
              CustomPaint(
                size: Size(_layout.width, totalHeight),
                painter: _GridPainter(),
              ),
            // Structural parts (Header/Body/Footer bands)
            ..._buildPartBands(totalHeight),
            // Layout objects
            ..._layout.objects.map((obj) => _buildObject(context, obj)),
          ],
        ),
      ),
    );
  }

  List<Widget> _buildPartBands(double totalHeight) {
    final widgets = <Widget>[];
    double currentY = 0;
    for (var part in _layout.parts) {
      final y = currentY;
      widgets.add(
        Positioned(
          left: 0, right: 0, top: y, height: part.height,
          child: Container(
            decoration: BoxDecoration(
              border: Border(
                bottom: BorderSide(
                    color: Colors.blue.withValues(alpha: 0.4), width: 1),
              ),
            ),
            child: Align(
              alignment: Alignment.bottomLeft,
              child: Container(
                padding:
                    const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                color: Colors.blue.withValues(alpha: 0.1),
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
      currentY += part.height;
    }
    return widgets;
  }

  // ─── Draggable Canvas Object ──────────────────────────────────────────────────

  Widget _buildObject(BuildContext context, LayoutObjectModel obj) {
    final isSelected = obj.id == _selectedObjectId;

    return Positioned(
      left: obj.x,
      top: obj.y,
      width: obj.width,
      height: obj.height,
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTap: () => setState(() => _selectedObjectId = obj.id),

        // ── Precise drag: track start position of both finger and object ──
        onPanStart: (details) {
          if (widget.activeTool != LayoutTool.pointer) return;
          _dragStart[obj.id] = details.globalPosition;
          _objStartPos[obj.id] = Offset(obj.x, obj.y);
          setState(() => _selectedObjectId = obj.id);
        },
        onPanUpdate: (details) {
          if (widget.activeTool != LayoutTool.pointer) return;
          final start = _dragStart[obj.id];
          final startPos = _objStartPos[obj.id];
          if (start == null || startPos == null) return;
          final delta = details.globalPosition - start;
          final newX = _snap((startPos.dx + delta.dx)
              .clamp(0.0, _layout.width - obj.width));
          final newY = _snap((startPos.dy + delta.dy).clamp(0.0, 1200.0));
          setState(() {
            _layout = _layout.copyWith(
              objects: _layout.objects
                  .map((o) =>
                      o.id == obj.id ? o.copyWith(x: newX, y: newY) : o)
                  .toList(),
            );
          });
        },
        onPanEnd: (_) {
          _dragStart.remove(obj.id);
          _objStartPos.remove(obj.id);
          // Position changed — auto-save
          _markLayoutDirty();
        },

        child: Stack(
          clipBehavior: Clip.none,
          children: [
            // Object body
            Positioned.fill(
              child: Container(
                decoration: _objectDecoration(obj, isSelected),
                child: _buildObjectContent(obj),
              ),
            ),
            // Selection handles (corners)
            if (isSelected) ..._buildSelectionHandles(obj),
          ],
        ),
      ),
    );
  }

  BoxDecoration _objectDecoration(LayoutObjectModel obj, bool isSelected) {
    Color bg = Colors.white;
    if (obj.style.fillColor != null &&
        obj.style.fillColor!.startsWith('#') &&
        obj.style.fillColor!.length >= 7) {
      final hex = obj.style.fillColor!.replaceAll('#', '');
      bg = Color(int.parse('0xFF$hex'));
    } else if (obj.type == 'button') {
      bg = const Color(0xFF1E88E5);
    } else if (obj.type == 'portal') {
      bg = const Color(0xFFF8F9FA);
    } else if (obj.type == 'oval' ||
        obj.type == 'rect' ||
        obj.type == 'rounded_rect') {
      bg = Colors.transparent;
    }

    if (obj.type == 'oval') {
      return BoxDecoration(
        shape: BoxShape.circle,
        color: bg,
        border: isSelected
            ? Border.all(color: const Color(0xFF1E88E5), width: 2)
            : Border.all(color: _parseBorderColor(obj), width: obj.style.borderWidth),
      );
    }

    return BoxDecoration(
      color: bg,
      borderRadius: BorderRadius.circular(obj.style.cornerRadius),
      border: isSelected
          ? Border.all(color: const Color(0xFF1E88E5), width: 2)
          : Border.all(
              color: _parseBorderColor(obj),
              width: obj.style.borderWidth,
            ),
    );
  }

  Color _parseBorderColor(LayoutObjectModel obj) {
    final bc = obj.style.borderColor;
    if (bc != null && bc.startsWith('#') && bc.length >= 7) {
      return Color(int.parse('0xFF${bc.replaceAll('#', '')}'));
    }
    return Colors.grey.withValues(alpha: 0.4);
  }

  List<Widget> _buildSelectionHandles(LayoutObjectModel obj) {
    const handleSize = 7.0;
    const half = handleSize / 2;
    final handleDec = BoxDecoration(
      color: const Color(0xFF1E88E5),
      border: Border.all(color: Colors.white, width: 1),
    );

    return [
      Positioned(left: -half, top: -half,
          child: Container(width: handleSize, height: handleSize, decoration: handleDec)),
      Positioned(right: -half, top: -half,
          child: Container(width: handleSize, height: handleSize, decoration: handleDec)),
      Positioned(left: -half, bottom: -half,
          child: Container(width: handleSize, height: handleSize, decoration: handleDec)),
      Positioned(right: -half, bottom: -half,
          child: Container(width: handleSize, height: handleSize, decoration: handleDec)),
    ];
  }

  Widget _buildObjectContent(LayoutObjectModel obj) {
    switch (obj.type) {
      case 'label':
        return Padding(
          padding: const EdgeInsets.symmetric(horizontal: 4),
          child: Align(
            alignment: _getAlignment(obj.style.textAlign),
            child: Text(
              obj.text.isEmpty ? '(Label)' : obj.text,
              style: TextStyle(
                fontSize: obj.style.fontSize,
                fontWeight: obj.style.fontWeight == 'bold'
                    ? FontWeight.bold
                    : FontWeight.normal,
              ),
              overflow: TextOverflow.ellipsis,
            ),
          ),
        );

      case 'field':
        return Container(
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
          decoration: BoxDecoration(
            border: Border.all(color: Colors.grey.shade400),
            borderRadius: BorderRadius.circular(3),
            color: Colors.white,
          ),
          child: Text(
            ':: ${obj.fieldBinding?.fieldName ?? "field"}',
            style: const TextStyle(
                fontSize: 12, color: Colors.blueGrey, fontStyle: FontStyle.italic),
            overflow: TextOverflow.ellipsis,
          ),
        );

      case 'button':
        return Center(
          child: Text(
            obj.text.isEmpty ? 'Button' : obj.text,
            style: const TextStyle(
                fontSize: 12, color: Colors.white, fontWeight: FontWeight.bold),
            overflow: TextOverflow.ellipsis,
          ),
        );

      case 'portal':
        return Padding(
          padding: const EdgeInsets.all(6),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(children: [
                const Icon(Icons.table_rows, size: 13, color: Colors.indigo),
                const SizedBox(width: 4),
                Text(obj.text,
                    style: const TextStyle(
                        fontSize: 11,
                        fontWeight: FontWeight.bold,
                        color: Colors.indigo)),
              ]),
              const Divider(height: 8),
              const Expanded(
                child: Center(
                  child: Text('Related records portal',
                      style: TextStyle(fontSize: 10, color: Colors.grey)),
                ),
              ),
            ],
          ),
        );

      case 'line':
        return CustomPaint(
          painter: _LinePainter(color: _parseBorderColor(obj)),
        );

      case 'rect':
      case 'rounded_rect':
      case 'oval':
        return const SizedBox.shrink();

      default:
        return Center(
            child: Text(obj.type, style: const TextStyle(fontSize: 10)));
    }
  }

  Alignment _getAlignment(String align) => switch (align) {
        'right' => Alignment.centerRight,
        'center' => Alignment.center,
        _ => Alignment.centerLeft,
      };

  // ─── Inspector Sidebar ────────────────────────────────────────────────────────

  Widget _buildInspector(BuildContext context) {
    final sel = _selectedObject;
    return Container(
      color: Theme.of(context).colorScheme.surface,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Container(
            padding: const EdgeInsets.all(10),
            color: Theme.of(context)
                .colorScheme
                .surfaceContainerHighest
                .withValues(alpha: 0.5),
            child: const Row(children: [
              Icon(Icons.tune, size: 15),
              SizedBox(width: 6),
              Text('Inspector',
                  style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
            ]),
          ),
          if (sel == null)
            const Expanded(
              child: Center(
                child: Padding(
                  padding: EdgeInsets.all(16),
                  child: Text(
                    'Select an element on the canvas to edit its properties.\n\nTip: Double-click the layout name in the toolbar to rename it.',
                    textAlign: TextAlign.center,
                    style: TextStyle(color: Colors.grey, fontSize: 11),
                  ),
                ),
              ),
            )
          else
            Expanded(
              child: ListView(
                padding: const EdgeInsets.all(12),
                children: [
                  // Type badge
                  Container(
                    padding:
                        const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                    decoration: BoxDecoration(
                      color: const Color(0xFF1E88E5).withValues(alpha: 0.1),
                      borderRadius: BorderRadius.circular(4),
                    ),
                    child: Text(
                      sel.type.toUpperCase(),
                      style: const TextStyle(
                          fontWeight: FontWeight.bold,
                          fontSize: 11,
                          color: Color(0xFF1E88E5)),
                    ),
                  ),
                  const SizedBox(height: 12),

                  // Position & Size
                  _inspectorLabel('Position & Size'),
                  Row(children: [
                    _numField('X', sel.x, (v) =>
                        _updateSelected(sel.copyWith(x: v))),
                    const SizedBox(width: 8),
                    _numField('Y', sel.y, (v) =>
                        _updateSelected(sel.copyWith(y: v))),
                  ]),
                  const SizedBox(height: 6),
                  Row(children: [
                    _numField('W', sel.width, (v) =>
                        _updateSelected(sel.copyWith(width: v.clamp(10, 2000)))),
                    const SizedBox(width: 8),
                    _numField('H', sel.height, (v) =>
                        _updateSelected(sel.copyWith(height: v.clamp(4, 2000)))),
                  ]),

                  if (sel.type == 'label' || sel.type == 'button') ...[
                    const Divider(height: 20),
                    _inspectorLabel('Content'),
                    TextField(
                      decoration: const InputDecoration(
                          labelText: 'Text', isDense: true),
                      controller:
                          TextEditingController(text: sel.text),
                      onChanged: (v) =>
                          _updateSelected(sel.copyWith(text: v)),
                    ),
                    const SizedBox(height: 8),
                    _inspectorLabel('Font size'),
                    Slider(
                      value: sel.style.fontSize.clamp(8.0, 48.0),
                      min: 8,
                      max: 48,
                      divisions: 40,
                      label: sel.style.fontSize.round().toString(),
                      onChanged: (v) => _updateSelected(sel.copyWith(
                          style: LayoutObjectStyle(
                        fillColor: sel.style.fillColor,
                        borderColor: sel.style.borderColor,
                        borderWidth: sel.style.borderWidth,
                        cornerRadius: sel.style.cornerRadius,
                        fontSize: v,
                        fontWeight: sel.style.fontWeight,
                        textColor: sel.style.textColor,
                        textAlign: sel.style.textAlign,
                      ))),
                    ),
                  ],

                  if (sel.type == 'field') ...[
                    const Divider(height: 20),
                    _inspectorLabel('Bound Column'),
                    DropdownButtonFormField<String>(
                      value: sel.fieldBinding?.fieldName,
                      decoration: const InputDecoration(isDense: true),
                      items: widget.table.columns.map((c) {
                        return DropdownMenuItem(
                            value: c.name,
                            child: Text('${c.displayName} (${c.fieldType})',
                                style: const TextStyle(fontSize: 11)));
                      }).toList(),
                      onChanged: (f) {
                        if (f != null) {
                          _updateSelected(sel.copyWith(
                              fieldBinding: FieldBindingModel(fieldName: f)));
                        }
                      },
                    ),
                  ],

                  const Divider(height: 24),
                  OutlinedButton.icon(
                    icon: const Icon(Icons.delete_outline,
                        size: 15, color: Colors.red),
                    label: const Text('Delete',
                        style: TextStyle(color: Colors.red, fontSize: 12)),
                    onPressed: _deleteSelectedObject,
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }

  Widget _inspectorLabel(String text) => Padding(
        padding: const EdgeInsets.only(bottom: 4),
        child: Text(text,
            style: const TextStyle(
                fontSize: 10,
                fontWeight: FontWeight.bold,
                color: Colors.grey)),
      );

  Widget _numField(String label, double value, ValueChanged<double> onChanged) {
    return Expanded(
      child: TextField(
        decoration: InputDecoration(labelText: label, isDense: true),
        keyboardType: TextInputType.number,
        controller: TextEditingController(text: value.round().toString()),
        onSubmitted: (val) {
          final n = double.tryParse(val);
          if (n != null) onChanged(n);
        },
      ),
    );
  }

  // ─── Helper Mutations ────────────────────────────────────────────────────────

  void _addObject(String type) {
    final newId = 'obj_${DateTime.now().millisecondsSinceEpoch}';
    LayoutObjectModel newObj;
    switch (type) {
      case 'field':
        final col = widget.table.columns
            .where((c) => !c.isPrimaryKey)
            .firstOrNull;
        newObj = LayoutObjectModel(
          id: newId, type: 'field', x: _snap(120), y: _snap(120),
          width: 240, height: 36,
          fieldBinding: FieldBindingModel(fieldName: col?.name ?? 'field'),
        );
        break;
      case 'button':
        newObj = LayoutObjectModel(
          id: newId, type: 'button', x: _snap(120), y: _snap(120),
          width: 140, height: 36, text: 'Button',
          style: const LayoutObjectStyle(
              fillColor: '#1E88E5', textColor: '#FFFFFF', cornerRadius: 6),
        );
        break;
      case 'portal':
        newObj = LayoutObjectModel(
          id: newId, type: 'portal', x: _snap(80), y: _snap(160),
          width: 480, height: 180, text: 'Portal',
          style: const LayoutObjectStyle(
              fillColor: '#F5F5F7', borderColor: '#B0BEC5'),
        );
        break;
      default:
        newObj = LayoutObjectModel(
          id: newId, type: 'label', x: _snap(120), y: _snap(120),
          width: 160, height: 28, text: 'New Label',
          style: const LayoutObjectStyle(fontSize: 14, fontWeight: 'bold'),
        );
    }
    setState(() {
      _layout = _layout.copyWith(objects: [..._layout.objects, newObj]);
      _selectedObjectId = newId;
    });
    _markLayoutDirty();
  }

  void _updateSelected(LayoutObjectModel updated) {
    setState(() {
      _layout = _layout.copyWith(
        objects: _layout.objects
            .map((o) => o.id == updated.id ? updated : o)
            .toList(),
      );
    });
    _markLayoutDirty();
  }

  /// Toolbar widget showing auto-save status.
  Widget _buildAutoSaveIndicator() {
    final (color, icon, tip) = switch (_autoSaveStatus) {
      _LayoutSaveStatus.saving => (Colors.blue,   Icons.sync,                 'Saving layout...'),
      _LayoutSaveStatus.saved  => (Colors.green,  Icons.cloud_done_outlined,  'Layout auto-saved'),
      _LayoutSaveStatus.dirty  => (Colors.orange, Icons.edit_note_outlined,   'Unsaved changes'),
      _LayoutSaveStatus.error  => (Colors.red,    Icons.cloud_off_outlined,   'Auto-save failed — click Save Layout'),
      _LayoutSaveStatus.idle   => (Colors.grey,   Icons.cloud_done_outlined,  'Layout saved'),
    };
    return Tooltip(
      message: tip,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (_autoSaveStatus == _LayoutSaveStatus.saving)
            const SizedBox(
              width: 12, height: 12,
              child: CircularProgressIndicator(strokeWidth: 1.5, color: Colors.blue),
            )
          else
            Icon(icon, size: 14, color: color),
          const SizedBox(width: 4),
          Text(
            switch (_autoSaveStatus) {
              _LayoutSaveStatus.saving => 'Saving...',
              _LayoutSaveStatus.saved  => 'Saved',
              _LayoutSaveStatus.dirty  => 'Unsaved',
              _LayoutSaveStatus.error  => 'Error',
              _LayoutSaveStatus.idle   => 'Auto-save',
            },
            style: TextStyle(fontSize: 10, color: color, fontWeight: FontWeight.bold),
          ),
        ],
      ),
    );
  }
}

// ─── Auto-save status enum ────────────────────────────────────────────────────
enum _LayoutSaveStatus { idle, dirty, saving, saved, error }

// ─── Painters ─────────────────────────────────────────────────────────────────

class _GridPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = Colors.black.withValues(alpha: 0.05)
      ..strokeWidth = 0.5;
    for (double x = 0; x < size.width; x += 16) {
      canvas.drawLine(Offset(x, 0), Offset(x, size.height), paint);
    }
    for (double y = 0; y < size.height; y += 16) {
      canvas.drawLine(Offset(0, y), Offset(size.width, y), paint);
    }
  }

  @override
  bool shouldRepaint(covariant CustomPainter _) => false;
}

class _LinePainter extends CustomPainter {
  final Color color;
  const _LinePainter({required this.color});

  @override
  void paint(Canvas canvas, Size size) {
    canvas.drawLine(
      Offset(0, size.height / 2),
      Offset(size.width, size.height / 2),
      Paint()
        ..color = color
        ..strokeWidth = 2,
    );
  }

  @override
  bool shouldRepaint(covariant _LinePainter old) => old.color != color;
}
