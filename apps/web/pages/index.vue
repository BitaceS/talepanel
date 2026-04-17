<script setup lang="ts">
import { useServersStore } from '~/stores/servers'
import { useApi } from '~/composables/useApi'
import { useModulesStore } from '~/stores/modules'
import type { Server as ServerType } from '~/types'

definePageMeta({
  title: 'Dashboard',
  middleware: 'auth',
})

const serversStore = useServersStore()
const api = useApi()
const modulesStore = useModulesStore()

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

// Fetch nodes
const nodes = ref<{ id: string; status: string }[]>([])
const activityLogs = ref<{ id: string; action: string; target_type: string; created_at: string }[]>([])

onMounted(async () => {
  clockInterval = setInterval(() => { now.value = new Date() }, 1000)
  serversStore.fetchServers()
  if (modulesStore.isEnabled('nodes')) {
    try {
      const data = await api.get<{ nodes: { id: string; status: string }[] }>('/nodes')
      nodes.value = data.nodes ?? []
    } catch { /* non-admin users won't have access */ }
  }
  try {
    const data = await api.get<{ logs: { id: string; action: string; target_type: string; created_at: string }[] }>('/admin/activity-logs')
    activityLogs.value = (data.logs ?? []).slice(0, 10)
  } catch { /* non-admin users won't have access */ }
})

const onlineNodes = computed(() => nodes.value.filter(n => n.status === 'online').length)

// Mock chart data for Server Performance
const perfBars = [
  { label: 'Mon', cpu: 45, ram: 62 },
  { label: 'Tue', cpu: 52, ram: 58 },
  { label: 'Wed', cpu: 38, ram: 71 },
  { label: 'Thu', cpu: 65, ram: 55 },
  { label: 'Fri', cpu: 42, ram: 68 },
  { label: 'Sat', cpu: 78, ram: 45 },
  { label: 'Sun', cpu: 35, ram: 52 },
]
</script>

