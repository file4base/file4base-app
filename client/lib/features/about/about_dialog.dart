import 'package:flutter/material.dart';

class AboutFile4BaseDialog extends StatelessWidget {
  final String serverStatus;

  const AboutFile4BaseDialog({super.key, required this.serverStatus});

  static void show(BuildContext context, {required String serverStatus}) {
    showDialog(
      context: context,
      builder: (context) => AboutFile4BaseDialog(serverStatus: serverStatus),
    );
  }

  @override
  Widget build(BuildContext context) {
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final logoAsset = isDark
        ? 'assets/branding/file4base-dark.png'
        : 'assets/branding/file4base-light.png';

    return AlertDialog(
      title: Row(
        children: [
          ClipRRect(
            borderRadius: BorderRadius.circular(8),
            child: Image.asset(
              logoAsset,
              width: 36,
              height: 36,
              fit: BoxFit.cover,
              errorBuilder: (_, __, ___) => Image.asset(
                'assets/branding/file4base-icon.png',
                width: 36,
                height: 36,
                errorBuilder: (_, __, ___) => const Icon(Icons.table_chart, size: 32, color: Colors.blue),
              ),
            ),
          ),
          const SizedBox(width: 12),
          const Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('File4Base', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 18)),
              Text('Version 1.0.0 (Alpha)', style: TextStyle(fontSize: 12, color: Colors.grey)),
            ],
          ),
        ],
      ),
      content: SizedBox(
        width: 440,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Modern, open-source rapid application development database engine inspired by FileMaker Pro.',
              style: TextStyle(fontSize: 13, height: 1.4),
            ),
            const SizedBox(height: 16),
            const Divider(),
            const SizedBox(height: 8),
            _buildInfoRow(context, 'License', 'GNU General Public License v3.0 (GPL-3.0)'),
            _buildInfoRow(context, 'Database Engine', serverStatus),
            _buildInfoRow(context, 'Target Platforms', 'macOS (M1/M2/M3 & Intel), Linux, Windows, Web'),
            _buildInfoRow(context, 'Core Modes', 'Browse (⌘B), Find (⌘F), Layout (⌘L), Preview (⌘U)'),
            const SizedBox(height: 16),
            Center(
              child: Image.asset(
                logoAsset,
                width: 120,
                height: 120,
                fit: BoxFit.contain,
                errorBuilder: (_, __, ___) => const SizedBox.shrink(),
              ),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Close'),
        ),
      ],
    );
  }

  Widget _buildInfoRow(BuildContext context, String label, String value) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4.0),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 130,
            child: Text(
              label,
              style: TextStyle(
                fontWeight: FontWeight.w600,
                fontSize: 12,
                color: Theme.of(context).colorScheme.primary,
              ),
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: const TextStyle(fontSize: 12),
            ),
          ),
        ],
      ),
    );
  }
}
