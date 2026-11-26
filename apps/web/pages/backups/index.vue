<script setup lang="ts">
import { DatabaseBackup, Calendar, Plus, Download, Trash2, RefreshCw, Play, Pause, Clock, Cloud, UploadCloud, HardDrive, Lock } from '@lucide/vue'
import { useBackupsStore } from '~/stores/backups'
import { useServersStore } from '~/stores/servers'
import { useAuthStore } from '~/stores/auth'
import type { Backup } from '~/types'

definePageMeta({ title: 'pages.backups', middleware: 'auth' })

const backupsStore = useBackupsStore()
const serversStore = useServersStore()
const authStore = useAuthStore()
const { t, te } = useI18n()

type Tab = 'backups' | 'schedules' | 'destinations'
const selectedServer = ref('')
const activeTab = ref<Tab>('backups')

const tabs = computed(() => [
  { key: 'backups' as Tab, label: t('backups.page.tabBackups'), icon: DatabaseBackup },
  { key: 'schedules' as Tab, label: t('backups.page.tabSchedules'), icon: Calendar },
  ...(authStore.isAdmin ? [{ key: 'destinations' as Tab, label: t('backups.page.tabDestinations'), icon: Cloud }] : []),
])

// Toast
const toast = ref('')
const toastType = ref<'success' | 'error'>('success')
let toastTimer: ReturnType<typeof setTimeout>
function showToast(msg: string, type: 'success' | 'error' = 'success') {
  toast.value = msg; toastType.value = type
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value = '' }, 3000)
}

function destinationName(id: string | null | undefined): string {
  if (!id) return t('backups.page.nodeOnly')
  return backupsStore.destinationSummaries.find(d => d.id === id)?.name ?? t('backups.page.removedDestination')
}

// Create backup modal
const showCreateModal = ref(false)
const createForm = reactive({ server_id: '', type: 'full', world_name: '', destination_id: '' })
const creating = ref(false)

function openCreateModal() {
  createForm.server_id = selectedServer.value
  showCreateModal.value = true
}

