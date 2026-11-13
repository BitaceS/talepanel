<script setup lang="ts">
import { LayoutTemplate, Plus, Trash2, RefreshCw, Copy } from '@lucide/vue'
import { useServerTemplatesStore } from '~/stores/serverTemplates'
import { useServersStore } from '~/stores/servers'
import type { ServerTemplate, ServerTemplateSettings } from '~/types'

// Server templates (admin). Built-ins ship with the panel and are read-only;
// custom templates can be created by hand or saved from an existing server.

const emit = defineEmits<{ toast: [msg: string, type: 'success' | 'error'] }>()

const store = useServerTemplatesStore()
const serversStore = useServersStore()
const loading = ref(false)
const loadError = ref('')

function errMsg(err: unknown, fallback: string): string {
  const e = err as { data?: { error?: string } }
  return e.data?.error ?? fallback
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    await store.fetchTemplates(true)
  } catch (err: unknown) {
    loadError.value = errMsg(err, 'Failed to load templates')
  } finally {
    loading.value = false
  }
}
onMounted(load)

// Launch flags a template may set; mirrors the API allowlist.
const launchFlagOptions = [
  { flag: '--accept-early-plugins', label: 'Accept early plugins', hint: 'required by mods that load before the server boots' },
  { flag: '--disable-sentry', label: 'Disable crash reporting', hint: 'no crash reports to Hypixel (Sentry)' },
  { flag: '--allow-op', label: 'Allow /op', hint: 'only admins can create servers from this template' },
]

// ── Create by hand ──────────────────────────────────────────────────────────
const showCreate = ref(false)
const saving = ref(false)
const formError = ref('')
const form = reactive({
  name: '',
  description: '',
  icon: 'dns',
  hytale_version: 'release',
  ram_limit_mb: '' as string | number,
  cpu_limit: '' as string | number,
  disk_limit_mb: '' as string | number,
  crash_limit: '' as string | number,
  crash_window_s: '' as string | number,
  auto_restart: true,
  launch_flags: [] as string[],
  network_role: 'none' as 'none' | 'hub' | 'gamemode',
})

function openCreate() {
  Object.assign(form, {
    name: '', description: '', icon: 'dns', hytale_version: 'release',
    ram_limit_mb: '', cpu_limit: '', disk_limit_mb: '', crash_limit: '', crash_window_s: '',
    auto_restart: true, launch_flags: [], network_role: 'none',
  })
  formError.value = ''
  showCreate.value = true
}

function optInt(v: string | number): number | undefined {
  if (v === '' || v === undefined) return undefined
  const n = parseInt(String(v), 10)
  return Number.isNaN(n) ? undefined : n
}

async function submitCreate() {
  if (!form.name.trim()) {
    formError.value = 'Name is required'
    return
  }
  const settings: ServerTemplateSettings = {
    hytale_version: form.hytale_version,
    ram_limit_mb: optInt(form.ram_limit_mb),
    cpu_limit: optInt(form.cpu_limit),
    disk_limit_mb: optInt(form.disk_limit_mb),
    crash_limit: optInt(form.crash_limit),
    crash_window_s: optInt(form.crash_window_s),
    auto_restart: form.auto_restart,
    launch_flags: form.launch_flags.length ? [...form.launch_flags] : undefined,
    network_role: form.network_role === 'none' ? undefined : form.network_role,
  }
  saving.value = true
  formError.value = ''
  try {
    await store.createTemplate({
      name: form.name.trim(),
      description: form.description.trim(),
      icon: form.icon.trim(),
      settings,
    })
    showCreate.value = false
    emit('toast', 'Template created', 'success')
  } catch (err: unknown) {
    formError.value = errMsg(err, 'Failed to create template')
  } finally {
    saving.value = false
  }
}

// ── Save from server ────────────────────────────────────────────────────────
const showFromServer = ref(false)
const fromServer = reactive({ server_id: '', name: '', description: '', icon: 'dns' })

async function openFromServer() {
  Object.assign(fromServer, { server_id: '', name: '', description: '', icon: 'dns' })
  formError.value = ''
  showFromServer.value = true
  if (!serversStore.servers.length) await serversStore.fetchServers()
}

watch(() => fromServer.server_id, (id) => {
  const srv = serversStore.servers.find(s => s.id === id)
  if (srv && !fromServer.name) fromServer.name = `${srv.name} settings`
})

