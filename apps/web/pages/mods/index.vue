<script setup lang="ts">
import { Search, Package, Trash2, Download, ExternalLink } from 'lucide-vue-next'
import { useModsStore } from '~/stores/mods'
import { useServersStore } from '~/stores/servers'
import type { CFMod, CFModFile } from '~/types'

definePageMeta({
  title: 'Mods',
  middleware: 'auth',
})

const modsStore = useModsStore()
const serversStore = useServersStore()

// ── Server selector ───────────────────────────────────────────────────────────
const selectedServerId = ref('')

onMounted(async () => {
  if (serversStore.servers.length === 0) {
    await serversStore.fetchServers()
  }
  if (!selectedServerId.value && serversStore.servers.length > 0) {
    selectedServerId.value = serversStore.servers[0].id
  }
})

watch(selectedServerId, (id) => {
  if (id) modsStore.fetchInstalled(id)
}, { immediate: true })

// ── Tabs ──────────────────────────────────────────────────────────────────────
const activeTab = ref<'installed' | 'browse'>('installed')

// ── Browse / CurseForge search ────────────────────────────────────────────────
const searchQuery = ref('')
let searchTimer: ReturnType<typeof setTimeout>

watch(searchQuery, (q) => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    modsStore.search(q)
  }, 400)
})

onMounted(() => {
  modsStore.search('')
})

// ── Install ───────────────────────────────────────────────────────────────────
const toast = ref('')
const toastType = ref<'success' | 'error'>('success')
let toastTimer: ReturnType<typeof setTimeout>

function showToast(msg: string, type: 'success' | 'error' = 'success') {
  toast.value = msg
  toastType.value = type
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value = '' }, 3000)
}

function pickFile(mod: CFMod): CFModFile | null {
  if (!mod.latestFiles || mod.latestFiles.length === 0) return null
  // Use the last file (most recent)
  return mod.latestFiles[mod.latestFiles.length - 1]
}

async function installMod(mod: CFMod) {
  if (!selectedServerId.value) {
    showToast('Select a server first', 'error')
    return
  }
  const file = pickFile(mod)
  if (!file) {
    showToast('No downloadable file found for this mod', 'error')
    return
  }
  if (!file.downloadUrl) {
    showToast('CurseForge has restricted direct downloads for this mod', 'error')
    return
  }
  try {
    await modsStore.install(selectedServerId.value, {
      filename: file.fileName,
      display_name: `${mod.name} ${file.displayName}`,
      version: file.displayName,
      download_url: file.downloadUrl,
      cf_mod_id: mod.id,
      cf_file_id: file.id,
    })
    showToast(`${mod.name} queued for installation`)
  } catch {
    showToast(modsStore.error ?? 'Install failed', 'error')
  }
}

async function removeMod(filename: string) {
  if (!selectedServerId.value) return
  try {
    await modsStore.remove(selectedServerId.value, filename)
    showToast('Mod removed')
  } catch {
    showToast(modsStore.error ?? 'Remove failed', 'error')
  }
}

function formatDownloads(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(n)
}

const isInstalled = (filename: string) =>
  modsStore.installed.some(m => m.filename === filename)
</script>

