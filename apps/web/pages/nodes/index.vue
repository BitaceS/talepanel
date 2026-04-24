<script setup lang="ts">
import { useApi } from '~/composables/useApi'
import { useAuthStore } from '~/stores/auth'

definePageMeta({ title: 'Nodes', middleware: 'auth' })

const api = useApi()
const authStore = useAuthStore()

interface Node {
  id: string
  name: string
  fqdn: string
  port: number
  location: string
  total_cpu: number
  total_ram_mb: number
  total_disk_mb: number
  max_servers: number
  status: string
  last_heartbeat?: string
  created_at: string
}

interface ClusterStats {
  total_nodes: number
  online_nodes: number
  total_servers: number
  running_servers: number
  avg_cpu_pct: number
  total_ram_mb: number
  used_ram_mb: number
  total_disk_mb: number
  used_disk_mb: number
}

interface NodeMetricPoint {
  sampled_at: string
  cpu_pct: number
  ram_used_mb: number
  disk_used_mb: number
  active_servers: number
}

const nodes = ref<Node[]>([])
const loading = ref(false)
const error = ref('')
const clusterStats = ref<ClusterStats | null>(null)
const nodeMetrics = ref<Record<string, NodeMetricPoint | null>>({})

async function fetchNodes() {
  loading.value = true
  error.value = ''
  try {
    const [nodesData, statsData] = await Promise.all([
      api.get<{ nodes: Node[] }>('/nodes'),
      api.get<ClusterStats>('/nodes/cluster-stats').catch(() => null),
    ])
    nodes.value = nodesData.nodes ?? []
    clusterStats.value = statsData

    // Fetch per-node metrics for online nodes
    const onlineNodes = nodes.value.filter(n => n.status === 'online')
    const results = await Promise.allSettled(
      onlineNodes.map(n =>
        api.get<{ metrics: NodeMetricPoint[] }>(`/nodes/${n.id}/metrics?hours=1`)
      )
    )
    const metricsMap: Record<string, NodeMetricPoint | null> = {}
    results.forEach((result, idx) => {
      const nodeId = onlineNodes[idx].id
      if (result.status === 'fulfilled') {
        const pts = result.value.metrics ?? []
        metricsMap[nodeId] = pts.length > 0 ? pts[pts.length - 1] : null
      } else {
        metricsMap[nodeId] = null
      }
    })
    nodeMetrics.value = metricsMap
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    error.value = e.data?.error ?? e.message ?? 'Failed to fetch nodes'
  } finally {
    loading.value = false
  }
}

onMounted(fetchNodes)

// ── Delete ────────────────────────────────────────────────────────────────────
const deletingId = ref('')
const toast = ref('')
const toastType = ref<'success' | 'error'>('success')
let toastTimer: ReturnType<typeof setTimeout>

function showToast(msg: string, type: 'success' | 'error' = 'success') {
  toast.value = msg
  toastType.value = type
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value = '' }, 3000)
}

async function deleteNode(id: string, name: string) {
  if (!confirm(`Delete node "${name}"? This cannot be undone.`)) return
  deletingId.value = id
  try {
    await api.delete(`/nodes/${id}`)
    nodes.value = nodes.value.filter(n => n.id !== id)
    showToast('Node deleted')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to delete node', 'error')
  } finally {
    deletingId.value = ''
  }
}

// ── Enrollment modal ──────────────────────────────────────────────────────────
const showRegisterModal = ref(false)
const registerLoading = ref(false)
const registerError = ref('')
const enrollment = ref<{ enrollment_id: string; token: string; expires_at: string } | null>(null)
const tokenCopied = ref(false)
const cmdCopied = ref(false)

const registerForm = reactive({
  name: '',
  total_cpu: 4,
  total_ram_mb: 8192,
  total_disk_mb: 102400,
  max_servers: 10,
})

const panelOrigin = computed(() =>
  typeof window !== 'undefined' ? window.location.origin : 'https://panel.example.com'
)

const installCommand = computed(() => {
  if (!enrollment.value) return ''
  return `sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh) --mode daemon \\
  --panel-url ${panelOrigin.value} \\
  --enrollment-token '${enrollment.value.token}'`
})

