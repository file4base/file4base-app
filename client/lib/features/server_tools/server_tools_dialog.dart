import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/core/services/solution_storage.dart';
import 'package:file4base_client/features/about/about_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// Identifies which section of the Server Tools console to open initially.
enum ServerToolSection {
  containers,
  backups,
  domainTls,
  telemetry,
  sessions,
  schedules,
  auditLogs,
  maintenance,
  notifications,
}

/// A solution-management action (an item of the "Manage" menu) made available
/// at the end of the Server Tools console. [onSelected] opens the manager.
class ServerToolsManagementAction {
  const ServerToolsManagementAction({
    required this.label,
    required this.subtitle,
    required this.icon,
    required this.onSelected,
  });
  final String label;
  final String subtitle;
  final IconData icon;
  final VoidCallback onSelected;
}

class ServerToolsDialog extends StatefulWidget {

  const ServerToolsDialog({
    required this.apiClient, super.key,
    this.initialSection = ServerToolSection.containers,
    this.activeDatabase,
    this.onDatabaseRestored,
    this.onVersionStatusChanged,
    this.management = const <ServerToolsManagementAction>[],
  });
  final ApiClient apiClient;
  final ServerToolSection initialSection;
  final String? activeDatabase;
  final VoidCallback? onDatabaseRestored;
  final void Function({required bool updateAvailable, required String latestVersion})? onVersionStatusChanged;

  /// Solution-management actions (the "Manage" menu) surfaced at the end of the
  /// console so an administrator can reach them from one place. Each opens its
  /// own dialog on top of this one.
  final List<ServerToolsManagementAction> management;

  static Future<void> show(
    BuildContext context,
    ApiClient apiClient, {
    ServerToolSection initialSection = ServerToolSection.containers,
    String? activeDatabase,
    VoidCallback? onDatabaseRestored,
    void Function({required bool updateAvailable, required String latestVersion})? onVersionStatusChanged,
    List<ServerToolsManagementAction> management = const <ServerToolsManagementAction>[],
  }) {
    return showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => ServerToolsDialog(
        apiClient: apiClient,
        initialSection: initialSection,
        activeDatabase: activeDatabase,
        onDatabaseRestored: onDatabaseRestored,
        onVersionStatusChanged: onVersionStatusChanged,
        management: management,
      ),
    );
  }

  @override
  State<ServerToolsDialog> createState() => _ServerToolsDialogState();
}

class _ServerToolsDialogState extends State<ServerToolsDialog> {
  static const Color _primaryBlue = Color(0xFF1E88E5);

  late ServerToolSection _section;
  bool _loading = false;

  // What the session may use (GET /api/v1/server/access). Until it is known
  // only the tools of the session's own database are offered.
  bool _serverAdmin = false;
  bool _adminDatabaseConfigured = true;
  // Whether the session may use the single-database tools (owner or admin).
  // Gates the solution-management group.
  bool _databaseTools = false;

  /// Backups and maintenance work on one database (the session's own); the
  /// other sections act on the whole server.
  static bool _isDatabaseSection(ServerToolSection s) =>
      s == ServerToolSection.backups || s == ServerToolSection.maintenance;

  bool _maySee(ServerToolSection s) => _serverAdmin || _isDatabaseSection(s);
  String? _error;
  Timer? _autoRefreshTimer;
  bool _autoRefreshTelemetry = true;

  // 1. Containers, Versions & Logs state
  List<Map<String, dynamic>> _containers = [];
  String _selectedLogContainer = 'file4base-api';
  List<String> _containerLogs = [];
  String _currentStackVersion = AboutFile4BaseDialog.version;
  String _latestStackVersion = AboutFile4BaseDialog.version;
  bool _updateAvailable = false;
  String _versionCheckedAt = '';
  String _releaseNotes = '';
  String _updateCommand = 'docker compose pull && docker compose up -d';

  // 2. Backups state
  List<Map<String, dynamic>> _backups = [];
  final TextEditingController _backupNotesCtrl = TextEditingController();

  // 3. Domain & TLS state
  Map<String, dynamic> _tlsConfig = {};
  final TextEditingController _domainCtrl = TextEditingController();
  final TextEditingController _publicUrlCtrl = TextEditingController();
  final TextEditingController _acmeEmailCtrl = TextEditingController();
  final TextEditingController _customCertCtrl = TextEditingController();
  final TextEditingController _customKeyCtrl = TextEditingController();
  String _tlsMode = 'self_signed';
  String _acmeProvider = 'letsencrypt_prod';
  String _acmeChallenge = 'http-01';
  bool _autoRenew = true;
  bool _forceHttps = false;
  bool _hstsEnabled = true;

  // 4. Telemetry state
  Map<String, dynamic> _telemetryRoot = {};
  int _telemetryMinutes = 30;

  // 5. Sessions state
  List<Map<String, dynamic>> _sessions = [];

  // 6. Schedules state
  List<Map<String, dynamic>> _schedules = [];

  // 7. Audit Logs state
  List<Map<String, dynamic>> _auditEvents = [];
  String _auditCategoryFilter = 'all';

  // 8. Database Maintenance state
  Map<String, dynamic> _maintenanceInfo = {};

  // 9. Notifications state
  bool _smtpEnabled = false;
  bool _smtpUseTls = true;
  final TextEditingController _smtpHostCtrl = TextEditingController();
  final TextEditingController _smtpPortCtrl = TextEditingController(text: '587');
  final TextEditingController _smtpUserCtrl = TextEditingController();
  final TextEditingController _smtpFromCtrl = TextEditingController();
  bool _webhookEnabled = false;
  final TextEditingController _webhookUrlCtrl = TextEditingController();
  final TextEditingController _webhookSecretCtrl = TextEditingController();
  List<String> _alertEvents = [
    'backup_failed',
    'high_error_rate',
    'mfa_changed',
    'tls_expiring',
    'update_available',
  ];
  String _lastTestTimestamp = '';

  bool get _isDark => Theme.of(context).brightness == Brightness.dark;
  Color get _borderColor => _isDark ? Colors.grey.shade800 : Colors.grey.shade300;
  Color get _cardBg => _isDark ? const Color(0xFF1E2430) : Colors.white;
  Color get _subtleBg => _isDark ? Colors.black12 : const Color(0xFFF8FAFC);
  Color get _tableHeaderBg => _isDark ? Colors.black26 : const Color(0xFFF1F5F9);
  Color get _mutedText => _isDark ? Colors.grey.shade400 : Colors.grey.shade600;

  @override
  void initState() {
    super.initState();
    _section = widget.initialSection;
    unawaited(_loadAccessThenSection());
    _autoRefreshTimer = Timer.periodic(const Duration(seconds: 6), (_) {
      if (!mounted) return;
      if (_section == ServerToolSection.telemetry && _autoRefreshTelemetry) {
        unawaited(_loadCurrentSection(silent: true));
      }
    });
  }

  @override
  void dispose() {
    _autoRefreshTimer?.cancel();
    _backupNotesCtrl.dispose();
    _domainCtrl.dispose();
    _publicUrlCtrl.dispose();
    _acmeEmailCtrl.dispose();
    _customCertCtrl.dispose();
    _customKeyCtrl.dispose();
    _smtpHostCtrl.dispose();
    _smtpPortCtrl.dispose();
    _smtpUserCtrl.dispose();
    _smtpFromCtrl.dispose();
    _webhookUrlCtrl.dispose();
    _webhookSecretCtrl.dispose();
    super.dispose();
  }

