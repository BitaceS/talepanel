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
    loadError.value = apiError(err, 'Failed to load scheduled tasks')
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch(() => props.serverId, load)

// ── Cron presets and hints ─────────────────────────────────────────────────
const PRESETS = [
  { id: 'hourly', label: 'Every hour', cron: '0 * * * *' },
  { id: 'every6h', label: 'Every 6 hours', cron: '0 */6 * * *' },
  { id: 'daily4', label: 'Daily at 04:00', cron: '0 4 * * *' },
  { id: 'weekly', label: 'Weekly, Sunday 04:00', cron: '0 4 * * 0' },
  { id: 'custom', label: 'Custom (cron)', cron: '' },
] as const

const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday']

const isNum = (s: string) => /^\d+$/.test(s)
const pad = (s: string) => s.padStart(2, '0')

// describeCron gives a readable hint for the common shapes and falls back to
// "Custom schedule" for anything else.
function describeCron(expr: string): string {
  const f = expr.trim().split(/\s+/)
  if (f.length !== 5) return 'Invalid expression'
  const [min = '', hour = '', dom = '', mon = '', dow = ''] = f
  const everyDay = dom === '*' && mon === '*'
  if (everyDay && dow === '*') {
    if (min === '0' && hour === '*') return 'Every hour'
    const stepMin = /^\*\/(\d+)$/.exec(min)
    if (stepMin && hour === '*') return `Every ${stepMin[1]} minutes`
    const stepHour = /^\*\/(\d+)$/.exec(hour)
    if (stepHour && isNum(min)) return `Every ${stepHour[1]} hours at :${pad(min)}`
    if (isNum(min) && hour === '*') return `Every hour at :${pad(min)}`
    if (isNum(min) && isNum(hour)) return `Daily at ${pad(hour)}:${pad(min)}`
  }
  if (everyDay && isNum(dow) && isNum(min) && isNum(hour)) {
    return `Weekly on ${WEEKDAYS[Number(dow)] ?? '?'} at ${pad(hour)}:${pad(min)}`
  }
  return 'Custom schedule'
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
    emit('toast', 'Task scheduled', 'success')
  } catch (err: unknown) {
    formError.value = apiError(err, 'Failed to create task')
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
    tasks.value = tasks.value.map(t => (t.id === task.id ? data.task : t))
  } catch (err: unknown) {
    emit('toast', apiError(err, 'Failed to update task'), 'error')
  } finally {
    busy.value = null
  }
}

async function remove(task: ServerTask) {
  if (!window.confirm('Delete this scheduled task?')) return
  busy.value = task.id
  try {
    await api.delete(`/servers/${props.serverId}/tasks/${task.id}`)
    tasks.value = tasks.value.filter(t => t.id !== task.id)
    emit('toast', 'Task deleted', 'success')
  } catch (err: unknown) {
    emit('toast', apiError(err, 'Failed to delete task'), 'error')
  } finally {
    busy.value = null
  }
}

