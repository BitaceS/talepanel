import { defineStore } from 'pinia'
import { useApi } from '~/composables/useApi'
import type { NetworkPlayer, Player, PlayerSession } from '~/types'

export const usePlayersStore = defineStore('players', () => {
  const api = useApi()
  const players = ref<Player[]>([])
  const networkPlayers = ref<NetworkPlayer[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  // One row per human instead of one row per (player, server): a player is
  // stored per server, and hytale_uuid is what makes them one person again.
  async function fetchNetworkPlayers() {
    loading.value = true
    error.value = null
    try {
      const data = await api.get<{ players: NetworkPlayer[] }>('/network/players')
      networkPlayers.value = data.players ?? []
    } catch (err: unknown) {
      const e = err as { data?: { error?: string }; message?: string }
      error.value = e.data?.error ?? e.message ?? 'Failed to fetch network players'
    } finally {
      loading.value = false
    }
  }

  async function banNetworkPlayer(hytaleUuid: string, reason: string) {
    await api.post(`/network/players/${hytaleUuid}/ban`, { reason })
    const p = networkPlayers.value.find(p => p.hytale_uuid === hytaleUuid)
    if (p) { p.is_banned = true; p.ban_reason = reason }
  }

  async function unbanNetworkPlayer(hytaleUuid: string) {
    await api.post(`/network/players/${hytaleUuid}/unban`)
    const p = networkPlayers.value.find(p => p.hytale_uuid === hytaleUuid)
    if (p) { p.is_banned = false; p.ban_reason = null }
  }

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

  async function kickPlayer(serverId: string, playerId: string, reason: string) {
    await api.post(`/servers/${serverId}/players/${playerId}/kick`, { reason })
  }

  async function setOp(serverId: string, playerId: string, op: boolean) {
    await api.patch(`/servers/${serverId}/players/${playerId}/op`, { op })
    const p = players.value.find(p => p.id === playerId)
    if (p) p.is_op = op
  }

  async function setMute(serverId: string, playerId: string, muted: boolean) {
    await api.patch(`/servers/${serverId}/players/${playerId}/mute`, { muted })
    const p = players.value.find(p => p.id === playerId)
    if (p) p.is_muted = muted
  }

  async function fetchPlayer(serverId: string, playerId: string): Promise<Player> {
    return await api.get<Player>(`/servers/${serverId}/players/${playerId}`)
  }

  async function fetchPlayerSessions(serverId: string, playerId: string): Promise<PlayerSession[]> {
    const data = await api.get<{ sessions: PlayerSession[] }>(`/servers/${serverId}/players/${playerId}/sessions`)
    return data.sessions ?? []
  }

  return {
    players, networkPlayers, loading, error,
    fetchPlayers, banPlayer, unbanPlayer, setWhitelist, kickPlayer, setOp, setMute,
    fetchPlayer, fetchPlayerSessions,
    fetchNetworkPlayers, banNetworkPlayer, unbanNetworkPlayer,
  }
})
