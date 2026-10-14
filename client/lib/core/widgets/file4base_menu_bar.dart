import 'package:file4base_client/core/api/api_client.dart'
    show CustomMenuItemModel, MenuSetModel, SavedFindModel;
import 'package:file4base_client/core/services/solution_storage.dart';
import 'package:file4base_client/features/help/check_updates_dialog.dart';
import 'package:file4base_client/features/help/issues_guide_dialog.dart';
import 'package:file4base_client/features/layout_engine/layout_object_visuals.dart'
    show LayoutMergeSymbols, parseLayoutColor;
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart'
    show LayoutObjectStyle;
import 'package:file4base_client/features/layout_engine/view_format_tools.dart'
    show kFormatColorSwatches, kLayoutFontFamilies, kLayoutFontSizes;
import 'package:file4base_client/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:url_launcher/url_launcher.dart';

class File4BaseMenuBar extends StatelessWidget {

  const File4BaseMenuBar({
    required this.activeMode, required this.onModeChanged, required this.onManageDatabase, required this.onOpenRemote, required this.onAbout, required this.isToolbarVisible, required this.onToggleToolbar, super.key,
    this.onManageLayouts,
    this.onManageSecurity,
    this.onManageScripts,
    this.onScriptWorkspace,
    this.onManageThemes,
    this.onManageValueLists,
    this.onNewDatabase,
    this.onOpenSolution,
    this.onSave,
    this.onSaveAs,
    this.onSaveCopyAs,
    this.onExportData,
    this.onImportData,
    this.onFileOptions,
    this.onChangePassword,
    this.onPageSetup,
    this.onPrint,
    this.onQuit,
    this.onExportRecords,
    this.onImportRecords,
    this.onImportFromDataSource,
    this.onManageDataSources,
    this.onDesignReport,
    this.onSaveDesignAsText,
    this.onDataViewer,
    this.onScriptDebugger,
    this.onManageCustomMenus,
    this.menuSets = const [],
    this.activeMenuSet,
    this.onSelectMenuSet,
    this.onRunCustomMenuItem,
    this.onDeveloperUtilities,
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
    this.savedFinds = const [],
    this.onRunSavedFind,
    this.onSaveCurrentFind,
    this.onManageSavedFinds,
    this.onSaveLayout,
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
    this.onUndo,
    this.onRedo,
    this.onCut,
    this.onCopy,
    this.onPaste,
    this.onClear,
    this.onSelectAll,
    this.onServerToolSelected,
    this.isPageMarginsVisible = false,
    this.onTogglePageMargins,
    this.isFormattingBarVisible = false,
    this.onToggleFormattingBar,
    this.isTextRulerVisible = false,
    this.onToggleTextRuler,
    this.activeObjectStyle = const LayoutObjectStyle(),
    this.onFormatStyleChanged,
    this.onOpenTextFormat,
    this.onOpenNumberFormat,
    this.onOpenDateFormat,
    this.onOpenTimeFormat,
    this.onOpenTimestampFormat,
    this.onOpenConditionalFormatting,
    this.onAlignObjects,
    this.onDistributeObjects,
    this.onResizeObjects,
    this.containerUpdateAvailable = false,
    this.containerLatestVersion,
  });
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
  final VoidCallback? onImportData;
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

  /// Records > Saved Finds (#34): the finds saved on the table in hand, the
  /// action that saves what Find mode holds now, and the one that opens the
  /// list to rename or delete them.
  final List<SavedFindModel> savedFinds;
  final ValueChanged<SavedFindModel>? onRunSavedFind;
  final VoidCallback? onSaveCurrentFind;
  final VoidCallback? onManageSavedFinds;
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

  /// File > Import Records > File... — brings records in from a CSV, a
  /// tab-separated file, an Excel workbook or XML (#38).
  final VoidCallback? onImportRecords;

  /// File > Import Records > External SQL Data Source... — reads the records
  /// out of another SQL database instead of a file (#47).
  final VoidCallback? onImportFromDataSource;

  /// File > Manage > Data Sources... — the registered connections (#47).
  final VoidCallback? onManageDataSources;

  /// Tools > Database Design Report... — what the solution is made of, as a
  /// page to read, and Save Design as XML... — the same as text, for version
  /// control (#49).
  final VoidCallback? onDesignReport;
  final VoidCallback? onSaveDesignAsText;

  /// Tools > Data Viewer — the fields of the record in hand and the
  /// expressions being watched (#50).
  final VoidCallback? onDataViewer;

  /// Tools > Script Debugger — a script run one step at a time (#51).
  final VoidCallback? onScriptDebugger;

  /// Tools > Custom Menus (#52): the solution's custom menu sets, the one
  /// currently active, the action to open the manager dialog, and the runner
  /// for custom menu items.
  final VoidCallback? onManageCustomMenus;
  final List<MenuSetModel> menuSets;
  final MenuSetModel? activeMenuSet;
  final ValueChanged<MenuSetModel?>? onSelectMenuSet;
  final ValueChanged<CustomMenuItemModel>? onRunCustomMenuItem;

  /// Tools > Developer Utilities... (#53).
  final VoidCallback? onDeveloperUtilities;

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

