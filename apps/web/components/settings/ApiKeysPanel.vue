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
const { t, locale } = useI18n()

const SCOPES: { id: string; label: string; detail: string }[] = [
  { id: 'power', label: 'settings.apiKeys.scopes.power.label', detail: 'settings.apiKeys.scopes.power.detail' },
  { id: 'console', label: 'settings.apiKeys.scopes.console.label', detail: 'settings.apiKeys.scopes.console.detail' },
  { id: 'manage', label: 'settings.apiKeys.scopes.manage.label', detail: 'settings.apiKeys.scopes.manage.detail' },
  { id: 'backups', label: 'settings.apiKeys.scopes.backups.label', detail: 'settings.apiKeys.scopes.backups.detail' },
  { id: 'players', label: 'settings.apiKeys.scopes.players.label', detail: 'settings.apiKeys.scopes.players.detail' },
]
const EXPIRY = [
  { days: 30, label: 'settings.apiKeys.expiry.days30' },
  { days: 90, label: 'settings.apiKeys.expiry.days90' },
  { days: 365, label: 'settings.apiKeys.expiry.year' },
  { days: 0, label: 'common.never' },
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
    loadError.value = apiError(err, t('settings.apiKeys.loadFailed'))
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
    formError.value = t('settings.apiKeys.nameRequired')
    return
  }
  if (!form.allServers && form.serverIds.length === 0) {
    formError.value = t('settings.apiKeys.serverRequired')
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
    formError.value = apiError(err, t('settings.apiKeys.createFailed'))
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
    emit('toast', t('settings.apiKeys.copyFailed'), 'error')
  }
}

// ── Revoke ─────────────────────────────────────────────────────────────────
const busy = ref<string | null>(null)

async function revoke(k: ApiKey) {
  if (!window.confirm(t('settings.apiKeys.revokeConfirm', { name: k.name }))) return
  busy.value = k.id
  try {
    await api.delete(`/api-keys/${k.id}`)
    keys.value = keys.value.filter(x => x.id !== k.id)
    emit('toast', t('settings.apiKeys.revoked'), 'success')
  } catch (err: unknown) {
    emit('toast', apiError(err, t('settings.apiKeys.revokeFailed')), 'error')
  } finally {
    busy.value = null
  }
}

// ── Display ────────────────────────────────────────────────────────────────
function scopeText(k: ApiKey): string {
  const names = k.scopes.map((s) => {
    const scope = SCOPES.find(x => x.id === s)
    return scope ? t(scope.label) : s
  })
  return names.length ? t('settings.apiKeys.readPlus', { scopes: names.join(', ') }) : t('settings.apiKeys.readOnly')
}

function serverText(k: ApiKey): string {
  if (!k.server_ids) return t('settings.apiKeys.allYourServers')
  const names = k.server_ids.map(id => servers.value.find(s => s.id === id)?.name ?? t('settings.apiKeys.deletedServer'))
  return names.join(', ')
}

function isExpired(k: ApiKey): boolean {
  return !!k.expires_at && new Date(k.expires_at) <= new Date()
}

function fmt(iso: string | null): string {
  return iso ? new Date(iso).toLocaleString(locale.value) : '—'
}

const inputClass = 'w-full bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50'
const labelClass = 'block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1'
</script>

