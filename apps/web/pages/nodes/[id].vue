<script setup lang="ts">
definePageMeta({ middleware: ['auth', 'module'], moduleId: 'nodes' })

const route = useRoute()
const api = useApi()
const nodeId = computed(() => route.params.id as string)

interface Node {
  id: string
  name: string
  fqdn: string
  port: number
  location?: string
  total_cpu: number
  total_ram_mb: number
  total_disk_mb: number
  max_servers: number
  status: string
  last_heartbeat?: string
  created_at: string
}

interface NodeMetricPoint {
  sampled_at: string
  cpu_pct: number
  ram_used_mb: number
  disk_used_mb: number
  active_servers: number
}

interface Server {
  id: string
  name: string
  status: string
  port: number
  node_id: string
}

const node = ref<Node | null>(null)
const metrics = ref<NodeMetricPoint[]>([])
const servers = ref<Server[]>([])
const loading = ref(true)
const error = ref('')

useHead({ title: computed(() => node.value ? `${node.value.name} · Nodes · TalePanel` : 'Node · TalePanel') })

const latestMetric = computed(() => metrics.value.length > 0 ? metrics.value[metrics.value.length - 1] : null)
const nodeServers = computed(() => servers.value.filter(s => s.node_id === nodeId.value))

async function fetchAll() {
  loading.value = true
  try {
    const [nodeData, metricsData, serversData] = await Promise.all([
      api.get<{ node: Node }>(`/nodes/${nodeId.value}`),
      api.get<{ metrics: NodeMetricPoint[] }>(`/nodes/${nodeId.value}/metrics?hours=24`).catch(() => ({ metrics: [] })),
      api.get<{ servers: Server[] }>('/servers').catch(() => ({ servers: [] })),
    ])
    node.value = nodeData.node
    metrics.value = metricsData.metrics ?? []
    servers.value = serversData.servers ?? []
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    error.value = e.data?.error ?? e.message ?? 'Failed to load node'
  } finally {
    loading.value = false
  }
}

onMounted(fetchAll)

const chartPoints = computed(() => {
  if (metrics.value.length < 2) return ''
  const pts = metrics.value
  const minTime = new Date(pts[0].sampled_at).getTime()
  const maxTime = new Date(pts[pts.length - 1].sampled_at).getTime()
  const timeRange = maxTime - minTime || 1
  return pts.map(p => {
    const x = 40 + ((new Date(p.sampled_at).getTime() - minTime) / timeRange) * 740
    const y = 190 - (p.cpu_pct / 100) * 180
    return `${x.toFixed(1)},${y.toFixed(1)}`
  }).join(' ')
})

