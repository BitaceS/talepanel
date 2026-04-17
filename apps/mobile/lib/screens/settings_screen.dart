import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../theme.dart';
import '../services/api_service.dart';

class SettingsScreen extends ConsumerWidget {
  const SettingsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final api = ref.read(apiServiceProvider);

    return Scaffold(
      backgroundColor: TalePanelColors.background,
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _SectionHeader(label: 'Connection'),
          _SettingsTile(
            icon: Icons.link,
            label: 'Panel URL',
            subtitle: api.baseUrl,
            onTap: () {},
          ),

          const SizedBox(height: 8),
          _SectionHeader(label: 'Account'),
          _SettingsTile(
            icon: Icons.logout,
            label: 'Sign out',
            color: TalePanelColors.danger,
            onTap: () async {
              await api.logout();
              if (context.mounted) context.go('/login');
            },
          ),

          const SizedBox(height: 24),
          const Center(
            child: Text(
              'TalePanel Mobile v0.1.0\nby Tyraxo',
              style: TextStyle(color: TalePanelColors.textMuted, fontSize: 12),
              textAlign: TextAlign.center,
            ),
          ),
        ],
      ),
    );
  }
}

class _SectionHeader extends StatelessWidget {
  final String label;
  const _SectionHeader({required this.label});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 8, top: 16),
      child: Text(
        label.toUpperCase(),
        style: const TextStyle(
          color: TalePanelColors.textMuted,
          fontSize: 11,
          fontWeight: FontWeight.w600,
          letterSpacing: 0.8,
        ),
      ),
    );
  }
}

class _SettingsTile extends StatelessWidget {
  final IconData icon;
  final String label;
  final String? subtitle;
  final Color? color;
  final VoidCallback onTap;

  const _SettingsTile({
    required this.icon,
    required this.label,
    this.subtitle,
    this.color,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final c = color ?? TalePanelColors.textPrimary;
    return ListTile(
      tileColor: TalePanelColors.surface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(10),
        side: const BorderSide(color: TalePanelColors.border),
      ),
      leading: Icon(icon, color: c, size: 20),
      title: Text(label, style: TextStyle(color: c)),
      subtitle: subtitle != null ? Text(subtitle!, style: const TextStyle(color: TalePanelColors.textMuted, fontSize: 12)) : null,
      trailing: const Icon(Icons.chevron_right, color: TalePanelColors.textMuted, size: 16),
      onTap: onTap,
    );
  }
}