async function createBackup() {
  if (!createForm.server_id) return
  creating.value = true
  try {
    await backupsStore.createBackup({
      server_id: createForm.server_id,
      type: createForm.type,
      world_name: createForm.world_name || undefined,
      destination_id: createForm.destination_id || null,
    })
    showCreateModal.value = false
    showToast(t('backups.page.started'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? t('backups.page.createFailed'), 'error')
  } finally {
    creating.value = false
  }
}

// Create schedule modal
const showScheduleModal = ref(false)
const scheduleForm = reactive({ server_id: '', cron_expr: '0 3 * * *', type: 'full', retention_count: 7, retention_days: 30, destination_id: '' })
const creatingSchedule = ref(false)

function openScheduleModal() {
  scheduleForm.server_id = selectedServer.value
  showScheduleModal.value = true
}

async function createSchedule() {
  if (!scheduleForm.server_id) return
  creatingSchedule.value = true
  try {
    await backupsStore.createSchedule({
      server_id: scheduleForm.server_id,
      cron_expr: scheduleForm.cron_expr,
      type: scheduleForm.type,
      retention_count: scheduleForm.retention_count,
      retention_days: scheduleForm.retention_days,
      destination_id: scheduleForm.destination_id || null,
    })
    showScheduleModal.value = false
    showToast(t('backups.page.scheduleCreated'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? t('backups.page.scheduleCreateFailed'), 'error')
  } finally {
    creatingSchedule.value = false
  }
}

async function deleteBackup(b: Backup) {
  const remote = b.upload_status === 'done'
    ? ' ' + t('backups.page.deleteRemoteNote', { name: b.destination_name ?? t('backups.page.theDestination') })
    : ''
  if (!confirm(t('backups.page.deleteConfirm') + remote)) return
  try {
    await backupsStore.deleteBackup(b.id)
    showToast(t('backups.page.deleted'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? t('backups.page.deleteFailed'), 'error')
  }
}

async function restoreBackup(b: Backup) {
  const fetchNote = b.local_present ? '' : ' ' + t('backups.page.restoreFetchNote')
  if (!confirm(t('backups.page.restoreConfirm') + fetchNote)) return
  try {
    await backupsStore.restoreBackup(b.id)
    showToast(t('backups.page.restoreStarted'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? t('backups.page.restoreFailed'), 'error')
  }
}

async function retryUpload(id: string) {
  try {
    await backupsStore.retryUpload(id)
    showToast(t('backups.page.uploadRestarted'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? t('backups.page.uploadRestartFailed'), 'error')
  }
}

async function downloadBackup(id: string) {
  try {
    const config = useRuntimeConfig()
    const res = await fetch(`${config.public.apiBase}/backups/${id}/download`, {
      headers: authStore.accessToken ? { Authorization: `Bearer ${authStore.accessToken}` } : {},
      credentials: 'include',
    })
    if (!res.ok) throw new Error('download failed')
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${id}.zip`
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  } catch { showToast(t('backups.page.downloadFailed'), 'error') }
}

async function toggleSchedule(id: string, enabled: boolean) {
  try {
    await backupsStore.toggleSchedule(id, enabled)
    showToast(enabled ? t('backups.page.scheduleEnabled') : t('backups.page.scheduleDisabled'))
  } catch { showToast(t('backups.page.scheduleToggleFailed'), 'error') }
}

async function deleteSchedule(id: string) {
  if (!confirm(t('backups.page.scheduleDeleteConfirm'))) return
  try {
    await backupsStore.deleteSchedule(id)
    showToast(t('backups.page.scheduleDeleted'))
  } catch { showToast(t('backups.page.scheduleDeleteFailed'), 'error') }
}

function statusColor(status: string) {
  switch (status) {
    case 'completed':
    case 'done': return 'text-tp-success bg-tp-success/15'
    case 'running':
    case 'uploading': return 'text-tp-primary bg-tp-primary/10'
    case 'pending': return 'text-tp-warning bg-tp-warning/15'
    case 'failed': return 'text-tp-error bg-tp-error/10'
    default: return 'text-tp-muted bg-tp-surface2'
  }
}

function uploadLabel(b: Backup): string {
  switch (b.upload_status) {
    case 'pending': return t('backups.page.upload.waiting')
    case 'uploading': return t('backups.page.upload.uploading')
    case 'done': return t('backups.page.upload.uploaded')
    case 'failed': return t('backups.page.upload.failed')
    default: return ''
  }
}

function formatBytes(bytes: number | null): string {
  if (!bytes) return '—'
  if (bytes >= 1073741824) return `${(bytes / 1073741824).toFixed(1)} GB`
  if (bytes >= 1048576) return `${(bytes / 1048576).toFixed(1)} MB`
  return `${(bytes / 1024).toFixed(1)} KB`
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString()
}

// Archive and upload run in the background on the node; poll while anything
// is still moving so the status column updates by itself.
const busy = computed(() => backupsStore.backups.some(b =>
  b.status === 'pending' || b.status === 'running' || b.upload_status === 'pending' || b.upload_status === 'uploading'))
let pollTimer: ReturnType<typeof setInterval> | undefined
watch(busy, (isBusy) => {
  clearInterval(pollTimer)
  if (isBusy) {
    pollTimer = setInterval(() => {
      if (selectedServer.value) backupsStore.fetchBackups(selectedServer.value, { silent: true })
    }, 5000)
  }
})
onBeforeUnmount(() => clearInterval(pollTimer))

watch(selectedServer, (sid) => {
  if (!sid) return
  backupsStore.fetchBackups(sid)
  backupsStore.fetchSchedules(sid)
})

onMounted(async () => {
  backupsStore.fetchDestinationSummaries()
  await serversStore.fetchServers()
  // The backup list is per server; preselect the first one.
  if (!selectedServer.value && serversStore.servers.length) {
    selectedServer.value = serversStore.servers[0]?.id ?? ''
  }
})
</script>

<template>
  <div class="p-6 space-y-5">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 class="text-tp-text font-display font-bold text-2xl">{{ t('pages.backups') }}</h2>
      <div class="flex items-center gap-2">
        <select v-model="selectedServer" class="bg-tp-surface rounded-xl px-3 py-2 text-sm text-tp-text">
          <option value="" disabled>{{ t('backups.page.selectServer') }}</option>
          <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
        </select>
        <UiButton variant="primary" size="md" @click="openCreateModal">
          <Plus class="w-4 h-4" /> {{ t('backups.page.createBackup') }}
        </UiButton>
      </div>
    </div>

    <!-- Tabs -->
    <div class="flex gap-1 bg-tp-surface rounded-xl p-1 w-fit">
      <button v-for="tab in tabs" :key="tab.key"
        :class="['flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all', activeTab === tab.key ? 'bg-tp-primary text-white shadow-sm' : 'text-tp-muted hover:text-tp-text hover:bg-tp-surface2']"
        @click="activeTab = tab.key">
        <component :is="tab.icon" class="w-4 h-4" /> {{ tab.label }}
      </button>
    </div>

    <!-- Backups list -->
    <div v-if="activeTab === 'backups'">
      <div v-if="backupsStore.loading" class="bg-tp-surface rounded-xl p-8 text-center">
        <RefreshCw class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
        <p class="text-tp-muted text-sm">{{ t('backups.page.loading') }}</p>
      </div>
      <div v-else-if="backupsStore.error" class="bg-tp-error/10 text-tp-error rounded-xl p-4 text-sm">{{ backupsStore.error }}</div>
      <div v-else-if="backupsStore.backups.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
        <DatabaseBackup class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-text font-display font-semibold mb-1">{{ t('backups.page.emptyTitle') }}</p>
        <p class="text-tp-muted text-sm">{{ t('backups.page.emptyText') }}</p>
      </div>
      <div v-else class="bg-tp-surface rounded-xl overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-tp-border">
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('backups.page.colStatus') }}</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('backups.page.colType') }}</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('backups.page.colStoredOn') }}</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('backups.page.colSize') }}</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('backups.page.colCreated') }}</th>
              <th class="text-right px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('backups.page.colActions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="b in backupsStore.backups" :key="b.id" class="border-b border-tp-border/50 hover:bg-tp-surface2/50 transition-colors align-top">
              <td class="px-4 py-3">
                <span :class="['text-xs font-medium px-2 py-0.5 rounded-full', statusColor(b.status)]" :title="b.error ?? undefined">{{ te(`backups.page.status.${b.status}`) ? t(`backups.page.status.${b.status}`) : b.status }}</span>
                <span v-if="b.encrypted" class="mt-1 flex items-center gap-1 text-xs text-tp-muted" :title="t('backups.page.encryptedHint')"><Lock class="w-3 h-3" /> {{ t('backups.page.encrypted') }}</span>
              </td>
              <td class="px-4 py-3 text-tp-text capitalize">{{ te(`backups.page.types.${b.type}`) ? t(`backups.page.types.${b.type}`) : b.type }}</td>
              <td class="px-4 py-3 text-xs">
                <div class="flex flex-col gap-1">
                  <span v-if="b.local_present" class="flex items-center gap-1 text-tp-muted"><HardDrive class="w-3.5 h-3.5" /> {{ t('backups.page.storedNode') }}</span>
                  <span v-if="b.upload_status" class="flex items-center gap-1 flex-wrap">
                    <Cloud class="w-3.5 h-3.5 text-tp-muted" />
                    <span class="text-tp-muted">{{ b.destination_name ?? t('backups.page.removedDestination') }}</span>
                    <span :class="['font-medium px-2 py-0.5 rounded-full', statusColor(b.upload_status)]">{{ uploadLabel(b) }}</span>
                  </span>
                  <span v-if="b.upload_status === 'failed' && b.upload_error" class="text-tp-error max-w-xs break-words whitespace-pre-wrap">{{ b.upload_error }}</span>
                </div>
              </td>
              <td class="px-4 py-3 text-tp-text">{{ formatBytes(b.size_bytes) }}</td>
              <td class="px-4 py-3 text-tp-muted text-xs">{{ formatDate(b.created_at) }}</td>
              <td class="px-4 py-3">
                <div class="flex items-center justify-end gap-1">
                  <button v-if="b.upload_status === 'failed' && b.local_present" :title="t('backups.page.retryUpload')" class="p-1.5 rounded-lg text-tp-primary hover:bg-tp-primary/10 transition-colors" @click="retryUpload(b.id)">
                    <UploadCloud class="w-4 h-4" />
                  </button>
                  <button v-if="b.status === 'completed'" :title="t('actions.download')" class="p-1.5 rounded-lg text-tp-accent hover:bg-tp-primary/10 transition-colors" @click="downloadBackup(b.id)">
                    <Download class="w-4 h-4" />
                  </button>
                  <button v-if="b.status === 'completed'" :title="t('backups.page.restore')" class="p-1.5 rounded-lg text-tp-warning hover:bg-tp-warning/10 transition-colors" @click="restoreBackup(b)">
                    <RefreshCw class="w-4 h-4" />
                  </button>
                  <button :title="t('actions.delete')" class="p-1.5 rounded-lg text-tp-danger hover:bg-tp-danger/10 transition-colors" @click="deleteBackup(b)">
                    <Trash2 class="w-4 h-4" />
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Schedules -->
    <div v-if="activeTab === 'schedules'">
      <div class="flex items-center justify-end mb-4">
        <UiButton variant="secondary" size="sm" @click="openScheduleModal" :disabled="!selectedServer">
          <Plus class="w-3.5 h-3.5" /> {{ t('backups.page.newSchedule') }}
        </UiButton>
      </div>
      <div v-if="!selectedServer" class="bg-tp-surface rounded-xl p-12 text-center">
        <Calendar class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-text font-display font-semibold mb-1">{{ t('backups.page.selectServerTitle') }}</p>
        <p class="text-tp-muted text-sm">{{ t('backups.page.selectServerText') }}</p>
      </div>
      <div v-else-if="backupsStore.schedules.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
        <Clock class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-text font-display font-semibold mb-1">{{ t('backups.page.noSchedules') }}</p>
        <p class="text-tp-muted text-sm">{{ t('backups.page.noSchedulesText') }}</p>
      </div>
      <div v-else class="space-y-3">
        <div v-for="sched in backupsStore.schedules" :key="sched.id" class="bg-tp-surface rounded-xl p-4 flex items-center justify-between">
          <div class="flex items-center gap-4">
            <div :class="['w-9 h-9 rounded-xl flex items-center justify-center', sched.enabled ? 'bg-tp-success/10' : 'bg-tp-surface2']">
              <Clock :class="['w-4 h-4', sched.enabled ? 'text-tp-success' : 'text-tp-muted']" />
            </div>
            <div>
              <p class="text-tp-text text-sm font-medium">{{ t('backups.page.scheduleType', { type: te(`backups.page.types.${sched.type}`) ? t(`backups.page.types.${sched.type}`) : sched.type }) }} — <span class="font-mono text-tp-muted">{{ sched.cron_expr }}</span></p>
              <p class="text-tp-muted text-xs">{{ destinationName(sched.destination_id) }} | {{ t('backups.page.retention', { copies: sched.retention_count ?? '∞', days: sched.retention_days ?? '∞' }) }}</p>
            </div>
          </div>
          <div class="flex items-center gap-2">
            <button :title="sched.enabled ? t('actions.disable') : t('actions.enable')" class="p-1.5 rounded-lg transition-colors" :class="sched.enabled ? 'text-tp-warning hover:bg-tp-warning/10' : 'text-tp-success hover:bg-tp-success/10'" @click="toggleSchedule(sched.id, !sched.enabled)">
              <Pause v-if="sched.enabled" class="w-4 h-4" /><Play v-else class="w-4 h-4" />
            </button>
            <button :title="t('actions.delete')" class="p-1.5 rounded-lg text-tp-danger hover:bg-tp-danger/10 transition-colors" @click="deleteSchedule(sched.id)">
              <Trash2 class="w-4 h-4" />
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Destinations (admin) -->
    <BackupsDestinationsPanel v-if="activeTab === 'destinations' && authStore.isAdmin" @toast="showToast" />

    <!-- Create Backup Modal -->
    <UiModal :open="showCreateModal" :title="t('backups.page.createBackup')" @close="showCreateModal = false">
      <div class="space-y-4">
        <div>
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">{{ t('backups.page.server') }}</label>
          <select v-model="createForm.server_id" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
            <option value="">{{ t('backups.page.selectServer') }}</option>
            <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </div>
        <div>
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">{{ t('backups.page.type') }}</label>
          <select v-model="createForm.type" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
            <option value="full">{{ t('backups.page.types.full') }}</option><option value="world">{{ t('backups.page.worldOnly') }}</option><option value="files">{{ t('backups.page.configFiles') }}</option>
          </select>
        </div>
        <div>
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">{{ t('backups.page.alsoCopyTo') }}</label>
          <select v-model="createForm.destination_id" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
            <option value="">{{ t('backups.page.nothingNodeOnly') }}</option>
            <option v-for="d in backupsStore.destinationSummaries" :key="d.id" :value="d.id">{{ d.name }}</option>
          </select>
          <p v-if="!backupsStore.destinationSummaries.length" class="text-tp-muted text-xs mt-1">
            {{ authStore.isAdmin ? t('backups.page.destHintAdmin') : t('backups.page.destHintUser') }}
          </p>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <UiButton variant="secondary" size="md" @click="showCreateModal = false">{{ t('actions.cancel') }}</UiButton>
          <UiButton variant="primary" size="md" :loading="creating" :disabled="!createForm.server_id" @click="createBackup">{{ t('actions.create') }}</UiButton>
        </div>
      </div>
    </UiModal>

    <!-- Create Schedule Modal -->
    <UiModal :open="showScheduleModal" :title="t('backups.page.createSchedule')" @close="showScheduleModal = false">
      <div class="space-y-4">
        <div>
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">{{ t('backups.page.server') }}</label>
          <select v-model="scheduleForm.server_id" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
            <option value="">{{ t('backups.page.selectServer') }}</option>
            <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </div>
        <UiInput v-model="scheduleForm.cron_expr" :label="t('backups.page.cronExpression')" placeholder="0 3 * * *" />
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">{{ t('backups.page.type') }}</label>
            <select v-model="scheduleForm.type" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
              <option value="full">{{ t('backups.page.types.full') }}</option><option value="world">{{ t('backups.page.types.world') }}</option><option value="files">{{ t('backups.page.types.files') }}</option>
            </select>
          </div>
          <div>
            <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">{{ t('backups.page.alsoCopyTo') }}</label>
            <select v-model="scheduleForm.destination_id" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
              <option value="">{{ t('backups.page.nodeOnly') }}</option>
              <option v-for="d in backupsStore.destinationSummaries" :key="d.id" :value="d.id">{{ d.name }}</option>
            </select>
          </div>
        </div>
        <div class="grid grid-cols-2 gap-3">
          <UiInput v-model.number="scheduleForm.retention_count" type="number" :label="t('backups.page.retainCopies')" placeholder="7" />
          <UiInput v-model.number="scheduleForm.retention_days" type="number" :label="t('backups.page.retainDays')" placeholder="30" />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <UiButton variant="secondary" size="md" @click="showScheduleModal = false">{{ t('actions.cancel') }}</UiButton>
          <UiButton variant="primary" size="md" :loading="creatingSchedule" :disabled="!scheduleForm.server_id" @click="createSchedule">{{ t('actions.create') }}</UiButton>
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
