import { defineStore } from 'pinia'
import { useApi } from '~/composables/useApi'
import type { Player } from '~/types'

export const usePlayersStore = defineStore('players', () => {
  const api = useApi()
  const players = ref<Player[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchPlayers(serverId: string) {
    loading.value = true
    error.value = null
    try {
      const data = await api.get<{ players: Player[] }>(`/servers/${serverId}/players`)
      players.value = data.players ?? []
    } catch (err: unknown) {
      const e = err as { data?: { error?: string }; message?: string }
      error.value = e.data?.error ?? e.message ?? 'Failed to fetch players'
    } finally {
      loading.value = false
    }
  }

  async function banPlayer(serverId: string, playerId: string, reason: string) {
    await api.post(`/servers/${serverId}/players/${playerId}/ban`, { reason })
    const p = players.value.find(p => p.id === playerId)
    if (p) { p.is_banned = true; p.ban_reason = reason }
  }

  async function unbanPlayer(serverId: string, playerId: string) {
    await api.post(`/servers/${serverId}/players/${playerId}/unban`)
    const p = players.value.find(p => p.id === playerId)
    if (p) { p.is_banned = false; p.ban_reason = null }
  }

  async function setWhitelist(serverId: string, playerId: string, whitelisted: boolean) {
    await api.patch(`/servers/${serverId}/players/${playerId}/whitelist`, { whitelisted })
    const p = players.value.find(p => p.id === playerId)
    if (p) p.is_whitelisted = whitelisted
  }

  return { players, loading, error, fetchPlayers, banPlayer, unbanPlayer, setWhitelist }
})
