<script setup lang="ts">
import {
  Activity, Cpu,
  RefreshCw, Shield, AlertTriangle, Clock,
  Zap, Eye, Ban, Globe, TrendingUp, Radio,
  ShieldAlert, ShieldCheck, ShieldX, Timer,
} from 'lucide-vue-next'
import { useApi } from '~/composables/useApi'
import { useServersStore } from '~/stores/servers'

definePageMeta({ title: 'Monitoring', middleware: 'auth' })

const api = useApi()
const serversStore = useServersStore()

onMounted(() => {
  if (!Array.isArray(serversStore.servers) || serversStore.servers.length === 0) {
    serversStore.fetchServers()
  }
  fetchAllMetrics()
  fetchNetworkStats()
  pollInterval = setInterval(() => {
    fetchAllMetrics()
    fetchNetworkStats()
  }, 2000) // 2s poll for near-real-time
})

onUnmounted(() => {
  if (pollInterval) clearInterval(pollInterval)
})

let pollInterval: ReturnType<typeof setInterval> | null = null

const servers = computed(() =>
  Array.isArray(serversStore.servers) ? serversStore.servers : []
)
const runningCount = computed(() => servers.value.filter(s => s.status === 'running').length)

// ── Server metrics ───────────────────────────────────────────────────────────
interface ServerMetrics {
  cpu: { usage_percent: number }
  memory: { used_mb: number; limit_mb: number }
  uptime_s: number
}

const metricsMap = ref<Record<string, ServerMetrics>>({})
const metricsLoading = ref(false)

async function fetchAllMetrics() {
  metricsLoading.value = true
  const running = servers.value.filter(s => s.status === 'running')
  await Promise.allSettled(
    running.map(async s => {
      const data = await api.get<ServerMetrics>(`/servers/${s.id}/metrics`)
      metricsMap.value[s.id] = data
    })
  )
  metricsLoading.value = false
}

const totalCpu = computed(() => {
  const vals = Object.values(metricsMap.value)
  if (vals.length === 0) return 0
  return Math.round(vals.reduce((acc, m) => acc + (m.cpu?.usage_percent ?? 0), 0) / vals.length)
})

const totalRamUsed = computed(() =>
  Object.values(metricsMap.value).reduce((acc, m) => acc + (m.memory?.used_mb ?? 0), 0)
)

// ── Network monitor data ─────────────────────────────────────────────────────
interface HistorySample { ts: number; pps_in: number; pps_out: number; bps_in: number; bps_out: number; udp_pps: number }
interface InterfaceInfo { name: string; rx_bytes: number; tx_bytes: number; rx_packets: number; tx_packets: number; pps_in: number; pps_out: number; bps_in: number; bps_out: number }
interface GamePortInfo { port: number; rx_queue: number; drops: number; active_connections: number }
interface ThreatEvent { timestamp: string; event_type: string; severity: string; description: string; source_ip: string | null; auto_mitigated: boolean; metrics: Record<string, number> }
interface TopTalker { ip: string; pps: number; connections: number; severity: string; blacklisted: boolean }
interface BlacklistEntry { ip: string; reason: string; expires_at: string; pps_at_block: number }

interface NetSnapshot {
  current: { pps_in: number; pps_out: number; bps_in: number; bps_out: number; udp_pps_in: number; udp_noports_pps: number; total_connections: number }
  averages: { pps_in_5s: number; pps_in_30s: number; bps_in_5s: number; bps_in_30s: number }
  peaks: { pps_in_30s: number; bps_in_30s: number }
  history: HistorySample[]
  interfaces: InterfaceInfo[]
  game_ports: GamePortInfo[]
  threats: { level: string; events: ThreatEvent[]; syn_flood_count: number; udp_flood_detected: boolean; pps_spike_detected: boolean; distributed_flood: boolean; top_talkers: TopTalker[] }
  mitigation: { nft_available: boolean; active: boolean; blacklisted_ips: BlacklistEntry[]; rate_limit_active: boolean; rules_applied: number }
}

const net = ref<NetSnapshot | null>(null)
const networkLoading = ref(false)
const networkTab = ref<'overview' | 'threats' | 'mitigation'>('overview')

