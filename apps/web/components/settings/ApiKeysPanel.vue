<script setup lang="ts">
import { KeyRound, Plus, Trash2, Copy, Check, Loader2 } from '@lucide/vue'
import { useApi } from '~/composables/useApi'

// The signed-in user's API keys. A key acts as its user, limited to the
// scopes and servers chosen here; reading servers is always allowed. Keys
// never reach the account, admin or node settings. The key itself is shown
// once, right after it is created.

interface ApiKey {
  id: string
  name: string
  prefix: string
  scopes: string[]
  server_ids: string[] | null
  expires_at: string | null
  last_used_at: string | null
  last_used_ip: string | null
  created_at: string
}
interface ServerRef { id: string; name: string }

const emit = defineEmits<{ toast: [msg: string, type: 'success' | 'error'] }>()
const api = useApi()

const SCOPES: { id: string; label: string; detail: string }[] = [
  { id: 'power', label: 'Power', detail: 'Start, stop, restart, kill' },
  { id: 'console', label: 'Console', detail: 'Send console commands, run saved game commands' },
  { id: 'manage', label: 'Manage', detail: 'Settings, reinstall, Hytale updates, scheduled tasks, active world' },
  { id: 'backups', label: 'Backups', detail: 'Create, restore, download and delete backups; backup schedules' },
  { id: 'players', label: 'Players', detail: 'Kick, ban, unban, mute, whitelist' },
]
const EXPIRY = [
  { days: 30, label: '30 days' },
  { days: 90, label: '90 days' },
  { days: 365, label: '1 year' },
  { days: 0, label: 'Never' },
]

const keys = ref<ApiKey[]>([])
const servers = ref<ServerRef[]>([])
const loading = ref(true)
const loadError = ref('')

function apiError(err: unknown, fallback: string): string {
  const e = err as { data?: { error?: string }; message?: string }
  return e.data?.error ?? e.message ?? fallback
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [k, s] = await Promise.all([
      api.get<{ api_keys: ApiKey[] }>('/api-keys'),
      api.get<{ servers: ServerRef[] }>('/servers'),
    ])
    keys.value = k.api_keys ?? []
    servers.value = s.servers ?? []
  } catch (err: unknown) {
    loadError.value = apiError(err, 'Failed to load API keys')
  } finally {
    loading.value = false
  }
}
onMounted(load)

// ── Create ─────────────────────────────────────────────────────────────────
const showForm = ref(false)
const saving = ref(false)
const formError = ref('')
const form = reactive({
  name: '',
  scopes: [] as string[],
  allServers: true,
  serverIds: [] as string[],
  expiresInDays: 90,
})
// The new key, shown once.
const created = ref<{ key: string; name: string } | null>(null)
const copied = ref(false)

function openForm() {
  Object.assign(form, { name: '', scopes: [], allServers: true, serverIds: [], expiresInDays: 90 })
  formError.value = ''
  created.value = null
  showForm.value = true
}

async function createKey() {
  formError.value = ''
  if (!form.name.trim()) {
    formError.value = 'Give the key a name, e.g. "Discord bot"'
    return
  }
  if (!form.allServers && form.serverIds.length === 0) {
    formError.value = 'Pick at least one server'
    return
  }
  saving.value = true
  try {
    const data = await api.post<{ api_key: ApiKey; key: string }>('/api-keys', {
      name: form.name.trim(),
      scopes: form.scopes,
      server_ids: form.allServers ? null : form.serverIds,
      expires_in_days: form.expiresInDays,
    })
    keys.value = [data.api_key, ...keys.value]
    created.value = { key: data.key, name: data.api_key.name }
    copied.value = false
    showForm.value = false
  } catch (err: unknown) {
    formError.value = apiError(err, 'Failed to create the API key')
  } finally {
    saving.value = false
  }
}

