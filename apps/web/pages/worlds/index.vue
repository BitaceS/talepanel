<script setup lang="ts">
import { Globe, Trash2, RefreshCw, CheckCircle, Star, Info } from '@lucide/vue'
import { useWorldsStore } from '~/stores/worlds'
import { useServersStore } from '~/stores/servers'

definePageMeta({ title: 'pages.worlds', middleware: 'auth' })

const worldsStore = useWorldsStore()
const serversStore = useServersStore()
const { t } = useI18n()

const selectedServer = ref('')

// Toast
const toast = ref('')
const toastType = ref<'success' | 'error'>('success')
let toastTimer: ReturnType<typeof setTimeout>
function showToast(msg: string, type: 'success' | 'error' = 'success') {
  toast.value = msg; toastType.value = type
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value = '' }, 3000)
}

// There is no "create world" here on purpose: TalePanel cannot generate a
// Hytale world, only the Hytale server can. Worlds appear once the daemon finds
// them under universe/worlds on the node.

const activatingId = ref<string | null>(null)

async function activateWorld(worldId: string) {
  if (!selectedServer.value) return
  activatingId.value = worldId
  try {
    const res = await worldsStore.setActive(selectedServer.value, worldId)
    // The daemon writes config.json; Hytale reads it at boot.
    showToast(res?.message ?? t('misc.worlds.activated'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? t('misc.worlds.activateFailed'), 'error')
  } finally {
    activatingId.value = null
  }
}

async function deleteWorld(world: { id: string; name: string }) {
  if (!confirm(t('misc.worlds.deleteConfirm', { name: world.name }))) return
  try {
    await worldsStore.deleteWorld(selectedServer.value, world.id)
    showToast(t('misc.worlds.deleted'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? t('misc.worlds.deleteFailed'), 'error')
  }
}

watch(selectedServer, (sid) => {
  if (sid) worldsStore.fetchWorlds(sid)
  else worldsStore.worlds = []
})

onMounted(() => { serversStore.fetchServers() })

function formatBytes(bytes: number | null): string {
  if (!bytes) return '—'
  if (bytes >= 1073741824) return `${(bytes / 1073741824).toFixed(1)} GB`
  if (bytes >= 1048576) return `${(bytes / 1048576).toFixed(1)} MB`
  return `${(bytes / 1024).toFixed(1)} KB`
}
</script>

<template>
  <div class="p-6 space-y-5">
    <div class="flex items-center justify-between">
      <h2 class="text-tp-text font-display font-bold text-2xl">{{ t('pages.worlds') }}</h2>
      <div class="flex items-center gap-2">
        <select v-model="selectedServer" class="bg-tp-surface rounded-xl px-3 py-2 text-sm text-tp-text">
          <option value="">{{ t('misc.worlds.selectServer') }}</option>
          <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
        </select>
      </div>
    </div>

    <!-- No server selected -->
    <div v-if="!selectedServer" class="bg-tp-surface rounded-xl p-12 text-center">
      <Globe class="w-10 h-10 text-tp-muted mx-auto mb-3" />
      <p class="text-tp-text font-display font-semibold mb-1">{{ t('misc.worlds.selectServerTitle') }}</p>
      <p class="text-tp-muted text-sm">{{ t('misc.worlds.selectServerText') }}</p>
    </div>

    <!-- Loading -->
    <div v-else-if="worldsStore.loading" class="bg-tp-surface rounded-xl p-8 text-center">
      <RefreshCw class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
      <p class="text-tp-muted text-sm">{{ t('misc.worlds.loading') }}</p>
    </div>

    <!-- Empty -->
    <div v-else-if="worldsStore.worlds.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
      <Globe class="w-10 h-10 text-tp-muted mx-auto mb-3" />
      <p class="text-tp-text font-display font-semibold mb-1">{{ t('misc.worlds.emptyTitle') }}</p>
      <i18n-t keypath="misc.worlds.emptyText" tag="p" class="text-tp-muted text-sm max-w-md mx-auto" scope="global">
        <template #path><code class="text-tp-text font-mono">universe/worlds</code></template>
      </i18n-t>
    </div>

    <!-- World list -->
    <template v-else>
      <p class="text-tp-muted text-xs flex items-center gap-1.5">
        <Info class="w-3.5 h-3.5 shrink-0" />
        <i18n-t keypath="misc.worlds.switchHint" tag="span" scope="global">
          <template #file><code class="font-mono">config.json</code></template>
        </i18n-t>
      </p>

      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <div v-for="w in worldsStore.worlds" :key="w.id"
          :class="['bg-tp-surface rounded-xl p-5 space-y-3 transition-colors', w.is_active ? 'ring-1 ring-tp-success/40' : '']">
          <div class="flex items-start justify-between">
            <div class="flex items-center gap-2">
              <Globe :class="['w-5 h-5', w.is_active ? 'text-tp-success' : 'text-tp-muted']" />
              <p class="text-tp-text font-semibold">{{ w.name }}</p>
            </div>
            <span v-if="w.is_active" class="text-xs font-medium px-2 py-0.5 rounded-full bg-tp-success/15 text-tp-success">{{ t('misc.worlds.active') }}</span>
          </div>
          <div class="grid grid-cols-2 gap-2 text-xs">
            <div class="bg-tp-surface2 rounded-xl p-2"><span class="text-tp-outline">{{ t('misc.worlds.size') }}</span> <span class="text-tp-text ml-1">{{ formatBytes(w.size_bytes) }}</span></div>
            <div class="bg-tp-surface2 rounded-xl p-2"><span class="text-tp-outline">{{ t('misc.worlds.seed') }}</span> <span class="text-tp-text ml-1">{{ w.seed ?? '—' }}</span></div>
          </div>
          <div class="flex gap-2 pt-1 border-t border-tp-border">
            <button v-if="!w.is_active"
              class="flex-1 flex items-center justify-center gap-1.5 py-2 rounded-xl text-xs font-medium bg-tp-surface2 text-tp-muted hover:bg-tp-success/10 hover:text-tp-success transition-colors disabled:opacity-50"
              :disabled="activatingId === w.id"
              :title="t('misc.worlds.setActiveHint')"
              @click="activateWorld(w.id)">
              <Star class="w-3.5 h-3.5" /> {{ activatingId === w.id ? t('misc.worlds.activating') : t('misc.worlds.setActive') }}
            </button>
            <button v-else class="flex-1 flex items-center justify-center gap-1.5 py-2 rounded-xl text-xs font-medium bg-tp-success/10 text-tp-success">
              <CheckCircle class="w-3.5 h-3.5" /> {{ t('misc.worlds.active') }}
            </button>
            <!-- The active world cannot be deleted: the server would boot into a
                 world that no longer exists. The API rejects it too (409). -->
            <button
              class="flex items-center justify-center gap-1.5 py-2 px-3 rounded-xl text-xs font-medium text-tp-danger hover:bg-tp-danger/10 transition-colors disabled:opacity-40 disabled:hover:bg-transparent"
              :disabled="w.is_active"
              :title="w.is_active ? t('misc.worlds.activateOtherFirst') : t('misc.worlds.deleteFromDisk')"
              @click="deleteWorld(w)">
              <Trash2 class="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </div>
    </template>

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