  /// Edit menu actions (`Undo`, `Redo`, `Cut`, `Copy`, `Paste`, `Clear`,
  /// `Select All`). When omitted, `File4BaseMenuBar` dispatches directly to the
  /// active or most recently focused `EditableTextState` via
  /// `EditMenuFocusTracker`.
  final VoidCallback? onUndo;
  final VoidCallback? onRedo;
  final VoidCallback? onCut;
  final VoidCallback? onCopy;
  final VoidCallback? onPaste;
  final VoidCallback? onClear;
  final VoidCallback? onSelectAll;
  final ValueChanged<String>? onServerToolSelected;
  final bool isPageMarginsVisible;
  final ValueChanged<bool>? onTogglePageMargins;
  final bool isFormattingBarVisible;
  final ValueChanged<bool>? onToggleFormattingBar;
  final bool isTextRulerVisible;
  final ValueChanged<bool>? onToggleTextRuler;
  final LayoutObjectStyle activeObjectStyle;
  final ValueChanged<LayoutObjectStyle>? onFormatStyleChanged;
  final VoidCallback? onOpenTextFormat;
  final VoidCallback? onOpenNumberFormat;
  final VoidCallback? onOpenDateFormat;
  final VoidCallback? onOpenTimeFormat;
  final VoidCallback? onOpenTimestampFormat;
  final VoidCallback? onOpenConditionalFormatting;
  final ValueChanged<String>? onAlignObjects;
  final ValueChanged<String>? onDistributeObjects;
  final ValueChanged<String>? onResizeObjects;
  final bool containerUpdateAvailable;
  final String? containerLatestVersion;

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
                color: const Color(0xFF1E88E5).withValues(alpha: 0.12),
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
    EditMenuFocusTracker.instance.ensureListening(context);
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
          _applyCustomMenu('File', _buildFileMenu(context), extendOnly: true),
          if (!_isMenuHidden('Edit'))
            _applyCustomMenu('Edit', _buildEditMenu(context)),
          if (!_isMenuHidden('View'))
            _applyCustomMenu('View', _buildViewMenu(context)),
          if (!_isMenuHidden('Insert'))
            _applyCustomMenu('Insert', _buildInsertMenu(context)),
          if (!_isMenuHidden('Format'))
            _applyCustomMenu('Format', _buildFormatMenu(context)),
          if (activeMode == OperationalMode.find) ...[
            if (!_isMenuHidden('Requests'))
              _applyCustomMenu('Requests', _buildRequestsMenu(context)),
          ] else ...[
            if (!_isMenuHidden('Records'))
              _applyCustomMenu('Records', _buildRecordsMenu(context)),
          ],
          if (!_isMenuHidden('Scripts'))
            _applyCustomMenu('Scripts', _buildScriptsMenu(context)),
          _applyCustomMenu(
            'Tools',
            _buildToolsMenu(context),
            preservedSuffix: [_buildCustomMenusSubmenu()],
          ),
          if (!_isMenuHidden('Server Tools'))
            _applyCustomMenu('Server Tools', _buildServerToolsMenu(context)),
          if (!_isMenuHidden('Window'))
            _applyCustomMenu('Window', _buildWindowMenu(context)),
          _buildHelpMenu(context),
        ],
      ),
    );
  }

  bool _isMenuHidden(String menuName) {
    final custom = activeMenuSet?.menuFor(menuName);
    return custom != null && custom.mode.toLowerCase() == 'hide';
  }

  Widget _applyCustomMenu(
    String menuName,
    Widget standard, {
    bool extendOnly = false,
    List<Widget> preservedSuffix = const [],
  }) {
    if (standard is! SubmenuButton) return standard;
    final custom = activeMenuSet?.menuFor(menuName);
    if (custom == null) return standard;
    if (!extendOnly && custom.mode.toLowerCase() == 'hide') {
      if (preservedSuffix.isNotEmpty) {
        return SubmenuButton(
          menuChildren: preservedSuffix,
          child: standard.child,
        );
      }
      return standard;
    }
    if (custom.items.isEmpty) return standard;

    final customWidgets = <Widget>[
      for (final item in custom.items)
        if (item.separator)
          const Divider(height: 1)
        else
          MenuItemButton(
            onPressed: onRunCustomMenuItem == null
                ? null
                : () => onRunCustomMenuItem!(item),
            shortcut: _parseShortcut(item.shortcut),
            child: Text(item.title),
          ),
    ];

    final replace = !extendOnly && custom.mode.toLowerCase() == 'replace';
    final children = replace
        ? <Widget>[
            ...customWidgets,
            if (preservedSuffix.isNotEmpty) ...[
              const Divider(height: 1),
              ...preservedSuffix,
            ],
          ]
        : <Widget>[
            ...standard.menuChildren,
            const Divider(height: 1),
            ...customWidgets,
          ];

    return SubmenuButton(
      menuChildren: children,
      child: standard.child,
    );
  }

  SingleActivator? _parseShortcut(String raw) {
    final trimmed = raw.trim();
    if (trimmed.isEmpty) return null;
    var meta = false;
    var control = false;
    var shift = false;
    var alt = false;
    LogicalKeyboardKey? key;
    for (final part in trimmed.split('+')) {
      final token = part.trim().toLowerCase();
      switch (token) {
        case 'cmd':
        case 'command':
        case 'meta':
          meta = true;
        case 'ctrl':
        case 'control':
          control = true;
        case 'shift':
          shift = true;
        case 'alt':
        case 'option':
        case 'opt':
          alt = true;
        default:
          key = _logicalKeyForToken(token);
      }
    }
    if (key == null) return null;
    // Do not bind reserved mode or quit shortcuts on a custom menu item.
    if ((meta || control) && !shift && !alt) {
      if (key == LogicalKeyboardKey.keyB ||
          key == LogicalKeyboardKey.keyF ||
          key == LogicalKeyboardKey.keyL ||
          key == LogicalKeyboardKey.keyU ||
          key == LogicalKeyboardKey.keyQ) {
        return null;
      }
    }
    return SingleActivator(
      key,
      meta: meta,
      control: control,
      shift: shift,
      alt: alt,
    );
  }

  LogicalKeyboardKey? _logicalKeyForToken(String token) {
    if (token.length == 1) {
      final code = token.codeUnitAt(0);
      if (code >= 97 && code <= 122) {
        return LogicalKeyboardKey(0x00000061 + (code - 97));
      }
      if (code >= 48 && code <= 57) {
        return LogicalKeyboardKey(0x00000030 + (code - 48));
      }
    }
    return null;
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
              : onSave,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true),
          child: const Text('Save'),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, 'Authentication Required', 'Please sign in to save solution changes.')
              : onSaveAs,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true, shift: true),
          child: const Text('Save As...'),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, 'Authentication Required', 'Please sign in to save a copy.')
              : onSaveCopyAs,
          child: const Text('Save a Copy As...'),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, 'Authentication Required', 'Please sign in to export database data.')
              : onExportData,
          child: const Text('Export Data...'),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, 'Authentication Required', 'Please sign in to import database data.')
              : onImportData,
          child: const Text('Import Data (.f4data)...'),
        ),
        const Divider(height: 1),
        const SubmenuButton(
          menuChildren: [
            MenuItemButton(
              child: Text('Local Host (localhost:8080)'),
            ),
          ],
          child: Text('Open Favorite'),
        ),
        const SubmenuButton(
          menuChildren: [
            MenuItemButton(
              child: Text('default_workspace (PostgreSQL)'),
            ),
          ],
          child: Text('Open Recent'),
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
              onPressed: onManageDataSources,
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
              : onChangePassword,
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
              onPressed: !isAuthenticated
                  ? () => _showNotice(context, 'Authentication Required',
                      'Please sign in to import records.')
                  : onImportRecords,
              child: const Text('File...'),
            ),
            // Importing a folder of pictures into container fields is a
            // separate piece of work; the item is shown disabled rather than
            // opening a dialog that does nothing (#38).
            const MenuItemButton(
              child: Text('Folder...'),
            ),
            MenuItemButton(
              onPressed: !isAuthenticated
                  ? () => _showNotice(context, 'Authentication Required',
                      'Please sign in to import records.')
                  : onImportRecords,
              child: const Text('XML Data Source...'),
            ),
            // Not ODBC: File4Base reads the engines it has Go drivers for,
            // and the item says so rather than promising a bridge (#47).
            MenuItemButton(
              onPressed: !isAuthenticated
                  ? () => _showNotice(context, 'Authentication Required',
                      'Please sign in to import records.')
                  : onImportFromDataSource,
              child: const Text('External SQL Data Source...'),
            ),
          ],
          child: const Text('Import Records'),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, 'Authentication Required', 'Please sign in to export records.')
              : onExportRecords,
          child: const Text('Export Records...'),
        ),
        const MenuItemButton(
          child: Text('Recover...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onQuit ?? SolutionStorageService.triggerQuit,
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
          onPressed: () {
            if (onUndo != null) {
              onUndo!();
            } else {
              EditMenuFocusTracker.instance.undoActiveText(context);
            }
          },
          shortcut: const SingleActivator(LogicalKeyboardKey.keyZ, meta: true),
          child: const Text('Undo'),
        ),
        MenuItemButton(
          onPressed: () {
            if (onRedo != null) {
              onRedo!();
            } else {
              EditMenuFocusTracker.instance.redoActiveText(context);
            }
          },
          shortcut: const SingleActivator(LogicalKeyboardKey.keyZ, meta: true, shift: true),
          child: const Text('Redo'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () {
            if (onCut != null) {
              onCut!();
            } else {
              EditMenuFocusTracker.instance.cutActiveText(context);
            }
          },
          shortcut: const SingleActivator(LogicalKeyboardKey.keyX, meta: true),
          child: const Text('Cut'),
        ),
        MenuItemButton(
          onPressed: () {
            if (onCopy != null) {
              onCopy!();
            } else {
              EditMenuFocusTracker.instance.copyActiveText(context);
            }
          },
          shortcut: const SingleActivator(LogicalKeyboardKey.keyC, meta: true),
          child: const Text('Copy'),
        ),
        MenuItemButton(
          onPressed: () {
            if (onPaste != null) {
              onPaste!();
            } else {
              EditMenuFocusTracker.instance.pasteActiveText(context);
            }
          },
          shortcut: const SingleActivator(LogicalKeyboardKey.keyV, meta: true),
          child: const Text('Paste'),
        ),
        MenuItemButton(
          onPressed: () {
            if (onClear != null) {
              onClear!();
            } else {
              EditMenuFocusTracker.instance.clearActiveText(context);
            }
          },
          child: const Text('Clear'),
        ),
        MenuItemButton(
          onPressed: () {
            if (onSelectAll != null) {
              onSelectAll!();
            } else {
              EditMenuFocusTracker.instance.selectAllActiveText(context);
            }
          },
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
            const MenuItemButton(
              child: Text('Find Next'),
            ),
            const MenuItemButton(
              child: Text('Find Previous'),
            ),
          ],
          child: const Text('Find/Replace'),
        ),
        const SubmenuButton(
          menuChildren: [
            MenuItemButton(
              child: Text('Check Selection...'),
            ),
            MenuItemButton(
              child: Text('Check Record...'),
            ),
            MenuItemButton(
              child: Text('Correct Word...'),
            ),
            MenuItemButton(
              child: Text('Dictionaries...'),
            ),
            MenuItemButton(
              child: Text('Edit User Dictionary...'),
            ),
            MenuItemButton(
              child: Text('Options...'),
            ),
          ],
          child: Text('Spelling'),
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
          leadingIcon: isPageMarginsVisible ? const Icon(Icons.check, size: 14) : null,
          onPressed: onTogglePageMargins != null
              ? () => onTogglePageMargins!(!isPageMarginsVisible)
              : null,
          child: const Text('Page Margins'),
        ),
        MenuItemButton(
          leadingIcon: isToolbarVisible ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => onToggleToolbar(!isToolbarVisible),
          child: const Text('Status Toolbar'),
        ),
        MenuItemButton(
          leadingIcon: isFormattingBarVisible ? const Icon(Icons.check, size: 14) : null,
          onPressed: onToggleFormattingBar != null
              ? () => onToggleFormattingBar!(!isFormattingBarVisible)
              : null,
          child: const Text('Formatting Bar'),
        ),
        MenuItemButton(
          leadingIcon: isTextRulerVisible ? const Icon(Icons.check, size: 14) : null,
          onPressed: onToggleTextRuler != null
              ? () => onToggleTextRuler!(!isTextRulerVisible)
              : null,
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
        const MenuItemButton(
          child: Text('From Index...'),
        ),
        const MenuItemButton(
          child: Text('From Last Visited Record'),
        ),
        MenuItemButton(
          onPressed: onInsertMergeField,
          child: const Text('Merge Field...'),
        ),
        const MenuItemButton(
          child: Text('Merge Variable...'),
        ),
      ],
      child: const Text('Insert', style: TextStyle(fontSize: 13)),
    );
  }

  // 5. Format Menu
  Widget _buildFormatMenu(BuildContext context) {
    final s = activeObjectStyle;
    final apply = onFormatStyleChanged;

    return SubmenuButton(
      menuChildren: [
        // 1. Font Submenu
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              leadingIcon: (s.fontFamily == null || s.fontFamily!.isEmpty)
                  ? const Icon(Icons.check, size: 14)
                  : null,
              onPressed: apply != null
                  ? () => apply(s.copyWith(clearFontFamily: true))
                  : null,
              child: const Text('System Default'),
            ),
            const Divider(height: 1),
            for (final fam in kLayoutFontFamilies)
              MenuItemButton(
                leadingIcon: s.fontFamily == fam
                    ? const Icon(Icons.check, size: 14)
                    : null,
                onPressed: apply != null
                    ? () => apply(s.copyWith(fontFamily: fam))
                    : null,
                child: Text(fam, style: TextStyle(fontFamily: fam)),
              ),
          ],
          child: const Text('Font'),
        ),

        // 2. Size Submenu
        SubmenuButton(
          menuChildren: [
            for (final sz in kLayoutFontSizes)
              MenuItemButton(
                leadingIcon: (s.fontSize - sz).abs() < 0.1
                    ? const Icon(Icons.check, size: 14)
                    : null,
                onPressed: apply != null
                    ? () => apply(s.copyWith(fontSize: sz))
                    : null,
                child: Text('${sz.round()} pt'),
              ),
          ],
          child: const Text('Size'),
        ),

        // 3. Style Submenu
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              leadingIcon: s.fontWeight != 'bold' &&
                      s.fontStyle != 'italic' &&
                      !s.underline &&
                      !s.strikethrough &&
                      s.textCase == 'none'
                  ? const Icon(Icons.check, size: 14)
                  : null,
              onPressed: apply != null
                  ? () => apply(
                        s.copyWith(
                          fontWeight: 'normal',
                          fontStyle: 'normal',
                          underline: false,
                          strikethrough: false,
                          textCase: 'none',
                        ),
                      )
                  : null,
              child: const Text('Plain Text'),
            ),
            const Divider(height: 1),
            MenuItemButton(
              leadingIcon: s.fontWeight == 'bold'
                  ? const Icon(Icons.check, size: 14)
                  : null,
              onPressed: apply != null
                  ? () => apply(
                        s.copyWith(
                          fontWeight: s.fontWeight == 'bold' ? 'normal' : 'bold',
                        ),
                      )
                  : null,
              child: const Text('Bold', style: TextStyle(fontWeight: FontWeight.bold)),
            ),
            MenuItemButton(
              leadingIcon: s.fontStyle == 'italic'
                  ? const Icon(Icons.check, size: 14)
                  : null,
              onPressed: apply != null
                  ? () => apply(
                        s.copyWith(
                          fontStyle: s.fontStyle == 'italic' ? 'normal' : 'italic',
                        ),
                      )
                  : null,
              child: const Text('Italic', style: TextStyle(fontStyle: FontStyle.italic)),
            ),
            MenuItemButton(
              leadingIcon: s.underline ? const Icon(Icons.check, size: 14) : null,
              onPressed: apply != null
                  ? () => apply(s.copyWith(underline: !s.underline))
                  : null,
              child: const Text(
                'Underline',
                style: TextStyle(decoration: TextDecoration.underline),
              ),
            ),
            MenuItemButton(
              leadingIcon: s.strikethrough ? const Icon(Icons.check, size: 14) : null,
              onPressed: apply != null
                  ? () => apply(s.copyWith(strikethrough: !s.strikethrough))
                  : null,
              child: const Text(
                'Strikethrough',
                style: TextStyle(decoration: TextDecoration.lineThrough),
              ),
            ),
            const Divider(height: 1),
            MenuItemButton(
              leadingIcon: s.textCase == 'upper' ? const Icon(Icons.check, size: 14) : null,
              onPressed: apply != null
                  ? () => apply(
                        s.copyWith(
                          textCase: s.textCase == 'upper' ? 'none' : 'upper',
                        ),
                      )
                  : null,
              child: const Text('UPPERCASE'),
            ),
            MenuItemButton(
              leadingIcon: s.textCase == 'lower' ? const Icon(Icons.check, size: 14) : null,
              onPressed: apply != null
                  ? () => apply(
                        s.copyWith(
                          textCase: s.textCase == 'lower' ? 'none' : 'lower',
                        ),
                      )
                  : null,
              child: const Text('lowercase'),
            ),
            MenuItemButton(
              leadingIcon: s.textCase == 'title' ? const Icon(Icons.check, size: 14) : null,
              onPressed: apply != null
                  ? () => apply(
                        s.copyWith(
                          textCase: s.textCase == 'title' ? 'none' : 'title',
                        ),
                      )
                  : null,
              child: const Text('Title Case'),
            ),
          ],
          child: const Text('Style'),
        ),

        // 4. Align Text Submenu
        SubmenuButton(
          menuChildren: [
            for (final entry in const [
              ('left', 'Left', Icons.format_align_left),
              ('center', 'Center', Icons.format_align_center),
              ('right', 'Right', Icons.format_align_right),
              ('justify', 'Full (Justify)', Icons.format_align_justify),
            ])
              MenuItemButton(
                leadingIcon: s.textAlign == entry.$1
                    ? const Icon(Icons.check, size: 14)
                    : Icon(entry.$3, size: 14),
                onPressed: apply != null
                    ? () => apply(s.copyWith(textAlign: entry.$1))
                    : null,
                child: Text(entry.$2),
              ),
          ],
          child: const Text('Align Text'),
        ),

        // 5. Line Spacing Submenu
        SubmenuButton(
          menuChildren: [
            for (final sp in const [
              (1.0, 'Single (1.0x)'),
              (1.15, '1.15x'),
              (1.2, 'Standard (1.2x)'),
              (1.5, 'One and a Half (1.5x)'),
              (2.0, 'Double (2.0x)'),
            ])
              MenuItemButton(
                leadingIcon: (s.lineSpacing - sp.$1).abs() < 0.02
                    ? const Icon(Icons.check, size: 14)
                    : null,
                onPressed: apply != null
                    ? () => apply(s.copyWith(lineSpacing: sp.$1))
                    : null,
                child: Text(sp.$2),
              ),
          ],
          child: const Text('Line Spacing'),
        ),

        // 6. Text Color Submenu
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              leadingIcon: s.textColor == null
                  ? const Icon(Icons.check, size: 14)
                  : const Icon(Icons.block, size: 14, color: Colors.red),
              onPressed: apply != null
                  ? () => apply(s.copyWith(clearTextColor: true))
                  : null,
              child: const Text('Default / Theme Color'),
            ),
            const Divider(height: 1),
            for (final entry in kFormatColorSwatches.entries)
              MenuItemButton(
                leadingIcon: Container(
                  width: 14,
                  height: 14,
                  decoration: BoxDecoration(
                    color: parseLayoutColor(entry.key),
                    border: Border.all(
                      color: s.textColor == entry.key
                          ? const Color(0xFF0284C7)
                          : Colors.black26,
                      width: s.textColor == entry.key ? 2 : 1,
                    ),
                    borderRadius: BorderRadius.circular(3),
                  ),
                ),
                onPressed: apply != null
                    ? () => apply(s.copyWith(textColor: entry.key))
                    : null,
                child: Text(entry.value),
              ),
          ],
          child: const Text('Text Color'),
        ),

        const Divider(height: 1),
        MenuItemButton(
          leadingIcon: const Icon(Icons.font_download_outlined, size: 15),
          onPressed: onOpenTextFormat,
          child: const Text('Text...'),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.numbers, size: 15),
          onPressed: onOpenNumberFormat,
          child: const Text('Number...'),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.calendar_today_outlined, size: 15),
          onPressed: onOpenDateFormat,
          child: const Text('Date...'),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.access_time, size: 15),
          onPressed: onOpenTimeFormat,
          child: const Text('Time...'),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.schedule_outlined, size: 15),
          onPressed: onOpenTimestampFormat,
          child: const Text('Timestamp...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          leadingIcon: const Icon(Icons.auto_fix_high, size: 15),
          onPressed: onOpenConditionalFormatting,
          child: const Text('Conditional...'),
        ),

        if (activeMode == OperationalMode.layout) ...[
          const Divider(height: 1),
          SubmenuButton(
            menuChildren: [
              MenuItemButton(
                leadingIcon: const Icon(Icons.align_horizontal_left, size: 15),
                onPressed: onAlignObjects != null ? () => onAlignObjects!('left') : null,
                child: const Text('Align Left Edges'),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.align_horizontal_center, size: 15),
                onPressed: onAlignObjects != null ? () => onAlignObjects!('center_h') : null,
                child: const Text('Align Horizontal Centers'),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.align_horizontal_right, size: 15),
                onPressed: onAlignObjects != null ? () => onAlignObjects!('right') : null,
                child: const Text('Align Right Edges'),
              ),
              const Divider(height: 1),
              MenuItemButton(
                leadingIcon: const Icon(Icons.align_vertical_top, size: 15),
                onPressed: onAlignObjects != null ? () => onAlignObjects!('top') : null,
                child: const Text('Align Top Edges'),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.align_vertical_center, size: 15),
                onPressed: onAlignObjects != null ? () => onAlignObjects!('center_v') : null,
                child: const Text('Align Vertical Centers'),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.align_vertical_bottom, size: 15),
                onPressed: onAlignObjects != null ? () => onAlignObjects!('bottom') : null,
                child: const Text('Align Bottom Edges'),
              ),
            ],
            child: const Text('Align Object'),
          ),
          SubmenuButton(
            menuChildren: [
              MenuItemButton(
                leadingIcon: const Icon(Icons.horizontal_distribute, size: 15),
                onPressed: onDistributeObjects != null
                    ? () => onDistributeObjects!('horizontal')
                    : null,
                child: const Text('Distribute Horizontally'),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.vertical_distribute, size: 15),
                onPressed: onDistributeObjects != null
                    ? () => onDistributeObjects!('vertical')
                    : null,
                child: const Text('Distribute Vertically'),
              ),
            ],
            child: const Text('Distribute'),
          ),
          SubmenuButton(
            menuChildren: [
              MenuItemButton(
                leadingIcon: const Icon(Icons.swap_horiz, size: 15),
                onPressed: onResizeObjects != null ? () => onResizeObjects!('width') : null,
                child: const Text('Match Smallest/Primary Width'),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.swap_vert, size: 15),
                onPressed: onResizeObjects != null ? () => onResizeObjects!('height') : null,
                child: const Text('Match Smallest/Primary Height'),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.aspect_ratio, size: 15),
                onPressed: onResizeObjects != null ? () => onResizeObjects!('both') : null,
                child: const Text('Match Both Width & Height'),
              ),
              const Divider(height: 1),
              MenuItemButton(
                leadingIcon: const Icon(Icons.grid_on, size: 15),
                onPressed: onResizeObjects != null ? () => onResizeObjects!('grid') : null,
                child: const Text('Snap Bounds to 8pt Grid'),
              ),
            ],
            child: const Text('Resize'),
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
        const MenuItemButton(
          child: Text('Delete All Records...'),
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
        _buildSavedFindsMenu(),
        const MenuItemButton(
          child: Text('Show Omitted Only'),
        ),
        const MenuItemButton(
          shortcut: SingleActivator(LogicalKeyboardKey.keyM, meta: true),
          child: Text('Omit Record'),
        ),
        const MenuItemButton(
          child: Text('Omit Multiple...'),
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
        const MenuItemButton(
          child: Text('Relookup Field Contents'),
        ),
      ],
      child: const Text('Records', style: TextStyle(fontSize: 13)),
    );
  }

  /// Records > Saved Finds (#34): save the requests in hand under a name,
  /// run one of the finds saved on this table, or open the list to rename
  /// and delete them.
  Widget _buildSavedFindsMenu() {
    final run = onRunSavedFind;
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: onSaveCurrentFind,
          child: const Text('Save Current Find...'),
        ),
        MenuItemButton(
          onPressed: savedFinds.isEmpty ? null : onManageSavedFinds,
          child: const Text('Edit Saved Finds...'),
        ),
        const Divider(height: 1),
        if (savedFinds.isEmpty)
          const MenuItemButton(
            child: Text('No saved finds for this table'),
          )
        else
          for (final find in savedFinds)
            MenuItemButton(
              onPressed: run == null ? null : () => run(find),
              child: Text(find.name),
            ),
      ],
      child: const Text('Saved Finds'),
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
        // The same submenu as in Records, because the criteria to save are
        // the ones being typed here (#34).
        _buildSavedFindsMenu(),
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
        const MenuItemButton(
          child: Text('[Custom User Scripts List]'),
        ),
      ],
      child: const Text('Scripts', style: TextStyle(fontSize: 13)),
    );
  }

  // 8. Tools Menu
  Widget _buildToolsMenu(BuildContext context) {
    // A tool that does not exist is shown disabled. The Tools menu used to
    // open a "roadmap" dialog for each of them, which read as though the
    // tool were there (#48).
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: onScriptDebugger,
          child: const Text('Script Debugger'),
        ),
        MenuItemButton(
          onPressed: onDataViewer,
          child: const Text('Data Viewer'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onDesignReport,
          child: const Text('Database Design Report...'),
        ),
        MenuItemButton(
          onPressed: onSaveDesignAsText,
          child: const Text('Save a Copy as XML...'),
        ),
        const Divider(height: 1),
        _buildCustomMenusSubmenu(),
        MenuItemButton(
          onPressed: onDeveloperUtilities,
          child: const Text('Developer Utilities...'),
        ),
        MenuItemButton(
          onPressed: onManageThemes,
          child: const Text('Layout Themes...'),
        ),
      ],
      child: const Text('Tools', style: TextStyle(fontSize: 13)),
    );
  }

  Widget _buildCustomMenusSubmenu() {
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: onManageCustomMenus,
          child: const Text('Manage Custom Menus...'),
        ),
        if (menuSets.isNotEmpty) ...[
          const Divider(height: 1),
          MenuItemButton(
            leadingIcon: activeMenuSet == null
                ? const Icon(Icons.check, size: 14)
                : const SizedBox(width: 14),
            onPressed: onSelectMenuSet == null
                ? null
                : () => onSelectMenuSet!(null),
            child: const Text('[Standard File4Base Menus]'),
          ),
          for (final set in menuSets)
            MenuItemButton(
              leadingIcon: activeMenuSet?.id == set.id
                  ? const Icon(Icons.check, size: 14)
                  : const SizedBox(width: 14),
              onPressed: onSelectMenuSet == null
                  ? null
                  : () => onSelectMenuSet!(set),
              child: Text(set.name),
            ),
        ],
      ],
      child: const Text('Custom Menus'),
    );
  }

  // 8b. Server Tools Menu
  Widget _buildServerToolsMenu(BuildContext context) {
    VoidCallback? trigger(String key) {
      if (!isAuthenticated || onServerToolSelected == null) return null;
      return () => onServerToolSelected!(key);
    }

    final latestLabel = (containerLatestVersion != null && containerLatestVersion!.isNotEmpty)
        ? 'v$containerLatestVersion'
        : 'NEW';

    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: trigger('containers'),
          leadingIcon: Icon(
            containerUpdateAvailable ? Icons.system_update_alt : Icons.view_in_ar_outlined,
            size: 18,
            color: containerUpdateAvailable ? Colors.amber.shade800 : null,
          ),
          trailingIcon: containerUpdateAvailable
              ? Container(
                  padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                  decoration: BoxDecoration(
                    color: Colors.amber.shade700,
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: Text(
                    'UPDATE $latestLabel',
                    style: const TextStyle(fontSize: 9.5, fontWeight: FontWeight.bold, color: Colors.white),
                  ),
                )
              : null,
          child: const Text('Containers & Services...'),
        ),
        MenuItemButton(
          onPressed: trigger('backups'),
          leadingIcon: const Icon(Icons.backup_outlined, size: 18),
          child: const Text('Backups & Snapshots...'),
        ),
        MenuItemButton(
          onPressed: trigger('domain_tls'),
          leadingIcon: const Icon(Icons.https_outlined, size: 18),
          child: const Text('Domain & SSL/TLS Certificates (ACME)...'),
        ),
        MenuItemButton(
          onPressed: trigger('telemetry'),
          leadingIcon: const Icon(Icons.insights_outlined, size: 18),
          child: const Text('API Telemetry & Live Metrics...'),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: trigger('sessions'),
          leadingIcon: const Icon(Icons.people_alt_outlined, size: 18),
          child: const Text('Connected Clients & Sessions...'),
        ),
        MenuItemButton(
          onPressed: trigger('schedules'),
          leadingIcon: const Icon(Icons.schedule_outlined, size: 18),
          child: const Text('Scheduled Tasks & Cron Scripts...'),
        ),
        MenuItemButton(
          onPressed: trigger('audit_logs'),
          leadingIcon: const Icon(Icons.policy_outlined, size: 18),
          child: const Text('Audit & Security Logs...'),
        ),
        MenuItemButton(
          onPressed: trigger('maintenance'),
          leadingIcon: const Icon(Icons.build_circle_outlined, size: 18),
          child: const Text('Database Maintenance & Optimizer...'),
        ),
        MenuItemButton(
          onPressed: trigger('notifications'),
          leadingIcon: const Icon(Icons.notifications_active_outlined, size: 18),
          child: const Text('Server Notifications, SMTP & Webhooks...'),
        ),
      ],
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Text('Server Tools', style: TextStyle(fontSize: 13)),
          if (containerUpdateAvailable) ...[
            const SizedBox(width: 5),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 1),
              decoration: BoxDecoration(
                color: Colors.amber.shade700,
                borderRadius: BorderRadius.circular(8),
              ),
              child: Text(
                latestLabel,
                style: const TextStyle(
                  fontSize: 9.5,
                  fontWeight: FontWeight.bold,
                  color: Colors.white,
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }

  // 9. Window Menu
  Widget _buildWindowMenu(BuildContext context) {
    return SubmenuButton(
      menuChildren: [
        const MenuItemButton(
          shortcut: SingleActivator(LogicalKeyboardKey.keyM, meta: true),
          child: Text('Minimize'),
        ),
        const MenuItemButton(
          child: Text('Zoom'),
        ),
        const Divider(height: 1),
        const MenuItemButton(
          child: Text('Tile Horizontally'),
        ),
        const MenuItemButton(
          child: Text('Tile Vertically'),
        ),
        const MenuItemButton(
          child: Text('Cascade'),
        ),
        const Divider(height: 1),
        const MenuItemButton(
          child: Text('New Window'),
        ),
        const MenuItemButton(
          child: Text('Show Window'),
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

class _ControllerUndoHistory {
  _ControllerUndoHistory(TextEditingValue initial) : lastValue = initial;

  TextEditingValue lastValue;
  final List<TextEditingValue> undoStack = [];
  final List<TextEditingValue> redoStack = [];
  bool isApplying = false;
}

/// Tracks the currently or most recently focused [EditableTextState] so that
/// clicking items in the top `Edit` menu (`Undo`, `Redo`, `Cut`, `Copy`,
/// `Paste`, `Clear`, `Select All`) still acts on the text field even after the
/// menu bar button takes primary focus.
class EditMenuFocusTracker {
  EditMenuFocusTracker._();

  static final EditMenuFocusTracker instance = EditMenuFocusTracker._();

  FocusManager? _boundFocusManager;
  EditableTextState? _lastEditable;
  bool _editableWasLastNonMenuFocus = false;
  String? _lastClipboardText;
  final Expando<_ControllerUndoHistory> _histories =
      Expando<_ControllerUndoHistory>('EditMenuUndoHistory');

  void ensureListening([BuildContext? context]) {
    final manager = FocusManager.instance;
    if (!identical(_boundFocusManager, manager)) {
      _boundFocusManager?.removeListener(_onFocusChanged);
      _boundFocusManager = manager;
      manager.addListener(_onFocusChanged);
    }
    _onFocusChanged();
    if (context != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (context.mounted) {
          _scanEditablesInTree(context);
        }
      });
    }
  }

  void _scanEditablesInTree(BuildContext context) {
    final root =
        context.findRootAncestorStateOfType<ScaffoldState>()?.context ?? context;
    void visit(Element el) {
      if (el is StatefulElement && el.state is EditableTextState) {
        final st = el.state as EditableTextState;
        if (st.mounted) {
          _ensureControllerHistory(st.widget.controller);
          if (st.widget.focusNode.hasFocus) {
            _lastEditable = st;
            _editableWasLastNonMenuFocus = true;
          }
        }
      }
      el.visitChildren(visit);
    }

    if (root is Element) {
      root.visitChildren(visit);
    }
  }

  /// True when an [EditableTextState] currently has focus or had focus right
  /// before the user opened a menu in the menu bar.
  bool get hasActiveEditableFocus {
    _syncCurrentFocus();
    return _editableWasLastNonMenuFocus &&
        _lastEditable != null &&
        _lastEditable!.mounted;
  }

  void _onFocusChanged() {
    _syncCurrentFocus();
  }

  void _syncCurrentFocus() {
    final node = FocusManager.instance.primaryFocus;
    final ctx = node?.context;
    if (ctx == null) return;

    final editable = _findEditableInContext(ctx);
    if (editable != null && editable.mounted) {
      _registerEditable(editable);
      _editableWasLastNonMenuFocus = true;
      return;
    }

    if (!_isMenuContext(ctx)) {
      _editableWasLastNonMenuFocus = false;
    }
  }

  void _registerEditable(EditableTextState editable) {
    _lastEditable = editable;
    _ensureControllerHistory(editable.widget.controller);
  }

  void _ensureControllerHistory(TextEditingController controller) {
    if (_histories[controller] == null) {
      final history = _ControllerUndoHistory(controller.value);
      _histories[controller] = history;
      controller.addListener(() {
        if (history.isApplying) return;
        final current = controller.value;
        if (current.text != history.lastValue.text) {
          history.undoStack.add(history.lastValue);
          if (history.undoStack.length > 50) {
            history.undoStack.removeAt(0);
          }
          history.redoStack.clear();
          history.lastValue = current;
        } else {
          history.lastValue = current;
        }
      });
    }
  }

  static EditableTextState? _findEditableInContext(BuildContext ctx) {
    if (ctx is StatefulElement && ctx.state is EditableTextState) {
      return ctx.state as EditableTextState;
    }
    final ancestor = ctx.findAncestorStateOfType<EditableTextState>();
    if (ancestor != null) return ancestor;

    EditableTextState? found;
    void visit(Element el) {
      if (found != null) return;
      if (el is StatefulElement && el.state is EditableTextState) {
        found = el.state as EditableTextState;
        return;
      }
      el.visitChildren(visit);
    }

    if (ctx is Element) {
      ctx.visitChildren(visit);
    }
    return found;
  }

  static bool _isMenuContext(BuildContext ctx) {
    var isMenu = false;
    ctx.visitAncestorElements((el) {
      final type = el.widget.runtimeType.toString();
      if (type.contains('Menu') || type.contains('Submenu')) {
        isMenu = true;
        return false;
      }
      return true;
    });
    return isMenu;
  }

  EditableTextState? resolveEditable([BuildContext? fallbackContext]) {
    _syncCurrentFocus();
    if (_lastEditable != null && _lastEditable!.mounted) {
      return _lastEditable;
    }
    if (fallbackContext != null) {
      final root = fallbackContext.findRootAncestorStateOfType<ScaffoldState>()?.context ??
          fallbackContext;
      EditableTextState? firstEditable;
      void search(Element el) {
        if (firstEditable != null) return;
        if (el is StatefulElement && el.state is EditableTextState) {
          final st = el.state as EditableTextState;
          if (st.mounted) {
            firstEditable = st;
            return;
          }
        }
        el.visitChildren(search);
      }

      if (root is Element) {
        root.visitChildren(search);
      }
      if (firstEditable != null) {
        _registerEditable(firstEditable!);
        return firstEditable;
      }
    }
    return null;
  }

  bool undoActiveText([BuildContext? fallbackContext]) {
    final editable = resolveEditable(fallbackContext);
    if (editable == null || !editable.mounted || editable.widget.readOnly) {
      return false;
    }
    final controller = editable.widget.controller;
    final history = _histories[controller];
    if (history != null && history.undoStack.isNotEmpty) {
      history.isApplying = true;
      try {
        history.redoStack.add(controller.value);
        final prev = history.undoStack.removeLast();
        history.lastValue = prev;
        editable.userUpdateTextEditingValue(prev, SelectionChangedCause.keyboard);
      } finally {
        history.isApplying = false;
      }
      editable.widget.focusNode.requestFocus();
      return true;
    }
    return false;
  }

  bool redoActiveText([BuildContext? fallbackContext]) {
    final editable = resolveEditable(fallbackContext);
    if (editable == null || !editable.mounted || editable.widget.readOnly) {
      return false;
    }
    final controller = editable.widget.controller;
    final history = _histories[controller];
    if (history != null && history.redoStack.isNotEmpty) {
      history.isApplying = true;
      try {
        history.undoStack.add(controller.value);
        final next = history.redoStack.removeLast();
        history.lastValue = next;
        editable.userUpdateTextEditingValue(next, SelectionChangedCause.keyboard);
      } finally {
        history.isApplying = false;
      }
      editable.widget.focusNode.requestFocus();
      return true;
    }
    return false;
  }

  bool cutActiveText([BuildContext? fallbackContext]) {
    final editable = resolveEditable(fallbackContext);
    if (editable == null || !editable.mounted) return false;
    final val = editable.textEditingValue;
    final hasSelection = val.selection.isValid && !val.selection.isCollapsed;
    final textToCopy = hasSelection ? val.selection.textInside(val.text) : val.text;
    if (textToCopy.isNotEmpty) {
      _lastClipboardText = textToCopy;
      Clipboard.setData(ClipboardData(text: textToCopy));
    }
    if (!editable.widget.readOnly) {
      final newValue = hasSelection
          ? TextEditingValue(
              text: val.selection.textBefore(val.text) + val.selection.textAfter(val.text),
              selection: TextSelection.collapsed(offset: val.selection.start),
            )
          : TextEditingValue.empty;
      editable.userUpdateTextEditingValue(newValue, SelectionChangedCause.toolbar);
    }
    editable.widget.focusNode.requestFocus();
    return true;
  }

  bool copyActiveText([BuildContext? fallbackContext]) {
    final editable = resolveEditable(fallbackContext);
    if (editable == null || !editable.mounted) return false;
    final val = editable.textEditingValue;
    final hasSelection = val.selection.isValid && !val.selection.isCollapsed;
    final textToCopy = hasSelection ? val.selection.textInside(val.text) : val.text;
    if (textToCopy.isNotEmpty) {
      _lastClipboardText = textToCopy;
      Clipboard.setData(ClipboardData(text: textToCopy));
    }
    editable.widget.focusNode.requestFocus();
    return true;
  }

  Future<bool> pasteActiveText([BuildContext? fallbackContext]) async {
    final editable = resolveEditable(fallbackContext);
    if (editable == null || !editable.mounted || editable.widget.readOnly) {
      return false;
    }
    final baseVal = editable.textEditingValue;
    final inAppClip = _lastClipboardText;
    if (inAppClip != null && inAppClip.isNotEmpty) {
      _insertIntoEditable(editable, baseVal, inAppClip);
      try {
        final data = await Clipboard.getData(Clipboard.kTextPlain);
        final osText = data?.text;
        if (osText != null &&
            osText.isNotEmpty &&
            osText != inAppClip &&
            editable.mounted) {
          _lastClipboardText = osText;
          _insertIntoEditable(editable, baseVal, osText);
        }
      } catch (_) {}
      return true;
    }

    String? clipText;
    try {
      final data = await Clipboard.getData(Clipboard.kTextPlain);
      clipText = data?.text;
    } catch (_) {}
    if (clipText == null || clipText.isEmpty || !editable.mounted) {
      return false;
    }
    _lastClipboardText = clipText;
    _insertIntoEditable(editable, editable.textEditingValue, clipText);
    return true;
  }

  void _insertIntoEditable(
    EditableTextState editable,
    TextEditingValue val,
    String clipText,
  ) {
    if (val.selection.isValid) {
      final before = val.selection.textBefore(val.text);
      final after = val.selection.textAfter(val.text);
      final nextText = '$before$clipText$after';
      final nextOffset = before.length + clipText.length;
      editable.userUpdateTextEditingValue(
        TextEditingValue(
          text: nextText,
          selection: TextSelection.collapsed(offset: nextOffset),
        ),
        SelectionChangedCause.toolbar,
      );
    } else {
      final nextText = '${val.text}$clipText';
      editable.userUpdateTextEditingValue(
        TextEditingValue(
          text: nextText,
          selection: TextSelection.collapsed(offset: nextText.length),
        ),
        SelectionChangedCause.toolbar,
      );
    }
    editable.widget.focusNode.requestFocus();
  }

  bool clearActiveText([BuildContext? fallbackContext]) {
    final editable = resolveEditable(fallbackContext);
    if (editable == null || !editable.mounted || editable.widget.readOnly) {
      return false;
    }
    final val = editable.textEditingValue;
    final hasSelection = val.selection.isValid && !val.selection.isCollapsed;
    final newValue = hasSelection
        ? TextEditingValue(
            text: val.selection.textBefore(val.text) + val.selection.textAfter(val.text),
            selection: TextSelection.collapsed(offset: val.selection.start),
          )
        : TextEditingValue.empty;
    editable.userUpdateTextEditingValue(newValue, SelectionChangedCause.toolbar);
    editable.widget.focusNode.requestFocus();
    return true;
  }

  bool selectAllActiveText([BuildContext? fallbackContext]) {
    final editable = resolveEditable(fallbackContext);
    if (editable == null || !editable.mounted) return false;
    final val = editable.textEditingValue;
    editable.userUpdateTextEditingValue(
      val.copyWith(
        selection: TextSelection(baseOffset: 0, extentOffset: val.text.length),
      ),
      SelectionChangedCause.toolbar,
    );
    editable.widget.focusNode.requestFocus();
    return true;
  }
}

