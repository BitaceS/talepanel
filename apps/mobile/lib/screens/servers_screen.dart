import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../theme.dart';
import 'dashboard_screen.dart' show serversProvider;

class ServersScreen extends ConsumerWidget {
  const ServersScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final serversAsync = ref.watch(serversProvider);

    return Scaffold(
      backgroundColor: TalePanelColors.background,
      appBar: AppBar(title: const Text('Servers')),
      body: serversAsync.when(
        loading: () => const Center(child: CircularProgressIndicator(color: TalePanelColors.primary)),
        error: (e, _) => Center(child: Text(e.toString(), style: const TextStyle(color: TalePanelColors.danger))),
        data: (servers) => ListView.separated(
          padding: const EdgeInsets.all(16),
          itemCount: servers.length,
          separatorBuilder: (_, __) => const SizedBox(height: 8),
          itemBuilder: (context, i) {
            final s = servers[i];
            return ListTile(
              tileColor: TalePanelColors.surface,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(10),
                side: const BorderSide(color: TalePanelColors.border),
              ),
              leading: Icon(
                Icons.circle,
                size: 10,
                color: s.isRunning ? TalePanelColors.success : s.isCrashed ? TalePanelColors.danger : TalePanelColors.textMuted,
              ),
              title: Text(s.name, style: const TextStyle(color: TalePanelColors.textPrimary)),
              subtitle: Text(s.status, style: const TextStyle(color: TalePanelColors.textMuted, fontSize: 12)),
              trailing: const Icon(Icons.chevron_right, color: TalePanelColors.textMuted),
              onTap: () => context.go('/servers/${s.id}'),
            );
          },
        ),
      ),
    );
  }
}