async function copyKey() {
  if (!created.value) return
  try {
    await navigator.clipboard.writeText(created.value.key)
    copied.value = true
  } catch {
    emit('toast', 'Copy failed — select the key and copy it by hand', 'error')
  }
}

// ── Revoke ─────────────────────────────────────────────────────────────────
const busy = ref<string | null>(null)

async function revoke(k: ApiKey) {
  if (!window.confirm(`Revoke the API key "${k.name}"? Scripts using it stop working at once.`)) return
  busy.value = k.id
  try {
    await api.delete(`/api-keys/${k.id}`)
    keys.value = keys.value.filter(x => x.id !== k.id)
    emit('toast', 'API key revoked', 'success')
  } catch (err: unknown) {
    emit('toast', apiError(err, 'Failed to revoke the API key'), 'error')
  } finally {
    busy.value = null
  }
}

// ── Display ────────────────────────────────────────────────────────────────
function scopeText(k: ApiKey): string {
  const names = k.scopes.map(s => SCOPES.find(x => x.id === s)?.label ?? s)
  return names.length ? `Read, ${names.join(', ')}` : 'Read only'
}

function serverText(k: ApiKey): string {
  if (!k.server_ids) return 'All your servers'
  const names = k.server_ids.map(id => servers.value.find(s => s.id === id)?.name ?? 'deleted server')
  return names.join(', ')
}

function isExpired(k: ApiKey): boolean {
  return !!k.expires_at && new Date(k.expires_at) <= new Date()
}

function fmt(iso: string | null): string {
  return iso ? new Date(iso).toLocaleString() : '—'
}

const inputClass = 'w-full bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50'
const labelClass = 'block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1'
</script>