<template>
  <div class="space-y-5">
    <div class="bg-tp-surface rounded-xl overflow-hidden">
      <div class="px-5 py-4 border-b border-tp-border flex items-start justify-between gap-3">
        <div>
          <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('settings.index.sections.apiKeys') }}</h3>
          <i18n-t keypath="settings.apiKeys.intro" tag="p" class="text-tp-muted text-xs mt-0.5" scope="global">
            <template #header>
              <span class="font-mono">Authorization: Bearer tp_…</span>
            </template>
          </i18n-t>
        </div>
        <UiButton v-if="!showForm" variant="primary" size="sm" class="shrink-0" @click="openForm">
          <Plus class="w-3.5 h-3.5" /> {{ t('settings.apiKeys.newKey') }}
        </UiButton>
      </div>

      <!-- The new key, once -->
      <div v-if="created" class="p-5 bg-tp-primary/10 space-y-2" role="status">
        <p class="text-tp-text text-sm font-semibold">{{ t('settings.apiKeys.created', { name: created.name }) }}</p>
        <p class="text-tp-muted text-xs">{{ t('settings.apiKeys.copyNow') }}</p>
        <div class="flex items-center gap-2">
          <code class="flex-1 min-w-0 break-all bg-tp-surface2 rounded-lg px-3 py-2 text-xs font-mono text-tp-text select-all">{{ created.key }}</code>
          <UiButton variant="secondary" size="sm" class="shrink-0" @click="copyKey">
            <Check v-if="copied" class="w-3.5 h-3.5" /><Copy v-else class="w-3.5 h-3.5" />
            {{ copied ? t('actions.copied') : t('actions.copy') }}
          </UiButton>
        </div>
        <UiButton variant="secondary" size="sm" @click="created = null">{{ t('auth.recoveryCodes.done') }}</UiButton>
      </div>

      <!-- Create form -->
      <div v-if="showForm" class="p-5 space-y-4 border-b border-tp-border">
        <div>
          <label :class="labelClass">{{ t('settings.apiKeys.name') }}</label>
          <input v-model="form.name" :class="inputClass" maxlength="64" :placeholder="t('settings.apiKeys.namePlaceholder')" />
        </div>

        <div>
          <span :class="labelClass">{{ t('settings.apiKeys.rights') }}</span>
          <div class="space-y-1.5">
            <label v-for="s in SCOPES" :key="s.id" class="flex items-start gap-2.5 cursor-pointer">
              <input v-model="form.scopes" type="checkbox" :value="s.id" class="mt-0.5 accent-tp-primary" />
              <span class="text-sm">
                <span class="text-tp-text font-medium">{{ t(s.label) }}</span>
                <span class="text-tp-muted text-xs"> — {{ t(s.detail) }}</span>
              </span>
            </label>
          </div>
          <p class="text-tp-muted text-xs mt-1.5">{{ t('settings.apiKeys.rightsHint') }}</p>
        </div>

        <div>
          <span :class="labelClass">{{ t('nav.servers') }}</span>
          <label class="flex items-center gap-2.5 cursor-pointer text-sm text-tp-text">
            <input v-model="form.allServers" type="radio" :value="true" class="accent-tp-primary" />
            {{ t('settings.apiKeys.allServers') }}
          </label>
          <label class="flex items-center gap-2.5 cursor-pointer text-sm text-tp-text mt-1">
            <input v-model="form.allServers" type="radio" :value="false" class="accent-tp-primary" />
            {{ t('settings.apiKeys.onlyThese') }}
          </label>
          <div v-if="!form.allServers" class="mt-2 ml-6 space-y-1 max-h-48 overflow-y-auto">
            <p v-if="servers.length === 0" class="text-tp-muted text-xs">{{ t('settings.apiKeys.noServers') }}</p>
            <label v-for="s in servers" :key="s.id" class="flex items-center gap-2.5 cursor-pointer text-sm text-tp-text">
              <input v-model="form.serverIds" type="checkbox" :value="s.id" class="accent-tp-primary" />
              {{ s.name }}
            </label>
          </div>
        </div>

        <div class="max-w-xs">
          <label :class="labelClass">{{ t('settings.apiKeys.expires') }}</label>
          <select v-model.number="form.expiresInDays" :class="inputClass">
            <option v-for="e in EXPIRY" :key="e.days" :value="e.days">{{ t(e.label) }}</option>
          </select>
        </div>

        <p v-if="formError" class="text-tp-danger text-xs">{{ formError }}</p>
        <div class="flex gap-2">
          <UiButton variant="primary" size="sm" :loading="saving" @click="createKey">{{ t('settings.apiKeys.createKey') }}</UiButton>
          <UiButton variant="secondary" size="sm" @click="showForm = false">{{ t('actions.cancel') }}</UiButton>
        </div>
      </div>

      <!-- List -->
      <div v-if="loading" class="p-8 text-center">
        <Loader2 class="w-5 h-5 text-tp-muted animate-spin mx-auto" />
      </div>
      <p v-else-if="loadError" class="p-5 text-tp-danger text-sm">{{ loadError }}</p>
      <div v-else-if="keys.length === 0 && !showForm" class="p-8 text-center">
        <KeyRound class="w-7 h-7 text-tp-muted mx-auto mb-2" />
        <p class="text-tp-muted text-xs">{{ t('settings.apiKeys.empty') }}</p>
      </div>
      <div v-else class="divide-y divide-tp-border">
        <div v-for="k in keys" :key="k.id" class="px-5 py-3 flex items-start justify-between gap-3"
          :class="{ 'opacity-60': isExpired(k) }">
          <div class="min-w-0">
            <p class="text-tp-text text-sm font-medium">
              {{ k.name }}
              <code class="ml-1 text-[11px] text-tp-outline font-mono">{{ k.prefix }}…</code>
              <UiBadge v-if="isExpired(k)" variant="danger" size="sm" class="ml-1">{{ t('settings.apiKeys.expired') }}</UiBadge>
            </p>
            <p class="text-tp-muted text-xs">{{ scopeText(k) }} · {{ serverText(k) }}</p>
            <p class="text-tp-muted text-xs mt-0.5">
              {{ t('settings.apiKeys.meta', { created: fmt(k.created_at), expires: k.expires_at ? fmt(k.expires_at) : t('settings.apiKeys.never') }) }} ·
              {{ k.last_used_ip ? t('settings.apiKeys.lastUsedFrom', { date: fmt(k.last_used_at), ip: k.last_used_ip }) : t('settings.apiKeys.lastUsed', { date: fmt(k.last_used_at) }) }}
            </p>
          </div>
          <button class="p-1.5 rounded-xl text-tp-danger hover:bg-tp-danger/10 transition-colors shrink-0"
            :title="t('settings.security.revoke')" :disabled="busy === k.id" @click="revoke(k)">
            <Trash2 class="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
