import 'dart:math' as math;
import 'package:flutter/material.dart';
import '../../../core/api/api_client.dart';
import 'specify_relationship_dialog.dart';
import 'specify_table_dialog.dart';

class RelationshipGraphWidget extends StatefulWidget {
  final List<TableModel> tables;
  final List<TableOccurrenceModel> occurrences;
  final List<RelationshipModel> relationships;
  final ApiClient apiClient;
  final VoidCallback onSchemaChanged;

  const RelationshipGraphWidget({
    super.key,
    required this.tables,
    required this.occurrences,
    required this.relationships,
    required this.apiClient,
    required this.onSchemaChanged,
  });

  @override
  State<RelationshipGraphWidget> createState() => _RelationshipGraphWidgetState();
}

class _RelationshipGraphWidgetState extends State<RelationshipGraphWidget> {
  late List<TableOccurrenceModel> _occurrences;
  late List<RelationshipModel> _relationships;
  String? _selectedOccurrenceId;
  String? _selectedRelationshipId;
  final Set<String> _collapsedOccurrences = {};

  final TransformationController _transformController = TransformationController();
  static const double _cardWidth = 200.0;
  static const double _headerHeight = 32.0;
  static const double _rowHeight = 22.0;

  @override
  void initState() {
    super.initState();
    _occurrences = List.from(widget.occurrences);
    _relationships = List.from(widget.relationships);
  }