async function submitRegister() {
  if (!registerForm.name) return
  registerLoading.value = true
  registerError.value = ''
  try {
    const data = await api.post<{ enrollment_id: string; token: string; expires_at: string }>(
      '/admin/nodes/enroll',
      {
        node_name: registerForm.name,
        total_cpu: Number(registerForm.total_cpu) || 0,
        total_ram_mb: Number(registerForm.total_ram_mb) || 0,
        total_disk_mb: Number(registerForm.total_disk_mb) || 0,
        max_servers: Number(registerForm.max_servers) || 0,
      }
    )
    enrollment.value = data
    showToast('Enrollment token created')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    registerError.value = e.data?.error ?? e.message ?? 'Failed to create enrollment'
  } finally {
    registerLoading.value = false
  }
}

async function copy(text: string, which: 'token' | 'cmd') {
  try {
    await navigator.clipboard.writeText(text)
    if (which === 'token') {
      tokenCopied.value = true
      setTimeout(() => { tokenCopied.value = false }, 2000)
    } else {
      cmdCopied.value = true
      setTimeout(() => { cmdCopied.value = false }, 2000)
    }
  } catch {
    showToast('Copy failed — select and copy manually', 'error')
  }
}

function closeModal() {
  showRegisterModal.value = false
  enrollment.value = null
  registerError.value = ''
  tokenCopied.value = false
  cmdCopied.value = false
  Object.assign(registerForm, { name: '', total_cpu: 4, total_ram_mb: 8192, total_disk_mb: 102400, max_servers: 10 })
  // Refresh list — the daemon may have redeemed the token already
  fetchNodes()
}

// ── Edit modal ────────────────────────────────────────────────────────────────
const showEditModal = ref(false)
const editingNode = ref<Node | null>(null)
const editForm = reactive({ name: '', location: '', max_servers: 0 })
const editLoading = ref(false)
const editError = ref('')

function openEditModal(node: Node) {
  editingNode.value = node
  editForm.name = node.name
  editForm.location = node.location ?? ''
  editForm.max_servers = node.max_servers
  editError.value = ''
  showEditModal.value = true
}

function closeEditModal() {
  showEditModal.value = false
  editingNode.value = null
  editError.value = ''
}

