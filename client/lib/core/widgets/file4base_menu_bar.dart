import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../../main.dart';

class File4BaseMenuBar extends StatelessWidget {
  final OperationalMode activeMode;
  final ValueChanged<OperationalMode> onModeChanged;
  final VoidCallback onManageDatabase;
  final VoidCallback? onManageLayouts;
  final VoidCallback? onManageSecurity;
  final VoidCallback? onManageScripts;
  final VoidCallback? onScriptWorkspace;
  final VoidCallback? onManageThemes;
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
  final VoidCallback? onShowAllRecords;
  final VoidCallback? onPerformFind;
  final VoidCallback? onSaveLayout;
  final bool isToolbarVisible;
  final ValueChanged<bool> onToggleToolbar;

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
    required this.onOpenRemote,
    required this.onAbout,
    this.onNewDatabase,
    this.onOpenSolution,
    this.onSave,
    this.onSaveAs,
    this.onSaveCopyAs,
    this.onExportData,
    this.onFileOptions,
    this.onPageSetup,
    this.onNewRecord,
    this.onDuplicateRecord,
    this.onDeleteRecord,
    this.onShowAllRecords,
    this.onPerformFind,
    this.onSaveLayout,
    required this.isToolbarVisible,
    required this.onToggleToolbar,
  });

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
        MenuItemButton(
          onPressed: onNewDatabase ?? () => _showNotice(context, 'New Database', 'Create a new table in Manage > Database.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyN, meta: true),
          child: const Text('New Database...'),
        ),
        MenuItemButton(
          onPressed: onOpenSolution ?? () => _showNotice(context, 'Open', 'Select a local File4Base solution (.f4p).'),
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
          onPressed: onSave ?? () => _showNotice(context, 'Save', 'Solution saved.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true),
          child: const Text('Save'),
        ),
        MenuItemButton(
          onPressed: onSaveAs ?? () => _showNotice(context, 'Save As', 'Save solution with a new name.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true, shift: true),
          child: const Text('Save As...'),
        ),
        MenuItemButton(
          onPressed: onSaveCopyAs ?? () => _showNotice(context, 'Save a Copy As', 'Full copy or database data file.'),
          child: const Text('Save a Copy As...'),
        ),
        MenuItemButton(
          onPressed: onExportData ?? () => _showNotice(context, 'Export Data', 'Saves a .f4data snapshot of all database rows. Structure is auto-saved separately.'),
          child: const Text('Export Data...'),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Open Favorite', 'Local Server (http://localhost:8080)'),
              child: const Text('Local Host (localhost:8080)'),
            ),
          ],
          child: const Text('Open Favorite'),
        ),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Open Recent', 'default_workspace'),
              child: const Text('default_workspace (PostgreSQL)'),
            ),
          ],
          child: const Text('Open Recent'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Close', 'Workspace window closed.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyW, meta: true),
          child: const Text('Close'),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: onManageDatabase,
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
              onPressed: () => _showRoadmapDialog(context, 'Value Lists', 'Roadmap Phase 5', 'Custom static value lists and dynamic relation-driven lists.'),
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
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: () => _showRoadmapDialog(context, 'Share with Clients', 'Roadmap Phase 6', 'Multi-user real-time synchronization over WebSockets and TCP.'),
              child: const Text('Share with File4Base Clients...'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'WebDirect', 'WebDirect is active on port 3000.'),
              child: const Text('Enable File4Base WebDirect...'),
            ),
            MenuItemButton(
              onPressed: () => _showRoadmapDialog(context, 'ODBC / JDBC', 'Roadmap Phase 9', 'Direct SQL wire protocol compatibility.'),
              child: const Text('Share with ODBC/JDBC...'),
            ),
          ],
          child: const Text('Sharing'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onFileOptions ?? () => _showNotice(context, 'File Options', 'Startup script, default credentials, and encryption.'),
          child: const Text('File Options...'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Change Password', 'Update account password.'),
          child: const Text('Change Password...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onPageSetup ?? () => onModeChanged(OperationalMode.preview),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyP, meta: true, shift: true),
          child: const Text('Page Setup...'),
        ),
        MenuItemButton(
          onPressed: () => onModeChanged(OperationalMode.preview),
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
          onPressed: () => _showRoadmapDialog(context, 'Export Records', 'Roadmap Phase 4', 'Export found set to CSV, JSON, or Excel format.'),
          child: const Text('Export Records...'),
        ),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Save as Excel', 'Generating Excel spreadsheet of active records.'),
              child: const Text('Excel...'),
            ),
            MenuItemButton(
              onPressed: () => onModeChanged(OperationalMode.preview),
              child: const Text('PDF...'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Save Snapshot Link', 'Exporting found set snapshot file4base link.'),
              child: const Text('Snapshot Link...'),
            ),
          ],
          child: const Text('Save/Send Records As'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Recover', 'Check database consistency and index integrity.'),
          child: const Text('Recover...'),
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
          onPressed: () => _showNotice(context, 'Undo', 'Revert last action.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyZ, meta: true),
          child: const Text('Undo'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Redo', 'Reapply undone action.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyZ, meta: true, shift: true),
          child: const Text('Redo'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Cut', 'Selection cut to clipboard.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyX, meta: true),
          child: const Text('Cut'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Copy', 'Selection copied to clipboard.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyC, meta: true),
          child: const Text('Copy'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Paste', 'Clipboard pasted.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyV, meta: true),
          child: const Text('Paste'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Clear', 'Selection cleared.'),
          child: const Text('Clear'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Select All', 'All elements selected.'),
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
              onPressed: () => _showNotice(context, 'Find Next', 'Searching next occurrence.'),
              child: const Text('Find Next'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Find Previous', 'Searching previous occurrence.'),
              child: const Text('Find Previous'),
            ),
          ],
          child: const Text('Find/Replace'),
        ),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Check Selection', 'Spell check active selection.'),
              child: const Text('Check Selection...'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Check Record', 'Spell check active record fields.'),
              child: const Text('Check Record...'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Correct Word', 'Suggested spelling correction.'),
              child: const Text('Correct Word...'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Dictionaries', 'Manage installed language dictionaries.'),
              child: const Text('Dictionaries...'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Edit User Dictionary', 'Custom vocabulary words.'),
              child: const Text('Edit User Dictionary...'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Spelling Options', 'Case sensitivity and auto-correction rules.'),
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
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          leadingIcon: activeMode == OperationalMode.browse ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => onModeChanged(OperationalMode.browse),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyB, meta: true),
          child: const Text('Browse Mode'),
        ),
        MenuItemButton(
          leadingIcon: activeMode == OperationalMode.find ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => onModeChanged(OperationalMode.find),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyF, meta: true),
          child: const Text('Find Mode'),
        ),
        MenuItemButton(
          leadingIcon: activeMode == OperationalMode.layout ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => onModeChanged(OperationalMode.layout),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyL, meta: true),
          child: const Text('Layout Mode'),
        ),
        MenuItemButton(
          leadingIcon: activeMode == OperationalMode.preview ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => onModeChanged(OperationalMode.preview),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyU, meta: true),
          child: const Text('Preview Mode'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Page Margins', 'Toggle paper boundary margins.'),
          child: const Text('Page Margins'),
        ),
        MenuItemButton(
          leadingIcon: isToolbarVisible ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => onToggleToolbar(!isToolbarVisible),
          child: const Text('Status Toolbar'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Formatting Bar', 'Toggle typography format bar.'),
          child: const Text('Formatting Bar'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Text Ruler', 'Toggle coordinate rulers.'),
          child: const Text('Text Ruler'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Zoom In', 'Scale view to 125%.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.equal, meta: true),
          child: const Text('Zoom In'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Zoom Out', 'Scale view to 75%.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.minus, meta: true),
          child: const Text('Zoom Out'),
        ),
      ],
      child: const Text('View', style: TextStyle(fontSize: 13)),
    );
  }

  // 4. Insert Menu
  Widget _buildInsertMenu(BuildContext context) {
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Insert Picture', 'Select image file (PNG, JPG, WebP) to insert into field.'),
          child: const Text('Picture...'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Insert Audio/Video', 'Select multimedia stream or file.'),
          child: const Text('Audio/Video...'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Insert PDF', 'Attach PDF document.'),
          child: const Text('PDF...'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Insert QuickTime', 'Embed QuickTime compatible stream.'),
          child: const Text('QuickTime...'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Insert File', 'Store raw binary attachment.'),
          child: const Text('File...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Current Date', DateTime.now().toIso8601String().split('T').first),
          child: const Text('Current Date'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Current Time', TimeOfDay.now().format(context)),
          child: const Text('Current Time'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Current User Name', 'Admin'),
          child: const Text('Current User Name'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'From Index', 'Select value from indexed column values.'),
          child: const Text('From Index...'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'From Last Visited Record', 'Duplicate field value from previous record.'),
          child: const Text('From Last Visited Record'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Merge Field', 'Insert dynamic {{Field}} merge marker.'),
          child: const Text('Merge Field...'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Merge Variable', 'Insert dynamic \$\$Variable marker.'),
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
          onPressed: () => _showNotice(context, 'Font', 'System fonts: Inter, Roboto, SF Pro, Segoe UI.'),
          child: const Text('Font'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Size', 'Select typography font size.'),
          child: const Text('Size'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Style', 'Bold, Italic, Underline, Strikethrough.'),
          child: const Text('Style'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Align Text', 'Left, Center, Right, Justify.'),
          child: const Text('Align Text'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Line Spacing', 'Single, 1.5 lines, Double.'),
          child: const Text('Line Spacing'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Text Color', 'Choose theme typography swatch.'),
          child: const Text('Text Color'),
        ),
        if (activeMode == OperationalMode.layout) ...[
          const Divider(height: 1),
          MenuItemButton(
            onPressed: () => _showNotice(context, 'Align Object', 'Align selected components to canvas grid.'),
            child: const Text('Align Object (Layout Mode)'),
          ),
          MenuItemButton(
            onPressed: () => _showNotice(context, 'Distribute', 'Equalize spacing across components.'),
            child: const Text('Distribute (Layout Mode)'),
          ),
          MenuItemButton(
            onPressed: () => _showNotice(context, 'Resize', 'Equalize width and height across components.'),
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
          onPressed: onNewRecord ?? () => _showNotice(context, 'New Record', 'Insert new record into active table.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyN, meta: true),
          child: const Text('New Record'),
        ),
        MenuItemButton(
          onPressed: onDuplicateRecord ?? () => _showNotice(context, 'Duplicate Record', 'Clone active record.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyD, meta: true),
          child: const Text('Duplicate Record'),
        ),
        MenuItemButton(
          onPressed: onDeleteRecord ?? () => _showNotice(context, 'Delete Record', 'Delete current record.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyE, meta: true),
          child: const Text('Delete Record...'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Delete All Records', 'Truncate active found set.'),
          child: const Text('Delete All Records...'),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Go to Record', 'First record.'),
              child: const Text('First'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Go to Record', 'Previous record.'),
              child: const Text('Previous'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Go to Record', 'Next record.'),
              child: const Text('Next'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Go to Record', 'Last record.'),
              child: const Text('Last'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Go to Record', 'Jump to record number.'),
              child: const Text('By Number...'),
            ),
          ],
          child: const Text('Go to Record'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onShowAllRecords ?? () => _showNotice(context, 'Show All Records', 'Clear active find criteria.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyJ, meta: true),
          child: const Text('Show All Records'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Show Omitted Only', 'Invert found set.'),
          child: const Text('Show Omitted Only'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Omit Record', 'Temporarily hide current row from found set.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyM, meta: true),
          child: const Text('Omit Record'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Omit Multiple', 'Omit N consecutive records.'),
          child: const Text('Omit Multiple...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Sort Records', 'Define multi-column ordering.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true),
          child: const Text('Sort Records...'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Unsort', 'Restore natural creation index order.'),
          child: const Text('Unsort'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showRoadmapDialog(context, 'Replace Field Contents', 'Roadmap Phase 4', 'Batch replace field values across all found records with calculation, serial, or constant.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.equal, meta: true),
          child: const Text('Replace Field Contents...'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Relookup Field Contents', 'Re-trigger lookup values based on relationship.'),
          child: const Text('Relookup Field Contents'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Revert Record', 'Discard uncommitted row modifications.'),
          child: const Text('Revert Record'),
        ),
      ],
      child: const Text('Records', style: TextStyle(fontSize: 13)),
    );
  }

  // 6b. Requests Menu (Find Mode)
  Widget _buildRequestsMenu(BuildContext context) {
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: onPerformFind ?? () => _showNotice(context, 'Perform Find', 'Executing SQL query from find criteria.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.enter),
          child: const Text('Perform Find'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'New Request', 'Adding disjunctive (OR) find request criteria.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyN, meta: true),
          child: const Text('New Request'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Duplicate Request', 'Cloning current find criteria.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyD, meta: true),
          child: const Text('Duplicate Request'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Delete Request', 'Removing active find request.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyE, meta: true),
          child: const Text('Delete Request'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Delete All Requests', 'Clearing all criteria.'),
          child: const Text('Delete All Requests'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Include / Omit', 'Toggle matching vs omission filter.'),
          child: const Text('Include / Omit'),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Go to Request', 'First request.'),
              child: const Text('First'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Go to Request', 'Previous request.'),
              child: const Text('Previous'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Go to Request', 'Next request.'),
              child: const Text('Next'),
            ),
            MenuItemButton(
              onPressed: () => _showNotice(context, 'Go to Request', 'Last request.'),
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
          onPressed: () => _showNotice(context, 'User Scripts', 'Custom user scripts will appear in this menu.'),
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
          onPressed: () => _showNotice(context, 'Minimize', 'Window minimized to taskbar/dock.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyM, meta: true),
          child: const Text('Minimize'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Zoom', 'Window maximized to screen bounds.'),
          child: const Text('Zoom'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Tile Horizontally', 'Arranging active windows horizontally.'),
          child: const Text('Tile Horizontally'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Tile Vertically', 'Arranging active windows vertically.'),
          child: const Text('Tile Vertically'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Cascade', 'Stacking active windows in cascade.'),
          child: const Text('Cascade'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'New Window', 'Opening secondary workspace instance.'),
          child: const Text('New Window'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Show Window', 'Focusing primary File4Base window.'),
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

  // 10. Help Menu
  Widget _buildHelpMenu(BuildContext context) {
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: () => _showNotice(context, 'File4Base Help', 'File4Base User & Developer Guide.'),
          child: const Text('File4Base Help'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Resource Center', 'Tutorials, sample templates, and community guides.'),
          child: const Text('Resource Center'),
        ),
        MenuItemButton(
          onPressed: () => _showRoadmapDialog(
            context,
            'Product Documentation',
            'Architecture & API',
            'Documentation available in docs/api/API_REFERENCE.md and docs/specs/file4base_menu_reference_guide.md.',
          ),
          child: const Text('Product Documentation'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'File4Base Community', 'https://github.com/file4base/file4base-app/discussions'),
          child: const Text('File4Base Community'),
        ),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Service & Support', 'https://github.com/file4base/file4base-app/issues'),
          child: const Text('Service & Support'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showNotice(context, 'Check for Updates', 'File4Base is up to date (v0.2.0).'),
          child: const Text('Check for Updates...'),
        ),
        MenuItemButton(
          onPressed: onAbout,
          child: const Text('About File4Base'),
        ),
      ],
      child: const Text('Help', style: TextStyle(fontSize: 13)),
    );
  }
}