const activeNodeId = computed(() => {
  const s = servers.value.find(s => s.node_id)
  return s?.node_id ?? null
})

async function fetchNetworkStats() {
  const nodeId = activeNodeId.value
  if (!nodeId) return
  networkLoading.value = true
  try {
    net.value = await api.get<NetSnapshot>(`/nodes/${nodeId}/network-stats`)
  } catch { /* daemon may not be reachable */ }
  finally { networkLoading.value = false }
}

// ── PPS sparkline (last 60 samples) ──────────────────────────────────────────
const ppsHistory = computed(() => net.value?.history ?? [])
const maxPps = computed(() => Math.max(1, ...ppsHistory.value.map(h => h.pps_in)))
function sparkY(pps: number): number {
  return 100 - (pps / maxPps.value) * 100
}
const sparklinePath = computed(() => {
  const h = ppsHistory.value
  if (h.length < 2) return ''
  const step = 100 / (h.length - 1)
  return h.map((s, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(1)},${sparkY(s.pps_in).toFixed(1)}`).join(' ')
})

// ── Helpers ──────────────────────────────────────────────────────────────────
function statusColor(status: string) {
  switch (status) {
    case 'running':   return 'bg-tp-success'
    case 'crashed':   return 'bg-tp-danger'
    case 'starting':
    case 'stopping':  return 'bg-tp-warning'
    case 'installing': return 'bg-tp-primary'
    default:          return 'bg-tp-muted'
  }
}

function formatUptime(seconds: number): string {
  if (!seconds || seconds <= 0) return '-'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}

function formatMB(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${mb} MB`
}

function formatBytes(b: number): string {
  if (b >= 1073741824) return `${(b / 1073741824).toFixed(2)} GB`
  if (b >= 1048576) return `${(b / 1048576).toFixed(1)} MB`
  if (b >= 1024) return `${(b / 1024).toFixed(0)} KB`
  return `${b} B`
}

function formatRate(bps: number): string {
  if (bps >= 1073741824) return `${(bps / 1073741824).toFixed(1)} Gbps`
  if (bps >= 1048576) return `${(bps / 1048576).toFixed(1)} Mbps`
  if (bps >= 1024) return `${(bps / 1024).toFixed(0)} Kbps`
  return `${Math.round(bps)} bps`
}

function formatPps(pps: number): string {
  if (pps >= 1000000) return `${(pps / 1000000).toFixed(1)}M`
  if (pps >= 1000) return `${(pps / 1000).toFixed(1)}K`
  return `${Math.round(pps)}`
}

function threatColor(level: string) {
  switch (level) {
    case 'critical': return { text: 'text-red-400', bg: 'bg-red-400/10', border: 'border-red-400/30', badge: 'bg-red-400/20 text-red-400' }
    case 'high':     return { text: 'text-tp-danger', bg: 'bg-tp-danger/10', border: 'border-tp-danger/30', badge: 'bg-tp-danger/20 text-tp-danger' }
    case 'medium':   return { text: 'text-tp-warning', bg: 'bg-tp-warning/10', border: 'border-tp-warning/30', badge: 'bg-tp-warning/20 text-tp-warning' }
    case 'low':      return { text: 'text-yellow-300', bg: 'bg-yellow-300/10', border: 'border-yellow-300/30', badge: 'bg-yellow-300/20 text-yellow-300' }
    default:         return { text: 'text-tp-success', bg: 'bg-tp-success/10', border: 'border-tp-success/30', badge: 'bg-tp-success/20 text-tp-success' }
  }
}

const expandedTalker = ref<string | null>(null)
</script>

<template>
  <div class="p-6 space-y-5">
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-3">
        <Activity class="w-6 h-6 text-tp-primary" />
        <h2 class="text-tp-text font-display font-bold text-2xl">Monitoring</h2>
      </div>
      <UiButton variant="secondary" size="sm" :loading="metricsLoading || networkLoading" @click="() => { fetchAllMetrics(); fetchNetworkStats() }">
        <RefreshCw class="w-3.5 h-3.5" />
        Refresh
      </UiButton>
    </div>

    <!-- Overview stat cards -->
    <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
      <div v-for="stat in [
        { label: 'Servers', value: `${runningCount}/${servers.length}`, icon: Activity, color: 'text-tp-primary', bg: 'bg-tp-primary/10' },
        { label: 'Avg CPU', value: totalCpu + '%', icon: Cpu, color: 'text-tp-warning', bg: 'bg-tp-warning/10' },
        { label: 'Inbound PPS', value: net?.current ? formatPps(net.current.pps_in) : '-', icon: TrendingUp, color: 'text-tp-success', bg: 'bg-tp-success/10' },
        { label: 'Threat Level', value: net?.threats?.level ? net.threats.level.toUpperCase() : '-', icon: Shield, color: net?.threats?.level && net.threats.level !== 'clear' ? 'text-tp-danger' : 'text-tp-success', bg: net?.threats?.level && net.threats.level !== 'clear' ? 'bg-tp-danger/10' : 'bg-tp-success/10' },
      ]" :key="stat.label"
        class="bg-tp-surface rounded-xl p-4 flex items-center gap-4">
        <div :class="['w-10 h-10 rounded-xl flex items-center justify-center shrink-0', stat.bg]">
          <component :is="stat.icon" :class="['w-5 h-5', stat.color]" />
        </div>
        <div>
          <p class="text-tp-text font-bold text-xl leading-none">{{ stat.value }}</p>
          <p class="text-tp-outline text-xs mt-1">{{ stat.label }}</p>
        </div>
      </div>
    </div>

    <!-- Server metrics table -->
    <div class="bg-tp-surface rounded-xl overflow-hidden">
      <div class="px-5 py-3 border-b border-tp-border flex items-center justify-between">
        <h3 class="text-tp-text font-display font-semibold text-sm">Server Metrics</h3>
        <span class="text-tp-outline text-xs">Polling every 2s</span>
      </div>
      <div v-if="servers.length === 0" class="p-8 text-center text-tp-muted text-sm">No servers to monitor.</div>
      <table v-else class="w-full text-sm">
        <thead>
          <tr class="border-b border-tp-border">
            <th class="text-left px-4 py-2 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Server</th>
            <th class="text-left px-4 py-2 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Status</th>
            <th class="text-left px-4 py-2 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">CPU</th>
            <th class="text-left px-4 py-2 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">RAM</th>
            <th class="text-left px-4 py-2 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Uptime</th>
            <th class="text-left px-4 py-2 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Port</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="server in servers" :key="server.id" class="border-b border-tp-border/50 hover:bg-tp-surface2/50">
            <td class="px-4 py-3">
              <NuxtLink :to="`/servers/${server.id}`" class="text-tp-text font-medium hover:text-tp-accent transition-colors">{{ server.name }}</NuxtLink>
            </td>
            <td class="px-4 py-3">
              <div class="flex items-center gap-2">
                <div :class="['w-2 h-2 rounded-full shrink-0', statusColor(server.status)]" />
                <span class="text-tp-muted text-xs capitalize">{{ server.status }}</span>
              </div>
            </td>
            <td class="px-4 py-3">
              <template v-if="metricsMap[server.id]">
                <div class="flex items-center gap-2">
                  <div class="w-16 h-1.5 bg-tp-surface2 rounded-full overflow-hidden">
                    <div class="h-full rounded-full transition-all"
                      :class="metricsMap[server.id].cpu.usage_percent > 80 ? 'bg-tp-danger' : metricsMap[server.id].cpu.usage_percent > 50 ? 'bg-tp-warning' : 'bg-tp-success'"
                      :style="{ width: `${Math.min(100, metricsMap[server.id].cpu.usage_percent)}%` }" />
                  </div>
                  <span class="text-tp-text text-xs font-mono">{{ Math.round(metricsMap[server.id].cpu.usage_percent) }}%</span>
                </div>
              </template>
              <span v-else class="text-tp-muted text-xs">-</span>
            </td>
            <td class="px-4 py-3">
              <template v-if="metricsMap[server.id]">
                <span class="text-tp-text text-xs font-mono">{{ metricsMap[server.id].memory.used_mb }}/{{ metricsMap[server.id].memory.limit_mb }} MB</span>
              </template>
              <span v-else class="text-tp-muted text-xs">-</span>
            </td>
            <td class="px-4 py-3">
              <span v-if="metricsMap[server.id]" class="text-tp-text text-xs flex items-center gap-1">
                <Clock class="w-3 h-3 text-tp-muted" />{{ formatUptime(metricsMap[server.id].uptime_s) }}
              </span>
              <span v-else class="text-tp-muted text-xs">-</span>
            </td>
            <td class="px-4 py-3 text-tp-muted text-xs font-mono">:{{ server.port }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Network section with tabs -->
    <div class="bg-tp-surface rounded-xl overflow-hidden">
      <!-- Tab header -->
      <div class="px-5 py-3 border-b border-tp-border flex items-center justify-between">
        <div class="flex items-center gap-1">
          <button v-for="tab in [
            { key: 'overview', label: 'Network Overview', icon: Globe },
            { key: 'threats', label: 'DDoS Detection', icon: ShieldAlert },
            { key: 'mitigation', label: 'Mitigation', icon: ShieldCheck },
          ]" :key="tab.key"
            :class="['flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors',
              networkTab === tab.key ? 'bg-tp-primary/10 text-tp-primary' : 'text-tp-muted hover:text-tp-text hover:bg-tp-surface2']"
            @click="networkTab = tab.key as any">
            <component :is="tab.icon" class="w-3.5 h-3.5" />
            {{ tab.label }}
            <span v-if="tab.key === 'threats' && net?.threats?.level && net.threats.level !== 'clear'"
              :class="['ml-1 px-1.5 py-0.5 rounded text-[10px] font-bold uppercase', threatColor(net.threats.level).badge]">
              {{ net.threats.level }}
            </span>
          </button>
        </div>
        <span v-if="networkLoading" class="text-tp-muted text-xs animate-pulse">updating...</span>
      </div>

      <!-- OVERVIEW TAB -->
      <div v-if="networkTab === 'overview'">
        <div v-if="!net" class="p-12 text-center">
          <Globe class="w-10 h-10 text-tp-muted mx-auto mb-3" />
          <p class="text-tp-muted text-sm">Waiting for network monitor to initialise...</p>
        </div>
        <div v-else class="p-4 space-y-4">
          <!-- PPS/BPS rate cards -->
          <div class="grid grid-cols-2 md:grid-cols-4 gap-3">
            <div class="bg-tp-surface2 rounded-xl p-3">
              <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Inbound PPS</p>
              <p class="text-tp-text font-mono text-2xl font-bold">{{ formatPps(net.current.pps_in) }}</p>
              <p class="text-tp-muted text-[10px] mt-0.5">5s avg: {{ formatPps(net.averages.pps_in_5s) }} | 30s: {{ formatPps(net.averages.pps_in_30s) }}</p>
            </div>
            <div class="bg-tp-surface2 rounded-xl p-3">
              <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Inbound BPS</p>
              <p class="text-tp-text font-mono text-2xl font-bold">{{ formatRate(net.current.bps_in * 8) }}</p>
              <p class="text-tp-muted text-[10px] mt-0.5">5s avg: {{ formatRate(net.averages.bps_in_5s * 8) }}</p>
            </div>
            <div class="bg-tp-surface2 rounded-xl p-3">
              <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">UDP Datagrams/s</p>
              <p :class="['font-mono text-2xl font-bold', net.current.udp_pps_in > 10000 ? 'text-tp-danger' : 'text-tp-text']">{{ formatPps(net.current.udp_pps_in) }}</p>
              <p class="text-tp-muted text-[10px] mt-0.5">NoPorts/s: {{ Math.round(net.current.udp_noports_pps) }}</p>
            </div>
            <div class="bg-tp-surface2 rounded-xl p-3">
              <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Peak PPS (30s)</p>
              <p class="text-tp-text font-mono text-2xl font-bold">{{ formatPps(net.peaks.pps_in_30s) }}</p>
              <p class="text-tp-muted text-[10px] mt-0.5">Peak BPS: {{ formatRate(net.peaks.bps_in_30s * 8) }}</p>
            </div>
          </div>

          <!-- PPS sparkline chart -->
          <div v-if="ppsHistory.length > 1" class="bg-tp-surface2 rounded-xl p-4">
            <div class="flex items-center justify-between mb-2">
              <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">PPS History (last 60s)</p>
              <p class="text-tp-muted text-xs">Peak: {{ formatPps(maxPps) }}</p>
            </div>
            <svg viewBox="0 0 100 40" class="w-full h-20" preserveAspectRatio="none">
              <!-- Grid lines -->
              <line x1="0" y1="0" x2="100" y2="0" stroke="currentColor" class="text-tp-border" stroke-width="0.2" />
              <line x1="0" y1="20" x2="100" y2="20" stroke="currentColor" class="text-tp-border" stroke-width="0.1" stroke-dasharray="1" />
              <line x1="0" y1="40" x2="100" y2="40" stroke="currentColor" class="text-tp-border" stroke-width="0.2" />
              <!-- Area fill -->
              <path :d="sparklinePath + ` L100,40 L0,40 Z`" fill="url(#ppsGradient)" opacity="0.3" />
              <!-- Line -->
              <path :d="sparklinePath" fill="none" stroke="currentColor" class="text-tp-primary" stroke-width="0.5" vector-effect="non-scaling-stroke" />
              <defs>
                <linearGradient id="ppsGradient" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stop-color="var(--color-tp-primary, #6366f1)" stop-opacity="0.4" />
                  <stop offset="100%" stop-color="var(--color-tp-primary, #6366f1)" stop-opacity="0" />
                </linearGradient>
              </defs>
            </svg>
          </div>

          <!-- Interface traffic -->
          <div v-if="net.interfaces.length > 0">
            <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-2 px-1">Interfaces</p>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
              <div v-for="iface in net.interfaces.filter(i => i.name !== 'lo')" :key="iface.name"
                class="bg-tp-surface2 rounded-xl p-3">
                <div class="flex items-center justify-between mb-2">
                  <span class="text-tp-text font-mono font-semibold text-sm">{{ iface.name }}</span>
                  <span class="text-tp-outline text-[10px]">{{ formatPps(iface.pps_in) }} pps in / {{ formatPps(iface.pps_out) }} out</span>
                </div>
                <div class="grid grid-cols-2 gap-2 text-xs">
                  <div>
                    <p class="text-tp-outline">RX Total</p>
                    <p class="text-tp-success font-mono">{{ formatBytes(iface.rx_bytes) }}</p>
                    <p class="text-tp-muted font-mono text-[10px]">{{ formatRate(iface.bps_in * 8) }}</p>
                  </div>
                  <div>
                    <p class="text-tp-outline">TX Total</p>
                    <p class="text-tp-primary font-mono">{{ formatBytes(iface.tx_bytes) }}</p>
                    <p class="text-tp-muted font-mono text-[10px]">{{ formatRate(iface.bps_out * 8) }}</p>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- Game port activity -->
          <div v-if="net.game_ports.length > 0">
            <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-2 px-1">Game Ports</p>
            <div class="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-6 gap-3">
              <div v-for="gp in net.game_ports" :key="gp.port" class="bg-tp-surface2 rounded-xl p-3 text-center">
                <p class="text-tp-text font-mono font-semibold">:{{ gp.port }}</p>
                <p class="text-tp-tertiary text-sm font-mono">{{ gp.active_connections }} conn</p>
                <p v-if="gp.rx_queue > 0" class="text-tp-warning text-[10px] font-mono">queue: {{ gp.rx_queue }}B</p>
                <p v-if="gp.drops > 0" class="text-tp-danger text-[10px] font-mono">{{ gp.drops }} drops</p>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- THREATS TAB -->
      <div v-if="networkTab === 'threats'">
        <div v-if="!net" class="p-12 text-center">
          <ShieldAlert class="w-10 h-10 text-tp-muted mx-auto mb-3" />
          <p class="text-tp-muted text-sm">Waiting for network monitor...</p>
        </div>
        <div v-else-if="!net.threats" class="p-12 text-center">
          <ShieldCheck class="w-10 h-10 text-tp-muted mx-auto mb-3" />
          <p class="text-tp-muted text-sm">Threat detection not available on this node.</p>
        </div>
        <div v-else>
          <!-- Threat level banner -->
          <div :class="['px-5 py-3 border-b border-tp-border flex items-center justify-between', threatColor(net.threats.level).bg]">
            <div class="flex items-center gap-2">
              <div :class="['w-8 h-8 rounded-xl flex items-center justify-center', net.threats.level === 'clear' ? 'bg-tp-success/20' : 'bg-tp-danger/20']">
                <ShieldCheck v-if="net.threats.level === 'clear'" class="w-4 h-4 text-tp-success" />
                <ShieldX v-else class="w-4 h-4 text-tp-danger" />
              </div>
              <div>
                <p :class="['text-sm font-semibold', threatColor(net.threats.level).text]">
                  {{ net.threats.level === 'clear' ? 'No Threats Detected' : `Threat Level: ${net.threats.level.toUpperCase()}` }}
                </p>
                <p class="text-tp-muted text-xs">
                  {{ formatPps(net.current.pps_in) }} PPS inbound | {{ net.current.total_connections }} game connections | {{ net.threats.top_talkers?.length ?? 0 }} tracked IPs
                </p>
              </div>
            </div>
          </div>

          <div class="p-4 space-y-4">
            <!-- Attack pattern indicators -->
            <div class="grid grid-cols-1 md:grid-cols-4 gap-3">
              <div :class="['rounded-xl border p-3',
                net.current.udp_pps_in > 20000 ? 'border-tp-danger/30 bg-tp-danger/5' :
                net.current.udp_pps_in > 10000 ? 'border-tp-warning/30 bg-tp-warning/5' : 'border-tp-border bg-tp-surface2']">
                <div class="flex items-center gap-2 mb-1">
                  <Zap :class="['w-4 h-4', net.current.udp_pps_in > 10000 ? 'text-tp-danger' : 'text-tp-muted']" />
                  <span class="text-tp-text text-xs font-semibold">UDP Flood</span>
                </div>
                <p class="text-tp-text font-mono text-lg font-bold">{{ formatPps(net.current.udp_pps_in) }}/s</p>
                <p class="text-tp-muted text-[10px]">UDP datagrams per second</p>
              </div>

              <div :class="['rounded-xl border p-3',
                net.threats.syn_flood_count > 20 ? 'border-tp-danger/30 bg-tp-danger/5' : 'border-tp-border bg-tp-surface2']">
                <div class="flex items-center gap-2 mb-1">
                  <Zap :class="['w-4 h-4', net.threats.syn_flood_count > 20 ? 'text-tp-danger' : 'text-tp-muted']" />
                  <span class="text-tp-text text-xs font-semibold">SYN Flood</span>
                </div>
                <p class="text-tp-text font-mono text-lg font-bold">{{ net.threats.syn_flood_count }}</p>
                <p class="text-tp-muted text-[10px]">half-open TCP connections</p>
              </div>

              <div :class="['rounded-xl border p-3',
                net.threats.pps_spike_detected ? 'border-tp-warning/30 bg-tp-warning/5' : 'border-tp-border bg-tp-surface2']">
                <div class="flex items-center gap-2 mb-1">
                  <TrendingUp :class="['w-4 h-4', net.threats.pps_spike_detected ? 'text-tp-warning' : 'text-tp-muted']" />
                  <span class="text-tp-text text-xs font-semibold">PPS Spike</span>
                </div>
                <p class="text-tp-text font-mono text-lg font-bold">{{ net.threats.pps_spike_detected ? 'DETECTED' : 'None' }}</p>
                <p class="text-tp-muted text-[10px]">sudden rate increase vs 30s avg</p>
              </div>

              <div :class="['rounded-xl border p-3',
                net.current.udp_noports_pps > 100 ? 'border-tp-warning/30 bg-tp-warning/5' : 'border-tp-border bg-tp-surface2']">
                <div class="flex items-center gap-2 mb-1">
                  <Radio :class="['w-4 h-4', net.current.udp_noports_pps > 100 ? 'text-tp-warning' : 'text-tp-muted']" />
                  <span class="text-tp-text text-xs font-semibold">Reflection/Scan</span>
                </div>
                <p class="text-tp-text font-mono text-lg font-bold">{{ Math.round(net.current.udp_noports_pps) }}/s</p>
                <p class="text-tp-muted text-[10px]">packets to closed ports</p>
              </div>
            </div>

            <!-- Top talkers -->
            <div v-if="net.threats.top_talkers.length > 0">
              <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-2 px-1">
                Top Talkers <span class="normal-case font-normal">({{ net.threats.top_talkers.length }} IPs)</span>
              </p>
              <div class="space-y-1">
                <div v-for="talker in net.threats.top_talkers" :key="talker.ip">
                  <button
                    :class="['w-full flex items-center justify-between rounded-xl px-4 py-2.5 text-left transition-colors',
                      talker.severity !== 'normal'
                        ? `${threatColor(talker.severity).bg} border ${threatColor(talker.severity).border}`
                        : 'bg-tp-surface2 border border-tp-border hover:bg-tp-surface2/80']"
                    @click="expandedTalker = expandedTalker === talker.ip ? null : talker.ip">
                    <div class="flex items-center gap-3">
                      <span class="text-tp-text text-xs font-mono font-semibold">{{ talker.ip }}</span>
                      <span v-if="talker.blacklisted" class="text-[10px] font-bold px-1.5 py-0.5 rounded bg-tp-danger/20 text-tp-danger">BLOCKED</span>
                      <span v-else-if="talker.severity !== 'normal'"
                        :class="['text-[10px] font-bold uppercase px-1.5 py-0.5 rounded', threatColor(talker.severity).badge]">
                        {{ talker.severity }}
                      </span>
                    </div>
                    <span class="text-tp-text text-xs font-mono">{{ talker.connections }} connections</span>
                  </button>
                </div>
              </div>
            </div>

            <!-- Threat event log -->
            <div>
              <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-2 px-1">
                Event Log <span class="normal-case font-normal">({{ net.threats.events.length }} events)</span>
              </p>
              <div v-if="net.threats.events.length === 0" class="text-center py-6 text-tp-muted text-sm">
                No threat events recorded.
              </div>
              <div v-else class="space-y-1 max-h-80 overflow-y-auto">
                <div v-for="(evt, i) in [...net.threats.events].reverse().slice(0, 50)" :key="i"
                  :class="['flex items-start gap-3 rounded-xl px-3 py-2 border text-xs', threatColor(evt.severity).bg, threatColor(evt.severity).border]">
                  <AlertTriangle :class="['w-3.5 h-3.5 shrink-0 mt-0.5', threatColor(evt.severity).text]" />
                  <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2">
                      <span :class="['font-semibold uppercase', threatColor(evt.severity).text]">{{ evt.severity }}</span>
                      <span class="text-tp-muted">{{ evt.event_type }}</span>
                      <span v-if="evt.auto_mitigated" class="text-tp-success text-[10px] font-semibold">AUTO-MITIGATED</span>
                    </div>
                    <p class="text-tp-text mt-0.5">{{ evt.description }}</p>
                    <p class="text-tp-muted text-[10px] mt-0.5">{{ new Date(evt.timestamp).toLocaleTimeString() }}</p>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- MITIGATION TAB -->
      <div v-if="networkTab === 'mitigation'">
        <div v-if="!net" class="p-12 text-center">
          <ShieldCheck class="w-10 h-10 text-tp-muted mx-auto mb-3" />
          <p class="text-tp-muted text-sm">Waiting for network monitor...</p>
        </div>
        <div v-else class="p-4 space-y-4">
          <!-- Mitigation status -->
          <div class="grid grid-cols-2 md:grid-cols-4 gap-3">
            <div :class="['rounded-xl border p-3', net.mitigation.nft_available ? 'border-tp-success/30 bg-tp-success/5' : 'border-tp-border bg-tp-surface2']">
              <div class="flex items-center gap-2 mb-1">
                <Shield :class="['w-4 h-4', net.mitigation.nft_available ? 'text-tp-success' : 'text-tp-muted']" />
                <span class="text-tp-text text-xs font-semibold">nftables</span>
              </div>
              <p :class="['font-mono text-sm font-bold', net.mitigation.nft_available ? 'text-tp-success' : 'text-tp-muted']">
                {{ net.mitigation.nft_available ? 'Available' : 'Unavailable' }}
              </p>
            </div>

            <div :class="['rounded-xl border p-3', net.mitigation.rate_limit_active ? 'border-tp-success/30 bg-tp-success/5' : 'border-tp-border bg-tp-surface2']">
              <div class="flex items-center gap-2 mb-1">
                <Timer class="w-4 h-4 text-tp-muted" />
                <span class="text-tp-text text-xs font-semibold">Rate Limiting</span>
              </div>
              <p :class="['font-mono text-sm font-bold', net.mitigation.rate_limit_active ? 'text-tp-success' : 'text-tp-muted']">
                {{ net.mitigation.rate_limit_active ? 'Active' : 'Inactive' }}
              </p>
              <p class="text-tp-muted text-[10px]">Per-IP: 5K pps | Global: 50K pps</p>
            </div>

            <div class="rounded-xl border border-tp-border bg-tp-surface2 p-3">
              <div class="flex items-center gap-2 mb-1">
                <Ban class="w-4 h-4 text-tp-muted" />
                <span class="text-tp-text text-xs font-semibold">Blacklisted</span>
              </div>
              <p class="text-tp-text font-mono text-sm font-bold">{{ net.mitigation.blacklisted_ips.length }} IPs</p>
            </div>

            <div class="rounded-xl border border-tp-border bg-tp-surface2 p-3">
              <div class="flex items-center gap-2 mb-1">
                <Eye class="w-4 h-4 text-tp-muted" />
                <span class="text-tp-text text-xs font-semibold">Rules Applied</span>
              </div>
              <p class="text-tp-text font-mono text-sm font-bold">{{ net.mitigation.rules_applied }}</p>
            </div>
          </div>

          <!-- Protection rules info -->
          <div v-if="net.mitigation.nft_available" class="bg-tp-surface2 rounded-xl p-4">
            <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-2">Active Protection Rules</p>
            <div class="space-y-2 text-xs font-mono">
              <div class="flex items-center gap-2 text-tp-text">
                <ShieldCheck class="w-3.5 h-3.5 text-tp-success shrink-0" />
                <span>Blacklist set (auto-block abusive IPs for 5min)</span>
              </div>
              <div class="flex items-center gap-2 text-tp-text">
                <ShieldCheck class="w-3.5 h-3.5 text-tp-success shrink-0" />
                <span>Per-IP rate limit: DROP if >5,000 UDP pps per source</span>
              </div>
              <div class="flex items-center gap-2 text-tp-text">
                <ShieldCheck class="w-3.5 h-3.5 text-tp-success shrink-0" />
                <span>Global rate limit: DROP if >50,000 UDP pps total on game ports</span>
              </div>
              <div class="flex items-center gap-2 text-tp-text">
                <ShieldCheck class="w-3.5 h-3.5 text-tp-success shrink-0" />
                <span>Traffic counters on UDP ports 5520-5600</span>
              </div>
            </div>
          </div>
          <div v-else class="bg-tp-warning/5 border border-tp-warning/20 rounded-xl p-4">
            <div class="flex items-center gap-2 mb-1">
              <AlertTriangle class="w-4 h-4 text-tp-warning" />
              <span class="text-tp-warning text-xs font-semibold">Mitigation Unavailable</span>
            </div>
            <p class="text-tp-muted text-xs">
              nftables is not available in this container. The daemon is running in detection-only mode.
              To enable mitigation, ensure the container has NET_ADMIN capability and nftables is installed.
            </p>
          </div>

          <!-- Blacklisted IPs -->
          <div v-if="net.mitigation.blacklisted_ips.length > 0">
            <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-2 px-1">Blacklisted IPs</p>
            <table class="w-full text-sm">
              <thead>
                <tr class="border-b border-tp-border">
                  <th class="text-left px-4 py-2 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">IP</th>
                  <th class="text-left px-4 py-2 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Reason</th>
                  <th class="text-left px-4 py-2 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">PPS at Block</th>
                  <th class="text-left px-4 py-2 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Expires</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="bl in net.mitigation.blacklisted_ips" :key="bl.ip" class="border-b border-tp-border/50">
                  <td class="px-4 py-2 text-tp-text text-xs font-mono">{{ bl.ip }}</td>
                  <td class="px-4 py-2 text-tp-muted text-xs">{{ bl.reason }}</td>
                  <td class="px-4 py-2 text-tp-danger text-xs font-mono">{{ formatPps(bl.pps_at_block) }}</td>
                  <td class="px-4 py-2 text-tp-muted text-xs">{{ new Date(bl.expires_at).toLocaleTimeString() }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
