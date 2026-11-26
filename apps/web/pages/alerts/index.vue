<script setup lang="ts">
import { AlertTriangle, Bell, CheckCircle, Info, Plus, RefreshCw, ToggleLeft, ToggleRight, Trash2 } from '@lucide/vue'
import { useAlertsStore } from '~/stores/alerts'
import { useServersStore } from '~/stores/servers'
import { useAuthStore } from '~/stores/auth'

definePageMeta({ title: 'pages.alerts', middleware: 'auth' })

const alertsStore = useAlertsStore()
const serversStore = useServersStore()
const authStore = useAuthStore()
const { t, te } = useI18n()

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
const ruleForm = reactive({ server_id: '', type: 'server_crashed', threshold: 90, channels: ['email'] as string[], webhook_url: '' })
const creating = ref(false)

// Node-level rules and global (all servers) rules are admin-only in the API.
const allRuleTypes = [
  { value: 'server_crashed', label: 'alerts.page.types.server_crashed', adminOnly: false },
  { value: 'cpu_high', label: 'alerts.page.types.cpu_high', adminOnly: true },
  { value: 'ram_high', label: 'alerts.page.types.ram_high', adminOnly: true },
  { value: 'disk_high', label: 'alerts.page.types.disk_high', adminOnly: true },
  { value: 'node_offline', label: 'alerts.page.types.node_offline', adminOnly: true },
  { value: 'ddos', label: 'alerts.page.types.ddos', adminOnly: true },
]
const ruleTypes = computed(() => allRuleTypes.filter(rt => authStore.isAdmin || !rt.adminOnly))

function openCreateModal() {
  if (!authStore.isAdmin && !ruleForm.server_id) {
    ruleForm.server_id = serversStore.servers[0]?.id ?? ''
  }
  showCreateModal.value = true
}

// Threshold only applies to percentage-metric rule types.
const thresholdTypes = ['cpu_high', 'ram_high', 'disk_high']
const showThreshold = computed(() => thresholdTypes.includes(ruleForm.type))

const channelOptions = [
  { value: 'email', label: 'alerts.page.channels.email' },
  { value: 'discord', label: 'alerts.page.channels.discord' },
  { value: 'webhook', label: 'alerts.page.channels.webhook' },
]
// Discord and generic webhook both deliver to the rule's webhook URL.
const needsWebhookUrl = computed(() =>
  ruleForm.channels.includes('discord') || ruleForm.channels.includes('webhook'),
)

function toggleChannel(value: string) {
  const i = ruleForm.channels.indexOf(value)
  if (i === -1) ruleForm.channels.push(value)
  else ruleForm.channels.splice(i, 1)
}

