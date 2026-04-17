<template>
  <div class="p-6 space-y-6">
    <div v-if="loading" class="bg-tp-surface rounded-xl p-8 text-center">
      <p class="text-tp-muted text-sm">Loading...</p>
    </div>

    <template v-else-if="player">
      <!-- Header -->
      <div class="flex items-start justify-between">
        <div class="flex items-center gap-4">
          <NuxtLink :to="`/servers/${serverId}`" class="p-2 rounded-lg text-tp-muted hover:text-tp-text hover:bg-tp-surface transition-colors">
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" /></svg>
          </NuxtLink>
          <div>
            <h1 class="text-tp-text font-display font-bold text-2xl">{{ player.username }}</h1>
            <p class="text-tp-muted text-xs font-mono mt-0.5">{{ player.hytale_uuid }}</p>
          </div>
        </div>
        <div class="flex gap-2 flex-wrap">
          <span v-if="player.is_banned"      class="px-2 py-1 rounded-lg text-xs font-medium bg-tp-error/10 text-tp-error">Banned</span>
          <span v-if="player.is_muted"       class="px-2 py-1 rounded-lg text-xs font-medium bg-orange-500/15 text-orange-400">Muted</span>
          <span v-if="player.is_op"          class="px-2 py-1 rounded-lg text-xs font-medium bg-yellow-500/15 text-yellow-400">Op</span>
          <span v-if="player.is_whitelisted" class="px-2 py-1 rounded-lg text-xs font-medium bg-tp-success/15 text-tp-success">Whitelisted</span>
        </div>
      </div>

      <!-- Stats -->
      <div class="grid grid-cols-2 sm:grid-cols-4 gap-4">
        <div class="bg-tp-surface rounded-xl p-4">
          <p class="text-tp-muted text-xs mb-1">Playtime</p>
          <p class="text-tp-text font-semibold text-sm">{{ formatDuration(player.playtime_s) }}</p>
        </div>
        <div class="bg-tp-surface rounded-xl p-4">
          <p class="text-tp-muted text-xs mb-1">First Seen</p>
          <p class="text-tp-text font-semibold text-sm">{{ formatDate(player.first_seen) }}</p>
        </div>
        <div class="bg-tp-surface rounded-xl p-4">
          <p class="text-tp-muted text-xs mb-1">Last Seen</p>
          <p class="text-tp-text font-semibold text-sm">{{ player.last_seen ? formatDate(player.last_seen) : 'Never' }}</p>
        </div>
        <div class="bg-tp-surface rounded-xl p-4">
          <p class="text-tp-muted text-xs mb-1">Sessions</p>
          <p class="text-tp-text font-semibold text-sm">{{ sessions.length }}</p>
        </div>
      </div>

      <!-- Actions -->
      <div class="bg-tp-surface rounded-xl p-4">
        <h2 class="text-tp-text font-semibold text-sm mb-3">Actions</h2>
        <div class="flex gap-2 flex-wrap">
          <button class="px-3 py-1.5 rounded-lg text-sm bg-red-500/20 text-red-400 hover:bg-red-500/30 transition-colors" @click="kick">
            Kick
          </button>
          <button class="px-3 py-1.5 rounded-lg text-sm transition-colors"
            :class="player.is_op ? 'bg-yellow-500/20 text-yellow-400 hover:bg-yellow-500/30' : 'bg-tp-surface2 text-tp-muted hover:text-tp-text hover:bg-tp-surface2'"
            @click="toggleOp">
            {{ player.is_op ? 'Deop' : 'Op' }}
          </button>
          <button class="px-3 py-1.5 rounded-lg text-sm transition-colors"
            :class="player.is_muted ? 'bg-orange-500/20 text-orange-400 hover:bg-orange-500/30' : 'bg-tp-surface2 text-tp-muted hover:text-tp-text hover:bg-tp-surface2'"
            @click="toggleMute">
            {{ player.is_muted ? 'Unmute' : 'Mute' }}
          </button>
          <button class="px-3 py-1.5 rounded-lg text-sm transition-colors"
            :class="player.is_banned ? 'bg-tp-success/15 text-tp-success hover:bg-tp-success/20' : 'bg-tp-error/10 text-tp-error hover:bg-tp-error/20'"
            @click="toggleBan">
            {{ player.is_banned ? 'Unban' : 'Ban' }}
          </button>
          <button class="px-3 py-1.5 rounded-lg text-sm transition-colors"
            :class="player.is_whitelisted ? 'bg-tp-warning/15 text-tp-warning hover:bg-tp-warning/20' : 'bg-tp-surface2 text-tp-muted hover:text-tp-text hover:bg-tp-surface2'"
            @click="toggleWhitelist">
            {{ player.is_whitelisted ? 'Remove Whitelist' : 'Whitelist' }}
          </button>
        </div>
      </div>

      <!-- Session History -->
      <div class="bg-tp-surface rounded-xl overflow-hidden">
        <div class="px-4 py-3 border-b border-tp-border">
          <h2 class="text-tp-text font-semibold text-sm">Session History</h2>
        </div>
        <p v-if="!sessions.length" class="px-4 py-8 text-tp-muted text-sm text-center">
          No sessions recorded.
        </p>
        <table v-else class="w-full text-sm">
          <thead>
            <tr class="border-b border-tp-border">
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Joined</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Left</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Duration</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in sessions" :key="s.joined_at" class="border-b border-tp-border/50 hover:bg-tp-surface2/50 transition-colors">
              <td class="px-4 py-3 text-tp-text text-xs">{{ formatDate(s.joined_at) }}</td>
              <td class="px-4 py-3 text-tp-muted text-xs">{{ s.left_at ? formatDate(s.left_at) : '—' }}</td>
              <td class="px-4 py-3 text-tp-muted text-xs">{{ s.duration_s != null ? formatDuration(s.duration_s) : '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <div v-else class="bg-tp-surface rounded-xl p-12 text-center">
      <p class="text-tp-text font-semibold mb-1">Player not found.</p>
      <p class="text-tp-muted text-sm">This player may not exist on this server.</p>
    </div>

    <!-- Toast -->
    <Transition name="toast">
      <div v-if="toast"
        :class="['fixed bottom-6 right-6 z-50 flex items-center gap-3 px-4 py-3 rounded-xl shadow-lg text-sm font-medium',
          toastType === 'success' ? 'bg-tp-success/15 text-tp-success' : 'bg-tp-error/10 text-tp-error']">
        <div :class="['w-2 h-2 rounded-full', toastType === 'success' ? 'bg-tp-success' : 'bg-tp-danger']" />
        {{ toast }}
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { usePlayersStore } from '~/stores/players'
import type { Player, PlayerSession } from '~/types'

definePageMeta({ title: 'Player Detail', middleware: 'auth' })

const route = useRoute()
const serverId = route.params.id as string
const playerId = route.params.playerId as string

const playersStore = usePlayersStore()

const player = ref<Player | null>(null)
const sessions = ref<PlayerSession[]>([])
const loading = ref(true)

// Toast
const toast = ref('')
const toastType = ref<'success' | 'error'>('success')
let toastTimer: ReturnType<typeof setTimeout>
function showToast(msg: string, type: 'success' | 'error' = 'success') {
  toast.value = msg
  toastType.value = type
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value = '' }, 3000)
}

