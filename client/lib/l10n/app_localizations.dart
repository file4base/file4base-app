import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:intl/intl.dart' as intl;

import 'app_localizations_de.dart';
import 'app_localizations_en.dart';
import 'app_localizations_es.dart';
import 'app_localizations_fr.dart';
import 'app_localizations_it.dart';
import 'app_localizations_nl.dart';
import 'app_localizations_pt.dart';

// ignore_for_file: type=lint

/// Callers can lookup localized strings with an instance of AppLocalizations
/// returned by `AppLocalizations.of(context)`.
///
/// Applications need to include `AppLocalizations.delegate()` in their app's
/// `localizationDelegates` list, and the locales they support in the app's
/// `supportedLocales` list. For example:
///
/// ```dart
/// import 'l10n/app_localizations.dart';
///
/// return MaterialApp(
///   localizationsDelegates: AppLocalizations.localizationsDelegates,
///   supportedLocales: AppLocalizations.supportedLocales,
///   home: MyApplicationHome(),
/// );
/// ```
///
/// ## Update pubspec.yaml
///
/// Please make sure to update your pubspec.yaml to include the following
/// packages:
///
/// ```yaml
/// dependencies:
///   # Internationalization support.
///   flutter_localizations:
///     sdk: flutter
///   intl: any # Use the pinned version from flutter_localizations
///
///   # Rest of dependencies
/// ```
///
/// ## iOS Applications
///
/// iOS applications define key application metadata, including supported
/// locales, in an Info.plist file that is built into the application bundle.
/// To configure the locales supported by your app, you’ll need to edit this
/// file.
///
/// First, open your project’s ios/Runner.xcworkspace Xcode workspace file.
/// Then, in the Project Navigator, open the Info.plist file under the Runner
/// project’s Runner folder.
///
/// Next, select the Information Property List item, select Add Item from the
/// Editor menu, then select Localizations from the pop-up menu.
///
/// Select and expand the newly-created Localizations item then, for each
/// locale your application supports, add a new item and select the locale
/// you wish to add from the pop-up menu in the Value field. This list should
/// be consistent with the languages listed in the AppLocalizations.supportedLocales
/// property.
abstract class AppLocalizations {
  AppLocalizations(String locale)
    : localeName = intl.Intl.canonicalizedLocale(locale.toString());

  final String localeName;

  static AppLocalizations? of(BuildContext context) {
    return Localizations.of<AppLocalizations>(context, AppLocalizations);
  }

  static const LocalizationsDelegate<AppLocalizations> delegate =
      _AppLocalizationsDelegate();

  /// A list of this localizations delegate along with the default localizations
  /// delegates.
  ///
  /// Returns a list of localizations delegates containing this delegate along with
  /// GlobalMaterialLocalizations.delegate, GlobalCupertinoLocalizations.delegate,
  /// and GlobalWidgetsLocalizations.delegate.
  ///
  /// Additional delegates can be added by appending to this list in
  /// MaterialApp. This list does not have to be used at all if a custom list
  /// of delegates is preferred or required.
  static const List<LocalizationsDelegate<dynamic>> localizationsDelegates =
      <LocalizationsDelegate<dynamic>>[
        delegate,
        GlobalMaterialLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
      ];

  /// A list of this localizations delegate's supported locales.
  static const List<Locale> supportedLocales = <Locale>[
    Locale('de'),
    Locale('en'),
    Locale('es'),
    Locale('fr'),
    Locale('it'),
    Locale('nl'),
    Locale('pt'),
  ];

  /// Top-level File menu
  ///
  /// In en, this message translates to:
  /// **'File'**
  String get menuFile;

  /// Top-level Edit menu
  ///
  /// In en, this message translates to:
  /// **'Edit'**
  String get menuEdit;

  /// Top-level View menu
  ///
  /// In en, this message translates to:
  /// **'View'**
  String get menuView;

  /// Top-level Insert menu
  ///
  /// In en, this message translates to:
  /// **'Insert'**
  String get menuInsert;

  /// Top-level Format menu
  ///
  /// In en, this message translates to:
  /// **'Format'**
  String get menuFormat;

  /// Top-level Records menu (Browse/Layout/Preview modes)
  ///
  /// In en, this message translates to:
  /// **'Records'**
  String get menuRecords;

  /// Top-level Requests menu (Find mode)
  ///
  /// In en, this message translates to:
  /// **'Requests'**
  String get menuRequests;

  /// Top-level Scripts menu
  ///
  /// In en, this message translates to:
  /// **'Scripts'**
  String get menuScripts;

  /// Top-level Tools menu
  ///
  /// In en, this message translates to:
  /// **'Tools'**
  String get menuTools;

  /// Top-level Server Tools menu
  ///
  /// In en, this message translates to:
  /// **'Server Tools'**
  String get menuServerTools;

  /// Top-level Window menu
  ///
  /// In en, this message translates to:
  /// **'Window'**
  String get menuWindow;

  /// Top-level Help menu
  ///
  /// In en, this message translates to:
  /// **'Help'**
  String get menuHelp;

  /// No description provided for @menuFileSignIn.
  ///
  /// In en, this message translates to:
  /// **'Sign In...'**
  String get menuFileSignIn;

  /// No description provided for @menuFileNewDatabase.
  ///
  /// In en, this message translates to:
  /// **'New Database...'**
  String get menuFileNewDatabase;

  /// No description provided for @menuFileOpen.
  ///
  /// In en, this message translates to:
  /// **'Open...'**
  String get menuFileOpen;

  /// No description provided for @menuFileOpenRemote.
  ///
  /// In en, this message translates to:
  /// **'Open Remote...'**
  String get menuFileOpenRemote;

  /// No description provided for @menuFileSave.
  ///
  /// In en, this message translates to:
  /// **'Save'**
  String get menuFileSave;

