import 'package:flutter/material.dart';

class AboutFile4BaseDialog extends StatefulWidget {
  final String serverStatus;

  const AboutFile4BaseDialog({super.key, required this.serverStatus});

  static void show(BuildContext context, {required String serverStatus}) {
    showDialog(
      context: context,
      barrierColor: Colors.black54,
      builder: (context) => AboutFile4BaseDialog(serverStatus: serverStatus),
    );
  }

  @override
  State<AboutFile4BaseDialog> createState() => _AboutFile4BaseDialogState();
}

class _AboutFile4BaseDialogState extends State<AboutFile4BaseDialog>
    with SingleTickerProviderStateMixin {
  late TabController _tabController;

  static const String _version = '0.4.14';

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 3, vsync: this);
    _tabController.addListener(() {
      if (mounted) setState(() {});
    });
  }

  @override
  void dispose() {
    _tabController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return Dialog(
      backgroundColor: Colors.transparent,
      insetPadding: const EdgeInsets.symmetric(horizontal: 24, vertical: 32),
      child: Container(
        width: 580,
        decoration: BoxDecoration(
          color: colorScheme.surface,
          borderRadius: BorderRadius.circular(16),
          border: Border.all(
            color: theme.dividerColor.withValues(alpha: 0.15),
          ),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: 0.35),
              blurRadius: 28,
              offset: const Offset(0, 10),
            ),
          ],
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // ─── Header ───────────────────────────────────────────────────
            _buildHeader(context),

            // ─── Modern TabBar ───────────────────────────────────────────
            _buildTabBar(context),

            // ─── Tab Content ─────────────────────────────────────────────
            ConstrainedBox(
              constraints: const BoxConstraints(minHeight: 280, maxHeight: 340),
              child: TabBarView(
                controller: _tabController,
                children: [
                  _buildAboutTab(context),
                  _buildSystemInfoTab(context),
                  _buildCreditsTab(context),
                ],
              ),
            ),

            // ─── Modern Button Row ───────────────────────────────────────
            _buildButtonRow(context),
          ],
        ),
      ),
    );
  }

  // ─── Header ───────────────────────────────────────────────────────────────

  Widget _buildHeader(BuildContext context) {
    final theme = Theme.of(context);

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 22, vertical: 16),
      decoration: BoxDecoration(
        color: const Color(0xFF1E88E5).withValues(alpha: 0.10),
        borderRadius: const BorderRadius.vertical(top: Radius.circular(16)),
        border: Border(
          bottom: BorderSide(
            color: theme.dividerColor.withValues(alpha: 0.12),
          ),
        ),
      ),
      child: Row(
        children: [
          Container(
            padding: const EdgeInsets.all(8),
            decoration: BoxDecoration(
              color: const Color(0xFF1E88E5).withValues(alpha: 0.20),
              borderRadius: BorderRadius.circular(8),
            ),
            child: const Icon(
              Icons.info_outline,
              color: Color(0xFF1E88E5),
              size: 24,
            ),
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  'About File4Base',
                  style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
                ),
                Text(
                  'Version $_version (Alpha) · Relational Database Engine',
                  style: TextStyle(
                    fontSize: 12,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
          IconButton(
            onPressed: () => Navigator.of(context).pop(),
            icon: const Icon(Icons.close, size: 20),
            tooltip: 'Close',
          ),
        ],
      ),
    );
  }

  // ─── Tab Bar ──────────────────────────────────────────────────────────────

  Widget _buildTabBar(BuildContext context) {
    final theme = Theme.of(context);

    return Container(
      decoration: BoxDecoration(
        border: Border(
          bottom: BorderSide(
            color: theme.dividerColor.withValues(alpha: 0.12),
          ),
        ),
      ),
      child: TabBar(
        controller: _tabController,
        indicatorColor: const Color(0xFF1E88E5),
        indicatorWeight: 3,
        labelColor: const Color(0xFF1E88E5),
        unselectedLabelColor: theme.colorScheme.onSurfaceVariant,
        labelStyle: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13),
        unselectedLabelStyle: const TextStyle(fontWeight: FontWeight.normal, fontSize: 13),
        tabs: const [
          Tab(
            icon: Icon(Icons.apps_rounded, size: 18),
            text: 'About',
          ),
          Tab(
            icon: Icon(Icons.tune_rounded, size: 18),
            text: 'System Info',
          ),
          Tab(
            icon: Icon(Icons.groups_rounded, size: 18),
            text: 'Credits',
          ),
        ],
      ),
    );
  }

  // ─── Tab 1: About / Overview ──────────────────────────────────────────────

  Widget _buildAboutTab(BuildContext context) {
    final theme = Theme.of(context);

    return SingleChildScrollView(
      padding: const EdgeInsets.all(24),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Left: Info & Description
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Wrap(
                  crossAxisAlignment: WrapCrossAlignment.center,
                  spacing: 8,
                  runSpacing: 4,
                  children: [
                    const Text(
                      'File4Base',
                      style: TextStyle(
                        fontSize: 26,
                        fontWeight: FontWeight.w900,
                        letterSpacing: -0.5,
                      ),
                    ),
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                      decoration: BoxDecoration(
                        color: const Color(0xFF1E88E5).withValues(alpha: 0.12),
                        borderRadius: BorderRadius.circular(6),
                        border: Border.all(
                          color: const Color(0xFF1E88E5).withValues(alpha: 0.3),
                        ),
                      ),
                      child: const Text(
                        'Open Source',
                        style: TextStyle(
                          fontSize: 11,
                          fontWeight: FontWeight.w700,
                          color: Color(0xFF1E88E5),
                        ),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 6),
                Text(
                  'Version $_version (Alpha)',
                  style: TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(height: 2),
                Text(
                  '© 2024-2026 File4Base Project · All Rights Reserved.',
                  style: TextStyle(
                    fontSize: 11,
                    color: theme.colorScheme.onSurfaceVariant.withValues(alpha: 0.8),
                  ),
                ),
                const SizedBox(height: 14),
                Container(
                  padding: const EdgeInsets.all(12),
                  decoration: BoxDecoration(
                    color: theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.3),
                    borderRadius: BorderRadius.circular(8),
                    border: Border.all(
                      color: theme.dividerColor.withValues(alpha: 0.12),
                    ),
                  ),
                  child: Text(
                    'File4Base is an open-source relational database engine '
                    'for rapid application development (RAD). '
                    'Licensed under GNU General Public License v3.0 (GPL-3.0).',
                    style: TextStyle(
                      fontSize: 12,
                      height: 1.5,
                      color: theme.colorScheme.onSurface,
                    ),
                  ),
                ),
                const SizedBox(height: 12),
                Row(
                  children: [
                    const Icon(
                      Icons.code_rounded,
                      size: 15,
                      color: Color(0xFF1E88E5),
                    ),
                    const SizedBox(width: 6),
                    Flexible(
                      child: Text(
                        'github.com/file4base/file4base-app',
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 11,
                          color: theme.colorScheme.onSurfaceVariant,
                          decoration: TextDecoration.underline,
                        ),
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
          const SizedBox(width: 24),
          // Right: Logo Card
          _buildLogoCard(context),
        ],
      ),
    );
  }

  // ─── Tab 2: System Info ───────────────────────────────────────────────────

  Widget _buildSystemInfoTab(BuildContext context) {
    return SingleChildScrollView(
      padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _infoTile(
            context,
            icon: Icons.tag_rounded,
            label: 'Version',
            value: '$_version (Alpha)',
          ),
          _infoTile(
            context,
            icon: Icons.gavel_rounded,
            label: 'License',
            value: 'GNU General Public License v3.0 (GPL-3.0)',
          ),
          _infoTile(
            context,
            icon: Icons.dns_rounded,
            label: 'Backend Engine',
            value: 'Go + chi HTTP router (pgx/v5 driver)',
          ),
          _infoTile(
            context,
            icon: Icons.storage_rounded,
            label: 'Database Status',
            value: widget.serverStatus,
            valueColor: widget.serverStatus.toLowerCase().contains('connect') ||
                    widget.serverStatus.toLowerCase().contains('ok')
                ? Colors.green.shade600
                : null,
          ),
          _infoTile(
            context,
            icon: Icons.web_rounded,
            label: 'Frontend Engine',
            value: 'Flutter 3.x (WebDirect & Desktop Native)',
          ),
          _infoTile(
            context,
            icon: Icons.devices_rounded,
            label: 'Platforms',
            value: 'macOS · Linux · Windows · Web Browser',
          ),
          _infoTile(
            context,
            icon: Icons.dashboard_customize_rounded,
            label: 'App Modes',
            value: 'Browse · Find · Layout · Preview',
          ),
        ],
      ),
    );
  }

  Widget _infoTile(
    BuildContext context, {
    required IconData icon,
    required String label,
    required String value,
    Color? valueColor,
  }) {
    final theme = Theme.of(context);

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5.0),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        decoration: BoxDecoration(
          color: theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.25),
          borderRadius: BorderRadius.circular(8),
          border: Border.all(
            color: theme.dividerColor.withValues(alpha: 0.08),
          ),
        ),
        child: Row(
          children: [
            Icon(icon, size: 16, color: const Color(0xFF1E88E5)),
            const SizedBox(width: 10),
            SizedBox(
              width: 120,
              child: Text(
                label,
                style: const TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w600,
                  color: Color(0xFF1E88E5),
                ),
              ),
            ),
            Expanded(
              child: Text(
                value,
                style: TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w500,
                  color: valueColor ?? theme.colorScheme.onSurface,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  // ─── Tab 3: Credits ───────────────────────────────────────────────────────

  Widget _buildCreditsTab(BuildContext context) {
    final theme = Theme.of(context);

    return SingleChildScrollView(
      padding: const EdgeInsets.all(22),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Built with Open Source Technologies',
            style: TextStyle(fontSize: 13, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 12),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _techBadge(context, Icons.flutter_dash, 'Flutter / Dart', 'Google'),
              _techBadge(context, Icons.terminal, 'Go Standard Library', 'Golang'),
              _techBadge(context, Icons.storage, 'PostgreSQL 16 & MariaDB', 'Database Engine'),
              _techBadge(context, Icons.router, 'chi HTTP Router', 'REST API Router'),
              _techBadge(context, Icons.bolt, 'pgx/v5 Driver', 'PostgreSQL Connectivity'),
              _techBadge(context, Icons.view_in_ar, 'Docker & Nginx', 'Container Runtime'),
            ],
          ),
          const SizedBox(height: 18),
          Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: const Color(0xFF1E88E5).withValues(alpha: 0.08),
              borderRadius: BorderRadius.circular(8),
              border: Border.all(
                color: const Color(0xFF1E88E5).withValues(alpha: 0.2),
              ),
            ),
            child: Row(
              children: [
                const Icon(
                  Icons.lightbulb_outline_rounded,
                  color: Color(0xFF1E88E5),
                  size: 20,
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    'Inspired by classic rapid application development (RAD) '
                    'desktop databases, rebuilt for the modern web and cloud.',
                    style: TextStyle(
                      fontSize: 12,
                      color: theme.colorScheme.onSurfaceVariant,
                      height: 1.4,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _techBadge(BuildContext context, IconData icon, String name, String subtitle) {
    final theme = Theme.of(context);

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.4),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(
          color: theme.dividerColor.withValues(alpha: 0.12),
        ),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 16, color: const Color(0xFF1E88E5)),
          const SizedBox(width: 8),
          Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(
                name,
                style: const TextStyle(fontSize: 12, fontWeight: FontWeight.bold),
              ),
              Text(
                subtitle,
                style: TextStyle(
                  fontSize: 10,
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  // ─── Right Logo Card ──────────────────────────────────────────────────────

  Widget _buildLogoCard(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      children: [
        Container(
          width: 130,
          height: 130,
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.4),
            borderRadius: BorderRadius.circular(20),
            border: Border.all(
              color: theme.dividerColor.withValues(alpha: 0.15),
            ),
            boxShadow: [
              BoxShadow(
                color: Colors.black.withValues(alpha: 0.08),
                blurRadius: 16,
                offset: const Offset(0, 4),
              ),
            ],
          ),
          child: ClipRRect(
            borderRadius: BorderRadius.circular(14),
            child: Image.asset(
              'assets/branding/file4base-icon-256.png',
              fit: BoxFit.contain,
              errorBuilder: (_, __, ___) => const Icon(
                Icons.folder_special,
                size: 64,
                color: Color(0xFF1E88E5),
              ),
            ),
          ),
        ),
        const SizedBox(height: 8),
        Text(
          'File4Base',
          style: TextStyle(
            fontSize: 11,
            fontWeight: FontWeight.bold,
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      ],
    );
  }

  // ─── Modern Buttons Row ───────────────────────────────────────────────────

  Widget _buildButtonRow(BuildContext context) {
    final theme = Theme.of(context);
    final activeIndex = _tabController.index;

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.35),
        borderRadius: const BorderRadius.vertical(bottom: Radius.circular(16)),
        border: Border(
          top: BorderSide(
            color: theme.dividerColor.withValues(alpha: 0.12),
          ),
        ),
      ),
      child: Row(
        children: [
          // Left side: Version tag
          Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(
                Icons.commit_rounded,
                size: 14,
                color: Color(0xFF1E88E5),
              ),
              const SizedBox(width: 4),
              Text(
                'v$_version',
                style: TextStyle(
                  fontSize: 11,
                  fontWeight: FontWeight.w600,
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
          const Spacer(),
          // Right side: Tab buttons + OK
          Wrap(
            spacing: 6,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              _tabSwitchButton(
                label: 'About',
                isActive: activeIndex == 0,
                onPressed: () => _tabController.animateTo(0),
              ),
              _tabSwitchButton(
                label: 'Info',
                isActive: activeIndex == 1,
                onPressed: () => _tabController.animateTo(1),
              ),
              _tabSwitchButton(
                label: 'Credits',
                isActive: activeIndex == 2,
                onPressed: () => _tabController.animateTo(2),
              ),
              const SizedBox(width: 4),
              FilledButton.icon(
                onPressed: () => Navigator.of(context).pop(),
                icon: const Icon(Icons.check_rounded, size: 16),
                label: const Text('OK'),
                style: FilledButton.styleFrom(
                  backgroundColor: const Color(0xFF1E88E5),
                  foregroundColor: Colors.white,
                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                  shape: RoundedRectangleBorder(
                    borderRadius: BorderRadius.circular(8),
                  ),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _tabSwitchButton({
    required String label,
    required bool isActive,
    required VoidCallback onPressed,
  }) {
    if (isActive) {
      return FilledButton.tonal(
        onPressed: onPressed,
        style: FilledButton.styleFrom(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(8),
          ),
        ),
        child: Text(
          label,
          style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 12),
        ),
      );
    }

    return OutlinedButton(
      onPressed: onPressed,
      style: OutlinedButton.styleFrom(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(8),
        ),
      ),
      child: Text(
        label,
        style: const TextStyle(fontSize: 12),
      ),
    );
  }
}
