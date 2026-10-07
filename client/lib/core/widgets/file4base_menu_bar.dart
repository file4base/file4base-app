import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:url_launcher/url_launcher.dart';
import '../services/solution_storage.dart';
import '../../features/help/check_updates_dialog.dart';
import '../../features/help/issues_guide_dialog.dart';
import '../../main.dart';
import '../../features/layout_engine/layout_object_visuals.dart' show LayoutMergeSymbols;

class File4BaseMenuBar extends StatelessWidget {
  final OperationalMode activeMode;
  final ValueChanged<OperationalMode> onModeChanged;
  final VoidCallback onManageDatabase;
  final VoidCallback? onManageLayouts;
  final VoidCallback? onManageSecurity;
  final VoidCallback? onManageScripts;
  final VoidCallback? onScriptWorkspace;
  final VoidCallback? onManageThemes;
  final VoidCallback? onManageValueLists;
  final VoidCallback onOpenRemote;
  final VoidCallback onAbout;
  final VoidCallback? onNewDatabase;
  final VoidCallback? onOpenSolution;
  final VoidCallback? onSave;
  final VoidCallback? onSaveAs;
  final VoidCallback? onSaveCopyAs;
  final VoidCallback? onExportData;
  final VoidCallback? onFileOptions;
  final VoidCallback? onPageSetup;
  final VoidCallback? onNewRecord;
  final VoidCallback? onDuplicateRecord;
  final VoidCallback? onDeleteRecord;
  final VoidCallback? onSortRecords;
  final VoidCallback? onUnsortRecords;
  final VoidCallback? onCommitRecord;
  final VoidCallback? onRevertRecord;

  /// `first`, `previous`, `next`, `last`, or a 1-based record number.
  final ValueChanged<String>? onGoToRecord;
  final VoidCallback? onShowAllRecords;
  final VoidCallback? onPerformFind;
  final VoidCallback? onNewFindRequest;
  final VoidCallback? onDuplicateFindRequest;
  final VoidCallback? onDeleteFindRequest;
  final VoidCallback? onDeleteAllFindRequests;
  final VoidCallback? onToggleFindOmit;

  /// `first`, `previous`, `next`, `last`, or a 1-based request number.
  final ValueChanged<String>? onGoToFindRequest;
  final VoidCallback? onSaveLayout;
  final bool isToolbarVisible;
  final ValueChanged<bool> onToggleToolbar;
  final VoidCallback? onZoomIn;
  final VoidCallback? onZoomOut;
  final VoidCallback? onResetZoom;
  final ValueChanged<double>? onSelectZoom;
  final double? zoomLevel;
  final VoidCallback? onChangePassword;
  final VoidCallback? onExportRecords;
  final VoidCallback? onPrint;
  final VoidCallback? onQuit;
  final bool isAuthenticated;
  final VoidCallback? onSignIn;
  final VoidCallback? onSignOut;

  /// Insert menu actions in Layout mode: embed a file (`image`, `pdf`,
  /// `video`, `file`) in the selected shape or a new object, insert a merge
  /// symbol (date, time, user name) or a merge field into layout text.
  final ValueChanged<String>? onInsertMedia;
  final ValueChanged<String>? onInsertSymbol;
  final VoidCallback? onInsertMergeField;
  final String? currentUserName;

