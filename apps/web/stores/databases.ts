import { defineStore } from 'pinia'
import type { ServerDatabase } from '~/types'

export const useDatabasesStore = defineStore('databases', {
  state: () => ({
    database: null as ServerDatabase | null,
    loading: false,
  }),

  actions: {
    async fetchDatabase(serverId: string) {
      const api = useApi()
      this.loading = true
      try {
        const data = await api.get<{ database: ServerDatabase }>(
          `/servers/${serverId}/database`
        )
        this.database = data.database
      } catch (err: unknown) {
        const fetchErr = err as { status?: number }
        if (fetchErr.status === 404) {
          this.database = null
        } else {
          throw err
        }
      } finally {
        this.loading = false
      }
    },

    async createDatabase(serverId: string) {
      const api = useApi()
      const data = await api.post<{ database: ServerDatabase }>(
        `/servers/${serverId}/database`
      )
      this.database = data.database
      return data.database
    },

    async deleteDatabase(serverId: string) {
      const api = useApi()
      await api.delete(`/servers/${serverId}/database`)
      this.database = null
    },

    async resetPassword(serverId: string) {
      const api = useApi()
      const data = await api.post<{ database: ServerDatabase }>(
        `/servers/${serverId}/database/reset-password`
      )
      this.database = data.database
      return data.database
    },
  },
})
