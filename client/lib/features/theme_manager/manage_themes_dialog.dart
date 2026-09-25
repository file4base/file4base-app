import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/theme/theme_model.dart';
import '../../core/theme/theme_provider.dart';

class ManageThemesDialog extends ConsumerStatefulWidget {
  const ManageThemesDialog({super.key});

  static Future<void> show(BuildContext context) {
    return showDialog(
      context: context,
      barrierDismissible: true,
      builder: (ctx) => const ManageThemesDialog(),
    );
  }

  @override
  ConsumerState<ManageThemesDialog> createState() => _ManageThemesDialogState();
}

class _ManageThemesDialogState extends ConsumerState<ManageThemesDialog> {
  late AppThemeDefinition _selectedTheme;
  String _searchFilter = '';

  @override
  void initState() {
    super.initState();
    _selectedTheme = ref.read(appThemeProvider);
  }

  List<AppThemeDefinition> get _filteredThemes {
    if (_searchFilter.isEmpty) {
      return AppThemes.allThemes;
    }
    final q = _searchFilter.toLowerCase();
    return AppThemes.allThemes.where((t) {
      return t.name.toLowerCase().contains(q) ||
          t.description.toLowerCase().contains(q) ||
          t.category.toLowerCase().contains(q);
    }).toList();
  }

