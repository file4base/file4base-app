/// FileOptionsModel defines startup credentials, initial layout, toolbar visibility,
/// Kiosk Mode settings, and script triggers (OnFirstWindowOpen, OnLastWindowClose, etc.)
/// for File4Base File Options and Developer Utilities.
class FileOptionsModel {

  const FileOptionsModel({
    this.autoLoginEnabled = false,
    this.isGuestLogin = false,
    this.defaultUsername = 'admin',
    this.defaultPassword = '',
    this.switchLayoutOnOpen = false,
    this.startupLayoutId = '',
    this.startupLayoutName = '',
    this.hideAllToolbars = false,
    this.kioskMode = false,
    this.kioskPrimaryLayoutId = '',
    this.kioskPrimaryLayoutName = '',
    this.kioskRequireLogin = true,
    this.kioskPreviewTimeoutSeconds = 10,
    this.encryptionEnabled = true,
    this.encryptionCipher = 'AES-256-GCM',
    this.encryptionHint = '',
    this.onFirstWindowOpenEnabled = false,
    this.onFirstWindowOpenScript = '',
    this.onFirstWindowOpenParam = '',
    this.onLastWindowCloseEnabled = false,
    this.onLastWindowCloseScript = '',
    this.onLastWindowCloseParam = '',
    this.onWindowOpenEnabled = false,
    this.onWindowOpenScript = '',
    this.onWindowOpenParam = '',
    this.onWindowCloseEnabled = false,
    this.onWindowCloseScript = '',
    this.onWindowCloseParam = '',
    this.onFileAVPlayerChangeEnabled = false,
    this.onFileAVPlayerChangeScript = '',
    this.onFileAVPlayerChangeParam = '',
  });

  factory FileOptionsModel.fromJson(Map<String, dynamic> json) {
    return FileOptionsModel(
      autoLoginEnabled: json['auto_login_enabled'] as bool? ?? false,
      isGuestLogin: json['is_guest_login'] as bool? ?? false,
      defaultUsername: json['default_username']?.toString() ?? 'admin',
      // A remembered password is never read from a file (#10).
      switchLayoutOnOpen: json['switch_layout_on_open'] as bool? ?? false,
      startupLayoutId: json['startup_layout_id']?.toString() ?? '',
      startupLayoutName: json['startup_layout_name']?.toString() ?? '',
      hideAllToolbars: json['hide_all_toolbars'] as bool? ?? false,
      kioskMode: json['kiosk_mode'] as bool? ?? false,
      kioskPrimaryLayoutId: json['kiosk_primary_layout_id']?.toString() ?? '',
      kioskPrimaryLayoutName: json['kiosk_primary_layout_name']?.toString() ?? '',
      kioskRequireLogin: json['kiosk_require_login'] as bool? ?? true,
      kioskPreviewTimeoutSeconds:
          (json['kiosk_preview_timeout_seconds'] as num?)?.toInt() ?? 10,
      encryptionEnabled: json['encryption_enabled'] as bool? ?? true,
      encryptionCipher: json['encryption_cipher']?.toString() ?? 'AES-256-GCM',
      encryptionHint: json['encryption_hint']?.toString() ?? '',
      onFirstWindowOpenEnabled: json['on_first_window_open_enabled'] as bool? ?? false,
      onFirstWindowOpenScript: json['on_first_window_open_script']?.toString() ?? '',
      onFirstWindowOpenParam: json['on_first_window_open_param']?.toString() ?? '',
      onLastWindowCloseEnabled: json['on_last_window_close_enabled'] as bool? ?? false,
      onLastWindowCloseScript: json['on_last_window_close_script']?.toString() ?? '',
      onLastWindowCloseParam: json['on_last_window_close_param']?.toString() ?? '',
      onWindowOpenEnabled: json['on_window_open_enabled'] as bool? ?? false,
      onWindowOpenScript: json['on_window_open_script']?.toString() ?? '',
      onWindowOpenParam: json['on_window_open_param']?.toString() ?? '',
      onWindowCloseEnabled: json['on_window_close_enabled'] as bool? ?? false,
      onWindowCloseScript: json['on_window_close_script']?.toString() ?? '',
      onWindowCloseParam: json['on_window_close_param']?.toString() ?? '',
      onFileAVPlayerChangeEnabled: json['on_file_av_player_change_enabled'] as bool? ?? false,
      onFileAVPlayerChangeScript: json['on_file_av_player_change_script']?.toString() ?? '',
      onFileAVPlayerChangeParam: json['on_file_av_player_change_param']?.toString() ?? '',
    );
  }
  // --- Open tab ---
  final bool autoLoginEnabled;
  final bool isGuestLogin;
  final String defaultUsername;
  /// Kept in memory for the session only: never written to a solution file.
  final String defaultPassword;
  final bool switchLayoutOnOpen;
  final String startupLayoutId;
  final String startupLayoutName;
  final bool hideAllToolbars;