  const File4BaseMenuBar({
    super.key,
    required this.activeMode,
    required this.onModeChanged,
    required this.onManageDatabase,
    this.onManageLayouts,
    this.onManageSecurity,
    this.onManageScripts,
    this.onScriptWorkspace,
    this.onManageThemes,
    this.onManageValueLists,
    required this.onOpenRemote,
    required this.onAbout,
    this.onNewDatabase,
    this.onOpenSolution,
    this.onSave,
    this.onSaveAs,
    this.onSaveCopyAs,
    this.onExportData,
    this.onFileOptions,
    this.onChangePassword,
    this.onPageSetup,
    this.onPrint,
    this.onQuit,
    this.onExportRecords,
    this.onNewRecord,
    this.onDuplicateRecord,
    this.onDeleteRecord,
    this.onSortRecords,
    this.onUnsortRecords,
    this.onCommitRecord,
    this.onRevertRecord,
    this.onGoToRecord,
    this.onShowAllRecords,
    this.onPerformFind,
    this.onNewFindRequest,
    this.onDuplicateFindRequest,
    this.onDeleteFindRequest,
    this.onDeleteAllFindRequests,
    this.onToggleFindOmit,
    this.onGoToFindRequest,
    this.onSaveLayout,
    required this.isToolbarVisible,
    required this.onToggleToolbar,
    this.onZoomIn,
    this.onZoomOut,
    this.onResetZoom,
    this.onSelectZoom,
    this.zoomLevel,
    this.isAuthenticated = true,
    this.onSignIn,
    this.onSignOut,
    this.onInsertMedia,
    this.onInsertSymbol,
    this.onInsertMergeField,
    this.currentUserName,
  });

