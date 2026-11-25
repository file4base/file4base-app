import 'package:file4base_client/core/providers/locale_provider.dart';
import 'package:file4base_client/l10n/l10n.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Application Preferences dialog opened from `Edit > Preferences...`.
///
/// Manages the menu/interface language (live-previewed while choosing, rolled
/// back on Cancel, and persisted on Apply) and provides quick access to the
/// server connection settings.
class PreferencesDialog extends ConsumerStatefulWidget {
  const PreferencesDialog({
    super.key,
    this.currentServerUrl,
    this.onOpenServerSettings,
  });

  final String? currentServerUrl;
  final VoidCallback? onOpenServerSettings;

  static Future<void> show(
    BuildContext context, {
    String? currentServerUrl,
    VoidCallback? onOpenServerSettings,
  }) async {
    await showDialog<void>(
      context: context,
      builder: (context) => PreferencesDialog(
        currentServerUrl: currentServerUrl,
        onOpenServerSettings: onOpenServerSettings,
      ),
    );
  }

  @override
  ConsumerState<PreferencesDialog> createState() => _PreferencesDialogState();
}

class _PreferencesDialogState extends ConsumerState<PreferencesDialog> {
  late final AppLocaleNotifier _localeNotifier;
  late final Locale _initialLocale;
  late String _selectedCode;
  bool _committed = false;

  @override
  void initState() {
    super.initState();
    _localeNotifier = ref.read(appLocaleProvider.notifier);
    _initialLocale = ref.read(appLocaleProvider);
    _selectedCode = SupportedAppLanguage.fromCode(_initialLocale.languageCode).code;
  }

  void _rollbackIfUncommitted() {
    if (_committed) return;
    _committed = true;
    _localeNotifier.setLocale(_initialLocale, persist: false);
  }

  void _selectLanguage(String code) {
    setState(() => _selectedCode = code);
    // Live-preview the selected menu language immediately without persisting
    // until the user confirms with Apply.
    _localeNotifier.setLanguageCode(code, persist: false);
  }

  void _applyAndClose() {
    _committed = true;
    _localeNotifier.setLanguageCode(_selectedCode);
    final selectedLang = SupportedAppLanguage.fromCode(_selectedCode);
    final l10n = context.l10n;
    final messenger = ScaffoldMessenger.maybeOf(context);
    Navigator.of(context).pop();
    if (_selectedCode != _initialLocale.languageCode && messenger != null) {
      messenger
        ..hideCurrentSnackBar()
        ..showSnackBar(
          SnackBar(
            content: Text(l10n.preferencesLanguageSaved(selectedLang.nativeName)),
            duration: const Duration(seconds: 2),
            behavior: SnackBarBehavior.floating,
            shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
          ),
        );
    }
  }