  // --- Kiosk Mode & Solution Protection (Developer Utilities) ---
  final bool kioskMode;
  final String kioskPrimaryLayoutId;
  final String kioskPrimaryLayoutName;
  final bool kioskRequireLogin;
  final int kioskPreviewTimeoutSeconds;
  final bool encryptionEnabled;
  final String encryptionCipher;
  final String encryptionHint;

  // --- Script Triggers tab ---
  final bool onFirstWindowOpenEnabled;
  final String onFirstWindowOpenScript;
  final String onFirstWindowOpenParam;

  final bool onLastWindowCloseEnabled;
  final String onLastWindowCloseScript;
  final String onLastWindowCloseParam;

  final bool onWindowOpenEnabled;
  final String onWindowOpenScript;
  final String onWindowOpenParam;

  final bool onWindowCloseEnabled;
  final String onWindowCloseScript;
  final String onWindowCloseParam;

  final bool onFileAVPlayerChangeEnabled;
  final String onFileAVPlayerChangeScript;
  final String onFileAVPlayerChangeParam;

  FileOptionsModel copyWith({
    bool? autoLoginEnabled,
    bool? isGuestLogin,
    String? defaultUsername,
    String? defaultPassword,
    bool? switchLayoutOnOpen,
    String? startupLayoutId,
    String? startupLayoutName,
    bool? hideAllToolbars,
    bool? kioskMode,
    String? kioskPrimaryLayoutId,
    String? kioskPrimaryLayoutName,
    bool? kioskRequireLogin,
    int? kioskPreviewTimeoutSeconds,
    bool? encryptionEnabled,
    String? encryptionCipher,
    String? encryptionHint,
    bool? onFirstWindowOpenEnabled,
    String? onFirstWindowOpenScript,
    String? onFirstWindowOpenParam,
    bool? onLastWindowCloseEnabled,
    String? onLastWindowCloseScript,
    String? onLastWindowCloseParam,
    bool? onWindowOpenEnabled,
    String? onWindowOpenScript,
    String? onWindowOpenParam,
    bool? onWindowCloseEnabled,
    String? onWindowCloseScript,
    String? onWindowCloseParam,
    bool? onFileAVPlayerChangeEnabled,
    String? onFileAVPlayerChangeScript,
    String? onFileAVPlayerChangeParam,
  }) {
    return FileOptionsModel(
      autoLoginEnabled: autoLoginEnabled ?? this.autoLoginEnabled,
      isGuestLogin: isGuestLogin ?? this.isGuestLogin,
      defaultUsername: defaultUsername ?? this.defaultUsername,
      defaultPassword: defaultPassword ?? this.defaultPassword,
      switchLayoutOnOpen: switchLayoutOnOpen ?? this.switchLayoutOnOpen,
      startupLayoutId: startupLayoutId ?? this.startupLayoutId,
      startupLayoutName: startupLayoutName ?? this.startupLayoutName,
      hideAllToolbars: hideAllToolbars ?? this.hideAllToolbars,
      kioskMode: kioskMode ?? this.kioskMode,
      kioskPrimaryLayoutId: kioskPrimaryLayoutId ?? this.kioskPrimaryLayoutId,
      kioskPrimaryLayoutName: kioskPrimaryLayoutName ?? this.kioskPrimaryLayoutName,
      kioskRequireLogin: kioskRequireLogin ?? this.kioskRequireLogin,
      kioskPreviewTimeoutSeconds:
          kioskPreviewTimeoutSeconds ?? this.kioskPreviewTimeoutSeconds,
      encryptionEnabled: encryptionEnabled ?? this.encryptionEnabled,
      encryptionCipher: encryptionCipher ?? this.encryptionCipher,
      encryptionHint: encryptionHint ?? this.encryptionHint,
      onFirstWindowOpenEnabled: onFirstWindowOpenEnabled ?? this.onFirstWindowOpenEnabled,
      onFirstWindowOpenScript: onFirstWindowOpenScript ?? this.onFirstWindowOpenScript,
      onFirstWindowOpenParam: onFirstWindowOpenParam ?? this.onFirstWindowOpenParam,
      onLastWindowCloseEnabled: onLastWindowCloseEnabled ?? this.onLastWindowCloseEnabled,
      onLastWindowCloseScript: onLastWindowCloseScript ?? this.onLastWindowCloseScript,
      onLastWindowCloseParam: onLastWindowCloseParam ?? this.onLastWindowCloseParam,
      onWindowOpenEnabled: onWindowOpenEnabled ?? this.onWindowOpenEnabled,
      onWindowOpenScript: onWindowOpenScript ?? this.onWindowOpenScript,
      onWindowOpenParam: onWindowOpenParam ?? this.onWindowOpenParam,
      onWindowCloseEnabled: onWindowCloseEnabled ?? this.onWindowCloseEnabled,
      onWindowCloseScript: onWindowCloseScript ?? this.onWindowCloseScript,
      onWindowCloseParam: onWindowCloseParam ?? this.onWindowCloseParam,
      onFileAVPlayerChangeEnabled: onFileAVPlayerChangeEnabled ?? this.onFileAVPlayerChangeEnabled,
      onFileAVPlayerChangeScript: onFileAVPlayerChangeScript ?? this.onFileAVPlayerChangeScript,
      onFileAVPlayerChangeParam: onFileAVPlayerChangeParam ?? this.onFileAVPlayerChangeParam,
    );
  }

