<script setup lang="ts">
import { useServersStore } from '~/stores/servers'
import { useApi } from '~/composables/useApi'
import { useModulesStore } from '~/stores/modules'
import { useAuthStore } from '~/stores/auth'
import type { Server as ServerType } from '~/types'

definePageMeta({
  title: 'Dashboard',
  middleware: 'auth',
})

const serversStore = useServersStore()
const api = useApi()
const modulesStore = useModulesStore()
const authStore = useAuthStore()

// Live clock
const now = ref(new Date())
let clockInterval: ReturnType<typeof setInterval>

onUnmounted(() => {
  clearInterval(clockInterval)
})

const dateDisplay = computed(() =>
  now.value.toLocaleDateString('en-US', {
    weekday: 'long',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })
)

const timeDisplay = computed(() =>
  now.value.toLocaleTimeString('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
)

const serversList = computed<ServerType[]>(() =>
  Array.isArray(serversStore.servers) ? serversStore.servers : []
)
const totalServers = computed(() => serversList.value.length)
const onlineServers = computed(() =>
  serversList.value.filter((s) => s.status === 'running').length
)

interface ClusterStats {
  total_nodes: number
  online_nodes: number
  avg_cpu_pct: number
  total_ram_mb: number
  used_ram_mb: number
  total_disk_mb: number
  used_disk_mb: number
}

interface PlayerActivity {
  online_by_server: Record<string, number>
  online_players: number
  last_24h: { hour: string; players: number }[]
}

const isAdmin = computed(() => authStore.user?.role === 'admin' || authStore.user?.role === 'owner')

// null = not loaded (or no access); the template shows "—" instead of a guess.
const cluster = ref<ClusterStats | null>(null)
const activity = ref<PlayerActivity | null>(null)
const activityLogs = ref<{ id: string; action: string; target_type: string; created_at: string }[]>([])

onMounted(async () => {
  clockInterval = setInterval(() => { now.value = new Date() }, 1000)
  serversStore.fetchServers()
  const jobs: Promise<void>[] = [
    api.get<PlayerActivity>('/servers/activity')
      .then((d) => { activity.value = d })
      .catch(() => { /* leave as unknown */ }),
  ]
  if (isAdmin.value) {
    if (modulesStore.isEnabled('nodes')) {
      jobs.push(api.get<ClusterStats>('/nodes/cluster-stats')
        .then((d) => { cluster.value = d })
        .catch(() => { /* leave as unknown */ }))
    }
    jobs.push(api.get<{ logs: { id: string; action: string; target_type: string; created_at: string }[] }>('/admin/activity-logs')
      .then((d) => { activityLogs.value = (d.logs ?? []).slice(0, 10) })
      .catch(() => { /* leave empty */ }))
  }
  await Promise.all(jobs)
})

const offlineNodes = computed(() => cluster.value ? cluster.value.total_nodes - cluster.value.online_nodes : 0)

function pct(used: number, total: number): number {
  return total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0
}

function formatMB(mb: number): string {
  return mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${mb} MB`
}

const resourceBars = computed(() => {
  const c = cluster.value
  if (!c) return []
  return [
    { label: 'CPU (avg)', value: Math.min(100, Math.round(c.avg_cpu_pct)), detail: `${Math.round(c.avg_cpu_pct)}%` },
    { label: 'RAM', value: pct(c.used_ram_mb, c.total_ram_mb), detail: `${formatMB(c.used_ram_mb)} / ${formatMB(c.total_ram_mb)}` },
    { label: 'Disk', value: pct(c.used_disk_mb, c.total_disk_mb), detail: `${formatMB(c.used_disk_mb)} / ${formatMB(c.total_disk_mb)}` },
  ]
})

// Player activity: one bar per hour, scaled to the busiest hour.
const hourly = computed(() => activity.value?.last_24h ?? [])
const peak = computed(() => hourly.value.reduce((best, h) => (h.players > best.players ? h : best), { hour: '', players: 0 }))
const hadPlayers = computed(() => peak.value.players > 0)
const avgPlayers = computed(() => {
  if (!hourly.value.length) return 0
  return Math.round((hourly.value.reduce((sum, h) => sum + h.players, 0) / hourly.value.length) * 10) / 10
})

function hourLabel(iso: string): string {
  return new Date(iso).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false })
}

// Audit actions are stored as snake_case route names ("post_auth_login").
function actionLabel(action: string): string {
  const words = action.replace(/^(get|post|put|patch|delete)_/, '').replace(/_/g, ' ')
  return words.charAt(0).toUpperCase() + words.slice(1)
}
</script>

<template>
  <div class="p-6 space-y-8">
    <!-- Page header -->
    <div class="flex items-start justify-between">
      <div>
        <h2 class="text-tp-text font-display font-bold text-3xl">Fleet Overview</h2>
        <p class="text-tp-muted text-sm mt-1">Your servers, nodes and players at a glance.</p>
      </div>
      <div class="text-right">
        <p class="text-tp-text font-mono text-2xl font-semibold tabular-nums">{{ timeDisplay }}</p>
        <p class="text-tp-outline text-xs mt-0.5">{{ dateDisplay }}</p>
      </div>
    </div>

    <!-- Stats row -->
    <div class="grid grid-cols-1 md:grid-cols-3 gap-5">
      <!-- Servers -->
      <div class="bg-tp-surface2 rounded-xl p-6">
        <p class="text-[11px] text-tp-outline uppercase tracking-widest font-semibold mb-2">Servers</p>
        <p class="text-tp-text font-display font-bold text-4xl tabular-nums">{{ totalServers }}</p>
        <div class="flex items-center gap-1.5 mt-2">
          <span :class="['material-symbols-outlined text-sm', onlineServers > 0 ? 'text-tp-success' : 'text-tp-muted']">play_circle</span>
          <p class="text-tp-muted text-xs">{{ onlineServers }} running</p>
        </div>
      </div>

      <!-- Nodes (admins) -->
      <div v-if="cluster" class="bg-tp-surface2 rounded-xl p-6">
        <p class="text-[11px] text-tp-outline uppercase tracking-widest font-semibold mb-2">Nodes</p>
        <p class="text-tp-text font-display font-bold text-4xl tabular-nums">{{ cluster.total_nodes }}</p>
        <div class="flex items-center gap-1.5 mt-2">
          <span :class="['material-symbols-outlined text-sm', offlineNodes > 0 ? 'text-tp-warning' : 'text-tp-success']">
            {{ offlineNodes > 0 ? 'warning' : 'check_circle' }}
          </span>
          <p :class="['text-xs', offlineNodes > 0 ? 'text-tp-warning' : 'text-tp-success']">
            {{ cluster.online_nodes }} online<template v-if="offlineNodes > 0">, {{ offlineNodes }} offline</template>
          </p>
        </div>
      </div>
      <div v-else class="bg-tp-surface2 rounded-xl p-6">
        <p class="text-[11px] text-tp-outline uppercase tracking-widest font-semibold mb-2">Stopped</p>
        <p class="text-tp-text font-display font-bold text-4xl tabular-nums">{{ totalServers - onlineServers }}</p>
        <div class="flex items-center gap-1.5 mt-2">
          <span class="material-symbols-outlined text-tp-muted text-sm">stop_circle</span>
          <p class="text-tp-muted text-xs">Servers not running</p>
        </div>
      </div>

      <!-- Players -->
      <div class="bg-tp-surface2 rounded-xl p-6">
        <p class="text-[11px] text-tp-outline uppercase tracking-widest font-semibold mb-2">Players online</p>
        <p class="text-tp-text font-display font-bold text-4xl tabular-nums">{{ activity ? activity.online_players : '—' }}</p>
        <div class="flex items-center gap-1.5 mt-2">
          <span class="material-symbols-outlined text-tp-accent text-sm">group</span>
          <p class="text-tp-muted text-xs">Across your servers</p>
        </div>
      </div>
    </div>

    <!-- Charts row -->
    <div :class="['grid grid-cols-1 gap-5', cluster ? 'lg:grid-cols-2' : '']">
      <!-- Node resources (admins) -->
      <div v-if="cluster" class="bg-tp-surface2 rounded-xl p-6">
        <h3 class="text-tp-text font-display font-semibold text-lg">Node Resources</h3>
        <p class="text-tp-muted text-xs mt-0.5">Last reported usage, summed over all nodes</p>
        <div class="space-y-5 mt-6">
          <div v-for="bar in resourceBars" :key="bar.label">
            <div class="flex items-center justify-between text-xs mb-1.5">
              <span class="text-tp-muted">{{ bar.label }}</span>
              <span class="text-tp-text font-mono tabular-nums">{{ bar.detail }}</span>
            </div>
            <div class="h-2.5 bg-tp-surface-highest rounded-full overflow-hidden">
              <div
                :class="['h-full rounded-full transition-all duration-500', bar.value >= 90 ? 'bg-tp-danger' : bar.value >= 75 ? 'bg-tp-warning' : 'progress-fill']"
                :style="{ width: bar.value + '%' }"
              />
            </div>
          </div>
        </div>
        <p v-if="cluster.total_nodes === 0" class="text-tp-muted text-xs mt-5">No nodes registered yet.</p>
      </div>

      <!-- Player activity -->
      <div class="bg-tp-surface2 rounded-xl p-6">
        <h3 class="text-tp-text font-display font-semibold text-lg">Player Activity</h3>
        <p class="text-tp-muted text-xs mt-0.5">Players per hour, last 24 hours</p>

        <div v-if="!activity" class="h-40 mt-6 mb-4 flex items-center justify-center text-tp-muted text-sm">—</div>
        <div v-else-if="!hadPlayers" class="h-40 mt-6 mb-4 flex flex-col items-center justify-center text-center">
          <span class="material-symbols-outlined text-3xl text-tp-muted mb-2">bedtime</span>
          <p class="text-tp-muted text-sm">No players in the last 24 hours.</p>
        </div>
        <template v-else>
          <div class="flex items-end gap-[3px] h-40 mt-6 mb-2">
            <div
              v-for="h in hourly"
              :key="h.hour"
              class="flex-1 bg-tp-primary/80 rounded-t-sm min-h-[2px]"
              :style="{ height: (h.players / peak.players) * 100 + '%' }"
              :title="`${hourLabel(h.hour)}: ${h.players} ${h.players === 1 ? 'player' : 'players'}`"
            />
          </div>
          <div class="flex justify-between text-[11px] text-tp-outline font-mono">
            <span>{{ hourly[0] ? hourLabel(hourly[0].hour) : '' }}</span>
            <span>now</span>
          </div>
        </template>

        <div class="grid grid-cols-3 gap-4 mt-5 pt-5 border-t border-tp-border/30">
          <div>
            <p class="text-[11px] text-tp-outline uppercase tracking-widest font-semibold">Now</p>
            <p class="text-tp-text font-display font-bold text-sm mt-1 tabular-nums">{{ activity ? activity.online_players : '—' }}</p>
          </div>
          <div>
            <p class="text-[11px] text-tp-outline uppercase tracking-widest font-semibold">Peak</p>
            <p class="text-tp-text font-display font-bold text-sm mt-1 tabular-nums">
              {{ hadPlayers ? `${peak.players} at ${hourLabel(peak.hour)}` : '—' }}
            </p>
          </div>
          <div>
            <p class="text-[11px] text-tp-outline uppercase tracking-widest font-semibold">Hourly avg</p>
            <p class="text-tp-text font-display font-bold text-sm mt-1 tabular-nums">{{ activity ? avgPlayers : '—' }}</p>
          </div>
        </div>
      </div>
    </div>

    <!-- Servers section -->
    <div>
      <div class="flex items-center justify-between mb-5">
        <h3 class="text-tp-text font-display font-semibold text-xl">Your Servers</h3>
        <NuxtLink to="/servers">
          <UiButton variant="primary" size="sm">
            <span class="material-symbols-outlined text-base">add</span>
            New Server
          </UiButton>
        </NuxtLink>
      </div>

      <!-- Loading skeleton -->
      <div v-if="serversStore.loading" class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-5">
        <div
          v-for="i in 3"
          :key="i"
          class="bg-tp-surface2 rounded-xl p-5 animate-pulse"
        >
          <div class="flex items-center gap-3 mb-4">
            <div class="w-2.5 h-2.5 bg-tp-surface3 rounded-full" />
            <div class="h-4 bg-tp-surface3 rounded w-32" />
          </div>
          <div class="grid grid-cols-2 gap-3 mb-4">
            <div class="h-14 bg-tp-surface3 rounded-xl" />
            <div class="h-14 bg-tp-surface3 rounded-xl" />
          </div>
          <div class="h-4 bg-tp-surface3 rounded w-20" />
        </div>
      </div>

      <!-- Empty state -->
      <div
        v-else-if="serversStore.servers.length === 0"
        class="bg-tp-surface2 rounded-xl p-16 text-center"
      >
        <div class="w-16 h-16 bg-tp-surface3 rounded-2xl flex items-center justify-center mx-auto mb-4">
          <span class="material-symbols-outlined text-3xl text-tp-muted">dns</span>
        </div>
        <h4 class="text-tp-text font-display font-semibold text-lg mb-2">No servers yet</h4>
        <p class="text-tp-muted text-sm mb-6">Create your first server to get started.</p>
        <NuxtLink to="/servers">
          <UiButton variant="primary" size="md">
            <span class="material-symbols-outlined text-base">add</span>
            Create your first server
          </UiButton>
        </NuxtLink>
      </div>

      <!-- Server grid -->
      <div v-else class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-5">
        <ServerCard
          v-for="server in serversList.slice(0, 6)"
          :key="server.id"
          :server="server"
          :players="activity ? (activity.online_by_server[server.id] ?? 0) : undefined"
        />
        <NuxtLink
          v-if="serversList.length > 6"
          to="/servers"
          class="bg-tp-surface2 rounded-xl p-5 flex items-center justify-center text-tp-muted hover:text-tp-accent hover:bg-tp-surface3 transition-all duration-200"
        >
          <span class="text-sm font-medium">View all {{ serversStore.servers.length }} servers</span>
        </NuxtLink>
      </div>
    </div>

    <!-- Recent activity -->
    <div>
      <h3 class="text-tp-text font-display font-semibold text-xl mb-5">Recent Activity</h3>
      <div v-if="activityLogs.length === 0" class="bg-tp-surface2 rounded-xl p-10 text-center">
        <div class="w-12 h-12 bg-tp-surface3 rounded-xl flex items-center justify-center mx-auto mb-3">
          <span class="material-symbols-outlined text-2xl text-tp-muted">history</span>
        </div>
        <p class="text-tp-muted text-sm font-medium">No recent activity</p>
      </div>
      <div v-else class="bg-tp-surface2 rounded-xl overflow-hidden">
        <div v-for="log in activityLogs" :key="log.id" class="flex items-center gap-4 px-5 py-3.5">
          <div class="w-2 h-2 rounded-full bg-tp-accent shrink-0" />
          <div class="flex-1 min-w-0">
            <p class="text-tp-text text-sm font-medium">{{ actionLabel(log.action) }}</p>
            <p class="text-tp-muted text-xs">{{ log.target_type }}</p>
          </div>
          <span class="text-tp-outline text-xs shrink-0">{{ new Date(log.created_at).toLocaleString() }}</span>
        </div>
      </div>
    </div>
  </div>
</template>
