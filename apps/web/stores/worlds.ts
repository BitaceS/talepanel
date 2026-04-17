import { defineStore } from 'pinia'
import { useApi } from '~/composables/useApi'
import type { World } from '~/types'

export const useWorldsStore = defineStore('worlds', () => {
  const api = useApi()
  const worlds = ref<World[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchWorlds(serverId: string) {
    loading.value = true
    error.value = null
    try {
      const data = await api.get<{ worlds: World[] }>(`/servers/${serverId}/worlds`)
      worlds.value = data.worlds ?? []
    } catch (err: unknown) {
      const e = err as { data?: { error?: string }; message?: string }
      error.value = e.data?.error ?? e.message ?? 'Failed to fetch worlds'
    } finally {
      loading.value = false
    }
  }

  async function createWorld(serverId: string, payload: { name: string; seed?: number; generator?: string }) {
    const data = await api.post<{ world: World }>(`/servers/${serverId}/worlds`, payload)
    worlds.value.unshift(data.world)
    return data.world
  }

  async function setActive(serverId: string, worldId: string) {
    await api.post(`/servers/${serverId}/worlds/${worldId}/activate`)
    worlds.value.forEach(w => { w.is_active = w.id === worldId })
  }

  async function deleteWorld(serverId: string, worldId: string) {
    await api.delete(`/servers/${serverId}/worlds/${worldId}`)
    worlds.value = worlds.value.filter(w => w.id !== worldId)
  }

  return { worlds, loading, error, fetchWorlds, createWorld, setActive, deleteWorld }
})
