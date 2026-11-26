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
const { t, te } = useI18n()

const TYPES = [
  { value: 'PLUGIN', label: 'mods.modtale.types.PLUGIN' },
  { value: 'DATA', label: 'mods.modtale.types.DATA' },
  { value: 'ART', label: 'mods.modtale.types.ART' },
] as const
const SORTS = [
  { value: '', label: 'mods.modtale.sorts.best' },
  { value: 'downloads', label: 'mods.modtale.sorts.downloads' },
  { value: 'updated', label: 'mods.modtale.sorts.updated' },
  { value: 'new', label: 'mods.modtale.sorts.new' },
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
    error.value = e.data?.error ?? e.message ?? t('mods.modtale.searchFailed')
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
    emit('toast', t('mods.modtale.versionsFailed'), 'error')
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
    emit('toast', t('mods.modtale.selectServerFirst'), 'error')
    return
  }
  try {
    const mod = await modsStore.installModtale(props.serverId, p.id, version, p.title)
    emit('toast', t('mods.modtale.installing', { name: p.title, version: mod.version }), 'success')
  } catch {
    emit('toast', modsStore.error ?? t('mods.modtale.installFailed'), 'error')
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

const typeLabel = (c: string) => te(`mods.modtale.typeSingular.${c}`) ? t(`mods.modtale.typeSingular.${c}`) : c
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
          :placeholder="t('mods.modtale.searchPlaceholder')"
          :aria-label="t('mods.modtale.searchLabel')"
          class="w-full bg-tp-surface2 border border-tp-border text-tp-text rounded-xl pl-9 pr-4 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50 focus:border-tp-primary transition-colors"
        />
      </div>
      <div class="flex gap-2">
        <select v-model="type" :aria-label="t('mods.modtale.typeLabel')"
          class="flex-1 sm:flex-none bg-tp-surface2 border border-tp-border text-tp-text rounded-xl px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-tp-primary/50">
          <option v-for="ty in TYPES" :key="ty.value" :value="ty.value">{{ t(ty.label) }}</option>
        </select>
        <select v-model="sort" :aria-label="t('mods.modtale.sortLabel')"
          class="flex-1 sm:flex-none bg-tp-surface2 border border-tp-border text-tp-text rounded-xl px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-tp-primary/50">
          <option v-for="s in SORTS" :key="s.value" :value="s.value">{{ t(s.label) }}</option>
        </select>
      </div>
    </div>

    <div v-if="error" class="flex items-start gap-3 bg-tp-error/10 text-tp-error rounded-xl px-4 py-3 text-sm">
      <span class="font-medium shrink-0">{{ t('mods.modtale.unavailable') }}</span>
      <span class="opacity-80">{{ error }}</span>
    </div>

    <!-- Loading -->
    <div v-if="loading && results.length === 0" class="space-y-2">
      <div v-for="i in 5" :key="i" class="h-20 bg-tp-surface2 rounded-xl animate-pulse" />
    </div>

    <!-- Empty -->
    <div v-else-if="!loading && !error && results.length === 0" class="bg-tp-surface rounded-xl p-8 text-center">
      <p class="text-tp-muted text-sm">
        {{ query.trim() ? t('mods.modtale.noMatch', { query: query.trim() }) : t('mods.modtale.noProjects') }}
      </p>
    </div>

    <!-- Results -->
    <div v-else-if="results.length" class="bg-tp-surface rounded-xl overflow-hidden">
      <div class="px-4 sm:px-5 py-2.5 border-b border-tp-border flex items-center justify-between gap-2">
        <p class="text-tp-outline text-xs">{{ t('mods.modtale.projects', { count: total.toLocaleString() }, total) }}</p>
        <i18n-t keypath="mods.modtale.from" tag="p" class="text-tp-outline text-xs" scope="global">
          <template #link><a href="https://modtale.net" target="_blank" rel="noopener noreferrer" class="underline hover:text-tp-text">modtale.net</a></template>
        </i18n-t>
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
                  :aria-label="t('mods.modtale.onModtale', { name: p.title })"
                >
                  <ExternalLink class="w-3.5 h-3.5" />
                </a>
              </div>
              <p v-if="p.author" class="text-tp-outline text-xs">{{ t('mods.modtale.byAuthor', { author: p.author }) }}</p>
              <p class="text-tp-muted text-xs mt-1 line-clamp-2">{{ p.description || t('mods.modtale.noDescription') }}</p>
              <p class="text-tp-outline text-xs mt-1">
                {{ typeLabel(p.classification) }} &middot; {{ t('mods.modtale.downloads', { n: formatCount(p.downloads) }) }}
                <template v-if="formatDate(p.updated_at)"> &middot; {{ t('mods.modtale.updated', { date: formatDate(p.updated_at) }) }}</template>
              </p>
            </div>

            <div class="flex flex-col items-end gap-1.5 shrink-0">
              <span v-if="installedVersion(p.id) !== null" class="text-tp-success text-xs font-medium mt-1">
                {{ t('mods.modtale.installed') }}{{ installedVersion(p.id) ? ` · ${installedVersion(p.id)}` : '' }}
              </span>
              <UiButton
                v-else
                variant="primary"
                size="sm"
                :loading="busy(p.id)"
                :disabled="!!modsStore.installingModtale || !serverId"
                :title="t('mods.modtale.installNewest')"
                @click="install(p)"
              >
                <Download class="w-3.5 h-3.5" />
                {{ t('mods.modtale.install') }}
              </UiButton>
              <button
                type="button"
                class="flex items-center gap-1 text-xs text-tp-muted hover:text-tp-text transition-colors"
                :aria-expanded="expanded === p.id"
                @click="toggleVersions(p)"
              >
                {{ t('mods.modtale.versions') }}
                <component :is="expanded === p.id ? ChevronUp : ChevronDown" class="w-3.5 h-3.5" />
              </button>
            </div>
          </div>

          <!-- Version list -->
          <div v-if="expanded === p.id" class="mt-3 sm:ml-16 rounded-xl bg-tp-surface2">
            <p v-if="loadingVersions === p.id" class="px-4 py-3 text-xs text-tp-muted">{{ t('mods.modtale.loadingVersions') }}</p>
            <p v-else-if="!versions[p.id]?.length" class="px-4 py-3 text-xs text-tp-muted">{{ t('mods.modtale.noVersions') }}</p>
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
                <span v-if="installedVersion(p.id) === v.version" class="text-tp-success text-xs font-medium">{{ t('mods.modtale.installed') }}</span>
                <UiButton
                  v-else-if="installedVersion(p.id) === null"
                  variant="secondary"
                  size="sm"
                  :loading="busy(p.id, v.version)"
                  :disabled="!!modsStore.installingModtale || !serverId"
                  @click="install(p, v.version)"
                >
                  {{ t('mods.modtale.install') }}
                </UiButton>
              </li>
            </ul>
            <p v-if="installedVersion(p.id) !== null && versions[p.id]?.length" class="px-4 py-2 text-[11px] text-tp-outline border-t border-tp-border">
              <i18n-t keypath="mods.modtale.alreadyInstalled" scope="global">
                <template #action><b>{{ t('mods.page.changeVersion') }}</b></template>
              </i18n-t>
            </p>
          </div>
        </li>
      </ul>
    </div>

    <div v-if="results.length < total" class="text-center">
      <UiButton variant="secondary" size="sm" :loading="loading" @click="search(page + 1)">{{ t('mods.modtale.loadMore') }}</UiButton>
    </div>

    <p class="text-tp-outline text-[11px] leading-relaxed">
      {{ t('mods.modtale.disclaimer') }}
    </p>
  </div>
</template>
