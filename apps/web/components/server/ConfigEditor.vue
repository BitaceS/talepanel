<script setup lang="ts">
import { AlertTriangle, Braces, FileJson, Loader2, RotateCcw, Save, SlidersHorizontal } from '@lucide/vue'
import { useApi } from '~/composables/useApi'
import {
  KNOWN_CONFIG_FILES, parseConfig, serializeConfig,
  type JsonValue,
} from '~/utils/configForm'

// Config tab: the server's JSON config files (config.json, permissions.json,
// whitelist.json, bans.json and any other *.json in the server root) as a
// form, with a raw JSON view next to it. Reads and writes go through the
// normal file routes, so the same permissions apply as in the Files tab.

interface FileEntry { name: string, size: number, is_dir: boolean }

const props = defineProps<{ serverId: string, status?: string }>()
const emit = defineEmits<{ toast: [msg: string, type: 'success' | 'error'] }>()

const api = useApi()

const files = ref<string[]>([])
const listLoading = ref(false)
const listError = ref('')

const selected = ref<string | null>(null)
const fileLoading = ref(false)
const fileError = ref('')
const saving = ref(false)

const original = ref('') // file content as last read or saved
const baseline = ref('') // original as the form would write it (formatting may differ)
const draft = shallowRef<JsonValue>(null) // form state
const rawText = ref('') // raw editor state
const mode = ref<'form' | 'raw'>('form')
const formBlocked = ref('') // why the form can't be used for this file
const rawError = ref('')

const running = computed(() => props.status === 'running' || props.status === 'starting')

function errorMessage(err: unknown, fallback: string): string {
  const e = err as { data?: { error?: string }, message?: string }
  return e.data?.error ?? e.message ?? fallback
}

async function loadList() {
  listLoading.value = true
  listError.value = ''
  try {
    const data = await api.get<{ entries: FileEntry[] }>(`/servers/${props.serverId}/files`, { path: '/' })
    const json = (data.entries ?? [])
      .filter(e => !e.is_dir && e.name.toLowerCase().endsWith('.json') && e.size <= 1_048_576)
      .map(e => e.name)
    const known = Object.keys(KNOWN_CONFIG_FILES).filter(n => json.includes(n))
    const other = json.filter(n => !(n in KNOWN_CONFIG_FILES)).sort((a, b) => a.localeCompare(b))
    files.value = [...known, ...other]
    if (!selected.value && files.value.length) await openFile(files.value[0]!)
  } catch (err) {
    listError.value = errorMessage(err, 'Failed to load the file list')
  } finally {
    listLoading.value = false
  }
}

const currentText = computed(() => {
  if (mode.value === 'raw') return rawText.value
  return serializeConfig(draft.value, original.value)
})
const dirty = computed(() => {
  if (!selected.value || fileError.value) return false
  const text = currentText.value
  return text !== original.value && text !== baseline.value
})

function loadInto(content: string) {
  original.value = content
  baseline.value = content
  rawText.value = content
  rawError.value = ''
  const parsed = parseConfig(content)
  if (!parsed.ok) {
    formBlocked.value = `Not valid JSON: ${parsed.error}`
    draft.value = null
    mode.value = 'raw'
    return
  }
  baseline.value = serializeConfig(parsed.value, content)
  draft.value = parsed.value
  formBlocked.value = parsed.formReason ?? ''
  if (formBlocked.value) mode.value = 'raw'
}

async function openFile(name: string) {
  if (name === selected.value) return
  if (dirty.value && !window.confirm(`Discard unsaved changes to ${selected.value}?`)) return
  selected.value = name
  fileLoading.value = true
  fileError.value = ''
  mode.value = 'form'
  try {
    const data = await api.get<{ content: string }>(`/servers/${props.serverId}/files/content`, { path: '/' + name })
    loadInto(data.content)
  } catch (err) {
    fileError.value = errorMessage(err, 'Failed to read the file')
  } finally {
    fileLoading.value = false
  }
}

function switchMode(next: 'form' | 'raw') {
  if (next === mode.value) return
  if (next === 'raw') {
    rawText.value = currentText.value
    rawError.value = ''
    mode.value = 'raw'
    return
  }
  const parsed = parseConfig(rawText.value)
  if (!parsed.ok) {
    rawError.value = `Fix the JSON first: ${parsed.error}`
    return
  }
  if (parsed.formReason) {
    rawError.value = parsed.formReason
    return
  }
  draft.value = parsed.value
  rawError.value = ''
  mode.value = 'form'
}

function revert() {
  loadInto(original.value)
  if (!formBlocked.value) mode.value = 'form'
}

async function save() {
  if (!selected.value || !dirty.value) return
  const content = currentText.value
  if (mode.value === 'raw') {
    const parsed = parseConfig(content)
    if (!parsed.ok) {
      rawError.value = `Not saved, the JSON is invalid: ${parsed.error}`
      return
    }
  }
  saving.value = true
  try {
    await api.put(`/servers/${props.serverId}/files/content`, { path: '/' + selected.value, content })
    loadInto(content)
    emit('toast', running.value ? `${selected.value} saved. Restart the server to apply it.` : `${selected.value} saved`, 'success')
  } catch (err) {
    emit('toast', errorMessage(err, 'Failed to save the file'), 'error')
  } finally {
    saving.value = false
  }
}

