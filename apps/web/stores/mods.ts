import { defineStore } from 'pinia'
import type { InstalledMod, CFMod, CFSearchResult } from '~/types'

interface InstallPayload {
  filename: string
  display_name: string
  version: string
  download_url: string
  cf_mod_id: number | null
  cf_file_id: number | null
}

export const useModsStore = defineStore('mods', {
  state: () => ({
    installed: [] as InstalledMod[],
    searchResults: [] as CFMod[],
    searchTotal: 0,
    searchPage: 0,
    loadingInstalled: false,
    loadingSearch: false,
    installingId: null as number | null,
    removingFilename: null as string | null,
    error: null as string | null,
  }),

  actions: {
    async fetchInstalled(serverId: string): Promise<void> {
      this.loadingInstalled = true
      this.error = null
      this.installed = []
      try {
        const api = useApi()
        const data = await api.get<{ mods: InstalledMod[] }>(`/servers/${serverId}/mods`)
        this.installed = data.mods
      } catch (err: unknown) {
        const e = err as { data?: { error?: string }; message?: string }
        this.error = e.data?.error ?? e.message ?? 'Failed to load mods'
      } finally {
        this.loadingInstalled = false
      }
    },

    async search(q: string, page = 0): Promise<void> {
      this.loadingSearch = true
      this.error = null
      if (page === 0) this.searchResults = []
      try {
        const api = useApi()
        const data = await api.get<CFSearchResult>('/curseforge/search', { q, page, pageSize: 20 })
        this.searchResults = page === 0 ? data.data : [...this.searchResults, ...data.data]
        this.searchTotal = data.pagination.totalCount
        this.searchPage = page
      } catch (err: unknown) {
        const e = err as { data?: { error?: string }; message?: string }
        this.error = e.data?.error ?? e.message ?? 'CurseForge search failed'
      } finally {
        this.loadingSearch = false
      }
    },

    async install(serverId: string, payload: InstallPayload): Promise<void> {
      this.installingId = payload.cf_mod_id
      this.error = null
      try {
        const api = useApi()
        const data = await api.post<{ mod: InstalledMod }>(`/servers/${serverId}/mods`, payload)
        // Replace or append
        const idx = this.installed.findIndex(m => m.filename === data.mod.filename)
        if (idx !== -1) {
          this.installed[idx] = data.mod
        } else {
          this.installed.unshift(data.mod)
        }
      } catch (err: unknown) {
        const e = err as { data?: { error?: string }; message?: string }
        this.error = e.data?.error ?? e.message ?? 'Install failed'
        throw err
      } finally {
        this.installingId = null
      }
    },

    async remove(serverId: string, filename: string): Promise<void> {
      this.removingFilename = filename
      this.error = null
      try {
        const api = useApi()
        await api.delete(`/servers/${serverId}/mods/${encodeURIComponent(filename)}`)
        this.installed = this.installed.filter(m => m.filename !== filename)
      } catch (err: unknown) {
        const e = err as { data?: { error?: string }; message?: string }
        this.error = e.data?.error ?? e.message ?? 'Remove failed'
        throw err
      } finally {
        this.removingFilename = null
      }
    },
  },
})
