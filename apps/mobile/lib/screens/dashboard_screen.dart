import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../theme.dart';
import '../services/api_service.dart';

// ─── State ────────────────────────────────────────────────────────────────────

final serversProvider = FutureProvider<List<Server>>((ref) async {
  final api = ref.read(apiServiceProvider);
  return api.getServers();
});

// ─── Screen ───────────────────────────────────────────────────────────────────

class DashboardScreen extends ConsumerWidget {
  const DashboardScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final serversAsync = ref.watch(serversProvider);

    return Scaffold(
      backgroundColor: TalePanelColors.background,
      appBar: AppBar(
        title: const Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Dashboard'),
            Text(
              'TalePanel',
              style: TextStyle(fontSize: 12, color: TalePanelColors.textMuted),
            ),
          ],
        ),
        actions: [
          IconButton(
            icon: const Icon(Icons.notifications_outlined),
            onPressed: () {},
          ),
          IconButton(
            icon: const Icon(Icons.refresh),
            onPressed: () => ref.refresh(serversProvider),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: () async => ref.refresh(serversProvider),
        color: TalePanelColors.primary,
        child: serversAsync.when(
          loading: () => const _LoadingState(),
          error: (err, _) => _ErrorState(error: err.toString()),
          data: (servers) => _DashboardContent(servers: servers),
        ),
      ),
    );
  }
}

// ─── Dashboard content ────────────────────────────────────────────────────────

class _DashboardContent extends StatelessWidget {
  final List<Server> servers;

  const _DashboardContent({required this.servers});

  @override
  Widget build(BuildContext context) {
    final online = servers.where((s) => s.isRunning).length;
    final crashed = servers.where((s) => s.isCrashed).length;

    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        // Stats row
        Row(
          children: [
            Expanded(child: _StatCard(label: 'Total', value: '${servers.length}', icon: Icons.dns)),
            const SizedBox(width: 12),
            Expanded(
              child: _StatCard(
                label: 'Online',
                value: '$online',
                icon: Icons.circle,
                iconColor: TalePanelColors.success,
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: _StatCard(
                label: 'Crashed',
                value: '$crashed',
                icon: Icons.warning_amber_rounded,
                iconColor: crashed > 0 ? TalePanelColors.danger : TalePanelColors.textMuted,
              ),
            ),
          ],
        ),

        const SizedBox(height: 24),

        // Server list
        Row(
          mainAxisAlignment: MainAxisAlignment.spaceBetween,
          children: [
            const Text(
              'Servers',
              style: TextStyle(
                fontSize: 16,
                fontWeight: FontWeight.w600,
                color: TalePanelColors.textPrimary,
              ),
            ),
            TextButton(
              onPressed: () => context.go('/servers'),
              child: const Text('See all →'),
            ),
          ],
        ),

        const SizedBox(height: 8),

        if (servers.isEmpty)
          const _EmptyServers()
        else
          ...servers.take(5).map((s) => Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: _ServerListTile(server: s),
              )),
      ],
    );
  }
}

class _StatCard extends StatelessWidget {
  final String label;
  final String value;
  final IconData icon;
  final Color? iconColor;

  const _StatCard({
    required this.label,
    required this.value,
    required this.icon,
    this.iconColor,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: TalePanelColors.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: TalePanelColors.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, color: iconColor ?? TalePanelColors.primary, size: 20),
          const SizedBox(height: 8),
          Text(
            value,
            style: const TextStyle(
              fontSize: 24,
              fontWeight: FontWeight.w700,
              color: TalePanelColors.textPrimary,
            ),
          ),
          Text(
            label,
            style: const TextStyle(color: TalePanelColors.textMuted, fontSize: 12),
          ),
        ],
      ),
    );
  }
}

class _ServerListTile extends StatelessWidget {
  final Server server;

  const _ServerListTile({required this.server});

  Color get _statusColor {
    switch (server.status) {
      case 'running':
        return TalePanelColors.success;
      case 'crashed':
        return TalePanelColors.danger;
      case 'starting':
      case 'stopping':
        return TalePanelColors.warning;
      default:
        return TalePanelColors.textMuted;
    }
  }

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: () => context.go('/servers/${server.id}'),
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: TalePanelColors.surface,
          borderRadius: BorderRadius.circular(10),
          border: Border.all(color: TalePanelColors.border),
        ),
        child: Row(
          children: [
            Container(
              width: 8,
              height: 8,
              decoration: BoxDecoration(
                color: _statusColor,
                shape: BoxShape.circle,
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    server.name,
                    style: const TextStyle(
                      fontWeight: FontWeight.w500,
                      color: TalePanelColors.textPrimary,
                    ),
                  ),
                  Text(
                    server.status,
                    style: const TextStyle(
                      fontSize: 12,
                      color: TalePanelColors.textMuted,
                    ),
                  ),
                ],
              ),
            ),
            const Icon(Icons.chevron_right, color: TalePanelColors.textMuted, size: 16),
          ],
        ),
      ),
    );
  }
}

class _EmptyServers extends StatelessWidget {
  const _EmptyServers();

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(32),
      decoration: BoxDecoration(
        color: TalePanelColors.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: TalePanelColors.border),
      ),
      child: const Column(
        children: [
          Icon(Icons.dns_outlined, size: 48, color: TalePanelColors.textMuted),
          SizedBox(height: 12),
          Text(
            'No servers yet',
            style: TextStyle(
              color: TalePanelColors.textPrimary,
              fontWeight: FontWeight.w500,
            ),
          ),
          SizedBox(height: 4),
          Text(
            'Create a server from the web panel.',
            style: TextStyle(color: TalePanelColors.textMuted, fontSize: 13),
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }
}

class _LoadingState extends StatelessWidget {
  const _LoadingState();

  @override
  Widget build(BuildContext context) {
    return const Center(
      child: CircularProgressIndicator(color: TalePanelColors.primary),
    );
  }
}

class _ErrorState extends StatelessWidget {
  final String error;

  const _ErrorState({required this.error});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            const Icon(Icons.error_outline, size: 48, color: TalePanelColors.danger),
            const SizedBox(height: 12),
            const Text(
              'Failed to load',
              style: TextStyle(color: TalePanelColors.textPrimary, fontWeight: FontWeight.w600),
            ),
            const SizedBox(height: 8),
            Text(
              error,
              style: const TextStyle(color: TalePanelColors.textMuted, fontSize: 13),
              textAlign: TextAlign.center,
            ),
          ],
        ),
      ),
    );
  }
}