function beforeUnload(e: BeforeUnloadEvent) {
  if (dirty.value) e.preventDefault()
}
onMounted(() => {
  loadList()
  window.addEventListener('beforeunload', beforeUnload)
})
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
</script>

<template>
  <div class="space-y-3">
    <div
      v-if="running"
      class="flex items-start gap-2 rounded-xl bg-tp-warning/10 border border-tp-warning/30 px-4 py-3 text-sm text-tp-text"
    >
      <AlertTriangle class="w-4 h-4 text-tp-warning shrink-0 mt-0.5" />
      <p>
        The server reads these files when it starts, so changes apply after a restart.
        A running server can also write its own copy back when it stops. To be safe, stop it, save, then start it.
      </p>
    </div>

    <div class="grid gap-3 md:grid-cols-[14rem_1fr]">
      <!-- File list -->
      <nav class="bg-tp-surface rounded-xl p-2 h-fit" aria-label="Config files">
        <div v-if="listLoading" class="flex items-center gap-2 px-2 py-3 text-sm text-tp-muted">
          <Loader2 class="w-4 h-4 animate-spin" /> Loading files…
        </div>
        <p v-else-if="listError" class="px-2 py-3 text-sm text-tp-danger">{{ listError }}</p>
        <p v-else-if="!files.length" class="px-2 py-3 text-sm text-tp-muted">
          No config files yet. Hytale creates them on the first start.
        </p>
        <ul v-else class="space-y-0.5">
          <li v-for="name in files" :key="name">
            <button
              type="button"
              :class="[
                'w-full text-left rounded-lg px-3 py-2 transition-colors',
                selected === name ? 'bg-tp-surface2 text-tp-text' : 'text-tp-muted hover:bg-tp-surface2 hover:text-tp-text',
              ]"
              :aria-current="selected === name ? 'true' : undefined"
              @click="openFile(name)"
            >
              <span class="block text-sm font-medium">{{ KNOWN_CONFIG_FILES[name]?.label ?? name }}</span>
              <span class="block text-xs font-mono text-tp-outline">{{ name }}</span>
            </button>
          </li>
        </ul>
      </nav>

      <!-- Editor -->
      <section v-if="selected" class="bg-tp-surface rounded-xl overflow-hidden min-w-0">
        <header class="flex flex-wrap items-center justify-between gap-2 px-4 py-2.5 border-b border-tp-border">
          <div class="flex items-center gap-2 min-w-0">
            <FileJson class="w-4 h-4 text-tp-muted shrink-0" />
            <span class="text-sm font-medium text-tp-text truncate">{{ selected }}</span>
            <span v-if="dirty" class="text-xs text-tp-warning">Unsaved</span>
          </div>
          <div class="flex items-center gap-2">
            <div class="flex rounded-lg bg-tp-surface2 p-0.5" role="group" aria-label="Editor mode">
              <button
                type="button"
                :class="['flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium', mode === 'form' ? 'bg-tp-surface3 text-tp-text' : 'text-tp-muted hover:text-tp-text', formBlocked ? 'opacity-50 cursor-not-allowed' : '']"
                :disabled="!!formBlocked"
                :aria-pressed="mode === 'form'"
                @click="switchMode('form')"
              >
                <SlidersHorizontal class="w-3.5 h-3.5" /> Form
              </button>
              <button
                type="button"
                :class="['flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium', mode === 'raw' ? 'bg-tp-surface3 text-tp-text' : 'text-tp-muted hover:text-tp-text']"
                :aria-pressed="mode === 'raw'"
                @click="switchMode('raw')"
              >
                <Braces class="w-3.5 h-3.5" /> Raw JSON
              </button>
            </div>
            <UiButton variant="secondary" size="sm" :disabled="!dirty || saving" @click="revert">
              <RotateCcw class="w-3.5 h-3.5" /> Revert
            </UiButton>
            <UiButton variant="primary" size="sm" :disabled="!dirty" :loading="saving" @click="save">
              <Save class="w-3.5 h-3.5" /> Save
            </UiButton>
          </div>
        </header>

        <div v-if="fileLoading" class="flex items-center gap-2 p-4 text-sm text-tp-muted">
          <Loader2 class="w-4 h-4 animate-spin" /> Reading {{ selected }}…
        </div>
        <p v-else-if="fileError" class="p-4 text-sm text-tp-danger">{{ fileError }}</p>
        <template v-else>
          <p v-if="KNOWN_CONFIG_FILES[selected]" class="px-4 pt-3 text-xs text-tp-muted">
            {{ KNOWN_CONFIG_FILES[selected]!.description }}
          </p>
          <p v-if="formBlocked" class="mx-4 mt-3 text-xs text-tp-warning">{{ formBlocked }}</p>
          <p v-if="rawError" class="mx-4 mt-3 text-xs text-tp-danger" role="alert">{{ rawError }}</p>

          <div v-if="mode === 'form'" class="p-4 max-w-2xl">
            <ServerConfigField
              :key="selected"
              :file="selected"
              :path="[]"
              :value="draft"
              @update="v => (draft = v)"
            />
          </div>
          <textarea
            v-else
            v-model="rawText"
            class="mt-3 w-full bg-tp-surface-lowest text-tp-tertiary font-mono text-xs p-4 focus:outline-none resize-y"
            style="min-height: 480px; tab-size: 2;"
            spellcheck="false"
            :aria-label="`Raw JSON of ${selected}`"
            @input="rawError = ''"
          />
        </template>
      </section>
    </div>
  </div>
</template>