<template>
  <div class="space-y-5">
    <div class="bg-tp-surface rounded-xl overflow-hidden">
      <div class="px-5 py-4 border-b border-tp-border flex items-start justify-between gap-3">
        <div>
          <h3 class="text-tp-text font-display font-semibold text-sm">API keys</h3>
          <p class="text-tp-muted text-xs mt-0.5">
            For scripts, bots and monitoring. A key acts as you, but only with the rights and servers you pick,
            and never reaches your account, admin or node settings. Send it as <span class="font-mono">Authorization: Bearer tp_…</span>
          </p>
        </div>
        <UiButton v-if="!showForm" variant="primary" size="sm" class="shrink-0" @click="openForm">
          <Plus class="w-3.5 h-3.5" /> New key
        </UiButton>
      </div>

      <!-- The new key, once -->
      <div v-if="created" class="p-5 bg-tp-primary/10 space-y-2" role="status">
        <p class="text-tp-text text-sm font-semibold">Key "{{ created.name }}" created</p>
        <p class="text-tp-muted text-xs">Copy it now. It is stored only as a hash and cannot be shown again.</p>
        <div class="flex items-center gap-2">
          <code class="flex-1 min-w-0 break-all bg-tp-surface2 rounded-lg px-3 py-2 text-xs font-mono text-tp-text select-all">{{ created.key }}</code>
          <UiButton variant="secondary" size="sm" class="shrink-0" @click="copyKey">
            <Check v-if="copied" class="w-3.5 h-3.5" /><Copy v-else class="w-3.5 h-3.5" />
            {{ copied ? 'Copied' : 'Copy' }}
          </UiButton>
        </div>
        <UiButton variant="secondary" size="sm" @click="created = null">Done</UiButton>
      </div>

      <!-- Create form -->
      <div v-if="showForm" class="p-5 space-y-4 border-b border-tp-border">
        <div>
          <label :class="labelClass">Name</label>
          <input v-model="form.name" :class="inputClass" maxlength="64" placeholder="e.g. Discord bot" />
        </div>

        <div>
          <span :class="labelClass">Rights (reading your servers is always allowed)</span>
          <div class="space-y-1.5">
            <label v-for="s in SCOPES" :key="s.id" class="flex items-start gap-2.5 cursor-pointer">
              <input v-model="form.scopes" type="checkbox" :value="s.id" class="mt-0.5 accent-tp-primary" />
              <span class="text-sm">
                <span class="text-tp-text font-medium">{{ s.label }}</span>
                <span class="text-tp-muted text-xs"> — {{ s.detail }}</span>
              </span>
            </label>
          </div>
          <p class="text-tp-muted text-xs mt-1.5">A key never gets more than your own permissions on a server.</p>
        </div>

        <div>
          <span :class="labelClass">Servers</span>
          <label class="flex items-center gap-2.5 cursor-pointer text-sm text-tp-text">
            <input v-model="form.allServers" type="radio" :value="true" class="accent-tp-primary" />
            All my servers, including ones I get later
          </label>
          <label class="flex items-center gap-2.5 cursor-pointer text-sm text-tp-text mt-1">
            <input v-model="form.allServers" type="radio" :value="false" class="accent-tp-primary" />
            Only these servers
          </label>
          <div v-if="!form.allServers" class="mt-2 ml-6 space-y-1 max-h-48 overflow-y-auto">
            <p v-if="servers.length === 0" class="text-tp-muted text-xs">You have no servers yet.</p>
            <label v-for="s in servers" :key="s.id" class="flex items-center gap-2.5 cursor-pointer text-sm text-tp-text">
              <input v-model="form.serverIds" type="checkbox" :value="s.id" class="accent-tp-primary" />
              {{ s.name }}
            </label>
          </div>
        </div>

        <div class="max-w-xs">
          <label :class="labelClass">Expires</label>
          <select v-model.number="form.expiresInDays" :class="inputClass">
            <option v-for="e in EXPIRY" :key="e.days" :value="e.days">{{ e.label }}</option>
          </select>
        </div>

        <p v-if="formError" class="text-tp-danger text-xs">{{ formError }}</p>
        <div class="flex gap-2">
          <UiButton variant="primary" size="sm" :loading="saving" @click="createKey">Create key</UiButton>
          <UiButton variant="secondary" size="sm" @click="showForm = false">Cancel</UiButton>
        </div>
      </div>

      <!-- List -->
      <div v-if="loading" class="p-8 text-center">
        <Loader2 class="w-5 h-5 text-tp-muted animate-spin mx-auto" />
      </div>
      <p v-else-if="loadError" class="p-5 text-tp-danger text-sm">{{ loadError }}</p>
      <div v-else-if="keys.length === 0 && !showForm" class="p-8 text-center">
        <KeyRound class="w-7 h-7 text-tp-muted mx-auto mb-2" />
        <p class="text-tp-muted text-xs">No API keys yet.</p>
      </div>
      <div v-else class="divide-y divide-tp-border">
        <div v-for="k in keys" :key="k.id" class="px-5 py-3 flex items-start justify-between gap-3"
          :class="{ 'opacity-60': isExpired(k) }">
          <div class="min-w-0">
            <p class="text-tp-text text-sm font-medium">
              {{ k.name }}
              <code class="ml-1 text-[11px] text-tp-outline font-mono">{{ k.prefix }}…</code>
              <UiBadge v-if="isExpired(k)" variant="danger" size="sm" class="ml-1">expired</UiBadge>
            </p>
            <p class="text-tp-muted text-xs">{{ scopeText(k) }} · {{ serverText(k) }}</p>
            <p class="text-tp-muted text-xs mt-0.5">
              Created {{ fmt(k.created_at) }} · Expires {{ k.expires_at ? fmt(k.expires_at) : 'never' }} ·
              Last used {{ fmt(k.last_used_at) }}<template v-if="k.last_used_ip"> from {{ k.last_used_ip }}</template>
            </p>
          </div>
          <button class="p-1.5 rounded-xl text-tp-danger hover:bg-tp-danger/10 transition-colors shrink-0"
            title="Revoke" :disabled="busy === k.id" @click="revoke(k)">
            <Trash2 class="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