  /// No description provided for @menuFileSaveAs.
  ///
  /// In en, this message translates to:
  /// **'Save As...'**
  String get menuFileSaveAs;

  /// No description provided for @menuFileSaveCopyAs.
  ///
  /// In en, this message translates to:
  /// **'Save a Copy As...'**
  String get menuFileSaveCopyAs;

  /// No description provided for @menuFileExportData.
  ///
  /// In en, this message translates to:
  /// **'Export Data...'**
  String get menuFileExportData;

  /// No description provided for @menuFileImportData.
  ///
  /// In en, this message translates to:
  /// **'Import Data (.f4data)...'**
  String get menuFileImportData;

  /// No description provided for @menuFileOpenFavorite.
  ///
  /// In en, this message translates to:
  /// **'Open Favorite'**
  String get menuFileOpenFavorite;

  /// No description provided for @menuFileLocalHost.
  ///
  /// In en, this message translates to:
  /// **'Local Host (localhost:8080)'**
  String get menuFileLocalHost;

  /// No description provided for @menuFileOpenRecent.
  ///
  /// In en, this message translates to:
  /// **'Open Recent'**
  String get menuFileOpenRecent;

  /// No description provided for @menuFileSignOutLock.
  ///
  /// In en, this message translates to:
  /// **'Sign Out (Lock Session)'**
  String get menuFileSignOutLock;

  /// No description provided for @menuFileManage.
  ///
  /// In en, this message translates to:
  /// **'Manage'**
  String get menuFileManage;

  /// No description provided for @menuManageDatabase.
  ///
  /// In en, this message translates to:
  /// **'Database...'**
  String get menuManageDatabase;

  /// No description provided for @menuManageSecurity.
  ///
  /// In en, this message translates to:
  /// **'Security...'**
  String get menuManageSecurity;

  /// No description provided for @menuManageValueLists.
  ///
  /// In en, this message translates to:
  /// **'Value Lists...'**
  String get menuManageValueLists;

  /// No description provided for @menuManageCustomFunctions.
  ///
  /// In en, this message translates to:
  /// **'Custom Functions...'**
  String get menuManageCustomFunctions;

  /// No description provided for @menuManageFileReferences.
  ///
  /// In en, this message translates to:
  /// **'File References...'**
  String get menuManageFileReferences;

  /// No description provided for @menuManageExternalDataSources.
  ///
  /// In en, this message translates to:
  /// **'External Data Sources...'**
  String get menuManageExternalDataSources;

  /// No description provided for @menuManageLayouts.
  ///
  /// In en, this message translates to:
  /// **'Layouts...'**
  String get menuManageLayouts;

  /// No description provided for @menuManageScripts.
  ///
  /// In en, this message translates to:
  /// **'Scripts...'**
  String get menuManageScripts;

  /// No description provided for @menuManageScriptTriggers.
  ///
  /// In en, this message translates to:
  /// **'Script Triggers...'**
  String get menuManageScriptTriggers;

  /// No description provided for @menuManageThemes.
  ///
  /// In en, this message translates to:
  /// **'Themes...'**
  String get menuManageThemes;

  /// No description provided for @menuFileOptions.
  ///
  /// In en, this message translates to:
  /// **'File Options...'**
  String get menuFileOptions;

  /// No description provided for @menuFileChangePassword.
  ///
  /// In en, this message translates to:
  /// **'Change Password...'**
  String get menuFileChangePassword;

  /// No description provided for @menuFilePageSetup.
  ///
  /// In en, this message translates to:
  /// **'Page Setup...'**
  String get menuFilePageSetup;

  /// No description provided for @menuFilePrint.
  ///
  /// In en, this message translates to:
  /// **'Print...'**
  String get menuFilePrint;

  /// No description provided for @menuFileImportRecords.
  ///
  /// In en, this message translates to:
  /// **'Import Records'**
  String get menuFileImportRecords;

  /// No description provided for @menuFileImportFile.
  ///
  /// In en, this message translates to:
  /// **'File...'**
  String get menuFileImportFile;

  /// No description provided for @menuFileImportFolder.
  ///
  /// In en, this message translates to:
  /// **'Folder...'**
  String get menuFileImportFolder;

  /// No description provided for @menuFileImportXmlDataSource.
  ///
  /// In en, this message translates to:
  /// **'XML Data Source...'**
  String get menuFileImportXmlDataSource;

  /// No description provided for @menuFileImportExternalSqlDataSource.
  ///
  /// In en, this message translates to:
  /// **'External SQL Data Source...'**
  String get menuFileImportExternalSqlDataSource;

  /// No description provided for @menuFileExportRecords.
  ///
  /// In en, this message translates to:
  /// **'Export Records...'**
  String get menuFileExportRecords;

  /// No description provided for @menuFileRecover.
  ///
  /// In en, this message translates to:
  /// **'Recover...'**
  String get menuFileRecover;

  /// No description provided for @menuFileQuit.
  ///
  /// In en, this message translates to:
  /// **'Quit'**
  String get menuFileQuit;

  /// No description provided for @menuEditUndo.
  ///
  /// In en, this message translates to:
  /// **'Undo'**
  String get menuEditUndo;

  /// No description provided for @menuEditRedo.
  ///
  /// In en, this message translates to:
  /// **'Redo'**
  String get menuEditRedo;

  /// No description provided for @menuEditCut.
  ///
  /// In en, this message translates to:
  /// **'Cut'**
  String get menuEditCut;

  /// No description provided for @menuEditCopy.
  ///
  /// In en, this message translates to:
  /// **'Copy'**
  String get menuEditCopy;

  /// No description provided for @menuEditPaste.
  ///
  /// In en, this message translates to:
  /// **'Paste'**
  String get menuEditPaste;

  /// No description provided for @menuEditClear.
  ///
  /// In en, this message translates to:
  /// **'Clear'**
  String get menuEditClear;