async function createRule() {
  creating.value = true
  try {
    await alertsStore.createRule({
      server_id: ruleForm.server_id || undefined,
      type: ruleForm.type,
      threshold: ruleForm.threshold,
      channels: ruleForm.channels,
      webhook_url: ruleForm.webhook_url || undefined,
    })
    showCreateModal.value = false
    showToast(t('alerts.page.ruleCreated'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? t('alerts.page.ruleCreateFailed'), 'error')
  } finally {
    creating.value = false
  }
}

async function toggleRule(ruleId: string, enabled: boolean) {
  try {
    await alertsStore.toggleRule(ruleId, enabled)
    showToast(enabled ? t('alerts.page.ruleEnabled') : t('alerts.page.ruleDisabled'))
  } catch { showToast(t('alerts.page.ruleToggleFailed'), 'error') }
}

async function deleteRule(ruleId: string) {
  if (!confirm(t('alerts.page.ruleDeleteConfirm'))) return
  try {
    await alertsStore.deleteRule(ruleId)
    showToast(t('alerts.page.ruleDeleted'))
  } catch { showToast(t('alerts.page.ruleDeleteFailed'), 'error') }
}

async function resolveEvent(eventId: string) {
  try {
    await alertsStore.resolveEvent(eventId)
    showToast(t('alerts.page.eventResolved'))
  } catch { showToast(t('alerts.page.resolveFailed'), 'error') }
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
  if (s < 60) return t('alerts.page.secondsAgo', { n: s })
  if (s < 3600) return t('time.minutesAgo', { n: Math.floor(s / 60) })
  if (s < 86400) return t('time.hoursAgo', { n: Math.floor(s / 3600) })
  return t('time.daysAgo', { n: Math.floor(s / 86400) })
}

function typeLabel(type: string): string {
  return te(`alerts.page.types.${type}`) ? t(`alerts.page.types.${type}`) : type.replace(/_/g, ' ')
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
      <h2 class="text-tp-text font-display font-bold text-2xl">{{ t('pages.alerts') }}</h2>
      <UiButton variant="secondary" size="sm" @click="alertsStore.fetchEvents(); alertsStore.fetchRules()">
        <RefreshCw class="w-3.5 h-3.5" /> {{ t('actions.refresh') }}
      </UiButton>
    </div>

    <!-- Tabs -->
    <div class="flex gap-1 bg-tp-surface rounded-xl p-1 w-fit">
      <button v-for="tab in [{ key: 'events', label: t('alerts.page.tabEvents'), count: alertsStore.events.length }, { key: 'rules', label: t('alerts.page.tabRules'), count: alertsStore.rules.length }]" :key="tab.key"
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
        <p class="text-tp-text font-display font-semibold mb-1">{{ t('alerts.page.noEvents') }}</p>
        <p class="text-tp-muted text-sm">{{ t('alerts.page.noEventsText') }}</p>
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
              <span class="text-xs font-medium px-2 py-0.5 rounded-full" :class="severityColor(event.severity)">{{ te(`alerts.page.severity.${event.severity}`) ? t(`alerts.page.severity.${event.severity}`) : event.severity }}</span>
              <span class="text-xs opacity-60">{{ typeLabel(event.type) }}</span>
            </div>
          </div>
          <button v-if="!event.resolved" :title="t('alerts.page.resolve')" class="p-1.5 rounded-lg hover:bg-black/10 transition-colors shrink-0" @click="resolveEvent(event.id)">
            <CheckCircle class="w-4 h-4" />
          </button>
          <CheckCircle v-else class="w-4 h-4 opacity-40 shrink-0" />
        </div>
      </div>
    </div>

    <!-- Rules tab -->
    <div v-if="activeTab === 'rules'">
      <div class="flex items-center justify-end mb-4">
        <UiButton variant="primary" size="sm" @click="openCreateModal">
          <Plus class="w-3.5 h-3.5" /> {{ t('alerts.page.newRule') }}
        </UiButton>
      </div>
      <div v-if="alertsStore.rules.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
        <Bell class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-text font-display font-semibold mb-1">{{ t('alerts.page.noRules') }}</p>
        <p class="text-tp-muted text-sm">{{ t('alerts.page.noRulesText') }}</p>
      </div>
      <div v-else class="space-y-3">
        <div v-for="rule in alertsStore.rules" :key="rule.id" class="bg-tp-surface rounded-xl p-4 flex items-center justify-between">
          <div class="flex items-center gap-4">
            <div :class="['w-9 h-9 rounded-xl flex items-center justify-center', rule.enabled ? 'bg-tp-primary/10' : 'bg-tp-surface2']">
              <Bell :class="['w-4 h-4', rule.enabled ? 'text-tp-primary' : 'text-tp-muted']" />
            </div>
            <div>
              <p class="text-tp-text text-sm font-medium capitalize">{{ typeLabel(rule.type) }}</p>
              <p class="text-tp-muted text-xs">
                {{ t('alerts.page.threshold', { value: rule.threshold ?? t('alerts.page.thresholdDefault') }) }}
                <span v-if="rule.server_id"> | {{ t('alerts.page.serverSpecific') }}</span>
                <span v-else> | {{ t('alerts.page.global') }}</span>
              </p>
            </div>
          </div>
          <div class="flex items-center gap-2">
            <button :title="rule.enabled ? t('actions.disable') : t('actions.enable')" class="p-1.5 rounded-lg transition-colors" :class="rule.enabled ? 'text-tp-success' : 'text-tp-muted'" @click="toggleRule(rule.id, !rule.enabled)">
              <ToggleRight v-if="rule.enabled" class="w-5 h-5" />
              <ToggleLeft v-else class="w-5 h-5" />
            </button>
            <button :title="t('actions.delete')" class="p-1.5 rounded-lg text-tp-danger hover:bg-tp-danger/10 transition-colors" @click="deleteRule(rule.id)">
              <Trash2 class="w-4 h-4" />
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Create Rule Modal -->
    <UiModal :open="showCreateModal" :title="t('alerts.page.createRule')" @close="showCreateModal = false">
      <div class="space-y-4">
        <div>
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">{{ t('alerts.page.serverLabel') }}</label>
          <select v-model="ruleForm.server_id" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
            <option v-if="authStore.isAdmin" value="">{{ t('alerts.page.globalAll') }}</option>
            <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </div>
        <div>
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">{{ t('alerts.page.alertType') }}</label>
          <select v-model="ruleForm.type" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
            <option v-for="rt in ruleTypes" :key="rt.value" :value="rt.value">{{ t(rt.label) }}</option>
          </select>
        </div>
        <div v-if="showThreshold">
          <label class="block text-sm font-medium mb-1">{{ t('alerts.page.thresholdLabel') }}</label>
          <input
            v-model.number="ruleForm.threshold"
            type="number"
            min="1"
            max="100"
            placeholder="90"
            class="w-full rounded border border-white/10 bg-white/5 px-3 py-2 text-sm"
          />
        </div>
        <div>
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">{{ t('alerts.page.notifyVia') }}</label>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="ch in channelOptions"
              :key="ch.value"
              type="button"
              :class="[
                'px-3 py-1.5 rounded-xl text-sm border transition-colors',
                ruleForm.channels.includes(ch.value)
                  ? 'bg-tp-primary/15 border-tp-primary text-tp-text'
                  : 'bg-tp-surface2 border-tp-border text-tp-muted hover:text-tp-text',
              ]"
              @click="toggleChannel(ch.value)"
            >
              {{ t(ch.label) }}
            </button>
          </div>
          <p v-if="ruleForm.channels.length === 0" class="text-tp-warning text-xs mt-1">{{ t('alerts.page.selectChannel') }}</p>
        </div>
        <div v-if="needsWebhookUrl">
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">
            {{ ruleForm.channels.includes('discord') ? t('alerts.page.discordWebhookUrl') : t('alerts.page.webhookUrl') }}
          </label>
          <input
            v-model="ruleForm.webhook_url"
            type="url"
            placeholder="https://discord.com/api/webhooks/…"
            class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text"
          />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <UiButton variant="secondary" size="md" @click="showCreateModal = false">{{ t('actions.cancel') }}</UiButton>
          <UiButton
            variant="primary"
            size="md"
            :loading="creating"
            :disabled="ruleForm.channels.length === 0 || (needsWebhookUrl && !ruleForm.webhook_url)"
            @click="createRule"
          >{{ t('actions.create') }}</UiButton>
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
