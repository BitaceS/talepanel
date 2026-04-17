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

const nodes = ref<Node[]>([])
const loading = ref(false)
const error = ref('')

async function fetchNodes() {
  loading.value = true
  error.value = ''
  try {
    const data = await api.get<{ nodes: Node[] }>('/nodes')
    nodes.value = data.nodes ?? []
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

// ── Register modal ────────────────────────────────────────────────────────────
const showRegisterModal = ref(false)
const registerLoading = ref(false)
const registerError = ref('')
const registeredNode = ref<{ node: Node; registration_token: string } | null>(null)

const registerForm = reactive({
  name: '',
  fqdn: '',
  port: 8444,
  location: '',
})

async function submitRegister() {
  if (!registerForm.name || !registerForm.fqdn) return
  registerLoading.value = true
  registerError.value = ''
  try {
    const data = await api.post<{ node: Node; registration_token: string }>('/nodes', {
      name: registerForm.name,
      fqdn: registerForm.fqdn,
      port: registerForm.port,
      location: registerForm.location,
    })
    registeredNode.value = data
    nodes.value.push(data.node)
    showToast('Node registered')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    registerError.value = e.data?.error ?? e.message ?? 'Failed to register node'
  } finally {
    registerLoading.value = false
  }
}

function closeModal() {
  showRegisterModal.value = false
  registeredNode.value = null
  registerError.value = ''
  Object.assign(registerForm, { name: '', fqdn: '', port: 8444, location: '' })
}

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
  // If heartbeat is recent, assume node has been up; show a placeholder uptime
  const s = Math.floor(diff / 1000)
  if (s > 300) return '--:--:--'
  return '99.9%'
}

const isAdmin = computed(() =>
  authStore.user?.role === 'admin' || authStore.user?.role === 'owner'
)

const onlineCount = computed(() => nodes.value.filter(n => n.status === 'online').length)
const allOnline = computed(() => nodes.value.length > 0 && onlineCount.value === nodes.value.length)

// Mock live node events
const liveEvents = ref([
  { time: '14:32:01', message: 'Node heartbeat received — dev-node' },
  { time: '14:31:45', message: 'Server hytale-prod-01 status: running' },
  { time: '14:31:12', message: 'Metrics collected — CPU 34%, RAM 76.4 GB' },
  { time: '14:30:58', message: 'Backup job completed — world_alpha.tar.gz' },
  { time: '14:30:22', message: 'Player connected: ShadowMiner_42' },
  { time: '14:29:47', message: 'Mod update check — no updates available' },
  { time: '14:29:01', message: 'Node heartbeat received — dev-node' },
  { time: '14:28:33', message: 'Server hytale-dev-02 status: stopped' },
  { time: '14:28:01', message: 'Disk usage check — 4.2 TB / 10 TB' },
  { time: '14:27:15', message: 'Player disconnected: CraftLord_99' },
])

// Helper: mock CPU load for a node (placeholder)
function mockCpuLoad(_node: Node): number {
  // Offline nodes show 0
  if (_node.status !== 'online') return 0
  // Use a deterministic pseudo-value based on node name hash
  let hash = 0
  for (let i = 0; i < _node.name.length; i++) hash = (hash * 31 + _node.name.charCodeAt(i)) % 100
  return Math.max(12, hash % 68 + 12)
}

// Helper: mock RAM usage for a node
function mockRamUsed(_node: Node): number {
  if (_node.status !== 'online') return 0
  return Math.round(_node.total_ram_mb * 0.6)
}

// Helper: mock disk usage for a node
function mockDiskUsed(_node: Node): number {
  if (_node.status !== 'online') return 0
  return Math.round(_node.total_disk_mb * 0.42)
}

// Helper: mock server count for a node
function mockServerCount(_node: Node): number {
  if (_node.status !== 'online') return 0
  return Math.min(Math.max(1, Math.floor(_node.max_servers * 0.75)), _node.max_servers)
}
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
          Register Node
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

    <!-- Main content: Node grid + Live Events -->
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
              <span class="text-tp-text text-xs font-semibold">{{ node.status === 'online' ? mockCpuLoad(node) + '%' : '\u2014' }}</span>
            </div>
            <div class="h-2 bg-tp-surface-highest rounded-full overflow-hidden">
              <div
                class="h-full rounded-full progress-fill transition-all duration-700"
                :style="{ width: node.status === 'online' ? mockCpuLoad(node) + '%' : '0%' }"
              />
            </div>
          </div>

          <!-- Memory Usage -->
          <div>
            <div class="flex items-center justify-between mb-1.5">
              <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">MEMORY USAGE</span>
              <span class="text-tp-text text-xs font-semibold">
                {{ node.status === 'online' ? formatMB(mockRamUsed(node)) + ' / ' + formatMB(node.total_ram_mb) : '\u2014' }}
              </span>
            </div>
            <div class="h-2 bg-tp-surface-highest rounded-full overflow-hidden">
              <div
                class="h-full rounded-full progress-fill transition-all duration-700"
                :style="{ width: node.status === 'online' && node.total_ram_mb > 0 ? (mockRamUsed(node) / node.total_ram_mb * 100).toFixed(1) + '%' : '0%' }"
              />
            </div>
          </div>

          <!-- Disk Storage -->
          <div>
            <div class="flex items-center justify-between mb-1.5">
              <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">DISK STORAGE</span>
              <span class="text-tp-text text-xs font-semibold">
                {{ node.status === 'online' ? formatMB(mockDiskUsed(node)) + ' / ' + formatMB(node.total_disk_mb) : '\u2014' }}
              </span>
            </div>
            <div class="h-2 bg-tp-surface-highest rounded-full overflow-hidden">
              <div
                class="h-full rounded-full progress-fill transition-all duration-700"
                :style="{ width: node.status === 'online' && node.total_disk_mb > 0 ? (mockDiskUsed(node) / node.total_disk_mb * 100).toFixed(1) + '%' : '0%' }"
              />
            </div>
          </div>

          <!-- Footer: capacity + actions -->
          <div class="flex items-center justify-between mt-auto pt-2">
            <span class="text-tp-outline text-xs">
              Capacity: {{ node.status === 'online' ? mockServerCount(node) : 0 }}/{{ node.max_servers }} Servers
            </span>
            <div class="flex items-center gap-2">
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

      <!-- Right column: Live Node Events -->
      <div class="flex flex-col gap-5">
        <div class="bg-tp-surface2 rounded-xl flex flex-col overflow-hidden h-fit">
          <div class="px-5 py-3 flex items-center justify-between">
            <div class="flex items-center gap-2">
              <span class="w-2 h-2 rounded-full bg-tp-tertiary animate-pulse" />
              <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">LIVE NODE EVENTS</span>
            </div>
            <span class="text-tp-outline text-[10px]">Auto-refresh</span>
          </div>
          <div class="bg-tp-surface-lowest flex-1 max-h-[520px] overflow-y-auto px-4 py-3 space-y-1.5 font-mono text-xs">
            <div v-for="(evt, i) in liveEvents" :key="i" class="flex gap-2 leading-5">
              <span class="text-tp-outline shrink-0">{{ evt.time }}</span>
              <span class="text-tp-tertiary">{{ evt.message }}</span>
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
        <p class="text-tp-text font-display font-bold text-3xl">{{ nodes.length }}</p>
      </div>
      <div class="bg-tp-surface2 rounded-xl p-5 text-center">
        <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-2">TOTAL PLAYERS</p>
        <p class="text-tp-text font-display font-bold text-3xl">&mdash;</p>
      </div>
      <div class="bg-tp-surface2 rounded-xl p-5 text-center">
        <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-2">GLOBAL LATENCY</p>
        <p class="text-tp-text font-display font-bold text-3xl">&mdash;</p>
      </div>
    </div>

    <!-- Register Modal -->
    <UiModal :open="showRegisterModal" title="Register Node" size="md" @close="closeModal">
      <!-- Success state -->
      <div v-if="registeredNode" class="space-y-4">
        <div class="bg-tp-success/10 rounded-xl px-4 py-3 text-tp-success text-sm">
          Node registered successfully! Copy the token below — it will not be shown again.
        </div>
        <div>
          <label class="text-tp-outline text-[10px] font-semibold uppercase tracking-widest block mb-1.5">Registration Token</label>
          <code class="block bg-tp-surface-lowest rounded-xl px-3 py-2.5 text-tp-tertiary text-xs font-mono break-all">
            {{ registeredNode.registration_token }}
          </code>
        </div>
      </div>

      <!-- Form -->
      <form v-else class="space-y-4" @submit.prevent="submitRegister">
        <div v-if="registerError" class="bg-tp-error/10 rounded-xl px-3 py-2.5 text-tp-error text-sm">
          {{ registerError }}
        </div>
        <UiInput v-model="registerForm.name" label="Node Name" placeholder="prod-node-01" :required="true" />
        <UiInput v-model="registerForm.fqdn" label="Hostname / IP" placeholder="node.example.com" :required="true" />
        <div class="grid grid-cols-2 gap-3">
          <UiInput v-model="registerForm.port" type="number" label="Daemon Port" placeholder="8444" />
          <UiInput v-model="registerForm.location" label="Location" placeholder="US-East" />
        </div>
      </form>

      <template #footer>
        <UiButton variant="ghost" size="md" @click="closeModal">
          {{ registeredNode ? 'Close' : 'Cancel' }}
        </UiButton>
        <UiButton v-if="!registeredNode" variant="primary" size="md" :loading="registerLoading" @click="submitRegister">
          Register
        </UiButton>
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