  void _applyTheme(AppThemeDefinition theme) {
    ref.read(appThemeProvider.notifier).setTheme(theme.id);
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Row(
          children: [
            const Icon(Icons.check_circle, color: Colors.white, size: 20),
            const SizedBox(width: 8),
            Text('Tema aplicado: ${theme.name}'),
          ],
        ),
        backgroundColor: theme.primaryAccent,
        duration: const Duration(seconds: 2),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final activeTheme = ref.watch(appThemeProvider);
    final isPreviewActive = activeTheme.id == _selectedTheme.id;

    return Dialog(
      backgroundColor: Colors.transparent,
      child: Container(
        width: 1040,
        height: 680,
        decoration: BoxDecoration(
          color: activeTheme.surface,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: activeTheme.border),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: activeTheme.isDark ? 0.6 : 0.2),
              blurRadius: 36,
              offset: const Offset(0, 16),
            ),
          ],
        ),
        child: Column(
          children: [
            // Top Window Bar
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
              decoration: BoxDecoration(
                color: activeTheme.surfaceContainer,
                borderRadius: const BorderRadius.vertical(top: Radius.circular(12)),
                border: Border(bottom: BorderSide(color: activeTheme.border)),
              ),
              child: Row(
                children: [
                  Icon(Icons.palette_outlined, color: activeTheme.primaryAccent, size: 22),
                  const SizedBox(width: 10),
                  Text(
                    'Gestionar temas (Manage Themes)',
                    style: TextStyle(
                      color: activeTheme.textPrimary,
                      fontSize: 15,
                      fontWeight: FontWeight.bold,
                    ),
                  ),
                  const SizedBox(width: 12),
                  Flexible(
                    child: Container(
                      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 3),
                      decoration: BoxDecoration(
                        color: activeTheme.primaryAccent.withValues(alpha: 0.15),
                        borderRadius: BorderRadius.circular(6),
                        border: Border.all(color: activeTheme.primaryAccent.withValues(alpha: 0.3)),
                      ),
                      child: Text(
                        'Activo: ${activeTheme.name}',
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          color: activeTheme.primaryAccent,
                          fontSize: 11,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 12),
                  IconButton(
                    icon: Icon(Icons.close, color: activeTheme.textSecondary, size: 20),
                    onPressed: () => Navigator.of(context).pop(),
                    tooltip: 'Cerrar',
                    splashRadius: 18,
                  ),
                ],
              ),
            ),

            // Main Content Area: Split 2 columns
            Expanded(
              child: Row(
                children: [
                  // Left: Themes list panel (360px)
                  Container(
                    width: 360,
                    decoration: BoxDecoration(
                      color: activeTheme.surface,
                      border: Border(right: BorderSide(color: activeTheme.border)),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        // Search bar
                        Padding(
                          padding: const EdgeInsets.all(12),
                          child: SizedBox(
                            height: 36,
                            child: TextField(
                              style: TextStyle(color: activeTheme.textPrimary, fontSize: 12),
                              decoration: InputDecoration(
                                hintText: 'Filtrar temas...',
                                hintStyle: TextStyle(color: activeTheme.textSecondary, fontSize: 12),
                                prefixIcon: Icon(Icons.search, size: 16, color: activeTheme.textSecondary),
                                contentPadding: EdgeInsets.zero,
                                filled: true,
                                fillColor: activeTheme.surfaceContainer,
                                border: OutlineInputBorder(
                                  borderRadius: BorderRadius.circular(8),
                                  borderSide: BorderSide(color: activeTheme.border),
                                ),
                                enabledBorder: OutlineInputBorder(
                                  borderRadius: BorderRadius.circular(8),
                                  borderSide: BorderSide(color: activeTheme.border),
                                ),
                              ),
                              onChanged: (v) => setState(() => _searchFilter = v),
                            ),
                          ),
                        ),

                        // Themes List
                        Expanded(
                          child: ListView.builder(
                            itemCount: _filteredThemes.length,
                            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                            itemBuilder: (context, idx) {
                              final theme = _filteredThemes[idx];
                              final isSelected = theme.id == _selectedTheme.id;
                              final isActive = theme.id == activeTheme.id;

                              return InkWell(
                                onTap: () => setState(() => _selectedTheme = theme),
                                borderRadius: BorderRadius.circular(8),
                                child: Container(
                                  margin: const EdgeInsets.only(bottom: 6),
                                  padding: const EdgeInsets.all(10),
                                  decoration: BoxDecoration(
                                    color: isSelected
                                        ? activeTheme.primaryAccent.withValues(alpha: 0.1)
                                        : Colors.transparent,
                                    borderRadius: BorderRadius.circular(8),
                                    border: Border.all(
                                      color: isSelected
                                          ? activeTheme.primaryAccent
                                          : (isActive ? activeTheme.successColor : activeTheme.border.withValues(alpha: 0.5)),
                                      width: isSelected ? 1.5 : 1,
                                    ),
                                  ),
                                  child: Column(
                                    crossAxisAlignment: CrossAxisAlignment.start,
                                    children: [
                                      Row(
                                        children: [
                                          Expanded(
                                            child: Text(
                                              theme.name,
                                              style: TextStyle(
                                                color: activeTheme.textPrimary,
                                                fontSize: 12,
                                                fontWeight: isSelected ? FontWeight.bold : FontWeight.w600,
                                              ),
                                              overflow: TextOverflow.ellipsis,
                                            ),
                                          ),
                                          if (isActive)
                                            Container(
                                              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                                              decoration: BoxDecoration(
                                                color: activeTheme.successColor.withValues(alpha: 0.15),
                                                borderRadius: BorderRadius.circular(4),
                                                border: Border.all(color: activeTheme.successColor.withValues(alpha: 0.4)),
                                              ),
                                              child: Text(
                                                '✔ ACTIVO',
                                                style: TextStyle(
                                                  color: activeTheme.successColor,
                                                  fontSize: 9,
                                                  fontWeight: FontWeight.bold,
                                                ),
                                              ),
                                            ),
                                        ],
                                      ),
                                      const SizedBox(height: 4),
                                      Text(
                                        theme.description,
                                        style: TextStyle(color: activeTheme.textSecondary, fontSize: 10),
                                        maxLines: 2,
                                        overflow: TextOverflow.ellipsis,
                                      ),
                                      const SizedBox(height: 8),
                                      // Color swatches row
                                      Row(
                                        children: [
                                          _buildColorSwatch('Fondo', theme.background),
                                          const SizedBox(width: 4),
                                          _buildColorSwatch('Superficie', theme.surface),
                                          const SizedBox(width: 4),
                                          _buildColorSwatch('Acento 1', theme.primaryAccent),
                                          const SizedBox(width: 4),
                                          _buildColorSwatch('Acento 2', theme.secondaryAccent),
                                          const Spacer(),
                                          Container(
                                            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
                                            decoration: BoxDecoration(
                                              color: activeTheme.surfaceContainer,
                                              borderRadius: BorderRadius.circular(4),
                                            ),
                                            child: Text(
                                              theme.isDark ? 'Oscuro' : 'Claro',
                                              style: TextStyle(color: activeTheme.textSecondary, fontSize: 9),
                                            ),
                                          ),
                                        ],
                                      ),
                                    ],
                                  ),
                                ),
                              );
                            },
                          ),
                        ),
                      ],
                    ),
                  ),

                  // Right: Live Interactive Theme Preview
                  Expanded(
                    child: Container(
                      color: activeTheme.surfaceContainer.withValues(alpha: 0.4),
                      padding: const EdgeInsets.all(16),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Text(
                                'Vista previa:',
                                style: TextStyle(color: activeTheme.textSecondary, fontSize: 12, fontWeight: FontWeight.bold),
                              ),
                              const SizedBox(width: 6),
                              Expanded(
                                child: Text(
                                  _selectedTheme.name,
                                  style: TextStyle(color: activeTheme.textPrimary, fontSize: 12, fontWeight: FontWeight.bold),
                                  overflow: TextOverflow.ellipsis,
                                ),
                              ),
                              const SizedBox(width: 8),
                              if (!isPreviewActive)
                                ElevatedButton.icon(
                                  icon: const Icon(Icons.check, size: 14),
                                  label: const Text('Aplicar este tema', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold)),
                                  style: ElevatedButton.styleFrom(
                                    backgroundColor: _selectedTheme.primaryAccent,
                                    foregroundColor: Colors.white,
                                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                                    minimumSize: Size.zero,
                                  ),
                                  onPressed: () => _applyTheme(_selectedTheme),
                                ),
                            ],
                          ),
                          const SizedBox(height: 12),

                          // Render mock UI in the selected theme
                          Expanded(
                            child: _buildMockLiveUI(_selectedTheme),
                          ),
                        ],
                      ),
                    ),
                  ),
                ],
              ),
            ),

            // Bottom Actions Bar
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
              decoration: BoxDecoration(
                color: activeTheme.surfaceContainer,
                borderRadius: const BorderRadius.vertical(bottom: Radius.circular(12)),
                border: Border(top: BorderSide(color: activeTheme.border)),
              ),
              child: Row(
                children: [
                  OutlinedButton.icon(
                    icon: const Icon(Icons.light_mode, size: 14),
                    label: const Text('Light (Blanco)', style: TextStyle(fontSize: 11)),
                    style: OutlinedButton.styleFrom(
                      foregroundColor: activeTheme.textPrimary,
                      side: BorderSide(color: activeTheme.border),
                      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                    ),
                    onPressed: () {
                      _applyTheme(AppThemes.light);
                      setState(() => _selectedTheme = AppThemes.light);
                    },
                  ),
                  const SizedBox(width: 8),
                  OutlinedButton.icon(
                    icon: const Icon(Icons.dark_mode, size: 14),
                    label: const Text('Dark Modern', style: TextStyle(fontSize: 11)),
                    style: OutlinedButton.styleFrom(
                      foregroundColor: activeTheme.textPrimary,
                      side: BorderSide(color: activeTheme.border),
                      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                    ),
                    onPressed: () {
                      _applyTheme(AppThemes.dark);
                      setState(() => _selectedTheme = AppThemes.dark);
                    },
                  ),
                  const Spacer(),
                  OutlinedButton(
                    onPressed: () => Navigator.of(context).pop(),
                    style: OutlinedButton.styleFrom(
                      foregroundColor: activeTheme.textPrimary,
                      side: BorderSide(color: activeTheme.border),
                    ),
                    child: const Text('Cerrar'),
                  ),
                  const SizedBox(width: 8),
                  ElevatedButton(
                    onPressed: () {
                      _applyTheme(_selectedTheme);
                      Navigator.of(context).pop();
                    },
                    style: ElevatedButton.styleFrom(
                      backgroundColor: _selectedTheme.primaryAccent,
                      foregroundColor: Colors.white,
                    ),
                    child: const Text('Aceptar y Aplicar'),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildColorSwatch(String label, Color color) {
    return Tooltip(
      message: '$label: #${color.toARGB32().toRadixString(16).padLeft(8, '0').substring(2).toUpperCase()}',
      child: Container(
        width: 14,
        height: 14,
        decoration: BoxDecoration(
          color: color,
          shape: BoxShape.circle,
          border: Border.all(color: Colors.black26, width: 1),
        ),
      ),
    );
  }

  Widget _buildMockLiveUI(AppThemeDefinition theme) {
    return Container(
      decoration: BoxDecoration(
        color: theme.background,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: theme.border),
      ),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(8),
        child: Column(
          children: [
            // Mock Menu Bar
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              color: theme.surface,
              child: SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                child: Row(
                  children: [
                    Icon(Icons.layers, size: 16, color: theme.primaryAccent),
                    const SizedBox(width: 8),
                    Text('File4Base Client', style: TextStyle(color: theme.textPrimary, fontSize: 11, fontWeight: FontWeight.bold)),
                    const SizedBox(width: 16),
                    Text('File', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                    const SizedBox(width: 12),
                    Text('Edit', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                    const SizedBox(width: 12),
                    Text('View', style: TextStyle(color: theme.textSecondary, fontSize: 11)),
                    const SizedBox(width: 12),
                    Text('Scripts', style: TextStyle(color: theme.primaryAccent, fontSize: 11, fontWeight: FontWeight.bold)),
                    const SizedBox(width: 24),
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                      decoration: BoxDecoration(
                        color: theme.surfaceContainer,
                        borderRadius: BorderRadius.circular(4),
                      ),
                      child: Text('file4base_dev', style: TextStyle(color: theme.textSecondary, fontSize: 10)),
                    ),
                  ],
                ),
              ),
            ),
            Divider(height: 1, color: theme.border),

            // Mock Body Split: Left Form + Right Script Preview
            Expanded(
              child: Row(
                children: [
                  // Form Mockup
                  Expanded(
                    flex: 5,
                    child: Container(
                      padding: const EdgeInsets.all(12),
                      color: theme.background,
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Expanded(
                                child: Text('Factura #1084', style: TextStyle(color: theme.textPrimary, fontSize: 11, fontWeight: FontWeight.bold), overflow: TextOverflow.ellipsis),
                              ),
                              Container(
                                padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 2),
                                decoration: BoxDecoration(
                                  color: theme.successColor.withValues(alpha: 0.15),
                                  borderRadius: BorderRadius.circular(4),
                                ),
                                child: Text('EMITIDA', style: TextStyle(color: theme.successColor, fontSize: 9, fontWeight: FontWeight.bold)),
                              ),
                            ],
                          ),
                          const SizedBox(height: 10),
                          Container(
                            padding: const EdgeInsets.all(10),
                            decoration: BoxDecoration(
                              color: theme.surface,
                              borderRadius: BorderRadius.circular(6),
                              border: Border.all(color: theme.border),
                            ),
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text('Cliente:', style: TextStyle(color: theme.textSecondary, fontSize: 10)),
                                const SizedBox(height: 2),
                                Text('Distribuciones Globales S.A.', style: TextStyle(color: theme.textPrimary, fontSize: 11, fontWeight: FontWeight.w600)),
                                const SizedBox(height: 6),
                                Text('Subtotal / Importe:', style: TextStyle(color: theme.textSecondary, fontSize: 10)),
                                const SizedBox(height: 2),
                                Text('5.240,00 €', style: TextStyle(color: theme.primaryAccent, fontSize: 13, fontWeight: FontWeight.bold)),
                              ],
                            ),
                          ),
                          const Spacer(),
                          Row(
                            children: [
                              OutlinedButton(
                                onPressed: () {},
                                style: OutlinedButton.styleFrom(
                                  foregroundColor: theme.textPrimary,
                                  side: BorderSide(color: theme.border),
                                  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                                  minimumSize: Size.zero,
                                ),
                                child: const Text('Cancelar', style: TextStyle(fontSize: 10)),
                              ),
                              const SizedBox(width: 8),
                              ElevatedButton(
                                onPressed: () {},
                                style: ElevatedButton.styleFrom(
                                  backgroundColor: theme.primaryAccent,
                                  foregroundColor: Colors.white,
                                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
                                  minimumSize: Size.zero,
                                ),
                                child: const Text('Guardar', style: TextStyle(fontSize: 10, fontWeight: FontWeight.bold)),
                              ),
                            ],
                          ),
                        ],
                      ),
                    ),
                  ),
                  VerticalDivider(width: 1, color: theme.border),

                  // Script Workspace Snippet Mockup
                  Expanded(
                    flex: 6,
                    child: Container(
                      padding: const EdgeInsets.all(12),
                      color: theme.surface,
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Icon(Icons.code, size: 14, color: theme.primaryAccent),
                              const SizedBox(width: 6),
                              Text('Guión: on_invoice_created', style: TextStyle(color: theme.textPrimary, fontSize: 11, fontWeight: FontWeight.bold)),
                            ],
                          ),
                          const SizedBox(height: 8),
                          // Steps
                          _buildMockStep(theme, '01', 'Ir a Presentación', '[Invoices_Detail]', theme.navColor),
                          _buildMockStep(theme, '02', 'Establecer Variable', r'[$subtotal = Sum(Items.price)]', theme.fieldsColor),
                          _buildMockStep(theme, '03', 'Si', '[Invoices::Total > 5000]', theme.controlColor),
                          _buildMockStep(theme, '04', 'Guardar Registros', '[Commit]', theme.recordsColor),
                          const Spacer(),
                          // Theme hex chips
                          Wrap(
                            spacing: 4,
                            runSpacing: 4,
                            children: [
                              _buildHexBadge('Fondo', theme.background, theme),
                              _buildHexBadge('Superficie', theme.surface, theme),
                              _buildHexBadge('Acento', theme.primaryAccent, theme),
                              _buildHexBadge('Texto', theme.textPrimary, theme),
                            ],
                          ),
                        ],
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildMockStep(AppThemeDefinition theme, String num, String action, String param, Color badgeColor) {
    return Container(
      margin: const EdgeInsets.only(bottom: 4),
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 3),
      decoration: BoxDecoration(
        color: theme.surfaceContainer.withValues(alpha: 0.5),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Row(
        children: [
          Text(num, style: TextStyle(color: theme.textSecondary, fontSize: 9, fontFamily: 'monospace')),
          const SizedBox(width: 6),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 1),
            decoration: BoxDecoration(
              color: badgeColor.withValues(alpha: 0.2),
              borderRadius: BorderRadius.circular(3),
            ),
            child: Text(action, style: TextStyle(color: badgeColor, fontSize: 9, fontWeight: FontWeight.bold)),
          ),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              param,
              style: TextStyle(color: theme.textPrimary, fontSize: 9, fontFamily: 'monospace'),
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildHexBadge(String label, Color color, AppThemeDefinition theme) {
    final hex = '#${color.toARGB32().toRadixString(16).padLeft(8, '0').substring(2).toUpperCase()}';
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: theme.background,
        borderRadius: BorderRadius.circular(4),
        border: Border.all(color: theme.border),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(width: 8, height: 8, decoration: BoxDecoration(color: color, shape: BoxShape.circle)),
          const SizedBox(width: 4),
          Text('$label: $hex', style: TextStyle(color: theme.textSecondary, fontSize: 8, fontFamily: 'monospace')),
        ],
      ),
    );
  }
}