// ── Display helpers ────────────────────────────────────────────────────────
const TYPE_LABEL: Record<TaskType, string> = {
  restart: 'Restart',
  update: 'Hytale update',
  command: 'Console command',
  broadcast: 'Broadcast',
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
  const warning = warn > 0 ? `Warns players ${warn} minute${warn === 1 ? '' : 's'} before` : 'No warning'
  return task.type === 'update' ? `Only when a newer build exists · backup first · ${warning}` : warning
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
        <h3 class="text-tp-text font-display font-semibold text-lg">Scheduled tasks</h3>
        <p class="text-tp-muted text-sm">Restarts, Hytale updates, console commands and broadcasts on a schedule. Updates also run on a stopped server; everything else only while it is running.</p>
      </div>
      <UiButton v-if="!showForm" variant="primary" size="sm" @click="openForm">
        <Plus class="w-3.5 h-3.5" /> New Task
      </UiButton>
    </div>

    <!-- Create form -->
    <div v-if="showForm" class="bg-tp-surface rounded-xl p-5 space-y-3">
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label :class="labelClass">Action</label>
          <select v-model="form.type" :class="inputClass">
            <option value="restart">Restart server</option>
            <option value="update">Hytale update (maintenance window)</option>
            <option value="command">Console command</option>
            <option value="broadcast">Broadcast message</option>
          </select>
        </div>
        <div>
          <label :class="labelClass">Schedule</label>
          <select v-model="form.preset" :class="inputClass">
            <option v-for="p in PRESETS" :key="p.id" :value="p.id">{{ p.label }}</option>
          </select>
        </div>
      </div>

      <div>
        <label :class="labelClass">Cron expression</label>
        <input v-model="form.cron" :class="[inputClass, 'font-mono']" placeholder="minute hour day month weekday"
          :readonly="form.preset !== 'custom'" />
        <p class="text-tp-muted text-xs mt-1">
          {{ describeCron(form.cron) }} · panel server time zone · at most every 5 minutes
        </p>
      </div>

      <div v-if="form.type === 'restart' || form.type === 'update'">
        <label :class="labelClass">Warn players beforehand (minutes, 0–15)</label>
        <input v-model.number="form.warn_minutes" type="number" min="0" max="15" :class="inputClass" />
        <p v-if="form.type === 'update'" class="text-tp-muted text-xs mt-1">
          At each run the panel checks the server's channel for a newer build. Only then it warns the players,
          takes a backup, stops the server, installs the build and starts it again. Nothing happens when it is up to date.
        </p>
      </div>
      <div v-else-if="form.type === 'command'">
        <label :class="labelClass">Command</label>
        <input v-model="form.command" :class="[inputClass, 'font-mono']" placeholder="e.g. world save" />
        <p class="text-tp-muted text-xs mt-1">Runs with your console role; commands above it are refused.</p>
      </div>
      <div v-else>
        <label :class="labelClass">Message</label>
        <input v-model="form.message" :class="inputClass" maxlength="200" placeholder="e.g. Join our Discord!" />
        <p class="text-xs mt-1" :class="messageTooLong ? 'text-tp-danger' : 'text-tp-muted'">{{ form.message.length }}/200</p>
      </div>

      <p v-if="formError" class="text-tp-danger text-xs">{{ formError }}</p>
      <div class="flex gap-2">
        <UiButton variant="primary" size="sm" :loading="saving" :disabled="messageTooLong" @click="createTask">Schedule</UiButton>
        <UiButton variant="secondary" size="sm" @click="showForm = false">Cancel</UiButton>
      </div>
    </div>

    <!-- List -->
    <div v-if="loading" class="bg-tp-surface rounded-xl p-8 text-center">
      <Loader2 class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
      <p class="text-tp-muted text-sm">Loading tasks...</p>
    </div>
    <div v-else-if="loadError" class="bg-tp-surface rounded-xl p-8 text-center">
      <p class="text-tp-danger text-sm">{{ loadError }}</p>
    </div>
    <div v-else-if="tasks.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
      <CalendarClock class="w-8 h-8 text-tp-muted mx-auto mb-3" />
      <p class="text-tp-text font-display font-semibold text-sm mb-1">No scheduled tasks</p>
      <p class="text-tp-muted text-xs">Schedule a nightly restart or a recurring broadcast.</p>
    </div>
    <div v-else class="space-y-2">
      <div v-for="t in tasks" :key="t.id" class="bg-tp-surface rounded-xl p-4 flex items-start justify-between gap-3"
        :class="{ 'opacity-60': !t.enabled }">
        <div class="flex items-start gap-3 min-w-0">
          <component :is="taskIcon(t.type)" class="w-5 h-5 text-tp-muted shrink-0 mt-0.5" />
          <div class="min-w-0">
            <p class="text-tp-text text-sm font-medium">
              {{ TYPE_LABEL[t.type] }}
              <span class="text-tp-muted font-normal">· {{ describeCron(t.cron_expr) }}</span>
              <code class="ml-1 text-[11px] text-tp-outline font-mono">{{ t.cron_expr }}</code>
            </p>
            <p class="text-tp-muted text-xs truncate" :class="{ 'font-mono': t.type === 'command' || t.type === 'broadcast' }">{{ taskDetail(t) }}</p>
            <p class="text-tp-muted text-xs mt-1">
              Next: {{ t.enabled ? fmtTime(t.next_run_at) : 'disabled' }} · Last: {{ fmtTime(t.last_run_at) }}
            </p>
            <div v-if="t.last_status" class="mt-1.5 flex items-center gap-2 min-w-0">
              <UiBadge :variant="statusVariant(t.last_status)" size="sm">{{ t.last_status }}</UiBadge>
              <span v-if="t.last_error" class="text-tp-muted text-xs truncate" :title="t.last_error">{{ t.last_error }}</span>
            </div>
          </div>
        </div>
        <div class="flex items-center gap-1 shrink-0">
          <button class="p-1.5 rounded-xl transition-colors"
            :class="t.enabled ? 'text-tp-success hover:bg-tp-success/10' : 'text-tp-muted hover:bg-tp-surface2'"
            :title="t.enabled ? 'Disable' : 'Enable'" :disabled="busy === t.id" @click="toggle(t)">
            <Power v-if="t.enabled" class="w-3.5 h-3.5" />
            <PowerOff v-else class="w-3.5 h-3.5" />
          </button>
          <button class="p-1.5 rounded-xl text-tp-danger hover:bg-tp-danger/10 transition-colors"
            title="Delete" :disabled="busy === t.id" @click="remove(t)">
            <Trash2 class="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