  /// No description provided for @menuEditSelectAll.
  ///
  /// In en, this message translates to:
  /// **'Select All'**
  String get menuEditSelectAll;

  /// No description provided for @menuEditFindReplace.
  ///
  /// In en, this message translates to:
  /// **'Find/Replace'**
  String get menuEditFindReplace;

  /// No description provided for @menuEditFindReplaceEllipsis.
  ///
  /// In en, this message translates to:
  /// **'Find/Replace...'**
  String get menuEditFindReplaceEllipsis;

  /// No description provided for @menuEditFindNext.
  ///
  /// In en, this message translates to:
  /// **'Find Next'**
  String get menuEditFindNext;

  /// No description provided for @menuEditFindPrevious.
  ///
  /// In en, this message translates to:
  /// **'Find Previous'**
  String get menuEditFindPrevious;

  /// No description provided for @menuEditSpelling.
  ///
  /// In en, this message translates to:
  /// **'Spelling'**
  String get menuEditSpelling;

  /// No description provided for @menuEditCheckSelection.
  ///
  /// In en, this message translates to:
  /// **'Check Selection...'**
  String get menuEditCheckSelection;

  /// No description provided for @menuEditCheckRecord.
  ///
  /// In en, this message translates to:
  /// **'Check Record...'**
  String get menuEditCheckRecord;

  /// No description provided for @menuEditCorrectWord.
  ///
  /// In en, this message translates to:
  /// **'Correct Word...'**
  String get menuEditCorrectWord;

  /// No description provided for @menuEditDictionaries.
  ///
  /// In en, this message translates to:
  /// **'Dictionaries...'**
  String get menuEditDictionaries;

  /// No description provided for @menuEditEditUserDictionary.
  ///
  /// In en, this message translates to:
  /// **'Edit User Dictionary...'**
  String get menuEditEditUserDictionary;

  /// No description provided for @menuEditSpellingOptions.
  ///
  /// In en, this message translates to:
  /// **'Options...'**
  String get menuEditSpellingOptions;

  /// No description provided for @menuEditPreferences.
  ///
  /// In en, this message translates to:
  /// **'Preferences...'**
  String get menuEditPreferences;

  /// No description provided for @menuViewBrowseMode.
  ///
  /// In en, this message translates to:
  /// **'Browse Mode'**
  String get menuViewBrowseMode;

  /// No description provided for @menuViewFindMode.
  ///
  /// In en, this message translates to:
  /// **'Find Mode'**
  String get menuViewFindMode;

  /// No description provided for @menuViewLayoutMode.
  ///
  /// In en, this message translates to:
  /// **'Layout Mode'**
  String get menuViewLayoutMode;

  /// No description provided for @menuViewPreviewMode.
  ///
  /// In en, this message translates to:
  /// **'Preview Mode'**
  String get menuViewPreviewMode;

  /// No description provided for @menuViewPageMargins.
  ///
  /// In en, this message translates to:
  /// **'Page Margins'**
  String get menuViewPageMargins;

  /// No description provided for @menuViewStatusToolbar.
  ///
  /// In en, this message translates to:
  /// **'Status Toolbar'**
  String get menuViewStatusToolbar;

  /// No description provided for @menuViewFormattingBar.
  ///
  /// In en, this message translates to:
  /// **'Formatting Bar'**
  String get menuViewFormattingBar;

  /// No description provided for @menuViewTextRuler.
  ///
  /// In en, this message translates to:
  /// **'Text Ruler'**
  String get menuViewTextRuler;

  /// No description provided for @menuViewZoomIn.
  ///
  /// In en, this message translates to:
  /// **'Zoom In'**
  String get menuViewZoomIn;

  /// No description provided for @menuViewZoomOut.
  ///
  /// In en, this message translates to:
  /// **'Zoom Out'**
  String get menuViewZoomOut;

  /// No description provided for @menuViewActualSize.
  ///
  /// In en, this message translates to:
  /// **'Actual Size (100%)'**
  String get menuViewActualSize;

  /// No description provided for @menuViewZoomLevel.
  ///
  /// In en, this message translates to:
  /// **'Zoom Level'**
  String get menuViewZoomLevel;

  /// No description provided for @menuInsertPicture.
  ///
  /// In en, this message translates to:
  /// **'Picture...'**
  String get menuInsertPicture;

  /// No description provided for @menuInsertAudioVideo.
  ///
  /// In en, this message translates to:
  /// **'Audio/Video...'**
  String get menuInsertAudioVideo;

  /// No description provided for @menuInsertPdf.
  ///
  /// In en, this message translates to:
  /// **'PDF...'**
  String get menuInsertPdf;

  /// No description provided for @menuInsertQuickTime.
  ///
  /// In en, this message translates to:
  /// **'QuickTime...'**
  String get menuInsertQuickTime;

  /// No description provided for @menuInsertFile.
  ///
  /// In en, this message translates to:
  /// **'File...'**
  String get menuInsertFile;

  /// No description provided for @menuInsertCurrentDate.
  ///
  /// In en, this message translates to:
  /// **'Current Date'**
  String get menuInsertCurrentDate;

  /// No description provided for @menuInsertCurrentTime.
  ///
  /// In en, this message translates to:
  /// **'Current Time'**
  String get menuInsertCurrentTime;

  /// No description provided for @menuInsertCurrentUserName.
  ///
  /// In en, this message translates to:
  /// **'Current User Name'**
  String get menuInsertCurrentUserName;

  /// No description provided for @menuInsertPageNumber.
  ///
  /// In en, this message translates to:
  /// **'Page Number'**
  String get menuInsertPageNumber;

  /// No description provided for @menuInsertFromIndex.
  ///
  /// In en, this message translates to:
  /// **'From Index...'**
  String get menuInsertFromIndex;

  /// No description provided for @menuInsertFromLastVisitedRecord.
  ///
  /// In en, this message translates to:
  /// **'From Last Visited Record'**
  String get menuInsertFromLastVisitedRecord;

