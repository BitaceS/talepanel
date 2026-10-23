<script setup lang="ts">
import { DatabaseBackup, Calendar, Plus, Download, Trash2, RefreshCw, Play, Pause, Clock, Cloud, UploadCloud, HardDrive } from '@lucide/vue'
import { useBackupsStore } from '~/stores/backups'
import { useServersStore } from '~/stores/servers'
import { useAuthStore } from '~/stores/auth'
import type { Backup } from '~/types'

definePageMeta({ title: 'Backups', middleware: 'auth' })

const backupsStore = useBackupsStore()
const serversStore = useServersStore()
const authStore = useAuthStore()

type Tab = 'backups' | 'schedules' | 'destinations'
const selectedServer = ref('')
const activeTab = ref<Tab>('backups')

const tabs = computed(() => [
  { key: 'backups' as Tab, label: 'Backups', icon: DatabaseBackup },
  { key: 'schedules' as Tab, label: 'Schedules', icon: Calendar },
  ...(authStore.isAdmin ? [{ key: 'destinations' as Tab, label: 'Destinations', icon: Cloud }] : []),
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
  if (!id) return 'This node only'
  return backupsStore.destinationSummaries.find(d => d.id === id)?.name ?? 'Removed destination'
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
    showToast('Backup started')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to create backup', 'error')
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
    showToast('Schedule created')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to create schedule', 'error')
  } finally {
    creatingSchedule.value = false
  }
}

async function deleteBackup(b: Backup) {
  const remote = b.upload_status === 'done' ? ` The copy on "${b.destination_name ?? 'the destination'}" is deleted too.` : ''
  if (!confirm(`Delete this backup? This cannot be undone.${remote}`)) return
  try {
    await backupsStore.deleteBackup(b.id)
    showToast('Backup deleted')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? 'Failed to delete', 'error')
  }
}

async function restoreBackup(b: Backup) {
  const fetchNote = b.local_present ? '' : ' The node first downloads it from the off-site destination, which can take a while.'
  if (!confirm(`Restore this backup? Current server files will be overwritten.${fetchNote}`)) return
  try {
    await backupsStore.restoreBackup(b.id)
    showToast('Restore initiated')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? 'Failed to restore', 'error')
  }
}

async function retryUpload(id: string) {
  try {
    await backupsStore.retryUpload(id)
    showToast('Upload restarted')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? 'Failed to restart upload', 'error')
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
  } catch { showToast('Failed to download', 'error') }
}

async function toggleSchedule(id: string, enabled: boolean) {
  try {
    await backupsStore.toggleSchedule(id, enabled)
    showToast(enabled ? 'Schedule enabled' : 'Schedule disabled')
  } catch { showToast('Failed to toggle schedule', 'error') }
}