function formatMB(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${mb} MB`
}

function timeAgo(iso?: string): string {
  if (!iso) return 'Never'
  const diff = Date.now() - new Date(iso).getTime()
  const s = Math.floor(diff / 1000)
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

function statusColor(status: string) {
  switch (status) {
    case 'running': return 'text-tp-tertiary bg-tp-tertiary/15'
    case 'stopped': return 'text-tp-muted bg-tp-surface3'
    case 'crashed': return 'text-tp-error bg-tp-error/15'
    default: return 'text-tp-warning bg-tp-warning/15'
  }
}

function nodeStatusColor(status: string) {
  switch (status) {
    case 'online': return 'text-tp-tertiary bg-tp-tertiary/15'
    case 'offline': return 'text-tp-muted bg-tp-surface3'
    default: return 'text-tp-warning bg-tp-warning/15'
  }
}
</script>

<template>
  <div class="p-6 space-y-6">
    <!-- Back link -->
    <NuxtLink
      to="/nodes"
      class="inline-flex items-center gap-1.5 text-sm text-tp-muted hover:text-tp-text transition-colors"
    >
      <span class="material-symbols-outlined text-base">arrow_back</span>
      Back to Nodes
    </NuxtLink>

    <!-- Loading state -->
    <div v-if="loading" class="flex items-center justify-center py-24 text-tp-muted">
      <span class="material-symbols-outlined animate-spin mr-2">progress_activity</span>
      Loading node…
    </div>

    <!-- Error state -->
    <div
      v-else-if="error"
      class="flex items-center gap-3 bg-tp-error/10 border border-tp-error/30 text-tp-error rounded-xl px-5 py-4 text-sm"
    >
      <span class="material-symbols-outlined">error</span>
      {{ error }}
    </div>

    <template v-else-if="node">
      <!-- Node header -->
      <div class="bg-tp-surface2 rounded-xl p-5 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div class="space-y-1">
          <div class="flex items-center gap-3 flex-wrap">
            <h1 class="text-xl font-semibold text-tp-text">{{ node.name }}</h1>
            <span
              class="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium capitalize"
              :class="nodeStatusColor(node.status)"
            >
              <span class="w-1.5 h-1.5 rounded-full bg-current"></span>
              {{ node.status }}
            </span>
          </div>
          <div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-tp-muted">
            <span class="flex items-center gap-1">
              <span class="material-symbols-outlined text-base">dns</span>
              {{ node.fqdn }}:{{ node.port }}
            </span>
            <span v-if="node.location" class="flex items-center gap-1">
              <span class="material-symbols-outlined text-base">location_on</span>
              {{ node.location }}
            </span>
            <span class="flex items-center gap-1">
              <span class="material-symbols-outlined text-base">schedule</span>
              Last heartbeat: {{ timeAgo(node.last_heartbeat) }}
            </span>
          </div>
        </div>
        <div class="flex items-center gap-2 text-sm text-tp-muted shrink-0">
          <span class="material-symbols-outlined text-base">memory</span>
          {{ node.max_servers }} max servers
        </div>
      </div>

      <!-- Live metrics cards -->
      <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <!-- CPU -->
        <div class="bg-tp-surface2 rounded-xl p-5 space-y-3">
          <div class="flex items-center justify-between">
            <span class="text-sm text-tp-muted font-medium">CPU Usage</span>
            <span class="material-symbols-outlined text-tp-primary text-xl">memory</span>
          </div>
          <div class="text-3xl font-bold text-tp-text">
            {{ latestMetric ? `${latestMetric.cpu_pct.toFixed(1)}%` : '—' }}
          </div>
          <div class="text-xs text-tp-muted">{{ node.total_cpu }} vCPU{{ node.total_cpu !== 1 ? 's' : '' }}</div>
        </div>

        <!-- RAM -->
        <div class="bg-tp-surface2 rounded-xl p-5 space-y-3">
          <div class="flex items-center justify-between">
            <span class="text-sm text-tp-muted font-medium">RAM Used</span>
            <span class="material-symbols-outlined text-tp-secondary text-xl">storage</span>
          </div>
          <div class="text-3xl font-bold text-tp-text">
            {{ latestMetric ? formatMB(latestMetric.ram_used_mb) : '—' }}
          </div>
          <div class="text-xs text-tp-muted">of {{ formatMB(node.total_ram_mb) }} total</div>
        </div>

        <!-- Disk -->
        <div class="bg-tp-surface2 rounded-xl p-5 space-y-3">
          <div class="flex items-center justify-between">
            <span class="text-sm text-tp-muted font-medium">Disk Used</span>
            <span class="material-symbols-outlined text-tp-accent text-xl">hard_drive</span>
          </div>
          <div class="text-3xl font-bold text-tp-text">
            {{ latestMetric ? formatMB(latestMetric.disk_used_mb) : '—' }}
          </div>
          <div class="text-xs text-tp-muted">of {{ formatMB(node.total_disk_mb) }} total</div>
        </div>
      </div>

      <!-- 24h CPU chart -->
      <div class="bg-tp-surface2 rounded-xl p-5 space-y-4">
        <h3 class="text-base font-semibold text-tp-text">CPU Load (24h)</h3>

        <div v-if="metrics.length >= 2">
          <svg
            viewBox="0 0 820 210"
            width="100%"
            preserveAspectRatio="xMidYMid meet"
            class="overflow-visible"
          >
            <!-- Grid lines -->
            <line x1="40" y1="145" x2="780" y2="145" stroke="currentColor" stroke-opacity="0.08" stroke-width="1" />
            <line x1="40" y1="100" x2="780" y2="100" stroke="currentColor" stroke-opacity="0.08" stroke-width="1" />
            <line x1="40" y1="55"  x2="780" y2="55"  stroke="currentColor" stroke-opacity="0.08" stroke-width="1" />
            <line x1="40" y1="10"  x2="780" y2="10"  stroke="currentColor" stroke-opacity="0.08" stroke-width="1" />
            <line x1="40" y1="190" x2="780" y2="190" stroke="currentColor" stroke-opacity="0.08" stroke-width="1" />

            <!-- Y axis labels -->
            <text x="32" y="194" text-anchor="end" font-size="10" fill="currentColor" opacity="0.4" class="font-mono">0</text>
            <text x="32" y="149" text-anchor="end" font-size="10" fill="currentColor" opacity="0.4" class="font-mono">25</text>
            <text x="32" y="104" text-anchor="end" font-size="10" fill="currentColor" opacity="0.4" class="font-mono">50</text>
            <text x="32" y="59"  text-anchor="end" font-size="10" fill="currentColor" opacity="0.4" class="font-mono">75</text>
            <text x="32" y="14"  text-anchor="end" font-size="10" fill="currentColor" opacity="0.4" class="font-mono">100</text>

            <!-- CPU line -->
            <polyline
              :points="chartPoints"
              fill="none"
              stroke="#3b82f6"
              stroke-width="2"
              stroke-linejoin="round"
              stroke-linecap="round"
            />
          </svg>
        </div>

        <div
          v-else
          class="flex flex-col items-center justify-center py-12 text-tp-muted text-sm gap-2"
        >
          <span class="material-symbols-outlined text-3xl opacity-40">show_chart</span>
          No metric data yet
        </div>
      </div>

      <!-- Servers on this node -->
      <div class="bg-tp-surface2 rounded-xl p-5 space-y-4">
        <h3 class="text-base font-semibold text-tp-text">
          Servers on this node
          <span class="ml-2 text-sm font-normal text-tp-muted">({{ nodeServers.length }})</span>
        </h3>

        <div v-if="nodeServers.length === 0" class="flex flex-col items-center justify-center py-10 text-tp-muted text-sm gap-2">
          <span class="material-symbols-outlined text-3xl opacity-40">dns</span>
          No servers on this node
        </div>

        <table v-else class="w-full text-sm">
          <thead>
            <tr class="border-b border-tp-surface3">
              <th class="text-left py-2 px-3 text-tp-muted font-medium">Name</th>
              <th class="text-left py-2 px-3 text-tp-muted font-medium">Status</th>
              <th class="text-left py-2 px-3 text-tp-muted font-medium">Port</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="server in nodeServers"
              :key="server.id"
              class="border-b border-tp-surface3/50 hover:bg-tp-surface3/30 transition-colors"
            >
              <td class="py-2.5 px-3">
                <NuxtLink
                  :to="`/servers/${server.id}`"
                  class="text-tp-text hover:text-tp-primary transition-colors font-medium"
                >
                  {{ server.name }}
                </NuxtLink>
              </td>
              <td class="py-2.5 px-3">
                <span
                  class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium capitalize"
                  :class="statusColor(server.status)"
                >
                  <span class="w-1.5 h-1.5 rounded-full bg-current"></span>
                  {{ server.status }}
                </span>
              </td>
              <td class="py-2.5 px-3 text-tp-muted font-mono">{{ server.port }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>
