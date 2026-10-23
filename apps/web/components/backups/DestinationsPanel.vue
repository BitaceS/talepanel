<script setup lang="ts">
import { Cloud, Plus, Pencil, Trash2, PlugZap, Copy, Check, RefreshCw, ExternalLink } from '@lucide/vue'
import { useBackupsStore } from '~/stores/backups'
import type { BackupDestination, BackupDestinationField, BackupDestinationProvider } from '~/types'

// Off-site backup destinations (admin only). The node runs rclone against
// these; secrets are write-only and never come back from the API.

const emit = defineEmits<{ toast: [msg: string, type: 'success' | 'error'] }>()

const store = useBackupsStore()
const loading = ref(false)
const loadError = ref('')

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    await store.fetchDestinations()
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    loadError.value = e.data?.error ?? 'Failed to load destinations'
  } finally {
    loading.value = false
  }
}
onMounted(load)

function providerOf(id: string): BackupDestinationProvider | undefined {
  return store.providers.find(p => p.id === id)
}

// ── Test connection ─────────────────────────────────────────────────────────
const testing = ref<string | null>(null)
const testResult = ref<Record<string, { ok: boolean; error?: string }>>({})

async function runTest(id: string) {
  testing.value = id
  try {
    testResult.value[id] = await store.testDestination(id)
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    testResult.value[id] = { ok: false, error: e.data?.error ?? 'Test request failed' }
  } finally {
    testing.value = null
  }
}

// ── Create / edit ───────────────────────────────────────────────────────────
const showModal = ref(false)
const editing = ref<BackupDestination | null>(null)
const saving = ref(false)
const formError = ref('')
const showAdvanced = ref(false)
const copied = ref(false)

const form = reactive({
  name: '',
  provider: 'onedrive',
  base_path: 'TalePanel/backups',
  options: {} as Record<string, string>,
  secrets: {} as Record<string, string>,
  keep_local: true,
  retention_count: null as number | null,
})

const currentProvider = computed(() => providerOf(form.provider))
const visibleFields = computed(() =>
  (currentProvider.value?.fields ?? []).filter(f => showAdvanced.value || !f.advanced),
)
const hasAdvanced = computed(() => (currentProvider.value?.fields ?? []).some(f => f.advanced))

function openCreate() {
  editing.value = null
  form.name = ''
  form.provider = store.providers[0]?.id ?? 'onedrive'
  form.base_path = 'TalePanel/backups'
  form.options = {}
  form.secrets = {}
  form.keep_local = true
  form.retention_count = 14
  formError.value = ''
  showAdvanced.value = false
  showModal.value = true
}

function openEdit(d: BackupDestination) {
  editing.value = d
  form.name = d.name
  form.provider = d.provider
  form.base_path = d.base_path
  form.options = { ...d.options }
  form.secrets = {}
  form.keep_local = d.keep_local
  form.retention_count = d.retention_count
  formError.value = ''
  showAdvanced.value = Object.keys(d.options).some(k => currentProvider.value?.fields.find(f => f.key === k)?.advanced)
  showModal.value = true
}

// Only a user-initiated provider switch clears the fields (openEdit sets the
// provider programmatically and must keep the loaded options).
function onProviderChange() {
  form.options = {}
  form.secrets = {}
  showAdvanced.value = false
}

function secretStored(f: BackupDestinationField): boolean {
  return !!editing.value && editing.value.provider === form.provider && !!editing.value.secrets_set[f.key]
}