async function submitEdit() {
  if (!editingNode.value) return
  editLoading.value = true
  editError.value = ''
  try {
    const node = editingNode.value
    const payload: Record<string, unknown> = {}
    if (editForm.name && editForm.name !== node.name) payload.name = editForm.name
    if (editForm.location !== (node.location ?? '')) payload.location = editForm.location
    if (editForm.max_servers !== node.max_servers) payload.max_servers = editForm.max_servers

    const data = await api.patch<{ node: Node }>(`/nodes/${node.id}`, payload)
    const idx = nodes.value.findIndex(n => n.id === node.id)
    if (idx !== -1) nodes.value[idx] = data.node
    showToast('Node updated')
    closeEditModal()
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    editError.value = e.data?.error ?? e.message ?? 'Failed to update node'
  } finally {
    editLoading.value = false
  }
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function statusColor(status: string) {
  switch (status) {
    case 'online': return 'text-tp-tertiary bg-tp-tertiary/15'
    case 'offline': return 'text-tp-muted bg-tp-surface3'
    default: return 'text-tp-warning bg-tp-warning/15'
  }
}

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

function uptimeFromHeartbeat(iso?: string): string {
  if (!iso) return '--:--:--'
  const diff = Date.now() - new Date(iso).getTime()
  const s = Math.floor(diff / 1000)
  if (s > 300) return '--:--:--'
  return '99.9%'
}

const isAdmin = computed(() =>
  authStore.user?.role === 'admin' || authStore.user?.role === 'owner'
)

const onlineCount = computed(() => nodes.value.filter(n => n.status === 'online').length)
const allOnline = computed(() => nodes.value.length > 0 && onlineCount.value === nodes.value.length)
</script>

<template>
  <div class="p-6 space-y-6">
    <!-- Header -->
    <div class="flex items-start justify-between">
      <div>
        <h2 class="text-tp-text font-display font-bold text-3xl">Regional Nodes</h2>
        <p class="text-tp-outline text-sm mt-1">Manage and monitor your distributed Hytale compute units.</p>
      </div>
      <div class="flex items-center gap-3">
        <span v-if="allOnline"
          class="flex items-center gap-2 text-xs font-semibold text-tp-tertiary bg-tp-tertiary/10 px-3 py-1.5 rounded-full">
          <span class="w-2 h-2 rounded-full bg-tp-tertiary animate-pulse" />
          ALL SYSTEMS OPERATIONAL
        </span>
        <span v-else-if="nodes.length > 0"
          class="flex items-center gap-2 text-xs font-semibold text-tp-warning bg-tp-warning/10 px-3 py-1.5 rounded-full">
          <span class="w-2 h-2 rounded-full bg-tp-warning" />
          {{ onlineCount }}/{{ nodes.length }} ONLINE
        </span>
        <UiButton variant="secondary" size="sm" :loading="loading" @click="fetchNodes">
          <span class="material-symbols-outlined text-base">refresh</span>
        </UiButton>
        <UiButton v-if="isAdmin" variant="primary" size="md" @click="showRegisterModal = true">
          <span class="material-symbols-outlined text-base">add</span>
          Add Node
        </UiButton>
      </div>
    </div>

    <!-- Error -->
    <div v-if="error" class="bg-tp-error/10 rounded-xl px-4 py-3 text-tp-error text-sm">
      {{ error }}
    </div>

    <!-- Loading -->
    <div v-if="loading && nodes.length === 0" class="grid grid-cols-1 lg:grid-cols-3 gap-5">
      <div class="lg:col-span-2 grid grid-cols-1 md:grid-cols-2 gap-5">
        <div v-for="i in 4" :key="i" class="bg-tp-surface2 rounded-xl p-6 animate-pulse h-64" />
      </div>
      <div class="bg-tp-surface2 rounded-xl animate-pulse h-96" />
    </div>

    <!-- Empty -->
    <div v-else-if="nodes.length === 0 && !loading"
      class="bg-tp-surface2 rounded-xl p-16 text-center">
      <div class="w-16 h-16 bg-tp-surface3 rounded-2xl flex items-center justify-center mx-auto mb-4">
        <span class="material-symbols-outlined text-3xl text-tp-muted">lan</span>
      </div>
      <h4 class="text-tp-text font-display font-semibold text-lg mb-2">No nodes registered</h4>
      <p class="text-tp-muted text-sm mb-6">Register a daemon node to host game servers.</p>
      <UiButton v-if="isAdmin" variant="primary" size="md" @click="showRegisterModal = true">
        <span class="material-symbols-outlined text-base">add</span>
        Register your first node
      </UiButton>
    </div>

    <!-- Main content: Node grid + Cluster Overview -->
    <div v-else class="grid grid-cols-1 lg:grid-cols-3 gap-5">
      <!-- Node cards — 2 columns on the left -->
      <div class="lg:col-span-2 grid grid-cols-1 md:grid-cols-2 gap-5">
        <div v-for="node in nodes" :key="node.id"
          class="bg-tp-surface2 rounded-xl p-5 flex flex-col gap-4">
          <!-- Top row: label + status badge -->
          <div class="flex items-center justify-between">
            <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">COMPUTE UNIT</span>
            <span :class="['text-[10px] uppercase tracking-widest font-semibold px-2.5 py-1 rounded-full', statusColor(node.status)]">
              {{ node.status.toUpperCase() }}
            </span>
          </div>

          <!-- Node name -->
          <div>
            <h3 class="text-tp-text font-display font-bold text-xl">{{ node.name }}</h3>
            <div class="flex items-center gap-3 mt-1 text-tp-outline text-xs">
              <span class="flex items-center gap-1">
                <span class="material-symbols-outlined text-sm">location_on</span>
                {{ node.location || 'Unknown' }}
              </span>
              <span class="flex items-center gap-1">
                <span class="material-symbols-outlined text-sm">schedule</span>
                Uptime: {{ uptimeFromHeartbeat(node.last_heartbeat) }}
              </span>
            </div>
          </div>

          <!-- CPU Load -->
          <div>
            <div class="flex items-center justify-between mb-1.5">
              <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">CPU LOAD</span>
              <span class="text-tp-text text-xs font-semibold">
                {{ node.status === 'online' && nodeMetrics[node.id] != null
                  ? (nodeMetrics[node.id]!.cpu_pct.toFixed(1) + '%')
                  : '\u2014' }}
              </span>
            </div>
            <div class="h-2 bg-tp-surface-highest rounded-full overflow-hidden">
              <div
                class="h-full rounded-full progress-fill transition-all duration-700"
                :style="{ width: node.status === 'online' && nodeMetrics[node.id] != null
                  ? (nodeMetrics[node.id]!.cpu_pct) + '%'
                  : '0%' }"
              />
            </div>
          </div>

          <!-- Memory Usage -->
          <div>
            <div class="flex items-center justify-between mb-1.5">
              <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">MEMORY USAGE</span>
              <span class="text-tp-text text-xs font-semibold">
                {{ node.status === 'online' && nodeMetrics[node.id] != null
                  ? formatMB(nodeMetrics[node.id]!.ram_used_mb) + ' / ' + formatMB(node.total_ram_mb)
                  : '\u2014' }}
              </span>
            </div>
            <div class="h-2 bg-tp-surface-highest rounded-full overflow-hidden">
              <div
                class="h-full rounded-full progress-fill transition-all duration-700"
                :style="{ width: node.status === 'online' && nodeMetrics[node.id] != null && node.total_ram_mb > 0
                  ? (nodeMetrics[node.id]!.ram_used_mb / node.total_ram_mb * 100).toFixed(1) + '%'
                  : '0%' }"
              />
            </div>
          </div>

          <!-- Disk Storage -->
          <div>
            <div class="flex items-center justify-between mb-1.5">
              <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">DISK STORAGE</span>
              <span class="text-tp-text text-xs font-semibold">
                {{ node.status === 'online' && nodeMetrics[node.id] != null
                  ? formatMB(nodeMetrics[node.id]!.disk_used_mb) + ' / ' + formatMB(node.total_disk_mb)
                  : '\u2014' }}
              </span>
            </div>
            <div class="h-2 bg-tp-surface-highest rounded-full overflow-hidden">
              <div
                class="h-full rounded-full progress-fill transition-all duration-700"
                :style="{ width: node.status === 'online' && nodeMetrics[node.id] != null && node.total_disk_mb > 0
                  ? (nodeMetrics[node.id]!.disk_used_mb / node.total_disk_mb * 100).toFixed(1) + '%'
                  : '0%' }"
              />
            </div>
          </div>

          <!-- Footer: capacity + actions -->
          <div class="flex items-center justify-between mt-auto pt-2">
            <span class="text-tp-outline text-xs">
              Capacity:
              {{ node.status === 'online' && nodeMetrics[node.id] != null
                ? nodeMetrics[node.id]!.active_servers
                : 0 }}/{{ node.max_servers }} Servers
            </span>
            <div class="flex items-center gap-2">
              <UiButton
                v-if="isAdmin"
                variant="ghost"
                size="sm"
                @click="openEditModal(node)"
              >
                <span class="material-symbols-outlined text-sm">edit</span>
              </UiButton>
              <UiButton
                v-if="isAdmin"
                variant="danger"
                size="sm"
                :loading="deletingId === node.id"
                :disabled="!!deletingId"
                @click="deleteNode(node.id, node.name)"
              >
                <span class="material-symbols-outlined text-sm">delete</span>
              </UiButton>
              <NuxtLink :to="`/nodes/${node.id}`"
                class="w-8 h-8 rounded-xl bg-tp-surface-highest flex items-center justify-center text-tp-outline hover:text-tp-text transition-colors">
                <span class="material-symbols-outlined text-base">arrow_forward</span>
              </NuxtLink>
            </div>
          </div>
        </div>
      </div>

      <!-- Right column: Cluster Overview -->
      <div class="flex flex-col gap-5">
        <div class="bg-tp-surface2 rounded-xl flex flex-col overflow-hidden h-fit">
          <div class="px-5 py-3 flex items-center justify-between border-b border-tp-surface3">
            <div class="flex items-center gap-2">
              <span class="w-2 h-2 rounded-full bg-tp-primary animate-pulse" />
              <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">CLUSTER OVERVIEW</span>
            </div>
          </div>
          <div class="px-5 py-4 space-y-4">
            <!-- Total Nodes -->
            <div class="flex items-center justify-between">
              <span class="text-xs text-tp-outline">Total Nodes</span>
              <span class="text-tp-text text-sm font-semibold font-display">
                {{ clusterStats?.total_nodes ?? nodes.length }}
              </span>
            </div>
            <!-- Online Nodes -->
            <div class="flex items-center justify-between">
              <span class="text-xs text-tp-outline">Online Nodes</span>
              <span class="text-tp-tertiary text-sm font-semibold font-display">
                {{ clusterStats?.online_nodes ?? onlineCount }}
              </span>
            </div>
            <!-- Running Servers -->
            <div class="flex items-center justify-between">
              <span class="text-xs text-tp-outline">Running Servers</span>
              <span class="text-tp-text text-sm font-semibold font-display">
                {{ clusterStats?.running_servers ?? '\u2014' }}
              </span>
            </div>
            <!-- Avg CPU -->
            <div class="flex items-center justify-between">
              <span class="text-xs text-tp-outline">Avg CPU</span>
              <span class="text-tp-text text-sm font-semibold font-display">
                {{ clusterStats != null ? clusterStats.avg_cpu_pct.toFixed(1) + '%' : '\u2014' }}
              </span>
            </div>
            <!-- RAM Usage -->
            <div>
              <div class="flex items-center justify-between mb-1.5">
                <span class="text-xs text-tp-outline">RAM Usage</span>
                <span class="text-tp-text text-xs font-semibold">
                  {{ clusterStats != null
                    ? formatMB(clusterStats.used_ram_mb) + ' / ' + formatMB(clusterStats.total_ram_mb)
                    : '\u2014' }}
                </span>
              </div>
              <div class="h-1.5 bg-tp-surface-highest rounded-full overflow-hidden">
                <div
                  class="h-full rounded-full progress-fill transition-all duration-700"
                  :style="{ width: clusterStats && clusterStats.total_ram_mb > 0
                    ? (clusterStats.used_ram_mb / clusterStats.total_ram_mb * 100).toFixed(1) + '%'
                    : '0%' }"
                />
              </div>
            </div>
            <!-- Disk Usage -->
            <div>
              <div class="flex items-center justify-between mb-1.5">
                <span class="text-xs text-tp-outline">Disk Usage</span>
                <span class="text-tp-text text-xs font-semibold">
                  {{ clusterStats != null
                    ? formatMB(clusterStats.used_disk_mb) + ' / ' + formatMB(clusterStats.total_disk_mb)
                    : '\u2014' }}
                </span>
              </div>
              <div class="h-1.5 bg-tp-surface-highest rounded-full overflow-hidden">
                <div
                  class="h-full rounded-full progress-fill transition-all duration-700"
                  :style="{ width: clusterStats && clusterStats.total_disk_mb > 0
                    ? (clusterStats.used_disk_mb / clusterStats.total_disk_mb * 100).toFixed(1) + '%'
                    : '0%' }"
                />
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Expand Infrastructure CTA -->
    <div v-if="nodes.length > 0"
      class="relative rounded-xl overflow-hidden bg-gradient-to-r from-tp-primary/20 via-tp-tertiary/10 to-tp-primary/5 p-8 flex items-center justify-between">
      <div>
        <h3 class="text-tp-text font-display font-bold text-xl mb-1">Expand Infrastructure</h3>
        <p class="text-tp-outline text-sm">Scale your Hytale network by provisioning additional compute nodes across regions.</p>
      </div>
      <UiButton v-if="isAdmin" variant="primary" size="md" @click="showRegisterModal = true">
        <span class="material-symbols-outlined text-base">add_circle</span>
        PROVISION NODE
      </UiButton>
    </div>

    <!-- Network Overview stats -->
    <div v-if="nodes.length > 0" class="grid grid-cols-3 gap-5">
      <div class="bg-tp-surface2 rounded-xl p-5 text-center">
        <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-2">TOTAL NODES</p>
        <p class="text-tp-text font-display font-bold text-3xl">{{ clusterStats?.total_nodes ?? nodes.length }}</p>
      </div>
      <div class="bg-tp-surface2 rounded-xl p-5 text-center">
        <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-2">RUNNING SERVERS</p>
        <p class="text-tp-text font-display font-bold text-3xl">{{ clusterStats?.running_servers ?? '\u2014' }}</p>
      </div>
      <div class="bg-tp-surface2 rounded-xl p-5 text-center">
        <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-2">AVG CPU LOAD</p>
        <p class="text-tp-text font-display font-bold text-3xl">
          {{ clusterStats != null ? clusterStats.avg_cpu_pct.toFixed(1) + '%' : '\u2014' }}
        </p>
      </div>
    </div>

    <!-- Enrollment Modal -->
    <UiModal :open="showRegisterModal" title="Add Node" size="lg" @close="closeModal">
      <!-- Success state: token + install command -->
      <div v-if="enrollment" class="space-y-4">
        <div class="bg-tp-warning/10 rounded-xl px-4 py-3 text-tp-warning text-sm">
          This token is valid for 15 minutes and can only be used once. Copy it now — it will not be shown again.
        </div>
        <div>
          <label class="text-tp-outline text-[10px] font-semibold uppercase tracking-widest block mb-1.5">Enrollment Token</label>
          <div class="flex items-center gap-2">
            <code class="flex-1 block bg-tp-surface-lowest rounded-xl px-3 py-2.5 text-tp-tertiary text-xs font-mono break-all">
              {{ enrollment.token }}
            </code>
            <UiButton variant="secondary" size="sm" @click="copy(enrollment.token, 'token')">
              <span class="material-symbols-outlined text-base">{{ tokenCopied ? 'check' : 'content_copy' }}</span>
            </UiButton>
          </div>
        </div>
        <div>
          <label class="text-tp-outline text-[10px] font-semibold uppercase tracking-widest block mb-1.5">Run this on the daemon host</label>
          <div class="flex items-start gap-2">
            <pre class="flex-1 bg-tp-surface-lowest rounded-xl px-3 py-2.5 text-tp-text text-xs font-mono whitespace-pre-wrap break-all">{{ installCommand }}</pre>
            <UiButton variant="secondary" size="sm" @click="copy(installCommand, 'cmd')">
              <span class="material-symbols-outlined text-base">{{ cmdCopied ? 'check' : 'content_copy' }}</span>
            </UiButton>
          </div>
          <p class="text-tp-outline text-xs mt-2">
            Once the daemon redeems the token, the new node will appear in the list.
          </p>
        </div>
      </div>

      <!-- Form -->
      <form v-else class="space-y-4" @submit.prevent="submitRegister">
        <div v-if="registerError" class="bg-tp-error/10 rounded-xl px-3 py-2.5 text-tp-error text-sm">
          {{ registerError }}
        </div>
        <p class="text-tp-outline text-sm">
          Create a one-shot enrollment token. Copy the token, then run the generated install command on the daemon host — the daemon self-registers using the token.
        </p>
        <UiInput v-model="registerForm.name" label="Node Name" placeholder="prod-node-01" :required="true" />
        <div class="grid grid-cols-2 gap-3">
          <UiInput v-model="registerForm.total_cpu" type="number" label="CPU Cores" placeholder="4" />
          <UiInput v-model="registerForm.max_servers" type="number" label="Max Servers" placeholder="10" />
        </div>
        <div class="grid grid-cols-2 gap-3">
          <UiInput v-model="registerForm.total_ram_mb" type="number" label="Total RAM (MB)" placeholder="8192" />
          <UiInput v-model="registerForm.total_disk_mb" type="number" label="Total Disk (MB)" placeholder="102400" />
        </div>
      </form>

      <template #footer>
        <UiButton variant="ghost" size="md" @click="closeModal">
          {{ enrollment ? 'Done' : 'Cancel' }}
        </UiButton>
        <UiButton v-if="!enrollment" variant="primary" size="md" :loading="registerLoading" @click="submitRegister">
          Create Enrollment Token
        </UiButton>
      </template>
    </UiModal>

    <!-- Edit Modal -->
    <UiModal :open="showEditModal" title="Edit Node" size="md" @close="closeEditModal">
      <form class="space-y-4" @submit.prevent="submitEdit">
        <div v-if="editError" class="bg-tp-error/10 rounded-xl px-3 py-2.5 text-tp-error text-sm">
          {{ editError }}
        </div>
        <UiInput v-model="editForm.name" label="Node Name" placeholder="prod-node-01" />
        <UiInput v-model="editForm.location" label="Location" placeholder="US-East" />
        <UiInput v-model="editForm.max_servers" type="number" label="Max Servers" placeholder="10" />
      </form>

      <template #footer>
        <UiButton variant="ghost" size="md" @click="closeEditModal">Cancel</UiButton>
        <UiButton variant="primary" size="md" :loading="editLoading" @click="submitEdit">Save</UiButton>
      </template>
    </UiModal>

    <!-- Toast -->
    <Transition name="toast">
      <div v-if="toast" :class="[
        'fixed bottom-6 right-6 z-50 flex items-center gap-3 px-4 py-3 rounded-xl shadow-ambient text-sm font-medium',
        toastType === 'success' ? 'bg-tp-success/15 text-tp-success' : 'bg-tp-error/15 text-tp-error',
      ]">
        <div :class="['w-2 h-2 rounded-full', toastType === 'success' ? 'bg-tp-success' : 'bg-tp-error']" />
        {{ toast }}
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.toast-enter-active, .toast-leave-active { transition: opacity 0.25s ease, transform 0.25s ease; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateY(8px); }

.progress-fill {
  background: linear-gradient(90deg, #3b82f6, #89ceff);
}
</style>
