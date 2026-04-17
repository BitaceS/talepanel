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

// ── Toggle mod (Task 5) ───────────────────────────────────────────────────────
async function toggleMod(serverId: string, filename: string, currentEnabled: boolean) {
  try {
    const api = useApi()
    await api.patch(`/servers/${serverId}/mods/${encodeURIComponent(filename)}/toggle`, {
      enabled: !currentEnabled,
    })
    await modsStore.fetchInstalled(serverId)
  } catch {
    showToast('Toggle failed', 'error')
  }
}

// ── Version switch (Task 6) ───────────────────────────────────────────────────
const versionSwitchMod = ref<InstanceType<typeof Object> | null>(null)
const versionFiles = ref<CFModFile[]>([])
const loadingVersions = ref(false)

async function openVersionSwitch(mod: typeof modsStore.installed[number]) {
  versionSwitchMod.value = mod
  loadingVersions.value = true
  try {
    const api = useApi()
    const data = await api.get<{ files: CFModFile[] }>(`/curseforge/mods/${mod.cf_mod_id}/files`)
    versionFiles.value = data.files ?? []
  } catch {
    versionFiles.value = []
    showToast('Failed to load versions', 'error')
  } finally {
    loadingVersions.value = false
  }
}

async function switchVersion(file: CFModFile) {
  if (!versionSwitchMod.value || !selectedServerId.value) return
  const mod = versionSwitchMod.value as typeof modsStore.installed[number]
  try {
    const api = useApi()
    await api.patch(`/servers/${selectedServerId.value}/mods/${encodeURIComponent(mod.filename)}`, {
      file_id: file.id,
      file_url: file.downloadUrl,
      display_name: file.displayName,
      version: Array.isArray(file.gameVersions) ? file.gameVersions[0] : '',
    })
    versionSwitchMod.value = null
    await modsStore.fetchInstalled(selectedServerId.value)
    showToast('Version updated')
  } catch {
    showToast('Version switch failed', 'error')
  }
}

// ── Custom JAR upload (Task 6) ────────────────────────────────────────────────
const uploadingMod = ref(false)