async function copyCommand() {
  const cmd = currentProvider.value?.authorize_command
  if (!cmd) return
  try {
    await navigator.clipboard.writeText(cmd)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch { /* clipboard unavailable */ }
}

async function save() {
  formError.value = ''
  const p = currentProvider.value
  if (!p) return
  const options: Record<string, string> = {}
  const secrets: Record<string, string> = {}
  for (const f of p.fields) {
    if (f.secret) {
      const v = form.secrets[f.key]
      if (v) secrets[f.key] = v
    } else {
      const v = form.options[f.key]
      if (v) options[f.key] = v
    }
  }
  saving.value = true
  try {
    const d = await store.saveDestination(editing.value?.id ?? null, {
      name: form.name,
      provider: form.provider,
      base_path: form.base_path,
      options,
      secrets,
      keep_local: form.keep_local,
      retention_count: form.retention_count && form.retention_count > 0 ? form.retention_count : null,
    })
    showModal.value = false
    emit('toast', 'Destination saved — testing the connection…', 'success')
    await runTest(d.id)
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    formError.value = e.data?.error ?? 'Could not save the destination'
  } finally {
    saving.value = false
  }
}

async function remove(d: BackupDestination) {
  if (!confirm(`Delete destination "${d.name}"? Backups already uploaded stay in your cloud storage, but the panel stops managing them.`)) return
  try {
    await store.deleteDestination(d.id)
    emit('toast', 'Destination deleted', 'success')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    emit('toast', e.data?.error ?? 'Failed to delete destination', 'error')
  }
}

const inputClass = 'w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text'
const labelClass = 'block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1'
</script>

<template>
  <div class="space-y-4">
    <div class="bg-tp-surface rounded-xl p-5 flex items-start justify-between gap-4">
      <div>
        <h3 class="text-tp-text font-display font-semibold text-lg flex items-center gap-2">
          <Cloud class="w-4 h-4 text-tp-primary" /> Off-site destinations
        </h3>
        <p class="text-tp-muted text-sm mt-1 max-w-2xl">
          Copy backups to OneDrive, Google Drive, Dropbox, S3, Backblaze B2, SFTP or WebDAV/Nextcloud, so a broken
          node does not take your backups with it. Each node uploads its own backups with rclone.
        </p>
      </div>
      <UiButton variant="primary" size="sm" :disabled="!store.providers.length" @click="openCreate">
        <Plus class="w-3.5 h-3.5" /> Add destination
      </UiButton>
    </div>

    <div v-if="loading" class="bg-tp-surface rounded-xl p-8 text-center">
      <RefreshCw class="w-6 h-6 text-tp-muted animate-spin mx-auto" />
    </div>
    <div v-else-if="loadError" class="bg-tp-error/10 text-tp-error rounded-xl p-4 text-sm">{{ loadError }}</div>
    <div v-else-if="store.destinations.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
      <Cloud class="w-10 h-10 text-tp-muted mx-auto mb-3" />
      <p class="text-tp-text font-display font-semibold mb-1">No destinations yet</p>
      <p class="text-tp-muted text-sm">Backups currently stay on the node only.</p>
    </div>
    <div v-else class="space-y-3">
      <div v-for="d in store.destinations" :key="d.id" class="bg-tp-surface rounded-xl p-4 space-y-2">
        <div class="flex items-center justify-between gap-3">
          <div class="min-w-0">
            <p class="text-tp-text text-sm font-medium truncate">{{ d.name }}</p>
            <p class="text-tp-muted text-xs truncate">
              {{ providerOf(d.provider)?.label ?? d.provider }} · <span class="font-mono">{{ d.base_path }}</span>
              · {{ d.keep_local ? 'keeps a local copy' : 'no local copy' }}
              · {{ d.retention_count ? `keeps newest ${d.retention_count} per server` : 'keeps all' }}
            </p>
          </div>
          <div class="flex items-center gap-1 shrink-0">
            <UiButton variant="secondary" size="sm" :loading="testing === d.id" @click="runTest(d.id)">
              <PlugZap class="w-3.5 h-3.5" /> Test connection
            </UiButton>
            <button title="Edit" class="p-1.5 rounded-lg text-tp-muted hover:text-tp-text hover:bg-tp-surface2" @click="openEdit(d)">
              <Pencil class="w-4 h-4" />
            </button>
            <button title="Delete" class="p-1.5 rounded-lg text-tp-danger hover:bg-tp-danger/10" @click="remove(d)">
              <Trash2 class="w-4 h-4" />
            </button>
          </div>
        </div>
        <div v-if="testResult[d.id]" :class="['text-xs rounded-lg px-3 py-2 whitespace-pre-wrap break-words', testResult[d.id]?.ok ? 'bg-tp-success/15 text-tp-success' : 'bg-tp-error/10 text-tp-error']">
          {{ testResult[d.id]?.ok ? 'Connection works: a test file was written and deleted.' : testResult[d.id]?.error }}
        </div>
      </div>
    </div>

    <UiModal :open="showModal" :title="editing ? 'Edit destination' : 'Add destination'" size="lg" @close="showModal = false">
      <div class="space-y-4 max-h-[70vh] overflow-y-auto pr-1">
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <UiInput v-model="form.name" label="Name" placeholder="My OneDrive" required />
          <div>
            <label :class="labelClass">Provider</label>
            <select v-model="form.provider" :class="inputClass" @change="onProviderChange">
              <option v-for="p in store.providers" :key="p.id" :value="p.id">{{ p.label }}</option>
            </select>
          </div>
        </div>

        <div v-if="currentProvider?.authorize_command" class="bg-tp-surface2 rounded-xl p-4 space-y-2 text-sm">
          <p class="text-tp-text font-medium">Connect your account</p>
          <ol class="list-decimal list-inside text-tp-muted space-y-1">
            <li>
              On your own computer (the one with a web browser), install rclone from
              <a href="https://rclone.org/downloads/" target="_blank" rel="noopener noreferrer" class="text-tp-primary hover:underline inline-flex items-center gap-1">rclone.org/downloads <ExternalLink class="w-3 h-3" /></a>.
            </li>
            <li>Open a terminal (Windows: PowerShell) in the rclone folder and run:</li>
          </ol>
          <div class="flex items-center gap-2">
            <code class="flex-1 font-mono text-tp-text bg-tp-surface rounded-lg px-3 py-2 select-all">{{ currentProvider.authorize_command }}</code>
            <button class="p-2 rounded-lg text-tp-muted hover:text-tp-text hover:bg-tp-surface" title="Copy" @click="copyCommand">
              <Check v-if="copied" class="w-4 h-4 text-tp-success" /><Copy v-else class="w-4 h-4" />
            </button>
          </div>
          <p class="text-tp-muted">
            Sign in in the browser window that opens, then paste everything rclone prints between
            <code class="font-mono">---&gt;</code> and <code class="font-mono">&lt;---End paste</code> into the token field below.
          </p>
        </div>

        <div v-for="f in visibleFields" :key="`${form.provider}-${f.key}`">
          <label :class="labelClass">
            {{ f.label }}<span v-if="f.required" class="text-tp-danger ml-0.5">*</span>
            <span v-if="f.secret && secretStored(f)" class="ml-2 normal-case tracking-normal text-tp-success">stored</span>
          </label>
          <template v-if="f.secret">
            <textarea v-if="f.multiline" v-model="form.secrets[f.key]" rows="3" :class="[inputClass, 'font-mono text-xs']"
              :placeholder="secretStored(f) ? 'Stored — leave empty to keep it' : f.placeholder" autocomplete="off" spellcheck="false" />
            <input v-else v-model="form.secrets[f.key]" type="password" :class="inputClass"
              :placeholder="secretStored(f) ? 'Stored — leave empty to keep it' : f.placeholder" autocomplete="new-password" />
          </template>
          <select v-else-if="f.choices?.length" v-model="form.options[f.key]" :class="inputClass">
            <option value="">{{ f.default ? `Default (${f.default})` : '—' }}</option>
            <option v-for="c in f.choices" :key="c" :value="c">{{ c }}</option>
          </select>
          <input v-else v-model="form.options[f.key]" :class="inputClass" :placeholder="f.placeholder || f.default" />
          <p v-if="f.help" class="text-tp-muted text-xs mt-1">{{ f.help }}</p>
        </div>
        <button v-if="hasAdvanced" class="text-xs text-tp-primary hover:underline" @click="showAdvanced = !showAdvanced">
          {{ showAdvanced ? 'Hide advanced options' : 'Show advanced options' }}
        </button>

        <div>
          <UiInput v-model="form.base_path" label="Folder" placeholder="TalePanel/backups" required />
          <p class="text-tp-muted text-xs mt-1">
            {{ currentProvider?.base_path_help }} Backups are stored as
            <span class="font-mono">{{ form.base_path || '…' }}/&lt;server id&gt;/&lt;backup id&gt;.zip</span>.
          </p>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <label class="flex items-start gap-2 text-sm text-tp-text bg-tp-surface2 rounded-xl p-3 cursor-pointer">
            <input v-model="form.keep_local" type="checkbox" class="mt-0.5">
            <span>
              Keep a local copy on the node
              <span class="block text-tp-muted text-xs">Off: the node deletes its copy once the upload succeeded (saves disk space; restores download it again).</span>
            </span>
          </label>
          <div>
            <label :class="labelClass">Keep newest per server</label>
            <input v-model.number="form.retention_count" type="number" min="1" :class="inputClass" placeholder="all">
            <p class="text-tp-muted text-xs mt-1">Older uploads of the same server are deleted from the destination. Empty keeps all.</p>
          </div>
        </div>

        <p v-if="formError" class="text-sm text-tp-error bg-tp-error/10 rounded-lg px-3 py-2">{{ formError }}</p>

        <div class="flex justify-end gap-2 pt-2">
          <UiButton variant="secondary" size="md" @click="showModal = false">Cancel</UiButton>
          <UiButton variant="primary" size="md" :loading="saving" :disabled="!form.name || !form.base_path" @click="save">
            Save and test
          </UiButton>
        </div>
      </div>
    </UiModal>
  </div>
</template>