  @override
  void didUpdateWidget(covariant RelationshipGraphWidget oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.occurrences != oldWidget.occurrences) {
      _occurrences = List.from(widget.occurrences);
    }
    if (widget.relationships != oldWidget.relationships) {
      _relationships = List.from(widget.relationships);
    }
  }

  @override
  void dispose() {
    _transformController.dispose();
    super.dispose();
  }

  TableModel? _getTable(String baseTableId) {
    return widget.tables.where((t) => t.id == baseTableId).firstOrNull;
  }

  double _getCardHeight(TableOccurrenceModel occ) {
    if (_collapsedOccurrences.contains(occ.id)) {
      return _headerHeight;
    }
    final tbl = _getTable(occ.baseTableId);
    final count = tbl?.columns.length ?? 0;
    return _headerHeight + (count * _rowHeight) + 8.0;
  }

  Offset _getAnchorPoint(TableOccurrenceModel occ, String columnId, bool isExit) {
    final tbl = _getTable(occ.baseTableId);
    final cols = tbl?.columns ?? [];
    int colIndex = cols.indexWhere((c) => c.id == columnId);
    if (colIndex < 0) colIndex = 0;

    final isCollapsed = _collapsedOccurrences.contains(occ.id);
    final yOffset = isCollapsed ? (_headerHeight / 2) : (_headerHeight + (colIndex * _rowHeight) + (_rowHeight / 2));
    final xOffset = isExit ? _cardWidth : 0.0;

    return Offset(occ.xPos + xOffset, occ.yPos + yOffset);
  }

  Future<void> _showAddOccurrenceDialog() async {
    final res = await SpecifyTableDialog.show(
      context,
      tables: widget.tables,
      existingOccurrences: _occurrences,
    );
    if (res != null && mounted) {
      try {
        // Position new occurrence near center or offset from existing
        double newX = 100.0;
        double newY = 100.0;
        if (_occurrences.isNotEmpty) {
          final last = _occurrences.last;
          newX = last.xPos + 60.0;
          newY = last.yPos + 40.0;
        }

        final created = await widget.apiClient.createOccurrence(
          baseTableId: res['base_table_id'] as String,
          name: res['name'] as String,
          xPos: newX,
          yPos: newY,
        );

        setState(() {
          _occurrences.add(created);
          _selectedOccurrenceId = created.id;
          _selectedRelationshipId = null;
        });
        widget.onSchemaChanged();
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('Error al crear ocurrencia de tabla: $e'), backgroundColor: Colors.red),
          );
        }
      }
    }
  }

  Future<void> _showAddRelationshipDialog({
    String? leftOccId,
    String? rightOccId,
    String? leftColId,
    String? rightColId,
  }) async {
    if (_occurrences.length < 2 && leftOccId == null && rightOccId == null) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Debe existir al menos 2 ocurrencias para crear una relación.')),
      );
      return;
    }

    final res = await SpecifyRelationshipDialog.show(
      context,
      occurrences: _occurrences,
      tables: widget.tables,
      preselectedLeftOccurrenceId: leftOccId ?? _selectedOccurrenceId,
      preselectedRightOccurrenceId: rightOccId,
      preselectedLeftColumnId: leftColId,
      preselectedRightColumnId: rightColId,
    );

    if (res != null && mounted) {
      try {
        final rel = await widget.apiClient.createRelationship(
          name: res['name'] as String?,
          leftOccurrenceId: res['left_occurrence_id'] as String,
          leftColumnId: res['left_column_id'] as String,
          rightOccurrenceId: res['right_occurrence_id'] as String,
          rightColumnId: res['right_column_id'] as String,
          operator: res['operator'] as String? ?? '=',
          allowCreation: res['allow_creation'] as bool? ?? false,
          cascadeDelete: res['cascade_delete'] as bool? ?? false,
          sortRelated: res['sort_related'] as String?,
        );

        setState(() {
          _relationships.add(rel);
          _selectedRelationshipId = rel.id;
        });
        widget.onSchemaChanged();
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('Error al crear relación: $e'), backgroundColor: Colors.red),
          );
        }
      }
    }
  }

  Future<void> _showEditRelationshipDialog(RelationshipModel rel) async {
    final res = await SpecifyRelationshipDialog.show(
      context,
      occurrences: _occurrences,
      tables: widget.tables,
      initialRelationship: rel,
    );

    if (res != null && mounted) {
      if (res['delete'] == true) {
        await _deleteRelationship(rel.id);
        return;
      }

      try {
        final updated = await widget.apiClient.updateRelationship(
          rel.id,
          name: res['name'] as String?,
          operator: res['operator'] as String?,
          allowCreation: res['allow_creation'] as bool?,
          cascadeDelete: res['cascade_delete'] as bool?,
          sortRelated: res['sort_related'] as String?,
        );

        setState(() {
          final idx = _relationships.indexWhere((r) => r.id == rel.id);
          if (idx >= 0) {
            _relationships[idx] = updated;
          }
        });
        widget.onSchemaChanged();
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('Error al actualizar relación: $e'), backgroundColor: Colors.red),
          );
        }
      }
    }
  }

  Future<void> _deleteOccurrence(String occId) async {
    final occ = _occurrences.firstWhere((o) => o.id == occId);
    final confirm = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Eliminar ocurrencia de tabla'),
        content: Text('¿Seguro que desea eliminar la ocurrencia "${occ.name}" del gráfico? Se eliminarán también las relaciones conectadas a ella.'),
        actions: [
          OutlinedButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancelar')),
          ElevatedButton(
            onPressed: () => Navigator.of(ctx).pop(true),
            style: ElevatedButton.styleFrom(backgroundColor: Colors.red, foregroundColor: Colors.white),
            child: const Text('Eliminar'),
          ),
        ],
      ),
    );

    if (confirm == true && mounted) {
      try {
        await widget.apiClient.deleteOccurrence(occId);
        setState(() {
          _occurrences.removeWhere((o) => o.id == occId);
          _relationships.removeWhere((r) => r.leftOccurrenceId == occId || r.rightOccurrenceId == occId);
          if (_selectedOccurrenceId == occId) _selectedOccurrenceId = null;
        });
        widget.onSchemaChanged();
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('Error al eliminar ocurrencia: $e'), backgroundColor: Colors.red),
          );
        }
      }
    }
  }

  Future<void> _deleteRelationship(String relId) async {
    try {
      await widget.apiClient.deleteRelationship(relId);
      setState(() {
        _relationships.removeWhere((r) => r.id == relId);
        if (_selectedRelationshipId == relId) _selectedRelationshipId = null;
      });
      widget.onSchemaChanged();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Error al eliminar relación: $e'), backgroundColor: Colors.red),
        );
      }
    }
  }

  void _onOccurrenceDragged(TableOccurrenceModel occ, DragUpdateDetails details) {
    setState(() {
      final idx = _occurrences.indexWhere((o) => o.id == occ.id);
      if (idx >= 0) {
        _occurrences[idx] = occ.copyWith(
          xPos: math.max(10.0, occ.xPos + details.delta.dx),
          yPos: math.max(10.0, occ.yPos + details.delta.dy),
        );
      }
    });
  }

  Future<void> _onOccurrenceDragEnd(TableOccurrenceModel occ) async {
    final current = _occurrences.firstWhere((o) => o.id == occ.id);
    try {
      await widget.apiClient.updateOccurrence(current.id, xPos: current.xPos, yPos: current.yPos);
    } catch (_) {
      // Background sync, silently continue
    }
  }

  void _resetView() {
    _transformController.value = Matrix4.identity();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    return Column(
      children: [
        // Top Toolbar
        Container(
          height: 44,
          padding: const EdgeInsets.symmetric(horizontal: 12),
          decoration: BoxDecoration(
            color: isDark ? const Color(0xFF23252B) : const Color(0xFFF2F4F7),
            border: Border(bottom: BorderSide(color: theme.dividerColor)),
          ),
          child: Row(
            children: [
              // + Add Table Occurrence Button
              ElevatedButton.icon(
                onPressed: _showAddOccurrenceDialog,
                icon: const Icon(Icons.add, size: 16),
                label: const Text('Tabla...'),
                style: ElevatedButton.styleFrom(
                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                  textStyle: const TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600),
                ),
              ),
              const SizedBox(width: 8),

              // + Add Relationship Button
              OutlinedButton.icon(
                onPressed: _occurrences.length >= 2 ? () => _showAddRelationshipDialog() : null,
                icon: const Icon(Icons.add_link, size: 16),
                label: const Text('Relación...'),
                style: OutlinedButton.styleFrom(
                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                  textStyle: const TextStyle(fontSize: 12.5),
                ),
              ),
              const SizedBox(width: 8),

              // Delete Button
              IconButton(
                icon: const Icon(Icons.delete_outline, size: 18),
                tooltip: _selectedOccurrenceId != null
                    ? 'Eliminar ocurrencia seleccionada'
                    : (_selectedRelationshipId != null ? 'Eliminar relación seleccionada' : 'Eliminar selección'),
                onPressed: _selectedOccurrenceId != null
                    ? () => _deleteOccurrence(_selectedOccurrenceId!)
                    : (_selectedRelationshipId != null ? () => _deleteRelationship(_selectedRelationshipId!) : null),
              ),
              const SizedBox(width: 8),

              // Center / Reset View
              IconButton(
                icon: const Icon(Icons.center_focus_strong_outlined, size: 18),
                tooltip: 'Centrar vista',
                onPressed: _resetView,
              ),

              const Spacer(),

              // Information Chip
              Text(
                '${_occurrences.length} ocurrencias  •  ${_relationships.length} relaciones',
                style: TextStyle(fontSize: 12, color: theme.textTheme.bodySmall?.color ?? Colors.grey[600]),
              ),
            ],
          ),
        ),

        // Interactive Graph Canvas
        Expanded(
          child: GestureDetector(
            onTap: () {
              setState(() {
                _selectedOccurrenceId = null;
                _selectedRelationshipId = null;
              });
            },
            child: Container(
              color: isDark ? const Color(0xFF191B20) : const Color(0xFFF9FAFB),
              child: ClipRect(
                child: InteractiveViewer(
                  transformationController: _transformController,
                  boundaryMargin: const EdgeInsets.all(1000.0),
                  minScale: 0.2,
                  maxScale: 2.5,
                  child: SizedBox(
                    width: 3000,
                    height: 2000,
                    child: Stack(
                      clipBehavior: Clip.none,
                      children: [
                        // Background Grid
                        Positioned.fill(
                          child: CustomPaint(
                            painter: _GridBackgroundPainter(
                              dotColor: isDark ? Colors.white.withOpacity(0.04) : Colors.black.withOpacity(0.05),
                            ),
                          ),
                        ),

                        // Connector Lines Painter
                        Positioned.fill(
                          child: CustomPaint(
                            painter: _RelationshipLinesPainter(
                              occurrences: _occurrences,
                              relationships: _relationships,
                              tables: widget.tables,
                              selectedRelationshipId: _selectedRelationshipId,
                              collapsedOccurrences: _collapsedOccurrences,
                              isDark: isDark,
                            ),
                          ),
                        ),

                        // Interactive Operator Badges centered on relationship lines
                        ..._buildRelationshipBadges(context, isDark),

                        // Draggable Table Occurrence Cards
                        ..._occurrences.map((occ) => _buildOccurrenceCard(context, occ, isDark)),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }

  List<Widget> _buildRelationshipBadges(BuildContext context, bool isDark) {
    final widgets = <Widget>[];

    for (final rel in _relationships) {
      final leftOcc = _occurrences.where((o) => o.id == rel.leftOccurrenceId).firstOrNull;
      final rightOcc = _occurrences.where((o) => o.id == rel.rightOccurrenceId).firstOrNull;
      if (leftOcc == null || rightOcc == null) continue;

      final isLeftLeft = leftOcc.xPos < rightOcc.xPos;
      final p1 = _getAnchorPoint(leftOcc, rel.leftColumnId, isLeftLeft);
      final p2 = _getAnchorPoint(rightOcc, rel.rightColumnId, !isLeftLeft);

      final midX = (p1.dx + p2.dx) / 2;
      final midY = (p1.dy + p2.dy) / 2;
      final isSelected = _selectedRelationshipId == rel.id;

      widgets.add(
        Positioned(
          left: midX - 16,
          top: midY - 12,
          child: GestureDetector(
            onTap: () {
              setState(() {
                _selectedRelationshipId = rel.id;
                _selectedOccurrenceId = null;
              });
            },
            onDoubleTap: () => _showEditRelationshipDialog(rel),
            child: Container(
              width: 32,
              height: 24,
              decoration: BoxDecoration(
                color: isSelected
                    ? Colors.amber[100]
                    : (isDark ? const Color(0xFF2D3039) : Colors.white),
                border: Border.all(
                  color: isSelected ? Colors.amber[800]! : (isDark ? Colors.grey[700]! : Colors.grey[400]!),
                  width: isSelected ? 2 : 1,
                ),
                borderRadius: BorderRadius.circular(5),
                boxShadow: [
                  BoxShadow(
                    color: Colors.black.withOpacity(0.12),
                    blurRadius: 3,
                    offset: const Offset(0, 1),
                  ),
                ],
              ),
              child: Center(
                child: Text(
                  rel.operator,
                  style: TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.bold,
                    color: isSelected ? Colors.amber[900] : (isDark ? Colors.white : Colors.black87),
                  ),
                ),
              ),
            ),
          ),
        ),
      );
    }

    return widgets;
  }

  Widget _buildOccurrenceCard(BuildContext context, TableOccurrenceModel occ, bool isDark) {
    final isSelected = _selectedOccurrenceId == occ.id;
    final isCollapsed = _collapsedOccurrences.contains(occ.id);
    final tbl = _getTable(occ.baseTableId);
    final columns = tbl?.columns ?? [];

    return Positioned(
      left: occ.xPos,
      top: occ.yPos,
      width: _cardWidth,
      child: GestureDetector(
        onTap: () {
          setState(() {
            _selectedOccurrenceId = occ.id;
            _selectedRelationshipId = null;
          });
        },
        onDoubleTap: () async {
          final res = await SpecifyTableDialog.show(
            context,
            tables: widget.tables,
            existingOccurrences: _occurrences,
            initialOccurrence: occ,
          );
          if (res != null && mounted) {
            final updatedName = res['name'] as String;
            try {
              final updated = await widget.apiClient.updateOccurrence(occ.id, name: updatedName);
              setState(() {
                final idx = _occurrences.indexWhere((o) => o.id == occ.id);
                if (idx >= 0) _occurrences[idx] = updated;
              });
              widget.onSchemaChanged();
            } catch (_) {}
          }
        },
        onPanUpdate: (details) => _onOccurrenceDragged(occ, details),
        onPanEnd: (_) => _onOccurrenceDragEnd(occ),
        child: Container(
          decoration: BoxDecoration(
            color: isDark ? const Color(0xFF21242C) : Colors.white,
            borderRadius: BorderRadius.circular(6),
            border: Border.all(
              color: isSelected
                  ? const Color(0xFFFF9800) // Amber / gold highlight like screenshot
                  : (isDark ? const Color(0xFF383C48) : const Color(0xFFB0B5C0)),
              width: isSelected ? 2.0 : 1.0,
            ),
            boxShadow: isSelected
                ? [
                    // Vibrant amber halo seen around CLIENTES 2 in user reference screenshot
                    BoxShadow(
                      color: const Color(0xFFFFB300).withOpacity(0.75),
                      blurRadius: 10,
                      spreadRadius: 2,
                    ),
                  ]
                : [
                    BoxShadow(
                      color: Colors.black.withOpacity(isDark ? 0.3 : 0.08),
                      blurRadius: 4,
                      offset: const Offset(0, 2),
                    ),
                  ],
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              // Header bar
              Container(
                height: _headerHeight,
                padding: const EdgeInsets.symmetric(horizontal: 8),
                decoration: BoxDecoration(
                  color: isSelected
                      ? (isDark ? const Color(0xFF3D331A) : const Color(0xFFFFF3CD))
                      : (isDark ? const Color(0xFF2C303A) : const Color(0xFFE4E7EC)),
                  borderRadius: BorderRadius.vertical(
                    top: const Radius.circular(5),
                    bottom: isCollapsed ? const Radius.circular(5) : Radius.zero,
                  ),
                ),
                child: Row(
                  children: [
                    Icon(
                      Icons.table_view_outlined,
                      size: 15,
                      color: isSelected ? const Color(0xFFB25E00) : (isDark ? Colors.grey[400] : Colors.grey[700]),
                    ),
                    const SizedBox(width: 6),
                    Expanded(
                      child: Text(
                        occ.name,
                        style: TextStyle(
                          fontSize: 12,
                          fontWeight: FontWeight.bold,
                          color: isSelected
                              ? (isDark ? const Color(0xFFFFD54F) : const Color(0xFF7A4100))
                              : (isDark ? Colors.white : Colors.black87),
                        ),
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                    InkWell(
                      onTap: () {
                        setState(() {
                          if (isCollapsed) {
                            _collapsedOccurrences.remove(occ.id);
                          } else {
                            _collapsedOccurrences.add(occ.id);
                          }
                        });
                      },
                      child: Icon(
                        isCollapsed ? Icons.unfold_more : Icons.unfold_less,
                        size: 14,
                        color: isDark ? Colors.grey[400] : Colors.grey[600],
                      ),
                    ),
                  ],
                ),
              ),

              // Fields List
              if (!isCollapsed) ...[
                if (columns.isEmpty)
                  Padding(
                    padding: const EdgeInsets.all(8.0),
                    child: Text(
                      'Sin campos',
                      style: TextStyle(fontSize: 11, color: Colors.grey[500]),
                    ),
                  )
                else
                  ...columns.map((col) {
                    return Container(
                      height: _rowHeight,
                      padding: const EdgeInsets.symmetric(horizontal: 8),
                      child: Row(
                        children: [
                          Icon(
                            col.isPrimaryKey ? Icons.key : Icons.commit,
                            size: 11,
                            color: col.isPrimaryKey ? Colors.amber[700] : Colors.grey[500],
                          ),
                          const SizedBox(width: 4),
                          Expanded(
                            child: Text(
                              col.displayName.isNotEmpty ? col.displayName : col.name,
                              style: TextStyle(
                                fontSize: 11,
                                color: isDark ? Colors.grey[300] : Colors.grey[800],
                                fontWeight: col.isPrimaryKey ? FontWeight.w600 : FontWeight.normal,
                              ),
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                        ],
                      ),
                    );
                  }),
                const SizedBox(height: 4),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _RelationshipLinesPainter extends CustomPainter {
  final List<TableOccurrenceModel> occurrences;
  final List<RelationshipModel> relationships;
  final List<TableModel> tables;
  final String? selectedRelationshipId;
  final Set<String> collapsedOccurrences;
  final bool isDark;

  _RelationshipLinesPainter({
    required this.occurrences,
    required this.relationships,
    required this.tables,
    required this.selectedRelationshipId,
    required this.collapsedOccurrences,
    required this.isDark,
  });

  TableModel? _getTable(String baseTableId) {
    return tables.where((t) => t.id == baseTableId).firstOrNull;
  }

  Offset _getAnchorPoint(TableOccurrenceModel occ, String columnId, bool isExit) {
    final tbl = _getTable(occ.baseTableId);
    final cols = tbl?.columns ?? [];
    int colIndex = cols.indexWhere((c) => c.id == columnId);
    if (colIndex < 0) colIndex = 0;

    final isCollapsed = collapsedOccurrences.contains(occ.id);
    final yOffset = isCollapsed
        ? (_RelationshipGraphWidgetState._headerHeight / 2)
        : (_RelationshipGraphWidgetState._headerHeight + (colIndex * _RelationshipGraphWidgetState._rowHeight) + (_RelationshipGraphWidgetState._rowHeight / 2));
    final xOffset = isExit ? _RelationshipGraphWidgetState._cardWidth : 0.0;

    return Offset(occ.xPos + xOffset, occ.yPos + yOffset);
  }

  @override
  void paint(Canvas canvas, Size size) {
    for (final rel in relationships) {
      final leftOcc = occurrences.where((o) => o.id == rel.leftOccurrenceId).firstOrNull;
      final rightOcc = occurrences.where((o) => o.id == rel.rightOccurrenceId).firstOrNull;
      if (leftOcc == null || rightOcc == null) continue;

      final isLeftLeft = leftOcc.xPos < rightOcc.xPos;
      final p1 = _getAnchorPoint(leftOcc, rel.leftColumnId, isLeftLeft);
      final p2 = _getAnchorPoint(rightOcc, rel.rightColumnId, !isLeftLeft);

      final isSelected = selectedRelationshipId == rel.id;

      final paint = Paint()
        ..color = isSelected
            ? Colors.amber[800]!
            : (isDark ? const Color(0xFF707789) : const Color(0xFF6B7280))
        ..strokeWidth = isSelected ? 2.5 : 1.5
        ..style = PaintingStyle.stroke
        ..strokeCap = StrokeCap.round;

      // Draw smooth orthogonal curve between the two anchors
      final path = Path();
      path.moveTo(p1.dx, p1.dy);

      final dx = (p2.dx - p1.dx).abs();
      final controlPoint1 = Offset(p1.dx + (isLeftLeft ? dx * 0.4 : -dx * 0.4), p1.dy);
      final controlPoint2 = Offset(p2.dx + (isLeftLeft ? -dx * 0.4 : dx * 0.4), p2.dy);

      path.cubicTo(controlPoint1.dx, controlPoint1.dy, controlPoint2.dx, controlPoint2.dy, p2.dx, p2.dy);
      canvas.drawPath(path, paint);

      // Draw anchor dot at start and end
      final dotPaint = Paint()
        ..color = isSelected ? Colors.amber[800]! : (isDark ? Colors.grey[400]! : Colors.grey[700]!)
        ..style = PaintingStyle.fill;
      canvas.drawCircle(p1, 3.0, dotPaint);
      canvas.drawCircle(p2, 3.0, dotPaint);
    }
  }

  @override
  bool shouldRepaint(covariant _RelationshipLinesPainter oldDelegate) {
    return oldDelegate.occurrences != occurrences ||
        oldDelegate.relationships != relationships ||
        oldDelegate.selectedRelationshipId != selectedRelationshipId ||
        oldDelegate.collapsedOccurrences != collapsedOccurrences;
  }
}

class _GridBackgroundPainter extends CustomPainter {
  final Color dotColor;

  _GridBackgroundPainter({required this.dotColor});

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()..color = dotColor;
    const spacing = 20.0;
    const dotRadius = 1.0;

    for (double x = 0; x < size.width; x += spacing) {
      for (double y = 0; y < size.height; y += spacing) {
        canvas.drawCircle(Offset(x, y), dotRadius, paint);
      }
    }
  }

  @override
  bool shouldRepaint(covariant _GridBackgroundPainter oldDelegate) => false;
}