<template>
  <div class="p-6 space-y-5">
    <!-- Header + server selector -->
    <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
      <h2 class="text-tp-text font-display font-bold text-2xl">Mods</h2>
      <div class="flex items-center gap-2">
        <label class="text-tp-outline text-sm shrink-0">Server:</label>
        <select
          v-model="selectedServerId"
          class="bg-tp-surface2 border border-tp-border text-tp-text rounded-xl px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-tp-primary/50 focus:border-tp-primary transition-colors min-w-48"
        >
          <option value="" disabled>Select a server</option>
          <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">
            {{ s.name }}
          </option>
        </select>
      </div>
    </div>

    <!-- No servers -->
    <div v-if="serversStore.servers.length === 0 && !serversStore.loading"
      class="bg-tp-surface rounded-xl p-12 text-center">
      <p class="text-tp-muted text-sm">Create a server first to manage its mods.</p>
    </div>

    <template v-else>
      <!-- Tabs -->
      <div class="border-b border-tp-border">
        <nav class="flex gap-1">
          <button
            v-for="tab in [{ id: 'installed', label: 'Installed' }, { id: 'browse', label: 'Browse CurseForge' }]"
            :key="tab.id"
            :class="[
              'flex items-center gap-2 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors',
              activeTab === tab.id
                ? 'border-tp-primary text-tp-primary'
                : 'border-transparent text-tp-muted hover:text-tp-text hover:border-tp-border',
            ]"
            @click="activeTab = tab.id as 'installed' | 'browse'"
          >
            {{ tab.label }}
            <span v-if="tab.id === 'installed' && modsStore.installed.length > 0"
              class="bg-tp-primary/15 text-tp-primary text-xs font-semibold px-1.5 py-0.5 rounded-full">
              {{ modsStore.installed.length }}
            </span>
          </button>
        </nav>
      </div>

      <!-- ── Installed tab ───────────────────────────────────────────────────── -->
      <div v-if="activeTab === 'installed'">
        <!-- Loading -->
        <div v-if="modsStore.loadingInstalled" class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-4">
          <div v-for="i in 3" :key="i" class="bg-tp-surface rounded-xl p-4 animate-pulse h-20" />
        </div>

        <!-- Empty -->
        <div v-else-if="modsStore.installed.length === 0"
          class="bg-tp-surface rounded-xl p-12 text-center">
          <Package class="w-10 h-10 text-tp-muted mx-auto mb-3" />
          <p class="text-tp-text font-display font-semibold mb-1">No mods installed</p>
          <p class="text-tp-muted text-sm">Browse CurseForge to install mods on this server.</p>
          <UiButton variant="primary" size="sm" class="mt-4" @click="activeTab = 'browse'">
            Browse mods
          </UiButton>
        </div>

        <!-- List -->
        <div v-else class="space-y-2">
          <div
            v-for="mod in modsStore.installed"
            :key="mod.id"
            class="bg-tp-surface rounded-xl px-4 py-3 flex items-center gap-4"
          >
            <Package class="w-5 h-5 text-tp-primary shrink-0" />
            <div class="flex-1 min-w-0">
              <p class="text-tp-text text-sm font-medium truncate">
                {{ mod.display_name || mod.filename }}
              </p>
              <p class="text-tp-muted text-xs font-mono mt-0.5">{{ mod.filename }}</p>
            </div>
            <span v-if="mod.version" class="text-tp-outline text-xs shrink-0">{{ mod.version }}</span>
            <UiButton
              variant="danger"
              size="sm"
              :loading="modsStore.removingFilename === mod.filename"
              :disabled="!!modsStore.removingFilename"
              @click="removeMod(mod.filename)"
            >
              <Trash2 class="w-3.5 h-3.5" />
            </UiButton>
          </div>
        </div>
      </div>

      <!-- ── Browse tab ─────────────────────────────────────────────────────── -->
      <div v-else class="space-y-4">
        <!-- Search box -->
        <div class="relative max-w-sm">
          <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-tp-muted pointer-events-none" />
          <input
            v-model="searchQuery"
            type="text"
            placeholder="Search Hytale mods..."
            class="w-full bg-tp-surface2 border border-tp-border text-tp-text rounded-xl pl-9 pr-4 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50 focus:border-tp-primary transition-colors"
          />
        </div>

        <!-- Loading skeleton -->
        <div v-if="modsStore.loadingSearch && modsStore.searchResults.length === 0"
          class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-4">
          <div v-for="i in 6" :key="i" class="bg-tp-surface rounded-xl p-4 animate-pulse h-40" />
        </div>

        <!-- No results -->
        <div v-else-if="!modsStore.loadingSearch && modsStore.searchResults.length === 0"
          class="bg-tp-surface rounded-xl p-12 text-center">
          <p class="text-tp-muted text-sm">No mods found. Try a different search.</p>
        </div>

        <!-- Results grid -->
        <div v-else class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-4">
          <div
            v-for="mod in modsStore.searchResults"
            :key="mod.id"
            class="bg-tp-surface rounded-xl p-4 flex flex-col gap-3"
          >
            <!-- Logo + name -->
            <div class="flex items-start gap-3">
              <img
                v-if="mod.logo?.thumbnailUrl"
                :src="mod.logo.thumbnailUrl"
                :alt="mod.name"
                class="w-12 h-12 rounded-xl object-cover shrink-0 bg-tp-surface2"
              />
              <div v-else class="w-12 h-12 rounded-xl bg-tp-surface2 flex items-center justify-center shrink-0">
                <Package class="w-6 h-6 text-tp-muted" />
              </div>
              <div class="min-w-0 flex-1">
                <p class="text-tp-text font-semibold text-sm leading-tight truncate">{{ mod.name }}</p>
                <p class="text-tp-outline text-xs mt-0.5">
                  {{ formatDownloads(mod.downloadCount) }} downloads
                </p>
              </div>
            </div>

            <!-- Summary -->
            <p class="text-tp-muted text-xs leading-relaxed line-clamp-2 flex-1">
              {{ mod.summary || 'No description available.' }}
            </p>

            <!-- Latest file info -->
            <p v-if="mod.latestFiles?.[mod.latestFiles.length - 1]" class="text-tp-outline text-xs font-mono">
              Latest: {{ mod.latestFiles[mod.latestFiles.length - 1].displayName }}
            </p>

            <!-- Actions -->
            <div class="flex items-center gap-2 mt-auto pt-1">
              <UiButton
                v-if="isInstalled(mod.latestFiles?.[mod.latestFiles.length - 1]?.fileName ?? '')"
                variant="secondary"
                size="sm"
                disabled
                class="flex-1"
              >
                Installed
              </UiButton>
              <UiButton
                v-else
                variant="primary"
                size="sm"
                class="flex-1"
                :loading="modsStore.installingId === mod.id"
                :disabled="!!modsStore.installingId || !selectedServerId"
                @click="installMod(mod)"
              >
                <Download class="w-3.5 h-3.5" />
                Install
              </UiButton>
              <a
                v-if="mod.links?.websiteUrl"
                :href="mod.links.websiteUrl"
                target="_blank"
                rel="noopener"
                class="p-2 rounded-lg text-tp-muted hover:text-tp-text hover:bg-tp-surface2 transition-colors"
                title="View on CurseForge"
              >
                <ExternalLink class="w-3.5 h-3.5" />
              </a>
            </div>
          </div>
        </div>

        <!-- Load more -->
        <div v-if="modsStore.searchResults.length < modsStore.searchTotal" class="text-center pt-2">
          <UiButton
            variant="secondary"
            size="sm"
            :loading="modsStore.loadingSearch"
            @click="modsStore.search(searchQuery, modsStore.searchPage + 1)"
          >
            Load more
          </UiButton>
        </div>
      </div>
    </template>

    <!-- Toast -->
    <Transition name="toast">
      <div
        v-if="toast"
        :class="[
          'fixed bottom-6 right-6 z-50 flex items-center gap-3 px-4 py-3 rounded-xl shadow-lg text-sm font-medium',
          toastType === 'success'
            ? 'bg-tp-success/15 text-tp-success'
            : 'bg-tp-error/10 text-tp-error',
        ]"
      >
        <div :class="['w-2 h-2 rounded-full', toastType === 'success' ? 'bg-tp-success' : 'bg-tp-danger']" />
        {{ toast }}
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.toast-enter-active, .toast-leave-active { transition: opacity 0.25s ease, transform 0.25s ease; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateY(8px); }
.line-clamp-2 { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
</style>
