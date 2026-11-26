<script setup lang="ts">
import { CalendarClock, Plus, Trash2, Loader2, RotateCcw, Terminal, Megaphone, Power, PowerOff, DownloadCloud } from '@lucide/vue'
import { useApi } from '~/composables/useApi'

// Scheduled tasks of one server: restarts, Hytale updates, console commands
// and broadcasts on a 5-field cron. Tasks run as the user who saved them,
// with that user's console role. Updates also run on a stopped server (it
// stays stopped); everything else only while the server is running.

type TaskType = 'restart' | 'update' | 'command' | 'broadcast'

interface TaskPayload {
  command?: string
  message?: string
  warn_minutes?: number
}

interface ServerTask {
  id: string
  server_id: string
  created_by?: string
  type: TaskType
  cron_expr: string
  payload: TaskPayload
  enabled: boolean
  next_run_at?: string
  last_run_at?: string
  last_status?: 'ok' | 'pending' | 'skipped' | 'failed'
  last_error?: string
  created_at: string
}

const props = defineProps<{ serverId: string }>()
const emit = defineEmits<{ toast: [msg: string, type: 'success' | 'error'] }>()

const api = useApi()
const { t } = useI18n()
const tasks = ref<ServerTask[]>([])
const loading = ref(false)
const loadError = ref('')

