import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../theme.dart';
import '../services/api_service.dart';

final serverDetailProvider = FutureProvider.family<Server, String>((ref, id) async {
  final api = ref.read(apiServiceProvider);
  return api.getServer(id);
});

class ServerDetailScreen extends ConsumerWidget {
  final String serverId;

  const ServerDetailScreen({super.key, required this.serverId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final serverAsync = ref.watch(serverDetailProvider(serverId));

    return Scaffold(
      backgroundColor: TalePanelColors.background,
      appBar: AppBar(
        title: serverAsync.maybeWhen(
          data: (s) => Text(s.name),
          orElse: () => const Text('Server'),
        ),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh),
            onPressed: () => ref.refresh(serverDetailProvider(serverId)),
          ),
        ],
      ),
      body: serverAsync.when(
        loading: () => const Center(child: CircularProgressIndicator(color: TalePanelColors.primary)),
        error: (e, _) => Center(child: Text(e.toString())),
        data: (server) => _ServerDetail(server: server, ref: ref),
      ),
    );
  }
}

class _ServerDetail extends StatelessWidget {
  final Server server;
  final WidgetRef ref;

  const _ServerDetail({required this.server, required this.ref});

  Future<void> _action(BuildContext context, Future<void> Function() fn, String label) async {
    try {
      await fn();
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('$label sent'), backgroundColor: TalePanelColors.success),
        );
        ref.refresh(serverDetailProvider(server.id));
      }
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed: $e'), backgroundColor: TalePanelColors.danger),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final api = ref.read(apiServiceProvider);
    final isRunning = server.isRunning;

    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        // Status card
        Container(
          padding: const EdgeInsets.all(16),
          decoration: BoxDecoration(
            color: TalePanelColors.surface,
            borderRadius: BorderRadius.circular(12),
            border: Border.all(color: TalePanelColors.border),
          ),
          child: Row(
            children: [
              Icon(
                Icons.circle,
                size: 10,
                color: isRunning ? TalePanelColors.success : server.isCrashed ? TalePanelColors.danger : TalePanelColors.textMuted,
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(server.name, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 18, color: TalePanelColors.textPrimary)),
                    Text(server.status.toUpperCase(), style: const TextStyle(fontSize: 11, color: TalePanelColors.textMuted, letterSpacing: 0.8)),
                  ],
                ),
              ),
            ],
          ),
        ),

        const SizedBox(height: 16),

        // Action buttons
        Row(
          children: [
            if (!isRunning)
              Expanded(
                child: _ActionButton(
                  label: 'Start',
                  color: TalePanelColors.success,
                  icon: Icons.play_arrow,
                  onPressed: () => _action(context, () => api.startServer(server.id), 'Start'),
                ),
              ),
            if (isRunning) ...[
              Expanded(
                child: _ActionButton(
                  label: 'Stop',
                  color: TalePanelColors.warning,
                  icon: Icons.stop,
                  onPressed: () => _action(context, () => api.stopServer(server.id), 'Stop'),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: _ActionButton(
                  label: 'Restart',
                  color: TalePanelColors.primary,
                  icon: Icons.restart_alt,
                  onPressed: () => _action(context, () => api.restartServer(server.id), 'Restart'),
                ),
              ),
            ],
            const SizedBox(width: 8),
            _ActionButton(
              label: 'Kill',
              color: TalePanelColors.danger,
              icon: Icons.dangerous_outlined,
              onPressed: () => _action(context, () => api.killServer(server.id), 'Kill'),
            ),
          ],
        ),

        const SizedBox(height: 16),

        // Info table
        Container(
          padding: const EdgeInsets.all(16),
          decoration: BoxDecoration(
            color: TalePanelColors.surface,
            borderRadius: BorderRadius.circular(12),
            border: Border.all(color: TalePanelColors.border),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('Info', style: TextStyle(fontWeight: FontWeight.w600, color: TalePanelColors.textPrimary)),
              const SizedBox(height: 12),
              _InfoRow(label: 'Version', value: server.hytaleVersion),
              _InfoRow(label: 'Port', value: ':${server.port}'),
              _InfoRow(label: 'Auto-restart', value: server.autoRestart ? 'Enabled' : 'Disabled'),
              if (server.ramLimitMb != null) _InfoRow(label: 'RAM Limit', value: '${server.ramLimitMb} MB'),
              if (server.activeWorld != null) _InfoRow(label: 'Active World', value: server.activeWorld!),
            ],
          ),
        ),
      ],
    );
  }
}

class _ActionButton extends StatelessWidget {
  final String label;
  final Color color;
  final IconData icon;
  final VoidCallback onPressed;

  const _ActionButton({
    required this.label,
    required this.color,
    required this.icon,
    required this.onPressed,
  });

  @override
  Widget build(BuildContext context) {
    return ElevatedButton.icon(
      style: ElevatedButton.styleFrom(
        backgroundColor: color.withOpacity(0.15),
        foregroundColor: color,
        side: BorderSide(color: color.withOpacity(0.4)),
        minimumSize: const Size(0, 44),
        padding: const EdgeInsets.symmetric(horizontal: 12),
      ),
      icon: Icon(icon, size: 16),
      label: Text(label),
      onPressed: onPressed,
    );
  }
}

class _InfoRow extends StatelessWidget {
  final String label;
  final String value;

  const _InfoRow({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Text(label, style: const TextStyle(color: TalePanelColors.textMuted, fontSize: 13)),
          Text(value, style: const TextStyle(color: TalePanelColors.textPrimary, fontSize: 13)),
        ],
      ),
    );
  }
}