  Map<String, dynamic> toJson() => {
    'auto_login_enabled': autoLoginEnabled,
    'is_guest_login': isGuestLogin,
    'default_username': defaultUsername,
    'switch_layout_on_open': switchLayoutOnOpen,
    'startup_layout_id': startupLayoutId,
    'startup_layout_name': startupLayoutName,
    'hide_all_toolbars': hideAllToolbars,
    'kiosk_mode': kioskMode,
    'kiosk_primary_layout_id': kioskPrimaryLayoutId,
    'kiosk_primary_layout_name': kioskPrimaryLayoutName,
    'kiosk_require_login': kioskRequireLogin,
    'kiosk_preview_timeout_seconds': kioskPreviewTimeoutSeconds,
    'encryption_enabled': encryptionEnabled,
    'encryption_cipher': encryptionCipher,
    if (encryptionHint.isNotEmpty) 'encryption_hint': encryptionHint,
    'on_first_window_open_enabled': onFirstWindowOpenEnabled,
    'on_first_window_open_script': onFirstWindowOpenScript,
    'on_first_window_open_param': onFirstWindowOpenParam,
    'on_last_window_close_enabled': onLastWindowCloseEnabled,
    'on_last_window_close_script': onLastWindowCloseScript,
    'on_last_window_close_param': onLastWindowCloseParam,
    'on_window_open_enabled': onWindowOpenEnabled,
    'on_window_open_script': onWindowOpenScript,
    'on_window_open_param': onWindowOpenParam,
    'on_window_close_enabled': onWindowCloseEnabled,
    'on_window_close_script': onWindowCloseScript,
    'on_window_close_param': onWindowCloseParam,
    'on_file_av_player_change_enabled': onFileAVPlayerChangeEnabled,
    'on_file_av_player_change_script': onFileAVPlayerChangeScript,
    'on_file_av_player_change_param': onFileAVPlayerChangeParam,
  };
}
