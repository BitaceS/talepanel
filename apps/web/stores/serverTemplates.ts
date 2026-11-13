import { defineStore } from 'pinia'
import type { ServerTemplate, ServerTemplateSettings } from '~/types'

export interface ServerTemplateInput {
  name: string
  description?: string
  icon?: string
  settings: ServerTemplateSettings
}

// Server templates: presets for the create-server form. Everyone can list
// them; only admins can add, change (custom only) or delete (custom only).
export const useServerTemplatesStore = defineStore('serverTemplates', {
  state: () => ({
    templates: [] as ServerTemplate[],
    loaded: false,
  }),

  getters: {
    builtin: (state) => state.templates.filter(t => t.is_builtin),
    custom: (state) => state.templates.filter(t => !t.is_builtin),
  },

  actions: {
    async fetchTemplates(force = false): Promise<void> {
      if (this.loaded && !force) return
      const api = useApi()
      const data = await api.get<{ templates: ServerTemplate[] }>('/server-templates')
      this.templates = data.templates
      this.loaded = true
    },

    async createTemplate(input: ServerTemplateInput): Promise<ServerTemplate> {
      const api = useApi()
      const data = await api.post<{ template: ServerTemplate }>('/admin/server-templates', input)
      this.templates.push(data.template)
      return data.template
    },

    async createFromServer(serverId: string, input: Omit<ServerTemplateInput, 'settings'>): Promise<ServerTemplate> {
      const api = useApi()
      const data = await api.post<{ template: ServerTemplate }>(
        `/admin/server-templates/from-server/${serverId}`, input)
      this.templates.push(data.template)
      return data.template
    },

    async deleteTemplate(id: string): Promise<void> {
      const api = useApi()
      await api.delete(`/admin/server-templates/${id}`)
      this.templates = this.templates.filter(t => t.id !== id)
    },
  },
})
