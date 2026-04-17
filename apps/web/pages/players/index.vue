<script setup lang="ts">
import { Users, Search, Ban, Shield, ShieldCheck, ShieldOff, RefreshCw } from 'lucide-vue-next'
import { usePlayersStore } from '~/stores/players'
import { useServersStore } from '~/stores/servers'

definePageMeta({ title: 'Players', middleware: 'auth' })

const playersStore = usePlayersStore()
const serversStore = useServersStore()

const selectedServer = ref('')
const searchQuery = ref('')
const filterTab = ref<'all' | 'online' | 'banned' | 'whitelisted'>('all')

// Toast
const toast = ref('')
const toastType = ref<'success' | 'error'>('success')
let toastTimer: ReturnType<typeof setTimeout>
function showToast(msg: string, type: 'success' | 'error' = 'success') {
  toast.value = msg; toastType.value = type
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value = '' }, 3000)
}

const filteredPlayers = computed(() => {
  let list = playersStore.players
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    list = list.filter(p => p.username.toLowerCase().includes(q))
  }
  switch (filterTab.value) {
    case 'banned': return list.filter(p => p.is_banned)
    case 'whitelisted': return list.filter(p => p.is_whitelisted)
    default: return list
  }
})

// Ban modal
const showBanModal = ref(false)
const banTarget = ref<{ id: string; username: string } | null>(null)
const banReason = ref('')

function openBan(player: { id: string; username: string }) {
  banTarget.value = player
  banReason.value = ''
  showBanModal.value = true
}

async function confirmBan() {
  if (!banTarget.value || !selectedServer.value) return
  try {
    await playersStore.banPlayer(selectedServer.value, banTarget.value.id, banReason.value)
    showBanModal.value = false
    showToast(`${banTarget.value.username} banned`)
  } catch { showToast('Failed to ban player', 'error') }
}

async function unban(playerId: string) {
  if (!selectedServer.value) return
  try {
    await playersStore.unbanPlayer(selectedServer.value, playerId)
    showToast('Player unbanned')
  } catch { showToast('Failed to unban', 'error') }
}

async function toggleWhitelist(playerId: string, whitelisted: boolean) {
  if (!selectedServer.value) return
  try {
    await playersStore.setWhitelist(selectedServer.value, playerId, whitelisted)
    showToast(whitelisted ? 'Added to whitelist' : 'Removed from whitelist')
  } catch { showToast('Failed to update whitelist', 'error') }
}

watch(selectedServer, (sid) => {
  if (sid) playersStore.fetchPlayers(sid)
  else playersStore.players = []
})

onMounted(() => { serversStore.fetchServers() })

