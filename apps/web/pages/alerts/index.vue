<script setup lang="ts">
import { CheckCircle, Info } from 'lucide-vue-next'
import { useAlertsStore } from '~/stores/alerts'
import { useServersStore } from '~/stores/servers'

definePageMeta({ title: 'Alerts', middleware: 'auth' })

const alertsStore = useAlertsStore()
const serversStore = useServersStore()

const activeTab = ref<'events' | 'rules'>('events')

// Toast
const toast = ref('')
const toastType = ref<'success' | 'error'>('success')
let toastTimer: ReturnType<typeof setTimeout>
function showToast(msg: string, type: 'success' | 'error' = 'success') {
  toast.value = msg; toastType.value = type
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value = '' }, 3000)
}

// Create rule modal
const showCreateModal = ref(false)
const ruleForm = reactive({ server_id: '', type: 'crash', threshold: 3, channels: [] as string[] })
const creating = ref(false)

const ruleTypes = [
  { value: 'crash', label: 'Server Crash' },
  { value: 'cpu_high', label: 'High CPU Usage' },
  { value: 'ram_high', label: 'High RAM Usage' },
  { value: 'disk_high', label: 'High Disk Usage' },
  { value: 'offline', label: 'Server Offline' },
  { value: 'ddos', label: 'DDoS Detected' },
]

async function createRule() {
  creating.value = true
  try {
    await alertsStore.createRule({
      server_id: ruleForm.server_id || undefined,
      type: ruleForm.type,
      threshold: ruleForm.threshold,
      channels: ruleForm.channels,
    })
    showCreateModal.value = false
    showToast('Alert rule created')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to create rule', 'error')
  } finally {
    creating.value = false
  }
}

async function toggleRule(ruleId: string, enabled: boolean) {
  try {
    await alertsStore.toggleRule(ruleId, enabled)
    showToast(enabled ? 'Rule enabled' : 'Rule disabled')
  } catch { showToast('Failed to toggle rule', 'error') }
}

async function deleteRule(ruleId: string) {
  if (!confirm('Delete this alert rule?')) return
  try {
    await alertsStore.deleteRule(ruleId)
    showToast('Rule deleted')
  } catch { showToast('Failed to delete rule', 'error') }
}

async function resolveEvent(eventId: string) {
  try {
    await alertsStore.resolveEvent(eventId)
    showToast('Event resolved')
  } catch { showToast('Failed to resolve', 'error') }
}

function severityColor(severity: string) {
  switch (severity) {
    case 'critical': return 'text-tp-danger bg-tp-danger/10'
    case 'warning': return 'text-tp-warning bg-tp-warning/10'
    default: return 'text-tp-primary bg-tp-primary/10'
  }
}

function severityIcon(severity: string) {
  switch (severity) {
    case 'critical': return AlertTriangle
    case 'warning': return AlertTriangle
    default: return Info
  }
}

function timeAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const s = Math.floor(diff / 1000)
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

onMounted(() => {
  serversStore.fetchServers()
  alertsStore.fetchEvents()
  alertsStore.fetchRules()
})
</script>