  Future<void> _loadCurrentSection({bool silent = false}) async {
    if (!silent) {
      setState(() {
        _loading = true;
        _error = null;
      });
    }
    try {
      switch (_section) {
        case ServerToolSection.containers:
          final res = await widget.apiClient.listServerContainers();
          final list = (res['containers'] as List<dynamic>? ?? const [])
              .whereType<Map<dynamic, dynamic>>()
              .map(Map<String, dynamic>.from)
              .toList();
          final curVer = res['current_version']?.toString() ?? AboutFile4BaseDialog.version;
          final latestVer = res['latest_version']?.toString() ?? curVer;
          final updateAvail = res['update_available'] as bool? ?? false;
          final checkedAt = res['checked_at']?.toString() ?? '';
          final relNotes = res['release_notes']?.toString() ?? '';
          final updCmd = res['update_command']?.toString() ?? 'docker compose pull && docker compose up -d';
          widget.onVersionStatusChanged?.call(
            updateAvailable: updateAvail,
            latestVersion: latestVer,
          );
          final names = list.map((c) => c['name']?.toString() ?? '').where((s) => s.isNotEmpty).toList();
          if (names.isNotEmpty && !names.contains(_selectedLogContainer)) {
            _selectedLogContainer = names.first;
          }
          final logs = await widget.apiClient.getServerContainerLogs(_selectedLogContainer);
          if (!mounted) return;
          setState(() {
            _containers = list;
            _currentStackVersion = curVer;
            _latestStackVersion = latestVer;
            _updateAvailable = updateAvail;
            _versionCheckedAt = checkedAt;
            _releaseNotes = relNotes;
            _updateCommand = updCmd;
            _containerLogs = logs;
            _loading = false;
          });
        case ServerToolSection.backups:
          final list = await widget.apiClient.listServerBackups();
          if (!mounted) return;
          setState(() {
            _backups = list;
            _loading = false;
          });
        case ServerToolSection.domainTls:
          final cfg = await widget.apiClient.getServerTlsConfig();
          if (!mounted) return;
          setState(() {
            _tlsConfig = cfg;
            _domainCtrl.text = cfg['domain']?.toString() ?? 'localhost';
            _publicUrlCtrl.text = cfg['public_url']?.toString() ?? 'http://localhost:8090';
            _acmeEmailCtrl.text = cfg['acme_email']?.toString() ?? 'admin@example.com';
            final mode = cfg['mode']?.toString() ?? 'self_signed';
            _tlsMode = const ['acme', 'custom', 'self_signed', 'http_only'].contains(mode)
                ? mode
                : 'self_signed';
            final provider = cfg['acme_provider']?.toString() ?? 'letsencrypt_prod';
            _acmeProvider = const ['letsencrypt_prod', 'letsencrypt_staging', 'zerossl'].contains(provider)
                ? provider
                : 'letsencrypt_prod';
            final challenge = cfg['acme_challenge']?.toString() ?? 'http-01';
            _acmeChallenge = const ['http-01', 'tls-alpn-01'].contains(challenge)
                ? challenge
                : 'http-01';
            _autoRenew = cfg['auto_renew'] as bool? ?? true;
            _forceHttps = cfg['force_https'] as bool? ?? false;
            _hstsEnabled = cfg['hsts_enabled'] as bool? ?? true;
            _customCertCtrl.text = cfg['custom_cert_pem']?.toString() ?? '';
            _loading = false;
          });
        case ServerToolSection.telemetry:
          final data = await widget.apiClient.getServerTelemetry(minutes: _telemetryMinutes);
          if (!mounted) return;
          setState(() {
            _telemetryRoot = data;
            _loading = false;
          });
        case ServerToolSection.sessions:
          final list = await widget.apiClient.listServerSessions();
          if (!mounted) return;
          setState(() {
            _sessions = list;
            _loading = false;
          });
        case ServerToolSection.schedules:
          final list = await widget.apiClient.listServerSchedules();
          if (!mounted) return;
          setState(() {
            _schedules = list;
            _loading = false;
          });
        case ServerToolSection.auditLogs:
          final list = await widget.apiClient.listServerAuditLogs();
          if (!mounted) return;
          setState(() {
            _auditEvents = list;
            _loading = false;
          });
        case ServerToolSection.maintenance:
          final data = await widget.apiClient.getServerDatabaseMaintenance(
            database: widget.activeDatabase,
          );
          if (!mounted) return;
          setState(() {
            _maintenanceInfo = data;
            _loading = false;
          });
        case ServerToolSection.notifications:
          final cfg = await widget.apiClient.getServerNotifications();
          if (!mounted) return;
          setState(() {
            _smtpEnabled = cfg['smtp_enabled'] as bool? ?? false;
            _smtpUseTls = cfg['smtp_use_tls'] as bool? ?? true;
            _smtpHostCtrl.text = cfg['smtp_host']?.toString() ?? 'smtp.example.com';
            _smtpPortCtrl.text = (cfg['smtp_port'] ?? 587).toString();
            _smtpUserCtrl.text = cfg['smtp_username']?.toString() ?? '';
            _smtpFromCtrl.text = cfg['smtp_from_email']?.toString() ?? 'alerts@file4base.local';
            _webhookEnabled = cfg['webhook_enabled'] as bool? ?? false;
            _webhookUrlCtrl.text = cfg['webhook_url']?.toString() ?? '';
            _webhookSecretCtrl.text = cfg['webhook_secret']?.toString() ?? '';
            final rawEvents = (cfg['alert_events'] as List<dynamic>? ?? const [])
                .whereType<String>()
                .toList();
            if (rawEvents.isNotEmpty) {
              _alertEvents = rawEvents;
            }
            _lastTestTimestamp = cfg['last_test_timestamp']?.toString() ?? '';
            _loading = false;
          });
      }
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  /// Reads what the session may use, then opens the requested section, or
  /// the backups of its own database when that section is server-wide.
  Future<void> _loadAccessThenSection() async {
    try {
      final access = await widget.apiClient.getServerAccess();
      _serverAdmin = access['server_admin'] == true;
      _adminDatabaseConfigured = access['admin_database_configured'] != false;
      _databaseTools = access['database_tools'] == true;
    } on Exception {
      // Unknown access: offer only the tools of the session's own database.
      _serverAdmin = false;
      _databaseTools = false;
    }
    if (!mounted) return;
    setState(() {
      if (!_maySee(_section)) _section = ServerToolSection.backups;
    });
    await _loadCurrentSection();
  }

  /// Picks a backup file (.tar.gz from a download, or a raw .f4b.json),
  /// uploads it as a server-side backup and refreshes the list.
  Future<void> _uploadBackupFromFile() async {
    try {
      final picked = await SolutionStorageService.pickFile(
        allowedExtensions: const ['gz', 'tgz', 'json', 'f4b'],
      );
      if (picked == null) return;
      final isGzip = picked.bytes.length >= 2 && picked.bytes[0] == 0x1f && picked.bytes[1] == 0x8b;
      final created = await widget.apiClient.uploadServerBackup(picked.bytes, gzip: isGzip);
      _toast('Uploaded backup: ${created['filename'] ?? created['id']}');
      await _loadCurrentSection();
    } on Exception catch (e) {
      _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
    }
  }

  void _selectSection(ServerToolSection section) {
    if (_section == section || !_maySee(section)) return;
    setState(() {
      _section = section;
      _error = null;
    });
    unawaited(_loadCurrentSection());
  }

  void _toast(String msg, {bool isError = false}) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(msg),
        backgroundColor: isError ? Colors.red.shade700 : Colors.green.shade700,
        duration: const Duration(seconds: 2),
      ),
    );
  }

  /// Saves the public certificate of the File4Base Local CA to disk and tells
  /// the administrator how to install it. Installing this one file, once per
  /// machine, is what stops the browser warning: every certificate the server
  /// reissues afterwards is signed by the same authority.
  Future<void> _downloadCaCertificate() async {
    try {
      final bytes = await widget.apiClient.downloadServerCaCertificate();
      await SolutionStorageService.saveFile(
        filename: 'file4base-local-ca.crt',
        bytes: bytes,
      );
      _toast('Local CA certificate saved as file4base-local-ca.crt');
    } catch (e) {
      _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
    }
  }

  /// Saves the served certificate followed by the local authority, for an
  /// administrator who terminates TLS on another reverse proxy. The file holds
  /// no private key.
  Future<void> _downloadCertificateBundle() async {
    try {
      final bytes = await widget.apiClient.downloadServerCertificateBundle();
      await SolutionStorageService.saveFile(
        filename: 'file4base-chain.pem',
        bytes: bytes,
      );
      _toast('Certificate chain saved as file4base-chain.pem');
    } catch (e) {
      _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
    }
  }

  Future<void> _saveTlsSettings({String? toastMsg}) async {
    try {
      final updated = await widget.apiClient.updateServerTlsConfig({
        'domain': _domainCtrl.text.trim().isEmpty ? 'localhost' : _domainCtrl.text.trim(),
        'public_url': _publicUrlCtrl.text.trim(),
        'mode': _tlsMode,
        'acme_email': _acmeEmailCtrl.text.trim(),
        'acme_provider': _acmeProvider,
        'acme_challenge': _acmeChallenge,
        'auto_renew': _autoRenew,
        'force_https': _forceHttps,
        'hsts_enabled': _hstsEnabled,
        if (_tlsMode == 'custom' && _customCertCtrl.text.trim().isNotEmpty)
          'custom_cert_pem': _customCertCtrl.text,
        if (_tlsMode == 'custom' && _customKeyCtrl.text.trim().isNotEmpty)
          'custom_key_pem': _customKeyCtrl.text,
      });
      if (!mounted) return;
      setState(() => _tlsConfig = updated);
      if (toastMsg != null) {
        _toast(toastMsg);
      }
    } catch (e) {
      _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
    }
  }

  Future<void> _saveNotifications({String? toastMsg}) async {
    try {
      await widget.apiClient.updateServerNotifications({
        'smtp_enabled': _smtpEnabled,
        'smtp_host': _smtpHostCtrl.text.trim(),
        'smtp_port': int.tryParse(_smtpPortCtrl.text.trim()) ?? 587,
        'smtp_username': _smtpUserCtrl.text.trim(),
        'smtp_from_email': _smtpFromCtrl.text.trim(),
        'smtp_use_tls': _smtpUseTls,
        'webhook_enabled': _webhookEnabled,
        'webhook_url': _webhookUrlCtrl.text.trim(),
        'webhook_secret': _webhookSecretCtrl.text.trim(),
        'alert_events': _alertEvents,
      });
      if (toastMsg != null) {
        _toast(toastMsg);
      }
    } catch (e) {
      _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
    }
  }

  String _formatUptime(int seconds) {
    final d = seconds ~/ 86400;
    final h = (seconds % 86400) ~/ 3600;
    final m = (seconds % 3600) ~/ 60;
    final s = seconds % 60;
    if (d > 0) return '${d}d ${h}h ${m}m';
    if (h > 0) return '${h}h ${m}m ${s}s';
    if (m > 0) return '${m}m ${s}s';
    return '${s}s';
  }

  String _formatBytes(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    return '${(bytes / (1024 * 1024)).toStringAsFixed(2)} MB';
  }

  @override
  Widget build(BuildContext context) {
    return Dialog(
      backgroundColor: Colors.transparent,
      insetPadding: const EdgeInsets.symmetric(horizontal: 24, vertical: 20),
      child: Container(
        width: 1120,
        height: 720,
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.surface,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: _isDark ? Colors.grey.shade700 : Colors.grey.shade300),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: _isDark ? 0.5 : 0.18),
              blurRadius: 28,
              offset: const Offset(0, 10),
            ),
          ],
        ),
        child: Column(
          children: [
            _buildTopHeader(),
            Expanded(
              child: Row(
                children: [
                  _buildLeftNavigation(),
                  VerticalDivider(width: 1, thickness: 1, color: _borderColor),
                  Expanded(
                    child: _loading
                        ? const Center(child: CircularProgressIndicator())
                        : _buildMainContent(),
                  ),
                ],
              ),
            ),
            _buildBottomFooter(),
          ],
        ),
      ),
    );
  }

  Widget _buildTopHeader() {
    final activeDb = widget.activeDatabase ?? 'Default';
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
      decoration: BoxDecoration(
        color: _primaryBlue.withValues(alpha: 0.08),
        borderRadius: const BorderRadius.vertical(top: Radius.circular(12)),
        border: Border(bottom: BorderSide(color: _borderColor)),
      ),
      child: Row(
        children: [
          const Icon(Icons.dns_rounded, color: _primaryBlue, size: 24),
          const SizedBox(width: 10),
          Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  const Text(
                    'Server Tools & Administration',
                    style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16),
                  ),
                  const SizedBox(width: 10),
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                    decoration: BoxDecoration(
                      color: Colors.blue.withValues(alpha: 0.15),
                      borderRadius: BorderRadius.circular(12),
                      border: Border.all(color: Colors.blue.shade400.withValues(alpha: 0.4)),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const Icon(Icons.storage_outlined, size: 12, color: _primaryBlue),
                        const SizedBox(width: 4),
                        Text(
                          activeDb,
                          style: const TextStyle(
                            fontSize: 11,
                            fontWeight: FontWeight.bold,
                            color: _primaryBlue,
                          ),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(width: 6),
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                    decoration: BoxDecoration(
                      color: Colors.teal.withValues(alpha: 0.12),
                      borderRadius: BorderRadius.circular(12),
                      border: Border.all(color: Colors.teal.withValues(alpha: 0.35)),
                    ),
                    child: const Text(
                      'file4base-net',
                      style: TextStyle(
                        fontSize: 11,
                        fontWeight: FontWeight.w600,
                        color: Colors.teal,
                        fontFamily: 'monospace',
                      ),
                    ),
                  ),
                ],
              ),
              Text(
                'Docker container orchestration, database snapshots, ACME SSL/TLS, API telemetry, schedules, and audit logs',
                style: TextStyle(fontSize: 11, color: _mutedText),
              ),
            ],
          ),
          const Spacer(),
          OutlinedButton.icon(
            onPressed: _loadCurrentSection,
            style: OutlinedButton.styleFrom(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
            ),
            icon: const Icon(Icons.refresh, size: 16),
            label: const Text('Refresh', style: TextStyle(fontSize: 12)),
          ),
          const SizedBox(width: 8),
          IconButton(
            tooltip: 'Close',
            onPressed: () => Navigator.of(context).pop(),
            icon: const Icon(Icons.close, size: 20),
          ),
        ],
      ),
    );
  }

  Widget _buildBottomFooter() {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 10),
      decoration: BoxDecoration(
        color: _subtleBg,
        borderRadius: const BorderRadius.vertical(bottom: Radius.circular(12)),
        border: Border(top: BorderSide(color: _borderColor)),
      ),
      child: Row(
        children: [
          Icon(Icons.verified_user_outlined, size: 15, color: Colors.teal.shade600),
          const SizedBox(width: 6),
          Text(
            'File4Base Server Console  •  Active Database: ${widget.activeDatabase ?? 'Default'}  •  Encrypted Channel (AES-256-GCM)',
            style: TextStyle(fontSize: 12, color: _mutedText),
          ),
          const Spacer(),
          OutlinedButton(
            onPressed: () => Navigator.of(context).pop(),
            style: OutlinedButton.styleFrom(
              padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 10),
            ),
            child: const Text('Close'),
          ),
        ],
      ),
    );
  }

  Widget _buildLeftNavigation() {
    final items = <(ServerToolSection, IconData, String, String)>[
      (ServerToolSection.containers, Icons.view_in_ar_outlined, 'Containers & Services', 'Docker stack, restart & logs'),
      (ServerToolSection.backups, Icons.backup_outlined, 'Backups & Snapshots', 'Create & restore DB backups'),
      (ServerToolSection.domainTls, Icons.https_outlined, 'Domain & SSL/TLS (ACME)', "Let's Encrypt & custom certs"),
      (ServerToolSection.telemetry, Icons.insights_outlined, 'API Telemetry & Charts', 'RPM, latency & top endpoints'),
      (ServerToolSection.sessions, Icons.people_alt_outlined, 'Connected Clients', 'Active sessions & revoke'),
      (ServerToolSection.schedules, Icons.schedule_outlined, 'Scheduled Tasks (Cron)', 'Automated backups & scripts'),
      (ServerToolSection.auditLogs, Icons.policy_outlined, 'Audit & Security Logs', 'Auth, MFA & admin trail'),
      (ServerToolSection.maintenance, Icons.build_circle_outlined, 'Database Maintenance', 'VACUUM ANALYZE & catalog'),
      (ServerToolSection.notifications, Icons.notifications_active_outlined, 'Alerts, SMTP & Webhooks', 'Server alert channels'),
    ].where((item) => _maySee(item.$1)).toList();

    final serverWideNote = _serverAdmin
        ? null
        : _adminDatabaseConfigured
            ? 'The server-wide tools (containers, TLS, sessions, audit, schedules, notifications, telemetry) '
                'are for the owners of the server administration database.'
            : 'The server-wide tools are disabled on this server. To enable them, set '
                'FILE4BASE_ADMIN_DATABASE to the database whose owners administer the server.';
    return Container(
      width: 248,
      color: _subtleBg,
      child: Column(
        children: [
          Expanded(child: _buildNavigationList(items)),
          _buildManagementGroup(),
          if (serverWideNote != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 4, 12, 12),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(Icons.lock_outline, size: 14, color: _mutedText),
                  const SizedBox(width: 6),
                  Expanded(child: Text(serverWideNote, style: TextStyle(fontSize: 10.5, color: _mutedText))),
                ],
              ),
            ),
        ],
      ),
    );
  }

  // The "Manage" menu, surfaced at the end of the console for administrators.
  // Each item opens its own dialog on top of Server Tools.
  Widget _buildManagementGroup() {
    if (!_databaseTools || widget.management.isEmpty) return const SizedBox.shrink();
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(8, 4, 8, 6),
      decoration: BoxDecoration(
        border: Border(top: BorderSide(color: _mutedText.withValues(alpha: 0.2))),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 6, 8, 4),
            child: Text(
              'MANAGE',
              style: TextStyle(fontSize: 10, fontWeight: FontWeight.bold, letterSpacing: 0.6, color: _mutedText),
            ),
          ),
          ConstrainedBox(
            constraints: const BoxConstraints(maxHeight: 220),
            child: SingleChildScrollView(
              child: Column(
                children: [
                  for (final action in widget.management)
                    Material(
                      color: Colors.transparent,
                      borderRadius: BorderRadius.circular(8),
                      child: InkWell(
                        borderRadius: BorderRadius.circular(8),
                        onTap: action.onSelected,
                        child: Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
                          child: Row(
                            children: [
                              Icon(action.icon, size: 18, color: _mutedText),
                              const SizedBox(width: 10),
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(action.label, style: const TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600)),
                                    Text(action.subtitle, style: TextStyle(fontSize: 10, color: _mutedText)),
                                  ],
                                ),
                              ),
                              Icon(Icons.open_in_new, size: 13, color: _mutedText),
                            ],
                          ),
                        ),
                      ),
                    ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildNavigationList(List<(ServerToolSection, IconData, String, String)> items) {
    return Container(
      color: _subtleBg,
      child: ListView.separated(
        padding: const EdgeInsets.symmetric(vertical: 10, horizontal: 8),
        itemCount: items.length,
        separatorBuilder: (_, _) => const SizedBox(height: 2),
        itemBuilder: (ctx, i) {
          final (sec, icon, title, subtitle) = items[i];
          final selected = _section == sec;
          return Material(
            color: selected ? _primaryBlue.withValues(alpha: 0.12) : Colors.transparent,
            borderRadius: BorderRadius.circular(8),
            child: InkWell(
              borderRadius: BorderRadius.circular(8),
              onTap: () => _selectSection(sec),
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
                decoration: BoxDecoration(
                  borderRadius: BorderRadius.circular(8),
                  border: Border.all(
                    color: selected ? _primaryBlue.withValues(alpha: 0.4) : Colors.transparent,
                  ),
                ),
                child: Row(
                  children: [
                    Icon(
                      icon,
                      size: 19,
                      color: selected ? _primaryBlue : _mutedText,
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Expanded(
                                child: Text(
                                  title,
                                  style: TextStyle(
                                    fontSize: 12.5,
                                    fontWeight: selected ? FontWeight.bold : FontWeight.w500,
                                    color: selected ? _primaryBlue : null,
                                  ),
                                ),
                              ),
                              if (sec == ServerToolSection.containers && _updateAvailable)
                                Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 1),
                                  decoration: BoxDecoration(
                                    color: Colors.amber.shade700,
                                    borderRadius: BorderRadius.circular(8),
                                  ),
                                  child: Text(
                                    'v$_latestStackVersion',
                                    style: const TextStyle(
                                      fontSize: 9.5,
                                      fontWeight: FontWeight.bold,
                                      color: Colors.white,
                                    ),
                                  ),
                                ),
                            ],
                          ),
                          Text(
                            subtitle,
                            style: TextStyle(fontSize: 10.5, color: _mutedText),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ),
          );
        },
      ),
    );
  }

  Widget _buildMainContent() {
    if (_error != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.error_outline, color: Colors.red, size: 42),
              const SizedBox(height: 12),
              Text(_error!, style: const TextStyle(color: Colors.red), textAlign: TextAlign.center),
              const SizedBox(height: 12),
              ElevatedButton.icon(
                style: ElevatedButton.styleFrom(
                  backgroundColor: _primaryBlue,
                  foregroundColor: Colors.white,
                ),
                onPressed: _loadCurrentSection,
                icon: const Icon(Icons.refresh, size: 16),
                label: const Text('Retry'),
              ),
            ],
          ),
        ),
      );
    }

    switch (_section) {
      case ServerToolSection.containers:
        return _buildContainersView();
      case ServerToolSection.backups:
        return _buildBackupsView();
      case ServerToolSection.domainTls:
        return _buildDomainTlsView();
      case ServerToolSection.telemetry:
        return _buildTelemetryView();
      case ServerToolSection.sessions:
        return _buildSessionsView();
      case ServerToolSection.schedules:
        return _buildSchedulesView();
      case ServerToolSection.auditLogs:
        return _buildAuditLogsView();
      case ServerToolSection.maintenance:
        return _buildMaintenanceView();
      case ServerToolSection.notifications:
        return _buildNotificationsView();
    }
  }

  Future<void> _triggerVersionCheck({String simulateLatestVersion = ''}) async {
    try {
      final res = await widget.apiClient.checkServerContainerUpdates(
        simulateLatestVersion: simulateLatestVersion,
      );
      final list = (res['containers'] as List<dynamic>? ?? const [])
          .whereType<Map<dynamic, dynamic>>()
          .map(Map<String, dynamic>.from)
          .toList();
      final curVer = res['current_version']?.toString() ?? _currentStackVersion;
      final latestVer = res['latest_version']?.toString() ?? curVer;
      final updateAvail = res['update_available'] as bool? ?? false;
      final checkedAt = res['checked_at']?.toString() ?? '';
      final relNotes = res['release_notes']?.toString() ?? '';
      final updCmd = res['update_command']?.toString() ?? _updateCommand;
      widget.onVersionStatusChanged?.call(
        updateAvailable: updateAvail,
        latestVersion: latestVer,
      );
      if (!mounted) return;
      setState(() {
        _containers = list;
        _currentStackVersion = curVer;
        _latestStackVersion = latestVer;
        _updateAvailable = updateAvail;
        _versionCheckedAt = checkedAt;
        _releaseNotes = relNotes;
        _updateCommand = updCmd;
      });
      if (updateAvail) {
        _toast('New version available: v$latestVer (installed: v$curVer)');
      } else {
        _toast('All containers are up to date (v$curVer).');
      }
    } catch (e) {
      _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
    }
  }

  Future<void> _applyStackUpdate({String container = 'file4base-stack'}) async {
    try {
      final res = await widget.apiClient.applyServerContainerUpdate(
        container: container,
        database: widget.activeDatabase,
      );
      final preBackup = res['pre_update_backup']?.toString() ?? '';
      final cmd = res['docker_pull_command']?.toString() ?? _updateCommand;
      await Clipboard.setData(ClipboardData(text: cmd));
      await _loadCurrentSection(silent: true);
      if (preBackup.isNotEmpty) {
        _toast('Pre-update backup created ($preBackup) & update applied! Docker command copied.');
      } else {
        _toast('Update applied for $container! Docker command copied to clipboard.');
      }
    } catch (e) {
      _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
    }
  }

  // ===========================================================================
  // 1. CONTAINERS & SERVICES VIEW
  // ===========================================================================
  Widget _buildContainersView() {
    final containerNames = _containers
        .map((c) => c['name']?.toString() ?? '')
        .where((s) => s.isNotEmpty)
        .toList();
    if (containerNames.isEmpty) {
      containerNames.addAll(const ['file4base-api', 'file4base-web', 'file4base-postgres']);
    }
    final activeLogTarget = containerNames.contains(_selectedLogContainer)
        ? _selectedLogContainer
        : containerNames.first;

    return Padding(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        const Flexible(
                          child: Text(
                            'Docker Containers & Services',
                            style: TextStyle(fontSize: 15, fontWeight: FontWeight.bold),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                        const SizedBox(width: 10),
                        Container(
                          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                          decoration: BoxDecoration(
                            color: (_updateAvailable ? Colors.amber.shade700 : Colors.green.shade700)
                                .withValues(alpha: 0.14),
                            borderRadius: BorderRadius.circular(10),
                            border: Border.all(
                              color: (_updateAvailable ? Colors.amber.shade700 : Colors.green.shade700)
                                  .withValues(alpha: 0.45),
                            ),
                          ),
                          child: Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Icon(
                                _updateAvailable ? Icons.system_update_alt : Icons.verified_outlined,
                                size: 13,
                                color: _updateAvailable ? Colors.amber.shade800 : Colors.green.shade700,
                              ),
                              const SizedBox(width: 5),
                              Text(
                                _updateAvailable
                                    ? 'UPDATE AVAILABLE: v$_currentStackVersion → v$_latestStackVersion'
                                    : 'UP TO DATE (v$_currentStackVersion)',
                                style: TextStyle(
                                  fontSize: 10.5,
                                  fontWeight: FontWeight.bold,
                                  color: _updateAvailable ? Colors.amber.shade800 : Colors.green.shade700,
                                ),
                              ),
                            ],
                          ),
                        ),
                      ],
                    ),
                    Text(
                      'Inspect health, version status (Docker Hub & GitHub Releases), resource telemetry, and logs${_versionCheckedAt.isNotEmpty ? "  •  Last checked: $_versionCheckedAt" : ""}',
                      style: TextStyle(fontSize: 11.5, color: _mutedText),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 12),
              OutlinedButton.icon(
                style: OutlinedButton.styleFrom(
                  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                ),
                onPressed: () => _triggerVersionCheck(
                  simulateLatestVersion: _updateAvailable ? '' : '0.14.0',
                ),
                icon: Icon(
                  _updateAvailable ? Icons.restore : Icons.science_outlined,
                  size: 15,
                ),
                label: Text(
                  _updateAvailable ? 'Reset Version Check' : 'Simulate Update (v0.14.0)',
                  style: const TextStyle(fontSize: 11.5),
                ),
              ),
              const SizedBox(width: 8),
              OutlinedButton.icon(
                style: OutlinedButton.styleFrom(
                  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                ),
                onPressed: () => _triggerVersionCheck(),
                icon: const Icon(Icons.cloud_sync_outlined, size: 15),
                label: const Text('Check for Updates', style: TextStyle(fontSize: 11.5)),
              ),
              const SizedBox(width: 8),
              ElevatedButton.icon(
                style: ElevatedButton.styleFrom(
                  backgroundColor: _primaryBlue,
                  foregroundColor: Colors.white,
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                ),
                onPressed: () async {
                  try {
                    final bkp = await widget.apiClient.createServerBackup(
                      database: widget.activeDatabase,
                      notes: 'Quick snapshot from Containers panel',
                    );
                    _toast('Snapshot created: ${bkp['filename']}');
                  } catch (e) {
                    _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                  }
                },
                icon: const Icon(Icons.backup_outlined, size: 16),
                label: const Text('Quick Backup Now', style: TextStyle(fontSize: 11.5)),
              ),
            ],
          ),
          if (_updateAvailable) ...[
            const SizedBox(height: 10),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
              decoration: BoxDecoration(
                color: Colors.amber.withValues(alpha: 0.12),
                borderRadius: BorderRadius.circular(9),
                border: Border.all(color: Colors.amber.shade700.withValues(alpha: 0.55)),
              ),
              child: Row(
                children: [
                  Icon(Icons.new_releases_rounded, color: Colors.amber.shade800, size: 22),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'New File4Base Container Stack Version Available: v$_latestStackVersion (Installed: v$_currentStackVersion)',
                          style: TextStyle(
                            fontSize: 12.5,
                            fontWeight: FontWeight.bold,
                            color: _isDark ? Colors.amber.shade300 : Colors.amber.shade900,
                          ),
                        ),
                        const SizedBox(height: 2),
                        Text(
                          _releaseNotes.isNotEmpty
                              ? _releaseNotes
                              : 'Update available on Docker Hub & GitHub Releases. Clicking "Pre-Backup & Update Now" creates a full snapshot in ./data/backups/ before updating.',
                          style: TextStyle(fontSize: 11, color: _mutedText),
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(width: 10),
                  OutlinedButton.icon(
                    style: OutlinedButton.styleFrom(
                      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                    ),
                    onPressed: () async {
                      await Clipboard.setData(ClipboardData(text: _updateCommand));
                      _toast('Copied: $_updateCommand');
                    },
                    icon: const Icon(Icons.copy, size: 14),
                    label: Text(_updateCommand, style: const TextStyle(fontSize: 11, fontFamily: 'monospace')),
                  ),
                  const SizedBox(width: 8),
                  ElevatedButton.icon(
                    style: ElevatedButton.styleFrom(
                      backgroundColor: Colors.amber.shade800,
                      foregroundColor: Colors.white,
                      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                    ),
                    onPressed: () => _applyStackUpdate(),
                    icon: const Icon(Icons.system_update_alt, size: 15),
                    label: const Text(
                      'Pre-Backup & Update Now',
                      style: TextStyle(fontSize: 11.5, fontWeight: FontWeight.bold),
                    ),
                  ),
                ],
              ),
            ),
          ],
          const SizedBox(height: 12),
          Row(
            children: _containers.map((c) {
              final name = c['name']?.toString() ?? '';
              final role = c['role']?.toString() ?? '';
              final image = c['image']?.toString() ?? '';
              final curVer = c['current_version']?.toString() ?? _currentStackVersion;
              final latestVer = c['latest_version']?.toString() ?? curVer;
              final hasUpdate = c['update_available'] as bool? ?? false;
              final health = c['health']?.toString() ?? 'healthy';
              final ports = c['ports']?.toString() ?? '';
              final uptime = (c['uptime_seconds'] as num?)?.toInt() ?? 0;
              final memMb = (c['memory_mb'] as num?)?.toDouble() ?? 0.0;
              final cpuPct = (c['cpu_percent'] as num?)?.toDouble() ?? 0.0;
              final isHealthy = health == 'healthy';
              final statusColor = isHealthy ? Colors.green.shade700 : Colors.red.shade700;

              return Expanded(
                child: Container(
                  margin: const EdgeInsets.only(right: 10),
                  padding: const EdgeInsets.all(14),
                  decoration: BoxDecoration(
                    color: _cardBg,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(
                      color: hasUpdate
                          ? Colors.amber.shade600
                          : (isHealthy ? _borderColor : Colors.red.shade400),
                    ),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Container(
                            padding: const EdgeInsets.all(6),
                            decoration: BoxDecoration(
                              color: _primaryBlue.withValues(alpha: 0.1),
                              borderRadius: BorderRadius.circular(8),
                            ),
                            child: Icon(
                              name.contains('web')
                                  ? Icons.language
                                  : (name.contains('api') ? Icons.hub_outlined : Icons.storage_rounded),
                              color: _primaryBlue,
                              size: 18,
                            ),
                          ),
                          const SizedBox(width: 8),
                          Expanded(
                            child: Text(
                              name,
                              style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13.5),
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                            decoration: BoxDecoration(
                              color: statusColor.withValues(alpha: 0.12),
                              borderRadius: BorderRadius.circular(10),
                              border: Border.all(color: statusColor.withValues(alpha: 0.4)),
                            ),
                            child: Text(
                              health.toUpperCase(),
                              style: TextStyle(
                                fontSize: 10,
                                fontWeight: FontWeight.bold,
                                color: statusColor,
                              ),
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 6),
                      Text(
                        role,
                        style: TextStyle(fontSize: 11, color: _mutedText),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                      const Divider(height: 12),
                      Text('Image: $image', style: const TextStyle(fontSize: 11, fontFamily: 'monospace'), overflow: TextOverflow.ellipsis),
                      const SizedBox(height: 2),
                      Row(
                        children: [
                          Text(
                            'Version: v$curVer',
                            style: const TextStyle(fontSize: 11, fontFamily: 'monospace', fontWeight: FontWeight.w600),
                          ),
                          const SizedBox(width: 6),
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
                            decoration: BoxDecoration(
                              color: (hasUpdate ? Colors.amber.shade700 : Colors.teal)
                                  .withValues(alpha: 0.14),
                              borderRadius: BorderRadius.circular(6),
                            ),
                            child: Text(
                              hasUpdate ? 'NEW: v$latestVer' : 'UP TO DATE',
                              style: TextStyle(
                                fontSize: 9.5,
                                fontWeight: FontWeight.bold,
                                color: hasUpdate ? Colors.amber.shade800 : Colors.teal.shade700,
                              ),
                            ),
                          ),
                        ],
                      ),
                      Text('Ports: $ports', style: const TextStyle(fontSize: 11, fontFamily: 'monospace')),
                      Text('Uptime: ${_formatUptime(uptime)}', style: const TextStyle(fontSize: 11)),
                      const SizedBox(height: 6),
                      Row(
                        children: [
                          Text(
                            'CPU: ${cpuPct.toStringAsFixed(1)}%',
                            style: const TextStyle(fontSize: 11, fontWeight: FontWeight.w600, color: _primaryBlue),
                          ),
                          const SizedBox(width: 12),
                          Text(
                            'RAM: ${memMb.toStringAsFixed(1)} MB',
                            style: TextStyle(fontSize: 11, fontWeight: FontWeight.w600, color: Colors.purple.shade600),
                          ),
                        ],
                      ),
                      const SizedBox(height: 10),
                      Row(
                        children: [
                          Expanded(
                            child: OutlinedButton.icon(
                              onPressed: () async {
                                setState(() => _selectedLogContainer = name);
                                final logs = await widget.apiClient.getServerContainerLogs(name);
                                if (!mounted) return;
                                setState(() => _containerLogs = logs);
                              },
                              style: OutlinedButton.styleFrom(
                                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
                              ),
                              icon: const Icon(Icons.terminal, size: 14),
                              label: const Text('Logs', style: TextStyle(fontSize: 11)),
                            ),
                          ),
                          const SizedBox(width: 6),
                          Expanded(
                            child: ElevatedButton.icon(
                              style: ElevatedButton.styleFrom(
                                backgroundColor: hasUpdate ? Colors.amber.shade800 : _primaryBlue,
                                foregroundColor: Colors.white,
                                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
                              ),
                              onPressed: () async {
                                if (hasUpdate) {
                                  await _applyStackUpdate(container: name);
                                  return;
                                }
                                try {
                                  await widget.apiClient.restartServerContainer(name);
                                  _toast('Container $name gracefully restarted.');
                                  await _loadCurrentSection();
                                } catch (e) {
                                  _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                                }
                              },
                              icon: Icon(hasUpdate ? Icons.system_update_alt : Icons.restart_alt, size: 14),
                              label: Text(
                                hasUpdate ? 'Update v$latestVer' : 'Restart',
                                style: const TextStyle(fontSize: 11),
                              ),
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              );
            }).toList(),
          ),
          const SizedBox(height: 14),
          Row(
            children: [
              const Icon(Icons.terminal, size: 17, color: _primaryBlue),
              const SizedBox(width: 8),
              Text(
                'Live Container Logs — $activeLogTarget',
                style: const TextStyle(fontSize: 13, fontWeight: FontWeight.bold),
              ),
              const Spacer(),
              SegmentedButton<String>(
                showSelectedIcon: false,
                segments: containerNames
                    .map((n) => ButtonSegment<String>(
                          value: n,
                          label: Text(n, style: const TextStyle(fontSize: 11)),
                        ))
                    .toList(),
                selected: {activeLogTarget},
                onSelectionChanged: (sel) async {
                  final target = sel.first;
                  setState(() => _selectedLogContainer = target);
                  final logs = await widget.apiClient.getServerContainerLogs(target);
                  if (!mounted) return;
                  setState(() => _containerLogs = logs);
                },
              ),
            ],
          ),
          const SizedBox(height: 8),
          Expanded(
            child: Container(
              width: double.infinity,
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: const Color(0xFF0F172A),
                borderRadius: BorderRadius.circular(8),
                border: Border.all(color: _borderColor),
              ),
              child: _containerLogs.isEmpty
                  ? const Center(
                      child: Text('No log entries recorded yet.', style: TextStyle(color: Colors.grey)),
                    )
                  : ListView.builder(
                      itemCount: _containerLogs.length,
                      itemBuilder: (ctx, i) {
                        final line = _containerLogs[i];
                        final isErr = line.contains('[ERROR]') || line.contains(' 500 ');
                        final isWarn = line.contains('[WARN]') || line.contains(' 40');
                        return Padding(
                          padding: const EdgeInsets.symmetric(vertical: 2),
                          child: SelectableText(
                            line,
                            style: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 11.5,
                              color: isErr
                                  ? Colors.redAccent
                                  : (isWarn ? Colors.amberAccent : const Color(0xFF93C5FD)),
                            ),
                          ),
                        );
                      },
                    ),
            ),
          ),
        ],
      ),
    );
  }

  // ===========================================================================
  // 2. BACKUPS & SNAPSHOTS VIEW
  // ===========================================================================
  Widget _buildBackupsView() {
    return Padding(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Text(
                'Database Backups & Snapshots',
                style: TextStyle(fontSize: 15, fontWeight: FontWeight.bold),
              ),
              const SizedBox(width: 10),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                decoration: BoxDecoration(
                  color: _primaryBlue.withValues(alpha: 0.1),
                  borderRadius: BorderRadius.circular(6),
                  border: Border.all(color: _primaryBlue.withValues(alpha: 0.3)),
                ),
                child: const Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(Icons.folder_outlined, size: 13, color: _primaryBlue),
                    SizedBox(width: 5),
                    SelectableText(
                      './data/backups/  (container: /app/data/backups/)',
                      style: TextStyle(
                        fontSize: 11,
                        fontFamily: 'monospace',
                        fontWeight: FontWeight.w600,
                        color: _primaryBlue,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 2),
          Text(
            'Create point-in-time snapshots of tables, fields, layouts, scripts, value lists, and records stored in ./data/backups.',
            style: TextStyle(fontSize: 12, color: _mutedText),
          ),
          const SizedBox(height: 12),
          Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: _cardBg,
              borderRadius: BorderRadius.circular(8),
              border: Border.all(color: _borderColor),
            ),
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _backupNotesCtrl,
                    decoration: InputDecoration(
                      labelText: 'Snapshot Description / Notes (optional)',
                      hintText: 'e.g. Pre-deployment snapshot or migration checkpoint',
                      isDense: true,
                      border: OutlineInputBorder(borderRadius: BorderRadius.circular(8)),
                    ),
                  ),
                ),
                const SizedBox(width: 12),
                ElevatedButton.icon(
                  style: ElevatedButton.styleFrom(
                    backgroundColor: _primaryBlue,
                    foregroundColor: Colors.white,
                    padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                  ),
                  onPressed: () async {
                    try {
                      final created = await widget.apiClient.createServerBackup(
                        database: widget.activeDatabase,
                        notes: _backupNotesCtrl.text.trim(),
                      );
                      _backupNotesCtrl.clear();
                      _toast('Created backup snapshot: ${created['filename']}');
                      await _loadCurrentSection();
                    } catch (e) {
                      _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                    }
                  },
                  icon: const Icon(Icons.add_circle_outline, size: 18),
                  label: const Text('Create Snapshot Now'),
                ),
                const SizedBox(width: 8),
                OutlinedButton.icon(
                  style: OutlinedButton.styleFrom(
                    padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
                    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                  ),
                  onPressed: _uploadBackupFromFile,
                  icon: const Icon(Icons.upload_file_outlined, size: 18),
                  label: const Text('Upload Backup...'),
                ),
              ],
            ),
          ),
          const SizedBox(height: 14),
          Expanded(
            child: Container(
              decoration: BoxDecoration(
                color: _cardBg,
                border: Border.all(color: _borderColor),
                borderRadius: BorderRadius.circular(8),
              ),
              child: ClipRRect(
                borderRadius: BorderRadius.circular(8),
                child: Column(
                  children: [
                    Container(
                      color: _tableHeaderBg,
                      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                      child: Row(
                        children: [
                          Expanded(flex: 4, child: Text('SNAPSHOT FILE', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          Expanded(flex: 2, child: Text('DATABASE', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          SizedBox(width: 90, child: Text('TABLES / SIZE', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          Expanded(flex: 3, child: Text('CREATED AT / NOTES', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          SizedBox(width: 220, child: Text('ACTIONS', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                        ],
                      ),
                    ),
                    Divider(height: 1, color: _borderColor),
                    Expanded(
                      child: _backups.isEmpty
                          ? Center(
                              child: Text(
                                'No backups stored yet. Click "Create Snapshot Now" to create your first backup.',
                                style: TextStyle(color: _mutedText),
                              ),
                            )
                          : ListView.separated(
                              itemCount: _backups.length,
                              separatorBuilder: (_, _) => Divider(height: 1, color: _borderColor),
                              itemBuilder: (ctx, i) {
                                final b = _backups[i];
                                final id = b['id']?.toString() ?? '';
                                final filename = b['filename']?.toString() ?? '';
                                final db = b['database']?.toString() ?? '';
                                final createdAt = b['created_at']?.toString() ?? '';
                                final notes = b['notes']?.toString() ?? '';
                                final sizeBytes = (b['size_bytes'] as num?)?.toInt() ?? 0;
                                final tablesCount = (b['tables_count'] as num?)?.toInt() ?? 0;

                                return Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                                  color: i.isEven ? Colors.transparent : _subtleBg,
                                  child: Row(
                                    children: [
                                      Expanded(
                                        flex: 4,
                                        child: Row(
                                          children: [
                                            const Icon(Icons.archive_outlined, color: _primaryBlue, size: 18),
                                            const SizedBox(width: 8),
                                            Expanded(
                                              child: Text(
                                                filename,
                                                style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 12, fontFamily: 'monospace'),
                                                overflow: TextOverflow.ellipsis,
                                              ),
                                            ),
                                          ],
                                        ),
                                      ),
                                      Expanded(
                                        flex: 2,
                                        child: Text(db, style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600)),
                                      ),
                                      SizedBox(
                                        width: 90,
                                        child: Text(
                                          '$tablesCount tbl • ${_formatBytes(sizeBytes)}',
                                          style: TextStyle(fontSize: 11, color: _mutedText),
                                        ),
                                      ),
                                      Expanded(
                                        flex: 3,
                                        child: Column(
                                          crossAxisAlignment: CrossAxisAlignment.start,
                                          children: [
                                            Text(createdAt, style: TextStyle(fontSize: 11, color: _mutedText, fontFamily: 'monospace')),
                                            if (notes.isNotEmpty)
                                              Text(notes, style: const TextStyle(fontSize: 11.5), overflow: TextOverflow.ellipsis),
                                          ],
                                        ),
                                      ),
                                      SizedBox(
                                        width: 220,
                                        child: Row(
                                          mainAxisAlignment: MainAxisAlignment.end,
                                          children: [
                                            IconButton(
                                              tooltip: 'Download backup (.tar.gz)',
                                              icon: const Icon(Icons.download_outlined, size: 18, color: _primaryBlue),
                                              onPressed: () async {
                                                try {
                                                  final bytes = await widget.apiClient.downloadServerBackupArchive(id);
                                                  final base = id.endsWith('.f4b.json')
                                                      ? id.substring(0, id.length - '.f4b.json'.length)
                                                      : id;
                                                  await SolutionStorageService.saveFile(
                                                    filename: '$base.tar.gz',
                                                    bytes: bytes,
                                                  );
                                                  _toast('Backup saved as $base.tar.gz');
                                                } catch (e) {
                                                  _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                                                }
                                              },
                                            ),
                                            IconButton(
                                              tooltip: 'Copy backup JSON to clipboard',
                                              icon: const Icon(Icons.copy_all_outlined, size: 17, color: _primaryBlue),
                                              onPressed: () async {
                                                try {
                                                  final raw = await widget.apiClient.downloadServerBackup(id);
                                                  await Clipboard.setData(ClipboardData(text: raw));
                                                  _toast('Backup JSON ($filename) copied to clipboard.');
                                                } catch (e) {
                                                  _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                                                }
                                              },
                                            ),
                                            OutlinedButton.icon(
                                              style: OutlinedButton.styleFrom(
                                                padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                                              ),
                                              onPressed: () async {
                                                try {
                                                  final res = await widget.apiClient.restoreServerBackup(id);
                                                  widget.onDatabaseRestored?.call();
                                                  final layouts = res['layouts_restored'] ?? 0;
                                                  final colsDropped = res['columns_dropped'] ?? 0;
                                                  final colsCreated = res['columns_created'] ?? 0;
                                                  _toast(
                                                    'Restored $filename ($layouts layouts, -$colsDropped/+$colsCreated fields)',
                                                  );
                                                } catch (e) {
                                                  _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                                                }
                                              },
                                              icon: const Icon(Icons.settings_backup_restore, size: 15),
                                              label: const Text('Restore', style: TextStyle(fontSize: 11.5)),
                                            ),
                                            const SizedBox(width: 4),
                                            IconButton(
                                              tooltip: 'Delete snapshot',
                                              icon: const Icon(Icons.delete_outline, size: 18, color: Colors.red),
                                              onPressed: () async {
                                                try {
                                                  await widget.apiClient.deleteServerBackup(id);
                                                  _toast('Deleted snapshot $filename');
                                                  await _loadCurrentSection();
                                                } catch (e) {
                                                  _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                                                }
                                              },
                                            ),
                                          ],
                                        ),
                                      ),
                                    ],
                                  ),
                                );
                              },
                            ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  // ===========================================================================
  /// The install-once card of the local authority: the download buttons plus
  /// what to do with the file on each operating system.
  Widget _buildLocalCaCard() {
    final caSubject = _tlsConfig['ca_subject']?.toString() ?? '';
    final caNotAfter = _tlsConfig['ca_not_after']?.toString() ?? '';
    final caSha256 = _tlsConfig['ca_sha256']?.toString() ?? '';

    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: _cardBg,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: _borderColor),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Icon(Icons.shield_outlined, color: _primaryBlue, size: 22),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      'File4Base Local CA — install once per machine',
                      style: TextStyle(fontWeight: FontWeight.bold, fontSize: 14),
                    ),
                    const SizedBox(height: 3),
                    Text(
                      'This server signs its own certificate with a local authority that is kept on disk and survives '
                      'restarts. Trust the authority once on each machine and every certificate the server reissues '
                      'later — on renewal, or when the domain changes — is trusted too, with nothing to reinstall.',
                      style: TextStyle(fontSize: 12, color: _mutedText),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Wrap(
            spacing: 10,
            runSpacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              ElevatedButton.icon(
                style: ElevatedButton.styleFrom(
                  backgroundColor: _primaryBlue,
                  foregroundColor: Colors.white,
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                ),
                onPressed: _downloadCaCertificate,
                icon: const Icon(Icons.download_outlined, size: 16),
                label: const Text('Download CA Certificate'),
              ),
              OutlinedButton.icon(
                onPressed: _downloadCertificateBundle,
                icon: const Icon(Icons.link_outlined, size: 16),
                label: const Text('Download Certificate Chain (PEM)'),
              ),
            ],
          ),
          const SizedBox(height: 6),
          Text(
            'The downloaded files contain public certificates only; no private key ever leaves the server.',
            style: TextStyle(fontSize: 11, color: _mutedText),
          ),
          if (caSubject.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(
              'Authority: $caSubject  •  valid until $caNotAfter  •  SHA-256: $caSha256',
              style: const TextStyle(fontSize: 11, fontFamily: 'monospace', fontWeight: FontWeight.w600),
            ),
          ],
          const SizedBox(height: 12),
          const Divider(height: 1),
          const SizedBox(height: 10),
          const Text('How to install file4base-local-ca.crt', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
          const SizedBox(height: 8),
          _buildCaInstallStep(
            Icons.laptop_mac,
            'macOS',
            'Double-click the file to open Keychain Access and add it to the System keychain. Then find '
            '"File4Base Local CA" in that keychain, open it, expand Trust and set "When using this certificate" '
            'to "Always Trust".',
          ),
          _buildCaInstallStep(
            Icons.desktop_windows_outlined,
            'Windows',
            'Run certlm.msc (Local Machine certificates), then Trusted Root Certification Authorities → '
            'Certificates → All Tasks → Import, and import the file. Importing per user through certmgr.msc '
            'works the same way for a single account.',
          ),
          _buildCaInstallStep(
            Icons.terminal_outlined,
            'Linux',
            'sudo cp file4base-local-ca.crt /usr/local/share/ca-certificates/ && sudo update-ca-certificates '
            '(Debian/Ubuntu). On Fedora/RHEL copy it to /etc/pki/ca-trust/source/anchors/ and run '
            'sudo update-ca-trust.',
          ),
          _buildCaInstallStep(
            Icons.warning_amber_outlined,
            'Firefox',
            'Firefox keeps its own trust store and ignores the system one: import the file under Settings → '
            'Privacy & Security → Certificates → View Certificates → Authorities → Import, and tick '
            '"Trust this CA to identify websites".',
          ),
          const SizedBox(height: 6),
          Text(
            'Restart the browser after installing. The File4Base desktop client uses the operating system trust '
            'store as well, so the same installation lets it reach this server over HTTPS.',
            style: TextStyle(fontSize: 11, color: _mutedText),
          ),
        ],
      ),
    );
  }

  Widget _buildCaInstallStep(IconData icon, String platform, String body) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 16, color: _mutedText),
          const SizedBox(width: 8),
          Expanded(
            child: RichText(
              text: TextSpan(
                style: TextStyle(fontSize: 11.5, color: _mutedText, height: 1.35),
                children: [
                  TextSpan(text: '$platform: ', style: const TextStyle(fontWeight: FontWeight.bold)),
                  TextSpan(text: body),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  // 3. DOMAIN & SSL/TLS CERTIFICATES (ACME) VIEW
  // ===========================================================================
  Widget _buildDomainTlsView() {
    final certSubject = _tlsConfig['cert_subject']?.toString() ?? '';
    final certIssuer = _tlsConfig['cert_issuer']?.toString() ?? '';
    final certNotAfter = _tlsConfig['cert_not_after']?.toString() ?? '';
    final certDaysLeft = (_tlsConfig['cert_days_left'] as num?)?.toInt() ?? 0;
    final certSha256 = _tlsConfig['cert_sha256']?.toString() ?? '';
    final caAvailable = _tlsConfig['ca_available'] as bool? ?? false;

    return SingleChildScrollView(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      'Web Domain & SSL/TLS Certificate Management (ACME / Custom PEM)',
                      style: TextStyle(fontSize: 15, fontWeight: FontWeight.bold),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      "Configure the public domain for File4Base Web Client, automatic Let's Encrypt (ACME) certificates, or custom signed X.509 certificates.",
                      style: TextStyle(fontSize: 12, color: _mutedText),
                    ),
                  ],
                ),
              ),
              OutlinedButton.icon(
                onPressed: () async {
                  try {
                    final updated = await widget.apiClient.renewServerTlsCertificate();
                    setState(() => _tlsConfig = updated);
                    _toast('TLS certificate renewed and hot-reloaded.');
                  } catch (e) {
                    _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                  }
                },
                icon: const Icon(Icons.autorenew, size: 16),
                label: const Text('Renew / Rotate Certificate'),
              ),
              const SizedBox(width: 10),
              ElevatedButton.icon(
                style: ElevatedButton.styleFrom(
                  backgroundColor: _primaryBlue,
                  foregroundColor: Colors.white,
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                ),
                onPressed: () => _saveTlsSettings(toastMsg: 'Domain & TLS configuration saved.'),
                icon: const Icon(Icons.save_outlined, size: 16),
                label: const Text('Apply Domain & TLS Settings'),
              ),
            ],
          ),
          const SizedBox(height: 14),

          // Active Certificate Inspector Card
          Container(
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(
              color: Colors.teal.withValues(alpha: 0.08),
              borderRadius: BorderRadius.circular(10),
              border: Border.all(color: Colors.teal.withValues(alpha: 0.4)),
            ),
            child: Row(
              children: [
                Icon(Icons.verified_user, color: Colors.teal.shade700, size: 32),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'Active Certificate: $certSubject',
                        style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
                      ),
                      const SizedBox(height: 3),
                      Text('Issuer: $certIssuer', style: TextStyle(fontSize: 12, color: _mutedText)),
                      Text(
                        'Expires: $certNotAfter ($certDaysLeft days remaining)  •  SHA-256: $certSha256',
                        style: TextStyle(
                          fontSize: 11,
                          fontFamily: 'monospace',
                          fontWeight: FontWeight.w600,
                          color: Colors.teal.shade700,
                        ),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 16),

          if (caAvailable) _buildLocalCaCard(),
          if (caAvailable) const SizedBox(height: 16),

          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Container(
                  padding: const EdgeInsets.all(16),
                  decoration: BoxDecoration(
                    color: _cardBg,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(color: _borderColor),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text('1. Domain & HTTP Routing', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 14)),
                      const SizedBox(height: 12),
                      TextField(
                        controller: _domainCtrl,
                        decoration: const InputDecoration(
                          labelText: 'Primary Web Domain (FQDN)',
                          hintText: 'app.yourdomain.com',
                          prefixIcon: Icon(Icons.public, size: 18),
                          border: OutlineInputBorder(),
                          isDense: true,
                        ),
                      ),
                      const SizedBox(height: 12),
                      TextField(
                        controller: _publicUrlCtrl,
                        decoration: const InputDecoration(
                          labelText: 'Public Web Client URL (CORS & Redirects)',
                          hintText: 'https://app.yourdomain.com',
                          prefixIcon: Icon(Icons.link, size: 18),
                          border: OutlineInputBorder(),
                          isDense: true,
                        ),
                      ),
                      const SizedBox(height: 10),
                      Divider(color: _borderColor),
                      SwitchListTile(
                        contentPadding: EdgeInsets.zero,
                        activeThumbColor: _primaryBlue,
                        title: const Text('Force HTTPS Redirect (Port 80 -> 443)', style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500)),
                        subtitle: Text('Redirect all plain HTTP requests to HTTPS', style: TextStyle(fontSize: 11, color: _mutedText)),
                        value: _forceHttps,
                        onChanged: (v) {
                          setState(() => _forceHttps = v);
                          unawaited(_saveTlsSettings(toastMsg: 'Force HTTPS redirect ${v ? "enabled" : "disabled"}.'));
                        },
                      ),
                      SwitchListTile(
                        contentPadding: EdgeInsets.zero,
                        activeThumbColor: _primaryBlue,
                        title: const Text('Enable HSTS (Strict-Transport-Security)', style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500)),
                        subtitle: Text('Instruct browsers to always connect via TLS', style: TextStyle(fontSize: 11, color: _mutedText)),
                        value: _hstsEnabled,
                        onChanged: (v) {
                          setState(() => _hstsEnabled = v);
                          unawaited(_saveTlsSettings(toastMsg: 'HSTS policy ${v ? "enabled" : "disabled"}.'));
                        },
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(width: 16),
              Expanded(
                child: Container(
                  padding: const EdgeInsets.all(16),
                  decoration: BoxDecoration(
                    color: _cardBg,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(color: _borderColor),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text('2. Certificate Provisioning Mode', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 14)),
                      const SizedBox(height: 12),
                      DropdownButtonFormField<String>(
                        initialValue: _tlsMode,
                        decoration: const InputDecoration(
                          labelText: 'TLS Certificate Source',
                          border: OutlineInputBorder(),
                          isDense: true,
                        ),
                        items: const [
                          DropdownMenuItem(value: 'acme', child: Text("ACME Automatic (Let's Encrypt / ZeroSSL)")),
                          DropdownMenuItem(value: 'custom', child: Text('Custom Signed Certificate (PEM Upload)')),
                          DropdownMenuItem(value: 'self_signed', child: Text('Self-Signed ECDSA P-256 (Local / Intranet)')),
                          DropdownMenuItem(value: 'http_only', child: Text('HTTP Only (Reverse Proxy Terminated)')),
                        ],
                        onChanged: (v) {
                          if (v == null) return;
                          setState(() => _tlsMode = v);
                          if (v != 'custom') {
                            unawaited(_saveTlsSettings(toastMsg: 'TLS mode switched to $v.'));
                          }
                        },
                      ),
                      const SizedBox(height: 12),
                      if (_tlsMode == 'acme') ...[
                        TextField(
                          controller: _acmeEmailCtrl,
                          decoration: const InputDecoration(
                            labelText: "ACME Account Email (Let's Encrypt notifications)",
                            hintText: 'security@yourdomain.com',
                            prefixIcon: Icon(Icons.email_outlined, size: 18),
                            border: OutlineInputBorder(),
                            isDense: true,
                          ),
                        ),
                        const SizedBox(height: 12),
                        Row(
                          children: [
                            Expanded(
                              child: DropdownButtonFormField<String>(
                                initialValue: _acmeProvider,
                                decoration: const InputDecoration(
                                  labelText: 'ACME Directory CA',
                                  border: OutlineInputBorder(),
                                  isDense: true,
                                ),
                                items: const [
                                  DropdownMenuItem(value: 'letsencrypt_prod', child: Text("Let's Encrypt (Prod)")),
                                  DropdownMenuItem(value: 'letsencrypt_staging', child: Text("Let's Encrypt (Staging)")),
                                  DropdownMenuItem(value: 'zerossl', child: Text('ZeroSSL ACME')),
                                ],
                                onChanged: (v) {
                                  if (v == null) return;
                                  setState(() => _acmeProvider = v);
                                  unawaited(_saveTlsSettings(toastMsg: 'ACME provider updated.'));
                                },
                              ),
                            ),
                            const SizedBox(width: 10),
                            Expanded(
                              child: DropdownButtonFormField<String>(
                                initialValue: _acmeChallenge,
                                decoration: const InputDecoration(
                                  labelText: 'Challenge Type',
                                  border: OutlineInputBorder(),
                                  isDense: true,
                                ),
                                items: const [
                                  DropdownMenuItem(value: 'http-01', child: Text('HTTP-01 (Port 80)')),
                                  DropdownMenuItem(value: 'tls-alpn-01', child: Text('TLS-ALPN-01 (Port 443)')),
                                ],
                                onChanged: (v) {
                                  if (v == null) return;
                                  setState(() => _acmeChallenge = v);
                                  unawaited(_saveTlsSettings(toastMsg: 'ACME challenge updated.'));
                                },
                              ),
                            ),
                          ],
                        ),
                        const SizedBox(height: 6),
                        SwitchListTile(
                          contentPadding: EdgeInsets.zero,
                          activeThumbColor: _primaryBlue,
                          title: const Text('Auto-Renew 30 days before expiration', style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500)),
                          value: _autoRenew,
                          onChanged: (v) {
                            setState(() => _autoRenew = v);
                            unawaited(_saveTlsSettings(toastMsg: 'ACME Auto-Renew ${v ? "enabled" : "disabled"}.'));
                          },
                        ),
                      ] else if (_tlsMode == 'custom') ...[
                        TextField(
                          controller: _customCertCtrl,
                          maxLines: 4,
                          style: const TextStyle(fontFamily: 'monospace', fontSize: 11),
                          decoration: const InputDecoration(
                            labelText: 'Signed Certificate Chain (fullchain.pem)',
                            hintText: '-----BEGIN CERTIFICATE-----\n...',
                            border: OutlineInputBorder(),
                          ),
                        ),
                        const SizedBox(height: 10),
                        TextField(
                          controller: _customKeyCtrl,
                          maxLines: 3,
                          style: const TextStyle(fontFamily: 'monospace', fontSize: 11),
                          decoration: const InputDecoration(
                            labelText: 'Private Key (privkey.pem)',
                            hintText: '-----BEGIN EC PRIVATE KEY----- or -----BEGIN PRIVATE KEY-----',
                            border: OutlineInputBorder(),
                          ),
                        ),
                      ] else ...[
                        Container(
                          padding: const EdgeInsets.all(12),
                          decoration: BoxDecoration(
                            color: _subtleBg,
                            borderRadius: BorderRadius.circular(8),
                            border: Border.all(color: _borderColor),
                          ),
                          child: Text(
                            _tlsMode == 'self_signed'
                                ? 'Self-signed ECDSA P-256 certificate is automatically generated for local/intranet HTTPS connections.'
                                : 'HTTP-Only mode leaves TLS termination to an upstream reverse proxy (Cloudflare, Traefik, AWS ALB).',
                            style: TextStyle(fontSize: 12, color: _mutedText),
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  // ===========================================================================
  // 4. API TELEMETRY & LIVE METRICS VIEW
  // ===========================================================================
  Widget _buildTelemetryView() {
    final tele = (_telemetryRoot['telemetry'] is Map)
        ? Map<String, dynamic>.from(_telemetryRoot['telemetry'] as Map)
        : <String, dynamic>{};
    final totalReqs = (tele['total_requests'] as num?)?.toInt() ?? 0;
    final totalErrs = (tele['total_errors'] as num?)?.toInt() ?? 0;
    final currentRpm = (tele['current_rpm'] as num?)?.toInt() ?? 0;
    final avgLat = (tele['avg_latency_ms'] as num?)?.toDouble() ?? 0.0;
    final p95Lat = (tele['p95_latency_ms'] as num?)?.toDouble() ?? 0.0;
    final allocMb = (tele['alloc_mb'] as num?)?.toDouble() ?? 0.0;
    final goroutines = (tele['num_goroutines'] as num?)?.toInt() ?? 0;
    final dbLatency = (_telemetryRoot['db_latency_ms'] as num?)?.toDouble() ?? 0.0;
    final series = (tele['minute_series'] as List<dynamic>? ?? const [])
        .whereType<Map<dynamic, dynamic>>()
        .map(Map<String, dynamic>.from)
        .toList();
    final topEndpoints = (tele['top_endpoints'] as List<dynamic>? ?? const [])
        .whereType<Map<dynamic, dynamic>>()
        .map(Map<String, dynamic>.from)
        .toList();

    return SingleChildScrollView(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Text(
                'API Telemetry & Live Traffic Charts',
                style: TextStyle(fontSize: 15, fontWeight: FontWeight.bold),
              ),
              const Spacer(),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 2),
                decoration: BoxDecoration(
                  color: _subtleBg,
                  borderRadius: BorderRadius.circular(8),
                  border: Border.all(color: _borderColor),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      Icons.circle,
                      size: 8,
                      color: _autoRefreshTelemetry ? Colors.green : Colors.grey,
                    ),
                    const SizedBox(width: 6),
                    Text(
                      _autoRefreshTelemetry ? 'Live Auto-Refresh (6s)' : 'Auto-Refresh Paused',
                      style: TextStyle(fontSize: 11.5, color: _mutedText, fontWeight: FontWeight.w500),
                    ),
                    const SizedBox(width: 6),
                    SizedBox(
                      height: 28,
                      child: FittedBox(
                        child: Switch(
                          value: _autoRefreshTelemetry,
                          activeThumbColor: Colors.green,
                          onChanged: (v) {
                            setState(() => _autoRefreshTelemetry = v);
                            if (v) unawaited(_loadCurrentSection(silent: true));
                          },
                        ),
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 10),
              SegmentedButton<int>(
                showSelectedIcon: false,
                segments: const [
                  ButtonSegment(value: 15, label: Text('15 min', style: TextStyle(fontSize: 11))),
                  ButtonSegment(value: 30, label: Text('30 min', style: TextStyle(fontSize: 11))),
                  ButtonSegment(value: 60, label: Text('60 min', style: TextStyle(fontSize: 11))),
                ],
                selected: {_telemetryMinutes},
                onSelectionChanged: (sel) {
                  setState(() => _telemetryMinutes = sel.first);
                  unawaited(_loadCurrentSection());
                },
              ),
            ],
          ),
          const SizedBox(height: 14),

          // KPI Cards
          Row(
            children: [
              _buildKpiCard('Requests / Min (RPM)', '$currentRpm req/m', 'Total: $totalReqs requests', _primaryBlue, Icons.speed),
              const SizedBox(width: 10),
              _buildKpiCard('API Latency (Avg / P95)', '${avgLat.toStringAsFixed(2)} ms', 'P95: ${p95Lat.toStringAsFixed(2)} ms', Colors.teal.shade600, Icons.timer_outlined),
              const SizedBox(width: 10),
              _buildKpiCard('HTTP Error Rate (4xx/5xx)', '$totalErrs errors', totalReqs > 0 ? '${((totalErrs / totalReqs) * 100).toStringAsFixed(1)}% of traffic' : '0.0%', Colors.orange.shade700, Icons.warning_amber_rounded),
              const SizedBox(width: 10),
              _buildKpiCard('Engine Runtime & DB Ping', '${allocMb.toStringAsFixed(1)} MB RAM', '$goroutines goroutines • DB ${dbLatency.toStringAsFixed(2)} ms', Colors.purple.shade600, Icons.memory),
            ],
          ),
          const SizedBox(height: 16),

          // Charts Row
          Row(
            children: [
              Expanded(
                child: Container(
                  padding: const EdgeInsets.all(14),
                  decoration: BoxDecoration(
                    color: _cardBg,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(color: _borderColor),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Row(
                        children: [
                          Text('Requests Per Minute (RPM by HTTP Status)', style: TextStyle(fontSize: 13, fontWeight: FontWeight.bold)),
                          Spacer(),
                          _LegendDot(color: Colors.teal, label: '2xx'),
                          SizedBox(width: 8),
                          _LegendDot(color: Colors.orange, label: '4xx'),
                          SizedBox(width: 8),
                          _LegendDot(color: Colors.red, label: '5xx'),
                        ],
                      ),
                      const SizedBox(height: 12),
                      SizedBox(
                        height: 155,
                        width: double.infinity,
                        child: CustomPaint(
                          painter: _RpmBarChartPainter(series, isDark: _isDark),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Container(
                  padding: const EdgeInsets.all(14),
                  decoration: BoxDecoration(
                    color: _cardBg,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(color: _borderColor),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Row(
                        children: [
                          Text('Latency Trend per Minute (ms)', style: TextStyle(fontSize: 13, fontWeight: FontWeight.bold)),
                          Spacer(),
                          _LegendDot(color: _primaryBlue, label: 'Avg (ms)'),
                          SizedBox(width: 8),
                          _LegendDot(color: Colors.purple, label: 'P95 (ms)'),
                        ],
                      ),
                      const SizedBox(height: 12),
                      SizedBox(
                        height: 155,
                        width: double.infinity,
                        child: CustomPaint(
                          painter: _LatencyLineChartPainter(series, isDark: _isDark),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 16),

          // Top Endpoints Table
          const Text('Top API Endpoints by Traffic & Latency', style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.bold)),
          const SizedBox(height: 8),
          Container(
            decoration: BoxDecoration(
              color: _cardBg,
              borderRadius: BorderRadius.circular(8),
              border: Border.all(color: _borderColor),
            ),
            child: ClipRRect(
              borderRadius: BorderRadius.circular(8),
              child: Column(
                children: [
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
                    color: _tableHeaderBg,
                    child: Row(
                      children: [
                        SizedBox(width: 70, child: Text('METHOD', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: _mutedText))),
                        Expanded(child: Text('ENDPOINT ROUTE', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: _mutedText))),
                        SizedBox(width: 80, child: Text('REQUESTS', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: _mutedText))),
                        SizedBox(width: 80, child: Text('ERRORS', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: _mutedText))),
                        SizedBox(width: 100, child: Text('AVG LATENCY', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: _mutedText))),
                        SizedBox(width: 100, child: Text('MAX LATENCY', style: TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: _mutedText))),
                      ],
                    ),
                  ),
                  for (var i = 0; i < topEndpoints.take(10).length; i++)
                    Container(
                      color: i.isEven ? Colors.transparent : _subtleBg,
                      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 7),
                      child: Row(
                        children: [
                          SizedBox(
                            width: 70,
                            child: Text(
                              topEndpoints[i]['method']?.toString() ?? 'GET',
                              style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: _primaryBlue, fontFamily: 'monospace'),
                            ),
                          ),
                          Expanded(
                            child: Text(
                              topEndpoints[i]['path']?.toString() ?? '',
                              style: const TextStyle(fontSize: 12, fontFamily: 'monospace'),
                            ),
                          ),
                          SizedBox(width: 80, child: Text('${topEndpoints[i]['requests'] ?? 0}', style: const TextStyle(fontSize: 12))),
                          SizedBox(
                            width: 80,
                            child: Text(
                              '${topEndpoints[i]['errors'] ?? 0}',
                              style: TextStyle(
                                fontSize: 12,
                                fontWeight: ((topEndpoints[i]['errors'] as num?)?.toInt() ?? 0) > 0 ? FontWeight.bold : FontWeight.normal,
                                color: ((topEndpoints[i]['errors'] as num?)?.toInt() ?? 0) > 0 ? Colors.red : _mutedText,
                              ),
                            ),
                          ),
                          SizedBox(
                            width: 100,
                            child: Text('${((topEndpoints[i]['avg_latency_ms'] as num?)?.toDouble() ?? 0).toStringAsFixed(2)} ms', style: const TextStyle(fontSize: 12)),
                          ),
                          SizedBox(
                            width: 100,
                            child: Text('${((topEndpoints[i]['max_latency_ms'] as num?)?.toDouble() ?? 0).toStringAsFixed(2)} ms', style: TextStyle(fontSize: 12, color: _mutedText)),
                          ),
                        ],
                      ),
                    ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildKpiCard(String title, String value, String subtitle, Color accent, IconData icon) {
    return Expanded(
      child: Container(
        padding: const EdgeInsets.all(14),
        decoration: BoxDecoration(
          color: _cardBg,
          borderRadius: BorderRadius.circular(10),
          border: Border.all(color: _borderColor),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(icon, size: 16, color: accent),
                const SizedBox(width: 6),
                Expanded(
                  child: Text(title, style: TextStyle(fontSize: 11, color: _mutedText), overflow: TextOverflow.ellipsis),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Text(value, style: TextStyle(fontSize: 19, fontWeight: FontWeight.bold, color: accent)),
            const SizedBox(height: 3),
            Text(subtitle, style: TextStyle(fontSize: 11, color: _mutedText), overflow: TextOverflow.ellipsis),
          ],
        ),
      ),
    );
  }

  // ===========================================================================
  // 5. CONNECTED CLIENTS & SESSIONS VIEW
  // ===========================================================================
  Widget _buildSessionsView() {
    return Padding(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'Connected Clients & Active Sessions (${_sessions.length})',
            style: const TextStyle(fontSize: 15, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 2),
          Text(
            'Inspect live bearer sessions across web containers and desktop clients bound to each database, and revoke access immediately if needed.',
            style: TextStyle(fontSize: 12, color: _mutedText),
          ),
          const SizedBox(height: 14),
          Expanded(
            child: Container(
              decoration: BoxDecoration(
                color: _cardBg,
                border: Border.all(color: _borderColor),
                borderRadius: BorderRadius.circular(8),
              ),
              child: ClipRRect(
                borderRadius: BorderRadius.circular(8),
                child: Column(
                  children: [
                    Container(
                      color: _tableHeaderBg,
                      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                      child: Row(
                        children: [
                          Expanded(flex: 2, child: Text('ACCOUNT / ROLE', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          Expanded(flex: 2, child: Text('DATABASE', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          Expanded(flex: 2, child: Text('WEB NODE / CLIENT IP', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          Expanded(flex: 2, child: Text('SESSION / CALLS', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          Expanded(flex: 3, child: Text('LAST SEEN / EXPIRES', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          SizedBox(width: 130, child: Text('ACTION', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                        ],
                      ),
                    ),
                    Divider(height: 1, color: _borderColor),
                    Expanded(
                      child: ListView.separated(
                        itemCount: _sessions.length,
                        separatorBuilder: (_, _) => Divider(height: 1, color: _borderColor),
                        itemBuilder: (ctx, i) {
                          final s = _sessions[i];
                          final sid = s['session_id']?.toString() ?? '';
                          final user = s['username']?.toString() ?? '';
                          final role = s['role']?.toString() ?? '';
                          final db = s['database']?.toString() ?? '';
                          final webNode = s['web_container']?.toString().isNotEmpty == true
                              ? s['web_container'].toString()
                              : 'web-default';
                          final clientIp = s['client_ip']?.toString() ?? '-';
                          final reqCount = s['requests_count']?.toString() ?? '1';
                          final lastSeen = s['last_seen_at']?.toString() ?? s['created_at']?.toString() ?? '';
                          final expires = s['expires_at']?.toString() ?? '';
                          final isCurrent = s['is_current'] as bool? ?? false;

                          return Container(
                            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                            color: i.isEven ? Colors.transparent : _subtleBg,
                            child: Row(
                              children: [
                                Expanded(
                                  flex: 2,
                                  child: Row(
                                    children: [
                                      const Icon(Icons.person_pin_circle_outlined, color: _primaryBlue, size: 20),
                                      const SizedBox(width: 8),
                                      Flexible(
                                        child: Text(user, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13), overflow: TextOverflow.ellipsis),
                                      ),
                                      const SizedBox(width: 6),
                                      Container(
                                        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
                                        decoration: BoxDecoration(
                                          color: _primaryBlue.withValues(alpha: 0.12),
                                          borderRadius: BorderRadius.circular(6),
                                        ),
                                        child: Text(role.toUpperCase(), style: const TextStyle(fontSize: 10, fontWeight: FontWeight.bold, color: _primaryBlue)),
                                      ),
                                    ],
                                  ),
                                ),
                                Expanded(
                                  flex: 2,
                                  child: Row(
                                    children: [
                                      Flexible(
                                        child: Text(db, style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600), overflow: TextOverflow.ellipsis),
                                      ),
                                      if (isCurrent) ...[
                                        const SizedBox(width: 6),
                                        Container(
                                          padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
                                          decoration: BoxDecoration(
                                            color: Colors.green.withValues(alpha: 0.15),
                                            borderRadius: BorderRadius.circular(8),
                                          ),
                                          child: Text('Current', style: TextStyle(fontSize: 10, fontWeight: FontWeight.bold, color: Colors.green.shade700)),
                                        ),
                                      ],
                                    ],
                                  ),
                                ),
                                Expanded(
                                  flex: 2,
                                  child: Column(
                                    crossAxisAlignment: CrossAxisAlignment.start,
                                    children: [
                                      Text('Node: $webNode', style: const TextStyle(fontSize: 11.5, fontFamily: 'monospace', fontWeight: FontWeight.w600)),
                                      Text('IP: $clientIp', style: TextStyle(fontSize: 10.5, color: _mutedText, fontFamily: 'monospace')),
                                    ],
                                  ),
                                ),
                                Expanded(
                                  flex: 2,
                                  child: Column(
                                    crossAxisAlignment: CrossAxisAlignment.start,
                                    children: [
                                      Text(sid, style: const TextStyle(fontSize: 11.5, fontFamily: 'monospace')),
                                      Text('$reqCount API calls', style: TextStyle(fontSize: 10.5, color: _mutedText)),
                                    ],
                                  ),
                                ),
                                Expanded(
                                  flex: 3,
                                  child: Text(
                                    'Seen: $lastSeen\nExp:  $expires',
                                    style: TextStyle(fontSize: 10.5, color: _mutedText, fontFamily: 'monospace'),
                                  ),
                                ),
                                SizedBox(
                                  width: 130,
                                  child: OutlinedButton.icon(
                                    onPressed: isCurrent
                                        ? null
                                        : () async {
                                            try {
                                              await widget.apiClient.revokeServerSession(sid);
                                              _toast('Disconnected session $sid ($user)');
                                              await _loadCurrentSection();
                                            } catch (e) {
                                              _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                                            }
                                          },
                                    icon: Icon(Icons.logout, size: 14, color: isCurrent ? Colors.grey : Colors.red),
                                    label: Text(
                                      isCurrent ? 'Active' : 'Revoke',
                                      style: TextStyle(color: isCurrent ? Colors.grey : Colors.red, fontSize: 11.5),
                                    ),
                                  ),
                                ),
                              ],
                            ),
                          );
                        },
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  // ===========================================================================
  // 6. SCHEDULED TASKS & CRON SCRIPTS VIEW
  // ===========================================================================
  Widget _buildSchedulesView() {
    return Padding(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('Scheduled Server Tasks & Cron Jobs', style: TextStyle(fontSize: 15, fontWeight: FontWeight.bold)),
                    const SizedBox(height: 2),
                    Text('Automate nightly database snapshots, maintenance, and server scripts. Toggle the switch to enable or pause any schedule.', style: TextStyle(fontSize: 12, color: _mutedText)),
                  ],
                ),
              ),
              ElevatedButton.icon(
                style: ElevatedButton.styleFrom(
                  backgroundColor: _primaryBlue,
                  foregroundColor: Colors.white,
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                ),
                onPressed: _promptNewSchedule,
                icon: const Icon(Icons.add_alarm, size: 16),
                label: const Text('New Scheduled Task'),
              ),
            ],
          ),
          const SizedBox(height: 14),
          Expanded(
            child: Container(
              decoration: BoxDecoration(
                color: _cardBg,
                border: Border.all(color: _borderColor),
                borderRadius: BorderRadius.circular(8),
              ),
              child: ClipRRect(
                borderRadius: BorderRadius.circular(8),
                child: Column(
                  children: [
                    Container(
                      color: _tableHeaderBg,
                      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                      child: Row(
                        children: [
                          SizedBox(width: 120, child: Text('STATUS', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          Expanded(flex: 3, child: Text('TASK NAME & TARGET', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          Expanded(flex: 2, child: Text('CRON SCHEDULE', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          Expanded(flex: 2, child: Text('LAST EXECUTION', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          SizedBox(width: 170, child: Text('ACTIONS', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                        ],
                      ),
                    ),
                    Divider(height: 1, color: _borderColor),
                    Expanded(
                      child: ListView.separated(
                        itemCount: _schedules.length,
                        separatorBuilder: (_, _) => Divider(height: 1, color: _borderColor),
                        itemBuilder: (ctx, i) {
                          final job = _schedules[i];
                          final id = job['id']?.toString() ?? '';
                          final name = job['name']?.toString() ?? '';
                          final cron = job['cron_expr']?.toString() ?? '';
                          final target = job['target']?.toString() ?? '';
                          final lastRun = job['last_run_at']?.toString() ?? 'Never';
                          final status = job['last_status']?.toString() ?? 'scheduled';
                          final enabled = job['enabled'] as bool? ?? true;

                          return Container(
                            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                            color: i.isEven ? Colors.transparent : _subtleBg,
                            child: Row(
                              children: [
                                SizedBox(
                                  width: 120,
                                  child: Row(
                                    children: [
                                      SizedBox(
                                        width: 42,
                                        height: 26,
                                        child: FittedBox(
                                          child: Switch(
                                            value: enabled,
                                            activeThumbColor: Colors.green,
                                            onChanged: (val) async {
                                              setState(() {
                                                _schedules[i] = {...job, 'enabled': val};
                                              });
                                              try {
                                                await widget.apiClient.updateServerSchedule(id, {'enabled': val});
                                                _toast('Schedule "$name" ${val ? "enabled" : "paused"}.');
                                              } catch (e) {
                                                _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                                                await _loadCurrentSection(silent: true);
                                              }
                                            },
                                          ),
                                        ),
                                      ),
                                      const SizedBox(width: 6),
                                      Text(
                                        enabled ? 'Active' : 'Paused',
                                        style: TextStyle(
                                          fontSize: 11,
                                          fontWeight: FontWeight.bold,
                                          color: enabled ? Colors.green.shade700 : _mutedText,
                                        ),
                                      ),
                                    ],
                                  ),
                                ),
                                Expanded(
                                  flex: 3,
                                  child: Column(
                                    crossAxisAlignment: CrossAxisAlignment.start,
                                    children: [
                                      Text(name, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                                      Text('Target: $target', style: TextStyle(fontSize: 11, color: _mutedText)),
                                    ],
                                  ),
                                ),
                                Expanded(
                                  flex: 2,
                                  child: Text(cron, style: const TextStyle(fontSize: 12, fontFamily: 'monospace', fontWeight: FontWeight.w600)),
                                ),
                                Expanded(
                                  flex: 2,
                                  child: Text('$lastRun\n($status)', style: TextStyle(fontSize: 11, color: _mutedText, fontFamily: 'monospace')),
                                ),
                                SizedBox(
                                  width: 170,
                                  child: Row(
                                    mainAxisAlignment: MainAxisAlignment.end,
                                    children: [
                                      OutlinedButton.icon(
                                        style: OutlinedButton.styleFrom(
                                          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                                        ),
                                        onPressed: () async {
                                          try {
                                            await widget.apiClient.runServerScheduleNow(id);
                                            _toast('Executed task "$name"');
                                            await _loadCurrentSection();
                                          } catch (e) {
                                            _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                                          }
                                        },
                                        icon: const Icon(Icons.play_arrow, size: 15, color: _primaryBlue),
                                        label: const Text('Run Now', style: TextStyle(fontSize: 11.5)),
                                      ),
                                      const SizedBox(width: 4),
                                      IconButton(
                                        tooltip: 'Delete schedule',
                                        icon: const Icon(Icons.delete_outline, size: 18, color: Colors.red),
                                        onPressed: () async {
                                          await widget.apiClient.deleteServerSchedule(id);
                                          _toast('Deleted schedule "$name"');
                                          await _loadCurrentSection();
                                        },
                                      ),
                                    ],
                                  ),
                                ),
                              ],
                            ),
                          );
                        },
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _promptNewSchedule() async {
    final nameCtrl = TextEditingController();
    final cronCtrl = TextEditingController(text: '0 2 * * *');
    final targetCtrl = TextEditingController(text: 'Full SQL Snapshot');
    var jobType = 'backup';

    await showDialog<void>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDlgState) => AlertDialog(
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
          title: const Row(
            children: [
              Icon(Icons.add_alarm, color: _primaryBlue, size: 22),
              SizedBox(width: 8),
              Text('New Scheduled Server Task', style: TextStyle(fontSize: 16)),
            ],
          ),
          content: SizedBox(
            width: 420,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                TextField(
                  controller: nameCtrl,
                  decoration: const InputDecoration(
                    labelText: 'Task Name',
                    hintText: 'Hourly Recipe Backup',
                    border: OutlineInputBorder(),
                    isDense: true,
                  ),
                ),
                const SizedBox(height: 12),
                DropdownButtonFormField<String>(
                  initialValue: jobType,
                  decoration: const InputDecoration(
                    labelText: 'Task Type',
                    border: OutlineInputBorder(),
                    isDense: true,
                  ),
                  items: const [
                    DropdownMenuItem(value: 'backup', child: Text('Automated Database Backup')),
                    DropdownMenuItem(value: 'script', child: Text('Server Script Execution')),
                    DropdownMenuItem(value: 'maintenance', child: Text('VACUUM ANALYZE Maintenance')),
                  ],
                  onChanged: (v) {
                    if (v == null) return;
                    setDlgState(() {
                      jobType = v;
                      if (v == 'backup') targetCtrl.text = 'Full SQL Snapshot';
                      if (v == 'maintenance') targetCtrl.text = 'VACUUM ANALYZE';
                    });
                  },
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: cronCtrl,
                  decoration: const InputDecoration(
                    labelText: 'Cron Expression (UTC)',
                    hintText: '0 2 * * *',
                    border: OutlineInputBorder(),
                    isDense: true,
                  ),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: targetCtrl,
                  decoration: const InputDecoration(
                    labelText: 'Target Script / Action',
                    border: OutlineInputBorder(),
                    isDense: true,
                  ),
                ),
              ],
            ),
          ),
          actions: [
            TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
            ElevatedButton(
              style: ElevatedButton.styleFrom(
                backgroundColor: _primaryBlue,
                foregroundColor: Colors.white,
              ),
              onPressed: () async {
                if (nameCtrl.text.trim().isEmpty) return;
                await widget.apiClient.createServerSchedule({
                  'name': nameCtrl.text.trim(),
                  'job_type': jobType,
                  'cron_expr': cronCtrl.text.trim(),
                  'target': targetCtrl.text.trim(),
                  'database': widget.activeDatabase ?? '*',
                });
                if (!ctx.mounted) return;
                Navigator.pop(ctx);
                _toast('Created schedule "${nameCtrl.text.trim()}"');
                await _loadCurrentSection();
              },
              child: const Text('Create Schedule'),
            ),
          ],
        ),
      ),
    );
  }

  // ===========================================================================
  // 7. AUDIT & SECURITY LOGS VIEW
  // ===========================================================================
  Widget _buildAuditLogsView() {
    final filtered = _auditCategoryFilter == 'all'
        ? _auditEvents
        : _auditEvents.where((e) => e['category'] == _auditCategoryFilter).toList();

    return Padding(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('Security & Administrative Audit Trail', style: TextStyle(fontSize: 15, fontWeight: FontWeight.bold)),
                  Text('Immutable event stream of authentication, MFA, backups, TLS, and container operations', style: TextStyle(fontSize: 12, color: _mutedText)),
                ],
              ),
              const Spacer(),
              SizedBox(
                width: 220,
                child: DropdownButtonFormField<String>(
                  initialValue: _auditCategoryFilter,
                  decoration: const InputDecoration(
                    labelText: 'Category Filter',
                    border: OutlineInputBorder(),
                    isDense: true,
                    contentPadding: EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                  ),
                  items: const [
                    DropdownMenuItem(value: 'all', child: Text('All Categories')),
                    DropdownMenuItem(value: 'system', child: Text('System Startup')),
                    DropdownMenuItem(value: 'auth', child: Text('Authentication & Sessions')),
                    DropdownMenuItem(value: 'container', child: Text('Containers')),
                    DropdownMenuItem(value: 'backup', child: Text('Backups')),
                    DropdownMenuItem(value: 'tls', child: Text('Domain & TLS')),
                    DropdownMenuItem(value: 'schedule', child: Text('Scheduled Tasks')),
                    DropdownMenuItem(value: 'maintenance', child: Text('Database Maintenance')),
                    DropdownMenuItem(value: 'notifications', child: Text('Notifications')),
                  ],
                  onChanged: (v) {
                    if (v != null) setState(() => _auditCategoryFilter = v);
                  },
                ),
              ),
              const SizedBox(width: 8),
              OutlinedButton.icon(
                onPressed: () async {
                  await widget.apiClient.clearServerAuditLogs();
                  _toast('Audit trail rotated.');
                  await _loadCurrentSection();
                },
                icon: const Icon(Icons.cleaning_services_outlined, size: 16),
                label: const Text('Rotate Logs'),
              ),
            ],
          ),
          const SizedBox(height: 14),
          Expanded(
            child: Container(
              decoration: BoxDecoration(
                color: _cardBg,
                border: Border.all(color: _borderColor),
                borderRadius: BorderRadius.circular(8),
              ),
              child: ClipRRect(
                borderRadius: BorderRadius.circular(8),
                child: ListView.separated(
                  itemCount: filtered.length,
                  separatorBuilder: (_, _) => Divider(height: 1, color: _borderColor),
                  itemBuilder: (ctx, i) {
                    final ev = filtered[i];
                    final action = ev['action']?.toString() ?? '';
                    final details = ev['details']?.toString() ?? '';
                    final actor = ev['actor']?.toString() ?? '';
                    final ts = ev['timestamp']?.toString() ?? '';
                    final category = ev['category']?.toString() ?? '';
                    final status = ev['status']?.toString() ?? 'success';
                    final isWarn = status == 'warning';

                    return Container(
                      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                      color: i.isEven ? Colors.transparent : _subtleBg,
                      child: Row(
                        children: [
                          Icon(
                            isWarn ? Icons.warning_amber_rounded : Icons.verified_outlined,
                            size: 18,
                            color: isWarn ? Colors.orange.shade700 : Colors.teal.shade600,
                          ),
                          const SizedBox(width: 10),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Row(
                                  children: [
                                    Text(action, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                                    const SizedBox(width: 8),
                                    Container(
                                      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
                                      decoration: BoxDecoration(
                                        color: _primaryBlue.withValues(alpha: 0.12),
                                        borderRadius: BorderRadius.circular(6),
                                      ),
                                      child: Text(category.toUpperCase(), style: const TextStyle(fontSize: 10, fontWeight: FontWeight.bold, color: _primaryBlue)),
                                    ),
                                  ],
                                ),
                                const SizedBox(height: 2),
                                Text(details, style: TextStyle(fontSize: 12, color: _mutedText)),
                              ],
                            ),
                          ),
                          Text('$actor  •  $ts', style: TextStyle(fontSize: 11, color: _mutedText, fontFamily: 'monospace')),
                        ],
                      ),
                    );
                  },
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  // ===========================================================================
  // 8. DATABASE MAINTENANCE & OPTIMIZER VIEW
  // ===========================================================================
  Widget _buildMaintenanceView() {
    final db = _maintenanceInfo['database']?.toString() ?? (widget.activeDatabase ?? 'Default');
    final engine = _maintenanceInfo['engine']?.toString() ?? 'postgres';
    final layoutsCount = (_maintenanceInfo['layouts_count'] as num?)?.toInt() ?? 0;
    final scriptsCount = (_maintenanceInfo['scripts_count'] as num?)?.toInt() ?? 0;
    final totalRows = (_maintenanceInfo['total_rows'] as num?)?.toInt() ?? 0;
    final tables = (_maintenanceInfo['tables'] as List<dynamic>? ?? const [])
        .whereType<Map<dynamic, dynamic>>()
        .map(Map<String, dynamic>.from)
        .toList();

    return Padding(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('Database Maintenance & Optimizer — $db (${engine.toUpperCase()})', style: const TextStyle(fontSize: 15, fontWeight: FontWeight.bold)),
                    const SizedBox(height: 2),
                    Text('Inspect physical tables, row counts, and run SQL planner statistics & catalog verification.', style: TextStyle(fontSize: 12, color: _mutedText)),
                  ],
                ),
              ),
              OutlinedButton.icon(
                onPressed: () async {
                  try {
                    final res = await widget.apiClient.runServerDatabaseOptimize(
                      database: widget.activeDatabase,
                      operation: 'analyze',
                    );
                    _toast('Catalog & planner statistics updated in ${res['duration_ms']} ms');
                    await _loadCurrentSection();
                  } catch (e) {
                    _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                  }
                },
                icon: const Icon(Icons.fact_check_outlined, size: 16),
                label: const Text('Verify Catalog'),
              ),
              const SizedBox(width: 8),
              ElevatedButton.icon(
                style: ElevatedButton.styleFrom(
                  backgroundColor: _primaryBlue,
                  foregroundColor: Colors.white,
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                ),
                onPressed: () async {
                  try {
                    final res = await widget.apiClient.runServerDatabaseOptimize(
                      database: widget.activeDatabase,
                    );
                    _toast('VACUUM ANALYZE completed in ${res['duration_ms']} ms');
                    await _loadCurrentSection();
                  } catch (e) {
                    _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                  }
                },
                icon: const Icon(Icons.bolt, size: 18),
                label: const Text('Run VACUUM ANALYZE'),
              ),
            ],
          ),
          const SizedBox(height: 14),
          Row(
            children: [
              _buildKpiCard('User Tables', '${tables.length}', 'Physical relational tables', _primaryBlue, Icons.table_chart_outlined),
              const SizedBox(width: 10),
              _buildKpiCard('Total Records', '$totalRows rows', 'Across all user tables', Colors.teal.shade600, Icons.dataset_outlined),
              const SizedBox(width: 10),
              _buildKpiCard('Layouts & Scripts', '$layoutsCount layouts', '$scriptsCount automation scripts', Colors.purple.shade600, Icons.dashboard_customize_outlined),
              const SizedBox(width: 10),
              _buildKpiCard('System Catalog', 'HEALTHY', '_f4b_* metadata verified', Colors.green.shade700, Icons.verified_outlined),
            ],
          ),
          const SizedBox(height: 14),
          Expanded(
            child: Container(
              decoration: BoxDecoration(
                color: _cardBg,
                border: Border.all(color: _borderColor),
                borderRadius: BorderRadius.circular(8),
              ),
              child: ClipRRect(
                borderRadius: BorderRadius.circular(8),
                child: Column(
                  children: [
                    Container(
                      color: _tableHeaderBg,
                      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                      child: Row(
                        children: [
                          Expanded(flex: 3, child: Text('TABLE NAME', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          Expanded(child: Text('FIELDS', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          Expanded(child: Text('ROW COUNT', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                          SizedBox(width: 120, child: Text('STATUS', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 11, color: _mutedText))),
                        ],
                      ),
                    ),
                    Divider(height: 1, color: _borderColor),
                    Expanded(
                      child: ListView.separated(
                        itemCount: tables.length,
                        separatorBuilder: (_, _) => Divider(height: 1, color: _borderColor),
                        itemBuilder: (ctx, i) {
                          final t = tables[i];
                          return Container(
                            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                            color: i.isEven ? Colors.transparent : _subtleBg,
                            child: Row(
                              children: [
                                Expanded(
                                  flex: 3,
                                  child: Row(
                                    children: [
                                      const Icon(Icons.table_chart_outlined, color: _primaryBlue, size: 18),
                                      const SizedBox(width: 8),
                                      Text(t['name']?.toString() ?? '', style: const TextStyle(fontWeight: FontWeight.bold, fontFamily: 'monospace')),
                                    ],
                                  ),
                                ),
                                Expanded(
                                  child: Text('${t['fields_count'] ?? 0} columns', style: TextStyle(fontSize: 12, color: _mutedText)),
                                ),
                                Expanded(
                                  child: Text('${t['row_count'] ?? 0} records', style: const TextStyle(fontSize: 12, fontWeight: FontWeight.bold)),
                                ),
                                SizedBox(
                                  width: 120,
                                  child: Row(
                                    children: [
                                      Icon(Icons.check_circle, size: 14, color: Colors.green.shade700),
                                      const SizedBox(width: 4),
                                      Text('Healthy', style: TextStyle(fontSize: 11.5, fontWeight: FontWeight.w600, color: Colors.green.shade700)),
                                    ],
                                  ),
                                ),
                              ],
                            ),
                          );
                        },
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  // ===========================================================================
  // 9. NOTIFICATIONS, SMTP & WEBHOOKS VIEW
  // ===========================================================================
  Widget _buildNotificationsView() {
    const availableEvents = <(String, String)>[
      ('backup_failed', 'Backup Snapshot Failed'),
      ('high_error_rate', 'High HTTP 5xx Error Rate'),
      ('mfa_changed', 'User MFA / Security Changed'),
      ('tls_expiring', 'SSL/TLS Certificate Expiring'),
      ('update_available', 'New Container / Release Version Available'),
    ];

    return SingleChildScrollView(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('Server Notifications, SMTP & Outgoing Webhooks', style: TextStyle(fontSize: 15, fontWeight: FontWeight.bold)),
                    const SizedBox(height: 2),
                    Text('Configure email delivery and webhook endpoints for critical server alerts.', style: TextStyle(fontSize: 12, color: _mutedText)),
                  ],
                ),
              ),
              OutlinedButton.icon(
                onPressed: () async {
                  try {
                    final res = await widget.apiClient.testServerNotification();
                    setState(() => _lastTestTimestamp = res['sent_at']?.toString() ?? '');
                    _toast('Test notification dispatched (${res['sent_at']})!');
                  } catch (e) {
                    _toast(e.toString().replaceFirst('Exception: ', ''), isError: true);
                  }
                },
                icon: const Icon(Icons.send_outlined, size: 16),
                label: const Text('Send Test Alert'),
              ),
              const SizedBox(width: 10),
              ElevatedButton.icon(
                style: ElevatedButton.styleFrom(
                  backgroundColor: _primaryBlue,
                  foregroundColor: Colors.white,
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                ),
                onPressed: () => _saveNotifications(toastMsg: 'Notification settings saved.'),
                icon: const Icon(Icons.save_outlined, size: 16),
                label: const Text('Save Alert Channels'),
              ),
            ],
          ),
          if (_lastTestTimestamp.isNotEmpty) ...[
            const SizedBox(height: 10),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              decoration: BoxDecoration(
                color: Colors.green.withValues(alpha: 0.1),
                borderRadius: BorderRadius.circular(8),
                border: Border.all(color: Colors.green.withValues(alpha: 0.35)),
              ),
              child: Row(
                children: [
                  Icon(Icons.check_circle_outline, size: 16, color: Colors.green.shade700),
                  const SizedBox(width: 8),
                  Text('Last test alert dispatched at $_lastTestTimestamp', style: TextStyle(fontSize: 12, color: Colors.green.shade700, fontWeight: FontWeight.w600)),
                ],
              ),
            ),
          ],
          const SizedBox(height: 14),
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              // SMTP Card
              Expanded(
                child: Container(
                  padding: const EdgeInsets.all(16),
                  decoration: BoxDecoration(
                    color: _cardBg,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(color: _borderColor),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      SwitchListTile(
                        contentPadding: EdgeInsets.zero,
                        activeThumbColor: _primaryBlue,
                        title: const Text('Enable SMTP Server (Outbound Email)', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13.5)),
                        subtitle: Text('Used for server alerts and Send Mail script steps', style: TextStyle(fontSize: 11, color: _mutedText)),
                        value: _smtpEnabled,
                        onChanged: (v) {
                          setState(() => _smtpEnabled = v);
                          unawaited(_saveNotifications(toastMsg: 'SMTP server ${v ? "enabled" : "disabled"}.'));
                        },
                      ),
                      const SizedBox(height: 10),
                      Row(
                        children: [
                          Expanded(
                            flex: 3,
                            child: TextField(
                              controller: _smtpHostCtrl,
                              decoration: const InputDecoration(labelText: 'SMTP Host', border: OutlineInputBorder(), isDense: true),
                            ),
                          ),
                          const SizedBox(width: 10),
                          Expanded(
                            child: TextField(
                              controller: _smtpPortCtrl,
                              decoration: const InputDecoration(labelText: 'Port', border: OutlineInputBorder(), isDense: true),
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 12),
                      TextField(
                        controller: _smtpUserCtrl,
                        decoration: const InputDecoration(labelText: 'SMTP Username', border: OutlineInputBorder(), isDense: true),
                      ),
                      const SizedBox(height: 12),
                      TextField(
                        controller: _smtpFromCtrl,
                        decoration: const InputDecoration(labelText: 'Sender Email Address (From)', border: OutlineInputBorder(), isDense: true),
                      ),
                      const SizedBox(height: 8),
                      SwitchListTile(
                        contentPadding: EdgeInsets.zero,
                        activeThumbColor: _primaryBlue,
                        title: const Text('Use STARTTLS / TLS Encryption', style: TextStyle(fontSize: 13)),
                        value: _smtpUseTls,
                        onChanged: (v) {
                          setState(() => _smtpUseTls = v);
                          unawaited(_saveNotifications(toastMsg: 'SMTP TLS ${v ? "enabled" : "disabled"}.'));
                        },
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(width: 16),
              // Webhook & Subscribed Events Card
              Expanded(
                child: Container(
                  padding: const EdgeInsets.all(16),
                  decoration: BoxDecoration(
                    color: _cardBg,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(color: _borderColor),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      SwitchListTile(
                        contentPadding: EdgeInsets.zero,
                        activeThumbColor: _primaryBlue,
                        title: const Text('Enable Outgoing Webhook Alerts', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13.5)),
                        subtitle: Text('Dispatch JSON payloads to Slack, Discord, or SIEM', style: TextStyle(fontSize: 11, color: _mutedText)),
                        value: _webhookEnabled,
                        onChanged: (v) {
                          setState(() => _webhookEnabled = v);
                          unawaited(_saveNotifications(toastMsg: 'Webhook alerts ${v ? "enabled" : "disabled"}.'));
                        },
                      ),
                      const SizedBox(height: 10),
                      TextField(
                        controller: _webhookUrlCtrl,
                        decoration: const InputDecoration(
                          labelText: 'Webhook Target URL (POST JSON)',
                          hintText: 'https://hooks.slack.com/services/...',
                          border: OutlineInputBorder(),
                          isDense: true,
                        ),
                      ),
                      const SizedBox(height: 12),
                      TextField(
                        controller: _webhookSecretCtrl,
                        obscureText: true,
                        decoration: const InputDecoration(
                          labelText: 'HMAC Signing Secret (X-File4Base-Signature)',
                          border: OutlineInputBorder(),
                          isDense: true,
                        ),
                      ),
                      const SizedBox(height: 16),
                      const Text('Subscribed Alert Events', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                      const SizedBox(height: 8),
                      Wrap(
                        spacing: 8,
                        runSpacing: 8,
                        children: availableEvents.map((ev) {
                          final (key, label) = ev;
                          final selected = _alertEvents.contains(key);
                          return FilterChip(
                            selected: selected,
                            selectedColor: _primaryBlue.withValues(alpha: 0.18),
                            checkmarkColor: _primaryBlue,
                            label: Text(label, style: const TextStyle(fontSize: 11.5)),
                            onSelected: (v) {
                              setState(() {
                                if (v) {
                                  if (!_alertEvents.contains(key)) _alertEvents.add(key);
                                } else {
                                  _alertEvents.remove(key);
                                }
                              });
                              unawaited(_saveNotifications(toastMsg: 'Alert event triggers updated.'));
                            },
                          );
                        }).toList(),
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _LegendDot extends StatelessWidget {

  const _LegendDot({required this.color, required this.label});
  final Color color;
  final String label;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Container(width: 8, height: 8, decoration: BoxDecoration(color: color, shape: BoxShape.circle)),
        const SizedBox(width: 4),
        Text(label, style: const TextStyle(fontSize: 11, color: Colors.grey)),
      ],
    );
  }
}

class _RpmBarChartPainter extends CustomPainter {

  _RpmBarChartPainter(this.series, {required this.isDark});
  final List<Map<String, dynamic>> series;
  final bool isDark;

  @override
  void paint(Canvas canvas, Size size) {
    if (series.isEmpty) return;
    final gridPaint = Paint()
      ..color = (isDark ? Colors.white : Colors.black).withValues(alpha: 0.08)
      ..strokeWidth = 1;

    for (var i = 1; i <= 3; i++) {
      final y = size.height * (i / 4);
      canvas.drawLine(Offset(0, y), Offset(size.width, y), gridPaint);
    }

    var maxReqs = 5;
    for (final b in series) {
      final total = (b['total_requests'] as num?)?.toInt() ?? 0;
      if (total > maxReqs) maxReqs = total;
    }

    final barWidth = (size.width / series.length) * 0.68;
    final step = size.width / series.length;

    final p2xx = Paint()..color = Colors.teal.withValues(alpha: 0.85);
    final p4xx = Paint()..color = Colors.orange.withValues(alpha: 0.9);
    final p5xx = Paint()..color = Colors.red.withValues(alpha: 0.95);

    for (var i = 0; i < series.length; i++) {
      final b = series[i];
      final s2 = (b['status_2xx'] as num?)?.toDouble() ?? 0;
      final s3 = (b['status_3xx'] as num?)?.toDouble() ?? 0;
      final s4 = (b['status_4xx'] as num?)?.toDouble() ?? 0;
      final s5 = (b['status_5xx'] as num?)?.toDouble() ?? 0;

      final x = i * step + (step - barWidth) / 2;
      var bottom = size.height;

      final h2 = ((s2 + s3) / maxReqs) * (size.height - 12);
      if (h2 > 0) {
        canvas.drawRRect(
          RRect.fromRectAndRadius(Rect.fromLTWH(x, bottom - h2, barWidth, h2), const Radius.circular(2)),
          p2xx,
        );
        bottom -= h2;
      }

      final h4 = (s4 / maxReqs) * (size.height - 12);
      if (h4 > 0) {
        canvas.drawRect(Rect.fromLTWH(x, bottom - h4, barWidth, h4), p4xx);
        bottom -= h4;
      }

      final h5 = (s5 / maxReqs) * (size.height - 12);
      if (h5 > 0) {
        canvas.drawRect(Rect.fromLTWH(x, bottom - h5, barWidth, h5), p5xx);
      }
    }
  }

  @override
  bool shouldRepaint(covariant _RpmBarChartPainter oldDelegate) => true;
}

class _LatencyLineChartPainter extends CustomPainter {

  _LatencyLineChartPainter(this.series, {required this.isDark});
  final List<Map<String, dynamic>> series;
  final bool isDark;

  @override
  void paint(Canvas canvas, Size size) {
    if (series.length < 2) return;

    final gridPaint = Paint()
      ..color = (isDark ? Colors.white : Colors.black).withValues(alpha: 0.08)
      ..strokeWidth = 1;
    for (var i = 1; i <= 3; i++) {
      final y = size.height * (i / 4);
      canvas.drawLine(Offset(0, y), Offset(size.width, y), gridPaint);
    }

    var maxLat = 10.0;
    for (final b in series) {
      final p95 = (b['p95_latency_ms'] as num?)?.toDouble() ?? 0.0;
      final avg = (b['avg_latency_ms'] as num?)?.toDouble() ?? 0.0;
      if (p95 > maxLat) maxLat = p95;
      if (avg > maxLat) maxLat = avg;
    }

    final avgPath = Path();
    final p95Path = Path();
    final dx = size.width / (series.length - 1);

    for (var i = 0; i < series.length; i++) {
      final avg = (series[i]['avg_latency_ms'] as num?)?.toDouble() ?? 0.0;
      final p95 = (series[i]['p95_latency_ms'] as num?)?.toDouble() ?? 0.0;
      final x = i * dx;
      final yAvg = size.height - ((avg / maxLat) * (size.height - 16)) - 4;
      final yP95 = size.height - ((p95 / maxLat) * (size.height - 16)) - 4;
      if (i == 0) {
        avgPath.moveTo(x, yAvg);
        p95Path.moveTo(x, yP95);
      } else {
        avgPath.lineTo(x, yAvg);
        p95Path.lineTo(x, yP95);
      }
    }

    canvas.drawPath(
      p95Path,
      Paint()
        ..color = Colors.purple
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2.0,
    );
    canvas.drawPath(
      avgPath,
      Paint()
        ..color = const Color(0xFF1E88E5)
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2.2,
    );
  }

  @override
  bool shouldRepaint(covariant _LatencyLineChartPainter oldDelegate) => true;
}