  /// No description provided for @menuInsertMergeField.
  ///
  /// In en, this message translates to:
  /// **'Merge Field...'**
  String get menuInsertMergeField;

  /// No description provided for @menuInsertMergeVariable.
  ///
  /// In en, this message translates to:
  /// **'Merge Variable...'**
  String get menuInsertMergeVariable;

  /// No description provided for @menuFormatFont.
  ///
  /// In en, this message translates to:
  /// **'Font'**
  String get menuFormatFont;

  /// No description provided for @menuFormatSystemDefault.
  ///
  /// In en, this message translates to:
  /// **'System Default'**
  String get menuFormatSystemDefault;

  /// No description provided for @menuFormatSize.
  ///
  /// In en, this message translates to:
  /// **'Size'**
  String get menuFormatSize;

  /// Font size in points
  ///
  /// In en, this message translates to:
  /// **'{size} pt'**
  String menuFormatSizePt(int size);

  /// No description provided for @menuFormatStyle.
  ///
  /// In en, this message translates to:
  /// **'Style'**
  String get menuFormatStyle;

  /// No description provided for @menuFormatPlainText.
  ///
  /// In en, this message translates to:
  /// **'Plain Text'**
  String get menuFormatPlainText;

  /// No description provided for @menuFormatBold.
  ///
  /// In en, this message translates to:
  /// **'Bold'**
  String get menuFormatBold;

  /// No description provided for @menuFormatItalic.
  ///
  /// In en, this message translates to:
  /// **'Italic'**
  String get menuFormatItalic;

  /// No description provided for @menuFormatUnderline.
  ///
  /// In en, this message translates to:
  /// **'Underline'**
  String get menuFormatUnderline;

  /// No description provided for @menuFormatStrikethrough.
  ///
  /// In en, this message translates to:
  /// **'Strikethrough'**
  String get menuFormatStrikethrough;

  /// No description provided for @menuFormatUppercase.
  ///
  /// In en, this message translates to:
  /// **'UPPERCASE'**
  String get menuFormatUppercase;

  /// No description provided for @menuFormatLowercase.
  ///
  /// In en, this message translates to:
  /// **'lowercase'**
  String get menuFormatLowercase;

  /// No description provided for @menuFormatTitleCase.
  ///
  /// In en, this message translates to:
  /// **'Title Case'**
  String get menuFormatTitleCase;

  /// No description provided for @menuFormatAlignText.
  ///
  /// In en, this message translates to:
  /// **'Align Text'**
  String get menuFormatAlignText;

  /// No description provided for @menuFormatAlignLeft.
  ///
  /// In en, this message translates to:
  /// **'Left'**
  String get menuFormatAlignLeft;

  /// No description provided for @menuFormatAlignCenter.
  ///
  /// In en, this message translates to:
  /// **'Center'**
  String get menuFormatAlignCenter;

  /// No description provided for @menuFormatAlignRight.
  ///
  /// In en, this message translates to:
  /// **'Right'**
  String get menuFormatAlignRight;

  /// No description provided for @menuFormatAlignJustify.
  ///
  /// In en, this message translates to:
  /// **'Full (Justify)'**
  String get menuFormatAlignJustify;

  /// No description provided for @menuFormatLineSpacing.
  ///
  /// In en, this message translates to:
  /// **'Line Spacing'**
  String get menuFormatLineSpacing;

  /// No description provided for @menuFormatLineSpacingSingle.
  ///
  /// In en, this message translates to:
  /// **'Single (1.0x)'**
  String get menuFormatLineSpacingSingle;

  /// No description provided for @menuFormatLineSpacing115.
  ///
  /// In en, this message translates to:
  /// **'1.15x'**
  String get menuFormatLineSpacing115;

  /// No description provided for @menuFormatLineSpacingStandard.
  ///
  /// In en, this message translates to:
  /// **'Standard (1.2x)'**
  String get menuFormatLineSpacingStandard;

  /// No description provided for @menuFormatLineSpacingOneHalf.
  ///
  /// In en, this message translates to:
  /// **'One and a Half (1.5x)'**
  String get menuFormatLineSpacingOneHalf;

  /// No description provided for @menuFormatLineSpacingDouble.
  ///
  /// In en, this message translates to:
  /// **'Double (2.0x)'**
  String get menuFormatLineSpacingDouble;

  /// No description provided for @menuFormatTextColor.
  ///
  /// In en, this message translates to:
  /// **'Text Color'**
  String get menuFormatTextColor;

  /// No description provided for @menuFormatDefaultThemeColor.
  ///
  /// In en, this message translates to:
  /// **'Default / Theme Color'**
  String get menuFormatDefaultThemeColor;

  /// No description provided for @colorBlack.
  ///
  /// In en, this message translates to:
  /// **'Black'**
  String get colorBlack;

  /// No description provided for @colorCharcoal.
  ///
  /// In en, this message translates to:
  /// **'Charcoal'**
  String get colorCharcoal;

  /// No description provided for @colorDarkGrey.
  ///
  /// In en, this message translates to:
  /// **'Dark Grey'**
  String get colorDarkGrey;

  /// No description provided for @colorGrey.
  ///
  /// In en, this message translates to:
  /// **'Grey'**
  String get colorGrey;

  /// No description provided for @colorLightGrey.
  ///
  /// In en, this message translates to:
  /// **'Light Grey'**
  String get colorLightGrey;

  /// No description provided for @colorWhite.
  ///
  /// In en, this message translates to:
  /// **'White'**
  String get colorWhite;

  /// No description provided for @colorBlue.
  ///
  /// In en, this message translates to:
  /// **'Blue'**
  String get colorBlue;

  /// No description provided for @colorNavy.
  ///
  /// In en, this message translates to:
  /// **'Navy'**
  String get colorNavy;

