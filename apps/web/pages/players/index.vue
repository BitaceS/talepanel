<script setup lang="ts">
import { Users, Search, Ban, Shield, ShieldCheck, ShieldOff, RefreshCw } from '@lucide/vue'
import { usePlayersStore } from '~/stores/players'
import { useServersStore } from '~/stores/servers'

definePageMeta({ title: 'pages.players', middleware: 'auth' })

const playersStore = usePlayersStore()
const serversStore = useServersStore()
const { t } = useI18n()

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

const filteredNetworkPlayers = computed(() => {
  const list = playersStore.networkPlayers
  if (!searchQuery.value) return list
  const q = searchQuery.value.toLowerCase()
  return list.filter(p => p.username.toLowerCase().includes(q))
})

// Ban modal — shared by the per-server ban and the network-wide ban. banTarget
// carries a player id for the former, a hytale_uuid for the latter.
const showBanModal = ref(false)
const banTarget = ref<{ id: string; username: string } | null>(null)
const banReason = ref('')
const banScope = ref<'server' | 'network'>('server')

function openBan(player: { id: string; username: string }) {
  banTarget.value = player
  banReason.value = ''
  banScope.value = 'server'
  showBanModal.value = true
}

function openNetworkBan(player: { hytale_uuid: string; username: string }) {
  banTarget.value = { id: player.hytale_uuid, username: player.username }
  banReason.value = ''
  banScope.value = 'network'
  showBanModal.value = true
}

async function confirmBan() {
  if (!banTarget.value) return

  if (banScope.value === 'network') {
    try {
      await playersStore.banNetworkPlayer(banTarget.value.id, banReason.value)
      showBanModal.value = false
      showToast(t('players.page.bannedNetwork', { name: banTarget.value.username }))
    } catch { showToast(t('players.page.banFailed'), 'error') }
    return
  }

  if (!selectedServer.value) return
  try {
    await playersStore.banPlayer(selectedServer.value, banTarget.value.id, banReason.value)
    showBanModal.value = false
    showToast(t('players.page.banned', { name: banTarget.value.username }))
  } catch { showToast(t('players.page.banFailed'), 'error') }
}

async function unbanNetwork(hytaleUuid: string) {
  try {
    await playersStore.unbanNetworkPlayer(hytaleUuid)
    showToast(t('players.page.networkBanLifted'))
  } catch { showToast(t('players.page.networkBanLiftFailed'), 'error') }
}

async function unban(playerId: string) {
  if (!selectedServer.value) return
  try {
    await playersStore.unbanPlayer(selectedServer.value, playerId)
    showToast(t('players.page.unbanned'))
  } catch { showToast(t('players.page.unbanFailed'), 'error') }
}

async function toggleWhitelist(playerId: string, whitelisted: boolean) {
  if (!selectedServer.value) return
  try {
    await playersStore.setWhitelist(selectedServer.value, playerId, whitelisted)
    showToast(whitelisted ? t('players.page.addedWhitelist') : t('players.page.removedWhitelist'))
  } catch { showToast(t('players.page.whitelistFailed'), 'error') }
}

async function kickPlayer(playerId: string, username: string) {
  if (!selectedServer.value) return
  const reason = prompt(t('players.page.kickPrompt', { name: username })) ?? ''
  try {
    await playersStore.kickPlayer(selectedServer.value, playerId, reason)
    showToast(t('players.page.kicked', { name: username }))
  } catch { showToast(t('players.page.kickFailed'), 'error') }
}

async function toggleOp(playerId: string, currentOp: boolean) {
  if (!selectedServer.value) return
  try {
    await playersStore.setOp(selectedServer.value, playerId, !currentOp)
    showToast(!currentOp ? t('players.page.opGranted') : t('players.page.opRemoved'))
  } catch { showToast(t('players.page.opFailed'), 'error') }
}