async function uploadMod(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  if (!file.name.endsWith('.jar')) {
    showToast('Only .jar files allowed', 'error')
    return
  }
  if (!selectedServerId.value) {
    showToast('Select a server first', 'error')
    return
  }
  uploadingMod.value = true
  try {
    const api = useApi()
    const form = new FormData()
    form.append('file', file)
    form.append('display_name', file.name.replace('.jar', ''))
    await api.post(`/servers/${selectedServerId.value}/mods/upload`, form)
    await modsStore.fetchInstalled(selectedServerId.value)
    showToast('Mod uploaded successfully')
  } catch {
    showToast('Upload failed', 'error')
  } finally {
    uploadingMod.value = false
    input.value = ''
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
          <!-- Toolbar: Upload JAR -->
          <div class="flex justify-end pb-1">
            <label
              class="cursor-pointer px-3 py-1.5 rounded bg-tp-surface2 hover:bg-tp-surface border border-tp-border text-tp-text text-sm transition-colors"
              :class="{ 'opacity-50 pointer-events-none': uploadingMod }"
            >
              {{ uploadingMod ? 'Uploading...' : 'Upload JAR' }}
              <input type="file" accept=".jar" class="hidden" @change="uploadMod" />
            </label>
          </div>

          <div
            v-for="mod in modsStore.installed"
            :key="mod.id"
            class="bg-tp-surface rounded-xl px-4 py-3 flex flex-col gap-1"
          >
            <!-- Main row -->
            <div class="flex items-center gap-4">
              <Package class="w-5 h-5 text-tp-primary shrink-0" />
              <div class="flex-1 min-w-0">
                <p class="text-tp-text text-sm font-medium truncate">
                  {{ mod.display_name || mod.filename }}
                </p>
                <p class="text-tp-muted text-xs font-mono mt-0.5">{{ mod.filename }}</p>
              </div>

              <!-- Action buttons -->
              <div class="flex items-center gap-1.5 shrink-0">
                <!-- Toggle button -->
                <button
                  @click="toggleMod(selectedServerId, mod.filename, mod.is_present)"
                  class="text-xs px-2 py-1 rounded transition-colors"
                  :class="mod.is_present
                    ? 'bg-green-500/20 text-green-400 hover:bg-green-500/30'
                    : 'bg-white/10 text-white/40 hover:bg-white/20'"
                >
                  {{ mod.is_present ? 'Enabled' : 'Disabled' }}
                </button>

                <!-- Change version (CurseForge only) -->
                <button
                  v-if="mod.source === 'curseforge' || mod.cf_mod_id"
                  @click="openVersionSwitch(mod)"
                  class="text-xs px-2 py-1 rounded bg-white/10 hover:bg-white/20 text-tp-muted hover:text-tp-text transition-colors"
                >
                  Change version
                </button>

                <!-- Delete -->
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

            <!-- Metadata row (Task 5) -->
            <div class="mt-1 text-xs text-white/50 flex flex-wrap gap-x-3 gap-y-0.5 ml-9">
              <span
                v-if="mod.source"
                class="px-1.5 py-0.5 rounded"
                :class="mod.source === 'curseforge' ? 'bg-orange-500/20 text-orange-300'
                      : mod.source === 'detected'   ? 'bg-blue-500/20 text-blue-300'
                      :                               'bg-white/10 text-white/50'"
              >
                {{ mod.source }}
              </span>
              <span v-if="mod.author">by {{ mod.author }}</span>
              <span v-if="mod.version">v{{ mod.version }}</span>
            </div>
            <p v-if="mod.description" class="mt-0.5 text-xs text-white/40 truncate ml-9">{{ mod.description }}</p>
            <div v-if="mod.detected_commands?.length" class="mt-1 flex flex-wrap gap-1 ml-9">
              <code
                v-for="cmd in mod.detected_commands"
                :key="cmd"
                class="px-1 py-0.5 bg-white/5 rounded text-xs text-white/50 font-mono"
              >{{ cmd }}</code>
            </div>
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

    <!-- ── Version switch modal (Task 6) ─────────────────────────────────────── -->
    <Teleport to="body">
      <div
        v-if="versionSwitchMod"
        class="fixed inset-0 bg-black/60 flex items-center justify-center z-50"
        @click.self="versionSwitchMod = null"
      >
        <div class="bg-[#0d1117] border border-white/10 rounded-xl p-6 w-full max-w-md shadow-2xl mx-4">
          <h3 class="text-lg font-semibold mb-4 text-tp-text">
            Change version — {{ (versionSwitchMod as typeof modsStore.installed[number]).display_name || (versionSwitchMod as typeof modsStore.installed[number]).filename }}
          </h3>
          <div v-if="loadingVersions" class="text-white/40 text-sm py-4 text-center">Loading versions...</div>
          <ul v-else-if="versionFiles.length" class="space-y-1 max-h-64 overflow-y-auto">
            <li
              v-for="f in versionFiles"
              :key="f.id"
              class="flex justify-between items-center px-3 py-2 rounded hover:bg-white/5 cursor-pointer transition-colors"
              @click="switchVersion(f)"
            >
              <span class="text-sm text-tp-text">{{ f.displayName }}</span>
              <span class="text-xs text-white/40">{{ Array.isArray(f.gameVersions) ? f.gameVersions[0] : '' }}</span>
            </li>
          </ul>
          <p v-else class="text-white/40 text-sm py-4 text-center">No versions available.</p>
          <button
            @click="versionSwitchMod = null"
            class="mt-4 text-sm text-white/40 hover:text-white transition-colors"
          >
            Cancel
          </button>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.toast-enter-active, .toast-leave-active { transition: opacity 0.25s ease, transform 0.25s ease; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateY(8px); }
.line-clamp-2 { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
</style>