  /// No description provided for @colorCyan.
  ///
  /// In en, this message translates to:
  /// **'Cyan'**
  String get colorCyan;

  /// No description provided for @colorGreen.
  ///
  /// In en, this message translates to:
  /// **'Green'**
  String get colorGreen;

  /// No description provided for @colorAmber.
  ///
  /// In en, this message translates to:
  /// **'Amber'**
  String get colorAmber;

  /// No description provided for @colorOrange.
  ///
  /// In en, this message translates to:
  /// **'Orange'**
  String get colorOrange;

  /// No description provided for @colorRed.
  ///
  /// In en, this message translates to:
  /// **'Red'**
  String get colorRed;

  /// No description provided for @colorPurple.
  ///
  /// In en, this message translates to:
  /// **'Purple'**
  String get colorPurple;

  /// No description provided for @menuFormatText.
  ///
  /// In en, this message translates to:
  /// **'Text...'**
  String get menuFormatText;

  /// No description provided for @menuFormatNumber.
  ///
  /// In en, this message translates to:
  /// **'Number...'**
  String get menuFormatNumber;

  /// No description provided for @menuFormatDate.
  ///
  /// In en, this message translates to:
  /// **'Date...'**
  String get menuFormatDate;

  /// No description provided for @menuFormatTime.
  ///
  /// In en, this message translates to:
  /// **'Time...'**
  String get menuFormatTime;

  /// No description provided for @menuFormatTimestamp.
  ///
  /// In en, this message translates to:
  /// **'Timestamp...'**
  String get menuFormatTimestamp;

  /// No description provided for @menuFormatConditional.
  ///
  /// In en, this message translates to:
  /// **'Conditional...'**
  String get menuFormatConditional;

  /// No description provided for @menuFormatAlignObject.
  ///
  /// In en, this message translates to:
  /// **'Align Object'**
  String get menuFormatAlignObject;

  /// No description provided for @menuFormatAlignLeftEdges.
  ///
  /// In en, this message translates to:
  /// **'Align Left Edges'**
  String get menuFormatAlignLeftEdges;

  /// No description provided for @menuFormatAlignHorizontalCenters.
  ///
  /// In en, this message translates to:
  /// **'Align Horizontal Centers'**
  String get menuFormatAlignHorizontalCenters;

  /// No description provided for @menuFormatAlignRightEdges.
  ///
  /// In en, this message translates to:
  /// **'Align Right Edges'**
  String get menuFormatAlignRightEdges;

  /// No description provided for @menuFormatAlignTopEdges.
  ///
  /// In en, this message translates to:
  /// **'Align Top Edges'**
  String get menuFormatAlignTopEdges;

  /// No description provided for @menuFormatAlignVerticalCenters.
  ///
  /// In en, this message translates to:
  /// **'Align Vertical Centers'**
  String get menuFormatAlignVerticalCenters;

  /// No description provided for @menuFormatAlignBottomEdges.
  ///
  /// In en, this message translates to:
  /// **'Align Bottom Edges'**
  String get menuFormatAlignBottomEdges;

  /// No description provided for @menuFormatDistribute.
  ///
  /// In en, this message translates to:
  /// **'Distribute'**
  String get menuFormatDistribute;

  /// No description provided for @menuFormatDistributeHorizontally.
  ///
  /// In en, this message translates to:
  /// **'Distribute Horizontally'**
  String get menuFormatDistributeHorizontally;

  /// No description provided for @menuFormatDistributeVertically.
  ///
  /// In en, this message translates to:
  /// **'Distribute Vertically'**
  String get menuFormatDistributeVertically;

  /// No description provided for @menuFormatResize.
  ///
  /// In en, this message translates to:
  /// **'Resize'**
  String get menuFormatResize;

  /// No description provided for @menuFormatMatchWidth.
  ///
  /// In en, this message translates to:
  /// **'Match Smallest/Primary Width'**
  String get menuFormatMatchWidth;

  /// No description provided for @menuFormatMatchHeight.
  ///
  /// In en, this message translates to:
  /// **'Match Smallest/Primary Height'**
  String get menuFormatMatchHeight;

  /// No description provided for @menuFormatMatchBoth.
  ///
  /// In en, this message translates to:
  /// **'Match Both Width & Height'**
  String get menuFormatMatchBoth;

  /// No description provided for @menuFormatSnapToGrid.
  ///
  /// In en, this message translates to:
  /// **'Snap Bounds to 8pt Grid'**
  String get menuFormatSnapToGrid;

  /// No description provided for @menuRecordsNewRecord.
  ///
  /// In en, this message translates to:
  /// **'New Record'**
  String get menuRecordsNewRecord;

  /// No description provided for @menuRecordsDuplicateRecord.
  ///
  /// In en, this message translates to:
  /// **'Duplicate Record'**
  String get menuRecordsDuplicateRecord;

  /// No description provided for @menuRecordsDeleteRecord.
  ///
  /// In en, this message translates to:
  /// **'Delete Record...'**
  String get menuRecordsDeleteRecord;

  /// No description provided for @menuRecordsDeleteAllRecords.
  ///
  /// In en, this message translates to:
  /// **'Delete All Records...'**
  String get menuRecordsDeleteAllRecords;

  /// No description provided for @menuRecordsCommitRecord.
  ///
  /// In en, this message translates to:
  /// **'Commit Record'**
  String get menuRecordsCommitRecord;

  /// No description provided for @menuRecordsRevertRecord.
  ///
  /// In en, this message translates to:
  /// **'Revert Record'**
  String get menuRecordsRevertRecord;

  /// No description provided for @menuRecordsGoToRecord.
  ///
  /// In en, this message translates to:
  /// **'Go to Record'**
  String get menuRecordsGoToRecord;

  /// No description provided for @menuNavFirst.
  ///
  /// In en, this message translates to:
  /// **'First'**
  String get menuNavFirst;