async function loadData() {
  loading.value = true
  try {
    const [p, s] = await Promise.all([
      playersStore.fetchPlayer(serverId, playerId),
      playersStore.fetchPlayerSessions(serverId, playerId),
    ])
    player.value = p
    sessions.value = s
  } catch {
    player.value = null
  } finally {
    loading.value = false
  }
}

async function kick() {
  const reason = prompt('Kick reason (optional):') ?? ''
  try {
    await playersStore.kickPlayer(serverId, playerId, reason)
    showToast('Player kicked')
    await loadData()
  } catch { showToast('Failed to kick player', 'error') }
}

async function toggleOp() {
  if (!player.value) return
  try {
    await playersStore.setOp(serverId, playerId, !player.value.is_op)
    showToast(player.value.is_op ? 'Op removed' : 'Op granted')
    await loadData()
  } catch { showToast('Failed to update op status', 'error') }
}

async function toggleMute() {
  if (!player.value) return
  try {
    await playersStore.setMute(serverId, playerId, !player.value.is_muted)
    showToast(player.value.is_muted ? 'Player unmuted' : 'Player muted')
    await loadData()
  } catch { showToast('Failed to update mute status', 'error') }
}

async function toggleBan() {
  if (!player.value) return
  try {
    if (player.value.is_banned) {
      await playersStore.unbanPlayer(serverId, playerId)
      showToast('Player unbanned')
    } else {
      const reason = prompt('Ban reason (optional):') ?? ''
      await playersStore.banPlayer(serverId, playerId, reason)
      showToast('Player banned')
    }
    await loadData()
  } catch { showToast('Failed to update ban status', 'error') }
}

async function toggleWhitelist() {
  if (!player.value) return
  try {
    await playersStore.setWhitelist(serverId, playerId, !player.value.is_whitelisted)
    showToast(player.value.is_whitelisted ? 'Removed from whitelist' : 'Added to whitelist')
    await loadData()
  } catch { showToast('Failed to update whitelist', 'error') }
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString()
}

function formatDuration(s: number): string {
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  if (h > 0) return `${h}h ${m}m ${sec}s`
  if (m > 0) return `${m}m ${sec}s`
  return `${sec}s`
}

onMounted(loadData)
</script>

<style scoped>
.toast-enter-active, .toast-leave-active { transition: opacity 0.25s ease, transform 0.25s ease; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateY(8px); }
</style>
