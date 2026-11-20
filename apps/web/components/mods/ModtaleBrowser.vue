<script setup lang="ts">
// Search and install mods from Modtale (modtale.net) for one server. Used on
// the Mods page and in the server page's Mods tab. The panel proxies and
// caches the Modtale API, and builds the download link itself on install.
import { Search, Package, Download, ExternalLink, ChevronDown, ChevronUp } from '@lucide/vue'
import { useModsStore } from '~/stores/mods'
import type { ModtaleProject, ModtaleSearchResult, ModtaleVersion } from '~/types'

const props = defineProps<{ serverId: string }>()
const emit = defineEmits<{ toast: [message: string, type: 'success' | 'error'] }>()

const modsStore = useModsStore()
const api = useApi()

const TYPES = [
  { value: 'PLUGIN', label: 'Plugins' },
  { value: 'DATA', label: 'Data packs' },
  { value: 'ART', label: 'Art packs' },
] as const
const SORTS = [
  { value: '', label: 'Best match' },
  { value: 'downloads', label: 'Most downloaded' },
  { value: 'updated', label: 'Recently updated' },
  { value: 'new', label: 'Newest' },
] as const

const query = ref('')
const type = ref<string>('PLUGIN')
const sort = ref<string>('')
const results = ref<ModtaleProject[]>([])
const total = ref(0)
const page = ref(0)
const loading = ref(false)
const error = ref<string | null>(null)

let searchSeq = 0
async function search(nextPage = 0) {
  const seq = ++searchSeq
  loading.value = true
  error.value = null
  try {
    const data = await api.get<ModtaleSearchResult>('/modtale/search', {
      q: query.value.trim(),
      type: type.value,
      sort: sort.value,
      page: nextPage,
      pageSize: 20,
    })
    if (seq !== searchSeq) return
    results.value = nextPage === 0 ? data.projects : [...results.value, ...data.projects]
    total.value = data.total
    page.value = nextPage
  } catch (err: unknown) {
    if (seq !== searchSeq) return
    const e = err as { data?: { error?: string }; message?: string }
    error.value = e.data?.error ?? e.message ?? 'Modtale search failed'
    if (nextPage === 0) results.value = []
  } finally {
    if (seq === searchSeq) loading.value = false
  }
}

let timer: ReturnType<typeof setTimeout> | undefined
watch(query, () => {
  clearTimeout(timer)
  timer = setTimeout(() => search(0), 400)
})
watch([type, sort], () => search(0))
onMounted(() => search(0))
onBeforeUnmount(() => clearTimeout(timer))

// ── Versions ─────────────────────────────────────────────────────────────────
const expanded = ref<string | null>(null)
const versions = ref<Record<string, ModtaleVersion[]>>({})
const loadingVersions = ref<string | null>(null)

async function toggleVersions(p: ModtaleProject) {
  if (expanded.value === p.id) {
    expanded.value = null
    return
  }
  expanded.value = p.id
  if (versions.value[p.id]) return
  loadingVersions.value = p.id
  try {
    const data = await api.get<{ versions: ModtaleVersion[] }>(`/modtale/projects/${encodeURIComponent(p.id)}/versions`)
    versions.value[p.id] = data.versions ?? []
  } catch {
    versions.value[p.id] = []
    emit('toast', 'Could not load versions from Modtale', 'error')
  } finally {
    loadingVersions.value = null
  }
}

// ── Install ──────────────────────────────────────────────────────────────────
function installedVersion(projectId: string): string | null {
  const m = modsStore.installed.find(i => i.modtale_project_id === projectId)
  return m ? (m.version || '') : null
}

async function install(p: ModtaleProject, version = '') {
  if (!props.serverId) {
    emit('toast', 'Select a server first', 'error')
    return
  }
  try {
    const mod = await modsStore.installModtale(props.serverId, p.id, version, p.title)
    emit('toast', `${p.title} ${mod.version} is being installed`, 'success')
  } catch {
    emit('toast', modsStore.error ?? 'Install failed', 'error')
  }
}

