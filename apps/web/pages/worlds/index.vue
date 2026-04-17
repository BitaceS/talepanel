<script setup lang="ts">
import { Globe, Plus, Trash2, RefreshCw, CheckCircle, Star } from 'lucide-vue-next'
import { useWorldsStore } from '~/stores/worlds'
import { useServersStore } from '~/stores/servers'

definePageMeta({ title: 'Worlds', middleware: 'auth' })

const worldsStore = useWorldsStore()
const serversStore = useServersStore()

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

// Create modal
const showCreateModal = ref(false)
const createForm = reactive({ name: '', seed: '', generator: '' })
const creating = ref(false)

async function createWorld() {
  if (!selectedServer.value || !createForm.name) return
  creating.value = true
  try {
    await worldsStore.createWorld(selectedServer.value, {
      name: createForm.name,
      seed: createForm.seed ? Number(createForm.seed) : undefined,
      generator: createForm.generator || undefined,
    })
    showCreateModal.value = false
    Object.assign(createForm, { name: '', seed: '', generator: '' })
    showToast('World created')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to create world', 'error')
  } finally {
    creating.value = false
  }
}

async function activateWorld(worldId: string) {
  if (!selectedServer.value) return
  try {
    await worldsStore.setActive(selectedServer.value, worldId)
    showToast('Active world updated')
  } catch { showToast('Failed to set active world', 'error') }
}

async function deleteWorld(worldId: string) {
  if (!confirm('Delete this world? This cannot be undone.')) return
  try {
    await worldsStore.deleteWorld(selectedServer.value, worldId)
    showToast('World deleted')
  } catch { showToast('Failed to delete world', 'error') }
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
      <h2 class="text-tp-text font-display font-bold text-2xl">Worlds</h2>
      <div class="flex items-center gap-2">
        <select v-model="selectedServer" class="bg-tp-surface rounded-xl px-3 py-2 text-sm text-tp-text">
          <option value="">Select Server...</option>
          <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
        </select>
        <UiButton variant="primary" size="md" :disabled="!selectedServer" @click="showCreateModal = true">
          <Plus class="w-4 h-4" /> New World
        </UiButton>
      </div>
    </div>

    <!-- No server selected -->
    <div v-if="!selectedServer" class="bg-tp-surface rounded-xl p-12 text-center">
      <Globe class="w-10 h-10 text-tp-muted mx-auto mb-3" />
      <p class="text-tp-text font-display font-semibold mb-1">Select a server</p>
      <p class="text-tp-muted text-sm">Choose a server to manage its worlds.</p>
    </div>

    <!-- Loading -->
    <div v-else-if="worldsStore.loading" class="bg-tp-surface rounded-xl p-8 text-center">
      <RefreshCw class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
      <p class="text-tp-muted text-sm">Loading worlds...</p>
    </div>

    <!-- Empty -->
    <div v-else-if="worldsStore.worlds.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
      <Globe class="w-10 h-10 text-tp-muted mx-auto mb-3" />
      <p class="text-tp-text font-display font-semibold mb-1">No worlds</p>
      <p class="text-tp-muted text-sm mb-4">This server has no tracked worlds yet.</p>
      <UiButton variant="primary" size="md" @click="showCreateModal = true"><Plus class="w-4 h-4" /> Create World</UiButton>
    </div>

    <!-- World list -->
    <div v-else class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
      <div v-for="w in worldsStore.worlds" :key="w.id"
        :class="['bg-tp-surface rounded-xl p-5 space-y-3 transition-colors', w.is_active ? 'ring-1 ring-tp-success/40' : '']">
        <div class="flex items-start justify-between">
          <div class="flex items-center gap-2">
            <Globe :class="['w-5 h-5', w.is_active ? 'text-tp-success' : 'text-tp-muted']" />
            <p class="text-tp-text font-semibold">{{ w.name }}</p>
          </div>
          <span v-if="w.is_active" class="text-xs font-medium px-2 py-0.5 rounded-full bg-tp-success/15 text-tp-success">Active</span>
        </div>
        <div class="grid grid-cols-2 gap-2 text-xs">
          <div class="bg-tp-surface2 rounded-xl p-2"><span class="text-tp-outline">Size:</span> <span class="text-tp-text ml-1">{{ formatBytes(w.size_bytes) }}</span></div>
          <div class="bg-tp-surface2 rounded-xl p-2"><span class="text-tp-outline">Seed:</span> <span class="text-tp-text ml-1">{{ w.seed ?? '—' }}</span></div>
        </div>
        <div class="flex gap-2 pt-1 border-t border-tp-border">
          <button v-if="!w.is_active" class="flex-1 flex items-center justify-center gap-1.5 py-2 rounded-xl text-xs font-medium bg-tp-surface2 text-tp-muted hover:bg-tp-success/10 hover:text-tp-success transition-colors" @click="activateWorld(w.id)">
            <Star class="w-3.5 h-3.5" /> Set Active
          </button>
          <button v-else class="flex-1 flex items-center justify-center gap-1.5 py-2 rounded-xl text-xs font-medium bg-tp-success/10 text-tp-success">
            <CheckCircle class="w-3.5 h-3.5" /> Active
          </button>
          <button class="flex items-center justify-center gap-1.5 py-2 px-3 rounded-xl text-xs font-medium text-tp-danger hover:bg-tp-danger/10 transition-colors" @click="deleteWorld(w.id)">
            <Trash2 class="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </div>

    <!-- Create World Modal -->
    <UiModal :show="showCreateModal" title="Create World" @close="showCreateModal = false">
      <div class="space-y-4">
        <UiInput v-model="createForm.name" label="World Name" placeholder="my-world" />
        <UiInput v-model="createForm.seed" label="Seed (optional)" placeholder="12345" />
        <UiInput v-model="createForm.generator" label="Generator (optional)" placeholder="default" />
        <div class="flex justify-end gap-2 pt-2">
          <UiButton variant="secondary" size="md" @click="showCreateModal = false">Cancel</UiButton>
          <UiButton variant="primary" size="md" :loading="creating" :disabled="!createForm.name" @click="createWorld">Create</UiButton>
        </div>
      </div>
    </UiModal>

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
