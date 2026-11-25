import 'dart:async';
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
import 'package:file4base_client/l10n/l10n.dart';
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
    this.onManageCustomFunctions,
    this.onManageFileReferences,
    this.onManageScriptTriggers,
    this.onPreferences,
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
    this.onRelookupFieldContents,
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

  /// File > Manage > Custom Functions: the solution's reusable formulas.
  final VoidCallback? onManageCustomFunctions;

  /// File > Manage > File References (#107): the other solutions this one
  /// reads tables from and performs scripts in.
  final VoidCallback? onManageFileReferences;

  /// File > Manage > Script Triggers (#26): every trigger of the solution's
  /// layouts, to review and enable those imported disabled by a migration.
  final VoidCallback? onManageScriptTriggers;

  /// Edit > Preferences...: opens the application preferences dialog (menu
  /// language management and client settings). Falls back to [onOpenRemote]
  /// when omitted.
  final VoidCallback? onPreferences;
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

  /// Records > Relookup Field Contents, from the active field as the match
  /// field. Null disables the command.
  final VoidCallback? onRelookupFieldContents;
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
    final l10n = context.l10n;
    final controller = TextEditingController();
    final target = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(l10n.dialogGoToRecordTitle),
        content: TextField(
          controller: controller,
          autofocus: true,
          keyboardType: TextInputType.number,
          decoration: InputDecoration(labelText: l10n.dialogGoToRecordNumberLabel),
          onSubmitted: (value) => Navigator.pop(ctx, value.trim()),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: Text(l10n.dialogCancel)),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, controller.text.trim()),
            child: Text(l10n.dialogGo),
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
    unawaited(showDialog(
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
            child: Text(context.l10n.dialogClose),
          ),
        ],
      ),
    ));
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
            preservedSuffix: [_buildCustomMenusSubmenu(context)],
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
    final l10n = context.l10n;
    return SubmenuButton(
      menuChildren: [
        if (!isAuthenticated) ...[
          MenuItemButton(
            onPressed: onSignIn,
            shortcut: const SingleActivator(LogicalKeyboardKey.keyL, meta: true),
            child: Row(
              children: [
                const Icon(Icons.login, size: 16, color: Color(0xFF1E88E5)),
                const SizedBox(width: 8),
                Text(l10n.menuFileSignIn),
              ],
            ),
          ),
          const Divider(height: 1),
        ],
        MenuItemButton(
          onPressed: onNewDatabase,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyN, meta: true),
          child: Text(l10n.menuFileNewDatabase),
        ),
        MenuItemButton(
          onPressed: onOpenSolution,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyO, meta: true),
          child: Text(l10n.menuFileOpen),
        ),
        MenuItemButton(
          onPressed: onOpenRemote,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyO, meta: true, shift: true),
          child: Text(l10n.menuFileOpenRemote),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, l10n.noticeAuthRequiredTitle, l10n.noticeAuthSaveSolution)
              : onSave,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true),
          child: Text(l10n.menuFileSave),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, l10n.noticeAuthRequiredTitle, l10n.noticeAuthSaveSolution)
              : onSaveAs,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true, shift: true),
          child: Text(l10n.menuFileSaveAs),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, l10n.noticeAuthRequiredTitle, l10n.noticeAuthSaveCopy)
              : onSaveCopyAs,
          child: Text(l10n.menuFileSaveCopyAs),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, l10n.noticeAuthRequiredTitle, l10n.noticeAuthExportData)
              : onExportData,
          child: Text(l10n.menuFileExportData),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, l10n.noticeAuthRequiredTitle, l10n.noticeAuthImportData)
              : onImportData,
          child: Text(l10n.menuFileImportData),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              child: Text(l10n.menuFileLocalHost),
            ),
          ],
          child: Text(l10n.menuFileOpenFavorite),
        ),
        SubmenuButton(
          menuChildren: const [
            MenuItemButton(
              child: Text('default_workspace (PostgreSQL)'),
            ),
          ],
          child: Text(l10n.menuFileOpenRecent),
        ),
        const Divider(height: 1),
        if (isAuthenticated && onSignOut != null) ...[
          MenuItemButton(
            onPressed: onSignOut,
            child: Row(
              children: [
                const Icon(Icons.lock_outline, size: 16, color: Colors.orange),
                const SizedBox(width: 8),
                Text(l10n.menuFileSignOutLock),
              ],
            ),
          ),
          const Divider(height: 1),
        ],
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: !isAuthenticated
                  ? (onSignIn ?? () => _showNotice(context, l10n.noticeAuthRequiredTitle, l10n.noticeAuthManageSchema))
                  : onManageDatabase,
              shortcut: const SingleActivator(LogicalKeyboardKey.keyD, meta: true, shift: true),
              child: Text(l10n.menuManageDatabase),
            ),
            MenuItemButton(
              onPressed: onManageSecurity ??
                  () => _showRoadmapDialog(context, 'Manage Security', 'Roadmap Phase 8', 'User accounts, privilege sets, and extended access rules.'),
              shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true, shift: true),
              child: Text(l10n.menuManageSecurity),
            ),
            MenuItemButton(
              onPressed: onManageValueLists,
              child: Text(l10n.menuManageValueLists),
            ),
            MenuItemButton(
              onPressed: onManageCustomFunctions,
              child: Text(l10n.menuManageCustomFunctions),
            ),
            MenuItemButton(
              onPressed: onManageFileReferences,
              child: Text(l10n.menuManageFileReferences),
            ),
            MenuItemButton(
              onPressed: onManageDataSources,
              child: Text(l10n.menuManageExternalDataSources),
            ),
            MenuItemButton(
              onPressed: onManageLayouts ?? () => onModeChanged(OperationalMode.layout),
              child: Text(l10n.menuManageLayouts),
            ),
            MenuItemButton(
              onPressed: onManageScripts ?? onScriptWorkspace ?? () => _showRoadmapDialog(context, 'Manage Scripts', 'Roadmap Phase 7', 'Automated workflows and calculation scripts.'),
              child: Text(l10n.menuManageScripts),
            ),
            MenuItemButton(
              onPressed: onManageScriptTriggers,
              child: Text(l10n.menuManageScriptTriggers),
            ),
            MenuItemButton(
              onPressed: onManageThemes ?? () => _showRoadmapDialog(context, 'Themes', 'Roadmap Phase 5', 'Visual styling themes, typography rules, and CSS presets.'),
              child: Text(l10n.menuManageThemes),
            ),
          ],
          child: Text(l10n.menuFileManage),
        ),
        MenuItemButton(
          onPressed: onFileOptions,
          child: Text(l10n.menuFileOptions),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, l10n.noticeAuthRequiredTitle, l10n.noticeAuthChangePassword)
              : onChangePassword,
          child: Text(l10n.menuFileChangePassword),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onPageSetup ?? () => onModeChanged(OperationalMode.preview),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyP, meta: true, shift: true),
          child: Text(l10n.menuFilePageSetup),
        ),
        MenuItemButton(
          onPressed: onPrint ?? () => onModeChanged(OperationalMode.preview),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyP, meta: true),
          child: Text(l10n.menuFilePrint),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: !isAuthenticated
                  ? () => _showNotice(context, l10n.noticeAuthRequiredTitle,
                      l10n.noticeAuthImportRecords)
                  : onImportRecords,
              child: Text(l10n.menuFileImportFile),
            ),
            // Importing a folder of pictures into container fields is a
            // separate piece of work; the item is shown disabled rather than
            // opening a dialog that does nothing (#38).
            MenuItemButton(
              child: Text(l10n.menuFileImportFolder),
            ),
            MenuItemButton(
              onPressed: !isAuthenticated
                  ? () => _showNotice(context, l10n.noticeAuthRequiredTitle,
                      l10n.noticeAuthImportRecords)
                  : onImportRecords,
              child: Text(l10n.menuFileImportXmlDataSource),
            ),
            // Not ODBC: File4Base reads the engines it has Go drivers for,
            // and the item says so rather than promising a bridge (#47).
            MenuItemButton(
              onPressed: !isAuthenticated
                  ? () => _showNotice(context, l10n.noticeAuthRequiredTitle,
                      l10n.noticeAuthImportRecords)
                  : onImportFromDataSource,
              child: Text(l10n.menuFileImportExternalSqlDataSource),
            ),
          ],
          child: Text(l10n.menuFileImportRecords),
        ),
        MenuItemButton(
          onPressed: !isAuthenticated
              ? () => _showNotice(context, l10n.noticeAuthRequiredTitle, l10n.noticeAuthExportRecords)
              : onExportRecords,
          child: Text(l10n.menuFileExportRecords),
        ),
        MenuItemButton(
          child: Text(l10n.menuFileRecover),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onQuit ?? SolutionStorageService.triggerQuit,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyQ, meta: true),
          child: Row(
            children: [
              const Icon(Icons.exit_to_app, size: 16, color: Colors.redAccent),
              const SizedBox(width: 8),
              Text(l10n.menuFileQuit),
            ],
          ),
        ),
      ],
      child: Text(l10n.menuFile, style: const TextStyle(fontSize: 13)),
    );
  }

  // 2. Edit Menu
  Widget _buildEditMenu(BuildContext context) {
    final l10n = context.l10n;
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
          child: Text(l10n.menuEditUndo),
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
          child: Text(l10n.menuEditRedo),
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
          child: Text(l10n.menuEditCut),
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
          child: Text(l10n.menuEditCopy),
        ),
        MenuItemButton(
          onPressed: () {
            if (onPaste != null) {
              onPaste!();
            } else {
              unawaited(EditMenuFocusTracker.instance.pasteActiveText(context));
            }
          },
          shortcut: const SingleActivator(LogicalKeyboardKey.keyV, meta: true),
          child: Text(l10n.menuEditPaste),
        ),
        MenuItemButton(
          onPressed: () {
            if (onClear != null) {
              onClear!();
            } else {
              EditMenuFocusTracker.instance.clearActiveText(context);
            }
          },
          child: Text(l10n.menuEditClear),
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
          child: Text(l10n.menuEditSelectAll),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: () => onModeChanged(OperationalMode.find),
              child: Text(l10n.menuEditFindReplaceEllipsis),
            ),
            MenuItemButton(
              child: Text(l10n.menuEditFindNext),
            ),
            MenuItemButton(
              child: Text(l10n.menuEditFindPrevious),
            ),
          ],
          child: Text(l10n.menuEditFindReplace),
        ),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              child: Text(l10n.menuEditCheckSelection),
            ),
            MenuItemButton(
              child: Text(l10n.menuEditCheckRecord),
            ),
            MenuItemButton(
              child: Text(l10n.menuEditCorrectWord),
            ),
            MenuItemButton(
              child: Text(l10n.menuEditDictionaries),
            ),
            MenuItemButton(
              child: Text(l10n.menuEditEditUserDictionary),
            ),
            MenuItemButton(
              child: Text(l10n.menuEditSpellingOptions),
            ),
          ],
          child: Text(l10n.menuEditSpelling),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onPreferences ?? onOpenRemote,
          shortcut: const SingleActivator(LogicalKeyboardKey.comma, meta: true),
          child: Text(l10n.menuEditPreferences),
        ),
      ],
      child: Text(l10n.menuEdit, style: const TextStyle(fontSize: 13)),
    );
  }

  // 3. View Menu
  Widget _buildViewMenu(BuildContext context) {
    final l10n = context.l10n;
    void handleModeChange(OperationalMode mode) {
      if (!isAuthenticated) {
        if (onSignIn != null) {
          onSignIn!();
        } else {
          _showNotice(context, l10n.noticeAuthRequiredTitle, l10n.noticeAuthDatabaseViews);
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
          child: Text(l10n.menuViewBrowseMode),
        ),
        MenuItemButton(
          leadingIcon: activeMode == OperationalMode.find ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => handleModeChange(OperationalMode.find),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyF, meta: true),
          child: Text(l10n.menuViewFindMode),
        ),
        MenuItemButton(
          leadingIcon: activeMode == OperationalMode.layout ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => handleModeChange(OperationalMode.layout),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyL, meta: true),
          child: Text(l10n.menuViewLayoutMode),
        ),
        MenuItemButton(
          leadingIcon: activeMode == OperationalMode.preview ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => handleModeChange(OperationalMode.preview),
          shortcut: const SingleActivator(LogicalKeyboardKey.keyU, meta: true),
          child: Text(l10n.menuViewPreviewMode),
        ),
        const Divider(height: 1),
        MenuItemButton(
          leadingIcon: isPageMarginsVisible ? const Icon(Icons.check, size: 14) : null,
          onPressed: onTogglePageMargins != null
              ? () => onTogglePageMargins!(!isPageMarginsVisible)
              : null,
          child: Text(l10n.menuViewPageMargins),
        ),
        MenuItemButton(
          leadingIcon: isToolbarVisible ? const Icon(Icons.check, size: 14) : null,
          onPressed: () => onToggleToolbar(!isToolbarVisible),
          child: Text(l10n.menuViewStatusToolbar),
        ),
        MenuItemButton(
          leadingIcon: isFormattingBarVisible ? const Icon(Icons.check, size: 14) : null,
          onPressed: onToggleFormattingBar != null
              ? () => onToggleFormattingBar!(!isFormattingBarVisible)
              : null,
          child: Text(l10n.menuViewFormattingBar),
        ),
        MenuItemButton(
          leadingIcon: isTextRulerVisible ? const Icon(Icons.check, size: 14) : null,
          onPressed: onToggleTextRuler != null
              ? () => onToggleTextRuler!(!isTextRulerVisible)
              : null,
          child: Text(l10n.menuViewTextRuler),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onZoomIn,
          shortcut: const SingleActivator(LogicalKeyboardKey.equal, meta: true),
          child: Text(l10n.menuViewZoomIn),
        ),
        MenuItemButton(
          onPressed: onZoomOut,
          shortcut: const SingleActivator(LogicalKeyboardKey.minus, meta: true),
          child: Text(l10n.menuViewZoomOut),
        ),
        MenuItemButton(
          onPressed: onResetZoom,
          shortcut: const SingleActivator(LogicalKeyboardKey.digit0, meta: true),
          child: Text(l10n.menuViewActualSize),
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
          child: Text(l10n.menuViewZoomLevel),
        ),
      ],
      child: Text(l10n.menuView, style: const TextStyle(fontSize: 13)),
    );
  }

  // 4. Insert Menu
  // In Layout mode the items insert into the layout (handlers supplied by the
  // host); in the other modes there is nothing to insert into, so they are
  // disabled rather than reporting an insertion that did not happen.
  Widget _buildInsertMenu(BuildContext context) {
    final l10n = context.l10n;
    final media = onInsertMedia;
    final symbol = onInsertSymbol;
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          leadingIcon: const Icon(Icons.image_outlined, size: 16),
          onPressed: media != null
              ? () => media('image')
              : null, // only available in Layout mode
          child: Text(l10n.menuInsertPicture),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.movie_outlined, size: 16),
          onPressed: media != null
              ? () => media('video')
              : null, // only available in Layout mode
          child: Text(l10n.menuInsertAudioVideo),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.picture_as_pdf_outlined, size: 16),
          onPressed: media != null
              ? () => media('pdf')
              : null, // only available in Layout mode
          child: Text(l10n.menuInsertPdf),
        ),
        MenuItemButton(
          onPressed: media != null
              ? () => media('video')
              : null, // only available in Layout mode
          child: Text(l10n.menuInsertQuickTime),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.attach_file, size: 16),
          onPressed: media != null
              ? () => media('file')
              : null, // only available in Layout mode
          child: Text(l10n.menuInsertFile),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: symbol != null
              ? () => symbol(LayoutMergeSymbols.currentDate)
              : null, // only available in Layout mode
          child: Text(l10n.menuInsertCurrentDate),
        ),
        MenuItemButton(
          onPressed: symbol != null
              ? () => symbol(LayoutMergeSymbols.currentTime)
              : null, // only available in Layout mode
          child: Text(l10n.menuInsertCurrentTime),
        ),
        MenuItemButton(
          onPressed: symbol != null
              ? () => symbol(LayoutMergeSymbols.currentUser)
              : null, // only available in Layout mode
          child: Text(l10n.menuInsertCurrentUserName),
        ),
        if (symbol != null)
          MenuItemButton(
            onPressed: () => symbol(LayoutMergeSymbols.pageNumber),
            child: Text(l10n.menuInsertPageNumber),
          ),
        const Divider(height: 1),
        MenuItemButton(
          child: Text(l10n.menuInsertFromIndex),
        ),
        MenuItemButton(
          child: Text(l10n.menuInsertFromLastVisitedRecord),
        ),
        MenuItemButton(
          onPressed: onInsertMergeField,
          child: Text(l10n.menuInsertMergeField),
        ),
        MenuItemButton(
          child: Text(l10n.menuInsertMergeVariable),
        ),
      ],
      child: Text(l10n.menuInsert, style: const TextStyle(fontSize: 13)),
    );
  }

  // 5. Format Menu
  Widget _buildFormatMenu(BuildContext context) {
    final l10n = context.l10n;
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
              child: Text(l10n.menuFormatSystemDefault),
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
          child: Text(l10n.menuFormatFont),
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
                child: Text(l10n.menuFormatSizePt(sz.round())),
              ),
          ],
          child: Text(l10n.menuFormatSize),
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
              child: Text(l10n.menuFormatPlainText),
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
              child: Text(
                l10n.menuFormatBold,
                style: const TextStyle(fontWeight: FontWeight.bold),
              ),
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
              child: Text(
                l10n.menuFormatItalic,
                style: const TextStyle(fontStyle: FontStyle.italic),
              ),
            ),
            MenuItemButton(
              leadingIcon: s.underline ? const Icon(Icons.check, size: 14) : null,
              onPressed: apply != null
                  ? () => apply(s.copyWith(underline: !s.underline))
                  : null,
              child: Text(
                l10n.menuFormatUnderline,
                style: const TextStyle(decoration: TextDecoration.underline),
              ),
            ),
            MenuItemButton(
              leadingIcon: s.strikethrough ? const Icon(Icons.check, size: 14) : null,
              onPressed: apply != null
                  ? () => apply(s.copyWith(strikethrough: !s.strikethrough))
                  : null,
              child: Text(
                l10n.menuFormatStrikethrough,
                style: const TextStyle(decoration: TextDecoration.lineThrough),
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
              child: Text(l10n.menuFormatUppercase),
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
              child: Text(l10n.menuFormatLowercase),
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
              child: Text(l10n.menuFormatTitleCase),
            ),
          ],
          child: Text(l10n.menuFormatStyle),
        ),

        // 4. Align Text Submenu
        SubmenuButton(
          menuChildren: [
            for (final entry in [
              ('left', l10n.menuFormatAlignLeft, Icons.format_align_left),
              ('center', l10n.menuFormatAlignCenter, Icons.format_align_center),
              ('right', l10n.menuFormatAlignRight, Icons.format_align_right),
              ('justify', l10n.menuFormatAlignJustify, Icons.format_align_justify),
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
          child: Text(l10n.menuFormatAlignText),
        ),

        // 5. Line Spacing Submenu
        SubmenuButton(
          menuChildren: [
            for (final sp in [
              (1.0, l10n.menuFormatLineSpacingSingle),
              (1.15, l10n.menuFormatLineSpacing115),
              (1.2, l10n.menuFormatLineSpacingStandard),
              (1.5, l10n.menuFormatLineSpacingOneHalf),
              (2.0, l10n.menuFormatLineSpacingDouble),
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
          child: Text(l10n.menuFormatLineSpacing),
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
              child: Text(l10n.menuFormatDefaultThemeColor),
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
                child: Text(localizedFormatColorName(l10n, entry.key, entry.value)),
              ),
          ],
          child: Text(l10n.menuFormatTextColor),
        ),

        const Divider(height: 1),
        MenuItemButton(
          leadingIcon: const Icon(Icons.font_download_outlined, size: 15),
          onPressed: onOpenTextFormat,
          child: Text(l10n.menuFormatText),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.numbers, size: 15),
          onPressed: onOpenNumberFormat,
          child: Text(l10n.menuFormatNumber),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.calendar_today_outlined, size: 15),
          onPressed: onOpenDateFormat,
          child: Text(l10n.menuFormatDate),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.access_time, size: 15),
          onPressed: onOpenTimeFormat,
          child: Text(l10n.menuFormatTime),
        ),
        MenuItemButton(
          leadingIcon: const Icon(Icons.schedule_outlined, size: 15),
          onPressed: onOpenTimestampFormat,
          child: Text(l10n.menuFormatTimestamp),
        ),
        const Divider(height: 1),
        MenuItemButton(
          leadingIcon: const Icon(Icons.auto_fix_high, size: 15),
          onPressed: onOpenConditionalFormatting,
          child: Text(l10n.menuFormatConditional),
        ),

        if (activeMode == OperationalMode.layout) ...[
          const Divider(height: 1),
          SubmenuButton(
            menuChildren: [
              MenuItemButton(
                leadingIcon: const Icon(Icons.align_horizontal_left, size: 15),
                onPressed: onAlignObjects != null ? () => onAlignObjects!('left') : null,
                child: Text(l10n.menuFormatAlignLeftEdges),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.align_horizontal_center, size: 15),
                onPressed: onAlignObjects != null ? () => onAlignObjects!('center_h') : null,
                child: Text(l10n.menuFormatAlignHorizontalCenters),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.align_horizontal_right, size: 15),
                onPressed: onAlignObjects != null ? () => onAlignObjects!('right') : null,
                child: Text(l10n.menuFormatAlignRightEdges),
              ),
              const Divider(height: 1),
              MenuItemButton(
                leadingIcon: const Icon(Icons.align_vertical_top, size: 15),
                onPressed: onAlignObjects != null ? () => onAlignObjects!('top') : null,
                child: Text(l10n.menuFormatAlignTopEdges),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.align_vertical_center, size: 15),
                onPressed: onAlignObjects != null ? () => onAlignObjects!('center_v') : null,
                child: Text(l10n.menuFormatAlignVerticalCenters),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.align_vertical_bottom, size: 15),
                onPressed: onAlignObjects != null ? () => onAlignObjects!('bottom') : null,
                child: Text(l10n.menuFormatAlignBottomEdges),
              ),
            ],
            child: Text(l10n.menuFormatAlignObject),
          ),
          SubmenuButton(
            menuChildren: [
              MenuItemButton(
                leadingIcon: const Icon(Icons.horizontal_distribute, size: 15),
                onPressed: onDistributeObjects != null
                    ? () => onDistributeObjects!('horizontal')
                    : null,
                child: Text(l10n.menuFormatDistributeHorizontally),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.vertical_distribute, size: 15),
                onPressed: onDistributeObjects != null
                    ? () => onDistributeObjects!('vertical')
                    : null,
                child: Text(l10n.menuFormatDistributeVertically),
              ),
            ],
            child: Text(l10n.menuFormatDistribute),
          ),
          SubmenuButton(
            menuChildren: [
              MenuItemButton(
                leadingIcon: const Icon(Icons.swap_horiz, size: 15),
                onPressed: onResizeObjects != null ? () => onResizeObjects!('width') : null,
                child: Text(l10n.menuFormatMatchWidth),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.swap_vert, size: 15),
                onPressed: onResizeObjects != null ? () => onResizeObjects!('height') : null,
                child: Text(l10n.menuFormatMatchHeight),
              ),
              MenuItemButton(
                leadingIcon: const Icon(Icons.aspect_ratio, size: 15),
                onPressed: onResizeObjects != null ? () => onResizeObjects!('both') : null,
                child: Text(l10n.menuFormatMatchBoth),
              ),
              const Divider(height: 1),
              MenuItemButton(
                leadingIcon: const Icon(Icons.grid_on, size: 15),
                onPressed: onResizeObjects != null ? () => onResizeObjects!('grid') : null,
                child: Text(l10n.menuFormatSnapToGrid),
              ),
            ],
            child: Text(l10n.menuFormatResize),
          ),
        ],
      ],
      child: Text(l10n.menuFormat, style: const TextStyle(fontSize: 13)),
    );
  }

  // 6. Records Menu (Browse Mode)
  Widget _buildRecordsMenu(BuildContext context) {
    final l10n = context.l10n;
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: onNewRecord,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyN, meta: true),
          child: Text(l10n.menuRecordsNewRecord),
        ),
        MenuItemButton(
          onPressed: onDuplicateRecord,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyD, meta: true),
          child: Text(l10n.menuRecordsDuplicateRecord),
        ),
        MenuItemButton(
          onPressed: onDeleteRecord,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyE, meta: true),
          child: Text(l10n.menuRecordsDeleteRecord),
        ),
        MenuItemButton(
          child: Text(l10n.menuRecordsDeleteAllRecords),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onCommitRecord,
          child: Text(l10n.menuRecordsCommitRecord),
        ),
        MenuItemButton(
          onPressed: onRevertRecord,
          child: Text(l10n.menuRecordsRevertRecord),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: onGoToRecord == null ? null : () => onGoToRecord!('first'),
              child: Text(l10n.menuNavFirst),
            ),
            MenuItemButton(
              onPressed: onGoToRecord == null ? null : () => onGoToRecord!('previous'),
              child: Text(l10n.menuNavPrevious),
            ),
            MenuItemButton(
              onPressed: onGoToRecord == null ? null : () => onGoToRecord!('next'),
              child: Text(l10n.menuNavNext),
            ),
            MenuItemButton(
              onPressed: onGoToRecord == null ? null : () => onGoToRecord!('last'),
              child: Text(l10n.menuNavLast),
            ),
            MenuItemButton(
              onPressed: onGoToRecord == null ? null : () => _promptGoToRecord(context),
              child: Text(l10n.menuNavByNumber),
            ),
          ],
          child: Text(l10n.menuRecordsGoToRecord),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onShowAllRecords,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyJ, meta: true),
          child: Text(l10n.menuRecordsShowAllRecords),
        ),
        _buildSavedFindsMenu(context),
        MenuItemButton(
          child: Text(l10n.menuRecordsShowOmittedOnly),
        ),
        MenuItemButton(
          shortcut: const SingleActivator(LogicalKeyboardKey.keyM, meta: true),
          child: Text(l10n.menuRecordsOmitRecord),
        ),
        MenuItemButton(
          child: Text(l10n.menuRecordsOmitMultiple),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onSortRecords,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyS, meta: true),
          child: Text(l10n.menuRecordsSortRecords),
        ),
        MenuItemButton(
          onPressed: onUnsortRecords,
          child: Text(l10n.menuRecordsUnsort),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => _showRoadmapDialog(context, 'Replace Field Contents', 'Roadmap Phase 4', 'Batch replace field values across all found records with calculation, serial, or constant.'),
          shortcut: const SingleActivator(LogicalKeyboardKey.equal, meta: true),
          child: Text(l10n.menuRecordsReplaceFieldContents),
        ),
        MenuItemButton(
          onPressed: onRelookupFieldContents,
          child: Text(l10n.menuRecordsRelookupFieldContents),
        ),
      ],
      child: Text(l10n.menuRecords, style: const TextStyle(fontSize: 13)),
    );
  }

  /// Records > Saved Finds (#34): save the requests in hand under a name,
  /// run one of the finds saved on this table, or open the list to rename
  /// and delete them.
  Widget _buildSavedFindsMenu(BuildContext context) {
    final l10n = context.l10n;
    final run = onRunSavedFind;
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: onSaveCurrentFind,
          child: Text(l10n.menuRecordsSaveCurrentFind),
        ),
        MenuItemButton(
          onPressed: savedFinds.isEmpty ? null : onManageSavedFinds,
          child: Text(l10n.menuRecordsEditSavedFinds),
        ),
        const Divider(height: 1),
        if (savedFinds.isEmpty)
          MenuItemButton(
            child: Text(l10n.menuRecordsNoSavedFinds),
          )
        else
          for (final find in savedFinds)
            MenuItemButton(
              onPressed: run == null ? null : () => run(find),
              child: Text(find.name),
            ),
      ],
      child: Text(l10n.menuRecordsSavedFinds),
    );
  }

  // 6b. Requests Menu (Find Mode)
  //
  // Find mode holds several requests: the records that match any of them,
  // minus the records matched by a request marked Omit.
  Widget _buildRequestsMenu(BuildContext context) {
    final l10n = context.l10n;
    final goTo = onGoToFindRequest;
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: onPerformFind,
          shortcut: const SingleActivator(LogicalKeyboardKey.enter),
          child: Text(l10n.menuRequestsPerformFind),
        ),
        MenuItemButton(
          onPressed: onNewFindRequest,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyN, meta: true),
          child: Text(l10n.menuRequestsNewRequest),
        ),
        MenuItemButton(
          onPressed: onDuplicateFindRequest,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyD, meta: true),
          child: Text(l10n.menuRequestsDuplicateRequest),
        ),
        MenuItemButton(
          onPressed: onDeleteFindRequest,
          shortcut: const SingleActivator(LogicalKeyboardKey.keyE, meta: true),
          child: Text(l10n.menuRequestsDeleteRequest),
        ),
        MenuItemButton(
          onPressed: onDeleteAllFindRequests,
          child: Text(l10n.menuRequestsDeleteAllRequests),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onToggleFindOmit,
          child: Text(l10n.menuRequestsIncludeOmit),
        ),
        const Divider(height: 1),
        SubmenuButton(
          menuChildren: [
            MenuItemButton(
              onPressed: goTo == null ? null : () => goTo('first'),
              child: Text(l10n.menuNavFirst),
            ),
            MenuItemButton(
              onPressed: goTo == null ? null : () => goTo('previous'),
              child: Text(l10n.menuNavPrevious),
            ),
            MenuItemButton(
              onPressed: goTo == null ? null : () => goTo('next'),
              child: Text(l10n.menuNavNext),
            ),
            MenuItemButton(
              onPressed: goTo == null ? null : () => goTo('last'),
              child: Text(l10n.menuNavLast),
            ),
          ],
          child: Text(l10n.menuRequestsGoToRequest),
        ),
        const Divider(height: 1),
        // The same submenu as in Records, because the criteria to save are
        // the ones being typed here (#34).
        _buildSavedFindsMenu(context),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () {
            onModeChanged(OperationalMode.browse);
            if (onShowAllRecords != null) onShowAllRecords!();
          },
          shortcut: const SingleActivator(LogicalKeyboardKey.keyJ, meta: true),
          child: Text(l10n.menuRequestsShowAllCancelFind),
        ),
      ],
      child: Text(
        l10n.menuRequests,
        style: const TextStyle(fontSize: 13, fontWeight: FontWeight.bold),
      ),
    );
  }

  // 7. Scripts Menu
  Widget _buildScriptsMenu(BuildContext context) {
    final l10n = context.l10n;
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
          child: Text(l10n.menuScriptsWorkspace),
        ),
        const Divider(height: 1),
        MenuItemButton(
          child: Text(l10n.menuScriptsCustomListPlaceholder),
        ),
      ],
      child: Text(l10n.menuScripts, style: const TextStyle(fontSize: 13)),
    );
  }

  // 8. Tools Menu
  Widget _buildToolsMenu(BuildContext context) {
    final l10n = context.l10n;
    // A tool that does not exist is shown disabled. The Tools menu used to
    // open a "roadmap" dialog for each of them, which read as though the
    // tool were there (#48).
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: onScriptDebugger,
          child: Text(l10n.menuToolsScriptDebugger),
        ),
        MenuItemButton(
          onPressed: onDataViewer,
          child: Text(l10n.menuToolsDataViewer),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: onDesignReport,
          child: Text(l10n.menuToolsDatabaseDesignReport),
        ),
        MenuItemButton(
          onPressed: onSaveDesignAsText,
          child: Text(l10n.menuToolsSaveCopyAsXml),
        ),
        const Divider(height: 1),
        _buildCustomMenusSubmenu(context),
        MenuItemButton(
          onPressed: onDeveloperUtilities,
          child: Text(l10n.menuToolsDeveloperUtilities),
        ),
        MenuItemButton(
          onPressed: onManageThemes,
          child: Text(l10n.menuToolsLayoutThemes),
        ),
      ],
      child: Text(l10n.menuTools, style: const TextStyle(fontSize: 13)),
    );
  }

  Widget _buildCustomMenusSubmenu(BuildContext context) {
    final l10n = context.l10n;
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: onManageCustomMenus,
          child: Text(l10n.menuToolsManageCustomMenus),
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
            child: Text(l10n.menuToolsStandardMenus),
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
      child: Text(l10n.menuToolsCustomMenus),
    );
  }

  // 8b. Server Tools Menu
  Widget _buildServerToolsMenu(BuildContext context) {
    final l10n = context.l10n;
    VoidCallback? trigger(String key) {
      if (!isAuthenticated || onServerToolSelected == null) return null;
      return () => onServerToolSelected!(key);
    }

    final latestLabel = (containerLatestVersion != null && containerLatestVersion!.isNotEmpty)
        ? 'v$containerLatestVersion'
        : l10n.menuServerToolsNewBadge;

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
                    l10n.menuServerToolsUpdateBadge(latestLabel),
                    style: const TextStyle(fontSize: 9.5, fontWeight: FontWeight.bold, color: Colors.white),
                  ),
                )
              : null,
          child: Text(l10n.menuServerToolsContainers),
        ),
        MenuItemButton(
          onPressed: trigger('backups'),
          leadingIcon: const Icon(Icons.backup_outlined, size: 18),
          child: Text(l10n.menuServerToolsBackups),
        ),
        MenuItemButton(
          onPressed: trigger('domain_tls'),
          leadingIcon: const Icon(Icons.https_outlined, size: 18),
          child: Text(l10n.menuServerToolsDomainTls),
        ),
        MenuItemButton(
          onPressed: trigger('telemetry'),
          leadingIcon: const Icon(Icons.insights_outlined, size: 18),
          child: Text(l10n.menuServerToolsTelemetry),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: trigger('sessions'),
          leadingIcon: const Icon(Icons.people_alt_outlined, size: 18),
          child: Text(l10n.menuServerToolsSessions),
        ),
        MenuItemButton(
          onPressed: trigger('schedules'),
          leadingIcon: const Icon(Icons.schedule_outlined, size: 18),
          child: Text(l10n.menuServerToolsSchedules),
        ),
        MenuItemButton(
          onPressed: trigger('audit_logs'),
          leadingIcon: const Icon(Icons.policy_outlined, size: 18),
          child: Text(l10n.menuServerToolsAuditLogs),
        ),
        MenuItemButton(
          onPressed: trigger('maintenance'),
          leadingIcon: const Icon(Icons.build_circle_outlined, size: 18),
          child: Text(l10n.menuServerToolsMaintenance),
        ),
        MenuItemButton(
          onPressed: trigger('notifications'),
          leadingIcon: const Icon(Icons.notifications_active_outlined, size: 18),
          child: Text(l10n.menuServerToolsNotifications),
        ),
        const Divider(height: 1),
        // The same "Manage" menu as File > Manage, at the end of Server Tools
        // (Containers excluded — it is a Server Tools section above).
        SubmenuButton(
          leadingIcon: const Icon(Icons.tune_outlined, size: 18),
          menuChildren: [
            MenuItemButton(
              onPressed: onManageDatabase,
              leadingIcon: const Icon(Icons.storage_outlined, size: 18),
              child: Text(l10n.menuManageDatabase),
            ),
            MenuItemButton(
              onPressed: onManageSecurity,
              leadingIcon: const Icon(Icons.security_outlined, size: 18),
              child: Text(l10n.menuManageSecurity),
            ),
            MenuItemButton(
              onPressed: onManageValueLists,
              leadingIcon: const Icon(Icons.list_alt_outlined, size: 18),
              child: Text(l10n.menuManageValueLists),
            ),
            MenuItemButton(
              onPressed: onManageCustomFunctions,
              leadingIcon: const Icon(Icons.functions, size: 18),
              child: Text(l10n.menuManageCustomFunctions),
            ),
            MenuItemButton(
              onPressed: onManageFileReferences,
              leadingIcon: const Icon(Icons.link, size: 18),
              child: Text(l10n.menuManageFileReferences),
            ),
            MenuItemButton(
              onPressed: onManageDataSources,
              leadingIcon: const Icon(Icons.cable_outlined, size: 18),
              child: Text(l10n.menuManageExternalDataSources),
            ),
            MenuItemButton(
              onPressed: onManageLayouts ?? () => onModeChanged(OperationalMode.layout),
              leadingIcon: const Icon(Icons.dashboard_customize_outlined, size: 18),
              child: Text(l10n.menuManageLayouts),
            ),
            MenuItemButton(
              onPressed: onManageScripts ?? onScriptWorkspace,
              leadingIcon: const Icon(Icons.code, size: 18),
              child: Text(l10n.menuManageScripts),
            ),
            MenuItemButton(
              onPressed: onManageScriptTriggers,
              leadingIcon: const Icon(Icons.bolt, size: 18),
              child: Text(l10n.menuManageScriptTriggers),
            ),
          ],
          child: Text(l10n.menuFileManage),
        ),
      ],
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(l10n.menuServerTools, style: const TextStyle(fontSize: 13)),
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
    final l10n = context.l10n;
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          shortcut: const SingleActivator(LogicalKeyboardKey.keyM, meta: true),
          child: Text(l10n.menuWindowMinimize),
        ),
        MenuItemButton(
          child: Text(l10n.menuWindowZoom),
        ),
        const Divider(height: 1),
        MenuItemButton(
          child: Text(l10n.menuWindowTileHorizontally),
        ),
        MenuItemButton(
          child: Text(l10n.menuWindowTileVertically),
        ),
        MenuItemButton(
          child: Text(l10n.menuWindowCascade),
        ),
        const Divider(height: 1),
        MenuItemButton(
          child: Text(l10n.menuWindowNewWindow),
        ),
        MenuItemButton(
          child: Text(l10n.menuWindowShowWindow),
        ),
        const Divider(height: 1),
        MenuItemButton(
          leadingIcon: const Icon(Icons.check, size: 14),
          onPressed: () {},
          child: Text(l10n.menuWindowMainWorkspace),
        ),
      ],
      child: Text(l10n.menuWindow, style: const TextStyle(fontSize: 13)),
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
    final l10n = context.l10n;
    return SubmenuButton(
      menuChildren: [
        MenuItemButton(
          onPressed: () => _launchUrl('https://file4base.github.io/file4base-app/'),
          leadingIcon: const Icon(Icons.help_center_outlined, size: 18),
          child: Text(l10n.menuHelpFile4BaseHelp),
        ),
        MenuItemButton(
          onPressed: () => _launchUrl('https://file4base.github.io/file4base-app/#guides'),
          leadingIcon: const Icon(Icons.menu_book_outlined, size: 18),
          child: Text(l10n.menuHelpResourceCenter),
        ),
        MenuItemButton(
          onPressed: () => _launchUrl('https://file4base.github.io/file4base-app/#rest-api'),
          leadingIcon: const Icon(Icons.description_outlined, size: 18),
          child: Text(l10n.menuHelpProductDocumentation),
        ),
        MenuItemButton(
          onPressed: () => _launchUrl('https://github.com/file4base/file4base-app/discussions'),
          leadingIcon: const Icon(Icons.forum_outlined, size: 18),
          child: Text(l10n.menuHelpCommunity),
        ),
        MenuItemButton(
          onPressed: () => IssuesGuideDialog.show(context),
          leadingIcon: const Icon(Icons.support_agent_outlined, size: 18),
          child: Text(l10n.menuHelpServiceSupport),
        ),
        const Divider(height: 1),
        MenuItemButton(
          onPressed: () => CheckUpdatesDialog.show(context),
          leadingIcon: const Icon(Icons.system_update_outlined, size: 18),
          child: Text(l10n.menuHelpCheckForUpdates),
        ),
        MenuItemButton(
          onPressed: onAbout,
          leadingIcon: const Icon(Icons.info_outline, size: 18),
          child: Text(l10n.menuHelpAbout),
        ),
      ],
      child: Text(l10n.menuHelp, style: const TextStyle(fontSize: 13)),
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
      unawaited(Clipboard.setData(ClipboardData(text: textToCopy)));
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
      unawaited(Clipboard.setData(ClipboardData(text: textToCopy)));
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