async function deleteSchedule(id: string) {
  if (!confirm('Delete this schedule?')) return
  try {
    await backupsStore.deleteSchedule(id)
    showToast('Schedule deleted')
  } catch { showToast('Failed to delete schedule', 'error') }
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
    case 'pending': return 'waiting'
    case 'uploading': return 'uploading'
    case 'done': return 'uploaded'
    case 'failed': return 'upload failed'
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
      <h2 class="text-tp-text font-display font-bold text-2xl">Backups</h2>
      <div class="flex items-center gap-2">
        <select v-model="selectedServer" class="bg-tp-surface rounded-xl px-3 py-2 text-sm text-tp-text">
          <option value="" disabled>Select server...</option>
          <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
        </select>
        <UiButton variant="primary" size="md" @click="openCreateModal">
          <Plus class="w-4 h-4" /> Create Backup
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
        <p class="text-tp-muted text-sm">Loading backups...</p>
      </div>
      <div v-else-if="backupsStore.error" class="bg-tp-error/10 text-tp-error rounded-xl p-4 text-sm">{{ backupsStore.error }}</div>
      <div v-else-if="backupsStore.backups.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
        <DatabaseBackup class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-text font-display font-semibold mb-1">No backups yet</p>
        <p class="text-tp-muted text-sm">Create your first backup to protect your server data.</p>
      </div>
      <div v-else class="bg-tp-surface rounded-xl overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-tp-border">
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Status</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Type</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Stored on</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Size</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Created</th>
              <th class="text-right px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="b in backupsStore.backups" :key="b.id" class="border-b border-tp-border/50 hover:bg-tp-surface2/50 transition-colors align-top">
              <td class="px-4 py-3">
                <span :class="['text-xs font-medium px-2 py-0.5 rounded-full', statusColor(b.status)]" :title="b.error ?? undefined">{{ b.status }}</span>
              </td>
              <td class="px-4 py-3 text-tp-text capitalize">{{ b.type }}</td>
              <td class="px-4 py-3 text-xs">
                <div class="flex flex-col gap-1">
                  <span v-if="b.local_present" class="flex items-center gap-1 text-tp-muted"><HardDrive class="w-3.5 h-3.5" /> Node</span>
                  <span v-if="b.upload_status" class="flex items-center gap-1 flex-wrap">
                    <Cloud class="w-3.5 h-3.5 text-tp-muted" />
                    <span class="text-tp-muted">{{ b.destination_name ?? 'Removed destination' }}</span>
                    <span :class="['font-medium px-2 py-0.5 rounded-full', statusColor(b.upload_status)]">{{ uploadLabel(b) }}</span>
                  </span>
                  <span v-if="b.upload_status === 'failed' && b.upload_error" class="text-tp-error max-w-xs break-words whitespace-pre-wrap">{{ b.upload_error }}</span>
                </div>
              </td>
              <td class="px-4 py-3 text-tp-text">{{ formatBytes(b.size_bytes) }}</td>
              <td class="px-4 py-3 text-tp-muted text-xs">{{ formatDate(b.created_at) }}</td>
              <td class="px-4 py-3">
                <div class="flex items-center justify-end gap-1">
                  <button v-if="b.upload_status === 'failed' && b.local_present" title="Retry upload" class="p-1.5 rounded-lg text-tp-primary hover:bg-tp-primary/10 transition-colors" @click="retryUpload(b.id)">
                    <UploadCloud class="w-4 h-4" />
                  </button>
                  <button v-if="b.status === 'completed'" title="Download" class="p-1.5 rounded-lg text-tp-accent hover:bg-tp-primary/10 transition-colors" @click="downloadBackup(b.id)">
                    <Download class="w-4 h-4" />
                  </button>
                  <button v-if="b.status === 'completed'" title="Restore" class="p-1.5 rounded-lg text-tp-warning hover:bg-tp-warning/10 transition-colors" @click="restoreBackup(b)">
                    <RefreshCw class="w-4 h-4" />
                  </button>
                  <button title="Delete" class="p-1.5 rounded-lg text-tp-danger hover:bg-tp-danger/10 transition-colors" @click="deleteBackup(b)">
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
          <Plus class="w-3.5 h-3.5" /> New Schedule
        </UiButton>
      </div>
      <div v-if="!selectedServer" class="bg-tp-surface rounded-xl p-12 text-center">
        <Calendar class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-text font-display font-semibold mb-1">Select a server</p>
        <p class="text-tp-muted text-sm">Choose a server above to view its backup schedules.</p>
      </div>
      <div v-else-if="backupsStore.schedules.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
        <Clock class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-text font-display font-semibold mb-1">No schedules</p>
        <p class="text-tp-muted text-sm">Create a schedule to automate backups.</p>
      </div>
      <div v-else class="space-y-3">
        <div v-for="sched in backupsStore.schedules" :key="sched.id" class="bg-tp-surface rounded-xl p-4 flex items-center justify-between">
          <div class="flex items-center gap-4">
            <div :class="['w-9 h-9 rounded-xl flex items-center justify-center', sched.enabled ? 'bg-tp-success/10' : 'bg-tp-surface2']">
              <Clock :class="['w-4 h-4', sched.enabled ? 'text-tp-success' : 'text-tp-muted']" />
            </div>
            <div>
              <p class="text-tp-text text-sm font-medium">{{ sched.type }} backup — <span class="font-mono text-tp-muted">{{ sched.cron_expr }}</span></p>
              <p class="text-tp-muted text-xs">{{ destinationName(sched.destination_id) }} | Retain {{ sched.retention_count ?? '∞' }} copies, {{ sched.retention_days ?? '∞' }} days</p>
            </div>
          </div>
          <div class="flex items-center gap-2">
            <button :title="sched.enabled ? 'Disable' : 'Enable'" class="p-1.5 rounded-lg transition-colors" :class="sched.enabled ? 'text-tp-warning hover:bg-tp-warning/10' : 'text-tp-success hover:bg-tp-success/10'" @click="toggleSchedule(sched.id, !sched.enabled)">
              <Pause v-if="sched.enabled" class="w-4 h-4" /><Play v-else class="w-4 h-4" />
            </button>
            <button title="Delete" class="p-1.5 rounded-lg text-tp-danger hover:bg-tp-danger/10 transition-colors" @click="deleteSchedule(sched.id)">
              <Trash2 class="w-4 h-4" />
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Destinations (admin) -->
    <BackupsDestinationsPanel v-if="activeTab === 'destinations' && authStore.isAdmin" @toast="showToast" />

    <!-- Create Backup Modal -->
    <UiModal :open="showCreateModal" title="Create Backup" @close="showCreateModal = false">
      <div class="space-y-4">
        <div>
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Server</label>
          <select v-model="createForm.server_id" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
            <option value="">Select server...</option>
            <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </div>
        <div>
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Type</label>
          <select v-model="createForm.type" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
            <option value="full">Full</option><option value="world">World Only</option><option value="files">Config Files</option>
          </select>
        </div>
        <div>
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Also copy to</label>
          <select v-model="createForm.destination_id" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
            <option value="">Nothing — keep it on this node only</option>
            <option v-for="d in backupsStore.destinationSummaries" :key="d.id" :value="d.id">{{ d.name }}</option>
          </select>
          <p v-if="!backupsStore.destinationSummaries.length" class="text-tp-muted text-xs mt-1">
            {{ authStore.isAdmin ? 'Add an off-site destination (OneDrive, Google Drive, S3, …) on the Destinations tab.' : 'An admin can add off-site destinations such as OneDrive or S3.' }}
          </p>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <UiButton variant="secondary" size="md" @click="showCreateModal = false">Cancel</UiButton>
          <UiButton variant="primary" size="md" :loading="creating" :disabled="!createForm.server_id" @click="createBackup">Create</UiButton>
        </div>
      </div>
    </UiModal>

    <!-- Create Schedule Modal -->
    <UiModal :open="showScheduleModal" title="Create Schedule" @close="showScheduleModal = false">
      <div class="space-y-4">
        <div>
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Server</label>
          <select v-model="scheduleForm.server_id" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
            <option value="">Select server...</option>
            <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </div>
        <UiInput v-model="scheduleForm.cron_expr" label="Cron Expression" placeholder="0 3 * * *" />
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Type</label>
            <select v-model="scheduleForm.type" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
              <option value="full">Full</option><option value="world">World</option><option value="files">Files</option>
            </select>
          </div>
          <div>
            <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Also copy to</label>
            <select v-model="scheduleForm.destination_id" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
              <option value="">This node only</option>
              <option v-for="d in backupsStore.destinationSummaries" :key="d.id" :value="d.id">{{ d.name }}</option>
            </select>
          </div>
        </div>
        <div class="grid grid-cols-2 gap-3">
          <UiInput v-model.number="scheduleForm.retention_count" type="number" label="Retain copies" placeholder="7" />
          <UiInput v-model.number="scheduleForm.retention_days" type="number" label="Retain days" placeholder="30" />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <UiButton variant="secondary" size="md" @click="showScheduleModal = false">Cancel</UiButton>
          <UiButton variant="primary" size="md" :loading="creatingSchedule" :disabled="!scheduleForm.server_id" @click="createSchedule">Create</UiButton>
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