<template>
  <div class="p-6 space-y-8">
    <!-- Page header -->
    <div class="flex items-start justify-between">
      <div>
        <h2 class="text-tp-text font-display font-bold text-3xl">Fleet Overview</h2>
        <p class="text-tp-muted text-sm mt-1">Real-time diagnostics for your Hytale ecosystem.</p>
      </div>
      <div class="text-right">
        <p class="text-tp-text font-mono text-2xl font-semibold tabular-nums">{{ timeDisplay }}</p>
        <p class="text-tp-outline text-xs mt-0.5">{{ dateDisplay }}</p>
      </div>
    </div>

    <!-- Stats row -->
    <div class="grid grid-cols-1 md:grid-cols-3 gap-5">
      <!-- Total Servers -->
      <div class="bg-tp-surface2 rounded-xl p-6">
        <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold mb-2">Total Servers</p>
        <p class="text-tp-text font-display font-bold text-4xl tabular-nums">{{ totalServers }}</p>
        <div class="flex items-center gap-1.5 mt-2">
          <span class="material-symbols-outlined text-tp-tertiary text-sm">trending_up</span>
          <p class="text-tp-tertiary text-xs">All systems monitored</p>
        </div>
      </div>

      <!-- Global Nodes -->
      <div class="bg-tp-surface2 rounded-xl p-6">
        <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold mb-2">Global Nodes</p>
        <p class="text-tp-text font-display font-bold text-4xl tabular-nums">{{ nodes.length > 0 ? String(nodes.length).padStart(2, '0') : '00' }}</p>
        <div class="flex items-center gap-1.5 mt-2">
          <span class="material-symbols-outlined text-tp-success text-sm">check_circle</span>
          <p class="text-tp-success text-xs">{{ onlineNodes }} operational</p>
        </div>
      </div>

      <!-- Active Players -->
      <div class="bg-tp-surface2 rounded-xl p-6">
        <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold mb-2">Active Players</p>
        <p class="text-tp-text font-display font-bold text-4xl tabular-nums">{{ serversList.reduce((sum) => sum, 0) }}</p>
        <div class="flex items-center gap-1.5 mt-2">
          <span class="material-symbols-outlined text-tp-accent text-sm">group</span>
          <p class="text-tp-muted text-xs">Across all servers</p>
        </div>
      </div>
    </div>

    <!-- Charts row -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-5">
      <!-- Server Performance (bar chart) -->
      <div class="bg-tp-surface2 rounded-xl p-6">
        <div class="flex items-start justify-between mb-1">
          <div>
            <h3 class="text-tp-text font-display font-semibold text-lg">Server Performance</h3>
            <p class="text-tp-muted text-xs mt-0.5">Real-time CPU/RAM telemetry</p>
          </div>
          <div class="flex items-center gap-4 text-xs">
            <div class="flex items-center gap-1.5">
              <div class="w-2.5 h-2.5 rounded-sm bg-tp-primary" />
              <span class="text-tp-muted">CPU</span>
            </div>
            <div class="flex items-center gap-1.5">
              <div class="w-2.5 h-2.5 rounded-sm bg-tp-tertiary" />
              <span class="text-tp-muted">RAM</span>
            </div>
          </div>
        </div>

        <!-- Bar chart -->
        <div class="flex items-end gap-3 h-40 mt-6 mb-4">
          <div v-for="bar in perfBars" :key="bar.label" class="flex-1 flex items-end gap-1">
            <div class="flex-1 bg-tp-primary/80 rounded-t transition-all duration-500" :style="{ height: bar.cpu + '%' }" />
            <div class="flex-1 bg-tp-tertiary/60 rounded-t transition-all duration-500" :style="{ height: bar.ram + '%' }" />
          </div>
        </div>
        <div class="flex gap-3">
          <div v-for="bar in perfBars" :key="bar.label" class="flex-1 text-center text-[10px] text-tp-outline">{{ bar.label }}</div>
        </div>

        <!-- Summary stats -->
        <div class="grid grid-cols-2 gap-4 mt-5 pt-5 border-t border-tp-border/30">
          <div>
            <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold">Total Allocation</p>
            <p class="text-tp-text font-display font-bold text-sm mt-1">{{ totalServers > 0 ? '—' : '0' }} / Available</p>
          </div>
          <div>
            <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold">Average CPU</p>
            <p class="text-tp-text font-display font-bold text-sm mt-1">—</p>
          </div>
        </div>
      </div>

      <!-- Player Activity (line chart placeholder) -->
      <div class="bg-tp-surface2 rounded-xl p-6">
        <div class="flex items-start justify-between mb-1">
          <div>
            <h3 class="text-tp-text font-display font-semibold text-lg">Player Activity</h3>
            <p class="text-tp-muted text-xs mt-0.5">24-hour player concurrency</p>
          </div>
          <span class="text-tp-muted text-xs">Last 24 Hours</span>
        </div>

        <!-- Line chart placeholder -->
        <div class="h-40 mt-6 mb-4 flex items-center justify-center">
          <svg class="w-full h-full" viewBox="0 0 400 150" preserveAspectRatio="none">
            <defs>
              <linearGradient id="lineGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="#89ceff" stop-opacity="0.3" />
                <stop offset="100%" stop-color="#89ceff" stop-opacity="0" />
              </linearGradient>
            </defs>
            <path d="M0,120 Q50,110 80,100 T140,80 T200,60 T260,45 T300,55 T340,70 T400,65" fill="none" stroke="#89ceff" stroke-width="2" />
            <path d="M0,120 Q50,110 80,100 T140,80 T200,60 T260,45 T300,55 T340,70 T400,65 L400,150 L0,150 Z" fill="url(#lineGrad)" />
          </svg>
        </div>

        <!-- Summary stats -->
        <div class="grid grid-cols-3 gap-4 mt-5 pt-5 border-t border-tp-border/30">
          <div>
            <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold">Morning Peak</p>
            <p class="text-tp-text font-display font-bold text-sm mt-1">0</p>
          </div>
          <div>
            <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold">Evening Peak</p>
            <p class="text-tp-text font-display font-bold text-sm mt-1">0</p>
          </div>
          <div>
            <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold">Avg Session</p>
            <p class="text-tp-text font-display font-bold text-sm mt-1">—</p>
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
        <div v-for="(log, i) in activityLogs" :key="log.id" class="flex items-center gap-4 px-5 py-3.5">
          <div class="w-2 h-2 rounded-full bg-tp-accent shrink-0" />
          <div class="flex-1 min-w-0">
            <p class="text-tp-text text-sm font-medium">{{ log.action }}</p>
            <p class="text-tp-muted text-xs">{{ log.target_type }}</p>
          </div>
          <span class="text-tp-outline text-xs shrink-0">{{ new Date(log.created_at).toLocaleString() }}</span>
        </div>
      </div>
    </div>
  </div>
</template>