  /// No description provided for @menuNavPrevious.
  ///
  /// In en, this message translates to:
  /// **'Previous'**
  String get menuNavPrevious;

  /// No description provided for @menuNavNext.
  ///
  /// In en, this message translates to:
  /// **'Next'**
  String get menuNavNext;

  /// No description provided for @menuNavLast.
  ///
  /// In en, this message translates to:
  /// **'Last'**
  String get menuNavLast;

  /// No description provided for @menuNavByNumber.
  ///
  /// In en, this message translates to:
  /// **'By Number...'**
  String get menuNavByNumber;

  /// No description provided for @menuRecordsShowAllRecords.
  ///
  /// In en, this message translates to:
  /// **'Show All Records'**
  String get menuRecordsShowAllRecords;

  /// No description provided for @menuRecordsSavedFinds.
  ///
  /// In en, this message translates to:
  /// **'Saved Finds'**
  String get menuRecordsSavedFinds;

  /// No description provided for @menuRecordsSaveCurrentFind.
  ///
  /// In en, this message translates to:
  /// **'Save Current Find...'**
  String get menuRecordsSaveCurrentFind;

  /// No description provided for @menuRecordsEditSavedFinds.
  ///
  /// In en, this message translates to:
  /// **'Edit Saved Finds...'**
  String get menuRecordsEditSavedFinds;

  /// No description provided for @menuRecordsNoSavedFinds.
  ///
  /// In en, this message translates to:
  /// **'No saved finds for this table'**
  String get menuRecordsNoSavedFinds;

  /// No description provided for @menuRecordsShowOmittedOnly.
  ///
  /// In en, this message translates to:
  /// **'Show Omitted Only'**
  String get menuRecordsShowOmittedOnly;

  /// No description provided for @menuRecordsOmitRecord.
  ///
  /// In en, this message translates to:
  /// **'Omit Record'**
  String get menuRecordsOmitRecord;

  /// No description provided for @menuRecordsOmitMultiple.
  ///
  /// In en, this message translates to:
  /// **'Omit Multiple...'**
  String get menuRecordsOmitMultiple;

  /// No description provided for @menuRecordsSortRecords.
  ///
  /// In en, this message translates to:
  /// **'Sort Records...'**
  String get menuRecordsSortRecords;

  /// No description provided for @menuRecordsUnsort.
  ///
  /// In en, this message translates to:
  /// **'Unsort'**
  String get menuRecordsUnsort;

  /// No description provided for @menuRecordsReplaceFieldContents.
  ///
  /// In en, this message translates to:
  /// **'Replace Field Contents...'**
  String get menuRecordsReplaceFieldContents;

  /// No description provided for @menuRecordsRelookupFieldContents.
  ///
  /// In en, this message translates to:
  /// **'Relookup Field Contents'**
  String get menuRecordsRelookupFieldContents;

  /// No description provided for @menuRequestsPerformFind.
  ///
  /// In en, this message translates to:
  /// **'Perform Find'**
  String get menuRequestsPerformFind;

  /// No description provided for @menuRequestsNewRequest.
  ///
  /// In en, this message translates to:
  /// **'New Request'**
  String get menuRequestsNewRequest;

  /// No description provided for @menuRequestsDuplicateRequest.
  ///
  /// In en, this message translates to:
  /// **'Duplicate Request'**
  String get menuRequestsDuplicateRequest;

  /// No description provided for @menuRequestsDeleteRequest.
  ///
  /// In en, this message translates to:
  /// **'Delete Request'**
  String get menuRequestsDeleteRequest;

  /// No description provided for @menuRequestsDeleteAllRequests.
  ///
  /// In en, this message translates to:
  /// **'Delete All Requests'**
  String get menuRequestsDeleteAllRequests;

  /// No description provided for @menuRequestsIncludeOmit.
  ///
  /// In en, this message translates to:
  /// **'Include / Omit'**
  String get menuRequestsIncludeOmit;

  /// No description provided for @menuRequestsGoToRequest.
  ///
  /// In en, this message translates to:
  /// **'Go to Request'**
  String get menuRequestsGoToRequest;

  /// No description provided for @menuRequestsShowAllCancelFind.
  ///
  /// In en, this message translates to:
  /// **'Show All Records (Cancel Find)'**
  String get menuRequestsShowAllCancelFind;

  /// No description provided for @menuScriptsWorkspace.
  ///
  /// In en, this message translates to:
  /// **'Script Workspace...'**
  String get menuScriptsWorkspace;

  /// No description provided for @menuScriptsCustomListPlaceholder.
  ///
  /// In en, this message translates to:
  /// **'[Custom User Scripts List]'**
  String get menuScriptsCustomListPlaceholder;

  /// No description provided for @menuToolsScriptDebugger.
  ///
  /// In en, this message translates to:
  /// **'Script Debugger'**
  String get menuToolsScriptDebugger;

  /// No description provided for @menuToolsDataViewer.
  ///
  /// In en, this message translates to:
  /// **'Data Viewer'**
  String get menuToolsDataViewer;

  /// No description provided for @menuToolsDatabaseDesignReport.
  ///
  /// In en, this message translates to:
  /// **'Database Design Report...'**
  String get menuToolsDatabaseDesignReport;

  /// No description provided for @menuToolsSaveCopyAsXml.
  ///
  /// In en, this message translates to:
  /// **'Save a Copy as XML...'**
  String get menuToolsSaveCopyAsXml;

  /// No description provided for @menuToolsCustomMenus.
  ///
  /// In en, this message translates to:
  /// **'Custom Menus'**
  String get menuToolsCustomMenus;

  /// No description provided for @menuToolsManageCustomMenus.
  ///
  /// In en, this message translates to:
  /// **'Manage Custom Menus...'**
  String get menuToolsManageCustomMenus;

  /// No description provided for @menuToolsStandardMenus.
  ///
  /// In en, this message translates to:
  /// **'[Standard File4Base Menus]'**
  String get menuToolsStandardMenus;