  /// Asks for a record number and goes there.
  Future<void> _promptGoToRecord(BuildContext context) async {
    final controller = TextEditingController();
    final target = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Go to Record'),
        content: TextField(
          controller: controller,
          autofocus: true,
          keyboardType: TextInputType.number,
          decoration: const InputDecoration(labelText: 'Record number'),
          onSubmitted: (value) => Navigator.pop(ctx, value.trim()),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, controller.text.trim()),
            child: const Text('Go'),
          ),
        ],
      ),
    );
    if (target != null && target.isNotEmpty) onGoToRecord?.call(target);
  }

  void _showNotice(BuildContext context, String title, String message) {
    ScaffoldMessenger.of(context).hideCurrentSnackBar();
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Row(
          children: [
            const Icon(Icons.info_outline, size: 18, color: Colors.white70),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                '$title: $message',
                style: const TextStyle(fontSize: 13),
              ),
            ),
          ],
        ),
        duration: const Duration(seconds: 2),
        behavior: SnackBarBehavior.floating,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
      ),
    );
  }

  void _showRoadmapDialog(BuildContext context, String featureName, String phase, String details) {
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Row(
          children: [
            const Icon(Icons.construction, color: Color(0xFF1E88E5)),
            const SizedBox(width: 10),
            Text(featureName),
          ],
        ),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
              decoration: BoxDecoration(
                color: const Color(0xFF1E88E5).withOpacity(0.12),
                borderRadius: BorderRadius.circular(4),
              ),
              child: Text(
                phase,
                style: const TextStyle(
                  color: Color(0xFF1E88E5),
                  fontWeight: FontWeight.bold,
                  fontSize: 12,
                ),
              ),
            ),
            const SizedBox(height: 12),
            Text(details, style: const TextStyle(fontSize: 14)),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(),
            child: const Text('Close'),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;

    return Container(
      height: 32,
      width: double.infinity,
      alignment: Alignment.centerLeft,
      decoration: BoxDecoration(
        color: isDark ? const Color(0xFF161B22) : const Color(0xFFF3F4F6),
        border: Border(
          bottom: BorderSide(
            color: isDark ? const Color(0xFF30363D) : const Color(0xFFE5E7EB),
            width: 1.0,
          ),
        ),
      ),
      child: MenuBar(
        style: MenuStyle(
          alignment: Alignment.centerLeft,
          backgroundColor: WidgetStatePropertyAll(
            isDark ? const Color(0xFF161B22) : const Color(0xFFF3F4F6),
          ),
          elevation: const WidgetStatePropertyAll(0),
          padding: const WidgetStatePropertyAll(EdgeInsets.symmetric(horizontal: 2)),
          shape: const WidgetStatePropertyAll(RoundedRectangleBorder()),
        ),
        children: [
          _buildFileMenu(context),
          _buildEditMenu(context),
          _buildViewMenu(context),
          _buildInsertMenu(context),
          _buildFormatMenu(context),
          if (activeMode == OperationalMode.find)
            _buildRequestsMenu(context)
          else
            _buildRecordsMenu(context),
          _buildScriptsMenu(context),
          _buildToolsMenu(context),
          _buildWindowMenu(context),
          _buildHelpMenu(context),
        ],
      ),
    );
  }

  // 1. File Menu
  Widget _buildFileMenu(BuildContext context) {
    return SubmenuButton(
      menuChildren: [
        if (!isAuthenticated) ...[
          MenuItemButton(
            onPressed: onSignIn,
            shortcut: const SingleActivator(LogicalKeyboardKey.keyL, meta: true),
            child: const Row(
              children: [
                Icon(Icons.login, size: 16, color: Color(0xFF1E88E5)),
                SizedBox(width: 8),
                Text('Sign In...'),
              ],
            ),
          ),
          const Divider(height: 1),
        ],
        MenuItemButton(
          onPressed: onNewDatabase,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyN, meta: true),
          child: const Text('New Database...'),
        ),
        MenuItemButton(
          onPressed: onOpenSolution,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyO, meta: true),
          child: const Text('Open...'),
        ),
        MenuItemButton(
          onPressed: onOpenRemote,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyO, meta: true, shift: true),
          child: const Text('Open Remote...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, 'Authentication Required', 'Please sign in to save solution changes.')
              : (onSave),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true),
          child: const Text('Save'),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, 'Authentication Required', 'Please sign in to save solution changes.')
              : (onSaveAs),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true, shift: true),
          child: const Text('Save As...'),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, 'Authentication Required', 'Please sign in to save a copy.')
              : (onSaveCopyAs),
          child: const Text('Save a Copy As...'),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, 'Authentication Required', 'Please sign in to export database data.')
              : (onExportData),
          child: const Text('Export Data...'),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: null, // not implemented: shown disabled
              child: const Text('Local Host (localhost:8080)'),
            ),
          ],
          child: const Text('Open Favorite'),
        ),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: null, // not implemented: shown disabled
              child: const Text('default_workspace (PostgreSQL)'),
            ),
          ],
          child: const Text('Open Recent'),
        ),
        const Divider(height: 1),
        if (isAuthenticated && onSignOut != null) ...[
          MenuItemButton(
            onPressed: onSignOut,
            child: const Row(
              children: [
                Icon(Icons.lock_outline, size: 16, color: Colors.orange),
                SizedBox(width: 8),
                Text('Sign Out (Lock Session)'),
              ],
            ),
          ),
          const Divider(height: 1),
        ],
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: !isAuthenticated
                  ? (onSignIn ?? () => _showNotice(context, 'Authentication Required', 'Please sign in to manage database schema.'))
                  : onManageDatabase,
              shortcut: const SingleActivator(LogicalKeyboardKey.keyD, meta: true, shift: true),
              child: const Text('Database...'),
            ),
            MenuItemButton(
              onPressed: onManageSecurity ??
                  () => _showRoadmapDialog(context, 'Manage Security', 'Roadmap Phase 8', 'User accounts, privilege sets, and extended access rules.'),
              shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true, shift: true),
              child: const Text('Security...'),
            ),
            MenuItemButton(
              onPressed: onManageValueLists,
              child: const Text('Value Lists...'),
            ),
            MenuItemButton(
              onPressed: () => _showRoadmapDialog(context, 'Custom Functions', 'Roadmap Phase 7', 'Calculation engine reusable functions and recursive formulas.'),
              child: const Text('Custom Functions...'),
            ),
            MenuItemButton(
              onPressed: () => _showRoadmapDialog(context, 'External Data Sources', 'Roadmap Phase 1/4', 'PostgreSQL, MariaDB, and ODBC external connections.'),
              child: const Text('External Data Sources...'),
            ),
            MenuItemButton(
              onPressed: onManageLayouts ?? () => onModeChanged(OperationalMode.layout),
              child: const Text('Layouts...'),
            ),
            MenuItemButton(
              onPressed: onManageScripts ?? onScriptWorkspace ?? () => _showRoadmapDialog(context, 'Manage Scripts', 'Roadmap Phase 7', 'Automated workflows and calculation scripts.'),
              child: const Text('Scripts...'),
            ),
            MenuItemButton(
              onPressed: () => _showRoadmapDialog(context, 'Containers', 'Roadmap Phase 8', 'Blob storage, S3 integration, and secure container files.'),
              child: const Text('Containers...'),
            ),
            MenuItemButton(
              onPressed: onManageThemes ?? () => _showRoadmapDialog(context, 'Themes', 'Roadmap Phase 5', 'Visual styling themes, typography rules, and CSS presets.'),
              child: const Text('Themes...'),
            ),
          ],
          child: const Text('Manage'),
        ),
        MenuItemButton(
          onPressed: onFileOptions,
          child: const Text('File Options...'),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, 'Authentication Required', 'Please sign in to change password.')
              : (onChangePassword),
          child: const Text('Change Password...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onPageSetup ?? () => onModeChanged(OperationalMode.preview),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyP, meta: true, shift: true),
          child: const Text('Page Setup...'),
        ),
        MenuItemButton(
          onPressed: onPrint ?? () => onModeChanged(OperationalMode.preview),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyP, meta: true),
          child: const Text('Print...'),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: () => _showRoadmapDialog(context, 'Import File', 'Roadmap Phase 4', 'Import CSV, XLSX, XML, or JSON into active table.'),
              child: const Text('File...'),
            ),
            MenuItemButton(
              onPressed: () => _showRoadmapDialog(context, 'Import Folder', 'Roadmap Phase 8', 'Batch import image assets into container fields.'),
              child: const Text('Folder...'),
            ),
            MenuItemButton(
              onPressed: () => _showRoadmapDialog(context, 'Import XML', 'Roadmap Phase 4', 'Parse XML schema data source.'),
              child: const Text('XML Data Source...'),
            ),
            MenuItemButton(
              onPressed: () => _showRoadmapDialog(context, 'Import ODBC', 'Roadmap Phase 4', 'Extract data directly from ODBC origin.'),
              child: const Text('ODBC Data Source...'),
            ),
          ],
          child: const Text('Import Records'),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, 'Authentication Required', 'Please sign in to export records.')
              : (onExportRecords ?? () => _showRoadmapDialog(context, 'Export Records', 'Roadmap Phase 4', 'Export records to CSV, JSON, XML, or Excel format.')),
          child: const Text('Export Records...'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Recover...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onQuit ?? () => SolutionStorageService.triggerQuit(),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyQ, meta: true),
          child: const Row(
            children: [
              Icon(Icons.exit_to_app, size: 16, color: Colors.redAccent),
              SizedBox(width: 8),
              Text('Quit'),
            ],
          ),
        ),
      ],
      child: const Text('File', style: TextStyle(fontSize: 13)),
    );
  }

  // 2. Edit Menu
  Widget _buildEditMenu(BuildContext context) {
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          shortcut: const SingleActivator(LogicalKeyboardKey.keyZ, meta: true),
          child: const Text('Undo'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          shortcut: const SingleActivator(LogicalKeyboardKey.keyZ, meta: true, shift: true),
          child: const Text('Redo'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          shortcut: const SingleActivator(LogicalKeyboardKey.keyX, meta: true),
          child: const Text('Cut'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          shortcut: const SingleActivator(LogicalKeyboardKey.keyC, meta: true),
          child: const Text('Copy'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          shortcut: const SingleActivator(LogicalKeyboardKey.keyV, meta: true),
          child: const Text('Paste'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Clear'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          shortcut: const SingleActivator(LogicalKeyboardKey.keyA, meta: true),
          child: const Text('Select All'),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: () => onModeChanged(OperationalMode.find),
              child: const Text('Find/Replace...'),
            ),
            MenuItemButton(
              onPressed: null, // not implemented: shown disabled
              child: const Text('Find Next'),
            ),
            MenuItemButton(
              onPressed: null, // not implemented: shown disabled
              child: const Text('Find Previous'),
            ),
          ],
          child: const Text('Find/Replace'),
        ),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: null, // not implemented: shown disabled
              child: const Text('Check Selection...'),
            ),
            MenuItemButton(
              onPressed: null, // not implemented: shown disabled
              child: const Text('Check Record...'),
            ),
            MenuItemButton(
              onPressed: null, // not implemented: shown disabled
              child: const Text('Correct Word...'),
            ),
            MenuItemButton(
              onPressed: null, // not implemented: shown disabled
              child: const Text('Dictionaries...'),
            ),
            MenuItemButton(
              onPressed: null, // not implemented: shown disabled
              child: const Text('Edit User Dictionary...'),
            ),
            MenuItemButton(
              onPressed: null, // not implemented: shown disabled
              child: const Text('Options...'),
            ),
          ],
          child: const Text('Spelling'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onOpenRemote,
          child: const Text('Preferences...'),
        ),
      ],
      child: const Text('Edit', style: TextStyle(fontSize: 13)),
    );
  }

  // 3. View Menu
  Widget _buildViewMenu(BuildContext context) {
    void handleModeChange(OperationalMode mode) {
      if (!isAuthenticated) {
        if (onSignIn != null) {
          onSignIn!();
        } else {
          _showNotice(context, 'Authentication Required', 'Please sign in to access database views.');
        }
        return;
      }
      onModeChanged(mode);
    }

    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          leadingIcon: activeMode == OperationalMode.browse ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => handleModeChange(OperationalMode.browse),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyB, meta: true),
          child: const Text('Browse Mode'),
        ),
        MenuItemButton(
          leadingIcon: activeMode == OperationalMode.find ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => handleModeChange(OperationalMode.find),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyF, meta: true),
          child: const Text('Find Mode'),
        ),
        MenuItemButton(
          leadingIcon: activeMode == OperationalMode.layout ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => handleModeChange(OperationalMode.layout),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyL, meta: true),
          child: const Text('Layout Mode'),
        ),
        MenuItemButton(
          leadingIcon: activeMode == OperationalMode.preview ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => handleModeChange(OperationalMode.preview),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyU, meta: true),
          child: const Text('Preview Mode'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Page Margins'),
        ),
        MenuItemButton(
          leadingIcon: isToolbarVisible ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => onToggleToolbar(!isToolbarVisible),
          child: const Text('Status Toolbar'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Formatting Bar'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Text Ruler'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onZoomIn,
          shortcut: const SingleActivator(LogicalKeyboardKey.equal, meta: true),
          child: const Text('Zoom In'),
        ),
        MenuItemButton(
          onPressed: onZoomOut,
          shortcut: const SingleActivator(LogicalKeyboardKey.minus, meta: true),
          child: const Text('Zoom Out'),
        ),
        MenuItemButton(
          onPressed: onResetZoom,
          shortcut: const SingleActivator(LogicalKeyboardKey.digit0, meta: true),
          child: const Text('Actual Size (100%)'),
        ),
        SubmenuButton(
          menuChildren: [
            for (final level in [4.0, 3.0, 2.0, 1.5, 1.0, 0.75, 0.5, 0.25])
              MenuItemButton(
                leadingIcon: zoomLevel != null && (zoomLevel! - level).abs() < 0.001
                    ? const Icon(Icons.check, size: 14)
                    : null,
                onPressed: onSelectZoom != null ? () => onSelectZoom!(level) : null,
                child: Text('${(level * 100).round()}%'),
              ),
          ],
          child: const Text('Zoom Level'),
        ),
      ],
      child: const Text('View', style: TextStyle(fontSize: 13)),
    );
  }

  // 4. Insert Menu
  // In Layout mode the items insert into the layout (handlers supplied by the
  // host); in the other modes there is nothing to insert into, so they are
  // disabled rather than reporting an insertion that did not happen.
  Widget _buildInsertMenu(BuildContext context) {
    final media = onInsertMedia;
    final symbol = onInsertSymbol;
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          leadingIcon: const Icon(Icons.image_outlined, size: 16),
          onPressed: media != null
              ? () => media('image')
              : null, // only available in Layout mode
          child: const Text('Picture...'),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.movie_outlined, size: 16),
          onPressed: media != null
              ? () => media('video')
              : null, // only available in Layout mode
          child: const Text('Audio/Video...'),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.picture_as_pdf_outlined, size: 16),
          onPressed: media != null
              ? () => media('pdf')
              : null, // only available in Layout mode
          child: const Text('PDF...'),
        ),
        MenuItemButton(
          onPressed: media != null
              ? () => media('video')
              : null, // only available in Layout mode
          child: const Text('QuickTime...'),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.attach_file, size: 16),
          onPressed: media != null
              ? () => media('file')
              : null, // only available in Layout mode
          child: const Text('File...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: symbol != null
              ? () => symbol(LayoutMergeSymbols.currentDate)
              : null, // only available in Layout mode
          child: const Text('Current Date'),
        ),
        MenuItemButton(
          onPressed: symbol != null
              ? () => symbol(LayoutMergeSymbols.currentTime)
              : null, // only available in Layout mode
          child: const Text('Current Time'),
        ),
        MenuItemButton(
          onPressed: symbol != null
              ? () => symbol(LayoutMergeSymbols.currentUser)
              : null, // only available in Layout mode
          child: const Text('Current User Name'),
        ),
        if (symbol != null)
          MenuItemButton(
            onPressed: () => symbol(LayoutMergeSymbols.pageNumber),
            child: const Text('Page Number'),
          ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('From Index...'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('From Last Visited Record'),
        ),
        MenuItemButton(
          onPressed: onInsertMergeField,
          child: const Text('Merge Field...'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Merge Variable...'),
        ),
      ],
      child: const Text('Insert', style: TextStyle(fontSize: 13)),
    );
  }

  // 5. Format Menu
  Widget _buildFormatMenu(BuildContext context) {
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Font'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Size'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Style'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Align Text'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Line Spacing'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Text Color'),
        ),
        if (activeMode == OperationalMode.layout) ...[
          const Divider(height: 1),
          MenuItemButton(
            onPressed: null, // not implemented: shown disabled
            child: const Text('Align Object (Layout Mode)'),
          ),
          MenuItemButton(
            onPressed: null, // not implemented: shown disabled
            child: const Text('Distribute (Layout Mode)'),
          ),
          MenuItemButton(
            onPressed: null, // not implemented: shown disabled
            child: const Text('Resize (Layout Mode)'),
          ),
        ],
      ],
      child: const Text('Format', style: TextStyle(fontSize: 13)),
    );
  }

  // 6. Records Menu (Browse Mode)
  Widget _buildRecordsMenu(BuildContext context) {
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: onNewRecord,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyN, meta: true),
          child: const Text('New Record'),
        ),
        MenuItemButton(
          onPressed: onDuplicateRecord,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyD, meta: true),
          child: const Text('Duplicate Record'),
        ),
        MenuItemButton(
          onPressed: onDeleteRecord,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyE, meta: true),
          child: const Text('Delete Record...'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Delete All Records...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onCommitRecord,
          child: const Text('Commit Record'),
        ),
        MenuItemButton(
          onPressed: onRevertRecord,
          child: const Text('Revert Record'),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: onGoToRecord == null ? null : () => onGoToRecord!('first'),
              child: const Text('First'),
            ),
            MenuItemButton(
              onPressed: onGoToRecord == null ? null : () => onGoToRecord!('previous'),
              child: const Text('Previous'),
            ),
            MenuItemButton(
              onPressed: onGoToRecord == null ? null : () => onGoToRecord!('next'),
              child: const Text('Next'),
            ),
            MenuItemButton(
              onPressed: onGoToRecord == null ? null : () => onGoToRecord!('last'),
              child: const Text('Last'),
            ),
            MenuItemButton(
              onPressed: onGoToRecord == null ? null : () => _promptGoToRecord(context),
              child: const Text('By Number...'),
            ),
          ],
          child: const Text('Go to Record'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onShowAllRecords,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyJ, meta: true),
          child: const Text('Show All Records'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Show Omitted Only'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          shortcut: const SingleActivator(LogicalKeyboardKey.keyM, meta: true),
          child: const Text('Omit Record'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Omit Multiple...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onSortRecords,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true),
          child: const Text('Sort Records...'),
        ),
        MenuItemButton(
          onPressed: onUnsortRecords,
          child: const Text('Unsort'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showRoadmapDialog(context, 'Replace Field Contents', 'Roadmap Phase 4', 'Batch replace field values across all found records with calculation, serial, or constant.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.equal, meta: true),
          child: const Text('Replace Field Contents...'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Relookup Field Contents'),
        ),
      ],
      child: const Text('Records', style: TextStyle(fontSize: 13)),
    );
  }

  // 6b. Requests Menu (Find Mode)
  //
  // Find mode holds several requests: the records that match any of them,
  // minus the records matched by a request marked Omit.
  Widget _buildRequestsMenu(BuildContext context) {
    final goTo = onGoToFindRequest;
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: onPerformFind,
          shortcut: const SingleActivator(LogicalKeyboardKey.enter),
          child: const Text('Perform Find'),
        ),
        MenuItemButton(
          onPressed: onNewFindRequest,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyN, meta: true),
          child: const Text('New Request'),
        ),
        MenuItemButton(
          onPressed: onDuplicateFindRequest,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyD, meta: true),
          child: const Text('Duplicate Request'),
        ),
        MenuItemButton(
          onPressed: onDeleteFindRequest,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyE, meta: true),
          child: const Text('Delete Request'),
        ),
        MenuItemButton(
          onPressed: onDeleteAllFindRequests,
          child: const Text('Delete All Requests'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onToggleFindOmit,
          child: const Text('Include / Omit'),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: goTo == null ? null : () => goTo('first'),
              child: const Text('First'),
            ),
            MenuItemButton(
              onPressed: goTo == null ? null : () => goTo('previous'),
              child: const Text('Previous'),
            ),
            MenuItemButton(
              onPressed: goTo == null ? null : () => goTo('next'),
              child: const Text('Next'),
            ),
            MenuItemButton(
              onPressed: goTo == null ? null : () => goTo('last'),
              child: const Text('Last'),
            ),
          ],
          child: const Text('Go to Request'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () {
            onModeChanged(OperationalMode.browse);
            if (onShowAllRecords != null) onShowAllRecords!();
          },
          shortcut: const SingleActivator(LogicalKeyboardKey.keyJ, meta: true),
          child: const Text('Show All Records (Cancel Find)'),
        ),
      ],
      child: const Text('Requests', style: TextStyle(fontSize: 13, fontWeight: FontWeight.bold)),
    );
  }

  // 7. Scripts Menu
  Widget _buildScriptsMenu(BuildContext context) {
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: onScriptWorkspace ?? onManageScripts ?? () => _showRoadmapDialog(
            context,
            'Script Workspace',
            'Roadmap Phase 7',
            'The Script Workspace provides an action-block programming environment to automate records, UI navigation, validations, and custom business logic.',
          ),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true, shift: true),
          child: const Text('Script Workspace...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('[Custom User Scripts List]'),
        ),
      ],
      child: const Text('Scripts', style: TextStyle(fontSize: 13)),
    );
  }

  // 8. Tools Menu
  Widget _buildToolsMenu(BuildContext context) {
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: () => _showRoadmapDialog(context, 'Script Debugger', 'Roadmap Phase 7', 'Step-by-step interactive script execution and breakpoint monitor.'),
          child: const Text('Script Debugger'),
        ),
        MenuItemButton(
          onPressed: () => _showRoadmapDialog(context, 'Data Viewer', 'Roadmap Phase 7', 'Real-time inspector for global variables (\$), local variables (\$\$), and watch formulas.'),
          child: const Text('Data Viewer'),
        ),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: () => _showRoadmapDialog(context, 'Custom Menus', 'Roadmap Phase 8', 'Customize, override, and reorder application menu bars per layout.'),
              child: const Text('Manage Custom Menus...'),
            ),
          ],
          child: const Text('Custom Menus'),
        ),
        MenuItemButton(
          onPressed: () => _showRoadmapDialog(context, 'Database Design Report', 'Roadmap Phase 8', 'Generate comprehensive XML/HTML schema and relationship audit documentation.'),
          child: const Text('Database Design Report...'),
        ),
        MenuItemButton(
          onPressed: () => _showRoadmapDialog(context, 'Developer Utilities', 'Roadmap Phase 8', 'File renaming, custom extension bundling, and kiosk mode generator.'),
          child: const Text('Developer Utilities...'),
        ),
      ],
      child: const Text('Tools', style: TextStyle(fontSize: 13)),
    );
  }

  // 9. Window Menu
  Widget _buildWindowMenu(BuildContext context) {
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          shortcut: const SingleActivator(LogicalKeyboardKey.keyM, meta: true),
          child: const Text('Minimize'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Zoom'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Tile Horizontally'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Tile Vertically'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Cascade'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('New Window'),
        ),
        MenuItemButton(
          onPressed: null, // not implemented: shown disabled
          child: const Text('Show Window'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          leadingIcon: const Icon(Icons.check, size: 14),
          onPressed: () {},
          child: const Text('1 File4Base (Main Workspace)'),
        ),
      ],
      child: const Text('Window', style: TextStyle(fontSize: 13)),
    );
  }

  Future<void> _launchUrl(String url) async {
    final uri = Uri.parse(url);
    if (await canLaunchUrl(uri)) {
      await launchUrl(uri, mode: LaunchMode.externalApplication);
    }
  }

  // 10. Help Menu
  Widget _buildHelpMenu(BuildContext context) {
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: () => _launchUrl('https://file4base.github.io/file4base-app/'),
          leadingIcon: const Icon(Icons.help_center_outlined, size: 18),
          child: const Text('File4Base Help'),
        ),
        MenuItemButton(
          onPressed: () => _launchUrl('https://file4base.github.io/file4base-app/#guides'),
          leadingIcon: const Icon(Icons.menu_book_outlined, size: 18),
          child: const Text('Resource Center'),
        ),
        MenuItemButton(
          onPressed: () => _launchUrl('https://file4base.github.io/file4base-app/#rest-api'),
          leadingIcon: const Icon(Icons.description_outlined, size: 18),
          child: const Text('Product Documentation'),
        ),
        MenuItemButton(
          onPressed: () => _launchUrl('https://github.com/file4base/file4base-app/discussions'),
          leadingIcon: const Icon(Icons.forum_outlined, size: 18),
          child: const Text('File4Base Community'),
        ),
        MenuItemButton(
          onPressed: () => IssuesGuideDialog.show(context),
          leadingIcon: const Icon(Icons.support_agent_outlined, size: 18),
          child: const Text('Service & Support...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => CheckUpdatesDialog.show(context, currentVersion: '0.4.23'),
          leadingIcon: const Icon(Icons.system_update_outlined, size: 18),
          child: const Text('Check for Updates...'),
        ),
        MenuItemButton(
          onPressed: onAbout,
          leadingIcon: const Icon(Icons.info_outline, size: 18),
          child: const Text('About File4Base'),
        ),
      ],
      child: const Text('Help', style: TextStyle(fontSize: 13)),
    );
  }
}
