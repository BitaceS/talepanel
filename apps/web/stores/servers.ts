import { defineStore } from 'pinia'
import type { Server } from '~/types'

interface CreateServerPayload {
  name: string
  hytale_version?: string
  ram_limit_mb?: number | null
  cpu_limit?: number | null
  auto_restart?: boolean
}

export const useServersStore = defineStore('servers', {
  state: () => ({
    servers: [] as Server[],
    currentServer: null as Server | null,
    loading: false,
    error: null as string | null,
  }),

  getters: {
    getServerById: (state) => (id: string): Server | undefined =>
      state.servers.find((s) => s.id === id),
  },

  actions: {
    async fetchServers(): Promise<void> {
      this.loading = true
      this.error = null
      this.servers = []
      try {
        const api = useApi()
        const data = await api.get<{ servers: Server[] }>('/servers')
        this.servers = data.servers
      } catch (err: unknown) {
        const e = err as { data?: { error?: string }; message?: string }
        this.error = e.data?.error ?? e.message ?? 'Failed to fetch servers'
      } finally {
        this.loading = false
      }
    },

    async fetchServer(id: string): Promise<void> {
      this.loading = true
      this.error = null
      try {
        const api = useApi()
        const data = await api.get<{ server: Server }>(`/servers/${id}`)
        this.currentServer = data.server
        // Update in list if present
        const idx = this.servers.findIndex((s) => s.id === id)
        if (idx !== -1) {
          this.servers[idx] = data.server
        }
      } catch (err: unknown) {
        const e = err as { data?: { error?: string }; message?: string }
        this.error = e.data?.error ?? e.message ?? 'Failed to fetch server'
      } finally {
        this.loading = false
      }
    },

    async createServer(payload: CreateServerPayload): Promise<Server> {
      const api = useApi()
      const data = await api.post<{ server: Server }>('/servers', payload)
      this.servers.unshift(data.server)
      return data.server
    },

    async startServer(id: string): Promise<void> {
      const api = useApi()
      await api.post(`/servers/${id}/start`)
      await this._refreshServer(id)
    },

    async stopServer(id: string): Promise<void> {
      const api = useApi()
      await api.post(`/servers/${id}/stop`)
      await this._refreshServer(id)
    },

    async restartServer(id: string): Promise<void> {
      const api = useApi()
      await api.post(`/servers/${id}/restart`)
      await this._refreshServer(id)
    },

    async killServer(id: string): Promise<void> {
      const api = useApi()
      await api.post(`/servers/${id}/kill`)
      await this._refreshServer(id)
    },

    async deleteServer(id: string): Promise<void> {
      const api = useApi()
      await api.delete(`/servers/${id}`)
      this.servers = this.servers.filter((s) => s.id !== id)
      if (this.currentServer?.id === id) {
        this.currentServer = null
      }
    },

    async _refreshServer(id: string): Promise<void> {
      try {
        const api = useApi()
        const data = await api.get<{ server: Server }>(`/servers/${id}`)
        const idx = this.servers.findIndex((s) => s.id === id)
        if (idx !== -1) {
          this.servers[idx] = data.server
        }
        if (this.currentServer?.id === id) {
          this.currentServer = data.server
        }
      } catch {
        // Best-effort refresh
      }
    },
  },
})