  /// No description provided for @menuToolsDeveloperUtilities.
  ///
  /// In en, this message translates to:
  /// **'Developer Utilities...'**
  String get menuToolsDeveloperUtilities;

  /// No description provided for @menuToolsLayoutThemes.
  ///
  /// In en, this message translates to:
  /// **'Layout Themes...'**
  String get menuToolsLayoutThemes;

  /// Badge shown in Server Tools when a container update is available
  ///
  /// In en, this message translates to:
  /// **'UPDATE {version}'**
  String menuServerToolsUpdateBadge(String version);

  /// No description provided for @menuServerToolsNewBadge.
  ///
  /// In en, this message translates to:
  /// **'NEW'**
  String get menuServerToolsNewBadge;

  /// No description provided for @menuServerToolsContainers.
  ///
  /// In en, this message translates to:
  /// **'Containers & Services...'**
  String get menuServerToolsContainers;

  /// No description provided for @menuServerToolsBackups.
  ///
  /// In en, this message translates to:
  /// **'Backups & Snapshots...'**
  String get menuServerToolsBackups;

  /// No description provided for @menuServerToolsDomainTls.
  ///
  /// In en, this message translates to:
  /// **'Domain & SSL/TLS Certificates (ACME)...'**
  String get menuServerToolsDomainTls;

  /// No description provided for @menuServerToolsTelemetry.
  ///
  /// In en, this message translates to:
  /// **'API Telemetry & Live Metrics...'**
  String get menuServerToolsTelemetry;

  /// No description provided for @menuServerToolsSessions.
  ///
  /// In en, this message translates to:
  /// **'Connected Clients & Sessions...'**
  String get menuServerToolsSessions;

  /// No description provided for @menuServerToolsSchedules.
  ///
  /// In en, this message translates to:
  /// **'Scheduled Tasks & Cron Scripts...'**
  String get menuServerToolsSchedules;

  /// No description provided for @menuServerToolsAuditLogs.
  ///
  /// In en, this message translates to:
  /// **'Audit & Security Logs...'**
  String get menuServerToolsAuditLogs;

  /// No description provided for @menuServerToolsMaintenance.
  ///
  /// In en, this message translates to:
  /// **'Database Maintenance & Optimizer...'**
  String get menuServerToolsMaintenance;

  /// No description provided for @menuServerToolsNotifications.
  ///
  /// In en, this message translates to:
  /// **'Server Notifications, SMTP & Webhooks...'**
  String get menuServerToolsNotifications;

  /// No description provided for @menuWindowMinimize.
  ///
  /// In en, this message translates to:
  /// **'Minimize'**
  String get menuWindowMinimize;

  /// No description provided for @menuWindowZoom.
  ///
  /// In en, this message translates to:
  /// **'Zoom'**
  String get menuWindowZoom;

  /// No description provided for @menuWindowTileHorizontally.
  ///
  /// In en, this message translates to:
  /// **'Tile Horizontally'**
  String get menuWindowTileHorizontally;

  /// No description provided for @menuWindowTileVertically.
  ///
  /// In en, this message translates to:
  /// **'Tile Vertically'**
  String get menuWindowTileVertically;

  /// No description provided for @menuWindowCascade.
  ///
  /// In en, this message translates to:
  /// **'Cascade'**
  String get menuWindowCascade;

  /// No description provided for @menuWindowNewWindow.
  ///
  /// In en, this message translates to:
  /// **'New Window'**
  String get menuWindowNewWindow;

  /// No description provided for @menuWindowShowWindow.
  ///
  /// In en, this message translates to:
  /// **'Show Window'**
  String get menuWindowShowWindow;

  /// No description provided for @menuWindowMainWorkspace.
  ///
  /// In en, this message translates to:
  /// **'1 File4Base (Main Workspace)'**
  String get menuWindowMainWorkspace;

  /// No description provided for @menuHelpFile4BaseHelp.
  ///
  /// In en, this message translates to:
  /// **'File4Base Help'**
  String get menuHelpFile4BaseHelp;

  /// No description provided for @menuHelpResourceCenter.
  ///
  /// In en, this message translates to:
  /// **'Resource Center'**
  String get menuHelpResourceCenter;

  /// No description provided for @menuHelpProductDocumentation.
  ///
  /// In en, this message translates to:
  /// **'Product Documentation'**
  String get menuHelpProductDocumentation;

  /// No description provided for @menuHelpCommunity.
  ///
  /// In en, this message translates to:
  /// **'File4Base Community'**
  String get menuHelpCommunity;

  /// No description provided for @menuHelpServiceSupport.
  ///
  /// In en, this message translates to:
  /// **'Service & Support...'**
  String get menuHelpServiceSupport;

  /// No description provided for @menuHelpCheckForUpdates.
  ///
  /// In en, this message translates to:
  /// **'Check for Updates...'**
  String get menuHelpCheckForUpdates;

  /// No description provided for @menuHelpAbout.
  ///
  /// In en, this message translates to:
  /// **'About File4Base'**
  String get menuHelpAbout;

  /// No description provided for @dialogGoToRecordTitle.
  ///
  /// In en, this message translates to:
  /// **'Go to Record'**
  String get dialogGoToRecordTitle;

  /// No description provided for @dialogGoToRecordNumberLabel.
  ///
  /// In en, this message translates to:
  /// **'Record number'**
  String get dialogGoToRecordNumberLabel;

  /// No description provided for @dialogCancel.
  ///
  /// In en, this message translates to:
  /// **'Cancel'**
  String get dialogCancel;

  /// No description provided for @dialogGo.
  ///
  /// In en, this message translates to:
  /// **'Go'**
  String get dialogGo;

  /// No description provided for @dialogClose.
  ///
  /// In en, this message translates to:
  /// **'Close'**
  String get dialogClose;