function busy(projectId: string, version = '') {
  return modsStore.installingModtale === `${projectId}@${version}`
}

function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(n)
}

function formatDate(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString()
}

const typeLabel = (c: string) => TYPES.find(t => t.value === c)?.label.replace(/s$/, '') ?? c
</script>

<template>
  <div class="space-y-3">
    <!-- Filters -->
    <div class="flex flex-col sm:flex-row gap-2">
      <div class="relative flex-1 min-w-0">
        <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-tp-muted pointer-events-none" />
        <input
          v-model="query"
          type="search"
          placeholder="Search Modtale..."
          aria-label="Search Modtale"
          class="w-full bg-tp-surface2 border border-tp-border text-tp-text rounded-xl pl-9 pr-4 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50 focus:border-tp-primary transition-colors"
        />
      </div>
      <div class="flex gap-2">
        <select v-model="type" aria-label="Type"
          class="flex-1 sm:flex-none bg-tp-surface2 border border-tp-border text-tp-text rounded-xl px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-tp-primary/50">
          <option v-for="t in TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
        </select>
        <select v-model="sort" aria-label="Sort"
          class="flex-1 sm:flex-none bg-tp-surface2 border border-tp-border text-tp-text rounded-xl px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-tp-primary/50">
          <option v-for="s in SORTS" :key="s.value" :value="s.value">{{ s.label }}</option>
        </select>
      </div>
    </div>

    <div v-if="error" class="flex items-start gap-3 bg-tp-error/10 text-tp-error rounded-xl px-4 py-3 text-sm">
      <span class="font-medium shrink-0">Modtale unavailable:</span>
      <span class="opacity-80">{{ error }}</span>
    </div>

    <!-- Loading -->
    <div v-if="loading && results.length === 0" class="space-y-2">
      <div v-for="i in 5" :key="i" class="h-20 bg-tp-surface2 rounded-xl animate-pulse" />
    </div>

    <!-- Empty -->
    <div v-else-if="!loading && !error && results.length === 0" class="bg-tp-surface rounded-xl p-8 text-center">
      <p class="text-tp-muted text-sm">
        {{ query.trim() ? `Nothing on Modtale matches "${query.trim()}".` : 'No projects of this type on Modtale yet.' }}
      </p>
    </div>

    <!-- Results -->
    <div v-else-if="results.length" class="bg-tp-surface rounded-xl overflow-hidden">
      <div class="px-4 sm:px-5 py-2.5 border-b border-tp-border flex items-center justify-between gap-2">
        <p class="text-tp-outline text-xs">{{ total.toLocaleString() }} {{ total === 1 ? 'project' : 'projects' }}</p>
        <p class="text-tp-outline text-xs">from <a href="https://modtale.net" target="_blank" rel="noopener noreferrer" class="underline hover:text-tp-text">modtale.net</a></p>
      </div>
      <ul class="divide-y divide-tp-border">
        <li v-for="p in results" :key="p.id" class="px-4 sm:px-5 py-4">
          <div class="flex items-start gap-3 sm:gap-4">
            <img
              v-if="p.image_url"
              :src="p.image_url"
              :alt="''"
              loading="lazy"
              referrerpolicy="no-referrer"
              class="w-12 h-12 rounded-xl object-cover shrink-0 bg-tp-surface2"
            />
            <div v-else class="w-12 h-12 rounded-xl bg-tp-surface2 flex items-center justify-center shrink-0">
              <Package class="w-6 h-6 text-tp-muted" />
            </div>

            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-1.5 min-w-0">
                <p class="text-tp-text text-sm font-semibold truncate">{{ p.title }}</p>
                <a
                  :href="httpsHref(p.page_url)"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="text-tp-muted hover:text-tp-text transition-colors shrink-0"
                  :aria-label="`${p.title} on Modtale`"
                >
                  <ExternalLink class="w-3.5 h-3.5" />
                </a>
              </div>
              <p v-if="p.author" class="text-tp-outline text-xs">by {{ p.author }}</p>
              <p class="text-tp-muted text-xs mt-1 line-clamp-2">{{ p.description || 'No description.' }}</p>
              <p class="text-tp-outline text-xs mt-1">
                {{ typeLabel(p.classification) }} &middot; {{ formatCount(p.downloads) }} downloads
                <template v-if="formatDate(p.updated_at)"> &middot; updated {{ formatDate(p.updated_at) }}</template>
              </p>
            </div>

            <div class="flex flex-col items-end gap-1.5 shrink-0">
              <span v-if="installedVersion(p.id) !== null" class="text-tp-success text-xs font-medium mt-1">
                Installed{{ installedVersion(p.id) ? ` · ${installedVersion(p.id)}` : '' }}
              </span>
              <UiButton
                v-else
                variant="primary"
                size="sm"
                :loading="busy(p.id)"
                :disabled="!!modsStore.installingModtale || !serverId"
                title="Install the newest release"
                @click="install(p)"
              >
                <Download class="w-3.5 h-3.5" />
                Install
              </UiButton>
              <button
                type="button"
                class="flex items-center gap-1 text-xs text-tp-muted hover:text-tp-text transition-colors"
                :aria-expanded="expanded === p.id"
                @click="toggleVersions(p)"
              >
                Versions
                <component :is="expanded === p.id ? ChevronUp : ChevronDown" class="w-3.5 h-3.5" />
              </button>
            </div>
          </div>

          <!-- Version list -->
          <div v-if="expanded === p.id" class="mt-3 sm:ml-16 rounded-xl bg-tp-surface2">
            <p v-if="loadingVersions === p.id" class="px-4 py-3 text-xs text-tp-muted">Loading versions...</p>
            <p v-else-if="!versions[p.id]?.length" class="px-4 py-3 text-xs text-tp-muted">No installable versions.</p>
            <ul v-else class="divide-y divide-tp-border max-h-64 overflow-y-auto">
              <li v-for="v in versions[p.id]" :key="v.id" class="flex items-center gap-3 px-4 py-2">
                <div class="flex-1 min-w-0">
                  <p class="text-tp-text text-xs font-medium truncate">
                    {{ v.version }}
                    <span v-if="v.channel && v.channel !== 'RELEASE'"
                      class="ml-1 px-1.5 py-0.5 rounded bg-tp-warning/15 text-tp-warning text-[10px] font-semibold">{{ v.channel.toLowerCase() }}</span>
                  </p>
                  <p class="text-tp-outline text-[11px] truncate">
                    <template v-if="v.game_versions.length">Hytale {{ v.game_versions.slice(0, 3).join(', ') }}<template v-if="v.game_versions.length > 3"> +{{ v.game_versions.length - 3 }}</template> &middot; </template>{{ formatDate(v.released_at) }}
                  </p>
                </div>
                <span v-if="installedVersion(p.id) === v.version" class="text-tp-success text-xs font-medium">Installed</span>
                <UiButton
                  v-else-if="installedVersion(p.id) === null"
                  variant="secondary"
                  size="sm"
                  :loading="busy(p.id, v.version)"
                  :disabled="!!modsStore.installingModtale || !serverId"
                  @click="install(p, v.version)"
                >
                  Install
                </UiButton>
              </li>
            </ul>
            <p v-if="installedVersion(p.id) !== null && versions[p.id]?.length" class="px-4 py-2 text-[11px] text-tp-outline border-t border-tp-border">
              Already installed. To switch, use <b>Change version</b> on the Mods page.
            </p>
          </div>
        </li>
      </ul>
    </div>

    <div v-if="results.length < total" class="text-center">
      <UiButton variant="secondary" size="sm" :loading="loading" @click="search(page + 1)">Load more</UiButton>
    </div>

    <p class="text-tp-outline text-[11px] leading-relaxed">
      Modtale is a community mod site, not affiliated with Hypixel Studios or TalePanel. Mods run with your
      server's rights; install only what you trust and take a backup first. Changes apply after a restart.
    </p>
  </div>
</template>