  void _cancelAndClose() {
    _rollbackIfUncommitted();
    Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    final l10n = context.l10n;
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;
    const accent = Color(0xFF1E88E5);
    final borderColor = isDark ? const Color(0xFF30363D) : const Color(0xFFE5E7EB);
    final cardBg = isDark ? const Color(0xFF161B22) : const Color(0xFFF9FAFB);

    return PopScope(
      onPopInvokedWithResult: (didPop, _) {
        if (didPop) {
          _rollbackIfUncommitted();
        }
      },
      child: AlertDialog(
        title: Row(
          children: [
            const Icon(Icons.tune, size: 22, color: accent),
            const SizedBox(width: 10),
            Text(
              l10n.preferencesTitle,
              style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 17),
            ),
          ],
        ),
        content: SizedBox(
          width: 480,
          child: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    const Icon(Icons.translate, size: 18, color: accent),
                    const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      l10n.preferencesMenuLanguageSection,
                      style: const TextStyle(
                        fontWeight: FontWeight.w600,
                        fontSize: 14,
                      ),
                    ),
                  ),
                  if (_selectedCode != SupportedAppLanguage.english.code)
                    TextButton(
                      onPressed: () =>
                          _selectLanguage(SupportedAppLanguage.english.code),
                      style: TextButton.styleFrom(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                        minimumSize: Size.zero,
                        tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                      ),
                      child: Text(
                        l10n.preferencesResetLanguage,
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                ],
              ),
              const SizedBox(height: 6),
              Text(
                l10n.preferencesMenuLanguageDescription,
                style: TextStyle(
                  fontSize: 12,
                  color: isDark ? Colors.grey.shade400 : Colors.grey.shade700,
                ),
              ),
              const SizedBox(height: 12),
              DecoratedBox(
                decoration: BoxDecoration(
                  color: cardBg,
                  borderRadius: BorderRadius.circular(8),
                  border: Border.all(color: borderColor),
                ),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    for (var i = 0; i < SupportedAppLanguage.all.length; i++) ...[
                      if (i > 0) Divider(height: 1, color: borderColor),
                      _buildLanguageTile(
                        context,
                        SupportedAppLanguage.all[i],
                        isSelected: _selectedCode == SupportedAppLanguage.all[i].code,
                        accent: accent,
                        isDark: isDark,
                      ),
                    ],
                  ],
                ),
              ),
              if (widget.currentServerUrl != null ||
                  widget.onOpenServerSettings != null) ...[
                const SizedBox(height: 18),
                Divider(height: 1, color: borderColor),
                const SizedBox(height: 14),
                Row(
                  children: [
                    const Icon(Icons.dns_outlined, size: 18, color: accent),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            l10n.preferencesServerSection,
                            style: const TextStyle(
                              fontWeight: FontWeight.w600,
                              fontSize: 13,
                            ),
                          ),
                          if (widget.currentServerUrl != null) ...[
                            const SizedBox(height: 2),
                            Text(
                              l10n.preferencesCurrentServer(widget.currentServerUrl!),
                              style: TextStyle(
                                fontSize: 11.5,
                                color: isDark
                                    ? Colors.grey.shade400
                                    : Colors.grey.shade700,
                              ),
                              overflow: TextOverflow.ellipsis,
                            ),
                          ],
                        ],
                      ),
                    ),
                    if (widget.onOpenServerSettings != null)
                      OutlinedButton.icon(
                        onPressed: () {
                          _applyAndClose();
                          widget.onOpenServerSettings!();
                        },
                        icon: const Icon(Icons.settings_ethernet, size: 15),
                        label: Text(
                          l10n.preferencesConfigureServer,
                          style: const TextStyle(fontSize: 12),
                        ),
                      ),
                  ],
                ),
              ],
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: _cancelAndClose,
          child: Text(l10n.dialogCancel),
        ),
        FilledButton(
          onPressed: _applyAndClose,
          child: Text(l10n.dialogApply),
        ),
      ],
      ),
    );
  }

  Widget _buildLanguageTile(
    BuildContext context,
    SupportedAppLanguage language, {
    required bool isSelected,
    required Color accent,
    required bool isDark,
  }) {
    final l10n = context.l10n;
    final localizedName = language.localizedName(l10n);
    final showSubtitle =
        localizedName.toLowerCase() != language.nativeName.toLowerCase();

    return InkWell(
      key: ValueKey('preferences_lang_${language.code}'),
      onTap: () => _selectLanguage(language.code),
      borderRadius: BorderRadius.circular(6),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
        color: isSelected ? accent.withValues(alpha: isDark ? 0.16 : 0.08) : null,
        child: Row(
          children: [
            Container(
              width: 30,
              height: 22,
              alignment: Alignment.center,
              decoration: BoxDecoration(
                color: isSelected
                    ? accent
                    : (isDark ? const Color(0xFF21262D) : const Color(0xFFE5E7EB)),
                borderRadius: BorderRadius.circular(4),
              ),
              child: Text(
                language.badge,
                style: TextStyle(
                  fontSize: 10.5,
                  fontWeight: FontWeight.bold,
                  color: isSelected
                      ? Colors.white
                      : (isDark ? Colors.grey.shade300 : Colors.grey.shade800),
                ),
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    language.nativeName,
                    style: TextStyle(
                      fontSize: 13.5,
                      fontWeight: isSelected ? FontWeight.w600 : FontWeight.w500,
                    ),
                  ),
                  if (showSubtitle)
                    Text(
                      localizedName,
                      style: TextStyle(
                        fontSize: 11,
                        color: isDark ? Colors.grey.shade400 : Colors.grey.shade600,
                      ),
                    ),
                ],
              ),
            ),
            if (isSelected) ...[
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                decoration: BoxDecoration(
                  color: accent.withValues(alpha: 0.15),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Text(
                  l10n.preferencesActiveLanguageBadge,
                  style: TextStyle(
                    fontSize: 10.5,
                    fontWeight: FontWeight.w600,
                    color: accent,
                  ),
                ),
              ),
              const SizedBox(width: 8),
              Icon(Icons.check_circle, size: 18, color: accent),
            ] else
              Icon(
                Icons.radio_button_unchecked,
                size: 18,
                color: isDark ? Colors.grey.shade600 : Colors.grey.shade400,
              ),
          ],
        ),
      ),
    );
  }
}