async function toggleMute(playerId: string, currentMuted: boolean) {
  if (!selectedServer.value) return
  try {
    await playersStore.setMute(selectedServer.value, playerId, !currentMuted)
    showToast(!currentMuted ? t('players.page.muted') : t('players.page.unmuted'))
  } catch { showToast(t('players.page.muteFailed'), 'error') }
}

watch(selectedServer, (sid) => {
  if (sid) playersStore.fetchPlayers(sid)
  else { playersStore.players = []; playersStore.fetchNetworkPlayers() }
})

onMounted(() => {
  serversStore.fetchServers()
  playersStore.fetchNetworkPlayers()
})

function formatPlaytime(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

function timeAgo(iso?: string | null): string {
  if (!iso) return t('common.never')
  const diff = Date.now() - new Date(iso).getTime()
  const s = Math.floor(diff / 1000)
  if (s < 60) return t('players.page.secondsAgo', { n: s })
  if (s < 3600) return t('time.minutesAgo', { n: Math.floor(s / 60) })
  if (s < 86400) return t('time.hoursAgo', { n: Math.floor(s / 3600) })
  return t('time.daysAgo', { n: Math.floor(s / 86400) })
}
</script>

<template>
  <div class="p-6 space-y-5">
    <div class="flex items-center justify-between">
      <h2 class="text-tp-text font-display font-bold text-2xl">{{ t('pages.players') }}</h2>
      <select v-model="selectedServer" class="bg-tp-surface rounded-xl px-3 py-2 text-sm text-tp-text">
        <option value="">{{ t('players.page.selectServer') }}</option>
        <option v-for="s in serversStore.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
      </select>
    </div>

    <!-- No server selected: the network view. One row per person, not per
         (person, server) — picking a server narrows this down, it is not a
         precondition for seeing anything. -->
    <template v-if="!selectedServer">
      <div class="flex items-center gap-3">
        <div class="relative flex-1 max-w-sm">
          <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-tp-muted" />
          <input v-model="searchQuery" type="text" :placeholder="t('players.page.searchNetwork')"
            class="w-full bg-tp-surface rounded-xl pl-9 pr-4 py-2 text-sm text-tp-text placeholder-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50" />
        </div>
        <UiButton variant="secondary" size="sm" :loading="playersStore.loading" @click="playersStore.fetchNetworkPlayers()">
          <RefreshCw class="w-3.5 h-3.5" /> {{ t('actions.refresh') }}
        </UiButton>
      </div>

      <div v-if="playersStore.loading" class="bg-tp-surface rounded-xl p-8 text-center">
        <RefreshCw class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
        <p class="text-tp-muted text-sm">{{ t('players.page.loading') }}</p>
      </div>

      <div v-else-if="filteredNetworkPlayers.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
        <Users class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-text font-display font-semibold mb-1">{{ t('players.page.emptyNetworkTitle') }}</p>
        <p class="text-tp-muted text-sm">{{ t('players.page.emptyNetworkText') }}</p>
      </div>

      <div v-else class="bg-tp-surface rounded-xl overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-tp-border">
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('players.page.colPlayer') }}</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('players.page.colPlaytimeNetwork') }}</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('players.page.colSeenOn') }}</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('players.page.colLastSeen') }}</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('players.page.colStatus') }}</th>
              <th class="text-right px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('players.page.colActions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in filteredNetworkPlayers" :key="p.hytale_uuid" class="border-b border-tp-border/50 hover:bg-tp-surface2/50 transition-colors">
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
              <td class="px-4 py-3 text-tp-muted text-xs">{{ t('players.page.serverCount', p.server_ids.length) }}</td>
              <td class="px-4 py-3 text-tp-muted text-xs">{{ timeAgo(p.last_seen) }}</td>
              <td class="px-4 py-3">
                <span v-if="p.is_banned" class="text-xs font-medium px-2 py-0.5 rounded-full text-tp-error bg-tp-error/10">{{ t('players.page.badgeBanned') }}</span>
                <span v-else class="text-tp-muted text-xs">—</span>
              </td>
              <td class="px-4 py-3">
                <div class="flex items-center justify-end gap-1">
                  <button v-if="!p.is_banned" :title="t('players.page.banEveryServer')"
                    class="text-xs px-2 py-1 rounded bg-tp-danger/20 text-tp-danger hover:bg-tp-danger/30 transition-colors"
                    @click="openNetworkBan(p)">
                    {{ t('players.page.banNetworkWide') }}
                  </button>
                  <button v-else :title="t('players.page.liftNetworkBan')"
                    class="text-xs px-2 py-1 rounded bg-tp-success/20 text-tp-success hover:bg-tp-success/30 transition-colors"
                    @click="unbanNetwork(p.hytale_uuid)">
                    {{ t('players.page.unban') }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <template v-else>
      <!-- Filters -->
      <div class="flex items-center gap-3">
        <div class="relative flex-1 max-w-sm">
          <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-tp-muted" />
          <input v-model="searchQuery" type="text" :placeholder="t('players.page.search')"
            class="w-full bg-tp-surface rounded-xl pl-9 pr-4 py-2 text-sm text-tp-text placeholder-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50" />
        </div>
        <div class="flex gap-1 bg-tp-surface rounded-xl p-1">
          <button v-for="f in [{ key: 'all', label: t('players.page.filterAll') }, { key: 'banned', label: t('players.page.badgeBanned') }, { key: 'whitelisted', label: t('players.page.badgeWhitelisted') }]" :key="f.key"
            :class="['px-3 py-1.5 rounded-lg text-xs font-medium transition-all', filterTab === f.key ? 'bg-tp-primary text-white' : 'text-tp-muted hover:text-tp-text']"
            @click="filterTab = f.key as 'all' | 'banned' | 'whitelisted'">
            {{ f.label }}
          </button>
        </div>
        <UiButton variant="secondary" size="sm" :loading="playersStore.loading" @click="playersStore.fetchPlayers(selectedServer)">
          <RefreshCw class="w-3.5 h-3.5" /> {{ t('actions.refresh') }}
        </UiButton>
      </div>

      <!-- Loading -->
      <div v-if="playersStore.loading" class="bg-tp-surface rounded-xl p-8 text-center">
        <RefreshCw class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
        <p class="text-tp-muted text-sm">{{ t('players.page.loading') }}</p>
      </div>

      <!-- Empty -->
      <div v-else-if="filteredPlayers.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
        <Users class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-text font-display font-semibold mb-1">{{ t('players.page.emptyTitle') }}</p>
        <p class="text-tp-muted text-sm">{{ t('players.page.emptyText') }}</p>
      </div>

      <!-- Player table -->
      <div v-else class="bg-tp-surface rounded-xl overflow-hidden">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-tp-border">
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('players.page.colPlayer') }}</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('players.page.colPlaytime') }}</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('players.page.colLastSeen') }}</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('players.page.colStatus') }}</th>
              <th class="text-right px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('players.page.colActions') }}</th>
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
                  <span v-if="p.is_banned" class="text-xs font-medium px-2 py-0.5 rounded-full text-tp-error bg-tp-error/10">{{ t('players.page.badgeBanned') }}</span>
                  <span v-if="p.is_whitelisted" class="text-xs font-medium px-2 py-0.5 rounded-full text-tp-success bg-tp-success/15">{{ t('players.page.badgeWhitelisted') }}</span>
                  <span v-if="p.is_op" class="text-xs font-medium px-2 py-0.5 rounded-full text-yellow-400 bg-yellow-500/15">{{ t('players.page.badgeOp') }}</span>
                  <span v-if="p.is_muted" class="text-xs font-medium px-2 py-0.5 rounded-full text-orange-400 bg-orange-500/15">{{ t('players.page.badgeMuted') }}</span>
                </div>
              </td>
              <td class="px-4 py-3">
                <div class="flex items-center justify-end gap-1">
                  <button v-if="!p.is_banned" :title="t('players.page.ban')" class="p-1.5 rounded-lg text-tp-danger hover:bg-tp-danger/10 transition-colors" @click="openBan(p)">
                    <Ban class="w-4 h-4" />
                  </button>
                  <button v-else :title="t('players.page.unban')" class="p-1.5 rounded-lg text-tp-success hover:bg-tp-success/10 transition-colors" @click="unban(p.id)">
                    <Shield class="w-4 h-4" />
                  </button>
                  <button :title="p.is_whitelisted ? t('players.page.removeWhitelist') : t('players.page.addWhitelist')"
                    class="p-1.5 rounded-lg transition-colors"
                    :class="p.is_whitelisted ? 'text-tp-warning hover:bg-tp-warning/10' : 'text-tp-muted hover:bg-tp-surface2'"
                    @click="toggleWhitelist(p.id, !p.is_whitelisted)">
                    <ShieldCheck v-if="p.is_whitelisted" class="w-4 h-4" />
                    <ShieldOff v-else class="w-4 h-4" />
                  </button>
                  <button :title="t('players.page.kick')"
                    class="text-xs px-2 py-1 rounded bg-red-500/20 text-red-400 hover:bg-red-500/30 transition-colors"
                    @click="kickPlayer(p.id, p.username)">
                    {{ t('players.page.kick') }}
                  </button>
                  <button :title="p.is_op ? t('players.page.removeOp') : t('players.page.grantOp')"
                    class="text-xs px-2 py-1 rounded transition-colors"
                    :class="p.is_op ? 'bg-yellow-500/20 text-yellow-400 hover:bg-yellow-500/30' : 'bg-white/10 text-white/40 hover:bg-white/20'"
                    @click="toggleOp(p.id, p.is_op)">
                    {{ p.is_op ? t('players.page.badgeOp') : t('players.page.noOp') }}
                  </button>
                  <button :title="p.is_muted ? t('players.page.unmute') : t('players.page.mute')"
                    class="text-xs px-2 py-1 rounded transition-colors"
                    :class="p.is_muted ? 'bg-orange-500/20 text-orange-400 hover:bg-orange-500/30' : 'bg-white/10 text-white/40 hover:bg-white/20'"
                    @click="toggleMute(p.id, p.is_muted)">
                    {{ p.is_muted ? t('players.page.badgeMuted') : t('players.page.mute') }}
                  </button>
                  <NuxtLink :to="`/servers/${selectedServer}/players/${p.id}`"
                    class="text-xs px-2 py-1 rounded bg-white/10 text-tp-muted hover:bg-white/20 hover:text-tp-text transition-colors">
                    {{ t('players.page.view') }}
                  </NuxtLink>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <!-- Ban Modal -->
    <UiModal :open="showBanModal" :title="banScope === 'network' ? t('players.page.banNetworkTitle') : t('players.page.banPlayer')" @close="showBanModal = false">
      <div class="space-y-4">
        <i18n-t v-if="banScope === 'network'" keypath="players.page.banNetworkText" tag="p" class="text-tp-text text-sm" scope="global">
          <template #name><span class="font-semibold">{{ banTarget?.username }}</span></template>
          <template #everyServer><span class="font-semibold">{{ t('players.page.everyServer') }}</span></template>
        </i18n-t>
        <i18n-t v-else keypath="players.page.banServerText" tag="p" class="text-tp-text text-sm" scope="global">
          <template #name><span class="font-semibold">{{ banTarget?.username }}</span></template>
        </i18n-t>
        <UiInput v-model="banReason" :label="t('players.page.reasonLabel')" :placeholder="t('players.page.reasonPlaceholder')" />
        <div class="flex justify-end gap-2 pt-2">
          <UiButton variant="secondary" size="md" @click="showBanModal = false">{{ t('actions.cancel') }}</UiButton>
          <UiButton variant="danger" size="md" @click="confirmBan">{{ t('players.page.banPlayer') }}</UiButton>
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