async function submitFromServer() {
  if (!fromServer.server_id || !fromServer.name.trim()) {
    formError.value = 'Pick a server and give the template a name'
    return
  }
  saving.value = true
  formError.value = ''
  try {
    await store.createFromServer(fromServer.server_id, {
      name: fromServer.name.trim(),
      description: fromServer.description.trim(),
      icon: fromServer.icon.trim(),
    })
    showFromServer.value = false
    emit('toast', 'Template saved from server', 'success')
  } catch (err: unknown) {
    formError.value = errMsg(err, 'Failed to save template')
  } finally {
    saving.value = false
  }
}

// ── Delete ──────────────────────────────────────────────────────────────────
const deleting = ref<string | null>(null)

async function remove(t: ServerTemplate) {
  if (!confirm(`Delete the template "${t.name}"? Servers created from it are not affected.`)) return
  deleting.value = t.id
  try {
    await store.deleteTemplate(t.id)
    emit('toast', 'Template deleted', 'success')
  } catch (err: unknown) {
    emit('toast', errMsg(err, 'Failed to delete template'), 'error')
  } finally {
    deleting.value = null
  }
}

const inputClass = 'w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text'
const labelClass = 'text-xs font-semibold text-tp-muted uppercase tracking-wider'
</script>

<template>
  <div class="space-y-4">
    <div class="bg-tp-surface rounded-xl p-5 flex items-start justify-between gap-4 flex-wrap">
      <div>
        <h3 class="text-tp-text font-display font-semibold text-lg flex items-center gap-2">
          <LayoutTemplate class="w-4 h-4 text-tp-primary" /> Server templates
        </h3>
        <p class="text-tp-muted text-sm mt-1 max-w-2xl">
          Presets offered when someone creates a server. A template only fills in defaults: the same rules apply as
          for settings chosen by hand, so a user cannot get admin-only options (such as <span class="font-mono">--allow-op</span>
          or a network role) through a template.
        </p>
      </div>
      <div class="flex gap-2">
        <UiButton variant="secondary" size="sm" @click="openFromServer">
          <Copy class="w-3.5 h-3.5" /> Save from server
        </UiButton>
        <UiButton variant="primary" size="sm" @click="openCreate">
          <Plus class="w-3.5 h-3.5" /> New template
        </UiButton>
      </div>
    </div>

    <div v-if="loading" class="bg-tp-surface rounded-xl p-8 text-center">
      <RefreshCw class="w-6 h-6 text-tp-muted animate-spin mx-auto" />
    </div>
    <div v-else-if="loadError" class="bg-tp-error/10 text-tp-error rounded-xl p-4 text-sm">{{ loadError }}</div>
    <template v-else>
      <div v-for="group in [
        { title: 'Custom', items: store.custom, empty: 'No custom templates yet. Create one or save the settings of an existing server.' },
        { title: 'Built-in', items: store.builtin, empty: '' },
      ]" :key="group.title" class="space-y-2">
        <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline px-1">{{ group.title }}</p>
        <div v-if="!group.items.length && group.empty" class="bg-tp-surface rounded-xl p-6 text-center text-tp-muted text-sm">
          {{ group.empty }}
        </div>
        <div v-for="t in group.items" :key="t.id" class="bg-tp-surface rounded-xl p-4 flex items-start justify-between gap-3">
          <div class="flex gap-3 min-w-0">
            <span class="material-symbols-outlined text-tp-primary text-xl shrink-0">{{ t.icon || 'dns' }}</span>
            <div class="min-w-0">
              <p class="text-tp-text text-sm font-medium">{{ t.name }}</p>
              <p v-if="t.description" class="text-tp-muted text-xs mt-0.5">{{ t.description }}</p>
              <div class="flex flex-wrap gap-1 mt-1.5">
                <span
                  v-for="chip in templateSummary(t.settings)"
                  :key="chip"
                  class="text-[10px] px-1.5 py-0.5 rounded bg-tp-surface3 text-tp-muted font-mono"
                >{{ chip }}</span>
              </div>
            </div>
          </div>
          <UiBadge v-if="t.is_builtin" variant="muted" size="sm">read-only</UiBadge>
          <UiButton v-else variant="danger" size="sm" :loading="deleting === t.id" @click="remove(t)">
            <Trash2 class="w-3.5 h-3.5" /> Delete
          </UiButton>
        </div>
      </div>
    </template>

    <!-- New template -->
    <UiModal :open="showCreate" title="New server template" size="lg" @close="showCreate = false">
      <form class="space-y-4" @submit.prevent="submitCreate">
        <div v-if="formError" class="bg-tp-error/10 rounded-xl px-3 py-2.5 text-tp-error text-sm">{{ formError }}</div>
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <div class="sm:col-span-2">
            <UiInput v-model="form.name" label="Name" placeholder="e.g. PvP arena" :required="true" />
          </div>
          <UiInput v-model="form.icon" label="Icon" placeholder="Material Symbols name, e.g. swords" />
        </div>
        <UiInput v-model="form.description" label="Description" placeholder="Shown on the template card" />

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div class="flex flex-col gap-1.5">
            <label for="tmpl-version" :class="labelClass">Hytale version</label>
            <select id="tmpl-version" v-model="form.hytale_version" :class="inputClass">
              <option value="release">Release</option>
              <option value="pre-release">Pre-release</option>
            </select>
          </div>
          <div class="flex flex-col gap-1.5">
            <label for="tmpl-role" :class="labelClass">Network role</label>
            <select id="tmpl-role" v-model="form.network_role" :class="inputClass">
              <option value="none">Not in the network</option>
              <option value="hub">Hub (lobby)</option>
              <option value="gamemode">Gamemode</option>
            </select>
          </div>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <UiInput v-model="form.ram_limit_mb" type="number" label="RAM (MB)" placeholder="default 2048" />
          <UiInput v-model="form.cpu_limit" type="number" label="CPU (%)" placeholder="100 = 1 core" />
          <UiInput v-model="form.disk_limit_mb" type="number" label="Disk (MB)" placeholder="default 10240" />
        </div>
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-3 items-end">
          <UiInput v-model="form.crash_limit" type="number" label="Crash limit" placeholder="default 3" />
          <UiInput v-model="form.crash_window_s" type="number" label="Crash window (s)" placeholder="default 300" />
          <label class="flex items-center gap-2 text-sm text-tp-text pb-2">
            <input v-model="form.auto_restart" type="checkbox" class="accent-tp-primary" />
            Auto restart
          </label>
        </div>

        <div class="flex flex-col gap-1.5">
          <span :class="labelClass">Launch flags</span>
          <label v-for="o in launchFlagOptions" :key="o.flag" class="flex items-center gap-2 text-sm text-tp-text">
            <input v-model="form.launch_flags" type="checkbox" :value="o.flag" class="accent-tp-primary" />
            {{ o.label }} <span class="font-mono text-xs text-tp-muted">{{ o.flag }}</span>
            <span class="text-xs text-tp-muted">· {{ o.hint }}</span>
          </label>
        </div>
      </form>
      <template #footer>
        <UiButton variant="ghost" size="md" @click="showCreate = false">Cancel</UiButton>
        <UiButton variant="primary" size="md" :loading="saving" @click="submitCreate">Create template</UiButton>
      </template>
    </UiModal>

    <!-- Save from server -->
    <UiModal :open="showFromServer" title="Save server settings as template" size="md" @close="showFromServer = false">
      <form class="space-y-4" @submit.prevent="submitFromServer">
        <div v-if="formError" class="bg-tp-error/10 rounded-xl px-3 py-2.5 text-tp-error text-sm">{{ formError }}</div>
        <div class="flex flex-col gap-1.5">
          <label for="tmpl-from-server" :class="labelClass">Server</label>
          <select id="tmpl-from-server" v-model="fromServer.server_id" :class="inputClass">
            <option value="" disabled>Choose a server…</option>
            <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
          <p class="text-tp-muted text-xs">
            Copies version, RAM, CPU, disk, auto restart, crash limits, launch flags and network role. Files and worlds
            are not copied.
          </p>
        </div>
        <UiInput v-model="fromServer.name" label="Template name" :required="true" />
        <UiInput v-model="fromServer.description" label="Description" />
        <UiInput v-model="fromServer.icon" label="Icon" placeholder="Material Symbols name" />
      </form>
      <template #footer>
        <UiButton variant="ghost" size="md" @click="showFromServer = false">Cancel</UiButton>
        <UiButton variant="primary" size="md" :loading="saving" @click="submitFromServer">Save template</UiButton>
      </template>
    </UiModal>
  </div>
</template>
