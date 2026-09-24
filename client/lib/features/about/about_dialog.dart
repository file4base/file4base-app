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

enum _AboutTab { main, info, credits }

class _AboutFile4BaseDialogState extends State<AboutFile4BaseDialog> {
  _AboutTab _tab = _AboutTab.main;

  @override
  Widget build(BuildContext context) {
    return Dialog(
      backgroundColor: Colors.transparent,
      child: Container(
        width: 520,
        decoration: BoxDecoration(
          color: const Color(0xFFF0EFE8), // Classic macOS/win98 dialog beige
          border: Border.all(color: const Color(0xFF888880), width: 1.5),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: 0.4),
              blurRadius: 20,
              offset: const Offset(4, 6),
            ),
          ],
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            // Title bar
            _buildTitleBar(context),
            // Body
            _buildBody(context),
            // Buttons row
            _buildButtonRow(context),
          ],
        ),
      ),
    );
  }

  // ─── Title Bar ─────────────────────────────────────────────────────────────

  Widget _buildTitleBar(BuildContext context) {
    return Container(
      height: 24,
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topCenter,
          end: Alignment.bottomCenter,
          colors: [Color(0xFF1E6FD9), Color(0xFF0F4FAF)],
        ),
      ),
      child: const Center(
        child: Text(
          'About File4Base',
          style: TextStyle(
            color: Colors.white,
            fontSize: 12,
            fontWeight: FontWeight.bold,
          ),
        ),
      ),
    );
  }

  // ─── Body ──────────────────────────────────────────────────────────────────

  Widget _buildBody(BuildContext context) {
    return Container(
      color: Colors.white,
      padding: const EdgeInsets.fromLTRB(24, 20, 24, 20),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // LEFT: Product name + info
          Expanded(
            child: _tab == _AboutTab.main
                ? _buildMainContent()
                : _tab == _AboutTab.info
                    ? _buildInfoContent()
                    : _buildCreditsContent(),
          ),
          const SizedBox(width: 24),
          // RIGHT: Logo / icon
          _buildLogoColumn(context),
        ],
      ),
    );
  }

  Widget _buildMainContent() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // Big product name
        const Text(
          'File4Base',
          style: TextStyle(
            fontSize: 36,
            fontWeight: FontWeight.w900,
            color: Color(0xFF1A1A1A),
            letterSpacing: -1,
            height: 1,
          ),
        ),
        const SizedBox(height: 2),
        RichText(
          text: const TextSpan(
            children: [
              TextSpan(
                text: 'Open ',
                style: TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.w700,
                  color: Color(0xFF555555),
                ),
              ),
              TextSpan(
                text: 'Source',
                style: TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.w700,
                  color: Color(0xFF1E88E5),
                ),
              ),
            ],
          ),
        ),
        const SizedBox(height: 16),
        const Text(
          'Version 0.4.4 (Alpha)',
          style: TextStyle(
            fontSize: 11,
            color: Color(0xFF555555),
          ),
        ),
        const SizedBox(height: 4),
        const Text(
          '©2024-2025 File4Base Project.',
          style: TextStyle(fontSize: 11, color: Color(0xFF555555)),
        ),
        const Text(
          'All Rights Reserved.',
          style: TextStyle(fontSize: 11, color: Color(0xFF555555)),
        ),
        const SizedBox(height: 14),
        const Text(
          'File4Base is an open-source relational\ndatabase engine for rapid application\ndevelopment. Licensed under GPL-3.0.',
          style: TextStyle(fontSize: 11, color: Color(0xFF333333), height: 1.5),
        ),
        const SizedBox(height: 10),
        const Text(
          'For more information, visit our repository\nat github.com/file4base.',
          style: TextStyle(fontSize: 11, color: Color(0xFF333333), height: 1.5),
        ),
      ],
    );
  }

  Widget _buildInfoContent() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text(
          'System Information',
          style: TextStyle(
              fontSize: 13, fontWeight: FontWeight.bold, color: Color(0xFF1A1A1A)),
        ),
        const SizedBox(height: 12),
        _infoRow('Version', '0.2.0 (Alpha)'),
        _infoRow('License', 'GNU GPL v3.0'),
        _infoRow('Backend', 'Go + chi router'),
        _infoRow('Database', widget.serverStatus),
        _infoRow('Frontend', 'Flutter 3.x (WebDirect)'),
        _infoRow('Platforms', 'macOS · Linux · Windows · Web'),
        _infoRow('Modes', 'Browse · Find · Layout · Preview'),
      ],
    );
  }

  Widget _buildCreditsContent() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: const [
        Text(
          'Credits',
          style: TextStyle(
              fontSize: 13, fontWeight: FontWeight.bold, color: Color(0xFF1A1A1A)),
        ),
        SizedBox(height: 12),
        Text(
          'Built with open-source technologies:\n'
          '• Flutter / Dart (Google)\n'
          '• Go standard library\n'
          '• PostgreSQL 16\n'
          '• chi HTTP router\n'
          '• pgx/v5 driver\n'
          '• Docker & Nginx',
          style: TextStyle(fontSize: 11, color: Color(0xFF333333), height: 1.7),
        ),
        SizedBox(height: 10),
        Text(
          'Inspired by classic rapid application\ndevelopment database tools.',
          style: TextStyle(fontSize: 11, color: Color(0xFF555555), height: 1.5),
        ),
      ],
    );
  }

  Widget _infoRow(String label, String value) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 90,
            child: Text(
              label,
              style: const TextStyle(
                  fontSize: 11,
                  fontWeight: FontWeight.w600,
                  color: Color(0xFF1E88E5)),
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: const TextStyle(fontSize: 11, color: Color(0xFF333333)),
            ),
          ),
        ],
      ),
    );
  }

  // ─── Logo Column (right side) ─────────────────────────────────────────────

  Widget _buildLogoColumn(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.end,
      children: [
        // Small logo top-right
        Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Image.asset(
              'assets/branding/file4base-icon-128.png',
              width: 22,
              height: 22,
              errorBuilder: (_, __, ___) => const Icon(
                Icons.table_chart,
                size: 20,
                color: Color(0xFF1E88E5),
              ),
            ),
            const SizedBox(width: 4),
            const Text(
              'File4Base',
              style: TextStyle(
                fontSize: 11,
                fontWeight: FontWeight.bold,
                color: Color(0xFF1E88E5),
              ),
            ),
          ],
        ),
        const SizedBox(height: 12),
        // Real File4Base branding icon
        ClipRRect(
          borderRadius: BorderRadius.circular(24),
          child: Image.asset(
            'assets/branding/file4base-icon-256.png',
            width: 140,
            height: 140,
            fit: BoxFit.contain,
            errorBuilder: (_, __, ___) => Container(
              width: 140,
              height: 140,
              decoration: BoxDecoration(
                color: const Color(0xFF0D1B3E),
                borderRadius: BorderRadius.circular(24),
              ),
              child: const Icon(
                Icons.folder_special,
                size: 72,
                color: Color(0xFF4FC3F7),
              ),
            ),
          ),
        ),
      ],
    );
  }

  // ─── Button Row ────────────────────────────────────────────────────────────

  Widget _buildButtonRow(BuildContext context) {
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 14),
      color: const Color(0xFFF0EFE8),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.end,
        children: [
          _classicButton(
            'Info',
            isActive: _tab == _AboutTab.info,
            onPressed: () => setState(() =>
                _tab = _tab == _AboutTab.info ? _AboutTab.main : _AboutTab.info),
          ),
          const SizedBox(width: 6),
          _classicButton(
            'Credits',
            isActive: _tab == _AboutTab.credits,
            onPressed: () => setState(() => _tab =
                _tab == _AboutTab.credits ? _AboutTab.main : _AboutTab.credits),
          ),
          const SizedBox(width: 6),
          _classicButton(
            'About',
            isActive: _tab == _AboutTab.main,
            onPressed: () => setState(() => _tab = _AboutTab.main),
          ),
          const SizedBox(width: 12),
          _classicButton(
            'OK',
            isDefault: true,
            onPressed: () => Navigator.of(context).pop(),
          ),
        ],
      ),
    );
  }

  Widget _classicButton(String label,
      {VoidCallback? onPressed, bool isDefault = false, bool isActive = false}) {
    return SizedBox(
      width: 72,
      height: 26,
      child: ElevatedButton(
        onPressed: onPressed,
        style: ElevatedButton.styleFrom(
          padding: EdgeInsets.zero,
          backgroundColor: isDefault
              ? const Color(0xFF1E88E5)
              : isActive
                  ? const Color(0xFFD0D8E8)
                  : const Color(0xFFF0EFE8),
          foregroundColor: isDefault ? Colors.white : const Color(0xFF1A1A1A),
          elevation: 2,
          side: BorderSide(
            color: isDefault
                ? const Color(0xFF0F4FAF)
                : const Color(0xFF888880),
            width: isDefault ? 2 : 1,
          ),
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(3)),
        ),
        child: Text(
          label,
          style: TextStyle(
            fontSize: 12,
            fontWeight: isDefault ? FontWeight.bold : FontWeight.normal,
          ),
        ),
      ),
    );
  }
}


