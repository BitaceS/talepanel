<script setup lang="ts">
import { DatabaseBackup, Calendar, Plus, Download, Trash2, RefreshCw, Play, Pause, Clock } from 'lucide-vue-next'
import { useBackupsStore } from '~/stores/backups'
import { useServersStore } from '~/stores/servers'

definePageMeta({ title: 'Backups', middleware: 'auth' })

const backupsStore = useBackupsStore()
const serversStore = useServersStore()

const selectedServer = ref('')
const activeTab = ref<'backups' | 'schedules'>('backups')

// Toast
const toast = ref('')
const toastType = ref<'success' | 'error'>('success')
let toastTimer: ReturnType<typeof setTimeout>
function showToast(msg: string, type: 'success' | 'error' = 'success') {
  toast.value = msg; toastType.value = type
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value = '' }, 3000)
}

// Create backup modal
const showCreateModal = ref(false)
const createForm = reactive({ server_id: '', type: 'full', storage: 'local', world_name: '' })
const creating = ref(false)

async function createBackup() {
  if (!createForm.server_id) return
  creating.value = true
  try {
    await backupsStore.createBackup({
      server_id: createForm.server_id,
      type: createForm.type,
      storage: createForm.storage,
      world_name: createForm.world_name || undefined,
    })
    showCreateModal.value = false
    showToast('Backup created')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to create backup', 'error')
  } finally {
    creating.value = false
  }
}

// Create schedule modal
const showScheduleModal = ref(false)
const scheduleForm = reactive({ server_id: '', cron_expr: '0 3 * * *', type: 'full', storage: 'local', retention_count: 7, retention_days: 30 })
const creatingSchedule = ref(false)

async function createSchedule() {
  if (!scheduleForm.server_id) return
  creatingSchedule.value = true
  try {
    await backupsStore.createSchedule({
      server_id: scheduleForm.server_id,
      cron_expr: scheduleForm.cron_expr,
      type: scheduleForm.type,
      storage: scheduleForm.storage,
      retention_count: scheduleForm.retention_count,
      retention_days: scheduleForm.retention_days,
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

async function deleteBackup(id: string) {
  if (!confirm('Delete this backup? This cannot be undone.')) return
  try {
    await backupsStore.deleteBackup(id)
    showToast('Backup deleted')
  } catch { showToast('Failed to delete', 'error') }
}

async function restoreBackup(id: string) {
  if (!confirm('Restore this backup? The server will be stopped during restore.')) return
  try {
    await backupsStore.restoreBackup(id)
    showToast('Restore initiated')
  } catch { showToast('Failed to restore', 'error') }
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
    case 'complete': return 'text-tp-success bg-tp-success/15'
    case 'running': return 'text-tp-primary bg-tp-primary/10'
    case 'pending': return 'text-tp-warning bg-tp-warning/15'
    case 'failed': return 'text-tp-error bg-tp-error/10'
    default: return 'text-tp-muted bg-tp-surface2'
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

watch(selectedServer, (sid) => {
  backupsStore.fetchBackups(sid || undefined)
  if (sid) backupsStore.fetchSchedules(sid)
})

onMounted(() => {
  serversStore.fetchServers()
  backupsStore.fetchBackups()
})
</script>

<template>
  <div class="p-6 space-y-5">
    <div class="flex items-center justify-between">
      <h2 class="text-tp-text font-display font-bold text-2xl">Backups</h2>
      <div class="flex items-center gap-2">
        <select v-model="selectedServer" class="bg-tp-surface rounded-xl px-3 py-2 text-sm text-tp-text">
          <option value="">All Servers</option>
          <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
        </select>
        <UiButton variant="primary" size="md" @click="showCreateModal = true">
          <Plus class="w-4 h-4" /> Create Backup
        </UiButton>
      </div>
    </div>

    <!-- Tabs -->
    <div class="flex gap-1 bg-tp-surface rounded-xl p-1 w-fit">
      <button v-for="tab in [{ key: 'backups', label: 'Backups', icon: DatabaseBackup }, { key: 'schedules', label: 'Schedules', icon: Calendar }]" :key="tab.key"
        :class="['flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all', activeTab === tab.key ? 'bg-tp-primary text-white shadow-sm' : 'text-tp-muted hover:text-tp-text hover:bg-tp-surface2']"
        @click="activeTab = tab.key as 'backups' | 'schedules'">
        <component :is="tab.icon" class="w-4 h-4" /> {{ tab.label }}
      </button>
    </div>

    <!-- Backups list -->
    <div v-if="activeTab === 'backups'">
      <div v-if="backupsStore.loading" class="bg-tp-surface rounded-xl p-8 text-center">
        <RefreshCw class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
        <p class="text-tp-muted text-sm">Loading backups...</p>
      </div>
      <div v-else-if="backupsStore.backups.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
        <DatabaseBackup class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-text font-display font-semibold mb-1">No backups yet</p>
        <p class="text-tp-muted text-sm">Create your first backup to protect your server data.</p>
      </div>
      <div v-else class="bg-tp-surface rounded-xl overflow-hidden">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-tp-border">
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Status</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Type</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Storage</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Size</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Created</th>
              <th class="text-right px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="b in backupsStore.backups" :key="b.id" class="border-b border-tp-border/50 hover:bg-tp-surface2/50 transition-colors">
              <td class="px-4 py-3">
                <span :class="['text-xs font-medium px-2 py-0.5 rounded-full', statusColor(b.status)]">{{ b.status }}</span>
              </td>
              <td class="px-4 py-3 text-tp-text capitalize">{{ b.type }}</td>
              <td class="px-4 py-3 text-tp-muted text-xs uppercase">{{ b.storage }}</td>
              <td class="px-4 py-3 text-tp-text">{{ formatBytes(b.size_bytes) }}</td>
              <td class="px-4 py-3 text-tp-muted text-xs">{{ formatDate(b.created_at) }}</td>
              <td class="px-4 py-3">
                <div class="flex items-center justify-end gap-1">
                  <button v-if="b.status === 'complete'" title="Restore" class="p-1.5 rounded-lg text-tp-accent hover:bg-tp-primary/10 transition-colors" @click="restoreBackup(b.id)">
                    <Download class="w-4 h-4" />
                  </button>
                  <button title="Delete" class="p-1.5 rounded-lg text-tp-danger hover:bg-tp-danger/10 transition-colors" @click="deleteBackup(b.id)">
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
        <UiButton variant="secondary" size="sm" @click="showScheduleModal = true" :disabled="!selectedServer">
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
              <p class="text-tp-muted text-xs">{{ sched.storage }} | Retain {{ sched.retention_count ?? '∞' }} copies, {{ sched.retention_days ?? '∞' }} days</p>
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

    <!-- Create Backup Modal -->
    <UiModal :show="showCreateModal" title="Create Backup" @close="showCreateModal = false">
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
          <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Storage</label>
          <select v-model="createForm.storage" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
            <option value="local">Local</option><option value="s3">S3 (MinIO)</option>
          </select>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <UiButton variant="secondary" size="md" @click="showCreateModal = false">Cancel</UiButton>
          <UiButton variant="primary" size="md" :loading="creating" :disabled="!createForm.server_id" @click="createBackup">Create</UiButton>
        </div>
      </div>
    </UiModal>

    <!-- Create Schedule Modal -->
    <UiModal :show="showScheduleModal" title="Create Schedule" @close="showScheduleModal = false">
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
            <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Storage</label>
            <select v-model="scheduleForm.storage" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
              <option value="local">Local</option><option value="s3">S3</option>
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