<template>
  <div class="p-6 space-y-5">
    <div class="flex items-center justify-between">
      <h2 class="text-tp-text font-display font-bold text-2xl">Alerts</h2>
      <UiButton variant="secondary" size="sm" @click="alertsStore.fetchEvents(); alertsStore.fetchRules()">
        <RefreshCw class="w-3.5 h-3.5" /> Refresh
      </UiButton>
    </div>

    <!-- Tabs -->
    <div class="flex gap-1 bg-tp-surface rounded-xl p-1 w-fit">
      <button v-for="tab in [{ key: 'events', label: 'Events', count: alertsStore.events.length }, { key: 'rules', label: 'Rules', count: alertsStore.rules.length }]" :key="tab.key"
        :class="['flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all', activeTab === tab.key ? 'bg-tp-primary text-white shadow-sm' : 'text-tp-muted hover:text-tp-text hover:bg-tp-surface2']"
        @click="activeTab = tab.key as 'events' | 'rules'">
        {{ tab.label }}
        <span v-if="activeTab === tab.key" class="bg-white/20 text-xs px-1.5 py-0.5 rounded-full">{{ tab.count }}</span>
      </button>
    </div>

    <!-- Events tab -->
    <div v-if="activeTab === 'events'">
      <div v-if="alertsStore.events.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
        <Bell class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-text font-display font-semibold mb-1">No alert events</p>
        <p class="text-tp-muted text-sm">Create alert rules to start receiving notifications.</p>
      </div>
      <div v-else class="space-y-3">
        <div v-for="event in alertsStore.events" :key="event.id"
          :class="['rounded-xl p-4 flex items-start gap-3', event.resolved ? 'opacity-50' : '', severityColor(event.severity)]">
          <component :is="severityIcon(event.severity)" class="w-4 h-4 mt-0.5 shrink-0" />
          <div class="flex-1 min-w-0">
            <div class="flex items-center justify-between gap-2">
              <p class="font-semibold text-sm">{{ event.title }}</p>
              <span class="text-xs opacity-70 shrink-0">{{ timeAgo(event.created_at) }}</span>
            </div>
            <p v-if="event.body" class="text-sm opacity-80 mt-0.5">{{ event.body }}</p>
            <div class="flex items-center gap-2 mt-2">
              <span class="text-xs font-medium px-2 py-0.5 rounded-full" :class="severityColor(event.severity)">{{ event.severity }}</span>
              <span class="text-xs opacity-60">{{ event.type }}</span>
            </div>
          </div>
          <button v-if="!event.resolved" title="Resolve" class="p-1.5 rounded-lg hover:bg-black/10 transition-colors shrink-0" @click="resolveEvent(event.id)">
            <CheckCircle class="w-4 h-4" />
          </button>
          <CheckCircle v-else class="w-4 h-4 opacity-40 shrink-0" />
        </div>
      </div>
    </div>

    <!-- Rules tab -->
    <div v-if="activeTab === 'rules'">
      <div class="flex items-center justify-end mb-4">
        <UiButton variant="primary" size="sm" @click="showCreateModal = true">
          <Plus class="w-3.5 h-3.5" /> New Rule
        </UiButton>
      </div>
      <div v-if="alertsStore.rules.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
        <Bell class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-text font-display font-semibold mb-1">No alert rules</p>
        <p class="text-tp-muted text-sm">Create a rule to get notified about server events.</p>
      </div>
      <div v-else class="space-y-3">
        <div v-for="rule in alertsStore.rules" :key="rule.id" class="bg-tp-surface rounded-xl p-4 flex items-center justify-between">
          <div class="flex items-center gap-4">
            <div :class="['w-9 h-9 rounded-xl flex items-center justify-center', rule.enabled ? 'bg-tp-primary/10' : 'bg-tp-surface2']">
              <Bell :class="['w-4 h-4', rule.enabled ? 'text-tp-primary' : 'text-tp-muted']" />
            </div>
            <div>
              <p class="text-tp-text text-sm font-medium capitalize">{{ rule.type.replace(/_/g, ' ') }}</p>
              <p class="text-tp-muted text-xs">
                Threshold: {{ rule.threshold ?? 'default' }}
                <span v-if="rule.server_id"> | Server-specific</span>
                <span v-else> | Global</span>
              </p>
            </div>
          </div>
          <div class="flex items-center gap-2">
            <button :title="rule.enabled ? 'Disable' : 'Enable'" class="p-1.5 rounded-lg transition-colors" :class="rule.enabled ? 'text-tp-success' : 'text-tp-muted'" @click="toggleRule(rule.id, !rule.enabled)">
              <ToggleRight v-if="rule.enabled" class="w-5 h-5" />
              <ToggleLeft v-else class="w-5 h-5" />
            </button>
            <button title="Delete" class="p-1.5 rounded-lg text-tp-danger hover:bg-tp-danger/10 transition-colors" @click="deleteRule(rule.id)">
              <Trash2 class="w-4 h-4" />
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Create Rule Modal -->
    <UiModal :show="showCreateModal" title="Create Alert Rule" @close="showCreateModal = false">
      <div class="space-y-4">
        <div>
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Server (optional — leave empty for global)</label>
          <select v-model="ruleForm.server_id" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
            <option value="">Global (all servers)</option>
            <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </div>
        <div>
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Alert Type</label>
          <select v-model="ruleForm.type" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
            <option v-for="rt in ruleTypes" :key="rt.value" :value="rt.value">{{ rt.label }}</option>
          </select>
        </div>
        <div v-if="['cpu_high', 'ram_high', 'disk_high'].includes(ruleForm.type)">
          <label class="block text-sm font-medium mb-1">Threshold (%)</label>
          <input
            v-model.number="ruleForm.threshold"
            type="number"
            min="1"
            max="100"
            placeholder="90"
            class="w-full rounded border border-white/10 bg-white/5 px-3 py-2 text-sm"
          />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <UiButton variant="secondary" size="md" @click="showCreateModal = false">Cancel</UiButton>
          <UiButton variant="primary" size="md" :loading="creating" @click="createRule">Create</UiButton>
        </div>
      </div>
    </UiModal>

    <!-- Toast -->
    <Transition name="toast">
      <div v-if="toast" :class="['fixed bottom-6 right-6 z-50 flex items-center gap-3 px-4 py-3 rounded-xl shadow-lg text-sm font-medium', toastType === 'success' ? 'bg-tp-success/15 text-tp-success' : 'bg-tp-error/10 text-tp-error']">
        <div :class="['w-2 h-2 rounded-full', toastType === 'success' ? 'bg-tp-success' : 'bg-tp-danger']" />
        {{ toast }}
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.toast-enter-active, .toast-leave-active { transition: opacity 0.25s ease, transform 0.25s ease; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateY(8px); }
</style>