function apiError(err: unknown, fallback: string): string {
  const e = err as { data?: { error?: string } }
  return e.data?.error ?? fallback
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const data = await api.get<{ tasks: ServerTask[] }>(`/servers/${props.serverId}/tasks`)
    tasks.value = data.tasks ?? []
  } catch (err: unknown) {
    loadError.value = apiError(err, t('servers.tasks.errors.load'))
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch(() => props.serverId, load)

// ── Cron presets and hints ─────────────────────────────────────────────────
// Labels are servers.tasks.presets.<id>.
const PRESETS = [
  { id: 'hourly', cron: '0 * * * *' },
  { id: 'every6h', cron: '0 */6 * * *' },
  { id: 'daily4', cron: '0 4 * * *' },
  { id: 'weekly', cron: '0 4 * * 0' },
  { id: 'custom', cron: '' },
] as const

// i18n keys under servers.tasks.weekdays; cron allows 0 and 7 for Sunday.
const WEEKDAYS = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun']

const isNum = (s: string) => /^\d+$/.test(s)
const pad = (s: string) => s.padStart(2, '0')

// describeCron gives a readable hint for the common shapes and falls back to
// "Custom schedule" for anything else.
function describeCron(expr: string): string {
  const f = expr.trim().split(/\s+/)
  if (f.length !== 5) return t('servers.tasks.cron.invalid')
  const [min = '', hour = '', dom = '', mon = '', dow = ''] = f
  const everyDay = dom === '*' && mon === '*'
  if (everyDay && dow === '*') {
    if (min === '0' && hour === '*') return t('servers.tasks.cron.everyHour')
    const stepMin = /^\*\/(\d+)$/.exec(min)
    if (stepMin && hour === '*') return t('servers.tasks.cron.everyMinutes', { n: stepMin[1] })
    const stepHour = /^\*\/(\d+)$/.exec(hour)
    if (stepHour && isNum(min)) return t('servers.tasks.cron.everyHoursAt', { n: stepHour[1], min: pad(min) })
    if (isNum(min) && hour === '*') return t('servers.tasks.cron.everyHourAt', { min: pad(min) })
    if (isNum(min) && isNum(hour)) return t('servers.tasks.cron.dailyAt', { time: `${pad(hour)}:${pad(min)}` })
  }
  if (everyDay && isNum(dow) && isNum(min) && isNum(hour)) {
    const day = WEEKDAYS[Number(dow)]
    return t('servers.tasks.cron.weeklyOn', {
      day: day ? t(`servers.tasks.weekdays.${day}`) : '?',
      time: `${pad(hour)}:${pad(min)}`,
    })
  }
  return t('servers.tasks.cron.custom')
}

// ── Create form ────────────────────────────────────────────────────────────
const showForm = ref(false)
const saving = ref(false)
const formError = ref('')
const form = reactive({
  type: 'restart' as TaskType,
  preset: 'daily4' as (typeof PRESETS)[number]['id'],
  cron: '0 4 * * *',
  command: '',
  message: '',
  warn_minutes: 5,
})

watch(() => form.preset, (id) => {
  const preset = PRESETS.find(p => p.id === id)
  if (preset && preset.cron) form.cron = preset.cron
})

const messageTooLong = computed(() => form.message.length > 200)

function openForm() {
  form.type = 'restart'
  form.preset = 'daily4'
  form.cron = '0 4 * * *'
  form.command = ''
  form.message = ''
  form.warn_minutes = 5
  formError.value = ''
  showForm.value = true
}

async function createTask() {
  formError.value = ''
  const payload: TaskPayload = {}
  if (form.type === 'restart' || form.type === 'update') payload.warn_minutes = Number(form.warn_minutes) || 0
  if (form.type === 'command') payload.command = form.command.trim()
  if (form.type === 'broadcast') payload.message = form.message.trim()
  saving.value = true
  try {
    const data = await api.post<{ task: ServerTask }>(`/servers/${props.serverId}/tasks`, {
      type: form.type,
      cron_expr: form.cron.trim(),
      payload,
    })
    tasks.value = [data.task, ...tasks.value]
    showForm.value = false
    emit('toast', t('servers.tasks.toast.scheduled'), 'success')
  } catch (err: unknown) {
    formError.value = apiError(err, t('servers.tasks.errors.create'))
  } finally {
    saving.value = false
  }
}

// ── Row actions ────────────────────────────────────────────────────────────
const busy = ref<string | null>(null)

async function toggle(task: ServerTask) {
  busy.value = task.id
  try {
    const data = await api.patch<{ task: ServerTask }>(
      `/servers/${props.serverId}/tasks/${task.id}`,
      { enabled: !task.enabled },
    )
    tasks.value = tasks.value.map(x => (x.id === task.id ? data.task : x))
  } catch (err: unknown) {
    emit('toast', apiError(err, t('servers.tasks.errors.update')), 'error')
  } finally {
    busy.value = null
  }
}

async function remove(task: ServerTask) {
  if (!window.confirm(t('servers.tasks.confirmDelete'))) return
  busy.value = task.id
  try {
    await api.delete(`/servers/${props.serverId}/tasks/${task.id}`)
    tasks.value = tasks.value.filter(x => x.id !== task.id)
    emit('toast', t('servers.tasks.toast.deleted'), 'success')
  } catch (err: unknown) {
    emit('toast', apiError(err, t('servers.tasks.errors.delete')), 'error')
  } finally {
    busy.value = null
  }
}

// ── Display helpers ────────────────────────────────────────────────────────
function typeLabel(type: TaskType): string {
  return t(`servers.tasks.types.${type}`)
}

function statusLabel(status: string): string {
  return ['ok', 'pending', 'skipped', 'failed'].includes(status) ? t(`servers.tasks.status.${status}`) : status
}

function taskIcon(type: TaskType) {
  if (type === 'command') return Terminal
  if (type === 'broadcast') return Megaphone
  if (type === 'update') return DownloadCloud
  return RotateCcw
}

function taskDetail(task: ServerTask): string {
  if (task.type === 'command') return task.payload.command ?? ''
  if (task.type === 'broadcast') return `say ${task.payload.message ?? ''}`
  const warn = task.payload.warn_minutes ?? 0
  const warning = warn > 0 ? t('servers.tasks.detail.warns', warn) : t('servers.tasks.detail.noWarning')
  return task.type === 'update' ? t('servers.tasks.detail.update', { warning }) : warning
}

function statusVariant(status?: string): 'success' | 'warning' | 'danger' | 'muted' | 'primary' {
  switch (status) {
    case 'ok': return 'success'
    case 'pending': return 'primary'
    case 'skipped': return 'warning'
    case 'failed': return 'danger'
    default: return 'muted'
  }
}

function fmtTime(iso?: string): string {
  return iso ? new Date(iso).toLocaleString() : '—'
}

const inputClass = 'w-full bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50'
const labelClass = 'block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1'
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between gap-3">
      <div>
        <h3 class="text-tp-text font-display font-semibold text-lg">{{ t('servers.tasks.title') }}</h3>
        <p class="text-tp-muted text-sm">{{ t('servers.tasks.intro') }}</p>
      </div>
      <UiButton v-if="!showForm" variant="primary" size="sm" @click="openForm">
        <Plus class="w-3.5 h-3.5" /> {{ t('servers.tasks.newTask') }}
      </UiButton>
    </div>

    <!-- Create form -->
    <div v-if="showForm" class="bg-tp-surface rounded-xl p-5 space-y-3">
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label :class="labelClass">{{ t('servers.tasks.action') }}</label>
          <select v-model="form.type" :class="inputClass">
            <option value="restart">{{ t('servers.tasks.options.restart') }}</option>
            <option value="update">{{ t('servers.tasks.options.update') }}</option>
            <option value="command">{{ t('servers.tasks.options.command') }}</option>
            <option value="broadcast">{{ t('servers.tasks.options.broadcast') }}</option>
          </select>
        </div>
        <div>
          <label :class="labelClass">{{ t('servers.tasks.schedule') }}</label>
          <select v-model="form.preset" :class="inputClass">
            <option v-for="p in PRESETS" :key="p.id" :value="p.id">{{ t(`servers.tasks.presets.${p.id}`) }}</option>
          </select>
        </div>
      </div>

      <div>
        <label :class="labelClass">{{ t('servers.tasks.cronExpression') }}</label>
        <input v-model="form.cron" :class="[inputClass, 'font-mono']" :placeholder="t('servers.tasks.cronPlaceholder')"
          :readonly="form.preset !== 'custom'" />
        <p class="text-tp-muted text-xs mt-1">
          {{ t('servers.tasks.cronHint', { desc: describeCron(form.cron) }) }}
        </p>
      </div>

      <div v-if="form.type === 'restart' || form.type === 'update'">
        <label :class="labelClass">{{ t('servers.tasks.warnLabel') }}</label>
        <input v-model.number="form.warn_minutes" type="number" min="0" max="15" :class="inputClass" />
        <p v-if="form.type === 'update'" class="text-tp-muted text-xs mt-1">
          {{ t('servers.tasks.updateHint') }}
        </p>
      </div>
      <div v-else-if="form.type === 'command'">
        <label :class="labelClass">{{ t('servers.tasks.command') }}</label>
        <input v-model="form.command" :class="[inputClass, 'font-mono']" :placeholder="t('servers.tasks.commandPlaceholder')" />
        <p class="text-tp-muted text-xs mt-1">{{ t('servers.tasks.commandHint') }}</p>
      </div>
      <div v-else>
        <label :class="labelClass">{{ t('servers.tasks.message') }}</label>
        <input v-model="form.message" :class="inputClass" maxlength="200" :placeholder="t('servers.tasks.messagePlaceholder')" />
        <p class="text-xs mt-1" :class="messageTooLong ? 'text-tp-danger' : 'text-tp-muted'">{{ form.message.length }}/200</p>
      </div>

      <p v-if="formError" class="text-tp-danger text-xs">{{ formError }}</p>
      <div class="flex gap-2">
        <UiButton variant="primary" size="sm" :loading="saving" :disabled="messageTooLong" @click="createTask">{{ t('servers.tasks.submit') }}</UiButton>
        <UiButton variant="secondary" size="sm" @click="showForm = false">{{ t('actions.cancel') }}</UiButton>
      </div>
    </div>

    <!-- List -->
    <div v-if="loading" class="bg-tp-surface rounded-xl p-8 text-center">
      <Loader2 class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
      <p class="text-tp-muted text-sm">{{ t('servers.tasks.loading') }}</p>
    </div>
    <div v-else-if="loadError" class="bg-tp-surface rounded-xl p-8 text-center">
      <p class="text-tp-danger text-sm">{{ loadError }}</p>
    </div>
    <div v-else-if="tasks.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
      <CalendarClock class="w-8 h-8 text-tp-muted mx-auto mb-3" />
      <p class="text-tp-text font-display font-semibold text-sm mb-1">{{ t('servers.tasks.emptyTitle') }}</p>
      <p class="text-tp-muted text-xs">{{ t('servers.tasks.emptyHint') }}</p>
    </div>
    <div v-else class="space-y-2">
      <div v-for="task in tasks" :key="task.id" class="bg-tp-surface rounded-xl p-4 flex items-start justify-between gap-3"
        :class="{ 'opacity-60': !task.enabled }">
        <div class="flex items-start gap-3 min-w-0">
          <component :is="taskIcon(task.type)" class="w-5 h-5 text-tp-muted shrink-0 mt-0.5" />
          <div class="min-w-0">
            <p class="text-tp-text text-sm font-medium">
              {{ typeLabel(task.type) }}
              <span class="text-tp-muted font-normal">· {{ describeCron(task.cron_expr) }}</span>
              <code class="ml-1 text-[11px] text-tp-outline font-mono">{{ task.cron_expr }}</code>
            </p>
            <p class="text-tp-muted text-xs truncate" :class="{ 'font-mono': task.type === 'command' || task.type === 'broadcast' }">{{ taskDetail(task) }}</p>
            <p class="text-tp-muted text-xs mt-1">
              {{ t('servers.tasks.nextLast', { next: task.enabled ? fmtTime(task.next_run_at) : t('servers.tasks.disabled'), last: fmtTime(task.last_run_at) }) }}
            </p>
            <div v-if="task.last_status" class="mt-1.5 flex items-center gap-2 min-w-0">
              <UiBadge :variant="statusVariant(task.last_status)" size="sm">{{ statusLabel(task.last_status) }}</UiBadge>
              <span v-if="task.last_error" class="text-tp-muted text-xs truncate" :title="task.last_error">{{ task.last_error }}</span>
            </div>
          </div>
        </div>
        <div class="flex items-center gap-1 shrink-0">
          <button class="p-1.5 rounded-xl transition-colors"
            :class="task.enabled ? 'text-tp-success hover:bg-tp-success/10' : 'text-tp-muted hover:bg-tp-surface2'"
            :title="task.enabled ? t('actions.disable') : t('actions.enable')" :disabled="busy === task.id" @click="toggle(task)">
            <Power v-if="task.enabled" class="w-3.5 h-3.5" />
            <PowerOff v-else class="w-3.5 h-3.5" />
          </button>
          <button class="p-1.5 rounded-xl text-tp-danger hover:bg-tp-danger/10 transition-colors"
            :title="t('actions.delete')" :disabled="busy === task.id" @click="remove(task)">
            <Trash2 class="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