  /// No description provided for @dialogApply.
  ///
  /// In en, this message translates to:
  /// **'Apply'**
  String get dialogApply;

  /// No description provided for @noticeAuthRequiredTitle.
  ///
  /// In en, this message translates to:
  /// **'Authentication Required'**
  String get noticeAuthRequiredTitle;

  /// No description provided for @noticeAuthSaveSolution.
  ///
  /// In en, this message translates to:
  /// **'Please sign in to save solution changes.'**
  String get noticeAuthSaveSolution;

  /// No description provided for @noticeAuthSaveCopy.
  ///
  /// In en, this message translates to:
  /// **'Please sign in to save a copy.'**
  String get noticeAuthSaveCopy;

  /// No description provided for @noticeAuthExportData.
  ///
  /// In en, this message translates to:
  /// **'Please sign in to export database data.'**
  String get noticeAuthExportData;

  /// No description provided for @noticeAuthImportData.
  ///
  /// In en, this message translates to:
  /// **'Please sign in to import database data.'**
  String get noticeAuthImportData;

  /// No description provided for @noticeAuthManageSchema.
  ///
  /// In en, this message translates to:
  /// **'Please sign in to manage database schema.'**
  String get noticeAuthManageSchema;

  /// No description provided for @noticeAuthChangePassword.
  ///
  /// In en, this message translates to:
  /// **'Please sign in to change password.'**
  String get noticeAuthChangePassword;

  /// No description provided for @noticeAuthImportRecords.
  ///
  /// In en, this message translates to:
  /// **'Please sign in to import records.'**
  String get noticeAuthImportRecords;

  /// No description provided for @noticeAuthExportRecords.
  ///
  /// In en, this message translates to:
  /// **'Please sign in to export records.'**
  String get noticeAuthExportRecords;

  /// No description provided for @noticeAuthDatabaseViews.
  ///
  /// In en, this message translates to:
  /// **'Please sign in to access database views.'**
  String get noticeAuthDatabaseViews;

  /// No description provided for @preferencesTitle.
  ///
  /// In en, this message translates to:
  /// **'Preferences'**
  String get preferencesTitle;

  /// No description provided for @preferencesMenuLanguageSection.
  ///
  /// In en, this message translates to:
  /// **'Menu Language'**
  String get preferencesMenuLanguageSection;

  /// No description provided for @preferencesMenuLanguageDescription.
  ///
  /// In en, this message translates to:
  /// **'Select the language used for all application menus. Changes apply immediately and are remembered across sessions.'**
  String get preferencesMenuLanguageDescription;

  /// No description provided for @preferencesActiveLanguageBadge.
  ///
  /// In en, this message translates to:
  /// **'Active'**
  String get preferencesActiveLanguageBadge;

  /// No description provided for @preferencesResetLanguage.
  ///
  /// In en, this message translates to:
  /// **'Reset to English'**
  String get preferencesResetLanguage;

  /// No description provided for @preferencesServerSection.
  ///
  /// In en, this message translates to:
  /// **'Server Connection'**
  String get preferencesServerSection;

  /// Displays the currently configured server URL in Preferences
  ///
  /// In en, this message translates to:
  /// **'Current server: {url}'**
  String preferencesCurrentServer(String url);

  /// No description provided for @preferencesConfigureServer.
  ///
  /// In en, this message translates to:
  /// **'Server Settings...'**
  String get preferencesConfigureServer;

  /// Confirmation snackbar when menu language is changed
  ///
  /// In en, this message translates to:
  /// **'Menu language updated to {language}.'**
  String preferencesLanguageSaved(String language);

  /// No description provided for @languageNameEn.
  ///
  /// In en, this message translates to:
  /// **'English'**
  String get languageNameEn;

  /// No description provided for @languageNameFr.
  ///
  /// In en, this message translates to:
  /// **'French'**
  String get languageNameFr;

  /// No description provided for @languageNameEs.
  ///
  /// In en, this message translates to:
  /// **'Spanish'**
  String get languageNameEs;

  /// No description provided for @languageNameDe.
  ///
  /// In en, this message translates to:
  /// **'German'**
  String get languageNameDe;

  /// No description provided for @languageNameIt.
  ///
  /// In en, this message translates to:
  /// **'Italian'**
  String get languageNameIt;

  /// No description provided for @languageNameNl.
  ///
  /// In en, this message translates to:
  /// **'Dutch'**
  String get languageNameNl;

  /// No description provided for @languageNamePt.
  ///
  /// In en, this message translates to:
  /// **'Portuguese'**
  String get languageNamePt;
}

class _AppLocalizationsDelegate
    extends LocalizationsDelegate<AppLocalizations> {
  const _AppLocalizationsDelegate();

  @override
  Future<AppLocalizations> load(Locale locale) {
    return SynchronousFuture<AppLocalizations>(lookupAppLocalizations(locale));
  }

  @override
  bool isSupported(Locale locale) => <String>[
    'de',
    'en',
    'es',
    'fr',
    'it',
    'nl',
    'pt',
  ].contains(locale.languageCode);

  @override
  bool shouldReload(_AppLocalizationsDelegate old) => false;
}

AppLocalizations lookupAppLocalizations(Locale locale) {
  // Lookup logic when only language code is specified.
  switch (locale.languageCode) {
    case 'de':
      return AppLocalizationsDe();
    case 'en':
      return AppLocalizationsEn();
    case 'es':
      return AppLocalizationsEs();
    case 'fr':
      return AppLocalizationsFr();
    case 'it':
      return AppLocalizationsIt();
    case 'nl':
      return AppLocalizationsNl();
    case 'pt':
      return AppLocalizationsPt();
  }

  throw FlutterError(
    'AppLocalizations.delegate failed to load unsupported locale "$locale". This is likely '
    'an issue with the localizations generation tool. Please file an issue '
    'on GitHub with a reproducible sample app and the gen-l10n configuration '
    'that was used.',
  );
}