function formatPlaytime(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

function timeAgo(iso?: string | null): string {
  if (!iso) return 'Never'
  const diff = Date.now() - new Date(iso).getTime()
  const s = Math.floor(diff / 1000)
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}
</script>

<template>
  <div class="p-6 space-y-5">
    <div class="flex items-center justify-between">
      <h2 class="text-tp-text font-display font-bold text-2xl">Players</h2>
      <select v-model="selectedServer" class="bg-tp-surface rounded-xl px-3 py-2 text-sm text-tp-text">
        <option value="">Select Server...</option>
        <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
      </select>
    </div>

    <!-- No server -->
    <div v-if="!selectedServer" class="bg-tp-surface rounded-xl p-12 text-center">
      <Users class="w-10 h-10 text-tp-muted mx-auto mb-3" />
      <p class="text-tp-text font-display font-semibold mb-1">Select a server</p>
      <p class="text-tp-muted text-sm">Choose a server to view and manage its players.</p>
    </div>

    <template v-else>
      <!-- Filters -->
      <div class="flex items-center gap-3">
        <div class="relative flex-1 max-w-sm">
          <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-tp-muted" />
          <input v-model="searchQuery" type="text" placeholder="Search players..."
            class="w-full bg-tp-surface rounded-xl pl-9 pr-4 py-2 text-sm text-tp-text placeholder-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50" />
        </div>
        <div class="flex gap-1 bg-tp-surface rounded-xl p-1">
          <button v-for="f in [{ key: 'all', label: 'All' }, { key: 'banned', label: 'Banned' }, { key: 'whitelisted', label: 'Whitelisted' }]" :key="f.key"
            :class="['px-3 py-1.5 rounded-lg text-xs font-medium transition-all', filterTab === f.key ? 'bg-tp-primary text-white' : 'text-tp-muted hover:text-tp-text']"
            @click="filterTab = f.key as 'all' | 'banned' | 'whitelisted'">
            {{ f.label }}
          </button>
        </div>
        <UiButton variant="secondary" size="sm" :loading="playersStore.loading" @click="playersStore.fetchPlayers(selectedServer)">
          <RefreshCw class="w-3.5 h-3.5" /> Refresh
        </UiButton>
      </div>

      <!-- Loading -->
      <div v-if="playersStore.loading" class="bg-tp-surface rounded-xl p-8 text-center">
        <RefreshCw class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
        <p class="text-tp-muted text-sm">Loading players...</p>
      </div>

      <!-- Empty -->
      <div v-else-if="filteredPlayers.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
        <Users class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-text font-display font-semibold mb-1">No players found</p>
        <p class="text-tp-muted text-sm">Players will appear here when they join the server.</p>
      </div>

      <!-- Player table -->
      <div v-else class="bg-tp-surface rounded-xl overflow-hidden">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-tp-border">
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Player</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Playtime</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Last Seen</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Status</th>
              <th class="text-right px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in filteredPlayers" :key="p.id" class="border-b border-tp-border/50 hover:bg-tp-surface2/50 transition-colors">
              <td class="px-4 py-3">
                <div class="flex items-center gap-3">
                  <div class="w-8 h-8 rounded-full bg-tp-primary/20 flex items-center justify-center text-tp-primary text-xs font-semibold uppercase">{{ p.username.charAt(0) }}</div>
                  <div>
                    <p class="text-tp-text font-medium">{{ p.username }}</p>
                    <p class="text-tp-muted text-xs font-mono">{{ p.hytale_uuid.substring(0, 8) }}...</p>
                  </div>
                </div>
              </td>
              <td class="px-4 py-3 text-tp-text text-xs">{{ formatPlaytime(p.playtime_s) }}</td>
              <td class="px-4 py-3 text-tp-muted text-xs">{{ timeAgo(p.last_seen) }}</td>
              <td class="px-4 py-3">
                <div class="flex items-center gap-1.5">
                  <span v-if="p.is_banned" class="text-xs font-medium px-2 py-0.5 rounded-full text-tp-error bg-tp-error/10">Banned</span>
                  <span v-if="p.is_whitelisted" class="text-xs font-medium px-2 py-0.5 rounded-full text-tp-success bg-tp-success/15">Whitelisted</span>
                </div>
              </td>
              <td class="px-4 py-3">
                <div class="flex items-center justify-end gap-1">
                  <button v-if="!p.is_banned" title="Ban" class="p-1.5 rounded-lg text-tp-danger hover:bg-tp-danger/10 transition-colors" @click="openBan(p)">
                    <Ban class="w-4 h-4" />
                  </button>
                  <button v-else title="Unban" class="p-1.5 rounded-lg text-tp-success hover:bg-tp-success/10 transition-colors" @click="unban(p.id)">
                    <Shield class="w-4 h-4" />
                  </button>
                  <button :title="p.is_whitelisted ? 'Remove from whitelist' : 'Add to whitelist'"
                    class="p-1.5 rounded-lg transition-colors"
                    :class="p.is_whitelisted ? 'text-tp-warning hover:bg-tp-warning/10' : 'text-tp-muted hover:bg-tp-surface2'"
                    @click="toggleWhitelist(p.id, !p.is_whitelisted)">
                    <ShieldCheck v-if="p.is_whitelisted" class="w-4 h-4" />
                    <ShieldOff v-else class="w-4 h-4" />
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <!-- Ban Modal -->
    <UiModal :show="showBanModal" title="Ban Player" @close="showBanModal = false">
      <div class="space-y-4">
        <p class="text-tp-text text-sm">Ban <span class="font-semibold">{{ banTarget?.username }}</span> from this server?</p>
        <UiInput v-model="banReason" label="Reason (optional)" placeholder="Rule violation..." />
        <div class="flex justify-end gap-2 pt-2">
          <UiButton variant="secondary" size="md" @click="showBanModal = false">Cancel</UiButton>
          <UiButton variant="danger" size="md" @click="confirmBan">Ban Player</UiButton>
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
